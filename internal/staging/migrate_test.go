// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package staging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suykerbuyk/vibe-vault/internal/testutil/gitx"
)

// setupVaultWithSessions constructs a fixture vault containing the legacy
// flat session layout for the named projects. Each project gets `count`
// notes whose content is "session <project>-<i>". Returns the vault path.
func setupVaultWithSessions(t *testing.T, projects map[string]int) string {
	t.Helper()
	dir := gitx.InitTestRepo(t)
	for project, count := range projects {
		sessionsDir := filepath.Join(dir, "Projects", project, "sessions")
		if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sessionsDir, err)
		}
		for i := 0; i < count; i++ {
			name := projectNoteName(project, i)
			body := []byte("session " + project + "-" + itoa(i) + "\n")
			if err := os.WriteFile(filepath.Join(sessionsDir, name), body, 0o644); err != nil {
				t.Fatalf("write note: %v", err)
			}
		}
	}
	gitx.GitRun(t, dir, "add", ".")
	gitx.GitRun(t, dir, "commit", "-m", "seed flat-layout sessions")
	return dir
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	n := i
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func projectNoteName(project string, i int) string {
	return "2026-05-03-" + project + "-" + itoa(i) + ".md"
}

// TestMigrate_PreservesContentAndHistory locks the canonical migration
// path: notes land under _pre-staging-archive/, content survives byte-
// for-byte, and `git log --follow` traces back to the pre-migration
// commit. The "follow" check is the key invariant — if `git mv` did not
// produce a rename detection, the history would be invisible to the
// operator after the migration.
func TestMigrate_PreservesContentAndHistory(t *testing.T) {
	vault := setupVaultWithSessions(t, map[string]int{"vibe-vault": 3})

	src := filepath.Join(vault, "Projects", "vibe-vault", "sessions", "2026-05-03-vibe-vault-0.md")
	want, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read pre-migration note: %v", err)
	}

	results, err := Migrate(MigrateOptions{VaultPath: vault, AllProjects: true})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].CommitSHA == "" {
		t.Errorf("commit SHA empty: %+v", results[0])
	}

	dst := filepath.Join(vault, "Projects", "vibe-vault", "sessions", MigrateArchiveDir, "2026-05-03-vibe-vault-0.md")
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read archived note: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("archived content drifted:\nwant=%q\ngot =%q", want, got)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source note still present after migrate: %v", err)
	}

	// `git log --follow` proves rename detection survived the move.
	logOut := gitx.GitRun(t, vault, "log", "--follow", "--format=%s", "--",
		"Projects/vibe-vault/sessions/_pre-staging-archive/2026-05-03-vibe-vault-0.md")
	if !strings.Contains(logOut, "seed flat-layout sessions") {
		t.Errorf("git log --follow did not reach the pre-migration commit:\n%s", logOut)
	}
}

// TestMigrate_AllProjectsProducesPerProjectCommits locks the v3 plan's
// per-project commit invariant: N commits, one per project, each
// commit subject naming its project. Single giant commit is rejected.
func TestMigrate_AllProjectsProducesPerProjectCommits(t *testing.T) {
	vault := setupVaultWithSessions(t, map[string]int{
		"alpha": 2,
		"bravo": 3,
		"gamma": 1,
	})
	preHead := strings.TrimSpace(gitx.GitRun(t, vault, "rev-parse", "HEAD"))

	results, err := Migrate(MigrateOptions{VaultPath: vault, AllProjects: true})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d, want 3", len(results))
	}

	logOut := gitx.GitRun(t, vault, "log", "--format=%s", preHead+"..HEAD")
	subjects := strings.Split(strings.TrimSpace(logOut), "\n")
	if len(subjects) != 3 {
		t.Fatalf("expected 3 commits since seed, got %d:\n%s", len(subjects), logOut)
	}

	wantNames := map[string]bool{"alpha": false, "bravo": false, "gamma": false}
	for _, s := range subjects {
		if !strings.Contains(s, "staging migrate:") {
			t.Errorf("commit subject does not name migration: %q", s)
		}
		for name := range wantNames {
			if strings.Contains(s, "project "+name) {
				wantNames[name] = true
			}
		}
	}
	for name, hit := range wantNames {
		if !hit {
			t.Errorf("no commit subject mentions project %q", name)
		}
	}
}

