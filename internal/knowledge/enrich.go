// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package knowledge

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

const systemPrompt = `You extract durable knowledge from Claude Code sessions.

Given session data with friction corrections (user corrections and what followed),
extract actionable lessons and architectural decisions.

Return JSON matching this schema:
{
  "lessons": [
    {"title": "...", "summary": "...", "body": "...", "confidence": 0.8, "category": "..."}
  ],
  "decisions": [
    {"title": "...", "summary": "...", "body": "...", "confidence": 0.8, "category": "..."}
  ]
}

Rules:
- lessons: "Don't do X because Y. Instead do Z." form. Derive from corrections only.
- decisions: "Chose X over Y because Z." form. Include alternatives considered.
- confidence: 0.0-1.0. Only include items >= 0.5. Be strict.
- 0-3 lessons, 0-3 decisions. Omit empty arrays.
- title: short imperative (lessons) or declarative (decisions), max 80 chars.
- summary: one sentence, max 120 chars.
- body: 2-5 sentences of actionable markdown. For lessons, explain the mistake, why it's wrong, and the correct approach. For decisions, explain the context, alternatives, and rationale.
- category: one word, lowercase (e.g. "testing", "serialization", "architecture", "error-handling", "performance").`

// knowledgeJSON is the expected JSON structure from the LLM response.
type knowledgeJSON struct {
	Lessons   []noteJSON `json:"lessons"`
	Decisions []noteJSON `json:"decisions"`
}

type noteJSON struct {
	Title      string  `json:"title"`
	Summary    string  `json:"summary"`
	Body       string  `json:"body"`
	Confidence float64 `json:"confidence"`
	Category   string  `json:"category"`
}

// Enrich calls the LLM to extract knowledge notes from session data.
// Returns nil if enrichment is disabled, no API key is set, or on error.
func Enrich(ctx context.Context, cfg config.EnrichmentConfig, input ExtractInput) ([]Note, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	apiKey := os.Getenv(cfg.APIKeyEnv)
	if apiKey == "" {
		return nil, nil
	}

	userPrompt := buildKnowledgePrompt(input)

	reqBody := chatRequest{
		Model: cfg.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
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

	return parseKnowledgeResponse(respBody)
}

func parseKnowledgeResponse(body []byte) ([]Note, error) {
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

	var kj knowledgeJSON
	if err := json.Unmarshal([]byte(content), &kj); err != nil {
		return nil, fmt.Errorf("unmarshal knowledge JSON: %w", err)
	}

	var notes []Note
	for _, l := range kj.Lessons {
		if l.Confidence < 0.5 {
			continue
		}
		notes = append(notes, Note{
			Type:       "lesson",
			Title:      l.Title,
			Summary:    l.Summary,
			Body:       l.Body,
			Confidence: l.Confidence,
			Category:   strings.ToLower(strings.TrimSpace(l.Category)),
		})
	}
	for _, d := range kj.Decisions {
		if d.Confidence < 0.5 {
			continue
		}
		notes = append(notes, Note{
			Type:       "decision",
			Title:      d.Title,
			Summary:    d.Summary,
			Body:       d.Body,
			Confidence: d.Confidence,
			Category:   strings.ToLower(strings.TrimSpace(d.Category)),
		})
	}

	return notes, nil
}

func buildKnowledgePrompt(input ExtractInput) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("## Session: %s\n\n", input.Project))
	b.WriteString(fmt.Sprintf("Friction score: %d/100\n\n", input.FrictionScore))

	if len(input.Corrections) > 0 {
		b.WriteString("## Correction→Resolution Pairs\n\n")
		for i, c := range input.Corrections {
			b.WriteString(fmt.Sprintf("### Correction %d (%s)\n", i+1, c.Pattern))
			b.WriteString(fmt.Sprintf("**User:** %s\n", c.UserText))
			b.WriteString(fmt.Sprintf("**Resolution:** %s\n\n", c.Resolution))
		}
	}

	if len(input.Decisions) > 0 {
		b.WriteString("## Decisions Made\n\n")
		for _, d := range input.Decisions {
			b.WriteString(fmt.Sprintf("- %s\n", d))
		}
		b.WriteString("\n")
	}

	if len(input.FilesChanged) > 0 {
		b.WriteString("## Files Changed\n\n")
		limit := len(input.FilesChanged)
		if limit > 30 {
			limit = 30
		}
		for _, f := range input.FilesChanged[:limit] {
			b.WriteString(fmt.Sprintf("- %s\n", f))
		}
		b.WriteString("\n")
	}

	// Include truncated transcript text for additional context
	if input.UserText != "" {
		userText := input.UserText
		if len(userText) > 8000 {
			userText = userText[:8000] + "\n...(truncated)"
		}
		b.WriteString("## User Messages\n\n")
		b.WriteString(userText)
		b.WriteString("\n\n")
	}

	if input.AssistantText != "" {
		asstText := input.AssistantText
		if len(asstText) > 8000 {
			asstText = asstText[:8000] + "\n...(truncated)"
		}
		b.WriteString("## Assistant Messages\n\n")
		b.WriteString(asstText)
		b.WriteString("\n\n")
	}

	return b.String()
}

// API types — mirrors enrichment package but kept local to avoid coupling.

type chatRequest struct {
	Model          string        `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	ResponseFormat *respFormat   `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Error   *apiError    `json:"error,omitempty"`
}

type chatChoice struct {
	Message chatMessage `json:"message"`
}

type respFormat struct {
	Type string `json:"type"`
}

type apiError struct {
	Message string `json:"message"`
}
