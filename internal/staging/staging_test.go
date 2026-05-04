// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/testutil/gitx"
)

// osExecCommand is a single-letter alias for exec.Command so the
// runGit helper below stays readable.
var osExecCommand = exec.Command

func TestSanitizeHostname(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"host", "host"},
		{"host.local", "host.local"},
		{"host-1", "host-1"},
		{"host_1", "host_1"},
		{"Host.LAN-3", "Host.LAN-3"},
		{"", "_unknown"},
		{".", "_unknown"},
		{"..", "_unknown"},
		{"../escape", "___escape"},
		{"host/with/slash", "host_with_slash"},
		{"a b", "a_b"},
		{"héllo", "h_llo"}, // é is one rune → one '_'
	}
	for _, tc := range cases {
		got := SanitizeHostname(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeHostname(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRoot_UsesXDGStateHome(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("XDG_STATE_HOME", custom)

	got, err := Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	want := filepath.Join(custom, "vibe-vault")
	if got != want {
		t.Errorf("Root() = %q, want %q", got, want)
	}
}

func TestRoot_FallsBackToLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	t.Setenv("VIBE_VAULT_HOME", home)

	got, err := Root()
	if err != nil {
		t.Fatalf("Root: %v", err)
	}
	want := filepath.Join(home, ".local", "state", "vibe-vault")
	if got != want {
		t.Errorf("Root() = %q, want %q", got, want)
	}
}

func TestPath_ProjectSubdir(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	got, err := Path("vibe-vault")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if !strings.HasSuffix(got, "/vibe-vault/vibe-vault") {
		t.Errorf("Path() = %q, want suffix /vibe-vault/vibe-vault", got)
	}
}

func TestPath_RejectsEmpty(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	if _, err := Path(""); err == nil {
		t.Error("Path(\"\") = nil, want error")
	}
}

// TestInit_FreshProject verifies the canonical happy path: staging dir
// + .git materialize at the XDG path, repo-local user.email/name are
// written, and the sentinel is dropped.
func TestInit_FreshProject(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	if err := Init("demo"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	stagingDir := filepath.Join(root, "vibe-vault", "demo")
	if _, err := os.Stat(filepath.Join(stagingDir, ".git", "HEAD")); err != nil {
		t.Errorf(".git/HEAD missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stagingDir, SentinelName)); err != nil {
		t.Errorf("sentinel missing: %v", err)
	}

	email := gitx.GitRun(t, stagingDir, "config", "--get", "user.email")
	if !strings.Contains(email, "vibe-vault@testhost") {
		t.Errorf("user.email = %q, want vibe-vault@testhost", strings.TrimSpace(email))
	}
	name := gitx.GitRun(t, stagingDir, "config", "--get", "user.name")
	if !strings.Contains(name, "vibe-vault") {
		t.Errorf("user.name = %q, want vibe-vault", strings.TrimSpace(name))
	}
}

// TestInit_Idempotent locks the fast-path no-op: a second Init on an
// already-bootstrapped project must be silent and must not rewrite the
// sentinel mtime.
func TestInit_Idempotent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	if err := Init("demo"); err != nil {
		t.Fatalf("Init #1: %v", err)
	}
	stagingDir := filepath.Join(root, "vibe-vault", "demo")
	sentPath := filepath.Join(stagingDir, SentinelName)
	st1, err := os.Stat(sentPath)
	if err != nil {
		t.Fatalf("stat sentinel: %v", err)
	}

	if iErr := Init("demo"); iErr != nil {
		t.Fatalf("Init #2: %v", iErr)
	}
	st2, err := os.Stat(sentPath)
	if err != nil {
		t.Fatalf("stat sentinel #2: %v", err)
	}
	if !st2.ModTime().Equal(st1.ModTime()) {
		t.Errorf("sentinel mtime changed across idempotent Init: %v -> %v",
			st1.ModTime(), st2.ModTime())
	}
}

// TestInit_NoXDGFallsBackToLocalState locks the no-XDG path: with
// XDG_STATE_HOME unset, Init writes under ~/.local/state/vibe-vault.
func TestInit_NoXDGFallsBackToLocalState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	home := t.TempDir()
	t.Setenv("VIBE_VAULT_HOME", home)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	if err := Init("demo"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	want := filepath.Join(home, ".local", "state", "vibe-vault", "demo", ".git", "HEAD")
	if _, err := os.Stat(want); err != nil {
		t.Errorf(".git/HEAD missing at %s: %v", want, err)
	}
}

// TestInit_NoGlobalGitIdentity verifies the v3-H2 invariant: even
// with no global user.email / user.name, Init succeeds because it
// writes a repo-local identity. Mirrors the multi-host CI scenario
// where each operator runs `vv hook install` on a fresh box.
func TestInit_NoGlobalGitIdentity(t *testing.T) {
	// Sandbox global git identity FIRST so Init's `git config` calls
	// don't pick up the runner's ~/.gitconfig. SandboxNoIdentity
	// rewrites HOME to a fresh tempdir; capture it AFTER the sandbox
	// rewrites so the staging dir lives inside the sandboxed tree.
	gitx.SandboxNoIdentity(t)
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	if err := Init("demo"); err != nil {
		t.Fatalf("Init with no global identity: %v", err)
	}

	stagingDir := filepath.Join(root, "vibe-vault", "demo")
	// Probe identity using a raw exec (bypassing gitx.GitRun, which
	// re-injects deterministic identity env vars and would defeat the
	// sandbox). Success here proves a future hook commit will not be
	// rejected for missing identity.
	out := runGit(t, stagingDir, "var", "GIT_AUTHOR_IDENT")
	if !strings.Contains(out, "vibe-vault@testhost") {
		t.Errorf("GIT_AUTHOR_IDENT = %q, want contains vibe-vault@testhost", out)
	}
}

// runGit invokes git in dir with the test process's existing env (no
// extra identity injection). Used by tests that have already
// established an identity sandbox and need to verify the sandbox
// holds end-to-end.
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := osExecCommand("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return string(out)
}

// TestInit_HostnameSanitized locks v3-C2: the raw os.Hostname output
// is never joined into a path component without sanitization. Setting
// VIBE_VAULT_HOSTNAME to a path-escape string must result in a
// filesystem-safe identity, not "vibe-vault@../escape".
func TestInit_HostnameSanitized(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	t.Setenv("VIBE_VAULT_HOSTNAME", "../escape")

	if err := Init("demo"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	stagingDir := filepath.Join(root, "vibe-vault", "demo")
	email := strings.TrimSpace(gitx.GitRun(t, stagingDir, "config", "--get", "user.email"))
	if strings.Contains(email, "..") || strings.Contains(email, "/") {
		t.Errorf("user.email = %q leaked path-escape characters", email)
	}
	if !strings.HasPrefix(email, "vibe-vault@") {
		t.Errorf("user.email = %q, want vibe-vault@<host>", email)
	}
}

// TestInit_RecoversFromMissingGit covers the v4-M1 partial-state
// scenario: sentinel survives but .git/ was deleted by the operator.
// Init must re-run `git init` even though the sentinel is present.
func TestInit_RecoversFromMissingGit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	if err := Init("demo"); err != nil {
		t.Fatalf("Init #1: %v", err)
	}
	stagingDir := filepath.Join(root, "vibe-vault", "demo")
	if err := os.RemoveAll(filepath.Join(stagingDir, ".git")); err != nil {
		t.Fatalf("rm -rf .git: %v", err)
	}
	if err := Init("demo"); err != nil {
		t.Fatalf("Init #2 after rm .git: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stagingDir, ".git", "HEAD")); err != nil {
		t.Errorf(".git/HEAD missing after recovery: %v", err)
	}
}

// TestRoot_NoHomeAndNoXDG locks the failure path: with neither
// XDG_STATE_HOME nor a usable home dir, Root must surface an error
// rather than silently return "" + nil (which would let later
// filepath.Join calls write under cwd).
func TestRoot_NoHomeAndNoXDG(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("VIBE_VAULT_HOME", "")
	t.Setenv("HOME", "")
	// On Linux without HOME, os.UserHomeDir returns ("", error).
	_, err := Root()
	if err == nil {
		t.Error("Root() with no home + no XDG = nil, want error")
	}
	if err != nil && !strings.Contains(err.Error(), "home") {
		// Tolerate either the wrapped HomeDir failure or our own sentinel.
		if !errors.Is(err, errors.New("staging: home dir is empty and XDG_STATE_HOME unset")) &&
			!strings.Contains(err.Error(), "home") {
			t.Errorf("Root() error = %v, want home-related", err)
		}
	}
}
