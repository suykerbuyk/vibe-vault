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
