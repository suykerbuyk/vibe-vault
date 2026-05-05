// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package sessionsource

import (
	"context"
	"sync/atomic"
	"testing"
)

// fakeSource is a controllable SessionSource for registry tests. It
// counts Start invocations so the double-Start guard test can assert
// that the second call is NOT plumbed through to the source.
type fakeSource struct {
	name    string
	enabled bool
	starts  atomic.Int32
}

func (f *fakeSource) Name() string  { return f.name }
func (f *fakeSource) Enabled() bool { return f.enabled }
func (f *fakeSource) Start(_ context.Context, _ Sink) error {
	f.starts.Add(1)
	return nil
}
func (f *fakeSource) Stop() error { return nil }

// TestRegistry_DoubleStartReturnsError is conformance test #9: a
// second Start on the same registered source returns an error AND
// does NOT launch a second goroutine (the second call must not
// reach source.Start).
func TestRegistry_DoubleStartReturnsError(t *testing.T) {
	r := NewRegistry()
	src := &fakeSource{name: "once", enabled: true}
	if err := r.Register(src); err != nil {
		t.Fatalf("Register: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := r.Start(ctx, "once", CaptureSink{}); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if got := src.starts.Load(); got != 1 {
		t.Fatalf("after first Start, source.Start called %d times, want 1", got)
	}

	// Second Start must error AND must NOT call source.Start again.
	if err := r.Start(ctx, "once", CaptureSink{}); err == nil {
		t.Error("second Start returned nil, want error")
	}
	if got := src.starts.Load(); got != 1 {
		t.Errorf("after second Start, source.Start called %d times, want 1 (no relaunch)", got)
	}
}

// TestRegistry_StopOnNeverStartedReturnsNil is conformance test
// #10: Stop on a never-Started source returns nil — Stop is
// idempotent and safe to call defensively at process shutdown,
// regardless of whether Start was ever invoked.
func TestRegistry_StopOnNeverStartedReturnsNil(t *testing.T) {
	r := NewRegistry()
	src := &fakeSource{name: "idle", enabled: true}
	if err := r.Register(src); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := r.Stop("idle"); err != nil {
		t.Errorf("Stop on never-Started source: %v, want nil", err)
	}
}
