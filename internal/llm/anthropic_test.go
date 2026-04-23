// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("expected /v1/messages, got %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key test-key, got %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("expected anthropic-version 2023-06-01, got %s", r.Header.Get("anthropic-version"))
		}

		// Verify request body has system as top-level field.
		var req anthropicRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.System == "" {
			t.Error("expected non-empty system field")
		}

		resp := anthropicResponse{
			Content: []anthropicContentBlock{
				{Type: "text", Text: `{"summary":"from claude"}`},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p, err := NewAnthropic(server.URL, "test-key", "claude-sonnet-4-6")
	if err != nil {
		t.Fatal(err)
	}

	resp, err := p.ChatCompletion(context.Background(), Request{
		System:      "You are helpful.",
		UserPrompt:  "Hello",
		Temperature: 0.3,
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Content != `{"summary":"from claude"}` {
		t.Fatalf("got %q", resp.Content)
	}
}

func TestAnthropicName(t *testing.T) {
	p, _ := NewAnthropic("http://localhost", "key", "model")
	if p.Name() != "anthropic" {
		t.Fatalf("got %q", p.Name())
	}
}

func TestStripJSONFence(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"no fence", `{"a":1}`, `{"a":1}`},
		{"json fence", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"bare fence", "```\n{\"a\":1}\n```", `{"a":1}`},
		{"fence with trailing ws", "```json\n{\"a\":1}\n```  \n", `{"a":1}`},
		{"fence with leading ws", "\n\n```json\n{\"a\":1}\n```", `{"a":1}`},
		{"multi-line json inside fence", "```json\n{\n  \"a\": 1\n}\n```", "{\n  \"a\": 1\n}"},
		{"empty", "", ""},
		{"backticks not at start", "prefix ```json {} ```", "prefix ```json {} ```"},
		{"preamble before json — kept as-is (not a fence)", "Here is the JSON:\n{\"a\":1}", "Here is the JSON:\n{\"a\":1}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripJSONFence(tc.in)
			if got != tc.want {
				t.Errorf("stripJSONFence(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAnthropicJSONModeStripsFences(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Simulate Claude wrapping its JSON output in a ```json fence.
		resp := map[string]any{
			"content": []map[string]string{
				{"type": "text", "text": "```json\n{\"summary\":\"wrapped\"}\n```"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p, _ := NewAnthropic(server.URL, "test-key", "model")
	resp, err := p.ChatCompletion(context.Background(), Request{
		System:     "You are helpful.",
		UserPrompt: "Hello",
		JSONMode:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != `{"summary":"wrapped"}` {
		t.Fatalf("JSONMode did not strip fence: got %q", resp.Content)
	}

	// Without JSONMode the response passes through verbatim.
	resp2, err := p.ChatCompletion(context.Background(), Request{
		System:     "You are helpful.",
		UserPrompt: "Hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp2.Content != "```json\n{\"summary\":\"wrapped\"}\n```" {
		t.Fatalf("non-JSONMode should not strip: got %q", resp2.Content)
	}
}
