# Architecture

Module responsibilities, data flows, and system architecture.

## Data Flow

```
Claude Code SessionEnd / Stop / PreCompact
        │
        ▼
   ┌─────────┐    stdin JSON: {session_id, transcript_path, cwd, ...}
   │ vv hook  │
   └────┬─────┘
        │
        ▼
   ┌──────────────────┐
   │  hook/handler.go  │    Parse stdin, dispatch by event type
   └────────┬─────────┘
            │
            ▼
   ┌───────────────────┐
   │ session/capture.go │    Orchestrator
   └─┬───┬───┬───┬───┬──┘
     │   │   │   │   │
     ▼   │   │   │   │
transcript/  │   │   │   │    Parse JSONL, compute stats, extract text
parser.go    │   │   │   │
     ▼       │   │   │   │
session/     │   │   │   │    Map CWD → project name, domain, branch
detect.go    │   │   │   │
             ▼   │   │   │
        index/   │   │   │    Load session-index.json, check dedup, get iteration
        index.go │   │   │
                 ▼   │   │
        narrative/   │   │    Heuristic extraction: activities, title, summary,
        extract.go   │   │    tag, decisions, open threads, Work Performed markdown
                     │   │
        prose/       │   │    Dialogue extraction: user/assistant turns, tool markers
        prose.go     │   │    (filler filtering, segment boundaries)
                     ▼   │
        enrichment/      │    LLM call: refines summary, decisions, threads, tag
        client.go        │    (skipped when prose extraction succeeds)
                         │
        friction/        │    Correction detection, composite friction scoring (0-100)
        analyze.go       │    (from dialogue + narrative + token efficiency + threads)
                         ▼
            meta/             Provenance stamping: meta.Stamp() fills
            provenance.go     NoteData.Host + NoteData.User immediately after
                              NoteDataFromTranscript returns. Host resolves via
                              $VIBE_VAULT_HOSTNAME → os.Hostname(); User resolves
                              via $USER → $LOGNAME → user.Current(). Single
                              convergence point covers all three capture paths
                              (MCP vv_capture_session, hook Stop/SessionEnd,
                              zed-reprocess). cwd is intentionally NOT stamped
                              yet — deferred pending MCP-vs-hook cwd semantics.
                         ▼
            index/            Score related sessions (shared files, threads, branch, tag)
            related.go
                         ▼
            render/           Build NoteData, render frontmatter + markdown body
            markdown.go       (host/user emitted in YAML before summary;
                              includes Work Performed + related sessions)
                         ▼
                    os.WriteFile    Projects/{project}/sessions/YYYY-MM-DD-NN.md
                    index.Save()   .vibe-vault/session-index.json
                         │
                         ▼  (SessionEnd only, not Stop checkpoints)
            synthesis/        Gather session note + git diff + knowledge + resume
            gather.go         + recent history + active tasks
                         │
                         ▼
            synthesis/        LLM call: identify learnings, stale entries,
            synthesize.go     resume updates, task completions
                         │
                         ▼
            synthesis/        Apply: append learnings to knowledge.md,
            actions.go        flag stale entries, update resume, retire tasks
                         │
                         ▼
            index/            Load index
            generate.go       GenerateContext() → history.md
```

### Zed Integration Flow

```
~/.local/share/zed/threads/threads.db
        │
        ▼
   ┌───────────┐
   │ zed/parser │    ParseDB() — SQLite read-only + zstd decompress
   └─────┬─────┘    + Rust-style enum JSON unmarshal
         │
         ▼
   Thread structs (ZedMessage with User/Agent envelopes,
                   ZedContent with Text/Thinking/ToolUse/Mention blocks,
                   tool_results on Agent messages)
         │
    ┌────┼────┬──────────┐
    ▼    ▼    ▼          ▼
convert  detect  narrative  prose
    │    │    │          │
    ▼    ▼    ▼          ▼
Transcript  Info  Narrative  Dialogue
    │
    └──── CaptureFromParsed → render → index
```

Three capture paths:

- **MCP capture (explicit):** Agent calls `vv_capture_session` →
  `session.CaptureFromParsed()` with agent-curated summary
