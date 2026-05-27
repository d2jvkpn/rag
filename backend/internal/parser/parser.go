package parser

import (
	"errors"
	"strings"
	"unicode"

	"backend/internal/model"
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
	Blocks    []ParseBlock // nil falls back to Text; populated when structure is available
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
