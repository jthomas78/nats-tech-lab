---
name: br-classification-heuristic
description: When it's unclear whether a new check is a formal BR-numbered domain rule or application-layer input validation, look for precedent in commands/*.go before asking
metadata:
  type: feedback
---

CLAUDE.md's AI Agent Workflow already requires confirming business rules before coding a feature — this memory is the narrower technique for *classifying* an ambiguous one, which isn't spelled out there.

This codebase already draws the line in code, not just docs: `commands/container.go`'s empty-field checks (containerID/cargo/originPort/destPort required) are explicitly commented `// Input validation (application layer, like BR-007 — not a domain rule)`, while BR-008..BR-015 live in `domain/container.go` with dedicated errors, Ginkgo `Context` blocks, and `BUSINESS_RULES.md` entries.

**How to apply:** Before asking the user whether a new check (e.g. an ID format constraint) should be a formal BR or plain input validation, check for a same-shaped precedent already in the codebase first — it often settles the classification without needing to ask. When precedent doesn't settle it (e.g. BR-016 container-ID-format: arguably either tier, and the answer changes where the code lives — domain vs application layer), ask via `AskUserQuestion` with the precedent-matching option listed first/recommended, rather than guessing.
