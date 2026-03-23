# vibe-vault Grok Workflow Migration Plan

## Executive Summary

This document outlines a comprehensive plan to migrate the "Context as Code" workflow from Claude Code to Zed editor, using Grok as the primary agentic AI pair programming partner. The plan builds on vibe-vault's existing MCP server infrastructure to emulate key Claude Code features while adapting to Zed's ecosystem and Grok's conversational tooling.

**Goal**: Enable seamless agentic pair programming in Zed with Grok, replicating Claude Code's contextual intelligence, command recognition, and session tracking capabilities.

**Key Challenges**: Zed lacks native slash command support and direct Git integration like Claude Code. Grok operates conversationally rather than via IDE hooks. MCP server provides the bridge.

## Current Claude Code Workflow Analysis

### Core Components to Replicate

1. **Slash Commands** (`@.claude/commands/`): Natural language instructions in markdown files (e.g., `/wrap.md`, `/restart.md`) executed as programmable commands.
2. **Session Hooks**: Automatic capture via SessionEnd/Stop/PreCompact events with structured markdown generation.
3. **Context Injection**: Dynamic `agentctx/` system for vault-resident context, injected via PreCompact hook.
4. **MCP Server Integration**: 8 tools (`vv_*` prefixed) for introspection and analytics.
5. **Agentic Behavior**: AI follows pair programming paradigm (expert implementation, human architectural guidance).

### Workflow Stages
- **Planning**: Plan mode defaults, detailed specs upfront.
- **Implementation**: Tool use, verification before completion.
- **Session Management**: `/wrap` to archive, `/restart` to resume context.

## Zed + Grok Migration Architecture

### Zed Editor Integration

**MCP Server Connection**:
- Zed supports MCP via configuration file (`.zed/settings.json`).
- Install vibe-vault MCP server as described in `vv mcp install --zed`.
- This enables Zed's agent panel to call `vv_capture_session` for push-based capture.

**Session Capture**:
- Replace Claude Code hooks with Zed extensions/scripts.
- Use Zed's task system or custom extensions to trigger capture on save/commit.
- Grok can initiate capture via MCP tools in conversation.

**Command Emulation**:
- No native slash commands in Zed; emulate via:
  - Zed keybindings to insert command templates.
  - Grok recognizing explicit triggers (e.g., "Execute /wrap").

### Grok AI Integration

**MCP Tool Access**:
- All 8 vibe-vault MCP tools available for contextual intelligence.
- Use tools like `vv_get_project_context` for "restart", `vv_capture_session` for "wrap".

**Conversational Commands**:
- Define trigger phrases for command-like behavior:
  - `/wrap` → Grok reads `agentctx/commands/wrap.md` and executes steps.
  - `/restart` → Grok calls `vv_get_project_context` + reads `agentctx/resume.md`.

**Pair Programming Adaptation**:
- Grok follows Claude Code workflow rules: plan first, verify before done, use subagents.
- Human retains architectural control; Grok handles implementation details.

## Detailed Implementation Plan

### Phase 1: MCP Server Setup in Zed (Week 1)

#### Steps:
1. **Install Zed Extension**:
   - Run `vv mcp install --zed` to configure MCP in Zed settings.
   - Verify connection via Zed agent panel (test `vv_list_projects`).

2. **Test MCP Tools**:
   - Execute each of the 8 tools from Zed agent panel.
   - Confirm data flow from Obsidian vault to Zed interface.

3. **Session Push Capture**:
   - Enable `vv_capture_session` tool in Zed.
   - Test manual capture of current session.

#### Milestones:
- MCP server responding in Zed.
- Basic session capture working.

#### Risks:
- Zed MCP support may have compatibility issues; test with latest Zed version.

### Phase 2: Command Emulation System (Weeks 2-3)

#### Steps:
1. **Create Zed Keybindings**:
   - Add to `.zed/keymap.json`:
     ```json
     {
       "context": "editor",
       "bindings": {
         "cmd-shift-w": "zed:insert_text",
         "args": { "text": "/wrap" }
       }
     }
     ```
   - Similar bindings for `/restart`, `/cancel-plan`.

2. **Grok Command Recognition**:
   - Define prompts: "When user types '/wrap', read `agentctx/commands/wrap.md` and follow instructions."
   - Implement via custom Grok instructions or conversation patterns.

3. **Command Execution Workflow**:
   - Grok parses markdown commands, breaks into steps.
   - Uses tools (MCP + filesystem) to execute:
     - Read files, update iterations.md, trigger captures.

