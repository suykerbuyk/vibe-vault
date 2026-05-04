# Operations Runbook

How to roll out a new vibe-vault binary, refreshed templates, and schema migrations across every project on every workstation in a multi-host fleet.

The piece-parts are documented elsewhere — DESIGN #41 (three-way baseline template sync), DESIGN #97 (MCPSurfaceVersion handshake), DESIGN #98 (worktree gc), DESIGN #103 (two-tier vault: host-local staging + per-host wrap-time sync), and the README's `vv context sync` section. This file is the single workstation-fleet runbook that integrates them.

## When to run this

Whenever any of the following land on `main`:

- A new `vv` binary version (anything in `cmd/vv/`, `internal/`, or `templates/`).
- A schema bump (`internal/context/schema.go` `LatestSchemaVersion`).
- A `MCPSurfaceVersion` bump (`internal/surface/version.go`).
- New embedded templates under `templates/agentctx/`.
- New MCP tools registered in `internal/mcp/server.go`.

If you're unsure, run it. Every step is idempotent — running with the binary already current is a no-op.

## Phase 1 — Developer workstation (the host that shipped the change)

Already done as part of the merging workflow:

```bash
cd ~/code/vibe-vault
git pull github main
make install              # rebuild + install to ~/.local/bin/vv (re-embeds templates, regenerates man pages)
vv context sync           # sync vibe-vault's own templates to its .claude/
vv worktree gc            # reap any subagent orphans from the dispatch
```

The `/wrap` advance to the next iter narrative is the operator's separate step.

## Phase 2 — Per-workstation rollout

For **every** workstation that runs vibe-vault. Order doesn't matter; each host is independent.

### 2a. Update binary

```bash
cd ~/code/vibe-vault
git pull github main      # discover-remote pattern; "github" is the canonical upstream
make install              # binary lands at ~/.local/bin/vv
```

### 2b. Verify the binary picked up the new surface

```bash
vv version                # confirm new commit hash
vv check --json | jq .checks[] | jq 'select(.name == "surface")'
                          # status must be "pass"; binary surface ≥ vault max
```

If `surface` is `fail`, the host's binary is older than what the vault expects. Re-run `git pull && make install`. If still failing, check `~/.local/bin/vv` is on `$PATH` ahead of any older system-wide `vv`.

### 2c. Sync vault state

```bash
vv vault pull             # auto-discovers configured remotes
```

If `vv vault pull` reports "regenerated", also run `vv index` once to rebuild auto-generated indexes. If it emits `WARNING: vault rebase kept LOCAL content`, see "Recovering Dropped Vault Narratives" below.

### 2d. Health check

```bash
vv check                  # all surface/schema/memory-link/resume-invariants checks should pass
```

`vv check` failures here are operator-action signals — read the detail field, follow the prescribed fix (e.g., `vv memory link <project>` for a missing memory symlink).

## Phase 3 — Per-project context sync

After binary + vault are current, refresh **each project's** repo-side context (`.claude/commands/`, CLAUDE.md, schema migrations).

### 3a. Identify projects on this workstation

```bash
# Each vault project that has a local source tree:
for d in ~/work/* ~/personal/* ~/opensource/* ~/code/*; do
  [ -d "$d/.git" ] && [ -d "$HOME/obsidian/VibeVault/Projects/$(basename "$d")" ] && echo "$d"
done
```

(Adjust the domain prefixes to match your `~/.config/vibe-vault/config.toml` `[domains]` table.)

### 3b. Sync each project

For every directory the script printed:

```bash
cd <project-path>
git pull                  # if the project's source repo has its own upstream
vv context sync           # three-way template merge (DESIGN #41)
vv check                  # confirms surface stamp + schema match
```

`vv context sync` reports per-file disposition:

| Verdict     | Meaning                                                         | Action |
|-------------|-----------------------------------------------------------------|--------|
| `UPDATE`    | Template changed; you didn't customize → auto-applied           | None   |
| `CREATE`    | New template file → created                                     | None   |
| `CONFLICT`  | Template changed AND you edited the live copy → skipped         | Triage (Phase 5) |
| (preserved) | You edited; template unchanged → kept your version              | None   |

