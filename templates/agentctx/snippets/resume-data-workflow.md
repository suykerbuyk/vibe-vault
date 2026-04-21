<!-- vv:data-workflow:start -->
## Data workflow

- **Code:** tracked in this project's git repo.
- **Iteration narratives, tasks, resume:** VibeVault
  `Projects/{{PROJECT}}/agentctx/` — sync'd via `vv vault push`.
- **Auto-memory:** VibeVault `Projects/{{PROJECT}}/agentctx/memory/` — the
  host-local `~/.claude/projects/<slug>/memory/` is a symlink into this
  directory, so Claude Code auto-memory writes land in VibeVault and sync
  across hosts. Set up via `vv memory link` (see vibe-vault README).
- **Cross-project learnings:** VibeVault `Knowledge/learnings/` —
  discoverable via `vv_list_learnings` / `vv_get_learning` when planning
  or when reasoning about testing / correctness.

On a new machine: `git clone <VibeVault>`, then in each project run
`vv memory link`. No manual copying required.
<!-- vv:data-workflow:end -->
