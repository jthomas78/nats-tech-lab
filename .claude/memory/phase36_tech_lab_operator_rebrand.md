---
name: phase36-tech-lab-operator-rebrand
description: refdata frontend app rebrands to "Tech Lab Operator" — 36.1 (nav/tab restructure) and 36.2 (absorbs admin's Trading Partners section) both IMPLEMENTED 2026-08-19
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
  into this sub-phase's design decisions: `OrganizationsPanel.vue`
  depends on admin's full `useTenantStore()`, while refdata only has a
  lighter `context` concept — needs a shim or rework before the panel can
  run standalone in Tech Lab Operator.

**Why this number:** the user explicitly asked for "Phase 36." That number
had been reserved (in practice, not by any rule) for the NATS server-hop
tracing phase's history — see [[phase63-nats-hop-tracing-renumbered]] — but
that phase is now live at Phase 63 (it was Phase 43 at the time of this
rebrand; renumbered again 2026-08-20b) with DEFERRED status. Per explicit user
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
`OrganizationsPanel.vue`'s actual dependency on admin's tenant store is
trivial (two reads, one a documented no-op); the real gap is that
`organizations-service` derives tenant identity from NATS-account
connection identity (admin's per-tenant reconnect model), while Tech Lab
Operator has one cross-tenant PLATFORM connection with no tenant identity
at all. User's target end-state: both refdata-service and
organizations-service are PLATFORM-tier, so Tech Lab Operator should keep
its single platform connection and treat "tenant" as an explicit
operator-selected request parameter — mirroring refdata-service's existing
Phase 32 `MountPlatformAPI` pattern (see
[[phase32_refdata_platform_credential]]) — not admin's per-tenant-reconnect
model. Giving `organizations-service` that platform-mounted path (plus an
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

**36.1 IMPLEMENTED (2026-08-19):** all checklist items done and
live-verified. `refdata/src/App.vue` now uses `NavList.vue` (`Operations` >
`Reference Data`, one entry — 36.2 adds `Trading Partners` alongside it)
instead of `TypeNavigator.vue`, which is deleted. Its content became a new
`ReferenceDataPanel.vue`: a `panel-tabs` strip (`Reference Data` / `Domain`
/ `Localization` / `Versioning`) matching `RpcPanel.vue`'s contract exactly,
including the same-element `:deep()` gotcha fix (`.rd-tabs.p-tabs`, not
`.rd-tabs :deep(.p-tabs)`) and the `.main-inner > .fill-height` global rule
having to be re-created locally (`.reference-data-row > .fill-height`,
`.rd-domain-body > .fill-height`) since `ItemGrid`/`CategoryTypeList` are no
longer direct children of `.main-inner`. Also first real usage in this repo
of the already-documented-but-unused `.page-head`/`.eyebrow-static`
convention from `app-shell-reference.html`/LAYOUT.md's "Main content"
section — copied verbatim rather than reinvented. One real behavioral gap
found and fixed during live verification: `store.selectedType` is shared
between the Reference Data and Domain tabs (`selectCategoryType` sets it
too), so switching top-level tabs needed an explicit re-sync watcher or the
tab you land on shows whatever type was last touched in the *other* tab —
see [[mockup_fidelity_functional_capability]] for why this class of gap
matters. Domain tab's Enums/Strings switch reuses `CategoryTypeList.vue`
unchanged behind a bare `Tabs`/`TabList` (no `TabPanels` — confirmed via
`Tabs.vue` source that PrimeVue doesn't require them; it's a pure nav
strip, not a second panel-owning tab set), dynamically listing whichever
`DOMAIN_CATEGORIES` actually have types rather than hardcoding Enums/
Strings, so a future `config`-category type shows up automatically.

**36.2 IMPLEMENTED (2026-08-19):** `OrganizationsPanel.vue` +
`IconShippers.vue`/`IconTransporters.vue` moved from `admin` into `refdata`
as a `Trading Partners` eyebrow alongside `Reference Data`. A real design gap
surfaced mid-implementation and was resolved with the user before writing
code (not silently assumed): the approved stopgap said "same per-tenant
connection pattern `admin` uses today," but admin's actual mechanism —
`GET/POST /api/tenant[/switch]` — reconnects a *shared backend* NATS
connection on shipping-service that `admin`'s own dictionary store also
depends on. Reusing it from `refdata` too would mean a tenant switch in Tech
Lab Operator silently reconnects a connection `admin` relies on — a new
cross-app coupling. User chose (via AskUserQuestion) the alternative:
`refdata` gained its own second, tenant-scoped browser connection
(`nats/useTenantConnection.js`, alongside the pre-existing PLATFORM-only
`useRefdataAdminConnection.js`) that reconnects only the *browser's own*
NATS credential — no backend endpoint touched at all, so no shared-state
coupling with `admin`. A new `stores/tenant.js` backs it, populated from
accounts-service's `GET /api/auth/tenants` (already proxied for the PLATFORM
connection's own credential fetch — no new proxy rule needed). This first
attempt used `GET /api/accounts` instead and was caught as wrong during live
verification: that endpoint includes the reserved `platform`/`sys`
infrastructure accounts (BR-AC06), which surfaced as bogus selectable
"tenants" with no real Shippers/Transporters — `/api/auth/tenants`
(`accounts.Store.ListActiveTenantNames`) already excludes them and is the
right endpoint. Once a tenant is selected, its own context list comes from
refdata-service's existing `context.list.v1` subject with an explicit
`{tenant}` filter in the body (BR-D35) — this already existed for exactly
this purpose and needed no backend change. Live-verified: registered a real
shipper against `organizations-service` under the `globex` tenant, expanded
a transporter row (Compliance Documents/Fleet Assets/Audit Trail all
populated with real data), and switched tenants (`acme` ↔ `globex`)
confirming each tenant's own fleet-context list loads independently. See
[[mockup_fidelity_functional_capability]] for why this class of
during-implementation gap is worth catching rather than assuming.
