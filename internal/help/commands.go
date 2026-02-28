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
system. Handles two event types:

  SessionEnd — parses transcript, writes a finalized session note
  Stop       — captures a mid-session checkpoint (no LLM enrichment)

Checkpoint notes are provisional: a subsequent Stop overwrites the
previous checkpoint, and SessionEnd finalizes it. Clear events and
unknown events are silently ignored.

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

// HookSubcommands is the ordered list of hook sub-subcommands.
var HookSubcommands = []Command{
	CmdHookInstall,
	CmdHookUninstall,
}

// Subcommands is the ordered list of all subcommands.
var Subcommands = []Command{
	CmdInit,
	CmdHook,
	CmdProcess,
	CmdIndex,
	CmdBackfill,
	CmdArchive,
	CmdReprocess,
	CmdCheck,
	CmdStats,
	CmdFriction,
	CmdTrends,
	CmdVersion,
}
