// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package session

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/suykerbuyk/vibe-vault/internal/atomicfile"
	"github.com/suykerbuyk/vibe-vault/internal/config"
	"github.com/suykerbuyk/vibe-vault/internal/enrichment"
	"github.com/suykerbuyk/vibe-vault/internal/friction"
	"github.com/suykerbuyk/vibe-vault/internal/index"
	"github.com/suykerbuyk/vibe-vault/internal/llm"
	"github.com/suykerbuyk/vibe-vault/internal/lockfile"
	"github.com/suykerbuyk/vibe-vault/internal/meta"
	"github.com/suykerbuyk/vibe-vault/internal/narrative"
	"github.com/suykerbuyk/vibe-vault/internal/prose"
	"github.com/suykerbuyk/vibe-vault/internal/render"
	"github.com/suykerbuyk/vibe-vault/internal/sanitize"
	"github.com/suykerbuyk/vibe-vault/internal/staging"
	"github.com/suykerbuyk/vibe-vault/internal/stats"
	"github.com/suykerbuyk/vibe-vault/internal/transcript"
)

// updateHarnessSessionID is the function the Capture wrapper calls to
// reconcile the harness-supplied session id into the cache file.
//
// Wired up by sessionclaim.init() to sessionclaim.UpdateHarnessSessionID
// — kept as a package-level variable here to break the import cycle
// (sessionclaim imports session for DetectProjectRoot). Tests may
// override this seam to assert the call site.
//
// Default no-op so that unit tests of the session package that never
// import sessionclaim still build and run cleanly.
var updateHarnessSessionID = func(projectRoot, harnessID string) error { return nil }

// SetUpdateHarnessSessionID wires the sessionclaim integration. Called
// from sessionclaim.init() to register the real implementation. Phase 4
// of session-slot-multihost-disambiguation.
func SetUpdateHarnessSessionID(fn func(projectRoot, harnessID string) error) {
	if fn == nil {
		updateHarnessSessionID = func(string, string) error { return nil }
		return
	}
	updateHarnessSessionID = fn
}

// CaptureOpts configures a Capture invocation.
type CaptureOpts struct {
	TranscriptPath string
	CWD            string
	SessionID      string
	Source         string       // source identifier (e.g. "zed"); empty = "claude-code"
	Force          bool         // skip dedup, overwrite existing note
	Checkpoint     bool         // provisional capture (Stop hook)
	SkipEnrichment bool         // skip LLM enrichment
	Provider       llm.Provider // LLM provider (nil = heuristic only)
	Index          *index.Index // shared index for batch operations (nil = load/save per call)
	AutoCaptured   bool         // mark note as auto-captured (lower confidence)
	// ProjectRoot is the absolute project-root path; resolved by caller via
	// session.DetectProjectRoot. Used by CaptureFromParsed for sessionclaim
	// integration (M8 architectural cleanup, Phase 4 of
	// session-slot-multihost-disambiguation). Empty falls back to legacy
	// behavior — no UpdateHarnessSessionID call.
	ProjectRoot string
	// StagingRoot, when non-empty, routes the session-note write into the
	// host-local staging dir at <StagingRoot>/<project>/<filename>
	// instead of the shared vault's flat-layout
	// Projects/<p>/sessions/<filename>. Phase 2 of
	// vault-two-tier-narrative-vs-sessions-split centralized this routing
	// decision here so all five session-write entry points (hook,
	// `vv process`, `vv backfill`, `vv reprocess`, Zed batch, MCP
	// `vv_capture_session`) honor the same rule. Empty preserves legacy
	// flat-vault behavior — back-compat surface for tests and
	// unconfigured environments.
	//
	// When non-empty, CaptureFromParsed also invokes staging.Commit
	// post-write. A staging-commit failure is logged WARN and never
	// propagates: the markdown write itself succeeded and the next
	// successful fire's commit will pick up the untracked file.
	StagingRoot string
}

// CaptureResult holds the output of a capture operation.
type CaptureResult struct {
	NotePath            string
	Project             string
	Domain              string
	Iteration           int
	Title               string
	Skipped             bool
	Reason              string
	FrictionScore       int
	FrictionAlert       string
	EnrichmentAttempted bool // true if the enrichment LLM call was made (regardless of outcome)
	EnrichmentApplied   bool // true when enrichment returned usable content and populated note fields
}

