package plexdash

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newPlaybackHistoryForTest builds a PlaybackHistory rooted at path with no
// background poller wired in. Tests drive Append() directly.
func newPlaybackHistoryForTest(t *testing.T, path string) *PlaybackHistory {
	t.Helper()
	return NewPlaybackHistory(path, nil, nil)
}

// ── named tests aligned with the plan's test IDs (H1..H7) ────────────────────

func TestPlaybackHistory_AppendAndLookupAt_ExactWindow(t *testing.T) {
	hist := newPlaybackHistoryForTest(t, filepath.Join(t.TempDir(), "history.jsonl"))
	t0 := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	hist.Append(PlaybackSnapshot{Ts: t0.Add(-60 * time.Second), RatingKey: "1", Title: "Before", PlayerName: "LG", DurationMs: 7200000})
	hist.Append(PlaybackSnapshot{Ts: t0, RatingKey: "2", Title: "Target", PlayerName: "LG", DurationMs: 7200000})
	hist.Append(PlaybackSnapshot{Ts: t0.Add(60 * time.Second), RatingKey: "3", Title: "After", PlayerName: "LG", DurationMs: 7200000})

	got, ok := hist.LookupAt(t0, "")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.RatingKey != "2" {
		t.Errorf("expected ratingKey 2, got %q", got.RatingKey)
	}
}

func TestPlaybackHistory_LookupAt_FilterByPlayer(t *testing.T) {
	hist := newPlaybackHistoryForTest(t, filepath.Join(t.TempDir(), "history.jsonl"))
	t0 := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	hist.Append(PlaybackSnapshot{Ts: t0, RatingKey: "samsung-key", PlayerName: "Samsung TV", DurationMs: 7200000})
	hist.Append(PlaybackSnapshot{Ts: t0, RatingKey: "lg-key", PlayerName: "LG OLED", DurationMs: 7200000})

	got, ok := hist.LookupAt(t0, "lg")
	if !ok {
		t.Fatal("expected lg hit")
	}
	if got.RatingKey != "lg-key" {
		t.Errorf("expected lg-key, got %q", got.RatingKey)
	}
}

func TestPlaybackHistory_LookupAt_NoEntries(t *testing.T) {
	hist := newPlaybackHistoryForTest(t, filepath.Join(t.TempDir(), "history.jsonl"))
	if _, ok := hist.LookupAt(time.Now(), ""); ok {
		t.Fatal("expected miss on empty history")
	}
}

func TestPlaybackHistory_LookupAt_FallbackToRuntimeWindow(t *testing.T) {
	hist := newPlaybackHistoryForTest(t, filepath.Join(t.TempDir(), "history.jsonl"))
	now := time.Now()
	hist.Append(PlaybackSnapshot{
		Ts:           now.Add(-10 * time.Minute),
		RatingKey:    "matrix",
		Title:        "The Matrix",
		PlayerName:   "LG",
		ViewOffsetMs: 20 * 60 * 1000,
		DurationMs:   2 * 60 * 60 * 1000,
	})
	got, ok := hist.LookupAt(now, "")
	if !ok {
		t.Fatal("expected fallback hit")
	}
	if got.RatingKey != "matrix" {
		t.Errorf("expected matrix, got %q", got.RatingKey)
	}
}

func TestPlaybackHistory_PersistAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	hist := newPlaybackHistoryForTest(t, path)
	now := time.Now()
	hist.Append(PlaybackSnapshot{Ts: now, RatingKey: "abc", IMDbID: "tt7", Title: "Saved", PlayerName: "LG", DurationMs: 5000000})

	hist2 := newPlaybackHistoryForTest(t, path)
	got, ok := hist2.LookupAt(now, "")
	if !ok {
		t.Fatal("expected reload hit")
	}
	if got.RatingKey != "abc" {
		t.Errorf("expected abc, got %q", got.RatingKey)
	}
}

