// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package knowledge

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ReadNotes walks Knowledge/learnings/ and Knowledge/decisions/ in the vault,
// parsing frontmatter from each .md file. Returns all active notes (skips
// .gitkeep and notes with status: archived).
func ReadNotes(vaultPath string) ([]Note, error) {
	var notes []Note

	for _, typeDir := range []string{"learnings", "decisions"} {
		dir := filepath.Join(vaultPath, "Knowledge", typeDir)

		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name == ".gitkeep" || !strings.HasSuffix(name, ".md") {
				continue
			}

			absPath := filepath.Join(dir, name)
			note, err := parseNoteFrontmatter(absPath)
			if err != nil {
				continue // skip unparseable files
			}

			if note.Type == "" {
				note.Type = strings.TrimSuffix(typeDir, "s") // "learnings" → "lesson", "decisions" → "decision"
			}

			note.NotePath = filepath.Join("Knowledge", typeDir, name)

			notes = append(notes, note)
		}
	}

	return notes, nil
}

// parseNoteFrontmatter extracts scalar frontmatter fields from a knowledge note.
// Parses: type, project, category, summary, confidence, date, status.
// Title is taken from the first "# " heading after frontmatter.
// Returns an error if the file cannot be read or has no frontmatter.
func parseNoteFrontmatter(path string) (Note, error) {
	f, err := os.Open(path)
	if err != nil {
		return Note{}, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var note Note
	var inFrontmatter bool
	var status string

	for scanner.Scan() {
		line := scanner.Text()

		if !inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = true
			}
			continue
		}

		// End of frontmatter
		if strings.TrimSpace(line) == "---" {
			break
		}

		// Skip list items (tags, source_sessions)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			continue
		}

		key, val := parseFrontmatterLine(line)
		if key == "" {
			continue
		}

		switch key {
		case "type":
			note.Type = val
		case "project":
			note.Project = val
		case "category":
			note.Category = val
		case "summary":
			note.Summary = val
		case "confidence":
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				note.Confidence = f
			}
		case "date":
			note.Date = val
		case "status":
			status = val
		}
	}

	// Skip archived notes
	if status == "archived" {
		return Note{}, os.ErrNotExist // sentinel: caller skips
	}

	// Find title from first "# " heading
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "# ") {
			note.Title = strings.TrimPrefix(line, "# ")
			break
		}
	}

	return note, scanner.Err()
}

// parseFrontmatterLine splits "key: value" and strips quotes from value.
func parseFrontmatterLine(line string) (string, string) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", ""
	}
	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])

	// Strip surrounding quotes
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		val = val[1 : len(val)-1]
	}

	return key, val
}
