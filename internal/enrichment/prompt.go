package enrichment

import (
	"fmt"
	"sort"
	"strings"
)

const (
	maxUserChars      = 12000
	maxAssistantChars = 12000
)

// PromptInput holds the transcript data needed to build the LLM prompt.
type PromptInput struct {
	UserText      string
	AssistantText string
	FilesChanged  []string
	ToolCounts    map[string]int
	Duration      int // minutes
	UserMessages  int
	AsstMessages  int

	// Narrative context (optional, from heuristic extraction)
	NarrativeSummary string   // Heuristic summary for LLM to refine
	NarrativeTag     string   // Heuristic tag
	Activities       []string // Activity descriptions for context
}

const systemPrompt = `You analyze Claude Code session transcripts and produce structured JSON summaries.

Respond with valid JSON only. No markdown, no explanation. Schema:
{
  "summary": "1-3 sentences. Past tense. Outcome-focused. What was accomplished.",
  "decisions": ["Decision — rationale", ...],
  "open_threads": ["Actionable next step", ...],
  "tag": "one of: implementation, debugging, review, planning, exploration, research"
}

Rules:
- summary: Past tense, focus on outcomes and what changed. 1-3 sentences max.
- decisions: 0-5 key technical decisions made during the session. Format: "Decision — rationale". Omit if none.
- open_threads: 0-3 unfinished items or natural next steps. Actionable, specific. Omit if none.
- tag: Classify the session's primary activity. Exactly one tag.`

func buildMessages(input PromptInput) []chatMessage {
	return []chatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: buildUserPrompt(input)},
	}
}

func buildUserPrompt(input PromptInput) string {
	var b strings.Builder

	// Metadata section
	b.WriteString(fmt.Sprintf("## Session Metadata\n"))
	b.WriteString(fmt.Sprintf("- Duration: %d minutes\n", input.Duration))
	b.WriteString(fmt.Sprintf("- User messages: %d\n", input.UserMessages))
	b.WriteString(fmt.Sprintf("- Assistant messages: %d\n", input.AsstMessages))

	// Tool usage
	if len(input.ToolCounts) > 0 {
		b.WriteString("\n## Tool Usage\n")
		// Sort keys for deterministic output
		keys := make([]string, 0, len(input.ToolCounts))
		for k := range input.ToolCounts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString(fmt.Sprintf("- %s: %d\n", k, input.ToolCounts[k]))
		}
	}

	// Files changed
	if len(input.FilesChanged) > 0 {
		b.WriteString("\n## Files Changed\n")
		for _, f := range input.FilesChanged {
			b.WriteString(fmt.Sprintf("- %s\n", f))
		}
	}

	// Heuristic analysis (from narrative extraction)
	if input.NarrativeSummary != "" || input.NarrativeTag != "" || len(input.Activities) > 0 {
		b.WriteString("\n## Heuristic Analysis\n")
		b.WriteString("The following was extracted heuristically. Refine rather than replace.\n")
		if input.NarrativeSummary != "" {
			b.WriteString(fmt.Sprintf("- Summary: %s\n", input.NarrativeSummary))
		}
		if input.NarrativeTag != "" {
			b.WriteString(fmt.Sprintf("- Tag: %s\n", input.NarrativeTag))
		}
		if len(input.Activities) > 0 {
			b.WriteString("- Activities:\n")
			max := len(input.Activities)
			if max > 20 {
				max = 20
			}
			for _, a := range input.Activities[:max] {
				b.WriteString(fmt.Sprintf("  - %s\n", a))
			}
			if len(input.Activities) > 20 {
				b.WriteString(fmt.Sprintf("  - ... and %d more\n", len(input.Activities)-20))
			}
		}
	}

	// Transcript text
	userText := truncate(input.UserText, maxUserChars)
	asstText := truncate(input.AssistantText, maxAssistantChars)

	b.WriteString("\n## User Messages\n")
	b.WriteString(userText)
	b.WriteString("\n\n## Assistant Messages\n")
	b.WriteString(asstText)

	return b.String()
}

func truncate(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}

	// Try to break at a newline before the limit
	truncated := text[:maxChars]
	if idx := strings.LastIndex(truncated, "\n"); idx > maxChars/2 {
		truncated = truncated[:idx]
	}

	return truncated + "\n[...truncated]"
}
