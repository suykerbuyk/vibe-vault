// Package mdutil provides shared markdown and text utilities used across
// vibe-vault's analysis and synthesis packages.
package mdutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MinWordLen is the minimum character length for a word to be considered significant.
const MinWordLen = 4

// SignificantWords extracts words ≥ MinWordLen chars, lowercased, with edge
// punctuation stripped, excluding common stop words.
func SignificantWords(s string) []string {
	words := strings.Fields(strings.ToLower(s))
	var result []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?\"'`()[]{}—-")
		if len(w) >= MinWordLen && !stopWords[w] {
			result = append(result, w)
		}
	}
	return result
}

// IsStopWord reports whether w is in the common stop-word list.
func IsStopWord(w string) bool {
	return stopWords[w]
}

// Overlap returns the number of shared elements between a and b.
// Duplicates in b do not inflate the count (deduplicating semantics).
func Overlap(a, b []string) int {
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	count := 0
	for _, s := range b {
		if set[s] {
			count++
			delete(set, s)
		}
	}
	return count
}

// SetIntersection returns elements present in both slices, deduplicated.
func SetIntersection(a, b []string) []string {
	set := make(map[string]bool, len(a))
	for _, s := range a {
		set[s] = true
	}
	var result []string
	for _, s := range b {
		if set[s] {
			result = append(result, s)
			delete(set, s)
		}
	}
	return result
}

// ReplaceSectionBody finds a ## heading in doc and replaces its body up to
// the next ## heading or EOF. Returns an error if the heading is not found.
func ReplaceSectionBody(doc, heading, newBody string) (string, error) {
	lines := strings.Split(doc, "\n")
	target := "## " + heading
	startIdx := -1

	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			startIdx = i
			break
		}
	}
	if startIdx == -1 {
		return "", fmt.Errorf("section %q not found", heading)
	}

	endIdx := len(lines)
	for i := startIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			endIdx = i
			break
		}
	}

	var result []string
	result = append(result, lines[:startIdx+1]...)
	result = append(result, "")
	body := strings.TrimRight(newBody, "\n")
	result = append(result, body)
	result = append(result, "")
	result = append(result, lines[endIdx:]...)

	return strings.Join(result, "\n"), nil
}

// AtomicWriteFile writes data to path via a temp file + rename for crash safety.
// Creates parent directories as needed.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".vv-tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}
	tmpPath = ""
	return nil
}

var stopWords = map[string]bool{
	"that": true, "this": true, "with": true, "from": true,
	"have": true, "been": true, "were": true, "will": true,
	"would": true, "could": true, "should": true, "what": true,
	"when": true, "where": true, "which": true, "their": true,
	"there": true, "these": true, "those": true, "them": true,
	"then": true, "than": true, "some": true, "also": true,
	"into": true, "each": true, "make": true, "like": true,
	"just": true, "over": true, "such": true, "only": true,
	"very": true, "more": true, "most": true, "other": true,
	"about": true, "after": true, "before": true, "being": true,
	"between": true, "does": true, "doing": true, "done": true,
}
