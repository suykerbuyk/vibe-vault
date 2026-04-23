// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package context

import (
	"regexp"
	"strings"
)

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
