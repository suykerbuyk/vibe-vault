package synthesis

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/llm"
)

func TestRun_NilProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Synthesis.Enabled = true

	report, err := Run(context.Background(), RunOpts{Provider: nil}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if report != nil {
		t.Error("expected nil report for nil provider")
	}
}

func TestRun_Disabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Synthesis.Enabled = false

	mp := &mockProvider{response: &llm.Response{Content: `{}`}}
	report, err := Run(context.Background(), RunOpts{Provider: mp}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if report != nil {
		t.Error("expected nil report when disabled")
	}
	if mp.calls != 0 {
		t.Error("should not call provider when disabled")
	}
}

func TestRun_EndToEnd(t *testing.T) {
	dir := t.TempDir()

	// Set up vault structure
	projectDir := filepath.Join(dir, "Projects", "testproj")
	agentctx := filepath.Join(projectDir, "agentctx")
	sessionsDir := filepath.Join(projectDir, "sessions")
	os.MkdirAll(agentctx, 0o755)
	os.MkdirAll(sessionsDir, 0o755)

	// Write knowledge.md
	os.WriteFile(filepath.Join(projectDir, "knowledge.md"), []byte(
		"# Knowledge — testproj\n\n## Decisions\n\n## Patterns\n\n## Learnings\n",
	), 0o644)

	// Write session note
	notePath := filepath.Join(sessionsDir, "2026-03-27-01.md")
	os.WriteFile(notePath, []byte(
		"---\nproject: testproj\ndate: 2026-03-27\nsummary: Test session\ntag: implementation\n---\n\n## Key Decisions\n\n- Test decision\n",
	), 0o644)

	resp := `{
		"learnings": [{"section": "Decisions", "entry": "Test learning from synthesis"}],
		"stale_entries": [],
		"resume_update": null,
		"task_updates": [],
		"reasoning": "test"
	}`
	mp := &mockProvider{response: &llm.Response{Content: resp}}

	cfg := config.DefaultConfig()
	cfg.VaultPath = dir
	cfg.Synthesis.Enabled = true

	report, err := Run(context.Background(), RunOpts{
		NotePath: notePath,
		CWD:      dir,
		Project:  "testproj",
		Provider: mp,
		Index:    nil,
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if report.LearningsAdded != 1 {
		t.Errorf("learnings added=%d, want 1", report.LearningsAdded)
	}

	// Verify file was updated
	data, _ := os.ReadFile(filepath.Join(projectDir, "knowledge.md"))
	if !strings.Contains(string(data), "Test learning from synthesis") {
		t.Error("learning not written to knowledge.md")
	}
}

func TestRun_LLMError(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "Projects", "testproj")
	sessionsDir := filepath.Join(projectDir, "sessions")
	os.MkdirAll(sessionsDir, 0o755)

	notePath := filepath.Join(sessionsDir, "2026-03-27-01.md")
	os.WriteFile(notePath, []byte(
		"---\nproject: testproj\ndate: 2026-03-27\nsummary: Test\ntag: test\n---\n",
	), 0o644)

	mp := &mockProvider{err: errors.New("API error")}

	cfg := config.DefaultConfig()
	cfg.VaultPath = dir
	cfg.Synthesis.Enabled = true

	_, err := Run(context.Background(), RunOpts{
		NotePath: notePath,
		CWD:      dir,
		Project:  "testproj",
		Provider: mp,
	}, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRun_EmptyResult(t *testing.T) {
	dir := t.TempDir()
	projectDir := filepath.Join(dir, "Projects", "testproj")
	sessionsDir := filepath.Join(projectDir, "sessions")
	os.MkdirAll(sessionsDir, 0o755)

	notePath := filepath.Join(sessionsDir, "2026-03-27-01.md")
	os.WriteFile(notePath, []byte(
		"---\nproject: testproj\ndate: 2026-03-27\nsummary: Quiet session\ntag: exploration\n---\n",
	), 0o644)

	resp := `{"learnings":[],"stale_entries":[],"resume_update":null,"task_updates":[],"reasoning":"nothing notable"}`
	mp := &mockProvider{response: &llm.Response{Content: resp}}

	cfg := config.DefaultConfig()
	cfg.VaultPath = dir
	cfg.Synthesis.Enabled = true

	report, err := Run(context.Background(), RunOpts{
		NotePath: notePath,
		CWD:      dir,
		Project:  "testproj",
		Provider: mp,
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if report.LearningsAdded != 0 {
		t.Errorf("expected no learnings added, got %d", report.LearningsAdded)
	}
}
