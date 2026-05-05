// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Note: shared fixture helpers (initGitRepo, gitCommit, gitCommitStamp,
// commitAllInRepo) live in tools_describe_iter_state_test.go for the
// duration of Commit 1; same package, so they resolve here without
// import changes. Commit 2 retires that file and the helpers move with
// the relocated test fixtures (or fold into the existing testutil/gitx
// package).

// TestLastIterAnchorSha_StampFileFound asserts that when the stamp file
// is committed and a later unrelated commit exists, the helper returns
// the stamp commit's SHA — not the latest HEAD.
func TestLastIterAnchorSha_StampFileFound(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	gitCommit(t, dir, "chore: initial", "")
	stampSHA := gitCommitStamp(t, dir, 41, "feat: wrap iter 41")
	laterSHA := gitCommit(t, dir, "docs: unrelated update", "")

	got, err := lastIterAnchorSha(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != stampSHA {
		t.Errorf("got %q, want stamp commit %q (later commit was %q)", got, stampSHA, laterSHA)
	}
}

// TestLastIterAnchorSha_StampFileMissing_ReturnsEmpty asserts a repo
// with commits but no stamp file returns ("", nil).
func TestLastIterAnchorSha_StampFileMissing_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	gitCommit(t, dir, "chore: initial", "")
	gitCommit(t, dir, "feat: something", "")

	got, err := lastIterAnchorSha(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string when no stamp commit exists", got)
	}
}

// TestLastIterAnchorSha_StampFileUntracked_ReturnsEmpty asserts that a
// stamp file written to disk but never `git add`-ed yields ("", nil).
// `git log -- <path>` requires the path be tracked; an untracked file
// produces no commits.
func TestLastIterAnchorSha_StampFileUntracked_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	gitCommit(t, dir, "chore: initial", "")

	stampDir := filepath.Join(dir, ".vibe-vault")
	if err := os.MkdirAll(stampDir, 0o755); err != nil {
		t.Fatalf("mkdir .vibe-vault: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stampDir, "last-iter"), []byte("41\n"), 0o644); err != nil {
		t.Fatalf("write stamp: %v", err)
	}

	got, err := lastIterAnchorSha(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string for untracked stamp file", got)
	}
}

// TestLastIterAnchorSha_StampFileMultipleVersions_ReturnsLatest asserts
// that when the stamp file is committed twice (different iters), the
// most recent commit's SHA wins.
func TestLastIterAnchorSha_StampFileMultipleVersions_ReturnsLatest(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	gitCommit(t, dir, "chore: initial", "")
	firstSHA := gitCommitStamp(t, dir, 41, "feat: wrap iter 41")
	secondSHA := gitCommitStamp(t, dir, 42, "feat: wrap iter 42")

	got, err := lastIterAnchorSha(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != secondSHA {
		t.Errorf("got %q, want latest stamp commit %q (first was %q)", got, secondSHA, firstSHA)
	}
}

// TestLastIterAnchorSha_StampPreservedAcrossRebase is the regression-lock
// for the wrap-shape-rebase-merge-not-recognized thread. A stamp commit
// on a feature branch, rebased onto main, must still be discoverable as
// the anchor by its (rebased) SHA. Rebase-merge preserves
// most-recent-touch on a tracked file.
func TestLastIterAnchorSha_StampPreservedAcrossRebase(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	gitCommit(t, dir, "chore: initial main", "")

	envs := []string{
		"HOME=" + dir,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=t@t",
		"PATH=" + os.Getenv("PATH"),
	}

	// Branch off, write the stamp, and commit on the feature branch.
	cb := exec.Command("git", "checkout", "-q", "-b", "feature/wrap")
	cb.Dir = dir
	cb.Env = envs
	if out, err := cb.CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b: %s", out)
	}
	featureSHA := gitCommitStamp(t, dir, 41, "feat: wrap iter 41 on feature")

	// Move main forward with an unrelated commit.
	co := exec.Command("git", "checkout", "-q", "main")
	co.Dir = dir
	co.Env = envs
	if out, err := co.CombinedOutput(); err != nil {
		t.Fatalf("git checkout main: %s", out)
	}
	gitCommit(t, dir, "chore: main moves on", "")

	// Switch back to feature branch and rebase onto main.
	co2 := exec.Command("git", "checkout", "-q", "feature/wrap")
	co2.Dir = dir
	co2.Env = envs
	if out, err := co2.CombinedOutput(); err != nil {
		t.Fatalf("git checkout feature: %s", out)
	}
	rb := exec.Command("git", "rebase", "-q", "main")
	rb.Dir = dir
	rb.Env = envs
	if out, err := rb.CombinedOutput(); err != nil {
		t.Fatalf("git rebase: %s", out)
	}

	// After rebase, HEAD points at the rebased version of the stamp
	// commit; that is the SHA the helper should return.
	rev := exec.Command("git", "rev-parse", "HEAD")
	rev.Dir = dir
	rev.Env = envs
	out, err := rev.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %s", out)
	}
	rebasedSHA := strings.TrimSpace(string(out))
	if rebasedSHA == featureSHA {
		t.Fatalf("rebase should have rewritten the stamp commit's SHA; got identical %q", featureSHA)
	}

	got, err := lastIterAnchorSha(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != rebasedSHA {
		t.Errorf("got %q, want rebased stamp commit %q (pre-rebase was %q)", got, rebasedSHA, featureSHA)
	}
}

