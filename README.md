# Plex Smash Deck

A self-hosted Plex dashboard that streams movies straight to an LG webOS TV and adds discovery, playlists, and library tracking on top.

Part of the [Smash Deck](https://github.com/niski84/smash-deck-catalog) family - self-hosted dashboards built in Go for the homelab.

## What It Does

Plays movies on an LG webOS TV using the SSAP WebSocket protocol built into modern LG sets. You select a single title or build a multi-select playlist and push it over the local network; the TV opens its own native media player and Plex is just the file server. This sidesteps the Plex client app on the TV entirely.

Generates random weighted playlists that lean toward titles you have watched less, so you get an always-on personal channel that actually shuffles. There is also a discovery view that searches by actor, co-actor, director, or production studio against TMDB and shows what is missing from your library, with optional one-click adds through Radarr.

Tracks the library with daily snapshots that diff against the previous day, surfaces patterns across newly added films (recurring directors, studios, decades, genres), and provides fast in-memory search across the whole library with poster art and rating popups that blend TMDB and OMDb scores.

## Tech Stack

- Go (single binary, no runtime dependencies)
- `golang.org/x/crypto` for SSAP client key handling
- Embedded vanilla HTML, CSS, and JavaScript (no framework)
- Playwright for end-to-end tests
- Docker / Compose support included

## Running

```bash
go build -o plex-dashboard ./cmd/plex-dashboard
./plex-dashboard
```

Configure via environment variables (see `.env.example`). Requires a Plex server, a Plex token, and a free TMDB API key. OMDb, Radarr, Fanart, and LG TV settings are optional. Default port is 8081.

## Status

Active development.

## License

MIT
