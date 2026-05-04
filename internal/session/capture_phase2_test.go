// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/transcript"
)

// minimalParsedTranscript builds a transcript stub that survives the
// trivial-session filter (≥2 user msgs OR ≥2 assistant msgs).
func minimalParsedTranscript(sessionID string) *transcript.Transcript {
	return &transcript.Transcript{
		Stats: transcript.Stats{
			SessionID:         sessionID,
			UserMessages:      3,
			AssistantMessages: 3,
			ToolUses:          5,
			StartTime:         time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC),
			Duration:          10 * time.Minute,
		},
	}
}

// TestPhase2_CaptureFromParsed_StagingRoute is the foundational
// regression lock: when CaptureOpts.StagingRoot is non-empty, the
// note write lands in <staging>/<project>/<filename> and NOT in
// <vault>/Projects/<p>/sessions/<filename>. All four entry-point
// wiring tests (`vv backfill`, `vv reprocess`, Zed batch,
// `vv_capture_session`) reduce to this contract.
func TestPhase2_CaptureFromParsed_StagingRoute(t *testing.T) {
	cfg := testConfig(t)
	stagingRoot := filepath.Join(t.TempDir(), "staging")
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "xdg"))

	tr := minimalParsedTranscript("phase2-routed-1")
	info := Info{Project: "demoproj", Domain: "personal", SessionID: "phase2-routed-1"}
	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}

	opts := CaptureOpts{
		TranscriptPath: "/tmp/test.jsonl",
		StagingRoot:    stagingRoot,
		Index:          idx,
	}

	result, err := CaptureFromParsed(tr, info, nil, nil, opts, cfg)
	if err != nil {
		t.Fatalf("CaptureFromParsed: %v", err)
	}
	if result.Skipped {
		t.Fatalf("expected non-skipped, got: %s", result.Reason)
	}

	// File-in-staging?
	stagingProj := filepath.Join(stagingRoot, "demoproj")
	mds, err := os.ReadDir(stagingProj)
	if err != nil {
		t.Fatalf("read staging dir: %v", err)
	}
	var stagingMD int
	for _, e := range mds {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
			stagingMD++
		}
	}
	if stagingMD == 0 {
		t.Errorf("staging dir has no .md files: %s", stagingProj)
	}

	// File-NOT-in-vault?
	vaultSessions := filepath.Join(cfg.VaultPath, "Projects", "demoproj", "sessions")
	if entries, _ := os.ReadDir(vaultSessions); len(entries) > 0 {
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
				t.Errorf("session note leaked into vault: %s", e.Name())
			}
		}
	}

	// Index entry's NotePath is absolute (staging convention).
	entry, ok := idx.Entries["phase2-routed-1"]
	if !ok {
		t.Fatal("session not in index")
	}
	if !filepath.IsAbs(entry.NotePath) {
		t.Errorf("staging-routed NotePath should be absolute, got %q", entry.NotePath)
	}
	if !strings.HasPrefix(entry.NotePath, stagingRoot) {
		t.Errorf("NotePath %q not under staging root %q", entry.NotePath, stagingRoot)
	}
}

// TestPhase2_CaptureFromParsed_BackCompat: empty StagingRoot
// preserves the legacy flat-vault layout. The integration tests
// rely on this back-compat shim until Phase 4's aggregator lands.
func TestPhase2_CaptureFromParsed_BackCompat(t *testing.T) {
	cfg := testConfig(t)
	tr := minimalParsedTranscript("phase2-backcompat-1")
	info := Info{Project: "demoproj", Domain: "personal", SessionID: "phase2-backcompat-1"}
	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}

	opts := CaptureOpts{
		TranscriptPath: "/tmp/test.jsonl",
		// StagingRoot intentionally empty — back-compat path.
		Index: idx,
	}
	result, err := CaptureFromParsed(tr, info, nil, nil, opts, cfg)
	if err != nil {
		t.Fatalf("CaptureFromParsed: %v", err)
	}
	if result.Skipped {
		t.Fatalf("unexpected skip: %s", result.Reason)
	}

	// File DOES land in vault sessions/.
	vaultSessions := filepath.Join(cfg.VaultPath, "Projects", "demoproj", "sessions")
	entries, err := os.ReadDir(vaultSessions)
	if err != nil {
		t.Fatalf("read vault sessions: %v", err)
	}
	var vaultMD int
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
			vaultMD++
		}
	}
	if vaultMD == 0 {
		t.Errorf("back-compat path: no .md in vault sessions/ %s", vaultSessions)
	}

	// Index entry's NotePath is vault-relative (legacy convention).
	entry, ok := idx.Entries["phase2-backcompat-1"]
	if !ok {
		t.Fatal("session not in index")
	}
	if filepath.IsAbs(entry.NotePath) {
		t.Errorf("back-compat NotePath should be vault-relative, got absolute %q", entry.NotePath)
	}
	if !strings.HasPrefix(entry.NotePath, "Projects/") {
		t.Errorf("back-compat NotePath %q should start with Projects/", entry.NotePath)
	}
}

