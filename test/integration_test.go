package test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/mcp"
	"github.com/suykerbuyk/vibe-vault/internal/testutil/gitx"
	"github.com/suykerbuyk/vibe-vault/internal/vaultfs"
	"github.com/suykerbuyk/vibe-vault/templates"
)

// vvBinary is the path to the compiled vv binary, set by TestMain.
var vvBinary string

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(0)
	}

	tmpDir, err := os.MkdirTemp("", "vv-integration-build-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	vvBinary = filepath.Join(tmpDir, "vv")
	cmd := exec.Command("go", "build", "-o", vvBinary, "./cmd/vv")
	// Test working dir is test/, so go up one level to project root
	cmd.Dir = filepath.Join("..")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build vv binary: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// --- Fixtures (loaded from testdata/) ---

// readTestdata loads a test fixture file from the testdata/ directory.
func readTestdata(t *testing.T, filename string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", filename))
	if err != nil {
		t.Fatalf("read testdata/%s: %v", filename, err)
	}
	return string(data)
}

// --- Helpers ---

func runVV(t *testing.T, env []string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command(vvBinary, args...)
	cmd.Env = env
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

func mustRunVV(t *testing.T, env []string, args ...string) string {
	t.Helper()
	stdout, stderr, err := runVV(t, env, args...)
	if err != nil {
		t.Fatalf("vv %s failed: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout, stderr)
	}
	return stdout
}

func writeFixture(t *testing.T, dir, filename, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
	return path
}

// HOME-sandbox classification (home-sandbox-audit, iter 141).
//
// Every first-party caller of os.UserHomeDir() / os.Getenv("HOME") /
// os.Getenv("USER") outside internal/config/ falls into one of three
// categories at the CALL-SITE level (not the helper level — a helper
// like plugin.ClaudePluginsDir feeds both read-only and write-path
// callers, which get classified independently):
//
//   A. Safe — pure string/path computation, no I/O on $HOME. No
//      sandboxing needed. Examples: sanitize.CompressHome (prefix
//      swap), zed.commonProjectRoot (depth-gate arithmetic),
//      meta.user (env fallback, no read).
//   B. Read-only operator-private — reads files or lstats under
//      ~/.claude/, ~/.config/, ~/.local/share/, or ~/.cache/ but
//      never writes. Sandboxing is required for test determinism
//      (output depends on operator machine state) but there is no
//      data-loss risk. Examples: check.CheckHook, check.CheckMCP,
//      check.CheckMemoryLink, hook.claudeDetected, hook.zedDetected,
//      plugin.AnyCacheInstalled / plugin.IsInstalled,
//      zed.DefaultDBPath (opened with ?mode=ro), cmd/vv.
//      defaultTranscriptDir (transcript discovery scan).
//   C. Sandbox-needed — WRITES to operator-private paths. HIGHEST
//      blast radius: any unsandboxed test reaching a category-C
//      site mutates the operator's real config. Sites:
//
//        hook.Install, hook.Uninstall,
//        hook.InstallMCP, hook.UninstallMCP,
//        hook.InstallClaudePlugin, hook.UninstallClaudePlugin
//          → write ~/.claude/settings.json
//        hook.InstallMCPZed, hook.UninstallMCPZed
//          → write ~/.config/zed/settings.json
//        plugin.InstallToCache, plugin.RegisterKnownMarketplace,
//        plugin.RegisterInstalledPlugin, plugin.Remove
//          → write ~/.claude/plugins/cache/vibe-vault-local/,
//            ~/.claude/plugins/known_marketplaces.json,
//            ~/.claude/plugins/installed_plugins.json
//        memory.Link / memory.Unlink (when opts.HomeDir=="")
//          → write ~/.claude/projects/<slug>/memory
//          [sandboxed via buildEnvWithHome in memory_link_cli]
//
// No integration subtest currently invokes any hook/plugin category-C
// entrypoint. The no_real_vault_mutation canary snapshots the
// category-C write targets (~/.claude/settings.json,
// ~/.config/zed/settings.json, ~/.claude/plugins/{cache/vibe-vault-
// local, known_marketplaces.json, installed_plugins.json}) pre/post
// and fails the run on any mutation — the regression gate for
// adding a new subtest that reaches those paths without sandboxing.
//
// When to use which env-builder:
//   - buildEnv: vault-only subtests that do not invoke any
//     ~/.claude/*, ~/.config/zed/*, or ~/.local/share/zed/* path.
//     The real $HOME is passed through for stdlib compatibility
//     (user.Current, etc.), but no category-B or C site is reached.
//   - buildEnvWithHome: any subtest that invokes a category-B read
//     (check.CheckHook, check.CheckMemoryLink, zed-transcript
//     discovery, etc.) or a category-C write (hook install,
//     memory.Link). Used today by check_resume_invariants,
//     memory_link_cli, vault_push_multi_remote.
//   - buildEnvWithHomeUser: subtests that assert on provenance-
//     stamped fields (host/user/cwd/origin_project in session
//     notes or iteration trailers). Sets VIBE_VAULT_HOSTNAME,
//     VIBE_VAULT_CWD, USER, LOGNAME to the test sentinels.
//
// expandHome() leak warning: buildEnv passes the real $HOME
// through, so any test that writes a "~/..." string into a config
// value (e.g. vault_path) resolves it against the operator's real
// HOME via config/config.go expandHome. No current test does this,
// but a regression would leak writes outside the tempdir sandbox.
// When in doubt, use buildEnvWithHome and a tempdir HOME.
//
// See doc/TESTING.md for the authoritative classification table
// and the list of sandboxed subtests.
func buildEnv(xdgConfigHome string) []string { //nolint:forbidigo // sandbox-helper: copies real HOME into env array
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"XDG_CONFIG_HOME=" + xdgConfigHome,
	}
}

func buildEnvWithHome(xdgConfigHome, home string) []string { //nolint:forbidigo // sandbox-helper
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + xdgConfigHome,
	}
}

// buildEnvWithHomeUser builds an env slice with HOME, USER, LOGNAME, and the
// VIBE_VAULT_HOSTNAME sentinel set. Callers get a deterministic provenance
// pair ("vibe-vault-test", user) so assertions against YAML frontmatter or
// iteration trailers do not depend on the operator's real hostname / login.
func buildEnvWithHomeUser(xdgConfigHome, home, user string) []string { //nolint:forbidigo // sandbox-helper
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + xdgConfigHome,
		"USER=" + user,
		"LOGNAME=" + user,
		"VIBE_VAULT_HOSTNAME=vibe-vault-test",
		"VIBE_VAULT_CWD=/vibe-vault-test-cwd",
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// findSessionNoteByID returns the absolute path to the session note
// recorded in session-index.json for the given session_id, joined onto
// vaultPath. Fails the test if the session is missing.
func findSessionNoteByID(t *testing.T, vaultPath, stateDir, sessionID string) string {
	t.Helper()
	idx := readIndex(t, stateDir)
	entry, ok := idx[sessionID]
	if !ok {
		t.Fatalf("session %q not in index", sessionID)
	}
	notePath, ok := entry["note_path"].(string)
	if !ok || notePath == "" {
		t.Fatalf("session %q has no note_path: %+v", sessionID, entry)
	}
	return filepath.Join(vaultPath, notePath)
}

// stemFromPath returns the filename without .md extension — used to
// match against the previous: "[[<stem>]]" wikilink format.
func stemFromPath(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".md")
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func readIndex(t *testing.T, stateDir string) map[string]map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stateDir, "session-index.json"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var idx map[string]map[string]any
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("unmarshal index: %v", err)
	}
	return idx
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func assertContains(t *testing.T, s, substr, msg string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("%s: expected %q to contain %q", msg, s, substr)
	}
}

func assertNotContains(t *testing.T, s, substr, msg string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("%s: expected %q to NOT contain %q", msg, s, substr)
	}
}

