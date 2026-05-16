// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// recordedRequest captures one request/response cycle so individual tests
// can assert on the body shape sent by the provider after each loop turn.
type recordedRequest struct {
	body anthropicAgenticRequest
	raw  []byte
}

// scriptedServer returns an httptest.Server that replies with the supplied
// canned responses in order, and records every inbound request for later
// inspection. Tests use it to drive AnthropicAgentic through a deterministic
// sequence of model turns without ever touching the real Anthropic API.
func scriptedServer(t *testing.T, responses []anthropicAgenticResponse) (*httptest.Server, *[]recordedRequest, *int32) {
	t.Helper()
	var requests []recordedRequest
	var idx int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var decoded anthropicAgenticRequest
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Errorf("unmarshal request body: %v\nraw: %s", err, string(raw))
		}
		requests = append(requests, recordedRequest{body: decoded, raw: raw})

		i := atomic.AddInt32(&idx, 1) - 1
		if int(i) >= len(responses) {
			http.Error(w, "no more scripted responses", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responses[i])
	}))
	return server, &requests, &idx
}

// TestAnthropicAgentic_ImplementsProvider is the compile-time-style assertion
// that AnthropicAgentic satisfies both the legacy Provider interface (so it
// drops in to existing dispatch code) and the new AgenticProvider interface.
func TestAnthropicAgentic_ImplementsProvider(t *testing.T) {
	var _ Provider = (*AnthropicAgentic)(nil)
	var _ AgenticProvider = (*AnthropicAgentic)(nil)
}

// TestAnthropicAgentic_SingleToolCall drives the simplest interesting case:
// turn 1 returns one tool_use; the executor responds; turn 2 returns text
// with end_turn. Asserts the executor was invoked, the second request body
// carried the tool_result, and the final response shape is correct.
func TestAnthropicAgentic_SingleToolCall(t *testing.T) {
	server, requests, _ := scriptedServer(t, []anthropicAgenticResponse{
		{
			Content: []anthropicAgenticContentBlock{
				{Type: "tool_use", ID: "tu_1", Name: "lookup", Input: json.RawMessage(`{"q":"hello"}`)},
			},
			StopReason: "tool_use",
			Usage:      anthropicAgenticUsage{InputTokens: 10, OutputTokens: 5},
		},
		{
			Content: []anthropicAgenticContentBlock{
				{Type: "text", Text: "the answer is 42"},
			},
			StopReason: "end_turn",
			Usage:      anthropicAgenticUsage{InputTokens: 20, OutputTokens: 8},
		},
	})
	defer server.Close()

	var calls []string
	exec := func(name string, input json.RawMessage) (json.RawMessage, bool) {
		calls = append(calls, name+":"+string(input))
		return json.RawMessage(`"42"`), false
	}

	p, err := NewAnthropicAgentic(server.URL, "k", "claude-test")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.RunTools(context.Background(), ToolsRequest{
		System: "you are helpful",
		Messages: []ToolsMessage{
			{Role: "user", Content: []ContentBlock{{Type: "text", Text: "hi"}}},
		},
		Tools:        []ToolSpec{{Name: "lookup", Description: "look it up", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		ToolExecutor: exec,
	})
	if err != nil {
		t.Fatalf("RunTools: %v", err)
	}
	if got := len(calls); got != 1 {
		t.Fatalf("executor calls = %d, want 1", got)
	}
	if calls[0] != `lookup:{"q":"hello"}` {
		t.Errorf("call = %q", calls[0])
	}
	if resp.StopReason != "stop" {
		t.Errorf("StopReason = %q, want stop", resp.StopReason)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "the answer is 42" {
		t.Errorf("Content = %+v", resp.Content)
	}
	if resp.Usage.OutputTokens != 8 {
		t.Errorf("Usage.OutputTokens = %d, want 8", resp.Usage.OutputTokens)
	}

	// Second request must echo the tool_result back to the model.
	if len(*requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(*requests))
	}
	second := (*requests)[1]
	if !strings.Contains(string(second.raw), `"tool_use_id":"tu_1"`) {
		t.Errorf("second request missing tool_use_id: %s", second.raw)
	}
	if !strings.Contains(string(second.raw), `"tool_result"`) {
		t.Errorf("second request missing tool_result type: %s", second.raw)
	}
}

// TestAnthropicAgentic_MultiToolCallSameTurn asserts parallel tool_use
// blocks in one assistant turn produce one user-turn response with both
// tool_result blocks present and matching the originals by tool_use_id.
func TestAnthropicAgentic_MultiToolCallSameTurn(t *testing.T) {
	server, requests, _ := scriptedServer(t, []anthropicAgenticResponse{
		{
			Content: []anthropicAgenticContentBlock{
				{Type: "tool_use", ID: "a", Name: "first", Input: json.RawMessage(`{}`)},
				{Type: "tool_use", ID: "b", Name: "second", Input: json.RawMessage(`{}`)},
			},
			StopReason: "tool_use",
		},
		{
			Content:    []anthropicAgenticContentBlock{{Type: "text", Text: "done"}},
			StopReason: "end_turn",
		},
	})
	defer server.Close()

	var names []string
	exec := func(name string, _ json.RawMessage) (json.RawMessage, bool) {
		names = append(names, name)
		return json.RawMessage(`"ok"`), false
	}

	p, _ := NewAnthropicAgentic(server.URL, "k", "m")
	_, err := p.RunTools(context.Background(), ToolsRequest{
		Messages:     []ToolsMessage{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "go"}}}},
		ToolExecutor: exec,
	})
	if err != nil {
		t.Fatalf("RunTools: %v", err)
	}
	if len(names) != 2 || names[0] != "first" || names[1] != "second" {
		t.Errorf("executor calls = %v, want [first second]", names)
	}
	// Both tool_result ids must be present in the second request body.
	body := string((*requests)[1].raw)
	if !strings.Contains(body, `"tool_use_id":"a"`) || !strings.Contains(body, `"tool_use_id":"b"`) {
		t.Errorf("second request missing one tool_use_id: %s", body)
	}
}