// Capture processes a transcript and writes a session note.
// This is the Claude Code entry point — it parses, detects, extracts, then delegates to CaptureFromParsed.
func Capture(opts CaptureOpts, cfg config.Config) (*CaptureResult, error) {
	transcriptPath := opts.TranscriptPath
	cwd := opts.CWD
	sessionID := opts.SessionID

	// Parse transcript
	t, err := transcript.ParseFile(transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("parse transcript: %w", err)
	}

	// Use transcript CWD if hook didn't provide one
	if cwd == "" {
		cwd = t.Stats.CWD
	}
	if sessionID == "" {
		sessionID = t.Stats.SessionID
	}

	// Resolve project root if caller didn't provide one (hook handlers may
	// have already done this, MCP path always does it before Capture).
	// Phase 4 / M8 of session-slot-multihost-disambiguation.
	if opts.ProjectRoot == "" && cwd != "" {
		opts.ProjectRoot = DetectProjectRoot(cwd)
	}

	// Detect session metadata
	info := Detect(cwd, t.Stats.GitBranch, t.Stats.Model, sessionID, cfg)

	// Reconcile harness session id into the cache file (M8 — eliminates the
	// order dance: callers that provide opts.SessionID from the harness JSON
	// get their id stamped into the claim regardless of whether MCP fired
	// first or hook fired first). UpdateHarnessSessionID self-bootstraps via
	// AcquireOrRefresh (H5), so a fresh hook-only short session still gets
	// a valid claim file. Errors are logged warnings, never propagated.
	if opts.ProjectRoot != "" && sessionID != "" {
		if updateErr := updateHarnessSessionID(opts.ProjectRoot, sessionID); updateErr != nil {
			log.Printf("warning: sessionclaim.UpdateHarnessSessionID: %v", updateErr)
		}
	}

	// Narrative extraction (heuristic enrichment from tool calls)
	narr := narrative.Extract(t, cwd)

	// Prose dialogue extraction
	dialogue := prose.Extract(t, cwd)

	opts.CWD = cwd
	opts.SessionID = sessionID

	return CaptureFromParsed(t, info, narr, dialogue, opts, cfg)
}

