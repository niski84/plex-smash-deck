package plexdash

// RED test for the new captions endpoint.
// Fails until the implementation per PLAN-tv-captions-endpoint.md is in place.
//
// Strategy: this is a smoke-level red test that imports the package and
// references three symbols the plan promises to introduce:
//   - ParseSRTToPlainText
//   - CaptionsCache (with Get / Put)
//   - handlePlaybackCaptions handler signature on *Server
//
// If any of those symbols are missing, the package fails to compile and the
// red phase is verified. Once the agent finishes, this file should still
// compile AND the assertions below should pass.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRED_ParseSRTToPlainText_StripsCueNumbersAndTimestamps(t *testing.T) {
	srt := []byte("1\n00:00:01,000 --> 00:00:03,000\nHello there\n\n2\n00:00:04,000 --> 00:00:06,000\n<i>General Kenobi</i>\n")
	got := ParseSRTToPlainText(srt)
	if !strings.Contains(got, "Hello there") {
		t.Errorf("expected 'Hello there' in output, got: %q", got)
	}
	if !strings.Contains(got, "General Kenobi") {
		t.Errorf("expected 'General Kenobi' in output, got: %q", got)
	}
	if strings.Contains(got, "00:00:") {
		t.Errorf("timestamps must be stripped, got: %q", got)
	}
	if strings.Contains(got, "<i>") || strings.Contains(got, "</i>") {
		t.Errorf("HTML tags must be stripped, got: %q", got)
	}
	if strings.Contains(got, "1\n") || strings.HasPrefix(got, "1") {
		t.Errorf("cue numbers must be stripped, got: %q", got)
	}
}

func TestRED_CaptionsCache_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	cache := &CaptionsCache{Dir: dir}

	wantText := "Sample caption text\nAnother line"
	wantMeta := CaptionsMeta{Title: "Test Movie", Year: 2024, Source: "local"}

	if err := cache.Put("tt1234567", wantText, wantMeta); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	gotText, gotMeta, ok := cache.Get("tt1234567")
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if gotText != wantText {
		t.Errorf("text mismatch: got %q want %q", gotText, wantText)
	}
	if gotMeta.Title != wantMeta.Title {
		t.Errorf("meta title: got %q want %q", gotMeta.Title, wantMeta.Title)
	}

	// Sidecar meta file must exist.
	if _, err := os.Stat(filepath.Join(dir, "tt1234567.meta.json")); err != nil {
		t.Errorf("expected meta sidecar to exist: %v", err)
	}
}

func TestRED_CaptionsCache_Miss(t *testing.T) {
	cache := &CaptionsCache{Dir: t.TempDir()}
	_, _, ok := cache.Get("tt0000000")
	if ok {
		t.Fatal("expected miss on empty cache, got hit")
	}
}

func TestRED_HandlePlaybackCaptions_ReturnsNoContentWhenNothingPlaying(t *testing.T) {
	// Build a Server with a stub PlexClient that returns no sessions.
	// Until the handler exists, this test fails at compile time.
	srv := newServerForCaptionsTest(t, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/playback/captions", nil)
	w := httptest.NewRecorder()
	srv.handlePlaybackCaptions(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 when no playback, got %d (body=%q)", w.Code, w.Body.String())
	}
}

// Helper: tests must be able to construct a *Server with stubbed deps.
// The agent's implementation is responsible for providing this constructor.
// If the signature differs, this helper is the place to adjust.
func newServerForCaptionsTest(t *testing.T, sessions []PlaybackSession, movies []Movie) *Server {
	t.Helper()
	return newCaptionsTestServer(sessions, movies, t.TempDir())
}
