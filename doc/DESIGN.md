# Design Decisions

Key architectural and design decisions in vibe-vault, with rationale.

1. **Transcript-first, no external state:** The binary reads the JSONL transcript
   directly. No dependency on MEMORY directories, state files, or other hooks.
   This avoids the compounding failure mode that broke the TypeScript predecessor.

2. **Streaming JSONL parser with 10MB line limit:** Uses `bufio.Scanner` with a
   large buffer rather than loading the full file into memory. Unparseable lines
   are skipped rather than failing the whole transcript.

3. **Session index for dedup and linking:** `.vibe-vault/session-index.json`
   stores all processed sessions keyed by `session_id`. Provides duplicate
   detection, same-day iteration counting, and cross-session wikilinks.

4. **Git remote-based project detection:** The project name is extracted from
   `git remote get-url origin` (stable across worktrees, renames, and machines),
   falling back to `filepath.Base(cwd)` when not in a git repo or no origin
   remote exists. Uses a 1-second timeout to handle NFS/CI edge cases.
   Domain is determined by matching CWD against configured workspace prefixes
   (`~/work/` -> work, `~/personal/` -> personal, `~/opensource/` -> opensource).

5. **Title heuristics skip noise:** `FirstUserMessage()` skips slash commands,
   `@resume` references, system caveats, and trivial confirmations (yes/ok/hi)
   to find the first meaningful user message for the session title.

6. **Notes organized by project, not date:** Session notes go to
   `Projects/{project}/sessions/YYYY-MM-DD-NN.md` rather than a date-based
   hierarchy. This keeps related sessions together in Obsidian's file explorer
   and separates session notes from project-level files (history.md,
   knowledge.md, tasks/).

7. **Three direct dependencies (BurntSushi/toml, klauspost/compress,
   modernc.org/sqlite):** Minimal dependency tree for a tool that runs on every
   session end. Enrichment uses `net/http` from stdlib -- no LLM SDKs. Zstd
   added in Phase 4 for transcript archival (~10:1 compression on JSONL).
   SQLite added in Phase 8 for Zed thread parsing and session index. fsnotify
   is indirect (via sqlite/zed watcher).

8. **Multi-provider LLM abstraction via `internal/llm/`:** The `Provider`
   interface (`ChatCompletion(ctx, Request) (*Response, error)` + `Name()`)
   with three implementations: OpenAI-compatible (covers xAI/Grok, OpenAI,
   Ollama), Anthropic Messages API, and Google Gemini REST API. All use direct
   `net/http` -- no SDK dependencies -- preserving the minimal dependency
   philosophy. `NewProvider(cfg)` factory routes by `cfg.Provider` field and
   wraps with `WithRetry()` (single retry with 2s backoff on transient errors).
   `enrichment.Generate()` accepts `llm.Provider`
   instead of building their own HTTP clients. Provider instantiated once in
   `hook/handler.go` and passed through `CaptureOpts`.

9. **Enrichment prompt design:** System prompt requests JSON with summary
   (1-3 sentences, past tense, outcome-focused), decisions (0-5, "Decision --
   rationale" format), open_threads (0-3 action items), and tag (one of 6
   activity categories). User/assistant text truncated to 12K chars each
   (~6K tokens total) to fit small model context windows.

10. **Heuristic cross-session linking:** Related sessions are computed using
    weighted scoring across four signals (shared files, thread->resolution
    matching, branch, tag) rather than LLM similarity. This keeps the index
    rebuild fast and deterministic. `significantWords()` filters short words
    and stop words for thread resolution matching.

11. **Note parser without YAML library:** `noteparse` uses a simple state
    machine (find `---`, read key:value, find closing `---`) because session
    note frontmatter is flat key-value pairs with no nested structures. This
    avoids adding a YAML dependency for a trivial use case.

12. **Per-project context docs as `history.md`:** Lives at
    `Projects/{project}/history.md` -- outside the `sessions/` subdirectory,
    so the rebuild skip logic (only process files inside `sessions/`) naturally
    excludes it from the index (self-referential loop prevention).

13. **CaptureOpts struct for extensibility:** Phase 4 replaced the positional
    `Capture(path, cwd, sessionID, cfg)` signature with `Capture(CaptureOpts, cfg)`.
    The `Force` field bypasses dedup for reprocessing. `TranscriptPath` is stored
    in the index so archive and reprocess can find the original later.

14. **Transcript discovery via UUID filename pattern:** `discover.Discover()`
    walks directories and filters by `[0-9a-f]{8}-...-[0-9a-f]{12}.jsonl`
    regex to skip non-transcript JSONL files. Subagent detection uses path
    component matching (`/subagents/`). Results sorted by mtime for
    chronological processing.

15. **Archive with zstd default level:** `archive.Archive()` uses zstd default
    compression (good speed/ratio balance). `Decompress()` returns a temp file
    path plus cleanup function -- caller defers cleanup. Archive dir is
    `.vibe-vault/archive/` inside the vault.

16. **Three-tier transcript lookup in reprocess:** Reprocess checks for
    transcripts in order: (1) original path from `TranscriptPath` in index,
    (2) archived copy in `.vibe-vault/archive/`, (3) fallback discovery scan
    of `~/.claude/projects/`. This handles all lifecycle stages -- live, archived,
    or moved.

17. **Build-time version injection via git describe:** `help.Version` is a `var`
    (not `const`) defaulting to `"dev"`. The Makefile injects the real version via
    `-ldflags "-X ...help.Version=$(VERSION)"` where `VERSION` is derived from
    `git describe --tags --always --dirty`. Tagged builds show `v0.3.0`, post-tag
    commits show `v0.3.0-2-g1a2b3c4`, dirty builds append `-dirty`, and `go run`
    without make shows `dev`. Single source of truth: git tags.

18. **Narrative extraction as pure-function layer:** `narrative.Extract()` takes
    a `*transcript.Transcript` and CWD string, returns a `*Narrative` struct.
    No I/O, no config, no side effects. Sits between parsing and rendering in
    the capture pipeline. Produces rich session documents heuristically from
    tool call patterns -- file creates/modifies, test runs, git commits, plan
    mode, decisions -- without requiring LLM enrichment. LLM enrichment becomes
    optional polish: it can refine summary/decisions/threads/tag but never
    replaces the factual WorkPerformed activity log. Segments split at
    `compact_boundary` entries, exploration activities are aggregated, and
    error->recovery patterns are detected within a 3-activity window.

19. **Semantic summaries replace mechanical file counts:** `inferSummary()` now
    generates intent-driven summaries using conventional commit prefixes and
    condensed outcomes (e.g., `feat: rate limiter (4+4 files, tests pass)` instead
    of `Created 4 and modified 4 files. All tests passed.`). Priority order for
    the subject: first commit message body, then session title, then dominant
    file path pattern. Commits are now extracted inside `narrative.Extract()`
    so they're available for summary generation.

20. **Friction analysis as pure-function pipeline:** `friction.Analyze()` is a
    pure function taking dialogue, narrative, stats, and prior threads -- no I/O,
    no config, no side effects. Correction detection uses two tiers: linguistic
    patterns (negation, redirect, undo, quality, repetition) on user turns, and
    contextual patterns (short negation after long assistant turn). The composite
    friction score (0-100) weights five signals: correction density (30%), token
    efficiency (25%), file retry density (20%), error cycle density (15%), and
    recurring open threads (10%). Fields persist in the index as `corrections`
    and `friction_score` with `omitempty` for backward compatibility.

21. **Metric trends via weekly ISO buckets with rolling averages:**
    `trends.Compute()` buckets sessions into ISO weeks, computes per-week averages
    for four metrics (friction score, tokens/file, corrections, duration), then
    applies a 4-week rolling average with 1.5 sigma anomaly detection. Direction is
    determined by comparing the most recent 4-week average against the previous
    4-week average (needs 8+ weeks of data). Zero-value metrics are excluded from
    their respective buckets to avoid diluting averages. The `--weeks` flag
    controls display window without affecting rolling average computation. Points
    are stored most-recent-first for display, oldest-first internally for
    computation.

