#!/usr/bin/env python3

import json
import os
import re
import sys
from pathlib import Path


def fail(message: str) -> int:
    print(message, file=sys.stderr)
    return 1


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


_W_NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
_R_NS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
_A_NS = "http://schemas.openxmlformats.org/drawingml/2006/main"
_WP_NS = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"


def _wtag(name: str) -> str:
    return f"{{{_W_NS}}}{name}"


def _atag(name: str) -> str:
    return f"{{{_A_NS}}}{name}"


def _wptag(name: str) -> str:
    return f"{{{_WP_NS}}}{name}"


def _is_heading(p_elem) -> bool:
    pstyle = p_elem.find(f".//{_wtag('pStyle')}")
    if pstyle is None:
        return False
    val = pstyle.get(f"{{{_W_NS}}}val") or ""
    lower = val.lower()
    # English heading styles (Heading1 … Heading6, heading1 … heading6)
    if lower.startswith("heading"):
        return True
    # CJK heading styles: 标题1 … 标题6, 标题
    if val.startswith("标题"):
        return True
    # Numeric style (1–6) used in many CJK DOCX templates
    if len(val) == 1 and "1" <= val <= "6":
        return True
    return False


def _para_text(p_elem) -> str:
    parts = []
    for t in p_elem.findall(f".//{_wtag('t')}"):
        parts.append(t.text or "")
    return "".join(parts)


def _drawing_blip_rid(drawing_elem) -> str:
    blip = drawing_elem.find(f".//{_atag('blip')}")
    if blip is not None:
        return blip.get(f"{{{_R_NS}}}embed") or ""
    return ""


def _drawing_label(drawing_elem) -> str:
    docPr = drawing_elem.find(f".//{_wptag('docPr')}")
    if docPr is not None:
        descr = docPr.get("descr") or ""
        if descr:
            return descr
        return docPr.get("title") or ""
    return ""


def _image_ext(rel) -> str:
    target = getattr(rel, "target_ref", "") or ""
    ext = os.path.splitext(target)[1].lower()
    return ext if ext else ".png"


def _process_para(p_elem, doc_part, section_number: int, image_counter: list, link_counter: list):
    """Return (text_with_placeholders, image_refs_with_blob, link_refs)."""
    text = _para_text(p_elem).strip()

    image_refs = []
    placeholders = []
    for drawing in p_elem.findall(f".//{_wtag('drawing')}"):
        rid = _drawing_blip_rid(drawing)
        if not rid:
            continue
        rel = doc_part.rels.get(rid)
        if not rel:
            continue
        try:
            blob = rel.target_part.blob
            ext = _image_ext(rel)
        except Exception:
            continue
        image_counter[0] += 1
        ref_id = f"docx-image-{section_number}-{image_counter[0]}"
        label = _drawing_label(drawing)
        image_refs.append({"ref_id": ref_id, "ref_type": "image", "label": label,
                           "caption": label, "page": section_number,
                           "_blob": blob, "_ext": ext})
        placeholders.append(f"[Image:{ref_id} {label}]" if label else f"[Image:{ref_id}]")

    if not text and placeholders:
        text = "\n".join(placeholders)
    elif placeholders:
        text = text + "\n" + "\n".join(placeholders)

    link_refs = []
    for hl in p_elem.findall(f".//{_wtag('hyperlink')}"):
        rid = hl.get(f"{{{_R_NS}}}id")
        if not rid:
            continue
        rel = doc_part.rels.get(rid)
        if not rel:
            continue
        url = rel.target_ref
        anchor = "".join(t.text or "" for t in hl.findall(f".//{_wtag('t')}")).strip()
        link_counter[0] += 1
        ref_id = f"docx-link-{section_number}-{link_counter[0]}"
        link_refs.append({"ref_id": ref_id, "ref_type": "link",
                          "label": anchor or url, "anchor_text": anchor,
                          "page": section_number, "url": url, "is_external": True})

    return text, image_refs, link_refs


def _process_table(tbl_elem, doc_part, section_number: int, image_counter: list):
    """Return (rows, image_refs_with_blob)."""
    rows = []
    all_image_refs = []

    for tr in tbl_elem.findall(f".//{_wtag('tr')}"):
        cells = []
        for tc in tr.findall(f".//{_wtag('tc')}"):
            cell_parts = []
            for p in tc.findall(f".//{_wtag('p')}"):
                para_text = _para_text(p).strip()
                placeholders = []
                for drawing in p.findall(f".//{_wtag('drawing')}"):
                    rid = _drawing_blip_rid(drawing)
                    if not rid:
                        continue
                    rel = doc_part.rels.get(rid)
                    if not rel:
                        continue
                    try:
                        blob = rel.target_part.blob
                        ext = _image_ext(rel)
                    except Exception:
                        continue
                    image_counter[0] += 1
                    ref_id = f"docx-image-{section_number}-{image_counter[0]}"
                    label = _drawing_label(drawing)
                    all_image_refs.append({"ref_id": ref_id, "ref_type": "image",
                                           "label": label, "caption": label,
                                           "page": section_number,
                                           "_blob": blob, "_ext": ext})
                    placeholders.append(f"[Image:{ref_id} {label}]" if label else f"[Image:{ref_id}]")
                if para_text:
                    cell_parts.append(para_text)
                cell_parts.extend(placeholders)
            cell_text = "<br>".join(cell_parts).replace("|", r"\|").strip()
            cells.append(cell_text)
        if cells:
            rows.append(cells)

    return rows, all_image_refs


