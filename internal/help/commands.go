// Copyright 2026 John Suykerbuyk <john@syketech.com>
// SPDX-License-Identifier: Apache-2.0 OR MIT

package help

import "strings"

// Version is the vv release version, set at build time via -ldflags.
// Defaults to "dev" when built without version injection (e.g. `go run`).
var Version = "dev"

// Flag describes a command-line flag.
type Flag struct {
	Name string // e.g. "--git" or "--event <name>"
	Desc string
}

// Arg describes a positional argument.
type Arg struct {
	Name     string // e.g. "path" or "transcript.jsonl"
	Desc     string
	Optional bool
}

// Command describes a vv subcommand (or the top-level binary when Name is "").
type Command struct {
	Name        string   // "init", "hook", etc; "" for top-level
	Synopsis    string   // one-line description (lowercase, for --help header)
	Brief       string   // short description for usage table (capitalized)
	Usage       string   // full usage line, e.g. "vv init [path] [--git]"
	TableUsage  string   // shortened usage for the top-level table (if different from Usage)
	Args        []Arg
	Flags       []Flag
	Description string   // multi-line prose (stored verbatim)
	Examples    []string // one per line, without leading 2-space indent
	SeeAlso     []string // man page cross-refs, e.g. "vv(1)"
}

// tableUsage returns TableUsage if set, otherwise Usage.
func (c Command) tableUsage() string {
	if c.TableUsage != "" {
		return c.TableUsage
	}
	return c.Usage
}

// ManName returns the man page name: "vv" for top-level, "vv-<name>" for subs.
// Spaces in Name are replaced with hyphens (e.g. "hook install" → "vv-hook-install").
func (c Command) ManName() string {
	if c.Name == "" {
		return "vv"
	}
	return "vv-" + strings.ReplaceAll(c.Name, " ", "-")
}

// TopLevel is the top-level vv command (used by FormatUsage).
var TopLevel = Command{
	Name:     "",
	Synopsis: "vibe-vault session capture",
}

var CmdInit = Command{
	Name:     "init",
	Synopsis: "create a new Obsidian vault for session notes",
	Brief:    "Create a new vault (default: ./vibe-vault)",
	Usage:    "vv init [path] [--git]",
	Args: []Arg{
		{Name: "path", Desc: "Target directory (default: ./vibe-vault)", Optional: true},
	},
	Flags: []Flag{
		{Name: "--git", Desc: "Initialize a git repository in the new vault"},
	},
	Description: `Creates a fully configured Obsidian vault with Dataview dashboards,
Templater templates, and session capture infrastructure. Also writes
a default config to ~/.config/vibe-vault/config.toml pointing at the
new vault.`,
	Examples: []string{
		"vv init                       Create ./vibe-vault",
		"vv init ~/obsidian/my-vault   Create at a specific path",
		"vv init --git                 Create with git repo initialized",
	},
	SeeAlso: []string{"vv(1)", "vv-hook(1)", "vv-check(1)"},
}

var CmdHook = Command{
	Name:     "hook",
	Synopsis: "Claude Code hook handler",
	Brief:    "Hook mode (reads stdin from Claude Code)",
	Usage:      "vv hook [install | uninstall | --event <name>]",
	TableUsage: "vv hook [install | ...]",
	Flags: []Flag{
		{Name: "--event <name>", Desc: "Override the hook event type (default: read from stdin)"},
	},
	Description: `Reads a JSON payload from stdin as delivered by Claude Code's hook
system. Handles three event types:

  SessionEnd — parses transcript, writes a finalized session note
  Stop       — captures a mid-session checkpoint (no LLM enrichment)
  PreCompact — captures a checkpoint before context compaction

Checkpoint notes are provisional: a subsequent Stop or PreCompact
overwrites the previous checkpoint, and SessionEnd finalizes it.

This command is meant to be called by Claude Code, not directly.

Subcommands:
  vv hook install     Add vv hooks to ~/.claude/settings.json
  vv hook uninstall   Remove vv hooks from ~/.claude/settings.json`,
	SeeAlso: []string{"vv(1)", "vv-process(1)", "vv-hook-install(1)", "vv-hook-uninstall(1)"},
}

