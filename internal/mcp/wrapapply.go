// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// File wrapapply.go houses ApplyBundle — the in-process orchestrator that
// executes every mutation class in a WrapBundle against the vault.
//
// Apply order (extended for H2-v3 thread_replace + state-block re-render):
//
//	iter
//	  → thread_insert × N
//	  → thread_replace × R          (NEW H2-v3)
//	  → thread_remove × M
//	  → carried_add × P
//	  → carried_remove × Q
//	  → set_commit_msg × 1
//	  → capture × 1
//	  → resume_state_blocks × 1     (Step 9; state-derived sub-regions)
//
// On the first error, ApplyBundle stops and returns the partial result
// (the wrap is fail-stop, NOT transactional — see Decision 63 in DESIGN.md).
//
// ApplyBundle is NOT re-entrant on resume.md: Step 9 reads then writes the
// file in one apply cycle, so two concurrent /wrap invocations on the same
// project could race. v1 ships without file locking on the assumption that
// a single operator runs /wrap serially; revisit if multi-operator wraps
// surface as a failure pattern.
//
// ApplyBundle is exported as a Go-callable helper so the Phase 3c dispatch
// handler can invoke it directly without re-entering the MCP loop. The
// vv_apply_wrap_bundle_by_handle tool is a thin wrapper that loads + verifies
// a skeleton from cache, reconstructs the bundle via FillBundle, and then
// calls ApplyBundle.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/mdutil"
	"github.com/suykerbuyk/vibe-vault/internal/wrapmetrics"
)

