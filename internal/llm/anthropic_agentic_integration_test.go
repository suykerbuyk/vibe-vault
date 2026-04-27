// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

//go:build integration_anthropic

package llm

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestIntegration_AnthropicAgentic_SmokeRoundtrip is a live-API smoke test
// gated behind the integration_anthropic build tag and the ANTHROPIC_API_KEY
// environment variable. It exists to catch wire-format regressions that
// httptest-driven unit tests can't detect — e.g. a typo in the JSON schema
// for the tool spec, or a header that Anthropic silently rejects.
//
// Cost is bounded by:
//   - Using claude-haiku-4-5 (the cheapest production model).
//   - Setting MaxIterations = 2 (single tool round-trip + final text turn).
//   - Sending a trivial single-token prompt.
//
// The test reads ANTHROPIC_API_KEY at call time and skips if unset, so it's
// safe to run in CI even when secrets aren't injected.
func TestIntegration_AnthropicAgentic_SmokeRoundtrip(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live integration test")
	}

	p, err := NewAnthropicAgentic("", apiKey, "claude-haiku-4-5")
	if err != nil {
		t.Fatalf("NewAnthropicAgentic: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var executorCalls int
	exec := func(name string, _ json.RawMessage) (json.RawMessage, bool) {
		executorCalls++
		// Whatever input the model passes for "echo", just hand back a fixed value.
		return json.RawMessage(`"42"`), false
	}

	resp, err := p.RunTools(ctx, ToolsRequest{
		System: "You answer concisely. When you need a number, call the echo tool.",
		Messages: []ToolsMessage{
			{Role: "user", Content: []ContentBlock{
				{Type: "text", Text: "Use the echo tool to fetch the canonical number, then state it."},
			}},
		},
		Tools: []ToolSpec{
			{
				Name:        "echo",
				Description: "Returns the canonical number.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
			},
		},
		MaxIterations: 4,
		ToolExecutor:  exec,
	})
	if err != nil {
		t.Fatalf("RunTools: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}
	if len(resp.Content) == 0 {
		t.Errorf("empty content slice")
	}
	// Sanity: at least one of the response blocks should be non-empty text
	// once the loop terminates. We don't assert on the literal content
	// because models drift; we just confirm the round-trip parsed.
	t.Logf("StopReason=%s ExecutorCalls=%d Usage=%+v",
		resp.StopReason, executorCalls, resp.Usage)
}
