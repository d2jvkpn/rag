package llm

import "testing"

func TestEmbeddingBatches(t *testing.T) {
	texts := make([]string, 11)
	for i := range texts {
		texts[i] = "x"
	}

	batches := embeddingBatches(texts, 10)
	if len(batches) != 2 {
		t.Fatalf("expected 2 batches, got %d", len(batches))
	}
	if batches[0].start != 0 || len(batches[0].texts) != 10 {
		t.Fatalf("unexpected first batch: start=%d len=%d", batches[0].start, len(batches[0].texts))
	}
	if batches[1].start != 10 || len(batches[1].texts) != 1 {
		t.Fatalf(
			"unexpected second batch: start=%d len=%d",
			batches[1].start,
			len(batches[1].texts),
		)
	}
}

func TestEmbeddingBatchesInvalidSizeFallsBack(t *testing.T) {
	texts := make([]string, 11)
	for i := range texts {
		texts[i] = "x"
	}

	batches := embeddingBatches(texts, 0)
	if len(batches) != 1 {
		t.Fatalf("expected single batch when batch size <= 0, got %d", len(batches))
	}
}
