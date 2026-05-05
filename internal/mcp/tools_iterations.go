// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/config"
)

// iterationHeadingRegexp matches a full iteration heading line:
//
//	### Iteration N — Title (YYYY-MM-DD)
//	### Iteration N (revision K) — Title (YYYY-MM-DD)
//
// Title may itself contain em dashes (iter 118's title does), so the date in
// parentheses is the anchor and the title capture is non-greedy.
//
// The optional `(revision K)` clause is captured into group 2; bare
// (non-revision) headings leave group 2 empty. Captures shift down by
// one slot vs. the legacy regex: g1=N, g2=K|"", g3=Title, g4=Date.
//
// Revision blocks are emitted by the content-addressable idempotency
// path of `vv_append_iteration` (DESIGN #106). Recognising them here
// is what gives parseIterations a clean heading boundary so the
// original iter's narrative doesn't extend into the revision body —
// downstream consumers (collectHistoryRows, summary tools) then see
// them as distinct entries that they can dedupe on `Number`.
var iterationHeadingRegexp = regexp.MustCompile(`^### Iteration (\d+)(?: \(revision (\d+)\))?\s*—\s*(.+?)\s*\((\d{4}-\d{2}-\d{2})\)\s*$`)

// provenanceTrailerRE matches the HTML-comment provenance trailer appended by
// vv_append_iteration to an iteration narrative. Anchored to end-of-string so
// the strip is a no-op on blocks without a trailer.
var provenanceTrailerRE = regexp.MustCompile(`\n*<!-- recorded:[^\n]*-->\s*\z`)

// frontmatterKeyValRE captures `key: value` lines inside a YAML front-matter
// block. Keys are conservative ASCII identifiers (letters, digits, underscores)
// and the value runs to end-of-line, with surrounding whitespace trimmed by
// the caller. The regex is only consulted between the opening and closing
// `---` markers, so non-matching lines inside the block are silently skipped.
var frontmatterKeyValRE = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*:\s*(.*?)\s*$`)

// Iteration is one parsed entry from iterations.md. Narrative uses omitempty
// so the compact "table" response format can drop narrative bodies cleanly.
//
// Frontmatter holds the optional YAML key/value pairs the iter writer may
// place between the heading and the body (per the iterations-summary-
// frontmatter task — D1, D4). It is populated by parseIterations and is
// json:"-" so the field never serializes into table or full responses
// (closes the v3-M5 leakage gate).
//
// Revision is non-zero (≥2) when the entry is a `### Iteration N (revision K)`
// block emitted by the content-addressable idempotency path
// (DESIGN #106). The original block carries Revision=0. Downstream
// consumers that emit one row per iteration N (project-history-tail,
// vv_get_iterations summary) dedupe on Number — see the deduping
// logic in collectHistoryRows and the summary handler.
type Iteration struct {
	Number      int               `json:"number"`
	Revision    int               `json:"revision,omitempty"`
	Date        string            `json:"date"`
	Title       string            `json:"title"`
	Narrative   string            `json:"narrative,omitempty"`
	Frontmatter map[string]string `json:"-"`
}

