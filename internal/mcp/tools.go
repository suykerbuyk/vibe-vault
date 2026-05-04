// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/effectiveness"
	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/inject"
	"github.com/suykerbuyk/vibe-vault/internal/narrative"
	"github.com/suykerbuyk/vibe-vault/internal/session"
	"github.com/suykerbuyk/vibe-vault/internal/sessionclaim"
	"github.com/suykerbuyk/vibe-vault/internal/staging"
	"github.com/suykerbuyk/vibe-vault/internal/transcript"
	"github.com/suykerbuyk/vibe-vault/internal/trends"
)

// sessionclaimAcquireOrRefresh is a test seam for the
// vv_capture_session handler's sessionclaim integration. Tests can
// override to inject (nil, err) and exercise the legacy fallback path
// (H6 contract). Default forwards to sessionclaim.AcquireOrRefresh.
var sessionclaimAcquireOrRefresh = sessionclaim.AcquireOrRefresh

var dateRegexp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// NewGetProjectContextTool creates the get_project_context tool.
func NewGetProjectContextTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_get_project_context",
			Description: "Get condensed project context including recent sessions, open threads, decisions, and friction trends. Use this to understand what has been happening in a project.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, returns context for all projects."
					},
					"sections": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Sections to include: summary, sessions, threads, decisions, friction. Default: all."
					},
					"max_tokens": {
						"type": "integer",
						"description": "Token budget for output. Default: configured default_max_tokens."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project   string   `json:"project"`
				Sections  []string `json:"sections"`
				MaxTokens int      `json:"max_tokens"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			if args.MaxTokens <= 0 {
				args.MaxTokens = cfg.MCP.DefaultMaxTokens
			}

			idx, err := index.Load(cfg.StateDir())
			if err != nil {
				return "", fmt.Errorf("load index: %w", err)
			}

			trendResult := trends.Compute(idx.Entries, args.Project, 4)

			opts := inject.Opts{
				Project:   args.Project,
				Format:    "json",
				Sections:  args.Sections,
				MaxTokens: args.MaxTokens,
			}

			result := inject.Build(idx.Entries, trendResult, opts)
			output, err := inject.Render(result, opts)
			if err != nil {
				return "", fmt.Errorf("render: %w", err)
			}
			return output, nil
		},
	}
}

// NewListProjectsTool creates the list_projects tool.
func NewListProjectsTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_list_projects",
			Description: "List all projects in the vault with session counts and date ranges.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			idx, err := index.Load(cfg.StateDir())
			if err != nil {
				return "", fmt.Errorf("load index: %w", err)
			}

			type projectInfo struct {
				Name              string `json:"name"`
				SessionCount      int    `json:"session_count"`
				FirstSession      string `json:"first_session"`
				LastSession       string `json:"last_session"`
				FrictionDirection string `json:"friction_direction,omitempty"`
			}

			projectMap := make(map[string]*projectInfo)
			for _, e := range idx.Entries {
				pi, ok := projectMap[e.Project]
				if !ok {
					pi = &projectInfo{Name: e.Project}
					projectMap[e.Project] = pi
				}
				pi.SessionCount++
				if pi.FirstSession == "" || e.Date < pi.FirstSession {
					pi.FirstSession = e.Date
				}
				if e.Date > pi.LastSession {
					pi.LastSession = e.Date
				}
			}

			var projects []projectInfo
			for _, pi := range projectMap {
				tr := trends.Compute(idx.Entries, pi.Name, 4)
				for _, m := range tr.Metrics {
					if m.Name == "friction" {
						pi.FrictionDirection = m.Direction
						break
					}
				}
				projects = append(projects, *pi)
			}

			sort.Slice(projects, func(i, j int) bool {
				return strings.ToLower(projects[i].Name) < strings.ToLower(projects[j].Name)
			})

			data, err := json.MarshalIndent(projects, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}

// validateProjectName rejects project names that could escape the vault boundary.
func validateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name is required")
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid project name: %q", name)
	}
	return nil
}

// NewSearchSessionsTool creates the search_sessions tool.
func NewSearchSessionsTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_search_sessions",
			Description: `Search and filter session notes by query, project, files, date range, and friction score. File matching uses filepath.Match which supports * (single segment) but not ** (recursive globs).`,
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"query": {
						"type": "string",
						"description": "Search text (case-insensitive) matched against title, summary, and decisions."
					},
					"project": {
						"type": "string",
						"description": "Filter by project name."
					},
					"files": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Filter by file glob patterns (filepath.Match syntax, single segment * only)."
					},
					"date_from": {
						"type": "string",
						"description": "Start date (YYYY-MM-DD, inclusive)."
					},
					"date_to": {
						"type": "string",
						"description": "End date (YYYY-MM-DD, inclusive)."
					},
					"min_friction": {
						"type": "integer",
						"description": "Minimum friction score."
					},
					"max_results": {
						"type": "integer",
						"description": "Maximum results to return (default 10)."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Query       string   `json:"query"`
				Project     string   `json:"project"`
				Files       []string `json:"files"`
				DateFrom    string   `json:"date_from"`
				DateTo      string   `json:"date_to"`
				MinFriction int      `json:"min_friction"`
				MaxResults  int      `json:"max_results"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.MaxResults <= 0 {
				args.MaxResults = 10
			}

			idx, err := index.Load(cfg.StateDir())
			if err != nil {
				return "", fmt.Errorf("load index: %w", err)
			}

			type sessionResult struct {
				SessionID     string   `json:"session_id"`
				Project       string   `json:"project"`
				Date          string   `json:"date"`
				Title         string   `json:"title"`
				Summary       string   `json:"summary,omitempty"`
				FrictionScore int      `json:"friction_score,omitempty"`
				FilesChanged  int      `json:"files_changed"`
				Decisions     []string `json:"decisions,omitempty"`
			}

			queryLower := strings.ToLower(args.Query)
			var results []sessionResult

			for _, e := range idx.Entries {
				// Project filter
				if args.Project != "" && e.Project != args.Project {
					continue
				}
				// Date range filter
				if args.DateFrom != "" && e.Date < args.DateFrom {
					continue
				}
				if args.DateTo != "" && e.Date > args.DateTo {
					continue
				}
				// Friction filter
				if args.MinFriction > 0 && e.FrictionScore < args.MinFriction {
					continue
				}
				// File pattern filter
				if len(args.Files) > 0 {
					matched := false
					for _, pattern := range args.Files {
						for _, f := range e.FilesChanged {
							if ok, _ := filepath.Match(pattern, f); ok {
								matched = true
								break
							}
						}
						if matched {
							break
						}
					}
					if !matched {
						continue
					}
				}
				// Query filter
				if queryLower != "" {
					found := strings.Contains(strings.ToLower(e.Title), queryLower) ||
						strings.Contains(strings.ToLower(e.Summary), queryLower)
					if !found {
						for _, d := range e.Decisions {
							if strings.Contains(strings.ToLower(d), queryLower) {
								found = true
								break
							}
						}
					}
					if !found {
						continue
					}
				}

				results = append(results, sessionResult{
					SessionID:     e.SessionID,
					Project:       e.Project,
					Date:          e.Date,
					Title:         e.Title,
					Summary:       e.Summary,
					FrictionScore: e.FrictionScore,
					FilesChanged:  len(e.FilesChanged),
					Decisions:     e.Decisions,
				})
			}

			// Sort date descending
			sort.Slice(results, func(i, j int) bool {
				return results[i].Date > results[j].Date
			})

			if len(results) > args.MaxResults {
				results = results[:args.MaxResults]
			}

			data, err := json.MarshalIndent(results, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}

// NewGetKnowledgeTool creates the get_knowledge tool.
func NewGetKnowledgeTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_get_knowledge",
			Description: "Get the knowledge.md file for a project, containing curated project-specific knowledge.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name (required)."
					}
				},
				"required": ["project"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project string `json:"project"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if err := validateProjectName(args.Project); err != nil {
				return "", err
			}

			path := filepath.Join(cfg.VaultPath, "Projects", args.Project, "knowledge.md")
			// Verify path is under vault
			absPath, err := filepath.Abs(path)
			if err != nil {
				return "", fmt.Errorf("resolve path: %w", err)
			}
			absVault, _ := filepath.Abs(cfg.VaultPath)
			if !strings.HasPrefix(absPath, absVault+string(filepath.Separator)) {
				return "", fmt.Errorf("path traversal rejected")
			}

			data, err := os.ReadFile(absPath)
			if err != nil {
				if os.IsNotExist(err) {
					return "", fmt.Errorf("knowledge.md not found for project %q", args.Project)
				}
				return "", fmt.Errorf("read knowledge: %w", err)
			}
			return string(data), nil
		},
	}
}

// NewGetSessionDetailTool creates the get_session_detail tool.
func NewGetSessionDetailTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_get_session_detail",
			Description: "Get the full markdown content of a specific session note.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name (required)."
					},
					"date": {
						"type": "string",
						"description": "Session date in YYYY-MM-DD format (required)."
					},
					"iteration": {
						"type": "integer",
						"description": "Day iteration number (default 1)."
					}
				},
				"required": ["project", "date"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project   string `json:"project"`
				Date      string `json:"date"`
				Iteration int    `json:"iteration"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if err := validateProjectName(args.Project); err != nil {
				return "", err
			}
			if !dateRegexp.MatchString(args.Date) {
				return "", fmt.Errorf("invalid date format: %q (expected YYYY-MM-DD)", args.Date)
			}
			if args.Iteration <= 0 {
				args.Iteration = 1
			}

			// Phase 5 / Mechanism 1: filenames may be legacy counter, plain
			// timestamp, or timestamp-with-suffix. Look up via the in-memory
			// session-index instead of constructing a filename. Multi-host
			// stale-index races may produce more than one entry for the same
			// (project, date, iteration); deterministic tiebreaker is
			// lex-earliest NotePath.
			idx, err := index.Load(cfg.StateDir())
			if err != nil {
				return "", fmt.Errorf("load index: %w", err)
			}

			var matches []index.SessionEntry
			for _, e := range idx.Entries {
				if e.Project == args.Project && e.Date == args.Date && e.Iteration == args.Iteration {
					matches = append(matches, e)
				}
			}
			if len(matches) == 0 {
				return "", fmt.Errorf("session note not found: project=%s date=%s iteration=%d",
					args.Project, args.Date, args.Iteration)
			}
			// Sort lex-ascending by NotePath. With timestamp filenames lex order
			// equals chronological order, so we get the earliest deterministically.
			sort.Slice(matches, func(i, j int) bool {
				return matches[i].NotePath < matches[j].NotePath
			})
			rel := matches[0].NotePath

			// Verify path is under vault.
			absPath, err := filepath.Abs(filepath.Join(cfg.VaultPath, rel))
			if err != nil {
				return "", fmt.Errorf("resolve path: %w", err)
			}
			absVault, _ := filepath.Abs(cfg.VaultPath)
			if !strings.HasPrefix(absPath, absVault+string(filepath.Separator)) {
				return "", fmt.Errorf("path traversal rejected")
			}

			data, err := os.ReadFile(absPath)
			if err != nil {
				if os.IsNotExist(err) {
					return "", fmt.Errorf("session note not found: %s", rel)
				}
				return "", fmt.Errorf("read session: %w", err)
			}
			return string(data), nil
		},
	}
}

