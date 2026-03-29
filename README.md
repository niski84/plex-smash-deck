# Plex Dashboard

I kept running into out-of-memory crashes on the Plex app, so I wrote my own thing. The core idea is simple: stream movies directly to the LG webOS native player over the local network — no Plex client on the TV, no middleman app eating RAM.

It turned into something a bit bigger than that.

![Plex Dashboard demo](docs/demo.gif)

<!-- readme-gallery:start -->

## Screenshots

This section is generated automatically. **Dashboard grids:** add files under [`images/`](images/) and list them in [`images/manifest.json`](images/manifest.json). **Doc images:** place files under [`docs/images/`](docs/images/) with a sidecar `{name}.meta.json` next to each `{name}.png` (see [`docs/images/image.meta.json`](docs/images/image.meta.json)). Run:

```bash
./scripts/generate-readme-images.sh
```

![Dashboard: search, Recently added to Plex sort, genre chips, and movie cards](images/dashboard-movie-grid.png)

![Dashboard: sort dropdown, decade filter, genre tags, Select All, Playback path, Play Selected, and cards with file-size coloring](images/dashboard-sort-controls.png)

![Dashboard hover: blended TMDB and OMDb ratings with overall average](docs/images/image.png)

After the hover delay, the panel merges **TMDB vote average** with **OMDb** (IMDb, Rotten Tomatoes, Metacritic) into one **Avg ★** line and shows per-source pills so you can see both the combined score and each provider at a glance.

<!-- readme-gallery:end -->

## What it does

**Plays movies on your LG TV** using the SSAP WebSocket protocol built into every modern LG webOS TV. Select one movie or build a multi-select playlist and push it straight over. The TV opens its own media player; Plex is just the file server.

**Generates random playlists** so you get the experience of your own always-on channel. It weights toward movies you've watched less, so you're not cycling through the same titles. Think of it as a personal shuffle that actually shuffles.

