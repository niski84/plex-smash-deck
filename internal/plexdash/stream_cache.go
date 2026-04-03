package plexdash

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const streamCacheDir = "data/stream-cache"

// streamDownload tracks a single background download from Plex to local disk.
type streamDownload struct {
	mu        sync.Mutex
	ratingKey string
	container string // mp4, mkv, etc.
	totalSize int64  // from Plex Content-Length; -1 if unknown
	cached    int64  // bytes written to disk so far
	complete  bool
	err       error
	cancel    context.CancelFunc
	done      chan struct{} // closed when download finishes (success or error)
}

// progress returns the current cached bytes and total size safely.
func (dl *streamDownload) progress() (cached, total int64, complete bool, errStr string) {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	if dl.err != nil {
		errStr = dl.err.Error()
	}
	return dl.cached, dl.totalSize, dl.complete, errStr
}

// cachePath returns the final file path for a completed download.
func streamCachePath(ratingKey, container string) string {
	return filepath.Join(streamCacheDir, ratingKey+"."+container)
}

// tmpPath returns the temp file path used during download.
func streamTmpPath(ratingKey, container string) string {
	return filepath.Join(streamCacheDir, "."+ratingKey+"."+container+".tmp")
}

// ── Server helpers ───────────────────────────────────────────────────────────

// findMovieByRatingKey looks up a movie from the cached library.
func (s *Server) findMovieByRatingKey(ratingKey string) (Movie, bool) {
	s.mlMu.RLock()
	defer s.mlMu.RUnlock()
	for _, m := range s.mlMovies {
		if m.RatingKey == ratingKey {
			return m, true
		}
	}
	return Movie{}, false
}

// containerForRatingKey returns the file container and whether the movie exists.
func (s *Server) containerForRatingKey(ratingKey string) (string, bool) {
	m, ok := s.findMovieByRatingKey(ratingKey)
	if !ok {
		return "", false
	}
	c := strings.ToLower(strings.TrimSpace(m.FileContainer))
	if c == "" {
		c = "mp4"
	}
	return c, true
}

// startStreamDownload ensures a download is running for the given ratingKey.
// If the file is already fully cached on disk it returns a pre-filled download.
// If a download is already in progress it returns the existing one.
// Otherwise it kicks off a new background download goroutine.
func (s *Server) startStreamDownload(ratingKey string) (*streamDownload, error) {
	m, ok := s.findMovieByRatingKey(ratingKey)
	if !ok {
		return nil, fmt.Errorf("movie %q not in library cache", ratingKey)
	}

	container := strings.ToLower(strings.TrimSpace(m.FileContainer))
	if container == "" {
		container = "mp4"
	}
	final := streamCachePath(ratingKey, container)

	// Already fully cached?
	if info, err := os.Stat(final); err == nil {
		return &streamDownload{
			ratingKey: ratingKey,
			container: container,
			totalSize: info.Size(),
			cached:    info.Size(),
			complete:  true,
			done:      closedChan(),
		}, nil
	}

	s.streamCacheMu.Lock()
	defer s.streamCacheMu.Unlock()

	// Already downloading?
	if dl, ok := s.streamDownloads[ratingKey]; ok {
		return dl, nil
	}

	// Read config under lock.
	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	plexURL := plexPartMediaURL(cfg.PlexBaseURL, cfg.PlexToken, m.PartKey)

	ctx, cancel := context.WithCancel(context.Background())
	dl := &streamDownload{
		ratingKey: ratingKey,
		container: container,
		totalSize: m.PartSize, // best guess from library metadata
		cancel:    cancel,
		done:      make(chan struct{}),
	}
	s.streamDownloads[ratingKey] = dl

	go s.runStreamDownload(ctx, dl, plexURL)
	return dl, nil
}

