// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/mdutil"
	"github.com/suykerbuyk/vibe-vault/internal/wraprender"
	"github.com/suykerbuyk/vibe-vault/templates"
)

// RenderResumeStateBlocks re-renders the three marker-bounded
// state-derived sub-regions of resume.md (active-tasks, current-state,
// project-history-tail) from filesystem ground truth and atomic-writes
// the result. Returns the rendered file content (so callers can
// fingerprint it) plus any error.
//
// This is the shared entry point used by:
//   - the legacy ApplyBundle Step 9 (transitional; retires in Phase 4)
//   - the D4b auto-heal hooks in vv_append_iteration and vv_update_resume
//
// Both paths must produce byte-identical output for the regression test
// in resume_state_blocks_test.go.
func RenderResumeStateBlocks(cfg config.Config, project string) (string, error) {
	resumeContent, absPath, err := readResume(cfg, project)
	if err != nil {
		return "", err
	}

	tasks, err := collectActiveTasks(cfg, project)
	if err != nil {
		return "", fmt.Errorf("collect active tasks: %w", err)
	}

	state, err := computeCurrentState(cfg, project)
	if err != nil {
		return "", fmt.Errorf("compute current state: %w", err)
	}

	rows, err := collectHistoryRows(cfg, project, 10)
	if err != nil {
		return "", fmt.Errorf("collect history rows: %w", err)
	}

	blocks := map[string]string{
		wraprender.RegionActiveTasks:        wraprender.RenderActiveTasks(tasks),
		wraprender.RegionCurrentState:       wraprender.RenderCurrentState(state),
		wraprender.RegionProjectHistoryTail: wraprender.RenderProjectHistoryTail(rows, 10),
	}

	updated, err := wraprender.ApplyMarkerBlocks(resumeContent, blocks)
	if err != nil {
		return "", fmt.Errorf("apply marker blocks: %w", err)
	}

	if err := mdutil.AtomicWriteFile(absPath, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("write resume: %w", err)
	}
	return updated, nil
}

// autoHealResumeStateBlocks is the D4b auto-heal hook called by
// vv_append_iteration and vv_update_resume after their primary write.
// It is best-effort: a missing resume.md does NOT block the primary
// tool's success (callers running on pre-resume.md projects must not
// see spurious errors).
//
// Legacy resume.md fixtures may also be missing the H2 anchors the
// renderer's self-insertion path needs (e.g., "## Current State" or
// "## Project History (recent)"). In that case wraprender returns
// ErrMissingSection — we treat that as "not Direction-C-ready, skip"
// rather than failing the primary write. Other errors propagate.
func autoHealResumeStateBlocks(cfg config.Config, project string) error {
	if _, _, err := readResume(cfg, project); err != nil {
		// resume.md missing or unreadable → no markers to heal.
		// Fall through silently rather than fail the primary call.
		if strings.Contains(err.Error(), "resume.md not found") {
			return nil
		}
		// Other read errors (path traversal, permissions) we surface.
		return err
	}
	if _, err := RenderResumeStateBlocks(cfg, project); err != nil {
		// Pre-Direction-C resume.md fixtures lacking the required H2
		// anchors are tolerated: the auto-heal silently no-ops so the
		// primary write succeeds. The renderer's self-insertion path
		// is the right machinery once the operator adds the anchors.
		if errors.Is(err, wraprender.ErrMissingSection) {
			return nil
		}
		// Genuine errors (write failures, malformed markers, etc.)
		// propagate so operators learn about drift early.
		return fmt.Errorf("auto-heal resume state blocks: %w", err)
	}
	return nil
}

// collectActiveTasks walks Projects/<p>/agentctx/tasks/*.md (excluding
// done/ and cancelled/ subdirs) and returns wraprender-shaped front-
// matter records. Sort order is delegated to the renderer.
func collectActiveTasks(cfg config.Config, project string) ([]wraprender.TaskFrontMatter, error) {
	tasksDir := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "tasks")
	if _, err := vaultPrefixCheck(tasksDir, cfg.VaultPath); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []wraprender.TaskFrontMatter
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		title, status, priority := parseTaskHeader(filepath.Join(tasksDir, e.Name()))
		out = append(out, wraprender.TaskFrontMatter{
			Slug:     slug,
			Title:    title,
			Status:   status,
			Priority: priority,
		})
	}
	return out, nil
}

