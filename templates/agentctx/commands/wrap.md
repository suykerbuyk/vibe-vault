Update resume.md and its dependent documents to reflect the current state
of the project. These files serve as the single source of truth for
restoring AI thread context and resuming work on this codebase.

resume.md is a THIN GATEWAY — not a diary. Keep it focused on current state,
open threads, and pointers. Stable reference material belongs in doc/ under
source control. Completed work details belong in iterations.md only.

## Canonical wrap flow (Phase 5+)

The canonical wrap flow is **synthesize → AI-inspects/edits → apply-bundle**.
One synthesize call, one apply call, all writes dispatched atomically:

1. AI composes the iteration narrative and structural inputs.
2. AI calls `vv_synthesize_wrap(iteration_narrative, title, subject, ...)` →
   receives a JSON bundle with all derived sub-artifacts, each with a
   synthesize-time SHA-256 fingerprint.
3. AI inspects and optionally edits the bundle in conversation context
   ("edit, don't regenerate").
4. AI calls `vv_apply_wrap_bundle(bundle)` — one call dispatches all writes:
   appends the iteration block, inserts/removes resume threads, adds/removes
   carried-forward bullets, writes commit.msg, and records a capture_session.

`vv_apply_wrap_bundle` returns `{applied_writes, drift_summary, metric_file}`.
Drift (apply_sha256 != synth_sha256 for any field) is logged to
`~/.cache/vibe-vault/wrap-metrics.jsonl` but does NOT block apply.

**capture_session is always present** in the bundle — it is never optional.

## Surgical APIs (available for hand-edits between wraps)

The per-tool APIs below remain available for one-off mutations between wraps
or when the AI needs to surgically update a single field. They are NOT called
from the canonical wrap flow above (vv_apply_wrap_bundle handles all of this
internally).

### Current State and other full-section rewrites

- Update resume.md using `vv_update_resume` for **Current State** (and
  any other non-Open Threads sections that need a full rewrite): Current
  State should hold only evergreen invariants (test count, iteration count,
  schema version, module path, MCP tool count, embedded template count).
  Do NOT add shipped-feature descriptions, narrative paragraphs, file
  inventories, architecture diagrams, design decisions, or module tables
  to resume.md. Shipped-capability descriptions belong in
  `agentctx/features.md`; stable reference material belongs in `doc/` files

### Open Threads surgical edits

- **Open a new thread** — `vv_thread_insert(position, slug, body)` where
  `position` is `{"mode": "top"}` / `{"mode": "bottom"}` /
  `{"mode": "after", "anchor_slug": "..."}` / `{"mode": "before", "anchor_slug": "..."}`.
  The `slug` is the identifier (text up to the first ` — ` or full heading
  if no em-dash separator). The `body` is everything after the heading line.
- **Update an existing thread** — `vv_thread_replace(slug, body)`. The tool
  preserves the original heading verbatim; supply only the new body content.
- **Close a resolved thread** — `vv_thread_remove(slug)`.
- All three tools reject `slug = "Carried forward"` — that section is
  managed by `vv_carried_*` tools (see below).
- Zero slug matches → hard error listing available slugs. One match →
  silent proceed. Multiple matches → first occurrence updated with a
  `candidates_warning` in the result.
- Only fall back to `vv_update_resume(section="Open Threads", ...)` if
  multiple threads need restructuring in a single shot (e.g., adding 3+
  new threads at once) or if the section needs a clean-slate rewrite.

### Carried forward surgical edits

- **Add a new item** — `vv_carried_add(slug, title, body?)`.
  `slug` is the unique identifier (e.g. `dry-run-coverage-gap`);
  `title` is the short description; `body` is optional detail prose.
  Errors if the slug already exists (case-insensitive).
- **Remove a resolved item** — `vv_carried_remove(slug)`.
  Case-insensitive slug match. Hard error if not found, listing all
  available slugs.
- **Promote to a task** — `vv_carried_promote_to_task(slug, new_task_slug)`.
  Creates `agentctx/tasks/{new_task_slug}.md` with the bullet body
  verbatim under a minimal frontmatter header, then removes the bullet.
  Use when a carried item has grown large enough to warrant its own
  task file. Hard error if slug not found or target task already exists.
- Canonical bullet form is `- **slug** — title body`. All tool-emitted
  bullets use this form; the parser accepts liberal variants on read
  (`- **slug:**`, `- **slug (note)**`, plain `- text`). Emit canonical
  form when writing new items manually.

### Iteration and commit.msg surgical tools

- `vv_append_iteration(title, narrative, iteration?, date?)` — append a
  single iteration block to iterations.md directly (bypasses synthesize).
- `vv_render_commit_msg(project_path, iteration, subject, prose_body,
  unit_tests, integration_subtests, lint_findings)` — assemble the full
  commit message string for inspection. Returns `{rendered, bytes}`.
- `vv_set_commit_msg(project_path, content)` — write commit.msg atomically
  to both vault and project root. On partial failure returns an actionable
  diagnostic with a manual `cp` command.

## Remaining wrap steps (all flows)

After `vv_apply_wrap_bundle` (or the surgical equivalents) complete:

- Read the current resume.md using `vv_get_resume` and compare against the
  actual codebase state (files, tests, architecture).
- When a new capability ships, add (or update) an entry in
  `agentctx/features.md` under the appropriate section heading. Keep it
  brief: 1–2 sentences on what it does, where it lives
  (package/file/flag), and the iteration that introduced it.
- If stable project documentation changed (architecture, design decisions,
  test structure), update the relevant doc/ file.
- Add a corresponding summary row to the Project History table in resume.md,
  keeping only the 5 most recent rows (drop the oldest row when adding a new
  one if the table already has 5). Older iterations remain retrievable via
  `vv_get_iterations` or directly from `iterations.md`.
- Move any completed plans from resume.md to the Completed Plans section
  in iterations.md, replacing them with a single-line pointer.
- Retire completed tasks: use `vv_list_tasks` to check each active task
  against the session's work and resume.md history — if a task has been
  implemented and committed, use `vv_manage_task` with `action: retire`
  to move it to done/, and update the resume.md file inventory accordingly.
- Ensure the commit.msg (written by vv_apply_wrap_bundle or vv_set_commit_msg)
  is complete and standalone in documenting all the code changes, features
  added, and bugs or warnings resolved. Don't be terse, be verbose.
- Stage all modified and newly added project files (use git add with explicit
  file paths — never use git add -A or git add .). Only stage files that are
  inside the git repo. Do NOT stage commit.msg — neither copy is repo-tracked.

Do not add "Co-Authored-By" lines to commit messages or source files.

After staging project files, sync vault changes to all remotes:

1. **Discover remotes**: Run `git remote` in the vault directory (read
   `vault_path` from `~/.config/vibe-vault/config.toml`) to dynamically
   discover all configured remotes. Do NOT assume any particular remote name
   (e.g., "origin") — vaults typically use names like `github`, `vault`, etc.
2. **Commit vault changes**: Run `git -C <vault_path> add -A` then
   `git -C <vault_path> commit -m "<short summary>"`. If nothing to commit,
   skip.
3. **Push to each remote**: For each discovered remote, run
   `git -C <vault_path> push <remote> main`. If a push fails, show the error
   and continue to the next remote — do not abort the entire wrap.
4. If all pushes fail (no remote, network error), warn and proceed — local
   state is still valid.

Do not ask for confirmation — just do the updates, stage the files, show what
changed, and note that the user should review before committing.
