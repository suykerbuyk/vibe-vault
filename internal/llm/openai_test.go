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

func TestOpenAIChatCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("expected /chat/completions, got %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %s", r.Header.Get("Authorization"))
		}

		resp := oaiResponse{
			Choices: []oaiChoice{
				{Message: oaiMessage{Content: `{"summary":"test"}`}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p, err := NewOpenAI(server.URL, "test-key", "gpt-4")
	if err != nil {
		t.Fatal(err)
	}

	resp, err := p.ChatCompletion(context.Background(), Request{
		Model:       "gpt-4",
		System:      "You are helpful.",
		UserPrompt:  "Hello",
		Temperature: 0.3,
		JSONMode:    true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if resp.Content != `{"summary":"test"}` {
		t.Fatalf("got %q, want %q", resp.Content, `{"summary":"test"}`)
	}
}

func TestOpenAITransientRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("service unavailable"))
			return
		}
		resp := oaiResponse{
			Choices: []oaiChoice{
				{Message: oaiMessage{Content: "ok"}},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	base, _ := NewOpenAI(server.URL, "test-key", "gpt-4")
	p := WithRetry(base)

	resp, err := p.ChatCompletion(context.Background(), Request{
		System:     "test",
		UserPrompt: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Fatalf("got %q, want %q", resp.Content, "ok")
	}
	if calls != 2 {
		t.Fatalf("expected 2 server calls, got %d", calls)
	}
}

func TestOpenAIPermanentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer server.Close()

	p, _ := NewOpenAI(server.URL, "test-key", "gpt-4")
	_, err := p.ChatCompletion(context.Background(), Request{
		System:     "test",
		UserPrompt: "test",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenAIName(t *testing.T) {
	p, _ := NewOpenAI("http://localhost", "key", "model")
	if p.Name() != "openai" {
		t.Fatalf("got %q, want %q", p.Name(), "openai")
	}
}
