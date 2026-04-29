// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package agentregistry

import (
	"fmt"
	"strings"
)

// Direction-C Phase 4 retired the only embedded agent (wrap-executor.md)
// when the wrap-bundle pipeline retired. The registry remains as
// v2-portability scaffolding — vv_get_agent_definition surfaces an empty
// catalogue for now and will repopulate when future agents are added.
//
// New agents go in agents/<name>.md with YAML frontmatter. Re-introduce
// the //go:embed pattern below when the directory has at least one .md
// file (Go's embed pattern requires at least one match):
//
//	//go:embed agents/*.md
//	var embeddedAgents embed.FS
//
// Then call loadEmbeddedAgents from init() to parse and register them.
//
// parseAgent and parseFrontmatter remain exported (unexported in package
// scope) for future re-introduction.

// parseAgent splits an agent definition source into frontmatter and body and
// returns a populated AgentDefinition. The Sha256 field is left blank — it is
// computed inside register() so that mutated copies hash deterministically.
//
// Format: opening "---\n" line, frontmatter key/value lines, "---\n" closing
// line, then the system-prompt body. The frontmatter parser is intentionally
// minimal — it supports only the schema the registry consumes (no nested
// mappings except the inline list and pipe block scalar styles below).
func parseAgent(src string) (AgentDefinition, error) {
	const delim = "---"
	// Normalise line endings.
	src = strings.ReplaceAll(src, "\r\n", "\n")
	if !strings.HasPrefix(src, delim+"\n") {
		return AgentDefinition{}, fmt.Errorf("missing opening %q line", delim)
	}
	rest := src[len(delim)+1:]
	end := strings.Index(rest, "\n"+delim+"\n")
	if end < 0 {
		// Allow a trailing closing delimiter with no following newline iff
		// the body is empty.
		if strings.HasSuffix(rest, "\n"+delim) {
			rest = rest[:len(rest)-len(delim)-1]
			fm, err := parseFrontmatter(rest)
			if err != nil {
				return AgentDefinition{}, err
			}
			return defFromFrontmatter(fm, "")
		}
		return AgentDefinition{}, fmt.Errorf("missing closing %q line", delim)
	}
	frontmatter := rest[:end]
	body := rest[end+len(delim)+2:] // skip "\n---\n"
	body = strings.TrimLeft(body, "\n")
	body = strings.TrimRight(body, "\n") + "\n"
	if body == "\n" {
		body = ""
	}

	fm, err := parseFrontmatter(frontmatter)
	if err != nil {
		return AgentDefinition{}, err
	}
	return defFromFrontmatter(fm, body)
}

// frontmatterMap is the intermediate shape the parser hands to defFromFrontmatter.
// Scalar keys map to strings; list-valued keys map to []string. This keeps the
// parser ignorant of the AgentDefinition layout and makes unknown-key
// detection trivial.
type frontmatterMap struct {
	scalars map[string]string
	lists   map[string][]string
}

// known frontmatter keys in canonical order.
var knownKeys = []string{
	"name",
	"version",
	"description",
	"required_tools",
	"forbidden_tools",
	"escalation_triggers",
	"output_format",
	"recommended_model_class",
}

func isKnownKey(k string) bool {
	for _, kk := range knownKeys {
		if kk == k {
			return true
		}
	}
	return false
}

