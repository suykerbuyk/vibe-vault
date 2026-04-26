// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/mdutil"
	"github.com/suykerbuyk/vibe-vault/internal/narrative"
	"github.com/suykerbuyk/vibe-vault/internal/session"
	"github.com/suykerbuyk/vibe-vault/internal/transcript"
	"github.com/suykerbuyk/vibe-vault/internal/wrapmetrics"
)

// applyWriteRecord records the outcome of a single dispatch step.
type applyWriteRecord struct {
	Step   string `json:"step"`
	Status string `json:"status"` // "ok" or "error"
	Detail string `json:"detail,omitempty"`
}

// driftSummary summarises the total synth-vs-apply drift across all fields.
type driftSummary struct {
	FieldsTotal    int  `json:"fields_total"`
	DriftedFields  int  `json:"drifted_fields"`
	TotalDriftBytes int `json:"total_drift_bytes"`
}

// applyResult is the JSON shape returned by vv_apply_wrap_bundle.
type applyResult struct {
	AppliedWrites []applyWriteRecord `json:"applied_writes"`
	DriftSummary  driftSummary       `json:"drift_summary"`
	MetricFile    string             `json:"metric_file"`
	ErrorAtStep   string             `json:"error_at_step,omitempty"`
}

// NewApplyWrapBundleTool creates the vv_apply_wrap_bundle tool.
//
// Receives the bundle produced by vv_synthesize_wrap (possibly AI-edited)
// and dispatches all writes atomically in order:
//  1. vv_append_iteration (iteration_block)
//  2. vv_thread_insert for each resume_thread_blocks entry
//  3. vv_thread_remove for each resume_threads_to_close entry
//  4. vv_carried_add for each carried_changes.add entry
//  5. vv_carried_remove for each carried_changes.remove entry
//  6. vv_set_commit_msg (commit_msg)
//  7. vv_capture_session (capture_session)
//
// For each field, apply-time SHA-256 is computed and both synth-time and
// apply-time fingerprints are logged to ~/.cache/vibe-vault/wrap-metrics.jsonl.
// Drift (apply != synth) is logged but does NOT abort the operation.
//
// On partial failure, returns applied_writes listing what succeeded plus an
// error_at_step field. Completed writes are not rolled back.
func NewApplyWrapBundleTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_apply_wrap_bundle",
			Description: "Dispatch all writes from a vv_synthesize_wrap bundle atomically: " +
				"appends the iteration block, inserts/removes threads, adds/removes " +
				"carried-forward bullets, writes commit.msg, and calls capture_session. " +
				"Computes apply-time SHA-256 for each field and logs both synth and apply " +
				"fingerprints to ~/.cache/vibe-vault/wrap-metrics.jsonl. " +
				"Drift between synth and apply is observable post-hoc (not blocked). " +
				"On partial failure, returns applied_writes + error_at_step; completed " +
				"writes are not rolled back.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"project": {
						"type": "string",
						"description": "Project name. If omitted, detected from working directory."
					},
					"project_path": {
						"type": "string",
						"description": "Absolute path to the project root (needed for vv_set_commit_msg). If omitted, derived via meta.ProjectRoot()."
					},
					"bundle": {
						"type": "object",
						"description": "The bundle object returned by vv_synthesize_wrap (possibly AI-edited)."
					}
				},
				"required": ["bundle"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project     string          `json:"project"`
				ProjectPath string          `json:"project_path"`
				Bundle      json.RawMessage `json:"bundle"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}
			if len(args.Bundle) == 0 {
				return "", fmt.Errorf("bundle is required")
			}

			var bundle WrapBundle
			if err := json.Unmarshal(args.Bundle, &bundle); err != nil {
				return "", fmt.Errorf("parse bundle: %w", err)
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}

			// Resolve project root for commit.msg write.
			projectRoot, projectRootErr := resolveProjectRoot(args.ProjectPath, cfg.VaultPath)

			// Collect provenance for metrics.
			host, user, cwd := provenanceForMetrics()
			iteration := bundle.Iteration

			var (
				writes      []applyWriteRecord
				metricLines []wrapmetrics.Line
				ds          driftSummary
				errorAtStep string
			)

			// recordMetricRaw records a metric line given the apply-side SHA and
			// byte length directly (used when the apply SHA can't be derived from a
			// simple string — e.g. capture_session which is a struct).
			recordMetricRaw := func(field, synthSHA, applySHA string, applyBytes int) {
				driftB := 0
				synthBytes := applyBytes
				if applySHA != synthSHA {
					ds.DriftedFields++
				}
				ds.FieldsTotal++
				ds.TotalDriftBytes += driftB

				metricLines = append(metricLines, wrapmetrics.Line{
					Timestamp:   time.Now().UTC().Format(time.RFC3339),
					Field:       field,
					SynthSHA256: synthSHA,
					ApplySHA256: applySHA,
					SynthBytes:  synthBytes,
					ApplyBytes:  applyBytes,
					DriftBytes:  driftB,
				})
			}

			// Helper: record a metric line for a string-content field.
			recordMetric := func(field, synthSHA, content string) {
				applySHA := fingerprintString(content)
				recordMetricRaw(field, synthSHA, applySHA, len(content))
			}

			// --- Step 1: append iteration_block ---
			{
				content := bundle.IterationBlock.Content
				recordMetric("iteration_block", bundle.IterationBlock.SynthSHA256, content)

				err := applyAppendIteration(cfg, project, bundle.Iteration, content)
				if err != nil {
					writes = append(writes, applyWriteRecord{Step: "append_iteration", Status: "error", Detail: err.Error()})
					errorAtStep = "append_iteration"
					goto done
				}
				writes = append(writes, applyWriteRecord{Step: "append_iteration", Status: "ok"})
			}

			// --- Step 2: thread_insert for each block ---
			for i, tb := range bundle.ResumeThreadBlocks {
				content := tb.Body
				recordMetric(fmt.Sprintf("resume_thread_blocks[%d]", i), tb.SynthSHA256, content)

				err := applyThreadInsert(cfg, project, tb)
				if err != nil {
					writes = append(writes, applyWriteRecord{
						Step:   fmt.Sprintf("thread_insert[%d]:%s", i, tb.Slug),
						Status: "error",
						Detail: err.Error(),
					})
					errorAtStep = fmt.Sprintf("thread_insert[%d]:%s", i, tb.Slug)
					goto done
				}
				writes = append(writes, applyWriteRecord{Step: fmt.Sprintf("thread_insert[%d]:%s", i, tb.Slug), Status: "ok"})
			}

			// --- Step 3: thread_remove for each close ---
			for i, tc := range bundle.ResumeThreadsToClose {
				recordMetric(fmt.Sprintf("resume_threads_to_close[%d]", i), tc.SynthSHA256, tc.Slug)

				err := applyThreadRemove(cfg, project, tc.Slug)
				if err != nil {
					writes = append(writes, applyWriteRecord{
						Step:   fmt.Sprintf("thread_remove[%d]:%s", i, tc.Slug),
						Status: "error",
						Detail: err.Error(),
					})
					errorAtStep = fmt.Sprintf("thread_remove[%d]:%s", i, tc.Slug)
					goto done
				}
				writes = append(writes, applyWriteRecord{Step: fmt.Sprintf("thread_remove[%d]:%s", i, tc.Slug), Status: "ok"})
			}

			// --- Step 4: carried_add ---
			for i, ca := range bundle.CarriedChanges.Add {
				content := ca.Slug + "\x00" + ca.Title + "\x00" + ca.Body
				recordMetric(fmt.Sprintf("carried_changes.add[%d]", i), ca.SynthSHA256, content)

				err := applyCarriedAdd(cfg, project, ca.Slug, ca.Title, ca.Body)
				if err != nil {
					writes = append(writes, applyWriteRecord{
						Step:   fmt.Sprintf("carried_add[%d]:%s", i, ca.Slug),
						Status: "error",
						Detail: err.Error(),
					})
					errorAtStep = fmt.Sprintf("carried_add[%d]:%s", i, ca.Slug)
					goto done
				}
				writes = append(writes, applyWriteRecord{Step: fmt.Sprintf("carried_add[%d]:%s", i, ca.Slug), Status: "ok"})
			}

			// --- Step 5: carried_remove ---
			for i, cr := range bundle.CarriedChanges.Remove {
				recordMetric(fmt.Sprintf("carried_changes.remove[%d]", i), cr.SynthSHA256, cr.Slug)

				err := applyCarriedRemove(cfg, project, cr.Slug)
				if err != nil {
					writes = append(writes, applyWriteRecord{
						Step:   fmt.Sprintf("carried_remove[%d]:%s", i, cr.Slug),
						Status: "error",
						Detail: err.Error(),
					})
					errorAtStep = fmt.Sprintf("carried_remove[%d]:%s", i, cr.Slug)
					goto done
				}
				writes = append(writes, applyWriteRecord{Step: fmt.Sprintf("carried_remove[%d]:%s", i, cr.Slug), Status: "ok"})
			}

			// --- Step 6: set_commit_msg ---
			{
				content := bundle.CommitMsg.Content
				recordMetric("commit_msg", bundle.CommitMsg.SynthSHA256, content)

				if projectRootErr != nil {
					writes = append(writes, applyWriteRecord{
						Step:   "set_commit_msg",
						Status: "error",
						Detail: fmt.Sprintf("project root not resolved: %v", projectRootErr),
					})
					errorAtStep = "set_commit_msg"
					goto done
				}

				err := applySetCommitMsg(cfg, project, projectRoot, content)
				if err != nil {
					writes = append(writes, applyWriteRecord{Step: "set_commit_msg", Status: "error", Detail: err.Error()})
					errorAtStep = "set_commit_msg"
					goto done
				}
				writes = append(writes, applyWriteRecord{Step: "set_commit_msg", Status: "ok"})
			}

			// --- Step 7: capture_session ---
			{
				cc := bundle.CaptureSession.Content
				// Fingerprint the struct the same way synthesize did: fingerprintJSON.
				applyCaptureSHA, fpErr := fingerprintJSON(cc)
				if fpErr != nil {
					applyCaptureSHA = ""
				}
				captureBytes := 0
				if captureJSON, jsonErr := json.Marshal(cc); jsonErr == nil {
					captureBytes = len(captureJSON)
				}
				recordMetricRaw("capture_session", bundle.CaptureSession.SynthSHA256, applyCaptureSHA, captureBytes)

				err := applyCaptureSesssion(cfg, project, cc)
				if err != nil {
					writes = append(writes, applyWriteRecord{Step: "capture_session", Status: "error", Detail: err.Error()})
					errorAtStep = "capture_session"
					goto done
				}
				writes = append(writes, applyWriteRecord{Step: "capture_session", Status: "ok"})
			}

		done:
			// Write metric lines — non-fatal on error.
			cacheDir, _ := wrapmetrics.CacheDir()
			_ = wrapmetrics.AppendBundleLines(host, user, cwd, project, iteration, metricLines)

			result := applyResult{
				AppliedWrites: writes,
				DriftSummary:  ds,
				MetricFile:    filepath.Join(cacheDir, wrapmetrics.ActiveFile),
				ErrorAtStep:   errorAtStep,
			}
			out, marshalErr := json.MarshalIndent(result, "", "  ")
			if marshalErr != nil {
				return "", fmt.Errorf("marshal result: %w", marshalErr)
			}
			return string(out) + "\n", nil
		},
	}
}

// applyAppendIteration appends the pre-built iteration block content to
// iterations.md, auto-incrementing when iterNum == 0.
func applyAppendIteration(cfg config.Config, project string, iterNum int, blockContent string) error {
	path := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "iterations.md")
	absPath, err := vaultPrefixCheck(path, cfg.VaultPath)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(absPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read iterations: %w", err)
	}
	content := string(data)
	if os.IsNotExist(err) {
		content = "# Iterations\n"
	}

	// If iterNum == 0, auto-increment from highest existing.
	if iterNum == 0 {
		existing := scanIterationNumbers(content)
		highest := 0
		for _, n := range existing {
			if n > highest {
				highest = n
			}
		}
		iterNum = highest + 1
	} else {
		// Check for duplicate.
		for _, n := range scanIterationNumbers(content) {
			if n == iterNum {
				return fmt.Errorf("iteration %d already exists", iterNum)
			}
		}
	}

	// The blockContent from synthesize already has the formatted block
	// (heading + narrative + trailer). We need to re-derive if iterNum changed.
	// However, the apply tool receives the verbatim block from the bundle —
	// if iterNum was 0 at synth time the heading will say "Iteration 0".
	// Detect that and re-build with the resolved number.
	if strings.Contains(blockContent, "### Iteration 0 —") {
		// Re-build: extract title, narrative, date from the block.
		rebuilt, ok := rebuildIterationBlock(blockContent, iterNum, cfg.VaultPath)
		if ok {
			blockContent = rebuilt
		}
	}

	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += blockContent

	return mdutil.AtomicWriteFile(absPath, []byte(content), 0o644)
}

// rebuildIterationBlock parses a synthesized iteration block and re-emits it
// with the given iteration number, preserving the title and narrative.
// Returns (rebuilt, true) on success, ("", false) on parse failure.
func rebuildIterationBlock(block string, iterNum int, vaultPath string) (string, bool) {
	lines := strings.Split(block, "\n")
	// Find the heading line: "### Iteration N — title (date)"
	for i, line := range lines {
		if strings.HasPrefix(line, "### Iteration ") {
			// Extract title+date suffix after "### Iteration N — ".
			dashIdx := strings.Index(line, " — ")
			if dashIdx < 0 {
				return "", false
			}
			suffix := line[dashIdx+3:] // "title (date)"
			// Extract date from "(date)" at end.
			lparen := strings.LastIndex(suffix, "(")
			rparen := strings.LastIndex(suffix, ")")
			if lparen < 0 || rparen < 0 || rparen < lparen {
				return "", false
			}
			date := suffix[lparen+1 : rparen]
			title := strings.TrimSpace(suffix[:lparen])

			// Narrative is everything between the heading line and the provenance trailer.
			narrative := strings.Join(lines[i+2:], "\n")
			// Strip provenance trailer (<!-- recorded: ... -->) if present.
			if idx := strings.Index(narrative, "\n\n<!-- recorded:"); idx >= 0 {
				narrative = narrative[:idx]
			}
			narrative = strings.TrimRight(narrative, "\n")
			return BuildIterationBlock(iterNum, title, narrative, date, vaultPath), true
		}
	}
	return "", false
}

// applyThreadInsert inserts a thread block into resume.md.
func applyThreadInsert(cfg config.Config, project string, tb BundleThreadBlock) error {
	content, absPath, err := readResume(cfg, project)
	if err != nil {
		return err
	}

	mode := tb.Position["mode"]
	anchor := tb.Position["anchor_slug"]
	pos := mdutil.InsertPosition{Mode: mode, AnchorSlug: anchor}

	updated, err := mdutil.InsertSubsection(content, openThreadsSection, pos, tb.Slug, tb.Body)
	if err != nil {
		return err
	}
	return mdutil.AtomicWriteFile(absPath, []byte(updated), 0o644)
}

// applyThreadRemove removes a thread slug from resume.md.
func applyThreadRemove(cfg config.Config, project, slug string) error {
	if err := rejectCarriedForward(slug); err != nil {
		return err
	}
	content, absPath, err := readResume(cfg, project)
	if err != nil {
		return err
	}
	raw, err := mdutil.RemoveSubsection(content, openThreadsSection, slug)
	if err != nil {
		return err
	}
	updated, _ := extractCandidatesWarning(raw)
	return mdutil.AtomicWriteFile(absPath, []byte(updated), 0o644)
}

// applyCarriedAdd adds a carried-forward bullet.
func applyCarriedAdd(cfg config.Config, project, slug, title, body string) error {
	content, absPath, err := readResume(cfg, project)
	if err != nil {
		return err
	}
	updated, err := mdutil.AddCarriedBullet(content, openThreadsSection, slug, title, body)
	if err != nil {
		return err
	}
	return mdutil.AtomicWriteFile(absPath, []byte(updated), 0o644)
}

// applyCarriedRemove removes a carried-forward bullet.
func applyCarriedRemove(cfg config.Config, project, slug string) error {
	content, absPath, err := readResume(cfg, project)
	if err != nil {
		return err
	}
	updated, err := mdutil.RemoveCarriedBullet(content, openThreadsSection, slug)
	if err != nil {
		return err
	}
	return mdutil.AtomicWriteFile(absPath, []byte(updated), 0o644)
}

// applySetCommitMsg writes commit.msg to both vault and project root.
func applySetCommitMsg(cfg config.Config, project, projectRoot, content string) error {
	vaultDest := vaultCommitMsgPath(cfg.VaultPath, project)
	absVaultDest, err := vaultPrefixCheck(vaultDest, cfg.VaultPath)
	if err != nil {
		return fmt.Errorf("vault path check: %w", err)
	}
	data := []byte(content)
	if err := atomicWriteCommitMsg(absVaultDest, data); err != nil {
		return fmt.Errorf("write vault commit.msg: %w", err)
	}
	projDest := projectCommitMsgPath(projectRoot)
	if err := atomicWriteCommitMsg(projDest, data); err != nil {
		return fmt.Errorf("vault commit.msg written to %q but project-root copy failed: %w", absVaultDest, err)
	}
	return nil
}

// applyCaptureSesssion calls session.CaptureFromParsed with the bundle payload.
func applyCaptureSesssion(cfg config.Config, _ string, cc BundleCaptureContent) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	randBytes := make([]byte, 16)
	if _, randErr := rand.Read(randBytes); randErr != nil {
		return fmt.Errorf("generate session ID: %w", randErr)
	}
	sessionID := "vv-apply:" + hex.EncodeToString(randBytes)

	branch := detectBranch(cwd)
	info := session.Detect(cwd, branch, "", sessionID, cfg)

	t := &transcript.Transcript{
		Stats: transcript.Stats{
			UserMessages:      2,
			AssistantMessages: 2,
			StartTime:         time.Now(),
			FilesWritten:      make(map[string]bool),
		},
	}
	for _, f := range cc.FilesChanged {
		t.Stats.FilesWritten[f] = true
	}

	narr := &narrative.Narrative{
		Title:       cc.Summary,
		Summary:     cc.Summary,
		Tag:         cc.Tag,
		Decisions:   cc.Decisions,
		OpenThreads: cc.OpenThreads,
	}

	opts := session.CaptureOpts{
		Source:         "vv-apply",
		SkipEnrichment: true,
		CWD:            cwd,
		SessionID:      sessionID,
	}

	result, err := session.CaptureFromParsed(t, info, narr, nil, opts, cfg)
	if err != nil {
		return fmt.Errorf("capture session: %w", err)
	}
	if result.Skipped {
		// Not an error — just log-level info.
		_ = result.Reason
	}
	return nil
}

// detectBranch is already defined in tools.go — used here without re-declaration.
// (it's in the same package, so no import needed.)
