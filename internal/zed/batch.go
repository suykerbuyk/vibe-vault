// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package zed

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/lockfile"
	"github.com/suykerbuyk/vibe-vault/internal/session"
	"github.com/suykerbuyk/vibe-vault/internal/sessionsource"
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

// BatchCapture processes a batch of Zed threads through the capture
// pipeline. It is the back-compat entry point used by `vv zed
// backfill` and the watcher loop in `vv zed watch`. Internally it
// constructs a CaptureSink and delegates to batchCaptureViaSink, so
// production paths share one code path with the SessionSource Sink.
func BatchCapture(opts BatchCaptureOpts) BatchResult {
	return batchCaptureViaSink(context.Background(), sessionsource.CaptureSink{}, opts)
}

// batchCaptureViaSink is the workhorse: it loads the index, parses
// each thread into the canonical (transcript, info, narrative,
// dialogue) tuple, then calls the supplied Sink with all four
// pre-parsed inputs set. session.Capture's routing fork detects
// the four-field set and runs CaptureFromParsed — preserving the
// existing pipeline byte-identically.
//
// The sink seam is what makes the SessionSource interface
// cohesive: both `vv zed watch` (registry-driven) and `vv zed
// backfill` (registry-bypassing) land in the same Sink call so
// the source-string plumbing is uniform regardless of entry point.
func batchCaptureViaSink(ctx context.Context, sink sessionsource.Sink, opts BatchCaptureOpts) BatchResult {
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

		// γ Phase 1: pass all four pre-parsed inputs so
		// session.Capture's routing fork takes the
		// CaptureFromParsed fast-path (byte-identical to the
		// pre-γ direct call). Source plumbed as
		// SourceCaptureSourceTag ("zed-acp") — registry name and
		// note frontmatter `source` field now align.
		captureOpts := session.CaptureOpts{
			TranscriptPath: fmt.Sprintf("zed:%s#%s", opts.DBPath, thread.ID),
			Source:         SourceCaptureSourceTag,
			Force:          opts.Force,
			AutoCaptured:   opts.AutoCaptured,
			SkipEnrichment: true,
			Index:          idx,
			StagingRoot:    stagingRoot,
			Transcript:     t,
			Info:           &info,
			Narrative:      narr,
			Dialogue:       dialogue,
		}

		captureResult, err := sink.Capture(ctx, captureOpts, opts.Cfg)
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
