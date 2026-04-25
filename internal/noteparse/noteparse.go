// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package noteparse

import (
	"io"
	"os"
	"strings"

	"github.com/suykerbuyk/vibe-vault/internal/frontmatter"
)

// Note represents a parsed session note with frontmatter and body sections.
type Note struct {
	// Frontmatter key-value pairs
	Frontmatter map[string]string

	// Parsed frontmatter fields
	SessionID string
	Date      string
	Project   string
	Domain    string
	Branch    string
	Model     string
	Iteration string
	Summary   string
	Tag       string
	Tags      []string // parsed from bracket list
	Previous  string

	// Body sections extracted from markdown
	Decisions    []string // from ## Key Decisions
	OpenThreads  []string // from ## Open Threads
	FilesChanged []string // from ## What Changed
	Commits      []string // from ## Commits
}

// ParseFile reads and parses a session note from disk.
func ParseFile(path string) (*Note, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f)
}

// Parse reads and parses a session note from a reader.
func Parse(r io.Reader) (*Note, error) {
	res, err := frontmatter.Parse(r, frontmatter.Options{})
	if err != nil {
		return nil, err
	}

	note := &Note{Frontmatter: res.Fields}

	// Map frontmatter to typed fields
	note.SessionID = res.Fields["session_id"]
	note.Date = res.Fields["date"]
	note.Project = res.Fields["project"]
	note.Domain = res.Fields["domain"]
	note.Branch = res.Fields["branch"]
	note.Model = res.Fields["model"]
	note.Iteration = res.Fields["iteration"]
	note.Summary = res.Fields["summary"]
	note.Previous = res.Fields["previous"]

	// Parse tags bracket list
	if tagsRaw, ok := res.Fields["tags"]; ok {
		note.Tags = parseBracketList(tagsRaw)
		// Extract activity tag (skip base session tags)
		for _, t := range note.Tags {
			if t != "vv-session" {
				note.Tag = t
				break
			}
		}
	}

	// Extract body sections
	note.Decisions = extractBulletSection(res.Body, "## Key Decisions")
	note.OpenThreads = extractCheckboxSection(res.Body, "## Open Threads")
	note.FilesChanged = extractCodeItems(res.Body, "## What Changed")
	note.Commits = extractCodeItems(res.Body, "## Commits")

	return note, nil
}

// parseBracketList parses "[a, b, c]" into []string{"a", "b", "c"}.
func parseBracketList(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil
	}
	s = s[1 : len(s)-1]
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// extractBulletSection extracts bullet items from a markdown section.
func extractBulletSection(lines []string, heading string) []string {
	return extractSection(lines, heading, func(line string) (string, bool) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			return strings.TrimSpace(trimmed[2:]), true
		}
		return "", false
	})
}

// extractCheckboxSection extracts unchecked checkbox items from a markdown section.
// Checked items (- [x]) are treated as resolved and skipped.
func extractCheckboxSection(lines []string, heading string) []string {
	return extractSection(lines, heading, func(line string) (string, bool) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ] ") {
			return strings.TrimSpace(trimmed[6:]), true
		}
		// Skip resolved/checked items.
		return "", false
	})
}

// extractCodeItems extracts backtick-wrapped items from bullet lists.
func extractCodeItems(lines []string, heading string) []string {
	return extractSection(lines, heading, func(line string) (string, bool) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- `") && strings.Contains(trimmed[3:], "`") {
			// Extract content between backticks
			start := 3
			end := strings.Index(trimmed[start:], "`") + start
			return trimmed[start:end], true
		}
		return "", false
	})
}

// extractSection finds a heading and collects items using the provided parser.
func extractSection(lines []string, heading string, parse func(string) (string, bool)) []string {
	var items []string
	inSection := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == heading {
			inSection = true
			continue
		}

		if inSection {
			// New heading ends the section
			if strings.HasPrefix(trimmed, "## ") || trimmed == "---" {
				break
			}
			if item, ok := parse(line); ok {
				items = append(items, item)
			}
		}
	}

	return items
}
