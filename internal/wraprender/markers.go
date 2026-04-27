// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package wraprender renders the state-derived sub-regions of resume.md
// (active-tasks list, current-state invariant bullets, project-history tail)
// into HTML-comment-bounded marker blocks.
//
// The package is the Phase-1 deliverable of the
// `wrap-resume-marker-blocks-for-state-derived-content` plan: it provides
// pure rendering helpers plus a self-healing apply step that the Phase-2
// `applyResumeStateBlocks` (Step 9 of `ApplyBundle`) calls to converge
// resume.md state-blocks to filesystem ground truth on every wrap.
//
// Marker convention:
//
//	<!-- vv:<region>:start -->
//	... rendered block ...
//	<!-- vv:<region>:end -->
//
// Three regions are supported:
//   - active-tasks
//   - current-state
//   - project-history-tail
//
// `ApplyMarkerBlocks` is self-healing: when a region's marker pair is
// absent, the function inserts the pair at a sensible default location
// relative to the existing H2/H3 anchors of resume.md before writing the
// rendered contents.
package wraprender

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// TaskFrontMatter is the renderer-facing view of a `tasks/*.md` file's
// front-matter as parsed by the apply-step. The renderer is
// I/O-free; the apply-step (Phase 2) populates these structs from disk.
type TaskFrontMatter struct {
	// Slug is the task filename without the trailing `.md` suffix.
	Slug string
	// Title is the task's display title (front-matter `title:` or the
	// first H1 heading inside the file).
	Title string
	// Status is the front-matter `status:` value (free-form short tag,
	// e.g., "Draft v1", "Phase 1 complete").
	Status string
	// Priority is the front-matter `priority:` value. The renderer
	// recognises "high", "medium", "low" for sort ordering; any other
	// value (including "") sorts after "low".
	Priority string
}

// CurrentState carries the headline counts the renderer emits inside the
// `current-state` marker block. Field semantics match the lock in the
// plan's "Headline-count sources (locked)" section.
//
// Test-count tracking deliberately lives outside this struct: per the
// Phase 2 review (Option C), the rendered marker block emits Iterations /
// MCP / Templates only, and operator-authored prose adjacent to the
// marker captures any test-count narrative. This avoids running
// `go test` on every wrap and avoids the headline-meaning shift between
// RUN-counted (subtest-inclusive) and function-counted enumeration.
type CurrentState struct {
	// Iterations is the count of `### Iteration N` headings in
	// `iterations.md`.
	Iterations int
	// MCPTools is the length of `(*mcp.Server).ToolNames()`.
	MCPTools int
	// Templates is the count of files (excluding directories) under
	// `templates.AgentctxFS()`.
	Templates int
}

// HistoryRow is one row of the `## Project History (recent)` table inside
// the `project-history-tail` marker block.
type HistoryRow struct {
	// Iteration is the iteration number (the `N` in `### Iteration N`).
	Iteration int
	// Date is the iteration date in `YYYY-MM-DD`.
	Date string
	// Summary is a one-line summary of the iteration.
	Summary string
}

// Region identifiers used in the `<!-- vv:<region>:start/end -->` marker
// pairs. Exported so callers can build maps keyed off the same string
// literals the renderer recognises.
const (
	RegionActiveTasks        = "active-tasks"
	RegionCurrentState       = "current-state"
	RegionProjectHistoryTail = "project-history-tail"
)

// markerStart returns the opening marker for a region.
func markerStart(region string) string {
	return "<!-- vv:" + region + ":start -->"
}

// markerEnd returns the closing marker for a region.
func markerEnd(region string) string {
	return "<!-- vv:" + region + ":end -->"
}

// priorityRank maps a priority string to a sort rank; lower ranks sort
// first. Unknown / empty priorities sort last (rank 3).
func priorityRank(p string) int {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	default:
		return 3
	}
}

