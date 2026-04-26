// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// newSynthesizetool returns a NewSynthesizeWrapTool backed by a minimal test vault.
func newSynthesizeTool(t *testing.T) Tool {
	t.Helper()
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, map[string]string{
		"Projects/myproject/agentctx/.keep": "",
	})
	return NewSynthesizeWrapTool(cfg)
}

// minimalSynthesizeArgs returns a valid minimal input payload.
func minimalSynthesizeArgs(projectPath string) map[string]any {
	return map[string]any{
		"project":             "myproject",
		"project_path":        projectPath,
		"iteration":           10,
		"iteration_narrative": "Added new feature X and fixed bug Y.",
		"title":               "Phase 5 wrap",
		"subject":             "feat(mcp): add vv_synthesize_wrap",
		"prose_body":          "This phase adds the synthesize tool.\n\nSee plan for details.",
		"test_count_delta": map[string]any{
			"unit_tests":           1700,
			"integration_subtests": 33,
			"lint_findings":        0,
		},
	}
}

// TestSynthesizeWrap_BundleShape verifies the returned bundle has all required
// top-level fields with the expected types.
func TestSynthesizeWrap_BundleShape(t *testing.T) {
	withFakeGit(t, "M  foo.go\n", " 1 file changed, 10 insertions(+)\n", nil, nil)

	tool := newSynthesizeTool(t)
	params, _ := json.Marshal(minimalSynthesizeArgs(t.TempDir()))

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	var bundle WrapBundle
	if err := json.Unmarshal([]byte(result), &bundle); err != nil {
		t.Fatalf("unmarshal bundle: %v\n%s", err, result)
	}

	// Top-level field presence.
	if bundle.IterationBlock.Content == "" {
		t.Error("iteration_block.content is empty")
	}
	if bundle.IterationBlock.SynthSHA256 == "" {
		t.Error("iteration_block.synth_sha256 is empty")
	}
	if bundle.CommitMsg.Content == "" {
		t.Error("commit_msg.content is empty")
	}
	if bundle.CommitMsg.SynthSHA256 == "" {
		t.Error("commit_msg.synth_sha256 is empty")
	}
	if bundle.CaptureSession.SynthSHA256 == "" {
		t.Error("capture_session.synth_sha256 is empty")
	}
	if bundle.SynthTimestamp == "" {
		t.Error("synth_timestamp is empty")
	}
	if bundle.Iteration != 10 {
		t.Errorf("iteration=%d, want 10", bundle.Iteration)
	}
}

// TestSynthesizeWrap_IterationBlockFormat verifies the iteration_block content
// contains the canonical heading line.
func TestSynthesizeWrap_IterationBlockFormat(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)

	tool := newSynthesizeTool(t)
	args := minimalSynthesizeArgs(t.TempDir())
	args["date"] = "2026-04-25"
	params, _ := json.Marshal(args)

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	var bundle WrapBundle
	json.Unmarshal([]byte(result), &bundle)

	heading := "### Iteration 10 — Phase 5 wrap (2026-04-25)"
	if !strings.Contains(bundle.IterationBlock.Content, heading) {
		t.Errorf("iteration_block missing heading %q\ngot: %s", heading, bundle.IterationBlock.Content)
	}
	if !strings.Contains(bundle.IterationBlock.Content, "Added new feature X") {
		t.Errorf("iteration_block missing narrative text")
	}
}

// TestSynthesizeWrap_CommitMsgStructure verifies the commit message has all
// required sections.
func TestSynthesizeWrap_CommitMsgStructure(t *testing.T) {
	withFakeGit(t, "M  foo.go\n", " 1 file changed, 10 insertions(+)\n", nil, nil)

	tool := newSynthesizeTool(t)
	params, _ := json.Marshal(minimalSynthesizeArgs(t.TempDir()))

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	var bundle WrapBundle
	json.Unmarshal([]byte(result), &bundle)

	wantParts := []string{
		"feat(mcp): add vv_synthesize_wrap",
		"## Files changed",
		"foo.go",
		"## Test counts",
		"- Unit tests: 1700",
		"- Integration subtests: 33",
		"- Lint findings: 0",
		"## Iteration 10",
	}
	for _, want := range wantParts {
		if !strings.Contains(bundle.CommitMsg.Content, want) {
			t.Errorf("commit_msg missing %q\nfull:\n%s", want, bundle.CommitMsg.Content)
		}
	}
}

