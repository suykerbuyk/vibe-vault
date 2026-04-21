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
- Update commit.msg — there are TWO copies and BOTH must be kept in sync:
    1. Vault archive: `<vault_path>/Projects/<project>/agentctx/commit.msg`
       (the canonical source; survives across machines via vault git sync).
    2. Project-root working copy: `<project_root>/commit.msg` (gitignored;
       the path `git commit -F commit.msg` reads when the user runs it).
  Workflow: Read the vault copy first (it likely exists from a prior
  iteration — Read must be called before Write will overwrite), overwrite
  the vault copy with the new message via Write, then copy the vault file
  to the project root via Bash `cp`. Neither copy is repo-tracked — do NOT
  stage either of them. Verify the project-root copy exists before finishing
  (`ls <project_root>/commit.msg`); a missing project-root copy means
  `git commit -F commit.msg` would fall back to a stale version or fail.
- Ensure the commit.msg is complete and standalone in documenting all the code
  changes, features added and bugs or warnings resolved. Don't be terse, be verbose.
- Stage all modified and newly added project files (use git add with explicit
  file paths — never use git add -A or git add .). Only stage files that are
  inside the git repo.

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
