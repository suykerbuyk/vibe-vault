package prose

import (
	"fmt"
	"strings"
)

// Render converts a Dialogue into markdown text for the session note.
func Render(d *Dialogue) string {
	if d == nil || len(d.Sections) == 0 {
		return ""
	}

	var b strings.Builder

	multiSection := len(d.Sections) > 1

	for i, sec := range d.Sections {
		if multiSection {
			if i > 0 {
				b.WriteString("\n")
			}
			heading := fmt.Sprintf("### Segment %d", i+1)
			if sec.UserRequest != "" {
				heading += fmt.Sprintf(": \"%s\"", sec.UserRequest)
			}
			b.WriteString(heading)
			b.WriteString("\n\n")
		}

		renderElements(&b, sec.Elements)
	}

	return b.String()
}

// renderElements writes a sequence of elements to the builder.
func renderElements(b *strings.Builder, elements []Element) {
	prevWasMarker := false

	for i, el := range elements {
		if el.Turn != nil {
			if i > 0 {
				b.WriteString("\n")
			}
			renderTurn(b, el.Turn)
			prevWasMarker = false
		} else if el.Marker != nil {
			if i > 0 && !prevWasMarker {
				b.WriteString("\n")
			}
			b.WriteString(fmt.Sprintf("*%s*\n", el.Marker.Text))
			prevWasMarker = true
		}
	}
}

// renderTurn writes a single turn as markdown.
func renderTurn(b *strings.Builder, t *Turn) {
	if t.Role == "user" {
		lines := strings.Split(t.Text, "\n")
		b.WriteString(fmt.Sprintf("> **User:** %s\n", lines[0]))
		for _, line := range lines[1:] {
			b.WriteString(fmt.Sprintf("> %s\n", line))
		}
	} else {
		b.WriteString(t.Text)
		b.WriteString("\n")
	}
}
