package friction

import "math"

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
