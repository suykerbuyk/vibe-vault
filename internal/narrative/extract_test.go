package narrative

import (
	"testing"
	"time"

	"github.com/johns/vibe-vault/internal/transcript"
)

func makeEntry(role, content string, toolUses ...transcript.ContentBlock) transcript.Entry {
	var msgContent interface{} = content
	if len(toolUses) > 0 {
		blocks := []interface{}{
			map[string]interface{}{"type": "text", "text": content},
		}
		for _, tu := range toolUses {
			blocks = append(blocks, map[string]interface{}{
				"type":  "tool_use",
				"id":    tu.ID,
				"name":  tu.Name,
				"input": tu.Input,
			})
		}
		msgContent = blocks
	}

	return transcript.Entry{
		Type:      role,
		Timestamp: time.Date(2027, 6, 15, 10, 0, 0, 0, time.UTC),
		CWD:       "/home/dev/project",
		Message: &transcript.Message{
			Role:    role,
			Content: msgContent,
		},
	}
}

func makeToolResult(toolUseID string, isError bool, content string) transcript.Entry {
	return transcript.Entry{
		Type:      "user",
		Timestamp: time.Date(2027, 6, 15, 10, 0, 1, 0, time.UTC),
		Message: &transcript.Message{
			Role: "user",
			Content: []interface{}{
				map[string]interface{}{
					"type":        "tool_result",
					"tool_use_id": toolUseID,
					"is_error":    isError,
					"content":     content,
				},
			},
		},
	}
}

func TestExtract_NilTranscript(t *testing.T) {
	narr := Extract(nil, "/tmp")
	if narr != nil {
		t.Error("expected nil for nil transcript")
	}
}

func TestExtract_EmptyTranscript(t *testing.T) {
	narr := Extract(&transcript.Transcript{}, "/tmp")
	if narr != nil {
		t.Error("expected nil for empty transcript")
	}
}

func TestClassifyToolUse_Write(t *testing.T) {
	tu := transcript.ContentBlock{
		Type: "tool_use",
		ID:   "tu1",
		Name: "Write",
		Input: map[string]interface{}{
			"file_path": "/home/dev/project/src/handler.go",
		},
	}
	act := classifyToolUse(tu, nil, time.Now(), "/home/dev/project")
	if act == nil {
		t.Fatal("expected activity")
	}
	if act.Kind != KindFileCreate {
		t.Errorf("kind: got %d, want KindFileCreate", act.Kind)
	}
	if act.Description != "Created `src/handler.go`" {
		t.Errorf("description: got %q", act.Description)
	}
}

func TestClassifyToolUse_Edit(t *testing.T) {
	tu := transcript.ContentBlock{
		Type: "tool_use",
		ID:   "tu1",
		Name: "Edit",
		Input: map[string]interface{}{
			"file_path": "/home/dev/project/src/handler.go",
		},
	}
	act := classifyToolUse(tu, nil, time.Now(), "/home/dev/project")
	if act == nil {
		t.Fatal("expected activity")
	}
	if act.Kind != KindFileModify {
		t.Errorf("kind: got %d, want KindFileModify", act.Kind)
	}
}

func TestClassifyToolUse_Read(t *testing.T) {
	tu := transcript.ContentBlock{
		Type: "tool_use",
		ID:   "tu1",
		Name: "Read",
	}
	act := classifyToolUse(tu, nil, time.Now(), "/tmp")
	if act == nil {
		t.Fatal("expected activity")
	}
	if act.Kind != KindExplore {
		t.Errorf("kind: got %d, want KindExplore", act.Kind)
	}
}

func TestClassifyToolUse_PlanMode(t *testing.T) {
	tu := transcript.ContentBlock{
		Type: "tool_use",
		ID:   "tu1",
		Name: "EnterPlanMode",
	}
	act := classifyToolUse(tu, nil, time.Now(), "/tmp")
	if act == nil {
		t.Fatal("expected activity")
	}
	if act.Kind != KindPlanMode {
		t.Errorf("kind: got %d, want KindPlanMode", act.Kind)
	}
	if act.Description != "Entered plan mode" {
		t.Errorf("description: got %q", act.Description)
	}
}

