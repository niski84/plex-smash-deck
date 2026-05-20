package plexdash

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// captionsPlexClient is the small slice of *PlexClient the handler needs. A
// local interface keeps tests free of real HTTP.
type captionsPlexClient interface {
	ListPlaybackSessions(ctx context.Context) ([]PlaybackSession, error)
}

// CaptionFetcher abstracts a remote subtitle source so tests can stub it
// without spinning up a real HTTP server.
type CaptionFetcher interface {
	Available() bool
	Fetch(ctx context.Context, imdbID string) ([]byte, error)
}

// captionsResponse is the success payload for /api/playback/captions.
type captionsResponse struct {
	RatingKey  string `json:"ratingKey"`
	IMDbID     string `json:"imdbId"`
	TMDBID     int    `json:"tmdbId,omitempty"`
	Title      string `json:"title"`
	Year       int    `json:"year"`
	PlayerName string `json:"playerName,omitempty"`
	Captions   string `json:"captions"`
	Source     string `json:"source"`
	FetchedAt  string `json:"fetchedAt"`
	ByteCount  int    `json:"byteCount"`
}

// captionsErrorResponse is the body for non-200 responses (404). 204 bodies are empty.
type captionsErrorResponse struct {
	Error string `json:"error"`
}

// localSubtitlesDir returns the on-disk directory checked for pre-staged SRT files.
func localSubtitlesDir() string {
	return filepath.Clean("data/subtitles")
}

