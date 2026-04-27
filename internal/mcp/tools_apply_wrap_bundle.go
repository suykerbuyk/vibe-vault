// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/mdutil"
	"github.com/suykerbuyk/vibe-vault/internal/narrative"
	"github.com/suykerbuyk/vibe-vault/internal/session"
	"github.com/suykerbuyk/vibe-vault/internal/transcript"
	"github.com/suykerbuyk/vibe-vault/internal/wraprender"
	"github.com/suykerbuyk/vibe-vault/templates"
)

// applyWriteRecord records the outcome of a single dispatch step.
type applyWriteRecord struct {
	Step   string `json:"step"`
	Status string `json:"status"` // "ok" or "error"
	Detail string `json:"detail,omitempty"`
}

// driftSummary summarises the total synth-vs-apply drift across all fields.
type driftSummary struct {
	FieldsTotal     int `json:"fields_total"`
	DriftedFields   int `json:"drifted_fields"`
	TotalDriftBytes int `json:"total_drift_bytes"`
}

// applyResult is the JSON shape returned by the wrap-apply tool.
type applyResult struct {
	AppliedWrites []applyWriteRecord `json:"applied_writes"`
	DriftSummary  driftSummary       `json:"drift_summary"`
	MetricFile    string             `json:"metric_file"`
	ErrorAtStep   string             `json:"error_at_step,omitempty"`
}

// expectedMutationCount computes the expected number of vault mutations the
// bundle should perform, derived purely from the skeleton's edit-plan slugs
// + 1 (iter) + 1 (commit_msg) + 1 (capture). Per Decision 8 of the wrap-
// model-tiering plan, this is the formula used to detect missing-prose or
// mis-mapped bodies before any vault write happens.
func expectedMutationCount(sk WrapSkeleton) int {
	return 1 + // iter
		len(sk.ResumeThreadBlocks) + // thread_insert × N
		len(sk.ResumeThreadsReplace) + // thread_replace × R (H2-v3)
		len(sk.ResumeThreadsToClose) + // thread_remove × M
		len(sk.CarriedChangesAdd) + // carried_add × P
		len(sk.CarriedChangesRemove) + // carried_remove × Q
		1 + // commit_msg
		1 // capture
}

// actualMutationCount counts the populated edit fields in the bundle. A
// thread or carried entry counts even if its body is empty — the body
// emptiness is a separate concern caught downstream.
func actualMutationCount(b WrapBundle) int {
	return 1 +
		len(b.ResumeThreadBlocks) +
		len(b.ResumeThreadsReplace) +
		len(b.ResumeThreadsToClose) +
		len(b.CarriedChanges.Add) +
		len(b.CarriedChanges.Remove) +
		1 +
		1
}

