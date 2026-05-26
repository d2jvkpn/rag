package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"backend/internal/llm"
	"backend/internal/model"
)

const (
	DefaultChunkSize    = 1000
	DefaultChunkOverlap = 150
	DefaultMinChunks    = 3
)

func chunkConfigHash(chunkSize, overlap, minChunks int) string {
	s := fmt.Sprintf("strategy=structure-first;chunk_size=%d;chunk_overlap=%d;min_chunks=%d", chunkSize, overlap, minChunks)
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

// BuildChunks splits blocks into DocumentChunks. Each block carries optional
// SectionTitle and PageStart metadata that is propagated to its chunks.
// If the total number of chunks produced is <= minChunks, the entire document
// is returned as a single chunk.
func BuildChunks(documentID, filename string, blocks []llm.ParseBlock, chunkVersion, chunkSize, overlap, minChunks int, approved bool) []model.DocumentChunk {
	status := "draft"
	if approved {
		status = "approved"
	}
	now := time.Now().UTC()

	allChunks := make([]model.DocumentChunk, 0)
	for _, block := range blocks {
		normalized := strings.TrimSpace(block.Text)
		if normalized == "" {
			continue
		}
		for _, seg := range splitByLength(normalized, chunkSize, overlap) {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			allChunks = append(allChunks, model.DocumentChunk{
				ChunkID:        uuid.Must(uuid.NewV7()).String(),
				CreatedAt:      now,
				UpdatedAt:      now,
				DocumentID:     documentID,
				ChunkIndex:     len(allChunks),
				SectionTitle:   block.SectionTitle,
				PageStart:      block.PageStart,
				PageEnd:        block.PageStart,
				Text:           seg,
				NormalizedText: seg,
				Status:         status,
				ChunkVersion:   chunkVersion,
				Source:         "auto",
				IsCurrent:      true,
				Filename:       filename,
				ResourceRefs:   []model.ResourceRef{},
			})
		}
	}

	if len(allChunks) <= minChunks {
		var texts []string
		for _, b := range blocks {
			if t := strings.TrimSpace(b.Text); t != "" {
				texts = append(texts, t)
			}
		}
		fullText := strings.TrimSpace(strings.Join(texts, "\n\n"))
		if fullText == "" {
			return nil
		}
		return []model.DocumentChunk{{
			ChunkID:        uuid.Must(uuid.NewV7()).String(),
			CreatedAt:      now,
			UpdatedAt:      now,
			DocumentID:     documentID,
			ChunkIndex:     0,
			Text:           fullText,
			NormalizedText: fullText,
			Status:         status,
			ChunkVersion:   chunkVersion,
			Source:         "auto",
			IsCurrent:      true,
			Filename:       filename,
			ResourceRefs:   []model.ResourceRef{},
		}}
	}

	for i := range allChunks {
		allChunks[i].ChunkIndex = i
	}
	return allChunks
}

// codeFenceGap is a placeholder that replaces \n\n inside code fences so that
// splitByLength does not break a code block at an internal blank line.
const codeFenceGap = "\x01"

// protectCodeFences replaces blank lines inside ``` / ~~~ fences with
// codeFenceGap so they survive the \n\n paragraph split.
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

		// oversized tables are split by row before being accumulated
		if isMarkdownTable(paragraph) && len([]rune(paragraph)) > chunkSize {
			if current != "" {
				segments = append(segments, current)
				current = ""
			}
			segments = append(segments, splitMarkdownTable(paragraph, chunkSize)...)
			continue
		}

		candidate := paragraph
		if current != "" {
			candidate = current + "\n\n" + paragraph
		}
		if len([]rune(candidate)) <= chunkSize {
			current = candidate
			continue
		}
		if current != "" {
			segments = append(segments, current)
			current = overlapTail(current, overlap) + "\n\n" + paragraph
			continue
		}
		segments = append(segments, splitRunes(paragraph, chunkSize, overlap)...)
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
		if len(current) > 0 && len([]rune(prefix+"\n"+strings.Join(next, "\n"))) > chunkSize {
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

// splitRunes splits text by character count, snapping cut points backward to
// the nearest sentence-ending punctuation within half the overlap window.
func splitRunes(text string, chunkSize, overlap int) []string {
	runes := []rune(text)
	if len(runes) <= chunkSize {
		return []string{text}
	}
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}
	window := overlap / 2
	parts := make([]string, 0)
	start := 0
	for start < len(runes) {
		end := start + chunkSize
		if end >= len(runes) {
			parts = append(parts, string(runes[start:]))
			break
		}
		// snap end backward to the nearest sentence boundary
		for i := end - 1; i >= end-window && i > start; i-- {
			if isSentenceEnd(runes[i]) {
				end = i + 1
				break
			}
		}
		parts = append(parts, string(runes[start:end]))
		next := end - overlap
		if next <= start {
			next = start + step
		}
		start = next
	}
	return parts
}

// overlapTail returns the tail of text used as overlap context for the next
// chunk. It advances the raw overlap start to the first sentence boundary so
// the overlap begins at a clean sentence.
func overlapTail(text string, overlap int) string {
	runes := []rune(text)
	if len(runes) <= overlap {
		return text
	}
	start := len(runes) - overlap
	for i := start; i < len(runes); i++ {
		if isSentenceEnd(runes[i]) && i+1 < len(runes) {
			return string(runes[i+1:])
		}
	}
	return string(runes[start:])
}
