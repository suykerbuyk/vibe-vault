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
