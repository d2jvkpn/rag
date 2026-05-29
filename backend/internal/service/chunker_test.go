package service

import (
	"strings"
	"testing"

	"backend/internal/parser"
)

func TestBuildChunksShortDocument(t *testing.T) {
	blocks := []parser.ParseBlock{{Text: "hello world"}}
	chunks := BuildChunks(
		"doc-1",
		"sample.md",
		blocks,
		1,
		DefaultChunkSize,
		DefaultChunkOverlap,
		DefaultMinChunks,
		false,
	)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Text != "hello world" {
		t.Fatalf("unexpected chunk text: %q", chunks[0].Text)
	}
}

func TestBuildChunksLongDocument(t *testing.T) {
	text := strings.Repeat("段落内容。", 700)
	blocks := []parser.ParseBlock{{Text: text}}
	chunks := BuildChunks(
		"doc-1",
		"sample.md",
		blocks,
		1,
		DefaultChunkSize,
		DefaultChunkOverlap,
		DefaultMinChunks,
		false,
	)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
}

func TestBuildChunksSectionMetadata(t *testing.T) {
	blocks := []parser.ParseBlock{
		{Text: strings.Repeat("内容句子。", 300), SectionTitle: "第一章", PageStart: 1},
		{Text: strings.Repeat("内容句子。", 300), SectionTitle: "第二章", PageStart: 2},
	}
	chunks := BuildChunks(
		"doc-1",
		"sample.md",
		blocks,
		1,
		DefaultChunkSize,
		DefaultChunkOverlap,
		DefaultMinChunks,
		false,
	)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	sawFirst, sawSecond := false, false
	for _, c := range chunks {
		if c.SectionTitle == "第一章" {
			sawFirst = true
		}
		if c.SectionTitle == "第二章" {
			sawSecond = true
		}
	}
	if !sawFirst || !sawSecond {
		t.Fatalf(
			"expected chunks from both sections, sawFirst=%v sawSecond=%v",
			sawFirst,
			sawSecond,
		)
	}
}

func TestBuildChunksMinChunksMerge(t *testing.T) {
	// two blocks produce 2 chunks; minChunks=3 forces them to merge into 1
	blocks := []parser.ParseBlock{
		{Text: "短文本一。"},
		{Text: "短文本二。"},
	}
	chunks := BuildChunks(
		"doc-1",
		"sample.md",
		blocks,
		1,
		DefaultChunkSize,
		DefaultChunkOverlap,
		3,
		false,
	)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 merged chunk, got %d", len(chunks))
	}
}

func TestOverlapTailSentenceBoundary(t *testing.T) {
	text := "第一句话内容。第二句话内容。第三句话内容，继续写。第四句话。"
	tail := overlapTail(text, 15)
	// tail must not start mid-sentence (must start after a sentence-end rune)
	if tail == "" {
		t.Fatal("overlapTail returned empty string")
	}
	// the character before the tail should be a sentence-end or tail equals full overlap
	runes := []rune(text)
	tailRunes := []rune(tail)
	pos := len(runes) - len(tailRunes)
	if pos > 0 && !isSentenceEnd(runes[pos-1]) {
		t.Fatalf("overlap tail starts mid-sentence at rune pos %d: %q", pos, tail)
	}
}

func TestSplitMarkdownTable(t *testing.T) {
	header := "| A | B | C |"
	sep := "| --- | --- | --- |"
	rows := make([]string, 20)
	for i := range rows {
		rows[i] = "| 数据 | 数据 | 数据 |"
	}
	table := header + "\n" + sep + "\n" + strings.Join(rows, "\n")

	// Use a chunkSize that fits prefix+1 row but not the whole table.
	prefix := header + "\n" + sep
	rowToks := countTokens(rows[0])
	if rowToks == 0 {
		rowToks = 1
	}
	chunkSize := countTokens(prefix+"\n"+rows[0]) + rowToks

	parts := splitMarkdownTable(table, chunkSize)
	if len(parts) < 2 {
		t.Fatalf("expected table to be split (chunkSize=%d), got %d part(s)", chunkSize, len(parts))
	}
	for i, p := range parts {
		lines := strings.Split(p, "\n")
		if lines[0] != header || lines[1] != sep {
			t.Fatalf("part %d missing header/separator", i)
		}
	}
}

func TestCodeFenceProtection(t *testing.T) {
	md := "intro paragraph\n\n```python\ndef foo():\n\n    pass\n```\n\ntrailing paragraph"
	// after splitByLength the code block must remain intact
	segs := splitByLength(md, 2000, 150)
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if !strings.Contains(segs[0], "def foo():\n\n    pass") {
		t.Errorf("code block was split; got:\n%s", segs[0])
	}
}

func TestBuildChunksMergesShortTail(t *testing.T) {
	// Build two blocks and derive chunkSize from their actual token counts so the
	// test is encoding-agnostic.
	//
	// Preconditions for tail merge to fire:
	//   b1 <= chunkSize < combined   (block1 fits; mergeSmallBlocks won't combine them)
	//   b2 < chunkSize/2             (tail is short → triggers merge)
	//
	// Use midpoint of b1 and combined as chunkSize to guarantee all three conditions.
	block1 := strings.Repeat("The quick brown fox jumps over the lazy dog. ", 20)
	block2 := "Short tail."

	b1 := countTokens(block1)
	b2 := countTokens(block2)
	combined := countTokens(block1 + "\n\n" + block2)

	if combined <= b1+1 {
		t.Skipf("gap too small: b1=%d combined=%d", b1, combined)
	}
	chunkSize := (b1 + combined) / 2 // b1 <= chunkSize < combined ✓
	if b2 >= chunkSize/2 {
		t.Skipf("block2 (%d tokens) not small enough for tail merge (need < %d)", b2, chunkSize/2)
	}

	blocks := []parser.ParseBlock{
		{Text: block1, PageStart: 1, PageEnd: 1},
		{Text: block2, PageStart: 2, PageEnd: 2},
	}
	chunks := BuildChunks("doc-1", "sample.md", blocks, 1, chunkSize, chunkSize/10, 1, false)
	if len(chunks) != 1 {
		t.Fatalf("expected tail merged into 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0].Text, strings.TrimSpace(block1)) {
		t.Error("merged chunk missing first block text")
	}
	if !strings.Contains(chunks[0].Text, strings.TrimSpace(block2)) {
		t.Error("merged chunk missing tail block text")
	}
	if chunks[0].PageEnd != 2 {
		t.Errorf("expected PageEnd=2, got %d", chunks[0].PageEnd)
	}
}

func TestIsMarkdownTable(t *testing.T) {
	table := "| col1 | col2 |\n| --- | --- |\n| a | b |"
	if !isMarkdownTable(table) {
		t.Fatal("expected isMarkdownTable=true")
	}
	notTable := "just a paragraph\nwith two lines"
	if isMarkdownTable(notTable) {
		t.Fatal("expected isMarkdownTable=false")
	}
}
