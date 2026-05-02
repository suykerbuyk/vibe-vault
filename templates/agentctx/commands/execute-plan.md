Dispatch one subagent per phase (or per task, if a phase is large) in sequence. Verify each deliverable against the plan in form, fit, and intent before moving on. Your job is context hygiene (stay under 200K tokens) — subagents implement, you verify.

## Orchestration

- **Sequential dispatch is the default.** Parallel subagents on overlapping file surfaces conflict. Use parallel only when dependencies AND file-touch surfaces are provably independent.
- **Prefer `isolation: "worktree"`.** Worktree-isolated subagents are empowered to commit their own work to their own branch — the worktree is the safety rail, the primary branch stays untouched until the orchestrator merges.
- **Non-isolated subagents stage but do not commit.** If a subagent works directly in the main working tree, the human approves each commit.
- **Orchestrator verifies, does not implement.** Read the diff, run the tests, inspect integration results.

## Definition of done (per phase)

- ≥80% unit test coverage, as close as realistic.
- Integration test proves end-to-end behavior in the full stack.
- Documentation updated (README, design, architecture, as relevant).

## When to pause

Any form/fit/function issue in review → STOP and tell the human. Explain the situation, offer options. Do not guess. No slop, no hallucination. When in doubt, ask your pair-programming partner.

## After each subagent dispatch returns

Subagent worktrees under `.claude/worktrees/agent-<id>/` accumulate during multi-phase dispatches. After verifying each subagent's deliverable (and either ff-merging or cherry-picking the worktree branch onto the feature branch), invoke the gc to reap the orphan:

1. **Verify capture** — `git cherry <feature-branch> worktree-agent-<id>` should emit only `- <sha>` lines (or be empty). A `+` prefix means the worktree's commit is NOT yet on the feature branch — pause and merge before reaping.
2. **Invoke gc** — call `vv_worktree_gc` (or `vv worktree gc` via Bash) with `candidate_parents` set to the feature branch name. The orchestrator's parent process is still alive mid-session, so the PID-liveness probe will report `alive` and the gc typically defers actual cleanup to the next session start.
3. **Cross-session reap** — orphans queued mid-session reap cleanly at the next `/restart` (parent process gone → PID probe returns `dead` → capture-verified branches are reaped). See DESIGN #98 for the two-tier strategy.

Mid-session attempts that report `alive` are the expected best-effort path; the high-value cleanup is cross-session. Don't wrestle with mid-session reaping — let the queue drain at session boundary.

## Aggregate review

After all phases complete, the human reviews the feature branch in aggregate and merges to main. Per-subcommit review is unnecessary when each subagent was isolated.
