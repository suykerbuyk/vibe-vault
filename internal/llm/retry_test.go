// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package llm

import (
	"context"
	"fmt"
	"testing"
	"time"
)

type mockProvider struct {
	calls    int
	failOnce bool
	err      error
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) ChatCompletion(_ context.Context, _ Request) (*Response, error) {
	m.calls++
	if m.failOnce && m.calls == 1 {
		return nil, &TransientError{Err: fmt.Errorf("server error")}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &Response{Content: "ok"}, nil
}

func TestRetryOnTransientError(t *testing.T) {
	mock := &mockProvider{failOnce: true}
	p := WithRetry(mock)

	resp, err := p.ChatCompletion(context.Background(), Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("got %q, want %q", resp.Content, "ok")
	}
	if mock.calls != 2 {
		t.Fatalf("expected 2 calls, got %d", mock.calls)
	}
}

func TestNoRetryOnPermanentError(t *testing.T) {
	mock := &mockProvider{err: fmt.Errorf("bad request")}
	p := WithRetry(mock)

	_, err := p.ChatCompletion(context.Background(), Request{})
	if err == nil {
		t.Fatal("expected error")
	}
	if mock.calls != 1 {
		t.Fatalf("expected 1 call (no retry), got %d", mock.calls)
	}
}

func TestRetryRespectsContext(t *testing.T) {
	mock := &mockProvider{failOnce: true}
	p := WithRetry(mock)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := p.ChatCompletion(ctx, Request{})
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestRetryName(t *testing.T) {
	mock := &mockProvider{}
	p := WithRetry(mock)
	if p.Name() != "mock" {
		t.Fatalf("got %q, want %q", p.Name(), "mock")
	}
}

func TestNoRetryOnSuccess(t *testing.T) {
	mock := &mockProvider{}
	p := WithRetry(mock)

	resp, err := p.ChatCompletion(context.Background(), Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("got %q, want %q", resp.Content, "ok")
	}
	if mock.calls != 1 {
		t.Fatalf("expected 1 call, got %d", mock.calls)
	}
}

func TestRetryBothFail(t *testing.T) {
	mock := &mockProvider{err: &TransientError{Err: fmt.Errorf("server down")}}
	p := WithRetry(mock)

	start := time.Now()
	_, err := p.ChatCompletion(context.Background(), Request{})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error after both attempts fail")
	}
	if mock.calls != 2 {
		t.Fatalf("expected 2 calls, got %d", mock.calls)
	}
	// Verify backoff delay (should be ~2s, allow 1.5-3.5s for CI variance).
	if elapsed < 1500*time.Millisecond {
		t.Fatalf("retry was too fast: %v", elapsed)
	}
}
