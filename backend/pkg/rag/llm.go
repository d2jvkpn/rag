package rag

import "context"

// LLM generates text given a system instruction and a user message.
type LLM interface {
	Complete(ctx context.Context, system, userMsg string) (string, error)
	Model() string
}

// NoopLLM returns empty answers; used until a real LLM is configured.
type NoopLLM struct{}

func (NoopLLM) Complete(_ context.Context, _, _ string) (string, error) { return "", nil }
func (NoopLLM) Model() string                                           { return "noop" }
