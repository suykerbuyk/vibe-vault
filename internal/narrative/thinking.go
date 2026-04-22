// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package narrative

import (
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/transcript"
)

// reasoningKeywords are phrases that indicate interesting reasoning.
var reasoningKeywords = []string{
	"alternatively",
	"trade-off",
	"tradeoff",
	"could also",
	"not sure",
	"risky",
	"considered",
	"downside",
	"instead of",
	"chose",
	"decided",
	"better approach",
	"on the other hand",
}

// ExtractReasoningHighlights scans thinking blocks for reasoning keywords
// and returns 0-5 bullet points with surrounding context.
func ExtractReasoningHighlights(t *transcript.Transcript) []string {
	if t == nil {
		return nil
	}

	var highlights []string
	seen := make(map[string]bool)

	for _, e := range t.Entries {
		if e.Message == nil || e.Message.Role != "assistant" {
			continue
		}

		thinking := transcript.ThinkingContent(e.Message)
		if thinking == "" {
			continue
		}

		sentences := splitSentences(thinking)
		for _, sentence := range sentences {
			lower := strings.ToLower(sentence)
			for _, kw := range reasoningKeywords {
				if strings.Contains(lower, kw) {
					// Deduplicate similar highlights
					key := strings.ToLower(strings.TrimSpace(sentence))
					if len(key) > 40 {
						key = key[:40]
					}
					if seen[key] {
						break
					}
					seen[key] = true

					highlight := strings.TrimSpace(sentence)
					if len(highlight) > 200 {
						highlight = highlight[:197] + "..."
					}
					highlights = append(highlights, highlight)
					if len(highlights) >= 5 {
						return highlights
					}
					break
				}
			}
		}
	}

	return highlights
}

// splitSentences splits text into rough sentence boundaries.
func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	for _, r := range text {
		current.WriteRune(r)
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			s := strings.TrimSpace(current.String())
			if len(s) > 10 { // skip very short fragments
				sentences = append(sentences, s)
			}
			current.Reset()
		}
	}

	// Don't forget the last fragment
	if s := strings.TrimSpace(current.String()); len(s) > 10 {
		sentences = append(sentences, s)
	}

	return sentences
}
