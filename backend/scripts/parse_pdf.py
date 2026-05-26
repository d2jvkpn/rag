#!/usr/bin/env python3

import json
import sys
from pathlib import Path


def fail(message: str) -> int:
    print(message, file=sys.stderr)
    return 1


def extract_page_text(page) -> str:
    attempts = [
        {},
        {"extraction_mode": "layout"},
    ]
    seen = set()
    for kwargs in attempts:
        key = tuple(sorted(kwargs.items()))
        if key in seen:
            continue
        seen.add(key)
        try:
            text = page.extract_text(**kwargs) or ""
        except TypeError:
            continue
        if text and text.strip():
            return text.strip()
    return ""


def main() -> int:
    if len(sys.argv) != 2:
        return fail("usage: parse_pdf.py <pdf_path>")

    path = Path(sys.argv[1])
    if not path.is_file():
        return fail(f"pdf file not found: {path}")

    try:
        from pypdf import PdfReader
    except Exception as exc:
        return fail(f"pypdf import failed: {exc}")

    try:
        reader = PdfReader(str(path))
    except Exception as exc:
        return fail(f"open pdf failed: {exc}")

    total_pages = len(reader.pages)
    pages = []
    failed_pages = 0
    for page in reader.pages:
        try:
            text = extract_page_text(page)
        except Exception as exc:
            failed_pages += 1
            text = ""
            if failed_pages == total_pages:
                return fail(f"pdf text extraction failed: {exc}")
        if text:
            pages.append(text)

    if not pages:
        return fail(
            "pdf text extraction failed: no extractable text found; "
            "this PDF may be scanned/image-based or use an unsupported encoding"
        )

    payload = {
        "text": "\n\n".join(pages).strip(),
        "page_count": total_pages,
    }
    json.dump(payload, sys.stdout, ensure_ascii=False)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
