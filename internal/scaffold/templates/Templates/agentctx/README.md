# Agentctx Templates

These templates are used by `vv context init` to scaffold vault-resident
AI context files for new projects.

## Variables

- `{{PROJECT}}` — project name (auto-detected or --project flag)
- `{{DATE}}` — current date (YYYY-MM-DD)

## How it works

When you run `vv context init`, each file here is checked before
falling back to the embedded defaults. To customize a template, edit
the file here. Your changes will be used for all future project inits.

## Shared commands

Files in commands/ are propagated to all projects by `vv context sync`.
Project-specific commands always take precedence (never overwritten).
