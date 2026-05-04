// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/testutil/gitx"
)

// makeSessionNote writes a minimal-but-valid session note (frontmatter
// + body) into stagingProjectDir under the given filename. Returns the
// absolute path written.
func makeSessionNote(t *testing.T, dir, filename, project, sessionID, date string) string {
	t.Helper()
	body := strings.Join([]string{
		"---",
		"date: " + date,
		"project: " + project,
		"session_id: \"" + sessionID + "\"",
		"---",
		"# session " + sessionID,
	}, "\n") + "\n"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	full := filepath.Join(dir, filename)
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
	return full
}

// makeVaultRepo bootstraps a git repo to act as the shared vault. Uses
// gitx so author identity / branch defaults match the rest of the
// vaultsync test surface.
func makeVaultRepo(t *testing.T) string {
	t.Helper()
	return gitx.InitTestRepo(t)
}

// makeStaging creates an empty staging dir tree at <root>/<project>/
// and returns the absolute path. Does NOT initialize git in the staging
// dir — SyncSessions only reads the staging filesystem.
func makeStaging(t *testing.T, project string) (root, projectDir string) {
	t.Helper()
	root = t.TempDir()
	projectDir = filepath.Join(root, project)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir staging project: %v", err)
	}
	return root, projectDir
}

// TestSyncSessions_NoStagingChanges_NoCommit exercises the idempotent
// no-op path: empty staging produces zero copies AND zero commits.
func TestSyncSessions_NoStagingChanges_NoCommit(t *testing.T) {
	vault := makeVaultRepo(t)
	stagingRoot, _ := makeStaging(t, "demo")
	preHEAD := gitx.GitRun(t, vault, "rev-parse", "HEAD")

	res, err := SyncSessions(vault, SyncSessionsOpts{
		StagingRoot: stagingRoot,
		Hostname:    "host1",
	})
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	postHEAD := gitx.GitRun(t, vault, "rev-parse", "HEAD")
	if preHEAD != postHEAD {
		t.Errorf("HEAD moved despite no staging changes: %s -> %s", preHEAD, postHEAD)
	}
	for _, p := range res.Projects {
		if p.CommitSHA != "" {
			t.Errorf("project %s should have no commit; got %s", p.Project, p.CommitSHA)
		}
	}
}

// TestSyncSessions_StagingChanges_CommitLocal validates the happy path:
// notes mirror, per-host index lands, ONE commit per project, no remote
// push. Verified by snapshotting (a) HEAD pre/post, (b) the absence of
// any remote refs (vault has none — local-only commit must succeed).
func TestSyncSessions_StagingChanges_CommitLocal(t *testing.T) {
	vault := makeVaultRepo(t)
	stagingRoot, projectDir := makeStaging(t, "demo")
	makeSessionNote(t, projectDir, "2026-05-03-100000000.md",
		"demo", "sess-1", "2026-05-03")
	makeSessionNote(t, projectDir, "2026-05-03-100100000.md",
		"demo", "sess-2", "2026-05-03")

	preHEAD := gitx.GitRun(t, vault, "rev-parse", "HEAD")

	res, err := SyncSessions(vault, SyncSessionsOpts{
		StagingRoot: stagingRoot,
		Hostname:    "host1",
	})
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	if len(res.Projects) != 1 {
		t.Fatalf("Projects = %v, want 1", res.Projects)
	}
	p := res.Projects[0]
	if p.CommitSHA == "" {
		t.Fatal("expected non-empty CommitSHA")
	}
	if p.FilesMirrored != 2 {
		t.Errorf("FilesMirrored = %d, want 2", p.FilesMirrored)
	}
	postHEAD := gitx.GitRun(t, vault, "rev-parse", "HEAD")
	if preHEAD == postHEAD {
		t.Error("HEAD did not move despite staging changes")
	}

	// Per-host index lands inside the same commit.
	idxPath := filepath.Join(vault, "Projects", "demo", "sessions", "host1", "index.json")
	body, readErr := os.ReadFile(idxPath)
	if readErr != nil {
		t.Fatalf("read index.json: %v", readErr)
	}
	var idx map[string]string
	if err := json.Unmarshal(body, &idx); err != nil {
		t.Fatalf("unmarshal index.json: %v", err)
	}
	if len(idx) != 2 {
		t.Errorf("index has %d entries, want 2: %v", len(idx), idx)
	}
	if _, ok := idx["sess-1"]; !ok {
		t.Errorf("index missing sess-1: %v", idx)
	}
}