// RenderActiveTasks renders the active-tasks H3 block.
//
// Output shape with N>0:
//
//	### Active tasks (N)
//
//	- **`<slug>`** (priority: <p>, status: <s>) — <title>
//	...
//
// Empty list output:
//
//	### Active tasks (0)
//
//	_No active tasks._
//
// Sort order is priority desc (high > medium > low > unset) then
// alphabetical by slug. Title pass-through: markdown-special characters in
// the title (pipe, asterisk, backtick, brackets) are emitted verbatim
// because the bullet shape (slug in a code span, fixed-position priority
// and status fields) does not depend on title content. Locked by
// `TestRenderActiveTasks_TitleEscaping`.
func RenderActiveTasks(tasks []TaskFrontMatter) string {
	if len(tasks) == 0 {
		return "### Active tasks (0)\n\n_No active tasks._\n"
	}
	sorted := make([]TaskFrontMatter, len(tasks))
	copy(sorted, tasks)
	sort.SliceStable(sorted, func(i, j int) bool {
		ri, rj := priorityRank(sorted[i].Priority), priorityRank(sorted[j].Priority)
		if ri != rj {
			return ri < rj
		}
		return sorted[i].Slug < sorted[j].Slug
	})
	var b strings.Builder
	fmt.Fprintf(&b, "### Active tasks (%d)\n\n", len(sorted))
	for _, t := range sorted {
		priority := strings.TrimSpace(t.Priority)
		if priority == "" {
			priority = "unset"
		}
		status := strings.TrimSpace(t.Status)
		if status == "" {
			status = "unset"
		}
		title := strings.TrimSpace(t.Title)
		fmt.Fprintf(&b, "- **`%s`** (priority: %s, status: %s) — %s\n",
			t.Slug, priority, status, title)
	}
	return b.String()
}

// RenderCurrentState renders the three-bullet invariant block consumed by
// the `current-state` marker. Output passes
// `internal/context.ValidateCurrentStateBody` (v10 contract) — locked by
// `TestRenderCurrentState_OutputPassesV10Validator`.
//
// Locked output shape:
//
//   - **Iterations:** <N> complete
//   - **MCP:** <N> tools + 1 prompt
//   - **Embedded:** <N> templates
func RenderCurrentState(state CurrentState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- **Iterations:** %d complete\n", state.Iterations)
	fmt.Fprintf(&b, "- **MCP:** %d tools + 1 prompt\n", state.MCPTools)
	fmt.Fprintf(&b, "- **Embedded:** %d templates\n", state.Templates)
	return b.String()
}

// RenderProjectHistoryTail renders the last `n` rows of the project
// history as a markdown table inside the `project-history-tail` marker.
// If `n <= 0`, all rows are rendered. With zero rows the function emits a
// table header followed by an italic "no iterations recorded yet" pointer
// — chosen over a bare table so the rendered region remains valid GFM and
// readable in plain markdown viewers; locked by
// `TestRenderProjectHistoryTail_HandlesEmptyIterations`.
func RenderProjectHistoryTail(rows []HistoryRow, n int) string {
	var b strings.Builder
	b.WriteString("| #   | Date       | Summary |\n")
	b.WriteString("| --- | ---------- | ------- |\n")
	if len(rows) == 0 {
		b.WriteString("\n_No iterations recorded yet._\n")
		return b.String()
	}
	start := 0
	if n > 0 && len(rows) > n {
		start = len(rows) - n
	}
	for _, r := range rows[start:] {
		summary := strings.ReplaceAll(r.Summary, "|", `\|`)
		summary = strings.ReplaceAll(summary, "\n", " ")
		fmt.Fprintf(&b, "| %d | %s | %s |\n", r.Iteration, r.Date, summary)
	}
	return b.String()
}

// ErrMissingSection is returned by `ApplyMarkerBlocks` when a region's
// marker pair is absent AND the H2 anchor needed for default insertion is
// missing too. The caller (Phase 2 apply-step) decides what to do; the
// renderer treats it as a structural failure.
var ErrMissingSection = errors.New("wraprender: required H2 section missing for marker insertion")

// ErrMalformedMarker is returned when a region's start marker is found
// without a matching end marker. The renderer refuses to guess where the
// region ends; the operator must repair the file (or delete the start
// marker so self-healing inserts a fresh pair).
var ErrMalformedMarker = errors.New("wraprender: start marker found without matching end marker")

// regionInsertion describes where to insert a marker pair when it is
// absent. `h2` is the canonical H2 heading the region lives under; `h3`
// (if non-empty) is an H3 subheading the marker sits immediately under
// (used for active-tasks, which goes under the `### Active tasks` H3).
type regionInsertion struct {
	region string
	h2     string
	h3     string
}

