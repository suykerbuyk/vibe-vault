package render

import (
	"strings"
	"testing"
	"time"

	"github.com/johns/vibe-vault/internal/transcript"
)

func TestSessionNote_AllFields(t *testing.T) {
	d := NoteData{
		Date:         "2026-02-22",
		Project:      "vibe-vault",
		Branch:       "feature/auth",
		Domain:       "work",
		Model:        "claude-opus-4-6",
		SessionID:    "sess-abc",
		Iteration:    1,
		Duration:     15,
		Messages:     8,
		InputTokens:  5000,
		OutputTokens: 2000,
		Title:        "Implement auth system",
		Summary:      "Added JWT authentication",
		PreviousNote: "2026-02-21-03",
		FilesChanged: []string{"auth.go", "middleware.go"},
		Decisions:    []string{"Use JWT over sessions"},
		OpenThreads:  []string{"Add refresh tokens"},
		EnrichedBy:   "grok-3-mini-fast",
		Tag:          "implementation",
		RelatedNotes: []RelatedNote{{Name: "2026-02-20-01", Reason: "2 shared files"}},
	}

	out := SessionNote(d)

	// Frontmatter
	checks := []string{
		"date: 2026-02-22",
		"type: session",
		"project: vibe-vault",
		"branch: feature/auth",
		"domain: work",
		"model: claude-opus-4-6",
		`session_id: "sess-abc"`,
		"iteration: 1",
		"duration_minutes: 15",
		"messages: 8",
		"tokens_in: 5000",
		"tokens_out: 2000",
		"status: completed",
		"tags: [cortana-session, implementation]",
		`summary: "Added JWT authentication"`,
		`previous: "[[2026-02-21-03]]"`,
		`related: ["[[2026-02-20-01]]"]`,
	}
	for _, c := range checks {
		if !strings.Contains(out, c) {
			t.Errorf("missing %q in output", c)
		}
	}

	// Sections
	sections := []string{
		"# Implement auth system",
		"## What Happened",
		"## What Changed",
		"- `auth.go`",
		"- `middleware.go`",
		"## Key Decisions",
		"- Use JWT over sessions",
		"## Open Threads",
		"- [ ] Add refresh tokens",
		"## Related Sessions",
		"[[2026-02-20-01]] — 2 shared files",
		"*vv v0.1.0 | enriched by grok-3-mini-fast*",
	}
	for _, s := range sections {
		if !strings.Contains(out, s) {
			t.Errorf("missing section %q in output", s)
		}
	}
}

func TestSessionNote_MinimalFields(t *testing.T) {
	d := NoteData{
		Date:      "2026-02-22",
		Project:   "myproject",
		Domain:    "personal",
		SessionID: "sess-min",
		Title:     "Session",
		Summary:   "Quick session",
	}

	out := SessionNote(d)

	if !strings.Contains(out, "# Session") {
		t.Error("missing title")
	}
	if !strings.Contains(out, "tags: [cortana-session]") {
		t.Error("missing default tags")
	}
	if !strings.Contains(out, "*vv v0.1.0*") {
		t.Error("missing plain footer")
	}

	// Optional sections should be absent
	for _, absent := range []string{"## What Changed", "## Key Decisions", "## Open Threads", "## Related Sessions", "branch:", "model:", "previous:"} {
		if strings.Contains(out, absent) {
			t.Errorf("unexpected %q in minimal output", absent)
		}
	}
}

func TestSessionNote_TagRendering(t *testing.T) {
	tests := []struct {
		tag  string
		want string
	}{
		{"", "tags: [cortana-session]"},
		{"debugging", "tags: [cortana-session, debugging]"},
	}
	for _, tt := range tests {
		d := NoteData{Date: "2026-01-01", Project: "p", Domain: "d", SessionID: "s", Title: "T", Summary: "S", Tag: tt.tag}
		out := SessionNote(d)
		if !strings.Contains(out, tt.want) {
			t.Errorf("tag=%q: want %q in output", tt.tag, tt.want)
		}
	}
}

func TestSessionNote_PreviousNote(t *testing.T) {
	d := NoteData{Date: "2026-01-01", Project: "p", Domain: "d", SessionID: "s", Title: "T", Summary: "S", PreviousNote: "2026-01-01-01"}
	out := SessionNote(d)
	if !strings.Contains(out, `previous: "[[2026-01-01-01]]"`) {
		t.Error("missing previous note link")
	}
}

