// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package agentregistry

import (
	"strings"
	"testing"
)

// snapshotRegistry returns a deep copy of the current registry contents so a
// test can mutate the global without poisoning siblings. Pair with
// restoreRegistry() in a t.Cleanup.
func snapshotRegistry() map[string]AgentDefinition {
	out := map[string]AgentDefinition{}
	for k, v := range registry {
		out[k] = *v
	}
	return out
}

func restoreRegistry(snapshot map[string]AgentDefinition) {
	registry = map[string]*AgentDefinition{}
	for k, v := range snapshot {
		d := v
		registry[k] = &d
	}
}

// withFreshRegistry resets the registry for the duration of the test and
// restores it after.
func withFreshRegistry(t *testing.T) {
	t.Helper()
	snap := snapshotRegistry()
	reset()
	t.Cleanup(func() { restoreRegistry(snap) })
}

func sampleDef(name string) AgentDefinition {
	return AgentDefinition{
		Name:                  name,
		Version:               "1.0",
		Description:           "test agent",
		SystemPrompt:          "you are a test\n",
		RequiredTools:         []string{"tool_a", "tool_b"},
		ForbiddenTools:        []string{"Bash"},
		EscalationTriggers:    []string{"trigger_one"},
		OutputFormat:          "terminal: foo()",
		RecommendedModelClass: "sonnet",
	}
}

func TestLookup_HappyPath(t *testing.T) {
	withFreshRegistry(t)
	register(sampleDef("alpha"))

	got, err := Lookup("alpha")
	if err != nil {
		t.Fatalf("Lookup: unexpected error: %v", err)
	}
	if got.Name != "alpha" || got.Version != "1.0" {
		t.Errorf("scalar fields wrong: %+v", got)
	}
	if got.Description != "test agent" {
		t.Errorf("description: %q", got.Description)
	}
	if got.SystemPrompt != "you are a test\n" {
		t.Errorf("system prompt: %q", got.SystemPrompt)
	}
	if len(got.RequiredTools) != 2 || got.RequiredTools[0] != "tool_a" {
		t.Errorf("required tools: %v", got.RequiredTools)
	}
	if len(got.ForbiddenTools) != 1 || got.ForbiddenTools[0] != "Bash" {
		t.Errorf("forbidden tools: %v", got.ForbiddenTools)
	}
	if len(got.EscalationTriggers) != 1 {
		t.Errorf("escalation triggers: %v", got.EscalationTriggers)
	}
	if got.OutputFormat != "terminal: foo()" {
		t.Errorf("output format: %q", got.OutputFormat)
	}
	if got.RecommendedModelClass != "sonnet" {
		t.Errorf("model class: %q", got.RecommendedModelClass)
	}
	if got.Sha256 == "" || len(got.Sha256) != 64 {
		t.Errorf("sha256 not populated: %q", got.Sha256)
	}

	// Mutating the returned slices must not corrupt subsequent lookups.
	got.RequiredTools[0] = "MUTATED"
	again, _ := Lookup("alpha")
	if again.RequiredTools[0] == "MUTATED" {
		t.Error("Lookup must return defensive copies of slice fields")
	}
}

func TestLookup_MissingName(t *testing.T) {
	withFreshRegistry(t)
	register(sampleDef("alpha"))

	_, err := Lookup("nope")
	if err == nil {
		t.Fatal("expected error for unknown name")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error message should reference the missing name: %v", err)
	}
}

func TestList_ReturnsSorted(t *testing.T) {
	withFreshRegistry(t)
	register(sampleDef("charlie"))
	register(sampleDef("alpha"))
	register(sampleDef("bravo"))

	all := List()
	if len(all) != 3 {
		t.Fatalf("expected 3 defs, got %d", len(all))
	}
	want := []string{"alpha", "bravo", "charlie"}
	for i, d := range all {
		if d.Name != want[i] {
			t.Errorf("position %d: got %q want %q", i, d.Name, want[i])
		}
	}
}

