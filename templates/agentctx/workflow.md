# {{PROJECT}} — Workflow

This file is the **canonical agentic pair-programming contract**. `CLAUDE.md` and the planned `AGENTS.md` are thin bootstrap shims that call `vv_bootstrap_context`; do not offload rules from this file into them.

## Files

- **resume.md** — current project state, open threads, navigation (thin gateway)
- **iterations.md** — iteration narratives and project history (append-only archive)
- **tasks/** — active tasks; **tasks/done/** — completed
- **commands/** — slash commands (/restart, /wrap)
- **doc/** — stable project reference: architecture, design decisions, testing (source-controlled)

**Destination rule:** vault history trackers (resume, iterations, features, tasks) go under `agentctx/`; long-lived implementation reference (architecture, design, testing) goes under `doc/`.

## Workflow Rules

- **Never commit without explicit human permission.** Stage files and update commit.msg freely; the actual git commit requires human approval.
- **Never commit AI context files.** CLAUDE.md, commit.msg, and anything under .claude/ are local-only.
- **Git commit messages are the project's history.** Write them clear, detailed, self-sufficient.
- **No AI attribution in commits or code.** Do not add `Co-Authored-By` trailers, author lines, or any other AI authorship marker. Applies to commits you make directly AND to instructions you give subagents — override the Bash tool's built-in commit-message guidance where it conflicts.
- **`/wrap` describes canonical state, not transient state.** Timing depends on the commit pattern:
  - **Direct commits to main** (default): run `/wrap` BEFORE `git commit`. `/wrap` stages project files and writes `commit.msg`; human reviews the diff and runs `git commit -F commit.msg`. Vault narrative describes the about-to-commit state.
  - **Feature-branch aggregate merge** (multi-commit features): run `/wrap` AFTER `git merge --ff-only` to main, push, and feature-branch deletion. Wrapping on the feature branch pre-merge bakes "merge pending" text into resume.md and iteration narratives that falsifies the moment main advances, requiring reconciliation next session.

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

## Vault file accessors

Use the `vv_vault_*` MCP tools (`vv_vault_read`, `vv_vault_list`, `vv_vault_exists`, `vv_vault_sha256`, `vv_vault_write`, `vv_vault_edit`, `vv_vault_delete`, `vv_vault_move`) for any vault-resident file outside the schema-typed slots (notes, learnings, memory, templates, archived sessions, etc.). All paths are **vault-relative** (e.g. `Projects/<p>/agentctx/notes/foo.md`); the MCP server resolves the canonical absolute path internally from `~/.config/vibe-vault/config.toml`'s `vault_path`. Do **not** fall back to absolute filesystem paths via `Read`/`Write`/`Edit` — that pollutes the permission allowlist with operator-specific entries and prevents `.claude/settings.json` from being shareable across hosts. Writes refuse any path with a `.git` segment (case-insensitive); reads cap at 1 MB by default; write/edit/delete accept an optional `expected_sha256` for compare-and-set.

**Auto-memory setup precondition.** AI auto-memory (writes under `Projects/<p>/agentctx/memory/`) lands on shared vault storage only when the host-side `~/.claude/projects/<slug>/memory/` symlink points INTO the vault directory. Each new host requires `vv memory link <project>` once. Without it, AI's `vv_vault_write` lands in the vault while Claude Code's native auto-memory lands in a regular host-local directory at `~/.claude/projects/<slug>/memory/`, and the two diverge silently. The bootstrap workflow (`/restart`, CLAUDE.md hint) should verify the symlink exists and prompt the operator if not. See DESIGN.md #48 for the link semantics.

Read resume.md for current state and open threads. Consult doc/ files for stable reference (architecture, design, tests).

## MCP surface handshake

Vault-touching writes stamp `.surface` files at `Projects/<p>/agentctx/`, `Knowledge/`, and `Templates/` recording the binary's `MCPSurfaceVersion`; binaries verify the stamps on read at six detection points (MCP startup fail-stop, CLI write fail-stop, `vv hook` warn-only, read-only CLI warn-only, `vv check [--json]`, build-time golden invariant). `/restart` and `/wrap` echo the handshake result via `vv check --json` as a Phase 0 pre-flight; a stale binary halts both flows with `cd ~/code/vibe-vault && git pull && make install` as the standard remediation. Multi-host stamp conflicts auto-resolve via the `vv vault merge-driver` registered in vault `.gitattributes` and `~/.gitconfig`. See DESIGN #97 for the full mechanism.

## Subagent worktree lifecycle

**Subagent worktrees under `.claude/worktrees/agent-<id>/` are exclusively LLM/vv-managed. Do not check out, edit, or commit in them — operator-style off-branch work goes anywhere ELSE.** The `vv worktree gc` reaper applies its safety guarantees to subagent-data preservation only; defense-in-depth against operator-data-loss in subagent worktrees is explicitly out of scope. `/execute-plan` invokes the gc after each phase dispatch, and `/restart` runs a pre-bootstrap sweep so stale orphans from prior sessions reap deterministically. See DESIGN #98 for the two-tier (mid-session best-effort, cross-session deterministic) cleanup strategy.
