package plexdash

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// statDirectory sums file sizes and counts under path; returns directory mtime as newest when empty.
func statDirectory(path string) (bytes int64, nfiles int, newest time.Time, exists bool, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, time.Time{}, false, nil
		}
		return 0, 0, time.Time{}, false, err
	}
	exists = true
	newest = fi.ModTime()
	if !fi.IsDir() {
		return fi.Size(), 1, fi.ModTime(), true, nil
	}
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		bytes += info.Size()
		nfiles++
		mt := info.ModTime()
		if mt.After(newest) {
			newest = mt
		}
		return nil
	})
	return bytes, nfiles, newest, exists, err
}

func statRegularFile(path string) (bytes int64, mod time.Time, exists bool, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, time.Time{}, false, nil
		}
		return 0, time.Time{}, false, err
	}
	if fi.IsDir() {
		return 0, time.Time{}, false, fmt.Errorf("expected file: %s", path)
	}
	return fi.Size(), fi.ModTime(), true, nil
}

func absDisplayPath(rel string) string {
	rel = filepath.Clean(rel)
	abs, err := filepath.Abs(rel)
	if err != nil {
		return rel
	}
	return abs
}

func cacheEntryMemory(id, label, location, notes string, updatedAt time.Time, extra string) map[string]any {
	m := map[string]any{
		"id":       id,
		"label":    label,
		"kind":     "memory",
		"location": location,
		"notes":    notes,
	}
	if !updatedAt.IsZero() {
		m["updatedAt"] = updatedAt.UTC().Format(time.RFC3339)
	}
	if extra != "" {
		m["extra"] = extra
	}
	return m
}

func cacheEntryDisk(id, label, kind, relPath, notes string, bytes int64, nfiles int, newest time.Time, exists bool) map[string]any {
	m := map[string]any{
		"id":       id,
		"label":    label,
		"kind":     kind,
		"location": absDisplayPath(relPath),
		"exists":   exists,
		"notes":    notes,
	}
	if exists {
		m["bytes"] = bytes
		m["fileCount"] = nfiles
		if !newest.IsZero() {
			m["updatedAt"] = newest.UTC().Format(time.RFC3339)
		}
	}
	return m
}

