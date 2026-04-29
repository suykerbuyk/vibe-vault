# /wrap

Wrap the current iteration into the vault using the surgical-render
path. The slash command is the orchestrator: it inspects iter state,
classifies the work-unit shape, calls a single render tool when prose
is needed, and applies mutations mechanically. Source-control history
and the vault filesystem are the canonical record; the LLM's job is to
write short, accurate prose when prose is needed — nothing more.

This is **the** wrap path. There is no fallback.

## Work-unit shapes

Pattern-match on `vv_describe_iter_state` output plus four
slash-command-computed fields:

| Shape | Match rule |
|---|---|
| `fresh-feature` | `commits_since_last_iter` non-empty AND `task_deltas.added` empty AND `vault_has_uncommitted_writes` false |
| `planning` | `commits_since_last_iter` empty AND `task_deltas.added` non-empty AND `vault_has_uncommitted_writes` false |
| `reconciliation` | `commits_since_last_iter` is a single merge commit AND `vault_has_uncommitted_writes` false |
| `vault-only` | `commits_since_last_iter` empty AND `task_deltas` empty AND `vault_has_uncommitted_writes` true |
| `writes-already-landed` | `vault_has_uncommitted_writes` true AND `iterations.md` already contains the entry for `iter_n+1` |

Only `fresh-feature` and `planning` always need an LLM call.
`reconciliation` needs minimal narrative, `vault-only` needs commit-msg
only, and `writes-already-landed` skips the render entirely.

## Pre-flight

1. `make pre-commit` clean before wrapping. Wrapping over a dirty
   pre-commit produces a misleading iter record.
2. An API key is configured for the tier's provider. Tiers map
   `provider:model` strings via `[wrap.tiers]` in
   `~/.config/vibe-vault/config.toml`. Set the key via
   `vv config set-key <provider> <key>` (preferred — stored at mode
   0600) or export the provider's `*_API_KEY` env var.
3. **Merge-pattern timing.** When the project uses a feature-branch
   merge pattern, run `/wrap` on the feature branch BEFORE merging.
   Wrapping after merge produces `reconciliation` instead of
   `fresh-feature`, losing the per-decision narrative.

## Procedure

### Stage 1 — State collection

Call `vv_describe_iter_state` for `iter_n`, `branch`,
`vault_has_uncommitted_writes`, `last_iter_anchor_sha`. Then compute
four slash-command fields, anchored by `last_iter_anchor_sha`
(substitute `HEAD` if empty — the project has no prior iter):

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
`writes-already-landed` over `vault-only`) and proceed.

### Stage 3 — Render call

Skip when shape is `writes-already-landed`. Otherwise parallel-fetch
the project-context bundle:

- `vv_get_resume` — current resume.md content
- `vv_get_iterations` — last few iter narratives (voice calibration +
  back-references)
- `vv_get_friction_trends` — friction-trend summary

Parse `open_threads` as a slug list from the resume.md
"Carried-forward threads" section.

Then dispatch by shape:

**`fresh-feature` / `planning`** — full render:

```
vv_render_wrap_text(
    kind         = "iter_narrative_and_commit_msg",
    tier         = "<tier>",                    // default sonnet; opus on re-run
    project_name = "<project>",
    iter_state   = {iter_n, branch, last_iter_anchor_sha,
                    commits_since_last_iter, files_changed,
                    task_deltas, test_counts},
    project_context = {resume_state, recent_iterations,
                       open_threads, friction_trends},
)
→ {narrative_title, narrative_body, commit_subject, commit_prose_body}
```

**`reconciliation`** — minimal narrative:

```
vv_render_wrap_text(
    kind = "iter_narrative", tier = "<tier>", project_name = "<project>",
    iter_state = {...},
    project_context = {resume_state: "", recent_iterations,
                       open_threads: [], friction_trends: {}},
)
→ {narrative_title, narrative_body}
```

**`vault-only`** — commit message only:

```
vv_render_wrap_text(
    kind = "commit_msg", tier = "<tier>", project_name = "<project>",
    iter_state = {...},
    project_context = {resume_state: "", recent_iterations,
                       open_threads: [], friction_trends: {}},
)
→ {commit_subject, commit_prose_body}
```

**`writes-already-landed`** — skip render. Compose the commit message
inline (one-line conventional-commit subject + 1–3 paragraph body)
and pass it directly to `vv_set_commit_msg` in Stage 4.

If output is poor, re-run with a higher tier (e.g. `tier = "opus"`).
There is no auto-escalation — the operator controls tier choice.

### Stage 4 — Mechanical apply

Order matters: iter row, then thread/carried, then commit msg, then
session capture.

1. **`vv_append_iteration`** — append the new row to `iterations.md`.
   Skip on `writes-already-landed`. The auto-heal hook re-renders the
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
4. **`vv_set_commit_msg`** — write the rendered (or composed)
   `commit_subject` + blank line + `commit_prose_body` to
   `commit.msg` at project root. Single `content` field; markdown
   verbatim.
5. **`vv_capture_session`** — record summary, tag, decisions,
   files_changed, open_threads.

### Stage 5 — Git plumbing (project side)

- `git add` the explicit paths in `iter_state.files_changed` plus the
  agentctx files actually modified by Stage 4
  (`agentctx/iterations.md`, `agentctx/resume.md`, any
  `agentctx/tasks/*.md` written during the iter). Never use `git add
  -A` or `git add .`.
- `git commit -F commit.msg` — uses the file written by
  `vv_set_commit_msg`.
- `git push <remote> <branch>` per the project's workflow rules. On
  feature-branch merge patterns, push to the feature branch and let
  the operator handle the merge.

### Stage 6 — Vault sync

`vv vault push` commits and pushes vault writes. On feature-branch
merge patterns, run vault push **after** the project-side push but
**before** the upstream merge — the vault iter row references the
project commit, so the project commit must exist on a pushed branch
by the time the vault record is published.

## Flags

- `/wrap` — uses `[wrap].default_model` as the tier when present;
  otherwise `sonnet`.
- `/wrap --tier <name>` — pin the tier (operator-defined tiers in
  `[wrap.tiers]` accepted).
- `/wrap --dry-run` — run Stages 1–3 and print the rendered prose plus
  proposed mutations without applying.

## Schema reminders

- `vv_render_wrap_text`: `kind` is one of
  `iter_narrative | commit_msg | iter_narrative_and_commit_msg`. Only
  the fields relevant to the requested kind are populated in the
  response.
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
