package transcript

import (
	"strings"
	"testing"
)

const testTranscript = `{"type":"file-history-snapshot","uuid":"aaa","timestamp":"2026-02-22T10:00:00Z","sessionId":"test-session","cwd":"/home/user/myproject","gitBranch":"main"}
{"type":"user","uuid":"bbb","timestamp":"2026-02-22T10:00:01Z","sessionId":"test-session","cwd":"/home/user/myproject","gitBranch":"main","message":{"role":"user","content":"Implement the login page"}}
{"type":"assistant","uuid":"ccc","timestamp":"2026-02-22T10:00:05Z","sessionId":"test-session","cwd":"/home/user/myproject","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"thinking","thinking":"Let me think about this..."},{"type":"text","text":"I'll implement the login page."},{"type":"tool_use","id":"toolu_1","name":"Write","input":{"file_path":"/home/user/myproject/src/login.tsx","content":"export default function Login() {}"}}],"usage":{"input_tokens":100,"output_tokens":50,"cache_creation_input_tokens":500,"cache_read_input_tokens":200}}}
{"type":"user","uuid":"ddd","timestamp":"2026-02-22T10:00:10Z","sessionId":"test-session","cwd":"/home/user/myproject","gitBranch":"main","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"File written successfully"}]},"toolUseResult":{"stdout":"File written successfully"}}
{"type":"progress","uuid":"eee","timestamp":"2026-02-22T10:00:11Z"}
{"type":"assistant","uuid":"fff","timestamp":"2026-02-22T10:00:15Z","sessionId":"test-session","cwd":"/home/user/myproject","gitBranch":"main","message":{"role":"assistant","model":"claude-opus-4-6","content":[{"type":"text","text":"The login page has been created."}],"usage":{"input_tokens":80,"output_tokens":30,"cache_creation_input_tokens":0,"cache_read_input_tokens":600}}}
{"type":"user","uuid":"ggg","timestamp":"2026-02-22T10:01:00Z","sessionId":"test-session","cwd":"/home/user/myproject","gitBranch":"main","message":{"role":"user","content":"Thanks!"}}`

func TestParse(t *testing.T) {
	tr, err := Parse(strings.NewReader(testTranscript))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// Should skip file-history-snapshot and progress entries
	if len(tr.Entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(tr.Entries))
	}

	// Check stats
	s := tr.Stats
	if s.SessionID != "test-session" {
		t.Errorf("session_id = %q, want %q", s.SessionID, "test-session")
	}
	if s.Model != "claude-opus-4-6" {
		t.Errorf("model = %q, want %q", s.Model, "claude-opus-4-6")
	}
	if s.GitBranch != "main" {
		t.Errorf("branch = %q, want %q", s.GitBranch, "main")
	}
	if s.UserMessages != 2 { // "Implement..." and "Thanks!" (tool result not counted)
		t.Errorf("user_messages = %d, want 2", s.UserMessages)
	}
	if s.AssistantMessages != 2 {
		t.Errorf("assistant_messages = %d, want 2", s.AssistantMessages)
	}
	if s.ToolUses != 1 {
		t.Errorf("tool_uses = %d, want 1", s.ToolUses)
	}
	if s.InputTokens != 180 {
		t.Errorf("input_tokens = %d, want 180", s.InputTokens)
	}
	if s.OutputTokens != 80 {
		t.Errorf("output_tokens = %d, want 80", s.OutputTokens)
	}
	if s.CacheReads != 800 {
		t.Errorf("cache_reads = %d, want 800", s.CacheReads)
	}
	if s.CacheWrites != 500 {
		t.Errorf("cache_writes = %d, want 500", s.CacheWrites)
	}

	// File tracking
	if !s.FilesWritten["/home/user/myproject/src/login.tsx"] {
		t.Error("expected src/login.tsx in files_written")
	}

	// Duration (1 minute)
	if int(s.Duration.Seconds()) != 59 {
		t.Errorf("duration = %v, want ~59s", s.Duration)
	}
}

func TestFirstUserMessage(t *testing.T) {
	tr, _ := Parse(strings.NewReader(testTranscript))
	msg := FirstUserMessage(tr)
	if msg != "Implement the login page" {
		t.Errorf("FirstUserMessage = %q, want %q", msg, "Implement the login page")
	}
}

func TestFirstUserMessage_SkipsResume(t *testing.T) {
	input := `{"type":"user","uuid":"a","timestamp":"2026-02-22T10:00:00Z","sessionId":"s","cwd":"/tmp","message":{"role":"user","content":"Please read @RESUME.md to get context"}}
{"type":"user","uuid":"b","timestamp":"2026-02-22T10:00:01Z","sessionId":"s","cwd":"/tmp","message":{"role":"user","content":"Now implement the auth system"}}`

	tr, _ := Parse(strings.NewReader(input))
	msg := FirstUserMessage(tr)
	if msg != "Now implement the auth system" {
		t.Errorf("FirstUserMessage = %q, want %q", msg, "Now implement the auth system")
	}
}

func TestContentBlocks_StringContent(t *testing.T) {
	msg := &Message{Content: "hello world"}
	blocks := ContentBlocks(msg)
	if len(blocks) != 1 || blocks[0].Text != "hello world" {
		t.Errorf("expected single text block, got %v", blocks)
	}
}

func TestTextContent(t *testing.T) {
	tr, _ := Parse(strings.NewReader(testTranscript))

	// First assistant message
	for _, e := range tr.Entries {
		if e.Message != nil && e.Message.Role == "assistant" {
			text := TextContent(e.Message)
			if text != "I'll implement the login page." {
				t.Errorf("TextContent = %q, want %q", text, "I'll implement the login page.")
			}
			break
		}
	}
}
