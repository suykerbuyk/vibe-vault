// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Tests for the γ Phase 1 routing fork at the top of Capture(). The
// fork inspects CaptureOpts and routes to CaptureFromParsed when all
// four pre-parsed inputs (Transcript, Info, Narrative, Dialogue) are
// non-nil; otherwise it takes the original JSONL parse-and-build
// path. These two tests pin both branches.

package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/narrative"
	"github.com/suykerbuyk/vibe-vault/internal/prose"
	"github.com/suykerbuyk/vibe-vault/internal/transcript"
)

// TestCapture_RoutesToParsedWhenAllFourSet is conformance test #13:
// when CaptureOpts has Transcript, Info, Narrative, and Dialogue
// all non-nil, Capture must route directly to CaptureFromParsed
// (no JSONL parsing). We assert this by setting an invalid
// TranscriptPath that would fail JSONL parsing — the test passes
// only if the fast-path takes over before the path is touched.
//
// As a second assertion we compare the result against a direct
// CaptureFromParsed call with the same inputs; the two outputs
// must be functionally identical (same project, same source, same
// non-trivial note path).
func TestCapture_RoutesToParsedWhenAllFourSet(t *testing.T) {
	cfg := testConfig(t)
	os.MkdirAll(filepath.Join(cfg.VaultPath, "Projects", "fork-proj", "sessions"), 0o755)

	tr := &transcript.Transcript{
		Stats: transcript.Stats{
			SessionID:         "fork-routing-1",
			UserMessages:      4,
			AssistantMessages: 4,
			ToolUses:          3,
			StartTime:         time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
			Duration:          5 * time.Minute,
		},
	}
	info := Info{
		Project:   "fork-proj",
		Domain:    "personal",
		SessionID: "fork-routing-1",
	}
	narr := &narrative.Narrative{
		Title:   "Routing fork test",
		Summary: "Pre-parsed input flows through the fast path",
		Tag:     "implementation",
	}
	dialogue := &prose.Dialogue{}

	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}

	// Deliberately invalid TranscriptPath. If the fork did not
	// take, transcript.ParseFile would error out before any work.
	opts := CaptureOpts{
		TranscriptPath: "/this/path/does/not/exist/and/parsing/would/fail.jsonl",
		Source:         "test-fork",
		Index:          idx,
		Transcript:     tr,
		Info:           &info,
		Narrative:      narr,
		Dialogue:       dialogue,
	}

	result, err := Capture(opts, cfg)
	if err != nil {
		t.Fatalf("Capture with all four pre-parsed: %v", err)
	}
	if result.Skipped {
		t.Fatalf("expected non-skipped, got skipped: %s", result.Reason)
	}
	if result.Project != "fork-proj" {
		t.Errorf("Project = %q, want fork-proj", result.Project)
	}

	// Cross-check: a direct CaptureFromParsed call with the same
	// inputs should land on the same project and produce a non-
	// skipped result. We use a fresh index so the dedup check
	// passes; the goal is functional equivalence, not identity of
	// note path (timestamps would differ).
	idx2 := &index.Index{Entries: make(map[string]index.SessionEntry)}
	opts2 := CaptureOpts{
		TranscriptPath: "/tmp/whatever.jsonl",
		Source:         "test-fork",
		Index:          idx2,
	}
	directResult, err := CaptureFromParsed(tr, info, narr, dialogue, opts2, cfg)
	if err != nil {
		t.Fatalf("direct CaptureFromParsed: %v", err)
	}
	if directResult.Project != result.Project {
		t.Errorf("project divergence: fork=%q, direct=%q",
			result.Project, directResult.Project)
	}
	if directResult.Title != result.Title {
		t.Errorf("title divergence: fork=%q, direct=%q",
			result.Title, directResult.Title)
	}
}

// TestCapture_RoutesToJSONLParseWhenFieldsNil is conformance test
// #14: when any of the four pre-parsed fields is nil, Capture must
// take the existing JSONL parse-and-build path. We assert this by
// pointing TranscriptPath at a real fixture and verifying the
// produced note carries metadata only the JSONL parser can supply
// (the parsed-fields branch wouldn't have run ParseFile and
// wouldn't see the fixture data).
func TestCapture_RoutesToJSONLParseWhenFieldsNil(t *testing.T) {
	cfg := testConfig(t)

	// Write a minimal but valid JSONL transcript. 4 messages so
	// the trivial-skip filter does not fire.
	const fixture = `{"type":"user","uuid":"u1","timestamp":"2026-04-02T10:00:00Z","sessionId":"fork-jsonl-1","cwd":"/tmp/jsonlproj","gitBranch":"main","message":{"role":"user","content":"Implement Y"}}
{"type":"assistant","uuid":"a1","timestamp":"2026-04-02T10:00:05Z","sessionId":"fork-jsonl-1","cwd":"/tmp/jsonlproj","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"On it"}],"usage":{"input_tokens":10,"output_tokens":5}}}
{"type":"user","uuid":"u2","timestamp":"2026-04-02T10:00:10Z","sessionId":"fork-jsonl-1","cwd":"/tmp/jsonlproj","gitBranch":"main","message":{"role":"user","content":"Looks good"}}
{"type":"assistant","uuid":"a2","timestamp":"2026-04-02T10:01:00Z","sessionId":"fork-jsonl-1","cwd":"/tmp/jsonlproj","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Done"}],"usage":{"input_tokens":8,"output_tokens":3}}}`

	transcriptPath := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(fixture), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := &index.Index{Entries: make(map[string]index.SessionEntry)}

	// Leave all four pre-parsed fields nil so the fork takes the
	// JSONL parse-and-build path.
	opts := CaptureOpts{
		TranscriptPath: transcriptPath,
		SessionID:      "fork-jsonl-1",
		CWD:            "/tmp/jsonlproj",
		Index:          idx,
		SkipEnrichment: true,
	}

	result, err := Capture(opts, cfg)
	if err != nil {
		t.Fatalf("Capture (JSONL path): %v", err)
	}
	if result.Skipped {
		t.Fatalf("expected non-skipped, got skipped: %s", result.Reason)
	}

	// The note path should mention the project resolved from
	// the transcript's CWD/sessionID. The JSONL parser wouldn't
	// have run if the fork mistakenly took the pre-parsed branch
	// (all four fields nil cannot satisfy that branch's
	// precondition, but this test pins the assertion regardless).
	if !strings.Contains(result.NotePath, "jsonlproj") {
		t.Errorf("NotePath = %q, expected to contain 'jsonlproj' (project resolved from JSONL parse)",
			result.NotePath)
	}

	// And the index entry must carry the session id parsed from
	// the JSONL — proving ParseFile actually ran.
	entry, ok := idx.Entries["fork-jsonl-1"]
	if !ok {
		t.Fatal("expected index entry for fork-jsonl-1")
	}
	if entry.Title == "" {
		t.Error("entry.Title is empty — JSONL parse-and-build path didn't populate fields")
	}
}
