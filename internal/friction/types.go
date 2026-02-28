package friction

// Correction represents a detected user correction in dialogue.
type Correction struct {
	TurnIndex int    // Index of the user turn in dialogue
	Text      string // The correction text
	Pattern   string // Which pattern matched (e.g. "negation", "redirect")
}

// Signals holds the raw friction signal values before scoring.
type Signals struct {
	Corrections       int     // Count of detected corrections
	CorrectionDensity float64 // corrections / user turns
	TokensPerFile     float64 // total tokens / files changed
	FileRetryDensity  float64 // files with 3+ modifications / total files
	ErrorCycleDensity float64 // unrecovered errors / total activities
	RecurringThreads  bool    // open threads recurring from prior session
}

// Result holds the full friction analysis output.
type Result struct {
	Score       int        // Composite friction score 0-100
	Signals     Signals
	Corrections []Correction
	Summary     []string   // Human-readable signal descriptions
}

// ProjectFriction holds aggregated friction data across sessions for a project.
type ProjectFriction struct {
	Project          string
	Sessions         int
	AvgScore         float64
	MaxScore         int
	TotalCorrections int
	HighFriction     int // sessions with score >= 40
}