func TestHandlePlaybackAt_ReturnsSnapshot(t *testing.T) {
	dir := t.TempDir()
	hist := newPlaybackHistoryForTest(t, filepath.Join(dir, "history.jsonl"))
	t0 := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	hist.Append(PlaybackSnapshot{Ts: t0, RatingKey: "rk-42", IMDbID: "tt99", Title: "Demo", PlayerName: "LG", ViewOffsetMs: 1000, DurationMs: 60000})

	s := &Server{playbackHistory: hist}
	req := httptest.NewRequest(http.MethodGet, "/api/playback/at?time="+t0.Format(time.RFC3339), nil)
	rec := httptest.NewRecorder()
	s.handlePlaybackAt(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["rating_key"] != "rk-42" {
		t.Errorf("rating_key = %v, want rk-42", body["rating_key"])
	}
}

func TestCaptionsHandler_AcceptsAtParam(t *testing.T) {
	dir := t.TempDir()
	hist := newPlaybackHistoryForTest(t, filepath.Join(dir, "history.jsonl"))
	t0 := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	hist.Append(PlaybackSnapshot{Ts: t0, RatingKey: "rk-77", IMDbID: "tt77", Title: "AtMovie", PlayerName: "LG", DurationMs: 7200000})

	movies := []Movie{{RatingKey: "rk-77", IMDbID: "tt77", Title: "AtMovie", Year: 2026}}
	s := newCaptionsTestServer(nil, movies, dir)
	s.playbackHistory = hist
	// Pre-warm captions cache so the handler hits the cache branch.
	if err := s.captionsCache.Put("tt77", "hello world", CaptionsMeta{Title: "AtMovie", Year: 2026, Source: "cache", FetchedAt: time.Now().UTC().Format(time.RFC3339), ByteCount: 11}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/playback/captions?at="+t0.Format(time.RFC3339), nil)
	rec := httptest.NewRecorder()
	s.handlePlaybackCaptions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp captionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.IMDbID != "tt77" {
		t.Errorf("imdbId = %q, want tt77", resp.IMDbID)
	}
	if !strings.Contains(resp.Captions, "hello world") {
		t.Errorf("captions missing seeded text: %q", resp.Captions)
	}
}

func TestLocalPlaybackAt_WindowBounds(t *testing.T) {
	castAt := time.Date(2026, 5, 20, 0, 25, 0, 0, time.UTC)
	s := &Server{}
	s.localPlayback = localPlaybackSend{
		Titles:     []string{"Normal (2026)"},
		RatingKeys: []string{"66609"},
		SentAt:     castAt,
	}
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"at cast time", castAt, true},
		{"90 min into movie", castAt.Add(90 * time.Minute), true},
		{"within 2 min grace before", castAt.Add(-1 * time.Minute), true},
		{"long before cast", castAt.Add(-30 * time.Minute), false},
		{"past 4h window", castAt.Add(localPlaybackWindow + time.Minute), false},
	}
	for _, c := range cases {
		if _, ok := s.localPlaybackAt(c.at); ok != c.want {
			t.Errorf("%s: ok=%v, want %v", c.name, ok, c.want)
		}
	}
}

func TestLocalPlaybackAt_EmptyOrNoRatingKey(t *testing.T) {
	s := &Server{}
	if _, ok := s.localPlaybackAt(time.Now()); ok {
		t.Error("empty localPlayback should not resolve")
	}
	// A cast with a title but no ratingKey can't yield captions — reject it.
	s.localPlayback = localPlaybackSend{Titles: []string{"Mystery"}, SentAt: time.Now()}
	if _, ok := s.localPlaybackAt(time.Now()); ok {
		t.Error("cast with no ratingKey should not resolve")
	}
}

func TestHandlePlaybackAt_FallsBackToLocalPlayback(t *testing.T) {
	// Empty poller history — simulates the Plex server being unreachable.
	hist := newPlaybackHistoryForTest(t, filepath.Join(t.TempDir(), "history.jsonl"))
	castAt := time.Now().UTC().Add(-20 * time.Minute)
	s := &Server{playbackHistory: hist}
	s.localPlayback = localPlaybackSend{
		Target:     "Living Room",
		Titles:     []string{"Normal (2026)"},
		RatingKeys: []string{"66609"},
		SentAt:     castAt,
		Source:     "movies_play",
	}
	clipTime := castAt.Add(5 * time.Minute)
	req := httptest.NewRequest(http.MethodGet, "/api/playback/at?time="+clipTime.Format(time.RFC3339), nil)
	rec := httptest.NewRecorder()
	s.handlePlaybackAt(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (local-playback fallback); body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["rating_key"] != "66609" {
		t.Errorf("rating_key = %v, want 66609", body["rating_key"])
	}
}