// TestAnthropicAgentic_SequentialToolCalls drives three rounds of tool_use
// before terminating, asserting every executor invocation and the final
// turn count.
func TestAnthropicAgentic_SequentialToolCalls(t *testing.T) {
	mk := func(id, name string) anthropicAgenticResponse {
		return anthropicAgenticResponse{
			Content: []anthropicAgenticContentBlock{
				{Type: "tool_use", ID: id, Name: name, Input: json.RawMessage(`{}`)},
			},
			StopReason: "tool_use",
		}
	}
	server, _, _ := scriptedServer(t, []anthropicAgenticResponse{
		mk("1", "alpha"),
		mk("2", "beta"),
		mk("3", "gamma"),
		{
			Content:    []anthropicAgenticContentBlock{{Type: "text", Text: "fin"}},
			StopReason: "end_turn",
		},
	})
	defer server.Close()

	var seq []string
	exec := func(name string, _ json.RawMessage) (json.RawMessage, bool) {
		seq = append(seq, name)
		return json.RawMessage(`"ok"`), false
	}
	p, _ := NewAnthropicAgentic(server.URL, "k", "m")
	resp, err := p.RunTools(context.Background(), ToolsRequest{
		Messages:     []ToolsMessage{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "go"}}}},
		ToolExecutor: exec,
	})
	if err != nil {
		t.Fatalf("RunTools: %v", err)
	}
	if got := strings.Join(seq, ","); got != "alpha,beta,gamma" {
		t.Errorf("sequence = %q, want alpha,beta,gamma", got)
	}
	if resp.StopReason != "stop" || resp.Content[0].Text != "fin" {
		t.Errorf("terminal response wrong: %+v", resp)
	}
}

// TestAnthropicAgentic_MaxIterationsBreaker asserts the safety cap fires
// before runaway tool-use loops can chew through cost. Caller sets
// MaxIterations = 3 and the server keeps returning tool_use; the loop must
// exit at iteration 3 with StopReason "max_tokens".
func TestAnthropicAgentic_MaxIterationsBreaker(t *testing.T) {
	infinite := anthropicAgenticResponse{
		Content: []anthropicAgenticContentBlock{
			{Type: "tool_use", ID: "x", Name: "t", Input: json.RawMessage(`{}`)},
		},
		StopReason: "tool_use",
	}
	server, requests, _ := scriptedServer(t, []anthropicAgenticResponse{infinite, infinite, infinite, infinite, infinite, infinite})
	defer server.Close()

	exec := func(_ string, _ json.RawMessage) (json.RawMessage, bool) {
		return json.RawMessage(`"ok"`), false
	}
	p, _ := NewAnthropicAgentic(server.URL, "k", "m")
	resp, err := p.RunTools(context.Background(), ToolsRequest{
		Messages:      []ToolsMessage{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "loop"}}}},
		ToolExecutor:  exec,
		MaxIterations: 3,
	})
	if err != nil {
		t.Fatalf("RunTools: %v", err)
	}
	if resp.StopReason != "max_tokens" {
		t.Errorf("StopReason = %q, want max_tokens", resp.StopReason)
	}
	if got := len(*requests); got != 3 {
		t.Errorf("requests = %d, want 3", got)
	}
}