22. **Per-project knowledge as manual files:** Knowledge is inherently
    project-specific, not global. Each project gets a simple `knowledge.md`
    file (seeded by `GenerateContext()`) with sections for Decisions,
    Patterns, and Learnings. No automated extraction machinery — humans
    (or AI via `/distill`) write entries manually. The previous global
    `Knowledge/` system with automated LLM extraction was removed as
    architecturally flawed (global when knowledge is project-specific)
    and never produced useful notes.

23. **Prose dialogue extraction as orthogonal layer:** `prose.Extract()` extracts
    *dialogue* from *text blocks* while narrative extracts *activities* from
    *tool calls*. They operate on different data in the transcript. Prose turns
    are interleaved with italic tool markers (file creates, test runs, commits).
    A filler filter excludes assistant text < 120 chars when tool_use blocks
    are present ("Let me read the file" noise). Pure-text assistant entries
    are always kept regardless of length. User turns are capped at 500 chars
    (longer text is usually pasted code). When prose succeeds, it replaces the
    summary paragraph ("## Session Dialogue" instead of "## What Happened")
    and LLM enrichment is skipped -- the raw conversation is higher quality
    than any LLM summary.

24. **agentctx/ as self-contained AI context directory:** All AI context
    for a project lives in `Projects/{project}/agentctx/` -- CLAUDE.md
    (behavioral rules), resume.md, iterations.md, commands/, tasks/. This
    separates human-curated AI context from machine-generated output
    (sessions/, history.md which remain at the project root). The repo-side
    CLAUDE.md is a 5-line pointer to the agentctx path. Slash commands
    are symlinked from `.claude/commands/` to `agentctx/commands/`. The
    entire AI context package is portable as a single directory and syncs
    across machines via obsidian-git. The agentctx/workflow.md includes rich
    behavioral rules (pair programming paradigm, plan mode, subagent strategy,
    verification standards) and a forward pointer to resume.md for dynamic state.

25. **Auto-refresh context on session end via `index.Load()` fast path:**
    `handleSessionEnd()` calls `index.Load()` (reads one JSON file, <100ms)
    rather than `index.Rebuild()` (walks all session notes) after capture.
    `session.Capture()` already updates and saves the index, so Load picks up
    the just-written entry. `GenerateContext()` takes `[]KnowledgeSummary` as
    a parameter (not reading knowledge internally) to avoid an import cycle
    (`index` -> `knowledge` -> `friction` -> `index`). Errors are stderr warnings
    that never fail the hook. Stop checkpoints skip context refresh.

26. **Cost estimation via glob-pattern model matching:** `PricingConfig` holds
    an array of `PricingModel` entries with glob patterns (e.g., `"claude-opus-*"`)
    and per-million token rates (input, output, cache read, cache write). Cost is
    computed during capture (where full cache breakdown is available) and stored
    in `SessionEntry.EstimatedCostUSD` -- aggregate stats sum the stored values
    rather than re-estimating from combined token counts.

27. **Tool effectiveness as gated analysis:** `stats.AnalyzeTools()` returns
    nil when all tools succeeded and no struggle patterns exist -- the "Tool
    Effectiveness" section only renders in session notes when there's something
    interesting to report (errors or 3+ Read->Edit cycles on the same file).

28. **Advisory file locking for index concurrency:** `index.Lock()` uses
    `syscall.Flock` (advisory, not mandatory) to prevent concurrent `Capture()`
    calls from corrupting the session index. Lock acquired before `index.Load()`,
    released after `idx.Save()`. Non-fatal: if lock fails, capture proceeds
    with a warning.

29. **Configurable session tags with `vv-session` default:** Session notes are
    tagged via `[tags]` config section: `session` sets the base tag (default
    `vv-session`), `extra` adds additional tags to every session. Tags are built
    by `Config.SessionTags(activityTag)` which concatenates session + extra +
    activity tag. The renderer uses `SessionTags` when populated, falling back
    to hardcoded `vv-session` for backward compatibility. The noteparser skips
    the base `vv-session` tag when extracting activity tags from existing notes.

30. **Per-project config overlay via `agentctx/config.toml`:** Each project can
    override global config settings by uncommenting values in
    `Projects/{project}/agentctx/config.toml`. The overlay is applied after
    project detection in `session.Capture()` via `Config.Overlay()`, which uses
    TOML metadata (`IsDefined()`) to only replace fields explicitly present in
    the overlay file. A fully-commented file changes nothing. This allows
    per-project enrichment models, custom tags, friction thresholds, etc.
    without duplicating the entire config. Schema migration 2→3 generates the
    template for existing projects.

31. **Zed thread parsing with custom JSON unmarshaling:** Zed serializes agent
    panel threads using Rust's `serde` enum format: messages are `{"User": {...}}`
    or `{"Agent": {...}}` envelopes (not `{role: "user"}`), content blocks are
    `{"Text": "..."}` or `{"ToolUse": {...}}` discriminated unions (not
    `{type: "text"}`), and tool results live on the Agent message in a
    `tool_results` map (not as separate conversation entries). The `internal/zed`
    package implements custom `json.RawMessage`-based unmarshaling that first
    decodes the envelope discriminator, then dispatches to type-specific parsers.
    This isolates all Zed schema complexity in the type layer — downstream code
    works with normalized `ZedMessage` structs with `Role`, `Content`, and
    `ToolResults` fields.

32. **Zed project detection without git subprocess:** Unlike Claude Code sessions
    where `session.Detect()` runs `git remote get-url origin`, Zed threads
    include an `initial_project_snapshot` with the worktree path and git branch
    already captured. `zed.DetectProject()` builds `session.Info` directly from
    this metadata, avoiding subprocess calls that would fail on stale or moved
    repository paths from historical threads.

33. **Zed tool name normalization at conversion time:** Zed uses different tool
    names than Claude Code (e.g., `terminal`/`bash` vs `Bash`, `read_file` vs
    `Read`, `find_path` vs `Glob`). A 28-entry normalization map in `convert.go`
    translates at parse time so downstream narrative/prose extraction can use
    the same canonical names as Claude Code sessions. This avoids conditional
    logic throughout the pipeline.

34. **Cross-project introspection via shared vault architecture:** The vault is
    a unified knowledge graph — all projects' session notes, task files,
    knowledge documents, and iteration histories coexist under
    `Projects/{project}/`. An AI agent working in project A can read structured
    history from project B by accessing the vault directly (session notes,
    `tasks/cancelled/`, `knowledge.md`), through `vv inject` (which assembles
    context for the current project from the shared index), or through MCP tools
    (`vv_list_projects`, `vv_search_sessions`, `vv_get_project_context`) which
    query across all projects. This emerges naturally from the vault-centric
    architecture rather than requiring explicit cross-project wiring. The agent
    never needs to parse Claude Code's internal JSONL transcripts — everything
    is pre-structured markdown in the vault.

35. **Cancelled plan preservation as institutional memory:** Tasks investigated
    and found not worth implementing are moved to `tasks/cancelled/` (not
    deleted) with a cancellation rationale section, and a pointer is added to
    `knowledge.md`. This serves two purposes: (1) prevents future AI sessions
    from re-proposing the same work, and (2) preserves the analysis for
    reference when conditions change. The `/cancel-plan` slash command
    orchestrates this workflow — disambiguation, rationale drafting, file
    updates, and cross-reference creation.

36. **Context effectiveness as self-measuring infrastructure:** `vv effectiveness`
    correlates context depth (number of prior sessions available via inject) with
    session outcomes (friction score, corrections, duration). Sessions are grouped
    into cohorts by context depth (none, early, building, mature) and compared
    using Pearson correlation. `vv reprocess --backfill-context` retroactively
    populates `ContextAvailable` data on historical sessions, creating a natural
    before/after dataset. This makes the context pipeline self-measuring — the
    tool can quantify whether its own context injection improves AI session
    quality.

