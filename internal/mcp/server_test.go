// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"

	"context"
)

func testServer() *Server {
	logger := log.New(io.Discard, "", 0)
	srv := NewServer(ServerInfo{Name: "test-server", Version: "0.1.0"}, logger)
	srv.RegisterTool(Tool{
		Definition: ToolDef{
			Name:        "echo",
			Description: "echoes input",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Msg string `json:"msg"`
			}
			if len(params) > 0 {
				json.Unmarshal(params, &args)
			}
			if args.Msg == "" {
				args.Msg = "empty"
			}
			return args.Msg, nil
		},
	})
	return srv
}

func sendAndReceive(t *testing.T, srv *Server, requests ...string) []Response {
	t.Helper()
	input := strings.Join(requests, "\n") + "\n"
	in := strings.NewReader(input)
	var out strings.Builder
	if err := srv.Serve(context.Background(), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	var responses []Response
	scanner := bufio.NewScanner(strings.NewReader(out.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var resp Response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("unmarshal response: %v\nline: %s", err, line)
		}
		responses = append(responses, resp)
	}
	return responses
}

func TestInitializeHandshake(t *testing.T) {
	srv := testServer()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	resp := responses[0]
	if resp.Error != nil {
		t.Fatalf("unexpected error: %v", resp.Error)
	}

	// Verify result contains server info
	data, _ := json.Marshal(resp.Result)
	var result InitializeResult
	json.Unmarshal(data, &result)
	if result.ServerInfo.Name != "test-server" {
		t.Errorf("server name = %q, want test-server", result.ServerInfo.Name)
	}
	if result.Capabilities.Tools == nil {
		t.Error("expected tools capability")
	}
}

func TestToolsList(t *testing.T) {
	srv := testServer()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	data, _ := json.Marshal(responses[0].Result)
	var result ToolsListResult
	json.Unmarshal(data, &result)
	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "echo" {
		t.Errorf("tool name = %q, want echo", result.Tools[0].Name)
	}
}

func TestToolsCallSuccess(t *testing.T) {
	srv := testServer()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"msg":"hello"}}}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	data, _ := json.Marshal(responses[0].Result)
	var result ToolsCallResult
	json.Unmarshal(data, &result)
	if len(result.Content) != 1 || result.Content[0].Text != "hello" {
		t.Errorf("content = %v, want [{text hello}]", result.Content)
	}
	if result.IsError {
		t.Error("unexpected isError")
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	srv := testServer()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nonexistent"}}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	data, _ := json.Marshal(responses[0].Result)
	var result ToolsCallResult
	json.Unmarshal(data, &result)
	if !result.IsError {
		t.Error("expected isError for unknown tool")
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].Text, "unknown tool") {
		t.Errorf("expected unknown tool error message, got %v", result.Content)
	}
}

func TestUnknownMethod(t *testing.T) {
	srv := testServer()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":5,"method":"foo/bar"}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if responses[0].Error.Code != CodeMethodNotFound {
		t.Errorf("error code = %d, want %d", responses[0].Error.Code, CodeMethodNotFound)
	}
}

func TestInvalidJSON(t *testing.T) {
	srv := testServer()
	responses := sendAndReceive(t, srv, `not json at all`)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].Error == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if responses[0].Error.Code != CodeParseError {
		t.Errorf("error code = %d, want %d", responses[0].Error.Code, CodeParseError)
	}
}

func TestNotificationNoResponse(t *testing.T) {
	srv := testServer()
	// Notification has no ID
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	)
	if len(responses) != 0 {
		t.Errorf("expected 0 responses for notification, got %d", len(responses))
	}
}

func TestStdinCloseCleanShutdown(t *testing.T) {
	srv := testServer()
	// Empty input simulates stdin close
	in := strings.NewReader("")
	var out strings.Builder
	if err := srv.Serve(context.Background(), in, &out); err != nil {
		t.Fatalf("expected clean shutdown, got: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output, got %q", out.String())
	}
}

func TestIDPreserved(t *testing.T) {
	srv := testServer()
	// String ID
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":"abc","method":"tools/list"}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if string(responses[0].ID) != `"abc"` {
		t.Errorf("ID = %s, want \"abc\"", string(responses[0].ID))
	}
}

// --- Prompts tests ---