var CmdProcess = Command{
	Name:       "process",
	Synopsis:   "process a single transcript file",
	Brief:      "Process a single transcript file",
	Usage:      "vv process <transcript.jsonl>",
	TableUsage: "vv process <file.jsonl>",
	Args: []Arg{
		{Name: "transcript.jsonl", Desc: "Path to a Claude Code JSONL transcript"},
	},
	Description: `Parses the transcript, detects the project from the session's working
directory, and writes a session note to the vault. Skips trivial
sessions (< 2 messages) and already-indexed sessions.`,
	Examples: []string{
		"vv process ~/.claude/projects/-home-user-myproject/abc123.jsonl",
	},
	SeeAlso: []string{"vv(1)", "vv-hook(1)", "vv-backfill(1)"},
}

var CmdIndex = Command{
	Name:     "index",
	Synopsis: "rebuild session index from notes",
	Brief:    "Rebuild session index from notes",
	Usage:    "vv index",
	Description: `Walks Projects/*/sessions/*.md in the vault, parses frontmatter from each
note, and rebuilds .vibe-vault/session-index.json. Preserves TranscriptPath
values from the existing index. Generates a history.md document for
each project with timeline, decisions, open threads, and key files.

Use this after manually editing or deleting session notes.`,
	SeeAlso: []string{"vv(1)", "vv-reprocess(1)"},
}

var CmdBackfill = Command{
	Name:     "backfill",
	Synopsis: "discover and process historical transcripts",
	Brief:    "Discover and process historical transcripts",
	Usage:    "vv backfill [path]",
	Args: []Arg{
		{Name: "path", Desc: "Directory to scan for transcripts (default: ~/.claude/projects/)", Optional: true},
	},
	Description: `Recursively discovers Claude Code JSONL transcripts by UUID filename
pattern, skips already-indexed sessions, and processes the rest through
the full capture pipeline. Also patches TranscriptPath on existing index
entries that lack it.

Subagent transcripts (in /subagents/ subdirectories) are automatically
filtered out.`,
	Examples: []string{
		"vv backfill                              Scan default Claude projects dir",
		"vv backfill ~/.claude/projects/myproj    Scan a specific directory",
	},
	SeeAlso: []string{"vv(1)", "vv-process(1)", "vv-archive(1)"},
}

var CmdArchive = Command{
	Name:     "archive",
	Synopsis: "compress transcripts into vault archive",
	Brief:    "Compress transcripts into vault archive",
	Usage:    "vv archive",
	Description: `Iterates all sessions in the index and compresses each transcript to
.vibe-vault/archive/{session-id}.jsonl.zst using zstd compression
(typically ~10:1 on JSONL). Skips already-archived and missing
transcripts. Originals are not deleted.

Reports total bytes before and after compression.`,
	SeeAlso: []string{"vv(1)", "vv-backfill(1)", "vv-reprocess(1)"},
}

var CmdReprocess = Command{
	Name:       "reprocess",
	Synopsis:   "re-generate notes from transcripts",
	Brief:      "Re-generate notes from transcripts",
	Usage:      "vv reprocess [--project <name>]",
	TableUsage: "vv reprocess [--project X]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Only reprocess sessions for this project"},
	},
	Description: `Re-runs the capture pipeline with Force mode for all (or filtered)
sessions in the index. Locates transcripts via three-tier lookup:

  1. Original path (TranscriptPath in index)
  2. Archived copy (.vibe-vault/archive/)
  3. Fallback discovery scan (~/.claude/projects/)

Overwrites existing notes in place (preserves iteration numbers).
Regenerates history.md for each affected project.`,
	Examples: []string{
		"vv reprocess                       Reprocess all sessions",
		"vv reprocess --project myproject   Reprocess one project only",
	},
	SeeAlso: []string{"vv(1)", "vv-archive(1)", "vv-index(1)"},
}

var CmdCheck = Command{
	Name:     "check",
	Synopsis: "validate config, vault, and hook setup",
	Brief:    "Validate config, vault, and hook setup",
	Usage:    "vv check",
	Description: `Runs diagnostic checks and prints a pass/warn/FAIL report:
  - Config file location and validity
  - Vault directory exists
  - Obsidian config present (.obsidian/)
  - Projects directory and note count
  - State directory (.vibe-vault/)
  - Session index validity and entry count
  - Domain paths exist
  - Enrichment config and API key
  - Claude Code hook setup in ~/.claude/settings.json

Exit code 0 if all checks pass or warn, 1 if any check fails.`,
	SeeAlso: []string{"vv(1)", "vv-init(1)"},
}

