// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/index"
	"github.com/johns/vibe-vault/internal/narrative"
	"github.com/johns/vibe-vault/internal/prose"
	"github.com/johns/vibe-vault/internal/transcript"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{VaultPath: t.TempDir()}
}

func TestCaptureFromParsed_Basic(t *testing.T) {
	cfg := testConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "testproj", "sessions"), 0o755)

	tr := &transcript.Transcript{
		Stats: transcript.Stats{
			SessionID:         "test-session-1",
			UserMessages:      3,
			AssistantMessages: 3,
			ToolUses:          5,
			StartTime:         time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC),
			Duration:          10 * time.Minute,
		},
	}

	info := Info{
		Project:   "testproj",
		Domain:    "personal",
		SessionID: "test-session-1",
	}

	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}

	opts := CaptureOpts{
		TranscriptPath: "/tmp/test.jsonl",
		Index:          idx,
	}

	result, err := CaptureFromParsed(tr, info, nil, nil, opts, cfg)
	if err != nil {
		t.Fatalf("CaptureFromParsed error: %v", err)
	}
	if result.Skipped {
		t.Fatalf("expected non-skipped result, got skipped: %s", result.Reason)
	}
	if result.Project != "testproj" {
		t.Errorf("Project = %q, want %q", result.Project, "testproj")
	}
	if !strings.Contains(result.NotePath, "testproj") {
		t.Errorf("NotePath = %q, expected to contain 'testproj'", result.NotePath)
	}

	// Verify index was updated
	entry, ok := idx.Entries["test-session-1"]
	if !ok {
		t.Fatal("session not found in index")
	}
	if entry.Source != "" {
		t.Errorf("Source = %q, want empty for claude-code", entry.Source)
	}
	if entry.SourceName() != "claude-code" {
		t.Errorf("SourceName() = %q, want %q", entry.SourceName(), "claude-code")
	}
}

func TestCaptureFromParsed_ZedSource(t *testing.T) {
	cfg := testConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "myproj", "sessions"), 0o755)

	tr := &transcript.Transcript{
		Stats: transcript.Stats{
			SessionID:         "zed:abc-123",
			UserMessages:      5,
			AssistantMessages: 4,
			ToolUses:          10,
			StartTime:         time.Date(2026, 3, 8, 14, 0, 0, 0, time.UTC),
			Duration:          20 * time.Minute,
		},
	}

	info := Info{
		Project:   "myproj",
		Domain:    "work",
		SessionID: "zed:abc-123",
		Model:     "anthropic/claude-sonnet-4-5",
	}

	narr := &narrative.Narrative{
		Title:   "Implement feature X",
		Summary: "Added feature X with tests",
		Tag:     "build",
	}

	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}

	opts := CaptureOpts{
		TranscriptPath: "zed:/path/to/threads.db#abc-123",
		Source:         "zed",
		SkipEnrichment: true,
		Index:          idx,
	}

	result, err := CaptureFromParsed(tr, info, narr, nil, opts, cfg)
	if err != nil {
		t.Fatalf("CaptureFromParsed error: %v", err)
	}
	if result.Skipped {
		t.Fatalf("expected non-skipped, got skipped: %s", result.Reason)
	}
	if result.Project != "myproj" {
		t.Errorf("Project = %q, want %q", result.Project, "myproj")
	}

	// Verify index entry has source=zed
	entry, ok := idx.Entries["zed:abc-123"]
	if !ok {
		t.Fatal("session not found in index")
	}
	if entry.Source != "zed" {
		t.Errorf("Source = %q, want %q", entry.Source, "zed")
	}
	if entry.SourceName() != "zed" {
		t.Errorf("SourceName() = %q, want %q", entry.SourceName(), "zed")
	}
	if entry.TranscriptPath != "zed:/path/to/threads.db#abc-123" {
		t.Errorf("TranscriptPath = %q, want zed: prefixed path", entry.TranscriptPath)
	}

	// Verify note file contains source in frontmatter
	notePath := filepath.Join(cfg.VaultPath, result.NotePath)
	data, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "source: zed") {
		t.Error("note missing 'source: zed' in frontmatter")
	}
}

