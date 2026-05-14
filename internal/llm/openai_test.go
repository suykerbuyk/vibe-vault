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

// TestOpenAIMaxTokens locks the wire-format contract for the MaxTokens
// field: an explicit non-zero value renders as "max_tokens":<n> in the JSON
// body, and the zero value omits the key entirely (omitempty) so the
// upstream service applies its own default.
func TestOpenAIMaxTokens(t *testing.T) {
	cases := []struct {
		name      string
		req       Request
		wantHas   bool
		wantValue int
	}{
		{
			name: "explicit non-zero appears on the wire",
			req: Request{
				System:     "sys",
				UserPrompt: "hi",
				MaxTokens:  1234,
			},
			wantHas:   true,
			wantValue: 1234,
		},
		{
			name: "zero omits max_tokens from the wire",
			req: Request{
				System:     "sys",
				UserPrompt: "hi",
			},
			wantHas: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotRaw []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var err error
				gotRaw, err = io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				resp := oaiResponse{
					Choices: []oaiChoice{{Message: oaiMessage{Content: "ok"}}},
				}
				json.NewEncoder(w).Encode(resp)
			}))
			defer server.Close()

			p, err := NewOpenAI(server.URL, "test-key", "gpt-4")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.ChatCompletion(context.Background(), tc.req); err != nil {
				t.Fatalf("ChatCompletion: %v", err)
			}

			hasField := strings.Contains(string(gotRaw), `"max_tokens"`)
			if hasField != tc.wantHas {
				t.Fatalf("max_tokens present = %v, want %v; body=%s", hasField, tc.wantHas, string(gotRaw))
			}
			if tc.wantHas {
				var probe struct {
					MaxTokens int `json:"max_tokens"`
				}
				if err := json.Unmarshal(gotRaw, &probe); err != nil {
					t.Fatalf("unmarshal body: %v", err)
				}
				if probe.MaxTokens != tc.wantValue {
					t.Errorf("max_tokens = %d, want %d", probe.MaxTokens, tc.wantValue)
				}
			}
		})
	}
}