// regionInsertionPoints is the per-region default-insertion map. Each
// entry encodes the H2 (and optional H3) anchor used when the marker pair
// is absent.
var regionInsertionPoints = []regionInsertion{
	{region: RegionActiveTasks, h2: "Open Threads", h3: "Active tasks"},
	{region: RegionCurrentState, h2: "Current State"},
	{region: RegionProjectHistoryTail, h2: "Project History (recent)"},
}

// ApplyMarkerBlocks replaces the contents of each named marker region in
// `resumeContent` with the supplied block. When a region's marker pair is
// absent, the function inserts the pair at the default location relative
// to the H2/H3 anchors documented in `regionInsertionPoints`.
//
// Self-healing semantics: only regions named in `blocks` are touched.
// Regions whose marker pair is present but absent from `blocks` are left
// untouched. The function preserves byte-equality of the file outside the
// touched marker spans.
//
// Errors:
//   - ErrMalformedMarker: a start marker is present without a matching
//     end marker.
//   - ErrMissingSection: a region's marker pair is absent AND its H2
//     anchor is missing, so default insertion has nowhere to land.
//
// Idempotence: calling `ApplyMarkerBlocks` twice with the same inputs
// returns byte-identical output the second time. Locked by
// `TestApplyMarkerBlocks_Idempotent`.
func ApplyMarkerBlocks(resumeContent string, blocks map[string]string) (string, error) {
	out := resumeContent
	// Iterate in the canonical regionInsertionPoints order so multiple
	// insertions on a markerless file land deterministically.
	for _, ip := range regionInsertionPoints {
		block, ok := blocks[ip.region]
		if !ok {
			continue
		}
		var err error
		out, err = applyOne(out, ip, block)
		if err != nil {
			return "", err
		}
	}
	return out, nil
}

// applyOne replaces or inserts a single region's marker block. The
// function is split out from ApplyMarkerBlocks so the per-region logic is
// covered by its own test cases.
func applyOne(content string, ip regionInsertion, block string) (string, error) {
	startTag := markerStart(ip.region)
	endTag := markerEnd(ip.region)

	startIdx := strings.Index(content, startTag)
	endIdx := strings.Index(content, endTag)

	switch {
	case startIdx >= 0 && endIdx >= 0 && endIdx > startIdx:
		// Replace existing region. Preserve everything outside the
		// span [startIdx .. endIdx+len(endTag)).
		afterStart := startIdx + len(startTag)
		// Normalise the body: a single leading and trailing newline
		// around the rendered block, regardless of how the previous
		// run shaped it. This keeps the second run byte-identical to
		// the first (idempotence).
		body := "\n" + ensureTrailingNewline(block)
		return content[:afterStart] + body + content[endIdx:], nil
	case startIdx >= 0 && (endIdx < 0 || endIdx <= startIdx):
		return "", fmt.Errorf("%w: region=%s", ErrMalformedMarker, ip.region)
	case startIdx < 0 && endIdx >= 0:
		// End marker without start is also malformed; treat the same
		// way so callers don't need to distinguish.
		return "", fmt.Errorf("%w: region=%s", ErrMalformedMarker, ip.region)
	}

	// Markers absent — insert at the default location.
	return insertRegion(content, ip, block)
}

