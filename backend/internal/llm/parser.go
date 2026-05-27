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
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"backend/internal/infra"
	"backend/internal/model"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ParseBlock is a structural unit from the parser (a section, slide, etc.)
// carrying optional metadata for the chunker.
type ParseBlock struct {
	Text         string
	SectionTitle string
	PageStart    int
	Refs         []model.ResourceRef
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
	refs []model.ResourceRef
}

var pdfGlyphReplacer = strings.NewReplacer(
	"\uf06c", "•",
	"\uf0b7", "•", // common private-use bullet from Symbol/Wingdings-like fonts
	"\uf0a7", "•",
	"\uf0d8", "•",
)

// Parse parses the file at path according to sourceType. mediaDir, when
// non-empty, is a directory where embedded images are extracted and saved;
// image ResourceRefs will have StoragePath set to the written file path.
func Parse(path, sourceType, mediaDir string) (ParseResult, error) {
	switch sourceType {
	case "markdown":
		return parseMarkdown(path)
	case "docx":
		return parseDocx(path, mediaDir)
	case "pptx":
		return parsePptx(path, mediaDir)
	case "pdf":
		return parsePDF(path, mediaDir)
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
// Each block carries the heading text as SectionTitle. Links and images are
// extracted into Refs; image syntax is replaced with a placeholder, and link
// URLs are stripped from the body text (anchor text is preserved).
func splitMarkdownBlocks(text string) []ParseBlock {
	lines := strings.Split(text, "\n")
	var blocks []ParseBlock
	var currentTitle string
	var currentLines []string

	flush := func() {
		raw := strings.TrimSpace(strings.Join(currentLines, "\n"))
		if raw == "" {
			return
		}
		cleanText, refs := extractMarkdownRefs(raw)
		blocks = append(blocks, ParseBlock{Text: cleanText, SectionTitle: currentTitle, Refs: refs})
	}

	for _, line := range lines {
		if isMarkdownHeading(line) {
			flush()
			currentTitle = strings.TrimSpace(strings.TrimLeft(line, "#"))
			currentLines = []string{line}
		} else {
			currentLines = append(currentLines, line)
		}
	}
	flush()
	if len(blocks) == 0 {
		cleanText, refs := extractMarkdownRefs(text)
		return []ParseBlock{{Text: cleanText, Refs: refs}}
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

func parseDocx(path, mediaDir string) (ParseResult, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return ParseResult{}, err
	}
	defer reader.Close()

	var docContent string
	var docRels map[string]string
	var docImageRels map[string]string
	for _, file := range reader.File {
		switch file.Name {
		case "word/_rels/document.xml.rels":
			content, readErr := readZipFile(file)
			if readErr == nil {
				s := string(content)
				docRels = parseOOXMLRels(s)
				docImageRels = parseOOXMLImageRels(s, "word")
			}
		case "word/document.xml":
			content, readErr := readZipFile(file)
			if readErr != nil {
				return ParseResult{}, readErr
			}
			docContent = string(content)
		}
	}

	if docContent == "" {
		return ParseResult{}, errors.New("docx content is empty")
	}
	blocks := extractDocxBlocks(docContent, docRels, docImageRels)
	if mediaDir != "" {
		resolveZIPMedia(&reader.Reader, blocks, mediaDir)
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
func extractDocxBlocks(content string, rels map[string]string, imageRels map[string]string) []ParseBlock {
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
		imgPlaceholder, paraRefs := extractDocxParaRefs(raw, rels, imageRels)
		if paraText == "" {
			paraText = imgPlaceholder
		}
		isHeading := false
		headingText := ""
		if m := docxHeadingStyleRe.FindStringSubmatch(raw); m != nil && isDocxHeadingStyle(m[1]) {
			isHeading = true
			headingText = paraText
		}
		if paraText != "" {
			items = append(items, docItem{isHeading: isHeading, heading: headingText, ob: ooxmlBlock{kind: "text", text: paraText, refs: paraRefs}})
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
		ooxmlItems := make([]ooxmlBlock, 0, len(items))
		for _, it := range items {
			ooxmlItems = append(ooxmlItems, it.ob)
		}
		return buildDocxFallbackBlock(ooxmlItems)
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
		var blockRefs []model.ResourceRef
		for _, item := range merged {
			blockRefs = append(blockRefs, item.refs...)
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
			result = append(result, ParseBlock{Text: text, SectionTitle: currentTitle, Refs: blockRefs})
		}
		currentItems = nil
	}

	for _, it := range items {
		if it.isHeading {
			flush()
			currentTitle = it.heading
			currentItems = []ooxmlBlock{{kind: "text", text: it.heading, refs: it.ob.refs}}
		} else {
			currentItems = append(currentItems, it.ob)
		}
	}
	flush()

	if len(result) == 0 {
		ooxmlItems := make([]ooxmlBlock, 0, len(items))
		for _, it := range items {
			ooxmlItems = append(ooxmlItems, it.ob)
		}
		return buildDocxFallbackBlock(ooxmlItems)
	}
	return result
}

// buildDocxFallbackBlock builds a single ParseBlock from pre-extracted ooxmlBlocks,
// preserving image placeholder text and refs that extractOOXMLTextWithMarkdownTables
// would otherwise lose.
func buildDocxFallbackBlock(ooxmlItems []ooxmlBlock) []ParseBlock {
	merged := mergeAdjacentOOXMLTables(ooxmlItems)
	lines := make([]string, 0, len(merged))
	var allRefs []model.ResourceRef
	for _, item := range merged {
		allRefs = append(allRefs, item.refs...)
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
	text := strings.TrimSpace(strings.Join(lines, "\n\n"))
	if text == "" {
		return nil
	}
	return []ParseBlock{{Text: text, Refs: allRefs}}
}

func parsePptx(path, mediaDir string) (ParseResult, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return ParseResult{}, err
	}
	defer reader.Close()

	rawSlides := map[string]string{}
	rawNotes := map[string]string{}
	rawRels := map[string]string{}

	for _, file := range reader.File {
		isSlide := strings.HasPrefix(file.Name, "ppt/slides/slide") && strings.HasSuffix(file.Name, ".xml")
		isNote := strings.HasPrefix(file.Name, "ppt/notesSlides/notesSlide") && strings.HasSuffix(file.Name, ".xml")
		isRels := strings.HasPrefix(file.Name, "ppt/slides/_rels/slide") && strings.HasSuffix(file.Name, ".rels")
		if !isSlide && !isNote && !isRels {
			continue
		}
		content, readErr := readZipFile(file)
		if readErr != nil {
			if isSlide || isNote {
				return ParseResult{}, readErr
			}
			continue
		}
		switch {
		case isSlide:
			rawSlides[file.Name] = string(content)
		case isNote:
			rawNotes[file.Name] = string(content)
		case isRels:
			rawRels[file.Name] = string(content)
		}
	}

	if len(rawSlides) == 0 {
		return ParseResult{}, errors.New("pptx content is empty")
	}

	slideRels := map[string]map[string]string{}
	slideImageRels := map[string]map[string]string{}
	for relsName, content := range rawRels {
		slideName := slideNameFromRels(relsName)
		slideRels[slideName] = parseOOXMLRels(content)
		slideImageRels[slideName] = parseOOXMLImageRels(content, "ppt/slides")
	}

	slideText := map[string]string{}
	slideRefs := map[string][]model.ResourceRef{}
	for slideName, content := range rawSlides {
		slideText[slideName] = extractOOXMLTextWithMarkdownTables(content, "a:p", "a:t", "a:tbl", "a:tr", "a:tc", false)
		slideRefs[slideName] = extractPptxSlideRefs(content, slideRels[slideName], slideImageRels[slideName])
	}

	noteText := map[string]string{}
	for noteName, content := range rawNotes {
		noteText[noteName] = extractParagraphText(content, "a:p", "a:t")
	}

	slideNames := make([]string, 0, len(slideText))
	for name := range slideText {
		slideNames = append(slideNames, name)
	}
	sort.Strings(slideNames)

	var blocks []ParseBlock
	for idx, name := range slideNames {
		section := strings.TrimSpace(slideText[name])
		refs := slideRefs[name]
		// append image placeholders so the text reflects image presence
		for _, ref := range refs {
			if ref.RefType == "image" {
				placeholder := "[图片]"
				if ref.Label != "" {
					placeholder = "[图片: " + ref.Label + "]"
				}
				section = strings.TrimSpace(section + "\n" + placeholder)
			}
		}
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
				Refs:         refs,
			})
		}
	}

	if len(blocks) == 0 {
		return ParseResult{}, errors.New("pptx content is empty")
	}

	if mediaDir != "" {
		resolveZIPMedia(&reader.Reader, blocks, mediaDir)
	}

	slideTexts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		slideTexts = append(slideTexts, b.Text)
	}
	text := strings.TrimSpace(strings.Join(slideTexts, "\n\n"))
	return ParseResult{Text: text, PageCount: len(blocks), Blocks: blocks}, nil
}

func parsePDF(path, mediaDir string) (ParseResult, error) {
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

	args := []string{scriptPath, path}
	if mediaDir != "" {
		args = append(args, mediaDir)
	}
	infra.L.Info("running pdf parser",
		zap.String("python", pythonBin),
		zap.String("script", scriptPath),
		zap.String("file", path),
		zap.String("media_dir", mediaDir),
		zap.Strings("argv", append([]string{pythonBin}, args...)),
	)

	cmd := exec.CommandContext(ctx, pythonBin, args...)
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
			Text string              `json:"text"`
			Page int                 `json:"page"`
			Refs []model.ResourceRef `json:"refs"`
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
		t := strings.TrimSpace(p.Text)
		if t == "" && len(p.Refs) == 0 {
			continue
		}
		refs := p.Refs
		if refs == nil {
			refs = []model.ResourceRef{}
		}
		blocks = append(blocks, ParseBlock{Text: t, PageStart: p.Page, Refs: refs})
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
			last.refs = append(last.refs, item.refs...)
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
	if item.refs != nil {
		clone.refs = append([]model.ResourceRef(nil), item.refs...)
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

// --- ref extraction helpers ---

var (
	mdImageRefRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]*)\)`)
	mdLinkRefRe  = regexp.MustCompile(`\[([^\]]+)\]\(([^)]*)\)`)

	relsElemRe   = regexp.MustCompile(`(?s)<Relationship\s[^<]*/?>`)
	relsIDRe     = regexp.MustCompile(`\bId="([^"]*)"`)
	relsTargetRe = regexp.MustCompile(`\bTarget="([^"]*)"`)
	relsTypeRe   = regexp.MustCompile(`\bType="([^"]*)"`)

	docxHyperlinkRe      = regexp.MustCompile(`(?s)<w:hyperlink\s([^>]*)>(.*?)</w:hyperlink>`)
	docxHyperlinkRidAttr = regexp.MustCompile(`r:id="([^"]*)"`)
	docxRunTextRe        = regexp.MustCompile(`(?s)<w:t[^>]*>([^<]*)</w:t>`)
	docxDrawingRe        = regexp.MustCompile(`<w:drawing[\s>]`)
	docxDocPrDescrRe     = regexp.MustCompile(`<wp:docPr\b[^>]*\bdescr="([^"]*)"`)
	docxDocPrTitleRe     = regexp.MustCompile(`<wp:docPr\b[^>]*\btitle="([^"]*)"`)

	pptxRunRe        = regexp.MustCompile(`(?s)<a:r\b[^>]*>(.*?)</a:r>`)
	pptxHlinkClickRe = regexp.MustCompile(`<a:hlinkClick\b[^>]*r:id="([^"]*)"`)
	pptxRunTextRe    = regexp.MustCompile(`(?s)<a:t[^>]*>([^<]*)</a:t>`)
	pptxPicRe        = regexp.MustCompile(`(?s)<p:pic\b.*?</p:pic>`)
	pptxCNvPrDescrRe = regexp.MustCompile(`<p:cNvPr\b[^>]*\bdescr="([^"]*)"`)
	pptxCNvPrNameRe  = regexp.MustCompile(`<p:cNvPr\b[^>]*\bname="([^"]*)"`)

	// shared between DOCX and PPTX: extracts r:embed from <a:blip>
	blipEmbedRe = regexp.MustCompile(`<a:blip\b[^>]*\br:embed="([^"]*)"`)
)

func isExternalURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

// parseOOXMLRels parses an OOXML .rels file and returns a map of rId → target URL
// for hyperlink relationships only.
func parseOOXMLRels(content string) map[string]string {
	rels := map[string]string{}
	for _, elem := range relsElemRe.FindAllString(content, -1) {
		idM := relsIDRe.FindStringSubmatch(elem)
		targetM := relsTargetRe.FindStringSubmatch(elem)
		typeM := relsTypeRe.FindStringSubmatch(elem)
		if idM == nil || targetM == nil || typeM == nil {
			continue
		}
		if strings.Contains(typeM[1], "/hyperlink") {
			rels[idM[1]] = targetM[1]
		}
	}
	return rels
}

// parseOOXMLImageRels parses an OOXML .rels file and returns a map of
// rId → ZIP-internal file path for image relationships. baseDir is the
// directory containing the rels file's parent (e.g. "word" for DOCX,
// "ppt/slides" for PPTX) used to resolve relative targets.
func parseOOXMLImageRels(content, baseDir string) map[string]string {
	rels := map[string]string{}
	for _, elem := range relsElemRe.FindAllString(content, -1) {
		idM := relsIDRe.FindStringSubmatch(elem)
		targetM := relsTargetRe.FindStringSubmatch(elem)
		typeM := relsTypeRe.FindStringSubmatch(elem)
		if idM == nil || targetM == nil || typeM == nil {
			continue
		}
		if strings.Contains(typeM[1], "/image") {
			zipPath := path.Join(baseDir, targetM[1])
			rels[idM[1]] = zipPath
		}
	}
	return rels
}

