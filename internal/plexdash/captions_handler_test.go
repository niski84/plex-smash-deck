package plexdash

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── stubs ────────────────────────────────────────────────────────────────────

type stubPlexClient struct {
	sessions []PlaybackSession
	err      error
}

func (s *stubPlexClient) ListPlaybackSessions(ctx context.Context) ([]PlaybackSession, error) {
	return s.sessions, s.err
}

type stubFetcher struct {
	available bool
	srt       []byte
	err       error
	calls     int
}

func (s *stubFetcher) Available() bool { return s.available }
func (s *stubFetcher) Fetch(ctx context.Context, imdbID string) ([]byte, error) {
	s.calls++
	return s.srt, s.err
}

// newCaptionsTestServer matches the helper signature used by the RED test in
// captions_redtest_test.go. dir is used as the cache root so disk IO stays
// inside the test sandbox.
func newCaptionsTestServer(sessions []PlaybackSession, movies []Movie, dir string) *Server {
	cfg := Config{LibraryKey: "1"}
	s := &Server{
		cfg:                cfg,
		settingsPath:       filepath.Join(dir, "settings.json"),
		discJobs:           newDiscoveryJobStore(),
		streamDownloads:    make(map[string]*streamDownload),
		captionsCache:      NewCaptionsCache(filepath.Join(dir, "captions-cache")),
		captionFetcher:     &stubFetcher{},
		captionsPlexClient: &stubPlexClient{sessions: sessions},
	}
	s.mlMu.Lock()
	s.mlMovies = movies
	s.mlCachedAt = time.Now()
	s.mlKey = cfg.LibraryKey
	s.mlMu.Unlock()
	return s
}

// newCaptionsHandlerTestServer builds a *Server pre-loaded with one fake movie in the
// in-memory library and the given stubs wired up.
func newCaptionsHandlerTestServer(t *testing.T, movies []Movie, plex captionsPlexClient, fetcher CaptionFetcher) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{LibraryKey: "1"}
	s := &Server{
		cfg:                cfg,
		settingsPath:       filepath.Join(dir, "settings.json"),
		discJobs:           newDiscoveryJobStore(),
		streamDownloads:    make(map[string]*streamDownload),
		captionsCache:      NewCaptionsCache(filepath.Join(dir, "captions-cache")),
		captionFetcher:     fetcher,
		captionsPlexClient: plex,
	}
	// Seed the in-memory movie cache so cachedListMovies returns immediately.
	s.mlMu.Lock()
	s.mlMovies = movies
	s.mlCachedAt = time.Now()
	s.mlKey = cfg.LibraryKey
	s.mlMu.Unlock()
	return s
}

func decodeCaptionsBody(t *testing.T, body []byte) captionsResponse {
	t.Helper()
	var resp captionsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode body: %v\nbody: %s", err, string(body))
	}
	return resp
}

// ── tests ────────────────────────────────────────────────────────────────────

