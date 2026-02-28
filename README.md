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
session ends, it reads the JSONL transcript from stdin, extracts metadata (duration,
tokens, files changed, branch), detects the project via git remote, optionally
enriches the note with an LLM summary, and writes a structured Obsidian markdown
note — organized by project, cross-linked to previous sessions, and queryable via
Dataview.

It can also backfill historical sessions, archive transcripts with zstd (~10:1
compression), rebuild cross-session indexes, and maintain per-project context
documents. Two dependencies. Zero runtime services. Notes are always written,
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
status: completed
tags: [cortana-session, implementation]
summary: "Implemented token-bucket rate limiter with Redis backing store"
previous: "[[2026-02-09-02]]"
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
first meaningful user message), and the files-changed section — just without the
LLM-generated summary, decisions, and threads.

## Table of Contents

- [Quick Start](#quick-start)
- [How It Works](#how-it-works)
- [Commands](#commands)
- [Configuration](#configuration)
- [Vault Structure](#vault-structure)
- [LLM Enrichment](#llm-enrichment)
- [Design Philosophy](#design-philosophy)
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

Add this to `~/.claude/settings.json`:

```json
{
  "hooks": {
    "SessionEnd": [
      {
        "matcher": "",
        "hooks": [{"type": "command", "command": "vv hook"}]
      }
    ],
    "Stop": [
      {
        "matcher": "",
        "hooks": [{"type": "command", "command": "vv hook"}]
      }
    ]
  }
}
```

The `SessionEnd` hook captures finalized session notes. The `Stop` hook captures
mid-session checkpoints (provisional notes without LLM enrichment that get
overwritten when the session ends).

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
┌──────────────────┐     Score related sessions, find previous note
│  Cross-link      │
└────────┬─────────┘
         │
         ▼
   Sessions/{project}/YYYY-MM-DD-NN.md
   .vibe-vault/session-index.json
```

**Transcript parsing** uses a streaming JSONL parser with a 10MB line buffer.
Unparseable lines are skipped — partial transcripts still produce notes.

**Project detection** extracts the repository name from `git remote get-url
origin` (stable across worktrees, renames, and machines), falling back to
`filepath.Base(cwd)` when git isn't available.

**Session dedup** uses the session index to skip already-processed transcripts
and assign same-day iteration numbers (`-01`, `-02`, etc.).

**Checkpoint capture** from Stop events creates provisional notes (no enrichment,
`status: checkpoint`). A subsequent Stop overwrites the previous checkpoint.
SessionEnd finalizes with full enrichment and `status: completed`.

**Cross-session linking** scores related sessions across four signals — shared
files, thread-to-resolution matching, same branch, and same activity tag — using
deterministic heuristics rather than embeddings.

## Commands

| Command | Description |
|---------|-------------|
| `vv init [path] [--git]` | Create a new vault (default: `./vibe-vault`) |
| `vv hook [--event <name>]` | Hook mode (reads stdin from Claude Code) |
| `vv process <file.jsonl>` | Process a single transcript file |
| `vv index` | Rebuild session index from notes |
| `vv backfill [path]` | Discover and process historical transcripts |
| `vv archive` | Compress transcripts into vault archive |
| `vv reprocess [--project X]` | Re-generate notes from transcripts |
| `vv check` | Validate config, vault, and hook setup |
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
├── Sessions/                   # Session notes, organized by project
│   └── {project}/
│       ├── YYYY-MM-DD-NN.md    # Session notes (auto-generated)
│       └── _context.md         # Per-project context doc (auto-generated)
├── Knowledge/
│   ├── decisions/              # Architectural decisions
│   ├── patterns/               # Reusable patterns
│   └── learnings/              # Lessons learned
├── Projects/                   # Project index notes
├── Dashboards/
│   ├── sessions.md             # All sessions (Dataview)
│   ├── by-project.md           # Sessions grouped by project
│   ├── decisions.md            # Decision log
│   ├── action-items.md         # Open threads across sessions
│   └── weekly-digest.md        # Weekly activity summary
├── Templates/                  # Templater templates for manual notes
├── _archive/                   # Completed/superseded notes
├── .obsidian/                  # Obsidian config (Dataview enabled)
├── .vibe-vault/
│   ├── session-index.json      # Session dedup + cross-linking index
│   └── archive/                # zstd-compressed transcript copies
├── scripts/                    # PII pre-push hook, install script
└── docs/                       # Architecture, examples, troubleshooting
```

Session notes use the naming convention `YYYY-MM-DD-NN.md` where `NN` is the
same-day iteration number. Notes are organized by project directory, keeping
related sessions together in Obsidian's file explorer.

## LLM Enrichment

Without enrichment, notes contain extracted metadata (frontmatter), the session
title, and files changed. With enrichment enabled, an LLM call adds:

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

Five principles drawn from 16 iterations of development:

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

Session notes live in `Sessions/{project}/` rather than `Sessions/YYYY/MM/`.
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
queryable artifacts. The roadmap is organized around three pillars:

### Three Pillars

**1. Project Evolution Tracking** — Session notes are a chronological record of
how a project was built: what was decided, what was attempted, what failed, what
was deferred. This is the history that git commits try to be but rarely are —
decisions in full context, with the reasoning preserved.

**2. Portable AI Memory** — RESUME.md, HISTORY.md, and task files are the AI's
working memory for a project. Today they live as untracked files in each repo.
Moving them into the vault makes project context survive machine migrations,
searchable across the portfolio, and accessible to any session that knows the
project name.

**3. AI Behavioral Observability** — Every transcript logs which tools the AI
used, how many tokens it consumed, whether it needed corrections. Aggregating
this across sessions reveals friction points, model regressions, prompt gaps,
and workflow bottlenecks. This is the "observability layer" Knox describes —
built on data vv already captures.

### Phase Summary

| Phase | Focus | Status |
|-------|-------|--------|
| 1 | Session capture — JSONL parsing, project detection, Obsidian notes | Complete |
| 2 | LLM enrichment — summary, decisions, threads via OpenAI-compatible API | Complete |
| 3 | Cross-session intelligence — related sessions, per-project context docs | Complete |
| 4 | Backfill & archive — historical transcripts, zstd compression, reprocess | Complete |
| 5 | Foundation — project namespacing, vault migration, man pages, stop hooks | Mostly complete |
| 6 | Session analytics — model tracking, computed metrics, `vv stats`, dashboards | Planned |
| 7 | Behavioral analysis — correction detection, repeated instructions, friction signals | Planned |
| 8 | Model comparison — model-vs-model stats, regression detection, trend analysis | Planned |
| 9 | Provenance — git commit linkage, timeline export, signed notes | Planned |
| 10 | Knowledge distillation — learning capture, knowledge notes, cross-project patterns | Planned |

### Context as Code Connections

Knox's thesis maps directly onto vibe-vault's roadmap:

| Knox's Principle | vibe-vault Implementation |
|------------------|--------------------------|
| **Observability** — log every context chunk, surface "missing context" signals | Tool usage tracking (Phase 5), friction detection (Phase 7), correction patterns (Phase 7) |
| **Testing** — statistical grading across runs, regression detection | Model comparison (Phase 8), trend analysis across sessions |
| **Version control** — context is versioned, auditable, diffable | Session notes are markdown in git, indexed and cross-linked |
| **Reuse** — context registries, versioned modules | Per-project `_context.md` (today), knowledge notes (Phase 10) |
| **CI/CD** — automated context pipelines, auto-refresh | Backfill pipeline, reprocess on upgrade, index rebuild |

### What's Out of Scope

- **Parallel eval / stress testing** — vv is a post-hoc observer, not an agent
  orchestrator. It captures what happened; it doesn't control what runs.
- **Pre-flight validation** — vv runs after the session ends. It has no hook
  into session start.
- **Multi-agent access controls** — vv is single-user, single-machine.
  Multi-agent coordination is an orchestration concern.
- **Real-time agent intervention** — mid-session intervention requires hooks
  that don't exist in Claude Code's current model.

## Development

### Build Commands

| Command | Description |
|---------|-------------|
| `make build` | Build `vv` binary |
| `make man` | Generate man pages |
| `make install` | Build + install binary and man pages to `~/.local/` |
| `make test` | Run unit tests (`-short` skips integration) |
| `make integration` | Run integration test suite |
| `make check` | Run `go vet` + all unit tests + integration |
| `make vet` | Run `go vet` only |
| `make clean` | Remove binary and generated man pages |

### Test Suite

**139 unit tests** across 16 test files + **1 integration test** with 10
subtests. The integration test exercises the full pipeline:
`init` → `process` → `index` → `backfill` → `archive` → `reprocess` →
`checkpoint lifecycle`.

```bash
make test          # unit tests only (~0.5s)
make integration   # full pipeline test (~0.3s)
make check         # everything: vet + unit + integration
```

### Stack

- Go 1.25, two dependencies: [BurntSushi/toml](https://github.com/BurntSushi/toml),
  [klauspost/compress](https://github.com/klauspost/compress)
- No LLM SDKs — enrichment uses `net/http` from stdlib
- No CGO — cross-compiles cleanly

## License

MIT — see [LICENSE](LICENSE).