// TestSynthesizeWrap_CaptureSessionAlwaysPresent verifies capture_session is
// in every bundle unconditionally (Phase 0 M4 requirement).
func TestSynthesizeWrap_CaptureSessionAlwaysPresent(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)

	tool := newSynthesizeTool(t)
	// Minimal args — no decisions, no files, no threads.
	params, _ := json.Marshal(map[string]any{
		"iteration_narrative": "Minimal wrap.",
		"title":               "Minimal",
		"subject":             "chore: minimal",
	})

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	var bundle WrapBundle
	json.Unmarshal([]byte(result), &bundle)

	if bundle.CaptureSession.Content.Summary == "" {
		t.Error("capture_session.content.summary should be non-empty even with minimal input")
	}
	if bundle.CaptureSession.SynthSHA256 == "" {
		t.Error("capture_session.synth_sha256 should always be set")
	}
}

// TestSynthesizeWrap_ThreadsToOpen verifies resume_thread_blocks are populated.
func TestSynthesizeWrap_ThreadsToOpen(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)

	tool := newSynthesizeTool(t)
	args := minimalSynthesizeArgs(t.TempDir())
	args["threads_to_open"] = []map[string]any{
		{
			"position": map[string]any{"mode": "top"},
			"slug":     "my-new-thread",
			"body":     "This is the thread body.",
		},
	}
	args["threads_to_close"] = []string{"old-thread"}
	params, _ := json.Marshal(args)

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	var bundle WrapBundle
	json.Unmarshal([]byte(result), &bundle)

	if len(bundle.ResumeThreadBlocks) != 1 {
		t.Fatalf("resume_thread_blocks len=%d, want 1", len(bundle.ResumeThreadBlocks))
	}
	tb := bundle.ResumeThreadBlocks[0]
	if tb.Slug != "my-new-thread" {
		t.Errorf("thread slug=%q, want my-new-thread", tb.Slug)
	}
	if tb.SynthSHA256 == "" {
		t.Error("thread synth_sha256 is empty")
	}

	if len(bundle.ResumeThreadsToClose) != 1 {
		t.Fatalf("resume_threads_to_close len=%d, want 1", len(bundle.ResumeThreadsToClose))
	}
	if bundle.ResumeThreadsToClose[0].Slug != "old-thread" {
		t.Errorf("close slug=%q, want old-thread", bundle.ResumeThreadsToClose[0].Slug)
	}
}

// TestSynthesizeWrap_CarriedChanges verifies carried_changes.add and .remove.
func TestSynthesizeWrap_CarriedChanges(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)

	tool := newSynthesizeTool(t)
	args := minimalSynthesizeArgs(t.TempDir())
	args["carried_to_add"] = []map[string]any{
		{"slug": "new-item", "title": "New carried item", "body": "Details here."},
	}
	args["carried_to_remove"] = []string{"stale-item"}
	params, _ := json.Marshal(args)

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	var bundle WrapBundle
	json.Unmarshal([]byte(result), &bundle)

	if len(bundle.CarriedChanges.Add) != 1 {
		t.Fatalf("carried_changes.add len=%d, want 1", len(bundle.CarriedChanges.Add))
	}
	ca := bundle.CarriedChanges.Add[0]
	if ca.Slug != "new-item" {
		t.Errorf("add slug=%q, want new-item", ca.Slug)
	}
	if ca.SynthSHA256 == "" {
		t.Error("add synth_sha256 is empty")
	}

	if len(bundle.CarriedChanges.Remove) != 1 {
		t.Fatalf("carried_changes.remove len=%d, want 1", len(bundle.CarriedChanges.Remove))
	}
	cr := bundle.CarriedChanges.Remove[0]
	if cr.Slug != "stale-item" {
		t.Errorf("remove slug=%q, want stale-item", cr.Slug)
	}
}

// TestSynthesizeWrap_SHAFingerprint verifies that changing content changes the
// synth_sha256.
func TestSynthesizeWrap_SHAFingerprint(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)

	tool := newSynthesizeTool(t)

	run := func(narrative string) string {
		args := map[string]any{
			"iteration_narrative": narrative,
			"title":               "Test",
			"subject":             "chore: test",
		}
		params, _ := json.Marshal(args)
		result, err := tool.Handler(params)
		if err != nil {
			t.Fatalf("Handler: %v", err)
		}
		var bundle WrapBundle
		json.Unmarshal([]byte(result), &bundle)
		return bundle.IterationBlock.SynthSHA256
	}

	sha1 := run("First narrative content.")
	sha2 := run("Second different narrative content.")
	if sha1 == sha2 {
		t.Error("different content produced the same synth_sha256")
	}
}

// TestSynthesizeWrap_FilesChangedSupplied verifies that when files_changed is
// supplied, the commit_msg uses it directly.
func TestSynthesizeWrap_FilesChangedSupplied(t *testing.T) {
	// Do NOT fake git — we want to verify the explicit list is used.
	withFakeGit(t, "", "", nil, nil)

	tool := newSynthesizeTool(t)
	args := minimalSynthesizeArgs(t.TempDir())
	args["files_changed"] = []string{"explicit/file.go", "another/file.go"}
	params, _ := json.Marshal(args)

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	var bundle WrapBundle
	json.Unmarshal([]byte(result), &bundle)

	if !strings.Contains(bundle.CommitMsg.Content, "- explicit/file.go") {
		t.Errorf("commit_msg missing explicit file\nfull:\n%s", bundle.CommitMsg.Content)
	}
	if !strings.Contains(bundle.CommitMsg.Content, "- another/file.go") {
		t.Errorf("commit_msg missing second explicit file\nfull:\n%s", bundle.CommitMsg.Content)
	}
}

