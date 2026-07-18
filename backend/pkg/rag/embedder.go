package rag

import "context"

// Embedder converts text chunks into dense vectors.
type Embedder interface {
	// Embed returns one embedding vector per input text, in the same order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Model returns the model identifier stored on each chunk for provenance.
	Model() string
}

// NoopEmbedder silently skips embedding; used until a real provider is configured.
type NoopEmbedder struct{}

func (NoopEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{}
	}
	return out, nil
}

func (NoopEmbedder) Model() string { return "noop" }
