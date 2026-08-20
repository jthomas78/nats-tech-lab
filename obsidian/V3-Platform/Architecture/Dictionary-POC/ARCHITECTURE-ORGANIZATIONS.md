# Architecture — Organizations: Transporter Registration & Vetting (Phase 38, IN PROGRESS — 38a/38b built)

Design reference for extending `trading-partner-service` (renamed to
`organizations-service` at the end of this phase, not the start — see
"Naming & Sequencing" below) with a real Transporter registration and
vetting workflow, modeled on Linebooker V2's `Transporters` admin screen.
Customers/Shippers are unaffected — see "Scope" below. For the
already-shipped Shipper/TradingPartner behavior this phase builds beside
(not on top of), see
[BUSINESS_RULES-TRADING-PARTNER.md](../../../../demos/01-dictionary/BUSINESS_RULES-TRADING-PARTNER.md)
(BR-TP01–BR-TP17, Phase 26).

**This doc started as a design conversation and is now partly an
implementation record.** The design was approved 2026-08-20 and sub-phases
**38a, 38b, 38c-i and 38d-i are built, tested, and green** (BR-TP18–BR-TP39);
the remainder runs 38c-ii → 38d-ii → 38e and none of it is started.
Sections below still describe the
design **as proposed** — where the built code deliberately diverges, see
"Implementation notes (38a/38b as built)" immediately after this, which is
authoritative for those two points. Treat everything concerning
38d-ii (Operating Areas + Tracking Credentials) and
38e (rename) as still proposal, not fact about running code.

**38c-ii shipped 2026-08-20 (BR-TP40–BR-TP45)**, so the "Document storage —
NATS Object Store" section below now describes running code. Two divergences
from its wording:

- **The ingress's auth is a service-minted capability ticket, not JWT
  verification.** ADR-048 finding 5 called for a "dedicated HTTP
  upload/download endpoint (own max-body limit, own auth)", which read as a
  wiring task. It was not: **nothing in this repo verifies a JWT** —
  `accounts-service` only mints NATS credentials, and every other caller
  authenticates by connecting to an account. So BR-TP41 mints a single-use,
  2-minute ticket over the authenticated NATS connection and the HTTP call
  carries only that, keeping the tenancy decision on the account boundary
  instead of building a second authentication system for two byte routes.
- **A document's bytes are write-once.** This section says objects are never
  deleted; the built code goes further and refuses to *overwrite* one, because
  replacing bytes the immutable log references is the same failure the
  service-minted object name exists to prevent. The correction path is
  BR-TP30's supersede-and-replace, which leaves both objects retrievable —
  which is also why `GetDocument` returns superseded rows that
  `ListDocuments` excludes.
- **The stream-budget check this section demands is done** (2026-08-20, live
  stack): ACME 5→6 streams of 10, GLOBEX 3→4, no `/jslimits` raise needed.
  The worry below that refdata's per-context *versioned* KV buckets could
  exceed `MaxStreams: 10` is **misplaced** — those buckets live in PLATFORM
  (limit 20, then at 9), not in the tenant accounts.

Two things 38d-i's build established that this doc's UI sections do not say,
both worth reading before 38d-ii extends the same surface:

- The Transporter UI is a **dedicated component with a drill-in detail view**
  (`TransporterPanel.vue`), not a role-parameterized panel with an expansion
  row. Shippers stay on `TradingPartnersPanel.vue`. 38d-ii's Operating Areas
  and Tracking Credentials are additional **tabs on that drill-in**, not new
  top-level screens.
- **Fleet assets cannot currently be added in the dev stack at all**: no
  `vehicle-type` corpus is seeded, so BR-TP14's live refdata validation
  rejects every code. Pre-existing and not a 38d-i regression, but it means the
  Fleet tab — and anything in 38d-ii that depends on a fleet asset existing —
  is unverifiable end-to-end until that corpus is seeded into the working
  context. The one-off `refdata-service/cmd/seed-vehicle-types` seeder does not
  close this as written: it targets context `linebooker` over the REST surface
  Phase 33 retired.

Implementation detail (final field names, error codes, exact test list)
belongs in `BUSINESS_RULES-TRADING-PARTNER.md` (or a new
`BUSINESS_RULES-ORGANIZATIONS.md` once the rename lands) and the
`.claude/plans/Main-POC-Plan.md` Phase 38 checklist, not here.

---

## Implementation notes (38a/38b as built)

Two decisions taken during 38b's implementation **supersede** the design
prose further down this document. They were forced by gaps found while
turning ADR-047/ADR-049 into specs, and both are additive corrections — the
aggregate-split decision in
[ADR-046](ADR-046-transporter-aggregate-split.md) is unaffected.

### 1. `FleetAvailabilityGate` is aggregate state; `AvailableForAssignment` is computed

The design reads as though `TransporterProfile` gates
`FleetAsset.AvailableForAssignment` directly. It does not, and cannot
without breaking 38a's boundary: `FleetAsset` lives in
`tradingpartner/internal/domain` as plain CRUD (BR-TP12/BR-TP13) with no
availability state at all, and 38a deliberately left it untouched.

As built:

- `TransporterProfile` owns a **`FleetAvailabilityGate` boolean** in its own
  aggregate state. It defaults `false`, is set `true` only when both vetting
  branches succeed (BR-TP21), and returns to `false` via an explicit
  `FleetAvailabilityRevoked` event (BR-TP22/BR-TP28).
- **`FleetAsset.AvailableForAssignment` is a computed read-layer value** — a
  join of the legacy per-asset rows with that gate — never a column
  `TransporterProfile` writes into, and never a new field on the legacy
  `FleetAsset` domain type.

This keeps writes aligned to aggregate ownership, which is what ADR-049's
"save boundaries must align to the aggregate boundary" finding actually
requires; `tradingpartner` keeps owning per-asset identity.

### 2. `HandleGitStatusDrop` performs both halves of the late-GIT reaction

ADR-047 said a late GIT invalidation invokes
`CompensateDeactivateFleetAssets`; ADR-049's resolution said the monitor
workflow calls `TradingPartner.Suspend()`. **Neither said it does both** —
leaving a real gap where a partner could end up suspended while its fleet
still read as available, or a compensation existed with no caller.

As built, `TransporterGitMonitorWorkflow` invokes **one** orchestration
command, `HandleGitStatusDrop(tradingPartnerID)`, which appends
`FleetAvailabilityRevoked` and then calls `TradingPartner.Suspend()`. It is
idempotent, so a repeated Schedule firing on an already-suspended partner is
a no-op rather than a second suspension. See BR-TP28.

### Also worth knowing about the built code

- **Projection is allowlist-driven.** The JetStream projector advances
  projected state only for a closed set of state-transition event types
  (`domain.ProjectsState`); document/GIT branch events are audit-only, and
  any **unrecognised** event type is acked and skipped. This is deliberate
  forward-compatibility: a denylist would let a future event type silently
  overwrite the projection with zero-valued state.
- **BR-TP27's durability spec needs a real Temporal server.** It skips
  silently unless `TEMPORAL_TEST_ADDRESS` is set, so a bare `ginkgo ./...`
  reports green without proving the guarantee. It has been run for real
  against the compose Temporal server (worker killed mid-workflow, restarted,
  workflow completed without re-asking the satisfied signal) — see
  `demos/01-dictionary/README.md` § "Temporal durability test (BR-TP27)".
- **Temporal Schedules, not `CronSchedule`.** The plan's wording says "cron
  workflow"; the implementation uses the current Schedules API, which is what
  the Temporal SDK now recommends. Behaviour is as designed.
- **New infra:** `temporal` (gRPC `7233`) and `temporal-ui` (`8233`)
  containers, backed by their own `temporal-postgres`.

---

## Scope

