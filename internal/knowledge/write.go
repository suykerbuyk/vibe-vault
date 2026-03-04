// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package knowledge

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// WriteNote renders and writes a knowledge note to the vault.
// Returns the relative path within the vault (e.g. "Knowledge/learnings/2026-02-28-dont-use-json-skip.md").
// Does not overwrite existing files. When a slug collision occurs, appends -2 through -10 suffix.
func WriteNote(vaultPath string, note Note) (string, error) {
	typeDir := "learnings"
	if note.Type == "decision" {
		typeDir = "decisions"
	}

	slug := slugify(note.Title)
	baseFilename := fmt.Sprintf("%s-%s", note.Date, slug)
	dir := filepath.Join("Knowledge", typeDir)

	// Try the base filename, then -2 through -10 on collision
	relPath, absPath, err := findAvailablePath(vaultPath, dir, baseFilename)
	if err != nil {
		return "", err
	}
	if relPath == "" {
		// All slots taken (base + -2 through -10)
		log.Printf("warning: all slug slots taken for %s, skipping", baseFilename)
		return "", nil
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return "", fmt.Errorf("create knowledge dir: %w", err)
	}

	md := RenderNote(note)
	if err := os.WriteFile(absPath, []byte(md), 0o644); err != nil {
		return "", fmt.Errorf("write knowledge note: %w", err)
	}

	return relPath, nil
}

// findAvailablePath finds the first available filename for a knowledge note.
// Tries baseFilename.md, then baseFilename-2.md through baseFilename-10.md.
// Returns ("", "", nil) if all slots are taken.
func findAvailablePath(vaultPath, dir, baseFilename string) (relPath, absPath string, err error) {
	// Try base filename first
	filename := baseFilename + ".md"
	rel := filepath.Join(dir, filename)
	abs := filepath.Join(vaultPath, rel)
	if _, err := os.Stat(abs); os.IsNotExist(err) {
		return rel, abs, nil
	}

	// Try -2 through -10
	for i := 2; i <= 10; i++ {
		filename = fmt.Sprintf("%s-%d.md", baseFilename, i)
		rel = filepath.Join(dir, filename)
		abs = filepath.Join(vaultPath, rel)
		if _, err := os.Stat(abs); os.IsNotExist(err) {
			return rel, abs, nil
		}
	}

	return "", "", nil
}

var nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a title to a URL/filename-safe slug.
func slugify(title string) string {
	s := strings.ToLower(title)

	// Replace non-alphanumeric chars with hyphens
	s = nonAlphaNum.ReplaceAllString(s, "-")

	// Trim leading/trailing hyphens
	s = strings.Trim(s, "-")

	// Collapse multiple hyphens
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}

	// Truncate to reasonable length
	if len(s) > 60 {
		// Try to cut at a word boundary
		cut := 60
		for cut > 40 && s[cut] != '-' {
			cut--
		}
		if s[cut] == '-' {
			s = s[:cut]
		} else {
			s = s[:60]
		}
	}

	// Final safety: ensure non-empty
	s = strings.TrimFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if s == "" {
		s = "note"
	}

	return s
}
