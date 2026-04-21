// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fixture builds an isolated {home, vault, project} tempdir layout and
// returns absolute paths for each. The project directory is
// vault-tracked (has agentctx/) when tracked=true.
type fixture struct {
	root      string // parent tempdir
	home      string // fake HOME
	vault     string // fake VibeVault
	project   string // absolute path to project cwd
	name      string // project basename used for agentctx path
	slugHome  string // ~/.claude/projects/{slug}
	slugMem   string // ~/.claude/projects/{slug}/memory
	agentctx  string // vault/Projects/{name}/agentctx
	target    string // vault/Projects/{name}/agentctx/memory
	conflicts string // vault/Projects/{name}/agentctx/memory-conflicts
}

func setupFixture(t *testing.T, tracked bool) *fixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	vault := filepath.Join(root, "vault")
	project := filepath.Join(root, "work", "demo-proj")
	for _, d := range []string{home, vault, project} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	t.Setenv("HOME", home)

	name := filepath.Base(project)
	agentctx := filepath.Join(vault, "Projects", name, "agentctx")
	if tracked {
		if err := os.MkdirAll(agentctx, 0o755); err != nil {
			t.Fatalf("mkdir agentctx: %v", err)
		}
	}

	// Slug is computed from the resolved absolute path. The tempdir on
	// macOS may be under /private/var — our resolver evaluates
	// symlinks before slug computation to match Claude Code's behavior.
	resolved, err := filepath.EvalSymlinks(project)
	if err != nil {
		resolved = project
	}
	slug := slugFromPath(resolved)
	slugHome := filepath.Join(home, ".claude", "projects", slug)

	return &fixture{
		root:      root,
		home:      home,
		vault:     vault,
		project:   project,
		name:      name,
		slugHome:  slugHome,
		slugMem:   filepath.Join(slugHome, "memory"),
		agentctx:  agentctx,
		target:    filepath.Join(agentctx, "memory"),
		conflicts: filepath.Join(agentctx, "memory-conflicts"),
	}
}

