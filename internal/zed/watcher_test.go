// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func waitForCondition(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

func TestWatch_DebounceFiresAfterQuiet(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "threads.db")
	walPath := dbPath + "-wal"

	// Create the files so fsnotify has something to watch
	if err := os.WriteFile(dbPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(walPath, []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}

	clock := newFakeClock()

	var called atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Watch(ctx, WatcherConfig{
			DBPath:   dbPath,
			Debounce: 100 * time.Millisecond,
			Clock:    clock,
		}, func() {
			called.Add(1)
		})
	}()

	// Give watcher time to start (fsnotify subscription)
	time.Sleep(50 * time.Millisecond)

	// Write to WAL file to trigger debounce
	if err := os.WriteFile(walPath, []byte("updated"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Steady-state wait: a single timer should be pending after the write.
	waitForCondition(t, 2*time.Second, func() bool {
		return clock.Pending() == 1
	})

	// Advance the fake clock past the debounce window; the callback fires
	// synchronously inside Advance.
	clock.Advance(100 * time.Millisecond)

	if got := called.Load(); got != 1 {
		t.Errorf("expected callback to fire once, got %d", got)
	}

	cancel()
	<-errCh
}

func TestWatch_DebounceResetsOnRepeatedWrites(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "threads.db")
	walPath := dbPath + "-wal"

	if err := os.WriteFile(dbPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(walPath, []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}

	clock := newFakeClock()

	var called atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Watch(ctx, WatcherConfig{
			DBPath:   dbPath,
			Debounce: 200 * time.Millisecond,
			Clock:    clock,
		}, func() {
			called.Add(1)
		})
	}()

	// Give watcher time to start (fsnotify subscription)
	time.Sleep(50 * time.Millisecond)

	// Write repeatedly. After each write, best-effort serialization waits
	// until at least i+1 timers have been registered, ensuring the watcher
	// has observed the event before we issue the next write.
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(walPath, []byte("update"), 0o644); err != nil {
			t.Fatal(err)
		}
		waitForCondition(t, 2*time.Second, func() bool {
			return clock.Registered() >= i+1
		})
	}

	// Steady-state wait: exactly one timer pending and at least 5 registered.
	// fsnotify on Linux may deliver 1-2 IN_MODIFY events per os.WriteFile
	// (O_TRUNC + Write), so Registered() may exceed 5; what matters is that
	// all in-flight events have been processed and only the most recent
	// timer remains pending before we Advance.
	waitForCondition(t, 2*time.Second, func() bool {
		return clock.Pending() == 1 && clock.Registered() >= 5
	})

	// Advance past the debounce window; the surviving timer fires.
	clock.Advance(200 * time.Millisecond)

	if got := called.Load(); got != 1 {
		t.Errorf("expected callback to fire once after debounce, got %d", got)
	}

	cancel()
	<-errCh
}

func TestWatch_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "threads.db")

	if err := os.WriteFile(dbPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- Watch(ctx, WatcherConfig{
			DBPath:   dbPath,
			Debounce: time.Hour, // should never fire
		}, func() {
			t.Error("callback should not fire")
		})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-errCh
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestWatch_IgnoresNonWALWrites(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "threads.db")

	if err := os.WriteFile(dbPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	clock := newFakeClock()

	var called atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Watch(ctx, WatcherConfig{
			DBPath:   dbPath,
			Debounce: 50 * time.Millisecond,
			Clock:    clock,
		}, func() {
			called.Add(1)
		})
	}()

	// Give watcher time to start (fsnotify subscription)
	time.Sleep(50 * time.Millisecond)

	// Write to the main DB file, not the WAL — should be ignored
	if err := os.WriteFile(dbPath, []byte("updated"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write to an unrelated file
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// No observable signal exists for filtered-out events (no timer is
	// registered, no callback runs), so we wall-clock sleep to let
	// fsnotify drain its event channel.
	time.Sleep(100 * time.Millisecond)

	if got := clock.Registered(); got != 0 {
		t.Errorf("expected no timers to be registered, got %d", got)
	}

	if got := called.Load(); got != 0 {
		t.Errorf("expected no callbacks, got %d", got)
	}

	cancel()
	<-errCh
}
