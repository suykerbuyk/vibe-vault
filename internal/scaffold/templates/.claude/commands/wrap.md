Update RESUME.md to fully reflect the current state of the project. This file
serves as the single source of truth for restoring AI thread context and resuming
work on this codebase.

Specifically:
- Read the current RESUME.md and CLAUDE.md
- Compare against the actual codebase state (files, architecture)
- Update all sections that are stale: file inventory, architecture diagrams,
  design decisions, status, and what was last worked on
- Rewrite commit.msg to document all code changes made in this session.
  Write it to both the repo root and the vault agentctx/ directory so
  they stay in sync.
- Stage all modified project files (use git add with explicit file paths —
  never use git add -A or git add .)

Do not add "Co-Authored-By" lines to commit messages or source files.

Do not ask for confirmation — just do the updates, stage the files, show what
changed, and note that the user should review before committing.
