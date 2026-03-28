package plexdash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The in-memory movie list is reused until explicitly invalidated (?nocache=1 on
// /api/movies, snapshot flows, etc.). Discovery, the dashboard, and other handlers
// all share this same slice so Plex is not refetched on a timer.

type Server struct {
	mu           sync.RWMutex
	cfg          Config
	settingsPath string

	mlMu       sync.RWMutex
	mlMovies   []Movie
	mlCachedAt time.Time
	mlKey      string // library key the cache was built for

	discJobs *discoveryJobStore

	playbackMu    sync.RWMutex
	localPlayback localPlaybackSend

	connMu       sync.RWMutex
	connectivity ConnectivityPayload

	// Plex stream throughput probe (connectivity): rate-limited, see connectivity.go.
	streamProbeMu     sync.Mutex
	plexStreamCache   PlexStreamMetrics
	lastStreamProbeAt time.Time

	// Plex library title count for /api/movies/cache-status — avoids hammering Plex
	// when several tabs poll or the hint refreshes often.
	remoteCountMu     sync.Mutex
	remoteCountVal    int
	remoteCountAt     time.Time
	remoteCountLibKey string
}

type localPlaybackSend struct {
	Target       string
	Titles       []string
	SentAt       time.Time
	Source       string // e.g. movies_play, playlist_play
	PlaylistName string
	Shuffled     bool
}

// persistedPlaybackState is written to data/playback-state.json so the dashboard
// can show the last webOS-direct queue after a server restart (Plex sessions
// usually omit that playback).
type persistedPlaybackState struct {
	Target       string   `json:"target"`
	Titles       []string `json:"titles"`
	SentAt       string   `json:"sentAt"`
	Source       string   `json:"source"`
	PlaylistName string   `json:"playlistName,omitempty"`
	Shuffled     bool     `json:"shuffled"`
}

func playbackStatePath() string {
	return filepath.Clean("data/playback-state.json")
}

const (
	minAllowedYear         = 1982
	maxAllowedYear         = 2016
	localPlaybackFreshness = 45 * time.Minute
)

func NewServer(cfg Config, client *PlexClient) *Server {
	_ = client
	s := &Server{
		cfg:          cfg,
		settingsPath: defaultSettingsPath(),
		discJobs:     newDiscoveryJobStore(),
	}
	s.loadPlaybackState()
	return s
}

func (s *Server) loadPlaybackState() {
	b, err := os.ReadFile(playbackStatePath())
	if err != nil {
		return
	}
	var raw persistedPlaybackState
	if err := json.Unmarshal(b, &raw); err != nil {
		return
	}
	if len(raw.Titles) == 0 {
		return
	}
	t, err := time.Parse(time.RFC3339, raw.SentAt)
	if err != nil {
		return
	}
	s.playbackMu.Lock()
	s.localPlayback = localPlaybackSend{
		Target:       raw.Target,
		Titles:       raw.Titles,
		SentAt:       t,
		Source:       raw.Source,
		PlaylistName: raw.PlaylistName,
		Shuffled:     raw.Shuffled,
	}
	s.playbackMu.Unlock()
}

func (s *Server) persistPlaybackState(state localPlaybackSend) {
	if len(state.Titles) == 0 || state.SentAt.IsZero() {
		_ = os.Remove(playbackStatePath())
		return
	}
	raw := persistedPlaybackState{
		Target:       state.Target,
		Titles:       state.Titles,
		SentAt:       state.SentAt.UTC().Format(time.RFC3339),
		Source:       state.Source,
		PlaylistName: state.PlaylistName,
		Shuffled:     state.Shuffled,
	}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(playbackStatePath()), 0o755); err != nil {
		fmt.Printf("[playback] mkdir: %v\n", err)
		return
	}
	tmp := playbackStatePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		fmt.Printf("[playback] write: %v\n", err)
		return
	}
	if err := os.Rename(tmp, playbackStatePath()); err != nil {
		fmt.Printf("[playback] rename: %v\n", err)
	}
}

// cachedListMovies returns the in-memory movie list when the library key matches,
// otherwise fetches from Plex and updates the cache.
func (s *Server) cachedListMovies(ctx context.Context) ([]Movie, error) {
	cfg := s.snapshot()
	s.mlMu.RLock()
	if s.mlKey == cfg.LibraryKey && len(s.mlMovies) > 0 {
		movies := s.mlMovies
		s.mlMu.RUnlock()
		return movies, nil
	}
	s.mlMu.RUnlock()

	client := NewPlexClient(cfg)
	movies, err := client.ListMovies(ctx, cfg.LibraryKey)
	if err != nil {
		return nil, err
	}

	s.mlMu.Lock()
	s.mlMovies = movies
	s.mlCachedAt = time.Now()
	s.mlKey = cfg.LibraryKey
	s.mlMu.Unlock()

	return movies, nil
}

// invalidateMovieListCache forces the next cachedListMovies call to fetch fresh data.
func (s *Server) invalidateMovieListCache() {
	s.mlMu.Lock()
	s.mlMovies = nil
	s.mlMu.Unlock()
	s.invalidatePlexStreamProbe()
}

func (s *Server) invalidatePlexStreamProbe() {
	s.streamProbeMu.Lock()
	s.lastStreamProbeAt = time.Time{}
	s.plexStreamCache = PlexStreamMetrics{}
	s.streamProbeMu.Unlock()
}

// WarmLibraryCacheOnStartup loads the Plex movie library into process memory once
// after the server process starts. Discovery, /api/movies, and other features
// then see a populated list without requiring a browser "Load Movies". The
// daily snapshot worker still invalidates and refetches Plex at run time; warmup
// mainly avoids an empty cache between restarts and the first scheduled job.
// Runs in a goroutine from main; logs errors and does not exit the process.
func (s *Server) WarmLibraryCacheOnStartup(ctx context.Context) {
	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	if !client.IsConfigured() {
		fmt.Println("[warmup] skipped — Plex base URL or token not configured")
		return
	}
	if strings.TrimSpace(cfg.LibraryKey) == "" {
		fmt.Println("[warmup] skipped — library key empty")
		return
	}
	tctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	fmt.Printf("[warmup] loading Plex library section %s into memory...\n", cfg.LibraryKey)
	_, err := s.cachedListMovies(tctx)
	if err != nil {
		fmt.Printf("[warmup] failed — %v\n", err)
		return
	}
	s.mlMu.RLock()
	n := len(s.mlMovies)
	s.mlMu.RUnlock()
	fmt.Printf("[warmup] ready — %d titles in memory (shared with Discovery & snapshots metadata)\n", n)
}

func (s *Server) snapshot() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Server) replaceConfig(cfg Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cfg = cfg
}

