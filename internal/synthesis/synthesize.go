package synthesis

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/suykerbuyk/vibe-vault/internal/llm"
)

// Synthesize sends the gathered input to the LLM and returns structured actions.
// Returns (nil, nil) if provider is nil.
func Synthesize(ctx context.Context, provider llm.Provider, input *Input) (*Result, error) {
	if provider == nil {
		return nil, nil
	}

	userPrompt := buildUserPrompt(input)

	resp, err := provider.ChatCompletion(ctx, llm.Request{
		System:      systemPrompt,
		UserPrompt:  userPrompt,
		Temperature: 0.3,
		JSONMode:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	var result Result
	if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
		return nil, fmt.Errorf("parse LLM response: %w", err)
	}

	result.Learnings = filterLearnings(result.Learnings)
	result.TaskUpdates = filterTaskUpdates(result.TaskUpdates)
	result.StaleEntries = filterStaleEntries(result.StaleEntries)

	return &result, nil
}

var validSections = map[string]bool{
	"Decisions": true,
	"Patterns":  true,
	"Learnings": true,
}

func filterLearnings(learnings []Learning) []Learning {
	var valid []Learning
	for _, l := range learnings {
		if !validSections[l.Section] {
			log.Printf("synthesis: dropping learning with invalid section %q", l.Section)
			continue
		}
		valid = append(valid, l)
	}
	return valid
}

var validActions = map[string]bool{
	"complete":      true,
	"update_status": true,
}

func filterTaskUpdates(updates []TaskUpdate) []TaskUpdate {
	var valid []TaskUpdate
	for _, u := range updates {
		if !validActions[u.Action] {
			log.Printf("synthesis: dropping task update with invalid action %q", u.Action)
			continue
		}
		valid = append(valid, u)
	}
	return valid
}

var validFiles = map[string]bool{
	"knowledge.md": true,
	"resume.md":    true,
}

func filterStaleEntries(entries []StaleEntry) []StaleEntry {
	var valid []StaleEntry
	for _, e := range entries {
		if !validFiles[e.File] {
			log.Printf("synthesis: dropping stale entry with invalid file %q", e.File)
			continue
		}
		if e.Index < 0 {
			log.Printf("synthesis: dropping stale entry with negative index %d", e.Index)
			continue
		}
		valid = append(valid, e)
	}
	return valid
}
