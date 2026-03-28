#!/usr/bin/env python3
"""Update README.md gallery section from images/manifest.json and docs/images/*.meta.json."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
MARKER_START = "<!-- readme-gallery:start -->"
MARKER_END = "<!-- readme-gallery:end -->"

DOCS_IMAGES_DIR = ROOT / "docs" / "images"
IMAGE_EXTENSIONS = {".png", ".jpg", ".jpeg", ".webp", ".gif"}


def collect_docs_gallery_entries() -> list[dict]:
    """Each raster under docs/images must have a sibling {stem}.meta.json."""
    if not DOCS_IMAGES_DIR.is_dir():
        return []
    entries: list[dict] = []
    for path in sorted(DOCS_IMAGES_DIR.iterdir()):
        if not path.is_file() or path.name.startswith("."):
            continue
        if path.suffix.lower() not in IMAGE_EXTENSIONS:
            continue
        meta_path = DOCS_IMAGES_DIR / f"{path.stem}.meta.json"
        if not meta_path.is_file():
            raise FileNotFoundError(
                f"docs/images/{path.name} requires sidecar docs/images/{path.stem}.meta.json "
                "(see docs/images/image.meta.json for an example)."
            )
        meta = json.loads(meta_path.read_text(encoding="utf-8"))
        if not isinstance(meta, dict):
            raise ValueError(f"{meta_path} must be a JSON object")
        alt = (meta.get("alt") or path.stem).strip() or path.stem
        caption = meta.get("caption")
        if caption is not None and not isinstance(caption, str):
            raise ValueError(f"{meta_path}: caption must be a string if set")
        order = meta.get("order")
        if order is not None and not isinstance(order, (int, float)):
            raise ValueError(f"{meta_path}: order must be a number if set")
        rel = path.relative_to(ROOT).as_posix()
        entries.append(
            {
                "rel_path": rel,
                "alt": alt,
                "caption": (caption or "").strip(),
                "order": int(order) if isinstance(order, (int, float)) else 9999,
                "stem": path.stem,
            }
        )
    entries.sort(key=lambda e: (e["order"], e["stem"].lower()))
    return entries


def gallery_block(root_manifest: list[dict], docs_entries: list[dict]) -> str:
    lines = [
        MARKER_START,
        "",
        "## Screenshots",
        "",
        "This section is generated automatically. **Dashboard grids:** add files under [`images/`](images/) and list them in [`images/manifest.json`](images/manifest.json). **Doc images:** place files under [`docs/images/`](docs/images/) with a sidecar `{name}.meta.json` next to each `{name}.png` (see [`docs/images/image.meta.json`](docs/images/image.meta.json)). Run:",
        "",
        "```bash",
        "./scripts/generate-readme-images.sh",
        "```",
        "",
    ]
    for item in root_manifest:
        fn = item.get("file")
        if not fn:
            continue
        path = ROOT / "images" / fn
        if not path.is_file():
            raise FileNotFoundError(f"images/{fn} not found (listed in manifest.json)")
        alt = item.get("alt") or "Screenshot"
        lines.append(f"![{alt}](images/{fn})")
        lines.append("")
    for e in docs_entries:
        lines.append(f"![{e['alt']}]({e['rel_path']})")
        lines.append("")
        if e["caption"]:
            lines.append(e["caption"])
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
        docs_entries = collect_docs_gallery_entries()
        block = gallery_block(manifest, docs_entries)
    except (FileNotFoundError, ValueError, json.JSONDecodeError) as e:
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
