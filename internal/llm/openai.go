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

// OpenAI implements Provider for OpenAI-compatible APIs.
// Covers: OpenAI, xAI/Grok, Ollama, any OpenAI-compatible endpoint.
type OpenAI struct {
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewOpenAI creates an OpenAI-compatible provider.
func NewOpenAI(baseURL, apiKey, model string) (*OpenAI, error) {
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	return &OpenAI{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		client:  &http.Client{},
	}, nil
}

func (o *OpenAI) Name() string { return "openai" }

func (o *OpenAI) ChatCompletion(ctx context.Context, req Request) (*Response, error) {
	messages := []oaiMessage{
		{Role: "system", Content: req.System},
		{Role: "user", Content: req.UserPrompt},
	}

	body := oaiRequest{
		Model:       o.model,
		Messages:    messages,
		Temperature: req.Temperature,
	}
	if req.JSONMode {
		body.ResponseFormat = &oaiRespFormat{Type: "json_object"}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := o.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.client.Do(httpReq)
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

	var oaiResp oaiResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if oaiResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", oaiResp.Error.Message)
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in response")
	}

	return &Response{Content: oaiResp.Choices[0].Message.Content}, nil
}

func isTransientStatus(code int) bool {
	return code == 429 || code == 500 || code == 502 || code == 503 || code == 504
}

// OpenAI API types

type oaiRequest struct {
	Model          string         `json:"model"`
	Messages       []oaiMessage   `json:"messages"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat *oaiRespFormat `json:"response_format,omitempty"`
}

type oaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type oaiRespFormat struct {
	Type string `json:"type"`
}

type oaiResponse struct {
	Choices []oaiChoice `json:"choices"`
	Error   *oaiError   `json:"error,omitempty"`
}

type oaiChoice struct {
	Message oaiMessage `json:"message"`
}

type oaiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}
