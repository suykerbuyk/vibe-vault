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
	Name        string // "init", "hook", etc; "" for top-level
	Synopsis    string // one-line description (lowercase, for --help header)
	Brief       string // short description for usage table (capitalized)
	Usage       string // full usage line, e.g. "vv init [path] [--git]"
	TableUsage  string // shortened usage for the top-level table (if different from Usage)
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
	Name:       "hook",
	Synopsis:   "Claude Code hook handler",
	Brief:      "Hook mode (reads stdin from Claude Code)",
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
		{Name: "--source <name>", Desc: "Filter by source (zed, claude-code)"},
		{Name: "--dry-run", Desc: "Show what would be reprocessed without writing"},
		{Name: "--backfill-context", Desc: "Populate ContextAvailable on entries (no reprocessing)"},
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
	Usage:      "vv stats [--project <name>] [--source <name>]",
	TableUsage: "vv stats [--project X]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Show stats for a specific project only"},
		{Name: "--source <name>", Desc: "Filter by source (zed, claude-code, or all)"},
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
	Usage:      "vv friction [--project <name>] [--source <name>]",
	TableUsage: "vv friction [--project X]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Show friction for a specific project only"},
		{Name: "--source <name>", Desc: "Filter by source (zed, claude-code, or all)"},
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
	Usage:      "vv trends [--project <name>] [--weeks <n>] [--source <name>]",
	TableUsage: "vv trends [--project X]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Show trends for a specific project only"},
		{Name: "--weeks <n>", Desc: "Number of weeks to display (default: 12)"},
		{Name: "--source <name>", Desc: "Filter by source (zed, claude-code, or all)"},
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

var CmdEffectiveness = Command{
	Name:       "effectiveness",
	Synopsis:   "analyze whether context availability improves session outcomes",
	Brief:      "Analyze context effectiveness on outcomes",
	Usage:      "vv effectiveness [--project <name>] [--format json]",
	TableUsage: "vv effectiveness [--project X]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Show effectiveness for a specific project only"},
		{Name: "--format <json>", Desc: "Output as JSON instead of human-readable text"},
	},
	Description: `Correlates context depth (number of prior sessions available) with
session outcomes (friction, corrections, duration). Groups sessions
into cohorts by context depth and computes Pearson correlation.

Cohorts:
  none (0)       No prior sessions available
  early (1-10)   Building initial context
  building (11-30)  Growing context base
  mature (30+)   Rich context available

Requires vv reprocess --backfill-context to have been run first to
populate ContextAvailable data on historical sessions.`,
	Examples: []string{
		"vv effectiveness                       Show all projects",
		"vv effectiveness --project myproject   Show one project",
		"vv effectiveness --format json         Output as JSON",
	},
	SeeAlso: []string{"vv(1)", "vv-friction(1)", "vv-trends(1)", "vv-reprocess(1)"},
}

