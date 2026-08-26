---
name: linebooker-trading-partner-phase-v1-scope
description: Confirmed 2026-08-13 scope decisions for the Trading Partner (Shipper/Transporter registration) implementation phase — single unified record, 3-state suspend/reactivate lifecycle, full v1 scope including documents and fleet assets, both roles built together via a type discriminator; plus an Opus design review whose findings were independently verified, including a caught-and-fixed contradiction requiring tenant-scoped NATS wiring for refdata validation
metadata:
  type: project
---

Confirmed via AskUserQuestion 2026-08-13, closing out the open decisions from [[linebooker_platform_vs_tenant_service_split]], [[linebooker_registration_ui_placement]], and [[linebooker_trading_partners_term_and_fleet_cardinality]]. This is the go-ahead for the "Trading Partner" plan phase — business rules confirmation and plan sign-off still needed before code, per CLAUDE.md's AI Agent Workflow, but these four architecture forks are now decided.

**1. Platform-identity vs tenant-membership split: NOT split now.** Build a single `TradingPartner` record (identity + status) with no separate platform-account/tenant-membership tables. Matches how other services in this POC started simple (e.g. refdata-service didn't get its own DB until Phase 21) rather than front-loading [[linebooker_platform_vs_tenant_service_split]]'s eventual-correct two-layer model. Revisit the split only when a real cross-tenant-membership need appears (e.g. one Transporter operating across two tenant marketplaces).

**2. Status lifecycle: simple 2-state (Registered -> Active) for v1** — not V2's four-state `TransporterProfileStatus` (`AWAITING_DOCUMENTATION` -> `DOCUMENTS_IN_REVIEW` -> `APPROVED`) and not a full event-sourced state machine (Shape C style). **Explicit follow-up requested, not yet designed:** the user wants a noted exploration of whether registration lifecycle should eventually become its own CQRS shape or use a temporal/effective-dated model (echoes [[linebooker_refdata_layering_model]]'s versioning-rules gap and CLAUDE.md's event-sourcing-vs-CRUD test — "does anything need to replay this" is unresolved for registration status changes, e.g. re-vetting after a suspension). Flag this as a named open item in the plan phase rather than deciding it now.

**3. v1 scope: full — documents AND fleet assets included**, not deferred to a follow-on phase. Compliance documents (subset of V2's `DocumentTypes` — candidates identified: `CIPC`, `DIRECTOR_ID`, `BANK_CONFIRMATION_LETTER`, `BEE_COMPLIANCE_CERTIFICATE`, `GOODS_IN_TRANSIT` insurance, `GIT_CONTINGENCY_POLICY`, `TERMS_AND_CONDITIONS`) and Transporter fleet assets (one-to-many, per [[linebooker_trading_partners_term_and_fleet_cardinality]]) are both in v1. **Subcontracting (`FleetAssetEntity.subcontractingOwner`) stays deferred regardless** — that was never in question, only the base fleet relationship and documents were.

**4. Build order: Shipper and Transporter together**, one generic `TradingPartner` aggregate with a type discriminator (`SHIPPER` | `TRANSPORTER`) — mirrors V2's `BusinessEntity` + `BusinessType` pattern, which [[v3_tenancy_axes_decision]] already flagged as worth adopting. Fleet assets and (likely) some document types are Transporter-only fields/children on the same aggregate, not a separate entity hierarchy.

**5. Suspend/reactivate: YES, in v1.** Confirmed 2026-08-13 — lifecycle is
`Registered` -> `Active` -> `Suspended` -> `Reactivate` (back to `Active`),
mirroring accounts-service's create/suspend/reactivate triple (BR-AC08-AC10
in `BUSINESS_RULES-ACCOUNTS.md`). All transitions remain manual, independent
of compliance-document approval state (decision #3 above still holds — no
document-gating on any transition in v1).

**6. Fields and documents confirmed 2026-08-13** (trimmed from the V2-evidence proposal): shared fields `name`/`tradingAs`/`companyName`/`registrationNo`/`vatRegistrationNo`/`type`/`status`/`context` (dropped `contactPerson`/`contactNo`); Transporter-only fleet assets (dropped `gitCoverage` as a profile field — see correction in #7). Documents: `CIPC`/`DIRECTOR_ID`/`BANK_CONFIRMATION_LETTER`/`TERMS_AND_CONDITIONS` for both roles, `GOODS_IN_TRANSIT` additionally for Transporter (dropped `GIT_CONTINGENCY_POLICY`/`BEE_COMPLIANCE_CERTIFICATE`). Written into Phase 26 of `.claude/plans/Main-POC-Plan.md`.

**7. Opus design review (2026-08-13) applied 10 findings to the Phase 26 plan section, independently spot-checked against the actual repo (not taken on faith) — verified correct:** transition legality matrix (3 legal edges, 6 illegal → 409, mirroring `reactivateAccount`'s guard), `SUSPENDED` added to the status enum, "Suspend has no enforcement consumer yet" stated explicitly, context→tenant 1:1 rationale (via `accounts.business_units`'s unique `context` column + refdata's `domain.Context.Tenant`), a new 26a1 append-only audit-events sub-phase mirroring BR-AC11, document storage decided as metadata-only (a `reference` string, no file bytes), `expiresAt` added to `ComplianceDocument`, and `coverageCents` restored onto the *document* (not the profile — V2's `TransporterDocumentEntity.coverage` lives there, correcting this memory's earlier #6-era assumption).

**Correction found during independent verification, since resolved:** the review's own 26d "REST-only, no NATS, no tenants package" transport conclusion contradicted its own finding that `vehicleTypeCode` must validate against refdata's corpus — `BUSINESS_RULES-REFDATA.md`'s BR-D28 forbids any REST fallback for backend-to-backend calls, and `refdataconsumer.New(nc *nats.Conn)` shows that call needs a tenant-scoped NATS connection (Phase 21's account-import model). **Resolved 2026-08-13: build the same `pricing-service`-style tenant-scoped NATS wiring (`internal/tenants`, `NATS_CREDS_DIR`, creds volume) for that one outbound `rpc.*` call**, keeping only the Admin-UI-facing surface REST-only. Also pinned down: `/api/trading-partners/{context}/...` follows pricing/refdata's context-scoped route shape, not accounts-service's flat shape, since `TradingPartner` carries `context`.

**Phase 26 is now fully IMPLEMENTED (2026-08-13), all sub-phases 26a-26e, live-verified against the real composed stack including in-browser.** BR-TP01-BR-TP14 confirmed and implemented in `demos/01-dictionary/backend/trading-partner-service/`; Admin UI's "Trading partners" section built (`TradingPartnersPanel.vue`). See `BUSINESS_RULES-TRADING-PARTNER.md` for the full rule text and per-sub-phase verification notes. Remaining items are deliberately deferred, not gaps: lifecycle-as-CQRS/temporal exploration, `ComplianceDocument` temporal classification, document-expiry-driven status, real file storage, terminal/offboarding state, platform-identity vs tenant-membership split, `notify.*` publication once a marketplace consumer exists.

**Phases 26f-26h also done (2026-08-13):** 26f split the Admin nav into
collapsible PLATFORM/SYSTEM groups (Trading partners → Shippers/Transporters);
26g registered the service via `micro.AddService` so it appears in the Services
panel; 26h moved the Admin UI onto `api.{context}.trading-partner.*` with REST
kept as a dual transport.

**Two corrections worth remembering, both from over-scoping on a misread:**

1. **Historical Admin transport, since superseded.** Phase 26h used Admin's
   then-existing tenant connection for organizations `api.*`; Phase 36 moved
   those screens to Tech Lab Operator and its own `refdata-tenant` connection.
   Admin's tenant connection was later removed. Admin now has one PLATFORM
   connection whose publish allowlist contains only three read-only refdata
   subjects; this history must not be used to infer current Admin permissions.
2. **Scoped signing keys are not in play** (`provisioner.go` uses the unscoped
   `SigningKeys.Add`; resolver JWTs carry flat key arrays), so a user JWT's own
   permissions are authoritative. [[nats_scoped_signing_keys]] describes
   *proposed* future work, not current state — a forward-looking memory that
   reads as present-tense.

**How to apply:** use this as the working precedent for the next
Trading-Partner-adjacent phase (e.g. the marketplace/tender phase that will
finally give BR-TP04's `Suspend` an enforcement consumer, per
[[linebooker_bid_tender_allocation_rules]]) — and note that phase is also when
`rpc.*` endpoints become justified here, since it brings the first backend
caller.
