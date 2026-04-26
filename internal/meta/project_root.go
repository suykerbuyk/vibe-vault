// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package meta

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrIsVaultRoot is returned by ProjectRoot when the discovered root equals
// the configured vault path. Callers that should not operate on the vault
// root (e.g. vv_set_commit_msg) treat this as a hard error.
var ErrIsVaultRoot = errors.New("matched directory is the vault root, not a project root")

// projectRootFunc is the test seam for deterministic unit tests.
// Production code uses the real walk; tests can replace this.
// Set to nil to use the real walk algorithm.
var projectRootFunc func(cwd, vaultPath string) (string, error)

// ProjectRoot walks up from cwd checking for an agentctx/ subdirectory
// (preferred) or a .git directory/file at each level. The first match is
// returned as the project root.
//
// If the matched directory equals vaultPath (or, when vaultPath is empty,
// the vault_path from ~/.config/vibe-vault/config.toml), ErrIsVaultRoot
// is returned. Callers decide how to handle it; wrap-related callers treat
// it as a hard error.
//
// Algorithm per level (locked):
//  1. Check agentctx/ subdirectory first. If found, this level is the candidate.
//  2. Else check .git/ directory or .git file. If found, this level is the candidate.
//  3. Else continue to parent. Stop at filesystem root → actionable error.
func ProjectRoot(cwd, vaultPath string) (string, error) {
	if projectRootFunc != nil {
		return projectRootFunc(cwd, vaultPath)
	}
	return projectRootWalk(cwd, vaultPath)
}

// projectRootWalk is the real walk algorithm used in production.
func projectRootWalk(cwd, vaultPath string) (string, error) {
	// Resolve vaultPath: if empty, load from config.
	resolvedVault := vaultPath
	if resolvedVault == "" {
		home, err := HomeDir()
		if err == nil && home != "" {
			// Read vault_path from config — defer to config package would create
			// an import cycle. Do a minimal TOML parse for vault_path only.
			cfgPath := filepath.Join(home, ".config", "vibe-vault", "config.toml")
			resolvedVault = readVaultPathFromConfig(cfgPath)
		}
	}

	// Canonicalize cwd.
	dir := cwd
	if !filepath.IsAbs(dir) {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", fmt.Errorf("resolve cwd %q: %w", cwd, err)
		}
		dir = abs
	}

	// Canonicalize vaultPath for comparison.
	absVault := ""
	if resolvedVault != "" {
		abs, err := filepath.Abs(resolvedVault)
		if err == nil {
			absVault = abs
		}
	}

	for {
		// Step 1: check agentctx/ first.
		if fi, err := os.Stat(filepath.Join(dir, "agentctx")); err == nil && fi.IsDir() {
			return checkCandidate(dir, absVault)
		}

		// Step 2: check .git directory or file.
		if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			_ = fi // present as either file or dir; both are valid
			return checkCandidate(dir, absVault)
		}

		// Step 3: move to parent.
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding a marker.
			return "", fmt.Errorf("no project root found walking up from %q: no agentctx/ or .git marker", cwd)
		}
		dir = parent
	}
}

// checkCandidate validates a candidate directory against the vault root.
func checkCandidate(dir, absVault string) (string, error) {
	if absVault != "" && dir == absVault {
		return "", ErrIsVaultRoot
	}
	return dir, nil
}

// readVaultPathFromConfig does a minimal line-scan of a config.toml to extract
// the vault_path value. This avoids importing the config package (import cycle)
// while still resolving the vault path when vaultPath is empty.
func readVaultPathFromConfig(cfgPath string) string {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return ""
	}
	// Look for a line matching: vault_path = "..."
	lines := splitLines(string(data))
	for _, line := range lines {
		val := extractTOMLString(line, "vault_path")
		if val != "" {
			// Expand leading ~/ with home dir.
			if len(val) >= 2 && val[:2] == "~/" {
				home, err := HomeDir()
				if err == nil && home != "" {
					return filepath.Join(home, val[2:])
				}
			}
			return val
		}
	}
	return ""
}

// splitLines splits a string on newlines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// extractTOMLString extracts the string value from a TOML line of the form
// `key = "value"` or `key = 'value'`. Returns "" if the line does not match.
func extractTOMLString(line, key string) string {
	// Trim spaces.
	trimmed := trimSpace(line)
	prefix := key + " ="
	if len(trimmed) <= len(prefix) {
		return ""
	}
	// Match key followed by optional spaces and =
	if !hasPrefix(trimmed, key) {
		return ""
	}
	rest := trimSpace(trimmed[len(key):])
	if len(rest) == 0 || rest[0] != '=' {
		return ""
	}
	rest = trimSpace(rest[1:])
	if len(rest) < 2 {
		return ""
	}
	quote := rest[0]
	if quote != '"' && quote != '\'' {
		return ""
	}
	end := len(rest) - 1
	for end > 0 && rest[end] != quote {
		end--
	}
	if end == 0 {
		return ""
	}
	return rest[1:end]
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