37. **MCP server as stdio JSON-RPC gateway:** `vv mcp` serves 20 tools + 1
    prompt over stdin/stdout JSON-RPC 2.0. All tool names use `vv_` prefix to
    avoid namespace collisions. Install paths differ by editor: Claude Code
    writes to `~/.claude/settings.json` `mcpServers`, Zed writes to
    `~/.config/zed/settings.json` `context_servers`. The server is stateless —
    each tool call loads the index fresh.

38. **Project identity via `.vibe-vault.toml`:** A `.vibe-vault.toml` file in
    a repo root overrides git-remote-based project detection with explicit name,
    domain, and tags. This handles repos where `origin` doesn't exist, points at
    a fork, or where the git-derived name is unhelpful. `identity.Detect()`
    checks for the file first, falls back to `session.Detect()`.

39. **Dynamic context injection via `vv inject`:** `inject.Build()` assembles
    context from the session index — recent sessions, open threads, decisions,
    friction trends — into a priority-ranked section list. `inject.Render()`
    applies a configurable token budget, dropping lowest-priority sections first.
    Used by `vv inject` CLI, `vv_get_project_context` MCP tool, and the
    PreCompact hook handler (which pipes inject output to stdout for Claude Code
    to ingest before context compaction).

40. **Zed hybrid capture — explicit MCP tool + automatic SQLite watcher:**
    `vv_capture_session` MCP tool accepts agent-curated summaries (explicit
    path). A background goroutine in the MCP server watches `threads.db-wal` via
    fsnotify, auto-capturing after a configurable debounce (default 5 min) when
    agents forget to call the tool (automatic path). Explicit captures take
    precedence; automatic captures get `status: auto-captured`. Controlled by
    `[zed] auto_capture` config (default true).

41. **Three-way baseline template sync:** `vv context sync` uses `.baseline`
    files to track what was last synced to each project. Three-way comparison
    (template vs baseline vs project file) distinguishes user customizations
    from stale templates. Unchanged files auto-update; user edits are preserved
    unless `--force` overrides conflicts. `.pinned` markers are always respected.
    Vault templates are refreshed from Go embeds on every sync (schema v8).

42. **MCP context gateway — read/write/bootstrap tools for agent-managed
    context.** 9 new tools added across three phases: read tools
    (`vv_get_workflow`, `vv_get_resume`, `vv_list_tasks`, `vv_get_task`), write
    tools (`vv_update_resume`, `vv_append_iteration`, `vv_manage_task`,
    `vv_refresh_index`), and a bootstrap tool (`vv_bootstrap_context`). Design
    principles: composable single-purpose tools, vault as single source of
    truth, human-editable markdown preserved throughout. The bootstrap tool
    enables single-call session start — one invocation returns resume, workflow,
    and active tasks — replacing the multi-file bootstrap chain for MCP-capable
    agents.

43. **Vault git sync with file-classification-driven conflict resolution:**
    The vault is a git repo shared across machines. `vv vault pull` and
    `vv vault push` automate synchronization at session boundaries. Conflict
    resolution during rebase is driven by file classification: auto-generated
    files (`history.md`, `session-index.json`) accept upstream and are
    regenerated locally via `vv index`; session notes (unique per-machine
    timestamps) have near-zero conflict risk; manual files (`knowledge.md`,
    `resume.md`, tasks) accept upstream but are reported for human review.
    Push commits with a hostname stamp and retries once via pull-rebase on
    rejection. On final failure, the error is surfaced for interactive
    resolution — the design avoids formulaic retry loops in favor of
    collaborative human-machine debugging. vv owns the vault repo completely
    (full git privileges) but never touches project source code repos.

44. **Session synthesis as end-of-session judgment layer:** After session
    capture writes the note, `synthesis.Run()` gathers the session note, git
    diff (8KB cap), current knowledge.md and resume.md (12KB each), recent
    history (last 5 sessions), and active tasks — then asks the LLM to
    identify novel learnings, stale entries, resume updates, and task
    completions. The four-stage pipeline (gather → synthesize → apply) runs
    before context refresh so `history.md` reflects updated knowledge.
    `Apply()` uses significant-word overlap (≥2 matching 4+ char non-stop
    words) to prevent duplicate learnings, a two-stage matcher (index-based +
    fuzzy text fallback) for stale entry flagging, and heading-targeted
    markdown editing for resume updates. Completed tasks are moved to `done/`.
    Enabled by default but inert without an LLM provider — synthesis
    piggybacks on the enrichment provider configuration. Disabled explicitly
    via `[synthesis] enabled = false`. The LLM call uses temperature 0.3 with
    JSON mode and a 15-second default timeout. Response validation filters
    invalid section names, out-of-bounds indices, and unknown task actions
    before any file modifications occur.

45. **Shared markdown utilities via `internal/mdutil`:** Five functions
    extracted from duplicate implementations across synthesis, MCP write
    tools, and index context generation. `SignificantWords()` extracts 4+
    char words with 50+ stop words filtered — used for duplicate detection
    and fuzzy matching. `ReplaceSectionBody()` finds a `## Heading` and
    replaces its body until the next heading or EOF. `AtomicWriteFile()`
    writes via temp file + rename for crash safety. `Overlap()` and
    `SetIntersection()` provide word-set operations. All MCP write tools
    (`vv_update_resume`, `vv_append_iteration`, `vv_manage_task`) now use
    `mdutil.AtomicWriteFile()` for safe writes and `mdutil.ReplaceSectionBody()`
    for heading-targeted edits.

46. **Three-tier template cascade with seed-once semantics:** Template
    content flows through three tiers during `vv context sync`:

    **Tier 1 — Embedded binary** (`templates/agentctx/**`, compiled into `vv`
    via `//go:embed`): The single source of truth. `forceUpdateVaultTemplates()`
    overwrites Tier 2 on every `vv context sync`, ensuring vault templates
    always match the current binary. `EnsureVaultTemplates()` (safeWrite,
    seed-only) is still used by `Init()` for first-time project setup.

    **Tier 2 — Vault `Templates/agentctx/`**: Refreshed from Go embeds on
    every `vv context sync`. `propagateSharedSubdir()` reads from here and
    uses three-way baseline comparison against per-project copies. Template
    changes auto-propagate to untouched files; user-customized files are
    preserved unless `--force` overrides conflicts.

    **Tier 3 — `Projects/<project>/agentctx/`**: Per-project deployed copies
    with `.baseline` tracking. Files with `.pinned` markers are exempt from
    propagation. This tier is what agents actually read at runtime.

    Consequence: after upgrading `vv`, `vv context sync` auto-updates all
    template-derived files that haven't been manually edited. Customized
    files are preserved (CUSTOMIZED or CONFLICT action) unless `--force` is
    used. `.pinned` markers permanently opt out of propagation.