// CaptureFromParsed is the shared pipeline for all sources (Claude Code, Zed, etc.).
// It filters, deduplicates, enriches, renders, and writes a session note.
func CaptureFromParsed(t *transcript.Transcript, info Info,
	narr *narrative.Narrative, dialogue *prose.Dialogue,
	opts CaptureOpts, cfg config.Config) (*CaptureResult, error) {

	sessionID := info.SessionID
	if sessionID == "" {
		sessionID = opts.SessionID
	}
	transcriptPath := opts.TranscriptPath

	// Skip trivial sessions (< 2 user messages)
	if t.Stats.UserMessages < 2 && t.Stats.AssistantMessages < 2 {
		return &CaptureResult{Skipped: true, Reason: "trivial session (< 2 messages)"}, nil
	}

	// Checkpoint-specific trivial filter: skip if no substantive work yet
	if opts.Checkpoint && t.Stats.ToolUses == 0 && t.Stats.AssistantMessages < 3 {
		return &CaptureResult{Skipped: true, Reason: "checkpoint: no substantive work yet"}, nil
	}

	// Apply per-project config overlay (project-local settings override global)
	cfg = cfg.WithProjectOverlay(info.Project)

	// Index management: use shared index if provided, otherwise load/lock per call
	var idx *index.Index
	var fl *lockfile.Lockfile
	if opts.Index != nil {
		idx = opts.Index
	} else {
		// Acquire index lock to prevent concurrent corruption
		stateDir := cfg.StateDir()
		if mkdirErr := os.MkdirAll(stateDir, 0o755); mkdirErr != nil {
			log.Printf("warning: could not create state dir: %v", mkdirErr)
		}
		indexLockPath := filepath.Join(stateDir, "session-index.json") + ".lock"
		var lockErr error
		fl, lockErr = lockfile.Acquire(indexLockPath)
		if lockErr != nil {
			log.Printf("warning: could not acquire index lock: %v", lockErr)
		}
		defer func() {
			if fl != nil {
				_ = fl.Release()
			}
		}()

		var err error
		idx, err = index.Load(cfg.StateDir())
		if err != nil {
			log.Printf("warning: could not load index: %v", err)
			idx = &index.Index{Entries: make(map[string]index.SessionEntry)}
		}
	}

	// Dedup: check existing entry
	existing, exists := idx.Entries[sessionID]
	if !opts.Force && exists && !existing.Checkpoint {
		return &CaptureResult{Skipped: true, Reason: "already processed"}, nil
	}

	// Single clock source (Mechanism 1, Phase 4 of
	// session-slot-multihost-disambiguation): one `now` drives the
	// filename's date prefix and timestamp body. The frontmatter `date`
	// field continues to reflect t.Stats.StartTime (the session's
	// wall-clock identity, not the write moment); only the filename uses
	// `now`. The two converge except across midnight, where the filename
	// reflects the write time and the frontmatter the session start.
	now := time.Now()
	date := now.Format("2006-01-02")
	frontmatterDate := t.Stats.StartTime.Format("2006-01-02")
	if frontmatterDate == "0001-01-01" {
		frontmatterDate = date
	}

	// Mechanism 3: same-session re-write via existing NotePath field.
	// If the index already knows this session AND its recorded project
	// matches, capture its prior NotePath so we can remove the stale
	// file after we've stat'd the new candidate path. Project changes
	// (e.g., reprocessing moves a session from "_unknown" to a detected
	// project) are treated as fresh writes — no removal of the prior
	// note (it belongs to a different conceptual entry).
	var prevPath string
	if exists && existing.Project == info.Project && existing.NotePath != "" {
		// Phase 2: staging-routed entries record an absolute path in
		// NotePath (the staging dir is host-local, not vault-relative).
		// Vault-routed entries keep the historical vault-relative form.
		// IsAbs disambiguates without a separate flag.
		if filepath.IsAbs(existing.NotePath) {
			prevPath = existing.NotePath
		} else {
			prevPath = filepath.Join(cfg.VaultPath, existing.NotePath)
		}
	}

	// Iteration is now purely a frontmatter field (filenames use
	// timestamps, not iteration counters). Compute as
	// "today's entries for this project + 1" so it remains a useful
	// human-readable ordinal in vault frontmatter. Phase 5's index
	// rebuild reads this back from frontmatter authoritatively.
	iteration := countTodayEntries(idx, info.Project, frontmatterDate) + 1
	if exists && existing.Project == info.Project && existing.Iteration > 0 {
		// Same-session re-write: preserve the prior iteration ordinal so
		// the user-visible counter doesn't drift on idempotent recapture.
		iteration = existing.Iteration
	}

	// Find previous session for linking
	var previousNote string
	if prev := idx.PreviousSession(info.Project, t.Stats.StartTime); prev != nil {
		// Use just the filename without extension for wikilink
		previousNote = filenameNoExt(prev.NotePath)
	}

	// Build note data
	noteData := render.NoteDataFromTranscript(t, info.Project, info.Domain, info.Branch, sessionID, iteration, previousNote)
	noteData.Source = opts.Source
	p := meta.Stamp()
	noteData.Host = p.Host
	noteData.User = p.User
	noteData.CWD = meta.SanitizeCWDForEmit(p.CWD, cfg.VaultPath)
	if noteData.CWD != "" {
		noteData.OriginProject = DetectProject(p.CWD)
	}

	// Fix source-aware fallback summary (e.g. "Zed agent session" instead of "Claude Code session")
	if opts.Source != "" && noteData.Summary == "Claude Code session" {
		noteData.Summary = render.SourceFallbackSummary(opts.Source)
	}

	// Session continuity: propagate ParentUUID if this is a /continue session
	if t.Stats.ParentUUID != "" {
		noteData.ParentSession = t.Stats.ParentUUID
	}

	// Apply narrative data
	if narr != nil {
		if narr.Title != "" && narr.Title != "Session" {
			noteData.Title = narr.Title
		}
		if narr.Summary != "" {
			noteData.Summary = narr.Summary
		}
		if narr.Tag != "" {
			noteData.Tag = narr.Tag
		}
		noteData.Decisions = narr.Decisions
		noteData.OpenThreads = narr.OpenThreads
		noteData.WorkPerformed = narr.WorkPerformed

		// Timeline (Phase 5 Task 21)
		noteData.Timeline = narrative.RenderTimeline(narr.Segments)
	}

	// Reasoning highlights from thinking blocks (Phase 4 Task 15)
	noteData.ReasoningHighlights = narrative.ExtractReasoningHighlights(t)

	// Apply prose dialogue
	if dialogue != nil {
		noteData.ProseDialogue = prose.Render(dialogue)
	}

	// Git commit extraction (from narrative, which runs ExtractCommits internally)
	var commits []narrative.Commit
	if narr != nil && len(narr.Commits) > 0 {
		commits = narr.Commits
		noteData.Commits = commits
	} else {
		// Fallback: extract commits directly if narrative was nil
		commits = narrative.ExtractCommits(t.Entries)
		if len(commits) > 0 {
			noteData.Commits = commits
		}
	}

	// Friction analysis
	var frictionResult *friction.Result
	if dialogue != nil || narr != nil {
		var priorThreads []string
		if prev := idx.PreviousSession(info.Project, t.Stats.StartTime); prev != nil {
			priorThreads = prev.OpenThreads
		}
		frictionResult = friction.Analyze(dialogue, narr, t.Stats, priorThreads)
	}
	var frictionAlert string
	if frictionResult != nil {
		noteData.FrictionScore = frictionResult.Score
		noteData.Corrections = frictionResult.Signals.Corrections
		noteData.FrictionSignals = frictionResult.Summary

		if cfg.Friction.AlertThreshold > 0 && frictionResult.Score >= cfg.Friction.AlertThreshold {
			top := friction.TopContributors(frictionResult.Signals, 2)
			parts := make([]string, len(top))
			for i, c := range top {
				parts[i] = fmt.Sprintf("%s: %.0f", c.Name, c.Weight)
			}
			frictionAlert = fmt.Sprintf("\u26a0 friction %d \u2014 %s", frictionResult.Score, strings.Join(parts, ", "))
		}
	}

	// Mark checkpoint status
	if opts.Checkpoint {
		noteData.Status = "checkpoint"
	}

	// Mark auto-captured status (lower confidence than explicit captures)
	if opts.AutoCaptured {
		noteData.Status = "auto-captured"
	}

	// Tool effectiveness analysis (Task 20)
	if narr != nil {
		te := stats.AnalyzeTools(narr.Segments)
		noteData.ToolEffectiveness = stats.RenderToolEffectiveness(te)
	}

	// Cost estimation
	if cfg.Pricing.Enabled {
		noteData.EstimatedCostUSD = stats.EstimateCost(cfg.Pricing, stats.CostInput{
			Model:        t.Stats.Model,
			InputTokens:  t.Stats.InputTokens,
			OutputTokens: t.Stats.OutputTokens,
			CacheReads:   t.Stats.CacheReads,
			CacheWrites:  t.Stats.CacheWrites,
		})
	}

	// LLM enrichment (graceful: skip on error or if disabled)
	// Skip when prose extraction produced output — the prose subsumes enrichment's purpose.
	var enrichmentAttempted, enrichmentApplied bool
	if !opts.SkipEnrichment && opts.Provider != nil && noteData.ProseDialogue == "" {
		enrichmentAttempted = true
		var filesChanged []string
		for f := range t.Stats.FilesWritten {
			filesChanged = append(filesChanged, sanitize.CompressHome(f))
		}
		sort.Strings(filesChanged)

		enrichInput := enrichment.PromptInput{
			UserText:      transcript.UserText(t),
			AssistantText: transcript.AssistantText(t),
			FilesChanged:  filesChanged,
			ToolCounts:    t.Stats.ToolCounts,
			Duration:      int(t.Stats.Duration.Minutes()),
			UserMessages:  t.Stats.UserMessages,
			AsstMessages:  t.Stats.AssistantMessages,
		}

		// Pass narrative context to enrichment for refinement
		if narr != nil {
			enrichInput.NarrativeSummary = narr.Summary
			enrichInput.NarrativeTag = narr.Tag
			for _, seg := range narr.Segments {
				for _, a := range seg.Activities {
					enrichInput.Activities = append(enrichInput.Activities, a.Description)
				}
			}
		}

		timeout := time.Duration(cfg.Enrichment.TimeoutSeconds) * time.Second
		if timeout == 0 {
			timeout = 10 * time.Second
		}
		enrichCtx, enrichCancel := context.WithTimeout(context.Background(), timeout)
		defer enrichCancel()

		enrichResult, enrichErr := enrichment.Generate(enrichCtx, opts.Provider, enrichInput)
		if enrichErr != nil {
			log.Printf("warning: enrichment failed: %v", enrichErr)
		}
		if enrichResult != nil {
			if enrichResult.Summary != "" {
				noteData.Summary = enrichResult.Summary
			}
			noteData.Decisions = enrichResult.Decisions
			noteData.OpenThreads = enrichResult.OpenThreads
			noteData.Tag = enrichResult.Tag
			noteData.EnrichedBy = cfg.Enrichment.Model
			enrichmentApplied = true
		}
	}

	// Phase 2: lazy staging-repo bootstrap. Two-stat fast path when the
	// project's staging dir is already initialized (~20µs); falls back
	// to in-process Init when the sentinel or .git/HEAD is missing.
	// Failures are logged WARN — the markdown write proceeds, and the
	// staging.Commit attempt below either succeeds (on partial init) or
	// logs WARN itself (fail-safe — the hook never errors out due to a
	// staging git failure).
	//
	// EnsureInitAt pins the init target to opts.StagingRoot so a
	// cfg.Staging.Root override flows through; without this, EnsureInit
	// would re-resolve via Root() (XDG default) and bootstrap the wrong
	// dir while the write went to the cfg path.
	if opts.StagingRoot != "" {
		if initErr := staging.EnsureInitAt(opts.StagingRoot, info.Project); initErr != nil {
			log.Printf("warning: staging EnsureInitAt(%s, %s): %v", opts.StagingRoot, info.Project, initErr)
		}
	}

	// Mechanism 1 (Phase 4): timestamp filename with sub-millisecond
	// collision-retry suffix. Stat each candidate; on collision (file
	// exists), bump suffix 1..9; fail loudly after 10 exhausted attempts
	// rather than silently overwriting a peer note.
	//
	// Phase 2 routing: opts.StagingRoot non-empty routes the write into
	// the host-local staging dir (absolute path, no vault prefix);
	// empty preserves the legacy flat-vault layout under
	// Projects/<p>/sessions/<filename>. Both branches share the same
	// collision-retry loop and post-write commit semantics.
	var (
		relPath string
		absPath string
	)
	for suffix := 0; suffix <= 9; suffix++ {
		if opts.StagingRoot != "" {
			absPath = staging.NotePath(opts.StagingRoot, info.Project, date, now, suffix)
			// For staging routing the index records the absolute path
			// (the staging dir lives outside the vault, so the
			// historical vault-relative convention does not apply).
			relPath = absPath
		} else {
			// Phase 1.5 back-compat: empty host → legacy flat layout
			// under <vault>/Projects/<p>/sessions/<filename>. The
			// helper's empty-host branch is the explicit back-compat
			// surface used by tests and unconfigured environments.
			relPath = render.NoteRelPathTimestamp(info.Project, "", date, now, suffix)
			absPath = filepath.Join(cfg.VaultPath, relPath)
		}
		// Same-session re-write (Mechanism 3): if the prior path is the
		// same as our candidate (rare, only on identical clock), allow
		// overwrite — the prevPath removal at the bottom would otherwise
		// nuke our own freshly-written file.
		if prevPath != "" && prevPath == absPath {
			break
		}
		if _, statErr := os.Stat(absPath); os.IsNotExist(statErr) {
			break // path is free
		} else if statErr != nil {
			return nil, fmt.Errorf("stat candidate path: %w", statErr)
		}
		if suffix == 9 {
			return nil, fmt.Errorf("session-note slot collision: 10 retries exhausted at %s", relPath)
		}
	}

	candidateEntry := index.SessionEntry{
		SessionID:    sessionID,
		Project:      info.Project,
		Domain:       info.Domain,
		Date:         frontmatterDate,
		Iteration:    iteration,
		Title:        noteData.Title,
		Summary:      noteData.Summary,
		Decisions:    noteData.Decisions,
		OpenThreads:  noteData.OpenThreads,
		Tag:          noteData.Tag,
		FilesChanged: noteData.FilesChanged,
		Branch:       info.Branch,
	}

	var previousNotePath string
	if prev := idx.PreviousSession(info.Project, t.Stats.StartTime); prev != nil {
		previousNotePath = prev.NotePath
	}

	related := idx.RelatedSessions(candidateEntry, previousNotePath)
	for _, r := range related {
		noteName := filenameNoExt(r.Entry.NotePath)
		noteData.RelatedNotes = append(noteData.RelatedNotes, render.RelatedNote{
			Name:   noteName,
			Reason: describeRelation(candidateEntry, r.Entry),
		})
	}

	// Build session tags from config
	noteData.SessionTags = cfg.SessionTags(noteData.Tag)

	// Render markdown
	markdown := render.SessionNote(noteData)

	// Mechanism 3: remove prior session note before writing the new one
	// (only when the prior path differs from the candidate path, which
	// is the common case — same-clock-tick rewrites of the identical
	// path fall through to atomicfile.Write's overwrite semantics).
	if prevPath != "" && prevPath != absPath {
		if removeErr := os.Remove(prevPath); removeErr != nil && !os.IsNotExist(removeErr) {
			log.Printf("warning: could not remove prior session note %s: %v", prevPath, removeErr)
		}
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}

	// Phase 2: staging writes pass vaultPath="" so atomicfile.Write
	// does not stamp a .surface side-channel under the vault tree (the
	// staging dir lives outside the vault). Vault writes keep the
	// existing stamp-on-success behavior.
	stampPath := cfg.VaultPath
	if opts.StagingRoot != "" {
		stampPath = ""
	}
	if err := atomicfile.Write(stampPath, absPath, []byte(markdown)); err != nil {
		return nil, fmt.Errorf("write note: %w", err)
	}

	// Phase 2: post-write staging-repo commit. Fail-safe — a commit
	// failure (identity, lock, disk full, etc.) leaves the markdown
	// file on disk and surfaces only as a WARN log; the next
	// successful fire's commit picks up the untracked file.
	if opts.StagingRoot != "" {
		stagingDir := filepath.Join(opts.StagingRoot, info.Project)
		commitMsg := fmt.Sprintf("session: %s/%s", info.Project, filepath.Base(absPath))
		if commitErr := staging.Commit(stagingDir, absPath, commitMsg); commitErr != nil {
			log.Printf("warning: staging commit failed for %s: %v", absPath, commitErr)
		}
	}

	// Probe context availability for effectiveness measurement
	ctxAvail := probeContextAvailable(cfg.VaultPath, info.Project, idx)

	// Update index
	idx.Add(index.SessionEntry{
		SessionID:        sessionID,
		NotePath:         relPath,
		Project:          info.Project,
		Domain:           info.Domain,
		Date:             frontmatterDate,
		Iteration:        iteration,
		Title:            noteData.Title,
		Model:            info.Model,
		Duration:         int(t.Stats.Duration.Minutes()),
		CreatedAt:        time.Now(),
		Summary:          noteData.Summary,
		Decisions:        noteData.Decisions,
		OpenThreads:      noteData.OpenThreads,
		Tag:              noteData.Tag,
		FilesChanged:     noteData.FilesChanged,
		Commits:          commitSHAs(commits),
		Branch:           info.Branch,
		TranscriptPath:   transcriptPath,
		Checkpoint:       opts.Checkpoint,
		Source:           opts.Source,
		ToolCounts:       t.Stats.ToolCounts,
		ToolUses:         t.Stats.ToolUses,
		TokensIn:         noteData.InputTokens,
		TokensOut:        noteData.OutputTokens,
		Messages:         noteData.Messages,
		Corrections:      frictionCorrections(frictionResult),
		FrictionScore:    frictionScore(frictionResult),
		EstimatedCostUSD: noteData.EstimatedCostUSD,
		ParentUUID:       t.Stats.ParentUUID,
		Context:          ctxAvail,
	})

	// Save index only if we own it (not shared batch mode)
	if opts.Index == nil {
		if err := idx.Save(); err != nil {
			log.Printf("warning: could not save index: %v", err)
		}
	}

	return &CaptureResult{
		NotePath:            relPath,
		Project:             info.Project,
		Domain:              info.Domain,
		Iteration:           iteration,
		Title:               noteData.Title,
		FrictionScore:       frictionScore(frictionResult),
		FrictionAlert:       frictionAlert,
		EnrichmentAttempted: enrichmentAttempted,
		EnrichmentApplied:   enrichmentApplied,
	}, nil
}

