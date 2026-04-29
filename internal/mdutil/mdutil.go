// Package mdutil provides shared markdown and text utilities used across
// vibe-vault's analysis and synthesis packages.
package mdutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MinWordLen is the minimum character length for a word to be considered significant.
const MinWordLen = 4

// SignificantWords extracts words ≥ MinWordLen chars, lowercased, with edge
// punctuation stripped, excluding common stop words.
func SignificantWords(s string) []string {
	words := strings.Fields(strings.ToLower(s))
	var result []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'`()[]{}—-")
		if len(w) >= MinWordLen && !stopWords[w] {
			result = append(result, w)
		}
	}
	return result
}

// IsStopWord reports whether w is in the common stop-word list.
func IsStopWord(w string) bool {
	return stopWords[w]
}

// Overlap returns the number of shared elements between a and b.
// Duplicates in b do not inflate the count (deduplicating semantics).
func Overlap(a, b []string) int {
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	count := 0
	for _, s := range b {
		if set[s] {
			count++
			delete(set, s)
		}
	}
	return count
}

// SetIntersection returns elements present in both slices, deduplicated.
func SetIntersection(a, b []string) []string {
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	var result []string
	for _, s := range b {
		if set[s] {
			result = append(result, s)
			delete(set, s)
		}
	}
	return result
}

// ReplaceSectionBody finds a ## heading in doc and replaces its body up to
// the next ## heading or EOF. Returns an error if the heading is not found.
func ReplaceSectionBody(doc, heading, newBody string) (string, error) {
	lines := strings.Split(doc, "\n")
	target := "## " + heading
	startIdx := -1

	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		return "", fmt.Errorf("section %q not found", heading)
	}

	endIdx := len(lines)
	for i := startIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			endIdx = i
			break
		}
	}

	var result []string
	result = append(result, lines[:startIdx+1]...)
	result = append(result, "")
	body := strings.TrimRight(newBody, "\n")
	result = append(result, body)
	result = append(result, "")
	result = append(result, lines[endIdx:]...)

	return strings.Join(result, "\n"), nil
}

// NormalizeSubheadingSlug extracts the slug from a ### heading text by
// truncating at the first " — " (space–em-dash–space) occurrence or
// returning the full text when that separator is absent.
//
// The input should be the text after the "### " prefix.
func NormalizeSubheadingSlug(headingText string) string {
	const sep = " — " // " — "
	if idx := strings.Index(headingText, sep); idx >= 0 {
		return headingText[:idx]
	}
	return headingText
}

// subheadingSlugs returns all ### sub-heading slugs found within the body of
// a ## parent section (from parentStart+1 up to parentEnd, exclusive).
func subheadingSlugs(lines []string, parentStart, parentEnd int) []string {
	var slugs []string
	for i := parentStart + 1; i < parentEnd; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "### ") {
			slugs = append(slugs, NormalizeSubheadingSlug(strings.TrimPrefix(line, "### ")))
		}
	}
	return slugs
}

// findParentSection locates the start and end line indices (end is exclusive)
// for a ## heading in lines. Returns (-1, -1) when not found.
func findParentSection(lines []string, parentHeading string) (start, end int) {
	target := "## " + parentHeading
	start = -1
	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			start = i
			break
		}
	}
	if start == -1 {
		return -1, -1
	}
	end = len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	return start, end
}

// htmlCommentLineRE matches a line that is purely an HTML comment.
var htmlCommentLineRE = func() func(string) bool {
	prefix := "<!--"
	suffix := "-->"
	return func(line string) bool {
		t := strings.TrimSpace(line)
		return strings.HasPrefix(t, prefix) && strings.HasSuffix(t, suffix)
	}
}()