47. **Future direction: vault Templates layer reassessment.** With schema v8,
    the vault `Templates/agentctx/` directory serves primarily as a
    pass-through cache: Go embeds overwrite it on every sync, then
    `propagateSharedSubdir()` reads from it to propagate to projects. The
    three-way `.baseline` tracking at the project level now handles the core
    problem the vault layer was designed for — distinguishing user edits from
    stale templates.

    **Two competing workflows drive the current three-tier design:**

    *Developer workflow (build from source):* Embeds are edited directly in
    `templates/agentctx/`, compiled into the binary, and should flow outward
    immediately. The vault Templates copy is always stale the moment source
    templates change. For this workflow, vault Templates are pure overhead.

    *End-user workflow (binary release):* Embeds are frozen at install time.
    Vault Templates are the only place to customize what propagates to all
    projects — e.g., adding a team-specific `/deploy` command or editing
    `/wrap` for a different git workflow. Without this layer, per-project
    customization (via `.baseline` conflict detection) still works, but
    there's no "customize once, propagate everywhere" mechanism.

    **The remaining unique capability of vault Templates** is user-created
    files that don't exist in Go embeds. `forceUpdateVaultTemplates()` only
    writes files present in the embed filesystem — it never deletes user
    additions. A custom `Templates/agentctx/commands/deploy.md` would persist
    across upgrades and propagate to all projects on sync.

    **Possible future simplifications:**

    - *Two-tier model:* Propagate directly from embeds to project agentctx,
      eliminating the vault Templates directory entirely. Simplest, but loses
      the "custom templates for all projects" capability. `.baseline` tracking
      at the project level still handles upgrade conflicts correctly.

    - *Custom overlay directory:* Introduce `Templates/agentctx/custom/` (or
      similar) for user-created templates that `vv` never overwrites. The rest
      of `Templates/agentctx/` becomes a true cache with no user-facing role.
      Cleanly separates vv-managed content from user-managed content.

    - *Status quo:* Keep three tiers. Low-cost overhead (one extra directory
      write per sync). The vault is under git, so the Templates directory has
      full history and three-way merge capability via the vault's own source
      control. No urgency to simplify until real-world usage patterns clarify
      whether anyone uses vault-level template customization.

    **Current recommendation:** Leave as-is. Document that vault Templates
    exist as a propagation cache plus optional customization overlay, but Go
    embeds are the source of truth for vv-shipped content. Revisit if users
    report confusion about the three-tier model or request better custom
    template support.

48. **Auto-memory symlink from Claude Code into the vault:** `vv memory link`
    establishes `~/.claude/projects/{slug}/memory/` as a symlink into
    `Projects/{name}/agentctx/memory/` inside the vault. Once linked, Claude
    Code's native auto-memory writes land on vault disk transparently, and
    the existing vault git sync propagates memory across machines — no
    sidecar process, no dedicated sync daemon.

    - **Slug computation:** Absolute working-dir path with `/` replaced by
      `-`, after `filepath.EvalSymlinks` resolves any symlinked cwd. Without
      the symlink resolution, `~/code/proj` and a `~/work/alias-proj -> ~/code/proj`
      symlink would produce two distinct slugs for one project, each pointing
      at a different vault target — a silent split-brain. Trailing slashes
      on `--working-dir` are trimmed before conversion.

    - **Project name resolution via `session.DetectProject`:** Memory link
      routes through the same identity-file → git-remote → basename chain
      that session capture uses. Basename-only mapping was rejected: a
      project cloned into a directory whose name diverges from its git
      remote or `.vibe-vault.toml` identity would get a different vault
      target than the rest of `vv` — an entire class of drift bugs for
      zero implementation benefit. Consistency with the rest of the
      binary is a hard requirement.

    - **Conflict-directory placement is a sibling, not a child:** When
      `--force` migration quarantines a host-local file whose content
      diverges from the vault copy, the quarantined copy lands in
      `Projects/{name}/agentctx/memory-conflicts/{timestamp}/`, explicitly
      *outside* the linked memory directory. Claude Code actively reads
      the memory directory at bootstrap; a conflict artifact inside it
      would pollute auto-memory output on every session start. Keeping
      conflicts as a sibling means the quarantined file is still visible
      to humans reviewing the vault but never leaks back into the agent's
      context window.

    - **Scope gate:** Link refuses unless `Projects/{name}/agentctx/` already
      exists (the implicit `vv init` marker). Scratch/experimental Claude
      projects stay host-local until a human opts them in — mirroring every
      session under `~/.claude/projects/` would fill the vault with noise.

    - **Unlink is rollback-only:** `vv memory unlink` removes the symlink,
      restores a real directory, and copies every vault-side file into it.
      The vault copy is preserved as the durable store. Normal usage is
      link-only; unlink exists so rollback is always well-defined rather
      than requiring manual `rm` + `cp`.

49. **Cross-project learnings as on-demand MCP tools, not inline in
    bootstrap.** `VibeVault/Knowledge/learnings/*.md` holds observations
    that apply across projects (testing philosophy, resume phrasing
    rules, feedback patterns). The natural place to surface them is
    `vv_bootstrap_context` — but inlining full learning content there
    would blow past the /restart 20–30K token budget as soon as the
    directory accumulates a handful of entries. Two tools separate the
    decision to load from the cost of loading:

    - **`vv_list_learnings`** returns frontmatter only (slug, name,
      description, type), ~20–50 tokens per entry. Cheap enough to call
      during planning so the agent can decide which specific learnings
      matter for the current task.
    - **`vv_get_learning(slug)`** returns the full body. Called only
      when the list already identified a relevant entry, so the
      expensive payload flows exactly when it would be read — never
      eagerly.
    - **`vv_bootstrap_context`** emits a single
      `knowledge_learnings_available: {count, hint}` field, present
      **only** when the directory has ≥1 valid file. Zero learnings →
      zero tokens; populated directory → ~25 tokens pointing the agent
      at the two dedicated tools. The field's mere presence doubles as
      a capability signal for agents that might otherwise not know the
      cross-project surface exists.

    The `type` constraint (`user | feedback | reference`; `project`
    rejected) enforces the directory's semantic boundary at parse time.
    Silently accepting `type: project` would leak project-scoped
    memories into a cross-project list, producing confusing output that
    the agent has no reliable way to filter. Malformed frontmatter is
    skipped with a stderr warning rather than surfaced inline in the
    JSON response — consumer code stays uniform, and vault-side data
    hygiene issues land on the operator instead of the agent.

50. **Marker-delimited block injection (v8→v9) — retired iter 145.**
    The `migrate8to9` mechanism for injecting a "Data workflow" block
    into per-project resume.md was removed once all operator vaults
    reached v10. Historical detail in iterations.md.

