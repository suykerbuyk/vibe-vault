Update resume.md and its dependent documents to reflect the current state
of the project. These files serve as the single source of truth for
restoring AI thread context and resuming work on this codebase.

resume.md is a THIN GATEWAY — not a diary. Keep it focused on current state,
open threads, and pointers. Stable reference material belongs in doc/ under
source control. Completed work details belong in iterations.md only.

Specifically:

- Ensure all code compiles without warnings, errors, or diagnostics
- Ensure all unit and integration tests pass
- Read the current resume.md using `vv_get_resume` and CLAUDE.md
- Compare against the actual codebase state (files, tests, architecture)
- Update resume.md using `vv_update_resume`: current state (test count,
  iteration count), open threads. Do NOT add file inventories, architecture
  diagrams, design decisions, or module tables to resume.md — those belong
  in doc/ files
- If stable project documentation changed (architecture, design decisions,
  test structure), update the relevant doc/ file
- Append a new iteration narrative to iterations.md using `vv_append_iteration`
  describing what changed in this session and why (past tense, technical detail)
- Add a corresponding summary row to the Project History table in resume.md
- Move any completed plans from resume.md to the Completed Plans section
  in iterations.md, replacing them with a single-line pointer
- Retire completed tasks: use `vv_list_tasks` to check each active task against
  the session's work and resume.md history — if a task has been implemented and
  committed, use `vv_manage_task` with `action: retire` to move it to done/,
  and update the resume.md file inventory accordingly
- Write commit.msg using the Write tool (it is a regular file at the repo root).
  commit.msg is NOT a repo-tracked file — do NOT stage it.
- Stage all modified and newly added project files (use git add with explicit
  file paths — never use git add -A or git add .). Only stage files that are
  inside the git repo.

Do not add "Co-Authored-By" lines to commit messages or source files.

Do not ask for confirmation — just do the updates, stage the files, show what
changed, and note that the user should review before committing.
