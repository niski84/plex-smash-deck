# PLAN — TV Captions Endpoint for plex-dashboard

## Problem

Nōgura (`²nd-whisper-brain`, port 8190) records voice but its mic picks up TV/movie audio bleeding from the room. We need plex-dashboard to expose the captions of the currently-playing movie so Nōgura can fingerprint and filter out TV chatter from real human speech.

## Design

### New endpoint

```
GET /api/playback/captions[?player=<name>][&ratingKey=<key>]
```

**Resolution order:**
1. If `ratingKey` is supplied → use it directly (skip playback lookup).
2. Else if `player` is supplied → call `PlexClient.ListPlaybackSessions()`, find the session whose `PlayerName` (case-insensitive) contains the value, use its `RatingKey`.
3. Else → call `ListPlaybackSessions()`, pick any active video session. If multiple, pick the first.
4. If no session and no `ratingKey` → return `204 No Content`.

**Once a `ratingKey` is resolved:**
1. Look up the movie in the cached library (`s.cachedListMovies()`) by `RatingKey` → get `IMDbID`, `TMDBID`, `Title`, `Year`.
2. If no `IMDbID` and no manual override → `404 captions: movie has no IMDB id`.
3. Try caption sources in order:
   - **Cache** (`data/captions-cache/<imdbId>.txt`) — if present, return immediately with `source: "cache"`.
   - **Local file** (`data/subtitles/<imdbId>.srt`) — parse SRT to plain text, copy to cache, return with `source: "local"`.
   - **OpenSubtitles** — if `OPENSUBTITLES_API_KEY` is set, fetch via API, write to cache, return with `source: "opensubtitles"`.
4. If all sources fail → `404 captions: not available`.

### Response shape

```json
{
  "ratingKey": "12345",
  "imdbId": "tt9999999",
  "tmdbId": 67890,
  "title": "The Matrix",
  "year": 1999,
  "playerName": "LG OLED",
  "captions": "Wake up Neo... The matrix has you... Follow the white rabbit...",
  "source": "opensubtitles",
  "fetchedAt": "2026-04-26T15:30:00Z",
  "byteCount": 84621
}
```

`captions` is plain text — line breaks separate caption cues — no SRT timestamps, no HTML tags, no formatting.

### OpenSubtitles client

REST API v1 (`https://api.opensubtitles.com/api/v1`):
1. `POST /login` with `{username, password}` → returns `token`. Cache token for 24h in memory.
2. `GET /subtitles?imdb_id=NNNNNN&languages=en` (use the IMDB id WITHOUT the `tt` prefix) → returns array of subtitle metadata; pick the first (highest rated by API default sort).
3. `POST /download` with `{file_id}` → returns a temporary download URL.
4. `GET <url>` → returns the SRT bytes.

Required env:
- `OPENSUBTITLES_API_KEY` — header `Api-Key: <key>`
- `OPENSUBTITLES_USERNAME`
- `OPENSUBTITLES_PASSWORD`
- `OPENSUBTITLES_USER_AGENT` (default `plex-dashboard/0.1`)

If any of the three credentials is missing → `OpenSubtitlesClient.Available()` returns false, fetch path is skipped silently.

### SRT → plain text parser

Strip:
- Numeric cue indices (lines that are just digits)
- Timestamp lines (matching `\d{2}:\d{2}:\d{2},\d{3} --> \d{2}:\d{2}:\d{2},\d{3}`)
- HTML tags (`<i>`, `<b>`, `<font ...>` etc — use a regex `<[^>]+>`)
- Music symbols (♪, [music], (music))
- Speaker labels at start of line (`JOHN: hello` → `hello`) — match `^[A-Z][A-Z\s]{1,15}:\s`
- Empty lines

Join cues with `\n`. Collapse runs of whitespace inside cues to single spaces.

### Disk cache

- Dir: `data/captions-cache/`
- File: `<imdbId>.txt` (already plain text)
- Sidecar JSON: `<imdbId>.meta.json` with `{title, year, source, fetchedAt, byteCount}`
- No TTL — captions don't change. Manual delete to refresh.
- Atomic write: write to `.tmp`, then rename.

## Files to create

| File | Purpose |
|---|---|
| `internal/plexdash/captions_handler.go` | HTTP handler `handlePlaybackCaptions` |
| `internal/plexdash/captions_cache.go` | Disk cache read/write + SRT-to-text parser |
| `internal/plexdash/opensubtitles_client.go` | OpenSubtitles REST v1 wrapper |
| `internal/plexdash/captions_handler_test.go` | Unit tests (table-driven, with stubbed PlexClient + OpenSubtitlesClient) |
| `internal/plexdash/captions_cache_test.go` | Cache + SRT parser tests |

## Files to modify

