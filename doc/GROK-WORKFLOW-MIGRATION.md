# Zed + Grok Workflow Guide

**Status:** Current-state operator guide. Most of what was originally
planned as a forward-looking migration has shipped. This document
describes how to run vibe-vault's "Context as Code" workflow inside
Zed with Grok as the agentic provider, what is already wired,
how to set it up, and what remains aspirational.

**Audience:** operators standing up a Zed + Grok workflow today,
and contributors picking up the remaining ACP-track work.

---

## What this is for

Vibe-vault was built around Claude Code: SessionEnd / Stop /
PreCompact hooks emit JSONL transcripts, `vv hook` parses them,
and the vault accumulates structured session notes plus
`agentctx/` context (resume, iterations, tasks, workflow rules).
Slash commands (`/restart`, `/wrap`, `/execute-plan`) are just
markdown files under `agentctx/commands/` — Claude Code reads
them and follows the instructions.

The same model runs in Zed with Grok as the model behind the
agent panel. The transcripts come from a different shape (Zed's
SQLite `threads.db` instead of Claude Code JSONL), the agent
calls `vv` MCP tools instead of `vv inject` via stdin, and the
provider routes to xAI instead of Anthropic — but `agentctx/`
is unchanged and the slash-command markdown files are read by
Grok the same way Claude Code reads them.

---

## Current state — what is already shipped

| Capability                          | Status      | Reference |
| ----------------------------------- | ----------- | --------- |
| MCP server install into Zed         | shipped     | `vv mcp install [--zed-only]` writes `~/.config/zed/settings.json:context_servers.vibe-vault` |
| Zed thread import (bulk)            | shipped     | `vv zed backfill` — SQLite read of `threads.db` + zstd decompress + render |
| Zed thread auto-capture (live)      | shipped     | `vv zed watch` and the MCP server's background watcher (DESIGN #40) |
| `vv_capture_session` MCP tool       | shipped     | Push-based capture from any MCP client (Zed agent panel included) |
| 43 MCP tools + 1 prompt             | shipped     | Run `vv mcp check --tools` for the full list |
| Grok as a first-class provider      | shipped iter 230 | `enrichment.provider = "grok"` is the default; `vv config set-key grok` resolves `XAI_API_KEY` |
| `XAI_API_KEY` MCP env passthrough   | shipped iter 229 | Plugin `.mcp.json` propagates the key to dispatched MCP subprocesses |
| Slash-command markdown files        | shipped     | `templates/agentctx/commands/{restart,wrap,execute-plan,review-plan,...}.md` synced via `vv context sync` |
| Zed-as-pluggable-SessionSource (γ Phase 1) | shipped iter 223 | `internal/zed/source.go` registers `zed-acp` source against the `SessionSource` registry |

What this means in practice: a freshly installed Zed paired
with `vv mcp install --zed-only` and an `XAI_API_KEY` in the
operator's environment can call any of the 43 `vv_*` tools
from the agent panel and have its threads auto-captured into
the vault on a 5-minute debounce. Type "Run /restart" in the
agent panel and Grok reads `agentctx/commands/restart.md`
through `vv_get_workflow` (or by reading the file directly)
and follows the instructions, including calling
`vv_bootstrap_context` for full project state.

What remains (Track C, deferred): native ACP-stream
capture — wiring the Source's `Start()` into the running
agent panel rather than reading the SQLite database after
the fact. See "Remaining work" below.

---

## Architecture

```
~/.config/zed/settings.json (context_servers.vibe-vault)
        │
        ▼
   Zed agent panel ──── grok-3-mini-fast / grok-4-fast (xAI)
        │
        ▼ JSON-RPC 2.0 over stdio
   ┌──────────────┐
   │  vv mcp      │   43 tools, 1 prompt — see VIBE_VAULT_MCP.md
   └──────┬───────┘
          │ background watcher (fsnotify on threads.db-wal)
          ▼
   ~/.local/share/zed/threads/threads.db
          │ (read-only SQLite + zstd decompress)
          ▼
   Thread → Transcript → Note (same render pipeline as Claude Code)
          │
          ▼
   Vault: Projects/<p>/sessions/<host>/<date>.md
   plus session-index.json
```

