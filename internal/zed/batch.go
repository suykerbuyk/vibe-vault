// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/lockfile"
	"github.com/suykerbuyk/vibe-vault/internal/session"
	"github.com/suykerbuyk/vibe-vault/internal/staging"
)

// BatchCaptureOpts configures a batch capture of Zed threads.
type BatchCaptureOpts struct {
	Threads       []Thread
	DBPath        string
	ProjectFilter string
	Force         bool
	AutoCaptured  bool
	Cfg           config.Config
	Logger        *log.Logger
}

// BatchResult holds the outcome of a batch capture operation.
type BatchResult struct {
	Processed int
	Skipped   int
	Errors    int
}

// BatchCapture processes a batch of Zed threads through the capture pipeline.
func BatchCapture(opts BatchCaptureOpts) BatchResult {
	logger := opts.Logger
	if logger == nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	var result BatchResult

	stateDir := opts.Cfg.StateDir()
	if mkdirErr := os.MkdirAll(stateDir, 0o755); mkdirErr != nil {
		logger.Printf("warning: could not create state dir: %v", mkdirErr)
	}
	indexLockPath := filepath.Join(stateDir, "session-index.json") + ".lock"
	fl, lockErr := lockfile.Acquire(indexLockPath)
	if lockErr != nil {
		logger.Printf("warning: could not acquire index lock: %v", lockErr)
	}
	defer func() {
		if fl != nil {
			_ = fl.Release()
		}
	}()

	idx, err := index.Load(opts.Cfg.StateDir())
	if err != nil {
		logger.Printf("error loading index: %v", err)
		result.Errors = len(opts.Threads)
		return result
	}

	// Phase 2 of vault-two-tier-narrative-vs-sessions-split: resolve
	// the staging root once per batch so every thread in this run uses
	// the same routing target. Empty string preserves legacy flat-vault
	// behavior on resolution failure.
	stagingRoot := staging.ResolveRoot(opts.Cfg.Staging.Root)

	for _, thread := range opts.Threads {
		info := DetectProject(&thread, opts.Cfg)

		if opts.ProjectFilter != "" && info.Project != opts.ProjectFilter {
			result.Skipped++
			continue
		}

		t, err := Convert(&thread)
		if err != nil {
			logger.Printf("error converting thread %s: %v", thread.ID, err)
			result.Errors++
			continue
		}
		if t == nil {
			result.Skipped++
			continue
		}

		narr := ExtractNarrative(&thread)
		dialogue := ExtractDialogue(&thread)

		captureOpts := session.CaptureOpts{
			TranscriptPath: fmt.Sprintf("zed:%s#%s", opts.DBPath, thread.ID),
			Source:         "zed",
			Force:          opts.Force,
			AutoCaptured:   opts.AutoCaptured,
			SkipEnrichment: true,
			Index:          idx,
			StagingRoot:    stagingRoot,
		}

		captureResult, err := session.CaptureFromParsed(t, info, narr, dialogue, captureOpts, opts.Cfg)
		if err != nil {
			logger.Printf("error processing thread %s: %v", thread.ID, err)
			result.Errors++
			continue
		}

		if captureResult.Skipped {
			result.Skipped++
			continue
		}

		result.Processed++
		logger.Printf("  %s → %s", captureResult.Project, captureResult.NotePath)
	}

	if err := idx.Save(); err != nil {
		logger.Printf("warning: could not save index: %v", err)
	}

	return result
}
