## Surface Handshake (DESIGN #97)

Run `vv check --json` first and parse the `surface` check. If
`status` is `"fail"`, the vault was last written by a newer `vv`
binary than this host has installed; halt the bootstrap and report
the actionable error to the operator (the `Detail` field names the
out-of-date stamp directory and the version mismatch). The standard
remediation is `cd ~/code/vibe-vault && git pull && make install`.

If `status` is `"warn"` or `"pass"`, proceed to vault sync.

## Vault Sync (multi-machine)

Before loading context, sync the vault to get the latest state from other
machines:

1. **Discover remotes**: Run `git remote` in the vault directory to dynamically
   discover all configured remotes and their names. Do NOT assume any particular
   remote name (e.g., "origin") — this project typically uses `github` as the
   primary upstream and `vault` as a local backup, but always discover rather
   than hardcode.
2. Run `vv vault pull` via Bash. If it fails because it expects a remote name
   that doesn't exist, pull from discovered remotes directly (e.g.,
   `git -C <vault_path> pull <remote> main` for each remote).
3. If it reports "regenerated", also run `vv index` to rebuild auto-generated files
4. If it reports files needing manual review, inform the user before proceeding
5. If it fails (no remote, network error), warn and proceed — local state is still valid

## Restoring full AI thread context:

Call `vv_bootstrap_context` to load resume, workflow, and active tasks in a
single call. If MCP tools are not available, run `vv inject` via Bash instead.

After bootstrap, continue loading context in this order:

1. **Sweep orphaned plans**: Check `~/.claude/plans/` for plan files from prior
   sessions (Claude Code creates these when plan mode is used). Use the Glob
   tool with pattern `*.md` and path set to the absolute expansion of
   `~/.claude/plans/` — do NOT put the full path in the pattern when also
   setting path. For each file found:
   - Read the plan to determine if it belongs to the current project (look for
     references to project files, directories, or the project name).
   - If it belongs to this project, move it to the project's agentctx/tasks/
     directory. Resolve the tasks path: read `vault_path` from
     `~/.config/vibe-vault/config.toml`, get the project name from the first
     line of `vv inject` output (`# Context: {name}`), then construct
     `{vault_path}/Projects/{project}/agentctx/tasks/`. Use `mv` via Bash.
   - If it belongs to a different project, leave it in `~/.claude/plans/`.
   - If it belongs to this project, **create it as a task with a descriptive
     slug** derived from the plan title:

     **Slugification rules:**
     a. Find the first markdown heading (`# ...` or `## ...`).
     b. Strip common prefixes: "Plan:", "Task:", "Feature:", "Bug:", "Fix:",
     "Implementation Plan:".
     c. Lowercase the remaining text.
     d. Replace spaces and underscores with hyphens.
     e. Remove all characters except `a-z`, `0-9`, and `-`.
     f. Collapse consecutive hyphens into one; trim leading/trailing hyphens.
     g. Truncate to 60 characters (break at a hyphen boundary if possible).
     h. Fallback: if no heading or empty after processing, use the original
     filename without `.md`.

     **Example**: `# Plan: Deprecate Agentctx Symlinks` becomes
     `deprecate-agentctx-symlinks`.

     Use `vv_manage_task` with `action: create`, `task` set to the derived
     slug, and `content` set to the full plan file content. Then delete the
     original file from `~/.claude/plans/` using `rm` via Bash.

   - Summarize each plan's disposition (created as task, other project, etc.).
     Plans MUST live in agentctx/tasks/, never in `~/.claude/plans/`.

2. **Auto-retire completed tasks**: List active tasks via `ls` on the tasks
   directory (exclude `done/` and `cancelled/` subdirectories). For each task:
   - Read its title and status line.
   - Check `git log --oneline -20` for commits that clearly implement the
     task's objective (matching keywords from the title).
   - **Auto-retire if**: status already says "Done" or "Complete", OR all
     checklist items (`- [x]`) are checked with none unchecked (`- [ ]`) AND
     recent commits match the task's subject matter.
   - **Never auto-retire if**: unchecked checklist items remain, status says
     "In Progress" or "Blocked", or no matching commits are found.
   - For each retirement: use `vv_manage_task` with `action: retire`, then
     use `vv_append_iteration` with a brief narrative noting which commits
     fulfilled the task.
   - On uncertainty, report "Task {slug} may be complete — review recommended"
     and leave it active. False negatives are far better than false positives.

3. `iterations.md` — iteration narratives (on demand, not required for routine work).
   Use `vv_get_project_context` or read directly if needed.
