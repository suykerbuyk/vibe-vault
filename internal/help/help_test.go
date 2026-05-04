package help

import (
	"fmt"
	"strings"
	"testing"
)

// expectedTerminal maps command name → exact expected terminal output.
// These strings are copied verbatim from the original inline help in main.go.
var expectedTerminal = map[string]string{
	"init": "vv init \u2014 create a new Obsidian vault for session notes\n" +
		"\n" +
		"Usage: vv init [path] [--git]\n" +
		"\n" +
		"Arguments:\n" +
		"  path       Target directory (default: ./vibe-vault)\n" +
		"\n" +
		"Flags:\n" +
		"  --git      Initialize a git repository in the new vault\n" +
		"\n" +
		"Creates a fully configured Obsidian vault with Dataview dashboards,\n" +
		"Templater templates, and session capture infrastructure. Also writes\n" +
		"a default config to ~/.config/vibe-vault/config.toml pointing at the\n" +
		"new vault.\n" +
		"\n" +
		"Examples:\n" +
		"  vv init                       Create ./vibe-vault\n" +
		"  vv init ~/obsidian/my-vault   Create at a specific path\n" +
		"  vv init --git                 Create with git repo initialized\n",

	"hook": "vv hook \u2014 Claude Code hook handler\n" +
		"\n" +
		"Usage: vv hook [install | uninstall | --event <name>]\n" +
		"\n" +
		"Flags:\n" +
		"  --event <name>   Override the hook event type (default: read from stdin)\n" +
		"\n" +
		"Reads a JSON payload from stdin as delivered by Claude Code's hook\n" +
		"system. Handles three event types:\n" +
		"\n" +
		"  SessionEnd \u2014 parses transcript, writes a finalized session note\n" +
		"  Stop       \u2014 captures a mid-session checkpoint (no LLM enrichment)\n" +
		"  PreCompact \u2014 captures a checkpoint before context compaction\n" +
		"\n" +
		"Checkpoint notes are provisional: a subsequent Stop or PreCompact\n" +
		"overwrites the previous checkpoint, and SessionEnd finalizes it.\n" +
		"\n" +
		"This command is meant to be called by Claude Code, not directly.\n" +
		"\n" +
		"Subcommands:\n" +
		"  vv hook install     Add vv hooks to ~/.claude/settings.json\n" +
		"  vv hook uninstall   Remove vv hooks from ~/.claude/settings.json\n",

	"process": "vv process \u2014 process a single transcript file\n" +
		"\n" +
		"Usage: vv process <transcript.jsonl>\n" +
		"\n" +
		"Arguments:\n" +
		"  transcript.jsonl   Path to a Claude Code JSONL transcript\n" +
		"\n" +
		"Parses the transcript, detects the project from the session's working\n" +
		"directory, and writes a session note to the vault. Skips trivial\n" +
		"sessions (< 2 messages) and already-indexed sessions.\n" +
		"\n" +
		"Examples:\n" +
		"  vv process ~/.claude/projects/-home-user-myproject/abc123.jsonl\n",

	"check": "vv check \u2014 validate config, vault, and hook setup\n" +
		"\n" +
		"Usage: vv check\n" +
		"\n" +
		"Runs diagnostic checks and prints a pass/warn/FAIL report:\n" +
		"  - Config file location and validity\n" +
		"  - Vault directory exists\n" +
		"  - Obsidian config present (.obsidian/)\n" +
		"  - Projects directory and note count\n" +
		"  - State directory (.vibe-vault/)\n" +
		"  - Session index validity and entry count\n" +
		"  - Domain paths exist\n" +
		"  - Enrichment config and API key\n" +
		"  - Claude Code hook setup in ~/.claude/settings.json\n" +
		"\n" +
		"Exit code 0 if all checks pass or warn, 1 if any check fails.\n",

	"index": "vv index \u2014 rebuild session index from notes\n" +
		"\n" +
		"Usage: vv index\n" +
		"\n" +
		"Walks Projects/*/sessions/**/*.md in the vault, parses frontmatter from each\n" +
		"note, and rebuilds .vibe-vault/session-index.json. The recursive ** glob\n" +
		"covers the flat layout, the per-host subtree (sessions/<host>/<date>/),\n" +
		"and the _pre-staging-archive/ migration archive. Preserves TranscriptPath\n" +
		"values from the existing index. Generates a history.md document for\n" +
		"each project with timeline, decisions, open threads, and key files.\n" +
		"\n" +
		"Use this after manually editing or deleting session notes.\n",

	"backfill": "vv backfill \u2014 discover and process historical transcripts\n" +
		"\n" +
		"Usage: vv backfill [path]\n" +
		"\n" +
		"Arguments:\n" +
		"  path   Directory to scan for transcripts (default: ~/.claude/projects/)\n" +
		"\n" +
		"Recursively discovers Claude Code JSONL transcripts by UUID filename\n" +
		"pattern, skips already-indexed sessions, and processes the rest through\n" +
		"the full capture pipeline. Also patches TranscriptPath on existing index\n" +
		"entries that lack it.\n" +
		"\n" +
		"Subagent transcripts (in /subagents/ subdirectories) are automatically\n" +
		"filtered out.\n" +
		"\n" +
		"Examples:\n" +
		"  vv backfill                              Scan default Claude projects dir\n" +
		"  vv backfill ~/.claude/projects/myproj    Scan a specific directory\n",

	"archive": "vv archive \u2014 compress transcripts into vault archive\n" +
		"\n" +
		"Usage: vv archive\n" +
		"\n" +
		"Iterates all sessions in the index and compresses each transcript to\n" +
		".vibe-vault/archive/{session-id}.jsonl.zst using zstd compression\n" +
		"(typically ~10:1 on JSONL). Skips already-archived and missing\n" +
		"transcripts. Originals are not deleted.\n" +
		"\n" +
		"Reports total bytes before and after compression.\n",

	"reprocess": "vv reprocess \u2014 re-generate notes from transcripts\n" +
		"\n" +
		"Usage: vv reprocess [--project <name>]\n" +
		"\n" +
		"Flags:\n" +
		"  --project <name>     Only reprocess sessions for this project\n" +
		"  --source <name>      Filter by source (zed, claude-code)\n" +
		"  --dry-run            Show what would be reprocessed without writing\n" +
		"  --backfill-context   Populate ContextAvailable on entries (no reprocessing)\n" +
		"\n" +
		"Re-runs the capture pipeline with Force mode for all (or filtered)\n" +
		"sessions in the index. Locates transcripts via three-tier lookup:\n" +
		"\n" +
		"  1. Original path (TranscriptPath in index)\n" +
		"  2. Archived copy (.vibe-vault/archive/)\n" +
		"  3. Fallback discovery scan (~/.claude/projects/)\n" +
		"\n" +
		"Overwrites existing notes in place (preserves iteration numbers).\n" +
		"Regenerates history.md for each affected project.\n" +
		"\n" +
		"Examples:\n" +
		"  vv reprocess                       Reprocess all sessions\n" +
		"  vv reprocess --project myproject   Reprocess one project only\n",

	"stats": "vv stats \u2014 show session analytics and metrics\n" +
		"\n" +
		"Usage: vv stats [--project <name>] [--source <name>]\n" +
		"\n" +
		"Flags:\n" +
		"  --project <name>   Show stats for a specific project only\n" +
		"  --source <name>    Filter by source (zed, claude-code, or all)\n" +
		"\n" +
		"Computes aggregate metrics from the session index and displays them\n" +
		"in aligned terminal output. Shows overview totals, per-project and\n" +
		"per-model breakdowns, activity tag distribution, monthly trends,\n" +
		"and top files.\n" +
		"\n" +
		"All data is read from the session index \u2014 no note re-parsing needed.\n" +
		"Run vv index first if token data appears incomplete (backfills token\n" +
		"counts from note frontmatter).\n" +
		"\n" +
		"Examples:\n" +
		"  vv stats                       Show global stats\n" +
		"  vv stats --project myproject   Show stats for one project\n",

	"friction": "vv friction \u2014 show friction analysis and correction patterns\n" +
		"\n" +
		"Usage: vv friction [--project <name>] [--source <name>]\n" +
		"\n" +
		"Flags:\n" +
		"  --project <name>   Show friction for a specific project only\n" +
		"  --source <name>    Filter by source (zed, claude-code, or all)\n" +
		"\n" +
		"Analyzes friction signals from the session index: correction density,\n" +
		"token efficiency, file retry patterns, error cycles, and recurring\n" +
		"open threads. Shows per-project aggregates and identifies high-friction\n" +
		"sessions.\n" +
		"\n" +
		"Friction scores range from 0 (smooth) to 100 (high friction). Sessions\n" +
		"scoring \u2265 40 are flagged as high-friction. Run vv reprocess to generate\n" +
		"friction data if none is available.\n" +
		"\n" +
		"Examples:\n" +
		"  vv friction                       Show global friction analysis\n" +
		"  vv friction --project myproject   Show friction for one project\n",

	"trends": "vv trends \u2014 show metric trends over time\n" +
		"\n" +
		"Usage: vv trends [--project <name>] [--weeks <n>] [--source <name>]\n" +
		"\n" +
		"Flags:\n" +
		"  --project <name>   Show trends for a specific project only\n" +
		"  --weeks <n>        Number of weeks to display (default: 12)\n" +
		"  --source <name>    Filter by source (zed, claude-code, or all)\n" +
		"\n" +
		"Analyzes metric trends from the session index over time using weekly\n" +
		"buckets with 4-week rolling averages. Shows direction (improving,\n" +
		"worsening, stable) by comparing the most recent 4 weeks against the\n" +
		"previous 4 weeks.\n" +
		"\n" +
		"Tracks four metrics:\n" +
		"  - Friction score (average per week)\n" +
		"  - Tokens per file (total tokens / files changed)\n" +
		"  - Corrections per session\n" +
		"  - Session duration\n" +
		"\n" +
		"Weeks where a metric deviates more than 1.5 standard deviations from\n" +
		"its rolling average are flagged as anomalies (spikes or dips).\n" +
		"\n" +
		"Examples:\n" +
		"  vv trends                       Show global trends (last 12 weeks)\n" +
		"  vv trends --weeks 24            Show last 24 weeks\n" +
		"  vv trends --project myproject   Show trends for one project\n",

	"context": "vv context \u2014 manage vault-resident AI context files\n" +
		"\n" +
		"Usage: vv context [init | migrate | sync]\n" +
		"\n" +
		"Manages AI workflow context files (resume, iterations, tasks) that live\n" +
		"in the Obsidian vault rather than as untracked repo-local files. This\n" +
		"makes context portable, searchable, and visible to Obsidian.\n" +
		"\n" +
		"Typical workflow:\n" +
		"  1. vv context init     First-time setup for a new project\n" +
		"  2. vv context sync     Run after updating vv to get new features\n" +
		"\n" +
		"Sync uses three-way comparison (template vs baseline vs project file)\n" +
		"to auto-update untouched files and preserve user customizations. Use\n" +
		"--force to override conflicts. Use .pinned markers to permanently\n" +
		"opt out of updates for specific files.\n" +
		"\n" +
		"Use \"migrate\" only if you have an older project with local RESUME.md\n" +
		"or HISTORY.md files that predate vault-resident context.\n" +
		"\n" +
		"Subcommands:\n" +
		"  vv context init      First-time setup: create context files + repo bootstrap\n" +
		"  vv context migrate   One-time: move legacy local files into vault\n" +
		"  vv context sync      Ongoing: apply schema upgrades + deploy commands\n",

	"inject": "vv inject \u2014 output session-start context payload\n" +
		"\n" +
		"Usage: vv inject [--project <name>] [--format <md|json>] [--sections <list>] [--max-tokens <n>]\n" +
		"\n" +
		"Flags:\n" +
		"  --project <name>     Project to inject context for (default: auto-detect)\n" +
		"  --format <md|json>   Output format (default: md)\n" +
		"  --sections <list>    Comma-separated sections to include (default: all)\n" +
		"  --max-tokens <n>     Token budget for output (default: 2000)\n" +
		"\n" +
		"Outputs a condensed, token-budgeted context payload for a project.\n" +
		"Assembles recent sessions, open threads, decisions, friction trends,\n" +
		"and knowledge notes into a single document suitable for injection at\n" +
		"session start.\n" +
		"\n" +
		"Available sections (in priority order):\n" +
		"  summary      Most recent session summary\n" +
		"  sessions     Last 5 sessions, newest first\n" +
		"  threads      Open threads from last 5 sessions (resolved filtered out)\n" +
		"  decisions    Decisions from last 30 days (deduped)\n" +
		"  friction     Friction trend direction and rolling average\n" +
		"  knowledge    Relevant knowledge notes (project + agnostic)\n" +
		"\n" +
		"When output exceeds --max-tokens, lowest-priority sections are dropped\n" +
		"until the budget is met.\n" +
		"\n" +
		"Examples:\n" +
		"  vv inject                                   Inject context for auto-detected project\n" +
		"  vv inject --project myproject                Inject context for a specific project\n" +
		"  vv inject --format json                      Output as JSON\n" +
		"  vv inject --sections summary,sessions        Only summary and sessions\n" +
		"  vv inject --max-tokens 500                   Compact output\n",

	"export": "vv export \u2014 export session data for external analysis\n" +
		"\n" +
		"Usage: vv export [--format <json|csv>] [--project <name>]\n" +
		"\n" +
		"Flags:\n" +
		"  --format <json|csv>   Output format (default: json)\n" +
		"  --project <name>      Export only sessions for this project\n" +
		"\n" +
		"Serializes session index entries for external analysis tools. Outputs\n" +
		"all sessions (or a project subset) in JSON or CSV format.\n" +
		"\n" +
		"JSON outputs a flat array of session objects with key fields. CSV\n" +
		"outputs a header row followed by one row per session, with columns:\n" +
		"date, project, session_id, title, tag, model, branch, duration_minutes,\n" +
		"tokens_in, tokens_out, messages, tool_uses, friction_score, corrections,\n" +
		"estimated_cost_usd.\n" +
		"\n" +
		"Output is sorted by date, then session ID.\n" +
		"\n" +
		"Examples:\n" +
		"  vv export                              Export all sessions as JSON\n" +
		"  vv export --format csv                 Export as CSV\n" +
		"  vv export --project myproject          Export one project as JSON\n" +
		"  vv export --format csv > sessions.csv  Export to file\n",

	"effectiveness": "vv effectiveness \u2014 analyze whether context availability improves session outcomes\n" +
		"\n" +
		"Usage: vv effectiveness [--project <name>] [--format json]\n" +
		"\n" +
		"Flags:\n" +
		"  --project <name>   Show effectiveness for a specific project only\n" +
		"  --format <json>    Output as JSON instead of human-readable text\n" +
		"\n" +
		"Correlates context depth (number of prior sessions available) with\n" +
		"session outcomes (friction, corrections, duration). Groups sessions\n" +
		"into cohorts by context depth and computes Pearson correlation.\n" +
		"\n" +
		"Cohorts:\n" +
		"  none (0)       No prior sessions available\n" +
		"  early (1-10)   Building initial context\n" +
		"  building (11-30)  Growing context base\n" +
		"  mature (30+)   Rich context available\n" +
		"\n" +
		"Requires vv reprocess --backfill-context to have been run first to\n" +
		"populate ContextAvailable data on historical sessions.\n" +
		"\n" +
		"Examples:\n" +
		"  vv effectiveness                       Show all projects\n" +
		"  vv effectiveness --project myproject   Show one project\n" +
		"  vv effectiveness --format json         Output as JSON\n",

	"vault": "vv vault \u2014 vault git synchronization\n" +
		"\n" +
		"Usage: vv vault <command>\n" +
		"\n" +
		"Manages git synchronization of the vault repo across machines.\n" +
		"The vault repo is owned entirely by vv \u2014 all git operations are safe.\n" +
		"\n" +
		"Subcommands:\n" +
		"  vv vault status         Show vault git state (clean/dirty, ahead/behind)\n" +
		"  vv vault pull           Fetch + rebase with automatic conflict resolution\n" +
		"  vv vault sync-sessions  Mirror host-local staging into vault per-host subtree\n" +
		"  vv vault push           Commit all changes and push\n" +
		"  vv vault recover        List upstream commits whose content was dropped on rebase\n",

	"staging": "vv staging \u2014 host-local session-capture staging dir\n" +
		"\n" +
		"Usage: vv staging <command>\n" +
		"\n" +
		"Manages the host-local staging dir for the two-tier vault layout.\n" +
		"The staging dir lives outside the shared Obsidian vault \u2014 by default at\n" +
		"$XDG_STATE_HOME/vibe-vault/<project>/ (or ~/.local/state/vibe-vault/<project>/).\n" +
		"Hooks write session notes into the staging dir; a wrap-time mirror\n" +
		"projects them into <vault>/Projects/<p>/sessions/<host>/ for cross-host\n" +
		"browse.\n" +
		"\n" +
		"Subcommands:\n" +
		"  vv staging init <p>      Initialize the staging dir for a project (idempotent)\n" +
		"  vv staging status <p>    Report staging dir presence and worktree state\n" +
		"  vv staging path <p>      Print the resolved staging dir path\n" +
		"  vv staging gc <p>        Run git gc --auto on the staging repo\n" +
		"  vv staging migrate ...   Archive flat-layout sessions into _pre-staging-archive/\n",

	"zed": "vv zed \u2014 import Zed agent panel threads\n" +
		"\n" +
		"Usage: vv zed <subcommand>\n" +
		"\n" +
		"Import Zed agent panel threads from the local threads.db SQLite\n" +
		"database into the vault. Threads are parsed, converted, and processed\n" +
		"through the same capture pipeline as Claude Code sessions.\n" +
		"\n" +
		"Subcommands:\n" +
		"  vv zed backfill   Import threads from Zed threads database\n" +
		"  vv zed list       List threads in the database\n" +
		"  vv zed watch      Watch for changes and auto-capture\n",

	"mcp": "vv mcp \u2014 start MCP server for AI agent integration\n" +
		"\n" +
		"Usage: vv mcp\n" +
		"    vv mcp install [--claude-only | --zed-only]\n" +
		"    vv mcp uninstall [--claude-only | --zed-only]\n" +
		"\n" +
		"Starts a Model Context Protocol (MCP) server that exposes vibe-vault\n" +
		"tools over JSON-RPC 2.0 on stdin/stdout. This allows AI agents like\n" +
		"Claude Code and Zed to query project context programmatically.\n" +
		"\n" +
		"Subcommands:\n" +
		"  install     Register the MCP server in editor settings\n" +
		"  uninstall   Remove the MCP server from editor settings\n" +
		"\n" +
		"Available tools:\n" +
		"  vv_append_iteration     Append a narrative to the iteration log\n" +
		"  vv_bootstrap_context    Single-call session start (resume + workflow + tasks)\n" +
		"  vv_capture_session      Record a session note from an agent conversation\n" +
		"  vv_get_effectiveness    Context effectiveness analysis\n" +
		"  vv_get_friction_trends  Friction and efficiency trend data over time\n" +
		"  vv_get_knowledge        Project knowledge.md content\n" +
		"  vv_get_project_context  Condensed project context (sessions, threads,\n" +
		"                          decisions, friction trends)\n" +
		"  vv_get_resume           Current resume.md content\n" +
		"  vv_get_session_detail   Full markdown of a specific session note\n" +
		"  vv_get_task             Read a specific task file\n" +
		"  vv_get_workflow         Current workflow.md content\n" +
		"  vv_list_projects        All projects with session counts and date ranges\n" +
		"  vv_list_tasks           List active and completed task files\n" +
		"  vv_manage_task          Create, update, or complete task files\n" +
		"  vv_refresh_index        Rebuild the session index\n" +
		"  vv_search_sessions      Search/filter sessions by query, project, files,\n" +
		"                          date range, friction score\n" +
		"  vv_update_resume        Update resume.md sections\n" +
		"\n" +
		"Available prompts:\n" +
		"  vv_session_guidelines   Agent instructions for session capture\n" +
		"\n" +
		"Setup:\n" +
		"  vv mcp install        # installs into all detected editors\n" +
		"  (restart your editor)\n" +
		"\n" +
		"Verify \u2014 after restarting your editor, ask the agent to \"list\n" +
		"vibe-vault projects\" or \"capture this session\". The agent will\n" +
		"call the MCP tools automatically.\n" +
		"\n" +
		"The server logs tool calls to stderr for observability.\n",

	"config": "vv config \u2014 manage vibe-vault configuration\n" +
		"\n" +
		"Usage: vv config [set-key]\n" +
		"\n" +
		"Manages settings stored in ~/.config/vibe-vault/config.toml.\n" +
		"\n" +
		"Subcommands:\n" +
		"  set-key   Store a per-provider API key (anthropic, openai, google)\n" +
		"\n" +
		"The wrap render path (vv_render_wrap_text) and hook enrichment /\n" +
		"synthesis both resolve provider keys via a layered lookup: the value\n" +
		"in config.toml wins, falling back to the provider's environment\n" +
		"variable (ANTHROPIC_API_KEY / OPENAI_API_KEY / GOOGLE_API_KEY) for\n" +
		"operators who already have shell-env-based setup.\n",

	"templates": "vv templates \u2014 inspect, compare, and reset vault templates\n" +
		"\n" +
		"Usage: vv templates [list | diff | show | reset]\n" +
		"\n" +
		"Manages vault templates against their built-in defaults. Over time,\n" +
		"built-in templates evolve with new features and better prompts, but\n" +
		"vault copies drift. This command makes recalibration easy.\n" +
		"\n" +
		"Subcommands:\n" +
		"  vv templates list              Show all templates with status\n" +
		"  vv templates diff [--file X]   Unified diff of vault vs defaults\n" +
		"  vv templates show <name>       Print built-in default to stdout\n" +
		"  vv templates reset [--file X]  Reset templates to defaults\n",

	"version": "vv version \u2014 print version\n" +
		"\n" +
		"Usage: vv version\n",

	"memory": "vv memory \u2014 manage Claude Code auto-memory symlink into vault\n" +
		"\n" +
		"Usage: vv memory [link | unlink]\n" +
		"\n" +
		"Establishes (or removes) a symlink from Claude Code's per-project\n" +
		"auto-memory directory (~/.claude/projects/{slug}/memory/) into the\n" +
		"project's vault-resident agentctx/memory/ directory.\n" +
		"\n" +
		"Once linked, Claude Code's native memory writes land on vault disk\n" +
		"transparently \u2014 the vault git sync then carries them across machines,\n" +
		"eliminating the per-host drift that otherwise plagues auto-memory.\n" +
		"\n" +
		"Only projects that have already been marked vibe-vault-tracked\n" +
		"(i.e. Projects/{name}/agentctx/ exists in the vault) can be linked.\n" +
		"Run 'vv init' (or 'vv context init') first for new projects.\n" +
		"\n" +
		"Subcommands:\n" +
		"  vv memory link     Symlink host-local memory into the vault\n" +
		"  vv memory unlink   Reverse the symlink (vault copy preserved)\n",

	"worktree": "vv worktree \u2014 subagent worktree management\n" +
		"\n" +
		"Usage: vv worktree <command>\n" +
		"\n" +
		"Manages git worktrees created by AI subagents under\n" +
		".claude/worktrees/. Subagents lock their worktrees with a marker\n" +
		"(\"claude agent <id> (pid N)\") so a crash leaves them on disk; this\n" +
		"command cluster reaps the stale ones safely.\n" +
		"\n" +
		"Subcommands:\n" +
		"  vv worktree gc   Reap stale subagent worktrees with capture verification\n",
}

