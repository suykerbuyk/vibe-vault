// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import "encoding/json"

// JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // string or number; nil for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// --- MCP handshake types ---

// InitializeParams is the client's initialize request params.
type InitializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	ClientInfo      ClientInfo `json:"clientInfo"`
}

// ClientInfo identifies the MCP client.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// InitializeResult is the server's initialize response.
type InitializeResult struct {
	ProtocolVersion string       `json:"protocolVersion"`
	ServerInfo      ServerInfo   `json:"serverInfo"`
	Capabilities    Capabilities `json:"capabilities"`
}

// ServerInfo identifies the MCP server.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// Capabilities declares what the server supports.
type Capabilities struct {
	Tools   *ToolsCap   `json:"tools,omitempty"`
	Prompts *PromptsCap `json:"prompts,omitempty"`
}

// ToolsCap declares tool support.
type ToolsCap struct{}

// --- Tool listing ---

// ToolsListResult is the response to tools/list.
type ToolsListResult struct {
	Tools []ToolDef `json:"tools"`
}

// ToolDef describes a tool.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// --- Tool invocation ---

// ToolsCallParams is the request params for tools/call.
type ToolsCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ToolsCallResult is the response to tools/call.
type ToolsCallResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a single content item in a tool result.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// --- Prompt listing ---

// PromptsCap declares prompt support.
type PromptsCap struct{}

// PromptsListResult is the response to prompts/list.
type PromptsListResult struct {
	Prompts []PromptDef `json:"prompts"`
}

// PromptDef describes a prompt.
type PromptDef struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Arguments   []PromptArg `json:"arguments,omitempty"`
}

// PromptArg describes an argument to a prompt.
type PromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// --- Prompt retrieval ---

// PromptsGetParams is the request params for prompts/get.
type PromptsGetParams struct {
	Name      string            `json:"name"`
	Arguments map[string]string `json:"arguments,omitempty"`
}

// PromptsGetResult is the response to prompts/get.
type PromptsGetResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// PromptMessage is a single message in a prompt response.
type PromptMessage struct {
	Role    string       `json:"role"`
	Content ContentBlock `json:"content"`
}
