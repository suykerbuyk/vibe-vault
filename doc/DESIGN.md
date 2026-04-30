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
    `vv_apply_wrap_bundle_by_handle` computes apply-time SHA-256 fingerprints
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
    `vv_synthesize_wrap_bundle` unconditionally includes a `capture_session`
    field in the `WrapBundle`. Downstream consumers of
    `vv_apply_wrap_bundle_by_handle` always call `vv_capture_session` as the
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

63. **Apply-bundle is fail-stop, not transactional:** `vv_apply_wrap_bundle_by_handle`
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

81. **Vault push convergence uses `--force-with-lease` keyed to the
    last-known-good SHA per prior remote; rebase failures abort and
    surface; convergence-rejected state surfaces in
    `PushResult.RemoteResults`. The `afterPushHook` test seam is the
    pattern for in-flight state injection in tests.**

    **Rationale.** Before this decision, `vaultsync.CommitAndPush`
    pushed remotes sequentially with a single pull-retry per remote.
    On a rejection the local branch was rebased onto the rejected
    remote and re-pushed *only to that remote* — leaving any prior
    accepting remote at the pre-rebase SHA. With N=2, this produced
    silent SHA divergence: github at X, vault at X', no machinery to
    converge. The high-value defect was not parallelism (N=2 caps the
    win at one round-trip) but the divergence itself.

    `--force-with-lease=refs/heads/<branch>:<expected-sha>` is the
    safe primitive: an atomic compare-and-swap that rejects if the
    remote ref has moved off `expected-sha`. Concurrent writers are
    caught and surfaced as
    `"convergence rejected (concurrent writer at <remote>): <err>"`
    in `PushResult.RemoteResults` rather than silently overwritten.
    Naked `--force` remains forbidden in this package — convergence
    is the only path that uses any form of force-push.

    Rebase failures call `git rebase --abort` and route to the
    per-remote error map rather than leaving HEAD polluted for the
    next remote in the loop. Fetch failures surface directly without
    masquerading as downstream rebase/push errors.
    `PushResult.CommitSHA` is refreshed to the post-loop HEAD if any
    rebase happened, so the CLI never prints a SHA that no longer
    exists at the converged remotes. One `log.Printf` breadcrumb on
    each successful force-convergence makes divergence-path frequency
    operator-observable in production without committing to a
    `PushResult` shape change.

    The `afterPushHook = func(remote string) {}` package-level seam
    matches the `internal/wrapmetrics/writer.go:81 warnFunc`
    precedent: a no-op default that tests override with
    `t.Cleanup`-restored assignment to inject mid-flight state
    changes (e.g., a concurrent writer mutating a bare remote between
    the recorded push and the convergence force-with-lease) without
    exposing them in the production API. This is the canonical
    pattern in this codebase for testing race-window behavior at
    package-private granularity.

    Parallel push is deferred. Re-open conditions: a third remote
    appears, or measurement shows push exceeds 15% of wrap latency.

