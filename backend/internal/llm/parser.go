package llm

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"backend/internal/infra"
	"go.uber.org/zap"
)

type ParseResult struct {
	Text      string `json:"text"`
	PageCount int    `json:"page_count"`
}

var pdfGlyphReplacer = strings.NewReplacer(
	"\uf06c", "•",
	"\uf0b7", "•", // common private-use bullet from Symbol/Wingdings-like fonts
	"\uf0a7", "•",
	"\uf0d8", "•",
)

func Parse(path, sourceType string) (ParseResult, error) {
	switch sourceType {
	case "markdown":
		return parseMarkdown(path)
	case "docx":
		return parseDocx(path)
	case "pptx":
		return parsePptx(path)
	case "pdf":
		return parsePDF(path)
	default:
		return ParseResult{}, errors.New("unsupported file type")
	}
}

func parseMarkdown(path string) (ParseResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ParseResult{}, err
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return ParseResult{}, errors.New("markdown content is empty")
	}
	return ParseResult{Text: text, PageCount: 1}, nil
}

func parseDocx(path string) (ParseResult, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return ParseResult{}, err
	}
	defer reader.Close()

	texts := make([]string, 0)
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		content, err := readZipFile(file)
		if err != nil {
			return ParseResult{}, err
		}
		texts = append(texts, extractOOXMLTextWithMarkdownTables(string(content), "w:p", "w:t", "w:tbl", "w:tr", "w:tc"))
	}

	text := strings.TrimSpace(strings.Join(texts, "\n"))
	if text == "" {
		return ParseResult{}, errors.New("docx content is empty")
	}
	return ParseResult{Text: text, PageCount: 1}, nil
}

func parsePptx(path string) (ParseResult, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return ParseResult{}, err
	}
	defer reader.Close()

	slideText := map[string]string{}
	noteText := map[string]string{}

	for _, file := range reader.File {
		switch {
		case strings.HasPrefix(file.Name, "ppt/slides/slide") && strings.HasSuffix(file.Name, ".xml"):
			content, err := readZipFile(file)
			if err != nil {
				return ParseResult{}, err
			}
			slideText[file.Name] = extractOOXMLTextWithMarkdownTables(string(content), "a:p", "a:t", "a:tbl", "a:tr", "a:tc")
		case strings.HasPrefix(file.Name, "ppt/notesSlides/notesSlide") && strings.HasSuffix(file.Name, ".xml"):
			content, err := readZipFile(file)
			if err != nil {
				return ParseResult{}, err
			}
			noteText[file.Name] = extractParagraphText(string(content), "a:p", "a:t")
		}
	}

	if len(slideText) == 0 {
		return ParseResult{}, errors.New("pptx content is empty")
	}

	slideNames := make([]string, 0, len(slideText))
	for name := range slideText {
		slideNames = append(slideNames, name)
	}
	sort.Strings(slideNames)

	var sections []string
	for idx, name := range slideNames {
		section := strings.TrimSpace(slideText[name])
		noteName := filepath.Join("ppt", "notesSlides", "notesSlide"+slideNumberFromName(name)+".xml")
		note := strings.TrimSpace(noteText[noteName])
		if note != "" {
			section = strings.TrimSpace(section + "\n备注: " + note)
		}
		if section != "" {
			sections = append(sections, "幻灯片 "+strconv.Itoa(idx+1)+"\n"+section)
		}
	}

	text := strings.TrimSpace(strings.Join(sections, "\n\n"))
	if text == "" {
		return ParseResult{}, errors.New("pptx content is empty")
	}
	return ParseResult{Text: text, PageCount: len(sections)}, nil
}

func parsePDF(path string) (ParseResult, error) {
	pythonBin := strings.TrimSpace(os.Getenv("PDF_PARSER_PYTHON"))
	if pythonBin == "" {
		pythonBin = "python3"
	}

	scriptPath := strings.TrimSpace(os.Getenv("PDF_PARSER_SCRIPT"))
	if scriptPath == "" {
		var err error
		scriptPath, err = pdfParserScriptPath()
		if err != nil {
			return ParseResult{}, err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	infra.L.Info("running pdf parser",
		zap.String("python", pythonBin),
		zap.String("script", scriptPath),
		zap.String("file", path),
		zap.Strings("argv", []string{pythonBin, scriptPath, path}),
	)

	cmd := exec.CommandContext(ctx, pythonBin, scriptPath, path)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			msg := strings.TrimSpace(string(exitErr.Stderr))
			if msg == "" {
				msg = err.Error()
			}
			return ParseResult{}, fmt.Errorf("pdf parser failed (python=%s script=%s file=%s): %s", pythonBin, scriptPath, path, msg)
		}
		if ctx.Err() == context.DeadlineExceeded {
			return ParseResult{}, fmt.Errorf("pdf parser timed out (python=%s script=%s file=%s)", pythonBin, scriptPath, path)
		}
		return ParseResult{}, fmt.Errorf("pdf parser exec failed (python=%s script=%s file=%s): %w", pythonBin, scriptPath, path, err)
	}

	var result ParseResult
	if err := json.Unmarshal(output, &result); err != nil {
		return ParseResult{}, fmt.Errorf("pdf parser returned invalid json (script=%s file=%s): %w", scriptPath, path, err)
	}
	result.Text = strings.TrimSpace(result.Text)
	if result.Text == "" {
		return ParseResult{}, errors.New("pdf content is empty")
	}
	if result.PageCount < 1 {
		result.PageCount = 1
	}
	return result, nil
}

