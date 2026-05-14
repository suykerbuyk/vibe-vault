// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestGoldenFixture_VibeVaultFlows asserts the testdata fixture translated
// from the 2026-05-13 spike artifact parses cleanly into FlowDoc and passes
// Validate. It is the Phase 1 acceptance check for the flowdoc-command epic
// and the foundation Phase 6's integration test will build on.
//
// If this test breaks after a schema change, fix the fixture (or the
// translation step that produced it) — do not relax Validate.
func TestGoldenFixture_VibeVaultFlows(t *testing.T) {
	path := filepath.Join("testdata", "golden", "vibe-vault-flows.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	var doc FlowDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal fixture %s: %v", path, err)
	}

	if err := Validate(&doc); err != nil {
		t.Fatalf("Validate fixture %s: %v", path, err)
	}

	// Light shape checks so a regression that empties nodes/flows is loud.
	if got := len(doc.Nodes); got < 25 || got > 30 {
		t.Errorf("fixture node count = %d, want 25..30", got)
	}
	if got := len(doc.Flows); got < 20 || got > 22 {
		t.Errorf("fixture flow count = %d, want 20..22", got)
	}
	if doc.Project != "vibe-vault" {
		t.Errorf("fixture project = %q, want \"vibe-vault\"", doc.Project)
	}
}
