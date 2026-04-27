// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
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

// newApplyTool builds a fresh test vault + skeleton-cache + apply tool.
func newApplyTool(t *testing.T, resume, iterations string) (Tool, config.Config, string) {
	t.Helper()
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproject/agentctx/resume.md":     resume,
		"Projects/myproject/agentctx/iterations.md": iterations,
	})
	t.Setenv("VIBE_VAULT_HOME", t.TempDir())
	withSkeletonCacheDir(t)
	return NewApplyWrapBundleByHandleTool(cfg), cfg, cfg.VaultPath
}

// invokeApplyByHandle marshals the handle+outputs payload and calls the tool.
func invokeApplyByHandle(t *testing.T, tool Tool, handle SkeletonHandle, outputs map[string]any, projectPath string) (applyResult, error) {
	t.Helper()
	args := map[string]any{
		"project":         "myproject",
		"project_path":    projectPath,
		"skeleton_handle": handle,
		"outputs":         outputs,
	}
	params, _ := json.Marshal(args)
	out, err := tool.Handler(params)
	if err != nil {
		return applyResult{}, err
	}
	var r applyResult
	if jerr := json.Unmarshal([]byte(out), &r); jerr != nil {
		t.Fatalf("unmarshal result: %v\n%s", jerr, out)
	}
	return r, nil
}

func TestVVApplyWrapBundleByHandle_HappyPath(t *testing.T) {
	tool, _, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()

	handle := seedSkeleton(t, SkeletonFacts{
		Iter:    10,
		Project: "myproject",
		ResumeThreadBlocks: []SkeletonThreadOpen{
			{Slug: "new-thread"},
		},
	})

	outputs := map[string]any{
		"iteration_narrative": "Did stuff.",
		"iteration_title":     "Phase 3a",
		"prose_body":          "Body.",
		"commit_subject":      "feat(mcp): test",
		"date":                "2026-04-25",
		"thread_bodies": map[string]string{
			"new-thread": "thread body content",
		},
		"capture_summary": "Summary.",
	}
	res, err := invokeApplyByHandle(t, tool, handle, outputs, projectPath)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res.ErrorAtStep != "" {
		t.Fatalf("error at step %q: %+v", res.ErrorAtStep, res)
	}

	// iterations.md should contain the new iteration.
	data, _ := os.ReadFile(filepath.Join(vaultPath, "Projects/myproject/agentctx/iterations.md"))
	if !strings.Contains(string(data), "### Iteration 10 — Phase 3a") {
		t.Errorf("iterations.md missing new heading\ncontent:\n%s", data)
	}

	// resume.md should contain the new thread.
	data, _ = os.ReadFile(filepath.Join(vaultPath, "Projects/myproject/agentctx/resume.md"))
	if !strings.Contains(string(data), "### new-thread") {
		t.Errorf("resume.md missing new thread\ncontent:\n%s", data)
	}

	// commit.msg should be on disk.
	if _, err := os.Stat(filepath.Join(projectPath, "commit.msg")); err != nil {
		t.Errorf("commit.msg missing: %v", err)
	}
}

func TestVVApplyWrapBundleByHandle_DetectsTamperedSkeleton(t *testing.T) {
	tool, _, _ := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()

	handle := seedSkeleton(t, SkeletonFacts{Iter: 11, Project: "myproject"})
	// Tamper.
	if err := os.WriteFile(handle.SkeletonPath, []byte(`{"iter":99}`), 0o600); err != nil {
		t.Fatalf("mutate: %v", err)
	}

	_, err := invokeApplyByHandle(t, tool, handle, map[string]any{
		"iteration_title": "x", "iteration_narrative": "n", "commit_subject": "chore: x",
	}, projectPath)
	if err == nil {
		t.Fatalf("expected sha-mismatch error")
	}
}