func TestFormatTerminal(t *testing.T) {
	for _, cmd := range Subcommands {
		t.Run(cmd.Name, func(t *testing.T) {
			expected, ok := expectedTerminal[cmd.Name]
			if !ok {
				t.Fatalf("no expected output for %q", cmd.Name)
			}
			got := FormatTerminal(cmd)
			if got != expected {
				t.Errorf("FormatTerminal(%q) mismatch.\n--- expected ---\n%s\n--- got ---\n%s\n--- diff ---\n%s",
					cmd.Name, quote(expected), quote(got), diff(expected, got))
			}
		})
	}
}

func TestFormatUsage(t *testing.T) {
	expected := fmt.Sprintf("vv v%s \u2014 vibe-vault session capture\n", Version) +
		"\n" +
		"Usage:\n" +
		"  vv init [path] [--git]           Create a new vault (default: ./vibe-vault)\n" +
		"  vv hook [install | ...]          Hook mode (reads stdin from Claude Code)\n" +
		"  vv context [init | ...]          Manage vault-resident AI context\n" +
		"  vv process <file.jsonl>          Process a single transcript file\n" +
		"  vv index                         Rebuild session index from notes\n" +
		"  vv backfill [path]               Discover and process historical transcripts\n" +
		"  vv archive                       Compress transcripts into vault archive\n" +
		"  vv reprocess [--project X]       Re-generate notes from transcripts\n" +
		"  vv check                         Validate config, vault, and hook setup\n" +
		"  vv stats [--project X]           Show session analytics and metrics\n" +
		"  vv friction [--project X]        Show friction analysis and correction patterns\n" +
		"  vv trends [--project X]          Show metric trends over time\n" +
		"  vv inject [--project X]          Output session-start context payload\n" +
		"  vv export [--format X]           Export session data (JSON or CSV)\n" +
		"  vv effectiveness [--project X]   Analyze context effectiveness on outcomes\n" +
		"  vv memory [link | ...]           Link Claude Code auto-memory into vault\n" +
		"  vv vault <command>               Vault git sync (pull, push, status, recover)\n" +
		"  vv staging [init | ...]          Manage host-local staging dir (init, status, gc, migrate)\n" +
		"  vv zed <subcommand>              Import Zed agent panel threads into vault\n" +
		"  vv mcp [install | ...]           Start MCP server (JSON-RPC over stdio)\n" +
		"  vv worktree [gc | ...]           Manage subagent worktrees (gc)\n" +
		"  vv config [set-key | ...]        Manage configuration (provider keys, etc.)\n" +
		"  vv templates [list | ...]        Inspect, compare, and reset vault templates\n" +
		"  vv version                       Print version\n" +
		"  vv help                          Show this help\n" +
		"\n" +
		"Hook integration (settings.json):\n" +
		"  {\"type\": \"command\", \"command\": \"vv hook\"}\n" +
		"\n" +
		"Configuration: ~/.config/vibe-vault/config.toml\n"

	got := FormatUsage(TopLevel, Subcommands)
	if got != expected {
		t.Errorf("FormatUsage mismatch.\n--- expected ---\n%s\n--- got ---\n%s\n--- diff ---\n%s",
			quote(expected), quote(got), diff(expected, got))
	}
}

