## Restoring full AI thread context:

Read CLAUDE.md, then read RESUME.md to restore full project context.

Run `vv inject` via Bash to load live vault context (recent sessions, open
threads, decisions, friction trends, knowledge). Include the full output
verbatim in your context — do not summarize it.

After reading, briefly confirm what you loaded and note the current state:
recent session activity from inject, and what was last worked on based on
recent git history.

## Workflow Orchestration

#### 1. Plan Mode Default
- Enter plan mode for ANY non-trivial task (3+ steps or architectural decisions)
- If something goes sideways, STOP and re-plan immediately - don't keep pushing
- Use plan mode for verification steps, not just building
- Write detailed specs upfront to reduce ambiguity

#### 2. Subagent Strategy
- Use subagents liberally to keep main context window clean
- Offload research, exploration, and parallel analysis to subagents
- For complex problems, throw more compute at it via subagents
- One tack per subagent for focused execution

#### 3. Verification Before Done
- Never mark a task complete without proving it works
- Diff behavior between main and your changes when relevant
- Ask yourself: "Would a staff engineer approve this?"
- Run tests, check logs, demonstrate correctness

#### 4. Autonomous Bug Fixing
- When given a bug report: just fix it. Don't ask for hand-holding
- Point at logs, errors, failing tests - then resolve them
- Zero context switching required from the user

## Document Conventions

- **resume.md** is a thin gateway — current state, open threads, pointers
- **doc/** holds stable project reference (architecture, design, testing) — source-controlled
- **iterations.md** is the append-only archive — completed work details go here
- Never add file inventories, module tables, or design decisions to resume.md

## Core Principles

- **Simplicity First**: Make every change as simple as possible. Impact minimal code.
- **No Laziness**: Find root causes. No temporary fixes. Senior developer standards.
- **Minimal Impact**: Changes should only touch what's necessary. Avoid introducing bugs.
- **Definition of Done**: AI can never declare a task as "done," only the human can define done.
