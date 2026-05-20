package plexdash

// PlaybackHistory records playback snapshots over time so callers can ask
// "what was playing at time T". A background poller queries
// PlexClient.ListPlaybackSessions() every 30s and appends each active session
// to an in-memory ring + a JSONL file. On startup the ring is rehydrated from
// the JSONL (last 24h only).
//
// Used by /api/playback/at, /api/playback/history, and the captions handler
// when the caller passes ?at=<rfc3339>.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PlaybackSnapshot is one row recorded by the poller. The type name avoids
// collision with the unrelated `Snapshot` type in snapshots.go.
type PlaybackSnapshot struct {
	Ts              time.Time `json:"ts"`
	RatingKey       string    `json:"rating_key"`
	IMDbID          string    `json:"imdb_id,omitempty"`
	TMDBID          int       `json:"tmdb_id,omitempty"`
	Title           string    `json:"title"`
	Year            int       `json:"year,omitempty"`
	PlayerName      string    `json:"player_name,omitempty"`
	PlayerMachineID string    `json:"player_machine_id,omitempty"`
	ViewOffsetMs    int64     `json:"view_offset_ms"`
	DurationMs      int64     `json:"duration_ms"`
}

const (
	playbackHistoryRingCap     = 5000
	playbackHistoryRetention   = 24 * time.Hour
	playbackHistoryPollPeriod  = 30 * time.Second
	playbackHistoryPollTimeout = 5 * time.Second
	playbackHistoryWindow      = 2 * time.Minute
)

// playbackHistoryPlexClient is the slice of *PlexClient the poller needs. A
// local interface keeps tests stub-friendly.
type playbackHistoryPlexClient interface {
	ListPlaybackSessions(ctx context.Context) ([]PlaybackSession, error)
}

// movieResolver returns the IMDb id / TMDB id for a given ratingKey when
// known. Production wires this to Server.cachedListMovies; tests can pass nil.
type movieResolver func(ctx context.Context, ratingKey string) (imdbID string, tmdbID int, ok bool)

// PlaybackHistory holds the rolling snapshot ring plus its on-disk JSONL log.
type PlaybackHistory struct {
	path string

	mu      sync.RWMutex
	entries []PlaybackSnapshot

	plex     playbackHistoryPlexClient
	resolve  movieResolver
	period   time.Duration
	timeout  time.Duration
}

// NewPlaybackHistory builds an empty PlaybackHistory rooted at path. Pass
// plex=nil and resolve=nil for tests that just want Append/LookupAt.
func NewPlaybackHistory(path string, plex playbackHistoryPlexClient, resolve movieResolver) *PlaybackHistory {
	h := &PlaybackHistory{
		path:    path,
		plex:    plex,
		resolve: resolve,
		period:  playbackHistoryPollPeriod,
		timeout: playbackHistoryPollTimeout,
	}
	h.loadFromDisk()
	return h
}

// Append adds one snapshot to the in-memory ring and appends a JSONL line to
// the on-disk log. Caller-supplied Ts is preserved (no clock substitution).
func (h *PlaybackHistory) Append(snap PlaybackSnapshot) {
	if snap.Ts.IsZero() {
		snap.Ts = time.Now().UTC()
	}
	h.mu.Lock()
	h.entries = append(h.entries, snap)
	if len(h.entries) > playbackHistoryRingCap {
		// Drop oldest in chunks so we don't churn on every append.
		drop := len(h.entries) - playbackHistoryRingCap
		h.entries = append(h.entries[:0], h.entries[drop:]...)
	}
	h.mu.Unlock()
	h.appendToDisk(snap)
}

// LookupAt finds the snapshot most relevant to time t, optionally restricted
// to a player whose name contains the supplied substring (case-insensitive).
//
// Algorithm (per PLAN-playback-history.md):
//   1. Filter to ±2 minutes of t.
//   2. If player != "", restrict to entries whose PlayerName contains player.
//   3. Pick the closest entry.
//   4. Fallback: most recent entry with ts <= t whose runtime still overlaps t.
//   5. Otherwise return ok=false.
func (h *PlaybackHistory) LookupAt(t time.Time, player string) (PlaybackSnapshot, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.entries) == 0 {
		return PlaybackSnapshot{}, false
	}
	needle := strings.ToLower(strings.TrimSpace(player))
	matchPlayer := func(e PlaybackSnapshot) bool {
		if needle == "" {
			return true
		}
		return strings.Contains(strings.ToLower(e.PlayerName), needle)
	}

	// Step 1+2+3: window match.
	var best PlaybackSnapshot
	bestDelta := time.Duration(1<<62)
	found := false
	for _, e := range h.entries {
		if !matchPlayer(e) {
			continue
		}
		d := e.Ts.Sub(t)
		if d < 0 {
			d = -d
		}
		if d <= playbackHistoryWindow && d < bestDelta {
			best = e
			bestDelta = d
			found = true
		}
	}
	if found {
		return best, true
	}

	// Step 4: fallback — most recent entry with ts <= t whose runtime still
	// covers t.
	var fb PlaybackSnapshot
	fbFound := false
	for _, e := range h.entries {
		if !matchPlayer(e) {
			continue
		}
		if e.Ts.After(t) {
			continue
		}
		// Movie ends at e.Ts + (duration - viewOffset). t must be before that.
		remaining := time.Duration(e.DurationMs-e.ViewOffsetMs) * time.Millisecond
		if remaining <= 0 {
			continue
		}
		if t.Sub(e.Ts) > remaining {
			continue
		}
		if !fbFound || e.Ts.After(fb.Ts) {
			fb = e
			fbFound = true
		}
	}
	if fbFound {
		return fb, true
	}
	return PlaybackSnapshot{}, false
}

