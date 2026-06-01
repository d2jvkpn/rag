package parser

import (
	"archive/zip"
	"errors"
	"regexp"
	"strings"

	"github.com/d2jvkpn/rag/backend/internal/model"
)

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
func extractDocxBlocks(
	content string,
	rels map[string]string,
	imageRels map[string]string,
) []ParseBlock {
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
		} else if imgPlaceholder != "" {
			paraText += "\n" + imgPlaceholder
		}
		isHeading := false
		headingText := ""
		if m := docxHeadingStyleRe.FindStringSubmatch(raw); m != nil && isDocxHeadingStyle(m[1]) {
			isHeading = true
			headingText = paraText
		}
		if paraText != "" {
			items = append(
				items,
				docItem{
					isHeading: isHeading,
					heading:   headingText,
					ob:        ooxmlBlock{kind: "text", text: paraText, refs: paraRefs},
				},
			)
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
			result = append(
				result,
				ParseBlock{Text: text, SectionTitle: currentTitle, Refs: blockRefs},
			)
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
