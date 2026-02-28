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
		"system. Handles two event types:\n" +
		"\n" +
		"  SessionEnd \u2014 parses transcript, writes a finalized session note\n" +
		"  Stop       \u2014 captures a mid-session checkpoint (no LLM enrichment)\n" +
		"\n" +
		"Checkpoint notes are provisional: a subsequent Stop overwrites the\n" +
		"previous checkpoint, and SessionEnd finalizes it. Clear events and\n" +
		"unknown events are silently ignored.\n" +
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
		"Walks Projects/*/sessions/*.md in the vault, parses frontmatter from each\n" +
		"note, and rebuilds .vibe-vault/session-index.json. Preserves TranscriptPath\n" +
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
		"  --project <name>   Only reprocess sessions for this project\n" +
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
		"Usage: vv stats [--project <name>]\n" +
		"\n" +
		"Flags:\n" +
		"  --project <name>   Show stats for a specific project only\n" +
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
		"Usage: vv friction [--project <name>]\n" +
		"\n" +
		"Flags:\n" +
		"  --project <name>   Show friction for a specific project only\n" +
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

	"version": "vv version \u2014 print version\n" +
		"\n" +
		"Usage: vv version\n",
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
		"  vv init [path] [--git]       Create a new vault (default: ./vibe-vault)\n" +
		"  vv hook [install | ...]      Hook mode (reads stdin from Claude Code)\n" +
		"  vv process <file.jsonl>      Process a single transcript file\n" +
		"  vv index                     Rebuild session index from notes\n" +
		"  vv backfill [path]           Discover and process historical transcripts\n" +
		"  vv archive                   Compress transcripts into vault archive\n" +
		"  vv reprocess [--project X]   Re-generate notes from transcripts\n" +
		"  vv check                     Validate config, vault, and hook setup\n" +
		"  vv stats [--project X]       Show session analytics and metrics\n" +
		"  vv friction [--project X]    Show friction analysis and correction patterns\n" +
		"  vv version                   Print version\n" +
		"  vv help                      Show this help\n" +
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

func TestRegistryCompleteness(t *testing.T) {
	expectedNames := []string{
		"init", "hook", "process", "index",
		"backfill", "archive", "reprocess", "check", "stats", "friction", "version",
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
