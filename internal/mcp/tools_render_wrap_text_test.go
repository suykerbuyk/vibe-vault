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
	"github.com/suykerbuyk/vibe-vault/internal/llm"
)

// scriptedProvider is a deterministic Provider stub used by the
// vv_render_wrap_text tests. The recorded last system / user prompts
// let tests assert prompt-template wiring without needing a live LLM.
type scriptedProvider struct {
	response   string
	err        error
	lastSystem string
	lastUser   string
	lastModel  string
	calls      int
}

func (s *scriptedProvider) Name() string { return "scripted" }

func (s *scriptedProvider) ChatCompletion(_ context.Context, req llm.Request) (*llm.Response, error) {
	s.calls++
	s.lastSystem = req.System
	s.lastUser = req.UserPrompt
	s.lastModel = req.Model
	if s.err != nil {
		return nil, s.err
	}
	return &llm.Response{Content: s.response}, nil
}

// withWrapRenderProviderFactory installs a test-side factory that
// returns the supplied provider for any tier label, then restores the
// production factory on cleanup.
func withWrapRenderProviderFactory(t *testing.T, p llm.Provider) {
	t.Helper()
	prev := wrapRenderProviderFactory
	wrapRenderProviderFactory = func(_ config.Config, _ string) (llm.Provider, error) {
		return p, nil
	}
	t.Cleanup(func() { wrapRenderProviderFactory = prev })
}

// configWithTiers returns a vault config with [wrap.tiers] populated
// so the production factory's tier resolution (when used) succeeds.
func configWithTiers(t *testing.T) config.Config {
	t.Helper()
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	cfg.Wrap.Tiers = map[string]string{
		"sonnet": "anthropic:claude-sonnet-4-6",
		"opus":   "anthropic:claude-opus-4-7",
	}
	return cfg
}

func sampleRenderArgs(t *testing.T, kind string) json.RawMessage {
	t.Helper()
	args := map[string]any{
		"kind":         kind,
		"tier":         "sonnet",
		"project_name": "myproj",
		"iter_state": map[string]any{
			"iter_n":               42,
			"branch":               "main",
			"last_iter_anchor_sha": "abc123",
			"commits_since_last_iter": []map[string]string{
				{"sha": "deadbeef", "subject": "feat: ship"},
			},
			"files_changed": []string{"main.go"},
			"task_deltas": map[string]any{
				"added":     []string{},
				"retired":   []string{"oldtask"},
				"cancelled": []string{},
			},
			"test_counts": map[string]int{
				"unit": 1974, "integration": 31, "lint": 0,
			},
		},
		"project_context": map[string]any{
			"resume_state":      "## Current State\n- Iterations: 168",
			"recent_iterations": "### Iteration 41 — Prior\n\nbody.",
			"open_threads":      []string{"slug-a", "slug-b"},
			"friction_trends":   map[string]any{"alert": false},
		},
	}
	out, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return out
}

func TestRenderWrapText_IterNarrativeKind(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: `{"narrative_title":"Title","narrative_body":"Body."}`,
	}
	withWrapRenderProviderFactory(t, scripted)

	tool := NewRenderWrapTextTool(cfg)
	out, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindIterNarrative))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var resp wrapRenderResponse
	if uErr := json.Unmarshal([]byte(out), &resp); uErr != nil {
		t.Fatalf("unmarshal: %v\nout: %s", uErr, out)
	}
	if resp.NarrativeTitle != "Title" || resp.NarrativeBody != "Body." {
		t.Errorf("got %+v", resp)
	}
	if resp.CommitSubject != "" || resp.CommitProseBody != "" {
		t.Errorf("commit fields should be empty for iter_narrative kind, got %+v", resp)
	}
	if !strings.Contains(scripted.lastUser, "Render the iteration narrative") {
		t.Errorf("user prompt did not match iter_narrative template; got: %q", scripted.lastUser)
	}
	if !strings.Contains(scripted.lastSystem, "vibe-vault wrap-text renderer") {
		t.Errorf("system prompt missing preamble; got: %q", scripted.lastSystem)
	}
}

func TestRenderWrapText_CommitMsgKind(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: `{"commit_subject":"feat: ship X","commit_prose_body":"Body."}`,
	}
	withWrapRenderProviderFactory(t, scripted)

	tool := NewRenderWrapTextTool(cfg)
	out, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindCommitMsg))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var resp wrapRenderResponse
	json.Unmarshal([]byte(out), &resp)
	if resp.CommitSubject != "feat: ship X" {
		t.Errorf("commit_subject = %q", resp.CommitSubject)
	}
	if resp.NarrativeTitle != "" || resp.NarrativeBody != "" {
		t.Errorf("narrative fields should be empty for commit_msg kind")
	}
	if !strings.Contains(scripted.lastUser, "Render the project-side commit message") {
		t.Errorf("user prompt did not match commit_msg template")
	}
}

