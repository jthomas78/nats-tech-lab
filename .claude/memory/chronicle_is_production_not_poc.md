---
name: chronicle-is-production-not-poc
description: The chronicle repo (/Users/jeremy/dev/github/linebooker/chronicle) is production code, not a POC; branch as feature/ or chore/ off develop
metadata:
  type: project
---

`/Users/jeremy/dev/github/linebooker/chronicle` is a production repository, not a POC or lab. Never apply this lab's `poc/` branch names or POC wording to it.

Its branches use `feature/<kebab-topic>` or `chore/<kebab-topic>`, cut from `develop`.

**Why:** The user corrected this on 2026-10-19 after a branch-name question about chronicle's OTel tracing update.

**How to apply:** When work or naming targets chronicle, use its own conventions (read its git branches), and treat it as production-grade.
