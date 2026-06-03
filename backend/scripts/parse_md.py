#!/usr/bin/env python3

import json
import re
import sys
from pathlib import Path


def fail(message: str) -> int:
    print(message, file=sys.stderr)
    return 1


_HEADING_RE = re.compile(r"^(#+)\s+.+", re.MULTILINE)
_INLINE_IMAGE_RE = re.compile(r"!\[([^\]]*)\]\(([^)]+)\)")
_INLINE_LINK_RE = re.compile(r"(?<!!)\[([^\]]*)\]\(([^)]+)\)")


def split_sections(text: str):
    """Split on any heading level (#, ##, ###, ...).

    Each section is (title, block_text).  block_text starts with the raw
    heading line so callers can process it uniformly.
    """
    lines = text.split("\n")
    blocks = []
    current_title = ""
    current_lines: list[str] = []

    def flush():
        raw = "\n".join(current_lines).strip()
        if raw:
            blocks.append((current_title, raw))

    for line in lines:
        if _HEADING_RE.match(line):
            flush()
            current_title = line.lstrip("#").strip()
            current_lines = [line]
        else:
            current_lines.append(line)
    flush()

    if not blocks:
        return [("", text)]
    return blocks


def process_text_refs(text: str, section_number: int):
    """Replace image and link syntax in text; collect refs.

    Images: ``![alt](url)`` → ``[Image:ref_id alt]`` (or ``[Image:ref_id]``).
    Links:  ``[anchor](url)`` → ``anchor`` (URL moved to refs only).
    """
    refs = []
    counters = {"image": 0, "link": 0}

    def replace_image(m):
        counters["image"] += 1
        alt = m.group(1).strip()
        url = m.group(2).strip()
        ref_id = f"md-image-{section_number}-{counters['image']}"
        refs.append({
            "ref_id": ref_id,
            "ref_type": "image",
            "label": alt or url,
            "caption": alt or url,
            "page": section_number,
            "url": url,
            "is_external": not url.startswith("#"),
        })
        return f"[Image:{ref_id} {alt}]" if alt else f"[Image:{ref_id}]"

    def replace_link(m):
        counters["link"] += 1
        anchor = m.group(1).strip()
        url = m.group(2).strip()
        ref_id = f"md-link-{section_number}-{counters['link']}"
        refs.append({
            "ref_id": ref_id,
            "ref_type": "link",
            "label": anchor or url,
            "anchor_text": anchor,
            "page": section_number,
            "url": url,
            "is_external": not url.startswith("#"),
        })
        return anchor

    # Images must be processed before links (image syntax is a superset).
    text = _INLINE_IMAGE_RE.sub(replace_image, text)
    text = _INLINE_LINK_RE.sub(replace_link, text)
    return text, refs


def main() -> int:
    if len(sys.argv) != 2:
        return fail("usage: parse_md.py <md_path>")

    path = Path(sys.argv[1])
    if not path.is_file():
        return fail(f"markdown file not found: {path}")

    try:
        content = path.read_text(encoding="utf-8")
    except Exception as exc:
        return fail(f"read failed: {exc}")

    sections = split_sections(content)
    page_data = []
    for section_number, (title, block_text) in enumerate(sections, start=1):
        clean_text, refs = process_text_refs(block_text, section_number)
        clean_text = clean_text.strip()
        if clean_text or refs:
            page_data.append({"text": clean_text, "page": section_number, "refs": refs})

    if not page_data:
        return fail("markdown extraction failed: no extractable content found")

    payload = {
        "text": "\n\n".join(p["text"] for p in page_data).strip(),
        "page_count": 1,
        "pages": page_data,
    }
    json.dump(payload, sys.stdout, ensure_ascii=False)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