func playerNameMatchesTarget(playerName, target string) bool {
	t := strings.TrimSpace(strings.ToLower(target))
	if t == "" {
		return false
	}
	p := strings.TrimSpace(strings.ToLower(playerName))
	if p == "" {
		return false
	}
	return strings.Contains(p, t) || strings.Contains(t, p) || p == t
}

// pickPrimarySession returns the best session for the target player name, or the
// first active session when the target is empty or does not match.
func pickPrimarySession(sessions []PlaybackSession, targetName string) (PlaybackSession, bool, bool) {
	if len(sessions) == 0 {
		return PlaybackSession{}, false, false
	}
	tn := strings.TrimSpace(targetName)
	if tn != "" {
		for i := range sessions {
			if playerNameMatchesTarget(sessions[i].PlayerName, tn) {
				return sessions[i], true, true
			}
		}
		return sessions[0], true, false
	}
	return sessions[0], true, true
}

func (s *Server) recordLocalPlayback(target string, titles []string, source, playlistName string, shuffle bool) {
	if len(titles) == 0 {
		return
	}
	clean := make([]string, 0, len(titles))
	for _, t := range titles {
		t = strings.TrimSpace(t)
		if t != "" {
			clean = append(clean, t)
		}
	}
	if len(clean) == 0 {
		return
	}
	s.playbackMu.Lock()
	s.localPlayback = localPlaybackSend{
		Target:       target,
		Titles:       clean,
		SentAt:       time.Now(),
		Source:       source,
		PlaylistName: playlistName,
		Shuffled:     shuffle,
	}
	st := s.localPlayback
	s.playbackMu.Unlock()
	s.persistPlaybackState(st)
}

func buildPlaybackStatusPayload(cfg Config, sessions []PlaybackSession, sessionsErr error, local localPlaybackSend) map[string]any {
	out := map[string]any{
		"targetClientName": strings.TrimSpace(cfg.TargetClientName),
		"plexSessions":     sessions,
	}
	if sessionsErr != nil {
		out["plexSessionsError"] = sessionsErr.Error()
	}
	now := time.Now()
	var localPayload map[string]any
	var localStale bool
	if !local.SentAt.IsZero() && len(local.Titles) > 0 {
		localStale = now.Sub(local.SentAt) > localPlaybackFreshness
		localPayload = map[string]any{
			"target":       local.Target,
			"titles":       local.Titles,
			"sentAt":       local.SentAt.UTC().Format(time.RFC3339),
			"source":       local.Source,
			"playlistName": local.PlaylistName,
			"shuffled":     local.Shuffled,
			"stale":        localStale,
			"queueLength":  len(local.Titles),
		}
		out["localSend"] = localPayload
	}

	primaryFrom := "idle"
	summaryLine := "No Plex session. Play from this dashboard (LG webOS direct) to show the last queue here — Plex usually does not report that as a session."
	var primary any

	if len(sessions) > 0 {
		sess, ok, matched := pickPrimarySession(sessions, cfg.TargetClientName)
		if ok {
			primaryFrom = "plex_session"
			title := sess.DisplayTitle()
			summaryLine = fmt.Sprintf("%s · %s — %s", sess.PlayerName, sess.PlayerState, title)
			if sess.DurationMs > 0 {
				summaryLine += fmt.Sprintf(" · %.0f%%", sess.ProgressPercent)
			}
			if !matched && strings.TrimSpace(cfg.TargetClientName) != "" {
				summaryLine += " (first active session; target name did not match)"
			}
			primary = sess
		}
	} else if localPayload != nil && !localStale {
		primaryFrom = "local_send"
		titles := local.Titles
		t0 := titles[0]
		rest := len(titles) - 1
		if rest > 0 {
			summaryLine = fmt.Sprintf("%s (webOS queue): %s + %d more — TV position not reported by Plex", local.Target, t0, rest)
		} else {
			summaryLine = fmt.Sprintf("%s (webOS queue): %s — TV position not reported by Plex", local.Target, t0)
		}
		primary = localPayload
	} else if localPayload != nil {
		summaryLine = "No Plex session; last send from this app is older than 45m (see localSend if needed)."
	}

	out["primaryFrom"] = primaryFrom
	out["summaryLine"] = summaryLine
	out["primary"] = primary
	return out
}

func (s *Server) handlePlaybackStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	sessions, sessionsErr := client.ListPlaybackSessions(ctx)
	if sessionsErr != nil {
		sessions = nil
	}
	s.playbackMu.RLock()
	local := s.localPlayback
	s.playbackMu.RUnlock()
	data := buildPlaybackStatusPayload(cfg, sessions, sessionsErr, local)
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/connectivity", s.handleConnectivity)
	mux.HandleFunc("/api/settings", s.handleSettings)
	mux.HandleFunc("/api/movies", s.handleMovies)
	mux.HandleFunc("/api/omdb-ratings", s.handleOMDbRatings)
	mux.HandleFunc("/api/movies/cache-status", s.handleMovieCacheStatus)
	mux.HandleFunc("/api/branding/banner-thumb", s.handleBrandingBannerThumb)
	mux.HandleFunc("/api/movies/sync-recent", s.handleMoviesSyncRecent)
	mux.HandleFunc("/api/genres", s.handleGenres)
	mux.HandleFunc("/api/players", s.handlePlayers)
	mux.HandleFunc("/api/plex/companion/control", s.handlePlexCompanionControl)
	mux.HandleFunc("/api/lg/volume", s.handleLgVolume)
	mux.HandleFunc("/api/playback/status", s.handlePlaybackStatus)
	mux.HandleFunc("/api/discovery/person-suggest", s.handleDiscoveryPersonSuggest)
	mux.HandleFunc("/api/discovery/collaborators", s.handleDiscoveryCollaborators)
	mux.HandleFunc("/api/discovery/filmography", s.handleDiscoveryFilmography)
	mux.HandleFunc("/api/discovery/tmdb-genres", s.handleDiscoveryTMDBGenres)
	mux.HandleFunc("/api/discovery/studio", s.handleDiscoveryStudio)
	mux.HandleFunc("/api/discovery/start", s.handleDiscoveryStart)
	mux.HandleFunc("/api/discovery/poll", s.handleDiscoveryPoll)
	mux.HandleFunc("/api/discovery/cache/invalidate", s.handleDiscoveryCacheInvalidate)
	mux.HandleFunc("/api/discovery/poster", s.handleDiscoveryPoster)
	mux.HandleFunc("/api/discovery/radarr/add", s.handleDiscoveryAddToRadarr)
	mux.HandleFunc("/api/snapshots/latest-diff", s.handleSnapshotLatestDiff)
	mux.HandleFunc("/api/snapshots/missing", s.handleSnapshotMissing)
	mux.HandleFunc("/api/snapshots/diff", s.handleSnapshotDiff)
	mux.HandleFunc("/api/snapshots/patterns", s.handleSnapshotPatterns)
	mux.HandleFunc("/api/snapshots/", s.handleSnapshotByID)
	mux.HandleFunc("/api/snapshots", s.handleSnapshots)
	mux.HandleFunc("/api/help/docs", s.handleHelpDocs)
	mux.HandleFunc("/api/help/doc", s.handleHelpDoc)

	mux.HandleFunc("/api/playlists/preview", s.handlePreviewPlaylist)
	mux.HandleFunc("/api/playlists", s.handleListPlaylists)
	mux.HandleFunc("/api/playlists/items", s.handlePlaylistItems)
	mux.HandleFunc("/api/plex/thumb", s.handlePlexThumb)
	mux.HandleFunc("/api/movies/play", s.handleMoviesPlay)
	mux.HandleFunc("/api/playlists/play", s.handlePlayPlaylist)
	mux.HandleFunc("/api/playlists/random", s.handleCreateRandomPlaylist)
	mux.HandleFunc("/api/playlists/by-people", s.handleCreatePlaylistByPeople)
	mux.HandleFunc("/api/playlists/by-genre-rating", s.handleCreatePlaylistByGenreRating)
	mux.HandleFunc("/api/playlists/random-play", s.handleCreateAndPlayRandomPlaylist)

	// Serve static dashboard UI.
	mux.Handle("/", http.FileServer(http.Dir(filepath.Clean("web/plex-dashboard"))))

	return requestLogMiddleware(mux)
}