var CmdStats = Command{
	Name:       "stats",
	Synopsis:   "show session analytics and metrics",
	Brief:      "Show session analytics and metrics",
	Usage:      "vv stats [--project <name>]",
	TableUsage: "vv stats [--project X]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Show stats for a specific project only"},
	},
	Description: `Computes aggregate metrics from the session index and displays them
in aligned terminal output. Shows overview totals, per-project and
per-model breakdowns, activity tag distribution, monthly trends,
and top files.

All data is read from the session index — no note re-parsing needed.
Run vv index first if token data appears incomplete (backfills token
counts from note frontmatter).`,
	Examples: []string{
		"vv stats                       Show global stats",
		"vv stats --project myproject   Show stats for one project",
	},
	SeeAlso: []string{"vv(1)", "vv-index(1)"},
}

var CmdFriction = Command{
	Name:       "friction",
	Synopsis:   "show friction analysis and correction patterns",
	Brief:      "Show friction analysis and correction patterns",
	Usage:      "vv friction [--project <name>]",
	TableUsage: "vv friction [--project X]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Show friction for a specific project only"},
	},
	Description: `Analyzes friction signals from the session index: correction density,
token efficiency, file retry patterns, error cycles, and recurring
open threads. Shows per-project aggregates and identifies high-friction
sessions.

Friction scores range from 0 (smooth) to 100 (high friction). Sessions
scoring ≥ 40 are flagged as high-friction. Run vv reprocess to generate
friction data if none is available.`,
	Examples: []string{
		"vv friction                       Show global friction analysis",
		"vv friction --project myproject   Show friction for one project",
	},
	SeeAlso: []string{"vv(1)", "vv-stats(1)", "vv-reprocess(1)"},
}

var CmdTrends = Command{
	Name:       "trends",
	Synopsis:   "show metric trends over time",
	Brief:      "Show metric trends over time",
	Usage:      "vv trends [--project <name>] [--weeks <n>]",
	TableUsage: "vv trends [--project X]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Show trends for a specific project only"},
		{Name: "--weeks <n>", Desc: "Number of weeks to display (default: 12)"},
	},
	Description: `Analyzes metric trends from the session index over time using weekly
buckets with 4-week rolling averages. Shows direction (improving,
worsening, stable) by comparing the most recent 4 weeks against the
previous 4 weeks.

Tracks four metrics:
  - Friction score (average per week)
  - Tokens per file (total tokens / files changed)
  - Corrections per session
  - Session duration

Weeks where a metric deviates more than 1.5 standard deviations from
its rolling average are flagged as anomalies (spikes or dips).`,
	Examples: []string{
		"vv trends                       Show global trends (last 12 weeks)",
		"vv trends --weeks 24            Show last 24 weeks",
		"vv trends --project myproject   Show trends for one project",
	},
	SeeAlso: []string{"vv(1)", "vv-stats(1)", "vv-friction(1)"},
}

var CmdInject = Command{
	Name:       "inject",
	Synopsis:   "output session-start context payload",
	Brief:      "Output session-start context payload",
	Usage:      "vv inject [--project <name>] [--format <md|json>] [--sections <list>] [--max-tokens <n>]",
	TableUsage: "vv inject [--project X]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Project to inject context for (default: auto-detect)"},
		{Name: "--format <md|json>", Desc: "Output format (default: md)"},
		{Name: "--sections <list>", Desc: "Comma-separated sections to include (default: all)"},
		{Name: "--max-tokens <n>", Desc: "Token budget for output (default: 2000)"},
	},
	Description: `Outputs a condensed, token-budgeted context payload for a project.
Assembles recent sessions, open threads, decisions, friction trends,
and knowledge notes into a single document suitable for injection at
session start.

Available sections (in priority order):
  summary      Most recent session summary
  sessions     Last 5 sessions, newest first
  threads      Open threads from last 5 sessions (resolved filtered out)
  decisions    Decisions from last 30 days (deduped)
  friction     Friction trend direction and rolling average
  knowledge    Relevant knowledge notes (project + agnostic)

When output exceeds --max-tokens, lowest-priority sections are dropped
until the budget is met.`,
	Examples: []string{
		"vv inject                                   Inject context for auto-detected project",
		"vv inject --project myproject                Inject context for a specific project",
		"vv inject --format json                      Output as JSON",
		"vv inject --sections summary,sessions        Only summary and sessions",
		"vv inject --max-tokens 500                   Compact output",
	},
	SeeAlso: []string{"vv(1)", "vv-stats(1)", "vv-trends(1)"},
}

