// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/testutil/gitx"
)

// --- readLastTasksSnapshot --------------------------------------------------

func TestReadLastTasksSnapshot_Missing(t *testing.T) {
	dir := t.TempDir()
	snap, err := readLastTasksSnapshot(dir)
	if err != nil {
		t.Fatalf("missing snapshot file should not error, got: %v", err)
	}
	if snap.IterN != 0 || snap.AnchorSHA != "" || len(snap.Active) != 0 ||
		len(snap.Done) != 0 || len(snap.Cancelled) != 0 {
		t.Errorf("missing snapshot should yield zero-value, got %+v", snap)
	}
}

func TestReadLastTasksSnapshot_Present(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".vibe-vault"), 0o755); err != nil {
		t.Fatalf("mkdir .vibe-vault: %v", err)
	}
	want := lastTasksSnapshot{
		IterN:     216,
		AnchorSHA: "deadbeef",
		Active:    []string{"task-a", "task-b"},
		Done:      []string{"task-x"},
		Cancelled: []string{"task-y"},
	}
	body, mErr := json.Marshal(want)
	if mErr != nil {
		t.Fatalf("marshal fixture: %v", mErr)
	}
	if wErr := os.WriteFile(filepath.Join(dir, ".vibe-vault", "last-tasks-snapshot.json"), body, 0o644); wErr != nil {
		t.Fatalf("write fixture: %v", wErr)
	}

	got, err := readLastTasksSnapshot(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.IterN != want.IterN || got.AnchorSHA != want.AnchorSHA {
		t.Errorf("scalar fields mismatch: got %+v want %+v", got, want)
	}
	if !equalUnordered(got.Active, want.Active) ||
		!equalUnordered(got.Done, want.Done) ||
		!equalUnordered(got.Cancelled, want.Cancelled) {
		t.Errorf("slug sets mismatch: got %+v want %+v", got, want)
	}
}

func TestReadLastTasksSnapshot_EmptyFileTreatedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".vibe-vault"), 0o755); err != nil {
		t.Fatalf("mkdir .vibe-vault: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".vibe-vault", "last-tasks-snapshot.json"), nil, 0o644); err != nil {
		t.Fatalf("write empty: %v", err)
	}
	snap, err := readLastTasksSnapshot(dir)
	if err != nil {
		t.Fatalf("empty file should not error, got: %v", err)
	}
	if snap.IterN != 0 || len(snap.Active) != 0 {
		t.Errorf("empty file should yield zero-value, got %+v", snap)
	}
}

func TestReadLastTasksSnapshot_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".vibe-vault"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".vibe-vault", "last-tasks-snapshot.json"),
		[]byte("not json"), 0o644); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	_, err := readLastTasksSnapshot(dir)
	if err == nil {
		t.Fatal("malformed JSON should produce an error, got nil")
	}
}

// --- enumerateLiveTasksFS ---------------------------------------------------

