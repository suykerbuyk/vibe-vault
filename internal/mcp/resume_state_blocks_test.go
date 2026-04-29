// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// resumeWithMarkersForHeal is a fixture that already has all three
// marker pairs in place. Useful when asserting the auto-heal path
// preserves marker positioning byte-identically.
const resumeWithMarkersForHeal = `# Resume

## Current State

<!-- vv:current-state:start -->
old current state
<!-- vv:current-state:end -->

## Open Threads

<!-- vv:active-tasks:start -->
old active tasks
<!-- vv:active-tasks:end -->

### existing-thread

Body.

### Carried forward

- **stale-item** — stale stale

## Project History (recent)

<!-- vv:project-history-tail:start -->
old history
<!-- vv:project-history-tail:end -->
`

const minimalIterationsForHeal = `# Iterations

### Iteration 9 — First (2026-04-24)

Narrative for iter 9.

### Iteration 10 — Second (2026-04-25)

Narrative for iter 10.
`

// makeStateBlocksVault builds a fresh vault with resume.md +
// iterations.md and one task file, returning the cfg for tests.
func makeStateBlocksVault(t *testing.T) config.Config {
	t.Helper()
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproject/agentctx/resume.md":     resumeWithMarkersForHeal,
		"Projects/myproject/agentctx/iterations.md": minimalIterationsForHeal,
	})
	taskPath := filepath.Join(cfg.VaultPath, "Projects", "myproject", "agentctx", "tasks", "alpha.md")
	if err := os.MkdirAll(filepath.Dir(taskPath), 0o755); err != nil {
		t.Fatalf("mkdir tasks: %v", err)
	}
	if err := os.WriteFile(taskPath, []byte("---\ntitle: Alpha\nstatus: WIP\npriority: high\n---\n# Alpha\n"), 0o644); err != nil {
		t.Fatalf("write task: %v", err)
	}
	return cfg
}

// TestRenderResumeStateBlocks_ByteIdenticalWithApplyBundle asserts the
// new shared entry point and the legacy ApplyBundle Step 9 produce the
// same on-disk resume.md content. This locks the contract that D4b
// auto-heal hooks preserve Step-9 semantics exactly.
func TestRenderResumeStateBlocks_ByteIdenticalWithApplyBundle(t *testing.T) {
	cfgA := makeStateBlocksVault(t)
	gotA, err := RenderResumeStateBlocks(cfgA, "myproject")
	if err != nil {
		t.Fatalf("RenderResumeStateBlocks: %v", err)
	}

	// Build a second identical vault and run the legacy entry point.
	// applyResumeStateBlocks now delegates to RenderResumeStateBlocks
	// but we exercise the legacy signature explicitly to lock the
	// contract for callers that still call through the old name.
	cfgB := makeStateBlocksVault(t)
	gotB, err := applyResumeStateBlocks(cfgB, "myproject", "")
	if err != nil {
		t.Fatalf("applyResumeStateBlocks: %v", err)
	}

	if gotA != gotB {
		t.Errorf("RenderResumeStateBlocks vs applyResumeStateBlocks output diverged\nA:\n%s\n---\nB:\n%s\n",
			gotA, gotB)
	}
	// Sanity: file on disk matches return value.
	dataA, _ := os.ReadFile(filepath.Join(cfgA.VaultPath, "Projects/myproject/agentctx/resume.md"))
	if string(dataA) != gotA {
		t.Errorf("on-disk content != returned content")
	}
}

// TestAutoHealAppendIteration_RegeneratesStateBlocks asserts the
// vv_append_iteration handler invokes the auto-heal hook so iteration
// counts in the marker block converge with the freshly-appended
// iteration.
func TestAutoHealAppendIteration_RegeneratesStateBlocks(t *testing.T) {
	cfg := makeStateBlocksVault(t)
	tool := NewAppendIterationTool(cfg)
	args, _ := json.Marshal(map[string]any{
		"project":   "myproject",
		"title":     "Auto-heal exercises",
		"narrative": "body",
		"date":      "2026-04-26",
	})
	if _, err := tool.Handler(args); err != nil {
		t.Fatalf("AppendIteration: %v", err)
	}

	resume, err := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects/myproject/agentctx/resume.md"))
	if err != nil {
		t.Fatalf("read resume: %v", err)
	}
	body := string(resume)

	// "old current state" marker body must be replaced with rendered
	// state. Iterations counted before append: 2; after append: 3.
	if strings.Contains(body, "old current state") {
		t.Errorf("auto-heal did not rewrite current-state marker body\n%s", body)
	}
	if strings.Contains(body, "old history") {
		t.Errorf("auto-heal did not rewrite project-history marker body\n%s", body)
	}
	if strings.Contains(body, "old active tasks") {
		t.Errorf("auto-heal did not rewrite active-tasks marker body\n%s", body)
	}
	// Marker pairs survived intact.
	for _, m := range []string{
		"<!-- vv:current-state:start -->",
		"<!-- vv:current-state:end -->",
		"<!-- vv:active-tasks:start -->",
		"<!-- vv:active-tasks:end -->",
		"<!-- vv:project-history-tail:start -->",
		"<!-- vv:project-history-tail:end -->",
	} {
		if !strings.Contains(body, m) {
			t.Errorf("missing marker after auto-heal: %q\n%s", m, body)
		}
	}
}