func runVVInDir(t *testing.T, env []string, dir string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command(vvBinary, args...)
	cmd.Env = env
	cmd.Dir = dir
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

func mustRunVVInDir(t *testing.T, env []string, dir string, args ...string) string {
	t.Helper()
	stdout, stderr, err := runVVInDir(t, env, dir, args...)
	if err != nil {
		t.Fatalf("vv %s failed: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout, stderr)
	}
	return stdout
}

func runVVWithStdin(t *testing.T, env []string, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := exec.Command(vvBinary, args...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return outBuf.String(), errBuf.String(), err
}

// --- Integration Test ---

func TestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Set up isolated directories
	vaultPath := t.TempDir()
	xdgConfigHome := t.TempDir()
	fixtureDir := t.TempDir()

	env := buildEnv(xdgConfigHome)
	stateDir := filepath.Join(vaultPath, ".vibe-vault")

	// Canary: snapshot the operator's real vault BEFORE any subtest runs.
	// The no_real_vault_mutation subtest at the end of this chain re-snapshots
	// and diffs. Any mutation to the protected paths during TestIntegration
	// means a subtest leaked writes out of its tempdir sandbox.
	preCanarySnapshot, canaryRealConfigPath, canaryRealVault := vaultCanarySnapshot(t)

	// Write fixture files
	a1Path := writeFixture(t, fixtureDir, "session-aaa-001.jsonl", readTestdata(t, "session-a1.jsonl"))
	a2Path := writeFixture(t, fixtureDir, "session-aaa-002.jsonl", readTestdata(t, "session-a2.jsonl"))
	trivialPath := writeFixture(t, fixtureDir, "session-trivial-001.jsonl", readTestdata(t, "trivial.jsonl"))
	bPath := writeFixture(t, fixtureDir, "session-bbb-001.jsonl", readTestdata(t, "session-b.jsonl"))
	narrPath := writeFixture(t, fixtureDir, "session-narr-001.jsonl", readTestdata(t, "narrative-session.jsonl"))
	frictionPath := writeFixture(t, fixtureDir, "session-friction-001.jsonl", readTestdata(t, "friction-session.jsonl"))

	// 1. init
	t.Run("init", func(t *testing.T) {
		stdout := mustRunVV(t, env, "init", vaultPath)

		// Vault structure exists
		if !dirExists(vaultPath) {
			t.Fatal("vault directory not created")
		}
		if !fileExists(filepath.Join(vaultPath, "README.md")) {
			t.Error("README.md not created")
		}
		if !fileExists(filepath.Join(vaultPath, ".obsidian", "app.json")) {
			t.Error(".obsidian/app.json not created")
		}
		if !fileExists(filepath.Join(vaultPath, "Projects", ".gitkeep")) {
			t.Error("Projects/.gitkeep not created")
		}

		// Config written
		cfgPath := filepath.Join(xdgConfigHome, "vibe-vault", "config.toml")
		if !fileExists(cfgPath) {
			t.Fatal("config.toml not created")
		}
		cfgContent := readFile(t, cfgPath)
		assertContains(t, cfgContent, "vault_path", "config content")

		// stdout
		assertContains(t, stdout, "Created new vault", "init stdout")
		assertContains(t, stdout, "Config written to", "init config created message")

		// Re-init with a different path updates config
		t.Run("reinit_updates_vault_path", func(t *testing.T) {
			altVault := t.TempDir()

			reStdout := mustRunVV(t, env, "init", altVault)
			assertContains(t, reStdout, "Config updated", "reinit stdout")

			cfgContent2 := readFile(t, cfgPath)
			assertContains(t, cfgContent2, altVault, "config points to new vault")
			assertNotContains(t, cfgContent2, vaultPath, "config no longer points to old vault")

			// Restore config to point back to the original vaultPath.
			// Can't use `vv init` because scaffold correctly refuses to
			// re-scaffold an existing vault directory — update config directly.
			restored := strings.Replace(cfgContent2, altVault, vaultPath, 1)
			if err := os.WriteFile(cfgPath, []byte(restored), 0o644); err != nil {
				t.Fatalf("restore config: %v", err)
			}
		})
	})

	// 2. process session A1
	t.Run("process_session_a1", func(t *testing.T) {
		stdout := mustRunVV(t, env, "process", a1Path)

		assertContains(t, stdout, "created:", "process stdout")

		// Note file exists (Phase 4 timestamp filename — find by index session_id).
		notePath := findSessionNoteByID(t, vaultPath, stateDir, "session-aaa-001")

		note := readFile(t, notePath)

		// Frontmatter checks
		assertContains(t, note, "date: 2027-06-15", "frontmatter date")
		assertContains(t, note, "project: myproject", "frontmatter project")
		assertContains(t, note, "session_id: \"session-aaa-001\"", "frontmatter session_id")
		assertContains(t, note, "iteration: 1", "frontmatter iteration")
		assertContains(t, note, "branch: feat/login", "frontmatter branch")
		assertContains(t, note, "status: completed", "frontmatter status")
		assertContains(t, note, "tokens_in:", "frontmatter tokens")

		// Title
		assertContains(t, note, "Implement the login page with OAuth support", "note title")

		// Index entry
		idx := readIndex(t, stateDir)
		entry, ok := idx["session-aaa-001"]
		if !ok {
			t.Fatal("session-aaa-001 not in index")
		}
		if entry["project"] != "myproject" {
			t.Errorf("index project: got %v, want myproject", entry["project"])
		}
		if entry["iteration"].(float64) != 1 {
			t.Errorf("index iteration: got %v, want 1", entry["iteration"])
		}
	})

	// 3. process session A2 (same day, iteration 2 + previous link)
	t.Run("process_session_a2_iteration", func(t *testing.T) {
		stdout := mustRunVV(t, env, "process", a2Path)
		assertContains(t, stdout, "created:", "process stdout")

		// Phase 4: timestamp filename — find by index session_id, since
		// findSessionNote-by-prefix would now match two notes.
		notePath := findSessionNoteByID(t, vaultPath, stateDir, "session-aaa-002")

		note := readFile(t, notePath)

		assertContains(t, note, "iteration: 2", "frontmatter iteration")
		// Previous wikilink uses the actual filename stem of session A1.
		a1Note := findSessionNoteByID(t, vaultPath, stateDir, "session-aaa-001")
		a1Stem := stemFromPath(a1Note)
		assertContains(t, note, "previous: \"[["+a1Stem+"]]\"", "previous link")

		// Index has both sessions
		idx := readIndex(t, stateDir)
		if _, ok := idx["session-aaa-001"]; !ok {
			t.Error("session-aaa-001 missing from index")
		}
		if _, ok := idx["session-aaa-002"]; !ok {
			t.Error("session-aaa-002 missing from index")
		}
	})

	// 4. process trivial (skipped)
	t.Run("process_trivial_skipped", func(t *testing.T) {
		stdout := mustRunVV(t, env, "process", trivialPath)
		assertContains(t, stdout, "skipped", "trivial stdout")

		// Should NOT be in index
		idx := readIndex(t, stateDir)
		if _, ok := idx["session-trivial-001"]; ok {
			t.Error("trivial session should not be in index")
		}
	})

	// 5. process session B (different project)
	t.Run("process_session_b_different_project", func(t *testing.T) {
		stdout := mustRunVV(t, env, "process", bPath)
		assertContains(t, stdout, "created:", "process stdout")

		notePath := findSessionNoteByID(t, vaultPath, stateDir, "session-bbb-001")

		note := readFile(t, notePath)

		assertContains(t, note, "project: other-project", "frontmatter project")
		assertContains(t, note, "iteration: 1", "frontmatter iteration")
		assertNotContains(t, note, "previous:", "no previous link for first in project")

		// Index has 3 entries
		idx := readIndex(t, stateDir)
		if len(idx) != 3 {
			t.Errorf("index entries: got %d, want 3", len(idx))
		}
	})

	// 6. process narrative session (with tool calls + results)
	t.Run("process_narrative_session", func(t *testing.T) {
		stdout := mustRunVV(t, env, "process", narrPath)
		assertContains(t, stdout, "created:", "process stdout")

		// Find by index session_id — the narrative testdata uses
		// session-narr-001 per the fixture.
		notePath := findSessionNoteByID(t, vaultPath, stateDir, "session-narr-001")

		note := readFile(t, notePath)

		// Session Dialogue section (prose extraction)
		assertContains(t, note, "## Session Dialogue", "session dialogue section")

		// User turns preserved in prose
		assertContains(t, note, "Add JWT authentication to the API", "user request in prose")
		assertContains(t, note, "commit the changes", "second user request in prose")

		// Assistant turns preserved in prose
		assertContains(t, note, "JWT authentication has been added and committed", "assistant conclusion in prose")

		// Tool markers in prose
		assertContains(t, note, "Created `internal/auth/handler.go`", "file create marker")
		assertContains(t, note, "Ran tests", "test marker")
		assertContains(t, note, "feat: add JWT authentication", "commit marker")

		// Work Performed section still present (complementary)
		assertContains(t, note, "## Work Performed", "work performed section")

		// Tag inferred
		assertContains(t, note, "implementation", "inferred tag")

		// Better title
		assertContains(t, note, "Add JWT authentication to the API", "narrative title")

		// Narrative summary has file info
		assertContains(t, note, "Created", "summary mentions creation")

		// Commits section
		assertContains(t, note, "## Commits", "commits section")
		assertContains(t, note, "- `abc1234`", "commit SHA in body")
		assertContains(t, note, "commits: [abc1234]", "commits frontmatter")

		// Index entry
		idx := readIndex(t, stateDir)
		narrEntry, ok := idx["session-narr-001"]
		if !ok {
			t.Error("session-narr-001 not in index")
		}
		if commits, ok := narrEntry["commits"].([]any); ok {
			if len(commits) != 1 || commits[0] != "abc1234" {
				t.Errorf("index commits = %v, want [abc1234]", commits)
			}
		} else {
			t.Error("index entry missing commits field")
		}
	})

	// 7. index rebuild
	t.Run("index_rebuild", func(t *testing.T) {
		stdout := mustRunVV(t, env, "index")
		assertContains(t, stdout, "indexed 4 sessions", "index stdout")

		// Rebuilt index still has all 4
		idx := readIndex(t, stateDir)
		if len(idx) != 4 {
			t.Errorf("rebuilt index entries: got %d, want 4", len(idx))
		}
		for _, sid := range []string{"session-aaa-001", "session-aaa-002", "session-bbb-001", "session-narr-001"} {
			if _, ok := idx[sid]; !ok {
				t.Errorf("rebuilt index missing %s", sid)
			}
		}

		// Commits survive rebuild
		narrEntry, ok := idx["session-narr-001"]
		if ok {
			if commits, ok := narrEntry["commits"].([]any); ok {
				if len(commits) != 1 || commits[0] != "abc1234" {
					t.Errorf("rebuilt index commits = %v, want [abc1234]", commits)
				}
			} else {
				t.Error("rebuilt index missing commits for session-narr-001")
			}
		}

		// history.md generated per project
		for _, project := range []string{"myproject", "other-project", "narr-project"} {
			ctxPath := filepath.Join(vaultPath, "Projects", project, "history.md")
			if !fileExists(ctxPath) {
				t.Errorf("history.md not created for %s", project)
				continue
			}
			ctx := readFile(t, ctxPath)
			assertContains(t, ctx, "type: project-context", fmt.Sprintf("%s context type", project))
			assertContains(t, ctx, "## Session Timeline", fmt.Sprintf("%s timeline", project))
		}
	})

	// 7b. per-project knowledge.md seeding via vv index
	t.Run("index_knowledge_seeding", func(t *testing.T) {
		// Run vv index — rebuilds index, generates history.md, seeds knowledge.md
		stdout := mustRunVV(t, env, "index")
		assertContains(t, stdout, "indexed", "index stdout")

		// Verify per-project knowledge.md is seeded
		for _, project := range []string{"myproject", "other-project", "narr-project"} {
			knowledgePath := filepath.Join(vaultPath, "Projects", project, "knowledge.md")
			if !fileExists(knowledgePath) {
				t.Errorf("knowledge.md not created for %s", project)
				continue
			}
			content := readFile(t, knowledgePath)
			assertContains(t, content, "# Knowledge — "+project, project+" knowledge title")
			assertContains(t, content, "## Decisions", project+" has Decisions section")
			assertContains(t, content, "## Patterns", project+" has Patterns section")
			assertContains(t, content, "## Learnings", project+" has Learnings section")
		}
	})

	// 7c. stats
	t.Run("stats", func(t *testing.T) {
		stdout := mustRunVV(t, env, "stats")

		// Should contain expected sections
		assertContains(t, stdout, "Overview", "stats overview section")
		assertContains(t, stdout, "sessions", "stats sessions label")
		assertContains(t, stdout, "Averages", "stats averages section")
		assertContains(t, stdout, "Models", "stats models section")
		assertContains(t, stdout, "Monthly Trend", "stats monthly section")

		// Non-zero values
		assertContains(t, stdout, "4", "stats should show session count")
		assertContains(t, stdout, "claude-opus-4-6", "stats should show model name")

		// Project filter
		projStdout := mustRunVV(t, env, "stats", "--project", "myproject")
		assertContains(t, projStdout, "myproject", "project filter in header")
		assertNotContains(t, projStdout, "\nProjects\n", "no Projects section when filtered")

		// Help flag
		_, helpStderr, _ := runVV(t, env, "stats", "--help")
		assertContains(t, helpStderr, "stats", "stats help text")
	})

	// 8. backfill
	t.Run("backfill", func(t *testing.T) {
		// Set up backfill directory structure: basePath/project-x/{uuid}.jsonl
		backfillDir := t.TempDir()
		projectDir := filepath.Join(backfillDir, "project-x")
		writeFixture(t, projectDir, "abcdef01-2345-6789-abcd-ef0123456789.jsonl", readTestdata(t, "backfill.jsonl"))

		stdout := mustRunVV(t, env, "backfill", backfillDir)
		assertContains(t, stdout, "Found", "backfill found")
		assertContains(t, stdout, "processed:", "backfill processed")

		// Index now has 5 entries (3 original + 1 narrative + 1 backfill)
		idx := readIndex(t, stateDir)
		if len(idx) != 5 {
			t.Errorf("index entries after backfill: got %d, want 5", len(idx))
		}
		if _, ok := idx["abcdef01-2345-6789-abcd-ef0123456789"]; !ok {
			t.Error("backfill session not in index")
		}
	})

	// 8. archive
	t.Run("archive", func(t *testing.T) {
		stdout := mustRunVV(t, env, "archive")
		assertContains(t, stdout, "archived:", "archive stdout")

		archiveDir := filepath.Join(stateDir, "archive")
		if !dirExists(archiveDir) {
			t.Fatal("archive directory not created")
		}

		// At least one .jsonl.zst file
		entries, err := os.ReadDir(archiveDir)
		if err != nil {
			t.Fatalf("read archive dir: %v", err)
		}
		var zstFiles int
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".jsonl.zst") {
				info, _ := e.Info()
				if info.Size() > 0 {
					zstFiles++
				}
			}
		}
		if zstFiles == 0 {
			t.Error("no non-empty .jsonl.zst files in archive")
		}
	})

	// 9. stop_checkpoint_then_session_end
	t.Run("stop_checkpoint_then_session_end", func(t *testing.T) {
		stopTranscriptPath := writeFixture(t, fixtureDir, "session-stop-001.jsonl", readTestdata(t, "stop-session.jsonl"))

		// Build hook JSON for Stop event
		stopJSON, _ := json.Marshal(map[string]string{
			"session_id":      "session-stop-001",
			"transcript_path": stopTranscriptPath,
			"hook_event_name": "Stop",
			"cwd":             "/home/dev/myproject",
		})

		// Fire Stop event
		_, stopStderr, err := runVVWithStdin(t, env, string(stopJSON), "hook", "--event", "Stop")
		if err != nil {
			t.Fatalf("Stop hook failed: %v\nstderr: %s", err, stopStderr)
		}

		// Verify checkpoint note exists with status: checkpoint.
		// Phase 4: timestamp filename — find by index session_id.
		notePath := findSessionNoteByID(t, vaultPath, stateDir, "session-stop-001")

		checkpointNote := readFile(t, notePath)
		assertContains(t, checkpointNote, "status: checkpoint", "checkpoint status")
		assertContains(t, checkpointNote, "## Tool Usage", "tool usage section")
		assertContains(t, checkpointNote, "tool_uses:", "tool_uses frontmatter")

		// Verify index has checkpoint flag
		idx := readIndex(t, stateDir)
		stopEntry, ok := idx["session-stop-001"]
		if !ok {
			t.Fatal("session-stop-001 not in index after Stop")
		}
		if checkpoint, isOk := stopEntry["checkpoint"].(bool); !isOk || !checkpoint {
			t.Errorf("expected checkpoint=true in index, got %v", stopEntry["checkpoint"])
		}

		// Fire SessionEnd event (should finalize)
		endJSON, _ := json.Marshal(map[string]string{
			"session_id":      "session-stop-001",
			"transcript_path": stopTranscriptPath,
			"hook_event_name": "SessionEnd",
			"cwd":             "/home/dev/myproject",
		})

		_, endStderr, err := runVVWithStdin(t, env, string(endJSON), "hook", "--event", "SessionEnd")
		if err != nil {
			t.Fatalf("SessionEnd hook failed: %v\nstderr: %s", err, endStderr)
		}

		// Verify note is now finalized. Mechanism 3 (Phase 4) removes
		// the prior checkpoint and writes a fresh timestamp file —
		// re-resolve via index to get the post-SessionEnd path.
		finalNotePath := findSessionNoteByID(t, vaultPath, stateDir, "session-stop-001")
		finalizedNote := readFile(t, finalNotePath)
		assertContains(t, finalizedNote, "status: completed", "finalized status")
		assertNotContains(t, finalizedNote, "status: checkpoint", "no checkpoint in finalized")

		// Verify index no longer has checkpoint flag
		idx = readIndex(t, stateDir)
		finalEntry, ok := idx["session-stop-001"]
		if !ok {
			t.Fatal("session-stop-001 not in index after SessionEnd")
		}
		if checkpoint, ok := finalEntry["checkpoint"].(bool); ok && checkpoint {
			t.Error("checkpoint should be false after SessionEnd finalization")
		}

		// Verify only one note file remains for this session — Mechanism 3
		// removes the prior checkpoint before writing the finalized note.
		// Both notes will share today's date prefix (write-time clock).
		// Count files referencing session-stop-001 via the post-finalize
		// index entry's NotePath; assert the prior path is gone.
		projectDir := filepath.Join(vaultPath, "Projects", "myproject", "sessions")
		entries, _ := os.ReadDir(projectDir)
		stopNotes := 0
		// We can't filter by 2027-07-01 anymore (filename = write time,
		// not StartTime). Instead we check that exactly one file for
		// "session-stop-001" survives by reading the file and matching
		// its session_id frontmatter.
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			data, _ := os.ReadFile(filepath.Join(projectDir, e.Name()))
			if strings.Contains(string(data), "session_id: \"session-stop-001\"") {
				stopNotes++
			}
		}
		if stopNotes != 1 {
			t.Errorf("expected 1 note file for session-stop-001, got %d", stopNotes)
		}
	})

	// 10a. process friction session
	t.Run("process_friction_session", func(t *testing.T) {
		stdout := mustRunVV(t, env, "process", frictionPath)
		assertContains(t, stdout, "created:", "process stdout")

		notePath := findSessionNoteByID(t, vaultPath, stateDir, "session-friction-001")

		note := readFile(t, notePath)

		// Should have friction signals from corrections
		assertContains(t, note, "friction_score:", "friction_score frontmatter")

		// Index should have friction data
		idx := readIndex(t, stateDir)
		entry, ok := idx["session-friction-001"]
		if !ok {
			t.Fatal("session-friction-001 not in index")
		}
		if score, ok := entry["friction_score"].(float64); !ok || score == 0 {
			t.Errorf("expected non-zero friction_score, got %v", entry["friction_score"])
		}
		if corrections, ok := entry["corrections"].(float64); !ok || corrections == 0 {
			t.Errorf("expected non-zero corrections, got %v", entry["corrections"])
		}
	})

	// 10b. friction command
	t.Run("friction", func(t *testing.T) {
		stdout := mustRunVV(t, env, "friction")

		// Should contain expected sections
		assertContains(t, stdout, "Friction Analysis", "friction header")
		assertContains(t, stdout, "Overview", "friction overview")

		// Project filter
		projStdout := mustRunVV(t, env, "friction", "--project", "myproject")
		assertContains(t, projStdout, "myproject", "project filter in header")

		// Help flag
		_, helpStderr, _ := runVV(t, env, "friction", "--help")
		assertContains(t, helpStderr, "friction", "friction help text")
	})

	// 10c. trends
	t.Run("trends", func(t *testing.T) {
		stdout := mustRunVV(t, env, "trends")

		// Should contain expected sections
		assertContains(t, stdout, "Overview", "trends overview section")
		assertContains(t, stdout, "sessions", "trends sessions label")
		assertContains(t, stdout, "weeks", "trends weeks label")

		// Project filter
		projStdout := mustRunVV(t, env, "trends", "--project", "myproject")
		assertContains(t, projStdout, "myproject", "project filter in header")

		// Weeks flag
		weeksStdout := mustRunVV(t, env, "trends", "--weeks", "4")
		assertContains(t, weeksStdout, "Overview", "trends with --weeks has overview")

		// Help flag
		_, helpStderr, _ := runVV(t, env, "trends", "--help")
		assertContains(t, helpStderr, "trends", "trends help text")
	})

	// 10d. inject
	t.Run("inject", func(t *testing.T) {
		// Default markdown output for myproject
		stdout := mustRunVV(t, env, "inject", "--project", "myproject")
		assertContains(t, stdout, "# Context: myproject", "inject context header")
		assertContains(t, stdout, "## Recent Sessions", "inject sessions section")

		// JSON format
		jsonStdout := mustRunVV(t, env, "inject", "--project", "myproject", "--format", "json")
		var parsed map[string]any
		if err := json.Unmarshal([]byte(jsonStdout), &parsed); err != nil {
			t.Fatalf("invalid JSON from inject: %v\noutput: %s", err, jsonStdout)
		}
		if parsed["project"] != "myproject" {
			t.Errorf("JSON project = %v, want myproject", parsed["project"])
		}

		// Sections filter
		sectionsStdout := mustRunVV(t, env, "inject", "--project", "myproject", "--sections", "summary,sessions")
		assertContains(t, sectionsStdout, "# Context: myproject", "sections filter has header")
		assertNotContains(t, sectionsStdout, "## Open Threads", "sections filter excludes threads")
		assertNotContains(t, sectionsStdout, "## Decisions", "sections filter excludes decisions")

		// Max tokens (very low)
		smallStdout := mustRunVV(t, env, "inject", "--project", "myproject", "--max-tokens", "50")
		assertContains(t, smallStdout, "# Context: myproject", "small budget has header")

		// Help flag
		_, helpStderr, _ := runVV(t, env, "inject", "--help")
		assertContains(t, helpStderr, "inject", "inject help text")

		// Warning for unknown project
		_, warnStderr, _ := runVV(t, env, "inject", "--project", "nonexistent")
		assertContains(t, warnStderr, "no sessions found", "warning for unknown project")

	})

	// 10e. context init + migrate
	t.Run("context_init_and_migrate", func(t *testing.T) {
		agentctxDir := filepath.Join(vaultPath, "Projects", "ctx-project", "agentctx")

		// --- Section 1: context init — verify agentctx artifacts ---
		repoCwd := t.TempDir()

		stdout := mustRunVVInDir(t, env, repoCwd, "context", "init", "--project", "ctx-project")
		assertContains(t, stdout, "Context initialized", "context init stdout")
		assertContains(t, stdout, "ctx-project", "context init project name")

		// Vault-side agentctx structure
		if !fileExists(filepath.Join(agentctxDir, "workflow.md")) {
			t.Error("vault agentctx/workflow.md not created")
		} else {
			content := readFile(t, filepath.Join(agentctxDir, "workflow.md"))
			assertContains(t, content, "Pair Programming", "agentctx/workflow.md behavioral rules")
		}
		if !fileExists(filepath.Join(agentctxDir, "resume.md")) {
			t.Error("vault agentctx/resume.md not created")
		}
		if !fileExists(filepath.Join(agentctxDir, "iterations.md")) {
			t.Error("vault agentctx/iterations.md not created")
		}
		if !fileExists(filepath.Join(agentctxDir, "commands", "restart.md")) {
			t.Error("vault agentctx/commands/restart.md not created")
		}
		if !fileExists(filepath.Join(agentctxDir, "commands", "wrap.md")) {
			t.Error("vault agentctx/commands/wrap.md not created")
		}
		if !dirExists(filepath.Join(agentctxDir, "tasks")) {
			t.Error("vault agentctx/tasks/ not created")
		}
		if !dirExists(filepath.Join(agentctxDir, "tasks", "done")) {
			t.Error("vault agentctx/tasks/done/ not created")
		}

		// Repo-side: CLAUDE.md is a regular file with MCP-first content
		if !fileExists(filepath.Join(repoCwd, "CLAUDE.md")) {
			t.Error("repo CLAUDE.md not created by context init")
		} else {
			claudeContent := readFile(t, filepath.Join(repoCwd, "CLAUDE.md"))
			assertContains(t, claudeContent, "vv_bootstrap_context", "CLAUDE.md references MCP bootstrap")
		}

		// Repo-side: CLAUDE.md is NOT a symlink
		if isSymlink(filepath.Join(repoCwd, "CLAUDE.md")) {
			t.Error("CLAUDE.md should be a regular file, not a symlink")
		}

		// Repo-side: .claude/commands is a real directory (not symlink)
		cmdsPath := filepath.Join(repoCwd, ".claude", "commands")
		if isSymlink(cmdsPath) {
			t.Error(".claude/commands should be a real directory, not a symlink")
		}

		// Commands are readable through the directory
		if !fileExists(filepath.Join(cmdsPath, "restart.md")) {
			t.Error("restart.md not readable in .claude/commands/")
		}
		if !fileExists(filepath.Join(cmdsPath, "wrap.md")) {
			t.Error("wrap.md not readable in .claude/commands/")
		}

		// .gitignore contains expected entries (NOT agentctx)
		gitignoreContent := readFile(t, filepath.Join(repoCwd, ".gitignore"))
		assertContains(t, gitignoreContent, "CLAUDE.md", ".gitignore contains CLAUDE.md")
		assertContains(t, gitignoreContent, "commit.msg", ".gitignore contains commit.msg")
		assertNotContains(t, gitignoreContent, "agentctx", ".gitignore should NOT contain agentctx")

		// .version file created at latest schema
		if !fileExists(filepath.Join(agentctxDir, ".version")) {
			t.Error("vault agentctx/.version not created")
		} else {
			versionContent := readFile(t, filepath.Join(agentctxDir, ".version"))
			assertContains(t, versionContent, "schema_version = 10", ".version has latest schema")
		}

		// No agentctx symlink at repo root (v5)
		if isSymlink(filepath.Join(repoCwd, "agentctx")) {
			t.Error("agentctx symlink should NOT exist at repo root")
		}

		// CLAUDE.md has no absolute path
		claudeContent := readFile(t, filepath.Join(repoCwd, "CLAUDE.md"))
		assertNotContains(t, claudeContent, vaultPath, "CLAUDE.md should not contain absolute vault path")

		// Templates/agentctx/ seeded in vault
		if !fileExists(filepath.Join(vaultPath, "Templates", "agentctx", "README.md")) {
			t.Error("Templates/agentctx/README.md not seeded")
		}

		// --- Section 2: context init idempotent re-run ---
		stdout2 := mustRunVVInDir(t, env, repoCwd, "context", "init", "--project", "ctx-project")
		assertContains(t, stdout2, "Context initialized", "idempotent context init stdout")

		// Vault files still exist
		if !fileExists(filepath.Join(agentctxDir, "workflow.md")) {
			t.Error("vault agentctx/workflow.md missing after re-run")
		}
		if !fileExists(filepath.Join(agentctxDir, "resume.md")) {
			t.Error("vault agentctx/resume.md missing after re-run")
		}
		if !fileExists(filepath.Join(agentctxDir, "iterations.md")) {
			t.Error("vault agentctx/iterations.md missing after re-run")
		}

		// Repo-side CLAUDE.md content unchanged
		claudeAfter := readFile(t, filepath.Join(repoCwd, "CLAUDE.md"))
		assertContains(t, claudeAfter, "vv_bootstrap_context", "CLAUDE.md still has MCP content after re-run")

		// .claude/commands still a valid directory
		if !dirExists(cmdsPath) {
			t.Error(".claude/commands not a directory after idempotent re-run")
		}

		// --- Section 3: context migrate — verify file copy + symlink replacement ---
		migrateDir := t.TempDir()
		writeFixture(t, migrateDir, "RESUME.md", "# My Resume\nProject state.")
		writeFixture(t, migrateDir, "HISTORY.md", "# Iteration History")
		writeFixture(t, filepath.Join(migrateDir, "tasks"), "001-feature.md", "Feature task")
		// Simulate a local command file (regular file, not symlink)
		writeFixture(t, filepath.Join(migrateDir, ".claude", "commands"), "custom.md", "# Custom Command")

		migrateStdout := mustRunVVInDir(t, env, migrateDir, "context", "migrate", "--project", "migrate-test")
		assertContains(t, migrateStdout, "Context migrated", "context migrate stdout")

		migrateAgentctx := filepath.Join(vaultPath, "Projects", "migrate-test", "agentctx")

		// Vault-side: migrated content in agentctx/
		migrateResume := filepath.Join(migrateAgentctx, "resume.md")
		if !fileExists(migrateResume) {
			t.Error("vault agentctx/resume.md not created by migrate")
		} else {
			content := readFile(t, migrateResume)
			assertContains(t, content, "My Resume", "migrated resume content")
		}

		migrateIter := filepath.Join(migrateAgentctx, "iterations.md")
		if !fileExists(migrateIter) {
			t.Error("vault agentctx/iterations.md not created by migrate")
		} else {
			content := readFile(t, migrateIter)
			assertContains(t, content, "Iteration History", "migrated iterations content")
		}

		migrateTask := filepath.Join(migrateAgentctx, "tasks", "001-feature.md")
		if !fileExists(migrateTask) {
			t.Error("vault agentctx/tasks/001-feature.md not created by migrate")
		} else {
			content := readFile(t, migrateTask)
			assertContains(t, content, "Feature task", "migrated task content")
		}

		// Local command was copied to vault agentctx/commands/
		if !fileExists(filepath.Join(migrateAgentctx, "commands", "custom.md")) {
			t.Error("vault agentctx/commands/custom.md not migrated")
		} else {
			content := readFile(t, filepath.Join(migrateAgentctx, "commands", "custom.md"))
			assertContains(t, content, "Custom Command", "migrated custom command content")
		}

		// Default commands generated
		if !fileExists(filepath.Join(migrateAgentctx, "commands", "restart.md")) {
			t.Error("vault agentctx/commands/restart.md not created by migrate")
		}
		if !fileExists(filepath.Join(migrateAgentctx, "commands", "wrap.md")) {
			t.Error("vault agentctx/commands/wrap.md not created by migrate")
		}

		// Behavioral rules present
		if !fileExists(filepath.Join(migrateAgentctx, "workflow.md")) {
			t.Error("vault agentctx/workflow.md not created by migrate")
		} else {
			content := readFile(t, filepath.Join(migrateAgentctx, "workflow.md"))
			assertContains(t, content, "Pair Programming", "migrate agentctx/workflow.md behavioral rules")
		}

		// Repo-side: CLAUDE.md updated to MCP-first content
		migrateClaudeContent := readFile(t, filepath.Join(migrateDir, "CLAUDE.md"))
		assertContains(t, migrateClaudeContent, "vv_bootstrap_context", "migrated CLAUDE.md has MCP-first content")

		// Repo-side: .claude/commands is a real directory (not symlink)
		migrateCmdsPath := filepath.Join(migrateDir, ".claude", "commands")
		if isSymlink(migrateCmdsPath) {
			t.Error(".claude/commands should be a real directory after migrate")
		}
		if !dirExists(migrateCmdsPath) {
			t.Error(".claude/commands should be a directory after migrate")
		}

		// Local originals preserved
		if !fileExists(filepath.Join(migrateDir, "RESUME.md")) {
			t.Error("local RESUME.md deleted by migrate")
		}
		if !fileExists(filepath.Join(migrateDir, "HISTORY.md")) {
			t.Error("local HISTORY.md deleted by migrate")
		}

		// Help flags work
		_, helpStderr, _ := runVV(t, env, "context", "--help")
		assertContains(t, helpStderr, "context", "context help text")

		_, initHelpStderr, _ := runVV(t, env, "context", "init", "--help")
		assertContains(t, initHelpStderr, "init", "context init help text")

		_, migrateHelpStderr, _ := runVV(t, env, "context", "migrate", "--help")
		assertContains(t, migrateHelpStderr, "migrate", "context migrate help text")
	})

	// 10e2. context sync
	t.Run("context_sync", func(t *testing.T) {
		syncCwd := t.TempDir()
		// Seed the project marker — `vv context sync` requires
		// .vibe-vault.toml in cwd or an ancestor (unless --all).
		writeFixture(t, syncCwd, ".vibe-vault.toml", "# vibe-vault project marker\n")

		// Create a legacy agentctx (no .version) to test migration
		legacyProject := "sync-legacy"
		legacyAgentctx := filepath.Join(vaultPath, "Projects", legacyProject, "agentctx")
		os.MkdirAll(filepath.Join(legacyAgentctx, "commands"), 0o755)
		os.WriteFile(filepath.Join(legacyAgentctx, "resume.md"), []byte("# Resume"), 0o644)

		// Run sync — should migrate 0→6
		stdout := mustRunVVInDir(t, env, syncCwd, "context", "sync", "--project", legacyProject)
		assertContains(t, stdout, "v0", "sync shows from version")
		assertContains(t, stdout, "v10", "sync shows to version")

		// .version should be at latest
		versionContent := readFile(t, filepath.Join(legacyAgentctx, ".version"))
		assertContains(t, versionContent, "schema_version = 10", ".version at latest after sync")

		// No agentctx symlink at repo root (v5)
		if isSymlink(filepath.Join(syncCwd, "agentctx")) {
			t.Error("agentctx symlink should NOT exist after v5 sync")
		}

		// CLAUDE.md should be a regular file with MCP-first content
		if isSymlink(filepath.Join(syncCwd, "CLAUDE.md")) {
			t.Error("CLAUDE.md should be a regular file after sync")
		}
		claudeContent := readFile(t, filepath.Join(syncCwd, "CLAUDE.md"))
		assertNotContains(t, claudeContent, vaultPath, "CLAUDE.md should not contain absolute path after sync")
		assertContains(t, claudeContent, "vv_bootstrap_context", "CLAUDE.md should have MCP-first content")

		// .claude/commands is a real directory (not symlink)
		if isSymlink(filepath.Join(syncCwd, ".claude", "commands")) {
			t.Error(".claude/commands should be a real directory after sync")
		}

		// Add a shared command to Templates, run sync again → propagated
		tmplCmds := filepath.Join(vaultPath, "Templates", "agentctx", "commands")
		os.MkdirAll(tmplCmds, 0o755)
		writeFixture(t, tmplCmds, "shared.md", "# Shared Command")

		syncStdout2 := mustRunVVInDir(t, env, syncCwd, "context", "sync", "--project", legacyProject)
		assertContains(t, syncStdout2, legacyProject, "second sync shows project name")

		// Shared command should be propagated
		if !fileExists(filepath.Join(legacyAgentctx, "commands", "shared.md")) {
			t.Error("shared command not propagated by sync")
		} else {
			content := readFile(t, filepath.Join(legacyAgentctx, "commands", "shared.md"))
			assertContains(t, content, "Shared Command", "propagated command content")
		}

		// --dry-run should not modify files
		writeFixture(t, tmplCmds, "drytest.md", "# Dry Test")
		mustRunVVInDir(t, env, syncCwd, "context", "sync", "--project", legacyProject, "--dry-run")
		if fileExists(filepath.Join(legacyAgentctx, "commands", "drytest.md")) {
			t.Error("dry-run should not create files")
		}

		// Third sync propagates drytest.md (dry-run didn't create it)
		syncStdout3 := mustRunVVInDir(t, env, syncCwd, "context", "sync", "--project", legacyProject)
		assertContains(t, syncStdout3, legacyProject, "third sync shows project name")

		// Fourth sync — truly idempotent, no changes
		syncStdout4 := mustRunVVInDir(t, env, syncCwd, "context", "sync", "--project", legacyProject)
		assertContains(t, syncStdout4, "current", "idempotent sync shows current")

		// Help flag
		_, syncHelpStderr, _ := runVV(t, env, "context", "sync", "--help")
		assertContains(t, syncHelpStderr, "sync", "context sync help text")
	})

	// 10e3. context marker-check guards
	t.Run("context_marker_guards", func(t *testing.T) {
		// sync without marker → hard-fail
		noMarker := t.TempDir()
		_, stderr, err := runVVInDir(t, env, noMarker, "context", "sync", "--project", "anything")
		if err == nil {
			t.Error("expected sync to fail without .vibe-vault.toml marker")
		}
		assertContains(t, stderr, "not in a vibe-vault project", "sync error message")

		// sync --all without marker → succeeds (explicit opt-out)
		_, _, err = runVVInDir(t, env, noMarker, "context", "sync", "--all")
		if err != nil {
			t.Errorf("sync --all should succeed without marker: %v", err)
		}

		// migrate without marker AND without --project → hard-fail
		_, stderr, err = runVVInDir(t, env, noMarker, "context", "migrate")
		if err == nil {
			t.Error("expected migrate to fail without marker or --project")
		}
		assertContains(t, stderr, "no --project flag", "migrate error message")

		// migrate with --project (no marker) → proceeds (legitimate bootstrap)
		migrateOK := t.TempDir()
		writeFixture(t, migrateOK, "RESUME.md", "# Legacy")
		_, _, err = runVVInDir(t, env, migrateOK, "context", "migrate", "--project", "guard-bootstrap-test")
		if err != nil {
			t.Errorf("migrate with --project should succeed without marker: %v", err)
		}

		// sync with marker in an ancestor directory → succeeds
		ancestorRoot := t.TempDir()
		writeFixture(t, ancestorRoot, ".vibe-vault.toml", "# marker\n")
		nested := filepath.Join(ancestorRoot, "sub", "dir")
		if mkErr := os.MkdirAll(nested, 0o755); mkErr != nil {
			t.Fatal(mkErr)
		}
		_, _, err = runVVInDir(t, env, nested, "context", "sync", "--project", "sync-legacy")
		if err != nil {
			t.Errorf("sync should succeed when marker is in ancestor: %v", err)
		}
	})

	// 10f. check resume-invariants — CLI smoke test for the v10 Current
	// State invariant lint. Exercises `vv check`'s `resume-invariants`
	// result against three fake repos: clean-pass, dirty-warn, pre-v10
	// (omitted). Sandboxes HOME so `CheckMemoryLink` — which calls
	// `os.UserHomeDir()` — cannot read the developer's real ~/.claude/.
	t.Run("check_resume_invariants", func(t *testing.T) {
		// Sandbox HOME for this subtest; CheckMemoryLink (in the vv check
		// pipeline) reads ~/.claude/ via os.UserHomeDir, which would
		// otherwise escape the test sandbox.
		sandboxedEnv := buildEnvWithHome(xdgConfigHome, t.TempDir())

		// --- Case 1: clean v10 project scaffolded by `vv context init` ---
		cleanRepo := t.TempDir()
		writeFixture(t, cleanRepo, ".vibe-vault.toml",
			"[project]\nname = \"resume-invariants-clean\"\n")
		mustRunVVInDir(t, sandboxedEnv, cleanRepo,
			"context", "init", "--project", "resume-invariants-clean")

		cleanStdout, cleanStderr, cleanErr := runVVInDir(t, sandboxedEnv, cleanRepo, "check")
		if cleanErr != nil {
			t.Fatalf("vv check (clean) returned non-zero: %v\nstdout: %s\nstderr: %s",
				cleanErr, cleanStdout, cleanStderr)
		}
		assertContains(t, cleanStdout, "resume-invariants",
			"clean case: check output mentions resume-invariants")
		// Report.Format() renders each row as "  %-4s  %-*s  %s\n" where
		// the first %s is Status.String() — "pass" for Pass. Assert the
		// row itself pairs the status with the check name.
		cleanPassRow := false
		for line := range strings.SplitSeq(cleanStdout, "\n") {
			if strings.Contains(line, "resume-invariants") && strings.Contains(line, "pass") {
				cleanPassRow = true
				break
			}
		}
		if !cleanPassRow {
			t.Errorf("clean case: expected a row pairing 'pass' with 'resume-invariants'\nstdout: %s",
				cleanStdout)
		}

		// --- Case 2: dirty v10 project — rewrite Current State body ---
		dirtyRepo := t.TempDir()
		writeFixture(t, dirtyRepo, ".vibe-vault.toml",
			"[project]\nname = \"resume-invariants-dirty\"\n")
		mustRunVVInDir(t, sandboxedEnv, dirtyRepo,
			"context", "init", "--project", "resume-invariants-dirty")

		dirtyResumePath := filepath.Join(vaultPath, "Projects",
			"resume-invariants-dirty", "agentctx", "resume.md")
		dirtyResume := readFile(t, dirtyResumePath)
		// Replace the body under `## Current State` (up to the next `## `
		// heading) with a single non-invariant bullet. Keep the heading
		// itself intact; leave every other section untouched.
		const csHeading = "## Current State"
		headingIdx := strings.Index(dirtyResume, csHeading)
		if headingIdx < 0 {
			t.Fatalf("dirty case: scaffolded resume.md missing %q heading\n%s",
				csHeading, dirtyResume)
		}
		bodyStart := headingIdx + len(csHeading)
		// Find the next `## ` heading after the body starts.
		rest := dirtyResume[bodyStart:]
		nextHeadingRel := strings.Index(rest, "\n## ")
		var nextHeadingAbs int
		if nextHeadingRel < 0 {
			nextHeadingAbs = len(dirtyResume)
		} else {
			nextHeadingAbs = bodyStart + nextHeadingRel
		}
		rewritten := dirtyResume[:bodyStart] +
			"\n\n- **Phase:** narrative paragraph explaining in-flight work\n\n" +
			dirtyResume[nextHeadingAbs:]
		if err := os.WriteFile(dirtyResumePath, []byte(rewritten), 0o644); err != nil {
			t.Fatalf("write dirty resume.md: %v", err)
		}

		dirtyStdout, dirtyStderr, dirtyErr := runVVInDir(t, sandboxedEnv, dirtyRepo, "check")
		// Warn does NOT cause non-zero exit — only Fail does. A non-zero
		// here indicates some other check failed; surface both streams.
		if dirtyErr != nil {
			t.Fatalf("vv check (dirty) returned non-zero: %v\nstdout: %s\nstderr: %s",
				dirtyErr, dirtyStdout, dirtyStderr)
		}
		assertContains(t, dirtyStdout, "resume-invariants",
			"dirty case: check output mentions resume-invariants")
		dirtyWarnRow := false
		for line := range strings.SplitSeq(dirtyStdout, "\n") {
			if strings.Contains(line, "resume-invariants") && strings.Contains(line, "warn") {
				dirtyWarnRow = true
				break
			}
		}
		if !dirtyWarnRow {
			t.Errorf("dirty case: expected a row pairing 'warn' with 'resume-invariants'\nstdout: %s",
				dirtyStdout)
		}
		// Detail for a non-invariant bullet includes the offending line
		// (truncated). The bullet we injected starts with "**Phase:**".
		assertContains(t, dirtyStdout, "Phase",
			"dirty case: warn detail references the offending bullet key")

		// --- Case 3: pre-v10 project — resume-invariants omitted entirely ---
		preV10Repo := t.TempDir()
		writeFixture(t, preV10Repo, ".vibe-vault.toml",
			"[project]\nname = \"resume-invariants-prev10\"\n")
		preV10Agentctx := filepath.Join(vaultPath, "Projects",
			"resume-invariants-prev10", "agentctx")
		if err := os.MkdirAll(preV10Agentctx, 0o755); err != nil {
			t.Fatalf("mkdir pre-v10 agentctx: %v", err)
		}
		if err := os.WriteFile(filepath.Join(preV10Agentctx, ".version"),
			[]byte("schema_version = 9\n"), 0o644); err != nil {
			t.Fatalf("write pre-v10 .version: %v", err)
		}
		if err := os.WriteFile(filepath.Join(preV10Agentctx, "resume.md"),
			[]byte("# resume-invariants-prev10\n\nMinimal resume with no Current State section.\n"),
			0o644); err != nil {
			t.Fatalf("write pre-v10 resume.md: %v", err)
		}

		preStdout, preStderr, preErr := runVVInDir(t, sandboxedEnv, preV10Repo, "check")
		if preErr != nil {
			t.Fatalf("vv check (pre-v10) returned non-zero: %v\nstdout: %s\nstderr: %s",
				preErr, preStdout, preStderr)
		}
		// The result Name is literally "resume-invariants"; Format renders
		// it as the middle column. Matching on that column specifically
		// avoids false hits where the project name (which contains the
		// substring) appears in the Detail column of other rows.
		for line := range strings.SplitSeq(preStdout, "\n") {
			// A row looks like: "  pass  resume-invariants  detail..."
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == "resume-invariants" {
				t.Errorf("pre-v10 case: resume-invariants check row must be omitted\nstdout: %s",
					preStdout)
				break
			}
		}
	})

	// 10h. check toolchain — vv-check-toolchain Phase 2. Confirms the
	// CheckToolchain() probe wired into runCheck() emits one
	// `tool:<bin>` row per spec, regardless of whether the cwd is a
	// detectable project. Two cases: a non-project temp dir (DetectProject
	// returns "_unknown" so the project-scoped checks are skipped, and the
	// toolchain probe is the only project-independent addition) and the
	// JSON path.
	t.Run("check_toolchain_non_project_cwd", func(t *testing.T) {
		nonProject := t.TempDir()
		stdout, stderr, err := runVVInDir(t, env, nonProject, "check")
		if err != nil {
			t.Fatalf("vv check (non-project cwd) returned non-zero: %v\nstdout: %s\nstderr: %s",
				err, stdout, stderr)
		}
		// Each toolchainSpec must show up as a `tool:<bin>` row in the
		// human-readable Format() output. The probe runs unconditionally,
		// so even a non-project cwd must surface every entry.
		for _, bin := range []string{"go", "golangci-lint", "gh", "make", "git"} {
			needle := "tool:" + bin
			if !strings.Contains(stdout, needle) {
				t.Errorf("non-project cwd: expected %q row in `vv check` output\nstdout: %s",
					needle, stdout)
			}
		}
	})

	t.Run("check_toolchain_json_emits_entries", func(t *testing.T) {
		nonProject := t.TempDir()
		stdout, stderr, err := runVVInDir(t, env, nonProject, "check", "--json")
		if err != nil {
			t.Fatalf("vv check --json returned non-zero: %v\nstdout: %s\nstderr: %s",
				err, stdout, stderr)
		}
		// Decode and project: collect every check whose Name has the
		// "tool:" prefix. The status of each is host-dependent (pass when
		// the binary is installed, warn when missing), but the entries
		// themselves must be present.
		var report struct {
			Checks []struct {
				Name   string `json:"name"`
				Status string `json:"status"`
				Detail string `json:"detail"`
			} `json:"checks"`
		}
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatalf("invalid JSON from `vv check --json`: %v\nstdout: %s", err, stdout)
		}
		toolEntries := map[string]string{}
		for _, c := range report.Checks {
			if strings.HasPrefix(c.Name, "tool:") {
				toolEntries[c.Name] = c.Status
			}
		}
		for _, bin := range []string{"go", "golangci-lint", "gh", "make", "git"} {
			name := "tool:" + bin
			status, ok := toolEntries[name]
			if !ok {
				t.Errorf("--json: missing entry for %q\nentries seen: %v", name, toolEntries)
				continue
			}
			// Host-dependent verdict: only assert that the value is one
			// of the documented set. fail is not produced by the probe.
			if status != "pass" && status != "warn" {
				t.Errorf("--json: %q status %q not in {pass, warn}", name, status)
			}
		}
	})

	// 10g. export
	t.Run("export", func(t *testing.T) {
		// JSON export (all sessions)
		jsonStdout := mustRunVV(t, env, "export")
		var jsonData []map[string]any
		if err := json.Unmarshal([]byte(jsonStdout), &jsonData); err != nil {
			t.Fatalf("invalid JSON from export: %v\noutput: %s", err, jsonStdout)
		}
		if len(jsonData) == 0 {
			t.Error("expected non-empty JSON array from export")
		}

		// JSON export filtered by project
		projStdout := mustRunVV(t, env, "export", "--project", "myproject")
		var projData []map[string]any
		if err := json.Unmarshal([]byte(projStdout), &projData); err != nil {
			t.Fatalf("invalid JSON from filtered export: %v", err)
		}
		for _, entry := range projData {
			if entry["project"] != "myproject" {
				t.Errorf("filtered export has wrong project: %v", entry["project"])
			}
		}

		// CSV export
		csvStdout := mustRunVV(t, env, "export", "--format", "csv")
		assertContains(t, csvStdout, "date,project,session_id", "CSV header")
		lines := strings.Split(strings.TrimSpace(csvStdout), "\n")
		if len(lines) < 2 {
			t.Errorf("expected header + data rows, got %d lines", len(lines))
		}

		// Help flag
		_, helpStderr, _ := runVV(t, env, "export", "--help")
		assertContains(t, helpStderr, "export", "export help text")
	})

	// 11. reprocess
	t.Run("reprocess", func(t *testing.T) {
		stdout := mustRunVV(t, env, "reprocess")
		assertContains(t, stdout, "reprocessed:", "reprocess stdout")

		// Notes still exist with valid content. Phase 4 timestamp
		// filenames mean we resolve via the post-reprocess index
		// rather than hardcoding -NN.md paths.
		for _, tc := range []struct {
			sessionID string
			project   string
		}{
			{"session-aaa-001", "myproject"},
			{"session-aaa-002", "myproject"},
			{"session-bbb-001", "other-project"},
		} {
			absPath := findSessionNoteByID(t, vaultPath, stateDir, tc.sessionID)
			note := readFile(t, absPath)
			assertContains(t, note, fmt.Sprintf("session_id: \"%s\"", tc.sessionID), tc.sessionID+" session_id")
			assertContains(t, note, fmt.Sprintf("project: %s", tc.project), tc.sessionID+" project")
		}

		// history.md regenerated
		for _, project := range []string{"myproject", "other-project"} {
			ctxPath := filepath.Join(vaultPath, "Projects", project, "history.md")
			if !fileExists(ctxPath) {
				t.Errorf("history.md not regenerated for %s", project)
			}
		}

		// Index preserved
		idx := readIndex(t, stateDir)
		if len(idx) < 3 {
			t.Errorf("index entries after reprocess: got %d, want >= 3", len(idx))
		}
	})

	// 12. MCP server
	t.Run("mcp", func(t *testing.T) {
		// Build a sequence of JSON-RPC requests, newline-delimited
		requests := strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"vv_list_projects"}}`,
			`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"vv_get_project_context","arguments":{"project":"myproject"}}}`,
			`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nonexistent"}}`,
			`{"jsonrpc":"2.0","id":6,"method":"unknown/method"}`,
		}, "\n")

		stdout, stderr, err := runVVWithStdin(t, env, requests, "mcp")
		if err != nil {
			t.Fatalf("vv mcp failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		// Parse responses
		lines := strings.Split(strings.TrimSpace(stdout), "\n")
		// Expect 6 responses (notification gets no response)
		if len(lines) != 6 {
			t.Fatalf("expected 6 response lines, got %d:\n%s", len(lines), stdout)
		}

		var responses []map[string]any
		for i, line := range lines {
			var resp map[string]any
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				t.Fatalf("response %d: invalid JSON: %v\nline: %s", i, err, line)
			}
			responses = append(responses, resp)
		}

		// Response 0: initialize — should have serverInfo
		if r := responses[0]["result"].(map[string]any); r["serverInfo"] == nil {
			t.Error("initialize: missing serverInfo")
		}

		// Response 1: tools/list — exact-set check for all 42 registered tools.
		// Update this list when adding or removing tools; the exact-set check
		// prevents silent breakage from numeric drift (O2 from iter-150).
		expectedTools := []string{
			"vv_get_project_context",
			"vv_list_projects",
			"vv_search_sessions",
			"vv_get_knowledge",
			"vv_get_session_detail",
			"vv_get_friction_trends",
			"vv_get_effectiveness",
			"vv_capture_session",
			"vv_get_workflow",
			"vv_get_resume",
			"vv_list_tasks",
			"vv_get_task",
			"vv_update_resume",
			"vv_append_iteration",
			"vv_manage_task",
			"vv_refresh_index",
			"vv_bootstrap_context",
			"vv_list_learnings",
			"vv_get_learning",
			"vv_get_iterations",
			"vv_get_project_root",
			"vv_set_commit_msg",
			"vv_stamp_iter",
			"vv_thread_insert",
			"vv_thread_replace",
			"vv_thread_remove",
			"vv_carried_add",
			"vv_carried_remove",
			"vv_carried_promote_to_task",
			"vv_vault_read",
			"vv_vault_list",
			"vv_vault_exists",
			"vv_vault_sha256",
			"vv_vault_write",
			"vv_vault_edit",
			"vv_vault_delete",
			"vv_vault_move",
			"vv_get_agent_definition",
			"vv_describe_iter_state",
			"vv_render_wrap_text",
			"vv_worktree_gc",
			"vv_check_toolchain",
		}
		toolsResult := responses[1]["result"].(map[string]any)
		tools := toolsResult["tools"].([]any)
		toolNames := make(map[string]bool)
		for _, tool := range tools {
			toolNames[tool.(map[string]any)["name"].(string)] = true
		}
		for _, want := range expectedTools {
			if !toolNames[want] {
				t.Errorf("tools/list: missing expected tool %q", want)
			}
		}
		if len(tools) != len(expectedTools) {
			// List unexpected extras to help diagnose.
			for name := range toolNames {
				found := false
				for _, want := range expectedTools {
					if name == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("tools/list: unexpected tool %q (not in expected set)", name)
				}
			}
			t.Errorf("tools/list: got %d tools, want %d", len(tools), len(expectedTools))
		}

		// Response 2: vv_list_projects — should return project data
		listResult := responses[2]["result"].(map[string]any)
		content := listResult["content"].([]any)
		if len(content) == 0 {
			t.Fatal("vv_list_projects: empty content")
		}
		listText := content[0].(map[string]any)["text"].(string)
		var projects []map[string]any
		if err := json.Unmarshal([]byte(listText), &projects); err != nil {
			t.Fatalf("vv_list_projects: invalid JSON in text: %v", err)
		}
		if len(projects) == 0 {
			t.Error("vv_list_projects: no projects returned")
		}

		// Response 3: vv_get_project_context — should return context for myproject
		ctxResult := responses[3]["result"].(map[string]any)
		ctxContent := ctxResult["content"].([]any)
		ctxText := ctxContent[0].(map[string]any)["text"].(string)
		var ctxParsed map[string]any
		if err := json.Unmarshal([]byte(ctxText), &ctxParsed); err != nil {
			t.Fatalf("vv_get_project_context: invalid JSON: %v", err)
		}
		if ctxParsed["project"] != "myproject" {
			t.Errorf("vv_get_project_context: project = %v, want myproject", ctxParsed["project"])
		}

		// Response 4: unknown tool — should have isError
		unknownResult := responses[4]["result"].(map[string]any)
		if unknownResult["isError"] != true {
			t.Error("unknown tool: expected isError=true")
		}

		// Response 5: unknown method — should have error
		if responses[5]["error"] == nil {
			t.Error("unknown method: expected error")
		}

		// Stderr should contain tool call log lines
		assertContains(t, stderr, "tools/call: vv_list_projects", "stderr log")
		assertContains(t, stderr, "tools/call: vv_get_project_context", "stderr log")
	})

	// 13. MCP learnings tools
	t.Run("mcp_learnings", func(t *testing.T) {
		// Seed three learning files, one per allowed type.
		learningsDir := filepath.Join(vaultPath, "Knowledge", "learnings")
		if err := os.MkdirAll(learningsDir, 0o755); err != nil {
			t.Fatalf("mkdir learnings: %v", err)
		}

		userLearning := `---
name: Sample User Learning
description: User-type learning for integration testing
type: user
---

Body content for the user-type sample learning.
`
		feedbackLearning := `---
name: Sample Feedback Learning
description: Feedback-type learning for integration testing
type: feedback
---

Body content for the feedback-type sample learning.
`
		referenceLearning := `---
name: Sample Reference Learning
description: Reference-type learning for integration testing
type: reference
---

Body content for the reference-type sample learning.
`
		writeFixture(t, learningsDir, "sample-user.md", userLearning)
		writeFixture(t, learningsDir, "sample-feedback.md", feedbackLearning)
		writeFixture(t, learningsDir, "sample-reference.md", referenceLearning)

		requests := strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"vv_list_learnings"}}`,
			`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"vv_list_learnings","arguments":{"filter_type":"feedback"}}}`,
			`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"vv_get_learning","arguments":{"slug":"sample-user"}}}`,
			`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"vv_get_learning","arguments":{"slug":"does-not-exist"}}}`,
		}, "\n")

		stdout, stderr, err := runVVWithStdin(t, env, requests, "mcp")
		if err != nil {
			t.Fatalf("vv mcp failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		trimmed := strings.TrimSpace(stdout)
		var lines []string
		for line := range strings.SplitSeq(trimmed, "\n") {
			lines = append(lines, line)
		}
		// 5 requests have an id → 5 responses; the notification is silent.
		if len(lines) != 5 {
			t.Fatalf("expected 5 response lines, got %d:\n%s", len(lines), stdout)
		}

		var responses []map[string]any
		for i, line := range lines {
			var resp map[string]any
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				t.Fatalf("response %d: invalid JSON: %v\nline: %s", i, err, line)
			}
			responses = append(responses, resp)
		}

		// Response 0: initialize — must have serverInfo.
		initResult, initOK := responses[0]["result"].(map[string]any)
		if !initOK {
			t.Fatalf("initialize: missing result: %v", responses[0])
		}
		if initResult["serverInfo"] == nil {
			t.Error("initialize: missing serverInfo")
		}

		// Helper to pull content[0].text out of a tool/call result.
		extractText := func(idx int) string {
			t.Helper()
			result, ok := responses[idx]["result"].(map[string]any)
			if !ok {
				t.Fatalf("response %d: missing result: %v", idx, responses[idx])
			}
			content, ok := result["content"].([]any)
			if !ok || len(content) == 0 {
				t.Fatalf("response %d: missing content: %v", idx, result)
			}
			first, ok := content[0].(map[string]any)
			if !ok {
				t.Fatalf("response %d: content[0] wrong shape: %v", idx, content[0])
			}
			text, ok := first["text"].(string)
			if !ok {
				t.Fatalf("response %d: content[0].text not a string: %v", idx, first)
			}
			return text
		}

		// Response 1: list all — 3 entries with the expected slugs.
		listAllText := extractText(1)
		var listAll []map[string]any
		if err := json.Unmarshal([]byte(listAllText), &listAll); err != nil {
			t.Fatalf("vv_list_learnings (all): invalid JSON in text: %v\ntext: %s", err, listAllText)
		}
		if len(listAll) != 3 {
			t.Errorf("vv_list_learnings (all): expected 3 entries, got %d (%v)", len(listAll), listAll)
		}
		gotSlugs := make(map[string]bool)
		for _, entry := range listAll {
			slug, _ := entry["slug"].(string)
			gotSlugs[slug] = true
		}
		wantSlugs := []string{"sample-user", "sample-feedback", "sample-reference"}
		for _, slug := range wantSlugs {
			if !gotSlugs[slug] {
				t.Errorf("vv_list_learnings (all): missing slug %q; got %v", slug, gotSlugs)
			}
		}
		if len(gotSlugs) != len(wantSlugs) {
			t.Errorf("vv_list_learnings (all): slug set mismatch, got %v want %v", gotSlugs, wantSlugs)
		}

		// Response 2: list filtered by feedback — 1 entry, slug sample-feedback.
		listFilteredText := extractText(2)
		var listFiltered []map[string]any
		if err := json.Unmarshal([]byte(listFilteredText), &listFiltered); err != nil {
			t.Fatalf("vv_list_learnings (feedback): invalid JSON in text: %v\ntext: %s", err, listFilteredText)
		}
		if len(listFiltered) != 1 {
			t.Fatalf("vv_list_learnings (feedback): expected 1 entry, got %d (%v)", len(listFiltered), listFiltered)
		}
		if slug, _ := listFiltered[0]["slug"].(string); slug != "sample-feedback" {
			t.Errorf("vv_list_learnings (feedback): slug = %q, want sample-feedback", slug)
		}

		// Response 3: get sample-user — full body + metadata.
		getText := extractText(3)
		var getParsed map[string]any
		if err := json.Unmarshal([]byte(getText), &getParsed); err != nil {
			t.Fatalf("vv_get_learning: invalid JSON in text: %v\ntext: %s", err, getText)
		}
		if slug, _ := getParsed["slug"].(string); slug != "sample-user" {
			t.Errorf("vv_get_learning: slug = %v, want sample-user", getParsed["slug"])
		}
		if name, _ := getParsed["name"].(string); name != "Sample User Learning" {
			t.Errorf("vv_get_learning: name = %v, want Sample User Learning", getParsed["name"])
		}
		body, _ := getParsed["content"].(string)
		if !strings.Contains(body, "Body content for the user-type sample learning.") {
			t.Errorf("vv_get_learning: body missing expected text; got %q", body)
		}

		// Response 4: get unknown slug — isError=true.
		missResult, missOK := responses[4]["result"].(map[string]any)
		if !missOK {
			t.Fatalf("vv_get_learning (missing): missing result: %v", responses[4])
		}
		if missResult["isError"] != true {
			t.Errorf("vv_get_learning (missing): expected isError=true, got %v", missResult["isError"])
		}

		// Stderr should show the tool calls.
		assertContains(t, stderr, "tools/call: vv_list_learnings", "stderr log")
		assertContains(t, stderr, "tools/call: vv_get_learning", "stderr log")
	})

	// 14. MCP vv_update_resume v10 Current-State guard (stdio transport).
	//
	// Exercises the same guard covered by the unit tests in
	// internal/mcp/tools_context_write_test.go, but over the real
	// vv mcp subprocess with NDJSON over stdin/stdout. The ctx-project
	// agentctx was seeded at v10 by the earlier context_init_and_migrate
	// subtest.
	t.Run("mcp_update_resume_guard", func(t *testing.T) {
		// Build tools/call arguments via json.Marshal so we never hand-write
		// escapes into the request line (matches updateResumePayload in
		// tools_context_write_test.go).
		updateCall := func(id int, section, content string) string {
			args := struct {
				Project string `json:"project"`
				Section string `json:"section"`
				Content string `json:"content"`
			}{"ctx-project", section, content}
			argsJSON, err := json.Marshal(args)
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}
			params := map[string]any{
				"name":      "vv_update_resume",
				"arguments": json.RawMessage(argsJSON),
			}
			paramsJSON, err := json.Marshal(params)
			if err != nil {
				t.Fatalf("marshal params: %v", err)
			}
			req := map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"method":  "tools/call",
				"params":  json.RawMessage(paramsJSON),
			}
			reqJSON, err := json.Marshal(req)
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}
			return string(reqJSON)
		}

		validBullets := "- **Tests:** 1459 across 36 packages.\n- **Lint:** clean.\n"
		narrativeBody := "This is a paragraph of project narrative that does not belong here."
		frobBody := "- **Frobnicator:** enabled.\n"
		openThreadsBody := "- arbitrary narrative line\n"

		requests := strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			updateCall(2, "Current State", validBullets),
			updateCall(3, "Current State", narrativeBody),
			updateCall(4, "Current State", frobBody),
			updateCall(5, "Open Threads", openThreadsBody),
		}, "\n")

		stdout, stderr, err := runVVWithStdin(t, env, requests, "mcp")
		if err != nil {
			t.Fatalf("vv mcp failed: %v\nstdout: %s\nstderr: %s", err, stdout, stderr)
		}

		trimmed := strings.TrimSpace(stdout)
		var lines []string
		for line := range strings.SplitSeq(trimmed, "\n") {
			lines = append(lines, line)
		}
		// 5 requests with an id → 5 responses; the notification is silent.
		if len(lines) != 5 {
			t.Fatalf("expected 5 response lines, got %d:\n%s", len(lines), stdout)
		}

		var responses []map[string]any
		for i, line := range lines {
			var resp map[string]any
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				t.Fatalf("response %d: invalid JSON: %v\nline: %s", i, err, line)
			}
			responses = append(responses, resp)
		}

		// Response 0: initialize — must have serverInfo.
		initResult, initOK := responses[0]["result"].(map[string]any)
		if !initOK {
			t.Fatalf("initialize: missing result: %v", responses[0])
		}
		if initResult["serverInfo"] == nil {
			t.Error("initialize: missing serverInfo")
		}

		// Helper to pull content[0].text from a tool/call response.
		extractText := func(idx int) string {
			t.Helper()
			result, ok := responses[idx]["result"].(map[string]any)
			if !ok {
				t.Fatalf("response %d: missing result: %v", idx, responses[idx])
			}
			content, ok := result["content"].([]any)
			if !ok || len(content) == 0 {
				t.Fatalf("response %d: missing content: %v", idx, result)
			}
			first, ok := content[0].(map[string]any)
			if !ok {
				t.Fatalf("response %d: content[0] wrong shape: %v", idx, content[0])
			}
			text, ok := first["text"].(string)
			if !ok {
				t.Fatalf("response %d: content[0].text not a string: %v", idx, first)
			}
			return text
		}

		// Response 1: valid invariant bullets — success, disk updated.
		validResult, ok := responses[1]["result"].(map[string]any)
		if !ok {
			t.Fatalf("valid Current State: missing result: %v", responses[1])
		}
		if isErr, _ := validResult["isError"].(bool); isErr {
			t.Errorf("valid Current State: unexpected isError=true; text=%q", extractText(1))
		}
		resumePath := filepath.Join(vaultPath, "Projects", "ctx-project", "agentctx", "resume.md")
		resumeAfterValid := readFile(t, resumePath)
		assertContains(t, resumeAfterValid, "- **Tests:** 1459 across 36 packages.", "resume.md gained Tests bullet")
		assertContains(t, resumeAfterValid, "- **Lint:** clean.", "resume.md gained Lint bullet")
		assertContains(t, resumeAfterValid, "## Current State", "Current State heading preserved")

		// Response 2: narrative body — isError=true, text points to features.md.
		narrResult, ok := responses[2]["result"].(map[string]any)
		if !ok {
			t.Fatalf("narrative rejection: missing result: %v", responses[2])
		}
		if narrResult["isError"] != true {
			t.Errorf("narrative rejection: expected isError=true, got %v", narrResult["isError"])
		}
		narrText := extractText(2)
		assertContains(t, narrText, "features.md", "narrative rejection points to features.md")

		// Response 3: non-whitelisted KEY bullet — isError=true, cites the key.
		frobResult, ok := responses[3]["result"].(map[string]any)
		if !ok {
			t.Fatalf("Frobnicator rejection: missing result: %v", responses[3])
		}
		if frobResult["isError"] != true {
			t.Errorf("Frobnicator rejection: expected isError=true, got %v", frobResult["isError"])
		}
		frobText := extractText(3)
		assertContains(t, frobText, "Frobnicator", "Frobnicator rejection names the offending key")

		// Response 4: Open Threads — v10 guard does not fire outside Current State.
		openResult, ok := responses[4]["result"].(map[string]any)
		if !ok {
			t.Fatalf("Open Threads happy path: missing result: %v", responses[4])
		}
		if isErr, _ := openResult["isError"].(bool); isErr {
			t.Errorf("Open Threads happy path: unexpected isError=true; text=%q", extractText(4))
		}
		resumeAfterOpen := readFile(t, resumePath)
		assertContains(t, resumeAfterOpen, "- arbitrary narrative line", "Open Threads section accepted narrative bullet")
		assertContains(t, resumeAfterOpen, "## Open Threads", "Open Threads heading preserved")
		// The valid Current State body from response 1 must still be present —
		// rejected writes (responses 2 & 3) must not have clobbered disk, and
		// the Open Threads write must not have touched Current State.
		assertContains(t, resumeAfterOpen, "- **Tests:** 1459 across 36 packages.", "Current State bullet still on disk after later writes")

		// Stderr should show the tool calls.
		assertContains(t, stderr, "tools/call: vv_update_resume", "stderr log")
	})

	// provenance_in_vault_writes exercises the three provenance touchpoints
	// (vv_append_iteration trailer, vv_get_iterations strip, vv_capture_session
	// YAML frontmatter) end-to-end through the stdio MCP harness. All three
	// touchpoints resolve host/user via meta.Stamp(), which this subtest pins
	// to deterministic values via VIBE_VAULT_HOSTNAME and USER/LOGNAME.
	//
	// ctx-project was scaffolded at v10 by context_init_and_migrate, so
	// {vaultPath}/Projects/ctx-project/agentctx/iterations.md already exists
	// from the template and is safe to append to.
	t.Run("provenance_in_vault_writes", func(t *testing.T) {
		provHome := t.TempDir()
		provEnv := buildEnvWithHomeUser(xdgConfigHome, provHome, "vibe-vault-user")

		// --- (1) vv_append_iteration: trailer must land on disk ---
		appendArgs, err := json.Marshal(struct {
			Project   string `json:"project"`
			Title     string `json:"title"`
			Narrative string `json:"narrative"`
		}{
			Project:   "ctx-project",
			Title:     "Provenance integration check",
			Narrative: "Body of the iteration narrative for provenance test.",
		})
		if err != nil {
			t.Fatalf("marshal append args: %v", err)
		}
		appendParams, err := json.Marshal(map[string]any{
			"name":      "vv_append_iteration",
			"arguments": json.RawMessage(appendArgs),
		})
		if err != nil {
			t.Fatalf("marshal append params: %v", err)
		}
		appendReq, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params":  json.RawMessage(appendParams),
		})
		if err != nil {
			t.Fatalf("marshal append request: %v", err)
		}

		// vv_get_iterations for the same project in full format so we can
		// verify the parser-side strip.
		getArgs, err := json.Marshal(struct {
			Project string `json:"project"`
			Format  string `json:"format"`
			Limit   int    `json:"limit"`
		}{
			Project: "ctx-project",
			Format:  "full",
			Limit:   5,
		})
		if err != nil {
			t.Fatalf("marshal get args: %v", err)
		}
		getParams, err := json.Marshal(map[string]any{
			"name":      "vv_get_iterations",
			"arguments": json.RawMessage(getArgs),
		})
		if err != nil {
			t.Fatalf("marshal get params: %v", err)
		}
		getReq, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      3,
			"method":  "tools/call",
			"params":  json.RawMessage(getParams),
		})
		if err != nil {
			t.Fatalf("marshal get request: %v", err)
		}

		iterRequests := strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			string(appendReq),
			string(getReq),
		}, "\n")

		iterStdout, iterStderr, err := runVVWithStdin(t, provEnv, iterRequests, "mcp")
		if err != nil {
			t.Fatalf("vv mcp (iteration path) failed: %v\nstdout: %s\nstderr: %s",
				err, iterStdout, iterStderr)
		}

		iterLines := strings.Split(strings.TrimSpace(iterStdout), "\n")
		if len(iterLines) != 3 {
			t.Fatalf("iteration path: expected 3 response lines, got %d:\n%s",
				len(iterLines), iterStdout)
		}
		var iterResponses []map[string]any
		for i, line := range iterLines {
			var resp map[string]any
			if jerr := json.Unmarshal([]byte(line), &resp); jerr != nil {
				t.Fatalf("iteration response %d: invalid JSON: %v\nline: %s", i, jerr, line)
			}
			iterResponses = append(iterResponses, resp)
		}

		// Response 1: vv_append_iteration success.
		appendResult, ok := iterResponses[1]["result"].(map[string]any)
		if !ok {
			t.Fatalf("vv_append_iteration: missing result: %v", iterResponses[1])
		}
		if isErr, _ := appendResult["isError"].(bool); isErr {
			t.Fatalf("vv_append_iteration: unexpected isError=true: %v", appendResult)
		}

		// Read iterations.md from disk (NOT via vv_get_iterations — the strip
		// would hide exactly what we're asserting).
		iterPath := filepath.Join(vaultPath, "Projects", "ctx-project", "agentctx", "iterations.md")
		iterBody := readFile(t, iterPath)
		// Phase 6.1 widened the trailer to the four-token "host user cwd origin"
		// shape. Under buildEnvWithHomeUser the sentinels produce:
		//   - host: VIBE_VAULT_HOSTNAME="vibe-vault-test"
		//   - user: USER="vibe-vault-user"
		//   - cwd:  VIBE_VAULT_CWD="/vibe-vault-test-cwd" (SanitizeCWDForEmit
		//     leaves absolute non-home paths unchanged; not inside the test vault)
		//   - origin: DetectProject("/vibe-vault-test-cwd") → "vibe-vault-test-cwd"
		//     (basename fallback — no identity file, no git remote)
		wantTrailer := "<!-- recorded: host=vibe-vault-test user=vibe-vault-user cwd=/vibe-vault-test-cwd origin=vibe-vault-test-cwd -->"
		if !strings.Contains(iterBody, wantTrailer) {
			t.Errorf("iterations.md missing provenance trailer %q:\n%s", wantTrailer, iterBody)
		}
		trimmedIter := strings.TrimRight(iterBody, "\n")
		if !strings.HasSuffix(trimmedIter, wantTrailer) {
			tail := trimmedIter
			if len(tail) > 200 {
				tail = tail[len(tail)-200:]
			}
			t.Errorf("iterations.md: trailer not at end-of-file; tail = %q", tail)
		}

		// Response 2: vv_get_iterations narrative must NOT contain the trailer.
		getResult, ok := iterResponses[2]["result"].(map[string]any)
		if !ok {
			t.Fatalf("vv_get_iterations: missing result: %v", iterResponses[2])
		}
		getContent, ok := getResult["content"].([]any)
		if !ok || len(getContent) == 0 {
			t.Fatalf("vv_get_iterations: missing content: %v", getResult)
		}
		getText, _ := getContent[0].(map[string]any)["text"].(string)
		var getParsed struct {
			Iterations []struct {
				Number    int    `json:"number"`
				Title     string `json:"title"`
				Narrative string `json:"narrative"`
			} `json:"iterations"`
		}
		if jerr := json.Unmarshal([]byte(getText), &getParsed); jerr != nil {
			t.Fatalf("vv_get_iterations: invalid JSON: %v\ntext: %s", jerr, getText)
		}
		var foundOurs bool
		for _, it := range getParsed.Iterations {
			if it.Title == "Provenance integration check" {
				foundOurs = true
				// Parser-strip gate: the narrative returned by vv_get_iterations
				// must carry zero provenance leakage against the four-token
				// trailer shape. Each leak token is a distinct regression class:
				//   - "<!-- recorded:" — whole-trailer strip failure
				//   - "cwd=" / "origin=" — strip matched but truncated partway
				//   - "VIBE_VAULT_CWD" — env var name leaked (would indicate the
				//     stamper swallowed its own key, not plausible but cheap gate)
				//   - "vibe-vault-test-cwd" — the sentinel path or origin value
				//     escaped the strip region.
				leakTokens := []string{
					"<!-- recorded:",
					"cwd=",
					"origin=",
					"VIBE_VAULT_CWD",
					"vibe-vault-test-cwd",
				}
				for _, leak := range leakTokens {
					if strings.Contains(it.Narrative, leak) {
						t.Errorf("vv_get_iterations: narrative leaked %q after strip: %q",
							leak, it.Narrative)
					}
				}
				if !strings.Contains(it.Narrative,
					"Body of the iteration narrative for provenance test.") {
					t.Errorf("vv_get_iterations: narrative body missing expected text: %q",
						it.Narrative)
				}
				break
			}
		}
		if !foundOurs {
			t.Errorf("vv_get_iterations: newly appended iteration not returned; got %d entries",
				len(getParsed.Iterations))
		}

		// --- (2) vv_capture_session: frontmatter must carry host/user ---
		// vv_capture_session resolves the project via session.DetectProject(cwd).
		// Use a tempdir whose basename matches a distinct test-owned project
		// name so the session note lands at a predictable vault path.
		captureParent := t.TempDir()
		captureProject := "provenance-capture"
		captureCwd := filepath.Join(captureParent, captureProject)
		if merr := os.MkdirAll(captureCwd, 0o755); merr != nil {
			t.Fatalf("mkdir capture cwd: %v", merr)
		}

		capArgs, err := json.Marshal(struct {
			Summary string `json:"summary"`
			Tag     string `json:"tag"`
		}{
			Summary: "Provenance capture smoke test. Confirms host and user are stamped on the session note YAML frontmatter.",
			Tag:     "review",
		})
		if err != nil {
			t.Fatalf("marshal capture args: %v", err)
		}
		capParams, err := json.Marshal(map[string]any{
			"name":      "vv_capture_session",
			"arguments": json.RawMessage(capArgs),
		})
		if err != nil {
			t.Fatalf("marshal capture params: %v", err)
		}
		capReq, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "tools/call",
			"params":  json.RawMessage(capParams),
		})
		if err != nil {
			t.Fatalf("marshal capture request: %v", err)
		}

		capRequests := strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
			`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			string(capReq),
		}, "\n")

		// Inline subprocess invocation so we can pin cmd.Dir (the existing
		// runVVWithStdin helper does not accept a working directory, and the
		// brief forbids adding new test helpers).
		capCmd := exec.Command(vvBinary, "mcp")
		capCmd.Env = provEnv
		capCmd.Dir = captureCwd
		capCmd.Stdin = strings.NewReader(capRequests)
		var capOut, capErr strings.Builder
		capCmd.Stdout = &capOut
		capCmd.Stderr = &capErr
		if rerr := capCmd.Run(); rerr != nil {
			t.Fatalf("vv mcp (capture path) failed: %v\nstdout: %s\nstderr: %s",
				rerr, capOut.String(), capErr.String())
		}

		capLines := strings.Split(strings.TrimSpace(capOut.String()), "\n")
		if len(capLines) != 2 {
			t.Fatalf("capture path: expected 2 response lines, got %d:\n%s",
				len(capLines), capOut.String())
		}
		var capResp map[string]any
		if jerr := json.Unmarshal([]byte(capLines[1]), &capResp); jerr != nil {
			t.Fatalf("vv_capture_session: invalid response JSON: %v\nline: %s",
				jerr, capLines[1])
		}
		capResult, ok := capResp["result"].(map[string]any)
		if !ok {
			t.Fatalf("vv_capture_session: missing result: %v", capResp)
		}
		if isErr, _ := capResult["isError"].(bool); isErr {
			t.Fatalf("vv_capture_session: unexpected isError=true: %v", capResult)
		}
		capContent, ok := capResult["content"].([]any)
		if !ok || len(capContent) == 0 {
			t.Fatalf("vv_capture_session: missing content: %v", capResult)
		}
		capText, _ := capContent[0].(map[string]any)["text"].(string)
		var capParsed struct {
			Status   string `json:"status"`
			Project  string `json:"project"`
			NotePath string `json:"note_path"`
		}
		if jerr := json.Unmarshal([]byte(capText), &capParsed); jerr != nil {
			t.Fatalf("vv_capture_session: invalid text JSON: %v\ntext: %s", jerr, capText)
		}
		if capParsed.Status != "captured" {
			t.Fatalf("vv_capture_session: status=%q want captured; full response: %s",
				capParsed.Status, capText)
		}
		if capParsed.Project != captureProject {
			t.Errorf("vv_capture_session: project=%q want %q",
				capParsed.Project, captureProject)
		}

		// note_path may be absolute or relative to vault — normalize.
		notePath := capParsed.NotePath
		if !filepath.IsAbs(notePath) {
			notePath = filepath.Join(vaultPath, notePath)
		}
		noteBody := readFile(t, notePath)

		// Verify YAML frontmatter carries host + user, both appearing before
		// the summary: line (which marks the tail of our stamped fields).
		fmEnd := strings.Index(noteBody, "\n---\n")
		if fmEnd < 0 {
			t.Fatalf("session note missing YAML frontmatter terminator:\n%s", noteBody)
		}
		frontmatter := noteBody[:fmEnd]
		summaryIdx := strings.Index(frontmatter, "\nsummary:")
		if summaryIdx < 0 {
			t.Fatalf("session note frontmatter missing summary: key:\n%s", frontmatter)
		}
		beforeSummary := frontmatter[:summaryIdx]
		if !strings.Contains(beforeSummary, "host: vibe-vault-test") {
			t.Errorf("session note: host: line missing or after summary:; frontmatter:\n%s",
				frontmatter)
		}
		if !strings.Contains(beforeSummary, "user: vibe-vault-user") {
			t.Errorf("session note: user: line missing or after summary:; frontmatter:\n%s",
				frontmatter)
		}
		// Phase 6.1 extension: cwd: and origin_project: are emitted between
		// user: and summary: in the NoteData → SessionNote rendering block.
		// Under buildEnvWithHomeUser the VIBE_VAULT_CWD sentinel pins cwd to
		// "/vibe-vault-test-cwd" (SanitizeCWDForEmit leaves absolute paths
		// outside $HOME and outside cfg.VaultPath unchanged) and DetectProject
		// resolves that path to "vibe-vault-test-cwd" via basename fallback.
		if !strings.Contains(beforeSummary, "cwd: /vibe-vault-test-cwd") {
			t.Errorf("session note: cwd: line missing or after summary:; frontmatter:\n%s",
				frontmatter)
		}
		if !strings.Contains(beforeSummary, "origin_project: vibe-vault-test-cwd") {
			t.Errorf("session note: origin_project: line missing or after summary:; frontmatter:\n%s",
				frontmatter)
		}

		assertContains(t, iterStderr, "tools/call: vv_append_iteration", "iter stderr log")
		assertContains(t, iterStderr, "tools/call: vv_get_iterations", "iter stderr log")
		assertContains(t, capErr.String(), "tools/call: vv_capture_session", "capture stderr log")
	})

	// context_sync_t1_t2_cascade verifies that `vv context sync` refreshes
	// the Tier-2 vault template cache from the embedded Tier-1 source.
	//
	// The code under test is forceUpdateVaultTemplates
	// (internal/context/sync.go:874), which overwrites every file under
	// {vaultPath}/Templates/agentctx/ with content from BuiltinTemplates()
	// on every syncProject invocation. We exercise that path through the
	// real CLI: corrupt the Tier-2 cache file, run `vv context sync`, and
	// byte-compare against the embedded FS source.
	//
	// Earlier subtests (context_init_and_migrate, context_sync) already
	// populated the Tier-2 cache via their init/sync invocations, so we
	// can corrupt the file directly without pre-seeding.
	t.Run("context_sync_t1_t2_cascade", func(t *testing.T) {
		// Corrupt the Tier-2 cache file with a sentinel.
		cascadeTarget := filepath.Join(vaultPath, "Templates", "agentctx", "commands", "restart.md")
		if err := os.WriteFile(cascadeTarget, []byte("STALE-T2-CACHE\n"), 0o644); err != nil {
			t.Fatalf("corrupt tier-2 cache: %v", err)
		}

		// `vv context sync` (without --all) requires a .vibe-vault.toml
		// marker in cwd or an ancestor — see cmd/vv/main.go:323-324.
		cascadeCwd := t.TempDir()
		writeFixture(t, cascadeCwd, ".vibe-vault.toml", "# vibe-vault project marker\n")

		// ctx-project was scaffolded at v10 by context_init_and_migrate.
		mustRunVVInDir(t, env, cascadeCwd, "context", "sync", "--project", "ctx-project")

		// Tier-2 cache must now be byte-equal to the embedded Tier-1 source.
		got, err := os.ReadFile(cascadeTarget)
		if err != nil {
			t.Fatalf("read tier-2 after sync: %v", err)
		}
		want, err := templates.AgentctxFS().ReadFile("agentctx/commands/restart.md")
		if err != nil {
			t.Fatalf("read embedded source: %v", err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("tier-2 restart.md not byte-equal to embedded source\nwant %d bytes:\n%s\n\ngot %d bytes:\n%s",
				len(want), string(want), len(got), string(got))
		}

		// Sanity: the stale sentinel must be gone.
		if bytes.Contains(got, []byte("STALE-T2-CACHE")) {
			t.Error("tier-2 cache still contains stale sentinel after sync")
		}
	})

	// memory_link_cli exercises the cmd/vv dispatch layer for
	// `vv memory link` and `vv memory unlink`: flag parsing, config
	// loading, and reaching the library. Library-level writethrough /
	// inode / vault-preservation invariants live in
	// internal/memory/memory_integration_test.go and are not repeated
	// here.
	//
	// We sandbox HOME per-subtest via buildEnvWithHome so the link
	// lands under a tempdir rather than the developer's real
	// ~/.claude/ directory.
	t.Run("memory_link_cli", func(t *testing.T) {
		fakeHome := t.TempDir()
		e1Env := buildEnvWithHome(xdgConfigHome, fakeHome)
		projectRepo := t.TempDir()

		// Scaffold the vault-side agentctx tree by running
		// `vv context init` — memory.Link requires
		// {vaultPath}/Projects/<project>/agentctx to exist.
		mustRunVVInDir(t, e1Env, projectRepo, "context", "init", "--project", "memory-cli-demo")

		// memory.Link resolves the project via session.DetectProject,
		// which reads .vibe-vault.toml. The template shipped by
		// `context init` leaves `name = ...` commented out, so detection
		// would fall back to the tempdir basename (e.g. "001"). Enable
		// the name line so DetectProject returns "memory-cli-demo",
		// matching the scaffolded agentctx directory.
		markerPath := filepath.Join(projectRepo, ".vibe-vault.toml")
		markerRaw, mErr := os.ReadFile(markerPath)
		if mErr != nil {
			t.Fatalf("read marker: %v", mErr)
		}
		enabled := strings.Replace(
			string(markerRaw),
			`# name = "memory-cli-demo"`,
			`name = "memory-cli-demo"`,
			1,
		)
		if enabled == string(markerRaw) {
			t.Fatalf("marker did not contain expected commented name line:\n%s", string(markerRaw))
		}
		if err := os.WriteFile(markerPath, []byte(enabled), 0o644); err != nil {
			t.Fatalf("rewrite marker: %v", err)
		}

		homeProjects := filepath.Join(fakeHome, ".claude", "projects")

		// Dry-run link: reports but creates no symlink.
		dryStdout := mustRunVV(t, e1Env, "memory", "link", "--working-dir", projectRepo, "--dry-run")
		assertContains(t, dryStdout, "(dry-run)", "dry-run label in stdout")
		assertContains(t, dryStdout, "memory-cli-demo", "dry-run mentions project")

		if dirExists(homeProjects) {
			entries, _ := os.ReadDir(homeProjects)
			for _, entry := range entries {
				candidate := filepath.Join(homeProjects, entry.Name(), "memory")
				if isSymlink(candidate) {
					t.Errorf("dry-run created symlink at %s", candidate)
				}
			}
		}

		// Real link: must create a symlink under
		// {fakeHome}/.claude/projects/<slug>/memory pointing at
		// {vaultPath}/Projects/memory-cli-demo/agentctx/memory.
		linkStdout := mustRunVV(t, e1Env, "memory", "link", "--working-dir", projectRepo)
		assertContains(t, linkStdout, "memory-cli-demo", "link mentions project")

		entries, err := os.ReadDir(homeProjects)
		if err != nil {
			t.Fatalf("read projects dir: %v", err)
		}
		var linkPath string
		for _, entry := range entries {
			candidate := filepath.Join(homeProjects, entry.Name(), "memory")
			if isSymlink(candidate) {
				if linkPath != "" {
					t.Fatalf("multiple memory symlinks found: %s and %s", linkPath, candidate)
				}
				linkPath = candidate
			}
		}
		if linkPath == "" {
			t.Fatal("no memory symlink created under fakeHome/.claude/projects/")
		}

		target, err := os.Readlink(linkPath)
		if err != nil {
			t.Fatalf("readlink %s: %v", linkPath, err)
		}
		wantTarget := filepath.Join(vaultPath, "Projects", "memory-cli-demo", "agentctx", "memory")
		if target != wantTarget {
			// On systems where /tmp is a symlink (e.g. /tmp -> /private/tmp
			// on macOS), EvalSymlinks of the intended target may differ
			// from the raw join.
			resolved, rerr := filepath.EvalSymlinks(wantTarget)
			if rerr != nil || target != resolved {
				t.Errorf("symlink target = %s, want %s (or resolved %s)", target, wantTarget, resolved)
			}
		}

		// Unlink: symlink must be gone afterwards.
		unlinkStdout := mustRunVV(t, e1Env, "memory", "unlink", "--working-dir", projectRepo)
		assertContains(t, unlinkStdout, "memory-cli-demo", "unlink mentions project")

		if isSymlink(linkPath) {
			t.Error("symlink still present after unlink")
		}
	})

	t.Run("vault_push_multi_remote", func(t *testing.T) {
		// Fully isolated from the shared vaultPath/xdgConfigHome.
		e2Vault := t.TempDir()
		e2Xdg := t.TempDir()
		e2Home := t.TempDir()
		e2Env := buildEnvWithHome(e2Xdg, e2Home)
		// vaultsync.CommitAndPush shells out to `git commit`, which needs
		// an identity. The sandboxed HOME has no .gitconfig, so inject
		// identity via process env so git picks it up regardless of
		// system gitconfig.
		e2Env = append(e2Env,
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)

		// Create the config pointing at e2Vault.
		mustRunVV(t, e2Env, "init", e2Vault)

		// Make the vault directory a git repo with an initial commit so
		// CommitAndPush has a clean history to push.
		gitx.GitRun(t, e2Vault, "init", "-b", "main")
		gitx.GitRun(t, e2Vault, "add", ".")
		gitx.GitRun(t, e2Vault, "commit", "-m", "initial vault commit")

		// Two bare remotes.
		githubRemote := gitx.InitBareRemote(t)
		vaultRemote := gitx.InitBareRemote(t)
		gitx.AddRemote(t, e2Vault, "github", githubRemote)
		gitx.AddRemote(t, e2Vault, "vault", vaultRemote)

		// --- Happy path: stage a change, push, both remotes advance ---
		writeFixture(t, e2Vault, "change-one.txt", "first change\n")

		happyStdout := mustRunVV(t, e2Env, "vault", "push", "--message", "first change")
		assertContains(t, happyStdout, "committed and pushed to 2 remote(s)",
			"happy path: stdout reports push to 2 remotes")

		// Both bare remotes must have advanced to the same SHA.
		githubSHA1 := strings.TrimSpace(gitx.GitRun(t, githubRemote, "rev-parse", "refs/heads/main"))
		vaultSHA1 := strings.TrimSpace(gitx.GitRun(t, vaultRemote, "rev-parse", "refs/heads/main"))
		if githubSHA1 == "" {
			t.Error("github remote: refs/heads/main did not advance")
		}
		if vaultSHA1 == "" {
			t.Error("vault remote: refs/heads/main did not advance")
		}
		if githubSHA1 != vaultSHA1 {
			t.Errorf("remotes diverged after push: github=%s vault=%s", githubSHA1, vaultSHA1)
		}

		// --- Partial failure: remove vaultRemote, stage another change, push ---
		if err := os.RemoveAll(vaultRemote); err != nil {
			t.Fatalf("remove vaultRemote: %v", err)
		}
		writeFixture(t, e2Vault, "change-two.txt", "second change\n")

		partialStdout, partialStderr, _ := runVV(t, e2Env, "vault", "push", "--message", "second change")
		// The `vault` remote push should fail; `github` should still succeed.
		assertContains(t, partialStdout, "partial push", "partial failure branch in stdout")
		assertContains(t, partialStdout, "github: ok", "github marked ok")
		assertContains(t, partialStdout, "vault: FAILED", "vault marked FAILED")

		// Sanity: github advanced; its new HEAD's parent is the first commit.
		githubSHA2 := strings.TrimSpace(gitx.GitRun(t, githubRemote, "rev-parse", "refs/heads/main"))
		if githubSHA2 == githubSHA1 {
			t.Error("github remote did not advance on partial-push run")
		}
		parent := strings.TrimSpace(gitx.GitRun(t, githubRemote, "rev-parse", "refs/heads/main^"))
		if parent != githubSHA1 {
			t.Errorf("github HEAD^ = %s, want %s (the first commit)", parent, githubSHA1)
		}

		_ = partialStderr // stderr may carry push progress lines; not asserted
	})

	// no_real_vault_mutation: Phase-0 canary. Re-snapshot the operator's real
	// vault and real config, diff against the pre-run snapshot, and fail with a
	// human-readable listing if any protected path was mutated by the preceding
	// subtests. Must run LAST in the shared-vault chain.
	t.Run("no_real_vault_mutation", func(t *testing.T) {
		if preCanarySnapshot == nil {
			t.Skip("canary pre-snapshot not taken (no operator config)")
		}
		postSnapshot, _, _ := vaultCanarySnapshotAt(t, canaryRealConfigPath, canaryRealVault)
		diffs := vaultCanaryDiff(preCanarySnapshot, postSnapshot)
		if len(diffs) != 0 {
			var b strings.Builder
			fmt.Fprintf(&b, "real vault was mutated by TestIntegration (%d path(s)):\n",
				len(diffs))
			fmt.Fprintf(&b, "  realVault:  %s\n", canaryRealVault)
			fmt.Fprintf(&b, "  realConfig: %s\n", canaryRealConfigPath)
			for _, d := range diffs {
				b.WriteString("  - ")
				b.WriteString(d)
				b.WriteString("\n")
			}
			t.Fatal(b.String())
		}

		// Post-snapshot sentinel grep: catches writes to real-vault paths
		// that fell outside the protected-roots list above (e.g. a brand
		// new Project directory created mid-run, or a stray markdown file
		// dropped outside any snapshotted root). Subtests that stamp
		// provenance run under VIBE_VAULT_HOSTNAME=vibe-vault-test and
		// USER=vibe-vault-user, so any match on the real vault outside
		// test-owned project subtrees indicates a HOME/XDG-sandbox escape.
		//
		// This grep is complementary to the mtime/sha snapshot: the
		// snapshot would have caught mutations to files that already
		// existed; the grep catches net-new files the snapshot never
		// enumerated.
		if canaryRealVault == "" {
			return
		}
		// Line-anchored patterns matching ONLY what the provenance writers
		// produce: a bare `host: vibe-vault-test` YAML line, or a full
		// iteration trailer starting at column 0. Human-authored prose
		// that quotes these strings (in code spans, inline, indented)
		// doesn't match — verified against the real vault at commit time.
		// This keeps the entire vault watched without directory-level
		// blind spots.
		//
		// Phase 6.4 adds complementary cwd-sentinel patterns for the
		// VIBE_VAULT_CWD="/vibe-vault-test-cwd" stamp. Any leak now trips
		// at least one of three independent signals: mtime/sha snapshot,
		// hostname grep, cwd grep. The cwd patterns mirror the hostname
		// patterns' anchoring (line-anchored, column-0) so quoted prose
		// in human-authored docs doesn't false-positive.
		sentinelREs := []*regexp.Regexp{
			regexp.MustCompile(`(?m)^host: vibe-vault-test\b`),
			regexp.MustCompile(`(?m)^<!--\s*recorded:\s*host=vibe-vault-test\b`),
			regexp.MustCompile(`(?m)^cwd:\s*/vibe-vault-test-cwd\b`),
			regexp.MustCompile(`(?m)^<!--\s*recorded:[^\n]*cwd=/vibe-vault-test-cwd\b`),
		}
		// Reuse the same protected-root list the snapshot pass walked.
		// Any sentinel hit under these roots is already accounted for by
		// the snapshot — suppress it here so we only flag true escapes.
		skipRoots := canaryProtectedRoots(canaryRealVault)
		// Extensions that are obviously binary and not worth reading.
		skipExts := map[string]struct{}{
			".zst": {}, ".gz": {}, ".png": {}, ".jpg": {}, ".jpeg": {},
			".gif": {}, ".webp": {}, ".pdf": {}, ".bin": {}, ".so": {},
			".dylib": {}, ".a": {}, ".o": {},
		}
		const maxReadBytes = 512 * 1024

		_ = filepath.WalkDir(canaryRealVault, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if d.IsDir() {
				// Skip VCS / editor metadata.
				if strings.HasPrefix(d.Name(), ".git") || d.Name() == ".obsidian" {
					return filepath.SkipDir
				}
				// Skip protected test-owned subtrees — snapshot pass covers them.
				for _, p := range skipRoots {
					if path == p || strings.HasPrefix(path, p+string(filepath.Separator)) {
						return filepath.SkipDir
					}
				}
				return nil
			}
			info, err := d.Info()
			if err != nil || !info.Mode().IsRegular() {
				return nil
			}
			if _, skip := skipExts[strings.ToLower(filepath.Ext(path))]; skip {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			buf := make([]byte, maxReadBytes)
			n, _ := io.ReadFull(f, buf)
			_ = f.Close()
			data := buf[:n]
			for _, re := range sentinelREs {
				loc := re.FindIndex(data)
				if loc == nil {
					continue
				}
				rel, relErr := filepath.Rel(canaryRealVault, path)
				if relErr != nil {
					rel = path
				}
				idx := loc[0]
				lineStart := bytes.LastIndexByte(data[:idx], '\n') + 1
				lineEnd := bytes.IndexByte(data[idx:], '\n')
				var line string
				if lineEnd < 0 {
					line = string(data[lineStart:])
				} else {
					line = string(data[lineStart : idx+lineEnd])
				}
				t.Errorf("sentinel canary: pattern %q matched in real vault at %s: %q",
					re.String(), rel, strings.TrimSpace(line))
				break
			}
			return nil
		})
	})
}

