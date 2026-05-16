// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"slices"
	"testing"
)

// mkView builds a RepoView from bare path/size pairs. SelectKeyFiles
// reads only view.Files, so the unexported accessors are left zero.
func mkView(files ...RepoFile) RepoView {
	return RepoView{Files: files}
}

func f(path string) RepoFile { return RepoFile{Path: path, Size: 100} }

func TestSelectKeyFiles_Go(t *testing.T) {
	view := mkView(
		f("go.mod"),
		f("go.sum"), // deps lockfile — not a manifest, not selected
		f("cmd/vv/main.go"),
		f("internal/flowdoc/repo.go"), // ordinary source — not selected
		f("README.md"),
	)
	got := SelectKeyFiles(view)
	want := []string{"README.md", "cmd/vv/main.go", "go.mod"}
	if !slices.Equal(got, want) {
		t.Errorf("SelectKeyFiles:\n got  %v\n want %v", got, want)
	}
}

func TestSelectKeyFiles_CCMake(t *testing.T) {
	view := mkView(
		f("CMakeLists.txt"),
		f("cmake/toolchain.cmake"), // module, not a manifest — not selected
		f("src/main.c"),
		f("src/engine.c"), // ordinary source — not selected
		f("src/CMakeLists.txt"), // nested manifest — selected
		f("README"),
	)
	got := SelectKeyFiles(view)
	want := []string{"CMakeLists.txt", "README", "src/CMakeLists.txt", "src/main.c"}
	if !slices.Equal(got, want) {
		t.Errorf("SelectKeyFiles:\n got  %v\n want %v", got, want)
	}
}

func TestSelectKeyFiles_Python(t *testing.T) {
	view := mkView(
		f("pyproject.toml"),
		f("requirements.txt"), // deps list — not a manifest, not selected
		f("pkg/__main__.py"),
		f("pkg/core.py"), // ordinary source — not selected
		f("README.rst"),  // not .md, not README-prefixed-at-root? it IS README-prefixed
	)
	got := SelectKeyFiles(view)
	want := []string{"README.rst", "pkg/__main__.py", "pyproject.toml"}
	if !slices.Equal(got, want) {
		t.Errorf("SelectKeyFiles:\n got  %v\n want %v", got, want)
	}
}

func TestSelectKeyFiles_CrossLangAndNode(t *testing.T) {
	view := mkView(
		f("Makefile"),     // cross-language manifest
		f("package.json"), // node manifest
		f("src/index.ts"), // node entry point
		f("src/util.ts"),  // ordinary source — not selected
	)
	got := SelectKeyFiles(view)
	want := []string{"Makefile", "package.json", "src/index.ts"}
	if !slices.Equal(got, want) {
		t.Errorf("SelectKeyFiles:\n got  %v\n want %v", got, want)
	}
}

func TestSelectKeyFiles_NestedDocsExcluded(t *testing.T) {
	view := mkView(
		f("README.md"),       // top-level — selected
		f("ARCHITECTURE.md"), // top-level — selected
		f("doc/DESIGN.md"),   // nested — NOT selected
		f("docs/guide.md"),   // nested — NOT selected
	)
	got := SelectKeyFiles(view)
	want := []string{"ARCHITECTURE.md", "README.md"}
	if !slices.Equal(got, want) {
		t.Errorf("SelectKeyFiles:\n got  %v\n want %v", got, want)
	}
}

func TestSelectKeyFiles_SkipsOversize(t *testing.T) {
	view := mkView(
		RepoFile{Path: "go.mod", Size: maxFileBytes + 1}, // oversize — content unreadable, skip
		RepoFile{Path: "cmd/app/main.go", Size: 200},
	)
	got := SelectKeyFiles(view)
	want := []string{"cmd/app/main.go"}
	if !slices.Equal(got, want) {
		t.Errorf("SelectKeyFiles:\n got  %v\n want %v", got, want)
	}
}