**Finds movies you're missing.** Search by actor, co-actor, director, or production studio (A24, Criterion, Pixar — whatever you're into) and see what's in their catalog that isn't in your library. If you have [Radarr](https://radarr.video) configured, you can add any missing title with one click.

**Tracks your library over time** with daily snapshots that diff against the previous day. It detects themes in newly added movies — recurring directors, actors, studios, genres, decades — and surfaces them in a patterns view. No snapshot is saved if nothing changed.

**Fast search across your whole library.** Title, actor, director, or studio — all rendered up front, no DOM rebuilding. Full-resolution poster art and a hover popup with plot, rating, runtime, and genres.

## What you'll need

- A **Plex** server with a movie library
- A **free [TMDB API account](https://www.themoviedb.org/settings/api)** — used for discovery, filmography lookups, and poster art
- An **LG Smart TV** on the same local network (for direct playback; everything else works without it)
- [Radarr](https://radarr.video) (optional, for one-click movie adds)
- Go 1.22+ to build from source, or grab a [pre-built binary](https://github.com/niski84/plex-smash-deck/releases) / [container image](docs/container.md)

## Quick start

```bash
git clone https://github.com/niski84/plex-smash-deck
cd plex-smash-deck
cp .env.example .env   # fill in your values
go run ./cmd/plex-dashboard
# Open http://localhost:8081
```

All settings can also be saved through the Settings tab in the UI — no restart needed.

### Docker

Portable **linux/amd64** and **linux/arm64** images are published to **GitHub Container Registry** on every **`v*`** tag (same trigger as release binaries): `ghcr.io/<owner>/<repo>:<tag>` and `:latest`. The image runs as a non-root user and keeps persistent state under **`/app/data`** (use a volume or bind mount). See **[docs/container.md](docs/container.md)** for `docker run`, Compose, and security notes. **No extra registry account** is required to pull public images; CI uses `GITHUB_TOKEN` to push.

## Documentation

User guides live in **[docs/](docs/)** as Markdown. Read them in the repo, or open the **Help** tab in the UI (same files, with cross-links between topics).

| Doc | Contents |
|-----|----------|
| [docs/00-getting-started.md](docs/00-getting-started.md) | First run, `.env`, health check, documentation map |
| [docs/connecting-your-tv.md](docs/connecting-your-tv.md) | LG TV / network / pairing |
| [docs/playback-and-webos.md](docs/playback-and-webos.md) | Playback model and webOS limits |
| [docs/01-dashboard-movies.md](docs/01-dashboard-movies.md) | Movie grid, sort, hover panel, play |
| [docs/02-discovery.md](docs/02-discovery.md) | Discovery jobs, TMDB, cache, Radarr |
| [docs/03-snapshots-settings-troubleshooting.md](docs/03-snapshots-settings-troubleshooting.md) | Snapshots, Settings, playlists, FAQ |
| [docs/04-background-health-checks.md](docs/04-background-health-checks.md) | Background connectivity probes (45s / 4m) vs `/api/health` |
| [docs/container.md](docs/container.md) | Docker / GHCR images, Compose, volumes, LAN access |

## Configuration

The Go app loads **one** `.env` in this order: `PLEX_DASHBOARD_ENV_FILE` (if set and exists), then walking **upward from the process current working directory**, then **`.env` beside the `plex-dashboard` binary**. That fixes “started the binary from `$HOME`” and “deployed binary + `.env` in the same folder.” **Git worktrees** often have **no** `.env` in the worktree and no useful parent on that walk; use a **symlink** `ln -s /path/to/main/.env .env` in the worktree, or set `PLEX_DASHBOARD_ENV_FILE`:

```bash
export PLEX_DASHBOARD_ENV_FILE=/path/to/your/main/plex-dashboard/.env
./scripts/reload.sh
```

`scripts/reload.sh` prefers `$PROJECT_DIR/.env`, then `PLEX_DASHBOARD_ENV_FILE`, then the same upward walk from the project directory.

| Variable | Description |
|---|---|
| `PLEX_DASHBOARD_ENV_FILE` | Absolute path to `.env` when it is not beside this checkout |
| `PLEX_BASE_URL` | e.g. `http://192.168.1.10:32400` |
| `PLEX_TOKEN` | Your Plex auth token |
| `PLEX_LIBRARY_KEY` | Section key for your movie library (usually `1`) |
| `PLEX_TARGET_CLIENT_NAME` | Display name of your LG TV as seen in Plex |
| `TMDB_API_KEY` | TMDB API v3 key |
| `LGTV_ADDR` | LG TV local IP, e.g. `192.168.1.20` |
| `LGTV_CLIENT_KEY` | Pairing key obtained on first connection |
| `RADARR_URL` | e.g. `http://192.168.1.10:7878` |
| `RADARR_API_KEY` | Radarr API key |
| `PORT` | HTTP port (default `8081`) |

## Building a release binary

```bash
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o plex-dashboard ./cmd/plex-dashboard
```

Pre-built binaries for Linux, macOS, and Windows are attached to every [GitHub Release](https://github.com/niski84/plex-smash-deck/releases) via CI. **Container images** (multi-arch) are pushed to **ghcr.io** on the same version tags; see [docs/container.md](docs/container.md).

### Windows installer (NSIS)

Tagged releases now also publish a Windows NSIS installer (`*-windows-x64-setup.exe`) that:

- Installs under `%LOCALAPPDATA%\Plex Smash Deck` (no admin required)
- Adds Start Menu entries:
  - `Plex Smash Deck (start server)`
  - `Open UI` (opens `http://127.0.0.1:8081/`)
  - `Uninstall Plex Smash Deck`
- Supports an optional **Run at login (background)** shortcut
- Registers Add/Remove Programs uninstall metadata
- **Upgrades:** Running a newer setup over an existing install updates the app **in place** (same folder under `%LOCALAPPDATA%`, files overwritten, `DisplayVersion` updated). It does **not** run uninstall first, so data and config next to the install are kept. Silent installs (`/S`) skip the upgrade notice.

## How it's built

Go standard library backend, vanilla JS frontend, single binary. No web framework, no JavaScript framework, no database. Movie metadata is cached in memory and on disk so Plex takes as few hits as possible.

```
Dockerfile              — multi-stage image (non-root, health check)
docker-compose.yml      — local Compose stack + named volume for /app/data
images/                 — README dashboard screenshots ([`manifest.json`](images/manifest.json))
docs/images/           — README doc figures; `{name}.meta.json` per `{name}.png` (same generator)
cmd/plex-dashboard/     — entry point
internal/plexdash/
  server.go             — HTTP routes, shared in-memory movie list (invalidated on refresh / snapshot)
  plex_client.go        — Plex XML API
  discovery.go          — TMDB filmography and studio gap analysis
  lgssap.go             — LG webOS SSAP WebSocket client
  snapshots.go          — Daily library diff snapshots
  snapshot_patterns.go  — Theme detection (studios, directors, decades…)
web/plex-dashboard/
  index.html            — Single-page frontend
data/
  movie-snapshots/      — JSON snapshot history
  tmdb-discovery-cache/ — TMDB disk cache (7–180 day TTL per resource)
```

Snapshot files are **not in git** (gitignored). Each clone/worktree has its own `data/movie-snapshots/` under that checkout’s working directory. To reuse the same history from another path (e.g. the main repo while developing in a worktree), set **`PLEXDASH_SNAPSHOT_DIR`** to an absolute path to that directory.

## Ideas and other TV support

If you want to add support for Roku, Fire TV, Apple TV, or anything else, open an issue and describe what you need. Pull requests are welcome. The LG integration lives in `lgssap.go` and is self-contained, so other targets should be straightforward to add alongside it.

## License

MIT