func TestCaptureFromParsed_TrivialSkip(t *testing.T) {
	cfg := testConfig(t)

	tr := &transcript.Transcript{
		Stats: transcript.Stats{
			UserMessages:      1,
			AssistantMessages: 1,
		},
	}

	info := Info{Project: "test", Domain: "personal", SessionID: "trivial-1"}
	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}

	result, err := CaptureFromParsed(tr, info, nil, nil, CaptureOpts{Index: idx}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Skipped {
		t.Error("expected trivial session to be skipped")
	}
	if result.Reason != "trivial session (< 2 messages)" {
		t.Errorf("Reason = %q, want trivial message", result.Reason)
	}
}

func TestCaptureFromParsed_Dedup(t *testing.T) {
	cfg := testConfig(t)

	tr := &transcript.Transcript{
		Stats: transcript.Stats{
			SessionID:         "dedup-1",
			UserMessages:      5,
			AssistantMessages: 5,
			StartTime:         time.Now(),
		},
	}

	info := Info{Project: "test", Domain: "personal", SessionID: "dedup-1"}
	idx := &index.Index{Entries: map[string]index.SessionEntry{
		"dedup-1": {SessionID: "dedup-1", Checkpoint: false},
	}}

	result, err := CaptureFromParsed(tr, info, nil, nil, CaptureOpts{Index: idx}, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Skipped {
		t.Error("expected dedup skip")
	}
	if result.Reason != "already processed" {
		t.Errorf("Reason = %q, want 'already processed'", result.Reason)
	}
}

func TestCaptureFromParsed_ZedFallbackSummary(t *testing.T) {
	cfg := testConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "proj", "sessions"), 0o755)

	// A transcript with no substantive first message → "Session" title → fallback summary
	tr := &transcript.Transcript{
		Stats: transcript.Stats{
			SessionID:         "zed:fallback-test",
			UserMessages:      3,
			AssistantMessages: 3,
			StartTime:         time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC),
		},
	}

	info := Info{Project: "proj", Domain: "personal", SessionID: "zed:fallback-test"}
	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}

	opts := CaptureOpts{
		Source: "zed",
		Index:  idx,
	}

	result, err := CaptureFromParsed(tr, info, nil, nil, opts, cfg)
	if err != nil {
		t.Fatalf("CaptureFromParsed error: %v", err)
	}
	if result.Skipped {
		t.Fatalf("expected non-skipped, got skipped: %s", result.Reason)
	}

	// Verify note uses "Zed agent session" fallback, not "Claude Code session"
	notePath := filepath.Join(cfg.VaultPath, result.NotePath)
	data, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	content := string(data)
	if strings.Contains(content, "Claude Code session") {
		t.Error("note should not contain 'Claude Code session' for zed source")
	}
	if !strings.Contains(content, "Zed agent session") {
		t.Error("note should contain 'Zed agent session' as fallback summary")
	}
}