func TestParse_FrontmatterEdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantErr string // substring; empty = expect success
	}{
		{
			name: "missing closing delim",
			src: `---
name: x
version: "1"
body without close
`,
			wantErr: "missing closing",
		},
		{
			name: "missing opening delim",
			src: `name: x
version: "1"
---
body
`,
			wantErr: "missing opening",
		},
		{
			name: "unknown frontmatter key",
			src: `---
name: x
mystery_field: hello
---
body
`,
			wantErr: "unknown frontmatter key",
		},
		{
			name: "missing required name",
			src: `---
version: "1"
---
body
`,
			wantErr: "missing required key",
		},
		{
			name: "empty body after closing delim",
			src: `---
name: empty-body
---
`,
			wantErr: "",
		},
		{
			name: "body with embedded triple-dash lines (must not split)",
			src: `---
name: with-dashes
---
intro paragraph
---
this dash line is part of the body
---
trailing line
`,
			wantErr: "",
		},
		{
			name: "block list",
			src: `---
name: block-list
escalation_triggers:
  - one
  - two
  - three
---
prompt
`,
			wantErr: "",
		},
		{
			name: "pipe block scalar",
			src: `---
name: pipe-scalar
output_format: |
  line one
  line two
---
prompt
`,
			wantErr: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def, err := parseAgent(tc.src)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				switch tc.name {
				case "empty body after closing delim":
					if def.SystemPrompt != "" {
						t.Errorf("expected empty body, got %q", def.SystemPrompt)
					}
				case "body with embedded triple-dash lines (must not split)":
					if !strings.Contains(def.SystemPrompt, "this dash line is part of the body") {
						t.Errorf("body lost its embedded --- lines: %q", def.SystemPrompt)
					}
					if !strings.Contains(def.SystemPrompt, "trailing line") {
						t.Errorf("body truncated at first interior ---: %q", def.SystemPrompt)
					}
				case "block list":
					if len(def.EscalationTriggers) != 3 || def.EscalationTriggers[2] != "three" {
						t.Errorf("block list parse: %v", def.EscalationTriggers)
					}
				case "pipe block scalar":
					if !strings.Contains(def.OutputFormat, "line one") ||
						!strings.Contains(def.OutputFormat, "line two") {
						t.Errorf("pipe scalar lost content: %q", def.OutputFormat)
					}
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestSha256_Stable(t *testing.T) {
	d := sampleDef("alpha")
	a := canonicalSha256(d)
	b := canonicalSha256(d)
	if a != b {
		t.Errorf("sha256 not stable across repeat calls: %s vs %s", a, b)
	}

	// Mutating any field must change the digest.
	mutations := []func(*AgentDefinition){
		func(x *AgentDefinition) { x.Name = "different" },
		func(x *AgentDefinition) { x.Version = "9.9" },
		func(x *AgentDefinition) { x.Description = "edited" },
		func(x *AgentDefinition) { x.SystemPrompt = "different prompt" },
		func(x *AgentDefinition) { x.RequiredTools = append(x.RequiredTools, "extra") },
		func(x *AgentDefinition) { x.ForbiddenTools = append(x.ForbiddenTools, "extra") },
		func(x *AgentDefinition) { x.EscalationTriggers = append(x.EscalationTriggers, "extra") },
		func(x *AgentDefinition) { x.OutputFormat = "different" },
		func(x *AgentDefinition) { x.RecommendedModelClass = "opus" },
	}
	for i, mut := range mutations {
		mutated := d
		mutated.RequiredTools = append([]string(nil), d.RequiredTools...)
		mutated.ForbiddenTools = append([]string(nil), d.ForbiddenTools...)
		mutated.EscalationTriggers = append([]string(nil), d.EscalationTriggers...)
		mut(&mutated)
		if canonicalSha256(mutated) == a {
			t.Errorf("mutation %d did not change sha256", i)
		}
	}
}

func TestSha256_FieldOrderIndependent(t *testing.T) {
	// Two source files where only the YAML key order differs.
	src1 := `---
name: order-test
version: "1"
description: same content
---
body
`
	src2 := `---
description: same content
version: "1"
name: order-test
---
body
`
	d1, err := parseAgent(src1)
	if err != nil {
		t.Fatalf("parse src1: %v", err)
	}
	d2, err := parseAgent(src2)
	if err != nil {
		t.Fatalf("parse src2: %v", err)
	}
	if canonicalSha256(d1) != canonicalSha256(d2) {
		t.Errorf("frontmatter key order changed canonical sha256:\n%s\n%s",
			canonicalSha256(d1), canonicalSha256(d2))
	}
}

// TestParseAgent_RoundTrip exercises parseAgent end-to-end on an
// in-memory definition with the canonical frontmatter shape. Direction-C
// Phase 4 retired the only embedded agent (wrap-executor); the parser
// itself is preserved as v2-portability scaffolding for re-introduction
// when future agents land. This test guards parser+frontmatter behavior
// without depending on any specific embedded file.
func TestParseAgent_RoundTrip(t *testing.T) {
	src := `---
name: example-agent
version: "1.0"
description: example for parser regression
required_tools: [tool_a, tool_b]
forbidden_tools: [Bash]
escalation_triggers:
  - trigger_one
  - trigger_two
output_format: |
  terminal: example()
recommended_model_class: sonnet
---
You are an example agent body.
`
	def, err := parseAgent(src)
	if err != nil {
		t.Fatalf("parseAgent: %v", err)
	}
	if def.Name != "example-agent" {
		t.Errorf("name: got %q want %q", def.Name, "example-agent")
	}
	if def.RecommendedModelClass != "sonnet" {
		t.Errorf("model class: got %q", def.RecommendedModelClass)
	}
	if len(def.RequiredTools) != 2 {
		t.Errorf("required_tools: got %d entries", len(def.RequiredTools))
	}
	if len(def.EscalationTriggers) != 2 {
		t.Errorf("escalation_triggers: got %d entries", len(def.EscalationTriggers))
	}
	if !strings.Contains(def.SystemPrompt, "example agent body") {
		t.Errorf("body not parsed: %q", def.SystemPrompt)
	}
}
