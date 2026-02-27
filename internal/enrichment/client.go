package enrichment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/johns/vibe-vault/internal/config"
)

var allowedTags = map[string]bool{
	"implementation": true,
	"debugging":      true,
	"review":         true,
	"planning":       true,
	"exploration":    true,
	"research":       true,
}

// Generate calls the LLM to enrich a session note.
// Returns (nil, nil) if enrichment is disabled or the API key is not set.
func Generate(ctx context.Context, cfg config.EnrichmentConfig, input PromptInput) (*Result, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	apiKey := os.Getenv(cfg.APIKeyEnv)
	if apiKey == "" {
		return nil, nil
	}

	messages := buildMessages(input)

	reqBody := chatRequest{
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: 0.3,
		ResponseFormat: &respFormat{
			Type: "json_object",
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return parseResponse(respBody)
}

func parseResponse(body []byte) (*Result, error) {
	var resp chatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("API error: %s", resp.Error.Message)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in response")
	}

	content := resp.Choices[0].Message.Content

	var ej enrichmentJSON
	if err := json.Unmarshal([]byte(content), &ej); err != nil {
		return nil, fmt.Errorf("unmarshal enrichment JSON: %w", err)
	}

	return &Result{
		Summary:     ej.Summary,
		Decisions:   ej.Decisions,
		OpenThreads: ej.OpenThreads,
		Tag:         validateTag(ej.Tag),
	}, nil
}

func validateTag(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if allowedTags[tag] {
		return tag
	}
	return ""
}
