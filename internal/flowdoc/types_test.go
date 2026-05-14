// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

import (
	"encoding/json"
	"strings"
	"testing"
)

// validDoc returns a fresh, valid FlowDoc the tests can mutate to exercise
// individual validation rules. Constructing it in a helper keeps each
// negative-case test focused on the single field it breaks.
func validDoc() *FlowDoc {
	return &FlowDoc{
		SchemaVersion: SchemaVersion,
		Project:       "vibe-vault",
		GeneratedAt:   "2026-05-13T16:00:00Z",
		Generator:     "vv flowdoc gen v1 / test",
		Nodes: []Node{
			{
				ID:          "internal/mcp",
				Label:       "MCP server",
				Role:        "tool dispatch",
				Path:        "internal/mcp",
				Language:    "go",
				LayoutGroup: "dispatch",
				Kind:        "subsystem",
			},
			{
				ID:          "cmd/vv",
				Label:       "vv CLI",
				Path:        "cmd/vv",
				Language:    "go",
				LayoutGroup: "binary",
				Kind:        "binary",
			},
			{
				ID:          "claude-code",
				Label:       "Claude Code",
				Path:        "external/claude-code",
				Language:    "external",
				LayoutGroup: "client",
				Kind:        "external",
			},
		},
		Flows: []Flow{
			{
				Slug:        "wrap",
				Label:       "/wrap (iteration capture)",
				Kind:        "slash-command",
				Description: "Capture an iteration and stamp it into the vault.",
				EntryPoint:  "wrap.md",
				Nodes:       []string{"claude-code", "internal/mcp"},
				Steps: []Step{
					{
						From:   "claude-code",
						To:     "internal/mcp",
						Op:     "tools/call vv_collect_wrap_state",
						Passes: "{project}",
						Ref:    "internal/mcp/tools_wrap.go:CollectWrapState",
					},
				},
			},
		},
	}
}

func TestValidate_RoundTrip(t *testing.T) {
	original := validDoc()

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got FlowDoc
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if err := Validate(&got); err != nil {
		t.Fatalf("Validate after round-trip: %v", err)
	}

	// Spot-check that $schema_version survived the tag rename.
	if got.SchemaVersion != SchemaVersion {
		t.Errorf("schema_version round-trip: got %q want %q", got.SchemaVersion, SchemaVersion)
	}
	// And that omitempty optional fields persisted when present.
	if got.Flows[0].Steps[0].Ref == "" {
		t.Errorf("steps[0].ref dropped during round-trip")
	}
}

func TestValidate_OmitemptyDropsBlankOptionalFields(t *testing.T) {
	doc := validDoc()
	doc.Nodes[1].Role = "" // omitempty
	doc.Flows[0].Elided = ""
	doc.Flows[0].Steps[0].Passes = ""
	doc.Flows[0].Steps[0].Ref = ""

	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	s := string(data)
	for _, banned := range []string{`"role":""`, `"elided":""`, `"passes":""`, `"ref":""`} {
		if strings.Contains(s, banned) {
			t.Errorf("expected omitempty to drop %s; got: %s", banned, s)
		}
	}
}

func TestValidate_NilDoc(t *testing.T) {
	err := Validate(nil)
	if err == nil {
		t.Fatal("Validate(nil): expected error, got nil")
	}
	if !strings.Contains(err.Error(), "nil") {
		t.Errorf("Validate(nil): error = %q, want substring \"nil\"", err)
	}
}

// negativeCase is one row in the table-driven Validate failure suite.
// mutate breaks a specific invariant on a freshly-built valid doc, and
// wantSubstr is checked against the returned error.
type negativeCase struct {
	name       string
	mutate     func(*FlowDoc)
	wantSubstr string
}