func (s *Server) handleHelpDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	entries, err := os.ReadDir(filepath.Clean("docs"))
	if err != nil {
		if os.IsNotExist(err) {
			respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"docs": []map[string]string{}}})
			return
		}
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
		return
	}
	docs := make([]map[string]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.TrimSpace(e.Name())
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		title := strings.TrimSuffix(name, filepath.Ext(name))
		title = strings.ReplaceAll(title, "-", " ")
		docs = append(docs, map[string]string{"name": name, "title": strings.TrimSpace(title)})
	}
	sort.SliceStable(docs, func(i, j int) bool {
		return strings.ToLower(docs[i]["name"]) < strings.ToLower(docs[j]["name"])
	})
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"docs": docs}})
}

func (s *Server) handleHelpDoc(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	name := filepath.Base(strings.TrimSpace(r.URL.Query().Get("name")))
	if name == "" || strings.Contains(name, "..") || !strings.HasSuffix(strings.ToLower(name), ".md") {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid doc name"})
		return
	}
	path := filepath.Join(filepath.Clean("docs"), name)
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			respondJSON(w, http.StatusNotFound, apiResponse{Success: false, Error: "doc not found"})
			return
		}
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"name":     name,
		"markdown": string(body),
	}})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"service": "plex-dashboard",
		},
	})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg := s.snapshot()
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: cfg})
		return
	case http.MethodPost:
		var req Config
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON body"})
			return
		}
		req.PlexBaseURL = strings.TrimRight(req.PlexBaseURL, "/")
		req.RadarrURL = strings.TrimRight(req.RadarrURL, "/")
		if req.Port == "" {
			req.Port = s.snapshot().Port
		}
		if req.LibraryKey == "" {
			req.LibraryKey = "1"
		}
		if req.TargetClientName == "" {
			req.TargetClientName = "Living Room"
		}
		if strings.TrimSpace(req.AppDisplayName) == "" {
			req.AppDisplayName = "plex-smash-deck"
		}
		req.HeroBannerURL = strings.TrimSpace(req.HeroBannerURL)
		if req.HeroBannerHeight <= 0 {
			req.HeroBannerHeight = 140
		}
		if req.HeroBannerHeight < 80 {
			req.HeroBannerHeight = 80
		}
		if req.HeroBannerHeight > 420 {
			req.HeroBannerHeight = 420
		}
		if req.RadarrProfileID <= 0 {
			req.RadarrProfileID = 1
		}
		if req.SnapshotHour < 0 || req.SnapshotHour > 23 {
			req.SnapshotHour = 2
		}

		// Persist settings and refresh in-memory configuration.
		if err := SavePersistedConfig(s.settingsPath, req); err != nil {
			respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
			return
		}
		s.replaceConfig(req)
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"saved": true}})
		return
	default:
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
	}
}

func (s *Server) handleMovies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()

	// ?nocache=1 lets the "Refresh Movies" button bypass and repopulate the cache.
	if r.URL.Query().Get("nocache") == "1" {
		s.invalidateMovieListCache()
	}

	cfg := s.snapshot()
	fmt.Printf("[API] /api/movies libraryKey=%s cached=%v\n", cfg.LibraryKey, r.URL.Query().Get("nocache") != "1")
	movies, err := s.cachedListMovies(ctx)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	limit := len(movies) // default: return all
	actor := r.URL.Query().Get("actor")
	director := r.URL.Query().Get("director")
	genre := r.URL.Query().Get("genre")
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr == nil && parsed > 0 {
			limit = parsed
		}
	}
	if actor != "" || director != "" {
		movies = filterMoviesByPeople(movies, actor, director)
	}
	if genre != "" {
		movies = filterMoviesByGenreRatingYear(movies, genre, 0, 0, 9999)
	}
	if limit > len(movies) {
		limit = len(movies)
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"count":  len(movies),
			"movies": movies[:limit],
		},
	})
}

// handleOMDbRatings: GET /api/omdb-ratings?tmdbId=123 or &imdbId=tt1234567
// Returns OMDb IMDb/RT/Metacritic rows plus optional TMDB vote average when tmdbId is provided.
func (s *Server) handleOMDbRatings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 22*time.Second)
	defer cancel()
	cfg := s.snapshot()
	tmdbStr := strings.TrimSpace(r.URL.Query().Get("tmdbId"))
	imdb := strings.TrimSpace(r.URL.Query().Get("imdbId"))
	var tmdbID int
	if tmdbStr != "" {
		tmdbID, _ = strconv.Atoi(tmdbStr)
	}
	out := map[string]any{"ok": false}
	omdbKey := strings.TrimSpace(cfg.OMDbAPIKey)
	tmdbKey := strings.TrimSpace(cfg.TMDBAPIKey)
	if omdbKey == "" && tmdbKey == "" {
		out["reason"] = "no_api_keys"
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: out})
		return
	}
	if imdb == "" && tmdbID > 0 && tmdbKey != "" {
		var err error
		imdb, err = tmdbFetchMovieExternalIDs(ctx, tmdbKey, tmdbID)
		if err != nil {
			imdb = ""
		}
	}
	var entries []OMDbRatingEntry
	if omdbKey != "" && imdb != "" {
		detail, err := FetchOMDbRatingsDetail(ctx, omdbKey, imdb)
		if err != nil {
			respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
			return
		}
		entries = append(entries, detail.Entries...)
	}
	if tmdbID > 0 && tmdbKey != "" {
		if v, ok := tmdbFetchMovieVoteAverage(ctx, tmdbKey, tmdbID); ok {
			entries = append(entries, OMDbRatingEntry{
				Source:  "tmdb",
				Label:   "TMDB",
				Display: fmt.Sprintf("%.1f", v),
				Score10: v,
			})
		}
	}
	if imdb != "" {
		out["imdbId"] = imdb
	}
	out["entries"] = entries
	out["average10"] = averageScore10(entries)
	if len(entries) > 0 {
		out["ok"] = true
	} else if omdbKey != "" && imdb == "" {
		out["reason"] = "no_imdb_id"
	} else {
		out["reason"] = "no_scores"
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: out})
}