func TestSessionNote_RelatedNotes(t *testing.T) {
	d := NoteData{
		Date: "2026-01-01", Project: "p", Domain: "d", SessionID: "s", Title: "T", Summary: "S",
		RelatedNotes: []RelatedNote{
			{Name: "2026-01-01-01", Reason: "3 shared files"},
			{Name: "2025-12-31-02", Reason: "branch: feat/x"},
		},
	}
	out := SessionNote(d)

	if !strings.Contains(out, `related: ["[[2026-01-01-01]]", "[[2025-12-31-02]]"]`) {
		t.Error("missing related frontmatter")
	}
	if !strings.Contains(out, "## Related Sessions") {
		t.Error("missing Related Sessions section")
	}
	if !strings.Contains(out, "[[2026-01-01-01]] — 3 shared files") {
		t.Error("missing first related note in section")
	}
}

func TestSessionNote_YAMLEscape(t *testing.T) {
	d := NoteData{
		Date: "2026-01-01", Project: "p", Domain: "d", SessionID: "s", Title: "T",
		Summary: `Fixed "broken" paths with C:\Users`,
	}
	out := SessionNote(d)
	if !strings.Contains(out, `summary: "Fixed \"broken\" paths with C:\\Users"`) {
		t.Errorf("YAML escaping failed, got summary line: %s",
			extractLine(out, "summary:"))
	}
}

func TestNoteDataFromTranscript(t *testing.T) {
	tr := &transcript.Transcript{
		Entries: []transcript.Entry{
			{
				Type:      "user",
				SessionID: "sess-1",
				Message:   &transcript.Message{Role: "user", Content: "Build the API"},
			},
		},
		Stats: transcript.Stats{
			StartTime:         time.Date(2026, 2, 22, 10, 0, 0, 0, time.UTC),
			EndTime:           time.Date(2026, 2, 22, 10, 15, 0, 0, time.UTC),
			Duration:          15 * time.Minute,
			UserMessages:      3,
			AssistantMessages: 3,
			InputTokens:       1000,
			OutputTokens:      500,
			CacheReads:        2000,
			CacheWrites:       800,
			Model:             "claude-opus-4-6",
			CWD:               "/home/user/project",
			FilesWritten:      map[string]bool{"/home/user/project/api.go": true},
		},
	}

	nd := NoteDataFromTranscript(tr, "myproj", "work", "main", "sess-1", 2, "2026-02-21-01")

	if nd.Date != "2026-02-22" {
		t.Errorf("date = %q, want 2026-02-22", nd.Date)
	}
	if nd.Project != "myproj" {
		t.Errorf("project = %q", nd.Project)
	}
	if nd.InputTokens != 1000+2000+800 {
		t.Errorf("input tokens = %d, want %d", nd.InputTokens, 3800)
	}
	if nd.OutputTokens != 500 {
		t.Errorf("output tokens = %d", nd.OutputTokens)
	}
	if nd.Messages != 6 {
		t.Errorf("messages = %d, want 6", nd.Messages)
	}
	if nd.Duration != 15 {
		t.Errorf("duration = %d, want 15", nd.Duration)
	}
	if nd.Title != "Build the API" {
		t.Errorf("title = %q", nd.Title)
	}
	if nd.PreviousNote != "2026-02-21-01" {
		t.Errorf("previous = %q", nd.PreviousNote)
	}
	if len(nd.FilesChanged) != 1 || nd.FilesChanged[0] != "api.go" {
		t.Errorf("files = %v", nd.FilesChanged)
	}
}

func TestNoteDataFromTranscript_ZeroTime(t *testing.T) {
	tr := &transcript.Transcript{
		Stats: transcript.Stats{
			// StartTime is zero
			FilesWritten: make(map[string]bool),
		},
	}

	nd := NoteDataFromTranscript(tr, "proj", "personal", "", "s", 1, "")
	today := time.Now().Format("2006-01-02")
	if nd.Date != today {
		t.Errorf("date = %q, want today %q", nd.Date, today)
	}
}

