package parser

import (
	"archive/zip"
	"io"
	"path/filepath"
	"regexp"
	"strings"

	"backend/internal/model"
)

type ooxmlBlock struct {
	kind string
	text string
	rows [][]string
	refs []model.ResourceRef
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
