package sanitize

import (
	"regexp"
	"strings"
)

var xmlTagPattern = regexp.MustCompile(
	`</?(?:local-command-(?:stdout|stderr|caveat)|command-(?:output|name|args|message)|` +
		`system-reminder|task-(?:id|notification)|persisted-output|thinking|tool-use-id|` +
		`tool|skill-name|plugin-id|vault)[^>]*>`,
)

// StripTags removes Claude Code XML wrapper tags from text.
func StripTags(text string) string {
	return strings.TrimSpace(xmlTagPattern.ReplaceAllString(text, ""))
}
