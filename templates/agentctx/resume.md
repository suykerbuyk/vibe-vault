---
type: project-resume
project: {{PROJECT}}
---

# {{PROJECT}} — Working Context

<!-- KEEP THIS FILE THIN. resume.md is a gateway to project context, not a diary.
     - Stable architecture, design decisions, test inventories -> doc/ (source-controlled)
     - Completed iteration narratives -> iterations.md (append-only archive)
     - Active work items -> tasks/ directory
     Only current state, open threads, and pointers to deeper context belong here. -->

## What This Project Is

<!-- Brief description of the project, stack, build/test commands -->

## Current State

<!-- Iteration count, test count, what phase the project is in.
     The block between vv:current-state markers below is machine-rendered
     by /wrap (DESIGN #90). Iterations / MCP / Embedded counts are
     auto-rendered; test count and any other prose remain operator-authored
     adjacent to the marker block. -->

<!-- vv:current-state:start -->
- **Iterations:** 0 complete
- **MCP:** 0 tools + 1 prompt
- **Embedded:** 0 templates
<!-- vv:current-state:end -->

## Open Threads

<!-- Active tasks, unresolved questions, next steps.
     The Active tasks block between vv:active-tasks markers is
     machine-rendered from tasks/*.md on every /wrap (DESIGN #90).
     Free-form narrative threads go outside the marker pair. -->

<!-- vv:active-tasks:start -->
### Active tasks (0)

_No active tasks._
<!-- vv:active-tasks:end -->

## Project History (recent)

<!-- The last N=10 iteration rows between vv:project-history-tail
     markers are machine-rendered from iterations.md on every /wrap
     (DESIGN #90). Older rows fall outside the marker region and remain
     prose-authored archive. -->

<!-- vv:project-history-tail:start -->
| #   | Date       | Summary |
| --- | ---------- | ------- |
<!-- vv:project-history-tail:end -->

## Reference Documents

| Document | Access | Purpose |
|----------|--------|---------|
| resume.md | `vv_get_resume` | This file — current state and navigation |
| workflow.md | `vv_get_workflow` | AI workflow rules and pair programming paradigm |
| iterations.md | `vv_get_iterations` | Append-only archive of iteration narratives |
| tasks/ | `vv_list_tasks` / `vv_get_task` | Active task files; tasks/done/ for completed |

<!-- Add doc/ entries as project documentation grows:
| ARCHITECTURE.md | doc/ | Data flow, module responsibilities |
| DESIGN.md | doc/ | Key design decisions with rationale |
| TESTING.md | doc/ | Test inventory and coverage |
-->
