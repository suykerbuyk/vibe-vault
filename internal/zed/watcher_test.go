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

	var called atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Watch(ctx, WatcherConfig{
			DBPath:   dbPath,
			Debounce: 100 * time.Millisecond,
		}, func() {
			called.Add(1)
		})
	}()

	// Give watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Write to WAL file to trigger debounce
	if err := os.WriteFile(walPath, []byte("updated"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce to fire
	time.Sleep(250 * time.Millisecond)

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

	var called atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Watch(ctx, WatcherConfig{
			DBPath:   dbPath,
			Debounce: 200 * time.Millisecond,
		}, func() {
			called.Add(1)
		})
	}()

	time.Sleep(50 * time.Millisecond)

	// Write repeatedly, resetting debounce each time
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(walPath, []byte("update"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// At this point, 250ms of writes have passed. Debounce (200ms) should
	// not have fired during the writes. Wait for it to fire after last write.
	time.Sleep(300 * time.Millisecond)

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

	var called atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- Watch(ctx, WatcherConfig{
			DBPath:   dbPath,
			Debounce: 50 * time.Millisecond,
		}, func() {
			called.Add(1)
		})
	}()

	time.Sleep(50 * time.Millisecond)

	// Write to the main DB file, not the WAL — should be ignored
	if err := os.WriteFile(dbPath, []byte("updated"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write to an unrelated file
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	if got := called.Load(); got != 0 {
		t.Errorf("expected no callbacks, got %d", got)
	}

	cancel()
	<-errCh
}
