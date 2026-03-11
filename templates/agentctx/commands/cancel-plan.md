Cancel a planned task that was investigated and found not worth implementing.

This preserves the analysis for future reference — preventing the same task
from being re-proposed later — while cleanly removing it from active work.

## Inputs

An optional argument names the task file to cancel (without path or extension,
e.g. `/cancel-plan cloud-transcription`). If no argument is given, follow the
disambiguation steps below.

## Step 1: Identify the task to cancel

List all `.md` files in `agentctx/tasks/` (excluding `done/` and `cancelled/`
subdirectories). Use `ls` via Bash since `agentctx` is a symlink.

- **If 0 tasks exist**: tell the user there are no active tasks to cancel.
  Stop here.
- **If 1 task exists**: proceed with that task. Show its name and status,
  and confirm with the user before continuing.
- **If 2+ tasks exist**: present a numbered list showing each task's name,
  priority, and status (read from the file header). Ask the user which one
  to cancel. Wait for their response before continuing.

If an argument was provided, match it against the task filenames. If no match,
show what's available and ask the user to clarify.

## Step 2: Draft cancellation rationale

Based on the conversation context (the investigation, analysis, or discussion
that led to cancelling), draft a concise cancellation rationale (2-4 sentences).
Focus on **why** the task isn't worth doing — not what it proposed.

Present the draft to the user and ask them to either:
- Accept it as-is
- Edit or rewrite it

Wait for their response before continuing.

## Step 3: Update the task file

Read the task file. Make these changes:

1. Update the **Status** line to `Cancelled (rev N+1)` (increment existing rev)
2. Add a **Cancelled** date line (today's date, format: YYYY-MM-DD)
3. Add a `## Cancellation Rationale` section immediately after the metadata
   block (Status/Created/Priority lines), containing the approved rationale

## Step 4: Move to cancelled/

Create `agentctx/tasks/cancelled/` if it doesn't exist. Move the task file
there using Bash (`mv`).

## Step 5: Update iterations.md

Append a brief iteration entry to `agentctx/iterations.md`:

```
## Iteration N — YYYY-MM-DD — Cancelled: {task name}

Investigated {task name}; cancelled. {One-sentence rationale summary}.
See `tasks/cancelled/{filename}` for the full analysis.
```

Read `iterations.md` to determine the next iteration number.

## Step 6: Update knowledge.md

Read the project's `knowledge.md`. Add a line under an appropriate heading
(create a `## Cancelled Plans` section if one doesn't exist):

```
- **{Task name}** cancelled (YYYY-MM-DD) — {one-sentence reason}.
  See `tasks/cancelled/{filename}`.
```

This prevents future sessions from re-proposing the same work.

## Step 7: Update resume.md

- Remove or update any reference to the cancelled task in the Open Threads
  section
- Update iteration count if changed
- Keep resume.md thin — just remove the pointer, don't add cancellation details

## Step 8: Confirm

Report what was done:
- Task file updated and moved
- iterations.md entry appended
- knowledge.md updated
- resume.md cleaned up

Note that vault files were updated directly (not staged in git, since
agentctx is a symlink to the vault).
