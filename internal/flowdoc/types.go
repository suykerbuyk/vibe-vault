// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package flowdoc defines the schema for `vv flowdoc gen` output —
// a language-agnostic JSON document describing a project's workflows
// (slash commands, CLI verbs, hooks, pipeline stages) as a graph of
// labeled nodes and ordered steps between them.
//
// The on-disk artifact lives at `<project>/doc/flows.json` and is
// rendered by the companion HTML viewer at `<project>/doc/FLOWS.html`.
//
// This file is the single source of truth for the `$schema_version: "1"`
// shape. Validate enforces the structural invariants the viewer and the
// `flowdoc-verify-lint` companion task rely on.
package flowdoc

import (
	"errors"
	"fmt"
)

// SchemaVersion is the only value accepted in FlowDoc.SchemaVersion for v1.
const SchemaVersion = "1"

// Enum sets for Node and Flow fields. Validate checks membership.
var (
	validLanguages = map[string]struct{}{
		"go":       {},
		"rust":     {},
		"c":        {},
		"cpp":      {},
		"python":   {},
		"doc":      {},
		"data":     {},
		"template": {},
		"external": {},
	}

	validNodeKinds = map[string]struct{}{
		"subsystem": {},
		"binary":    {},
		"library":   {}, // added 2026-05-16: Rust workspaces and other multi-crate
		"service":   {}, // languages legitimately have library nodes; cando-rs measurement
		"template":  {}, // (flowdoc-gen-source-ingestion Phase 3) emitted kind="library"
		"stage":     {}, // and the old enum aborted gen entirely.
		"external":  {},
	}

	validFlowKinds = map[string]struct{}{
		"slash-command":  {},
		"cli-verb":       {},
		"hook":           {},
		"pipeline-stage": {},
	}
)

// FlowDoc is the top-level container serialized to flows.json.
type FlowDoc struct {
	SchemaVersion string `json:"$schema_version"`
	Project       string `json:"project"`
	GeneratedAt   string `json:"generated_at"`
	Generator     string `json:"generator"`
	Nodes         []Node `json:"nodes"`
	Flows         []Flow `json:"flows"`
}

// Node is a labeled element in the project graph (a package, binary,
// template, external service, …). Node IDs must be unique within a FlowDoc.
type Node struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Role        string `json:"role,omitempty"`
	Path        string `json:"path"`
	Language    string `json:"language"`
	LayoutGroup string `json:"layout_group"`
	Kind        string `json:"kind"`
}

// Flow describes a single workflow (slash command, CLI verb, hook, or
// pipeline stage) as an ordered sequence of Steps between Nodes referenced
// by ID. Flow slugs must be unique within a FlowDoc.
type Flow struct {
	Slug        string   `json:"slug"`
	Label       string   `json:"label"`
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	Elided      string   `json:"elided,omitempty"`
	EntryPoint  string   `json:"entry_point"`
	Nodes       []string `json:"nodes"`
	Steps       []Step   `json:"steps"`
}

// Step is one directed transition in a Flow: a call/dispatch/handoff from
// one Node to another. Both From and To must reference top-level Node IDs.
type Step struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Op     string `json:"op"`
	Passes string `json:"passes,omitempty"`
	Ref    string `json:"ref,omitempty"`
}

// Validate enforces the FlowDoc v1 invariants. It returns the first error
// encountered; callers wanting a full report can re-run after fixing.
func Validate(doc *FlowDoc) error {
	if doc == nil {
		return errors.New("flowdoc: doc is nil")
	}

	if doc.SchemaVersion != SchemaVersion {
		return fmt.Errorf("flowdoc: $schema_version must be %q, got %q", SchemaVersion, doc.SchemaVersion)
	}

	if doc.Project == "" {
		return errors.New("flowdoc: project must be non-empty")
	}

	if len(doc.Nodes) == 0 {
		return errors.New("flowdoc: nodes must be non-empty")
	}

	nodeIDs, err := validateNodes(doc.Nodes)
	if err != nil {
		return err
	}

	return validateFlows(doc.Flows, nodeIDs)
}

