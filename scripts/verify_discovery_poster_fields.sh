#!/usr/bin/env bash
# Optional: requires TMDB_API_KEY and a running plex-dashboard (same PORT as verify or pass BASE_URL).
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"
if [[ -z "${TMDB_API_KEY:-}" ]] && [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi
if [[ -z "${TMDB_API_KEY:-}" ]]; then
  echo "[verify_discovery_poster_fields] skip: TMDB_API_KEY not set"
  exit 0
fi
BASE_URL="${BASE_URL:-http://127.0.0.1:8081}"
JSON="$(curl -fsS "${BASE_URL}/api/discovery/filmography?person=Tom%20Hanks&role=actor&minRating=0")"
echo "$JSON" | rg -q '"success"\s*:\s*true' || { echo "[fail] filmography not success"; exit 1; }
# At least one item should have posterUrl or posterPath when TMDB returns data
echo "$JSON" | rg '"posterUrl"|"posterPath"' >/dev/null || { echo "[fail] no poster fields in response"; exit 1; }
echo "[ok] discovery response includes posterUrl and/or posterPath"