4. **Template Integration**:
   - Create Zed snippets for command starters.
   - Link to `agentctx/commands/` for documentation.

#### Milestones:
- Zed keybindings trigger command insertion.
- Grok recognizes and executes 3 core commands (`/wrap`, `/restart`, `/cancel-plan`).

#### Risks:
- Grok may need explicit triggering; test conversation flow.

### Phase 3: Session Management and Hooks (Weeks 4-5)

#### Steps:
1. **Session End Triggers**:
   - Create Zed extension for auto-capture on file save/close.
   - Use Zed's `tasks.json` to run `vv hook` equivalents.

2. **Context Injection**:
   - Implement PreCompact-like behavior via Zed scripts.
   - Grok calls `vv inject` via MCP when context needed.

3. **Workflow State Persistence**:
   - Use `agentctx/resume.md` as single source of truth.
   - Grok updates state after each action.

4. **Error Handling**:
   - Graceful fallbacks if MCP unavailable.
   - Logging via Zed console or file.

#### Milestones:
- Automatic session capture on Zed events.
- Context injection working without manual triggers.

#### Risks:
- Zed's extension API may limit hook depth; monitor performance.

### Phase 4: Pair Programming Enhancement (Weeks 6-7)

#### Steps:
1. **Grok Workflow Rules Enforcement**:
   - Embed Claude Code workflow rules into Grok prompts.
   - Plan mode default: Grok asks for confirmation before major changes.

2. **Subagent Utilization**:
   - Grok uses MCP tools for parallel research.
   - Offloads tasks (e.g., documentation reading) to "subagents" via tool calls.

3. **Verification Protocols**:
   - Grok runs tests/builds before marking tasks complete.
   - Asks: "Would a staff engineer approve this?"

4. **Human-AI Balance**:
   - Grok defers architectural decisions to user.
   - User guides via high-level direction.

#### Milestones:
- Grok demonstrates autonomous yet guided implementation.
- Successful multi-step task completion.

#### Risks:
- Over-reliance on Grok may reduce user involvement; maintain balance.

### Phase 5: Testing and Optimization (Weeks 8-9)

#### Steps:
1. **End-to-End Testing**:
   - Simulate full Claude Code workflow in Zed+Grok.
   - Test with real development tasks.

2. **Performance Tuning**:
   - Monitor MCP response times; optimize if slow.
   - Reduce context window bloat via selective reading.

3. **User Feedback Integration**:
   - Iterate on command recognition accuracy.
   - Document limitations (e.g., no native hooks).

4. **Documentation Updates**:
   - Update `ARCHITECTURE.md`, `DESIGN.md` for Zed+Grok specifics.
   - Create Zed setup guide.

#### Milestones:
- Seamless workflow matching 80% of Claude Code functionality.
- Performance benchmarks meet requirements (<5s response times).

#### Risks:
- Workflow gaps may require Zed feature requests.

## Technology Stack and Dependencies

- **Zed Version**: Latest stable with MCP support.
- **Grok Integration**: Via MCP tools + conversational prompts.
- **vibe-vault**: MCP server (already implemented).
- **File System**: Symlinks for compatibility (`agentctx/` to project root).
- **Tools**: MCP client libraries for Zed extensions.

## Success Metrics

1. **Functionality Coverage**: 90% of Claude Code features emulated.
2. **Performance**: Session capture <10s, command execution <5s.
3. **Usability**: User reports "feels like Claude Code" in Zed.
4. **Reliability**: Zero data loss, consistent context injection.

## Risks and Mitigations

- **Zed API Limitations**: May require custom extensions; partner with Zed team if needed.
- **Grok Tool Boundaries**: Can't directly modify files; use MCP/file tools carefully.
- **Context Window**: Large agentctx/ files may cause bloat; implement selective reading.
- **Migration Learning Curve**: Provide training docs and gradual rollout.

## Timeline and Resources

- **Timeline**: 9 weeks total, 2 engineers (1 Zed expert, 1 AI integration).
- **Resources**: Zed extension development kit, MCP specification docs, vibe-vault source.
- **Budget**: Primarily development time; potential Zed license if needed.

## Conclusion

This migration plan transforms vibe-vault from Claude Code-specific to a flexible, editor-agnostic "Context as Code" system. By leveraging Zed's MCP capabilities and Grok's agentic tooling, we maintain the core benefits while expanding compatibility. The phased approach ensures incremental progress with continuous validation against Claude Code workflows.

**Next Steps**:
- Kick off Phase 1 MCP setup.
- Assign Zed integration specialist.
- Schedule weekly reviews to track progress.
```
```END