// runStreamDownload performs the actual HTTP fetch and writes to disk.
func (s *Server) runStreamDownload(ctx context.Context, dl *streamDownload, plexURL string) {
	defer func() {
		close(dl.done)
		s.streamCacheMu.Lock()
		// Only remove if this is still the registered download.
		if cur, ok := s.streamDownloads[dl.ratingKey]; ok && cur == dl {
			delete(s.streamDownloads, dl.ratingKey)
		}
		s.streamCacheMu.Unlock()
	}()

	if err := os.MkdirAll(streamCacheDir, 0o755); err != nil {
		dl.mu.Lock()
		dl.err = fmt.Errorf("mkdir: %w", err)
		dl.mu.Unlock()
		return
	}

	tmpFile := streamTmpPath(dl.ratingKey, dl.container)
	finalFile := streamCachePath(dl.ratingKey, dl.container)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, plexURL, nil)
	if err != nil {
		dl.mu.Lock()
		dl.err = err
		dl.mu.Unlock()
		return
	}

	client := &http.Client{Timeout: 0} // no timeout — large files
	resp, err := client.Do(req)
	if err != nil {
		dl.mu.Lock()
		dl.err = fmt.Errorf("plex fetch: %w", err)
		dl.mu.Unlock()
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		dl.mu.Lock()
		dl.err = fmt.Errorf("plex returned HTTP %d", resp.StatusCode)
		dl.mu.Unlock()
		return
	}

	// Update total size from Content-Length if available.
	if cl := resp.ContentLength; cl > 0 {
		dl.mu.Lock()
		dl.totalSize = cl
		dl.mu.Unlock()
	}

	f, err := os.Create(tmpFile)
	if err != nil {
		dl.mu.Lock()
		dl.err = fmt.Errorf("create tmp: %w", err)
		dl.mu.Unlock()
		return
	}

	buf := make([]byte, 256*1024) // 256 KB chunks
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := f.Write(buf[:n]); wErr != nil {
				f.Close()
				os.Remove(tmpFile)
				dl.mu.Lock()
				dl.err = fmt.Errorf("write: %w", wErr)
				dl.mu.Unlock()
				return
			}
			dl.mu.Lock()
			dl.cached += int64(n)
			dl.mu.Unlock()
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			f.Close()
			os.Remove(tmpFile)
			dl.mu.Lock()
			dl.err = fmt.Errorf("read: %w", readErr)
			dl.mu.Unlock()
			return
		}
	}

	f.Close()

	// Atomic rename.
	if err := os.Rename(tmpFile, finalFile); err != nil {
		dl.mu.Lock()
		dl.err = fmt.Errorf("rename: %w", err)
		dl.mu.Unlock()
		return
	}

	// Update final cached size from disk to be precise.
	if info, err := os.Stat(finalFile); err == nil {
		dl.mu.Lock()
		dl.cached = info.Size()
		dl.totalSize = info.Size()
		dl.mu.Unlock()
	}

	dl.mu.Lock()
	dl.complete = true
	dl.mu.Unlock()
}

// closedChan returns a channel that is already closed.
func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// ── streamCacheReader ────────────────────────────────────────────────────────
// Wraps an os.File and a streamDownload. When Read reaches the download
// frontier it blocks until more bytes are written (or the download finishes).

type streamCacheReader struct {
	f      *os.File
	dl     *streamDownload
	offset int64 // current read position
	limit  int64 // max byte to read (exclusive); -1 for unlimited
}

func (r *streamCacheReader) Read(p []byte) (int, error) {
	if r.limit >= 0 && r.offset >= r.limit {
		return 0, io.EOF
	}
	// Clamp read size to limit.
	toRead := p
	if r.limit >= 0 {
		remaining := r.limit - r.offset
		if int64(len(toRead)) > remaining {
			toRead = toRead[:remaining]
		}
	}

	for {
		n, err := r.f.Read(toRead)
		if n > 0 {
			r.offset += int64(n)
			// Suppress file EOF if the download is still going — the file
			// will grow and we will read more on the next call.
			if err == io.EOF {
				_, _, complete, _ := r.dl.progress()
				if !complete {
					err = nil
				}
			}
			return n, err
		}
		if err != io.EOF {
			return 0, err
		}
		// We got 0 bytes + EOF from the file. Check if the download is still going.
		cached, _, complete, dlErr := r.dl.progress()
		if dlErr != "" {
			return 0, fmt.Errorf("download error: %s", dlErr)
		}
		if complete {
			return 0, io.EOF
		}
		// Download is still going — wait for more bytes to land on disk.
		if r.offset >= cached {
			time.Sleep(150 * time.Millisecond)
			continue
		}
		// Bytes available on disk but file read returned 0 — retry read.
		time.Sleep(50 * time.Millisecond)
	}
}

