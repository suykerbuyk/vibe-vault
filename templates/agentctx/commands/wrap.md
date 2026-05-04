# /wrap

Wrap the current iteration into the vault. The slash command is the
orchestrator: it inspects iter state, classifies the work-unit shape,
composes the iter narrative inline, and applies mutations
mechanically. Source-control history and the vault filesystem are the
canonical record; the orchestrator's job is to write short, accurate
prose using its full session context — no LLM render dispatch, no
tier selection.

This is **the** wrap path. There is no fallback.

## Work-unit shapes

Pattern-match on `vv_describe_iter_state` output plus four
slash-command-computed fields:

| Shape | Match rule |
|---|---|
| `fresh-feature` | `commits_since_last_iter` non-empty |
| `planning` | `commits_since_last_iter` empty AND `task_deltas.added` non-empty |
| `bookkeeping` | `commits_since_last_iter` empty AND `task_deltas.added` empty |
| `writes-already-landed` | `vault_has_uncommitted_writes` true AND `iterations.md` already contains the entry for `iter_n+1` |

The four `(commits-empty, task-added-empty)` Cartesian cells partition
cleanly: any commits → `fresh-feature` (new task in the same iter is
mentioned in narrative, no separate dispatch); empty + new task →
`planning`; empty + no task → `bookkeeping`. `writes-already-landed`
is the orthogonal short-circuit and wins on tie via the existing
"prefer most restrictive" rule.

The `vault_has_uncommitted_writes` field drops out of three of the
four rules. After always-stamps, vault dirty/clean is purely about
when in the wrap sequence the describe-iter-state call happens, not
about the iter's shape.

`bookkeeping` (empty window + no new task) covers two cases that
previously lived in separate shapes: post-merge reconciliation of a
previously-wrapped feature branch, and pre-staged vault-only work.
Both produce the same minimal narrative + mechanically-composed
commit message (`chore(wrap): stamp iter N`).
`writes-already-landed` short-circuits the narrative entirely.

