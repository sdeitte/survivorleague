// Package aiclient talks to the Anthropic Messages API, currently for one
// purpose: generating the weekly AI recap text (see internal/recap). One
// short generation per league per week — this is deliberately the
// smallest possible client, not a general-purpose SDK wrapper.
package aiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// DefaultBaseURL is Anthropic's production API host.
const DefaultBaseURL = "https://api.anthropic.com"

// DefaultModel is used when ANTHROPIC_MODEL is unset. Chosen for writing
// quality (the recap needs to actually sound fun/human) over raw
// reasoning — volume is tiny (one short generation per league per week),
// so cost is not a factor in this choice.
const DefaultModel = "claude-sonnet-5"

// anthropicVersion is the API version header Anthropic's Messages API
// requires on every request.
const anthropicVersion = "2023-06-01"

// defaultMaxTokens caps a recap generation well above what a ~150-250 word
// recap needs, as a cost/runaway-generation guard, not a real limit.
const defaultMaxTokens = 1024

// HTTPDoer is the subset of *http.Client this package depends on —
// matches every other external client in this codebase (schedule.HTTPDoer,
// notify.HTTPDoer), which is what makes Client testable against an
// httptest.Server with zero live network calls.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client talks to the Anthropic Messages API.
type Client struct {
	httpClient HTTPDoer
	baseURL    string
	apiKey     string
	model      string
}

// NewClient constructs a Client. baseURL/model are configurable (rather
// than hardcoded) so tests can point at an httptest.Server and so the
// model can be bumped via ANTHROPIC_MODEL without a code change. apiKey
// may be empty in local dev (no ANTHROPIC_API_KEY configured yet);
// requests simply fail with 401 until a real key is set.
func NewClient(httpClient HTTPDoer, baseURL, apiKey, model string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{httpClient: httpClient, baseURL: baseURL, apiKey: apiKey, model: model}
}

type messagesRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// anthropicErrorResponse is the Messages API's documented error body
// shape: {"type": "error", "error": {"type": "...", "message": "..."}}.
type anthropicErrorResponse struct {
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// AnthropicError wraps a non-2xx response with enough context to debug an
// auth failure or an unexpected schema change without a live-network
// repro — mirrors schedule.CFBDError/notify.ResendError's identical
// pattern in this codebase.
type AnthropicError struct {
	StatusCode int
	Body       string
}

func (e *AnthropicError) Error() string {
	return fmt.Sprintf("aiclient: request failed with status %d: %s", e.StatusCode, e.Body)
}

// GenerateText sends prompt as a single user message and returns the
// model's full text response (concatenating every text content block, in
// order — Anthropic's response can carry more than one). Returns an error
// if the response contains no text content at all.
func (c *Client) GenerateText(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(messagesRequest{
		Model:     c.model,
		MaxTokens: defaultMaxTokens,
		Messages:  []message{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("aiclient: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("aiclient: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("aiclient: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("aiclient: read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errResp anthropicErrorResponse
		msg := string(respBody)
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Error.Message != "" {
			msg = errResp.Error.Message
		}
		return "", &AnthropicError{StatusCode: resp.StatusCode, Body: msg}
	}

	var parsed messagesResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("aiclient: decode response: %w", err)
	}

	var text string
	for _, block := range parsed.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}
	if text == "" {
		return "", fmt.Errorf("aiclient: response contained no text content")
	}
	return text, nil
}