// NewApplyWrapBundleByHandleTool creates the vv_apply_wrap_bundle_by_handle tool.
//
// Loads a skeleton from the host-local cache via the handle, sha256-verifies
// it against the handle, reconstructs the full bundle via FillBundle, and
// dispatches the writes via ApplyBundle.
func NewApplyWrapBundleByHandleTool(cfg config.Config) Tool {
	return Tool{
		Definition: ToolDef{
			Name: "vv_apply_wrap_bundle_by_handle",
			Description: "Dispatch all writes for a wrap iteration, identified by a " +
				"skeleton handle plus executor-supplied prose outputs. The skeleton " +
				"is loaded from host-local cache and sha256-verified against the " +
				"handle; the bundle is reconstructed in-memory via FillBundle. Apply " +
				"order: iter -> thread_insert × N -> thread_replace × R -> thread_remove " +
				"× M -> carried_add × P -> carried_remove × Q -> set_commit_msg -> " +
				"capture_session. Each field's apply-time SHA-256 is logged to " +
				"~/.cache/vibe-vault/wrap-metrics.jsonl alongside the synth-time SHA. " +
				"On partial failure the apply is fail-stop: applied_writes lists what " +
				"succeeded, error_at_step names the failing step, completed writes are " +
				"NOT rolled back.",
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
					"skeleton_handle": {
						"type": "object",
						"description": "{iter, skeleton_path, skeleton_sha256} returned by vv_prepare_wrap_skeleton.",
						"properties": {
							"iter":            {"type": "integer"},
							"skeleton_path":   {"type": "string"},
							"skeleton_sha256": {"type": "string"}
						},
						"required": ["iter", "skeleton_path"]
					},
					"outputs": {
						"type": "object",
						"description": "Executor-supplied prose: {iteration_narrative, iteration_title, prose_body, commit_subject, date, thread_bodies, carried_bodies, capture_summary, capture_tag, capture_decisions, capture_files_changed, capture_open_threads}.",
						"properties": {
							"iteration_narrative":   {"type": "string"},
							"iteration_title":       {"type": "string"},
							"prose_body":            {"type": "string"},
							"commit_subject":        {"type": "string"},
							"date":                  {"type": "string"},
							"thread_bodies":         {"type": "object"},
							"carried_bodies":        {"type": "object"},
							"capture_summary":       {"type": "string"},
							"capture_tag":           {"type": "string"},
							"capture_decisions":     {"type": "array", "items": {"type": "string"}},
							"capture_files_changed": {"type": "array", "items": {"type": "string"}},
							"capture_open_threads":  {"type": "array", "items": {"type": "string"}}
						}
					}
				},
				"required": ["skeleton_handle", "outputs"]
			}`),
		},
		Handler: func(params json.RawMessage) (string, error) {
			var args struct {
				Project        string         `json:"project"`
				ProjectPath    string         `json:"project_path"`
				SkeletonHandle SkeletonHandle `json:"skeleton_handle"`
				Outputs        proseInputArgs `json:"outputs"`
			}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return "", fmt.Errorf("invalid arguments: %w", err)
				}
			}

			skeleton, err := loadSkeletonByHandle(args.SkeletonHandle)
			if err != nil {
				return "", err
			}
			if strings.Contains(args.Outputs.CommitSubject, "\n") {
				return "", fmt.Errorf("outputs.commit_subject must be a single line (no newlines)")
			}
			bundle := FillBundle(skeleton, args.Outputs.toProseFields())

			// Decision 8: validate expected vs actual mutation count BEFORE any
			// vault write. Phase 3b's QC tool will surface this earlier with
			// structured trigger info; here a raw error is sufficient.
			expected := expectedMutationCount(skeleton)
			actual := actualMutationCount(bundle)
			if expected != actual {
				return "", fmt.Errorf("mutation count mismatch: skeleton expects %d mutations, bundle has %d (no vault writes performed)", expected, actual)
			}

			project, err := resolveProject(args.Project)
			if err != nil {
				return "", err
			}
			projectRoot, projectRootErr := resolveProjectRoot(args.ProjectPath, cfg.VaultPath)
			rootArg := projectRoot
			if projectRootErr != nil {
				rootArg = ""
			}

			result, applyErr := ApplyBundle(context.TODO(), cfg, project, rootArg, bundle)
			if applyErr != nil {
				return "", applyErr
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

	if strings.Contains(blockContent, "### Iteration 0 —") {
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
	for i, line := range lines {
		if strings.HasPrefix(line, "### Iteration ") {
			dashIdx := strings.Index(line, " — ")
			if dashIdx < 0 {
				return "", false
			}
			suffix := line[dashIdx+3:]
			lparen := strings.LastIndex(suffix, "(")
			rparen := strings.LastIndex(suffix, ")")
			if lparen < 0 || rparen < 0 || rparen < lparen {
				return "", false
			}
			date := suffix[lparen+1 : rparen]
			title := strings.TrimSpace(suffix[:lparen])

			narrative := strings.Join(lines[i+2:], "\n")
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
		_ = result.Reason
	}
	return nil
}

// applyResumeStateBlocks re-renders the three marker-bounded state-derived
// sub-regions of resume.md (active-tasks, current-state,
// project-history-tail) from filesystem ground truth and atomic-writes the
// result. Returns the rendered file content (so the caller can fingerprint
// it for the metric line) plus any error.
//
// projectRoot, when non-empty and pointing at a Go project (with go.mod),
// is the working directory used for the RUN-counted test enumeration. When
// the project root is empty or has no go.mod, test counts default to zero —
// the renderer still emits the bullet shape, just with N=0/M=0. This keeps
// non-Go consumer projects (rezbldr, vibe-palace, etc.) compatible with
// Step 9.
func applyResumeStateBlocks(cfg config.Config, project, projectRoot string) (string, error) {
	resumeContent, absPath, err := readResume(cfg, project)
	if err != nil {
		return "", err
	}

	tasks, err := collectActiveTasks(cfg, project)
	if err != nil {
		return "", fmt.Errorf("collect active tasks: %w", err)
	}

	state, err := computeCurrentState(cfg, project, projectRoot)
	if err != nil {
		return "", fmt.Errorf("compute current state: %w", err)
	}

	rows, err := collectHistoryRows(cfg, project, 10)
	if err != nil {
		return "", fmt.Errorf("collect history rows: %w", err)
	}

	blocks := map[string]string{
		wraprender.RegionActiveTasks:        wraprender.RenderActiveTasks(tasks),
		wraprender.RegionCurrentState:       wraprender.RenderCurrentState(state),
		wraprender.RegionProjectHistoryTail: wraprender.RenderProjectHistoryTail(rows, 10),
	}

	updated, err := wraprender.ApplyMarkerBlocks(resumeContent, blocks)
	if err != nil {
		return "", fmt.Errorf("apply marker blocks: %w", err)
	}

	if err := mdutil.AtomicWriteFile(absPath, []byte(updated), 0o644); err != nil {
		return "", fmt.Errorf("write resume: %w", err)
	}
	return updated, nil
}

// collectActiveTasks walks Projects/<p>/agentctx/tasks/*.md (excluding
// done/ and cancelled/ subdirs) and returns wraprender-shaped front-matter
// records. Sort order is delegated to the renderer.
func collectActiveTasks(cfg config.Config, project string) ([]wraprender.TaskFrontMatter, error) {
	tasksDir := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "tasks")
	if _, err := vaultPrefixCheck(tasksDir, cfg.VaultPath); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []wraprender.TaskFrontMatter
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		title, status, priority := parseTaskHeader(filepath.Join(tasksDir, e.Name()))
		out = append(out, wraprender.TaskFrontMatter{
			Slug:     slug,
			Title:    title,
			Status:   status,
			Priority: priority,
		})
	}
	return out, nil
}

// computeCurrentState gathers the four headline counts the renderer needs.
// Iteration count comes from a heading scan of iterations.md; MCP-tool
// count from a throw-away Server populated via RegisterAllTools; embedded
// template count from templates.AgentctxFS(); and tests / test-package
// counts from RUN-counted `go test` enumeration in projectRoot. When
// projectRoot is empty or has no go.mod, tests/packages default to zero.
func computeCurrentState(cfg config.Config, project, projectRoot string) (wraprender.CurrentState, error) {
	state := wraprender.CurrentState{}

	iterPath := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "iterations.md")
	if _, err := vaultPrefixCheck(iterPath, cfg.VaultPath); err == nil {
		data, readErr := os.ReadFile(iterPath)
		if readErr == nil {
			state.Iterations = len(scanIterationNumbers(string(data)))
		} else if !os.IsNotExist(readErr) {
			return state, fmt.Errorf("read iterations.md: %w", readErr)
		}
	}

	tools, prompts := countMCPTools(cfg)
	state.MCPTools = tools
	_ = prompts // current renderer hard-codes "+ 1 prompt"

	templateCount, err := countAgentctxTemplates()
	if err != nil {
		return state, fmt.Errorf("count templates: %w", err)
	}
	state.Templates = templateCount

	testCount, packageCount := countRunTests(projectRoot)
	state.Tests = testCount
	state.TestPackages = packageCount

	return state, nil
}

// countMCPTools returns the count of registered tools (and prompts) on a
// throw-away Server. The Server is wired with the canonical
// RegisterAllTools list so the rendered count tracks production reality.
func countMCPTools(cfg config.Config) (tools, prompts int) {
	srv := NewServer(ServerInfo{Name: "vibe-vault", Version: ""}, log.New(io.Discard, "", 0))
	RegisterAllTools(srv, cfg)
	tools = len(srv.ToolNames())
	prompts = len(srv.prompts)
	return tools, prompts
}

// countAgentctxTemplates walks the embedded `templates/agentctx/` FS and
// returns the count of files (excluding directories). Mirrors the
// `Embedded templates: N` headline in the live vibe-vault resume.md.
func countAgentctxTemplates() (int, error) {
	count := 0
	err := fs.WalkDir(templates.AgentctxFS(), ".", func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			count++
		}
		return nil
	})
	return count, err
}

// countRunTests enumerates Go test functions in projectRoot. Uses
// `go test -count=1 -list '.*' ./...` which prints one Test… function name
// per line per package without executing test bodies (~1s on this repo at
// cache-warm; slower on cold builds).
//
// **Plan deviation (R3 follow-up):** the plan locked the headline to
// "RUN-counted" via `go test -run='^$' -v ./...`, but `-run='^$'` skips
// enumeration entirely (zero `=== RUN` lines emit), so that formula
// returns zero. `-list '.*'` returns test-function count (no subtests),
// which is consistent and fast. The headline meaning shifts from
// "RUN-counted" to "test-function-counted"; flagged in Phase 2 reporting
// as a follow-up for the operator to confirm or replace with full
// `-v ./...` if subtests must be counted.
//
// Returns (0, 0) silently when projectRoot is empty, has no go.mod, or
// `go test` errors out — Step 9 is best-effort about counts and must not
// fail the wrap because of a transient toolchain issue. Test-package
// count is the number of distinct package paths whose `-list` output
// contained at least one test name.
func countRunTests(projectRoot string) (tests, packages int) {
	if projectRoot == "" {
		return 0, 0
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "go.mod")); err != nil {
		return 0, 0
	}
	cmd := exec.Command("go", "test", "-count=1", "-list", ".*", "./...")
	cmd.Dir = projectRoot
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	// Best-effort: ignore exit error; partial output still parses.
	_ = cmd.Run()

	pkgSet := make(map[string]struct{})
	var currentPkgHasTests bool
	scanner := bufio.NewScanner(&buf)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Test"):
			// `-list` emits one identifier per line; only Test* counts.
			tests++
			currentPkgHasTests = true
		case strings.HasPrefix(line, "ok  \t") || strings.HasPrefix(line, "ok\t"),
			strings.HasPrefix(line, "FAIL\t") || strings.HasPrefix(line, "FAIL "):
			// Trailing per-package status line. Promote the package to
			// pkgSet iff at least one Test* identifier was listed for it.
			fields := strings.Fields(line)
			if len(fields) >= 2 && currentPkgHasTests {
				pkgSet[fields[1]] = struct{}{}
			}
			currentPkgHasTests = false
		}
	}
	return tests, len(pkgSet)
}

// collectHistoryRows scans Projects/<p>/agentctx/iterations.md and returns
// the last `n` `### Iteration N — Title (YYYY-MM-DD)` headings as
// HistoryRow records. Returns the rows in iteration-ascending order; the
// renderer handles the tail-window slice.
func collectHistoryRows(cfg config.Config, project string, n int) ([]wraprender.HistoryRow, error) {
	iterPath := filepath.Join(cfg.VaultPath, "Projects", project, "agentctx", "iterations.md")
	if _, err := vaultPrefixCheck(iterPath, cfg.VaultPath); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(iterPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	parsed := parseIterations(string(data))
	rows := make([]wraprender.HistoryRow, 0, len(parsed))
	for _, it := range parsed {
		rows = append(rows, wraprender.HistoryRow{
			Iteration: it.Number,
			Date:      it.Date,
			Summary:   summarizeIterationNarrative(it.Narrative),
		})
	}
	// Document-order is iteration-ascending in vibe-vault, but defensively
	// sort by iteration number so out-of-order edits don't break the tail.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Iteration < rows[j].Iteration })

	if n > 0 && len(rows) > n {
		rows = rows[len(rows)-n:]
	}
	return rows, nil
}

// summarizeIterationNarrative extracts a one-line summary from an iteration
// body. Picks the first non-blank paragraph, joins its lines on " ",
// truncates to ~120 characters at a word boundary, and replaces stray
// pipe characters so the summary is safe to drop into a GFM table cell
// (the renderer also escapes pipes; this is defense in depth).
func summarizeIterationNarrative(narr string) string {
	const maxLen = 120
	for _, para := range strings.Split(narr, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		// Join wrapped lines with single spaces.
		lines := strings.Split(para, "\n")
		flat := strings.Join(lines, " ")
		flat = strings.TrimSpace(flat)
		if flat == "" {
			continue
		}
		if len(flat) <= maxLen {
			return flat
		}
		// Truncate at last word boundary before maxLen.
		cut := flat[:maxLen]
		if i := strings.LastIndex(cut, " "); i > 0 {
			cut = cut[:i]
		}
		return strings.TrimRight(cut, " ,;:") + "…"
	}
	return ""
}
