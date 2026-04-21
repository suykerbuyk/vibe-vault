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

## Aggregate review

After all phases complete, the human reviews the feature branch in aggregate and merges to main. Per-subcommit review is unnecessary when each subagent was isolated.