func TestRenderWrapText_BothKind(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: `{"narrative_title":"T","narrative_body":"NB","commit_subject":"feat: x","commit_prose_body":"CB"}`,
	}
	withWrapRenderProviderFactory(t, scripted)

	tool := NewRenderWrapTextTool(cfg)
	out, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindIterNarrativeAndCommitMsg))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var resp wrapRenderResponse
	json.Unmarshal([]byte(out), &resp)
	if resp.NarrativeTitle != "T" || resp.NarrativeBody != "NB" ||
		resp.CommitSubject != "feat: x" || resp.CommitProseBody != "CB" {
		t.Errorf("incomplete output: %+v", resp)
	}
	if !strings.Contains(scripted.lastUser, "Render BOTH the iteration narrative") {
		t.Errorf("user prompt did not match joint template")
	}
}

func TestRenderWrapText_BothKindRejectsIncomplete(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: `{"narrative_title":"T","narrative_body":"NB"}`, // missing commit fields
	}
	withWrapRenderProviderFactory(t, scripted)

	tool := NewRenderWrapTextTool(cfg)
	_, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindIterNarrativeAndCommitMsg))
	if err == nil {
		t.Fatal("expected error for incomplete joint kind")
	}
	if !strings.Contains(err.Error(), "incomplete output") {
		t.Errorf("error = %v", err)
	}
}

func TestRenderWrapText_InvalidKind(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{response: `{}`}
	withWrapRenderProviderFactory(t, scripted)

	tool := NewRenderWrapTextTool(cfg)
	args, _ := json.Marshal(map[string]any{
		"kind":         "bogus",
		"tier":         "sonnet",
		"project_name": "p",
		"iter_state":   map[string]any{"iter_n": 1},
	})
	_, err := tool.Handler(args)
	if err == nil || !strings.Contains(err.Error(), "invalid kind") {
		t.Fatalf("want invalid-kind error, got %v", err)
	}
}

func TestRenderWrapText_MissingTier(t *testing.T) {
	cfg := configWithTiers(t)
	tool := NewRenderWrapTextTool(cfg)
	args, _ := json.Marshal(map[string]any{
		"kind":         WrapRenderKindIterNarrative,
		"project_name": "p",
		"iter_state":   map[string]any{"iter_n": 1},
	})
	_, err := tool.Handler(args)
	if err == nil || !strings.Contains(err.Error(), "tier is required") {
		t.Fatalf("want tier-required error, got %v", err)
	}
}

func TestRenderWrapText_BadIterN(t *testing.T) {
	cfg := configWithTiers(t)
	tool := NewRenderWrapTextTool(cfg)
	args, _ := json.Marshal(map[string]any{
		"kind":         WrapRenderKindIterNarrative,
		"tier":         "sonnet",
		"project_name": "p",
		"iter_state":   map[string]any{"iter_n": 0},
	})
	_, err := tool.Handler(args)
	if err == nil || !strings.Contains(err.Error(), "iter_n must be a positive integer") {
		t.Fatalf("want iter_n error, got %v", err)
	}
}

func TestRenderWrapText_CodeFenceTolerated(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: "```json\n{\"narrative_title\":\"T\",\"narrative_body\":\"B\"}\n```",
	}
	withWrapRenderProviderFactory(t, scripted)

	tool := NewRenderWrapTextTool(cfg)
	out, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindIterNarrative))
	if err != nil {
		t.Fatalf("should tolerate code fence: %v", err)
	}
	var resp wrapRenderResponse
	json.Unmarshal([]byte(out), &resp)
	if resp.NarrativeTitle != "T" {
		t.Errorf("narrative_title = %q", resp.NarrativeTitle)
	}
}

func TestRenderWrapText_CommitSubjectMustBeSingleLine(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: `{"commit_subject":"feat: x\nbody","commit_prose_body":"b"}`,
	}
	withWrapRenderProviderFactory(t, scripted)

	tool := NewRenderWrapTextTool(cfg)
	_, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindCommitMsg))
	if err == nil || !strings.Contains(err.Error(), "single-line") {
		t.Fatalf("want single-line error, got %v", err)
	}
}

// TestRenderWrapText_ProviderError surfaces upstream provider errors
// (network, auth, etc.) verbatim — operators should see the underlying
// failure, not a generic "render failed" wrapper.
func TestRenderWrapText_ProviderError(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		err: &llm.TransientError{Err: nil},
	}
	withWrapRenderProviderFactory(t, scripted)

	tool := NewRenderWrapTextTool(cfg)
	_, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindIterNarrative))
	if err == nil {
		t.Fatal("expected error from provider")
	}
}

// TestRenderWrapText_MalformedJSON checks the renderer rejects
// non-JSON provider output cleanly.
func TestRenderWrapText_MalformedJSON(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{response: "not json at all"}
	withWrapRenderProviderFactory(t, scripted)

	tool := NewRenderWrapTextTool(cfg)
	_, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindIterNarrative))
	if err == nil || !strings.Contains(err.Error(), "parse provider JSON") {
		t.Fatalf("want parse error, got %v", err)
	}
}

