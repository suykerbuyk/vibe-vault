// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"slices"
	"strings"
	"testing"
)

// viewFromFiles writes rel->content pairs into a temp dir and returns a
// WalkRepo view over it. The temp dir has no .git, so WalkRepo takes the
// WalkDir-fallback path; that is fine here — BuildContext only needs the
// listing and a working ReadFile.
func viewFromFiles(t *testing.T, files map[string]string) RepoView {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		writeFixtureFile(t, root, rel, []byte(content))
	}
	v, err := WalkRepo(root)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	return v
}

func TestBuildContext_TreeAndContents(t *testing.T) {
	view := viewFromFiles(t, map[string]string{
		"go.mod":          "module example.com/app\n",
		"cmd/app/main.go": "package main\nfunc main() {}\n",
		"internal/x.go":   "package internal\n",
		"README.md":       "# App\n",
	})
	keyFiles := SelectKeyFiles(view) // go.mod, cmd/app/main.go, README.md

	block, stats := BuildContext(view, keyFiles, DefaultContextBudgetBytes)

	// Tree listing names every enumerated file, key file or not.
	if !strings.Contains(block, "# Project file tree") {
		t.Errorf("missing tree listing header")
	}
	for _, want := range []string{"go.mod", "cmd/app/main.go", "internal/x.go", "README.md"} {
		if !strings.Contains(block, want) {
			t.Errorf("tree listing missing %q", want)
		}
	}

	// Key-file contents are inlined under delimiters; non-key files are not.
	if !strings.Contains(block, "===== FILE: go.mod =====") {
		t.Errorf("go.mod content not inlined under a delimiter")
	}
	if !strings.Contains(block, "module example.com/app") {
		t.Errorf("go.mod body not present in context")
	}
	if strings.Contains(block, "===== FILE: internal/x.go =====") {
		t.Errorf("non-key file internal/x.go should not be inlined")
	}

	if stats.TreeFileCount != 4 {
		t.Errorf("TreeFileCount = %d, want 4", stats.TreeFileCount)
	}
	wantIncluded := []string{"README.md", "cmd/app/main.go", "go.mod"}
	got := append([]string(nil), stats.Included...)
	slices.Sort(got)
	if !slices.Equal(got, wantIncluded) {
		t.Errorf("Included = %v, want %v", got, wantIncluded)
	}
	if len(stats.Dropped) != 0 {
		t.Errorf("Dropped = %v, want none", stats.Dropped)
	}
	if stats.TotalBytes != len(block) {
		t.Errorf("TotalBytes = %d, want %d", stats.TotalBytes, len(block))
	}
}

func TestBuildContext_BudgetDropsLowPriority(t *testing.T) {
	// A small manifest + small entry point + a large doc. Budget fits the
	// two small high-priority files but not the doc.
	bigDoc := strings.Repeat("x", 4096)
	view := viewFromFiles(t, map[string]string{
		"go.mod":          "module m\n",                // manifest, class 0
		"cmd/app/main.go": "package main\nfunc main(){}", // entry point, class 1
		"README.md":       bigDoc,                       // top-level doc, class 2
	})
	keyFiles := SelectKeyFiles(view)

	block, stats := BuildContext(view, keyFiles, 1024)

	if !slices.Contains(stats.Included, "go.mod") {
		t.Errorf("manifest go.mod should be included under budget; included=%v", stats.Included)
	}
	if !slices.Contains(stats.Included, "cmd/app/main.go") {
		t.Errorf("entry point should be included under budget; included=%v", stats.Included)
	}
	if !slices.Contains(stats.Dropped, "README.md") {
		t.Errorf("oversized low-priority doc should be dropped; dropped=%v", stats.Dropped)
	}
	// The tree listing is ALWAYS present even when content is budget-capped.
	for _, want := range []string{"go.mod", "cmd/app/main.go", "README.md"} {
		if !strings.Contains(block, want) {
			t.Errorf("tree listing missing %q despite budget cap", want)
		}
	}
	if !strings.Contains(block, bigDoc) {
		// good: the dropped doc's *content* must not be inlined
	} else {
		t.Errorf("dropped doc content leaked into the context block")
	}
}

func TestBuildContext_NoBudget(t *testing.T) {
	bigDoc := strings.Repeat("y", 8192)
	view := viewFromFiles(t, map[string]string{
		"go.mod":    "module m\n",
		"README.md": bigDoc,
	})
	keyFiles := SelectKeyFiles(view)

	_, stats := BuildContext(view, keyFiles, 0) // 0 = no budget
	if len(stats.Dropped) != 0 {
		t.Errorf("no-budget run dropped files: %v", stats.Dropped)
	}
	if len(stats.Included) != 2 {
		t.Errorf("no-budget run included %d files, want 2", len(stats.Included))
	}
}

func TestBuildContext_OversizeKeyFileDropped(t *testing.T) {
	// An oversize manifest is selected by SelectKeyFiles only if under the
	// cap — but a key file passed directly that ReadFile refuses must be
	// recorded as dropped, not crash the build.
	view := viewFromFiles(t, map[string]string{
		"go.mod":          "module m\n",
		"cmd/app/main.go": "package main\nfunc main(){}",
	})
	// "ghost.go" is not in the view at all → ReadFile errors → dropped.
	block, stats := BuildContext(view, []string{"go.mod", "ghost.go"}, DefaultContextBudgetBytes)
	if !slices.Contains(stats.Included, "go.mod") {
		t.Errorf("go.mod should be included; included=%v", stats.Included)
	}
	if !slices.Contains(stats.Dropped, "ghost.go") {
		t.Errorf("unreadable key file should be dropped; dropped=%v", stats.Dropped)
	}
	if strings.Contains(block, "===== FILE: ghost.go") {
		t.Errorf("unreadable file should not appear in the block")
	}
}

func TestBuildContext_EmptySelection(t *testing.T) {
	view := viewFromFiles(t, map[string]string{"internal/a.go": "package internal\n"})
	block, stats := BuildContext(view, nil, DefaultContextBudgetBytes)
	if len(stats.Included) != 0 {
		t.Errorf("Included = %v, want none", stats.Included)
	}
	if !strings.Contains(block, "# Project file tree") {
		t.Errorf("tree listing must still be present with no key files")
	}
	if !strings.Contains(block, "no file contents available") {
		t.Errorf("expected the empty-selection note, got: %q", block)
	}
}

func TestKeyFileClass(t *testing.T) {
	cases := []struct {
		path string
		want int
	}{
		{"go.mod", 0},
		{"CMakeLists.txt", 0},
		{"Makefile", 0},
		{"cmd/vv/main.go", 1},
		{"src/main.c", 1},
		{"pkg/__main__.py", 1},
		{"README.md", 2},
		{"ARCHITECTURE.md", 2},
	}
	for _, c := range cases {
		if got := keyFileClass(c.path); got != c.want {
			t.Errorf("keyFileClass(%q) = %d, want %d", c.path, got, c.want)
		}
	}
}