var CmdMcp = Command{
	Name:       "mcp",
	Synopsis:   "start MCP server for AI agent integration",
	Brief:      "Start MCP server (JSON-RPC over stdio)",
	TableUsage: "vv mcp [install | ...]",
	Usage:      "vv mcp\n    vv mcp install [--claude-only | --zed-only]\n    vv mcp uninstall [--claude-only | --zed-only]",
	Description: `Starts a Model Context Protocol (MCP) server that exposes vibe-vault
tools over JSON-RPC 2.0 on stdin/stdout. This allows AI agents like
Claude Code and Zed to query project context programmatically.

Subcommands:
  install     Register the MCP server in editor settings
  uninstall   Remove the MCP server from editor settings

Available tools:
  vv_append_iteration     Append a narrative to the iteration log
  vv_bootstrap_context    Single-call session start (resume + workflow + tasks)
  vv_capture_session      Record a session note from an agent conversation
  vv_get_effectiveness    Context effectiveness analysis
  vv_get_friction_trends  Friction and efficiency trend data over time
  vv_get_knowledge        Project knowledge.md content
  vv_get_project_context  Condensed project context (sessions, threads,
                          decisions, friction trends)
  vv_get_resume           Current resume.md content
  vv_get_session_detail   Full markdown of a specific session note
  vv_get_task             Read a specific task file
  vv_get_workflow         Current workflow.md content
  vv_list_projects        All projects with session counts and date ranges
  vv_list_tasks           List active and completed task files
  vv_manage_task          Create, update, or complete task files
  vv_refresh_index        Rebuild the session index
  vv_search_sessions      Search/filter sessions by query, project, files,
                          date range, friction score
  vv_update_resume        Update resume.md sections

Available prompts:
  vv_session_guidelines   Agent instructions for session capture

Setup:
  vv mcp install        # installs into all detected editors
  (restart your editor)

Verify — after restarting your editor, ask the agent to "list
vibe-vault projects" or "capture this session". The agent will
call the MCP tools automatically.

The server logs tool calls to stderr for observability.`,
	SeeAlso: []string{"vv(1)", "vv-inject(1)", "vv-mcp-install(1)", "vv-mcp-uninstall(1)"},
}

var CmdMcpInstall = Command{
	Name:     "mcp install",
	Synopsis: "register MCP server in editor settings",
	Brief:    "Add vibe-vault MCP server to settings.json",
	Usage:    "vv mcp install [--claude-only | --zed-only | --claude-plugin]",
	Flags: []Flag{
		{Name: "--claude-only", Desc: "Install only into Claude Code (~/.claude/settings.json)"},
		{Name: "--zed-only", Desc: "Install only into Zed (~/.config/zed/settings.json)"},
		{Name: "--claude-plugin", Desc: "Deploy as Claude Code plugin (fixes tool registration bug #2682)"},
	},
	Description: `Adds a "vibe-vault" entry to each detected editor's MCP/context server
settings. By default, installs into all editors whose config directories
exist (~/.claude/ for Claude Code, ~/.config/zed/ for Zed).

Editors not detected are skipped with an advisory message.

Creates the settings file and parent directory if they don't exist.
Preserves all existing settings and MCP servers. A backup is saved to
settings.json.vv.bak before any modification.

This command is idempotent: running it when the MCP server is already
configured prints an informational message and exits successfully.

Use --claude-plugin if Claude Code does not expose vibe-vault tools after
a standard install. This deploys vibe-vault as a local plugin, which uses
a different (working) code path for tool registration. Both plugin and
mcpServers entries can safely coexist.

Restart the editor after running this command.`,
	SeeAlso: []string{"vv-mcp(1)", "vv-mcp-uninstall(1)"},
}

var CmdMcpUninstall = Command{
	Name:     "mcp uninstall",
	Synopsis: "remove MCP server from editor settings",
	Brief:    "Remove vibe-vault MCP server from settings.json",
	Usage:    "vv mcp uninstall [--claude-only | --zed-only | --claude-plugin]",
	Flags: []Flag{
		{Name: "--claude-only", Desc: "Uninstall only from Claude Code (~/.claude/settings.json)"},
		{Name: "--zed-only", Desc: "Uninstall only from Zed (~/.config/zed/settings.json)"},
		{Name: "--claude-plugin", Desc: "Remove the Claude Code plugin and its marketplace entry"},
	},
	Description: `Removes the "vibe-vault" entry from each detected editor's MCP/context
server settings. By default, removes from all editors whose config
directories exist.

Use --claude-plugin to remove the plugin installed by
"vv mcp install --claude-plugin". This removes the plugin directory,
marketplace registration, and enabledPlugins entry.

Preserves all other settings and MCP servers. A backup is saved to
settings.json.vv.bak before any modification.

This command is idempotent: running it when the MCP server is not
configured prints an informational message and exits successfully.`,
	SeeAlso: []string{"vv-mcp(1)", "vv-mcp-install(1)"},
}

