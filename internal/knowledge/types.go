// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package knowledge

// Note represents a single knowledge note to be written to the vault.
type Note struct {
	Type          string   // "lesson" or "decision"
	Title         string
	Summary       string   // One-line for frontmatter
	Body          string   // Actionable content (markdown)
	Project       string
	Date          string
	SourceSession string   // Wikilink target, e.g. "2026-02-28-01"
	Tags          []string
	Confidence    float64  // 0.0-1.0 from LLM
	Category      string   // e.g. "testing", "error-handling"
	NotePath      string   // Relative vault path, populated by ReadNotes (not used by WriteNote)
}

// CorrectionPair links a user correction to what followed.
type CorrectionPair struct {
	UserText   string // What the user said (the correction)
	Resolution string // What changed after (assistant response/actions)
	Pattern    string // From friction: negation, redirect, undo, quality
}

// ExtractInput holds everything needed for the LLM knowledge extraction call.
type ExtractInput struct {
	UserText      string
	AssistantText string
	Corrections   []CorrectionPair
	Decisions     []string // From narrative heuristics
	FrictionScore int
	Project       string
	FilesChanged  []string
}
