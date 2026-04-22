package synthesis

import (
	"context"
	"fmt"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/llm"
)

// RunOpts configures a synthesis run.
type RunOpts struct {
	NotePath string
	CWD      string
	Project  string
	Provider llm.Provider
	Index    *index.Index
}

// Run is the top-level orchestrator. Gathers context, calls LLM, applies actions.
// Returns (nil, nil) if provider is nil or synthesis is disabled.
func Run(ctx context.Context, opts RunOpts, cfg config.Config) (*ActionReport, error) {
	if opts.Provider == nil {
		return nil, nil
	}
	if !cfg.Synthesis.Enabled {
		return nil, nil
	}

	input, err := GatherInput(opts.NotePath, opts.CWD, cfg, opts.Index)
	if err != nil {
		return nil, fmt.Errorf("gather input: %w", err)
	}

	result, err := Synthesize(ctx, opts.Provider, input)
	if err != nil {
		return nil, fmt.Errorf("synthesize: %w", err)
	}
	if result == nil {
		return &ActionReport{}, nil
	}

	// Check if result is entirely empty
	if len(result.Learnings) == 0 && len(result.StaleEntries) == 0 &&
		result.ResumeUpdate == nil && len(result.TaskUpdates) == 0 {
		return &ActionReport{}, nil
	}

	report, err := Apply(result, opts.Project, cfg)
	if err != nil {
		return nil, fmt.Errorf("apply: %w", err)
	}

	return report, nil
}
