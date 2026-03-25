#!/usr/bin/env bash
# Assembles docs/demo-frames/*.png into docs/demo.gif using ImageMagick.
# Per-frame delays (hundredths of a second) are mapped by filename keyword.
# Frames not matching any keyword get the DEFAULT_DELAY.
set -euo pipefail

FRAMES_DIR="docs/demo-frames"
OUT="docs/demo.gif"
THUMB_W=1400   # resize width; height scales proportionally
DEFAULT_DELAY=120  # 1.2 s

declare -A DELAYS=(
  ["dashboard-home"]=80
  ["dashboard-grid"]=250    # let the viewer take in the full grid
  ["search-sneak"]=80
  ["search-sneakers"]=150
  ["search-sneakers-hover"]=250
  ["sneakers-lightbox"]=350
  ["search-cleared"]=150
  ["movies-selected"]=200
  ["snapshots-tab"]=100
  ["snapshots-diff"]=250
  ["snapshots-patterns"]=250
  ["discovery-tab"]=100
  ["discovery-studio-mode"]=100
  ["discovery-a24-ready"]=120
  ["discovery-searching"]=80
  ["discovery-results"]=200
  ["discovery-results-scrolled"]=250
  ["discovery-row-hover"]=300
)

echo "=== Building $OUT from $FRAMES_DIR ==="

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Resize each frame and tag with its delay.
inputs=()
for png in "$FRAMES_DIR"/*.png; do
  name=$(basename "$png" .png)
  # Strip leading frame number (e.g. "001-dashboard-grid" → "dashboard-grid")
  key="${name#*-}"

  delay=${DEFAULT_DELAY}
  for k in "${!DELAYS[@]}"; do
    if [[ "$key" == *"$k"* ]]; then
      delay="${DELAYS[$k]}"
      break
    fi
  done

  out_frame="$tmp/${name}.gif"
  convert "$png" -resize "${THUMB_W}x" -delay "$delay" "$out_frame"
  inputs+=("$out_frame")
  echo "  frame: $name  delay=${delay}cs"
done

echo "Assembling ${#inputs[@]} frames..."
convert -loop 0 "${inputs[@]}" "$OUT"

size=$(du -sh "$OUT" | cut -f1)
echo "✓ $OUT  ($size)"
