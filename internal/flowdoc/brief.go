// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

// Brief is the system prompt for `vv flowdoc gen`. Sent verbatim to the
// chosen LLM provider; the user message instructs the model to produce
// flows.json conforming to the schema described below.
//
// Concrete language and per-field enum membership beat abstract guidance —
// LLMs do best when shown the exact JSON shape they must emit.
const Brief = `You are a code archaeologist. Read a project's source tree and emit a
single JSON document describing its workflows as a graph of nodes and
ordered steps. Return ONLY the JSON object — no prose, no markdown
fences.

# Schema ($schema_version: "1")

{
  "$schema_version": "1",
  "project": "<name>",
  "generated_at": "<RFC3339>",
  "generator": "vv flowdoc gen",
  "nodes": [{
    "id": "<unique-id>",          // e.g. "internal/mcp"
    "label": "<short>",
    "role": "<one-line>",          // optional
    "path": "<repo-relative>",
    "language": "<enum>",          // go|rust|c|cpp|python|doc|data|template|external
    "layout_group": "<group>",     // free-form; pick 5–8 groups for the project
    "kind": "<enum>"               // subsystem|binary|service|template|stage|external
  }],
  "flows": [{
    "slug": "<kebab-case>",
    "label": "<short>",
    "kind": "<enum>",              // slash-command|cli-verb|hook|pipeline-stage
    "description": "<paragraph>",
    "elided": "<caveats>",         // optional
    "entry_point": "<see below>",
    "nodes": ["<node-id>", ...],
    "steps": [{
      "from": "<node-id>", "to": "<node-id>",
      "op": "<verb>",              // "dispatch", "calls", "reads"
      "passes": "<payload>",       // optional
      "ref": "<path:Symbol>"       // optional
    }]
  }]
}

# entry_point by kind

- slash-command:  template name, e.g. "wrap.md"
- cli-verb:       "vv <verb>", e.g. "vv flowdoc gen"
- hook:           event name, e.g. "SessionEnd"
- pipeline-stage: stage name, e.g. "synthesize"

# ref policy

Use path:Symbol where Symbol is a function or type name (e.g.
"internal/session/capture.go:func Capture"). Line numbers drift across
refactors — avoid them. Fall back to the bare path when no symbol applies.

# Ground-truth sources

- cmd/, internal/, pkg/ — defines nodes and the call graph.
- templates/agentctx/commands/*.md — bodies are slash-command flows.
- internal/mcp/prompts.go — prompt bodies describe slash-command flows.

# Constraints

- Node IDs and flow slugs are unique.
- Every flow's nodes[] and every step's from/to MUST reference an id in
  the top-level nodes[] array.
- Enum fields MUST use values from the enums above.
- Output MUST parse as a single JSON object — no fences, no prose.
`
