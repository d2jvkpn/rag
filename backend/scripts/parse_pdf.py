#!/usr/bin/env python3

import json
import os
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


def rows_to_markdown(rows) -> str:
    return format_markdown_table(rows)


def table_label(table, table_index: int) -> str:
    page_label = format_page_range(table["pages"])
    return f"表格 {table_index}（第 {page_label} 页）"


def render_table(table, table_index: int) -> str:
    return f"{table_label(table, table_index)}\n\n{rows_to_markdown(table['rows'])}"


def build_table_ref(table, table_index: int):
    label = table_label(table, table_index)
    pages = table.get("pages") or []
    page = pages[0] if pages else 0
    page_part = format_page_range(pages) or str(page)
    return {
        "ref_id": f"pdf-table-{page_part}-{table_index}",
        "ref_type": "table",
        "label": label,
        "caption": label,
        "page": page,
    }


def image_label(page_number: int, image_index: int) -> str:
    return f"图片 {image_index}（第 {page_number} 页）"


def save_page_image(page, image, page_number: int, image_index: int, media_dir, storage_base):
    if media_dir is None or storage_base is None:
        return ""
    try:
        media_dir.mkdir(parents=True, exist_ok=True)
        filename = f"pdf-page-{page_number}-image-{image_index}.png"
        disk_path = media_dir / filename
        px0, ptop, px1, pbottom = page.bbox
        bbox = (
            max(float(image["x0"]), float(px0)),
            max(float(image["top"]), float(ptop)),
            min(float(image["x1"]), float(px1)),
            min(float(image["bottom"]), float(pbottom)),
        )
        if bbox[0] >= bbox[2] or bbox[1] >= bbox[3]:
            return ""
        page.crop(bbox).to_image(resolution=144).save(str(disk_path), format="PNG")
        return str(disk_path.relative_to(storage_base))
    except Exception:
        return ""


def extract_page_images(page, page_number: int, media_dir=None, storage_base=None):
    refs = []
    image_index = 0
    for image in getattr(page, "images", []) or []:
        width = float(image.get("width") or 0)
        height = float(image.get("height") or 0)
        # Ignore tiny decorative icons and avatars; keep content-sized figures.
        if width < 48 or height < 48:
            continue
        image_index += 1
        label = image_label(page_number, image_index)
        ref = {
            "ref_id": f"pdf-image-{page_number}-{image_index}",
            "ref_type": "image",
            "label": label,
            "caption": f"{label}，约 {round(width)}×{round(height)} pt",
            "page": page_number,
        }
        storage_path = save_page_image(page, image, page_number, image_index, media_dir, storage_base)
        if storage_path:
            ref["storage_path"] = storage_path
        refs.append(ref)
    return refs


def extract_page_links(page, page_number: int):
    links = []
    for index, link in enumerate(getattr(page, "hyperlinks", []) or [], start=1):
        url = link.get("uri") or link.get("url")
        if not url:
            continue
        anchor_text = ""
        bbox = (link.get("x0"), link.get("top"), link.get("x1"), link.get("bottom"))
        if all(v is not None for v in bbox):
            try:
                anchor_text = (page.crop(bbox).extract_text() or "").strip()
            except Exception:
                anchor_text = ""
        links.append({
            "ref_id": f"pdf-link-{page_number}-{index}",
            "ref_type": "link",
            "label": anchor_text or url,
            "anchor_text": anchor_text,
            "page": page_number,
            "url": url,
            "is_external": True,
        })
    return links


def build_page_refs(page_item):
    refs = []
    refs.extend(page_item.get("images") or [])
    for index, table in enumerate(page_item["tables"], start=1):
        refs.append(build_table_ref(table, index))
    refs.extend(page_item.get("links") or [])
    return refs


def format_page_range(pages) -> str:
    if not pages:
        return ""
    if len(pages) == 1:
        return str(pages[0])
    return f"{pages[0]}-{pages[-1]}"


def extract_page_tables(page, page_number: int):
    try:
        table_objs = page.find_tables()
    except Exception:
        table_objs = None

    if table_objs is not None:
        tables = []
        bboxes = []
        for table in table_objs:
            rows = table.extract()
            markdown = format_markdown_table(rows)
            if not markdown:
                continue
            bbox = getattr(table, "bbox", None)
            if bbox is not None:
                bboxes.append(expand_bbox(bbox))
            tables.append({"pages": [page_number], "rows": normalize_rows(rows), "bbox": bbox})
        return tables, bboxes

    try:
        tables = page.extract_tables()
    except Exception:
        return [], []

    extracted = []
    for rows in tables or []:
        markdown = format_markdown_table(rows)
        if not markdown:
            continue
        extracted.append({"pages": [page_number], "rows": normalize_rows(rows), "bbox": None})
    return extracted, []