// TestMigrate_RewritesSessionIndex locks v3-H3: pre-migration index
// entries with old paths get rewritten to _pre-staging-archive/ paths,
// and SessionIDs are preserved. Index-less vaults are a no-op.
func TestMigrate_RewritesSessionIndex(t *testing.T) {
	vault := setupVaultWithSessions(t, map[string]int{"vibe-vault": 2})

	stateDir := filepath.Join(vault, ".vibe-vault")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	idx := map[string]map[string]any{
		"sid-A": {
			"session_id": "sid-A",
			"note_path":  "Projects/vibe-vault/sessions/2026-05-03-vibe-vault-0.md",
			"project":    "vibe-vault",
		},
		"sid-B": {
			"session_id": "sid-B",
			"note_path":  "Projects/vibe-vault/sessions/2026-05-03-vibe-vault-1.md",
			"project":    "vibe-vault",
		},
		"sid-untouched": {
			"session_id": "sid-untouched",
			"note_path":  "Projects/other/sessions/something.md",
			"project":    "other",
		},
	}
	raw, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	indexPath := filepath.Join(stateDir, "session-index.json")
	if wErr := os.WriteFile(indexPath, raw, 0o644); wErr != nil {
		t.Fatalf("write index: %v", wErr)
	}
	gitx.GitRun(t, vault, "add", ".")
	gitx.GitRun(t, vault, "commit", "-m", "seed index")

	results, err := Migrate(MigrateOptions{VaultPath: vault, Project: "vibe-vault"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got := results[0].IndexFixed; got != 2 {
		t.Errorf("IndexFixed = %d, want 2", got)
	}

	out, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read post-migration index: %v", err)
	}
	var post map[string]map[string]any
	if err := json.Unmarshal(out, &post); err != nil {
		t.Fatalf("parse post-migration index: %v", err)
	}

	cases := []struct {
		sid, want string
	}{
		{"sid-A", "Projects/vibe-vault/sessions/_pre-staging-archive/2026-05-03-vibe-vault-0.md"},
		{"sid-B", "Projects/vibe-vault/sessions/_pre-staging-archive/2026-05-03-vibe-vault-1.md"},
		{"sid-untouched", "Projects/other/sessions/something.md"},
	}
	for _, tc := range cases {
		entry, ok := post[tc.sid]
		if !ok {
			t.Errorf("session %s missing from post-index", tc.sid)
			continue
		}
		notePath, _ := entry["note_path"].(string)
		if notePath != tc.want {
			t.Errorf("session %s note_path = %q, want %q", tc.sid, notePath, tc.want)
		}
	}
}

// TestMigrate_NoFlatNotesIsNoop guards the empty-set path. A project
// whose sessions/ already lives entirely under per-host or archive
// subtrees must surface as a result with empty Moved/CommitSHA — never
// as an error.
func TestMigrate_NoFlatNotesIsNoop(t *testing.T) {
	vault := gitx.InitTestRepo(t)
	hostDir := filepath.Join(vault, "Projects", "demo", "sessions", "host-a", "2026-05-03")
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(hostDir, "x.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gitx.GitRun(t, vault, "add", ".")
	gitx.GitRun(t, vault, "commit", "-m", "per-host only")

	results, err := Migrate(MigrateOptions{VaultPath: vault, Project: "demo"})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if len(results[0].Moved) != 0 || results[0].CommitSHA != "" {
		t.Errorf("expected no-op result, got %+v", results[0])
	}
}

// TestMigrate_RejectsBadOptions asserts the input validation contract.
func TestMigrate_RejectsBadOptions(t *testing.T) {
	cases := []struct {
		name string
		opts MigrateOptions
	}{
		{"empty vault", MigrateOptions{Project: "demo"}},
		{"no project, no all", MigrateOptions{VaultPath: "/tmp/x"}},
		{"both", MigrateOptions{VaultPath: "/tmp/x", Project: "demo", AllProjects: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Migrate(tc.opts); err == nil {
				t.Errorf("Migrate(%+v) = nil error, want error", tc.opts)
			}
		})
	}
}
