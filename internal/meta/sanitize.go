// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package meta

import (
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/sanitize"
)

// SanitizeCWDForEmit prepares a raw cwd for emission in vault writes.
// Returns empty string if cwd is empty or resolves to a path inside the
// provided vault prefix (vault-rooted cwds are noise — the target path
// is already encoded in the project field). Otherwise returns the
// home-compressed form with trailer-unsafe byte sequences neutralized:
//
//   - "-->" becomes "--" (the iteration-block trailer regex terminates
//     on the first "-->", so a raw substring match would truncate).
//   - Newlines become spaces (trailer format is single-line; YAML value
//     emission is also single-line).
//
// vaultPath is the configured vault root (cfg.VaultPath). Pass "" to
// skip the vault-prefix check entirely (unit tests exercising the
// sanitization logic in isolation should do this).
func SanitizeCWDForEmit(cwd, vaultPath string) string {
	if cwd == "" {
		return ""
	}
	if vaultPath != "" && strings.HasPrefix(cwd, vaultPath) {
		return ""
	}
	cwd = sanitize.CompressHome(cwd)
	cwd = strings.ReplaceAll(cwd, "-->", "--")
	cwd = strings.ReplaceAll(cwd, "\n", " ")
	return cwd
}
