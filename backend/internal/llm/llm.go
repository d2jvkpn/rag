package llm

import "context"

// LLM generates text given a system instruction and a user message.
type LLM interface {
	Complete(ctx context.Context, system, userMsg string) (string, error)
	Model() string
}

// Noop returns empty answers; used until a real LLM is configured.
type Noop struct{}

func (Noop) Complete(_ context.Context, _, _ string) (string, error) { return "", nil }
func (Noop) Model() string                                           { return "noop" }
