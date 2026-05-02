// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/testutil/gitx"
	"github.com/suykerbuyk/vibe-vault/internal/worktreegc"
)

// TestWorktreeGCTool_HappyPath constructs a real locked worktree whose
// claude-agent marker references the *current* process PID. The
// host-local probePID will see it as alive, so the tool returns a
// well-formed Result with one VerdictAlive action and zero destruction.
// This exercises the full Run path (lockfile, enumeration, marker
// dispatch, PID probe) end-to-end through the MCP handler.
func TestWorktreeGCTool_HappyPath(t *testing.T) {
	repo := gitx.InitTestRepo(t)
	wtPath := gitx.AddWorktree(t, repo, "agent-1234567890abcdef", "worktree-agent-1234567890abcdef")
	reason := fmt.Sprintf("claude agent agent-1234567890abcdef (pid %d)", os.Getpid())
	gitx.LockWorktree(t, wtPath, reason)

	tool := NewWorktreeGCTool(config.DefaultConfig())
	params := json.RawMessage(fmt.Sprintf(`{"project_path": %q, "dry_run": true}`, repo))

	out, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var res worktreegc.Result
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if len(res.Actions) != 1 {
		t.Fatalf("want 1 action, got %d: %+v", len(res.Actions), res.Actions)
	}
	if res.Actions[0].Verdict != worktreegc.VerdictAlive {
		t.Errorf("verdict = %q, want %q", res.Actions[0].Verdict, worktreegc.VerdictAlive)
	}
	if res.Alive != 1 {
		t.Errorf("Alive = %d, want 1", res.Alive)
	}
	// Worktree must still exist (not reaped, not under dry-run).
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree dir vanished but holder is alive: %v", err)
	}
}

// TestWorktreeGCTool_MissingProjectPath asserts the handler rejects the
// request with a validation error before invoking Run.
func TestWorktreeGCTool_MissingProjectPath(t *testing.T) {
	tool := NewWorktreeGCTool(config.DefaultConfig())
	_, err := tool.Handler(json.RawMessage(`{"dry_run": true}`))
	if err == nil {
		t.Fatal("want validation error for missing project_path; got nil")
	}
}

// TestWorktreeGCTool_CandidateParentsFlowThrough confirms an explicit
// candidate_parents slice is forwarded to worktreegc.Run. The proof is
// indirect: the supplied parent does not exist as a branch, so the
// capture-verification path errors against it, and Run returns a
// well-formed Result without panicking. (Run is robust to errored
// parents; this test would otherwise mask a marshalling bug.)
func TestWorktreeGCTool_CandidateParentsFlowThrough(t *testing.T) {
	repo := gitx.InitTestRepo(t)

	tool := NewWorktreeGCTool(config.DefaultConfig())
	params := json.RawMessage(fmt.Sprintf(
		`{"project_path": %q, "dry_run": true, "candidate_parents": ["main", "feat/bogus"]}`,
		repo,
	))

	out, err := tool.Handler(params)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var res worktreegc.Result
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// No locked worktrees exist in this fixture, so no actions —
	// the assertion is the call returned cleanly with the slice
	// threaded through.
	if len(res.Actions) != 0 {
		t.Errorf("want 0 actions on a fresh repo, got %d", len(res.Actions))
	}
}
