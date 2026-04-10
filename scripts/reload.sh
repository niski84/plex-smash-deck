#!/bin/bash
set -euo pipefail

# Short double beep when reload succeeds (ASCII BEL). Enable "audible bell" in your terminal if silent.
_reload_ok_double_beep() {
	local _n
	for _n in 1 2; do
		printf '\a' 2>/dev/null || true
		sleep 0.12
	done
}

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BINARY="$PROJECT_DIR/plex-dashboard"
HUB_PORT="${HUB_PORT:-9110}"
HUB_URL="http://127.0.0.1:${HUB_PORT}"

echo "=== plex-dashboard reload ==="

cd "$PROJECT_DIR"

# ── vault: pull fresh secrets from Infisical before sourcing .env ─────────────
_VAULT_SYNC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && cd ../../infrastructure && pwd)/sync-secrets.sh"
if [[ -f "$_VAULT_SYNC" ]] && [[ -n "${INFISICAL_CLIENT_ID:-}" ]]; then
  echo "→ Pulling secrets from vault (plex-dashboard)..."
  "$_VAULT_SYNC" --pull plex-dashboard 2>/dev/null || echo "  ⚠  Vault pull skipped (using cached .env)"
fi
# ─────────────────────────────────────────────────────────────────────────────

# Build Tailwind CSS
echo "→ Tailwind CSS…"
if [ -f package.json ] && [ -d node_modules ]; then
    npm run build:css
fi

# Build the new binary
echo "→ Building cmd/plex-dashboard..."
go vet ./...
go build -o "$BINARY" ./cmd/plex-dashboard
echo "  Build OK: $BINARY"

# ── Delegate process management to deck-hub ───────────────────────────────────
# If the hub is running, let it restart the app (single managed instance).
# If the hub is not running, fall back to direct start so dev still works.
if curl -fsS "${HUB_URL}/api/health" >/dev/null 2>&1; then
  echo "→ Asking deck-hub to restart plex-dashboard..."
  curl -fsS -X POST "${HUB_URL}/api/apps/plex-dashboard/restart" >/dev/null
  echo "→ Waiting for deck-hub to report plex-dashboard healthy..."
  for i in $(seq 1 40); do
    sleep 0.3
    STATUS="$(curl -fsS "${HUB_URL}/api/apps" 2>/dev/null | grep -o '"plex-dashboard"[^}]*"state":"[^"]*"' | grep -o '"state":"[^"]*"' | cut -d'"' -f4 || true)"
    if [[ "$STATUS" == "running" ]]; then
      echo "✓ plex-dashboard is running (managed by deck-hub at ${HUB_URL}/v/plex-dashboard)"
      _reload_ok_double_beep
      exit 0
    fi
  done
  # Fall through to direct health check in case state polling fails
  PORT="${PORT:-8081}"
  if curl -fsS "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
    echo "✓ plex-dashboard healthy at http://127.0.0.1:${PORT}/"
    _reload_ok_double_beep
    exit 0
  fi
  echo "✗ plex-dashboard did not come up — check deck-hub.log"
  exit 1
fi

# ── Fallback: hub not running, start directly ────────────────────────────────
echo "  (deck-hub not detected on ${HUB_URL} — starting directly)"

# Stop any existing instance
echo "→ Stopping existing plex-dashboard..."
if [[ -f "$PROJECT_DIR/server.pid" ]]; then
	OLD_PID="$(tr -d ' \n' <"$PROJECT_DIR/server.pid" 2>/dev/null || true)"
	if [[ -n "$OLD_PID" ]] && kill -0 "$OLD_PID" 2>/dev/null; then
		kill "$OLD_PID" 2>/dev/null || true
		for _ in $(seq 1 25); do
			kill -0 "$OLD_PID" 2>/dev/null || break
			sleep 0.2
		done
	fi
fi
if [[ -x "$BINARY" ]]; then
	pkill -f "$BINARY" 2>/dev/null && sleep 1 || true
fi

# Load env
ENV_FILE=""
if [[ -f "$PROJECT_DIR/.env" ]]; then
    ENV_FILE="$PROJECT_DIR/.env"
elif [[ -n "${PLEX_DASHBOARD_ENV_FILE:-}" && -f "${PLEX_DASHBOARD_ENV_FILE}" ]]; then
    ENV_FILE="$PLEX_DASHBOARD_ENV_FILE"
fi
if [[ -n "$ENV_FILE" ]]; then
    set -a
    # shellcheck disable=SC1090
    source "$ENV_FILE"
    set +a
    echo "  Env: $ENV_FILE"
fi

PORT="${PORT:-8081}"
export PORT
echo "  PORT=$PORT (open http://127.0.0.1:${PORT}/)"
nohup "$BINARY" >"$PROJECT_DIR/server.log" 2>&1 &
NEW_PID=$!
echo "$NEW_PID" >"$PROJECT_DIR/server.pid"

for i in $(seq 1 30); do
    sleep 0.3
    if curl -fsS "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
        echo "✓ Server running at http://127.0.0.1:${PORT}/ (PID $NEW_PID)"
        _reload_ok_double_beep
        exit 0
    fi
done

echo "✗ Server did not respond within 9s — last lines of server.log:"
tail -n 50 "$PROJECT_DIR/server.log" 2>/dev/null || true
exit 1