func TestSelectKeyFiles_SortedAndEmpty(t *testing.T) {
	// Empty view → empty result (not nil-vs-empty pedantry, just len 0).
	if got := SelectKeyFiles(mkView()); len(got) != 0 {
		t.Errorf("SelectKeyFiles(empty) = %v, want empty", got)
	}
	// No matches → empty result.
	noMatch := mkView(f("internal/a.go"), f("internal/b.go"), f("LICENSE"))
	if got := SelectKeyFiles(noMatch); len(got) != 0 {
		t.Errorf("SelectKeyFiles(no matches) = %v, want empty", got)
	}
	// Output is sorted regardless of input order.
	view := mkView(f("zoo.md"), f("go.mod"), f("README.md"), f("cmd/x/main.go"))
	got := SelectKeyFiles(view)
	if !slices.IsSorted(got) {
		t.Errorf("SelectKeyFiles output not sorted: %v", got)
	}
}

// TestSelectKeyFiles_OverPhase1Fixture exercises SelectKeyFiles against
// the real Phase-1 testdata tree via WalkRepo. The fixture is copied to
// a temp dir first: testdata/repo lives inside the vibe-vault checkout,
// so pointing WalkRepo at it directly would resolve `git rev-parse
// --show-toplevel` to the whole vibe-vault repo. The temp copy has no
// .git, so this also covers the WalkDir-fallback enumeration path.
func TestSelectKeyFiles_OverPhase1Fixture(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "testdata/repo", root)
	view, err := WalkRepo(root)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	got := SelectKeyFiles(view)
	// Fixture has CMakeLists.txt (manifest), src/main.c (entry point),
	// README.md (top-level doc). cmake/foo.cmake, internal/foo.go and the
	// .gitignore files are not key files; vendor/ and dist/ were already
	// filtered as noise by WalkRepo.
	want := []string{"CMakeLists.txt", "README.md", "src/main.c"}
	if !slices.Equal(got, want) {
		t.Errorf("SelectKeyFiles over fixture:\n got  %v\n want %v", got, want)
	}
}

func TestIsManifest(t *testing.T) {
	yes := []string{"go.mod", "go.work", "CMakeLists.txt", "meson.build",
		"pyproject.toml", "setup.py", "Cargo.toml", "package.json",
		"Makefile", "GNUmakefile"}
	for _, b := range yes {
		if !isManifest(b) {
			t.Errorf("isManifest(%q) = false, want true", b)
		}
	}
	no := []string{"go.sum", "requirements.txt", "main.go", "README.md",
		"foo.cmake", "config.toml"}
	for _, b := range no {
		if isManifest(b) {
			t.Errorf("isManifest(%q) = true, want false", b)
		}
	}
}

func TestIsEntryPoint(t *testing.T) {
	yes := []string{"main.go", "main.c", "main.cpp", "main.cc", "main.cxx",
		"__main__.py", "main.py", "app.py", "main.rs", "index.js", "index.ts"}
	for _, b := range yes {
		if !isEntryPoint(b) {
			t.Errorf("isEntryPoint(%q) = false, want true", b)
		}
	}
	no := []string{"helper.go", "engine.c", "lib.py", "go.mod", "README.md"}
	for _, b := range no {
		if isEntryPoint(b) {
			t.Errorf("isEntryPoint(%q) = true, want false", b)
		}
	}
}

func TestIsTopLevelDoc(t *testing.T) {
	yes := []string{"README", "README.md", "readme.md", "ARCHITECTURE.md",
		"DESIGN.md", "Readme.txt"}
	for _, p := range yes {
		if !isTopLevelDoc(p) {
			t.Errorf("isTopLevelDoc(%q) = false, want true", p)
		}
	}
	no := []string{"doc/DESIGN.md", "docs/readme.md", "internal/x.go",
		"LICENSE", "src/main.c"}
	for _, p := range no {
		if isTopLevelDoc(p) {
			t.Errorf("isTopLevelDoc(%q) = true, want false", p)
		}
	}
}
