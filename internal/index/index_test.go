package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Backwards compat & enriched roundtrip ---

func TestIndexBackwardsCompat(t *testing.T) {
	// Old-format JSON without enriched fields should load cleanly
	oldJSON := `{
		"sess-001": {
			"session_id": "sess-001",
			"note_path": "Sessions/myproject/2026-02-20-01.md",
			"project": "myproject",
			"domain": "personal",
			"date": "2026-02-20",
			"iteration": 1,
			"title": "Old session",
			"model": "claude-sonnet",
			"duration_minutes": 30,
			"created_at": "2026-02-20T10:00:00Z"
		}
	}`

	dir := t.TempDir()
	path := filepath.Join(dir, "session-index.json")
	if err := os.WriteFile(path, []byte(oldJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	e := idx.Entries["sess-001"]
	if e.SessionID != "sess-001" {
		t.Errorf("SessionID = %q, want %q", e.SessionID, "sess-001")
	}
	if e.Title != "Old session" {
		t.Errorf("Title = %q, want %q", e.Title, "Old session")
	}
	// Enriched fields should be zero values
	if e.Summary != "" {
		t.Errorf("Summary = %q, want empty", e.Summary)
	}
	if len(e.Decisions) != 0 {
		t.Errorf("Decisions len = %d, want 0", len(e.Decisions))
	}
	if len(e.FilesChanged) != 0 {
		t.Errorf("FilesChanged len = %d, want 0", len(e.FilesChanged))
	}
	if e.Branch != "" {
		t.Errorf("Branch = %q, want empty", e.Branch)
	}
}

func TestIndexEnrichedRoundTrip(t *testing.T) {
	dir := t.TempDir()
	idx, _ := Load(dir)

	entry := SessionEntry{
		SessionID:    "sess-enriched",
		NotePath:     "Sessions/proj/2026-02-25-01.md",
		Project:      "proj",
		Domain:       "personal",
		Date:         "2026-02-25",
		Iteration:    1,
		Title:        "Enriched session",
		CreatedAt:    time.Date(2026, 2, 25, 10, 0, 0, 0, time.UTC),
		Summary:      "Implemented cross-session linking",
		Decisions:    []string{"Use heuristic scoring", "Cap at 3 results"},
		OpenThreads:  []string{"Add thread resolution", "Support custom weights"},
		Tag:          "implementation",
		FilesChanged: []string{"internal/index/related.go", "internal/index/context.go"},
		Branch:       "feature/phase3",
	}

	idx.Add(entry)
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload
	idx2, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	e := idx2.Entries["sess-enriched"]
	if e.Summary != "Implemented cross-session linking" {
		t.Errorf("Summary = %q", e.Summary)
	}
	if len(e.Decisions) != 2 {
		t.Errorf("Decisions len = %d, want 2", len(e.Decisions))
	}
	if len(e.OpenThreads) != 2 {
		t.Errorf("OpenThreads len = %d, want 2", len(e.OpenThreads))
	}
	if e.Tag != "implementation" {
		t.Errorf("Tag = %q", e.Tag)
	}
	if len(e.FilesChanged) != 2 {
		t.Errorf("FilesChanged len = %d, want 2", len(e.FilesChanged))
	}
	if e.Branch != "feature/phase3" {
		t.Errorf("Branch = %q", e.Branch)
	}
}

func TestIndexTranscriptPathRoundTrip(t *testing.T) {
	dir := t.TempDir()
	idx, _ := Load(dir)

	entry := SessionEntry{
		SessionID:      "sess-tp",
		NotePath:       "Sessions/proj/2026-02-27-01.md",
		Project:        "proj",
		Date:           "2026-02-27",
		Iteration:      1,
		TranscriptPath: "/home/user/.claude/projects/proj/abc-def.jsonl",
	}

	idx.Add(entry)
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	idx2, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	e := idx2.Entries["sess-tp"]
	if e.TranscriptPath != "/home/user/.claude/projects/proj/abc-def.jsonl" {
		t.Errorf("TranscriptPath = %q, want original path", e.TranscriptPath)
	}
}

func TestIndexCheckpointRoundTrip(t *testing.T) {
	dir := t.TempDir()
	idx, _ := Load(dir)

	entry := SessionEntry{
		SessionID:  "sess-ckpt",
		NotePath:   "Sessions/proj/2026-02-27-01.md",
		Project:    "proj",
		Date:       "2026-02-27",
		Iteration:  1,
		Checkpoint: true,
	}

	idx.Add(entry)
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	idx2, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	e := idx2.Entries["sess-ckpt"]
	if !e.Checkpoint {
		t.Error("Checkpoint should be true after round-trip")
	}
}

func TestIndexToolCountsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	idx, _ := Load(dir)

	entry := SessionEntry{
		SessionID: "sess-tools",
		NotePath:  "Sessions/proj/2026-02-27-01.md",
		Project:   "proj",
		Date:      "2026-02-27",
		Iteration: 1,
		ToolCounts: map[string]int{
			"Bash":  12,
			"Read":  15,
			"Write": 8,
		},
		ToolUses: 35,
	}

	idx.Add(entry)
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	idx2, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	e := idx2.Entries["sess-tools"]
	if e.ToolUses != 35 {
		t.Errorf("ToolUses = %d, want 35", e.ToolUses)
	}
	if len(e.ToolCounts) != 3 {
		t.Errorf("ToolCounts len = %d, want 3", len(e.ToolCounts))
	}
	if e.ToolCounts["Bash"] != 12 {
		t.Errorf("ToolCounts[Bash] = %d, want 12", e.ToolCounts["Bash"])
	}
}

// --- Rebuild tests ---

func writeNote(t *testing.T, dir, project, filename, content string) {
	t.Helper()
	projDir := filepath.Join(dir, project)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, filename), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const goodNote = `---
date: 2026-02-25
type: session
project: myproject
domain: personal
session_id: "rebuild-001"
iteration: 1
branch: feature/rebuild
tags: [cortana-session, implementation]
summary: "Built rebuild command"
duration_minutes: 30
---

# Built rebuild command

## What Changed

- ` + "`internal/index/rebuild.go`" + `

## Key Decisions

- Walk the sessions directory

## Open Threads

- [ ] Handle nested dirs
`

func TestRebuild(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "Sessions")
	stateDir := t.TempDir()

	writeNote(t, sessionsDir, "myproject", "2026-02-25-01.md", goodNote)

	idx, count, err := Rebuild(sessionsDir, stateDir)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	e, ok := idx.Entries["rebuild-001"]
	if !ok {
		t.Fatal("entry not found")
	}
	if e.Project != "myproject" {
		t.Errorf("Project = %q", e.Project)
	}
	if e.Summary != "Built rebuild command" {
		t.Errorf("Summary = %q", e.Summary)
	}
	if e.Branch != "feature/rebuild" {
		t.Errorf("Branch = %q", e.Branch)
	}
	if e.Tag != "implementation" {
		t.Errorf("Tag = %q", e.Tag)
	}
	if len(e.Decisions) != 1 {
		t.Errorf("Decisions len = %d, want 1", len(e.Decisions))
	}
	if len(e.OpenThreads) != 1 {
		t.Errorf("OpenThreads len = %d, want 1", len(e.OpenThreads))
	}
	if len(e.FilesChanged) != 1 {
		t.Errorf("FilesChanged len = %d, want 1", len(e.FilesChanged))
	}
}

