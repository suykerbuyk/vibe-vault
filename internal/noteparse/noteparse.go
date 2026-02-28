package noteparse

import (
	"bufio"
	"io"
	"os"
	"strings"
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
	scanner := bufio.NewScanner(r)
	note := &Note{
		Frontmatter: make(map[string]string),
	}

	// State machine for frontmatter
	inFrontmatter := false
	frontmatterDone := false
	var bodyLines []string

	for scanner.Scan() {
		line := scanner.Text()

		if !inFrontmatter && !frontmatterDone {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = true
				continue
			}
			// No frontmatter delimiter found yet — treat as body
			bodyLines = append(bodyLines, line)
			frontmatterDone = true
			continue
		}

		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = false
				frontmatterDone = true
				continue
			}
			// Parse key: value
			if idx := strings.IndexByte(line, ':'); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				val := strings.TrimSpace(line[idx+1:])
				// Strip surrounding quotes
				val = stripQuotes(val)
				note.Frontmatter[key] = val
			}
			continue
		}

		// Body
		bodyLines = append(bodyLines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Map frontmatter to typed fields
	note.SessionID = note.Frontmatter["session_id"]
	note.Date = note.Frontmatter["date"]
	note.Project = note.Frontmatter["project"]
	note.Domain = note.Frontmatter["domain"]
	note.Branch = note.Frontmatter["branch"]
	note.Model = note.Frontmatter["model"]
	note.Iteration = note.Frontmatter["iteration"]
	note.Summary = note.Frontmatter["summary"]
	note.Previous = note.Frontmatter["previous"]

	// Parse tags bracket list
	if tagsRaw, ok := note.Frontmatter["tags"]; ok {
		note.Tags = parseBracketList(tagsRaw)
		// Extract activity tag (non-cortana-session tag)
		for _, t := range note.Tags {
			if t != "cortana-session" {
				note.Tag = t
				break
			}
		}
	}

	// Extract body sections
	note.Decisions = extractBulletSection(bodyLines, "## Key Decisions")
	note.OpenThreads = extractCheckboxSection(bodyLines, "## Open Threads")
	note.FilesChanged = extractCodeItems(bodyLines, "## What Changed")
	note.Commits = extractCodeItems(bodyLines, "## Commits")

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

// extractCheckboxSection extracts checkbox items from a markdown section.
func extractCheckboxSection(lines []string, heading string) []string {
	return extractSection(lines, heading, func(line string) (string, bool) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- [ ] ") {
			return strings.TrimSpace(trimmed[6:]), true
		}
		if strings.HasPrefix(trimmed, "- [x] ") {
			return strings.TrimSpace(trimmed[6:]), true
		}
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

func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