Three capture paths (DESIGN #40):

- **Explicit (push-based):** Grok calls `vv_capture_session` with
  an agent-curated summary. Highest fidelity — the agent supplies
  decisions, files-changed, open threads. Bypasses LLM enrichment
  (`SkipEnrichment: true`).

- **Automatic (background watcher):** the MCP server runs an
  fsnotify watcher on `threads.db-wal`. Quiet for `[zed]
  debounce_minutes` (default 5), then reads the new threads,
  parses them, and renders notes with `status: auto-captured`.
  Falls back to LLM enrichment for the summary.

- **Bulk backfill:** `vv zed backfill` scans the entire
  `threads.db` and renders notes for every thread not already
  in `session-index.json`. Used to seed the vault from
  pre-existing Zed history.

Explicit captures take precedence: when both fire for the same
thread (rare), the explicit summary wins.

---

## Setup — getting it running today

### 1. Install vibe-vault MCP into Zed

```sh
vv mcp install --zed-only
# or, to install into both Claude Code and Zed:
vv mcp install
```

This writes a `context_servers.vibe-vault` block into
`~/.config/zed/settings.json`:

```json
"context_servers": {
  "vibe-vault": {
    "command": "vv",
    "args": ["mcp"]
  }
}
```

Existing settings are preserved; a backup lands at
`settings.json.vv.bak`. Restart Zed; the agent panel should
list 43 `vv_*` tools.

Verify with `vv mcp check`. The plugin path
(`vv mcp install --claude-plugin`) is Claude-Code-specific
and does not apply here.

### 2. Configure Grok as the provider

The default config has Grok as the enrichment provider already
(iter 230). Set the API key:

```sh
vv config set-key grok    # prompts for the key, writes to ~/.config/vibe-vault/config.toml
# or
export XAI_API_KEY=xai-...
```

`llm.ResolveAPIKey` resolves config first, then env. Verify:

```sh
vv check --json | jq '.checks[] | select(.name=="enrichment")'
```

Should report `pass` with detail like `grok/grok-4-fast (API
key set)`. The MCP plugin propagates `XAI_API_KEY` into
dispatched subprocesses; Zed inherits it from the operator
shell.

Override the model in config if needed:

```toml
[enrichment]
provider = "grok"
model = "grok-4-fast"     # or grok-3-mini-fast (default), grok-3, etc.

[providers.grok]
# api_key = "..."           # or via vv config set-key grok / XAI_API_KEY
# base_url = "..."          # optional; only set to override xAI's default endpoint
```

`providers.grok.base_url` overrides `enrichment.base_url`; the
legacy `enrichment.base_url` is retained for backwards
compatibility but per-provider is the preferred axis.

### 3. Configure session capture

Defaults are usually right. The full `[zed]` block:

```toml
[zed]
db_path = ""              # empty = ~/.local/share/zed/threads/threads.db
debounce_minutes = 5      # quiet period before auto-capture fires
auto_capture = true       # set false to disable the MCP background watcher
```

Bulk-import existing threads:

```sh
vv zed backfill                # default DB path
vv zed list                    # list parsed threads (does not capture)
vv zed watch --debounce 1m     # standalone watcher (alternative to MCP background)
```

The standalone `vv zed watch` is intended for when the MCP
server is not running (e.g. CLI-only workflow). When `vv mcp`
is registered in Zed and `[zed] auto_capture = true`, the
background watcher inside the MCP server handles capture; the
standalone command becomes redundant.

### 4. Bring slash commands into the project

```sh
vv context sync   # in each project root
```

This populates `<project>/agentctx/commands/{restart,wrap,
execute-plan,review-plan,cancel-plan,features-split,license,
makefile}.md` from the embedded templates. Grok reads these
the same way Claude Code does — they are plain markdown with
instructions; no editor-specific interpreter is required.

---

## Session capture in detail

This is the section that was previously underspecified. The
short version: there is no "Zed extension that hooks events"
analogous to Claude Code's `SessionEnd / Stop / PreCompact`.
Zed instead writes a SQLite database; vibe-vault reads it.

### File layout

```
~/.local/share/zed/threads/
├── threads.db       # SQLite, the canonical store
├── threads.db-wal   # write-ahead log; fsnotify target for low-latency change detection
└── threads.db-shm   # shared-memory; not watched
```

Threads are stored compressed (zstd) inside SQLite blob columns
with Rust-style enum JSON (`{"User": {...}}`, `{"Agent": {...}}`)
that requires custom unmarshaling. The full schema mapping lives
in `internal/zed/types.go` and the parser in
`internal/zed/parser.go`.

### Watcher behavior

`internal/zed/watcher.go:Watch()` opens `threads.db-wal` with
fsnotify, debounces writes for `cfg.Debounce` (default 5min), and
fires `onChange()` exactly once per quiet period. The default
balances "capture promptly" against "don't capture mid-conversation
and re-capture three times as Grok finishes each turn."

The MCP server starts the watcher as a goroutine on
`vv mcp` startup when `[zed] auto_capture = true` and the
`zed-acp` source's `Enabled()` reports true (i.e., DB path
exists). On change, the watcher rescans, identifies new
threads not in the index, and renders them.

### Conversion pipeline

