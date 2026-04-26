// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package vaultfs

// Acceptance tests for the production auto-memory shared-storage pattern
// (D14): the host-side ~/.claude/projects/<slug>/memory/ is a symlink that
// points INTO the vault's Projects/<p>/agentctx/memory/, which is itself a
// regular directory. AI calls to vaultfs.{Write,Read,Edit,Delete} go through
// the vault path; Claude Code's auto-memory tooling reads/writes via the
// host-side symlink. The production setup (verified via `readlink`) is:
//
//	# vault is the real directory:
//	$VAULT/Projects/<p>/agentctx/memory/   <- regular directory
//	$HOME/.claude/projects/<slug>/memory   -> $VAULT/Projects/<p>/agentctx/memory/
//
// These tests mirror that exact direction (host-symlink-INTO-vault) and
// assert that vault-side mutations converge on the same physical inodes
// observed via the host-side symlink view.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// autoMemorySetup creates:
//   - vault tempdir V with Projects/foo/agentctx/memory/ as a real directory
//   - host  tempdir H with H/memory -> V/Projects/foo/agentctx/memory  (symlink)
//
// Returns (vault, host, hostMem, vaultMemRel) where hostMem is the symlink
// path Claude Code's tooling uses, and vaultMemRel is the vault-relative path
// of the memory directory.
func autoMemorySetup(t *testing.T) (vault, host, hostMem, vaultMemRel string) {
	t.Helper()
	vault = t.TempDir()
	host = t.TempDir()
	vaultMemRel = filepath.Join("Projects", "foo", "agentctx", "memory")
	vaultMemAbs := filepath.Join(vault, vaultMemRel)
	if err := os.MkdirAll(vaultMemAbs, 0o755); err != nil {
		t.Fatalf("mkdir vault memory: %v", err)
	}
	hostMem = filepath.Join(host, "memory")
	if err := os.Symlink(vaultMemAbs, hostMem); err != nil {
		t.Skipf("symlink unsupported on this filesystem: %v", err)
	}
	return vault, host, hostMem, vaultMemRel
}

// TestVaultfs_AutoMemoryWrite_VisibleViaHostSymlink writes a memory file via
// the canonical vault path (vaultfs.Write) and asserts the file is visible
// through the host-side symlink view, using the same physical inode.
func TestVaultfs_AutoMemoryWrite_VisibleViaHostSymlink(t *testing.T) {
	vault, _, hostMem, vaultMemRel := autoMemorySetup(t)

	rel := filepath.Join(vaultMemRel, "MEMORY.md")
	body := "auto-memory entry written via vaultfs\n"
	if _, err := Write(vault, rel, body, ""); err != nil {
		t.Fatalf("vaultfs.Write: %v", err)
	}

	// Vault-side file landed at the canonical real-dir location.
	vaultPath := filepath.Join(vault, rel)
	gotVault, err := os.ReadFile(vaultPath)
	if err != nil {
		t.Fatalf("read vault file: %v", err)
	}
	if string(gotVault) != body {
		t.Errorf("vault content = %q, want %q", gotVault, body)
	}

	// Host-side symlink view shows the same content.
	hostPath := filepath.Join(hostMem, "MEMORY.md")
	gotHost, err := os.ReadFile(hostPath)
	if err != nil {
		t.Fatalf("read via host symlink: %v", err)
	}
	if string(gotHost) != body {
		t.Errorf("host content = %q, want %q", gotHost, body)
	}

	// And both paths report the same inode (true convergence, not a copy).
	infoVault, err := os.Stat(vaultPath)
	if err != nil {
		t.Fatalf("stat vault: %v", err)
	}
	infoHost, err := os.Stat(hostPath)
	if err != nil {
		t.Fatalf("stat host: %v", err)
	}
	if !os.SameFile(infoVault, infoHost) {
		t.Errorf("vault and host paths should reference the same file (inode mismatch)")
	}
}

// TestVaultfs_AutoMemoryRead_ViaHostSymlink_ReturnsVaultContent writes
// directly to the vault location via stdlib (simulating an external tool
// landing content at the real dir) and reads via vaultfs.Read on the vault
// path. The host-symlink view is asserted to return the same bytes.
func TestVaultfs_AutoMemoryRead_ViaHostSymlink_ReturnsVaultContent(t *testing.T) {
	vault, _, hostMem, vaultMemRel := autoMemorySetup(t)

	rel := filepath.Join(vaultMemRel, "feedback_x.md")
	body := "feedback note from external tool\n"
	if err := os.WriteFile(filepath.Join(vault, rel), []byte(body), 0o644); err != nil {
		t.Fatalf("seed vault file: %v", err)
	}

	got, err := Read(vault, rel, 0)
	if err != nil {
		t.Fatalf("vaultfs.Read: %v", err)
	}
	if got.Content != body {
		t.Errorf("Read.Content = %q, want %q", got.Content, body)
	}

	// Reading via the host-side symlink yields the same bytes.
	gotHost, err := os.ReadFile(filepath.Join(hostMem, "feedback_x.md"))
	if err != nil {
		t.Fatalf("host read: %v", err)
	}
	if string(gotHost) != body {
		t.Errorf("host content = %q, want %q", gotHost, body)
	}
}

// TestVaultfs_AutoMemoryEdit_PropagatesViaHostSymlink uses vaultfs.Edit on
// the vault path and asserts the edit is observable through the host-side
// symlink view.
func TestVaultfs_AutoMemoryEdit_PropagatesViaHostSymlink(t *testing.T) {
	vault, _, hostMem, vaultMemRel := autoMemorySetup(t)

	rel := filepath.Join(vaultMemRel, "MEMORY.md")
	original := "Initial memory line.\nMore text.\n"
	if err := os.WriteFile(filepath.Join(vault, rel), []byte(original), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := Edit(vault, rel, "Initial memory line.", "Updated memory line.", false, "")
	if err != nil {
		t.Fatalf("vaultfs.Edit: %v", err)
	}
	if res.Replacements != 1 {
		t.Errorf("replacements = %d, want 1", res.Replacements)
	}

	// Host-side observes the new content.
	gotHost, err := os.ReadFile(filepath.Join(hostMem, "MEMORY.md"))
	if err != nil {
		t.Fatalf("host read: %v", err)
	}
	want := "Updated memory line.\nMore text.\n"
	if string(gotHost) != want {
		t.Errorf("host content = %q, want %q", gotHost, want)
	}
}

// TestVaultfs_AutoMemoryDelete_VisibleViaHostSymlink deletes a memory file
// through the vault path and asserts the host-side symlink view reflects the
// removal (file unreachable, ENOENT).
func TestVaultfs_AutoMemoryDelete_VisibleViaHostSymlink(t *testing.T) {
	vault, _, hostMem, vaultMemRel := autoMemorySetup(t)

	rel := filepath.Join(vaultMemRel, "feedback_doomed.md")
	if err := os.WriteFile(filepath.Join(vault, rel), []byte("ephemeral\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Sanity: visible via host before delete.
	if _, err := os.Stat(filepath.Join(hostMem, "feedback_doomed.md")); err != nil {
		t.Fatalf("pre-delete host stat: %v", err)
	}

	if _, err := Delete(vault, rel, ""); err != nil {
		t.Fatalf("vaultfs.Delete: %v", err)
	}

	// Vault-side: gone.
	if _, err := os.Stat(filepath.Join(vault, rel)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("vault file should be gone, stat err = %v", err)
	}
	// Host-side: also gone (same inode reachability).
	if _, err := os.Stat(filepath.Join(hostMem, "feedback_doomed.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("host view should be gone, stat err = %v", err)
	}
}
