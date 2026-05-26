#!/usr/bin/env python3

import json
import re
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


def expand_bbox(bbox, padding: float = 2.0):
    x0, top, x1, bottom = bbox
    return (x0 - padding, top - padding, x1 + padding, bottom + padding)


def obj_intersects_bbox(obj, bbox) -> bool:
    x0 = obj.get("x0")
    x1 = obj.get("x1")
    top = obj.get("top")
    bottom = obj.get("bottom")
    if None in (x0, x1, top, bottom):
        return False

    bx0, btop, bx1, bbottom = bbox
    return not (x1 <= bx0 or x0 >= bx1 or bottom <= btop or top >= bbottom)


def remove_table_text(page, bboxes):
    if not bboxes:
        return page
    try:
        return page.filter(
            lambda obj: not any(obj_intersects_bbox(obj, bbox) for bbox in bboxes)
        )
    except Exception:
        return page


def clean_cell(cell) -> str:
    if cell is None:
        return ""
    text = str(cell).replace("\r\n", "\n").replace("\r", "\n")
    lines = [line.strip() for line in text.split("\n") if line.strip()]
    text = "<br>".join(lines)
    text = re.sub(r"\s+", " ", text).strip()
    return text.replace("|", r"\|")


def format_markdown_table(rows) -> str:
    normalized = []
    max_cols = 0
    for row in rows:
        if row is None:
            continue
        cells = [clean_cell(cell) for cell in row]
        if not any(cells):
            continue
        normalized.append(cells)
        max_cols = max(max_cols, len(cells))

    if not normalized or max_cols == 0:
        return ""

    for row in normalized:
        if len(row) < max_cols:
            row.extend([""] * (max_cols - len(row)))

    header = normalized[0]
    if not any(header):
        header = [f"列{i + 1}" for i in range(max_cols)]
        normalized[0] = header

    lines = [
        "| " + " | ".join(header) + " |",
        "| " + " | ".join(["---"] * max_cols) + " |",
    ]
    for row in normalized[1:]:
        lines.append("| " + " | ".join(row) + " |")
    return "\n".join(lines)


def extract_page_tables(page, page_number: int) -> list[str]:
    try:
        table_objs = page.find_tables()
    except Exception:
        table_objs = None

    if table_objs is not None:
        rendered = []
        bboxes = []
        table_index = 0
        for table in table_objs:
            rows = table.extract()
            markdown = format_markdown_table(rows)
            if not markdown:
                continue
            table_index += 1
            bbox = getattr(table, "bbox", None)
            if bbox is not None:
                bboxes.append(expand_bbox(bbox))
            rendered.append(f"表格 {table_index}（第 {page_number} 页）\n\n{markdown}")
        return rendered, bboxes

    try:
        tables = page.extract_tables()
    except Exception:
        return [], []

    rendered = []
    table_index = 0
    for rows in tables or []:
        markdown = format_markdown_table(rows)
        if not markdown:
            continue
        table_index += 1
        rendered.append(f"表格 {table_index}（第 {page_number} 页）\n\n{markdown}")
    return rendered, []


def extract_page_content(page, page_number: int) -> str:
    tables, table_bboxes = extract_page_tables(page, page_number)
    text_page = remove_table_text(page, table_bboxes)
    text = extract_page_text(text_page)
    parts = []
    if text:
        parts.append(text)
    parts.extend(tables)
    return "\n\n".join(part for part in parts if part.strip()).strip()


def main() -> int:
    if len(sys.argv) != 2:
        return fail("usage: parse_pdf.py <pdf_path>")

    path = Path(sys.argv[1])
    if not path.is_file():
        return fail(f"pdf file not found: {path}")

    try:
        import pdfplumber
    except Exception as exc:
        return fail(f"pdfplumber import failed: {exc}")

    try:
        pdf = pdfplumber.open(str(path))
    except Exception as exc:
        return fail(f"open pdf failed: {exc}")

    with pdf:
        total_pages = len(pdf.pages)
        pages = []
        failed_pages = 0
        for index, page in enumerate(pdf.pages, start=1):
            try:
                page_content = extract_page_content(page, index)
            except Exception as exc:
                failed_pages += 1
                if failed_pages == total_pages:
                    return fail(f"pdf text extraction failed: {exc}")
                continue
            if page_content:
                pages.append(page_content)

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
