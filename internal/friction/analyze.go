// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package friction

import (
	"fmt"

	"github.com/johns/vibe-vault/internal/mdutil"
	"github.com/johns/vibe-vault/internal/narrative"
	"github.com/johns/vibe-vault/internal/prose"
	"github.com/johns/vibe-vault/internal/transcript"
)

// Analysis thresholds for signal detection and summary generation.
const (
	retryThreshold         = 3      // file modifications before counting as retry
	summaryTokensPerFile   = 20000  // tokens/file threshold for summary mention
	summaryFileRetryPct    = 0.2    // file retry density threshold for summary mention
	summaryErrorCyclePct   = 0.1    // error cycle density threshold for summary mention
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
		DurationMinutes:   int(stats.Duration.Minutes()),
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
			if count >= retryThreshold {
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

// hasRecurringThreads checks if any prior threads appear again using Jaccard similarity.
func hasRecurringThreads(prior, current []string) bool {
	for _, p := range prior {
		pWords := mdutil.SignificantWords(p)
		for _, c := range current {
			cWords := mdutil.SignificantWords(c)
			if jaccardSimilarity(pWords, cWords) >= 0.5 {
				return true
			}
		}
	}
	return false
}

// jaccardSimilarity computes |intersection| / |union| for two word sets.
func jaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	setA := make(map[string]bool, len(a))
	for _, w := range a {
		setA[w] = true
	}
	setB := make(map[string]bool, len(b))
	for _, w := range b {
		setB[w] = true
	}

	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// buildSummary generates human-readable signal descriptions.
func buildSummary(s Signals, corrections, userTurns int) []string {
	var lines []string

	if corrections > 0 {
		lines = append(lines, fmt.Sprintf("%d corrections in %d user turns (%.0f%% density)",
			corrections, userTurns, s.CorrectionDensity*100))
	}

	if s.TokensPerFile > summaryTokensPerFile {
		lines = append(lines, fmt.Sprintf("%.0fK tokens/file changed", s.TokensPerFile/1000))
	}

	if s.FileRetryDensity > summaryFileRetryPct {
		lines = append(lines, fmt.Sprintf("%.0f%% of files required %d+ edits", s.FileRetryDensity*100, retryThreshold))
	}

	if s.ErrorCycleDensity > summaryErrorCyclePct {
		lines = append(lines, fmt.Sprintf("%.0f%% of activities were unresolved errors", s.ErrorCycleDensity*100))
	}

	if s.RecurringThreads {
		lines = append(lines, "Open threads recurring from prior session")
	}

	return lines
}
