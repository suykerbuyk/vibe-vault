package render

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/johns/vibe-vault/internal/narrative"
	"github.com/johns/vibe-vault/internal/transcript"
)

// RelatedNote holds a related session link and the reason for the relation.
type RelatedNote struct {
	Name   string // wikilink target, e.g. "2026-02-25-01"
	Reason string // human-readable, e.g. "3 shared files, branch: feature/auth"
}

// NoteData holds everything needed to render a session note.
type NoteData struct {
	Date         string // YYYY-MM-DD
	Project      string
	Branch       string
	Domain       string
	Model        string
	SessionID    string
	Iteration    int
	Duration     int // minutes
	Messages     int // user + assistant
	InputTokens  int
	OutputTokens int
	Title        string
	Summary      string
	PreviousNote string // wikilink target, e.g. "2026-02-21-03"
	FilesChanged []string
	Decisions    []string
	OpenThreads  []string
	EnrichedBy   string // model name, e.g. "grok-3-mini-fast"
	Tag          string // activity tag, e.g. "implementation"
	RelatedNotes []RelatedNote
	ToolCounts    map[string]int
	TotalTools    int
	Status        string // "completed" or "checkpoint"
	Commits       []narrative.Commit // Git commits extracted from tool output
	WorkPerformed string             // Rendered markdown for Work Performed section
	ProseDialogue string             // Rendered prose section (empty = use summary fallback)
}