// handleMovieCacheStatus returns how old the in-memory movie list is and (when
// Plex is configured) a lightweight remote title count from Plex for comparison.
func (s *Server) handleMovieCacheStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	cfg := s.snapshot()
	client := NewPlexClient(cfg)

	s.mlMu.RLock()
	cachedCount := len(s.mlMovies)
	cachedAt := s.mlCachedAt
	cacheKey := s.mlKey
	s.mlMu.RUnlock()

	out := map[string]any{
		"cachedCount":     cachedCount,
		"libraryKey":      cfg.LibraryKey,
		"cacheKeyMatches": cacheKey == cfg.LibraryKey,
	}
	if cachedCount > 0 && !cachedAt.IsZero() {
		out["cachedAtISO"] = cachedAt.UTC().Format(time.RFC3339)
	}
	if !client.IsConfigured() {
		out["plexConfigured"] = false
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: out})
		return
	}
	out["plexConfigured"] = true

	const remoteCountTTL = 90 * time.Second
	now := time.Now()
	s.remoteCountMu.Lock()
	if s.remoteCountLibKey == cfg.LibraryKey && !s.remoteCountAt.IsZero() && now.Sub(s.remoteCountAt) < remoteCountTTL {
		n := s.remoteCountVal
		ageSec := int(now.Sub(s.remoteCountAt).Seconds())
		s.remoteCountMu.Unlock()
		out["plexRemoteCount"] = n
		out["plexRemoteCountCached"] = true
		out["plexRemoteCountCachedAgeSec"] = ageSec
		if cacheKey == cfg.LibraryKey && cachedCount > 0 {
			out["deltaVsCache"] = n - cachedCount
		}
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: out})
		return
	}
	s.remoteCountMu.Unlock()

	n, err := client.LibraryMovieTotalCount(ctx, cfg.LibraryKey)
	if err != nil {
		if errors.Is(err, errPlexTotalSizeMissing) {
			out["remoteCountErrorCode"] = "totalSizeMissing"
		} else {
			out["remoteCountError"] = err.Error()
		}
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: out})
		return
	}
	s.remoteCountMu.Lock()
	s.remoteCountVal = n
	s.remoteCountAt = time.Now()
	s.remoteCountLibKey = cfg.LibraryKey
	s.remoteCountMu.Unlock()

	out["plexRemoteCount"] = n
	if cacheKey == cfg.LibraryKey && cachedCount > 0 {
		out["deltaVsCache"] = n - cachedCount
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: out})
}

// pickMovieForBannerPoster chooses a library title for the dashboard hero when
// no custom banner URL is set: highest ViewCount, else first with a rating key.
func pickMovieForBannerPoster(movies []Movie) (Movie, bool) {
	if len(movies) == 0 {
		return Movie{}, false
	}
	bestIdx := -1
	for i := range movies {
		if movies[i].RatingKey == "" {
			continue
		}
		if bestIdx < 0 {
			bestIdx = i
			continue
		}
		if movies[i].ViewCount > movies[bestIdx].ViewCount {
			bestIdx = i
		}
	}
	if bestIdx >= 0 {
		return movies[bestIdx], true
	}
	for i := range movies {
		if movies[i].RatingKey != "" {
			return movies[i], true
		}
	}
	return Movie{}, false
}

// handleBrandingBannerThumb returns JSON { "url": "/api/plex/thumb?ratingKey=..." }
// so the dashboard can show a poster even before the client has loaded /api/movies.
func (s *Server) handleBrandingBannerThumb(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	movies, err := s.cachedListMovies(ctx)
	if err != nil {
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"url": "", "error": err.Error()}})
		return
	}
	m, ok := pickMovieForBannerPoster(movies)
	if !ok {
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"url": ""}})
		return
	}
	u := "/api/plex/thumb?ratingKey=" + url.QueryEscape(m.RatingKey)
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"url": u}})
}

// handleMoviesSyncRecent merges titles from Plex’s recentlyAdded feed into the
// in-memory list. Cheaper than a full library fetch when only new movies were
// added. Does not remove titles (use full Refresh for that). Discovery/TMDB
// disk caches are untouched.
func (s *Server) handleMoviesSyncRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	if !client.IsConfigured() {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "Plex not configured"})
		return
	}

	s.mlMu.RLock()
	base := s.mlMovies
	cacheKey := s.mlKey
	s.mlMu.RUnlock()

	if len(base) == 0 || cacheKey != cfg.LibraryKey {
		respondJSON(w, http.StatusBadRequest, apiResponse{
			Success: false,
			Error:   "server has no movie list yet — use Load / Refresh Movies once for a full Plex sync",
		})
		return
	}

	remote, err := client.LibraryMovieTotalCount(ctx, cfg.LibraryKey)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	local := len(base)
	if remote <= local {
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
			"added":           0,
			"total":           local,
			"plexRemoteCount": remote,
			"message":         "No new titles to merge (or Plex count is not higher than this list — use full Refresh if you removed titles).",
		}})
		return
	}

	delta := remote - local
	existing := make(map[string]struct{}, len(base))
	for _, m := range base {
		existing[m.RatingKey] = struct{}{}
	}

	want := delta + 32
	newMovies, err := client.FetchNewMoviesFromRecentlyAdded(ctx, cfg.LibraryKey, existing, want)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}

	merged := append(append([]Movie(nil), base...), newMovies...)
	SortMoviesDefaultView(merged)

	s.mlMu.Lock()
	s.mlMovies = merged
	s.mlCachedAt = time.Now()
	s.mlKey = cfg.LibraryKey
	s.mlMu.Unlock()

	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"added":           len(newMovies),
		"total":           len(merged),
		"plexRemoteCount": remote,
		"message":         "merged from Plex recently added",
	}})
}

