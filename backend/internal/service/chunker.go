package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"backend/internal/model"
	"backend/internal/uuid"
)

const (
	defaultChunkSize    = 1000
	defaultChunkOverlap = 150
)

func chunkConfigHash() string {
	sum := sha256.Sum256([]byte("strategy=structure-first;chunk_size=1000;chunk_overlap=150"))
	return hex.EncodeToString(sum[:])
}

func BuildChunks(documentID, filename, text string, chunkVersion int) []model.DocumentChunk {
	normalized := strings.TrimSpace(text)
	if normalized == "" {
		return nil
	}

	var segments []string
	if len([]rune(normalized)) <= 3000 {
		segments = []string{normalized}
	} else {
		segments = splitByLength(normalized, defaultChunkSize, defaultChunkOverlap)
	}

	now := time.Now().UTC()
	chunks := make([]model.DocumentChunk, 0, len(segments))
	for index, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		chunks = append(chunks, model.DocumentChunk{
			ChunkID:        uuid.NewV7(),
			CreatedAt:      now,
			UpdatedAt:      now,
			DocumentID:     documentID,
			ChunkIndex:     index,
			Text:           segment,
			NormalizedText: segment,
			Status:         "draft",
			ChunkVersion:   chunkVersion,
			Source:         "auto",
			IsCurrent:      true,
			Filename:       filename,
			ResourceRefs:   []model.ResourceRef{},
		})
	}
	return chunks
}

func splitByLength(text string, chunkSize, overlap int) []string {
	paragraphs := strings.Split(text, "\n\n")
	segments := make([]string, 0)
	current := ""

	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
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
		for _, forced := range splitRunes(paragraph, chunkSize, overlap) {
			segments = append(segments, forced)
		}
	}

	if strings.TrimSpace(current) != "" {
		segments = append(segments, current)
	}
	return segments
}

func splitRunes(text string, chunkSize, overlap int) []string {
	runes := []rune(text)
	if len(runes) <= chunkSize {
		return []string{text}
	}

	parts := make([]string, 0)
	step := chunkSize - overlap
	if step <= 0 {
		step = chunkSize
	}
	for start := 0; start < len(runes); start += step {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		parts = append(parts, string(runes[start:end]))
		if end == len(runes) {
			break
		}
	}
	return parts
}

func overlapTail(text string, overlap int) string {
	runes := []rune(text)
	if len(runes) <= overlap {
		return text
	}
	return string(runes[len(runes)-overlap:])
}