### 3c. Vault-only projects (no source repo)

For projects that exist only in the vault (research notes, dashboards, retired projects), one host can run a single sweep:

```bash
vv context sync --all     # runs vault-side schema migrations across every Projects/<p>/
                          # NOTE: skips repo-side .claude/commands/ deployment
```

Run this from any project root (e.g. `cd ~/code/vibe-vault && vv context sync --all`). It's host-independent — once one workstation runs it, the migrations are recorded in the vault's schema versioning files and pulled by every other host's `vv vault pull`.

## Phase 4 — Worktree orphan cleanup (DESIGN #98)

Once per workstation:

```bash
vv worktree gc --dry-run  # inspect any stale .claude/worktrees/agent-* from prior sessions
vv worktree gc            # reap them (cross-session deterministic)
```

Going forward, the pre-bootstrap `/restart` template runs this automatically — manual invocation is only needed during the initial rollout to clean up pre-existing orphans.

`uncaptured-work` verdicts here mean a subagent's worktree branch has commits not in any candidate parent — surface to operator decision before re-running with `--force-uncaptured`.

## Phase 5 — Conflict triage

If Phase 3 reported any `CONFLICT`:

```bash
# Re-list conflicts cleanly
vv context sync --dry-run

# For each conflicting file, inspect the divergence:
diff -u <vault-path-to-file>.baseline <vault-path-to-file>

# Decide: keep your customization (do nothing — it stays as-is) OR accept upstream:
vv context sync --force --project <name>   # overwrites all of <name>'s customizations
```

Conflicts are rare and only happen when an operator intentionally edited a synced template. They aren't bugs.

## Phase 6 — Multi-machine sync hygiene

The vault is shared across hosts via git (`~/obsidian/VibeVault/`). Two write-side races are documented:

- **`wrap-anchor-rebase-stamps-swallow-substantive-work`** — local stamp/anchor topology can invert on rebase.
- **W1-vs-W2 wrap race** — two workstations wrapping concurrently produce duplicate iter numbers.

Mitigations operationally:

