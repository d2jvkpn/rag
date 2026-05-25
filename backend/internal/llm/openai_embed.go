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
	client openai.Client
	model  string
}

func NewOpenAIEmbedder(baseURL, apiKey, model string) *OpenAIEmbedder {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &OpenAIEmbedder{
		client: openai.NewClient(opts...),
		model:  model,
	}
}

func (o *OpenAIEmbedder) Model() string { return o.model }

func (o *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	resp, err := o.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
		Input: openai.EmbeddingNewParamsInputUnion{OfArrayOfStrings: texts},
		Model: openai.EmbeddingModel(o.model),
	})
	if err != nil {
		return nil, err
	}
	embeddings := make([][]float32, len(texts))
	for _, d := range resp.Data {
		if int(d.Index) < len(embeddings) {
			f32 := make([]float32, len(d.Embedding))
			for i, v := range d.Embedding {
				f32[i] = float32(v)
			}
			embeddings[d.Index] = f32
		}
	}
	return embeddings, nil
}
