---
name: chronicle-refdata-architecture-docs
description: Chronicle has a reference-data concern (ADR-REF-DATA, L3 overview, L4 service design) derived from lab demo 01; branch docs/architecture-refdata
metadata:
  type: project
---

On 2026-10-20 Chronicle got `documentation/architecture/L4_model/L3_L4/reference-data/`:
`ADR/ADR-REF-DATA.md` (13 rules), `L3-reference-data-overview-v0.1.html` and
`L4-reference-data-service-design-v0.1.html` (changes R1–R9, decisions D1–D8, checks
REF-F01–F10 / REF-Q01–Q05, Appendix A = lab findings E1–E13). The L0 atlas lists them.
Branch `docs/architecture-refdata`, not committed at the time.

Key adaptation from the lab: one `REFDATA` KV bucket per account (CONTROL + each
TENANT_<alias>), written by a `refdata` user inside that account — no imports/exports,
which fixes the lab's bucket-per-version stream exhaustion and wildcard tenant leak.

**Why:** Chronicle docs never name the lab; they say "an earlier proof of concept".
**How to apply:** keep R/D/check IDs consistent across ADR, L3 and L4 when editing any one.
See [[chronicle-is-production-not-poc]].