// NewGetFrictionTrendsTool creates the get_friction_trends tool.
func NewGetFrictionTrendsTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_get_friction_trends",
			Description: "Get friction and efficiency trend data over time, useful for understanding development velocity patterns.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Filter by project name. If omitted, shows trends across all projects."
					},
					"weeks": {
						"type": "integer",
						"description": "Number of weeks to display (default 8)."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project string `json:"project"`
				Weeks   int    `json:"weeks"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if args.Weeks <= 0 {
				args.Weeks = 8
			}

			idx, err := index.Load(cfg.StateDir())
			if err != nil {
				return "", fmt.Errorf("load index: %w", err)
			}

			result := trends.Compute(idx.Entries, args.Project, args.Weeks)
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}

// NewGetEffectivenessTool creates the get_effectiveness tool.
func NewGetEffectivenessTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_get_effectiveness",
			Description: "Analyze whether context availability correlates with better session outcomes (lower friction, fewer corrections). Requires vv reprocess --backfill-context to have been run first.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Filter by project name. If omitted, analyzes all projects."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project string `json:"project"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			idx, err := index.Load(cfg.StateDir())
			if err != nil {
				return "", fmt.Errorf("load index: %w", err)
			}

			result := effectiveness.Analyze(idx.Entries, args.Project)
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(data) + "\n", nil
		},
	}
}

