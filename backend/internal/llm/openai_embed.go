package llm

import (
	"context"
	"net/http"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// OpenAIEmbedder implements Embedder against any OpenAI-compatible embeddings endpoint.
type OpenAIEmbedder struct {
	client    openai.Client
	model     string
	batchSize int
}

const defaultEmbeddingBatchSize = 10

func NewOpenAIEmbedder(baseURL, apiKey, model string, batchSize int) *OpenAIEmbedder {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	if batchSize <= 0 {
		batchSize = defaultEmbeddingBatchSize
	}
	return &OpenAIEmbedder{
		client:    openai.NewClient(opts...),
		model:     model,
		batchSize: batchSize,
	}
}

func (o *OpenAIEmbedder) Model() string { return o.model }

func (o *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	embeddings := make([][]float32, len(texts))
	for _, batch := range embeddingBatches(texts, o.batchSize) {
		resp, err := o.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
			Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: batch.texts},
			Model: openai.EmbeddingModel(o.model),
		})
		if err != nil {
			return nil, err
		}
		for _, d := range resp.Data {
			idx := batch.start + int(d.Index)
			if idx < len(embeddings) {
				f32 := make([]float32, len(d.Embedding))
				for i, v := range d.Embedding {
					f32[i] = float32(v)
				}
				embeddings[idx] = f32
			}
		}
	}
	return embeddings, nil
}

type embeddingBatch struct {
	start int
	texts []string
}

func embeddingBatches(texts []string, batchSize int) []embeddingBatch {
	if len(texts) == 0 {
		return nil
	}
	if batchSize <= 0 {
		batchSize = len(texts)
	}

	batches := make([]embeddingBatch, 0, (len(texts)+batchSize-1)/batchSize)
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batches = append(batches, embeddingBatch{
			start: start,
			texts: texts[start:end],
		})
	}
	return batches
}
