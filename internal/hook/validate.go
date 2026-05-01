// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package hook

import (
	"fmt"
	"sort"
)

// Schema source: https://json.schemastore.org/claude-code-settings.json
// See doc/DESIGN.md #96 for the pinned validator contract.

// hookCommandVariants is the data-driven table of hookCommand `anyOf`
// variants. Each variant carries its `type` const (the map key), the
// required string fields beyond `type`, and the optional fields drawn from
// the schema's per-variant `properties` map. A variant's allowed field set
// is `{"type"} ∪ requiredStrings ∪ optionalFields`; any other field name
// fails strict `additionalProperties: false`. Adding a sixth variant is a
// one-row edit.
//
// optionalFields is name-only — v1 does NOT type-check optional fields. A
// `timeout: "thirty"` (string) passes; only the field-NAME set is checked.
// v2 candidate if a real-world false-pass on a wrong-typed optional field
// surfaces.
var hookCommandVariants = map[string]struct {
	requiredStrings []string
	optionalFields  []string
}{
	"command": {
		requiredStrings: []string{"command"},
		optionalFields:  []string{"timeout", "async", "asyncRewake", "shell", "if", "statusMessage"},
	},
	"prompt": {
		requiredStrings: []string{"prompt"},
		optionalFields:  []string{"model", "timeout", "if", "statusMessage"},
	},
	"agent": {
		requiredStrings: []string{"prompt"},
		optionalFields:  []string{"model", "timeout", "if", "statusMessage"},
	},
	"http": {
		requiredStrings: []string{"url"},
		optionalFields:  []string{"headers", "allowedEnvVars", "timeout", "if", "statusMessage"},
	},
	"mcp_tool": {
		requiredStrings: []string{"server", "tool"},
		optionalFields:  []string{"input", "timeout", "if", "statusMessage"},
	},
}

// ValidateHooks walks a settings.json hooks block and returns
// any shape errors that would cause Claude Code to reject the
// file. Accepts the raw value (settings["hooks"]) so it
// handles non-map values (string, array, null) without
// panicking. Empty slice means valid.
//
// Schema source: https://json.schemastore.org/claude-code-settings.json
// See doc/DESIGN.md #96 for the pinned contract.
func ValidateHooks(value any) []error {
	// Absence is fine — Install() writes a hooks block from scratch when none
	// exists. Distinguishing nil-absent from non-map-present matters here.
	if value == nil {
		return nil
	}

	hooksMap, ok := value.(map[string]any)
	if !ok {
		return []error{fmt.Errorf("hooks: expected object, got %s", typeOf(value))}
	}

	// Determinism: iterate event names in sorted order so test assertions are
	// stable. Map iteration is randomized in Go.
	events := make([]string, 0, len(hooksMap))
	for event := range hooksMap {
		events = append(events, event)
	}
	sort.Strings(events)

	var errs []error
	for _, event := range events {
		errs = append(errs, validateEvent(event, hooksMap[event])...)
	}
	return errs
}

// validateEvent walks one event's array (e.g. hooks.SessionEnd) and emits any
// shape errors found. Event-name enum is intentionally NOT validated — see
// DESIGN #96 (staleness avoidance for the closed enum of ~25 names).
func validateEvent(event string, value any) []error {
	arr, ok := value.([]any)
	if !ok {
		return []error{fmt.Errorf("hooks.%s: expected array, got %s", event, typeOf(value))}
	}

	var errs []error
	for i, entry := range arr {
		errs = append(errs, validateMatcher(event, i, entry)...)
	}
	return errs
}

