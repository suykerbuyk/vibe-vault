// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package enrichment

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/johns/vibe-vault/internal/llm"
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
// Returns (nil, nil) if provider is nil (enrichment disabled/unavailable).
func Generate(ctx context.Context, provider llm.Provider, input PromptInput) (*Result, error) {
	if provider == nil {
		return nil, nil
	}

	messages := buildMessages(input)

	resp, err := provider.ChatCompletion(ctx, llm.Request{
		System:      messages[0].Content, // system prompt
		UserPrompt:  messages[1].Content, // user prompt
		Temperature: 0.3,
		JSONMode:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("enrichment: %w", err)
	}

	return parseResponse(resp.Content)
}

func parseResponse(content string) (*Result, error) {
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
