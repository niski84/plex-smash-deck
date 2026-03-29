#!/bin/bash
set -euo pipefail

# Short double beep when reload succeeds (ASCII BEL). Enable “audible bell” in your terminal if silent.
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

echo "=== plex-dashboard reload ==="

cd "$PROJECT_DIR"

# Stop the instance we last started (avoid broad pkill matching unrelated processes)
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
# Fallback: same binary path from a previous manual start
if [[ -x "$BINARY" ]]; then
	pkill -f "$BINARY" 2>/dev/null && sleep 1 || true
fi

# Build the new binary
echo "→ Building cmd/plex-dashboard..."
go vet ./...
go build -o "$BINARY" ./cmd/plex-dashboard
echo "  Build OK: $BINARY"

# Load env and start (same discovery idea as Go's findDotEnvPath)
echo "→ Starting plex-dashboard..."
ENV_FILE=""
if [[ -f "$PROJECT_DIR/.env" ]]; then
    ENV_FILE="$PROJECT_DIR/.env"
elif [[ -n "${PLEX_DASHBOARD_ENV_FILE:-}" && -f "${PLEX_DASHBOARD_ENV_FILE}" ]]; then
    ENV_FILE="$PLEX_DASHBOARD_ENV_FILE"
else
    _d="$(cd "$PROJECT_DIR" && pwd)"
    for _ in $(seq 1 32); do
        if [[ -f "$_d/.env" ]]; then
            ENV_FILE="$_d/.env"
            break
        fi
        _p="$(dirname "$_d")"
        [[ "$_p" == "$_d" ]] && break
        _d="$_p"
    done
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
echo "  PORT=$PORT (set in environment or .env; open http://127.0.0.1:${PORT}/)"
nohup "$BINARY" >"$PROJECT_DIR/server.log" 2>&1 &
NEW_PID=$!
echo "$NEW_PID" >"$PROJECT_DIR/server.pid"

# Wait for it to start
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