// TestRenderWrapText_PromptIncludesIterState verifies the iter_state
// JSON appears in the rendered user prompt verbatim — so operators
// can see exactly what facts the LLM was given.
func TestRenderWrapText_PromptIncludesIterState(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: `{"narrative_title":"T","narrative_body":"B"}`,
	}
	withWrapRenderProviderFactory(t, scripted)

	tool := NewRenderWrapTextTool(cfg)
	if _, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindIterNarrative)); err != nil {
		t.Fatalf("handler: %v", err)
	}
	for _, want := range []string{`"sha": "deadbeef"`, `"branch": "main"`,
		`"unit": 1974`, `"last_iter_anchor_sha": "abc123"`} {
		if !strings.Contains(scripted.lastUser, want) {
			t.Errorf("user prompt missing %q\nfull prompt:\n%s", want, scripted.lastUser)
		}
	}
	// Project context interpolated as raw values, not JSON-quoted.
	if !strings.Contains(scripted.lastUser, "## Current State") {
		t.Errorf("user prompt missing resume_state interpolation")
	}
	if !strings.Contains(scripted.lastUser, "slug-a, slug-b") {
		t.Errorf("user prompt missing open_threads interpolation")
	}
}

// TestRenderWrapText_XMLSpecialCharsRoundTrip is the D2 regression
// test: prose containing XML-special characters (<tag>, </tag>,
// &copy;, literal <sha>..<sha> strings) MUST round-trip verbatim
// through the tool's json.Unmarshal + verbatim-write paths. Proves
// vibe-vault's MCP layer is not the source of harness-side mangling.
func TestRenderWrapText_XMLSpecialCharsRoundTrip(t *testing.T) {
	cfg := configWithTiers(t)

	xmlBody := "<tag>start</tag>\n\nThis spans <sha>abc</sha>..<sha>def</sha> with &copy; 2026 and <some/> and <![CDATA[stuff]]>."
	xmlSubject := "feat: <wrap> and </wrap> retained verbatim"

	respJSON, err := json.Marshal(map[string]string{
		"narrative_title":   "<title> with </title>",
		"narrative_body":    xmlBody,
		"commit_subject":    xmlSubject,
		"commit_prose_body": xmlBody,
	})
	if err != nil {
		t.Fatalf("marshal scripted response: %v", err)
	}
	scripted := &scriptedProvider{response: string(respJSON)}
	withWrapRenderProviderFactory(t, scripted)

	// Also pass XML special chars THROUGH the input — to prove
	// json.Unmarshal of the request preserves them on the way in.
	args := map[string]any{
		"kind":         WrapRenderKindIterNarrativeAndCommitMsg,
		"tier":         "sonnet",
		"project_name": "<project> & co",
		"iter_state": map[string]any{
			"iter_n":               7,
			"branch":               "main",
			"last_iter_anchor_sha": "<sha>abc</sha>",
			"commits_since_last_iter": []map[string]string{
				{"sha": "<x>", "subject": "feat: <ship/> & <Goth>'`"},
			},
			"files_changed": []string{"<file>", "&path"},
			"task_deltas": map[string]any{
				"added":     []string{"<task>"},
				"retired":   []string{},
				"cancelled": []string{},
			},
			"test_counts": map[string]int{"unit": 1, "integration": 0, "lint": 0},
		},
		"project_context": map[string]any{
			"resume_state":      "<state>",
			"recent_iterations": "<iter>",
			"open_threads":      []string{"<thread>"},
			"friction_trends":   map[string]any{"alert": "<alert/>"},
		},
	}
	argsJSON, _ := json.Marshal(args)

	tool := NewRenderWrapTextTool(cfg)
	out, err := tool.Handler(argsJSON)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var resp wrapRenderResponse
	if uErr := json.Unmarshal([]byte(out), &resp); uErr != nil {
		t.Fatalf("unmarshal output: %v", uErr)
	}
	if resp.NarrativeTitle != "<title> with </title>" {
		t.Errorf("narrative_title mangled: %q", resp.NarrativeTitle)
	}
	if resp.NarrativeBody != xmlBody {
		t.Errorf("narrative_body mangled: %q", resp.NarrativeBody)
	}
	if resp.CommitSubject != xmlSubject {
		t.Errorf("commit_subject mangled: %q", resp.CommitSubject)
	}
	if resp.CommitProseBody != xmlBody {
		t.Errorf("commit_prose_body mangled: %q", resp.CommitProseBody)
	}
	// Prove input-side XML round-trip: the LLM must have seen the
	// literal characters in its user prompt, not HTML-escaped ones.
	if !strings.Contains(scripted.lastUser, "<sha>abc</sha>") {
		t.Errorf("input XML mangled in prompt; got: %s", scripted.lastUser)
	}
	if !strings.Contains(scripted.lastUser, "&path") {
		t.Errorf("input ampersand mangled in prompt; got: %s", scripted.lastUser)
	}
}