func (r *streamCacheReader) Close() error {
	return r.f.Close()
}

// ── HTTP Handlers ────────────────────────────────────────────────────────────

// handleStream serves a Plex media file, proxied through the local cache.
// GET /api/stream/{ratingKey}
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}

	ratingKey := strings.TrimPrefix(r.URL.Path, "/api/stream/")
	if ratingKey == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "missing ratingKey"})
		return
	}

	container, ok := s.containerForRatingKey(ratingKey)
	if !ok {
		respondJSON(w, http.StatusNotFound, apiResponse{Success: false, Error: "movie not in library"})
		return
	}

	final := streamCachePath(ratingKey, container)

	// Fast path: fully cached → http.ServeFile handles Range, Content-Length, etc.
	if _, err := os.Stat(final); err == nil {
		serveVideoFile(w, r, final, container)
		return
	}

	// Start (or join) the download.
	dl, err := s.startStreamDownload(ratingKey)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
		return
	}

	// If download just completed between our stat and here, serve the final file.
	if dl.complete {
		serveVideoFile(w, r, final, container)
		return
	}

	// Serve from the in-progress temp file with a blocking reader.
	s.serveInProgressStream(w, r, dl, container)
}

// serveVideoFile sends a fully cached file with proper video content type.
func serveVideoFile(w http.ResponseWriter, r *http.Request, path, container string) {
	w.Header().Set("Content-Type", videoMIME(container))
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeFile(w, r, path)
}

// serveInProgressStream serves bytes from a file that is still being downloaded.
// If the requested Range is beyond the cached frontier, it proxies directly from Plex
// (needed for MP4 moov atom at end-of-file).
func (s *Server) serveInProgressStream(w http.ResponseWriter, r *http.Request, dl *streamDownload, container string) {
	cached, totalSize, _, _ := dl.progress()

	// Parse Range header.
	rangeStart, rangeEnd, hasRange := parseRangeHeader(r.Header.Get("Range"), totalSize)

	// If this is a Range request beyond what we've cached, proxy directly from Plex.
	// This is critical: browsers seek to the end of MP4 files to read the moov atom.
	if hasRange && rangeStart > cached {
		s.proxyRangeFromPlex(w, r, dl.ratingKey, container, rangeStart, rangeEnd, totalSize)
		return
	}

	tmpFile := streamTmpPath(dl.ratingKey, dl.container)

	// Wait briefly for at least some data to be available.
	waitCtx, waitCancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer waitCancel()
	for cached == 0 {
		_, _, _, dlErr := dl.progress()
		if dlErr != "" {
			respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: "download failed: " + dlErr})
			return
		}
		select {
		case <-waitCtx.Done():
			respondJSON(w, http.StatusGatewayTimeout, apiResponse{Success: false, Error: "timeout waiting for data"})
			return
		case <-dl.done:
			cached, _, _, dlErr = dl.progress()
			if dlErr != "" {
				respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: "download failed: " + dlErr})
				return
			}
			if cached == 0 {
				respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: "download produced no data"})
				return
			}
		case <-time.After(200 * time.Millisecond):
			cached, _, _, _ = dl.progress()
			continue
		}
		break
	}

	f, err := os.Open(tmpFile)
	if err != nil {
		// Maybe the download finished and renamed — try the final file.
		final := streamCachePath(dl.ratingKey, container)
		if _, statErr := os.Stat(final); statErr == nil {
			serveVideoFile(w, r, final, container)
			return
		}
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: "cannot open cache file"})
		return
	}

	ct := videoMIME(container)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("X-Stream-Complete", "false")

	reader := &streamCacheReader{f: f, dl: dl, limit: -1}

	if hasRange {
		if _, err := f.Seek(rangeStart, io.SeekStart); err != nil {
			f.Close()
			respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: "seek failed"})
			return
		}
		reader.offset = rangeStart
		if rangeEnd >= 0 {
			reader.limit = rangeEnd + 1
		}

		endStr := "*"
		if rangeEnd >= 0 {
			endStr = strconv.FormatInt(rangeEnd, 10)
		}
		totalStr := "*"
		if totalSize > 0 {
			totalStr = strconv.FormatInt(totalSize, 10)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%s/%s", rangeStart, endStr, totalStr))
		if rangeEnd >= 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(rangeEnd-rangeStart+1, 10))
		}
		w.WriteHeader(http.StatusPartialContent)
	} else {
		if totalSize > 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(totalSize, 10))
		}
		w.WriteHeader(http.StatusOK)
	}

	io.Copy(w, reader) //nolint:errcheck
	f.Close()
}

