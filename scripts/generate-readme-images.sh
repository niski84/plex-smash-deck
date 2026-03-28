#!/usr/bin/env bash
# Regenerate the README "Screenshots" section from images/manifest.json and docs/images/*.meta.json
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
exec python3 "$ROOT/scripts/generate_readme_images.py" "$@"
