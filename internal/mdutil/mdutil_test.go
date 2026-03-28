package mdutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSignificantWords_Basic(t *testing.T) {
	got := SignificantWords("Implemented session capture with retry logic")
	want := []string{"implemented", "session", "capture", "retry", "logic"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("word %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSignificantWords_StopWords(t *testing.T) {
	got := SignificantWords("this will have been done before")
	if len(got) != 0 {
		t.Errorf("expected no significant words, got %v", got)
	}
}

func TestSignificantWords_PunctuationTrimming(t *testing.T) {
	got := SignificantWords(`"hello," (world!) [testing]`)
	want := []string{"hello", "world", "testing"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("word %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSignificantWords_ShortWordsFiltered(t *testing.T) {
	got := SignificantWords("I am a go dev")
	if len(got) != 0 {
		t.Errorf("expected no words (all < 4 chars), got %v", got)
	}
}

func TestIsStopWord(t *testing.T) {
	if !IsStopWord("this") {
		t.Error("expected 'this' to be a stop word")
	}
	if IsStopWord("session") {
		t.Error("expected 'session' to not be a stop word")
	}
}

func TestOverlap_Matching(t *testing.T) {
	a := []string{"session", "capture", "retry"}
	b := []string{"capture", "retry", "logic"}
	got := Overlap(a, b)
	if got != 2 {
		t.Errorf("got %d, want 2", got)
	}
}

func TestOverlap_DuplicatesInB(t *testing.T) {
	a := []string{"session", "capture"}
	b := []string{"capture", "capture", "capture"}
	got := Overlap(a, b)
	if got != 1 {
		t.Errorf("got %d, want 1 (dedup semantics)", got)
	}
}

func TestOverlap_NoMatch(t *testing.T) {
	a := []string{"session", "capture"}
	b := []string{"retry", "logic"}
	got := Overlap(a, b)
	if got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

func TestOverlap_Empty(t *testing.T) {
	if got := Overlap(nil, []string{"a"}); got != 0 {
		t.Errorf("nil a: got %d, want 0", got)
	}
	if got := Overlap([]string{"a"}, nil); got != 0 {
		t.Errorf("nil b: got %d, want 0", got)
	}
}

func TestSetIntersection_Basic(t *testing.T) {
	got := SetIntersection(
		[]string{"session", "capture", "retry"},
		[]string{"capture", "retry", "logic"},
	)
	want := []string{"capture", "retry"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("elem %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSetIntersection_NoDuplicates(t *testing.T) {
	got := SetIntersection(
		[]string{"a", "b"},
		[]string{"a", "a", "b"},
	)
	if len(got) != 2 {
		t.Errorf("got %v, want 2 elements (deduped)", got)
	}
}

func TestSetIntersection_Empty(t *testing.T) {
	got := SetIntersection(nil, []string{"a"})
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestReplaceSectionBody_Basic(t *testing.T) {
	doc := "# Title\n\n## Foo\n\nold content\n\n## Bar\n\nbar content"
	got, err := ReplaceSectionBody(doc, "Foo", "new content")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "new content") {
		t.Error("missing new content")
	}
	if strings.Contains(got, "old content") {
		t.Error("old content should be replaced")
	}
	if !strings.Contains(got, "bar content") {
		t.Error("other sections should be preserved")
	}
}

func TestReplaceSectionBody_NotFound(t *testing.T) {
	doc := "# Title\n\n## Foo\n\ncontent"
	_, err := ReplaceSectionBody(doc, "Missing", "new")
	if err == nil {
		t.Fatal("expected error for missing section")
	}
}

func TestReplaceSectionBody_LastSection(t *testing.T) {
	doc := "# Title\n\n## Only\n\nold stuff"
	got, err := ReplaceSectionBody(doc, "Only", "replaced")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "replaced") {
		t.Error("missing replacement")
	}
	if strings.Contains(got, "old stuff") {
		t.Error("old content should be gone")
	}
}

func TestReplaceSectionBody_PreservesOtherSections(t *testing.T) {
	doc := "## A\n\na content\n\n## B\n\nb content\n\n## C\n\nc content"
	got, err := ReplaceSectionBody(doc, "B", "new b")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "a content") {
		t.Error("section A should be preserved")
	}
	if !strings.Contains(got, "new b") {
		t.Error("section B should have new content")
	}
	if !strings.Contains(got, "c content") {
		t.Error("section C should be preserved")
	}
}

func TestAtomicWriteFile_CreatesDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "dir", "file.md")
	err := AtomicWriteFile(path, []byte("hello"), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("got %q, want %q", data, "hello")
	}
}

func TestAtomicWriteFile_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.md")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Errorf("got %q, want %q", data, "new")
	}
}