// handlePlexThumb proxies a Plex poster by ratingKey, keeping the Plex token
// server-side. Hi-res bytes are stored under data/plex-thumb-cache/full/;
// older flat data/plex-thumb-cache/{ratingKey}.jpg files still serve until
// replaced. The browser may cache responses for 1 year.
func (s *Server) handlePlexThumb(w http.ResponseWriter, r *http.Request) {
	ratingKey := r.URL.Query().Get("ratingKey")
	if ratingKey == "" {
		http.NotFound(w, r)
		return
	}
	// Sanitise ratingKey — must be numeric-ish to form a safe filename.
	for _, c := range ratingKey {
		if (c < '0' || c > '9') && c != '-' {
			http.Error(w, "invalid ratingKey", http.StatusBadRequest)
			return
		}
	}
	cfg := s.snapshot()
	// Request a large poster from Plex so the disk cache holds the best
	// resolution Plex will provide (capped by source). Legacy flat cache files
	// still serve until a hi-res fetch lands under full/.
	thumbURL := cfg.PlexBaseURL + "/library/metadata/" + ratingKey + "/thumb?X-Plex-Token=" + cfg.PlexToken +
		"&width=3840&height=5760"
	fullPath := filepath.Join(plexThumbCacheDir, "full", ratingKey+".jpg")
	legacyPath := filepath.Join(plexThumbCacheDir, ratingKey+".jpg")

	if serveCachedPosterFile(w, fullPath, "image/jpeg") {
		return
	}
	if serveCachedPosterFile(w, legacyPath, "image/jpeg") {
		return
	}
	if !serveOrCachePoster(w, thumbURL, fullPath, "image/jpeg") {
		http.NotFound(w, r)
	}
}

// handleMoviesPlay streams a caller-supplied list of Plex movies to the TV via
// the webOS native media player. The caller provides ratingKey + partKey so we
// never need a second round-trip to Plex just to locate the file URL.
func (s *Server) handleMoviesPlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	var req struct {
		Items []struct {
			RatingKey string `json:"ratingKey"`
			PartKey   string `json:"partKey"`
			Container string `json:"container"`
			Title     string `json:"title"`
			PartSize  int64  `json:"partSize"`
		} `json:"items"`
		ClientName string `json:"clientName"`
		Shuffle    bool   `json:"shuffle"`
		// Transport "companion" uses Plex HTTP companion (playMedia) — works for many
		// players with a non-SSAP URI. Empty or "webos" keeps the previous LG webOS direct path.
		Transport string `json:"transport"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON: " + err.Error()})
		return
	}
	if len(req.Items) == 0 {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "items array is empty"})
		return
	}

	cfg := s.snapshot()
	streamItems := make([]WebOSStreamItem, 0, len(req.Items))
	for _, item := range req.Items {
		mimeContainer := item.Container
		if mimeContainer == "" {
			mimeContainer = "mp4"
		}
		streamItems = append(streamItems, WebOSStreamItem{
			StreamURL: cfg.PlexBaseURL + item.PartKey + "?X-Plex-Token=" + cfg.PlexToken,
			Title:     item.Title,
			Container: mimeContainer,
			Size:      item.PartSize,
		})
	}

	if req.Shuffle {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		rng.Shuffle(len(streamItems), func(i, j int) { streamItems[i], streamItems[j] = streamItems[j], streamItems[i] })
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	client := NewPlexClient(cfg)
	clientName := strings.TrimSpace(req.ClientName)
	if clientName == "" {
		clientName = cfg.TargetClientName
	}

	transport := strings.ToLower(strings.TrimSpace(req.Transport))
	if transport == "companion" {
		if len(req.Items) != 1 {
			respondJSON(w, http.StatusBadRequest, apiResponse{
				Success: false,
				Error:   "transport=companion supports exactly one movie per request (webOS direct still queues multiple titles)",
			})
			return
		}
		rk := strings.TrimSpace(req.Items[0].RatingKey)
		if rk == "" {
			respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "ratingKey is required"})
			return
		}
		lp := client.ListPlayers(ctx)
		player, err := SelectPlayerForCompanion(lp.Players, clientName)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: err.Error()})
			return
		}
		if err := client.playMovieOnClient(ctx, player, rk); err != nil {
			respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
			return
		}
		title := strings.TrimSpace(req.Items[0].Title)
		if title == "" {
			title = "item " + rk
		}
		s.recordLocalPlayback(player.Name, []string{title}, "movies_play_companion", "", false)
		respondJSON(w, http.StatusOK, apiResponse{
			Success: true,
			Data: map[string]any{
				"count":     1,
				"target":    player.Name,
				"transport": "companion",
			},
		})
		return
	}

	targetName, err := client.PlayStreamItemsOnTV(ctx, streamItems, clientName)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}

	titles := make([]string, len(streamItems))
	for i, it := range streamItems {
		titles[i] = strings.TrimSpace(it.Title)
	}
	s.recordLocalPlayback(targetName, titles, "movies_play", "", req.Shuffle)

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"count":  len(streamItems),
			"target": targetName,
		},
	})
}

// handlePlexCompanionControl: POST JSON { "clientName", "action", "seekMs?" } — Plex companion
// playback commands (pause, play, skipNext, …). Requires a non-SSAP player (see SelectPlayerForCompanion).
func (s *Server) handlePlexCompanionControl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	var req struct {
		ClientName string `json:"clientName"`
		Action     string `json:"action"`
		SeekMs     int64  `json:"seekMs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON: " + err.Error()})
		return
	}
	action := strings.TrimSpace(strings.ToLower(req.Action))
	if action == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "action is required"})
		return
	}

	cfg := s.snapshot()
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()

	client := NewPlexClient(cfg)
	clientName := strings.TrimSpace(req.ClientName)
	if clientName == "" {
		clientName = cfg.TargetClientName
	}
	lp := client.ListPlayers(ctx)
	player, err := SelectPlayerForCompanion(lp.Players, clientName)
	if err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: err.Error()})
		return
	}

	apiAction := action
	switch action {
	case "seekto", "seek_to":
		apiAction = "seekTo"
	}

	if err := client.SendPlaybackControl(ctx, player, apiAction, req.SeekMs); err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"target": player.Name,
			"action": apiAction,
		},
	})
}

func (s *Server) handleGenres(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	genres, err := client.ListGenres(ctx, cfg.LibraryKey)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"genres": genres,
		},
	})
}

func discoveryQueryBool(raw string) bool {
	v := strings.TrimSpace(strings.ToLower(raw))
	return v == "1" || v == "true" || v == "yes"
}

