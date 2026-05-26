package llm

import (
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
	if err := exec.Command("python3", "-c", "import pypdf").Run(); err != nil {
		t.Skipf("pypdf not available: %v", err)
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
