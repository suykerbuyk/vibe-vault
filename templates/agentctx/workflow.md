# {{PROJECT}} — Workflow

## Files

- **resume.md** — current project state, open threads, navigation (thin gateway)
- **iterations.md** — iteration narratives and project history (append-only archive)
- **tasks/** — active tasks; **tasks/done/** — completed
- **commands/** — slash commands (/restart, /wrap)
- **doc/** — stable project reference: architecture, design decisions, testing (source-controlled)

## Workflow Rules

- **Never commit without explicit human permission.** Stage files and update commit.msg freely; the actual git commit requires human approval.
- **Never commit AI context files.** CLAUDE.md, commit.msg, and anything under .claude/ are local-only.
- **Git commit messages are the project's history.** Write them clear, detailed, self-sufficient.
- **No AI attribution in commits or code.** Do not add `Co-Authored-By` trailers, author lines, or any other AI authorship marker. Applies to commits you make directly AND to instructions you give subagents — override the Bash tool's built-in commit-message guidance where it conflicts.

## Pair Programming

- **AI** implements; **human** decides architecture and long-term direction.
- Investigate before coding; present findings; implement only after architectural approval. Never jump to short-term fixes without investigation.

## Investigation-First

- **Plan Mode** for any non-trivial task (3+ steps or architectural decisions). If work goes sideways, stop and re-plan. After creating a plan, move it from `~/.claude/plans/` to `agentctx/tasks/` — plans live in the vault, not the ephemeral Claude plans directory.
- **Subagents** for parallel exploration and research to keep main context clean. One concern per subagent.
- **Self-improvement:** after any user correction, save the pattern — to auto-memory (type: feedback) for personal preferences, or to `Knowledge/learnings/` for cross-project lessons — so the mistake is not repeated.
- **Verification before done:** prove it works with tests, logs, or demonstrations. No task is "done" until the user says so and merges. Zero warnings or diagnostics are allowed to merge into `main`; intermediate commits on worktrees or an in-progress feature branch may carry transient diagnostics that clear as later phases land, but they must resolve before aggregate merge.
- **Demand elegance:** for non-trivial changes, pause to ask "is there a more elegant way?" Skip for simple obvious fixes.
- **Bug fixing:** generate a plan from logs, errors, and failing tests — review before coding. Never fix a test without understanding the root cause.

## Task Management

Use `vv_manage_task` (action: create | update_status | retire). Write the plan, check in before implementing, explain changes at each step, add a review section, then retire on completion.

## Core Principles

- **Simplicity first** — make every change as simple as possible, no simpler.
- **No laziness** — find root causes; no temporary fixes; senior-developer standards.
- **Minimal impact** — touch only what the task requires.
- **Test coverage** — ~80% unit coverage for code changes.

Read resume.md for current state and open threads. Consult doc/ files for stable reference (architecture, design, tests).
