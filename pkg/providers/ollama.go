package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type OllamaProvider struct {
	baseURL string
	model   string
	client  *http.Client
}

func NewOllama(baseURL, model string) *OllamaProvider {
	return &OllamaProvider{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (o *OllamaProvider) Name() string  { return "Ollama" }
func (o *OllamaProvider) Model() string { return o.model }
func (o *OllamaProvider) Close() error  { return nil }

func (o *OllamaProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	start := time.Now()

	reqBody := map[string]any{
		"model":    o.model,
		"messages": messages,
		"stream":   false,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama request failed (is ollama running?): %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		PromptEvalCount int `json:"prompt_eval_count"`
		EvalCount       int `json:"eval_count"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &Response{
		Content:          result.Message.Content,
		Model:            o.model,
		PromptTokens:     result.PromptEvalCount,
		CompletionTokens: result.EvalCount,
		Duration:         time.Since(start),
		Cost:             0,
	}, nil
}
