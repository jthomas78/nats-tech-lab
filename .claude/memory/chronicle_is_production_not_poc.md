---
name: chronicle-is-production-not-poc
description: The chronicle repo (/Users/jeremy/dev/github/linebooker/chronicle) is production code, not a POC; branch as feature/, chore/ or docs/ off develop
metadata:
  type: project
---

`/Users/jeremy/dev/github/linebooker/chronicle` is a production repository, not a POC or lab. Never apply this lab's `poc/` branch names or POC wording to it.

Its branches use `feature/<kebab-topic>` or `chore/<kebab-topic>`, cut from `develop`. Architecture-doc branches use `docs/<topic>` (for example `docs/architecture-refdata`) and may be cut from the `docs/L4-model-otel-tracing` line, because `documentation/architecture/L4_model/` exists only there until it merges.

**Why:** The user corrected this on 2026-10-19 after a branch-name question about chronicle's OTel tracing update.

**How to apply:** When work or naming targets chronicle, use its own conventions (read its git branches), and treat it as production-grade.
