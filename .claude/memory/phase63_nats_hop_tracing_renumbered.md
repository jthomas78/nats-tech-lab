---
name: phase63-nats-hop-tracing-renumbered
description: NATS 2.11 Server-Hop Tracing phase renumbered 29→41→36→43→63 (last move 2026-08-20, when 40–49 shifted to 60–69); DEFERRED, design approved, implementation on hold pending further research. Phase 36 has since been freed and reused for an unrelated phase.
metadata:
  type: project
---

The "NATS 2.11 Server-Hop Tracing" phase (`Nats-Trace-Dest` hop tree, "Trace
this subject" control) is now **Phase 63**, held in
`.claude/plans/Main-POC-Plan-Candidates.md` (moved out of the live
`Main-POC-Plan.md` on 2026-08-21, since it is deferred and not in flight)
— not Phase 43, not Phase 41, not Phase 29, and no longer Phase 36. It has
been renumbered four times (29→41 on 2026-08-17, 41→36 on 2026-08-18, 36→43
later on 2026-08-18 when the design gate deferred implementation pending
further research, and 43→63 on 2026-08-20 when the whole 40–49 block was
shifted to 60–69 to free the 40s). Status is **DEFERRED 2026-08-18 — design approved, implementation
on hold**, not PROPOSED. In the candidates file it sits under
"Deferred / on-hold (original numbering retained)", ahead of Phase 43 and the
Phase 100+ block; the live plan carries a one-line entry for it under
"Candidate, deferred, and on-hold phases".

**Why:** the design-gate spike fully validated a corrected design (see the
phase's own "Spike findings"/"Design decisions" and BR-042), but the user
chose to defer implementation rather than start immediately, so the phase
moved to the 100+-adjacent block per this plan's own "never-implemented
phases move out of the active low-numbered block" convention.

On 2026-08-19, on explicit user request, **Phase 36 was freed and reused for
a second, unrelated phase** ([[phase36-tech-lab-operator-rebrand]] — the
Tech Lab Operator rebrand + Trading Partners migration). Before that reuse,
every remaining live "Phase 36" reference to server-hop tracing (BR-042's
heading, `ARCHITECTURE-COMMUNICATIONS.md` §6, `ARCHITECTURE-ADMIN.md` §4.5,
and the `phase36-trace-the-subject-options.png` image, renamed to
`phase43-...`) was swept to cite 43 instead, so "Phase 36" is now
unambiguous going forward. The `phase43-*.png` filenames were deliberately
**not** renamed again in the 2026-08-20b 43→63 move — asset renames there
would be purely cosmetic.

**How to apply:** if a past conversation, memory file, or doc excerpt says
"Phase 43," "Phase 41," "Phase 29," or "Phase 36" about server-hop tracing,
treat it as
stale — the design content is unchanged, only the number, position, and
status moved. If something says "Phase 36" about anything else (rebrand,
nav, Trading Partners), that's the *current* Phase 36, not this phase. The
full renumbering lineage is in this phase's own header blockquote and in the
"Renumbering" log sections (2026-08-18, 2026-08-18b, 2026-08-19, 2026-08-20b)
under "Renumbering history" in `.claude/plans/Main-POC-Plan-ARCHIVE.md`.
