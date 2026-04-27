// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package main

import (
	"bytes"
	"io"
	"log"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/help"
	"github.com/suykerbuyk/vibe-vault/internal/mcp"
)

// newRegisteredTestServer builds an *mcp.Server populated by registerMCPTools
// against a default config — the same registration the live `vv mcp check`
// command uses.
func newRegisteredTestServer(t *testing.T) *mcp.Server {
	t.Helper()
	cfg := config.DefaultConfig()
	logger := log.New(io.Discard, "", 0)
	srv := mcp.NewServer(mcp.ServerInfo{Name: "vibe-vault", Version: help.Version}, logger)
	registerMCPTools(srv, cfg)
	srv.SetInstructions(mcpInstructions)
	return srv
}

func TestMcpCheck_ToolsFlag_OutputsSortedNames(t *testing.T) {
	srv := newRegisteredTestServer(t)

	var buf bytes.Buffer
	printToolNames(&buf, srv)

	out := buf.String()
	if strings.TrimSpace(out) == "" {
		t.Fatal("printToolNames produced empty output; expected at least one tool")
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("expected at least one tool line")
	}

	// Tool names are lower_snake_case starting with the vv_ prefix; digits are
	// allowed (e.g. vv_vault_sha256).
	namePattern := regexp.MustCompile(`^vv_[a-z0-9_]+$`)
	for _, line := range lines {
		if !namePattern.MatchString(line) {
			t.Errorf("tool name %q does not match ^vv_[a-z0-9_]+$", line)
		}
	}

	sorted := append([]string(nil), lines...)
	sort.Strings(sorted)
	for i := range lines {
		if lines[i] != sorted[i] {
			t.Errorf("output is not in sorted order at index %d: got %q, want %q", i, lines[i], sorted[i])
		}
	}
}

func TestMcpCheck_ToolsFlag_SkipsComplianceChecks(t *testing.T) {
	srv := newRegisteredTestServer(t)

	var buf bytes.Buffer
	printToolNames(&buf, srv)

	out := buf.String()
	if strings.Contains(out, "[PASS]") {
		t.Errorf("--tools output unexpectedly contains [PASS] marker:\n%s", out)
	}
	if strings.Contains(out, "[FAIL]") {
		t.Errorf("--tools output unexpectedly contains [FAIL] marker:\n%s", out)
	}
}

func TestMcpCheck_ToolsFlag_HelpMentionsFlag(t *testing.T) {
	if !strings.Contains(mcpCheckHelp, "--tools") {
		t.Errorf("mcpCheckHelp does not mention --tools flag:\n%s", mcpCheckHelp)
	}
}