// TestAnthropicAgentic_DefaultsMaxIterations asserts that MaxIterations == 0
// is treated as the package default of 10. We script 11 tool-use turns and
// expect the breaker to fire at request 10.
func TestAnthropicAgentic_DefaultsMaxIterations(t *testing.T) {
	tu := anthropicAgenticResponse{
		Content: []anthropicAgenticContentBlock{
			{Type: "tool_use", ID: "x", Name: "t", Input: json.RawMessage(`{}`)},
		},
		StopReason: "tool_use",
	}
	canned := make([]anthropicAgenticResponse, 11)
	for i := range canned {
		canned[i] = tu
	}
	server, requests, _ := scriptedServer(t, canned)
	defer server.Close()

	exec := func(_ string, _ json.RawMessage) (json.RawMessage, bool) {
		return json.RawMessage(`"ok"`), false
	}
	p, _ := NewAnthropicAgentic(server.URL, "k", "m")
	resp, err := p.RunTools(context.Background(), ToolsRequest{
		Messages:     []ToolsMessage{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "go"}}}},
		ToolExecutor: exec,
		// MaxIterations omitted (0) — should default to 10.
	})
	if err != nil {
		t.Fatalf("RunTools: %v", err)
	}
	if resp.StopReason != "max_tokens" {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if got := len(*requests); got != defaultMaxIterations {
		t.Errorf("requests = %d, want %d", got, defaultMaxIterations)
	}
}

// TestAnthropicAgentic_ErrorToolResultRoundtrip verifies that an executor
// returning isError = true causes the next request body to carry the
// tool_result with is_error: true.
func TestAnthropicAgentic_ErrorToolResultRoundtrip(t *testing.T) {
	server, requests, _ := scriptedServer(t, []anthropicAgenticResponse{
		{
			Content: []anthropicAgenticContentBlock{
				{Type: "tool_use", ID: "boom", Name: "bad", Input: json.RawMessage(`{}`)},
			},
			StopReason: "tool_use",
		},
		{
			Content:    []anthropicAgenticContentBlock{{Type: "text", Text: "recovered"}},
			StopReason: "end_turn",
		},
	})
	defer server.Close()

	exec := func(_ string, _ json.RawMessage) (json.RawMessage, bool) {
		return json.RawMessage(`"failure detail"`), true
	}
	p, _ := NewAnthropicAgentic(server.URL, "k", "m")
	_, err := p.RunTools(context.Background(), ToolsRequest{
		Messages:     []ToolsMessage{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "go"}}}},
		ToolExecutor: exec,
	})
	if err != nil {
		t.Fatalf("RunTools: %v", err)
	}
	body := string((*requests)[1].raw)
	if !strings.Contains(body, `"is_error":true`) {
		t.Errorf("second request missing is_error:true: %s", body)
	}
}

// TestAnthropicAgentic_RespectsContext asserts a canceled context surfaces
// as an error from RunTools.
func TestAnthropicAgentic_RespectsContext(t *testing.T) {
	// Server that never replies; any pending request should error when ctx is canceled.
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	exec := func(_ string, _ json.RawMessage) (json.RawMessage, bool) { return nil, false }
	p, _ := NewAnthropicAgentic(server.URL, "k", "m")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled
	_, err := p.RunTools(ctx, ToolsRequest{
		Messages:     []ToolsMessage{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "x"}}}},
		ToolExecutor: exec,
	})
	if err == nil {
		t.Fatal("expected error from canceled context, got nil")
	}
}

// TestAnthropicAgentic_PassesSystemPrompt asserts the System field is
// forwarded to the wire-level "system" field on every request.
func TestAnthropicAgentic_PassesSystemPrompt(t *testing.T) {
	server, requests, _ := scriptedServer(t, []anthropicAgenticResponse{
		{Content: []anthropicAgenticContentBlock{{Type: "text", Text: "ok"}}, StopReason: "end_turn"},
	})
	defer server.Close()
	p, _ := NewAnthropicAgentic(server.URL, "k", "m")
	_, err := p.RunTools(context.Background(), ToolsRequest{
		System:       "you are a strict reviewer",
		Messages:     []ToolsMessage{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "review"}}}},
		ToolExecutor: func(_ string, _ json.RawMessage) (json.RawMessage, bool) { return nil, false },
	})
	if err != nil {
		t.Fatalf("RunTools: %v", err)
	}
	if got := (*requests)[0].body.System; got != "you are a strict reviewer" {
		t.Errorf("system = %q", got)
	}
}