// Range returns all snapshots whose Ts is within [since, until] inclusive.
// If player != "", only entries whose PlayerName contains player are returned.
// Results are sorted by Ts ascending.
func (h *PlaybackHistory) Range(since, until time.Time, player string) []PlaybackSnapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()
	needle := strings.ToLower(strings.TrimSpace(player))
	out := make([]PlaybackSnapshot, 0, len(h.entries))
	for _, e := range h.entries {
		if e.Ts.Before(since) || e.Ts.After(until) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(e.PlayerName), needle) {
			continue
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts.Before(out[j].Ts) })
	return out
}

// Start launches the background poll loop. Returns immediately. The loop
// exits when ctx is cancelled. If plex==nil (e.g. tests) Start is a no-op.
func (h *PlaybackHistory) Start(ctx context.Context) {
	if h.plex == nil {
		return
	}
	go h.run(ctx)
}

func (h *PlaybackHistory) run(ctx context.Context) {
	// Don't fetch immediately — give the rest of the server a moment to come
	// up. The first tick fires after `period`.
	t := time.NewTicker(h.period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.poll(ctx)
		}
	}
}

func (h *PlaybackHistory) poll(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, h.timeout)
	defer cancel()
	sessions, err := h.plex.ListPlaybackSessions(ctx)
	if err != nil {
		log.Printf("[playback-history] poll: %v", err)
		return
	}
	if len(sessions) == 0 {
		return // nothing playing — empty polls are not written
	}
	now := time.Now().UTC()
	for _, sess := range sessions {
		if sess.RatingKey == "" {
			continue
		}
		t := strings.ToLower(strings.TrimSpace(sess.Type))
		if t != "" && t != "movie" && t != "episode" && t != "video" {
			continue
		}
		year, _ := strconv.Atoi(strings.TrimSpace(sess.Year))
		snap := PlaybackSnapshot{
			Ts:              now,
			RatingKey:       sess.RatingKey,
			Title:           sess.Title,
			Year:            year,
			PlayerName:      sess.PlayerName,
			PlayerMachineID: sess.MachineID,
			ViewOffsetMs:    sess.ViewOffsetMs,
			DurationMs:      sess.DurationMs,
		}
		if h.resolve != nil {
			if imdb, tmdb, ok := h.resolve(ctx, sess.RatingKey); ok {
				snap.IMDbID = imdb
				snap.TMDBID = tmdb
			}
		}
		h.Append(snap)
	}
}

// ── disk persistence ─────────────────────────────────────────────────────────

func (h *PlaybackHistory) appendToDisk(snap PlaybackSnapshot) {
	if h.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		log.Printf("[playback-history] mkdir: %v", err)
		return
	}
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		log.Printf("[playback-history] open %s: %v", h.path, err)
		return
	}
	defer f.Close()
	b, err := json.Marshal(snap)
	if err != nil {
		log.Printf("[playback-history] marshal: %v", err)
		return
	}
	b = append(b, '\n')
	if _, err := f.Write(b); err != nil {
		log.Printf("[playback-history] write: %v", err)
	}
}

// loadFromDisk rehydrates the in-memory ring from JSONL. Stops loading once
// it sees an entry older than 24h.
func (h *PlaybackHistory) loadFromDisk() {
	if h.path == "" {
		return
	}
	f, err := os.Open(h.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[playback-history] open %s: %v", h.path, err)
		}
		return
	}
	defer f.Close()
	cutoff := time.Now().Add(-playbackHistoryRetention)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	loaded := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var snap PlaybackSnapshot
		if err := json.Unmarshal([]byte(line), &snap); err != nil {
			continue
		}
		if snap.Ts.Before(cutoff) {
			continue
		}
		h.entries = append(h.entries, snap)
		loaded++
		if len(h.entries) > playbackHistoryRingCap {
			drop := len(h.entries) - playbackHistoryRingCap
			h.entries = append(h.entries[:0], h.entries[drop:]...)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[playback-history] scan: %v", err)
	}
	if loaded > 0 {
		log.Printf("[playback-history] loaded %d entries from %s", loaded, h.path)
	}
}

// String exists so PlaybackSnapshot logs nicely.
func (s PlaybackSnapshot) String() string {
	return fmt.Sprintf("PlaybackSnapshot{ratingKey=%s title=%q player=%s offset=%dms duration=%dms ts=%s}",
		s.RatingKey, s.Title, s.PlayerName, s.ViewOffsetMs, s.DurationMs, s.Ts.Format(time.RFC3339))
}
