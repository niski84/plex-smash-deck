# Plex Library Dashboard

Standalone Go web app for browsing a Plex movie library, TMDB discovery / gap analysis, Radarr add, and movie snapshots.

Previously lived under the RV sale monorepo; this is its own repository.

## Run

```bash
# Optional: copy .env with PLEX_BASE_URL, PLEX_TOKEN, TMDB_API_KEY, etc.
# If you split from the RV sale repo, you can copy its .env here, or symlink it.
go run ./cmd/plex-dashboard
```

Default port: **8081** (override with `PORT`).

**Data / cache:** This repo ships with an empty `data/` folder. To reuse TMDB cache, snapshots, and saved Plex settings from the old monorepo, copy `data/tmdb-discovery-cache/`, `data/movie-snapshots/`, and `data/plexdash-settings.json` from your **rv sale** project (or run with `cwd` there — settings paths are relative to the working directory).

## Build & deploy (local)

```bash
./scripts/reload.sh
```

Builds `./cmd/plex-dashboard` to `./plex-dashboard`, starts it, and tails readiness via `/api/health`.

## CLI: snapshots

```bash
go build -o snapshot-cli ./cmd/snapshot-cli
./snapshot-cli --url http://127.0.0.1:8081 list
```

## Verify

```bash
./scripts/verify_plex_dashboard.sh
```

## Related

- **Wyze Feral Smash Deck** (home automation UI) lives in the separate **rv sale** repo under `internal/wyzeferal/`.
