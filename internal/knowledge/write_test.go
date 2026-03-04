// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteNote_Basic(t *testing.T) {
	vaultPath := t.TempDir()

	note := Note{
		Type:          "lesson",
		Title:         "Don't use json skip",
		Summary:       "Avoid json:\"-\" when you need the field",
		Body:          "The json skip tag causes data loss.",
		Date:          "2026-03-04",
		Project:       "test-proj",
		SourceSession: "2026-03-04-01",
		Confidence:    0.9,
		Category:      "serialization",
	}

	relPath, err := WriteNote(vaultPath, note)
	if err != nil {
		t.Fatalf("WriteNote: %v", err)
	}

	if relPath == "" {
		t.Fatal("expected non-empty relPath")
	}

	absPath := filepath.Join(vaultPath, relPath)
	data, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read written note: %v", err)
	}

	if !strings.Contains(string(data), "Don't use json skip") {
		t.Error("note content missing title")
	}
}

func TestWriteNote_SlugCollision(t *testing.T) {
	vaultPath := t.TempDir()

	note := Note{
		Type:          "lesson",
		Title:         "Test Collision",
		Summary:       "First note",
		Body:          "Body one.",
		Date:          "2026-03-04",
		Project:       "proj",
		SourceSession: "2026-03-04-01",
		Confidence:    0.8,
		Category:      "testing",
	}

	// Write the first note
	relPath1, err := WriteNote(vaultPath, note)
	if err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Write a second note with the same slug — should get -2 suffix
	note.Summary = "Second note"
	note.Body = "Body two."
	relPath2, err := WriteNote(vaultPath, note)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}

	if relPath1 == relPath2 {
		t.Errorf("collision not resolved: both got %q", relPath1)
	}

	if !strings.Contains(relPath2, "-2.md") {
		t.Errorf("expected -2 suffix, got %q", relPath2)
	}

	// Verify both files exist with distinct content
	data1, _ := os.ReadFile(filepath.Join(vaultPath, relPath1))
	data2, _ := os.ReadFile(filepath.Join(vaultPath, relPath2))
	if strings.Contains(string(data1), "Second note") {
		t.Error("first note should not contain second note content")
	}
	if !strings.Contains(string(data2), "Second note") {
		t.Error("second note should contain its own content")
	}
}

func TestWriteNote_SlugCollisionMultiple(t *testing.T) {
	vaultPath := t.TempDir()

	note := Note{
		Type:          "decision",
		Title:         "Same Title",
		Summary:       "Note",
		Body:          "Body.",
		Date:          "2026-03-04",
		Project:       "proj",
		SourceSession: "2026-03-04-01",
		Confidence:    0.8,
		Category:      "architecture",
	}

	// Write 3 notes with same slug
	paths := make([]string, 3)
	for i := 0; i < 3; i++ {
		note.Summary = "Note " + string(rune('A'+i))
		p, err := WriteNote(vaultPath, note)
		if err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		paths[i] = p
	}

	// All paths should be distinct
	seen := make(map[string]bool)
	for _, p := range paths {
		if seen[p] {
			t.Errorf("duplicate path: %q", p)
		}
		seen[p] = true
	}

	// Verify suffixes: base, -2, -3
	if strings.Contains(paths[0], "-2") || strings.Contains(paths[0], "-3") {
		t.Errorf("first path should be base: %q", paths[0])
	}
	if !strings.Contains(paths[1], "-2.md") {
		t.Errorf("second path should have -2: %q", paths[1])
	}
	if !strings.Contains(paths[2], "-3.md") {
		t.Errorf("third path should have -3: %q", paths[2])
	}
}

func TestWriteNote_AllSlotsTaken(t *testing.T) {
	vaultPath := t.TempDir()

	note := Note{
		Type:          "lesson",
		Title:         "Full Slots",
		Summary:       "Note",
		Body:          "Body.",
		Date:          "2026-03-04",
		Project:       "proj",
		SourceSession: "2026-03-04-01",
		Confidence:    0.8,
		Category:      "testing",
	}

	// Write 10 notes (base + -2 through -10)
	for i := 0; i < 10; i++ {
		note.Summary = "Note " + string(rune('A'+i))
		p, err := WriteNote(vaultPath, note)
		if err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		if p == "" {
			t.Fatalf("write %d: got empty path before all slots taken", i)
		}
	}

	// 11th write should return empty path (all slots taken)
	note.Summary = "Overflow"
	p, err := WriteNote(vaultPath, note)
	if err != nil {
		t.Fatalf("overflow write: %v", err)
	}
	if p != "" {
		t.Errorf("expected empty path when all slots taken, got %q", p)
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Simple Title", "simple-title"},
		{"  spaces  everywhere  ", "spaces-everywhere"},
		{"special!@#chars", "special-chars"},
		{"UPPERCASE", "uppercase"},
		{"", "note"},
		{"a-b-c", "a-b-c"},
	}

	for _, tt := range tests {
		got := slugify(tt.input)
		if got != tt.want {
			t.Errorf("slugify(%q): got %q, want %q", tt.input, got, tt.want)
		}
	}
}