// resolveZIPMedia walks all image refs in blocks whose StoragePath holds a
// temporary ZIP-internal path, extracts the file from the ZIP, writes it to
// mediaDir, and replaces StoragePath with a path relative to mediaDir's parent
// (i.e. relative to data/static/ when mediaDir is data/static/{docID}).
func resolveZIPMedia(reader *zip.Reader, blocks []ParseBlock, mediaDir string) {
	mediaDir = filepath.Clean(mediaDir)
	if err := os.MkdirAll(mediaDir, 0o755); err != nil {
		return
	}
	storageBase := filepath.Dir(mediaDir)
	seen := map[string]string{} // zipPath → relative storage path (or "" on failure)
	for bi := range blocks {
		for ri := range blocks[bi].Refs {
			ref := &blocks[bi].Refs[ri]
			if ref.RefType != "image" || ref.StoragePath == "" {
				continue
			}
			zipPath := ref.StoragePath
			if relPath, ok := seen[zipPath]; ok {
				ref.StoragePath = relPath
				continue
			}
			data := readFromZIP(reader, zipPath)
			if len(data) == 0 {
				ref.StoragePath = ""
				seen[zipPath] = ""
				continue
			}
			diskPath := filepath.Join(mediaDir, filepath.Base(zipPath))
			if err := os.WriteFile(diskPath, data, 0o644); err != nil {
				ref.StoragePath = ""
				seen[zipPath] = ""
				continue
			}
			relPath, err := filepath.Rel(storageBase, diskPath)
			if err != nil {
				relPath = diskPath
			}
			ref.StoragePath = relPath
			seen[zipPath] = relPath
		}
	}
}

