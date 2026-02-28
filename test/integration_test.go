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

// fixtureStopSession: session with tool usage for Stop/checkpoint testing.
// 3 user + 3 assistant, 2 tool uses (Write, Edit), sessionId session-stop-001
const fixtureStopSession = `{"type":"user","uuid":"u1","sessionId":"session-stop-001","timestamp":"2027-07-01T10:00:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/stop","message":{"role":"user","content":"Implement the stop hook feature"}}
{"type":"assistant","uuid":"a1","sessionId":"session-stop-001","timestamp":"2027-07-01T10:01:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/stop","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"I'll implement the stop hook."},{"type":"tool_use","id":"tu1","name":"Write","input":{"file_path":"/home/dev/myproject/handler.go","content":"// stop handler"}}],"usage":{"input_tokens":500,"output_tokens":200,"cache_creation_input_tokens":100,"cache_read_input_tokens":50}}}
{"type":"user","uuid":"u2","sessionId":"session-stop-001","timestamp":"2027-07-01T10:02:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/stop","message":{"role":"user","content":"Add checkpoint capture logic"}}
{"type":"assistant","uuid":"a2","sessionId":"session-stop-001","timestamp":"2027-07-01T10:03:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/stop","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Adding checkpoint capture."},{"type":"tool_use","id":"tu2","name":"Edit","input":{"file_path":"/home/dev/myproject/capture.go","old_string":"// TODO","new_string":"captureCheckpoint()"}}],"usage":{"input_tokens":400,"output_tokens":150,"cache_creation_input_tokens":0,"cache_read_input_tokens":200}}}
{"type":"user","uuid":"u3","sessionId":"session-stop-001","timestamp":"2027-07-01T10:04:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/stop","message":{"role":"user","content":"Great, that looks good"}}
{"type":"assistant","uuid":"a3","sessionId":"session-stop-001","timestamp":"2027-07-01T10:05:00Z","cwd":"/home/dev/myproject","gitBranch":"feat/stop","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Done! The stop hook feature is complete."}],"usage":{"input_tokens":300,"output_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":100}}}
`

// fixtureNarrativeSession: session with Write, Bash test, and Bash git commit tool calls
// for testing narrative extraction. Includes tool results. sessionId session-narr-001
const fixtureNarrativeSession = `{"type":"user","uuid":"u1","sessionId":"session-narr-001","timestamp":"2027-08-10T09:00:00Z","cwd":"/home/dev/narr-project","gitBranch":"feat/auth","message":{"role":"user","content":"Add JWT authentication to the API"}}
{"type":"assistant","uuid":"a1","sessionId":"session-narr-001","timestamp":"2027-08-10T09:01:00Z","cwd":"/home/dev/narr-project","gitBranch":"feat/auth","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"I'll create the JWT handler."},{"type":"tool_use","id":"ntu1","name":"Write","input":{"file_path":"/home/dev/narr-project/internal/auth/handler.go","content":"package auth\n\nfunc Handler() {}"}}],"usage":{"input_tokens":500,"output_tokens":200,"cache_creation_input_tokens":100,"cache_read_input_tokens":50}}}
{"type":"user","uuid":"u1r","sessionId":"session-narr-001","timestamp":"2027-08-10T09:01:30Z","cwd":"/home/dev/narr-project","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"ntu1","content":"File created successfully"}]}}
{"type":"assistant","uuid":"a2","sessionId":"session-narr-001","timestamp":"2027-08-10T09:02:00Z","cwd":"/home/dev/narr-project","gitBranch":"feat/auth","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Now let me run the tests."},{"type":"tool_use","id":"ntu2","name":"Bash","input":{"command":"go test ./..."}}],"usage":{"input_tokens":400,"output_tokens":150,"cache_creation_input_tokens":0,"cache_read_input_tokens":200}}}
{"type":"user","uuid":"u2r","sessionId":"session-narr-001","timestamp":"2027-08-10T09:02:30Z","cwd":"/home/dev/narr-project","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"ntu2","content":"ok  \tgithub.com/dev/narr-project\t0.5s\nPASS"}]}}
{"type":"user","uuid":"u2","sessionId":"session-narr-001","timestamp":"2027-08-10T09:03:00Z","cwd":"/home/dev/narr-project","gitBranch":"feat/auth","message":{"role":"user","content":"Great, now commit the changes"}}
{"type":"assistant","uuid":"a3","sessionId":"session-narr-001","timestamp":"2027-08-10T09:04:00Z","cwd":"/home/dev/narr-project","gitBranch":"feat/auth","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Committing the changes."},{"type":"tool_use","id":"ntu3","name":"Bash","input":{"command":"git commit -m \"feat: add JWT authentication\""}}],"usage":{"input_tokens":300,"output_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":100}}}
{"type":"user","uuid":"u3r","sessionId":"session-narr-001","timestamp":"2027-08-10T09:04:30Z","cwd":"/home/dev/narr-project","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"ntu3","content":"[feat/auth abc1234] feat: add JWT authentication\n 1 file changed"}]}}
{"type":"assistant","uuid":"a4","sessionId":"session-narr-001","timestamp":"2027-08-10T09:05:00Z","cwd":"/home/dev/narr-project","gitBranch":"feat/auth","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Done! JWT authentication has been added and committed."}],"usage":{"input_tokens":200,"output_tokens":80,"cache_creation_input_tokens":0,"cache_read_input_tokens":50}}}
`