func testServerWithPrompt() *Server {
	srv := testServer()
	srv.RegisterPrompt(Prompt{
		Definition: PromptDef{
			Name:        "test_prompt",
			Description: "a test prompt",
			Arguments: []PromptArg{
				{Name: "name", Description: "a name", Required: false},
			},
		},
		Handler: func(args map[string]string) (PromptsGetResult, error) {
			text := "hello"
			if name, ok := args["name"]; ok && name != "" {
				text = "hello " + name
			}
			return PromptsGetResult{
				Description: "test prompt result",
				Messages: []PromptMessage{
					{Role: "user", Content: ContentBlock{Type: "text", Text: text}},
				},
			}, nil
		},
	})
	return srv
}

func TestPromptsListEmpty(t *testing.T) {
	srv := testServer() // no prompts registered
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	data, _ := json.Marshal(responses[0].Result)
	var result PromptsListResult
	json.Unmarshal(data, &result)
	if len(result.Prompts) != 0 {
		t.Errorf("expected 0 prompts, got %d", len(result.Prompts))
	}
}

func TestPromptsList(t *testing.T) {
	srv := testServerWithPrompt()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	data, _ := json.Marshal(responses[0].Result)
	var result PromptsListResult
	json.Unmarshal(data, &result)
	if len(result.Prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(result.Prompts))
	}
	if result.Prompts[0].Name != "test_prompt" {
		t.Errorf("prompt name = %q, want test_prompt", result.Prompts[0].Name)
	}
}

func TestPromptsGet(t *testing.T) {
	srv := testServerWithPrompt()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"test_prompt","arguments":{"name":"world"}}}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("unexpected error: %v", responses[0].Error)
	}

	data, _ := json.Marshal(responses[0].Result)
	var result PromptsGetResult
	json.Unmarshal(data, &result)
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Content.Text != "hello world" {
		t.Errorf("text = %q, want 'hello world'", result.Messages[0].Content.Text)
	}
	if result.Messages[0].Role != "user" {
		t.Errorf("role = %q, want user", result.Messages[0].Role)
	}
}

func TestPromptsGetUnknown(t *testing.T) {
	srv := testServerWithPrompt()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/get","params":{"name":"nonexistent"}}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].Error == nil {
		t.Fatal("expected error for unknown prompt")
	}
	if !strings.Contains(responses[0].Error.Message, "unknown prompt") {
		t.Errorf("error message = %q, want to contain 'unknown prompt'", responses[0].Error.Message)
	}
}

func TestPromptsCapabilityAdvertised(t *testing.T) {
	srv := testServerWithPrompt()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	data, _ := json.Marshal(responses[0].Result)
	var result InitializeResult
	json.Unmarshal(data, &result)
	if result.Capabilities.Tools == nil {
		t.Error("expected tools capability")
	}
	if result.Capabilities.Prompts == nil {
		t.Error("expected prompts capability when prompts are registered")
	}
}

func TestPromptsCapabilityOmittedWhenEmpty(t *testing.T) {
	srv := testServer() // no prompts
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	data, _ := json.Marshal(responses[0].Result)
	var result InitializeResult
	json.Unmarshal(data, &result)
	if result.Capabilities.Prompts != nil {
		t.Error("prompts capability should not be advertised when no prompts registered")
	}
}

// --- Defensive protocol tests ---

