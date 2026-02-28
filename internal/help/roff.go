package help

import (
	"fmt"
	"strings"
	"time"
)

// FormatRoff renders a subcommand as a roff-formatted man page (.1).
// If date is empty, today's date is used (pass a fixed date for reproducible builds).
func FormatRoff(c Command, date string) string {
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	var b strings.Builder

	// .TH header
	fmt.Fprintf(&b, ".TH %s 1 %q %q %q\n",
		strings.ToUpper(c.ManName()), date, "vv "+Version, "Vibe-Vault Manual")

	// NAME
	b.WriteString(".SH NAME\n")
	fmt.Fprintf(&b, "%s \\- %s\n", c.ManName(), escapeRoff(c.Synopsis))

	// SYNOPSIS
	b.WriteString(".SH SYNOPSIS\n")
	b.WriteString(".B " + escapeRoff(c.Usage) + "\n")

	// DESCRIPTION
	if c.Description != "" {
		b.WriteString(".SH DESCRIPTION\n")
		writeRoffParagraphs(&b, c.Description)
	}

	// OPTIONS (args + flags)
	if len(c.Args) > 0 || len(c.Flags) > 0 {
		b.WriteString(".SH OPTIONS\n")
		for _, a := range c.Args {
			fmt.Fprintf(&b, ".TP\n.B %s\n%s\n", escapeRoff(a.Name), escapeRoff(a.Desc))
		}
		for _, f := range c.Flags {
			fmt.Fprintf(&b, ".TP\n.B %s\n%s\n", escapeRoff(f.Name), escapeRoff(f.Desc))
		}
	}

	// EXAMPLES
	if len(c.Examples) > 0 {
		b.WriteString(".SH EXAMPLES\n")
		b.WriteString(".nf\n")
		for _, e := range c.Examples {
			b.WriteString(escapeRoff(e) + "\n")
		}
		b.WriteString(".fi\n")
	}

	// SEE ALSO
	if len(c.SeeAlso) > 0 {
		b.WriteString(".SH SEE ALSO\n")
		refs := make([]string, len(c.SeeAlso))
		for i, ref := range c.SeeAlso {
			refs[i] = formatManRef(ref)
		}
		b.WriteString(strings.Join(refs, ",\n") + "\n")
	}

	return b.String()
}

// FormatRoffTopLevel renders the top-level vv.1 man page with a COMMANDS section.
func FormatRoffTopLevel(top Command, subs []Command, date string) string {
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}

	var b strings.Builder

	// .TH header
	fmt.Fprintf(&b, ".TH VV 1 %q %q %q\n",
		date, "vv "+Version, "Vibe-Vault Manual")

	// NAME
	b.WriteString(".SH NAME\n")
	fmt.Fprintf(&b, "vv \\- %s\n", escapeRoff(top.Synopsis))

	// SYNOPSIS
	b.WriteString(".SH SYNOPSIS\n")
	b.WriteString(".B vv\n.I command\n.RI [ options ]\n")

	// DESCRIPTION
	b.WriteString(".SH DESCRIPTION\n")
	b.WriteString(".B vv\n")
	b.WriteString("(vibe-vault) captures Claude Code session transcripts as structured\n")
	b.WriteString("Obsidian notes with frontmatter metadata, project context documents,\n")
	b.WriteString("and Dataview dashboards.\n")

	// COMMANDS
	b.WriteString(".SH COMMANDS\n")
	for _, s := range subs {
		fmt.Fprintf(&b, ".TP\n.B \"%s\"\n%s\n", escapeRoff(s.tableUsage()), escapeRoff(s.Brief))
	}

	// CONFIGURATION
	b.WriteString(".SH CONFIGURATION\n")
	b.WriteString("Configuration file: ~/.config/vibe-vault/config.toml\n")

	// SEE ALSO
	b.WriteString(".SH SEE ALSO\n")
	refs := make([]string, len(subs))
	for i, s := range subs {
		refs[i] = formatManRef(s.ManName() + "(1)")
	}
	b.WriteString(strings.Join(refs, ",\n") + "\n")

	return b.String()
}

// escapeRoff escapes characters that have special meaning in roff:
//   - backslashes → \\
//   - leading dots → \&.
//   - bare hyphens → \-  (for proper rendering of dashes)
func escapeRoff(s string) string {
	// Escape backslashes first
	s = strings.ReplaceAll(s, `\`, `\\`)
	// Leading dots on lines
	s = strings.ReplaceAll(s, "\n.", "\n\\&.")
	if strings.HasPrefix(s, ".") {
		s = "\\&" + s
	}
	// Hyphens → roff minus signs
	s = strings.ReplaceAll(s, "-", "\\-")
	return s
}

// writeRoffParagraphs writes multi-line description text as roff paragraphs.
// Blank lines in the input become .PP paragraph breaks.
func writeRoffParagraphs(b *strings.Builder, text string) {
	lines := strings.Split(text, "\n")
	prevBlank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if !prevBlank {
				b.WriteString(".PP\n")
			}
			prevBlank = true
			continue
		}
		prevBlank = false
		b.WriteString(escapeRoff(line) + "\n")
	}
}

// formatManRef formats a "name(section)" reference with bold name.
func formatManRef(ref string) string {
	// Input like "vv-init(1)" → ".BR vv-init (1)"
	if i := strings.Index(ref, "("); i >= 0 {
		name := ref[:i]
		section := ref[i:]
		return fmt.Sprintf(".BR %s %s", escapeRoff(name), section)
	}
	return ".B " + escapeRoff(ref)
}
