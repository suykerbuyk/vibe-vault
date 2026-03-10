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

// Server is a JSON-RPC 2.0 stdio MCP server.
type Server struct {
	tools  map[string]Tool
	info   ServerInfo
	logger *log.Logger
}

// NewServer creates a new MCP server.
func NewServer(info ServerInfo, logger *log.Logger) *Server {
	return &Server{
		tools:  make(map[string]Tool),
		info:   info,
		logger: logger,
	}
}

// RegisterTool adds a tool to the server.
func (s *Server) RegisterTool(t Tool) {
	s.tools[t.Definition.Name] = t
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
	default:
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeMethodNotFound, Message: fmt.Sprintf("unknown method: %s", req.Method)},
		}
	}
}

func (s *Server) handleInitialize(req Request) Response {
	result := InitializeResult{
		ProtocolVersion: "2024-11-05",
		ServerInfo:      s.info,
		Capabilities:    Capabilities{Tools: &ToolsCap{}},
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