var CmdExport = Command{
	Name:       "export",
	Synopsis:   "export session data for external analysis",
	Brief:      "Export session data (JSON or CSV)",
	Usage:      "vv export [--format <json|csv>] [--project <name>]",
	TableUsage: "vv export [--format X]",
	Flags: []Flag{
		{Name: "--format <json|csv>", Desc: "Output format (default: json)"},
		{Name: "--project <name>", Desc: "Export only sessions for this project"},
	},
	Description: `Serializes session index entries for external analysis tools. Outputs
all sessions (or a project subset) in JSON or CSV format.

JSON outputs a flat array of session objects with key fields. CSV
outputs a header row followed by one row per session, with columns:
date, project, session_id, title, tag, model, branch, duration_minutes,
tokens_in, tokens_out, messages, tool_uses, friction_score, corrections,
estimated_cost_usd.

Output is sorted by date, then session ID.`,
	Examples: []string{
		"vv export                              Export all sessions as JSON",
		"vv export --format csv                 Export as CSV",
		"vv export --project myproject          Export one project as JSON",
		"vv export --format csv > sessions.csv  Export to file",
	},
	SeeAlso: []string{"vv(1)", "vv-stats(1)"},
}

var CmdMcp = Command{
	Name:     "mcp",
	Synopsis: "start MCP server for AI agent integration",
	Brief:    "Start MCP server (JSON-RPC over stdio)",
	Usage:    "vv mcp",
	Description: `Starts a Model Context Protocol (MCP) server that exposes vibe-vault
tools over JSON-RPC 2.0 on stdin/stdout. This allows AI agents like
Claude Code to query project context on demand.

Available tools:
  get_project_context   Condensed project context (sessions, threads,
                        decisions, friction trends)
  list_projects         All projects with session counts and date ranges

Configure in Claude Code's settings:
  {
    "mcpServers": {
      "vibe-vault": {
        "command": "vv",
        "args": ["mcp"]
      }
    }
  }

The server logs tool calls to stderr for observability.`,
	SeeAlso: []string{"vv(1)", "vv-inject(1)"},
}

var CmdVersion = Command{
	Name:     "version",
	Synopsis: "print version",
	Brief:    "Print version",
	Usage:    "vv version",
	SeeAlso:  []string{"vv(1)"},
}

var CmdHookInstall = Command{
	Name:     "hook install",
	Synopsis: "add vv hooks to Claude Code settings",
	Brief:    "Add vv hooks to settings.json",
	Usage:    "vv hook install",
	Description: `Adds SessionEnd and Stop hook entries to ~/.claude/settings.json so
that Claude Code automatically calls vv after each session.

Creates the settings file and parent directory if they don't exist.
Preserves all existing settings and hooks. A backup is saved to
settings.json.vv.bak before any modification.

This command is idempotent: running it when hooks are already
configured prints an informational message and exits successfully.`,
	SeeAlso: []string{"vv(1)", "vv-hook(1)", "vv-hook-uninstall(1)", "vv-check(1)"},
}

var CmdHookUninstall = Command{
	Name:     "hook uninstall",
	Synopsis: "remove vv hooks from Claude Code settings",
	Brief:    "Remove vv hooks from settings.json",
	Usage:    "vv hook uninstall",
	Description: `Removes SessionEnd and Stop hook entries containing "vv hook" from
~/.claude/settings.json. Preserves all other settings and hooks.
A backup is saved to settings.json.vv.bak before any modification.

Cleans up empty hook arrays and the hooks map when no hooks remain.

This command is idempotent: running it when hooks are not present
prints an informational message and exits successfully.`,
	SeeAlso: []string{"vv(1)", "vv-hook(1)", "vv-hook-install(1)"},
}

