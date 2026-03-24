// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"io"
	"log"
	"testing"
)

func TestRunChecksAllPass(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	srv := NewServer(ServerInfo{Name: "test-server", Version: "0.1.0"}, logger)
	srv.RegisterTool(Tool{
		Definition: ToolDef{
			Name:        "echo",
			Description: "echoes input",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
		},
		Handler: func(params json.RawMessage) (string, error) { return "ok", nil },
	})
	srv.RegisterPrompt(Prompt{
		Definition: PromptDef{
			Name:        "test_prompt",
			Description: "a test prompt",
		},
		Handler: func(args map[string]string) (PromptsGetResult, error) {
			return PromptsGetResult{
				Messages: []PromptMessage{{Role: "user", Content: ContentBlock{Type: "text", Text: "hello"}}},
			}, nil
		},
	})
	srv.SetInstructions("Test instructions")

	results := RunChecks(srv)
	for _, r := range results {
		if !r.Pass {
			t.Errorf("[FAIL] %s: %s", r.Name, r.Detail)
		}
	}
	if len(results) != 11 {
		t.Errorf("expected 11 checks, got %d", len(results))
	}
}

func TestRunChecksEmptyServer(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	srv := NewServer(ServerInfo{Name: "empty", Version: "0.0.0"}, logger)
	// No tools, no prompts, no instructions.

	results := RunChecks(srv)

	// Find the empty-server check.
	var found bool
	for _, r := range results {
		if r.Name == "empty-server tools/list returns [] not null" {
			found = true
			if !r.Pass {
				t.Errorf("empty-server check failed: %s", r.Detail)
			}
		}
	}
	if !found {
		t.Error("empty-server check not found in results")
	}
}
