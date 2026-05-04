# Session Capture Architecture — Two-Tier Vault + Pluggable Source

**Status:** strategic direction, agreed iter 201. Implementation tasks to be filed.
**Authors:** operator + AI pair-programming session, 2026-05-03.
**Supersedes:** much of `mcp-driven-vault-sync` (Draft v4).

---

## Why this document exists

This is a long-running architectural reminder. The decisions below
were made under pressure of two days of recurring vault-coherence
incidents (iter 196 multi-machine narrative race, iter 199 6-day
cross-host divergence with 60+ silently-dropped commits, iter 198
untracked-file rebase precondition gap, multiple wrap-iter-drift
warnings). The goal is to capture *why* we chose what we chose
while the evidence is fresh, so that future iterations of the
plan are bound to the constraints that led here rather than to
local optima.

Read this when:

- A new task touches vault sync, session capture, hooks, or
  multi-host coordination.
- An active plan proposes a structural change that conflicts
  with the direction below.
- You're about to file a task labeled "fix vault sync" — first
  ask whether the right answer is to remove the workload from
  the shared vault, not to make sync more clever.

---

## Problem statement

The vault is doing two jobs with incompatible correctness models.

**Job 1 — Long-lived narrative & decision record.** resume.md,
iterations.md, tasks/, doc/, knowledge/, features.md.
Low write rate (~3 commits per work day). High read value.
Must be coherent across machines. Authorship matters.
Designed-for use case.

