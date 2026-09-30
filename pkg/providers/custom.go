package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type CustomProvider struct {
	endpoint     string
	headers      map[string]string
	messageField string
	sessionField string
	client       *http.Client
}

func NewCustom(endpoint string, headers map[string]string, messageField, sessionField string) (*CustomProvider, error) {
	if messageField == "" {
		messageField = "message"
	}
	if sessionField == "" {
		sessionField = "session"
	}
	return &CustomProvider{
		endpoint:     endpoint,
		headers:      headers,
		messageField: messageField,
		sessionField: sessionField,
		client:       &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (c *CustomProvider) Name() string  { return "Custom" }
func (c *CustomProvider) Model() string { return c.endpoint }
func (c *CustomProvider) Close() error  { return nil }

func (c *CustomProvider) Chat(ctx context.Context, messages []Message) (*Response, error) {
	start := time.Now()
	prompt := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			prompt = messages[i].Content
			break
		}
	}

	reqBody := map[string]any{
		c.messageField: prompt,
		c.sessionField: "ai-redteam-session",
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request to %s failed: %w", c.endpoint, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	content := extractTextFromJSON(respBody)
	if content == "" {
		content = string(respBody)
	}

	return &Response{
		Content:  content,
		Model:    "custom",
		Duration: time.Since(start),
		Cost:     0,
	}, nil
}

func extractTextFromJSON(data []byte) string {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return ""
	}

	for _, key := range []string{"text", "content", "message", "response", "reply", "answer"} {
		if val, ok := raw[key]; ok {
			switch v := val.(type) {
			case string:
				return v
			case map[string]any:
				if text, ok := v["text"].(string); ok {
					return text
				}
				if content, ok := v["content"].(string); ok {
					return content
				}
			}
		}
	}

	for _, val := range raw {
		if nested, ok := val.(map[string]any); ok {
			for _, key := range []string{"text", "content"} {
				if text, ok := nested[key].(string); ok {
					return text
				}
			}
		}
	}

	for _, val := range raw {
		if s, ok := val.(string); ok && len(s) > 20 && !strings.HasPrefix(s, "http") {
			return s
		}
	}

	return ""
}
