package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type OpenAIProvider struct {
	apiKey          string
	model           string
	baseURL         string
	client          *http.Client
	maxTokens       int
	useStreaming    bool
	earlyStop       bool
	adaptiveMu      sync.Mutex
	recentDurations []time.Duration
	adaptiveTimeout time.Duration
}

func NewOpenAI(apiKey, model string) *OpenAIProvider {
	return &OpenAIProvider{
		apiKey:          apiKey,
		model:           model,
		baseURL:         "https://api.openai.com/v1",
		client:          &http.Client{Timeout: 120 * time.Second},
		maxTokens:       2048,
		useStreaming:    false,
		earlyStop:       false,
		adaptiveTimeout: 120 * time.Second,
	}
}

func isReasoningModel(model string) bool {
	model = strings.ToLower(model)
	reasoning := []string{
		"reasoning", "o1", "o3", "qwq", "r1", "deepseek-r1",
		"marco-o1", "thinking", "reflection",
	}
	for _, r := range reasoning {
		if strings.Contains(model, r) {
			return true
		}
	}
	return false
}

func NewOpenAICompatible(baseURL, apiKey, model string) *OpenAIProvider {
	if apiKey == "" {
		apiKey = "not-needed"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	timeout := 180 * time.Second
	maxTokens := 2048

	return &OpenAIProvider{
		apiKey:          apiKey,
		model:           model,
		baseURL:         baseURL,
		client:          &http.Client{Timeout: timeout},
		maxTokens:       maxTokens,
		useStreaming:    false,
		earlyStop:       false,
		adaptiveTimeout: timeout,
	}
}

func (o *OpenAIProvider) Name() string {
	if o.baseURL != "https://api.openai.com/v1" {
		return fmt.Sprintf("Local (%s)", o.baseURL)
	}
	return "OpenAI"
}

func (o *OpenAIProvider) Model() string     { return o.model }
func (o *OpenAIProvider) Close() error      { return nil }
func (o *OpenAIProvider) IsReasoning() bool { return isReasoningModel(o.model) }

func (o *OpenAIProvider) SetTimeout(d time.Duration) {
	o.adaptiveMu.Lock()
	defer o.adaptiveMu.Unlock()
	o.adaptiveTimeout = d
	o.client.Timeout = d
}

func (o *OpenAIProvider) SetMaxTokens(n int) {
	if n > 0 {
		o.maxTokens = n
	}
}

func (o *OpenAIProvider) SetStreaming(enabled bool) {
	o.useStreaming = enabled
}

func (o *OpenAIProvider) SetEarlyStop(enabled bool) {
	o.earlyStop = enabled
}

func (o *OpenAIProvider) updateAdaptiveTimeout(d time.Duration) {
	o.adaptiveMu.Lock()
	defer o.adaptiveMu.Unlock()

	o.recentDurations = append(o.recentDurations, d)
	if len(o.recentDurations) > 5 {
		o.recentDurations = o.recentDurations[1:]
	}

	if len(o.recentDurations) >= 3 {
		var total time.Duration
		for _, dur := range o.recentDurations {
			total += dur
		}
		avg := total / time.Duration(len(o.recentDurations))
		newTimeout := avg * 2
		if newTimeout < 30*time.Second {
			newTimeout = 30 * time.Second
		}
		if newTimeout > 600*time.Second {
			newTimeout = 600 * time.Second
		}
		o.adaptiveTimeout = newTimeout
		o.client.Timeout = newTimeout
	}
}

func (o *OpenAIProvider) antiReasoningPrefix() string {
	if !isReasoningModel(o.model) {
		return ""
	}
	return "Respond directly with the final answer. Do NOT output thinking steps.\n\n"
}

func (o *OpenAIProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	start := time.Now()

	if prefix := o.antiReasoningPrefix(); prefix != "" {
		for i := len(messages) - 1; i >= 0; i-- {
			if messages[i].Role == "user" {
				messages[i].Content = prefix + messages[i].Content
				break
			}
		}
	}

	if o.useStreaming {
		return o.chatStreaming(ctx, messages, start)
	}
	return o.chatBlocking(ctx, messages, start)
}

func (o *OpenAIProvider) chatBlocking(ctx context.Context, messages []Message, start time.Time) (*Response, error) {
	reqBody := map[string]any{
		"model":       o.model,
		"messages":    messages,
		"max_tokens":  o.maxTokens,
		"temperature": 0.1,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	url := o.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := o.client.Do(req)
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
		Choices []struct {
			Message struct {
				Content          string `json:"content"`
				ReasoningContent string `json:"reasoning_content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	content := result.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" && result.Choices[0].Message.ReasoningContent != "" {
		content = "[Reasoning Output]: " + result.Choices[0].Message.ReasoningContent
	}

	duration := time.Since(start)
	o.updateAdaptiveTimeout(duration)

	var cost float64
	if o.baseURL == "https://api.openai.com/v1" {
		cost = calculateOpenAICost(o.model, result.Usage.PromptTokens, result.Usage.CompletionTokens)
	}

	return &Response{
		Content:          content,
		Model:            result.Model,
		PromptTokens:     result.Usage.PromptTokens,
		CompletionTokens: result.Usage.CompletionTokens,
		Duration:         duration,
		Cost:             cost,
	}, nil
}

func (o *OpenAIProvider) chatStreaming(ctx context.Context, messages []Message, start time.Time) (*Response, error) {
	reqBody := map[string]any{
		"model":       o.model,
		"messages":    messages,
		"max_tokens":  o.maxTokens,
		"temperature": 0.1,
		"stream":      true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	url := o.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder
	model := o.model

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Model   string `json:"model"`
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
				} `json:"delta"`
			} `json:"choices"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if chunk.Model != "" {
			model = chunk.Model
		}

		if len(chunk.Choices) > 0 {
			if chunk.Choices[0].Delta.Content != "" {
				contentBuilder.WriteString(chunk.Choices[0].Delta.Content)
			}
			if chunk.Choices[0].Delta.ReasoningContent != "" {
				reasoningBuilder.WriteString(chunk.Choices[0].Delta.ReasoningContent)
			}
		}
	}

	duration := time.Since(start)
	o.updateAdaptiveTimeout(duration)

	finalContent := contentBuilder.String()
	if strings.TrimSpace(finalContent) == "" && reasoningBuilder.Len() > 0 {
		finalContent = "[Reasoning Output]: " + reasoningBuilder.String()
	}

	return &Response{
		Content:          finalContent,
		Model:            model,
		PromptTokens:     0,
		CompletionTokens: len(finalContent) / 4,
		Duration:         duration,
		Cost:             0,
	}, nil
}

func calculateOpenAICost(model string, promptTokens, completionTokens int) float64 {
	prices := map[string][2]float64{
		"gpt-4o":        {2.50, 10.00},
		"gpt-4o-mini":   {0.15, 0.60},
		"gpt-4-turbo":   {10.00, 30.00},
		"gpt-3.5-turbo": {0.50, 1.50},
		"o1-preview":    {15.00, 60.00},
		"o1-mini":       {3.00, 12.00},
	}

	p, ok := prices[model]
	if !ok {
		p = prices["gpt-4o"]
	}

	return float64(promptTokens)/1_000_000*p[0] + float64(completionTokens)/1_000_000*p[1]
}
