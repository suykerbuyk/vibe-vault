// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package hook

import (
	"strings"
	"testing"
)

// TestValidateHooks is one RUN-counted test function with many subtests so
// granular failure output is preserved without inflating the function count.
// Cases mirror DESIGN #96 and the iter-178 bug-report fixture.
func TestValidateHooks(t *testing.T) {
	// Case 1: nil value (settings has no `hooks` block at all). Absent block
	// is NOT an error — Install() writes from scratch when absent.
	t.Run("nil_absent_is_valid", func(t *testing.T) {
		errs := ValidateHooks(nil)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors for nil, got %d: %v", len(errs), errs)
		}
	})

	// Case 2: top-level non-map values. Each yields exactly one top-level
	// error.
	t.Run("toplevel_string_invalid", func(t *testing.T) {
		errs := ValidateHooks("broken")
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if !strings.Contains(errs[0].Error(), "hooks: expected object") {
			t.Errorf("unexpected error message: %v", errs[0])
		}
	})
	t.Run("toplevel_array_invalid", func(t *testing.T) {
		errs := ValidateHooks([]any{})
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if !strings.Contains(errs[0].Error(), "hooks: expected object") {
			t.Errorf("unexpected error message: %v", errs[0])
		}
	})
	t.Run("toplevel_number_invalid", func(t *testing.T) {
		errs := ValidateHooks(float64(42))
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if !strings.Contains(errs[0].Error(), "hooks: expected object") {
			t.Errorf("unexpected error message: %v", errs[0])
		}
	})
	t.Run("toplevel_bool_invalid", func(t *testing.T) {
		errs := ValidateHooks(true)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if !strings.Contains(errs[0].Error(), "hooks: expected object") {
			t.Errorf("unexpected error message: %v", errs[0])
		}
	})

	// Case 3: event value non-array.
	t.Run("event_value_non_array", func(t *testing.T) {
		hooks := map[string]any{
			"SessionEnd": "string instead of array",
		}
		errs := ValidateHooks(hooks)
		if len(errs) != 1 {
			t.Fatalf("expected 1 error, got %d: %v", len(errs), errs)
		}
		if !strings.Contains(errs[0].Error(), "hooks.SessionEnd") {
			t.Errorf("expected error path 'hooks.SessionEnd', got: %v", errs[0])
		}
		if !strings.Contains(errs[0].Error(), "expected array") {
			t.Errorf("expected 'expected array' in error, got: %v", errs[0])
		}
	})

	// Case 4: iter-178 fixture (CRITICAL — regression test for the bug report).
	// The legacy flat shape `{type, command}` directly under the event array
	// fails because the matcher-wrapper's required `hooks` array is missing.
	// Fields `command` and `type` at matcher level also trigger
	// additionalProperties errors. We assert at least one error names the
	// PostToolUse[0] path.
	t.Run("iter178_regression_fixture", func(t *testing.T) {
		hooks := map[string]any{
			"PostToolUse": []any{
				map[string]any{
					"command": "vv hook",
					"type":    "command",
				},
			},
		}
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected at least one error for iter-178 shape, got 0")
		}
		// At least one error must point to PostToolUse[0].
		var foundPath bool
		for _, e := range errs {
			if strings.Contains(e.Error(), "hooks.PostToolUse[0]") {
				foundPath = true
				break
			}
		}
		if !foundPath {
			t.Errorf("expected at least one error naming hooks.PostToolUse[0], got: %v", errs)
		}
	})

	// Case 5: matcher absent (optional field) — well-formed.
	t.Run("matcher_absent_valid", func(t *testing.T) {
		hooks := map[string]any{
			"SessionEnd": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "vv hook"},
					},
				},
			},
		}
		errs := ValidateHooks(hooks)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})

	// Case 6: matcher present (empty string).
	t.Run("matcher_empty_string_valid", func(t *testing.T) {
		hooks := map[string]any{
			"SessionEnd": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "vv hook"},
					},
				},
			},
		}
		errs := ValidateHooks(hooks)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})
	// Case 6b: matcher present (named).
	t.Run("matcher_named_valid", func(t *testing.T) {
		hooks := map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{"type": "command", "command": "echo hi"},
					},
				},
			},
		}
		errs := ValidateHooks(hooks)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})

	// Case 7: each of 5 hookCommand variants — valid.
	t.Run("variant_command_valid", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "command", "command": "echo hi"})
		errs := ValidateHooks(hooks)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})
	t.Run("variant_prompt_valid", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "prompt", "prompt": "say hi"})
		errs := ValidateHooks(hooks)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})
	t.Run("variant_agent_valid", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "agent", "prompt": "do thing"})
		errs := ValidateHooks(hooks)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})
	t.Run("variant_http_valid", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "http", "url": "https://example.com/hook"})
		errs := ValidateHooks(hooks)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})
	t.Run("variant_mcp_tool_valid", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "mcp_tool", "server": "vibe-vault", "tool": "vv_capture_session"})
		errs := ValidateHooks(hooks)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})

	// Case 8: each of 5 hookCommand variants — missing required field.
	t.Run("variant_command_missing_command", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "command"})
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected error, got none")
		}
		if !strings.Contains(errs[0].Error(), "missing required field 'command'") {
			t.Errorf("unexpected error: %v", errs[0])
		}
	})
	t.Run("variant_prompt_missing_prompt", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "prompt"})
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected error, got none")
		}
		if !strings.Contains(errs[0].Error(), "missing required field 'prompt'") {
			t.Errorf("unexpected error: %v", errs[0])
		}
	})
	t.Run("variant_agent_missing_prompt", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "agent"})
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected error, got none")
		}
		if !strings.Contains(errs[0].Error(), "missing required field 'prompt'") {
			t.Errorf("unexpected error: %v", errs[0])
		}
	})
	t.Run("variant_http_missing_url", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "http"})
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected error, got none")
		}
		if !strings.Contains(errs[0].Error(), "missing required field 'url'") {
			t.Errorf("unexpected error: %v", errs[0])
		}
	})
	t.Run("variant_mcp_tool_missing_server", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "mcp_tool", "tool": "vv_thing"})
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected error, got none")
		}
		var found bool
		for _, e := range errs {
			if strings.Contains(e.Error(), "missing required field 'server'") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected 'missing required field server' error, got: %v", errs)
		}
	})
	t.Run("variant_mcp_tool_missing_tool", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "mcp_tool", "server": "vibe-vault"})
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected error, got none")
		}
		var found bool
		for _, e := range errs {
			if strings.Contains(e.Error(), "missing required field 'tool'") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected 'missing required field tool' error, got: %v", errs)
		}
	})

	// Case 9: unknown type value.
	t.Run("unknown_type_value", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "unknown", "command": "x"})
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected error, got none")
		}
		if !strings.Contains(errs[0].Error(), "unknown value 'unknown'") {
			t.Errorf("unexpected error: %v", errs[0])
		}
	})

	// Case 10: missing type field.
	t.Run("missing_type_field", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"command": "echo"})
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected error, got none")
		}
		if !strings.Contains(errs[0].Error(), "missing required field 'type'") {
			t.Errorf("unexpected error: %v", errs[0])
		}
	})

	// Case 11: type wrong Go type (e.g. number).
	t.Run("type_wrong_go_type", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": float64(42)})
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected error, got none")
		}
		if !strings.Contains(errs[0].Error(), "field 'type' must be string") {
			t.Errorf("unexpected error: %v", errs[0])
		}
	})

	// Case 12: unknown field at hookMatcher level.
	t.Run("unknown_field_at_matcher", func(t *testing.T) {
		hooks := map[string]any{
			"SessionEnd": []any{
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "vv hook"},
					},
					"weird": float64(1),
				},
			},
		}
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected error, got none")
		}
		var found bool
		for _, e := range errs {
			if strings.Contains(e.Error(), "unknown field 'weird'") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected 'unknown field weird' at matcher level, got: %v", errs)
		}
	})

	// Case 13: unknown field at hookCommand level for variant 'command'.
	t.Run("unknown_field_at_hookcommand", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{
			"type":    "command",
			"command": "x",
			"weird":   float64(1),
		})
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected error, got none")
		}
		var found bool
		for _, e := range errs {
			if strings.Contains(e.Error(), "unknown field 'weird' for type 'command'") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected 'unknown field weird for type command', got: %v", errs)
		}
	})

	// Case 14: required field wrong Go type.
	t.Run("required_field_wrong_go_type", func(t *testing.T) {
		hooks := wrapVariant(map[string]any{"type": "command", "command": float64(42)})
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected error, got none")
		}
		if !strings.Contains(errs[0].Error(), "field 'command' must be string") {
			t.Errorf("unexpected error: %v", errs[0])
		}
	})

	// Case 15: unknown event name passes intentionally — DESIGN #96 deliberate
	// skip of the top-level event-name enum (staleness avoidance).
	t.Run("unknown_event_name_passes", func(t *testing.T) {
		hooks := map[string]any{
			"FutureEventName": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "x"},
					},
				},
			},
		}
		errs := ValidateHooks(hooks)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors (event-name enum SKIPPED per DESIGN #96), got %d: %v", len(errs), errs)
		}
	})

	// Case 16: well-formed multi-event multi-entry.
	t.Run("multi_event_multi_entry_valid", func(t *testing.T) {
		hooks := map[string]any{
			"SessionEnd": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "vv hook"},
					},
				},
				map[string]any{
					"matcher": "",
					"hooks": []any{
						map[string]any{"type": "command", "command": "other-tool"},
					},
				},
			},
			"Stop": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "vv hook"},
					},
				},
			},
		}
		errs := ValidateHooks(hooks)
		if len(errs) != 0 {
			t.Errorf("expected 0 errors, got %d: %v", len(errs), errs)
		}
	})

	// Case 17: foreign event valid + vv-event broken — exactly one error,
	// pointing only at PostToolUse[0]. Foreign-valid case must not add noise.
	t.Run("foreign_valid_with_vv_invalid", func(t *testing.T) {
		hooks := map[string]any{
			"Stop": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "vv hook"},
					},
				},
			},
			"PostToolUse": []any{
				map[string]any{
					"command": "vv hook",
					"type":    "command",
				},
			},
		}
		errs := ValidateHooks(hooks)
		if len(errs) == 0 {
			t.Fatal("expected errors for the broken PostToolUse entry, got 0")
		}
		// Every error should point to PostToolUse[0].
		for _, e := range errs {
			if !strings.Contains(e.Error(), "hooks.PostToolUse[0]") {
				t.Errorf("expected every error to name hooks.PostToolUse[0], got: %v", e)
			}
		}
	})
}

