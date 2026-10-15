---
name: plan_phases_sequential
description: New plan phase takes the first free number after the last used one, and every phase sits in numeric order in Main-POC-Plan.md
metadata:
  type: feedback
---

A new phase in `.claude/plans/Main-POC-Plan.md` takes the **first free
number** after the existing ones (check the live plan, ARCHIVE, Candidates,
memory and docs for prior use), not an arbitrary jump. Every `### Phase N`
section sits in **numeric order** in the file — never appended at the end.

**Why:** User corrected 2026-10-14: an OTel phase numbered 70 and appended
after Phase 64 was renumbered to 54 and moved after 53; Phases 44/45 (which
had drifted below 64) were moved back between 43 and 46 the same day.

**How to apply:** Before adding a phase, grep the plan files for the
highest used numbers, pick the first gap after the latest completed phase,
and insert the section where its number belongs. Related:
[[project_plan_location]].
