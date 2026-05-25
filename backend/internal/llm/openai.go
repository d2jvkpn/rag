package llm

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// OpenAILLM implements LLM against any OpenAI-compatible chat completions endpoint.
type OpenAILLM struct {
	client openai.Client
	model  string
}

func NewOpenAILLM(baseURL, apiKey, model string) *OpenAILLM {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(&http.Client{Timeout: 120 * time.Second}),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}
	return &OpenAILLM{
		client: openai.NewClient(opts...),
		model:  model,
	}
}

func (o *OpenAILLM) Model() string { return o.model }

func (o *OpenAILLM) Complete(ctx context.Context, system, userMsg string) (string, error) {
	resp, err := o.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModel(o.model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
			openai.UserMessage(userMsg),
		},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("llm: empty choices in response")
	}
	return resp.Choices[0].Message.Content, nil
}
