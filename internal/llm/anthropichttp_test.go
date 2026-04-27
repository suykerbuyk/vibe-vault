// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAnthropicHTTPCore_DoSendsExpectedHeaders confirms the three required
// Anthropic Messages API headers are present on every request.
func TestAnthropicHTTPCore_DoSendsExpectedHeaders(t *testing.T) {
	var seen http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	core := newAnthropicHTTPCore(server.URL, "secret-key", "claude-test", nil)
	resp, err := core.do(context.Background(), []byte(`{"k":1}`), nil)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if got := seen.Get("anthropic-version"); got != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want 2023-06-01", got)
	}
	if got := seen.Get("x-api-key"); got != "secret-key" {
		t.Errorf("x-api-key = %q, want secret-key", got)
	}
	if got := seen.Get("content-type"); got != "application/json" {
		t.Errorf("content-type = %q, want application/json", got)
	}
}

// TestAnthropicHTTPCore_DoMergesExtraHeaders verifies extra headers ride
// alongside the defaults (the typical case is anthropic-beta gating tool-use
// preview features).
func TestAnthropicHTTPCore_DoMergesExtraHeaders(t *testing.T) {
	var seen http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	core := newAnthropicHTTPCore(server.URL, "k", "m", nil)
	extra := map[string]string{
		"anthropic-beta": "tools-2024-04-04",
		"x-custom":       "yes",
	}
	resp, err := core.do(context.Background(), []byte(`{}`), extra)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if got := seen.Get("anthropic-beta"); got != "tools-2024-04-04" {
		t.Errorf("anthropic-beta = %q", got)
	}
	if got := seen.Get("x-custom"); got != "yes" {
		t.Errorf("x-custom = %q", got)
	}
	// Defaults still set.
	if got := seen.Get("x-api-key"); got != "k" {
		t.Errorf("x-api-key = %q (default lost)", got)
	}
}

// TestAnthropicHTTPCore_DoPostsToCorrectPath asserts the endpoint is
// /v1/messages regardless of trailing slashes on baseURL.
func TestAnthropicHTTPCore_DoPostsToCorrectPath(t *testing.T) {
	var path, method string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		method = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Test with trailing slash to verify TrimRight in constructor.
	core := newAnthropicHTTPCore(server.URL+"/", "k", "m", nil)
	resp, err := core.do(context.Background(), []byte(`{}`), nil)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	if path != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", path)
	}
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
}

// TestAnthropicHTTPCore_DefaultBaseURL ensures an empty baseURL falls back
// to the production endpoint. We don't actually dial it; we just inspect the
// resolved baseURL field.
func TestAnthropicHTTPCore_DefaultBaseURL(t *testing.T) {
	core := newAnthropicHTTPCore("", "k", "m", nil)
	if !strings.HasPrefix(core.baseURL, "https://api.anthropic.com") {
		t.Errorf("default baseURL = %q", core.baseURL)
	}
}
