# vibe-vault Architecture

## Overview

`vv` is a single Go binary that captures Claude Code sessions into an Obsidian vault.

## Data Flow

```
Claude Code SessionEnd
    → stdin: {session_id, transcript_path, cwd, hook_event_name}
    → vv hook
    → Parse full JSONL transcript (streaming, 10MB line limit)
    → Skip: file-history-snapshot, progress entries
    → Extract: user messages, assistant messages, tool uses, system events
    → Compute: duration, token totals, file changes, message counts
    → Detect: project (from cwd), domain (from config), branch (from transcript)
    → Generate: title from first meaningful user message
    → Link: find previous session in index
    → Render: Obsidian markdown with frontmatter
    → Write: Projects/{project}/sessions/YYYY-MM-DD-NN.md
    → Index: update .vibe-vault/session-index.json
```

## Transcript Format (Claude Code)

Each line in the JSONL transcript is a JSON object with:
- `type`: "user", "assistant", "system", "file-history-snapshot", "progress"
- `uuid`, `parentUuid`: message threading
- `sessionId`, `timestamp`, `cwd`, `gitBranch`, `version`: metadata
- `message`: nested object with `role`, `content`, `model`, `usage`

Content blocks in assistant messages:
- `thinking`: extended thinking (skipped for notes)
- `text`: response text
- `tool_use`: tool invocations (name, input, id)

User messages may contain:
- String content (direct user text)
- Array with `tool_result` blocks (output from tool invocations)
- Array with `text` blocks

## Token Accounting

Claude's API reports:
- `input_tokens`: non-cached prompt tokens
- `cache_creation_input_tokens`: tokens written to cache
- `cache_read_input_tokens`: tokens read from cache
- `output_tokens`: generated tokens

`vv` reports `tokens_in` as the sum of all three input categories (total context size).

## Title Heuristics

First meaningful user message is selected by skipping:
1. Resume/context instructions (`@resume`, `/resume`, `/recall`, etc.)
2. Slash commands (any message starting with `/`)
3. System-injected caveats (stripped XML tags first)
4. Short confirmations/greetings/farewells (< 80 chars matching known patterns)

Falls back to the first user message if all are filtered.

## Session Index

`.vibe-vault/session-index.json` maps session IDs to:
- Note path (relative to vault)
- Project, domain, date, iteration
- Title, model, duration
- Creation timestamp

Used for: dedup (skip already-processed sessions), iteration counting (multiple sessions per day), cross-session linking (`previous` frontmatter field).

## Package Structure

```
cmd/vv/main.go              Entry point, subcommand routing
internal/
  hook/handler.go            Stdin JSON parsing, event dispatch
  transcript/
    types.go                 Entry, Message, ContentBlock, Usage, Stats
    parser.go                JSONL streaming parser, content block extraction
    stats.go                 Stats computation, user message extraction, title heuristics
  session/
    detect.go                Project/domain/branch detection from cwd
    capture.go               Orchestrator: parse → detect → render → index
  render/markdown.go         Obsidian frontmatter + body rendering
  config/config.go           TOML config parsing (~/.config/vibe-vault/config.toml)
  index/index.go             session-index.json read/write, iteration counting
  sanitize/redact.go         XML tag stripping
```

## Planned Extensions

- **Phase 2:** LLM enrichment (OpenAI-compatible + Ollama), zstd transcript archives
- **Phase 3:** Cross-session linking, project context generation (`vv index`)
- **Phase 4:** Historical backfill (`vv backfill`), transcript reprocessing (`vv reprocess`)
