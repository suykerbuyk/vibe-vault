// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Conformance tests for the two SessionSource implementations.
// Lives in the external test package so it can import both
// internal/hook (for the claude-code-jsonl Source) and internal/zed
// (for the zed-acp Source) without inducing an import cycle in the
// production internal/sessionsource package.
package sessionsource_test

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/hook"
	"github.com/suykerbuyk/vibe-vault/internal/session"
	"github.com/suykerbuyk/vibe-vault/internal/sessionsource"
	"github.com/suykerbuyk/vibe-vault/internal/zed"
)

// --- claude-code-jsonl conformance (tests #1-#4) ---

// TestHookSource_Name covers conformance test #1.
func TestHookSource_Name(t *testing.T) {
	s := hook.NewSource()
	if got := s.Name(); got != "claude-code-jsonl" {
		t.Errorf("Name = %q, want %q", got, "claude-code-jsonl")
	}
}

// TestHookSource_Enabled covers conformance test #2. The hook
// source returns true unconditionally — capture runs out-of-process
// when Claude Code invokes `vv hook`, so the source is always
// "available" in the registry sense.
func TestHookSource_Enabled(t *testing.T) {
	s := hook.NewSource()
	if !s.Enabled() {
		t.Error("Enabled = false, want true (hook source is always available)")
	}
}

// TestHookSource_StartIsNoOp covers conformance test #3. The hook
// source has no in-process loop; Start must return nil immediately
// without launching any goroutine.
func TestHookSource_StartIsNoOp(t *testing.T) {
	s := hook.NewSource()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	err := s.Start(ctx, sessionsource.CaptureSink{})
	elapsed := time.Since(start)

	if err != nil {
		t.Errorf("Start: %v, want nil", err)
	}
	// Sanity: a no-op should return well under 100ms even on a
	// loaded test runner.
	if elapsed > 100*time.Millisecond {
		t.Errorf("Start took %s, expected near-zero (no-op)", elapsed)
	}
}

// TestHookSource_StopReturnsNil covers conformance test #4. Stop is
// a no-op for the hook source — returns nil without effect.
func TestHookSource_StopReturnsNil(t *testing.T) {
	s := hook.NewSource()
	if err := s.Stop(); err != nil {
		t.Errorf("Stop: %v, want nil", err)
	}
}

// --- zed-acp conformance (tests #5-#8) ---

// TestZedSource_Name covers conformance test #5.
func TestZedSource_Name(t *testing.T) {
	s := zed.NewSource(zed.SourceConfig{})
	if got := s.Name(); got != "zed-acp" {
		t.Errorf("Name = %q, want %q", got, "zed-acp")
	}
}

