#!/usr/bin/env bash
# Assembles docs/demo-frames/*.png into docs/demo.gif using ImageMagick.
# Per-frame delays (hundredths of a second) are mapped by filename keyword.
# Frames not matching any keyword get the DEFAULT_DELAY.
set -euo pipefail

FRAMES_DIR="docs/demo-frames"
OUT="docs/demo.gif"
THUMB_W=1400   # resize width; height scales proportionally
DEFAULT_DELAY=175  # 1.75 s

declare -A DELAYS=(
  ["dashboard-home"]=150
  ["dashboard-grid"]=350
  ["search-sneak"]=150
  ["search-sneakers"]=250
  ["search-sneakers-hover"]=350
  ["sneakers-lightbox"]=450
  ["search-cleared"]=250
  ["movies-selected"]=300
  ["snapshots-tab"]=200
  ["snapshots-diff"]=350
  ["snapshots-patterns"]=350
  ["discovery-tab"]=200
  ["discovery-studio-mode"]=200
  ["discovery-a24-ready"]=250
  ["discovery-searching"]=175
  ["discovery-results"]=350
  ["discovery-results-scrolled"]=350
  ["discovery-row-hover"]=450
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