// handleSettingsCaches: GET /api/settings/caches — sizes and mtimes for on-disk caches plus server memory cache metadata.
func (s *Server) handleSettingsCaches(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondJSON(w, http.StatusMethodNotAllowed, apiResponse{Success: false, Error: "method not allowed"})
		return
	}

	cwd, _ := os.Getwd()
	caches := make([]map[string]any, 0, 12)

	s.mlMu.RLock()
	nMovies := len(s.mlMovies)
	mlAt := s.mlCachedAt
	mlKey := s.mlKey
	s.mlMu.RUnlock()
	mlExtra := fmt.Sprintf("Library key in cache: %q (must match current settings for a hit).", mlKey)
	if nMovies == 0 {
		mlExtra = "Empty until someone loads the library (Dashboard Load/Refresh or Discovery). " + mlExtra
	}
	memMovies := cacheEntryMemory(
		"serverMovies",
		"Plex movie list (server RAM)",
		"In-process (shared by Dashboard, Discovery, playlists)",
		"Refresh from Plex with Dashboard “Refresh Movies” or ?nocache=1. Cleared on server restart.",
		mlAt,
		mlExtra,
	)
	memMovies["titleCount"] = nMovies
	caches = append(caches, memMovies)

	s.remoteCountMu.Lock()
	rcVal := s.remoteCountVal
	rcAt := s.remoteCountAt
	rcLib := s.remoteCountLibKey
	s.remoteCountMu.Unlock()
	rcExtra := ""
	if !rcAt.IsZero() {
		rcExtra = fmt.Sprintf("Cached total: %d titles (Plex library key %q).", rcVal, rcLib)
	} else {
		rcExtra = "Not fetched yet; loads when the Dashboard library hint runs."
	}
	caches = append(caches, cacheEntryMemory(
		"plexRemoteCount",
		"Plex library title count (server)",
		"In-process, ~90s TTL",
		"Avoids hammering Plex when the grid hint polls.",
		rcAt,
		rcExtra,
	))

	s.streamProbeMu.Lock()
	ps := s.plexStreamCache
	s.streamProbeMu.Unlock()
	var probeAt time.Time
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(ps.ProbedAt)); err == nil {
		probeAt = t
	}
	streamExtra := strings.TrimSpace(ps.Level + " — " + ps.Message)
	streamExtra = strings.Trim(strings.TrimSpace(streamExtra), "—")
	streamExtra = strings.TrimSpace(streamExtra)
	if streamExtra == "" {
		streamExtra = "No sample yet (needs Plex + movies in server memory)."
	}
	if ps.Mbps > 0 {
		streamExtra += fmt.Sprintf(" (~%.1f Mb/s)", ps.Mbps)
	}
	if strings.TrimSpace(ps.MovieTitle) != "" {
		streamExtra += fmt.Sprintf(" [%s]", ps.MovieTitle)
	}
	caches = append(caches, cacheEntryMemory(
		"plexStreamSample",
		"Plex throughput sample (server)",
		"In-process",
		"~4 minute interval; small ranged read from one library file.",
		probeAt,
		streamExtra,
	))

	tmdbDir := defaultDiscoveryCacheDir()
	b, n, newest, ex, err := statDirectory(tmdbDir)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
		return
	}
	caches = append(caches, cacheEntryDisk(
		"tmdbDiscovery",
		"TMDB / Discovery disk cache",
		"directory",
		tmdbDir,
		"Cleared by Discovery tab “Clear TMDB cache”.",
		b, n, newest, ex,
	))

	omdbDir := defaultOMDbCacheDir()
	b, n, newest, ex, err = statDirectory(omdbDir)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
		return
	}
	caches = append(caches, cacheEntryDisk(
		"omdb",
		"OMDb ratings cache",
		"directory",
		omdbDir,
		"Cleared together with TMDB discovery cache.",
		b, n, newest, ex,
	))

	snapDir := snapshotDir()
	b, n, newest, ex, err = statDirectory(snapDir)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
		return
	}
	caches = append(caches, cacheEntryDisk(
		"snapshots",
		"Daily movie snapshots",
		"directory",
		snapDir,
		"Index + snapshot-*.json; managed by server snapshot jobs.",
		b, n, newest, ex,
	))

	pbPath := playbackStatePath()
	pbBytes, pbMod, pbEx, err := statRegularFile(pbPath)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
		return
	}
	pbEntry := cacheEntryDisk("playbackState", "Last webOS playback hint", "file", pbPath, "Small JSON for UI after restart.", 0, 0, time.Time{}, pbEx)
	if pbEx {
		pbEntry["bytes"] = pbBytes
		pbEntry["fileCount"] = 1
		pbEntry["updatedAt"] = pbMod.UTC().Format(time.RFC3339)
	}
	caches = append(caches, pbEntry)

	setPath := s.settingsPath
	if strings.TrimSpace(setPath) == "" {
		setPath = defaultSettingsPath()
	}
	stBytes, stMod, stEx, err := statRegularFile(setPath)
	if err != nil {
		respondJSON(w, http.StatusInternalServerError, apiResponse{Success: false, Error: err.Error()})
		return
	}
	stEntry := cacheEntryDisk("settingsFile", "Saved settings file", "file", setPath, "Persisted API keys and options (Save Settings).", 0, 0, time.Time{}, stEx)
	if stEx {
		stEntry["bytes"] = stBytes
		stEntry["fileCount"] = 1
		stEntry["updatedAt"] = stMod.UTC().Format(time.RFC3339)
	}
	caches = append(caches, stEntry)

	respondJSON(w, http.StatusOK, apiResponse{Success: true, Data: map[string]any{
		"workingDirectory": cwd,
		"caches":           caches,
	}})
}