var CmdConfig = Command{
	Name:       "config",
	Synopsis:   "manage vibe-vault configuration",
	Brief:      "Manage configuration (provider keys, etc.)",
	Usage:      "vv config [set-key]",
	TableUsage: "vv config [set-key | ...]",
	Description: `Manages settings stored in ~/.config/vibe-vault/config.toml.

Subcommands:
  set-key   Store a per-provider API key (anthropic, openai, google)

The wrap render path (vv_render_wrap_text) and hook enrichment /
synthesis both resolve provider keys via a layered lookup: the value
in config.toml wins, falling back to the provider's environment
variable (ANTHROPIC_API_KEY / OPENAI_API_KEY / GOOGLE_API_KEY) for
operators who already have shell-env-based setup.`,
	SeeAlso: []string{"vv(1)", "vv-config-set-key(1)"},
}

var CmdConfigSetKey = Command{
	Name:     "config set-key",
	Synopsis: "store a provider API key in config.toml",
	Brief:    "Set a per-provider API key",
	Usage:    "vv config set-key [--force] <provider> <key|->",
	Args: []Arg{
		{Name: "provider", Desc: "Provider short name: anthropic, openai, or google"},
		{Name: "key", Desc: `API key value, or "-" to read from stdin`},
	},
	Flags: []Flag{
		{Name: "--force", Desc: "Overwrite an existing key for this provider"},
	},
	Description: `Writes [providers.<provider>].api_key into ~/.config/vibe-vault/config.toml,
preserving all other lines, comments, and sections (line-oriented edit).
The file is written atomically with mode 0600; the parent directory is
chmod'd to 0700.

Stdin form: when the key argument is "-", the value is read from stdin
and a single trailing newline is trimmed (so "echo $KEY | vv config
set-key anthropic -" works). Embedded newlines and surrounding
whitespace are rejected.

Refuses to overwrite an existing non-empty key for the same provider
unless --force is passed.

Once set, the dispatch handler and hook/synthesis paths resolve their
key from config first, falling back to the provider's environment
variable (ANTHROPIC_API_KEY, OPENAI_API_KEY, GOOGLE_API_KEY) when the
config slot is empty.`,
	Examples: []string{
		"vv config set-key anthropic sk-ant-...     Store an Anthropic key",
		"echo $KEY | vv config set-key openai -     Pipe key from stdin",
		"vv config set-key --force google new-key   Overwrite existing key",
	},
	SeeAlso: []string{"vv(1)", "vv-config(1)"},
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
	Name:       "context",
	Synopsis:   "manage vault-resident AI context files",
	Brief:      "Manage vault-resident AI context",
	Usage:      "vv context [init | migrate | sync]",
	TableUsage: "vv context [init | ...]",
	Description: `Manages AI workflow context files (resume, iterations, tasks) that live
in the Obsidian vault rather than as untracked repo-local files. This
makes context portable, searchable, and visible to Obsidian.

Typical workflow:
  1. vv context init     First-time setup for a new project
  2. vv context sync     Run after updating vv to get new features

Sync uses three-way comparison (template vs baseline vs project file)
to auto-update untouched files and preserve user customizations. Use
--force to override conflicts. Use .pinned markers to permanently
opt out of updates for specific files.

Use "migrate" only if you have an older project with local RESUME.md
or HISTORY.md files that predate vault-resident context.

Subcommands:
  vv context init      First-time setup: create context files + repo bootstrap
  vv context migrate   One-time: move legacy local files into vault
  vv context sync      Ongoing: apply schema upgrades + deploy commands`,
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
	Description: `First-time setup for a project. Run this once from your repo root.

Creates vault-side context files:
  resume.md        AI working context (current state, open threads)
  iterations.md    Iteration history (append-only archive)
  workflow.md      AI behavioral rules and workflow standards
  tasks/           Task tracking directory
  commands/        Slash commands (restart, wrap, license, makefile)

Creates repo-side files:
  CLAUDE.md            MCP-first instructions (loaded by AI agents)
  .claude/commands/    Slash commands (deployed from vault)
  .vibe-vault.toml     Project identity file (committed to repo)

The .vibe-vault.toml is created with all values commented out. While
commented, vv uses heuristics (git remote, directory name) to detect
the project. Uncomment values to override detection — useful when
the git remote name doesn't match your project name.

Project name is auto-detected from .vibe-vault.toml, git remote, or
directory name. Existing files are skipped unless --force is specified.`,
	Examples: []string{
		"vv context init                       Scaffold for auto-detected project",
		"vv context init --project myproject   Scaffold for a specific project",
		"vv context init --force               Overwrite existing files",
	},
	SeeAlso: []string{"vv(1)", "vv-context(1)", "vv-context-sync(1)"},
}