```
parser.ParseDB()          ← SQLite + zstd, read-only
  └─→ Thread (ZedMessage[], ZedContent blocks)
        │
        ├─→ convert.Convert()   → transcript.Transcript
        │       (28-entry tool name normalization, mention→text,
        │        per-request token aggregation)
        │
        ├─→ detect.DetectProject() → session.Info
        │       (worktree path basename, snapshot branch,
        │        config-based domain — no git subprocess)
        │
        ├─→ narrative.ExtractNarrative() → Activities, tags, decisions
        │       (single-segment; commit extraction from terminal blocks)
        │
        └─→ prose.ExtractDialogue() → readable session-dialogue prose
```

The render pipeline downstream of `Transcript` is the same
shared code that Claude Code JSONL flows through. From
`Transcript` onward, the source is invisible — same friction
analysis, same enrichment, same note format. The
`source: zed-acp` frontmatter field is the only consumer-visible
marker.

### Three explicit-capture entry points

1. `vv_capture_session` MCP tool. Designed for Grok to call at
   end-of-work-unit. Required: `summary`. Optional: `tag`,
   `decisions`, `files_changed`, `open_threads`, `model`.

2. `vv_session_guidelines` prompt. Returned by `prompts/list`
   so Zed can present it to Grok as agent guidance — "when
   to call `vv_capture_session`, what to put in each field."

3. Manual: `vv zed backfill --since <date>` re-renders missed
   threads after a watcher gap.

### What gets dropped on the floor

Compared to Claude Code's hook-based capture:

- **Mid-session checkpoints.** Claude's Stop event creates
  `status: checkpoint` notes. Zed has no equivalent event;
  capture is end-of-thread only.
- **PreCompact context injection.** Claude calls `vv inject`
  before context compaction; Zed has no such trigger. Grok
  must call `vv_bootstrap_context` (or `vv_get_resume`)
  explicitly at session start. The `/restart` markdown
  command embeds this requirement.
- **Real-time tool-call telemetry.** Threads are observed
  post-hoc, not streamed. The remaining ACP-track work
  (below) closes this gap if needed.

These are honest gaps. Most workflows do not need them.

---

## Zed extension opportunities

Zed has a real extension system (Rust crates, exposed via the
`zed::extension::Extension` trait) and a non-extension config
surface (`tasks.json`, `keymap.json`, slash-command stubs in
the agent panel). None of the following are shipped — they
are concrete extension points where vibe-vault could land
more native UX. Treat as design notes for future contributions.

### Tier 1 — config-only (no extension needed)

**Keybindings into `vv` commands.** Add to
`~/.config/zed/keymap.json`:

```json
[
  {
    "context": "Workspace",
    "bindings": {
      "cmd-shift-w": ["task::Spawn", { "task_name": "vv-wrap" }],
      "cmd-shift-r": ["task::Spawn", { "task_name": "vv-restart-context" }]
    }
  }
]
```

paired with `~/.config/zed/tasks.json`:

```json
[
  {
    "label": "vv-wrap",
    "command": "vv",
    "args": ["inject", "--project", "$ZED_WORKTREE_NAME"],
    "reveal": "always"
  },
  {
    "label": "vv-restart-context",
    "command": "vv",
    "args": ["inject"],
    "reveal": "always"
  }
]
```

This gives keybinding-driven access to vibe-vault commands
without writing any code, and works today.

**Agent-panel "stock prompts."** Zed's agent panel supports
saved prompts. Pre-author one per slash command — the prompt
body is "Read `agentctx/commands/restart.md` and follow it"
for `/restart`, etc. Grok consumes the markdown the same way
Claude Code does.

### Tier 2 — Zed extension (Rust)

A `zed-vibe-vault` extension could expose:

- **Slash-command surface in the assistant.** The Zed
  extension API exposes assistant-side slash commands
  (`SlashCommand` trait). A vibe-vault extension could
  register `/wrap`, `/restart`, etc. that resolve to the
  corresponding `agentctx/commands/*.md` body, returning
  it as the slash command's output for Grok to consume.
  This makes the slash-command UX symmetric with Claude
  Code without changing the underlying markdown files.

- **Status-bar capture indicator.** Listen on the
  `vv_capture_session` MCP tool calls and render
  "captured Nm ago" in the status bar — useful operator
  feedback that the agent is wrapping up correctly.

- **Codelens / inline insights.** Surface `vv friction`
  counts adjacent to recently-edited regions, or
  `vv search-sessions` results inline. Reads-only against
  the vault; no write coupling.

Each of these is one or two days of Rust work against the
Zed extension API. None are blockers — Tier 1 alone covers
the operator-facing UX gap.

### Tier 3 — ACP-stream native capture (Track C)

This is the remaining migration work. See "Remaining work"
below.

---

## Pair-programming workflow rules

Grok consumes the same workflow rules Claude Code does — they
are project-resident markdown, not editor-coupled config.
`vv_get_workflow` returns `agentctx/workflow.md`;
`vv_bootstrap_context` returns workflow + resume + active
tasks in one call.

