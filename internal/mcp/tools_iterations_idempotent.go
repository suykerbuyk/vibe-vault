// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

// Content-addressable idempotency for `vv_append_iteration`.
//
// The tool implements a deterministic three-case contract over
// `(existing iterations.md bytes, incoming title+narrative+date+
// summary+shape)`:
//
//  1. Heading absent (`### Iteration N` not present) → normal append.
//  2. Heading present, body byte-identical (after trailing-newline
//     normalization) → idempotent no-op. The existing entry's
//     metadata is returned with `idempotent: true`. Zero LLM round-
//     trips. This is the multi-host-converge path and the wrap-retry-
//     after-transient-failure path.
//  3. Heading present, body differs → append a revision block beneath
//     the original. Heading shape: `### Iteration N (revision K)`
//     where K is incremented from the highest existing revision for N
//     (or 2 on first divergence). Frontmatter on the revision block
//     carries `revises: N` and `revised_at: <RFC3339 timestamp>`. The
//     original heading and body are untouched.
//
// Whitespace normalization (documented to match the Draft v2 plan):
//   - Trailing newlines on each side are stripped before byte-
//     comparison; tolerate single-vs-double trailing newline between
//     blocks.
//   - Internal whitespace, list indentation, and quote characters are
//     NOT normalized — they are semantic for markdown rendering, and
//     normalizing them would silently discard intentional formatting
//     refinements.
//   - The provenance trailer (`<!-- recorded: host=H ... -->`) is
//     stripped from the existing block before comparison so a re-wrap
//     from a different host with otherwise byte-identical narrative
//     classifies as the idempotent no-op rather than a divergence.
//
// Revision-block bookkeeping: revisions occupy their own
// `### Iteration N (revision K)` heading rather than being inlined
// under the original heading. Downstream parsers
// (`scanIterationNumbers`, `iterNarrativeRe`,
// `nextIterFromIterationsMD`, `iterationHeadingRegexp`,
// `parseIterations`, `collectHistoryRows`) all key off the iteration
// number, so revisions are visible to consumers but never fragment
// `iter_n` or the project-history-tail row count.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// iterationHeadingWithRevisionRe matches both plain
// `### Iteration N` and revision `### Iteration N (revision K)`
// headings. Capture group 1 is the iteration number; capture group 2
// (when present) is the revision number K.
//
// The regex is anchored at line start (multiline flag) and matches
// only the heading line. The optional `(revision K)` clause is
// captured eagerly via two alternatives so the regex engine prefers
// the revision-bearing form when present (Go's RE2 is leftmost-first
// without alternative ordering, so the explicit alternative is
// expressed via two separate compiled passes — see findIterationBlock
// for the actual matching loop).
var iterationHeadingWithRevisionRe = regexp.MustCompile(
	`(?m)^### Iteration (\d+)(?: \(revision (\d+)\))?(?:[ \t]|$|—)`)

// provenanceTrailerStripRE matches the HTML-comment provenance
// trailer that `provenanceTrailer` emits (`<!-- recorded: ... -->`).
// Anchored to end-of-text and tolerant of trailing whitespace so
// stripping is a no-op on bodies without a trailer.
var provenanceTrailerStripRE = regexp.MustCompile(
	`\n+<!--\s*recorded:[^\n]*-->\s*\z`)

// iterationBlockSpan reports the byte range of an existing iteration
// block in `content`, plus the highest revision number seen for that
// iter. The block runs from the start of the matching heading line up
// to (but not including) the next `### Iteration ...` heading or
// end-of-file.
//
// Returns (-1, -1, 0, false) when the iter has no heading present.
type iterationBlockSpan struct {
	headingStart   int  // byte offset of the matching heading line
	bodyStart      int  // byte offset just past the heading's newline
	bodyEnd        int  // byte offset of the next ### Iteration heading or EOF
	highestRev     int  // highest revision number observed for iterN
	originalExists bool // true iff a non-revision (### Iteration N without "(revision ...)") heading exists
}