**In scope:** Transporter registration, document/compliance vetting, GIT
(Goods-in-Transit) insurance verification, fleet, operating areas, tracking
credentials, rate sheets, admin settings — the Linebooker V2 `Transporters`
screen. **Out of scope for this phase:** Customers (V2's sibling screen —
explicitly deferred, "might do thereafter" per the requesting conversation),
Members (blocked on auth/user-registration work that doesn't exist yet), and
any real marketplace/tender consumer of transporter status (still the
deferred item named in the Phase 62 close-out list).

![System architecture of trading-partner-service after Phase 38: the existing TradingPartner aggregate keeps its plain-CRUD lifecycle unchanged aside from a new partner-update command and a widened compliance_documents primary key, while a new event-sourced TransporterProfile sibling package, a new Temporal server orchestrating the vetting saga and a GIT-expiry cron workflow, and a new NATS Object Store bucket for document bytes are added inside the same service container.](images/phase38-organizations-architecture.png)

Editable source: [phase38-organizations-architecture.html](../../../../demos/01-dictionary/diagrams/phase38-organizations-architecture.html)
— hand-authored inline SVG rather than a Draw.io workbook page, so
`./diagrams/export-png.sh` does **not** regenerate it. Re-export with
`node diagrams/export-html-png.mjs diagrams/phase38-organizations-architecture.html \`
`  ../../obsidian/V3-Platform/Architecture/Dictionary-POC/images/phase38-organizations-architecture.png 1024 --clip=".wrap"`
from `demos/01-dictionary/`. The 1024px width is the geometry the page was
reviewed at; changing it changes the layout. The `--clip=".wrap"` is
load-bearing, not optional — dropping it can silently reintroduce a
dead-space band at the bottom of the export.

## Linebooker V2 source fidelity

The data-sections and lifecycle design below are grounded in the actual V2
source (`/Users/jeremy/dev/github/linebooker/linebooker`), not the two
list-view/nav screenshots alone. Verified entity map, for traceability if
V2 needs re-checking later:

| Concept | V2 class | File |
|---|---|---|
| Transporter profile | `TransporterProfileEntity` | `backend/src/main/java/com/linebooker/console/domain/TransporterProfileEntity.java` |
| Shared company identity | `BusinessEntity` | `.../domain/BusinessEntity.java` |
| Compliance document (incl. GIT) | `TransporterDocumentEntity` | `.../domain/TransporterDocumentEntity.java` |
| Uploaded file metadata | `DocumentEntity` | `.../domain/DocumentEntity.java` |
| Fleet asset | `FleetAssetEntity` | `.../domain/FleetAssetEntity.java` |
| Tracking credential (base) | `TrackingCredentialsEntity` | `.../component/tracking/domain/TrackingCredentialsEntity.java` |
| Operating area (geo) | `GeoAreaEntity` / `TransporterOperatingAreaEntity` | `.../component/operatingareas/domain/GeoAreaEntity.java` / `.../domain/TransporterOperatingAreaEntity.java` |
| Rate sheet (customer-owned) | `RateSheetEntity` / `RateSheetVersionEntryEntity` | `.../domain/RateSheetEntity.java` / `.../domain/RateSheetVersionEntryEntity.java` |

Every "V2 real shape" note in the sections below traces back to this
source, read directly (not inferred from the UI screenshots).

### V2 database verification (2026-08-20)

The entity map above was read from **Java source only**. On 2026-08-20 the
V2 stack was started locally and the same concepts were checked against the
**running `linebooker_v2` MySQL instance** (`localhost:3307`) — schema *and*
row counts. This matters because a JPA entity proves a table exists, not
that anything uses it, and the two diverge sharply in one place:

| Concept | Schema present | Rows | Note |
|---|---|---|---|
| `geo_areas` (`GeoAreaEntity`) | yes — `multipolygon` SRID 4326, spatial indexes, `parent_id` hierarchy | **0** | Built, never populated |
| `transporter_operating_areas` | yes — denormalized `level` + `country_code` | **0** | Built, never populated |
| `region_entity` | flat `(id, name, country_id)` | 217 | **The live model** |
| `country_entity` | flat `(id, name)` | 57 | **The live model** |
| `transporter_profile_entity_region` | plain M:N join, no level/geometry | **48,041** | **The live model** |
| `town_entity` | — | 1,373 | Not wired into operating areas |
| `tracking_credentials_entity` | base row | 94 | Secret column empty on all 94 |
| per-provider credential satellites | 20 tables | 15 populated | Plaintext `varchar`, no encryption — confirmed |

**Standing lesson:** where this doc says "V2's real shape," it means the
shape in the *source*. For anything where the POC design is justified as a
simplification *of what V2 runs*, check the row counts too — the Operating
Areas row below was wrong precisely because it didn't.

## Current state (Phase 26, unaffected by this phase)

`trading-partner-service` today has one `TradingPartner` aggregate with a
`Type` (`SHIPPER`|`TRANSPORTER`) discriminator, plain Postgres CRUD, a
3-state lifecycle (`Registered → Active ⇄ Suspended`), and
`ComplianceDocument`/`FleetAsset` child records — see BR-TP01–BR-TP17. It
has no JetStream event sourcing, no KV cache, and stores document
*metadata* only (a free-text `Reference`, no actual file bytes anywhere in
the stack).

## Decision: shared `TradingPartner` identity + a separate, event-sourced `TransporterProfile` aggregate

**Revised 2026-08-20, via [ADR-046](ADR-046-transporter-aggregate-split.md).**
Two earlier passes of this doc are superseded here, kept below for honest
history rather than silently rewritten:

1. First pass: reuse `TradingPartner`'s existing `Type` discriminator —
   rejected (would grow `if Type == TRANSPORTER` branching indefinitely).
2. Second pass ([ADR-046](ADR-046-transporter-aggregate-split.md)'s initial
   recommendation): a fully separate `Transporter` aggregate duplicating
   identity fields (name, registrationNo, VAT no.) alongside its
   Transporter-specific data. Accepted initially, then **explicitly
   reconsidered and replaced by this option** once the ADR's own "Option C"
   alternative was reviewed and preferred.

**Decision:** `TradingPartner` (Phase 26, unchanged) stays the **single
identity aggregate for both Shipper and Transporter** — `Register`, its
`Type` discriminator, and its `Registered → Active ⇄ Suspended` lifecycle
are untouched, and `PartnerTypeTransporter` **stays a fully legal, actively
used value** (not retired — this reverses the prior draft's "required
correction," which no longer applies under this shape). A new
**`TransporterProfile`** aggregate — event-sourced, Temporal-orchestrated —
holds everything that's actually Transporter-specific: fleet, documents,
GIT state, tracking credentials, operating areas, the vetting workflow's
own state. `TransporterProfile` is keyed by the **same ID** as its
`TradingPartner` record (no separate surrogate ID, no join table) — a 1:1
relationship by shared identity, not a foreign key.

**Why this over the duplicated-identity version:** no duplicated fields at
all (not even the ~4 cheap scalars the previous version accepted), and it's
the more textbook DDD move — aggregate boundaries drawn around consistency
need (does this data need replay/saga/compensation?) rather than around
"type of business entity." `TradingPartner`'s existing, tested identity and
basic lifecycle genuinely serves both Shipper and Transporter unchanged;
only the vetting-specific data and behavior are new.

**What this costs, honestly** (the reason the duplicated-identity version
was picked first): registration becomes a two-step operation —
(1) `TradingPartner.Register(name, TRANSPORTER, context)`, then
(2) create the `TransporterProfile` and start
`TransporterVettingWorkflow` — and a genuine cross-aggregate invariant now
exists between them (`TradingPartner.Activate()` must not succeed for a
Transporter until `TransporterProfile` reaches `Vetted`). Both are handled
explicitly, not hand-waved:

- **Partial-failure handling between steps 1 and 2**: `CreateTransporterProfile(tradingPartnerID)`
  is idempotent (upsert-by-ID). The `Register` command handler retries step
  2 a bounded number of times before returning; if it still fails, the API
  returns "partner created, profile pending" rather than a bare error, and
  a standalone idempotent `EnsureTransporterProfile(tradingPartnerID)`
  command lets an operator retry step 2 alone without re-registering
  identity. A `TradingPartner{Type: TRANSPORTER}` with no profile yet is a
  visible, recoverable "stuck in Registered" state, not a security hole —
  materially less severe than the prior version's dual-entry-point hazard,
  where a legacy path could skip vetting *entirely* rather than just delay
  it.
- **The cross-aggregate `Activate` guard**: `TradingPartner.Activate()`
  itself is **unchanged** (still just checks `Status == Registered`,
  BR-TP03). The guard lives one layer up, in the command-handling/API
  boundary that already routes `activate` (`browserrpc`/`api.*` layer, not
  the domain): for a `TRANSPORTER`-typed partner, look up
  `TransporterProfile`'s current status from its own read model before
  delegating to `TradingPartner.Activate()`; reject with a new
  application-level error unless `Vetted`. For `SHIPPER`, this check is
  skipped entirely — BR-TP03's behavior for Shipper is identical to today,
  byte-for-byte. **Dependency direction matters here**: this check lives in
  a thin orchestration point that depends on *both* packages, or in
  `transporterprofile` calling into `tradingpartner`'s existing repository
  — never the reverse. `tradingpartner` does not gain a new dependency on
  the newer, more complex `transporterprofile` package. This is exactly
  the "cross aggregate invariant using Saga and compensating functions"
  the original design conversation asked to test — and a more genuine test
  of it than the duplicated-identity version gave, since the two sides are
  now real, separately-owned aggregates with different consistency models
  (plain CRUD vs. event-sourced), not two branches inside one aggregate.

**Design call — same service, new package (not a new microservice):**
`TransporterProfile` lands as a sibling domain package inside the (still
named, until the rename sub-phase) `trading-partner-service` — its own
`internal/domain`, `internal/postgres`, `internal/temporal`, own Postgres
tables in the same `trading_partner` Postgres database (or a new
`transporterprofile` schema in it — confirm at implementation time), and
its own JetStream stream — not a new container/port/compose entry. Kept
from the prior draft unchanged: this is a design call made to keep this
phase's infra footprint to "add Temporal + NATS Object Store," not also
"add a new service," and is easy to split into a real second service later
if the POC's findings warrant it.

**Migration note:** unchanged from the prior draft — no production data is
at stake; existing demo/seed data is dropped and reseeded under this shape.

**Correction (2026-08-20, via [ADR-046](ADR-046-transporter-aggregate-split.md)'s
own Correction note):** "no changes to `tradingpartner`" above is **not
accurate as originally stated**, on two independently-found, additive
counts, both now resolved elsewhere in this doc: a new `partner-update`
command/handler/repository/domain method is needed to make Company
Information editable at all (see "Concurrency"), and
`compliance_documents`' primary key needs a document ID to represent more
than one `GOODS_IN_TRANSIT` document at a time (see "Data sections" and
"Document storage"). The underlying decision is unaffected — both changes
are additive to a tested aggregate, the same regression profile this
section already argued for over Option A's *subtractive* alternative — but
the guarantee as written oversold it, and is corrected here rather than
quietly left standing.

## CRUD vs. event sourcing

Applying the repo's own test (`ARCHITECTURE.md` § "Event Sourcing vs Plain
CRUD" — *does anything need to replay this?*): **yes, for `TransporterProfile`,
no, for `TradingPartner`** — a cleaner split than the prior draft's, since
it's now genuinely one aggregate per answer, not one aggregate serving both.
The vetting decision sequence is itself a domain concern (an operator or
auditor needs to answer "what did we check, in what order, and who
approved what" after the fact), a Temporal workflow needs to durably
resume from a specific point after a crash, and the GIT/insurance saga
needs compensating actions defined against specific prior steps — all of
which need a real event history, not just current-state rows.

- **`TransporterProfile` aggregate**: event-sourced. JetStream stream
  `TRANSPORTER` (LimitsPolicy, replay-capable), subjects
  `evt.{context}.organizations.transporter.{id}.{event}` — `{id}` is the
  **same ID as the profile's `TradingPartner`**, not a separate surrogate;
  the second subject token is `organizations` (the eventual service name)
  even before the rename lands, since new subjects are cheaper to name
  right up front than to rename later; see "Naming & Sequencing."
- **Read side**: Postgres projection + NATS KV cache, same "Shape B"
  pattern `shipping-service` already settled on (eager write-through: the
  same JetStream handler that upserts Postgres also overwrites KV; cache
  miss falls through to Postgres). No new pattern invented here — reusing
  the decided shape is itself a data point for the pattern-cards doc
  ("does Shape B hold up for a second, differently-shaped aggregate?").
- **`TradingPartner`** (identity, both Shipper and Transporter): stays
  plain CRUD, entirely unchanged from Phase 26, per the decision above.

## Temporal — role and workflow design

Temporal orchestrates **only the `TransporterProfile` vetting workflow**,
not `TradingPartner`'s CRUD operations (editing company info, etc. stay
ordinary Postgres writes, no workflow involved) and not the profile's own
non-vetting commands (adding a fleet asset outside the saga is still a
plain event-sourced command). Each vetting-relevant state transition the
workflow drives also publishes a JetStream event, so the rest of the
platform (frontend reactivity, future cross-service consumers) integrates
the same way it already does with `shipping-service` — nothing downstream
needs to know Temporal exists.

**Reviewed in [ADR-047](ADR-047-transporter-vetting-temporal-saga.md) —
required amendment, not optional hardening:** every JetStream publish this
workflow triggers must happen inside a Temporal Activity (never inline in
workflow code — a hard Temporal determinism requirement on top of the
correctness reason below), carrying `Nats-Msg-Id` keyed on
`tradingPartnerID` + event type + a workflow-local step counter (**not**
Temporal's own `RunID`, which deliberately changes across a `Resubmit`),
with the stream's `Duplicates` window configured explicitly. This matters
because the workflow's own retry behavior makes it load-bearing, not
optional: if a publish Activity's JetStream write succeeds but its result
never reaches the worker (a crash — precisely what the durability test
below deliberately induces), Temporal retries the Activity, and without a
dedup guard that produces a **second**, distinct, validly-ack'd JetStream
event for the same transition. **Note this isn't free reuse of Phase 101**
— Phase 101 itself has zero implementation in this repo today (verified:
every checklist line unchecked, no `Nats-Msg-Id`/`Nats-Expected-Last-Subject-Sequence`
code anywhere); 38b is where this pattern gets built for the first time,
with the workflow's retry behavior exercising it from day one.

**New infra**: a Temporal server (`temporal` + `temporal-ui` containers,
dev-mode SQLite or Postgres backend) joins `demos/01-dictionary`'s compose
stack — this is a new dependency of the same magnitude as adding NATS
itself was, called out explicitly per the "no silent scope creep" spirit of
this repo's phase design gate.

**Workflow**: `TransporterVettingWorkflow`, workflow ID
`{context}-transporter-vetting-{tradingPartnerID}` (the shared ID, not a
separate profile surrogate), task queue `organizations-vetting`. Two
branches run **in parallel**, both required before the profile itself is
`Vetted` — deliberately structured as a saga with two independent,
each-compensable branches (not a linear if/else guard), since testing real
compensation was an explicit goal. Reaching `Vetted` is **not** the same as
`TradingPartner` becoming `Active` — see "Lifecycle" for the cross-aggregate
step that follows:

```
                        ┌─────────────────────────┐
                        │ TransporterVettingWorkflow│
                        └────────────┬─────────────┘
                    ┌────────────────┴────────────────┐
                    ▼                                  ▼
        ┌───────────────────────┐          ┌───────────────────────────┐
        │ Branch A: Documents    │          │ Branch B: GIT Verification │
        │                        │          │                            │
        │ 1. AwaitDocumentUpload │          │ 1. AwaitGitDocumentUpload  │
        │    (signal per doc)    │          │ 2. RequestGitVerification  │
        │ 2. ReviewDocument      │          │    (activity → mock        │
        │    (signal: approve/   │          │     insurer service;       │
        │     reject, per doc)   │          │     can fail or time out)  │
        │ 3. AllRequiredApproved?│          │ 3. GitVerified?            │
        └───────────┬────────────┘          └─────────────┬──────────────┘
                    │ both branches succeed                │
                    └───────────────┬───────────────────────┘
                                    ▼
                    ┌───────────────────────────────┐
                    │ ActivateFleetAssets            │
                    │ (flips each FleetAsset.        │
                    │  AvailableForAssignment = true)│
                    │ → TransporterProfile: Vetted   │
                    │ → publish transporter.vetted   │
                    └───────────────────────────────┘
                                    │
                (cross-aggregate step, NOT part of this workflow —
                 see "Lifecycle": TradingPartner.Activate() is
                 guarded on TransporterProfile.Status == Vetted)
```

`ActivateFleetAssets` and reaching `Vetted` are entirely **internal to
`TransporterProfile`** — an intra-aggregate saga outcome. Moving
`TradingPartner` to `Active` is a **separate, cross-aggregate** step
(auto- or operator-triggered) described in "Lifecycle" below; the workflow
itself never calls into `tradingpartner` directly.

**Compensation** (the actual saga test): if Branch B fails or times out
*after* Branch A has already approved some/all documents, the workflow runs
`CompensateRevertDocumentApprovals` and never runs `ActivateFleetAssets`;
if `ActivateFleetAssets` itself has already run (a redelivery/race edge
case) and a later event forces re-evaluation (e.g. GIT certificate expires
post-activation — see "Lifecycle" below), `CompensateDeactivateFleetAssets`
flips `AvailableForAssignment` back to `false`. This gives the design a
concrete, testable compensating-transaction pair rather than a single
happy path.

> **As built (38b) — two corrections to the paragraphs above.** See
> "Implementation notes (38a/38b as built)" near the top of this doc.
> (1) What flips is `TransporterProfile`'s own **`FleetAvailabilityGate`**
> boolean, via an explicit `FleetAvailabilityRevoked` event;
> `FleetAsset.AvailableForAssignment` is a computed read-layer value, not a
> field the workflow writes. (2) There is **no compensation for a
> GIT-success-only path** — because both branches must succeed before the
> gate opens or `Vetted` is reached, GIT passing alone never produced a side
> effect to undo. The post-`Vetted` expiry case is handled by
> `HandleGitStatusDrop`, which revokes availability *and* suspends the
> partner in one idempotent command.

**Compensation is a new event, never a rewrite ([ADR-047](ADR-047-transporter-vetting-temporal-saga.md)):**
in an event-sourced aggregate, "documents go back to `PendingReview`" can
only be correct as a **new**, explicitly-named event —
`DocumentApprovalReverted` (projected as "current status reads
`PendingReview` again"), `FleetAvailabilityRevoked` — appended to the log,
never a deletion or rewrite of the original `Approved`/`Available` event.
This isn't just correctness pedantry: the whole reason this aggregate is
event-sourced is so "what did we check, in what order, who approved what"
stays answerable after the fact — a literal revert-in-place would silently
destroy exactly that audit trail. The log should show *approved, then
reverted, then why*, not just *pending* with the approval's history erased.

**Mock insurer activity**: `RequestGitVerification` calls a small stub
(in-process fake or a tiny separate HTTP/NATS endpoint — decide at
implementation time) with three configurable outcomes for testing:
immediate pass, immediate fail, and hang-past-timeout — the last one is
what exercises the workflow's timeout → compensation path, not just the
explicit-rejection path. **Required, not a tuning nicety
([ADR-047](ADR-047-transporter-vetting-temporal-saga.md)):** the Temporal
Go SDK requires an Activity to declare `StartToCloseTimeout` or
`ScheduleToCloseTimeout` — omitting both is a startup-time configuration
error. Given real GIT verification could take far longer than a test run
should ever wait, and the workflow overall needs to tolerate human-timescale
document review (V2's `DOCUMENTS_IN_REVIEW` can last days) without an
unrelated execution timeout killing it, these values must be explicit and
environment-configurable — a short test-profile value distinct from a
production-scale placeholder — not left to whatever the SDK would otherwise
require you to guess at.

**Workflow ID reuse on `Resubmit`, needs explicit verification
([ADR-047](ADR-047-transporter-vetting-temporal-saga.md)):** starting a
fresh `TransporterVettingWorkflow` under the *same* workflow ID after a
prior run reached `Rejected` (terminal) depends on an explicit
`WorkflowIDReusePolicy` at start time — this repo's own Phase 101 already
models the right posture for an unverified SDK behavior claim ("⚠️ Verify
against current NATS docs before implementing"); the same discipline
applies here against the current Temporal Go SDK docs, not this doc's
assumption. A test that drives a workflow to `Rejected` and confirms
`Resubmit` actually starts a fresh run (not an ID-collision error) is part
of 38b, not an afterthought.

**Versioning — accepted POC-scope gap, not a silent omission
([ADR-047](ADR-047-transporter-vetting-temporal-saga.md)):** real vetting
could span days, and this workflow's code will likely change across 38b's
own iteration; Temporal's determinism rules mean changing workflow code
while instances are in flight needs `GetVersion`-style patching, which is
disproportionate engineering for what this phase actually tests (saga/
compensation/durability mechanics, not long-term production operability).
Not addressed here — mitigated only by not editing the workflow's code
while a test instance is genuinely mid-flight during the durability test
itself.

**Durability test** (explicit deliverable, not just "trust Temporal"):
start a vetting workflow, approve some but not all required documents,
**kill the Temporal worker process**, restart it, and confirm the workflow
resumes waiting for the remaining signals — it does not re-ask for
already-approved documents and does not lose the GIT-verification branch's
progress. This is the concrete acceptance test for "test durability too."

## Lifecycle — two aggregates, one guard between them

This is the direct consequence of the shared-identity decision above: there
are now genuinely **two** state machines, not one. `TradingPartner`'s
(`Registered → Active ⇄ Suspended`, BR-TP03–TP05) is **completely
unchanged** — same states, same transitions, same guards, for both Shipper
and Transporter. `TransporterProfile` gets its own, separate,
Temporal-driven vetting state machine. The only new coupling between them
is one guard: `TradingPartner.Activate()` (for a `TRANSPORTER`-typed
partner only) requires `TransporterProfile.Status == Vetted` first — see
"Decision" above for exactly where that check lives.

**V2 real shape, for comparison:** V2's screenshot "Status" column is not
one field — it's **four independent axes** on the real entities:
`BusinessEntity.accountInactive` (binary Active/Inactive — maps onto
`TradingPartner`'s own status here, unchanged from Phase 26),
`TransporterProfileEntity.status` (enum `TransporterProfileStatus`, 4
values: `NO_TS_AND_CS` → "T&Cs not accepted", `AWAITING_DOCUMENTATION`,
`DOCUMENTS_IN_REVIEW` → "Vetting in progress", `APPROVED` — maps onto
`TransporterProfile`'s new vetting state machine here),
`TransporterProfileEntity.underProbation` (an orthogonal boolean, admin-set),
and `TransporterRegistrationStepType` (`MINIMAL`/`DETAILS`/`LEGAL`,
registration-wizard progress). V2's own split across `BusinessEntity` vs.
`TransporterProfileEntity` is, gratifyingly, close to this design's own
`TradingPartner`/`TransporterProfile` split — independent validation that
the two-aggregate shape isn't an invented complication, V2 effectively has
the same seam. **V2 has no enforced transition guard between the 4 vetting
states** — the admin UI dropdown sets any of the four values directly, no
state-machine validator found in the backend. This design improves on that
(a Temporal-guarded state machine is the whole point of testing Temporal
here) — worth its own pattern-card observation (see "Outcomes").

```
TradingPartner (unchanged, Phase 26)          TransporterProfile (new, Phase 38)
──────────────────────────────────            ──────────────────────────────────
                                               AwaitingDocumentation
     Registered  ◀──register(TRANSPORTER)──    (≈ V2 NO_TS_AND_CS,
         │                                      T&Cs not yet accepted)
         │                                          │ (workflow starts)
         │                                          ▼
         │                                   DocumentsInReview
         │                              ┌────────────┤
         │                              ▼            ▼
         │                 docs+GIT both pass   docs or GIT fail/timeout
         │                              │            │
         │                              ▼            ▼
         │                          Vetted        Rejected
         │                      (≈ V2 APPROVED)  (compensations run —
         │                              │         POC addition; V2 has no
         │                              │         terminal state, just
         │                              │         stays in-review)
         │                              │              │
         │                              │         Resubmit ──▶ fresh workflow
         │                              │
         └───Activate()◀── guarded on ──┘
              (BR-TP03,               TransporterProfile.Status == Vetted
               unchanged)             — the cross-aggregate check; see
              │                       "Decision" for where it lives
              ▼
           Active ⇄ Suspended
        (≈ V2 accountInactive, BR-TP04/TP05, unchanged)

UnderProbation: independent boolean flag (≈ V2 underProbation), admin-set,
                informational only in v1 — V2 itself has no clear enforcement
                wiring for it either, so this phase doesn't invent one.
```

`Rejected` is terminal for a given vetting attempt but not for the
`TransporterProfile` record itself — an operator can trigger `Resubmit`
(mirrors `ComplianceDocument`'s existing `Resubmit` verb from Phase 26) to
start a fresh `TransporterVettingWorkflow`; `TradingPartner` simply stays
`Registered` throughout, since it was never told to activate.
Registration-step progress (`TransporterRegistrationStepType`) is **not**
modeled as a separate axis in this design — the wizard's own step position
(see "Frontend") already serves that purpose without a fourth persisted
status field.

## Data sections

Grounded in the real V2 source (see "Linebooker V2 source fidelity" above),
not just the two list-view/nav screenshots. Each row states V2's actual
shape and this phase's POC-scope subset, with the reason for anything
deliberately dropped or changed.

| Section | V2 real shape (verified) | POC scope, Phase 38 |
|---|---|---|
| **Company Information** | `BusinessEntity`: name, companyName, tradingSince (string), registrationNo, vatRegistrationNo, vatNumber, vatRegistered, vatRate/businessType/referralSource enums, message. `TransporterProfileEntity`: contactNo, contactPerson, addresses (typed `PHYSICAL`/`BILLING`). Plus a derived `acumaticaAccountCode` from an external accounting-linker integration (not a real column). | **Lives on `TradingPartner`** (existing, Phase 26, shared with Shipper — not duplicated, per the "Decision" above): name, companyName, registrationNo, vatRegistrationNo. `TransporterProfile` adds only what's genuinely Transporter-specific here: vatRegistered, contactPerson, contactNo/email, physical + billing addresses (typed, matching V2). **Dropped:** tradingSince, vatRate/businessType/referralSource (tax/marketing metadata orthogonal to the saga/event-sourcing question this POC tests), acumaticaAccountCode (external accounting integration, no analog here). |
| **Tracking Credentials** | Base `TrackingCredentialsEntity` (providerName, `trackingProvider` enum of 35 vendors) + **one satellite table per provider** storing the actual secret as **plain columns** — e.g. `CarTrackTrackingCredentialsEntity.apiKey`, `WebfleetTrackingCredentialsEntity.password` — confirmed **no encryption anywhere** in the codebase (no `@Convert`/`AttributeConverter` found). `credentialType` enum: `API_KEY`\|`USERNAME_PASSWORD`\|`METADATA_ONLY`. | One `TrackingCredential` child record: `provider` (enum, small representative POC list), `credentialType` (same 3-value enum as V2 — a genuinely useful part to keep), and a `credentialsConfigured bool`. **Confirmed divergence from V2**: the actual secret payload is written to a NATS KV bucket (`organizations-secrets`, at-rest encryption enabled) keyed `{context}.transporter.{id}.trackingcreds`, via a command that never publishes the secret onto the JetStream event stream — an event-sourced aggregate's log is meant to be replayed/audited, so baking raw credentials into it would be *worse* than V2's already-bad plaintext-column approach, since an event log can't easily redact history the way a row can be updated. |
| **Fleet** | `FleetAssetEntity`: type (`TRAILER`\|`HORSE`\|`RIGID`\|...), trailerType, registrationNo, vinNo, make, model, year, ownership (`OWNED`\|`SUBCONTRACTED`), status, trackingStatus, trackingCredentialsEntity link. Notable: `isOwner()` requires ownership=`OWNED` **and** a linked tracking credential **and** trackingStatus=`LIVE`, all three at once. | registrationNo (globally unique), VIN, make, model, vehicleTypeCode (validated live against refdata-service, BR-TP14), ownership (`OWNED`\|`SUBCONTRACTED`, kept — it's cheap and directly informs the saga), `availableForAssignment bool`. **Design call, inspired by V2's `isOwner()`:** `availableForAssignment` is computed the same multi-condition way — true only when ownership is resolved, tracking credentials are configured, *and* the saga's activation gate has passed — not a single hand-set flag. |
| **Operating Areas** | **Two models; only one is real** (corrected 2026-08-20 — see "V2 database verification" above). The `GeoAreaEntity` polygon/GIS model (hierarchical `COUNTRY`\|`REGION`\|`MUNICIPALITY`\|`CUSTOM` levels, JTS polygon geometry, joined via `TransporterOperatingAreaEntity` which denormalizes level + countryCode "for query performance" — V2's own code comment) exists in the schema and holds **zero rows**, as does its join table; the MapLibre GL + vector-tile frontend renders a model with no data in it. What V2 **actually runs** is a flat two-level list: `region_entity(id, name, country_id)` → `country_entity(id, name)`, joined many-to-many via `transporter_profile_entity_region` — **48,041 live assignments** over 217 regions and 57 countries, with no geometry, no level column and no depth below Region. `LinebookerTownRegion.xlsx` confirmed not wired into any code; `town_entity`'s 1,373 rows aren't wired into operating areas either. | **Country → Region matches what V2 runs — it is not a reduction of it.** This row previously called the two-level design "a reduced-depth version of V2's real hierarchical/polygon model"; the row counts disprove that, and the POC design needs no simplification defence. Keep **Leaflet + OpenStreetMap** over a small hand-authored GeoJSON. Join row stays `TransporterOperatingArea(transporterId, regionCode, level)` — `level` is kept even though V2's *live* join has no such column, because V2's flat list demonstrably needs one (Botswana's regions mix districts with cities, and a country-name catch-all row absorbs "nationwide" transporters; see "Operating Areas — region seed" below). Region list owned by refdata-service, **seeded from V2's real corpus** rather than invented. **One deliberate improvement on V2:** V2 has no locale dimension on region or country, so translations became *duplicate rows* — `Wes-Kaap`/`Vrystaat`/`IGauteng` sit beside `Western Cape`/`Free State`/`Gauteng` with their own ids and their own assignments, and South Africa has 11+ country rows (`Suid-Afrika`, `Sudáfrica`, `Afrique du Sud`, `ZA`, …). refdata-service already resolves locale as its own dimension, so the seed collapses these into canonical rows with locale-keyed labels. |
| **GIT Certificate** | **Confirmed derived, not stored:** `TransporterProfileEntity.gitStatus`'s getter ignores its own column and instead computes the "worst" status across the transporter's `GOODS_IN_TRANSIT`-typed documents (enum `GitStatusType`: `PENDING`\|`ACTIVE`\|`EXPIRED`\|`REJECTED`\|`NONE` — **5 values**, one more than the screenshot's visible 4). `gitCoverage` **is** a real stored `Long` directly on the profile — separate from any one document's own coverage field. Admin override: `hideGitRequiredForAllocation`. | No new fields beyond one addition below — reuses the existing `ComplianceDocument` type `GOODS_IN_TRANSIT`. `GitStatus` is **derived** exactly as V2 does it (worst-of-documents, same 5-value enum — corrected from this doc's earlier 4-value assumption); `GitCoverage` is a separate stored field on the `TransporterProfile` aggregate root, not per-document. `hideGitRequiredForAllocation` carried into Admin Settings below, since it directly gates this phase's saga. **Requires a `tradingpartner` schema change** ([ADR-048](ADR-048-document-storage-nats-object-store.md) finding 2c, resolved in "Document storage" below): `compliance_documents`' PK `(trading_partner_id, type)` allows only one `GOODS_IN_TRANSIT` document at a time, which cannot represent a renewal existing alongside an expiring certificate — the true worst-of-documents case this row's own V2 fidelity requires. The PK gains a service-minted document ID; superseding a document becomes an explicit transition, not a silent upsert. Also **maintained, not just checked once at activation**, via the Temporal cron workflow in "Cross-aggregate invariant / saga" below — `EXPIRED` arrives from the passage of time alone, with no event to hang a one-time guard on. |
| **Documents** | `TransporterDocumentEntity` → `DocumentEntity` (contentType, documentName, storedFileName, documentLocation, documentSize, uploadDate). Blobs stored in **Google Cloud Storage** (`GoogleCloudStorageServiceImpl`) — not Firebase (the Firebase Admin SDK config present in the repo isn't consumed by document upload anywhere found), not S3. | Metadata field names aligned to V2's for closer fidelity (documentName, contentType, documentSize, uploadDate). **Storage backend intentionally differs from V2**: NATS Object Store, not GCS — this is a NATS-pattern evaluation lab, not a GCS-integration exercise; the decision in "Document storage" below stands unchanged. |
| **Rate Sheets** | `RateSheetEntity` is owned by `CustomerProfileEntity`, not Transporter; per-lane entries (`RateSheetVersionEntryEntity`) reference a `customerRouteEntity` (the lane), `vehicleTypeEntities`, a diesel-escalation sub-model, and a `FeeScaleEntity` link. **From the Transporter's side, V2's UI is read-only** — transporters view rates customers set for them; they don't author their own flat rate table. | **Resolved: stub/placeholder tab only for this phase.** The earlier "structured data capture, Transporter-owned" answer assumed a shape V2 doesn't actually have — a faithful version needs Customer + Route, both out of scope here. This phase adds the UI tab (empty state, "available once Customer/Route exist") but no persistence or domain model — matching V2's real ownership rather than building a Transporter-owned table that has no analog in the system being replicated. Revisit with real fidelity if/when a Customer phase lands. |
| **Admin Settings** | `businessVisibility` enum + specific-customer visibility list; Load Access flags (`biddingAllowed`/`allocatedAllowed`/`allocatedBiddingAllowed`, feature-flagged tender variants); `hideGitRequiredForAllocation`; `underProbation`; `businessCommentEntities` — an append-only, timestamped, per-user comment log (not a single notes field). | marketplace-visibility toggle (matches V2's `businessVisibility`), Load Access flags carried as **informational/no-op fields for now** (mirrors BR-TP04's existing "no enforcement consumer yet" pattern — the future marketplace/tender consumer is still the same deferred item), `hideGitRequiredForAllocation` (real gate on this phase's saga). **Notes: reuse the existing `audit_events` table/pattern from Phase 26** instead of a new free-text notes field — V2's own comment log is structurally the same "timestamped per-actor entry," and this repo already has that pattern built. **Dropped:** "preferred payment terms" (this doc's earlier guess — not found anywhere in V2). |

## Operating Areas — region seed

Sourced 2026-08-20 from the live `linebooker_v2` database, not invented.
Closes open question 1.

### Scope: South Africa, Botswana, Namibia

V2's corpus spans 42 countries with regions, but usage is concentrated: SA
(2,886 transporters), Namibia (601), Botswana (722), Zimbabwe (669), Zambia
(615), Mozambique (546), Lesotho (582), Eswatini (410) — the SADC road
corridor — then a long tail of single-digit rows (`Norway` 2, `Bangladesh`
2, `Croatia` 0).

Seeding **SA + Botswana + Namibia** (33 regions) is the recommendation. It
is enough to exercise the Country → Region hierarchy, a country switcher,
and cross-border operating areas, while keeping hand-authored GeoJSON to
three countries. Each additional country is mostly GeoJSON labour with no
new design content. The other SADC countries are a later seed-data task,
not a design change.

### Three data-quality decisions the real corpus forces

V2's flat list has no constraints protecting any of these, which is the
most useful thing the corpus tells us:

1. **Locale duplicates collapse.** Translations exist as separate region
   rows with their own assignments — SA's 9 provinces appear as 17 rows
   (`Wes-Kaap` 1,823, `Noord-Kaap` 1,783, `Noordwes` 1,777, `Vrystaat`
   1,775, `IFleyistata` 915, `KwaXhosa` 799, `IGauteng` 557,
   `IKipi laseNyakatho` 553); Namibia repeats `Erongo`/`Erongo Region`,
   `Khomas`/`Khomas Region`, `Hardap`/`Hardap Region`,
   `Oshikoto`/`Oshikoto Region`, `Otjozondjupa`/`Otjozondjoepa`. Seed
   canonical rows keyed by code, with locale-keyed labels via
   refdata-service's existing locale dimension.
2. **"Nationwide" is a level, not a region.** Botswana and Namibia each
   have a region row named after the country itself — `Botswana` (708
   transporters) and `Namibia` (596), the highest counts in each country —
   used to mean "operates nationwide." Do **not** seed these as regions.
   Represent them as a `level = COUNTRY` assignment, which the
   `TransporterOperatingArea.level` column already supports. This is the
   clearest justification for keeping `level`: without it the concept has
   nowhere to live except a fake region, which is exactly what V2 did.
3. **Levels must not mix in one list.** Botswana's rows interleave
   districts with cities and towns (`Gaborone City` 208, `Francistown
   City` 173, `Lobatse Town` 66, `Jwaneng Town` 23, `Selibe Phikwe Town`
   3) plus Setswana duplicates (`Kgaolo ya Ghanzi`, `Kgaolo ya Legare`,
   `Motsana wa Molapowabojang`). Seed `level = REGION` rows only; the town
   rows map to ISO's separate city codes and are out of scope for the
   two-level POC.

### The seed

Codes are **ISO 3166-2**, which V2's own list tracks closely enough to
confirm the source (its Botswana rows match the ISO district + city split
almost exactly). Codes are canonical; `name` here is the `en` label, with
other locales carried as refdata locale variants.

**South Africa (`ZA`) — 9 provinces, all 9 in live use**

| Code | Name | V2 transporters | Locale duplicates in V2 |
|---|---|---|---|
| `ZA-GP` | Gauteng | 2,720 | `IGauteng` |
| `ZA-KZN` | KwaZulu-Natal | 2,558 | — |
| `ZA-WC` | Western Cape | 2,508 | `Wes-Kaap` |
| `ZA-MP` | Mpumalanga | 2,495 | — |
| `ZA-LP` | Limpopo | 2,491 | — |
| `ZA-FS` | Free State | 2,471 | `Vrystaat`, `IFleyistata` |
| `ZA-NW` | North West | 2,448 | `Noordwes` |
| `ZA-EC` | Eastern Cape | 2,416 | `KwaXhosa` |
| `ZA-NC` | Northern Cape | 2,366 | `Noord-Kaap`, `IKipi laseNyakatho` |

**Botswana (`BW`) — 10 districts** (`BW-CE` Central, `BW-CH` Chobe,
`BW-GH` Ghanzi, `BW-KG` Kgalagadi, `BW-KL` Kgatleng, `BW-KW` Kweneng,
`BW-NE` North-East, `BW-NW` North-West, `BW-SE` South-East, `BW-SO`
Southern). V2 additionally carries 5 city/town rows — excluded per decision
3 above.

**Namibia (`NA`) — 14 regions** (`NA-ER` Erongo, `NA-HA` Hardap, `NA-KA`
ǁKaras, `NA-KE` Kavango East, `NA-KW` Kavango West, `NA-KH` Khomas,
`NA-KU` Kunene, `NA-OW` Ohangwena, `NA-OH` Omaheke, `NA-OS` Omusati,
`NA-ON` Oshana, `NA-OT` Oshikoto, `NA-OD` Otjozondjupa, `NA-CA` Zambezi).

**One open call on Namibia:** V2 has a single `Kavango Region` (126
transporters), predating the 2013 split into Kavango East/West; ISO has
both. The seed lists both (current reality) — if the demo ever imports V2
assignment data, `Kavango Region` needs a mapping decision. Note also that
V2's `??Karas` is a mojibake'd `ǁKaras` (the name genuinely begins with a
lateral click character), so the seed should carry the correct Unicode and
not inherit V2's encoding damage.

## Document storage — NATS Object Store

**Chosen over S3/MinIO** because tenant isolation comes free from the NATS
account boundary that already isolates every stream and KV bucket, and
because it's itself a pattern worth documenting/comparing for the eventual
pattern-cards doc. Reviewed in
[ADR-048](ADR-048-document-storage-nats-object-store.md), which **affirms
the choice but rejects the original rationale**: "avoids a new infra
dependency purely for file bytes" framed this as cost-free, and it isn't.
The honest framing is *deliberately accepting a shared failure domain with
the event log, in exchange for evaluating the NATS pattern* — see the quota
point below. S3/MinIO is genuinely better on failure isolation and on byte
transport (presigned URLs); it is worse on tenant isolation and teaches this
lab nothing, which is why the decision stands.

- One Object Store bucket, tenant-scoped by NATS account **the same way KV
  buckets already are** (see the repo-wide KV convention: one bucket per
  role per account, `{context}` lives in the key, not the bucket name) —
  e.g. bucket `organizations-docs`. **Confirmed sound** by ADR-048.
- **Explicit limits are required, and are a new convention here.** No KV
  bucket in this repo sets any limit today (every creation site passes only
  `Bucket`, one adds `TTL`). An Object Store bucket is a JetStream stream,
  so it draws on the tenant account's **already-tight** JetStream limits:
  a default tenant gets `DiskStorage: 1 GiB`, `MaxStreams: 10`
  (`accounts-service/accounts/handler.go:344`). Two consequences: document
  bytes and the event log **compete for the same 1 GiB**, so enough PDFs
  can stop event publishing for the whole tenant; and Phase 38's new
  streams (`TRANSPORTER`, the organizations read cache, the secrets bucket,
  `OBJ_organizations-docs`) plus refdata's per-context *versioned* KV
  buckets can plausibly exceed `MaxStreams: 10`. So: set `MaxBytes` on the
  bucket, cap per-file size at the service boundary, and count the
  per-tenant stream budget before 38c-ii — raising tenant limits via the
  existing `POST /api/accounts/{name}/jslimits` endpoint if needed.
- Object name: `{context}.transporter.{id}.{docType}.{documentID}` — `{id}`
  is the shared `TradingPartner`/`TransporterProfile` ID (see "Decision"),
  not a separate surrogate; `{documentID}` is a **service-minted UUID**.
  Mirrors the existing KV key format (`{context}.{entityType}.{id}`) with
  the doc type and document ID appended, consistent with the repo's
  established naming convention rather than inventing a new one.
  **The original filename is deliberately not in the name** (ADR-048
  finding 2): a user-controlled name makes object identity user-controlled,
  and two uploads of the same docType with the same filename would resolve
  to the same object — silently purging the earlier document's bytes while
  the event log still records both uploads, so the log would assert a
  document that cannot be retrieved. Filename lives in Object Store
  metadata plus the Postgres projection instead. (This also sidesteps
  character legality: KV keys here are restricted to `[-/_=.a-zA-Z0-9]`,
  while real filenames carry spaces, parentheses and non-ASCII.)
- **Write order is forced: blob first, event second, never the reverse.**
  Nothing spans Object Store and the event stream transactionally, and the
  two failure modes aren't symmetric. Publish-then-upload leaves a dangling
  reference in an **immutable** log — unrecoverable in kind, since an event
  can only be compensated, not retracted (see
  [ADR-047](ADR-047-transporter-vetting-temporal-saga.md)'s same constraint
  one layer up). Upload-then-publish leaves at worst an orphan blob:
  invisible to every reader, garbage-collectable by name. This composes
  with ADR-047's `Nats-Msg-Id` dedup only because the object name above is
  stable and service-minted, which makes the upload idempotent under
  Temporal Activity retry — the two amendments depend on each other.
- **Objects are never deleted in this POC.** Deliberate policy, not an
  omission: objects can be deleted, events cannot, so any real erasure
  leaves the log referencing bytes that no longer exist. Retention/erasure
  is named as out of scope. Worth keeping for the pattern-cards doc, since
  the tension resolves in an interesting direction — the blob store is
  where erasure *can* live precisely because the log holds references
  rather than payloads, the same principle that put Tracking Credentials in
  encrypted KV rather than the event stream.
- `ComplianceDocument.Reference` stores this object name; upload/download
  goes through `organizations-service` (never a raw NATS Object Store
  client credential handed to the browser), matching how the browser never
  gets `rpc.>`. **This needs an ingress the service does not currently
  have** (ADR-048 finding 5): the whole command surface is NATS `micro`
  request/reply with JSON bodies, which is neither a streaming transport
  nor able to exceed the server's `max_payload`, and Phase 33.5
  deliberately deleted all fourteen REST routes leaving only `/healthz`.
  There is no multipart handling, `io.Copy`, or body-size cap anywhere in
  the repo to model on. So 38c-ii must reintroduce a **dedicated HTTP
  upload/download endpoint** (own max-body limit, own auth) — a scoped,
  deliberate partial reversal of Phase 33.5, and real work to budget rather
  than an assumed capability. Note also that Object Store has no
  presigned-URL equivalent, so the service is a *mandatory* byte proxy in
  both directions — a permanent property, and a fair point in S3's favour
  for the pattern-cards comparison.
- **Coupling to GIT status, resolved:** the Data-sections table derives
  `GitStatus` as the worst across the transporter's `GOODS_IN_TRANSIT`
  documents — *plural*, faithfully matching V2's real getter — but
  `compliance_documents`' primary key today is `(trading_partner_id, type)`,
  i.e. one document per type, with `document-add` as an upsert. That shape
  cannot hold more than one `GOODS_IN_TRANSIT` document at a time, which
  breaks the moment a certificate is renewed: the renewal's `document-add`
  upserts over the expiring one before it has actually expired, so V2's
  worst-of-documents derivation would only ever see one document, never a
  transition between two. **Resolved: the primary key gains a service-minted
  document ID** — `(trading_partner_id, id)`, `type` becomes a plain
  (non-unique) column, and `document-add` becomes an insert, not an upsert
  (superseding/expiring a document is a new explicit
  `document-supersede`-style transition, not a silent overwrite — the same
  "never silently destroy an auditable prior state" principle behind
  ADR-047's compensation-events requirement and this section's own
  filename-vs-object-name fix above, applied a third time). This is a
  genuine `tradingpartner` schema and domain change — see "Decision" and
  ADR-046's Correction note — additive (existing single-document callers are
  unaffected by widening the key), not a breaking one.

## Cross-aggregate invariant / saga — two layers, not one

The shared-identity decision above splits what was one saga story into two,
genuinely different layers — both worth testing, for different reasons:

**1. Intra-aggregate saga (inside `TransporterProfile`):** a
`FleetAsset` cannot be `availableForAssignment` unless (a) all required
compliance documents are `Approved` and (b) GIT coverage is verified,
active, and unexpired. Enforced by the two-branch saga above, not a
synchronous check at read time — the invariant can be **temporarily
violated during the saga** (documents approved while GIT verification is
still pending) and must be **actively repaired** via
`CompensateRevertDocumentApprovals`, not just guarded against up front.
Both sides of this saga live in the same aggregate, same consistency
model — this tests Temporal's compensation machinery.

**2. Cross-aggregate activation gate (`TransporterProfile` ↔ `TradingPartner`):**
`TradingPartner.Activate()` must not succeed for a `TRANSPORTER`-typed
partner unless its `TransporterProfile` has reached `Vetted`. This is the
**genuinely cross-aggregate** case — two separately-owned aggregates with
different consistency models (event-sourced vs. plain CRUD), connected by
one guard at the command-handling boundary, not a Temporal saga branch.
It doesn't need compensation in the same sense (nothing is optimistically
executed and later undone here — `Activate` simply refuses to proceed), and
it's the more realistic shape of a cross-aggregate constraint in a system
with genuinely separate bounded contexts, worth reporting on separately
from (1) in the eventual pattern-cards doc.

**Deliberately called a *gate*, not an *invariant*
([ADR-049](ADR-049-cross-aggregate-concurrency.md) finding 2).** As designed
this is a **precondition checked once**, at activation, and nothing
re-checks it afterwards. That gap has real teeth here because of a decision
made elsewhere in this same design: `GitStatus` is **derived, and one of its
inputs is time** — `EXPIRED` arrives by the passage of a date, with no
command, no actor, and **no event** to hang a guard on. So a
`TradingPartner` can sit at `ACTIVE` indefinitely with an expired GIT
certificate, and nothing notices. That is the constraint being broken in the
ordinary course of business, not under a race.

Three options were considered:

- **(a) Gate only** — scope it explicitly as a precondition, state that
  post-activation drift is out of scope. Cheapest, but the pattern-cards doc
  could then never claim a *maintained* cross-aggregate invariant — only a
  precondition, which undersells what this phase set out to test.
- **(b) React to revocation** — a durable `evt.*` consumer suspends the
  partner when the profile leaves `Vetted`. Handles a saga compensation
  (`FleetAvailabilityRevoked`) reaching `TradingPartner`, but **cannot catch
  `EXPIRED`** at all — nothing publishes when a date simply passes, so this
  option alone leaves the sharpest version of the gap (silent time-based
  drift) exactly as open as (a) does.
- **(c) Scheduled re-evaluation** — the only option that catches time-derived
  expiry, since nothing publishes when a date passes.

**Resolved: (c), implemented as a lightweight Temporal cron workflow, and it
subsumes (b) rather than needing it as a separate mechanism.**

- A `TransporterGitMonitorWorkflow` starts once, when
  `TransporterVettingWorkflow` reaches `Vetted` (chained, not a second
  manually-triggered workflow), using Temporal's built-in cron schedule
  (`CronSchedule`) rather than a hand-rolled sleep loop — daily is sufficient
  for a POC; the interval is config-driven like the Activity timeouts in
  "Temporal" above.
- Each tick: an Activity reads `TransporterProfile`'s current `GitStatus`
  (Postgres projection — same read-side rule as the `Activate` guard) and
  `TradingPartner`'s current status from the same orchestration layer that
  already implements the `Activate` guard (never a new dependency direction
  — this workflow calls into both existing read paths, it doesn't add
  either aggregate a dependency on the other).
- If `GitStatus` is no longer `ACTIVE` (covers `EXPIRED` from the passage of
  time **and** any saga compensation that already revoked fleet
  availability — one check subsumes both triggers, so (b)'s reactive
  consumer is not needed as a second mechanism) **and** `TradingPartner` is
  currently `Active`, the workflow calls `TradingPartner.Suspend(id,
  "GIT certificate expired or revoked")` — the existing BR-TP04 operation,
  unchanged, invoked from the orchestration layer exactly as `Activate`
  already is. No new `tradingpartner` domain code.
- The workflow terminates itself once `TradingPartner` reaches `Suspended`
  or `Reactivate`d off this reason and re-vetted — bounding it to a finite
  lifetime rather than running forever per transporter, which keeps it
  inside the same "acceptable POC scope" versioning-risk envelope
  [ADR-047](ADR-047-transporter-vetting-temporal-saga.md) already accepted
  for the main vetting workflow.

This makes the word "invariant" earned: the constraint is now actively
maintained, not just checked once at activation, and it demonstrates a
second, genuinely different Temporal capability (durable cron re-evaluation)
beyond the saga/compensation mechanics `TransporterVettingWorkflow` already
tests — a stronger pattern-cards result than (a) or (b) alone.

## Concurrency — two operators editing the same Transporter

**This now splits by aggregate, and needs two different mechanisms — a
consequence of the shared-identity decision the earlier single-aggregate
answer didn't need to consider.** Reviewed in
[ADR-049](ADR-049-cross-aggregate-concurrency.md), which affirms the
two-mechanism split (it's the correct consequence of ADR-046) but corrects
the sizing of both halves substantially.

- **`TransporterProfile` fields** (fleet, documents, GIT, tracking
  credentials, operating areas): event-sourced, so this draws on the design
  already proposed for the Ship domain in **Phase 101** ("Write-Side Safety
  — Optimistic Concurrency + Publish Dedup"): commands carry the last-seen
  stream sequence, JetStream's `Nats-Expected-Last-Subject-Sequence` guards
  the publish, a losing concurrent writer gets rejected, re-hydrates, and
  either auto-retries or surfaces a conflict in the UI.
  - ⚠️ **The severity of Phase 101's own flagged caveat does not transfer
    from Ship to `TransporterProfile` — this is a blocking design decision
    for 38a, not an implementation detail** (ADR-049 finding 1). The header
    guards **the published subject only**, and the subject taxonomy puts
    the event type in the **last** token. Ship gets away with this: four
    event types driven by one naturally-serialising state machine, so two
    operators racing on the same ship collide on the same subject.
    `TransporterProfile` is the opposite — its event types are concurrent
    *by design* (an operator adds a fleet asset while another approves a
    document while the workflow records GIT verification). Three different
    final tokens, three different subjects, three per-subject guards that
    each pass, and **no conflict detected between any of them**. The
    mechanism is close to a no-op for exactly the scenario this section
    exists to address. **Resolved:** use
    `Nats-Expected-Last-Subject-Sequence-Subject` — confirmed present and
    client-supported in the pinned `nats.go v1.52.0`
    (`jetstream.ExpectedLastSubjSeqSubjHeader`,
    `jetstream/message.go:198`, exercised by that version's own
    `publish_test.go`), so this is a verified capability, not an assumption
    to carry into implementation. Every `TransporterProfile` publish sets
    this header to the wildcard filter `evt.{context}.organizations.transporter.{id}.>`
    (via `PublishAsyncPending`/`PublishMsg`'s `WithExpectLastSequencePerSubject`-
    style publish option, applied against the filter rather than one leaf
    subject), so a losing writer on *any* event type for this aggregate is
    rejected — not just a same-type collision. This keeps the repo-wide
    subject taxonomy exactly as specified in
    ARCHITECTURE-COMMUNICATIONS.md § 2 (fixed arity, `{event}` last,
    positional parsing) — no divergence needed, and the one-subject-per-
    aggregate fallback is dropped from consideration.
  - **Interaction with Temporal, which neither ADR-047 nor the prior draft
    covered** (ADR-049 finding 4): an operator edit landing between a
    publish Activity's hydrate and its publish gets the append rejected
    (err 10071), which Temporal sees as a failed Activity and retries. The
    two designs are compatible — ADR-047's `Nats-Msg-Id` is keyed on
    `tradingPartnerID` + event type + step counter, deliberately not the
    `RunID`, so it stays stable across retries while the expected sequence
    changes — but a sequence conflict is **not a business failure** and
    must never reach the compensation path. A persistent editor could
    otherwise exhaust the retry policy and fire
    `CompensateRevertDocumentApprovals` because two humans were typing at
    once. Required: classify sequence-conflict as its own retryable error
    type with a retry policy sized for human edit cadence, surfacing as
    "try again," never as a failed vetting.
- **`TradingPartner` fields** (Company Information — name, registrationNo,
  vatRegistrationNo): plain CRUD, so Phase 101's JetStream-sequence
  mechanism **does not apply here** — this aggregate has no event stream to
  guard a publish against. Two operators editing company name
  simultaneously needs a classic optimistic-lock instead: a `version`
  column, `UPDATE ... WHERE id = ? AND version = ?`, reject with 409 on a
  mismatched version, same UI conflict treatment as above.
  - **Correction (ADR-049 finding 5a):** an earlier draft of this section
    said status transitions "already get a natural check via
    `WHERE status = ?`". **There is no such predicate.** The only UPDATE on
    the table is `SET status = $2 WHERE id = $1`; safety comes from a
    pessimistic row lock taken earlier in the same transaction
    (`SELECT … WHERE id = $1 FOR UPDATE`), so the domain guard always runs
    against the persisted status. The conclusion (transitions are safe) was
    right; the reason was wrong, and it matters — the `version` column is a
    *second, different* mechanism alongside a pessimistic lock, not an
    extension of an existing compare-and-set. It is still genuinely needed,
    for a reason worth stating because "we already lock the row" is an easy
    and wrong objection: `FOR UPDATE` locks at *save* time, so operator A
    opening an edit form, B saving, then A saving is a **silent lost
    update** — A's transaction sees B's value and overwrites it, no
    conflict raised. Detecting that needs a version compared against what A
    *read*, which a row lock structurally cannot do. Pessimistic locks
    protect transactions; optimistic locks protect user think-time.
  - **This is more than a new column — it's new `tradingpartner` code**
    (ADR-049 finding 5b). Company Information is not editable today *at
    all*: the repository port exposes only
    `Register`/`Get`/`List`/`Activate`/`Suspend`/`Reactivate`, the fourteen
    `api.*` handlers contain no `partner-update`, and `registerRequest`
    accepts only `{Name, Type}` — so `trading_as`, `company_name`,
    `registration_no` and `vat_registration_no` are columns that **no code
    path ever writes a non-empty value into**. Making the section editable
    needs a new domain method, repository method, command, and `api.*`
    handler, plus the `version` column. See "Open questions" for where it
    lands; and note this is one of **two** independently-found
    `tradingpartner` changes (the other is `compliance_documents`' PK, in
    "Document storage") that ADR-046's "zero changes to `tradingpartner`"
    claim did not anticipate.
- **One composed UI over two conflict mechanisms** (ADR-049 finding 6): the
  "Frontend" section's promise that the split is *a backend seam only* is
  correct for **reads** and a hazard for **writes**. The two aggregates fail
  differently (a JetStream 10071 rejection vs. a Postgres version mismatch),
  and a single save spanning the Company Information tab and a profile tab
  can half-commit — leaving one aggregate updated, one not, and a UI that
  has deliberately hidden which is which. **Save boundaries must align to
  the aggregate boundary** (per-section saves, never one submit spanning
  both), and the conflict UI must name the section that lost.
- **The activation gate's stale read fails in the permissive direction**
  (ADR-049 finding 3): the guard reads the Shape B read model, which lags
  the log, so it can still read `Vetted` after a revocation/compensation
  event is published but not yet projected — activation then succeeds on a
  premise that is already false. (The reverse direction merely refuses and
  self-corrects.) Cheap mitigation: the guard reads the **Postgres
  projection, not the KV cache**, with the accepted staleness window stated
  explicitly. Strictly-correct would mean hydrating the profile's event
  stream at guard time — disproportionate here, but a recorded choice rather
  than a default. Distinct from
  [ADR-047](ADR-047-transporter-vetting-temporal-saga.md) finding 5, which
  confirmed the guard reads the read model *rather than Temporal*; this is
  about which layer of the read model.

## Frontend

Nav: `Transporters` already exists in the Tech Lab Operator UI
(`refdata` app) as a sibling of `Shippers`, both currently routing into the
same `TradingPartnersPanel.vue`. This phase gives Transporter its **own**
component (mirrors the aggregate split) rather than continuing to share
one panel parameterized by type. **The two-aggregate split (`TradingPartner`
+ `TransporterProfile`) is a backend seam only** — the UI composes both
into one record the operator never sees as "two things": a single API
call (or two calls composed server-side) backs the detail view, and the
top-level status shown to the operator is `TradingPartner`'s (Registered/
Active/Suspended), with vetting progress shown as a sub-status underneath,
not as a second, competing status field.

- **Registration**: a short multi-step wizard (Company Info → Fleet →
  Operating Areas → Documents) that creates the `Draft` record and kicks
  off `TransporterVettingWorkflow` on submission of the last step.
- **Detail view**: a tabbed record page, one tab per data section above,
  plus a **state-transition visualization** at the top — a horizontal
  stepper (`Draft → Documents → Under Review → GIT Verification → Vetted →
  Active`, with `Suspended`/`Rejected` as visually distinct side-branches)
  showing current position, driven by the same `evt.*`/KV read model the
  rest of the frontend already watches (no direct Temporal dependency in
  the browser — confirmed sound in
  [ADR-047](ADR-047-transporter-vetting-temporal-saga.md): every status
  reference in this design goes through the read model, never Temporal's
  own workflow Query API, avoiding a hard runtime dependency on Temporal
  just to read status).
- **Vetting/compliance status**: per-document approve/reject badges (reuse
  the existing `ComplianceDocument` status pattern) plus a derived overall
  GIT Status badge — **5 values** (`None`/`Pending`/`Active`/`Expired`/
  `Rejected`, matching V2's real `GitStatusType` enum, one more than the
  screenshot's visible 4), computed as the worst-of-documents exactly as
  V2 does it, not hand-set.
- **Operating Areas map**: Leaflet + OpenStreetMap, a two-level
  (Country → Region) overlay from a small hand-authored GeoJSON, toggled by
  click, kept in sync with a checklist view for accessibility/precision.
  V2's vector-tile/polygon-drawing stack (MapLibre + Municipality/Custom
  levels) is not replicated — and note that stack renders V2's *unpopulated*
  GIS model, so this is not a fidelity gap so much as declining to rebuild
  something V2 never switched on.
- Visual design: reuses `shared/unifi-theme` + `shared/ui-shell` exactly as
  every other panel in this app does (per this repo's frontend design
  system rule) — no new palette or shell for this feature. A concrete
  wireframe (via the `frontend-design` skill) is a good next deliverable
  once this written design is confirmed, kept as a separate step rather
  than folded into this doc.

## Naming & sequencing

The `trading-partners` → `organizations` rename (service directory,
package name, subject tokens, UI labels) happens **last**, as its own
sub-phase, after the vetting/Temporal/Object-Store work ships and is
verified under the current name. Rationale: renaming first would mean
verifying brand-new, higher-risk domain logic (Temporal, sagas, Object
Store) at the same time as a mechanical rename, making it harder to
attribute a regression to one or the other. New subjects introduced by this
phase are still named `organizations` from day one (see "CRUD vs. event
sourcing" above) since subject names are cheap to get right up front and
expensive to migrate later once consumers exist.

Sub-phase order, in execution sequence (tracked as one plan Phase 38 with
lettered sub-phases — settled 2026-08-20, they stay lettered rather than
becoming separate phase numbers; see the note after this list):

1. **38a** — `TransporterProfile` domain package, event sourcing skeleton
   (aggregate keyed by the shared `TradingPartner` ID, commands, JetStream
   stream, Postgres projection, KV cache) — no Temporal yet, just the
   CRUD-shaped commands (add fleet asset, edit profile fields) proven
   event-sourced end to end, every publish carrying
   `Nats-Expected-Last-Subject-Sequence-Subject` against the aggregate's
   wildcard filter (see "Concurrency" — confirmed supported, no taxonomy
   divergence needed). Also: the idempotent
   `CreateTransporterProfile`/`EnsureTransporterProfile` pair (see
   "Decision"), and the cross-aggregate `Activate` guard at the
   command-handling boundary reading the Postgres projection (not the KV
   cache). `TradingPartner.Register` already accepts
   `PartnerTypeTransporter` today and needs no change; `tradingpartner`'s
   own two additive changes (`partner-update`, `compliance_documents`' PK)
   are tracked separately (see "Open questions" 3 and 6) and don't block
   this sub-phase's start.
2. **38b** — Temporal vetting workflow + GIT saga + compensations +
   durability test + the `TransporterGitMonitorWorkflow` cron workflow that
   makes the activation gate a maintained invariant (see "Cross-aggregate
   invariant / saga").
3. **38c-i** — *(built 2026-08-20)* The `tradingpartner` schema pass plus
   editable Company Information. Both additive schema changes land here in one migration:
   the `compliance_documents` change (service-minted document ID, PK
   widened to `(trading_partner_id, id)`) and the `version` column with
   `partner-update` and a widened `Register` (see "Open questions" 3 and
   6). **Split out of a single 38c on 2026-08-20** — see the note below
   this list.
4. **38d-i** — *(built 2026-08-20)* Transporter UI: dedicated component,
   wizard, tabbed detail view, state-transition stepper, Company Information
   editing. **Not purely
   frontend** — an earlier draft of this list said it was, which was wrong.
   No `api.*` endpoint exposed `TransporterProfile` at all (38a wired its
   projection reader only into the activation guard), so the stepper and
   vetting tab had no data source and 38b's saga was invisible to any UI.
   38d-i therefore adds `partner.profile.get` (the 16th endpoint) plus the
   derived GIT status (BR-TP37/BR-TP38). Owner of the
   conflict-*presentation* decision (BR-TP39: inline banner, operator's
   input preserved). Runs ahead of 38c-ii, so its Documents tab ships
   against `compliance_documents` metadata only, with upload/download
   visibly deferred rather than stubbed as no-ops.
   *As built*, it also had to fix the browser's request helper, which dropped
   the error envelope's `conflict` discriminator — without it BR-TP39's banner
   could not be triggered at all, and a 409 had been reaching every frontend in
   this repo as an indistinguishable generic error.
5. **38c-ii** — NATS Object Store document upload/download, replacing the
   metadata-only `Reference` field's meaning, plus the dedicated HTTP
   ingress. Depends on 38c-i's document ID, which is the object name's
   `{documentID}` token. Touches no schema. Completes 38d-i's Documents tab,
   so it carries a small frontend tail.
   *As built* (2026-08-20, BR-TP40–BR-TP45): it **did** touch the schema after
   all — five nullable `file_*` columns on `compliance_documents`, since
   BR-TP45 projects file metadata rather than reading it back from the bucket,
   which keeps the Documents tab's listing path off the object store entirely.
   `Reference` was left alone rather than repurposed: BR-TP36 already pinned it
   to the document ID, and a document's file is a separate, optional thing.
   The ingress's auth turned out to be a real design hole rather than wiring
   (see the disclaimer at the top of this doc), and `nginx.conf` needed
   `client_max_body_size 10m` — nginx's 1 MiB default would have answered 413
   before the service was ever consulted.
6. **38d-ii** — Operating Areas + Tracking Credentials. Split out because
   **neither has any backend** (verified 2026-08-20): no `OperatingArea`
   persistence, no region corpus, no `organizations-secrets` KV command.
   Each is a new data section needing persistence. The split was also made
   to stop open question 1's unsourced region list gating the rest of the
   Transporter UI; **that question is now closed** (corpus sourced from the
   live V2 database, seed in "Operating Areas — region seed" above), so
   38d-ii is unblocked — the split stands on its own scope grounds.
7. **38e** — `organizations` rename (service, packages, subjects, UI
   labels) across the whole trading-partner surface (Shipper included,
   since the service-level rename affects both aggregates even though only
   Transporter changed shape).

> **38c split into 38c-i and 38c-ii (2026-08-20).** Once question 3 put
> `partner-update` + `version` into 38c, that one sub-phase carried two
> unrelated concerns: a Postgres/RPC schema-and-editing pass and a
> binary-transport/Object Store pass, each wanting its own business-rules
> pass and its own red→green cycle. The split also answers this section's
> own open framing above ("lettered sub-phases, or separate phase numbers")
> in practice: letters stay under one phase number, and a letter subdivides
> with a roman numeral when scope demands it rather than being promoted.
>
> **Order is 38a → 38b → 38c-i → 38d-i → 38c-ii → 38d-ii → 38e (decided
> 2026-08-20).**
> The list above is in that order, so **the letters are not alphabetical** —
> `38c-ii` sits after `38d-i` on purpose. Labels were left stable rather than
> reshuffled, since ADR-048, ADR-049 and the plan already cite them; read
> the list order, not the letters. 38d-i moved ahead because after the split
> it depends only on 38c-i and nothing in the blob path, so running it
> earlier makes the vetting lifecycle demonstrable sooner and exercises the
> 38a/38b/38c-i API surface while that code is fresh. The accepted cost is
> one deliberately incomplete tab (Documents, metadata-only) plus a small
> frontend tail in 38c-ii.

## Open questions

Both security- and scope-relevant questions this doc previously flagged
are now resolved: Tracking Credentials use an at-rest-encrypted NATS KV
bucket, never the JetStream event log (confirmed divergence from V2's
plaintext columns); Rate Sheets are a stub/placeholder tab for this phase
(confirmed, matching V2's real Customer-owned shape rather than building a
Transporter-owned table with no V2 analog).

> **Status after 38a/38b (2026-08-20).** Questions 4 and 5 below are no
> longer just resolved on paper — they are **built and tested**: the
> subject-guard shape ships as BR-TP20, and the maintained-invariant option
> ships as the `TransporterGitMonitorWorkflow` Schedule plus
> `HandleGitStatusDrop` (BR-TP28). Question 3 (`partner-update` + `version`)
> is **now resolved as well** — it lands in 38c-i with `Register` widened,
> see that question below; only its conflict-*presentation* half stays open,
> as a 38d-i decision. Question 6's `compliance_documents` PK widening is
> 38c-i work too, untouched so far — so 38c-i carries both `tradingpartner`
> schema changes in one migration pass, and 38c-ii (the Object Store /
> binary path, split out on the same date) touches no schema at all.

What's left:

1. **Operating Areas region list sourcing — RESOLVED 2026-08-20.** Sourced
   from the live V2 database (see "V2 database verification" above), not
   invented: `region_entity` holds the real corpus, and the answer to "SA
   provinces or something more granular?" is **the 9 provinces** — nothing
   more granular exists in the operating-areas data at all (`town_entity`
   is unrelated to it). All 9 are heavily used: 2,366–2,720 transporters
   each, out of ~2,886 SA transporters. The concrete seed, its scope, and
   the three data-quality decisions it forces are in
   "Operating Areas — region seed" above. 38d-ii is unblocked.
2. **Sub-phase vs. separate phase numbers** — captured as a sequencing
   decision above; whichever way, this is deliberately sized larger than
   this repo's usual single-phase scope and should not be attempted as one
   undifferentiated block of work.
3. **`TradingPartner` optimistic-lock scope — RESOLVED 2026-08-20.**
   Reframed by [ADR-049](ADR-049-cross-aggregate-concurrency.md) finding 7
   from "38a or a follow-up?" to "where does the `partner-update` command
   land?", since the lock is inseparable from a command that did not exist
   at all (see "Concurrency").

   **Editing Company Information was confirmed as a hard requirement**, so
   the read-only-in-38d-i fallback this question offered is **withdrawn**.
   Both `partner-update` and the `version` column land in **38c-i**,
   together with a **widening of `Register`** to accept
   `companyName`/`registrationNo`/`vatRegistrationNo`/`tradingAs`.

   Two reasons for 38c-i over 38d-i. First, 38c-i already owns a
   `tradingpartner` migration (the `compliance_documents` PK), so the
   `version` column rides the same migration pass instead of adding a
   second one; 38d-i then stays frontend-plus-one-endpoint. (This
   pairing is in fact why 38c split: 38c-i is the schema-and-editing pass,
   38c-ii the blob path.) Second, widening `Register`
   — rather than having 38d-i's wizard register-then-update — avoids
   introducing exactly the half-commit shape finding 6 warns about, in the
   sub-phase whose own principle is that save boundaries align to the
   aggregate boundary.

   Note the starting point is emptier than "add a column" suggests:
   `registerRequest` accepts only `{Name, Type}`, and `company_name`,
   `registration_no`, `vat_registration_no` and `trading_as` are columns
   **no code path writes a non-empty value into today**. So this is a new
   domain method, repository method, `api.*` handler (the 15th — the
   "advertises all 14 api.* endpoints" spec changes with it), migration, and
   409 path, under new BR-TP29+ rules. **Still open, deliberately:** how the
   conflict is *presented* (dialog vs. inline, and what recovery it offers)
   — a 38d-i decision, not a 38c-i one.
4. **Subject-guard shape for `TransporterProfile` — resolved.**
   `Nats-Expected-Last-Subject-Sequence-Subject`, confirmed present and
   client-supported in the pinned `nats.go v1.52.0`
   (`jetstream.ExpectedLastSubjSeqSubjHeader`) and confirmed server-side
   (`nats-server v2.14.5`'s own tests exercise exactly this
   wildcard-filter-tracks-combined-last-sequence behavior, e.g. `kv.1.*`
   tracking `kv.1.foo`/`kv.1.bar`/`kv.1.baz` together). See "Concurrency."
   No divergence from the platform subject taxonomy needed; the
   one-subject-per-aggregate fallback is dropped.
5. **Activation gate vs. maintained invariant — resolved.** Option (c): a
   `TransporterGitMonitorWorkflow`, a Temporal cron workflow started once
   `TransporterVettingWorkflow` reaches `Vetted`, periodically re-checks
   `GitStatus` and calls `TradingPartner.Suspend()` (existing BR-TP04
   operation, unchanged) if it's no longer `ACTIVE`. Subsumes option (b) —
   the same check catches both a saga-side revocation and time-derived
   `EXPIRED` — so no separate `evt.*` consumer is needed. See
   "Cross-aggregate invariant / saga."
6. **Multi-document GIT status vs. `compliance_documents` primary key —
   resolved.** The key gains a service-minted document ID —
   `(trading_partner_id, id)`, `type` becomes non-unique, `document-add`
   becomes an insert rather than an upsert, and superseding a document is an
   explicit transition rather than a silent overwrite. See "Data sections"
   (GIT Certificate row) and "Document storage." This document ID is the
   same one used in the Object Store object name
   (`{context}.transporter.{id}.{docType}.{documentID}`), so 38c-ii depends
   on it — which is why 38c-i mints it, ahead of the blob path that consumes
   it. One piece of new schema serves both.
7. **ADR-046's "zero changes to `tradingpartner`" — corrected, not
   retracted.** Both changes found by items 3 and 6 above are recorded as a
   Correction note in [ADR-046](ADR-046-transporter-aggregate-split.md) and
   in this doc's "Decision" section. The decision still holds and is still
   better than Option A on this axis — additive changes to a tested
   aggregate beat Option A's *subtractive* "retire
   `PartnerTypeTransporter`" — the original claim was imprecise, not wrong
   in substance.

## Outcomes — pattern-cards contribution (future deliverable)

Once 38a–38e ship, this phase produces at least three comparison points
worth a pattern card each, in the style of `obsidian/Event sourcing/Event
Sourcing + CQRS + NATS — Pattern Cards.pdf`:

1. **Plain CRUD vs. event-sourced, same conceptual domain, same service,
   now genuinely one aggregate per model** (`TradingPartner` vs.
   `TransporterProfile`) — a cleaner natural experiment than the original
   Shape A/B/C comparison, and cleaner still than this doc's own first
   revision (which put both models under one duplicated-identity
   aggregate): here the CRUD side is shared by *both* Shipper and
   Transporter, and only the event-sourced side is Transporter-specific,
   so the comparison isn't confounded by "is this aggregate different
   because of the party type or because of the consistency model."
2. **Temporal-orchestrated saga with real compensating transactions** —
   first genuine saga in this repo; findings on whether the
   compensation design held up under the durability test.
3. **NATS Object Store for document storage** — first use of Object Store
   in this repo (KV and JetStream streams are already well-trodden; Object
   Store is not).
4. **Guarded vs. unguarded state transitions, same real-world domain** — V2
   (the production reference this design is modeled on) has no enforced
   transition guard between its 4 vetting states; this phase's Temporal
   state machine does. Worth reporting whether the guard caught anything
   V2's unguarded dropdown would have let through during testing.
5. **A genuine cross-aggregate invariant spanning two consistency models**
   (`TradingPartner`, plain CRUD, vs. `TransporterProfile`, event-sourced)
   — see "Cross-aggregate invariant / saga" layer 2. Worth reporting
   whether guarding this at the command-handling boundary (rather than a
   database-level constraint, which is impossible here since they're
   different stores/consistency models entirely) held up, and whether the
   two-step registration's partial-failure handling
   (`CreateTransporterProfile`/`EnsureTransporterProfile`) was ever
   actually exercised in practice.

Not drafted now — this doc is the design, the pattern cards are the
retrospective once there's something to report on.
