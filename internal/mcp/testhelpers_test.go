// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

// Shared test fixtures for git-backed tests in this package. Relocated
// here from tools_describe_iter_state_test.go (Commit 2 of
// wrap-mcp-offload, plan v8): the describe-iter-state tool retired with
// the surface 15→16 bump, but gitprobe_test.go and
// tools_collect_wrap_state_test.go still need these helpers. Same
// package, single home for cross-test fixtures.
//
// The internal/testutil/gitx package covers most new tests; these helpers
// remain because the legacy snapshot-style fixtures (file-touching
// gitCommit, stamp-file gitCommitStamp) shape commit graphs the gitx
// helpers do not, and rewriting every caller would inflate the diff.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// initGitRepo runs `git init` in dir so the directory is a valid git
// working tree. All tests using this helper should use t.Chdir / t.Setenv
// discipline.
func initGitRepo(t *testing.T, dir string) {
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

// commitAllInRepo stages and commits every file in dir as the initial
// commit, so subsequent `git status --porcelain` reports clean.
func commitAllInRepo(t *testing.T, dir, subject string) {
	t.Helper()
	envs := []string{
		"HOME=" + dir,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=t@t",
		"PATH=" + os.Getenv("PATH"),
	}
	add := exec.Command("git", "add", "-A")
	add.Dir = dir
	add.Env = envs
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %s", out)
	}
	commit := exec.Command("git", "commit", "-q", "--allow-empty", "-m", subject)
	commit.Dir = dir
	commit.Env = envs
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s", out)
	}
}

// gitCommit creates one commit in dir with the given subject + body.
// Returns the resulting commit SHA.
func gitCommit(t *testing.T, dir, subject, body string) string {
	t.Helper()
	envs := []string{
		"HOME=" + dir,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=t@t",
		"PATH=" + os.Getenv("PATH"),
	}
	// Touch a file so the commit isn't empty.
	tag := strings.ReplaceAll(subject, " ", "_")
	if err := os.WriteFile(filepath.Join(dir, "f-"+tag+".txt"), []byte(subject), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	add := exec.Command("git", "add", ".")
	add.Dir = dir
	add.Env = envs
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %s", out)
	}
	msg := subject
	if body != "" {
		msg = subject + "\n\n" + body
	}
	commit := exec.Command("git", "commit", "-q", "-m", msg)
	commit.Dir = dir
	commit.Env = envs
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s", out)
	}
	rev := exec.Command("git", "rev-parse", "HEAD")
	rev.Dir = dir
	rev.Env = envs
	out, err := rev.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse: %s", out)
	}
	return strings.TrimSpace(string(out))
}

// gitCommitStamp writes the iter stamp file (.vibe-vault/last-iter) with
// `iter\n`, git-adds it, and commits it with the given subject. Returns
// the resulting commit SHA. This is the canonical anchor-producing
// commit shape under the post-DESIGN-#93 stamp-file regime.
func gitCommitStamp(t *testing.T, dir string, iter int, subject string) string {
	t.Helper()
	envs := []string{
		"HOME=" + dir,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=t@t",
		"PATH=" + os.Getenv("PATH"),
	}
	stampDir := filepath.Join(dir, ".vibe-vault")
	if err := os.MkdirAll(stampDir, 0o755); err != nil {
		t.Fatalf("mkdir .vibe-vault: %v", err)
	}
	stampPath := filepath.Join(stampDir, "last-iter")
	content := strconv.Itoa(iter) + "\n"
	if err := os.WriteFile(stampPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write stamp file: %v", err)
	}
	add := exec.Command("git", "add", ".vibe-vault/last-iter")
	add.Dir = dir
	add.Env = envs
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add stamp: %s", out)
	}
	commit := exec.Command("git", "commit", "-q", "-m", subject)
	commit.Dir = dir
	commit.Env = envs
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("git commit stamp: %s", out)
	}
	rev := exec.Command("git", "rev-parse", "HEAD")
	rev.Dir = dir
	rev.Env = envs
	out, err := rev.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse: %s", out)
	}
	return strings.TrimSpace(string(out))
}