func (s *Server) handleDiscoveryFilmography(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}

	person := strings.TrimSpace(r.URL.Query().Get("person"))
	role := strings.TrimSpace(r.URL.Query().Get("role"))
	playlistTitle := strings.TrimSpace(r.URL.Query().Get("playlistTitle"))
	director := strings.TrimSpace(r.URL.Query().Get("director"))
	coActor := strings.TrimSpace(r.URL.Query().Get("coActor"))
	minYear, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("minYear")))
	maxYear, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("maxYear")))
	minRating, _ := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("minRating")), 64)
	excludeNonTheatrical := discoveryQueryBool(r.URL.Query().Get("excludeNonTheatrical"))
	if person == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "person query is required"})
		return
	}

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	plexMovies, err := s.cachedListMovies(ctx)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: "plex library unavailable: " + err.Error()})
		return
	}

	var cacheStats DiscoveryCacheStats
	items, err := AnalyzeFilmography(ctx, cfg, client, plexMovies, person, role, playlistTitle, director, coActor, minYear, maxYear, minRating, nil, excludeNonTheatrical, &cacheStats)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}

	missing := 0
	nPosterURL := 0
	nPosterPath := 0
	for _, item := range items {
		if !item.InLibrary {
			missing++
		}
		if strings.TrimSpace(item.PosterURL) != "" {
			nPosterURL++
		}
		if strings.TrimSpace(item.PosterPath) != "" {
			nPosterPath++
		}
	}
	DiscoveryDebugf("[filmography] person=%q role=%q total=%d missing_lib=%d posterUrl_nonempty=%d posterPath_nonempty=%d cache=%+v",
		person, role, len(items), missing, nPosterURL, nPosterPath, cacheStats)
	if len(items) > 0 {
		s := items[0]
		DiscoveryDebugf("[filmography] sample tmdbId=%d title=%q posterUrl_len=%d posterPath=%q",
			s.TMDBID, s.Title, len(strings.TrimSpace(s.PosterURL)), strings.TrimSpace(s.PosterPath))
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"person":        person,
			"role":          role,
			"playlistTitle": playlistTitle,
			"director":      director,
			"coActor":       coActor,
			"minYear":       minYear,
			"maxYear":       maxYear,
			"minRating":     minRating,
			"total":         len(items),
			"missing":       missing,
			"items":         items,
			"cache":         cacheStats,
		},
	})
}

// handleDiscoveryStudio: GET /api/discovery/studio?company=A24&minYear=&maxYear=&minRating=0
func (s *Server) handleDiscoveryStudio(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	company := strings.TrimSpace(r.URL.Query().Get("company"))
	if company == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "company query is required"})
		return
	}
	minYear, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("minYear")))
	maxYear, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("maxYear")))
	minRating, _ := strconv.ParseFloat(strings.TrimSpace(r.URL.Query().Get("minRating")), 64)
	excludeNonTheatrical := discoveryQueryBool(r.URL.Query().Get("excludeNonTheatrical"))

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	plexMovies, err := s.cachedListMovies(ctx)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: "plex library unavailable: " + err.Error()})
		return
	}

	items, resolvedName, err := AnalyzeStudio(ctx, cfg, client, plexMovies, company, minYear, maxYear, minRating, nil, excludeNonTheatrical)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"company": resolvedName,
			"items":   items,
		},
	})
}

// handleDiscoveryTMDBGenres returns TMDB /genre/movie/list entries (id + name) for optional discovery filters.
func (s *Server) handleDiscoveryTMDBGenres(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	cfg := s.snapshot()
	if strings.TrimSpace(cfg.TMDBAPIKey) == "" {
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"genres": []any{}}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	cache := newDiskDiscoveryCache(defaultDiscoveryCacheDir())
	m, err := tmdbGenreMapWithCache(ctx, cfg.TMDBAPIKey, cache)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	type row struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	rows := make([]row, 0, len(m))
	for id, name := range m {
		rows = append(rows, row{ID: id, Name: name})
	}
	sort.Slice(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].Name) < strings.ToLower(rows[j].Name)
	})
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"genres": rows}})
}

