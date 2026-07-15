---
name: phase-11-10-localization-approved-option-d
description: Phase 11.10 (localize frontend-port) is approved with Option D — all UI copy in refdata, bundled fallback generated at build time — not yet implemented
metadata:
  type: project
---

Phase 11.10 was added to `.claude/plans/Dictionary-Service-Plan.md` (2026-07-15) to extend Phase 11.7's refdata-as-TMS pattern from its two proof-of-concept strings (`filter.all`, `nav.language`) to the full `frontend-port` shipping UI (~90 hardcoded strings across `App.vue`, `FleetPanel.vue`, `ShipsAtPortPanel.vue`, `TerminalPanel.vue`). `frontend` (architecture/demo UI) and `frontend-dict` are out of scope for this phase.

**Status: approved (2026-07-15), Option D chosen. Not yet implemented** (explicitly deferred by the user pending a separate go-ahead).

**Option D — all refdata, generated fallback.** Every UI string is a `ui-copy` refdata item (en+es seed rows in `uiCopySeed`, `seed.go`) — the sole authored source. The bundled vue-i18n fallback (`shared/refdata/uiCopyFallback.en.js`, required by BR-D11 for offline/cold-paint rendering) is no longer hand-written; it's *generated* from the seed at build time (lockfile model: a generator + `prebuild` hook + a CI drift-check that fails if regenerating produces a diff). This avoids double-maintaining `en` (the flaw in plain "everything in refdata") while keeping first-paint correctness and a single source of truth. Optional complement: a `<UiString code="…">` component as a call-site provenance seam.

Options A (plain all-refdata, hand-written bundle), B (all bundled), and C (split by editorial ownership, needing a namespace convention) were considered and rejected in favor of D — see the Phase 11.10 decision table in the plan for the full reasoning if this needs to be revisited. Because D routes every string through refdata with no split, the previously-proposed **BR-D17** (namespace boundary convention, needed only for C) was dropped from the plan.

**Remaining before implementation can start:**
- Key namespace design (organizational only under D, not a routing boundary)
- The generator + `prebuild` hook + CI drift-check tooling (new, not yet built)
- `BR-D16` (proposed): all Port-UI copy resolves through i18n — needs a test and a `BUSINESS_RULES.md` entry when implemented
- String extraction across the 4 files, `es` translations (human-reviewed, not machine output)

**How to apply:** If asked to continue Phase 11.10, the option decision is settled (D) — don't re-litigate it. Do not start implementation until the user explicitly asks to; as of 2026-07-15 they've approved the plan but separately said not to implement yet.
