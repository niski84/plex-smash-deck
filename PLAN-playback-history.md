# PLAN — Playback History (delta on captions endpoint)

## Problem

The captions endpoint we just shipped (`/api/playback/captions`) only knows what's playing **right now**. But Nōgura processes voice sessions with a delay — by the time a session is processed, the movie may have ended or changed. We need to look up "what was playing at timestamp T", not "what's playing now".

## Design

### Plex-dashboard side: record + serve playback history

A background poller queries `PlexClient.ListPlaybackSessions()` every 30s and writes each active session to a JSONL log at `data/playback-history.jsonl`. Each line is one snapshot:

```json
{
  "ts": "2026-04-26T15:30:00Z",
  "rating_key": "12345",
  "imdb_id": "tt9999999",
  "tmdb_id": 67890,
  "title": "The Matrix",
  "year": 1999,
  "player_name": "LG OLED",
  "player_machine_id": "abc123",
  "view_offset_ms": 1234567,
  "duration_ms": 8160000
}
```

Empty polls (nothing playing) are NOT written.

On startup, the in-memory ring loads the last 24h of entries from the JSONL (stops parsing once an entry's `ts` is older than 24h). New entries are appended to the file AND added to the ring. The ring caps at 5000 entries (~ 41 hours of busy 30s polling).

### LookupAt algorithm

`LookupAt(t time.Time, player string) (PlaybackSnapshot, bool)` — note: the existing `Snapshot` type in `snapshots.go` is unrelated, so use `PlaybackSnapshot` to avoid collision:
1. Filter ring entries to those within `[t-2min, t+2min]`. (Generous window to handle poll cadence.)
2. If `player != ""`, restrict to entries whose `player_name` contains `player` case-insensitively.
3. If multiple matches, pick the one with smallest `|entry.ts - t|`.
4. If no matches at all in `±2min`, fall back to: entries with `ts <= t` (most recent before t) where `t` is plausibly within the movie's runtime — i.e. `(t - entry.ts).Seconds() < (entry.duration_ms - entry.view_offset_ms)/1000`. This catches the case where a movie was 30 minutes in at the last poll and the session is 10 minutes after that poll.
5. If still no match, return `false`.

### New endpoints

```
GET /api/playback/at?time=<rfc3339>[&player=<name>]
    → 200 { rating_key, imdb_id, title, player_name, view_offset_ms, duration_ms, snapshot_at }
    → 204 if no movie was playing at that time

GET /api/playback/history?since=<rfc3339>&until=<rfc3339>[&player=<name>]
    → 200 { snapshots: [...], count: N }
```

### Modify existing endpoint

`GET /api/playback/captions` accepts new optional query param `at=<rfc3339>`:
- If `at` is supplied AND no `ratingKey`/`player` resolution succeeded → use `LookupAt(at, "")` to find the right ratingKey.
- If `at` AND `player` are supplied → use `LookupAt(at, player)` (skips the live `ListPlaybackSessions` path).
- If `at` is missing → existing live-lookup behaviour (unchanged).

### Files to create (plex-dashboard)

| File | Purpose |
|---|---|
| `internal/plexdash/playback_history.go` | `PlaybackHistory`, `PlaybackSnapshot`, `LookupAt`, `Append`, JSONL load/persist, background `Start()` poller |
| `internal/plexdash/playback_history_test.go` | 6 tests (see below) |
| `internal/plexdash/playback_at_handler.go` | `handlePlaybackAt`, `handlePlaybackHistory` |

### Files to modify (plex-dashboard)

| File | Change |
|---|---|
| `internal/plexdash/server.go` | Construct `PlaybackHistory` in `NewServer`, call `Start(ctx)` from a startup hook (or in `NewServer` if there's no separate startup), register `/api/playback/at` and `/api/playback/history` |
| `internal/plexdash/captions_handler.go` | Accept `?at=` param, route to `PlaybackHistory.LookupAt` when present |

### Test cases (plex-dashboard)

| ID | Test | Expected |
|---|---|---|
| H1 | `TestPlaybackHistory_AppendAndLookupAt_ExactWindow` | Append 3 snapshots at t-60s, t, t+60s; LookupAt(t, "") returns the middle one | RED→GREEN |
| H2 | `TestPlaybackHistory_LookupAt_FilterByPlayer` | Two entries at same time, different players; LookupAt(t, "lg") returns the LG one | RED→GREEN |
| H3 | `TestPlaybackHistory_LookupAt_NoEntries` | Empty history → returns ok=false | RED→GREEN |
| H4 | `TestPlaybackHistory_LookupAt_FallbackToRuntimeWindow` | Last entry at t-10min with viewOffset=20min, duration=2h → LookupAt(t, "") still returns it (within runtime) | RED→GREEN |
| H5 | `TestPlaybackHistory_PersistAndReload` | Append entries, write to JSONL, create new instance pointing at same file → ring contains the entries | RED→GREEN |
| H6 | `TestHandlePlaybackAt_ReturnsSnapshot` | Pre-loaded history, GET /api/playback/at?time=<ts> → 200 with the right rating_key | RED→GREEN |
| H7 | `TestCaptionsHandler_AcceptsAtParam` | History contains a movie, captions cache is pre-warmed for that imdbId, GET /api/playback/captions?at=<ts> → 200 with cached text | RED→GREEN |

---

## Nogura side: per-clip captions cache (separate change, same feature)

### Files to modify (nogura)

| File | Change |
|---|---|
| `internal/pipeline/tv_captions_client.go` | Stop the always-on poller. Add `CaptionsForTime(ctx, t time.Time) (grams map[string]struct{}, title, imdbID string, ok bool)` that calls plex-dashboard `/api/playback/captions?at=<rfc3339>`. Maintain in-memory cache keyed by `imdbID` so repeated clips from the same movie don't refetch. Eviction: drop entries older than 4h. |
| `internal/pipeline/tv_chat_filter.go` | The stage's `Process` reads the clip's recorded timestamp (use `clip.CapturedAt` if it exists; otherwise fall back to `time.Now()` and log once). Calls `client.CaptionsForTime(ctx, capturedAt)` instead of `client.Snapshot()`. |

### Interface contract change

The `CaptionsSnapshotter` interface evolves:

```go
type CaptionsSnapshotter interface {
    // CaptionsForTime returns the trigram set + movie title for whatever was
    // playing at the supplied time. If no movie was playing or fetch fails,
    // returns (nil, "", "", false). Must NEVER return an error.
    CaptionsForTime(ctx context.Context, at time.Time) (grams map[string]struct{}, title string, imdbID string, ok bool)
}
```

This is a breaking change to the interface, but the only implementer is `PlexCaptionsClient` and the only consumer is `TVChatFilterStage` — both ours, both updated atomically.

### Tests to update / add (nogura)

| ID | Test | Expected |
|---|---|---|
| N1 | `TestTVChatFilter_UsesClipTimestamp` | Stub returns different captions for different timestamps; clip with capturedAt=T1 gets T1's captions | RED→GREEN |
| N2 | `TestPlexCaptionsClient_CaptionsForTime_CacheHit` | First call to CaptionsForTime hits HTTP; second call for same imdbID skips HTTP, returns cached grams | RED→GREEN |
| N3 | `TestPlexCaptionsClient_CaptionsForTime_204` | Server returns 204 → ok=false, no panic | RED→GREEN |
| N4 | `TestPlexCaptionsClient_CaptionsForTime_CacheEviction` | Insert entry with timestamp 5h ago → next call refetches | RED→GREEN |

### How `clip.CapturedAt` is populated

If `AudioClip` doesn't currently have a `CapturedAt time.Time` field, the implementing agent adds one. Set by the streamer (audio_channel.go) when the clip is constructed: `CapturedAt: time.Now()`. Default zero-value is treated as "use time.Now() at processing time" with a one-time log warning.

## Constraints

- **Plex-dashboard:** poll interval 30s. Each poll has 5s context timeout. Poller respects `ctx.Done()`. JSONL writes use atomic append (just `os.OpenFile` with `O_APPEND|O_WRONLY` + a single `Write`).
- **Nogura:** captions cache thread-safe (`sync.RWMutex`). Cache TTL 4h. Per-imdbId cache means a 2h movie scrubs once per session.
- **Fail-open** everywhere on the nogura side (no captions → no drops).
- **No new go.mod deps in either repo.**
- Reload commands:
  - `POST http://localhost:8100/api/reload?project=plex-dashboard`
  - `POST http://localhost:8100/api/reload?project=²nd-whisper-brain`

## Done when

- Plex-dashboard: `go test ./internal/plexdash/ -run "TestPlaybackHistory|TestHandlePlaybackAt|TestCaptionsHandler_AcceptsAtParam" -v` all pass.
- Nogura: `go test ./internal/pipeline/ -run "TestTVChatFilter_UsesClipTimestamp|TestPlexCaptionsClient_CaptionsForTime" -v` all pass.
- Both servers reload cleanly. `curl 'http://localhost:8081/api/playback/at?time=2026-04-26T12:00:00Z'` returns 200 or 204 (not 500, not 404 from router).
- After ~1 minute of uptime, `data/playback-history.jsonl` exists and (if a movie was playing) has at least one entry.
