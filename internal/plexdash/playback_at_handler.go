package plexdash

// /api/playback/at and /api/playback/history — read-only views into the
// PlaybackHistory ring. Used by Nōgura's TV chatter filter to look up
// "what was playing at this clip's CapturedAt".

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"
)

// playbackAtResponse is the success payload for /api/playback/at.
type playbackAtResponse struct {
	RatingKey       string `json:"rating_key"`
	IMDbID          string `json:"imdb_id,omitempty"`
	TMDBID          int    `json:"tmdb_id,omitempty"`
	Title           string `json:"title,omitempty"`
	Year            int    `json:"year,omitempty"`
	PlayerName      string `json:"player_name,omitempty"`
	PlayerMachineID string `json:"player_machine_id,omitempty"`
	ViewOffsetMs    int64  `json:"view_offset_ms"`
	DurationMs      int64  `json:"duration_ms"`
	SnapshotAt      string `json:"snapshot_at"`
}

// handlePlaybackAt resolves "what was playing at time=<rfc3339>".
//
//	200 → JSON snapshot.
//	204 → no movie was playing at that time.
//	400 → missing/invalid time parameter.
func (s *Server) handlePlaybackAt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		captionsRespondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query()
	tStr := strings.TrimSpace(q.Get("time"))
	if tStr == "" {
		captionsRespondError(w, http.StatusBadRequest, "missing time")
		return
	}
	t, err := time.Parse(time.RFC3339, tStr)
	if err != nil {
		captionsRespondError(w, http.StatusBadRequest, "invalid time (expected RFC3339)")
		return
	}
	player := strings.TrimSpace(q.Get("player"))

	// Primary: the poller history (needs a reachable Plex server).
	var snap PlaybackSnapshot
	var ok bool
	if s.playbackHistory != nil {
		snap, ok = s.playbackHistory.LookupAt(t, player)
	}
	// Fallback: what the dashboard itself cast to the TV. This needs no
	// reachable Plex server — it is recorded the moment the user issues a
	// play command — so it works even when the poller is dead.
	if !ok {
		snap, ok = s.localPlaybackAt(t)
	}
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	out := playbackAtResponse{
		RatingKey:       snap.RatingKey,
		IMDbID:          snap.IMDbID,
		TMDBID:          snap.TMDBID,
		Title:           snap.Title,
		Year:            snap.Year,
		PlayerName:      snap.PlayerName,
		PlayerMachineID: snap.PlayerMachineID,
		ViewOffsetMs:    snap.ViewOffsetMs,
		DurationMs:      snap.DurationMs,
		SnapshotAt:      snap.Ts.UTC().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("[playback-at] encode: %v", err)
	}
}

// localPlaybackWindow bounds how long after a cast the dashboard still treats
// it as "what is on the TV". Generous enough for any feature-length movie,
// capped so a stale cast does not shadow genuinely-new viewing forever.
const localPlaybackWindow = 4 * time.Hour

// localPlaybackAt returns the title the dashboard itself last cast to the TV
// as a synthetic PlaybackSnapshot, when time t falls within that cast's
// plausible window. Unlike the poller history this needs no reachable Plex
// server — the cast is recorded the instant the user issues a play command.
//
// ok=false when nothing has been cast, the cast carries no ratingKey, or t
// is outside the window.
func (s *Server) localPlaybackAt(t time.Time) (PlaybackSnapshot, bool) {
	s.playbackMu.RLock()
	lp := s.localPlayback
	s.playbackMu.RUnlock()

	if len(lp.Titles) == 0 || len(lp.RatingKeys) == 0 || lp.SentAt.IsZero() {
		return PlaybackSnapshot{}, false
	}
	const grace = 2 * time.Minute // tolerate a clip captured just before the cast registered
	if t.Before(lp.SentAt.Add(-grace)) || t.After(lp.SentAt.Add(localPlaybackWindow)) {
		return PlaybackSnapshot{}, false
	}
	return PlaybackSnapshot{
		Ts:         lp.SentAt,
		RatingKey:  lp.RatingKeys[0],
		Title:      lp.Titles[0],
		PlayerName: lp.Target,
	}, true
}

// playbackHistoryResponse is the success payload for /api/playback/history.
type playbackHistoryResponse struct {
	Snapshots []PlaybackSnapshot `json:"snapshots"`
	Count     int                `json:"count"`
}

// handlePlaybackHistory returns all snapshots within [since, until].
//
//	200 → JSON list (possibly empty).
//	400 → missing/invalid since/until.
func (s *Server) handlePlaybackHistory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		captionsRespondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.playbackHistory == nil {
		respondPlaybackHistory(w, nil)
		return
	}
	q := r.URL.Query()
	sinceStr := strings.TrimSpace(q.Get("since"))
	untilStr := strings.TrimSpace(q.Get("until"))
	if sinceStr == "" || untilStr == "" {
		captionsRespondError(w, http.StatusBadRequest, "since and until are required")
		return
	}
	since, err := time.Parse(time.RFC3339, sinceStr)
	if err != nil {
		captionsRespondError(w, http.StatusBadRequest, "invalid since (expected RFC3339)")
		return
	}
	until, err := time.Parse(time.RFC3339, untilStr)
	if err != nil {
		captionsRespondError(w, http.StatusBadRequest, "invalid until (expected RFC3339)")
		return
	}
	player := strings.TrimSpace(q.Get("player"))
	respondPlaybackHistory(w, s.playbackHistory.Range(since, until, player))
}

func respondPlaybackHistory(w http.ResponseWriter, snaps []PlaybackSnapshot) {
	if snaps == nil {
		snaps = []PlaybackSnapshot{}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(playbackHistoryResponse{Snapshots: snaps, Count: len(snaps)}); err != nil {
		log.Printf("[playback-history] encode: %v", err)
	}
}