func TestEnumerateLiveTasksFS_AllPartitions(t *testing.T) {
	tasksDir := t.TempDir()
	mustMkdir(t, filepath.Join(tasksDir, "done"))
	mustMkdir(t, filepath.Join(tasksDir, "cancelled"))

	mustWriteFile(t, filepath.Join(tasksDir, "active-1.md"), "x")
	mustWriteFile(t, filepath.Join(tasksDir, "active-2.md"), "x")
	mustWriteFile(t, filepath.Join(tasksDir, "done", "shipped-1.md"), "x")
	mustWriteFile(t, filepath.Join(tasksDir, "cancelled", "killed-1.md"), "x")
	// Should be ignored:
	mustWriteFile(t, filepath.Join(tasksDir, ".hidden.md"), "x")
	mustWriteFile(t, filepath.Join(tasksDir, "README.txt"), "x")
	mustMkdir(t, filepath.Join(tasksDir, "subdir"))

	active, done, cancelled, err := enumerateLiveTasksFS(tasksDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !equalUnordered(active, []string{"active-1", "active-2"}) {
		t.Errorf("active = %v, want [active-1 active-2]", active)
	}
	if !equalUnordered(done, []string{"shipped-1"}) {
		t.Errorf("done = %v, want [shipped-1]", done)
	}
	if !equalUnordered(cancelled, []string{"killed-1"}) {
		t.Errorf("cancelled = %v, want [killed-1]", cancelled)
	}
}

func TestEnumerateLiveTasksFS_MissingPartitionsAreEmpty(t *testing.T) {
	tasksDir := t.TempDir()
	mustWriteFile(t, filepath.Join(tasksDir, "active-1.md"), "x")
	// no done/, no cancelled/

	active, done, cancelled, err := enumerateLiveTasksFS(tasksDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !equalUnordered(active, []string{"active-1"}) {
		t.Errorf("active = %v, want [active-1]", active)
	}
	if len(done) != 0 {
		t.Errorf("done = %v, want empty", done)
	}
	if len(cancelled) != 0 {
		t.Errorf("cancelled = %v, want empty", cancelled)
	}
}

func TestEnumerateLiveTasksFS_MissingTasksDir(t *testing.T) {
	active, done, cancelled, err := enumerateLiveTasksFS(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(active) != 0 || len(done) != 0 || len(cancelled) != 0 {
		t.Errorf("missing tasksDir should yield empty slices, got %v %v %v", active, done, cancelled)
	}
}

// --- computeTaskDeltas -------------------------------------------------------

func TestComputeTaskDeltas_AddedRetiredCancelled(t *testing.T) {
	snap := lastTasksSnapshot{
		Active:    []string{"alpha", "beta", "gamma"},
		Done:      []string{"old-shipped"},
		Cancelled: []string{"old-killed"},
	}
	liveActive := []string{"alpha", "delta"}            // beta + gamma left active; delta is new
	liveDone := []string{"old-shipped", "beta"}         // beta retired
	liveCancelled := []string{"old-killed", "gamma"}    // gamma cancelled

	got := computeTaskDeltas(snap, liveActive, liveDone, liveCancelled)
	if !equalUnordered(got.Added, []string{"delta"}) {
		t.Errorf("added = %v, want [delta]", got.Added)
	}
	if !equalUnordered(got.Retired, []string{"beta"}) {
		t.Errorf("retired = %v, want [beta]", got.Retired)
	}
	if !equalUnordered(got.Cancelled, []string{"gamma"}) {
		t.Errorf("cancelled = %v, want [gamma]", got.Cancelled)
	}
}

func TestComputeTaskDeltas_EmptySnapshotYieldsAllAsAdded(t *testing.T) {
	snap := lastTasksSnapshot{}
	got := computeTaskDeltas(snap, []string{"a", "b", "c"}, nil, nil)
	if !equalUnordered(got.Added, []string{"a", "b", "c"}) {
		t.Errorf("first-wrap should treat all live active as added, got %v", got.Added)
	}
	if len(got.Retired) != 0 || len(got.Cancelled) != 0 {
		t.Errorf("first-wrap retired/cancelled should be empty, got %+v", got)
	}
}

func TestComputeTaskDeltas_AlreadyKnownSlugIsNotAdded(t *testing.T) {
	// Slug previously in done/ that's still in done/ is a no-op.
	snap := lastTasksSnapshot{
		Done: []string{"shipped"},
	}
	got := computeTaskDeltas(snap, nil, []string{"shipped"}, nil)
	if len(got.Added)+len(got.Retired)+len(got.Cancelled) != 0 {
		t.Errorf("steady-state done-slug should produce no deltas, got %+v", got)
	}
}

// --- oldestRootCommit --------------------------------------------------------

func TestOldestRootCommit_SingleRoot(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	// Add a second commit so HEAD is not the root.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitx.GitRun(t, dir, "add", "f.txt")
	gitx.GitRun(t, dir, "commit", "-m", "second")

	rootSHA := strings.TrimSpace(gitx.GitRun(t, dir, "rev-list", "--max-parents=0", "HEAD"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := oldestRootCommit(ctx, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != rootSHA {
		t.Errorf("oldestRootCommit = %q, want %q", got, rootSHA)
	}
}

func TestOldestRootCommit_MultiRoot(t *testing.T) {
	// gitx.InitTestRepo gives us a repo with one root commit and the
	// "main" branch set as default. We'll create a second orphan
	// branch with its own root, then merge it back into main with
	// --allow-unrelated-histories so HEAD reaches both roots.
	dir := gitx.InitTestRepo(t)

	// Capture main's root SHA (the older of the two roots since we
	// committed it first, in real wall-clock time).
	mainRoot := strings.TrimSpace(gitx.GitRun(t, dir, "rev-parse", "HEAD"))

	// Sleep briefly so the second root has a strictly later commit
	// timestamp; `git rev-list --max-parents=0 HEAD` orders by commit
	// date (newest first) so the older root must land last.
	time.Sleep(1100 * time.Millisecond)

	// Create the orphan branch with its own root commit.
	gitx.GitRun(t, dir, "checkout", "--orphan", "orphan")
	gitx.GitRun(t, dir, "rm", "-rf", ".")
	if err := os.WriteFile(filepath.Join(dir, "orphan.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write orphan file: %v", err)
	}
	gitx.GitRun(t, dir, "add", "orphan.txt")
	gitx.GitRun(t, dir, "commit", "-m", "orphan root")
	orphanRoot := strings.TrimSpace(gitx.GitRun(t, dir, "rev-parse", "HEAD"))

	// Merge the orphan branch back into main so HEAD's history
	// contains both root commits (multi-root).
	gitx.GitRun(t, dir, "checkout", "main")
	mergeCmd := exec.Command("git", "merge", "orphan", "--allow-unrelated-histories", "-m", "merge orphan")
	mergeCmd.Dir = dir
	mergeCmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
		"GIT_EDITOR=true",
	)
	if out, err := mergeCmd.CombinedOutput(); err != nil {
		t.Fatalf("git merge --allow-unrelated-histories: %s: %v", out, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := oldestRootCommit(ctx, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Sanity check: there should be two root SHAs, and the result is
	// the LAST line of `git rev-list --max-parents=0 HEAD`. With
	// orphan committed AFTER main, rev-list emits orphanRoot first,
	// mainRoot last — so the helper returns mainRoot.
	if got != mainRoot {
		t.Errorf("oldestRootCommit = %q, want main's root %q (orphan was %q)", got, mainRoot, orphanRoot)
	}
}

func TestOldestRootCommit_EmptyProjectDir(t *testing.T) {
	ctx := context.Background()
	got, err := oldestRootCommit(ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("empty projectDir should yield empty SHA, got %q", got)
	}
}

// --- commitsSinceAnchor / filesChangedSinceAnchor ---------------------------

func TestCommitsSinceAnchor_AndFilesChanged(t *testing.T) {
	dir := gitx.InitTestRepo(t)
	anchor := strings.TrimSpace(gitx.GitRun(t, dir, "rev-parse", "HEAD"))

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitx.GitRun(t, dir, "add", "a.txt")
	gitx.GitRun(t, dir, "commit", "-m", "feat: add a")

	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("y"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitx.GitRun(t, dir, "add", "b.txt")
	gitx.GitRun(t, dir, "commit", "-m", "feat: add b")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	commits, err := commitsSinceAnchor(ctx, dir, anchor)
	if err != nil {
		t.Fatalf("commitsSinceAnchor: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2: %+v", len(commits), commits)
	}
	// `git log` emits newest first, so the first entry is "feat: add b".
	if commits[0].Subject != "feat: add b" || commits[1].Subject != "feat: add a" {
		t.Errorf("unexpected subjects: %+v", commits)
	}
	if commits[0].SHA == "" || commits[1].SHA == "" {
		t.Errorf("each commit must carry a SHA: %+v", commits)
	}

	files, err := filesChangedSinceAnchor(ctx, dir, anchor)
	if err != nil {
		t.Fatalf("filesChangedSinceAnchor: %v", err)
	}
	if !equalUnordered(files, []string{"a.txt", "b.txt"}) {
		t.Errorf("files = %v, want [a.txt b.txt]", files)
	}
}

func TestCommitsSinceAnchor_EmptyAnchor(t *testing.T) {
	ctx := context.Background()
	commits, err := commitsSinceAnchor(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 0 {
		t.Errorf("empty anchor should yield no commits, got %v", commits)
	}
}

func TestFilesChangedSinceAnchor_EmptyAnchor(t *testing.T) {
	ctx := context.Background()
	files, err := filesChangedSinceAnchor(ctx, t.TempDir(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("empty anchor should yield no files, got %v", files)
	}
}

// --- testCountsFromTestingMD ------------------------------------------------

func TestTestCountsFromTestingMD_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TESTING.md")
	body := "# Testing\n\nSome prose.\n\n" +
		"**Test counts: 2291 unit + 32 integration + 0 lint = 2323 tests**\n\nMore prose.\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	u, i, l, warn := testCountsFromTestingMD(path)
	if warn != "" {
		t.Errorf("warning should be empty on happy path, got %q", warn)
	}
	if u != 2291 || i != 32 || l != 0 {
		t.Errorf("got (%d, %d, %d), want (2291, 32, 0)", u, i, l)
	}
}

func TestTestCountsFromTestingMD_MissingFile(t *testing.T) {
	u, i, l, warn := testCountsFromTestingMD(filepath.Join(t.TempDir(), "absent.md"))
	if u != 0 || i != 0 || l != 0 {
		t.Errorf("missing file should yield zeros, got (%d, %d, %d)", u, i, l)
	}
	if warn == "" {
		t.Error("missing file should yield a non-empty warning")
	}
}

func TestTestCountsFromTestingMD_HeadlineNotFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TESTING.md")
	if err := os.WriteFile(path, []byte("# Testing\n\nNo headline here.\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	u, i, l, warn := testCountsFromTestingMD(path)
	if u != 0 || i != 0 || l != 0 {
		t.Errorf("unparseable file should yield zeros, got (%d, %d, %d)", u, i, l)
	}
	if warn == "" {
		t.Error("unparseable file should yield a non-empty warning")
	}
}

func TestTestCountsFromTestingMD_EmptyPath(t *testing.T) {
	u, i, l, warn := testCountsFromTestingMD("")
	if u != 0 || i != 0 || l != 0 || warn == "" {
		t.Errorf("empty path should yield zeros + warning, got (%d, %d, %d, %q)", u, i, l, warn)
	}
}

// --- vv_collect_wrap_state integration tests --------------------------------
//
// These cases exercise the registered MCP tool against fixture repos.
// They cover the equivalent semantics of the now-deleted
// TestDescribeIterState_* tests (H2-v6 disposition: rewrite the 6
// describe-iter-state cases against the new collector tool).

// TestCollectWrapState_Basic covers a fresh project with a clean vault:
// expect iter_n >= 1, branch populated, both dirty flags false, and
// last_iter_anchor_sha empty.
func TestCollectWrapState_Basic(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)
	commitAllInRepo(t, cfg.VaultPath, "initial vault state")

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "initial commit", "")
	t.Chdir(projDir)

	tool := NewCollectWrapStateTool(cfg)
	out, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var res CollectWrapStateResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v\nresult: %s", err, out)
	}

	if res.IterN < 1 {
		t.Errorf("iter_n = %d, want >= 1", res.IterN)
	}
	if res.Branch == "" {
		t.Errorf("branch should be non-empty in a git repo")
	}
	if res.VaultHasUncommittedWrites {
		t.Errorf("clean vault should have vault_has_uncommitted_writes=false")
	}
	if res.ProjectHasUncommittedWrites {
		t.Errorf("clean project should have project_has_uncommitted_writes=false")
	}
	if res.LastIterAnchorSha != "" {
		t.Errorf("fresh project should have empty last_iter_anchor_sha; got %q", res.LastIterAnchorSha)
	}
	if res.Shape == "" {
		t.Errorf("shape should be classified, got empty string")
	}
}

// TestCollectWrapState_DirtyVault asserts vault_has_uncommitted_writes
// flips to true when the project's own subtree under
// Projects/<project>/ has an uncommitted file. Per the C4-followup fix,
// the probe is per-project: a dirty file inside the project's subtree
// must trip the flag.
func TestCollectWrapState_DirtyVault(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)
	commitAllInRepo(t, cfg.VaultPath, "initial vault state")
	// Drop a dirty file inside the project's own subtree.
	projInVault := filepath.Join(cfg.VaultPath, "Projects", "myproj")
	if err := os.MkdirAll(projInVault, 0o755); err != nil {
		t.Fatalf("mkdir project subtree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projInVault, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write dirty: %v", err)
	}

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "init", "")
	t.Chdir(projDir)

	tool := NewCollectWrapStateTool(cfg)
	out, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var res CollectWrapStateResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !res.VaultHasUncommittedWrites {
		t.Errorf("dirty file in project subtree should report vault_has_uncommitted_writes=true")
	}
}

// TestCollectWrapState_OtherProjectDirty_NoFalsePositive is the
// regression-lock for the collect-wrap-state-vault-dirty-scoping
// carried thread. A dirty file under a SIBLING project's subtree
// (Projects/other/...) must NOT trip vault_has_uncommitted_writes for
// the project we're wrapping. This is the bug iter 219 hit two-machine
// when `vv context sync` regenerated 57 OTHER projects' agentctx files
// after `make install`, none of which were under Projects/vibe-vault/.
func TestCollectWrapState_OtherProjectDirty_NoFalsePositive(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)
	commitAllInRepo(t, cfg.VaultPath, "initial vault state")
	// Dirty a sibling project's subtree, NOT the project under wrap.
	otherDir := filepath.Join(cfg.VaultPath, "Projects", "other-project")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("mkdir other project: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write dirty in other project: %v", err)
	}

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "init", "")
	t.Chdir(projDir)

	tool := NewCollectWrapStateTool(cfg)
	out, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var res CollectWrapStateResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res.VaultHasUncommittedWrites {
		t.Errorf("sibling-project dirt must NOT trip vault_has_uncommitted_writes for myproj; got true (regression!)")
	}
}

// TestCollectWrapState_VaultRootDirty_NoFalsePositive asserts that an
// uncommitted file at the vault ROOT (outside any Projects/<x>/
// subtree) does not trip the per-project probe either. Pairs with the
// sibling-project regression-lock above.
func TestCollectWrapState_VaultRootDirty_NoFalsePositive(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)
	commitAllInRepo(t, cfg.VaultPath, "initial vault state")
	// Dirty at the vault root, outside Projects/.
	if err := os.WriteFile(filepath.Join(cfg.VaultPath, "vault-root-dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write dirty: %v", err)
	}

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "init", "")
	t.Chdir(projDir)

	tool := NewCollectWrapStateTool(cfg)
	out, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var res CollectWrapStateResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res.VaultHasUncommittedWrites {
		t.Errorf("vault-root dirt outside Projects/ must NOT trip vault_has_uncommitted_writes; got true")
	}
}

// TestCollectWrapState_PriorIterAnchorFound asserts that a project with
// a stamped iter commit + an iterations.md narrative resolves the
// anchor SHA and computes iter_n = max + 1.
func TestCollectWrapState_PriorIterAnchorFound(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)
	commitAllInRepo(t, cfg.VaultPath, "initial vault state")

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "chore: initial", "")
	priorSHA := gitCommitStamp(t, projDir, 41, "feat: ship something (iter 41 stamp)")
	gitCommit(t, projDir, "docs: update notes", "")
	t.Chdir(projDir)

	iterPath := filepath.Join(cfg.VaultPath, "Projects", "myproj", "agentctx", "iterations.md")
	if err := os.MkdirAll(filepath.Dir(iterPath), 0o755); err != nil {
		t.Fatalf("mkdir iterations.md parent: %v", err)
	}
	iterContent := "# Iterations\n\n## Iteration Narratives\n\n" +
		"### Iteration 40 — earlier work (2026-04-26)\n\nbody\n\n" +
		"### Iteration 41 — ship something (2026-04-27)\n\nbody\n"
	if err := os.WriteFile(iterPath, []byte(iterContent), 0o644); err != nil {
		t.Fatalf("write iterations.md: %v", err)
	}

	tool := NewCollectWrapStateTool(cfg)
	out, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var res CollectWrapStateResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res.IterN != 42 {
		t.Errorf("iter_n = %d, want 42", res.IterN)
	}
	if res.LastIterAnchorSha != priorSHA {
		t.Errorf("last_iter_anchor_sha = %q, want %q", res.LastIterAnchorSha, priorSHA)
	}
	// iter_n_minus_one (= 41) IS in iterations.md, so the flag should
	// be true.
	if !res.IterNMinusOneAlreadyInIterationsMD {
		t.Errorf("iter_n_minus_one_already_in_iterations_md = false, want true (### Iteration 41 present)")
	}
}