func TestRebuildSkipsMalformed(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "Sessions")
	stateDir := t.TempDir()

	// Note without session_id
	noID := `---
date: 2026-02-25
type: session
project: myproject
---

# No session ID
`
	writeNote(t, sessionsDir, "myproject", "2026-02-25-01.md", noID)

	_, count, err := Rebuild(sessionsDir, stateDir)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (malformed should be skipped)", count)
	}
}

func TestRebuildSkipsUnderscoreFiles(t *testing.T) {
	sessionsDir := filepath.Join(t.TempDir(), "Sessions")
	stateDir := t.TempDir()

	writeNote(t, sessionsDir, "myproject", "_context.md", goodNote)

	_, count, err := Rebuild(sessionsDir, stateDir)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (underscore files should be skipped)", count)
	}
}

// --- Related sessions tests ---

func TestRelatedSharedFiles(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID:    "s1",
		Project:      "proj",
		FilesChanged: []string{"a.go", "b.go", "c.go"},
	}

	candidate := SessionEntry{
		SessionID:    "s2",
		Project:      "proj",
		FilesChanged: []string{"b.go", "c.go", "d.go"},
	}

	results := idx.RelatedSessions(candidate, "")
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	// 2 shared files * 3 = 6 >= min 5
	if results[0].Score != 6 {
		t.Errorf("Score = %d, want 6", results[0].Score)
	}
}