// findIterationBlock walks `content` for all `### Iteration N` and
// `### Iteration N (revision K)` headings matching iterN. Returns the
// span of the original (non-revision) heading and the highest
// revision number K observed. If no original heading is present,
// `originalExists` is false and the caller should append normally.
func findIterationBlock(content string, iterN int) iterationBlockSpan {
	span := iterationBlockSpan{headingStart: -1, bodyStart: -1, bodyEnd: -1}
	matches := iterationHeadingWithRevisionRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return span
	}
	// Pre-extract a parallel view of {headingStart, iterN, revK?} so we
	// can both locate the original block and compute next-end via the
	// next match in document order.
	type entry struct {
		start int
		n     int
		rev   int
	}
	var entries []entry
	for _, m := range matches {
		// m layout: [matchStart, matchEnd, g1Start, g1End, g2Start?, g2End?]
		nStr := content[m[2]:m[3]]
		n, err := strconv.Atoi(nStr)
		if err != nil {
			continue
		}
		rev := 0
		if len(m) >= 6 && m[4] >= 0 {
			revStr := content[m[4]:m[5]]
			if r, rerr := strconv.Atoi(revStr); rerr == nil {
				rev = r
			}
		}
		entries = append(entries, entry{start: m[0], n: n, rev: rev})
	}
	for i, e := range entries {
		if e.n != iterN {
			continue
		}
		if e.rev > span.highestRev {
			span.highestRev = e.rev
		}
		if e.rev == 0 && !span.originalExists {
			// First (and per-construction only) original heading.
			span.originalExists = true
			span.headingStart = e.start
			// bodyStart = end of the heading line (advance to next
			// newline + 1).
			eol := strings.IndexByte(content[e.start:], '\n')
			if eol < 0 {
				span.bodyStart = len(content)
			} else {
				span.bodyStart = e.start + eol + 1
			}
			// bodyEnd = next heading start or EOF.
			if i+1 < len(entries) {
				span.bodyEnd = entries[i+1].start
			} else {
				span.bodyEnd = len(content)
			}
		}
	}
	return span
}

// extractIterationBody returns the body bytes between the heading
// line and the next iteration heading (or EOF), with the trailing
// provenance trailer stripped and the leading blank-line separator
// (the `\n\n` between heading and body emitted by the writer)
// normalized away. Used both to surface the existing body for
// byte-comparison and to populate the idempotent-no-op response
// metadata.
func extractIterationBody(content string, span iterationBlockSpan) string {
	if !span.originalExists {
		return ""
	}
	body := content[span.bodyStart:span.bodyEnd]
	// Strip the leading blank-line separator. The writer emits
	// `\n<heading>\n\n<body>...` — `bodyStart` lands right after
	// the heading's terminating `\n`, so the slice begins with the
	// `\n` of the blank-line separator. Trim leading `\n` so the
	// canonical body mirrors what `canonicalIterationBody` builds
	// from `(frontmatterBlock, narrative)`.
	body = strings.TrimLeft(body, "\n")
	// Strip trailing newlines so the comparison is anchored to body
	// content rather than block separators.
	body = strings.TrimRight(body, "\n")
	// Strip the provenance trailer if present so a re-wrap from a
	// different host with byte-identical narrative still classifies
	// as idempotent.
	body = provenanceTrailerStripRE.ReplaceAllString(body, "")
	body = strings.TrimRight(body, "\n")
	return body
}

// canonicalIterationBody constructs the byte sequence that
// `extractIterationBody` would return for the same heading + body
// pair. Used to compare incoming inputs against the existing block.
//
// `frontmatterBlock` is the empty string when neither summary nor
// shape was supplied (legacy shape); `narrative` is the trimmed
// narrative body. The function reconstructs the same body bytes the
// writer would emit MINUS the provenance trailer (stripped from both
// sides before comparison).
func canonicalIterationBody(frontmatterBlock, narrative string) string {
	body := frontmatterBlock + strings.TrimRight(narrative, "\n")
	body = strings.TrimRight(body, "\n")
	return body
}

// canonicalEqual compares two body strings under the documented
// whitespace-normalization rule. Trailing newlines on each side are
// stripped before byte-comparison; internal whitespace and list
// indentation are NOT normalized.
func canonicalEqual(existingBody, incomingBody string) bool {
	a := strings.TrimRight(existingBody, "\n \t")
	b := strings.TrimRight(incomingBody, "\n \t")
	return a == b
}

// buildIterationBlockBytes returns the full block bytes
// `\n<heading>\n\n<body><trailer>\n` matching the writer's exact
// output shape. Used both for the legacy append path and for the
// revision-block append path.
func buildIterationBlockBytes(heading, body, trailer string) string {
	return fmt.Sprintf("\n%s\n\n%s%s\n", heading, body, trailer)
}