// proxyRangeFromPlex handles Range requests for byte regions not yet cached
// by proxying directly to Plex. This is essential for MP4 files where the
// browser seeks to the end to read the moov atom before playback can start.
func (s *Server) proxyRangeFromPlex(w http.ResponseWriter, r *http.Request, ratingKey, container string, rangeStart, rangeEnd, totalSize int64) {
	m, ok := s.findMovieByRatingKey(ratingKey)
	if !ok {
		respondJSON(w, http.StatusNotFound, apiResponse{Success: false, Error: "movie not found"})
		return
	}

	s.mu.RLock()
	cfg := s.cfg
	s.mu.RUnlock()

	plexURL := plexPartMediaURL(cfg.PlexBaseURL, cfg.PlexToken, m.PartKey)

	// Build the Range header for Plex.
	var rangeHdr string
	if rangeEnd >= 0 {
		rangeHdr = fmt.Sprintf("bytes=%d-%d", rangeStart, rangeEnd)
	} else {
		rangeHdr = fmt.Sprintf("bytes=%d-", rangeStart)
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, plexURL, nil)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: "request build failed"})
		return
	}
	req.Header.Set("Range", rangeHdr)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		respondJSON(w, http.StatusBadGateway, apiResponse{Success: false, Error: "plex fetch failed"})
		return
	}
	defer resp.Body.Close()

	ct := videoMIME(container)
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Accept-Ranges", "bytes")

	// Forward Plex's Content-Range if present, otherwise build our own.
	if cr := resp.Header.Get("Content-Range"); cr != "" {
		w.Header().Set("Content-Range", cr)
	} else if totalSize > 0 {
		end := rangeEnd
		if end < 0 {
			end = totalSize - 1
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", rangeStart, end, totalSize))
	}
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		w.Header().Set("Content-Length", cl)
	}

	if resp.StatusCode == http.StatusPartialContent || resp.StatusCode == http.StatusOK {
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(resp.StatusCode)
	}

	io.Copy(w, resp.Body) //nolint:errcheck
}

// parseRangeHeader handles a simple "bytes=start-end" or "bytes=start-" range.
func parseRangeHeader(rangeHdr string, totalSize int64) (start, end int64, ok bool) {
	if rangeHdr == "" || !strings.HasPrefix(rangeHdr, "bytes=") {
		return 0, -1, false
	}
	spec := strings.TrimPrefix(rangeHdr, "bytes=")
	// Only handle the first range.
	if idx := strings.Index(spec, ","); idx >= 0 {
		spec = spec[:idx]
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, -1, false
	}
	start, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, -1, false
	}
	endPart := strings.TrimSpace(parts[1])
	if endPart == "" {
		// "bytes=start-" → open-ended
		if totalSize > 0 {
			return start, totalSize - 1, true
		}
		return start, -1, true
	}
	end, err = strconv.ParseInt(endPart, 10, 64)
	if err != nil {
		return 0, -1, false
	}
	return start, end, true
}

// videoMIME returns the MIME type for a given container.
func videoMIME(container string) string {
	switch strings.ToLower(container) {
	case "mp4", "m4v":
		return "video/mp4"
	case "mkv":
		return "video/x-matroska"
	case "avi":
		return "video/x-msvideo"
	case "webm":
		return "video/webm"
	default:
		return "video/mp4"
	}
}

// ── Preload handler ──────────────────────────────────────────────────────────

