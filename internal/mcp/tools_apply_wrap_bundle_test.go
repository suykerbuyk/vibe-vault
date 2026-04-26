// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/wrapmetrics"
)

// minimalResumeMd is a resume.md fixture with an ## Open Threads section and
// a ### Carried forward sub-section, usable by apply-bundle tests.
const minimalResumeMd = `# Resume

## Current State

**Iteration:** 10

## Open Threads

### existing-thread

Existing thread body.

### Carried forward

- **stale-item** — stale item title stale body

## Project History

nothing here
`

// minimalIterationsMd contains one existing iteration so auto-increment works.
const minimalIterationsMd = `# Iterations

### Iteration 9 — Previous wrap (2026-04-24)

Previous narrative.
`

// buildMinimalBundle constructs a WrapBundle suitable for apply tests.
// iteration is set to avoid the "0 → auto-increment" path; all slice fields
// default to empty (non-nil) to avoid null JSON.
func buildMinimalBundle(iteration int, iterContent, commitContent string) WrapBundle {
	cc := BundleCaptureContent{
		Summary:      "Test wrap summary.",
		Tag:          "implementation",
		Decisions:    []string{},
		FilesChanged: []string{},
		OpenThreads:  []string{},
	}
	captureSHA, _ := fingerprintJSON(cc)
	return WrapBundle{
		IterationBlock: BundleFieldWithContent{
			Content:     iterContent,
			SynthSHA256: fingerprintString(iterContent),
		},
		CommitMsg: BundleFieldWithContent{
			Content:     commitContent,
			SynthSHA256: fingerprintString(commitContent),
		},
		ResumeThreadBlocks:   []BundleThreadBlock{},
		ResumeThreadsToClose: []BundleThreadClose{},
		CarriedChanges: BundleCarriedChanges{
			Add:    []BundleCarriedAdd{},
			Remove: []BundleCarriedRemove{},
		},
		CaptureSession: BundleCaptureSession{
			Content:     cc,
			SynthSHA256: captureSHA,
		},
		SynthTimestamp: "2026-04-25T17:00:00Z",
		Iteration:      iteration,
	}
}

// newApplyTool creates a NewApplyWrapBundleTool with a test vault containing
// the given resume.md and iterations.md.
func newApplyTool(t *testing.T, resume, iterations string) (Tool, string) {
	t.Helper()
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproject/agentctx/resume.md":     resume,
		"Projects/myproject/agentctx/iterations.md": iterations,
	})
	// Pin $VIBE_VAULT_HOME so wrapmetrics writes to the test temp dir.
	t.Setenv("VIBE_VAULT_HOME", t.TempDir())
	return NewApplyWrapBundleTool(cfg), cfg.VaultPath
}

// invokeApply marshals bundle and calls the apply tool.
func invokeApply(t *testing.T, tool Tool, bundle WrapBundle, projectPath string) applyResult {
	t.Helper()
	bundleJSON, err := json.Marshal(bundle)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}
	params, _ := json.Marshal(map[string]any{
		"project":      "myproject",
		"project_path": projectPath,
		"bundle":       json.RawMessage(bundleJSON),
	})
	out, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	var result applyResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("unmarshal result: %v\n%s", err, out)
	}
	return result
}

// TestApplyWrapBundle_AppendIteration verifies iterations.md is updated.
func TestApplyWrapBundle_AppendIteration(t *testing.T) {
	iterBlock := BuildIterationBlock(10, "Phase 5 apply", "Narrative text here.", "2026-04-25", "")
	commitContent := "chore: test\n\nBody.\n"
	projectPath := t.TempDir()

	tool, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)

	bundle := buildMinimalBundle(10, iterBlock, commitContent)
	result := invokeApply(t, tool, bundle, projectPath)

	if result.ErrorAtStep != "" {
		t.Fatalf("unexpected error at step %q: %v", result.ErrorAtStep, result)
	}

	// Verify iterations.md contains the new block.
	iterPath := filepath.Join(vaultPath, "Projects/myproject/agentctx/iterations.md")
	data, err := os.ReadFile(iterPath)
	if err != nil {
		t.Fatalf("read iterations.md: %v", err)
	}
	if !strings.Contains(string(data), "### Iteration 10 — Phase 5 apply") {
		t.Errorf("iterations.md missing new iteration heading\ncontent:\n%s", data)
	}
}