// describeRelation builds a human-readable reason for why two sessions are related.
func describeRelation(a, b index.SessionEntry) string {
	var parts []string

	// Shared files
	shared := sharedFiles(a.FilesChanged, b.FilesChanged)
	if shared > 0 {
		parts = append(parts, fmt.Sprintf("%d shared files", shared))
	}

	// Same branch
	if a.Branch != "" && a.Branch == b.Branch && a.Branch != "main" && a.Branch != "master" {
		parts = append(parts, fmt.Sprintf("branch: %s", a.Branch))
	}

	// Same tag
	if a.Tag != "" && a.Tag == b.Tag {
		parts = append(parts, fmt.Sprintf("tag: %s", a.Tag))
	}

	if len(parts) == 0 {
		parts = append(parts, "related work")
	}

	return strings.Join(parts, ", ")
}

func sharedFiles(a, b []string) int {
	set := make(map[string]bool, len(a))
	for _, f := range a {
		set[f] = true
	}
	count := 0
	for _, f := range b {
		if set[f] {
			count++
		}
	}
	return count
}

func commitSHAs(commits []narrative.Commit) []string {
	if len(commits) == 0 {
		return nil
	}
	shas := make([]string, len(commits))
	for i, c := range commits {
		shas[i] = c.SHA
	}
	return shas
}

