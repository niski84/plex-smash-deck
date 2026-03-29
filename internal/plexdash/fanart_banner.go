package plexdash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const fanartBannerCacheDir = "data/fanart-banner-cache"

const fanartLogMaxEntries = 200

// fanartLogAppend records a line for the Settings tab activity log (and stderr).
func (s *Server) fanartLogAppend(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if len(line) > 1800 {
		line = line[:1800] + "…"
	}
	s.fanartLogMu.Lock()
	s.fanartLog = append(s.fanartLog, FanartLogEntry{
		Time: time.Now().UTC().Format(time.RFC3339),
		Line: line,
	})
	if len(s.fanartLog) > fanartLogMaxEntries {
		s.fanartLog = s.fanartLog[len(s.fanartLog)-fanartLogMaxEntries:]
	}
	s.fanartLogMu.Unlock()
	fmt.Printf("[fanart-log] %s\n", line)
}

var allowedBannerSchedules = map[string]struct{}{
	"5m": {}, "10m": {}, "30m": {},
	"1h": {}, "3h": {}, "8h": {},
	"24h": {}, "48h": {}, "1w": {}, "once": {},
}

// NormalizeBannerSchedule returns a canonical token or default.
func NormalizeBannerSchedule(s, def string) string {
	t := strings.TrimSpace(strings.ToLower(s))
	if t == "1d" || t == "day" {
		return "24h"
	}
	if t == "2d" {
		return "48h"
	}
	if t == "week" || t == "7d" {
		return "1w"
	}
	if _, ok := allowedBannerSchedules[t]; ok {
		return t
	}
	if strings.TrimSpace(def) == "" {
		return "1h"
	}
	return def
}

func bannerScheduleDuration(s string) (d time.Duration, once bool) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "once":
		return 0, true
	case "5m":
		return 5 * time.Minute, false
	case "10m":
		return 10 * time.Minute, false
	case "30m":
		return 30 * time.Minute, false
	case "1h":
		return time.Hour, false
	case "3h":
		return 3 * time.Hour, false
	case "8h":
		return 8 * time.Hour, false
	case "24h":
		return 24 * time.Hour, false
	case "48h":
		return 48 * time.Hour, false
	case "1w":
		return 7 * 24 * time.Hour, false
	default:
		return time.Hour, false
	}
}

func sortMoviesForFanartBanner(movies []Movie) []Movie {
	out := make([]Movie, 0, len(movies))
	for _, m := range movies {
		if strings.TrimSpace(m.RatingKey) == "" {
			continue
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		vi, vj := out[i].ViewCount, out[j].ViewCount
		if vi != vj {
			return vi > vj
		}
		ai, aj := out[i].AddedAtEpoch, out[j].AddedAtEpoch
		if ai != aj {
			return ai > aj
		}
		if out[i].Year != out[j].Year {
			return out[i].Year > out[j].Year
		}
		return out[i].Title < out[j].Title
	})
	return out
}

type fanartManifestEntry struct {
	ID           string `json:"id"`
	File         string `json:"file"`
	Bytes        int64  `json:"bytes"`
	LastUsedUnix int64  `json:"lastUsedUnix"`
	SourceURL    string `json:"sourceURL"`
}

type fanartManifest struct {
	Entries []fanartManifestEntry `json:"entries"`
}

func fanartManifestPath() string {
	return filepath.Join(fanartBannerCacheDir, "manifest.json")
}

func readFanartManifest() fanartManifest {
	b, err := os.ReadFile(fanartManifestPath())
	if err != nil {
		return fanartManifest{}
	}
	var m fanartManifest
	if json.Unmarshal(b, &m) != nil {
		return fanartManifest{}
	}
	return m
}

func writeFanartManifest(m fanartManifest) error {
	if err := os.MkdirAll(fanartBannerCacheDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := fanartManifestPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, fanartManifestPath())
}

func fanartURLHash(sourceURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sourceURL)))
	return hex.EncodeToString(sum[:16])
}