// TestCmdIndex_GlobIsRecursive is the Phase 1.5 regression lock for the
// help description at internal/help/commands.go:135. The β2 layout puts
// session notes under `Projects/<p>/sessions/<host>/<date>/<file>.md` and
// `Projects/<p>/sessions/_pre-staging-archive/<file>.md`; the description
// must therefore advertise the recursive `**` glob, not the flat `*`
// form, so operators reading `vv help index` understand the actual
// walker semantics.
func TestCmdIndex_GlobIsRecursive(t *testing.T) {
	if !strings.Contains(CmdIndex.Description, "Projects/*/sessions/**/*.md") {
		t.Errorf("CmdIndex.Description should advertise recursive glob form Projects/*/sessions/**/*.md; got:\n%s",
			CmdIndex.Description)
	}
	if strings.Contains(CmdIndex.Description, "Projects/*/sessions/*.md\n") ||
		strings.Contains(CmdIndex.Description, "Projects/*/sessions/*.md ") {
		t.Errorf("CmdIndex.Description should NOT advertise the flat glob form (would mislead operators about per-host layout); got:\n%s",
			CmdIndex.Description)
	}
}

func TestRegistryCompleteness(t *testing.T) {
	expectedNames := []string{
		"init", "hook", "context", "process", "index",
		"backfill", "archive", "reprocess", "check", "stats", "friction", "trends", "inject", "export", "effectiveness", "memory", "vault", "staging", "zed", "mcp", "worktree", "config", "templates", "version",
	}
	if len(Subcommands) != len(expectedNames) {
		t.Fatalf("expected %d subcommands, got %d", len(expectedNames), len(Subcommands))
	}
	for i, name := range expectedNames {
		if Subcommands[i].Name != name {
			t.Errorf("Subcommands[%d].Name = %q, want %q", i, Subcommands[i].Name, name)
		}
		if Subcommands[i].Synopsis == "" {
			t.Errorf("Subcommands[%d] (%s) has empty Synopsis", i, name)
		}
		if Subcommands[i].Usage == "" {
			t.Errorf("Subcommands[%d] (%s) has empty Usage", i, name)
		}
		if Subcommands[i].Brief == "" {
			t.Errorf("Subcommands[%d] (%s) has empty Brief", i, name)
		}
	}
}

