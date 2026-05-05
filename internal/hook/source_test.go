// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package hook

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/index"
)

// TestHookCapture_SetsSourceClaudeCodeJsonl is conformance test #11
// (source-tag plumbing): the hook's end-to-end capture path must
// stamp Source = "claude-code-jsonl" into both the index entry and
// the rendered note frontmatter. Today's behavior left Source empty
// at the production callsite — γ Phase 1 closes that gap so the
// source identity is uniform top-to-bottom.
func TestHookCapture_SetsSourceClaudeCodeJsonl(t *testing.T) {
	cfg := testConfig(t)
	transcriptPath := writeTranscript(t, minimalTranscript)

	input := &Input{
		SessionID:      "source-tag-sess",
		TranscriptPath: transcriptPath,
		HookEventName:  "SessionEnd",
		CWD:            "/tmp/proj",
	}

	if err := handleInput(input, "", cfg); err != nil {
		t.Fatalf("handleInput: %v", err)
	}

	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	entry, ok := idx.Entries["source-tag-sess"]
	if !ok {
		t.Fatalf("expected index entry for source-tag-sess; entries: %v", idx.Entries)
	}
	if entry.Source != SourceName {
		t.Errorf("entry.Source = %q, want %q", entry.Source, SourceName)
	}
	if entry.SourceName() != SourceName {
		t.Errorf("entry.SourceName() = %q, want %q", entry.SourceName(), SourceName)
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
	if !strings.Contains(string(data), "source: "+SourceName) {
		t.Errorf("note frontmatter missing 'source: %s'", SourceName)
	}
}
