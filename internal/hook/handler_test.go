package hook

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johns/vibe-vault/internal/config"
)

// minimalTranscript is a JSONL transcript with enough messages to not be skipped as trivial.
const minimalTranscript = `{"type":"user","uuid":"a","timestamp":"2026-02-22T10:00:00Z","sessionId":"test-sess","cwd":"/tmp/proj","gitBranch":"main","message":{"role":"user","content":"Implement feature X"}}
{"type":"assistant","uuid":"b","timestamp":"2026-02-22T10:00:05Z","sessionId":"test-sess","cwd":"/tmp/proj","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"I'll implement feature X."}],"usage":{"input_tokens":100,"output_tokens":50}}}
{"type":"user","uuid":"c","timestamp":"2026-02-22T10:00:10Z","sessionId":"test-sess","cwd":"/tmp/proj","gitBranch":"main","message":{"role":"user","content":"Looks good, thanks"}}
{"type":"assistant","uuid":"d","timestamp":"2026-02-22T10:01:00Z","sessionId":"test-sess","cwd":"/tmp/proj","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Done!"}],"usage":{"input_tokens":80,"output_tokens":20}}}`

// minimalTranscriptWithTools has tool uses, so it passes the checkpoint trivial filter.
const minimalTranscriptWithTools = `{"type":"user","uuid":"a","timestamp":"2026-02-22T10:00:00Z","sessionId":"test-sess","cwd":"/tmp/proj","gitBranch":"main","message":{"role":"user","content":"Implement feature X"}}
{"type":"assistant","uuid":"b","timestamp":"2026-02-22T10:00:05Z","sessionId":"test-sess","cwd":"/tmp/proj","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"I'll implement feature X."},{"type":"tool_use","id":"tu1","name":"Write","input":{"file_path":"/tmp/proj/x.go","content":"package x"}}],"usage":{"input_tokens":100,"output_tokens":50}}}
{"type":"user","uuid":"c","timestamp":"2026-02-22T10:00:10Z","sessionId":"test-sess","cwd":"/tmp/proj","gitBranch":"main","message":{"role":"user","content":"Looks good, thanks"}}
{"type":"assistant","uuid":"d","timestamp":"2026-02-22T10:01:00Z","sessionId":"test-sess","cwd":"/tmp/proj","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"Done!"}],"usage":{"input_tokens":80,"output_tokens":20}}}`

func testConfig(t *testing.T) config.Config {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.VaultPath = filepath.Join(t.TempDir(), "vault")
	cfg.Enrichment.Enabled = false
	return cfg
}