- **SQLite backfill:** `vv zed backfill` → `zed.ParseDB()` → convert → capture
  (batch processing of historical threads)
- **Auto-capture:** MCP server background watcher (`zed.Watcher`) monitors
  `threads.db-wal` via fsnotify → debounce → auto-capture callback
  (`status: auto-captured`). Explicit captures take precedence.

Additional commands: `vv zed list` shows parsed threads, `vv zed watch` runs
the standalone SQLite watcher for auto-capturing outside the MCP server.

### Index Rebuild Flow (`vv index`)

```
vv index
    │
    ▼
index/rebuild.go     Walk Projects/*/sessions/*.md, skip non-session files
    │
    ▼
noteparse/           Parse frontmatter + body sections from each note
noteparse.go
    │
    ▼
index/index.go       Build enriched SessionEntry for each note
    │
    ▼
index.Save()         Write .vibe-vault/session-index.json
    │
    ▼
index/generate.go    GenerateContext(): write history.md per project
    ▼
Done
```

### Backfill / Archive / Reprocess Flows

```
vv backfill [path]           vv archive              vv reprocess [--project X]
    │                            │                       │
    ▼                            ▼                       ▼
discover.Discover()          Load index              Load index
    │                            │                       │
    ▼                            ▼                       ▼
For each transcript:         For each entry:          For each entry:
  idx.Has()? patch TP+skip     IsArchived? skip         Find transcript:
  session.Capture()            archive.Archive()          1. TranscriptPath
  print progress               (zstd compress)             2. archive → Decompress
    │                            │                          3. discover.FindBySessionID
    ▼                            ▼                       │
Print summary               Print summary             session.Capture(Force:true)
                            (src MB → arch MB)            │
                                                       GenerateContext()
```

### MCP Server Flow (`vv mcp`)

```
Claude Code / AI agent
        │
        ▼  (JSON-RPC 2.0 over stdio)
   ┌──────────┐
   │ vv mcp   │    bufio.Scanner line-delimited JSON
   └────┬─────┘
        │
        ▼
   mcp/server.go    Dispatch: initialize, tools/list, tools/call,
        │                       prompts/list, prompts/get
        │
        ├─── vv_get_project_context  → index.Load() → trends.Compute()
        │                            → inject.Build() → inject.Render()
        ├─── vv_list_projects        → index.Load() → idx.Projects()
        │                            → trends.Compute() per project
        ├─── vv_search_sessions      → index.Load() → filter/search
        ├─── vv_get_knowledge        → read Projects/{project}/knowledge.md
        ├─── vv_get_session_detail   → read session note markdown
        ├─── vv_get_friction_trends  → trends.Compute() → format
        ├─── vv_get_effectiveness    → effectiveness analysis
        ├─── vv_capture_session      → session.CaptureFromParsed()
        │                            → (stamps Host/User via meta.Stamp(),
        │                               see Data Flow diagram above)
        ├─── vv_append_iteration     → assemble iteration block
        │                            → append provenanceTrailer(meta.Stamp())
        │                               HTML-comment trailer into the block
        │                               (tools_context_write.go:~254);
        │                               vv_get_iterations strips it via
        │                               parseIterations before returning
        │                               narrative to callers.
        │
        └─── prompt: vv_session_guidelines → agent instructions for capture
```

### Context Sync Flow (`vv context`)

```
vv context init                    vv context sync
    │                                  │
    ▼                                  ▼
context.Init()                     context.Sync()
    │                                  │
    ▼                                  ▼
Scaffold agentctx/                 Refresh vault templates from embeds
from templates.                    Run migrations (schema 0→10)
    │                                  │
    ▼                                  ▼
Create repo files                  Three-way baseline propagation
(CLAUDE.md, .claude/)              (template vs baseline vs project)
                                   + v8→v9: snippets/ propagation
                                     and marker-block injection into
                                     agentctx/resume.md (.datablock.baseline)
```

### Template Cascade (three-tier, baseline-tracked)

