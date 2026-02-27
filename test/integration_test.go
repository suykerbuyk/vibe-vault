package test

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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

// --- Fixtures ---

// fixtureSessionA1: normal session, project "myproject", branch "feat/login",
// 3 user + 3 assistant messages, 2 tool uses (Write, Edit), sessionId session-aaa-001
const fixtureSessionA1 = `{"type":"user","uuid":"u1","sessionId":"session-aaa-001","timestamp":"2027-06-15T10:00:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/login","message":{"role":"user","content":"Implement the login page with OAuth support"}}
{"type":"assistant","uuid":"a1","sessionId":"session-aaa-001","timestamp":"2027-06-15T10:01:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/login","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"I'll implement the login page."},{"type":"tool_use","id":"tu1","name":"Write","input":{"file_path":"/home/dev/myproject/src/login.tsx","content":"// login page"}}],"usage":{"input_tokens":500,"output_tokens":200,"cache_creation_input_tokens":100,"cache_read_input_tokens":50}}}
{"type":"user","uuid":"u2","sessionId":"session-aaa-001","timestamp":"2027-06-15T10:02:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/login","message":{"role":"user","content":"Now add the OAuth callback handler"}}
{"type":"assistant","uuid":"a2","sessionId":"session-aaa-001","timestamp":"2027-06-15T10:03:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/login","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Adding OAuth callback."},{"type":"tool_use","id":"tu2","name":"Edit","input":{"file_path":"/home/dev/myproject/src/auth.ts","old_string":"// TODO","new_string":"handleCallback()"}}],"usage":{"input_tokens":400,"output_tokens":150,"cache_creation_input_tokens":0,"cache_read_input_tokens":200}}}
{"type":"user","uuid":"u3","sessionId":"session-aaa-001","timestamp":"2027-06-15T10:04:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/login","message":{"role":"user","content":"Add error handling for the OAuth flow"}}
{"type":"assistant","uuid":"a3","sessionId":"session-aaa-001","timestamp":"2027-06-15T10:05:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/login","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Done! The login page now has full OAuth support with error handling."}],"usage":{"input_tokens":300,"output_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":100}}}
`

// fixtureSessionA2: same project/date as A1, 2 user + 2 assistant, sessionId session-aaa-002
const fixtureSessionA2 = `{"type":"user","uuid":"u1","sessionId":"session-aaa-002","timestamp":"2027-06-15T14:00:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/login","message":{"role":"user","content":"Add unit tests for the login page"}}
{"type":"assistant","uuid":"a1","sessionId":"session-aaa-002","timestamp":"2027-06-15T14:01:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/login","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"I'll write tests for the login page."}],"usage":{"input_tokens":300,"output_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
{"type":"user","uuid":"u2","sessionId":"session-aaa-002","timestamp":"2027-06-15T14:02:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/login","message":{"role":"user","content":"Make sure to test the error cases too"}}
{"type":"assistant","uuid":"a2","sessionId":"session-aaa-002","timestamp":"2027-06-15T14:03:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/login","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Added comprehensive tests including error cases."}],"usage":{"input_tokens":200,"output_tokens":80,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
`

// fixtureTrivial: 1 user + 1 assistant (skipped: < 2 messages each)
const fixtureTrivial = `{"type":"user","uuid":"u1","sessionId":"session-trivial-001","timestamp":"2027-06-15T12:00:00Z","cwd":"/home/dev/myproject","message":{"role":"user","content":"hello"}}
{"type":"assistant","uuid":"a1","sessionId":"session-trivial-001","timestamp":"2027-06-15T12:01:00Z","cwd":"/home/dev/myproject","message":{"role":"assistant","model":"claude-opus-4-6","content":"Hi there!","usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
`

// fixtureSessionB: different project "other-project", 2 user + 2 assistant
const fixtureSessionB = `{"type":"user","uuid":"u1","sessionId":"session-bbb-001","timestamp":"2027-06-15T16:00:00Z","cwd":"/home/dev/other-project","gitBranch":"main","message":{"role":"user","content":"Set up the CI pipeline for this project"}}
{"type":"assistant","uuid":"a1","sessionId":"session-bbb-001","timestamp":"2027-06-15T16:01:00Z","cwd":"/home/dev/other-project","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"I'll set up the CI pipeline."}],"usage":{"input_tokens":200,"output_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
{"type":"user","uuid":"u2","sessionId":"session-bbb-001","timestamp":"2027-06-15T16:02:00Z","cwd":"/home/dev/other-project","gitBranch":"main","message":{"role":"user","content":"Add a deploy step too"}}
{"type":"assistant","uuid":"a2","sessionId":"session-bbb-001","timestamp":"2027-06-15T16:03:00Z","cwd":"/home/dev/other-project","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Done. CI pipeline with deploy step is ready."}],"usage":{"input_tokens":150,"output_tokens":80,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
`