func TestToolsListEmptyReturnsArray(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	srv := NewServer(ServerInfo{Name: "empty", Version: "0.0.0"}, logger)
	// No tools registered
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	// Marshal and check raw JSON for [] not null
	raw, err := json.Marshal(responses[0].Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	rawStr := string(raw)
	if strings.Contains(rawStr, `"tools":null`) {
		t.Errorf("tools list should serialize as [] not null; got: %s", rawStr)
	}
	if !strings.Contains(rawStr, `"tools":[]`) {
		t.Errorf("expected tools:[], got: %s", rawStr)
	}
}

func TestPromptsListEmptyReturnsEmptyArray(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	srv := NewServer(ServerInfo{Name: "empty", Version: "0.0.0"}, logger)
	// No prompts registered
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"prompts/list"}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	raw, err := json.Marshal(responses[0].Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	rawStr := string(raw)
	if strings.Contains(rawStr, `"prompts":null`) {
		t.Errorf("prompts list should serialize as [] not null; got: %s", rawStr)
	}
	if !strings.Contains(rawStr, `"prompts":[]`) {
		t.Errorf("expected prompts:[], got: %s", rawStr)
	}
}

func TestInitializeLogsClientInfo(t *testing.T) {
	var logBuf strings.Builder
	logger := log.New(&logBuf, "", 0)
	srv := NewServer(ServerInfo{Name: "test-server", Version: "0.1.0"}, logger)
	srv.RegisterTool(Tool{
		Definition: ToolDef{
			Name:        "echo",
			Description: "echoes input",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Handler: func(params json.RawMessage) (string, error) { return "ok", nil },
	})

	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","clientInfo":{"name":"claude-code","version":"2.1.74"}}}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("unexpected error: %v", responses[0].Error)
	}

	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "claude-code") {
		t.Errorf("expected log to contain client name 'claude-code', got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "2.1.74") {
		t.Errorf("expected log to contain client version '2.1.74', got: %s", logOutput)
	}
	if !strings.Contains(logOutput, "2025-03-26") {
		t.Errorf("expected log to contain protocol version '2025-03-26', got: %s", logOutput)
	}
}

func TestClaudeCodeHandshake(t *testing.T) {
	srv := testServerWithPrompt()
	responses := sendAndReceive(t, srv,
		// 1. initialize with 2025-03-26 + roots capability
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{"roots":{}},"clientInfo":{"name":"claude-code","version":"2.1.74"}}}`,
		// 2. notifications/initialized (no response expected)
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		// 3. tools/list
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		// 4. prompts/list
		`{"jsonrpc":"2.0","id":3,"method":"prompts/list","params":{}}`,
	)

	// Should get 3 responses (initialize, tools/list, prompts/list — notification gets none)
	if len(responses) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(responses))
	}

	// Response 1: initialize
	if responses[0].Error != nil {
		t.Fatalf("initialize error: %v", responses[0].Error)
	}
	initData, _ := json.Marshal(responses[0].Result)
	var initResult InitializeResult
	json.Unmarshal(initData, &initResult)
	if initResult.ProtocolVersion != "2025-03-26" {
		t.Errorf("protocol version = %q, want 2025-03-26", initResult.ProtocolVersion)
	}
	if initResult.Capabilities.Tools == nil {
		t.Error("expected tools capability")
	}

	// Response 2: tools/list
	if responses[1].Error != nil {
		t.Fatalf("tools/list error: %v", responses[1].Error)
	}
	toolsData, _ := json.Marshal(responses[1].Result)
	var toolsResult ToolsListResult
	json.Unmarshal(toolsData, &toolsResult)
	if len(toolsResult.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(toolsResult.Tools))
	}

	// Response 3: prompts/list
	if responses[2].Error != nil {
		t.Fatalf("prompts/list error: %v", responses[2].Error)
	}
	promptsData, _ := json.Marshal(responses[2].Result)
	var promptsResult PromptsListResult
	json.Unmarshal(promptsData, &promptsResult)
	if len(promptsResult.Prompts) != 1 {
		t.Errorf("expected 1 prompt, got %d", len(promptsResult.Prompts))
	}
}

func TestResponsesAreNewlineDelimitedJSON(t *testing.T) {
	srv := testServer()
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n") + "\n"

	in := strings.NewReader(input)
	var out strings.Builder
	if err := srv.Serve(context.Background(), in, &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	raw := out.String()
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), raw)
	}

	for i, line := range lines {
		if line == "" {
			t.Errorf("line %d is empty", i)
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("line %d is not valid JSON: %v\n  line: %s", i, err, line)
		}
		// Verify no embedded newlines
		if strings.ContainsAny(line, "\n\r") {
			t.Errorf("line %d contains embedded newlines", i)
		}
	}
}

func TestToolSchemasHaveTypeObject(t *testing.T) {
	srv := testServer()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	)
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}

	data, _ := json.Marshal(responses[0].Result)
	var result ToolsListResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, tool := range result.Tools {
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("tool %q: inputSchema is not valid JSON: %v", tool.Name, err)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("tool %q: inputSchema.type = %v, want 'object'", tool.Name, schema["type"])
		}
		props, ok := schema["properties"]
		if !ok {
			t.Errorf("tool %q: inputSchema missing 'properties'", tool.Name)
			continue
		}
		if _, isMap := props.(map[string]any); !isMap {
			t.Errorf("tool %q: inputSchema.properties is not an object", tool.Name)
		}
	}
}

// --- Version negotiation tests ---

func TestVersionNegotiationClientOlder(t *testing.T) {
	srv := testServer()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
	)
	data, _ := json.Marshal(responses[0].Result)
	var result InitializeResult
	json.Unmarshal(data, &result)
	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("got %q, want 2024-11-05", result.ProtocolVersion)
	}
}

func TestVersionNegotiationClientNewer(t *testing.T) {
	srv := testServer()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"test"}}}`,
	)
	data, _ := json.Marshal(responses[0].Result)
	var result InitializeResult
	json.Unmarshal(data, &result)
	if result.ProtocolVersion != "2025-03-26" {
		t.Errorf("got %q, want 2025-03-26", result.ProtocolVersion)
	}
}