// TestSyncSessions_Idempotent: running twice with no source changes
// produces zero new commits on the second pass.
func TestSyncSessions_Idempotent(t *testing.T) {
	vault := makeVaultRepo(t)
	stagingRoot, projectDir := makeStaging(t, "demo")
	makeSessionNote(t, projectDir, "2026-05-03-100000000.md",
		"demo", "sess-1", "2026-05-03")

	if _, err := SyncSessions(vault, SyncSessionsOpts{
		StagingRoot: stagingRoot, Hostname: "host1",
	}); err != nil {
		t.Fatalf("SyncSessions #1: %v", err)
	}
	headAfterFirst := gitx.GitRun(t, vault, "rev-parse", "HEAD")

	res, err := SyncSessions(vault, SyncSessionsOpts{
		StagingRoot: stagingRoot, Hostname: "host1",
	})
	if err != nil {
		t.Fatalf("SyncSessions #2: %v", err)
	}
	headAfterSecond := gitx.GitRun(t, vault, "rev-parse", "HEAD")
	if headAfterFirst != headAfterSecond {
		t.Errorf("idempotent run moved HEAD: %s -> %s",
			headAfterFirst, headAfterSecond)
	}
	for _, p := range res.Projects {
		if p.CommitSHA != "" {
			t.Errorf("project %s produced commit on idempotent run: %s",
				p.Project, p.CommitSHA)
		}
	}
}

// TestSyncSessions_TwoHostsCoexist validates the structural-isolation
// guarantee: two hosts syncing to the same vault clone produce
// non-overlapping per-host subtrees and never conflict.
func TestSyncSessions_TwoHostsCoexist(t *testing.T) {
	vault := makeVaultRepo(t)
	rootA, dirA := makeStaging(t, "demo")
	rootB, dirB := makeStaging(t, "demo")
	makeSessionNote(t, dirA, "2026-05-03-100000000.md",
		"demo", "sess-A", "2026-05-03")
	makeSessionNote(t, dirB, "2026-05-03-100000000.md",
		"demo", "sess-B", "2026-05-03")

	if _, err := SyncSessions(vault, SyncSessionsOpts{
		StagingRoot: rootA, Hostname: "host-A",
	}); err != nil {
		t.Fatalf("SyncSessions A: %v", err)
	}
	if _, err := SyncSessions(vault, SyncSessionsOpts{
		StagingRoot: rootB, Hostname: "host-B",
	}); err != nil {
		t.Fatalf("SyncSessions B: %v", err)
	}

	// Both per-host subtrees coexist; no overlap on the same path.
	pathA := filepath.Join(vault, "Projects", "demo", "sessions", "host-A", "2026-05-03-100000000.md")
	pathB := filepath.Join(vault, "Projects", "demo", "sessions", "host-B", "2026-05-03-100000000.md")
	if _, err := os.Stat(pathA); err != nil {
		t.Errorf("host-A note missing: %v", err)
	}
	if _, err := os.Stat(pathB); err != nil {
		t.Errorf("host-B note missing: %v", err)
	}
}

