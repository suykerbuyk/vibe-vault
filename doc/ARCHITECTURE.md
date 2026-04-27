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
            provenance.go     NoteData.Host + NoteData.User + NoteData.CWD +
            sanitize.go       NoteData.OriginProject immediately after
                              NoteDataFromTranscript returns. Host resolves via
                              $VIBE_VAULT_HOSTNAME → os.Hostname(); User resolves
                              via $USER → $LOGNAME → user.Current(); CWD resolves
                              via $VIBE_VAULT_CWD → os.Getwd() then through
                              SanitizeCWDForEmit (vault-rooted → empty; otherwise
                              home-compressed, trailer-unsafe bytes neutralized);
                              OriginProject is session.DetectProject on the
                              stamped cwd. Single convergence point covers all
                              three capture paths (MCP vv_capture_session, hook
                              Stop/SessionEnd, zed-reprocess).
                         ▼
            index/            Score related sessions (shared files, threads, branch, tag)
            related.go
                         ▼
            render/           Build NoteData, render frontmatter + markdown body
            markdown.go       (host/user/cwd/origin_project emitted in YAML
                              before summary; includes Work Performed + related
                              sessions)
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
        │                            → (stamps Host/User/CWD/OriginProject
        │                               via meta.Stamp() + SanitizeCWDForEmit
        │                               + session.DetectProject; see Data Flow
        │                               diagram above)
        ├─── vv_append_iteration     → assemble iteration block
        │                            → append provenanceTrailer(meta.Stamp(),
        │                               cfg.VaultPath) HTML-comment trailer
        │                               (host=H user=U cwd=C origin=P, each
        │                               token conditional) into the block
        │                               (tools_context_write.go:~254);
        │                               vv_get_iterations strips it via
        │                               parseIterations before returning
        │                               narrative to callers.
        │
        ├─── vv_get_project_root     → meta.ProjectRoot(cwd, vaultPath)
        │                            → walk up checking agentctx/ before .git/
        │                            → returns ErrIsVaultRoot if matched = vault
        ├─── vv_set_commit_msg       → write commit.msg at project_path
        │                            → or vault-side fallback path
        ├─── vv_thread_insert        → mdutil.InsertSubsection() on resume.md
        ├─── vv_thread_replace       → mdutil.ReplaceSubsectionBody() on resume.md
        ├─── vv_thread_remove        → mdutil.RemoveSubsection() on resume.md
        ├─── vv_carried_add          → mdutil.AddCarriedBullet() on resume.md
        ├─── vv_carried_remove       → mdutil.RemoveCarriedBullet() on resume.md
        ├─── vv_carried_promote_to_task → remove bullet + create task file
        ├─── vv_render_commit_msg    → git status + diff --cached --stat
        │                            → render subject + body from convention file
        ├─── vv_synthesize_wrap_bundle→ read resume.md, iterations.md, knowledge.md
        │                            → build WrapBundle (all fields + SHA-256
        │                               fingerprints at synth time)
        ├─── vv_apply_wrap_bundle_by_handle    → in-process dispatch (see Canonical Wrap
        │                               Pattern diagram below); logs drift metrics
        │                               to ~/.cache/vibe-vault/wrap-metrics.jsonl
        │
        └─── prompt: vv_session_guidelines → agent instructions for capture
```

### Canonical Wrap Pattern

The recommended `/wrap` flow uses two tools instead of seven sequential
surgical calls. `vv_synthesize_wrap_bundle` reads all relevant vault state in one
call and returns a `WrapBundle` JSON object. The AI edits the bundle fields
(iteration narrative, thread updates, carried bullets, commit message,
capture summary). `vv_apply_wrap_bundle_by_handle` dispatches all writes in a single
in-process call — no MCP round-trips per write.

```
vv_synthesize_wrap_bundle(project, project_path)
        │
        ▼  WrapBundle JSON (with synth-time SHA-256 per field)
   AI edits bundle fields
   (iteration block, threads, carried bullets, commit msg, capture)
        │
        ▼
