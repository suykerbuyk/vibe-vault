// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sort"
)

// Tool pairs a definition with its handler.
type Tool struct {
	Definition ToolDef
	Handler    func(params json.RawMessage) (string, error)
}

// Prompt pairs a definition with its handler.
type Prompt struct {
	Definition PromptDef
	Handler    func(args map[string]string) (PromptsGetResult, error)
}

// Server is a JSON-RPC 2.0 stdio MCP server.
type Server struct {
	tools   map[string]Tool
	prompts map[string]Prompt
	info    ServerInfo
	logger  *log.Logger
}

// NewServer creates a new MCP server.
func NewServer(info ServerInfo, logger *log.Logger) *Server {
	return &Server{
		tools:   make(map[string]Tool),
		prompts: make(map[string]Prompt),
		info:    info,
		logger:  logger,
	}
}

// RegisterTool adds a tool to the server.
func (s *Server) RegisterTool(t Tool) {
	s.tools[t.Definition.Name] = t
}

// RegisterPrompt adds a prompt to the server.
func (s *Server) RegisterPrompt(p Prompt) {
	s.prompts[p.Definition.Name] = p
}

// Serve reads JSON-RPC requests from in and writes responses to out.
// It returns on EOF or context cancellation.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024) // 1MB max line

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			resp := Response{
				JSONRPC: "2.0",
				ID:      nil,
				Error:   &RPCError{Code: CodeParseError, Message: "parse error"},
			}
			s.writeResponse(out, resp)
			continue
		}

		resp := s.dispatch(req)

		// Notifications (nil ID) get no response.
		if req.ID == nil {
			continue
		}

		s.writeResponse(out, resp)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return nil
}

func (s *Server) dispatch(req Request) Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		return Response{} // no-op, no response sent
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	case "prompts/list":
		return s.handlePromptsList(req)
	case "prompts/get":
		return s.handlePromptsGet(req)
	default:
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeMethodNotFound, Message: fmt.Sprintf("unknown method: %s", req.Method)},
		}
	}
}

func (s *Server) handleInitialize(req Request) Response {
	caps := Capabilities{Tools: &ToolsCap{}}
	if len(s.prompts) > 0 {
		caps.Prompts = &PromptsCap{}
	}
	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		ServerInfo:      s.info,
		Capabilities:    caps,
	}
	return Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) handleToolsList(req Request) Response {
	var defs []ToolDef
	for _, t := range s.tools {
		defs = append(defs, t.Definition)
	}
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})
	return Response{JSONRPC: "2.0", ID: req.ID, Result: ToolsListResult{Tools: defs}}
}

func (s *Server) handleToolsCall(req Request) Response {
	var params ToolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidParams, Message: "invalid params"},
		}
	}

	tool, ok := s.tools[params.Name]
	if !ok {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: ToolsCallResult{
				Content: []ContentBlock{{Type: "text", Text: fmt.Sprintf("unknown tool: %s", params.Name)}},
				IsError: true,
			},
		}
	}

	s.logger.Printf("tools/call: %s", params.Name)

	text, err := tool.Handler(params.Arguments)
	if err != nil {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: ToolsCallResult{
				Content: []ContentBlock{{Type: "text", Text: err.Error()}},
				IsError: true,
			},
		}
	}

	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  ToolsCallResult{Content: []ContentBlock{{Type: "text", Text: text}}},
	}
}

func (s *Server) handlePromptsList(req Request) Response {
	var defs []PromptDef
	for _, p := range s.prompts {
		defs = append(defs, p.Definition)
	}
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})
	return Response{JSONRPC: "2.0", ID: req.ID, Result: PromptsListResult{Prompts: defs}}
}

func (s *Server) handlePromptsGet(req Request) Response {
	var params PromptsGetParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidParams, Message: "invalid params"},
		}
	}

	prompt, ok := s.prompts[params.Name]
	if !ok {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidParams, Message: fmt.Sprintf("unknown prompt: %s", params.Name)},
		}
	}

	s.logger.Printf("prompts/get: %s", params.Name)

	result, err := prompt.Handler(params.Arguments)
	if err != nil {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInternalError, Message: err.Error()},
		}
	}

	return Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) writeResponse(out io.Writer, resp Response) {
	data, err := json.Marshal(resp)
	if err != nil {
		s.logger.Printf("marshal response: %v", err)
		return
	}
	data = append(data, '\n')
	if _, err := out.Write(data); err != nil {
		s.logger.Printf("write response: %v", err)
	}
}
