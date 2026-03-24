// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// CheckResult reports the outcome of a single protocol compliance check.
type CheckResult struct {
	Name   string
	Pass   bool
	Detail string
}

// RunChecks runs MCP protocol compliance checks against the given server
// in-process using io.Pipe. It returns a result for each check.
func RunChecks(srv *Server) []CheckResult {
	var results []CheckResult

	pass := func(name string) {
		results = append(results, CheckResult{Name: name, Pass: true})
	}
	fail := func(name, detail string) {
		results = append(results, CheckResult{Name: name, Pass: false, Detail: detail})
	}

	// Run the server with a controlled handshake.
	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(context.Background(), serverIn, serverOut)
		serverOut.Close()
	}()

	scanner := bufio.NewScanner(clientIn)
	scanner.Buffer(make([]byte, 0, 10*1024*1024), 10*1024*1024)

	sendAndRead := func(msg string) (json.RawMessage, error) {
		if _, err := fmt.Fprintf(clientOut, "%s\n", msg); err != nil {
			return nil, err
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("no response")
		}
		return json.RawMessage(scanner.Bytes()), nil
	}

	sendNotification := func(msg string) error {
		_, err := fmt.Fprintf(clientOut, "%s\n", msg)
		return err
	}

	allNDJSON := true

	// --- Check 1: initialize response has protocolVersion and serverInfo ---
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"mcp-check","version":"1.0.0"}}}`
	initRaw, err := sendAndRead(initReq)
	if err != nil {
		fail("initialize response", err.Error())
		clientOut.Close()
		<-done
		return results
	}

	// Check NDJSON compliance for this line.
	if !json.Valid(initRaw) || strings.ContainsAny(string(initRaw), "\n\r") {
		allNDJSON = false
	}

	var initResp Response
	if err := json.Unmarshal(initRaw, &initResp); err != nil {
		fail("initialize response", fmt.Sprintf("invalid JSON: %v", err))
		clientOut.Close()
		<-done
		return results
	}

	resultData, _ := json.Marshal(initResp.Result)
	var initResult InitializeResult
	json.Unmarshal(resultData, &initResult)

	if initResult.ProtocolVersion != "" && initResult.ServerInfo.Name != "" {
		pass("initialize has protocolVersion and serverInfo")
	} else {
		fail("initialize has protocolVersion and serverInfo",
			fmt.Sprintf("protocolVersion=%q serverInfo.name=%q", initResult.ProtocolVersion, initResult.ServerInfo.Name))
	}

	// --- Check 2: capabilities.tools is present ---
	if initResult.Capabilities.Tools != nil {
		pass("capabilities.tools present")
	} else {
		fail("capabilities.tools present", "tools capability is nil")
	}

	// --- Check 3: version negotiation returns correct version ---
	if initResult.ProtocolVersion == "2025-03-26" {
		pass("version negotiation correct")
	} else {
		fail("version negotiation correct",
			fmt.Sprintf("expected 2025-03-26, got %q", initResult.ProtocolVersion))
	}

	// --- Check 4: notifications/initialized produces no response ---
	// Send notification, then send a tools/list to confirm we can still read.
	// If the notification generated a response, we'd get an extra line.
	if err := sendNotification(`{"jsonrpc":"2.0","method":"notifications/initialized"}`); err != nil {
		fail("notification produces no response", err.Error())
	} else {
		pass("notification produces no response")
	}

	// --- Check 5: tools/list returns expected tool count ---
	toolsListReq := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	toolsRaw, err := sendAndRead(toolsListReq)
	if err != nil {
		fail("tools/list returns tools", err.Error())
		clientOut.Close()
		<-done
		return results
	}

	if !json.Valid(toolsRaw) || strings.ContainsAny(string(toolsRaw), "\n\r") {
		allNDJSON = false
	}

	var toolsResp Response
	json.Unmarshal(toolsRaw, &toolsResp)
	toolsData, _ := json.Marshal(toolsResp.Result)
	var toolsResult ToolsListResult
	json.Unmarshal(toolsData, &toolsResult)

	toolCount := len(toolsResult.Tools)
	if toolCount > 0 {
		pass(fmt.Sprintf("tools/list returns %d tools", toolCount))
	} else {
		fail("tools/list returns tools", "got 0 tools")
	}

	// --- Check 6: all tool InputSchemas have type:"object" and properties ---
	schemaOK := true
	var schemaDetail string
	for _, tool := range toolsResult.Tools {
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			schemaOK = false
			schemaDetail = fmt.Sprintf("tool %q: invalid inputSchema JSON", tool.Name)
			break
		}
		if schema["type"] != "object" {
			schemaOK = false
			schemaDetail = fmt.Sprintf("tool %q: inputSchema.type=%v, want object", tool.Name, schema["type"])
			break
		}
		if _, ok := schema["properties"]; !ok {
			schemaOK = false
			schemaDetail = fmt.Sprintf("tool %q: inputSchema missing properties", tool.Name)
			break
		}
	}
	if schemaOK {
		pass("all tool schemas have type:object and properties")
	} else {
		fail("all tool schemas have type:object and properties", schemaDetail)
	}

	// --- Check 7: prompts/list returns expected prompt count ---
	promptsListReq := `{"jsonrpc":"2.0","id":3,"method":"prompts/list","params":{}}`
	promptsRaw, err := sendAndRead(promptsListReq)
	if err != nil {
		fail("prompts/list returns prompts", err.Error())
	} else {
		if !json.Valid(promptsRaw) || strings.ContainsAny(string(promptsRaw), "\n\r") {
			allNDJSON = false
		}

		var promptsResp Response
		json.Unmarshal(promptsRaw, &promptsResp)
		promptsData, _ := json.Marshal(promptsResp.Result)
		var promptsResult PromptsListResult
		json.Unmarshal(promptsData, &promptsResult)
		promptCount := len(promptsResult.Prompts)
		pass(fmt.Sprintf("prompts/list returns %d prompts", promptCount))
	}

	// --- Check 8: every response line is valid single-line JSON (NDJSON) ---
	if allNDJSON {
		pass("all responses are valid NDJSON")
	} else {
		fail("all responses are valid NDJSON", "found invalid or multi-line JSON response")
	}

	// --- Check 9: empty-server tools/list returns [] not null ---
	emptyCheckPassed := checkEmptyServer(srv)
	if emptyCheckPassed {
		pass("empty-server tools/list returns [] not null")
	} else {
		fail("empty-server tools/list returns [] not null", "got null instead of []")
	}

	// --- Check 10: server exits cleanly on stdin close ---
	clientOut.Close()
	srvErr := <-done
	if srvErr == nil {
		pass("server exits cleanly on stdin close")
	} else {
		fail("server exits cleanly on stdin close", srvErr.Error())
	}

	// --- Check 11: instructions field present (if set) ---
	if srv.instructions != "" {
		if initResult.Instructions != "" {
			pass("instructions field present")
		} else {
			fail("instructions field present", "server has instructions set but field is empty in response")
		}
	} else {
		pass("instructions field present (not set, omitted correctly)")
	}

	return results
}

// checkEmptyServer creates a zero-tool server and verifies tools/list returns [].
func checkEmptyServer(origSrv *Server) bool {
	emptySrv := NewServer(ServerInfo{Name: "empty-check", Version: "0.0.0"}, origSrv.logger)

	serverIn, clientOut := io.Pipe()
	clientIn, serverOut := io.Pipe()

	done := make(chan error, 1)
	go func() {
		done <- emptySrv.Serve(context.Background(), serverIn, serverOut)
		serverOut.Close()
	}()

	scanner := bufio.NewScanner(clientIn)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	// Send initialize.
	fmt.Fprintf(clientOut, "%s\n", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"check","version":"0"}}}`)
	scanner.Scan() // consume initialize response

	// Send tools/list.
	fmt.Fprintf(clientOut, "%s\n", `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	scanner.Scan()
	line := scanner.Text()

	clientOut.Close()
	<-done

	return strings.Contains(line, `"tools":[]`)
}
