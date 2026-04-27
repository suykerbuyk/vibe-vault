// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package wrapdispatch

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/agentregistry"
	"github.com/suykerbuyk/vibe-vault/internal/llm"
)

// mockAgenticProvider is a scripted llm.AgenticProvider used by the dispatch
// tests. Each call to RunTools advances scriptIdx and replays a canned turn
// schedule: every script entry is one model turn; the provider invokes the
// caller's executor for any tool_use blocks in that turn before yielding the
// final-turn ToolsResponse to the caller.
type mockAgenticProvider struct {
	turns []mockTurn
	err   error
}

// mockTurn describes one scripted model turn. ToolUses are dispatched through
// the caller-supplied executor in order; if Final is true, the dispatcher
// returns the captured Stop / Content immediately after the executor calls,
// simulating a real provider's terminal response.
type mockTurn struct {
	ToolUses []mockToolUse
	Final    bool
	Stop     string
	Content  []llm.ContentBlock
	Usage    llm.UsageStats
}

// mockToolUse is one tool_use block to dispatch in a turn.
type mockToolUse struct {
	Name  string
	Input json.RawMessage
}

func (m *mockAgenticProvider) Name() string { return "mock-agentic" }

func (m *mockAgenticProvider) ChatCompletion(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return nil, errors.New("ChatCompletion not implemented in mock")
}

func (m *mockAgenticProvider) RunTools(_ context.Context, req llm.ToolsRequest) (*llm.ToolsResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	maxIter := req.MaxIterations
	if maxIter <= 0 {
		maxIter = 10
	}
	var lastUsage llm.UsageStats
	for i, turn := range m.turns {
		if i >= maxIter {
			// Hit the safety cap before reaching the final turn — surface
			// max_tokens like the real Anthropic provider does.
			return &llm.ToolsResponse{
				StopReason: "max_tokens",
				Content:    nil,
				Usage:      lastUsage,
			}, nil
		}
		for _, tu := range turn.ToolUses {
			req.ToolExecutor(tu.Name, tu.Input)
		}
		lastUsage = turn.Usage
		if turn.Final {
			return &llm.ToolsResponse{
				StopReason: turn.Stop,
				Content:    turn.Content,
				Usage:      lastUsage,
			}, nil
		}
	}
	// Ran out of script before any final turn — treat as max_tokens.
	return &llm.ToolsResponse{
		StopReason: "max_tokens",
		Content:    nil,
		Usage:      lastUsage,
	}, nil
}

// fixtureAgent returns a minimal AgentDefinition usable in dispatch tests.
func fixtureAgent() *agentregistry.AgentDefinition {
	return &agentregistry.AgentDefinition{
		Name:                  "wrap-executor",
		Version:               "1.0",
		Description:           "test fixture",
		SystemPrompt:          "You are the wrap-executor.",
		RecommendedModelClass: "sonnet",
	}
}

// fixtureSkeleton returns a minimal skeleton blob.
func fixtureSkeleton() json.RawMessage {
	return json.RawMessage(`{"iter":42,"project":"t","files_changed":["a.go"]}`)
}

// fixtureSynth returns a SynthesizeFunc that records its calls and yields a
// stub bundle so tests can assert direct-routing behaviour.
func fixtureSynth(calls *int) SynthesizeFunc {
	return func(_ context.Context, _ json.RawMessage, _ json.RawMessage) (json.RawMessage, error) {
		*calls++
		return json.RawMessage(`{"bundle":"stub"}`), nil
	}
}

// finishOK builds a wrap_executor_finish call with status=ok + outputs.
func finishOK(outputs string) mockToolUse {
	return mockToolUse{
		Name:  "wrap_executor_finish",
		Input: json.RawMessage(`{"status":"ok","outputs":` + outputs + `}`),
	}
}

// finishEscalate builds a wrap_executor_finish call with status=escalate.
func finishEscalate(reason string) mockToolUse {
	in, _ := json.Marshal(map[string]any{"status": "escalate", "reason": reason})
	return mockToolUse{Name: "wrap_executor_finish", Input: in}
}

