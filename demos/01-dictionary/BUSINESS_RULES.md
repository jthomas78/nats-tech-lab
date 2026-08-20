# Business Rules — Index

Split by domain so a rule add/edit only requires reading its own file:

- **[BUSINESS_RULES-SHIPPING.md](BUSINESS_RULES-SHIPPING.md)** — Ship + Container
  aggregates on the `SHIPPING` stream (BR-001–BR-022), plus guards, AIS status,
  container status tables, the Phase 15–18 `api.*`/`notify.*` transport rules
  (BR-023–BR-027), the Phase 17c Admin UI presentation rule (BR-028 —
  scoped to what the Connections/Services panels *display*, not the wire),
  the Phase 16g Sea Freight Flow presentation rule (BR-029 — Fleet
  Management, Ships at Port, and Terminal Yard panels show a loading state,
  not an empty one, mid tenant/context switch), and the Phase 16h
  reactive-provisioning rule (BR-030 — a tenant minted by accounts-service is
  immediately usable, no operator/restart needed; see BR-AC08, ACCOUNTS
  file, for the publishing side), and the Phase 16i reactive-teardown rule
  (BR-031 — a tenant suspended by accounts-service stops holding
  shipping-service resources open instead of reconnect-looping forever; see
  BR-AC09, ACCOUNTS file, for the publishing side), and the Phase 16j
  reactive-restore rule (BR-032 — a reactivated tenant becomes usable again
  immediately, closing the created/suspended/reactivated lifecycle triple;
  see BR-AC10, ACCOUNTS file), and the Phase 16k connection-honesty rule
  (BR-033 — the status badge reflects the NATS connection, and command
  failures name the cause rather than the transport symptom), and the
  Phase 27 Admin UI presentation rule (BR-034 — the new Account Activity
  panel proxies `/accstatz` per account and renders `slow_consumers` as a
  silent-until-nonzero alarm, not a routine stat), and the Phase 28
  distributed-tracing rules (BR-035 — the Request/Reply & Traces panel's
  `[traces]` presentation; BR-036 — the `obs.trace.*` envelope contract,
  PLATFORM-only publishing, redact-before-truncate; BR-037 — trace
  propagation on every outbound message, one span per logical RPC call), and
  the Phase 31 Shape B consolidation rule (BR-038 — the ship list is served
  from the Postgres projection; KV is a per-entity cache, never a list source).
  Rules live in `dictionary/internal/domain/` (BR-001–022),
  `dictionary/internal/browserrpc/` + `dictionary/internal/eventhandler/`
  (BR-023–024, 026–028), `internal/refdataconsumer/` (BR-025, 027),
  `frontend/seafreight-app/src/stores/port.js` (BR-029),
  `dictionary/internal/rest/tenant.go` + `dictionary/composition.go`
  (BR-030–032), `frontend/seafreight-app/src/App.vue` +
  `src/nats/useNatsConnection.js` (BR-031, BR-033),
  `observability-service/observability/internal/rest/nats_connections.go`
  (moved from shipping-service's `dictionary/internal/rest/nats_ops.go`,
  Phase 30h) + `frontend/admin/src/components/AccountsOverviewPanel.vue`
  (BR-034, now Accounts' `Overview` tab rather than a standalone nav item —
  Phase 45; see also BR-043/BR-044 for that phase's history/search rules),
  and
  `dictionary/internal/natstrace/` + `frontend/admin/src/components/
  TraceWaterfall.vue` (BR-035–037). BR-045–048 (Phase 47, PROPOSED — design
  approved, implementation on hold) add a sibling `obs.pubsub.*` channel for
  `evt.*`/`notify.*` fire-and-forget traffic, complementing `obs.trace.*`'s
  request/reply coverage; see
  [ARCHITECTURE-OBSERVABILITY.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-OBSERVABILITY.md)
  (ADR-047) for the full design.
- **[BUSINESS_RULES-REFDATA.md](BUSINESS_RULES-REFDATA.md)** — Reference Data
  Service (BR-D01–BR-D45, BR-D39 being the Phase 28 `obs.trace.*` mirror of
  BR-036 and BR-D45 (Phase 47a, PROPOSED) pointing this service's `evt.*`
  publish site at the new `obs.pubsub.*` hook, BR-045). Rules live in
  `backend/refdata-service/refdata/internal/domain/dictionary.go`.
- **[BUSINESS_RULES-ACCOUNTS.md](BUSINESS_RULES-ACCOUNTS.md)** — Accounts
  Service (BR-AC01–BR-AC32): NATS account provisioning, suspension,
  reactivation, reserved-name protection via decentralized JWTs, (BR-AC08)
  publishing `notify.accounts.account.created` so shipping-service can react
  to a newly-minted tenant immediately (see BR-030, SHIPPING file, for the
  consumer side), and (BR-AC09) the mirrored `notify.accounts.account.suspended`
  for a suspended tenant (see BR-031, SHIPPING file) and
  `notify.accounts.account.reactivated` (BR-AC10, see BR-032) completing the
  lifecycle triple, and (BR-AC11) an append-only Postgres audit trail
  (`accounts.audit_events`) recording actor/outcome/metadata for every
  lifecycle action, (BR-AC12) runtime update of a tenant's JetStream
  resource limits via `POST /api/accounts/{name}/jslimits`, re-minting the
  account JWT via `$SYS.REQ.CLAIMS.UPDATE` and persisting to Postgres, and
  (BR-AC14–BR-AC29) business-unit registration, context-slug immutability,
  JWT TTL policy, and import/export health reporting added Phases 21-22.
  BR-AC30 (Phase 28) adds `allow_trace: true` and a per-tenant `obs.trace.>`
  stream export to every minted tenant account JWT, never to a browser JWT.
  BR-AC31 (Phase 30a) adds a per-tenant `$SRV.>` service export (imported by
  PLATFORM with a `monitor.{tenant}.srv.>` local remap) so cross-account
  service discovery for `observability-service` can reach every tenant.
  BR-AC32 (Phase 30b) adds six narrow, explicit `$JS.API` service exports
  (imported by PLATFORM with a `monitor.{tenant}.js.>` local remap) for
  read-oriented JetStream/KV introspection, deliberately excluding stream
  management subjects. BR-AC34 (Phase 47a, PROPOSED — design approved,
  implementation on hold) mirrors BR-AC30 for a second Stream export,
  `obs.pubsub.>`, needing no local remap for the same reason `obs.trace.>`
  doesn't.
  Rules live in `backend/accounts-service/accounts/handler.go`,
  `provisioner.go`, `store.go`, `audit.go`, and `jwt.go`.
- **[BUSINESS_RULES-PRICING.md](BUSINESS_RULES-PRICING.md)** — Pricing
  Service (BR-P01–BR-P25, the last being the Phase 28 `obs.trace.*` mirror of
  BR-036): all three of the ported source aggregates —
  `FeeScale` (BR-P01–BR-P06), `RateSheet` (BR-P07–BR-P12, BR-P17–BR-P24),
  `FixedRate` (BR-P13–BR-P15) — now have a domain model, each following the
  same draft/published/rolled-back version lifecycle (reusing
  refdata-service's corpus pattern, duplicated per aggregate rather than
  shared). Notable fixes to real fail-open bugs found in the ported source
  system: a bid above every FeeScale range now errors instead of charging
  zero fee (BR-P05), and active-version selection is unambiguous
  highest-published-version everywhere, not the source's
  insertion-order/earliest-version fallbacks (BR-P06/BR-P09/BR-P14). Ported
  from a real freight marketplace's `RateSheet`/`FeeScale`/`FixedRate`
  domain. Unlike refdata-service, this domain is write-adjacent (a fee
  calculation sits on a load-accept path in the source system), so it is
  its own service rather than a refdata-service extension. Postgres/REST
  wiring is live (own `pricing-postgres` container, port 5435;
  `pricing-service` on port 7203) and verified end to end. Phase 25e
  resolved cross-service wiring in favor of the Sea Freight Flow *browser*
  talking to pricing-service directly — `shipping-service` never consults
  it — and Phase 25f built that path: an `api.*` adapter over one NATS
  connection per tenant (`internal/tenants`), verified live against real
  tenant creds. Phase 25g added a `List` per aggregate (BR-P16, excludes
  soft-deleted FeeScales) and a Sea Freight Flow "Pricing" tab that
  bootstraps off it — no live `notify.*` updates yet, since pricing-service
  publishes no change stream. Phase 25h added the manual-entry UX on top —
  register/create-draft/add-range-or-entry/publish/rollback for all three
  aggregates, live-verified in-browser end to end. Phase 25i added the
  effective-dated diesel overlay for `RateSheet` (BR-P17–P24): a `major.minor`
  two-axis version identity, a context-scoped diesel price index
  (`pricing.diesel_prices`), auto-appended contiguous overlay windows
  (`pricing.rate_sheet_overlays`), and `RateForLoad` date-interval resolution
  — backend fully implemented, live-verified via docker compose, and a
  Pricing-tab frontend surface (diesel price index/list, per-sheet "Apply
  Diesel Overlay" control, `major.minor` + overlay-window display) shipped
  and live-verified in-browser. The docker-compose smoke test surfaced a
  real bug fixed in the same phase: entries with no authored diesel
  baseline (`InitialDieselCents == 0` — true of every pre-25i seeded rate
  sheet) were silently corrupted to a $0 adjusted rate by a division-by-zero
  NaN-to-int64 conversion; BR-P24 now skips overlaying those entries
  instead. Rules live in
  `backend/pricing-service/pricing/internal/domain/fee_scale.go`,
  `rate_sheet.go`, and `fixed_rate.go`.
- **[BUSINESS_RULES-TRADING-PARTNER.md](BUSINESS_RULES-TRADING-PARTNER.md)**
  — Trading Partner Service (BR-TP01–BR-TP15, confirmed 2026-08-13; BR-TP15
  added Phase 28 as the `obs.trace.*` mirror of BR-036, prototyped in this
  service first): Shipper/Transporter registration in a new
  `trading-partner-service`, ported from V2's `BusinessEntity`/
  `TransporterProfileEntity`/`TransporterDocumentEntity`/`FleetAssetEntity`.
  Covers only the `TradingPartner` aggregate's
  `Register`→`Activate`→`Suspend`→`Reactivate` lifecycle (mirrors
  accounts-service's create/suspend/reactivate triple, BR-AC08–AC10) and its
  append-only audit trail (mirrors BR-AC11's conventions verbatim). Plain
  Postgres CRUD, not event-sourced. Compliance documents and Transporter
  fleet assets (with refdata-validated `vehicleTypeCode`, requiring
  `trading-partner-service` to hold a tenant-scoped `rpc.*` client per
  BR-D28) get their own BR-TP numbers once Phase 26's later sub-phases
  start. Rules will live in
  `trading-partner-service/tradingpartner/internal/domain/`.

When CLAUDE.md's Quality Rule #4 says "update `BUSINESS_RULES.md`," it means:
add/edit the rule in whichever of the domain files above matches the domain
the change touches. This index file itself should stay a pointer — don't add
rule detail here.
