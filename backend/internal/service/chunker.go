package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	tiktoken "github.com/pkoukk/tiktoken-go"

	"github.com/google/uuid"

	"backend/internal/model"
	"backend/internal/rag/parser"
)

const (
	DefaultChunkSize     = 800
	DefaultChunkOverlap  = 100
	DefaultMinChunks     = 2
	DefaultTokenEncoding = "cl100k_base"
)

// Package-level tokenizer. Initialised lazily on first use.
// Call SetTokenEncoding before any chunking to override the encoding.
var (
	tokMu   sync.Mutex
	tokEnc  *tiktoken.Tiktoken
	tokName = DefaultTokenEncoding
)

// SetTokenEncoding changes the BPE encoding used for token counting.
// Must be called before the first countTokens call (e.g. at app startup).
func SetTokenEncoding(name string) {
	tokMu.Lock()
	defer tokMu.Unlock()
	if tokName == name {
		return
	}
	tokName = name
	tokEnc = nil
}

func getTokenizer() *tiktoken.Tiktoken {
	tokMu.Lock()
	defer tokMu.Unlock()
	if tokEnc == nil {
		enc, err := tiktoken.GetEncoding(tokName)
		if err != nil {
			panic(fmt.Sprintf("tiktoken encoding %q: %v", tokName, err))
		}
		tokEnc = enc
	}
	return tokEnc
}

func countTokens(text string) int {
	return len(getTokenizer().Encode(text, nil, nil))
}

func chunkConfigHash(chunkSize, overlap, minChunks int) string {
	tokMu.Lock()
	enc := tokName
	tokMu.Unlock()
	s := fmt.Sprintf(
		"strategy=structure-first;encoding=%s;chunk_size=%d;chunk_overlap=%d;min_chunks=%d",
		enc, chunkSize, overlap, minChunks,
	)
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func isSentenceEnd(r rune) bool {
	switch r {
	case '。', '.', '!', '?', '！', '？', '；', ';':
		return true
	}
	return false
}

func newChunk(
	documentID, filename string,
	chunkVersion int,
	status string,
	now time.Time,
) model.DocumentChunk {
	return model.DocumentChunk{
		ChunkID:      uuid.Must(uuid.NewV7()).String(),
		CreatedAt:    now,
		UpdatedAt:    now,
		DocumentID:   documentID,
		ChunkVersion: chunkVersion,
		Status:       status,
		Source:       "auto",
		IsCurrent:    true,
		Filename:     filename,
	}
}

// mergeSmallBlocks combines consecutive blocks whose combined token count fits
// within chunkSize. Prevents tiny chunks when document sections are shorter than
// chunkSize — adjacent sections are accumulated until they'd exceed the limit.
func mergeSmallBlocks(blocks []parser.ParseBlock, chunkSize int) []parser.ParseBlock {
	if len(blocks) == 0 {
		return nil
	}
	result := make([]parser.ParseBlock, 0, len(blocks))
	cur := blocks[0]
	for _, b := range blocks[1:] {
		bText := strings.TrimSpace(b.Text)
		if bText == "" {
			continue
		}
		curText := strings.TrimSpace(cur.Text)
		combined := curText + "\n\n" + bText
		if countTokens(combined) <= chunkSize {
			cur.Text = combined
			cur.Refs = append(cur.Refs, b.Refs...)
			if b.PageEnd > cur.PageEnd {
				cur.PageEnd = b.PageEnd
			}
		} else {
			result = append(result, cur)
			cur = b
		}
	}
	if strings.TrimSpace(cur.Text) != "" {
		result = append(result, cur)
	}
	return result
}

// BuildChunks splits blocks into DocumentChunks. Each block carries optional
// SectionTitle and PageStart metadata that is propagated to its chunks.
// If the total number of chunks produced is <= minChunks, the entire document
// is returned as a single chunk. If the last chunk is shorter than chunkSize/2
// tokens it is merged into the preceding chunk to avoid trailing fragments.
func BuildChunks(
	documentID, filename string,
	blocks []parser.ParseBlock,
	chunkVersion, chunkSize, overlap, minChunks int,
	approved bool,
) []model.DocumentChunk {
	status := "draft"
	if approved {
		status = "approved"
	}
	now := time.Now().UTC()

	blocks = mergeSmallBlocks(blocks, chunkSize)

	allChunks := make([]model.DocumentChunk, 0)
	for _, block := range blocks {
		normalized := strings.TrimSpace(block.Text)
		if normalized == "" {
			continue
		}
		blockRefs := block.Refs
		if blockRefs == nil {
			blockRefs = []model.ResourceRef{}
		}
		for _, seg := range splitByLength(normalized, chunkSize, overlap) {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			c := newChunk(documentID, filename, chunkVersion, status, now)
			c.ChunkIndex = len(allChunks)
			c.SectionTitle = block.SectionTitle
			c.PageStart = block.PageStart
			c.PageEnd = block.PageEnd
			c.Text = seg
			c.NormalizedText = seg
			c.ResourceRefs = blockRefs
			allChunks = append(allChunks, c)
		}
	}

	if len(allChunks) >= 2 {
		last := &allChunks[len(allChunks)-1]
		if countTokens(last.Text) < chunkSize/2 {
			prev := &allChunks[len(allChunks)-2]
			prev.Text = prev.Text + "\n\n" + last.Text
			prev.NormalizedText = prev.NormalizedText + "\n\n" + last.NormalizedText
			prev.ResourceRefs = append(prev.ResourceRefs, last.ResourceRefs...)
			if last.PageEnd > prev.PageEnd {
				prev.PageEnd = last.PageEnd
			}
			allChunks = allChunks[:len(allChunks)-1]
		}
	}

	if len(allChunks) < minChunks {
		var texts []string
		var allRefs []model.ResourceRef
		pageStart, pageEnd := 0, 0
		for _, b := range blocks {
			if t := strings.TrimSpace(b.Text); t != "" {
				texts = append(texts, t)
			}
			allRefs = append(allRefs, b.Refs...)
			if pageStart == 0 || (b.PageStart > 0 && b.PageStart < pageStart) {
				pageStart = b.PageStart
			}
			if b.PageEnd > pageEnd {
				pageEnd = b.PageEnd
			}
		}
		if allRefs == nil {
			allRefs = []model.ResourceRef{}
		}
		fullText := strings.TrimSpace(strings.Join(texts, "\n\n"))
		if fullText == "" {
			return nil
		}
		c := newChunk(documentID, filename, chunkVersion, status, now)
		c.Text = fullText
		c.NormalizedText = fullText
		c.ResourceRefs = allRefs
		c.PageStart = pageStart
		c.PageEnd = pageEnd
		return []model.DocumentChunk{c}
	}

	for i := range allChunks {
		allChunks[i].ChunkIndex = i
	}
	return allChunks
}

// codeFenceGap is a placeholder that replaces \n\n inside code fences so that
// splitByLength does not break a code block at an internal blank line.
const codeFenceGap = "\x01"

func protectCodeFences(text string) string {
	lines := strings.Split(text, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inFence {
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = true
			}
		} else {
			if trimmed == "```" || trimmed == "~~~" {
				inFence = false
			} else if trimmed == "" {
				lines[i] = codeFenceGap
			}
		}
	}
	return strings.Join(lines, "\n")
}

