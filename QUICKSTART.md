# vibe-vault Quickstart

Get from zero to a fully populated Obsidian vault in under five minutes.

## 1. Build and Install

```bash
git clone git@github.com:suykerbuyk/vibe-vault.git
cd vibe-vault
make install
```

This builds the `vv` binary and copies it to `~/.local/bin/`. Ensure
`~/.local/bin` is in your `$PATH`:

```bash
# Add to ~/.bashrc or ~/.zshrc if not already present
export PATH="$HOME/.local/bin:$PATH"
```

Verify:

```bash
vv version
```

## 2. Create Your Vault

Pick a name and location for your Obsidian vault. The path argument is the
full directory — the last component becomes the vault name.

```bash
vv init ~/obsidian/my-sessions
```

This does three things:

1. **Scaffolds a complete Obsidian vault** — dashboards, templates,
   knowledge directories, Dataview configuration, and session capture
   infrastructure
2. **Writes a default config** to `~/.config/vibe-vault/config.toml`
   with the vault path set to what you chose
3. **Prints next steps** including the hook configuration

To also initialize git in the vault (recommended if you want version
history of your session notes):

```bash
vv init ~/obsidian/my-sessions --git
```

### Open in Obsidian

Open the vault directory in Obsidian. Install the **Dataview** community
plugin to power the pre-built dashboards (sessions, decisions, action
items, weekly digest).

## 3. Connect Claude Code

Register vv as a Claude Code hook so every session is automatically captured:

```bash
vv hook install
```

This adds `SessionEnd`, `Stop`, and `PreCompact` hook entries to
`~/.claude/settings.json`, creating the file if it doesn't exist. A backup is
saved to `settings.json.vv.bak` before any modification. The command is
idempotent — running it again when hooks are already configured is a no-op.

- **SessionEnd** captures finalized notes when a session completes (including
  `/clear` events)
- **Stop** captures mid-session checkpoints (provisional notes that get
  overwritten when the session ends)
- **PreCompact** captures a checkpoint before context compaction, preserving
  full context before it gets summarized

To remove the hooks later: `vv hook uninstall`

### Enable the MCP server

Register the vibe-vault MCP server so Claude Code can query project context:

```bash
vv mcp install
```

This adds a `vibe-vault` entry to the `mcpServers` section of
`~/.claude/settings.json`. Restart Claude Code after running this command.
This detects all installed editors (Claude Code, Zed) and installs into each
one. Use `--claude-only` or `--zed-only` to target a single editor.

The MCP server exposes 19 tools that let the agent search sessions, read
project knowledge, manage AI context, and access friction trends on demand.

To remove the MCP server later: `vv mcp uninstall`

### Verify the setup

```bash
vv check
```

This validates your config, vault structure, Obsidian setup, domain paths,
hook integration, and MCP server registration. Every line should show `PASS`
or `WARN` — any `FAIL` means something needs fixing. Example output:

```
  pass  config           ~/.config/vibe-vault/config.toml
  pass  vault            ~/obsidian/my-sessions
  warn  obsidian         .obsidian/ not found (open vault in Obsidian first)
  pass  projects         Projects/ (0 notes)
  pass  state            .vibe-vault/ found
  warn  index            session-index.json not found
  pass  enrichment       disabled
  pass  hook             vv hook found in ~/.claude/settings.json
  pass  mcp              vibe-vault MCP server found in ~/.claude/settings.json
```

At this point, **every new Claude Code session will automatically create a
note** in `Projects/{project}/sessions/YYYY-MM-DD-NN.md`.

## 4. Backfill Historical Sessions

You already have transcripts from past Claude Code sessions sitting in
`~/.claude/projects/`. Backfill processes them all into your new vault:

```bash
vv backfill
```

This scans `~/.claude/projects/` by default. To scan a different location:

```bash
vv backfill ~/other/transcript/directory
```

Backfill will:
- Discover all JSONL transcripts (filtering out subagent transcripts)
- Skip sessions already in the index
- Process each transcript through the full capture pipeline
- Report a summary: processed, skipped, patched, errors

### Archive the raw transcripts

