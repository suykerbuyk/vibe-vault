// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// TestZedCapture_SetsSourceZedAcp is conformance test #12 (source-
// tag plumbing): the Zed batch-capture path must stamp
// Source = "zed-acp" into both the index entry and the rendered
// note frontmatter. Pre-γ the Zed path stamped "zed"; γ Phase 1
// renames to "zed-acp" so the registry name and the on-disk source
// tag align (the legacy "zed" string still resolves correctly via
// render.SourceFallbackSummary, which now matches both).
func TestZedCapture_SetsSourceZedAcp(t *testing.T) {
	cfg := batchTestConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "testproj", "sessions"), 0o755)
	os.MkdirAll(cfg.StateDir(), 0o755)

	logger := log.New(os.Stderr, "", 0)
	threads := []Thread{makeThread("source-tag-zed", 6)}

	result := BatchCapture(BatchCaptureOpts{
		Threads: threads,
		DBPath:  "/tmp/threads.db",
		Cfg:     cfg,
		Logger:  logger,
	})
	if result.Processed != 1 {
		t.Fatalf("Processed = %d, want 1", result.Processed)
	}

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	entry, ok := idx.Entries["zed:source-tag-zed"]
	if !ok {
		t.Fatalf("expected index entry for zed:source-tag-zed")
	}
	if entry.Source != SourceCaptureSourceTag {
		t.Errorf("entry.Source = %q, want %q", entry.Source, SourceCaptureSourceTag)
	}
	if entry.SourceName() != SourceCaptureSourceTag {
		t.Errorf("entry.SourceName() = %q, want %q", entry.SourceName(), SourceCaptureSourceTag)
	}

	// Verify the rendered note carries the same source string in
	// frontmatter — the index and the note must not diverge.
	notePath := entry.NotePath
	if !filepath.IsAbs(notePath) {
		notePath = filepath.Join(cfg.VaultPath, notePath)
	}
	data, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("read note %s: %v", notePath, err)
	}
	if !strings.Contains(string(data), "source: "+SourceCaptureSourceTag) {
		t.Errorf("note frontmatter missing 'source: %s'", SourceCaptureSourceTag)
	}
}