| File | Change |
|---|---|
| `internal/plexdash/config.go` | Add `OpenSubtitlesAPIKey`, `OpenSubtitlesUsername`, `OpenSubtitlesPassword`, `OpenSubtitlesUserAgent` env reads. |
| `internal/plexdash/server.go` | Register `/api/playback/captions` route in `Routes()` (must register BEFORE catch-all wildcards). Construct `OpenSubtitlesClient` in `NewServer`. |

## Interfaces (for testability)

```go
// In captions_handler.go — define an interface the handler depends on, so
// tests can stub it without spinning up real HTTP.
type CaptionFetcher interface {
    Available() bool
    Fetch(ctx context.Context, imdbID string) (srtBytes []byte, err error)
}

// OpenSubtitlesClient implements CaptionFetcher.

// In captions_cache.go:
type CaptionsCache struct {
    Dir string // e.g. "data/captions-cache"
}
func (c *CaptionsCache) Get(imdbID string) (text string, meta CaptionsMeta, ok bool)
func (c *CaptionsCache) Put(imdbID string, text string, meta CaptionsMeta) error

// SRT parser is a free function:
func ParseSRTToPlainText(srt []byte) string
```

The handler signature:

```go
func (s *Server) handlePlaybackCaptions(w http.ResponseWriter, r *http.Request) {
    // 1. Resolve ratingKey from query / playback sessions.
    // 2. Look up movie in cached library.
    // 3. cache.Get() — if hit, write JSON response, return.
    // 4. Try local file path data/subtitles/<imdbId>.srt — if exists, parse, cache, return.
    // 5. fetcher.Fetch() — if hit, parse, cache, return.
    // 6. 404.
}
```

## Step-by-step implementation

1. Add env reads to `config.go` (near the existing OPENSUBTITLES-adjacent pattern). Keep them grouped at the bottom.
2. Create `captions_cache.go` with `CaptionsCache`, `CaptionsMeta`, and `ParseSRTToPlainText`.
3. Create `opensubtitles_client.go` with `OpenSubtitlesClient` implementing `CaptionFetcher`. Cache the auth token in-struct with a `tokenExpiresAt`. On 401, force re-login.
4. Create `captions_handler.go` with `handlePlaybackCaptions`.
5. Register route in `server.go` `Routes()` — search for the existing `mux.HandleFunc("/api/movies/resolve", ...)` line and add immediately above it.
6. Construct cache + client in `NewServer` and store on `*Server` so the handler can reach them.

## Test cases

| ID | Test | Expected | Status |
|---|---|---|---|
| T1 | `TestHandlePlaybackCaptions_NoSession` | When `ListPlaybackSessions` returns empty and no `ratingKey` query → status 204 | RED → GREEN |
| T2 | `TestHandlePlaybackCaptions_CacheHit` | When cache file exists for resolved imdbId → returns 200 with `source:"cache"` and the cached text body | RED → GREEN |
| T3 | `TestHandlePlaybackCaptions_LocalSrtFallback` | When `data/subtitles/<imdbId>.srt` exists → parses, caches, returns `source:"local"` | RED → GREEN |
| T4 | `TestHandlePlaybackCaptions_OpenSubtitlesFetch` | When fetcher returns SRT bytes → parses, caches, returns `source:"opensubtitles"` | RED → GREEN |
| T5 | `TestHandlePlaybackCaptions_FetcherUnavailable_NoSrt` | When fetcher.Available()=false and no local srt → 404 | RED → GREEN |
| T6 | `TestParseSRTToPlainText` | Input with cue numbers, timestamps, `<i>` tags, music symbols → plain text only | RED → GREEN |
| T7 | `TestCaptionsCache_RoundTrip` | Put then Get returns same text + meta | RED → GREEN |
| T8 | `TestHandlePlaybackCaptions_ResolveByPlayerName` | Two playback sessions, `?player=lg` matches the LG one (case-insensitive substring) | RED → GREEN |

Use stubs for `PlexClient` and `CaptionFetcher` — no real HTTP in tests.

For the handler tests, build a small stub PlexClient interface — just the methods the handler needs. If the handler currently takes `*PlexClient` directly, refactor to take a small local interface defined in the handler file.

## Constraints

- **No new dependencies in go.mod beyond stdlib + what's already there.** SRT parsing is regex-based, OpenSubtitles is plain `net/http`.
- All disk writes atomic (temp + rename).
- All external HTTP calls have a 15s timeout via `context.WithTimeout`.
- All errors that prevent caption return are logged with `log.Printf("[captions] ...")` but NEVER cause a 500 — return 204/404 instead.
- Reload after change: `POST http://localhost:8100/api/reload?project=plex-dashboard`

## Done when

- All 8 tests pass: `go test ./internal/plexdash/ -run TestHandlePlaybackCaptions -v` and `go test ./internal/plexdash/ -run TestParseSRT -v` and `go test ./internal/plexdash/ -run TestCaptionsCache -v`
- `go build ./...` clean
- Manual smoke: `curl -s 'http://localhost:8081/api/playback/captions?ratingKey=<known-key>' | jq` returns 204/404/200 sensibly
