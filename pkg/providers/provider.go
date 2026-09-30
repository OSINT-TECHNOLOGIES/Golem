package providers

import (
	"context"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Response struct {
	Content          string
	Model            string
	PromptTokens     int
	CompletionTokens int
	Duration         time.Duration
	Cost             float64
}

type LLMProvider interface {
	Name() string
	Model() string
	Chat(ctx context.Context, messages []Message) (*Response, error)
	Close() error
}
