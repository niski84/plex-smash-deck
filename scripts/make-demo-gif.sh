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