// --- Canary helpers (Phase 0: detect leaks out of per-subtest sandboxes) ---

// canaryFileInfo captures the identity of a single regular file for
// comparison purposes.
type canaryFileInfo struct {
	relPath string
	mtime   time.Time
	sha256  string
}

// canarySnapshot maps absolute path roots to the set of files found under
// each root, keyed by relative path within the root. A nil entry for a given
// root means "the root did not exist at snapshot time, skip".
type canarySnapshot struct {
	// roots maps absolute root path → (relPath → canaryFileInfo). A root
	// whose value is a nil map was not present on disk; absent keys are
	// treated the same.
	roots map[string]map[string]canaryFileInfo
	// configFile is the snapshot of $HOME/.config/vibe-vault/config.toml, or
	// nil if the operator config was not present.
	configFile *canaryFileInfo
	// homeSingleFiles captures per-path snapshots of operator-private files
	// under $HOME that are category-C write targets (see the HOME-sandbox
	// classification comment near buildEnv). Keys are absolute paths; a nil
	// value means the file did not exist at snapshot time. Covers:
	//   - ~/.claude/settings.json            (hook.Install/Uninstall,
	//                                         hook.InstallMCP/UninstallMCP,
	//                                         hook.InstallClaudePlugin/Uninstall)
	//   - ~/.config/zed/settings.json        (hook.InstallMCPZed/Uninstall)
	//   - ~/.claude/plugins/known_marketplaces.json (plugin registry)
	//   - ~/.claude/plugins/installed_plugins.json  (plugin registry)
	homeSingleFiles map[string]*canaryFileInfo
}

