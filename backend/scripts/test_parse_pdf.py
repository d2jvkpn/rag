import importlib.util
import pathlib
import unittest


SCRIPT_PATH = pathlib.Path(__file__).with_name("parse_pdf.py")
SPEC = importlib.util.spec_from_file_location("parse_pdf", SCRIPT_PATH)
parse_pdf = importlib.util.module_from_spec(SPEC)
assert SPEC.loader is not None
SPEC.loader.exec_module(parse_pdf)


class FakeTable:
    def __init__(self, rows, bbox):
        self._rows = rows
        self.bbox = bbox

    def extract(self):
        return self._rows


class FakePageImage:
    def save(self, path, format=None):
        pathlib.Path(path).write_bytes(b"png")


class FakeCroppedPage:
    def __init__(self, text):
        self._text = text

    def extract_text(self, **kwargs):
        return self._text

    def to_image(self, resolution=144):
        return FakePageImage()


class FakePage:
    def __init__(self, texts=None, filtered_text="", tables=None, objects=None, hyperlinks=None, images=None, crop_text=""):
        self._texts = texts or {(): ""}
        self._filtered_text = filtered_text
        self._tables = tables or []
        self._objects = objects or []
        self.hyperlinks = hyperlinks or []
        self.images = images or []
        self.bbox = (0, 0, 600, 800)
        self._crop_text = crop_text

    def extract_text(self, **kwargs):
        key = tuple(sorted(kwargs.items()))
        return self._texts.get(key, self._texts.get((), ""))

    def find_tables(self):
        return self._tables

    def extract_tables(self):
        return [table.extract() for table in self._tables]

    def filter(self, predicate):
        kept = [obj for obj in self._objects if predicate(obj)]
        text = self._filtered_text if len(kept) != len(self._objects) else self._texts.get((), "")
        return FakePage(texts=self._texts, filtered_text=self._filtered_text, tables=self._tables, objects=kept, hyperlinks=self.hyperlinks, images=self.images, crop_text=self._crop_text)._with_default_text(text)

    def crop(self, bbox):
        return FakeCroppedPage(self._crop_text)

    def _with_default_text(self, text):
        self._texts = {(): text}
        return self


class ParsePDFTests(unittest.TestCase):
    def test_extract_page_content_excludes_table_region_text(self):
        page = FakePage(
            texts={(): "标题\n表格内容\n结尾"},
            filtered_text="标题\n结尾",
            tables=[
                FakeTable(
                    rows=[["列1", "列2"], ["A", "B"]],
                    bbox=(10, 10, 100, 60),
                )
            ],
            objects=[
                {"x0": 0, "x1": 5, "top": 0, "bottom": 5},
                {"x0": 20, "x1": 80, "top": 20, "bottom": 40},
            ],
        )

        got = parse_pdf.render_page_content(parse_pdf.extract_page_content(page, 1))

        self.assertEqual(
            got,
            "标题\n结尾\n\n表格 1（第 1 页）\n\n| 列1 | 列2 |\n| --- | --- |\n| A | B |",
        )

    def test_extract_page_content_falls_back_when_no_table_bboxes(self):
        class NoBBoxTable(FakeTable):
            def __init__(self, rows):
                self._rows = rows

        page = FakePage(
            texts={(): "正文\n表格线性文本"},
            filtered_text="不会使用",
            tables=[NoBBoxTable([["指标", "值"], ["收入", "100"]])],
            objects=[{"x0": 20, "x1": 80, "top": 20, "bottom": 40}],
        )

        got = parse_pdf.render_page_content(parse_pdf.extract_page_content(page, 2))

        self.assertEqual(
            got,
            "正文\n表格线性文本\n\n表格 1（第 2 页）\n\n| 指标 | 值 |\n| --- | --- |\n| 收入 | 100 |",
        )


    def test_extract_page_content_returns_image_table_and_link_refs(self):
        page = FakePage(
            texts={(): "正文"},
            tables=[FakeTable(rows=[["列1", "列2"], ["A", "B"]], bbox=(10, 10, 100, 60))],
            hyperlinks=[{"uri": "https://example.com", "x0": 1, "top": 2, "x1": 3, "bottom": 4}],
            images=[
                {"width": 12, "height": 12},
                {"width": 120, "height": 80},
            ],
            crop_text="Example",
        )

        page_item = parse_pdf.extract_page_content(page, 5)
        refs = parse_pdf.build_page_refs(page_item)

        self.assertEqual(len(refs), 3)
        self.assertEqual(refs[0]["ref_type"], "image")
        self.assertEqual(refs[0]["label"], "图片 1（第 5 页）")
        self.assertEqual(refs[0]["page"], 5)
        self.assertEqual(refs[1]["ref_type"], "table")
        self.assertEqual(refs[1]["label"], "表格 1（第 5 页）")
        self.assertEqual(refs[1]["page"], 5)
        self.assertEqual(refs[2]["ref_type"], "link")
        self.assertEqual(refs[2]["anchor_text"], "Example")
        self.assertEqual(refs[2]["url"], "https://example.com")


    def test_extract_page_images_writes_storage_path(self):
        media_dir = pathlib.Path(self.id().replace('.', '_'))
        root = pathlib.Path(self._testMethodName)
        storage_base = pathlib.Path(self._testMethodName + "_base")
        media_dir = storage_base / "doc-1"
        page = FakePage(images=[{"width": 120, "height": 80, "x0": 1, "top": 2, "x1": 121, "bottom": 82}])

        try:
            refs = parse_pdf.extract_page_images(page, 7, media_dir, storage_base)

            self.assertEqual(len(refs), 1)
            self.assertEqual(refs[0]["storage_path"], "doc-1/pdf-page-7-image-1.png")
            self.assertTrue((storage_base / refs[0]["storage_path"]).is_file())
        finally:
            if storage_base.exists():
                for child in sorted(storage_base.rglob("*"), reverse=True):
                    if child.is_file():
                        child.unlink()
                    else:
                        child.rmdir()
                storage_base.rmdir()

    def test_merge_cross_page_tables_deduplicates_repeated_header(self):
        page_items = [
            {
                "text": "第一页正文",
                "tables": [
                    {
                        "pages": [1],
                        "rows": [["指标", "值"], ["收入", "100"]],
                        "bbox": (0, 500, 100, 790),
                    }
                ],
            },
            {
                "text": "第二页正文",
                "tables": [
                    {
                        "pages": [2],
                        "rows": [["指标", "值"], ["成本", "60"]],
                        "bbox": (0, 0, 100, 120),
                    }
                ],
            },
        ]

        merged = parse_pdf.merge_cross_page_tables(page_items)

        self.assertEqual(len(merged[0]["tables"]), 1)
        self.assertEqual(merged[0]["tables"][0]["pages"], [1, 2])
        self.assertEqual(
            merged[0]["tables"][0]["rows"],
            [["指标", "值"], ["收入", "100"], ["成本", "60"]],
        )
        self.assertEqual(merged[1]["tables"], [])

    def test_render_page_content_uses_page_range_for_merged_table(self):
        page_item = {
            "text": "合并结果",
            "tables": [
                {
                    "pages": [3, 4],
                    "rows": [["指标", "值"], ["收入", "100"], ["成本", "60"]],
                    "bbox": None,
                }
            ],
        }

        got = parse_pdf.render_page_content(page_item)

        self.assertEqual(
            got,
            "合并结果\n\n表格 1（第 3-4 页）\n\n| 指标 | 值 |\n| --- | --- |\n| 收入 | 100 |\n| 成本 | 60 |",
        )


if __name__ == "__main__":
    unittest.main()