// handleDiscoveryStart kicks off a filmography or studio analysis in the
// background and returns a jobId immediately. The caller polls
// /api/discovery/poll?jobId=<id> until State == "done" or "error".
// This avoids holding a long-lived HTTP connection open, which causes browsers
// to show "this tab is taking too long" dialogs.
func (s *Server) handleDiscoveryStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}

	var req struct {
		Mode                   string  `json:"mode"`
		Person                 string  `json:"person"`
		Role                   string  `json:"role"`
		PlaylistTitle          string  `json:"playlistTitle"`
		Director               string  `json:"director"`
		CoActor                string  `json:"coActor"`
		Company                string  `json:"company"`
		MinYear                int     `json:"minYear"`
		MaxYear                int     `json:"maxYear"`
		MinRating              float64 `json:"minRating"`
		GenreIDs               []int   `json:"genreIds"`
		ExcludeNonTheatrical   bool    `json:"excludeNonTheatrical"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON"})
		return
	}

	job := s.discJobs.create()
	cfg := s.snapshot()
	client := NewPlexClient(cfg)

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		defer cancel()

		s.discJobs.setRunning(job, "Loading library…")
		plexMovies, err := s.cachedListMovies(ctx)
		if err != nil {
			s.discJobs.setFailed(job, "Plex library unavailable: "+err.Error())
			return
		}

		switch req.Mode {
		case "studio":
			s.discJobs.setRunning(job, "Searching TMDB for "+req.Company+"…")
			items, resolvedName, err := AnalyzeStudio(ctx, cfg, client, plexMovies, req.Company, req.MinYear, req.MaxYear, req.MinRating, req.GenreIDs, req.ExcludeNonTheatrical)
			if err != nil {
				s.discJobs.setFailed(job, err.Error())
				return
			}
			missing := 0
			for _, it := range items {
				if !it.InLibrary {
					missing++
				}
			}
			s.discJobs.setDone(job, map[string]any{
				"company": resolvedName,
				"total":   len(items),
				"missing": missing,
				"items":   items,
			})
		case "browse":
			s.discJobs.setRunning(job, "Fetching TMDB discover (by year)…")
			items, browseLabel, err := AnalyzeBrowse(ctx, cfg, client, plexMovies, req.MinYear, req.MaxYear, req.MinRating, req.GenreIDs, req.ExcludeNonTheatrical)
			if err != nil {
				s.discJobs.setFailed(job, err.Error())
				return
			}
			missing := 0
			for _, it := range items {
				if !it.InLibrary {
					missing++
				}
			}
			s.discJobs.setDone(job, map[string]any{
				"browseLabel": browseLabel,
				"total":       len(items),
				"missing":     missing,
				"items":       items,
			})
		default:
			s.discJobs.setRunning(job, "Fetching TMDB filmography for "+req.Person+"…")
			var cacheStats DiscoveryCacheStats
			items, err := AnalyzeFilmography(ctx, cfg, client, plexMovies, req.Person, req.Role,
				req.PlaylistTitle, req.Director, req.CoActor, req.MinYear, req.MaxYear, req.MinRating, req.GenreIDs, req.ExcludeNonTheatrical, &cacheStats)
			if err != nil {
				s.discJobs.setFailed(job, err.Error())
				return
			}
			missing := 0
			for _, it := range items {
				if !it.InLibrary {
					missing++
				}
			}
			s.discJobs.setDone(job, map[string]any{
				"person":        req.Person,
				"role":          req.Role,
				"playlistTitle": req.PlaylistTitle,
				"total":         len(items),
				"missing":       missing,
				"items":         items,
				"cache":         cacheStats,
			})
		}
	}()

	respondJSON(w, http.StatusAccepted, apiResponse{
		Success: true,
		Data:    map[string]any{"jobId": job.ID},
	})
}

// handleDiscoveryPoll returns the current state of a background discovery job.
func (s *Server) handleDiscoveryPoll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	jobID := r.URL.Query().Get("jobId")
	if jobID == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "jobId required"})
		return
	}
	job, ok := s.discJobs.get(jobID)
	if !ok {
		respondJSON(w, http.StatusNotFound, apiResponse{Success: false, Error: "job not found"})
		return
	}

	s.discJobs.mu.Lock()
	state := job.State
	message := job.Message
	result := job.Result
	errMsg := job.ErrMsg
	s.discJobs.mu.Unlock()

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"state":   state,
			"message": message,
			"result":  result,
			"error":   errMsg,
		},
	})
}

// handleDiscoveryCacheInvalidate removes on-disk TMDB discovery cache files so the
// next analysis refetches from TMDB. Does not clear the in-memory Plex movie list.
func (s *Server) handleDiscoveryCacheInvalidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	dir := defaultDiscoveryCacheDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"removed": 0}})
			return
		}
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
		return
	}
	removed := 0
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
			return
		}
		removed++
	}
	omdbN, omdbErr := RemoveAllOMDbCache()
	if omdbErr != nil {
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: omdbErr.Error()})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"removed": removed, "omdbRemoved": omdbN}})
}

func (s *Server) handleDiscoveryPersonSuggest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	cfg := s.snapshot()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	suggestions, err := SuggestPeople(ctx, cfg, query)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	movies, _ := s.cachedListMovies(ctx)
	role := DiscoverCollaborators(movies, query).SuggestedRole
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"suggestions": suggestions, "suggestedRole": role}})
}

func (s *Server) handleDiscoveryCollaborators(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	person := strings.TrimSpace(r.URL.Query().Get("person"))
	if person == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "person query is required"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()
	movies, err := s.cachedListMovies(ctx)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	collabs := DiscoverCollaborators(movies, person)
	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"suggestedRole": collabs.SuggestedRole,
			"directors":     collabs.Directors,
			"actors":        collabs.Actors,
		},
	})
}

func (s *Server) handleDiscoveryAddToRadarr(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}

	var req struct {
		Items []RadarrAddItem `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON body"})
		return
	}
	if len(req.Items) == 0 {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "no selected items provided"})
		return
	}

	cfg := s.snapshot()
	if !cfg.RadarrEnabled {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "radarr integration is disabled in Settings"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	result, err := AddMoviesToRadarr(ctx, cfg, req.Items)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: result})
}

func (s *Server) handleCreateRandomPlaylist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	var req struct {
		Title string `json:"title"`
		Count int    `json:"count"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{
			Success: false,
			Error:   "invalid JSON body",
		})
		return
	}
	if req.Title == "" {
		req.Title = "Tonight Picks"
	}
	if req.Count <= 0 {
		req.Count = 12
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	fmt.Printf("[API] /api/playlists/random title=%q count=%d\n", req.Title, req.Count)
	result, err := client.CreateRandomPlaylist(ctx, cfg.LibraryKey, req.Title, req.Count)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data:    result,
	})
}

func (s *Server) handleListPlaylists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	playlists, err := client.ListPlaylists(ctx)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"playlists": playlists}})
}

func (s *Server) handlePlaylistItems(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	if title == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "title query is required"})
		return
	}
	limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	if limit <= 0 {
		limit = 300
	}

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	movies, err := client.PlaylistMovies(ctx, title, limit)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{"title": title, "count": len(movies), "movies": movies}})
}

func (s *Server) handlePlayPlaylist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	var req struct {
		Title      string `json:"title"`
		ClientName string `json:"clientName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON body"})
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "playlist title is required"})
		return
	}
	if strings.TrimSpace(req.ClientName) == "" {
		req.ClientName = s.snapshot().TargetClientName
	}

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	result, err := client.PlayPlaylistOnClient(ctx, req.Title, req.ClientName)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}
	if len(result.SentTitles) > 0 {
		s.recordLocalPlayback(result.TargetClient, result.SentTitles, "playlist_play", result.PlaylistTitle, false)
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: result})
}

