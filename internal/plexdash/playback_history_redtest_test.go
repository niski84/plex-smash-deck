package plexdash

// RED tests for playback history.
// Fails until PLAN-playback-history.md is implemented.

import (
	"path/filepath"
	"testing"
	"time"
)

func TestRED_PlaybackHistory_AppendAndLookupAt_ExactWindow(t *testing.T) {
	hist := newPlaybackHistoryForTest(t, filepath.Join(t.TempDir(), "history.jsonl"))

	t0 := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	hist.Append(PlaybackSnapshot{Ts: t0.Add(-60 * time.Second), RatingKey: "1", IMDbID: "tt1", Title: "Before", PlayerName: "LG", DurationMs: 7200000})
	hist.Append(PlaybackSnapshot{Ts: t0, RatingKey: "2", IMDbID: "tt2", Title: "Target", PlayerName: "LG", DurationMs: 7200000})
	hist.Append(PlaybackSnapshot{Ts: t0.Add(60 * time.Second), RatingKey: "3", IMDbID: "tt3", Title: "After", PlayerName: "LG", DurationMs: 7200000})

	got, ok := hist.LookupAt(t0, "")
	if !ok {
		t.Fatal("expected hit, got miss")
	}
	if got.RatingKey != "2" {
		t.Errorf("expected ratingKey 2 (closest), got %q", got.RatingKey)
	}
}

func TestRED_PlaybackHistory_LookupAt_FilterByPlayer(t *testing.T) {
	hist := newPlaybackHistoryForTest(t, filepath.Join(t.TempDir(), "history.jsonl"))

	t0 := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	hist.Append(PlaybackSnapshot{Ts: t0, RatingKey: "samsung-key", PlayerName: "Samsung TV", DurationMs: 7200000})
	hist.Append(PlaybackSnapshot{Ts: t0, RatingKey: "lg-key", PlayerName: "LG OLED", DurationMs: 7200000})

	got, ok := hist.LookupAt(t0, "lg")
	if !ok {
		t.Fatal("expected hit for player=lg")
	}
	if got.RatingKey != "lg-key" {
		t.Errorf("expected lg-key, got %q", got.RatingKey)
	}
}

func TestRED_PlaybackHistory_LookupAt_NoEntries(t *testing.T) {
	hist := newPlaybackHistoryForTest(t, filepath.Join(t.TempDir(), "history.jsonl"))
	_, ok := hist.LookupAt(time.Now(), "")
	if ok {
		t.Fatal("expected miss on empty history, got hit")
	}
}

func TestRED_PlaybackHistory_LookupAt_FallbackToRuntimeWindow(t *testing.T) {
	hist := newPlaybackHistoryForTest(t, filepath.Join(t.TempDir(), "history.jsonl"))

	now := time.Now()
	// Last snapshot was 10 min ago, viewOffset was 20min, total runtime 2h.
	// At "now", the movie should still be playing (20m + 10m = 30m, < 2h).
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
		t.Fatal("expected fallback hit (still within runtime), got miss")
	}
	if got.RatingKey != "matrix" {
		t.Errorf("expected matrix, got %q", got.RatingKey)
	}
}

func TestRED_PlaybackHistory_PersistAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")

	hist := newPlaybackHistoryForTest(t, path)
	now := time.Now()
	hist.Append(PlaybackSnapshot{Ts: now, RatingKey: "abc", IMDbID: "tt7", Title: "Saved", PlayerName: "LG", DurationMs: 5000000})

	// Create a fresh instance pointing at the same file.
	hist2 := newPlaybackHistoryForTest(t, path)

	got, ok := hist2.LookupAt(now, "")
	if !ok {
		t.Fatal("expected entry to be reloaded from disk")
	}
	if got.RatingKey != "abc" {
		t.Errorf("expected abc, got %q", got.RatingKey)
	}
}