vv_apply_wrap_bundle_by_handle(project, project_path, bundle)
        │
        ▼  in-process sequential dispatch:
        ├── 1. vv_append_iteration       (iteration_block)
        ├── 2. vv_thread_insert          (resume_thread_blocks, each)
        ├── 3. vv_thread_replace         (resume_threads_to_replace, each; H2-v3)
        ├── 4. vv_thread_remove          (resume_threads_to_close, each)
        ├── 5. vv_carried_add            (carried_changes.add, each)
        ├── 6. vv_carried_remove         (carried_changes.remove, each)
        ├── 7. vv_set_commit_msg         (commit_msg)
        ├── 8. vv_capture_session        (capture_session)
        └── 9. resume_state_blocks       (DESIGN #90; state-derived sub-regions)
                │
                ▼  for each field:
        wrapmetrics.AppendBundleLines()
        → ~/.cache/vibe-vault/wrap-metrics.jsonl
          (synth SHA vs apply SHA, logged but never abort)

On first error: returns applied_writes + error_at_step.
Completed writes are not rolled back (each is semantically correct
in isolation).
```

**Step 9 — `resume_state_blocks`** (DESIGN #90). After
`capture_session` succeeds, `applyResumeStateBlocks` re-renders three
marker-bounded regions in `Projects/<p>/agentctx/resume.md` from
filesystem ground truth: the `### Active tasks (N)` H3 inside
`## Open Threads` (sourced from `tasks/*.md` minus `done/` /
`cancelled/`), the invariant-bullet block inside `## Current State`
(Iterations from `iterations.md` heading scan, MCP tool count from
`(*mcp.Server).ToolNames()`, embedded template count from
`templates.AgentctxFS()` walk; the Tests count is deliberately NOT
emitted — operator-chosen scope reduction documented in DESIGN #90),
and the last-N=10 rows of the iteration table inside
`## Project History (recent)`. The renderer
(`internal/wraprender/markers.go`) is self-healing: it locates
`<!-- vv:<region>:start --> ... <!-- vv:<region>:end -->` pairs and
replaces their contents in place, OR inserts the pair at a sensible
default location if absent. Step 9 is the last resume.md mutation in
any wrap cycle, so prior `vv_update_resume` clobbers from the inline
orchestrator path are healed automatically. Step 9's metric line
records `synth_sha == apply_sha == fingerprint(rendered_content)` and
is special-cased out of `driftSummary` (the bundle never carries
marker content, so synth-vs-apply drift accounting is meaningless).
Both inline and dispatch paths converge on `ApplyBundle`, so Step 9
fires uniformly for both — closing the iter-165–166 dispatch-path-
specific drift class.

The per-tool surgical APIs (`vv_thread_insert`, `vv_set_commit_msg`, etc.)
remain available for hand-edits but are not called from the canonical flow.

### Wrap Dispatch (Architecture A1)

The `wrap-model-tiering` epic (DESIGN #83-#88) splits the bundle synthesis
into a skeleton + prose pair and moves the per-tier executor dispatch loop
server-side, in-process inside the MCP server, behind a single
`vv_wrap_dispatch` MCP tool. The orchestrator (Opus, in the Claude Code
session) emits ONE MCP call per tier; the handler runs the multi-turn
tool-use loop with a downgraded executor (Sonnet first, escalating to Opus
on QC failure) and returns proposed outputs or an `escalate_reason`.
Quality gating runs BEFORE apply (H3-v2): the vault is atomic from its
perspective.

```
operator: /wrap
    │
    ▼
orchestrator (Opus, Claude Code session)
    │
    ├── pre-flight: vv mcp check --tools (assert 5 wrap tools present)
    │
    ├── vv_prepare_wrap_skeleton(orchestrator-facts)
    │     → server writes ~/.cache/vibe-vault/wrap-bundles/iter-<N>-skeleton.json
    │     → returns SkeletonHandle{iter, path, sha256}
    │
    ├── for tier in [sonnet, opus]:
    │     │
    │     │ ┌──────────────────────────────────────────────────────────┐
    │     │ │ vv_wrap_dispatch(handle, tier, "wrap-executor")          │
    │     │ │  (server-side, in-process, in MCP server)                │
    │     │ │   ├── resolve tier → provider:model from [wrap.tiers]    │
    │     │ │   ├── agentregistry.Lookup("wrap-executor") (direct Go)  │
    │     │ │   ├── instantiate AgenticProvider                        │
    │     │ │   ├── ToolsRequest{system, messages, tools=[             │
    │     │ │   │     vv_synthesize_wrap_bundle (OQ-5 direct helper),  │
    │     │ │   │     wrap_executor_finish (in-loop, H4-v3)]}          │
    │     │ │   ├── provider.RunTools() → multi-turn loop              │
    │     │ │   │     │ executor: vv_synthesize_wrap_bundle(prose)     │
    │     │ │   │     │   → handler routes to FillBundle() directly    │
    │     │ │   │     │ executor: wrap_executor_finish(status, ...)    │
    │     │ │   │     │   → captured in Go variable (in-loop)          │
    │     │ │   ├── stderr progress: [wrap-dispatch] tier=... t=...    │
    │     │ │   ├── write DispatchLine to wrap-dispatch.jsonl          │
    │     │ │   └── return {outputs?, escalate_reason?, metrics}       │
    │     │ └──────────────────────────────────────────────────────────┘
    │     │
    │     ├── if escalate_reason: continue
    │     │
    │     ├── vv_wrap_quality_check(handle, outputs)
    │     │     → 4 trigger checks (read-only against vault)
    │     │     → if !passed: continue
    │     │
    │     └── break  # tier succeeded
    │
    ├── vv_apply_wrap_bundle_by_handle(handle, outputs)
    │     → mutate vault: iter + threads + carried + commit_msg + capture
    │
    └── git plumbing (Bash): add, commit, push
```

Key invariants:

- **Skeleton handle compare-and-set** — every consumer
  (`vv_synthesize_wrap_bundle`, `vv_apply_wrap_bundle_by_handle`,
  `vv_wrap_quality_check`, `vv_wrap_dispatch`) verifies the on-disk
  skeleton's sha256 matches the handle's; mismatch returns `"skeleton
  cache file modified after handle issued"`. Protects against
  concurrent /wrap runs and manual cache edits. (DESIGN #86.)
- **QC reads, never mutates** — `vv_wrap_quality_check` runs the
  multi-match dry-run against live vault state but is byte-equality
  invariant on resume.md / iterations.md.
  `TestVVWrapQualityCheck_NoVaultMutation` is structural. (DESIGN #87.)
- **Synthesize routes in-process** — when the executor calls
  `vv_synthesize_wrap_bundle` from inside the dispatch loop, the handler's
  local `ToolExecutor` invokes `internal/mcp/wrapbundle.go::FillBundle()`
  via direct Go call (NOT a re-entrant MCP roundtrip). (DESIGN #84,
  OQ-5.)
- **Terminal signal is in-loop, not registered** — the executor finishes
  the conversation by calling the in-loop tool spec
  `wrap_executor_finish(status, reason?, outputs?)`; that name is NOT in
  the registered MCP tool list. Treating it as a registered tool would
  leak a private handshake into the public surface. (DESIGN #84, H4-v3.)
- **Per-wrap dispatch metrics** — `wrap-dispatch.jsonl` is a sibling to
  the existing `wrap-metrics.jsonl`; the two writers and schemas are
  independent. `vv stats wrap` reads both. (DESIGN #88.)

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
| `cmd/wrap-trace` | `main.go` | Phase 0 measurement harness: replays a session transcript through the full wrap pipeline, measures per-step latency and token cost, and emits a golden JSONL report. Reuses `internal/transcript/parser.go` for transcript reading; no production MCP dependency. |
| `templates` | `embed.go` | `//go:embed all:agentctx` — embeds 23 agentctx template files (commands, skills, settings) into the binary; `AgentctxFS()` returns the `embed.FS`. Templates use `{{PROJECT}}`/`{{DATE}}` placeholders resolved at runtime. These are Tier 1 of the three-tier template cascade (see DESIGN.md #46). |
| `context` | `context.go` | `Init()` — scaffold vault-resident context (templates from embed.FS, repo-side CLAUDE.md symlink + .claude/{commands,rules,skills,agents} symlinks, agentctx symlink, .version); `Migrate()` — copy local files to vault + force-update repo-side; `claudeSubdirs` var defines .claude/ subdirectories; helpers: safeWrite, safeSymlink, gitignoreEnsure, copyFile/Dir |
| `context` | `schema.go` | `VersionFile` TOML struct, `ReadVersion`/`WriteVersion`, `LatestSchemaVersion` const (10), `Migration` type + registry (0→1 through 7→8 plus a no-op 9→10 contract-marker entry that brings post-v7 vaults to v10 in one step), `MigrationContext` (incl. `DryRun` field), `migrationsFrom()` |
| `context` | `invariants.go` | v10 Current-State contract primitives: `CurrentStateSection` const, `IsInvariantBullet(line)` line-level classifier (18-entry first-word whitelist + 200-rune trailing cap; regex tolerates leading dash/indentation and widened key class for acronyms like `MCP`/`CLI`), `ValidateCurrentStateBody(body)` document-level scanner (skips blanks, markdown headings, and single-/multi-line HTML-comment regions via `inComment` state flag; every other line must satisfy `IsInvariantBullet`). Consumed by the synthesis agent's Features-routing prompt and the `vv_update_resume` v10 guard. |
| `context` | `sync.go` | `Sync()` — run schema migrations + three-way baseline propagation for one or all projects; `SyncOpts`/`SyncResult`/`ProjectSyncResult` types; `propagateSharedSubdir()` with `.baseline` tracking (template vs baseline vs project three-way comparison); `propagateDir()` with `dirContentsChanged()` gate; `isSidecar()`/`writeBaseline()`/`readBaseline()`/`cleanPending()` helpers; `forceUpdateVaultTemplates()`; migrations `1→2` through `7→8` (level-set with baselines) |
| `context` | `template.go` | `TemplateVars`, `DefaultVars()`, `resolveTemplate()` (vault Templates/agentctx/ first, fallback to `templates.AgentctxFS()`), `readEmbedded()`, `applyVars()` ({{PROJECT}}/{{DATE}}), `BuiltinTemplates()` (walks embed.FS), `EnsureVaultTemplates()` (seed-once for Init), `forceUpdateVaultTemplates()` (always-overwrite for Sync — Tier 1→2 refresh) |
| `friction` | `types.go` | `Correction`, `Signals`, `Result`, `ProjectFriction` types |
| `friction` | `detect.go` | `DetectCorrections()` — linguistic (negation, redirect, undo, quality, repetition) + contextual (short negation after long assistant turn) correction detection |
| `friction` | `score.go` | `Score()` — weighted composite friction score (0-100): correction density (30), token efficiency (25), file retry (20), error cycles (15), recurring threads (10) |
| `friction` | `analyze.go` | `Analyze()` — pure-function orchestrator: corrections + narrative signals + token efficiency + thread recurrence → `Result` with score + human-readable signals |
| `friction` | `format.go` | `ComputeProjectFriction()` — aggregate per-project friction from index; `Format()` — aligned terminal output for `vv friction` |
| `mcp` | `protocol.go` | JSON-RPC 2.0 and MCP message types (Request, Response, InitializeResult, ToolDef, ToolsCallResult, ContentBlock, PromptDef, PromptArg, PromptMessage) |
| `mcp` | `server.go` | Stdio transport: `Server.Serve()` reads newline-delimited JSON, dispatches initialize/tools/list/tools/call/prompts/list/prompts/get, logs tool calls to stderr |
| `mcp` | `tools.go` | 8 read/capture tools (all `vv_`-prefixed): `vv_get_project_context`, `vv_list_projects`, `vv_search_sessions`, `vv_get_knowledge`, `vv_get_session_detail`, `vv_get_friction_trends`, `vv_get_effectiveness`, `vv_capture_session` |
| `mcp` | `tools_project.go` | `vv_get_project_root` — calls `meta.ProjectRoot()` and returns the project root path, or an error if in vault root |
| `mcp` | `tools_commit_msg.go` | `vv_set_commit_msg` — writes `commit.msg` at an explicit `project_path` or falls back to a vault-side path; `subject` is required |
| `mcp` | `tools_thread.go` | `vv_thread_insert`, `vv_thread_replace`, `vv_thread_remove` — surgical Open Threads subsection edits on `resume.md` using `mdutil` subsection family; rejects the reserved "Carried forward" slug |
| `mcp` | `tools_carried.go` | `vv_carried_add`, `vv_carried_remove`, `vv_carried_promote_to_task` — manage `Carried forward` bullet list in resume.md via `mdutil.CarriedBullet` helpers; promote creates a task file and removes the bullet atomically |
| `mcp` | `tools_render_commit_msg.go` | `vv_render_commit_msg` — reads `git status` + `git diff --cached --stat`, renders a conventional commit message from convention file and AI-supplied subject+body; `RenderCommitMsg()` exported as package-level function for reuse in `vv_synthesize_wrap_bundle` |
| `mcp` | `tools_synthesize_wrap.go` | `vv_synthesize_wrap_bundle` — handle-based; reads cached skeleton via `wrapbundlecache`, calls `wrapbundle.FillBundle()` with executor-supplied prose, returns the filled `WrapBundle`. Compare-and-set via `skeleton_sha256`. Replaces the pre-epic inline `vv_synthesize_wrap` (DESIGN #86, Decision 23 fold). |
| `mcp` | `tools_apply_wrap_bundle.go` | `vv_apply_wrap_bundle_by_handle` — handle-based in-process orchestrator: dispatches all `WrapBundle` writes via `wrapapply.ApplyBundle()` (no MCP round-trips); applies the H2-v3 ordering `iter → thread_insert → thread_replace → thread_remove → carried_add → carried_remove → set_commit_msg → capture → resume_state_blocks` (Step 9, DESIGN #90); logs synth-vs-apply SHA drift to `wrapmetrics` (Step 9 special-cased to skip the drift counter); fail-stop on first error, no rollback. The new `applyResumeStateBlocks` helper re-renders the three marker-bounded resume.md sub-regions (active-tasks, current-state, project-history-tail) from filesystem ground truth. |
| `mcp` | `tools_prepare_skeleton.go` | `vv_prepare_wrap_skeleton` — collects orchestrator-supplied facts, calls `wrapbundle.BuildSkeleton()`, persists to `~/.cache/vibe-vault/wrap-bundles/iter-<N>-skeleton.json` via `wrapbundlecache`, log-rotates to keep three most recent; returns `SkeletonHandle{iter, path, sha256}` (DESIGN #86). |
| `mcp` | `wrapbundle.go` | `WrapSkeleton` + `WrapBundle` types; `BuildSkeleton()` (orchestrator-facts only) and `FillBundle(skeleton, prose)` (executor prose merge) pure helpers. Source of the skeleton sha256 stamp consumed by all four handle-aware tools. |
| `mcp` | `wrapapply.go` | Extracted `ApplyBundle(ctx, ...)` helper carrying the H2-v3 mutation-class dispatch order including the `thread_replace` step plus DESIGN #90's Step 9 `resume_state_blocks` (last). Called by both `vv_apply_wrap_bundle_by_handle` and the dispatch handler's quality-gate path. The package comment documents Step 9 ordering vs `vv_update_resume`: Step 9 must remain LAST so any prior orchestrator clobber of a marker pair is healed by `wraprender.ApplyMarkerBlocks`'s self-healing insertion. |
| `wraprender` | `markers.go` | Renderer for resume.md state-derived sub-regions (DESIGN #90). Public API: `RenderActiveTasks`, `RenderCurrentState`, `RenderProjectHistoryTail`, `ApplyMarkerBlocks`. The latter is **self-healing** — it replaces marker-pair contents in place when the pair is present, OR inserts the pair at a sensible default location relative to existing H2/H3 anchors when absent. Drives `applyResumeStateBlocks` in `internal/mcp/tools_apply_wrap_bundle.go`. |
| `mcp` | `tools_quality_check.go` | `vv_wrap_quality_check` — runs the four QC triggers (multi-match ambiguity, mutation-count mismatch, semantic-presence failure, commit-subject invalid) against proposed outputs; reads vault state for the dry-run check but never mutates it (H3-v2 invariant; DESIGN #87). Compare-and-set via `skeleton_sha256`. |
| `mcp` | `tools_wrap_dispatch.go` | `vv_wrap_dispatch` — server-side dispatch entry point (Architecture A1). Resolves `tier` → `provider:model` from `[wrap.tiers]`, looks up agent definition via `agentregistry`, calls `llm.ResolveAPIKey(tierProvider, cfg.Providers)` to obtain the key (DESIGN #89; same resolver as `NewProvider`), instantiates `AgenticProvider`, runs `internal/wrapdispatch.Dispatch()` and returns `{outputs?, escalate_reason?, dispatch_metrics}`; emits stderr progress lines and writes one `DispatchLine` per call (DESIGN #84). |
| `mcp` | `tools_agents.go` | `vv_get_agent_definition(name)` — alternative read path for the embedded agent registry (v2 portability scaffolding; v1 `vv_wrap_dispatch` reads via direct Go call). Returns sha256-stamped agent definition record. |
| `mcp` | `tools_vault.go` | 8 generic vault-relative file accessor tools: `vv_vault_read`, `vv_vault_list`, `vv_vault_exists`, `vv_vault_sha256`, `vv_vault_write`, `vv_vault_edit`, `vv_vault_delete`, `vv_vault_move`. Each constructor (`NewVault*Tool(cfg config.Config)`) closure-captures `cfg.VaultPath`; the AI passes vault-relative paths only and the handler joins them under the configured vault root via `vaultfs` package. Write/edit/delete/move accept an optional `expected_sha256` for compare-and-set. Reads cap at 1 MB by default (settable up to 10 MB via `max_bytes`). |
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
| `llm` | `provider.go`, `types.go`, `retry.go`, `openai.go`, `anthropic.go`, `google.go` | Multi-provider LLM abstraction: `Provider` interface, OpenAI-compatible/Anthropic/Gemini implementations, retry with backoff. `NewProvider(enrich, providers)` calls `ResolveAPIKey(enrich.Provider, providers)` to obtain the key (config-first / env-fallback / actionable-error), so hook + synthesis paths share the same resolution semantics as `vv_wrap_dispatch` (DESIGN #89) |
| `llm` | `keyresolver.go` | `ResolveAPIKey(provider, providers) (string, error)` — single resolution point for Anthropic / OpenAI / Google API keys. Three-tier precedence: `[providers.<P>].api_key` (config) → `os.Getenv(envVarFor(provider))` → actionable error naming both `vv config set-key <provider> <key>` and the env var. Called by `NewProvider` (hook + synthesis path, routes by `[enrichment].provider`) AND by the dispatch handler in `internal/mcp/tools_wrap_dispatch.go` (routes by `[wrap.tiers]` provider-prefix). Different routing axes, identical resolution (DESIGN #89). |
| `templates` (internal) | `templates.go`, `diff.go`, `reset.go` | Template registry, vault-vs-embedded comparison, `vv templates` status reporting |
| `vaultsync` | `vaultsync.go` | `Classify()` — file classification (Regenerable/AppendOnly/Manual/ConfigFile) for conflict resolution; `GetStatus()` — vault git state (branch, clean/dirty, ahead/behind); `Pull()` — fetch + rebase with auto-stash and classification-driven conflict resolution; `CommitAndPush()` — stage all, commit with hostname stamp, push with rebase-fallback + force-with-lease convergence to prior remotes; `EnsureRemote()` — verify origin exists |
| `synthesis` | `types.go` | Data structures: `Input`, `Result`, `Learning`, `StaleEntry`, `ResumeUpdate`, `TaskUpdate`, `ActionReport` |
| `synthesis` | `gather.go` | `GatherInput()` — collect session note, git diff (8KB cap), knowledge.md, resume.md, recent history (last 5 sessions), active tasks into `Input` struct |
| `synthesis` | `prompt.go` | System and user prompt construction for LLM synthesis call; bullet numbering for LLM reference; structured JSON output schema |
| `synthesis` | `synthesize.go` | `Synthesize()` — LLM invocation (temp 0.3, JSON mode) + response validation/filtering (section names, file targets, index bounds, action types) |
| `synthesis` | `actions.go` | `Apply()` — execute synthesis result: append learnings to knowledge.md (with significant-word duplicate detection), flag stale entries (index + fuzzy fallback), update resume sections, move completed tasks to `done/` |
| `synthesis` | `run.go` | `Run()` — top-level orchestrator: gather → synthesize → apply; short-circuits on nil provider, disabled config, or empty result |
| `mdutil` | `mdutil.go` | Shared markdown/text utilities: `SignificantWords()` (4+ char, stop-word filtered), `Overlap()`/`SetIntersection()` (word set operations), `ReplaceSectionBody()` (heading-targeted markdown editing), `AtomicWriteFile()` (temp + rename crash safety); subsection family: `ReplaceSubsectionBody()`, `InsertSubsection()`, `RemoveSubsection()`, `NormalizeSubheadingSlug()` (text up to first ` — ` separator) |
| `vaultfs` | `safety.go`, `read.go`, `write.go`, `types.go` | Generic vault-relative file accessors (read, list, exists, sha256, write, edit, delete, move) with path-traversal protection, case-insensitive `.git` refusal, atomic writes via `mdutil` delegation. `ValidateRelPath()` rejects absolute paths, `..` segments, null/control bytes, empty, and bare `.`; `ResolveSafePath()` joins under the configured vault root and verifies the realpath via `filepath.EvalSymlinks` stays inside the vault; `IsRefusedWritePath()` rejects any path whose segment matches `.git` case-insensitively. Linux-primary scope (no Windows-reserved-name check). Powers the eight `vv_vault_*` MCP tools. |
| `mdutil` | `carried.go` | `CarriedBullet` type + liberal-on-read parser (`ParseCarriedForward`) + strict-on-write emitter (`EmitCarriedBullets`, `BuildCarriedBullet`); `AddCarriedBullet()`, `RemoveCarriedBullet()`, `GetCarriedBullet()` for resume.md "Carried forward" subsections |
| `wrapmetrics` | `writer.go` | Host-local JSONL metric writer at `~/.cache/vibe-vault/wrap-metrics.jsonl`; `AppendLine()`, `AppendBundleLines()`, `CacheDir()`, rotation to `wrap-metrics-archive-YYYY.jsonl` at 1000-line threshold via `rotateIfNeeded()` |
| `wrapmetrics` | `dispatch_writer.go` | Append-only writer for the per-wrap dispatch jsonl `~/.cache/vibe-vault/wrap-dispatch.jsonl` (sibling to `wrap-metrics.jsonl`). `WriteDispatchLine()` emits one `DispatchLine{tier, provider_model, duration_ms, outcome, expected_mutations, actual_mutations, input_tokens, output_tokens, escalate_reason}` per `vv_wrap_dispatch` invocation; `ReadDispatchLines(limit)` for `vv stats wrap`. Two writers and schemas independent of the per-field drift writer (DESIGN #88). |
| `wrapmetrics` | `stats.go` | `ComputeWrapStats()` aggregates both jsonl files: per-tier median duration, escalation rate, top reasons, plus per-field median drift_bytes. `FormatWrapStats()` renders the `vv stats wrap` terminal output. |
| `wrapbundlecache` | `cache.go` | Host-local skeleton cache at `~/.cache/vibe-vault/wrap-bundles/iter-<N>-skeleton.json`. `Write()` (atomic temp+rename), `Read(iter)` with traversal-rejection on the iter argument, `RotateKeepN(n)` deletes oldest skeletons keeping the n most recent (default n=3). Survives MCP server restart; underpins the skeleton-handle compare-and-set (DESIGN #86). |
| `wrapdispatch` | `dispatch.go` | Pure-Go dispatch loop logic invoked by the `vv_wrap_dispatch` MCP handler. `Dispatch(ctx, Request) (Response, error)` runs the agentic LLM tool-use loop (mockable `AgenticProvider` seam for tests), routes the in-loop `vv_synthesize_wrap_bundle` callback to `internal/mcp/wrapbundle.FillBundle()` directly (OQ-5), recognizes the in-loop terminal signal `wrap_executor_finish` (H4-v3), enforces a max-iterations breaker, emits stderr progress per executor tool-call, and returns either `{outputs, metrics}` or `{escalate_reason, metrics}` (DESIGN #84). |
| `agentregistry` | `registry.go`, `embedded.go` | Embedded registry of agent definitions (system prompt + tool whitelist + escalation triggers + output_format). `Lookup(name)` returns the stored definition with sha256 stamp; `List()` returns sorted names. `//go:embed agents/*.md` ships definitions in-binary. v1 `vv_wrap_dispatch` consumes via direct Go call; the MCP tool `vv_get_agent_definition` and the generated `.claude/agents/wrap-executor.md` artifact (via `make agents`) are v2 portability surfaces (DESIGN #85). |
| `llm` | `anthropichttp.go` | `anthropicHTTPCore` shared HTTP plumbing core (base URL, model, API key, max tokens, retry policy, http.Client). Embedded by both text-only `Anthropic` and tool-use `AnthropicAgentic` to eliminate duplication of header injection + retry + status branching (C1-v2 fix; DESIGN #83). Pattern anticipates a future `openaihttp.go` for v2 multi-provider lift. |
| `llm` | `anthropic_agentic.go`, `types.go` | `AgenticProvider interface { Provider; RunTools(ctx, ToolsRequest) (ToolsResponse, error) }` defined in `types.go` next to the existing text-only `Provider`. `AnthropicAgentic` (v1) implements it via Anthropic's tool-use API: multi-turn loop with `ToolExecutor` callback, `tool_use` / `tool_result` block round-tripping, max-iterations breaker, and integration test build-tagged behind `//go:build integration` (DESIGN #83). |
| `meta` | `provenance.go`, `sanitize.go` | `Stamp()` — resolves host/user/cwd/origin_project for provenance metadata. `HomeDir()` — config-aware home directory. `ProjectRoot(cwd, vaultPath)` — walks up the directory tree checking for `agentctx/` first, then `.git/`; returns `ErrIsVaultRoot` if matched directory equals the configured vault path |
| `sanitize` | `redact.go` | Regex-based XML tag stripping for Claude Code wrapper tags |
| `memory` | `memory.go` | `Link()`/`Unlink()` for `vv memory` — slug derivation (symlink-resolved + `/` → `-`), project resolution via `session.DetectProject`, migrate pre-existing host-local memory into the vault target (drop identical, move unique, quarantine conflicts to sibling `memory-conflicts/{timestamp}/` under `--force`), establish/remove the `~/.claude/projects/{slug}/memory` ↔ `Projects/{name}/agentctx/memory` symlink. Host-local writes go through to the vault by POSIX symlink semantics; see DESIGN.md #48 |

### Cross-package dependencies

- `vaultfs` → `mdutil`: `vaultfs.Write`/`Edit`/`Move` delegate atomic file
  writes to `mdutil.AtomicWriteFile(path, data, 0o644)`. The dependency is
  one-directional; `mdutil` imports stdlib only and has no upward awareness
  of `vaultfs`. There is no import cycle. `vaultfs` always passes
  `perm = 0o644` (matching the 24 existing call sites elsewhere in the
  codebase) so the helper is not duplicated and the on-disk mode bits are
  uniform across writers. The unrelated `atomicWriteCommitMsg` in
  `internal/mcp/tools_commit_msg.go` is intentionally NOT consolidated —
  its alternate caller writes outside the vault scope and `vaultfs.Write`
  cannot subsume it. (Mirrors the iter-152 cross-package documentation
  pattern.)

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