var CmdContextMigrate = Command{
	Name:     "context migrate",
	Synopsis: "one-time import of legacy local context files to vault",
	Brief:    "One-time import of legacy local files to vault",
	Usage:    "vv context migrate [--project <name>] [--force]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Override auto-detected project name"},
		{Name: "--force", Desc: "Overwrite existing vault files"},
	},
	Description: `One-time operation for projects that have local context files from
before vault-resident context was introduced. Most users should use
"vv context init" instead.

Copies local files into the vault:
  RESUME.md  → Projects/{project}/agentctx/resume.md
  HISTORY.md → Projects/{project}/agentctx/iterations.md
  tasks/     → Projects/{project}/agentctx/tasks/ (recursive)

Then sets up repo-side files (same as init). Local originals are
preserved — remove them manually after verifying the migration.

After migrating, use "vv context sync" for future updates.`,
	Examples: []string{
		"vv context migrate                       Migrate auto-detected project",
		"vv context migrate --project myproject   Migrate a specific project",
		"vv context migrate --force               Overwrite existing vault files",
	},
	SeeAlso: []string{"vv(1)", "vv-context(1)", "vv-context-init(1)"},
}

var CmdContextSync = Command{
	Name:     "context sync",
	Synopsis: "update project context after upgrading vv",
	Brief:    "Update context after upgrading vv",
	Usage:    "vv context sync [--project <name>] [--all] [--dry-run] [--force]",
	Flags: []Flag{
		{Name: "--project <name>", Desc: "Override auto-detected project name"},
		{Name: "--all", Desc: "Sync all projects (vault-only, skip repo deployment)"},
		{Name: "--dry-run", Desc: "Report changes without modifying any files"},
		{Name: "--force", Desc: "Overwrite user-customized files (resolve conflicts)"},
	},
	Description: `Run this from your repo root after upgrading vv to pick up new features.

What sync does:
  1. Refreshes vault templates from the Go-embedded defaults
  2. Schema migrations — upgrades the vault-side agentctx directory and
     repo-side files (CLAUDE.md, .claude/commands/) to the latest version
  3. Template propagation — uses three-way comparison (template vs baseline
     vs project file) to update commands and skills

Three-way sync behavior:
  - Template unchanged → nothing to do
  - Template changed, you didn't edit → auto-update (UPDATE)
  - Template changed, you also edited → skip (CONFLICT)
  - New file in template → create (CREATE)
  - You edited, template unchanged → preserved (no action)

Use --force to overwrite CONFLICT files. Use .pinned markers to
permanently opt out of updates for specific files.

In --all mode, only vault-side operations are performed (no repo-side
deployment). Run from each repo root without --all for repo-side updates.`,
	Examples: []string{
		"vv context sync                       Sync current project",
		"vv context sync --dry-run             Preview changes",
		"vv context sync --all                 Sync all projects (vault-only)",
		"vv context sync --force               Override conflicts",
	},
	SeeAlso: []string{"vv(1)", "vv-context(1)", "vv-templates(1)"},
}

