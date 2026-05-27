package parser

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/model"
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
	script := `#!/usr/bin/env python3
import json
print(json.dumps({
    'text': 'hello\nworld',
    'page_count': 2,
    'pages': [{
        'text': 'hello',
        'page': 1,
        'refs': [{
            'ref_id': 'pdf-link-1-1',
            'ref_type': 'link',
            'label': 'Example',
            'anchor_text': 'Example',
            'page': 1,
            'url': 'https://example.com',
            'is_external': True,
        }],
    }],
}))
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake parser: %v", err)
	}

	t.Setenv("PDF_PARSER_SCRIPT", scriptPath)
	t.Setenv("PDF_PARSER_PYTHON", "python3")

	got, err := Parse("ignored.pdf", "pdf", "")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got.PageCount != 2 {
		t.Fatalf("unexpected page count: %d", got.PageCount)
	}
	if got.Text != "hello\nworld" {
		t.Fatalf("unexpected text: %q", got.Text)
	}
	if len(got.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(got.Blocks))
	}
	if len(got.Blocks[0].Refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(got.Blocks[0].Refs))
	}
	ref := got.Blocks[0].Refs[0]
	if ref.RefType != "link" || ref.AnchorText != "Example" || ref.URL != "https://example.com" ||
		!ref.IsExternal {
		t.Fatalf("unexpected ref: %+v", ref)
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

	got, err := Parse(path, "pdf", "")
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

	got, err := Parse(path, "docx", "")
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

	got, err := Parse(path, "docx", "")
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

	got, err := Parse(path, "pptx", "")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if got.PageCount != 1 {
		t.Fatalf("unexpected page count: %d", got.PageCount)
	}

	want := strings.Join([]string{
		"Slide 1",
		"概览",
		"",
		"| 指标 | 数值 |",
		"| --- | --- |",
		"| 收入 | 100 |",
		"Notes: 补充说明",
	}, "\n")
	if got.Text != want {
		t.Fatalf("unexpected text:\n%s\nwant:\n%s", got.Text, want)
	}
}

func TestParseDocxHeadingBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "headings.docx")
	files := map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:pPr><w:pStyle w:val="Heading1"/></w:pPr>
      <w:r><w:t>第一章</w:t></w:r>
    </w:p>
    <w:p><w:r><w:t>第一章内容。</w:t></w:r></w:p>
    <w:p>
      <w:pPr><w:pStyle w:val="Heading2"/></w:pPr>
      <w:r><w:t>第二章</w:t></w:r>
    </w:p>
    <w:p><w:r><w:t>第二章内容。</w:t></w:r></w:p>
  </w:body>
</w:document>`,
	}
	if err := writeZipFixture(path, files); err != nil {
		t.Fatalf("write docx fixture: %v", err)
	}
	got, err := Parse(path, "docx", "")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(got.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(got.Blocks))
	}
	if got.Blocks[0].SectionTitle != "第一章" {
		t.Errorf("expected SectionTitle='第一章', got %q", got.Blocks[0].SectionTitle)
	}
	if got.Blocks[1].SectionTitle != "第二章" {
		t.Errorf("expected SectionTitle='第二章', got %q", got.Blocks[1].SectionTitle)
	}
}

func TestParseDocxNoHeadingsFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noh.docx")
	files := map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>段落一</w:t></w:r></w:p>
    <w:p><w:r><w:t>段落二</w:t></w:r></w:p>
  </w:body>
