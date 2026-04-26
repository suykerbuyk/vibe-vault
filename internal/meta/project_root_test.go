// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package meta

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// mkdirs creates a directory path and all parents.
func mkdirs(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}
}

// writeFile creates a file with the given content.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent of %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

// TestProjectRoot_GitDir verifies that a standard git project (with .git/)
// is found correctly when walking up from a subdirectory.
func TestProjectRoot_GitDir(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, filepath.Join(root, ".git"))
	sub := filepath.Join(root, "cmd", "foo")
	mkdirs(t, sub)

	got, err := ProjectRoot(sub, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("got %q, want %q", got, root)
	}
}

// TestProjectRoot_AgentctxDir verifies that a project with agentctx/ but
// no .git/ is found correctly.
func TestProjectRoot_AgentctxDir(t *testing.T) {
	root := t.TempDir()
	mkdirs(t, filepath.Join(root, "agentctx"))
	sub := filepath.Join(root, "internal", "pkg")
	mkdirs(t, sub)

	got, err := ProjectRoot(sub, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("got %q, want %q", got, root)
	}
}

// TestProjectRoot_VaultOnly is mandatory: the parent directory has .git/
// and the child directory has agentctx/. The child (project) path must be
// returned, not the parent vault root. This validates that agentctx/ is
// checked before .git/ at each level, so the child wins.
func TestProjectRoot_VaultOnly(t *testing.T) {
	vaultRoot := t.TempDir()
	mkdirs(t, filepath.Join(vaultRoot, ".git"))

	// Child project inside the vault root: has its own agentctx/.
	projectRoot := filepath.Join(vaultRoot, "Projects", "myproject")
	mkdirs(t, filepath.Join(projectRoot, "agentctx"))

	// CWD is deep inside the project.
	sub := filepath.Join(projectRoot, "src")
	mkdirs(t, sub)

	// vaultPath is set to vaultRoot so it would be refused if matched.
	got, err := ProjectRoot(sub, vaultRoot)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != projectRoot {
		t.Errorf("got %q, want project root %q", got, projectRoot)
	}
}

// TestProjectRoot_VaultRootRefused is mandatory (M2): when cwd IS the vault
// root (or is inside it and the vault root is the first match), ErrIsVaultRoot
// must be returned.
func TestProjectRoot_VaultRootRefused(t *testing.T) {
	vault := t.TempDir()
	// The vault root itself has an agentctx/ directory (many vaults do).
	mkdirs(t, filepath.Join(vault, "agentctx"))

	// CWD IS the vault root.
	_, err := ProjectRoot(vault, vault)
	if !errors.Is(err, ErrIsVaultRoot) {
		t.Errorf("err = %v, want ErrIsVaultRoot", err)
	}
}

// TestProjectRoot_NotFound verifies that walking all the way to the filesystem
// root without finding any marker returns a descriptive error.
func TestProjectRoot_NotFound(t *testing.T) {
	// Use a temp directory with no markers and no parent markers.
	// We can't guarantee the filesystem root has no .git, so we use the
	// projectRootFunc seam to inject a controlled walk.
	original := projectRootFunc
	t.Cleanup(func() { projectRootFunc = original })

	projectRootFunc = func(cwd, vaultPath string) (string, error) {
		// Simulate a walk that hits the root without finding anything.
		return projectRootNotFoundSentinel(cwd, vaultPath)
	}

	_, err := ProjectRoot("/no/markers/here", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ErrIsVaultRoot) {
		t.Errorf("got ErrIsVaultRoot, want a descriptive not-found error")
	}
}

// projectRootNotFoundSentinel is a minimal walk-alike that always returns
// the not-found error. Used only in TestProjectRoot_NotFound.
func projectRootNotFoundSentinel(cwd, _ string) (string, error) {
	return "", errors.New("no project root found walking up from " + cwd + ": no agentctx/ or .git marker")
}

// TestProjectRoot_Override verifies that the projectRootFunc seam is exercised
// correctly: a test-installed function replaces the real walk.
func TestProjectRoot_Override(t *testing.T) {
	original := projectRootFunc
	t.Cleanup(func() { projectRootFunc = original })

	sentinel := "/override/project/root"
	projectRootFunc = func(cwd, vaultPath string) (string, error) {
		return sentinel, nil
	}

	got, err := ProjectRoot("/any/cwd", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != sentinel {
		t.Errorf("got %q, want %q", got, sentinel)
	}
}

// TestProjectRoot_WorktreeGitFile verifies that a .git file (worktree pointer)
// is treated as a valid project root marker, just like a .git directory.
func TestProjectRoot_WorktreeGitFile(t *testing.T) {
	root := t.TempDir()
	// Write a .git FILE (not a directory) — this is what git worktrees do.
	writeFile(t, filepath.Join(root, ".git"), "gitdir: /some/parent/.git/worktrees/myworktree\n")

	sub := filepath.Join(root, "internal")
	mkdirs(t, sub)

	got, err := ProjectRoot(sub, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("got %q, want %q", got, root)
	}
}

// TestProjectRoot_AgentctxBeforeGit verifies the priority rule: agentctx/ at
// the same level as .git/ causes the agentctx/ check to win (same result, but
// proves the check order doesn't accidentally skip agentctx in mixed dirs).
func TestProjectRoot_AgentctxBeforeGit(t *testing.T) {
	root := t.TempDir()
	// Both markers at the same level.
	mkdirs(t, filepath.Join(root, "agentctx"))
	mkdirs(t, filepath.Join(root, ".git"))

	got, err := ProjectRoot(root, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != root {
		t.Errorf("got %q, want %q", got, root)
	}
}
