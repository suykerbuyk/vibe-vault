// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRender_GoldenFixture loads the Phase 1 golden fixture, renders the
// embedded HTML viewer against it, and asserts the rendered document is
// well-formed: it contains the doctype, the inlined JSON, every node ID,
// and no leftover template placeholder.
func TestRender_GoldenFixture(t *testing.T) {
	path := filepath.Join("testdata", "golden", "vibe-vault-flows.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	var doc FlowDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	var buf bytes.Buffer
	if err := Render(&buf, &doc); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out := buf.String()

	// doctype (HTML5; the spike used uppercase but either is acceptable).
	if !strings.Contains(out, "<!DOCTYPE html>") && !strings.Contains(out, "<!doctype html>") {
		t.Errorf("rendered output is missing the doctype declaration")
	}

	// The FLOWS const must be wired in and start with a JSON object.
	idx := strings.Index(out, "const FLOWS =")
	if idx < 0 {
		t.Fatalf("rendered output is missing `const FLOWS =`")
	}
	tail := out[idx+len("const FLOWS ="):]
	tail = strings.TrimLeft(tail, " \t")
	if !strings.HasPrefix(tail, "{") {
		t.Errorf("`const FLOWS =` should be followed by a JSON object, got: %.40q", tail)
	}

	// Every node ID from the golden fixture must appear in the output as a
	// quoted JSON string literal. (Path-escaping for ids containing `/` is
	// the same in JSON and in our `</` escape because `/` alone is not
	// touched — only the two-char sequence `</`.)
	for _, n := range doc.Nodes {
		needle := "\"" + n.ID + "\""
		if !strings.Contains(out, needle) {
			t.Errorf("rendered output is missing node id %q", n.ID)
		}
	}

	// The placeholder must have been substituted exactly once and must NOT
	// appear in the rendered output.
	if strings.Contains(out, "{{.FlowsJSON}}") {
		t.Errorf("rendered output still contains the {{.FlowsJSON}} placeholder")
	}
}

// TestRender_ScriptEscape constructs a minimal FlowDoc whose description
// contains a literal `</script>`. The Render helper must escape `</` to
// `<\/` so the inline script element cannot be broken out of.
func TestRender_ScriptEscape(t *testing.T) {
	doc := &FlowDoc{
		SchemaVersion: SchemaVersion,
		Project:       "escape-probe",
		Nodes: []Node{
			{ID: "a", Label: "a", Path: "a/", Language: "go", LayoutGroup: "x", Kind: "binary"},
			{ID: "b", Label: "b", Path: "b/", Language: "go", LayoutGroup: "x", Kind: "binary"},
		},
		Flows: []Flow{{
			Slug:        "evil",
			Label:       "Evil Flow",
			Kind:        "cli-verb",
			Description: "this description contains </script><script>alert(1)</script>",
			EntryPoint:  "operator",
			Nodes:       []string{"a", "b"},
			Steps:       []Step{{From: "a", To: "b", Op: "noop"}},
		}},
	}

	var buf bytes.Buffer
	if err := Render(&buf, doc); err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := buf.String()

	// Locate the inline `<script>const FLOWS = ...;</script>` block. The raw
	// `</script>` from the description must not appear there — the escape
	// should have rewritten it to `<\/script>`.
	start := strings.Index(out, "const FLOWS =")
	if start < 0 {
		t.Fatalf("rendered output is missing `const FLOWS =`")
	}
	// The matching </script> we expect to find is the legitimate closer of
	// our inline block, which lives on its own line *after* the FLOWS const
	// expression terminates. The escaped variants from the description live
	// inside the JSON string and must NOT contain a literal `</`.
	end := strings.Index(out[start:], "</script>")
	if end < 0 {
		t.Fatalf("rendered output never closes the FLOWS script block")
	}
	scriptBody := out[start : start+end]

	if strings.Contains(scriptBody, "</script>") {
		t.Errorf("FLOWS script body contains an unescaped </script>:\n%s", scriptBody)
	}
	if strings.Contains(scriptBody, "</style>") {
		t.Errorf("FLOWS script body contains an unescaped </style>")
	}

	// Positive assertion: the description's `</script>` must survive in some
	// escaped form. Two acceptable forms exist:
	//   1. `<\/script>` — our defensive strings.Replace pass.
	//   2. `</script>` — json.Marshal's default HTML-escaping of
	//      `<`/`>` (which already neutralizes the script-break vector).
	// Either is sufficient on its own; we just need at least one present.
	hasBackslashForm := strings.Contains(scriptBody, "<\\/script>")
	hasUnicodeForm := strings.Contains(scriptBody, "\\u003c/script\\u003e")
	if !hasBackslashForm && !hasUnicodeForm {
		t.Errorf("FLOWS script body is missing any escaped form of </script>; got:\n%s", scriptBody)
	}
}

// TestRender_EmptyFlows renders a FlowDoc with no flows and asserts the
// viewer ships its empty-state copy. Also covers the nil-doc branch.
func TestRender_EmptyFlows(t *testing.T) {
	t.Run("nil-doc", func(t *testing.T) {
		var buf bytes.Buffer
		if err := Render(&buf, nil); err != nil {
			t.Fatalf("Render(nil): %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "No flows in this document.") {
			t.Errorf("nil-doc render missing empty-state copy")
		}
	})

	t.Run("empty-flows", func(t *testing.T) {
		doc := &FlowDoc{
			SchemaVersion: SchemaVersion,
			Project:       "empty",
			Nodes: []Node{
				{ID: "only", Label: "only", Path: "p/", Language: "go", LayoutGroup: "x", Kind: "binary"},
			},
			Flows: nil,
		}
		var buf bytes.Buffer
		if err := Render(&buf, doc); err != nil {
			t.Fatalf("Render: %v", err)
		}
		out := buf.String()
		if !strings.Contains(out, "No flows in this document.") {
			t.Errorf("empty-flows render missing empty-state copy")
		}
		// Sanity: doctype + node id still present.
		if !strings.Contains(out, "<!DOCTYPE html>") && !strings.Contains(out, "<!doctype html>") {
			t.Errorf("empty-flows render missing doctype")
		}
		if !strings.Contains(out, "\"only\"") {
			t.Errorf("empty-flows render missing the single node id")
		}
	})
}

// TestRender_NilWriter asserts Render rejects a nil writer rather than
// panicking. Quick cover of the input-guard branch.
func TestRender_NilWriter(t *testing.T) {
	err := Render(nil, &FlowDoc{SchemaVersion: SchemaVersion, Project: "x"})
	if err == nil {
		t.Fatalf("Render(nil writer) returned nil error")
	}
	if !strings.Contains(err.Error(), "writer is nil") {
		t.Errorf("unexpected error: %v", err)
	}
}

// errWriter always fails — used to exercise the write-error branch in Render.
type errWriter struct{ err error }

func (e *errWriter) Write(_ []byte) (int, error) { return 0, e.err }

// TestRender_WriteError ensures write failures are wrapped with the package
// prefix instead of silently dropped.
func TestRender_WriteError(t *testing.T) {
	want := errors.New("disk full")
	err := Render(&errWriter{err: want}, &FlowDoc{SchemaVersion: SchemaVersion, Project: "x"})
	if err == nil {
		t.Fatalf("Render with failing writer returned nil error")
	}
	if !errors.Is(err, want) {
		t.Errorf("Render error does not wrap underlying write error: %v", err)
	}
}

// Ensure the package's io.Writer contract still compiles cleanly under the
// test build (vet/staticcheck would flag an unused import otherwise).
var _ io.Writer = (*bytes.Buffer)(nil)
