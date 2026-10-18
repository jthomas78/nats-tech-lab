---
name: v3_adrs_never_cite_the_lab
description: V3-scope ADRs must not reference the lab (no lab ADR links, phases, demos, lab docs or lab code); lab findings shape the content but are written as general reasoning
metadata:
  type: feedback
---

A `v3` ADR (`obsidian/V3-Platform/Architecture/ADR/v3/`) never names the lab. No link to a `lab/` ADR, no `applied_by`/`related` pointing at a lab ADR, no Dictionary-POC docs, no plan phases, no demo names, no lab package names (`shared/otelnats`), no "the lab found…".

Lab findings still decide the content. Restate each one as a general fact or failure mode (e.g. "a bridge that reads only the platform subject loses tenant spans"), not as a lab result.

The link runs one way: a `lab` ADR may still say `applies:` and `**Governed by:**` to the V3 ADR.

**Why:** The lab is the user's personal project for decision making and testing viability. The rest of the team cannot access it, so a lab reference in a V3 ADR is a dead end for them (user, 2026-10-18, on ADR-058).

**How to apply:** Writing or reviewing any V3 ADR or other team-facing V3 document. Older V3 ADRs (053, 054, 056) predate this rule and may still cite the lab.
