// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEnsureInit_BothPresent_FastPath: when sentinel and .git/HEAD
// both exist, EnsureInit returns nil without re-running Init. We
// detect "no work" by the sentinel mtime not changing.
func TestEnsureInit_BothPresent_FastPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	if err := Init("demo"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	stagingDir := filepath.Join(root, "vibe-vault", "demo")
	sentPath := filepath.Join(stagingDir, SentinelName)
	st1, err := os.Stat(sentPath)
	if err != nil {
		t.Fatalf("stat sentinel: %v", err)
	}

	if ensureErr := EnsureInit("demo"); ensureErr != nil {
		t.Fatalf("EnsureInit: %v", ensureErr)
	}
	st2, statErr := os.Stat(sentPath)
	if statErr != nil {
		t.Fatalf("stat sentinel #2: %v", statErr)
	}
	if !st2.ModTime().Equal(st1.ModTime()) {
		t.Errorf("EnsureInit fast path rewrote sentinel: %v -> %v",
			st1.ModTime(), st2.ModTime())
	}
}

// TestEnsureInit_MissingSentinel: sentinel deleted but .git/HEAD
// present. EnsureInit must recover (re-create the sentinel).
func TestEnsureInit_MissingSentinel(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")
	if err := Init("demo"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	stagingDir := filepath.Join(root, "vibe-vault", "demo")
	sentPath := filepath.Join(stagingDir, SentinelName)

	if err := os.Remove(sentPath); err != nil {
		t.Fatalf("rm sentinel: %v", err)
	}
	if err := EnsureInit("demo"); err != nil {
		t.Fatalf("EnsureInit recovery: %v", err)
	}
	if _, err := os.Stat(sentPath); err != nil {
		t.Errorf("sentinel not recreated: %v", err)
	}
}

// TestEnsureInit_MissingGit: sentinel survives but .git/ wiped.
// v4-M1 lock — without the .git/HEAD co-stat, EnsureInit would
// fast-path skip and the next staging.Commit would fail per fire.
func TestEnsureInit_MissingGit(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")
	if err := Init("demo"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	stagingDir := filepath.Join(root, "vibe-vault", "demo")

	if err := os.RemoveAll(filepath.Join(stagingDir, ".git")); err != nil {
		t.Fatalf("rm .git: %v", err)
	}
	if err := EnsureInit("demo"); err != nil {
		t.Fatalf("EnsureInit recovery: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stagingDir, ".git", "HEAD")); err != nil {
		t.Errorf(".git/HEAD not recreated: %v", err)
	}
}

// TestEnsureInit_BothMissing: cold start. EnsureInit invokes Init,
// both sentinel and .git/HEAD materialize.
func TestEnsureInit_BothMissing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", root)
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	if err := EnsureInit("fresh"); err != nil {
		t.Fatalf("EnsureInit cold: %v", err)
	}
	stagingDir := filepath.Join(root, "vibe-vault", "fresh")
	if _, err := os.Stat(filepath.Join(stagingDir, SentinelName)); err != nil {
		t.Errorf("sentinel missing post-cold: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stagingDir, ".git", "HEAD")); err != nil {
		t.Errorf(".git/HEAD missing post-cold: %v", err)
	}
}

// TestResolveRoot_CfgRootWins: an explicit cfg.Staging.Root override
// supersedes the XDG default.
func TestResolveRoot_CfgRootWins(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // present so XDG path is non-empty
	t.Setenv(disableEnv, "")
	custom := "/custom/staging/root"
	got := ResolveRoot(custom)
	if got != custom {
		t.Errorf("ResolveRoot(%q) = %q, want %q", custom, got, custom)
	}
}

// TestResolveRoot_FallsBackToXDG: no cfg override, XDG_STATE_HOME
// resolves the path.
func TestResolveRoot_FallsBackToXDG(t *testing.T) {
	custom := t.TempDir()
	t.Setenv("XDG_STATE_HOME", custom)
	t.Setenv(disableEnv, "")
	got := ResolveRoot("")
	want := filepath.Join(custom, "vibe-vault")
	if got != want {
		t.Errorf("ResolveRoot(\"\") = %q, want %q", got, want)
	}
}

// TestResolveRoot_DisableEnv: VIBE_VAULT_DISABLE_STAGING=1 forces
// "" regardless of cfgRoot or XDG. Documented escape hatch.
func TestResolveRoot_DisableEnv(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv(disableEnv, "1")
	if got := ResolveRoot(""); got != "" {
		t.Errorf("ResolveRoot with DISABLE=1 = %q, want \"\"", got)
	}
	if got := ResolveRoot("/explicit/override"); got != "" {
		t.Errorf("ResolveRoot with DISABLE=1 + cfgRoot = %q, want \"\"", got)
	}
}

// TestEnsureInitAt_PinnedRoot: cfg.Staging.Root override flows to
// the init target. Without EnsureInitAt, EnsureInit would re-resolve
// via the XDG default and bootstrap the wrong dir.
func TestEnsureInitAt_PinnedRoot(t *testing.T) {
	customRoot := filepath.Join(t.TempDir(), "custom")
	// XDG points elsewhere on purpose so a misuse of EnsureInit would
	// leak into the wrong dir.
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "wrong-xdg"))
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")

	if err := EnsureInitAt(customRoot, "demo"); err != nil {
		t.Fatalf("EnsureInitAt: %v", err)
	}
	stagingDir := filepath.Join(customRoot, "demo")
	if _, err := os.Stat(filepath.Join(stagingDir, SentinelName)); err != nil {
		t.Errorf("sentinel missing under custom root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stagingDir, ".git", "HEAD")); err != nil {
		t.Errorf(".git/HEAD missing under custom root: %v", err)
	}
}

// TestEnsureInitAt_EmptyProject returns ErrEmptyProject (programming
// error path).
func TestEnsureInitAt_EmptyProject(t *testing.T) {
	if err := EnsureInitAt("/some/root", ""); err == nil {
		t.Error("EnsureInitAt with empty project = nil, want ErrEmptyProject")
	}
}

// TestEnsureInitAt_EmptyRoot delegates to EnsureInit (back-compat).
func TestEnsureInitAt_EmptyRoot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("VIBE_VAULT_HOSTNAME", "testhost")
	if err := EnsureInitAt("", "demo"); err != nil {
		t.Fatalf("EnsureInitAt(\"\", \"demo\"): %v", err)
	}
}
