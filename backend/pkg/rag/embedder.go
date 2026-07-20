package rag

import "context"

// Embedder converts text chunks into dense vectors.
type Embedder interface {
	// Embed returns one embedding vector per input text, in the same order.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Model returns the model identifier stored on each chunk for provenance.
	Model() string
}

// EmbedderWithUsage is an optional capability implemented by embedders that
// can report provider-side token consumption alongside the vectors, for
// callers that want to log or meter it (e.g. the MCP search server).
type EmbedderWithUsage interface {
	Embedder
	// EmbedWithUsage behaves like Embed but also returns the total number of
	// tokens the provider billed for the request.
	EmbedWithUsage(ctx context.Context, texts []string) ([][]float32, int, error)
}

// NoopEmbedder silently skips embedding; used until a real provider is configured.
type NoopEmbedder struct{}

func (NoopEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	var out [][]float32

	out = make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{}
	}
	return out, nil
}

func (NoopEmbedder) Model() string { return "noop" }
