---
name: phase36-tech-lab-operator-rebrand
description: APPROVED 2026-08-19 — refdata frontend app rebrands to "Tech Lab Operator" (36.1) then absorbs admin's Trading Partners section (36.2); design gate passed, mockups required before either sub-phase's implementation starts
metadata:
  type: project
---

Phase 36 in `.claude/plans/Main-POC-Plan.md` (added 2026-08-19, design gate
**APPROVED** the same day) covers two sub-phases:

- **36.1** — rename `demos/01-dictionary/frontend/refdata` from "Dictionary"
  to "Tech Lab Operator." New nav: a top-level "Operations" group containing
  "Reference Data," built with `NavList.vue` (refdata doesn't use it today —
  its current sidebar is the hand-rolled `TypeNavigator.vue`, dynamic per
  dictionary type/category). The existing nav content (type/category
  browsing, Localization, Versioning) moves into a tabbed info panel
  following the already-documented "Panel top tabs" contract in
  `shared/unifi-theme/LAYOUT.md`, copying admin's `RpcPanel.vue` pattern
  (Pulse/Traces/Messages) rather than inventing a new tab style. Frontend-
  visual-only — no backend/business-rule change. Renaming is narrowly
  scoped to the app's own branding (`App.vue`'s `#brand`, `index.html`
  title, three README table/heading lines) — must NOT touch the unrelated
  "dictionary" naming used throughout the Go backend, Postgres, or the
  `Dictionary-POC` obsidian vault/demo path, which all stay as-is.
- **36.2** — migrate the "Trading Partners" nav (Platform group > Shippers/
  Transporters) from `admin` into Tech Lab Operator. Known risk carried
  into this sub-phase's design decisions: `TradingPartnersPanel.vue`
  depends on admin's full `useTenantStore()`, while refdata only has a
  lighter `context` concept — needs a shim or rework before the panel can
  run standalone in Tech Lab Operator.

**Why this number:** the user explicitly asked for "Phase 36." That number
had been reserved (in practice, not by any rule) for the NATS server-hop
tracing phase's history — see [[phase43-nats-hop-tracing-renumbered]] — but
that phase is now live at Phase 43 with DEFERRED status. Per explicit user
instruction, every remaining stale "Phase 36" reference to server-hop
tracing was swept to cite 43 first (2026-08-19 collision-cleanup renumbering
log in `Main-POC-Plan.md`), and only then was 36 reassigned to this phase.

**Approval terms (2026-08-19):** the user approved both sub-phases' design
decisions as drafted, plus two additions: (1) mockups of the final frontend
outcome must be produced and explicitly approved by the user before any
implementation code is written, for 36.1 and 36.2 independently; (2) a new
architecture doc, `obsidian/V3-Platform/Architecture/Dictionary-POC/
ARCHITECTURE-PLATFORM.md`, was created as the entry point for Tech Lab
Operator's design, cross-referencing `ARCHITECTURE-DICTIONARY.md` (Reference
Data is a subset of Platform's broader operator-facing surface). CLAUDE.md's
"Architecture Docs" index now lists it.

**How to apply:** "Phase 36" from 2026-08-19 onward means this rebrand/
migration phase, not server-hop tracing. Design is approved, but neither
sub-phase's implementation checklist may start until its own mockup step is
done and explicitly approved — treat the mockup item as a hard gate, not a
formality.

**36.1 mockup iteration (2026-08-19):** first pass was rejected for showing
read-only layout only; revised against the live app at `localhost:7102` to
carry forward every existing create/edit affordance (New enum, Add value,
Edit, Details/Translations/Usage, Contexts/Corpus Versions/Diff) — see
[[mockup-fidelity-functional-capability]]. While reviewing, spotted that
`ARCHITECTURE-DICTIONARY.md`'s "Known gaps" note claiming the versioning UI
"is not yet built into `frontend/refdata`" is stale — that UI is live.
User explicitly deferred fixing this: **do not touch it now; revisit once
Phase 36.2 is complete.**

**36.1 mockup APPROVED (2026-08-19):** user approved the revised mockup as-is.
36.1 implementation may now proceed against it. 36.2 scoping starts next,
independently gated on its own mockup per the Approval terms above.

**36.2 `useTenantStore()` risk RESOLVED (2026-08-19):** investigation showed
`TradingPartnersPanel.vue`'s actual dependency on admin's tenant store is
trivial (two reads, one a documented no-op); the real gap is that
`trading-partner-service` derives tenant identity from NATS-account
connection identity (admin's per-tenant reconnect model), while Tech Lab
Operator has one cross-tenant PLATFORM connection with no tenant identity
at all. User's target end-state: both refdata-service and
trading-partner-service are PLATFORM-tier, so Tech Lab Operator should keep
its single platform connection and treat "tenant" as an explicit
operator-selected request parameter — mirroring refdata-service's existing
Phase 32 `MountPlatformAPI` pattern (see
[[phase32_refdata_platform_credential]]) — not admin's per-tenant-reconnect
model. Giving `trading-partner-service` that platform-mounted path (plus an
authorization check, since a platform credential acting on a tenant by
parameter is a wider trust surface than today's connection-scoped model) is
real backend work the user explicitly deferred out of 36.2 — they want to
explore the general "operator selects a tenant" UX further once 36.2 ships.
**36.2 stopgap:** migrate the panel using admin's existing per-tenant
connection pattern unchanged, just relocated into `refdata` — full detail
in `Main-POC-Plan.md`'s 36.2 design decisions. User also flagged the
current live Trading Partners UI is at `http://localhost:7100/` (admin
app) — use it as the fidelity source for the 36.2 mockup, per
[[mockup-fidelity-functional-capability]].

**36.2 mockup APPROVED (2026-08-19):** user approved the Trading Partners
mockup (Operations > Trading Partners, Shippers/Transporters tabs,
expandable rows carrying forward Compliance Documents/Fleet
Assets/Audit Trail/Activate-Suspend). Both 36.1 and 36.2 implementation
checklists are now unblocked on their mockup-gate items; remaining
checklist items in each (rebrand file changes, LAYOUT.md updates,
component migration, in-browser verification) are still open.
