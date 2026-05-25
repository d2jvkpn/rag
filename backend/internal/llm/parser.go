package llm

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type ParseResult struct {
	Text      string
	PageCount int
}

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
		texts = append(texts, extractParagraphText(string(content), "w:p", "w:t"))
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
			slideText[file.Name] = extractParagraphText(string(content), "a:p", "a:t")
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
	raw, err := os.ReadFile(path)
	if err != nil {
		return ParseResult{}, err
	}

	matches := regexp.MustCompile(`\(([^()]*)\)\s*Tj`).FindAllSubmatch(raw, -1)
	fragments := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		text := decodePDFString(match[1])
		if strings.TrimSpace(text) != "" {
			fragments = append(fragments, text)
		}
	}

	if len(fragments) == 0 {
		return ParseResult{}, errors.New("pdf text extraction failed: only simple text PDFs are supported in the current scaffold")
	}

	text := strings.TrimSpace(strings.Join(fragments, "\n"))
	if text == "" {
		return ParseResult{}, errors.New("pdf content is empty")
	}
	return ParseResult{Text: text, PageCount: max(1, bytes.Count(raw, []byte("/Type /Page")))}, nil
}

func CleanText(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")

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

func decodePDFString(input []byte) string {
	out := make([]byte, 0, len(input))
	escaped := false
	for i := 0; i < len(input); i++ {
		ch := input[i]
		if escaped {
			switch ch {
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case '\\', '(', ')':
				out = append(out, ch)
			default:
				out = append(out, ch)
			}
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		out = append(out, ch)
	}
	return strings.TrimSpace(string(out))
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
