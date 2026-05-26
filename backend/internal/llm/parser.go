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

// ParseBlock is a structural unit from the parser (a section, slide, etc.)
// carrying optional metadata for the chunker.
type ParseBlock struct {
	Text         string
	SectionTitle string
	PageStart    int
}

type ParseResult struct {
	Text      string       `json:"text"`
	PageCount int          `json:"page_count"`
	Blocks    []ParseBlock // nil for DOCX/PDF; populated for Markdown and PPTX
}

type ooxmlBlock struct {
	kind string
	text string
	rows [][]string
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
	return ParseResult{Text: text, PageCount: 1, Blocks: splitMarkdownBlocks(text)}, nil
}

// splitMarkdownBlocks splits Markdown text into blocks at heading boundaries.
// Each block carries the heading text as SectionTitle.
func splitMarkdownBlocks(text string) []ParseBlock {
	lines := strings.Split(text, "\n")
	var blocks []ParseBlock
	var currentTitle string
	var currentLines []string

	for _, line := range lines {
		if isMarkdownHeading(line) {
			if t := strings.TrimSpace(strings.Join(currentLines, "\n")); t != "" {
				blocks = append(blocks, ParseBlock{Text: t, SectionTitle: currentTitle})
			}
			currentTitle = strings.TrimSpace(strings.TrimLeft(line, "#"))
			currentLines = []string{line}
		} else {
			currentLines = append(currentLines, line)
		}
	}
	if t := strings.TrimSpace(strings.Join(currentLines, "\n")); t != "" {
		blocks = append(blocks, ParseBlock{Text: t, SectionTitle: currentTitle})
	}
	if len(blocks) == 0 {
		return []ParseBlock{{Text: text}}
	}
	return blocks
}

func isMarkdownHeading(line string) bool {
	if !strings.HasPrefix(line, "#") {
		return false
	}
	rest := strings.TrimLeft(line, "#")
	return rest == "" || rest[0] == ' ' || rest[0] == '\t'
}

func parseDocx(path string) (ParseResult, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return ParseResult{}, err
	}
	defer reader.Close()

	var blocks []ParseBlock
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		content, err := readZipFile(file)
		if err != nil {
			return ParseResult{}, err
		}
		blocks = extractDocxBlocks(string(content))
	}

	if len(blocks) == 0 {
		return ParseResult{}, errors.New("docx content is empty")
	}
	texts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b.Text != "" {
			texts = append(texts, b.Text)
		}
	}
	text := strings.TrimSpace(strings.Join(texts, "\n\n"))
	if text == "" {
		return ParseResult{}, errors.New("docx content is empty")
	}
	return ParseResult{Text: text, PageCount: 1, Blocks: blocks}, nil
}

var docxHeadingStyleRe = regexp.MustCompile(`<w:pStyle\s[^>]*w:val="([^"]*)"`)

func isDocxHeadingStyle(val string) bool {
	lower := strings.ToLower(val)
	if strings.HasPrefix(lower, "heading") {
		return true
	}
	if strings.HasPrefix(lower, "标题") {
		return true
	}
	// numeric styles 1–6 are headings in many CJK DOCX templates
	if len(val) == 1 && val[0] >= '1' && val[0] <= '6' {
		return true
	}
	return false
}

