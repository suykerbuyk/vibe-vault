// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory to the first
// ancestor containing a go.mod file — the module root, which is also the
// repo root the golden fixture's relative paths are anchored against.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod walking up from test working dir")
		}
		dir = parent
	}
}

// TestVerifyRefs_GoldenClean loads the Phase 1 golden fixture and asserts
// VerifyRefs against the real repo tree produces ZERO hard errors. Weak-
// match warnings are expected and acceptable (the golden has many
// path:line refs pointing inside function bodies); we log the count.
//
// If this test reports hard errors, that is a real defect in the Phase 1
// fixture translation — it must be fixed in the fixture, not papered over
// here.
func TestVerifyRefs_GoldenClean(t *testing.T) {
	path := filepath.Join("testdata", "golden", "vibe-vault-flows.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var doc FlowDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	root := repoRoot(t)
	issues := VerifyRefs(&doc, root)

	var hardErrors []RefIssue
	weakMatches := 0
	for _, i := range issues {
		if i.IsError() {
			hardErrors = append(hardErrors, i)
		} else {
			weakMatches++
		}
	}

	t.Logf("golden fixture: %d weak-match warnings", weakMatches)

	if len(hardErrors) > 0 {
		t.Errorf("golden fixture produced %d HARD ERRORS (Phase 1 fixture-quality bug):", len(hardErrors))
		for _, e := range hardErrors {
			t.Errorf("  [%s] %s: %s — %s", e.Kind, e.Location, e.Ref, e.Detail)
		}
	}
}

// TestVerifyRefs_SyntheticDriftCases builds small in-memory FlowDocs that
// each exercise exactly one RefIssueKind, and asserts the expected kind is
// produced. Real on-disk files in testdata/flowdoc/drift-fixtures/ and a
// guaranteed-absent path are used as ref targets.
func TestVerifyRefs_SyntheticDriftCases(t *testing.T) {
	// fixtureDir is relative to this package's directory (the test cwd).
	fixture := "testdata/flowdoc/drift-fixtures/sample.go"
	repo := "." // repoRoot for synthetic cases: the package dir itself.

	// baseNode is a valid node so Validate-adjacent invariants hold; tests
	// mutate copies of the doc rather than this template.
	mkDoc := func(nodes []Node, flows []Flow) *FlowDoc {
		return &FlowDoc{
			SchemaVersion: SchemaVersion,
			Project:       "synthetic",
			Nodes:         nodes,
			Flows:         flows,
		}
	}

	goodNode := Node{ID: "good", Label: "good", Path: fixture, Language: "go", LayoutGroup: "g", Kind: "subsystem"}

	cases := []struct {
		name     string
		doc      *FlowDoc
		wantKind RefIssueKind
		wantErr  bool
	}{
		{
			name: "missing node path",
			doc: mkDoc([]Node{
				{ID: "bogus", Label: "bogus", Path: "internal/definitely/not/here.go", Language: "go", LayoutGroup: "g", Kind: "subsystem"},
			}, nil),
			wantKind: IssueMissingFile,
			wantErr:  true,
		},
		{
			name: "external node path skipped",
			doc: mkDoc([]Node{
				{ID: "ext", Label: "ext", Path: "(external)", Language: "external", LayoutGroup: "g", Kind: "external"},
			}, nil),
			wantKind: "", // no issue expected
		},
		{
			name: "dangling flow.nodes ref",
			doc: mkDoc([]Node{goodNode}, []Flow{
				{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Nodes: []string{"good", "ghost"}},
			}),
			wantKind: IssueDanglingNodeRef,
			wantErr:  true,
		},
		{
			name: "dangling step.from ref",
			doc: mkDoc([]Node{goodNode}, []Flow{
				{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
					{From: "ghost", To: "good", Op: "call"},
				}},
			}),
			wantKind: IssueDanglingNodeRef,
			wantErr:  true,
		},
		{
			name: "dangling step.to ref",
			doc: mkDoc([]Node{goodNode}, []Flow{
				{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
					{From: "good", To: "ghost", Op: "call"},
				}},
			}),
			wantKind: IssueDanglingNodeRef,
			wantErr:  true,
		},
		{
			name: "step ref missing file",
			doc: mkDoc([]Node{goodNode}, []Flow{
				{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
					{From: "good", To: "good", Op: "call", Ref: "internal/gone/x.go"},
				}},
			}),
			wantKind: IssueMissingFile,
			wantErr:  true,
		},
		{
			name: "step ref missing symbol",
			doc: mkDoc([]Node{goodNode}, []Flow{
				{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
					{From: "good", To: "good", Op: "call", Ref: fixture + ":NoSuchSymbol"},
				}},
			}),
			wantKind: IssueMissingSymbol,
			wantErr:  true,
		},
		{
			name: "step ref out of range line",
			doc: mkDoc([]Node{goodNode}, []Flow{
				{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
					{From: "good", To: "good", Op: "call", Ref: fixture + ":99999"},
				}},
			}),
			wantKind: IssueOutOfRangeLine,
			wantErr:  true,
		},
		{
			name: "step ref weak match",
			doc: mkDoc([]Node{goodNode}, []Flow{
				// Line 31 of sample.go is `return w.Name` — not a decl.
				{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
					{From: "good", To: "good", Op: "call", Ref: fixture + ":31"},
				}},
			}),
			wantKind: IssueWeakMatch,
			wantErr:  false,
		},
		{
			name: "step ref valid symbol — no issue",
			doc: mkDoc([]Node{goodNode}, []Flow{
				{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
					{From: "good", To: "good", Op: "call", Ref: fixture + ":Build"},
				}},
			}),
			wantKind: "",
		},
		{
			name: "step ref valid decl line — no issue",
			doc: mkDoc([]Node{goodNode}, []Flow{
				// Line 22 of sample.go is `func Build() *Widget {`.
				{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
					{From: "good", To: "good", Op: "call", Ref: fixture + ":22"},
				}},
			}),
			wantKind: "",
		},
		{
			name: "step ref valid method symbol — no issue",
			doc: mkDoc([]Node{goodNode}, []Flow{
				{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
					{From: "good", To: "good", Op: "call", Ref: fixture + ":Render"},
				}},
			}),
			wantKind: "",
		},
		{
			name: "step ref valid const symbol — no issue",
			doc: mkDoc([]Node{goodNode}, []Flow{
				{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
					{From: "good", To: "good", Op: "call", Ref: fixture + ":Greeting"},
				}},
			}),
			wantKind: "",
		},
		{
			name: "step ref bare path exists — no issue",
			doc: mkDoc([]Node{goodNode}, []Flow{
				{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
					{From: "good", To: "good", Op: "call", Ref: fixture},
				}},
			}),
			wantKind: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issues := VerifyRefs(tc.doc, repo)
			if tc.wantKind == "" {
				if len(issues) != 0 {
					t.Fatalf("expected no issues, got %d: %+v", len(issues), issues)
				}
				return
			}
			if len(issues) != 1 {
				t.Fatalf("expected exactly 1 issue, got %d: %+v", len(issues), issues)
			}
			got := issues[0]
			if got.Kind != tc.wantKind {
				t.Errorf("issue kind = %q, want %q (%+v)", got.Kind, tc.wantKind, got)
			}
			if got.IsError() != tc.wantErr {
				t.Errorf("issue IsError() = %v, want %v", got.IsError(), tc.wantErr)
			}
		})
	}
}

