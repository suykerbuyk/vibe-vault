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

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/enrichment"
	"github.com/johns/vibe-vault/internal/friction"
	"github.com/johns/vibe-vault/internal/index"
	"github.com/johns/vibe-vault/internal/knowledge"
	"github.com/johns/vibe-vault/internal/llm"
	"github.com/johns/vibe-vault/internal/narrative"
	"github.com/johns/vibe-vault/internal/prose"
	"github.com/johns/vibe-vault/internal/render"
	"github.com/johns/vibe-vault/internal/stats"
	"github.com/johns/vibe-vault/internal/transcript"
)

// CaptureOpts configures a Capture invocation.
type CaptureOpts struct {
	TranscriptPath string
	CWD            string
	SessionID      string
	Force          bool         // skip dedup, overwrite existing note
	Checkpoint     bool         // provisional capture (Stop hook)
	SkipEnrichment bool         // skip LLM enrichment
	Provider       llm.Provider // LLM provider (nil = heuristic only)
}

// CaptureResult holds the output of a capture operation.
type CaptureResult struct {
	NotePath      string
	Project       string
	Domain        string
	Iteration     int
	Title         string
	Skipped       bool
	Reason        string
	FrictionScore int
	FrictionAlert string
}

// Capture processes a transcript and writes a session note.
func Capture(opts CaptureOpts, cfg config.Config) (*CaptureResult, error) {
	transcriptPath := opts.TranscriptPath
	cwd := opts.CWD
	sessionID := opts.SessionID

	// Parse transcript
	t, err := transcript.ParseFile(transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("parse transcript: %w", err)
	}

	// Skip trivial sessions (< 2 user messages)
	if t.Stats.UserMessages < 2 && t.Stats.AssistantMessages < 2 {
		return &CaptureResult{Skipped: true, Reason: "trivial session (< 2 messages)"}, nil
	}

	// Checkpoint-specific trivial filter: skip if no substantive work yet
	if opts.Checkpoint && t.Stats.ToolUses == 0 && t.Stats.AssistantMessages < 3 {
		return &CaptureResult{Skipped: true, Reason: "checkpoint: no substantive work yet"}, nil
	}

	// Use transcript CWD if hook didn't provide one
	if cwd == "" {
		cwd = t.Stats.CWD
	}
	if sessionID == "" {
		sessionID = t.Stats.SessionID
	}

	// Detect session metadata
	info := Detect(cwd, t.Stats.GitBranch, t.Stats.Model, sessionID, cfg)

	// Acquire index lock to prevent concurrent corruption
	stateDir := cfg.StateDir()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		log.Printf("warning: could not create state dir: %v", err)
	}
	indexLockPath := filepath.Join(stateDir, "session-index.json")
	fl, lockErr := index.Lock(indexLockPath)
	if lockErr != nil {
		log.Printf("warning: could not acquire index lock: %v", lockErr)
	}
	defer func() {
		if fl != nil {
			fl.Unlock()
		}
	}()

	// Load session index
	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		log.Printf("warning: could not load index: %v", err)
		idx = &index.Index{Entries: make(map[string]index.SessionEntry)}
	}

	// Dedup: check existing entry
	existing, exists := idx.Entries[sessionID]
	if !opts.Force && exists && !existing.Checkpoint {
		return &CaptureResult{Skipped: true, Reason: "already processed"}, nil
	}

	// Determine date and iteration
	date := t.Stats.StartTime.Format("2006-01-02")
	if date == "0001-01-01" {
		date = time.Now().Format("2006-01-02")
	}

	// Reuse existing iteration when overwriting to avoid duplicate note files
	var iteration int
	if opts.Force || exists {
		if exists {
			iteration = existing.Iteration
			// Clean up old note if path will change (e.g., project reassignment)
			oldPath := filepath.Join(cfg.VaultPath, existing.NotePath)
			newRelPath := render.NoteRelPath(info.Project, date, existing.Iteration)
			newPath := filepath.Join(cfg.VaultPath, newRelPath)
			if oldPath != newPath {
				os.Remove(oldPath)
			}
		}
	}
	if iteration == 0 {
		iteration = idx.NextIteration(info.Project, date)
	}

	// Find previous session for linking
	var previousNote string
	if prev := idx.PreviousSession(info.Project, t.Stats.StartTime); prev != nil {
		// Use just the filename without extension for wikilink
		previousNote = filenameNoExt(prev.NotePath)
	}

	// Build note data
	noteData := render.NoteDataFromTranscript(t, info.Project, info.Domain, info.Branch, sessionID, iteration, previousNote)

	// Narrative extraction (heuristic enrichment from tool calls)
	narr := narrative.Extract(t, cwd)
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

	// Prose dialogue extraction
	dialogue := prose.Extract(t, cwd)
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
	if !opts.SkipEnrichment && opts.Provider != nil && noteData.ProseDialogue == "" {
		var filesChanged []string
		for f := range t.Stats.FilesWritten {
			filesChanged = append(filesChanged, f)
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
		}
	}

	// Knowledge extraction (separate LLM call, high-signal sessions only)
	if !opts.SkipEnrichment && opts.Provider != nil {
		shouldExtract := (frictionResult != nil && frictionResult.Score >= 30) ||
			len(noteData.Decisions) >= 2
		if shouldExtract {
			pairs := knowledge.PairCorrections(frictionResult, dialogue)

			var kFilesChanged []string
			for f := range t.Stats.FilesWritten {
				kFilesChanged = append(kFilesChanged, f)
			}
			sort.Strings(kFilesChanged)

			kInput := knowledge.ExtractInput{
				UserText:      transcript.UserText(t),
				AssistantText: transcript.AssistantText(t),
				Corrections:   pairs,
				Decisions:     noteData.Decisions,
				FrictionScore: frictionScore(frictionResult),
				Project:       info.Project,
				FilesChanged:  kFilesChanged,
			}

			kTimeout := time.Duration(cfg.Enrichment.TimeoutSeconds) * time.Second
			if kTimeout == 0 {
				kTimeout = 10 * time.Second
			}
			kCtx, kCancel := context.WithTimeout(context.Background(), kTimeout)
			defer kCancel()

			sessionNoteName := filenameNoExt(render.NoteFilename(date, iteration))

			notes, kErr := knowledge.Enrich(kCtx, opts.Provider, kInput)
			if kErr != nil {
				log.Printf("warning: knowledge extraction failed: %v", kErr)
			}
			for _, note := range notes {
				if note.Confidence < 0.7 {
					continue
				}
				note.Date = date
				note.Project = info.Project
				note.SourceSession = sessionNoteName
				kPath, wErr := knowledge.WriteNote(cfg.VaultPath, note)
				if wErr != nil {
					log.Printf("warning: write knowledge note: %v", wErr)
					continue
				}
				noteData.KnowledgeNotes = append(noteData.KnowledgeNotes, kPath)
			}
		}
	}

	// Compute related sessions
	relPath := render.NoteRelPath(info.Project, date, iteration)
	candidateEntry := index.SessionEntry{
		SessionID:    sessionID,
		Project:      info.Project,
		Domain:       info.Domain,
		Date:         date,
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

	// Render markdown
	markdown := render.SessionNote(noteData)

	// Write note file
	absPath := filepath.Join(cfg.VaultPath, relPath)

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}

	if err := os.WriteFile(absPath, []byte(markdown), 0o644); err != nil {
		return nil, fmt.Errorf("write note: %w", err)
	}

	// Update index
	idx.Add(index.SessionEntry{
		SessionID:      sessionID,
		NotePath:       relPath,
		Project:        info.Project,
		Domain:         info.Domain,
		Date:           date,
		Iteration:      iteration,
		Title:          noteData.Title,
		Model:          info.Model,
		Duration:       int(t.Stats.Duration.Minutes()),
		CreatedAt:      time.Now(),
		Summary:        noteData.Summary,
		Decisions:      noteData.Decisions,
		OpenThreads:    noteData.OpenThreads,
		Tag:            noteData.Tag,
		FilesChanged:   noteData.FilesChanged,
		Commits:        commitSHAs(commits),
		Branch:         info.Branch,
		TranscriptPath: transcriptPath,
		Checkpoint:     opts.Checkpoint,
		ToolCounts:     t.Stats.ToolCounts,
		ToolUses:       t.Stats.ToolUses,
		TokensIn:       noteData.InputTokens,
		TokensOut:      noteData.OutputTokens,
		Messages:       noteData.Messages,
		Corrections:    frictionCorrections(frictionResult),
		FrictionScore:  frictionScore(frictionResult),
		KnowledgeNotes:   noteData.KnowledgeNotes,
		EstimatedCostUSD: noteData.EstimatedCostUSD,
	})

	if err := idx.Save(); err != nil {
		log.Printf("warning: could not save index: %v", err)
	}

	return &CaptureResult{
		NotePath:      relPath,
		Project:       info.Project,
		Domain:        info.Domain,
		Iteration:     iteration,
		Title:         noteData.Title,
		FrictionScore: frictionScore(frictionResult),
		FrictionAlert: frictionAlert,
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

func filenameNoExt(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		return base[:len(base)-len(ext)]
	}
	return base
}