</w:document>`,
	}
	if err := writeZipFixture(path, files); err != nil {
		t.Fatalf("write docx fixture: %v", err)
	}
	got, err := Parse(path, "docx", "")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(got.Blocks) != 1 {
		t.Fatalf("expected single fallback block, got %d", len(got.Blocks))
	}
	if got.Blocks[0].SectionTitle != "" {
		t.Errorf("expected empty SectionTitle, got %q", got.Blocks[0].SectionTitle)
	}
}

func TestSplitMarkdownBlocksHeadings(t *testing.T) {
	md := "# Title\n\nIntro paragraph.\n\n## Section A\n\nContent A.\n\n## Section B\n\nContent B."
	blocks := splitMarkdownBlocks(md)
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(blocks))
	}
	cases := []struct{ title, wantTitle string }{
		{blocks[0].SectionTitle, "Title"},
		{blocks[1].SectionTitle, "Section A"},
		{blocks[2].SectionTitle, "Section B"},
	}
	for _, c := range cases {
		if c.title != c.wantTitle {
			t.Errorf("expected SectionTitle=%q, got %q", c.wantTitle, c.title)
		}
	}
}

func TestSplitMarkdownBlocksNoHeadings(t *testing.T) {
	md := "Just a paragraph without headings."
	blocks := splitMarkdownBlocks(md)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].SectionTitle != "" {
		t.Errorf("expected empty SectionTitle, got %q", blocks[0].SectionTitle)
	}
}

func TestParsePptxReturnsBlocks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slides.pptx")
	files := map[string]string{
		"ppt/slides/slide1.xml": `<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>第一页</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
		"ppt/slides/slide2.xml": `<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>第二页</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
	}
	if err := writeZipFixture(path, files); err != nil {
		t.Fatalf("write pptx fixture: %v", err)
	}
	got, err := Parse(path, "pptx", "")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(got.Blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(got.Blocks))
	}
	if got.Blocks[0].PageStart != 1 || got.Blocks[1].PageStart != 2 {
		t.Errorf(
			"unexpected PageStart values: %d, %d",
			got.Blocks[0].PageStart,
			got.Blocks[1].PageStart,
		)
	}
	if got.Blocks[0].SectionTitle != "Slide 1" {
		t.Errorf("unexpected SectionTitle: %q", got.Blocks[0].SectionTitle)
	}
}

func TestExtractMarkdownRefs(t *testing.T) {
	input := "See [Go docs](https://go.dev) and ![logo](img/logo.png) for details."
	text, refs := extractMarkdownRefs(input)

	if text != "See Go docs and [Image: logo] for details." {
		t.Fatalf("unexpected text: %q", text)
	}
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
	// image is extracted first
	if refs[0].RefType != "image" || refs[0].Label != "logo" || refs[0].URL != "img/logo.png" {
		t.Errorf("unexpected image ref: %+v", refs[0])
	}
	if refs[1].RefType != "link" || refs[1].AnchorText != "Go docs" ||
		refs[1].URL != "https://go.dev" ||
		!refs[1].IsExternal {
		t.Errorf("unexpected link ref: %+v", refs[1])
	}
}

func TestSplitMarkdownBlocksExtractsRefs(t *testing.T) {
	md := "# Section\n\nSee [example](https://example.com)."
	blocks := splitMarkdownBlocks(md)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if len(blocks[0].Refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(blocks[0].Refs))
	}
	if blocks[0].Refs[0].RefType != "link" || blocks[0].Refs[0].URL != "https://example.com" {
		t.Errorf("unexpected ref: %+v", blocks[0].Refs[0])
	}
	if strings.Contains(blocks[0].Text, "https://example.com") {
		t.Errorf("URL should have been stripped from text: %q", blocks[0].Text)
	}
}

func TestParseDocxExtractsHyperlinkRefs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.docx")
	files := map[string]string{
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com" TargetMode="External"/>
</Relationships>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <w:body>
    <w:p>
      <w:hyperlink r:id="rId1">
        <w:r><w:t>Visit Example</w:t></w:r>
      </w:hyperlink>
    </w:p>
    <w:p><w:r><w:t>Normal paragraph.</w:t></w:r></w:p>
  </w:body>
</w:document>`,
	}
	if err := writeZipFixture(path, files); err != nil {
		t.Fatalf("write docx fixture: %v", err)
	}

	got, err := Parse(path, "docx", "")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(got.Blocks) == 0 {
		t.Fatal("expected at least one block")
	}
	var allRefs []model.ResourceRef
	for _, b := range got.Blocks {
		allRefs = append(allRefs, b.Refs...)
	}
	if len(allRefs) != 1 {
		t.Fatalf("expected 1 ref, got %d: %+v", len(allRefs), allRefs)
	}
	if allRefs[0].RefType != "link" || allRefs[0].AnchorText != "Visit Example" ||
		allRefs[0].URL != "https://example.com" {
		t.Errorf("unexpected ref: %+v", allRefs[0])
	}
}