func TestRelatedThreadResolution(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	// s1 has open thread about "authentication system"
	idx.Entries["s1"] = SessionEntry{
		SessionID:   "s1",
		Project:     "proj",
		OpenThreads: []string{"implement authentication system validation"},
	}

	// Candidate has a decision resolving it
	candidate := SessionEntry{
		SessionID: "s2",
		Project:   "proj",
		Decisions: []string{"built authentication system with JWT tokens"},
	}

	results := idx.RelatedSessions(candidate, "")
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	// Thread match = 10
	if results[0].Score < 10 {
		t.Errorf("Score = %d, want >= 10", results[0].Score)
	}
}

func TestRelatedSameBranch(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID: "s1",
		Project:   "proj",
		Branch:    "feature/auth",
	}

	candidate := SessionEntry{
		SessionID: "s2",
		Project:   "proj",
		Branch:    "feature/auth",
	}

	results := idx.RelatedSessions(candidate, "")
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	if results[0].Score != 5 {
		t.Errorf("Score = %d, want 5", results[0].Score)
	}
}

func TestRelatedSameBranchMainExcluded(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID: "s1",
		Project:   "proj",
		Branch:    "main",
	}

	candidate := SessionEntry{
		SessionID: "s2",
		Project:   "proj",
		Branch:    "main",
	}

	results := idx.RelatedSessions(candidate, "")
	if len(results) != 0 {
		t.Errorf("len = %d, want 0 (main branch excluded)", len(results))
	}
}

func TestRelatedSameTag(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID:    "s1",
		Project:      "proj",
		Tag:          "debugging",
		Branch:       "feature/fix",
		FilesChanged: []string{"x.go"},
	}

	candidate := SessionEntry{
		SessionID:    "s2",
		Project:      "proj",
		Tag:          "debugging",
		Branch:       "feature/fix",
		FilesChanged: []string{"x.go"},
	}

	results := idx.RelatedSessions(candidate, "")
	if len(results) != 1 {
		t.Fatalf("len = %d, want 1", len(results))
	}
	// branch(5) + tag(2) + 1 file(3) = 10
	if results[0].Score != 10 {
		t.Errorf("Score = %d, want 10", results[0].Score)
	}
}

func TestRelatedMinScoreFilter(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID: "s1",
		Project:   "proj",
		Tag:       "implementation",
	}

	// Only tag match = 2, below minimum of 5
	candidate := SessionEntry{
		SessionID: "s2",
		Project:   "proj",
		Tag:       "implementation",
	}

	results := idx.RelatedSessions(candidate, "")
	if len(results) != 0 {
		t.Errorf("len = %d, want 0 (below min score)", len(results))
	}
}

func TestRelatedPreviousExclusion(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID: "s1",
		NotePath:  "Sessions/proj/2026-02-24-01.md",
		Project:   "proj",
		Branch:    "feature/x",
	}

	candidate := SessionEntry{
		SessionID: "s2",
		Project:   "proj",
		Branch:    "feature/x",
	}

	results := idx.RelatedSessions(candidate, "Sessions/proj/2026-02-24-01.md")
	if len(results) != 0 {
		t.Errorf("len = %d, want 0 (previous excluded)", len(results))
	}
}

func TestRelatedMaxResults(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	// Add 5 entries all sharing files with candidate
	for i := 0; i < 5; i++ {
		id := string(rune('a'+i)) + "-session"
		idx.Entries[id] = SessionEntry{
			SessionID:    id,
			Project:      "proj",
			FilesChanged: []string{"shared1.go", "shared2.go"},
			Date:         "2026-02-20",
		}
	}

	candidate := SessionEntry{
		SessionID:    "candidate",
		Project:      "proj",
		FilesChanged: []string{"shared1.go", "shared2.go"},
	}

	results := idx.RelatedSessions(candidate, "")
	if len(results) != 3 {
		t.Errorf("len = %d, want 3 (max cap)", len(results))
	}
}

func TestSignificantWords(t *testing.T) {
	words := significantWords("This is the authentication system for users")
	// "this" = stop word, "the" < 4 chars, "is" < 4 chars, "for" < 4 chars
	// Should get: "authentication", "system", "users"
	if len(words) != 3 {
		t.Errorf("len = %d, want 3, got %v", len(words), words)
	}

	found := make(map[string]bool)
	for _, w := range words {
		found[w] = true
	}
	for _, want := range []string{"authentication", "system", "users"} {
		if !found[want] {
			t.Errorf("missing word %q", want)
		}
	}
}

// --- Context document tests ---