var CmdTemplates = Command{
	Name:       "templates",
	Synopsis:   "inspect, compare, and reset vault templates",
	Brief:      "Inspect, compare, and reset vault templates",
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
  vv zed list       List threads in the database
  vv zed watch      Watch for changes and auto-capture`,
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

var CmdMemory = Command{
	Name:       "memory",
	Synopsis:   "manage Claude Code auto-memory symlink into vault",
	Brief:      "Link Claude Code auto-memory into vault",
	Usage:      "vv memory [link | unlink]",
	TableUsage: "vv memory [link | ...]",
	Description: `Establishes (or removes) a symlink from Claude Code's per-project
auto-memory directory (~/.claude/projects/{slug}/memory/) into the
project's vault-resident agentctx/memory/ directory.

Once linked, Claude Code's native memory writes land on vault disk
transparently — the vault git sync then carries them across machines,
eliminating the per-host drift that otherwise plagues auto-memory.

Only projects that have already been marked vibe-vault-tracked
(i.e. Projects/{name}/agentctx/ exists in the vault) can be linked.
Run 'vv init' (or 'vv context init') first for new projects.

Subcommands:
  vv memory link     Symlink host-local memory into the vault
  vv memory unlink   Reverse the symlink (vault copy preserved)`,
	SeeAlso: []string{"vv(1)", "vv-memory-link(1)", "vv-memory-unlink(1)", "vv-context(1)"},
}

var CmdMemoryLink = Command{
	Name:     "memory link",
	Synopsis: "symlink host-local auto-memory into the vault",
	Brief:    "Symlink host-local memory into the vault",
	Usage:    "vv memory link [--working-dir <path>] [--force] [--dry-run]",
	Flags: []Flag{
		{Name: "--working-dir <path>", Desc: "Project working directory (default: current directory)"},
		{Name: "--force", Desc: "Override refusal on wrong-symlink or conflicting files"},
		{Name: "--dry-run", Desc: "Report actions without modifying any files"},
	},
	Description: `Computes the Claude slug from the working directory (symlinks
resolved, trailing slashes normalized), resolves the vibe-vault
project name via the same detector used by the rest of vv
(.vibe-vault.toml → git remote → basename), and arranges:

  1. Creates VibeVault/Projects/{name}/agentctx/memory/ if missing.
  2. Creates ~/.claude/projects/{slug}/ parent if missing (fresh
     machine where Claude Code has not opened the project yet).
  3. Migrates any pre-existing host-local memory into the vault
     target (identical files dropped, unique files moved).
  4. On content conflict without --force, refuses. With --force,
     quarantines host-local copies under
     agentctx/memory-conflicts/{timestamp}/ (a SIBLING of memory/,
     not a child — so they never pollute auto-memory output).
  5. Creates the symlink.

Already-linked projects report "already linked" and exit 0.`,
	Examples: []string{
		"vv memory link                     Link from current directory",
		"vv memory link --dry-run           Preview what would happen",
		"vv memory link --force             Migrate despite conflicts",
	},
	SeeAlso: []string{"vv(1)", "vv-memory(1)", "vv-memory-unlink(1)"},
}

var CmdMemoryUnlink = Command{
	Name:     "memory unlink",
	Synopsis: "restore host-local auto-memory (rollback)",
	Brief:    "Reverse memory link (vault copy preserved)",
	Usage:    "vv memory unlink [--working-dir <path>] [--force] [--dry-run]",
	Flags: []Flag{
		{Name: "--working-dir <path>", Desc: "Project working directory (default: current directory)"},
		{Name: "--force", Desc: "Detach even if the symlink target is unrelated"},
		{Name: "--dry-run", Desc: "Report actions without modifying any files"},
	},
	Description: `Removes the host-local symlink, creates a real directory in its
place, and copies every file from the vault target into it. The
vault copy is NOT removed — it remains the durable store, so this
command is safe to re-run.

Refuses if the host-local path is a real directory (nothing to
undo) or a symlink pointing somewhere unexpected (re-run with
--force to detach anyway).

This inverse is primarily for rollback; normal usage is link-only.`,
	Examples: []string{
		"vv memory unlink                   Reverse link for current project",
		"vv memory unlink --dry-run         Preview rollback",
	},
	SeeAlso: []string{"vv(1)", "vv-memory(1)", "vv-memory-link(1)"},
}

// MemorySubcommands is the ordered list of memory sub-subcommands.
var MemorySubcommands = []Command{
	CmdMemoryLink,
	CmdMemoryUnlink,
}

var CmdVault = Command{
	Name:     "vault",
	Synopsis: "vault git synchronization",
	Brief:    "Vault git sync (pull, push, status, recover)",
	Usage:    "vv vault <command>",
	Description: `Manages git synchronization of the vault repo across machines.
The vault repo is owned entirely by vv — all git operations are safe.

Subcommands:
  vv vault status    Show vault git state (clean/dirty, ahead/behind)
  vv vault pull      Fetch + rebase with automatic conflict resolution
  vv vault push      Commit all changes and push
  vv vault recover   List upstream commits whose content was dropped on rebase`,
}

var CmdVaultStatus = Command{
	Name:     "status",
	Synopsis: "show vault git state",
	Brief:    "Show vault git state",
	Usage:    "vv vault status",
	Description: `Shows the vault repository's git status: current branch,
clean/dirty state, ahead/behind counts, and remote configuration.`,
}

var CmdVaultPull = Command{
	Name:     "pull",
	Synopsis: "fetch and rebase vault repo",
	Brief:    "Fetch + rebase with auto conflict resolution",
	Usage:    "vv vault pull",
	Description: `Fetches from all configured remotes and rebases local commits on
the tracking upstream (falls back to first remote). Rebase conflicts are
auto-resolved by KEEPING LOCAL across all four file classes — local work is
the most recent operator intent on this machine. Per-class side-effects:
  - Auto-generated files (history.md, session-index): keep local; mark
    Regenerate so the caller runs 'vv index' to rebuild.
  - Session notes: keep local; unique timestamp filenames make collisions
    near-impossible.
  - Manual files (knowledge.md, resume.md, iterations.md, tasks): keep
    local; record the upstream commit's SHA and subject for inspection.
  - Templates/config: keep local; templates change rarely, config is
    host-local-adjacent.

When upstream Manual-class content is dropped, a WARNING is printed to
stderr listing the dropped commits. Inspect and recover with:
  vv vault recover [--days N]

If regeneration is needed, run 'vv index' afterward to rebuild.`,
}

var CmdVaultRecover = Command{
	Name:     "recover",
	Synopsis: "list upstream commits whose content was dropped on rebase",
	Brief:    "List/inspect dropped upstream content",
	Usage:    "vv vault recover [--days N] [--show <sha>] [--diff <sha> -- <path>]",
	Flags: []Flag{
		{"--days <N>", "Window in days to walk reachable history (default: 7; no upper cap)"},
		{"--show <sha>", "Run `git show <sha>` for the named commit"},
		{"--diff <sha> -- <path>", "Print `git show <sha>:<path>` next to `git show HEAD:<path>`"},
	},
	Description: `Walks reachable history from HEAD back N days and reports upstream
commits whose recorded blob for at least one Manual-class file (knowledge.md,
resume.md, iterations.md, tasks/*) differs from HEAD's current blob — the
candidates whose content a prior 'vv vault pull' rebase resolution dropped in
favor of local work.

Reflog is NOT consulted: after a peer machine's rebase pushes to the remote,
that machine's prior commits remain reachable from main; the "drop" happened
to file content during merge resolution, not to commit reachability.

There is no --apply flag; manual integration preserves operator judgment about
ordering and merge style.`,
}

var CmdVaultPush = Command{
	Name:     "push",
	Synopsis: "commit and push vault changes",
	Brief:    "Commit all changes and push to all remotes",
	Usage:    "vv vault push [--message <msg>]",
	Flags: []Flag{
		{"--message <msg>", "Custom commit message (default: auto-generated)"},
	},
	Description: `Stages all vault changes, commits with a machine-stamped message,
and pushes to all configured remotes. For each remote, if push is rejected,
fetches and rebases from that remote and retries once. Reports per-remote
success or failure.`,
}

// VaultSubcommands is the ordered list of vault sub-subcommands.
var VaultSubcommands = []Command{
	CmdVaultStatus,
	CmdVaultPull,
	CmdVaultPush,
	CmdVaultRecover,
}

var CmdWorktree = Command{
	Name:       "worktree",
	Synopsis:   "subagent worktree management",
	Brief:      "Manage subagent worktrees (gc)",
	TableUsage: "vv worktree [gc | ...]",
	Usage:      "vv worktree <command>",
	Description: `Manages git worktrees created by AI subagents under
.claude/worktrees/. Subagents lock their worktrees with a marker
("claude agent <id> (pid N)") so a crash leaves them on disk; this
command cluster reaps the stale ones safely.

Subcommands:
  vv worktree gc   Reap stale subagent worktrees with capture verification`,
	SeeAlso: []string{"vv(1)", "vv-worktree-gc(1)"},
}

var CmdWorktreeGc = Command{
	Name:     "worktree gc",
	Synopsis: "reap stale subagent worktrees",
	Brief:    "Reap stale subagent worktrees with capture verification",
	Usage:    "vv worktree gc [--dry-run] [--json] [--candidate-parents <csv>] [--force-uncaptured]",
	Flags: []Flag{
		{Name: "--dry-run", Desc: "Report would-reap actions without destructive changes"},
		{Name: "--json", Desc: "Emit Result as indented JSON to stdout"},
		{Name: "--candidate-parents <csv>", Desc: "Comma-separated branch list for capture verification (default: resolved at runtime)"},
		{Name: "--force-uncaptured", Desc: "Reap even when the worktree branch contains commits not in any candidate parent"},
	},
	Description: `Scans every locked worktree under the current repository,
inspects its claude-agent marker, probes the holder PID for liveness,
and (when the holder is dead) verifies the worktree branch's commits
are captured by an authoritative parent before destructively removing
the worktree. Default-branch-aware via git symbolic-ref.

A repo-level lockfile keyed by absolute git-common-dir serializes
concurrent invocations across linked worktrees of the same repository.

Exit codes:
  0  success (per-worktree errors are surfaced in the output)
  2  fatal failure (lock contention, enumeration error)`,
	Examples: []string{
		"vv worktree gc --dry-run                          Inspect without destruction",
		"vv worktree gc                                    Reap stale worktrees",
		"vv worktree gc --candidate-parents main,develop   Override default parents",
		"vv worktree gc --force-uncaptured                 Reap even uncaptured branches",
	},
	SeeAlso: []string{"vv(1)", "vv-worktree(1)"},
}

// WorktreeSubcommands is the ordered list of worktree sub-subcommands.
var WorktreeSubcommands = []Command{
	CmdWorktreeGc,
}

// ZedSubcommands is the ordered list of zed sub-subcommands.
var ZedSubcommands = []Command{
	CmdZedBackfill,
	CmdZedList,
	CmdZedWatch,
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

// McpSubcommands is the ordered list of mcp sub-subcommands.
var McpSubcommands = []Command{
	CmdMcpInstall,
	CmdMcpUninstall,
}

// ConfigSubcommands is the ordered list of config sub-subcommands.
var ConfigSubcommands = []Command{
	CmdConfigSetKey,
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
	CmdEffectiveness,
	CmdMemory,
	CmdVault,
	CmdZed,
	CmdMcp,
	CmdWorktree,
	CmdConfig,
	CmdTemplates,
	CmdVersion,
}
