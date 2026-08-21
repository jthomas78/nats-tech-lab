# Business Rules — Organizations Service (`backend/organizations-service/`)

> The `BR-TP*` identifiers are deliberately retained because ADR-046,
> ADR-048, ADR-049, the architecture docs, the plan, and code comments cite
> them. Renaming the prefix would invalidate those established references.
>
> Split out of `BUSINESS_RULES.md` to keep per-domain reads small. See that
> file's index for the Shipping (BR-001–BR-033), Reference Data
> (BR-D01–BR-D34), Accounts (BR-AC01–BR-AC13), and Pricing (BR-P01–BR-P24)
> domain rules.

**BR-TP01–BR-TP14 confirmed 2026-08-13** (Phase 26, IMPLEMENTED end to end —
[Main-POC-Plan.md](../../.claude/plans/Main-POC-Plan.md)). Covers 26a (the
`Organization` aggregate's registration/lifecycle), 26a1 (its audit
trail), 26b (compliance documents), and 26c (Transporter fleet assets),
26d (Postgres/REST/tenant-NATS wiring), and 26e (Admin UI) — all
live-verified against the real composed stack, including in-browser.
A separate service, separate legacy-named Postgres database
(`trading_partner`) with its application schema at `organizations`, no datastore shared with `shipping-service`,
`refdata-service`, `accounts-service`, or `pricing-service` — see
`tenant_service_separation_decision.md`. Plain Postgres CRUD (not
event-sourced) — see `ARCHITECTURE.md` § "Event Sourcing vs Plain CRUD" and
the Phase 26 plan section's own per-entity CQRS classification.

### BR-TP01–BR-TP06 — Organization registration and lifecycle

- **BR-TP01:** An `Organization`'s `type` (`SHIPPER` | `TRANSPORTER`) is
  required at registration and immutable thereafter — there is no "convert a
  Shipper into a Transporter" operation. Mirrors V2's `BusinessType`
  discriminator (`linebooker_shipper_vs_customer_naming.md`,
  `v3_tenancy_axes_decision.md`).
- **BR-TP02 (`Register`):** Creating an `Organization` always lands in
  `Registered` status — this is creation, not a transition, so it has no
  "illegal from" case the way `Activate`/`Suspend`/`Reactivate` do below.
  **Confirmed 2026-08-13:** only `name`, `type`, and `context` are required
  at `Register` time; `tradingAs`, `companyName`, `registrationNo`, and
  `vatRegistrationNo` are all optional, fillable incrementally as KYC/vetting
  proceeds — matching a real onboarding flow where an operator starts a
  record before every detail is confirmed.
- **BR-TP03 (`Activate`):** Legal only from `Registered` → `Active`. Called
  on an `Organization` in any other status (`Active` or `Suspended`), it is
  rejected with `409 Conflict` — mirrors `reactivateAccount`'s guard shape in
  `accounts-service/accounts/handler.go`.
- **BR-TP04 (`Suspend`):** Legal only from `Active` → `Suspended`, and
  **requires a non-empty `reason`** — rejected at the domain boundary (not
  just a REST-layer check) if `reason` is empty. Called on a
  `Organization` in any other status (`Registered` or `Suspended`), it is
  rejected with `409 Conflict`. **v1 has no enforcement consumer for this
  status** — nothing in this POC yet refuses a `Suspended` partner's bids or
  loads; the eventual consumer is the marketplace/tender phase (see
  `linebooker_bid_tender_allocation_rules.md`). What `Suspend` delivers today
  is the guarded state machine plus the audit trail (BR-TP06), not an
  enforced boundary.