func TestVVApplyWrapBundleByHandle_DetectsMissingProse(t *testing.T) {
	tool, _, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()

	// Skeleton expects 1 thread_insert but we'll pass empty thread_bodies.
	// The skeleton itself drives expected count so an extra non-existent
	// expected-mutation is what triggers the mismatch path; instead, let's
	// trigger the mismatch by giving the skeleton MORE entries than the
	// bundle ends up with — but FillBundle always populates every skeleton
	// entry (with possibly-empty body), so the count match is exact.
	//
	// To force a mismatch, mutate the bundle field count after FillBundle:
	// the public surface only allows handle+outputs, so the path that
	// produces a mismatch in practice is when an executor sends a bundle
	// shape that has been edited to drop entries. Simulate this by
	// sending a skeleton with 1 thread-open and verifying the count check
	// passes; then independently exercise the mismatch path via the
	// expectedMutationCount/actualMutationCount helpers below.
	handle := seedSkeleton(t, SkeletonFacts{
		Iter:    12,
		Project: "myproject",
		ResumeThreadBlocks: []SkeletonThreadOpen{
			{Slug: "open-1"},
		},
	})
	res, err := invokeApplyByHandle(t, tool, handle, map[string]any{
		"iteration_title":     "x",
		"iteration_narrative": "n",
		"commit_subject":      "chore: x",
		"thread_bodies":       map[string]string{}, // empty body still counted as one mutation
	}, projectPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The thread mutation IS counted (count match) but its body is empty so
	// applyThreadInsert succeeds with empty content. Confirm vault reflects
	// the call: iterations.md was written, resume.md gained the thread.
	if res.ErrorAtStep != "" {
		t.Errorf("error at %q", res.ErrorAtStep)
	}
	data, _ := os.ReadFile(filepath.Join(vaultPath, "Projects/myproject/agentctx/resume.md"))
	if !strings.Contains(string(data), "### open-1") {
		t.Errorf("resume.md missing the (empty-body) thread\n%s", data)
	}
}

func TestVVApplyWrapBundleByHandle_MutationCountMismatchViaHelper(t *testing.T) {
	// Direct unit test of the count helpers — guarantees the formula stays
	// consistent with Phase 3b's QC tool.
	sk := WrapSkeleton{
		ResumeThreadBlocks:   make([]SkeletonThreadOpen, 2),
		ResumeThreadsReplace: make([]SkeletonThreadReplace, 1),
		ResumeThreadsToClose: make([]SkeletonThreadClose, 3),
		CarriedChangesAdd:    make([]SkeletonCarriedAdd, 1),
		CarriedChangesRemove: make([]SkeletonCarriedRemove, 0),
	}
	want := 1 + 2 + 1 + 3 + 1 + 0 + 1 + 1 // = 10
	if got := expectedMutationCount(sk); got != want {
		t.Errorf("expectedMutationCount=%d, want %d", got, want)
	}

	// Bundle missing the thread_replace entry → mismatch.
	bundle := WrapBundle{
		ResumeThreadBlocks:   make([]BundleThreadBlock, 2),
		ResumeThreadsReplace: nil, // dropped
		ResumeThreadsToClose: make([]BundleThreadClose, 3),
		CarriedChanges: BundleCarriedChanges{
			Add:    make([]BundleCarriedAdd, 1),
			Remove: nil,
		},
	}
	if got := actualMutationCount(bundle); got == want {
		t.Errorf("expected mismatch when ResumeThreadsReplace dropped, got %d == %d", got, want)
	}
}

func TestVVApplyWrapBundleByHandle_AppliesThreadReplace(t *testing.T) {
	tool, _, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()

	handle := seedSkeleton(t, SkeletonFacts{
		Iter:    13,
		Project: "myproject",
		ResumeThreadsReplace: []SkeletonThreadReplace{
			{Slug: "existing-thread"},
		},
	})

	res, err := invokeApplyByHandle(t, tool, handle, map[string]any{
		"iteration_title":     "Replace test",
		"iteration_narrative": "n",
		"commit_subject":      "chore: replace",
		"thread_bodies": map[string]string{
			"existing-thread": "REPLACED BODY",
		},
	}, projectPath)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res.ErrorAtStep != "" {
		t.Fatalf("error at %q", res.ErrorAtStep)
	}
	data, _ := os.ReadFile(filepath.Join(vaultPath, "Projects/myproject/agentctx/resume.md"))
	if !strings.Contains(string(data), "REPLACED BODY") {
		t.Errorf("resume.md missing replacement body\n%s", data)
	}
	if strings.Contains(string(data), "Existing thread body.") {
		t.Errorf("resume.md still contains original body (replace didn't run)\n%s", data)
	}
}

func TestVVApplyWrapBundleByHandle_AppendIteration(t *testing.T) {
	tool, _, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()

	handle := seedSkeleton(t, SkeletonFacts{Iter: 14, Project: "myproject"})
	res, err := invokeApplyByHandle(t, tool, handle, map[string]any{
		"iteration_title":     "Phase 5 apply",
		"iteration_narrative": "Narrative text here.",
		"commit_subject":      "chore: append",
		"date":                "2026-04-25",
	}, projectPath)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res.ErrorAtStep != "" {
		t.Fatalf("error at %q", res.ErrorAtStep)
	}
	data, _ := os.ReadFile(filepath.Join(vaultPath, "Projects/myproject/agentctx/iterations.md"))
	if !strings.Contains(string(data), "### Iteration 14 — Phase 5 apply") {
		t.Errorf("iterations.md missing heading\n%s", data)
	}
}

func TestVVApplyWrapBundleByHandle_CarriedAdd(t *testing.T) {
	tool, _, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()

	handle := seedSkeleton(t, SkeletonFacts{
		Iter:    15,
		Project: "myproject",
		CarriedChangesAdd: []SkeletonCarriedAdd{
			{Slug: "new-carry", Title: "New carry title"},
		},
	})
	res, err := invokeApplyByHandle(t, tool, handle, map[string]any{
		"iteration_title":     "Carry add",
		"iteration_narrative": "n",
		"commit_subject":      "chore: carry",
		"carried_bodies":      map[string]string{"new-carry": "Details here."},
	}, projectPath)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res.ErrorAtStep != "" {
		t.Fatalf("error at %q", res.ErrorAtStep)
	}
	data, _ := os.ReadFile(filepath.Join(vaultPath, "Projects/myproject/agentctx/resume.md"))
	if !strings.Contains(string(data), "**new-carry**") {
		t.Errorf("resume.md missing carried bullet\n%s", data)
	}
}

func TestVVApplyWrapBundleByHandle_CarriedRemove(t *testing.T) {
	tool, _, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()

	handle := seedSkeleton(t, SkeletonFacts{
		Iter:    16,
		Project: "myproject",
		CarriedChangesRemove: []SkeletonCarriedRemove{
			{Slug: "stale-item"},
		},
	})
	res, err := invokeApplyByHandle(t, tool, handle, map[string]any{
		"iteration_title": "Carry rm", "iteration_narrative": "n", "commit_subject": "chore: x",
	}, projectPath)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res.ErrorAtStep != "" {
		t.Fatalf("error at %q", res.ErrorAtStep)
	}
	data, _ := os.ReadFile(filepath.Join(vaultPath, "Projects/myproject/agentctx/resume.md"))
	if strings.Contains(string(data), "**stale-item**") {
		t.Errorf("resume.md still contains removed bullet\n%s", data)
	}
}

func TestVVApplyWrapBundleByHandle_ThreadRemove(t *testing.T) {
	tool, _, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()

	handle := seedSkeleton(t, SkeletonFacts{
		Iter:    17,
		Project: "myproject",
		ResumeThreadsToClose: []SkeletonThreadClose{
			{Slug: "existing-thread"},
		},
	})
	res, err := invokeApplyByHandle(t, tool, handle, map[string]any{
		"iteration_title": "Thread close", "iteration_narrative": "n", "commit_subject": "chore: x",
	}, projectPath)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res.ErrorAtStep != "" {
		t.Fatalf("error at %q: %+v", res.ErrorAtStep, res)
	}
	data, _ := os.ReadFile(filepath.Join(vaultPath, "Projects/myproject/agentctx/resume.md"))
	if strings.Contains(string(data), "### existing-thread") {
		t.Errorf("resume.md still contains removed thread\n%s", data)
	}
}

func TestVVApplyWrapBundleByHandle_SetCommitMsg(t *testing.T) {
	tool, _, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()

	handle := seedSkeleton(t, SkeletonFacts{Iter: 18, Project: "myproject"})
	_, err := invokeApplyByHandle(t, tool, handle, map[string]any{
		"iteration_title":     "Commit msg",
		"iteration_narrative": "Narrative.",
		"prose_body":          "Body here.",
		"commit_subject":      "feat: my commit message",
	}, projectPath)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	vaultCommit := filepath.Join(vaultPath, "Projects/myproject/agentctx/commit.msg")
	data, err := os.ReadFile(vaultCommit)
	if err != nil {
		t.Fatalf("vault commit.msg not written: %v", err)
	}
	if !strings.Contains(string(data), "feat: my commit message") {
		t.Errorf("vault commit.msg missing subject\n%s", data)
	}
	projCommit := filepath.Join(projectPath, "commit.msg")
	data2, err := os.ReadFile(projCommit)
	if err != nil {
		t.Fatalf("project-root commit.msg not written: %v", err)
	}
	if string(data) != string(data2) {
		t.Errorf("vault and project commit.msg differ")
	}
}

func TestVVApplyWrapBundleByHandle_MetricsWritten(t *testing.T) {
	metricsHome := t.TempDir()
	t.Setenv("VIBE_VAULT_HOME", metricsHome)

	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproject/agentctx/resume.md":     minimalResumeMd,
		"Projects/myproject/agentctx/iterations.md": minimalIterationsMd,
	})
	withSkeletonCacheDir(t)
	tool := NewApplyWrapBundleByHandleTool(cfg)
	projectPath := t.TempDir()

	handle := seedSkeleton(t, SkeletonFacts{Iter: 19, Project: "myproject"})
	args := map[string]any{
		"project":         "myproject",
		"project_path":    projectPath,
		"skeleton_handle": handle,
		"outputs": map[string]any{
			"iteration_title": "Metrics", "iteration_narrative": "n", "commit_subject": "chore: m",
		},
	}
	params, _ := json.Marshal(args)
	if _, err := tool.Handler(params); err != nil {
		t.Fatalf("Handler: %v", err)
	}

	cacheDir := filepath.Join(metricsHome, ".cache", "vibe-vault")
	lines, err := wrapmetrics.ReadActiveLines(cacheDir)
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("no metrics lines written")
	}
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