// wrapVariant wraps a hookCommand item inside a single matcher entry under
// SessionEnd, the most common shape in vv-emitted settings.
func wrapVariant(item map[string]any) map[string]any {
	return map[string]any{
		"SessionEnd": []any{
			map[string]any{
				"hooks": []any{item},
			},
		},
	}
}

// TestParseSettings sanity-checks the exported wrapper around the existing
// JSONC-aware parser.
func TestParseSettings(t *testing.T) {
	t.Run("empty_input_yields_empty_map", func(t *testing.T) {
		m, err := ParseSettings([]byte(""))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m) != 0 {
			t.Errorf("expected empty map, got %v", m)
		}
	})
	t.Run("whitespace_only_yields_empty_map", func(t *testing.T) {
		m, err := ParseSettings([]byte("   \n\t"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m) != 0 {
			t.Errorf("expected empty map, got %v", m)
		}
	})
	t.Run("valid_jsonc_with_comment", func(t *testing.T) {
		m, err := ParseSettings([]byte("{\n  // comment\n  \"a\": 1\n}"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v, ok := m["a"].(float64); !ok || v != 1 {
			t.Errorf("expected a=1, got %v", m)
		}
	})
	t.Run("invalid_json_returns_error", func(t *testing.T) {
		_, err := ParseSettings([]byte("{not json"))
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
