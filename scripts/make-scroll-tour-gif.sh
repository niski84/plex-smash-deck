#!/usr/bin/env bash
# Assembles docs/scroll-tour-frames/*.png into docs/scroll-tour.gif using ImageMagick.
#
# Per-frame delays (hundredths of a second) are assigned by keyword in the frame name:
#   *-top      → longer pause so the viewer registers the new tab
#   *-scroll-* → shorter delay (motion feel)
#
# Run after:  npm run scroll-tour   (generates the PNG frames via Playwright)
set -euo pipefail

FRAMES_DIR="docs/scroll-tour-frames"
OUT="docs/scroll-tour.gif"
THUMB_W=1400      # resize width; height scales proportionally
DEFAULT_DELAY=100 # fallback if no keyword matches

declare -A DELAYS=(
  ["top"]=200       # pause at the top of each tab
  ["scroll-1"]=80
  ["scroll-2"]=80
  ["scroll-3"]=80
  ["scroll-4"]=80
  ["scroll-5"]=80
)

if [[ ! -d "$FRAMES_DIR" ]]; then
  echo "ERROR: frames directory not found: $FRAMES_DIR" >&2
  echo "Run 'npm run scroll-tour' first to generate the PNG frames." >&2
  exit 1
fi

mapfile -t pngs < <(ls "$FRAMES_DIR"/*.png 2>/dev/null)
if [[ ${#pngs[@]} -eq 0 ]]; then
  echo "ERROR: no PNG frames found in $FRAMES_DIR" >&2
  exit 1
fi

echo "=== Building $OUT from $FRAMES_DIR ==="

args=()
count=0
for png in "${pngs[@]}"; do
  name=$(basename "$png" .png)
  key="${name#*-}"   # strip leading frame-number prefix (e.g. "001-")

  delay=${DEFAULT_DELAY}
  for k in "${!DELAYS[@]}"; do
    if [[ "$key" == *"$k"* ]]; then
      delay="${DELAYS[$k]}"
      break
    fi
  done

  args+=(-delay "$delay" -resize "${THUMB_W}x" "$png")
  printf "  frame: %-40s delay=%dcs\n" "$name" "$delay"
  (( count++ )) || true
done

echo "Assembling $count frames..."
convert -loop 0 "${args[@]}" "$OUT"

echo "Embedded delays:"
identify -verbose "$OUT" 2>/dev/null | grep "Delay:" | head -6

size=$(du -sh "$OUT" | cut -f1)
echo "✓ $OUT  ($size, $count frames)"