// TestRenderUserPrompt_GoldenSnapshot pins the prompt-string output
// for one fixed input across the three kinds, so any prompt-template
// edit surfaces as a reviewable diff.
func TestRenderUserPrompt_GoldenSnapshot(t *testing.T) {
	req := wrapRenderRequest{
		Kind:        WrapRenderKindIterNarrative,
		Tier:        "sonnet",
		ProjectName: "vibe-vault",
		IterState: wrapRenderIterStateInput{
			IterN:                42,
			Branch:               "main",
			LastIterAnchorSha:    "abcd",
			CommitsSinceLastIter: []wrapRenderCommitInput{{SHA: "deadbeef", Subject: "feat: x"}},
			FilesChanged:         []string{"a.go"},
			TaskDeltas:           wrapRenderTaskDeltasInput{Added: []string{"new"}},
			TestCounts:           wrapRenderTestCountsInput{Unit: 1, Integration: 0, Lint: 0},
		},
		ProjectContext: wrapRenderProjectContextInput{
			ResumeState:      "RS",
			RecentIterations: "RI",
			OpenThreads:      []string{"a", "b"},
			FrictionTrends:   json.RawMessage(`{"k":1}`),
		},
	}
	got, err := renderUserPrompt(req)
	if err != nil {
		t.Fatalf("renderUserPrompt: %v", err)
	}
	for _, sub := range []string{
		`"vibe-vault" iteration 42`,
		`"sha": "deadbeef"`,
		`"new"`,
		`a, b`,
		`{"k":1}`,
	} {
		if !strings.Contains(got, sub) {
			t.Errorf("rendered prompt missing %q\nrendered:\n%s", sub, got)
		}
	}

	// Same payload with commit_msg kind: must use the commit-message
	// template, NOT the iter-narrative one.
	reqCM := req
	reqCM.Kind = WrapRenderKindCommitMsg
	gotCM, err := renderUserPrompt(reqCM)
	if err != nil {
		t.Fatalf("renderUserPrompt commit_msg: %v", err)
	}
	if strings.Contains(gotCM, "narrative_title") {
		t.Error("commit_msg prompt should not mention narrative_title schema")
	}
	if !strings.Contains(gotCM, "commit_subject") {
		t.Error("commit_msg prompt should mention commit_subject")
	}

	// Joint kind exercises BOTH schema fragments.
	reqJoint := req
	reqJoint.Kind = WrapRenderKindIterNarrativeAndCommitMsg
	gotJoint, err := renderUserPrompt(reqJoint)
	if err != nil {
		t.Fatalf("renderUserPrompt joint: %v", err)
	}
	if !strings.Contains(gotJoint, "narrative_title") || !strings.Contains(gotJoint, "commit_subject") {
		t.Error("joint prompt should mention both schemas")
	}
}

// --- Phase A: renderer + caller-contract tests (21–24) ---

// Test 21: wrapRenderResponse round-trips Summary across narrative kinds;
// commit_msg kind zeroes Summary even when the LLM leaks it.
func TestRenderWrapText_SummaryRoundTripAndCommitZero(t *testing.T) {
	cfg := configWithTiers(t)

	t.Run("iter_narrative_round_trip", func(t *testing.T) {
		scripted := &scriptedProvider{
			response: `{"narrative_title":"T","narrative_body":"NB","summary":"LLM-supplied summary."}`,
		}
		withWrapRenderProviderFactory(t, scripted)
		tool := NewRenderWrapTextTool(cfg)
		out, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindIterNarrative))
		if err != nil {
			t.Fatalf("handler: %v", err)
		}
		var resp wrapRenderResponse
		if jerr := json.Unmarshal([]byte(out), &resp); jerr != nil {
			t.Fatalf("unmarshal: %v", jerr)
		}
		if resp.Summary != "LLM-supplied summary." {
			t.Errorf("Summary = %q, want round-trip", resp.Summary)
		}
	})

	t.Run("joint_round_trip", func(t *testing.T) {
		scripted := &scriptedProvider{
			response: `{"narrative_title":"T","narrative_body":"NB","summary":"Joint summary.","commit_subject":"feat: x","commit_prose_body":"CB"}`,
		}
		withWrapRenderProviderFactory(t, scripted)
		tool := NewRenderWrapTextTool(cfg)
		out, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindIterNarrativeAndCommitMsg))
		if err != nil {
			t.Fatalf("handler: %v", err)
		}
		var resp wrapRenderResponse
		_ = json.Unmarshal([]byte(out), &resp)
		if resp.Summary != "Joint summary." {
			t.Errorf("joint Summary = %q, want round-trip", resp.Summary)
		}
	})

	t.Run("commit_msg_zeroes_leaked_summary", func(t *testing.T) {
		// LLM leaks summary even though commit_msg shouldn't carry it.
		scripted := &scriptedProvider{
			response: `{"commit_subject":"feat: x","commit_prose_body":"CB","summary":"LEAKED — should be zeroed."}`,
		}
		withWrapRenderProviderFactory(t, scripted)
		tool := NewRenderWrapTextTool(cfg)
		out, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindCommitMsg))
		if err != nil {
			t.Fatalf("handler: %v", err)
		}
		var resp wrapRenderResponse
		_ = json.Unmarshal([]byte(out), &resp)
		if resp.Summary != "" {
			t.Errorf("commit_msg kind must zero leaked summary; got %q", resp.Summary)
		}
		// Make sure JSON didn't carry the summary key either (omitempty).
		if strings.Contains(out, `"summary"`) {
			t.Errorf("commit_msg JSON must omit summary key; got: %s", out)
		}
	})
}

