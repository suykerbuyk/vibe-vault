// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Marker pair and sidecar filenames for the v9 data-workflow block.
const (
	dataWorkflowStart      = "<!-- vv:data-workflow:start -->"
	dataWorkflowEnd        = "<!-- vv:data-workflow:end -->"
	dataWorkflowSnippet    = "snippets/resume-data-workflow.md"
	dataWorkflowOptout     = "resume.md.no-data-workflow"
	dataWorkflowBaselineFn = ".datablock.baseline" // appended to resume.md path
	dataWorkflowResume     = "resume.md"
)

// migrate8to9 propagates the snippets/ subdir and injects (or updates) the
// canonical data-workflow block in the project's agentctx/resume.md.
//
// Behaviour (see doc/DESIGN.md #50):
//  1. Propagate snippets/ via propagateSharedSubdir (Tier-2 → Tier-3 copy
//     with baseline/pin handling, same as commands/skills).
//  2. If agentctx/resume.md.no-data-workflow exists, record SKIP-OPTOUT and
//     return — the project has explicitly opted out of block injection.
//  3. Read the Tier-3 snippet (possibly user-customized if .pinned) and
//     inject its marker-delimited body into agentctx/resume.md.
//  4. The injection is governed by a span-only baseline stored at
//     agentctx/resume.md.datablock.baseline, separate from the file-level
//     baseline used by propagateSharedSubdir.
//  5. Idempotent: if the injected span already matches the snippet body,
//     no write and no action are recorded.
func migrate8to9(ctx MigrationContext) ([]FileAction, error) {
	var actions []FileAction

	// 1. Propagate snippets/ subdir (same three-way baseline machinery as
	//    commands/ and skills/). force=true when --force is set; otherwise
	//    respect user-customized files unless the template changed cleanly.
	subActions := propagateSharedSubdir(ctx.VaultPath, ctx.AgentctxPath, "snippets", ctx.DryRun, ctx.Force)
	actions = append(actions, subActions...)

	resumePath := filepath.Join(ctx.AgentctxPath, dataWorkflowResume)
	optoutPath := filepath.Join(ctx.AgentctxPath, dataWorkflowOptout)

	// 2. Honor per-project opt-out marker.
	if _, err := os.Stat(optoutPath); err == nil {
		actions = append(actions, FileAction{
			Path:     dataWorkflowResume,
			Action:   "SKIP-OPTOUT",
			Location: "vault",
		})
		return actions, nil
	}

	// 3. Read Tier-3 snippet. If missing (e.g., project has no agentctx/
	//    layout yet), skip injection silently — propagateSharedSubdir above
	//    already reported whatever was reportable.
	snippetPath := filepath.Join(ctx.AgentctxPath, dataWorkflowSnippet)
	snippetBody, err := readSnippetBody(snippetPath, ctx.Project)
	if err != nil {
		// Not fatal: record and move on.
		actions = append(actions, FileAction{
			Path:     dataWorkflowSnippet,
			Action:   "MISSING",
			Location: "vault",
		})
		return actions, nil
	}

	// 4. Read resume.md (absent is OK — we create it below).
	resumeData, _ := os.ReadFile(resumePath)

	injectActions, err := injectDataWorkflowBlock(ctx, resumePath, resumeData, snippetBody)
	if err != nil {
		return actions, err
	}
	actions = append(actions, injectActions...)

	return actions, nil
}

// readSnippetBody reads the Tier-3 snippet file, applies {{PROJECT}}/{{DATE}}
// variable substitution, and returns the content between the vv:data-workflow
// markers (inclusive). The markers are required — a well-formed snippet always
// carries them. Returns an error if the file is missing, unreadable, or does
// not contain a complete marker pair.
func readSnippetBody(snippetPath, project string) (string, error) {
	raw, err := os.ReadFile(snippetPath)
	if err != nil {
		return "", err
	}
	content := applyVars(string(raw), DefaultVars(project))
	start := strings.Index(content, dataWorkflowStart)
	end := strings.Index(content, dataWorkflowEnd)
	if start < 0 || end < 0 || end < start {
		return "", fmt.Errorf("snippet missing marker pair")
	}
	// Include both markers in the extracted block.
	end += len(dataWorkflowEnd)
	return strings.TrimRight(content[start:end], "\n"), nil
}

