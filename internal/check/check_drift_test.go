// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package check

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// driftInitGitRepo runs `git init -b main` and seeds a single commit so
// the repo has a resolvable HEAD. Mirrors the helper in
// internal/mcp/tools_describe_iter_state_test.go (intentionally
// duplicated — internal/check must not import internal/mcp).
func driftInitGitRepo(t *testing.T, dir string) {
	t.Helper()
	envs := []string{
		"HOME=" + dir,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=t@t",
		"PATH=" + os.Getenv("PATH"),
	}
	cmds := [][]string{
		{"git", "init", "-q", "-b", "main"},
		{"git", "config", "user.email", "t@t"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "-q", "--allow-empty", "-m", "init"},
	}
	for _, c := range cmds {
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Dir = dir
		cmd.Env = envs
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", c, string(out))
		}
	}
}

// driftCheckoutBranch creates and switches to a new branch.
func driftCheckoutBranch(t *testing.T, dir, branch string) {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "checkout", "-q", "-b", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("checkout -b %s: %s", branch, out)
	}
}

// driftDetachHEAD checks out the current commit's SHA so HEAD is detached.
func driftDetachHEAD(t *testing.T, dir string) {
	t.Helper()
	rev := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := rev.Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	sha := strings.TrimSpace(string(out))
	co := exec.Command("git", "-C", dir, "checkout", "-q", "--detach", sha)
	if cout, err := co.CombinedOutput(); err != nil {
		t.Fatalf("detach checkout: %s", cout)
	}
}

// driftWriteLastIter writes <repoPath>/.vibe-vault/last-iter with the
// given content (no trailing newline normalization beyond what the
// caller passes).
func driftWriteLastIter(t *testing.T, repoPath, content string) {
	t.Helper()
	dir := filepath.Join(repoPath, ".vibe-vault")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "last-iter"), []byte(content), 0o644); err != nil {
		t.Fatalf("write last-iter: %v", err)
	}
}

// driftWriteIterations writes
// <vaultPath>/Projects/<project>/agentctx/iterations.md with the given
// body.
func driftWriteIterations(t *testing.T, vaultPath, project, body string) {
	t.Helper()
	dir := filepath.Join(vaultPath, "Projects", project, "agentctx")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "iterations.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write iterations.md: %v", err)
	}
}

func TestCheckWrapIterDrift_NoDrift(t *testing.T) {
	repo := t.TempDir()
	vault := t.TempDir()
	driftInitGitRepo(t, repo)
	driftWriteLastIter(t, repo, "42\n")
	driftWriteIterations(t, vault, "X", "### Iteration 41 — foo (2026-05-01)\n\n### Iteration 42 — bar (2026-05-02)\n")

	got := CheckWrapIterDrift(repo, vault, "X")
	if got == nil {
		t.Fatal("CheckWrapIterDrift returned nil")
	}
	if got.Status != Pass {
		t.Errorf("Status = %v, want Pass", got.Status)
	}
	if !strings.Contains(got.Detail, "in sync (iter 42)") {
		t.Errorf("Detail = %q, want contains %q", got.Detail, "in sync (iter 42)")
	}
}

func TestCheckWrapIterDrift_Drift(t *testing.T) {
	repo := t.TempDir()
	vault := t.TempDir()
	driftInitGitRepo(t, repo)
	driftWriteLastIter(t, repo, "42")
	driftWriteIterations(t, vault, "X", "### Iteration 42 — bar\n### Iteration 50 — baz\n")

	got := CheckWrapIterDrift(repo, vault, "X")
	if got == nil {
		t.Fatal("CheckWrapIterDrift returned nil")
	}
	if got.Status != Warn {
		t.Errorf("Status = %v, want Warn", got.Status)
	}
	if !strings.Contains(got.Detail, "local iter 42 behind vault iter 50") {
		t.Errorf("Detail = %q, want contains %q", got.Detail, "local iter 42 behind vault iter 50")
	}
}