var CmdContext = Command{
	Name:     "context",
	Synopsis: "manage vault-resident AI context files",
	Brief:    "Manage vault-resident AI context",
	Usage:      "vv context [init | migrate | sync]",
	TableUsage: "vv context [init | ...]",
	Description: `Manages AI workflow context files (resume, iterations, tasks) that live
in the Obsidian vault rather than as untracked repo-local files. This
makes context portable, searchable, and visible to Obsidian.

Subcommands:
  vv context init      Scaffold vault-resident context for current project
  vv context migrate   Copy existing local files to vault
  vv context sync      Migrate schema and propagate shared commands`,
	SeeAlso: []string{"vv(1)", "vv-context-init(1)", "vv-context-migrate(1)", "vv-context-sync(1)"},
}

var CmdContextInit = Command{
	Name:     "context init",
	Synopsis: "scaffold vault-resident context for a project",
	Brief:    "Scaffold vault-resident context",
	Usage:    "vv context init [--project <name>] [--force]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Override auto-detected project name"},
		{Name: "--force", Desc: "Overwrite existing files"},
	},
	Description: `Creates vault-resident context files for the current project:

Vault-side (in Projects/{project}/):
  resume.md        AI working context skeleton
  iterations.md    Iteration narratives skeleton
  tasks/           Task directory with done/ subdirectory

Repo-side (in current directory):
  CLAUDE.md              Vault pointer (workflow rules in vault-side workflow.md)
  .claude/commands/restart.md  Session resume command
  .claude/commands/wrap.md     Session wrap command

Also ensures .gitignore has entries for CLAUDE.md and commit.msg.

Existing files are skipped unless --force is specified. Project name
is auto-detected from git remote or directory name.`,
	Examples: []string{
		"vv context init                       Scaffold for auto-detected project",
		"vv context init --project myproject   Scaffold for a specific project",
		"vv context init --force               Overwrite existing files",
	},
	SeeAlso: []string{"vv(1)", "vv-context(1)", "vv-context-migrate(1)"},
}

var CmdContextMigrate = Command{
	Name:     "context migrate",
	Synopsis: "copy existing local context files to vault",
	Brief:    "Copy local context files to vault",
	Usage:    "vv context migrate [--project <name>] [--force]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Override auto-detected project name"},
		{Name: "--force", Desc: "Overwrite existing vault files"},
	},
	Description: `Copies existing repo-local context files to the vault:

  RESUME.md  → Projects/{project}/resume.md
  HISTORY.md → Projects/{project}/iterations.md
  tasks/     → Projects/{project}/tasks/ (recursive)

Then force-updates repo-side files (CLAUDE.md, .claude/commands/) to
point at the vault. Local originals are NOT deleted — remove them
manually after verifying the migration.

Files that don't exist locally are skipped. Vault files that already
exist are skipped unless --force is specified.`,
	Examples: []string{
		"vv context migrate                       Migrate auto-detected project",
		"vv context migrate --project myproject   Migrate a specific project",
		"vv context migrate --force               Overwrite existing vault files",
	},
	SeeAlso: []string{"vv(1)", "vv-context(1)", "vv-context-init(1)"},
}

var CmdContextSync = Command{
	Name:     "context sync",
	Synopsis: "migrate schema and propagate shared commands",
	Brief:    "Migrate schema and propagate shared commands",
	Usage:    "vv context sync [--project <name>] [--all] [--dry-run] [--force]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Override auto-detected project name"},
		{Name: "--all", Desc: "Sync all projects with agentctx (vault-only operations)"},
		{Name: "--dry-run", Desc: "Report changes without modifying any files"},
		{Name: "--force", Desc: "Force overwrite existing files during migration"},
	},
	Description: `Runs schema migrations and propagates shared commands for one or all
projects.

Schema migrations upgrade the agentctx directory structure to the latest
version. For example, migrating from v0 to v2 adds a .version file,
creates an agentctx symlink at the repo root, rewrites CLAUDE.md to
use relative paths, and replaces .claude/commands with a relative
symlink.

Shared command propagation copies new .md files from
Templates/agentctx/commands/ to each project's agentctx/commands/.
Existing project commands are never overwritten.

In --all mode, only vault-side operations are performed (no repo-side
symlinks). Run from each repo root without --all for repo-side updates.`,
	Examples: []string{
		"vv context sync                       Sync current project",
		"vv context sync --dry-run             Preview changes",
		"vv context sync --all                 Sync all projects (vault-only)",
		"vv context sync --project myproject   Sync a specific project",
	},
	SeeAlso: []string{"vv(1)", "vv-context(1)", "vv-context-init(1)"},
}

