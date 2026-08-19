# AGENTS.md

## Canonical repository guidance

Before doing any work in this repository, read and follow [`CLAUDE.md`](CLAUDE.md).
It is the single source of truth for repository structure, commands,
architecture, quality rules, agent workflow, plans, and session memory.

Follow the session-memory policy in `CLAUDE.md`: start with
`.claude/memory/MEMORY.md`, load individual memory files only when their index
hooks are relevant, and save shared project memories under `.claude/memory/`.

Do not create or maintain parallel `.Codex/` or `.codex/` copies of repository
guidance, plans, skills, or memory. Codex-specific instructions belong here
only when they cannot be expressed as shared guidance in `CLAUDE.md`.
