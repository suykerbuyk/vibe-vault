// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package knowledge surfaces cross-project learnings stored under
// VibeVault/Knowledge/learnings/*.md as on-demand loadable entries.
//
// Each learning file uses a constrained subset of Claude Code's
// auto-memory frontmatter schema: a required name, description, and
// type (one of "user", "feedback", "reference"). The "project" type is
// explicitly rejected here because a project-scoped memory has no
// meaning in a cross-project directory — accepting it silently would
// produce misleading list output.
//
// This package exposes three entry points used by the MCP layer:
//   - List walks the learnings directory and returns metadata only.
//   - Get returns the full content of a single learning by slug.
//   - Count reports the number of valid learnings (for bootstrap
//     hinting) without materializing any content.
//
// Malformed files (unreadable, missing frontmatter markers, disallowed
// type, missing required fields) are skipped with a warning logged to
// stderr so the tool output stays uniform and machine-parseable.
package knowledge

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LearningMetadata is the frontmatter-only view of a learning file,
// returned by List and used by Get as the header block.
type LearningMetadata struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

// Learning is a full learning: metadata plus the markdown body below
// the closing frontmatter delimiter.
type Learning struct {
	LearningMetadata
	Content string `json:"content"`
}

// allowedTypes enumerates the frontmatter types that are valid inside
// Knowledge/learnings/. "project" is intentionally excluded.
var allowedTypes = map[string]struct{}{
	"user":      {},
	"feedback":  {},
	"reference": {},
}

// learningsDir returns the absolute path of Knowledge/learnings/ for
// the given vault root. It does not verify the directory exists.
func learningsDir(vaultPath string) string {
	return filepath.Join(vaultPath, "Knowledge", "learnings")
}

// List returns metadata for every valid learning file in the vault's
// Knowledge/learnings/ directory, optionally filtered by type.
//
// filterType: when empty, returns all valid entries; otherwise only
// entries whose frontmatter "type" equals filterType. Results are
// sorted alphabetically by slug for deterministic output. A missing
// directory yields an empty slice with no error (callers can treat the
// zero-case uniformly).
func List(vaultPath, filterType string) ([]LearningMetadata, error) {
	return listWithWarn(vaultPath, filterType, os.Stderr)
}

// listWithWarn is the test-friendly entry point: callers inject the
// warning sink so skip-warnings can be asserted without scraping
// stderr.
func listWithWarn(vaultPath, filterType string, warn io.Writer) ([]LearningMetadata, error) {
	dir := learningsDir(vaultPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []LearningMetadata{}, nil
		}
		return nil, fmt.Errorf("read learnings dir: %w", err)
	}

	var results []LearningMetadata
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		slug := strings.TrimSuffix(e.Name(), ".md")
		path := filepath.Join(dir, e.Name())

		meta, _, perr := parseLearning(path)
		if perr != nil {
			fmt.Fprintf(warn, "vv: skipping %s: %v\n", path, perr)
			continue
		}
		meta.Slug = slug

		if filterType != "" && meta.Type != filterType {
			continue
		}
		results = append(results, meta)
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Slug < results[j].Slug })
	if results == nil {
		results = []LearningMetadata{}
	}
	return results, nil
}

// Get returns the full learning for a given slug. Returns a descriptive
// error listing available slugs when the file is missing or invalid.
func Get(vaultPath, slug string) (*Learning, error) {
	return getWithWarn(vaultPath, slug, os.Stderr)
}

// getWithWarn is the test-friendly entry point for Get.
func getWithWarn(vaultPath, slug string, warn io.Writer) (*Learning, error) {
	if slug == "" {
		return nil, fmt.Errorf("slug is required")
	}
	// Defensive: reject any slug that would escape the learnings dir.
	if strings.ContainsAny(slug, "/\\") || strings.Contains(slug, "..") {
		return nil, fmt.Errorf("invalid slug: %q", slug)
	}

	dir := learningsDir(vaultPath)
	path := filepath.Join(dir, slug+".md")

	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, unknownSlugError(vaultPath, slug, warn)
		}
		return nil, fmt.Errorf("stat learning: %w", err)
	}

	meta, body, perr := parseLearning(path)
	if perr != nil {
		return nil, fmt.Errorf("learning %q is malformed: %w", slug, perr)
	}
	meta.Slug = slug

	return &Learning{LearningMetadata: meta, Content: body}, nil
}

