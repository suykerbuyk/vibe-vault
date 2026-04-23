// Package synthesis implements the end-of-session judgment layer that
// propagates learnings, flags stale context, and updates project documents.
package synthesis

import "github.com/suykerbuyk/vibe-vault/internal/noteparse"

// Input holds everything the synthesis agent needs to make judgments.
type Input struct {
	SessionNote   *noteparse.Note
	GitDiff       string
	KnowledgeMD   string
	ResumeMD      string
	RecentHistory []HistoryEntry
	TaskSummaries []TaskSummary
}

// HistoryEntry is a recent session summary for context.
type HistoryEntry struct {
	Date    string
	Title   string
	Summary string
	Tag     string
}

// TaskSummary is an active task's name and status.
type TaskSummary struct {
	Name   string
	Title  string
	Status string
}

// Result is the structured output from the LLM.
type Result struct {
	Learnings    []Learning    `json:"learnings"`
	StaleEntries []StaleEntry  `json:"stale_entries"`
	ResumeUpdate *ResumeUpdate `json:"resume_update"`
	TaskUpdates  []TaskUpdate  `json:"task_updates"`
	Reasoning    string        `json:"reasoning"`
}

// Learning is a new entry to append to knowledge.md.
type Learning struct {
	Section string `json:"section"` // "Decisions", "Patterns", or "Learnings"
	Entry   string `json:"entry"`
}

// StaleEntry identifies an entry that is no longer accurate.
type StaleEntry struct {
	File    string `json:"file"`    // "knowledge.md" or "resume.md"
	Section string `json:"section"`
	Index   int    `json:"index"` // 0-based bullet index within section
	Entry   string `json:"entry"` // approximate text (fallback matching)
	Reason  string `json:"reason"`
}

// ResumeUpdate provides new content for resume.md sections.
//
// Features is a single prose entry that, on v10+ projects, is appended as a
// bullet to agentctx/features.md rather than written into resume.md's Current
// State. Empty string is a no-op. On pre-v10 projects the field is ignored
// silently (features.md is not part of the schema yet).
type ResumeUpdate struct {
	CurrentState string `json:"current_state"`
	OpenThreads  string `json:"open_threads"`
	Features     string `json:"features"`
}

// TaskUpdate describes a task status change.
type TaskUpdate struct {
	Name   string `json:"name"`   // task filename
	Action string `json:"action"` // "complete" or "update_status"
	Status string `json:"status"`
	Reason string `json:"reason"`
}

// ActionReport summarizes what Apply did.
type ActionReport struct {
	LearningsAdded   int
	LearningsSkipped int
	StalesFlagged    int
	StalesSkipped    int
	ResumeUpdated    bool
	TasksUpdated     int
}