`bookkeeping` replaces the retired `vault-only` and `reconciliation`
shapes (DESIGN #93). Rebase-merge and squash-merge of a previously-
wrapped feature branch land naturally in `bookkeeping` because the
merge produces no project work between anchor (the rebased/squashed
wrap commit) and HEAD.

## Pre-flight

0. **Surface handshake (DESIGN #97).** Run `vv check --json` and
   parse the `surface` check. If `status` is `"fail"`, the vault is
   ahead of this host's binary — halt and report the actionable
   error; do not wrap with a stale binary. The standard remediation
   is `cd ~/code/vibe-vault && git pull && make install`.
1. `make pre-commit` clean before wrapping. Wrapping over a dirty
   pre-commit produces a misleading iter record.
2. **Merge-pattern timing.** When the project uses a feature-branch
   merge pattern, run `/wrap` on the feature branch BEFORE merging.
   Wrapping after merge produces `bookkeeping` instead of
   `fresh-feature`, losing the per-decision narrative.

## Procedure

### Stage 1 — State collection

Call `vv_describe_iter_state` for `iter_n`, `branch`,
`vault_has_uncommitted_writes`, `last_iter_anchor_sha`. Then compute
four slash-command fields, anchored by `last_iter_anchor_sha`:

- `last_iter_anchor_sha` is `git log -n 1 --format=%H --
  .vibe-vault/last-iter` — the SHA of the most recent commit that
  wrote the iter stamp file. Empty when the project hasn't run a
  post-DESIGN-#93 wrap yet; the orchestrator then substitutes the
  output of:

      git rev-list --max-parents=0 HEAD | tail -1

  which is the project's oldest root commit. Deterministic, single
  command, no operator judgment. The resulting `commits_since_
  last_iter` window spans the project's entire history; shape
  classification falls naturally into `fresh-feature` (or
  `planning` if the only delta is a new task file). One-time
  transition per project — the wrap that lands under this fallback
  writes the stamp, and every subsequent wrap anchors mechanically.

- `commits_since_last_iter` — `git log --format="%H %s"
  <anchor>..HEAD`. Parse into `[{sha, subject}]`.
- `files_changed` — `git diff --name-only <anchor>..HEAD`.
- `task_deltas` — walk `agentctx/tasks/` against `git show
  <anchor>:agentctx/tasks/`:
  - `added` — slugs present at HEAD but absent at the anchor
  - `retired` — slugs whose `status:` at HEAD is `shipped`/`retired`
    and was not at the anchor
  - `cancelled` — slugs whose `status:` at HEAD is `cancelled` and
    was not at the anchor
- `test_counts` — read `doc/TESTING.md` headline
  (`<unit> unit / <integration> integration / <lint> lint`) when
  present; otherwise enumerate via the project's test runner. Use `0`
  for any counter that does not apply.

### Stage 2 — Shape classification

Apply the rules from "Work-unit shapes" above. Exactly one shape must
match; if zero or more match, prefer the more restrictive (e.g.
`writes-already-landed` over `bookkeeping`) and proceed.

### Stage 3 — Compose narrative inline

The orchestrator (Claude Code) writes the iter narrative directly
using its full session context. No render dispatch, no tier
selection, no LLM round-trip.

Compose the iter narrative covering:

- Shape-appropriate framing (`fresh-feature` cites commits/files;
  `planning` cites the new task slug + decisions; `bookkeeping`
  acknowledges work substance even when the commit graph is empty;
  `writes-already-landed` short-circuits — no narrative).
- Citation discipline: SHAs + file paths + line numbers when
  referenced.
- Summary line ≤200 chars (single sentence, no newlines) for the
  iterations.md front-matter.
- Compose the conventional-commit subject + body inline.

Pass the orchestrator-composed prose directly to `vv_append_iteration`
(Stage 4) with `summary` and `shape` args populated. The commit
message is composed inline and written via `vv_set_commit_msg`.

### Stage 4 — Mechanical apply

Order matters: iter row, then thread/carried, then commit msg, then
session capture.

1. **`vv_append_iteration`** — append the new row to `iterations.md`.
   Skip on `writes-already-landed`. Pass the orchestrator-composed
   `summary` and the Stage-2 shape verbatim as optional args
   (`summary=<from orchestrator>`, `shape=<classified>`) so the
   writer emits a YAML front-matter block between the heading and
   body. The block feeds `vv_get_iterations(format=summary)` at the
   structured-digest fast path; absence falls back to first-paragraph
   extraction at read time without breaking anything. For
   `bookkeeping` shape: pass `shape="bookkeeping"`; the orchestrator
   still produces `summary`. The auto-heal hook re-renders the
   resume.md state-derived regions (`vv:active-tasks`,
   `vv:current-state`, `vv:project-history-tail`) from filesystem
   ground truth post-write — no separate marker-block step required.
2. **`vv_update_resume`** — mutate resume.md narrative sections only
   when the wrap carries non-state changes. Auto-heal hook also fires.
3. **Thread/carried mutations** as needed:
   - `vv_thread_insert(slug, body, anchor?)` — open new thread
   - `vv_thread_replace(slug, body)` — update existing thread
     (hard-errors on slug ambiguity; refine slug if needed)
   - `vv_thread_remove(slug)` — close a thread (same hard-error
     semantics)
   - `vv_carried_add(slug, title, body)` — add carried-forward bullet
   - `vv_carried_remove(slug)` — drop a carried-forward bullet
   - `vv_carried_promote_to_task(slug, task_path)` — promote a carried
     bullet into a full task file
4. **`vv_set_commit_msg`** — write the orchestrator-composed
   `commit_subject` + blank line + `commit_prose_body` to
   `commit.msg` at project root. Single `content` field; markdown
   verbatim.
5. **`vv_stamp_iter`** — write the iter number to
   `.vibe-vault/last-iter`. Required for every wrap shape. The
   file is the canonical project-side anchor used by Stage 1 of
   the next wrap; skipping it leaves the next wrap's anchor
   pointing at this iter's predecessor and produces incorrect
   `commits_since_last_iter` windows. Stage 5 git-add must include
   `.vibe-vault/last-iter`.
6. **`vv_capture_session`** — record summary, tag, decisions,
   files_changed, open_threads.

### Stage 5 — Git plumbing (project side)

- `git add` the explicit paths in `iter_state.files_changed` plus
  the agentctx files actually modified by Stage 4
  (`agentctx/iterations.md`, `agentctx/resume.md`, any
  `agentctx/tasks/*.md` written during the iter), plus the project
  iter stamp file `.vibe-vault/last-iter`. Never use `git add -A`
  or `git add .`.
- `git commit -F commit.msg` — uses the file written by
  `vv_set_commit_msg`.
- **Pre-push gate (operator-mandatory).** After `git commit -F
  commit.msg` and BEFORE `git push`, run:

      git diff --name-only HEAD~1 HEAD

  The output dictates the push path:

  - **Output is exactly `.vibe-vault/last-iter`** → safe to direct-
    push: `git push github main`. The `detect-admin-commit`
    workflow short-circuits Lint+Test to ~20s green post-push,
    leaving main green.
  - **Output contains ANYTHING else** → DO NOT direct-push. Open
    a PR via the standard feature-branch flow. The operator
    visually confirms green Lint+Test on the PR before merging.

  This pre-flight check is the single point where operator
  discipline gates main against substantive direct-push regression.
  Performing it on every wrap is mandatory under the current model
  (DESIGN #102) — there is no server-side `required_status_checks`
  gate to catch a missed check.

  Iter-shape examples observed in this project's history:

  - Iter 196 wrap: diff is `.vibe-vault/last-iter` only → direct-
    push eligible.
  - Iter 197 wrap: diff is `.vibe-vault/last-iter` only → direct-
    push eligible.
  - Iter 195 wrap: diff is `.vibe-vault/last-iter` + `doc/DESIGN.md`
    + `doc/TESTING.md` + test files → PR required.

  Stamp-only wraps are common but not universal; planning iters
  that file new tasks, DESIGN entries, or doc updates often land
  mixed content alongside the stamp.

### Stage 6 — Vault sync

1. **`vv vault sync-sessions`** — mirrors host-local staging
   (`<XDG_STATE_HOME>/vibe-vault/<project>/`) into
   `<vault>/Projects/<p>/sessions/<host>/` for every project with
   pending changes, writes the per-host `index.json`, and creates one
   LOCAL commit per project. No remote push happens here. Idempotent:
   re-running with no source changes performs zero copies AND zero
   commits, so calling it on every wrap is safe (and required —
   without it, hook-fired session notes never reach the shared vault).

2. **`vv vault push`** — commits any remaining vault writes (narrative:
   `iterations.md`, `resume.md`, tasks, last-iter stamp) AND pushes
   ALL pending vault commits (the per-project sync-sessions commits
   from step 1 plus the narrative commit) to all configured remotes
   in a single network round-trip. The deferred-push design preserves
   the single-push wrap invariant: one network operation per wrap,
   regardless of how many projects produced session content.

   On feature-branch merge patterns, run vault push **after** the
   project-side push but **before** the upstream merge — the vault
   iter row references the project commit, so the project commit must
   exist on a pushed branch by the time the vault record is published.

   `vv vault push --paths <p1> --paths <p2>` is available when the
   caller knows the explicit list of vault files it intends to
   publish; it stages only those paths and leaves any other dirty
   file in the vault working tree untouched. Today's wrap procedure
   uses the catch-all form because the per-host sync-sessions step
   has already pre-committed the session subtrees with explicit-path
   precision.

## Flags

- `/wrap` — runs the full wrap procedure.
- `/wrap --dry-run` — run Stages 1–3 and print the composed prose
  plus proposed mutations without applying.

## Schema reminders

- `vv_set_commit_msg`: single `content` field. Pass
  `<commit_subject>\n\n<commit_prose_body>` verbatim.
- `vv_thread_replace` / `vv_thread_remove`: hard-error on slug
  ambiguity. Refine the slug with more of the title text until
  exactly one section matches.

## Commit message rules

- Conventional-commit prefix (`feat:`, `fix:`, `refactor:`, `chore:`,
  `docs:`, `test:`, `build:`, `ci:`) inferred from the work shape.
- Include design-decision numbers in parens when present
  (e.g. `feat(wrap): ... (DESIGN #92)`).
- No trailing period on the subject line.
- No `Co-Authored-By` lines, no AI attribution, no "Generated with X"
  trailers — neither in commit messages nor in source files.

Do not ask for confirmation — just do the updates, stage the files,
show what changed, and note that the user should review before
committing.
