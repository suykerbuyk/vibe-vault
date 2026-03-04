package enrichment

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/johns/vibe-vault/internal/llm"
)

func TestTruncate(t *testing.T) {
	// Short text — no truncation
	short := "hello world"
	if got := truncate(short, 100); got != short {
		t.Errorf("short: got %q, want %q", got, short)
	}

	// Exact length — no truncation
	exact := strings.Repeat("a", 50)
	if got := truncate(exact, 50); got != exact {
		t.Errorf("exact: got %q, want %q", got, exact)
	}

	// Over limit — breaks at newline
	lines := "line one\nline two\nline three\nline four\nline five"
	got := truncate(lines, 30)
	if !strings.HasSuffix(got, "\n[...truncated]") {
		t.Errorf("over-limit: expected truncation suffix, got %q", got)
	}
	if strings.Contains(got, "line five") {
		t.Errorf("over-limit: should not contain final line, got %q", got)
	}
}

func TestBuildMessages(t *testing.T) {
	input := PromptInput{
		UserText:      "implement the thing",
		AssistantText: "I'll implement that for you",
		FilesChanged:  []string{"main.go", "util.go"},
		ToolCounts:    map[string]int{"Read": 3, "Edit": 2, "Bash": 1},
		Duration:      15,
		UserMessages:  5,
		AsstMessages:  4,
	}

	msgs := buildMessages(input)

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	if msgs[0].Role != "system" {
		t.Errorf("first message role: got %q, want %q", msgs[0].Role, "system")
	}

	if msgs[1].Role != "user" {
		t.Errorf("second message role: got %q, want %q", msgs[1].Role, "user")
	}

	userPrompt := msgs[1].Content

	// Check metadata present
	if !strings.Contains(userPrompt, "Duration: 15 minutes") {
		t.Error("missing duration in prompt")
	}
	if !strings.Contains(userPrompt, "User messages: 5") {
		t.Error("missing user message count")
	}

	// Check tool counts are sorted
	bashIdx := strings.Index(userPrompt, "Bash: 1")
	editIdx := strings.Index(userPrompt, "Edit: 2")
	readIdx := strings.Index(userPrompt, "Read: 3")
	if bashIdx == -1 || editIdx == -1 || readIdx == -1 {
		t.Error("missing tool counts in prompt")
	}
	if bashIdx > editIdx || editIdx > readIdx {
		t.Error("tool counts not sorted alphabetically")
	}

	// Check files present
	if !strings.Contains(userPrompt, "main.go") || !strings.Contains(userPrompt, "util.go") {
		t.Error("missing files in prompt")
	}

	// Check transcript sections present
	if !strings.Contains(userPrompt, "## User Messages") {
		t.Error("missing user messages section")
	}
	if !strings.Contains(userPrompt, "## Assistant Messages") {
		t.Error("missing assistant messages section")
	}
}

func TestParseResponse(t *testing.T) {
	content := `{
		"summary": "Implemented user authentication with JWT tokens.",
		"decisions": ["Used JWT over sessions — stateless and scalable"],
		"open_threads": ["Add refresh token rotation"],
		"tag": "implementation"
	}`

	result, err := parseResponse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Summary != "Implemented user authentication with JWT tokens." {
		t.Errorf("summary: got %q", result.Summary)
	}
	if len(result.Decisions) != 1 || result.Decisions[0] != "Used JWT over sessions — stateless and scalable" {
		t.Errorf("decisions: got %v", result.Decisions)
	}
	if len(result.OpenThreads) != 1 || result.OpenThreads[0] != "Add refresh token rotation" {
		t.Errorf("open_threads: got %v", result.OpenThreads)
	}
	if result.Tag != "implementation" {
		t.Errorf("tag: got %q", result.Tag)
	}
}

func TestParseResponse_BadJSON(t *testing.T) {
	_, err := parseResponse("not json at all")
	if err == nil {
		t.Fatal("expected error for bad JSON content")
	}
}

func TestValidateTag(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"implementation", "implementation"},
		{"DEBUGGING", "debugging"},
		{"  planning  ", "planning"},
		{"invalid", ""},
		{"Implementation Details", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := validateTag(tt.input)
		if got != tt.want {
			t.Errorf("validateTag(%q): got %q, want %q", tt.input, got, tt.want)
		}
	}
}

// mockProvider implements llm.Provider for testing.
type mockProvider struct {
	response *llm.Response
	err      error
	calls    int
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) ChatCompletion(_ context.Context, _ llm.Request) (*llm.Response, error) {
	m.calls++
	return m.response, m.err
}

func TestGenerate_NilProvider(t *testing.T) {
	result, err := Generate(context.Background(), nil, PromptInput{})
	if result != nil || err != nil {
		t.Errorf("nil provider: got result=%v, err=%v", result, err)
	}
}

func TestGenerate_MockProvider(t *testing.T) {
	mock := &mockProvider{
		response: &llm.Response{
			Content: `{"summary":"Built enrichment pipeline.","decisions":["Raw HTTP over SDK — fewer deps"],"open_threads":["Add retry logic"],"tag":"implementation"}`,
		},
	}

	input := PromptInput{
		UserText:     "implement enrichment",
		AssistantText: "done",
		Duration:      5,
		UserMessages:  2,
		AsstMessages:  2,
	}

	result, err := Generate(context.Background(), mock, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}
	if result.Summary != "Built enrichment pipeline." {
		t.Errorf("summary: got %q", result.Summary)
	}
	if result.Tag != "implementation" {
		t.Errorf("tag: got %q", result.Tag)
	}
	if mock.calls != 1 {
		t.Errorf("expected 1 call, got %d", mock.calls)
	}
}

func TestGenerate_ProviderError(t *testing.T) {
	mock := &mockProvider{
		err: fmt.Errorf("connection refused"),
	}

	_, err := Generate(context.Background(), mock, PromptInput{UserText: "test"})
	if err == nil {
		t.Fatal("expected error from provider")
	}
}
