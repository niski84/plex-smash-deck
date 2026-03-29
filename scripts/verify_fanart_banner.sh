#!/usr/bin/env bash
# Proves fanart banner API endpoints respond (200 JSON) when the server is up.
set -euo pipefail
BASE="${PLEX_DASHBOARD_URL:-http://127.0.0.1:8081}"

for path in \
  "/api/branding/fanart-banner" \
  "/api/fanart-banner/cache-status" \
  "/api/fanart-banner/log"
do
  url="${BASE}${path}"
  code=$(curl -sS -o /tmp/fanart_verify.json -w "%{http_code}" "$url")
  if [[ "$code" != "200" ]]; then
    echo "FAIL $url -> HTTP $code" >&2
    exit 1
  fi
  if ! grep -q '"success":true' /tmp/fanart_verify.json; then
    echo "FAIL $url -> not success JSON: $(head -c 200 /tmp/fanart_verify.json)" >&2
    exit 1
  fi
  echo "OK $path"
done

echo "fanart banner verify: all OK"
