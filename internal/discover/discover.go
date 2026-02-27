package discover

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.jsonl$`)

// TranscriptFile represents a discovered transcript on disk.
type TranscriptFile struct {
	Path       string
	SessionID  string // UUID extracted from filename
	IsSubagent bool   // true if under */subagents/
	ModTime    int64  // unix timestamp for sorting
}

// Discover walks basePath recursively and returns all transcript JSONL files
// with valid UUID filenames, sorted by modification time (oldest first).
func Discover(basePath string) ([]TranscriptFile, error) {
	var results []TranscriptFile

	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible entries
		}
		if info.IsDir() {
			return nil
		}

		name := filepath.Base(path)
		if !uuidPattern.MatchString(name) {
			return nil
		}

		sessionID := strings.TrimSuffix(name, ".jsonl")
		isSubagent := strings.Contains(path, string(filepath.Separator)+"subagents"+string(filepath.Separator))

		results = append(results, TranscriptFile{
			Path:       path,
			SessionID:  sessionID,
			IsSubagent: isSubagent,
			ModTime:    info.ModTime().Unix(),
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].ModTime < results[j].ModTime
	})

	return results, nil
}

// FindBySessionID locates a specific transcript by session ID under basePath.
// Checks basePath/*/{sessionID}.jsonl and basePath/*/subagents/{sessionID}.jsonl.
func FindBySessionID(basePath, sessionID string) (string, error) {
	filename := sessionID + ".jsonl"

	// Check direct project dirs first
	entries, err := os.ReadDir(basePath)
	if err != nil {
		return "", err
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		// Check main transcript location
		candidate := filepath.Join(basePath, e.Name(), filename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}

		// Check subagents directory
		candidate = filepath.Join(basePath, e.Name(), "subagents", filename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", os.ErrNotExist
}
