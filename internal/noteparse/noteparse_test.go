package noteparse

import (
	"strings"
	"testing"
)

const sampleNote = `---
date: 2026-02-25
type: session
project: vibe-vault
branch: feature/index
domain: personal
model: claude-sonnet-4-20250514
session_id: "abc-123"
iteration: 2
duration_minutes: 45
messages: 12
tokens_in: 5000
tokens_out: 3000
status: completed
tags: [cortana-session, implementation]
summary: "Added index rebuild command"
previous: "[[2026-02-24-01]]"
---

# Added index rebuild command

## What Happened

Added index rebuild command for reprocessing session notes.

## What Changed

- ` + "`internal/index/rebuild.go`" + `
- ` + "`cmd/vv/main.go`" + `
- ` + "`internal/index/index.go`" + `

## Key Decisions

- Use line-based parser instead of YAML library
- Skip files prefixed with underscore

## Open Threads

- [ ] Add progress bar for large vaults
- [ ] Support custom session directories

---
*vv v0.1.0 | enriched by grok-3-mini-fast*
`

func TestParseFrontmatter(t *testing.T) {
	note, err := Parse(strings.NewReader(sampleNote))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if note.SessionID != "abc-123" {
		t.Errorf("SessionID = %q, want %q", note.SessionID, "abc-123")
	}
	if note.Date != "2026-02-25" {
		t.Errorf("Date = %q, want %q", note.Date, "2026-02-25")
	}
	if note.Project != "vibe-vault" {
		t.Errorf("Project = %q, want %q", note.Project, "vibe-vault")
	}
	if note.Branch != "feature/index" {
		t.Errorf("Branch = %q, want %q", note.Branch, "feature/index")
	}
	if note.Domain != "personal" {
		t.Errorf("Domain = %q, want %q", note.Domain, "personal")
	}
	if note.Iteration != "2" {
		t.Errorf("Iteration = %q, want %q", note.Iteration, "2")
	}
	if note.Summary != "Added index rebuild command" {
		t.Errorf("Summary = %q, want %q", note.Summary, "Added index rebuild command")
	}
	if note.Previous != "[[2026-02-24-01]]" {
		t.Errorf("Previous = %q, want %q", note.Previous, "[[2026-02-24-01]]")
	}
}

func TestParseBracketList(t *testing.T) {
	note, err := Parse(strings.NewReader(sampleNote))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(note.Tags) != 2 {
		t.Fatalf("Tags len = %d, want 2", len(note.Tags))
	}
	if note.Tags[0] != "cortana-session" {
		t.Errorf("Tags[0] = %q, want %q", note.Tags[0], "cortana-session")
	}
	if note.Tags[1] != "implementation" {
		t.Errorf("Tags[1] = %q, want %q", note.Tags[1], "implementation")
	}
	if note.Tag != "implementation" {
		t.Errorf("Tag = %q, want %q", note.Tag, "implementation")
	}
}

func TestExtractDecisions(t *testing.T) {
	note, err := Parse(strings.NewReader(sampleNote))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(note.Decisions) != 2 {
		t.Fatalf("Decisions len = %d, want 2", len(note.Decisions))
	}
	if note.Decisions[0] != "Use line-based parser instead of YAML library" {
		t.Errorf("Decisions[0] = %q", note.Decisions[0])
	}
}

func TestExtractOpenThreads(t *testing.T) {
	note, err := Parse(strings.NewReader(sampleNote))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(note.OpenThreads) != 2 {
		t.Fatalf("OpenThreads len = %d, want 2", len(note.OpenThreads))
	}
	if note.OpenThreads[0] != "Add progress bar for large vaults" {
		t.Errorf("OpenThreads[0] = %q", note.OpenThreads[0])
	}
}

func TestExtractFilesChanged(t *testing.T) {
	note, err := Parse(strings.NewReader(sampleNote))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if len(note.FilesChanged) != 3 {
		t.Fatalf("FilesChanged len = %d, want 3", len(note.FilesChanged))
	}
	if note.FilesChanged[0] != "internal/index/rebuild.go" {
		t.Errorf("FilesChanged[0] = %q", note.FilesChanged[0])
	}
}

func TestMissingFrontmatter(t *testing.T) {
	input := "# Just a heading\n\nSome body text.\n"
	note, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if note.SessionID != "" {
		t.Errorf("SessionID = %q, want empty", note.SessionID)
	}
	if len(note.Frontmatter) != 0 {
		t.Errorf("Frontmatter len = %d, want 0", len(note.Frontmatter))
	}
}

func TestEmptyFile(t *testing.T) {
	note, err := Parse(strings.NewReader(""))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if note.SessionID != "" {
		t.Errorf("SessionID = %q, want empty", note.SessionID)
	}
}
