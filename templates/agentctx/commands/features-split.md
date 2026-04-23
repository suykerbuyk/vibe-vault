Split a vault project's `agentctx/resume.md` Current State section into
two files: invariants stay in `resume.md`, shipped-capability prose moves
to `agentctx/features.md`. One-time operator pass per project. Invoked
`/features-split <project>`.

This is the v10 Current-State contract enforced by hand. After the split,
the synthesis agent and the `vv_update_resume` MCP tool refuse to write
narrative back to Current State, so the contract holds without further
intervention.

## When to run

Run on a project once it bumps to schema v10 and its Current State
section has accumulated shipped-capability prose. Skip projects whose
Current State is already invariants-only (test counts, version pins,
module path, etc.) — most projects under ~700 B of Current State don't
need a split.

The three projects flagged for a split today: `RezBldrVault`, `rezbldr`,
`vibe-palace`. `cando-rs` and `proteus-hvpc-support` are judgment
calls — split if the section contains feature descriptions, leave alone
if it's just counts and pointers.

If the operator omits `<project>`, ask which project to split. Do not
guess.

## What counts as an invariant (the v10 classifier)

A line stays in Current State only if it matches all three:

1. Starts with optional list marker, then `**Key:**` (e.g.,
   `- **Tests:** 1429 across 36 packages`).
2. Key's **first word** is in this whitelist:

   ```
   Iterations, Tests, Lint, Schema, Module, MCP, Embedded,
   Stack, Binary, Config, Bootstrap, Build, CLI, License,
   Git, Coverage, Distribution, External
   ```

3. Trailing content after `:**` is ≤200 characters.

Anything else — narrative paragraphs, multi-sentence capability
descriptions, "Phase" / "Status" / "Workflow" / "Recent" / "New
capability" / "Latest artifacts" bullets, continuation lines without a
bolded key — moves to `features.md`. The shared classifier source of
truth is `internal/context/invariants.go` in the vibe-vault repo; this
list mirrors it.

## Procedure

1. **Read the current state.** Open `<vault_path>/Projects/<project>/agentctx/resume.md`.
   Locate the `## Current State` section (or whatever the project's
   equivalent is — some projects use `## Current Status`). Verify the
   project's `.version` reads 10 (`<vault_path>/Projects/<project>/agentctx/.version`);
   if it reads <10, stop and tell the operator to run `vv context sync`
   first so the guards activate.

2. **Classify each bullet block.** A bullet block is the lead bullet
   plus any indented continuation lines. For each block, decide:
   - **Keeps:** matches all three classifier rules above.
   - **Moves:** anything else.

   When in doubt, move it. False negatives (kept narrative) cost the
   contract; false positives (moved invariant) just inflate features.md
   marginally.

3. **Open or create `agentctx/features.md`.** If the file doesn't
   exist, seed it with this header:

   ```markdown
   # <project> — Shipped Capabilities

   Condensed index of features the project currently ships. Each entry
   describes what the feature does, where it lives, and the iteration
   that introduced (or finalized) it. For iteration narratives see
   `iterations.md`; for current project invariants (test count,
   schema version, module path) see `resume.md`.

   This file is maintained by `/wrap` — when a new capability ships,
   the wrap flow records it here rather than accumulating prose in
   `resume.md`.
   ```

4. **Move bullets under themed sections.** Use a project-appropriate
   taxonomy. The vibe-vault baseline taxonomy is a good starting
   point, but adapt to the project's actual surface area:

   - `## Template cascade & context sync`
   - `## Memory & knowledge`
   - `## MCP server`
   - `## Vault operations`
   - `## Workflow & discipline`
   - `## Code hygiene`

   If a bullet doesn't fit any themed section, append it under
   `## Ungrouped` (the synthesis agent's default landing zone for
   future entries — keeping the heading present is harmless and saves
   a future `/wrap` from creating it).

   Preserve each bullet's wording verbatim. The goal is a relocation,
   not a rewrite. If the bullet was unwieldy, leave a TODO comment
   below it for a follow-up tightening pass — don't conflate
   reorganization with editing.

5. **Rewrite Current State with invariants only.** After moving the
   narrative, the section should contain only invariant bullets plus
   a single trailing pointer line:

   ```markdown
   *See `agentctx/features.md` for shipped-capability index.*
   ```

   Leave a blank line above the pointer. Do not add any other
   commentary.

6. **Verify the contract holds.** Re-read the rewritten Current State.
   Every remaining line must be either: an invariant bullet matching
   the classifier, a blank line, the pointer, or a section heading.
   If anything else slipped through, fix it before committing.

   Optional dry-run check: if you have a vibe-vault binary handy
   (`vv version` ≥ the iter that ships v10), the same classifier
   logic is enforced by `vv_update_resume` on Current State writes —
   so a quick test write after the split will surface any missed
   narrative as a guard rejection.

7. **Commit via vault git.** No code-repo commit involved — this is
   a vault-side reorganization only. Stage and commit both files in
   the vault:

   ```
   git -C <vault_path> add Projects/<project>/agentctx/resume.md \
                           Projects/<project>/agentctx/features.md
   git -C <vault_path> commit -m "<project>: split Current State into resume.md invariants + features.md"
   ```

8. **Push to all vault remotes.** Discover remotes dynamically — do
   NOT assume any particular name (vaults typically use `github` and
   `vault` but always discover):

   ```
   git -C <vault_path> remote
   ```

   For each discovered remote, run `git -C <vault_path> push <remote> main`.
   If a push fails, show the error and continue to the next remote —
   do not abort. If all pushes fail, warn and proceed; local state
   is still valid.

## What this skill does NOT do

- It does not edit `iterations.md`. Iteration narratives stay where
  they are.
- It does not touch `doc/` files in the project's code repo.
- It does not bump `.version`. That happens automatically on `vv
  context sync` (Phase 1 of the v10 contract migration).
- It does not run on more than one project per invocation. Run it
  again per project.
- It does not auto-discover which projects need splitting. The
  operator decides per project, guided by the size table in the
  task plan (`features-md-schema-migration`) or by reading each
  project's Current State.

## After the split

Once the project is split, future `/wrap` runs route new
shipped-capability entries directly to `features.md`. The synthesis
agent (when an LLM provider is configured) does the same. The MCP
`vv_update_resume` tool refuses any narrative writes to Current State
on v10+ projects. The contract holds.