// NewCaptureSessionTool creates the vv_capture_session tool.
// This enables push-based session capture from any Zed agent via MCP.
func NewCaptureSessionTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_capture_session",
			Description: "Record a session note from the current agent conversation. Call this at the end of a work session to capture what was accomplished.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"summary": {
						"type": "string",
						"description": "What was accomplished in this session (required)."
					},
					"title": {
						"type": "string",
						"description": "Note title. Defaults to first sentence of summary."
					},
					"tag": {
						"type": "string",
						"description": "Activity tag: implementation, debugging, refactor, exploration, review, docs, etc."
					},
					"model": {
						"type": "string",
						"description": "Model that performed the work (e.g. claude-sonnet-4-6)."
					},
					"decisions": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Key decisions made during the session."
					},
					"files_changed": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Files that were created or modified."
					},
					"open_threads": {
						"type": "array",
						"items": {"type": "string"},
						"description": "Unresolved items or follow-up work."
					}
				},
				"required": ["summary"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Summary      string   `json:"summary"`
				Title        string   `json:"title"`
				Tag          string   `json:"tag"`
				Model        string   `json:"model"`
				Decisions    []string `json:"decisions"`
				FilesChanged []string `json:"files_changed"`
				OpenThreads  []string `json:"open_threads"`
			}
			if err := json.Unmarshal(params, &args); err != nil {
				return "", fmt.Errorf("invalid arguments: %w", err)
			}
			if args.Summary == "" {
				return "", fmt.Errorf("summary is required")
			}

			// Derive title from summary if not provided
			title := args.Title
			if title == "" {
				title = firstSentence(args.Summary)
			}

			// Detect context from CWD (Zed sets MCP server CWD to worktree root)
			cwd, err := os.Getwd()
			if err != nil {
				return "", fmt.Errorf("get working directory: %w", err)
			}

			// Detect git branch (1s timeout, same as detect.go)
			branch := detectBranch(cwd)

			// Phase 4 (M9) of session-slot-multihost-disambiguation:
			// integrate sessionclaim. Resolve project root, acquire or
			// refresh the host-local claim, derive the session id and
			// source from it. On failure (H6 contract) fall back to
			// legacy crypto/rand mint with hardcoded "zed" source so
			// today's behavior is preserved on read-only filesystems
			// or transient I/O errors.
			projectRoot := session.DetectProjectRoot(cwd)
			claim, claimErr := sessionclaimAcquireOrRefresh(projectRoot)
			if claimErr != nil {
				log.Printf("warning: sessionclaim.AcquireOrRefresh: %v", claimErr)
			}

			var sessionID string
			if claim != nil {
				sessionID = sessionclaim.EffectiveSessionID(claim)
			}
			if sessionID == "" {
				// Legacy fallback (H6): synthesize a zed-mcp session id.
				randBytes := make([]byte, 16)
				if _, randErr := rand.Read(randBytes); randErr != nil {
					return "", fmt.Errorf("generate session ID: %w", randErr)
				}
				sessionID = "zed-mcp:" + hex.EncodeToString(randBytes)
			}

			// Source flips on claim.harness:
			//   claude-code → "" (no source field, default)
			//   zed-mcp     → "zed"
			//   unknown     → "unknown"
			// On nil claim (sessionclaim failure or read-only), use the
			// pre-Phase-4 hardcoded "zed" so the existing user contract
			// is preserved on the H6 fallback path.
			var source string
			if claim != nil {
				source = sessionclaim.EffectiveSource(claim)
			} else {
				source = "zed"
			}

			// Detect project and domain
			info := session.Detect(cwd, branch, args.Model, sessionID, cfg)

			// Build minimal transcript that passes triviality check
			t := &transcript.Transcript{
				Stats: transcript.Stats{
					UserMessages:      2,
					AssistantMessages: 2,
					StartTime:         time.Now(),
					FilesWritten:      make(map[string]bool),
				},
			}
			for _, f := range args.FilesChanged {
				t.Stats.FilesWritten[f] = true
			}

			// Build narrative from tool input
			narr := &narrative.Narrative{
				Title:       title,
				Summary:     args.Summary,
				Tag:         args.Tag,
				Decisions:   args.Decisions,
				OpenThreads: args.OpenThreads,
			}

			// Phase 2 of vault-two-tier-narrative-vs-sessions-split:
			// route MCP-captured notes (called from /wrap) into the
			// host-local staging dir. Pre-Phase-2, every wrap silently
			// leaked a session note to the shared vault.
			opts := session.CaptureOpts{
				Source:         source,
				ProjectRoot:    projectRoot,
				SkipEnrichment: true,
				CWD:            cwd,
				SessionID:      sessionID,
				StagingRoot:    staging.ResolveRoot(cfg.Staging.Root),
			}

			result, err := session.CaptureFromParsed(t, info, narr, nil, opts, cfg)
			if err != nil {
				return "", fmt.Errorf("capture session: %w", err)
			}

			if result.Skipped {
				return fmt.Sprintf(`{"status": "skipped", "reason": %q}`, result.Reason), nil
			}

			resp := map[string]any{
				"status":    "captured",
				"project":   result.Project,
				"note_path": result.NotePath,
				"iteration": result.Iteration,
			}
			data, _ := json.Marshal(resp)
			return string(data), nil
		},
	}
}

// firstSentence extracts the first sentence from text.
func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "Session"
	}
	// Split on sentence-ending punctuation followed by space or end
	for i, ch := range text {
		if ch == '.' || ch == '!' || ch == '?' {
			if i+1 >= len(text) || text[i+1] == ' ' || text[i+1] == '\n' {
				s := text[:i+1]
				if len(s) > 80 {
					return s[:77] + "..."
				}
				return s
			}
		}
	}
	// No sentence end found — take first line
	if idx := strings.IndexByte(text, '\n'); idx > 0 {
		text = text[:idx]
	}
	if len(text) > 80 {
		return text[:77] + "..."
	}
	return text
}

// detectBranch runs git rev-parse to get the current branch with a 1s timeout.
func detectBranch(cwd string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