func TestCaptureFromParsed_WithNarrativeAndDialogue(t *testing.T) {
	cfg := testConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "myproj", "sessions"), 0o755)

	tr := &transcript.Transcript{
		Stats: transcript.Stats{
			SessionID:         "zed:full-test",
			UserMessages:      5,
			AssistantMessages: 5,
			ToolUses:          8,
			StartTime:         time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC),
			Duration:          15 * time.Minute,
		},
	}

	info := Info{
		Project:   "myproj",
		Domain:    "personal",
		SessionID: "zed:full-test",
		Model:     "anthropic/claude-sonnet-4-5",
		Branch:    "feature/xyz",
	}

	narr := &narrative.Narrative{
		Title:   "Refactor auth module",
		Summary: "Extracted auth logic into separate package",
		Tag:     "refactor",
		Commits: []narrative.Commit{
			{SHA: "abc1234", Message: "refactor: extract auth"},
		},
	}

	dialogue := &prose.Dialogue{
		Sections: []prose.Section{
			{
				UserRequest: "Refactor the auth module",
				Elements: []prose.Element{
					{Turn: &prose.Turn{Role: "user", Text: "Please refactor the auth module"}},
					{Turn: &prose.Turn{Role: "assistant", Text: "I'll extract the auth logic into a separate package."}},
				},
			},
		},
	}

	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}

	opts := CaptureOpts{
		TranscriptPath: "zed:/db#full-test",
		Source:         "zed",
		SkipEnrichment: true,
		Index:          idx,
	}

	result, err := CaptureFromParsed(tr, info, narr, dialogue, opts, cfg)
	if err != nil {
		t.Fatalf("CaptureFromParsed error: %v", err)
	}
	if result.Skipped {
		t.Fatalf("expected non-skipped, got skipped: %s", result.Reason)
	}
	if result.Title != "Refactor auth module" {
		t.Errorf("Title = %q, want %q", result.Title, "Refactor auth module")
	}

	// Verify note content
	data, err := os.ReadFile(filepath.Join(cfg.VaultPath, result.NotePath))
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	content := string(data)

	checks := []string{
		"source: zed",
		"Refactor auth module",
		"Session Dialogue",
		"abc1234",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Errorf("note missing expected content: %q", check)
		}
	}

	// Verify index entry
	entry := idx.Entries["zed:full-test"]
	if entry.Source != "zed" {
		t.Errorf("index Source = %q, want %q", entry.Source, "zed")
	}
	if entry.Tag != "refactor" {
		t.Errorf("index Tag = %q, want %q", entry.Tag, "refactor")
	}
}

func TestCaptureFromParsed_Idempotent(t *testing.T) {
	cfg := testConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "proj", "sessions"), 0o755)

	tr := &transcript.Transcript{
		Stats: transcript.Stats{
			SessionID:         "zed:idem-test",
			UserMessages:      3,
			AssistantMessages: 3,
			StartTime:         time.Date(2026, 3, 8, 10, 0, 0, 0, time.UTC),
		},
	}

	info := Info{Project: "proj", Domain: "personal", SessionID: "zed:idem-test"}
	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}

	opts := CaptureOpts{
		Source: "zed",
		Index:  idx,
	}

	// First capture
	result1, err := CaptureFromParsed(tr, info, nil, nil, opts, cfg)
	if err != nil {
		t.Fatalf("first capture error: %v", err)
	}
	if result1.Skipped {
		t.Fatalf("first capture skipped: %s", result1.Reason)
	}

	// Second capture without force → should be deduped
	result2, err := CaptureFromParsed(tr, info, nil, nil, opts, cfg)
	if err != nil {
		t.Fatalf("second capture error: %v", err)
	}
	if !result2.Skipped {
		t.Error("second capture should be skipped (dedup)")
	}
	if result2.Reason != "already processed" {
		t.Errorf("Reason = %q, want 'already processed'", result2.Reason)
	}

	// Third capture with force → should succeed
	opts.Force = true
	result3, err := CaptureFromParsed(tr, info, nil, nil, opts, cfg)
	if err != nil {
		t.Fatalf("third capture error: %v", err)
	}
	if result3.Skipped {
		t.Error("third capture (with force) should not be skipped")
	}
	if result3.NotePath != result1.NotePath {
		t.Errorf("forced capture path = %q, want same path %q", result3.NotePath, result1.NotePath)
	}
}

func TestCaptureFromParsed_ContextAvailable_NoContext(t *testing.T) {
	cfg := testConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "newproj", "sessions"), 0o755)

	tr := &transcript.Transcript{
		Stats: transcript.Stats{
			SessionID:         "ctx-none",
			UserMessages:      3,
			AssistantMessages: 3,
			StartTime:         time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
		},
	}

	info := Info{Project: "newproj", Domain: "personal", SessionID: "ctx-none"}
	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}
	opts := CaptureOpts{Index: idx}

	result, err := CaptureFromParsed(tr, info, nil, nil, opts, cfg)
	if err != nil {
		t.Fatalf("CaptureFromParsed error: %v", err)
	}
	if result.Skipped {
		t.Fatalf("expected non-skipped, got: %s", result.Reason)
	}

	entry := idx.Entries["ctx-none"]
	if entry.Context != nil {
		t.Errorf("Context should be nil for first session with no history/knowledge, got %+v", entry.Context)
	}
}

