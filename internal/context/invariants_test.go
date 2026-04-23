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

func TestValidateCurrentStateBody(t *testing.T) {
	overCap := "- **Tests:** " + strings.Repeat("x", 201)

	tests := []struct {
		name      string
		body      string
		wantOK    bool
		wantBadIn string // substring that must appear in badLine when !wantOK
	}{
		{
			name: "all valid bullets",
			body: "- **Iterations:** 125 complete.\n" +
				"- **Tests:** 1409 across 36 packages.\n" +
				"- **Lint:** clean.\n" +
				"- **Schema:** v10.\n" +
				"- **Module:** `github.com/suykerbuyk/vibe-vault`.\n" +
				"- **MCP:** 20 tools + 1 prompt.\n" +
				"- **Embedded templates:** 20.\n",
			wantOK: true,
		},
		{
			name:   "blank lines between bullets",
			body:   "\n- **Tests:** 1409.\n\n- **Lint:** clean.\n\n",
			wantOK: true,
		},
		{
			name:   "single-line HTML comment interleaved",
			body:   "- **Tests:** 1409.\n<!-- inline note -->\n- **Lint:** clean.\n",
			wantOK: true,
		},
		{
			name: "multi-line HTML comment skipped",
			body: "- **Tests:** 1409.\n" +
				"<!--\n" +
				"  Note: MCP count bumps whenever a new tool lands.\n" +
				"  This comment must not trip the validator.\n" +
				"-->\n" +
				"- **MCP:** 20 tools + 1 prompt.\n",
			wantOK: true,
		},
		{
			name:   "markdown subheading skipped",
			body:   "### Sub-topic\n- **Tests:** 1409.\n",
			wantOK: true,
		},
		{
			name:      "continuation line rejected",
			body:      "- **Tests:** 1409 across\n  36 packages.\n",
			wantOK:    false,
			wantBadIn: "36 packages",
		},
		{
			name:      "non-whitelisted key rejected",
			body:      "- **Tests:** 1409.\n- **Frobnicator:** enabled.\n",
			wantOK:    false,
			wantBadIn: "Frobnicator",
		},
		{
			name:      "trailing over 200 runes rejected",
			body:      overCap + "\n",
			wantOK:    false,
			wantBadIn: "xxxx",
		},
		{
			name:   "empty body passes",
			body:   "",
			wantOK: true,
		},
		{
			name: "unclosed HTML comment silently skips rest",
			body: "- **Tests:** 1409.\n" +
				"<!-- start of comment with no terminator\n" +
				"this paragraph would normally be rejected\n" +
				"but is swallowed by the open comment region.\n",
			wantOK: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bad, ok := ValidateCurrentStateBody(tc.body)
			if ok != tc.wantOK {
				t.Fatalf("ValidateCurrentStateBody ok = %v, want %v (bad = %q)", ok, tc.wantOK, bad)
			}
			if !tc.wantOK && !strings.Contains(bad, tc.wantBadIn) {
				t.Errorf("badLine = %q, want substring %q", bad, tc.wantBadIn)
			}
			if tc.wantOK && bad != "" {
				t.Errorf("expected empty badLine on success, got %q", bad)
			}
		})
	}
}