// ReplaceSubsectionBody replaces the body of a ### sub-heading inside a ##
// parent section. The sub-heading is matched by its normalized slug (text up
// to the first " — " or end-of-line).
//
// Rules:
//   - Zero matches → error listing all available slugs.
//   - One match → replace silently; return (modifiedDoc, nil).
//   - Multiple matches → hard error (Direction-C D9: ambiguity is a
//     pre-write structural failure, not a post-hoc warning). The returned
//     error lists the candidate slugs to help the operator disambiguate.
//
// The newBody must NOT include the ### heading line. The function re-emits the
// original heading line verbatim.
//
// HTML-comment-only lines inside the replaced body cause a hard error because
// the tool cannot safely preserve them.
func ReplaceSubsectionBody(doc, parentHeading, subHeading, newBody string) (string, error) {
	lines := strings.Split(doc, "\n")

	parentStart, parentEnd := findParentSection(lines, parentHeading)
	if parentStart == -1 {
		return "", fmt.Errorf("parent section %q not found", parentHeading)
	}

	// Collect all sub-heading positions that match the slug.
	type match struct{ headLineIdx, bodyEnd int }
	var matches []match

	i := parentStart + 1
	for i < parentEnd {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "### ") {
			slug := NormalizeSubheadingSlug(strings.TrimPrefix(line, "### "))
			if slug == subHeading {
				// Find body end: next ### within the parent or the parent end.
				bodyEnd := parentEnd
				for j := i + 1; j < parentEnd; j++ {
					if strings.HasPrefix(strings.TrimSpace(lines[j]), "### ") {
						bodyEnd = j
						break
					}
				}
				matches = append(matches, match{i, bodyEnd})
			}
		}
		i++
	}

	if len(matches) == 0 {
		available := subheadingSlugs(lines, parentStart, parentEnd)
		return "", fmt.Errorf("slug %q not found in %s; available: %v", subHeading, parentHeading, available)
	}
	if len(matches) > 1 {
		// Direction-C D9: ambiguity is a pre-write structural failure.
		var candidateSlugs []string
		for _, mm := range matches {
			candidateSlugs = append(candidateSlugs, NormalizeSubheadingSlug(
				strings.TrimPrefix(strings.TrimSpace(lines[mm.headLineIdx]), "### ")))
		}
		return "", fmt.Errorf("slug %q ambiguous in %s: %d matches (candidates: %s); resolve duplicates before retrying",
			subHeading, parentHeading, len(matches), strings.Join(candidateSlugs, ","))
	}

	m := matches[0]

	// Reject if the existing body contains HTML-comment-only lines.
	for k := m.headLineIdx + 1; k < m.bodyEnd; k++ {
		if htmlCommentLineRE(lines[k]) {
			return "", fmt.Errorf("marker preservation not implemented inside replaced subsection body; manual edit required")
		}
	}

	// Build the replacement.
	var result []string
	result = append(result, lines[:m.headLineIdx+1]...) // up to and including ### heading
	result = append(result, "")
	body := strings.TrimRight(newBody, "\n")
	result = append(result, body)
	result = append(result, "")
	result = append(result, lines[m.bodyEnd:]...)

	return strings.Join(result, "\n"), nil
}

// InsertPosition describes where inside a ## parent section a new ### block
// should be inserted.
type InsertPosition struct {
	// Mode is one of "top", "bottom", "after", "before".
	Mode string
	// AnchorSlug is the normalized slug of the adjacent ### heading.
	// Required when Mode is "after" or "before".
	AnchorSlug string
}