func TestParseDocxExtractsImageRef(t *testing.T) {
	path := filepath.Join(t.TempDir(), "img.docx")
	files := map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing">
  <w:body>
    <w:p>
      <w:r>
        <w:drawing>
          <wp:inline><wp:docPr id="1" name="fig1" descr="Figure caption"/></wp:inline>
        </w:drawing>
      </w:r>
    </w:p>
    <w:p><w:r><w:t>后续段落。</w:t></w:r></w:p>
  </w:body>
</w:document>`,
	}
	if err := writeZipFixture(path, files); err != nil {
		t.Fatalf("write docx fixture: %v", err)
	}

	got, err := Parse(path, "docx", "")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	var allRefs []model.ResourceRef
	for _, b := range got.Blocks {
		allRefs = append(allRefs, b.Refs...)
	}
	if len(allRefs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(allRefs))
	}
	if allRefs[0].RefType != "image" || allRefs[0].Label != "Figure caption" {
		t.Errorf("unexpected ref: %+v", allRefs[0])
	}
	// placeholder text should appear in the document text
	if !strings.Contains(got.Text, "[Image: Figure caption]") {
		t.Errorf("expected image placeholder in text, got: %q", got.Text)
	}
}

func TestParsePptxExtractsImageRef(t *testing.T) {
	path := filepath.Join(t.TempDir(), "img.pptx")
	files := map[string]string{
		"ppt/slides/slide1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
       xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main">
  <p:cSld><p:spTree>
    <p:sp><p:txBody><a:p><a:r><a:t>Slide text</a:t></a:r></a:p></p:txBody></p:sp>
    <p:pic>
      <p:nvPicPr><p:cNvPr id="3" name="Picture 1" descr="Chart overview"/></p:nvPicPr>
    </p:pic>
  </p:spTree></p:cSld>
</p:sld>`,
	}
	if err := writeZipFixture(path, files); err != nil {
		t.Fatalf("write pptx fixture: %v", err)
	}

	got, err := Parse(path, "pptx", "")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(got.Blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(got.Blocks))
	}
	if len(got.Blocks[0].Refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(got.Blocks[0].Refs))
	}
	ref := got.Blocks[0].Refs[0]
	if ref.RefType != "image" || ref.Label != "Chart overview" {
		t.Errorf("unexpected ref: %+v", ref)
	}
	if !strings.Contains(got.Blocks[0].Text, "[Image: Chart overview]") {
		t.Errorf("expected image placeholder in block text: %q", got.Blocks[0].Text)
	}
}

func TestParseDocxExtractsImageToStoragePath(t *testing.T) {
	imgData := []byte{0x89, 0x50, 0x4e, 0x47} // minimal PNG header bytes
	path := filepath.Join(t.TempDir(), "img.docx")
	files := map[string]string{
		"word/_rels/document.xml.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/img.png"/>
</Relationships>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
            xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
            xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
            xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <w:body>
    <w:p>
      <w:r>
        <w:drawing>
          <wp:inline><wp:docPr id="1" name="fig" descr="Test image"/></wp:inline>
          <a:blip r:embed="rId1"/>
        </w:drawing>
      </w:r>
    </w:p>
    <w:p><w:r><w:t>Content.</w:t></w:r></w:p>
  </w:body>
</w:document>`,
	}
	// write image bytes as a real zip entry
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(content))
	}
	w, _ := zw.Create("word/media/img.png")
	_, _ = w.Write(imgData)
	_ = zw.Close()
	_ = f.Close()

	staticBase := t.TempDir()                            // acts as data/static/
	mediaDir := filepath.Join(staticBase, "doc-abc-123") // data/static/{docID}
	got, err := Parse(path, "docx", mediaDir)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	var imgRef *model.ResourceRef
	for bi := range got.Blocks {
		for ri := range got.Blocks[bi].Refs {
			r := &got.Blocks[bi].Refs[ri]
			if r.RefType == "image" {
				imgRef = r
			}
		}
	}
	if imgRef == nil {
		t.Fatal("no image ref found")
	}
	if imgRef.StoragePath == "" {
		t.Fatal("StoragePath should be set after media extraction")
	}
	// StoragePath must be relative to staticBase (data/static/), not absolute
	if filepath.IsAbs(imgRef.StoragePath) {
		t.Errorf("StoragePath should be relative, got: %q", imgRef.StoragePath)
	}
	// must start with the docID segment
	if !strings.HasPrefix(imgRef.StoragePath, "doc-abc-123/") {
		t.Errorf("StoragePath should start with docID, got: %q", imgRef.StoragePath)
	}
	diskPath := filepath.Join(staticBase, imgRef.StoragePath)
	if _, statErr := os.Stat(diskPath); statErr != nil {
		t.Fatalf("image file not written to disk at %q: %v", diskPath, statErr)
	}
	disk, _ := os.ReadFile(diskPath)
	if string(disk) != string(imgData) {
		t.Error("written image content mismatch")
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

func TestCleanTextSpecialChars(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"nbsp mid-word", "foo bar", "foo bar"},
		{"nbsp trimmed", " text ", "text"},
		{"soft hyphen removed", "computa­tion", "computation"},
		{"zero-width space removed", "foo​bar", "foobar"},
		{"zero-width non-joiner removed", "foo‌bar", "foobar"},
		{"zero-width joiner removed", "foo‍bar", "foobar"},
		{"word joiner removed", "foo⁠bar", "foobar"},
		{"replacement char removed", "ok�end", "okend"},
		{"object replacement removed", "ok￼end", "okend"},
		{"line separator to newline", "line1 line2", "line1\nline2"},
		{"para separator to blank line", "para1 para2", "para1\n\npara2"},
		{"unmapped PUA removed", "text", "text"},
		{"mapped PUA kept as bullet", " item", "• item"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CleanText(c.input)
			if got != c.want {
				t.Fatalf("CleanText(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}
