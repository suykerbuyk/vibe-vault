// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

//go:build integration

package memory

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// TestIntegration_LinkAndWritethrough exercises the real vv binary
// end-to-end: builds it, runs `vv memory link` against tempdir-rooted
// fake HOME and fake VibeVault, writes a file via the symlink, and
// asserts that (a) the file appears on the vault side and (b) both
// paths report the same inode.
func TestIntegration_LinkAndWritethrough(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	vault := filepath.Join(root, "vault")
	project := filepath.Join(root, "work", "int-demo")
	agentctx := filepath.Join(vault, "Projects", filepath.Base(project), "agentctx")
	for _, d := range []string{home, vault, project, agentctx} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	// Minimal config so `vv` can load without error. Tests that mutate
	// $HOME for XDG resolution implicitly place config under the fake
	// home via the same env.
	xdg := filepath.Join(home, ".config", "vibe-vault")
	if err := os.MkdirAll(xdg, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := "vault_path = \"" + vault + "\"\n"
	if err := os.WriteFile(filepath.Join(xdg, "config.toml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	binPath := filepath.Join(root, "vv-int")
	// Resolve repo root: the test file sits at
	// internal/memory/memory_integration_test.go.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))

	build := exec.Command("go", "build", "-o", binPath, "./cmd/vv")
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	cmd := exec.Command(binPath, "memory", "link", "--working-dir", project)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vv memory link: %v\n%s", err, out)
	}

	resolvedProj, err := filepath.EvalSymlinks(project)
	if err != nil {
		resolvedProj = project
	}
	slug := slugFromPath(resolvedProj)
	source := filepath.Join(home, ".claude", "projects", slug, "memory")
	target := filepath.Join(agentctx, "memory")

	// Symlink exists and points at the vault target.
	tgt, err := os.Readlink(source)
	if err != nil {
		t.Fatalf("readlink %s: %v", source, err)
	}
	if tgt != target {
		t.Errorf("symlink target = %s, want %s", tgt, target)
	}

	// Writethrough: write via the symlink, read from vault side.
	payload := []byte("integration writethrough\n")
	if err := os.WriteFile(filepath.Join(source, "note.md"), payload, 0o644); err != nil {
		t.Fatalf("write via symlink: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "note.md")); err != nil || string(got) != string(payload) {
		t.Errorf("vault-side read: err=%v got=%q", err, got)
	}

	// Inode equivalence.
	var sa, sb syscall.Stat_t
	if err := syscall.Stat(filepath.Join(source, "note.md"), &sa); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Stat(filepath.Join(target, "note.md"), &sb); err != nil {
		t.Fatal(err)
	}
	if sa.Ino != sb.Ino {
		t.Errorf("inode mismatch: %d vs %d", sa.Ino, sb.Ino)
	}

	// Round-trip unlink.
	un := exec.Command(binPath, "memory", "unlink", "--working-dir", project)
	un.Env = cmd.Env
	if out, err := un.CombinedOutput(); err != nil {
		t.Fatalf("vv memory unlink: %v\n%s", err, out)
	}
	info, err := os.Lstat(source)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("expected real dir after unlink")
	}
	// Vault copy preserved.
	if _, err := os.Stat(filepath.Join(target, "note.md")); err != nil {
		t.Errorf("vault copy missing: %v", err)
	}
}