// InsertSubsection inserts a new ### sub-heading block at the requested
// position inside a ## parent section. The caller supplies the body WITHOUT
// the heading line; the function emits "### subHeading\n\nbody\n".
//
// slug must NOT already exist inside the parent — hard error if it does.
func InsertSubsection(doc, parentHeading string, pos InsertPosition, subHeading, body string) (string, error) {
	lines := strings.Split(doc, "\n")

	parentStart, parentEnd := findParentSection(lines, parentHeading)
	if parentStart == -1 {
		return "", fmt.Errorf("parent section %q not found", parentHeading)
	}

	// Reject if slug already exists.
	for _, s := range subheadingSlugs(lines, parentStart, parentEnd) {
		if s == NormalizeSubheadingSlug(subHeading) {
			return "", fmt.Errorf("slug %q already exists in %s", subHeading, parentHeading)
		}
	}

	block := "### " + subHeading + "\n\n" + strings.TrimRight(body, "\n") + "\n"

	var insertIdx int // lines will be inserted BEFORE this index
	switch pos.Mode {
	case "top":
		// Right after the parent heading line; skip a blank line if present.
		insertIdx = parentStart + 1
		if insertIdx < parentEnd && strings.TrimSpace(lines[insertIdx]) == "" {
			insertIdx++
		}

	case "bottom":
		// Right before the next ## or end-of-doc; back up past trailing blanks.
		insertIdx = parentEnd
		for insertIdx > parentStart+1 && strings.TrimSpace(lines[insertIdx-1]) == "" {
			insertIdx--
		}

	case "after", "before":
		if pos.AnchorSlug == "" {
			return "", fmt.Errorf("anchor_slug required for position mode %q", pos.Mode)
		}
		// Find the anchor sub-heading.
		anchorIdx := -1
		anchorBodyEnd := -1
		for j := parentStart + 1; j < parentEnd; j++ {
			line := strings.TrimSpace(lines[j])
			if strings.HasPrefix(line, "### ") {
				slug := NormalizeSubheadingSlug(strings.TrimPrefix(line, "### "))
				if slug == pos.AnchorSlug {
					anchorIdx = j
					anchorBodyEnd = parentEnd
					for k := j + 1; k < parentEnd; k++ {
						if strings.HasPrefix(strings.TrimSpace(lines[k]), "### ") {
							anchorBodyEnd = k
							break
						}
					}
					break
				}
			}
		}
		if anchorIdx == -1 {
			available := subheadingSlugs(lines, parentStart, parentEnd)
			return "", fmt.Errorf("anchor slug %q not found in %s; available: %v", pos.AnchorSlug, parentHeading, available)
		}
		if pos.Mode == "after" {
			// Insert after anchor body end (back up past trailing blanks).
			insertIdx = anchorBodyEnd
			for insertIdx > anchorIdx+1 && strings.TrimSpace(lines[insertIdx-1]) == "" {
				insertIdx--
			}
		} else { // before
			insertIdx = anchorIdx
		}

	default:
		return "", fmt.Errorf("unknown position mode %q; expected top, bottom, after, or before", pos.Mode)
	}

	// Splice: lines[:insertIdx] + block lines + lines[insertIdx:]
	blockLines := strings.Split(block, "\n")
	var result []string
	result = append(result, lines[:insertIdx]...)
	result = append(result, blockLines...)
	result = append(result, lines[insertIdx:]...)

	return strings.Join(result, "\n"), nil
}

// RemoveSubsection removes a ### sub-heading block (heading line + body) from
// inside a ## parent section. Matches by normalized slug.
//
//   - Zero matches → error.
//   - One match → remove silently.
//   - Multiple matches → hard error (Direction-C D9: ambiguity is a
//     pre-write structural failure, not a post-hoc warning). The returned
//     error lists the candidate slugs to help the operator disambiguate.
func RemoveSubsection(doc, parentHeading, subHeading string) (string, error) {
	lines := strings.Split(doc, "\n")

	parentStart, parentEnd := findParentSection(lines, parentHeading)
	if parentStart == -1 {
		return "", fmt.Errorf("parent section %q not found", parentHeading)
	}

	type match struct{ headLineIdx, bodyEnd int }
	var matches []match

	for i := parentStart + 1; i < parentEnd; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "### ") {
			slug := NormalizeSubheadingSlug(strings.TrimPrefix(line, "### "))
			if slug == subHeading {
				bodyEnd := parentEnd
				for j := i + 1; j < parentEnd; j++ {
					if strings.HasPrefix(strings.TrimSpace(lines[j]), "### ") {
						bodyEnd = j
						break
					}
				}
				matches = append(matches, match{i, bodyEnd})
			}
		}
	}

	if len(matches) == 0 {
		available := subheadingSlugs(lines, parentStart, parentEnd)
		return "", fmt.Errorf("slug %q not found in %s; available: %v", subHeading, parentHeading, available)
	}
	if len(matches) > 1 {
		// Direction-C D9: ambiguity is a pre-write structural failure.
		var candidateSlugs []string
		for _, mm := range matches {
			candidateSlugs = append(candidateSlugs, NormalizeSubheadingSlug(
				strings.TrimPrefix(strings.TrimSpace(lines[mm.headLineIdx]), "### ")))
		}
		return "", fmt.Errorf("slug %q ambiguous in %s: %d matches (candidates: %s); resolve duplicates before retrying",
			subHeading, parentHeading, len(matches), strings.Join(candidateSlugs, ","))
	}

	m := matches[0]

	// Remove: lines before the heading (trimming trailing blank lines before the heading)
	// + lines from bodyEnd onward.
	keepBefore := m.headLineIdx
	for keepBefore > parentStart+1 && strings.TrimSpace(lines[keepBefore-1]) == "" {
		keepBefore--
	}

	var result []string
	result = append(result, lines[:keepBefore]...)
	// Preserve a blank separator if we still have content after.
	if m.bodyEnd < len(lines) {
		result = append(result, "")
	}
	result = append(result, lines[m.bodyEnd:]...)

	return strings.Join(result, "\n"), nil
}

