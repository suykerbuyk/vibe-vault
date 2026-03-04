// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/johns/vibe-vault/internal/llm"
)

const knowledgeSystemPrompt = `You extract durable knowledge from Claude Code sessions.

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
// Returns nil if provider is nil (enrichment disabled/unavailable).
func Enrich(ctx context.Context, provider llm.Provider, input ExtractInput) ([]Note, error) {
	if provider == nil {
		return nil, nil
	}

	userPrompt := buildKnowledgePrompt(input)

	resp, err := provider.ChatCompletion(ctx, llm.Request{
		System:      knowledgeSystemPrompt,
		UserPrompt:  userPrompt,
		Temperature: 0.3,
		JSONMode:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("knowledge extraction: %w", err)
	}

	return parseKnowledgeResponse(resp.Content)
}

func parseKnowledgeResponse(content string) ([]Note, error) {
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