// injectDataWorkflowBlock updates agentctx/resume.md to contain the snippet
// block between <!-- vv:data-workflow:start --> / <!-- vv:data-workflow:end -->
// markers, honouring baseline-based conflict detection.
//
// Cases:
//   - Both markers present, current span == snippetBody → idempotent (no-op).
//   - Both markers present, current span != snippetBody AND matches baseline
//     (or no baseline) → UPDATE.
//   - Both markers present, current span != snippetBody AND != baseline →
//     CONFLICT; rewrite only under ctx.Force.
//   - Neither marker present → INSERT after first `## ` heading (or append).
//   - Exactly one marker present → CONFLICT; rewrite under ctx.Force by
//     replacing from the last-seen marker forward.
//
// Writes resume.md.datablock.baseline with the injected snippetBody so that
// later runs can detect user edits to the block itself (span-only baseline,
// separate from the whole-file .baseline used elsewhere).
func injectDataWorkflowBlock(ctx MigrationContext, resumePath string, resumeData []byte, snippetBody string) ([]FileAction, error) {
	baselinePath := resumePath + dataWorkflowBaselineFn
	baselineData, _ := os.ReadFile(baselinePath)
	baselineStr := strings.TrimRight(string(baselineData), "\n")

	startIdx := bytes.Index(resumeData, []byte(dataWorkflowStart))
	endIdx := bytes.Index(resumeData, []byte(dataWorkflowEnd))

	switch {
	case startIdx >= 0 && endIdx >= 0 && endIdx >= startIdx:
		return replaceBlockSpan(ctx, resumePath, baselinePath, resumeData, snippetBody, baselineStr, startIdx, endIdx)
	case startIdx < 0 && endIdx < 0:
		return insertBlock(ctx, resumePath, baselinePath, resumeData, snippetBody)
	default:
		// Exactly one marker present — malformed.
		return handleMalformedMarkers(ctx, resumePath, baselinePath, resumeData, snippetBody, startIdx, endIdx)
	}
}

// replaceBlockSpan handles the "both markers present" case.
func replaceBlockSpan(ctx MigrationContext, resumePath, baselinePath string, resumeData []byte, snippetBody, baselineStr string, startIdx, endIdx int) ([]FileAction, error) {
	endOfBlock := endIdx + len(dataWorkflowEnd)
	currentSpan := string(resumeData[startIdx:endOfBlock])

	if currentSpan == snippetBody {
		// Idempotent: content matches. Backfill baseline silently if missing.
		if !ctx.DryRun && baselineStr != snippetBody {
			_ = os.WriteFile(baselinePath, []byte(snippetBody+"\n"), 0o644)
		}
		return nil, nil
	}

	// Span differs from snippet. Determine if this is a user edit (CONFLICT)
	// or a clean auto-update (UPDATE).
	if baselineStr != "" && currentSpan != baselineStr && !ctx.Force {
		// User edited the block AND the snippet changed — conflict.
		return []FileAction{{
			Path:     dataWorkflowResume,
			Action:   "CONFLICT",
			Location: "vault",
		}}, nil
	}

	if ctx.DryRun {
		return []FileAction{{
			Path:     dataWorkflowResume,
			Action:   "DRY-RUN",
			Location: "vault",
		}}, nil
	}

	// Rewrite span in-place.
	var buf bytes.Buffer
	buf.Write(resumeData[:startIdx])
	buf.WriteString(snippetBody)
	buf.Write(resumeData[endOfBlock:])
	if err := writeResume(resumePath, buf.Bytes()); err != nil {
		return nil, err
	}
	_ = os.WriteFile(baselinePath, []byte(snippetBody+"\n"), 0o644)
	return []FileAction{{
		Path:     dataWorkflowResume,
		Action:   "UPDATE",
		Location: "vault",
	}}, nil
}

