package help

import (
	"fmt"
	"strings"
)

// FormatTerminal renders a subcommand's help text for terminal --help output.
// The output is identical to the original inline fmt.Fprintf help text.
func FormatTerminal(c Command) string {
	var sections []string

	// Header: "vv <name> — <synopsis>"
	sections = append(sections, fmt.Sprintf("vv %s \u2014 %s", c.Name, c.Synopsis))

	// Usage line
	sections = append(sections, fmt.Sprintf("Usage: %s", c.Usage))

	// Compute description column for args/flags alignment.
	// Column = 2 (indent) + colWidth, where entries are padded to colWidth.
	// When both args and flags exist, minimum column is 13 for visual balance.
	maxNameLen := 0
	for _, a := range c.Args {
		if len(a.Name) > maxNameLen {
			maxNameLen = len(a.Name)
		}
	}
	for _, f := range c.Flags {
		if len(f.Name) > maxNameLen {
			maxNameLen = len(f.Name)
		}
	}
	col := 2 + maxNameLen + 3
	if len(c.Args) > 0 && len(c.Flags) > 0 && col < 13 {
		col = 13
	}

	// Arguments section
	if len(c.Args) > 0 {
		s := "Arguments:\n"
		for _, a := range c.Args {
			gap := col - 2 - len(a.Name)
			s += fmt.Sprintf("  %s%s%s", a.Name, strings.Repeat(" ", gap), a.Desc)
		}
		sections = append(sections, s)
	}

	// Flags section
	if len(c.Flags) > 0 {
		s := "Flags:\n"
		for _, f := range c.Flags {
			gap := col - 2 - len(f.Name)
			s += fmt.Sprintf("  %s%s%s", f.Name, strings.Repeat(" ", gap), f.Desc)
		}
		sections = append(sections, s)
	}

	// Description
	if c.Description != "" {
		sections = append(sections, c.Description)
	}

	// Examples
	if len(c.Examples) > 0 {
		s := "Examples:\n"
		for _, e := range c.Examples {
			s += "  " + e + "\n"
		}
		// Trim final newline — Join adds \n\n between sections,
		// and we add a trailing \n after Join.
		s = strings.TrimRight(s, "\n")
		sections = append(sections, s)
	}

	return strings.Join(sections, "\n\n") + "\n"
}

// FormatUsage renders the top-level usage text (for vv --help / vv help).
func FormatUsage(top Command, subs []Command) string {
	var b strings.Builder

	// Header
	fmt.Fprintf(&b, "vv v%s \u2014 %s\n", Version, top.Synopsis)

	// Subcommand table
	b.WriteString("\nUsage:\n")

	// Collect table entries: usage → brief
	type entry struct {
		usage string
		brief string
	}
	entries := make([]entry, 0, len(subs)+1)
	for _, s := range subs {
		entries = append(entries, entry{s.tableUsage(), s.Brief})
	}
	entries = append(entries, entry{"vv help", "Show this help"})

	// Find max usage width for alignment
	maxWidth := 0
	for _, e := range entries {
		if len(e.usage) > maxWidth {
			maxWidth = len(e.usage)
		}
	}

	for _, e := range entries {
		gap := maxWidth - len(e.usage) + 3
		fmt.Fprintf(&b, "  %s%s%s\n", e.usage, strings.Repeat(" ", gap), e.brief)
	}

	// Footer
	b.WriteString(`
Hook integration (settings.json):
  {"type": "command", "command": "vv hook"}

Configuration: ~/.config/vibe-vault/config.toml
`)
	return b.String()
}
