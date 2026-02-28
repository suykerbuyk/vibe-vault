package friction

import (
	"fmt"
	"strings"

	"github.com/johns/vibe-vault/internal/narrative"
	"github.com/johns/vibe-vault/internal/prose"
	"github.com/johns/vibe-vault/internal/transcript"
)

// Analyze performs full friction analysis from prose dialogue, narrative, and stats.
// All parameters are optional (nil-safe).
func Analyze(
	dialogue *prose.Dialogue,
	narr *narrative.Narrative,
	stats transcript.Stats,
	priorThreads []string,
) *Result {
	result := &Result{}

	// Correction detection
	result.Corrections = DetectCorrections(dialogue)
	corrections := len(result.Corrections)

	// Count user turns
	userTurns := stats.UserMessages
	if userTurns == 0 {
		userTurns = 1 // avoid division by zero
	}

	// Count files changed
	filesChanged := len(stats.FilesWritten)
	if filesChanged == 0 {
		filesChanged = 1 // avoid division by zero
	}

	// Total tokens
	totalTokens := stats.InputTokens + stats.OutputTokens + stats.CacheReads + stats.CacheWrites

	// Build signals
	signals := Signals{
		Corrections:       corrections,
		CorrectionDensity: float64(corrections) / float64(userTurns),
		TokensPerFile:     float64(totalTokens) / float64(filesChanged),
	}

	// File retry density from narrative
	if narr != nil {
		fileMods := make(map[string]int)
		var totalActivities, unresolvedErrors int

		for _, seg := range narr.Segments {
			for _, a := range seg.Activities {
				totalActivities++
				switch a.Kind {
				case narrative.KindFileCreate, narrative.KindFileModify:
					fileMods[a.Description]++
				}
				if a.IsError && !a.Recovered {
					unresolvedErrors++
				}
			}
		}

		// File retry density: files with 3+ modifications / total unique files
		retryFiles := 0
		totalFiles := len(fileMods)
		for _, count := range fileMods {
			if count >= 3 {
				retryFiles++
			}
		}
		if totalFiles > 0 {
			signals.FileRetryDensity = float64(retryFiles) / float64(totalFiles)
		}

		// Error cycle density
		if totalActivities > 0 {
			signals.ErrorCycleDensity = float64(unresolvedErrors) / float64(totalActivities)
		}
	}

	// Recurring threads check
	if len(priorThreads) > 0 && narr != nil {
		signals.RecurringThreads = hasRecurringThreads(priorThreads, narr.OpenThreads)
	}

	result.Signals = signals
	result.Score = Score(signals)

	// Build human-readable summaries
	result.Summary = buildSummary(signals, corrections, userTurns)

	return result
}

// hasRecurringThreads checks if any prior threads appear again.
func hasRecurringThreads(prior, current []string) bool {
	for _, p := range prior {
		pWords := significantWords(p)
		for _, c := range current {
			cWords := significantWords(c)
			overlap := wordOverlap(pWords, cWords)
			if overlap >= 2 {
				return true
			}
		}
	}
	return false
}

// significantWords extracts lowercase words >= 4 chars.
func significantWords(s string) []string {
	words := strings.Fields(strings.ToLower(s))
	var result []string
	for _, w := range words {
		if len(w) >= 4 {
			result = append(result, w)
		}
	}
	return result
}

// wordOverlap counts shared words between two sets.
func wordOverlap(a, b []string) int {
	set := make(map[string]bool, len(a))
	for _, w := range a {
		set[w] = true
	}
	count := 0
	for _, w := range b {
		if set[w] {
			count++
		}
	}
	return count
}

// buildSummary generates human-readable signal descriptions.
func buildSummary(s Signals, corrections, userTurns int) []string {
	var lines []string

	if corrections > 0 {
		lines = append(lines, fmt.Sprintf("%d corrections in %d user turns (%.0f%% density)",
			corrections, userTurns, s.CorrectionDensity*100))
	}

	if s.TokensPerFile > 20000 {
		lines = append(lines, fmt.Sprintf("%.0fK tokens/file changed", s.TokensPerFile/1000))
	}

	if s.FileRetryDensity > 0.2 {
		lines = append(lines, fmt.Sprintf("%.0f%% of files required 3+ edits", s.FileRetryDensity*100))
	}

	if s.ErrorCycleDensity > 0.1 {
		lines = append(lines, fmt.Sprintf("%.0f%% of activities were unresolved errors", s.ErrorCycleDensity*100))
	}

	if s.RecurringThreads {
		lines = append(lines, "Open threads recurring from prior session")
	}

	return lines
}