// readOperatorConfigVaultPath parses the operator's real config directly —
// bypassing internal/config so we don't re-enter the env-aware loader whose
// behavior is itself under investigation in this task. Returns ("", "", nil)
// with no error if the config file does not exist (the canary should skip).
func readOperatorConfigVaultPath() (vaultPath, configPath string, err error) {
	home, err := os.UserHomeDir() //nolint:forbidigo // canary-real-home: reads operator's actual config path; sandboxing it would defeat the canary's purpose
	if err != nil {
		return "", "", fmt.Errorf("user home dir: %w", err)
	}
	cfgPath := filepath.Join(home, ".config", "vibe-vault", "config.toml")
	if _, statErr := os.Stat(cfgPath); os.IsNotExist(statErr) {
		return "", "", nil
	} else if statErr != nil {
		return "", "", fmt.Errorf("stat %s: %w", cfgPath, statErr)
	}
	var parsed struct {
		VaultPath string `toml:"vault_path"`
	}
	if _, err := toml.DecodeFile(cfgPath, &parsed); err != nil {
		return "", "", fmt.Errorf("decode %s: %w", cfgPath, err)
	}
	if parsed.VaultPath == "" {
		return "", cfgPath, fmt.Errorf("operator config %s has empty vault_path", cfgPath)
	}
	// Expand ~/ to the operator's home.
	if strings.HasPrefix(parsed.VaultPath, "~/") {
		parsed.VaultPath = filepath.Join(home, parsed.VaultPath[2:])
	}
	return parsed.VaultPath, cfgPath, nil
}