var CmdTemplates = Command{
	Name:     "templates",
	Synopsis: "inspect, compare, and reset vault templates",
	Brief:    "Inspect, compare, and reset vault templates",
	Usage:      "vv templates [list | diff | show | reset]",
	TableUsage: "vv templates [list | ...]",
	Description: `Manages vault templates against their built-in defaults. Over time,
built-in templates evolve with new features and better prompts, but
vault copies drift. This command makes recalibration easy.

Subcommands:
  vv templates list              Show all templates with status
  vv templates diff [--file X]   Unified diff of vault vs defaults
  vv templates show <name>       Print built-in default to stdout
  vv templates reset [--file X]  Reset templates to defaults`,
	SeeAlso: []string{"vv(1)", "vv-context-init(1)"},
}

var CmdTemplatesList = Command{
	Name:     "templates list",
	Synopsis: "list all templates with status",
	Brief:    "List all templates with status",
	Usage:    "vv templates list",
	Description: `Shows all 12 built-in templates with their current status:

  default     Vault copy matches the built-in default
  customized  Vault copy has been modified
  missing     Template not found in vault`,
	SeeAlso: []string{"vv(1)", "vv-templates(1)"},
}

var CmdTemplatesDiff = Command{
	Name:     "templates diff",
	Synopsis: "show differences between vault and default templates",
	Brief:    "Diff vault templates against defaults",
	Usage:    "vv templates diff [--file <name>]",
	Flags: []Flag{
		{Name: "--file <name>", Desc: "Diff only this template (relative path, e.g. agentctx/workflow.md)"},
	},
	Description: `Shows unified diffs between vault template copies and their built-in
defaults. Without --file, shows diffs for all customized templates.`,
	Examples: []string{
		"vv templates diff                            Diff all customized templates",
		"vv templates diff --file agentctx/resume.md  Diff a specific template",
	},
	SeeAlso: []string{"vv(1)", "vv-templates(1)", "vv-templates-reset(1)"},
}

var CmdTemplatesShow = Command{
	Name:     "templates show",
	Synopsis: "print a built-in default template to stdout",
	Brief:    "Print built-in default to stdout",
	Usage:    "vv templates show <name>",
	Args: []Arg{
		{Name: "name", Desc: "Template relative path (e.g. agentctx/workflow.md, session-summary.md)"},
	},
	Description: `Prints the built-in default content of a template to stdout. Useful
for inspecting defaults without modifying vault files.`,
	Examples: []string{
		"vv templates show agentctx/workflow.md",
		"vv templates show session-summary.md",
	},
	SeeAlso: []string{"vv(1)", "vv-templates(1)"},
}

var CmdTemplatesReset = Command{
	Name:     "templates reset",
	Synopsis: "reset vault templates to built-in defaults",
	Brief:    "Reset vault templates to defaults",
	Usage:    "vv templates reset [--file <name> | --all] [--force]",
	Flags: []Flag{
		{Name: "--file <name>", Desc: "Reset only this template"},
		{Name: "--all", Desc: "Reset all templates"},
		{Name: "--force", Desc: "Actually perform the reset (dry-run without this flag)"},
	},
	Description: `Overwrites vault template copies with their built-in defaults.

Without --force, performs a dry-run showing what would be reset.
Requires either --file or --all to specify scope.`,
	Examples: []string{
		"vv templates reset --all                              Dry-run: show what would reset",
		"vv templates reset --all --force                      Reset all templates",
		"vv templates reset --file agentctx/resume.md --force  Reset one template",
	},
	SeeAlso: []string{"vv(1)", "vv-templates(1)", "vv-templates-diff(1)"},
}

