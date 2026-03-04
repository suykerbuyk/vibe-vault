// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package friction

import (
	"math"
	"sort"
)

// Signal weights and thresholds for friction scoring.
const (
	weightCorrectionDensity = 30
	weightTokenEfficiency   = 25
	weightFileRetryDensity  = 20
	weightErrorCycleDensity = 15
	weightRecurringThreads  = 10

	thresholdCorrectionDensity = 0.30
	thresholdTokensPerFile     = 50000.0
	thresholdFileRetryDensity  = 0.50
	thresholdErrorCycleDensity = 0.20
)

// Score computes the composite friction score from signals.
// Returns 0-100 where higher means more friction.
func Score(s Signals) int {
	// Normalize each signal to [0, 1]
	corrNorm := clamp(s.CorrectionDensity / thresholdCorrectionDensity)
	tokenNorm := clamp(s.TokensPerFile / thresholdTokensPerFile)
	retryNorm := clamp(s.FileRetryDensity / thresholdFileRetryDensity)
	errorNorm := clamp(s.ErrorCycleDensity / thresholdErrorCycleDensity)
	threadNorm := 0.0
	if s.RecurringThreads {
		threadNorm = 1.0
	}

	// Weighted sum
	raw := corrNorm*float64(weightCorrectionDensity) +
		tokenNorm*float64(weightTokenEfficiency) +
		retryNorm*float64(weightFileRetryDensity) +
		errorNorm*float64(weightErrorCycleDensity) +
		threadNorm*float64(weightRecurringThreads)

	score := int(math.Round(raw))
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	return score
}

// SignalContribution represents a single signal's weighted contribution to the friction score.
type SignalContribution struct {
	Name   string
	Weight float64 // weighted contribution (normalized × weight)
}

// TopContributors computes each signal's weighted contribution and returns the top N.
func TopContributors(s Signals, n int) []SignalContribution {
	corrNorm := clamp(s.CorrectionDensity / thresholdCorrectionDensity)
	tokenNorm := clamp(s.TokensPerFile / thresholdTokensPerFile)
	retryNorm := clamp(s.FileRetryDensity / thresholdFileRetryDensity)
	errorNorm := clamp(s.ErrorCycleDensity / thresholdErrorCycleDensity)
	threadNorm := 0.0
	if s.RecurringThreads {
		threadNorm = 1.0
	}

	contribs := []SignalContribution{
		{"corrections", math.Round(corrNorm * float64(weightCorrectionDensity))},
		{"tokens/file", math.Round(tokenNorm * float64(weightTokenEfficiency))},
		{"file retries", math.Round(retryNorm * float64(weightFileRetryDensity))},
		{"error cycles", math.Round(errorNorm * float64(weightErrorCycleDensity))},
		{"recurring threads", math.Round(threadNorm * float64(weightRecurringThreads))},
	}

	// Filter zero contributions
	var nonzero []SignalContribution
	for _, c := range contribs {
		if c.Weight > 0 {
			nonzero = append(nonzero, c)
		}
	}

	sort.Slice(nonzero, func(i, j int) bool {
		return nonzero[i].Weight > nonzero[j].Weight
	})

	if n > len(nonzero) {
		n = len(nonzero)
	}
	return nonzero[:n]
}

// clamp limits a value to [0, 1].
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