func TestClassifyToolUse_AskUserQuestion(t *testing.T) {
	tu := transcript.ContentBlock{
		Type: "tool_use",
		ID:   "tu1",
		Name: "AskUserQuestion",
		Input: map[string]interface{}{
			"questions": []interface{}{
				map[string]interface{}{
					"question": "Which database should we use?",
				},
			},
		},
	}
	act := classifyToolUse(tu, nil, time.Now(), "/tmp")
	if act == nil {
		t.Fatal("expected activity")
	}
	if act.Kind != KindDecision {
		t.Errorf("kind: got %d, want KindDecision", act.Kind)
	}
	if act.Description != "Decision: Which database should we use?" {
		t.Errorf("description: got %q", act.Description)
	}
}

func TestClassifyToolUse_Task(t *testing.T) {
	tu := transcript.ContentBlock{
		Type: "tool_use",
		ID:   "tu1",
		Name: "Task",
		Input: map[string]interface{}{
			"description": "Research auth patterns",
		},
	}
	act := classifyToolUse(tu, nil, time.Now(), "/tmp")
	if act == nil {
		t.Fatal("expected activity")
	}
	if act.Kind != KindDelegation {
		t.Errorf("kind: got %d, want KindDelegation", act.Kind)
	}
}

func TestClassifyBash_TestCommand(t *testing.T) {
	act := ClassifyBashCommand("go test ./...", nil, time.Now(), "/tmp")
	if act.Kind != KindTestRun {
		t.Errorf("kind: got %d, want KindTestRun", act.Kind)
	}
	if act.Description != "Ran tests (success)" {
		t.Errorf("description: got %q", act.Description)
	}
}

func TestClassifyBash_TestFailed(t *testing.T) {
	result := &ToolResult{IsError: true, Content: "FAIL"}
	act := ClassifyBashCommand("go test ./...", result, time.Now(), "/tmp")
	if act.Kind != KindTestRun {
		t.Errorf("kind: got %d, want KindTestRun", act.Kind)
	}
	if !act.IsError {
		t.Error("expected IsError=true")
	}
	if act.Description != "Ran tests (failed)" {
		t.Errorf("description: got %q", act.Description)
	}
}

func TestClassifyBash_GitCommit(t *testing.T) {
	act := ClassifyBashCommand(`git commit -m "feat: add auth"`, nil, time.Now(), "/tmp")
	if act.Kind != KindGitCommit {
		t.Errorf("kind: got %d, want KindGitCommit", act.Kind)
	}
	if act.Description != `Committed: "feat: add auth"` {
		t.Errorf("description: got %q", act.Description)
	}
}

func TestClassifyBash_GitPush(t *testing.T) {
	act := ClassifyBashCommand("git push origin main", nil, time.Now(), "/tmp")
	if act.Kind != KindGitPush {
		t.Errorf("kind: got %d, want KindGitPush", act.Kind)
	}
}

func TestClassifyBash_Build(t *testing.T) {
	act := ClassifyBashCommand("make build", nil, time.Now(), "/tmp")
	if act.Kind != KindBuild {
		t.Errorf("kind: got %d, want KindBuild", act.Kind)
	}
}

func TestClassifyBash_GeneralCommand(t *testing.T) {
	act := ClassifyBashCommand("ls -la", nil, time.Now(), "/tmp")
	if act.Kind != KindCommand {
		t.Errorf("kind: got %d, want KindCommand", act.Kind)
	}
}

func TestIsTestCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"go test ./...", true},
		{"npm test", true},
		{"make test", true},
		{"make integration", true},
		{"pytest -v", true},
		{"cargo test", true},
		{"bun test", true},
		{"ls -la", false},
		{"git status", false},
	}
	for _, tc := range tests {
		got := isTestCommand(tc.cmd)
		if got != tc.want {
			t.Errorf("isTestCommand(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}

func TestAggregateExploration(t *testing.T) {
	activities := []Activity{
		{Kind: KindExplore, Tool: "Read"},
		{Kind: KindExplore, Tool: "Grep"},
		{Kind: KindExplore, Tool: "Glob"},
		{Kind: KindFileCreate, Description: "Created `handler.go`"},
		{Kind: KindExplore, Tool: "Read"},
	}
	result := aggregateExploration(activities)
	if len(result) != 3 {
		t.Fatalf("expected 3 activities, got %d", len(result))
	}
	if result[0].Kind != KindExplore {
		t.Errorf("first should be aggregated explore")
	}
	if result[0].Description != "Explored codebase (3 lookups)" {
		t.Errorf("description: got %q", result[0].Description)
	}
	if result[1].Kind != KindFileCreate {
		t.Errorf("second should be file create")
	}
	if result[2].Kind != KindExplore {
		t.Errorf("third should be explore")
	}
}

func TestDetectRecoveries(t *testing.T) {
	activities := []Activity{
		{Kind: KindTestRun, IsError: true, Description: "Ran tests (failed)"},
		{Kind: KindFileModify, Description: "Modified handler.go"},
		{Kind: KindTestRun, IsError: false, Description: "Ran tests (success)"},
	}
	detectRecoveries(activities)
	if !activities[0].Recovered {
		t.Error("first test error should be marked as recovered")
	}
}

func TestDetectRecoveries_NoRecovery(t *testing.T) {
	activities := []Activity{
		{Kind: KindTestRun, IsError: true, Description: "Ran tests (failed)"},
		{Kind: KindFileCreate, Description: "Created file"},
		{Kind: KindFileCreate, Description: "Created another"},
		{Kind: KindFileCreate, Description: "Created yet another"},
		{Kind: KindTestRun, IsError: false, Description: "Ran tests (success)"},
	}
	detectRecoveries(activities)
	if activities[0].Recovered {
		t.Error("recovery too far away should not be detected")
	}
}

func TestExtractCommitMessage(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{`git commit -m "feat: add auth"`, "feat: add auth"},
		{`git commit -m 'fix: bug'`, "fix: bug"},
		{`git commit --allow-empty -m "test"`, "test"},
		{`git commit`, ""},
	}
	for _, tc := range tests {
		got := extractCommitMessage(tc.cmd)
		if got != tc.want {
			t.Errorf("extractCommitMessage(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

func TestBuildToolResultMap(t *testing.T) {
	entries := []transcript.Entry{
		makeToolResult("tu1", false, "success output"),
		makeToolResult("tu2", true, "error output"),
	}
	m := BuildToolResultMap(entries)
	if len(m) != 2 {
		t.Fatalf("expected 2 results, got %d", len(m))
	}
	if m["tu1"].IsError {
		t.Error("tu1 should not be error")
	}
	if !m["tu2"].IsError {
		t.Error("tu2 should be error")
	}
}

func TestFirstUserRequest_SkipsNoise(t *testing.T) {
	entries := []transcript.Entry{
		makeEntry("user", "/restart"),
		makeEntry("user", "ok"),
		makeEntry("user", "Implement the login page"),
	}
	got := firstUserRequest(entries)
	if got != "Implement the login page" {
		t.Errorf("got %q, want %q", got, "Implement the login page")
	}
}

func TestFirstUserRequest_SkipsMeta(t *testing.T) {
	meta := makeEntry("user", "System context loading...")
	meta.IsMeta = true
	entries := []transcript.Entry{
		meta,
		makeEntry("user", "Fix the auth bug"),
	}
	got := firstUserRequest(entries)
	if got != "Fix the auth bug" {
		t.Errorf("got %q, want %q", got, "Fix the auth bug")
	}
}

func TestShortenPath(t *testing.T) {
	tests := []struct {
		path, cwd, want string
	}{
		{"/home/dev/project/src/main.go", "/home/dev/project", "src/main.go"},
		{"/other/path/file.go", "/home/dev/project", "/other/path/file.go"},
		{"relative.go", "", "relative.go"},
	}
	for _, tc := range tests {
		got := shortenPath(tc.path, tc.cwd)
		if got != tc.want {
			t.Errorf("shortenPath(%q, %q) = %q, want %q", tc.path, tc.cwd, got, tc.want)
		}
	}
}

func TestTruncateStr(t *testing.T) {
	if got := truncateStr("short", 10); got != "short" {
		t.Errorf("got %q", got)
	}
	if got := truncateStr("this is a very long string", 15); got != "this is a ve..." {
		t.Errorf("got %q", got)
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("line1\nline2"); got != "line1" {
		t.Errorf("got %q", got)
	}
	if got := firstLine("single"); got != "single" {
		t.Errorf("got %q", got)
	}
}
