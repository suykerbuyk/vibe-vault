// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// flowdoc_test.go is the single canonical "the golden fixture is a valid
// end-to-end FlowDoc contract" integration test for the flowdoc-command
// epic. It absorbs the Phase 1 acceptance check that previously lived in
// golden_test.go (now deleted) and adds structural-landmark, ref-resolution,
// and shape-bound assertions on top.
//
// Scope: this file exercises only the shipped flowdoc package surface
// (Validate, VerifyRefs, FormatRefIssues) against the committed golden
// fixture. No mocking, no LLM — it runs unconditionally in `make test`.
//
// If a test here fails, the fix belongs in the fixture (or the generator
// that produced it), not in a relaxed assertion.

// loadGoldenDoc opens the committed golden fixture and unmarshals it into a
// FlowDoc. It fails the test on any read or parse error. All subtests in
// this file go through this helper so the fixture is loaded exactly one way.
func loadGoldenDoc(t *testing.T) *FlowDoc {
	t.Helper()
	path := filepath.Join("testdata", "golden", "vibe-vault-flows.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden fixture %s: %v", path, err)
	}
	var doc FlowDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal golden fixture %s: %v", path, err)
	}
	return &doc
}

// repoRoot — walk up to the directory containing go.mod — is intentionally
// NOT redefined here. verify_test.go already provides a package-level
// repoRoot helper with exactly this behavior; both test files are in
// `package flowdoc`, so TestGoldenFixture_RefsResolve below reuses that
// single definition. Defining a second copy would be a duplicate-symbol
// compile error, and keeping one shared helper guarantees the two files
// stay consistent.

// TestGoldenFixture_SchemaConformance is the preserved Phase 1 acceptance
// check: the golden fixture parses cleanly and passes Validate.
func TestGoldenFixture_SchemaConformance(t *testing.T) {
	doc := loadGoldenDoc(t)
	if err := Validate(doc); err != nil {
		t.Fatalf("Validate(golden) returned error: %v", err)
	}
	if doc.Project != "vibe-vault" {
		t.Errorf("golden project = %q, want %q", doc.Project, "vibe-vault")
	}
}

// TestGoldenFixture_KnownNodesPresent asserts a set of stable structural
// landmark node IDs survive every regeneration of the golden fixture. If a
// future regen silently drops one of these, that is a signal worth a loud
// test failure rather than quiet drift.
func TestGoldenFixture_KnownNodesPresent(t *testing.T) {
	doc := loadGoldenDoc(t)

	have := make(map[string]struct{}, len(doc.Nodes))
	for _, n := range doc.Nodes {
		have[n.ID] = struct{}{}
	}

	want := []string{
		"internal/mcp",
		"cmd/vv",
		"internal/wraprender",
		"internal/llm",
		"internal/vaultsync",
		"claude-code",
	}

	var missing []string
	for _, id := range want {
		if _, ok := have[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		t.Errorf("golden fixture is missing %d expected landmark node(s): %v", len(missing), missing)
	}
}

// TestGoldenFixture_RefsResolve runs VerifyRefs against the real repo tree
// and asserts ZERO hard errors. Weak-match warnings are expected and fine —
// the golden has many path:line refs pointing inside function bodies — so
// they are only logged, never failed on. On a hard-error failure it prints
// FormatRefIssues of the error subset so the failure is actionable.
func TestGoldenFixture_RefsResolve(t *testing.T) {
	doc := loadGoldenDoc(t)
	root := repoRoot(t)

	issues := VerifyRefs(doc, root)

	var errIssues []RefIssue
	warnings := 0
	for _, iss := range issues {
		if iss.IsError() {
			errIssues = append(errIssues, iss)
		} else {
			warnings++
		}
	}

	t.Logf("golden fixture VerifyRefs: %d hard error(s), %d weak-match warning(s)", len(errIssues), warnings)

	if len(errIssues) > 0 {
		t.Errorf("golden fixture produced %d hard ref error(s):\n%s",
			len(errIssues), FormatRefIssues(errIssues))
	}
}

// TestGoldenFixture_ShapeBounds asserts the fixture has not been
// catastrophically truncated by a bad regen. The bounds are intentionally
// loose — this catches an emptied/halved document, not normal drift.
func TestGoldenFixture_ShapeBounds(t *testing.T) {
	doc := loadGoldenDoc(t)

	if got := len(doc.Nodes); got < 25 || got > 35 {
		t.Errorf("golden node count = %d, want within [25,35]", got)
	}
	if got := len(doc.Flows); got < 18 || got > 25 {
		t.Errorf("golden flow count = %d, want within [18,25]", got)
	}
}
