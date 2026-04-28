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
	"regexp"
	"sort"

	"github.com/suykerbuyk/vibe-vault/internal/config"
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
	tools        map[string]Tool
	prompts      map[string]Prompt
	info         ServerInfo
	logger       *log.Logger
	instructions string
	debug        bool
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

// RegisterAllTools registers every production tool + prompt on srv. This is
// the single source of truth for the MCP-tool inventory: cmd/vv/main.go calls
// it from `vv mcp` server startup, and `applyResumeStateBlocks` (Step 9 of
// `ApplyBundle`) calls it on a throw-away Server to count tools for the
// `current-state` marker block via `srv.ToolNames()`.
//
// Keep this list aligned with cmd/vv/main.go's surfaced Tool factories.
// Adding or removing a tool here flows through to the rendered MCP count
// in resume.md on the next wrap.
func RegisterAllTools(srv *Server, cfg config.Config) {
	srv.RegisterTool(NewGetProjectContextTool(cfg))
	srv.RegisterTool(NewListProjectsTool(cfg))
	srv.RegisterTool(NewSearchSessionsTool(cfg))
	srv.RegisterTool(NewGetKnowledgeTool(cfg))
	srv.RegisterTool(NewGetSessionDetailTool(cfg))
	srv.RegisterTool(NewGetFrictionTrendsTool(cfg))
	srv.RegisterTool(NewGetEffectivenessTool(cfg))
	srv.RegisterTool(NewCaptureSessionTool(cfg))
	srv.RegisterTool(NewGetWorkflowTool(cfg))
	srv.RegisterTool(NewGetResumeTool(cfg))
	srv.RegisterTool(NewListTasksTool(cfg))
	srv.RegisterTool(NewGetTaskTool(cfg))
	srv.RegisterTool(NewUpdateResumeTool(cfg))
	srv.RegisterTool(NewAppendIterationTool(cfg))
	srv.RegisterTool(NewManageTaskTool(cfg))
	srv.RegisterTool(NewRefreshIndexTool(cfg))
	srv.RegisterTool(NewBootstrapContextTool(cfg))
	srv.RegisterTool(NewListLearningsTool(cfg))
	srv.RegisterTool(NewGetLearningTool(cfg))
	srv.RegisterTool(NewGetIterationsTool(cfg))
	srv.RegisterTool(NewGetProjectRootTool(cfg))
	srv.RegisterTool(NewSetCommitMsgTool(cfg))
	srv.RegisterTool(NewThreadInsertTool(cfg))
	srv.RegisterTool(NewThreadReplaceTool(cfg))
	srv.RegisterTool(NewThreadRemoveTool(cfg))
	srv.RegisterTool(NewCarriedAddTool(cfg))
	srv.RegisterTool(NewCarriedRemoveTool(cfg))
	srv.RegisterTool(NewCarriedPromoteToTaskTool(cfg))
	srv.RegisterTool(NewRenderCommitMsgTool(cfg))
	srv.RegisterTool(NewPrepareWrapSkeletonTool())
	srv.RegisterTool(NewSynthesizeWrapTool(cfg))
	srv.RegisterTool(NewApplyWrapBundleByHandleTool(cfg))
	srv.RegisterTool(NewWrapQualityCheckTool(cfg))
	srv.RegisterTool(NewWrapDispatchTool(cfg))
	srv.RegisterTool(NewVaultReadTool(cfg))
	srv.RegisterTool(NewVaultListTool(cfg))
	srv.RegisterTool(NewVaultExistsTool(cfg))
	srv.RegisterTool(NewVaultSha256Tool(cfg))
	srv.RegisterTool(NewVaultWriteTool(cfg))
	srv.RegisterTool(NewVaultEditTool(cfg))
	srv.RegisterTool(NewVaultDeleteTool(cfg))
	srv.RegisterTool(NewVaultMoveTool(cfg))
	srv.RegisterTool(NewGetAgentDefinitionTool())
	srv.RegisterPrompt(NewSessionGuidelinesPrompt())
}

// ToolNames returns the registered tool names in stable alphabetical order.
// Useful for inventory/diagnostic output (e.g. `vv mcp check --tools`) without
// going through the JSON-RPC protocol.
func (s *Server) ToolNames() []string {
	names := make([]string, 0, len(s.tools))
	for name := range s.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// RegisterPrompt adds a prompt to the server.
func (s *Server) RegisterPrompt(p Prompt) {
	s.prompts[p.Definition.Name] = p
}

// SetInstructions sets the instructions string returned in the initialize response.
func (s *Server) SetInstructions(text string) {
	s.instructions = text
}

// SetDebug enables verbose protocol logging of all JSON-RPC messages.
func (s *Server) SetDebug(on bool) {
	s.debug = on
}

// Serve reads JSON-RPC requests from in and writes responses to out.
// It returns on EOF or context cancellation.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 10*1024*1024), 10*1024*1024) // 10MB max line

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

		if s.debug {
			s.logger.Printf("[MCP] <- %s", line)
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
			if s.debug {
				s.logger.Printf("[MCP] (notification %s, no response)", req.Method)
			}
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

// versionDateRe matches YYYY-MM-DD format.
var versionDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// negotiateVersion picks min(client, MaxProtocolVersion) with MinProtocolVersion as floor.
func negotiateVersion(clientVersion string) string {
	if !versionDateRe.MatchString(clientVersion) {
		return MinProtocolVersion
	}
	// Date-string versions sort lexicographically.
	v := clientVersion
	if v > MaxProtocolVersion {
		v = MaxProtocolVersion
	}
	if v < MinProtocolVersion {
		v = MinProtocolVersion
	}
	return v
}

func (s *Server) handleInitialize(req Request) Response {
	negotiated := MinProtocolVersion

	// Log client info for diagnostics and negotiate version.
	if req.Params != nil {
		var initParams InitializeParams
		if err := json.Unmarshal(req.Params, &initParams); err == nil {
			s.logger.Printf("initialize: client=%s version=%s protocolVersion=%s",
				initParams.ClientInfo.Name, initParams.ClientInfo.Version, initParams.ProtocolVersion)
			negotiated = negotiateVersion(initParams.ProtocolVersion)
		}
	}

	caps := Capabilities{Tools: &ToolsCap{}}
	if len(s.prompts) > 0 {
		caps.Prompts = &PromptsCap{}
	}
	result := InitializeResult{
		ProtocolVersion: negotiated,
		ServerInfo:      s.info,
		Capabilities:    caps,
		Instructions:    s.instructions,
	}
	return Response{JSONRPC: "2.0", ID: req.ID, Result: result}
}

func (s *Server) handleToolsList(req Request) Response {
	defs := make([]ToolDef, 0, len(s.tools))
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
	defs := make([]PromptDef, 0, len(s.prompts))
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
	if s.debug {
		s.logger.Printf("[MCP] -> %s", data)
	}
	data = append(data, '\n')
	if _, err := out.Write(data); err != nil {
		s.logger.Printf("write response: %v", err)
	}
}