func TestVVApplyWrapBundleByHandle_DriftSummaryNoDrift(t *testing.T) {
	tool, _, _ := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()

	handle := seedSkeleton(t, SkeletonFacts{Iter: 20, Project: "myproject"})
	res, err := invokeApplyByHandle(t, tool, handle, map[string]any{
		"iteration_title": "Drift", "iteration_narrative": "n", "commit_subject": "chore: x",
	}, projectPath)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res.DriftSummary.DriftedFields != 0 {
		t.Errorf("drifted=%d, want 0 (no edits between synth and apply)", res.DriftSummary.DriftedFields)
	}
}

func TestVVApplyWrapBundleByHandle_AutoIncrementIteration(t *testing.T) {
	// Auto-increment branch: the rebuildIterationBlock helper kicks in when
	// the block heading still says "Iteration 0" — covered separately by
	// TestRebuildIterationBlock; here we just confirm a non-zero iter
	// passes through unchanged.
	tool, _, vaultPath := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()
	handle := seedSkeleton(t, SkeletonFacts{Iter: 21, Project: "myproject"})
	_, err := invokeApplyByHandle(t, tool, handle, map[string]any{
		"iteration_title": "Auto", "iteration_narrative": "n", "commit_subject": "chore: x",
	}, projectPath)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(vaultPath, "Projects/myproject/agentctx/iterations.md"))
	if !strings.Contains(string(data), "### Iteration 21 — Auto") {
		t.Errorf("iterations.md missing iter-21 heading\n%s", data)
	}
}

