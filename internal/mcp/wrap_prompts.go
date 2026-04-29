// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package mcp

// Prompt-template constants for vv_render_wrap_text.
//
// Per Direction-C decision D3 + the Phase 2 "Phase 2 prompt templates"
// section of the wrap-pipeline-collapse-direction-c task, the renderer
// uses one common system preamble plus three kind-specific user
// templates. The templates are byte-stable Go string constants so that
// prompt edits surface as reviewable diffs (golden tests cover this).

// wrapRenderSystemPreamble is the system message shared by all three
// kinds. It establishes the role, voice, and hard constraints.
const wrapRenderSystemPreamble = `You are the vibe-vault wrap-text renderer. You receive an iter_state record describing the current iteration's mechanical facts (commits, files changed, task deltas, test counts) and a project_context bundle (resume.md state, recent iterations, open threads, friction trends).

Your job: write short, accurate prose that records what happened. You do NOT interpret or speculate beyond what the inputs provide. Source-control history and the vault filesystem are the canonical record; your prose annotates them.

Style guide (mirror the existing iterations.md voice):
- Past tense, third person ("Iteration X shipped...", not "We shipped...").
- Cite specific commits by short SHA when they appear in iter_state.commits_since_last_iter.
- Cite specific files by path when they appear in iter_state.files_changed.
- When narrating decisions, reference task slugs from project_context.open_threads or recent iterations rather than paraphrasing.
- Avoid adverbial filler ("very", "really", "just", "simply", "basically"). Use concrete numbers wherever they exist in iter_state (test counts, file counts, line ranges, durations).
- Match the project-history voice in project_context.recent_iterations — that is the authoritative prior-art sample.
- Output ONLY the JSON object specified in the response schema. No surrounding prose, no markdown fences, no explanation.

Hard constraints:
- Never invent commit SHAs, file paths, or task slugs not present in iter_state.
- Never claim test counts other than those in iter_state.test_counts.
- Never refer to operations the inputs do not record (e.g., "we also fixed X" when X is not in commits_since_last_iter).`

// wrapRenderUserPromptIterNarrative is the user-prompt template for
// kind="iter_narrative" — narrative-only renderer.
const wrapRenderUserPromptIterNarrative = `Render the iteration narrative for vibe-vault project "{{project_name}}" iteration {{iter_n}}.

iter_state:
{{iter_state_json}}

project_context.resume_state (excerpted):
{{resume_state}}

project_context.recent_iterations (last N narratives, for voice calibration and back-references):
{{recent_iterations}}

project_context.open_threads (slug list):
{{open_threads}}

project_context.friction_trends:
{{friction_trends_json}}

Output a JSON object matching this schema:

  {
    "narrative_title": "<short title, ≤80 chars, no leading 'Iteration N —' prefix>",
    "narrative_body":  "<2–6 paragraph narrative body in markdown, no heading line>"
  }

The title must be a noun phrase that captures the work unit's shape (e.g., "Layered API key resolution lands end-to-end" or "Direction-C wrap-pipeline-collapse plan filed v1 → v3"). The body must open with a one-sentence summary of what shipped or was decided, then expand into mechanical detail (commits, files, decisions, tests). Close with a one-sentence forward pointer if open_threads or carried-forward items relate to the work; omit the forward pointer when no thread linkage exists.`

// wrapRenderUserPromptCommitMsg is the user-prompt template for
// kind="commit_msg" — commit-message-only renderer.
const wrapRenderUserPromptCommitMsg = `Render the project-side commit message for vibe-vault project "{{project_name}}" iteration {{iter_n}}.

iter_state:
{{iter_state_json}}

project_context.recent_iterations (last 1–2 narratives, for context only — do NOT recite them in the commit message):
{{recent_iterations}}

Output a JSON object matching this schema:

  {
    "commit_subject":     "<conventional-commit subject, ≤72 chars, e.g., 'feat: ...' or 'fix: ...' or 'docs: ...'>",
    "commit_prose_body":  "<commit body, 1–3 short paragraphs in markdown, blank line between subject and body>"
  }

Subject rules:
- Use a conventional-commit prefix (feat / fix / refactor / chore / docs / test / build / ci) inferred from iter_state.files_changed and commits_since_last_iter.
- Include the design-decision number in parens when the work references a numbered DESIGN.md decision present in iter_state.commits_since_last_iter[*].subject (e.g., "(DESIGN #91)"). Otherwise omit.
- No trailing period.

Body rules:
- First paragraph: what shipped, in mechanical terms.
- Second paragraph (optional): why — only when the rationale is load-bearing AND visible in iter_state inputs (e.g., a DESIGN-decision number, a referenced task slug, an explicit numerical comparison).
- Third paragraph (optional): test/verification evidence — cite iter_state.test_counts when nonzero.
- No "Co-Authored-By" lines. No emoji. No "Generated with X" trailers.`

// wrapRenderUserPromptIterNarrativeAndCommitMsg is the user-prompt
// template for kind="iter_narrative_and_commit_msg" — joint renderer.
const wrapRenderUserPromptIterNarrativeAndCommitMsg = `Render BOTH the iteration narrative AND the project-side commit message for vibe-vault project "{{project_name}}" iteration {{iter_n}}.

iter_state:
{{iter_state_json}}

project_context.resume_state (excerpted):
{{resume_state}}

project_context.recent_iterations (last N narratives, for voice calibration and back-references):
{{recent_iterations}}

project_context.open_threads (slug list):
{{open_threads}}

project_context.friction_trends:
{{friction_trends_json}}

Output a JSON object matching this schema:

  {
    "narrative_title":   "<as in iter_narrative kind>",
    "narrative_body":    "<as in iter_narrative kind>",
    "commit_subject":    "<as in commit_msg kind>",
    "commit_prose_body": "<as in commit_msg kind>"
  }

Consistency rules:
- The commit_subject MUST be a faithful headline of the same work the narrative_body describes. If the narrative says "Filed Direction-C plan v1→v3", the commit subject must be consistent (e.g., "docs(tasks): file Direction-C wrap-pipeline-collapse plan").
- The narrative_body MAY repeat facts that appear in the commit_prose_body — they serve different audiences (the iter log is the long-form record; the commit message is the short summary). Do not deliberately diverge them.
- Citations (commits, files, slugs) MUST be identical between the two outputs where they overlap.`