// parseFrontmatter parses the YAML-subset frontmatter. Supported value forms:
//   - bare scalar:   key: value
//   - quoted scalar: key: "1.0"
//   - inline list:   key: [a, b, c]
//   - block list:    key:\n  - a\n  - b
//   - pipe block:    key: |\n  line one\n  line two
//
// Unknown top-level keys are an error (strict validation).
func parseFrontmatter(text string) (frontmatterMap, error) {
	out := frontmatterMap{
		scalars: map[string]string{},
		lists:   map[string][]string{},
	}
	lines := strings.Split(text, "\n")
	i := 0
	for i < len(lines) {
		raw := lines[i]
		// Skip blank lines at the top level.
		if strings.TrimSpace(raw) == "" {
			i++
			continue
		}
		// Top-level lines must not start with whitespace.
		if raw != strings.TrimLeft(raw, " \t") {
			return out, fmt.Errorf("unexpected indented line at top level: %q", raw)
		}
		colon := strings.Index(raw, ":")
		if colon < 0 {
			return out, fmt.Errorf("expected key:value, got %q", raw)
		}
		key := strings.TrimSpace(raw[:colon])
		if !isKnownKey(key) {
			return out, fmt.Errorf("unknown frontmatter key %q (allowed: %s)", key, strings.Join(knownKeys, ", "))
		}
		valuePart := strings.TrimSpace(raw[colon+1:])
		switch {
		case valuePart == "|":
			// Pipe block scalar — consume indented continuation lines.
			i++
			var blockLines []string
			for i < len(lines) {
				bl := lines[i]
				if strings.TrimSpace(bl) == "" {
					blockLines = append(blockLines, "")
					i++
					continue
				}
				if !strings.HasPrefix(bl, "  ") {
					break
				}
				blockLines = append(blockLines, strings.TrimPrefix(bl, "  "))
				i++
			}
			// Trim any trailing blanks but preserve interior structure.
			for len(blockLines) > 0 && blockLines[len(blockLines)-1] == "" {
				blockLines = blockLines[:len(blockLines)-1]
			}
			out.scalars[key] = strings.Join(blockLines, "\n")
		case strings.HasPrefix(valuePart, "[") && strings.HasSuffix(valuePart, "]"):
			// Inline list.
			items := splitInlineList(valuePart[1 : len(valuePart)-1])
			out.lists[key] = items
			i++
		case valuePart == "":
			// Block list — consume "  - item" continuation lines.
			i++
			var items []string
			for i < len(lines) {
				bl := lines[i]
				trim := strings.TrimSpace(bl)
				if trim == "" {
					i++
					continue
				}
				if !strings.HasPrefix(bl, "  - ") && !strings.HasPrefix(bl, "- ") {
					break
				}
				if strings.HasPrefix(bl, "  - ") {
					items = append(items, strings.TrimSpace(bl[4:]))
				} else {
					items = append(items, strings.TrimSpace(bl[2:]))
				}
				i++
			}
			out.lists[key] = items
		default:
			out.scalars[key] = unquote(valuePart)
			i++
		}
	}
	return out, nil
}

// splitInlineList parses the comma-separated items between [ and ]. Items may
// be bare or quoted; surrounding whitespace is trimmed.
func splitInlineList(inner string) []string {
	if strings.TrimSpace(inner) == "" {
		return nil
	}
	parts := strings.Split(inner, ",")
	items := make([]string, 0, len(parts))
	for _, p := range parts {
		items = append(items, unquote(strings.TrimSpace(p)))
	}
	return items
}

// unquote strips a single pair of surrounding double or single quotes.
func unquote(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// defFromFrontmatter projects the parsed map onto an AgentDefinition. Required
// scalar fields ("name") are validated; everything else may be empty.
func defFromFrontmatter(fm frontmatterMap, body string) (AgentDefinition, error) {
	def := AgentDefinition{
		Name:                  fm.scalars["name"],
		Version:               fm.scalars["version"],
		Description:           fm.scalars["description"],
		OutputFormat:          fm.scalars["output_format"],
		RecommendedModelClass: fm.scalars["recommended_model_class"],
		RequiredTools:         fm.lists["required_tools"],
		ForbiddenTools:        fm.lists["forbidden_tools"],
		EscalationTriggers:    fm.lists["escalation_triggers"],
		SystemPrompt:          body,
	}
	if def.Name == "" {
		return AgentDefinition{}, fmt.Errorf("frontmatter is missing required key %q", "name")
	}
	return def, nil
}
