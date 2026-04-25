// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package stats

import (
	"path/filepath"

	"github.com/suykerbuyk/vibe-vault/internal/config"
)

// CostInput holds the token counts needed for cost estimation.
type CostInput struct {
	Model        string
	InputTokens  int
	OutputTokens int
	CacheReads   int
	CacheWrites  int
}

// EstimateCost computes the estimated USD cost for a session based on pricing config.
// Returns 0 if pricing is disabled or no matching model pattern is found.
func EstimateCost(cfg config.PricingConfig, input CostInput) float64 {
	if !cfg.Enabled || len(cfg.Models) == 0 {
		return 0
	}

	pm := matchModel(cfg.Models, input.Model)
	if pm == nil {
		return 0
	}

	cost := float64(input.InputTokens) * pm.InputPerMillion / 1_000_000
	cost += float64(input.OutputTokens) * pm.OutputPerMillion / 1_000_000
	cost += float64(input.CacheReads) * pm.CacheReadPerMillion / 1_000_000
	cost += float64(input.CacheWrites) * pm.CacheWritePerMillion / 1_000_000

	return cost
}

// matchModel finds the first PricingModel whose pattern matches the model name.
func matchModel(models []config.PricingModel, name string) *config.PricingModel {
	for i := range models {
		matched, err := filepath.Match(models[i].Pattern, name)
		if err == nil && matched {
			return &models[i]
		}
	}
	return nil
}