func restoreCodeFences(text string) string {
	return strings.ReplaceAll(text, "\n"+codeFenceGap+"\n", "\n\n")
}

func splitByLength(text string, chunkSize, overlap int) []string {
	text = protectCodeFences(text)
	paragraphs := strings.Split(text, "\n\n")
	segments := make([]string, 0)
	current := ""

	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}

		paraLen := countTokens(paragraph)

		// oversized tables are split by row before being accumulated
		if isMarkdownTable(paragraph) && paraLen > chunkSize {
			if current != "" {
				segments = append(segments, current)
				current = ""
			}
			segments = append(segments, splitMarkdownTable(paragraph, chunkSize)...)
			continue
		}

		var candidateLen int
		if current == "" {
			candidateLen = paraLen
		} else {
			candidateLen = countTokens(current + "\n\n" + paragraph)
		}
		if candidateLen <= chunkSize {
			if current != "" {
				current = current + "\n\n" + paragraph
			} else {
				current = paragraph
			}
			continue
		}
		if current != "" {
			segments = append(segments, current)
			tail := overlapTail(current, overlap)
			current = tail + "\n\n" + paragraph
			continue
		}
		segments = append(segments, splitByTokens(paragraph, chunkSize, overlap)...)
	}

	if strings.TrimSpace(current) != "" {
		segments = append(segments, current)
	}
	for i, seg := range segments {
		segments[i] = restoreCodeFences(seg)
	}
	return segments
}

func isMarkdownTable(text string) bool {
	lines := strings.SplitN(strings.TrimSpace(text), "\n", 3)
	if len(lines) < 2 {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(lines[0]), "|") &&
		strings.Contains(lines[1], "---")
}

// splitMarkdownTable splits an oversized Markdown table by rows, repeating the
// header and separator line in every part.
func splitMarkdownTable(text string, chunkSize int) []string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) < 2 {
		return []string{text}
	}
	header := lines[0]
	separator := lines[1]
	dataRows := lines[2:]
	prefix := header + "\n" + separator

	var parts []string
	var current []string
	for _, row := range dataRows {
		next := append(current, row) //nolint:gocritic
		if len(current) > 0 && countTokens(prefix+"\n"+strings.Join(next, "\n")) > chunkSize {
			parts = append(parts, prefix+"\n"+strings.Join(current, "\n"))
			current = []string{row}
		} else {
			current = next
		}
	}
	if len(current) > 0 {
		parts = append(parts, prefix+"\n"+strings.Join(current, "\n"))
	}
	if len(parts) == 0 {
		return []string{text}
	}
	return parts
}

// splitByTokens splits text using a token-count sliding window when a single
// paragraph exceeds chunkSize. This is the fallback called by splitByLength.
func splitByTokens(text string, chunkSize, overlap int) []string {
	enc := getTokenizer()
	tokens := enc.Encode(text, nil, nil)
	if len(tokens) <= chunkSize {
		return []string{text}
	}
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}
	var parts []string
	start := 0
	for start < len(tokens) {
		end := start + chunkSize
		if end > len(tokens) {
			end = len(tokens)
		}
		parts = append(parts, strings.TrimSpace(string(enc.Decode(tokens[start:end]))))
		if end == len(tokens) {
			break
		}
		next := end - overlap
		if next <= start {
			next = start + step
		}
		start = next
	}
	return parts
}

// overlapTail returns the tail of text to use as overlap context for the next
// chunk. The tail is overlap tokens long, then advanced to the first sentence
// boundary so the overlap begins at a clean sentence.
func overlapTail(text string, overlap int) string {
	enc := getTokenizer()
	tokens := enc.Encode(text, nil, nil)
	if len(tokens) <= overlap {
		return text
	}
	tail := strings.TrimSpace(string(enc.Decode(tokens[len(tokens)-overlap:])))
	runes := []rune(tail)
	for i, r := range runes {
		if isSentenceEnd(r) && i+1 < len(runes) {
			return string(runes[i+1:])
		}
	}
	return tail
}
