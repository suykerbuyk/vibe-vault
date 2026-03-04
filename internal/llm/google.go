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
)

// Google implements Provider for the Google Gemini REST API.
type Google struct {
	apiKey string
	model  string
	client *http.Client
}

// NewGoogle creates a Google Gemini provider.
func NewGoogle(apiKey, model string) (*Google, error) {
	return &Google{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{},
	}, nil
}

func (g *Google) Name() string { return "google" }

func (g *Google) ChatCompletion(ctx context.Context, req Request) (*Response, error) {
	// Build the Gemini generateContent request.
	body := geminiRequest{
		Contents: []geminiContent{
			{
				Role: "user",
				Parts: []geminiPart{
					{Text: req.UserPrompt},
				},
			},
		},
		GenerationConfig: &geminiGenConfig{
			Temperature: req.Temperature,
		},
	}

	if req.System != "" {
		body.SystemInstruction = &geminiContent{
			Parts: []geminiPart{{Text: req.System}},
		}
	}

	if req.JSONMode {
		body.GenerationConfig.ResponseMimeType = "application/json"
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		g.model, g.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
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

	var gemResp geminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if gemResp.Error != nil {
		return nil, fmt.Errorf("API error: %s", gemResp.Error.Message)
	}

	if len(gemResp.Candidates) == 0 {
		return nil, fmt.Errorf("empty candidates in response")
	}

	// Extract text from first candidate's content parts.
	var text string
	for _, part := range gemResp.Candidates[0].Content.Parts {
		if part.Text != "" {
			text = part.Text
			break
		}
	}
	if text == "" {
		return nil, fmt.Errorf("no text content in response")
	}

	return &Response{Content: text}, nil
}

// Gemini API types

type geminiRequest struct {
	Contents          []geminiContent  `json:"contents"`
	SystemInstruction *geminiContent   `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenConfig struct {
	Temperature      float64 `json:"temperature"`
	ResponseMimeType string  `json:"responseMimeType,omitempty"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
	Error      *geminiError      `json:"error,omitempty"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
