package narrative

import "time"

// ActivityKind classifies what a tool call accomplished.
type ActivityKind int

const (
	KindFileCreate ActivityKind = iota
	KindFileModify
	KindTestRun
	KindGitCommit
	KindGitPush
	KindBuild
	KindCommand
	KindDecision
	KindPlanMode
	KindDelegation
	KindExplore
	KindError
)

// Activity represents a single meaningful action extracted from a tool call.
type Activity struct {
	Timestamp   time.Time
	Kind        ActivityKind
	Description string // "Created `internal/auth/handler.go`"
	Tool        string // "Write", "Bash", etc.
	IsError     bool
	Recovered   bool   // Error was followed by successful retry
	Detail      string // Command text, error message (optional)
}

// Segment represents a contiguous block of conversation, split at compact_boundary.
type Segment struct {
	Index        int
	StartTime    time.Time
	EndTime      time.Time
	Activities   []Activity
	UserRequest  string // First meaningful user message in segment
	TokensBefore int    // preTokens from compact_boundary (0 for first)
}

// Commit represents a git commit extracted from tool output.
type Commit struct {
	SHA     string // Short SHA from git output (7+ chars)
	Message string // Commit message
}

// Narrative holds the full extracted narrative for a session.
type Narrative struct {
	Segments      []Segment
	Commits       []Commit // Git commits extracted from tool output
	Title         string   // Better title from conversation analysis
	Summary       string   // Multi-sentence heuristic summary
	WorkPerformed string   // Rendered markdown for note section
	Decisions     []string
	OpenThreads   []string
	Tag           string // Inferred from tool usage patterns
}
