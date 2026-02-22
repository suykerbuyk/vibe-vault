package session

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/johns/sesscap/internal/config"
	"github.com/johns/sesscap/internal/index"
	"github.com/johns/sesscap/internal/render"
	"github.com/johns/sesscap/internal/transcript"
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