4. Call `vv_get_project_context` for structured context (sessions, threads,
   decisions, friction trends, knowledge).
5. `doc/*.md` — stable reference (architecture, design, testing) — read on demand when needed
6. When a task is completed, use `vv_manage_task` with `action: retire` to move
   it to `tasks/done/` and use `vv_append_iteration` to record a summary.

After reading, briefly confirm what you loaded and note the current state:
test count, open tasks, recent session activity, and what was last worked on
based on recent git history. If active task files exist, summarize each with
its priority and status, and recommend which to start based on priority order
and dependencies.

## Workflow Orchestration

### The Pair Programming Paradigm

#### The AI's Role: Expert Implementation

**Strengths**:

- Expert coder with deep technical knowledge
- Better than any human at writing correct, idiomatic code
- Can rapidly analyze code patterns and identify issues
- Excellent at systematic investigation and testing

**Responsibilities**:

- Investigate problems thoroughly BEFORE implementing fixes
- Present findings and action plans for review
- Implement solutions only after architectural approval
- Write comprehensive, maintainable code

#### The Human's Role: Architectural Vision

- Context that spans the entire project across many days and iterations
- Understanding of long-term maintainability goals
- Knowledge of project principles and design patterns
- Ability to see how changes affect the whole system

**Responsibilities**:

- Guide architectural decisions
- Guide architectural decisions, approve implementation approaches
- Maintain project-wide consistency and long-term goals
- Ensure changes align with long-term goals

#### Critical Anti-Pattern: Premature Implementation

**NEVER: Jump to coding short-term fixes without investigation**

### The Investigation-First Workflow

#### 1. Plan Mode Default

- Enter plan mode for ANY non-trivial task (3+ steps or architectural decisions)
- If something goes sideways, STOP and re-plan immediately - don't keep pushing
- Use plan mode for verification steps, not just building
- Write detailed specs upfront to reduce ambiguity
- After creating a plan in plan mode, immediately create it as a task using
  `vv_manage_task create` with a descriptive slug (see slugification rules in
  step 1 above). Then delete the original from `~/.claude/plans/`.

#### 2. Subagent Strategy

- Use subagents liberally to keep main context window clean
- Offload research, exploration, and parallel analysis to subagents
- For complex problems, throw more compute at it via subagents
- One tack per subagent for focused execution

#### 3. Self-Improvement Loop

- After ANY correction from the user: save the pattern — to auto-memory (type: feedback) for personal preferences, or to `Knowledge/learnings/` for cross-project lessons
- Write rules for yourself that prevent the same mistake
- Ruthlessly iterate until the mistake rate drops
- Auto-memory (`MEMORY.md`) loads automatically at session start; cross-project learnings are discoverable via `vv_list_learnings` / `vv_get_learning`

#### 4. Verification Before Done

- Never mark a task complete without proving it works
- No task is "done" until the user says it is and does the actual commit
- Diff behavior between main and your changes when relevant
- Ask yourself: "Would a staff engineer approve this?"
- Run tests, check logs, demonstrate correctness
- We do not allow any project warnings or diagnostic messages to be part of a commit. Fix them.

#### 5. Demand Elegance (Balanced)

- For non-trivial changes: pause and ask "is there a more elegant way?"
- If a fix feels hacky: "Knowing everything I know now, implement the elegant solution"
- Skip this for simple, obvious fixes - don't over-engineer
- Challenge your own work before presenting it

#### 6. Autonomous Bug Fixing

- When given a bug report: generate a plan and review with the user
- Point at logs, errors, failing tests - then plan to resolve them
- Zero context switching required from the user
- Never fix a test without understanding the root cause.
- Never add file inventories, module tables, or design decisions to resume.md

## Task Management

1. **Plan First**: Write plan using `vv_manage_task` with `action: create`
2. **Verify Plan**: Check in before starting implementation
3. **Track Progress**: Use `vv_manage_task` with `action: update_status` as you go
4. **Explain Changes**: High-level summary at each step in the task file
5. **Document Results**: Add review section to the task file
6. **Capture Learnings**: Save reusable patterns to auto-memory (personal) or `Knowledge/learnings/` (cross-project) to improve future sessions

## Core Principles

- **Simplicity First**: Make every change as simple as possible but no simpler. Impact minimal code.
- **No Laziness**: Find root causes. No temporary fixes. Senior developer standards.
- **Minimal Impact**: Changes should only touch what's necessary. Avoid introducing bugs.
- **Test Coverage**: Ensure close to 80% unit test coverage for all code changes.