func TestManName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"", "vv"},
		{"init", "vv-init"},
		{"hook", "vv-hook"},
		{"hook install", "vv-hook-install"},
	}
	for _, tt := range tests {
		c := Command{Name: tt.name}
		if got := c.ManName(); got != tt.want {
			t.Errorf("Command{Name: %q}.ManName() = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestEscapeRoff(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`simple text`, `simple text`},
		{`back\slash`, `back\\slash`},
		{`.leading dot`, `\&.leading dot`},
		{"line1\n.line2", "line1\n\\&.line2"},
		{`--flag`, `\-\-flag`},
		{`a-b`, `a\-b`},
		{`no special`, `no special`},
		{`.vibe-vault/archive/`, `\&.vibe\-vault/archive/`},
	}
	for _, tt := range tests {
		got := escapeRoff(tt.input)
		if got != tt.want {
			t.Errorf("escapeRoff(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatRoffStructure(t *testing.T) {
	fixedDate := "2026-02-27"

	allCmds := append(Subcommands, HookSubcommands...)
	allCmds = append(allCmds, ContextSubcommands...)
	allCmds = append(allCmds, ZedSubcommands...)
	allCmds = append(allCmds, TemplatesSubcommands...)
	allCmds = append(allCmds, MemorySubcommands...)
	// Test each subcommand has required sections
	for _, cmd := range allCmds {
		t.Run(cmd.Name, func(t *testing.T) {
			out := FormatRoff(cmd, fixedDate)

			required := []string{".TH", ".SH NAME", ".SH SYNOPSIS"}
			for _, section := range required {
				if !strings.Contains(out, section) {
					t.Errorf("FormatRoff(%q) missing required section %q", cmd.Name, section)
				}
			}

			// Verify .TH has correct name
			expectedTH := strings.ToUpper(cmd.ManName())
			if !strings.Contains(out, ".TH "+expectedTH) {
				t.Errorf("FormatRoff(%q) .TH should contain %q", cmd.Name, expectedTH)
			}

			// Optional sections appear when data present
			if cmd.Description != "" && !strings.Contains(out, ".SH DESCRIPTION") {
				t.Errorf("FormatRoff(%q) has Description but missing .SH DESCRIPTION", cmd.Name)
			}
			if (len(cmd.Args) > 0 || len(cmd.Flags) > 0) && !strings.Contains(out, ".SH OPTIONS") {
				t.Errorf("FormatRoff(%q) has Args/Flags but missing .SH OPTIONS", cmd.Name)
			}
			if len(cmd.Examples) > 0 && !strings.Contains(out, ".SH EXAMPLES") {
				t.Errorf("FormatRoff(%q) has Examples but missing .SH EXAMPLES", cmd.Name)
			}
			if len(cmd.SeeAlso) > 0 && !strings.Contains(out, ".SH SEE ALSO") {
				t.Errorf("FormatRoff(%q) has SeeAlso but missing .SH SEE ALSO", cmd.Name)
			}
		})
	}
}

func TestFormatRoffTopLevelStructure(t *testing.T) {
	fixedDate := "2026-02-27"
	out := FormatRoffTopLevel(TopLevel, Subcommands, fixedDate)

	required := []string{
		".TH VV 1",
		".SH NAME",
		".SH SYNOPSIS",
		".SH DESCRIPTION",
		".SH COMMANDS",
		".SH CONFIGURATION",
		".SH SEE ALSO",
	}
	for _, section := range required {
		if !strings.Contains(out, section) {
			t.Errorf("FormatRoffTopLevel missing section %q", section)
		}
	}

	// All subcommands should be listed (check escaped form)
	for _, cmd := range Subcommands {
		escaped := escapeRoff(cmd.Brief)
		if !strings.Contains(out, escaped) {
			t.Errorf("FormatRoffTopLevel missing subcommand brief %q (escaped: %q)", cmd.Brief, escaped)
		}
	}
}

func TestFormatRoffEscapesDescription(t *testing.T) {
	fixedDate := "2026-02-27"
	// archive description starts with ".vibe-vault" which needs escaping
	out := FormatRoff(CmdArchive, fixedDate)
	if strings.Contains(out, "\n.vibe-vault") {
		t.Error("FormatRoff(archive) did not escape leading dot in .vibe-vault")
	}
}

// quote shows a string with escape sequences visible.
func quote(s string) string {
	return fmt.Sprintf("%q", s)
}

func TestFormatTerminal_HookSubcommands(t *testing.T) {
	for _, cmd := range HookSubcommands {
		t.Run(cmd.Name, func(t *testing.T) {
			out := FormatTerminal(cmd)
			// Verify header format
			prefix := fmt.Sprintf("vv %s \u2014 %s\n", cmd.Name, cmd.Synopsis)
			if !strings.HasPrefix(out, prefix) {
				t.Errorf("FormatTerminal(%q) header mismatch.\nwant prefix: %q\ngot:         %q", cmd.Name, prefix, out[:min(len(out), len(prefix)+20)])
			}
			// Verify usage line present
			if !strings.Contains(out, "Usage: "+cmd.Usage) {
				t.Errorf("FormatTerminal(%q) missing usage line", cmd.Name)
			}
			// Verify description present
			if cmd.Description != "" && !strings.Contains(out, cmd.Description) {
				t.Errorf("FormatTerminal(%q) missing description", cmd.Name)
			}
		})
	}
}

func TestFormatTerminal_McpSubcommands(t *testing.T) {
	for _, cmd := range McpSubcommands {
		t.Run(cmd.Name, func(t *testing.T) {
			out := FormatTerminal(cmd)
			// Verify header format
			prefix := fmt.Sprintf("vv %s \u2014 %s\n", cmd.Name, cmd.Synopsis)
			if !strings.HasPrefix(out, prefix) {
				t.Errorf("FormatTerminal(%q) header mismatch.\nwant prefix: %q\ngot:         %q", cmd.Name, prefix, out[:min(len(out), len(prefix)+20)])
			}
			// Verify usage line present
			if !strings.Contains(out, "Usage: "+cmd.Usage) {
				t.Errorf("FormatTerminal(%q) missing usage line", cmd.Name)
			}
			// Verify description present
			if cmd.Description != "" && !strings.Contains(out, cmd.Description) {
				t.Errorf("FormatTerminal(%q) missing description", cmd.Name)
			}
		})
	}
}

func TestFormatTerminal_MemorySubcommands(t *testing.T) {
	for _, cmd := range MemorySubcommands {
		t.Run(cmd.Name, func(t *testing.T) {
			out := FormatTerminal(cmd)
			prefix := fmt.Sprintf("vv %s — %s\n", cmd.Name, cmd.Synopsis)
			if !strings.HasPrefix(out, prefix) {
				t.Errorf("FormatTerminal(%q) header mismatch.\nwant prefix: %q\ngot:         %q", cmd.Name, prefix, out[:min(len(out), len(prefix)+20)])
			}
			if !strings.Contains(out, "Usage: "+cmd.Usage) {
				t.Errorf("FormatTerminal(%q) missing usage line", cmd.Name)
			}
			if cmd.Description != "" && !strings.Contains(out, cmd.Description) {
				t.Errorf("FormatTerminal(%q) missing description", cmd.Name)
			}
		})
	}
}

func TestFormatTerminal_ContextSubcommands(t *testing.T) {
	for _, cmd := range ContextSubcommands {
		t.Run(cmd.Name, func(t *testing.T) {
			out := FormatTerminal(cmd)
			// Verify header format
			prefix := fmt.Sprintf("vv %s \u2014 %s\n", cmd.Name, cmd.Synopsis)
			if !strings.HasPrefix(out, prefix) {
				t.Errorf("FormatTerminal(%q) header mismatch.\nwant prefix: %q\ngot:         %q", cmd.Name, prefix, out[:min(len(out), len(prefix)+20)])
			}
			// Verify usage line present
			if !strings.Contains(out, "Usage: "+cmd.Usage) {
				t.Errorf("FormatTerminal(%q) missing usage line", cmd.Name)
			}
			// Verify description present
			if cmd.Description != "" && !strings.Contains(out, cmd.Description) {
				t.Errorf("FormatTerminal(%q) missing description", cmd.Name)
			}
		})
	}
}

// diff shows a line-by-line comparison highlighting the first difference.
func diff(expected, got string) string {
	el := strings.Split(expected, "\n")
	gl := strings.Split(got, "\n")
	max := len(el)
	if len(gl) > max {
		max = len(gl)
	}
	var b strings.Builder
	for i := 0; i < max; i++ {
		var e, g string
		if i < len(el) {
			e = el[i]
		}
		if i < len(gl) {
			g = gl[i]
		}
		marker := "  "
		if e != g {
			marker = "! "
		}
		if e != g {
			fmt.Fprintf(&b, "%sline %d:\n  exp: %q\n  got: %q\n", marker, i+1, e, g)
		}
	}
	return b.String()
}