// ApplyBundle executes the bundle against the vault. ctx is reserved for
// future cancellation hookup; current implementations of the per-step apply
// helpers do not consume it. cfg supplies the vault path; project disambiguates
// the target Project/<name>/ subtree; projectRoot is needed for the
// commit.msg dual-write.
//
// On the first error, ApplyBundle stops and returns ErrorAtStep set; earlier
// successful writes are NOT rolled back.
func ApplyBundle(_ context.Context, cfg config.Config, project, projectRoot string, bundle WrapBundle) (applyResult, error) {
	host, user, cwd := provenanceForMetrics()
	iteration := bundle.Iteration

	var (
		writes      []applyWriteRecord
		metricLines []wrapmetrics.Line
		ds          driftSummary
		errorAtStep string
	)

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

	recordMetric := func(field, synthSHA, content string) {
		applySHA := fingerprintString(content)
		recordMetricRaw(field, synthSHA, applySHA, len(content))
	}

	// Step 1: append iteration_block.
	{
		content := bundle.IterationBlock.Content
		recordMetric("iteration_block", bundle.IterationBlock.SynthSHA256, content)

		if err := applyAppendIteration(cfg, project, bundle.Iteration, content); err != nil {
			writes = append(writes, applyWriteRecord{Step: "append_iteration", Status: "error", Detail: err.Error()})
			errorAtStep = "append_iteration"
			return finishApply(host, user, cwd, project, iteration, writes, ds, metricLines, errorAtStep), nil
		}
		writes = append(writes, applyWriteRecord{Step: "append_iteration", Status: "ok"})
	}

	// Step 2: thread_insert.
	for i, tb := range bundle.ResumeThreadBlocks {
		content := tb.Body
		recordMetric(fmt.Sprintf("resume_thread_blocks[%d]", i), tb.SynthSHA256, content)

		if err := applyThreadInsert(cfg, project, tb); err != nil {
			step := fmt.Sprintf("thread_insert[%d]:%s", i, tb.Slug)
			writes = append(writes, applyWriteRecord{Step: step, Status: "error", Detail: err.Error()})
			errorAtStep = step
			return finishApply(host, user, cwd, project, iteration, writes, ds, metricLines, errorAtStep), nil
		}
		writes = append(writes, applyWriteRecord{Step: fmt.Sprintf("thread_insert[%d]:%s", i, tb.Slug), Status: "ok"})
	}

	// Step 3: thread_replace (H2-v3).
	for i, tr := range bundle.ResumeThreadsReplace {
		content := tr.Body
		recordMetric(fmt.Sprintf("resume_threads_to_replace[%d]", i), tr.SynthSHA256, content)

		if err := applyThreadReplace(cfg, project, tr); err != nil {
			step := fmt.Sprintf("thread_replace[%d]:%s", i, tr.Slug)
			writes = append(writes, applyWriteRecord{Step: step, Status: "error", Detail: err.Error()})
			errorAtStep = step
			return finishApply(host, user, cwd, project, iteration, writes, ds, metricLines, errorAtStep), nil
		}
		writes = append(writes, applyWriteRecord{Step: fmt.Sprintf("thread_replace[%d]:%s", i, tr.Slug), Status: "ok"})
	}

	// Step 4: thread_remove.
	for i, tc := range bundle.ResumeThreadsToClose {
		recordMetric(fmt.Sprintf("resume_threads_to_close[%d]", i), tc.SynthSHA256, tc.Slug)

		if err := applyThreadRemove(cfg, project, tc.Slug); err != nil {
			step := fmt.Sprintf("thread_remove[%d]:%s", i, tc.Slug)
			writes = append(writes, applyWriteRecord{Step: step, Status: "error", Detail: err.Error()})
			errorAtStep = step
			return finishApply(host, user, cwd, project, iteration, writes, ds, metricLines, errorAtStep), nil
		}
		writes = append(writes, applyWriteRecord{Step: fmt.Sprintf("thread_remove[%d]:%s", i, tc.Slug), Status: "ok"})
	}

	// Step 5: carried_add.
	for i, ca := range bundle.CarriedChanges.Add {
		content := ca.Slug + "\x00" + ca.Title + "\x00" + ca.Body
		recordMetric(fmt.Sprintf("carried_changes.add[%d]", i), ca.SynthSHA256, content)

		if err := applyCarriedAdd(cfg, project, ca.Slug, ca.Title, ca.Body); err != nil {
			step := fmt.Sprintf("carried_add[%d]:%s", i, ca.Slug)
			writes = append(writes, applyWriteRecord{Step: step, Status: "error", Detail: err.Error()})
			errorAtStep = step
			return finishApply(host, user, cwd, project, iteration, writes, ds, metricLines, errorAtStep), nil
		}
		writes = append(writes, applyWriteRecord{Step: fmt.Sprintf("carried_add[%d]:%s", i, ca.Slug), Status: "ok"})
	}

	// Step 6: carried_remove.
	for i, cr := range bundle.CarriedChanges.Remove {
		recordMetric(fmt.Sprintf("carried_changes.remove[%d]", i), cr.SynthSHA256, cr.Slug)

		if err := applyCarriedRemove(cfg, project, cr.Slug); err != nil {
			step := fmt.Sprintf("carried_remove[%d]:%s", i, cr.Slug)
			writes = append(writes, applyWriteRecord{Step: step, Status: "error", Detail: err.Error()})
			errorAtStep = step
			return finishApply(host, user, cwd, project, iteration, writes, ds, metricLines, errorAtStep), nil
		}
		writes = append(writes, applyWriteRecord{Step: fmt.Sprintf("carried_remove[%d]:%s", i, cr.Slug), Status: "ok"})
	}

	// Step 7: set_commit_msg.
	{
		content := bundle.CommitMsg.Content
		recordMetric("commit_msg", bundle.CommitMsg.SynthSHA256, content)

		if projectRoot == "" {
			writes = append(writes, applyWriteRecord{
				Step:   "set_commit_msg",
				Status: "error",
				Detail: "project root not resolved",
			})
			errorAtStep = "set_commit_msg"
			return finishApply(host, user, cwd, project, iteration, writes, ds, metricLines, errorAtStep), nil
		}
		if err := applySetCommitMsg(cfg, project, projectRoot, content); err != nil {
			writes = append(writes, applyWriteRecord{Step: "set_commit_msg", Status: "error", Detail: err.Error()})
			errorAtStep = "set_commit_msg"
			return finishApply(host, user, cwd, project, iteration, writes, ds, metricLines, errorAtStep), nil
		}
		writes = append(writes, applyWriteRecord{Step: "set_commit_msg", Status: "ok"})
	}

	// Step 8: capture_session.
	{
		cc := bundle.CaptureSession.Content
		applyCaptureSHA, fpErr := fingerprintJSON(cc)
		if fpErr != nil {
			applyCaptureSHA = ""
		}
		captureBytes := 0
		if captureJSON, jsonErr := json.Marshal(cc); jsonErr == nil {
			captureBytes = len(captureJSON)
		}
		recordMetricRaw("capture_session", bundle.CaptureSession.SynthSHA256, applyCaptureSHA, captureBytes)

		if err := applyCaptureSesssion(cfg, project, cc); err != nil {
			writes = append(writes, applyWriteRecord{Step: "capture_session", Status: "error", Detail: err.Error()})
			errorAtStep = "capture_session"
			return finishApply(host, user, cwd, project, iteration, writes, ds, metricLines, errorAtStep), nil
		}
		writes = append(writes, applyWriteRecord{Step: "capture_session", Status: "ok"})
	}

	// Step 9: resume_state_blocks. Re-renders the three marker-bounded
	// sub-regions of resume.md (active-tasks, current-state, project-history-tail)
	// from filesystem ground truth on every wrap, fixing the dispatch-path
	// drift class verified across iters 165–166. Step 9 must remain LAST so
	// any prior `vv_update_resume` calls in the same wrap that clobbered
	// markers are healed by `wraprender.ApplyMarkerBlocks` here.
	//
	// Metric-line special case: the bundle never carries marker content, so
	// synth-vs-apply drift accounting is meaningless for this step. We
	// record synth_sha = apply_sha = fingerprint(rendered_content) so
	// `recordMetricRaw` does NOT increment `ds.DriftedFields` for Step 9
	// (it still increments `ds.FieldsTotal` for accountability).
	{
		rendered, err := applyResumeStateBlocks(cfg, project, projectRoot)
		if err != nil {
			writes = append(writes, applyWriteRecord{Step: "resume_state_blocks", Status: "error", Detail: err.Error()})
			errorAtStep = "resume_state_blocks"
			return finishApply(host, user, cwd, project, iteration, writes, ds, metricLines, errorAtStep), nil
		}
		sha := fingerprintString(rendered)
		recordMetricRaw("resume_state_blocks", sha, sha, len(rendered))
		writes = append(writes, applyWriteRecord{Step: "resume_state_blocks", Status: "ok"})
	}

	return finishApply(host, user, cwd, project, iteration, writes, ds, metricLines, errorAtStep), nil
}

