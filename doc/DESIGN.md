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

7. **Two dependencies (BurntSushi/toml, klauspost/compress):** Minimal
   dependency tree for a tool that runs on every session end. Enrichment uses
   `net/http` from stdlib -- no LLM SDKs. Zstd added in Phase 4 for transcript
   archival (~10:1 compression on JSONL).

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
    both `cortana-session` (legacy) and `vv-session` when extracting activity
    tags from existing notes.

30. **Per-project config overlay via `agentctx/config.toml`:** Each project can
    override global config settings by uncommenting values in
    `Projects/{project}/agentctx/config.toml`. The overlay is applied after
    project detection in `session.Capture()` via `Config.Overlay()`, which uses
    TOML metadata (`IsDefined()`) to only replace fields explicitly present in
    the overlay file. A fully-commented file changes nothing. This allows
    per-project enrichment models, custom tags, friction thresholds, etc.
    without duplicating the entire config. Schema migration 2→3 generates the
    template for existing projects.