1. Always `vv vault pull` before `/wrap`. If you forget, the vault's `vv vault merge-driver` (registered in `~/.gitconfig` and the vault's `.gitattributes`) auto-resolves `.surface` conflicts; non-`.surface` conflicts surface to manual `git mergetool`.
2. Don't push wrap stamp commits across workstations simultaneously.
3. Stamp-only wrap commits direct-push to main (DESIGN #102; see "Direct-pushing wrap commits to main" below). If a direct push is unexpectedly rejected, a hidden Ruleset, organization-level rule, or pre-receive hook is gating; capture the rejection message and pivot to a feature-branch PR for that wrap while the protection state is investigated.

## Two-tier vault: staging + per-host sync

`β2` (DESIGN #103) splits the shared vault into two substrates: hook-paced
session writes land in a host-local staging git repo at
`~/.local/state/vibe-vault/<project>/` (or `$XDG_STATE_HOME/vibe-vault/<project>/`
when set); wrap-time `vv vault sync-sessions` mirrors each host's staging
working tree into `<vault>/Projects/<p>/sessions/<host>/<date>/`. Per-host
paths mean no two hosts ever write the same session-content path — merge
conflicts on session content are structurally impossible.

### `vv staging init <project>` — bootstrap the host-local staging dir

Creates `<staging-root>/<project>/` as a fresh git repo with repo-local
`user.email = vibe-vault@<sanitized-host>` and `user.name = vibe-vault`,
then drops a `.init-done` sentinel. Idempotent: re-running on an already-
initialized project reports status without re-initializing. The hook
auto-inits in-process on first fire if the sentinel is absent, so this
is rarely required by hand — explicit invocation is useful when
provisioning a new workstation, when the staging root has been wiped, or
when scripting a fleet rollout.

### `vv staging status / path / gc <project>` — operator commands

- `vv staging status <project>` — reports staging dir presence,
  working-tree state, last commit timestamp.
- `vv staging path <project>` — prints the resolved staging dir path.
  Useful for `cd $(vv staging path foo)` to inspect the staging repo
  directly.
- `vv staging gc <project>` — wraps `git gc --auto` on the staging repo.
  Hooks commit every fire, so the staging repo grows unbounded over
  time; `vv staging gc` is documented as the operator-invoked
  housekeeping path. Not invoked automatically.

### `vv staging migrate [--all-projects | --project <p>]` — one-time pre-β2 migration

Detects projects with committed sessions under
`<vault>/Projects/<p>/sessions/*.md` (the pre-β2 flat layout) and moves
them in-place to `<vault>/Projects/<p>/sessions/_pre-staging-archive/`
via `git mv` so per-file history follows the rename. Splits commits
per-project (one commit per project, not one ~800-file commit) for
diffability and per-project revert. The `session-index.json` `note_path`
entries are rewritten in-place to point at the new archive paths so MCP
tools and `vv inject` keep working immediately.

**`_pre-staging-archive/` is permanent vault history and must never be
deleted.** It records every pre-β2 session and is the only path under
which those notes exist post-migration. The walker treats it the same
as any per-host subtree.

### `vv vault sync-sessions [--project <p> | --all-projects]` — wrap-time mirror

For each in-scope project, mirrors `<staging-root>/<project>/` into
`<vault>/Projects/<p>/sessions/<host>/`, then commits the mirrored
paths via `vaultsync.CommitAndPushPaths(..., push=false)` — local
commit only, no network push. The terminal `vv vault push` performs
the single network push for ALL pending vault commits (narrative +
sessions), preserving the single-push wrap invariant.

- **`--all-projects` is the default.** Projects are enumerated from
  `<staging-root>/*/` (staging is the source of truth for "what has
  unsynced changes"), NOT from `<vault>/Projects/*/`.
- **Per-host conflict guarantee.** Each host writes only into
  `<vault>/Projects/<p>/sessions/<host>/`; cross-host content never
  shares a path. Merge conflicts on session content are structurally
  impossible.
- **Idempotent.** The mirror is a Go-native walk + content-hash
  skip-if-equal copy. No staging changes → empty mirror diff → no
  vault commit, no error.
- **Push-deferred.** Sync-sessions commits land locally. The wrap
  procedure already runs `vv vault push` as its terminal step; the
  sync-sessions commit rides that single push.

### Hostname rename + `VIBE_VAULT_HOSTNAME` escape hatch

Sessions captured under an old hostname remain under that host's
subtree forever. After a rename, new sessions land under the new
hostname's subtree; the old subtree remains browsable but inert. To
land mid-rename sessions under the original subtree (e.g., during a
cutover window where some captures should still be attributed to the
prior name), set:

```bash
export VIBE_VAULT_HOSTNAME=oldhost
```

This overrides `os.Hostname()` resolution at
`internal/meta/provenance.go:68`. Documented as the supported
rename-recovery path; unset the var when cutover is complete.

### Migration recipe (zero-explicit-step path)

After β2 ships, hook-routed captures emit absolute staging-dir paths
into `session-index.json` until the first `vv vault sync-sessions`
runs. The aggregator emits vault-relative paths and overwrites those
absolute-path entries on the next `vv index` call. The expected order
on a fresh-β2 workstation:

1. Hook fires (absolute paths land in the index — transient state).
2. `/wrap` runs `vv vault sync-sessions`, mirroring staging into the
   per-host vault subtree.
3. `vv index` (also part of `/wrap`'s post-sync sweep, or invoked
   manually) walks per-host subtrees and rewrites entries to
   vault-relative form.

No explicit migration command is needed — the absolute-path entries
are self-healing on the next index pass.

### Staging vs vault commit identity

Staging commits use the repo-local `vibe-vault@<sanitized-host>`
identity written by `vv staging init`; vault commits use the
operator's global git identity. This asymmetry is intentional:

- Staging is a host-local commit substrate that records every hook
  fire as durable debugging history. Attribution to the binary
  identity makes the staging repo's log inspectable as
  "what did the hook see?", independent of whoever was at the
  keyboard.
- The vault is operator-authored and remains so; sync-sessions
  commits ride the operator's identity and appear in the vault log
  alongside narrative commits.

Hook never depends on global `git config --global user.email`; the
repo-local identity from `vv staging init` is sufficient.

### Cross-project SessionID collision WARN signal

The aggregator hard-errors on intra-project SessionID collisions and
WARNs (last-write-wins) on cross-project collisions. A cross-project
WARN is almost always a project clone with overlapping captures — two
project directories sharing the same git remote (and therefore the
same project name) where both have routed captures into the same
session log. Investigate; the right fix is usually to disambiguate one
of the project identities via `.vibe-vault.toml`.

### Integration tests + `VIBE_VAULT_DISABLE_STAGING=1`

Integration tests that pre-date β2 still write directly into the
shared vault under the legacy flat layout via the
`VIBE_VAULT_DISABLE_STAGING=1` env shim. The shim is also a documented
operator escape hatch for the migration window — set the env var to
fall back to pre-β2 routing if a host's staging dir is wedged and the
operator wants to capture a session before debugging the staging-side
issue. Tests will grow `vv vault sync-sessions` steps as part of γ
work; until then the shim is the supported back-compat path.

## Recovering Dropped Vault Narratives

When `vv vault pull` rebases local commits onto upstream, file conflicts on Manual-class files (`iterations.md`, `resume.md`, `tasks/*`, `knowledge.md`) are auto-resolved by keeping the LOCAL side. This is the right policy operationally — local work is the most recent operator intent on this machine and the rebase target's content remains reachable from `main` — but the upstream-side file content is dropped from the working tree at that point. `vv vault pull` surfaces the drop on stderr:

```
WARNING: vault rebase kept LOCAL content; the following upstream commits' file content was dropped:
  Projects/foo/iterations.md  (from a1b2c3d: "iter-N narrative")
Inspect with: vv vault recover [--days N]
```

### Why this happens

The race is two-machine same-iter wrap. Machine A wraps iter N and pushes; machine B (within the race window) wraps iter N independently. B's `vv vault pull` (or its `CommitAndPush` rebase fallback) hits a conflict on `iterations.md` because both sides edited the same file. The resolver keeps B's local content uniformly across all four file classes (Manual, Regenerable, AppendOnly, ConfigFile), per the keep-local-on-Manual policy. A's commit is **not lost** — it's still reachable from `main` after B's push lands; only the file content on B's working copy was dropped during merge resolution.

### Recovery flow

1. **List candidates** with `vv vault recover` (defaults to the past 7 days of reachable history). Output is a table of `{age, sha, subject, files}` rows — one row per upstream commit whose Manual-class blob differs from HEAD's:

   ```
   AGE  SHA      SUBJECT                                                       FILES
   2h   a1b2c3d  iter-N narrative                                              Projects/foo/iterations.md
   ```

2. **Inspect a candidate** two ways:

   - `vv vault recover --show <sha>` runs `git show <sha>` so the operator sees the full commit (message, author, diff against its parent).
   - `vv vault recover --diff <sha> -- <path>` prints the dropped blob (`git show <sha>:<path>`) and the current HEAD blob (`git show HEAD:<path>`) side-by-side so the operator can see what was kept versus what was dropped.

3. **Manually integrate** the dropped content. There is no `--apply` flag in v1 by design — ordering, renumbering, and merge style for `iterations.md` are operator judgment calls. Open the file in an editor, fold in the dropped narrative as appropriate, save.

4. **Commit and push** as normal: `cd ~/obsidian/VibeVault && git add -A && git commit -m '<message>' && vv vault push` (or let the next `/wrap` cover the commit + push).

### Window

`--days N` extends the walk depth (default 7, no upper cap — the cost is `git log` traversal). If a candidate is older than the default window, pass `--days 30` (or longer) explicitly; there is no silent truncation.

### Cross-machine

The recovery walks **reachable history from HEAD**, not the local reflog. This is load-bearing: after B's rebase pushes to the remote, A's commits remain reachable from `main`, so recovery works identically on either machine. There is no multi-machine reflog asymmetry to worry about; whichever host runs `vv vault recover` after a `vv vault pull` will see the same candidate set, regardless of which host originally produced the dropped commit.

## Direct-pushing wrap commits to main

After iter 197 the `required_status_checks` subresource was deleted from `main` branch protection (DESIGN #102). Stamp-only wrap commits — those whose entire diff is `.vibe-vault/last-iter` — direct-push to main without a PR cycle. Substantive commits continue to ship through PRs with an operator-visual CI check.

The wrap.md template's Stage 5 carries the pre-push gate; this section colocates the procedure with the four operator commitments for ongoing reference.

### Pre-push gate (mandatory on every wrap)

After `git commit -F commit.msg` and BEFORE `git push`, run:

```bash
git diff --name-only HEAD~1 HEAD
```

The output dictates the push path:

- **Output is exactly `.vibe-vault/last-iter`** → safe to direct-push: `git push github main`. The `detect-admin-commit` workflow short-circuits Lint+Test to ~20s green post-push, leaving main green.
- **Output contains ANYTHING else** → DO NOT direct-push. Open a PR via the standard feature-branch flow. The operator visually confirms green Lint+Test on the PR before merging.

This pre-flight check is the single point where operator discipline gates main against substantive direct-push regression. There is no server-side `required_status_checks` gate to catch a missed check.

Iter-shape examples observed in this project's history:

- Iter 196 wrap: diff is `.vibe-vault/last-iter` only → direct-push eligible.
- Iter 197 wrap: diff is `.vibe-vault/last-iter` only → direct-push eligible.
- Iter 195 wrap: diff is `.vibe-vault/last-iter` + `doc/DESIGN.md` + `doc/TESTING.md` + test files → PR required.

Stamp-only wraps are common but not universal; planning iters that file new tasks, DESIGN entries, or doc updates often land mixed content alongside the stamp.

### Operator commitments (verbatim from DESIGN #102)

Server-side enforcement of "main is always green" is replaced by operator discipline. The operator explicitly accepts:

1. **Substantive commits go through PRs.** Direct push is reserved for stamp-only wrap commits and operator-judgment-call administrative diffs (e.g., emergency hotfixes documented inline). Discipline, not enforcement.
2. **PR merge requires a visual CI check.** GitHub will enable the merge button regardless of CI status. The operator visually confirms green Lint+Test on the PR before merging. Same information as before, no longer blocking.
3. **Red main is recoverable.** If a buggy substantive direct push slips operator discipline, the post-push workflow surfaces red on main; revert via a normal PR (or an operator-direct push under the same model) returns main to green. `enforce_admins: true` continues to block force-push so revert is the only path.
4. **No CI alarms exist by default.** If sustained red main becomes a real concern, file a follow-up task to add a workflow that fails loudly when main has unreverted red commits. Out of scope here.

### `vv stamp_iter` is an MCP tool, not a CLI subcommand

The stamp file `.vibe-vault/last-iter` is written via the MCP tool `vv_stamp_iter` from inside `/wrap` Stage 4 — **not** by an operator-invoked CLI command. There is no `vv stamp_iter` CLI subcommand to run by hand. Stage 5 of `/wrap` stages the stamp file alongside the iter narrative + agentctx files, runs `git commit -F commit.msg`, applies the pre-push gate above, and pushes. The operator never invokes `stamp_iter` directly.

If you find yourself reaching for "let me just stamp the iter manually" outside `/wrap`, stop — the stamp is part of the wrap commit's atomicity contract and lives behind the Stage 4 sequence (append iteration → resume update → thread mutations → commit msg → stamp → session capture). Out-of-band stamping breaks DESIGN #93's mechanical anchor.

### Cross-reference

- DESIGN #102 — protection-relaxation rationale, the v1/v2 alternatives explored, and the trade-off framing.

## Selective vault push (`--paths`)

`vv vault push` accepts an opt-in `--paths <pathspec>` flag (repeatable) that stages only the listed paths via `git add -- <paths>...` instead of the default catch-all `git add -A`. The default behaviour with no flag is unchanged: every dirty path in the vault working tree is swept into the commit, preserving today's ad-hoc-cleanup ergonomics. The flag is opt-in for callers that know exactly which files belong to the work unit they are publishing.

### The contamination scenario it closes

Two Claude Code sessions run on the same workstation against the same vault. Session A is wrapping `Projects/foo`; session B is mid-flight editing `Projects/bar/iterations.md` and has an unsaved scratch note in `Projects/bar/agentctx/notes.md`. Both sessions share the same `~/obsidian/VibeVault/` working tree. Without `--paths`, A's wrap-time `vv vault push` runs `git add -A` and sweeps B's dirty `Projects/bar/*` files into A's commit — B's in-flight scratch is now published under A's commit subject, attributed to A's iter narrative. With `--paths`, A names only its own `Projects/foo/...` files; B's working-tree edits stay dirty and untouched, ready for B to commit on its own schedule.

### CLI examples

Single-path push — narrative-only update:

```bash
vv vault push --paths Projects/foo/agentctx/iterations.md
```

Commits exactly `Projects/foo/agentctx/iterations.md`. Any other dirty file in the vault working tree (in `Projects/foo/` or anywhere else) stays dirty.

Multi-path push — wrap-shape commit covering iter + resume from one project:

```bash
vv vault push --paths Projects/foo/agentctx/iterations.md \
              --paths Projects/foo/agentctx/resume.md
```

Commits exactly those two files. Concurrent dirty files in `Projects/bar/` (or any other project the operator has not named) remain in the working tree.

### Catch-all is still the default

`vv vault push` with no `--paths` flag preserves today's behaviour — `git add -A` of every dirty path. This is the right semantics for the operator-runs-cli case (sweeping miscellaneous notes after a writing session, recovering after a power-cycle, etc.) where naming each file would be tedious. The new flag is opt-in for callers that want contamination safety.

### Recovery if a contaminated commit slipped through

If `vv vault push` (catch-all form) published an unintended file alongside the intended set, recover with the standard git workflow. If the contaminated commit has not yet been pushed to a shared remote, soft-reset and re-stage selectively:

```bash
cd ~/obsidian/VibeVault
git reset --soft HEAD~1                # uncommit, keep the index + working tree
git reset HEAD -- <unintended-paths>   # unstage the contaminated subset
vv vault push --paths <intended-path-1> --paths <intended-path-2>
# Then commit the unintended subset on its own — typically from the other session that owns it.
```

If the contaminated commit has already been pushed, revert and re-publish the intended subset:

```bash
cd ~/obsidian/VibeVault
git revert <sha>                       # produces a revert commit
git push                               # publish the revert
vv vault push --paths <intended-path-1> --paths <intended-path-2>
# Re-stage and push the unintended subset separately.
```

### Forward note

`--paths` is now load-bearing under β2's `vv vault sync-sessions` (DESIGN
#103). The sync orchestrator names the per-host session subtree it just
mirrored (`Projects/<p>/sessions/<host>`) explicitly — `vaultsync.CommitAndPushPaths(vault, msg, [<that path>], push=false)` —
so its commit cannot accidentally sweep concurrent dirty content from
other hosts' subtrees, narrative files, or another in-flight session's
work. The opt-in operator flag and the β2-mandatory orchestrator path
share the same `--paths`/`paths []string` plumbing; the contamination
safety the flag offers operators is the same safety β2 relies on for
the wrap-time mirror. Operators retain the opt-in behavior for ad-hoc
pushes; β2 selects it unconditionally for sync-sessions.

## Automation: cron-based freshness (carried thread, not yet deployed)

Two cron lines per workstation deploy `vv-binary-freshness-guard` Mechanism F (DESIGN reference: the freshness-guard task in `tasks/done/`):

```cron
# Every 15 minutes: pull vault if working tree is clean (logs to /tmp/vv-vault-pull.log)
*/15 * * * *  cd ~/obsidian/VibeVault && [ -z "$(git status --porcelain)" ] && /home/$USER/.local/bin/vv vault pull >> /tmp/vv-vault-pull.log 2>&1

# Weekly: rebuild + reinstall vibe-vault binary
0 6 * * 0  cd ~/code/vibe-vault && git pull github main && make install >> /tmp/vv-weekly-install.log 2>&1
```

Adjust paths to match your shell's `$HOME` expansion. After deploy, verify with `tail /tmp/vv-vault-pull.log` after ≥ 30 min of clean idle.

### Relationship to β2 sync-sessions

β2 (DESIGN #103) wrap-time `vv vault sync-sessions` does NOT obviate the
freshness-guard cron. The two address different concerns:

- The cron is about **narrative-repo freshness** — keeping the shared
  vault's `agentctx/`, `tasks/`, `iterations.md`, `resume.md`, etc.
  in sync across machines on a 15-minute cadence so that bootstrap
  context on host B reflects host A's recent narrative writes without
  requiring an explicit `vv vault pull`.
- `vv vault sync-sessions` is about **session-content publication** —
  mirroring host-local staging into the shared vault under per-host
  paths, on the wrap cadence.

Both run against the same shared vault, but they cover non-overlapping
file sets (narrative content lives outside `Projects/*/sessions/`;
sessions live inside it). Under β2 the vault's working-tree change
rate drops to wrap-paced (~3/day) regardless of per-host hook fire
rate, so the cron's `[ -z "$(git status --porcelain)" ]` clean-idle
gate fires far more often — making the freshness-guard cron *more*
effective, not less. Re-evaluate the deployment if the
`mcp-driven-vault-sync-residual` task surfaces a sharper bound; for
now the cron remains the supported narrative-freshness mechanism.

## Inventory matrix (sample for a single host)

For each project your fleet runs vibe-vault under, record once which workstations have a local source tree:

| Project              | s76 | bd770i | Notes                       |
|----------------------|-----|--------|-----------------------------|
| vibe-vault           | ✓   | ✓      | The binary itself           |
| rezbldr              | ✓   |        |                             |
| vibe-palace          | ✓   |        |                             |
| recmeet              | ✓   |        |                             |
| (vault-only project) | —   | —      | `vv context sync --all` covers it from any host |

When a workstation joins the fleet:

1. Run Phase 2 once.
2. For each project the workstation should run, run Phase 3 once.
3. Add the cron lines from "Automation" above.
4. `vv memory link <project>` per project (auto-memory symlink — required for vault-resident memory).

## Quick reference

- **Single workstation, all updates:** Phase 2 → Phase 3 (per project) → Phase 4 → Phase 5 (if needed).
- **Workstation-fleet rollout for a single PR:** repeat Phase 2 + Phase 3 on each workstation. Order doesn't matter.
- **Vault-only project changes:** `vv context sync --all` from any host once; other hosts pick up via `vv vault pull`.
- **Health check anywhere:** `vv check` — surface, schema, hooks, MCP, memory-link, resume-invariants.

## Related references

- DESIGN #7 — three-direct-deps philosophy.
- DESIGN #41 — three-way baseline template sync mechanics.
- DESIGN #48 — auto-memory host-side symlink semantics.
- DESIGN #97 — MCPSurfaceVersion handshake + merge-driver.
- DESIGN #98 — worktree gc lifecycle.
- DESIGN #101 — vault rebase resolver policy + reachable-history recovery contract.
- DESIGN #102 — drop `required_status_checks` on `main`; operator-discipline-gated direct push for stamp-only wrap commits.
- DESIGN #103 — two-tier vault: narrative repo + host-local staging + per-host wrap-time mirror (β2). See `doc/SESSION-CAPTURE-ARCHITECTURE.md` for the long-running architectural reminder.
- README §"Schema migrations" and §"`vv context sync`".
- Carried-forward thread `cross-project-template-propagation` (in resume.md): the per-project sweep is mechanical via `vv context sync` once Phase 2 is done.
- Carried-forward thread `freshness-guard-cron-deployment-pending`: the cron deployment in §Automation hardens this.