// finishApply writes metric lines (best-effort) and packages the apply result.
func finishApply(host, user, cwd, project string, iteration int, writes []applyWriteRecord, ds driftSummary, metricLines []wrapmetrics.Line, errorAtStep string) applyResult {
	cacheDir, _ := wrapmetrics.CacheDir()
	_ = wrapmetrics.AppendBundleLines(host, user, cwd, project, iteration, metricLines)

	return applyResult{
		AppliedWrites: writes,
		DriftSummary:  ds,
		MetricFile:    filepath.Join(cacheDir, wrapmetrics.ActiveFile),
		ErrorAtStep:   errorAtStep,
	}
}

// applyThreadReplace replaces an existing thread's body in resume.md (H2-v3).
// Per the wrap-model-tiering plan, thread_replace lives in the apply order
// between thread_insert and thread_remove.
func applyThreadReplace(cfg config.Config, project string, tr BundleThreadReplace) error {
	if err := rejectCarriedForwardReplace(tr.Slug); err != nil {
		return err
	}
	content, absPath, err := readResume(cfg, project)
	if err != nil {
		return err
	}
	updated, err := mdutil.ReplaceSubsectionBody(content, openThreadsSection, tr.Slug, tr.Body)
	if err != nil {
		// Direction-C D9: multi-match is now a hard error.
		return err
	}
	return mdutil.AtomicWriteFile(absPath, []byte(updated), 0o644)
}