// fanartCacheTotalBytes sums manifest entry sizes (best-effort vs disk).
func fanartCacheTotalBytes(m fanartManifest) int64 {
	var t int64
	for _, e := range m.Entries {
		t += e.Bytes
	}
	return t
}

func (s *Server) fanartEvictIfNeeded(needFree int64, maxTotal int64) error {
	s.fanartManifestMu.Lock()
	defer s.fanartManifestMu.Unlock()

	m := readFanartManifest()
	if maxTotal <= 0 {
		maxTotal = 200 << 20
	}
	for fanartCacheTotalBytes(m)+needFree > maxTotal && len(m.Entries) > 0 {
		sort.SliceStable(m.Entries, func(i, j int) bool {
			if m.Entries[i].LastUsedUnix != m.Entries[j].LastUsedUnix {
				return m.Entries[i].LastUsedUnix < m.Entries[j].LastUsedUnix
			}
			return m.Entries[i].ID < m.Entries[j].ID
		})
		victim := m.Entries[0]
		m.Entries = m.Entries[1:]
		_ = os.Remove(filepath.Join(fanartBannerCacheDir, victim.File))
		fmt.Printf("[fanart-cache] evicted id=%s bytes=%d (budget)\n", victim.ID, victim.Bytes)
		s.fanartLogAppend(fmt.Sprintf("Fanart cache eviction (budget): id=%s removed ~%d bytes", victim.ID, victim.Bytes))
	}
	return writeFanartManifest(m)
}

func (s *Server) fanartTouchEntry(id string) {
	if id == "" {
		return
	}
	s.fanartManifestMu.Lock()
	defer s.fanartManifestMu.Unlock()
	m := readFanartManifest()
	now := time.Now().Unix()
	changed := false
	for i := range m.Entries {
		if m.Entries[i].ID == id {
			m.Entries[i].LastUsedUnix = now
			changed = true
			break
		}
	}
	if changed {
		_ = writeFanartManifest(m)
	}
}

// fanartDownloadToCache fetches image bytes into the LRU disk cache.
func (s *Server) fanartDownloadToCache(ctx context.Context, sourceURL string, maxMB int) (entryID string, err error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return "", fmt.Errorf("empty url")
	}
	maxTotal := int64(maxMB) * 1024 * 1024
	if maxTotal <= 0 {
		maxTotal = 200 << 20
	}

	id := fanartURLHash(sourceURL)
	fileName := id + ".img"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		s.fanartLogAppend(fmt.Sprintf("Fanart download request failed: %v", err))
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		s.fanartLogAppend(fmt.Sprintf("Fanart download HTTP error: %d from image URL", resp.StatusCode))
		return "", fmt.Errorf("upstream http %d", resp.StatusCode)
	}

	if err := os.MkdirAll(fanartBannerCacheDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(fanartBannerCacheDir, fileName)
	tmp, err := os.CreateTemp(fanartBannerCacheDir, ".dl-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	n, copyErr := io.Copy(tmp, io.LimitReader(resp.Body, 40<<20))
	if cerr := tmp.Close(); cerr != nil && copyErr == nil {
		copyErr = cerr
	}
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		s.fanartLogAppend(fmt.Sprintf("Fanart download failed (read/write): %v", copyErr))
		return "", copyErr
	}

	// Make room before committing the new file.
	if err := s.fanartEvictIfNeeded(n, maxTotal); err != nil {
		_ = os.Remove(tmpPath)
		s.fanartLogAppend(fmt.Sprintf("Fanart download aborted (evict): %v", err))
		return "", err
	}

	if err := os.Rename(tmpPath, dest); err != nil {
		_ = os.Remove(tmpPath)
		s.fanartLogAppend(fmt.Sprintf("Fanart download failed (rename): %v", err))
		return "", err
	}

	s.fanartManifestMu.Lock()
	defer s.fanartManifestMu.Unlock()
	m := readFanartManifest()
	now := time.Now().Unix()
	found := false
	for i := range m.Entries {
		if m.Entries[i].ID == id {
			m.Entries[i].Bytes = n
			m.Entries[i].LastUsedUnix = now
			m.Entries[i].SourceURL = sourceURL
			m.Entries[i].File = fileName
			found = true
			break
		}
	}
	if !found {
		m.Entries = append(m.Entries, fanartManifestEntry{
			ID:           id,
			File:         fileName,
			Bytes:        n,
			LastUsedUnix: now,
			SourceURL:    sourceURL,
		})
	}
	// Second pass eviction if we crossed budget (e.g. race).
	for fanartCacheTotalBytes(m) > maxTotal && len(m.Entries) > 0 {
		sort.SliceStable(m.Entries, func(i, j int) bool {
			if m.Entries[i].LastUsedUnix != m.Entries[j].LastUsedUnix {
				return m.Entries[i].LastUsedUnix < m.Entries[j].LastUsedUnix
			}
			return m.Entries[i].ID < m.Entries[j].ID
		})
		victim := m.Entries[0]
		if victim.ID == id && len(m.Entries) == 1 {
			break
		}
		if victim.ID == id {
			victim = m.Entries[1]
			m.Entries = append(m.Entries[:1], m.Entries[2:]...)
		} else {
			m.Entries = m.Entries[1:]
		}
		_ = os.Remove(filepath.Join(fanartBannerCacheDir, victim.File))
		fmt.Printf("[fanart-cache] evicted id=%s bytes=%d (post-add)\n", victim.ID, victim.Bytes)
		s.fanartLogAppend(fmt.Sprintf("Fanart cache eviction (post-add): id=%s removed ~%d bytes", victim.ID, victim.Bytes))
	}
	if err := writeFanartManifest(m); err != nil {
		return id, err
	}
	s.fanartLogAppend(fmt.Sprintf("Fanart image saved to cache: id=%s bytes=%d", id, n))
	return id, nil
}