func TestHandlePlaybackCaptions_NoSession(t *testing.T) {
	s := newCaptionsHandlerTestServer(t, nil, &stubPlexClient{}, &stubFetcher{})
	req := httptest.NewRequest(http.MethodGet, "/api/playback/captions", nil)
	rec := httptest.NewRecorder()
	s.handlePlaybackCaptions(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204\nbody: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePlaybackCaptions_CacheHit(t *testing.T) {
	movies := []Movie{{RatingKey: "42", Title: "The Matrix", Year: 1999, IMDbID: "tt0133093"}}
	s := newCaptionsHandlerTestServer(t, movies, &stubPlexClient{}, &stubFetcher{})
	if err := s.captionsCache.Put("tt0133093", "cached captions text",
		CaptionsMeta{Title: "The Matrix", Year: 1999, Source: "opensubtitles", FetchedAt: "2026-01-01T00:00:00Z", ByteCount: 20}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/playback/captions?ratingKey=42", nil)
	rec := httptest.NewRecorder()
	s.handlePlaybackCaptions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200\nbody: %s", rec.Code, rec.Body.String())
	}
	resp := decodeCaptionsBody(t, rec.Body.Bytes())
	if resp.Source != "cache" {
		t.Fatalf("source: got %q want cache", resp.Source)
	}
	if resp.Captions != "cached captions text" {
		t.Fatalf("captions: got %q", resp.Captions)
	}
	if resp.IMDbID != "tt0133093" {
		t.Fatalf("imdbId: got %q", resp.IMDbID)
	}
	if resp.Title != "The Matrix" || resp.Year != 1999 {
		t.Fatalf("title/year: got %q/%d", resp.Title, resp.Year)
	}
}

func TestHandlePlaybackCaptions_LocalSrtFallback(t *testing.T) {
	movies := []Movie{{RatingKey: "7", Title: "Local Hero", Year: 1983, IMDbID: "tt0085859"}}
	fetcher := &stubFetcher{available: true, srt: []byte("should not be used")}
	s := newCaptionsHandlerTestServer(t, movies, &stubPlexClient{}, fetcher)

	// Stage a local SRT in cwd/data/subtitles. Use a temp cwd so we don't
	// pollute the repo's data/ directory.
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	if err := os.MkdirAll(filepath.Join(tmpWD, "data", "subtitles"), 0o755); err != nil {
		t.Fatalf("mkdir local subs: %v", err)
	}
	srt := []byte("1\n00:00:01,000 --> 00:00:03,000\nHello world\n")
	if err := os.WriteFile(filepath.Join(tmpWD, "data", "subtitles", "tt0085859.srt"), srt, 0o644); err != nil {
		t.Fatalf("write local srt: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/playback/captions?ratingKey=7", nil)
	rec := httptest.NewRecorder()
	s.handlePlaybackCaptions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200\nbody: %s", rec.Code, rec.Body.String())
	}
	resp := decodeCaptionsBody(t, rec.Body.Bytes())
	if resp.Source != "local" {
		t.Fatalf("source: got %q want local", resp.Source)
	}
	if !strings.Contains(resp.Captions, "Hello world") {
		t.Fatalf("captions: got %q", resp.Captions)
	}
	if fetcher.calls != 0 {
		t.Fatalf("fetcher should not be called when local SRT present, calls=%d", fetcher.calls)
	}
	// Cache should now contain the parsed text for next time.
	if got, _, ok := s.captionsCache.Get("tt0085859"); !ok || got == "" {
		t.Fatal("expected cache to be populated after local SRT path")
	}
}

func TestHandlePlaybackCaptions_OpenSubtitlesFetch(t *testing.T) {
	movies := []Movie{{RatingKey: "9", Title: "Predator", Year: 1987, IMDbID: "tt0093773"}}
	srt := []byte("1\n00:00:01,000 --> 00:00:03,000\n<i>Get to the chopper!</i>\n")
	fetcher := &stubFetcher{available: true, srt: srt}
	s := newCaptionsHandlerTestServer(t, movies, &stubPlexClient{}, fetcher)

	req := httptest.NewRequest(http.MethodGet, "/api/playback/captions?ratingKey=9", nil)
	rec := httptest.NewRecorder()
	s.handlePlaybackCaptions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200\nbody: %s", rec.Code, rec.Body.String())
	}
	resp := decodeCaptionsBody(t, rec.Body.Bytes())
	if resp.Source != "opensubtitles" {
		t.Fatalf("source: got %q want opensubtitles", resp.Source)
	}
	if !strings.Contains(resp.Captions, "Get to the chopper!") {
		t.Fatalf("captions: got %q", resp.Captions)
	}
	if fetcher.calls != 1 {
		t.Fatalf("fetcher.calls: got %d want 1", fetcher.calls)
	}
	if got, _, ok := s.captionsCache.Get("tt0093773"); !ok || got == "" {
		t.Fatal("expected cache populated after opensubtitles fetch")
	}
}

func TestHandlePlaybackCaptions_FetcherUnavailable_NoSrt(t *testing.T) {
	movies := []Movie{{RatingKey: "1", Title: "Nobody", Year: 2021, IMDbID: "tt7888964"}}
	fetcher := &stubFetcher{available: false}
	s := newCaptionsHandlerTestServer(t, movies, &stubPlexClient{}, fetcher)

	// chdir to an empty tmp so there is no data/subtitles tree at all.
	prevWD, _ := os.Getwd()
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	req := httptest.NewRequest(http.MethodGet, "/api/playback/captions?ratingKey=1", nil)
	rec := httptest.NewRecorder()
	s.handlePlaybackCaptions(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404\nbody: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePlaybackCaptions_ResolveByPlayerName(t *testing.T) {
	movies := []Movie{
		{RatingKey: "100", Title: "First Movie", Year: 2010, IMDbID: "tt0000100"},
		{RatingKey: "200", Title: "Second Movie", Year: 2015, IMDbID: "tt0000200"},
	}
	plex := &stubPlexClient{
		sessions: []PlaybackSession{
			{RatingKey: "100", PlayerName: "Living Room Roku", Type: "movie"},
			{RatingKey: "200", PlayerName: "LG OLED C2", Type: "movie"},
		},
	}
	srt := []byte("1\n00:00:01,000 --> 00:00:03,000\nLG line\n")
	fetcher := &stubFetcher{available: true, srt: srt}
	s := newCaptionsHandlerTestServer(t, movies, plex, fetcher)

	req := httptest.NewRequest(http.MethodGet, "/api/playback/captions?player=lg", nil)
	rec := httptest.NewRecorder()
	s.handlePlaybackCaptions(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want 200\nbody: %s", rec.Code, rec.Body.String())
	}
	resp := decodeCaptionsBody(t, rec.Body.Bytes())
	if resp.RatingKey != "200" {
		t.Fatalf("ratingKey: got %q want 200 (LG match)", resp.RatingKey)
	}
	if resp.IMDbID != "tt0000200" {
		t.Fatalf("imdbId: got %q want tt0000200", resp.IMDbID)
	}
	if !strings.EqualFold(resp.PlayerName, "LG OLED C2") {
		t.Fatalf("playerName: got %q", resp.PlayerName)
	}
}

func TestHandlePlaybackCaptions_NoIMDb(t *testing.T) {
	movies := []Movie{{RatingKey: "5", Title: "Indie Flick", Year: 2020}} // IMDbID empty
	s := newCaptionsHandlerTestServer(t, movies, &stubPlexClient{}, &stubFetcher{available: true})
	req := httptest.NewRequest(http.MethodGet, "/api/playback/captions?ratingKey=5", nil)
	rec := httptest.NewRecorder()
	s.handlePlaybackCaptions(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404\nbody: %s", rec.Code, rec.Body.String())
	}
}

func TestHandlePlaybackCaptions_FetcherError404(t *testing.T) {
	movies := []Movie{{RatingKey: "9", Title: "Obscure", Year: 1990, IMDbID: "tt0000999"}}
	fetcher := &stubFetcher{available: true, err: errors.New("boom")}
	s := newCaptionsHandlerTestServer(t, movies, &stubPlexClient{}, fetcher)

	prevWD, _ := os.Getwd()
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevWD) })

	req := httptest.NewRequest(http.MethodGet, "/api/playback/captions?ratingKey=9", nil)
	rec := httptest.NewRecorder()
	s.handlePlaybackCaptions(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status: got %d want 404 (fetcher error must not 500), body: %s", rec.Code, rec.Body.String())
	}
}
