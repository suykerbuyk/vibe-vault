// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// installTestEnv sets up an isolated $HOME and a fresh vault dir for an
// install test. Returns (vaultPath, homeDir).
func installTestEnv(t *testing.T) (string, string) {
	t.Helper()
	home := t.TempDir()
	vault := t.TempDir()
	t.Setenv("HOME", home)
	return vault, home
}

func TestEnsureMergeDriverInstalled_FreshVault(t *testing.T) {
	vault, home := installTestEnv(t)

	installed, err := EnsureMergeDriverInstalled(vault)
	if err != nil {
		t.Fatalf("EnsureMergeDriverInstalled: %v", err)
	}
	if !installed {
		t.Fatalf("installed = false, want true (fresh vault)")
	}

	gaData, err := os.ReadFile(filepath.Join(vault, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	if !strings.Contains(string(gaData), "*.surface merge=vv-surface") {
		t.Errorf(".gitattributes missing vv-surface entry:\n%s", gaData)
	}

	gcData, err := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if err != nil {
		t.Fatalf("read .gitconfig: %v", err)
	}
	gc := string(gcData)
	if !strings.Contains(gc, `[merge "vv-surface"]`) {
		t.Errorf(".gitconfig missing [merge \"vv-surface\"] section:\n%s", gc)
	}
	if !strings.Contains(gc, "driver = vv vault merge-driver %O %A %B") {
		t.Errorf(".gitconfig missing driver line:\n%s", gc)
	}
}

func TestEnsureMergeDriverInstalled_AlreadyInstalled(t *testing.T) {
	vault, home := installTestEnv(t)

	// Pre-populate both files with the expected entries so the second call
	// can detect them.
	if err := os.WriteFile(
		filepath.Join(vault, ".gitattributes"),
		[]byte("*.surface merge=vv-surface\n"),
		0o644,
	); err != nil {
		t.Fatalf("seed .gitattributes: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(home, ".gitconfig"),
		[]byte("[merge \"vv-surface\"]\n\tdriver = vv vault merge-driver %O %A %B\n"),
		0o644,
	); err != nil {
		t.Fatalf("seed .gitconfig: %v", err)
	}

	installed, err := EnsureMergeDriverInstalled(vault)
	if err != nil {
		t.Fatalf("EnsureMergeDriverInstalled: %v", err)
	}
	if installed {
		t.Fatalf("installed = true, want false (already installed)")
	}

	// Idempotency: running a second time also reports false.
	installed2, err := EnsureMergeDriverInstalled(vault)
	if err != nil {
		t.Fatalf("EnsureMergeDriverInstalled (2nd): %v", err)
	}
	if installed2 {
		t.Fatalf("installed (2nd) = true, want false")
	}
}

func TestEnsureMergeDriverInstalled_PartialInstall(t *testing.T) {
	vault, home := installTestEnv(t)

	// Only the .gitattributes side is pre-installed. The gitconfig side
	// must be written; the function should report installed=true.
	if err := os.WriteFile(
		filepath.Join(vault, ".gitattributes"),
		[]byte("*.surface merge=vv-surface\n"),
		0o644,
	); err != nil {
		t.Fatalf("seed .gitattributes: %v", err)
	}

	installed, err := EnsureMergeDriverInstalled(vault)
	if err != nil {
		t.Fatalf("EnsureMergeDriverInstalled: %v", err)
	}
	if !installed {
		t.Fatalf("installed = false, want true (gitconfig side not yet installed)")
	}

	gcData, err := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if err != nil {
		t.Fatalf("read .gitconfig: %v", err)
	}
	if !strings.Contains(string(gcData), `[merge "vv-surface"]`) {
		t.Errorf(".gitconfig missing section after partial install:\n%s", gcData)
	}

	// .gitattributes must not have grown a duplicate line.
	gaData, err := os.ReadFile(filepath.Join(vault, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	count := strings.Count(string(gaData), "*.surface merge=vv-surface")
	if count != 1 {
		t.Errorf(".gitattributes has %d copies of the entry, want 1:\n%s", count, gaData)
	}
}

func TestEnsureMergeDriverInstalled_PreservesExistingContent(t *testing.T) {
	vault, home := installTestEnv(t)

	gaSeed := "# vault-managed gitattributes\n*.md text\n*.png binary\n"
	gcSeed := "[user]\n\temail = test@example.com\n[core]\n\tautocrlf = false\n"

	if err := os.WriteFile(filepath.Join(vault, ".gitattributes"), []byte(gaSeed), 0o644); err != nil {
		t.Fatalf("seed .gitattributes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(gcSeed), 0o644); err != nil {
		t.Fatalf("seed .gitconfig: %v", err)
	}

	installed, err := EnsureMergeDriverInstalled(vault)
	if err != nil {
		t.Fatalf("EnsureMergeDriverInstalled: %v", err)
	}
	if !installed {
		t.Fatalf("installed = false, want true")
	}

	gaData, err := os.ReadFile(filepath.Join(vault, ".gitattributes"))
	if err != nil {
		t.Fatalf("read .gitattributes: %v", err)
	}
	gc := string(gaData)
	for _, want := range []string{
		"# vault-managed gitattributes",
		"*.md text",
		"*.png binary",
		"*.surface merge=vv-surface",
	} {
		if !strings.Contains(gc, want) {
			t.Errorf(".gitattributes missing %q after install:\n%s", want, gc)
		}
	}

	gcData, err := os.ReadFile(filepath.Join(home, ".gitconfig"))
	if err != nil {
		t.Fatalf("read .gitconfig: %v", err)
	}
	gcs := string(gcData)
	for _, want := range []string{
		"[user]",
		"email = test@example.com",
		"[core]",
		"autocrlf = false",
		`[merge "vv-surface"]`,
		"driver = vv vault merge-driver %O %A %B",
	} {
		if !strings.Contains(gcs, want) {
			t.Errorf(".gitconfig missing %q after install:\n%s", want, gcs)
		}
	}
}

func TestEnsureMergeDriverInstalled_Idempotent(t *testing.T) {
	vault, _ := installTestEnv(t)

	installed1, err := EnsureMergeDriverInstalled(vault)
	if err != nil || !installed1 {
		t.Fatalf("first install: installed=%v err=%v", installed1, err)
	}
	installed2, err := EnsureMergeDriverInstalled(vault)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if installed2 {
		t.Fatalf("second install reported installed=true, want false")
	}
}

func TestUserHome_HomeEnvSet(t *testing.T) {
	t.Setenv("HOME", "/tmp/some-fake-home")
	got, err := userHome()
	if err != nil {
		t.Fatalf("userHome: %v", err)
	}
	if got != "/tmp/some-fake-home" {
		t.Errorf("userHome = %q, want %q", got, "/tmp/some-fake-home")
	}
}

func TestUserHome_FallsBackWhenHomeUnset(t *testing.T) {
	// When $HOME is unset, userHome falls back to os.UserHomeDir, which on
	// most platforms either returns a non-empty path or an error. Either
	// non-error result is acceptable; we're verifying the fallback branch
	// is exercised, not its specific outcome.
	t.Setenv("HOME", "")
	got, err := userHome()
	if err != nil {
		// Acceptable on hosts where os.UserHomeDir cannot resolve a home
		// (no $HOME and no /etc/passwd entry). The fallback branch ran.
		return
	}
	if got == "" {
		t.Errorf("userHome with HOME unset returned empty path and nil error")
	}
}

func TestEnsureGitattributesEntry_EmptyVaultPath(t *testing.T) {
	installed, err := ensureGitattributesEntry("")
	if err != nil {
		t.Fatalf("ensureGitattributesEntry(\"\"): %v", err)
	}
	if installed {
		t.Errorf("ensureGitattributesEntry(\"\") = true, want false")
	}
}

func TestEnsureGitattributesEntry_ReadError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file mode permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitattributes")
	if err := os.WriteFile(path, []byte("anything"), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ensureGitattributesEntry(dir)
	if err == nil {
		t.Errorf("ensureGitattributesEntry on unreadable file: err = nil, want non-nil")
	}
}

func TestEnsureGitconfigSection_ReadError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file mode permissions")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".gitconfig")
	if err := os.WriteFile(path, []byte("anything"), 0o000); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := ensureGitconfigSection()
	if err == nil {
		t.Errorf("ensureGitconfigSection on unreadable file: err = nil, want non-nil")
	}
}

func TestContainsLine(t *testing.T) {
	cases := []struct {
		name string
		data string
		line string
		want bool
	}{
		{"empty data", "", "x", false},
		{"exact match", "a\nb\nc", "b", true},
		{"trimmed match", "  hello  \nworld\n", "hello", true},
		{"prefix only no match", "*.surface merge=vv-surface-other", "*.surface merge=vv-surface", false},
		{"trailing newline", "*.surface merge=vv-surface\n", "*.surface merge=vv-surface", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := containsLine([]byte(tc.data), tc.line); got != tc.want {
				t.Errorf("containsLine(%q, %q) = %v, want %v", tc.data, tc.line, got, tc.want)
			}
		})
	}
}