// fixtureFrictionSession: session with correction patterns. sessionId session-friction-001
const fixtureFrictionSession = `{"type":"user","uuid":"u1","sessionId":"session-friction-001","timestamp":"2027-09-01T10:00:00Z","cwd":"/home/dev/friction-project","gitBranch":"main","message":{"role":"user","content":"Implement the user profile page"}}
{"type":"assistant","uuid":"a1","sessionId":"session-friction-001","timestamp":"2027-09-01T10:01:00Z","cwd":"/home/dev/friction-project","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"I'll create the profile page with a basic layout."},{"type":"tool_use","id":"ftu1","name":"Write","input":{"file_path":"/home/dev/friction-project/profile.tsx","content":"// profile page"}}],"usage":{"input_tokens":500,"output_tokens":200,"cache_creation_input_tokens":100,"cache_read_input_tokens":50}}}
{"type":"user","uuid":"u1r","sessionId":"session-friction-001","timestamp":"2027-09-01T10:01:30Z","cwd":"/home/dev/friction-project","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"ftu1","content":"File created successfully"}]}}
{"type":"user","uuid":"u2","sessionId":"session-friction-001","timestamp":"2027-09-01T10:02:00Z","cwd":"/home/dev/friction-project","gitBranch":"main","message":{"role":"user","content":"No, that's not what I wanted. Use a card layout instead."}}
{"type":"assistant","uuid":"a2","sessionId":"session-friction-001","timestamp":"2027-09-01T10:03:00Z","cwd":"/home/dev/friction-project","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Sorry about that. Let me redo it with a card layout."},{"type":"tool_use","id":"ftu2","name":"Edit","input":{"file_path":"/home/dev/friction-project/profile.tsx","old_string":"// profile page","new_string":"// card layout profile"}}],"usage":{"input_tokens":400,"output_tokens":150,"cache_creation_input_tokens":0,"cache_read_input_tokens":200}}}
{"type":"user","uuid":"u2r","sessionId":"session-friction-001","timestamp":"2027-09-01T10:03:30Z","cwd":"/home/dev/friction-project","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"ftu2","content":"File edited successfully"}]}}
{"type":"user","uuid":"u3","sessionId":"session-friction-001","timestamp":"2027-09-01T10:04:00Z","cwd":"/home/dev/friction-project","gitBranch":"main","message":{"role":"user","content":"Actually, I meant to use the existing UserCard component"}}
{"type":"assistant","uuid":"a3","sessionId":"session-friction-001","timestamp":"2027-09-01T10:05:00Z","cwd":"/home/dev/friction-project","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Using the existing UserCard component now."},{"type":"tool_use","id":"ftu3","name":"Edit","input":{"file_path":"/home/dev/friction-project/profile.tsx","old_string":"// card layout profile","new_string":"<UserCard />"}}],"usage":{"input_tokens":300,"output_tokens":100,"cache_creation_input_tokens":0,"cache_read_input_tokens":100}}}
{"type":"user","uuid":"u3r","sessionId":"session-friction-001","timestamp":"2027-09-01T10:05:30Z","cwd":"/home/dev/friction-project","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"ftu3","content":"File edited successfully"}]}}
{"type":"assistant","uuid":"a4","sessionId":"session-friction-001","timestamp":"2027-09-01T10:06:00Z","cwd":"/home/dev/friction-project","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Done! The profile page now uses the existing UserCard component."}],"usage":{"input_tokens":200,"output_tokens":80,"cache_creation_input_tokens":0,"cache_read_input_tokens":50}}}
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
	narrPath := writeFixture(t, fixtureDir, "session-narr-001.jsonl", fixtureNarrativeSession)
	frictionPath := writeFixture(t, fixtureDir, "session-friction-001.jsonl", fixtureFrictionSession)

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
		assertContains(t, stdout, "Done", "init stdout")
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

	// 7b. stats
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
		writeFixture(t, projectDir, "abcdef01-2345-6789-abcd-ef0123456789.jsonl", fixtureBackfill)

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

	// 10d. context init + migrate
	t.Run("context_init_and_migrate", func(t *testing.T) {
		// Use a temp dir as the "repo" for context commands
		repoCwd := t.TempDir()

		// context init (run from repoCwd)
		stdout := mustRunVVInDir(t, env, repoCwd, "context", "init", "--project", "ctx-project")
		assertContains(t, stdout, "Context initialized", "context init stdout")
		assertContains(t, stdout, "ctx-project", "context init project name")

		// Vault files created
		if !fileExists(filepath.Join(vaultPath, "Projects", "ctx-project", "resume.md")) {
			t.Error("vault resume.md not created by context init")
		}
		if !fileExists(filepath.Join(vaultPath, "Projects", "ctx-project", "iterations.md")) {
			t.Error("vault iterations.md not created by context init")
		}
		if !dirExists(filepath.Join(vaultPath, "Projects", "ctx-project", "tasks")) {
			t.Error("vault tasks/ not created by context init")
		}
		if !dirExists(filepath.Join(vaultPath, "Projects", "ctx-project", "tasks", "done")) {
			t.Error("vault tasks/done/ not created by context init")
		}

		// Repo files created
		if !fileExists(filepath.Join(repoCwd, "CLAUDE.md")) {
			t.Error("repo CLAUDE.md not created by context init")
		}
		if !fileExists(filepath.Join(repoCwd, ".claude", "commands", "restart.md")) {
			t.Error("repo restart.md not created by context init")
		}

		// CLAUDE.md has vault pointer
		claudeContent := readFile(t, filepath.Join(repoCwd, "CLAUDE.md"))
		assertContains(t, claudeContent, "resume.md", "CLAUDE.md vault pointer")

		// context migrate — set up local files in a different temp dir
		migrateDir := t.TempDir()
		writeFixture(t, migrateDir, "RESUME.md", "# My Resume\nProject state.")
		writeFixture(t, migrateDir, "HISTORY.md", "# Iteration History")
		writeFixture(t, filepath.Join(migrateDir, "tasks"), "001-feature.md", "Feature task")

		migrateStdout := mustRunVVInDir(t, env, migrateDir, "context", "migrate", "--project", "migrate-test")
		assertContains(t, migrateStdout, "Context migrated", "context migrate stdout")

		// Verify vault contents from migrate
		migrateResume := filepath.Join(vaultPath, "Projects", "migrate-test", "resume.md")
		if !fileExists(migrateResume) {
			t.Error("vault resume.md not created by context migrate")
		} else {
			content := readFile(t, migrateResume)
			assertContains(t, content, "My Resume", "migrated resume content")
		}

		migrateIter := filepath.Join(vaultPath, "Projects", "migrate-test", "iterations.md")
		if !fileExists(migrateIter) {
			t.Error("vault iterations.md not created by context migrate")
		} else {
			content := readFile(t, migrateIter)
			assertContains(t, content, "Iteration History", "migrated iterations content")
		}

		// Migrated task file in vault
		migrateTask := filepath.Join(vaultPath, "Projects", "migrate-test", "tasks", "001-feature.md")
		if !fileExists(migrateTask) {
			t.Error("vault task not created by context migrate")
		} else {
			content := readFile(t, migrateTask)
			assertContains(t, content, "Feature task", "migrated task content")
		}

		// Local originals preserved
		if !fileExists(filepath.Join(migrateDir, "RESUME.md")) {
			t.Error("local RESUME.md deleted by migrate")
		}
		if !fileExists(filepath.Join(migrateDir, "HISTORY.md")) {
			t.Error("local HISTORY.md deleted by migrate")
		}

		// Repo-side files updated by migrate
		migrateClaudeContent := readFile(t, filepath.Join(migrateDir, "CLAUDE.md"))
		assertContains(t, migrateClaudeContent, "resume.md", "migrated CLAUDE.md vault pointer")

		// Help flags work
		_, helpStderr, _ := runVV(t, env, "context", "--help")
		assertContains(t, helpStderr, "context", "context help text")

		_, initHelpStderr, _ := runVV(t, env, "context", "init", "--help")
		assertContains(t, initHelpStderr, "init", "context init help text")

		_, migrateHelpStderr, _ := runVV(t, env, "context", "migrate", "--help")
		assertContains(t, migrateHelpStderr, "migrate", "context migrate help text")
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
}
