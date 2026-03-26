#!/usr/bin/env bash
# Assembles docs/demo-frames/*.png into docs/demo.gif using ImageMagick.
# Per-frame delays (hundredths of a second) are mapped by filename keyword.
# Frames not matching any keyword get the DEFAULT_DELAY.
set -euo pipefail

FRAMES_DIR="docs/demo-frames"
OUT="docs/demo.gif"
THUMB_W=1400   # resize width; height scales proportionally
DEFAULT_DELAY=90

declare -A DELAYS=(
  ["dashboard-home"]=75
  ["dashboard-grid"]=175
  ["search-sneak"]=75
  ["search-sneakers"]=125
  ["search-sneakers-hover"]=175
  ["sneakers-lightbox"]=225
  ["search-cleared"]=125
  ["movies-selected"]=150
  ["snapshots-tab"]=100
  ["snapshots-diff"]=175
  ["snapshots-patterns"]=175
  ["discovery-tab"]=100
  ["discovery-studio-mode"]=100
  ["discovery-a24-ready"]=125
  ["discovery-searching"]=90
  ["discovery-results"]=175
  ["discovery-results-scrolled"]=175
  ["discovery-row-hover"]=225
)

echo "=== Building $OUT from $FRAMES_DIR ==="

# Build one convert command with -delay before each PNG.
# This is the only reliable way to embed per-frame delays in ImageMagick —
# setting delay on intermediate single-frame GIFs and then re-combining them
# causes the delays to be overwritten during assembly.
args=()
count=0
for png in "$FRAMES_DIR"/*.png; do
  name=$(basename "$png" .png)
  key="${name#*-}"   # strip leading frame number

  delay=${DEFAULT_DELAY}
  for k in "${!DELAYS[@]}"; do
    if [[ "$key" == *"$k"* ]]; then
      delay="${DELAYS[$k]}"
      break
    fi
  done

  args+=(-delay "$delay" -resize "${THUMB_W}x" "$png")
  echo "  frame: $name  delay=${delay}cs"
  (( count++ )) || true
done

echo "Assembling $count frames..."
convert -loop 0 "${args[@]}" "$OUT"

# Verify delays were actually embedded.
echo "Embedded delays:"
identify -verbose "$OUT" 2>/dev/null | grep "Delay:" | head -5

size=$(du -sh "$OUT" | cut -f1)
echo "✓ $OUT  ($size)"
