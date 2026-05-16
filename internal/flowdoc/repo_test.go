// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// mustGit runs a git command in dir, failing the test on error, and
// returns the combined output.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s failed: %v\n%s", args, dir, err, out)
	}
	return string(out)
}

// copyTree recursively copies the static fixture tree at src into dst.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copyTree %s -> %s: %v", src, dst, err)
	}
}

// writeFixtureFile writes data to dir/rel, creating parent dirs.
func writeFixtureFile(t *testing.T, dir, rel string, data []byte) {
	t.Helper()
	target := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// buildSubRepoCommit creates a one-commit throwaway git repo and returns
// its HEAD SHA. The SHA is used as the target of the main fixture's
// gitlink entry, so `git ls-files --stage` reports a genuine 160000
// entry without a .gitmodules file or an on-disk submodule checkout.
func buildSubRepoCommit(t *testing.T) string {
	t.Helper()
	sub := t.TempDir()
	writeFixtureFile(t, sub, "sub.txt", []byte("submodule content\n"))
	mustGit(t, sub, "init", "-q", "-b", "main")
	mustGit(t, sub, "config", "user.email", "test@example.com")
	mustGit(t, sub, "config", "user.name", "flowdoc-test")
	mustGit(t, sub, "config", "commit.gpgsign", "false")
	mustGit(t, sub, "add", ".")
	mustGit(t, sub, "commit", "-q", "-m", "sub")
	return strings.TrimSpace(mustGit(t, sub, "rev-parse", "HEAD"))
}

// buildGitFixture materializes the static testdata/repo tree into a
// fresh temp git checkout, then adds the dynamic pieces that cannot live
// in committed testdata: the gitignored files (their names would also be
// ignored by THIS repo's git if committed under testdata/repo/), an
// oversize file, and a 160000-mode submodule gitlink. Returns the
// checkout root.
func buildGitFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	copyTree(t, "testdata/repo", root)

	// Files the fixture's own .gitignore / src/.gitignore will ignore —
	// created on disk but never staged by `git add .`.
	writeFixtureFile(t, root, "ignored-file.txt", []byte("ignored\n"))
	writeFixtureFile(t, root, "gen-output/generated.c", []byte("// generated\n"))
	writeFixtureFile(t, root, "src/local-junk.txt", []byte("junk\n"))

	// An oversize file: enumerated in Files, but ReadFile must refuse it.
	writeFixtureFile(t, root, "bigfile.txt", bytes.Repeat([]byte("x"), maxFileBytes+1))

	mustGit(t, root, "init", "-q", "-b", "main")
	mustGit(t, root, "config", "user.email", "test@example.com")
	mustGit(t, root, "config", "user.name", "flowdoc-test")
	mustGit(t, root, "config", "commit.gpgsign", "false")
	mustGit(t, root, "add", ".")

	// Inject the submodule gitlink (160000) into the index.
	subSHA := buildSubRepoCommit(t)
	mustGit(t, root, "update-index", "--add", "--cacheinfo", "160000,"+subSHA+",external/sub")

	mustGit(t, root, "commit", "-q", "-m", "fixture")
	return root
}

// viewPaths returns the kept-file paths of v, in Files order.
func viewPaths(v RepoView) []string {
	p := make([]string, len(v.Files))
	for i, f := range v.Files {
		p[i] = f.Path
	}
	return p
}

func TestWalkRepo_GitLsFiles(t *testing.T) {
	root := buildGitFixture(t)
	v, err := WalkRepo(root)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	if v.Source != WalkSourceGit {
		t.Errorf("Source = %q, want %q", v.Source, WalkSourceGit)
	}
	got := viewPaths(v)
	want := []string{
		".gitignore",
		"CMakeLists.txt",
		"README.md",
		"bigfile.txt",
		"cmake/foo.cmake",
		"internal/foo.go",
		"src/.gitignore",
		"src/main.c",
	}
	if !slices.Equal(got, want) {
		t.Errorf("kept files:\n got  %v\n want %v", got, want)
	}
}

