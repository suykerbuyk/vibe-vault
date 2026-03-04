# LinkedIn Post — vibe-vault Announcement

---

**"Vibe coding" failed me. So I went back to what actually works.**

Last year I pair-programmed with AI to build a 95,000-line industrial CAN bus command-and-control system in Rust — DBC spec files compiled to native type-safe data structures, 700+ tests, safety-critical hardware control. Not a web app. Not a prototype. Production systems software.

And "vibe coding" was a complete disaster for it.

What worked was treating the AI exactly like I'd treat a brilliant junior engineer: the same paired programming discipline I'd evangelized as an Agile practitioner for over a decade. Clear architectural guardrails. Investigation before implementation. Never trust a fix you can't verify. The AI writes excellent code — when you lead it like a tech lead, not a wish-granting machine.

But there was a problem. Every session, the AI forgot everything. Dozens of iterations of context — architectural decisions, trade-offs considered and rejected, patterns discovered, bugs root-caused — evaporated into opaque JSONL transcript files. I was manually maintaining resume documents and migrating markdown context between machines, branches, and worktrees. It worked, but it didn't scale.

So I built vibe-vault.

**vibe-vault** (`vv`) is an open-source Go binary that runs as a Claude Code hook. When an AI coding session ends, it automatically:

→ Parses the JSONL transcript into structured narratives
→ Extracts prose dialogue, git commits, tool activity, and key decisions
→ Detects friction patterns — corrections, error cycles, redo requests
→ Scores and cross-links related sessions across your entire project history
→ Writes a structured Obsidian markdown note, queryable with Dataview
→ Optionally enriches notes with any OpenAI-compatible LLM

Two dependencies. Zero runtime services. Notes are always written, even when enrichment fails.

But capture was just the beginning. After watching Dru Knox's talk "Stop Prompting, Start Engineering," I realized vibe-vault was already implementing most of what he calls "Context as Code" — the idea that AI context deserves the same engineering discipline we give production code: version control, testing, observability, and reuse.

So I kept building:

**Project Evolution Tracking** — Session notes are the chronological record git commits try to be but rarely are. Not just what changed, but *why*. Decisions in full context, with the reasoning preserved.

**Portable AI Memory** — `vv context init` scaffolds a self-contained context package per project in your Obsidian vault: behavioral rules, project state, iteration history, slash commands, task management. Context that survives machine migrations, is searchable across your portfolio, and gives any new session instant recall.

**AI Behavioral Observability** — Every transcript logs tool usage, token consumption, and correction patterns. Aggregated across sessions, `vv friction` and `vv trends` reveal friction points, prompt gaps, and workflow bottlenecks. This is the observability layer Knox describes — built on data the tool already captures.

The result: 428 tests, 44 iterations, 10 completed development phases, all built through the same AI pair-programming workflow the tool is designed to support.

What I've learned through this journey:

1. AI pair programming with senior engineering discipline dramatically outperforms "vibe coding" for anything non-trivial.
2. Context is the bottleneck. Not the model. Not the prompt. The *context*.
3. Sessions are the new unit of work in AI-assisted development, and they deserve real observability.
4. The developers who will thrive aren't the ones who prompt the best — they're the ones who engineer the best context.

vibe-vault is open source under dual Apache 2.0 / MIT license.

GitHub: https://github.com/suykerbuyk/vibe-vault

I'm actively looking for roles where this kind of agentic systems thinking meets real engineering problems. If your team is building with AI — not just talking about it — I'd love to connect.

#AgenticProgramming #ContextEngineering #AIAssistedDevelopment #Golang #OpenSource #PairProgramming #DeveloperTools #ObsidianMD
