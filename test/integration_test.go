package test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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

func buildEnv(xdgConfigHome string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"XDG_CONFIG_HOME=" + xdgConfigHome,
	}
}

func buildEnvWithHome(xdgConfigHome, home string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + xdgConfigHome,
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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

		// Note file exists
		notePath := filepath.Join(vaultPath, "Projects", "myproject", "sessions", "2027-06-15-01.md")
		if !fileExists(notePath) {
			t.Fatalf("note not created at %s", notePath)
		}

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

		notePath := filepath.Join(vaultPath, "Projects", "myproject", "sessions", "2027-06-15-02.md")
		if !fileExists(notePath) {
			t.Fatalf("note not created at %s", notePath)
		}

		note := readFile(t, notePath)

		assertContains(t, note, "iteration: 2", "frontmatter iteration")
		assertContains(t, note, "previous: \"[[2027-06-15-01]]\"", "previous link")

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

		notePath := filepath.Join(vaultPath, "Projects", "other-project", "sessions", "2027-06-15-01.md")
		if !fileExists(notePath) {
			t.Fatalf("note not created at %s", notePath)
		}

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

		notePath := filepath.Join(vaultPath, "Projects", "narr-project", "sessions", "2027-08-10-01.md")
		if !fileExists(notePath) {
			t.Fatalf("note not created at %s", notePath)
		}

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
		if commits, ok := narrEntry["commits"].([]interface{}); ok {
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
			if commits, ok := narrEntry["commits"].([]interface{}); ok {
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

		// Verify checkpoint note exists with status: checkpoint
		notePath := filepath.Join(vaultPath, "Projects", "myproject", "sessions", "2027-07-01-01.md")
		if !fileExists(notePath) {
			t.Fatalf("checkpoint note not created at %s", notePath)
		}

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

		// Verify note is now finalized
		finalizedNote := readFile(t, notePath)
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

		// Verify only one note file for this session (not two)
		projectDir := filepath.Join(vaultPath, "Projects", "myproject", "sessions")
		entries, _ := os.ReadDir(projectDir)
		stopNotes := 0
		for _, e := range entries {
			if strings.Contains(e.Name(), "2027-07-01") {
				stopNotes++
			}
		}
		if stopNotes != 1 {
			t.Errorf("expected 1 note file for 2027-07-01, got %d", stopNotes)
		}
	})

	// 10a. process friction session
	t.Run("process_friction_session", func(t *testing.T) {
		stdout := mustRunVV(t, env, "process", frictionPath)
		assertContains(t, stdout, "created:", "process stdout")

		notePath := filepath.Join(vaultPath, "Projects", "friction-project", "sessions", "2027-09-01-01.md")
		if !fileExists(notePath) {
			t.Fatalf("note not created at %s", notePath)
		}

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
		var parsed map[string]interface{}
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

	// 10g. export
	t.Run("export", func(t *testing.T) {
		// JSON export (all sessions)
		jsonStdout := mustRunVV(t, env, "export")
		var jsonData []map[string]interface{}
		if err := json.Unmarshal([]byte(jsonStdout), &jsonData); err != nil {
			t.Fatalf("invalid JSON from export: %v\noutput: %s", err, jsonStdout)
		}
		if len(jsonData) == 0 {
			t.Error("expected non-empty JSON array from export")
		}

		// JSON export filtered by project
		projStdout := mustRunVV(t, env, "export", "--project", "myproject")
		var projData []map[string]interface{}
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

		// Notes still exist with valid content
		for _, tc := range []struct {
			path      string
			sessionID string
			project   string
		}{
			{"Projects/myproject/sessions/2027-06-15-01.md", "session-aaa-001", "myproject"},
			{"Projects/myproject/sessions/2027-06-15-02.md", "session-aaa-002", "myproject"},
			{"Projects/other-project/sessions/2027-06-15-01.md", "session-bbb-001", "other-project"},
		} {
			absPath := filepath.Join(vaultPath, tc.path)
			if !fileExists(absPath) {
				t.Errorf("note missing after reprocess: %s", tc.path)
				continue
			}
			note := readFile(t, absPath)
			assertContains(t, note, fmt.Sprintf("session_id: \"%s\"", tc.sessionID), tc.path+" session_id")
			assertContains(t, note, fmt.Sprintf("project: %s", tc.project), tc.path+" project")
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

		var responses []map[string]interface{}
		for i, line := range lines {
			var resp map[string]interface{}
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				t.Fatalf("response %d: invalid JSON: %v\nline: %s", i, err, line)
			}
			responses = append(responses, resp)
		}

		// Response 0: initialize — should have serverInfo
		if r := responses[0]["result"].(map[string]interface{}); r["serverInfo"] == nil {
			t.Error("initialize: missing serverInfo")
		}

		// Response 1: tools/list — should have tools array
		toolsResult := responses[1]["result"].(map[string]interface{})
		tools := toolsResult["tools"].([]interface{})
		if len(tools) != 20 {
			t.Errorf("tools/list: expected 20 tools, got %d", len(tools))
		}
		toolNames := make(map[string]bool)
		for _, tool := range tools {
			toolNames[tool.(map[string]interface{})["name"].(string)] = true
		}
		if !toolNames["vv_get_project_context"] {
			t.Error("tools/list: missing vv_get_project_context")
		}
		if !toolNames["vv_list_projects"] {
			t.Error("tools/list: missing vv_list_projects")
		}

		// Response 2: vv_list_projects — should return project data
		listResult := responses[2]["result"].(map[string]interface{})
		content := listResult["content"].([]interface{})
		if len(content) == 0 {
			t.Fatal("vv_list_projects: empty content")
		}
		listText := content[0].(map[string]interface{})["text"].(string)
		var projects []map[string]interface{}
		if err := json.Unmarshal([]byte(listText), &projects); err != nil {
			t.Fatalf("vv_list_projects: invalid JSON in text: %v", err)
		}
		if len(projects) == 0 {
			t.Error("vv_list_projects: no projects returned")
		}

		// Response 3: vv_get_project_context — should return context for myproject
		ctxResult := responses[3]["result"].(map[string]interface{})
		ctxContent := ctxResult["content"].([]interface{})
		ctxText := ctxContent[0].(map[string]interface{})["text"].(string)
		var ctxParsed map[string]interface{}
		if err := json.Unmarshal([]byte(ctxText), &ctxParsed); err != nil {
			t.Fatalf("vv_get_project_context: invalid JSON: %v", err)
		}
		if ctxParsed["project"] != "myproject" {
			t.Errorf("vv_get_project_context: project = %v, want myproject", ctxParsed["project"])
		}

		// Response 4: unknown tool — should have isError
		unknownResult := responses[4]["result"].(map[string]interface{})
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
}
