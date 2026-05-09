# Zed Agent Panel Integration: Investigation Findings (iter 232)

## Status

This document captures the architectural findings from the iter-232
`zed-extension-spike` task. The spike's primary deliverable — a Rust+WASM
extension exposing `/vv-restart` in Zed — was built and installed
successfully, but **does not surface in the Agent panel where the operator
actually works**. The right path for Agent-panel slash commands is **MCP
prompts**, not the Zed extension API.

This document is the evidence trail before pivoting iteration 233+ to the
MCP-prompts approach. All claims here are cited to specific files, URLs,
or empirical observations.

## Goal of the spike

Ship a minimal `zed-vibe-vault` extension that registers `/vv-restart`
in Zed's assistant. Resolve the slash-command body by shelling out to
`vv command get restart`, returning the canonical body from `vv`'s
embedded templates. See `agentctx/tasks/zed-extension-spike.md` for the
full plan.

## What was built (and works for what it targets)

The spike landed on the `zed-extension-spike` branch with these
components, all green under `make pre-commit`:

| Component                                  | Path                                                | Status                                       |
|--------------------------------------------|-----------------------------------------------------|----------------------------------------------|
| Rust+WASM extension crate                  | `zed-extension/`                                    | Builds clean (`cargo build --target wasm32-wasip1`) |
| Extension manifest                         | `zed-extension/extension.toml`                      | `[slash_commands.vv-restart]` + `[[capabilities]] kind="process:exec"` |
| Extension implementation                   | `zed-extension/src/lib.rs`                          | `Extension::run_slash_command` shells out to `vv command get` |
| Go-side `vv command get <name>` subcommand | `cmd/vv/command.go` + `cmd/vv/command_test.go`      | 6 tests (incl. enumeration over all 8 templates) all PASS |
| Help registration                          | `internal/help/commands.go`                         | `CmdCommand` + `CmdCommandGet` + `CommandSubcommands` |
| Man-page generation                        | `cmd/gen-man/main.go`                               | Generates `vv-command.1` + `vv-command-get.1` |
| Makefile facade                            | `Makefile` `##@ Zed Extension` section              | `make zed-extension-build` works |
| Verdict gap report (now superseded by this doc) | `zed-extension/GAP-REPORT.md`                      | Written; partial — missed the Agent-vs-Assistant split |

Operator was able to install the extension (`zed: install dev extension`
succeeded; resolved evidence below). The extension compiled to a 678 KB
WASM component and was placed at
`~/.local/share/zed/extensions/installed/vibe-vault/`.

## What does not work, and why

The operator typed `/vv-restart` in Zed's Agent panel and received:

> The /vv-restart command is not supported by Zed Agent.
> Available commands: /vv_session_guidelines

This is the central finding: **extension `[slash_commands.X]`
registrations do not reach Zed's Agent panel**. The `/vv_session_guidelines`
the user saw is sourced from a completely different mechanism (vibe-vault's
own MCP server), confirmed below.

### Evidence: where `/vv_session_guidelines` actually comes from

The vibe-vault MCP server (`vv mcp`) registers this prompt at session
start:

- `internal/mcp/prompts.go:14-40` — `NewSessionGuidelinesPrompt()`
  returns a `Prompt{Definition: PromptDef{Name: "vv_session_guidelines", ...}}`.
- `internal/mcp/server.go:109` —
  `srv.RegisterPrompt(NewSessionGuidelinesPrompt())` is called from
  `RegisterAllTools`.
- `vv check --json` reports `enrichment` and `synthesis` rows; tool
  surface count is "43 tools + 1 prompt" (the prompt being this one).
- The operator's `~/.config/zed/settings.json` has `vibe-vault` listed
  under `context_servers` with `command = "vv"`, `args = ["mcp"]` —
  i.e., Zed launches the same `vv mcp` server we ship.

The MCP server speaks JSON-RPC `prompts/list` and `prompts/get`
endpoints (see `internal/mcp/server.go:215-217`). Zed's Agent panel
calls `prompts/list` against each configured `context_servers.*`
entry and surfaces the prompt names as slash commands. That is why
`/vv_session_guidelines` appeared without any extension involvement.