// extractDocxBlocks splits a DOCX document.xml into blocks at heading paragraph
// boundaries. Each block carries the heading text as SectionTitle. When no
// heading styles are detected the entire document is returned as one block,
// preserving existing behavior.
func extractDocxBlocks(content string) []ParseBlock {
	blockRe := regexp.MustCompile(`(?s)<w:tbl[\s>].*?</w:tbl>|<w:p[\s>].*?</w:p>`)
	tableStartRe := regexp.MustCompile(`(?s)^<w:tbl[\s>]`)

	rawBlocks := blockRe.FindAllString(content, -1)

	type docItem struct {
		isHeading bool
		heading   string
		ob        ooxmlBlock
	}

	items := make([]docItem, 0, len(rawBlocks))
	for _, raw := range rawBlocks {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if tableStartRe.MatchString(raw) {
			rows := extractTableRows(raw, "w:p", "w:t", "w:tr", "w:tc")
			if len(rows) > 0 {
				items = append(items, docItem{ob: ooxmlBlock{kind: "table", rows: rows}})
			}
			continue
		}
		paraText := extractParagraphText(raw, "w:p", "w:t")
		isHeading := false
		headingText := ""
		if m := docxHeadingStyleRe.FindStringSubmatch(raw); m != nil && isDocxHeadingStyle(m[1]) {
			isHeading = true
			headingText = paraText
		}
		if paraText != "" {
			items = append(items, docItem{isHeading: isHeading, heading: headingText, ob: ooxmlBlock{kind: "text", text: paraText}})
		}
	}

	hasHeadings := false
	for _, it := range items {
		if it.isHeading {
			hasHeadings = true
			break
		}
	}
	if !hasHeadings {
		text := extractOOXMLTextWithMarkdownTables(content, "w:p", "w:t", "w:tbl", "w:tr", "w:tc", true)
		if text == "" {
			return nil
		}
		return []ParseBlock{{Text: text}}
	}

	var result []ParseBlock
	var currentTitle string
	var currentItems []ooxmlBlock

	flush := func() {
		if len(currentItems) == 0 {
			return
		}
		merged := mergeAdjacentOOXMLTables(currentItems)
		lines := make([]string, 0, len(merged))
		for _, item := range merged {
			switch item.kind {
			case "table":
				if t := renderMarkdownTable(item.rows); t != "" {
					lines = append(lines, t)
				}
			default:
				if item.text != "" {
					lines = append(lines, item.text)
				}
			}
		}
		if text := strings.TrimSpace(strings.Join(lines, "\n\n")); text != "" {
			result = append(result, ParseBlock{Text: text, SectionTitle: currentTitle})
		}
		currentItems = nil
	}

	for _, it := range items {
		if it.isHeading {
			flush()
			currentTitle = it.heading
			currentItems = []ooxmlBlock{{kind: "text", text: it.heading}}
		} else {
			currentItems = append(currentItems, it.ob)
		}
	}
	flush()

	if len(result) == 0 {
		text := extractOOXMLTextWithMarkdownTables(content, "w:p", "w:t", "w:tbl", "w:tr", "w:tc", true)
		if text == "" {
			return nil
		}
		return []ParseBlock{{Text: text}}
	}
	return result
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
			slideText[file.Name] = extractOOXMLTextWithMarkdownTables(string(content), "a:p", "a:t", "a:tbl", "a:tr", "a:tc", false)
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

	var blocks []ParseBlock
	for idx, name := range slideNames {
		section := strings.TrimSpace(slideText[name])
		noteName := filepath.Join("ppt", "notesSlides", "notesSlide"+slideNumberFromName(name)+".xml")
		note := strings.TrimSpace(noteText[noteName])
		if note != "" {
			section = strings.TrimSpace(section + "\n备注: " + note)
		}
		if section != "" {
			slideNum := idx + 1
			label := "幻灯片 " + strconv.Itoa(slideNum)
			blocks = append(blocks, ParseBlock{
				Text:         label + "\n" + section,
				SectionTitle: label,
				PageStart:    slideNum,
			})
		}
	}

	if len(blocks) == 0 {
		return ParseResult{}, errors.New("pptx content is empty")
	}

	slideTexts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		slideTexts = append(slideTexts, b.Text)
	}
	text := strings.TrimSpace(strings.Join(slideTexts, "\n\n"))
	return ParseResult{Text: text, PageCount: len(blocks), Blocks: blocks}, nil
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

	var payload struct {
		Text      string `json:"text"`
		PageCount int    `json:"page_count"`
		Pages     []struct {
			Text string `json:"text"`
			Page int    `json:"page"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(output, &payload); err != nil {
		return ParseResult{}, fmt.Errorf("pdf parser returned invalid json (script=%s file=%s): %w", scriptPath, path, err)
	}
	payload.Text = strings.TrimSpace(payload.Text)
	if payload.Text == "" {
		return ParseResult{}, errors.New("pdf content is empty")
	}
	if payload.PageCount < 1 {
		payload.PageCount = 1
	}
	blocks := make([]ParseBlock, 0, len(payload.Pages))
	for _, p := range payload.Pages {
		if t := strings.TrimSpace(p.Text); t != "" {
			blocks = append(blocks, ParseBlock{Text: t, PageStart: p.Page})
		}
	}
	return ParseResult{Text: payload.Text, PageCount: payload.PageCount, Blocks: blocks}, nil
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

func extractOOXMLTextWithMarkdownTables(content, paraTag, runTag, tableTag, rowTag, cellTag string, mergeAdjacentTables bool) string {
	blockRe := regexp.MustCompile(`(?s)<` + regexp.QuoteMeta(tableTag) + `[\s>].*?</` + regexp.QuoteMeta(tableTag) + `>|<` + regexp.QuoteMeta(paraTag) + `[\s>].*?</` + regexp.QuoteMeta(paraTag) + `>`)
	tableStartRe := regexp.MustCompile(`(?s)^<` + regexp.QuoteMeta(tableTag) + `[\s>]`)

	blocks := blockRe.FindAllString(content, -1)
	items := make([]ooxmlBlock, 0, len(blocks))
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		if tableStartRe.MatchString(block) {
			rows := extractTableRows(block, paraTag, runTag, rowTag, cellTag)
			if len(rows) > 0 {
				items = append(items, ooxmlBlock{kind: "table", rows: rows})
			}
			continue
		}
		para := extractParagraphText(block, paraTag, runTag)
		if para != "" {
			items = append(items, ooxmlBlock{kind: "text", text: para})
		}
	}
	if mergeAdjacentTables {
		items = mergeAdjacentOOXMLTables(items)
	}

	lines := make([]string, 0, len(items))
	for _, item := range items {
		if item.kind == "table" {
			if table := renderMarkdownTable(item.rows); table != "" {
				lines = append(lines, table)
			}
			continue
		}
		if item.text != "" {
			lines = append(lines, item.text)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n\n"))
}

func extractMarkdownTable(content, paraTag, runTag, rowTag, cellTag string) string {
	return renderMarkdownTable(extractTableRows(content, paraTag, runTag, rowTag, cellTag))
}

func extractTableRows(content, paraTag, runTag, rowTag, cellTag string) [][]string {
	rowRe := regexp.MustCompile(`(?s)<` + regexp.QuoteMeta(rowTag) + `[\s>].*?</` + regexp.QuoteMeta(rowTag) + `>`)
	cellRe := regexp.MustCompile(`(?s)<` + regexp.QuoteMeta(cellTag) + `[\s>].*?</` + regexp.QuoteMeta(cellTag) + `>`)

	rowMatches := rowRe.FindAllString(content, -1)
	if len(rowMatches) == 0 {
		return nil
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
		return nil
	}

	for i := range rows {
		for len(rows[i]) < maxCols {
			rows[i] = append(rows[i], "")
		}
	}
	return rows
}

func renderMarkdownTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}

	var builder strings.Builder
	builder.WriteString(formatMarkdownTableRow(rows[0]))
	builder.WriteString("\n")
	separator := make([]string, 0, len(rows[0]))
	for i := 0; i < len(rows[0]); i++ {
		separator = append(separator, "---")
	}
	builder.WriteString(formatMarkdownTableRow(separator))
	for _, row := range rows[1:] {
		builder.WriteString("\n")
		builder.WriteString(formatMarkdownTableRow(row))
	}
	return builder.String()
}

func mergeAdjacentOOXMLTables(items []ooxmlBlock) []ooxmlBlock {
	if len(items) == 0 {
		return items
	}

	merged := make([]ooxmlBlock, 0, len(items))
	for _, item := range items {
		if len(merged) == 0 {
			merged = append(merged, cloneOOXMLBlock(item))
			continue
		}

		last := &merged[len(merged)-1]
		if last.kind == "table" && item.kind == "table" && canMergeOOXMLTables(last.rows, item.rows) {
			last.rows = mergeOOXMLTableRows(last.rows, item.rows)
			continue
		}
		merged = append(merged, cloneOOXMLBlock(item))
	}
	return merged
}

func canMergeOOXMLTables(left, right [][]string) bool {
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	return len(left[0]) == len(right[0])
}

func mergeOOXMLTableRows(left, right [][]string) [][]string {
	merged := cloneTableRows(left)
	rightRows := cloneTableRows(right)
	if len(merged) > 0 && len(rightRows) > 0 && equalStringSlices(merged[0], rightRows[0]) {
		rightRows = rightRows[1:]
	}
	merged = append(merged, rightRows...)
	return merged
}

func cloneOOXMLBlock(item ooxmlBlock) ooxmlBlock {
	clone := ooxmlBlock{kind: item.kind, text: item.text}
	if item.rows != nil {
		clone.rows = cloneTableRows(item.rows)
	}
	return clone
}

func cloneTableRows(rows [][]string) [][]string {
	cloned := make([][]string, 0, len(rows))
	for _, row := range rows {
		rowCopy := append([]string(nil), row...)
		cloned = append(cloned, rowCopy)
	}
	return cloned
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