func movieToBannerPayload(m Movie) map[string]any {
	topActors := m.Actors
	if len(topActors) > 8 {
		topActors = topActors[:8]
	}
	return map[string]any{
		"ratingKey":   m.RatingKey,
		"title":       m.Title,
		"year":        m.Year,
		"tmdbId":      m.TMDBID,
		"durationMs":  m.DurationMillis,
		"viewCount":   m.ViewCount,
		"rating":      m.Rating,
		"summary":     m.Summary,
		"actors":      topActors,
		"partKey":     m.PartKey,
		"container":   m.FileContainer,
		"partSize":    m.PartSize,
	}
}

// syncFanartBannerPayload updates in-memory/disk fanart state and returns the same JSON shape as the HTTP handler.
func (s *Server) syncFanartBannerPayload(ctx context.Context, cfg Config) map[string]any {
	out := map[string]any{"active": false}
	if !cfg.FanartEnabled || strings.TrimSpace(cfg.FanartAPIKey) == "" {
		out["reason"] = "fanart_disabled_or_missing_key"
		return out
	}

	movies, err := s.cachedListMovies(ctx)
	if err != nil {
		return map[string]any{"active": false, "error": err.Error()}
	}
	queue := sortMoviesForFanartBanner(movies)
	if len(queue) == 0 {
		out["reason"] = "no_movies"
		return out
	}

	refreshTok := NormalizeBannerSchedule(cfg.BannerArtRefresh, "1h")
	rotateTok := NormalizeBannerSchedule(cfg.BannerRotateInterval, "30m")
	rotDur, rotOnce := bannerScheduleDuration(rotateTok)
	refDur, refOnce := bannerScheduleDuration(refreshTok)
	now := time.Now()

	s.fanartBannerMu.Lock()
	if !s.fanartBannerInitialized {
		s.fanartBannerInitialized = true
		s.fanartBannerLastRotate = now
		s.fanartBannerLastFetch = time.Time{}
		s.fanartBannerRotateIdx = 0
	}

	if !rotOnce && now.Sub(s.fanartBannerLastRotate) >= rotDur {
		s.fanartBannerRotateIdx = (s.fanartBannerRotateIdx + 1) % len(queue)
		s.fanartBannerLastRotate = now
		s.fanartBannerLastFetch = time.Time{}
	}

	idx := s.fanartBannerRotateIdx % len(queue)
	m := queue[idx]

	movieChanged := m.RatingKey != s.fanartBannerMovieKey
	if movieChanged {
		s.fanartBannerMovieKey = m.RatingKey
		s.fanartBannerLastFetch = time.Time{}
	}

	needFetch := movieChanged
	if !needFetch {
		if refOnce {
			if s.fanartBannerLastFetch.IsZero() {
				needFetch = true
			}
		} else if s.fanartBannerLastFetch.IsZero() || now.Sub(s.fanartBannerLastFetch) >= refDur {
			needFetch = true
		}
	}

	var fetchErr string
	if needFetch {
		s.fanartLogAppend(fmt.Sprintf("Banner art refresh — title=%q ratingKey=%s tmdb=%d", m.Title, m.RatingKey, m.TMDBID))
		s.fanartBannerLastFetch = now
		if m.TMDBID > 0 {
			artURL, ferr := FetchFanartMovieBannerURL(ctx, m.TMDBID, cfg.FanartAPIKey, cfg.FanartClientKey)
			if ferr == nil && artURL != "" {
				eid, derr := s.fanartDownloadToCache(ctx, artURL, cfg.FanartBannerCacheMaxMB)
				if derr == nil {
					s.fanartBannerImageKind = "fanart"
					s.fanartBannerFanartURL = artURL
					s.fanartBannerDiskID = eid
					s.fanartBannerTMDB = m.TMDBID
					s.fanartBannerVersion++
				} else {
					fetchErr = derr.Error()
					s.fanartBannerImageKind = "plex"
					s.fanartBannerFanartURL = ""
					s.fanartBannerDiskID = ""
					s.fanartLogAppend(fmt.Sprintf("Fanart API ok but cache write failed — %q: %s", m.Title, fetchErr))
				}
			} else {
				if ferr != nil {
					fetchErr = ferr.Error()
					s.fanartLogAppend(fmt.Sprintf("Fanart API no image — %q tmdb=%d: %s", m.Title, m.TMDBID, fetchErr))
				} else {
					s.fanartLogAppend(fmt.Sprintf("Fanart API returned no banner URL — %q tmdb=%d", m.Title, m.TMDBID))
				}
				s.fanartBannerImageKind = "plex"
				s.fanartBannerFanartURL = ""
				s.fanartBannerDiskID = ""
			}
		} else {
			s.fanartBannerImageKind = "plex"
			s.fanartBannerFanartURL = ""
			s.fanartBannerDiskID = ""
			fetchErr = "no_tmdb_id"
			s.fanartLogAppend(fmt.Sprintf("Banner using Plex thumb — %q has no TMDB id on Plex metadata", m.Title))
		}
	}

	kind := s.fanartBannerImageKind
	diskID := s.fanartBannerDiskID
	ver := s.fanartBannerVersion
	s.fanartBannerMu.Unlock()

	imageURL := ""
	if kind == "fanart" && diskID != "" {
		imageURL = fmt.Sprintf("/api/branding/fanart-banner/file?v=%d", ver)
	}
	if imageURL == "" {
		imageURL = "/api/plex/thumb?ratingKey=" + url.QueryEscape(m.RatingKey)
	}

	return map[string]any{
		"active":       true,
		"imageUrl":     imageURL,
		"movie":        movieToBannerPayload(m),
		"source":       kind,
		"fetchError":   fetchErr,
		"rotateIdx":    idx,
		"queueLen":     len(queue),
		"refreshToken": refreshTok,
		"rotateToken":  rotateTok,
	}
}