// fixtureBackfill: UUID session ID for discover.Discover(), project "project-x"
const fixtureBackfill = `{"type":"user","uuid":"u1","sessionId":"abcdef01-2345-6789-abcd-ef0123456789","timestamp":"2027-06-15T18:00:00Z","cwd":"/home/dev/project-x","gitBranch":"main","message":{"role":"user","content":"Initialize the database schema for the new service"}}
{"type":"assistant","uuid":"a1","sessionId":"abcdef01-2345-6789-abcd-ef0123456789","timestamp":"2027-06-15T18:01:00Z","cwd":"/home/dev/project-x","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"I'll create the database schema."}],"usage":{"input_tokens":200,"output_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
{"type":"user","uuid":"u2","sessionId":"abcdef01-2345-6789-abcd-ef0123456789","timestamp":"2027-06-15T18:02:00Z","cwd":"/home/dev/project-x","gitBranch":"main","message":{"role":"user","content":"Add migration support as well"}}
{"type":"assistant","uuid":"a2","sessionId":"abcdef01-2345-6789-abcd-ef0123456789","timestamp":"2027-06-15T18:03:00Z","cwd":"/home/dev/project-x","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Database schema with migration support is ready."}],"usage":{"input_tokens":150,"output_tokens":80,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}
`

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

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
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

