// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package knowledge

import (
	"fmt"
	"strings"
)

// RenderNote produces Obsidian markdown for a knowledge Note, matching the
// vault's existing knowledge note format (learnings and decisions).
func RenderNote(n Note) string {
	var b strings.Builder

	// Frontmatter
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("date: %s\n", n.Date))
	b.WriteString(fmt.Sprintf("type: %s\n", n.Type))
	b.WriteString(fmt.Sprintf("project: %s\n", n.Project))
	b.WriteString("domain: personal\n")
	b.WriteString("status: active\n")

	// Tags
	tags := []string{"knowledge", n.Type}
	if n.Category != "" {
		tags = append(tags, n.Category)
	}
	tags = append(tags, n.Tags...)
	b.WriteString("tags:\n")
	for _, tag := range tags {
		b.WriteString(fmt.Sprintf("  - %s\n", tag))
	}

	b.WriteString(fmt.Sprintf("summary: \"%s\"\n", escapeYAML(n.Summary)))
	b.WriteString(fmt.Sprintf("confidence: %.2f\n", n.Confidence))

	if n.SourceSession != "" {
		b.WriteString("source_sessions:\n")
		b.WriteString(fmt.Sprintf("  - \"[[%s]]\"\n", n.SourceSession))
	}

	if n.Category != "" {
		b.WriteString(fmt.Sprintf("category: \"%s\"\n", n.Category))
	}

	b.WriteString("---\n\n")

	// Title
	b.WriteString(fmt.Sprintf("# %s\n\n", n.Title))

	// Body varies by type
	if n.Type == "lesson" {
		b.WriteString("## What Was Learned\n\n")
		b.WriteString(n.Body)
		b.WriteString("\n")
	} else {
		// decision
		b.WriteString("## Context\n\n")
		b.WriteString(n.Body)
		b.WriteString("\n")
	}

	return b.String()
}

func escapeYAML(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}