// parseIterations walks an iterations.md body and returns structured entries
// in document order. Content before the first "### Iteration N" heading is
// preamble and ignored. Each entry's narrative is everything between its
// heading and the next heading (or EOF), with leading/trailing blank lines
// trimmed.
//
// If the body opens with a YAML front-matter block (a `---` fence on a line
// by itself, optionally preceded by blank lines), the key/value pairs are
// parsed into Iteration.Frontmatter and the entire fenced block is stripped
// from Narrative. A malformed front-matter block (no closing `---`) logs a
// warning and falls back to no-frontmatter capture; the unclosed YAML stays
// inside Narrative so operators can spot the drift.
func parseIterations(content string) []Iteration {
	var out []Iteration
	var current *Iteration
	var buf strings.Builder

	flush := func() {
		if current == nil {
			return
		}
		raw := strings.TrimLeft(buf.String(), "\n")
		fm, body, ok := extractIterationFrontmatter(raw)
		if ok {
			current.Frontmatter = fm
			raw = body
		}
		narr := strings.TrimSpace(raw)
		narr = provenanceTrailerRE.ReplaceAllString(narr, "")
		current.Narrative = strings.TrimRight(narr, "\n")
		out = append(out, *current)
		buf.Reset()
		current = nil
	}

	for _, line := range strings.Split(content, "\n") {
		if m := iterationHeadingRegexp.FindStringSubmatch(line); m != nil {
			flush()
			num, _ := strconv.Atoi(m[1])
			rev := 0
			if m[2] != "" {
				rev, _ = strconv.Atoi(m[2])
			}
			current = &Iteration{
				Number:   num,
				Revision: rev,
				Title:    strings.TrimSpace(m[3]),
				Date:     m[4],
			}
			continue
		}
		if current != nil {
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	flush()
	return out
}

// extractIterationFrontmatter inspects the trimmed body of an iteration
// (everything between its heading and the next heading) and, if it opens
// with a YAML-style `---` fence, captures the enclosed key/value pairs and
// returns the body with the fenced block stripped.
//
// Returns (frontmatter, body-without-block, ok). When ok is false the body
// is returned untouched and frontmatter is nil — covering both the no-front-
// matter case and the malformed (unterminated) case (which also logs a
// warning, per the parser-strip back-compat contract).
func extractIterationFrontmatter(body string) (map[string]string, string, bool) {
	lines := strings.Split(body, "\n")
	// Skip leading blank lines so a body that opens with a blank line then
	// `---` still picks up the front-matter cleanly.
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || strings.TrimSpace(lines[i]) != "---" {
		return nil, body, false
	}
	openIdx := i
	i++

	fm := map[string]string{}
	closed := false
	for ; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "---" {
			closed = true
			break
		}
		if trimmed == "" {
			// Blank lines inside the fence are tolerated.
			continue
		}
		if m := frontmatterKeyValRE.FindStringSubmatch(trimmed); m != nil {
			key := m[1]
			val := strings.TrimSpace(m[2])
			fm[key] = val
		}
		// Unrecognised lines are silently skipped (forward-compat for
		// future schema additions or stray block content).
	}

	if !closed {
		log.Printf("vv: warning — iteration front-matter at line %d has no closing '---'; treating as narrative",
			openIdx+1)
		return nil, body, false
	}

	// Rejoin the body after the closing fence. i now indexes the closing
	// `---` line; everything after that is the real narrative.
	rest := ""
	if i+1 < len(lines) {
		rest = strings.Join(lines[i+1:], "\n")
	}
	// Strip a single leading newline-blank-line if present so the narrative
	// starts cleanly. TrimSpace at the call site handles the rest.
	rest = strings.TrimLeft(rest, "\n")
	return fm, rest, true
}

// IterationSummary is the shape returned by format="summary". Each entry
// reports the iter heading data plus the structured `summary` and `shape`
// from front-matter (when present) or first-paragraph fallback (per D8).
// The `*_present` discriminators tell consumers whether the value came
// from front-matter; `shape_present=false` means no fallback exists for
// shape (per D3).
type IterationSummary struct {
	Number         int    `json:"number"`
	Date           string `json:"date"`
	Title          string `json:"title"`
	Shape          string `json:"shape"`
	ShapePresent   bool   `json:"shape_present"`
	Summary        string `json:"summary"`
	SummaryPresent bool   `json:"summary_present"`
}

// NewGetIterationsTool creates the vv_get_iterations tool.
func NewGetIterationsTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name:        "vv_get_iterations",
			Description: "Get iteration narratives from a project's iterations.md. Defaults to the 10 most recent entries in compact table format. Use format=\"full\" to include narrative bodies, format=\"summary\" for a body-free 1-line digest per iter (with first-paragraph fallback when front-matter is absent), or since_iteration to fetch a specific range.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"limit": {
						"type": "integer",
						"description": "Maximum number of iterations to return, newest-first. Default: 10."
					},
					"since_iteration": {
						"type": "integer",
						"description": "Only return iterations with number >= this value. Limit still applies to the filtered set."
					},
					"format": {
						"type": "string",
						"enum": ["table", "full", "summary"],
						"description": "\"table\" returns {number,date,title} only (compact). \"full\" includes the narrative body. \"summary\" returns {number,date,title,shape,shape_present,summary,summary_present} — front-matter when present, else first-paragraph fallback for the summary. Default: \"table\"."
					}
				}
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project        string `json:"project"`
				Limit          *int   `json:"limit"`
				SinceIteration *int   `json:"since_iteration"`
				Format         string `json:"format"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			limit := 10
			if args.Limit != nil {
				limit = *args.Limit
				if limit < 1 {
					return "", fmt.Errorf("limit must be >= 1")
				}
			}

			format := args.Format
			if format == "" {
				format = "table"
			}
			if format != "table" && format != "full" && format != "summary" {
				return "", fmt.Errorf("invalid format %q — must be \"table\", \"full\", or \"summary\"", format)
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			path := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "iterations.md")
			absPath, err := vaultPrefixCheck(path, cfg.VaultPath)
			if err != nil {
				return "", err
			}

			data, err := os.ReadFile(absPath)
			if err != nil {
				if os.IsNotExist(err) {
					return "", fmt.Errorf("iterations.md not found for project %q — run `vv context init` first", project)
				}
				return "", fmt.Errorf("read iterations: %w", err)
			}

			parsed := parseIterations(string(data))

			// Dedupe revision blocks (DESIGN #106) so consumers see
			// one row per iteration N — last-wins on revision K, so a
			// divergent re-wrap surfaces as the most recent
			// authoritative narrative. The original Revision=0 entry
			// is replaced by the highest-K revision for the same N.
			byIter := make(map[int]Iteration, len(parsed))
			order := make([]int, 0, len(parsed))
			for _, it := range parsed {
				if prev, ok := byIter[it.Number]; ok {
					if it.Revision > prev.Revision {
						byIter[it.Number] = it
					}
					continue
				}
				byIter[it.Number] = it
				order = append(order, it.Number)
			}
			all := make([]Iteration, 0, len(order))
			for _, n := range order {
				all = append(all, byIter[n])
			}
			total := len(all)

			if args.SinceIteration != nil {
				filtered := all[:0]
				for _, it := range all {
					if it.Number >= *args.SinceIteration {
						filtered = append(filtered, it)
					}
				}
				all = filtered
			}

			// Newest-first
			for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
				all[i], all[j] = all[j], all[i]
			}

			if len(all) > limit {
				all = all[:limit]
			}

			if format == "summary" {
				summaries := make([]IterationSummary, 0, len(all))
				for i := range all {
					it := &all[i]
					entry := IterationSummary{
						Number: it.Number,
						Date:   it.Date,
						Title:  it.Title,
					}
					if shape := it.Frontmatter["shape"]; shape != "" {
						entry.Shape = shape
						entry.ShapePresent = true
					}
					if s := it.Frontmatter["summary"]; s != "" {
						entry.Summary = s
						entry.SummaryPresent = true
					} else {
						entry.Summary = truncateForSummary(it.Narrative, 200)
						// summary_present remains false — the value came from
						// the first-paragraph fallback, not front-matter.
					}
					summaries = append(summaries, entry)
				}

				result := struct {
					Project    string             `json:"project"`
					Total      int                `json:"total"`
					Returned   int                `json:"returned"`
					Iterations []IterationSummary `json:"iterations"`
				}{
					Project:    project,
					Total:      total,
					Returned:   len(summaries),
					Iterations: summaries,
				}
				if result.Iterations == nil {
					result.Iterations = []IterationSummary{}
				}
				outBytes, marshalErr := json.MarshalIndent(result, "", "  ")
				if marshalErr != nil {
					return "", fmt.Errorf("marshal: %w", marshalErr)
				}
				return string(outBytes) + "\n", nil
			}

			if format == "table" {
				for i := range all {
					all[i].Narrative = ""
				}
			}

			result := struct {
				Project    string      `json:"project"`
				Total      int         `json:"total"`
				Returned   int         `json:"returned"`
				Iterations []Iteration `json:"iterations"`
			}{
				Project:    project,
				Total:      total,
				Returned:   len(all),
				Iterations: all,
			}
			if result.Iterations == nil {
				result.Iterations = []Iteration{}
			}

			out, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal: %w", err)
			}
			return string(out) + "\n", nil
		},
	}
}
