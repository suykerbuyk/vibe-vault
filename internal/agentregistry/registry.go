// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

// Package agentregistry holds named agent definitions (system prompt + tool
// whitelist + escalation triggers + recommended model class). It is consumed
// at runtime by the in-process wrap dispatcher (Phase 3) via Lookup() and
// also exposed over MCP via vv_get_agent_definition for v2-portability so
// orchestrators that do not embed the vibe-vault binary can read the same
// catalogue.
//
// Definitions live as embedded markdown files under agents/<name>.md with a
// YAML-style frontmatter delimited by --- lines and a freeform system prompt
// body. Embedded.go parses them at init() and panics on malformed input —
// the catalogue ships with the binary, so any error is a build defect.
package agentregistry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// AgentDefinition is the canonical, parsed representation of a single agent
// entry. Field order matches the JSON wire format and is load-bearing for
// Sha256 stability.
type AgentDefinition struct {
	Name                  string   `json:"name"`
	Version               string   `json:"version"`
	Description           string   `json:"description"`
	SystemPrompt          string   `json:"system_prompt"`
	RequiredTools         []string `json:"required_tools"`
	ForbiddenTools        []string `json:"forbidden_tools"`
	EscalationTriggers    []string `json:"escalation_triggers"`
	OutputFormat          string   `json:"output_format"`
	RecommendedModelClass string   `json:"recommended_model_class"`
	Sha256                string   `json:"sha256"`
}

// registry holds the parsed AgentDefinitions keyed by name. Populated by
// embedded.go init().
var registry = map[string]*AgentDefinition{}

// Lookup returns the registered agent definition by name, or (nil, error) if
// no such agent is registered.
func Lookup(name string) (*AgentDefinition, error) {
	def, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found in registry", name)
	}
	// Return a shallow copy so callers cannot mutate the registry. The
	// string-slice fields are also re-allocated so a caller appending to
	// them does not poison the cached entry.
	out := *def
	out.RequiredTools = append([]string(nil), def.RequiredTools...)
	out.ForbiddenTools = append([]string(nil), def.ForbiddenTools...)
	out.EscalationTriggers = append([]string(nil), def.EscalationTriggers...)
	return &out, nil
}

// List returns all registered agent definitions sorted by Name. Returned
// pointers are fresh copies — same defensive contract as Lookup.
func List() []*AgentDefinition {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*AgentDefinition, 0, len(names))
	for _, name := range names {
		def, _ := Lookup(name)
		out = append(out, def)
	}
	return out
}

// register installs def into the registry under def.Name and computes its
// canonical Sha256. It is invoked by embedded.go during init(). If two agents
// claim the same name, register panics — duplicate names indicate a build
// defect.
func register(def AgentDefinition) {
	def.Sha256 = canonicalSha256(def)
	if _, dup := registry[def.Name]; dup {
		panic(fmt.Sprintf("agentregistry: duplicate agent name %q", def.Name))
	}
	d := def
	registry[def.Name] = &d
}

// canonicalSha256 computes the sha256 of a definition's canonical JSON form.
// The Sha256 field is zeroed before marshalling so the digest is independent
// of any prior value. Slice fields are nil-coerced to empty so two equivalent
// definitions (one with []string{} versus nil) hash identically.
func canonicalSha256(def AgentDefinition) string {
	def.Sha256 = ""
	if def.RequiredTools == nil {
		def.RequiredTools = []string{}
	}
	if def.ForbiddenTools == nil {
		def.ForbiddenTools = []string{}
	}
	if def.EscalationTriggers == nil {
		def.EscalationTriggers = []string{}
	}
	data, err := json.Marshal(def)
	if err != nil {
		// Marshalling a struct of strings/slice-of-strings cannot fail in
		// practice; treat any error as fatal config corruption.
		panic(fmt.Sprintf("agentregistry: marshal canonical form: %v", err))
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// reset clears the registry. Test-only — guarded by build-tag-free package
// visibility (callers must be in the same package).
func reset() {
	registry = map[string]*AgentDefinition{}
}
