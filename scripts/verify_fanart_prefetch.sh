#!/usr/bin/env bash
# Proves fanart-movie prefetch endpoint returns JSON 200 (may have zero items without TMDB/key).
set -euo pipefail
BASE="${PLEX_DASHBOARD_URL:-http://127.0.0.1:8081}"
code=$(curl -sS -o /tmp/fanart-prefetch.json -w "%{http_code}" "${BASE}/api/fanart-movie/prefetch?tmdbId=0")
test "$code" = "200"
python3 - <<'PY'
import json
with open("/tmp/fanart-prefetch.json") as f:
    j = json.load(f)
assert j.get("success") is True
d = j.get("data") or {}
assert "items" in d
assert isinstance(d["items"], list)
print("verify_fanart_prefetch: OK", d.get("reason", ""), "items=", len(d["items"]))
PY
