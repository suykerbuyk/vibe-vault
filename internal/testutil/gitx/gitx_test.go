// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package gitx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAddWorktree verifies AddWorktree creates a worktree at the
// iter-185 layout path and registers it with git.
func TestAddWorktree(t *testing.T) {
	repo := InitTestRepo(t)
	wtPath := AddWorktree(t, repo, "agent-aaaaaaaaaaaaaaaa", "feat/test")

	// Returned path should exist on disk.
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree path %s does not exist: %v", wtPath, err)
	}

	// Layout sanity: <repo>/.claude/worktrees/agent-aaaaaaaaaaaaaaaa.
	wantSuffix := filepath.Join(".claude", "worktrees", "agent-aaaaaaaaaaaaaaaa")
	if !strings.HasSuffix(wtPath, wantSuffix) {
		t.Errorf("worktree path = %s; want suffix %s", wtPath, wantSuffix)
	}

	// `git worktree list` should mention the new worktree.
	out := GitRun(t, repo, "worktree", "list")
	if !strings.Contains(out, wtPath) {
		t.Errorf("git worktree list output does not mention %s:\n%s", wtPath, out)
	}
}

// TestLockWorktree verifies LockWorktree records the lock reason in the
// porcelain output for the worktree.
func TestLockWorktree(t *testing.T) {
	repo := InitTestRepo(t)
	wtPath := AddWorktree(t, repo, "agent-bbbbbbbbbbbbbbbb", "feat/locktest")

	const reason = "claude-agent: smoke test"
	LockWorktree(t, wtPath, reason)

	out := GitRun(t, repo, "worktree", "list", "--porcelain")
	// Look for the worktree's block and assert it contains the locked
	// directive with the reason. Porcelain format emits one block per
	// worktree separated by blank lines; locked appears as either
	// `locked` (no reason) or `locked <reason>`.
	if !strings.Contains(out, wtPath) {
		t.Fatalf("worktree %s missing from porcelain output:\n%s", wtPath, out)
	}
	if !strings.Contains(out, "locked "+reason) {
		t.Errorf("porcelain output missing `locked %s`:\n%s", reason, out)
	}
}

// TestWriteWorktreeMarker verifies the helper writes the expected
// reason+newline content at <gitCommonDir>/worktrees/<name>/locked.
func TestWriteWorktreeMarker(t *testing.T) {
	repo := InitTestRepo(t)
	// `git rev-parse --git-common-dir` yields the shared .git dir.
	commonDir := strings.TrimSpace(GitRun(t, repo, "rev-parse", "--git-common-dir"))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repo, commonDir)
	}

	const (
		name   = "agent-cccccccccccccccc"
		reason = "claude-agent: orphan marker"
	)
	WriteWorktreeMarker(t, commonDir, name, reason)

	markerPath := filepath.Join(commonDir, "worktrees", name, "locked")
	got, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatalf("read marker %s: %v", markerPath, err)
	}
	want := reason + "\n"
	if string(got) != want {
		t.Errorf("marker content = %q; want %q", string(got), want)
	}
}

// TestInitTestRepoWithDefault verifies the default-branch parameter is
// honored.
func TestInitTestRepoWithDefault(t *testing.T) {
	repo := InitTestRepoWithDefault(t, "master")
	out := strings.TrimSpace(GitRun(t, repo, "rev-parse", "--abbrev-ref", "HEAD"))
	if out != "master" {
		t.Errorf("HEAD branch = %q; want %q", out, "master")
	}
}