// TestPhase2_CaptureFromParsed_BackfillRegression simulates the
// `vv backfill` entry-point wiring: StagingRoot supplied, a fresh
// transcript routed through CaptureFromParsed. v4-C2 lock — pre-
// Phase-2 backfills bypassed staging entirely.
func TestPhase2_CaptureFromParsed_BackfillRegression(t *testing.T) {
	cfg := testConfig(t)
	stagingRoot := filepath.Join(t.TempDir(), "staging")
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "xdg"))

	tr := minimalParsedTranscript("phase2-backfill-1")
	info := Info{Project: "backfillproj", Domain: "personal", SessionID: "phase2-backfill-1"}
	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}

	// `vv backfill` passes Force=false (regular dedup) and no Provider.
	opts := CaptureOpts{
		TranscriptPath: "/tmp/backfill-fixture.jsonl",
		StagingRoot:    stagingRoot,
		Index:          idx,
	}
	if _, err := CaptureFromParsed(tr, info, nil, nil, opts, cfg); err != nil {
		t.Fatalf("CaptureFromParsed: %v", err)
	}

	// File-in-staging?
	mds, _ := os.ReadDir(filepath.Join(stagingRoot, "backfillproj"))
	if len(mds) == 0 {
		t.Errorf("backfill regression: no notes in staging %s", filepath.Join(stagingRoot, "backfillproj"))
	}
	// File-NOT-in-vault?
	if entries, _ := os.ReadDir(filepath.Join(cfg.VaultPath, "Projects", "backfillproj", "sessions")); len(entries) > 0 {
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
				t.Errorf("backfill regression: note leaked into vault: %s", e.Name())
			}
		}
	}
}

// TestPhase2_CaptureFromParsed_ReprocessRegression simulates the
// `vv reprocess` entry-point wiring: StagingRoot supplied + Force=true
// (reprocess bypasses dedup). v4-C2 entry-point lock.
func TestPhase2_CaptureFromParsed_ReprocessRegression(t *testing.T) {
	cfg := testConfig(t)
	stagingRoot := filepath.Join(t.TempDir(), "staging")
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "xdg"))

	tr := minimalParsedTranscript("phase2-reprocess-1")
	info := Info{Project: "reproproj", Domain: "personal", SessionID: "phase2-reprocess-1"}
	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}

	// First write to seed the index.
	if _, err := CaptureFromParsed(tr, info, nil, nil, CaptureOpts{
		TranscriptPath: "/tmp/repro.jsonl",
		StagingRoot:    stagingRoot,
		Index:          idx,
	}, cfg); err != nil {
		t.Fatalf("seed CaptureFromParsed: %v", err)
	}

	// Second invocation with Force=true — reprocess semantics.
	tr2 := minimalParsedTranscript("phase2-reprocess-1")
	if _, err := CaptureFromParsed(tr2, info, nil, nil, CaptureOpts{
		TranscriptPath: "/tmp/repro.jsonl",
		StagingRoot:    stagingRoot,
		Force:          true,
		Index:          idx,
	}, cfg); err != nil {
		t.Fatalf("reprocess CaptureFromParsed: %v", err)
	}

	mds, _ := os.ReadDir(filepath.Join(stagingRoot, "reproproj"))
	if len(mds) == 0 {
		t.Errorf("reprocess regression: no notes in staging")
	}
	if entries, _ := os.ReadDir(filepath.Join(cfg.VaultPath, "Projects", "reproproj", "sessions")); len(entries) > 0 {
		for _, e := range entries {
			if !e.IsDir() && filepath.Ext(e.Name()) == ".md" {
				t.Errorf("reprocess regression: note leaked into vault: %s", e.Name())
			}
		}
	}
}