// handlePlaybackCaptions resolves the currently playing movie (or one specified
// via query params) and returns its captions as plain text.
//
// Errors NEVER produce a 500. We return 204 (no session/no key), 404 (no
// captions available), or 200 (success) and log via log.Printf("[captions] ...").
func (s *Server) handlePlaybackCaptions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		captionsRespondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	ratingKey := strings.TrimSpace(q.Get("ratingKey"))
	playerName := strings.TrimSpace(q.Get("player"))
	atStr := strings.TrimSpace(q.Get("at"))

	resolved := s.resolveCaptionsRatingKey(r.Context(), ratingKey, playerName)
	// If the live resolution failed and the caller supplied ?at=, fall back
	// first to the poller history ring, then to the dashboard's own cast
	// state (which works even when the Plex server is unreachable).
	if resolved.RatingKey == "" && atStr != "" {
		if at, err := time.Parse(time.RFC3339, atStr); err == nil {
			if s.playbackHistory != nil {
				if snap, ok := s.playbackHistory.LookupAt(at, playerName); ok {
					resolved = resolvedCaptionsTarget{RatingKey: snap.RatingKey, PlayerName: snap.PlayerName}
				}
			}
			if resolved.RatingKey == "" {
				if snap, ok := s.localPlaybackAt(at); ok {
					resolved = resolvedCaptionsTarget{RatingKey: snap.RatingKey, PlayerName: snap.PlayerName}
				}
			}
		} else {
			log.Printf("[captions] invalid ?at=%q: %v", atStr, err)
		}
	}
	if resolved.RatingKey == "" {
		// No active session and no explicit ratingKey — say so.
		w.WriteHeader(http.StatusNoContent)
		return
	}

	movie, ok := s.lookupMovieByRatingKey(r.Context(), resolved.RatingKey)
	if !ok {
		log.Printf("[captions] ratingKey=%s not found in cached library", resolved.RatingKey)
		captionsRespondError(w, http.StatusNotFound, "captions: movie not in library")
		return
	}
	imdbID := strings.TrimSpace(movie.IMDbID)
	if imdbID == "" {
		log.Printf("[captions] ratingKey=%s (%s) has no IMDB id", resolved.RatingKey, movie.Title)
		captionsRespondError(w, http.StatusNotFound, "captions: movie has no IMDB id")
		return
	}

	cache := s.captionsCacheOrDefault()

	// 1) Cache hit.
	if text, meta, ok := cache.Get(imdbID); ok && text != "" {
		captionsRespondSuccess(w, captionsResponse{
			RatingKey:  resolved.RatingKey,
			IMDbID:     imdbID,
			TMDBID:     movie.TMDBID,
			Title:      orZero(meta.Title, movie.Title),
			Year:       firstNonZero(meta.Year, movie.Year),
			PlayerName: resolved.PlayerName,
			Captions:   text,
			Source:     "cache",
			FetchedAt:  orZero(meta.FetchedAt, captionsNowRFC3339()),
			ByteCount:  firstNonZero(meta.ByteCount, len(text)),
		})
		return
	}

	// 2) Local SRT fallback.
	localPath := filepath.Join(localSubtitlesDir(), imdbID+".srt")
	if srt, err := os.ReadFile(localPath); err == nil {
		text := ParseSRTToPlainText(srt)
		if text == "" {
			log.Printf("[captions] local SRT %s parsed to empty text", localPath)
		} else {
			s.persistCaptions(cache, imdbID, text, movie, "local")
			captionsRespondSuccess(w, captionsResponse{
				RatingKey:  resolved.RatingKey,
				IMDbID:     imdbID,
				TMDBID:     movie.TMDBID,
				Title:      movie.Title,
				Year:       movie.Year,
				PlayerName: resolved.PlayerName,
				Captions:   text,
				Source:     "local",
				FetchedAt:  captionsNowRFC3339(),
				ByteCount:  len(text),
			})
			return
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		log.Printf("[captions] local SRT read %s: %v", localPath, err)
	}

	// 3) OpenSubtitles fetcher.
	fetcher := s.captionFetcherForRequest()
	if fetcher == nil || !fetcher.Available() {
		log.Printf("[captions] no remote fetcher available for %s (%s)", imdbID, movie.Title)
		captionsRespondError(w, http.StatusNotFound, "captions: not available")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	srt, err := fetcher.Fetch(ctx, imdbID)
	if err != nil {
		log.Printf("[captions] fetcher %s (%s): %v", imdbID, movie.Title, err)
		captionsRespondError(w, http.StatusNotFound, "captions: not available")
		return
	}
	text := ParseSRTToPlainText(srt)
	if strings.TrimSpace(text) == "" {
		log.Printf("[captions] fetcher returned empty text for %s (%s)", imdbID, movie.Title)
		captionsRespondError(w, http.StatusNotFound, "captions: not available")
		return
	}
	s.persistCaptions(cache, imdbID, text, movie, "opensubtitles")
	captionsRespondSuccess(w, captionsResponse{
		RatingKey:  resolved.RatingKey,
		IMDbID:     imdbID,
		TMDBID:     movie.TMDBID,
		Title:      movie.Title,
		Year:       movie.Year,
		PlayerName: resolved.PlayerName,
		Captions:   text,
		Source:     "opensubtitles",
		FetchedAt:  captionsNowRFC3339(),
		ByteCount:  len(text),
	})
}

type resolvedCaptionsTarget struct {
	RatingKey  string
	PlayerName string
}

// resolveCaptionsRatingKey applies the resolution order from the plan:
//  1. explicit ratingKey query param wins.
//  2. ?player=<substr> — case-insensitive substring match against PlayerName.
//  3. otherwise pick any active video session (first one).
//
// Returns an empty RatingKey when nothing matches; the caller responds 204.
func (s *Server) resolveCaptionsRatingKey(parentCtx context.Context, ratingKey, playerName string) resolvedCaptionsTarget {
	if ratingKey != "" {
		return resolvedCaptionsTarget{RatingKey: ratingKey, PlayerName: playerName}
	}
	client := s.plexClientForCaptions()
	if client == nil {
		return resolvedCaptionsTarget{}
	}
	ctx, cancel := context.WithTimeout(parentCtx, 15*time.Second)
	defer cancel()
	sessions, err := client.ListPlaybackSessions(ctx)
	if err != nil {
		log.Printf("[captions] ListPlaybackSessions: %v", err)
		return resolvedCaptionsTarget{}
	}
	if playerName != "" {
		needle := strings.ToLower(playerName)
		for _, sess := range sessions {
			if strings.Contains(strings.ToLower(sess.PlayerName), needle) {
				return resolvedCaptionsTarget{RatingKey: sess.RatingKey, PlayerName: sess.PlayerName}
			}
		}
		return resolvedCaptionsTarget{}
	}
	for _, sess := range sessions {
		t := strings.ToLower(strings.TrimSpace(sess.Type))
		if t != "" && t != "movie" && t != "episode" && t != "video" {
			continue
		}
		if sess.RatingKey == "" {
			continue
		}
		return resolvedCaptionsTarget{RatingKey: sess.RatingKey, PlayerName: sess.PlayerName}
	}
	return resolvedCaptionsTarget{}
}

// lookupMovieByRatingKey scans the in-memory library for a matching ratingKey.
func (s *Server) lookupMovieByRatingKey(parentCtx context.Context, ratingKey string) (Movie, bool) {
	ctx, cancel := context.WithTimeout(parentCtx, 15*time.Second)
	defer cancel()
	movies, err := s.cachedListMovies(ctx)
	if err != nil {
		log.Printf("[captions] cachedListMovies: %v", err)
		return Movie{}, false
	}
	for _, m := range movies {
		if m.RatingKey == ratingKey {
			return m, true
		}
	}
	return Movie{}, false
}

func (s *Server) persistCaptions(cache *CaptionsCache, imdbID, text string, movie Movie, source string) {
	meta := CaptionsMeta{
		Title:     movie.Title,
		Year:      movie.Year,
		Source:    source,
		FetchedAt: captionsNowRFC3339(),
		ByteCount: len(text),
	}
	if err := cache.Put(imdbID, text, meta); err != nil {
		log.Printf("[captions] cache put %s: %v", imdbID, err)
	}
}

// ── plumbing: tests can override these via fields on *Server (see below) ────

// captionsCacheOrDefault returns s.captionsCache if set, otherwise a default
// rooted at data/captions-cache.
func (s *Server) captionsCacheOrDefault() *CaptionsCache {
	if s.captionsCache != nil {
		return s.captionsCache
	}
	return NewCaptionsCache("data/captions-cache")
}

// captionFetcherForRequest returns the configured remote fetcher (may be nil).
func (s *Server) captionFetcherForRequest() CaptionFetcher {
	return s.captionFetcher
}

// plexClientForCaptions returns the captions-scoped Plex client. Tests inject a
// stub via s.captionsPlexClient; production wraps a fresh PlexClient(snapshot()).
func (s *Server) plexClientForCaptions() captionsPlexClient {
	if s.captionsPlexClient != nil {
		return s.captionsPlexClient
	}
	cfg := s.snapshot()
	return NewPlexClient(cfg)
}

// ── helpers ──────────────────────────────────────────────────────────────────

func captionsRespondSuccess(w http.ResponseWriter, payload captionsResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("[captions] encode response: %v", err)
	}
}

func captionsRespondError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(captionsErrorResponse{Error: msg})
}

func orZero(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func firstNonZero(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}