// TestApplyWrapBundle_ThreadInsert verifies resume.md gets a new thread.
func TestApplyWrapBundle_ThreadInsert(t *testing.T) {
	iterBlock := BuildIterationBlock(10, "With threads", "Narrative.", "2026-04-25", "")
	commitContent := "chore: thread test\n\nBody.\n"
	projectPath := t.TempDir()

	tool, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)

	bundle := buildMinimalBundle(10, iterBlock, commitContent)
	bundle.ResumeThreadBlocks = []BundleThreadBlock{
		{
			Position:    map[string]string{"mode": "top"},
			Slug:        "new-phase5-thread",
			Body:        "This is the new thread body.",
			SynthSHA256: fingerprintString("new-phase5-thread\x00This is the new thread body."),
		},
	}
	result := invokeApply(t, tool, bundle, projectPath)
	if result.ErrorAtStep != "" {
		t.Fatalf("error at step %q", result.ErrorAtStep)
	}

	resumePath := filepath.Join(vaultPath, "Projects/myproject/agentctx/resume.md")
	data, _ := os.ReadFile(resumePath)
	if !strings.Contains(string(data), "### new-phase5-thread") {
		t.Errorf("resume.md missing new thread\ncontent:\n%s", data)
	}
}

// TestApplyWrapBundle_ThreadRemove verifies an existing thread is removed.
func TestApplyWrapBundle_ThreadRemove(t *testing.T) {
	iterBlock := BuildIterationBlock(10, "Remove thread", "Narrative.", "2026-04-25", "")
	commitContent := "chore: remove thread\n\nBody.\n"
	projectPath := t.TempDir()

	tool, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)

	bundle := buildMinimalBundle(10, iterBlock, commitContent)
	bundle.ResumeThreadsToClose = []BundleThreadClose{
		{Slug: "existing-thread", SynthSHA256: fingerprintString("existing-thread")},
	}
	result := invokeApply(t, tool, bundle, projectPath)
	if result.ErrorAtStep != "" {
		t.Fatalf("error at step %q: %+v", result.ErrorAtStep, result)
	}

	resumePath := filepath.Join(vaultPath, "Projects/myproject/agentctx/resume.md")
	data, _ := os.ReadFile(resumePath)
	if strings.Contains(string(data), "### existing-thread") {
		t.Errorf("resume.md still contains removed thread\ncontent:\n%s", data)
	}
}

// TestApplyWrapBundle_CarriedAdd verifies a new carried-forward bullet appears.
func TestApplyWrapBundle_CarriedAdd(t *testing.T) {
	iterBlock := BuildIterationBlock(10, "Carried add", "Narrative.", "2026-04-25", "")
	commitContent := "chore: carried\n\nBody.\n"
	projectPath := t.TempDir()

	tool, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)

	bundle := buildMinimalBundle(10, iterBlock, commitContent)
	bundle.CarriedChanges.Add = []BundleCarriedAdd{
		{
			Slug:        "new-carried-item",
			Title:       "New carried item",
			Body:        "Details here.",
			SynthSHA256: fingerprintString("new-carried-item\x00New carried item\x00Details here."),
		},
	}
	result := invokeApply(t, tool, bundle, projectPath)
	if result.ErrorAtStep != "" {
		t.Fatalf("error at step %q: %+v", result.ErrorAtStep, result)
	}

	resumePath := filepath.Join(vaultPath, "Projects/myproject/agentctx/resume.md")
	data, _ := os.ReadFile(resumePath)
	if !strings.Contains(string(data), "**new-carried-item**") {
		t.Errorf("resume.md missing new carried bullet\ncontent:\n%s", data)
	}
}