func pdfParserScriptPath() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("resolve pdf parser script path: runtime caller unavailable")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "scripts", "parse_pdf.py"))
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

func CleanText(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	input = pdfGlyphReplacer.Replace(input)

	var builder strings.Builder
	lastBlank := false
	for _, line := range strings.Split(input, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if lastBlank {
				continue
			}
			lastBlank = true
			builder.WriteString("\n")
			continue
		}
		lastBlank = false
		builder.WriteString(trimmed)
		builder.WriteString("\n")
	}

	cleaned := strings.TrimSpace(builder.String())
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, cleaned)
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// extractParagraphText splits XML by paragraph tag (e.g. "w:p" or "a:p"), then
// concatenates all run tags (e.g. "w:t" or "a:t") within each paragraph without
// separators. Paragraphs are joined with newlines. This preserves words that Word
// splits across multiple runs due to mixed formatting.
func extractParagraphText(content, paraTag, runTag string) string {
	paraRe := regexp.MustCompile(`(?s)<` + regexp.QuoteMeta(paraTag) + `[\s>].*?</` + regexp.QuoteMeta(paraTag) + `>`)
	runRe := regexp.MustCompile(`(?s)<` + regexp.QuoteMeta(runTag) + `[^>]*>(.*?)</` + regexp.QuoteMeta(runTag) + `>`)

	paras := paraRe.FindAllString(content, -1)
	lines := make([]string, 0, len(paras))
	for _, para := range paras {
		runs := runRe.FindAllStringSubmatch(para, -1)
		var sb strings.Builder
		for _, r := range runs {
			if len(r) >= 2 {
				sb.WriteString(htmlUnescape(stripXML(r[1])))
			}
		}
		if line := strings.TrimSpace(sb.String()); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func extractOOXMLTextWithMarkdownTables(content, paraTag, runTag, tableTag, rowTag, cellTag string) string {
	blockRe := regexp.MustCompile(`(?s)<` + regexp.QuoteMeta(tableTag) + `[\s>].*?</` + regexp.QuoteMeta(tableTag) + `>|<` + regexp.QuoteMeta(paraTag) + `[\s>].*?</` + regexp.QuoteMeta(paraTag) + `>`)
	tableStartRe := regexp.MustCompile(`(?s)^<` + regexp.QuoteMeta(tableTag) + `[\s>]`)

	blocks := blockRe.FindAllString(content, -1)
	lines := make([]string, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if tableStartRe.MatchString(block) {
			table := extractMarkdownTable(block, paraTag, runTag, rowTag, cellTag)
			if table != "" {
				lines = append(lines, table)
			}
			continue
		}
		para := extractParagraphText(block, paraTag, runTag)
		if para != "" {
			lines = append(lines, para)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n\n"))
}

func extractMarkdownTable(content, paraTag, runTag, rowTag, cellTag string) string {
	rowRe := regexp.MustCompile(`(?s)<` + regexp.QuoteMeta(rowTag) + `[\s>].*?</` + regexp.QuoteMeta(rowTag) + `>`)
	cellRe := regexp.MustCompile(`(?s)<` + regexp.QuoteMeta(cellTag) + `[\s>].*?</` + regexp.QuoteMeta(cellTag) + `>`)

	rowMatches := rowRe.FindAllString(content, -1)
	if len(rowMatches) == 0 {
		return ""
	}

	rows := make([][]string, 0, len(rowMatches))
	maxCols := 0
	for _, row := range rowMatches {
		cellMatches := cellRe.FindAllString(row, -1)
		if len(cellMatches) == 0 {
			continue
		}

		cells := make([]string, 0, len(cellMatches))
		for _, cell := range cellMatches {
			text := extractParagraphText(cell, paraTag, runTag)
			text = strings.ReplaceAll(text, "\n", "<br>")
			text = strings.TrimSpace(text)
			text = strings.ReplaceAll(text, "|", `\|`)
			cells = append(cells, text)
		}
		rows = append(rows, cells)
		maxCols = max(maxCols, len(cells))
	}
	if len(rows) == 0 || maxCols == 0 {
		return ""
	}

	for i := range rows {
		for len(rows[i]) < maxCols {
			rows[i] = append(rows[i], "")
		}
	}

	var builder strings.Builder
	builder.WriteString(formatMarkdownTableRow(rows[0]))
	builder.WriteString("\n")
	separator := make([]string, 0, maxCols)
	for i := 0; i < maxCols; i++ {
		separator = append(separator, "---")
	}
	builder.WriteString(formatMarkdownTableRow(separator))
	for _, row := range rows[1:] {
		builder.WriteString("\n")
		builder.WriteString(formatMarkdownTableRow(row))
	}
	return builder.String()
}

func formatMarkdownTableRow(cells []string) string {
	var builder strings.Builder
	builder.WriteString("|")
	for _, cell := range cells {
		builder.WriteString(" ")
		builder.WriteString(strings.TrimSpace(cell))
		builder.WriteString(" |")
	}
	return builder.String()
}

func stripXML(input string) string {
	re := regexp.MustCompile(`<[^>]+>`)
	return re.ReplaceAllString(input, "")
}

func htmlUnescape(input string) string {
	replacer := strings.NewReplacer(
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&apos;", "'",
	)
	return replacer.Replace(input)
}

func slideNumberFromName(name string) string {
	base := filepath.Base(name)
	base = strings.TrimPrefix(base, "slide")
	return strings.TrimSuffix(base, ".xml")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