// TestAnthropicAgentic_PassesToolSpecs asserts the Tools catalogue is
// forwarded with name, description, and input_schema present.
func TestAnthropicAgentic_PassesToolSpecs(t *testing.T) {
	server, requests, _ := scriptedServer(t, []anthropicAgenticResponse{
		{Content: []anthropicAgenticContentBlock{{Type: "text", Text: "ok"}}, StopReason: "end_turn"},
	})
	defer server.Close()
	p, _ := NewAnthropicAgentic(server.URL, "k", "m")
	_, err := p.RunTools(context.Background(), ToolsRequest{
		Messages: []ToolsMessage{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "x"}}}},
		Tools: []ToolSpec{
			{Name: "search", Description: "search the web", InputSchema: json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}}}`)},
		},
		ToolExecutor: func(_ string, _ json.RawMessage) (json.RawMessage, bool) { return nil, false },
	})
	if err != nil {
		t.Fatalf("RunTools: %v", err)
	}
	body := string((*requests)[0].raw)
	if !strings.Contains(body, `"name":"search"`) {
		t.Errorf("body missing tool name: %s", body)
	}
	if !strings.Contains(body, `"description":"search the web"`) {
		t.Errorf("body missing description: %s", body)
	}
	if !strings.Contains(body, `"input_schema"`) || !strings.Contains(body, `"properties"`) {
		t.Errorf("body missing input_schema: %s", body)
	}
}

// TestAnthropicAgentic_RequiresToolExecutor asserts the provider rejects
// requests that omit the dispatcher rather than panicking on a nil call.
func TestAnthropicAgentic_RequiresToolExecutor(t *testing.T) {
	p, _ := NewAnthropicAgentic("http://localhost", "k", "m")
	_, err := p.RunTools(context.Background(), ToolsRequest{
		Messages: []ToolsMessage{{Role: "user"}},
	})
	if err == nil {
		t.Fatal("expected error for missing ToolExecutor, got nil")
	}
}

// TestAnthropicAgentic_DelegatesChatCompletion asserts the embedded *Anthropic
// path serves single-turn ChatCompletion calls correctly.
func TestAnthropicAgentic_DelegatesChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropicResponse{
			Content: []anthropicContentBlock{{Type: "text", Text: "single-turn-ok"}},
		})
	}))
	defer server.Close()

	p, _ := NewAnthropicAgentic(server.URL, "k", "m")
	if p.Name() != "anthropic" {
		t.Errorf("Name = %q", p.Name())
	}
	resp, err := p.ChatCompletion(context.Background(), Request{System: "s", UserPrompt: "u"})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.Content != "single-turn-ok" {
		t.Errorf("Content = %q", resp.Content)
	}
}

// TestToWireBlockRoundtrip drives every Type branch in the wire conversion
// helpers so future regressions can't slip through coverage. text, tool_use,
// tool_result, and an unknown fallback all exercise both the to-wire and
// from-wire halves.
func TestToWireBlockRoundtrip(t *testing.T) {
	cases := []ContentBlock{
		{Type: "text", Text: "hello"},
		{Type: "tool_use", ToolUseID: "tu_42", ToolName: "lookup", ToolInput: json.RawMessage(`{"q":"x"}`)},
		{Type: "tool_use", ToolUseID: "tu_43", ToolName: "noinput"}, // empty Input -> default {}
		{Type: "tool_result", ToolUseID: "tu_42", ToolResult: json.RawMessage(`"ok"`), IsError: false},
		{Type: "tool_result", ToolUseID: "tu_42", ToolResult: json.RawMessage(`"bad"`), IsError: true},
		{Type: "unknown_kind", Text: "passthrough"},
	}
	for _, in := range cases {
		wire := toWireBlock(in)
		if in.Type == "tool_use" && len(wire.Input) == 0 {
			t.Errorf("tool_use Input should default to {}, got empty for %+v", in)
		}
		got := fromWireBlocks([]anthropicAgenticContentBlock{wire})
		if len(got) != 1 {
			t.Fatalf("fromWireBlocks returned %d blocks", len(got))
		}
		if got[0].Type != in.Type {
			t.Errorf("Type round-trip: got %q, want %q", got[0].Type, in.Type)
		}
	}
}

// TestToWireMessageRewritesToolRole asserts the "tool" portability role is
// rewritten to "user" since Anthropic encodes tool results as user messages.
func TestToWireMessageRewritesToolRole(t *testing.T) {
	wire := toWireMessage(ToolsMessage{
		Role: "tool",
		Content: []ContentBlock{
			{Type: "tool_result", ToolUseID: "x", ToolResult: json.RawMessage(`"ok"`)},
		},
	})
	if wire.Role != "user" {
		t.Errorf("tool role rewritten to %q, want user", wire.Role)
	}
}

// TestAnthropicAgentic_PropagatesAPIError asserts a non-2xx, non-transient
// status surfaces as a plain error (not wrapped in TransientError).
func TestAnthropicAgentic_PropagatesAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"bad model"}}`))
	}))
	defer server.Close()

	p, _ := NewAnthropicAgentic(server.URL, "k", "m")
	_, err := p.RunTools(context.Background(), ToolsRequest{
		Messages:     []ToolsMessage{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "x"}}}},
		ToolExecutor: func(_ string, _ json.RawMessage) (json.RawMessage, bool) { return nil, false },
	})
	if err == nil {
		t.Fatal("expected error from 400 response")
	}
	if !strings.Contains(err.Error(), "status 400") {
		t.Errorf("err = %v, want status 400", err)
	}
}