func TestCaptureFromParsed_ContextAvailable_WithHistory(t *testing.T) {
	cfg := testConfig(t)
	projDir := filepath.Join(cfg.VaultPath, "Projects", "matureproj")
	os.MkdirAll(filepath.Join(projDir, "sessions"), 0o755)

	// Create history.md and non-empty knowledge.md
	os.WriteFile(filepath.Join(projDir, "history.md"), []byte("# History\nsome content"), 0o644)
	os.WriteFile(filepath.Join(projDir, "knowledge.md"), []byte("# Knowledge\ndecisions here"), 0o644)

	tr := &transcript.Transcript{
		Stats: transcript.Stats{
			SessionID:         "ctx-full",
			UserMessages:      3,
			AssistantMessages: 3,
			StartTime:         time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
		},
	}

	// Pre-populate index with existing sessions for this project
	idx := &index.Index{Entries: map[string]index.SessionEntry{
		"prev-1": {Project: "matureproj", Date: "2026-03-08"},
		"prev-2": {Project: "matureproj", Date: "2026-03-07"},
		"other":  {Project: "otherproj", Date: "2026-03-08"},
	}}

	info := Info{Project: "matureproj", Domain: "personal", SessionID: "ctx-full"}
	opts := CaptureOpts{Index: idx}

	result, err := CaptureFromParsed(tr, info, nil, nil, opts, cfg)
	if err != nil {
		t.Fatalf("CaptureFromParsed error: %v", err)
	}
	if result.Skipped {
		t.Fatalf("expected non-skipped, got: %s", result.Reason)
	}

	entry := idx.Entries["ctx-full"]
	if entry.Context == nil {
		t.Fatal("Context should not be nil when history and knowledge exist")
	}
	if !entry.Context.HasHistory {
		t.Error("HasHistory should be true")
	}
	if !entry.Context.HasKnowledge {
		t.Error("HasKnowledge should be true")
	}
	// 2 existing sessions (probe runs before idx.Add)
	if entry.Context.HistorySessions != 2 {
		t.Errorf("HistorySessions = %d, want 2", entry.Context.HistorySessions)
	}
}

func TestCaptureFromParsed_ContextAvailable_EmptyKnowledge(t *testing.T) {
	cfg := testConfig(t)
	projDir := filepath.Join(cfg.VaultPath, "Projects", "halfproj")
	os.MkdirAll(filepath.Join(projDir, "sessions"), 0o755)

	// history.md exists but knowledge.md is empty
	os.WriteFile(filepath.Join(projDir, "history.md"), []byte("# History"), 0o644)
	os.WriteFile(filepath.Join(projDir, "knowledge.md"), []byte(""), 0o644)

	tr := &transcript.Transcript{
		Stats: transcript.Stats{
			SessionID:         "ctx-half",
			UserMessages:      3,
			AssistantMessages: 3,
			StartTime:         time.Date(2026, 3, 9, 10, 0, 0, 0, time.UTC),
		},
	}

	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}
	info := Info{Project: "halfproj", Domain: "personal", SessionID: "ctx-half"}
	opts := CaptureOpts{Index: idx}

	result, err := CaptureFromParsed(tr, info, nil, nil, opts, cfg)
	if err != nil {
		t.Fatalf("CaptureFromParsed error: %v", err)
	}
	if result.Skipped {
		t.Fatalf("expected non-skipped, got: %s", result.Reason)
	}

	entry := idx.Entries["ctx-half"]
	if entry.Context == nil {
		t.Fatal("Context should not be nil when history exists")
	}
	if !entry.Context.HasHistory {
		t.Error("HasHistory should be true")
	}
	if entry.Context.HasKnowledge {
		t.Error("HasKnowledge should be false for empty knowledge.md")
	}
}