// insertBlock handles the "neither marker present" case.
func insertBlock(ctx MigrationContext, resumePath, baselinePath string, resumeData []byte, snippetBody string) ([]FileAction, error) {
	if ctx.DryRun {
		return []FileAction{{
			Path:     dataWorkflowResume,
			Action:   "DRY-RUN",
			Location: "vault",
		}}, nil
	}

	newContent := insertAfterFirstH2(resumeData, snippetBody)
	if err := writeResume(resumePath, newContent); err != nil {
		return nil, err
	}
	_ = os.WriteFile(baselinePath, []byte(snippetBody+"\n"), 0o644)
	return []FileAction{{
		Path:     dataWorkflowResume,
		Action:   "INSERT",
		Location: "vault",
	}}, nil
}

// handleMalformedMarkers handles the "exactly one marker present" case.
// Without --force, record CONFLICT and leave the file alone. With --force,
// rewrite the span starting from whichever marker exists.
func handleMalformedMarkers(ctx MigrationContext, resumePath, baselinePath string, resumeData []byte, snippetBody string, startIdx, endIdx int) ([]FileAction, error) {
	if !ctx.Force {
		return []FileAction{{
			Path:     dataWorkflowResume,
			Action:   "CONFLICT",
			Location: "vault",
		}}, nil
	}

	if ctx.DryRun {
		return []FileAction{{
			Path:     dataWorkflowResume,
			Action:   "DRY-RUN",
			Location: "vault",
		}}, nil
	}

	var buf bytes.Buffer
	switch {
	case startIdx >= 0:
		// Only start marker — replace from start marker to EOF with snippetBody.
		buf.Write(resumeData[:startIdx])
		buf.WriteString(snippetBody)
	case endIdx >= 0:
		// Only end marker — replace from BOF through end marker with snippetBody.
		buf.WriteString(snippetBody)
		endOfBlock := endIdx + len(dataWorkflowEnd)
		buf.Write(resumeData[endOfBlock:])
	}
	if err := writeResume(resumePath, buf.Bytes()); err != nil {
		return nil, err
	}
	_ = os.WriteFile(baselinePath, []byte(snippetBody+"\n"), 0o644)
	return []FileAction{{
		Path:     dataWorkflowResume,
		Action:   "UPDATE",
		Location: "vault",
	}}, nil
}

// insertAfterFirstH2 returns resumeData with snippetBody inserted after the
// first `## ` heading's block (i.e., before the *next* heading or at EOF).
// If no `## ` heading is present, the snippet is appended to the end.
//
// Inserting after the first heading's block — rather than immediately after
// the `## ` line — keeps the existing first section intact and places the
// Data workflow content as its own sibling section. Appending a trailing
// newline ensures the snippet separates cleanly from whatever follows.
func insertAfterFirstH2(resumeData []byte, snippetBody string) []byte {
	lines := strings.Split(string(resumeData), "\n")

	// Locate first `## ` heading.
	firstH2 := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			firstH2 = i
			break
		}
	}

	if firstH2 < 0 {
		// No `## ` heading — append.
		trimmed := strings.TrimRight(string(resumeData), "\n")
		if trimmed == "" {
			return []byte(snippetBody + "\n")
		}
		return []byte(trimmed + "\n\n" + snippetBody + "\n")
	}

	// Find the next `## ` heading after firstH2, or EOF.
	insertAt := len(lines)
	for i := firstH2 + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			insertAt = i
			break
		}
	}

	// Build output: lines[:insertAt] + snippet + blank + lines[insertAt:].
	before := strings.Join(lines[:insertAt], "\n")
	before = strings.TrimRight(before, "\n")
	after := strings.Join(lines[insertAt:], "\n")

	var buf bytes.Buffer
	buf.WriteString(before)
	buf.WriteString("\n\n")
	buf.WriteString(snippetBody)
	if after != "" {
		buf.WriteString("\n\n")
		buf.WriteString(after)
	} else {
		buf.WriteString("\n")
	}
	return buf.Bytes()
}

// writeResume writes content to resumePath, creating parent directories as
// needed. The trailing newline is preserved if present; otherwise added.
func writeResume(resumePath string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(resumePath), 0o755); err != nil {
		return err
	}
	// Ensure file ends with a newline.
	if len(content) == 0 || content[len(content)-1] != '\n' {
		content = append(content, '\n')
	}
	return os.WriteFile(resumePath, content, 0o644)
}