var CmdZed = Command{
	Name:     "zed",
	Synopsis: "import Zed agent panel threads",
	Brief:    "Import Zed agent panel threads into vault",
	Usage:    "vv zed <subcommand>",
	Description: `Import Zed agent panel threads from the local threads.db SQLite
database into the vault. Threads are parsed, converted, and processed
through the same capture pipeline as Claude Code sessions.

Subcommands:
  vv zed backfill   Import threads from Zed threads database
  vv zed list       List threads in the database`,
	SeeAlso: []string{"vv(1)", "vv-backfill(1)"},
}

var CmdZedBackfill = Command{
	Name:     "zed backfill",
	Synopsis: "import threads from Zed threads database",
	Brief:    "Import threads from Zed threads database",
	Usage:    "vv zed backfill [--db PATH] [--project NAME] [--since DATE] [--force]",
	Flags: []Flag{
		{Name: "--db <path>", Desc: "Path to threads.db (default: ~/.local/share/zed/threads/threads.db)"},
		{Name: "--project <name>", Desc: "Only import threads for this project"},
		{Name: "--since <date>", Desc: "Only import threads updated after this date (YYYY-MM-DD)"},
		{Name: "--force", Desc: "Re-import already-processed threads"},
	},
	Description: `Reads Zed agent panel threads from the SQLite database, converts them
to the vibe-vault transcript format, and processes each through the
capture pipeline. Session notes are written to the vault with
source: zed in the frontmatter.

Thread-to-session mapping:
  - Session ID: "zed:<thread-uuid>"
  - Project: detected from worktree path in thread snapshot
  - Model: from thread model metadata
  - Narrative/dialogue: extracted from Zed message format

Batch optimized: loads index once, processes all threads, saves once.`,
	Examples: []string{
		"vv zed backfill                                    Import from default DB",
		"vv zed backfill --db ~/custom/threads.db           Use custom DB path",
		"vv zed backfill --project myproj --since 2026-03-01  Filter by project and date",
		"vv zed backfill --force                            Re-import all threads",
	},
	SeeAlso: []string{"vv(1)", "vv-zed(1)", "vv-backfill(1)"},
}

var CmdZedList = Command{
	Name:     "zed list",
	Synopsis: "list threads in the Zed database",
	Brief:    "List threads in the Zed database",
	Usage:    "vv zed list [--db PATH] [--since DATE] [--limit N]",
	Flags: []Flag{
		{Name: "--db <path>", Desc: "Path to threads.db (default: ~/.local/share/zed/threads/threads.db)"},
		{Name: "--since <date>", Desc: "Only show threads updated after this date (YYYY-MM-DD)"},
		{Name: "--limit <n>", Desc: "Maximum number of threads to show"},
	},
	Description: `Lists threads from the Zed agent panel database in tabular format.
Shows thread ID, last updated time, message count, and title/summary.`,
	Examples: []string{
		"vv zed list                    List all threads",
		"vv zed list --limit 10         Show 10 most recent",
		"vv zed list --since 2026-03-01 Show threads from March onward",
	},
	SeeAlso: []string{"vv(1)", "vv-zed(1)"},
}

// ZedSubcommands is the ordered list of zed sub-subcommands.
var ZedSubcommands = []Command{
	CmdZedBackfill,
	CmdZedList,
}

// TemplatesSubcommands is the ordered list of templates sub-subcommands.
var TemplatesSubcommands = []Command{
	CmdTemplatesList,
	CmdTemplatesDiff,
	CmdTemplatesShow,
	CmdTemplatesReset,
}

// HookSubcommands is the ordered list of hook sub-subcommands.
var HookSubcommands = []Command{
	CmdHookInstall,
	CmdHookUninstall,
}

// ContextSubcommands is the ordered list of context sub-subcommands.
var ContextSubcommands = []Command{
	CmdContextInit,
	CmdContextMigrate,
	CmdContextSync,
}

// Subcommands is the ordered list of all subcommands.
var Subcommands = []Command{
	CmdInit,
	CmdHook,
	CmdContext,
	CmdProcess,
	CmdIndex,
	CmdBackfill,
	CmdArchive,
	CmdReprocess,
	CmdCheck,
	CmdStats,
	CmdFriction,
	CmdTrends,
	CmdInject,
	CmdExport,
	CmdZed,
	CmdMcp,
	CmdTemplates,
	CmdVersion,
}
