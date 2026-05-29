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
	PageEnd      int
	Refs         []model.ResourceRef
}

type ParseResult struct {
	Text      string       `json:"text"`
	PageCount int          `json:"page_count"`
	Blocks    []ParseBlock // nil falls back to Text; populated when structure is available
}

// textNormalizer handles character-level normalization before line processing.
// Order matters: \r\n must precede \r to avoid double-replacing.
var textNormalizer = strings.NewReplacer(
	"\r\n", "\n",
	"\r", "\n",
	" ", " ", // non-breaking space → regular space
	" ", "\n", // Unicode line separator → newline
	" ", "\n\n", // Unicode paragraph separator → double newline
	// PUA bullets from Symbol/Wingdings fonts
	"", "•",
	"", "•",
	"", "•",
	"", "•",
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
	input = textNormalizer.Replace(input)

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
		switch r {
		case '­', // soft hyphen
			'​', '‌', '‍', // zero-width space / non-joiner / joiner
			'⁠',      // word joiner
			'￼', '�': // object replacement, replacement character
			return -1
		}
		// remove remaining unmapped PUA characters (Symbol/Wingdings artifacts)
		if r >= '' && r <= '' {
			return -1
		}
		return r
	}, cleaned)
}
