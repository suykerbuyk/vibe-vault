# {{VAULT_NAME}}

An Obsidian vault that automatically captures AI coding sessions into structured, searchable Markdown notes.

## How It Works

The `vv` binary (from [vibe-vault](https://github.com/suykerbuyk/vibe-vault)) runs as a Claude Code hook. When a session ends, it:

1. Reads the full JSONL transcript
2. Extracts stats (duration, tokens, files changed, message counts)
3. Detects project/domain/branch from the working directory
4. Generates a session note with Obsidian-compatible frontmatter
5. Maintains a session index for cross-session linking

Notes land in `Projects/{project}/sessions/YYYY-MM-DD-NN.md` and are queryable via Dataview.

## Quick Start

1. Install `vv`: `cd ~/code/vibe-vault && make install`
2. Configure: `~/.config/vibe-vault/config.toml`
3. Add hook to `~/.claude/settings.json`:
   ```json
   {"hooks": {"SessionEnd": [{"matcher": "", "hooks": [{"type": "command", "command": "vv hook"}]}]}}
   ```
4. Start a Claude Code session. When it ends, a note appears in `Projects/`.

## Vault Structure

```
Projects/{project}/sessions/  Auto-generated session notes
Projects/{project}/knowledge.md  Per-project knowledge (manual)
Dashboards/               Dataview-powered views
Templates/                Templater templates
```

## Security

This is a public template. Personal session content is gitignored. PII scanning runs pre-push and in CI.

## License

MIT
