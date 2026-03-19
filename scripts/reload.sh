#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BINARY="$PROJECT_DIR/plex-dashboard"

echo "=== plex-dashboard reload ==="

# Kill any running plex-dashboard processes
echo "→ Stopping existing plex-dashboard..."
pkill -f "plex-dashboard" 2>/dev/null && sleep 1 || echo "  (none running)"

# Build the new binary
echo "→ Building cmd/plex-dashboard..."
cd "$PROJECT_DIR"
go build -o "$BINARY" ./cmd/plex-dashboard
echo "  Build OK: $BINARY"

# Load env and start
echo "→ Starting plex-dashboard..."
if [ -f "$PROJECT_DIR/.env" ]; then
    set -a
    # shellcheck disable=SC1090
    source "$PROJECT_DIR/.env"
    set +a
fi

PORT="${PORT:-8081}"
nohup "$BINARY" >"$PROJECT_DIR/server.log" 2>&1 &
NEW_PID=$!
echo $NEW_PID > "$PROJECT_DIR/server.pid"

# Wait for it to start
for i in $(seq 1 30); do
    sleep 0.3
    if curl -fsS "http://127.0.0.1:${PORT}/api/health" >/dev/null 2>&1; then
        echo "✓ Server running at http://127.0.0.1:${PORT}/ (PID $NEW_PID)"
        exit 0
    fi
done

echo "✗ Server did not respond within 9s — check server.log"
exit 1