// TestAnthropicAgentic_TransientStatusIsRetryable asserts a 5xx surfaces as
// *TransientError so the WithRetry wrapper kicks in.
func TestAnthropicAgentic_TransientStatusIsRetryable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":{"message":"overloaded"}}`))
	}))
	defer server.Close()

	p, _ := NewAnthropicAgentic(server.URL, "k", "m")
	_, err := p.RunTools(context.Background(), ToolsRequest{
		Messages:     []ToolsMessage{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "x"}}}},
		ToolExecutor: func(_ string, _ json.RawMessage) (json.RawMessage, bool) { return nil, false },
	})
	var te *TransientError
	if err == nil {
		t.Fatal("expected error")
	}
	if !asTransient(err, &te) {
		t.Errorf("err = %v, want *TransientError", err)
	}
}

// asTransient is a tiny errors.As helper to keep the test body readable.
func asTransient(err error, target **TransientError) bool {
	for e := err; e != nil; {
		if te, ok := e.(*TransientError); ok {
			*target = te
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := e.(unwrapper)
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// TestAnthropicAgentic_ObjectResultEncodedAsString asserts that when a
// ToolExecutor returns a JSON object (not a JSON string), RunTools re-encodes
// it as a JSON string before sending it to the Anthropic API. Anthropic's
// tool_result.content must be a string or list of content blocks; raw JSON
// objects cause a 400. This covers the FillBundle() return-value path.
func TestAnthropicAgentic_ObjectResultEncodedAsString(t *testing.T) {
	server, requests, _ := scriptedServer(t, []anthropicAgenticResponse{
		{
			Content:    []anthropicAgenticContentBlock{{Type: "tool_use", ID: "tu_obj", Name: "fill", Input: json.RawMessage(`{}`)}},
			StopReason: "tool_use",
		},
		{
			Content:    []anthropicAgenticContentBlock{{Type: "text", Text: "done"}},
			StopReason: "end_turn",
		},
	})
	defer server.Close()

	exec := func(_ string, _ json.RawMessage) (json.RawMessage, bool) {
		// Simulates FillBundle returning a JSON object.
		return json.RawMessage(`{"iter_title":"Test","prose":"body"}`), false
	}
	p, _ := NewAnthropicAgentic(server.URL, "k", "m")
	_, err := p.RunTools(context.Background(), ToolsRequest{
		Messages:     []ToolsMessage{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "go"}}}},
		ToolExecutor: exec,
	})
	if err != nil {
		t.Fatalf("RunTools: %v", err)
	}
	body := string((*requests)[1].raw)
	// The content must be a JSON string (starts with "), not a raw object.
	if !strings.Contains(body, `"content":"{`) {
		t.Errorf("object result not re-encoded as JSON string in: %s", body)
	}
}

// TestNormalizeStopReason exercises the small mapping table directly so
// future additions don't silently drop wire values.
func TestNormalizeStopReason(t *testing.T) {
	cases := map[string]string{
		"end_turn":      "stop",
		"stop_sequence": "stop",
		"":              "stop",
		"max_tokens":    "max_tokens",
		"tool_use":      "tool_use",
		"refusal":       "stop", // unknown falls back
	}
	for in, want := range cases {
		if got := normalizeStopReason(in); got != want {
			t.Errorf("normalizeStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}
