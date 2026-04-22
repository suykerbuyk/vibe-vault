package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/mdutil"
)

// --- Backwards compat & enriched roundtrip ---

func TestIndexBackwardsCompat(t *testing.T) {
	// Old-format JSON without enriched fields should load cleanly
	oldJSON := `{
		"sess-001": {
			"session_id": "sess-001",
			"note_path": "Projects/myproject/sessions/2026-02-20-01.md",
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
		NotePath:     "Projects/proj/sessions/2026-02-25-01.md",
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
		NotePath:       "Projects/proj/sessions/2026-02-27-01.md",
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
		NotePath:   "Projects/proj/sessions/2026-02-27-01.md",
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
		NotePath:  "Projects/proj/sessions/2026-02-27-01.md",
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

func TestIndexTokensRoundTrip(t *testing.T) {
	dir := t.TempDir()
	idx, _ := Load(dir)

	entry := SessionEntry{
		SessionID: "sess-tokens",
		NotePath:  "Projects/proj/sessions/2026-02-27-01.md",
		Project:   "proj",
		Date:      "2026-02-27",
		Iteration: 1,
		TokensIn:  12345,
		TokensOut: 6789,
		Messages:  42,
	}

	idx.Add(entry)
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	idx2, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	e := idx2.Entries["sess-tokens"]
	if e.TokensIn != 12345 {
		t.Errorf("TokensIn = %d, want 12345", e.TokensIn)
	}
	if e.TokensOut != 6789 {
		t.Errorf("TokensOut = %d, want 6789", e.TokensOut)
	}
	if e.Messages != 42 {
		t.Errorf("Messages = %d, want 42", e.Messages)
	}
}

func TestIndexCommitsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	idx, _ := Load(dir)

	entry := SessionEntry{
		SessionID: "sess-commits",
		NotePath:  "Projects/proj/sessions/2026-02-28-01.md",
		Project:   "proj",
		Date:      "2026-02-28",
		Iteration: 1,
		Commits:   []string{"abc1234", "def5678"},
	}

	idx.Add(entry)
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	idx2, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	e := idx2.Entries["sess-commits"]
	if len(e.Commits) != 2 {
		t.Fatalf("Commits len = %d, want 2", len(e.Commits))
	}
	if e.Commits[0] != "abc1234" {
		t.Errorf("Commits[0] = %q, want abc1234", e.Commits[0])
	}
	if e.Commits[1] != "def5678" {
		t.Errorf("Commits[1] = %q, want def5678", e.Commits[1])
	}
}

func TestIndexFrictionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	idx, _ := Load(dir)

	entry := SessionEntry{
		SessionID:     "sess-friction",
		NotePath:      "Projects/proj/sessions/2026-02-28-01.md",
		Project:       "proj",
		Date:          "2026-02-28",
		Iteration:     1,
		Corrections:   3,
		FrictionScore: 42,
	}

	idx.Add(entry)
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	idx2, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	e := idx2.Entries["sess-friction"]
	if e.Corrections != 3 {
		t.Errorf("Corrections = %d, want 3", e.Corrections)
	}
	if e.FrictionScore != 42 {
		t.Errorf("FrictionScore = %d, want 42", e.FrictionScore)
	}
}

func TestIndexParentUUIDRoundTrip(t *testing.T) {
	dir := t.TempDir()
	idx, _ := Load(dir)

	entry := SessionEntry{
		SessionID:  "sess-continued",
		NotePath:   "Projects/proj/sessions/2026-03-01-01.md",
		Project:    "proj",
		Date:       "2026-03-01",
		Iteration:  1,
		ParentUUID: "external-prev-uuid-abc",
	}

	idx.Add(entry)
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	idx2, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	e := idx2.Entries["sess-continued"]
	if e.ParentUUID != "external-prev-uuid-abc" {
		t.Errorf("ParentUUID = %q, want %q", e.ParentUUID, "external-prev-uuid-abc")
	}
}

func TestIndexParentUUIDOmitEmpty(t *testing.T) {
	dir := t.TempDir()
	idx, _ := Load(dir)

	entry := SessionEntry{
		SessionID: "sess-no-parent",
		NotePath:  "Projects/proj/sessions/2026-03-01-01.md",
		Project:   "proj",
		Date:      "2026-03-01",
		Iteration: 1,
	}

	idx.Add(entry)
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Read raw JSON and verify parent_uuid is not present
	data, err := os.ReadFile(filepath.Join(dir, "session-index.json"))
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	if _, exists := raw["sess-no-parent"]["parent_uuid"]; exists {
		t.Error("parent_uuid should be omitted when empty")
	}
}

// --- Rebuild tests ---

func writeNote(t *testing.T, dir, project, filename, content string) {
	t.Helper()
	sessDir := filepath.Join(dir, project, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, filename), []byte(content), 0o644); err != nil {
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
tags: [vv-session, implementation]
summary: "Built rebuild command"
duration_minutes: 30
tokens_in: 5000
tokens_out: 2000
messages: 15
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
	projectsDir := filepath.Join(t.TempDir(), "Projects")
	stateDir := t.TempDir()

	writeNote(t, projectsDir, "myproject", "2026-02-25-01.md", goodNote)

	idx, count, err := Rebuild(projectsDir, stateDir)
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
	if e.TokensIn != 5000 {
		t.Errorf("TokensIn = %d, want 5000", e.TokensIn)
	}
	if e.TokensOut != 2000 {
		t.Errorf("TokensOut = %d, want 2000", e.TokensOut)
	}
	if e.Messages != 15 {
		t.Errorf("Messages = %d, want 15", e.Messages)
	}
}

func TestRebuildSkipsMalformed(t *testing.T) {
	projectsDir := filepath.Join(t.TempDir(), "Projects")
	stateDir := t.TempDir()

	// Note without session_id
	noID := `---
date: 2026-02-25
type: session
project: myproject
---

# No session ID
`
	writeNote(t, projectsDir, "myproject", "2026-02-25-01.md", noID)

	_, count, err := Rebuild(projectsDir, stateDir)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (malformed should be skipped)", count)
	}
}

func TestRebuildSkipsNonSessionFiles(t *testing.T) {
	projectsDir := filepath.Join(t.TempDir(), "Projects")
	stateDir := t.TempDir()

	// Write a file at the project root (not inside sessions/) — should be skipped
	projDir := filepath.Join(projectsDir, "myproject")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "history.md"), []byte(goodNote), 0o644); err != nil {
		t.Fatal(err)
	}

	_, count, err := Rebuild(projectsDir, stateDir)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 (non-session files should be skipped)", count)
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
		NotePath:  "Projects/proj/sessions/2026-02-24-01.md",
		Project:   "proj",
		Branch:    "feature/x",
	}

	candidate := SessionEntry{
		SessionID: "s2",
		Project:   "proj",
		Branch:    "feature/x",
	}

	results := idx.RelatedSessions(candidate, "Projects/proj/sessions/2026-02-24-01.md")
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
	words := mdutil.SignificantWords("This is the authentication system for users")
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

// recentOpts returns ContextOptions with Now set close to the test dates,
// ensuring all sessions fall within the "recent" (full detail) window.
func recentOpts(alertThreshold int) ContextOptions {
	return ContextOptions{
		AlertThreshold: alertThreshold,
		Now:            time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC),
	}
}

func TestProjectContextTimeline(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID: "s1", Project: "proj", Date: "2026-02-20",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-02-20-01.md",
		Summary: "First session", Tag: "planning",
	}
	idx.Entries["s2"] = SessionEntry{
		SessionID: "s2", Project: "proj", Date: "2026-02-21",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-02-21-01.md",
		Summary: "Second session", Tag: "implementation",
	}

	doc := idx.ProjectContext("proj", recentOpts(0))

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

func TestProjectContextContinuedSession(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID: "s1", Project: "proj", Date: "2026-02-20",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-02-20-01.md",
		Summary: "Initial session",
	}
	idx.Entries["s2"] = SessionEntry{
		SessionID: "s2", Project: "proj", Date: "2026-02-20",
		Iteration: 2, NotePath: "Projects/proj/sessions/2026-02-20-02.md",
		Summary: "Continued work", ParentUUID: "external-uuid-xyz",
	}

	doc := idx.ProjectContext("proj", recentOpts(0))

	if !contains(doc, "[[2026-02-20-02]] ↩continued") {
		t.Error("continued session should have ↩continued marker in timeline")
	}
	if contains(doc, "[[2026-02-20-01]] ↩continued") {
		t.Error("non-continued session should NOT have ↩continued marker")
	}
}

func TestProjectContextDecisionDedup(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID: "s1", Project: "proj", Date: "2026-02-20",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-02-20-01.md",
		Decisions: []string{"Use JWT auth", "Use PostgreSQL"},
	}
	idx.Entries["s2"] = SessionEntry{
		SessionID: "s2", Project: "proj", Date: "2026-02-21",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-02-21-01.md",
		Decisions: []string{"Use JWT auth", "Add rate limiting"}, // "Use JWT auth" is duplicate
	}

	doc := idx.ProjectContext("proj", recentOpts(0))

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
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-02-20-01.md",
		OpenThreads: []string{"implement authentication system"},
	}
	idx.Entries["s2"] = SessionEntry{
		SessionID: "s2", Project: "proj", Date: "2026-02-21",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-02-21-01.md",
		Decisions: []string{"completed authentication system with JWT"},
	}

	doc := idx.ProjectContext("proj", recentOpts(0))

	// "implement authentication system" should be filtered out
	// because "completed authentication system with JWT" resolves it
	if contains(doc, "- [ ] implement authentication system") {
		t.Error("resolved thread should not appear in Open Threads")
	}
}

func TestProjectContextKeyFiles(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	// Use recent dates so recency weighting applies (within 14 days of Now)
	now := time.Date(2026, 2, 25, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		id := string(rune('a'+i)) + "-session"
		files := []string{"main.go"}
		if i < 2 {
			files = append(files, "rare.go")
		}
		idx.Entries[id] = SessionEntry{
			SessionID: id, Project: "proj", Date: "2026-02-20",
			Iteration: i + 1, NotePath: "Projects/proj/sessions/note.md",
			FilesChanged: files,
		}
	}

	doc := idx.ProjectContext("proj", ContextOptions{Now: now})

	// With default boost=3 and 4 recent sessions: main.go = 4*3 = 12 (>= 5, shown)
	if !contains(doc, "`main.go` (4 sessions)") {
		t.Error("main.go should appear as key file with 4 sessions")
	}
	// rare.go = 2*3 = 6 (>= 5, now shown with recency weighting)
	// The old test expected rare.go to not appear (old threshold was 3 raw sessions).
	// With recency weighting, 2 recent sessions * 3 = score 6 >= threshold 5, so it shows.
	if !contains(doc, "`rare.go`") {
		t.Error("rare.go should appear (2 recent sessions * boost 3 = score 6 >= threshold 5)")
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
		NotePath:  "Projects/proj/sessions/2026-02-25-01.md",
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

func TestProjectContextFrictionAlert(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	// 5 sessions with high friction scores (avg 50)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("s-alert-%d", i)
		idx.Entries[id] = SessionEntry{
			SessionID: id, Project: "proj",
			Date:      fmt.Sprintf("2026-02-%02d", 20+i),
			Iteration: 1, NotePath: fmt.Sprintf("Projects/proj/sessions/2026-02-%02d-01.md", 20+i),
			FrictionScore: 50,
		}
	}

	doc := idx.ProjectContext("proj", recentOpts(40))

	if !contains(doc, "## ⚠ Friction Alert") {
		t.Error("expected friction alert section for avg friction 50 with threshold 40")
	}
	if !contains(doc, "threshold: 40") {
		t.Error("expected threshold value in alert section")
	}
}

func TestProjectContextFrictionAlertBelowThreshold(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	// 5 sessions with low friction scores (avg 20)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("s-low-%d", i)
		idx.Entries[id] = SessionEntry{
			SessionID: id, Project: "proj",
			Date:      fmt.Sprintf("2026-02-%02d", 20+i),
			Iteration: 1, NotePath: fmt.Sprintf("Projects/proj/sessions/2026-02-%02d-01.md", 20+i),
			FrictionScore: 20,
		}
	}

	doc := idx.ProjectContext("proj", recentOpts(40))

	if contains(doc, "Friction Alert") {
		t.Error("should not have friction alert for avg friction 20 with threshold 40")
	}
}

func TestProjectContextFrictionAlertDisabled(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("s-dis-%d", i)
		idx.Entries[id] = SessionEntry{
			SessionID: id, Project: "proj",
			Date:      fmt.Sprintf("2026-02-%02d", 20+i),
			Iteration: 1, NotePath: fmt.Sprintf("Projects/proj/sessions/2026-02-%02d-01.md", 20+i),
			FrictionScore: 50,
		}
	}

	doc := idx.ProjectContext("proj", recentOpts(0))

	if contains(doc, "Friction Alert") {
		t.Error("should not have friction alert when threshold is 0")
	}
}

// --- Tiered timeline tests ---

func TestTimelineTieredRendering(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}
	now := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	// Recent (within 7 days): full detail
	idx.Entries["s-recent"] = SessionEntry{
		SessionID: "s-recent", Project: "proj", Date: "2026-03-05",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-03-05-01.md",
		Summary: "Recent work", Tag: "implementation", FrictionScore: 35,
	}
	// Window (8-30 days): condensed
	idx.Entries["s-window"] = SessionEntry{
		SessionID: "s-window", Project: "proj", Date: "2026-02-15",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-02-15-01.md",
		Summary: "Window work", Tag: "debugging", FrictionScore: 45,
	}
	// Old (>30 days): omitted
	idx.Entries["s-old"] = SessionEntry{
		SessionID: "s-old", Project: "proj", Date: "2026-01-01",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-01-01-01.md",
		Summary: "Old work", Tag: "planning", FrictionScore: 50,
	}

	doc := idx.ProjectContext("proj", ContextOptions{Now: now})

	// Recent: should have full detail (summary, friction, tag)
	if !contains(doc, "— Recent work") {
		t.Error("recent session should have summary")
	}
	if !contains(doc, "⚡35") {
		t.Error("recent session should have friction indicator")
	}

	// Window: should have tag but NOT summary or friction
	if !contains(doc, "[[2026-02-15-01]]") {
		t.Error("window session should appear in timeline")
	}
	if !contains(doc, "[[2026-02-15-01]] #debugging") {
		t.Error("window session should have tag")
	}
	if contains(doc, "— Window work") {
		t.Error("window session should NOT have summary (condensed)")
	}
	if contains(doc, "⚡45") {
		t.Error("window session should NOT have friction indicator (condensed)")
	}

	// Old: should NOT appear in timeline
	if contains(doc, "[[2026-01-01-01]]") {
		t.Error("old session should be omitted from timeline")
	}

	// Session count in frontmatter should still reflect ALL sessions
	if !contains(doc, "sessions: 3") {
		t.Error("session count should include all sessions, not just visible ones")
	}
}

func TestTimelineCustomWindows(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}
	now := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	// Session 20 days ago — within custom recent window of 25 days
	idx.Entries["s1"] = SessionEntry{
		SessionID: "s1", Project: "proj", Date: "2026-02-16",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-02-16-01.md",
		Summary: "Should be full detail",
	}

	doc := idx.ProjectContext("proj", ContextOptions{
		Now:                now,
		TimelineRecentDays: 25,
		TimelineWindowDays: 60,
	})

	// With 25-day recent window, this session should have full detail
	if !contains(doc, "— Should be full detail") {
		t.Error("session within custom recent window should have summary")
	}
}

// --- Decision decay tests ---

func TestDecisionDecayStaleDropped(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// Decision from 120 days ago, never referenced again
	idx.Entries["s-old"] = SessionEntry{
		SessionID: "s-old", Project: "proj", Date: "2026-02-01",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-02-01-01.md",
		Decisions: []string{"Use SQLite for storage"},
	}
	// Recent session with unrelated decision
	idx.Entries["s-recent"] = SessionEntry{
		SessionID: "s-recent", Project: "proj", Date: "2026-05-30",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-05-30-01.md",
		Decisions: []string{"Add rate limiting"},
	}

	doc := idx.ProjectContext("proj", ContextOptions{Now: now})

	if contains(doc, "Use SQLite") {
		t.Error("stale decision (120 days, unreferenced) should be pruned")
	}
	if !contains(doc, "Add rate limiting") {
		t.Error("recent decision should be kept")
	}
}

func TestDecisionDecayReferencedKept(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// Old decision
	idx.Entries["s-old"] = SessionEntry{
		SessionID: "s-old", Project: "proj", Date: "2026-02-01",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-02-01-01.md",
		Decisions: []string{"Use stdlib HTTP client for providers"},
	}
	// Recent session that references the old decision
	idx.Entries["s-recent"] = SessionEntry{
		SessionID: "s-recent", Project: "proj", Date: "2026-05-30",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-05-30-01.md",
		Decisions: []string{"Keep stdlib HTTP client, no SDK dependency"},
	}

	doc := idx.ProjectContext("proj", ContextOptions{Now: now})

	if !contains(doc, "Use stdlib HTTP client for providers") {
		t.Error("decision referenced by recent session should survive decay")
	}
}

func TestDecisionPermanentMarker(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// Old decision with [permanent] marker, never referenced
	idx.Entries["s-old"] = SessionEntry{
		SessionID: "s-old", Project: "proj", Date: "2026-01-01",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-01-01-01.md",
		Decisions: []string{"Use stdlib HTTP, no SDK dependencies [permanent]"},
	}

	doc := idx.ProjectContext("proj", ContextOptions{Now: now})

	if !contains(doc, "[permanent]") {
		t.Error("decision with [permanent] marker should survive decay regardless of age")
	}
}

func TestDecisionCoreMarker(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	idx.Entries["s-old"] = SessionEntry{
		SessionID: "s-old", Project: "proj", Date: "2026-01-01",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-01-01-01.md",
		Decisions: []string{"Dual Apache/MIT license [core]"},
	}

	doc := idx.ProjectContext("proj", ContextOptions{Now: now})

	if !contains(doc, "[core]") {
		t.Error("decision with [core] marker should survive decay regardless of age")
	}
}

func TestDecisionCap15(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}
	now := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	// Add 20 unique decisions across recent sessions
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("s-%d", i)
		idx.Entries[id] = SessionEntry{
			SessionID: id, Project: "proj",
			Date:      fmt.Sprintf("2026-03-%02d", (i%7)+1),
			Iteration: i + 1,
			NotePath:  fmt.Sprintf("Projects/proj/sessions/2026-03-%02d-%02d.md", (i%7)+1, i+1),
			Decisions: []string{fmt.Sprintf("Decision number %d is unique", i)},
		}
	}

	doc := idx.ProjectContext("proj", ContextOptions{Now: now})

	// Count decision lines
	decisionCount := 0
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "- Decision number") {
			decisionCount++
		}
	}
	if decisionCount > 15 {
		t.Errorf("decisions should be capped at 15, got %d", decisionCount)
	}
}

// --- Key files recency weighting tests ---

func TestKeyFilesRecencyWeighting(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}
	now := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	// Old file: touched in 10 sessions 60 days ago (weight=1 each, score=10)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("s-old-%d", i)
		idx.Entries[id] = SessionEntry{
			SessionID: id, Project: "proj", Date: "2026-01-08",
			Iteration: i + 1, NotePath: "Projects/proj/sessions/note.md",
			FilesChanged: []string{"old_file.go"},
		}
	}

	// Recent file: touched in 2 sessions within 14 days (weight=3 each, score=6)
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("s-recent-%d", i)
		idx.Entries[id] = SessionEntry{
			SessionID: id, Project: "proj", Date: "2026-03-06",
			Iteration: i + 1, NotePath: "Projects/proj/sessions/note2.md",
			FilesChanged: []string{"recent_file.go"},
		}
	}

	doc := idx.ProjectContext("proj", ContextOptions{Now: now})

	// Both should appear (both score >= 5)
	if !contains(doc, "old_file.go") {
		t.Error("old_file.go should appear (score 10)")
	}
	if !contains(doc, "recent_file.go") {
		t.Error("recent_file.go should appear (score 6)")
	}
}

func TestKeyFilesOldBelowThreshold(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}
	now := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	// File touched in 4 sessions 60 days ago (weight=1 each, score=4 < threshold 5)
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("s-old-%d", i)
		idx.Entries[id] = SessionEntry{
			SessionID: id, Project: "proj", Date: "2026-01-08",
			Iteration: i + 1, NotePath: "Projects/proj/sessions/note.md",
			FilesChanged: []string{"marginally_used.go"},
		}
	}

	doc := idx.ProjectContext("proj", ContextOptions{Now: now})

	if contains(doc, "marginally_used.go") {
		t.Error("file with weighted score 4 should not appear (threshold 5)")
	}
}

func TestKeyFilesMidRangeRecency(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}
	now := time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC)

	// File touched 3 times within 30 days (weight=2 each, score=6)
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("s-mid-%d", i)
		idx.Entries[id] = SessionEntry{
			SessionID: id, Project: "proj", Date: "2026-02-20",
			Iteration: i + 1, NotePath: "Projects/proj/sessions/note.md",
			FilesChanged: []string{"mid_file.go"},
		}
	}

	doc := idx.ProjectContext("proj", ContextOptions{Now: now})

	if !contains(doc, "mid_file.go") {
		t.Error("mid_file.go should appear (score 6 >= threshold 5)")
	}
}

// --- Edge cases ---

func TestProjectContextSingleSession(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}

	idx.Entries["s1"] = SessionEntry{
		SessionID: "s1", Project: "proj", Date: "2026-03-08",
		Iteration: 1, NotePath: "Projects/proj/sessions/2026-03-08-01.md",
		Summary: "Only session", Tag: "planning",
		Decisions: []string{"Initial architecture [permanent]"},
	}

	doc := idx.ProjectContext("proj", ContextOptions{
		Now: time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
	})

	if !contains(doc, "sessions: 1") {
		t.Error("should show 1 session")
	}
	if !contains(doc, "— Only session") {
		t.Error("single session should show in timeline with full detail")
	}
	if !contains(doc, "Initial architecture [permanent]") {
		t.Error("decision should appear")
	}
}

func TestProjectContextAllOldSessions(t *testing.T) {
	idx := &Index{Entries: make(map[string]SessionEntry)}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// All sessions older than 30 days
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("s-%d", i)
		idx.Entries[id] = SessionEntry{
			SessionID: id, Project: "proj",
			Date:      fmt.Sprintf("2026-01-%02d", i+1),
			Iteration: 1, NotePath: fmt.Sprintf("Projects/proj/sessions/2026-01-%02d-01.md", i+1),
			Summary:   fmt.Sprintf("Old session %d", i),
		}
	}

	doc := idx.ProjectContext("proj", ContextOptions{Now: now})

	// Timeline should be empty (all sessions beyond window)
	if contains(doc, "[[2026-01-") {
		t.Error("all sessions beyond window should be omitted from timeline")
	}
	// But the document should still be generated with session count
	if !contains(doc, "sessions: 5") {
		t.Error("session count should include all sessions")
	}
}

func TestIsPermanentDecision(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"Use stdlib HTTP [permanent]", true},
		{"Use stdlib HTTP [PERMANENT]", true},
		{"Use stdlib HTTP [Permanent]", true},
		{"Dual license [core]", true},
		{"Dual license [CORE]", true},
		{"Regular decision about core logic", false},
		{"No markers here", false},
	}
	for _, tt := range tests {
		got := isPermanentDecision(tt.text)
		if got != tt.want {
			t.Errorf("isPermanentDecision(%q) = %v, want %v", tt.text, got, tt.want)
		}
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

func TestContextAvailableRoundTrip(t *testing.T) {
	dir := t.TempDir()

	idx := &Index{Entries: map[string]SessionEntry{
		"with-ctx": {
			SessionID: "with-ctx",
			Project:   "proj",
			Date:      "2026-03-09",
			Iteration: 1,
			Title:     "Session with context",
			CreatedAt: time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
			Context: &ContextAvailable{
				HasHistory:      true,
				HasKnowledge:    true,
				HistorySessions: 5,
			},
		},
		"no-ctx": {
			SessionID: "no-ctx",
			Project:   "proj",
			Date:      "2026-03-09",
			Iteration: 2,
			Title:     "Session without context",
			CreatedAt: time.Date(2026, 3, 9, 11, 0, 0, 0, time.UTC),
		},
	}}

	idx.path = filepath.Join(dir, "session-index.json")
	if err := idx.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Entry with context should round-trip
	e := loaded.Entries["with-ctx"]
	if e.Context == nil {
		t.Fatal("Context should not be nil after round-trip")
	}
	if !e.Context.HasHistory {
		t.Error("HasHistory should be true")
	}
	if !e.Context.HasKnowledge {
		t.Error("HasKnowledge should be true")
	}
	if e.Context.HistorySessions != 5 {
		t.Errorf("HistorySessions = %d, want 5", e.Context.HistorySessions)
	}

	// Entry without context should have nil
	e2 := loaded.Entries["no-ctx"]
	if e2.Context != nil {
		t.Errorf("Context should be nil for entry without context, got %+v", e2.Context)
	}
}

func TestContextAvailableBackwardsCompat(t *testing.T) {
	// Old entries without the context field should load fine
	oldJSON := `{
		"old-sess": {
			"session_id": "old-sess",
			"project": "proj",
			"date": "2026-02-01",
			"iteration": 1,
			"title": "Old session",
			"created_at": "2026-02-01T10:00:00Z"
		}
	}`

	var entries map[string]SessionEntry
	if err := json.Unmarshal([]byte(oldJSON), &entries); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	e := entries["old-sess"]
	if e.Context != nil {
		t.Errorf("Context should be nil for old entries, got %+v", e.Context)
	}
}

func TestProjectSessionCount(t *testing.T) {
	idx := &Index{Entries: map[string]SessionEntry{
		"a": {Project: "proj1"},
		"b": {Project: "proj1"},
		"c": {Project: "proj2"},
		"d": {Project: "proj1"},
	}}

	if got := idx.ProjectSessionCount("proj1"); got != 3 {
		t.Errorf("ProjectSessionCount(proj1) = %d, want 3", got)
	}
	if got := idx.ProjectSessionCount("proj2"); got != 1 {
		t.Errorf("ProjectSessionCount(proj2) = %d, want 1", got)
	}
	if got := idx.ProjectSessionCount("nonexistent"); got != 0 {
		t.Errorf("ProjectSessionCount(nonexistent) = %d, want 0", got)
	}
}
