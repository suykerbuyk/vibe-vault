# Vibe Vault MCP Server

## What It Is

Vibe Vault includes an MCP (Model Context Protocol) server that exposes vault
data and session capture to AI agents over JSON-RPC 2.0. When configured,
Claude Code, Zed, or any MCP-compatible client can query project history,
search sessions, capture new sessions, access friction trends, and read
project knowledge directly — without reading files or running CLI commands.

```
Claude Code / Zed ←→ JSON-RPC 2.0 (stdio) ←→ vv mcp ←→ session-index.json
```

All tool and prompt names are prefixed with `vv_` to avoid collisions when
multiple MCP servers are registered in the same editor.

## Tools

The MCP server exposes 8 tools:

### `vv_get_project_context`

Returns condensed project context: recent sessions, open threads, decisions,
and friction trends. This is the JSON equivalent of `vv inject`.

**Parameters:**

| Parameter    | Type     | Default | Description |
|-------------|----------|---------|-------------|
| `project`   | string   | all     | Project name filter |
| `sections`  | string[] | all     | Which sections: `summary`, `sessions`, `threads`, `decisions`, `friction` |
| `max_tokens`| integer  | 2000    | Token budget (drops lowest-priority sections to fit) |

**Section priority** (truncation drops from the end): summary → sessions →
threads → decisions → friction.

### `vv_list_projects`

Lists all vault projects with session counts, date ranges, and friction
direction. No parameters.

### `vv_search_sessions`

Search and filter sessions by query, project, files, date range, or friction
score.

**Parameters:**

| Parameter      | Type    | Default | Description |
|---------------|---------|---------|-------------|
| `query`       | string  | —       | Full-text search across titles, summaries, decisions |
| `project`     | string  | all     | Project name filter |
| `files`       | string[]| —       | Filter by files changed |
| `after`       | string  | —       | Date range start (YYYY-MM-DD) |
| `before`      | string  | —       | Date range end (YYYY-MM-DD) |
| `min_friction`| integer | —       | Minimum friction score |
| `limit`       | integer | 10      | Maximum results |

### `vv_get_knowledge`

Returns the content of a project's `knowledge.md` file — curated conventions,
patterns, and learnings.

**Parameters:**

| Parameter | Type   | Default | Description |
|-----------|--------|---------|-------------|
| `project` | string | required | Project name |

### `vv_get_session_detail`

Returns the full markdown content of a specific session note.

**Parameters:**

| Parameter   | Type    | Default  | Description |
|------------|---------|----------|-------------|
| `project`  | string  | required | Project name |
| `date`     | string  | required | Session date (YYYY-MM-DD) |
| `iteration`| integer | 1        | Same-day iteration number |

### `vv_get_friction_trends`

Returns weekly friction scores, anomalies, and per-metric breakdowns.

**Parameters:**

| Parameter | Type    | Default | Description |
|-----------|---------|---------|-------------|
| `project` | string  | all     | Project name filter |
| `weeks`   | integer | 8       | Number of weeks to return |

### `vv_get_effectiveness`

Context effectiveness analysis — correlates context depth with session outcomes.

**Parameters:**

| Parameter | Type   | Default | Description |
|-----------|--------|---------|-------------|
| `project` | string | all     | Project name filter |

### `vv_capture_session`

Record a session note from an agent conversation. Designed for push-based
session capture from Zed or other MCP clients.

**Parameters:**

| Parameter       | Type     | Default | Description |
|----------------|----------|---------|-------------|
| `summary`      | string   | required | What was accomplished |
| `title`        | string   | auto     | Note title (defaults to first sentence of summary) |
| `tag`          | string   | auto     | Activity tag: implementation, debugging, refactor, etc. |
| `model`        | string   | —        | Model that performed the work |
| `decisions`    | string[] | —        | Key decisions made |
| `files_changed`| string[] | —        | Files that were modified |
| `open_threads` | string[] | —        | Unresolved work items |

The handler detects the project from CWD, builds a minimal transcript to
pass the triviality check, and calls `session.CaptureFromParsed()` with
`Source: "zed"` and `SkipEnrichment: true`.

## Prompts

The MCP server exposes 1 prompt:

### `vv_session_guidelines`

Agent instructions for when and how to call `vv_capture_session`. This prompt
is returned via `prompts/list` and `prompts/get` — MCP clients can present it
to agents as guidance for session capture.

**Arguments:**