func writeTranscript(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHandleInput_SessionEnd(t *testing.T) {
	cfg := testConfig(t)
	transcriptPath := writeTranscript(t, minimalTranscript)

	input := &Input{
		SessionID:      "test-sess",
		TranscriptPath: transcriptPath,
		HookEventName:  "SessionEnd",
		CWD:            "/tmp/proj",
	}

	if err := handleInput(input, "", cfg); err != nil {
		t.Fatalf("handleInput: %v", err)
	}

	// Verify the note was written
	projDir :=filepath.Join(cfg.VaultPath, "Projects")
	entries, err := os.ReadDir(projDir)
	if err != nil {
		t.Fatalf("read projects dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected session note directory to be created")
	}
}

func TestHandleInput_SessionEnd_MissingTranscript(t *testing.T) {
	cfg := testConfig(t)
	input := &Input{
		SessionID:     "test-sess",
		HookEventName: "SessionEnd",
		CWD:           "/tmp/proj",
		// TranscriptPath intentionally empty
	}

	err := handleInput(input, "", cfg)
	if err == nil {
		t.Fatal("expected error for missing transcript path")
	}
}

func TestHandleInput_EventOverride(t *testing.T) {
	cfg := testConfig(t)
	input := &Input{
		SessionID:     "test-sess",
		HookEventName: "SessionEnd", // would normally trigger session capture
	}

	// Override to Stop — no transcript, so returns nil silently
	err := handleInput(input, "Stop", cfg)
	if err != nil {
		t.Fatalf("handleInput with Stop override: %v", err)
	}
}

func TestHandleInput_ClearReason(t *testing.T) {
	cfg := testConfig(t)
	input := &Input{
		SessionID:     "test-sess",
		HookEventName: "SessionEnd",
		Reason:        "clear",
	}

	err := handleInput(input, "", cfg)
	if err != nil {
		t.Fatalf("handleInput with clear reason: %v", err)
	}
}

func TestHandleInput_StopEvent(t *testing.T) {
	cfg := testConfig(t)
	transcriptPath := writeTranscript(t, minimalTranscriptWithTools)

	input := &Input{
		SessionID:      "test-sess-stop",
		TranscriptPath: transcriptPath,
		HookEventName:  "Stop",
		CWD:            "/tmp/proj",
	}

	if err := handleInput(input, "", cfg); err != nil {
		t.Fatalf("handleInput Stop: %v", err)
	}

	// Verify a checkpoint note was created
	projDir :=filepath.Join(cfg.VaultPath, "Projects")
	entries, err := os.ReadDir(projDir)
	if err != nil {
		t.Fatalf("read projects dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected checkpoint note directory to be created")
	}
}

func TestHandleInput_StopThenSessionEnd(t *testing.T) {
	cfg := testConfig(t)
	transcriptPath := writeTranscript(t, minimalTranscriptWithTools)

	// Stop creates checkpoint
	stopInput := &Input{
		SessionID:      "test-sess",
		TranscriptPath: transcriptPath,
		HookEventName:  "Stop",
		CWD:            "/tmp/proj",
	}
	if err := handleInput(stopInput, "", cfg); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// SessionEnd finalizes
	endInput := &Input{
		SessionID:      "test-sess",
		TranscriptPath: transcriptPath,
		HookEventName:  "SessionEnd",
		CWD:            "/tmp/proj",
	}
	if err := handleInput(endInput, "", cfg); err != nil {
		t.Fatalf("SessionEnd: %v", err)
	}

	// Verify only one note file exists (overwritten, not duplicated)
	projDir :=filepath.Join(cfg.VaultPath, "Projects")
	var noteFiles []string
	filepath.Walk(projDir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Ext(path) == ".md" {
			noteFiles = append(noteFiles, path)
		}
		return nil
	})
	if len(noteFiles) != 1 {
		t.Errorf("expected 1 note file, got %d: %v", len(noteFiles), noteFiles)
	}

	// Read the note and verify status is completed (finalized)
	if len(noteFiles) > 0 {
		data, err := os.ReadFile(noteFiles[0])
		if err != nil {
			t.Fatalf("read note: %v", err)
		}
		content := string(data)
		if !strings.Contains(content, "status: completed") {
			t.Error("expected status: completed in finalized note")
		}
		if strings.Contains(content, "status: checkpoint") {
			t.Error("finalized note should not have status: checkpoint")
		}
	}
}

func TestHandleInput_StopNoTranscript(t *testing.T) {
	cfg := testConfig(t)
	input := &Input{
		SessionID:     "test-sess",
		HookEventName: "Stop",
		// TranscriptPath intentionally empty
	}

	err := handleInput(input, "", cfg)
	if err != nil {
		t.Fatalf("Stop with no transcript should return nil, got: %v", err)
	}
}

func TestHandleInput_StopMissingFile(t *testing.T) {
	cfg := testConfig(t)
	input := &Input{
		SessionID:      "test-sess",
		TranscriptPath: "/nonexistent/path/transcript.jsonl",
		HookEventName:  "Stop",
	}

	err := handleInput(input, "", cfg)
	if err != nil {
		t.Fatalf("Stop with missing file should return nil, got: %v", err)
	}
}

func TestHandleInput_UnknownEvent(t *testing.T) {
	cfg := testConfig(t)
	input := &Input{
		SessionID:     "test-sess",
		HookEventName: "FooBar",
	}

	err := handleInput(input, "", cfg)
	if err == nil {
		t.Fatal("expected error for unknown event")
	}
	if want := "unknown hook event: FooBar"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestHandleInput_EmptyEvent(t *testing.T) {
	// Empty HookEventName defaults to SessionEnd behavior
	cfg := testConfig(t)
	transcriptPath := writeTranscript(t, minimalTranscript)

	input := &Input{
		SessionID:      "test-sess-empty",
		TranscriptPath: transcriptPath,
		CWD:            "/tmp/proj",
		// HookEventName intentionally empty — should default to SessionEnd
	}

	err := handleInput(input, "", cfg)
	if err != nil {
		t.Fatalf("handleInput empty event: %v", err)
	}
}

func TestInputJSON(t *testing.T) {
	original := Input{
		SessionID:            "sess-123",
		TranscriptPath:       "/home/user/.claude/sessions/abc.jsonl",
		HookEventName:        "SessionEnd",
		CWD:                  "/home/user/project",
		Reason:               "manual",
		LastAssistantMessage: "Done!",
		PermissionMode:       "auto",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Input
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded != original {
		t.Errorf("round-trip mismatch:\n  got:  %+v\n  want: %+v", decoded, original)
	}
}
