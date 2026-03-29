package plexdash

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const fanartMoviePrefetchTimeout = 120 * time.Second

func validFanartCacheID(id string) bool {
	id = strings.TrimSpace(strings.ToLower(id))
	if len(id) != 32 {
		return false
	}
	for _, c := range id {
		if c >= '0' && c <= '9' || c >= 'a' && c <= 'f' {
			continue
		}
		return false
	}
	return true
}

func (s *Server) tmdbIDForRatingKey(ctx context.Context, rk string) int {
	rk = strings.TrimSpace(rk)
	if rk == "" {
		return 0
	}
	movies, err := s.cachedListMovies(ctx)
	if err != nil {
		return 0
	}
	for _, m := range movies {
		if m.RatingKey == rk {
			return m.TMDBID
		}
	}
	return 0
}

// handleFanartMovieCacheFile serves a cached fanart blob by manifest id (same disk store as hero banner).
func (s *Server) handleFanartMovieCacheFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if !validFanartCacheID(id) {
		http.NotFound(w, r)
		return
	}

	s.fanartTouchEntry(id)

	s.fanartManifestMu.Lock()
	m := readFanartManifest()
	var file string
	for _, e := range m.Entries {
		if e.ID == id {
			file = e.File
			break
		}
	}
	s.fanartManifestMu.Unlock()

	if file == "" {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(fanartBannerCacheDir, filepath.Base(file))
	b, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := http.DetectContentType(b)
	if ct == "" {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", immutableCacheControl)
	_, _ = w.Write(b)
}

// GET /api/fanart-movie/prefetch?tmdbId=123 or ?ratingKey=… — fetch fanart.tv manifest, download images into LRU cache, return same-origin URLs for the lightbox.
func (s *Server) handleFanartMoviePrefetch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	cfg := s.snapshot()
	q := r.URL.Query()
	tmdbStr := strings.TrimSpace(q.Get("tmdbId"))
	rk := strings.TrimSpace(q.Get("ratingKey"))

	ctx, cancel := context.WithTimeout(r.Context(), fanartMoviePrefetchTimeout)
	defer cancel()

	tmdbID, _ := strconv.Atoi(tmdbStr)
	if tmdbID <= 0 && rk != "" {
		tmdbID = s.tmdbIDForRatingKey(ctx, rk)
	}

	out := map[string]any{
		"tmdbId": tmdbID,
		"items":  []map[string]any{},
	}

	if !cfg.FanartEnabled || strings.TrimSpace(cfg.FanartAPIKey) == "" {
		out["reason"] = "fanart_disabled_or_missing_key"
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: out})
		return
	}
	if tmdbID <= 0 {
		out["reason"] = "no_tmdb_id"
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: out})
		return
	}

	payload, err := FetchFanartMoviePayload(ctx, tmdbID, cfg.FanartAPIKey, cfg.FanartClientKey)
	if err != nil {
		out["reason"] = "fanart_fetch_failed"
		out["error"] = err.Error()
		s.fanartLogAppend("Fanart gallery prefetch: API error tmdb=" + strconv.Itoa(tmdbID) + ": " + err.Error())
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: out})
		return
	}

	entries := EnumerateFanartGallery(payload, 48)
	s.fanartLogAppend("Fanart gallery prefetch: tmdb=" + strconv.Itoa(tmdbID) + " candidates=" + strconv.Itoa(len(entries)))

	seen := map[string]struct{}{}
	var items []map[string]any
	for _, e := range entries {
		u := strings.TrimSpace(e.URL)
		if u == "" {
			continue
		}
		if _, dup := seen[u]; dup {
			continue
		}
		seen[u] = struct{}{}
		cacheID, derr := s.fanartDownloadToCache(ctx, u, cfg.FanartBannerCacheMaxMB)
		if derr != nil || cacheID == "" {
			if derr != nil {
				s.fanartLogAppend("Fanart gallery prefetch skip: " + derr.Error())
			}
			continue
		}
		items = append(items, map[string]any{
			"kind":   e.Kind,
			"id":     cacheID,
			"width":  e.Width,
			"height": e.Height,
			"label":  e.Label,
			"url":    "/api/fanart-movie/cache-file?id=" + cacheID,
		})
	}
	out["items"] = items
	out["count"] = len(items)
	if len(items) == 0 {
		out["reason"] = "nothing_cached"
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: out})
}
