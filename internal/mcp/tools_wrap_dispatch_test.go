// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/agentregistry"
	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/llm"
	"github.com/suykerbuyk/vibe-vault/internal/wrapbundlecache"
	"github.com/suykerbuyk/vibe-vault/internal/wrapdispatch"
	"github.com/suykerbuyk/vibe-vault/internal/wrapmetrics"
)

// withDispatchLineWriter swaps the dispatch-metrics writer for the test
// duration and restores the production default on cleanup.
func withDispatchLineWriter(t *testing.T, fn func(wrapmetrics.DispatchLine) error) {
	t.Helper()
	old := dispatchLineWriter
	dispatchLineWriter = fn
	t.Cleanup(func() { dispatchLineWriter = old })
}

// dispatchTestProvider is a scripted llm.AgenticProvider for the wrap-dispatch
// MCP tests. It mirrors the wrapdispatch package's mock but is duplicated
// here so the mcp test code does not import a test-only package.
type dispatchTestProvider struct {
	turns []dispatchTestTurn
	err   error
}

type dispatchTestTurn struct {
	Tool  string
	Input json.RawMessage
	Final bool
	Stop  string
	Usage llm.UsageStats
}

func (d *dispatchTestProvider) Name() string { return "test-mock" }
func (d *dispatchTestProvider) ChatCompletion(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return nil, errors.New("not implemented")
}
func (d *dispatchTestProvider) RunTools(_ context.Context, req llm.ToolsRequest) (*llm.ToolsResponse, error) {
	if d.err != nil {
		return nil, d.err
	}
	var lastUsage llm.UsageStats
	for _, turn := range d.turns {
		if turn.Tool != "" {
			req.ToolExecutor(turn.Tool, turn.Input)
		}
		lastUsage = turn.Usage
		if turn.Final {
			return &llm.ToolsResponse{StopReason: turn.Stop, Usage: lastUsage}, nil
		}
	}
	return &llm.ToolsResponse{StopReason: "max_tokens", Usage: lastUsage}, nil
}

// withProviderFactory swaps the package-level providerFactory for the
// duration of the test and restores it on cleanup. The adapter dispatches
// the MCP tool at request time rather than at registration time, so the
// override takes effect on every call between the swap and the cleanup.
func withProviderFactory(t *testing.T, fn func(providerModel, apiKey string) (llm.AgenticProvider, error)) {
	t.Helper()
	old := providerFactory
	providerFactory = fn
	t.Cleanup(func() { providerFactory = old })
}

// preparedSkeleton writes a stub skeleton via the cache helper and returns
// the handle the tests pass to vv_wrap_dispatch.
func preparedSkeleton(t *testing.T) SkeletonHandle {
	t.Helper()
	skel := WrapSkeleton{
		Iter:    100,
		Project: "test-project",
	}
	data, err := json.Marshal(skel)
	if err != nil {
		t.Fatalf("marshal skeleton: %v", err)
	}
	path, sha, err := wrapbundlecache.Write(skel.Iter, data)
	if err != nil {
		t.Fatalf("write skeleton: %v", err)
	}
	return SkeletonHandle{Iter: skel.Iter, SkeletonPath: path, SkeletonSHA256: sha}
}

// withAPIKey ensures ANTHROPIC_API_KEY is set during the test. If pass is
// empty, the env var is cleared instead (used by the missing-key test).
func withAPIKey(t *testing.T, pass string) {
	t.Helper()
	prev, hadPrev := os.LookupEnv("ANTHROPIC_API_KEY")
	if pass == "" {
		_ = os.Unsetenv("ANTHROPIC_API_KEY")
	} else {
		_ = os.Setenv("ANTHROPIC_API_KEY", pass)
	}
	t.Cleanup(func() {
		if hadPrev {
			_ = os.Setenv("ANTHROPIC_API_KEY", prev)
		} else {
			_ = os.Unsetenv("ANTHROPIC_API_KEY")
		}
	})
}

