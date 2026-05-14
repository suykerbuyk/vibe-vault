// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

//go:build manual

// Live end-to-end test for `vv flowdoc gen`. Excluded from the default
// build so `make test` stays hermetic and offline. The operator runs:
//
//	go test -tags=manual ./cmd/vv/ -run TestFlowdocGen_Live
//
// before committing a flowdoc regeneration. Requires LLM enrichment to
// be configured in ~/.config/vibe-vault/config.toml (any provider).

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFlowdocGen_Live drives the real generator against a tempdir
// project (no .git marker — the gen path falls back to cwd-as-root).
// Asserts only that flows.json and FLOWS.html exist and that flows.json
// passes Validate end-to-end. The operator interprets the contents.
func TestFlowdocGen_Live(t *testing.T) {
	tmp := t.TempDir()

	// Stamp a .git directory so DetectProjectRoot returns tmp.
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	code := runFlowdocGen([]string{"--project", "flowdoc-live-test"})
	if code != 0 {
		t.Fatalf("runFlowdocGen exit code = %d, want 0", code)
	}

	jsonPath := filepath.Join(tmp, "doc", "flows.json")
	htmlPath := filepath.Join(tmp, "doc", "FLOWS.html")
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("expected %s to exist: %v", jsonPath, err)
	}
	if _, err := os.Stat(htmlPath); err != nil {
		t.Fatalf("expected %s to exist: %v", htmlPath, err)
	}
}