// buildRevisionFrontmatterBlock constructs the YAML front-matter for
// a revision block. Always emits `revises:` and `revised_at:`; emits
// `shape:` and `summary:` when they were supplied to the call.
//
// The block is terminated with a trailing blank line so the narrative
// body starts cleanly below.
func buildRevisionFrontmatterBlock(revises int, revisedAt time.Time, summary, shape string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "revises: %d\n", revises)
	fmt.Fprintf(&b, "revised_at: %s\n", revisedAt.UTC().Format(time.RFC3339))
	if shape != "" {
		fmt.Fprintf(&b, "shape: %s\n", shape)
	}
	if summary != "" {
		fmt.Fprintf(&b, "summary: %s\n", summary)
	}
	b.WriteString("---\n\n")
	return b.String()
}

// revisionHeading builds the canonical `### Iteration N (revision K)`
// heading line.
func revisionHeading(num, rev int, title, date string) string {
	return fmt.Sprintf("### Iteration %d (revision %d) — %s (%s)", num, rev, title, date)
}

// appendIterationOutcome captures which of the three contract cases
// the call resolved to. Returned to the handler so it can shape the
// JSON response.
type appendIterationOutcome struct {
	Action      string // "appended" | "idempotent" | "revised"
	Iteration   int
	Revision    int    // 0 for the original; ≥2 for revisions
	Heading     string // exact heading line written (or matched, if idempotent)
	NewContent  string // updated iterations.md bytes (only set when Action != "idempotent")
	IsIdempotent bool
}

// resolveAppendIteration computes the outcome for the three-case
// contract. Pure function modulo `meta.Stamp()` (called by the
// handler upstream and threaded in via `trailer`). Returns the
// outcome plus the final iterations.md content.
//
// Inputs:
//   - content: current iterations.md bytes (or "# Iterations\n" for
//     a fresh file)
//   - iterN: iteration number to write
//   - title, date: heading components
//   - frontmatterBlock: the YAML front-matter the writer would emit
//     (empty for legacy shape)
//   - narrative: the narrative body (trailing-whitespace-trimmed)
//   - trailer: the provenance trailer (already constructed by the
//     handler so the function stays I/O-free)
//   - summary, shape: passed to the revision-block front-matter
//     builder when the divergent case is hit
//   - now: timestamp source for `revised_at` (test seam)
func resolveAppendIteration(
	content string,
	iterN int,
	title, date string,
	frontmatterBlock, narrative, trailer string,
	summary, shape string,
	now time.Time,
) appendIterationOutcome {
	heading := iterationHeading(iterN, title, date)
	incomingBody := canonicalIterationBody(frontmatterBlock, narrative)

	span := findIterationBlock(content, iterN)
	if !span.originalExists {
		// Case 1: heading absent → normal append.
		block := buildIterationBlockBytes(heading, frontmatterBlock+strings.TrimRight(narrative, "\n"), trailer)
		out := content
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		out += block
		return appendIterationOutcome{
			Action:     "appended",
			Iteration:  iterN,
			Revision:   0,
			Heading:    heading,
			NewContent: out,
		}
	}

	// Heading present — extract existing body for comparison.
	existingBody := extractIterationBody(content, span)
	if canonicalEqual(existingBody, incomingBody) {
		// Case 2: byte-identical (after normalization) → idempotent
		// no-op. NewContent left empty so the handler skips the
		// disk write.
		return appendIterationOutcome{
			Action:       "idempotent",
			Iteration:    iterN,
			Revision:     0,
			Heading:      heading,
			IsIdempotent: true,
		}
	}

	// Case 3: heading present, body differs → append revision block.
	nextRev := span.highestRev + 1
	if nextRev < 2 {
		nextRev = 2
	}
	revHeading := revisionHeading(iterN, nextRev, title, date)
	revFM := buildRevisionFrontmatterBlock(iterN, now, summary, shape)
	revBody := revFM + strings.TrimRight(narrative, "\n")
	revBlock := buildIterationBlockBytes(revHeading, revBody, trailer)
	out := content
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += revBlock
	return appendIterationOutcome{
		Action:     "revised",
		Iteration:  iterN,
		Revision:   nextRev,
		Heading:    revHeading,
		NewContent: out,
	}
}

// nowFunc is the now() seam. Tests can override it (see
// `withFixedNow` in tools_iterations_idempotent_test.go) to produce
// deterministic `revised_at` timestamps in revision-block
// front-matter. Production callers always go through `time.Now()`.
var nowFunc = func() time.Time { return time.Now() }