```
Tier 1: Embedded binary            Tier 2: Vault Templates/       Tier 3: Project agentctx/
(templates/agentctx/**)            (Templates/agentctx/**)        (Projects/<proj>/agentctx/**)
         │                                  │                              │
         │ forceUpdateVaultTemplates()     │ propagateSharedSubdir()     │ runtime reads
         │ always overwrites on            │ three-way baseline compare  │ by AI agents
         │ every sync                      │ auto-update untouched       │
         ▼                                  ▼                              ▼
   Embeds are source of          Always matches embeds           .baseline = last sync state
   truth. Vault templates        after sync. Changes here        .pinned = exempt from updates
   are a propagation cache.      auto-propagate to untouched     CONFLICT = both changed
                                 project files.                  --force = override conflicts
```

Source of truth: Tier 1 (Go embeds). See DESIGN.md decisions #41 and #46.

## Module Responsibilities

| Package | File | Responsibility |
|---------|------|----------------|
| `cmd/vv` | `main.go` | CLI arg parsing, subcommand routing (including hook sub-subcommands), help via `internal/help`, `wantsHelp()` flag guard, unknown flag rejection, `runTrends()` with `--project` and `--weeks` flags, `runInject()` with `--project`/`--format`/`--sections`/`--max-tokens` flags, `runExport()` with `--format`/`--project` flags, `runContext()` with `sync` sub-subcommand (`--project`/`--all`/`--dry-run`/`--force`), `runCheck()` agentctx schema check |
| `cmd/gen-man` | `main.go` | Generates `man/*.1` files from help registry (Subcommands + HookSubcommands + ContextSubcommands) |
| `templates` | `embed.go` | `//go:embed all:agentctx` — embeds 24 agentctx template files (commands, skills, snippets, settings) into the binary; `AgentctxFS()` returns the `embed.FS`. Templates use `{{PROJECT}}`/`{{DATE}}` placeholders resolved at runtime. These are Tier 1 of the three-tier template cascade (see DESIGN.md #46). The `snippets/` subdirectory holds marker-delimited blocks injected into user-owned files by schema migrations (e.g., v8→v9's `resume-data-workflow.md`). |
| `context` | `context.go` | `Init()` — scaffold vault-resident context (templates from embed.FS, repo-side CLAUDE.md symlink + .claude/{commands,rules,skills,agents} symlinks, agentctx symlink, .version); `Migrate()` — copy local files to vault + force-update repo-side; `claudeSubdirs` var defines .claude/ subdirectories; helpers: safeWrite, safeSymlink, gitignoreEnsure, copyFile/Dir |
| `context` | `schema.go` | `VersionFile` TOML struct, `ReadVersion`/`WriteVersion`, `LatestSchemaVersion` const (10), `Migration` type + registry (0→1 through 9→10; 9→10 is a no-op contract-marker bump), `MigrationContext` (incl. `DryRun` field), `migrationsFrom()`, no-op `migrate9to10()` |
| `context` | `invariants.go` | v10 Current-State contract primitives: `CurrentStateSection` const, `IsInvariantBullet(line)` line-level classifier (18-entry first-word whitelist + 200-rune trailing cap; regex tolerates leading dash/indentation and widened key class for acronyms like `MCP`/`CLI`), `ValidateCurrentStateBody(body)` document-level scanner (skips blanks, markdown headings, and single-/multi-line HTML-comment regions via `inComment` state flag; every other line must satisfy `IsInvariantBullet`). Consumed by the synthesis agent's Features-routing prompt and the `vv_update_resume` v10 guard. |
| `context` | `sync.go` | `Sync()` — run schema migrations + three-way baseline propagation for one or all projects; `SyncOpts`/`SyncResult`/`ProjectSyncResult` types; `propagateSharedSubdir()` with `.baseline` tracking (template vs baseline vs project three-way comparison); `propagateDir()` with `dirContentsChanged()` gate; `isSidecar()`/`writeBaseline()`/`readBaseline()`/`cleanPending()` helpers; `forceUpdateVaultTemplates()`; migrations `1→2` through `7→8` (level-set with baselines) |
| `context` | `blocksync.go` | `migrate8to9()` — data-workflow block injection migration; `injectDataWorkflowBlock()`, `insertAfterFirstH2()`, `readSnippetBody()`, `writeResume()` helpers. Reads Tier-3 `snippets/resume-data-workflow.md` and injects a marker-delimited span into `agentctx/resume.md` with span-only `.datablock.baseline` tracking for conflict detection. Opt-outs: `snippets/resume-data-workflow.md.pinned` (freeze snippet body) and `resume.md.no-data-workflow` (skip injection). |
| `context` | `template.go` | `TemplateVars`, `DefaultVars()`, `resolveTemplate()` (vault Templates/agentctx/ first, fallback to `templates.AgentctxFS()`), `readEmbedded()`, `applyVars()` ({{PROJECT}}/{{DATE}}), `BuiltinTemplates()` (walks embed.FS), `EnsureVaultTemplates()` (seed-once for Init), `forceUpdateVaultTemplates()` (always-overwrite for Sync — Tier 1→2 refresh) |
| `friction` | `types.go` | `Correction`, `Signals`, `Result`, `ProjectFriction` types |
| `friction` | `detect.go` | `DetectCorrections()` — linguistic (negation, redirect, undo, quality, repetition) + contextual (short negation after long assistant turn) correction detection |
| `friction` | `score.go` | `Score()` — weighted composite friction score (0-100): correction density (30), token efficiency (25), file retry (20), error cycles (15), recurring threads (10) |
| `friction` | `analyze.go` | `Analyze()` — pure-function orchestrator: corrections + narrative signals + token efficiency + thread recurrence → `Result` with score + human-readable signals |
| `friction` | `format.go` | `ComputeProjectFriction()` — aggregate per-project friction from index; `Format()` — aligned terminal output for `vv friction` |
| `mcp` | `protocol.go` | JSON-RPC 2.0 and MCP message types (Request, Response, InitializeResult, ToolDef, ToolsCallResult, ContentBlock, PromptDef, PromptArg, PromptMessage) |
| `mcp` | `server.go` | Stdio transport: `Server.Serve()` reads newline-delimited JSON, dispatches initialize/tools/list/tools/call/prompts/list/prompts/get, logs tool calls to stderr |
| `mcp` | `tools.go` | 8 tools (all `vv_`-prefixed): `vv_get_project_context`, `vv_list_projects`, `vv_search_sessions`, `vv_get_knowledge`, `vv_get_session_detail`, `vv_get_friction_trends`, `vv_get_effectiveness`, `vv_capture_session` |
| `mcp` | `prompts.go` | `NewSessionGuidelinesPrompt()` — agent instructions for when/how to call `vv_capture_session` |
| `help` | `commands.go` | Command/Flag/Arg structs, Version var (build-time injection via ldflags), registry of 17 subcommands + 2 hook + 3 context + 3 vault subcommands (status, pull, push), ManName() with space→hyphen |
| `help` | `terminal.go` | `FormatTerminal()` and `FormatUsage()` — terminal help output |
| `help` | `roff.go` | `FormatRoff()` and `FormatRoffTopLevel()` — roff-formatted man pages |
| `check` | `check.go` | 10 diagnostic checks (config, vault, obsidian, projects, state, index, domains, enrichment, hook, agentctx schema), `Run()` aggregator, `Report.Format()`, `CheckAgentctxSchema()` (pass/warn by version) |
| `archive` | `archive.go` | Zstd compress/decompress via klauspost/compress, IsArchived, ArchivePath |
| `config` | `config.go` | TOML config with XDG paths, `~` expansion, defaults, `SessionTag()`/`SessionTags()` for configurable session tags, `Overlay()` for per-project config, `WithProjectOverlay()` loads `Projects/{project}/agentctx/config.toml` |
| `config` | `write.go` | Write/update config.toml with action status, ConfigDir(), CompressHome(), updateVaultPath(), `ProjectConfigTemplate()` for per-project overlay scaffolds |
| `discover` | `discover.go` | Walk directories for UUID-named `.jsonl` transcripts, subagent detection, FindBySessionID |
| `hook` | `handler.go` | Stdin JSON parsing (2s timeout), `handleInput()` dispatch logic (extracted for testability), dispatches SessionEnd/Stop/PreCompact, auto-refresh context on SessionEnd via `GenerateContext()` (no knowledge injection) |
| `hook` | `setup.go` | `Install()`/`Uninstall()` for `~/.claude/settings.json`: 3 events (SessionEnd, Stop, PreCompact), idempotent JSON manipulation, backup, directory creation; `InstallMCPZed()`/`UninstallMCPZed()` for `~/.config/zed/settings.json` (Zed `context_servers` format) |
| `inject` | `inject.go` | `Build()` — assemble context from index entries and trends; `FormatMarkdown()`/`FormatJSON()` renderers; `Render()` — format + token-budget truncation loop (drops lowest-priority sections); `estimateTokens()` — word count × 1.3 |
| `scaffold` | `scaffold.go` | `go:embed` vault scaffold templates (for `vv init`), `Init()` scaffolder with `{{VAULT_NAME}}` replacement. Distinct from `templates/` which holds agentctx templates for `vv context init` |
| `transcript` | `parser.go` | Streaming JSONL parser, skips non-conversation types |
| `transcript` | `types.go` | All data types: Entry (incl. native `IsMeta`, `PlanContent` fields), Message, ContentBlock, Usage, Stats |
| `transcript` | `stats.go` | Stats aggregation, file tracking, user/assistant text, title heuristics |
| `enrichment` | `types.go` | `Result` (exported), API request/response types |
| `enrichment` | `prompt.go` | `PromptInput` (incl. narrative context fields), system prompt, user prompt builder, text truncation, heuristic analysis section |
| `enrichment` | `client.go` | `Generate()` — HTTP POST to OpenAI-compatible endpoint, response parsing, tag validation |
| `narrative` | `types.go` | `Activity`, `Segment`, `Narrative`, `Commit` structs; 12 `ActivityKind` constants (FileCreate, FileModify, TestRun, GitCommit, GitPush, Build, Command, Decision, PlanMode, Delegation, Explore, Error) |
| `narrative` | `segment.go` | `SegmentEntries()` — split at `compact_boundary`, boundary entries excluded |
| `narrative` | `extract.go` | `Extract()` entry point, `classifyToolUse()`, `ClassifyBashCommand()`, `IsNoiseMessage()`, `BuildToolResultMap()`, `ToolResult`, `ExtractCommits()`, `parseCommitResult()` (all exported) |
| `prose` | `prose.go` | `Extract()` — dialogue extraction from transcript text blocks: Turn/Marker/Element/Section/Dialogue types, filler filter (120 chars), user cap (500 chars) |
| `prose` | `render.go` | `Render()` — markdown output: blockquote user turns, plain assistant text, italic markers, segment headers |
| `narrative` | `infer.go` | `inferTitle()`, `inferSummary()` (intent-driven with conventional commit prefixes), `inferIntentPrefix()`, `inferSubject()`, `formatOutcomes()`, `inferTag()`, `inferOpenThreads()`, `extractDecisions()` |
| `narrative` | `render.go` | `RenderWorkPerformed()` — single/multi-segment markdown, long-session filtering (>50 activities) |
| `stats` | `stats.go` | `Compute()` — aggregate metrics from index entries with optional project filter, returns `Summary` with totals, averages, and sorted breakdowns (projects, models, tags, files, monthly) |
| `stats` | `format.go` | `Format()` — aligned terminal output with overview, averages, projects, models, tags, monthly trend, top files; token/duration/int formatting helpers |
| `stats` | `export.go` | `ExportEntries()` — filter, sort, and convert `SessionEntry` map to `[]ExportEntry`; `ExportJSON()` and `ExportCSV()` serializers |
| `trends` | `trends.go` | `Compute()` — weekly bucketing by ISO week, 4-week rolling averages, anomaly detection (1.5σ), direction analysis (improving/worsening/stable), `--project` filter, `--weeks` display limit |
| `trends` | `format.go` | `Format()` — aligned terminal output: overview (direction arrows), per-metric week tables with rolling avg, anomaly markers (spike/dip), anomalies summary; token/duration/int formatting helpers |
| `session` | `capture.go` | Orchestration via `CaptureOpts`: parse → detect → **project config overlay** → index → **narrative** → **prose** → **commits** → enrich (skipped when prose succeeds) → **friction** → relate → render → write. Force mode reuses existing iteration to overwrite in place |
| `session` | `detect.go` | Git remote origin + CWD-based project name, config-based domain detection |
| `index` | `index.go` | Enriched SessionEntry + TranscriptPath + Commits + Friction + token/message counts, JSON index: dedup, iteration counting, cross-linking |
| `index` | `rebuild.go` | `Rebuild()` — walk Projects/*/sessions/, parse via noteparse, preserve TranscriptPaths from old index, backfill token/message counts |
| `index` | `related.go` | `RelatedSessions()` — multi-signal scoring (files, threads, branch, tag) |
| `index` | `context.go` | `ProjectContext()` — per-project history.md (timeline with friction indicators, decisions, threads, friction patterns, key files) |
| `index` | `generate.go` | `GenerateContext()` — shared function writing per-project `history.md` + seeding per-project `knowledge.md`; `GenerateResult` type with metrics; used by `runIndex()`, `runReprocess()`, and `handleSessionEnd()` |
| `noteparse` | `noteparse.go` | Line-based frontmatter parser + body section extraction (decisions, threads, files, commits) |
| `render` | `markdown.go` | Obsidian note rendering: frontmatter (incl. commits, friction_score, corrections), Session Dialogue / What Happened (conditional), Commits, Friction Signals, Work Performed, tool usage table, wikilinks, related sessions |
| `zed` | `types.go` | Zed agent panel JSON schema types with custom unmarshaling for Rust-style enum format (Thread, ZedMessage, ZedContent, MentionURI, ZedToolResult, TokenUsage, ZedModel, ProjectSnapshot, WorktreeSnapshot) |
| `zed` | `parser.go` | `ParseDB()` — SQLite reader via `modernc.org/sqlite` (read-only), zstd decompression, Rust-style enum message parsing; `ParseThread()` — single thread decompression + unmarshal |
| `zed` | `convert.go` | `Convert()` — Thread → `transcript.Transcript` with 28-entry tool name normalization, per-request token aggregation, mention→text conversion |
| `zed` | `detect.go` | `DetectProject()` — builds `session.Info` from thread metadata without git subprocess (worktree path basename, snapshot branch, config-based domain) |
| `zed` | `narrative.go` | `ExtractNarrative()` — single-segment Narrative from Zed tools, commit extraction from terminal results, tag inference |
| `zed` | `prose.go` | `ExtractDialogue()` — Dialogue from Zed messages, mention inlining, filler filter, error markers from tool_results |
| `zed` | `watcher.go` | `Watcher` — fsnotify on `threads.db-wal`, debounce, auto-capture callback |
| `zed` | `batch.go` | Batch capture helpers for backfill |
| `effectiveness` | `effectiveness.go` | Context depth vs session outcome correlation (cohort analysis, Pearson correlation) |
| `identity` | `identity.go` | `.vibe-vault.toml` parser — explicit project name/domain/tags override |
| `llm` | `provider.go`, `types.go`, `retry.go`, `openai.go`, `anthropic.go`, `google.go` | Multi-provider LLM abstraction: `Provider` interface, OpenAI-compatible/Anthropic/Gemini implementations, retry with backoff |
| `templates` (internal) | `templates.go`, `diff.go`, `reset.go` | Template registry, vault-vs-embedded comparison, `vv templates` status reporting |
| `vaultsync` | `vaultsync.go` | `Classify()` — file classification (Regenerable/AppendOnly/Manual/ConfigFile) for conflict resolution; `GetStatus()` — vault git state (branch, clean/dirty, ahead/behind); `Pull()` — fetch + rebase with auto-stash and classification-driven conflict resolution; `CommitAndPush()` — stage all, commit with hostname stamp, push with one pull-retry; `EnsureRemote()` — verify origin exists |
| `synthesis` | `types.go` | Data structures: `Input`, `Result`, `Learning`, `StaleEntry`, `ResumeUpdate`, `TaskUpdate`, `ActionReport` |
| `synthesis` | `gather.go` | `GatherInput()` — collect session note, git diff (8KB cap), knowledge.md, resume.md, recent history (last 5 sessions), active tasks into `Input` struct |
| `synthesis` | `prompt.go` | System and user prompt construction for LLM synthesis call; bullet numbering for LLM reference; structured JSON output schema |
| `synthesis` | `synthesize.go` | `Synthesize()` — LLM invocation (temp 0.3, JSON mode) + response validation/filtering (section names, file targets, index bounds, action types) |
| `synthesis` | `actions.go` | `Apply()` — execute synthesis result: append learnings to knowledge.md (with significant-word duplicate detection), flag stale entries (index + fuzzy fallback), update resume sections, move completed tasks to `done/` |
| `synthesis` | `run.go` | `Run()` — top-level orchestrator: gather → synthesize → apply; short-circuits on nil provider, disabled config, or empty result |
| `mdutil` | `mdutil.go` | Shared markdown/text utilities: `SignificantWords()` (4+ char, stop-word filtered), `Overlap()`/`SetIntersection()` (word set operations), `ReplaceSectionBody()` (heading-targeted markdown editing), `AtomicWriteFile()` (temp + rename crash safety) |
| `sanitize` | `redact.go` | Regex-based XML tag stripping for Claude Code wrapper tags |
| `memory` | `memory.go` | `Link()`/`Unlink()` for `vv memory` — slug derivation (symlink-resolved + `/` → `-`), project resolution via `session.DetectProject`, migrate pre-existing host-local memory into the vault target (drop identical, move unique, quarantine conflicts to sibling `memory-conflicts/{timestamp}/` under `--force`), establish/remove the `~/.claude/projects/{slug}/memory` ↔ `Projects/{name}/agentctx/memory` symlink. Host-local writes go through to the vault by POSIX symlink semantics; see DESIGN.md #48 |

## Template System

Two separate template systems serve different purposes:

**Scaffold templates** (`internal/scaffold/templates/`) — used by `vv init` to
create new Obsidian vaults. Embedded via `//go:embed` in `scaffold.go`. Contains
vault structure (dashboards, `.obsidian/` config, scripts).

**Agentctx templates** (`templates/agentctx/`) — used by `vv context init` to
scaffold per-project AI context. Embedded via `//go:embed` in `templates/embed.go`.
Contains 11 `.md` files: CLAUDE.md, workflow.md, resume.md, iterations.md,
README.md, and commands/{restart,wrap,license,makefile,review-plan,cancel-plan}.md.

Template resolution (`resolveTemplate()` in `template.go`):
1. Check vault `Templates/agentctx/{path}` — allows per-vault customization
2. Fall back to embedded default from `templates.AgentctxFS()`
3. Apply `{{PROJECT}}` and `{{DATE}}` variable substitution

The `vv templates` subcommand compares vault templates against embedded defaults,
showing which are customized, missing, or at default.

### Repo-side File Architecture

`vv context init` creates regular files deployed from the vault (no symlinks
since schema v5):

```
repo/
├── CLAUDE.md              Regular file (MCP-first instructions)
├── commit.msg             Regular file (working commit message)
├── .vibe-vault.toml       Project identity (committed to repo)
└── .claude/
    ├── commands/          Regular directory (deployed from vault agentctx/)
    ├── rules/             Regular directory
    ├── skills/            Regular directory
    └── agents/            Regular directory
```

`.claude/` subdirectories contain regular file copies of vault agentctx content,
deployed by `deploySubdirToRepo()` during `vv context sync`. The vault is
canonical; repo-side files are overwritten on every sync.

## Error Handling

- **Transcript parsing:** Unparseable JSONL lines are silently skipped (partial transcripts still produce notes).
- **Index failures:** Logged as warnings, don't block note creation.
- **Stdin timeout:** 2-second deadline prevents hook from blocking Claude Code.
- **Missing config:** Falls back to `DefaultConfig()` (vault at `~/obsidian/vibe-vault`).
- **Enrichment failures:** Logged as warnings, note is still written unenriched. Returns `(nil, nil)` when disabled or API key missing — no error path.

## Token Accounting

Total input tokens = `input_tokens` + `cache_read_input_tokens` + `cache_creation_input_tokens`.
This is computed in `render.NoteDataFromTranscript()` and written to frontmatter as `tokens_in`.