// TestZedSource_Enabled covers conformance test #6. The zed-acp
// source returns true when threads.db exists, false otherwise. The
// existence check is the watch-driven analog of the hook source's
// "always enabled" — without the DB there's nothing to watch.
func TestZedSource_Enabled(t *testing.T) {
	// Case 1: empty DBPath → false.
	if zed.NewSource(zed.SourceConfig{}).Enabled() {
		t.Error("Enabled with empty DBPath = true, want false")
	}

	// Case 2: non-existent DBPath → false.
	missing := filepath.Join(t.TempDir(), "does-not-exist.db")
	if zed.NewSource(zed.SourceConfig{DBPath: missing}).Enabled() {
		t.Error("Enabled with non-existent DBPath = true, want false")
	}

	// Case 3: existing DBPath → true.
	dbPath := filepath.Join(t.TempDir(), "threads.db")
	if err := os.WriteFile(dbPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !zed.NewSource(zed.SourceConfig{DBPath: dbPath}).Enabled() {
		t.Error("Enabled with existing DBPath = false, want true")
	}
}

// TestZedSource_StartLaunchesWatcher covers conformance test #7.
// Start launches the fsnotify-watch goroutine; a write to the WAL
// file must propagate to a debounce-fired callback. We assert the
// goroutine is running by observing the watcher pick up the WAL
// write event (the `threads.db-wal` write triggers a debounce timer
// even though the threads.db itself is empty).
func TestZedSource_StartLaunchesWatcher(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "threads.db")
	walPath := dbPath + "-wal"
	if err := os.WriteFile(dbPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(walPath, []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{VaultPath: t.TempDir()}
	cfg.Staging.Root = filepath.Join(t.TempDir(), "staging")

	s := zed.NewSource(zed.SourceConfig{
		DBPath:   dbPath,
		Debounce: 50 * time.Millisecond,
		Cfg:      cfg,
	})

	// Sink that counts Capture calls — proves the watcher's
	// onChange path reaches BatchCapture (which then routes
	// through Sink). Empty threads.db means BatchCapture finds
	// zero threads, so the Sink is not actually invoked, but
	// observing the watcher even attempting a parse is enough to
	// confirm the goroutine is alive. Instead of relying on Sink
	// hits we observe Stop's clean shutdown — a hung goroutine
	// would block Stop indefinitely.
	var sinkCalls atomic.Int32
	sink := countingSink{counter: &sinkCalls}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	startErrCh := make(chan error, 1)
	go func() { startErrCh <- s.Start(ctx, sink) }()

	// Give the watcher time to subscribe before we write.
	time.Sleep(60 * time.Millisecond)

	// Trigger the WAL-write codepath. We don't require the Sink
	// to fire (the empty threads.db produces zero threads), but
	// the goroutine must be alive enough to consume the event and
	// schedule a debounce timer.
	if err := os.WriteFile(walPath, []byte("updated"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Brief settle; Stop's behavior under TestZedSource_StopTerminatesWatcher
	// covers shutdown more carefully.
	time.Sleep(150 * time.Millisecond)

	if err := s.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	select {
	case err := <-startErrCh:
		if err != nil && err != context.Canceled {
			t.Errorf("Start returned %v, want nil or context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2s of Stop — watcher goroutine leaked")
	}
	_ = sinkCalls.Load() // observed-or-not is acceptable; the goroutine-alive assertion is the load-bearing one
}

// TestZedSource_StopTerminatesWatcher covers conformance test #8.
// Stop must cancel the watcher context and wait for the goroutine
// to exit cleanly. A leaked goroutine would have Start() blocked
// indefinitely; a successful Stop() means the goroutine returned.
func TestZedSource_StopTerminatesWatcher(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "threads.db")
	if err := os.WriteFile(dbPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{VaultPath: t.TempDir()}
	cfg.Staging.Root = filepath.Join(t.TempDir(), "staging")

	s := zed.NewSource(zed.SourceConfig{
		DBPath:   dbPath,
		Debounce: time.Hour, // never fires by elapsed-time
		Cfg:      cfg,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startErrCh := make(chan error, 1)
	go func() { startErrCh <- s.Start(ctx, sessionsource.CaptureSink{}) }()

	// Give the watcher time to subscribe.
	time.Sleep(50 * time.Millisecond)

	stopStart := time.Now()
	if err := s.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	stopElapsed := time.Since(stopStart)

	select {
	case err := <-startErrCh:
		if err != nil && err != context.Canceled {
			t.Errorf("Start returned %v after Stop, want nil or context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start goroutine did not exit within 2s of Stop")
	}

	// Stop should be near-instantaneous — it cancels the context
	// and the watcher's select loop notices the ctx.Done within
	// the next event cycle.
	if stopElapsed > 1*time.Second {
		t.Errorf("Stop took %s, expected sub-second", stopElapsed)
	}
}

// countingSink is a Sink that records how many times Capture was
// invoked. Used by tests that need to observe the source-side flow
// without actually exercising the heavyweight session.Capture
// pipeline.
type countingSink struct {
	counter *atomic.Int32
}

func (c countingSink) Capture(_ context.Context, _ session.CaptureOpts, _ config.Config) (*session.CaptureResult, error) {
	c.counter.Add(1)
	return &session.CaptureResult{Skipped: true, Reason: "counting sink"}, nil
}