// WarmFanartBannerPrefetch runs after library warm-up: fetches fanart for the hero banner in the background
// so the first browser request often hits a warm disk cache.
func (s *Server) WarmFanartBannerPrefetch(ctx context.Context) {
	const maxWait = 120 * time.Second
	const step = 500 * time.Millisecond
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		cfg := s.snapshot()
		if !cfg.FanartEnabled || strings.TrimSpace(cfg.FanartAPIKey) == "" {
			fmt.Println("[fanart] prefetch skipped — disabled or no FANART_API_KEY")
			s.fanartLogAppend("Startup prefetch skipped (fanart disabled or no FANART_API_KEY)")
			return
		}
		tctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		movies, err := s.cachedListMovies(tctx)
		cancel()
		if err == nil && len(movies) > 0 {
			s.fanartLogAppend("Startup prefetch: library ready, syncing banner art in background")
			tctx2, cancel2 := context.WithTimeout(ctx, 90*time.Second)
			_ = s.syncFanartBannerPayload(tctx2, cfg)
			cancel2()
			fmt.Println("[fanart] prefetch: banner sync completed in background")
			s.fanartLogAppend("Startup prefetch: banner sync finished")
			return
		}
		select {
		case <-ctx.Done():
			fmt.Println("[fanart] prefetch cancelled")
			s.fanartLogAppend("Startup prefetch cancelled")
			return
		case <-time.After(step):
		}
	}
	fmt.Println("[fanart] prefetch skipped — library not ready in time")
	s.fanartLogAppend("Startup prefetch skipped (library not ready in time)")
}