def normalize_rows(rows):
    normalized = []
    max_cols = 0
    for row in rows or []:
        if row is None:
            continue
        cells = [clean_cell(cell) for cell in row]
        if not any(cells):
            continue
        normalized.append(cells)
        max_cols = max(max_cols, len(cells))

    if not normalized or max_cols == 0:
        return []

    for row in normalized:
        if len(row) < max_cols:
            row.extend([""] * (max_cols - len(row)))

    if not any(normalized[0]):
        normalized[0] = [f"列{i + 1}" for i in range(max_cols)]
    return normalized


def can_merge_tables(prev_table, next_table) -> bool:
    if not prev_table["rows"] or not next_table["rows"]:
        return False
    return len(prev_table["rows"][0]) == len(next_table["rows"][0])


def merge_tables(prev_table, next_table):
    merged = {
        "pages": prev_table["pages"] + next_table["pages"],
        "rows": [row[:] for row in prev_table["rows"]],
        "bbox": prev_table.get("bbox"),
    }
    next_rows = [row[:] for row in next_table["rows"]]
    if next_rows and merged["rows"] and next_rows[0] == merged["rows"][0]:
        next_rows = next_rows[1:]
    merged["rows"].extend(next_rows)
    return merged


def merge_cross_page_tables(page_items):
    if not page_items:
        return page_items

    merged_pages = [page_items[0]]
    for current in page_items[1:]:
        previous = merged_pages[-1]
        if previous["tables"] and current["tables"]:
            last_prev = previous["tables"][-1]
            first_current = current["tables"][0]
            if can_merge_tables(last_prev, first_current):
                previous["tables"][-1] = merge_tables(last_prev, first_current)
                current = dict(current)
                current["tables"] = current["tables"][1:]
        merged_pages.append(current)
    return merged_pages


def extract_page_content(page, page_number: int, media_dir=None, storage_base=None):
    tables, table_bboxes = extract_page_tables(page, page_number)
    text_page = remove_table_text(page, table_bboxes)
    text = extract_page_text(text_page)
    images = extract_page_images(page, page_number, media_dir, storage_base)
    links = extract_page_links(page, page_number)
    return {"text": text, "tables": tables, "images": images, "links": links}


def render_page_content(page_item) -> str:
    parts = []
    if page_item["text"]:
        parts.append(page_item["text"])
    for index, table in enumerate(page_item["tables"], start=1):
        parts.append(render_table(table, index))
    return "\n\n".join(part for part in parts if part.strip()).strip()


def main() -> int:
    if len(sys.argv) not in (2, 3):
        return fail("usage: parse_pdf.py <pdf_path> [media_dir]")

    path = Path(sys.argv[1])
    if not path.is_file():
        return fail(f"pdf file not found: {path}")

    media_dir = Path(sys.argv[2]) if len(sys.argv) == 3 and sys.argv[2] else None
    storage_base_env = os.environ.get("PDF_PARSER_STORAGE_BASE")
    storage_base = Path(storage_base_env) if storage_base_env else (media_dir.parent if media_dir is not None else None)

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
        page_items = []
        failed_pages = 0
        for index, page in enumerate(pdf.pages, start=1):
            try:
                page_item = extract_page_content(page, index, media_dir, storage_base)
                page_item["page"] = index
            except Exception as exc:
                failed_pages += 1
                if failed_pages == total_pages:
                    return fail(f"pdf text extraction failed: {exc}")
                continue
            page_items.append(page_item)

        page_items = merge_cross_page_tables(page_items)
        page_data = []
        for page_item in page_items:
            rendered = render_page_content(page_item)
            refs = build_page_refs(page_item)
            if rendered or refs:
                page_data.append({"text": rendered, "page": page_item["page"], "refs": refs})

    if not page_data:
        return fail(
            "pdf text extraction failed: no extractable text found; "
            "this PDF may be scanned/image-based or use an unsupported encoding"
        )

    payload = {
        "text": "\n\n".join(p["text"] for p in page_data).strip(),
        "page_count": total_pages,
        "pages": page_data,
    }
    json.dump(payload, sys.stdout, ensure_ascii=False)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
