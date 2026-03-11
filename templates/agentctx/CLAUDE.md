# CLAUDE.md

Read agentctx/resume.md at session start for full context and behavioral rules.

Do not commit this file, commit.msg, or anything under .claude/.

## Session Capture

If you have access to the `vv_capture_session` tool, call it at the end of
each **work unit** to record what was accomplished. A work unit is a coherent
piece of focused work — not every conversational turn:

- Implementing a feature or fixing a bug
- Completing a refactor or code review
- Finishing an investigation or design session
- Any time the user says "wrap up," "save," or "capture"

Call `vv_capture_session` with:
- **summary** (required): 2-3 sentences on what was accomplished
- **tag**: implementation | debugging | refactor | exploration | review | docs | planning
- **decisions**: key technical decisions made (array)
- **files_changed**: files created or modified (array)
- **open_threads**: unresolved items or follow-up work (array)

Write summaries that help a developer resuming this work tomorrow.