func (s *Server) handleBrandingFanartBanner(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	cfg := s.snapshot()
	data := s.syncFanartBannerPayload(ctx, cfg)
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: data})
}

// handleBrandingFanartBannerFile serves the current fanart disk blob; 404 if not fanart-backed.
func (s *Server) handleBrandingFanartBannerFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	s.fanartBannerMu.Lock()
	id := strings.TrimSpace(s.fanartBannerDiskID)
	kind := s.fanartBannerImageKind
	s.fanartBannerMu.Unlock()

	if kind != "fanart" || id == "" {
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

func (s *Server) handleFanartBannerCacheStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	s.fanartManifestMu.Lock()
	m := readFanartManifest()
	s.fanartManifestMu.Unlock()
	var total int64
	for _, e := range m.Entries {
		total += e.Bytes
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"dir":         fanartBannerCacheDir,
		"entries":     len(m.Entries),
		"bytes":       total,
		"maxBytes":    int64(s.snapshot().FanartBannerCacheMaxMB) * 1024 * 1024,
		"maxMB":       s.snapshot().FanartBannerCacheMaxMB,
		"description": "Fanart.tv banner images downloaded for the hero strip",
	}})
}

func (s *Server) handleFanartBannerCacheInvalidate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	s.fanartManifestMu.Lock()
	m := readFanartManifest()
	n := len(m.Entries)
	for _, e := range m.Entries {
		_ = os.Remove(filepath.Join(fanartBannerCacheDir, filepath.Base(e.File)))
	}
	m.Entries = nil
	_ = writeFanartManifest(m)
	s.fanartManifestMu.Unlock()

	s.fanartBannerMu.Lock()
	s.fanartBannerInitialized = false
	s.fanartBannerRotateIdx = 0
	s.fanartBannerLastRotate = time.Time{}
	s.fanartBannerLastFetch = time.Time{}
	s.fanartBannerMovieKey = ""
	s.fanartBannerImageKind = ""
	s.fanartBannerFanartURL = ""
	s.fanartBannerDiskID = ""
	s.fanartBannerTMDB = 0
	s.fanartBannerVersion++
	s.fanartBannerMu.Unlock()

	fmt.Printf("[fanart-cache] cleared %d manifest entries\n", n)
	s.fanartLogAppend(fmt.Sprintf("Fanart banner cache cleared (%d file record(s))", n))
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"removedEntries": n,
	}})
}

func (s *Server) handleFanartBannerLog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}
	s.fanartLogMu.Lock()
	n := len(s.fanartLog)
	entries := make([]FanartLogEntry, n)
	copy(entries, s.fanartLog)
	s.fanartLogMu.Unlock()
	// Newest first for the UI
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"entries": entries,
	}})
}