// TestApplyWrapBundle_CarriedRemove verifies an existing carried bullet is removed.
func TestApplyWrapBundle_CarriedRemove(t *testing.T) {
	iterBlock := BuildIterationBlock(10, "Carried remove", "Narrative.", "2026-04-25", "")
	commitContent := "chore: carried\n\nBody.\n"
	projectPath := t.TempDir()

	tool, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)

	bundle := buildMinimalBundle(10, iterBlock, commitContent)
	bundle.CarriedChanges.Remove = []BundleCarriedRemove{
		{Slug: "stale-item", SynthSHA256: fingerprintString("stale-item")},
	}
	result := invokeApply(t, tool, bundle, projectPath)
	if result.ErrorAtStep != "" {
		t.Fatalf("error at step %q: %+v", result.ErrorAtStep, result)
	}

	resumePath := filepath.Join(vaultPath, "Projects/myproject/agentctx/resume.md")
	data, _ := os.ReadFile(resumePath)
	if strings.Contains(string(data), "**stale-item**") {
		t.Errorf("resume.md still contains removed carried bullet\ncontent:\n%s", data)
	}
}

// TestApplyWrapBundle_SetCommitMsg verifies commit.msg is written.
func TestApplyWrapBundle_SetCommitMsg(t *testing.T) {
	iterBlock := BuildIterationBlock(10, "Commit msg", "Narrative.", "2026-04-25", "")
	commitContent := "feat: my commit message\n\nBody here.\n"
	projectPath := t.TempDir()

	tool, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)

	bundle := buildMinimalBundle(10, iterBlock, commitContent)
	result := invokeApply(t, tool, bundle, projectPath)
	if result.ErrorAtStep != "" {
		t.Fatalf("error at step %q: %+v", result.ErrorAtStep, result)
	}

	// Check vault copy.
	vaultCommit := filepath.Join(vaultPath, "Projects/myproject/agentctx/commit.msg")
	data, err := os.ReadFile(vaultCommit)
	if err != nil {
		t.Fatalf("vault commit.msg not written: %v", err)
	}
	if string(data) != commitContent {
		t.Errorf("vault commit.msg=%q, want %q", data, commitContent)
	}

	// Check project-root copy.
	projCommit := filepath.Join(projectPath, "commit.msg")
	data, err = os.ReadFile(projCommit)
	if err != nil {
		t.Fatalf("project-root commit.msg not written: %v", err)
	}
	if string(data) != commitContent {
		t.Errorf("project-root commit.msg=%q, want %q", data, commitContent)
	}
}

// TestApplyWrapBundle_MetricsWritten verifies that metrics lines are written
// for each bundle field.
func TestApplyWrapBundle_MetricsWritten(t *testing.T) {
	metricsHome := t.TempDir()
	t.Setenv("VIBE_VAULT_HOME", metricsHome)

	iterBlock := BuildIterationBlock(10, "Metrics", "Narrative.", "2026-04-25", "")
	commitContent := "chore: metrics\n\nBody.\n"
	projectPath := t.TempDir()

	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproject/agentctx/resume.md":     minimalResumeMd,
		"Projects/myproject/agentctx/iterations.md": minimalIterationsMd,
	})
	tool := NewApplyWrapBundleTool(cfg)

	bundle := buildMinimalBundle(10, iterBlock, commitContent)
	invokeApply(t, tool, bundle, projectPath)

	cacheDir := filepath.Join(metricsHome, ".cache", "vibe-vault")
	lines, err := wrapmetrics.ReadActiveLines(cacheDir)
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("no metrics lines written")
	}

	// Every line should have field, synth_sha256, apply_sha256.
	for i, raw := range lines {
		var m map[string]any
		if jsonErr := json.Unmarshal([]byte(raw), &m); jsonErr != nil {
			t.Fatalf("line %d unmarshal: %v", i, jsonErr)
		}
		for _, key := range []string{"field", "synth_sha256", "apply_sha256"} {
			if _, ok := m[key]; !ok {
				t.Errorf("line %d missing %q", i, key)
			}
		}
	}
}

