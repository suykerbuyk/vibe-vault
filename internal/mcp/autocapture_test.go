// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
)

func TestStartAutoCapture_MissingDBPath_NoOp(t *testing.T) {
	// Set HOME to a temp dir so DefaultDBPath returns a path in a non-existent dir
	t.Setenv("HOME", t.TempDir())

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	ctx := context.Background()
	errCh := StartAutoCapture(ctx, AutoCaptureConfig{
		DBPath: "", // empty — DefaultDBPath resolves to non-existent dir
		Logger: logger,
		Cfg:    config.Config{VaultPath: t.TempDir()},
	})

	// Channel should be closed immediately (no-op)
	select {
	case err, ok := <-errCh:
		if ok && err != nil {
			t.Errorf("expected closed channel or nil error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("errCh should be closed immediately for missing DB path")
	}

	// Verify it logged a warning about missing directory
	output := buf.String()
	if !strings.Contains(output, "disabling") {
		t.Errorf("expected disabling warning, got: %q", output)
	}
}

func TestStartAutoCapture_ExplicitEmptyDBPath_NoOp(t *testing.T) {
	// Force DefaultDBPath to return empty by making UserHomeDir fail
	t.Setenv("HOME", "")

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	ctx := context.Background()
	errCh := StartAutoCapture(ctx, AutoCaptureConfig{
		DBPath: "",
		Logger: logger,
		Cfg:    config.Config{VaultPath: t.TempDir()},
	})

	select {
	case err, ok := <-errCh:
		if ok && err != nil {
			t.Errorf("expected closed channel or nil error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("errCh should be closed immediately")
	}
}

func TestStartAutoCapture_ContextCancellation(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "threads.db")

	// Create the DB file so the watcher can start
	if err := os.WriteFile(dbPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := StartAutoCapture(ctx, AutoCaptureConfig{
		DBPath:   dbPath,
		Debounce: time.Hour,
		Logger:   logger,
		Cfg:      config.Config{VaultPath: t.TempDir()},
	})

	// Give the goroutine time to start
	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled {
			t.Errorf("expected nil or context.Canceled, got: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("errCh should receive after context cancellation")
	}
}

func TestStartAutoCapture_WALWriteFiresCallback(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "threads.db")
	walPath := dbPath + "-wal"

	if err := os.WriteFile(dbPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(walPath, []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := StartAutoCapture(ctx, AutoCaptureConfig{
		DBPath:   dbPath,
		Debounce: 100 * time.Millisecond,
		Logger:   logger,
		Cfg:      config.Config{VaultPath: t.TempDir()},
	})

	// Give watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Write to WAL to trigger the watcher
	if err := os.WriteFile(walPath, []byte("updated"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait for debounce + processing
	time.Sleep(500 * time.Millisecond)

	// The callback will try to parse the fake DB and fail — that's expected.
	// We just verify it attempted (logged something about reading threads).
	output := buf.String()
	if !strings.Contains(output, "auto-capture") {
		t.Errorf("expected auto-capture log output, got: %q", output)
	}

	cancel()
	<-errCh
}

func TestStartAutoCapture_NoStdoutPollution(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "threads.db")
	walPath := dbPath + "-wal"

	if err := os.WriteFile(dbPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(walPath, []byte("wal"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Capture stdout
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	var stderrBuf bytes.Buffer
	logger := log.New(&stderrBuf, "", 0)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	errCh := StartAutoCapture(ctx, AutoCaptureConfig{
		DBPath:   dbPath,
		Debounce: 100 * time.Millisecond,
		Logger:   logger,
		Cfg:      config.Config{VaultPath: t.TempDir()},
	})

	// Give watcher time to start
	time.Sleep(50 * time.Millisecond)

	// Trigger a WAL write
	if err := os.WriteFile(walPath, []byte("trigger"), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)
	cancel()
	<-errCh

	// Read stdout
	w.Close()
	var stdoutBuf bytes.Buffer
	stdoutBuf.ReadFrom(r)
	os.Stdout = oldStdout

	if stdoutBuf.Len() > 0 {
		t.Errorf("expected no stdout output, got: %q", stdoutBuf.String())
	}
}
