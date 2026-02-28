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

This adds `SessionEnd` and `Stop` hook entries to `~/.claude/settings.json`,
creating the file if it doesn't exist. A backup is saved to
`settings.json.vv.bak` before any modification. The command is idempotent —
running it again when hooks are already configured is a no-op.

- **SessionEnd** captures finalized notes when a session completes
- **Stop** captures mid-session checkpoints (provisional notes that get
  overwritten when the session ends)

To remove the hooks later: `vv hook uninstall`

### Manual alternative

If you prefer to edit `~/.claude/settings.json` yourself, add (or merge
into) the `hooks` object:

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

### Verify the setup

```bash
vv check
```

This validates your config, vault structure, Obsidian setup, domain paths,
and hook integration. Every line should show `PASS` or `WARN` — any `FAIL`
means something needs fixing. Example output:

```
  PASS  config           ~/.config/vibe-vault/config.toml
  PASS  vault            ~/obsidian/my-sessions
  WARN  obsidian         .obsidian/ not found (open vault in Obsidian first)
  PASS  projects         Projects/ (0 notes)
  PASS  state            .vibe-vault/ found
  WARN  index            session-index.json not found
  PASS  enrichment       disabled
  PASS  hook             vv hook found in ~/.claude/settings.json
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

## 5. Connect a Project

vibe-vault detects projects automatically — there's no per-project
registration step. When a session runs, `vv` determines the project name
from the working directory:

1. **Git remote** (preferred) — extracts the repo name from
   `git remote get-url origin`. This is stable across worktrees, directory
   renames, and machines.
2. **Directory basename** (fallback) — uses the name of the working
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

## 6. Optional: Vault-Resident AI Context

AI workflow files (resume, iteration history, tasks) can live in the vault
instead of as untracked repo-local files. This makes them portable, searchable
in Obsidian, and accessible to any session.

### New project (no existing context files)

From your project's repo root:

```bash
vv context init
```

This creates:
- **Vault-side**: `Projects/{project}/resume.md`, `iterations.md`, `tasks/`
- **Repo-side**: `CLAUDE.md` (vault pointer), `.claude/commands/restart.md`,
  `.claude/commands/wrap.md`
- Updates `.gitignore` to exclude `CLAUDE.md` and `commit.msg`

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
- `RESUME.md` → `Projects/{project}/resume.md`
- `HISTORY.md` → `Projects/{project}/iterations.md`
- `tasks/` → `Projects/{project}/tasks/` (recursive)

Then rewrites the repo-side files to point at the vault. Local originals
are preserved — remove them manually after verifying the migration.

### How it works after setup

- `/restart` reads `resume.md` from the vault (not `./RESUME.md`)
- `/wrap` updates vault-side files (resume, tasks)
- `CLAUDE.md` in the repo is a lightweight pointer with workflow rules
- Everything is searchable in Obsidian alongside session notes

## 7. Optional: Enable LLM Enrichment

By default, notes contain extracted metadata and files changed but no
narrative summary. To add LLM-generated summaries, decisions, and open
threads:

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
| Verify setup | `vv check` |
| Backfill history | `vv backfill` |
| Archive transcripts | `vv archive` |
| Rebuild index | `vv index` |
| Reprocess notes | `vv reprocess` |
| Init project context | `vv context init` |
| Migrate local context | `vv context migrate` |
| Process one file | `vv process path/to/transcript.jsonl` |
| Per-command help | `vv <command> --help` |
| Man pages | `man vv` or `man vv-<command>` |
