#!/usr/bin/env bash
# Verifies the Movie Snapshots API endpoints.
set -euo pipefail

PORT="${PORT:-8081}"
BASE="http://127.0.0.1:${PORT}"
PASS=0; FAIL=0

check() {
  local desc="$1" expected="$2" actual="$3"
  if echo "$actual" | grep -q "$expected"; then
    echo "  ✓ $desc"
    PASS=$((PASS+1))
  else
    echo "  ✗ $desc"
    echo "    Expected to contain: $expected"
    echo "    Got: $(echo "$actual" | head -c 300)"
    FAIL=$((FAIL+1))
  fi
}

echo "=== Snapshots API Verify (port $PORT) ==="

# 1. List snapshots (may be empty)
echo
echo "[1] GET /api/snapshots"
resp=$(curl -sf "$BASE/api/snapshots" || echo '{"success":false,"error":"connection refused"}')
check "returns success:true" '"success":true' "$resp"
check "has snapshots array" '"snapshots"' "$resp"

# 2. Take a snapshot
echo
echo "[2] POST /api/snapshots"
resp=$(curl -sf -X POST "$BASE/api/snapshots" || echo '{"success":false,"error":"connection refused"}')
check "returns success:true" '"success":true' "$resp"
check "has snapshot.id" '"id"' "$resp"
check "has snapshot.count" '"count"' "$resp"

# Extract the snapshot ID
SNAP_ID=$(echo "$resp" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('data',{}).get('snapshot',{}).get('id',''))" 2>/dev/null || true)
echo "    Snapshot ID: ${SNAP_ID:-<not parsed>}"

# 3. List again – should have at least one
echo
echo "[3] GET /api/snapshots (after take)"
resp=$(curl -sf "$BASE/api/snapshots" || echo '{"success":false}')
check "returns success:true" '"success":true' "$resp"

# 4. Load specific snapshot
if [ -n "$SNAP_ID" ]; then
  echo
  echo "[4] GET /api/snapshots/${SNAP_ID}"
  resp=$(curl -sf "$BASE/api/snapshots/${SNAP_ID}" || echo '{"success":false}')
  check "returns success:true" '"success":true' "$resp"
  check "has movies array" '"movies"' "$resp"
fi

# 5. Take a second snapshot for diff
echo
echo "[5] POST /api/snapshots (second)"
resp2=$(curl -sf -X POST "$BASE/api/snapshots" || echo '{"success":false}')
SNAP_ID2=$(echo "$resp2" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('data',{}).get('snapshot',{}).get('id',''))" 2>/dev/null || true)
check "second snapshot success" '"success":true' "$resp2"

# 6. Diff the two snapshots
if [ -n "$SNAP_ID" ] && [ -n "$SNAP_ID2" ] && [ "$SNAP_ID" != "$SNAP_ID2" ]; then
  echo
  echo "[6] GET /api/snapshots/diff?from=${SNAP_ID}&to=${SNAP_ID2}"
  resp=$(curl -sf "$BASE/api/snapshots/diff?from=${SNAP_ID}&to=${SNAP_ID2}" || echo '{"success":false}')
  check "diff returns success:true" '"success":true' "$resp"
  check "diff has added array" '"added"' "$resp"
  check "diff has removed array" '"removed"' "$resp"
  check "diff has netChange" '"netChange"' "$resp"
fi

echo
echo "=== Results: ${PASS} passed, ${FAIL} failed ==="
[ "$FAIL" -eq 0 ]
