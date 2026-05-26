package llm

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanText(t *testing.T) {
	cleaned := CleanText("a\r\n\r\n\r\nb\r\n")
	if cleaned != "a\n\nb" {
		t.Fatalf("unexpected cleaned text: %q", cleaned)
	}
}

func TestCleanTextNormalizesPDFBulletGlyphs(t *testing.T) {
	cleaned := CleanText("第一项\n 第二项\r\n\uf0a7 第三项")
	want := "第一项\n• 第二项\n• 第三项"
	if cleaned != want {
		t.Fatalf("unexpected cleaned text: %q want %q", cleaned, want)
	}
}

func TestParsePDFUsesPythonScript(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "fake_parse_pdf.py")
	script := "#!/usr/bin/env python3\nimport json\nprint(json.dumps({'text':'hello\\nworld','page_count':2}))\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake parser: %v", err)
	}

	t.Setenv("PDF_PARSER_SCRIPT", scriptPath)
	t.Setenv("PDF_PARSER_PYTHON", "python3")

	got, err := Parse("ignored.pdf", "pdf")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got.PageCount != 2 {
		t.Fatalf("unexpected page count: %d", got.PageCount)
	}
	if got.Text != "hello\nworld" {
		t.Fatalf("unexpected text: %q", got.Text)
	}
}

func TestParsePDFSample(t *testing.T) {
	path := filepath.Join("..", "..", "..", "data", "docs.2026-03-16", "W020200702389760473273.pdf")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("sample pdf not available: %v", err)
	}
	if err := exec.Command("python3", "-c", "import pdfplumber").Run(); err != nil {
		t.Skipf("pdfplumber not available: %v", err)
	}

	t.Setenv("PDF_PARSER_PYTHON", "python3")

	got, err := Parse(path, "pdf")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got.PageCount < 1 {
		t.Fatalf("expected at least one page, got %d", got.PageCount)
	}
	if len(strings.TrimSpace(got.Text)) < 100 {
		t.Fatalf("expected extracted text, got len=%d", len(got.Text))
	}
}

func TestParseDocxConvertsTablesToMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.docx")
	files := map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>段落一</w:t></w:r></w:p>
    <w:tbl>
      <w:tr>
        <w:tc><w:p><w:r><w:t>姓名</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>备注</w:t></w:r></w:p></w:tc>
      </w:tr>
      <w:tr>
        <w:tc><w:p><w:r><w:t>张三</w:t></w:r></w:p></w:tc>
        <w:tc>
          <w:p><w:r><w:t>第一行</w:t></w:r></w:p>
          <w:p><w:r><w:t>第二行</w:t></w:r></w:p>
        </w:tc>
      </w:tr>
    </w:tbl>
    <w:p><w:r><w:t>段落二</w:t></w:r></w:p>
  </w:body>
</w:document>`,
	}
	if err := writeZipFixture(path, files); err != nil {
		t.Fatalf("write docx fixture: %v", err)
	}

	got, err := Parse(path, "docx")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	want := strings.Join([]string{
		"段落一",
		"",
		"| 姓名 | 备注 |",
		"| --- | --- |",
		"| 张三 | 第一行<br>第二行 |",
		"",
		"段落二",
	}, "\n")
	if got.Text != want {
		t.Fatalf("unexpected text:\n%s\nwant:\n%s", got.Text, want)
	}
}

func TestParseDocxMergesAdjacentContinuationTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "continuation.docx")
	files := map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>统计表</w:t></w:r></w:p>
    <w:tbl>
      <w:tr>
        <w:tc><w:p><w:r><w:t>指标</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>数值</w:t></w:r></w:p></w:tc>
      </w:tr>
      <w:tr>
        <w:tc><w:p><w:r><w:t>收入</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>100</w:t></w:r></w:p></w:tc>
      </w:tr>
    </w:tbl>
    <w:tbl>
      <w:tr>
        <w:tc><w:p><w:r><w:t>指标</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>数值</w:t></w:r></w:p></w:tc>
      </w:tr>
      <w:tr>
        <w:tc><w:p><w:r><w:t>成本</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>60</w:t></w:r></w:p></w:tc>
      </w:tr>
    </w:tbl>
  </w:body>
</w:document>`,
	}
	if err := writeZipFixture(path, files); err != nil {
		t.Fatalf("write docx fixture: %v", err)
	}

	got, err := Parse(path, "docx")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	want := strings.Join([]string{
		"统计表",
		"",
		"| 指标 | 数值 |",
		"| --- | --- |",
		"| 收入 | 100 |",
		"| 成本 | 60 |",
	}, "\n")
	if got.Text != want {
		t.Fatalf("unexpected text:\n%s\nwant:\n%s", got.Text, want)
	}
}

func TestParsePptxConvertsTablesToMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.pptx")
	files := map[string]string{
		"ppt/slides/slide1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld>
    <p:spTree>
      <p:sp>
        <p:txBody>
          <a:p><a:r><a:t>概览</a:t></a:r></a:p>
        </p:txBody>
      </p:sp>
      <p:graphicFrame>
        <a:graphic>
          <a:graphicData>
            <a:tbl>
              <a:tr>
                <a:tc><a:txBody><a:p><a:r><a:t>指标</a:t></a:r></a:p></a:txBody></a:tc>
                <a:tc><a:txBody><a:p><a:r><a:t>数值</a:t></a:r></a:p></a:txBody></a:tc>
              </a:tr>
              <a:tr>
                <a:tc><a:txBody><a:p><a:r><a:t>收入</a:t></a:r></a:p></a:txBody></a:tc>
                <a:tc><a:txBody><a:p><a:r><a:t>100</a:t></a:r></a:p></a:txBody></a:tc>
              </a:tr>
            </a:tbl>
          </a:graphicData>
        </a:graphic>
      </p:graphicFrame>
    </p:spTree>
  </p:cSld>
</p:sld>`,
		"ppt/notesSlides/notesSlide1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<p:notes xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>补充说明</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld>
</p:notes>`,
	}
	if err := writeZipFixture(path, files); err != nil {
		t.Fatalf("write pptx fixture: %v", err)
	}

	got, err := Parse(path, "pptx")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got.PageCount != 1 {
		t.Fatalf("unexpected page count: %d", got.PageCount)
	}

	want := strings.Join([]string{
		"幻灯片 1",
		"概览",
		"",
		"| 指标 | 数值 |",
		"| --- | --- |",
		"| 收入 | 100 |",
		"备注: 补充说明",
	}, "\n")
	if got.Text != want {
		t.Fatalf("unexpected text:\n%s\nwant:\n%s", got.Text, want)
	}
}

func writeZipFixture(path string, files map[string]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			_ = zw.Close()
			return err
		}
	}
	return zw.Close()
}