| Argument  | Type   | Required | Description |
|-----------|--------|----------|-------------|
| `project` | string | no       | Project name for contextual instructions |

Prompts capability is only advertised when prompts are registered.

## How It Works Under the Hood

### Data Pipeline

```
Session Capture (vv hook)
    ↓ writes session note + updates index
session-index.json
    ↓ loaded by MCP tools
inject.Build()  →  assembles Result struct
    ↓                (recent sessions, threads, decisions, friction)
trends.Compute()  →  4-week rolling averages, anomaly detection
    ↓
inject.Render()  →  JSON with token-budget truncation
    ↓
JSON-RPC response  →  AI agent receives structured context
```

### Key data sources

- **Session index** (`~/.vibe-vault/session-index.json`): Every captured
  session with metadata — title, summary, decisions, open threads, friction
  score, files changed, commits, tokens, cost, duration, model.
- **Trend engine**: Buckets sessions into ISO weeks, computes rolling
  averages across friction, tokens/file, corrections, duration, cost.
  Flags anomalies at >1.5σ from the rolling mean.
- **History.md** (per-project, generated by `vv index`): Tiered session
  timeline, key decisions with staleness decay, open threads, friction
  patterns, recency-weighted key files. Written to
  `Projects/{project}/history.md` in the vault.

### What gets captured per session

| Field | Source |
|-------|--------|
| Title, summary, tag | LLM enrichment of transcript |
| Decisions | Extracted from narrative (architectural choices, trade-offs) |
| Open threads | Unresolved work items from the session |
| Friction score | Composite 0-100: correction density (30%), token efficiency (25%), file retry density (20%), error cycles (15%), recurring threads (10%) |
| Files changed, commits | Git diff analysis during capture |
| Token counts, cost | Transcript token accounting |
| Context available | Whether history.md and knowledge.md existed at capture time |

## Setup

### Claude Code

```bash
vv mcp install      # adds vibe-vault to ~/.claude/settings.json
# restart Claude Code
```

This adds the following to your settings:

```json
{
  "mcpServers": {
    "vibe-vault": {
      "command": "vv",
      "args": ["mcp"]
    }
  }
}
```

### Zed

```bash
vv mcp install --zed    # adds vibe-vault to ~/.config/zed/settings.json
# restart Zed
```

This adds the following to your Zed settings:

```json
{
  "context_servers": {
    "vibe-vault": {
      "command": {
        "path": "vv",
        "args": ["mcp"]
      }
    }
  }
}
```

### Verify

After restarting your editor, ask the agent to "list vibe-vault projects"
or "capture this session". The agent will call the MCP tools automatically.

### Keep the index fresh

The MCP server reads from `session-index.json`, which is updated by:

- **`vv hook`** — automatically on SessionEnd/Stop/PreCompact events
- **`vv index`** — manual full rebuild from vault notes
- **`vv reprocess`** — re-enrich existing sessions with updated LLM logic

Run `vv index` periodically (or after manual vault edits) to keep
history.md and the index in sync.

## Architecture Reference

```
cmd/vv/main.go:runMcp()          CLI entry point, registers tools + prompts
internal/mcp/protocol.go         JSON-RPC 2.0 types, MCP handshake, prompt types
internal/mcp/server.go           Stdio transport, dispatch loop (tools + prompts)
internal/mcp/tools.go            8 tool definitions and handlers
internal/mcp/prompts.go          Prompt definitions and handlers
internal/inject/inject.go        Context building and rendering
internal/trends/trends.go        Trend computation and anomaly detection
internal/index/index.go          SessionEntry struct, Load/Save
internal/index/context.go        history.md generation (ProjectContext)
internal/index/generate.go       vv index orchestration
internal/friction/score.go       Friction scoring (composite 0-100)
internal/hook/setup.go           MCP install/uninstall (Claude Code + Zed)
```

## Related Commands

| Command | Relationship to MCP |
|---------|-------------------|
| `vv inject` | Same pipeline as `vv_get_project_context`, but CLI output (md/json) |
| `vv index` | Rebuilds the session index that MCP tools read from |
| `vv hook` | Writes new sessions that appear in MCP queries |
| `vv trends` | Same trend engine used by `vv_get_friction_trends` |
| `vv stats` | Project statistics (not yet exposed via MCP) |
| `vv reprocess` | Re-enriches sessions, improving MCP query results |
| `vv effectiveness` | Same analysis as `vv_get_effectiveness`, but CLI output |