def _merge_adjacent_tables(items: list) -> list:
    """Merge consecutive table items with matching column counts; deduplicate header."""
    merged = []
    for item in items:
        if (item["kind"] == "table" and merged
                and merged[-1]["kind"] == "table"
                and merged[-1]["rows"] and item["rows"]
                and len(merged[-1]["rows"][0]) == len(item["rows"][0])):
            next_rows = [list(r) for r in item["rows"]]
            if next_rows and merged[-1]["rows"] and next_rows[0] == merged[-1]["rows"][0]:
                next_rows = next_rows[1:]
            merged[-1]["rows"].extend(next_rows)
            merged[-1]["refs"] = merged[-1].get("refs", []) + item.get("refs", [])
        else:
            merged.append({
                "kind": item["kind"],
                "text": item.get("text", ""),
                "rows": [list(r) for r in item.get("rows", [])],
                "refs": list(item.get("refs", [])),
            })
    return merged


def save_docx_image(blob: bytes, ref_id: str, ext: str, media_dir, storage_base) -> str:
    if media_dir is None or storage_base is None:
        return ""
    try:
        media_dir.mkdir(parents=True, exist_ok=True)
        filename = f"{ref_id}{ext}"
        disk_path = media_dir / filename
        disk_path.write_bytes(blob)
        return str(disk_path.relative_to(storage_base))
    except Exception:
        return ""


def build_sections(doc, media_dir=None, storage_base=None):
    """Walk document body and group content into heading-bounded sections.

    Returns a list of section dicts, each with:
      heading (str), section_num (int), items (list of kind/text/rows/refs).
    """
    sections = []
    section_num = 1
    image_counter = [0]
    link_counter = [0]
    current_heading = ""
    current_items: list = []

    def save_section():
        if current_heading or current_items:
            sections.append({
                "heading": current_heading,
                "section_num": section_num,
                "items": list(current_items),
            })

    body = doc.element.body
    for child in body:
        tag = child.tag.split("}")[-1] if "}" in child.tag else child.tag

        if tag == "p":
            if _is_heading(child):
                save_section()
                section_num += 1
                current_heading = _para_text(child).strip()
                current_items = []
                image_counter[0] = 0
                link_counter[0] = 0
            else:
                text, img_refs, lnk_refs = _process_para(
                    child, doc.part, section_num, image_counter, link_counter
                )
                refs = img_refs + lnk_refs
                if text or refs:
                    current_items.append({"kind": "text", "text": text, "refs": refs})

        elif tag == "tbl":
            rows, img_refs = _process_table(child, doc.part, section_num, image_counter)
            if rows:
                current_items.append({"kind": "table", "rows": rows, "refs": img_refs})

    save_section()
    return sections


def render_section(section: dict, media_dir=None, storage_base=None):
    """Render a section to (text, refs). Saves image blobs to disk if media_dir given."""
    items = _merge_adjacent_tables(section["items"])
    section_num = section["section_num"]

    parts = []
    heading = section.get("heading", "")
    if heading:
        parts.append(heading)

    all_refs = []
    table_index = 0

    for item in items:
        if item["kind"] == "text":
            if item["text"]:
                parts.append(item["text"])
            for ref in item.get("refs", []):
                if "_blob" in ref:
                    blob, ext = ref.pop("_blob"), ref.pop("_ext", ".png")
                    storage_path = save_docx_image(blob, ref["ref_id"], ext, media_dir, storage_base)
                    if storage_path:
                        ref = dict(ref)
                        ref["storage_path"] = storage_path
                all_refs.append({k: v for k, v in ref.items() if not k.startswith("_")})

        elif item["kind"] == "table":
            table_index += 1
            md = format_markdown_table(item["rows"])
            if md:
                parts.append(md)
            for ref in item.get("refs", []):
                if "_blob" in ref:
                    blob, ext = ref.pop("_blob"), ref.pop("_ext", ".png")
                    storage_path = save_docx_image(blob, ref["ref_id"], ext, media_dir, storage_base)
                    if storage_path:
                        ref = dict(ref)
                        ref["storage_path"] = storage_path
                all_refs.append({k: v for k, v in ref.items() if not k.startswith("_")})
            all_refs.append({
                "ref_id": f"docx-table-{section_num}-{table_index}",
                "ref_type": "table",
                "label": f"Table {table_index} (Section {section_num})",
                "caption": f"Table {table_index} (Section {section_num})",
                "page": section_num,
            })

    text = "\n\n".join(p for p in parts if p.strip()).strip()
    return text, all_refs


def main() -> int:
    if len(sys.argv) not in (2, 3):
        return fail("usage: parse_docx.py <docx_path> [media_dir]")

    path = Path(sys.argv[1])
    if not path.is_file():
        return fail(f"docx file not found: {path}")

    media_dir = Path(sys.argv[2]) if len(sys.argv) == 3 and sys.argv[2] else None
    storage_base_env = os.environ.get("DOCX_PARSER_STORAGE_BASE")
    storage_base = Path(storage_base_env) if storage_base_env else (
        media_dir.parent if media_dir is not None else None
    )

    try:
        from docx import Document
    except Exception as exc:
        return fail(f"python-docx import failed: {exc}")

    try:
        doc = Document(str(path))
    except Exception as exc:
        return fail(f"open docx failed: {exc}")

    sections = build_sections(doc, media_dir, storage_base)
    page_data = []
    for section in sections:
        text, refs = render_section(section, media_dir, storage_base)
        if text or refs:
            page_data.append({"text": text, "page": section["section_num"], "refs": refs})

    if not page_data:
        return fail("docx extraction failed: no extractable content found")

    payload = {
        "text": "\n\n".join(p["text"] for p in page_data).strip(),
        "page_count": 1,
        "pages": page_data,
    }
    json.dump(payload, sys.stdout, ensure_ascii=False)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
