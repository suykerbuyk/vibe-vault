// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"path/filepath"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/llm"
	"github.com/suykerbuyk/vibe-vault/internal/session"
	"github.com/suykerbuyk/vibe-vault/internal/sessionclaim"
	"github.com/suykerbuyk/vibe-vault/internal/synthesis"
)

// Input is the JSON object Claude Code sends to hooks via stdin.
type Input struct {
	SessionID            string `json:"session_id"`
	TranscriptPath       string `json:"transcript_path"`
	HookEventName        string `json:"hook_event_name"`
	CWD                  string `json:"cwd"`
	Reason               string `json:"reason,omitempty"`
	LastAssistantMessage string `json:"last_assistant_message,omitempty"`
	PermissionMode       string `json:"permission_mode,omitempty"`
}

// Handle reads hook input from stdin and processes it.
func Handle(cfg config.Config, event string) error {
	input, err := readStdin()
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	return handleInput(input, event, cfg)
}

// handleInput contains all dispatch logic, separated from stdin reading for testability.
func handleInput(input *Input, event string, cfg config.Config) error {
	// Use event override if provided (e.g., --event stop)
	if event != "" {
		input.HookEventName = event
	}

	switch input.HookEventName {
	case "SessionEnd", "":
		return handleSessionEnd(input, cfg)
	case "Stop", "PreCompact":
		return handleStop(input, cfg)
	default:
		return fmt.Errorf("unknown hook event: %s", input.HookEventName)
	}
}

// maxStdinSize is the maximum number of bytes read from stdin (64KB).
const maxStdinSize = 64 * 1024

func readStdin() (*Input, error) {
	return readStdinFrom(os.Stdin, 2*time.Second)
}

// readStdinFrom reads and parses hook JSON from the given reader with a timeout.
// On timeout, the pipe writer is closed to unblock the reading goroutine,
// preventing goroutine leaks.
func readStdinFrom(r io.Reader, timeout time.Duration) (*Input, error) {
	// Use a pipe so we can close the writer to unblock the reader on timeout.
	pr, pw := io.Pipe()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Copy from stdin (with size limit) into the pipe.
	// Closing pw unblocks any pending read on pr.
	go func() {
		_, err := io.Copy(pw, io.LimitReader(r, maxStdinSize))
		if err != nil {
			pw.CloseWithError(err)
		} else {
			pw.Close()
		}
	}()

	done := make(chan struct{})
	var data []byte
	var readErr error

	go func() {
		data, readErr = io.ReadAll(pr)
		close(done)
	}()

	select {
	case <-done:
		// Read completed
	case <-ctx.Done():
		// Timeout: close the pipe writer to unblock ReadAll on the pipe reader
		pw.CloseWithError(fmt.Errorf("stdin read timeout"))
		<-done // wait for goroutine to finish
		return nil, fmt.Errorf("stdin read timeout")
	}

	if readErr != nil {
		return nil, readErr
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("empty stdin")
	}

	var input Input
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("parse stdin JSON: %w", err)
	}

	return &input, nil
}

func handleStop(input *Input, cfg config.Config) error {
	if input.TranscriptPath == "" {
		return nil
	}

	if _, err := os.Stat(input.TranscriptPath); os.IsNotExist(err) {
		return nil
	}

	// Resolve project root for sessionclaim integration (Phase 4 / M8 of
	// session-slot-multihost-disambiguation). Empty input.CWD falls back
	// inside session.Capture once it parses the transcript.
	projectRoot := session.DetectProjectRoot(input.CWD)

	result, err := session.Capture(session.CaptureOpts{
		TranscriptPath: input.TranscriptPath,
		CWD:            input.CWD,
		SessionID:      input.SessionID,
		ProjectRoot:    projectRoot,
		Checkpoint:     true,
		SkipEnrichment: true,
	}, cfg)
	if err != nil {
		return fmt.Errorf("capture checkpoint: %w", err)
	}

	if result.Skipped {
		return nil
	}

	fmt.Fprintf(os.Stderr, "vv: checkpoint %s → %s\n", result.Project, result.NotePath)
	return nil
}

