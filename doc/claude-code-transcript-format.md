# Claude Code JSONL Transcript Format Reference

This document describes the structure of JSONL transcript files written by
Claude Code during sessions. Each line is a complete JSON object representing
one event. The transcript file lives at
`~/.claude/projects/{project-path-slug}/{session-uuid}.jsonl`.

All examples below are extracted from real transcripts on this machine.

---

## Table of Contents

1. [File Location and Naming](#file-location-and-naming)
2. [Entry Types Overview](#entry-types-overview)
3. [Common Fields](#common-fields)
4. [Entry Type: `user`](#entry-type-user)
5. [Entry Type: `assistant`](#entry-type-assistant)
6. [Entry Type: `system`](#entry-type-system)
7. [Entry Type: `file-history-snapshot`](#entry-type-file-history-snapshot)
8. [Entry Type: `progress`](#entry-type-progress)
9. [Content Blocks](#content-blocks)
10. [Tool Use and Tool Results](#tool-use-and-tool-results)
11. [Token Accounting (Usage)](#token-accounting-usage)
12. [Hook Event Input (stdin JSON)](#hook-event-input-stdin-json)
13. [Field Inventory](#field-inventory)
14. [Entry Type Distribution (Real Example)](#entry-type-distribution-real-example)

---

## File Location and Naming

```
~/.claude/projects/
├── -home-johns-code-vibe-vault/           # project slug (path with - for /)
│   ├── 93a206c3-f40c-4201-9ada-17142784919d.jsonl   # main session transcript
│   ├── 5ba568d6-c8b4-4533-9d75-87b9fb8799bf.jsonl   # another session
│   └── CLAUDE.md                                      # project instructions
├── -home-johns-code-recmeet/
│   ├── 7a9bf70d-266b-4f36-97b8-db5dfb05fb27.jsonl   # 7.1 MB, 2169 lines
│   └── ...
```

- **Filename** = session UUID + `.jsonl`
- **One JSON object per line** (newline-delimited JSON)
- **Line sizes** can reach 10+ MB for large assistant responses
- **Subagent transcripts** live under `{project}/subagents/`

---

## Entry Types Overview

| Type | Role | Description | Frequency |
|------|------|-------------|-----------|
| `user` | user message | User prompts and tool results | Common |
| `assistant` | assistant response | Claude responses with tool calls | Common |
| `system` | system event | Compaction, stop hooks, turn timing | Occasional |
| `file-history-snapshot` | file tracking | Backup snapshots before edits | Per-edit |
| `progress` | progress update | Hook execution, streaming progress | Very frequent |

**Real distribution from a 994-line transcript (3.5 MB):**

```
assistant (text only):        96
assistant (with thinking):    21
assistant (with tool_use):   208
file-history-snapshot:        38
progress:                    389
system/compact_boundary:       1
system/stop_hook_summary:      4
system/turn_duration:          8
user (plain message):         16
user (isMeta):                 5
user (tool_result):          208
```

---

## Common Fields

Every entry shares these root-level fields:

```json
{
  "type":        "user|assistant|system|file-history-snapshot|progress",
  "uuid":        "70122267-c3e1-4db4-a71c-d0a2a28e645b",
  "parentUuid":  "2ecfbca8-03da-4bf4-bd92-338e95fc413a",
  "sessionId":   "93a206c3-f40c-4201-9ada-17142784919d",
  "timestamp":   "2026-02-27T14:56:15.319Z",
  "cwd":         "/home/johns/code/vibe-vault",
  "version":     "2.1.59",
  "gitBranch":   "main",
  "slug":        "declarative-swimming-sun",
  "isSidechain": false,
  "userType":    "external"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Entry type discriminator |
| `uuid` | UUID | Unique identifier for this entry |
| `parentUuid` | UUID\|null | UUID of the preceding entry in the conversation thread |
| `sessionId` | UUID | Session identifier (matches filename) |
| `timestamp` | ISO 8601 | When this entry was created (UTC) |
| `cwd` | string | Working directory at time of entry |
| `version` | string | Claude Code version (e.g., "2.1.59") |
| `gitBranch` | string | Git branch name (empty if not in a repo) |
| `slug` | string | Human-readable session slug (e.g., "declarative-swimming-sun") |
| `isSidechain` | bool | Whether this is a side conversation (subagent) |
| `userType` | string | Always "external" for user-facing sessions |

---

## Entry Type: `user`

User entries represent either **user-authored messages** or **tool results**.

### Plain User Message

```json
{
  "type": "user",
  "uuid": "2b2256ba-1a76-4239-8f42-e58780f3196e",
  "parentUuid": null,
  "sessionId": "93a206c3-f40c-4201-9ada-17142784919d",
  "timestamp": "2026-02-27T14:56:11.031Z",
  "cwd": "/home/johns/code/vibe-vault",
  "version": "2.1.59",
  "gitBranch": "main",
  "message": {
    "role": "user",
    "content": "Implement the following plan:\n\n# Phase 4: Backfill..."
  },
  "planContent": "# Phase 4: Backfill and Archive\n\n## Context\n..."
}
```

**Key fields:**
- `message.content` — can be a **string** (plain text) or an **array** of content blocks
- `planContent` — present when the user submitted a plan; contains the full plan text
- `isMeta` — **not present** on genuine user messages

### Meta/System-Injected User Message

```json
{
  "type": "user",
  "isMeta": true,
  "message": {
    "role": "user",
    "content": "<local-command-caveat>Caveat: The messages below were generated by the user while running local commands. DO NOT respond to these messages or otherwise consider them in your response unless the user explicitly asks you to.</local-command-caveat>"
  }
}
```

**`isMeta: true`** marks system-injected messages that should be skipped when
looking for the user's actual intent. Common patterns:
- `<local-command-caveat>` wrappers
- Slash command expansions (`/resume`, `/wrap`, etc.)
- Skill/command output (`<command-name>`, `<local-command-stdout>`)

### Slash Command Message

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": "<command-name>/resume</command-name>\n            <command-message>resume</command-message>\n            <command-args></command-args>"
  }
}
```

### Tool Result (from tool execution)

```json
{
  "type": "user",
  "uuid": "f20898d5-f1b5-4ecc-a29b-c9d62401e6bc",
  "parentUuid": "70122267-c3e1-4db4-a71c-d0a2a28e645b",
  "sourceToolAssistantUUID": "70122267-c3e1-4db4-a71c-d0a2a28e645b",
  "message": {
    "role": "user",
    "content": [
      {
        "type": "tool_result",
        "tool_use_id": "toolu_01KNv9w2f8FgND1ARKhfWyKv",
        "content": "File does not exist. Note: your current working directory is /home/johns/code/vibe-vault.",
        "is_error": true
      }
    ]
  },
  "toolUseResult": "Error: File does not exist..."
}
```

**Key fields:**
- `message.content` is an **array** with `tool_result` blocks
- `tool_use_id` links back to the `id` of the `tool_use` block in the assistant message
- `is_error` — true if the tool execution failed
- `sourceToolAssistantUUID` — UUID of the assistant entry that requested this tool
- `toolUseResult` — string representation of the tool output (redundant with content)

---

## Entry Type: `assistant`

Assistant entries contain Claude's responses — text, thinking, and tool calls.

### Text-Only Response

```json
{
  "type": "assistant",
  "uuid": "2ecfbca8-03da-4bf4-bd92-338e95fc413a",
  "parentUuid": "2b2256ba-1a76-4239-8f42-e58780f3196e",
  "requestId": "req_011CYYmW7pQPSRpoEoUpiBjE",
  "message": {
    "model": "claude-opus-4-6",
    "id": "msg_016hmgMATAbfAzSFUyjUszw2",
    "type": "message",
    "role": "assistant",
    "content": [
      {
        "type": "text",
        "text": "I'll start by reading the key existing files..."
      }
    ],
    "stop_reason": null,
    "stop_sequence": null,
    "usage": {
      "input_tokens": 3,
      "cache_creation_input_tokens": 3818,
      "cache_read_input_tokens": 18792,
      "output_tokens": 2
    }
  }
}
```

### Response with Thinking

```json
{
  "type": "assistant",
  "message": {
    "model": "claude-opus-4-6",
    "role": "assistant",
    "content": [
      {
        "type": "thinking",
        "thinking": "The memory file doesn't exist yet. Let me read the key files individually.",
        "signature": "EvIBCkYICxgCKkBHSCCt0HN..."
      }
    ],
    "usage": { "..." }
  }
}
```

**Note:** Thinking blocks have a `signature` field (cryptographic signature for
extended thinking verification). This field has no practical use for transcript
analysis.

### Response with Tool Use

```json
{
  "type": "assistant",
  "message": {
    "model": "claude-opus-4-6",
    "role": "assistant",
    "content": [
      {
        "type": "tool_use",
        "id": "toolu_01KNv9w2f8FgND1ARKhfWyKv",
        "name": "Read",
        "input": {
          "file_path": "/home/johns/.claude/projects/.../MEMORY.md"
        },
        "caller": {
          "type": "direct"
        }
      }
    ],
    "usage": { "..." }
  }
}
```

**An assistant entry may contain multiple content blocks** — text + thinking +
multiple tool_use calls in a single response. The `content` array can mix types.

**Key assistant-specific fields:**

| Field | Type | Description |
|-------|------|-------------|
| `requestId` | string | API request ID (e.g., "req_011CYYmW...") |
| `message.model` | string | Model used (e.g., "claude-opus-4-6") |
| `message.id` | string | API message ID (e.g., "msg_016hmg...") |
| `message.stop_reason` | string\|null | Why generation stopped ("end_turn", "tool_use", null) |
| `message.usage` | object | Token accounting (see [Usage](#token-accounting-usage)) |

---

## Entry Type: `system`

System entries are injected by Claude Code, not by the user or model.

### `compact_boundary` — Context Compaction

```json
{
  "type": "system",
  "subtype": "compact_boundary",
  "content": "Conversation compacted",
  "isMeta": false,
  "level": "info",
  "logicalParentUuid": "db07d9a2-c198-46ad-b688-8479a637bda1",
  "compactMetadata": {
    "trigger": "auto",
    "preTokens": 168597
  }
}
```

**Key fields:**
- `compactMetadata.trigger` — "auto" (hit context limit) or "manual" (/compact)
- `compactMetadata.preTokens` — token count before compaction
- This entry marks where earlier context was summarized/compressed

### `stop_hook_summary` — Stop Hook Execution

```json
{
  "type": "system",
  "subtype": "stop_hook_summary",
  "hookCount": 1,
  "hookInfos": [
    {
      "command": "bun ~/.claude/hooks/learning-sync.hook.ts",
      "durationMs": 48
    }
  ],
  "hookErrors": [
    "Failed with non-blocking status code: error: ENOENT reading..."
  ],
  "preventedContinuation": false,
  "stopReason": "",
  "hasOutput": true,
  "level": "suggestion"
}
```

### `turn_duration` — Turn Timing

```json
{
  "type": "system",
  "subtype": "turn_duration",
  "durationMs": 273253,
  "isMeta": false
}
```

Records wall-clock time for a complete turn (user prompt → all tool
calls → final response).

---

## Entry Type: `file-history-snapshot`

Tracks file state for undo/restore capability.

### Empty Snapshot (session start)

```json
{
  "type": "file-history-snapshot",
  "messageId": "2b2256ba-1a76-4239-8f42-e58780f3196e",
  "snapshot": {
    "messageId": "2b2256ba-1a76-4239-8f42-e58780f3196e",
    "trackedFileBackups": {},
    "timestamp": "2026-02-27T14:56:11.404Z"
  },
  "isSnapshotUpdate": false
}
```

### Snapshot with File Backups (after edits)

```json
{
  "type": "file-history-snapshot",
  "messageId": "e2e802b7-6595-4e60-a442-ee449062fd25",
  "isSnapshotUpdate": true,
  "snapshot": {
    "messageId": "2b2256ba-1a76-4239-8f42-e58780f3196e",
    "timestamp": "2026-02-27T14:56:11.404Z",
    "trackedFileBackups": {
      "internal/index/index.go": "...base64 or content..."
    }
  }
}
```

- `isSnapshotUpdate: true` means this updates an existing snapshot
- `trackedFileBackups` maps relative file paths to their pre-edit content
- These entries are **skipped** during transcript parsing (not conversation data)

---

## Entry Type: `progress`

High-frequency entries for streaming and hook progress.

```json
{
  "type": "progress",
  "data": {
    "type": "hook_progress",
    "hookEvent": "PostToolUse",
    "hookName": "PostToolUse:Read",
    "command": "callback"
  },
  "parentToolUseID": "toolu_011UVsToFwv7PDzwcXuKXrLH",
  "toolUseID": "toolu_011UVsToFwv7PDzwcXuKXrLH"
}
```

Progress entries are the most frequent type (~40% of all lines) and are
**skipped** during transcript parsing (no conversation content).

---

## Content Blocks

The `message.content` field can be either a **string** or an **array of blocks**.

### Text Block

```json
{ "type": "text", "text": "I'll implement the login page." }
```

### Thinking Block

```json
{
  "type": "thinking",
  "thinking": "Let me analyze the codebase structure...",
  "signature": "EvIBCkYICxgC..."
}
```

### Tool Use Block

```json
{
  "type": "tool_use",
  "id": "toolu_01VAHeXL7TzPydMqH6BrBuTU",
  "name": "Write",
  "input": {
    "file_path": "/home/johns/code/vibe-vault/internal/discover/discover.go",
    "content": "package discover\n\nimport (\n..."
  },
  "caller": { "type": "direct" }
}
```

### Tool Result Block

```json
{
  "type": "tool_result",
  "tool_use_id": "toolu_01KNv9w2f8FgND1ARKhfWyKv",
  "content": "File created successfully",
  "is_error": false
}
```

---

## Tool Use and Tool Results

### Pairing

Every `tool_use` block in an **assistant** entry generates a corresponding
`tool_result` block in a subsequent **user** entry. They are linked by the
`id`/`tool_use_id` field:

```
Assistant entry:  tool_use.id = "toolu_01KNv..."
    ↓
User entry:       tool_result.tool_use_id = "toolu_01KNv..."
```

### Tool Input Schemas (by tool name)

**Read:**
```json
{ "file_path": "/absolute/path/to/file.go" }
```

**Write:**
```json
{
  "file_path": "/absolute/path/to/file.go",
  "content": "full file content..."
}
```

**Edit:**
```json
{
  "file_path": "/absolute/path/to/file.go",
  "old_string": "text to find",
  "new_string": "replacement text",
  "replace_all": false
}
```

**Bash:**
```json
{
  "command": "go test ./...",
  "description": "Run all tests"
}
```

**Grep:**
```json
{
  "pattern": "func Extract",
  "path": "/home/johns/code/vibe-vault",
  "output_mode": "content"
}
```

**Glob:**
```json
{ "pattern": "**/*.go" }
```

**Task (subagent delegation):**
```json
{
  "description": "Research auth patterns",
  "prompt": "Find all authentication...",
  "subagent_type": "Explore"
}
```

**AskUserQuestion:**
```json
{
  "questions": [
    {
      "question": "Which database should we use?",
      "header": "Database",
      "options": [
        { "label": "PostgreSQL", "description": "..." },
        { "label": "SQLite", "description": "..." }
      ],
      "multiSelect": false
    }
  ]
}
```

**EnterPlanMode / ExitPlanMode:**
```json
{}
```

### The `caller` Field

Tool use blocks include a `caller` field indicating how the tool was invoked:

```json
"caller": { "type": "direct" }
```

Known values: `"direct"` (Claude called it directly).

---

## Token Accounting (Usage)

Present on every assistant message:

```json
{
  "usage": {
    "input_tokens": 3,
    "output_tokens": 200,
    "cache_creation_input_tokens": 3818,
    "cache_read_input_tokens": 18792,
    "cache_creation": {
      "ephemeral_5m_input_tokens": 0,
      "ephemeral_1h_input_tokens": 3818
    },
    "service_tier": "standard",
    "inference_geo": "not_available"
  }
}
```

| Field | Description |
|-------|-------------|
| `input_tokens` | Fresh (non-cached) input tokens |
| `output_tokens` | Output tokens generated |
| `cache_creation_input_tokens` | Tokens written to prompt cache |
| `cache_read_input_tokens` | Tokens read from prompt cache |
| `cache_creation.ephemeral_5m_input_tokens` | 5-minute cache tier |
| `cache_creation.ephemeral_1h_input_tokens` | 1-hour cache tier |
| `service_tier` | API service tier ("standard") |
| `inference_geo` | Inference geography |

**Total input tokens** = `input_tokens` + `cache_read_input_tokens` + `cache_creation_input_tokens`

---

## Hook Event Input (stdin JSON)

When Claude Code fires a hook (SessionEnd, Stop, PreToolUse, PostToolUse), it
sends JSON via stdin with these fields:

### Common Fields (all hooks)

```json
{
  "session_id": "93a206c3-f40c-4201-9ada-17142784919d",
  "transcript_path": "~/.claude/projects/-home-johns-code-vibe-vault/93a206c3-f40c-4201-9ada-17142784919d.jsonl",
  "cwd": "/home/johns/code/vibe-vault",
  "permission_mode": "default",
  "hook_event_name": "SessionEnd"
}
```

### SessionEnd

```json
{
  "session_id": "...",
  "transcript_path": "...",
  "cwd": "...",
  "hook_event_name": "SessionEnd",
  "reason": "prompt_input_exit"
}
```

**Reason values:** `"clear"`, `"logout"`, `"prompt_input_exit"`,
`"bypass_permissions_disabled"`, `"other"`

### Stop

```json
{
  "session_id": "...",
  "transcript_path": "...",
  "cwd": "...",
  "hook_event_name": "Stop",
  "stop_hook_active": false,
  "last_assistant_message": "Done! The feature is complete."
}
```

### PreToolUse

```json
{
  "session_id": "...",
  "hook_event_name": "PreToolUse",
  "tool_name": "Bash",
  "tool_input": { "command": "rm -rf /tmp/test" },
  "tool_use_id": "toolu_01VAHeXL7TzPydMqH6BrBuTU"
}
```

### PostToolUse

```json
{
  "session_id": "...",
  "hook_event_name": "PostToolUse",
  "tool_name": "Write",
  "tool_input": { "file_path": "...", "content": "..." },
  "tool_use_id": "toolu_01VAHeXL7TzPydMqH6BrBuTU",
  "tool_result": "File created successfully"
}
```

---

## Field Inventory

### Root-level fields (all entry types)

| Field | Type | Present on | Description |
|-------|------|-----------|-------------|
| `type` | string | all | Entry type discriminator |
| `uuid` | UUID | all | Unique entry identifier |
| `parentUuid` | UUID\|null | all | Previous entry in thread |
| `logicalParentUuid` | UUID | system | Logical parent (for compaction) |
| `sessionId` | UUID | all | Session identifier |
| `timestamp` | ISO 8601 | all | Entry creation time (UTC) |
| `cwd` | string | all | Working directory |
| `version` | string | all | Claude Code version |
| `gitBranch` | string | all | Git branch |
| `slug` | string | all | Human-readable session name |
| `isSidechain` | bool | all | Subagent conversation flag |
| `userType` | string | all | Always "external" |
| `isMeta` | bool | user, system | System-injected message flag |
| `message` | object | user, assistant | Message payload |
| `requestId` | string | assistant | API request ID |
| `subtype` | string | system | System event subtype |
| `toolUseResult` | string\|object | user (tool_result) | Tool execution output |
| `sourceToolAssistantUUID` | UUID | user (tool_result) | Assistant that requested tool |
| `planContent` | string | user | Full plan text (when submitting a plan) |
| `data` | object | progress | Progress event data |
| `parentToolUseID` | string | progress | Related tool use |
| `toolUseID` | string | progress | Related tool use |
| `snapshot` | object | file-history-snapshot | File backup data |
| `messageId` | string | file-history-snapshot | Related message |
| `isSnapshotUpdate` | bool | file-history-snapshot | Update vs new snapshot |

### Message fields

| Field | Type | Present on | Description |
|-------|------|-----------|-------------|
| `role` | string | all messages | "user" or "assistant" |
| `content` | string\|array | all messages | Text or content block array |
| `model` | string | assistant | Model name |
| `id` | string | assistant | API message ID |
| `type` | string | assistant | Always "message" |
| `stop_reason` | string\|null | assistant | "end_turn", "tool_use", null |
| `stop_sequence` | string\|null | assistant | Stop sequence if hit |
| `usage` | object | assistant | Token accounting |

### System entry subtypes

| Subtype | Description | Key fields |
|---------|-------------|------------|
| `compact_boundary` | Context was compacted | `compactMetadata.trigger`, `compactMetadata.preTokens` |
| `stop_hook_summary` | Stop hook executed | `hookCount`, `hookInfos`, `hookErrors`, `preventedContinuation` |
| `turn_duration` | Turn wall-clock time | `durationMs` |

---

## Entry Type Distribution (Real Example)

From a 994-line, 3.5 MB transcript of a vibe-vault development session:

| Pattern | Count | % |
|---------|-------|---|
| progress (hook/streaming) | 389 | 39.1% |
| assistant with tool_use | 208 | 20.9% |
| user with tool_result | 208 | 20.9% |
| assistant (text only) | 96 | 9.7% |
| file-history-snapshot | 38 | 3.8% |
| assistant with thinking | 21 | 2.1% |
| user (plain message) | 16 | 1.6% |
| system/turn_duration | 8 | 0.8% |
| user (isMeta) | 5 | 0.5% |
| system/stop_hook_summary | 4 | 0.4% |
| system/compact_boundary | 1 | 0.1% |

**Observations:**
- ~40% of lines are `progress` entries (skipped during parsing)
- Tool use and tool result entries always appear in equal numbers (1:1 pairing)
- Actual user messages are only ~1.6% of all entries
- `file-history-snapshot` entries appear before each file-modifying tool call
- Thinking blocks appear in ~18% of assistant responses (21 out of 117 non-tool responses)

---

## Notes for vibe-vault Processing

The transcript parser (`internal/transcript/parser.go`) skips these entry types:
- `file-history-snapshot` — file backup data, not conversation
- `progress` — streaming/hook progress, not conversation

The narrative extractor (`internal/narrative/extract.go`) processes:
- `assistant` entries with `tool_use` blocks → mapped to Activities
- `user` entries with `tool_result` blocks → paired with tool uses for success/failure
- `system` entries with `subtype: compact_boundary` → segment boundaries
- `user` entries (plain) → user requests for title inference

Entries with `isMeta: true` are skipped when looking for meaningful user messages.