func TestParseRef(t *testing.T) {
	cases := []struct {
		in     string
		form   refForm
		path   string
		line   int
		symbol string
	}{
		{"internal/mcp/prompts.go", refBarePath, "internal/mcp/prompts.go", 0, ""},
		{"cmd/vv/main.go:1005", refPathLine, "cmd/vv/main.go", 1005, ""},
		{"internal/mcp/prompts.go:NewRestartPrompt", refPathSymbol, "internal/mcp/prompts.go", 0, "NewRestartPrompt"},
		{"cmd/vv/main.go:runCheck", refPathSymbol, "cmd/vv/main.go", 0, "runCheck"},
		{"internal/archive/", refBarePath, "internal/archive/", 0, ""},
		// trailing colon with empty suffix falls back to bare path
		{"weird/path:", refBarePath, "weird/path:", 0, ""},
		// last-colon split: only the final colon segment is the suffix
		{"a:b/c.go:42", refPathLine, "a:b/c.go", 42, ""},
		{"a:b/c.go:Sym", refPathSymbol, "a:b/c.go", 0, "Sym"},
	}
	for _, tc := range cases {
		got := parseRef(tc.in)
		if got.form != tc.form {
			t.Errorf("parseRef(%q).form = %v, want %v", tc.in, got.form, tc.form)
		}
		if got.path != tc.path {
			t.Errorf("parseRef(%q).path = %q, want %q", tc.in, got.path, tc.path)
		}
		if got.line != tc.line {
			t.Errorf("parseRef(%q).line = %d, want %d", tc.in, got.line, tc.line)
		}
		if got.symbol != tc.symbol {
			t.Errorf("parseRef(%q).symbol = %q, want %q", tc.in, got.symbol, tc.symbol)
		}
	}
}