func (f *fixture) linkOpts() Opts {
	return Opts{
		WorkingDir: f.project,
		VaultPath:  f.vault,
		HomeDir:    f.home,
		Now:        func() time.Time { return time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC) },
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestSlugFromPath(t *testing.T) {
	cases := map[string]string{
		"/home/johns/code/hnsw":    "-home-johns-code-hnsw",
		"/home/johns/code/hnsw/":   "-home-johns-code-hnsw",
		"/":                        "-",
		"/a":                       "-a",
		"/home/johns/code/vv-test": "-home-johns-code-vv-test",
	}
	for in, want := range cases {
		if got := slugFromPath(in); got != want {
			t.Errorf("slugFromPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLink_ScopeCheckRefusal(t *testing.T) {
	fx := setupFixture(t, false) // not vault-tracked

	_, err := Link(fx.linkOpts())
	if err == nil {
		t.Fatal("expected refusal when agentctx is missing")
	}
	if !strings.Contains(err.Error(), "vv init") {
		t.Errorf("expected hint to run `vv init`, got: %v", err)
	}
}

func TestLink_FreshMachine_NoHostState(t *testing.T) {
	fx := setupFixture(t, true)

	res, err := Link(fx.linkOpts())
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if res.AlreadyLinked {
		t.Error("expected fresh link, got AlreadyLinked=true")
	}

	info, err := os.Lstat(fx.slugMem)
	if err != nil {
		t.Fatalf("lstat symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("expected source to be a symlink")
	}
	tgt, err := os.Readlink(fx.slugMem)
	if err != nil {
		t.Fatal(err)
	}
	if tgt != fx.target {
		t.Errorf("symlink target = %s, want %s", tgt, fx.target)
	}
	if _, err := os.Stat(fx.target); err != nil {
		t.Errorf("target not created: %v", err)
	}
}

func TestLink_Idempotent(t *testing.T) {
	fx := setupFixture(t, true)

	if _, err := Link(fx.linkOpts()); err != nil {
		t.Fatalf("first link: %v", err)
	}
	res, err := Link(fx.linkOpts())
	if err != nil {
		t.Fatalf("second link: %v", err)
	}
	if !res.AlreadyLinked {
		t.Error("expected AlreadyLinked=true on second call")
	}
}

func TestLink_EmptyRealDir_IsMigrated(t *testing.T) {
	fx := setupFixture(t, true)

	if err := os.MkdirAll(fx.slugMem, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Link(fx.linkOpts()); err != nil {
		t.Fatalf("link: %v", err)
	}
	info, err := os.Lstat(fx.slugMem)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected source to be a symlink after empty-dir migration")
	}
}

func TestLink_RealDirMigration_NoConflicts(t *testing.T) {
	fx := setupFixture(t, true)

	writeFile(t, filepath.Join(fx.slugMem, "goals.md"), "project goals")
	writeFile(t, filepath.Join(fx.slugMem, "testing.md"), "testing philosophy")

	res, err := Link(fx.linkOpts())
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if res.AlreadyLinked {
		t.Error("unexpected AlreadyLinked")
	}
	for _, name := range []string{"goals.md", "testing.md"} {
		p := filepath.Join(fx.target, name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file in vault target: %s: %v", name, err)
		}
	}
	// Source should now be a symlink.
	info, err := os.Lstat(fx.slugMem)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("expected source to be a symlink")
	}
}

func TestLink_RealDirMigration_IdenticalFiles_Drop(t *testing.T) {
	fx := setupFixture(t, true)
	writeFile(t, filepath.Join(fx.slugMem, "shared.md"), "same content")
	writeFile(t, filepath.Join(fx.target, "shared.md"), "same content")

	if _, err := Link(fx.linkOpts()); err != nil {
		t.Fatalf("link: %v", err)
	}
	// Vault copy preserved.
	if got := readFile(t, filepath.Join(fx.target, "shared.md")); got != "same content" {
		t.Errorf("vault content mutated: %q", got)
	}
	// Source became a symlink (no conflict dir).
	if _, err := os.Stat(fx.conflicts); !os.IsNotExist(err) {
		t.Errorf("expected no conflict dir, got err=%v", err)
	}
}

func TestLink_RealDirMigration_DifferentContent_NoForce_Refuses(t *testing.T) {
	fx := setupFixture(t, true)
	writeFile(t, filepath.Join(fx.slugMem, "notes.md"), "host local")
	writeFile(t, filepath.Join(fx.target, "notes.md"), "vault version")

	_, err := Link(fx.linkOpts())
	if err == nil {
		t.Fatal("expected refusal")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("expected --force hint, got: %v", err)
	}
	// Nothing mutated on refusal (best-effort: vault content intact).
	if got := readFile(t, filepath.Join(fx.target, "notes.md")); got != "vault version" {
		t.Errorf("vault content should not change on refusal: %q", got)
	}
}

func TestLink_RealDirMigration_DifferentContent_Force_Quarantines(t *testing.T) {
	fx := setupFixture(t, true)
	writeFile(t, filepath.Join(fx.slugMem, "notes.md"), "host local")
	writeFile(t, filepath.Join(fx.target, "notes.md"), "vault version")

	opts := fx.linkOpts()
	opts.Force = true
	res, err := Link(opts)
	if err != nil {
		t.Fatalf("link --force: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(filepath.Dir(res.Actions[0].Path)), "") {
		// nothing, just a usage of Actions to keep the linter quiet
	}

	// Vault copy preserved.
	if got := readFile(t, filepath.Join(fx.target, "notes.md")); got != "vault version" {
		t.Errorf("vault content mutated: %q", got)
	}

	// Conflict dir MUST be sibling of target, not a child.
	entries, err := os.ReadDir(fx.conflicts)
	if err != nil {
		t.Fatalf("read conflicts: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 timestamped conflict dir, got %d", len(entries))
	}
	quarantine := filepath.Join(fx.conflicts, entries[0].Name(), "notes.md")
	if got := readFile(t, quarantine); got != "host local" {
		t.Errorf("quarantined content wrong: %q", got)
	}

	// Explicit check: conflict dir is NOT inside memory/.
	if _, err := os.Stat(filepath.Join(fx.target, "memory-conflicts")); !os.IsNotExist(err) {
		t.Errorf("conflict dir must not live inside memory/")
	}
}

func TestLink_WrongSymlink_NoForce_Refuses(t *testing.T) {
	fx := setupFixture(t, true)

	// Pre-create the parent and a wrong symlink.
	if err := os.MkdirAll(filepath.Dir(fx.slugMem), 0o755); err != nil {
		t.Fatal(err)
	}
	wrongTarget := filepath.Join(fx.root, "somewhere-else")
	if err := os.MkdirAll(wrongTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(wrongTarget, fx.slugMem); err != nil {
		t.Fatal(err)
	}

	_, err := Link(fx.linkOpts())
	if err == nil {
		t.Fatal("expected refusal on wrong-symlink without --force")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("expected --force hint, got: %v", err)
	}
}

func TestLink_WrongSymlink_Force_Repairs(t *testing.T) {
	fx := setupFixture(t, true)

	if err := os.MkdirAll(filepath.Dir(fx.slugMem), 0o755); err != nil {
		t.Fatal(err)
	}
	wrongTarget := filepath.Join(fx.root, "somewhere-else")
	if err := os.MkdirAll(wrongTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(wrongTarget, fx.slugMem); err != nil {
		t.Fatal(err)
	}

	opts := fx.linkOpts()
	opts.Force = true
	if _, err := Link(opts); err != nil {
		t.Fatalf("link --force: %v", err)
	}

	tgt, err := os.Readlink(fx.slugMem)
	if err != nil {
		t.Fatal(err)
	}
	if tgt != fx.target {
		t.Errorf("symlink after repair = %s, want %s", tgt, fx.target)
	}
}

func TestLink_BrokenSymlink_IsRepairedAsIdempotent(t *testing.T) {
	fx := setupFixture(t, true)

	// Create a symlink whose target path is the vault memory dir even
	// though it doesn't exist yet. Link must treat this as "already
	// linked" — Link will also materialize the target on its own.
	if err := os.MkdirAll(filepath.Dir(fx.slugMem), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fx.target, fx.slugMem); err != nil {
		t.Fatal(err)
	}

	res, err := Link(fx.linkOpts())
	if err != nil {
		t.Fatalf("link: %v", err)
	}
	if !res.AlreadyLinked {
		t.Error("expected AlreadyLinked on broken-link repair")
	}
	if _, err := os.Stat(fx.target); err != nil {
		t.Errorf("target must be materialized: %v", err)
	}
}

func TestLink_SymlinkedCwd_ResolvesToSingleSlug(t *testing.T) {
	fx := setupFixture(t, true)

	// Create a symlink to project and run with that as working dir.
	alias := filepath.Join(fx.root, "alias-cwd")
	if err := os.Symlink(fx.project, alias); err != nil {
		t.Fatal(err)
	}

	opts := fx.linkOpts()
	opts.WorkingDir = alias

	if _, err := Link(opts); err != nil {
		t.Fatalf("link via alias: %v", err)
	}

	if _, err := os.Lstat(fx.slugMem); err != nil {
		t.Errorf("expected canonical slug dir to exist: %v", err)
	}
}

func TestLink_TrailingSlashNormalized(t *testing.T) {
	fx := setupFixture(t, true)

	opts := fx.linkOpts()
	opts.WorkingDir = fx.project + "/"

	if _, err := Link(opts); err != nil {
		t.Fatalf("link with trailing slash: %v", err)
	}
	if _, err := os.Lstat(fx.slugMem); err != nil {
		t.Errorf("expected slug dir from normalized cwd: %v", err)
	}
}

func TestLink_DryRun_NoSideEffects(t *testing.T) {
	fx := setupFixture(t, true)

	writeFile(t, filepath.Join(fx.slugMem, "goals.md"), "project goals")

	opts := fx.linkOpts()
	opts.DryRun = true
	res, err := Link(opts)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if len(res.Actions) == 0 {
		t.Error("expected planned actions")
	}

	// Nothing changed: source still a real dir.
	info, err := os.Lstat(fx.slugMem)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("dry-run must not create symlink")
	}
	// File still at host-local.
	if _, err := os.Stat(filepath.Join(fx.slugMem, "goals.md")); err != nil {
		t.Errorf("dry-run must not move files: %v", err)
	}
	// Vault target must not materialize.
	if _, err := os.Stat(fx.target); !os.IsNotExist(err) {
		t.Errorf("dry-run must not create target dir")
	}
}

func TestLink_RejectsMissingVaultPath(t *testing.T) {
	fx := setupFixture(t, true)
	opts := fx.linkOpts()
	opts.VaultPath = ""
	if _, err := Link(opts); err == nil {
		t.Fatal("expected error on empty VaultPath")
	}
}

func TestUnlink_RoundTrip(t *testing.T) {
	fx := setupFixture(t, true)

	writeFile(t, filepath.Join(fx.slugMem, "goals.md"), "goals")
	if _, err := Link(fx.linkOpts()); err != nil {
		t.Fatal(err)
	}
	// Simulate agent writing a new memory through the symlink.
	writeFile(t, filepath.Join(fx.slugMem, "new-memory.md"), "learned X")

	if _, err := Unlink(fx.linkOpts()); err != nil {
		t.Fatalf("unlink: %v", err)
	}

	// Source now a real dir with both files.
	info, err := os.Lstat(fx.slugMem)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("expected real directory after unlink")
	}
	for _, name := range []string{"goals.md", "new-memory.md"} {
		if _, err := os.Stat(filepath.Join(fx.slugMem, name)); err != nil {
			t.Errorf("missing after unlink: %s: %v", name, err)
		}
	}
	// Vault copy preserved.
	for _, name := range []string{"goals.md", "new-memory.md"} {
		if _, err := os.Stat(filepath.Join(fx.target, name)); err != nil {
			t.Errorf("vault missing: %s: %v", name, err)
		}
	}
}

func TestUnlink_NotLinked_Refuses(t *testing.T) {
	fx := setupFixture(t, true)

	// Real directory, not a symlink.
	if err := os.MkdirAll(fx.slugMem, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Unlink(fx.linkOpts())
	if err == nil {
		t.Fatal("expected refusal when source is a real directory")
	}
	if !strings.Contains(err.Error(), "not linked") {
		t.Errorf("expected 'not linked' message, got: %v", err)
	}
}

func TestUnlink_Missing_Refuses(t *testing.T) {
	fx := setupFixture(t, true)

	_, err := Unlink(fx.linkOpts())
	if err == nil {
		t.Fatal("expected refusal when source does not exist")
	}
}

func TestUnlink_WrongTarget_NoForce_Refuses(t *testing.T) {
	fx := setupFixture(t, true)

	if err := os.MkdirAll(filepath.Dir(fx.slugMem), 0o755); err != nil {
		t.Fatal(err)
	}
	elsewhere := filepath.Join(fx.root, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, fx.slugMem); err != nil {
		t.Fatal(err)
	}

	_, err := Unlink(fx.linkOpts())
	if err == nil {
		t.Fatal("expected refusal on unrelated symlink")
	}
}

func TestUnlink_DryRun_NoSideEffects(t *testing.T) {
	fx := setupFixture(t, true)
	if _, err := Link(fx.linkOpts()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(fx.target, "in-vault.md"), "vault text")

	opts := fx.linkOpts()
	opts.DryRun = true
	if _, err := Unlink(opts); err != nil {
		t.Fatalf("dry-run unlink: %v", err)
	}

	// Still a symlink.
	info, err := os.Lstat(fx.slugMem)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("dry-run unlink must not remove symlink")
	}
}

func TestEqualPaths(t *testing.T) {
	if !equalPaths("/a/b", "/a/b/") {
		t.Error("trailing slash should be equal")
	}
	if !equalPaths("/a/./b", "/a/b") {
		t.Error("cleaned paths should be equal")
	}
	if equalPaths("/a/b", "/a/c") {
		t.Error("different paths must not be equal")
	}
}

func TestResolve_UsesCwdWhenUnset(t *testing.T) {
	fx := setupFixture(t, true)

	// Chdir to the project so resolve()'s fallback kicks in.
	orig, _ := os.Getwd()
	defer os.Chdir(orig)
	if err := os.Chdir(fx.project); err != nil {
		t.Fatal(err)
	}

	opts := fx.linkOpts()
	opts.WorkingDir = ""
	if _, err := Link(opts); err != nil {
		t.Fatalf("link via getwd: %v", err)
	}
	if _, err := os.Lstat(fx.slugMem); err != nil {
		t.Errorf("expected symlink via getwd: %v", err)
	}
}

func TestResolve_UsesHomeEnvWhenUnset(t *testing.T) {
	fx := setupFixture(t, true)

	// HomeDir left empty — the environment HOME is honored via
	// os.UserHomeDir. Our fixture already t.Setenv'd HOME.
	opts := fx.linkOpts()
	opts.HomeDir = ""
	if _, err := Link(opts); err != nil {
		t.Fatalf("link via HOME env: %v", err)
	}
	if _, err := os.Lstat(fx.slugMem); err != nil {
		t.Errorf("expected symlink under HOME env: %v", err)
	}
}

func TestMoveFile_Fallback(t *testing.T) {
	// Covers the copy+remove path. On a single filesystem os.Rename
	// will succeed — we still exercise moveFile's happy path which is
	// the common case.
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "nested", "dst.txt")
	if err := os.WriteFile(src, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source should be removed after move")
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "hello" {
		t.Errorf("dst content: err=%v got=%q", err, b)
	}
}

func TestHashFile_MissingFileErrors(t *testing.T) {
	if _, err := hashFile("/nonexistent/path/xyz"); err == nil {
		t.Error("expected error on missing file")
	}
}

func TestCopyFile_SourceMissingErrors(t *testing.T) {
	dir := t.TempDir()
	if err := copyFile("/nonexistent/source", filepath.Join(dir, "out")); err == nil {
		t.Error("expected error on missing source")
	}
}

func TestLink_NestedSubdirRefused(t *testing.T) {
	fx := setupFixture(t, true)

	if err := os.MkdirAll(filepath.Join(fx.slugMem, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Link(fx.linkOpts())
	if err == nil {
		t.Fatal("expected refusal on nested subdir in memory")
	}
	if !strings.Contains(err.Error(), "subdirectory") {
		t.Errorf("expected 'subdirectory' in error, got: %v", err)
	}
}