**Job 2 — Hot session telemetry.** sessions/*.md emitted by
SessionEnd / Stop / PreCompact hooks. High write rate
(every conversation turn or every session end). Low individual
read value (mostly indexed-then-archived). Per-machine
source-of-truth — each session note is owned by exactly one
host; nothing else writes to it. No cross-host merge semantics
are needed or meaningful.

Job 2 generates ~95% of the vault churn. Every observed
multi-host coherence failure traces back to Job-2 churn
polluting Job-1 coherence:

- The Obsidian community git plugin's auto-commit feature
  fights `vv vault push` writes — almost all of those
  conflicts are over session files.
- The iter-196 / iter-199 multi-machine races happened
  because two hosts were both writing high-frequency content
  to the same merge surface.
- The iter-198 untracked-precondition gap surfaced because
  hook-fired sessions land as untracked files relative to the
  vault's HEAD; rebase-in-progress hits them.

Making sync more clever (the original `mcp-driven-vault-sync`
v4 framing) addresses the symptom. Removing the workload
from the shared vault addresses the cause.

---

## Options considered

| Option | Description | Cross-host session browse-ability | Implementation cost | Bug-class closure |
|--------|-------------|-----------------------------------|---------------------|-------------------|
| α      | Move sessions/ to host-local cache (`~/.cache/vibe-vault/sessions/`). Vault loses Job 2 entirely. | **Lost** — session notes don't appear on peer hosts | Smallest | Closes Job-1/Job-2 churn coupling |
| β1     | Local-only staging git repo per host; sessions never reach shared vault. | **Lost** — same as α, with durable host-local history | Moderate | Same as α + adds local commit log of every hook fire |
| β2     | Local-only staging git repo per host + wrap-time rsync into shared vault under per-host paths. | **Preserved** — shared vault sees per-host session subtrees, no merge surface overlap | Moderate-large | Closes everything α/β1 close, AND preserves operator-stated non-negotiable |
| γ      | Replace hooks with MCP-side JSONL transcript scraping. | Independent of capture *source*; orthogonal to where notes land. | Open-ended (per-harness) | Closes "no SessionEnd hook in Zed" (already partial), enables future TUI / IDE coverage |

**Operator constraint surfaced this session:** "Being able to peer
into session history has been very valuable in debugging
vibe-vault issues across multiple projects and hosts."

This eliminates α and β1. Cross-host session-note browse-ability
is non-negotiable. β2 is the only storage-layer answer that
preserves it while solving the coherence problem.

**Operator observation on γ:** "The most portable concept (but
not implementation details) across things like Zed and other
TUIs and IDEs that support session history. As I understand it,
this is the ONLY mechanism we have for Zed."

This pushes γ from "interesting future direction" to "the
long-run architectural shape." Hooks are a Claude-Code-specific
mechanism. The ecosystem is broadening — Zed already requires
non-hook capture, and Gemini-CLI / OpenClaude / future harnesses
will likely also lack a SessionEnd-equivalent.

---

## Decision

**Two-phase architectural direction. Both phases land. Phase 1
is the immediate finish line; Phase 2 is the long-running play.**

### Phase 1 — β2 (two-tier vault)

**One repo per concern.**

- The shared vault (`~/obsidian/VibeVault`, with `github` and
  `vault` remotes) holds Job-1 content only. Wrap is the only
  writer. Commit log is wrap-paced (~3/day).
- A staging repo lives at
  `~/.local/state/vibe-vault/<project>/` per project (XDG
  state dir, honoring `$XDG_STATE_HOME` when set, fully
  outside the vault tree). No remote. Hooks write the
  per-fire markdown here, and the staging package commits it
  via two fork-execs (`git add` + `git commit`) under a
  repo-local `vibe-vault@<sanitized-host>` identity written
  by `vv staging init`. The shared vault knows nothing about
  staging — no `.gitignore` rule, no path inside the vault
  tree refers to staging content.
- A wrap-time `vv vault sync-sessions` step mirrors the
  staging repo's working tree into the shared vault under a
  **per-host path layout** (`Projects/<p>/sessions/<host>/<date>/`)
  via a Go-native `filepath.WalkDir` + content-hash
  skip-if-equal copy (no `rsync` dep). Per-host paths mean
  no host ever writes a path another host writes — merge
  conflicts are structurally impossible.
- The shared vault's commit log gains one "sessions for
  host=<H>" commit per wrap (push-deferred via
  `CommitAndPushPaths(..., push=false)`; the terminal
  `vv vault push` rides one network push for narrative +
  sessions together). Wrap-paced, atomic, operator-authored.

**Considered and rejected: `Projects/<p>/sessions/.git/`
(sessions IS the staging repo).** The original v1 proposal
nested the staging git repo inside the shared vault at
`<vault>/Projects/<p>/sessions/.git/`, with the vault's
`.gitignore` excluding `Projects/*/sessions/` to keep
hook-fired content out of the vault commit log. Operator's
iter-203 `/review-plan` rejected this in favor of the XDG
host-local path because it eliminates three hazard classes
at once: no git-in-git nesting (no risk of an Obsidian git
plugin recursing into a nested `.git`), no vault-side
`.gitignore` management for staging content, and no Obsidian
file-explorer visibility for staging churn. Cross-host
browse-ability is preserved by the wrap-time mirror, which
remains the operator non-negotiable.

**Result:** cross-host session browse-ability preserved (every
host sees every other host's sessions/ subtree). Job-1
coherence becomes wrap-paced. Obsidian git plugin auto-commit
storm becomes harmless (vault working tree changes ~3x/day).
The mcp-driven-vault-sync freshness gate becomes a footnote
(narrative repo sees ~3 writes/day; cache-hit rate ~99%
without trying). The iter-196 / iter-199 / iter-198 bug
classes are eliminated by construction.

**Filed as:** `vault-two-tier-narrative-vs-sessions-split`
(to be drafted next).

### Phase 2 — γ (pluggable session source)

**One concept, specialized implementations per coding
environment.**

- Define `SessionSource` interface in
  `internal/sessionsource/`. Methods cover discovery
  ("what sessions exist?"), capture ("turn this session into
  a note"), and lifecycle hints ("this session is mid-flight
  vs ended").
- Refactor existing capture into two implementations:
  `claude-code-jsonl` (the SessionEnd / Stop / PreCompact
  hook) and `zed-acp` (the existing Zed track). Both already
  exist in different shapes; γ extracts the abstraction
  rather than designing one upfront.
- Add a third implementation: `claude-code-jsonl-direct` —
  MCP-side scraping of the live transcript, parallel to the
  hook (not replacing it; live comparison data tells us
  whether to deprecate the hook later).
- Future implementations land on demand: gemini-cli,
  open-claude, cursor, whatever ships next. Each is a
  ~200-LoC adapter that translates harness-specific
  transcript layout into the canonical SessionSource
  interface.

**Result:** session capture decouples from any single harness's
hook system. Notes still flow through the same β2 staging
repo + wrap-time sync regardless of source. Adding a new
harness becomes additive, not architectural.

**Filed as:** `session-source-interface` (to be drafted as
Draft, dispatch deferred until β2 is in flight or shipped).

---

## The unifying principle

> **One common concept, specialized implementations per coding
> environment, persisted through one storage architecture.**

- The *concept* is "a session is a sequence of turns producing
  decisions, threads, and artifacts." That's invariant across
  Claude Code, Zed, future TUIs.
- The *implementations* differ — JSONL transcripts, ACP
  protocol streams, future harness-specific formats. Each
  adapter knows its harness; nothing else does.
- The *storage* is two-tier vault + per-host paths. Storage
  doesn't care where notes came from. Capture doesn't care
  how they're persisted.

This is a clean three-layer split. Today's architecture
collapses all three layers into "the hook writes a markdown
file into the shared vault." That collapse is what produced
two days of incidents.

---

## Sequencing

| Order | Task                                          | Why this order                                      |
|-------|-----------------------------------------------|-----------------------------------------------------|
| 1     | `vault-push-selective-staging-cli` (~120 LoC) | Closes the `git add -A` contamination today, regardless of β2 progress. CLI-only; no MCP entanglement. |
| 2     | `vault-two-tier-narrative-vs-sessions-split` (β2) | The structural fix. Subsumes ~80% of `mcp-driven-vault-sync`. Subsumes most of `wrap-mcp-offload-state-collector-and-preflight` (or re-scopes it on top). |
| 3     | `mcp-driven-vault-sync-residual` (~80 LoC)    | What's left after β2: freshness gate for narrative repo + autostash `--include-untracked` fix. May not be needed at all once β2 lands; re-evaluate. |
| 4     | `session-source-interface` (γ as boundary)    | Refactor existing two implementations behind the interface. No new behavior. |
| 5     | `claude-code-jsonl-direct-source`             | First new γ implementation, runs in parallel with hook. Live comparison. |
| 6+    | Per-harness γ adapters as needed              | Gemini-CLI, OpenClaude, etc. Filed when a real user surfaces. |

Tasks 1, 2 are the immediate finish line. Task 3 may evaporate
once β2 is shipped — re-evaluate at that point rather than
pre-commit.

---

## Implications for currently active tasks

| Task                                                  | Disposition under this direction                      |
|-------------------------------------------------------|-------------------------------------------------------|
| `mcp-driven-vault-sync` (Draft v4)                    | **Retire after β2 task is filed.** β2 subsumes it; the residual fits in #3 above. |
| `wrap-mcp-offload-state-collector-and-preflight`      | **Re-scope on top of β2** OR retire if β2's wrap-time orchestration absorbs it. Decide at β2 plan-review time. |
| `grok-provider-support`                               | Independent. Proceed when prioritized. |
| `claude-code-tool-selection-telemetry`                | Independent. May feed γ-era telemetry surface; no blocker. |
| `mcp-call-tracing`                                    | Independent. Useful diagnostic for both β2 and γ rollouts. |
| `vault-rebase-resolver-policy`                        | Already shipped iter 197. Phase 1 of γ-era multi-source still benefits. |

---

## Open questions deferred to implementation time

The following are known-unknowns that should be resolved
inside their respective task plans, not pre-committed here:

1. **Staging repo init / migration.** First-time setup on
   existing vaults that already have committed sessions/
   content. Likely: rewrite vault history to drop sessions/
   from the working tree, OR leave existing committed
   sessions in place as a frozen archive and start fresh in
   the staging repo. β2 plan must pick one.
2. **session-index.json placement.** Currently single
   vault-side file. Under β2: per-host file aggregated at
   wrap time? Or per-host file pushed into the shared vault
   under the per-host path layout, with a synthesized
   "all-host" index built lazily on read? β2 plan picks.
3. **Wrap atomicity guarantees.** The wrap-time
   sync-sessions step adds a non-trivial chunk of work
   inside the wrap critical section. If it fails midway,
   what's the operator-visible state? β2 plan must
   specify rollback / retry semantics.
4. **γ interface shape.** Do not design upfront. Extract
   from the two existing implementations; the abstraction
   that drops out is the one to keep. If extraction reveals
   a third axis the existing two don't share, surface it
   then.
5. **Hook deprecation timeline under γ.** Live comparison
   data dictates this. Set a one-month observation window
   minimum after the JSONL-direct source ships in parallel.

---

## Anti-patterns this document blocks

If a future task plan proposes any of the following, this
document is the rebuttal:

- **"Make vault sync more clever / add a freshness gate at
  the MCP boundary."** That's `mcp-driven-vault-sync` v4.
  β2 makes it unnecessary by removing the workload that
  required it.
- **"Move sessions/ host-local to fix multi-host coherence."**
  That's α / β1. Operator-rejected because cross-host
  session browse-ability is load-bearing for cross-project
  debugging.
- **"Add a periodic sync ticker / cron / daemon."** Already
  rejected at v1→v2 of mcp-driven-vault-sync; β2 makes it
  doubly unnecessary because wrap-time sync is the only
  operation crossing the host boundary.
- **"Replace hooks with JSONL scraping now."** That's γ
  applied without β2. Solves the harness-portability
  problem but leaves the storage-coherence problem in
  place. β2 is sequenced first for a reason.
- **"Add per-tool freshness opt-in/opt-out config."** Q2 of
  mcp-driven-vault-sync v3 already settled this with a
  single-rule resolution; under β2 the question is moot.

---

## How to use this document

- Cite it in DESIGN.md as a single entry pointing here.
- Cite it in any task file under `Background` when the task
  touches vault sync, session capture, or multi-host
  coordination.
- Update it (don't replace it) when a phase ships — append a
  "Status as of iter N" stanza describing what landed and
  what's now active.
- Retire it only when β2 + γ Phase 1 have both shipped and
  the architectural direction is fully realized in code. At
  that point, fold the surviving content into ARCHITECTURE.md
  and delete this file.

---

## Status as of iter 205 (β2 shipped)

**Layout choice changed from the original proposal.** The
Decision section above originally pinned the staging repo at
`<vault>/Projects/<p>/sessions/.git/` (sessions-IS-staging);
v1 of the implementation plan inherited that. v2 of the plan
(iter 203 `/review-plan` revision) moved the staging dir to
`~/.local/state/vibe-vault/<project>/` (XDG state dir,
fully outside the vault). The Phase 1 prose above has been
updated in place to reflect the shipped layout, and the
original `sessions/.git/` proposal is preserved in a
"Considered and rejected" subsection within the same
section. The decision-history record (this doc's value) is
preserved; only the load-bearing layout pin was rewritten.

**What shipped under DESIGN #103.** Phases 1 → 4 landed on
the `vault-two-tier` branch:

- Phase 1 (`30805ff`) — `internal/staging/` package
  (`SanitizeHostname`, `Root`, `Init`, `Path`, `.init-done`
  sentinel); `vv staging init/status/path/gc/migrate`
  subcommands; `[staging] root` config field;
  `vaultsync.GitCommand` exported.
- Phase 1.5 (`7753ca6`) — `internal/index/rebuild.go`
  walker generalized to per-host AND
  `_pre-staging-archive/` subtrees;
  `NoteRelPathTimestamp(project, host, date, t, suffix)`
  takes an explicit host segment; dead `NoteRelPath`
  deleted; `vaultsync.Classify()` gains an explicit
  per-host arm; the dead grandparent-project fallback in
  `rebuild.go` deleted (frontmatter `project:` is
  mandatory).
- Phase 2 (`73fda8c`) — `session.CaptureOpts.StagingRoot`
  routes session writes through the staging package across
  all 5 entry points (hook, `vv process`, `vv backfill`,
  `vv reprocess`, Zed batch, MCP `vv_capture_session`);
  `staging.NotePath / Commit / EnsureInit / EnsureInitAt /
  ResolveRoot` introduced; `VIBE_VAULT_DISABLE_STAGING=1`
  env shim ships as the back-compat path for integration
  tests and a documented operator escape hatch.
- Phase 3 (`31fdaa2`) — `vv vault sync-sessions [--project | --all-projects]`;
  `staging.Mirror` + `staging.SyncSessions`; per-host
  `<vault>/Projects/<p>/sessions/<host>/index.json`
  written by the mirror; `CommitAndPushPaths(_, _, _, push bool)`
  push-deferred mode; `Pull` autostash gains
  `--include-untracked`.
- Phase 4 (`930aec3`) — `internal/index/aggregator.go`
  (`AggregateProject`); `Rebuild` refactored to call
  `AggregateProject` per project; `Host` field added to
  `SessionEntry`; intra-project SessionID collision is an
  error, cross-project collision WARNs and applies
  last-write-wins; the aggregator emits vault-relative
  paths and overwrites the absolute-path entries Phase 2's
  hook writes leave in the index.
- Phase 5 (this doc + `doc/ARCHITECTURE.md` "Two-tier vault"
  section + `doc/OPERATIONS.md` "Two-tier vault: staging +
  per-host sync" section + `doc/DESIGN.md` #103 status
  stanza + `doc/DESIGN.md` #6 host-isolation note +
  `README.md` multi-host paragraph rewrite). Iter 205.

**Phase 6 (live verification + retirement)** is the
remaining work. Once it lands, retire
`mcp-driven-vault-sync` (Draft v4) — superseded by β2 —
and re-evaluate
`wrap-mcp-offload-state-collector-and-preflight` against
β2's wrap-time orchestration to decide retire-vs-rescope.

**γ (pluggable session source)** remains queued behind β2.
Implementation is unchanged from the Phase 2 description
above.