// Test 22: zeroNonKindFields rejects Summary >200 chars (no auto-truncate).
func TestRenderWrapText_RejectsSummaryOverLength(t *testing.T) {
	cfg := configWithTiers(t)
	long := strings.Repeat("x", 201)
	scripted := &scriptedProvider{
		response: `{"narrative_title":"T","narrative_body":"NB","summary":"` + long + `"}`,
	}
	withWrapRenderProviderFactory(t, scripted)
	tool := NewRenderWrapTextTool(cfg)
	_, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindIterNarrative))
	if err == nil {
		t.Fatal("expected error for >200-char summary")
	}
	if !strings.Contains(err.Error(), "summary") || !strings.Contains(err.Error(), "200") {
		t.Errorf("error should cite summary + 200; got: %v", err)
	}
}

// Test 23: zeroNonKindFields rejects Summary containing newline.
func TestRenderWrapText_RejectsSummaryNewline(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: `{"narrative_title":"T","narrative_body":"NB","summary":"line one\nline two"}`,
	}
	withWrapRenderProviderFactory(t, scripted)
	tool := NewRenderWrapTextTool(cfg)
	_, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindIterNarrative))
	if err == nil {
		t.Fatal("expected error for multi-line summary")
	}
	if !strings.Contains(err.Error(), "multi-line") && !strings.Contains(err.Error(), "single-line") {
		t.Errorf("error should cite single-line; got: %v", err)
	}
}

// Test 24: Path A H5 fallback. When LLM omits summary for narrative kind
// and NarrativeBody is non-empty, Summary is auto-populated from the
// first-paragraph extraction. No error.
func TestRenderWrapText_PathAFallbackFromBody(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		// No summary key.
		response: `{"narrative_title":"T","narrative_body":"First paragraph of the narrative body.\n\nSecond paragraph."}`,
	}
	withWrapRenderProviderFactory(t, scripted)
	tool := NewRenderWrapTextTool(cfg)
	out, err := tool.Handler(sampleRenderArgs(t, WrapRenderKindIterNarrative))
	if err != nil {
		t.Fatalf("Path A fallback should not error: %v", err)
	}
	var resp wrapRenderResponse
	_ = json.Unmarshal([]byte(out), &resp)
	if resp.Summary == "" {
		t.Error("Summary should be auto-filled from NarrativeBody")
	}
	if !strings.Contains(resp.Summary, "First paragraph") {
		t.Errorf("fallback summary should come from first paragraph; got %q", resp.Summary)
	}
	if strings.Contains(resp.Summary, "Second paragraph") {
		t.Errorf("fallback should stop at paragraph boundary; got %q", resp.Summary)
	}
}

// TestRenderWrapText_DefaultProviderFactoryRejectsEmpty asserts the
// production factory returns an actionable error when [wrap.tiers] is
// empty. This locks the operator-guidance contract.
func TestRenderWrapText_DefaultProviderFactoryRejectsEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Wrap.Tiers = nil
	_, err := wrapRenderProviderFactory(cfg, "sonnet")
	if err == nil || !strings.Contains(err.Error(), "[wrap.tiers] not configured") {
		t.Errorf("expected [wrap.tiers] error, got %v", err)
	}
}

// TestRenderWrapText_DefaultProviderFactoryUnknownTier checks tier-not-
// in-config returns a "define it" pointer to the operator.
func TestRenderWrapText_DefaultProviderFactoryUnknownTier(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Wrap.Tiers = map[string]string{"opus": "anthropic:x"}
	_, err := wrapRenderProviderFactory(cfg, "sonnet")
	if err == nil || !strings.Contains(err.Error(), "unknown tier") {
		t.Errorf("expected unknown-tier error, got %v", err)
	}
}

// --- Phase A: vault_side_narrative_seed tests (vault-side-narrative-seed task) ---

// renderArgsWithSeed returns the standard sample args with an optional
// vault_side_narrative_seed value injected. When seed is nil, the field
// is omitted from the JSON entirely (exercising the omitempty
// equivalence per L3).
func renderArgsWithSeed(t *testing.T, kind string, seed *string) json.RawMessage {
	t.Helper()
	args := map[string]any{
		"kind":         kind,
		"tier":         "sonnet",
		"project_name": "myproj",
		"iter_state": map[string]any{
			"iter_n":               42,
			"branch":               "main",
			"last_iter_anchor_sha": "abc123",
			"commits_since_last_iter": []map[string]string{
				{"sha": "deadbeef", "subject": "feat: ship"},
			},
			"files_changed": []string{"main.go"},
			"task_deltas": map[string]any{
				"added":     []string{},
				"retired":   []string{"oldtask"},
				"cancelled": []string{},
			},
			"test_counts": map[string]int{
				"unit": 1974, "integration": 31, "lint": 0,
			},
		},
		"project_context": map[string]any{
			"resume_state":      "## Current State\n- Iterations: 168",
			"recent_iterations": "### Iteration 41 — Prior\n\nbody.",
			"open_threads":      []string{"slug-a", "slug-b"},
			"friction_trends":   map[string]any{"alert": false},
		},
	}
	if seed != nil {
		args["vault_side_narrative_seed"] = *seed
	}
	out, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return out
}

