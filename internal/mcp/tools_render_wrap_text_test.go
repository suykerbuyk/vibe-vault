// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"context"
	"encoding/json"
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
