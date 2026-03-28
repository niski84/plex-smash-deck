# Getting started

## Documentation map

Suggested reading order for a new install (same order as the **Help** tab menu):

- **1.** [Getting started](help:00-getting-started.md) (this page) — run, env, health check
- **2.** [Connecting your TV](help:connecting-your-tv.md) — LG / network / pairing
- **3.** [Playback and webOS](help:playback-and-webos.md) — how playback is sent, limitations
- **4.** [Dashboard — movie grid](help:01-dashboard-movies.md) — library, sort, hover, play
- **5.** [Discovery](help:02-discovery.md) — TMDB gaps vs your library
- **6.** [Snapshots, settings & troubleshooting](help:03-snapshots-settings-troubleshooting.md)
- **7.** [Background connectivity & health checks](help:04-background-health-checks.md) — periodic probes (Internet, Plex, TMDB, …) vs `/api/health`

On GitHub or in your editor, open the same files under **`docs/*.md`**.

---

**Plex Dashboard** (`plex-dashboard`) is a small, self-hosted web app that sits beside your **Plex** movie library. It gives you a fast local UI for browsing, discovery, playlists, and (optionally) sending playback to devices on your network—especially **LG webOS** TVs using the TV’s native player while Plex serves the files.

## Prerequisites

- **Plex** with a movie library you can reach from the machine running the dashboard.
- **Go 1.21+** if you build from source, or a [pre-built binary](https://github.com/niski84/plex-smash-deck/releases).
- A **[TMDB](https://www.themoviedb.org/settings/api) API key** for discovery, filmography, and poster art.
- **Radarr** (optional) and an **LG TV** on the same LAN (optional; only needed for those features).

Run the server from the **project root** (the directory that contains `web/plex-dashboard/`). The process loads static files relative to the current working directory, so starting it from somewhere else will break the UI.

## Configuration and `.env`

On startup the app loads **one** `.env` file, in this order:

1. The path in **`PLEX_DASHBOARD_ENV_FILE`** (if set and the file exists).
2. Otherwise, walking **upward** from the process **current working directory** looking for `.env`.
3. Otherwise, **`.env` next to the `plex-dashboard` binary**.

Values from `.env` are merged with anything already saved in **`data/plexdash-settings.json`** (saved fields fill gaps where the environment is empty). You can also change most options in the UI **Settings** tab and save—no restart required for those.

**Git worktrees** often have no `.env` in the worktree; symlink one in, or point `PLEX_DASHBOARD_ENV_FILE` at your main checkout’s file.

Environment variables the app actually reads (all optional unless noted—you need Plex and TMDB wired up for a useful experience):

| Variable | Role |
|----------|------|
| `PLEX_DASHBOARD_ENV_FILE` | Absolute path to `.env` when it isn’t found by the search above |
| `PORT` | HTTP listen port (**default `8081`**) |
| `PLEX_BASE_URL` | Plex server base URL (e.g. `http://192.168.1.10:32400`) |
| `PLEX_TOKEN` | Plex auth token |
| `PLEX_LIBRARY_KEY` | Movie library section key (**default `1`**) |
| `PLEX_TARGET_CLIENT_NAME` | Plex “player” name to target (**default `Living Room`**) |
| `PLEX_AUTO_TARGET_DETECTED` | Set to `1` to use auto-detected target (see Settings) |
| `APP_DISPLAY_NAME` | Label for the app (**default `plex-smash-deck`**) |
| `HERO_BANNER_URL`, `HERO_BANNER_HEIGHT`, `HERO_BANNER_HIDDEN` | Optional header banner |
| `TMDB_API_KEY` | TMDB API v3 key |
| `TMDB_READ_ACCESS_TOKEN` | TMDB read access token (v4) if you use it |
| `OMDB_API_KEY`, `OMDB_BLEND_RATINGS` | Optional OMDb ratings (`OMDB_BLEND_RATINGS=1` to blend with TMDB) |
| `RADARR_ENABLED`, `RADARR_URL`, `RADARR_API_KEY`, `RADARR_ROOT_FOLDER`, `RADARR_PROFILE_ID` | Optional Radarr integration |
| `LGTV_ADDR`, `LGTV_CLIENT_KEY` | LG webOS TV IP and pairing key for direct playback |
| `PLEXDASH_SNAPSHOT_DIR` | Absolute path for snapshot JSON (default `data/movie-snapshots` under cwd) |

If your repository includes **`.env.example`**, copy it to **`.env`** and edit; otherwise create **`.env`** yourself with the variables you need.

## Run the server

From the project root:

```bash
go run ./cmd/plex-dashboard
```

Or run a built binary the same way you would any other executable (again with cwd at the project root, or next to a valid `web/plex-dashboard` tree if you package it that way).

The default URL is **`http://localhost:8081`** (or **`http://127.0.0.1:8081`**) when `PORT` is unset.

### Sanity check

With the server up:

```bash
curl -sS http://127.0.0.1:8081/api/health
```

You should get JSON with `"success": true` and `"service": "plex-dashboard"`.

## First visit

1. Open the UI in your browser at `http://localhost:8081/` (adjust host/port if needed).
2. Click the **Settings** tab in the top bar. Enter **Plex** URL, token, and library key, plus **TMDB** (and anything else you use). Click **Save Settings**—settings persist under `data/plexdash-settings.json`.
3. For **TV connection**, pairing, and network quirks, see **[Connecting your TV](connecting-your-tv.md)**. For how playback is sent to LG webOS and what to expect, see **[Playback model and LG webOS limitations](playback-and-webos.md)**.

In-app **Help** lists Markdown files from **`docs/`** (this file included). Use **`help:filename.md`** links inside a doc to open another guide in the same tab.
