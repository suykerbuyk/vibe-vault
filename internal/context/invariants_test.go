// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"strings"
	"testing"
)

func TestIsInvariantBullet(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		// Whitelist keys — every first-word entry must pass at least once.
		{"iterations", "- **Iterations:** 124 complete.", true},
		{"tests", "- **Tests:** 1360 across 36 packages.", true},
		{"lint", "- **Lint:** clean.", true},
		{"schema", "- **Schema:** v10 (contract marker).", true},
		{"module", "- **Module:** `github.com/suykerbuyk/vibe-vault`.", true},
		{"mcp", "- **MCP:** 20 tools + 1 prompt.", true},
		{"embedded templates", "- **Embedded templates:** 20.", true},
		{"stack", "- **Stack:** Go 1.25.", true},
		{"binary", "- **Binary:** `~/.local/bin/vv`.", true},
		{"config", "- **Config:** `~/.config/vibe-vault/config.toml`.", true},
		{"bootstrap payload", "- **Bootstrap payload:** ~11 KB.", true},
		{"build", "- **Build:** `make build`.", true},
		{"cli", "- **CLI:** 12 subcommands.", true},
		{"license", "- **License:** Apache-2.0 OR MIT.", true},
		{"git", "- **Git:** `main` tracked against `github/main`.", true},
		{"coverage", "- **Coverage:** 78%.", true},
		{"distribution", "- **Distribution:** source + binary.", true},
		{"external dep", "- **External dep:** none.", true},

		// Exclusion keys — canonical narrative-prone first words.
		{"phase", "- **Phase:** 3 in progress.", false},
		{"status", "- **Status:** Planning complete.", false},
		{"workflow", "- **Workflow:** TBD.", false},
		{"recent", "- **Recent:** see iterations.md.", false},
		{"new capability", "- **New capability:** Zed watcher.", false},
		{"latest artifacts", "- **Latest artifacts:** see release.", false},

		// Trailing-content length gate.
		{"at limit", "- **Tests:** " + strings.Repeat("x", 200), true},
		{"one over limit", "- **Tests:** " + strings.Repeat("x", 201), false},
		{"far over limit", "- **Tests:** " + strings.Repeat("x", 1000), false},
		{"empty trailing", "- **Tests:**", true},

		// Continuation lines — no **Key:** marker.
		{"continuation indented", "  continued text without marker.", false},
		{"continuation bulleted", "- continued text without marker.", false},
		{"paragraph line", "Some narrative paragraph.", false},

		// Malformed markdown.
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"no bold markers", "Tests: 1360.", false},
		{"single asterisks", "*Tests:* 1360.", false},
		{"missing closing bold", "- **Tests: 1360.", false},
		{"key starts lowercase", "- **tests:** 1360.", false},
		{"key empty", "- **:** content.", false},
		{"no colon", "- **Tests** 1360.", false},
		{"key with punctuation", "- **Tests!:** 1360.", false},
		{"parenthesized key rejected", "- **Tests (unit):** 1360.", false},

		// Layout variations that should still pass.
		{"no dash leading", "**Tests:** 1360.", true},
		{"extra indent", "    - **Tests:** 1360.", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsInvariantBullet(tc.in)
			if got != tc.want {
				t.Errorf("IsInvariantBullet(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
