# Business Rules — Trading Partner Service (`backend/trading-partner-service/`)

> Split out of `BUSINESS_RULES.md` to keep per-domain reads small. See that
> file's index for the Shipping (BR-001–BR-033), Reference Data
> (BR-D01–BR-D34), Accounts (BR-AC01–BR-AC13), and Pricing (BR-P01–BR-P24)
> domain rules.

**BR-TP01–BR-TP14 confirmed 2026-08-13** (Phase 26, IMPLEMENTED end to end —
[Main-POC-Plan.md](../../.claude/plans/Main-POC-Plan.md)). Covers 26a (the
`TradingPartner` aggregate's registration/lifecycle), 26a1 (its audit
trail), 26b (compliance documents), and 26c (Transporter fleet assets),
26d (Postgres/REST/tenant-NATS wiring), and 26e (Admin UI) — all
live-verified against the real composed stack, including in-browser.
A separate service, separate Postgres schema
(`trading_partner`), no datastore shared with `shipping-service`,
`refdata-service`, `accounts-service`, or `pricing-service` — see
`tenant_service_separation_decision.md`. Plain Postgres CRUD (not
event-sourced) — see `ARCHITECTURE.md` § "Event Sourcing vs Plain CRUD" and
the Phase 26 plan section's own per-entity CQRS classification.

### BR-TP01–BR-TP06 — TradingPartner registration and lifecycle

- **BR-TP01:** A `TradingPartner`'s `type` (`SHIPPER` | `TRANSPORTER`) is
  required at registration and immutable thereafter — there is no "convert a
  Shipper into a Transporter" operation. Mirrors V2's `BusinessType`
  discriminator (`linebooker_shipper_vs_customer_naming.md`,
  `v3_tenancy_axes_decision.md`).
- **BR-TP02 (`Register`):** Creating a `TradingPartner` always lands in
  `Registered` status — this is creation, not a transition, so it has no
  "illegal from" case the way `Activate`/`Suspend`/`Reactivate` do below.
  **Confirmed 2026-08-13:** only `name`, `type`, and `context` are required
  at `Register` time; `tradingAs`, `companyName`, `registrationNo`, and
  `vatRegistrationNo` are all optional, fillable incrementally as KYC/vetting
  proceeds — matching a real onboarding flow where an operator starts a
  record before every detail is confirmed.
- **BR-TP03 (`Activate`):** Legal only from `Registered` → `Active`. Called
  on a `TradingPartner` in any other status (`Active` or `Suspended`), it is
  rejected with `409 Conflict` — mirrors `reactivateAccount`'s guard shape in
  `accounts-service/accounts/handler.go`.
- **BR-TP04 (`Suspend`):** Legal only from `Active` → `Suspended`, and
  **requires a non-empty `reason`** — rejected at the domain boundary (not
  just a REST-layer check) if `reason` is empty. Called on a
  `TradingPartner` in any other status (`Registered` or `Suspended`), it is
  rejected with `409 Conflict`. **v1 has no enforcement consumer for this
  status** — nothing in this POC yet refuses a `Suspended` partner's bids or
  loads; the eventual consumer is the marketplace/tender phase (see
  `linebooker_bid_tender_allocation_rules.md`). What `Suspend` delivers today
  is the guarded state machine plus the audit trail (BR-TP06), not an
  enforced boundary.