// TestDispatch_HappyPath_FinishOk drives the simplest successful path: the
// model fires a single wrap_executor_finish(status="ok") and ends turn.
func TestDispatch_HappyPath_FinishOk(t *testing.T) {
	calls := 0
	provider := &mockAgenticProvider{
		turns: []mockTurn{{
			ToolUses: []mockToolUse{finishOK(`{"iteration_narrative":"did stuff"}`)},
			Final:    true,
			Stop:     "stop",
			Usage:    llm.UsageStats{InputTokens: 100, OutputTokens: 25},
		}},
	}
	out, err := Dispatch(context.Background(), Request{
		SkeletonJSON:    fixtureSkeleton(),
		Tier:            "sonnet",
		ProviderModel:   "anthropic:claude-sonnet-4-6",
		AgentDefinition: fixtureAgent(),
		Provider:        provider,
		SynthesizeFn:    fixtureSynth(&calls),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out.EscalateReason != "" {
		t.Errorf("EscalateReason=%q, want empty", out.EscalateReason)
	}
	if len(out.Outputs) == 0 {
		t.Fatalf("Outputs is empty")
	}
	if out.Metrics.ToolCallCount != 1 {
		t.Errorf("ToolCallCount=%d, want 1", out.Metrics.ToolCallCount)
	}
	if out.Metrics.ProviderModel != "anthropic:claude-sonnet-4-6" {
		t.Errorf("ProviderModel=%q, want anthropic:claude-sonnet-4-6", out.Metrics.ProviderModel)
	}
	if out.Metrics.InputTokens != 100 || out.Metrics.OutputTokens != 25 {
		t.Errorf("token metrics = %+v, want input=100 output=25", out.Metrics)
	}
	if out.Metrics.DurationMs < 0 {
		t.Errorf("DurationMs negative: %d", out.Metrics.DurationMs)
	}
	if calls != 0 {
		t.Errorf("SynthesizeFn was called %d times, want 0", calls)
	}
}

// TestDispatch_EscalateReason verifies that an executor-driven escalate
// surface reaches the caller verbatim.
func TestDispatch_EscalateReason(t *testing.T) {
	calls := 0
	provider := &mockAgenticProvider{
		turns: []mockTurn{{
			ToolUses: []mockToolUse{finishEscalate("too ambiguous")},
			Final:    true,
			Stop:     "stop",
		}},
	}
	out, err := Dispatch(context.Background(), Request{
		SkeletonJSON:    fixtureSkeleton(),
		Tier:            "haiku",
		ProviderModel:   "anthropic:claude-haiku-4-5",
		AgentDefinition: fixtureAgent(),
		Provider:        provider,
		SynthesizeFn:    fixtureSynth(&calls),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out.EscalateReason != "too ambiguous" {
		t.Errorf("EscalateReason=%q, want %q", out.EscalateReason, "too ambiguous")
	}
	if len(out.Outputs) != 0 {
		t.Errorf("Outputs should be empty on escalate; got %s", string(out.Outputs))
	}
}

// TestDispatch_MissingTerminalSignal asserts Decision-8's missing-terminal-
// signal path: model emits end_turn without ever calling wrap_executor_finish.
func TestDispatch_MissingTerminalSignal(t *testing.T) {
	calls := 0
	provider := &mockAgenticProvider{
		turns: []mockTurn{{
			ToolUses: nil,
			Final:    true,
			Stop:     "stop",
			Content:  []llm.ContentBlock{{Type: "text", Text: "I am done."}},
		}},
	}
	out, err := Dispatch(context.Background(), Request{
		SkeletonJSON:    fixtureSkeleton(),
		Tier:            "sonnet",
		ProviderModel:   "anthropic:claude-sonnet-4-6",
		AgentDefinition: fixtureAgent(),
		Provider:        provider,
		SynthesizeFn:    fixtureSynth(&calls),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out.EscalateReason != "missing_terminal_signal" {
		t.Errorf("EscalateReason=%q, want missing_terminal_signal", out.EscalateReason)
	}
}

// TestDispatch_SynthesizeRoundTrip is the OQ-5 direct-helper test: the model
// first invokes vv_synthesize_wrap_bundle, then on a subsequent turn calls
// wrap_executor_finish(status="ok"). Asserts SynthesizeFn was invoked once
// with the prose payload, and the run completes successfully.
func TestDispatch_SynthesizeRoundTrip(t *testing.T) {
	calls := 0
	var capturedProse json.RawMessage
	synth := func(_ context.Context, _ json.RawMessage, prose json.RawMessage) (json.RawMessage, error) {
		calls++
		capturedProse = prose
		return json.RawMessage(`{"bundle":"ok"}`), nil
	}
	provider := &mockAgenticProvider{
		turns: []mockTurn{
			{
				ToolUses: []mockToolUse{{
					Name:  "vv_synthesize_wrap_bundle",
					Input: json.RawMessage(`{"prose":{"iteration_narrative":"draft"}}`),
				}},
			},
			{
				ToolUses: []mockToolUse{finishOK(`{"iteration_narrative":"final"}`)},
				Final:    true,
				Stop:     "stop",
			},
		},
	}
	out, err := Dispatch(context.Background(), Request{
		SkeletonJSON:    fixtureSkeleton(),
		Tier:            "sonnet",
		ProviderModel:   "anthropic:claude-sonnet-4-6",
		AgentDefinition: fixtureAgent(),
		Provider:        provider,
		SynthesizeFn:    synth,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out.EscalateReason != "" {
		t.Errorf("EscalateReason=%q, want empty", out.EscalateReason)
	}
	if calls != 1 {
		t.Errorf("SynthesizeFn called %d times, want 1", calls)
	}
	var prose map[string]string
	if jerr := json.Unmarshal(capturedProse, &prose); jerr != nil {
		t.Fatalf("captured prose not JSON: %v\n%s", jerr, capturedProse)
	}
	if prose["iteration_narrative"] != "draft" {
		t.Errorf("captured prose=%v, want iteration_narrative=draft", prose)
	}
	if out.Metrics.ToolCallCount != 2 {
		t.Errorf("ToolCallCount=%d, want 2", out.Metrics.ToolCallCount)
	}
}

// TestDispatch_ProviderError_BecomesEscalateReason wraps a provider error
// into EscalateReason "provider_error: ..." rather than failing the whole
// dispatch (so multi-tier escalation can advance past a transient failure).
func TestDispatch_ProviderError_BecomesEscalateReason(t *testing.T) {
	calls := 0
	provider := &mockAgenticProvider{err: errors.New("boom")}
	out, err := Dispatch(context.Background(), Request{
		SkeletonJSON:    fixtureSkeleton(),
		Tier:            "sonnet",
		ProviderModel:   "anthropic:claude-sonnet-4-6",
		AgentDefinition: fixtureAgent(),
		Provider:        provider,
		SynthesizeFn:    fixtureSynth(&calls),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.HasPrefix(out.EscalateReason, "provider_error: ") {
		t.Errorf("EscalateReason=%q, want prefix provider_error:", out.EscalateReason)
	}
	if !strings.Contains(out.EscalateReason, "boom") {
		t.Errorf("EscalateReason=%q, want to contain inner error 'boom'", out.EscalateReason)
	}
}

// TestDispatch_RecordsMetrics asserts every metrics field is populated.
func TestDispatch_RecordsMetrics(t *testing.T) {
	calls := 0
	provider := &mockAgenticProvider{
		turns: []mockTurn{{
			ToolUses: []mockToolUse{finishOK(`{"iteration_narrative":"x"}`)},
			Final:    true,
			Stop:     "stop",
			Usage:    llm.UsageStats{InputTokens: 333, OutputTokens: 77},
		}},
	}
	out, err := Dispatch(context.Background(), Request{
		SkeletonJSON:    fixtureSkeleton(),
		Tier:            "opus",
		ProviderModel:   "anthropic:claude-opus-4-7",
		AgentDefinition: fixtureAgent(),
		Provider:        provider,
		SynthesizeFn:    fixtureSynth(&calls),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	m := out.Metrics
	if m.ProviderModel != "anthropic:claude-opus-4-7" {
		t.Errorf("ProviderModel=%q", m.ProviderModel)
	}
	if m.InputTokens != 333 || m.OutputTokens != 77 {
		t.Errorf("tokens = (%d,%d), want (333,77)", m.InputTokens, m.OutputTokens)
	}
	if m.ToolCallCount != 1 {
		t.Errorf("ToolCallCount=%d", m.ToolCallCount)
	}
	if m.DurationMs < 0 {
		t.Errorf("DurationMs negative: %d", m.DurationMs)
	}
}

// TestDispatch_EmitsProgressLines captures ProgressFn invocations and checks
// each tool_use produced a line containing tier/model/tool_call counters.
func TestDispatch_EmitsProgressLines(t *testing.T) {
	calls := 0
	provider := &mockAgenticProvider{
		turns: []mockTurn{
			{ToolUses: []mockToolUse{{
				Name:  "vv_synthesize_wrap_bundle",
				Input: json.RawMessage(`{"prose":{}}`),
			}}},
			{
				ToolUses: []mockToolUse{finishOK(`{"iteration_narrative":"x"}`)},
				Final:    true,
				Stop:     "stop",
			},
		},
	}
	var lines []string
	out, err := Dispatch(context.Background(), Request{
		SkeletonJSON:    fixtureSkeleton(),
		Tier:            "haiku",
		ProviderModel:   "anthropic:claude-haiku-4-5",
		AgentDefinition: fixtureAgent(),
		Provider:        provider,
		SynthesizeFn:    fixtureSynth(&calls),
		ProgressFn:      func(line string) { lines = append(lines, line) },
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(out.Outputs) == 0 {
		t.Fatalf("Outputs is empty")
	}
	if len(lines) != 2 {
		t.Fatalf("expected 2 progress lines, got %d: %v", len(lines), lines)
	}
	for i, line := range lines {
		if !strings.Contains(line, "tier=haiku") {
			t.Errorf("line %d missing tier=haiku: %s", i, line)
		}
		if !strings.Contains(line, "model=anthropic:claude-haiku-4-5") {
			t.Errorf("line %d missing model=...: %s", i, line)
		}
		if !strings.Contains(line, "tool_call=") {
			t.Errorf("line %d missing tool_call counter: %s", i, line)
		}
	}
}

// TestDispatch_EnforceMaxIterations: model loops endlessly with tool_use
// turns; dispatcher must terminate at MaxIterations and report
// max_iterations_exceeded.
func TestDispatch_EnforceMaxIterations(t *testing.T) {
	calls := 0
	infinite := []mockTurn{}
	for i := 0; i < 30; i++ {
		infinite = append(infinite, mockTurn{
			ToolUses: []mockToolUse{{
				Name:  "vv_synthesize_wrap_bundle",
				Input: json.RawMessage(`{"prose":{}}`),
			}},
		})
	}
	provider := &mockAgenticProvider{turns: infinite}
	out, err := Dispatch(context.Background(), Request{
		SkeletonJSON:    fixtureSkeleton(),
		Tier:            "sonnet",
		ProviderModel:   "anthropic:claude-sonnet-4-6",
		AgentDefinition: fixtureAgent(),
		Provider:        provider,
		SynthesizeFn:    fixtureSynth(&calls),
		MaxIterations:   5,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out.EscalateReason != "max_iterations_exceeded" {
		t.Errorf("EscalateReason=%q, want max_iterations_exceeded", out.EscalateReason)
	}
}

// TestDispatch_RejectsNilRequiredFields exercises the input validation.
func TestDispatch_RejectsNilRequiredFields(t *testing.T) {
	cases := []struct {
		name string
		req  Request
	}{
		{
			name: "nil_provider",
			req: Request{
				SkeletonJSON:    fixtureSkeleton(),
				AgentDefinition: fixtureAgent(),
				SynthesizeFn:    func(_ context.Context, _ json.RawMessage, _ json.RawMessage) (json.RawMessage, error) { return nil, nil },
			},
		},
		{
			name: "nil_synth",
			req: Request{
				SkeletonJSON:    fixtureSkeleton(),
				AgentDefinition: fixtureAgent(),
				Provider:        &mockAgenticProvider{},
			},
		},
		{
			name: "nil_agent",
			req: Request{
				SkeletonJSON: fixtureSkeleton(),
				Provider:     &mockAgenticProvider{},
				SynthesizeFn: func(_ context.Context, _ json.RawMessage, _ json.RawMessage) (json.RawMessage, error) { return nil, nil },
			},
		},
		{
			name: "empty_skeleton",
			req: Request{
				AgentDefinition: fixtureAgent(),
				Provider:        &mockAgenticProvider{},
				SynthesizeFn:    func(_ context.Context, _ json.RawMessage, _ json.RawMessage) (json.RawMessage, error) { return nil, nil },
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Dispatch(context.Background(), tc.req); err == nil {
				t.Fatalf("expected error from Dispatch with %s", tc.name)
			}
		})
	}
}

// TestDispatch_OkWithoutOutputs verifies the executor escalation when
// status="ok" but no outputs payload was supplied (Decision-8 boundary case).
func TestDispatch_OkWithoutOutputs(t *testing.T) {
	calls := 0
	provider := &mockAgenticProvider{
		turns: []mockTurn{{
			ToolUses: []mockToolUse{{
				Name:  "wrap_executor_finish",
				Input: json.RawMessage(`{"status":"ok"}`),
			}},
			Final: true,
			Stop:  "stop",
		}},
	}
	out, err := Dispatch(context.Background(), Request{
		SkeletonJSON:    fixtureSkeleton(),
		Tier:            "sonnet",
		ProviderModel:   "anthropic:claude-sonnet-4-6",
		AgentDefinition: fixtureAgent(),
		Provider:        provider,
		SynthesizeFn:    fixtureSynth(&calls),
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out.EscalateReason != "ok_without_outputs" {
		t.Errorf("EscalateReason=%q, want ok_without_outputs", out.EscalateReason)
	}
	if len(out.Outputs) != 0 {
		t.Errorf("Outputs should be empty: %s", string(out.Outputs))
	}
}

// TestDispatch_PriorAttemptsRenderedIntoPrompt verifies the user-prompt
// escalation context surfaces prior tier reasons. Uses a recording provider
// that captures the raw seed messages.
func TestDispatch_PriorAttemptsRenderedIntoPrompt(t *testing.T) {
	calls := 0
	var seenSystem string
	var seenText string
	provider := &recordingProvider{
		respond: func(req llm.ToolsRequest) (*llm.ToolsResponse, error) {
			seenSystem = req.System
			if len(req.Messages) > 0 && len(req.Messages[0].Content) > 0 {
				seenText = req.Messages[0].Content[0].Text
			}
			req.ToolExecutor("wrap_executor_finish", json.RawMessage(`{"status":"ok","outputs":{"iteration_narrative":"x"}}`))
			return &llm.ToolsResponse{StopReason: "stop"}, nil
		},
	}
	_, err := Dispatch(context.Background(), Request{
		SkeletonJSON:    fixtureSkeleton(),
		Tier:            "sonnet",
		ProviderModel:   "anthropic:claude-sonnet-4-6",
		AgentDefinition: fixtureAgent(),
		Provider:        provider,
		SynthesizeFn:    fixtureSynth(&calls),
		PriorAttempts: []PriorAttempt{
			{Tier: "haiku", EscalateReason: "missing_terminal_signal"},
		},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if seenSystem != "You are the wrap-executor." {
		t.Errorf("System=%q, want fixture", seenSystem)
	}
	if !strings.Contains(seenText, "tier=haiku escalated: missing_terminal_signal") {
		t.Errorf("user prompt did not include prior-attempt context:\n%s", seenText)
	}
	if !strings.Contains(seenText, `"iter": 42`) {
		t.Errorf("user prompt did not include skeleton:\n%s", seenText)
	}
}

// recordingProvider is a tiny llm.AgenticProvider that delegates the entire
// turn to a caller-supplied closure. Used by tests that need to inspect the
// ToolsRequest the dispatcher built (system prompt, seed message, tools, etc).
type recordingProvider struct {
	respond func(req llm.ToolsRequest) (*llm.ToolsResponse, error)
}

func (r *recordingProvider) Name() string { return "recording-mock" }
func (r *recordingProvider) ChatCompletion(_ context.Context, _ llm.Request) (*llm.Response, error) {
	return nil, errors.New("not implemented")
}
func (r *recordingProvider) RunTools(_ context.Context, req llm.ToolsRequest) (*llm.ToolsResponse, error) {
	return r.respond(req)
}
