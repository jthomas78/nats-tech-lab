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
  TraceWaterfall.vue` (BR-035–037). BR-045–049 (Phase 43, CONFIRMED
  2026-08-25 — design approved, reviewed, amended, and cleared for
  implementation) add a sibling `obs.pubsub.*` channel for `evt.*`/`notify.*`
  fire-and-forget traffic, complementing `obs.trace.*`'s request/reply
  coverage — instrumented **in the `evt.*` seam** and at each `notify.*` call
  site, with BR-049 making that coverage a checked convention rather than a
  remembered one; see
  [ARCHITECTURE-OBSERVABILITY.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-OBSERVABILITY.md)
  (ADR-047) for the full design.
  BR-051–054 (Phase 48, PARTLY IMPLEMENTED 2026-08-26 — 051 done in
  48b/48c, 052 retired, 053's bound in 48f, 054's harness in 48h and its
  traces panel in 48c; 053's write shape and 054's Messages panel
  outstanding) do the
  same for the *other* channel: a trace span's tenant comes from its arrival
  subject and never from its envelope, and is stored per span (BR-051);
  BR-052's first-writer-wins guard was retired the day it landed, because a
  tenant span and a PLATFORM span under one `traceId` is the most ordinary
  cross-account trace in the stack, not a dispute; `trace-request-reply`
  becomes a bounded window
  written one idempotent span at a time (BR-053), and both panels name the
  originating account, proven by a multi-span cross-account harness
  (BR-054). See BR-AC36, ACCOUNTS file, for the JWT remap they depend on.
  BR-056–057 (Phase 49, IMPLEMENTED 2026-08-26) fix the arithmetic that
  panel draws with: a span carries no start time on the wire, so a start is
  derived as finish minus duration, and publishing that duration truncated
  to whole milliseconds biased every derived start LATE — worst for the
  longest span, which is the root. The wire field is now `durationUs`
  (BR-056, no millisecond field beside it), and the waterfall clamps a
  child's bar never to start left of its parent's (BR-057) so the picture
  stays possible under the clock skew that microseconds cannot fix.
- **[BUSINESS_RULES-REFDATA.md](BUSINESS_RULES-REFDATA.md)** — Reference Data
  Service (BR-D01–BR-D48, BR-D39 being the Phase 28 `obs.trace.*` mirror of
  BR-036 and BR-D45 (Phase 43a, CONFIRMED) pointing this service's `evt.*`
  seam and its `notifybridge.go` `notify.*` call site at the new
  `obs.pubsub.*` hook, BR-045). BR-D46–BR-D48
  (Phase 38d-ii) add the `region` corpus and the Country → Region hierarchy
  behind Operating Areas — expressed entirely in the existing
  `DictionaryReference` mechanism with **no schema change**, and forbidding
  V2's duplicate-row-per-language anti-pattern in favour of localizations
  (consumed by BR-TP46–BR-TP50 in the ORGANIZATIONS file). Rules live in
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
  management subjects. BR-AC34 (Phase 43a, CONFIRMED
  2026-08-25) mirrors BR-AC30 for a second
  Stream export, `obs.pubsub.>` — but imported by PLATFORM **with** a
  `monitor.{tenant}.pubsub.>` local remap, unlike `obs.trace.>`: without one,
  N tenants' streams land on one local subject and the importer cannot tell
  which account a message came from, which is the whole point of the Messages
  panel. It also instruments this service's own four
  `notify.accounts.account.*` publishes. BR-AC36 (Phase 48a, APPROVED
  2026-08-26 — not yet implemented) retrofits that same remap onto the
  `obs.trace.>` import, which BR-AC34 explicitly left out of scope; the
  consuming side is BR-051–054, SHIPPING file.
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
- **[BUSINESS_RULES-ORGANIZATIONS.md](BUSINESS_RULES-ORGANIZATIONS.md)**
  — Organizations Service (BR-TP01–BR-TP15, confirmed 2026-08-13; BR-TP15
  added Phase 28 as the `obs.trace.*` mirror of BR-036, prototyped in this
  service first): Shipper/Transporter registration in a new
  `organizations-service`, ported from V2's `BusinessEntity`/
  `TransporterProfileEntity`/`TransporterDocumentEntity`/`FleetAssetEntity`.
  Covers only the `Organization` aggregate's
  `Register`→`Activate`→`Suspend`→`Reactivate` lifecycle (mirrors
  accounts-service's create/suspend/reactivate triple, BR-AC08–AC10) and its
  append-only audit trail (mirrors BR-AC11's conventions verbatim). Plain
  Postgres CRUD, not event-sourced. Compliance documents and Transporter
  fleet assets (with refdata-validated `vehicleTypeCode`, requiring
  `organizations-service` to hold a tenant-scoped `rpc.*` client per
  BR-D28) get their own BR-TP numbers once Phase 26's later sub-phases
  start. BR-TP18–BR-TP45 (Phase 38a–38c-ii) add the event-sourced
  `TransporterProfile` aggregate, its Temporal vetting saga, editable Company
  Information, and Object Store-backed document files. BR-TP46–BR-TP55
  (Phase 38d-ii) add operating areas (BR-TP46–BR-TP50 — Country/Region
  assignment over BR-D46–BR-D48's corpus, overlap rejection, and freely
  editable post-`Vetted` coverage) and tracking credentials (BR-TP51–BR-TP55
  — the secret lives only in an at-rest-encrypted KV bucket and never on the
  event log, credentials overwrite where documents are write-once, and the
  configured flag extends `AvailableForAssignment`). BR-TP75 (Phase 43a,
  CONFIRMED 2026-08-25) points this service's `evt.*` seam
  (`JetStreamEventStore.append`) at the `obs.pubsub.*` hook, BR-045; its
  transporter-profile payloads are the priority case for BR-046's redaction
  review. Rules will live in
  `organizations-service/organizations/internal/domain/`.

When CLAUDE.md's Quality Rule #4 says "update `BUSINESS_RULES.md`," it means:
add/edit the rule in whichever of the domain files above matches the domain
the change touches. This index file itself should stay a pointer — don't add
rule detail here.