func readFromZIP(reader *zip.Reader, name string) []byte {
	for _, f := range reader.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil
			}
			defer rc.Close()
			data, _ := io.ReadAll(rc)
			return data
		}
	}
	return nil
}

// extractMarkdownRefs strips link URLs and image syntax from Markdown text,
// replacing images with placeholder text and returning extracted refs.
func extractMarkdownRefs(text string) (string, []model.ResourceRef) {
	var refs []model.ResourceRef

	// images before links: image syntax is a superset of link syntax
	text = mdImageRefRe.ReplaceAllStringFunc(text, func(match string) string {
		m := mdImageRefRe.FindStringSubmatch(match)
		alt, url := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		refs = append(refs, model.ResourceRef{
			RefID:      uuid.Must(uuid.NewV7()).String(),
			RefType:    "image",
			Label:      alt,
			URL:        url,
			IsExternal: isExternalURL(url),
		})
		if alt != "" {
			return "[图片: " + alt + "]"
		}
		return "[图片]"
	})

	// links: keep anchor text, strip URL
	text = mdLinkRefRe.ReplaceAllStringFunc(text, func(match string) string {
		m := mdLinkRefRe.FindStringSubmatch(match)
		anchor, url := strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		refs = append(refs, model.ResourceRef{
			RefID:      uuid.Must(uuid.NewV7()).String(),
			RefType:    "link",
			AnchorText: anchor,
			URL:        url,
			IsExternal: isExternalURL(url),
		})
		return anchor
	})

	return text, refs
}