- **BR-TP05 (`Reactivate`):** Legal only from `Suspended` → `Active`. Called
  on an `Organization` in any other status (`Registered` or `Active`), it
  is rejected with `409 Conflict` — completes the
  `Register`→`Activate`→`Suspend`→`Reactivate` lifecycle, mirroring
  accounts-service's create/suspend/reactivate triple (BR-AC08–AC10).
  There is no further terminal/offboarding state in v1 (explicit non-goal —
  see the Phase 26 plan section's retention rationale, mirroring BR-AC03).
- **BR-TP06:** Every lifecycle state change — register, activate, suspend,
  reactivate — records an immutable row in `organizations.audit_events`:
  action, partner id, actor, source IP, an outcome of `success`/`failed`,
  and a JSONB metadata payload (for `Suspend`, `reason` lands here). The
  table is append-only (no `UPDATE`, no `DELETE`) — this is the
  substantive counterweight to BR-TP04's "no enforcement consumer": the
  durable record of who suspended whom, when, and why is the actual
  deliverable of the lifecycle for v1. **Reuses BR-AC11's conventions
  verbatim, not reinvented:** actor is a placeholder until WorkOS-backed
  human auth lands (shared basic-auth username, overridable per request via
  an `X-Actor` header); audit writes are best-effort — a failed insert is
  logged but never blocks or rolls back the lifecycle operation it
  describes; a request that fails validation before any mutation is
  attempted (e.g. BR-TP03/04/05's `409 Conflict`, BR-TP04's empty-`reason`
  rejection) writes nothing, since there is no partial state yet worth
  recording.

### BR-TP07–BR-TP11 — Compliance documents (26b)

> **Superseded in part by BR-TP29–BR-TP31 (Phase 38c-i, 2026-08-20).**
> BR-TP08's "at most one per `(partner, type)`" **upsert** invariant is
> replaced by a service-minted document `id`, an insert-always
> `AddDocument`, and an explicit `SUPERSEDED` transition; BR-TP09–BR-TP11
> now address a document by `id` rather than by `type`. BR-TP07's per-type
> vocabulary rules are unaffected. Read those rules alongside this section.

**Confirmed 2026-08-13.** Document storage is metadata-only (no file
bytes) — `Reference` is an opaque external locator, unvalidated in v1 (Phase
26 plan section's storage decision). `ExpiresAt`/`CoverageCents` are both
nullable, freely settable on any document with **no domain-level
enforcement** — in particular, `CoverageCents` is *conventionally* only
meaningful on a `GOODS_IN_TRANSIT` document (V2's insurance-coverage field
lived on the document, not the profile), but v1 deliberately does not reject
setting it on another type. `ExpiresAt` is stored but nothing reads it yet
(the expiry-driven-status exploration stays a named deferred item — see the
Phase 26 plan section).

- **BR-TP07:** A `ComplianceDocument`'s `type` must be one of `CIPC`,
  `DIRECTOR_ID`, `BANK_CONFIRMATION_LETTER`, `TERMS_AND_CONDITIONS` (valid
  for both `SHIPPER` and `TRANSPORTER`) or `GOODS_IN_TRANSIT` (valid **only**
  for `TRANSPORTER` — rejected for `SHIPPER`). Any other value is rejected.
- **BR-TP08 (`AddDocument`):** Adding a document requires a non-empty
  `reference` and a `type` valid per BR-TP07 for the partner's `type`;
  always creates the document in `Pending` status. **Repository-level
  invariant, deferred to 26d (not a pure-domain spec — same scoping
  treatment as BR-TP06):** at most one `ComplianceDocument` exists per
  `(Organization, type)` — adding a document for a type that already
  exists **upserts** (replaces the existing row and resets it to `Pending`,
  since new content always needs fresh review), enforced via a Postgres
  unique constraint (mirrors BR-P01/BR-AC01's uniqueness-via-DB-index
  pattern), not a pure-domain guard function.
- **BR-TP09 (`Approve`):** Legal only from `Pending` → `Approved`. Called on
  a document in any other status (`Approved` or `Rejected`), rejected with
  `409 Conflict`.
- **BR-TP10 (`Reject`):** Legal only from `Pending` → `Rejected`. Called on
  a document in any other status (`Approved` or `Rejected`), rejected with
  `409 Conflict`.
- **BR-TP11 (`Resubmit`):** Legal only from `Rejected` → `Pending`,
  confirmed 2026-08-13 (resubmission is in scope for v1, not deferred).
  Called on a document in any other status (`Pending` or `Approved`),
  rejected with `409 Conflict`. There is no `Approved` → anything transition
  in v1 — an approved document is not un-approved or re-reviewed once
  decided. *(Amended by BR-TP30: `Approved` → `SUPERSEDED` is legal from
  38c-i. That is not an un-approval — the approval stands over the record it
  was given for; supersession retires that record in favour of a newer
  document, which starts its own review at `Pending`.)*

Document status remains fully independent of the parent
`Organization.status` in v1, per BR-TP04's note and the Phase 26 plan
section's "Deferred: document-driven status" item — nothing here gates or
is gated by `Activate`/`Suspend`/`Reactivate`.

### BR-TP12–BR-TP14 — Transporter fleet assets (26c)

**Confirmed 2026-08-13.** Fleet assets are a trimmed `FleetAssetEntity`
(`registrationNo`, `vin`, `make`, `model`, `vehicleTypeCode`) —
`subcontractingOwner` stays out of scope regardless of anything else decided
here (settled earlier, not reopened).

- **BR-TP12:** A `FleetAsset` may only be attached to an `Organization`
  whose `type` is `TRANSPORTER` — adding one for a `SHIPPER` is rejected.
  Mirrors BR-TP07's per-type restriction pattern for `GOODS_IN_TRANSIT`.
- **BR-TP13 (`AddFleetAsset`):** Requires a non-empty `registrationNo` and a
  non-empty `vehicleTypeCode`; `vin`/`make`/`model` stay free text and
  optional (they identify the specific truck, not a vocabulary — no
  refdata corpus governs them). **Repository-level invariant, deferred to
  26d (not a pure-domain spec, same treatment as BR-TP08's one-per-type
  invariant):** `registrationNo` is unique — no two `FleetAsset`s (even
  across different Transporters) may share one, since a real vehicle
  registration identifies exactly one physical truck. Enforced via a
  Postgres unique constraint, not a pure-domain guard function.
- **BR-TP14 (deferred to 26d — not a 26c domain-layer spec):**
  `vehicleTypeCode` must exist in refdata-service's `vehicle-type` corpus;
  an unknown code is rejected. **This cannot be a pure `internal/domain`
  Ginkgo spec** — validating existence requires calling refdata-service,
  which (per BR-D28, no REST fallback for backend-to-backend calls) means a
  tenant-scoped `rpc.*` client, mirroring `shipping-service`'s own
  `internal/refdataconsumer` package. Just as shipping-service's refdata
  existence checks are tested at the consumer/adapter layer (see
  `refdataconsumer/consumer_test.go`) and not abstracted behind a domain
  port with a fake, BR-TP14's specs land with 26d's adapter, not here.
  Requires the `vehicle-type` corpus to be seeded (run
  `refdata-service/cmd/seed-vehicle-types` against the composed stack, or
  equivalent) before it can be exercised live.

---

**26a status (2026-08-13): IMPLEMENTED.** Rules live in
`organizations-service/organizations/internal/domain/organizations.go`;
specs in `organizations-service/organizations/organization_test.go`
cover every cell of the BR-TP03–BR-TP05 transition matrix plus
BR-TP01/BR-TP02/BR-TP04's field/reason validation. BR-TP06 (audit trail) is
not yet implemented — its append-only Postgres adapter lands with 26d and
is verified live, not by a domain-layer spec (see the Phase 26 plan
section's scoping note, mirroring accounts-service's own untested
`AuditLog`).

**26b status (2026-08-13): IMPLEMENTED.** Rules live in
`internal/domain/compliance_document.go`; specs in
`compliance_document_test.go` cover BR-TP07's partner-type-restricted
vocabulary, BR-TP08's reference/type validation, and every cell of the
BR-TP09–BR-TP11 document-status transition matrix. Not yet implemented
(26d/26e): Postgres persistence (including the repository-level
one-per-`(partner, type)` upsert noted under BR-TP08) and the Admin UI
surface.

**26c status (2026-08-13): IMPLEMENTED (domain shape only).** Rules live in
`internal/domain/fleet_asset.go`; specs in `fleet_asset_test.go` cover
BR-TP12's Transporter-only guard and BR-TP13's required-field validation,
plus an explicit spec confirming BR-TP14 (refdata corpus existence) is
*not* checked at this layer. Combined with 26a/26b: 37/37 specs green
(`ginkgo ./...`), `go build`/`go vet`/`gofmt -l` clean.

**26d status (2026-08-13): IMPLEMENTED, live-verified.** Postgres schema +
repository adapters (`internal/postgres/`), application layer
(`internal/application/commands/`), REST API
(`internal/rest/handlers.go`), `internal/tenants` (per-tenant NATS
connections) + `internal/refdataclient` (BR-TP14's `rpc.*` client),
`cmd/main.go`, Dockerfile, docker-compose entry (5436/7204), nginx route.
BR-TP14 is now implemented end-to-end: live-verified against a real
`refdata-service` — a bogus `vehicleTypeCode` rejected, a real one
(`TAUTLINER`, from the `vehicle-type` corpus seeded via
`refdata-service/cmd/seed-vehicle-types`) accepted, over the `acme` tenant's
own NATS connection. BR-TP13's `registrationNo` uniqueness and the full
transition-matrix `409` guards were also confirmed live, along with all
four audit actions (BR-TP06) landing correctly in Postgres.

**26e status (2026-08-13): IMPLEMENTED, live-verified in-browser.** New
"Organizations" nav category in `frontend/admin` (own eyebrow, per
`linebooker_registration_ui_placement.md` — not folded into Accounts or
RefData), `OrganizationsPanel.vue`: register dialog, list table with a
row menu (Activate/Suspend-with-reason/Reactivate/Add Document/Add Fleet
Asset), and a row expansion showing Compliance Documents, Fleet Assets
(Transporter only), and the Audit Trail. Verified end to end via the
Browser pane against the real composed stack: registered a Transporter,
expanded the row, hit BR-TP14's live refdata validation both ways (a bogus
code rejected, a real one accepted after seeding the corpus into the
partner's own context), activated, and suspended with a reason — all three
resulting audit rows rendered correctly, reason included. `npm run build`
clean; `vitest run`'s one failure is pre-existing and unrelated (confirmed
via `git stash`).

**26e nav follow-up (2026-08-13): split per role.** The single "Registration"
screen became two — **Shippers** and **Transporters** — under a "Trading
partners" eyebrow inside a new collapsible **PLATFORM** group in the admin
sidebar (`shared/ui-shell/NavList.vue`'s `{ group, sections }` shape; see
`shared/unifi-theme/LAYOUT.md`). Both roles are still one aggregate with a
type discriminator server-side (BR-TP01) — the split is presentational, so
`OrganizationsPanel.vue` is parameterized by a `partnerType` prop and
mounted twice rather than duplicated. Consequences: the register dialog no
longer has a Type field (the panel's own role supplies it), the list has no
Type column, and the list is filtered client-side because
`GET /api/organizations/{context}` still takes no `type` query param —
revisit if that list ever paginates server-side. No BR-TP rule changed.

**26g status (2026-08-13): IMPLEMENTED, live-verified.** `internal/browserrpc`
registers the service via `micro.AddService` on each tenant connection
(`Name: "organizations-service"`, `Version: 1.0.0`, `Metadata{"tenant": …}`),
making it discoverable in the Admin UI's Services panel — it had been absent
despite running, because an outbound-only `rpc.*` requester answers nothing on
`$SRV`. **Zero `api.*` endpoints are registered:** REST remains the live
inbound transport, and no BR-TP rule changed. 6 new specs
(`organizations/browserrpc_test.go`, embedded NATS) assert discoverability
over a real `$SRV.PING` broadcast; 43 total green.

**26h status (2026-08-13): IMPLEMENTED, live-verified in-browser.** The Admin
UI now reaches this service over
`api.{context}.organizations.{entity}.{action}.v1` (14 endpoints, 6 tokens
each) instead of REST — **`api.*`, not `rpc.*`**, because `rpc.*` is
service-to-service and "a browser credential is never granted `rpc.>`"
(CLAUDE.md; `ARCHITECTURE-COMMUNICATIONS.md` § 2.4). `rpc.*` endpoints wait for
a real backend caller (the marketplace/tender phase). **REST stays wired and
serving the same operations** — dual transport, matching pricing-service — so
`curl` against port 7204 remains the debugging path; the browser simply no
longer takes it.

No BR-TP rule changed, but the transport tightens two of them:

- **BR-TP14's tenant is no longer client-supplied.** REST's
  `fleetAssetRequest` carries a `tenant` field because HTTP had no tenant
  identity to work from. Over NATS the tenant *is* the account the connection
  authenticated into, and the adapter (one per tenant connection) reads it from
  there — a body-supplied `tenant` is ignored. `api.js`'s `addFleetAsset` keeps
  its `tenant` parameter only so the panel's call site is unchanged; the value
  is deliberately unused.
- **Context cannot be spoofed via the body.** Every handler derives
  `{context}` from the subject (`contextFromSubject`), never from a request
  field, matching pricing/shipping's adapters.

**BR-TP06 actor over NATS:** the audit `actor` comes from an optional `X-Actor`
NATS header over the same `"admin"` placeholder REST uses — deliberately the
same (low) trust level, not a new claim of authenticated identity.
`source_ip` has no NATS equivalent (`micro.Request` exposes no client
address), so it records the caller's `Nats-Requestor` identity prefixed with
`nats:` — e.g. `nats:admin-tenant/50439daa0ae847f7` — which also makes a row's
originating transport self-evident next to REST's `192.168.65.1:35243`.

**No credential change was required**, contrary to this phase's original
scoping: the Admin UI's *tenant* connection already carries
`Pub.Allow = ["api.>", "_INBOX.>"]` from the same `MintBrowserToken`
seafreight uses. Only the PLATFORM connection is publish-denied, and it isn't
the one used here. See Phase 26h in `.claude/plans/Main-POC-Plan.md` for the
correction.

**Phase 26 is now fully implemented (26a-26e) and live-verified**, closing
out `.claude/plans/Main-POC-Plan.md`'s Phase 26 checklist. Remaining named
open items (all deliberately deferred, not gaps): lifecycle-as-CQRS/temporal
exploration, `ComplianceDocument`'s temporal classification,
document-expiry-driven status, real file storage, terminal/offboarding
state, platform-identity vs tenant-membership split, and `notify.*`
publication once a marketplace consumer exists.

### BR-TP15 (Phase 28) — The same `obs.trace.*` wire contract as `BUSINESS_RULES-SHIPPING.md`'s BR-036, on organizations-service's publisher side

Mirrors `BUSINESS_RULES-SHIPPING.md`'s BR-036 for this service's own tracing publisher — prototyped here first (Phase 28a), since this service already has `observe`/`reply`/`actor` helpers and no JetStream, before being copied to pricing, shipping, and refdata (Phase 28b). `browserrpc.Adapter`'s `traceSpan` is a strict superset of its existing `obsEnvelope` — no field renamed or retyped, every addition (`traceId`, `spanId`, `parentSpanId`, `service`/`entity`/`action`, `statusCode`/`statusMessage`, `attributes`, `redacted`, `truncated`) `omitempty` — and every `obs.trace.{context}.organizations.{entity}.{action}` publish goes to the PLATFORM account only, with the same redact-before-truncate ordering and 4 KiB cap BR-036 establishes. Never blocks or fails a business path.

- **Enforced in:** `organizations/internal/natstrace` (new package, Phase 28a) — the prototype `Tracer.publish()` redaction-then-truncate ordering and `traceSpan` struct that Phase 28b's clones mirror field-for-field; the `AddEndpoint` decorator that starts a span per request without a hand-pasted `publishObs` call at each of the 14 handler sites.
- **Test:** `organizations/internal/natstrace/natstrace_test.go` — the shared cross-service contract test (BR-036's clone) asserting the `traceSpan` JSON shape, and that an old-shape `obsEnvelope` still decodes; `browserrpc_roundtrip_test.go`'s `obs.*` side-channel context gains a decoding assertion (the existing test only checks the raw subject string, not the envelope shape).

### BR-TP16 (Phase 33.5) — Business operations are reachable only over `api.*`/`rpc.*`; REST reduces to infra health

All 14 `/api/organizations/{context}/...` REST routes (Organization registration/lifecycle/audit, ComplianceDocument review, FleetAsset registration) are deleted now that `internal/browserrpc`'s `api.*` adapter (Phase 26h) has full 1:1 parity with them. Nothing outside organizations-service ever called them: `frontend/admin`'s `OrganizationsPanel.vue` already talks to organizations-service exclusively over `api.*` (`api.js`'s `tpRequest` helper, predating this phase). REST's only remaining surface is `GET /healthz`, mirroring the convention `dictionary/internal/rest` and `pricing/internal/rest` already established (`BUSINESS_RULES-PRICING.md`'s BR-P26). organizations-service has no admin-only or BasicAuth-gated REST route distinct from its business CRUD to carve out an exception for — every one of the 14 deleted routes was tenant-facing business data (even the ones the whole-mux `BasicAuth` wrapper gated), so there is no third "admin REST" category here, only the two: infra (`/healthz`) and business (`api.*`/`rpc.*`). The now-unused `BasicAuth`/`auditActor` helpers (`internal/rest/middleware.go`) and the `ORGANIZATIONS_AUTH_SECRET` env var were removed with the routes they gated, rather than left wired to nothing.

- **Enforced in:** `organizations/internal/rest/handlers.go` (now just `Mount()` registering `/healthz`); `organizations/composition.go`'s `Handlers.Mount(mux)` no longer takes command-handler dependencies or an auth secret since the REST layer has none left to wire; `cmd/main.go` serves the mux directly, unauthenticated, instead of wrapping it in a BasicAuth gate that no longer protects anything.
- **Test:** N/A — this is a route-deletion/transport-contract rule, not a domain rule; correctness is covered by `go build ./...` compiling cleanly with `internal/rest` down to zero business handlers, and the full `ginkgo ./...` suite staying green since `api.*`/`rpc.*` and the domain layer are untouched.

### BR-TP17 (Phase 34) — This service's mirror of `BUSINESS_RULES-SHIPPING.md`'s BR-040 mux allowlist rule

`organizations/internal/rest/handlers.go`'s package-level `Mount(mux)`
returns `[]string` — exactly `["GET /healthz"]`, the one route BR-TP16 left
standing.

- **Enforced in:** `organizations/internal/rest/handlers.go`'s `Mount`.
- **Test:** `organizations/internal/rest/handlers_allowlist_test.go` —
  `TestMountRoutesMatchAdminAllowlist` asserts `Mount(mux)`'s returned route
  list `ConsistOf("GET /healthz")`.

### BR-TP18 (Phase 38a) — CreateTransporterProfile / EnsureTransporterProfile

A `TransporterProfile` uses its associated `Organization` ID as its
aggregate ID. No surrogate ID or join record is created. Creation begins in
`AwaitingDocumentation`. Both `CreateTransporterProfile(organizationID)`
and `EnsureTransporterProfile(organizationID)` are idempotent: if the
profile does not exist, exactly one creation event is appended; if it already
exists, the existing profile is returned without appending another creation
event or resetting its state. Concurrent creation attempts may produce a
sequence conflict internally, but after re-hydration converge on the same
single profile.

- **Enforced in:** `transporterprofile/domain` (shared-ID aggregate and
  `AwaitingDocumentation` initial state) and `transporterprofile/orchestration`
  (`CreateTransporterProfile` / `EnsureTransporterProfile` hydration and
  idempotent conflict convergence).
- **Test:** `transporterprofile/orchestration/orchestration_test.go` — the
  BR-TP18 context covers shared identity, initial state, repeated calls, and
  concurrent creation convergence with one creation event.

### BR-TP19 (Phase 38a) — Transporter activation gate

At the command-handling/orchestration boundary, activating a
`TRANSPORTER`-typed `Organization` is permitted only when the associated
`TransporterProfile` exists and its projected status is `Vetted`. A missing
or non-`Vetted` profile rejects activation without changing the partner's
`Registered` status. The check reads the canonical Postgres projection
directly, never the KV cache. A `SHIPPER` bypasses this check and retains
BR-TP03's existing behavior unchanged.

- **Enforced in:** `transporterprofile/orchestration`'s activation handler,
  wired only at `organizations/internal/browserrpc`'s live
  `partner.activate` boundary; `organizations/internal/domain` remains
  unchanged.
- **Test:** `transporterprofile/orchestration/orchestration_test.go` — the
  BR-TP19 context covers missing, non-Vetted, Vetted, and SHIPPER paths and
  asserts the canonical projection reader is used instead of KV.

### BR-TP20 (Phase 38a) — Aggregate-wide optimistic sequence guard

Every `TransporterProfile` event publish uses the last sequence observed
while hydrating that aggregate and guards the wildcard filter
`evt.{context}.organizations.transporter.{organizationID}.>`. Publishing
must use `Nats-Expected-Last-Subject-Sequence-Subject` with that wildcard,
never the plain per-leaf-subject option alone. If any event type has advanced
the aggregate since hydration, JetStream rejects the append as a sequence
conflict; no event or projection mutation is produced by the losing command.

- **Enforced in:** `transporterprofile/orchestration`'s JetStream event store;
  every append carries the hydrated aggregate sequence plus its wildcard
  filter.
- **Test:** `transporterprofile/orchestration/orchestration_test.go` — the
  BR-TP20 context verifies the exact headers and proves a different event leaf
  causes the stale append to lose without producing another event or
  projection mutation.

### BR-TP21 (Phase 38b) — Both vetting branches gate Vetted and fleet availability

`TransporterVettingWorkflow` runs document review and GIT verification as
parallel branches. A `TransporterProfile` reaches `Vetted` and its aggregate-
owned `FleetAvailabilityGate` flips from its default `false` to `true` only
after both branches succeed; either branch succeeding alone leaves both
outcomes unavailable. The legacy per-asset `FleetAsset` rows remain owned by
`organizations/internal/domain` and are not changed by this workflow. A
fleet asset's `AvailableForAssignment` is a computed query/read-layer value
produced by joining those untouched rows with `FleetAvailabilityGate`; it is
not a column written by `TransporterProfile` and is not a field added to the
legacy `FleetAsset` domain type.

- **Enforced in:** `transporterprofile/workflow` (parallel AND-gate),
  `transporterprofile/domain` (`FleetAvailabilityGate` state), and the
  transporter-profile query/read join.
- **Test:** `transporterprofile/workflow/workflow_test.go` — the BR-TP21
  context proves neither branch can vet or open the gate alone, and both
  successful branches can.

### BR-TP22 (Phase 38b) — Saga failure appends compensation events

If GIT verification fails or times out after document approvals, the workflow
appends `DocumentApprovalReverted`, projects those approvals back to
`PendingReview`, leaves `FleetAvailabilityGate == false`, and ends the attempt
as `Rejected`. If fleet availability had previously taken effect and is later
invalidated, compensation appends `FleetAvailabilityRevoked`, which flips the
aggregate-owned gate to `false`. Compensation is always a new forward event;
it never deletes or rewrites the event history and never mutates legacy
`FleetAsset` rows. GIT-branch success alone has no compensable side effect,
since `FleetAvailabilityGate`/`Vetted` require both branches to succeed before
taking effect. An optimistic sequence conflict is a retryable "try again"
outcome and must not enter the compensation path.

- **Enforced in:** `transporterprofile/workflow`,
  `transporterprofile/activities`, and `transporterprofile/domain`'s
  hydrate/apply handling for the two explicitly-named compensation events.
- **Test:** `transporterprofile/workflow/workflow_test.go` — the BR-TP22
  context covers rejection, timeout, append-only compensation, GIT-success-
  alone, gate revocation, and sequence-conflict behavior.

### BR-TP23 (Phase 38b) — Document review is signal-driven and per reference

During 38b, document approval is an interim in-memory/test-only workflow
concern. `TransporterVettingWorkflow` accepts `DocumentApproved` and
`DocumentRejected` signals carrying a document reference, records each
reference once, and resumes from the accumulated signal state. A rejection
fails that vetting attempt; approvals satisfy the document branch only when
all required references for that attempt have been approved. Real document
upload, persistence, and NATS Object Store integration remain 38c scope.

- **Enforced in:** `transporterprofile/workflow`'s deterministic signal
  handling; no 38c storage adapter is introduced.
- **Test:** `transporterprofile/workflow/workflow_test.go` — the BR-TP23
  context covers per-reference approvals, duplicate signals, and rejection.

### BR-TP24 (Phase 38b) — Workflow event publication is retry-safe

Every JetStream publish triggered by a workflow runs inside a Temporal
Activity, never in workflow code. The `TRANSPORTER` stream config declares an
explicit duplicate window, and every activity publish supplies
`Nats-Msg-Id` as
`{organizationID}:{event}:{attemptNumber}:{step}`. `attemptNumber` is
ordinary `TransporterVettingWorkflow` input sourced from the aggregate's
event history; the key is never derived from Temporal's RunID. An activity
retry after a successful publish therefore receives the original acknowledgement
without appending the same transition twice.

- **Enforced in:** `transporterprofile/activities` and
  `transporterprofile/orchestration`'s stream/event-store configuration.
- **Test:** `transporterprofile/activities/activities_test.go` — the BR-TP24
  context verifies exact message IDs, the duplicate window, and one stored
  event after a simulated retry.

### BR-TP25 (Phase 38b) — GIT verification has explicit configurable timeouts

`RequestGitVerification` supports immediate pass, immediate fail, and a
hang-past-timeout test outcome. Its Temporal Activity options always declare
explicit `StartToCloseTimeout` and `ScheduleToCloseTimeout` values supplied by
worker configuration, with short test values distinct from the production-
scale defaults. Failures and exhausted timeouts reject the attempt and follow
BR-TP22's compensation behavior.

- **Enforced in:** `transporterprofile/workflow` Activity options,
  `transporterprofile/activities`' mock insurer, and
  `transporterprofile/worker` configuration.
- **Test:** `transporterprofile/workflow/workflow_test.go` — the BR-TP25
  context covers pass, fail, and timeout with the configured bounds.

### BR-TP26 (Phase 38b) — Rejected profiles resubmit under the same workflow ID

`Rejected` is terminal for one vetting attempt, not for the
`TransporterProfile`. `Resubmit` first appends `VettingResubmitted` on the
profile with the incremented monotonic `attemptNumber` (count of prior vetting
attempts + 1), then starts a fresh `TransporterVettingWorkflow` under the same
`{context}-transporter-vetting-{organizationID}` workflow ID and passes that
`attemptNumber` as ordinary workflow input. The start explicitly uses the
Temporal workflow-ID reuse policy that permits a new run after the prior
`Rejected` run has closed; it never depends on RunID for domain identity or
deduplication.

**The `VettingResubmitted` event carries `DocumentsInReview`, not the
`Rejected` status it leaves.** *Amended 2026-08-21, during 38b's completion.*
It previously carried `p.state.Status` — the status being resubmitted away
from — which left a profile with a live attempt running still reporting
`Rejected` for the whole attempt: the workflow skips its own `VettingStarted`
append when `Resubmitted` is true (see the workflow's guard), so nothing else
ever moved it.

Two things made this invisible until now, both worth recording because they
generalise:

1. **`Apply()` already hardcodes `StatusDocumentsInReview` for
   `VettingResubmittedEvent`, so the aggregate was never wrong — only the
   event payload was.** The projector does not replay `Apply`; it copies
   `Status` straight off the payload, and already carries a special case for
   `CreatedEvent` for exactly this reason. So there are two implementations of
   "what does this event mean for state", and this rule is where they
   disagreed. **A test asserting the resubmitted `State` passes with or
   without the bug** — the assertion has to be on the appended event.
2. **The path had no production caller until 38b's completion**, so it had
   only ever run in tests, which do not read the projection back.

- **Enforced in:** `transporterprofile/orchestration`'s resubmit command and
  workflow starter, plus `transporterprofile/domain`'s attempt tracking and
  `VettingResubmitted` hydrate/apply path.
- **Test:** `transporterprofile/workflow/workflow_test.go` — the BR-TP26
  context drives an attempt to `Rejected`, verifies event-before-start order
  and incremented input, and confirms a fresh run starts under the same ID.
  `transporterprofile/orchestration/orchestration_test.go` adds the amendment
  above, asserting the **appended event's** `Status` rather than the returned
  `State`, with the reason written into the spec so it is not "simplified"
  back into a vacuous state assertion later. Verified live: attempt 2 reads
  `DocumentsInReview` while Temporal shows the run `Running`.

### BR-TP27 (Phase 38b) — Worker restart preserves workflow progress

Stopping the organizations-vetting worker while a vetting workflow is in
progress and starting a replacement worker on the same task queue resumes the
same durable workflow. Already-approved document references and completed GIT
progress are retained; the replacement waits only for outstanding signals and
does not append duplicate transition events.

- **Enforced in:** Temporal workflow history plus BR-TP24's idempotent
  activity boundary; no process-local workflow state is authoritative.
- **Test:** `transporterprofile/worker/durability_test.go` — the BR-TP27
  harness stops and replaces the worker mid-workflow and completes the
  original execution without re-sending satisfied inputs.

### BR-TP28 (Phase 38b) — Scheduled GIT drops revoke availability and suspend

Once vetting reaches `Vetted`, a Temporal Schedule starts periodic
`TransporterGitMonitorWorkflow` executions (the plan's older
"CronSchedule" wording is informal/outdated). Each execution reads the
canonical TransporterProfile projection. When it detects a drop from active
GIT status, it invokes one orchestration-level command,
`HandleGitStatusDrop(organizationID)`, which atomically from the caller's
perspective (a) appends `FleetAvailabilityRevoked` on `TransporterProfile`,
flipping `FleetAvailabilityGate` to `false`, then (b) calls
`Organization.Suspend()` with the GIT-expired-or-revoked reason. Repeated
monitor executions are idempotent and do not append or suspend again once the
drop has been handled.

- **Enforced in:** `transporterprofile/workflow` monitor,
  `transporterprofile/worker` Schedule setup, and
  `transporterprofile/orchestration.HandleGitStatusDrop`.
- **Runtime wiring (added 2026-08-21, completing 38b):** the Schedule is not
  created out-of-band — the vetting workflow executes
  `ScheduleGitMonitorActivity` immediately after `VettedEvent`, so every
  transporter that reaches `Vetted` gets its monitor as part of the same
  saga. `organizations/composition.go`'s `gitMonitor` implements
  `GitStatusDrop`/`IsGitActive`/`ScheduleGitMonitor`, and
  `tenants.Manager.ProfileStore(tenant)` resolves the event store per
  tenant, so the handler is no longer bound to a single (tenant, context)
  pair at construction. `IsGitActive` reuses
  `domain.DeriveGitStatus(docs, now) == GitStatusActive` rather than
  restating what "active" means. Interval:
  `ORGANIZATIONS_GIT_MONITOR_INTERVAL` (5m in compose).
- **The schedule args must carry the tenant, not just the context.** Context
  → tenant is many-to-one, so an activity that only knows `Context` cannot
  pick the connection to publish over. `GitMonitorScheduleOptions` omitted
  `Tenant` initially and the live monitor failed with "tenant is not
  connected"; the spec now asserts `action.Args`, not only the workflow
  input.
- **Test:** `transporterprofile/workflow/workflow_test.go` and
  `transporterprofile/orchestration/orchestration_test.go` — the BR-TP28
  contexts verify Schedule-driven detection, command ordering, both effects,
  and idempotency.
- **Verified live 2026-08-21:** schedule created as
  `acme-transporter-git-monitor-<id>`; the drop path flipped
  `fleetAvailabilityGate` to false and set the organization `SUSPENDED`;
  three monitor runs all `Completed` with no second suspend.

### BR-TP29–BR-TP31 (Phase 38c-i) — Compliance documents gain an identity

**Approved 2026-08-20.** These three rules **supersede BR-TP08's
"at most one per `(partner, type)`" repository invariant** and change how
BR-TP09–BR-TP11 address a document. BR-TP08's *intent* is preserved — one
live document per type — but its mechanism (the primary key, and an upsert
that destroyed the previous row) is replaced, because 38c-ii needs a stable
per-document identifier for the Object Store object name and an upsert
leaves no history to audit.

- **BR-TP29:** Every `ComplianceDocument` carries an `id`, minted by the
  service (not supplied by the caller — same treatment as
  `Organization.ID`, a Postgres `gen_random_uuid()` default returned to
  the caller). The primary key becomes `(organization_id, id)`.
  `AddDocument` always **inserts**; it never upserts.
- **BR-TP30 (supersession):** At most one document per `(partner, type)` is
  **current**. Adding a document for a type that already has a current
  document **supersedes** that document: a new terminal `SUPERSEDED` status,
  reached by an explicit transition, legal from `Pending`, `Approved` and
  `Rejected` alike. A `SUPERSEDED` document accepts no further transition —
  `Approve`/`Reject`/`Resubmit` on one is rejected `409 Conflict`. Note this
  is the one transition that may leave `Approved`, which BR-TP11 otherwise
  forbids: superseding does not un-approve past work, it retires the record
  that approval applied to.
- **BR-TP31 (addressing):** `Approve`/`Reject`/`Resubmit` address a document
  by `id`, not by `type` — after BR-TP29 a type no longer uniquely
  identifies a row. `document.list` returns **current documents only**
  (superseded rows are retained in Postgres for audit but not returned),
  so the response shape 38b's workflow and 38d's Documents tab consume is
  unchanged by this sub-phase.

- **Enforced in:** `internal/domain/compliance_document.go` (the
  `SUPERSEDED` transition and its guards), `internal/postgres/compliance_document_repository.go`
  (insert-not-upsert, supersede-then-insert in one transaction, current-only
  reads, the ID mint), `internal/postgres/migrate.go` (the PK widening).
- **Test:** `organizations/compliance_document_test.go`'s BR-TP30 context
  covers superseding from each of the three non-terminal statuses and
  rejection of every transition on a superseded document.
  `internal/postgres/repository_test.go` covers the repository half —
  distinct IDs rather than an upsert, the superseded row surviving for audit
  while `ListDocuments` returns only the current one, and types staying
  independent of each other. **The Postgres specs are gated on
  `ORGANIZATIONS_TEST_DATABASE_URL`** and skip silently without it — see
  the README. A plain green `ginkgo ./...` does not prove them.

### BR-TP32–BR-TP35 (Phase 38c-i) — Editable Company Information

**Approved 2026-08-20**, closing the design's open question 3. Editing
Company Information is a confirmed product requirement, so the
"ship it read-only" fallback (ADR-049 finding 7) is withdrawn. Until this
sub-phase, `company_name`, `registration_no`, `vat_registration_no` and
`trading_as` were columns **no code path wrote a non-empty value into**.

- **BR-TP32 (`UpdateDetails`):** Mutates `name`, `tradingAs`,
  `companyName`, `registrationNo`, `vatRegistrationNo`. `type` and
  `context` are **immutable**: `type` gates document validity (BR-TP07) and
  fleet-asset attachment (BR-TP12), so editing it could retroactively
  invalidate rows that were legal when created; `context` is the
  business-unit scope, and moving a partner between contexts is a
  migration, not an edit. `status` is untouched — it has its own lifecycle
  rules (BR-TP03–BR-TP05). `name` **is** editable: BR-TP01 already records
  that it is not a reliable natural key and IDs are server-generated, so
  nothing depends on its stability.
- **BR-TP33 (`version`):** Every `organizations` row carries a `version`,
  starting at `1`, incremented by exactly `1` on every successful write —
  including the lifecycle transitions (`Activate`/`Suspend`/`Reactivate`),
  so an edit form left open across someone else's suspension goes stale.
- **BR-TP34 (optimistic concurrency):** `UpdateDetails` requires the
  `version` the caller read; the write is
  `UPDATE … WHERE id = ? AND version = ?` and a mismatch is rejected
  `409 Conflict` with **no partial write**. The lifecycle transitions
  **bump `version` but do not check it** — they are already guarded by the
  status state machine, which rejects an illegal repeat on status alone, so
  requiring a version would only make a correct transition fail.
  This rule exists for the lost-update-across-think-time case (ADR-049
  finding 5a) that a `SELECT … FOR UPDATE` **structurally cannot catch**: a
  row lock protects the duration of a transaction, not the minutes an
  operator spends with an edit form open.
- **BR-TP35 (`Register` widening):** `Register` optionally accepts
  `companyName`, `registrationNo`, `vatRegistrationNo` and `tradingAs`. The
  required set is unchanged (`name`, `type`, `context`) and omitted fields
  stay empty, so every existing caller is unaffected. Widening `Register` —
  rather than having 38d's wizard register-then-update — deliberately avoids
  the half-commit shape ADR-049 finding 6 warns about, where a failed second
  call leaves a partner registered with none of its details.

- **Enforced in:** `internal/domain/organizations.go` (`UpdateDetails`,
  the immutable-field boundary), `internal/postgres` (the versioned
  `UPDATE` and every write's bump), `internal/browserrpc` (the
  `partner.update` endpoint and its `409`).
- **Test:** `organizations/organization_test.go` covers the domain rule,
  including the think-time case explicitly: two readers load the same
  version, both write, the second is rejected and the first writer's values
  survive intact. `organizations/browserrpc_roundtrip_test.go` covers it
  over `api.*`, asserting the `409` code and that `type`/`context`/`status`
  supplied in an update body are ignored.
  `internal/postgres/repository_test.go` covers what only a database can
  show: eight goroutines holding the same version write simultaneously, and
  **exactly one** wins while the other seven get `ErrVersionConflict`. The
  domain guard alone cannot establish that — every one of the eight passes
  it. **Gated on `ORGANIZATIONS_TEST_DATABASE_URL`**, see the README.
- **Not decided here:** how the conflict is *presented* to the operator
  (dialog vs. inline, and what recovery it offers) — a 38d decision.

### BR-TP36 (Phase 38c-i) — The document ID is the vetting document reference

BR-TP29's document `id` **is** the opaque document reference BR-TP23's
`TransporterVettingWorkflow` signals carry, and the `{documentID}` token in
38c-ii's Object Store object name
(`{context}.transporter.{id}.{docType}.{documentID}`). 38b's workflow
already keys on an opaque string and needs no structural change; this rule
pins what that string is, so the three sub-phases agree on one identifier
instead of three.

- **Enforced in:** documentation and the orchestration boundary that starts
  vetting — no new mechanism.
- **Test:** covered where the reference crosses the boundary; no separate
  spec beyond BR-TP23's existing workflow contexts.

### BR-TP37–BR-TP39 (Phase 38d-i) — Vetting state reaches the browser

**Approved 2026-08-20.** Before this sub-phase, no `api.*` endpoint exposed
`TransporterProfile` at all: 38a wired its projection reader only into the
`Activate` guard, so 38b's entire Temporal saga was invisible to any UI.
These rules are what make the vetting lifecycle observable.

- **BR-TP37 (`organization.profile.get`):** A single `api.*` endpoint (the 16th)
  returns a Transporter's `TransporterProfile` state — vetting status,
  attempt number, fleet-availability gate, GIT-verified flag and per-document
  review states. It reads the **canonical Postgres projection**, never
  Temporal's workflow Query API — the same rule BR-TP19's activation guard
  follows, and for the same reason (ADR-047/ADR-049 finding 3): status must
  not acquire a hard runtime dependency on Temporal being reachable. Asked
  for a `SHIPPER`, or for a Transporter with no profile yet, it returns a
  well-formed "no profile" answer rather than an error — a Shipper legitimately
  has none, so that is not a failure.
- **BR-TP38 (derived GIT status):** GIT status is one of five values —
  `None`, `Pending`, `Active`, `Expired`, `Rejected` (V2's real
  `GitStatusType`) — and is **always derived, never stored or hand-set**. It
  is the *worst* status across the partner's **current** `GOODS_IN_TRANSIT`
  documents (superseded rows are excluded, per BR-TP31), with severity
  ordered `Rejected` > `Expired` > `Pending` > `Active`; `None` only when the
  partner has no current GIT document. A document whose `expiresAt` is in the
  past reads as `Expired` regardless of its stored status.
  This is the first rule that **reads** `expiresAt`, which BR-TP07–BR-TP11
  stored but left unused. It deliberately does *not* mutate the document's
  own status on expiry — there is no scheduled expiry job, and inventing one
  here would quietly turn the deferred "expiry-driven status" exploration into
  shipped behaviour. Expiry affects the derived badge only, evaluated at read
  time.
- **BR-TP39 (conflict presentation):** When a Company Information save is
  rejected by BR-TP34, the UI shows an **inline banner on the affected
  section** that **keeps the operator's typed values** and offers two explicit
  choices: *Reload* (discard mine, take theirs) or *Overwrite* (re-read the
  current version and reapply mine). The operator's input is never discarded
  without an explicit choice — silently reloading over an open edit form
  destroys exactly the work BR-TP34 exists to protect, so a "helpful"
  auto-reload would defeat the rule it appears to serve. The banner names the
  section that lost (ADR-049 finding 6), not just "a conflict occurred".
  The banner is triggered by the **`conflict` flag on the service's error
  envelope** (`shared/browserrpc` `ErrorResponse.Conflict`), never by matching
  on the message text. Message prose is not a contract: a reworded backend
  error would silently downgrade this banner to a generic failure toast, which
  is the same lost update BR-TP34 exists to prevent, just reached by a
  different route. *Implementing this required a fix* — the browser's NATS
  request helper threw `new Error(body.error)` and dropped both `notFound` and
  `conflict`, so a 409 had been indistinguishable from a 500 in every frontend
  in this repo.
  Overwrite is deliberately **last-write-wins across the whole section**, not a
  field-level merge: it resubmits every Company Information field the operator
  holds, so a concurrent edit to a field they did not touch is also replaced.
  BR-TP34 still guards the retry, so a further concurrent write produces
  another banner rather than a silent loss.

- **Enforced in:** `internal/domain/git_status.go` (BR-TP38's derivation —
  domain layer, since "worst-of" is a business rule, not display logic),
  `internal/browserrpc` (BR-TP37's endpoint), and
  `frontend/refdata/src/components/TransporterPanel.vue` (BR-TP39), on top of
  `frontend/refdata/src/nats/connectionFactory.js` preserving the envelope's
  discriminators.
- **Test:** `organizations/git_status_test.go` covers BR-TP38's severity
  ordering, expiry-at-read-time, exclusion of superseded documents, and the
  empty case. `organizations/browserrpc_roundtrip_test.go` covers BR-TP37
  over the wire, including the Shipper/no-profile answer. BR-TP39 is covered
  by the frontend component specs.

### BR-TP40–BR-TP45 (Phase 38c-ii) — Compliance document files

**Approved 2026-08-20.** These complete the Documents tab 38d-i shipped with a
deliberately inert Upload control. Everything before this sub-phase treated a
document as metadata only: `reference` was an opaque external locator and no
bytes existed anywhere. Reviewed in
[ADR-048](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ADR-048-document-storage-nats-object-store.md),
whose four remaining amendments these rules discharge.

- **BR-TP40 (a dedicated byte ingress):** Document bytes move over **two HTTP
  routes** (`POST /files/documents`, `GET /files/documents`), not over
  `api.*`. This is a scoped, deliberate partial reversal of Phase 33.5's REST
  retirement, forced rather than chosen: the command surface is NATS `micro`
  request/reply with JSON bodies, which is neither a streaming transport nor
  able to exceed the server's `max_payload` (unset in this lab's `nats.conf`,
  so the 1 MiB default). The two routes carry **bytes and nothing else** — no
  business decision, no state transition, no lifecycle command is reachable
  over HTTP, all of which still require an authenticated NATS connection.
  BR-TP17's mux allowlist test is widened by exactly these two entries and
  stays closed to everything else; the entries are listed literally in the
  test rather than deferred to the production constant, so a third route
  still fails it. A consequence worth recording for the pattern-cards
  comparison: Object Store has **no presigned-URL equivalent**, so the
  service is a *mandatory* byte proxy in both directions — a permanent
  property of the choice, and a fair point in S3's favour.
- **BR-TP41 (transfer is authorized by a service-minted ticket):** The browser
  first calls `document.upload-ticket`/`document.download-ticket` over its
  authenticated per-tenant NATS connection; the service returns a
  **single-use, short-lived (2 min) capability token** bound to one
  `(tenant, context, partnerID, documentID, direction)` grant. The HTTP call
  carries only that token, in a header — never a query parameter, since a URL
  reaches logs, history and pasted links. Nothing about the tenant, context or
  document is read from the HTTP request; all of it comes off the redeemed
  ticket, so a ticket for tenant A cannot be spent against tenant B's bucket
  whatever the request claims.
  *This rule exists because of a finding, not a preference.* ADR-048 finding 5
  said the ingress needs "own auth", but **no JWT verification exists anywhere
  in this repo** — `accounts-service` only ever *mints* NATS credentials, and
  every other caller authenticates by connecting to a NATS account, which is
  server-enforced. HTTP is therefore the one path outside the boundary that is
  this platform's entire tenancy enforcement. A ticket keeps the authoritative
  decision where it already works rather than inventing a second
  authentication system for two byte-shovelling routes; verifying a bare user
  JWT as a bearer token was the alternative, and is weaker (no
  proof-of-possession, unlike the nkey challenge the NATS connection performs).
  Eligibility is checked at **mint** time as well as at redemption, so an
  operator is refused before spending a minute uploading. A mismatched
  direction still consumes the token, so it cannot be retried against the
  other route.
- **BR-TP42 (object names are service-minted):** The object name is
  `{context}.transporter.{id}.{docType}.{documentID}` — every token
  service-controlled, mirroring the repo's KV key convention rather than
  inventing a second scheme. The **original filename is deliberately absent**
  (ADR-048 finding 2): a user-controlled name makes object *identity*
  user-controlled, and two uploads of the same docType sharing a filename
  would resolve to one object — silently purging the earlier document's bytes
  while the log still records both, so the log would assert a document that
  cannot be retrieved. The filename lives in object metadata and the Postgres
  projection instead. This also sidesteps character legality, since real
  filenames carry spaces, parentheses and non-ASCII.
- **BR-TP43 (blob first, record second; write-once):** Nothing spans the
  Object Store and Postgres transactionally, and the two failure modes are not
  symmetric. Record-then-store leaves a projection — and upstream, an
  **immutable** event log — asserting a document whose bytes were never
  written; an event can only be compensated, not retracted (ADR-047's
  constraint one layer up). Store-then-record leaves at worst an **orphan
  object**: invisible to every reader, addressable by name, harmless. So the
  order is forced, not preferred.
  A document's bytes are consequently **write-once** — there is no replace.
  Overwriting an object would purge bytes the log still references, which is
  the exact failure BR-TP42 exists to prevent. The supported correction is
  BR-TP30's supersede-and-replace, which leaves both objects retrievable, and
  which is why **objects are never deleted** in this POC and why
  `GetDocument` returns superseded rows that `ListDocuments` (BR-TP31)
  excludes. The write-once guard is re-applied under `SELECT … FOR UPDATE`, so
  two uploads racing the same document cannot both win — and the loser has
  already written bytes, which is precisely why an orphan must be the
  acceptable outcome.
- **BR-TP44 (explicit limits):** **10 MiB per file** at the service boundary
  and **`MaxBytes` 256 MiB** on the bucket. Not decoration: an Object Store
  bucket *is* a JetStream stream, so document bytes and the `TRANSPORTER`
  event log compete for the same 1 GiB per-tenant allowance — enough uploads
  would stop event publishing for the whole tenant. 256 MiB leaves the log,
  the thing that cannot be re-derived, the clear majority. The per-file cap is
  enforced on the **bytes actually read** (`io.LimitReader` at Max+1, plus
  `http.MaxBytesReader` at the transport edge), never on a client-declared
  `Content-Length` a client can understate. A zero-byte upload is refused too:
  it would produce a document the log says has a file whose retrieval returns
  nothing. The browser's own pre-check is a courtesy that spares a doomed
  transfer, not a control.
  *Stream-budget check (ADR-048's precondition, done 2026-08-20 against the
  running stack):* ACME held 5 streams of 10 and GLOBEX 3, so
  `OBJ_organizations-docs` takes them to 6 and 4 — it fits, and no
  `/jslimits` raise is needed. ADR-048's related worry that refdata's
  per-context *versioned* KV buckets would exhaust the budget is **misplaced**:
  those 9 buckets live in **PLATFORM** (limit 20), not in the tenant accounts.
- **BR-TP45 (file metadata is projected):** `fileName`, `contentType`,
  `sizeBytes`, `objectName` and `uploadedAt` are stored on the document row,
  all nullable together — a document legitimately has no file until one is
  uploaded, and BR-TP43 makes the transition one-way, so there is no "had a
  file, now doesn't" state. Projecting it keeps the listing path off the object
  store entirely: the Documents tab renders names and sizes from one Postgres
  query, and the bucket is touched only when bytes actually move. Download
  sets `Content-Type` and an RFC 5987 `Content-Disposition` from the
  projection, and reads the **stored** object name rather than recomputing it
  — a name rebuilt from parts would diverge from the stored one the moment the
  naming rule changed. Filenames cross the HTTP boundary percent-encoded,
  because header values are ASCII and real filenames are not.

- **Enforced in:** `internal/domain/compliance_document.go`
  (`AttachFile`, `DocumentObjectName`, `MaxDocumentFileBytes` — the size cap
  and the naming rule are business rules, not transport concerns),
  `internal/filetickets` (BR-TP41's grants),
  `internal/application/commands/document_file.go` (BR-TP43's write order),
  `internal/objectstore` (bucket + `MaxBytes`),
  `internal/rest/document_files.go` (BR-TP40's ingress and its status
  mapping), `internal/browserrpc` (the two mint endpoints, taking tenant from
  the connection and `{context}` from the subject), and
  `frontend/refdata/src/api.js` + `components/TransporterPanel.vue`.
- **Test:** `organizations/document_file_test.go` covers all six —
  the naming rule (including a hostile `../../etc/passwd` filename that
  reaches metadata but never identity), ticket single-use/expiry/direction,
  the orphan-not-dangling-reference outcome when recording fails, the
  at-limit/over-limit/empty boundary, and the HTTP status mapping over
  `httptest`. `frontend/refdata/src/documentFileApi.spec.js` covers the
  browser's two-step flow, the percent-encoding, and status propagation.
  `internal/rest/handlers_allowlist_test.go` pins BR-TP40's widened surface.
  Verified live against the composed stack: a real upload with a non-ASCII
  filename, byte-identical download, ticket replay refused with 403, a second
  upload refused with `conflict: true`, and an 11 MiB attempt refused with 413
  leaving the document file-less and a 10 MiB orphan in the bucket — BR-TP43's
  trade, observed rather than asserted.

### BR-TP46–BR-TP50 (Phase 38d-ii) — Operating areas

**Approved 2026-08-20.** Operating areas declare where a Transporter is
willing to carry — commercial coverage, not compliance evidence. The region
corpus itself is refdata-owned (BR-D46–BR-D48 in
[BUSINESS_RULES-REFDATA.md](BUSINESS_RULES-REFDATA.md)); these rules govern
only the assignment of corpus entries to a Transporter.

Sourced from the live V2 database rather than from its Java source, which
matters here: V2's `GeoAreaEntity` polygon/GIS model and its
`transporter_operating_areas` join both hold **zero rows**, while a flat
`region_entity` → `country_entity` two-level list carries **48,041 live
assignments**. Country → Region therefore matches what V2 runs rather than
simplifying it (see
[ARCHITECTURE-ORGANIZATIONS.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-ORGANIZATIONS.md)
§ "V2 database verification" and § "Operating Areas — region seed").

- **BR-TP46:** An operating area may only be assigned to an `Organization`
  whose `type` is `TRANSPORTER`; a `SHIPPER` is rejected. Mirrors BR-TP12's
  per-type restriction for fleet assets.
- **BR-TP47 (assignment shape and corpus validation):** An assignment is
  `(transporterID, level, code)` where `level` is `COUNTRY` or `REGION`. The
  `code` must exist and be active in the matching refdata corpus for that
  level — `country` for `COUNTRY`, `region` for `REGION`; an unknown or
  deprecated code is rejected. **Not a pure-domain spec** — same treatment as
  BR-TP14, since existence checking requires the tenant-scoped `rpc.*`
  client (BR-D28 forbids a REST fallback for backend-to-backend calls), so
  its specs land at the adapter layer.
  `level` is retained even though V2's live join has no such column, because
  BR-TP48 and BR-TP49 both need it and because V2 demonstrably needs the
  concept it lacks — see BR-TP48.
- **BR-TP48 (no redundant overlap):** Adding a `REGION` whose parent
  `COUNTRY` is already assigned is rejected, and adding a `COUNTRY` when any
  region inside it is already assigned is rejected. Parentage resolves
  through BR-D47's `country` relation, so this rule depends on the corpus
  being well-formed and not on any denormalized parent held here.
  Making that literally true required a change on refdata's side: `rpc.*
  item.get` did not expose an item's references, so the relation was not
  readable cross-service. The cheap alternative — copying `country` into the
  region item's `attrs` — was rejected because it would have created a
  second source of truth for the same fact and made this rule's own wording
  false. The field was added to the item.get contract instead (additive and
  `omitempty`, with a spec pinning that existing consumers are unaffected).
  *Rejection, not silent collapse:* the alternative — auto-removing the
  now-redundant rows — would let one write delete rows the operator never
  touched, which is awkward to render in the UI and worse to explain in the
  audit trail BR-TP50 requires. An explicit rejection makes the operator
  resolve the ambiguity.
  *Why `level = COUNTRY` exists at all:* "operates nationwide" is a real and
  heavily-used declaration — V2 expresses it by assigning a **fake region row
  named after the country**, and those rows are the single most-assigned
  entry in each country (`Botswana` 708 transporters, `Namibia` 596, both
  ranking above every genuine district). Modelling it as a level keeps the
  statement stable: with regions-only, seeding a new region would silently
  shrink an existing Transporter's declared coverage.
- **BR-TP49 (uniqueness):** `(transporter, level, code)` is unique — the same
  area cannot be assigned twice. **Repository-level invariant**, enforced by
  a Postgres unique constraint rather than a domain guard, mirroring
  BR-TP13's `registrationNo` and BR-TP08's one-per-type treatment.
- **BR-TP50 (freely editable, including after `Vetted`):** Operating areas
  may be added or removed at any point in the profile lifecycle. A change
  does **not** return the profile to `Pending`, does not re-run the Temporal
  vetting workflow, and does not touch `FleetAvailabilityGate`. Every change
  is recorded in the existing `audit_events` table per BR-TP06's
  actor/outcome conventions.
  *The reasoning is a boundary claim, not a convenience:* neither branch of
  the vetting saga (BR-TP21's GIT insurance verification and document
  approval) reads operating areas, so no area change can invalidate a
  vetting decision. Re-vetting on an area change would re-run an insurance
  check over a fact insurance does not depend on. If a future rule ever makes
  coverage a compliance input — a per-territory permit, say — this rule is
  the one that has to change first.

- **Enforced in:** `organizations/internal/domain/operating_area.go`
  (BR-TP46/BR-TP47's shape guards and BR-TP48's overlap rule — note
  `ValidateOperatingAreaShape` is split out from `AddOperatingArea` so the
  application layer can reject a doomed request before spending an `rpc.*`
  round trip, rather than inventing a placeholder country to satisfy a
  guard), `organizations/internal/domain/operating_area_resolver.go`
  (BR-TP47's port), `organizations/internal/refdataclient/client.go`
  (`ResolveArea`) and `internal/tenants` (the tenant-connection delegate),
  `organizations/internal/application/commands/operating_area.go`
  (orchestration + BR-TP50's audit), the unique index in
  `internal/postgres/migrate.go` with
  `internal/postgres/operating_area_repository.go` translating its violation
  (BR-TP49), and three `api.*` endpoints in `internal/browserrpc/adapter.go`.
  *Correction:* this list previously named
  `transporterprofile/internal/domain` and a `refdataconsumer` package —
  both were guesses written before implementation, and neither path exists.
- **Test:** `organizations/operating_area_test.go` — 13 specs, one
  `Context` per rule, with BR-TP48 covering both directions
  (region-under-assigned-country and country-over-assigned-region), the
  legal non-overlapping cases, and an assertion that a refused add does not
  mutate the existing set. BR-TP49 is deliberately not domain-specced (it is
  a database constraint); it and BR-TP50 are verified live. The `api.*`
  endpoint-count guard in `browserrpc_test.go` widens from 18 to 21 and pins
  the three new subjects.
  Verified live against the composed stack over real tenant credentials: two
  regions added with `countryCode` resolved from refdata's relation rather
  than the request (the wire payload carries no country at all); a duplicate
  refused by BR-TP49; `ZA` refused over its own assigned regions and `BW-CE`
  refused under an assigned `BW` (BR-TP48, both directions); an unknown code
  refused by BR-TP47; removing an unheld area reported rather than silently
  succeeding; and BR-TP50's `audit_events` rows written for every add and
  remove, carrying level/code/countryCode metadata.

**Frontend (38d-ii, both rule groups).** Two tabs on the existing Transporter
drill-in: `frontend/refdata/src/components/TransporterPanel.vue` plus
`OperatingAreaMap.vue` (Leaflet + OpenStreetMap over
`public/geo/operating-areas.geojson`, fetched at runtime so ~470 KB of
geometry never enters the bundle), with client specs in
`src/organizationsApi.spec.js`.

Two presentation decisions that are rule-driven rather than cosmetic:

- **The map is not the authoritative control.** The region checklist beside
  it writes through the same handler, so the two cannot drift; the map exists
  because "the whole Western Cape" is faster to click than to find in a list,
  not because coverage needs a map. Anything reachable only by clicking a
  polygon would be unreachable without a pointer. The checklist also renders
  BR-TP48's blocked reason inline ("1 region(s) of BW are assigned
  individually") rather than letting an operator click into a server-side
  rejection.
- **The credential field is write-only, and says so.** BR-TP52 means no
  api.* call can return a payload, so the UI must not imply otherwise: the
  input is `type="password"`, it is cleared immediately on success (a stale
  secret sitting in a form is one waiting to be shoulder-surfed), and the tab
  states plainly that credentials "cannot be read back — not by this screen,
  not by any API". `METADATA_ONLY` disables the field entirely, since that
  V2 case genuinely has no secret to enter.

Verified in-browser end to end against the composed stack: a region toggled
from the checklist highlighted on the map and persisted with its
`countryCode` resolved server-side; BR-TP48's blocked reason appeared on the
country row; a credential typed into the browser stored, listed as
`Configured`, and the form cleared — with **0 plaintext hits for that typed
value in a full `pg_dump` and 0 in the `TRANSPORTER` log**, the event
carrying only provider/credentialType and the KV value opaque under `xxd`.

### BR-TP51–BR-TP55 (Phase 38d-ii) — Tracking credentials

**Approved 2026-08-20.** A Transporter's telematics-provider credentials.
This is a **confirmed, deliberate divergence from V2**, which stores raw
secrets as plaintext columns across 20 per-provider satellite tables with no
encryption anywhere — verified in both the source (no `@Convert`/
`AttributeConverter`) and the live database (`cartrack.api_key`,
`webfleet.password`, plain `varchar`, 15 of the 20 tables populated).

- **BR-TP51 (shape):** A tracking credential may only attach to a
  `TRANSPORTER`-typed partner (mirrors BR-TP46/BR-TP12); at most one exists
  per `(transporter, provider)`; and `credentialType` must be one of
  `API_KEY`, `USERNAME_PASSWORD`, `METADATA_ONLY` — V2's real three-value
  enum, kept because V2's live spread (40 / 34 / 15) shows all three carry
  real weight. `provider` is a small representative enum, not V2's 35
  vendors. V2's free-text `providerName` column is **not** carried: its live
  values are visibly corrupted (`MixSite1`…`MixSite16`, `ctrack-32332`,
  `Autotrak51`) beside a clean `trackingProvider` enum holding the same fact.
- **BR-TP52 (the secret exists in exactly one place, and never in the
  clear):** The credential payload is **sealed by this service with
  AES-256-GCM** and the ciphertext written to the `organizations-secrets`
  NATS KV bucket, keyed
  `{context}.transporter.{id}.trackingcreds.{provider}`.
  *Reworded during implementation (2026-08-20).* This rule originally said
  "a NATS KV bucket with at-rest encryption enabled", which assumed a
  per-bucket switch that does not exist: NATS at-rest encryption is a
  server-wide `jetstream { key: ... }` directive covering every stream and
  bucket, and this lab's `nats.conf` does not set it. Enabling it would
  re-key the whole lab's storage to protect one bucket. Service-side sealing
  was chosen instead and is **strictly stronger than the original wording
  asked for**: the ciphertext is opaque to anyone reading the bucket — a
  `nats kv get`, a JetStream backup, a NATS operator — not merely to someone
  holding the disk, and the guarantee stays local to the feature that needs
  it rather than becoming a property of the deployment.
  The trade, recorded rather than hidden: this service now holds a key. It
  is read from the environment and never persisted, a wrong-length key is
  refused rather than padded or hashed into shape, and a missing key makes
  the store refuse to open — failing closed, because storing a credential in
  the clear owing to missing configuration is the exact outcome this rule
  exists to prevent. Losing the key costs a re-entry of rotatable secrets
  (BR-TP54), not loss of anything irreplaceable. It is **never**
  published to JetStream, never written to Postgres, and never returned by
  any read path — projections and RPC replies expose `provider`,
  `credentialType` and `credentialsConfigured`, and nothing else. A read
  endpoint that returns a secret is a bug in this rule, not a feature
  request.
  *Why not the event log:* an event-sourced log is meant to be replayed and
  audited, and it cannot be redacted the way a row can be updated. Baking raw
  credentials into it would be strictly worse than V2's already-bad plaintext
  columns, because V2 can at least `UPDATE` a compromised value out of
  existence.
- **BR-TP53 (KV first, event second):** Nothing spans NATS KV and the event
  log transactionally, and the two failure orders are not symmetric — the
  same asymmetry BR-TP43 records for document bytes. Event-first leaves an
  **immutable** log asserting a configured credential that was never stored,
  and an event can only be compensated, never retracted. KV-first leaves at
  worst an unreferenced secret in the bucket, which BR-TP54 makes
  self-correcting on the next write. So the order is forced, not preferred.
- **BR-TP54 (credentials are mutable; documents are not):** Re-configuring a
  provider **overwrites** the KV entry in place. There is no supersede-and-
  retain, no version history, and no "previous credential" to retrieve.
  *This is deliberately the opposite of BR-TP43's write-once documents, and
  the contrast is the point rather than an inconsistency.* A compliance
  document is **evidence**: the log references a specific artifact, so
  destroying its bytes would make the log assert something unretrievable. A
  credential is **current state**: secrets rotate as a matter of routine
  hygiene, nothing in the log references a payload (BR-TP52 guarantees it),
  and retaining superseded secrets would mean keeping compromised material
  alive for no reader. The rule of thumb this phase contributes: *store
  evidence write-once, store state overwritable, and let what the log
  references decide which is which.*
- **BR-TP55 (the flag is event-sourced and gates fleet availability):**
  Configuring a credential appends `TrackingCredentialConfigured`, carrying
  `provider`, `credentialType` and the resulting `credentialsConfigured`
  flag — secret-free by construction, since BR-TP52 keeps the payload out of
  the aggregate entirely. The flag must be event-sourced rather than a bare
  projection column precisely because it feeds a write-side decision.
  `AvailableForAssignment` gates on the `FleetAvailabilityGate` **and** at
  least one configured credential.
  *Corrected during implementation (2026-08-20).* This rule said the value
  was "extended". It was not: `AvailableForAssignment` **did not exist
  anywhere in the codebase** — not in Go, not in the frontend — despite the
  38a/38b implementation note describing it as a built computed read-layer
  value. This rule therefore creates it.
  It gates on **two** conditions, not V2's three. The missing one is
  ownership, and its absence is a genuine gap rather than a simplification
  chosen here: BR-TP13's `FleetAsset` carries only
  registrationNo/vin/make/model/vehicleTypeCode — the `ownership` field the
  design intended was never built, and adding it changes 38a's model, which
  is not this rule's to approve. A second reduction: V2 links a credential
  **per fleet asset**, while `FleetAsset` here has no credential link, so
  credentials are profile-level and the honest granularity is "this
  transporter may be assigned loads" rather than "this truck may". Both gaps
  are recorded in the code beside the computation.
  It remains a **computed read-layer value** — a join, never a column, and no
  new field on the legacy `FleetAsset` domain type — preserving 38a's
  boundary and ADR-049's "save boundaries align to the aggregate boundary"
  finding exactly as the 38a/38b implementation note describes.

- **Enforced in:** `organizations/internal/domain/tracking_credential.go`
  (BR-TP51's guards, and `TrackingCredentialSecretKey` — the key format is a
  business rule and lives in the domain for the same reason BR-TP42's
  `DocumentObjectName` does), `organizations/internal/secrets` (BR-TP52's
  AES-256-GCM sealing and the bucket, with `History: 1` implementing
  BR-TP54), `organizations/internal/application/commands/tracking_credential.go`
  (BR-TP53's write order), `organizations/transporterprofile/domain/profile.go`
  (BR-TP55's event, its `Apply` case, and `State.AvailableForAssignment`),
  `transporterprofile/orchestration/profile.go` (the sequence-guarded
  append), and two `api.*` endpoints in `internal/browserrpc/adapter.go`.
  *Correction:* this list previously used `transporterprofile/internal/...`
  paths that do not exist — they were guesses written before implementation.
  **BR-TP52 has no read endpoint anywhere**: `SecretStore` exposes `Put`
  only to the command layer, and the api.* surface has exactly two
  tracking-credential subjects. The absence of a third is the enforcement.
- **Test:** `organizations/tracking_credential_test.go` (6 specs — BR-TP51's
  guards, plus a **structural** assertion that `TrackingCredential` has no
  secret-bearing field, so adding one fails a test rather than passing
  review), `organizations/tracking_credential_order_test.go` (5 specs —
  BR-TP53's ordering in all four outcomes, including that a payload failure
  appends no event and that a rejected credential stores nothing at all),
  `internal/secrets/secrets_test.go` (5 tests — sealed output never contains
  its plaintext, sealing is non-deterministic so equal secrets are
  indistinguishable in the bucket, tampering is rejected, a wrong-length key
  is refused), and `transporterprofile/availability_test.go` (BR-TP55 across
  all four gate/credential combinations, BR-TP54's overwrite-on-replay, and
  BR-TP52 searched for in both event bytes and projected state).
  Negative assertions throughout: a rule about what must *never* be emitted
  is tested by absence, and the searches use `%#v` rather than named fields
  so a leak into an unexpected field cannot slip through.
  Verified live against the composed stack over real tenant credentials: a
  credential configured and then rotated; a bogus provider refused; and the
  secret hunted where it must not be — **0 plaintext hits in a full
  `pg_dump` of the whole database, 0 in the `TRANSPORTER` JetStream log**,
  the published event carrying only `provider`/`credentialType`, the KV
  value opaque ciphertext under `xxd`, and **1** history revision after a
  rotation (BR-TP54 overwrites rather than accumulating superseded
  secrets).

### BR-TP59 (Phase 38h-i) — A compliance document may carry an optional, future-dated expiry

**Approved 2026-08-21.** A `ComplianceDocument` may carry an `expiresAt`
(Unix seconds), supplied when the document is registered or set afterwards
through a dedicated command. It is **optional** — a document with no expiry
cannot lapse by time — and, **when supplied, must be strictly in the
future** at the moment of writing. Clearing it (an explicit `null`) is
always legal and is never checked against the clock.

Setting an expiry is **not a review transition**: status is untouched, and
an Approved document may have its expiry corrected without being
re-reviewed. Renewing cover is a new document (BR-TP30); correcting a
mistyped date is not a decision a reviewer needs to retake. A `SUPERSEDED`
document refuses the change, consistently with every other transition off
that terminal status.

- **Why future-dated is enforced rather than accepted:** a past date on a
  write is a data-entry error, not a lapse that has already happened.
  Accepting one would arm 38h-ii's cover timer (BR-TP60) against an instant
  already gone by, producing an immediate suspension indistinguishable from
  a real business event.
- **Why it exists at all:** `expires_at` and `ComplianceDocument.ExpiresAt`
  both predate this rule, but **no API had ever written them**. That is why
  the TIMESTAMPTZ-into-`*int64` scan defect fixed 2026-08-21 went unnoticed
  for so long, and why BR-TP28's expiry-driven suspension had never been
  genuinely exercisable — the walkthrough that found it had to set the
  column with SQL.
- **Enforced in:** `domain.ComplianceDocument.SetExpiry` — the single
  point at which an expiry is written, called by both the add path
  (`commands.ComplianceDocumentHandler.AddDocument`) and
  `postgres.ComplianceDocumentRepository.SetDocumentExpiry`, so the rule
  cannot hold on one route and be forgotten on the other. The repository
  applies the guard against the row it has locked `FOR UPDATE`, not against
  the caller's copy.
- **Subject:** `api.{context}.organizations.document.set-expiry.v1`, its own
  verb rather than a field on approve — an expiry is a fact about the
  document, and folding it into the review would conflate "this cover runs
  to date X" with "a human accepted it". `document.add.v1` gains an optional
  `expiresAt`. The field is a pointer on the wire so "not supplied" stays
  distinct from "cleared".
- **Test:** `organizations/compliance_document_test.go` (7 specs — future
  accepted, past and exactly-now refused, nil clears, Approved allowed,
  Superseded refused, receiver not mutated),
  `internal/postgres/repository_test.go` (3 specs — set after the fact,
  cleared, and a past date refused at the repository boundary rather than
  only in the domain), plus the `api.*` round trip in
  `browserrpc_roundtrip_test.go` and the subject allowlist in
  `browserrpc_test.go`.
- **Verified live 2026-08-21** over `acme.creds` against the composed
  stack: added with an expiry and read back on the response; moved forward
  by `set-expiry`; a past date refused with "compliance document expiry
  must be in the future"; cleared to `null` and confirmed NULL in Postgres.
- **Known wart:** the refusal comes back as **500**, not 400. This is not
  new to this rule — every input-validation error on this surface does
  (`ErrReferenceRequired`, `ErrInvalidDocumentType`), because the shared
  `api.*` reply path classifies only 404 and 409 specially. Fixing it means
  adding a 400 class to `shared/browserrpc` and classifying every
  validation error at once, which is deliberately not done here rather than
  making this one rule inconsistent with its neighbours.

### BR-TP60–BR-TP63 (Phase 38h-ii) — The cover watch is a durable timer bound to the Vetted state

**Approved 2026-08-21.** These four rules **replace BR-TP28's polling
Temporal Schedule** with a durable timer held inside the vetting workflow.
BR-TP28's *effects* are unchanged — a lapse still appends
`FleetAvailabilityRevoked` and then suspends the organization, through the
same `HandleGitStatusDrop` command. What changes is **when the check runs**.

- **BR-TP60** — a transporter that reaches `Vetted` gets exactly one cover
  watcher, armed to the earliest expiry across its current
  `GOODS_IN_TRANSIT` documents. When that instant arrives it performs
  BR-TP28's drop. A document with no expiry cannot lapse by time, so the
  watcher parks on the signal alone rather than arming.
- **BR-TP61** — writing a GIT document's expiry (BR-TP59) re-arms the
  watcher, which **re-reads** the expiry rather than taking it from the
  signal payload: the document is the authority, and a date in a signal can
  be stale by the time it is delivered. The watcher never fires on a
  superseded expiry.
- **BR-TP62** — an armed timer exists **if and only if** the profile is
  `Vetted`. The timer lives inside a `workflow.WithCancel` scope in the
  vetting workflow, so leaving `Vetted` cancels it structurally — there is
  no cleanup step to forget and no window in which the state has moved on
  but the timer has not. The workflow therefore stays *running* while the
  profile is `Vetted`, continuing as new every `maxCoverCycles` re-arms so
  history stays bounded.
- **BR-TP63** — a lapse is a state transition, `Vetted` → `CoverLapsed`,
  not merely a gate flipped underneath an unchanged status. There is no
  direct un-lapse: renewed cover is a new document (BR-TP30), reviewed like
  any other, and re-vetting is BR-TP26's normal resubmit path.

**Why the polling schedule went.** It asked, every 5 minutes forever, whether
an instant already written down in the document had passed. Measured on the
live stack 2026-08-21: **184 monitor executions against 16 vetting
workflows**, from 8 schedules, growing linearly with the fleet and unbounded
in time — and nothing ever deleted a schedule, so a suspended transporter
kept polling and one schedule had been failing every interval with "tenant is
not connected" since before the tenant fix.

- **Enforced in:** `transporterprofile/workflow.watchCover` (the timer and
  its cancellation scope), `activities.CoverExpiry` →
  `composition.gitMonitor.CoverExpiry` (earliest expiry across current GIT
  documents, read on every arming, never cached),
  `worker.VettingService.SignalCoverChanged` (BR-TP61's re-arm), and
  `domain.TransporterProfile.RevokeFleetAvailability` (BR-TP63's
  transition). `TransporterGitMonitorWorkflow`, `GitMonitorScheduleOptions`
  and `ORGANIZATIONS_GIT_MONITOR_INTERVAL` are **deleted**;
  `worker.DeleteGitMonitorSchedules` clears the retired schedules at startup.
- **Observability:** because a vetted run stays *running*, it has no Result
  to read — so the workflow exposes a `vettingState` **query**, which is
  what `temporal workflow query` and the Temporal UI report. Without it the
  most common state would be the one you cannot see.
- **`Apply` takes the status from the event, not a constant**, so replaying
  a pre-38h-ii `FleetAvailabilityRevoked` (which carries the incumbent
  status) reconstructs what actually happened instead of retro-fitting
  `CoverLapsed` onto history.
- **Test:** `transporterprofile/workflow/workflow_test.go` (parks in Vetted
  holding the timer; arms nothing on a Rejected or compensated attempt;
  parks without arming when there is no expiry; re-arms on `CoverChanged`
  and does **not** fire on the superseded expiry; fires exactly one drop and
  ends in `CoverLapsed`), `transporterprofile/availability_test.go`
  (BR-TP63's transition and its refusal to run twice),
  `transporterprofile/orchestration/orchestration_test.go` (the
  half-completed retry — see below), `worker/worker_test.go`
  (`IsGitMonitorScheduleID` matches schedules but not action IDs or the
  vetting workflow), and
  `internal/postgres/transporter_projection_test.go`.
- **Two defects this uncovered, both silent:**
  1. **The drop's retry branch keyed off `Vetted`.** It recognises a
     half-finished drop — revocation appended, suspend failed — by the state
     the first call left behind, which BR-TP63 moved to `CoverLapsed`. Left
     as it was, a retry returned success **without suspending**, leaving a
     transporter whose cover had lapsed still ACTIVE and assignable. There
     was no spec for the half-completed path, which is why nothing went red.
  2. **The projection's status CHECK constraint.** It enumerated the four
     old statuses, so the projector's write of `CoverLapsed` was rejected,
     Nak'd, and redelivered forever — ack floor frozen one message behind
     while the consumer sequence climbed past 47,000, with nothing in the
     service log. Because the drop suspends the organization *before* the
     projection catches up, the visible symptom was a **SUSPENDED
     organization whose profile still read `Vetted` with the gate open**.
     `transporter_projection_test.go` now asserts every domain `Status` is
     writable, so the two lists cannot drift again.
- **Verified live 2026-08-21** against the composed stack: 8 retired
  schedules cleared at startup (`cleared retired GIT monitor schedules
  (38h-ii) count=8`), **0** monitor executions afterwards, a transporter
  vetted with 90 seconds of cover, its run parked and answering
  `vettingState` with `Vetted`, the timer firing at the exact expiry, and
  the profile ending `CoverLapsed` with `fleetAvailabilityGate=false` while
  the organization went `SUSPENDED`.
