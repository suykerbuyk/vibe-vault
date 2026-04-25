# vibe-vault

Automatic session capture for Claude Code — structured Obsidian notes from every AI coding session.

## The Problem

Every AI coding session generates institutional knowledge: architectural
decisions, debugging insights, trade-offs considered and rejected, patterns
discovered. When the session ends, that knowledge evaporates into opaque JSONL
transcript files buried in `~/.claude/projects/`.

Git commits record *what* changed but rarely *why*. The reasoning, the
alternatives explored, the context that informed each decision — all of it
disappears. Sessions are the new unit of work in AI-assisted development, and
they deserve the same observability we give to code, builds, and deployments.

## The Solution

vibe-vault (`vv`) is a single Go binary that runs as a Claude Code hook. When a
session ends, it reads the JSONL transcript from stdin, extracts structured
narratives (tool activity segments, prose dialogue, git commits), detects the
project via git remote, optionally enriches the note with an LLM summary, and
writes a structured Obsidian markdown note — organized by project, cross-linked
to previous sessions, and queryable via Dataview.

It can also backfill historical sessions, archive transcripts with zstd (~10:1
compression), rebuild cross-session indexes, maintain per-project context
documents, compute session analytics, and detect friction patterns in AI
interactions. Two dependencies. Zero runtime services. Notes are always written,
even when enrichment fails.

## Sample Output

A session note produced by `vv` with LLM enrichment enabled:

```markdown
---
date: 2026-02-10
type: session
project: acme-api
branch: feat/rate-limiting
domain: work
model: claude-opus-4-6
session_id: "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
iteration: 1
duration_minutes: 47
messages: 24
tokens_in: 52000
tokens_out: 18000
tool_uses: 38
status: completed
tags: [vv-session, implementation]
commits: ["a1b2c3d", "d4e5f6a"]
friction_score: 12
summary: "feat: token-bucket rate limiter with Redis (4+4 files, tests pass)"
previous: "[[2026-02-09-02]]"
related: ["[[2026-02-08-01]]"]
---

# Add rate limiting middleware and update API docs

## What Happened

Implemented per-client rate limiting for public API endpoints. Used
token-bucket algorithm with Redis backing store for distributed deployments
and automatic in-memory fallback for local dev. Updated OpenAPI spec with
429 responses and Retry-After headers. All tests pass.

## Key Decisions

- Token bucket over sliding window — allows short bursts (better UX for
  batch operations from CI pipelines)
- Redis with in-memory fallback — production uses Redis for distributed
  state, local dev auto-falls back to Map based on REDIS_URL presence
- Rate limit headers on every response — clients can proactively throttle
  by reading X-RateLimit-Remaining

## Dialogue

> **User**: Add rate limiting to the public API endpoints
>
> **Assistant**: I'll implement a token-bucket rate limiter with Redis...

## What Changed

- `src/middleware/rate-limiter.ts` (new)
- `src/middleware/rate-limiter.test.ts` (new)
- `src/config/redis.ts` (modified)
- `docs/openapi.yaml` (modified)

## Open Threads

- [ ] Deploy to staging and monitor 429 rates
- [ ] Add per-endpoint rate limit overrides

---
*vv v0.1.0 | enriched by grok-3-mini-fast*
```

Without enrichment, notes still contain all frontmatter, the title (from the
first meaningful user message), extracted dialogue, files changed, and narrative
summary — just without the LLM-generated "What Happened", decisions, and threads.

## Table of Contents

