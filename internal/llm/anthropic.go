// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Anthropic implements Provider for the Anthropic Messages API (single-turn
// chat completion). HTTP plumbing lives in *anthropicHTTPCore (anthropichttp.go)
// for symmetry with future provider variants that may want to share it.
type Anthropic struct {
	*anthropicHTTPCore
}

// NewAnthropic creates an Anthropic provider. The signature is preserved
// from the pre-refactor version so existing callers keep compiling unchanged.
func NewAnthropic(baseURL, apiKey, model string) (*Anthropic, error) {
	return &Anthropic{
		anthropicHTTPCore: newAnthropicHTTPCore(baseURL, apiKey, model, nil),
	}, nil
}

func (a *Anthropic) Name() string { return "anthropic" }

func (a *Anthropic) ChatCompletion(ctx context.Context, req Request) (*Response, error) {
	messages := []anthropicMessage{
		{Role: "user", Content: req.UserPrompt},
	}

	body := anthropicRequest{
		Model:       a.model,
		MaxTokens:   4096,
		System:      req.System,
		Messages:    messages,
		Temperature: req.Temperature,
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := a.do(ctx, payload, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if isTransientStatus(resp.StatusCode) {
		return nil, &TransientError{Err: fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var antResp anthropicResponse
	if err := json.Unmarshal(respBody, &antResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if antResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", antResp.Error.Message)
	}

	// Extract text from content blocks.
	var text string
	for _, block := range antResp.Content {
		if block.Type == "text" {
			text = block.Text
			break
		}
	}
	if text == "" {
		return nil, fmt.Errorf("no text content in response")
	}

	// Anthropic has no native JSON-mode flag (unlike OpenAI's response_format),
	// so Claude tends to wrap JSON responses in ```json ... ``` markdown fences.
	// Strip them when the caller requested JSON output; no-op otherwise.
	if req.JSONMode {
		text = stripJSONFence(text)
	}

	return &Response{Content: text}, nil
}

// stripJSONFence removes a surrounding ```json ... ``` or ``` ... ``` fence
// if the text is wrapped in one. Returns the trimmed content. Safe on
// unwrapped text (no-op). Used by the Anthropic provider to coerce raw JSON
// output since Anthropic has no response_format equivalent.
func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the opening fence line (```json or bare ```).
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	} else {
		// Single-line ```...``` edge case.
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}

// Anthropic API types

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature float64            `json:"temperature"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []anthropicContentBlock `json:"content"`
	Error   *anthropicError         `json:"error,omitempty"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type anthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