After backfill, compress the original transcripts into the vault for
safekeeping (~10:1 compression with zstd):

```bash
vv archive
```

Archived copies live in `.vibe-vault/archive/` inside the vault. The
originals in `~/.claude/projects/` are untouched — `vv archive` only
copies and compresses, it never deletes source files.

### Rebuild the index

After a large backfill, rebuild the cross-session index to generate
per-project context documents and refresh related-session links:

```bash
vv index
```

This walks all session notes, rebuilds `session-index.json`, and generates
a `history.md` file for each project directory with a timeline, key
decisions, open threads, and frequently-changed files.

### Explore your data

After backfilling, use the analytics commands to explore what you've captured:

```bash
vv stats                       # session counts, tokens, models, top projects
vv friction                    # correction patterns and high-friction sessions
vv trends                      # weekly metric trends with anomaly detection
vv inject                      # session-start context payload
```

All four accept `--project myproject` to filter to a single project.
`vv inject` also supports `--format json`, `--sections`, and `--max-tokens`.

## 5. Connect a Project

vibe-vault detects projects automatically — there's no per-project
registration step. When a session runs, `vv` determines the project name
from the working directory:

1. **`.vibe-vault.toml`** (highest priority) — if present in the repo root,
   the `project.name` value is used. Created as a commented-out template by
   `vv context init`. Uncomment and set values to override automatic detection.
2. **Git remote** (preferred) — extracts the repo name from
   `git remote get-url origin`. This is stable across worktrees, directory
   renames, and machines.
3. **Directory basename** (fallback) — uses the name of the working
   directory when git isn't available.

### New projects

Just start a Claude Code session in any git repository. When the session
ends, `vv` will:

- Detect the project name from the git remote (or directory name)
- Create `Projects/{project}/sessions/` in the vault
- Write the session note as `YYYY-MM-DD-01.md`

No configuration needed. The project directory is created on first use.

### Existing projects with history

If you have past sessions for a project that weren't captured (because
`vv` wasn't installed yet), backfill picks them up automatically —
transcripts are matched to projects by the CWD recorded in each transcript.

```bash
# Backfill discovers all projects at once
vv backfill
```

### Domain mapping

To tag sessions with a domain (work, personal, opensource), configure
workspace prefixes in `~/.config/vibe-vault/config.toml`:

```toml
[domains]
work = "~/work"
personal = "~/personal"
opensource = "~/opensource"
```

Any session whose CWD starts with `~/work/` gets `domain: work` in its
frontmatter. This powers the domain-filtered dashboards in Obsidian.

### Session tags

Session notes are tagged with `vv-session` by default. Customize in
`~/.config/vibe-vault/config.toml`:

```toml
[tags]
session = "vv-session"    # base tag for all session notes
# extra = ["my-team"]     # additional tags applied to every session
```

Tags can also be overridden per-project via `agentctx/config.toml`
(see section 6).

## 6. Optional: Vault-Resident AI Context

AI workflow files (resume, iteration history, tasks, slash commands) can live
in the vault instead of as untracked repo-local files. Everything goes into a
single `agentctx/` directory per project — portable, searchable in Obsidian,
and synced across machines.

### New project (no existing context files)

From your project's repo root:

```bash
vv context init
```

This creates:
- **Vault-side** (`Projects/{project}/agentctx/`):
  - `CLAUDE.md` — MCP-first instructions (deployed as regular file to repo)
  - `workflow.md` — behavioral rules (pair programming, plan mode, verification)
  - `resume.md` — project state scaffold
  - `iterations.md` — iteration history
  - `config.toml` — per-project config overlay (all settings commented out)
  - `commands/restart.md`, `commands/wrap.md` — slash commands
  - `rules/`, `skills/`, `agents/` — Claude Code extensions
  - `tasks/`, `tasks/done/` — task tracking
- **Repo-side** (regular files, no symlinks):
  - `CLAUDE.md` — MCP-first instructions (calls `vv_bootstrap_context`)
  - `.claude/commands/` — slash commands deployed from vault
  - `.claude/rules/`, `.claude/skills/`, `.claude/agents/` — Claude Code extensions
  - `commit.msg` — commit message scratch file
