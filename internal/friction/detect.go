package friction

import (
	"strings"

	"github.com/johns/vibe-vault/internal/prose"
)

// DetectCorrections scans prose dialogue for user correction patterns.
func DetectCorrections(dialogue *prose.Dialogue) []Correction {
	if dialogue == nil {
		return nil
	}

	var corrections []Correction
	turnIndex := 0

	for _, sec := range dialogue.Sections {
		var prevAssistantLong bool
		for _, elem := range sec.Elements {
			if elem.Turn == nil {
				continue
			}

			if elem.Turn.Role == "assistant" {
				prevAssistantLong = len(elem.Turn.Text) > 200
				continue
			}

			// User turn
			text := elem.Turn.Text
			lower := strings.ToLower(text)

			// Tier 1: linguistic patterns
			if pattern := matchCorrectionPattern(lower); pattern != "" {
				corrections = append(corrections, Correction{
					TurnIndex: turnIndex,
					Text:      truncate(text, 120),
					Pattern:   pattern,
				})
			} else if prevAssistantLong && len(text) < 100 && matchShortNegation(lower) {
				// Tier 2: short negation after long assistant turn
				corrections = append(corrections, Correction{
					TurnIndex: turnIndex,
					Text:      truncate(text, 120),
					Pattern:   "short-negation",
				})
			}

			turnIndex++
			prevAssistantLong = false
		}
	}

	return corrections
}

// matchCorrectionPattern returns the pattern name if text matches a correction pattern.
func matchCorrectionPattern(lower string) string {
	// Negation patterns
	negations := []string{
		"no, ", "no that's", "not what i", "i said ", "i meant ",
		"that's not", "no i ",
	}
	for _, p := range negations {
		if strings.HasPrefix(lower, p) || strings.Contains(lower, p) {
			return "negation"
		}
	}

	// Redirect patterns
	redirects := []string{
		"actually,", "actually ", "wait,", "wait ", "stop", "instead,", "instead ", "rather,", "rather ",
	}
	for _, p := range redirects {
		if strings.HasPrefix(lower, p) {
			return "redirect"
		}
	}

	// Undo patterns
	undos := []string{
		"revert", "undo", "roll back", "go back",
	}
	for _, p := range undos {
		if strings.Contains(lower, p) {
			return "undo"
		}
	}

	// Quality complaints
	complaints := []string{
		"that's wrong", "doesn't work", "still broken", "still failing",
		"that's broken", "doesn't compile", "still not",
	}
	for _, p := range complaints {
		if strings.Contains(lower, p) {
			return "quality"
		}
	}

	// Repetition patterns
	repetitions := []string{
		"i already", "as i said", "like i said",
	}
	for _, p := range repetitions {
		if strings.Contains(lower, p) {
			return "repetition"
		}
	}

	return ""
}

// matchShortNegation checks for negation at the start of a short message.
func matchShortNegation(lower string) bool {
	starters := []string{"no", "wrong", "nope", "that's wrong", "not right"}
	for _, s := range starters {
		if strings.HasPrefix(lower, s) {
			return true
		}
	}
	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
