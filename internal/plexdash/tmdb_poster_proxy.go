package plexdash

import (
	"io"
	"net/http"
	"strings"
	"time"
)

// handleDiscoveryPoster proxies TMDB poster images through this app so the browser loads
// same-origin URLs (avoids hotlink / Referer issues with image.tmdb.org).
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
	upstream := "https://image.tmdb.org/t/p/" + size + path
	ctx := r.Context()
	upMethod := http.MethodGet
	if r.Method == http.MethodHead {
		upMethod = http.MethodHead
	}
	req, err := http.NewRequestWithContext(ctx, upMethod, upstream, nil)
	if err != nil {
		DiscoveryDebugf("[poster] FAIL new_request err=%v", err)
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		DiscoveryDebugf("[poster] FAIL upstream_dial err=%v url=%s", err, upstream)
		http.Error(w, "upstream fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	DiscoveryDebugf("[poster] upstream status=%d ct=%q url=%s", resp.StatusCode, resp.Header.Get("Content-Type"), upstream)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		DiscoveryDebugf("[poster] FAIL bad_status status=%d", resp.StatusCode)
		http.Error(w, "poster not found", http.StatusNotFound)
		return
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	if r.Method == http.MethodHead {
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			w.Header().Set("Content-Length", cl)
		}
		DiscoveryDebugf("[poster] OK HEAD only path=%q", path)
		return
	}
	n, copyErr := io.Copy(w, resp.Body)
	if copyErr != nil {
		DiscoveryDebugf("[poster] WARN copy_err=%v bytes=%d path=%q", copyErr, n, path)
		return
	}
	DiscoveryDebugf("[poster] OK GET bytes=%d path=%q", n, path)
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
