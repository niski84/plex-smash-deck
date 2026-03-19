#!/usr/bin/env bash
# End-to-end: TMDB → filmography JSON → local poster proxy → JPEG bytes on disk.
# Requires: TMDB_API_KEY (from env or .env in repo root).
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi
if [[ -z "${TMDB_API_KEY:-}" ]]; then
  echo "[verify_poster_e2e] FAIL: set TMDB_API_KEY or add to .env"
  exit 1
fi

PORT="$(python3 -c 'import socket; s=socket.socket(); s.bind(("",0)); print(s.getsockname()[1]); s.close()' 2>/dev/null)" || PORT="8099"
export PORT
BIN="$(mktemp /tmp/plexdash-e2e-XXXXXX)"
go build -o "$BIN" ./cmd/plex-dashboard

LOG_OUT="$(mktemp /tmp/plexdash-e2e-log-XXXXXX)"
"$BIN" >"$LOG_OUT" 2>&1 &
PID=$!
trap 'kill "$PID" >/dev/null 2>&1 || true; rm -f "$BIN"' EXIT

BASE="http://127.0.0.1:${PORT}"
for _ in {1..40}; do
  if curl -fsS "${BASE}/api/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.15
done

echo "[1] Direct TMDB /movie/550 poster_path"
TMDB_POSTER_PATH="$(curl -fsS "https://api.themoviedb.org/3/movie/550?api_key=${TMDB_API_KEY}" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("poster_path") or "")')"
if [[ -z "$TMDB_POSTER_PATH" ]]; then
  echo "[verify_poster_e2e] FAIL: no poster_path from TMDB for movie 550"
  exit 1
fi
echo "    poster_path=$TMDB_POSTER_PATH"

echo "[2] Local /api/discovery/poster (GET image bytes)"
ENC_PATH="$(python3 -c "import urllib.parse; print(urllib.parse.quote('''${TMDB_POSTER_PATH}''', safe=''))")"
HTTP_CODE="$(curl -sS -o /tmp/plexdash-e2e-poster.jpg -w "%{http_code}" "${BASE}/api/discovery/poster?path=${ENC_PATH}")"
if [[ "$HTTP_CODE" != "200" ]]; then
  echo "[verify_poster_e2e] FAIL: poster proxy http=$HTTP_CODE (expected 200)"
  tail -20 "$LOG_OUT" || true
  exit 1
fi
SIZE="$(wc -c </tmp/plexdash-e2e-poster.jpg | tr -d ' ')"
if [[ "${SIZE:-0}" -lt 500 ]]; then
  echo "[verify_poster_e2e] FAIL: jpeg too small (${SIZE} bytes)"
  exit 1
fi
echo "    downloaded ${SIZE} bytes → /tmp/plexdash-e2e-poster.jpg"

echo "[3] /api/discovery/filmography (Tom Hanks, actor)"
curl -fsS --max-time 120 "${BASE}/api/discovery/filmography?person=Tom%20Hanks&role=actor&minRating=0" | python3 -c '
import json, sys
j = json.load(sys.stdin)
assert j.get("success") is True, j
data = j.get("data") or {}
items = data.get("items") or []
assert len(items) > 0, "no items"
first = items[0]
pu = (first.get("posterUrl") or "").strip()
pp = (first.get("posterPath") or "").strip()
assert pu or pp, "no poster fields on first row: " + repr(first)
print("    first_row tmdbId=%s posterUrl_len=%d posterPath=%r" % (first.get("tmdbId"), len(pu), pp))
'

echo "[4] Debug log tail (data/plexdash-discovery.log)"
if [[ -f data/plexdash-discovery.log ]]; then
  tail -n 15 data/plexdash-discovery.log
else
  echo "    (no log yet — run discovery in UI to populate)"
fi

echo "[verify_poster_e2e] OK"