// Test 1: kind=iter_narrative with non-empty seed — seed substring
// appears verbatim in scripted.lastUser (rendered prompt).
func TestRenderWrapText_VaultSideNarrativeSeedInIterNarrativePrompt(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: `{"narrative_title":"T","narrative_body":"B."}`,
	}
	withWrapRenderProviderFactory(t, scripted)

	seed := "Iter 42 ships post-merge reconciliation context not in iter_state."
	tool := NewRenderWrapTextTool(cfg)
	if _, err := tool.Handler(renderArgsWithSeed(t, WrapRenderKindIterNarrative, &seed)); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.Contains(scripted.lastUser, seed) {
		t.Errorf("seed substring missing from rendered iter_narrative prompt\nseed: %q\nprompt:\n%s",
			seed, scripted.lastUser)
	}
	if strings.Contains(scripted.lastUser, "(none provided)") {
		t.Errorf("placeholder leaked into prompt despite non-empty seed; prompt:\n%s", scripted.lastUser)
	}
}

// Test 2: kind=iter_narrative_and_commit_msg with non-empty seed —
// substring appears in the joint prompt.
func TestRenderWrapText_VaultSideNarrativeSeedInJointPrompt(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: `{"narrative_title":"T","narrative_body":"B","commit_subject":"feat: x","commit_prose_body":"CB"}`,
	}
	withWrapRenderProviderFactory(t, scripted)

	seed := "Verification milestone: third dispatch in five-phase epic."
	tool := NewRenderWrapTextTool(cfg)
	if _, err := tool.Handler(renderArgsWithSeed(t, WrapRenderKindIterNarrativeAndCommitMsg, &seed)); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.Contains(scripted.lastUser, seed) {
		t.Errorf("seed substring missing from rendered joint prompt\nseed: %q\nprompt:\n%s",
			seed, scripted.lastUser)
	}
	if strings.Contains(scripted.lastUser, "(none provided)") {
		t.Errorf("placeholder leaked into joint prompt despite non-empty seed; prompt:\n%s", scripted.lastUser)
	}
}

// Test 3: kind=commit_msg with non-empty seed — handler hard-errors
// at validation per D2; provider must not be called.
func TestRenderWrapText_VaultSideNarrativeSeedRejectedForCommitMsg(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: `{"commit_subject":"feat: x","commit_prose_body":"B"}`,
	}
	withWrapRenderProviderFactory(t, scripted)

	seed := "should-be-rejected seed for commit_msg kind"
	tool := NewRenderWrapTextTool(cfg)
	_, err := tool.Handler(renderArgsWithSeed(t, WrapRenderKindCommitMsg, &seed))
	if err == nil {
		t.Fatal("expected error for seed-with-commit_msg kind")
	}
	if !strings.Contains(err.Error(), "vault_side_narrative_seed is not supported for kind=commit_msg") {
		t.Errorf("error should match D2 message; got: %v", err)
	}
	if !strings.Contains(err.Error(), "mechanically derived from iter_state") {
		t.Errorf("error should explain mechanical-derivation rationale; got: %v", err)
	}
	if scripted.calls != 0 {
		t.Errorf("provider must not be called when validation rejects request; calls = %d", scripted.calls)
	}
}

// Test 4: empty seed ("") renders the prompt section as "(none
// provided)" per MC1; literal empty value does not appear as a leak.
func TestRenderWrapText_VaultSideNarrativeSeedEmptyRendersPlaceholder(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: `{"narrative_title":"T","narrative_body":"B."}`,
	}
	withWrapRenderProviderFactory(t, scripted)

	empty := ""
	tool := NewRenderWrapTextTool(cfg)
	if _, err := tool.Handler(renderArgsWithSeed(t, WrapRenderKindIterNarrative, &empty)); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.Contains(scripted.lastUser, "(none provided)") {
		t.Errorf("placeholder missing from rendered prompt for empty seed\nprompt:\n%s", scripted.lastUser)
	}
	// The {{vault_side_narrative_seed}} token must have been substituted
	// — no raw template token should leak.
	if strings.Contains(scripted.lastUser, "{{vault_side_narrative_seed}}") {
		t.Errorf("raw template token leaked into prompt; substitution failed:\n%s", scripted.lastUser)
	}
}

