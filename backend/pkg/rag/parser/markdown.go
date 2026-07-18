package parser

import (
	"errors"
	"os"
	"strings"
)

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