82. **Plugin `.mcp.json` writes `"command": "vv"` (PATH-relative), not an
    absolute binary path.** The Claude Code plugin's MCP config at
    `~/.local/share/vibe-vault/claude-plugin/vibe-vault/.mcp.json` (and
    its mirror in the harness cache) invokes `vv` via PATH lookup at
    session start, mirroring how `~/.claude/settings.json`
    `mcpServers.vibe-vault` is configured.

    **Rationale.** The prior implementation (`internal/plugin/generate.go`
    `resolveBinary()` helper) attempted `exec.LookPath("vv")` first and
    fell back to `os.Executable()`, then wrote the resolved absolute path
    into the plugin config. This produced a footgun: when the user had
    multiple `vv` binaries on the system (e.g., `~/.local/bin/vv` from
    `make install` and `~/code/go/bin/vv` from a stale `go install`),
    invoking the install via the stale binary directly caused
    `os.Executable()` to return the stale path, which then got pinned
    into the plugin config. The harness loaded that plugin config at
    session start and spawned the stale binary, exposing only its older
    tool surface — visible to the user as "MCP server tool surface lags
    running binary" even though `vv mcp check` against the canonical
    `~/.local/bin/vv` reported the correct tool count. The lag persisted
    through `make install` cycles because the plugin config itself was
    never rewritten.

    PATH lookup converges on whatever the operator's shell resolves `vv`
    to first, which is the canonical install in every supported setup.
    The cost is one PATH lookup at MCP server spawn time (microseconds);
    the benefit is automatic convergence on `make install` updates and
    elimination of the absolute-path-pinning failure mode.

    `resolveBinary()` was removed entirely; both `Generate()` (used by
    `vv mcp install --claude-plugin`) and `InstallToCache()` (used by
    the same command's cache-write path) now write `"command": "vv"`
    literally. Tests `TestGenerate_PathRelativeBinary` and
    `TestInstallToCache_PathRelativeBinary` enforce the literal value
    rather than the prior `filepath.IsAbs()` predicate.

    Re-open conditions: if a future Claude Code release passes a custom
    PATH to spawned MCP servers that omits the operator's vv install
    location, this decision would need to revert to absolute paths
    (with the stale-binary pinning addressed by some other mechanism,
    e.g., always invoking install via the freshly-built binary in
    `Makefile`).

83. **Reuse `internal/llm/` for agentic dispatch; sibling
    `AgenticProvider` interface alongside text-only `Provider`.**
    *Superseded by #92 (Direction-C). The executor-isolation defect
    documented in #92 retired the entire `AgenticProvider` /
    `AnthropicAgentic` / `RunTools` chain as dead code; the new
    render path consumes only the basic single-turn `Provider`
    interface. Retained for historical reference.* The
    pre-existing `internal/llm/Provider` interface (`internal/llm/types.go`)
    is text-only — single-turn `ChatCompletion(ctx, request) (Response, error)`
    with no tool-use plumbing. The `wrap-model-tiering` epic introduces a
    multi-turn tool-use dispatch loop for the wrap executor. Two options
    were considered for the new surface: (a) a parallel `internal/llmrouter/`
    package that owns the tool-use abstractions independently of the
    text-only path, and (b) a sibling interface inside the existing
    `internal/llm/` package that extends the text-only interface.

    **Rationale.** Chose (b). `AgenticProvider interface { Provider;
    RunTools(ctx, ToolsRequest) (ToolsResponse, error) }` lives in
    `internal/llm/types.go` next to `Provider`, so a single package owns
    every LLM provider abstraction in the codebase. Splitting the package
    would have duplicated retry, header, model-resolution, and base-URL
    plumbing across two trees with no compensating boundary — `RunTools`
    and `ChatCompletion` against the same vendor share API key, base URL,
    and HTTP client by construction.

    **C1-v2 fix (HTTP plumbing extraction).** Embedding alone produced a
    stutter where `Anthropic.do(req)` and `AnthropicAgentic.do(req)` were
    near-identical 40-line copies of header injection + retry +
    response-status branching. Decision 83 factors that into
    `internal/llm/anthropichttp.go` — `anthropicHTTPCore` carries the
    shared base URL, model name, API key, max-tokens, retry policy, and
    HTTP client; both `Anthropic` (text-only) and `AnthropicAgentic`
    (tool-use) embed `*anthropicHTTPCore` and reach common request
    plumbing through method promotion. The pattern anticipates v2
    multi-provider expansion: when the `openai-compatible` agentic
    dispatcher lands (serving OpenAI / Grok / Ollama / vLLM via one
    code path), a parallel `internal/llm/openaihttp.go` will carry the
    OpenAI-shaped core and serve both the existing `OpenAI` and a future
    `OpenAIAgentic`.

    **v1 scope.** Anthropic-only. `[wrap.tiers]` config validation rejects
    any `provider:` prefix other than `anthropic:` at config-load time.
    v2 (deferred) lifts that constraint behind an `openai-compatible`
    dispatcher.

    Source: `internal/llm/anthropichttp.go`, `internal/llm/anthropic_agentic.go`,
    `internal/llm/types.go` (Phase 1 commits `d27e795..28a3a04`).
    Re-open conditions: if v2 multi-provider lift surfaces a friction
    that the embedding pattern cannot cleanly express (e.g., shared retry
    semantics need provider-specific divergence), revisit the
    parallel-package option then.

84. **Two-stage /wrap dispatch with orchestrator quality gate; QC runs
    BEFORE apply (H3-v2). Architecture A1: dispatch loop server-side via
    `vv_wrap_dispatch` MCP tool; `internal/wrapdispatch/` runs in-process
    inside the MCP server.**
    *Superseded by #92 (Direction-C). The dispatch ladder was
    architecturally unable to produce the citation-rigor output its
    QC layer demanded — the executor was forbidden from fetching
    context yet QC required commit SHAs and per-paragraph citations.
    The retirement removed the entire `vv_wrap_dispatch` /
    `internal/wrapdispatch/` / wrap-executor agent / QC layer chain.
    Retained for historical reference.* /wrap was iter-157 inline-Opus — the
    orchestrator-side procedure direct-emitted iteration narrative,
    thread updates, carried bullets, commit message, and capture summary
    in a single Opus turn. Iter 161 baseline measurement showed 17 min
    wall-clock and ~60K AI-output tokens per /wrap (operator-confirmed).
    Decision 19's three-bucket gate placed the epic firmly in
    bucket (c) — projected wall-clock savings ≥2 min from a Sonnet swap
    on the executor warrants the full Phases 1-4 lift.

    **Architecture A1 (pinned in plan v5 per operator direction).** The
    dispatch loop runs server-side, in-process, inside the MCP server
    process via a new MCP tool
    `vv_wrap_dispatch(skeleton_handle, tier, agent_name, prior_attempts?)`.
    The orchestrator's `commands/wrap.md` invokes this tool exactly ONCE
    per tier; the handler returns proposed outputs (or an `escalate_reason`)
    plus dispatch metrics. The orchestrator never sees the per-LLM-tool-call
    fan-out — that machinery lives behind the MCP boundary.

    Why server-side: the alternative (A2) would have placed the loop in
    `commands/wrap.md` itself, requiring the orchestrator (Opus) to
    impersonate a tool-dispatch runtime. That doubles the Opus token
    cost (the orchestrator now reads every executor turn) and puts the
    breakable retry / escalation logic into prose markdown rather than
    typed Go code. A1 isolates the dispatch state machine in
    `internal/wrapdispatch/dispatch.go` where it is unit-testable with
    a mock `AgenticProvider` and a recorded fixture for each escalation
    branch.

    **H3-v2 sequencing.** Quality gate runs BEFORE apply, against
    proposed outputs in memory — NOT against vault state. A failed QC
    returns an `escalate_reason`; the orchestrator advances to the next
    tier with proposed outputs discarded. The vault is atomic from its
    perspective: either the bundle applies fully, or the vault is
    untouched. Under earlier sequencing (apply-then-QC) a failed gate
    would have left the vault partially mutated, and the next escalation
    tier would have re-applied on top, producing duplicate threads /
    carried bullets / iteration blocks. H3-v2 rules out that class.

    **H4-v3 demotion.** The terminal signal `wrap_executor_finish(status,
    reason?, outputs?)` is an in-loop tool spec passed via
    `ToolsRequest.Tools` — it is NOT a registered MCP tool. The dispatch
    handler's local `ToolExecutor` callback recognizes the call, captures
    the args into a Go variable, and returns. Treating it as a registered
    MCP tool would have leaked an internal handshake into the public tool
    surface and the integration-test exact-set assertion.

    **OQ-5 directive (synthesize routing).** When the executor invokes
    `vv_synthesize_wrap_bundle` from inside the dispatch loop, the
    handler's local `ToolExecutor` calls
    `internal/mcp/wrapbundle.go::FillBundle()` directly via Go function
    call — NOT via a re-entrant MCP roundtrip back through the server
    transport. The pattern follows the existing in-process dispatch
    precedent in `tools_apply_wrap_bundle.go` (peer handler bodies
    invoked directly; see DESIGN #50). Re-entrant MCP would have
    required the dispatch handler to construct JSON-RPC requests
    against its own server and parse the responses back out — pure
    overhead with no testability win.

    **OQ-6 directive (transport timeout).** stdio MCP transport
    accommodates 1-3 minute dispatch durations with no mitigation.
    The custom MCP implementation in `internal/mcp/server.go` has no
    per-request timeout; client (Claude Code harness) timeouts default
    to "indefinite while parent process is alive." The handler emits
    one stderr progress line per executor LLM tool-call so the
    operator sees a heartbeat during the wait
    (`[wrap-dispatch] tier=sonnet attempt=1 t=23s tool=vv_synthesize_wrap_bundle`).
    Future revisit if Claude Code introduces a hard MCP timeout — at
    that point the dispatcher would split into prepare/poll handles.

    Source: `internal/mcp/tools_wrap_dispatch.go`,
    `internal/wrapdispatch/dispatch.go`, `internal/mcp/wrapapply.go`
    (Phase 3c commits `407c320..f81e955`; the H3-v2 sequencing relies
    on Phase 3b's `vv_wrap_quality_check` from commits
    `4c645a6..e15c7cb`).

85. **MCP-served agent registry via `vv_get_agent_definition`;
    build-time generation of `.claude/agents/<name>.md` artifacts. BOTH
    the MCP tool AND the generated file are v2-portability scaffolding;
    v1's `vv_wrap_dispatch` handler reads the registry via direct Go
    call.**
    *Superseded by #92 (Direction-C). The `wrap-executor` agent
    template was deleted in Direction-C Phase 4 — its
    `forbidden_tools` list was the load-bearing source of the
    executor-isolation defect (see #92). The `internal/agentregistry/`
    package and the `vv_get_agent_definition` MCP tool survive as
    generic agent-registry scaffolding with no remaining wrap-flow
    consumer; the registry's `agents/` directory ships empty in
    Direction-C. Retained for historical reference.* The `wrap-model-tiering` epic introduces the `wrap-executor`
    agent definition — a system prompt plus tool whitelist plus
    escalation triggers plus output_format contract that drives the
    dispatch executor's behavior. Three distribution surfaces are shipped
    in v1, of which only one is consumed by v1's own dispatch path:

    - **In-binary embedded registry** (`internal/agentregistry/embedded.go`
      with `//go:embed agents/*.md`). The `wrap-executor.md` source of
      truth lives at `internal/agentregistry/agents/wrap-executor.md`
      and is read by v1's `vv_wrap_dispatch` handler via
      `agentregistry.Lookup("wrap-executor")` — a direct Go function
      call inside the MCP server process.
    - **Generated `.claude/agents/<name>.md` artifact** via
      `vv internal generate-agents` (target `make agents`, hooked into
      `make install`). For orchestrators that do NOT embed the
      vibe-vault binary — i.e., a v2 future where /wrap runs from a
      session host that cannot link `internal/agentregistry` directly.
    - **MCP tool `vv_get_agent_definition(name)`** as an alternative
      read path for orchestrators that DO speak MCP but lack direct Go
      access.

    **v1 NEVER consumes the MCP tool or the generated file.** H1-v3
    explicitly removed `vv_get_agent_definition` from
    `vv_wrap_dispatch`'s pre-flight assertion list, and the
    `commands/wrap.md` rewrite does not read
    `.claude/agents/wrap-executor.md`. An operator-facing comment at the
    top of the generated file declares that scope explicitly so a future
    reader does not assume v1 round-trips through it.

    **Why ship them in v1 anyway.** Defining the contract once upfront
    avoids retrofitting two distribution surfaces later when v2 lands —
    the registry sha256 stamping and frontmatter shape are settled in
    v1 against tests, so v2 only needs to wire the consumers. Cost is
    one new MCP tool entry on the surface (43 → 43 with this slot
    counted) and a regenerated artifact under `.claude/agents/`. The
    registry source-of-truth is `internal/agentregistry/agents/`, which
    keeps the MCP tool and the generated file byte-identical by
    construction.

    Source: `internal/agentregistry/{registry,embedded}.go`,
    `internal/agentregistry/agents/wrap-executor.md`,
    `internal/mcp/tools_agents.go`, `cmd/vv/internal.go`, generated
    `.claude/agents/wrap-executor.md` (Phase 2 commits
    `b196887..eda3baa`).

86. **Skeleton + prose split; orchestrator-facts cached host-local in
    `~/.cache/vibe-vault/wrap-bundles/iter-<N>-skeleton.json`; per-tier
    prose ephemeral; log-rotate keeps three most recent skeletons.**
    *Superseded by #92 (Direction-C). The skeleton + prose split was
    a consequence of the dispatch ladder's executor-isolation defect
    (#92): the executor needed a `WrapBundle` it could mutate without
    fetching context. Direction-C retires the bundle pipeline entirely;
    `vv_render_wrap_text` receives the project context bundle inline
    and returns prose directly. `internal/wrapbundlecache/` (and the
    `_legacy/` migration scaffolding from #91) retire with the
    pipeline. Retained for historical reference.* C2-v3 finding (plan v5 review): the original Phase 3 contract
    required the synthesizer to emit prose the executor had not yet
    generated, putting cart before horse. The bundle splits into two
    artifacts:

    - **Skeleton** — orchestrator-supplied facts only, no prose. Built
      once per /wrap via `vv_prepare_wrap_skeleton` (Phase 3a). Persisted
      to `~/.cache/vibe-vault/wrap-bundles/iter-<N>-skeleton.json` by
      `internal/wrapbundlecache/cache.go`. Survives MCP server restarts
      (consequential: a /wrap interrupted by a server crash does not
      need to re-collect orchestrator facts). Log-rotate keeps the
      three most recent skeletons; older skeletons are deleted on
      every cache write.
    - **Bundle** — skeleton plus executor-supplied prose
      (iteration_narrative, capture_session.summary, decisions, files,
      thread bodies, carried titles, commit-message body). Built
      in-memory via `vv_synthesize_wrap_bundle(skeleton_handle,
      prose-fields)` returning the full bundle. NOT cached — each
      tier's prose is ephemeral, and an escalation discards prior-tier
      prose entirely.

    **H2-v3 schema bump (thread-replace).** `WrapBundle` and
    `WrapSkeleton` carry `resume_threads_to_replace` / `ResumeThreadsReplace`
    for thread-REPLACE operations. Iter-159 added `vv_thread_replace`
    as a peer to `vv_thread_insert` / `vv_thread_remove`; the wrap
    pipeline now plumbs it end-to-end. Apply order extends to:
    `iter → thread_insert × N → thread_replace × R (NEW) → thread_remove
    × M → carried_add × P → carried_remove × Q → set_commit_msg × 1 →
    capture × 1`. The mutation-count check in
    `vv_wrap_quality_check` (DESIGN #87) and the count formula in
    `vv_apply_wrap_bundle_by_handle` were updated in lockstep — the
    formula reads `3 + N + R + M + P + Q` (the constant 3 covers
    iter + commit_msg + capture).

    **Compare-and-set guard via skeleton sha256.** The handle returned
    by `vv_prepare_wrap_skeleton` includes `skeleton_sha256`; subsequent
    calls (`vv_synthesize_wrap_bundle`, `vv_apply_wrap_bundle_by_handle`,
    `vv_wrap_quality_check`, `vv_wrap_dispatch`) verify the on-disk
    skeleton's sha matches the handle's. Mismatch returns `"skeleton
    cache file modified after handle issued"` and refuses the call.
    This protects against concurrent /wrap runs racing through the
    same iter, and against manual edits to the cache file between
    handle issuance and consumption. The check is structural across
    all four consumer tools — no path bypasses it.

    **Decision 23 fold (no shims).** The handle-based variants REPLACE
    the inline tools outright. `vv_synthesize_wrap` (singleton tool
    that read everything itself and returned a one-shot bundle) is
    renamed `vv_synthesize_wrap_bundle` and now requires a skeleton
    handle; `vv_apply_wrap_bundle` is renamed
    `vv_apply_wrap_bundle_by_handle` and likewise requires the handle.
    No backward-compat shims — orchestrators that called the old names
    get an explicit "tool not found" error and must update.
    `commands/wrap.md` was rewritten to call the handle-based names in
    the same epic.

    Source: `internal/wrapbundlecache/cache.go`,
    `internal/mcp/wrapbundle.go`, `internal/mcp/tools_prepare_skeleton.go`,
    `internal/mcp/tools_synthesize_wrap.go`,
    `internal/mcp/tools_apply_wrap_bundle.go` (Phase 3a commits
    `f5d96f5..6f2da4c`).

87. **Server-side quality gate runs four trigger checks against
    proposed outputs without touching vault state; the H3-v2 invariant
    is structural.**
    *Superseded by #92 (Direction-C). The QC layer was the gate that
    surfaced the executor-isolation defect: it demanded commit SHAs
    and per-paragraph citations the sandboxed executor could not
    produce. Direction-C retired the entire `vv_wrap_quality_check`
    tool and `internal/mcp/tools_quality_check.go` (517 lines).
    Of the four QC rules: `multi_match_ambiguity` was lifted into
    `mdutil.ReplaceSubsectionBody` and `mdutil.RemoveSubsection` as
    a hard error (#92); the other three rules dropped (citation
    accuracy is now the LLM's responsibility validated at operator
    review time, since the renderer receives proper context).
    Retained for historical reference.* Decision 8 of the wrap-model-tiering plan defines
    six escalation triggers. Four are owned by `vv_wrap_quality_check`
    (the tool); the other two — `mcp_tool_error_after_retry` and
    `missing_terminal_signal` — fire from the `internal/wrapdispatch`
    loop directly. The four QC triggers:

    1. **`multi_match_ambiguity`** — dry-run thread-and-carried lookup
       against live vault state. Multi-match for replace/remove
       operations (i.e., the slug resolves to ≥2 subsections) or a
       missing anchor for replace/remove fires the trigger. The check
       is read-only against `Projects/<name>/agentctx/resume.md`.
    2. **`mutation_count_mismatch`** — derived count from the skeleton
       (`3 + N + R + M + P + Q`, where the constant 3 is
       iter + commit_msg + capture; R is the H2-v3 thread-replace
       term) versus the actual count present in the filled bundle.
       Off-by-one in v3 review (C2-v2) was caught and corrected to
       include thread-replace. Tested by
       `TestVVWrapQualityCheck_DetectsMutationCountMismatch_CorrectFormulaIncludesThreadReplace`.
    3. **`semantic_presence_failure`** — every paragraph in
       `iteration_narrative` must cite at least one of {commit SHA,
       file path, function name, decision number}. The narrative as
       a whole must additionally include a commit-range SHA span
       (`a1b2c3d..e4f5a6b`-shape). M3-v3 file-path regex was
       empirically loosened from `/[a-zA-Z0-9_./-]+` (strict
       leading-slash) to a slash-bearing path with typical source
       extensions (`.go`, `.md`, `.toml`, `.json`, etc.) because the
       leading-slash form fails every existing iteration narrative
       in this project's vault. Tested by
       `TestSemanticPresenceFailures_FilePathRegexAcceptsNoLeadingSlash`.
    4. **`commit_subject_invalid`** — non-empty AND not in the
       rejection list `["WIP", "wip", "fix", "update", "change",
       "edit"]`. Empty subject fires too. The list is the project's
       conventional-commit gate (a non-anchored hint subject would
       defeat the convention).

    **H3-v2 invariant (NON-NEGOTIABLE).** QC reads vault state for the
    dry-run check but NEVER mutates it. `TestVVWrapQualityCheck_NoVaultMutation`
    (and the integration counterpart
    `TestIntegration_WrapQualityCheck_DetectsExpectedFailures_IncludingThreadReplace`)
    compare resume.md and iterations.md byte-for-byte before vs after
    a QC run; passing this test is structural to the H3-v2 contract,
    not optional. A future change that introduces any mutation under
    QC fails this test by construction.

    **Why before-apply.** Under earlier sequencing (apply-then-QC) a
    failed gate would leave the vault partially mutated; the next
    escalation tier would re-apply on top, producing duplicates. Under
    H3-v2 sequencing, proposed outputs sit in memory until QC passes;
    apply is atomic from the vault's perspective, and a failed gate
    leaves the vault untouched.

    Source: `internal/mcp/tools_quality_check.go`,
    `internal/mcp/tools_quality_check_test.go` (Phase 3b commits
    `4c645a6..e15c7cb`).

88. **Per-wrap dispatch metrics in parallel `wrap-dispatch.jsonl`
    artifact; existing per-field `wrap-metrics.jsonl` untouched.
    `[wrap]` config schema with H3-v3 Overlay map-merge fix; `vv stats
    wrap` aggregates both.**
    *Superseded by #92 (Direction-C). All three observable surfaces
    retired with the dispatch infrastructure: `wrap-dispatch.jsonl`
    (written only by `internal/wrapdispatch/`), `wrap-metrics.jsonl`
    (written only by `wrapapply.finishApply`), and the
    `vv stats wrap` CLI subcommand. `internal/wrapmetrics/` deleted.
    The `[wrap].escalation_ladder` config field also retired with
    a one-line stderr deprecation log on detection (#92, decision
    D5). `[wrap].tiers` and `[wrap].default_model` survive as
    `vv_render_wrap_text`'s tier→provider:model lookup table.
    Retained for historical reference.* C4 finding: the existing
    `wrap-metrics.jsonl` writer (`internal/wrapmetrics/writer.go`)
    carries per-field drift records (synth_sha256 vs apply_sha256 per
    bundle field). Per-wrap dispatch metrics (per-tier durations,
    escalation reasons, token usage) need a different record schema.
    Two options: (a) extend the existing `Line` struct with optional
    dispatch fields, (b) sibling jsonl with a separate schema.

    **Chose (b).** A new `~/.cache/vibe-vault/wrap-dispatch.jsonl`
    sibling to the existing per-field jsonl, with a `DispatchLine`
    record schema: `tier`, `provider_model`, `duration_ms`, `outcome`
    (`ok` / `escalate` / `error`), `expected_mutations`,
    `actual_mutations`, `input_tokens`, `output_tokens`,
    `escalate_reason`. The two writers are independent
    (`internal/wrapmetrics/dispatch_writer.go` does not import
    `writer.go`); `vv stats wrap` reads both files and renders two
    independent reports (median duration per tier, escalation rate,
    top reasons; AND median drift_bytes per field).

    **`[wrap]` config schema** (Decision 3 in the plan):
    - `default_model = "sonnet"` (operator's actual value; the default
      when the section is absent is `"opus"` to preserve iter-157
      behavior).
    - `escalation_ladder = ["sonnet", "opus"]`.
    - `[wrap.tiers]` map: tier name → `provider:model` string. v1
      enforces an `anthropic:` prefix at config validation time
      (`TestValidate_NonAnthropicProviderRejected`); v2 lifts the
      restriction.

    **H3-v3 Overlay map-merge fix.** `Overlay()`
    (`internal/config/config.go`) uses field-by-field `md.IsDefined()`
    copying. Existing `Pricing.Models` slice is whole-replacement on
    overlay (the iter-78 baseline for that field). For `Wrap.Tiers` we
    need MERGE semantics — a per-project overlay that defines only
    `[wrap.tiers.opus] = "anthropic:claude-opus-4-7"` should not erase
    the operator-global `[wrap.tiers.sonnet]` entry. Explicit
    map-merge code was added in `Overlay()` (clone-then-merge to avoid
    mutating the caller's map). The `Load()` path leverages
    BurntSushi/toml's automatic map-merge semantics into a
    pre-populated map.

    The two paths are tested separately: `TestLoad_PartialMapOverride`
    exercises the Load merge; `TestOverlay_PartialMapMerge` exercises
    the in-memory Overlay merge (which the toml library does not
    cover because Overlay merges from Go structs, not TOML files).
    M1-v2 + H3-v3 split test coverage was the cue that the two paths
    were divergent until this fix.

    **Test isolation note (post-Phase-4 fix in commit `8ed4a2f`).**
    Handler tests that exercise the full `vv_wrap_dispatch` path were
    leaking dispatch jsonl entries into the operator's real
    `~/.cache/vibe-vault/wrap-dispatch.jsonl`. `withSkeletonCacheDir`
    only redirected `wrapbundlecache.CacheDir`, not
    `wrapmetrics.CacheDir()`. Fix: the helper now pins
    `VIBE_VAULT_HOME` to a tempdir if not already set by the caller.
    Discovered via /restart's `vv stats wrap` smoke producing
    synthetic test data (a tier=`sonnet` line with mock provider
    fingerprints). The leak is now caught at write time by HOME-sandbox
    canary `no_real_vault_mutation`.

    Source: `internal/wrapmetrics/dispatch_writer.go`,
    `internal/config/config.go` (`WrapConfig` + `Overlay` map-merge),
    `internal/wrapmetrics/stats.go`, `cmd/vv/main.go` (the
    `stats wrap` subcommand),
    `internal/mcp/tools_prepare_skeleton_test.go`,
    `test/integration_test.go` (the test-isolation fix in commit
    `8ed4a2f`). Phase 4 commits `1bb15a5..52eb607` plus `8ed4a2f`.

89. **API key resolution: config-first / env-fallback / actionable-error,
    shared resolver across hook + synthesis + dispatch.** The operator's
    Pro Max subscription handles orchestrator AI usage; the standalone
    provider APIs that back hook enrichment, session synthesis, and
    `vv_wrap_dispatch` bill against **separately metered keys** that
    don't fit Claude Code's ambient-shell-env model (the operator
    deliberately unsets `ANTHROPIC_API_KEY` before launching Claude Code
    to keep the subscription path clean). Iter 163 first hit this with a
    raw "ANTHROPIC_API_KEY not set" error; iter 164 added env passthrough
    to the plugin `.mcp.json`; iter 165 (this decision) introduced
    explicit per-provider config storage with a layered resolver.

    **Decision.** A new `[providers.<P>].api_key` schema in
    `~/.config/vibe-vault/config.toml` stores keys for `anthropic`,
    `openai`, and `google` (the three providers in `internal/llm/`
    today). A new exported helper `llm.ResolveAPIKey(provider,
    providers)` (`internal/llm/keyresolver.go`) returns the key with
    three-tier precedence:

    1. `providers.<P>.APIKey` (config.toml) — wins if non-empty.
    2. `os.Getenv(envVarFor(provider))` — fallback for operators with
       env-var-based setup.
    3. Both empty → return an actionable error naming **both**
       `vv config set-key <provider> <key>` AND the provider's env var,
       so operators in either setup style get unambiguous guidance.

    `envVarFor` maps `anthropic→ANTHROPIC_API_KEY`,
    `openai→OPENAI_API_KEY`, `google→GOOGLE_API_KEY`. Unknown providers
    return an error naming the supported set.

    **Resolver is shared, factory is not.** `NewProvider(enrich,
    providers)` (`internal/llm/provider.go`) routes by
    `[enrichment].provider` for the hook + synthesis path. The
    dispatch handler in `internal/mcp/tools_wrap_dispatch.go` routes by
    the tier-string-prefix extracted from `[wrap.tiers]`
    ("anthropic:claude-sonnet-4-6" → provider="anthropic"). **Different
    routing axes, identical resolution semantics** — so the resolver is a
    small exported helper called by both call sites, not a factory
    wrapper. Folding the resolver into `NewProvider`'s body would have
    left dispatch out (the v2 plan review caught this).

    **Storage and write semantics.** `vv config set-key <provider>
    <key|->` (`cmd/vv/config_setkey.go`) writes the key via a
    line-oriented in-place editor pattern-matching
    `internal/config/write.go:updateVaultPath()` so unrelated lines,
    sections, and comments survive untouched. Atomic temp+rename in the
    same directory at mode 0600 (pattern-matches
    `internal/wrapbundlecache/cache.go`); parent directory chmod'd to
    0700 defensively. Stdin form (`-`) trims a single trailing newline
    but rejects embedded newlines, leading/trailing whitespace, and
    empty values. `--force` required to overwrite an existing key. The
    same line-oriented edit pattern means `vv init` re-runs are
    key-blind — `WriteDefault()` only touches `vault_path`, locked by
    `TestWriteDefault_PreservesProviderKeys`.

    **Defaults.** `Validate()` does NOT require keys; configs with no
    `[providers]` section load cleanly to an empty `ProvidersConfig{}`
    (BurntSushi/toml zero-value default). `vv init` stamps three
    commented `[providers.<P>]` stubs at the end of the generated
    config — discoverable for the operator who needs them, zero new
    setup for the operator who doesn't. `Overlay()` merges providers
    field-by-field via the existing `md.IsDefined()` pattern.

    **Plugin .mcp.json env block — preemptive expansion.**
    `internal/plugin/plugin.go` `mcpEnvPassthroughKeys` lists all three
    provider env vars (not just `ANTHROPIC_API_KEY`). Cost is two extra
    list entries plus two test-loop extensions; harmless when the env
    vars are unset (Claude Code expands `${VAR}` to empty). Removes a
    future "why doesn't my OPENAI key reach the subprocess" support
    query.

    **Consequences.** Env-var fallback preserved for existing setups —
    no migration needed. `vv config set-key` is the one-command path
    forward for operators who want config-based keys. The standalone
    provider APIs no longer require ambient shell env, decoupling
    dispatch billing from Claude Code's env-handling semantics.

    Source: `internal/llm/keyresolver.go`,
    `internal/llm/keyresolver_test.go`, `internal/llm/provider.go`,
    `internal/config/config.go` (`ProvidersConfig`, `ProviderConfig`,
    `Overlay`), `internal/config/write.go` (`WriteDefault` +
    `ProjectConfigTemplate` stubs), `internal/config/write_test.go`
    (`TestWriteDefault_PreservesProviderKeys`),
    `internal/mcp/tools_wrap_dispatch.go`,
    `internal/mcp/tools_wrap_dispatch_test.go`,
    `cmd/vv/config_setkey.go`, `cmd/vv/config_setkey_test.go`,
    `internal/help/commands.go` (`CmdConfig` + `CmdConfigSetKey`),
    `internal/plugin/plugin.go` (`mcpEnvPassthroughKeys`),
    `internal/plugin/plugin_test.go`, `internal/plugin/inject_test.go`.

90. **Marker-bounded state-derived regions in resume.md; `ApplyBundle`
    Step 9 (`resume_state_blocks`) re-renders them last from filesystem
    ground truth; `ApplyMarkerBlocks` self-heals when markers are
    absent.** Iters 165–166 surfaced a dispatch-path-specific drift
    class: `## Project History (recent)` missed the iter-165 row, and
    `### Active tasks (1)` still named the retired
    `dispatch-api-key-resolution` task even though its file had moved
    to `tasks/done/`. Root cause: the inline-orchestrator wrap path
    maintains those sub-regions via `vv_update_resume`
    (`internal/mcp/tools_context_write.go`), which the wrap-executor's
    `forbidden_tools` blocks for the dispatched-executor path.
    Different content classes, different authoring mechanisms, no
    common authority — until Step 9.

    **Two content classes, one new authority.** resume.md content
    splits cleanly along an authoring axis. **Narrative** content —
    Project Summary, iteration prose, decision rationale, Open
    Threads prose, carried-forward bullets — stays LLM-authored
    through the existing `applyThreadInsert` / `applyCarriedAdd` /
    `vv_update_resume` mechanisms. **State-derived** content — the
    Active-tasks list, Current-State headline counts, Project-History
    tail rows — is generated from filesystem ground truth and now
    flows through the new `applyResumeStateBlocks` apply step into
    marker-bounded regions. Both wrap paths converge on `ApplyBundle`
    (DESIGN #84 Architecture A1), so Step 9 fires uniformly for
    inline AND dispatch.

    **Three marker regions.** `<!-- vv:active-tasks:start --> ...
    <!-- vv:active-tasks:end -->` inside `## Open Threads` carries
    the `### Active tasks (N)` H3 rendered from `tasks/*.md` minus
    `done/` and `cancelled/` (sorted priority desc, then slug asc).
    `<!-- vv:current-state:start --> ... :end -->` inside `## Current
    State` carries invariant bullets — see headline-count lockdown
    below. `<!-- vv:project-history-tail:start --> ... :end -->`
    inside `## Project History (recent)` carries the last N=10 rows
    of the iteration table, scanned from `iterations.md` headings.
    The marker shape pattern-matches the iter-116 `<!-- vv:data-
    workflow:start -->` precedent already in the live vault file;
    the rendering machinery is fresh under `internal/wraprender/`
    (the data-workflow block is hand-edited and has no Go renderer).

    **No LLM enforcement needed.** v1 of this design considered a
    triple-defense layer enforcing marker invariants in synthesizer
    prompts, in QC, and in apply. The wrap-executor agent is
    structurally forbidden from authoring resume.md content via
    `internal/agentregistry/agents/wrap-executor.md` `forbidden_tools`
    — `vv_update_resume`, `Edit`, `Write`,
    `vv_apply_wrap_bundle_by_handle`. Prompt and QC enforcement would
    have solved a non-existent threat. The fix is a single
    apply-pipeline mutation, not a new policing layer.

    **Step 9 ordering vs `vv_update_resume`.** The inline-path
    orchestrator may call `vv_update_resume(section="Current State",
    ...)` during a wrap. That handler rewrites the full body of the
    H2 section via `mdutil.ReplaceSectionBody`, so any orchestrator-
    authored `content` arg that omits the marker pair clobbers it.
    Resolution: **Step 9 runs LAST in `ApplyBundle`, after
    `capture_session`, AND `ApplyMarkerBlocks` is self-healing.** If
    the marker pair survives the orchestrator's writes, Step 9
    replaces its contents in place; if the pair was clobbered or
    never existed, Step 9 inserts the pair at a sensible default
    location relative to existing H2/H3 anchors and renders fresh
    contents. Either way the post-wrap state converges to filesystem
    truth. The dispatch path has no clobbering risk — only Step 9
    writes the affected regions.

    **Step 9 metric line is special-cased out of drift counters.**
    Every other apply step records a synth-vs-apply SHA pair to
    `wrap-metrics.jsonl`; the synthesizer never carries marker
    content (the bundle has no resume-state-blocks field), so
    synth_sha and apply_sha are equal by construction. Step 9
    records the metric line with `synth_sha == apply_sha ==
    fingerprint(rendered_content)` and `synth_bytes == apply_bytes`,
    and `recordMetricRaw` is gated to NOT increment
    `driftSummary.DriftedFields` for `Step == "resume_state_blocks"`.
    Locked by `TestApplyBundle_ResumeStateBlocksMetricLineSpecialCase`.

    **Headline-count meaning is locked — operator-chosen scope
    reduction.** The renderer emits exactly three invariant bullets
    in the `current-state` region: `**Iterations:** <N> complete`,
    `**MCP:** <N> tools + 1 prompt`, `**Embedded:** <N> templates`.
    The plan's Phase 1 contract included a fourth `**Tests:** ...`
    bullet sourced from RUN-counted `go test ./...`; Option C
    (commit `74a02c7`) deliberately dropped it before merge. Two
    reasons: (1) the renderer would have to shell out to `go test`
    on every wrap to get a deterministic RUN-count, an unacceptable
    Step-9 cost; (2) RUN-counted vs test-function-counted are
    different numbers and prior resume.md prose has used both — the
    machine-rendered bullet would have locked one meaning silently
    (R3 in the plan). Test count and other prose remain
    operator-authored adjacent to the marker block. Future re-add
    of a `Tests:` bullet requires a separate plan with an explicit
    headline-meaning decision; do not slip it back in incrementally.

    **Iterations / MCP / Embedded sources** are stable and cheap.
    Iteration count from heading scan of `iterations.md`. MCP tool
    count from `(*mcp.Server).ToolNames()` (introduced iter 161,
    DESIGN #88's per-iter measurement gate). Embedded template
    count from `fs.WalkDir` over `templates.AgentctxFS()`
    (`templates/embed.go`) — deliberately not the scaffold-side
    `internal/scaffold/scaffold.go` embed FS, which is a separate
    artifact serving a different purpose.

    **`ApplyMarkerBlocks` self-healing demotes the retrofit utility
    to deferred follow-up.** With Step 9 inserting markers on first
    apply, the optional `vv check resume-markers [--fix]` utility
    becomes pre-population polish only; consumer projects (rezbldr,
    vibe-palace, others) acquire the markers automatically on their
    next wrap. Per plan recommendation #4, the utility is deferred
    until multi-project rollout demands markers visible before the
    next wrap on each project. Not load-bearing; not implemented.

    **v10 invariants-only contract honored.** The renderer's
    `current-state` output is a strict subset of the v10
    invariant-bullet whitelist (`Iterations`, `MCP`, `Embedded` are
    all in `invariantFirstWords`). HTML comment lines pass the
    multi-line comment carve-out in
    `internal/context/invariants.go`. The contract is locked by
    `TestRenderCurrentState_OutputPassesV10Validator`, which calls
    `context.ValidateCurrentStateBody` on the renderer's output and
    asserts ok=true — a future renderer change that violates the
    contract fails CI before reaching production.

    **Carried-bullet structural non-collision.** `applyCarriedAdd`
    (Step 5) and `applyResumeStateBlocks` (Step 9) both nominally
    mutate `## Open Threads`, but `mdutil/carried.go`
    `locateCarriedBullets` parses by `### ` prefix and only operates
    inside the exact `### Carried forward` H3. HTML comments in
    other H3 subsections (notably `### Active tasks`) are ignored
    by the parser. Collision risk is structurally zero, locked as a
    regression test by
    `TestApplyBundle_ResumeStateBlocksAfterCarriedAdd_BothIntact`.

    **Fail-stop on Step 9.** Matches every prior step (DESIGN #63).
    A malformed `iterations.md` or a transient FS error after Step 8
    succeeded leaves the operator with a committed iteration
    narrative and stale resume.md state — recoverable by hand on a
    subsequent wrap. Warn-and-continue was considered and rejected
    for v1; revisit only if the failure pattern actually surfaces.

    Source: `internal/wraprender/markers.go`,
    `internal/wraprender/markers_test.go`,
    `internal/mcp/wrapapply.go` (Step 9 ordering + drift-summary
    special case), `internal/mcp/tools_apply_wrap_bundle.go`
    (`applyResumeStateBlocks` helper),
    `internal/mcp/tools_apply_wrap_bundle_test.go` (six new Step-9
    tests). Phase 1 commit `937f016`, Phase 2 commit `d2f6474`,
    Option-C correction (drops the Tests bullet) commit `74a02c7`.

91. **Per-project wrap-skeleton cache subdirectory layout; legacy
    self-heal to `_legacy/`; `DefaultRotationN` and rotation /
    read-side diagnostics share one source of truth.** The wrap
    skeleton cache (`~/.cache/vibe-vault/wrap-bundles/`) was a single
    flat directory shared across every project, with rotation evicting
    the lowest-iter files globally. Iter 167 surfaced two failures of
    that policy: cando-rs (iter-10) and RezBldrVault (iter-39) both
    fell off the dispatch path because vibe-vault's iter-164/165/166
    skeletons stayed resident under the global "keep 3 highest"
    policy. Both projects had to fall back to the surgical apply path
    documented in `templates/agentctx/commands/wrap.md` until their
    own iter counts caught up — which would never happen for projects
    that wrap less frequently than vibe-vault.
    
    **Decision: per-project subdirectory.**
    `<base>/<project>/iter-<N>-skeleton.json`. Rotation walks one
    subdirectory and evicts only that project's files; cross-project
    eviction is impossible by construction. Considered alternatives
    were a project-prefix filename pattern (forces every consumer that
    scans the dir to filter by prefix) and mtime-based rotation
    (doesn't bound per-project disk usage; a chatty project still
    starves a quiet one). Per-project subdir is the only option that
    structurally eliminates cross-project eviction with one mechanical
    API change — threading a `project string` argument through
    `Write`, `RotateKeepN`, `SkeletonPath`, and `CacheDir`. Five
    production call sites and five test sites needed the new arg; the
    `Read`/`SkeletonHandle` shape stayed unchanged because the
    handle's absolute `skeleton_path` self-encodes the per-project
    subdirectory.
    
    **`DefaultRotationN = 3` exported single source of truth.** The
    pre-fix code hardcoded `RotateKeepN(3)` at the call site in
    `tools_prepare_skeleton.go` AND assumed N=3 implicitly in the
    "keep 3 most recent" wording in DESIGN #86 / ARCHITECTURE.md /
    documentation. v3 promotes the literal to
    `wrapbundlecache.DefaultRotationN` so the call-site rotation, the
    read-side diagnostic message ("rotation policy: keep N most
    recent per project"), and any future operator-facing reference
    all share one constant. Future tuning is a single edit. We
    deliberately DID NOT expose N as a config knob in this iteration
    — no operator has asked, the validation surface would expand for
    zero observed demand, and a follow-up plan can plumb
    `config.WrapConfig.SkeletonRetainCount` in five lines if demand
    materializes.
    
    **Package-local `validateProject` twin avoids an import cycle.**
    The cache layer trusts MCP-tool-validated `project` input but
    runs a defensive re-check at the top of every public function that
    takes `project`. The check pattern-matches
    `internal/mcp/tools.go:163-172` `validateProjectName` (rejects
    empty, `/`, `\`, `..`) but lives inline in
    `internal/wrapbundlecache/cache.go` because `internal/mcp` already
    imports `internal/wrapbundlecache` — a reverse import would close
    a cycle and fail to compile. The package-level comment above the
    duplicate documents the sync expectation: `if you change one,
    update the other in the same patch`. Cost is ~8 lines of code;
    benefit is robustness against future direct callers from
    non-MCP packages (`test/integration_test.go` paths, hypothetical
    CLI subcommands for cache inspection) that would otherwise be a
    silent footgun.
    
    **Self-healing legacy migration to `_legacy/`.** On first
    `CacheDir(project)` invocation per process, gated by
    `sync.Once`, `migrateLegacyFiles(base)` relocates any
    pre-existing `<base>/iter-*-skeleton.json` files to
    `<base>/_legacy/iter-*-skeleton.json`. Operators upgrading from
    the flat layout see their old skeletons preserved in a clearly
    named sibling directory rather than disappearing. The migration
    is multi-process safe: each `vv mcp` subprocess runs its own
    `legacyMigrationOnce`, but per-rename `os.IsNotExist` is treated
    as "another process beat us to this file" and tolerated; only
    successful relocations count toward the stderr emit, so the
    notice reflects work actually done by the calling process rather
    than a misleading detection count. `_legacy/` is excluded from
    per-project rotation by construction (RotateKeepN walks
    `<base>/<project>/`, not `<base>/`); operators can manually
    delete `_legacy/` once they've inspected it. The system does not
    auto-prune.
    
    **One-way upgrade caveat.** A downgraded binary writes new
    skeletons to the old flat location and reads via the old
    `SkeletonPath(iter)`; the new-binary `_legacy/` directory is
    invisible to the old code (skipped by `e.IsDir() continue` in
    the legacy `RotateKeepN`). Result: downgrade works mechanically,
    but old-binary writes mix with new-binary writes if the operator
    yo-yos between binaries. Documented as "do not flap binaries;
    the upgrade is one-way." Re-upgrading after a downgrade re-runs
    the migration sweep on the next-process boot, relocating any
    flat-layout files written during the downgrade window into
    `_legacy/`.
    
    **Step 9 metric N/A.** DESIGN #90 introduced the
    `resume_state_blocks` Step-9 special case in the synth-vs-apply
    drift counter. That special case is for resume.md state-derived
    regions; this epic touches the wrap-skeleton CACHE rather than
    wrap-resume RENDER and inherits no drift-counter impact. The
    cache layer doesn't write `wrap-metrics.jsonl` records.
    
    **`LegacyIterSentinel = -1` and `LegacyProjectName = "_legacy"`
    constants for the InspectAll wire contract.** Phase 2 added
    `wrapbundlecache.InspectAll() (map[string]ProjectStats, error)`
    so `vv stats wrap` could render per-project rows. Real skeleton
    files have `iter >= 1` (rejected at write time when `iter <= 0`),
    but the `_legacy/` directory's row in the rendered table needs
    SOMETHING in the iter columns. We chose `-1` as a sentinel that
    the renderer detects to substitute `(relocated; safe to remove)`
    in place of the iter-column values, paired with a `_legacy`
    project-name string that the renderer keys on. Both are exported
    from `wrapbundlecache` AND duplicated as locked-string literals
    inside `internal/wrapmetrics/stats.go` with comments referencing
    each other — keeping the import boundary clean (`wrapmetrics`
    must not depend on `wrapbundlecache`; the integration happens
    in `cmd/vv/main.go` which imports both). The sync-comment
    documents the wire contract: change one literal, change all four
    references in the same patch.
    
    **Rotation diagnostic emits stderr when deletions happen;
    read-side diagnostic on `os.IsNotExist`.** When `RotateKeepN`
    deletes any files, it writes one line to stderr naming the
    project, the deleted files (basenames only, not full paths),
    and the kept count: `wrapbundlecache: rotated project=vibe-vault
    deleted=[iter-163-skeleton.json] kept=3`. When `Read` hits
    `os.IsNotExist`, it wraps the error with an actionable hint
    naming the absolute path AND the rotation policy
    (`DefaultRotationN`) — the operator sees "skeleton cache miss
    at <path> — was the skeleton evicted by RotateKeepN, or did the
    orchestrator pass a stale handle? (rotation policy: keep N most
    recent per project)". Both diagnostics use the same
    `DefaultRotationN` constant rather than a re-typed literal, so
    rotation tuning never produces a stale diagnostic message.
    
    **`vv stats wrap` cache section, unconditional rendering.**
    Phase 2 extended `wrapmetrics.WrapStats` with a `Cache
    map[string]CacheRow` field and `wrapmetrics.FormatWrapStats`
    with a new "wrap skeleton cache" section after the drift
    section. Per-project rows show `skeletons`, `bytes`,
    `oldest_iter`, `newest_iter`. The `_legacy` row is special-cased
    to a parenthetical: `_legacy   2 (relocated; safe to remove)`,
    triggered by `OldestIter == wrapbundlecache.LegacyIterSentinel`
    on a row whose project name is `wrapbundlecache.LegacyProjectName`
    (locked-string literals — see constants above). The cache
    section is unconditionally rendered regardless of jsonl
    presence: previously `FormatWrapStats` early-returned a "no data
    yet" sentinel when both jsonl files were empty, which would have
    hidden the cache state at exactly the moment it's most useful
    (operator just ran `vv stats wrap` because they don't know
    what's in the cache). The early-return was removed; the function
    now falls through and the cache section renders even when no
    drift / dispatch metrics exist. The "no data yet" sentinel is
    preserved for the metrics sections that have no rows.
    
    Source: `internal/wrapbundlecache/cache.go`,
    `internal/wrapbundlecache/cache_test.go` (12 tests, +6 from the
    pre-epic 6), `internal/mcp/tools_prepare_skeleton.go`
    (project arg threading + tightened `validateProjectName` +
    `DefaultRotationN` constant use),
    `internal/mcp/tools_prepare_skeleton_test.go` (+1 invalid-slug
    test), `internal/mcp/tools_synthesize_wrap_test.go`,
    `internal/mcp/tools_wrap_dispatch_test.go`,
    `test/integration_test.go`, `internal/wrapmetrics/stats.go`
    (`Cache map[string]CacheRow` field, `CacheRow` type,
    cache-section renderer with `_legacy`-row carve-out),
    `cmd/vv/main.go` (`computeStatsWrap` invokes
    `wrapbundlecache.InspectAll`), `cmd/vv/stats_wrap_test.go`
    (4 tests, +1 from 3). Phase 1 commit `e29aaec`, Phase 2 commit
    `f811392`.

92. **Direction-C: Wrap pipeline collapse — record-and-manage, not
    interpret.** Iters 163–167 surfaced a steady stream of wrap-flow
    failures (sonnet QC failures, opus marginal passes, surgical
    fallback used in five consecutive iters). The audit traced the
    pattern to a structural defect, not a model-quality issue: the
    dispatch ladder shipped iter 162 (DESIGN #84) was architecturally
    unable to produce the citation-rigor output its QC layer (#87)
    demanded.

    **Root cause: executor isolation.** Two facts from the v4 plan
    audit make this concrete:

    1. The wrap-executor agent template
       (`internal/agentregistry/agents/wrap-executor.md`, line 6,
       deleted in Phase 4) explicitly forbade `vv_get_resume`,
       `vv_get_iterations`, `Read`, and every other context-fetch
       tool — the executor was a sandboxed prose-drafter with no
       ability to consult project state.
    2. The skeleton (`WrapSkeleton`,
       `internal/mcp/wrapbundle.go:41–54`) carried slug-IDs and
       file paths but NOT commit SHAs, decision text, narrative
       context, or resume.md state. The user prompt in
       `dispatch.go:312–332` injected only the skeleton JSON +
       prior-tier escalation reasons.

    QC then required the executor to cite specific commit SHAs,
    decision-number references, and per-paragraph file/function
    citations (`tools_quality_check.go:87–93`). The LLM was given
    an impossible task. Sonnet's failures and opus's marginal passes
    weren't a model-quality issue; the LLM was correctly refusing
    (or marginally hallucinating) when the required citations were
    absent from its input. No amount of tier escalation, prompt
    tuning, or QC relaxation could fix this — the defect was in
    the architectural premise (sandboxed executor + slug-only
    skeleton).

    **The new shape.** Direction-C collapses the bundle pipeline
    into two new MCP tools and a context-aware slash command:

    - `vv_describe_iter_state(project?)` — minimal server state
      record. Returns `{iter_n, branch, vault_has_uncommitted_writes,
      last_iter_anchor_sha}`. Server-computable fields only;
      everything else (commits since last iter, files changed, task
      deltas, test counts) is computed by the slash command via
      git/filesystem, anchored by `last_iter_anchor_sha`.
      ~40 lines reusing existing helpers
      (`index.Index.NextIteration`, `vaultsync` git plumbing).
    - `vv_render_wrap_text(kind, tier, iter_state, project_context)`
      — single context-aware renderer with a `kind:` discriminator
      (`iter_narrative` | `commit_msg` | `iter_narrative_and_commit_msg`).
      One LLM call, returns prose. Consumes only the basic single-turn
      `Provider` interface; no `AgenticProvider`, no `RunTools`, no
      multi-turn loop. Tier-string lookup via `[wrap.tiers]`;
      single-tier-per-call (operator re-runs with `--tier=opus` if
      output is poor; no auto-escalation).
    - `templates/agentctx/commands/wrap.md` rewritten as the
      canonical surgical-render path (218 lines). Shape detection
      (`fresh-feature` / `planning` / `reconciliation` / `vault-only`
      / `writes-already-landed`) lives in the slash command, not
      Go code, so shape semantics are editable without rebuild +
      reinstall (the `make install re-embeds templates` friction
      documented in auto-memory). Three of five shapes don't need
      LLM synthesis at all.

    The principle: **/wrap records history; it does not interpret
    history.** Source control and the vault filesystem are the
    canonical record. The LLM's job is to write short, accurate
    prose when prose is needed — nothing more. Everything else
    (file mutation, git plumbing, vault sync) is deterministic.

    **D4b auto-heal hooks for marker-bounded resume.md state
    regions.** DESIGN #90 introduced three marker-bounded regions
    rendered last in `ApplyBundle` Step 9
    (`active-tasks` / `current-state` / `project-history-tail`).
    With `ApplyBundle` retired, the same mechanism is preserved
    via post-write hooks in `vv_append_iteration` and
    `vv_update_resume`: after their primary write succeeds, the
    handler re-renders the three regions from filesystem ground
    truth via the same `wraprender.ApplyMarkerBlocks` machinery.
    The Step-9 helpers (`collectActiveTasks`, `computeCurrentState`,
    `collectHistoryRows`) extracted from the deleted
    `tools_apply_wrap_bundle.go` into
    `internal/mcp/resume_state_blocks.go` for reuse. Byte-identity
    regression-locked against the prior Step-9 output by
    `internal/mcp/resume_state_blocks_test.go`.

    **mdutil semantic change: hard-error multi-match.**
    `mdutil.ReplaceSubsectionBody` and `mdutil.RemoveSubsection`
    formerly returned a `"candidates_warning:"` prefix on
    multi-match (a soft warning consumers had to extract via
    `extractCandidatesWarning`). They now hard-error. This lifts
    the QC layer's `multi_match_ambiguity` rule into the write
    path: `vv_thread_replace` and `vv_thread_remove` no longer need
    the warning-extraction dance — the err branch catches multi-match
    directly. The `extractCandidatesWarning` helper and the
    `CandidatesWarning` JSON field on the thread result type are
    deleted. The two dead production callers in the retired
    `tools_apply_wrap_bundle.go` and `wrapapply.go` retire with
    Phase 4. Test sites in `internal/mdutil/mdutil_test.go` and
    `internal/mcp/tools_thread_test.go` rewritten to assert
    `err != nil` and document-unchanged on multi-match.

    **Config simplification.** `WrapConfig.EscalationLadder` removed
    from the struct, defaults, validation, overlay, and tests. The
    legacy key silently no-ops (BurntSushi/toml is non-strict by
    default — `Load()` never calls `MetaData.Undecoded()`); a
    one-line stderr deprecation log on `md.IsDefined("wrap",
    "escalation_ladder")` helps operators clean up legacy config
    without surprise behavior change. `WrapConfig.Tiers` and
    `WrapConfig.DefaultModel` survive as `vv_render_wrap_text`'s
    tier→provider:model lookup table.

    **Net MCP tool count: 43 → 39.** Five retirements
    (`vv_synthesize_wrap_bundle`, `vv_apply_wrap_bundle_by_handle`,
    `vv_wrap_quality_check`, `vv_wrap_dispatch`,
    `vv_prepare_wrap_skeleton`), one fold (`vv_render_commit_msg`
    folds into `vv_render_wrap_text`'s `kind: "commit_msg"` variant),
    two additions (`vv_describe_iter_state`, `vv_render_wrap_text`).
    The integration test's exact-set assertion at `expectedTools`
    in `test/integration_test.go` was updated in lockstep. Internal
    packages retired: `internal/wrapdispatch/`,
    `internal/wrapbundlecache/`, `internal/wrapmetrics/`. Internal
    LLM-side dead code retired: `internal/llm/anthropic_agentic.go`,
    `AgenticProvider` interface in `internal/llm/types.go`,
    `RunTools` method on `anthropicHTTPCore`. CLI subcommand retired:
    `vv stats wrap` (the entire subcommand — all three sections
    sourced their data from the retired infrastructure). Agent
    template retired: `wrap-executor.md` (the
    `internal/agentregistry/` package and `vv_get_agent_definition`
    MCP tool survive as generic agent-registry scaffolding with
    no remaining wrap-flow consumer).

    **What this closes.** `dispatch-qc-planning-iter-rigidity` (the
    QC layer goes away); surgical-fallback-as-default (surgical IS
    canonical now); the iter-7 `vv_render_commit_msg` XML-mangling
    class on the vibe-vault side (regression-tested by
    `TestRenderWrapText_XMLSpecialCharsRoundTrip` —
    XML-special-character prose round-trips verbatim through
    `json.Unmarshal` + verbatim-write); and `wrap-pipeline-idempotency`
    (the `writes-already-landed` shape becomes first-class via
    slash-command shape detection). Implicit: the
    `AgenticProvider` / `AnthropicAgentic` / `RunTools` multi-turn
    tool-use surface area retires as dead code; future agentic
    features would re-introduce as needed.

    Source: `internal/mcp/tools_describe_iter_state.go`,
    `internal/mcp/tools_describe_iter_state_test.go`,
    `internal/mcp/tools_render_wrap_text.go`,
    `internal/mcp/tools_render_wrap_text_test.go`,
    `internal/mcp/wrap_prompts.go` (system preamble + 3 user-prompt
    constants), `internal/mcp/resume_state_blocks.go`,
    `internal/mcp/resume_state_blocks_test.go` (D4b auto-heal byte-
    identity coverage), `internal/mcp/tools_context_write.go` (D4b
    hook insertion in `NewAppendIterationTool` and
    `NewUpdateResumeTool`), `internal/mdutil/mdutil.go`
    (`ReplaceSubsectionBody`, `RemoveSubsection` hard-error),
    `internal/mdutil/mdutil_test.go` (rewritten multi-match
    coverage), `internal/mcp/tools_thread.go` (caller updates;
    `extractCandidatesWarning` deleted), `internal/mcp/tools_thread_test.go`
    (rewritten ambiguity tests), `internal/config/config.go`
    (`EscalationLadder` removed; deprecation log in `Load()`),
    `internal/config/config_test.go` (escalation tests removed),
    `templates/agentctx/commands/wrap.md` (canonical surgical-render
    path, 218 lines). Phase 2 commits `d3ea015`, `db878b7`, `add2a08`,
    `dc91bb5`, `903d17f`. Phase 3 commit `e034650`. Phase 4 commits
    `163fa82`, `c8705fc`, `c63ddc0`, `1f8da79`. Phase 5 (this entry):
    docs only.
    Re-open conditions: future agentic-tool-use features
    (vault-graph search, multi-document planning, chained
    code-edit loops) would re-introduce an `AgenticProvider`
    interface and a per-task multi-turn dispatcher — but each
    such feature must demonstrate that its executor has access
    to every input its quality gate demands, lest it repeat the
    iter-162 defect.

93. **Mechanical iter anchor via `.vibe-vault/last-iter` stamp
    file; written by `vv_stamp_iter`, looked up via `git log -n 1
    -- .vibe-vault/last-iter`.** Direction-C (Decision 92) collapsed
    the wrap pipeline to record-and-manage but kept the iter anchor
    on a free-text signal: `vv_describe_iter_state` searched
    commit bodies for an `## Iteration N` H2 footer. Iters 168–170
    surfaced two carried-forward threads
    (`wrap-anchor-when-prior-iter-was-vault-only`,
    `wrap-shape-rebase-merge-not-recognized`) whose patches kept
    bouncing because the underlying signal was already dead.

    **Decision.** The wrap pipeline anchors on a project-tracked
    stamp file at `<projectPath>/.vibe-vault/last-iter` whose
    write is performed by the `vv_stamp_iter` MCP tool as the
    canonical Stage 4 step. `vv_describe_iter_state.last_iter_anchor_sha`
    is computed by `git log -n 1 --format=%H -- .vibe-vault/last-iter`
    — the SHA of the most recent commit that touched the stamp
    file. The stamp is committed every wrap; from the second wrap
    onward the anchor is always present and always correct.

    **Why.** Forensics on the prior signal: of the last 200 commits
    across all branches as of iter 170, only 5 contained the
    `## Iteration N` H2 footer (iters 153, 155, 159, 162, 164 —
    all from the pre-Direction-C dispatch-ladder era; last sighting
    is iter 164's commit `25b1892`). Decision 92's wrap.md rewrite
    did not instruct the orchestrator to emit the footer, and
    `vv_render_wrap_text` does not synthesize it. The convention
    died with Direction-C; nothing has been replenishing the signal
    for ~6 iters. Both carried-forward threads
    (`wrap-anchor-when-prior-iter-was-vault-only`,
    `wrap-shape-rebase-merge-not-recognized`) were symptoms of the
    same dead convention rather than independent bugs — every
    "rebase-merge not recognized" or "vault-only iter mis-anchored"
    failure traced back to the body-grep loop returning empty or
    falling through to a 50-commit-deep stale anchor.

    **What this supersedes.** The body-grep loop in
    `lastIterAnchorSha` and the `iterAnchorRe` regex
    (`internal/mcp/tools_describe_iter_state.go`) are deleted; the
    new helper is ~15 lines and shells `git log -n 1 --format=%H
    -- .vibe-vault/last-iter`. The `targetIter int` argument that
    discriminated footer matches is gone — there is exactly one
    anchor SHA (most recent commit touching the stamp), so no
    discrimination is needed. The walk-back-by-N approach proposed
    in v1 of this task's plan is abandoned in favor of Option D
    (the stamp file): walk-back was operating on the same dead
    signal and would have returned increasingly stale anchors as
    the convention continued to atrophy.

    **Why a stamp file.** The stamp removes free-text from the
    loop entirely. The MCP tool writes a single decimal integer
    plus newline (e.g. `170\n`) to a tracked file; git records the
    commit SHA; `git log -n 1 -- <path>` retrieves it. Three
    primitives, all mechanical, all enforced by infrastructure
    outside the LLM. The tool is the only writer (server-enforced),
    no prompt-engineering surface remains, and `git log` of a tracked
    path survives every merge style by construction — rebase-merge,
    squash-merge, ff-merge, and direct commit all preserve which
    commit last touched a tracked file. `Config.StateDir()`
    (`internal/config/config.go:499`) returns
    `<vaultPath>/.vibe-vault` for host-local vault state and is
    a different filesystem path with different content semantics
    from the stamp's `<projectPath>/.vibe-vault/last-iter`; the two
    never overlap because vault-path and project-path are distinct
    in `~/.config/vibe-vault/config.toml`. Future maintainers
    grepping for `.vibe-vault/` will hit both contexts and must
    state which root any reference is relative to.

    **Migration.** Existing projects don't have `.vibe-vault/last-iter`
    at the time this decision lands. The first post-PR wrap creates
    and commits it; subsequent wraps find it via `git log --
    .vibe-vault/last-iter`. Pre-creation wraps fall through to a
    single mechanical fallback in `wrap.md` Stage 1: `git rev-list
    --max-parents=0 HEAD | tail -1` — the project's oldest root
    commit, deterministic, no operator judgment. The resulting
    `commits_since_last_iter` window spans the project's entire
    history; shape classification falls naturally into
    `fresh-feature` (or `planning` if the only delta is a new task
    file). One-time transition per project.

    **Companion shape-taxonomy rework.** With every wrap stamping,
    the prior `vault-only` vs `reconciliation` distinction collapses:
    both now key on the same observable (empty
    `commits_since_last_iter` window) rather than on stamp-touch
    inspection. `vault-only` and `reconciliation` retire; the new
    `bookkeeping` shape replaces both — empty commits AND empty
    `task_deltas.added` → `bookkeeping`, mechanically composed
    `chore(wrap): stamp iter N` commit with a one-line
    "Bookkeeping iter — no project-side work this cycle." body.
    `fresh-feature` relaxes to drop the prior `task_deltas.added
    empty` constraint, so the four shape rules now partition the
    `(commits-empty, task-added-empty)` Cartesian cleanly:
    `fresh-feature` (any commits) / `planning` (empty + new task)
    / `bookkeeping` (empty + no task) / `writes-already-landed`
    (orthogonal short-circuit on dirty vault + iter row already
    present in iterations.md). Rebase-merge and squash-merge of a
    previously-wrapped feature branch land naturally in
    `bookkeeping` because the merge produces no project work
    between anchor (the rebased/squashed wrap commit) and HEAD.
    Shape detection lives in `templates/agentctx/commands/wrap.md`,
    not in Go, so the rules remain editable without rebuild +
    reinstall.

    **Alternatives rejected.** *Restore the H2 footer convention.*
    Pure prompt-engineering — every wrap depends on the LLM
    faithfully copying a wrap.md instruction into the commit body.
    One missed wrap silently poisons every subsequent anchor
    lookup. Not server-enforceable. *Subject-regex on a `(#NN)`
    PR-merge suffix.* Externally dependent (GitHub-specific),
    conflates "PR merge" with "iter wrap" (not all PR merges are
    wraps), and doesn't help the walk-back side of the problem
    at all. *Walk-back-by-N through the dead footer signal.* See
    "What this supersedes" above — operating on the same dead
    convention.

    **`atomicWriteFile` extraction.** The atomic write helper
    formerly inlined as `atomicWriteCommitMsg` in
    `internal/mcp/tools_commit_msg.go:144-177` extracts to a
    package-private `atomicWriteFile(path, data)` helper alongside
    `dirOf`. The temp-file prefix renames from `.vv-commit-msg-*`
    to `.vv-tmp-*` so the helper's name matches its scope. Both
    `vv_set_commit_msg` and `vv_stamp_iter` call it, and any
    future tool that needs same-filesystem-rename atomicity gets
    it for free.

    **Net MCP tool count: 39 → 40.** Single addition
    (`vv_stamp_iter`); no retirements. The integration test's
    exact-set assertion at `expectedTools` in
    `test/integration_test.go` updates in lockstep.

    Source: `internal/mcp/tools_stamp_iter.go`,
    `internal/mcp/tools_stamp_iter_test.go` (9 tests),
    `internal/mcp/tools_describe_iter_state.go`
    (`lastIterAnchorSha` rewrite; `iterAnchorRe` deleted),
    `internal/mcp/tools_describe_iter_state_test.go` (2 tests
    deleted — `_NoMatch`, `_TargetIterZero`; 7
    `TestLastIterAnchorSha_*` tests added covering stamp-found,
    stamp-missing, untracked, multiple-versions, rebase-preservation,
    no-git tolerance, empty-cwd; `_PriorIterAnchorFound` reseeded
    to write the stamp instead of an H2 footer),
    `internal/mcp/server.go` (`vv_stamp_iter` registered alongside
    `vv_set_commit_msg`), `internal/mcp/tools_commit_msg.go`
    (helper extraction), `templates/agentctx/commands/wrap.md`
    (Stage 1 fallback rewrite, shape-table rewrite for the
    `bookkeeping` collapse, Stage 4 `vv_stamp_iter` step, Stage 5
    git-add reminder). Phase 5 commit `9f64793`. Phase 1+2 commit
    `48c36d3`. Phase 3+4 commit `a9d6a77`. Phase 6 + Phase 7 +
    Phase 8 (this entry): docs only.
    Re-open conditions: future per-project anchors beyond the
    iter stamp (e.g., last-feature-branch base, last-release tag)
    should reuse the stamp-file pattern under `.vibe-vault/`
    rather than reviving any commit-body-grep mechanism; one
    file per anchor purpose.

94. **Path-conditional CI bypass for administrative commits via
    leading `detect-admin-commit` job + `grep -vxFf` allowlist;
    `Test`/`Lint` short-circuit on `is_admin_only`; allowlist
    starts at `.vibe-vault/last-iter` only.** Decision #93's
    mechanical iter anchor produces a wrap commit per cycle whose
    sole project-side change is a single integer written to
    `.vibe-vault/last-iter`. Subjecting every such commit's PR to
    ~9 minutes of Test + Lint adds friction without correctness
    benefit; the diff carries no Go, no docs, no config that
    affects build output.

    **Decision.** `.github/workflows/ci.yml` introduces a leading
    `detect-admin-commit` job that checks whether the diff is
    contained within an allowlist of administrative paths. When
    so, the `Test` and `Lint` jobs short-circuit to success in
    20-40 seconds (cold-start + full clone + early-exit, dominated
    by runner provisioning) instead of the ~9 minutes a substantive
    build takes. The named status checks the branch protection
    requires are still produced — just quickly — and are still
    consumed via the standard PR-merge path. The bypass shortens
    the substantive build; it does not remove the PR. The allowlist
    starts at `.vibe-vault/last-iter` only.

    **Why.** Wrap commits under DESIGN #93 produce single-file
    changes to the iter stamp file that the CI cannot meaningfully
    validate (no Go code touched, no documentation, no
    configuration that affects build output). The bypass eliminates
    the ~9 minutes of CI wall-clock per stamp-only PR (replaced
    with ~20 seconds of admin-only short-circuit), but the PR
    pattern itself remains in place because branch protection's
    `required_status_checks: strict: true` runs PRE-push and won't
    accept commits whose required checks haven't already passed on
    a prior CI run. The path-conditional bypass keeps protection
    in place for substantive changes while making the CI cost on
    administrative ones negligible.

    **Why a fast PR path, not a no-PR path.** GitHub branch
    protection's strict required-status-checks setting demands the
    named checks have already passed on a prior CI run before
    accepting a push or merge. CI workflows run AFTER push/PR
    creation. So a "direct push to main" pattern would require
    disabling strict required-status-checks (or adopting Repository
    Rulesets path-conditional bypass, Option B in the original task
    plan), neither of which is in scope for DESIGN #94. The bypass
    scope is therefore: keep the PR pattern, make the CI cost on
    stamp-only PRs negligible. Verified live via PR #22 (21s
    end-to-end) and PR #23 (17s end-to-end) — the operational
    improvement is real even though the PR step remains.

    **Why CI-level, not branch-protection-level.** GitHub's legacy
    branch protection (currently in use on this repo) doesn't
    support path-conditional status check exemptions. Repository
    Rulesets do, but migrating the protection config is a larger
    separate change with its own audit surface. Path-conditional
    logic in the workflow file is universally available,
    version-controlled, and reverts cleanly with a single commit.

    **Allowlist policy.** Each addition to the `ALLOWLIST` array
    in `detect-admin-commit` requires:

    1. A second concrete use case — never speculative additions.
    2. A DESIGN-decision-style justification recorded in this
       section (or a successor decision) noting why the path is
       administrative AND why CI cannot meaningfully validate it.

    **Alternatives rejected.** Repository Rulesets path-conditioning
    (Option B) — split config audit surface, plan availability
    uncertainty. GitHub App bypass actor (Option C) — wrong
    abstraction (privileges vs administrative content). Make-side
    conditional checks (Option D) — couples build-tool semantics
    to administrative-commit detection.

    **Risk surface.** A malicious actor with write access could
    rename a file to `.vibe-vault/last-iter` to slip past CI.
    Mitigation: this is a friction-reduction, not a security
    boundary; write access already implies trust. If the threat
    model widens, add commit-content validation (file content
    must be a single integer + newline ≤8 bytes).

    **Operational note: coverage artifact gated.** The `Test`
    job's `Upload coverage artifact` step is gated on
    `is_admin_only != 'true'` along with the rest of the
    substantive steps. Admin-only commits do not produce a
    `coverage.out` artifact. No current consumer breaks (the
    artifact is uploaded for retention, not consumed by
    downstream jobs), but future workflows that expect a recent
    coverage artifact for every commit must filter to substantive
    commits.

    **Always-exit-0 mandate.** The `detect-admin-commit` job's
    bash script is hard-coded to never exit non-zero. Any failure
    in path detection, diff resolution, or temp-file creation
    falls through to `admin=false` (the safe default — route to
    full CI) and the script exits 0. Without this invariant, a
    single bash bug would skip the dependent `Test` and `Lint`
    jobs, which GitHub treats as required-checks-not-passing,
    blocking every merge to main repo-wide. With
    `enforce_admins: true`, even repo admins would be blocked.
    Defensive coding here is mandatory, not optional.

    Source: `.github/workflows/ci.yml` (Phase 1 commit `933e609`,
    Phase 2 commit `6199cfd`). Phase 4 (this entry): docs only.
    Re-open conditions: any allowlist growth beyond
    `.vibe-vault/last-iter` requires a follow-up decision entry
    per the allowlist policy above; migration to Repository
    Rulesets path-conditioning would supersede the workflow-level
    bypass entirely.

95. **Clock-injection seam in `internal/zed/` for deterministic
    debounce tests.** `internal/zed/clock.go` introduces a `Clock`
    interface with one method (`AfterFunc(d time.Duration, f func()) Stoppable`)
    and a `Stoppable` interface with one method (`Stop() bool` —
    matches `*time.Timer.Stop()` so `realClock` can return a
    `*time.Timer` directly without an adapter). Production passes
    `realClock{}` (default when `WatcherConfig.Clock == nil`); tests
    pass a `fakeClock` (in `clock_test.go`) that advances on demand
    and tracks registration count for synchronization. `fakeTimer`
    holds a back-pointer to its parent clock so `Stop()` acquires
    the same mutex `Advance()` uses, serializing the two against
    each other.

    **Why.** Pre-existing flake `TestWatch_DebounceResetsOnRepeated-
    Writes` (`internal/zed/watcher_test.go:61`, surfaced PR #18) was
    timing-dependent on real-time `time.Sleep` between writes vs a
    200ms debounce window. CI scheduler hiccups >150ms split one
    expected callback fire into two. Two sibling tests
    (`DebounceFiresAfterQuiet`, `IgnoresNonWALWrites`) had latent
    same-category flake potential and are converted in the same
    commit. `TestWatch_ContextCancellation` is left untouched: it
    doesn't bracket a debounce window, so a fake-clock variant
    earns nothing.

    **Steady-state sync.** Test conversions use
    `waitForCondition(Pending()==1 && Registered()>=N)` rather than
    immediate `assert Pending()==1` to absorb fsnotify multi-event
    delivery (Linux `os.WriteFile`'s `O_TRUNC + Write` produces 1–2
    `IN_MODIFY` events per call) and to avoid observing the
    transient `Pending()==0` between the watcher's `Stop()` and
    `AfterFunc()` calls (two non-atomic statements at
    `watcher.go:71-74`).

    **Scope.** Single package, single timing primitive. The seam
    is intentionally narrow — generalizing to `internal/clock/` for
    cross-package use is deferred until a second package needs it.

    **Alternatives rejected.** Real-time margin bump doesn't
    eliminate the flake category — flakes return on slower
    runners. Channel synchronization doesn't address the wall-clock
    dependence inherent to debounce semantics. An earlier
    plan-revision had `fakeTimer.Stop()` mutating `cancelled`/
    `fired` without the back-pointer mutex; rejected during
    review-plan as a data race against `Advance()`, even though the
    project's CI doesn't currently run `-race`.

    Source: `internal/zed/clock.go` + `internal/zed/clock_test.go`
    + `internal/zed/watcher.go` (commit `92db838`); test
    conversions in `internal/zed/watcher_test.go` (commit
    `4f0eb62`). Re-open conditions: a second package needs the
    Clock seam (lift to `internal/clock/`); CI gains `-race` and a
    new helper-side race regresses; or a debounce-bracketing test
    not currently in scope is added and starts flaking — fold the
    same conversion pattern in.
