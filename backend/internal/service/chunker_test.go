package service

import (
	"strings"
	"testing"
)

func TestBuildChunksShortDocument(t *testing.T) {
	chunks := BuildChunks("doc-1", "sample.md", "hello world", 1, DefaultChunkSize, DefaultChunkOverlap, DefaultMinChunks, false)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Text != "hello world" {
		t.Fatalf("unexpected chunk text: %q", chunks[0].Text)
	}
}

func TestBuildChunksLongDocument(t *testing.T) {
	text := strings.Repeat("段落内容。", 700)
	chunks := BuildChunks("doc-1", "sample.md", text, 1, DefaultChunkSize, DefaultChunkOverlap, DefaultMinChunks, false)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}