func TestProjectContextTimeline(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID: "s1", Project: "proj", Date: "2026-02-20",
		Iteration: 1, NotePath: "Sessions/proj/2026-02-20-01.md",
		Summary: "First session", Tag: "planning",
	}
	idx.Entries["s2"] = SessionEntry{
		SessionID: "s2", Project: "proj", Date: "2026-02-21",
		Iteration: 1, NotePath: "Sessions/proj/2026-02-21-01.md",
		Summary: "Second session", Tag: "implementation",
	}

	doc := idx.ProjectContext("proj")

	if !contains(doc, "[[2026-02-20-01]]") {
		t.Error("missing first session wikilink")
	}
	if !contains(doc, "[[2026-02-21-01]]") {
		t.Error("missing second session wikilink")
	}
	if !contains(doc, "#planning") {
		t.Error("missing planning tag")
	}
	if !contains(doc, "sessions: 2") {
		t.Error("missing session count")
	}
}

func TestProjectContextDecisionDedup(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID: "s1", Project: "proj", Date: "2026-02-20",
		Iteration: 1, NotePath: "Sessions/proj/2026-02-20-01.md",
		Decisions: []string{"Use JWT auth", "Use PostgreSQL"},
	}
	idx.Entries["s2"] = SessionEntry{
		SessionID: "s2", Project: "proj", Date: "2026-02-21",
		Iteration: 1, NotePath: "Sessions/proj/2026-02-21-01.md",
		Decisions: []string{"Use JWT auth", "Add rate limiting"}, // "Use JWT auth" is duplicate
	}

	doc := idx.ProjectContext("proj")

	// Count occurrences of "Use JWT auth" in decisions section
	count := countOccurrences(doc, "Use JWT auth")
	if count != 1 {
		t.Errorf("'Use JWT auth' appears %d times, want 1 (deduped)", count)
	}
}

func TestProjectContextThreadResolution(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID: "s1", Project: "proj", Date: "2026-02-20",
		Iteration: 1, NotePath: "Sessions/proj/2026-02-20-01.md",
		OpenThreads: []string{"implement authentication system"},
	}
	idx.Entries["s2"] = SessionEntry{
		SessionID: "s2", Project: "proj", Date: "2026-02-21",
		Iteration: 1, NotePath: "Sessions/proj/2026-02-21-01.md",
		Decisions: []string{"completed authentication system with JWT"},
	}

	doc := idx.ProjectContext("proj")

	// "implement authentication system" should be filtered out
	// because "completed authentication system with JWT" resolves it
	if contains(doc, "- [ ] implement authentication system") {
		t.Error("resolved thread should not appear in Open Threads")
	}
}

func TestProjectContextKeyFiles(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	for i := 0; i < 4; i++ {
		id := string(rune('a'+i)) + "-session"
		files := []string{"main.go"}
		if i < 2 {
			files = append(files, "rare.go")
		}
		idx.Entries[id] = SessionEntry{
			SessionID: id, Project: "proj", Date: "2026-02-20",
			Iteration: i + 1, NotePath: "Sessions/proj/note.md",
			FilesChanged: files,
		}
	}

	doc := idx.ProjectContext("proj")

	if !contains(doc, "`main.go` (4 sessions)") {
		t.Error("main.go should appear as key file with 4 sessions")
	}
	if contains(doc, "`rare.go`") {
		t.Error("rare.go should not appear (only 2 sessions, threshold is 3)")
	}
}

func TestProjects(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{SessionID: "s1", Project: "beta"}
	idx.Entries["s2"] = SessionEntry{SessionID: "s2", Project: "alpha"}
	idx.Entries["s3"] = SessionEntry{SessionID: "s3", Project: "beta"}

	projects := idx.Projects()
	if len(projects) != 2 {
		t.Fatalf("len = %d, want 2", len(projects))
	}
	if projects[0] != "alpha" || projects[1] != "beta" {
		t.Errorf("projects = %v, want [alpha beta]", projects)
	}
}

// --- Renderer tests ---

func TestIndexSaveLoad(t *testing.T) {
	dir := t.TempDir()
	idx, _ := Load(dir)

	idx.Add(SessionEntry{
		SessionID: "test-1",
		NotePath:  "Sessions/proj/2026-02-25-01.md",
		Project:   "proj",
		Date:      "2026-02-25",
		Iteration: 1,
	})

	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify JSON is valid
	data, err := os.ReadFile(filepath.Join(dir, "session-index.json"))
	if err != nil {
		t.Fatal(err)
	}

	var entries map[string]SessionEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if entries["test-1"].SessionID != "test-1" {
		t.Errorf("SessionID = %q", entries["test-1"].SessionID)
	}
}

// helpers

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func countOccurrences(s, substr string) int {
	count := 0
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			count++
			i += len(substr) - 1
		}
	}
	return count
}