func TestWalkRepo_GitlinkFiltered(t *testing.T) {
	root := buildGitFixture(t)
	// Sanity: the gitlink really is in the index as a 160000 entry.
	staged := mustGit(t, root, "ls-files", "--stage")
	if !strings.Contains(staged, "160000") || !strings.Contains(staged, "external/sub") {
		t.Fatalf("fixture gitlink not staged; ls-files --stage:\n%s", staged)
	}
	v, err := WalkRepo(root)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	if slices.Contains(viewPaths(v), "external/sub") {
		t.Errorf("gitlink path external/sub leaked into view: %v", viewPaths(v))
	}
}

func TestWalkRepo_GitignoreRespected(t *testing.T) {
	root := buildGitFixture(t)
	v, err := WalkRepo(root)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	for _, p := range viewPaths(v) {
		switch p {
		case "ignored-file.txt", "gen-output/generated.c", "src/local-junk.txt":
			t.Errorf("gitignored path %q leaked into view", p)
		}
	}
}

func TestWalkRepo_CommittedNoiseDenylist(t *testing.T) {
	root := buildGitFixture(t)
	// Sanity: the noise files ARE tracked (committed), so the denylist —
	// not gitignore — is necessarily what excludes them.
	tracked := mustGit(t, root, "ls-files")
	if !strings.Contains(tracked, "vendor/cpp-httplib/httplib.cpp") {
		t.Fatalf("fixture vendor file not tracked; ls-files:\n%s", tracked)
	}
	v, err := WalkRepo(root)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	for _, p := range viewPaths(v) {
		if isNoisePath(p) {
			t.Errorf("noise path %q leaked into view", p)
		}
	}
}

func TestWalkRepo_WalkDirFallback(t *testing.T) {
	root := t.TempDir()
	copyTree(t, "testdata/repo", root)
	// Same names the git fixture's .gitignore would catch — but with no
	// .git here, gitignore semantics (class 1) must NOT apply.
	writeFixtureFile(t, root, "ignored-file.txt", []byte("ignored\n"))
	writeFixtureFile(t, root, "src/local-junk.txt", []byte("junk\n"))

	v, err := WalkRepo(root)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	if v.Source != WalkSourceWalkDir {
		t.Errorf("Source = %q, want %q", v.Source, WalkSourceWalkDir)
	}
	paths := viewPaths(v)
	// Class 3 (committed-noise) still applies on the fallback path.
	for _, p := range paths {
		if isNoisePath(p) {
			t.Errorf("noise path %q leaked into fallback view", p)
		}
	}
	// Class 1 (gitignore) does NOT apply without git.
	if !slices.Contains(paths, "ignored-file.txt") || !slices.Contains(paths, "src/local-junk.txt") {
		t.Errorf("fallback view should keep would-be-ignored files; got %v", paths)
	}
}

func TestWalkRepo_Budget(t *testing.T) {
	root := buildGitFixture(t)
	v, err := WalkRepo(root)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}
	if v.Budget.FileCount != len(v.Files) {
		t.Errorf("Budget.FileCount = %d, want %d", v.Budget.FileCount, len(v.Files))
	}
	var sum int64
	for _, f := range v.Files {
		sum += f.Size
	}
	if v.Budget.TotalBytes != sum {
		t.Errorf("Budget.TotalBytes = %d, want %d", v.Budget.TotalBytes, sum)
	}
	if want := sum / tokenBytesPerToken; v.Budget.EstimatedTokens != want {
		t.Errorf("Budget.EstimatedTokens = %d, want %d", v.Budget.EstimatedTokens, want)
	}
	// bigfile.txt alone exceeds 1 MiB, so the budget must reflect oversize
	// files (they are enumerated even though ReadFile refuses them).
	if v.Budget.TotalBytes <= maxFileBytes {
		t.Errorf("Budget.TotalBytes = %d, want > %d (oversize file counted)", v.Budget.TotalBytes, int64(maxFileBytes))
	}
}