func frictionCorrections(r *friction.Result) int {
	if r == nil {
		return 0
	}
	return r.Signals.Corrections
}

func frictionScore(r *friction.Result) int {
	if r == nil {
		return 0
	}
	return r.Score
}

// countTodayEntries returns the number of entries in idx for the given
// (project, date) pair. Used for the iteration-counter frontmatter
// field at write time. Mechanism 1 / Phase 4 of session-slot-
// multihost-disambiguation: filenames no longer encode the iteration
// counter, but frontmatter still does for backwards compat with read
// paths that look up via (project, date, iteration). Index rebuild
// (Phase 5) reads iteration from frontmatter authoritatively.
func countTodayEntries(idx *index.Index, project, date string) int {
	count := 0
	for _, e := range idx.Entries {
		if e.Project == project && e.Date == date {
			count++
		}
	}
	return count
}

func filenameNoExt(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		return base[:len(base)-len(ext)]
	}
	return base
}

// probeContextAvailable checks what project context existed at capture time.
// Returns nil if nothing is available (keeps JSON clean for projects with no context).
func probeContextAvailable(vaultPath, project string, idx *index.Index) *index.ContextAvailable {
	projDir := filepath.Join(vaultPath, "Projects", project)

	hasHistory := fileExists(filepath.Join(projDir, "history.md"))
	hasKnowledge := fileNonEmpty(filepath.Join(projDir, "knowledge.md"))
	historySessions := idx.ProjectSessionCount(project)

	if !hasHistory && !hasKnowledge && historySessions == 0 {
		return nil
	}

	return &index.ContextAvailable{
		HasHistory:      hasHistory,
		HasKnowledge:    hasKnowledge,
		HistorySessions: historySessions,
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Size() > 0
}
