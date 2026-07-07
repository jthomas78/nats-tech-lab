---
name: project-plan-location
description: Where plan markdown files are stored in this repo
metadata:
  type: project
---

Plan `.md` files live in `.claude/plans/` inside the repo root — not at the project root.

**Why:** User moved them there explicitly (2026-07-07).

**How to apply:** When referencing or creating plan files, always use the path `.claude/plans/<name>.md`.