func TestAllDigits(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"123", true},
		{"0", true},
		{"12a", false},
		{"a", false},
		{" 12", false},
	}
	for _, tc := range cases {
		if got := allDigits(tc.in); got != tc.want {
			t.Errorf("allDigits(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsParenthesizedPath(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"(external)", true},
		{"(filesystem)", true},
		{"  (external)  ", true},
		{"internal/mcp/", false},
		{"cmd/vv/main.go", false},
		{"(half", false},
		{"half)", false},
	}
	for _, tc := range cases {
		if got := isParenthesizedPath(tc.in); got != tc.want {
			t.Errorf("isParenthesizedPath(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestVerifyRefs_NilDoc(t *testing.T) {
	if issues := VerifyRefs(nil, "."); issues != nil {
		t.Errorf("VerifyRefs(nil) = %+v, want nil", issues)
	}
}

func TestVerifyRefs_DirAsLineOrSymbol(t *testing.T) {
	// A path:line / path:Symbol ref whose path resolves to a directory.
	dir := "testdata/flowdoc/drift-fixtures"
	node := Node{ID: "n", Label: "n", Path: dir, Language: "go", LayoutGroup: "g", Kind: "subsystem"}

	docLine := &FlowDoc{
		SchemaVersion: SchemaVersion, Project: "p",
		Nodes: []Node{node},
		Flows: []Flow{{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
			{From: "n", To: "n", Op: "call", Ref: dir + ":10"},
		}}},
	}
	issues := VerifyRefs(docLine, ".")
	if len(issues) != 1 || issues[0].Kind != IssueMissingFile {
		t.Errorf("dir:line — got %+v, want one missing-file issue", issues)
	}

	docSym := &FlowDoc{
		SchemaVersion: SchemaVersion, Project: "p",
		Nodes: []Node{node},
		Flows: []Flow{{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
			{From: "n", To: "n", Op: "call", Ref: dir + ":Sym"},
		}}},
	}
	issues = VerifyRefs(docSym, ".")
	if len(issues) != 1 || issues[0].Kind != IssueMissingSymbol {
		t.Errorf("dir:Symbol — got %+v, want one missing-symbol issue", issues)
	}
	if !strings.Contains(issues[0].Detail, "directory") {
		t.Errorf("dir:Symbol detail = %q, want mention of 'directory'", issues[0].Detail)
	}
}

func TestFormatRefIssues(t *testing.T) {
	if got := FormatRefIssues(nil); got != "" {
		t.Errorf("FormatRefIssues(nil) = %q, want empty", got)
	}

	issues := []RefIssue{
		{Kind: IssueWeakMatch, Location: `flow "z" step 1`, Ref: "a.go:5", Detail: "not a decl"},
		{Kind: IssueMissingFile, Location: `flow "wrap" step 3`, Ref: "internal/gone/x.go", Detail: "file does not exist"},
		{Kind: IssueDanglingNodeRef, Location: `flow "a" nodes[]`, Ref: "ghost", Detail: "not in nodes[]"},
	}
	out := FormatRefIssues(issues)
	if !strings.Contains(out, "errors (2):") {
		t.Errorf("expected 'errors (2):' header, got:\n%s", out)
	}
	if !strings.Contains(out, "warnings (1):") {
		t.Errorf("expected 'warnings (1):' header, got:\n%s", out)
	}
	if !strings.Contains(out, `[missing-file] flow "wrap" step 3: internal/gone/x.go — file does not exist`) {
		t.Errorf("missing expected error line, got:\n%s", out)
	}
	// errors must appear before warnings
	if strings.Index(out, "errors (2):") > strings.Index(out, "warnings (1):") {
		t.Errorf("errors section must precede warnings section, got:\n%s", out)
	}
}