// canaryProtectedRoots returns the list of real-vault subtrees the canary
// watches. Globs (paths ending in "*") are expanded against the filesystem;
// non-glob paths are returned as-is. Missing paths are filtered out here
// rather than in the snapshot walker so the caller has a clean list.
func canaryProtectedRoots(realVault string) []string {
	patterns := []string{
		filepath.Join(realVault, "Projects", "ctx-project"),
		filepath.Join(realVault, "Projects", "myproject"),
		filepath.Join(realVault, "Projects", "narr-project"),
		filepath.Join(realVault, "Projects", "other-project"),
		filepath.Join(realVault, "Projects", "memory-cli-demo"),
		filepath.Join(realVault, "Projects", "sync-legacy"),
		filepath.Join(realVault, "Projects", "resume-invariants-*"),
	}
	var out []string
	for _, p := range patterns {
		if strings.Contains(p, "*") {
			matches, err := filepath.Glob(p)
			if err != nil {
				continue
			}
			out = append(out, matches...)
			continue
		}
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// canaryShouldSkipFile returns true for entries the canary must not monitor
// because they are written by non-test processes (Claude Code hooks, Obsidian
// sync) and would create false positives. The skip test is applied to the
// path relative to the root it was found under.
func canaryShouldSkipFile(rel string) bool {
	// Top-level iterations.md or history.md inside a watched root: skip.
	if rel == "iterations.md" || rel == "history.md" {
		return true
	}
	// Any sessions/*.md file (Claude Code hook output).
	if strings.HasPrefix(rel, "sessions"+string(filepath.Separator)) &&
		strings.HasSuffix(rel, ".md") {
		return true
	}
	return false
}

// canaryHomePrivateSingleFiles returns the list of absolute paths the canary
// monitors as single files under $HOME. These correspond to category-C write
// targets (see HOME-sandbox classification near buildEnv) — if any of these
// files appears, disappears, or has its contents change across a
// TestIntegration run, a subtest leaked writes to the operator's real
// config. Returns an empty slice if $HOME cannot be resolved.
func canaryHomePrivateSingleFiles() []string {
	home, err := os.UserHomeDir() //nolint:forbidigo // canary-real-home: monitors operator $HOME for leaks across TestIntegration runs
	if err != nil || home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".config", "zed", "settings.json"),
		filepath.Join(home, ".claude", "plugins", "known_marketplaces.json"),
		filepath.Join(home, ".claude", "plugins", "installed_plugins.json"),
	}
}