// SessionNote renders a full Obsidian markdown note from NoteData.
func SessionNote(d NoteData) string {
	var b strings.Builder

	// Frontmatter
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("date: %s\n", d.Date))
	b.WriteString("type: session\n")
	b.WriteString(fmt.Sprintf("project: %s\n", d.Project))
	if d.Branch != "" {
		b.WriteString(fmt.Sprintf("branch: %s\n", d.Branch))
	}
	b.WriteString(fmt.Sprintf("domain: %s\n", d.Domain))
	if d.Model != "" {
		b.WriteString(fmt.Sprintf("model: %s\n", d.Model))
	}
	b.WriteString(fmt.Sprintf("session_id: \"%s\"\n", d.SessionID))
	b.WriteString(fmt.Sprintf("iteration: %d\n", d.Iteration))
	b.WriteString(fmt.Sprintf("duration_minutes: %d\n", d.Duration))
	b.WriteString(fmt.Sprintf("messages: %d\n", d.Messages))
	b.WriteString(fmt.Sprintf("tokens_in: %d\n", d.InputTokens))
	b.WriteString(fmt.Sprintf("tokens_out: %d\n", d.OutputTokens))
	if d.TotalTools > 0 {
		b.WriteString(fmt.Sprintf("tool_uses: %d\n", d.TotalTools))
		var toolNames []string
		for name := range d.ToolCounts {
			toolNames = append(toolNames, name)
		}
		sort.Strings(toolNames)
		b.WriteString(fmt.Sprintf("tools: [%s]\n", strings.Join(toolNames, ", ")))
	}
	status := d.Status
	if status == "" {
		status = "completed"
	}
	b.WriteString(fmt.Sprintf("status: %s\n", status))
	if len(d.Commits) > 0 {
		var shas []string
		for _, c := range d.Commits {
			shas = append(shas, c.SHA)
		}
		b.WriteString(fmt.Sprintf("commits: [%s]\n", strings.Join(shas, ", ")))
	}
	if d.Tag != "" {
		b.WriteString(fmt.Sprintf("tags: [cortana-session, %s]\n", d.Tag))
	} else {
		b.WriteString("tags: [cortana-session]\n")
	}
	b.WriteString(fmt.Sprintf("summary: \"%s\"\n", escapeYAML(d.Summary)))
	if d.PreviousNote != "" {
		b.WriteString(fmt.Sprintf("previous: \"[[%s]]\"\n", d.PreviousNote))
	}
	if len(d.RelatedNotes) > 0 {
		b.WriteString("related: [")
		for i, r := range d.RelatedNotes {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("\"[[%s]]\"", r.Name))
		}
		b.WriteString("]\n")
	}
	b.WriteString("---\n\n")

	// Title
	b.WriteString(fmt.Sprintf("# %s\n\n", d.Title))

	// Session Dialogue / What Happened
	if d.ProseDialogue != "" {
		b.WriteString("## Session Dialogue\n\n")
		b.WriteString(d.ProseDialogue)
		b.WriteString("\n")
	} else {
		b.WriteString("## What Happened\n\n")
		b.WriteString(fmt.Sprintf("%s\n\n", d.Summary))
	}

	// What Changed
	if len(d.FilesChanged) > 0 {
		b.WriteString("## What Changed\n\n")
		for _, f := range d.FilesChanged {
			b.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
		b.WriteString("\n")
	}

	// Commits
	if len(d.Commits) > 0 {
		b.WriteString("## Commits\n\n")
		for _, c := range d.Commits {
			b.WriteString(fmt.Sprintf("- `%s` %s\n", c.SHA, c.Message))
		}
		b.WriteString("\n")
	}

	// Work Performed
	if d.WorkPerformed != "" {
		b.WriteString("## Work Performed\n\n")
		b.WriteString(d.WorkPerformed)
		b.WriteString("\n")
	}

	// Tool Usage
	if d.TotalTools > 0 {
		b.WriteString("## Tool Usage\n\n")
		b.WriteString(fmt.Sprintf("**Total: %d tool calls**\n\n", d.TotalTools))
		b.WriteString("| Tool | Count |\n")
		b.WriteString("|------|-------|\n")
		var toolNames []string
		for name := range d.ToolCounts {
			toolNames = append(toolNames, name)
		}
		sort.Strings(toolNames)
		for _, name := range toolNames {
			b.WriteString(fmt.Sprintf("| %s | %d |\n", name, d.ToolCounts[name]))
		}
		b.WriteString("\n")
	}

	// Key Decisions
	if len(d.Decisions) > 0 {
		b.WriteString("## Key Decisions\n\n")
		for _, d := range d.Decisions {
			b.WriteString(fmt.Sprintf("- %s\n", d))
		}
		b.WriteString("\n")
	}

	// Open Threads
	if len(d.OpenThreads) > 0 {
		b.WriteString("## Open Threads\n\n")
		for _, t := range d.OpenThreads {
			b.WriteString(fmt.Sprintf("- [ ] %s\n", t))
		}
		b.WriteString("\n")
	}

	// Related Sessions
	if len(d.RelatedNotes) > 0 {
		b.WriteString("## Related Sessions\n\n")
		for _, r := range d.RelatedNotes {
			b.WriteString(fmt.Sprintf("- [[%s]] — %s\n", r.Name, r.Reason))
		}
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("---\n")
	if d.EnrichedBy != "" {
		b.WriteString(fmt.Sprintf("*vv v0.1.0 | enriched by %s*\n", d.EnrichedBy))
	} else {
		b.WriteString("*vv v0.1.0*\n")
	}

	return b.String()
}

// NoteDataFromTranscript builds NoteData from parsed transcript data and session metadata.
func NoteDataFromTranscript(t *transcript.Transcript, project, domain, branch, sessionID string, iteration int, previous string) NoteData {
	s := t.Stats

	date := s.StartTime.Format("2006-01-02")
	if date == "0001-01-01" {
		date = time.Now().Format("2006-01-02")
	}

	firstMsg := transcript.FirstUserMessage(t)
	title := titleFromFirstMessage(firstMsg)
	summary := title
	if summary == "Session" {
		summary = "Claude Code session"
	}

	// Total input tokens = direct + cache reads + cache writes
	totalInput := s.InputTokens + s.CacheReads + s.CacheWrites

	// Collect changed files (written/edited), strip common prefix
	var filesChanged []string
	for f := range s.FilesWritten {
		filesChanged = append(filesChanged, shortenPath(f, s.CWD))
	}
	sort.Strings(filesChanged)

	return NoteData{
		Date:         date,
		Project:      project,
		Branch:       branch,
		Domain:       domain,
		Model:        s.Model,
		SessionID:    sessionID,
		Iteration:    iteration,
		Duration:     int(s.Duration.Minutes()),
		Messages:     s.UserMessages + s.AssistantMessages,
		InputTokens:  totalInput,
		OutputTokens: s.OutputTokens,
		Title:        title,
		Summary:      summary,
		PreviousNote: previous,
		FilesChanged: filesChanged,
		ToolCounts:   s.ToolCounts,
		TotalTools:   s.ToolUses,
	}
}

// NoteFilename returns the filename for a session note: YYYY-MM-DD-NN.md
func NoteFilename(date string, iteration int) string {
	return fmt.Sprintf("%s-%02d.md", date, iteration)
}

// NoteRelPath returns the relative path within the vault for a session note.
func NoteRelPath(project, date string, iteration int) string {
	return filepath.Join("Sessions", project, NoteFilename(date, iteration))
}

func titleFromFirstMessage(msg string) string {
	if msg == "" {
		return "Session"
	}

	msg = strings.TrimSpace(msg)

	// Take first line
	if idx := strings.IndexByte(msg, '\n'); idx > 0 {
		msg = msg[:idx]
	}

	// Skip trivials
	lower := strings.ToLower(msg)
	trivials := []string{"hi", "hello", "hey", "ok", "okay", "yes", "no", "thanks", "thank you", "y", "n"}
	for _, t := range trivials {
		if lower == t {
			return "Session"
		}
	}

	// Truncate
	if len(msg) > 80 {
		msg = msg[:77] + "..."
	}

	return msg
}

func escapeYAML(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

func shortenPath(path, cwd string) string {
	if cwd != "" && strings.HasPrefix(path, cwd+"/") {
		return path[len(cwd)+1:]
	}
	return path
}