func TestValidate_NegativeCases(t *testing.T) {
	cases := []negativeCase{
		{
			name:       "schema version wrong",
			mutate:     func(d *FlowDoc) { d.SchemaVersion = "2" },
			wantSubstr: "$schema_version",
		},
		{
			name:       "project empty",
			mutate:     func(d *FlowDoc) { d.Project = "" },
			wantSubstr: "project",
		},
		{
			name:       "nodes empty",
			mutate:     func(d *FlowDoc) { d.Nodes = nil },
			wantSubstr: "nodes must be non-empty",
		},
		{
			name:       "node id empty",
			mutate:     func(d *FlowDoc) { d.Nodes[0].ID = "" },
			wantSubstr: "id must be non-empty",
		},
		{
			name: "node id duplicated",
			mutate: func(d *FlowDoc) {
				d.Nodes[1].ID = d.Nodes[0].ID
				// also fix up the flow refs so we don't trip a different rule first
				d.Flows[0].Nodes = []string{d.Nodes[0].ID}
				d.Flows[0].Steps = nil
			},
			wantSubstr: "duplicated",
		},
		{
			name:       "node label empty",
			mutate:     func(d *FlowDoc) { d.Nodes[0].Label = "" },
			wantSubstr: "label must be non-empty",
		},
		{
			name:       "node path empty",
			mutate:     func(d *FlowDoc) { d.Nodes[0].Path = "" },
			wantSubstr: "path must be non-empty",
		},
		{
			name:       "node language empty",
			mutate:     func(d *FlowDoc) { d.Nodes[0].Language = "" },
			wantSubstr: "language must be non-empty",
		},
		{
			name:       "node language not in enum",
			mutate:     func(d *FlowDoc) { d.Nodes[0].Language = "haskell" },
			wantSubstr: "language",
		},
		{
			name:       "node layout_group empty",
			mutate:     func(d *FlowDoc) { d.Nodes[0].LayoutGroup = "" },
			wantSubstr: "layout_group must be non-empty",
		},
		{
			name:       "node kind empty",
			mutate:     func(d *FlowDoc) { d.Nodes[0].Kind = "" },
			wantSubstr: "kind must be non-empty",
		},
		{
			name:       "node kind not in enum",
			mutate:     func(d *FlowDoc) { d.Nodes[0].Kind = "widget" },
			wantSubstr: "kind",
		},
		{
			name: "flow slug duplicated",
			mutate: func(d *FlowDoc) {
				d.Flows = append(d.Flows, d.Flows[0])
			},
			wantSubstr: "duplicated",
		},
		{
			name:       "flow kind not in enum",
			mutate:     func(d *FlowDoc) { d.Flows[0].Kind = "macro" },
			wantSubstr: "kind",
		},
		{
			name:       "flow entry_point empty",
			mutate:     func(d *FlowDoc) { d.Flows[0].EntryPoint = "" },
			wantSubstr: "entry_point",
		},
		{
			name:       "flow nodes ref unknown id",
			mutate:     func(d *FlowDoc) { d.Flows[0].Nodes = []string{"nope/no-such-node"} },
			wantSubstr: "unknown node id",
		},
		{
			name:       "step from ref unknown id",
			mutate:     func(d *FlowDoc) { d.Flows[0].Steps[0].From = "ghost" },
			wantSubstr: "unknown node id",
		},
		{
			name:       "step to ref unknown id",
			mutate:     func(d *FlowDoc) { d.Flows[0].Steps[0].To = "ghost" },
			wantSubstr: "unknown node id",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := validDoc()
			tc.mutate(doc)
			err := Validate(doc)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error = %q, want substring %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

// TestValidate_EmptyFlowsAllowed confirms a doc with zero flows still
// validates — the schema requires nodes, but flows[] may be empty for
// projects in early scaffolding.
func TestValidate_EmptyFlowsAllowed(t *testing.T) {
	doc := validDoc()
	doc.Flows = nil
	if err := Validate(doc); err != nil {
		t.Fatalf("expected nil error for empty flows; got %v", err)
	}
}

// TestValidate_AllLanguageEnumValues exercises every accepted language so
// the enum map can't silently regress.
func TestValidate_AllLanguageEnumValues(t *testing.T) {
	for lang := range validLanguages {
		t.Run(lang, func(t *testing.T) {
			doc := validDoc()
			doc.Nodes[0].Language = lang
			if err := Validate(doc); err != nil {
				t.Errorf("language %q rejected: %v", lang, err)
			}
		})
	}
}

// TestValidate_AllNodeKindEnumValues exercises every accepted node kind.
func TestValidate_AllNodeKindEnumValues(t *testing.T) {
	for kind := range validNodeKinds {
		t.Run(kind, func(t *testing.T) {
			doc := validDoc()
			doc.Nodes[0].Kind = kind
			if err := Validate(doc); err != nil {
				t.Errorf("node kind %q rejected: %v", kind, err)
			}
		})
	}
}

// TestValidate_AllFlowKindEnumValues exercises every accepted flow kind.
func TestValidate_AllFlowKindEnumValues(t *testing.T) {
	for kind := range validFlowKinds {
		t.Run(kind, func(t *testing.T) {
			doc := validDoc()
			doc.Flows[0].Kind = kind
			if err := Validate(doc); err != nil {
				t.Errorf("flow kind %q rejected: %v", kind, err)
			}
		})
	}
}