// TestLastIterAnchorSha_NoGit_ReturnsEmpty asserts a non-git directory
// yields ("", nil).
func TestLastIterAnchorSha_NoGit_ReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	got, err := lastIterAnchorSha(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string for non-git directory", got)
	}
}

// TestLastIterAnchorSha_EmptyCwd_ReturnsEmpty asserts the empty-cwd
// guard returns ("", nil).
func TestLastIterAnchorSha_EmptyCwd_ReturnsEmpty(t *testing.T) {
	got, err := lastIterAnchorSha("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want empty string for empty cwd", got)
	}
}

// TestGitCmdRunner_Defaulted ensures the test seam exists and the default
// implementation works on a real git directory.
func TestGitCmdRunner_Defaulted(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	gitCommit(t, dir, "test", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := gitCmdRunner(ctx, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("git rev-parse: %v", err)
	}
	got := strings.TrimSpace(out)
	if got != "main" {
		t.Errorf("branch = %q, want main", got)
	}
}

// TestGitCmd_NoInteractiveEditor verifies that gitCmdRunner pins
// GIT_EDITOR=true on cmd.Env so operators with vim/nano configured
// as core.editor don't see vv hang during git invocations that would
// otherwise pop the editor. Regression guard mirroring vaultsync's
// TestGitCmd_NoInteractiveEditor (commit 8df6e09, iter 216): if a
// future change drops GIT_EDITOR=true from gitCmdRunner's env, this
// test fails by hanging or raising an editor-not-found error. Pairs
// with H1-v6 in the wrap-mcp-offload plan.
func TestGitCmd_NoInteractiveEditor(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	gitCommit(t, dir, "seed", "")

	// Configure an interactive-looking core.editor that would block on
	// stdin if actually invoked. With GIT_EDITOR=true pinned in
	// gitCmdRunner, git ignores this setting and short-circuits.
	envs := []string{
		"HOME=" + dir,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=t@t",
		"PATH=" + os.Getenv("PATH"),
	}
	cfg := exec.Command("git", "config", "core.editor", "cat")
	cfg.Dir = dir
	cfg.Env = envs
	if out, err := cfg.CombinedOutput(); err != nil {
		t.Fatalf("git config core.editor: %s: %v", out, err)
	}

	// `git commit --amend --no-edit` would normally still consult the
	// editor in some configurations; gitCmdRunner's GIT_EDITOR=true
	// pin makes that a no-op. We use a 5s context so any hang in
	// `cat`-as-editor surfaces as a context-deadline error.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := gitCmdRunner(ctx, dir, "commit", "--amend", "--no-edit"); err != nil {
		t.Fatalf("commit --amend hung or failed: %v", err)
	}
}

// TestProjectHasUncommittedWrites_CleanRepo asserts a freshly committed
// repo reports clean.
func TestProjectHasUncommittedWrites_CleanRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	gitCommit(t, dir, "init", "")

	dirty, err := projectHasUncommittedWrites(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dirty {
		t.Errorf("clean repo should be reported clean, got dirty")
	}
}

// TestProjectHasUncommittedWrites_DirtyRepo asserts an untracked file
// flips the result to dirty.
func TestProjectHasUncommittedWrites_DirtyRepo(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	gitCommit(t, dir, "init", "")

	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}

	dirty, err := projectHasUncommittedWrites(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dirty {
		t.Errorf("repo with untracked file should be reported dirty")
	}
}

// TestProjectHasUncommittedWrites_NotARepo asserts a non-git directory
// is treated as clean (no signal available).
func TestProjectHasUncommittedWrites_NotARepo(t *testing.T) {
	dir := t.TempDir()
	dirty, err := projectHasUncommittedWrites(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dirty {
		t.Errorf("non-git directory should be treated as clean, got dirty")
	}
}

// TestProjectHasUncommittedWrites_EmptyPath asserts the empty-path
// guard returns clean.
func TestProjectHasUncommittedWrites_EmptyPath(t *testing.T) {
	dirty, err := projectHasUncommittedWrites("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dirty {
		t.Errorf("empty path should be reported clean")
	}
}
