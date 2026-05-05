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
	"testing"
)

// TestNewGrok_DefaultBaseURL asserts that an empty baseURL falls back to
// GrokDefaultBaseURL — the contract that lets operators construct a working
// Grok client with just an API key + model.
func TestNewGrok_DefaultBaseURL(t *testing.T) {
	got, err := NewGrok("", "test-key", "grok-3-mini-fast")
	if err != nil {
		t.Fatalf("NewGrok: %v", err)
	}
	if got.baseURL != GrokDefaultBaseURL {
		t.Errorf("baseURL = %q, want %q (GrokDefaultBaseURL)", got.baseURL, GrokDefaultBaseURL)
	}
	if got.apiKey != "test-key" {
		t.Errorf("apiKey = %q, want test-key", got.apiKey)
	}
	if got.model != "grok-3-mini-fast" {
		t.Errorf("model = %q, want grok-3-mini-fast", got.model)
	}
}

// TestNewGrok_OverrideBaseURL asserts that an explicit baseURL flows through
// unchanged (modulo NewOpenAI's trailing-slash trim) — the contract that lets
// operators point at a regional endpoint, a self-hosted mirror, or a test
// server.
func TestNewGrok_OverrideBaseURL(t *testing.T) {
	const override = "https://example-mirror.test/v1"
	got, err := NewGrok(override, "test-key", "grok-3-mini-fast")
	if err != nil {
		t.Fatalf("NewGrok: %v", err)
	}
	if got.baseURL != override {
		t.Errorf("baseURL = %q, want %q (override should win over default)", got.baseURL, override)
	}
}

// TestNewGrok_RoundTrip exercises the full request/response cycle through a
// stub server addressed via the override base URL. Locks: URL path
// (chat/completions), Authorization header (Bearer <key>), and
// Response.Content extraction from the OpenAI-shaped payload.
func TestNewGrok_RoundTrip(t *testing.T) {
	var gotPath string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")

		// Drain the request body so the client sees a clean close.
		_, _ = io.Copy(io.Discard, r.Body)

		resp := oaiResponse{
			Choices: []oaiChoice{
				{Message: oaiMessage{Role: "assistant", Content: "hello from grok"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	p, err := NewGrok(srv.URL, "test-key", "grok-3-mini-fast")
	if err != nil {
		t.Fatalf("NewGrok: %v", err)
	}

	resp, err := p.ChatCompletion(context.Background(), Request{
		System:     "you are a test",
		UserPrompt: "ping",
	})
	if err != nil {
		t.Fatalf("ChatCompletion: %v", err)
	}
	if resp.Content != "hello from grok" {
		t.Errorf("Content = %q, want %q", resp.Content, "hello from grok")
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") || !strings.HasSuffix(gotAuth, "test-key") {
		t.Errorf("Authorization = %q, want \"Bearer test-key\"", gotAuth)
	}
}
