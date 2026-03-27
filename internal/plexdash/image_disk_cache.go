package plexdash

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// imageCacheDir is the base directory for server-side image disk caches.
const plexThumbCacheDir = "data/plex-thumb-cache"
const tmdbPosterCacheDir = "data/tmdb-poster-cache"

// immutableCacheControl is set on all cached image responses. 1-year max-age
// plus immutable tells browsers not to revalidate even on force-refresh.
const immutableCacheControl = "public, max-age=31536000, immutable"

// serveCachedPosterFile streams an existing cache file to w. Returns false if
// the file is missing or unreadable.
func serveCachedPosterFile(w http.ResponseWriter, path, contentType string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	ct := contentType
	if ct == "" {
		ct = "image/jpeg"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", immutableCacheControl)
	io.Copy(w, f) //nolint:errcheck
	return true
}

// serveOrCachePoster checks the disk cache at cachePath. If the file exists it
// is streamed directly to the client. If not, it fetches from upstreamURL,
// writes the bytes to cachePath atomically (tmp → rename), and streams the
// response. contentType is used when serving from disk.
//
// Returns true if the response was written (caller must not write again).
func serveOrCachePoster(w http.ResponseWriter, upstreamURL, cachePath, contentType string) bool {
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return false
	}

	// ── Serve from disk cache ─────────────────────────────────────────────────
	if f, err := os.Open(cachePath); err == nil {
		defer f.Close()
		ct := contentType
		if ct == "" {
			ct = "image/jpeg"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", immutableCacheControl)
		io.Copy(w, f) //nolint:errcheck
		return true
	}

	// ── Fetch from upstream ───────────────────────────────────────────────────
	resp, err := http.Get(upstreamURL) //nolint:noctx
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp != nil {
			resp.Body.Close()
		}
		return false
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = contentType
	}
	if ct == "" {
		ct = "image/jpeg"
	}

	// Write to a temp file first, then rename for atomicity.
	tmp, err := os.CreateTemp(filepath.Dir(cachePath), ".img-*")
	if err != nil {
		// Can't cache — stream directly to client without caching.
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "public, max-age=86400")
		io.Copy(w, resp.Body) //nolint:errcheck
		return true
	}

	// Tee: write simultaneously to temp file and HTTP response.
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", immutableCacheControl)
	if _, err := io.Copy(io.MultiWriter(w, tmp), resp.Body); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return true // response was partially written; return true to avoid double-write
	}
	tmp.Close()
	os.Rename(tmp.Name(), cachePath) //nolint:errcheck
	return true
}
