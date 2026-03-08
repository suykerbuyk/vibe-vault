Review recent session notes from the vault and extract durable knowledge worth preserving.

## What to Do

1. **Read session notes** from `Projects/` for the specified period (default: last 7 days). Use Glob to find `Projects/*/sessions/*.md` and read each note.

2. **Identify extractable knowledge** across three categories:
   - **Decisions** — Architectural or design choices that should guide future work (e.g., "Use REST not GraphQL for this API", "Deploy to Fly.io not Vercel")
   - **Patterns** — Reusable approaches or techniques worth remembering (e.g., "Bun spawn needs explicit stdio config", "Use fire-and-forget git commits in hooks")
   - **Learnings** — Lessons from what worked or failed (e.g., "Frontmatter parsing is faster than full YAML parse for simple fields")

3. **Check for duplicates** — Before adding knowledge, check the project's `knowledge.md` to avoid duplicating insights that already exist.

4. **Update the project's knowledge.md** file at `Projects/{project}/knowledge.md` with new insights. Each project has a simple knowledge file with sections for Decisions, Patterns, and Learnings.

5. **Git commit** the changes: `git add Projects/ && git commit -m "distill: extract knowledge from recent sessions"`

## Quality Filters

- Only extract insights that are **durable** — they should be useful weeks or months from now
- Skip session-specific details (timestamps, ISC counts, specific file paths)
- Skip trivial sessions with no meaningful output
- Prefer **concrete and actionable** insights over vague observations
- Each entry should be self-contained — readable without the source session

## Arguments

If the user provides a time range (e.g., `/distill last 30 days`), use that instead of the default 7 days. If the user provides a project name (e.g., `/distill cortana-obsidian`), filter sessions to that project/domain only.
