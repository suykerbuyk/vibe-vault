package enrichment

// Result holds the LLM-generated enrichment for a session note.
type Result struct {
	Summary     string
	Decisions   []string
	OpenThreads []string
	Tag         string
}

// API request/response types for OpenAI-compatible chat completions.

type chatRequest struct {
	Model          string       `json:"model"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64      `json:"temperature"`
	ResponseFormat *respFormat  `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
	Error   *apiError    `json:"error,omitempty"`
}

type chatChoice struct {
	Message chatMessage `json:"message"`
}

type respFormat struct {
	Type string `json:"type"`
}

type apiError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// enrichmentJSON is the expected JSON structure from the LLM response.
type enrichmentJSON struct {
	Summary     string   `json:"summary"`
	Decisions   []string `json:"decisions"`
	OpenThreads []string `json:"open_threads"`
	Tag         string   `json:"tag"`
}
