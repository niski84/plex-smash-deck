# Background connectivity checks (not `/api/health`)

This page describes the **periodic probes** the server runs so the UI can show **Settings → Connectivity**, the **header signal** summary, and **Dashboard** file-size coloring. It is **separate** from:

- **`GET /api/health`** — a tiny “is the HTTP server up?” check (no dependency probes).
- **Discovery `poll`** — job status for long-running TMDB analysis, not network health.

---

## How often things run

| What | Interval | Notes |
|------|----------|--------|
| **Lightweight probe bundle** | **~45 seconds** | Runs once at startup, then on a ticker. Implemented in `internal/plexdash/connectivity.go` as `connectivityProbeInterval`. |
| **Plex stream sample** (throughput) | **At most every ~4 minutes** | Rate-limited separately (`plexStreamProbeInterval`). Not every 45s tick performs a new download. |
| **Browser refresh** | **~12 seconds** | The SPA calls `GET /api/connectivity` on a timer so the UI updates; it **reads** the server’s last snapshot, it does not trigger probes. |

Each lightweight run uses a shared **~8 second** deadline for the HTTP/TCP checks below (plus the stream probe has its own **~22 second** HTTP timeout when it actually runs).

---

## Checks in each lightweight bundle

All of these are recorded as rows with a **level**: `ok`, `warn`, `error`, or `skip` (`skip` = not configured, so not tested).

| Check | What it does | `ok` means |
|--------|----------------|------------|
| **Internet** | TCP dial **1.1.1.1:443** (Cloudflare); if that fails, **8.8.8.8:53** (Google DNS). | At least one path reached the public internet. |
| **Plex server** | `GET …/identity` with your token (5s HTTP client timeout). | Plex returned a success-class status. **401** is **warn** (bad token). Other bad codes / network errors → **error**. |
| **TMDB API** | `GET /3/configuration` with your API key. | TMDB returned success. Missing key → **skip**. Reachability / key problems → **warn** (not configured as hard-fail for the whole app). |
| **OMDb API** | Optional: `GET` a tiny known-title request with your key. | Missing key → **skip**. Invalid key / API errors often surface as **warn** via OMDb’s JSON `Response` field. |
| **LG TV (SSAP)** | TLS dial to **`LGTV_ADDR:3001`** (self-signed cert allowed). | Port accepts TLS — the TV’s SSAP WebSocket endpoint is reachable. Missing IP → **skip**. |

**Overall summary** logic (high level): if **Internet** is **error**, the whole summary is **error**. Otherwise any **error** row wins; else **warn** rows (including a slow Plex stream sample) are rolled into a **warn** summary; else **ok**.

---

## Plex stream sample (throughput)

This is **not** full movie playback. The server picks **one** movie from **in-memory** library metadata (preferring a **smaller** `PartSize` when known to reduce disk work), builds a direct **media URL** for that file on Plex, and issues a **short ranged read** (up to **512 KiB**, at least **16 KiB** needed to score).

- **Purpose:** Estimate **Mb/s** from this machine → Plex for the **Dashboard** orange/red **file size** hints vs bitrate.
- **When it runs:** Only when Plex URL + token are set **and** the server already has movies loaded (e.g. after **Load / Refresh Movies**). If the library list is empty in memory, the probe **skips** with a message to refresh the Dashboard list.
- **Slow threshold:** Below **~12 Mb/s** the sample is still **ok** for connectivity but flagged **warn** with text that heavy titles may buffer (aligned with poster coloring).

---

## Where you see results

- **Settings → Connectivity** — full table, stream line, and **connectivity history** chart (history is **browser-local** only).
- **Dashboard header** — compact “signal” style summary driven by the same payload.
- **Movie cards** — file size color vs latest **Plex stream** `mbps` when available.

---

## Related docs

- [Getting started](help:00-getting-started.md) — `curl` **health** check vs this document.
- [Dashboard — movie grid](help:01-dashboard-movies.md) — file size coloring detail.
- [Snapshots, settings & troubleshooting](help:03-snapshots-settings-troubleshooting.md) — Settings groups including Connectivity.
