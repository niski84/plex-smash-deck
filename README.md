# Plex Dashboard

A self-hosted Go web app for browsing your Plex movie library, discovering films you're missing, and playing movies directly on an LG Smart TV.

![Plex Dashboard demo](docs/demo.gif)

## Features

- **Movie grid** — tile view of your entire Plex library with poster art, file sizes, and instant search by title, actor, or director
- **Hover popup** — plot summary, rating, runtime, directors, and genres on card hover
- **Multi-select & playlist** — check movies and send them as a sequential playlist directly to the TV
- **LG Smart TV playback** — streams files to the LG webOS native media player via the SSAP protocol (no Plex app needed on the TV)
- **Discovery / gap analysis** — find TMDB credits missing from your library; search by actor, director, or production company (A24, Pixar, …)
- **Studio discovery** — browse any TMDB production company's catalog and see what you're missing
- **Daily snapshots** — automatic diff of library changes with pattern analysis (studios, directors, actors, genres, decades)
- **Radarr integration** — add missing movies straight from Discovery
- **Fast search** — all 7 000+ cards are pre-rendered; search toggles visibility rather than rebuilding the DOM

## Quick start

```
git clone https://github.com/niski84/plex-dashboard
cd plex-dashboard
cp .env.example .env   # fill in Plex URL, token, library key, TMDB key
go run ./cmd/plex-dashboard
# Open http://localhost:8081
```

## Configuration (`.env`)

| Variable | Description |
|---|---|
| `PLEX_BASE_URL` | e.g. `http://192.168.1.10:32400` |
| `PLEX_TOKEN` | Your Plex auth token |
| `PLEX_LIBRARY_KEY` | Section key for your movie library (usually `1`) |
| `TARGET_CLIENT_NAME` | Display name of your LG TV as seen in Plex |
| `TMDB_API_KEY` | [TMDB API v3 key](https://www.themoviedb.org/settings/api) |
| `LGTV_ADDR` | LG TV IP:port, e.g. `192.168.1.20:3001` |
| `LGTV_CLIENT_KEY` | Pairing key from first connection |
| `PORT` | HTTP port (default `8081`) |

## Building a release binary

```
GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o plex-dashboard ./cmd/plex-dashboard
```

Pre-built binaries for Linux/macOS/Windows are attached to every [GitHub Release](https://github.com/niski84/plex-dashboard/releases) via the CI workflow.

## Architecture

```
cmd/plex-dashboard/     # main entry point
internal/plexdash/
  server.go             # HTTP routes + shared movie list cache (15-min TTL)
  plex_client.go        # Plex XML API client
  discovery.go          # TMDB filmography + studio gap analysis
  discovery_cache.go    # Disk cache for TMDB responses (7–180 day TTL)
  lgssap.go             # LG webOS SSAP WebSocket client
  snapshots.go          # Daily library snapshots
  snapshot_patterns.go  # Pattern analysis (studios, directors, actors, …)
web/plex-dashboard/
  index.html            # Single-page frontend (vanilla JS, no framework)
data/
  movie-snapshots/      # JSON snapshots
  tmdb-discovery-cache/ # TMDB disk cache
```

## Caching

| Layer | Where | TTL |
|---|---|---|
| Movie list | Server in-memory | 15 min |
| Movie list | Browser localStorage | 1 hour |
| Poster thumbnails | Browser HTTP cache | 7 days |
| TMDB filmography | Disk | 7 days |
| TMDB person ID | Disk | 180 days |
| TMDB movie details | Disk | 60 days |

Manual "Refresh Movies" bypasses both caches with `?nocache=1`.

## License

MIT
