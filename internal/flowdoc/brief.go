// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package flowdoc

// BriefAgenticAddendum is appended to Brief when `vv flowdoc gen` runs
// in the agentic strategy. It describes the three tools (read_file,
// grep, list_dir) the model can call to explore the project itself,
// instead of receiving a pre-curated tree + file pack as in single-shot.
// The tool catalogue is also sent over the wire — this is the
// orienting prose.
const BriefAgenticAddendum = `

# Tool use

This run exposes three tools you can call to explore the project:

- read_file(path)              — read one file's contents (max 1 MiB; binary
                                  files refused).
- grep(pattern, [path_prefix]) — Go-regexp search across all kept files;
                                  up to 50 matches per call.
- list_dir(path, [recursive])  — directory listing; pass path="" for the
                                  project root.

Explore the project at your own pace. The tools see only the kept set
of source files — gitignored entries, submodule gitlinks, and
committed-noise directories (vendor/, dist/, build/, etc.) are filtered
out.

# Final response — CRITICAL

When you have enough to produce a complete flows.json, your final
message must be ONLY the JSON object. Specifically:

- Do NOT start with prose like "Now I will produce flows.json:",
  "Note that...", "Here is the result:", or any commentary.
- Do NOT end with prose like "Let me know if you need...".
- Do NOT wrap the JSON in markdown fences (`+"```"+`json or `+"```"+`).
- The first character of your final response must be `+"`{`"+`.
- The last character of your final response must be `+"`}`"+`.

The downstream parser feeds your text directly to json.Unmarshal and
fails on any leading or trailing non-JSON character. Treat this as the
single most important constraint of the task.
`

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
    "kind": "<enum>"               // subsystem|binary|library|service|template|stage|external
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

Use path:Symbol where Symbol is the bare function or type name — NOT
"func Symbol" or "type Symbol", just the identifier (e.g.
"internal/session/capture.go:Capture", not ":func Capture"). The
verifier resolves Symbol against declarations in the file and a
"func "-prefixed string never matches. Line numbers drift across
refactors — avoid them. Fall back to the bare path when no symbol applies.

# Ground-truth sources

The user message includes a listing of the project's file tree and the
contents of selected key files. They are the only authority — never
invent a path. Project layout varies by language; locate these
categories within the provided tree rather than assuming fixed paths:

- Build manifests (go.mod, CMakeLists.txt, package.json, pyproject.toml,
  Cargo.toml, Makefile, …) — declare the project's type, build targets,
  and module layout.
- Entry points (main.*, __main__.py, index.*, a file defining main(),
  the binary or service a manifest names) — where execution begins.
- Command / hook / pipeline definitions (slash-command templates, CLI
  verb dispatch, hook registrations, pipeline-stage configs) — each is
  the entry_point of a flow.
- Top-level docs (README, ARCHITECTURE, design notes) — author intent
  and naming conventions.
- Source directories — the subsystems that become nodes and the calls
  between them that become steps.

You may reference a path that appears in the tree listing even if its
contents were not provided, but do not describe behavior you cannot see.

# Constraints

- Node IDs and flow slugs are unique.
- Every flow's nodes[] and every step's from/to MUST reference an id in
  the top-level nodes[] array.
- Enum fields MUST use values from the enums above.
- Output MUST parse as a single JSON object — no fences, no prose.
`