51. **Shipped-capability descriptions live in `agentctx/features.md`,
    not `resume.md`.** `resume.md` is loaded into every session's
    bootstrap context and was accumulating prose paragraphs describing
    every shipped feature — content duplicated in `iterations.md`
    narratives, commit messages, and `doc/DESIGN.md`. Iter 119's
    bootstrap-payload measurement showed `resume.md` at ~18.4 KB with
    roughly ~10 KB of that being the "Current State" feature prose
    alone.

    **The split.** `resume.md`'s Current State now holds only evergreen
    invariants (test count, iteration count, schema version, module
    path, MCP tool count, embedded template count). Shipped-capability
    descriptions go to a new `agentctx/features.md` with themed
    headings (Template cascade / Memory & knowledge / MCP server /
    Vault operations / Workflow & discipline / Code hygiene). Each
    features.md entry is 1–2 sentences pointing at a package or file
    and the iteration that introduced it.

    **Why features.md is not in `doc/`.** `doc/` carries source-
    controlled implementation documentation that ships alongside the
    code (architecture, design rationale, test inventory). Shipped
    capabilities are vault project history — they evolve session by
    session, are project-scoped (not protocol-scoped), and belong with
    `iterations.md` in `agentctx/`. A project reading from a packaged
    binary without the vault context still has `doc/` from the repo
    clone; features.md is the vault's equivalent.

    **Why not auto-generate from `iterations.md`.** Tempting, but
    iteration narratives are verbose and describe *changes* to
    capabilities rather than the capabilities themselves. The manual
    index can group multiple iterations under one entry (e.g., "MCP
    server — 20 tools + 1 prompt, iters 95–121") and can describe
    what the feature *is* now, not how it evolved. An auto-generated
    version would force either a 1:1 iteration-to-feature mapping
    (lossy) or a parse of iteration narratives into feature
    descriptions (fragile).

    **`/wrap` routing.** When a new capability ships, `/wrap` now adds
    (or updates) an entry in `agentctx/features.md` under the
    appropriate section heading rather than accumulating prose in
    `resume.md`. `resume.md`'s Current State bullets still get
    refreshed with new counts (tests, iterations, MCP tools), but no
    longer accept narrative.

    **Deferred: schema v9→v10 migration.** Automatically splitting
    other projects' existing resume.md Current State into a new
    features.md lands with schema v10. Phase 2b shipped the manual
    version for this project only (A3 variant). The migration
    (`features-md-schema-migration` task) follows the iter 116
    v8→v9 pattern — marker-delimited injection, `.baseline` tracking,
    `.pinned` / suppression opt-outs — and will run automatically on
    the next `vv context sync` once implemented. Payload savings: for
    this project, ~7.4 KB per `vv_bootstrap_context` call. For other
    projects, proportional to however much Current State prose has
    accumulated.

52. **`vv context` subcommand guardrails and top-level file sync.**
    Two sync-side correctness bugs closed together in iter 122.

    **Bug 1: Marker check.** Before iter 122 `vv context
    {init,sync,migrate}` ran in any directory, including empty
    tmpdirs with no `.vibe-vault.toml`. Accidental invocations in
    the wrong directory silently created vault Projects/ entries
    under basename heuristics or pushed cwd-derived `.claude/`
    content into unrelated repos. `internal/identity/FindMarker`
    walks up the directory tree looking for `.vibe-vault.toml`,
    returning typed `ErrNoProjectMarker` on miss. Dispatch semantics:
    `sync` hard-fails without a marker (skipped when `--all` — that
    flag is an explicit opt-out operating on vault state independent
    of cwd); `migrate` hard-fails unless a marker exists OR an
    explicit `--project` flag is supplied (migrate legitimately
    bootstraps the marker as part of its action, so requiring one
    up-front would break the core legacy-bootstrap use case — the
    `--project` flag is itself an intent signal); `init` prompts
    `[y/N]` when an ancestor (not cwd itself) has a marker, bypassed
    by `-y`/`--yes` for scripted use.

    **Bug 2: workflow.md never participated in sync.** Pre-iter-122
    `syncProject()` propagated only the `propagateDirs = {"commands",
    "skills"}` subdirectories. Top-level `agentctx/` files
    (`workflow.md`, `resume.md`, `iterations.md`, `features.md`)
    were written once by `Init()` from templates and then never
    touched by sync. Consequence: iter 121's Phase 2d compression
    of `workflow.md` (Tier 1 + Tier 2) never reached any existing
    project — three-way tracking couldn't reconcile a file it never
    scanned. The iter 121 narrative had flagged this edge case
    explicitly ("this project's Tier 3 has never had a
    `workflow.md.baseline` file, so it's never been three-way-
    tracked").

    **Fix shape.** Extracted the inner three-way comparison body of
    `propagateSharedSubdir` into a new `propagateFile()` helper.
    Added `propagateTopLevel(vaultPath, agentctxPath, dryRun, force)`
    keyed off a package-level `topLevelFiles = []string{"workflow.md"}`
    constant — iterates the list, computes src/dst paths, delegates
    each file to the shared helper. Wired into `syncProject` after
    the existing `propagateSharedSubdir` loop. `propagateFile` now
    also takes a `TemplateVars` argument and applies
    `applyVars()` to the template bytes before any comparison or
    write, so `{{PROJECT}}` / `{{DATE}}` tokens no longer leak into
    baselines. `varsForAgentctx(agentctxPath)` derives the project
    name from the `<vault>/Projects/<name>/agentctx` path convention.

    **Why not add `resume.md`, `iterations.md`, `features.md` to
    `topLevelFiles`?** Those three are per-project state, not
    template-driven. `iterations.md` is append-only project history;
    `features.md` is a project-scoped capability index; `resume.md`
    carries per-project current-state invariants whose general
    cross-project propagation is the explicit scope of the
    `features-md-schema-migration` task (schema v9→v10). Scoping
    `topLevelFiles` to `workflow.md` today keeps the iter 122 change
    minimal; the list is deliberately open for future additions.

    **Regression caught during live verification.** The first force-
    sync after the refactor wrote unsubstituted template content —
    `# {{PROJECT}} — Workflow` instead of `# vibe-vault — Workflow`.
    Root cause: the extracted `propagateFile()` copied raw template
    bytes with no `applyVars()` pass. The pre-refactor
    `propagateSharedSubdir` had the same gap but never hit it
    because no commands-subdir file uses template tokens. Fixed in
    the same task by threading `TemplateVars` through the
    propagation pipeline; pinned with
    `TestPropagateTopLevel_SubstitutesProjectToken`.

    **User-facing recipe.** On projects that predate top-level sync:
    (a) first `vv context sync --project <name>` reports
    `CUSTOMIZED [vault] workflow.md` if the project file differs
    from the template (no baseline exists yet, so sync treats it as
    user-owned); (b) to accept the compressed template, rerun as
    `vv context sync --force --project <name>` — emits `UPDATE` and
    seeds `workflow.md.baseline`. Payload savings realized on this
    project: ~714 B from compression plus proper substitution.

53. **Provenance metadata is stamped algorithmically at write time in
    Go, not passed as MCP arguments.** Iter 136's three-pass
    bisection of `integration-test-harness-vault-leak` couldn't
    distinguish test-caused from operator-caused vault pollution
    because no provenance metadata survived in any write. Git's
    committer field is too coarse: a single `vv vault push` bundles
    writes from many `vv` invocations across potentially multiple
    machines, so the commit author identifies who synced, not who
    produced each individual session note or iteration block.

    **Decision.** `internal/meta/Stamp()` resolves `host` (via
    `$VIBE_VAULT_HOSTNAME` or `os.Hostname()`) and `user` (via
    `$USER` → `$LOGNAME` → `user.Current()`) at write time in the Go
    binary. The values flow into `render.NoteData.Host`/`.User` for
    session notes (emitted in YAML frontmatter before `summary:`) and
    into an HTML-comment trailer appended to each iteration block.
    MCP tool schemas do **not** gain `host`/`user`/`cwd` parameters —
    agents stay oblivious and the feature is algorithmic to how
    `vv capture` and `vv_append_iteration` work.

    **Rationale.** Three goals align: (1) keep agents oblivious — no
    per-call boilerplate for callers to pass context metadata, and
    no way for a buggy or misbehaving agent to forge a fake host;
    (2) don't burn context tokens — the feature adds zero input
    surface to MCP tools, so the bootstrap payload and every
    subsequent call stay the same size; (3) single convergence
    points — `session.CaptureFromParsed` is the one function all
    three session-capture paths (MCP `vv_capture_session`, hook
    Stop/SessionEnd, zed-reprocess) route through, and
    `vv_append_iteration`'s block-assembly step is the single site
    for iteration provenance. One stamp call per convergence point
    covers every call path upstream.

    **Phase 6 amendment — `cwd` + `origin_project` ship as process
    cwd at write time.** The original Phase 5 deferral argued that
    the MCP-path `os.Getwd()` ("where did the server run") and the
    hook-path JSON-supplied cwd ("where did the session start")
    answered different forensic questions and risked encoding that
    ambiguity under a single `cwd:` key. The 2026-04-24 `/review-plan`
    session reversed that framing: under real Claude Code + MCP +
    hook semantics, both paths resolve to a cwd that represents the
    originating session. The hook subprocess inherits cwd from
    Claude Code (the user's work cwd in practice), and the MCP server
    runs in whichever directory Claude Code was launched from. Both
    answer the same question — *"which project's session produced
    this write?"* — so one field with one meaning covers both paths.
    `meta.Stamp().CWD` therefore resolves to `os.Getwd()` of the
    acting process (Option A from the review; Option B threading
    `opts.CWD` through the call chain would produce identical output
    under normal operation for a signature change with zero gain).

    **Decision (Phase 6).** Session notes gain a `cwd:` line in YAML
    frontmatter immediately after `user:` and before `summary:`, and
    iteration-block trailers gain space-separated `cwd=C origin=P`
    tokens after the existing `host=H user=U` pair — full trailer
    shape `<!-- recorded: host=H user=U cwd=C origin=P -->`. Each
    token is conditional: empty values are omitted entirely rather
    than emitted as `cwd= ` or `origin= `. `origin_project` is
    computed at stamp time via `session.DetectProject` against the
    stamped cwd and emitted alongside in both sites, giving
    machine-readable cross-project attribution without forcing
    future consumers to re-derive the project from a cwd string.
    For session notes `origin_project` usually equals the target
    `project:` field (session-note project is itself cwd-derived);
    for MCP iteration-block appends the two routinely differ — the
    target project is the explicit `project` argument to
    `vv_append_iteration`, and the origin project is the server's
    cwd-derived project. That divergence is the forensic payload.

    **Cross-project workflow driver.** The motivating pattern is
    the operator discovering an issue in project A (e.g., `recmeet`,
    `rezbldr`) during an A-rooted session and reaching across to
    modify project B (typically `vibe-vault` embedded templates or
    workflow docs) without `cd`-ing out of A. The resulting vault
    writes land in B's subtree but forensically belong to an
    A-originating session. Without `cwd` + `origin_project`, `host`
    + `user` alone cannot distinguish "vibe-vault write from a
    vibe-vault session" from "vibe-vault write from a recmeet
    session" — every edit from the same workstation looks identical
    in the metadata. The Phase 6 stamp closes that gap and leaves a
    greppable trail without bisecting git history.

    **Privacy.** `internal/meta/sanitize.go`'s `SanitizeCWDForEmit`
    is applied at both stamp sites: (1) if the resolved cwd is
    inside `cfg.VaultPath`, emit empty (the target path already
    appears in the `project:` field, so a vault-rooted cwd is
    noise); (2) otherwise `sanitize.CompressHome` strips the
    `/home/<user>/` prefix to `~/...`, keeping the
    project-identifying tail as the forensic payload; (3)
    trailer-unsafe byte sequences (`-->`, `\n`) are neutralized
    (`-->` → `--`, newline → space) so a crafted cwd cannot truncate
    the `provenanceTrailerRE` match and leak bytes back into parsed
    narrative. Linux `os.Getwd()` in practice never returns such
    paths, but write-side sanitization means the parser regex at
    `internal/mcp/tools_iterations.go:29` stays unchanged.

    **Testability.** `os.Hostname()` calls `uname(2)` and cannot be
    overridden via `$HOSTNAME`, so a package-level `hostnameFunc`
    var provides a test seam — unit tests swap it out;
    `$VIBE_VAULT_HOSTNAME` gives operators a production override
    via the same code path the tests exercise. Phase 6 mirrors that
    pattern exactly: a package-level `cwdFunc = os.Getwd` with a
    `$VIBE_VAULT_CWD` env override checked first in `cwd()`. One
    precedent, two fields, one test-seam mechanism. Integration
    tests use `VIBE_VAULT_HOSTNAME=vibe-vault-test` and
    `VIBE_VAULT_CWD=/vibe-vault-test-cwd` as sentinels that the
    extended `no_real_vault_mutation` canary greps for across every
    written note and iteration block — belt-and-suspenders insurance
    beyond the existing mtime/sha snapshot. A stray write with an
    unexpected real hostname or cwd now fails the canary
    immediately, regardless of whether the snapshot happens to
    match.

54. **`meta.ProjectRoot()` per-level check order — `agentctx/` before
    `.git/`:** `ProjectRoot(cwd, vaultPath)` walks upward from `cwd`,
    and at each directory level it checks for `agentctx/` before
    checking for `.git/`. The first directory containing either marker
    is the project root.

    **Rationale.** Vault-only projects (no git repo) have an `agentctx/`
    directory but no `.git/`. Checking `.git/` first would skip those
    directories and walk past the project root. Checking `agentctx/`
    first makes the function work correctly for both git-backed and
    vault-only projects. The tie-breaking is irrelevant for projects
    that have both — the walk stops at the first level that matches
    either marker.

55. **`ErrIsVaultRoot` vault-root refusal:** `ProjectRoot` returns the
    sentinel error `ErrIsVaultRoot` when the matched directory equals
    the configured vault path (passed explicitly or read from
    `~/.config/vibe-vault/config.toml`). The caller decides what to
    do; wrap-related callers surface an actionable message and abort.

    **Rationale.** The vault is itself an Obsidian repo with an
    `agentctx/` directory. Without the guard, `ProjectRoot` would
    return the vault root when called from inside a vault-resident
    project that has no `agentctx/` of its own, or from the vault
    directory itself. That would silently write `commit.msg` and
    iteration blocks into the vault root — indistinguishable from a
    legitimate vault project but semantically wrong. The sentinel
    keeps the error visible and the logic in callers, not embedded as
    a magic path comparison in every consumer.

56. **Slug normalization rule — heading text up to first ` — `:**
    `NormalizeSubheadingSlug()` derives a stable identifier from a
    `### ` heading line by taking the text up to the first
    ` — ` (space–em-dash–space) separator, or the full text if no
    separator exists. Matching is exact-equal (case-sensitive, no
    whitespace normalization). The same rule applies in
    `ReplaceSubsectionBody`, `InsertSubsection`, and `RemoveSubsection`.

    **Rationale.** Headings often carry a topical key followed by a
    descriptive suffix separated by ` — ` (e.g., `### my-task — open
    since iter 140`). The topical key is the stable identifier; the
    suffix changes as status evolves. Locking the slug to the prefix
    means callers do not need to re-derive the full heading text when
    the description changes. No v11 schema migration is required — the
    rule is purely a parsing convention over existing headings.

57. **Liberal-on-read / strict-on-write carried bullet parser:**
    `ParseCarriedForward()` accepts multiple bullet forms on read —
    `- **slug**`, `- **slug:**`, `- **slug (note)**`, plain `- text` —
    without normalizing the source document. `EmitCarriedBullets()` and
    `BuildCarriedBullet()` always emit the canonical `- **slug**` form.

    **Rationale.** Requiring strictly-formatted input before a tool
    call works creates friction when the document was written by hand
    or by an older version of the tool. Liberal parsing tolerates
    variation without forcing a format-fix step. Strict emission means
    the document converges toward canonical form over time — each write
    normalizes the bullets it touches without a dedicated migration
    pass.

58. **No runtime drift gate between synthesize and apply:** When
    `vv_apply_wrap_bundle` computes apply-time SHA-256 fingerprints
    for each bundle field and finds them differing from the synth-time
    fingerprints embedded in the bundle, the divergence is logged to
    `wrap-metrics.jsonl` but does **not** abort the operation.

    **Rationale.** The AI is intended to edit the bundle between
    synthesize and apply. That editing is the point of the two-step
    flow — the AI reviews and improves the synthesized content. A
    drift gate would fire on every legitimate AI edit. Observability
    (logged drift, per-field byte counts, both SHAs in the metric
    record) provides forensic capability without introducing a
    "must match" invariant that the normal workflow would violate
    constantly.

59. **Host-local drift metric file at `~/.cache/vibe-vault/wrap-metrics.jsonl`:**
    Wrap metrics are written to a host-local cache path, not to the
    vault. The file rotates to `wrap-metrics-archive-YYYY.jsonl` at
    1000 lines.

    **Rationale.** The vault is a shared git repository. A vault-side
    JSONL file appended independently on multiple machines creates an
    append-race that requires `merge=union` gitattributes or a custom
    merge driver. Moving the file host-local eliminates the race at the
    cost of per-machine (rather than cross-machine) drift trends.
    Operators who want aggregated views can copy the files manually.

60. **`subject` REQUIRED in `vv_render_commit_msg`:** The tool's
    input schema marks `subject` as a required field. There is no
    auto-derivation from the convention file or git history.

    **Rationale.** The AI is the source of truth for subject-line
    semantics — it understands what changed in this session and can
    write a meaningful subject. Auto-derivation from convention files
    or branch names produces generic or wrong subjects. Requiring the
    AI to provide the subject explicitly keeps the responsibility
    where the signal is.

61. **`vv_capture_session` always present in synthesize bundle:**
    `vv_synthesize_wrap` unconditionally includes a `capture_session`
    field in the `WrapBundle`. Downstream consumers of
    `vv_apply_wrap_bundle` always call `vv_capture_session` as the
    final dispatch step.

    **Rationale.** A Phase 0 grep of the codebase confirmed that
    `vv_capture_session` is referenced in `index.go`, `context.go`,
    `mcp/tools.go`, and `prompts.go` — it is a core part of the
    session lifecycle, not optional. Omitting it from the bundle would
    require callers to handle capture separately, splitting the
    "one-call wrap" invariant. Making it unconditional keeps the
    dispatch contract simple.

62. **`project_path` REQUIRED and EXPLICIT in `vv_set_commit_msg`:**
    The tool does not internally detect the project path. The AI calls
    `vv_get_project_root` first to discover the path, then passes it
    explicitly to `vv_set_commit_msg`.

    **Rationale.** Explicit beats magic. Internal path detection would
    duplicate the logic in `meta.ProjectRoot()` and couple the tool to
    a detection heuristic that may not match what the AI already knows.
    The discovery tool exists precisely to answer "where is the project
    root?" — requiring the AI to call it first keeps each tool's
    responsibility narrow and makes the call chain auditable.

63. **Apply-bundle is fail-stop, not transactional:** `vv_apply_wrap_bundle`
    dispatches writes sequentially; on the first error it returns
    immediately with an `applied_writes` list of completed steps and an
    `error_at_step` field identifying where the failure occurred.
    Completed writes are not rolled back.

    **Rationale.** Each write in the bundle is semantically correct in
    isolation — an appended iteration block or an inserted thread entry
    has independent value even if the subsequent `commit.msg` write
    fails. Rolling back completed writes would mean removing valid vault
    state that the user can see and verify. Fail-stop with an explicit
    success/failure report gives the operator enough information to
    re-run the remaining steps manually if needed, without undoing work
    that succeeded.

64. **Vault root resolution via closure-captured `cfg.VaultPath`,
    not `meta.VaultRoot()`:** Each `vv_vault_*` tool constructor takes
    `cfg config.Config` and closure-captures `cfg.VaultPath`. Tool
    handlers join vault-relative inputs against that captured path
    directly. No `meta.VaultRoot()` indirection, no `vaultRootFunc`
    test seam.

    **Rationale.** Mirrors the iter-152 tool pattern (`tools_thread.go`,
    `tools_carried.go`, etc.), which closure-captures `cfg` for every
    handler. Adding a `meta.VaultRoot()` wrapper would introduce an
    abstraction with no MCP caller. Tests inject a temp vault by
    constructing `config.Config{VaultPath: tempPath}` and passing it
    to the constructor (D13); production callers go through the
    standard `config.Load()` path. The unexported
    `readVaultPathFromConfig` in `internal/meta/project_root.go` stays
    package-private; vault file accessors do not call it.

65. **`ValidateRelPath` rejection set is exhaustive at the input
    boundary:** Absolute paths (leading `/`), `..` segments after
    `filepath.Clean`, null bytes (`\x00`), control characters
    (`\x01-\x1f`, `\x7f`), the cleaned result `"."` (vault-root
    reference is incoherent for write/edit/delete), and the empty
    string are all rejected before the path is joined under the vault
    root. Windows-reserved names (`CON`, `PRN`, `AUX`, `NUL`, `COM1-9`,
    `LPT1-9`) are NOT validated in v1.

    **Rationale.** vibe-vault is Linux-primary and the vault is
    git-managed; cross-platform support is not a stated requirement.
    The Windows-name skip is documented in
    `internal/vaultfs/safety.go`'s package comment so a later
    cross-platform pass has an obvious extension point. The empty-string
    and `.` rejections close two attack-surface gaps the v2 review
    identified: an empty path would otherwise pass `filepath.Clean`
    unchanged, and a `.` would resolve to the vault root and let a
    write replace it wholesale.

66. **Symlink policy: realpath check via `filepath.EvalSymlinks`,
    `os.Lstat` first for `Exists`:** `ResolveSafePath` joins the
    relative path under the vault, then calls `filepath.EvalSymlinks`
    and verifies `strings.HasPrefix(realpath, absVault+sep)`.
    `vv_vault_exists` uses `os.Lstat` first to detect symlink presence
    (succeeds even for dangling links), then calls `EvalSymlinks` to
    verify resolvability — on `EvalSymlinks` error from a dangling
    target, `Exists` returns `{exists: false}` (the effective file is
    unreachable through the symlink).

    **Rationale.** `filepath.Abs` + `strings.HasPrefix` is not
    symlink-safe: a symlink under the vault pointing at `/etc/passwd`
    passes the prefix check but resolves outside the vault. Realpath
    resolution closes that escape. The Lstat-then-EvalSymlinks
    sequence in `Exists` lets callers distinguish "file is missing"
    from "symlink points to a missing target" without leaking
    information about non-vault paths. The auto-memory pattern
    (vault-side regular dir, host-side symlink INTO the vault) is
    unaffected by this check — the realpath of the vault-side dir
    stays inside the vault. The check matters for OTHER symlinks an
    operator might create under the vault that point outward.

67. **Read size cap is a NEW mechanism (1 MB default, 10 MB ceiling):**
    `vv_vault_read` and `vv_vault_sha256` cap byte transfer at 1 MB
    by default, settable up to 10 MB via the `max_bytes` argument.
    This is NOT derived from existing token-cap precedent (e.g.
    `vv_get_project_context` caps by tokens with default 4000); the
    new policy is documented in tool descriptions.

    **Rationale.** Token caps make sense for narrative content piped
    into LLM context windows; byte caps make sense for arbitrary file
    content where the AI does not necessarily ingest the whole payload
    into context. The two policies coexist by tool category. The 10 MB
    ceiling caps blast radius for misuse without forcing every read to
    chunk.

68. **Atomic writes delegate to `mdutil.AtomicWriteFile` with
    `perm = 0o644`:** `vaultfs.Write` (and the file-write paths inside
    `vaultfs.Edit`/`vaultfs.Move`) call
    `mdutil.AtomicWriteFile(path, data, 0o644)` — never duplicate the
    temp+rename pattern. The `0o644` perm matches all 24 existing call
    sites elsewhere in the codebase.

    **Rationale.** The atomic-write helper is correct and well-tested
    (`TestAtomicWriteFile_CreatesDir` and friends already cover parent-
    dir creation, overwrite, and crash safety in
    `internal/mdutil/mdutil_test.go`). Delegating eliminates the third
    copy of the temp+rename pattern. `0o644` is uniform across every
    other writer (`internal/synthesis/actions.go`, the per-tool MCP
    write surfaces, etc.); locking it here keeps on-disk mode bits
    consistent and removes a parameter no caller wants to set. The
    duplicate `atomicWriteCommitMsg` at
    `internal/mcp/tools_commit_msg.go` is intentionally NOT
    consolidated: its alternate caller writes to the project root —
    outside the vault scope `vaultfs.Write` covers — so the
    duplication is the correct trade-off given the dual-copy
    semantics. Not filed as a follow-up cleanup.

69. **Compare-and-set is OPTIONAL via `expected_sha256`:** `write`,
    `edit`, and `delete` accept an optional `expected_sha256` argument.
    When provided, the file's current SHA-256 must match or the
    operation aborts with `ErrShaConflict`. When omitted, the operation
    proceeds unconditionally.

    **Rationale.** First-write scenarios (creating a new file, seeding
    a notes file) have no prior SHA to compare against, and forcing
    the AI to read-then-write would burn tokens and round-trips for
    no safety gain. Making the field optional gives concurrent-write
    safety where it matters (subsequent edits) without first-write
    friction.

70. **Permissive top-level write mode, except `.git/`:** Any path
    under the vault root is allowed for write/create/delete EXCEPT
    paths whose segments match `.git` case-insensitively (per #71).
    New top-level directories (`Scratch/`, ad-hoc working dirs) are
    allowed; the operator reviews the diff via `/wrap` and the vault
    git commit.

    **Rationale.** The vault is the AI's primary work surface;
    locking it down to a hardcoded directory whitelist would force a
    schema change every time a new use case emerges. Operator review
    at the git-commit boundary is the right control point — the same
    point at which the vault syncs across machines.

71. **`.git/` segment refusal is case-insensitive and segment-equal,
    not substring:** `IsRefusedWritePath(p)` rejects iff any segment
    of `filepath.Clean(p)` matches `.git` under
    `strings.EqualFold(seg, ".git")`. `Projects/<p>/foo.git/bar` is
    ALLOWED (substring, not segment). `Projects/<p>/.git/foo`,
    `Projects/<p>/.GIT/foo`, and `.gIt/HEAD` are all REFUSED. Applies
    to `vv_vault_write`, `vv_vault_edit`, `vv_vault_delete`, and both
    sides of `vv_vault_move`.

    **Rationale.** Case-insensitivity is a cross-filesystem hazard
    guard: macOS/NTFS resolve `.GIT` and `.git` to the same directory,
    Linux ext4 differs but a Linux operator could mount a
    case-insensitive filesystem (FAT/exFAT, network mounts). The
    case-sensitive form would let a `.GIT/HEAD` write land
    successfully on Linux and then collide with the real `.git/HEAD`
    on macOS sync. Segment-equality (rather than substring) preserves
    legitimate names like `foo.git/bar` (clone directory naming
    convention).

72. **Implicit parent-directory creation comes free via
    `mdutil.AtomicWriteFile`:** `mdutil.AtomicWriteFile` already calls
    `os.MkdirAll` on the parent before the temp+rename. `vaultfs.Write`
    inherits this behavior via the D5 delegation; no additional logic
    in the vaultfs layer.

    **Rationale.** Forcing the AI to call `vv_vault_mkdir` (which
    doesn't exist) before every write would add round-trips for the
    common case of "create file under existing or new dir." Implicit
    creation handles 99% of cases. Listed as #74 (D9) in the v3 plan
    purely to prevent an implementer from adding redundant
    `os.MkdirAll` calls in `vaultfs/write.go`.

73. **`vv_vault_delete` deletes FILES only in v1:** Empty-directory
    delete returns an informative error suggesting that directory
    removal is not yet supported. Recursive directory delete is out of
    scope.

    **Rationale.** Recursive directory delete is high-blast-radius
    even with safety guards — a misvalidated path or a careless AI
    call could remove an entire project subtree before the operator
    notices. v1 stays file-only; if a real use case for empty-dir
    removal surfaces, it can be added with a narrow contract (empty
    only, single level, no recursion).

74. **No exec, pure file I/O:** `vaultfs` and `tools_vault.go` never
    invoke external commands. Git operations (commit, push) remain
    operator responsibility via `vv vault push` (or future tools).

    **Rationale.** Separation of concerns. The AI writes to the vault;
    the operator reviews and commits. Adding `git add`/`git commit`
    inside the write path would break the review boundary that makes
    operator control meaningful.

75. **Tool count assertion bumped from 31 to 39 with explicit
    `expectedTools` slice:** The integration test at
    `test/integration_test.go` enumerates every expected tool name in
    a slice; the bidirectional assertion (`missing expected tool` and
    `unexpected tool`) catches both omissions and name typos. The
    eight new `vv_vault_*` entries land on the slice in this PR.

    **Rationale.** A numeric `len(tools) != 39` check would silently
    pass if a tool was renamed or replaced. The explicit slice makes
    the contract checkable: a future PR that adds a tool must add the
    name explicitly, and a removal must drop it explicitly. Catches
    silent count drift across multi-PR feature branches.

76. **Test injection via `config.Config{VaultPath: tempPath}`, no
    runtime `vault_path` parameter:** Tests construct
    `config.Config{VaultPath: tempVaultPath}` and pass it to the tool
    constructor (`mcp.NewVaultReadTool(testCfg)`), matching the
    iter-152 integration pattern. Tools do NOT accept a caller-supplied
    `vault_path` argument in production. The existing integration
    mechanism via `VIBE_VAULT_HOME` still works for end-to-end
    subprocess tests; both approaches coexist.

    **Rationale.** A runtime `vault_path` argument on the tool surface
    would defeat the MCP-as-gatekeeper property: any caller could
    point the tool at any directory, bypassing the operator's
    configured vault. Constructor injection lets tests substitute a
    temp directory at construction time without exposing a hole on
    the production tool schema.

77. **Auto-memory writes via the generic accessor; no dedicated
    `vv_memory_*` tool group:** AI calls
    `vv_vault_write("Projects/<p>/agentctx/memory/<file>.md", ...)`
    against the canonical vault location. The host-side
    `~/.claude/projects/<slug>/memory/` is a symlink INTO that vault
    directory (created by `vv memory link`, see #48), so Claude Code's
    native auto-memory and the AI's MCP writes converge on the same
    physical files.

    **Setup precondition.** Each host requires `vv memory link
    <project>` once. Until that runs, the host-side path is a regular
    directory (Claude Code's default). AI writes via vault-relative
    paths land in the vault; native auto-memory writes land
    host-locally; the two diverge silently. The bootstrap workflow
    documents the precondition; `vv check` flags a missing symlink.

    **Rationale.** A `vv_memory_*` tool group would duplicate the
    generic accessor with a narrower path scope and add tool-list
    surface for no new capability. The symlink+generic-accessor
    convergence is simpler and reuses every safety guarantee
    (`.git`-segment refusal, realpath check, atomic write). The
    setup precondition is a one-time per-host operator action,
    well-bounded and visible.

78. **Tools register at the existing single registration site
    `cmd/vv/main.go:registerMCPTools`:** Eight
    `srv.RegisterTool(mcp.NewVault*Tool(cfg))` calls are appended to
    the same span as every other production MCP tool. New file
    `internal/mcp/tools_vault.go` defines the constructors.

    **Rationale.** A second registration site would bifurcate the
    tool surface and complicate every "what tools are loaded?" audit.
    One registration site, one ordering, one diff to read for tool
    visibility.

79. **Path validation is fresh-written in
    `internal/vaultfs/safety.go`, not borrowed from
    `vaultPrefixCheck`:** The existing `vaultPrefixCheck` in
    `internal/mcp/tools_context.go` is NOT reused. It uses
    `filepath.Abs` + `strings.HasPrefix` with no `EvalSymlinks` and
    is therefore not symlink-safe; its callers feed it
    already-validated absolute paths produced inside the MCP server,
    not raw user input. `validateTaskName` is cited as a design
    precedent for relpath rejection but not directly reused (it's
    task-name-specific).

    **Rationale.** Reusing a not-symlink-safe helper at the new
    boundary would import the bug into a new attack surface.
    Fresh-writing closes the gap and lets `safety.go` be the one
    place the realpath invariant is enforced. The two checks live
    side by side at different boundaries, with documentation in
    package comments calling out the difference.

80. **`vv_vault_list` default-hides `.git` entries case-insensitively:**
    When iterating `os.ReadDir` results, `vv_vault_list` filters out
    any entry whose name matches `.git` under
    `strings.EqualFold(name, ".git")`. Operators can enumerate `.git/`
    contents via Bash if they need to; the AI never sees `.git/`
    through the generic accessor.

    **Rationale.** Consistent with the write-side refusal in #71. If
    `.git/` is invisible to the AI's writers, it should also be
    invisible to the AI's listers — otherwise the AI sees entries it
    cannot inspect or modify, generating spurious "is this protected?"
    round-trips. Hiding by default keeps the AI's view of the vault
    coherent. Mandatory test: `TestList_HidesDotGit` plus a
    case-insensitive variant.