// fixtureStopSession: session with tool usage for Stop/checkpoint testing.
// 3 user + 3 assistant, 2 tool uses (Write, Edit), sessionId session-stop-001
const fixtureStopSession = `{"type":"user","uuid":"u1","sessionId":"session-stop-001","timestamp":"2027-07-01T10:00:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/stop","message":{"role":"user","content":"Implement the stop hook feature"}}
{"type":"assistant","uuid":"a1","sessionId":"session-stop-001","timestamp":"2027-07-01T10:01:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/stop","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"I'll implement the stop hook."},{"type":"tool_use","id":"tu1","name":"Write","input":{"file_path":"/home/dev/myproject/handler.go","content":"// stop handler"}}],"usage":{"input_tokens":500,"output_tokens":200,"cache_creation_input_tokens":100,"cache_read_input_tokens":50}}}
{"type":"user","uuid":"u2","sessionId":"session-stop-001","timestamp":"2027-07-01T10:02:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/stop","message":{"role":"user","content":"Add checkpoint capture logic"}}
{"type":"assistant","uuid":"a2","sessionId":"session-stop-001","timestamp":"2027-07-01T10:03:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/stop","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Adding checkpoint capture."},{"type":"tool_use","id":"tu2","name":"Edit","input":{"file_path":"/home/dev/myproject/capture.go","old_string":"// TODO","new_string":"captureCheckpoint()"}}],"usage":{"input_tokens":400,"output_tokens":150,"cache_creation_input_tokens":0,"cache_read_input_tokens":200}}}
{"type":"user","uuid":"u3","sessionId":"session-stop-001","timestamp":"2027-07-01T10:04:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/stop","message":{"role":"user","content":"Great, that looks good"}}
{"type":"assistant","uuid":"a3","sessionId":"session-stop-001","timestamp":"2027-07-01T10:05:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/stop","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Done! The stop hook feature is complete."}],"usage":{"input_tokens":300,"output_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":100}}}
`

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
	a1Path := writeFixture(t, fixtureDir, "session-aaa-001.jsonl", fixtureSessionA1)
	a2Path := writeFixture(t, fixtureDir, "session-aaa-002.jsonl", fixtureSessionA2)
	trivialPath := writeFixture(t, fixtureDir, "session-trivial-001.jsonl", fixtureTrivial)
	bPath := writeFixture(t, fixtureDir, "session-bbb-001.jsonl", fixtureSessionB)

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
		if !fileExists(filepath.Join(vaultPath, "Sessions", ".gitkeep")) {
			t.Error("Sessions/.gitkeep not created")
		}

		// Config written
		cfgPath := filepath.Join(xdgConfigHome, "vibe-vault", "config.toml")
		if !fileExists(cfgPath) {
			t.Fatal("config.toml not created")
		}
		cfgContent := readFile(t, cfgPath)
		assertContains(t, cfgContent, "vault_path", "config content")

		// stdout
		assertContains(t, stdout, "Done", "init stdout")
	})

	// 2. process session A1
	t.Run("process_session_a1", func(t *testing.T) {
		stdout := mustRunVV(t, env, "process", a1Path)

		assertContains(t, stdout, "created:", "process stdout")

		// Note file exists
		notePath := filepath.Join(vaultPath, "Sessions", "myproject", "2027-06-15-01.md")
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

		notePath := filepath.Join(vaultPath, "Sessions", "myproject", "2027-06-15-02.md")
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

		notePath := filepath.Join(vaultPath, "Sessions", "other-project", "2027-06-15-01.md")
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

	// 6. index rebuild
	t.Run("index_rebuild", func(t *testing.T) {
		stdout := mustRunVV(t, env, "index")
		assertContains(t, stdout, "indexed 3 sessions", "index stdout")

		// Rebuilt index still has all 3
		idx := readIndex(t, stateDir)
		if len(idx) != 3 {
			t.Errorf("rebuilt index entries: got %d, want 3", len(idx))
		}
		for _, sid := range []string{"session-aaa-001", "session-aaa-002", "session-bbb-001"} {
			if _, ok := idx[sid]; !ok {
				t.Errorf("rebuilt index missing %s", sid)
			}
		}

		// _context.md generated per project
		for _, project := range []string{"myproject", "other-project"} {
			ctxPath := filepath.Join(vaultPath, "Sessions", project, "_context.md")
			if !fileExists(ctxPath) {
				t.Errorf("_context.md not created for %s", project)
				continue
			}
			ctx := readFile(t, ctxPath)
			assertContains(t, ctx, "type: project-context", fmt.Sprintf("%s context type", project))
			assertContains(t, ctx, "## Session Timeline", fmt.Sprintf("%s timeline", project))
		}
	})

	// 7. backfill
	t.Run("backfill", func(t *testing.T) {
		// Set up backfill directory structure: basePath/project-x/{uuid}.jsonl
		backfillDir := t.TempDir()
		projectDir := filepath.Join(backfillDir, "project-x")
		writeFixture(t, projectDir, "abcdef01-2345-6789-abcd-ef0123456789.jsonl", fixtureBackfill)

		stdout := mustRunVV(t, env, "backfill", backfillDir)
		assertContains(t, stdout, "Found", "backfill found")
		assertContains(t, stdout, "processed:", "backfill processed")

		// Index now has 4 entries
		idx := readIndex(t, stateDir)
		if len(idx) != 4 {
			t.Errorf("index entries after backfill: got %d, want 4", len(idx))
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
		stopTranscriptPath := writeFixture(t, fixtureDir, "session-stop-001.jsonl", fixtureStopSession)

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
		notePath := filepath.Join(vaultPath, "Sessions", "myproject", "2027-07-01-01.md")
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
		if checkpoint, ok := stopEntry["checkpoint"].(bool); !ok || !checkpoint {
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
		projectDir := filepath.Join(vaultPath, "Sessions", "myproject")
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

	// 10. reprocess
	t.Run("reprocess", func(t *testing.T) {
		stdout := mustRunVV(t, env, "reprocess")
		assertContains(t, stdout, "reprocessed:", "reprocess stdout")

		// Notes still exist with valid content
		for _, tc := range []struct {
			path      string
			sessionID string
			project   string
		}{
			{"Sessions/myproject/2027-06-15-01.md", "session-aaa-001", "myproject"},
			{"Sessions/myproject/2027-06-15-02.md", "session-aaa-002", "myproject"},
			{"Sessions/other-project/2027-06-15-01.md", "session-bbb-001", "other-project"},
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

		// _context.md regenerated
		for _, project := range []string{"myproject", "other-project"} {
			ctxPath := filepath.Join(vaultPath, "Sessions", project, "_context.md")
			if !fileExists(ctxPath) {
				t.Errorf("_context.md not regenerated for %s", project)
			}
		}

		// Index preserved
		idx := readIndex(t, stateDir)
		if len(idx) < 3 {
			t.Errorf("index entries after reprocess: got %d, want >= 3", len(idx))
		}
	})
}
