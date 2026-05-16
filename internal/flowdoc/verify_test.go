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
		// C++/Rust scope qualifier: `::` is not a split point, so
		// path:Type::method parses with the full qualifier as symbol.
		{"crate/src:Type::method", refPathSymbol, "crate/src", 0, "Type::method"},
		{"src/main.rs:J1939Simulator::new", refPathSymbol, "src/main.rs", 0, "J1939Simulator::new"},
		{"src/main.cpp:Foo::Bar::baz", refPathSymbol, "src/main.cpp", 0, "Foo::Bar::baz"},
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

// TestSymbolCandidates checks the qualifier-stripping helper that lets
// `Type::method` (C++/Rust) and `Module.func` (TS/Python) refs match
// the bare-identifier per-language grammars.
func TestSymbolCandidates(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"bare", []string{"bare"}},
		{"Type::method", []string{"Type::method", "method"}},
		{"Foo::Bar::baz", []string{"Foo::Bar::baz", "baz"}},
		{"Module.func", []string{"Module.func", "func"}},
		{"a.b.c", []string{"a.b.c", "c"}},
		{"trailing::", []string{"trailing::"}}, // empty rightmost — no expansion
		{"trailing.", []string{"trailing."}},
	}
	for _, tc := range cases {
		got := symbolCandidates(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("symbolCandidates(%q) len = %d, want %d (%v)", tc.in, len(got), len(tc.want), got)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("symbolCandidates(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

// TestSymbolDeclared_TypeMethodForm asserts a `Type::method` symbol
// resolves cleanly against a Rust file declaring the method inside an
// impl block — the model commonly emits this form for Rust/C++.
func TestSymbolDeclared_TypeMethodForm(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "verify-symbols", "sample.rs"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !symbolDeclared("sample.rs", string(data), "CanMonitor::new") {
		t.Errorf("CanMonitor::new not found despite `pub fn new` inside impl CanMonitor")
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

// TestDetectLangByPath table-checks the basename / extension dispatch
// that drives the per-language declaration grammars.
func TestDetectLangByPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"foo.go", "go"},
		{"cmd/vv/main.go", "go"},
		{"src/main.c", "c-family"},
		{"src/main.h", "c-family"},
		{"src/main.cpp", "c-family"},
		{"src/main.cc", "c-family"},
		{"src/main.hpp", "c-family"},
		{"CMakeLists.txt", "cmake"},
		{"cmake/foo.cmake", "cmake"},
		{"Makefile", "make"},
		{"GNUmakefile", "make"},
		{"build/foo.mk", "make"},
		{"src/lib.rs", "rust"},
		{"app/main.py", "python"},
		{"web/index.ts", "node"},
		{"web/main.js", "node"},
		{"web/index.tsx", "node"},
		{"web/index.mjs", "node"},
		// unknown
		{"data/foo.txt", ""},
		{"data/foo.kt", ""},
		{"data/foo.scala", ""},
		{"README.md", ""},
		{"src/Main.CPP", "c-family"}, // case-insensitive ext
	}
	for _, tc := range cases {
		if got := detectLangByPath(tc.in); got != tc.want {
			t.Errorf("detectLangByPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSymbolDeclared_PerLanguage drives the per-language grammars
// against tiny on-disk fixtures under testdata/verify-symbols/. For
// each language: (a) a real declaration resolves clean, (b) a typoed
// symbol still reports missing-symbol, (c) call sites are not matched
// as declarations.
func TestSymbolDeclared_PerLanguage(t *testing.T) {
	fix := func(name string) string { return filepath.Join("testdata", "verify-symbols", name) }

	type symCase struct {
		name   string
		file   string
		symbol string
		want   bool
	}
	cases := []symCase{
		// C: function definition, struct, typedef, macro, call site
		{"c func decl", fix("sample.c"), "run_pipeline", true},
		{"c static func", fix("sample.c"), "helper_count", true},
		{"c void func", fix("sample.c"), "emit_frame", true},
		{"c struct", fix("sample.c"), "Frame", true},
		{"c typedef", fix("sample.c"), "frame_t", true},
		{"c macro", fix("sample.c"), "MAX_FRAMES", true},
		{"c missing", fix("sample.c"), "ghost_func", false},

		// C++: namespace, std::string return type, class
		{"cpp class", fix("sample.cpp"), "CanMonitor", true},
		{"cpp method-of-class declared in header-style decl", fix("sample.cpp"), "start", true},
		{"cpp std::string func", fix("sample.cpp"), "standalone_main", true},
		{"cpp void-ref func", fix("sample.cpp"), "run_pipeline", true},
		{"cpp missing", fix("sample.cpp"), "missing_pipeline", false},

		// CMake: targets, function, project
		{"cmake project", fix("CMakeLists.txt"), "recmeet", true},
		{"cmake add_executable second", fix("CMakeLists.txt"), "recmeet-daemon", true},
		{"cmake add_library", fix("CMakeLists.txt"), "recmeet-core", true},
		{"cmake function()", fix("CMakeLists.txt"), "emit_targets", true},
		{"cmake set()", fix("CMakeLists.txt"), "BUILD_FLAGS", true},
		{"cmake missing", fix("CMakeLists.txt"), "ghost-target", false},

		// Make: targets
		{"make build target", fix("Makefile"), "build", true},
		{"make clean target", fix("Makefile"), "clean", true},
		{"make missing", fix("Makefile"), "ghost", false},
		// CFLAGS is a variable assignment ("CFLAGS := ...") — must NOT
		// match as a target.
		{"make var assignment not a target", fix("Makefile"), "CFLAGS", false},

		// Rust: fn, pub fn, async fn, struct, enum, const, impl, macro_rules
		{"rust pub fn", fix("sample.rs"), "run_single_frame_mode", true},
		{"rust async fn", fix("sample.rs"), "run_replay_mode", true},
		{"rust private fn", fix("sample.rs"), "load_config", true},
		{"rust struct", fix("sample.rs"), "CanMonitor", true},
		{"rust enum", fix("sample.rs"), "FrameKind", true},
		{"rust const", fix("sample.rs"), "MAX_FRAMES", true},
		{"rust macro_rules", fix("sample.rs"), "trace_call", true},
		{"rust missing", fix("sample.rs"), "ghost_fn", false},

		// Python: def, async def, class, module-level assignment
		{"py def", fix("sample.py"), "load_config", true},
		{"py async def", fix("sample.py"), "run_replay_mode", true},
		{"py class", fix("sample.py"), "CanMonitor", true},
		{"py module const", fix("sample.py"), "MAX_FRAMES", true},
		{"py missing", fix("sample.py"), "ghost_def", false},

		// Node/TS: export function, export class, export const, interface
		{"ts export async function", fix("sample.ts"), "runReplayMode", true},
		{"ts function", fix("sample.ts"), "loadConfig", true},
		{"ts export class", fix("sample.ts"), "CanMonitor", true},
		{"ts interface", fix("sample.ts"), "FrameLike", true},
		{"ts export const", fix("sample.ts"), "MAX_FRAMES", true},
		{"ts missing", fix("sample.ts"), "ghostFn", false},

		// Unknown extension: always assume declared (don't false-positive).
		{"unknown ext accepts any symbol", fix("sample.unknown"), "AnySymbolGoesHere", true},
		{"unknown ext still accepts another", fix("sample.unknown"), "AlsoFine", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(tc.file)
			if err != nil {
				t.Fatalf("read %s: %v", tc.file, err)
			}
			got := symbolDeclared(tc.file, string(data), tc.symbol)
			if got != tc.want {
				t.Errorf("symbolDeclared(%s, %q) = %v, want %v",
					tc.file, tc.symbol, got, tc.want)
			}
		})
	}
}

// TestVerifyRefs_RealRecmeetCases reproduces the exact false-positive
// errors reported by the iter-240 flowdoc-gen measurement on recmeet
// (5 errors) and the symbol subset of cando-rs's errors. Each ref
// here was reported `missing-symbol` by the Go-only grammar; with the
// multi-language dispatch they should all resolve clean.
func TestVerifyRefs_RealRecmeetCases(t *testing.T) {
	// Fixture symbol names mirror the recmeet measurement output.
	repo := "."
	node := Node{ID: "n", Label: "n", Path: "testdata/verify-symbols", Language: "external", LayoutGroup: "g", Kind: "external"}

	refs := []string{
		"testdata/verify-symbols/Makefile:build",
		"testdata/verify-symbols/CMakeLists.txt:recmeet",
		"testdata/verify-symbols/CMakeLists.txt:recmeet-daemon",
		"testdata/verify-symbols/sample.cpp:standalone_main",
		"testdata/verify-symbols/sample.cpp:run_pipeline",
		"testdata/verify-symbols/sample.rs:run_single_frame_mode",
		"testdata/verify-symbols/sample.rs:run_replay_mode",
		"testdata/verify-symbols/sample.rs:CanMonitor",
		"testdata/verify-symbols/sample.rs:load_config",
	}

	var steps []Step
	for _, r := range refs {
		steps = append(steps, Step{From: "n", To: "n", Op: "call", Ref: r})
	}
	doc := &FlowDoc{
		SchemaVersion: SchemaVersion,
		Project:       "synthetic",
		Nodes:         []Node{node},
		Flows: []Flow{{
			Slug: "multi-language", Kind: "cli-verb", EntryPoint: "x", Steps: steps,
		}},
	}

	issues := VerifyRefs(doc, repo)
	for _, i := range issues {
		if i.IsError() {
			t.Errorf("expected zero hard errors, got: [%s] %s: %s — %s",
				i.Kind, i.Location, i.Ref, i.Detail)
		}
	}
}

// TestSymbolFoundInDir drives the non-recursive directory grep used
// when a path:Symbol ref names a package directory rather than a
// specific file. Files with unknown extensions are skipped (their
// grammar isn't known) so they cannot spuriously satisfy a lookup
// via the unknown-extension auto-accept rule.
func TestSymbolFoundInDir(t *testing.T) {
	dir := filepath.Join("testdata", "verify-symbols")

	cases := []struct {
		symbol string
		want   bool
	}{
		// Real declarations across the fixture suite.
		{"run_pipeline", true},      // both sample.c and sample.cpp
		{"CanMonitor", true},        // sample.cpp class, sample.rs struct, sample.py / sample.ts class
		{"run_single_frame_mode", true},
		{"load_config", true}, // sample.rs + sample.py
		{"build", true},       // Makefile target
		{"recmeet", true},     // CMakeLists.txt add_executable
		// Symbols that don't exist anywhere in the fixture set.
		{"ghost_symbol", false},
		{"NotDeclaredAnywhere", false},
	}
	for _, tc := range cases {
		t.Run(tc.symbol, func(t *testing.T) {
			if got := symbolFoundInDir(dir, tc.symbol); got != tc.want {
				t.Errorf("symbolFoundInDir(%q, %q) = %v, want %v",
					dir, tc.symbol, got, tc.want)
			}
		})
	}
}

// TestSymbolFoundInDir_RecursiveCrateLayout asserts the directory grep
// recurses into subdirectories matching common multi-crate / src-
// rooted layouts (Rust workspaces, C/C++ projects). The model
// commonly emits `crate:Symbol` where the actual file is at
// `crate/src/<file>` or deeper.
func TestSymbolFoundInDir_RecursiveCrateLayout(t *testing.T) {
	tmp := t.TempDir()
	// Two-level Rust-crate-like layout: <root>/<crate>/src/lib.rs.
	if err := os.MkdirAll(filepath.Join(tmp, "my-crate", "src"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "my-crate", "src", "lib.rs"),
		[]byte("pub fn deeply_nested_symbol() {}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// Noisy directory that should be pruned even though it contains a
	// matching symbol — must NOT trip the lookup of a different symbol.
	if err := os.MkdirAll(filepath.Join(tmp, "my-crate", "target"), 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "my-crate", "target", "build.rs"),
		[]byte("pub fn ghost_in_target() {}\n"), 0o644); err != nil {
		t.Fatalf("write target file: %v", err)
	}

	if !symbolFoundInDir(tmp, "deeply_nested_symbol") {
		t.Errorf("recursive grep failed to find symbol in <crate>/src/lib.rs")
	}
	if symbolFoundInDir(tmp, "ghost_in_target") {
		t.Errorf("recursive grep traversed target/ — should be pruned")
	}
	if symbolFoundInDir(tmp, "NotAnywhere") {
		t.Errorf("recursive grep returned true for a never-declared symbol")
	}
}

// TestSymbolFoundInDir_DepthCap asserts the recursive walk caps at
// dirSymbolMaxDepth — pathological deep nesting cannot stall the
// verifier.
func TestSymbolFoundInDir_DepthCap(t *testing.T) {
	tmp := t.TempDir()
	// Build a chain a/b/c/d/e/f/g (7 levels) — past dirSymbolMaxDepth.
	deep := filepath.Join(tmp, "a", "b", "c", "d", "e", "f", "g")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(deep, "f.go"),
		[]byte("package g\nfunc TooDeepToFind() {}\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if symbolFoundInDir(tmp, "TooDeepToFind") {
		t.Errorf("depth cap not enforced — symbol at depth 7 was found")
	}
}

// TestSymbolFoundInDir_NonexistentDir confirms the grep is robust to a
// non-existent path (the verifier already short-circuits via os.Stat
// before calling, but the helper itself should still degrade cleanly).
func TestSymbolFoundInDir_NonexistentDir(t *testing.T) {
	if symbolFoundInDir("internal/definitely/not/here", "Anything") {
		t.Errorf("symbolFoundInDir on missing dir = true, want false")
	}
}

// TestSymbolFoundInDir_SkipsUnknownExtensions guards against the
// unknown-extension auto-accept rule leaking into directory greps.
// A `.unknown` file with no parseable declaration must NOT satisfy a
// lookup, even though symbolDeclared("sample.unknown", _, _) returns
// true for any symbol when called directly.
func TestSymbolFoundInDir_SkipsUnknownExtensions(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "data.unknown"), []byte("nothing here"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if symbolFoundInDir(tmp, "AnySymbol") {
		t.Errorf("unknown-extension file satisfied directory grep — should be skipped")
	}
}

// TestVerifyRefs_BarePathMissingParentExistsDowngrades asserts that a
// bare-path ref to a non-existent file inside a real directory
// downgrades to weak-match. Matches the vibe-vault drift mode where
// the model emits `internal/inject/build.go` (a real package, an
// invented filename).
func TestVerifyRefs_BarePathMissingParentExistsDowngrades(t *testing.T) {
	dir := filepath.Join("testdata", "verify-symbols")
	repo := "."
	node := Node{ID: "n", Label: "n", Path: dir, Language: "external", LayoutGroup: "g", Kind: "external"}

	doc := &FlowDoc{
		SchemaVersion: SchemaVersion, Project: "p",
		Nodes: []Node{node},
		Flows: []Flow{{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
			{From: "n", To: "n", Op: "call", Ref: dir + "/imaginary.go"},
		}}},
	}
	issues := VerifyRefs(doc, repo)
	if len(issues) != 1 || issues[0].Kind != IssueWeakMatch {
		t.Fatalf("expected weak-match for bare-path with real parent, got %+v", issues)
	}

	// Parent dir also missing → still a hard error.
	doc.Flows[0].Steps[0].Ref = "no/such/dir/imaginary.go"
	issues = VerifyRefs(doc, repo)
	if len(issues) != 1 || issues[0].Kind != IssueMissingFile {
		t.Fatalf("expected missing-file for absent parent, got %+v", issues)
	}
}

// TestVerifyRefs_FileExistsSymbolInSiblingDowngrades asserts that
// path:Symbol where the file IS correct but the symbol lives in a
// sibling file in the same package downgrades to weak-match. Matches
// the recmeet/vibe-palace drift where the model emits an entry-point
// file but the helper is in another file.
func TestVerifyRefs_FileExistsSymbolInSiblingDowngrades(t *testing.T) {
	// sample.cpp's `CanMonitor` class is in sample.cpp itself, so we
	// need a symbol that exists in a SIBLING. Pick `run_single_frame_mode`
	// which lives in sample.rs but not sample.cpp.
	repo := "."
	ref := "testdata/verify-symbols/sample.cpp:run_single_frame_mode"
	node := Node{ID: "n", Label: "n", Path: "testdata/verify-symbols", Language: "external", LayoutGroup: "g", Kind: "external"}

	doc := &FlowDoc{
		SchemaVersion: SchemaVersion, Project: "p",
		Nodes: []Node{node},
		Flows: []Flow{{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
			{From: "n", To: "n", Op: "call", Ref: ref},
		}}},
	}
	issues := VerifyRefs(doc, repo)
	if len(issues) != 1 || issues[0].Kind != IssueWeakMatch {
		t.Fatalf("expected weak-match for sibling-symbol fallback, got %+v", issues)
	}

	// Symbol truly absent from the entire directory → still missing-symbol.
	doc.Flows[0].Steps[0].Ref = "testdata/verify-symbols/sample.cpp:NotAnywhereInDir"
	issues = VerifyRefs(doc, repo)
	if len(issues) != 1 || issues[0].Kind != IssueMissingSymbol {
		t.Fatalf("expected missing-symbol when no sibling has it, got %+v", issues)
	}
}

// TestVerifyRefs_WrongFileRightDirDowngrades asserts the lenient
// file-missing fallback: a ref `dir/wrong.go:Sym` whose `wrong.go`
// doesn't exist but whose `dir/` contains `Sym` in a sibling file
// downgrades from missing-file (hard error) to weak-match (warning).
// This matches the most common generator drift mode — model picks a
// plausible filename in the right package.
func TestVerifyRefs_WrongFileRightDirDowngrades(t *testing.T) {
	dir := filepath.Join("testdata", "verify-symbols")
	repo := "."
	node := Node{ID: "n", Label: "n", Path: dir, Language: "external", LayoutGroup: "g", Kind: "external"}

	wrongFileRef := dir + "/imaginary_file_does_not_exist.cpp:run_pipeline"
	doc := &FlowDoc{
		SchemaVersion: SchemaVersion, Project: "p",
		Nodes: []Node{node},
		Flows: []Flow{{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: []Step{
			{From: "n", To: "n", Op: "call", Ref: wrongFileRef},
		}}},
	}
	issues := VerifyRefs(doc, repo)
	if len(issues) != 1 {
		t.Fatalf("expected exactly 1 issue, got %d: %+v", len(issues), issues)
	}
	if issues[0].Kind != IssueWeakMatch {
		t.Errorf("want weak-match (downgraded), got %s — %s", issues[0].Kind, issues[0].Detail)
	}
	if issues[0].IsError() {
		t.Errorf("weak-match should not be a hard error")
	}

	// Sibling has no such symbol → stays a hard missing-file error.
	missingRef := dir + "/imaginary_file_does_not_exist.cpp:GhostSymbolNoSibling"
	doc.Flows[0].Steps[0].Ref = missingRef
	issues = VerifyRefs(doc, repo)
	if len(issues) != 1 || issues[0].Kind != IssueMissingFile {
		t.Fatalf("expected missing-file, got %+v", issues)
	}

	// Parent dir itself missing → still missing-file.
	doc.Flows[0].Steps[0].Ref = "no/such/dir/whatever.go:run_pipeline"
	issues = VerifyRefs(doc, repo)
	if len(issues) != 1 || issues[0].Kind != IssueMissingFile {
		t.Fatalf("expected missing-file for absent parent, got %+v", issues)
	}
}

// TestVerifyRefs_DirectoryPathSymbolResolves asserts the
// package-style refs the iter-241 measurement produced now resolve
// against real directories instead of false-positively failing with
// "path is a directory, cannot resolve symbol".
func TestVerifyRefs_DirectoryPathSymbolResolves(t *testing.T) {
	// Use the per-language fixture directory as a package-style ref
	// target. Real declarations from across the fixture set should
	// resolve clean; unknown ones should still fail.
	dir := filepath.Join("testdata", "verify-symbols")
	repo := "."
	node := Node{ID: "n", Label: "n", Path: dir, Language: "external", LayoutGroup: "g", Kind: "external"}

	mkDoc := func(refs []string) *FlowDoc {
		var steps []Step
		for _, r := range refs {
			steps = append(steps, Step{From: "n", To: "n", Op: "call", Ref: r})
		}
		return &FlowDoc{
			SchemaVersion: SchemaVersion, Project: "p",
			Nodes: []Node{node},
			Flows: []Flow{{Slug: "f", Kind: "cli-verb", EntryPoint: "x", Steps: steps}},
		}
	}

	// Resolves clean — real declarations under the directory.
	doc := mkDoc([]string{
		dir + ":run_pipeline",  // sample.c / sample.cpp
		dir + ":CanMonitor",    // sample.cpp / sample.rs / sample.py / sample.ts
		dir + ":build",         // Makefile target
		dir + ":recmeet",       // CMakeLists.txt add_executable
	})
	for _, i := range VerifyRefs(doc, repo) {
		if i.IsError() {
			t.Errorf("expected clean, got [%s] %s: %s — %s", i.Kind, i.Location, i.Ref, i.Detail)
		}
	}

	// Still fails — symbol absent from every file in the directory.
	doc = mkDoc([]string{dir + ":SymbolDoesNotExistInDir"})
	issues := VerifyRefs(doc, repo)
	if len(issues) != 1 || issues[0].Kind != IssueMissingSymbol {
		t.Fatalf("expected one missing-symbol, got %+v", issues)
	}
	if !strings.Contains(issues[0].Detail, "directory") {
		t.Errorf("detail should mention directory, got %q", issues[0].Detail)
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