// TestApplyWrapBundle_PartialFailure_StopsAtError verifies that when thread
// insert fails (e.g., slug already exists), apply stops and returns error_at_step.
func TestApplyWrapBundle_PartialFailure_StopsAtError(t *testing.T) {
	iterBlock := BuildIterationBlock(10, "Partial fail", "Narrative.", "2026-04-25", "")
	commitContent := "chore: partial\n\nBody.\n"
	projectPath := t.TempDir()

	// "existing-thread" already exists in minimalResumeMd — inserting it again
	// should fail (InsertSubsection returns error for duplicate slug).
	tool, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)

	bundle := buildMinimalBundle(10, iterBlock, commitContent)
	bundle.ResumeThreadBlocks = []BundleThreadBlock{
		{
			// This slug already exists → thread_insert will error.
			Position:    map[string]string{"mode": "top"},
			Slug:        "existing-thread",
			Body:        "Duplicate.",
			SynthSHA256: fingerprintString("existing-thread\x00Duplicate."),
		},
	}
	result := invokeApply(t, tool, bundle, projectPath)

	if result.ErrorAtStep == "" {
		t.Fatal("expected error_at_step to be set")
	}
	if !strings.Contains(result.ErrorAtStep, "thread_insert") {
		t.Errorf("error_at_step=%q, want it to contain 'thread_insert'", result.ErrorAtStep)
	}

	// append_iteration should have succeeded (it ran before thread_insert).
	appendDone := false
	for _, w := range result.AppliedWrites {
		if w.Step == "append_iteration" && w.Status == "ok" {
			appendDone = true
		}
	}
	if !appendDone {
		t.Error("append_iteration should have succeeded before the thread_insert failure")
	}

	// Verify iterations.md was written (partial success).
	iterPath := filepath.Join(vaultPath, "Projects/myproject/agentctx/iterations.md")
	data, _ := os.ReadFile(iterPath)
	if !strings.Contains(string(data), "### Iteration 10") {
		t.Error("iterations.md should have been written before thread_insert failed")
	}
}

// TestApplyWrapBundle_BundleMissing verifies an error when bundle is absent.
func TestApplyWrapBundle_BundleMissing(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	tool := NewApplyWrapBundleTool(cfg)
	t.Setenv("VIBE_VAULT_HOME", t.TempDir())

	_, err := tool.Handler(json.RawMessage(`{"project":"myproject"}`))
	if err == nil {
		t.Fatal("expected error for missing bundle")
	}
	if !strings.Contains(err.Error(), "bundle") {
		t.Errorf("error=%q, want mention of 'bundle'", err.Error())
	}
}

// TestApplyWrapBundle_AutoIncrementIteration verifies iteration=0 in the
// bundle header ("Iteration 0") triggers a rebuild with the auto-incremented
// number.
func TestApplyWrapBundle_AutoIncrementIteration(t *testing.T) {
	// Build a block with iteration=0 — this simulates a synthesize call where
	// the AI didn't know the iteration number.
	iterBlock := BuildIterationBlock(0, "Auto inc", "Narrative.", "2026-04-25", "")
	if !strings.Contains(iterBlock, "### Iteration 0 —") {
		t.Fatalf("expected 'Iteration 0' in block, got: %s", iterBlock)
	}
	commitContent := "chore: auto-inc\n\nBody.\n"
	projectPath := t.TempDir()

	tool, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)

	bundle := buildMinimalBundle(0, iterBlock, commitContent)
	result := invokeApply(t, tool, bundle, projectPath)
	if result.ErrorAtStep != "" {
		t.Fatalf("error at step %q: %+v", result.ErrorAtStep, result)
	}

	iterPath := filepath.Join(vaultPath, "Projects/myproject/agentctx/iterations.md")
	data, _ := os.ReadFile(iterPath)
	// Should have been rebuilt to Iteration 10 (9+1).
	if !strings.Contains(string(data), "### Iteration 10 —") {
		t.Errorf("expected auto-incremented iteration 10\ncontent:\n%s", data)
	}
	if strings.Contains(string(data), "### Iteration 0 —") {
		t.Errorf("iteration 0 should have been rewritten\ncontent:\n%s", data)
	}
}