// TestCollectWrapState_BranchDetection asserts the branch field reports
// the active git branch.
func TestCollectWrapState_BranchDetection(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	initGitRepo(t, cfg.VaultPath)

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "init", "")

	envs := []string{
		"HOME=" + projDir,
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=t@t",
		"PATH=" + os.Getenv("PATH"),
	}
	cb := exec.Command("git", "checkout", "-q", "-b", "feature/wibble")
	cb.Dir = projDir
	cb.Env = envs
	if out, err := cb.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %s", out)
	}
	t.Chdir(projDir)

	tool := NewCollectWrapStateTool(cfg)
	out, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var res CollectWrapStateResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res.Branch != "feature/wibble" {
		t.Errorf("branch = %q, want feature/wibble", res.Branch)
	}
}

// TestCollectWrapState_InvalidProjectName asserts the resolveProject
// validator gates path-traversal-style names.
func TestCollectWrapState_InvalidProjectName(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)

	tool := NewCollectWrapStateTool(cfg)
	if _, err := tool.Handler(json.RawMessage(`{"project":"../etc"}`)); err == nil {
		t.Fatal("want project-validation error, got nil")
	}
}

// TestCollectWrapState_NoVaultGit asserts a vault that is not a git
// repo degrades vault_has_uncommitted_writes to false (no signal
// available is treated as clean).
func TestCollectWrapState_NoVaultGit(t *testing.T) {
	cfg := writeTestVault(t, map[string]index.SessionEntry{}, nil)
	// No git init on the vault.

	projDir := t.TempDir()
	initGitRepo(t, projDir)
	gitCommit(t, projDir, "init", "")
	t.Chdir(projDir)

	tool := NewCollectWrapStateTool(cfg)
	out, err := tool.Handler(json.RawMessage(`{"project":"myproj"}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var res CollectWrapStateResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if res.VaultHasUncommittedWrites {
		t.Errorf("non-git vault should report vault_has_uncommitted_writes=false")
	}
}

// --- shared helpers ---------------------------------------------------------

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent of %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func equalUnordered(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}