- **BR-TP05 (`Reactivate`):** Legal only from `Suspended` → `Active`. Called
  on a `TradingPartner` in any other status (`Registered` or `Active`), it
  is rejected with `409 Conflict` — completes the
  `Register`→`Activate`→`Suspend`→`Reactivate` lifecycle, mirroring
  accounts-service's create/suspend/reactivate triple (BR-AC08–AC10).
  There is no further terminal/offboarding state in v1 (explicit non-goal —
  see the Phase 26 plan section's retention rationale, mirroring BR-AC03).
- **BR-TP06:** Every lifecycle state change — register, activate, suspend,
  reactivate — records an immutable row in `trading_partner.audit_events`:
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
  `(TradingPartner, type)` — adding a document for a type that already
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
  decided.

Document status remains fully independent of the parent
`TradingPartner.status` in v1, per BR-TP04's note and the Phase 26 plan
section's "Deferred: document-driven status" item — nothing here gates or
is gated by `Activate`/`Suspend`/`Reactivate`.

### BR-TP12–BR-TP14 — Transporter fleet assets (26c)

**Confirmed 2026-08-13.** Fleet assets are a trimmed `FleetAssetEntity`
(`registrationNo`, `vin`, `make`, `model`, `vehicleTypeCode`) —
`subcontractingOwner` stays out of scope regardless of anything else decided
here (settled earlier, not reopened).

- **BR-TP12:** A `FleetAsset` may only be attached to a `TradingPartner`
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
`trading-partner-service/tradingpartner/internal/domain/trading_partner.go`;
specs in `trading-partner-service/tradingpartner/trading_partner_test.go`
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
"Trading partners" nav category in `frontend/admin` (own eyebrow, per
`linebooker_registration_ui_placement.md` — not folded into Accounts or
RefData), `TradingPartnersPanel.vue`: register dialog, list table with a
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
`TradingPartnersPanel.vue` is parameterized by a `partnerType` prop and
mounted twice rather than duplicated. Consequences: the register dialog no
longer has a Type field (the panel's own role supplies it), the list has no
Type column, and the list is filtered client-side because
`GET /api/trading-partners/{context}` still takes no `type` query param —
revisit if that list ever paginates server-side. No BR-TP rule changed.

**26g status (2026-08-13): IMPLEMENTED, live-verified.** `internal/browserrpc`
registers the service via `micro.AddService` on each tenant connection
(`Name: "trading-partner-service"`, `Version: 1.0.0`, `Metadata{"tenant": …}`),
making it discoverable in the Admin UI's Services panel — it had been absent
despite running, because an outbound-only `rpc.*` requester answers nothing on
`$SRV`. **Zero `api.*` endpoints are registered:** REST remains the live
inbound transport, and no BR-TP rule changed. 6 new specs
(`tradingpartner/browserrpc_test.go`, embedded NATS) assert discoverability
over a real `$SRV.PING` broadcast; 43 total green.

**26h status (2026-08-13): IMPLEMENTED, live-verified in-browser.** The Admin
UI now reaches this service over
`api.{context}.trading-partner.{entity}.{action}.v1` (14 endpoints, 6 tokens
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

### BR-TP15 (Phase 28) — The same `obs.trace.*` wire contract as `BUSINESS_RULES-SHIPPING.md`'s BR-036, on trading-partner-service's publisher side

Mirrors `BUSINESS_RULES-SHIPPING.md`'s BR-036 for this service's own tracing publisher — prototyped here first (Phase 28a), since this service already has `observe`/`reply`/`actor` helpers and no JetStream, before being copied to pricing, shipping, and refdata (Phase 28b). `browserrpc.Adapter`'s `traceSpan` is a strict superset of its existing `obsEnvelope` — no field renamed or retyped, every addition (`traceId`, `spanId`, `parentSpanId`, `service`/`entity`/`action`, `statusCode`/`statusMessage`, `attributes`, `redacted`, `truncated`) `omitempty` — and every `obs.trace.{context}.trading-partner.{entity}.{action}` publish goes to the PLATFORM account only, with the same redact-before-truncate ordering and 4 KiB cap BR-036 establishes. Never blocks or fails a business path.

- **Enforced in:** `tradingpartner/internal/natstrace` (new package, Phase 28a) — the prototype `Tracer.publish()` redaction-then-truncate ordering and `traceSpan` struct that Phase 28b's clones mirror field-for-field; the `AddEndpoint` decorator that starts a span per request without a hand-pasted `publishObs` call at each of the 14 handler sites.
- **Test:** `tradingpartner/internal/natstrace/natstrace_test.go` — the shared cross-service contract test (BR-036's clone) asserting the `traceSpan` JSON shape, and that an old-shape `obsEnvelope` still decodes; `browserrpc_roundtrip_test.go`'s `obs.*` side-channel context gains a decoding assertion (the existing test only checks the raw subject string, not the envelope shape).
