// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package sessionclaim

import (
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/pidlive"
)

// Harness names. Stable string literals; persisted in the claim file
// and consumed by EffectiveSource.
const (
	HarnessClaudeCode = "claude-code"
	HarnessZedMCP     = "zed-mcp"
	HarnessUnknown    = "unknown"
)

// DetectHarness inspects the parent process's command name (via
// pidlive.ParentName) and classifies it into one of the canonical
// harness identifiers.
//
// Match is case-insensitive substring:
//   - contains "claude" → "claude-code"
//   - contains "zed"    → "zed-mcp"
//   - anything else     → "unknown"
//
// Any error from pidlive.ParentName (file missing, permission denied,
// gopsutil failure) yields "unknown" — the safe fallthrough specified
// by Mechanism 2 decision 6. Windows always returns "unknown" because
// pidlive.ParentName returns empty there.
func DetectHarness(ppid int) string {
	name, err := pidlive.ParentName(ppid)
	if err != nil || name == "" {
		return HarnessUnknown
	}
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "claude"):
		return HarnessClaudeCode
	case strings.Contains(lower, "zed"):
		return HarnessZedMCP
	default:
		return HarnessUnknown
	}
}
