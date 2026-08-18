---
name: phase36-nats-hop-tracing-renumbered
description: NATS 2.11 Server-Hop Tracing phase renumbered 29→41→36 (2026-08-18); still PROPOSED/not started; now sits right after Phase 35 in Main-POC-Plan.md
metadata:
  type: project
---

The "NATS 2.11 Server-Hop Tracing" phase (`Nats-Trace-Dest` hop tree, "Trace
this subject" control) is now **Phase 36** in `.claude/plans/Main-POC-Plan.md`
— not Phase 41 and not Phase 29. It has been renumbered twice (29→41 on
2026-08-17, 41→36 on 2026-08-18) and still hasn't been implemented; status
stays PROPOSED throughout both moves. It now sits physically right after
Phase 35, ahead of Phase 40, so the live plan reads ascending top-to-bottom
again.

**Why:** the 2026-08-17 renumbering grouped it with Phase 40 (Credential
Lifecycle Hardening) in the 40s purely because both were orphaned mid-block
at the same time — they have no dependency on each other. Once Phase 35
completed the next day, 36–39 opened up as the next available slot
immediately after the last completed phase, so the pairing that justified
sitting in the 40s no longer applied.

**How to apply:** if a past conversation, memory file, or doc excerpt says
"Phase 41" or "Phase 29" about NATS server-hop tracing, treat it as stale —
the design content is unchanged, only the number and position moved. The
full renumbering lineage (with dates) is preserved in the phase's own header
blockquotes and in a `## Renumbering (2026-08-18 — Phase 41 → Phase 36...)`
log section near the end of `Main-POC-Plan.md`.
`ARCHITECTURE-COMMUNICATIONS.md` §6 and `ARCHITECTURE-ADMIN.md` §4.5 were
updated in the same pass to cite Phase 36 too.