func TestVersionNegotiationClientMatch(t *testing.T) {
	srv := testServer()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","clientInfo":{"name":"test"}}}`,
	)
	data, _ := json.Marshal(responses[0].Result)
	var result InitializeResult
	json.Unmarshal(data, &result)
	if result.ProtocolVersion != "2025-03-26" {
		t.Errorf("got %q, want 2025-03-26", result.ProtocolVersion)
	}
}

func TestVersionNegotiationBadVersion(t *testing.T) {
	srv := testServer()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"garbage","clientInfo":{"name":"test"}}}`,
	)
	data, _ := json.Marshal(responses[0].Result)
	var result InitializeResult
	json.Unmarshal(data, &result)
	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("got %q, want 2024-11-05", result.ProtocolVersion)
	}
}

// --- listChanged capability tests ---

func TestCapabilitiesListChangedField(t *testing.T) {
	srv := testServer()
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
	)
	raw, _ := json.Marshal(responses[0].Result)
	rawStr := string(raw)
	if !strings.Contains(rawStr, `"listChanged":false`) {
		t.Errorf("expected listChanged:false in capabilities, got: %s", rawStr)
	}
}

// --- Instructions tests ---

func TestInitializeHasInstructions(t *testing.T) {
	srv := testServer()
	srv.SetInstructions("Use bootstrap at start.")
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
	)
	data, _ := json.Marshal(responses[0].Result)
	var result InitializeResult
	json.Unmarshal(data, &result)
	if result.Instructions != "Use bootstrap at start." {
		t.Errorf("instructions = %q, want 'Use bootstrap at start.'", result.Instructions)
	}
}

func TestInitializeNoInstructions(t *testing.T) {
	srv := testServer()
	// No SetInstructions call.
	responses := sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test"}}}`,
	)
	raw, _ := json.Marshal(responses[0].Result)
	rawStr := string(raw)
	if strings.Contains(rawStr, `"instructions"`) {
		t.Errorf("instructions should be omitted when empty, got: %s", rawStr)
	}
}

// --- Debug logging tests ---

func TestDebugLogsRequests(t *testing.T) {
	var logBuf strings.Builder
	logger := log.New(&logBuf, "", 0)
	srv := NewServer(ServerInfo{Name: "test", Version: "0.1.0"}, logger)
	srv.RegisterTool(Tool{
		Definition: ToolDef{
			Name:        "echo",
			Description: "echoes",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Handler: func(params json.RawMessage) (string, error) { return "ok", nil },
	})
	srv.SetDebug(true)

	sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	)

	if !strings.Contains(logBuf.String(), "[MCP] <-") {
		t.Errorf("expected [MCP] <- in debug logs, got: %s", logBuf.String())
	}
}

func TestDebugLogsResponses(t *testing.T) {
	var logBuf strings.Builder
	logger := log.New(&logBuf, "", 0)
	srv := NewServer(ServerInfo{Name: "test", Version: "0.1.0"}, logger)
	srv.RegisterTool(Tool{
		Definition: ToolDef{
			Name:        "echo",
			Description: "echoes",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Handler: func(params json.RawMessage) (string, error) { return "ok", nil },
	})
	srv.SetDebug(true)

	sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	)

	if !strings.Contains(logBuf.String(), "[MCP] ->") {
		t.Errorf("expected [MCP] -> in debug logs, got: %s", logBuf.String())
	}
}

func TestDebugOffNoExtraLogs(t *testing.T) {
	var logBuf strings.Builder
	logger := log.New(&logBuf, "", 0)
	srv := NewServer(ServerInfo{Name: "test", Version: "0.1.0"}, logger)
	srv.RegisterTool(Tool{
		Definition: ToolDef{
			Name:        "echo",
			Description: "echoes",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
		Handler: func(params json.RawMessage) (string, error) { return "ok", nil },
	})
	// debug is off by default

	sendAndReceive(t, srv,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
	)

	if strings.Contains(logBuf.String(), "[MCP]") {
		t.Errorf("expected no [MCP] prefixes with debug off, got: %s", logBuf.String())
	}
}