// Test 5: omitted seed (field absent from JSON, omitempty equivalence
// per L3) renders identically to the empty-seed case.
func TestRenderWrapText_VaultSideNarrativeSeedOmittedRendersPlaceholder(t *testing.T) {
	cfg := configWithTiers(t)

	// Render with omitted seed.
	scriptedOmitted := &scriptedProvider{
		response: `{"narrative_title":"T","narrative_body":"B."}`,
	}
	withWrapRenderProviderFactory(t, scriptedOmitted)
	toolOmitted := NewRenderWrapTextTool(cfg)
	if _, err := toolOmitted.Handler(renderArgsWithSeed(t, WrapRenderKindIterNarrative, nil)); err != nil {
		t.Fatalf("handler (omitted): %v", err)
	}

	// Render with explicit empty-string seed (must restore factory after
	// the omitted-side run, since withWrapRenderProviderFactory's cleanup
	// is t.Cleanup-scoped to the test, not the call site).
	scriptedEmpty := &scriptedProvider{
		response: `{"narrative_title":"T","narrative_body":"B."}`,
	}
	withWrapRenderProviderFactory(t, scriptedEmpty)
	toolEmpty := NewRenderWrapTextTool(cfg)
	empty := ""
	if _, err := toolEmpty.Handler(renderArgsWithSeed(t, WrapRenderKindIterNarrative, &empty)); err != nil {
		t.Fatalf("handler (empty): %v", err)
	}

	if scriptedOmitted.lastUser != scriptedEmpty.lastUser {
		t.Errorf("omitted-seed prompt must equal empty-seed prompt (omitempty equivalence)\n"+
			"omitted:\n%s\n--\nempty:\n%s", scriptedOmitted.lastUser, scriptedEmpty.lastUser)
	}
	if !strings.Contains(scriptedOmitted.lastUser, "(none provided)") {
		t.Errorf("omitted-seed prompt missing placeholder\nprompt:\n%s", scriptedOmitted.lastUser)
	}
}

// Test 6: seed at exactly 4096 chars passes validation.
func TestValidateWrapRenderRequest_VaultSideNarrativeSeedAtBoundary(t *testing.T) {
	req := wrapRenderRequest{
		Kind:        WrapRenderKindIterNarrative,
		Tier:        "sonnet",
		ProjectName: "p",
		IterState: wrapRenderIterStateInput{
			IterN: 1,
		},
		VaultSideNarrativeSeed: strings.Repeat("x", 4096),
	}
	if err := validateWrapRenderRequest(req); err != nil {
		t.Errorf("seed of exactly %d chars should pass; got: %v", vaultSideNarrativeSeedMaxChars, err)
	}
}

// Test 7: seed at 4097 chars rejected with the actionable error.
func TestValidateWrapRenderRequest_VaultSideNarrativeSeedExceedsBoundary(t *testing.T) {
	req := wrapRenderRequest{
		Kind:        WrapRenderKindIterNarrative,
		Tier:        "sonnet",
		ProjectName: "p",
		IterState: wrapRenderIterStateInput{
			IterN: 1,
		},
		VaultSideNarrativeSeed: strings.Repeat("y", 4097),
	}
	err := validateWrapRenderRequest(req)
	if err == nil {
		t.Fatalf("seed of %d chars should be rejected", 4097)
	}
	if !strings.Contains(err.Error(), "vault_side_narrative_seed exceeds 4096 character limit") {
		t.Errorf("error should cite the 4096 limit; got: %v", err)
	}
	if !strings.Contains(err.Error(), "got 4097") {
		t.Errorf("error should report the actual length; got: %v", err)
	}
	if !strings.Contains(err.Error(), "split context across multiple wraps if needed") {
		t.Errorf("error should be actionable; got: %v", err)
	}
}

// Test 8: seed with markdown special characters (backticks, headings,
// fences) survives substitution unescaped — drops into the prompt
// verbatim.
func TestRenderWrapText_VaultSideNarrativeSeedWithMarkdownSurvives(t *testing.T) {
	cfg := configWithTiers(t)
	scripted := &scriptedProvider{
		response: `{"narrative_title":"T","narrative_body":"B."}`,
	}
	withWrapRenderProviderFactory(t, scripted)

	seed := "## Heading\n\n" +
		"Inline `backticks` and ```fenced code``` and a list:\n" +
		"- item 1\n" +
		"- item 2 with `code`\n\n" +
		"```go\nfunc x() {}\n```\n"
	tool := NewRenderWrapTextTool(cfg)
	if _, err := tool.Handler(renderArgsWithSeed(t, WrapRenderKindIterNarrative, &seed)); err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !strings.Contains(scripted.lastUser, seed) {
		t.Errorf("markdown-laden seed substring missing from rendered prompt verbatim\nseed:\n%s\nprompt:\n%s",
			seed, scripted.lastUser)
	}
}

