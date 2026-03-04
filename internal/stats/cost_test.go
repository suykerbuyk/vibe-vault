package stats

import (
	"math"
	"testing"

	"github.com/johns/vibe-vault/internal/config"
)

func TestEstimateCost_Disabled(t *testing.T) {
	cfg := config.PricingConfig{Enabled: false}
	cost := EstimateCost(cfg, CostInput{Model: "claude-opus-4-6", InputTokens: 100000})
	if cost != 0 {
		t.Errorf("disabled pricing should return 0, got %f", cost)
	}
}

func TestEstimateCost_NoModels(t *testing.T) {
	cfg := config.PricingConfig{Enabled: true}
	cost := EstimateCost(cfg, CostInput{Model: "claude-opus-4-6", InputTokens: 100000})
	if cost != 0 {
		t.Errorf("no models should return 0, got %f", cost)
	}
}

func TestEstimateCost_NoMatch(t *testing.T) {
	cfg := config.PricingConfig{
		Enabled: true,
		Models: []config.PricingModel{
			{Pattern: "gpt-*", InputPerMillion: 5.0, OutputPerMillion: 15.0},
		},
	}
	cost := EstimateCost(cfg, CostInput{Model: "claude-opus-4-6", InputTokens: 100000})
	if cost != 0 {
		t.Errorf("no match should return 0, got %f", cost)
	}
}

func TestEstimateCost_BasicComputation(t *testing.T) {
	cfg := config.PricingConfig{
		Enabled: true,
		Models: []config.PricingModel{
			{
				Pattern:          "claude-*",
				InputPerMillion:  3.00,
				OutputPerMillion: 15.00,
			},
		},
	}

	input := CostInput{
		Model:        "claude-opus-4-6",
		InputTokens:  1_000_000,
		OutputTokens: 100_000,
	}

	cost := EstimateCost(cfg, input)
	// 1M input * $3/M + 100K output * $15/M = $3.00 + $1.50 = $4.50
	expected := 4.50
	if math.Abs(cost-expected) > 0.001 {
		t.Errorf("cost = %f, want %f", cost, expected)
	}
}

func TestEstimateCost_WithCache(t *testing.T) {
	cfg := config.PricingConfig{
		Enabled: true,
		Models: []config.PricingModel{
			{
				Pattern:              "claude-*",
				InputPerMillion:      3.00,
				OutputPerMillion:     15.00,
				CacheReadPerMillion:  0.30,
				CacheWritePerMillion: 3.75,
			},
		},
	}

	input := CostInput{
		Model:        "claude-opus-4-6",
		InputTokens:  500_000,
		OutputTokens: 50_000,
		CacheReads:   2_000_000,
		CacheWrites:  500_000,
	}

	cost := EstimateCost(cfg, input)
	// 500K * $3/M + 50K * $15/M + 2M * $0.30/M + 500K * $3.75/M
	// = $1.50 + $0.75 + $0.60 + $1.875 = $4.725
	expected := 4.725
	if math.Abs(cost-expected) > 0.001 {
		t.Errorf("cost = %f, want %f", cost, expected)
	}
}

func TestEstimateCost_FirstMatchWins(t *testing.T) {
	cfg := config.PricingConfig{
		Enabled: true,
		Models: []config.PricingModel{
			{Pattern: "claude-opus-*", InputPerMillion: 15.00, OutputPerMillion: 75.00},
			{Pattern: "claude-*", InputPerMillion: 3.00, OutputPerMillion: 15.00},
		},
	}

	input := CostInput{
		Model:        "claude-opus-4-6",
		InputTokens:  1_000_000,
		OutputTokens: 100_000,
	}

	cost := EstimateCost(cfg, input)
	// Should match claude-opus-* first: 1M * $15/M + 100K * $75/M = $15 + $7.50
	expected := 22.50
	if math.Abs(cost-expected) > 0.001 {
		t.Errorf("cost = %f, want %f (first pattern should win)", cost, expected)
	}
}

func TestEstimateCost_ZeroTokens(t *testing.T) {
	cfg := config.PricingConfig{
		Enabled: true,
		Models: []config.PricingModel{
			{Pattern: "claude-*", InputPerMillion: 3.00, OutputPerMillion: 15.00},
		},
	}

	cost := EstimateCost(cfg, CostInput{Model: "claude-opus-4-6"})
	if cost != 0 {
		t.Errorf("zero tokens should give zero cost, got %f", cost)
	}
}