func TestRepoView_ReadFile(t *testing.T) {
	root := buildGitFixture(t)
	v, err := WalkRepo(root)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	// Kept file: content comes back.
	data, err := v.ReadFile("src/main.c")
	if err != nil {
		t.Fatalf("ReadFile(src/main.c): %v", err)
	}
	if !strings.Contains(string(data), "int main") {
		t.Errorf("src/main.c content unexpected: %q", data)
	}

	// Filtered path (noise): rejected, not read.
	if _, err := v.ReadFile("vendor/cpp-httplib/httplib.cpp"); err == nil {
		t.Errorf("ReadFile on a filtered path should error")
	}
	// Unknown path: rejected.
	if _, err := v.ReadFile("does/not/exist.go"); err == nil {
		t.Errorf("ReadFile on an unknown path should error")
	}

	// Oversize file: enumerated in Files, but content refused.
	if !slices.Contains(viewPaths(v), "bigfile.txt") {
		t.Fatalf("bigfile.txt should be enumerated in Files")
	}
	if _, err := v.ReadFile("bigfile.txt"); err == nil {
		t.Errorf("ReadFile on an oversize file should error")
	}
}

func TestRepoView_Search(t *testing.T) {
	root := buildGitFixture(t)
	v, err := WalkRepo(root)
	if err != nil {
		t.Fatalf("WalkRepo: %v", err)
	}

	hits, err := v.Search(`func Foo`)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("Search(func Foo) = %d hits, want 1: %+v", len(hits), hits)
	}
	if hits[0].Path != "internal/foo.go" {
		t.Errorf("hit path = %q, want internal/foo.go", hits[0].Path)
	}
	if hits[0].Line != 4 {
		t.Errorf("hit line = %d, want 4", hits[0].Line)
	}

	// No match: empty result, no error.
	none, err := v.Search(`zzz_no_such_symbol_zzz`)
	if err != nil {
		t.Fatalf("Search (no match): %v", err)
	}
	if len(none) != 0 {
		t.Errorf("Search (no match) = %d hits, want 0", len(none))
	}

	// Invalid regexp: hard error.
	if _, err := v.Search(`(unclosed`); err == nil {
		t.Errorf("Search with invalid regexp should error")
	}
}

func TestWalkRepo_NonexistentRoot(t *testing.T) {
	_, err := WalkRepo(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Errorf("WalkRepo on a nonexistent root should error")
	}
}

func TestParseLsFilesEntry(t *testing.T) {
	tests := []struct {
		name     string
		entry    string
		wantMode string
		wantPath string
		wantOK   bool
	}{
		{"regular file", "100644 abc123 0\tsrc/main.c", "100644", "src/main.c", true},
		{"gitlink", "160000 def456 0\texternal/sub", "160000", "external/sub", true},
		{"path with spaces", "100644 abc 0\tdir/a b.txt", "100644", "dir/a b.txt", true},
		{"no tab", "100644 abc123 0 src/main.c", "", "", false},
		{"empty", "", "", "", false},
		{"empty path", "100644 abc 0\t", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, path, ok := parseLsFilesEntry(tt.entry)
			if mode != tt.wantMode || path != tt.wantPath || ok != tt.wantOK {
				t.Errorf("parseLsFilesEntry(%q) = (%q,%q,%v), want (%q,%q,%v)",
					tt.entry, mode, path, ok, tt.wantMode, tt.wantPath, tt.wantOK)
			}
		})
	}
}

func TestIsNoisePath(t *testing.T) {
	noise := []string{
		"vendor/x.c",
		"a/node_modules/b.js",
		"dist/out.txt",
		"build/gen.c",
		"third_party/lib/z.h",
	}
	for _, p := range noise {
		if !isNoisePath(p) {
			t.Errorf("isNoisePath(%q) = false, want true", p)
		}
	}
	clean := []string{
		"src/main.c",
		"internal/foo.go",
		"cmd/vv/main.go",
		"README.md",
		"vendored.go", // segment is "vendored.go", not "vendor"
	}
	for _, p := range clean {
		if isNoisePath(p) {
			t.Errorf("isNoisePath(%q) = true, want false", p)
		}
	}
}