func handleSessionEnd(input *Input, cfg config.Config) error {
	if input.TranscriptPath == "" {
		return fmt.Errorf("no transcript_path in hook input")
	}

	// Create LLM provider (nil if disabled). When enabled, the layered key
	// resolver (providers.<P>.api_key → env-var → actionable error) runs
	// inside NewProvider.
	provider, providerErr := llm.NewProvider(cfg.Enrichment, cfg.Providers)
	if providerErr != nil {
		log.Printf("warning: LLM provider init failed: %v", providerErr)
	}

	// Resolve project root for sessionclaim integration (Phase 4 / M8).
	projectRoot := session.DetectProjectRoot(input.CWD)

	result, err := session.Capture(session.CaptureOpts{
		TranscriptPath: input.TranscriptPath,
		CWD:            input.CWD,
		SessionID:      input.SessionID,
		ProjectRoot:    projectRoot,
		Provider:       provider,
	}, cfg)
	if err != nil {
		// On error, do NOT release the claim — keep it alive so retry/
		// recovery works on the next event.
		return fmt.Errorf("capture session: %w", err)
	}

	// Release the session claim after a successful Stop-to-SessionEnd
	// lifecycle (Phase 4 of session-slot-multihost-disambiguation).
	// Errors are logged warnings, not propagated — release failures are
	// non-fatal for the user-visible capture pipeline.
	if projectRoot != "" {
		if releaseErr := sessionclaim.ReleaseSession(projectRoot); releaseErr != nil {
			log.Printf("warning: sessionclaim.ReleaseSession: %v", releaseErr)
		}
	}

	if result.Skipped {
		fmt.Fprintf(os.Stderr, "vv: skipped (%s)\n", result.Reason)
		return nil
	}

	// Report enrichment status in output. The "enriched by X" message is only
	// printed when enrichment actually produced usable content — checking config
	// validity alone is not enough, since a valid config can still 401/500/timeout
	// at the HTTP layer and leave the note heuristic-only, and prose extraction
	// deliberately short-circuits the LLM enrichment path when it has output.
	providerName, model, reason := llm.Available(cfg.Enrichment)
	var enrichTag string
	switch {
	case !cfg.Enrichment.Enabled:
		enrichTag = "heuristic — no LLM configured"
	case reason != "":
		enrichTag = fmt.Sprintf("heuristic — LLM unavailable: %s", reason)
	case !result.EnrichmentAttempted:
		enrichTag = "prose-extracted (LLM enrichment skipped by design)"
	case result.EnrichmentApplied:
		enrichTag = fmt.Sprintf("enriched by %s/%s", providerName, model)
	default:
		enrichTag = fmt.Sprintf("heuristic — LLM call failed (target: %s/%s; see warning above)", providerName, model)
	}
	fmt.Fprintf(os.Stderr, "vv: session captured → %s (%s)\n", result.NotePath, enrichTag)

	if result.FrictionAlert != "" {
		fmt.Fprintf(os.Stderr, "vv: %s\n", result.FrictionAlert)
	}

	// Session synthesis (judgment layer) — runs before context refresh
	if cfg.Synthesis.Enabled {
		if provider == nil {
			fmt.Fprintf(os.Stderr, "vv: synthesis skipped — no LLM provider configured\n")
		} else {
			synthIdx, synthIdxErr := index.Load(cfg.StateDir())
			if synthIdxErr != nil {
				fmt.Fprintf(os.Stderr, "vv: synthesis skipped — index load failed: %v\n", synthIdxErr)
			} else {
				synthTimeout := time.Duration(cfg.Synthesis.TimeoutSeconds) * time.Second
				if synthTimeout == 0 {
					synthTimeout = 60 * time.Second
				}
				synthCtx, synthCancel := context.WithTimeout(context.Background(), synthTimeout)
				defer synthCancel()

				notePath := filepath.Join(cfg.VaultPath, result.NotePath)
				report, synthErr := synthesis.Run(synthCtx, synthesis.RunOpts{
					NotePath: notePath,
					CWD:      input.CWD,
					Project:  result.Project,
					Provider: provider,
					Index:    synthIdx,
				}, cfg)
				if synthErr != nil {
					fmt.Fprintf(os.Stderr, "vv: synthesis warning: %v\n", synthErr)
				} else if report != nil {
					fmt.Fprintf(os.Stderr, "vv: synthesis — %d learnings, %d stale flagged, %d tasks updated\n",
						report.LearningsAdded, report.StalesFlagged, report.TasksUpdated)
				}
			}
		}
	}

	// Auto-refresh context documents
	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		fmt.Fprintf(os.Stderr, "vv: warning: could not refresh context: %v\n", err)
		return nil
	}

	genResult, err := index.GenerateContext(idx, cfg.VaultPath, index.ContextOptions{
		AlertThreshold:       cfg.Friction.AlertThreshold,
		TimelineRecentDays:   cfg.History.TimelineRecentDays,
		TimelineWindowDays:   cfg.History.TimelineWindowDays,
		DecisionStaleDays:    cfg.History.DecisionStaleDays,
		KeyFilesRecencyBoost: cfg.History.KeyFilesRecencyBoost,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "vv: warning: context refresh failed: %v\n", err)
		return nil
	}
	fmt.Fprintf(os.Stderr, "vv: context refreshed (%d projects)\n", genResult.ProjectsUpdated)

	return nil
}