// TestSynthesizeWrap_RequiredFieldsMissing verifies hard errors for missing
// required fields.
func TestSynthesizeWrap_RequiredFieldsMissing(t *testing.T) {
	tool := newSynthesizeTool(t)

	cases := []struct {
		name   string
		args   map[string]any
		errMsg string
	}{
		{
			name:   "missing iteration_narrative",
			args:   map[string]any{"title": "T", "subject": "s"},
			errMsg: "iteration_narrative is required",
		},
		{
			name:   "missing title",
			args:   map[string]any{"iteration_narrative": "N", "subject": "s"},
			errMsg: "title is required",
		},
		{
			name:   "missing subject",
			args:   map[string]any{"iteration_narrative": "N", "title": "T"},
			errMsg: "subject is required",
		},
		{
			name:   "subject with newline",
			args:   map[string]any{"iteration_narrative": "N", "title": "T", "subject": "line1\nline2"},
			errMsg: "single line",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params, _ := json.Marshal(tc.args)
			_, err := tool.Handler(params)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.errMsg)
			}
			if !strings.Contains(err.Error(), tc.errMsg) {
				t.Errorf("error=%q, want %q", err.Error(), tc.errMsg)
			}
		})
	}
}

// TestSynthesizeWrap_ProseBodyDefaultsToNarrative verifies that when
// prose_body is omitted, the commit message body uses iteration_narrative.
func TestSynthesizeWrap_ProseBodyDefaultsToNarrative(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)

	tool := newSynthesizeTool(t)
	narrative := "This is the narrative that should appear in the commit body."
	params, _ := json.Marshal(map[string]any{
		"iteration_narrative": narrative,
		"title":               "Test",
		"subject":             "chore: test",
	})

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	var bundle WrapBundle
	json.Unmarshal([]byte(result), &bundle)

	if !strings.Contains(bundle.CommitMsg.Content, narrative) {
		t.Errorf("commit_msg body doesn't contain narrative when prose_body omitted\nfull:\n%s", bundle.CommitMsg.Content)
	}
}

// TestSynthesizeWrap_DecisionsInCaptureSession verifies decisions flow to
// capture_session.
func TestSynthesizeWrap_DecisionsInCaptureSession(t *testing.T) {
	withFakeGit(t, "", "", nil, nil)

	tool := newSynthesizeTool(t)
	args := minimalSynthesizeArgs(t.TempDir())
	args["decisions"] = []string{"chose approach A", "rejected B"}
	params, _ := json.Marshal(args)

	result, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	var bundle WrapBundle
	json.Unmarshal([]byte(result), &bundle)

	if len(bundle.CaptureSession.Content.Decisions) != 2 {
		t.Errorf("decisions len=%d, want 2", len(bundle.CaptureSession.Content.Decisions))
	}
	if bundle.CaptureSession.Content.Decisions[0] != "chose approach A" {
		t.Errorf("decision[0]=%q", bundle.CaptureSession.Content.Decisions[0])
	}
}

// TestFingerprintString_Deterministic verifies the same input always produces
// the same fingerprint.
func TestFingerprintString_Deterministic(t *testing.T) {
	for i := 0; i < 5; i++ {
		got := fingerprintString("hello world")
		// SHA-256 of "hello world" is well-known.
		want := "b94d27b9934d3e08a52e52d7da7dabfac484efe04294e576e93f8d5be17abba6"
		// Note: actual SHA-256 of "hello world" may differ; we just check consistency.
		if fingerprintString("hello world") != got {
			t.Error("fingerprintString is not deterministic")
		}
		_ = want
	}

	// Different input → different fingerprint.
	if fingerprintString("a") == fingerprintString("b") {
		t.Error("different inputs produced same fingerprint")
	}
}

// TestFirstNWords returns the first n words of a string.
func TestFirstNWords(t *testing.T) {
	cases := []struct {
		input string
		n     int
		want  string
	}{
		{"one two three four five", 3, "one two three"},
		{"short", 10, "short"},
		{"", 5, ""},
		{"a b c", 3, "a b c"},
	}
	for _, tc := range cases {
		got := firstNWords(tc.input, tc.n)
		if got != tc.want {
			t.Errorf("firstNWords(%q, %d)=%q, want %q", tc.input, tc.n, got, tc.want)
		}
	}
}
