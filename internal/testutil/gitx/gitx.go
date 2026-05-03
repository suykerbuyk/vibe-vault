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
	return initTestRepoWithDefault(t, "main")
}

// InitTestRepoWithDefault creates a non-bare git repo with an initial
// commit in a temp directory, using the supplied default-branch name
// (e.g. "master", "trunk"). Returns the repo path.
func InitTestRepoWithDefault(t *testing.T, defaultBranch string) string {
	t.Helper()
	return initTestRepoWithDefault(t, defaultBranch)
}

// initTestRepoWithDefault is the shared implementation for InitTestRepo
// and InitTestRepoWithDefault. Mirrors InitTestRepo's prior behavior
// exactly except the default branch is parameterized.
func initTestRepoWithDefault(t *testing.T, defaultBranch string) string {
	t.Helper()
	dir := t.TempDir()

	GitRun(t, dir, "init", "-b", defaultBranch)
	// Write identity into repo-local config so any git invocation in this
	// dir — including subprocess ones from production code under test —
	// finds a committer identity. GitRun's env-var injection only covers
	// commands it runs itself; code paths like vaultsync.gitCmd need the
	// identity in .git/config. CI runners have no global identity.
	GitRun(t, dir, "config", "user.email", "test@test.com")
	GitRun(t, dir, "config", "user.name", "test")
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

// InitTestRepoNoIdentity creates a repo at a tempdir with NO
// committer identity configured anywhere — used to test fail-fast
// identity-probe paths. Pairs with SandboxNoIdentity to fully
// isolate the test from the operator's git config.
//
// Sets `user.useConfigOnly=true` in the repo-local config to defeat
// git's getpwuid()/hostname synthesis fallback. Without this, hosts
// whose /etc/passwd has a populated GECOS field AND whose hostname
// resolves to an FQDN (i.e. contains '@'-eligible content) cause
// `git var GIT_AUTHOR_IDENT` to succeed with a synthesized identity
// like `John Doe <user@host.example.com>`, defeating the no-identity
// sandbox. `user.useConfigOnly` tells git to refuse that synthesis
// and require an explicit config-file identity, which in turn fails
// fast in this fixture exactly as the test asserts.
func InitTestRepoNoIdentity(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	GitRun(t, dir, "init", "-b", "main")
	GitRun(t, dir, "config", "user.useConfigOnly", "true")
	return dir
}

// SandboxNoIdentity scrubs all four git-identity env vars and
// redirects HOME / XDG_CONFIG_HOME / GIT_CONFIG_GLOBAL /
// GIT_CONFIG_SYSTEM so subprocess `git` invocations cannot
// resolve any identity. Restores prior values via t.Cleanup.
//
// Required because t.Setenv only SETS values; it does not unset.
// Empty-string env values are not equivalent to unset for git's
// identity-resolution path.
func SandboxNoIdentity(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL",
		"GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL",
	} {
		prev, ok := os.LookupEnv(k)
		os.Unsetenv(k)
		if ok {
			key := k
			val := prev
			t.Cleanup(func() { os.Setenv(key, val) })
		}
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "xdg"))
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
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

// AddWorktree creates a new linked worktree under
// <repoDir>/.claude/worktrees/<worktreeName> on a fresh branch named
// <branch>. Mirrors the iter-185 production layout used by
// Claude-agent worktrees. Returns the absolute worktree path.
//
// `git worktree add` auto-creates intermediate parent directories
// (verified empirically on git 2.x), so no separate os.MkdirAll is
// required.
func AddWorktree(t *testing.T, repoDir, worktreeName, branch string) string {
	t.Helper()
	wtPath := filepath.Join(repoDir, ".claude", "worktrees", worktreeName)
	GitRun(t, repoDir, "worktree", "add", wtPath, "-b", branch)
	return wtPath
}

// LockWorktree invokes `git worktree lock --reason="<reason>"
// <worktreePath>`, which causes git to refuse to prune or remove the
// worktree until it is unlocked. Fatals on error.
//
// The git command runs with cwd set to worktreePath so git can resolve
// the enclosing repository regardless of the test's process cwd.
func LockWorktree(t *testing.T, worktreePath, reason string) {
	t.Helper()
	cmd := exec.Command("git", "worktree", "lock", "--reason="+reason, worktreePath)
	cmd.Dir = worktreePath
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git worktree lock %s: %s: %v", worktreePath, out, err)
	}
}

// WriteWorktreeMarker writes a `locked` marker file directly into
// <gitCommonDir>/worktrees/<worktreeName>/locked containing the supplied
// reason text plus a trailing newline (mirroring the empirical
// claude-agent format, where files end with 0x0a).
//
// This helper is intended for tests that need to construct a marker
// for a worktree that is NOT registered with `git worktree add` —
// e.g. corrupt-marker or orphan-marker test cases. It does NOT touch
// any git metadata beyond the locked file itself.
//
// Auto-creates the <gitCommonDir>/worktrees/<worktreeName>/ directory
// chain via os.MkdirAll(..., 0o755) before writing.
func WriteWorktreeMarker(t *testing.T, gitCommonDir, worktreeName, reason string) {
	t.Helper()
	wtMetaDir := filepath.Join(gitCommonDir, "worktrees", worktreeName)
	if err := os.MkdirAll(wtMetaDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", wtMetaDir, err)
	}
	markerPath := filepath.Join(wtMetaDir, "locked")
	if err := os.WriteFile(markerPath, []byte(reason+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", markerPath, err)
	}
}