// Test 9: golden-snapshot — D3 instruction paragraph ("Treat the seed
// as load-bearing ground truth") appears verbatim with a representative
// seed; placeholder is replaced when seed is non-empty. Table-driven
// across both narrative kinds.
func TestRenderUserPrompt_VaultSideNarrativeSeedGoldenSnapshot(t *testing.T) {
	const repSeed = "Iter 209 is the second of 2-3 recommended verification wraps for iterations-summary-frontmatter Phase A. iter-208 PR rebase-merged to main preserving 3 commits."

	cases := []struct {
		name string
		kind string
	}{
		{"iter_narrative", WrapRenderKindIterNarrative},
		{"iter_narrative_and_commit_msg", WrapRenderKindIterNarrativeAndCommitMsg},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := wrapRenderRequest{
				Kind:        tc.kind,
				Tier:        "sonnet",
				ProjectName: "vibe-vault",
				IterState: wrapRenderIterStateInput{
					IterN:                42,
					Branch:               "main",
					LastIterAnchorSha:    "abcd",
					CommitsSinceLastIter: []wrapRenderCommitInput{{SHA: "deadbeef", Subject: "feat: x"}},
					FilesChanged:         []string{"a.go"},
					TestCounts:           wrapRenderTestCountsInput{Unit: 1},
				},
				ProjectContext: wrapRenderProjectContextInput{
					ResumeState:      "RS",
					RecentIterations: "RI",
					OpenThreads:      []string{"a"},
					FrictionTrends:   json.RawMessage(`{"k":1}`),
				},
				VaultSideNarrativeSeed: repSeed,
			}
			got, err := renderUserPrompt(req)
			if err != nil {
				t.Fatalf("renderUserPrompt: %v", err)
			}
			// D3 instruction substring (verbatim).
			if !strings.Contains(got, "Treat the seed as load-bearing ground truth") {
				t.Errorf("rendered prompt missing D3 instruction clause; rendered:\n%s", got)
			}
			// Seed substring (verbatim).
			if !strings.Contains(got, repSeed) {
				t.Errorf("rendered prompt missing representative seed; rendered:\n%s", got)
			}
			// Placeholder must be replaced.
			if strings.Contains(got, "(none provided)") {
				t.Errorf("placeholder leaked despite non-empty seed; rendered:\n%s", got)
			}
			// Section header.
			if !strings.Contains(got, "vault_side_narrative_seed (orchestrator-supplied context):") {
				t.Errorf("rendered prompt missing seed section header; rendered:\n%s", got)
			}
		})
	}
}

// Test 10: M1 + M2 instruction clauses appear verbatim in both
// narrative-kind rendered prompts.
func TestRenderUserPrompt_VaultSideNarrativeSeedInstructionClauses(t *testing.T) {
	for _, kind := range []string{WrapRenderKindIterNarrative, WrapRenderKindIterNarrativeAndCommitMsg} {
		t.Run(kind, func(t *testing.T) {
			req := wrapRenderRequest{
				Kind:        kind,
				Tier:        "sonnet",
				ProjectName: "p",
				IterState: wrapRenderIterStateInput{
					IterN: 1,
				},
				VaultSideNarrativeSeed: "any seed",
			}
			got, err := renderUserPrompt(req)
			if err != nil {
				t.Fatalf("renderUserPrompt: %v", err)
			}
			// M1 clause: "the seed IS the substance"
			if !strings.Contains(got, "the seed IS the substance") {
				t.Errorf("M1 clause missing for kind %q; rendered:\n%s", kind, got)
			}
			// M2 clause: "seed is operator-supplied ground truth and
			// supersedes iter_state's silence"
			if !strings.Contains(got, "seed is operator-supplied ground truth and supersedes iter_state's silence") {
				t.Errorf("M2 clause missing for kind %q; rendered:\n%s", kind, got)
			}
		})
	}

	// Sanity: M1/M2 clauses do NOT appear in the commit_msg template
	// (D2 — seed never flows there).
	reqCM := wrapRenderRequest{
		Kind:        WrapRenderKindCommitMsg,
		Tier:        "sonnet",
		ProjectName: "p",
		IterState: wrapRenderIterStateInput{
			IterN: 1,
		},
	}
	gotCM, err := renderUserPrompt(reqCM)
	if err != nil {
		t.Fatalf("renderUserPrompt commit_msg: %v", err)
	}
	if strings.Contains(gotCM, "the seed IS the substance") {
		t.Error("commit_msg template must not carry the seed M1 clause")
	}
}

// Test 11: templates/agentctx/commands/wrap.md carries the new
// "Composing the vault_side_narrative_seed" sub-section with both
// canonical example shape strings.
func TestWrapTemplate_VaultSideNarrativeSeedSubsection(t *testing.T) {
	// Tests run with cwd = package dir (internal/mcp). The template
	// sits at the repo root: ../../templates/agentctx/commands/wrap.md.
	path := filepath.Join("..", "..", "templates", "agentctx", "commands", "wrap.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read wrap.md: %v", err)
	}
	body := string(data)

	for _, want := range []string{
		"### Composing the vault_side_narrative_seed",
		"Verification-milestone shape (iter 208)",
		"Post-merge reconciliation shape (iter 209)",
		"vault_side_narrative_seed = \"<orchestrator-supplied prose, ≤4096 chars>\"",
		"`commit_msg` kind hard-errors when seed is non-empty",
		"`(none provided)`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("wrap.md missing required substring %q", want)
		}
	}
}