// TestApplyWrapBundle_DriftSummaryNoSHA checks that when synth and apply
// SHA match (no edits), drift summary shows 0 drifted fields.
func TestApplyWrapBundle_DriftSummaryNoSHA(t *testing.T) {
	iterBlock := BuildIterationBlock(10, "Drift check", "Narrative.", "2026-04-25", "")
	commitContent := "chore: drift\n\nBody.\n"
	projectPath := t.TempDir()

	tool, _ := newApplyTool(t, minimalResumeMd, minimalIterationsMd)

	bundle := buildMinimalBundle(10, iterBlock, commitContent)
	result := invokeApply(t, tool, bundle, projectPath)
	if result.ErrorAtStep != "" {
		t.Fatalf("error at step %q", result.ErrorAtStep)
	}

	// When synth and apply SHA match, no fields should be drifted.
	if result.DriftSummary.DriftedFields != 0 {
		t.Errorf("DriftedFields=%d, want 0 (no edits between synth and apply)",
			result.DriftSummary.DriftedFields)
	}
}

// TestRebuildIterationBlock verifies that rebuildIterationBlock extracts title
// and narrative correctly and re-emits with the given iteration number.
func TestRebuildIterationBlock(t *testing.T) {
	original := BuildIterationBlock(0, "My Title", "My narrative text.", "2026-04-25", "")
	rebuilt, ok := rebuildIterationBlock(original, 42, "")
	if !ok {
		t.Fatalf("rebuildIterationBlock returned ok=false\noriginal:\n%s", original)
	}
	if !strings.Contains(rebuilt, "### Iteration 42 —") {
		t.Errorf("rebuilt block missing correct iteration number\ngot:\n%s", rebuilt)
	}
	if !strings.Contains(rebuilt, "My Title") {
		t.Errorf("rebuilt block missing title\ngot:\n%s", rebuilt)
	}
	if !strings.Contains(rebuilt, "My narrative text.") {
		t.Errorf("rebuilt block missing narrative\ngot:\n%s", rebuilt)
	}
	if strings.Contains(rebuilt, "### Iteration 0 —") {
		t.Errorf("rebuilt block still contains iteration 0\ngot:\n%s", rebuilt)
	}
}

// TestBuildIterationBlock_Basic checks the exported BuildIterationBlock function.
func TestBuildIterationBlock_Basic(t *testing.T) {
	block := BuildIterationBlock(5, "Test Phase", "Narrative body.", "2026-04-25", "")
	wantParts := []string{
		"### Iteration 5 — Test Phase (2026-04-25)",
		"Narrative body.",
	}
	for _, want := range wantParts {
		if !strings.Contains(block, want) {
			t.Errorf("block missing %q\nfull:\n%s", want, block)
		}
	}
}

// TestApplyWrapBundle_MetricFilePathInResult verifies the result includes the
// metric file path.
func TestApplyWrapBundle_MetricFilePathInResult(t *testing.T) {
	metricsHome := t.TempDir()
	t.Setenv("VIBE_VAULT_HOME", metricsHome)

	iterBlock := BuildIterationBlock(10, "Metric path", "Narrative.", "2026-04-25", "")
	projectPath := t.TempDir()
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproject/agentctx/resume.md":     minimalResumeMd,
		"Projects/myproject/agentctx/iterations.md": minimalIterationsMd,
	})
	tool := NewApplyWrapBundleTool(cfg)

	bundle := buildMinimalBundle(10, iterBlock, "chore: x\n\nBody.\n")
	bundleJSON, _ := json.Marshal(bundle)
	params, _ := json.Marshal(map[string]any{
		"project":      "myproject",
		"project_path": projectPath,
		"bundle":       json.RawMessage(bundleJSON),
	})
	out, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	var result applyResult
	json.Unmarshal([]byte(out), &result)

	if result.MetricFile == "" {
		t.Error("metric_file should be non-empty in result")
	}
	wantSuffix := wrapmetrics.ActiveFile
	if !strings.HasSuffix(result.MetricFile, wantSuffix) {
		t.Errorf("metric_file=%q should end with %q", result.MetricFile, wantSuffix)
	}
	_ = fmt.Sprintf("metric_file: %s", result.MetricFile) // suppress unused warning
}
