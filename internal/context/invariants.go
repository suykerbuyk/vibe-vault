// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"regexp"
	"strings"
)

// CurrentStateSection is the canonical heading name for the section gated by
// the v10 Current State contract. Exported so MCP handlers and future lints
// can reference it without re-hardcoding the string.
const CurrentStateSection = "Current State"

// invariantKeyRe matches a bolded key-value bullet at the start of a line,
// capturing the key (between `**...**`, before `:`) and the trailing content
// (after `:**`).
//
// Spec divergence: the feature plan specifies `[A-Z][a-z][\w\s]*` for the
// key, but that pattern rejects all-caps acronym keys like `MCP` and `CLI`
// which are both on the whitelist below. The key class is widened to
// `[A-Z][\w ]*?` so acronyms pass.
var invariantKeyRe = regexp.MustCompile(`^\s*-?\s*\*\*([A-Z][\w ]*?):\*\*(.*)$`)

// invariantFirstWords is the set of acceptable first words for an invariant
// bullet key under the v10 Current State contract. Adding entries here is a
// contract change; do so only with care.
var invariantFirstWords = map[string]struct{}{
	"Iterations":   {},
	"Tests":        {},
	"Lint":         {},
	"Schema":       {},
	"Module":       {},
	"MCP":          {},
	"Embedded":     {},
	"Stack":        {},
	"Binary":       {},
	"Config":       {},
	"Bootstrap":    {},
	"Build":        {},
	"CLI":          {},
	"License":      {},
	"Git":          {},
	"Coverage":     {},
	"Distribution": {},
	"External":     {},
}

// invariantMaxTrailing caps the rune-length of the content that follows
// `:**`. Anything longer is narrative-prone and belongs in features.md.
const invariantMaxTrailing = 200

// IsInvariantBullet reports whether a single line qualifies as an invariant
// bullet under the v10 Current State contract. A qualifying line:
//   - starts with optional list marker, then `**Key:**`;
//   - Key's first word is in the whitelist (Iterations, Tests, Lint, ...);
//   - trailing content after `:**` is ≤invariantMaxTrailing runes.
//
// Continuation lines (no `**Key:**` prefix) return false. Narrative-prone
// keys (Phase, Status, Workflow, Recent, New, Latest) are rejected by
// virtue of not being on the whitelist.
func IsInvariantBullet(line string) bool {
	m := invariantKeyRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	key := strings.TrimSpace(m[1])
	if key == "" {
		return false
	}
	firstWord := key
	if i := strings.IndexByte(key, ' '); i >= 0 {
		firstWord = key[:i]
	}
	if _, ok := invariantFirstWords[firstWord]; !ok {
		return false
	}
	trailing := strings.TrimSpace(m[2])
	return len([]rune(trailing)) <= invariantMaxTrailing
}

// italicPointerRe matches a line whose entire content is a single-asterisk
// italic span, e.g., `*See agentctx/features.md for shipped-capability
// index.*`. Such lines are emitted by `/wrap` and `/features-split` as
// section footers — not bullets. The validator skips them.
var italicPointerRe = regexp.MustCompile(`^\*[^*].*[^*]\*$`)

// ValidateCurrentStateBody scans a Current-State section body line-by-line
// against the v10 contract. It skips blank lines, markdown headings,
// HTML-comment regions (single-line or multi-line), and italic pointer
// lines; every other line must satisfy IsInvariantBullet. Returns the first
// failing line and false, or ("", true) if all candidate lines pass.
//
// Multi-line HTML comments are handled via an inComment state flag — a line
// containing `<!--` enters the region, a line containing `-->` exits it.
// Lines inside the region are skipped regardless of content. An unclosed
// comment silently skips all subsequent lines; document-level malformation
// is not the validator's concern.
//
// Italic pointer lines (`*...*` wrapping the whole line) are emitted by
// `/wrap` and `/features-split` as section footers (e.g., `*See
// agentctx/features.md for shipped-capability index.*`) — they carry no
// invariant state and are skipped.
//
// Continuation / wrapped-bullet lines (no `**Key:**` prefix) are rejected by
// IsInvariantBullet: the v10 contract requires each bullet to fit on a
// single line (≤200 runes trailing); wrapping is forbidden.
func ValidateCurrentStateBody(body string) (badLine string, ok bool) {
	inComment := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if inComment {
			if strings.Contains(line, "-->") {
				inComment = false
			}
			continue
		}
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "<!--") {
			if !strings.Contains(line, "-->") {
				inComment = true
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if italicPointerRe.MatchString(line) {
			continue
		}
		if !IsInvariantBullet(raw) {
			return raw, false
		}
	}
	return "", true
}
