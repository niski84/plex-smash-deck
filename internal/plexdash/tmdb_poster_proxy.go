package plexdash

import (
	"net/http"
	"strings"
)

// handleDiscoveryPoster proxies TMDB poster images through this app so the
// browser loads same-origin URLs (avoids hotlink / Referer issues with
// image.tmdb.org). Images are cached permanently on disk; TMDB is only hit
// once per poster path+size combination.
func (s *Server) handleDiscoveryPoster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	DiscoveryDebugf("[poster] method=%s raw_path_param_len=%d", r.Method, len(raw))
	path := raw
	if path == "" {
		DiscoveryDebugf("[poster] FAIL empty path query")
		http.Error(w, "path query required", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if !validTMDBPosterFilePath(path) {
		DiscoveryDebugf("[poster] FAIL validation path=%q", truncateDiscoveryLog(path, 200))
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	size := strings.TrimSpace(r.URL.Query().Get("size"))
	if !validTMDBPosterSize(size) {
		size = "w185"
	}

	// HEAD requests: skip disk cache and proxy directly (needed for pre-flight).
	upstream := "https://image.tmdb.org/t/p/" + size + path
	if r.Method == http.MethodHead {
		resp, err := http.Head(upstream) //nolint:noctx
		if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if resp != nil {
				resp.Body.Close()
			}
			http.Error(w, "poster not found", http.StatusNotFound)
			return
		}
		resp.Body.Close()
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			w.Header().Set("Content-Length", cl)
		}
		w.Header().Set("Cache-Control", immutableCacheControl)
		DiscoveryDebugf("[poster] OK HEAD only path=%q", path)
		return
	}

	// Build a safe cache filename: strip leading "/" from the TMDB path and
	// prefix with the size so w185 and w780 of the same poster are distinct.
	safeFile := size + "-" + strings.TrimPrefix(path, "/")
	cachePath := tmdbPosterCacheDir + "/" + safeFile

	if !serveOrCachePoster(w, upstream, cachePath, "image/jpeg") {
		DiscoveryDebugf("[poster] FAIL serve_or_cache path=%q", path)
		http.Error(w, "poster not found", http.StatusNotFound)
	}
	DiscoveryDebugf("[poster] OK GET path=%q", path)
}

func truncateDiscoveryLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// validTMDBPosterSize allows only the TMDB image size tokens we actually use.
func validTMDBPosterSize(s string) bool {
	switch s {
	case "w92", "w154", "w185", "w342", "w500", "w780", "original":
		return true
	}
	return false
}

// validTMDBPosterFilePath keeps paths safe for appending to image.tmdb.org (no traversal).
// TMDB occasionally uses punctuation outside [a-z0-9._-]; strict ASCII rejected valid posters.
func validTMDBPosterFilePath(path string) bool {
	if len(path) < 2 || len(path) > 768 {
		return false
	}
	if path[0] != '/' {
		return false
	}
	if strings.Contains(path, "..") || strings.Contains(path, "//") {
		return false
	}
	return true
}