// dispatchEnvelope is the shape returned by the MCP handler.
type dispatchEnvelope struct {
	Outputs         json.RawMessage              `json:"outputs"`
	EscalateReason  string                       `json:"escalate_reason"`
	DispatchMetrics wrapdispatch.DispatchMetrics `json:"dispatch_metrics"`
}

// fixtureWrapConfig returns a config.Config carrying the v1 default
// tier map. Tests inject this when the test scenario doesn't care about
// custom tier resolution; tests that DO care construct their own
// config.Config and call NewWrapDispatchTool directly.
func fixtureWrapConfig() config.Config {
	return config.Config{Wrap: config.WrapConfig{
		DefaultModel: "sonnet",
		Tiers: map[string]string{
			"haiku":  "anthropic:claude-haiku-4-5",
			"sonnet": "anthropic:claude-sonnet-4-6",
			"opus":   "anthropic:claude-opus-4-7",
		},
	}}
}

// callDispatch is a thin marshal+invoke helper that constructs the tool
// against a default fixture config. Use callDispatchWithConfig when a
// test needs to vary the [wrap] config.
func callDispatch(t *testing.T, args map[string]any) (string, error) {
	t.Helper()
	return callDispatchWithConfig(t, fixtureWrapConfig(), args)
}

// callDispatchWithConfig dispatches against an explicit config, exposing
// the config-driven tier resolution path to tests.
func callDispatchWithConfig(t *testing.T, cfg config.Config, args map[string]any) (string, error) {
	t.Helper()
	tool := NewWrapDispatchTool(cfg)
	params, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return tool.Handler(params)
}

// TestVVWrapDispatch_RejectsMissingHandle covers the input validation guard.
func TestVVWrapDispatch_RejectsMissingHandle(t *testing.T) {
	withSkeletonCacheDir(t)
	withAPIKey(t, "key")

	// Empty handle.
	_, err := callDispatch(t, map[string]any{
		"skeleton_handle": map[string]any{},
		"tier":            "sonnet",
		"agent_name":      "wrap-executor",
	})
	if err == nil {
		t.Fatalf("expected error for empty skeleton_handle")
	}
	if !strings.Contains(err.Error(), "iter") && !strings.Contains(err.Error(), "skeleton_path") && !strings.Contains(err.Error(), "skeleton_sha256") {
		t.Errorf("error %q does not mention any handle field", err)
	}

	// Missing tier.
	handle := preparedSkeleton(t)
	_, err = callDispatch(t, map[string]any{
		"skeleton_handle": handle,
		"agent_name":      "wrap-executor",
	})
	if err == nil {
		t.Fatalf("expected error for missing tier")
	}

	// Missing agent_name.
	_, err = callDispatch(t, map[string]any{
		"skeleton_handle": handle,
		"tier":            "sonnet",
	})
	if err == nil {
		t.Fatalf("expected error for missing agent_name")
	}
}

// TestVVWrapDispatch_RejectsUnknownTier asserts the tier→model resolver
// rejects labels not present in [wrap.tiers] and points the operator at
// the config section.
func TestVVWrapDispatch_RejectsUnknownTier(t *testing.T) {
	withSkeletonCacheDir(t)
	withAPIKey(t, "key")
	handle := preparedSkeleton(t)

	// Fixture config has no "quasar" tier.
	_, err := callDispatch(t, map[string]any{
		"skeleton_handle": handle,
		"tier":            "quasar",
		"agent_name":      "wrap-executor",
	})
	if err == nil {
		t.Fatalf("expected error for unknown tier")
	}
	if !strings.Contains(err.Error(), "wrap.tiers") {
		t.Errorf("error %q should point at [wrap.tiers] config section", err)
	}
}