func TestCheckWrapIterDrift_LocalAhead(t *testing.T) {
	repo := t.TempDir()
	vault := t.TempDir()
	driftInitGitRepo(t, repo)
	driftWriteLastIter(t, repo, "50")
	driftWriteIterations(t, vault, "X", "### Iteration 41\n### Iteration 42\n")

	got := CheckWrapIterDrift(repo, vault, "X")
	if got == nil {
		t.Fatal("CheckWrapIterDrift returned nil")
	}
	if got.Status != Pass {
		t.Errorf("Status = %v, want Pass", got.Status)
	}
	if !strings.Contains(got.Detail, "ahead") {
		t.Errorf("Detail = %q, want contains %q", got.Detail, "ahead")
	}
}

func TestCheckWrapIterDrift_FreshProject_NoLastIter(t *testing.T) {
	repo := t.TempDir()
	vault := t.TempDir()
	driftInitGitRepo(t, repo)
	driftWriteIterations(t, vault, "X", "### Iteration 1\n")

	got := CheckWrapIterDrift(repo, vault, "X")
	if got == nil {
		t.Fatal("CheckWrapIterDrift returned nil")
	}
	if got.Status != Pass {
		t.Errorf("Status = %v, want Pass", got.Status)
	}
	if !strings.Contains(got.Detail, "no local stamp") {
		t.Errorf("Detail = %q, want contains %q", got.Detail, "no local stamp")
	}
}

func TestCheckWrapIterDrift_NoIterationsMD(t *testing.T) {
	repo := t.TempDir()
	vault := t.TempDir()
	driftInitGitRepo(t, repo)
	driftWriteLastIter(t, repo, "42")
	// No iterations.md.

	got := CheckWrapIterDrift(repo, vault, "X")
	if got == nil {
		t.Fatal("CheckWrapIterDrift returned nil")
	}
	if got.Status != Pass {
		t.Errorf("Status = %v, want Pass", got.Status)
	}
	if !strings.Contains(got.Detail, "no vault iterations") {
		t.Errorf("Detail = %q, want contains %q", got.Detail, "no vault iterations")
	}
}

func TestCheckWrapIterDrift_FeatureBranch(t *testing.T) {
	repo := t.TempDir()
	vault := t.TempDir()
	driftInitGitRepo(t, repo)
	driftCheckoutBranch(t, repo, "feat/x")
	driftWriteLastIter(t, repo, "42")
	driftWriteIterations(t, vault, "X", "### Iteration 50\n")

	got := CheckWrapIterDrift(repo, vault, "X")
	if got == nil {
		t.Fatal("CheckWrapIterDrift returned nil")
	}
	if got.Status != Pass {
		t.Errorf("Status = %v, want Pass", got.Status)
	}
	if !strings.Contains(got.Detail, "skipped (feature branch feat/x)") {
		t.Errorf("Detail = %q, want contains %q", got.Detail, "skipped (feature branch feat/x)")
	}
}

func TestCheckWrapIterDrift_DetachedHEAD(t *testing.T) {
	repo := t.TempDir()
	vault := t.TempDir()
	driftInitGitRepo(t, repo)
	driftDetachHEAD(t, repo)
	driftWriteLastIter(t, repo, "42")
	driftWriteIterations(t, vault, "X", "### Iteration 50\n")

	got := CheckWrapIterDrift(repo, vault, "X")
	if got == nil {
		t.Fatal("CheckWrapIterDrift returned nil")
	}
	if got.Status != Pass {
		t.Errorf("Status = %v, want Pass", got.Status)
	}
	if !strings.Contains(got.Detail, "skipped (detached HEAD)") {
		t.Errorf("Detail = %q, want contains %q", got.Detail, "skipped (detached HEAD)")
	}
}

// TestCheckWrapIterDrift_NilOnEmptyInputs ensures the helper matches
// the existing project-scoped check convention: nil on missing scope.
func TestCheckWrapIterDrift_NilOnEmptyInputs(t *testing.T) {
	cases := []struct {
		repo    string
		vault   string
		project string
	}{
		{"", "/v", "X"},
		{"/r", "", "X"},
		{"/r", "/v", ""},
		{"/r", "/v", "_unknown"},
	}
	for _, c := range cases {
		got := CheckWrapIterDrift(c.repo, c.vault, c.project)
		if got != nil {
			t.Errorf("CheckWrapIterDrift(%q,%q,%q) = %+v, want nil", c.repo, c.vault, c.project, got)
		}
	}
}
