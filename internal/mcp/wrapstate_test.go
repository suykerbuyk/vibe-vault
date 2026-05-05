// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNextIterFromIterationsMD(t *testing.T) {
	tests := []struct {
		name    string
		content string // empty means do not write the file
		want    int
	}{
		{name: "missing file", content: "", want: 1},
		{name: "no headers", content: "# Iterations\n\nno entries yet\n", want: 1},
		{name: "single header", content: "### Iteration 1 — first (2026-01-01)\n", want: 2},
		{name: "many headers", content: "### Iteration 40 — a\n### Iteration 41 — b\n### Iteration 168 — z\n", want: 169},
		{name: "out of order", content: "### Iteration 168 — z\n### Iteration 40 — a\n", want: 169},
		{name: "h2 ignored", content: "## Iteration 999 — wrong level\n### Iteration 7 — right\n", want: 8},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vaultRoot := t.TempDir()
			projAgentctx := filepath.Join(vaultRoot, "Projects", "myproj", "agentctx")
			if err := os.MkdirAll(projAgentctx, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if tc.content != "" {
				if err := os.WriteFile(filepath.Join(projAgentctx, "iterations.md"), []byte(tc.content), 0o644); err != nil {
					t.Fatalf("write iterations.md: %v", err)
				}
			}
			got, err := nextIterFromIterationsMD(vaultRoot, "myproj")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNextIterFromIterationsMD_EmptyArgs(t *testing.T) {
	if got, err := nextIterFromIterationsMD("", "myproj"); err != nil || got != 1 {
		t.Errorf("empty vault path: got (%d, %v), want (1, nil)", got, err)
	}
	if got, err := nextIterFromIterationsMD(t.TempDir(), ""); err != nil || got != 1 {
		t.Errorf("empty project: got (%d, %v), want (1, nil)", got, err)
	}
}