- [Quick Start](#quick-start)
- [How It Works](#how-it-works)
- [Commands](#commands)
- [Configuration](#configuration)
- [Vault Structure](#vault-structure)
- [LLM Enrichment](#llm-enrichment)
- [Design Philosophy](#design-philosophy)
- [Cross-Project Introspection](#cross-project-introspection)
- [Roadmap — Context as Code](#roadmap--context-as-code)
- [Development](#development)
- [License](#license)

## Quick Start

### Prerequisites

- **Go 1.25+** (for building from source)
- **Obsidian** (for browsing the vault)
- **Dataview plugin** (recommended — powers the scaffolded dashboards)

### Build and Install

```bash
git clone git@github.com:suykerbuyk/vibe-vault.git
cd vibe-vault
make install    # builds vv → ~/.local/bin/vv, installs man pages
```

Ensure `~/.local/bin` is in your `$PATH`.

### Initialize a Vault

```bash
vv init ~/obsidian/vibe-vault
```

This scaffolds a full Obsidian vault with dashboards, templates, and session
capture infrastructure. It also writes a default config to
`~/.config/vibe-vault/config.toml`.

### Add Claude Code Hooks

```bash
vv hook install
```

This adds `SessionEnd`, `Stop`, and `PreCompact` hook entries to
`~/.claude/settings.json`, creating the file if it doesn't exist. A backup is
saved to `settings.json.vv.bak` before any modification. The command is
idempotent — running it again when hooks are already configured is a no-op.

To remove the hooks later: `vv hook uninstall`

- **SessionEnd** captures finalized session notes (including `/clear` events)
- **Stop** captures mid-session checkpoints (provisional notes without LLM
  enrichment that get overwritten when the session ends)
- **PreCompact** captures a checkpoint before context compaction, preserving
  full context that would otherwise be lost to summarization

### Enable the MCP Server

```bash
vv mcp install           # detects and installs into all editors (Claude Code, Zed)
vv mcp install --claude-only   # Claude Code only
vv mcp install --zed-only      # Zed only
```

This registers the vibe-vault MCP server so AI agents can query project
context, search sessions, capture new sessions, and access friction trends
on demand. Restart your editor after running this command.

To remove the MCP server later: `vv mcp uninstall`

### Verify Setup

```bash
vv check
```

Validates config, vault structure, Obsidian setup, and hook integration.

## How It Works

```
Claude Code session ends
         │
         ▼
┌──────────────────┐     stdin: {session_id, transcript_path, cwd}
│     vv hook      │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐     Parse JSONL transcript → stats, messages, files
│  Parse & Extract │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐     Segments, dialogue, commits, summary, tag
│ Narrative + Prose│
└────────┬─────────┘
         │
         ▼
┌──────────────────┐     git remote origin → project name, CWD → domain
│  Detect Project  │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐     Optional: summary, decisions, threads, tag
│  LLM Enrichment  │     (graceful skip if disabled or API unreachable)
└────────┬─────────┘
         │
         ▼
┌──────────────────┐     Correction detection, friction scoring
│ Friction Analysis│
└────────┬─────────┘
         │
         ▼
┌──────────────────┐     Score related sessions, find previous note
│  Cross-link      │
└────────┬─────────┘
         │
         ▼
   Projects/{project}/sessions/YYYY-MM-DD-NN.md
   .vibe-vault/session-index.json
         │
         ▼
┌──────────────────┐     Propagate learnings, flag stale entries,
│    Synthesis     │     update resume, retire completed tasks
└────────┬─────────┘     (enabled by default, requires LLM provider)
         │
         ▼
   Projects/{project}/knowledge.md (learnings appended)
   Projects/{project}/agentctx/resume.md (state updated)
```

**Transcript parsing** uses a streaming JSONL parser with a 10MB line buffer.
Unparseable lines are skipped — partial transcripts still produce notes.

**Narrative extraction** segments tool activity into a structured timeline,
extracts prose dialogue (user/assistant turns), detects git commit SHAs from
tool output, and generates intent-driven summaries using conventional commit
prefixes (e.g., `feat: rate limiter (4+4 files, tests pass)`).

**Friction analysis** detects user corrections in the dialogue (negation,
redirects, undo requests, quality complaints) and computes a composite friction
score (0-100) from correction density, token efficiency, file retry patterns,
error cycles, and recurring open threads.

**Project detection** checks for a `.vibe-vault.toml` identity file first
(highest priority), then extracts the repository name from `git remote get-url
origin` (stable across worktrees, renames, and machines), falling back to
`filepath.Base(cwd)` when git isn't available.

**Session dedup** uses the session index to skip already-processed transcripts
and assign same-day iteration numbers (`-01`, `-02`, etc.).

**Checkpoint capture** from Stop and PreCompact events creates provisional notes
(no enrichment, `status: checkpoint`). A subsequent checkpoint overwrites the
previous one. SessionEnd (including `/clear`) finalizes with full enrichment and
`status: completed`. PreCompact checkpoints preserve full context before
compaction summarizes it away.

**Session synthesis** runs after capture as an end-of-session judgment layer.
It gathers the session note, git diff, current knowledge and resume, recent
history, and active tasks — then asks the LLM to identify novel learnings,
flag stale entries, update the project resume, and retire completed tasks.
Learnings are deduplicated against existing knowledge using significant-word
overlap. Synthesis is enabled by default but inert without an LLM provider;
it piggybacks on the enrichment configuration.

**Cross-session linking** scores related sessions across four signals — shared
files, thread-to-resolution matching, same branch, and same activity tag — using
deterministic heuristics rather than embeddings.

## Commands

| Command | Description |
|---------|-------------|
| `vv init [path] [--git]` | Create a new vault (default: `./vibe-vault`) |
| `vv hook` | Hook mode (reads stdin from Claude Code) |
| `vv hook install` | Register hooks in `~/.claude/settings.json` |
| `vv hook uninstall` | Remove hooks from `~/.claude/settings.json` |
| `vv process <file.jsonl>` | Process a single transcript file |
| `vv index` | Rebuild session index from notes |
| `vv backfill [path]` | Discover and process historical transcripts |
| `vv archive` | Compress transcripts into vault archive |
| `vv reprocess [--project X]` | Re-generate notes from transcripts |
| `vv check` | Validate config, vault, and hook setup |
| `vv stats [--project X]` | Show session analytics and metrics |
| `vv friction [--project X]` | Show friction analysis and correction patterns |
| `vv trends [--project X]` | Show metric trends over time |
| `vv inject [--project X]` | Output session-start context payload |
| `vv export [--format X]` | Export session data (JSON or CSV) |
| `vv effectiveness [--project X]` | Analyze context effectiveness on outcomes |
| `vv vault status` | Show vault git state (branch, clean/dirty, ahead/behind) |
| `vv vault pull` | Fetch + rebase vault with automatic conflict resolution |
| `vv vault push [--message X]` | Commit all vault changes and push to remote |
| `vv context [init \| migrate \| sync]` | Manage vault-resident AI context files |
| `vv memory link` | Symlink Claude Code auto-memory into the vault |
| `vv memory unlink` | Rollback: restore host-local auto-memory |
| `vv mcp` | Start MCP server for AI agent integration |
| `vv mcp install` | Register MCP server in all detected editors |
| `vv mcp uninstall` | Remove MCP server from all detected editors |
| `vv templates [list \| diff \| show \| reset]` | Inspect, compare, and reset vault templates |
| `vv version` | Print version |

### Common Workflows

**Backfill all historical sessions:**
```bash
vv backfill                 # scans ~/.claude/projects/ by default
vv backfill ~/other/path    # scan a specific directory
```

**Archive transcripts after backfill:**
```bash
vv archive    # zstd-compress transcripts (~10:1), stores in .vibe-vault/archive/
```

**Re-generate notes after upgrading vv:**
```bash
vv reprocess                       # all sessions
vv reprocess --project myproject   # one project only
```

**View session analytics:**
```bash
vv stats                       # global stats: projects, models, activity
vv stats --project myproject   # stats for one project only
```

**Analyze friction patterns:**
```bash
vv friction                       # correction density, high-friction sessions
vv friction --project myproject   # friction for one project only
```

**Track metric trends over time:**
```bash
vv trends                          # weekly metrics with anomaly detection
vv trends --project myproject      # trends for one project only
vv trends --weeks 8                # limit display to last 8 weeks
```

**Inject session-start context:**
```bash
vv inject                                   # context for auto-detected project
vv inject --project myproject               # context for a specific project
vv inject --format json                      # output as JSON
vv inject --sections summary,sessions        # only specific sections
vv inject --max-tokens 500                   # compact output
```

**Set up vault-resident AI context for a project:**
```bash
vv context init                       # scaffold context from current directory
vv context init --project myproject   # specify project name
vv context migrate                    # copy existing RESUME.md/HISTORY.md/tasks/commands to vault
vv context sync                       # run schema migrations + deploy templates to repo
vv context sync --all                 # sync all projects (vault-only operations)
vv context sync --dry-run             # preview changes without modifying files
vv context sync --force               # overwrite user-customized files (resolve conflicts)
```

**Mirror Claude Code auto-memory in the vault:**
```bash
vv memory link                      # symlink ~/.claude/projects/{slug}/memory/ into vault
vv memory link --dry-run            # preview the migration plan without touching disk
vv memory link --force              # resolve conflicts by quarantining host-local copies
vv memory unlink                    # rollback: restore a real dir (vault copy preserved)
```

`vv memory link` computes the Claude slug from the resolved absolute working
directory (symlinks resolved, trailing slashes normalized), uses the same
project detector as the rest of `vv` (identity file → git remote → basename),
creates `Projects/{name}/agentctx/memory/` in the vault if missing, migrates
any pre-existing host-local files into the vault target, and establishes the
symlink. Running it again is a no-op. Conflicting files (same name, different
content) are refused without `--force`; with `--force` they are quarantined
under `agentctx/memory-conflicts/{timestamp}/` — a *sibling* of the memory
directory, not a child, so they do not pollute Claude Code's auto-memory
output. Only projects that already have `Projects/{name}/agentctx/` (the
vibe-vault-tracked marker) can be linked — run `vv init` or
`vv context init` first for new projects.

**Data-workflow block in `agentctx/resume.md`:**

Schema v9 (applied automatically on the next `vv context sync`) injects a
canonical "Data workflow" section into each project's
`agentctx/resume.md`, delimited by:

```
<!-- vv:data-workflow:start -->
...
<!-- vv:data-workflow:end -->
```

**MCP server for AI agent integration:**
```bash
vv mcp install                 # detect and install into all editors (then restart)
vv mcp install --claude-only   # Claude Code only
vv mcp install --zed-only      # Zed only
vv mcp uninstall               # remove from all editors
vv mcp                         # start server directly (used by editors, not run manually)
```

This exposes 20 tools including `vv_bootstrap_context` (session-start context
in one call), `vv_get_project_context`, `vv_list_projects`,
`vv_search_sessions`, `vv_get_resume`, `vv_update_resume`, `vv_get_iterations`,
`vv_manage_task`, `vv_capture_session`, `vv_list_learnings`, `vv_get_learning`,
and more. Plus
1 prompt (`vv_session_guidelines`). All names are prefixed with `vv_` to
avoid collisions with other MCP servers. AI agents call these on demand
instead of requiring pre-loaded context.

**Cross-project learnings (`Knowledge/learnings/`):** Drop markdown files
into `VibeVault/Knowledge/learnings/` to surface observations that apply
across projects (testing philosophy, resume phrasing rules, feedback
patterns). Each file uses a frontmatter header:

```markdown
---
name: Testing philosophy
description: Nothing is done until proven end-to-end with real data
type: user
---

Body content...
```

The `type` field is constrained to `user`, `feedback`, or `reference` —
`type: project` is rejected because a project-scoped memory has no
meaning in a cross-project directory. Malformed files are skipped with
a stderr warning.

Agents discover learnings on demand:

- `vv_list_learnings` returns metadata only (slug, name, description,
  type) so the agent can choose what to load — cheap enough to call
  during planning.
- `vv_get_learning(slug)` returns full content.
- `vv_bootstrap_context` adds a one-line
  `knowledge_learnings_available: {count, hint}` field **only** when at
  least one valid learning file exists, keeping the bootstrap payload
  under the /restart token budget when the directory is empty.

**Synchronize vault across machines:**
```bash
vv vault status                                    # show vault git state
vv vault pull                                      # fetch + rebase with auto conflict resolution
vv vault push                                      # commit + push (auto-generated message)
vv vault push --message "sync after refactor"      # commit + push with custom message
```

The vault is a git repository shared across machines. `vv vault pull` fetches
upstream changes and rebases local commits, automatically resolving conflicts
based on file type: auto-generated files (`history.md`, session index) accept
upstream and are regenerated locally; session notes (unique timestamps per
machine) have near-zero conflict risk; manual files (`knowledge.md`) accept
upstream but are flagged for human review. `vv vault push` stages all changes,
commits with a hostname-stamped message, and pushes — retrying once via
pull-rebase if rejected. On final failure, it surfaces the error for
interactive resolution.

The `/restart` and `/wrap` AI workflow commands call `vv vault pull` and
`vv vault push` automatically at session boundaries, keeping the vault
synchronized without manual git operations.

**Inspect and reset vault templates:**
```bash
vv templates list                                # show all templates with status
vv templates diff                                # unified diff of all customized templates
vv templates diff --file agentctx/resume.md      # diff a specific template
vv templates show session-summary.md             # print built-in default to stdout
vv templates reset --all                         # dry-run: show what would reset
vv templates reset --all --force                 # reset all templates to defaults
vv templates reset --file agentctx/resume.md --force  # reset one template
```

**Diagnose issues:**
```bash
vv check    # validates config, vault, hook setup; exit code 1 on failures
```

Every subcommand has detailed help: `vv <command> --help` or `man vv-<command>`.

## Configuration

Config lives at `~/.config/vibe-vault/config.toml` (respects `$XDG_CONFIG_HOME`).
Running `vv init` creates a default config automatically.

```toml
# Path to your Obsidian vault (~ is expanded)
vault_path = "~/obsidian/vibe-vault"

# Map workspace directories to domain labels
# Sessions from ~/work/myproject get domain: work
[domains]
work = "~/work"
personal = "~/personal"
opensource = "~/opensource"

# LLM enrichment (disabled by default)
[enrichment]
enabled = false
timeout_seconds = 10
provider = "openai"                  # any OpenAI-compatible endpoint
model = "grok-3-mini-fast"
api_key_env = "XAI_API_KEY"          # environment variable holding the key
base_url = "https://api.x.ai/v1"

# Transcript archival
[archive]
compress = true

# Session synthesis (enabled by default, requires enrichment LLM)
[synthesis]
enabled = true
timeout_seconds = 15
```

**No config file is required.** Without one, `vv` uses sensible defaults:
vault at `~/obsidian/vibe-vault`, enrichment disabled, domain detection from
standard workspace paths.

The config file is created automatically by `vv init`. You only need to edit
it to enable enrichment or customize domain paths.

## Vault Structure

`vv init` scaffolds a complete Obsidian vault:

```
vibe-vault/
├── Projects/                   # Project-centric organization
│   └── {project}/
│       ├── agentctx/           # AI context package (vv context init)
│       │   ├── CLAUDE.md       # MCP-first instructions (deployed to repo)
│       │   ├── workflow.md     # Behavioral rules and workflow standards
│       │   ├── resume.md       # Project state, architecture, decisions
│       │   ├── iterations.md   # Iteration narratives and history
│       │   ├── commands/       # Slash commands (/restart, /wrap)
│       │   ├── rules/          # Claude Code rules
│       │   ├── skills/         # Claude Code skills
│       │   ├── agents/         # Claude Code agents
│       │   └── tasks/          # Active and completed tasks
│       ├── sessions/           # Session notes (auto-generated)
│       │   └── YYYY-MM-DD-NN.md
│       ├── knowledge.md        # Per-project knowledge (manual, seeded by vv)
│       └── history.md          # Per-project context doc (auto-generated)
├── Dashboards/
│   ├── sessions.md             # All sessions (Dataview)
│   ├── by-project.md           # Sessions grouped by project
│   ├── action-items.md         # Open threads across sessions
│   └── weekly-digest.md        # Weekly activity summary
├── Templates/                  # Templater templates for manual notes
├── _archive/                   # Completed/superseded notes
├── .obsidian/                  # Obsidian config (Dataview enabled)
├── .vibe-vault/
│   ├── session-index.json      # Session dedup + cross-linking index
│   └── archive/                # zstd-compressed transcript copies
├── scripts/                    # PII pre-push hook, install script
└── doc/                        # Architecture, examples, troubleshooting
```

Session notes use the naming convention `YYYY-MM-DD-NN.md` where `NN` is the
same-day iteration number. Notes are organized by project directory, keeping
related sessions together in Obsidian's file explorer.

## LLM Enrichment

Without enrichment, notes contain extracted metadata (frontmatter), the session
title, narrative summary, dialogue excerpts, files changed, git commits, and
friction signals — all derived from the transcript alone. With enrichment
enabled, an LLM call adds:

- **What Happened** — 1-3 sentence summary (past tense, outcome-focused)
- **Key Decisions** — 0-5 decisions in "Decision — rationale" format
- **Open Threads** — 0-3 actionable items for follow-up
- **Activity tag** — one of: `implementation`, `debugging`, `planning`,
  `exploration`, `review`, `research`

### Enabling Enrichment

1. Set `enabled = true` in `[enrichment]` config
2. Set the API key environment variable (e.g., `export XAI_API_KEY=...`)
3. That's it — next session will include enriched sections

### Provider Compatibility

Any OpenAI-compatible chat completions endpoint works. Examples:

```toml
# xAI Grok (default)
[enrichment]
enabled = true
model = "grok-3-mini-fast"
api_key_env = "XAI_API_KEY"
base_url = "https://api.x.ai/v1"

# OpenAI
[enrichment]
enabled = true
model = "gpt-4o-mini"
api_key_env = "OPENAI_API_KEY"
base_url = "https://api.openai.com/v1"

# Local Ollama
[enrichment]
enabled = true
model = "llama3"
api_key_env = "OLLAMA_API_KEY"    # set to any non-empty value
base_url = "http://localhost:11434/v1"
```

### Graceful Degradation

Enrichment never blocks note creation. If the API key is unset, the endpoint is
unreachable, or the response is malformed, the note is written without enrichment
sections. A warning is logged, but the session is still captured. Notes are always
written.

## Design Philosophy

Five principles drawn from 35 iterations of development:

### Transcript-first, no external state

The binary reads the JSONL transcript directly. No dependency on MEMORY
directories, state files, or other hooks. The transcript is the single source
of truth — everything else is derived. This avoids the compounding failure modes
that broke the TypeScript predecessor.

### Two dependencies

[BurntSushi/toml](https://github.com/BurntSushi/toml) for config parsing.
[klauspost/compress](https://github.com/klauspost/compress) for zstd archival.
Enrichment uses `net/http` from stdlib — no LLM SDKs. A minimal dependency tree
matters for a tool that runs on every session end.

### Notes by project, not date

Session notes live in `Projects/{project}/sessions/` rather than a flat date-based layout.
This keeps related sessions together in Obsidian's file explorer, makes
per-project context documents natural, and mirrors how developers think about
their work — by project, not by calendar.

### Heuristic linking over embeddings

Related sessions are scored using weighted signals across four dimensions:
shared files, thread-to-resolution matching, same branch, and same activity tag.
No vector database, no embedding model, no external service. The index rebuild
stays fast and deterministic.

### Graceful degradation everywhere

Every layer degrades gracefully:
- Unparseable JSONL lines are skipped (partial transcripts still produce notes)
- Missing config falls back to sensible defaults
- Enrichment failures produce unenriched notes (never blocks capture)
- Index failures are logged as warnings (never blocks note creation)
- Stdin timeout (2s) prevents the hook from blocking Claude Code

## Cross-Project Introspection

Most AI coding tools are siloed: the agent working on project A has no knowledge
of what happened in project B. Lessons learned, patterns discovered, tasks
investigated and rejected — all confined to a single repository's context.

vibe-vault breaks this barrier. Because all projects share a single Obsidian
vault, an AI agent working in *any* project can read structured session history,
decisions, friction patterns, and task outcomes from *every other* project. This
isn't theoretical — it works today through three mechanisms:

**1. Unified vault as knowledge graph.** Session notes, task files, knowledge
documents, and iteration histories from all projects live in one searchable
vault. An agent in project A can read `Projects/B/sessions/2026-03-10-06.md`
to understand what was built, what decisions were made, and what went wrong.

**2. `vv inject` and MCP tools provide structured access.** The `vv inject`
command and MCP tools (`vv_list_projects`, `vv_search_sessions`,
`vv_get_project_context`) query across all projects. An agent can ask "what
friction patterns exist in project B?" or "what was the last session in project
C about?" and get structured answers from the vault — no JSONL transcript
parsing, no Claude Code internals.

**3. Cancelled plans prevent re-litigation.** When a task is investigated and
found not worth implementing, `/cancel-plan` records the rationale in the
project's `knowledge.md` and preserves the full analysis in `tasks/cancelled/`.
Future AI sessions — in the same project or a different one — can discover
that the work was already evaluated and why it was rejected.

### A Concrete Example

While working on vibe-vault itself, an AI agent was asked about a recent session
in the recmeet project (a completely separate C++ codebase). Without switching
projects or loading any transcripts, the agent:

- Read recmeet's session notes directly from the vault
- Reviewed the task files and discovered a `tasks/cancelled/` directory
- Understood the cancelled task's rationale and used it to inform a new
  vibe-vault feature (`/cancel-plan`)

The agent never touched Claude Code's internal storage. Everything came through
the vault layer — structured markdown files that vibe-vault maintains
automatically. This is cross-project institutional memory, accessible to any AI
agent that can read files.

## Roadmap — Context as Code

vibe-vault started as a session capture tool — a hook that turns transcripts into
Obsidian notes. But the data it collects enables something larger.

In his talk *"Stop Prompting, Start Engineering: The Context as Code Shift"*,
Dru Knox argues that we've moved from prompt engineering to **context
engineering** — and context deserves the same professional discipline we apply to
production code: version control, testing, observability, and reuse. Sessions are
not throwaway interactions; they're the primary record of how AI-assisted software
gets built.

vibe-vault is infrastructure for treating sessions as observable, structured,
queryable artifacts. The roadmap is organized around four pillars:

### Four Pillars

**1. Project Evolution Tracking** — Session notes are a chronological record of
how a project was built: what was decided, what was attempted, what failed, what
was deferred. This is the history that git commits try to be but rarely are —
decisions in full context, with the reasoning preserved.

**2. Portable AI Memory** — `vv context init` scaffolds a self-contained
`agentctx/` directory per project in the vault, containing behavioral rules,
project state, iteration history, slash commands, rules, skills, agents, and
tasks. `vv context migrate` moves existing repo-local files into `agentctx/`.
The repo gets regular files (`CLAUDE.md`, `.claude/commands/`) deployed from
the vault by `vv context sync`. MCP tools (`vv_bootstrap_context`,
`vv_update_resume`, etc.) provide direct access to vault context. Clone the
repo, run `vv context init`, and resume with full context on any machine.

**3. AI Behavioral Observability** — Every transcript logs which tools the AI
used, how many tokens it consumed, whether it needed corrections. Aggregating
this across sessions reveals friction points, model regressions, prompt gaps,
and workflow bottlenecks. This is the "observability layer" Knox describes —
built on data vv already captures.

**4. Cross-Project Intelligence** — The shared vault creates a unified knowledge
layer across all projects. AI agents can learn from decisions made in other
codebases, avoid repeating investigated-and-rejected approaches, and transfer
patterns between projects. This emerges naturally from the vault architecture
rather than requiring explicit knowledge transfer mechanisms.

### Phase Summary

| Phase | Focus | Status |
|-------|-------|--------|
| 1 | Session capture — JSONL parsing, project detection, Obsidian notes | Complete |
| 2 | LLM enrichment — summary, decisions, threads via OpenAI-compatible API | Complete |
| 3 | Cross-session intelligence — related sessions, per-project context docs | Complete |
| 4 | Backfill & archive — historical transcripts, zstd compression, reprocess | Complete |
| 5 | Foundation — project namespacing, vault migration, man pages, stop hooks | Complete |
| 6 | Session analytics — narrative extraction, prose dialogue, `vv stats`, semantic summaries | Complete |
| 7 | Behavioral analysis — correction detection, friction scoring, `vv friction` | Complete |
| 8 | Analytics & trends — dashboards, metric trends, `vv trends` | Complete |
| 9 | Portable AI memory — vault-resident context, per-project knowledge templates | Complete |
| 10 | Cross-project intelligence — unified vault queries, cancelled plan preservation, effectiveness analysis | Complete |
| 11 | Session synthesis — end-of-session judgment layer, knowledge propagation, stale detection, shared mdutil | Complete |

### Context as Code Connections

Knox's thesis maps directly onto vibe-vault's roadmap:

| Knox's Principle | vibe-vault Implementation |
|------------------|--------------------------|
| **Observability** — log every context chunk, surface "missing context" signals | Tool usage tracking, friction detection, correction patterns, `vv stats`, `vv friction` |
| **Testing** — statistical grading across runs, regression detection | `vv trends` anomaly detection, weekly metric tracking, `vv effectiveness` context impact analysis |
| **Version control** — context is versioned, auditable, diffable | Session notes are markdown in git, indexed and cross-linked |
| **Reuse** — context registries, versioned modules | Per-project `history.md`, semantic summaries, `agentctx/` portable context, cross-project knowledge transfer via shared vault |
| **CI/CD** — automated context pipelines, auto-refresh | Backfill pipeline, reprocess on upgrade, index rebuild |

### What's Out of Scope

- **Parallel eval / stress testing** — vv is a post-hoc observer, not an agent
  orchestrator. It captures what happened; it doesn't control what runs.
- **Multi-agent access controls** — vv is single-user (one session per machine
  per branch). Multi-agent coordination is an orchestration concern.
- **Real-time agent intervention** — vv captures checkpoints at natural
  boundaries (stop, compaction, clear) but does not modify agent behavior
  mid-turn.

## Development

### Build Commands

| Command | Description |
|---------|-------------|
| `make build` | Build `vv` binary |
| `make man` | Generate man pages |
| `make install` | Build + install binary and man pages to `~/.local/` |
| `make test` | Run unit tests (`-short` skips integration) |
| `make integration` | Run integration test suite |
| `make check` | Run `go vet` + all unit tests + integration (cache-busting) |
| `make pre-commit` | Run `go vet` + unit tests + integration (cache-friendly) |
| `make hooks` | Set `core.hooksPath` to `.githooks/` for pre-commit hook |
| `make vet` | Run `go vet` only |
| `make clean` | Remove binary and generated man pages |

### Test Suite

**1187 tests** across 33 test packages + **1 integration test** with 22
subtests. The integration test exercises the full pipeline:
`init` → `process` → `index` → `stats` →
`backfill` → `archive` → `checkpoint lifecycle` → `friction` →
`trends` → `inject` → `context init/migrate` → `context sync` →
`export` → `reprocess`.

```bash
make test          # unit tests only (~0.5s)
make integration   # full pipeline test (~0.3s)
make check         # everything: vet + unit + integration
```

### Stack

- Go 1.25, three direct dependencies: [BurntSushi/toml](https://github.com/BurntSushi/toml),
  [klauspost/compress](https://github.com/klauspost/compress),
  [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) (for Zed thread parsing)
- No LLM SDKs — enrichment uses `net/http` from stdlib
- No CGO — cross-compiles cleanly (pure-Go SQLite)

## License

Dual-licensed under [Apache 2.0](https://www.apache.org/licenses/LICENSE-2.0) or [MIT](https://opensource.org/licenses/MIT), at your option. See [LICENSE](LICENSE) for details.