// handleStreamPreload starts a background download without serving the stream.
// POST /api/stream/preload  body: {"ratingKey": "..."}
func (s *Server) handleStreamPreload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	var req struct {
		RatingKey string `json:"ratingKey"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RatingKey == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "ratingKey required"})
		return
	}
	dl, err := s.startStreamDownload(req.RatingKey)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
		return
	}
	cached, total, complete, _ := dl.progress()
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"ratingKey":   req.RatingKey,
		"totalSize":   total,
		"cachedBytes": cached,
		"complete":    complete,
	}})
}

// ── Status handler ───────────────────────────────────────────────────────────

// handleStreamStatus returns download progress for a given ratingKey.
// GET /api/stream/status/{ratingKey}
func (s *Server) handleStreamStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	ratingKey := strings.TrimPrefix(r.URL.Path, "/api/stream/status/")
	if ratingKey == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "missing ratingKey"})
		return
	}

	container, _ := s.containerForRatingKey(ratingKey)

	// Check active download first.
	s.streamCacheMu.RLock()
	dl, active := s.streamDownloads[ratingKey]
	s.streamCacheMu.RUnlock()

	if active {
		cached, total, complete, dlErr := dl.progress()
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
			"ratingKey":   ratingKey,
			"totalSize":   total,
			"cachedBytes": cached,
			"complete":    complete,
			"error":       dlErr,
		}})
		return
	}

	// Check for completed file on disk.
	if container != "" {
		final := streamCachePath(ratingKey, container)
		if info, err := os.Stat(final); err == nil {
			respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
				"ratingKey":   ratingKey,
				"totalSize":   info.Size(),
				"cachedBytes": info.Size(),
				"complete":    true,
				"error":       "",
			}})
			return
		}
	}

	// Not cached, not downloading.
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"ratingKey":   ratingKey,
		"totalSize":   int64(0),
		"cachedBytes": int64(0),
		"complete":    false,
		"error":       "",
	}})
}

// ── Cache list handler ───────────────────────────────────────────────────────

// handleStreamCacheList returns all cached stream files.
// GET /api/stream/cache
func (s *Server) handleStreamCacheList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}

	entries, err := os.ReadDir(streamCacheDir)
	if err != nil {
		respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: []any{}})
		return
	}

	type cacheEntry struct {
		RatingKey string `json:"ratingKey"`
		FileName  string `json:"fileName"`
		Size      int64  `json:"size"`
		Complete  bool   `json:"complete"`
	}
	var items []cacheEntry
	var totalBytes int64

	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		rk := strings.TrimSuffix(name, ext)
		totalBytes += info.Size()
		items = append(items, cacheEntry{
			RatingKey: rk,
			FileName:  name,
			Size:      info.Size(),
			Complete:  true,
		})
	}

	// Include active downloads.
	s.streamCacheMu.RLock()
	for rk, dl := range s.streamDownloads {
		cached, _, complete, _ := dl.progress()
		if !complete {
			items = append(items, cacheEntry{
				RatingKey: rk,
				FileName:  rk + "." + dl.container,
				Size:      cached,
				Complete:  false,
			})
			totalBytes += cached
		}
	}
	s.streamCacheMu.RUnlock()

	if items == nil {
		items = []cacheEntry{}
	}

	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"items":      items,
		"totalBytes": totalBytes,
	}})
}

// ── Cache delete handler ─────────────────────────────────────────────────────

// handleStreamCacheDelete removes a cached stream file and cancels any active download.
// DELETE /api/stream/cache/{ratingKey}
func (s *Server) handleStreamCacheDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	ratingKey := strings.TrimPrefix(r.URL.Path, "/api/stream/cache/")
	if ratingKey == "" {
		respondJSON(w, http.StatusBadRequest, apiResponse{Success: false, Error: "missing ratingKey"})
		return
	}

	// Cancel active download if any.
	s.streamCacheMu.Lock()
	if dl, ok := s.streamDownloads[ratingKey]; ok {
		dl.cancel()
		delete(s.streamDownloads, ratingKey)
	}
	s.streamCacheMu.Unlock()

	// Remove files from disk (both final and temp, for any container).
	removed := false
	entries, _ := os.ReadDir(streamCacheDir)
	for _, e := range entries {
		name := e.Name()
		// Match "ratingKey.ext" or ".ratingKey.ext.tmp"
		if strings.HasPrefix(name, ratingKey+".") || strings.HasPrefix(name, "."+ratingKey+".") {
			os.Remove(filepath.Join(streamCacheDir, name))
			removed = true
		}
	}

	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"ratingKey": ratingKey,
		"removed":   removed,
	}})
}