// computeCurrentState gathers the headline counts the renderer needs.
// Iteration count comes from a heading scan of iterations.md; MCP-tool
// count from a throw-away Server populated via RegisterAllTools;
// embedded template count from templates.AgentctxFS().
//
// Test-count tracking is intentionally absent from the rendered marker
// block (Phase 2 review, Option C). Operator-authored prose adjacent
// to the marker captures any test-count narrative.
func computeCurrentState(cfg config.Config, project string) (wraprender.CurrentState, error) {
	state := wraprender.CurrentState{}

	iterPath := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "iterations.md")
	if _, err := vaultPrefixCheck(iterPath, cfg.VaultPath); err == nil {
		data, readErr := os.ReadFile(iterPath)
		if readErr == nil {
			state.Iterations = len(scanIterationNumbers(string(data)))
		} else if !os.IsNotExist(readErr) {
			return state, fmt.Errorf("read iterations.md: %w", readErr)
		}
	}

	tools, prompts := countMCPTools(cfg)
	state.MCPTools = tools
	_ = prompts // current renderer hard-codes "+ 1 prompt"

	templateCount, err := countAgentctxTemplates()
	if err != nil {
		return state, fmt.Errorf("count templates: %w", err)
	}
	state.Templates = templateCount

	return state, nil
}

// countMCPTools returns the count of registered tools (and prompts) on
// a throw-away Server. The Server is wired with the canonical
// RegisterAllTools list so the rendered count tracks production reality.
func countMCPTools(cfg config.Config) (tools, prompts int) {
	srv := NewServer(ServerInfo{Name: "vibe-vault", Version: ""}, log.New(io.Discard, "", 0))
	RegisterAllTools(srv, cfg)
	tools = len(srv.ToolNames())
	prompts = len(srv.prompts)
	return tools, prompts
}

// countAgentctxTemplates walks the embedded `templates/agentctx/` FS
// and returns the count of files (excluding directories). Mirrors the
// `Embedded templates: N` headline in the live vibe-vault resume.md.
func countAgentctxTemplates() (int, error) {
	count := 0
	err := fs.WalkDir(templates.AgentctxFS(), ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	return count, err
}

// collectHistoryRows scans Projects/<p>/agentctx/iterations.md and
// returns the last `n` `### Iteration N — Title (YYYY-MM-DD)` headings
// as HistoryRow records. Returns the rows in iteration-ascending
// order; the renderer handles the tail-window slice.
func collectHistoryRows(cfg config.Config, project string, n int) ([]wraprender.HistoryRow, error) {
	iterPath := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "iterations.md")
	if _, err := vaultPrefixCheck(iterPath, cfg.VaultPath); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(iterPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	parsed := parseIterations(string(data))
	rows := make([]wraprender.HistoryRow, 0, len(parsed))
	for _, it := range parsed {
		rows = append(rows, wraprender.HistoryRow{
			Iteration: it.Number,
			Date:      it.Date,
			Summary:   summarizeIterationNarrative(it.Narrative),
		})
	}
	// Document-order is iteration-ascending in vibe-vault, but
	// defensively sort by iteration number so out-of-order edits do
	// not break the tail.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Iteration < rows[j].Iteration })

	if n > 0 && len(rows) > n {
		rows = rows[len(rows)-n:]
	}
	return rows, nil
}

// summarizeIterationNarrative extracts a one-line summary from an
// iteration body. Picks the first non-blank paragraph, joins its
// lines on " ", truncates to ~120 characters at a word boundary, and
// replaces stray pipe characters so the summary is safe to drop into
// a GFM table cell (the renderer also escapes pipes; this is defense
// in depth).
func summarizeIterationNarrative(narr string) string {
	const maxLen = 120
	for _, para := range strings.Split(narr, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		// Join wrapped lines with single spaces.
		lines := strings.Split(para, "\n")
		flat := strings.Join(lines, " ")
		flat = strings.TrimSpace(flat)
		if flat == "" {
			continue
		}
		if len(flat) <= maxLen {
			return flat
		}
		// Truncate at last word boundary before maxLen.
		cut := flat[:maxLen]
		if i := strings.LastIndex(cut, " "); i > 0 {
			cut = cut[:i]
		}
		return strings.TrimRight(cut, " ,;:") + "…"
	}
	return ""
}
