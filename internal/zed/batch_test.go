// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/index"
)

func batchTestConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{VaultPath: t.TempDir()}
}

func makeThread(id string, msgs int) Thread {
	var messages []ZedMessage
	for i := 0; i < msgs; i++ {
		if i%2 == 0 {
			messages = append(messages, ZedMessage{
				Role:    "user",
				Content: []ZedContent{{Type: "text", Text: "user message"}},
			})
		} else {
			messages = append(messages, ZedMessage{
				Role:    "assistant",
				Content: []ZedContent{{Type: "text", Text: "assistant response"}},
			})
		}
	}
	return Thread{
		ID:        id,
		Summary:   "Test thread " + id,
		UpdatedAt: time.Now(),
		Messages:  messages,
		Model:     &ZedModel{Provider: "anthropic", Model: "claude-sonnet-4-5"},
		ProjectSnapshot: &ProjectSnapshot{
			WorktreeSnapshots: []WorktreeSnapshot{
				{WorktreePath: "/home/user/code/testproj", GitBranch: "main"},
			},
		},
	}
}

func TestBatchCapture_Basic(t *testing.T) {
	cfg := batchTestConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "testproj", "sessions"), 0o755)
	os.MkdirAll(cfg.StateDir(), 0o755)

	logger := log.New(os.Stderr, "", 0)
	threads := []Thread{makeThread("abc-123", 6)}

	result := BatchCapture(BatchCaptureOpts{
		Threads: threads,
		DBPath:  "/tmp/threads.db",
		Cfg:     cfg,
		Logger:  logger,
	})

	if result.Processed != 1 {
		t.Errorf("Processed = %d, want 1", result.Processed)
	}
	if result.Errors != 0 {
		t.Errorf("Errors = %d, want 0", result.Errors)
	}

	// Verify index entry was created
	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	if _, ok := idx.Entries["zed:abc-123"]; !ok {
		t.Error("expected index entry for zed:abc-123")
	}
}

func TestBatchCapture_Dedup(t *testing.T) {
	cfg := batchTestConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "testproj", "sessions"), 0o755)
	os.MkdirAll(cfg.StateDir(), 0o755)

	logger := log.New(os.Stderr, "", 0)
	threads := []Thread{makeThread("dedup-1", 6)}

	// First capture
	r1 := BatchCapture(BatchCaptureOpts{
		Threads: threads,
		DBPath:  "/tmp/threads.db",
		Cfg:     cfg,
		Logger:  logger,
	})
	if r1.Processed != 1 {
		t.Fatalf("first capture: Processed = %d, want 1", r1.Processed)
	}

	// Second capture — should be skipped (dedup)
	r2 := BatchCapture(BatchCaptureOpts{
		Threads: threads,
		DBPath:  "/tmp/threads.db",
		Cfg:     cfg,
		Logger:  logger,
	})
	if r2.Skipped != 1 {
		t.Errorf("second capture: Skipped = %d, want 1", r2.Skipped)
	}
	if r2.Processed != 0 {
		t.Errorf("second capture: Processed = %d, want 0", r2.Processed)
	}
}

func TestBatchCapture_AutoCaptured(t *testing.T) {
	cfg := batchTestConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "testproj", "sessions"), 0o755)
	os.MkdirAll(cfg.StateDir(), 0o755)

	logger := log.New(os.Stderr, "", 0)
	threads := []Thread{makeThread("auto-1", 6)}

	result := BatchCapture(BatchCaptureOpts{
		Threads:      threads,
		DBPath:       "/tmp/threads.db",
		AutoCaptured: true,
		Cfg:          cfg,
		Logger:       logger,
	})

	if result.Processed != 1 {
		t.Fatalf("Processed = %d, want 1", result.Processed)
	}

	// Verify the note has status: auto-captured
	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	entry, ok := idx.Entries["zed:auto-1"]
	if !ok {
		t.Fatal("expected index entry for zed:auto-1")
	}
	notePath := filepath.Join(cfg.VaultPath, entry.NotePath)
	data, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("read note: %v", err)
	}
	if !strings.Contains(string(data), "status: auto-captured") {
		t.Error("note missing 'status: auto-captured' in frontmatter")
	}
}

func TestBatchCapture_ProjectFilter(t *testing.T) {
	cfg := batchTestConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "testproj", "sessions"), 0o755)
	os.MkdirAll(cfg.StateDir(), 0o755)

	logger := log.New(os.Stderr, "", 0)
	threads := []Thread{makeThread("filter-1", 6)}

	result := BatchCapture(BatchCaptureOpts{
		Threads:       threads,
		DBPath:        "/tmp/threads.db",
		ProjectFilter: "otherproj",
		Cfg:           cfg,
		Logger:        logger,
	})

	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (filtered out)", result.Skipped)
	}
	if result.Processed != 0 {
		t.Errorf("Processed = %d, want 0", result.Processed)
	}
}

func TestBatchCapture_EmptyThreads(t *testing.T) {
	cfg := batchTestConfig(t)
	os.MkdirAll(cfg.StateDir(), 0o755)

	logger := log.New(os.Stderr, "", 0)

	result := BatchCapture(BatchCaptureOpts{
		Threads: nil,
		DBPath:  "/tmp/threads.db",
		Cfg:     cfg,
		Logger:  logger,
	})

	if result.Processed != 0 || result.Skipped != 0 || result.Errors != 0 {
		t.Errorf("expected all zeros for empty input, got %+v", result)
	}
}

func TestBatchCapture_TrivialThread(t *testing.T) {
	cfg := batchTestConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "testproj", "sessions"), 0o755)
	os.MkdirAll(cfg.StateDir(), 0o755)

	logger := log.New(os.Stderr, "", 0)
	// Thread with only 2 messages (1 user, 1 assistant) — trivial
	threads := []Thread{makeThread("trivial-1", 2)}

	result := BatchCapture(BatchCaptureOpts{
		Threads: threads,
		DBPath:  "/tmp/threads.db",
		Cfg:     cfg,
		Logger:  logger,
	})

	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1 (trivial session)", result.Skipped)
	}
}