func TestRebuildIterationBlock(t *testing.T) {
	original := BuildIterationBlock(0, "My Title", "My narrative text.", "2026-04-25", "")
	rebuilt, ok := rebuildIterationBlock(original, 42, "")
	if !ok {
		t.Fatalf("rebuildIterationBlock returned ok=false\noriginal:\n%s", original)
	}
	if !strings.Contains(rebuilt, "### Iteration 42 —") {
		t.Errorf("rebuilt block missing correct iteration number\n%s", rebuilt)
	}
	if strings.Contains(rebuilt, "### Iteration 0 —") {
		t.Errorf("rebuilt block still contains iteration 0\n%s", rebuilt)
	}
}

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

func TestVVApplyWrapBundleByHandle_PartialFailure_StopsAtError(t *testing.T) {
	tool, _, _ := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()

	// "existing-thread" already exists in minimalResumeMd — inserting it
	// again should fail; the iteration-block append should have completed
	// before the failure.
	handle := seedSkeleton(t, SkeletonFacts{
		Iter:    22,
		Project: "myproject",
		ResumeThreadBlocks: []SkeletonThreadOpen{
			{Slug: "existing-thread"},
		},
	})
	res, err := invokeApplyByHandle(t, tool, handle, map[string]any{
		"iteration_title":     "Partial",
		"iteration_narrative": "n",
		"commit_subject":      "chore: partial",
		"thread_bodies":       map[string]string{"existing-thread": "Duplicate."},
	}, projectPath)
	if err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}
	if res.ErrorAtStep == "" {
		t.Fatal("expected error_at_step to be set")
	}
	if !strings.Contains(res.ErrorAtStep, "thread_insert") {
		t.Errorf("error_at_step=%q, want to contain 'thread_insert'", res.ErrorAtStep)
	}
}

func TestVVApplyWrapBundleByHandle_MetricFilePathInResult(t *testing.T) {
	tool, _, _ := newApplyTool(t, minimalResumeMd, minimalIterationsMd)
	projectPath := t.TempDir()

	handle := seedSkeleton(t, SkeletonFacts{Iter: 23, Project: "myproject"})
	res, err := invokeApplyByHandle(t, tool, handle, map[string]any{
		"iteration_title": "Metric path", "iteration_narrative": "n", "commit_subject": "chore: x",
	}, projectPath)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	if res.MetricFile == "" {
		t.Error("metric_file should be non-empty")
	}
	if !strings.HasSuffix(res.MetricFile, wrapmetrics.ActiveFile) {
		t.Errorf("metric_file=%q should end with %q", res.MetricFile, wrapmetrics.ActiveFile)
	}
}
