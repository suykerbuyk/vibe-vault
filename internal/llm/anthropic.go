// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Anthropic implements Provider for the Anthropic Messages API.
type Anthropic struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewAnthropic creates an Anthropic provider.
func NewAnthropic(baseURL, apiKey, model string) (*Anthropic, error) {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &Anthropic{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{},
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

	url := a.baseURL + "/v1/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
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

	return &Response{Content: text}, nil
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
