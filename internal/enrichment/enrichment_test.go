package enrichment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/johns/vibe-vault/internal/config"
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
		UserText:     "implement the thing",
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
	resp := chatResponse{
		Choices: []chatChoice{
			{
				Message: chatMessage{
					Role: "assistant",
					Content: `{
						"summary": "Implemented user authentication with JWT tokens.",
						"decisions": ["Used JWT over sessions — stateless and scalable"],
						"open_threads": ["Add refresh token rotation"],
						"tag": "implementation"
					}`,
				},
			},
		},
	}

	body, _ := json.Marshal(resp)
	result, err := parseResponse(body)
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

func TestParseResponse_EmptyChoices(t *testing.T) {
	resp := chatResponse{Choices: []chatChoice{}}
	body, _ := json.Marshal(resp)
	_, err := parseResponse(body)
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
	if !strings.Contains(err.Error(), "empty choices") {
		t.Errorf("wrong error: %v", err)
	}
}

func TestParseResponse_BadJSON(t *testing.T) {
	resp := chatResponse{
		Choices: []chatChoice{
			{Message: chatMessage{Content: "not json at all"}},
		},
	}
	body, _ := json.Marshal(resp)
	_, err := parseResponse(body)
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

func TestGenerate_Disabled(t *testing.T) {
	cfg := config.EnrichmentConfig{Enabled: false}
	result, err := Generate(context.Background(), cfg, PromptInput{})
	if result != nil || err != nil {
		t.Errorf("disabled: got result=%v, err=%v", result, err)
	}
}

func TestGenerate_NoAPIKey(t *testing.T) {
	cfg := config.EnrichmentConfig{
		Enabled:   true,
		APIKeyEnv: "VV_TEST_NONEXISTENT_KEY_12345",
	}
	result, err := Generate(context.Background(), cfg, PromptInput{})
	if result != nil || err != nil {
		t.Errorf("no key: got result=%v, err=%v", result, err)
	}
}

func TestGenerate_MockServer(t *testing.T) {
	cannedResponse := chatResponse{
		Choices: []chatChoice{
			{
				Message: chatMessage{
					Role: "assistant",
					Content: `{"summary":"Built enrichment pipeline.","decisions":["Raw HTTP over SDK — fewer deps"],"open_threads":["Add retry logic"],"tag":"implementation"}`,
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request structure
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s, want POST", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-key-123" {
			t.Errorf("auth: got %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type: got %q", r.Header.Get("Content-Type"))
		}

		// Verify request body
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Temperature != 0.3 {
			t.Errorf("temperature: got %f, want 0.3", req.Temperature)
		}
		if req.ResponseFormat == nil || req.ResponseFormat.Type != "json_object" {
			t.Error("missing response_format")
		}
		if len(req.Messages) != 2 {
			t.Errorf("messages: got %d, want 2", len(req.Messages))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cannedResponse)
	}))
	defer server.Close()

	t.Setenv("VV_TEST_KEY", "test-key-123")

	cfg := config.EnrichmentConfig{
		Enabled:   true,
		APIKeyEnv: "VV_TEST_KEY",
		Model:     "test-model",
		BaseURL:   server.URL,
	}

	input := PromptInput{
		UserText:     "implement enrichment",
		AssistantText: "done",
		Duration:      5,
		UserMessages:  2,
		AsstMessages:  2,
	}

	result, err := Generate(context.Background(), cfg, input)
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
}

func TestGenerate_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay longer than the client timeout
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	t.Setenv("VV_TEST_KEY_TIMEOUT", "test-key")

	cfg := config.EnrichmentConfig{
		Enabled:   true,
		APIKeyEnv: "VV_TEST_KEY_TIMEOUT",
		Model:     "test-model",
		BaseURL:   server.URL,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := Generate(ctx, cfg, PromptInput{UserText: "test"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestGenerate_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"rate limit exceeded"}}`))
	}))
	defer server.Close()

	t.Setenv("VV_TEST_KEY_429", "test-key")

	cfg := config.EnrichmentConfig{
		Enabled:   true,
		APIKeyEnv: "VV_TEST_KEY_429",
		Model:     "test-model",
		BaseURL:   server.URL,
	}

	_, err := Generate(context.Background(), cfg, PromptInput{UserText: "test"})
	if err == nil {
		t.Fatal("expected error for 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should mention status code: %v", err)
	}
}