func TestNoteFilename(t *testing.T) {
	tests := []struct {
		date      string
		iteration int
		want      string
	}{
		{"2026-02-22", 1, "2026-02-22-01.md"},
		{"2026-02-22", 10, "2026-02-22-10.md"},
		{"2025-12-31", 3, "2025-12-31-03.md"},
	}
	for _, tt := range tests {
		got := NoteFilename(tt.date, tt.iteration)
		if got != tt.want {
			t.Errorf("NoteFilename(%q, %d) = %q, want %q", tt.date, tt.iteration, got, tt.want)
		}
	}
}

func TestNoteRelPath(t *testing.T) {
	got := NoteRelPath("vibe-vault", "2026-02-22", 1)
	want := "Sessions/vibe-vault/2026-02-22-01.md"
	if got != want {
		t.Errorf("NoteRelPath = %q, want %q", got, want)
	}
}

func TestSessionNote_ToolUsage(t *testing.T) {
	d := NoteData{
		Date: "2026-01-01", Project: "p", Domain: "d", SessionID: "s", Title: "T", Summary: "S",
		ToolCounts: map[string]int{"Bash": 12, "Read": 15, "Write": 8},
		TotalTools: 35,
	}
	out := SessionNote(d)

	// Frontmatter
	if !strings.Contains(out, "tool_uses: 35") {
		t.Error("missing tool_uses in frontmatter")
	}
	if !strings.Contains(out, "tools: [Bash, Read, Write]") {
		t.Errorf("missing/wrong tools in frontmatter, got: %s", extractLine(out, "tools:"))
	}

	// Body section
	if !strings.Contains(out, "## Tool Usage") {
		t.Error("missing ## Tool Usage section")
	}
	if !strings.Contains(out, "**Total: 35 tool calls**") {
		t.Error("missing total tool calls line")
	}
	if !strings.Contains(out, "| Bash | 12 |") {
		t.Error("missing Bash row in tool table")
	}
	if !strings.Contains(out, "| Read | 15 |") {
		t.Error("missing Read row in tool table")
	}
	if !strings.Contains(out, "| Write | 8 |") {
		t.Error("missing Write row in tool table")
	}
}

func TestSessionNote_CheckpointStatus(t *testing.T) {
	d := NoteData{
		Date: "2026-01-01", Project: "p", Domain: "d", SessionID: "s", Title: "T", Summary: "S",
		Status: "checkpoint",
	}
	out := SessionNote(d)
	if !strings.Contains(out, "status: checkpoint") {
		t.Error("missing status: checkpoint in frontmatter")
	}
	if strings.Contains(out, "status: completed") {
		t.Error("should not have status: completed when checkpoint")
	}
}

func TestSessionNote_NoTools(t *testing.T) {
	d := NoteData{
		Date: "2026-01-01", Project: "p", Domain: "d", SessionID: "s", Title: "T", Summary: "S",
	}
	out := SessionNote(d)
	if strings.Contains(out, "## Tool Usage") {
		t.Error("should not have Tool Usage section when TotalTools == 0")
	}
	if strings.Contains(out, "tool_uses:") {
		t.Error("should not have tool_uses in frontmatter when TotalTools == 0")
	}
}

func TestSessionNote_ProseDialogue(t *testing.T) {
	d := NoteData{
		Date: "2026-01-01", Project: "p", Domain: "d", SessionID: "s", Title: "T", Summary: "S",
		ProseDialogue: "> **User:** Add auth\n\nI'll implement JWT authentication.\n",
	}
	out := SessionNote(d)
	if !strings.Contains(out, "## Session Dialogue") {
		t.Error("missing ## Session Dialogue section")
	}
	if strings.Contains(out, "## What Happened") {
		t.Error("should not have ## What Happened when prose is present")
	}
	if !strings.Contains(out, "> **User:** Add auth") {
		t.Error("missing prose content")
	}
}

func TestSessionNote_ProseDialogueFallback(t *testing.T) {
	d := NoteData{
		Date: "2026-01-01", Project: "p", Domain: "d", SessionID: "s", Title: "T",
		Summary: "Quick session",
	}
	out := SessionNote(d)
	if !strings.Contains(out, "## What Happened") {
		t.Error("missing ## What Happened fallback")
	}
	if strings.Contains(out, "## Session Dialogue") {
		t.Error("should not have ## Session Dialogue when prose is empty")
	}
	if !strings.Contains(out, "Quick session") {
		t.Error("missing summary text")
	}
}

// extractLine finds the first line containing substr for error messages.
func extractLine(text, substr string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, substr) {
			return line
		}
	}
	return "<not found>"
}