// canaryHomePrivateRoots returns the list of directory roots under $HOME the
// canary walks. Scoped narrowly to ~/.claude/plugins/cache/vibe-vault-local/
// — Claude Code itself writes to other subtrees of ~/.claude/plugins/ during
// normal operation (other plugins' caches, unrelated marketplace entries), so
// a broader watch would false-positive on operator activity between the pre-
// and post-snapshot. If noise appears, add entries to canaryShouldSkipFile
// rather than widening scope. Missing paths are dropped so the caller has a
// clean list.
func canaryHomePrivateRoots() []string {
	home, err := os.UserHomeDir() //nolint:forbidigo // canary-real-home: monitors operator ~/.claude/plugins/cache/vibe-vault-local/
	if err != nil || home == "" {
		return nil
	}
	candidates := []string{
		filepath.Join(home, ".claude", "plugins", "cache", "vibe-vault-local"),
	}
	var out []string
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

// snapshotRoot walks a single root and returns a (relPath → info) map. If the
// root does not exist, returns (nil, nil). Symlinks are not followed.
func snapshotRoot(root string) (map[string]canaryFileInfo, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	result := make(map[string]canaryFileInfo)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		// Skip symlinks; we only hash regular files.
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if canaryShouldSkipFile(rel) {
			return nil
		}
		sum, err := sha256File(path)
		if err != nil {
			return err
		}
		result[rel] = canaryFileInfo{
			relPath: rel,
			mtime:   info.ModTime(),
			sha256:  sum,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// sha256File computes the hex-encoded sha256 digest of a file's contents.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// snapshotConfigFile snapshots the operator's real config.toml, or returns
// nil if it doesn't exist.
func snapshotConfigFile(configPath string) (*canaryFileInfo, error) {
	info, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	sum, err := sha256File(configPath)
	if err != nil {
		return nil, err
	}
	return &canaryFileInfo{
		relPath: configPath,
		mtime:   info.ModTime(),
		sha256:  sum,
	}, nil
}

// snapshotSingleFile is the generalized form of snapshotConfigFile: capture
// the mtime+sha of an arbitrary single regular file by absolute path, or
// return nil if the file does not exist. Used for the category-C home-
// private write-target snapshots (~/.claude/settings.json,
// ~/.config/zed/settings.json, ~/.claude/plugins/*.json).
func snapshotSingleFile(path string) (*canaryFileInfo, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil
	}
	sum, err := sha256File(path)
	if err != nil {
		return nil, err
	}
	return &canaryFileInfo{
		relPath: path,
		mtime:   info.ModTime(),
		sha256:  sum,
	}, nil
}

// vaultCanarySnapshot is the entry point used at TestIntegration start. It
// reads the operator config, discovers the real vault, and snapshots
// protected paths. Returns (nil, "", "") if no operator config is present —
// in that case the canary subtest will skip.
func vaultCanarySnapshot(t *testing.T) (*canarySnapshot, string, string) {
	t.Helper()
	realVault, configPath, err := readOperatorConfigVaultPath()
	if err != nil {
		t.Fatalf("canary: read operator config: %v", err)
	}
	if configPath == "" {
		return nil, "", ""
	}
	snap, _, _ := vaultCanarySnapshotAt(t, configPath, realVault)
	return snap, configPath, realVault
}

// vaultCanarySnapshotAt takes a snapshot using an already-known config path
// and real vault path (used for the "after" snapshot so we don't re-resolve).
// Covers three distinct surfaces:
//   - vault-rooted project subtrees (canaryProtectedRoots)
//   - the operator's ~/.config/vibe-vault/config.toml (snap.configFile)
//   - category-C home-private write targets: ~/.claude/settings.json,
//     ~/.config/zed/settings.json, ~/.claude/plugins/{known_marketplaces,
//     installed_plugins}.json (snap.homeSingleFiles), and the directory
//     tree ~/.claude/plugins/cache/vibe-vault-local/ (added into
//     snap.roots alongside the vault-rooted ones).
func vaultCanarySnapshotAt(t *testing.T, configPath, realVault string) (*canarySnapshot, string, string) {
	t.Helper()
	snap := &canarySnapshot{
		roots:           make(map[string]map[string]canaryFileInfo),
		homeSingleFiles: make(map[string]*canaryFileInfo),
	}
	for _, root := range canaryProtectedRoots(realVault) {
		files, err := snapshotRoot(root)
		if err != nil {
			t.Fatalf("canary: snapshot %s: %v", root, err)
		}
		snap.roots[root] = files
	}
	for _, root := range canaryHomePrivateRoots() {
		files, err := snapshotRoot(root)
		if err != nil {
			t.Fatalf("canary: snapshot %s: %v", root, err)
		}
		snap.roots[root] = files
	}
	cfg, err := snapshotConfigFile(configPath)
	if err != nil {
		t.Fatalf("canary: snapshot config %s: %v", configPath, err)
	}
	snap.configFile = cfg
	for _, path := range canaryHomePrivateSingleFiles() {
		info, err := snapshotSingleFile(path)
		if err != nil {
			t.Fatalf("canary: snapshot %s: %v", path, err)
		}
		snap.homeSingleFiles[path] = info
	}
	return snap, configPath, realVault
}

// vaultCanaryDiff compares two snapshots and returns a slice of human-readable
// strings, one per mutation (added / removed / modified file, or changed
// config). Empty slice means no mutation.
func vaultCanaryDiff(before, after *canarySnapshot) []string {
	var out []string

	// Config file diff.
	switch {
	case before.configFile == nil && after.configFile != nil:
		out = append(out, fmt.Sprintf("ADDED    config: %s", after.configFile.relPath))
	case before.configFile != nil && after.configFile == nil:
		out = append(out, fmt.Sprintf("REMOVED  config: %s", before.configFile.relPath))
	case before.configFile != nil && after.configFile != nil:
		if before.configFile.sha256 != after.configFile.sha256 {
			out = append(out, fmt.Sprintf(
				"MODIFIED config: %s  (mtime %s → %s)",
				before.configFile.relPath,
				before.configFile.mtime.Format(time.RFC3339Nano),
				after.configFile.mtime.Format(time.RFC3339Nano)))
		}
	}

	// Home-private single-file diffs (category-C write targets). Union the
	// keys across before/after so a newly snapshotted path on a future run
	// still reports cleanly. Paths absent from both maps contribute nothing.
	homePathSet := make(map[string]struct{})
	for p := range before.homeSingleFiles {
		homePathSet[p] = struct{}{}
	}
	for p := range after.homeSingleFiles {
		homePathSet[p] = struct{}{}
	}
	homePaths := make([]string, 0, len(homePathSet))
	for p := range homePathSet {
		homePaths = append(homePaths, p)
	}
	sort.Strings(homePaths)
	for _, path := range homePaths {
		bf := before.homeSingleFiles[path]
		af := after.homeSingleFiles[path]
		switch {
		case bf == nil && af != nil:
			out = append(out, fmt.Sprintf("ADDED    home-private: %s", path))
		case bf != nil && af == nil:
			out = append(out, fmt.Sprintf("REMOVED  home-private: %s", path))
		case bf != nil && af != nil:
			if bf.sha256 != af.sha256 {
				out = append(out, fmt.Sprintf(
					"MODIFIED home-private: %s  (mtime %s → %s, sha %s → %s)",
					path,
					bf.mtime.Format(time.RFC3339Nano),
					af.mtime.Format(time.RFC3339Nano),
					bf.sha256[:8], af.sha256[:8]))
			}
		}
	}

	// Root diffs. Union of roots across before/after; roots that newly
	// appeared or disappeared are reported as such.
	rootSet := make(map[string]struct{})
	for r := range before.roots {
		rootSet[r] = struct{}{}
	}
	for r := range after.roots {
		rootSet[r] = struct{}{}
	}
	rootList := make([]string, 0, len(rootSet))
	for r := range rootSet {
		rootList = append(rootList, r)
	}
	sort.Strings(rootList)

	for _, root := range rootList {
		beforeFiles := before.roots[root]
		afterFiles := after.roots[root]
		if beforeFiles == nil && afterFiles != nil && len(afterFiles) > 0 {
			out = append(out, fmt.Sprintf("ADDED    root: %s (now contains %d file(s))",
				root, len(afterFiles)))
			for rel := range afterFiles {
				out = append(out, fmt.Sprintf("  ADDED    %s/%s", root, rel))
			}
			continue
		}
		if beforeFiles != nil && afterFiles == nil {
			out = append(out, fmt.Sprintf("REMOVED  root: %s (contained %d file(s))",
				root, len(beforeFiles)))
			continue
		}
		// File-level diff within root. Collect sorted rel paths from union.
		relSet := make(map[string]struct{})
		for rel := range beforeFiles {
			relSet[rel] = struct{}{}
		}
		for rel := range afterFiles {
			relSet[rel] = struct{}{}
		}
		rels := make([]string, 0, len(relSet))
		for rel := range relSet {
			rels = append(rels, rel)
		}
		sort.Strings(rels)
		for _, rel := range rels {
			bf, okBefore := beforeFiles[rel]
			af, okAfter := afterFiles[rel]
			switch {
			case !okBefore && okAfter:
				out = append(out, fmt.Sprintf("ADDED    %s/%s", root, rel))
			case okBefore && !okAfter:
				out = append(out, fmt.Sprintf("REMOVED  %s/%s", root, rel))
			case okBefore && okAfter:
				if bf.sha256 != af.sha256 {
					delta := af.mtime.Sub(bf.mtime)
					out = append(out, fmt.Sprintf(
						"MODIFIED %s/%s  (mtime Δ=%s: %s → %s, sha %s → %s)",
						root, rel, delta,
						bf.mtime.Format(time.RFC3339Nano),
						af.mtime.Format(time.RFC3339Nano),
						bf.sha256[:8], af.sha256[:8]))
				}
			}
		}
	}

	return out
}

// --- Vault accessor MCP tool integration tests (vv-vault-file-accessors) ---
//
// These tests exercise the vv_vault_* MCP tools end-to-end through their
// handler closures. They construct a config.Config with VaultPath set to a
// tempdir per D13's "test injection at construction time" pattern; no
// vault_path runtime parameter is supplied to any tool.

// vaultTestSetup creates a tempdir vault and a config.Config pointing at it.
// Returns (cfg, vaultPath). The vault directory exists; subdirectories are
// created on demand by individual tests.
func vaultTestSetup(t *testing.T) (config.Config, string) {
	t.Helper()
	vault := t.TempDir()
	return config.Config{VaultPath: vault}, vault
}

// vaultWriteRaw writes data to <vault>/<rel> via stdlib, creating parents.
// Used to seed test fixtures that bypass the MCP layer.
func vaultWriteRaw(t *testing.T, vault, rel, content string) {
	t.Helper()
	full := filepath.Join(vault, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

// callVaultTool marshals args into JSON and invokes tool.Handler.
func callVaultTool(t *testing.T, tool mcp.Tool, args any) (string, error) {
	t.Helper()
	var params json.RawMessage
	if args != nil {
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("marshal args: %v", err)
		}
		params = raw
	}
	return tool.Handler(params)
}

func TestIntegration_VaultRead_HappyPath(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	vaultWriteRaw(t, vault, "Notes/foo.md", "hello world")

	tool := mcp.NewVaultReadTool(cfg)
	out, err := callVaultTool(t, tool, map[string]any{"path": "Notes/foo.md"})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got struct {
		Content string `json:"content"`
		Bytes   int64  `json:"bytes"`
		Sha256  string `json:"sha256"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if got.Content != "hello world" {
		t.Errorf("content = %q, want %q", got.Content, "hello world")
	}
	if got.Bytes != 11 {
		t.Errorf("bytes = %d, want 11", got.Bytes)
	}
	if got.Sha256 == "" {
		t.Error("sha256 missing")
	}
}

func TestIntegration_VaultRead_PathTraversal(t *testing.T) {
	cfg, _ := vaultTestSetup(t)
	tool := mcp.NewVaultReadTool(cfg)
	for _, bad := range []string{"../etc/passwd", "/etc/passwd", "foo/../../../etc/passwd"} {
		_, err := callVaultTool(t, tool, map[string]any{"path": bad})
		if err == nil {
			t.Errorf("path %q: expected error, got nil", bad)
		}
	}
}

func TestIntegration_VaultRead_SymlinkEscape(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	// Place a file outside the vault, then create a symlink inside the vault
	// pointing to it. Read must reject.
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("classified"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	linkPath := filepath.Join(vault, "escape.md")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	tool := mcp.NewVaultReadTool(cfg)
	_, err := callVaultTool(t, tool, map[string]any{"path": "escape.md"})
	if err == nil {
		t.Fatal("expected symlink-escape error, got nil")
	}
	if !errors.Is(err, vaultfs.ErrSymlinkEscape) && !strings.Contains(err.Error(), "escape") {
		t.Errorf("error = %v, want symlink escape", err)
	}
}

func TestIntegration_VaultList_HappyPath(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	vaultWriteRaw(t, vault, "Notes/a.md", "A")
	vaultWriteRaw(t, vault, "Notes/b.md", "BB")
	if err := os.MkdirAll(filepath.Join(vault, "Notes/sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	tool := mcp.NewVaultListTool(cfg)
	out, err := callVaultTool(t, tool, map[string]any{"path": "Notes"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var got struct {
		Entries []vaultfs.Entry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if len(got.Entries) != 3 {
		t.Fatalf("entries = %d, want 3 (%v)", len(got.Entries), got.Entries)
	}
	names := map[string]string{}
	for _, e := range got.Entries {
		names[e.Name] = e.Type
	}
	if names["a.md"] != "file" || names["b.md"] != "file" || names["sub"] != "dir" {
		t.Errorf("entries map mismatch: %v", names)
	}
	for _, e := range got.Entries {
		if e.Sha256 != "" {
			t.Errorf("sha256 unexpectedly set on default list: %v", e)
		}
	}
}

func TestIntegration_VaultList_HidesDotGit(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	vaultWriteRaw(t, vault, "Notes/keep.md", "x")
	if err := os.MkdirAll(filepath.Join(vault, "Notes/.git"), 0o755); err != nil {
		t.Fatal(err)
	}

	tool := mcp.NewVaultListTool(cfg)
	out, err := callVaultTool(t, tool, map[string]any{"path": "Notes"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var got struct {
		Entries []vaultfs.Entry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	for _, e := range got.Entries {
		if strings.EqualFold(e.Name, ".git") {
			t.Errorf("list returned .git-like entry: %v", e)
		}
	}
}

func TestIntegration_VaultList_IncludeSha256(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	vaultWriteRaw(t, vault, "Notes/a.md", "A")

	tool := mcp.NewVaultListTool(cfg)
	out, err := callVaultTool(t, tool, map[string]any{
		"path":           "Notes",
		"include_sha256": true,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var got struct {
		Entries []vaultfs.Entry `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(got.Entries))
	}
	want := sha256.Sum256([]byte("A"))
	if got.Entries[0].Sha256 != hex.EncodeToString(want[:]) {
		t.Errorf("sha256 = %q, want %q", got.Entries[0].Sha256, hex.EncodeToString(want[:]))
	}
}

func TestIntegration_VaultExists_File(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	vaultWriteRaw(t, vault, "Notes/a.md", "x")
	if err := os.MkdirAll(filepath.Join(vault, "Notes/sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	tool := mcp.NewVaultExistsTool(cfg)

	// File exists.
	out, err := callVaultTool(t, tool, map[string]any{"path": "Notes/a.md"})
	if err != nil {
		t.Fatalf("exists file: %v", err)
	}
	var got vaultfs.Existence
	if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
		t.Fatalf("unmarshal: %v\n%s", uerr, out)
	}
	if !got.Exists || got.Type != "file" {
		t.Errorf("file: got %+v, want exists=true type=file", got)
	}

	// Directory exists.
	out, err = callVaultTool(t, tool, map[string]any{"path": "Notes/sub"})
	if err != nil {
		t.Fatalf("exists dir: %v", err)
	}
	if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
		t.Fatalf("unmarshal: %v\n%s", uerr, out)
	}
	if !got.Exists || got.Type != "dir" {
		t.Errorf("dir: got %+v, want exists=true type=dir", got)
	}

	// Missing.
	out, err = callVaultTool(t, tool, map[string]any{"path": "Notes/missing.md"})
	if err != nil {
		t.Fatalf("exists missing: %v", err)
	}
	if uerr := json.Unmarshal([]byte(out), &got); uerr != nil {
		t.Fatalf("unmarshal: %v\n%s", uerr, out)
	}
	if got.Exists {
		t.Errorf("missing: got %+v, want exists=false", got)
	}
}

func TestIntegration_VaultSha256_HappyPath(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	vaultWriteRaw(t, vault, "Notes/a.md", "abc")

	tool := mcp.NewVaultSha256Tool(cfg)
	out, err := callVaultTool(t, tool, map[string]any{"path": "Notes/a.md"})
	if err != nil {
		t.Fatalf("sha256: %v", err)
	}
	var got vaultfs.Sha256Result
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	want := sha256.Sum256([]byte("abc"))
	if got.Sha256 != hex.EncodeToString(want[:]) {
		t.Errorf("sha256 = %q, want %q", got.Sha256, hex.EncodeToString(want[:]))
	}
	if got.Bytes != 3 {
		t.Errorf("bytes = %d, want 3", got.Bytes)
	}
}

// TestIntegration_VaultRead_TempVaultPath_ViaConfig verifies the D13 test
// injection contract: the tool resolves vault path from cfg.VaultPath, not
// from any runtime parameter or global. Two parallel cfgs pointed at two
// distinct tempdirs MUST resolve independently.
func TestIntegration_VaultRead_TempVaultPath_ViaConfig(t *testing.T) {
	cfgA, vaultA := vaultTestSetup(t)
	cfgB, vaultB := vaultTestSetup(t)
	vaultWriteRaw(t, vaultA, "x.md", "from-A")
	vaultWriteRaw(t, vaultB, "x.md", "from-B")

	toolA := mcp.NewVaultReadTool(cfgA)
	toolB := mcp.NewVaultReadTool(cfgB)

	getContent := func(out string) string {
		var got struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("unmarshal: %v\n%s", err, out)
		}
		return got.Content
	}

	outA, err := callVaultTool(t, toolA, map[string]any{"path": "x.md"})
	if err != nil {
		t.Fatalf("toolA read: %v", err)
	}
	outB, err := callVaultTool(t, toolB, map[string]any{"path": "x.md"})
	if err != nil {
		t.Fatalf("toolB read: %v", err)
	}
	if got := getContent(outA); got != "from-A" {
		t.Errorf("toolA content = %q, want from-A", got)
	}
	if got := getContent(outB); got != "from-B" {
		t.Errorf("toolB content = %q, want from-B", got)
	}
}

func TestIntegration_VaultWrite_HappyPath(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	tool := mcp.NewVaultWriteTool(cfg)

	_, err := callVaultTool(t, tool, map[string]any{
		"path":    "Notes/new.md",
		"content": "fresh",
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(vault, "Notes/new.md"))
	if err != nil {
		t.Fatalf("read after write: %v", err)
	}
	if string(got) != "fresh" {
		t.Errorf("content = %q, want fresh", got)
	}
}

func TestIntegration_VaultWrite_PathTraversal(t *testing.T) {
	cfg, _ := vaultTestSetup(t)
	tool := mcp.NewVaultWriteTool(cfg)
	for _, bad := range []string{"../escape.md", "/abs/escape.md", "foo/../../etc/x"} {
		_, err := callVaultTool(t, tool, map[string]any{"path": bad, "content": "x"})
		if err == nil {
			t.Errorf("path %q: expected error", bad)
		}
	}
}

func TestIntegration_VaultWrite_RefusesGitDir(t *testing.T) {
	cfg, _ := vaultTestSetup(t)
	tool := mcp.NewVaultWriteTool(cfg)
	for _, bad := range []string{".git/HEAD", "Projects/foo/.git/config"} {
		_, err := callVaultTool(t, tool, map[string]any{"path": bad, "content": "x"})
		if err == nil {
			t.Errorf("path %q: expected refusal", bad)
		} else if !errors.Is(err, vaultfs.ErrRefusedPath) {
			t.Errorf("path %q: got %v, want ErrRefusedPath", bad, err)
		}
	}
}

func TestIntegration_VaultWrite_RefusesGitDir_CaseInsensitive(t *testing.T) {
	cfg, _ := vaultTestSetup(t)
	tool := mcp.NewVaultWriteTool(cfg)
	for _, bad := range []string{".GIT/HEAD", "Projects/foo/.Git/config", "Projects/foo/.gIt/x"} {
		_, err := callVaultTool(t, tool, map[string]any{"path": bad, "content": "x"})
		if err == nil {
			t.Errorf("path %q: expected refusal", bad)
		} else if !errors.Is(err, vaultfs.ErrRefusedPath) {
			t.Errorf("path %q: got %v, want ErrRefusedPath", bad, err)
		}
	}
}

func TestIntegration_VaultWrite_AllowsGitSubstring(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	tool := mcp.NewVaultWriteTool(cfg)
	// "foo.git" is a SEGMENT containing ".git" as a substring but not
	// equal to ".git" — D8 allows it.
	_, err := callVaultTool(t, tool, map[string]any{
		"path":    "Projects/foo.git/notes.md",
		"content": "ok",
	})
	if err != nil {
		t.Fatalf("write foo.git/notes.md: unexpected error %v", err)
	}
	got, err := os.ReadFile(filepath.Join(vault, "Projects/foo.git/notes.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "ok" {
		t.Errorf("content = %q, want ok", got)
	}
}

func TestIntegration_VaultWrite_CompareAndSet(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	vaultWriteRaw(t, vault, "Notes/a.md", "v1")
	tool := mcp.NewVaultWriteTool(cfg)

	want := sha256.Sum256([]byte("v1"))
	wantHex := hex.EncodeToString(want[:])

	// Matching expected_sha256 succeeds.
	_, err := callVaultTool(t, tool, map[string]any{
		"path":            "Notes/a.md",
		"content":         "v2",
		"expected_sha256": wantHex,
	})
	if err != nil {
		t.Fatalf("CAS match: %v", err)
	}

	// Now content is "v2"; supplying the OLD sha must fail with conflict.
	_, err = callVaultTool(t, tool, map[string]any{
		"path":            "Notes/a.md",
		"content":         "v3",
		"expected_sha256": wantHex,
	})
	if err == nil {
		t.Fatal("CAS mismatch: expected error, got nil")
	}
	if !errors.Is(err, vaultfs.ErrShaConflict) {
		t.Errorf("CAS mismatch: got %v, want ErrShaConflict", err)
	}
	// File must still be "v2" (not "v3").
	got, _ := os.ReadFile(filepath.Join(vault, "Notes/a.md"))
	if string(got) != "v2" {
		t.Errorf("file should remain v2 after CAS conflict, got %q", got)
	}
}

func TestIntegration_VaultEdit_HappyPath(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	vaultWriteRaw(t, vault, "Notes/a.md", "Hello, world!\n")
	tool := mcp.NewVaultEditTool(cfg)

	out, err := callVaultTool(t, tool, map[string]any{
		"path":       "Notes/a.md",
		"old_string": "world",
		"new_string": "vault",
	})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	var got vaultfs.EditResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if got.Replacements != 1 {
		t.Errorf("replacements = %d, want 1", got.Replacements)
	}
	data, _ := os.ReadFile(filepath.Join(vault, "Notes/a.md"))
	if string(data) != "Hello, vault!\n" {
		t.Errorf("content = %q, want %q", data, "Hello, vault!\n")
	}
}

func TestIntegration_VaultEdit_AmbiguousMatch(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	vaultWriteRaw(t, vault, "Notes/a.md", "foo\nfoo\n")
	tool := mcp.NewVaultEditTool(cfg)

	_, err := callVaultTool(t, tool, map[string]any{
		"path":       "Notes/a.md",
		"old_string": "foo",
		"new_string": "bar",
	})
	if err == nil {
		t.Fatal("expected ambiguous-match error, got nil")
	}
	if !strings.Contains(err.Error(), "replace_all") {
		t.Errorf("error should mention replace_all, got %v", err)
	}

	// With replace_all: true, the same call succeeds.
	out, err := callVaultTool(t, tool, map[string]any{
		"path":        "Notes/a.md",
		"old_string":  "foo",
		"new_string":  "bar",
		"replace_all": true,
	})
	if err != nil {
		t.Fatalf("replace_all: %v", err)
	}
	var got vaultfs.EditResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if got.Replacements != 2 {
		t.Errorf("replacements = %d, want 2", got.Replacements)
	}
}

func TestIntegration_VaultDelete_HappyPath(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	vaultWriteRaw(t, vault, "Notes/doomed.md", "x")
	tool := mcp.NewVaultDeleteTool(cfg)

	out, err := callVaultTool(t, tool, map[string]any{"path": "Notes/doomed.md"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	var got vaultfs.DeleteResult
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, out)
	}
	if !got.Removed {
		t.Error("removed = false, want true")
	}
	if _, err := os.Stat(filepath.Join(vault, "Notes/doomed.md")); !os.IsNotExist(err) {
		t.Errorf("file still exists after delete: %v", err)
	}
}

func TestIntegration_VaultDelete_RefusesDirectory(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	if err := os.MkdirAll(filepath.Join(vault, "Notes/sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	tool := mcp.NewVaultDeleteTool(cfg)
	_, err := callVaultTool(t, tool, map[string]any{"path": "Notes/sub"})
	if err == nil {
		t.Fatal("expected error deleting directory")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("error should mention directory, got %v", err)
	}
}

func TestIntegration_VaultMove_HappyPath(t *testing.T) {
	cfg, vault := vaultTestSetup(t)
	vaultWriteRaw(t, vault, "Notes/a.md", "movable")
	tool := mcp.NewVaultMoveTool(cfg)

	_, err := callVaultTool(t, tool, map[string]any{
		"from_path": "Notes/a.md",
		"to_path":   "Archive/a.md",
	})
	if err != nil {
		t.Fatalf("move: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(vault, "Notes/a.md")); !os.IsNotExist(serr) {
		t.Errorf("source still exists: %v", serr)
	}
	got, err := os.ReadFile(filepath.Join(vault, "Archive/a.md"))
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(got) != "movable" {
		t.Errorf("dst content = %q, want movable", got)
	}
}

// TestIntegration_AutoMemoryWrite_VisibleViaHostSymlink is the full
// end-to-end MCP-handler version of the auto-memory acceptance test: writing
// via vv_vault_write to the canonical vault path lands on disk such that the
// host-side symlink view (the path Claude Code's auto-memory tooling uses)
// observes the same content. Mirrors the production setup created by
// `vv memory link`.
func TestIntegration_AutoMemoryWrite_VisibleViaHostSymlink(t *testing.T) {
	cfg, vault := vaultTestSetup(t)

	memDir := filepath.Join(vault, "Projects", "foo", "agentctx", "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("mkdir vault memory: %v", err)
	}

	host := t.TempDir()
	hostMem := filepath.Join(host, "memory")
	if err := os.Symlink(memDir, hostMem); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	tool := mcp.NewVaultWriteTool(cfg)
	rel := "Projects/foo/agentctx/memory/MEMORY.md"
	body := "auto-memory entry from MCP\n"
	if _, err := callVaultTool(t, tool, map[string]any{"path": rel, "content": body}); err != nil {
		t.Fatalf("vv_vault_write: %v", err)
	}

	// Vault-side: actual file landed.
	gotVault, err := os.ReadFile(filepath.Join(vault, rel))
	if err != nil {
		t.Fatalf("read vault file: %v", err)
	}
	if string(gotVault) != body {
		t.Errorf("vault content = %q, want %q", gotVault, body)
	}

	// Host-side: same file visible via the symlink.
	gotHost, err := os.ReadFile(filepath.Join(hostMem, "MEMORY.md"))
	if err != nil {
		t.Fatalf("read via host symlink: %v", err)
	}
	if string(gotHost) != body {
		t.Errorf("host content = %q, want %q", gotHost, body)
	}
}

// vaultPushTestSetup builds an isolated vault + bare-remote pair suitable
// for exercising `vv vault push`. Returns the env slice (HOME/XDG sandboxed,
// git identity injected), the vault working copy path, and the bare remote
// path. Mirrors the inline setup in TestIntegration/vault_push_multi_remote
// but factored out so the --paths variants can share it.
func vaultPushTestSetup(t *testing.T) (env []string, vaultDir, remoteDir string) {
	t.Helper()
	vaultDir = t.TempDir()
	xdg := t.TempDir()
	home := t.TempDir()
	env = buildEnvWithHome(xdg, home)
	// vaultsync.CommitAndPush{,Paths} shells out to `git commit`, which
	// needs an identity. The sandboxed HOME has no .gitconfig, so inject
	// identity via process env.
	env = append(env,
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	mustRunVV(t, env, "init", vaultDir)

	// Make the vault directory a git repo with an initial commit so
	// CommitAndPush has a clean history to push.
	gitx.GitRun(t, vaultDir, "init", "-b", "main")
	gitx.GitRun(t, vaultDir, "add", ".")
	gitx.GitRun(t, vaultDir, "commit", "-m", "initial vault commit")

	remoteDir = gitx.InitBareRemote(t)
	gitx.AddRemote(t, vaultDir, "origin", remoteDir)
	return env, vaultDir, remoteDir
}

// TestIntegration_VaultPush_PathsFlag_Selective verifies that
// `vv vault push --paths foo.md` stages and commits ONLY foo.md, leaving
// bar.md dirty in the working tree. Locks the Phase 2 selective-staging
// CLI surface.
func TestIntegration_VaultPush_PathsFlag_Selective(t *testing.T) {
	env, vaultDir, remoteDir := vaultPushTestSetup(t)

	// Two dirty files.
	writeFixture(t, vaultDir, "foo.md", "foo content\n")
	writeFixture(t, vaultDir, "bar.md", "bar content\n")

	stdout := mustRunVV(t, env, "vault", "push", "--message", "selective", "--paths", "foo.md")
	assertContains(t, stdout, "committed and pushed", "selective push reports commit")

	// (a) Commit at HEAD on the remote contains exactly foo.md.
	headSHA := strings.TrimSpace(gitx.GitRun(t, remoteDir, "rev-parse", "refs/heads/main"))
	files := strings.TrimSpace(gitx.GitRun(t, remoteDir,
		"diff-tree", "--no-commit-id", "--name-only", "-r", headSHA))
	got := strings.Split(files, "\n")
	sort.Strings(got)
	want := []string{"foo.md"}
	if !equalStringSlices(got, want) {
		t.Errorf("commit files = %v, want %v", got, want)
	}

	// (b) bar.md remains dirty in the working tree.
	status := strings.TrimSpace(gitx.GitRun(t, vaultDir, "status", "--porcelain"))
	if !strings.Contains(status, "bar.md") {
		t.Errorf("bar.md not dirty after selective push; status=%q", status)
	}
	if strings.Contains(status, "foo.md") {
		t.Errorf("foo.md still appears dirty after selective push; status=%q", status)
	}
}

// TestIntegration_VaultPush_NoFlag_CatchAll verifies that the legacy
// `vv vault push` (no --paths) still stages every dirty path. Regression-
// locks Phase 2 against accidentally routing the catch-all entry through
// the new function with a wrong default.
func TestIntegration_VaultPush_NoFlag_CatchAll(t *testing.T) {
	env, vaultDir, remoteDir := vaultPushTestSetup(t)

	writeFixture(t, vaultDir, "foo.md", "foo content\n")
	writeFixture(t, vaultDir, "bar.md", "bar content\n")

	stdout := mustRunVV(t, env, "vault", "push", "--message", "catch-all")
	assertContains(t, stdout, "committed and pushed", "catch-all push reports commit")

	// Both files in the commit at HEAD on the remote.
	headSHA := strings.TrimSpace(gitx.GitRun(t, remoteDir, "rev-parse", "refs/heads/main"))
	files := strings.TrimSpace(gitx.GitRun(t, remoteDir,
		"diff-tree", "--no-commit-id", "--name-only", "-r", headSHA))
	got := strings.Split(files, "\n")
	sort.Strings(got)
	want := []string{"bar.md", "foo.md"}
	if !equalStringSlices(got, want) {
		t.Errorf("commit files = %v, want %v", got, want)
	}

	// Working tree is now clean.
	status := strings.TrimSpace(gitx.GitRun(t, vaultDir, "status", "--porcelain"))
	if status != "" {
		t.Errorf("working tree dirty after catch-all push; status=%q", status)
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