Key rules that survive the migration verbatim:

- Investigate before implementing; plan for non-trivial
  changes; verify before declaring done.
- Use subagents for parallel research; one concern per
  subagent.
- After a correction, save the pattern (auto-memory or
  `Knowledge/learnings/`) so the same mistake doesn't
  recur.
- Never mark a task complete without proving it works.
- Never commit AI-attribution trailers
  (`Co-Authored-By` etc.).

These are model-agnostic. Grok follows them when given
`vv_get_workflow` at session start, the same way any other
LLM does. The `/restart` slash command in
`agentctx/commands/restart.md` is the canonical bootstrap;
running it gets Grok aligned with project state in one call.

---

## Remaining work — Track C / ACP-native capture

The Source already carries a name: `zed-acp` (see
`internal/zed/source.go:SourceName`). The "ACP" in the name
is the Zed Agent Client Protocol — the streaming protocol
the agent panel uses internally to communicate with the
running model. Today, vibe-vault's Zed source reads the
SQLite database that ACP writes to, not the ACP stream
itself.

**Why the gap is acceptable today:** SQLite-watcher capture
is end-of-thread-only with a 5-minute debounce. For most
post-mortem session-note workflows, that is sufficient. The
prose-extraction and friction-analysis pipelines run
identically on either input.

**Why it matters for parity:** ACP-stream capture would
enable mid-session checkpoint notes (matching Claude Code's
Stop event) and lower latency on auto-capture. It is also
the first concrete consumer of the iter-201 two-tier vault
+ pluggable-source decision.

**Promotion criteria:** `zed-session-export-hook` was
retired iter 132 with this thread carried forward. Re-open
as a task only if (a) capture rate via the SQLite watcher
falls below ~80% of expected threads, or (b) operator
workflow demand for mid-session checkpoints
materializes. Until then, the watcher path is correct.

References:

- DESIGN #40 — Zed hybrid capture (explicit + watcher)
- DESIGN #103 — Two-tier vault + pluggable session source
- `doc/SESSION-CAPTURE-ARCHITECTURE.md` — full architectural
  rationale for the two-tier vault and source registry.

---

## Limitations and caveats

- **Zed agent panel does not auto-call `vv_capture_session`.**
  Grok must be prompted (or guided by `vv_session_guidelines`)
  to call it. The watcher fallback compensates but with
  lower fidelity (LLM-summarized rather than agent-curated).
- **SQLite read-only assumption.** Vibe-vault never writes to
  `threads.db`. If Zed changes its schema, the parser will
  need an update — pinned via `internal/zed/parser_test.go`
  fixtures.
- **No PreCompact equivalent.** When Zed compacts a long
  thread, vibe-vault sees the compaction after the fact.
  Long sessions should be wrapped (via `/wrap` slash command
  or `vv_capture_session`) before compaction kicks in.
- **API key surface.** `XAI_API_KEY` lands in
  `~/.config/vibe-vault/config.toml` (mode 0600) when set
  via `vv config set-key grok`. If the config has ever been
  shared or screen-recorded, rotate the key. See
  `api-key-plaintext-rotation-awareness` in resume.md.
- **Provider lock-in is shallow.** Grok routes through
  `internal/llm/openaihttp.go` (xAI is OpenAI-compatible).
  Switching to another OpenAI-compatible provider is a
  config change, not code.

---

## Quick reference

| Task                                    | Command |
| --------------------------------------- | ------- |
| Install MCP into Zed                    | `vv mcp install --zed-only` |
| Verify install                          | `vv mcp check` |
| Set Grok API key                        | `vv config set-key grok` (or `export XAI_API_KEY=...`) |
| Surface check                           | `vv check --json` |
| Bulk-import existing Zed threads        | `vv zed backfill` |
| List parsed threads                     | `vv zed list` |
| Standalone watcher (no MCP)             | `vv zed watch [--debounce 1m]` |
| Sync slash-command markdown to project  | `vv context sync` |
| Inspect MCP tool surface                | `vv mcp check --tools` |
| Bootstrap project context (from Grok)   | call `vv_bootstrap_context` |
| Capture a session (from Grok)           | call `vv_capture_session` with summary + decisions |

---

## Conclusion

The "Grok migration" is mostly retrospective. The Zed
integration shipped over Tracks A and B (iter 132 closeout
of `zed-session-export-hook`); Grok-as-first-class shipped
in iter 230. What remains is Track C — native ACP-stream
capture — and it is a deliberately-deferred enhancement, not
a blocker.

Operators standing up a fresh Zed + Grok workflow today
should follow the four-step setup above. Contributors looking
for impact should consider the Tier-2 Zed extension ideas
(slash-command surface in the assistant; status-bar capture
indicator) as the highest-leverage native-UX work, and
Track C if mid-session capture latency becomes a real
constraint.
