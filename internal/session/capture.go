package session

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/johns/vibe-vault/internal/config"
	"github.com/johns/vibe-vault/internal/enrichment"
	"github.com/johns/vibe-vault/internal/index"
	"github.com/johns/vibe-vault/internal/render"
	"github.com/johns/vibe-vault/internal/transcript"
)

// CaptureResult holds the output of a capture operation.
type CaptureResult struct {
	NotePath  string
	Project   string
	Domain    string
	Iteration int
	Title     string
	Skipped   bool
	Reason    string
}

// Capture processes a transcript and writes a session note.
func Capture(transcriptPath string, cwd string, sessionID string, cfg config.Config) (*CaptureResult, error) {
	// Parse transcript
	t, err := transcript.ParseFile(transcriptPath)
	if err != nil {
		return nil, fmt.Errorf("parse transcript: %w", err)
	}

	// Skip trivial sessions (< 2 user messages)
	if t.Stats.UserMessages < 2 && t.Stats.AssistantMessages < 2 {
		return &CaptureResult{Skipped: true, Reason: "trivial session (< 2 messages)"}, nil
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

	// Load session index
	idx, err := index.Load(cfg.StateDir())
	if err != nil {
		log.Printf("warning: could not load index: %v", err)
		idx = &index.Index{Entries: make(map[string]index.SessionEntry)}
	}

	// Skip if already processed
	if idx.Has(sessionID) {
		return &CaptureResult{Skipped: true, Reason: "already processed"}, nil
	}

	// Determine date and iteration
	date := t.Stats.StartTime.Format("2006-01-02")
	if date == "0001-01-01" {
		date = time.Now().Format("2006-01-02")
	}
	iteration := idx.NextIteration(info.Project, date)

	// Find previous session for linking
	var previousNote string
	if prev := idx.PreviousSession(info.Project, t.Stats.StartTime); prev != nil {
		// Use just the filename without extension for wikilink
		previousNote = filenameNoExt(prev.NotePath)
	}

	// Build note data
	noteData := render.NoteDataFromTranscript(t, info.Project, info.Domain, info.Branch, sessionID, iteration, previousNote)

	// LLM enrichment (graceful: skip on error or if disabled)
	var filesChanged []string
	for f := range t.Stats.FilesWritten {
		filesChanged = append(filesChanged, f)
	}
	sort.Strings(filesChanged)

	enrichInput := enrichment.PromptInput{
		UserText:     transcript.UserText(t),
		AssistantText: transcript.AssistantText(t),
		FilesChanged:  filesChanged,
		ToolCounts:    t.Stats.ToolCounts,
		Duration:      int(t.Stats.Duration.Minutes()),
		UserMessages:  t.Stats.UserMessages,
		AsstMessages:  t.Stats.AssistantMessages,
	}

	timeout := time.Duration(cfg.Enrichment.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	enrichCtx, enrichCancel := context.WithTimeout(context.Background(), timeout)
	defer enrichCancel()

	enrichResult, enrichErr := enrichment.Generate(enrichCtx, cfg.Enrichment, enrichInput)
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

	// Render markdown
	markdown := render.SessionNote(noteData)

	// Write note file
	relPath := render.NoteRelPath(info.Project, date, iteration)
	absPath := filepath.Join(cfg.VaultPath, relPath)

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("create session dir: %w", err)
	}

	if err := os.WriteFile(absPath, []byte(markdown), 0o644); err != nil {
		return nil, fmt.Errorf("write note: %w", err)
	}

	// Update index
	idx.Add(index.SessionEntry{
		SessionID: sessionID,
		NotePath:  relPath,
		Project:   info.Project,
		Domain:    info.Domain,
		Date:      date,
		Iteration: iteration,
		Title:     noteData.Title,
		Model:     info.Model,
		Duration:  int(t.Stats.Duration.Minutes()),
		CreatedAt: time.Now(),
	})

	if err := idx.Save(); err != nil {
		log.Printf("warning: could not save index: %v", err)
	}

	return &CaptureResult{
		NotePath:  relPath,
		Project:   info.Project,
		Domain:    info.Domain,
		Iteration: iteration,
		Title:     noteData.Title,
	}, nil
}

func filenameNoExt(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		return base[:len(base)-len(ext)]
	}
	return base
}
