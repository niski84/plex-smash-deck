# Running Plex Smash Deck in a container

Official images are built for **linux/amd64** and **linux/arm64** and published to **GitHub Container Registry (GHCR)** on each **version tag** (`v*`), alongside release binaries.

## What you need

- **Docker** (or Podman with compose compatibility) on the machine that will run the UI
- Your usual **Plex** URL + token and **TMDB** key (same as a bare-metal install)
- **No extra cloud accounts** for pulling images: GHCR is free for public packages; authentication is only required for **private** repos or **pushing** images (handled by GitHub Actions with `GITHUB_TOKEN`).

## Pull and run (recommended after a release)

Replace `OWNER` and `TAG` (for example `v1.2.3`):

```bash
docker pull ghcr.io/OWNER/plex-smash-deck:TAG
docker run -d --name plex-smash-deck \
  --restart unless-stopped \
  -p 8081:8081 \
  --env-file .env \
  -v plexdash_data:/app/data \
  ghcr.io/OWNER/plex-smash-deck:TAG
```

- **`/app/data`** holds settings, snapshots, caches, and playback state. Use a named volume (as above) or a bind mount such as `-v /path/on/host/data:/app/data`.
- **`PORT`**: If you set `PORT=9090` in `.env`, publish that port instead, e.g. `-p 9090:9090`.

Open `http://127.0.0.1:8081/` (or your host IP) in a browser.

## Docker Compose (build from this repo)

```bash
cp .env.example .env
# edit .env
docker compose up -d
```

This builds the image locally and tags it `plex-smash-deck:local`.

## Security notes

- The image runs as **non-root** (UID **65532**).
- The runtime image is **Alpine**-based with **CA certificates** so HTTPS to Plex, TMDB, etc. works.
- **Secrets** belong in `.env` or your orchestrator’s secret store — not in the image.
- CI attaches **SBOM** and **provenance** attestations on published images when using the default workflow (supply-chain transparency on GitHub).

## Health checks

The image defines a Docker **HEALTHCHECK** against `GET /api/health`. Orchestrators (Kubernetes, Nomad, etc.) can use the same HTTP probe on `PORT`.

## Troubleshooting

- **LG / Radarr / fanart** from Docker: the container must reach those hosts on your LAN. On some systems you need `host.docker.internal` or `network_mode: host` (Linux only) — see your Docker docs.
- **Plex URL**: Use a URL reachable **from inside the container** (often your server’s LAN IP, not `localhost` on the host).
