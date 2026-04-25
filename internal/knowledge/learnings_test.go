// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package knowledge

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeVault creates a temp vault root with the given relative files.
// Files are written as-is (including frontmatter) so each test can
// verify end-to-end parsing behavior without shared fixtures.
func writeVault(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return root
}

func TestListEmptyDirectoryReturnsEmptySlice(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/.keep": "",
	})
	got, err := List(root, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(got))
	}
}

func TestListMissingDirectoryReturnsEmptySlice(t *testing.T) {
	root := t.TempDir()
	got, err := List(root, "")
	if err != nil {
		t.Fatalf("List missing dir: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestListSortsAlphabeticallyBySlug(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/zeta.md":  validLearning("Zeta", "last", "user"),
		"Knowledge/learnings/alpha.md": validLearning("Alpha", "first", "user"),
		"Knowledge/learnings/mid.md":   validLearning("Mid", "middle", "user"),
	})
	got, err := List(root, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	wantOrder := []string{"alpha", "mid", "zeta"}
	for i, w := range wantOrder {
		if got[i].Slug != w {
			t.Errorf("entry %d: got slug %q, want %q", i, got[i].Slug, w)
		}
	}
}

func TestListFilterByType(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/u1.md": validLearning("User 1", "a", "user"),
		"Knowledge/learnings/f1.md": validLearning("Feedback 1", "b", "feedback"),
		"Knowledge/learnings/r1.md": validLearning("Ref 1", "c", "reference"),
		"Knowledge/learnings/u2.md": validLearning("User 2", "d", "user"),
	})
	got, err := List(root, "user")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 user entries, got %d", len(got))
	}
	for _, m := range got {
		if m.Type != "user" {
			t.Errorf("filtered result has type %q, want user", m.Type)
		}
	}
}

func TestListRejectsTypeProject(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/bad.md":  validLearning("Bad", "nope", "project"),
		"Knowledge/learnings/good.md": validLearning("Good", "ok", "user"),
	})
	var warn bytes.Buffer
	got, err := listWithWarn(root, "", &warn)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "good" {
		t.Errorf("expected only 'good' entry, got %v", got)
	}
	if !strings.Contains(warn.String(), "bad.md") {
		t.Errorf("expected warning for bad.md, got %q", warn.String())
	}
	if !strings.Contains(warn.String(), "project") {
		t.Errorf("warning should mention the rejected type, got %q", warn.String())
	}
}

func TestListRejectsUnknownType(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/weird.md": validLearning("Weird", "nope", "arbitrary"),
	})
	var warn bytes.Buffer
	got, err := listWithWarn(root, "", &warn)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 entries, got %d", len(got))
	}
	if !strings.Contains(warn.String(), "weird.md") {
		t.Errorf("expected warning mentioning weird.md, got %q", warn.String())
	}
}

func TestListSkipsMalformedFrontmatter(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/nofm.md":    "No frontmatter here, just a body.\n",
		"Knowledge/learnings/unterm.md":  "---\nname: X\ndescription: Y\ntype: user\n",
		"Knowledge/learnings/missing.md": "---\nname: X\n---\n\nbody\n",
		"Knowledge/learnings/ok.md":      validLearning("OK", "good", "user"),
	})
	var warn bytes.Buffer
	got, err := listWithWarn(root, "", &warn)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "ok" {
		t.Errorf("expected only 'ok' entry, got %v", got)
	}
	for _, bad := range []string{"nofm.md", "unterm.md", "missing.md"} {
		if !strings.Contains(warn.String(), bad) {
			t.Errorf("expected warning for %s, got %q", bad, warn.String())
		}
	}
}

func TestListSkipsNonMarkdownFiles(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/README.txt": "not markdown",
		"Knowledge/learnings/a.md":       validLearning("A", "x", "user"),
	})
	got, err := List(root, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
}

func TestGetReturnsFullContent(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/testing.md": "---\nname: Testing philosophy\ndescription: proven end-to-end\ntype: user\n---\n\nThe actual body\nspans multiple lines.\n",
	})
	got, err := Get(root, "testing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Slug != "testing" {
		t.Errorf("slug = %q", got.Slug)
	}
	if got.Name != "Testing philosophy" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Type != "user" {
		t.Errorf("type = %q", got.Type)
	}
	if !strings.Contains(got.Content, "The actual body") {
		t.Errorf("content = %q", got.Content)
	}
	if !strings.Contains(got.Content, "spans multiple lines") {
		t.Errorf("content missing second line: %q", got.Content)
	}
}