// Count reports the number of valid learning files in Knowledge/learnings/.
// Missing directory returns 0 with nil error. Parse failures are logged
// to stderr and excluded from the count, mirroring List semantics so
// bootstrap never over-reports availability.
func Count(vaultPath string) (int, error) {
	return countWithWarn(vaultPath, os.Stderr)
}

func countWithWarn(vaultPath string, warn io.Writer) (int, error) {
	items, err := listWithWarn(vaultPath, "", warn)
	if err != nil {
		return 0, err
	}
	return len(items), nil
}

// unknownSlugError builds an actionable error that lists the slugs
// actually available, so the caller can correct a typo without making
// a second round trip.
func unknownSlugError(vaultPath, slug string, warn io.Writer) error {
	available, err := listWithWarn(vaultPath, "", warn)
	if err != nil || len(available) == 0 {
		return fmt.Errorf("learning %q not found (no learnings available)", slug)
	}
	slugs := make([]string, 0, len(available))
	for _, a := range available {
		slugs = append(slugs, a.Slug)
	}
	return fmt.Errorf("learning %q not found; available: %s", slug, strings.Join(slugs, ", "))
}

// parseLearning reads the file at path, extracts frontmatter, validates
// the type constraint and required fields, and returns metadata plus
// body. Errors carry enough context to be logged as skip warnings.
//
// Frontmatter rules:
//   - File MUST open with a "---" delimiter (leading whitespace allowed).
//   - Frontmatter MUST be closed by a second "---".
//   - Keys are parsed as "key: value" with quoted values tolerated.
//   - Required keys: name, description, type.
//   - type ∈ {user, feedback, reference}; anything else (including
//     "project") is rejected.
func parseLearning(path string) (LearningMetadata, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return LearningMetadata{}, "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 4*1024*1024)

	fm := map[string]string{}
	var bodyLines []string

	state := 0 // 0 = before frontmatter, 1 = inside, 2 = after
	for scanner.Scan() {
		line := scanner.Text()
		switch state {
		case 0:
			if strings.TrimSpace(line) == "---" {
				state = 1
				continue
			}
			// Any content before the frontmatter delimiter is a
			// malformed learning; the schema requires frontmatter.
			return LearningMetadata{}, "", fmt.Errorf("missing frontmatter opener")
		case 1:
			if strings.TrimSpace(line) == "---" {
				state = 2
				continue
			}
			if idx := strings.IndexByte(line, ':'); idx > 0 {
				key := strings.TrimSpace(line[:idx])
				val := strings.TrimSpace(line[idx+1:])
				val = stripQuotes(val)
				fm[key] = val
			}
		case 2:
			bodyLines = append(bodyLines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return LearningMetadata{}, "", fmt.Errorf("scan: %w", err)
	}

	if state == 0 {
		return LearningMetadata{}, "", fmt.Errorf("no frontmatter block")
	}
	if state == 1 {
		return LearningMetadata{}, "", fmt.Errorf("unterminated frontmatter")
	}

	meta := LearningMetadata{
		Name:        fm["name"],
		Description: fm["description"],
		Type:        fm["type"],
	}
	if meta.Name == "" {
		return LearningMetadata{}, "", fmt.Errorf("missing required frontmatter field: name")
	}
	if meta.Description == "" {
		return LearningMetadata{}, "", fmt.Errorf("missing required frontmatter field: description")
	}
	if meta.Type == "" {
		return LearningMetadata{}, "", fmt.Errorf("missing required frontmatter field: type")
	}
	if meta.Type == "project" {
		return LearningMetadata{}, "", fmt.Errorf("type %q is not allowed in Knowledge/learnings (project-scoped memories belong in a project's agentctx/memory/)", meta.Type)
	}
	if _, ok := allowedTypes[meta.Type]; !ok {
		return LearningMetadata{}, "", fmt.Errorf("type %q is not allowed (expected one of: user, feedback, reference)", meta.Type)
	}

	// Trim a single leading empty line so the body reads cleanly while
	// preserving intentional blank lines deeper in the file.
	if len(bodyLines) > 0 && strings.TrimSpace(bodyLines[0]) == "" {
		bodyLines = bodyLines[1:]
	}
	return meta, strings.Join(bodyLines, "\n"), nil
}

// stripQuotes removes matching single or double quotes from the edges
// of a frontmatter value. Keeps the parser forgiving about how users
// hand-author learning files.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
