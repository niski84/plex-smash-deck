#!/usr/bin/env bash
# Assembles docs/demo-frames/*.png into docs/demo.gif using ImageMagick.
# Per-frame delays (hundredths of a second) are mapped by filename keyword.
# Frames not matching any keyword get the DEFAULT_DELAY.
set -euo pipefail

FRAMES_DIR="docs/demo-frames"
OUT="docs/demo.gif"
THUMB_W=1400   # resize width; height scales proportionally
DEFAULT_DELAY=250  # 2.5 s

declare -A DELAYS=(
  ["dashboard-home"]=200
  ["dashboard-grid"]=500    # let the viewer take in the full grid
  ["search-sneak"]=180
  ["search-sneakers"]=300
  ["search-sneakers-hover"]=500
  ["sneakers-lightbox"]=600
  ["search-cleared"]=300
  ["movies-selected"]=400
  ["snapshots-tab"]=250
  ["snapshots-diff"]=500
  ["snapshots-patterns"]=500
  ["discovery-tab"]=250
  ["discovery-studio-mode"]=250
  ["discovery-a24-ready"]=300
  ["discovery-searching"]=200
  ["discovery-results"]=450
  ["discovery-results-scrolled"]=450
  ["discovery-row-hover"]=600
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
