// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package meta

import (
	"testing"
)

// TestSanitizeCWDForEmit pins the documented behavior of the helper:
// vault-prefix short-circuit, home compression, and trailer-unsafe byte
// neutralization. HOME is set explicitly via t.Setenv so CompressHome's
// reading of os.UserHomeDir() is deterministic regardless of where the
// test runs.
func TestSanitizeCWDForEmit(t *testing.T) {
	// Pin HOME so sanitize.CompressHome is deterministic across machines.
	// All subtests inherit this; ones that need a different HOME can
	// override locally with t.Setenv.
	t.Setenv("HOME", "/home/johns")

	tests := []struct {
		name      string
		cwd       string
		vaultPath string
		want      string
	}{
		{
			name:      "empty input returns empty",
			cwd:       "",
			vaultPath: "/home/johns/obsidian/VibeVault",
			want:      "",
		},
		{
			name:      "inside vault returns empty",
			cwd:       "/home/johns/obsidian/VibeVault/Projects/foo",
			vaultPath: "/home/johns/obsidian/VibeVault",
			want:      "",
		},
		{
			name:      "empty vault prefix falls through to compression",
			cwd:       "/home/johns/code/recmeet",
			vaultPath: "",
			want:      "~/code/recmeet",
		},
		{
			name:      "home compression when vault prefix does not match",
			cwd:       "/home/johns/code/recmeet",
			vaultPath: "/some/other/path",
			want:      "~/code/recmeet",
		},
		{
			name:      "arrow sequence is neutralized",
			cwd:       "/tmp/a-->b",
			vaultPath: "",
			want:      "/tmp/a--b",
		},
		{
			name:      "embedded newline becomes space",
			cwd:       "/tmp/line1\nline2",
			vaultPath: "",
			want:      "/tmp/line1 line2",
		},
		{
			// Known limitation: strings.HasPrefix matches any path that
			// textually begins with vaultPath, including sibling
			// directories whose name shares a prefix with the vault
			// (e.g. "VibeVault_backup" when the vault is "VibeVault").
			// Operators should avoid naming directories that share a
			// prefix with the vault root. If this ever becomes a real
			// problem, fix by requiring a trailing "/" boundary; the
			// current behavior is pinned here deliberately so a change
			// is caught by a failing test.
			name:      "prefix-only match is treated as inside vault (known limitation)",
			cwd:       "/home/johns/obsidian/VibeVault_backup/thing",
			vaultPath: "/home/johns/obsidian/VibeVault",
			want:      "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeCWDForEmit(tc.cwd, tc.vaultPath)
			if got != tc.want {
				t.Errorf("SanitizeCWDForEmit(%q, %q) = %q, want %q",
					tc.cwd, tc.vaultPath, got, tc.want)
			}
		})
	}
}
