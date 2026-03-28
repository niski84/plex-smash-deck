#!/usr/bin/env python3
"""Update README.md gallery section from images/manifest.json."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MARKER_START = "<!-- readme-gallery:start -->"
MARKER_END = "<!-- readme-gallery:end -->"


def gallery_block(manifest: list[dict]) -> str:
    lines = [
        MARKER_START,
        "",
        "## Screenshots",
        "",
        "This section is generated from [`images/manifest.json`](images/manifest.json). Add files under [`images/`](images/) and list them there, then run:",
        "",
        "```bash",
        "./scripts/generate-readme-images.sh",
        "```",
        "",
    ]
    for item in manifest:
        fn = item.get("file")
        if not fn:
            continue
        path = ROOT / "images" / fn
        if not path.is_file():
            raise FileNotFoundError(f"images/{fn} not found (listed in manifest.json)")
        alt = item.get("alt") or "Screenshot"
        lines.append(f"![{alt}](images/{fn})")
        lines.append("")
    lines.append(MARKER_END)
    return "\n".join(lines)


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument(
        "--check",
        action="store_true",
        help="Exit 1 if README.md would change (for CI)",
    )
    args = ap.parse_args()

    manifest_path = ROOT / "images" / "manifest.json"
    readme_path = ROOT / "README.md"
    if not manifest_path.is_file():
        print(f"Missing {manifest_path}", file=sys.stderr)
        return 1

    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if not isinstance(manifest, list):
        print("manifest.json must be a JSON array", file=sys.stderr)
        return 1

    try:
        block = gallery_block(manifest)
    except FileNotFoundError as e:
        print(e, file=sys.stderr)
        return 1
    current = readme_path.read_text(encoding="utf-8")
    pattern = re.compile(
        re.escape(MARKER_START) + r".*?" + re.escape(MARKER_END),
        re.DOTALL,
    )
    if not pattern.search(current):
        print(
            f"README.md must contain {MARKER_START} … {MARKER_END}",
            file=sys.stderr,
        )
        return 1

    new_text = pattern.sub(block, current, count=1)
    if args.check:
        if new_text != current:
            print(
                "README gallery is out of date; run ./scripts/generate-readme-images.sh",
                file=sys.stderr,
            )
            return 1
        return 0

    readme_path.write_text(new_text, encoding="utf-8", newline="\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
