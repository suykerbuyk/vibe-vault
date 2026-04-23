package synthesis

import (
	"fmt"
	"strings"
)

const systemPrompt = `You are a session synthesis agent for a developer knowledge base. Your job is to analyze a completed coding session and produce structured JSON describing what should be updated in the project's living documents.

Output MUST be valid JSON matching this schema:
{
  "learnings": [{"section": "Decisions|Patterns|Learnings", "entry": "text"}],
  "stale_entries": [{"file": "knowledge.md|resume.md", "section": "heading", "index": 0, "entry": "approx text", "reason": "why stale"}],
  "resume_update": {"current_state": "text or empty", "open_threads": "text or empty", "features": "text or empty"} or null,
  "task_updates": [{"name": "task-slug", "action": "complete|update_status", "status": "new status", "reason": "why"}],
  "reasoning": "brief explanation of your analysis"
}

Rules:
- Use past tense, outcome-focused language for learnings
- Learnings must be genuinely new — not already present in the current knowledge
- For stale entries: provide the section heading and the 0-based bullet index within that section. The index is the primary identifier; the entry text is for verification
- Resume updates should reflect the project state AFTER this session
- Only update tasks when the session clearly completed or advanced them
- Be conservative: when uncertain, omit rather than guess
- Use empty arrays for absent fields, not null (except resume_update which can be null)
- Learning sections must be exactly "Decisions", "Patterns", or "Learnings"
- "current_state" must only contain invariant bullets (counts, versions, IDs). Narrative and shipped-capability prose belongs in "features".
`

// buildUserPrompt assembles all input fields into a labeled prompt.
func buildUserPrompt(input *Input) string {
	var b strings.Builder

	// Session summary
	b.WriteString("## Session Summary\n\n")
	if input.SessionNote != nil {
		note := input.SessionNote
		if note.Date != "" {
			fmt.Fprintf(&b, "- **Date**: %s\n", note.Date)
		}
		if note.Summary != "" {
			fmt.Fprintf(&b, "- **Summary**: %s\n", note.Summary)
		}
		if note.Tag != "" {
			fmt.Fprintf(&b, "- **Tag**: %s\n", note.Tag)
		}
	}
	b.WriteString("\n")

	// Key decisions
	if input.SessionNote != nil && len(input.SessionNote.Decisions) > 0 {
		b.WriteString("## Key Decisions\n\n")
		for _, d := range input.SessionNote.Decisions {
			fmt.Fprintf(&b, "- %s\n", d)
		}
		b.WriteString("\n")
	}

	// Open threads
	if input.SessionNote != nil && len(input.SessionNote.OpenThreads) > 0 {
		b.WriteString("## Open Threads\n\n")
		for _, t := range input.SessionNote.OpenThreads {
			fmt.Fprintf(&b, "- %s\n", t)
		}
		b.WriteString("\n")
	}

	// Files changed
	if input.SessionNote != nil && len(input.SessionNote.FilesChanged) > 0 {
		b.WriteString("## Files Changed\n\n")
		for _, f := range input.SessionNote.FilesChanged {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	// Git diff
	b.WriteString("## Git Diff\n\n")
	if input.GitDiff != "" {
		b.WriteString("```diff\n")
		b.WriteString(input.GitDiff)
		if !strings.HasSuffix(input.GitDiff, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("```\n\n")
	} else {
		b.WriteString("(no commits)\n\n")
	}

	// Current knowledge with numbered bullets
	b.WriteString("## Current Knowledge (knowledge.md)\n\n")
	if input.KnowledgeMD != "" {
		b.WriteString(numberBullets(input.KnowledgeMD))
		b.WriteString("\n")
	} else {
		b.WriteString("(empty — will be created if learnings provided)\n\n")
	}

	// Current resume
	b.WriteString("## Current Resume (resume.md)\n\n")
	if input.ResumeMD != "" {
		b.WriteString(input.ResumeMD)
		b.WriteString("\n\n")
	} else {
		b.WriteString("(not present)\n\n")
	}

	// Recent history
	if len(input.RecentHistory) > 0 {
		b.WriteString("## Recent History\n\n")
		for _, h := range input.RecentHistory {
			fmt.Fprintf(&b, "- **%s** [%s]: %s\n", h.Date, h.Tag, h.Summary)
		}
		b.WriteString("\n")
	}

	// Active tasks
	if len(input.TaskSummaries) > 0 {
		b.WriteString("## Active Tasks\n\n")
		for _, t := range input.TaskSummaries {
			fmt.Fprintf(&b, "- **%s**: %s (status: %s)\n", t.Name, t.Title, t.Status)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// numberBullets adds [N] prefixes to bullet lines within each section
// so the LLM can reference them by index for stale entry detection.
func numberBullets(md string) string {
	lines := strings.Split(md, "\n")
	var result []string
	bulletIdx := 0
	inSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			inSection = true
			bulletIdx = 0
			result = append(result, line)
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			inSection = false
			result = append(result, line)
			continue
		}
		if inSection && strings.HasPrefix(trimmed, "- ") {
			numbered := fmt.Sprintf("[%d] %s", bulletIdx, line)
			result = append(result, numbered)
			bulletIdx++
			continue
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}