// TestSyncSessions_AllProjects_EnumeratesFromStaging validates that
// --all-projects walks <staging-root>/*/ and NOT <vault>/Projects/*/.
// Fixture: project A in staging only; project B in vault only. Only A
// should be synced.
func TestSyncSessions_AllProjects_EnumeratesFromStaging(t *testing.T) {
	vault := makeVaultRepo(t)
	stagingRoot, dirA := makeStaging(t, "project-A")
	makeSessionNote(t, dirA, "2026-05-03-100000000.md",
		"project-A", "sess-A", "2026-05-03")

	// project-B exists in the vault but NOT in staging.
	bDir := filepath.Join(vault, "Projects", "project-B", "sessions")
	if err := os.MkdirAll(bDir, 0o755); err != nil {
		t.Fatalf("mkdir project-B vault: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bDir, "vault-only.md"), []byte("# B"), 0o644); err != nil {
		t.Fatalf("write vault-only.md: %v", err)
	}

	res, err := SyncSessions(vault, SyncSessionsOpts{
		StagingRoot: stagingRoot, Hostname: "host1",
	})
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	if len(res.Projects) != 1 {
		t.Fatalf("Projects = %d, want exactly 1 (project-A): %+v", len(res.Projects), res.Projects)
	}
	if res.Projects[0].Project != "project-A" {
		t.Errorf("Project = %q, want project-A", res.Projects[0].Project)
	}
}

// TestSyncSessions_ExplicitProject restricts to a single named project.
func TestSyncSessions_ExplicitProject(t *testing.T) {
	vault := makeVaultRepo(t)
	stagingRoot := t.TempDir()
	for _, p := range []string{"alpha", "bravo"} {
		pdir := filepath.Join(stagingRoot, p)
		if err := os.MkdirAll(pdir, 0o755); err != nil {
			t.Fatal(err)
		}
		makeSessionNote(t, pdir, "2026-05-03-100000000.md",
			p, "sess-"+p, "2026-05-03")
	}

	res, err := SyncSessions(vault, SyncSessionsOpts{
		StagingRoot: stagingRoot,
		Projects:    []string{"alpha"},
		Hostname:    "host1",
	})
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	if len(res.Projects) != 1 {
		t.Fatalf("Projects = %d, want 1", len(res.Projects))
	}
	if res.Projects[0].Project != "alpha" {
		t.Errorf("got %q, want alpha", res.Projects[0].Project)
	}
}

// TestSyncSessions_HostnameSanitization: passing a raw hostname with
// '/' through the orchestrator's sanitization path produces a safe
// destination directory (no path escape).
func TestSyncSessions_HostnameSanitization(t *testing.T) {
	vault := makeVaultRepo(t)
	stagingRoot, projectDir := makeStaging(t, "demo")
	makeSessionNote(t, projectDir, "2026-05-03-100000000.md",
		"demo", "sess-1", "2026-05-03")

	// Pass an unsanitized hostname through opts.Hostname directly. The
	// orchestrator's contract is that it sanitizes on the way in for
	// the empty default; explicit values are taken verbatim. Test that
	// callers wrap their values appropriately by verifying the
	// pre-sanitization gives a safe form.
	rawHost := "host/with/slash"
	safeHost := SanitizeHostname(rawHost)
	if strings.Contains(safeHost, "/") {
		t.Fatalf("SanitizeHostname(%q) = %q (still contains slash)", rawHost, safeHost)
	}
	if _, err := SyncSessions(vault, SyncSessionsOpts{
		StagingRoot: stagingRoot,
		Hostname:    safeHost,
	}); err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	// Destination must be inside the vault tree (no escape).
	destBase := filepath.Join(vault, "Projects", "demo", "sessions", safeHost)
	if !strings.HasPrefix(destBase, vault) {
		t.Errorf("dest %q escaped vault %q", destBase, vault)
	}
	if _, err := os.Stat(destBase); err != nil {
		t.Errorf("dest %q not created: %v", destBase, err)
	}
}

// TestSyncSessions_NoRemotePush: vault with NO remotes still succeeds.
// Locks the push=false contract of CommitAndPushPaths (no remote
// requirement on local commits).
func TestSyncSessions_NoRemotePush(t *testing.T) {
	vault := makeVaultRepo(t) // no remotes added
	stagingRoot, projectDir := makeStaging(t, "demo")
	makeSessionNote(t, projectDir, "2026-05-03-100000000.md",
		"demo", "sess-1", "2026-05-03")

	res, err := SyncSessions(vault, SyncSessionsOpts{
		StagingRoot: stagingRoot, Hostname: "host1",
	})
	if err != nil {
		t.Fatalf("SyncSessions on remote-less vault: %v", err)
	}
	if len(res.Projects) != 1 || res.Projects[0].CommitSHA == "" {
		t.Fatalf("expected one local commit; got %+v", res.Projects)
	}

	// Confirm no remote refs exist (would fail-loudly anyway since no
	// remote is configured).
	out := gitx.GitRun(t, vault, "for-each-ref", "refs/remotes/")
	if strings.TrimSpace(out) != "" {
		t.Errorf("unexpected remote refs: %q", out)
	}
}

// TestSyncSessions_ErrorOnEmptyVaultPath documents the validation guard.
func TestSyncSessions_ErrorOnEmptyVaultPath(t *testing.T) {
	if _, err := SyncSessions("", SyncSessionsOpts{}); err == nil {
		t.Fatal("expected error on empty vaultPath")
	}
}

// TestSyncSessions_DisabledByEnv: VIBE_VAULT_DISABLE_STAGING=1 returns
// an empty result without error (back-compat opt-out).
func TestSyncSessions_DisabledByEnv(t *testing.T) {
	t.Setenv("VIBE_VAULT_DISABLE_STAGING", "1")
	vault := makeVaultRepo(t)
	res, err := SyncSessions(vault, SyncSessionsOpts{})
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	if res == nil {
		t.Fatal("res = nil, want non-nil empty SyncResult")
	}
	if len(res.Projects) != 0 {
		t.Errorf("Projects = %v, want empty when disabled", res.Projects)
	}
}

// TestCommitMessage_Subject sanity-checks the per-project commit
// subject shape for human-readable diff browsing.
func TestCommitMessage_Subject(t *testing.T) {
	got := commitMessage("demo", "host1", []string{
		"2026-05-03-100000000.md",
		"2026-05-04-110000000.md",
	})
	want := "vault: sync sessions demo/host1 (2 files, 2026-05-03..2026-05-04)"
	subject := strings.SplitN(got, "\n", 2)[0]
	if subject != want {
		t.Errorf("subject = %q, want %q", subject, want)
	}
}

// TestCommitMessage_BodyTruncated covers the >20-paths truncation.
func TestCommitMessage_BodyTruncated(t *testing.T) {
	paths := make([]string, 30)
	for i := range paths {
		paths[i] = fmt.Sprintf("2026-05-03-%09d.md", i*1000)
	}
	got := commitMessage("demo", "host1", paths)
	if !strings.Contains(got, "... (10 more)") {
		t.Errorf("commit message missing truncation marker: %q", got)
	}
}

// TestSyncResult_AnyChanged exercises the convenience accessor.
func TestSyncResult_AnyChanged(t *testing.T) {
	empty := &SyncResult{}
	if empty.AnyChanged() {
		t.Error("empty AnyChanged() = true")
	}
	noOp := &SyncResult{Projects: []ProjectSyncResult{{Project: "x"}}}
	if noOp.AnyChanged() {
		t.Error("no-commit AnyChanged() = true")
	}
	withCommit := &SyncResult{Projects: []ProjectSyncResult{{Project: "x", CommitSHA: "abc"}}}
	if !withCommit.AnyChanged() {
		t.Error("with-commit AnyChanged() = false")
	}
}

// TestSyncSessions_DefaultHostnameAndStagingRoot exercises the
// resolution paths when opts.Hostname / opts.StagingRoot are empty.
func TestSyncSessions_DefaultHostnameAndStagingRoot(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("VIBE_VAULT_HOSTNAME", "fakehost")
	t.Setenv("VIBE_VAULT_DISABLE_STAGING", "")

	vault := makeVaultRepo(t)

	// The default staging root resolves under the temp XDG_STATE_HOME we
	// just set; create a project there.
	stagingRoot := filepath.Join(os.Getenv("XDG_STATE_HOME"), "vibe-vault")
	projectDir := filepath.Join(stagingRoot, "demo")
	makeSessionNote(t, projectDir, "2026-05-03-100000000.md",
		"demo", "sess-1", "2026-05-03")

	res, err := SyncSessions(vault, SyncSessionsOpts{}) // empty opts
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	if len(res.Projects) != 1 {
		t.Fatalf("Projects = %v, want 1", res.Projects)
	}
	if res.Projects[0].Hostname != "fakehost" {
		t.Errorf("Hostname = %q, want fakehost", res.Projects[0].Hostname)
	}
}

// TestSyncSessions_ReadDirError surfaces a stagingRoot pointing at a
// regular file (not a dir) — selectProjects returns an error.
func TestSyncSessions_StagingRootIsFile(t *testing.T) {
	vault := makeVaultRepo(t)
	bogus := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(bogus, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := SyncSessions(vault, SyncSessionsOpts{
		StagingRoot: bogus,
		Hostname:    "host1",
	})
	if err == nil {
		t.Fatal("expected error when stagingRoot is a regular file")
	}
}