// validateNodes checks per-node required fields, enum membership, and
// uniqueness of IDs. Returns the set of seen IDs for cross-referencing
// from Flow.Nodes / Step.From / Step.To.
func validateNodes(nodes []Node) (map[string]struct{}, error) {
	ids := make(map[string]struct{}, len(nodes))
	for i, n := range nodes {
		if n.ID == "" {
			return nil, fmt.Errorf("flowdoc: nodes[%d].id must be non-empty", i)
		}
		if _, dup := ids[n.ID]; dup {
			return nil, fmt.Errorf("flowdoc: nodes[%d].id %q is duplicated", i, n.ID)
		}
		if n.Label == "" {
			return nil, fmt.Errorf("flowdoc: nodes[%d] (id=%q) label must be non-empty", i, n.ID)
		}
		if n.Path == "" {
			return nil, fmt.Errorf("flowdoc: nodes[%d] (id=%q) path must be non-empty", i, n.ID)
		}
		if n.Language == "" {
			return nil, fmt.Errorf("flowdoc: nodes[%d] (id=%q) language must be non-empty", i, n.ID)
		}
		if _, ok := validLanguages[n.Language]; !ok {
			return nil, fmt.Errorf("flowdoc: nodes[%d] (id=%q) language %q is not a valid enum value", i, n.ID, n.Language)
		}
		if n.LayoutGroup == "" {
			return nil, fmt.Errorf("flowdoc: nodes[%d] (id=%q) layout_group must be non-empty", i, n.ID)
		}
		if n.Kind == "" {
			return nil, fmt.Errorf("flowdoc: nodes[%d] (id=%q) kind must be non-empty", i, n.ID)
		}
		if _, ok := validNodeKinds[n.Kind]; !ok {
			return nil, fmt.Errorf("flowdoc: nodes[%d] (id=%q) kind %q is not a valid enum value", i, n.ID, n.Kind)
		}
		ids[n.ID] = struct{}{}
	}
	return ids, nil
}

// validateFlows checks per-flow required fields, enum membership, slug
// uniqueness, and cross-references back to the top-level node ID set.
func validateFlows(flows []Flow, nodeIDs map[string]struct{}) error {
	slugs := make(map[string]struct{}, len(flows))
	for i, f := range flows {
		if f.Slug == "" {
			return fmt.Errorf("flowdoc: flows[%d].slug must be non-empty", i)
		}
		if _, dup := slugs[f.Slug]; dup {
			return fmt.Errorf("flowdoc: flows[%d].slug %q is duplicated", i, f.Slug)
		}
		slugs[f.Slug] = struct{}{}

		if _, ok := validFlowKinds[f.Kind]; !ok {
			return fmt.Errorf("flowdoc: flows[%d] (slug=%q) kind %q is not a valid enum value", i, f.Slug, f.Kind)
		}
		if f.EntryPoint == "" {
			return fmt.Errorf("flowdoc: flows[%d] (slug=%q) entry_point must be non-empty", i, f.Slug)
		}

		for j, nodeID := range f.Nodes {
			if _, ok := nodeIDs[nodeID]; !ok {
				return fmt.Errorf("flowdoc: flows[%d] (slug=%q) nodes[%d] references unknown node id %q", i, f.Slug, j, nodeID)
			}
		}

		for j, s := range f.Steps {
			if _, ok := nodeIDs[s.From]; !ok {
				return fmt.Errorf("flowdoc: flows[%d] (slug=%q) steps[%d].from references unknown node id %q", i, f.Slug, j, s.From)
			}
			if _, ok := nodeIDs[s.To]; !ok {
				return fmt.Errorf("flowdoc: flows[%d] (slug=%q) steps[%d].to references unknown node id %q", i, f.Slug, j, s.To)
			}
		}
	}
	return nil
}
