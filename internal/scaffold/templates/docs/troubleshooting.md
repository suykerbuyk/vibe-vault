# Troubleshooting

## Hook not firing

- Verify `vv` is in your PATH: `which vv`
- Check `~/.claude/settings.json` has the SessionEnd hook configured
- Hooks load at session start — changes require a new session

## No session note created

- Check stderr output: `vv` logs to stderr
- Session may be trivial (< 2 messages) — skipped by design
- Session may already be in the index — check `.vibe-vault/session-index.json`
- Context clears (`reason=clear`) are skipped

## Wrong project detected

- `vv` uses the `cwd` from hook input (the directory where Claude Code was invoked)
- Project name is the last path component of `cwd`
- If `cwd` is missing, falls back to transcript metadata

## Wrong domain

- Check `~/.config/vibe-vault/config.toml` domain paths
- Paths must match exactly (after `~` expansion)
- Default domain is `personal` when no match

## Title is unhelpful

- Phase 1 uses heuristic title from first meaningful user message
- Skips: resume instructions, slash commands, confirmations, farewells
- If all messages are filtered, falls back to first message
- Phase 2 will add LLM-generated titles

## Config not loading

- Config path: `~/.config/vibe-vault/config.toml`
- Also checks `$XDG_CONFIG_HOME/vibe-vault/config.toml`
- TOML syntax errors logged to stderr
- Falls back to defaults if no config found

## Manual processing

Process a transcript directly:
```
vv process ~/.claude/projects/{encoded-path}/{session-id}.jsonl
```
