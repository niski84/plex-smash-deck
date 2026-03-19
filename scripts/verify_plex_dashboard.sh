#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# Use a free port by default so verify doesn't hit an unrelated process already on 8081.
if [[ -z "${PORT:-}" ]]; then
  PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()' 2>/dev/null)" || PORT="8081"
fi
export PORT
BASE_URL="http://127.0.0.1:${PORT}"

echo "[verify] building plex-dashboard..."
go build -o /tmp/plex-dashboard ./cmd/plex-dashboard

echo "[verify] starting server..."
/tmp/plex-dashboard > /tmp/plex-dashboard.log 2>&1 &
SERVER_PID=$!
trap 'kill ${SERVER_PID} >/dev/null 2>&1 || true' EXIT

for _ in {1..20}; do
  if curl -fsS "${BASE_URL}/api/health" >/dev/null; then
    break
  fi
  sleep 0.5
done

echo "[verify] checking /api/health"
HEALTH_JSON="$(curl -fsS "${BASE_URL}/api/health")"
echo "$HEALTH_JSON" | rg '"success"\s*:\s*true' >/dev/null
echo "[ok] health endpoint"

echo "[verify] checking /api/movies"
MOVIES_JSON="$(curl -fsS "${BASE_URL}/api/movies?limit=5")"
echo "$MOVIES_JSON" | rg '"success"\s*:\s*true' >/dev/null
MOVIE_COUNT="$(echo "$MOVIES_JSON" | rg -o '"count"\s*:\s*[0-9]+' | rg -o '[0-9]+' | sed -n '1p')"
echo "$MOVIES_JSON" | rg '"Actors"\s*:' >/dev/null
echo "$MOVIES_JSON" | rg '"Directors"\s*:' >/dev/null
echo "[ok] movies endpoint count=${MOVIE_COUNT:-unknown}"

echo "[verify] checking /api/players"
PLAYERS_JSON="$(curl -fsS "${BASE_URL}/api/players")"
echo "$PLAYERS_JSON" | rg '"success"\s*:\s*true' >/dev/null
PLAYER_COUNT="$(echo "$PLAYERS_JSON" | (rg -o '"Name"\s*:' || true) | wc -l | tr -d ' ')"
echo "[ok] players endpoint visiblePlayers=${PLAYER_COUNT:-0}"

echo "[verify] checking /api/genres"
GENRES_JSON="$(curl -fsS "${BASE_URL}/api/genres")"
echo "$GENRES_JSON" | rg '"success"\s*:\s*true' >/dev/null
echo "[ok] genres endpoint"

echo "[verify] checking /api/discovery/poster (TMDB image proxy)"
POSTER_CODE="$(curl -sS -o /tmp/plexdash-poster-test.jpg -w "%{http_code}" "${BASE_URL}/api/discovery/poster?path=%2F7WsyChQLEftFiDOVTGkv3hFpyyt.jpg")"
if [[ "$POSTER_CODE" != "200" ]] || [[ ! -s /tmp/plexdash-poster-test.jpg ]]; then
  echo "[fail] poster proxy: http=$POSTER_CODE size=$(wc -c </tmp/plexdash-poster-test.jpg 2>/dev/null || echo 0)"
  exit 1
fi
echo "[ok] poster proxy (jpeg bytes received)"

echo "[verify] done"
