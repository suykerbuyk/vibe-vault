# What Gets Captured

This page defines exactly what data `vv` uses to generate session notes.

## Session note inputs

1. Hook event payload via stdin (`session_id`, `transcript_path`, `cwd`, `hook_event_name`).
2. Full JSONL transcript — every message, tool use, and tool result.
3. Git branch from transcript metadata.
4. Working directory for project/domain detection.

## Extracted fields

- **Stats:** duration, user/assistant message counts, tool use counts, token usage (input + cache + output)
- **Files changed:** tracked from Write, Edit, NotebookEdit tool uses
- **Project:** last path component of `cwd`
- **Domain:** mapped from `cwd` against configured workspace directories
- **Branch:** from transcript `gitBranch` field
- **Model:** from assistant message metadata (skips synthetic models)
- **Title/summary:** first meaningful user message (skips resume instructions, confirmations, farewells)
- **Session linking:** `previous` field links to last session for same project via session index

## Not captured (by design)

- Thinking block content (internal reasoning)
- Tool result output (stdout/stderr from commands)
- Binary artifacts
- Secret values (sanitization strips XML tags; PII scanning at push time)

## Future enrichment outputs (Phase 2)

Session notes will include when LLM enrichment is enabled:
- Narrative summary (what happened and why)
- Key decisions and rationale
- Open threads / action items
- Enrichment metadata in frontmatter (`summary_engine`, `summary_model`)

## Knowledge extraction

Via `/distill` command (manual):
- `Knowledge/decisions/*.md`
- `Knowledge/patterns/*.md`
- `Knowledge/learnings/*.md`

Each note links back via `source_sessions`.
