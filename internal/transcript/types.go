package transcript

import "time"

// Entry represents a single line in a Claude Code JSONL transcript.
type Entry struct {
	Type      string    `json:"type"`
	UUID      string    `json:"uuid"`
	ParentUUID string   `json:"parentUuid"`
	SessionID string    `json:"sessionId"`
	Timestamp time.Time `json:"timestamp"`
	CWD       string    `json:"cwd"`
	Version   string    `json:"version"`
	GitBranch string    `json:"gitBranch"`

	// Present on assistant messages
	Message   *Message  `json:"message,omitempty"`
	RequestID string    `json:"requestId,omitempty"`

	// Present on system messages
	Subtype string `json:"subtype,omitempty"`

	// IsMeta marks system-injected messages (e.g., CLAUDE.md, context reminders)
	// that should be skipped when extracting user requests for titles.
	// Native field in Claude Code's JSONL output.
	IsMeta bool `json:"isMeta,omitempty"`

	// Present on tool result entries (type=user with tool results)
	ToolUseResult        *ToolUseResult `json:"toolUseResult,omitempty"`
	SourceToolAssistantUUID string      `json:"sourceToolAssistantUUID,omitempty"`
}

// Message is the inner message object on user/assistant entries.
type Message struct {
	Role    string      `json:"role"`
	Model   string      `json:"model,omitempty"`
	ID      string      `json:"id,omitempty"`
	Content interface{} `json:"content"` // string or []ContentBlock
	Usage   *Usage      `json:"usage,omitempty"`
}

// ContentBlock represents one block in a content array.
type ContentBlock struct {
	Type      string      `json:"type"`
	Text      string      `json:"text,omitempty"`
	Thinking  string      `json:"thinking,omitempty"`
	ID        string      `json:"id,omitempty"`         // tool_use id
	Name      string      `json:"name,omitempty"`       // tool name
	Input     interface{} `json:"input,omitempty"`       // tool input
	ToolUseID string      `json:"tool_use_id,omitempty"` // tool_result
	Content   interface{} `json:"content,omitempty"`     // tool_result content (string or array)
	IsError   bool        `json:"is_error,omitempty"`
}

// Usage tracks token consumption for an assistant message.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// ToolUseResult holds stdout/stderr from tool execution.
type ToolUseResult struct {
	Stdout      string `json:"stdout,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
	Interrupted bool   `json:"interrupted,omitempty"`
}

// Stats holds aggregated statistics for a parsed transcript.
type Stats struct {
	SessionID    string
	Model        string
	GitBranch    string
	CWD          string
	StartTime    time.Time
	EndTime      time.Time
	Duration     time.Duration
	UserMessages int
	AssistantMessages int
	ToolUses     int
	InputTokens  int
	OutputTokens int
	CacheReads   int
	CacheWrites  int
	FilesRead    map[string]bool
	FilesWritten map[string]bool
	ToolCounts   map[string]int
}

// Transcript holds the fully parsed result of a JSONL transcript.
type Transcript struct {
	Entries []Entry
	Stats   Stats
}
