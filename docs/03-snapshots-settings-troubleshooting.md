# Snapshots, settings, playlists, and troubleshooting

**New here?** See [Getting started](help:00-getting-started.md).

For **LG TV setup, SSAP, and remote playback paths**, see [Connecting your TV](help:connecting-your-tv.md) and [Playback and webOS](help:playback-and-webos.md).

---

## Snapshots tab

Snapshots record **which movie titles (and years)** were in your Plex movie library at a point in time. They are stored on disk under **`data/movie-snapshots/`** by default. To share snapshot history across git worktrees, set env **`PLEXDASH_SNAPSHOT_DIR`** to an absolute path (see `internal/plexdash/snapshots.go`).

- **Take Snapshot** — Captures the current library list from the server’s Plex-backed view.
- **Refresh View** — Reloads snapshot metadata from disk without taking a new snapshot.
- **Latest Drop** — After **at least two** snapshots, compares the **newest** snapshot to the **previous** one: **New Movies** vs **Went Missing**.
- **Missing banner** — Shown when titles disappear between snapshots, grouped to highlight sustained gaps.
- **Snapshot History** — Table of all snapshots with **Change vs Prev** and **View Changes** (diff vs the prior snapshot).
- **Compare any two** — Pick **From** / **To** snapshots and **Compare**; the result panel lists **Added** and **Removed** between those two captures. **Pattern Analysis** may appear when the diff suggests batch-style adds (e.g. many new titles at once).
- **Daily auto-snapshot** — Configured on **Settings** (see below): one automatic snapshot per day, **skipped silently** if the movie count is unchanged from the previous snapshot. Schedule uses **UTC**.

---

## Settings tab

Environment variables load first; **Save Settings** persists values into **`data/plexdash-settings.json`** (merged with env on startup). LG TV variables (**`LGTV_*`**) are **not** on this form—use **`.env`** or the environment; see the TV docs linked above.

Groups in the UI:

| Group | Purpose |
|--------|---------|
| **Plex** | Base URL, token, movie library key, default target player, optional “prefer detected player”. |
| **Branding** | App header name, optional hero banner URL / height, hide banner. |
| **TMDB** | API key (v3) and optional read token field. |
| **OMDb** | Optional key; **Blend OMDb + TMDB** affects Discovery ratings when enabled. Dashboard hover can show extra source pills when OMDb data exists. |
| **Connectivity** | Read-only status: internet, Plex, TMDB, LG checks, Plex sample throughput. **Connectivity history** is stored **in the browser only** (local chart). For what runs in the background and how often, see **[Background connectivity & health checks](help:04-background-health-checks.md)**. |
| **Dashboard genre bar** | Default pinned/hidden/excluded genre lists; **Apply** / **Reset** (browser-local bar behavior as described in the Settings copy). |
| **Radarr** | Enable integration, URL, API key, root folder, profile ID. |
| **Snapshot Schedule** | Enable/disable daily auto-snapshot and **hour (UTC)**. |

**Note:** The client always sends a fixed **`Port`** field (`8081`) in the save payload for API compatibility; your server’s real listen port is whatever you configure when you start it (e.g. **`PORT`** env, default **8081** in code).

---

## Playlists (Dashboard tab)

There is **no separate Playlists tab**. Playlist-related controls live on the **Dashboard** inside **Library** cards:

- **Play Playlist** — Refresh list from Plex, pick a playlist, preview items, **Play Selected** (see in-UI tooltip about webOS / Plex playlist behavior).
- **Create Playlist by Actor/Director** — Build a playlist from credits vs your library.
- **Create Playlist by Genre & Rating** — Filtered create flow with year/rating constraints and preview (as shown in the UI).

Discovery may show an **In playlist** column when the backend supplies that for a given flow.

---

## Troubleshooting (quick)

| Symptom | Things to check |
|--------|------------------|
| Empty library / Load Movies fails | Plex URL, token, library key; server logs; **Settings → Save** so config persists. |
| Discovery never finishes | **TMDB** key; open browser devtools Network for **`/api/discovery/poll`**; try **Clear TMDB cache** on Discovery. |
| OMDb pills missing on hover | Title needs TMDB id from Plex; **OMDb** key and optional blend only affect Discovery sorting—hover still needs key for OMDb fetch. |
| Playback wrong device / no start | **Settings** default player vs Dashboard **Client** dropdown; **WebOS direct** vs **Plex companion** transport; TV docs above. |
| Radarr add disabled or errors | **Use Radarr integration** checked; URL, API key, root, profile; server must reach Radarr. |
| Snapshots empty or stale | **Take Snapshot** at least once; **`PLEXDASH_SNAPSHOT_DIR`** if you switched worktrees; disk space under `data/movie-snapshots`. |
| Port confusion | UI save payload vs actual **`PORT`** / how you launch the binary—see note above. |

For deeper TV and Plex-remote behavior, use **[playback-and-webos.md](playback-and-webos.md)**.

---

## Related docs

- **[00-getting-started.md](00-getting-started.md)** — Run, env, health check.  
- **[01-dashboard-movies.md](01-dashboard-movies.md)** — Grid, sort, hover, play.  
- **[02-discovery.md](02-discovery.md)** — Discovery jobs, cache, cart.  
