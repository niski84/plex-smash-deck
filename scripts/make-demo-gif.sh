#!/usr/bin/env bash
# Assembles docs/demo-frames/*.png into docs/demo.gif using ImageMagick.
# Per-frame delays (hundredths of a second) are mapped by filename keyword.
# Frames not matching any keyword get the DEFAULT_DELAY.
set -euo pipefail

FRAMES_DIR="docs/demo-frames"
OUT="docs/demo.gif"
THUMB_W=1400   # resize width; height scales proportionally
DEFAULT_DELAY=350  # 3.5 s

declare -A DELAYS=(
  ["dashboard-home"]=300
  ["dashboard-grid"]=700    # let the viewer take in the full grid
  ["search-sneak"]=300
  ["search-sneakers"]=500
  ["search-sneakers-hover"]=700
  ["sneakers-lightbox"]=900
  ["search-cleared"]=500
  ["movies-selected"]=600
  ["snapshots-tab"]=400
  ["snapshots-diff"]=700
  ["snapshots-patterns"]=700
  ["discovery-tab"]=400
  ["discovery-studio-mode"]=400
  ["discovery-a24-ready"]=500
  ["discovery-searching"]=350
  ["discovery-results"]=700
  ["discovery-results-scrolled"]=700
  ["discovery-row-hover"]=900
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