### Evidence: extension slash commands target the legacy Assistant panel

Two upstream Zed conversations confirm the structural split:

- [zed-industries/zed#41405](https://github.com/zed-industries/zed/issues/41405)
  ("AI: Cannot use slash commands in third-party External AI Agents")
  — operators using external AI agents (qwen-code, iflow) reported
  `/help` and other slash commands return *"Available commands: none"*
  in the Agent panel. The bug was eventually closed; the implicit
  resolution is that the Agent panel uses the agent's own command
  surface (or MCP prompts), not extension registrations.
- [zed-industries/zed#53908](https://github.com/zed-industries/zed/discussions/53908)
  ("Add reasoning effort toggle and slash commands for Claude Code
  (ACP) in Agent Panel") — even Claude Code's own native slash
  commands (`/compact`, `/effort`, etc.) do not pass through Zed's
  Agent panel today. The discussion notes: *"slash commands that are
  available in Claude Code CLI ... are completely inaccessible when
  running inside Zed's Agent Panel. The chat input doesn't pass them
  through to the agent."*

The published Zed documentation reinforces this:

- [Zed Slash Command Extensions docs](https://github.com/zed-industries/zed/blob/main/docs/src/extensions/slash-commands.md)
  are framed as *"slash commands for use in the Assistant"* (i.e., the
  legacy Assistant panel), not the Agent panel.
- [Zed Agent Panel docs](https://zed.dev/docs/ai/agent-panel) describe
  the Agent panel's context surface as `@`-mentions, tool profiles,
  and MCP servers — and **make no mention of extension slash
  commands** as a contribution surface for the Agent panel. The
  capability list given for extensions on
  [developing-extensions](https://zed.dev/docs/extensions/developing-extensions)
  is: languages, debuggers, themes/icon themes, snippets, MCP/context
  servers — slash commands are documented separately and historically
  scoped to the Assistant panel.

### Evidence: the operator IS in the Agent panel

The operator's `~/.config/zed/settings.json` has:

```json
{
  "agent": {
    "default_model": {
      "model": "grok-code-fast-1",
      "provider": "x_ai"
    },
    ...
  },
  "context_servers": {
    "vibe-vault": { "command": "vv", "args": ["mcp"] },
    "vibe-palace": { "command": "vp", "args": ["mcp"] }
  },
  "language_models": {
    "providers": {
      "xai": { "models": [...] }
    }
  }
}
```

The Agent panel routed to xAI/Grok is the operator's primary workflow.
The Assistant panel may still exist in the Zed UI but is not the panel
the operator uses; iteration 230's `grok-provider-support` and
iteration 231's GROK-WORKFLOW-MIGRATION refresh both target the Agent
panel + xAI workflow as canonical.

## Verified extension API surface (kept for completeness)

Independent of the panel issue, the spike did successfully verify what
the extension API exposes. This investigation cost is not wasted: any
future extension-side feature must work within these constraints.

| Feature                              | Status                                       | Source                                                  |
|--------------------------------------|----------------------------------------------|---------------------------------------------------------|
| Slash commands                       | Available (Assistant panel only)             | [SlashCommand struct](https://docs.rs/zed_extension_api/latest/zed_extension_api/struct.SlashCommand.html), [SlashCommandOutput](https://docs.rs/zed_extension_api/latest/zed_extension_api/struct.SlashCommandOutput.html), [Section](https://docs.rs/zed_extension_api/latest/zed_extension_api/struct.SlashCommandOutputSection.html) |
| `process:exec` capability            | Available                                    | [extensions/capabilities](https://zed.dev/docs/extensions/capabilities) |
| `process::Command` runtime           | Available; no stdin / no current_dir         | [Command](https://docs.rs/zed_extension_api/latest/zed_extension_api/process/struct.Command.html), [Output](https://docs.rs/zed_extension_api/latest/zed_extension_api/process/struct.Output.html) |
| `Worktree::read_text_file(rel)`      | Available; single-file only; sandboxed       | [Worktree](https://docs.rs/zed_extension_api/latest/zed_extension_api/struct.Worktree.html) |
| `Worktree::which(bin)`               | Available                                    | same                                                    |
| `Worktree::shell_env()`              | Available                                    | same                                                    |
| `std::fs::*` outside extension dir   | Sandboxed; returns `os error 44`             | [zed#16933 maintainer comment](https://github.com/zed-industries/zed/issues/16933) |
| `chdir` from extension               | Forbidden; `register_extension!` overrides to `errno=58 NOTSUP` | [extension_api.rs L304-322 in upstream main branch](https://github.com/zed-industries/zed/blob/main/crates/extension_api/src/extension_api.rs) |
| Status-bar / panels / modals         | Not available                                | maintainer comment on #16933 ("no support for modifying the UI to create new panels") |
| Codelens / inline insights / gutter decorations | Not available                       | same                                                    |
| MCP/context-server registration      | Available                                    | `Extension::context_server_command` trait method        |

A separate finding worth highlighting: the `register_extension!`
macro source actively forbids `chdir` from extension code. The
relevant excerpt (paraphrased from
`crates/extension_api/src/extension_api.rs` in `zed-industries/zed`):

```rust
#[unsafe(no_mangle)]
pub unsafe extern "C" fn chdir(raw_path: *const std::ffi::c_char) -> i32 {
    // Forbid extensions from changing CWD and so return an appropriate error code.
    errno = 58; // NOTSUP
    return -1;
}
```

This is by design and reinforces that any cwd-dependent behavior must
be handled by the host process the extension shells out to.

## Operational observations

### Install-Dev-Extension UX gotcha

The operator initially saw:

> Failed to install dev extension: No extension manifest found for
> extension target

This error string is generated at
[`crates/extension/src/extension_manifest.rs`](https://github.com/zed-industries/zed/blob/main/crates/extension/src/extension_manifest.rs#L35-L37)
and substitutes `extension_dir.file_name()` into the message. The
substituted name `target` indicated the file picker had landed on
`zed-extension/target/` (the cargo build artifact directory) instead
of `zed-extension/`.

Workaround: delete `zed-extension/target/` before the install
attempt; Zed compiles the wasm itself during install and does not
need a pre-existing `target/`. Also: Zed's compiled output lands at
`zed-extension/extension.wasm` (in the source directory itself), not
at `zed-extension/target/...`. Both paths are now in `.gitignore`.

The picker invocation source
([`crates/extensions_ui/src/extensions_ui.rs:111-126`](https://github.com/zed-industries/zed/blob/main/crates/extensions_ui/src/extensions_ui.rs))
uses `gpui::PathPromptOptions { files: false, directories: true,
multiple: false, ... }` — a directory-only picker. The user must
select the directory containing `extension.toml` itself (no drilling
in or out).

### MCP prompt invocation produced "cruft"

Operator reported that running `/vv_session_guidelines` in the Agent
panel "confused the heck out of the local AI there and probably added
'cruft' to our session history."

Reading the prompt body (`internal/mcp/prompts.go:42-82`), the text
instructs the AI to call `vv_capture_session` *"when you are finishing
a work session ... or have completed a significant unit of work."*
The prompt is intended as a **session-start** preamble (per its
description: *"Include this at session start to enable automatic
session capture"*). When invoked mid-session by an Agent-panel user,
the AI may interpret it as a directive to capture immediately,
producing a low-value session note. **Action item for a future
iteration:** review the prompt body for mid-session-invocation
robustness, or document in the prompt that it should only be used at
session start, or split into `vv_session_start` (preamble) +
`vv_session_capture_now` (explicit trigger). Tracked as a thread,
not promoted to a task in this iteration.

## Multi-client concurrency: design constraint surfaced this iter

The operator raised a forward-looking constraint:

> What happens if I am working in Claude Code and the Zed Agentic
> panel at the same time on the same project? I totally see Claude
> writing the plans and Grok executing them in the near future. This
> is a situation we don't want to design ourselves out of!

Two clients (Claude Code via its native MCP integration, Zed via
`context_servers.vibe-vault`) on the same machine, same project,
concurrently. Each client launches its **own** `vv mcp` subprocess.
Both subprocesses access the same vault filesystem.

This is structurally a new dimension on the existing multi-host vault
sync concern (DESIGN #103, the lazy-freshness-gate proposal in
`agentctx/tasks/mcp-driven-vault-sync.md`):

| Existing scenario        | Resolution today                                                          |
|--------------------------|---------------------------------------------------------------------------|
| Multiple hosts            | Vault git sync. Iter-197 fixed the keep-local resolver; lazy freshness gate proposed in `mcp-driven-vault-sync` (Draft v4) bounds divergence further. |
| Multiple clients, one host | **Not explicitly handled today.** Each `vv mcp` is a separate process accessing shared vault state via filesystem.        |

What "works" today by accident:

- **Read-only operations** (`vv_get_resume`, `vv_list_tasks`, etc.):
  always safe; pure reads from filesystem.
- **Append-only operations** (`vv_append_iteration` is content-addressable
  idempotent per DESIGN #106): two simultaneous appends with identical
  bodies are safely no-op'd.
- **Vault git sync** (`vv vault push`): atomic at the git-commit
  boundary; concurrent pushes from same host serialize through git
  index lock.

What may NOT be safe under concurrent two-client load:

- **`commit.msg` writes from `/wrap` flows**: both clients running
  `/wrap` simultaneously could clobber each other's `commit.msg`
  before the operator commits.
- **Task file writes (`vv_manage_task`)**: two clients creating
  different tasks in parallel are likely safe (different filenames),
  but two clients editing the same task file race.
- **Resume.md writes (`vv_update_resume`)**: marker-block-bounded
  writes (DESIGN #90) provide some safety, but two simultaneous
  edits to the same block could lose data.
- **Wrap-bundle skeleton cache writes**: per-project rotation
  (DESIGN #91) but cache writers don't lock.

**Action item for iteration 233 or later:** review the MCP write-path
tools for concurrent-writer safety. Candidate mitigations:

1. **File-lock the host-local staging dir** (`~/.local/share/vibe-vault/...`)
   when any MCP write is in flight. Already a constrained surface
   (DESIGN around staging migration).
2. **Lazy freshness gate at the MCP boundary** (proposed in
   `agentctx/tasks/mcp-driven-vault-sync.md`) extends naturally to
   "is the on-disk state newer than what I last read?" not just
   "is the vault upstream newer?" — covers the parallel-clients case
   structurally.
3. **Write-side advisory locks** on specific files
   (`flock(LOCK_EX, "<path>.lock")`) for tasks/, resume, commit.msg.
4. **Document the limitation explicitly** so operators avoid running
   `/wrap` simultaneously from two clients on the same project.

This is not blocking the MCP-prompts pivot; flagged here so the
pivot's design docs reference it. The MCP-prompts mechanism itself
is read-only on the prompt-fetch path (it returns text) and does not
introduce new concurrent-write hazards.

## Implications for vibe-vault: pivot plan

### What survives from the spike

- **`cmd/vv/command.go` — KEEP.** A clean CLI surface for printing
  embedded slash-command bodies has uses outside Zed (CI, scripting,
  AI clients without MCP, debugging). 30 LoC of code, 6 tests, full
  help/man-page integration. Not coupled to the extension at all.
- **`templates.Registry.DefaultContent("agentctx/commands/<name>.md")`
  call pattern — KEEP.** This is the right way to fetch canonical
  command bodies and will be used directly by the new MCP prompts.

### What gets retired

- **`zed-extension/` directory (the Rust+WASM crate) — RETIRE.** The
  extension reaches the Assistant panel only, which is not the
  operator's working surface. Maintaining a parallel surface for a
  panel the operator does not use creates ongoing maintenance cost
  with no value delivered. The branch deliberately stops short of
  committing the extension code; the iteration 233 pivot will
  delete `zed-extension/` from the repo before merging the rest of
  the branch's changes.
- **`Makefile`'s `##@ Zed Extension` section** — RETIRE alongside
  `zed-extension/`. No targets remain after the directory is gone.
- **`zed-extension/GAP-REPORT.md`** — superseded by this document.

### What gets added (iteration 233)

- **MCP prompts in `internal/mcp/prompts.go`** mirroring the existing
  `NewSessionGuidelinesPrompt` shape:
  - `NewRestartPrompt()` → `vv_restart` → returns
    `templates.New().DefaultContent("agentctx/commands/restart.md")`
  - Optionally: `NewWrapPrompt()`, `NewReviewPlanPrompt()`,
    `NewExecutePlanPrompt()`, etc. — one per `agentctx/commands/*.md`.
- **`RegisterPrompt` calls in `internal/mcp/server.go:RegisterAllTools`**
  for each new prompt.
- **Tests in `internal/mcp/prompts_test.go`** mirroring the existing
  `TestNewSessionGuidelinesPrompt` pattern. The
  `TestRunCommandGet_AllKnownCommands` enumeration in
  `cmd/vv/command_test.go` already locks the slug → relPath mapping;
  the prompt-side test only needs to assert each prompt is registered
  and its handler returns the expected body.
- **`vv_session_guidelines` body review** — flagged above; either
  refine the wording for mid-session safety or split into
  start-vs-trigger variants.

### What stays in the iteration 232 commit

The intent is for this document, plus `cmd/vv/command.go` /
`command_test.go` / help-system entries / Makefile cleanup of any
zed-extension references / the `agentctx/tasks/zed-extension-spike.md`
task being marked **superseded** with this doc as the explanation —
to land as a single iteration-232 commit. The Rust/Cargo files do
NOT land in main. The retired GAP-REPORT.md does NOT land in main.

## Updates to existing documents (iteration 232)

This investigation invalidates parts of two existing documents.
Updates to land in the iteration-232 commit:

- **`doc/GROK-WORKFLOW-MIGRATION.md` "Tier-2 Zed extension" section**
  must be rewritten again (it was just rewritten with the
  extension-shipped framing in iteration 232 phase 5). The corrected
  version should describe the Agent-panel-vs-Assistant-panel split
  and point at the MCP-prompts mechanism as the canonical
  Agent-panel slash-command surface.
- **`doc/DESIGN.md` entry #108** ("Zed extension is a transport
  adapter") should be revised or replaced with a new entry capturing
  the Agent-panel-vs-Assistant-panel split as the actual decision.
  The "transport adapter" framing was correct for the Assistant
  panel but irrelevant for the panel the operator uses.

## References

### Zed extension API documentation

- [zed_extension_api crate root (docs.rs)](https://docs.rs/zed_extension_api)
- [Zed: Developing Extensions](https://zed.dev/docs/extensions/developing-extensions)
- [Zed: Extension Capabilities](https://zed.dev/docs/extensions/capabilities)
- [Zed: Slash Command Extensions docs (in-tree)](https://github.com/zed-industries/zed/blob/main/docs/src/extensions/slash-commands.md)
- [Zed: Agent Panel](https://zed.dev/docs/ai/agent-panel)
- [SlashCommand struct](https://docs.rs/zed_extension_api/latest/zed_extension_api/struct.SlashCommand.html)
- [SlashCommandOutput struct](https://docs.rs/zed_extension_api/latest/zed_extension_api/struct.SlashCommandOutput.html)
- [SlashCommandOutputSection struct](https://docs.rs/zed_extension_api/latest/zed_extension_api/struct.SlashCommandOutputSection.html)
- [Worktree struct (5-method API)](https://docs.rs/zed_extension_api/latest/zed_extension_api/struct.Worktree.html)
- [process::Command](https://docs.rs/zed_extension_api/latest/zed_extension_api/process/struct.Command.html)
- [process::Output](https://docs.rs/zed_extension_api/latest/zed_extension_api/process/struct.Output.html)

### Zed source files cited

(All paths under `zed-industries/zed`, `main` branch at iteration time.)

- [`crates/extension/src/extension_manifest.rs`](https://github.com/zed-industries/zed/blob/main/crates/extension/src/extension_manifest.rs)
  — manifest loader; source of the `"No extension manifest found for
  extension <name>"` error string.
- [`crates/extension_host/src/extension_host.rs`](https://github.com/zed-industries/zed/blob/main/crates/extension_host/src/extension_host.rs)
  — `install_dev_extension` function (line ~955 at investigation time).
- [`crates/extensions_ui/src/extensions_ui.rs`](https://github.com/zed-industries/zed/blob/main/crates/extensions_ui/src/extensions_ui.rs)
  — file-picker invocation for `InstallDevExtension` (line ~111).
- [`crates/extension_api/src/extension_api.rs`](https://github.com/zed-industries/zed/blob/main/crates/extension_api/src/extension_api.rs)
  — `Extension` trait definition; `register_extension!` macro
  forbidding `chdir`.

### Zed GitHub issues / discussions

- [zed-industries/zed#16933](https://github.com/zed-industries/zed/issues/16933)
  — *"Unable to read from fs in slash command extension"*
  (closed, "stale"). Maintainer comment establishes the WASM I/O
  sandbox restriction.
- [zed-industries/zed#17714](https://github.com/zed-industries/zed/discussions/17714)
  — *"How to read file contents in a custom slash command?"*
  (closed, "outdated"). Confirms `Worktree::read_text_file` works
  for single files.
- [zed-industries/zed#17403](https://github.com/zed-industries/zed/discussions/17403)
  — *"The usage of the Slash Command extension"*. General slash-command
  discussion thread.
- [zed-industries/zed#41405](https://github.com/zed-industries/zed/issues/41405)
  — *"AI: Cannot use slash commands in third-party External AI
  Agents"*. **Primary evidence** for the Agent-panel-doesn't-see-extension-slash-commands
  finding.
- [zed-industries/zed#53908](https://github.com/zed-industries/zed/discussions/53908)
  — *"Add reasoning effort toggle and slash commands for Claude Code
  (ACP) in Agent Panel"*. Reinforces #41405 with a Claude-Code-specific
  case.

### vibe-vault source files cited

- `internal/mcp/prompts.go` — `NewSessionGuidelinesPrompt`,
  `sessionGuidelinesText`.
- `internal/mcp/server.go:109` — `RegisterPrompt(NewSessionGuidelinesPrompt())`.
- `internal/mcp/server.go:215-217` — `prompts/list` and `prompts/get`
  JSON-RPC handler dispatch.
- `internal/mcp/prompts_test.go` — pattern for prompt registration tests.
- `cmd/vv/command.go` + `cmd/vv/command_test.go` — the surviving
  `vv command get <name>` subcommand.
- `internal/templates/templates.go` —
  `Registry.DefaultContent(relPath) (body []byte, ok bool)`.

### Spike artifacts (retired, not landing in main)

- `zed-extension/Cargo.toml`, `extension.toml`, `src/lib.rs`,
  `LICENSE`, `README.md`, `GAP-REPORT.md` — built and verified
  functional but reaches the wrong panel; will be removed before
  iteration-232 commit lands.

### Operator setup at investigation time

- `~/.config/zed/settings.json` — Agent panel default model
  `grok-code-fast-1` via `provider: x_ai`; `context_servers` includes
  `vibe-vault` (`command: vv`, `args: ["mcp"]`) and `vibe-palace`.
- `~/.local/share/zed/extensions/installed/vibe-vault/` — the
  successfully-installed (but non-functional in Agent panel)
  Zed extension from this spike.

### Empirical observations

- `vv check --json` at investigation time reports surface=16,
  schema=10, tools=43, plus 1 prompt — the prompt being
  `vv_session_guidelines`.
- `vv mcp check --tools` lists all 43 tool names (none named
  `vv_session_guidelines` — that name is a prompt, not a tool).
- Operator-observed result: invoking `/vv-restart` in the Agent panel
  yielded *"The /vv-restart command is not supported by Zed Agent.
  Available commands: /vv_session_guidelines"*.