- Updates `.gitignore` to exclude `CLAUDE.md`, `commit.msg`, and `.claude/`

The project name is auto-detected from the git remote. To override:

```bash
vv context init --project myproject
```

### Existing project (migrate local files)

If you already have `RESUME.md`, `HISTORY.md`, or `tasks/` in your repo:

```bash
vv context migrate
```

This copies:
- `RESUME.md` → `Projects/{project}/agentctx/resume.md`
- `HISTORY.md` → `Projects/{project}/agentctx/iterations.md`
- `tasks/` → `Projects/{project}/agentctx/tasks/` (recursive)
- `.claude/commands/*.md` → `Projects/{project}/agentctx/commands/` (regular files only)

Then deploys MCP-first `CLAUDE.md` and commands as regular files to the repo.
Local originals are preserved — remove them manually after verifying.

### Sync schema and shared commands

After upgrading `vv`, existing projects may have an older agentctx schema.
Sync brings them up to date, propagates any new shared commands from
`Templates/agentctx/commands/`, and deploys them to the repo:

```bash
vv context sync                    # sync current project
vv context sync --all              # sync all projects (vault-only)
vv context sync --dry-run          # preview changes
vv context sync --force            # overwrite conflicts
```

### How it works after setup

- Vault-side `agentctx/` is the canonical source for all context files
- `CLAUDE.md` instructs the AI to call `vv_bootstrap_context` at session start (MCP tool)
- `.claude/commands/` contains slash commands deployed from the vault on each `vv context sync`
- `/restart` loads context via MCP tools (`vv_bootstrap_context`, `vv_list_tasks`)
- `/wrap` updates vault-side files via MCP tools (`vv_update_resume`, `vv_append_iteration`)
- If MCP tools are unavailable, `vv inject` via Bash provides the same context
- Everything is searchable in Obsidian alongside session notes
- Clone the repo, run `vv context init`, and resume with full context on any machine

## 7. Optional: Enable LLM Enrichment

By default, notes contain extracted metadata, heuristic narrative summaries,
dialogue excerpts, git commits, and friction signals — all derived from the
transcript without any API calls. To add LLM-refined summaries, key decisions,
and open threads:

1. Edit `~/.config/vibe-vault/config.toml`:

```toml
[enrichment]
enabled = true
model = "grok-3-mini-fast"
api_key_env = "XAI_API_KEY"
base_url = "https://api.x.ai/v1"
```

2. Set the API key:

```bash
export XAI_API_KEY="your-key-here"  # add to ~/.bashrc or ~/.zshrc
```

Any OpenAI-compatible endpoint works — swap the model, key env var, and
base URL for OpenAI, Ollama, or any other provider. See the
[README](README.md#llm-enrichment) for examples.

Enrichment never blocks note creation. If the API is unreachable or the
key is missing, the note is written without enrichment sections.

### Re-enrich existing notes

To regenerate notes with enrichment after enabling it:

```bash
vv reprocess                       # all sessions
vv reprocess --project myproject   # one project only
```

## Quick Reference

| Task | Command |
|------|---------|
| Build and install | `make install` |
| Create vault | `vv init ~/obsidian/my-sessions` |
| Create vault with git | `vv init ~/obsidian/my-sessions --git` |
| Install hooks | `vv hook install` |
| Remove hooks | `vv hook uninstall` |
| Enable MCP server | `vv mcp install` |
| Remove MCP server | `vv mcp uninstall` |
| Verify setup | `vv check` |
| Backfill history | `vv backfill` |
| Archive transcripts | `vv archive` |
| Rebuild index | `vv index` |
| Session analytics | `vv stats` |
| Friction analysis | `vv friction` |
| Metric trends | `vv trends` |
| Inject context | `vv inject` |
| Reprocess notes | `vv reprocess` |
| Init project context | `vv context init` |
| Migrate local context | `vv context migrate` |
| Sync schema/commands | `vv context sync` |
| Override conflicts | `vv context sync --force` |
| Process one file | `vv process path/to/transcript.jsonl` |
| Per-command help | `vv <command> --help` |
| Man pages | `man vv` or `man vv-<command>` |