// insertRegion inserts a fresh marker pair (with `block` between) into
// `content` at the default anchor for `ip`.
//
// Insertion rules:
//   - active-tasks: inside `## Open Threads`. If `### Active tasks` H3
//     already exists, the marker pair replaces that H3 and its body
//     up to the next `### ` heading (because the rendered block carries
//     its own `### Active tasks (N)` H3). Otherwise the marker pair is
//     inserted at the top of the `## Open Threads` body.
//   - current-state: at the top of the `## Current State` body. Existing
//     prose below is preserved adjacent to the inserted block.
//   - project-history-tail: at the top of the `## Project History
//     (recent)` body. Existing table/prose below is preserved adjacent.
func insertRegion(content string, ip regionInsertion, block string) (string, error) {
	lines := strings.Split(content, "\n")
	h2Idx := findHeading(lines, 2, ip.h2)
	if h2Idx < 0 {
		return "", fmt.Errorf("%w: %q (region=%s)", ErrMissingSection, ip.h2, ip.region)
	}
	bodyStart := h2Idx + 1
	bodyEnd := findNextHeading(lines, bodyStart, 2)

	startTag := markerStart(ip.region)
	endTag := markerEnd(ip.region)

	if ip.h3 != "" {
		// active-tasks special case: replace the existing H3 block
		// (heading + body up to the next `### ` or `## `) with the
		// marker pair, since the rendered block already carries its
		// own `### Active tasks (N)` heading. Match by prefix because
		// the live H3 carries a count suffix (e.g., `### Active tasks
		// (3)`).
		h3Idx := findHeadingPrefixInRange(lines, bodyStart, bodyEnd, 3, ip.h3)
		if h3Idx >= 0 {
			h3End := findNextH3OrH2(lines, h3Idx+1, bodyEnd)
			marker := []string{
				startTag,
				strings.TrimRight(block, "\n"),
				endTag,
				"",
			}
			newLines := make([]string, 0, len(lines)+len(marker)-(h3End-h3Idx))
			newLines = append(newLines, lines[:h3Idx]...)
			newLines = append(newLines, marker...)
			newLines = append(newLines, lines[h3End:]...)
			return strings.Join(newLines, "\n"), nil
		}
	}

	// Default insertion: drop the marker pair at the top of the H2
	// body (immediately after the heading line), with one blank line
	// before any pre-existing body content.
	insertion := []string{
		"",
		startTag,
		strings.TrimRight(block, "\n"),
		endTag,
		"",
	}
	newLines := make([]string, 0, len(lines)+len(insertion))
	newLines = append(newLines, lines[:bodyStart]...)
	newLines = append(newLines, insertion...)
	newLines = append(newLines, lines[bodyStart:]...)
	return strings.Join(newLines, "\n"), nil
}

// findHeading returns the index of a `^#{level} <title>` heading in
// `lines`, or -1 if absent. Match is exact on title (after trimming
// surrounding whitespace).
func findHeading(lines []string, level int, title string) int {
	prefix := strings.Repeat("#", level) + " "
	want := strings.TrimSpace(title)
	for i, ln := range lines {
		if !strings.HasPrefix(ln, prefix) {
			continue
		}
		got := strings.TrimSpace(strings.TrimPrefix(ln, prefix))
		if got == want {
			return i
		}
	}
	return -1
}

// findHeadingPrefixInRange returns the index of a `^#{level} <title>...`
// heading in lines[start:end]. The match is by prefix: the heading's
// title must start with `title` followed by end-of-line or a non-word
// separator. This accommodates rendered H3s that carry a trailing count
// or qualifier (e.g., `### Active tasks (3)`).
func findHeadingPrefixInRange(lines []string, start, end, level int, title string) int {
	if start < 0 {
		start = 0
	}
	if end < 0 || end > len(lines) {
		end = len(lines)
	}
	prefix := strings.Repeat("#", level) + " "
	want := strings.TrimSpace(title)
	for i := start; i < end; i++ {
		if !strings.HasPrefix(lines[i], prefix) {
			continue
		}
		got := strings.TrimSpace(strings.TrimPrefix(lines[i], prefix))
		if got == want {
			return i
		}
		if strings.HasPrefix(got, want) {
			rest := got[len(want):]
			if rest == "" || rest[0] == ' ' || rest[0] == '(' || rest[0] == '\t' {
				return i
			}
		}
	}
	return -1
}

// findNextHeading returns the index of the next heading at the given
// level or shallower, starting from `from`. Returns len(lines) if none.
func findNextHeading(lines []string, from, level int) int {
	for i := from; i < len(lines); i++ {
		ln := lines[i]
		if !strings.HasPrefix(ln, "#") {
			continue
		}
		// Count leading hashes.
		hashes := 0
		for hashes < len(ln) && ln[hashes] == '#' {
			hashes++
		}
		if hashes <= level && hashes < len(ln) && ln[hashes] == ' ' {
			return i
		}
	}
	return len(lines)
}

// findNextH3OrH2 returns the index of the next `### ` or `## ` heading
// after `from`, capped at `limit`.
func findNextH3OrH2(lines []string, from, limit int) int {
	if limit < 0 || limit > len(lines) {
		limit = len(lines)
	}
	for i := from; i < limit; i++ {
		ln := lines[i]
		if strings.HasPrefix(ln, "## ") || strings.HasPrefix(ln, "### ") {
			return i
		}
	}
	return limit
}

// ensureTrailingNewline guarantees `s` ends with exactly one `\n`. Used
// to normalise the body inside a marker pair so idempotence holds across
// arbitrary input shaping.
func ensureTrailingNewline(s string) string {
	s = strings.TrimRight(s, "\n")
	return s + "\n"
}
