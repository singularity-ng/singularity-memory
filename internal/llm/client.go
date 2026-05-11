package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"git.infra.centralcloud.com/centralcloud/operations-memory/internal/modelrouter"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client struct {
	route  modelrouter.Route
	http   *http.Client
}

func New(route modelrouter.Route) *Client {
	return &Client{
		route: route,
		http:  &http.Client{Timeout: 120 * time.Second},
	}
}

type request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
}

type Option func(*request)

func WithMaxTokens(n int) Option {
	return func(r *request) { r.MaxTokens = &n }
}

func WithTemperature(f float64) Option {
	return func(r *request) { r.Temperature = &f }
}

// WithSystemPrompt prepends a system message if one is not already present.
func WithSystemPrompt(s string) Option {
	return func(r *request) {
		if len(r.Messages) > 0 && r.Messages[0].Role == "system" {
			return
		}
		r.Messages = append([]Message{{Role: "system", Content: s}}, r.Messages...)
	}
}

// Complete sends messages to the LLM and returns the assistant message content.
func (c *Client) Complete(ctx context.Context, messages []Message, opts ...Option) (string, error) {
	req := &request{
		Model:    c.route.Model,
		Messages: messages,
	}
	for _, opt := range opts {
		opt(req)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("llm: marshal request: %w", err)
	}

	baseURL := strings.TrimRight(c.route.BaseURL, "/")
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("llm: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.route.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.route.APIKey)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("llm: request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("llm: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm: non-2xx response %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("llm: parse response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("llm: no choices in response")
	}
	return result.Choices[0].Message.Content, nil
}