// TestAutoHealUpdateResume_RegeneratesStateBlocks asserts vv_update_resume
// also calls the auto-heal hook.
func TestAutoHealUpdateResume_RegeneratesStateBlocks(t *testing.T) {
	cfg := makeStateBlocksVault(t)
	tool := NewUpdateResumeTool(cfg)
	args, _ := json.Marshal(map[string]any{
		"project": "myproject",
		"section": "Current State",
		"content": "<!-- vv:current-state:start -->\nold current state\n<!-- vv:current-state:end -->\n",
	})
	if _, err := tool.Handler(args); err != nil {
		t.Fatalf("UpdateResume: %v", err)
	}

	resume, err := os.ReadFile(filepath.Join(cfg.VaultPath, "Projects/myproject/agentctx/resume.md"))
	if err != nil {
		t.Fatalf("read resume: %v", err)
	}
	body := string(resume)
	// The auto-heal must have rewritten the marker body even though
	// the operator wrote a stale value through update_resume.
	if strings.Contains(body, "old current state") {
		t.Errorf("auto-heal did not converge marker body to ground truth\n%s", body)
	}
}

// TestAutoHealResumeStateBlocks_NoResumeMd ensures the auto-heal hook
// is best-effort: a missing resume.md does NOT block the primary tool
// (e.g., a project still in pre-resume.md state).
func TestAutoHealResumeStateBlocks_NoResumeMd(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	if err := autoHealResumeStateBlocks(cfg, "no-such-project"); err != nil {
		t.Errorf("auto-heal should silently skip when resume.md missing; got %v", err)
	}
}

// TestRenderResumeStateBlocks_DeterministicAcrossRuns asserts running
// the renderer twice in a row returns identical content (the renderer
// is converging on filesystem ground truth, so the second run must
// be a no-op byte-wise).
func TestRenderResumeStateBlocks_DeterministicAcrossRuns(t *testing.T) {
	cfg := makeStateBlocksVault(t)
	first, err := RenderResumeStateBlocks(cfg, "myproject")
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, err := RenderResumeStateBlocks(cfg, "myproject")
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if first != second {
		t.Errorf("renderer not idempotent\nfirst:\n%s\nsecond:\n%s\n", first, second)
	}
}

// TestCollectActiveTasksReturnsAll exercises the helper directly
// (extra coverage on the new file's API surface).
func TestCollectActiveTasksReturnsAll(t *testing.T) {
	cfg := makeStateBlocksVault(t)
	tasks, err := collectActiveTasks(cfg, "myproject")
	if err != nil {
		t.Fatalf("collectActiveTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Slug != "alpha" {
		t.Errorf("got %+v", tasks)
	}
}

// TestComputeCurrentState_HitsAllSources exercises the helper.
func TestComputeCurrentState_HitsAllSources(t *testing.T) {
	cfg := makeStateBlocksVault(t)
	state, err := computeCurrentState(cfg, "myproject")
	if err != nil {
		t.Fatalf("computeCurrentState: %v", err)
	}
	if state.Iterations != 2 {
		t.Errorf("iterations = %d, want 2", state.Iterations)
	}
	if state.MCPTools < 30 {
		// Production registers many tools; ensure RegisterAllTools wired
		// correctly by checking we got a plausible count.
		t.Errorf("mcp_tools = %d, want >= 30", state.MCPTools)
	}
	if state.Templates < 1 {
		t.Errorf("templates = %d, want >= 1", state.Templates)
	}
}

// TestCollectHistoryRowsRespectsLimit exercises the helper.
func TestCollectHistoryRowsRespectsLimit(t *testing.T) {
	cfg := makeStateBlocksVault(t)
	rows, err := collectHistoryRows(cfg, "myproject", 1)
	if err != nil {
		t.Fatalf("collectHistoryRows: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("len(rows) = %d, want 1", len(rows))
	}
	if rows[0].Iteration != 10 {
		t.Errorf("rows[0].Iteration = %d, want 10 (most recent)", rows[0].Iteration)
	}
}

func TestSummarizeIterationNarrative(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Short narrative.", "Short narrative."},
		{"\n\nFirst paragraph here.\n\nSecond.", "First paragraph here."},
		{strings.Repeat("word ", 40), strings.TrimRight(strings.Repeat("word ", 24), " ") + "…"},
	}
	for _, c := range cases {
		got := summarizeIterationNarrative(c.in)
		if got != c.want {
			t.Errorf("summarizeIterationNarrative(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// silenceContext is a no-op used to keep the context import live in
// case future helpers add ctx-aware seams.
var _ = context.Background
