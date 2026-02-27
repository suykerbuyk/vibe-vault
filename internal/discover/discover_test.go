package discover

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTranscript(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"type":"test"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverFindsTranscripts(t *testing.T) {
	base := t.TempDir()

	id1 := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	id2 := "11111111-2222-3333-4444-555555555555"

	writeTranscript(t, filepath.Join(base, "proj-a", id1+".jsonl"), time.Now().Add(-time.Hour))
	writeTranscript(t, filepath.Join(base, "proj-b", id2+".jsonl"), time.Now())

	results, err := Discover(base)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}

	// Oldest first
	if results[0].SessionID != id1 {
		t.Errorf("first = %q, want %q (oldest first)", results[0].SessionID, id1)
	}
	if results[1].SessionID != id2 {
		t.Errorf("second = %q, want %q", results[1].SessionID, id2)
	}
}

func TestDiscoverSubagentDetection(t *testing.T) {
	base := t.TempDir()

	mainID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	subID := "11111111-2222-3333-4444-555555555555"

	writeTranscript(t, filepath.Join(base, "proj", mainID+".jsonl"), time.Now())
	writeTranscript(t, filepath.Join(base, "proj", "subagents", subID+".jsonl"), time.Now())

	results, err := Discover(base)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("len = %d, want 2", len(results))
	}

	byID := make(map[string]TranscriptFile)
	for _, r := range results {
		byID[r.SessionID] = r
	}

	if byID[mainID].IsSubagent {
		t.Error("main transcript should not be subagent")
	}
	if !byID[subID].IsSubagent {
		t.Error("subagent transcript should be detected as subagent")
	}
}

func TestDiscoverUUIDFiltering(t *testing.T) {
	base := t.TempDir()

	validID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	writeTranscript(t, filepath.Join(base, "proj", validID+".jsonl"), time.Now())
	writeTranscript(t, filepath.Join(base, "proj", "not-a-uuid.jsonl"), time.Now())
	writeTranscript(t, filepath.Join(base, "proj", "settings.json"), time.Now())

	results, err := Discover(base)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("len = %d, want 1 (only UUID filenames)", len(results))
	}
	if results[0].SessionID != validID {
		t.Errorf("SessionID = %q, want %q", results[0].SessionID, validID)
	}
}

func TestFindBySessionID(t *testing.T) {
	base := t.TempDir()

	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	expected := filepath.Join(base, "proj-a", id+".jsonl")
	writeTranscript(t, expected, time.Now())

	path, err := FindBySessionID(base, id)
	if err != nil {
		t.Fatalf("FindBySessionID: %v", err)
	}
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}

	// Not found
	_, err = FindBySessionID(base, "00000000-0000-0000-0000-000000000000")
	if !os.IsNotExist(err) {
		t.Errorf("expected not-exist error, got: %v", err)
	}
}

func TestFindBySessionIDSubagent(t *testing.T) {
	base := t.TempDir()

	id := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	expected := filepath.Join(base, "proj", "subagents", id+".jsonl")
	writeTranscript(t, expected, time.Now())

	path, err := FindBySessionID(base, id)
	if err != nil {
		t.Fatalf("FindBySessionID: %v", err)
	}
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}
}
