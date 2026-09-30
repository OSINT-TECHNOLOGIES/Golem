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

type AnthropicProvider struct {
	apiKey string
	model  string
	client *http.Client
}

func NewAnthropic(apiKey, model string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (a *AnthropicProvider) Name() string  { return "Anthropic" }
func (a *AnthropicProvider) Model() string { return a.model }
func (a *AnthropicProvider) Close() error  { return nil }

func (a *AnthropicProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	start := time.Now()
	var systemText string
	var chatMessages []Message
	for _, m := range messages {
		if m.Role == "system" {
			systemText = m.Content
		} else {
			chatMessages = append(chatMessages, m)
		}
	}

	if len(chatMessages) == 0 {
		chatMessages = []Message{{Role: "user", Content: "Hello"}}
	}

	reqBody := map[string]any{
		"model":      a.model,
		"max_tokens": 4096,
		"messages":   chatMessages,
	}
	if systemText != "" {
		reqBody["system"] = systemText
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST",
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Model   string `json:"model"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	content := ""
	for _, c := range result.Content {
		if c.Type == "text" {
			content += c.Text
		}
	}

	duration := time.Since(start)
	cost := calculateAnthropicCost(a.model, result.Usage.InputTokens, result.Usage.OutputTokens)

	return &Response{
		Content:          content,
		Model:            result.Model,
		PromptTokens:     result.Usage.InputTokens,
		CompletionTokens: result.Usage.OutputTokens,
		Duration:         duration,
		Cost:             cost,
	}, nil
}

func calculateAnthropicCost(model string, inputTokens, outputTokens int) float64 {
	prices := map[string][2]float64{
		"claude-3-5-sonnet-20241022": {3.00, 15.00},
		"claude-3-5-sonnet":          {3.00, 15.00},
		"claude-3-opus":              {15.00, 75.00},
		"claude-3-haiku":             {0.25, 1.25},
	}

	p := prices[model]
	if p[0] == 0 {
		p = [2]float64{3.00, 15.00}
	}

	return float64(inputTokens)/1_000_000*p[0] + float64(outputTokens)/1_000_000*p[1]
}
