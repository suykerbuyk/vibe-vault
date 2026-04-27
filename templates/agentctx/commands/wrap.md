# /wrap

Wrap the current iteration into the vault using the wrap-executor subagent.

This procedure dispatches a cheaper-tier model (sonnet by default) at the
executor seat so the orchestrator session does not pay full Opus cost for
every wrap. The orchestrator owns the natural-language ladder: it walks
`[wrap].escalation_ladder` and stops at the first tier whose outputs pass
the quality gate.

## Pre-flight

1. Run `vv mcp check --tools` and assert these tools are present (fail
   loudly if any are missing):
   - `vv_prepare_wrap_skeleton`
   - `vv_synthesize_wrap_bundle`
   - `vv_apply_wrap_bundle_by_handle`
   - `vv_wrap_quality_check`
   - `vv_wrap_dispatch`

   Note: `vv_get_agent_definition` is v2-portability scaffolding and is
   NOT required for v1 dispatch.
2. Verify pre-commit clean: `make pre-commit`.

## Procedure

### Stage 0 — Skeleton preparation

Compute orchestrator facts (NOT prose). The skeleton carries structural
inputs only; the executor produces the prose:

- `iter` (next iteration number, int)
- `project` (project name)
- `files_changed` (from `git diff --name-only`)
- `test_count_delta` (compare test counts vs prior wrap, single int sum)
- `decisions` (key technical decisions)
- `threads_to_open` (slugs + anchor positions; bodies filled by executor)
- `threads_to_replace` (slugs; bodies filled by executor)
- `threads_to_close` (slugs)
- `carried_to_add` (slugs + titles; bodies filled by executor)
- `carried_to_remove` (slugs)
- `task_retirements`

Call `vv_prepare_wrap_skeleton(...)` with these facts. Capture the returned
`SkeletonHandle{iter, skeleton_path, skeleton_sha256}` — every Stage 1/2
tool requires this handle for compare-and-set safety.

### Stage 1 — Dispatch loop

Read `[wrap].default_model` from `~/.config/vibe-vault/config.toml`. Walk
`[wrap].escalation_ladder` starting at `default_model`:

```
prior_attempts = []
for tier in escalation_ladder:
    result = vv_wrap_dispatch(
        skeleton_handle = handle,
        tier            = tier,
        agent_name      = "wrap-executor",
        prior_attempts  = prior_attempts,
    )
    if result.escalate_reason:
        prior_attempts.append({tier, escalate_reason: result.escalate_reason})
        continue
    qc = vv_wrap_quality_check(skeleton_handle = handle, outputs = result.outputs)
    if qc.passed:
        outputs = result.outputs
        break
    prior_attempts.append({tier, escalate_reason: "qc_failed: " + qc.failures})

if outputs is None:
    report "all tiers exhausted across the ladder"; exit
```

The quality gate runs BEFORE the apply call so a failed tier's outputs
never reach the vault.

### Stage 2 — Apply

Call `vv_apply_wrap_bundle_by_handle(skeleton_handle, outputs)`. Then run
the git operations:

- `git add` the project files listed in `skeleton.files_changed` (use
  explicit paths; never `git add -A` or `git add .`).
- `git -C <vault> add -A && git -C <vault> commit -m "<short summary>" &&
  git -C <vault> push <remote> main` for each remote returned by
  `git -C <vault> remote`.

## Flags

- `/wrap` — uses `[wrap].default_model` as the starting tier.
- `/wrap --model sonnet|opus|haiku|<custom>` — pin the starting tier.
  Operator-defined tiers in `[wrap.tiers]` are accepted; v1 supports only
  `anthropic:` providers.
- `/wrap --inline` — alias for `--model opus`, run inline (no dispatch).
  Use this when you specifically need the orchestrator session to handle
  the wrap (e.g. for debugging the dispatch path itself).
- `/wrap --no-cache` — re-prepare the skeleton ONCE at the top of the
  procedure, then cache the locks for the rest of the wrap.

## Schema reminders (avoid harness friction)

Tool input schemas use Go type semantics, not free-form prose:

- `vv_prepare_wrap_skeleton`: `iter` is int, `test_count_delta` is int
  (a single sum, NOT an object with per-package counts).
- `vv_apply_wrap_bundle_by_handle`: `outputs` is an object. The harness
  XML-param protocol may serialize an object as a string; if you see
  `cannot unmarshal string into Go value of type`, fall back to the
  surgical apply path: `vv_append_iteration` + `vv_thread_insert` +
  `vv_thread_replace` + `vv_thread_remove` + `vv_carried_add` +
  `vv_carried_remove` + `vv_set_commit_msg` + `vv_capture_session`.
- `vv_wrap_dispatch`: `tier` is a string read against `[wrap.tiers]`. If
  the operator hasn't defined the tier you pass, the dispatch errors
  with a pointer at the config section.

## Why this layout

Phase 4 of wrap-model-tiering (iter 162) routes wrap-executor work
through `vv_wrap_dispatch` to reduce orchestrator-side token output.
Iter 161 baseline was ~17 minutes / 60K tokens for an inline-Opus wrap
(bucket (c), full Phases 1-4). The dispatch path lets a sonnet executor
handle ~80% of wraps and only escalates to opus when sonnet's quality
gate fails. Per-dispatch telemetry lands in
`~/.cache/vibe-vault/wrap-dispatch.jsonl`; aggregate it with
`vv stats wrap` to track tier durations, escalation rates, and top
escalation reasons.

The orchestrator owns the ladder rather than the server because the
ladder is a natural-language judgement (when to escalate, which prior
attempts to surface in the next prompt) — keeping that logic close to
the user-facing chat session reads cleaner than a server-side state
machine.

Do not add "Co-Authored-By" lines to commit messages or source files.

Do not ask for confirmation — just do the updates, stage the files, show
what changed, and note that the user should review before committing.
