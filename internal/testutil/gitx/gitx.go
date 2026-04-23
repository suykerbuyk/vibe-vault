// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package gitx provides git-related helpers for tests. Helpers create
// temporary repos, configure remotes, and run git commands with
// deterministic author/committer identity.
package gitx

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// InitTestRepo creates a non-bare git repo with an initial commit in a
// temp directory. Returns the repo path.
func InitTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	GitRun(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("init"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	GitRun(t, dir, "add", ".")
	GitRun(t, dir, "commit", "-m", "initial")

	return dir
}

// InitBareRemote creates a bare git repo suitable as a push/fetch
// target. Returns the repo path.
func InitBareRemote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--bare", "-b", "main")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git init --bare: %s: %v", out, err)
	}
	return dir
}

// AddRemote adds a named remote to a repo.
func AddRemote(t *testing.T, repoDir, name, url string) {
	t.Helper()
	cmd := exec.Command("git", "remote", "add", name, url)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git remote add %s: %s: %v", name, out, err)
	}
}

// GitRun runs a git command in the given directory with deterministic
// identity env vars. Returns combined output; fatals on error.
func GitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return string(out)
}