// validateMatcher checks a single hookMatcher entry: required `hooks` array,
// optional string `matcher`, strict additionalProperties.
func validateMatcher(event string, index int, entry any) []error {
	path := fmt.Sprintf("hooks.%s[%d]", event, index)

	matcher, ok := entry.(map[string]any)
	if !ok {
		return []error{fmt.Errorf("%s: expected object, got %s", path, typeOf(entry))}
	}

	var errs []error

	// Required: hooks (array).
	innerRaw, hasHooks := matcher["hooks"]
	if !hasHooks {
		errs = append(errs, fmt.Errorf("%s: missing required field 'hooks' (matcher-wrapper shape)", path))
	}

	// Optional: matcher (string).
	if mRaw, hasMatcher := matcher["matcher"]; hasMatcher {
		if _, isString := mRaw.(string); !isString {
			errs = append(errs, fmt.Errorf("%s: field 'matcher' must be string, got %s", path, typeOf(mRaw)))
		}
	}

	// additionalProperties: false. Walk keys in sorted order for determinism.
	keys := make([]string, 0, len(matcher))
	for k := range matcher {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "hooks" || k == "matcher" {
			continue
		}
		errs = append(errs, fmt.Errorf("%s: unknown field '%s' (additionalProperties: false)", path, k))
	}

	// If `hooks` is present, validate its inner items.
	if hasHooks {
		innerArr, isArr := innerRaw.([]any)
		if !isArr {
			errs = append(errs, fmt.Errorf("%s.hooks: expected array, got %s", path, typeOf(innerRaw)))
		} else {
			for j, item := range innerArr {
				errs = append(errs, validateHookCommand(path, j, item)...)
			}
		}
	}

	return errs
}

// validateHookCommand checks a single hookCommand item: required `type` enum,
// per-variant required string fields, strict additionalProperties.
func validateHookCommand(matcherPath string, index int, item any) []error {
	path := fmt.Sprintf("%s.hooks[%d]", matcherPath, index)

	cmd, ok := item.(map[string]any)
	if !ok {
		return []error{fmt.Errorf("%s: expected object, got %s", path, typeOf(item))}
	}

	var errs []error

	// Required: type (string, must be one of the 5 const values).
	typeRaw, hasType := cmd["type"]
	if !hasType {
		errs = append(errs, fmt.Errorf("%s: missing required field 'type'", path))
		return errs
	}
	typeStr, isString := typeRaw.(string)
	if !isString {
		errs = append(errs, fmt.Errorf("%s: field 'type' must be string, got %s", path, typeOf(typeRaw)))
		return errs
	}
	variant, known := hookCommandVariants[typeStr]
	if !known {
		errs = append(errs, fmt.Errorf("%s: field 'type' has unknown value '%s' (must be one of: agent, command, http, mcp_tool, prompt)", path, typeStr))
		return errs
	}

	// Build the allowed-field set for this variant:
	// {"type"} ∪ requiredStrings ∪ optionalFields.
	allowed := map[string]bool{"type": true}
	for _, f := range variant.requiredStrings {
		allowed[f] = true
	}
	for _, f := range variant.optionalFields {
		allowed[f] = true
	}

	// Per-variant required fields.
	for _, f := range variant.requiredStrings {
		raw, has := cmd[f]
		if !has {
			errs = append(errs, fmt.Errorf("%s: missing required field '%s' for type '%s'", path, f, typeStr))
			continue
		}
		if _, isStr := raw.(string); !isStr {
			errs = append(errs, fmt.Errorf("%s: field '%s' must be string, got %s", path, f, typeOf(raw)))
		}
	}

	// additionalProperties: false. Walk keys in sorted order for determinism.
	keys := make([]string, 0, len(cmd))
	for k := range cmd {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if allowed[k] {
			continue
		}
		errs = append(errs, fmt.Errorf("%s: unknown field '%s' for type '%s' (additionalProperties: false)", path, k, typeStr))
	}

	return errs
}

// typeOf returns a short human label for the dynamic type of v, used in
// error messages. JSON-shaped names (object/array/string/number/bool/null)
// instead of Go-shaped ones (map[string]any/[]any/...) so error messages
// align with the schema vocabulary the operator sees in Claude Code's own
// errors.
func typeOf(v any) string {
	if v == nil {
		return "null"
	}
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case bool:
		return "bool"
	case float64, float32, int, int64, int32:
		return "number"
	default:
		return fmt.Sprintf("%T", v)
	}
}