func TestGetUnknownSlugErrorListsAvailable(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/alpha.md": validLearning("Alpha", "a", "user"),
		"Knowledge/learnings/beta.md":  validLearning("Beta", "b", "user"),
	})
	_, err := Get(root, "gamma")
	if err == nil {
		t.Fatal("expected error for unknown slug")
	}
	msg := err.Error()
	if !strings.Contains(msg, "gamma") {
		t.Errorf("error should mention requested slug: %q", msg)
	}
	if !strings.Contains(msg, "alpha") || !strings.Contains(msg, "beta") {
		t.Errorf("error should list available slugs: %q", msg)
	}
}

func TestGetUnknownSlugEmptyDir(t *testing.T) {
	root := t.TempDir()
	_, err := Get(root, "anything")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no learnings available") {
		t.Errorf("expected 'no learnings available' in error, got %q", err.Error())
	}
}

func TestGetRejectsSlugWithPathSeparators(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/a.md": validLearning("A", "a", "user"),
	})
	cases := []string{"../etc/passwd", "sub/a", "a/../a", "..", "a\\b"}
	for _, s := range cases {
		if _, err := Get(root, s); err == nil {
			t.Errorf("expected error for slug %q", s)
		}
	}
}

func TestGetRejectsEmptySlug(t *testing.T) {
	root := t.TempDir()
	if _, err := Get(root, ""); err == nil {
		t.Error("expected error for empty slug")
	}
}

func TestGetRejectsMalformedFile(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/bad.md": "---\nname: only-name\n---\n\nbody\n",
	})
	_, err := Get(root, "bad")
	if err == nil {
		t.Fatal("expected error for malformed file")
	}
	if !strings.Contains(err.Error(), "malformed") {
		t.Errorf("error should say 'malformed', got %q", err.Error())
	}
}

func TestGetRejectsTypeProject(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/p.md": validLearning("P", "d", "project"),
	})
	_, err := Get(root, "p")
	if err == nil {
		t.Fatal("expected error for type: project")
	}
}

func TestCountMatchesListLength(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/a.md":   validLearning("A", "x", "user"),
		"Knowledge/learnings/b.md":   validLearning("B", "y", "feedback"),
		"Knowledge/learnings/bad.md": validLearning("Bad", "z", "project"),
	})
	var warn bytes.Buffer
	got, err := countWithWarn(root, &warn)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if got != 2 {
		t.Errorf("Count = %d, want 2 (bad.md with type=project excluded)", got)
	}
}

func TestCountMissingDirectoryIsZero(t *testing.T) {
	root := t.TempDir()
	n, err := Count(root)
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n != 0 {
		t.Errorf("Count = %d, want 0", n)
	}
}

func TestListBodyNotLeakedIntoMetadata(t *testing.T) {
	// Frontmatter-only outputs must never smuggle body content into
	// the metadata fields — that would inflate token counts exactly
	// where the design demands parsimony.
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/a.md": "---\nname: A\ndescription: desc\ntype: user\n---\n\nThis body should never appear in list output.\n",
	})
	got, err := List(root, "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry")
	}
	if strings.Contains(got[0].Name, "body") ||
		strings.Contains(got[0].Description, "body") ||
		strings.Contains(got[0].Type, "body") {
		t.Errorf("body content leaked into metadata: %+v", got[0])
	}
}

func TestParseLearningToleratesQuotedValues(t *testing.T) {
	root := writeVault(t, map[string]string{
		"Knowledge/learnings/q.md": "---\nname: \"Quoted Name\"\ndescription: 'single quotes'\ntype: user\n---\n\nbody\n",
	})
	got, err := Get(root, "q")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Quoted Name" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Description != "single quotes" {
		t.Errorf("description = %q", got.Description)
	}
}

// validLearning returns a minimal well-formed learning file body.
func validLearning(name, desc, typ string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\ntype: " + typ + "\n---\n\nbody\n"
}