// AtomicWriteFile writes data to path via a temp file + rename for crash safety.
// Creates parent directories as needed.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".vv-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	tmpPath = ""
	return nil
}

// CountSubsectionMatches counts the number of ### sub-heading slugs that
// match the given normalized slug inside the named ## parent section. The
// slug comparison uses NormalizeSubheadingSlug on both sides so the
// caller's `slug` argument is treated identically to the renderer.
//
// Returns 0 when the parent section is missing.
func CountSubsectionMatches(doc, parentHeading, slug string) int {
	lines := strings.Split(doc, "\n")
	parentStart, parentEnd := findParentSection(lines, parentHeading)
	if parentStart == -1 {
		return 0
	}
	target := NormalizeSubheadingSlug(slug)
	count := 0
	for i := parentStart + 1; i < parentEnd; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "### ") {
			s := NormalizeSubheadingSlug(strings.TrimPrefix(line, "### "))
			if s == target {
				count++
			}
		}
	}
	return count
}

// CountAllSubsectionSlugs returns a map from normalized slug → count of
// occurrences for every ### sub-heading inside the named ## parent
// section. excludeSlug is treated as a normalized slug to skip (used by
// callers that own a specific slug via a separate API — e.g. the
// "Carried forward" sub-heading is not counted as a thread). Pass "" to
// count everything.
//
// Returns an empty map when the parent section is missing.
func CountAllSubsectionSlugs(doc, parentHeading, excludeSlug string) map[string]int {
	out := make(map[string]int)
	lines := strings.Split(doc, "\n")
	parentStart, parentEnd := findParentSection(lines, parentHeading)
	if parentStart == -1 {
		return out
	}
	excl := NormalizeSubheadingSlug(excludeSlug)
	for i := parentStart + 1; i < parentEnd; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "### ") {
			slug := NormalizeSubheadingSlug(strings.TrimPrefix(line, "### "))
			if excludeSlug != "" && slug == excl {
				continue
			}
			out[slug]++
		}
	}
	return out
}

// CountCarriedSlugsIn returns a map from lowercased carried-forward slug
// to occurrence count inside the named ### Carried forward sub-section
// of the given ## parent heading. Returns an empty map when either
// section is missing.
//
// The ported counterpart of internal/mcp/tools_quality_check.go's
// countCarriedSlugs (Direction-C D9: lifted into mdutil so the helper
// survives the Phase 4 retirement of tools_quality_check.go).
func CountCarriedSlugsIn(doc, parentHeading, carriedHeading string) map[string]int {
	out := make(map[string]int)
	lines := strings.Split(doc, "\n")
	parentStart, parentEnd := findParentSection(lines, parentHeading)
	if parentStart == -1 {
		return out
	}
	target := NormalizeSubheadingSlug(carriedHeading)
	cfStart := -1
	cfEnd := parentEnd
	for i := parentStart + 1; i < parentEnd; i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "### ") {
			slug := NormalizeSubheadingSlug(strings.TrimPrefix(line, "### "))
			if slug == target {
				cfStart = i
				for j := i + 1; j < parentEnd; j++ {
					if strings.HasPrefix(strings.TrimSpace(lines[j]), "### ") {
						cfEnd = j
						break
					}
				}
				break
			}
		}
	}
	if cfStart == -1 {
		return out
	}
	body := strings.Join(lines[cfStart+1:cfEnd], "\n")
	for _, b := range ParseCarriedForward(body) {
		out[strings.ToLower(b.Slug)]++
	}
	return out
}

var stopWords = map[string]bool{
	"that": true, "this": true, "with": true, "from": true,
	"have": true, "been": true, "were": true, "will": true,
	"would": true, "could": true, "should": true, "what": true,
	"when": true, "where": true, "which": true, "their": true,
	"there": true, "these": true, "those": true, "them": true,
	"then": true, "than": true, "some": true, "also": true,
	"into": true, "each": true, "make": true, "like": true,
	"just": true, "over": true, "such": true, "only": true,
	"very": true, "more": true, "most": true, "other": true,
	"about": true, "after": true, "before": true, "being": true,
	"between": true, "does": true, "doing": true, "done": true,
}