// TestVVWrapDispatch_RejectsEmptyTiersConfig asserts a missing
// [wrap.tiers] section produces a clear error pointing at the config
// file rather than a confusing "unknown tier" message.
func TestVVWrapDispatch_RejectsEmptyTiersConfig(t *testing.T) {
	withSkeletonCacheDir(t)
	withAPIKey(t, "key")
	handle := preparedSkeleton(t)

	emptyCfg := config.Config{} // no Wrap.Tiers
	_, err := callDispatchWithConfig(t, emptyCfg, map[string]any{
		"skeleton_handle": handle,
		"tier":            "sonnet",
		"agent_name":      "wrap-executor",
	})
	if err == nil {
		t.Fatalf("expected error for empty [wrap.tiers]")
	}
	if !strings.Contains(err.Error(), "wrap.tiers") {
		t.Errorf("error %q should mention [wrap.tiers]", err)
	}
}

// TestVVWrapDispatch_CustomTierFromConfig confirms operator-defined tier
// labels (beyond the default haiku/sonnet/opus) work end-to-end through
// the config-driven resolveTier path.
func TestVVWrapDispatch_CustomTierFromConfig(t *testing.T) {
	withSkeletonCacheDir(t)
	withAPIKey(t, "key")
	handle := preparedSkeleton(t)

	customCfg := config.Config{Wrap: config.WrapConfig{
		Tiers: map[string]string{
			"deep": "anthropic:claude-opus-4-7",
		},
	}}

	mock := &dispatchTestProvider{
		turns: []dispatchTestTurn{{
			Tool:  "wrap_executor_finish",
			Input: json.RawMessage(`{"status":"ok","outputs":{"iteration_narrative":"ok"}}`),
			Final: true,
			Stop:  "stop",
		}},
	}
	withProviderFactory(t, func(_, _ string) (llm.AgenticProvider, error) {
		return mock, nil
	})

	out, err := callDispatchWithConfig(t, customCfg, map[string]any{
		"skeleton_handle": handle,
		"tier":            "deep",
		"agent_name":      "wrap-executor",
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	var env dispatchEnvelope
	if jerr := json.Unmarshal([]byte(out), &env); jerr != nil {
		t.Fatalf("unmarshal envelope: %v\n%s", jerr, out)
	}
	if env.DispatchMetrics.ProviderModel != "anthropic:claude-opus-4-7" {
		t.Errorf("ProviderModel=%q, want anthropic:claude-opus-4-7 (from custom tier)",
			env.DispatchMetrics.ProviderModel)
	}
}

// TestVVWrapDispatch_RejectsTamperedSkeleton confirms the compare-and-set
// guard fires when the cache file is mutated after the handle is issued.
func TestVVWrapDispatch_RejectsTamperedSkeleton(t *testing.T) {
	withSkeletonCacheDir(t)
	withAPIKey(t, "key")
	handle := preparedSkeleton(t)

	// Tamper with the on-disk skeleton bytes.
	if err := os.WriteFile(handle.SkeletonPath, []byte(`{"iter":999,"project":"tampered"}`), 0o600); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	_, err := callDispatch(t, map[string]any{
		"skeleton_handle": handle,
		"tier":            "sonnet",
		"agent_name":      "wrap-executor",
	})
	if err == nil {
		t.Fatalf("expected sha mismatch error after tamper")
	}
	if !strings.Contains(err.Error(), "sha mismatch") && !strings.Contains(err.Error(), "modified") {
		t.Errorf("error %q should mention sha/modified", err)
	}
}

// TestVVWrapDispatch_RejectsMissingAPIKey verifies the env-var guard.
func TestVVWrapDispatch_RejectsMissingAPIKey(t *testing.T) {
	withSkeletonCacheDir(t)
	withAPIKey(t, "")
	handle := preparedSkeleton(t)

	_, err := callDispatch(t, map[string]any{
		"skeleton_handle": handle,
		"tier":            "sonnet",
		"agent_name":      "wrap-executor",
	})
	if err == nil {
		t.Fatalf("expected error for missing ANTHROPIC_API_KEY")
	}
	if !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Errorf("error %q should name ANTHROPIC_API_KEY", err)
	}
}

// TestVVWrapDispatch_RejectsUnknownAgent asserts the registry lookup error
// surfaces to the caller (vs. swallowing it into a dispatch escalation).
func TestVVWrapDispatch_RejectsUnknownAgent(t *testing.T) {
	withSkeletonCacheDir(t)
	withAPIKey(t, "key")
	handle := preparedSkeleton(t)

	_, err := callDispatch(t, map[string]any{
		"skeleton_handle": handle,
		"tier":            "sonnet",
		"agent_name":      "nonexistent-agent",
	})
	if err == nil {
		t.Fatalf("expected error for unknown agent")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should report registry lookup failure", err)
	}
}

// TestVVWrapDispatch_HappyPathWithMockProvider injects a scripted provider
// via the providerFactory test seam, and asserts the dispatch outcome
// serialises correctly: outputs populated, escalate_reason empty, metrics
// surfaced.
func TestVVWrapDispatch_HappyPathWithMockProvider(t *testing.T) {
	withSkeletonCacheDir(t)
	withAPIKey(t, "test-key")
	handle := preparedSkeleton(t)

	// The agent registry must contain wrap-executor — it does, courtesy of
	// the embedded init.
	if _, err := agentregistry.Lookup("wrap-executor"); err != nil {
		t.Fatalf("wrap-executor not registered: %v", err)
	}

	mock := &dispatchTestProvider{
		turns: []dispatchTestTurn{{
			Tool:  "wrap_executor_finish",
			Input: json.RawMessage(`{"status":"ok","outputs":{"iteration_narrative":"happy path"}}`),
			Final: true,
			Stop:  "stop",
			Usage: llm.UsageStats{InputTokens: 50, OutputTokens: 12},
		}},
	}
	withProviderFactory(t, func(_, _ string) (llm.AgenticProvider, error) {
		return mock, nil
	})

	out, err := callDispatch(t, map[string]any{
		"skeleton_handle": handle,
		"tier":            "sonnet",
		"agent_name":      "wrap-executor",
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	var env dispatchEnvelope
	if jerr := json.Unmarshal([]byte(out), &env); jerr != nil {
		t.Fatalf("unmarshal envelope: %v\n%s", jerr, out)
	}
	if env.EscalateReason != "" {
		t.Errorf("EscalateReason=%q, want empty", env.EscalateReason)
	}
	if len(env.Outputs) == 0 {
		t.Fatalf("Outputs is empty; raw=%s", out)
	}
	if env.DispatchMetrics.ProviderModel != "anthropic:claude-sonnet-4-6" {
		t.Errorf("ProviderModel=%q, want anthropic:claude-sonnet-4-6", env.DispatchMetrics.ProviderModel)
	}
	if env.DispatchMetrics.ToolCallCount != 1 {
		t.Errorf("ToolCallCount=%d, want 1", env.DispatchMetrics.ToolCallCount)
	}
	if env.DispatchMetrics.InputTokens != 50 || env.DispatchMetrics.OutputTokens != 12 {
		t.Errorf("token counts = %+v, want (50,12)", env.DispatchMetrics)
	}
}

// TestVVWrapDispatch_EscalateRoundTrip asserts the response envelope
// surfaces escalate_reason without an outputs payload.
func TestVVWrapDispatch_EscalateRoundTrip(t *testing.T) {
	withSkeletonCacheDir(t)
	withAPIKey(t, "test-key")
	handle := preparedSkeleton(t)

	mock := &dispatchTestProvider{
		turns: []dispatchTestTurn{{
			Tool:  "wrap_executor_finish",
			Input: json.RawMessage(`{"status":"escalate","reason":"semantic_presence_failure"}`),
			Final: true,
			Stop:  "stop",
		}},
	}
	withProviderFactory(t, func(_, _ string) (llm.AgenticProvider, error) {
		return mock, nil
	})

	out, err := callDispatch(t, map[string]any{
		"skeleton_handle": handle,
		"tier":            "haiku",
		"agent_name":      "wrap-executor",
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	var env dispatchEnvelope
	if jerr := json.Unmarshal([]byte(out), &env); jerr != nil {
		t.Fatalf("unmarshal envelope: %v\n%s", jerr, out)
	}
	if env.EscalateReason != "semantic_presence_failure" {
		t.Errorf("EscalateReason=%q", env.EscalateReason)
	}
	if len(env.Outputs) != 0 {
		t.Errorf("Outputs should be empty on escalate: %s", string(env.Outputs))
	}
	if env.DispatchMetrics.ProviderModel != "anthropic:claude-haiku-4-5" {
		t.Errorf("ProviderModel=%q", env.DispatchMetrics.ProviderModel)
	}
}

// TestVVWrapDispatch_SynthesizeFnRoutesToFillBundle confirms the OQ-5
// invariant at the MCP layer: when the executor invokes
// vv_synthesize_wrap_bundle, the SynthesizeFunc closure runs FillBundle
// directly and returns a real WrapBundle JSON to the model.
func TestVVWrapDispatch_SynthesizeFnRoutesToFillBundle(t *testing.T) {
	withSkeletonCacheDir(t)
	withAPIKey(t, "test-key")
	handle := preparedSkeleton(t)

	// Capture the tool_result content the executor returns to the model.
	// The recordingProvider invokes the executor for one synth call, then
	// finishes ok. We assert the executor's return value parses as a
	// WrapBundle and carries the iteration index from the skeleton.
	provider := &mcpRecordingProvider{
		respond: func(req llm.ToolsRequest) (*llm.ToolsResponse, error) {
			synthOut, _ := req.ToolExecutor("vv_synthesize_wrap_bundle", json.RawMessage(`{"prose":{"iteration_narrative":"draft body"}}`))
			t.Logf("synth tool returned: %s", string(synthOut))
			var bundle WrapBundle
			if jerr := json.Unmarshal(synthOut, &bundle); jerr != nil {
				t.Errorf("synth output not a WrapBundle: %v\n%s", jerr, synthOut)
			}
			if bundle.Iteration != 100 {
				t.Errorf("bundle.Iteration=%d, want 100", bundle.Iteration)
			}
			req.ToolExecutor("wrap_executor_finish", json.RawMessage(`{"status":"ok","outputs":{"iteration_narrative":"final"}}`))
			return &llm.ToolsResponse{StopReason: "stop"}, nil
		},
	}
	withProviderFactory(t, func(_, _ string) (llm.AgenticProvider, error) {
		return provider, nil
	})

	out, err := callDispatch(t, map[string]any{
		"skeleton_handle": handle,
		"tier":            "sonnet",
		"agent_name":      "wrap-executor",
	})
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	var env dispatchEnvelope
	if jerr := json.Unmarshal([]byte(out), &env); jerr != nil {
		t.Fatalf("unmarshal envelope: %v\n%s", jerr, out)
	}
	if env.EscalateReason != "" {
		t.Errorf("EscalateReason=%q, want empty", env.EscalateReason)
	}
	if len(env.Outputs) == 0 {
		t.Errorf("Outputs is empty: %s", out)
	}
}

// TestVVWrapDispatch_EmitsDispatchLine_OK confirms the handler writes
// one DispatchLine per call on a successful tier with the expected
// fields populated (outcome=ok, model_used=<tier>).
func TestVVWrapDispatch_EmitsDispatchLine_OK(t *testing.T) {
	withSkeletonCacheDir(t)
	withAPIKey(t, "key")
	handle := preparedSkeleton(t)

	var captured []wrapmetrics.DispatchLine
	withDispatchLineWriter(t, func(line wrapmetrics.DispatchLine) error {
		captured = append(captured, line)
		return nil
	})

	mock := &dispatchTestProvider{
		turns: []dispatchTestTurn{{
			Tool:  "wrap_executor_finish",
			Input: json.RawMessage(`{"status":"ok","outputs":{"iteration_narrative":"ok"}}`),
			Final: true,
			Stop:  "stop",
			Usage: llm.UsageStats{InputTokens: 200, OutputTokens: 30},
		}},
	}
	withProviderFactory(t, func(_, _ string) (llm.AgenticProvider, error) { return mock, nil })

	if _, err := callDispatch(t, map[string]any{
		"skeleton_handle": handle,
		"tier":            "sonnet",
		"agent_name":      "wrap-executor",
	}); err != nil {
		t.Fatalf("Handler: %v", err)
	}

	if len(captured) != 1 {
		t.Fatalf("captured %d dispatch lines, want 1", len(captured))
	}
	got := captured[0]
	if got.Iter != handle.Iter {
		t.Errorf("Iter = %d, want %d", got.Iter, handle.Iter)
	}
	if len(got.TierAttempts) != 1 {
		t.Fatalf("TierAttempts len = %d, want 1", len(got.TierAttempts))
	}
	att := got.TierAttempts[0]
	if att.Tier != "sonnet" {
		t.Errorf("attempt.Tier = %q, want sonnet", att.Tier)
	}
	if att.ProviderModel != "anthropic:claude-sonnet-4-6" {
		t.Errorf("attempt.ProviderModel = %q", att.ProviderModel)
	}
	if att.Outcome != "ok" {
		t.Errorf("attempt.Outcome = %q, want ok", att.Outcome)
	}
	if att.InputTokens != 200 || att.OutputTokens != 30 {
		t.Errorf("token counts = (%d,%d), want (200,30)", att.InputTokens, att.OutputTokens)
	}
	if got.ModelUsed != "sonnet" {
		t.Errorf("ModelUsed = %q, want sonnet", got.ModelUsed)
	}
	if got.AgentDefinitionVersion == "" {
		t.Errorf("AgentDefinitionVersion empty (registry should populate)")
	}
}

// TestVVWrapDispatch_EmitsDispatchLine_Escalate confirms an escalation
// produces an "escalate" outcome line with EscalateReason populated and
// ModelUsed empty.
func TestVVWrapDispatch_EmitsDispatchLine_Escalate(t *testing.T) {
	withSkeletonCacheDir(t)
	withAPIKey(t, "key")
	handle := preparedSkeleton(t)

	var captured []wrapmetrics.DispatchLine
	withDispatchLineWriter(t, func(line wrapmetrics.DispatchLine) error {
		captured = append(captured, line)
		return nil
	})

	mock := &dispatchTestProvider{
		turns: []dispatchTestTurn{{
			Tool:  "wrap_executor_finish",
			Input: json.RawMessage(`{"status":"escalate","reason":"semantic_presence_failure"}`),
			Final: true,
			Stop:  "stop",
		}},
	}
	withProviderFactory(t, func(_, _ string) (llm.AgenticProvider, error) { return mock, nil })

	if _, err := callDispatch(t, map[string]any{
		"skeleton_handle": handle,
		"tier":            "haiku",
		"agent_name":      "wrap-executor",
	}); err != nil {
		t.Fatalf("Handler: %v", err)
	}

	if len(captured) != 1 {
		t.Fatalf("captured %d lines, want 1", len(captured))
	}
	att := captured[0].TierAttempts[0]
	if att.Outcome != "escalate" {
		t.Errorf("Outcome = %q, want escalate", att.Outcome)
	}
	if att.EscalateReason != "semantic_presence_failure" {
		t.Errorf("EscalateReason = %q", att.EscalateReason)
	}
	if captured[0].ModelUsed != "" {
		t.Errorf("ModelUsed = %q, want empty on escalate", captured[0].ModelUsed)
	}
}

// mcpRecordingProvider is a tiny llm.AgenticProvider that delegates to a
// caller-supplied closure. Mirrors wrapdispatch's recordingProvider but is
// duplicated here to avoid cross-package test-helper imports.
type mcpRecordingProvider struct {
	respond func(req llm.ToolsRequest) (*llm.ToolsResponse, error)
}

func (m *mcpRecordingProvider) Name() string { return "mcp-recording-mock" }
func (m *mcpRecordingProvider) ChatCompletion(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return nil, errors.New("not implemented")
}
func (m *mcpRecordingProvider) RunTools(_ context.Context, req llm.ToolsRequest) (*llm.ToolsResponse, error) {
	return m.respond(req)
}