func (s *Server) handlePreviewPlaylist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}

	var req struct {
		Title     string  `json:"title"`
		Genre     string  `json:"genre"`
		MinRating float64 `json:"minRating"`
		Actor     string  `json:"actor"`
		Director  string  `json:"director"`
		MinYear   int     `json:"minYear"`
		MaxYear   int     `json:"maxYear"`
		Limit     int     `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "invalid JSON body"})
		return
	}
	if req.MinYear == 0 {
		req.MinYear = minAllowedYear
	}
	if req.MaxYear == 0 {
		req.MaxYear = maxAllowedYear
	}
	if req.Limit <= 0 {
		req.Limit = 100
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	movies, err := s.cachedListMovies(ctx)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: err.Error()})
		return
	}

	if strings.TrimSpace(req.Actor) != "" || strings.TrimSpace(req.Director) != "" {
		movies = filterMoviesByPeople(movies, req.Actor, req.Director)
	}
	movies = filterMoviesByGenreRatingYear(movies, req.Genre, req.MinRating, req.MinYear, req.MaxYear)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	movies = ReorderMoviesByViewCountShuffleTies(movies, rng)

	total := len(movies)
	if req.Limit > len(movies) {
		req.Limit = len(movies)
	}
	preview := movies[:req.Limit]

	title := strings.TrimSpace(req.Title)
	if title == "" {
		if strings.TrimSpace(req.Genre) == "" {
			title = "ALL-rando"
		} else {
			title = strings.ToUpper(strings.TrimSpace(req.Genre)) + "-rando"
		}
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"suggestedTitle": title,
			"totalMatched":   total,
			"showing":        len(preview),
			"filters": map[string]any{
				"genre":     req.Genre,
				"minRating": req.MinRating,
				"actor":     req.Actor,
				"director":  req.Director,
				"minYear":   req.MinYear,
				"maxYear":   req.MaxYear,
			},
			"movies": preview,
		},
	})
}

func (s *Server) handlePlayers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	fmt.Printf("[API] /api/players targetClient=%q\n", cfg.TargetClientName)
	lp := client.ListPlayers(ctx)

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"targetClient": cfg.TargetClientName,
			"players":      lp.Players,
		},
	})
}

// decodeLgVolumePostJSON reads {"level":0-100} or {"volume":...} (UseNumber for JS floats).
func decodeLgVolumePostJSON(r *http.Request) (int, error) {
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return 0, fmt.Errorf("invalid JSON body")
	}
	var raw any
	var ok bool
	if raw, ok = m["level"]; ok {
		return jsonAnyToVolumePercent(raw)
	}
	if raw, ok = m["volume"]; ok {
		return jsonAnyToVolumePercent(raw)
	}
	return 0, fmt.Errorf(`body needs "level" or "volume" (0-100)`)
}

func jsonAnyToVolumePercent(v any) (int, error) {
	var n int
	switch x := v.(type) {
	case json.Number:
		i64, err := x.Int64()
		if err != nil {
			f, ferr := x.Float64()
			if ferr != nil {
				return 0, fmt.Errorf("level must be a number")
			}
			n = int(math.Round(f))
		} else {
			n = int(i64)
		}
	case float64:
		n = int(math.Round(x))
	case int:
		n = x
	case int64:
		n = int(x)
	default:
		return 0, fmt.Errorf("level must be a number")
	}
	if n < 0 {
		n = 0
	}
	if n > 100 {
		n = 100
	}
	return n, nil
}

func (s *Server) handleLgVolume(w http.ResponseWriter, r *http.Request) {
	cfg := s.snapshot()
	addr := strings.TrimSpace(cfg.LGTVAddr)
	key := strings.TrimSpace(cfg.LGTVClientKey)

	switch r.Method {
	case http.MethodGet:
		if addr == "" || key == "" {
			respondJSON(w, http.StatusOK, apiResponse{
				Success: true,
				Data: map[string]any{
					"supported": false,
				},
			})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		st, err := GetVolumeSSAP(ctx, addr, key)
		if err != nil {
			respondJSON(w, http.StatusOK, apiResponse{
				Success: true,
				Data: map[string]any{
					"supported": true,
					"error":     err.Error(),
				},
			})
			return
		}
		respondJSON(w, http.StatusOK, apiResponse{
			Success: true,
			Data: map[string]any{
				"supported": true,
				"volume":    st.Volume,
				"mute":      st.Mute,
			},
		})
	case http.MethodPost:
		level, err := decodeLgVolumePostJSON(r)
		if err != nil {
			respondJSON(w, http.StatusBadRequest, apiResponse{
				Success: false,
				Error:   err.Error(),
			})
			return
		}
		if addr == "" || key == "" {
			respondJSON(w, http.StatusOK, apiResponse{
				Success: true,
				Data: map[string]any{
					"supported": false,
				},
			})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		st, err := SetVolumeSSAP(ctx, addr, key, level)
		if err != nil {
			respondJSON(w, http.StatusOK, apiResponse{
				Success: true,
				Data: map[string]any{
					"supported": true,
					"error":     err.Error(),
				},
			})
			return
		}
		respondJSON(w, http.StatusOK, apiResponse{
			Success: true,
			Data: map[string]any{
				"supported": true,
				"volume":    st.Volume,
				"mute":      st.Mute,
			},
		})
	default:
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
	}
}

func (s *Server) handleCreatePlaylistByPeople(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	var req struct {
		Title    string `json:"title"`
		Count    int    `json:"count"`
		Actor    string `json:"actor"`
		Director string `json:"director"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{
			Success: false,
			Error:   "invalid JSON body",
		})
		return
	}
	if req.Title == "" {
		req.Title = "People Picks"
	}
	if req.Count <= 0 {
		req.Count = 12
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	fmt.Printf("[API] /api/playlists/by-people title=%q count=%d actor=%q director=%q\n", req.Title, req.Count, req.Actor, req.Director)
	result, err := client.CreatePlaylistByPeople(ctx, cfg.LibraryKey, req.Title, req.Actor, req.Director, req.Count)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data:    result,
	})
}

func (s *Server) handleCreateAndPlayRandomPlaylist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	var req struct {
		Title      string `json:"title"`
		Count      int    `json:"count"`
		ClientName string `json:"clientName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{
			Success: false,
			Error:   "invalid JSON body",
		})
		return
	}
	if req.Title == "" {
		req.Title = "Tonight Picks"
	}
	if req.Count <= 0 {
		req.Count = 12
	}
	if req.ClientName == "" {
		req.ClientName = s.snapshot().TargetClientName
	}

	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	fmt.Printf("[API] /api/playlists/random-play title=%q count=%d client=%q\n", req.Title, req.Count, req.ClientName)
	result, err := client.CreateRandomPlaylistAndPlay(ctx, cfg.LibraryKey, req.Title, req.Count, req.ClientName)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data:    result,
	})
}

func (s *Server) handleCreatePlaylistByGenreRating(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{
			Success: false,
			Error:   "method not allowed",
		})
		return
	}

	var req struct {
		Genre     string  `json:"genre"`
		MinRating float64 `json:"minRating"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, apiResponse{
			Success: false,
			Error:   "invalid JSON body",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	cfg := s.snapshot()
	client := NewPlexClient(cfg)
	fmt.Printf("[API] /api/playlists/by-genre-rating genre=%q minRating=%.1f years=%d-%d\n", req.Genre, req.MinRating, minAllowedYear, maxAllowedYear)
	result, err := client.CreatePlaylistByGenreRatingYear(ctx, cfg.LibraryKey, req.Genre, req.MinRating, minAllowedYear, maxAllowedYear)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{
			Success: false,
			Error:   err.Error(),
		})
		return
	}

	respondJSON(w, http.StatusOK, apiResponse{
		Success: true,
		Data: map[string]any{
			"playlist": result,
			"rule": map[string]any{
				"minYear":   minAllowedYear,
				"maxYear":   maxAllowedYear,
				"minRating": req.MinRating,
				"genre":     req.Genre,
			},
		},
	})
}

type apiResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

func respondJSON(w http.ResponseWriter, status int, payload apiResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func requestLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("[HTTP] %s %s\n", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