// extractDocxParaRefs extracts hyperlink and image refs from a DOCX paragraph
// XML fragment. It also returns an image placeholder string when the paragraph
// contains a drawing but no text runs (so the caller can substitute it).
// imageRels maps rId to ZIP-internal path; image refs have StoragePath set to
// that path as a temporary value resolved later by resolveZIPMedia.
func extractDocxParaRefs(paraXML string, rels map[string]string, imageRels map[string]string) (imgPlaceholder string, refs []model.ResourceRef) {
	for _, m := range docxHyperlinkRe.FindAllStringSubmatch(paraXML, -1) {
		attrs, inner := m[1], m[2]
		url := ""
		if rm := docxHyperlinkRidAttr.FindStringSubmatch(attrs); rm != nil && rels != nil {
			url = rels[rm[1]]
		}
		var parts []string
		for _, tm := range docxRunTextRe.FindAllStringSubmatch(inner, -1) {
			parts = append(parts, tm[1])
		}
		anchor := strings.TrimSpace(strings.Join(parts, ""))
		if anchor == "" && url == "" {
			continue
		}
		refs = append(refs, model.ResourceRef{
			RefID:      uuid.Must(uuid.NewV7()).String(),
			RefType:    "link",
			AnchorText: anchor,
			URL:        url,
			IsExternal: isExternalURL(url),
		})
	}

	if docxDrawingRe.MatchString(paraXML) {
		label := ""
		if m := docxDocPrDescrRe.FindStringSubmatch(paraXML); m != nil {
			label = htmlUnescape(m[1])
		} else if m := docxDocPrTitleRe.FindStringSubmatch(paraXML); m != nil {
			label = htmlUnescape(m[1])
		}
		if label != "" {
			imgPlaceholder = "[图片: " + label + "]"
		} else {
			imgPlaceholder = "[图片]"
		}
		storePath := ""
		if imageRels != nil {
			if bm := blipEmbedRe.FindStringSubmatch(paraXML); bm != nil {
				storePath = imageRels[bm[1]]
			}
		}
		refs = append(refs, model.ResourceRef{
			RefID:       uuid.Must(uuid.NewV7()).String(),
			RefType:     "image",
			Label:       label,
			StoragePath: storePath,
		})
	}

	return imgPlaceholder, refs
}

// extractPptxSlideRefs extracts hyperlink and image refs from a PPTX slide XML.
// imageRels maps rId to ZIP-internal path; image refs have StoragePath set to
// that path as a temporary value resolved later by resolveZIPMedia.
func extractPptxSlideRefs(slideXML string, rels map[string]string, imageRels map[string]string) []model.ResourceRef {
	var refs []model.ResourceRef

	for _, m := range pptxRunRe.FindAllStringSubmatch(slideXML, -1) {
		inner := m[1]
		hm := pptxHlinkClickRe.FindStringSubmatch(inner)
		if hm == nil {
			continue
		}
		url := ""
		if rels != nil {
			url = rels[hm[1]]
		}
		var parts []string
		for _, tm := range pptxRunTextRe.FindAllStringSubmatch(inner, -1) {
			parts = append(parts, tm[1])
		}
		anchor := strings.TrimSpace(strings.Join(parts, ""))
		if anchor == "" && url == "" {
			continue
		}
		refs = append(refs, model.ResourceRef{
			RefID:      uuid.Must(uuid.NewV7()).String(),
			RefType:    "link",
			AnchorText: anchor,
			URL:        url,
			IsExternal: isExternalURL(url),
		})
	}

	for _, picXML := range pptxPicRe.FindAllString(slideXML, -1) {
		label := ""
		if m := pptxCNvPrDescrRe.FindStringSubmatch(picXML); m != nil {
			label = htmlUnescape(m[1])
		} else if m := pptxCNvPrNameRe.FindStringSubmatch(picXML); m != nil {
			label = htmlUnescape(m[1])
		}
		storePath := ""
		if imageRels != nil {
			if bm := blipEmbedRe.FindStringSubmatch(picXML); bm != nil {
				storePath = imageRels[bm[1]]
			}
		}
		refs = append(refs, model.ResourceRef{
			RefID:       uuid.Must(uuid.NewV7()).String(),
			RefType:     "image",
			Label:       label,
			StoragePath: storePath,
		})
	}

	return refs
}

// slideNameFromRels derives the slide file path from its rels file path.
// e.g. "ppt/slides/_rels/slide1.xml.rels" → "ppt/slides/slide1.xml"
func slideNameFromRels(relsPath string) string {
	s := strings.Replace(relsPath, "/_rels/", "/", 1)
	return strings.TrimSuffix(s, ".rels")
}
