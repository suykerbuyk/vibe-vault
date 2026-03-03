package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadNotes_Empty(t *testing.T) {
	vault := t.TempDir()

	notes, err := ReadNotes(vault)
	if err != nil {
		t.Fatalf("ReadNotes: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("len = %d, want 0", len(notes))
	}
}

func TestReadNotes_LessonsAndDecisions(t *testing.T) {
	vault := t.TempDir()

	// Write a lesson via WriteNote
	lesson := Note{
		Type:       "lesson",
		Title:      "Always validate input",
		Summary:    "Input validation prevents downstream errors",
		Body:       "Validate at system boundaries.",
		Project:    "myproject",
		Date:       "2026-02-28",
		Confidence: 0.85,
		Category:   "error-handling",
	}
	if _, err := WriteNote(vault, lesson); err != nil {
		t.Fatalf("WriteNote lesson: %v", err)
	}

	// Write a decision via WriteNote
	decision := Note{
		Type:       "decision",
		Title:      "Use JWT for auth",
		Summary:    "JWT chosen over sessions for statelessness",
		Body:       "JWT tokens with short expiry.",
		Project:    "myproject",
		Date:       "2026-02-28",
		Confidence: 0.90,
		Category:   "architecture",
	}
	if _, err := WriteNote(vault, decision); err != nil {
		t.Fatalf("WriteNote decision: %v", err)
	}

	notes, err := ReadNotes(vault)
	if err != nil {
		t.Fatalf("ReadNotes: %v", err)
	}

	if len(notes) != 2 {
		t.Fatalf("len = %d, want 2", len(notes))
	}

	// Check that we got one of each type
	types := map[string]bool{}
	for _, n := range notes {
		types[n.Type] = true
		if n.Project != "myproject" {
			t.Errorf("Project = %q, want myproject", n.Project)
		}
		if n.Date != "2026-02-28" {
			t.Errorf("Date = %q, want 2026-02-28", n.Date)
		}
		if n.NotePath == "" {
			t.Error("NotePath should be set")
		}
		if n.Summary == "" {
			t.Error("Summary should be set")
		}
		if n.Category == "" {
			t.Error("Category should be set")
		}
		if n.Confidence == 0 {
			t.Error("Confidence should be set")
		}
	}
	if !types["lesson"] || !types["decision"] {
		t.Errorf("types = %v, want both lesson and decision", types)
	}
}

func TestReadNotes_SkipsGitkeep(t *testing.T) {
	vault := t.TempDir()
	dir := filepath.Join(vault, "Knowledge", "learnings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	notes, err := ReadNotes(vault)
	if err != nil {
		t.Fatalf("ReadNotes: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("len = %d, want 0 (.gitkeep should be skipped)", len(notes))
	}
}

func TestReadNotes_SkipsArchived(t *testing.T) {
	vault := t.TempDir()
	dir := filepath.Join(vault, "Knowledge", "learnings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	archivedNote := `---
date: 2026-02-28
type: lesson
project: myproject
status: archived
summary: "Old lesson"
confidence: 0.80
category: testing
---

# Old Lesson
`
	if err := os.WriteFile(filepath.Join(dir, "2026-02-28-old-lesson.md"), []byte(archivedNote), 0o644); err != nil {
		t.Fatal(err)
	}

	notes, err := ReadNotes(vault)
	if err != nil {
		t.Fatalf("ReadNotes: %v", err)
	}
	if len(notes) != 0 {
		t.Errorf("len = %d, want 0 (archived notes should be skipped)", len(notes))
	}
}

func TestParseNoteFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test-note.md")

	content := `---
date: 2026-03-01
type: lesson
project: test-project
status: active
summary: "Test summary here"
confidence: 0.75
category: testing
tags:
  - knowledge
  - lesson
source_sessions:
  - "[[2026-03-01-01]]"
---

# Test Title

## What Was Learned

Some content here.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	note, err := parseNoteFrontmatter(path)
	if err != nil {
		t.Fatalf("parseNoteFrontmatter: %v", err)
	}

	if note.Type != "lesson" {
		t.Errorf("Type = %q, want lesson", note.Type)
	}
	if note.Project != "test-project" {
		t.Errorf("Project = %q, want test-project", note.Project)
	}
	if note.Date != "2026-03-01" {
		t.Errorf("Date = %q, want 2026-03-01", note.Date)
	}
	if note.Summary != "Test summary here" {
		t.Errorf("Summary = %q, want 'Test summary here'", note.Summary)
	}
	if note.Confidence != 0.75 {
		t.Errorf("Confidence = %f, want 0.75", note.Confidence)
	}
	if note.Category != "testing" {
		t.Errorf("Category = %q, want testing", note.Category)
	}
	if note.Title != "Test Title" {
		t.Errorf("Title = %q, want 'Test Title'", note.Title)
	}
}
