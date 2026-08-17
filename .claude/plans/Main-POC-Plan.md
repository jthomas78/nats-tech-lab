# nats-tech-lab — Implementation Plan

## Purpose

A lab application for evaluating NATS.io patterns in the context of a V3 greenfield logistics platform. Each demo is self-contained: the user picks a pattern from the lab shell, reads an intro, launches the demo (Docker), and shuts it down when done.

The core architectural question being investigated: **what is the correct responsibility split between JetStream (event backbone), NATS KV (fast lookup/watch/cache), Postgres (transactional source of truth), and CQRS projections?**

**Project goal — Dictionary (shared reference/master data):** a central repository for lookup values used throughout the platform — vehicle types, order statuses, currencies, units of measure, trailer types, Incoterms, hazard classes, countries, etc. — delivered as a separate service with localization, typed cross-references, and a versioned NATS-KV cache. See [Dictionary-Service-Plan.md](Dictionary-Service-Plan.md) (Phase 11, approved 2026-07-13).

---

## Project Structure

```
nats-tech-lab/
  lab-shell/              # Vue 3 + PrimeVue + Pinia frontend (demo menu + intro pages)
  demos/
    01-dictionary/        # First demo: Dictionary POC
      backend/            # Go service (hexagonal layout, borrowed from Fizmath Plaza)
      frontend/           # Vue 3 demo UI (isolated, own docker-compose)
      docker-compose.yml  # Spins up: Postgres + NATS + backend + frontend
      README.md           # Intro text shown in lab shell
```

---

## Lab Shell (Phase 1)

**Stack:** Vue 3 + PrimeVue v4 + Pinia

**Responsibility:** A simple menu listing available demos. Each entry shows:

- Demo title and one-line description
- A "Launch" button that opens the demo UI in a new tab (or iframe — decided: new tab for Phase 1)
- A brief intro page explaining the pattern being demonstrated

**Key design note:** Pinia stores are intentionally used as a frontend analogue to server-side materialized views. Both are projected read models derived from an event source — just at different layers (KV/Postgres on server, Pinia in browser). This parallel should be explicit in the UI and docs.

**Phase 1 scope:** Static menu + intro pages only. No live status. Microfrontend integration is out of scope.

---

## UI Styling — UniFi Aesthetic

**Library:** PrimeVue v4 (Vue 3-only). Start from the **Aura preset** (darkest built-in) and override `--p-*` CSS tokens.

**Design target:** UniFi Network Application — dark, data-dense, angular. Not a pixel-perfect clone; enough fidelity to evoke the aesthetic.

**Verified starting tokens** (community reverse-engineered from proxmorph; text colors survived adversarial verification, background/accent did not — extract those from a live UniFi instance via devtools):

```css
/* Text — medium confidence (proxmorph, 2-1 vote) */
--p-text-color:          #DEE0E3;   /* primary text */
--p-text-muted-color:    #B7BCC2;   /* secondary / label */
--p-text-disabled-color: #737C87;   /* disabled / hint */

/* Background + accent — extract from live UniFi instance */
/* Open UniFi Network App → devtools → inspect :root for --ubnt-* or --unifi-* */
```

**Dark mode:** PrimeVue v4 activates dark mode via `document.documentElement.classList.toggle('p-dark')` — the same class-toggle pattern UniFi uses (`.ubnt-mod-dark` on `body`). Default to dark.

**Data tables:** Use `<DataTable size="small">` — supports frozen columns, row grouping, multi-level headers, and lazy loading, matching the density of UniFi's grid views.

**Shared theme file:** Both `lab-shell/` and `demos/01-dictionary/frontend/` import the same custom Aura-based preset so styling stays in sync across frontends.

**Shared page shell (topbar + sidebar):** documented contract in [`shared/unifi-theme/LAYOUT.md`](../../shared/unifi-theme/LAYOUT.md); extraction into real shared code (`AppShell.vue`) is scoped in [AppShell-Extraction-Plan.md](AppShell-Extraction-Plan.md) (PROPOSED — awaiting approval).

---

## Demo 01 — Dictionary POC

### Problem

Dictionary/reference data (UI dropdowns, enums, locale config, tenant config, CQRS read-model lookup data) needs to be:

- Derived from an event source
- Returned based on application context (company/business unit; locale resolved at read time — see § Phase 16: tenant and region are separate axes, not part of `{context}`)
- Available with low latency

### Two Shapes to Compare Side-by-Side

#### Shape A — NATS KV as the Read Model

- Event handlers project directly from JetStream into KV
- Dictionary reads go straight to KV
- No Postgres-backed read table involved
- KV key format: `{context}:{entityType}:{id}` (e.g. `en-GB:currency:GBP`)

#### Shape B — NATS KV as Cache in Front of Postgres

- Canonical CQRS projection lives in Postgres (the write-side event sourcing table)
- KV is a derived, low-latency cache/distribution layer
- Eager write-through: the JetStream handler upserts Postgres then immediately overwrites the KV entry with the persisted value
- Cache miss falls through to Postgres

### Backend (Go)

Borrow from Fizmath Plaza: jstream wrapper, waiter, monolith composition, hexagonal layout, Docker Compose setup. **Do not retrofit Fizmath — start fresh.**

**Key differences from Fizmath Plaza:**

- Stream retention: `LimitsPolicy` (not `InterestPolicy`) — required for event replay
- Add NATS KV store usage (Fizmath has none)
- Context-aware key design (context in key prefix; see § Phase 16 — context is company/business-unit, not tenant or region)
- No gRPC-Gateway needed for this demo — plain HTTP REST is fine

**Domain structure:**

```
demos/01-dictionary/backend/
  cmd/main.go               # bootstraps monolith, calls Startup on each module
  internal/monolith/        # Monolith + Module interfaces (ported from Fizmath)
  internal/jstream/         # JetStream wrapper with LimitsPolicy
  internal/kvstore/         # NATS KV wrapper
  dictionary/
    composition.go
    internal/
      domain/               # DictionaryEntry entity, events, repo interface
      application/
        commands/           # CreateEntry, UpdateEntry
        queries/            # GetEntry (Shape A: from KV, Shape B: from KV→Postgres)
      postgres/             # repo implementation (Shape B only)
      eventhandler/         # JetStream consumer → projects into KV (both shapes)
      rest/                 # HTTP handlers
```

### Stream Design

```
Stream name:    DICTIONARY
Subjects:       DICTIONARY.entry.created
                DICTIONARY.entry.updated
Retention:      LimitsPolicy (enables replay)
Storage:        File
```

### KV Bucket Design

```
Bucket names:   dict-a    (Shape A read model — one bucket per tenant, shared across contexts)
                dict-b    (Shape B cache — one bucket per tenant)
                container (container projection)
                meta      (cross-cutting lookup sets)
Key format:     {context}.{entityType}.{id}
Value:          JSON-encoded DictionaryEntry / ContainerState / metadata
```

Context isolation within a tenant is enforced by key prefix: every key is
stored as `{context}.{entityType}.{id}`, and `ListKeysFiltered` / `kv.Watch`
filter on `{context}.>` to scope each operation to its own context. The NATS
KV key character constraint (`[-/_=.a-zA-Z0-9]`) means `.` is the separator.

> **Phase 20b note:** Prior to Phase 20b, buckets were named
> `dict-a-{context}` (one per business-unit context per tenant). Phase 20b
> collapsed these to one shared bucket per role per tenant. Each new context
> no longer consumes additional NATS streams, which was the trigger for the
> `js_max_streams` exhaustion incident (see BR-AC12).

### Frontend (Demo UI)

Isolated Vue 3 app inside `demos/01-dictionary/frontend/`. Own docker-compose service.

Two panels side by side:

- **Shape A panel** — reads from KV directly; shows key, value, KV sequence
- **Shape B panel** — reads from KV cache with Postgres fallback; shows cache hit/miss

A form to create/update a dictionary entry fires a command to the backend, which publishes an event. Both panels update reactively (KV watch → SSE or WebSocket → frontend).

---

## Docker Compose Strategy

Each demo has its own `docker-compose.yml`. Lab shell has its own. They do not share networks.

```yaml
# demos/01-dictionary/docker-compose.yml services:
  nats:       nats:latest with JetStream enabled
  postgres:   postgres:16
  backend:    built from ./backend
  frontend:   built from ./frontend
```

Tear-down is `docker compose down` inside the demo directory.

---

## Implementation Phases

### Phases 0–11 — Completed

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original rationale
or checklist detail for a specific completed phase).

- [x] Phase 0 — Scaffolding (Go module, monolith interfaces, JetStream/KV wrappers, docker-compose)
- [x] Phase 1 — Shape A (KV-only read model)
- [x] Phase 2 — Shape B (KV cache + Postgres projection)
- [x] Phase 3 — Demo Frontend (Vue 3 + PrimeVue, side-by-side Shape A/B panels)
- [x] Phase 4 — Lab Shell (demo menu + intro pages)
- [x] Phase 5 — Data-Flow Vertical Layout Redesign (JetStream panel, event log filters)
- [x] Phase 6 — Shipping Domain + Shape C (Event Sourcing Reconstruction) — Ship/Port/Cargo domain, ShapeCPanel
- [x] Phase 7 — Swagger/OpenAPI + Ginkgo Test Runner
- [x] Phase 8 — Two-Aggregate Domain + Terminal + Port Frontend (single stream) — Container aggregate, BR-008–BR-016, `frontend-port`
- [x] Phase 8.2 — Ship Management Split View, Fleet Panel, Yard Split, BR-016
- [x] Phase 8.3 — Surrogate Key (UUID) for Container
- [x] Phase 9 — Subject Taxonomy + Doc Realignment (as-built at the time: `{region}.events.{tenant}.{aggregate}.{id}.{event}` — since superseded twice, first by `evt.{context}.…` and then by § Phase 16, which removed tenant and region from subjects entirely)
- [x] Phase 9.5 — Ports Reference Table (BR-017, BR-018)
- [x] Phase 9.6 — Postgres Tables Admin Panel (Reference Data → Ports)
- [x] Phase 10 — Performance Baseline (pull-forward, pre-Phase 11/15) — k6 harness, Shape C/hydration/throughput baselines
- [x] Phase 11 — Dictionary as a Service (APPROVED 2026-07-13) — see [Dictionary-Service-Plan.md](Dictionary-Service-Plan.md); sub-phases 11.1–11.11 all delivered

**Verification status (2026-07-09):** full compose stack runs end to end (5 services), Swagger UI live, both frontend `/api` proxies working, live smoke test of full container lifecycle passing, `go build`/`go vet`/`ginkgo ./...` (22/22 at that point) and both frontend builds green. Full detail in the archive.


### Phases 12–14 — Completed

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original rationale
or checklist detail for a specific completed phase).

- [x] Phase 12 — Refdata Versioning, Tenancy & Template Inheritance (sub-phases 12.1–12.11) — see
      [Refdata-Versioning-Tenancy-Design.md](Refdata-Versioning-Tenancy-Design.md); corpus-level
      versioning, draft/publish/rollback, multi-level template inheritance, hybrid KV
      materialization, subject taxonomy (`evt.{context}.{service}.{entity}.{id}.{event}`), Ship
      surrogate UUID identity, and dual-transport `rpc.*` (later made the sole backend-to-backend
      transport, no REST fallback). BR-V01–V08, BR-020–022, BR-D22, BR-D25/D26/D28.
- [x] Phase 13 (13a/13b) — NATS Accounts Tenancy Spike: proved server-enforced per-account
      JetStream isolation (separate streams/KV per account) with a static `accounts{}` block;
      `shipping-service` gained a tenant-scoped connection swap (topbar tenant selector).
      `refdata-service` excluded (needs exports/imports for cross-tenant sharing — deferred).
      Findings recorded in `ARCHITECTURE.md`. No numbered BR — infrastructure invariant, not a
      domain rule.
- [x] Phase 14 (14a/14b/14c) — Accounts Service & Decentralized JWT Tenancy: replaced the static
      accounts block with NATS operator mode (`resolver: full`, `.creds` files for every service);
      new `accounts-service` (own Postgres, port 5434) mints/suspends/reactivates NATS accounts at
      runtime via `jwt/v2`+`nkeys` and `$SYS.REQ.CLAIMS.UPDATE`/`DELETE`; "Platform > Accounts" page
      in the admin UI. See [accounts_service_plan.md](../memory/accounts_service_plan.md) and
      [BUSINESS_RULES-ACCOUNTS.md](../../demos/01-dictionary/BUSINESS_RULES-ACCOUNTS.md) (BR-AC01–06)
      for the full lifecycle including later-added reactivation and the no-hard-delete decision.

---

### Phases 15–19 — Completed

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original rationale
or checklist detail for a specific completed phase).

- [x] Phase 15 (15a/15b/15c/15d) — Browser NATS WebSocket Transport: one WebSocket connection
      replaces SSE/REST for Sea Freight Flow (`rpc.*`/`notify.*`), shipping-service natsrpc
      server, `auth-service` mints short-lived browser JWTs (later merged into accounts-service,
      Phase 19)
- [x] Phase 16 (16a–16k) — Subject Taxonomy & Tenancy Formalization: settled `{context}` =
      company/business-unit (not tenant/region), NATS account = the only tenancy boundary; `api.*`
      migration, reserved-name enforcement (BR-AC07/BR-D33), refdata context tree + dynamic context
      list, reactive tenant provisioning/teardown/restore (BR-030–033/BR-AC08–10), Sea Freight Flow
      connection-honesty fixes (BR-033)
- [x] Phase 17 (17a/17b/17c) — Request/Reply Panel v2, Connections + Services Panels: obs envelope
      gained headers/timestamp/size (BR-D36/BR-026); panel rebuilt with filtering, facets, paired
      Request/Reply detail; new Connections/Services admin panels (BR-028) with account→tenant
      labeling and coverage-audit-driven Vitest infra for `frontend/admin`; Connections' Total card
      now reads `N / max_connections` (from `/varz`) with a capacity bar under it, amber at 80%,
      and surfaces `/connz`'s paging envelope only when a response actually paged — the page size
      is never framed as a connection ceiling
- [x] Phase 18 — Requestor/Responder Identity Headers: `Nats-Requestor`/`Nats-Responder` headers
      (BR-D37/BR-027) identify caller/responder instance on every `rpc.*`/`api.*` call
- [x] Phase 19 — Merge auth-service into accounts-service: `auth-service` had no independent state
      or connection beyond `accounts.Store`, so its routes moved into accounts-service's own process
      and its separate binary/compose service were removed

---

### Phase 20 (20a DONE 2026-08-03, 20b IN PROGRESS) — JetStream Account Limits: Update, Visibility, and Stream-Count Redesign

#### Goal

Raised as "Gap #5" from the 2026-08-03 accounts architecture review: the `acme` account hit `js_max_streams=10` with zero headroom (`SHIPPING` + 4 KV buckets × 2 contexts already provisioned), silently wedging the Shape A/B projectors into an infinite Nak/redeliver loop for a new context — a tenant minted at runtime had no way to raise its own limits, and no way to see usage against them beforehand. Two parts, per Synadia's own operational guidance for this failure mode: (20a) make limit changes a routine, supported, monitored operation; (20b) stop the *design* from needing ever-larger limits by collapsing the per-`{context}` stream fan-out so a tenant's stream cost is fixed regardless of how many business-unit contexts it adds.

#### Phase 20a — accounts-service: update + visibility (DONE 2026-08-03)

- [x] `Provisioner.UpdateAccountLimits` + `Store.SetJSLimits` (mirrors the existing `ReactivateAccount` pattern)
- [x] `POST /api/accounts/{name}/jslimits` handler + route + `AuditActionJSLimitsUpdated` — see **BR-AC12**, `BUSINESS_RULES-ACCOUNTS.md`
- [x] `GET /api/accounts/usage` — bulk JetStream usage endpoint (`accounts/jsusage.go`, `GET {NATS_MONITOR_URL}/jsz?accounts=true` joined against `Store.List()`'s limits)
- [x] Ginkgo specs: successful update reflected via `GET`/live `AccountInfo()`, 404 unknown account, negative-value rejection, audit row, notify event
- [x] `BUSINESS_RULES-ACCOUNTS.md` BR-AC12 entry; `BUSINESS_RULES.md` index bump
- [x] `ginkgo ./...` from `backend/accounts-service/` green

#### Phase 20b — Admin UI + shipping-service kvstore redesign (DONE 2026-08-03)

- [x] Admin UI: usage column (colored above 80%/100%) + Edit Limits dialog reusing the Create Account dialog's four `InputNumber` fields, calling the Phase 20a endpoint
- [x] `internal/kvstore/kv.go`: collapse per-`(role, context)` bucket to one bucket per role per **tenant** (bucket name = `s.prefix`, already tenant-scoped since `kvstore.New(js, prefix)` is called per-tenant); `Put`/`Get`/`Delete` build the real key as `kvContext + "." + key`; `Keys`/`Watch` switch to a context-filtered `ListKeysFiltered(ctx, kvContext+".>")` and equivalent filtered `Watch` (NATS KV keys with `.` become multi-token subjects internally)
- [x] `queries/terminal.go`'s `Terminal.List`/`ListByPort`/`ListByShip` and Shape A's `ListShips`: no code change needed — isolation now comes from the filtered `Keys()` call above, not bucket separation
- [x] Migration: since `SHIPPING` retention is `LimitsPolicy` (full replay), delete the old per-context KV buckets and let the Shape A/B projector consumers replay `SHIPPING` from the start to repopulate under the new key scheme — no custom migration script
- [x] Tests: `internal/natsaccounts/isolation_test.go`'s `TestKVBucketIsolation` doc comment already describes the target shape (one bucket, no context suffix) — confirm it now matches shipped code; add specs asserting two contexts under the same tenant get correctly-scoped, non-overlapping `Keys()`/`Watch()` results from a shared bucket
- [x] `BUSINESS_RULES-SHIPPING.md`: rewrite BR-020's bucket-naming clause (bucket names are tenant-scoped, `{context}` is now a KV-key prefix component) and light footnote pass on BR-021/024/030-032
- [x] Swagger doc comments (`dictionary/internal/rest/kv.go`): update `@Description` strings that reference per-context bucket names
- [x] `ginkgo ./...` from `backend/shipping-service/` green
- [x] Live smoke test: reapply acme's existing limits through `POST /api/accounts/acme/jslimits`; `GET /api/accounts/usage` reports `5 / 10` streams (`SHIPPING` + four shared KV buckets); confirm `PHASE20B-ATLANTIC` in `acme-atlantic-fleet` appears in Sea Freight Flow


### Phase 21 (IMPLEMENTED 2026-08-03) — Account Exports/Imports: Two-Account Partitioning (PLATFORM Cross-Cutting, Tenant Data-Plane)

#### Goal

Move cross-account communication from "open a second connection with a second `.creds` file" (today: `accounts-service` holds SYS+PLATFORM, `shipping-service` holds PLATFORM + every tenant account) to NATS's own account-JWT-declared exports/imports — the fix Phase 13's completion note and `ARCHITECTURE-ACCOUNTS.md`'s "Production-scale fix" sketch both flagged and deferred. Target partitioning: **PLATFORM** holds cross-cutting services (`accounts-service`, `refdata-service`) and declares exports; **tenant accounts** (`acme`, `globex`, runtime-minted) hold the data plane (`shipping-service`'s per-tenant `SHIPPING` stream + KV, the browser) and declare matching imports.

Bonus: a service import with a subject remap (tenant publishes a bare local subject; the server stamps the tenant's own account identity onto it) closes a real gap — today `refdataconsumer`'s `{context}` is caller-supplied, so nothing stops a client connected as `acme` from asking for `globex`'s data.

Full design (four export/import declarations, `refdataconsumer`/`provisioner.go`/`bootstrap-operator.sh`/shipping-service changes, test plan, doc updates) recorded in the plan-mode session that produced this phase — see git history / session log for 2026-08-03, or regenerate from this checklist:

#### Checklist

- [x] `bootstrap-operator.sh`: PLATFORM exports/imports and restricted `shipping-admin` user
- [x] `accounts/provisioner.go`: preserve claim wiring and mint tenant imports
- [x] `accounts/handler.go`: fetches PLATFORM public key before minting
- [x] `refdataconsumer/consumer.go`: four fixed local subjects; context-list unchanged
- [x] tenant-scoped consumer and lifecycle subscriptions wired in `tenant.go`
- [x] narrowed admin connection/docs and `NATS_ADMIN_CREDS_PATH`
- [x] Docker admin credential path and tenant-creds exclusion
- [x] import/remap/isolation lifecycle specs
- [x] account-claim wiring/preservation specs
- [x] refdata consumer test harness asserts local-import transport
- [x] account/import preservation business rule
- [x] refdata/shipping rule notes updated
- [x] accounts architecture updated
- [x] `ginkgo ./...` green in both `accounts-service` and `shipping-service`
- [ ] Live verification: `bootstrap-operator.sh --force` + `docker compose down -v && up --build`; refdata labels still resolve for both tenants; Connections/Services panels still show PLATFORM-labeled rows; a tenant-created event still reactively provisions shipping-service resources; crafting the old-style cross-context subject directly now fails/times out

### Phase 22 — Business Units Owned by accounts-service

#### Goal

Business units (the `{context}` scope — `acme-pacific-fleet`, `acme-atlantic-fleet`, …) are currently independent of the tenant/account concept: refdata-service seeds them via its own `seed.go`, with only an unenforced `tenant` metadata column (BR-D34) loosely linking a context back to an account name. There is no registration flow — the two demo business units are a fixed, hardcoded pair, and three frontend stores each carry their own hardcoded `CONTEXTS` fallback array as a result.

This phase makes accounts-service the sole authority for which business units exist per account: registered through the Admin UI's Accounts panel, with a reserved `_default_bu` context that silently covers any account with zero registered business units, and no hardcoded context lists anywhere in the frontend.

#### Design

**Ownership.** accounts-service gains its own `business_units` table (one row per account per business unit: `account_id`, `name`, `visible`, `created_at`) — the authoritative registry a human manages via the Admin UI. refdata-service's existing `contexts` table remains the store every context-consuming read (corpus inheritance, KV/Postgres scoping) already goes through unchanged; accounts-service becomes its *writer* — calling refdata-service's existing `POST /api/refdata/admin/contexts` at BU-creation time — instead of refdata-service seeding a fixed list at its own startup. This keeps the shipping-service/refdata-service read path (`rpc.*.refdata.*` and BR-D35's `ListByTenant`) exactly as it is today; only who writes the row changes.

**Reserved `_default_bu`.** A single shared literal context value (not per-account), seeded once by refdata-service's own `seed.go` alongside `_platform` — the same sharing model `_platform` already uses, safe because tenant isolation is the NATS account boundary, not the context string (Phase 20b). `Parent: PlatformContext`, `Tenant: ""` (untenanted, so BR-D34/BR-D35's `ListByTenant` returns it for every tenant automatically, the same mechanism that already surfaces `_platform`). Requires a second named, sanctioned exception to BR-D33 (the first being `_platform`).

Every account implicitly resolves to `_default_bu` when it has zero registered real business units — accounts-service does not need to create a per-account row for it in refdata-service; it only needs its own per-account `business_units` row (name `_default_bu`, `visible` defaulting to `true`) purely for the Admin UI's own visibility bookkeeping.

**Mutual exclusivity — relaxed, not strict.** `_default_bu` and real business units are not hard-exclusive. Registering an account's first real business unit always surfaces a confirmation prompt in the Admin UI asking whether to hide `_default_bu` (no attempt to detect whether it actually holds data — that would require accounts-service reading into shipping-service/refdata-service's stores, a new cross-service read dependency this phase deliberately avoids). Confirming sets `visible = false` on the account's `_default_bu` row (`PATCH /api/accounts/{name}/business-units/_default_bu`); declining leaves it visible and selectable permanently alongside real business units. `visible` is a toggle, not a delete, and is also directly editable per-row in the Admin UI's business-unit table.

**Migration of `_default_bu`'s underlying data — explicitly deferred.** No migration path ships in this phase. Flagging as a known gap: `_default_bu`'s context id may already be referenced inside published NATS events (JetStream history, KV entries) that a later migration to a named business unit would need to handle without silently orphaning or duplicating data.

**Demo seed data.** refdata-service's `seed.go` drops its own creation of the two demo business units (`PacificFleetContext`/`BusinessUnitContext` constants and the `Register()` calls that create them) — those become the responsibility of accounts-service's own seed step, calling the new BU-creation endpoint for the `acme` account exactly like it already seeds the `acme`/`globex` accounts themselves. The BR-V06/V07 hazard-class override demo data (`3` override, `X1` addition — currently seeded onto the two demo business units) moves onto `_default_bu` instead, since that context is guaranteed to exist at refdata-service's own startup with no ordering dependency on accounts-service's dynamic creation.

**shipping-service.** `dictionary/internal/postgres/migrate.go`'s `seedDefaultPorts` currently hardcodes `contexts := []string{"acme-pacific-fleet", "acme-atlantic-fleet"}` — replaced with seeding default ports only for `_default_bu` (the one context guaranteed to exist for every tenant at every startup). A dynamically-registered real business unit starts with zero ports; the operator adds them via the existing "Add a shipping port" UI action, consistent with "no hardcoded/fallback lists" for business units generally. `listRefdataContexts`'s existing `rpc.*` call is unchanged in transport; refdata-service's `ListByTenant` now filters to `visible = true` server-side (no separate "include hidden" mode needed — the Admin UI's BU management table reads accounts-service's own registry directly, which already carries `visible` per row, not refdata-service's copy).

**Frontend.** The three hardcoded `CONTEXTS` fallback arrays (`frontend/seafreight-app/src/stores/port.js`, `frontend/admin/src/stores/dictionary.js`, `frontend/refdata/src/stores/dictionary.js`) are deleted outright, per this phase's "no fallbacks" requirement — a failed business-unit fetch shows an empty dropdown, not a guessed one. `frontend/admin/src/components/AccountsPanel.vue` gains a business-unit sub-table per account (name, visible checkbox, "Add business unit" action) and the hide-confirmation dialog described above.

#### Checklist

- [x] `accounts-service`: `business_units` table + migration (`account_id`, `name`, `visible` default `true`, `created_at`; unique on `(account_id, name)`)
- [x] `accounts-service`: reserved-name validation mirroring BR-D33 (leading `_` rejected except the literal `_default_bu`); PLATFORM/SYS accounts excluded from BU registration entirely
- [x] `accounts-service`: `_default_bu` row auto-created for every account at account-creation time (`visible: true`)
- [x] `accounts-service`: `GET/POST /api/accounts/{name}/business-units`, `PATCH /api/accounts/{name}/business-units/{buName}` (`{visible: bool}`); `POST` also calls refdata-service's `POST /api/refdata/admin/contexts` to create the matching context row (`tenant` set to the account name, per BR-D34)
- [x] `accounts-service`: demo seed step registers `acme-pacific-fleet`/`acme-atlantic-fleet` for the `acme` account via the new endpoint (replacing refdata-service's retired hardcoded seeding of the same two contexts)
- [x] `refdata-service`: `visible` boolean column on `refdata.contexts` (default `true`); `ListByTenant` filters to `visible = true`
- [x] `refdata-service`: `_default_bu` reserved context — second sanctioned BR-D33 exception, seeded once in `seed.go` (`Parent: PlatformContext`, untenanted)
- [x] `refdata-service`: `seed.go` drops `PacificFleetContext`/`BusinessUnitContext` creation; BR-V06/V07 hazard-class demo override (`3`, `V1`) moves onto `_default_bu`
- [x] `shipping-service`: `migrate.go`'s `seedDefaultPorts` seeds default ports for `_default_bu` only, not a hardcoded business-unit list
- [x] `frontend/admin`: `AccountsPanel.vue` business-unit sub-table (name, visible checkbox, add action) + hide-`_default_bu` confirmation dialog on an account's first real-BU registration
- [x] `frontend`: delete the three hardcoded `CONTEXTS` fallback arrays (`seafreight-app/stores/port.js`, `admin/stores/dictionary.js`, `refdata/stores/dictionary.js`) and any spec asserting their contents
- [x] `BUSINESS_RULES-ACCOUNTS.md`: new BR-AC15/16/17 entries (BU registration, `_default_bu` reservation, visibility toggle semantics)
- [x] `BUSINESS_RULES-REFDATA.md`: BR-D38 (`_default_bu` sanctioned exception) alongside BR-D33; note on BR-D34/BR-D35 that per-tenant BU rows are now accounts-service-authored, not refdata-service-seeded
- [x] `ARCHITECTURE-ACCOUNTS.md` / `ARCHITECTURE-DICTIONARY.md` updated (this phase's design section)
- [x] `ginkgo ./...` green in `accounts-service` and `refdata-service`; shipping-service tests updated for the dropped static port-context seeding
- [x] Live verification: `docker compose down -v && up --build`; fleet dropdowns populate from the live business-unit list with no hardcoded fallback; adding an account's first real BU surfaces the hide-`_default_bu` prompt; declining leaves `_default_bu` selectable alongside the new BU; toggling `visible` in the Admin UI table is reflected in the fleet dropdown

### Phase 22b (IMPLEMENTED 2026-08-13) — Business Unit Name/Context Split; Per-Tenant Default BU

#### Goal

Phase 22 gave a business unit a single `name` field that doubles as both the human label and the `{context}` subject token, so an operator registering "Pacific Fleet" has to type `acme-pacific-fleet` and live with that string as the UI label forever. This phase splits the two: a free-text English **name** (`Pacific Fleet`) and an immutable, subject-safe **context** slug (`acme-pacific-fleet`), auto-derived from the name at registration and editable before it is committed.

It also retires the shared `_default_bu` *as a tenant's context*. Today every account with no real business units resolves to one globally shared context value, and because `refdata.dictionary_items` is keyed `(context, type_key, code)` with no tenant column, two tenants writing the same code under `_default_bu` collide on the same Postgres rows. Each account gets its own `{tenant}-default` instead.

#### Design

**refdata-service already models the split.** `refdata.contexts` has carried both a `context` column (PK, subject-safe, validated by `ValidateContextName`) and a free-text `name` column since Phase 16 — accounts-service has simply been collapsing them, sending `{"context": buName, "name": buName}` from both `cmd/main.go`'s `refdataRegisterContext` and `accounts/handler.go`'s `callRefdataRegisterContext`. **No refdata-service schema change is required by this phase**; accounts-service stops throwing the display name away.

**Slug derivation and immutability.** The slug is auto-derived in the Add-BU dialog as `{tenant}-{slugify(name)}` and stays editable until submit. After that it is immutable — not a preference but a hard constraint: none of refdata's data tables (`dictionary_items`, `dictionary_localizations`, `dictionary_references`, `dictionary_locales`) carry a foreign key back to `refdata.contexts`, so renaming a slug silently orphans every row, plus the `refdata-{context}` KV bucket, the versioned corpus buckets, and the already-immutable `evt.{context}.…` JetStream history. `name` stays freely editable and gains the `PATCH` field it has never had.

**Global slug uniqueness.** accounts-service's `UNIQUE (account_id, name)` is per-account, but refdata's `context` is a **primary key — globally unique** — and `ContextRepository.Register` upserts on conflict while accounts-service's call to it is best-effort/log-only. A slug clash therefore lets one tenant silently overwrite another tenant's context row (name and `tenant` ownership metadata). `UNIQUE (context)` across all accounts, rejected with a 409, closes that; tenant-prefixing makes it collide-by-accident-proof in practice. The prefix is a naming convention for uniqueness and readability only — per `ARCHITECTURE-COMMUNICATIONS.md` § 2.3 the value stays **opaque** and is never split on `-` to recover the tenant.

**Validation moves upstream.** accounts-service currently validates only non-empty and a leading `_`; there is no charset check at all, so a BU named `west coast` persists locally and fails *silently* downstream because the refdata call is best-effort. A `ValidateSubjectToken`-equivalent moves onto the slug at accounts-service, stricter than refdata's `^[A-Za-z0-9_-]+$`: **lowercase-only**, since NATS subjects are case-sensitive and `Acme` ≠ `acme` is a live footgun, plus a length cap (the slug ends up inside `refdata-{context}-v{N}` bucket names).

**Default BU: tenant-owned, no `_` prefix, readonly.** The default becomes `{tenant}-default` (name `Default`, `[reserved]` tag retained in the UI). Dropping the `_` prefix keeps BR-D33 a hard two-exception rule instead of growing an exception per tenant, and removes the need for a `RegisterDefaultBu`-style validation bypass — a tenant default is just an ordinary BU that happens to be auto-created. It is identified by an explicit `is_default BOOLEAN` column, **not** by string-matching the slug (Phase 22 hardcodes the literal `_default_bu` in ~3 backend and ~4 frontend spots; per-tenant slugs break all of them). Readonly covers identity only: not renamable, not deletable, not creatable through `POST /business-units`. `visible` remains toggleable — BR-AC17's hide-once-a-real-BU-exists flow depends on it.

**`_default_bu` survives as a platform-owned template.** It stops being any tenant's context and becomes the parent every tenant default inherits from: `_platform` → `_default_bu` → `{tenant}-default`. Its `_` prefix becomes *correct* rather than an exception-by-fiat, BR-D38 keeps its sanctioned-exception status with a clarified meaning, and the BR-V06/V07 hazard-class override demo data stays in one place while every tenant default still inherits it through the ancestor walk.

**Inheritance works, but only through the corpus path.** `CorpusRepository.CreateDraft` walks the ancestor chain and flattens each ancestor's locally-authored rows (`domain.FlattenCorpus`); the live path (`item_repository.go` et al.) is a flat `WHERE context = $1` with no chain traversal anywhere. This is **already true of `_default_bu` today** — it works because it is directly seeded and its corpus is published at seed time, not because live reads inherit. Per-tenant defaults parented into the same chain therefore reproduce current behavior exactly, with two ordering requirements: (1) locales are **not** covered by corpus flattening (`dictionary_locales` is on the flat path), so each tenant default must have its locales registered explicitly the way `seed.go` already does for `_default_bu`; (2) `CreateDraft` *silently* skips an ancestor with no published corpus, so tenant-default creation must be ordered after refdata-service's seed completes, with a retry rather than best-effort. Closing the live-path gap is deferred to Phase 106.

#### Checklist

- [x] `accounts-service`: `business_units` gains `context TEXT NOT NULL` + `is_default BOOLEAN NOT NULL DEFAULT false`; `UNIQUE (context)` global; keep `UNIQUE (account_id, name)` for display names. Migration backfills `context = name` (every existing value is already a valid slug — `acme-pacific-fleet`, `_default_bu`), rewrites any legacy `_default_bu` row to `{tenant}-default` before the unique index is built (two accounts both carrying the shared literal would otherwise collide on it), then `SET NOT NULL`
- [x] `accounts-service`: slug validation — `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, 48-char cap, leading `_` impossible by construction (the charset has no way to produce one); applied on write in `createBusinessUnit`, not deferred to the best-effort refdata call (`accounts/slug.go`)
- [x] `accounts-service`: `businessUnitResponse` gains `context` + `isDefault`; `createBURequest` gains optional `context` (server derives `{tenant}-{slugify(name)}` when omitted); `updateBURequest` gains `name` (as a pointer, alongside `visible`, so a rename-only request can't be misread as "hide")
- [x] `accounts-service`: `PATCH /api/accounts/{name}/business-units/{buContext}` path param is now the slug; rejects rename of an `is_default` row (409); no delete endpoint exists for a BU at all (Phase 22 never added one), so "not deletable" holds trivially; `visible` toggle unchanged
- [x] `accounts-service`: both refdata calls send `context` and `name` as distinct values, via a new shared `RefdataClient` (`accounts/refdata.go`) — replaces the two hand-rolled, already-drifting HTTP helpers in `handler.go` and `cmd/main.go`
- [x] `accounts-service`: default BU becomes `{tenant}-default` (name `Default`, `is_default: true`), auto-created in `createAccount` (BR-AC16) **and** in `cmd/main.go`'s `seedPreexistingAccounts`; registers the context with `Parent: "_default_bu"`, registers its locales explicitly, then `CreateDraft` + `Publish` — gated on `RefdataClient.WaitForPublishedAncestor`, which doubles as a "refdata-service isn't listening yet" retry, not just a "corpus not published yet" check (a cold `docker compose up` hits connection-refused, not merely an empty result, on the very first call)
- [x] `refdata-service`: no schema change; `_default_bu` stops being assigned to tenants and is documented as the platform-owned template parent
- [x] `shipping-service`: investigated and found to be a **non-issue**, not a fix — `port_repository.go`'s `_default_bu` OR-fallback and `migrate.go`'s seeded base-port list target the *platform template* context, which still exists unchanged (it never stopped being a real, valid context; it only stopped being assigned as anyone's tenant default). Left untouched; the frontend's separate `getPorts('_default_bu')` TODO(tenant-scoping) hack in `seafreight-app/stores/port.js` is likewise unaffected and remains its own pre-existing, unrelated gap
- [x] `frontend/admin`: Add-BU dialog gains two fields (Name free-text; Context auto-derived via a client-side `deriveContext`/`slugify` mirroring the backend, editable, regex-validated inline, with an immutable-after-creation warning); BU table gains a Context column (mono); `is_default` replaces every `bu.name === '_default_bu'` string-match; hide-default-placeholder dialog now names the real per-tenant slug instead of a hardcoded literal
- [x] `frontend`: `availableContexts` becomes `{context, name}[]` in `seafreight-app` (sourced from accounts-service's now-unfiltered BU list, selecting on `isDefault` rather than a `_`-prefix string check) and `frontend/refdata` (sourced from refdata-service's context list, which already had both fields — the frontend was just discarding `name`); the two `<Select>`s gain `option-label="name" option-value="context"` (precedent: `refdata/components/VersioningPanel.vue`). `frontend/admin`'s `dictionary.js` has no context `<Select>` at all (context is auto-picked, never operator-chosen) and was left as-is — no label/value split to make. `store.context` keeps holding the slug everywhere, so the ~80 downstream API call sites are unaffected
- [x] `BUSINESS_RULES-ACCOUNTS.md`: BR-AC26 (name/context split + slug immutability), BR-AC27 (slug charset + global uniqueness), BR-AC28 (default BU is tenant-owned, auto-created, readonly), BR-AC29 (tenant default parenting, locale registration, corpus publish ordering); amend BR-AC15/16/17
- [x] `BUSINESS_RULES-REFDATA.md`: amend BR-D38 — `_default_bu` is the platform-owned template parent for tenant defaults, never a tenant's own context
- [x] `ARCHITECTURE-ACCOUNTS.md` / `ARCHITECTURE-DICTIONARY.md` updated — rewrote ARCHITECTURE-ACCOUNTS.md's "Business unit registration" section for the name/context split and per-tenant default; rewrote ARCHITECTURE-DICTIONARY.md's Seeding section and "Contexts form a tree" (now three levels: `_platform` → `{_default_bu, real BUs}` → `{tenant}-default`), fixing a pre-existing stale claim along the way (the per-context locale-registration loop never actually ran over `acme-pacific-fleet`/`acme-atlantic-fleet` — only over the two reserved roots)
- [x] **Tests**: Phase 22 shipped with *no* Go coverage of BU behavior at all — added `accounts/slug_test.go` (`ValidateContext`/`DeriveContext`/`DefaultContext`, BR-AC26–28) and a new `Describe("Business units …")` block in `accounts/handler_test.go` covering BR-AC16/26/27/28 end to end over real HTTP + Postgres (default auto-create, derived vs. explicit slug, invalid-slug rejection, cross-account global-uniqueness conflict, rename-preserves-slug, default-rename-rejected-but-still-toggleable, default-sorts-first)
- [x] `ginkgo ./...` green in `accounts-service` (82/82 specs, including the new BR-AC26–29 coverage)
- [x] Live verification: `docker compose down -v && up --build`; confirmed in Postgres that `acme`/`globex` each carry their own `{tenant}-default` row (`acme-default`, `globex-default`) with no `_default_bu` collision, and that `refdata.contexts` shows the full `_platform → _default_bu → {tenant}-default` chain with `name` populated distinctly from `context`; confirmed via the versioned corpus API that `acme-default` inherits `_platform`'s full item set (`sourceContext: "_platform"` on every row) while the live (non-versioned) read path correctly returns empty, matching the documented Phase 106 gap; exercised the Admin UI end to end — Add-BU dialog auto-derives `acme-west-coast-fleet` from "West Coast Fleet" and rejects an edited-to-invalid slug inline; registering globex's first real BU correctly named its real slug (`globex-default`) in the hide-placeholder dialog rather than a hardcoded literal, and hiding it round-tripped to `visible: false` in Postgres

Phase 22b is now fully complete: code, tests, both `BUSINESS_RULES-*.md` files, both architecture docs, and the Admin UI's BU table width follow-up (widened from a 36rem cap to a proportioned, full-width layout once the Context column made it cramped) are all done and verified live.

### Phase 23 (IMPLEMENTED 2026-08-04, pending live verification) — Admin UI: SSE → NATS WebSocket Migration (Dual-Connection Model)

#### Goal

Replace all four of `frontend/admin`'s `EventSource`/SSE streams (dictionary watch, KV inspector, JetStream watch, RPC watch) with direct browser-side NATS WebSocket pub/sub, closing the multi-tab connection-exhaustion gap Phase 15 already fixed for `seafreight-app` (`admin_ui_realtime_transport_options` memory) and decoupling the topbar connection indicator from BU/tenant selection (today it's a side effect of `/api/watch/{context}` failing on an empty context). Scope: `frontend/admin` only — `frontend/refdata` is a separate, later decision.

#### Design

**Two browser connections**, mirroring the split shipping-service's backend already makes internally (`Deps.TenantNC`/`Deps.JS` vs `Deps.PlatformJS`):
- **Admin/Platform** — new dedicated NATS user under `PLATFORM`, minted via a new `MintAdminToken`/`GET /api/auth/adminConnectInfo` path in `accounts-service/auth/`, deliberately *not* routed through `Store.Get(tenant)`/`SigningKeySeed`/`Status` (PLATFORM has no tenant lifecycle). Opened once at boot, never reconnects. Sub-only permissions, no `$JS.API.>`/`$KV.>`: `notify.accounts.account.>` plus new REFDATA/RPCTRACE `notify.*` subjects (below). Drives the topbar connection indicator.
- **Tenant** — existing `MintBrowserToken`/`connectInfo?tenant=` flow, unchanged, plus one added sub permission: `obs.api.>` (RPC panel live tail). Reopens on tenant switch, same as `seafreight-app` today.

**New `notify.*` publish points** (backend adds these; browser gets no `$JS.API.>`/`$KV.>` permissions anywhere — decided over the alternative of granting direct JetStream/KV API access):
- KV bucket puts (today's `watchKVBucket`/`kv.WatchAll`) → `notify.{tenant}.kv.{bucket}.{key}.changed`
- Raw `SHIPPING` stream events (today's `watchJetStream`) → `notify.{context}.shipping.{entity}.{event}` (may already be redundant with the existing Shape A/B `notify.*` publish in `eventhandler/handler.go:181` — confirm at implementation time whether JetStream watch needs its own subject or can reuse it)
- REFDATA changes (today's `watchRefdata`) → `notify._platform.refdata.{typeKey}.changed`
- RPCTRACE (today's `watchRPCObs`'s replay half) → `notify._platform.rpctrace.entry` for live tail

**Replay/bootstrap** for the two streams that need history (JetStream raw watch, RPCTRACE last-10-min) moves to a one-shot REST GET — a stripped-down version of today's existing replay logic minus the long-lived SSE wrapper — followed by a `notify.*` subscribe for the live continuation, the same bootstrap-then-subscribe shape `seafreight-app` already uses for `api.*.list.v1` + `notify.*`.

**Frontend**: new `usePlatformConnection.js` (Admin/Platform singleton, modeled on `seafreight-app/src/nats/useNatsConnection.js`) plus a tenant-connection composable reused/adapted from the same file. `stores/dictionary.js`'s `connect()`/`disconnect()`, `KvInspector.vue`, `StreamView.vue`, and `RpcPanel.vue` all move off `EventSource` onto these. Topbar `connected` tag (`App.vue:110`) switches from `store.connected` to the Admin/Platform connection's own state.

**Backend removal**: all of `sse.go`'s `watch`/`watchTerminal`/`watchKVBucket`/`watchJetStream`/`watchRPCObs`/`watchRefdata` handlers and their routes, plus the corresponding URL builders in `api.js` (`watchUrl`, `kvBucketWatchUrl`, `jetstreamWatchUrl`, `rpcWatchUrl`).

**Open implementation fact to confirm before coding**: where PLATFORM's signing key material actually lives today (however `shipping-admin.creds` itself gets signed in `bootstrap-operator.sh`) — `MintAdminToken` needs the same access.

**Approved BR-AC18** (to land in `BUSINESS_RULES-ACCOUNTS.md` alongside BR-AC15-17 as part of this phase's checklist):
> **BR-AC18 — Admin token minting is isolated from the tenant lifecycle.** `MintAdminToken` mints a NATS user JWT under `PLATFORM` directly from its own signing key material, independent of `accounts.Store`'s `Status`/`SigningKeySeed`/reactivation state machine (which governs tenant accounts only). The minted JWT carries subscribe-only permissions (no `$JS.API.>`, `$KV.>`, or publish grants) scoped to `notify.accounts.account.>` and the REFDATA/RPCTRACE `notify.*` subjects this phase adds. Enforced by an isolation test asserting the admin JWT cannot publish to any subject and cannot subscribe to any tenant-scoped (`api.*`/`notify.{tenant}.*`) subject.

#### Checklist

- [x] Confirm PLATFORM signing-key access path for accounts-service — turned out already solved: `cmd/main.go`'s `ensureSigningKey` establishes a signing key for the seeded `platform` account at every boot (same as tenants), so `MintAdminToken` just reads `Store.Get(ctx, "platform")` directly, no new Provisioner plumbing needed
- [x] `accounts-service/auth`: `MintAdminToken` + `GET /api/auth/adminConnectInfo`; Ginkgo isolation spec per BR-AC18 (`auth/token_test.go`, `auth/handler_test.go`)
- [x] `accounts-service/auth/token.go`: added `obs.api.>` to tenant `MintBrowserToken` sub permissions
- [x] Backend: new `notify.*` publish points + specs — KV puts/deletes (`internal/kvstore.Store.EnableNotify`), SHIPPING raw ship/container events (`eventhandler.publishRawNotify`, piggybacked on the existing Shape A/container projectors rather than a new consumer), REFDATA + RPCTRACE (two new permanent background bridges, `eventhandler.RegisterRefdataNotify`/`RegisterRPCTraceNotify` — registered once at `composition.go` startup, not per-tenant, since both read PLATFORM-account streams)
- [x] Backend: one-shot REST bootstrap endpoints — `GET /api/jetstream/replay`, `GET /api/rpctrace/replay` (`rest/replay.go`), plus one not originally scoped: `GET /api/kv/buckets/{bucket}/entries` (`rest/kv.go`) — needed once it became clear `watchKVBucket`'s snapshot half had no one-shot equivalent yet
- [x] Backend: deleted `sse.go`'s `watch`/`watchTerminal`/`watchKVBucket`/`watchJetStream`/`replayJetStream`/`watchRPCObs` + routes. **Scope correction found during implementation: `watchRefdata`/`/api/refdata-watch` was NOT deleted** — it backs `shared/refdata/useRefdataLabels.js`'s UI-text/label refresh, used by every frontend in this repo (admin, seafreight-app, refdata), not just the four admin-specific panels this phase targeted; conflating it with the other five was a scope-drift mistake in this checklist's original `notify.*` bullet, caught before landing. It now lives in its own `rest/refdata_watch.go`, unchanged.
- [x] Frontend: `usePlatformConnection.js` + `useNatsConnection.js` (tenant) for `frontend/admin`, sharing a `connectionFactory.js` — both subscribe-only (this app issues no `api.*` commands, so no `request()`/publish surface was built)
- [x] Frontend: migrated `dictionary.js`, `KvInspector.vue`, `StreamView.vue`, `RpcPanel.vue` off `EventSource`
- [x] Frontend: topbar connection indicator wired to Admin/Platform connection state
- [x] Frontend: deleted `watchUrl`/`kvBucketWatchUrl`/`jetstreamWatchUrl`/`jetstreamStreamUrl`/`rpcWatchUrl`/`watchTerminalUrl` from `api.js` (the last was already-dead code, removed in the same pass)
- [x] `BUSINESS_RULES-ACCOUNTS.md`: BR-AC18 entry
- [x] `ARCHITECTURE-COMMUNICATIONS.md` § 6 / `ARCHITECTURE-ACCOUNTS.md` "Admin UI browser connections": dual-connection browser model + new `notify.*` subjects, with mermaid diagrams
- [x] `go build`/`go vet`/`go test ./...` green in `accounts-service` and `shipping-service`; `vite build` + `vitest run` + `eslint` (0 errors) green in `frontend/admin`
- [ ] Live verification: `nats/bootstrap-operator.sh --force` (needed — `shipping-admin`'s permissions gained `notify._platform.>` publish) + `docker compose down -v && up --build`, then confirm multi-tab open no longer exhausts connections, the connection indicator reflects PLATFORM connectivity independent of BU/tenant selection, and all four panels function with SSE fully removed — not yet run in this session (destructive/regenerates all creds, needs an explicit go-ahead)

### Phase 24 (PROPOSED) — Credential Lifecycle Hardening: Hermetic Tests, Volume-Backed Creds, Runtime Tenant Provisioning

#### Goal

BR-AC19 (2026-08-06, `BUSINESS_RULES-ACCOUNTS.md`) fixed a real incident — `globex` threw `nats: Authorization Violation` on every connection after nothing more than `docker compose down -v && up --build` — by exporting the seeded accounts' (`platform`/`acme`/`globex`) signing key seeds to git and making `accounts-service` adopt them deterministically instead of minting a fresh random key on every wiped boot. That fix is landed, tested, and verified end to end (commit `a2c6378`).

This phase goes one step further: BR-AC19 made the *seeded accounts' identity* deterministic across a volume wipe, but the `.creds` files themselves still exist only because `bootstrap-operator.sh` wrote them once into a bind mount (`./nats/creds`) that `docker compose down -v` never clears. That's a second, independent lifecycle mismatch of the same shape — a runtime artifact whose durability doesn't match the durability of what it depends on — just currently masked because BR-AC19 keeps the thing it depends on stable. This phase eliminates the mismatch structurally rather than continuing to rely on that stability holding, and finishes the Phase 14b thesis that `accounts-service`, not a one-shot script, is how tenants come into being.

Three independently-shippable sub-phases, each a prerequisite for the next:

- **24a — Hermetic account/creds specs.** `internal/natsaccounts/isolation_test.go` and `dictionary/tenant_switch_test.go` currently read committed `nats/creds/*.creds` off the repo path. 24b/24c both change what that path even is, so this has to land first or both suites break on `go test` alone, not just at runtime.
- **24b — Named volume for `nats/creds`.** Creds get wiped by `down -v` exactly like the Postgres/resolver state they depend on, closing the lifecycle-mismatch class entirely — and reaches `sys.creds`/`shipping-admin.creds`, which BR-AC19 deliberately did not touch.
- **24c — Runtime provisioning of `acme`/`globex`.** `PLATFORM` stays bootstrapped (rationale below); `acme`/`globex` move to the same `POST /api/accounts` path a real tenant already goes through, removing the last two accounts that exist only because a script minted them once.

#### Design

**24a — Hermetic tests**

Migrate both files off the repo-path `.creds`/JWT files onto the pattern `accounts/operator_helper_test.go` already established: mint a throwaway operator/SYS/account/user in-process via `jwt/v2` + `nkeys`, no `nsc` binary, no dependency on `nats/`'s current contents. Standalone quality improvement even without 24b/24c — today these specs are coupled to whatever `bootstrap-operator.sh` last produced, which is exactly the kind of coupling this whole incident was about.

Before treating this as done, search for any other test reading `nats/creds` or `nats/resolver` off the repo path — don't assume the scope is only the two files currently known.

**24b — Named volume for creds**

New named volume (e.g. `nats-creds-data`) mounted where `./nats/creds` is today — `:ro` for consumers, read-write for `accounts-service`, same shape as the current bind mount. `./nats/keys` and `./nats/resolver` stay bind mounts: BR-AC19 made those git-committed artifacts the deterministic source of truth, and that property is what this phase depends on, not what it replaces.

The volume needs a populator, since nothing today writes `platform`/`acme`/`globex`/`sys`/`shipping-admin` creds except the one-shot bootstrap script:

- `platform`/`acme`/`globex`: extend `ensureSigningKey`/`seedPreexistingAccounts` (`cmd/main.go`) to mint-and-write a `.creds` file whenever it adopts or establishes a signing key — the same thing `reactivateAccount` already does, just at startup instead of behind the suspend/reactivate gate.
- `sys.creds` — **chicken-and-egg, not a copy of the above.** `accounts-service` needs `sys.creds` to make its *first* NATS connection at all (`NATS_CREDS_PATH`, before `Provisioner` exists), so it cannot self-heal this one the way it does the others — there is no connection yet to push a claims update over. Resolve by exporting `sys`'s signing key seed from `bootstrap-operator.sh` (same as BR-AC19 did for the other three) and minting `sys.creds` in a pure-crypto step — no NATS connection required, `CreateUser` is local signing — that runs before the process's first `waitForNATS` call.
- `shipping-admin.creds` — **needs its restricted permission set ported into Go, not just re-minted.** `Provisioner.CreateUser` mints unrestricted users; `shipping-admin`'s scoped `--allow-pub`/`--allow-sub` list in `bootstrap-operator.sh` (the one carrying the `$SRV.>` publish fix noted in its own comments) exists only as `nsc` CLI calls today. Needs either a `CreateRestrictedUser` variant taking explicit subject lists, or a fixed-permission option added to `CreateUser` for this one caller.

**24c — Runtime provisioning of `acme`/`globex`**

`SYS` and `PLATFORM` stay bootstrapped; only `acme`/`globex` move to `POST /api/accounts` at seed time. The split is principled, not expedient: `PLATFORM` *exports* subjects (`bootstrap-operator.sh`'s `nsc add export` calls) and hosts permanent startup connections from `refdata-service` and `shipping-service` — a hard boot-ordering dependency `Provisioner` has no machinery for today, since `newAccountClaims` only ever builds *imports* (BR-AC14). Tenant accounts only import, are discovered lazily via `notify.accounts.account.created`/`EnsureTenantByName` (BR-030), and are exactly what `Provisioner.CreateAccount` already does for any `POST /api/accounts`-created tenant — `acme`/`globex` just need to go through that same door instead of a side door.

Watch for the startup-ordering trap this surfaces: `shipping-service` currently discovers tenants by scanning the creds directory. If `acme`/`globex` are created asynchronously after `accounts-service` boots, `shipping-service` may scan before they exist. BR-030's `notify.accounts.account.created` path exists for exactly this — confirm its current callers actually cover this case rather than assuming it does.

#### Checklist

- [x] 24a: search for every test reading `nats/creds/*` or `nats/resolver/*` off the repo path (verify scope beyond the two files already known)
- [x] 24a: migrate `isolation_test.go` to synthetic accounts (pattern: `operator_helper_test.go`)
- [x] 24a: migrate `tenant_switch_test.go` to synthetic accounts
- [x] 24a: `ginkgo ./...` green in `shipping-service` with zero remaining test-code dependency on `nats/creds`/`nats/resolver` paths
- [ ] 24b: `bootstrap-operator.sh` exports `sys`'s signing key seed (`nats/keys/sys-signing-key.nk`), matching BR-AC19's pattern for the other three accounts
- [ ] 24b: pure-crypto `sys.creds` bootstrap step ahead of `accounts-service`'s first NATS dial
- [ ] 24b: `Provisioner` gains a restricted-permission user-minting path; `shipping-admin`'s exact permission set ported from `bootstrap-operator.sh`'s `nsc edit user` calls
- [ ] 24b: `ensureSigningKey`/`seedPreexistingAccounts` mint-and-write `platform`/`acme`/`globex` creds on adoption/establishment
- [ ] 24b: `docker-compose.yml` — named volume replacing the `./nats/creds` bind mount across every consumer
- [ ] 24b: `BUSINESS_RULES-ACCOUNTS.md` — new rule documenting the populate-on-boot mechanism and the `sys.creds` special case
- [ ] 24b: Live verification — `docker compose down -v && up --build` with `./nats/creds` no longer existing as a bind mount at all; confirm every service connects
- [ ] 24c: `Provisioner`/seed step creates `acme`/`globex` via `POST /api/accounts` (or an equivalent internal call) instead of `bootstrap-operator.sh`
- [ ] 24c: confirm `EnsureTenantByName`/`notify.accounts.account.created` actually covers the boot-ordering gap this introduces — trace current callers, don't assume
- [ ] 24c: `bootstrap-operator.sh` scope reduced to `operator` + `SYS` + `PLATFORM` only
- [ ] 24c: `BUSINESS_RULES-ACCOUNTS.md` — rule change documenting PLATFORM-bootstrapped / tenants-runtime as the enforced split
- [ ] 24c: Live verification — fresh `down -v && up --build` produces working `acme`/`globex` tenants with no bootstrap involvement beyond `PLATFORM`

### Phase 25 (25a–25d, 25f–25h IMPLEMENTED; 25e RESOLVED, 2026-08-06) — Pricing Service: Port Linebooker's Rate/Fee Domain

#### Goal

Explore whether refdata-service's corpus-versioning *pattern*
(draft → published → rollback, immutable snapshots) holds up for a
**write-adjacent** domain, by porting the pricing/rate domain of a real
production freight marketplace (Linebooker) —
`RateSheetEntity`/`FeeScaleEntity`/`FixedRateEntity` and their versioning —
into a new standalone `pricing-service`, alongside `shipping-service` and
`refdata-service`. Unlike refdata-service, this domain sits on a write path
in the source system (fee calculation on load-accept, bid validation
against book-now price), so it gets its own service and own Postgres rather
than a merge into refdata-service — see
`obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-DICTIONARY.md`
BR-D28 for the read-only boundary this deliberately does *not* inherit.

A source-code read of the Linebooker domain surfaced several real bugs
worth fixing rather than porting faithfully: FeeScale range matching is
fail-open (a bid above the top range silently charges zero fee), FeeScale
active-version selection sorts by insertion order (`id desc`) instead of
its own `activationDate`, and RateSheet "current version" falls back to
the *earliest* version rather than reporting "none yet." All three are
fixed in the port (BR-P05/BR-P06 in `BUSINESS_RULES-PRICING.md`) rather
than reproduced.

**Confirmed CQRS classification:** plain Postgres CRUD, not event-sourced —
applying the heuristic in `ARCHITECTURE.md` § "Event Sourcing vs Plain
CRUD," nothing here ever needs to replay a log to reconstruct state; "what
rate was in effect on date X" is answered by querying stored, effective-
dated/versioned rows, the same shape as refdata's corpus snapshots.

**Sub-phases**, each independently landable:

- **25a — FeeScale domain (IMPLEMENTED).** `FeeScale`/`FeeScaleVersion`/
  `FeeScaleRange` in `pricing-service/pricing/internal/domain/fee_scale.go`,
  with the corpus draft/published/rolled-back lifecycle reused at
  per-aggregate granularity (each FeeScale versions independently, not one
  context-wide bundle covering RateSheet/FixedRate too). Domain layer only —
  no Postgres/REST/NATS adapters, no `cmd/` yet.
- **25b — RateSheet domain (IMPLEMENTED).** `RateSheet`/`RateSheetVersion`/
  `RateSheetEntry` in `rate_sheet.go` — self-contained opaque
  `CustomerKey`/`RouteKey`/`VehicleType` identifiers owned by
  pricing-service, no dependency on shipping-service's Ship/Container. The
  same draft/published/rolled-back lifecycle as 25a, duplicated with
  distinct error names rather than unified behind a shared type (would have
  meant rewriting 25a's already-passing specs). `FeeScaleOverride`
  resolves when set; the default-fallback half of that rule stays deferred
  (no customer aggregate to hang a default off of).
- **25c — FixedRate domain (IMPLEMENTED).** `FixedRate`/`FixedRateVersion`
  in `fixed_rate.go`, same lifecycle shape again. The source's two
  independently-set "fixed rate" flags (`PricingType.FIXED_RATE` on the
  load, `RateSheetType.FIXED_RATE` on the rate sheet) don't collapse into a
  new field on `FixedRate` as originally planned — on closer read there is
  no `FixedRate`-side flag to collapse; `RateSheet.Type` (BR-P07) is simply
  designated the single source of truth for that "needs admin gating"
  signal, recorded as a design constraint for whenever 25e adds a load to
  gate (see `BUSINESS_RULES-PRICING.md`'s scope note — deliberately not a
  numbered BR-P, since there's nothing testable yet).
- **25d — Service wiring (IMPLEMENTED).** Own `pricing` Postgres schema/
  container (`pricing-postgres`, port 5435 — next free after
  `accounts-postgres`'s 5434), a `postgres/*_repository.go` adapter per
  aggregate mirroring refdata's corpus-repository transaction/rollback
  pattern (Publish/Rollback are transaction-bound; Rollback marks the
  previously-active version `rolled-back` with `rolled_back_by` pointing at
  the new one, exactly like `CorpusRepository.Rollback`), thin
  `application/commands` pass-through handlers (no notifier — no NATS this
  phase), a REST API under `/api/pricing/{context}/...` (port 7203, next
  free in the backend range), and a `pricing-service` docker-compose entry.
  Verified end to end against a live Postgres via `docker compose up`: full
  register → draft → add range/entry → publish → active-version resolution
  → fee/drop-charge calculation → rollback cycle for all three aggregates.
- **25e (RESOLVED) — Cross-service consultation.** Not `shipping-service`
  consulting pricing-service on a write path (the original framing) — the
  real first consumer is the Sea Freight Flow **browser**, directly, over
  `api.*`, mimicking a "Pricing" tab's manual rate-entry UX loosely modeled
  on Linebooker's own admin screens (auto-chained fee-scale range rows kept;
  Linebooker's forced-infinite top range and date-driven versioning
  deliberately not carried over — the former would recreate the exact
  fail-open bug BR-P05 closed, the latter contradicts the corpus
  draft/publish/rollback model already chosen for this port).
- **25f (IMPLEMENTED) — `api.*` adapter + tenant NATS connections.** See
  `BUSINESS_RULES-PRICING.md`'s "`api.*` frontend adapter" entry for the
  full design (package `internal/browserrpc`, lifecycle manager
  `internal/tenants`) — live-verified via the `nats` CLI against real
  `acme`/`globex` tenant creds; the reactive
  `notify.accounts.account.created`-triggered provisioning path mirrors
  shipping-service's proven mechanism but wasn't independently exercised
  with a freshly-minted tenant in this pass.
- **25g (IMPLEMENTED) — Sea Freight Flow "Pricing" tab.** A new sidebar
  entry in `frontend/seafreight-app` (pushed into `App.vue`'s `sections`
  array, no router changes needed — per `shared/unifi-theme/LAYOUT.md`), a
  new Pinia store (`stores/pricing.js`) modeled on `stores/port.js`'s
  bootstrap-then-loading-clears shape, and a read-only `PricingPanel.vue`
  listing every FeeScale/RateSheet/FixedRate in the current context.
  Discovered mid-phase that no endpoint existed to list what's registered
  without already knowing an exact name — added `List` per aggregate
  (BR-P16 in `BUSINESS_RULES-PRICING.md`, excludes soft-deleted FeeScales)
  across the domain/Postgres/REST/`api.*` layers first. Unlike
  `stores/port.js`, there is **no `notify.*` subscription** — pricing-service
  publishes no change-notification stream yet, so this store is a one-shot
  bootstrap read per connect/context-switch, not a live-updating view (BR-029's
  loading-state convention still applies, since the same reset-then-fetch
  gap exists). Live-verified in-browser against the real `docker compose`
  stack: seeded FeeScale/RateSheet/FixedRate rows via REST, confirmed they
  render over the real `api.*` NATS WebSocket path, confirmed a BU-context
  switch reconnects and shows that context's own distinct rows, and
  confirmed the new l10n keys (added to `refdata-service/refdata/seed.go`'s
  `l10nSeed` and regenerated via `npm run gen:i18n`) render correctly in
  both English and Spanish. Drilling into a row's version detail and the
  add/edit/publish/rollback UI are Phase 25h, deliberately out of scope here.
- **25h (IMPLEMENTED) — Manual-entry UX.** `FeeScalePanel.vue`/
  `RateSheetPanel.vue`/`FixedRatePanel.vue` (composed by `PricingPanel.vue`,
  replacing its 25g read-only tables) — register, create-draft, add-
  range-or-entry (auto-chained lower limits for FeeScale, no forced-infinite
  top row — that would recreate BR-P05's fail-open bug), publish, roll back,
  for all three aggregates; RateSheet/FixedRate also get an active/inactive
  toggle (re-`Register` with the flag flipped). FixedRate's "add entry" step
  collapses into "create draft" itself, since `CreateDraft` takes its rate
  fields directly rather than incrementally. No endpoint resolves an
  arbitrary version's ranges/entries by number, so a draft's rows are
  tracked client-side as added in the current session rather than
  re-fetched — a disclosed limitation, not a bug. `stores/pricing.js` only
  grew list-upserting actions (`register*`/`toggle*Active`); every other
  action is a direct `api.js` call from the owning panel, mirroring
  `ShipsAtPortPanel.vue`/`TerminalPanel.vue`'s existing split. Live-verified
  end to end in-browser for all three aggregates' full lifecycle.
- **Deferred, not rejected:** context-tree inheritance (a business-unit
  context falling back to `_platform` defaults) for pricing entities — every
  aggregate carries a flat `context` field but does not walk an ancestry
  chain.

#### Checklist

- [x] 25a: Business rules confirmed with user before implementation (BR-P01–BR-P06)
- [x] 25a: Ginkgo specs written from rules before implementation (`pricing/fee_scale_test.go`), confirmed red
- [x] 25a: `domain.FeeScale`/`FeeScaleVersion`/`FeeScaleRange` implemented, specs green (`ginkgo ./...` in `pricing-service`)
- [x] 25a: `BUSINESS_RULES-PRICING.md` written and indexed from `BUSINESS_RULES.md`
- [x] 25b: RateSheet domain rules confirmed with user (BR-P07–BR-P12), specs written (confirmed red), implemented (green)
- [x] 25c: FixedRate domain rules confirmed with user (BR-P13–BR-P15), specs written (confirmed red), implemented (green); flag-conflation resolved as a design constraint on `RateSheet.Type`, not a new field
- [x] 25b/25c: `BUSINESS_RULES-PRICING.md` updated with BR-P07–BR-P15 and the deferred/design-constraint notes; index in `BUSINESS_RULES.md` updated
- [x] 25d: Postgres schema + adapters (own `pricing-postgres`, port 5435) for all three aggregates
- [x] 25d: REST API (`cmd/main.go`, port 7203), docker-compose entries (`pricing-postgres` + `pricing-service`)
- [x] 25d: Live-verified via `docker compose up` — full lifecycle for all three aggregates, including rollback semantics
- [x] 25d: `BUSINESS_RULES-PRICING.md`/plan updated to reflect service wiring is live
- [x] 25e: Resolved with user — browser talks to pricing-service directly via `api.*`; `shipping-service` never consults it
- [x] 25f: `internal/browserrpc` adapter (27 endpoints across FeeScale/RateSheet/FixedRate) implemented, mirroring shipping-service's browserrpc pattern
- [x] 25f: `internal/tenants` lifecycle manager (`EnsureAll`/`EnsureByName`/`TeardownByName`, notify.accounts.account.* subscription) implemented
- [x] 25f: `cmd/main.go`/docker-compose wired (`NATS_URL`, `NATS_CREDS_DIR`, creds volume, depends_on `nats`)
- [x] 25f: Live-verified via `nats` CLI against real `acme`/`globex` creds — full FeeScale lifecycle + confirmed per-tenant adapter isolation
- [x] 25f: `BUSINESS_RULES-PRICING.md`/plan updated
- [x] 25g: `List` endpoint added per aggregate (BR-P16) across domain/Postgres/REST/`api.*` layers, Ginkgo spec green
- [x] 25g: `stores/pricing.js` + `PricingPanel.vue` + `IconPricing.vue` built; wired into `App.vue`'s sections/onMounted/onUnmounted and `stores/tenant.js`'s tenant-switch reconnect
- [x] 25g: l10n keys added to `refdata-service/refdata/seed.go`, `npm run gen:i18n` regenerated `l10nFallback.en.js`
- [x] 25g: `App.spec.js`'s hardcoded nav-item list updated for the new tab; full `vitest run` back to the same pre-existing failure count (7, unrelated to this phase)
- [x] 25g: Live-verified in-browser against `docker compose` — seeded data renders over real `api.*` NATS, BU-context switch reconnects to a distinct context's rows, English/Spanish both render correctly
- [x] 25g: `BUSINESS_RULES-PRICING.md`/`BUSINESS_RULES.md`/plan updated
- [x] 25h: `api.js` extended with the full register/get/create-draft/add-range-or-entry/set-fee-scale-override/publish/rollback/versions/active endpoint set for all three aggregates
- [x] 25h: `stores/pricing.js` gained `register*`/`toggle*Active` list-upserting actions; `stores/pricing.spec.js` added (6 specs, mirrors `port.spec.js`'s conventions)
- [x] 25h: `FeeScalePanel.vue`/`RateSheetPanel.vue`/`FixedRatePanel.vue` built (register dialog, row-expansion detail, create-draft, add-range-or-entry, publish, rollback); `PricingPanel.vue` simplified to compose them
- [x] 25h: l10n keys (42 new) added to `seed.go`, `npm run gen:i18n` regenerated `l10nFallback.en.js`; cross-checked every `t(...)` call used in the new panels resolves to a real key
- [x] 25h: `vite build` and full `vitest run` clean (31 tests: 24 passing + the pre-existing 7 unrelated failures, same as before this phase)
- [x] 25h: Live-verified in-browser against `docker compose` — full register→draft→add-range/entry→publish→rollback cycle for FeeScale, RateSheet (incl. fee-scale override), and FixedRate (incl. active/inactive toggle)
- [x] 25h: `BUSINESS_RULES-PRICING.md`/`BUSINESS_RULES.md`/plan updated

### Phase 25i (DONE) — Effective-Dated Diesel Overlay

#### Goal

Add the one dimension the Phase 25 port deliberately left out: diesel
adjustments as a **date-effective overlay** on a stable, published rate
sheet — not a re-publish. A source-code read of Linebooker (delegated
investigation, 2026-08-07, citations in the 25i design sketch) established
that `RateSheetDieselAdjustmentEntity` is an independently effective-dated
overlay (`start_date`/`end_date`/`minor_version`) on a stable
`RateSheetVersionEntryEntity`, selected **by the load's own execution
date** (`RateSheetVersionEntryEntityRepository.java:106-121`;
`RateSheetEntityServiceImpl.java:372-377`) — not "the latest." This
**supersedes and corrects** the claim currently written into
`pricing/internal/domain/rate_sheet.go:75-77` and `fixed_rate.go:36-39`
that "a diesel-triggered repricing is just another publish": that is
factually wrong about the source and loses real behaviour (a backdated
load must reprice against the diesel window in effect *then*).

**Confirmed CQRS classification (unchanged):** still plain Postgres CRUD,
not event-sourced. "What diesel-adjusted rate was in effect on date X" is
answered by an interval query over dated rows, never by replaying a log —
the exact worked example of "reference-looking data that needs
date-effective history but still isn't event-sourced" that
`ARCHITECTURE.md` § "Event Sourcing vs Plain CRUD" describes.

#### Versioning model — two axes, `major.minor`

The correction is not that "publish" goes away; it is that **two kinds of
change were collapsed onto one axis**. This phase splits them:

- **Major version — content changes → publish.** Editing lanes, base
  rates, or drop rules stays exactly the existing `draft → published →
  rolled-back` lifecycle (BR-P09, **unchanged**).
- **Minor version — a diesel-price change → append an overlay.** A new
  diesel price appends a dated adjustment on top of the *currently
  published major version*, with **no new major publish**.

Resolved identity becomes `major.minor` (e.g. `v3.0` published content,
`v3.1`/`v3.2` diesel re-prices on it) — matching Linebooker's
`major_version` + `minor_version` two-column model.

#### Decisions locked (with user, 2026-08-07)

- **Diesel price index lives in `pricing-service`** (one more table in
  schema `pricing`), not `refdata-service` — keeps the overlay
  self-contained, zero new cross-service `rpc.*` hops, does not re-open the
  BR-D28 read-only-boundary question. (The refdata option is recorded as a
  rejected-for-now alternative, not deleted.)
- **Diesel adjustments are auto-appended as immediately-effective minor
  versions** — no draft/publish gate. They are system-generated from an
  authoritative price change; the human draft/publish ceremony stays for
  content (major) edits only. Faithful to the source.
- **FixedRate overlay deferred to Phase 25j** — 25i covers RateSheet (the
  richer case); FixedRate reuses the identical pattern as a mechanical
  follow-up (`FixedRateSubVersionEntity` in the source).

#### Proposed business rules (final wording confirmed at 25i-a, before specs)

- **BR-P17** — A resolved rate carries a `major.minor` version. Major = the
  existing published content version (BR-P09, unchanged); minor = a diesel
  overlay on a published major. A diesel-price change bumps minor only,
  never a new major publish.
- **BR-P18** — A diesel price index is a time series `active_date →
  (cent_coastal_price, cent_inland_price)`. "Price in effect on date X" =
  the entry with the greatest `active_date ≤ X`.
- **BR-P19** — Each rate-sheet entry carries its diesel baseline
  (`cent_initial_diesel_price`, `diesel_percentage`, `diesel_type` ∈
  {coastal, inland}), authored as part of the major version; these are the
  formula inputs.
- **BR-P20** — When a diesel price becomes effective on date D, a new
  minor-version adjustment is appended per affected entry (`start_date =
  D`); the previously-open window's `end_date` closes to D (contiguous,
  non-overlapping). Adjusted rate is computed **once, at creation**:
  `adjusted = base + base·(dieselPct/100)·((currentDiesel − initialDiesel)/initialDiesel)`,
  with `currentDiesel` the indexed price (coastal|inland per the entry's
  `diesel_type`) in effect on D.
- **BR-P21** — If no diesel price is indexed on/before D, adjustment
  creation is **rejected** (same fail-closed spirit as BR-P05), never
  silently zero or "current."
- **BR-P22** — Pricing a load resolves `(sheet, route, vehicle,
  effectiveDate)`: active published major (BR-P08) → entry for
  route×vehicle → the adjustment window containing `effectiveDate`
  (`start ≤ date < next start`; the last window is open-ended) → that
  adjusted rate → then the drop surcharge (BR-P12, unchanged).
  `effectiveDate` = the load's pickup date, not "now."
- **BR-P23** — A date preceding the first adjustment window falls back to
  the entry's authored base rate. A newly published major version starts
  with no overlays and accrues its own going forward.

#### Design

- **Domain** (`pricing/internal/domain/`): `rate_sheet.go`'s
  `RateSheetEntry` gains `CentInitialDieselPrice`, `DieselPercentage`,
  `DieselType`; new `DieselAdjustment` (minor version, start/end,
  precomputed `CentAdjustedRate`); a `DieselPriceIndex` lookup
  (`greatest active_date ≤ X`); a `major.minor` resolver
  (`ResolveRate(sheet, versions, adjustments, route, vehicle,
  effectiveDate)`); the BR-P20 formula. Major-version lifecycle code
  untouched.
- **Postgres** (`pricing/internal/postgres/migrate.go` + repos): 3 new
  columns on `pricing.rate_sheet_entries`; new `pricing.diesel_prices`
  (`context, active_date, cent_coastal_price, cent_inland_price`); new
  `pricing.rate_sheet_diesel_adjustments` (`context, rate_sheet_name,
  major_version, route_key, vehicle_type, minor_version, start_date,
  end_date, cent_adjusted_rate`, FK to the entry) with window-maintenance
  on insert.
- **`api.*`/browserrpc** (`internal/browserrpc`): publish-a-diesel-price
  (which auto-generates the overlay across affected entries),
  list-diesel-prices, and resolve-rate-at-date. Overlay generation is a
  server-side effect of the price publish, not a separate authoring step
  (locked decision).
- **Frontend** (`frontend/seafreight-app`, Pricing tab): a diesel-price
  entry/list surface and a resolved `major.minor` + effective-date
  display on the rate-sheet panel. l10n keys via `seed.go` +
  `gen:i18n` (BR-D16), same as 25g/25h.

#### Sub-phases (each independently landable)

- **25i-a — Domain + rules.** Confirm BR-P17–P23 wording, write Ginkgo
  specs (red), implement (green). Domain layer only.
- **25i-b — Postgres.** Schema + repositories + migration + window
  maintenance; integration-verified.
- **25i-c — `api.*` + UI.** browserrpc endpoints + Pricing-tab surfaces;
  live-verified via `docker compose`.
- **25j (separate) — FixedRate overlay.** Same pattern applied to
  FixedRate sub-versions.

#### Checklist

- [x] 25i-a: BR-P17–P23 final wording confirmed with user before specs (2026-08-07)
- [x] 25i-a: Ginkgo specs written from rules (`pricing/rate_sheet_diesel_test.go`), confirmed red
- [x] 25i-a: Domain types `DieselPrice`/`DieselOverlay`; `MinorVersion`+`Overlays` on `RateSheetVersion`; `DieselPct`/`InitialDieselCents` on `RateSheetEntry`; `AdjustedRate`, `DieselPriceOn`, `AppendDieselOverlay`, `AppendDieselOverlayFromIndex`, `RateForLoad` implemented; 40/40 specs green
- [x] 25i-a: Correction landed — "just another publish" replaced in `rate_sheet.go`/`fixed_rate.go`; superseded-claim note + BR-P14 body updated in `BUSINESS_RULES-PRICING.md`
- [x] 25i-b: `migrate.go` — `minor_version` on `rate_sheet_versions`; `diesel_pct`/`initial_diesel_cents` on `rate_sheet_entries`; new `pricing.diesel_prices` + `pricing.rate_sheet_overlays` tables (note: plan used `rate_sheet_diesel_adjustments` — shipped as `rate_sheet_overlays`); `AddEntry`/`ActiveVersion` updated; `IndexDieselPrice`/`ListDieselPrices`/`PersistDieselOverlay` implemented
- [x] 25i-b: Live-verified via `docker compose` — indexed diesel prices, confirmed overlays appended (contiguous window-closing, minor-version bump), confirmed BR-P21 fail-closed (404 on an unindexed date) and BR-P22/BR-P23 resolution behavior
- [x] 25i-c: REST endpoints (`POST/GET /api/pricing/{context}/diesel-prices`, `POST .../diesel-overlay`); browserrpc subjects (`DieselPriceIndexSubject`, `DieselPriceListSubject`, `RateSheetApplyOverlaySubject`); `ApplyDieselOverlay` command; error mapping for `ErrNoDieselPrice`/`ErrEntryNotFound`
- [x] 25i-c: Pricing-tab frontend — `DieselPricePanel.vue` (index/list surface); `RateSheetPanel.vue` gained `major.minor` display, an overlays table, and an "Apply Diesel Overlay" control; l10n keys added via `seed.go` + `gen:i18n`
- [x] 25i-c: `vite build` + `vitest run` clean (pre-existing unrelated `useL10nCopy`/`useRefdataLabels` localStorage failures excluded); live-verified in-browser against real stack, including a DatePicker-timezone bug fix (`.toISOString()` on a local-midnight `Date` shifted the calendar day back a day under UTC+2 — fixed with a UTC-midnight re-anchor in both `DieselPricePanel.vue` and `RateSheetPanel.vue`)
- [x] BR-P24 (found + fixed during 25i-c's live smoke test): `AppendDieselOverlay` divided by `InitialDieselCents` with no zero guard — any entry without an authored diesel baseline (true of every pre-25i seeded rate sheet) was silently corrupted to a $0 adjusted rate via a NaN→int64 conversion. Fixed to skip overlaying baseline-less entries (they keep resolving to their base rate via BR-P23); 3 new Ginkgo specs added, 43/43 green.
- [x] `BUSINESS_RULES-PRICING.md` updated with BR-P17–P24 (IMPLEMENTED, Phase 25i); index in `BUSINESS_RULES.md` range updated
- [x] Plan checklist updated to reflect landed sub-phases

### Phase 26 (IMPLEMENTED, 2026-08-13) — Trading Partner Service: Shipper/Transporter Registration

#### Goal

Port Linebooker's Shipper/Transporter onboarding model (V2's `BusinessEntity`
+ `CustomerProfileEntity`/`TransporterProfileEntity`/`TransporterDocumentEntity`/
`FleetAssetEntity`) into a new standalone `trading-partner-service`, alongside
`shipping-service`/`refdata-service`/`accounts-service`/`pricing-service`.
Collective role term is **Trading partners** (Shipper + Transporter — see
`linebooker_shipper_vs_customer_naming.md`,
`linebooker_trading_partners_term_and_fleet_cardinality.md` in
`.claude/memory/`), surfaced in Admin UI under a new "Trading partners" nav
section rather than RefData UI — this is organisation-owned master data that
*consumes* refdata lookups, not a vocabulary itself
(`linebooker_registration_ui_placement.md`).

**Why Admin UI and not `seafreight-app`, given Phase 25's counter-precedent.**
The immediately preceding "port a real V2 domain" phase put its UI in
`frontend/seafreight-app/` (`PricingPanel.vue`, nav key `pricing`), not Admin
— so the precedent for "ported business domain → which frontend" currently
points at the ops app, and this phase deliberately diverges from it. The
distinction is *who does the task, and how often*: pricing is day-to-day
operational work performed by the same people running ships and terminals,
whereas partner onboarding is operator-administration — rare, privileged,
PII/KYC-bearing, and adjacent to the tenant lifecycle already administered on
the Accounts screen. That places it with Accounts in the Admin UI, matching
`linebooker_registration_ui_placement.md`'s reasoning. Recorded explicitly so
a later reader doesn't find two ported domains in two different frontends
with no stated reason.

**Confirmed CQRS classification.** CLAUDE.md's test ("does anything need to
replay this?") applies per entity, not per service, so it is applied per
entity here rather than asserted once for the phase:

- **`TradingPartner` identity** (`name`, `registrationNo`, `vatRegistrationNo`,
  …) — only current state is ever queried; nothing reconstructs a partner
  from a log. **Plain Postgres CRUD.**
- **`FleetAsset`** — same; a truck's current registration/VIN/make/model is
  all anything asks for. **Plain Postgres CRUD.**
- **`ComplianceDocument` — CRUD now, temporal later (not a clean CRUD case).**
  "Was this partner's GIT cover valid on the date of the load that was
  damaged?" is a point-in-time question, which is precisely CLAUDE.md's own
  *"a rate table where 'what was in effect on date X' matters"* counter-example
  to the reference-data-looking-therefore-CRUD instinct. v1 still ships plain
  CRUD — nothing in the POC asks that question yet — but the classification is
  provisional and is filed under the deferred item below, not settled
  alongside the two above.
- **Status transitions** — the deferred item itself; see below.

Note on the Phase 25 precedent: pricing-service avoided event sourcing but did
**not** avoid history — it built an explicit draft/published/rolled-back
version lifecycle in Postgres (BR-P07–P12). In this repo, "ported domain,
plain CRUD" has in practice meant "CRUD *plus* explicit versioning wherever
history matters." Phase 25 is not a licence for pure CRUD everywhere, and
`ComplianceDocument` above is where that distinction bites.

**Deferred, not rejected (named open item, not designed or scheduled):** the
user flagged wanting a follow-up exploration of whether the Registered→Active
transition should eventually become its own CQRS/event-sourced shape or use a
temporal/effective-dated model (e.g. for re-vetting after suspension)
(`linebooker_trading_partner_phase_v1_scope.md`). Two further questions belong
in that *same* exploration rather than being decided piecemeal: (a)
`ComplianceDocument`'s temporal classification above, and (b)
document-expiry-driven suspension (see "Deferred: document-driven status"
below).

**Scope decisions confirmed 2026-08-13** (full detail:
`linebooker_trading_partner_phase_v1_scope.md`):

- **No platform-identity/tenant-membership split for v1.** One
  `TradingPartner` record (identity + status), no separate platform-account
  vs. tenant-membership tables.
  **Why this does not make "suspended" ambiguous:** `TradingPartner.context`
  is not a free-floating label. `accounts.business_units` carries a
  **globally unique** `context` column with an `account_id` FK
  (`accounts/store.go`'s `business_units_context_key`), and refdata's
  `domain.Context` carries a `Tenant` field with `ListByTenant`. So
  context → tenant is 1:1 and resolvable from data that already exists;
  a partner row is therefore **single-tenant by construction**, and
  `Suspended` means "suspended within this context, which belongs to exactly
  one tenant." The absence of a membership table removes a degree of freedom
  rather than creating an ambiguity.
  **Trigger that would force the split:** the first real-world partner
  needing rows in two contexts owned by *different* tenants. At that point
  `context` alone stops identifying the partner and the platform-identity /
  tenant-membership layering of
  `linebooker_platform_vs_tenant_service_split.md` becomes required. Until
  then, don't build it.
- **Status lifecycle: `Registered` → `Active` → `Suspended` → `Reactivate`
  (back to `Active`)**, confirmed 2026-08-13 to mirror accounts-service's
  create/suspend/reactivate triple — all transitions set **manually**,
  independent of compliance-document approval state (not derived/gated by
  documents in v1).
- **Transition legality matrix** (needed before any BR-TP spec can be
  written; mirrors accounts-service's explicit guards, e.g.
  `reactivateAccount`'s `409 Conflict` when status is not `Suspended` —
  `accounts/handler.go`):

  | From \ command | `Activate` | `Suspend` | `Reactivate` |
  |---|---|---|---|
  | `Registered` | → `Active` | **409** (nothing to suspend from; a partner that should never go live is simply never activated) | **409** |
  | `Active`     | **409** (already active) | → `Suspended` | **409** (not suspended) |
  | `Suspended`  | **409** (`Reactivate` is the only way back — one named command per edge, as in accounts-service) | **409** (already suspended) | → `Active` |

  `Register` is creation, not a transition; it always lands in `Registered`.
- **v1 `Suspend` has no enforcement consumer — this is a status flag, not a
  gate.** accounts-service's suspend has teeth: it revokes the account JWT at
  the resolver, force-evicts live connections, deletes the `.creds` file, and
  publishes `notify.accounts.account.suspended`, which two downstream services
  act on (BR-AC09 → BR-031/BR-032). `TradingPartner.Suspended` has **no
  equivalent enforcement point anywhere in this POC** — there is no
  tender/bid/marketplace service yet to refuse a suspended partner. Stated
  explicitly so the phase isn't mistaken for shipping an enforced boundary:
  what 26a actually delivers is a guarded state machine plus an audit trail.
  **The eventual consumer is the marketplace/tender phase** (bid submission
  and allocation must reject a non-`Active` partner — see
  `linebooker_bid_tender_allocation_rules.md`); a `notify.*` publication
  mirroring BR-AC08–AC10 is deliberately *not* built now, because there is
  nothing to subscribe to it.
- **No offboarding / terminal state in v1 — explicit non-goal, not an
  oversight.** accounts-service deliberately allows no hard delete, on
  regulatory-retention grounds (BR-AC03); a KYC/compliance record with
  identity documents attached has at least as strong a retention argument, so
  "delete a trading partner" is not implemented and no `Deregistered`/`Closed`
  terminal state is defined. A partner that stops trading is left
  `Suspended`. Adding a distinct terminal state is a follow-on decision, and
  belongs with the deferred lifecycle exploration above.
- **v1 includes both compliance documents and Transporter fleet assets** —
  not deferred to a follow-on phase. Subcontracting
  (`FleetAssetEntity.subcontractingOwner`) stays out of scope regardless.
- **Shipper and Transporter built together**, one generic `TradingPartner`
  aggregate with a `PartnerType` discriminator (`SHIPPER` | `TRANSPORTER`),
  mirroring V2's `BusinessEntity` + `BusinessType` pattern
  (`v3_tenancy_axes_decision.md`).

**Confirmed fields** (trimmed from V2's full `BusinessEntity`/
`TransporterProfileEntity`, per 2026-08-13 sign-off):

- Shared: `name`, `tradingAs`, `companyName`, `registrationNo`,
  `vatRegistrationNo`, `type` (`SHIPPER`|`TRANSPORTER`), `status`
  (`REGISTERED`|`ACTIVE`|`SUSPENDED` — all three states of the lifecycle
  above), `context` (business-unit scope, per this repo's `{context}`
  convention). Dropped from V2: `contactPerson`/`contactNo` (redundant with a
  future Users/contacts model).
- Transporter-only: fleet assets, one-to-many (`registrationNo`, `vin`,
  `make`, `model`, **`vehicleTypeCode`** — a trimmed `FleetAssetEntity`, no
  `subcontractingOwner`).
  **`vehicleTypeCode` is validated against refdata**, not stored as free
  text. Without it this phase consumes *zero* reference data, which both
  wastes the demonstration (a repo whose central question is
  refdata-as-a-service) and hollows out the stated reason for the Admin-UI
  placement — `linebooker_registration_ui_placement.md` justifies it partly on
  "consumes refdata lookups (VehicleType/DocumentType/Country pickers)". The
  corpus already exists: `refdata-service/cmd/seed-vehicle-types` builds a
  `vehicle-type` dictionary type (plus a `vehicle-type-category` domain enum)
  directly from Linebooker V2's own `VehicleType.java`, via refdata's REST
  admin API. `make`/`model` stay free text — they are the specific truck, not
  a vocabulary.
- **`gitCoverage`: the earlier "insurance is a document, not a number"
  rationale was wrong on the V2 evidence and is corrected here.** In V2 the
  coverage amount does not live on the profile at all — it lives *on the
  document*, as `TransporterDocumentEntity.coverage`/`centCoverage`. So
  "tracked as a document" does not justify dropping the number; the number is
  part of the document record. It is therefore **restored as
  `coverageCents` on `ComplianceDocument`** (nullable, meaningful only for
  `GOODS_IN_TRANSIT`), not on `TradingPartner`. Any "is this transporter
  adequately insured?" view needs it, and it is close to free once the
  document entity exists. Consciously dropped from the same V2 entity, for the
  record rather than by omission: `insuranceCompanyName`,
  `insuranceContactPersonName`, `insuranceContactNumber`,
  `thirdPartyContingencyCover` (the last pairs with the dropped
  `GIT_CONTINGENCY_POLICY` document type, so both go together).

**Confirmed compliance documents** (subset of V2's `DocumentTypes`,
per-document `status`: `PENDING`|`APPROVED`|`REJECTED`, independent of the
parent `TradingPartner.status`):

- Both roles: `CIPC` (company registration cert), `DIRECTOR_ID`,
  `BANK_CONFIRMATION_LETTER`, `TERMS_AND_CONDITIONS`.
- Transporter-only addition: `GOODS_IN_TRANSIT` (insurance cert). Dropped
  from V2 candidates: `GIT_CONTINGENCY_POLICY`, `BEE_COMPLIANCE_CERTIFICATE`
  — note the latter is a ZA-regulatory artefact and a business call rather
  than a technical one; expect it back.
- **Document type stays a Go enum in v1, not a refdata `document-type`
  vocabulary — deliberate, and worth the sentence** because
  `linebooker_registration_ui_placement.md` names `DocumentType` as an example
  of exactly the sort of stable shared vocabulary refdata *should* own. The
  reason it stays in the domain layer for now: the per-type rules are domain
  logic, not lookup data (which types are Transporter-only, which carry
  `coverageCents`, which are required before a partner can sensibly be
  activated later), and refdata has no `document-type` corpus seeded today.
  Migrating it to refdata later is additive. `vehicleTypeCode` above is the
  opposite case and *does* go to refdata, because its corpus already exists
  and carries no per-code behaviour.
- **Per-document fields:** `type`, `status`
  (`PENDING`|`APPROVED`|`REJECTED`), **`expiresAt` (nullable)**,
  **`coverageCents` (nullable)**, `reference` (see storage decision below),
  `updatedAt`.
  **`expiresAt` is added in v1 even though nothing reads it.** V2's
  `TransporterDocumentEntity` has an `expiry_date` column, and GIT insurance
  certificates genuinely expire — expiry, not rejection, is the realistic
  trigger for the deferred document-driven-status rule below. Adding the
  column now costs nothing; adding it later costs a migration, and without it
  the UI cannot even *display* an expired certificate as expired.
- **Document storage: metadata-only in v1 — no file bytes are stored, and
  this is a decision, not an omission.** V2 splits document metadata
  (`TransporterDocumentEntity`) from the file itself (a separate
  `DocumentEntity`). This stack has no blob store and adding one (S3/MinIO
  container, upload endpoints, content-type and size validation, signed URLs)
  is a materially larger piece of work than the domain model this phase is
  actually about. `ComplianceDocument` therefore carries a `reference` string
  (an opaque external document locator, unvalidated in v1) alongside status
  and expiry. Consequence to keep straight in 26e: the Admin UI surfaces
  **document registration and status management, not file upload** — the
  earlier "document upload" wording was wrong and is retracted. Real file
  storage is a follow-on phase.

**Deferred: document-driven status (named open item, mirroring the
CQRS/temporal one — not a settled non-feature).** v1 keeps every status
transition manual and fully independent of document state, as confirmed
2026-08-13. That decision stands for this phase, since gating would couple two
state machines before either has been exercised. But it is a *deferral with a
known trigger*, not a permanent design property, and was previously written up
as though settled — corrected here so it is tracked symmetrically with the
CQRS/temporal exploration. The trigger is `expiresAt` passing: a Transporter
whose `GOODS_IN_TRANSIT` cover has lapsed is the concrete case where a
document should influence status. **Intended shape when it lands:** an expiry
raises a flag/notification for an operator, and does **not** auto-flip
`status` — so the "all transitions are manual and audited" invariant (and its
BR-TP specs) survives the change rather than being contradicted by it.

**Sub-phases**, each independently landable (mirroring Phase 25's
domain-first decomposition):

- **26a — TradingPartner domain model.** `TradingPartner` aggregate +
  `PartnerType`/`PartnerStatus` in
  `trading-partner-service/tradingpartner/internal/domain/`. Manual
  `Register` → `Activate` → `Suspend` → `Reactivate` lifecycle (confirmed
  2026-08-13 — mirrors accounts-service's create/suspend/reactivate triple,
  BR-AC08–AC10 in `BUSINESS_RULES-ACCOUNTS.md`), all transitions manual (no
  document-approval gating in v1, per the Active-gate decision above) and
  guarded by the legality matrix above (every illegal edge is a
  `409 Conflict`, as in `reactivateAccount`). `PartnerStatus`: `Registered` |
  `Active` | `Suspended`. `Suspend` takes a **required `reason`** (see 26a1).
  Domain layer only.
- **26a1 — Lifecycle audit trail.** An append-only
  `trading_partner.audit_events` table, deliberately the same shape as
  `accounts.audit_events` (BR-AC11): action, partner id, actor, source IP,
  outcome `success`/`failed`, JSONB metadata; no `UPDATE`, no `DELETE`.
  **This is the substantive counterweight to `Suspend` having no enforcement
  consumer in v1** — the durable record of *who* suspended *whom*, *when*, and
  *why* is the actual deliverable of the lifecycle, and a KYC/compliance
  domain carries a stronger audit obligation than the NATS-account minting
  BR-AC11 already covers. Reuse BR-AC11's conventions verbatim rather than
  inventing new ones: `X-Actor` header over the basic-auth username as the
  pre-WorkOS actor placeholder; best-effort writes that log but never block or
  roll back the operation they describe; nothing written for a request that
  fails validation before any state was mutated. `Suspend`'s `reason` is
  required at the domain boundary and lands in `metadata`.
- **26b — Compliance documents.** `ComplianceDocument` child entity
  (per-role subset above, with `expiresAt`/`coverageCents`/`reference`),
  independent status field, no link enforced to parent
  `TradingPartner.status` in v1 — a deferral with a named trigger, per
  "Deferred: document-driven status" above, not a settled non-feature.
  Metadata-only: no file bytes, no upload path.
- **26c — Transporter fleet assets.** `FleetAsset` child entity,
  one-to-many off `TradingPartner` (Transporter only), including
  `vehicleTypeCode` validated against refdata's `vehicle-type` corpus.
- **26d — Service wiring.** Own Postgres (`trading-partner-postgres`, host
  port 5436) and REST API (`trading-partner-service`, host port 7204), plus a
  docker-compose entry. **Ports verified free directly against
  `demos/01-dictionary/docker-compose.yml`** (not assumed): Postgres hosts in
  use are 5432 shipping / 5433 refdata / 5434 accounts / 5435 pricing;
  backend hosts in use are 7200 shipping / 7201 refdata / 7202 accounts /
  7203 pricing. 5436 and 7204 are the next free in each range.
  - **Transport: REST (frontend) + a tenant-scoped `rpc.*` client
    (backend-to-backend, for 26c's `vehicleTypeCode` validation only) —
    corrected 2026-08-13 after a design-review contradiction.** The first
    pass of this plan called the transport "REST-only, no NATS, no tenants
    package," reasoning by analogy to accounts-service. That's wrong: BR-D28
    in `BUSINESS_RULES-REFDATA.md` is absolute — *"`rpc.*` is the only
    transport for backend-to-backend synchronous calls, full stop... no HTTP
    client, base URL, or hostname/port config pointing at a peer backend
    service"* — there is no REST fallback for one backend calling another,
    and that was tried and deliberately closed. Since 26c validates
    `vehicleTypeCode` against refdata-service, and
    `refdataconsumer.New(nc *nats.Conn)` shows that call rides the caller's
    **own tenant-local NATS import** (Phase 21's account-export/import model
    — even reading platform-root corpus data requires being connected as
    that tenant's account, not a shared/platform connection), this service
    needs the **same tenant-scoped NATS wiring `pricing-service` built**:
    `internal/tenants` (per-tenant connection lifecycle,
    `notify.accounts.account.*` reactive provisioning/teardown, mirroring
    `pricing/internal/tenants`), `NATS_CREDS_DIR` + creds volume mount in
    docker-compose, and a small `refdataconsumer`-style client for the one
    `rpc.{context}.refdata.type.list.v1` (or `item.get.v1`) call 26c needs.
    The browser-facing surface stays REST-only (no `api.*` browserrpc
    adapter for the Admin UI — that part of the original reasoning holds);
    only the backend's *outbound* call to refdata-service moves onto
    `rpc.*`. Every `nats.Connect` call must set `nats.Name("trading-partner-service")`
    per CLAUDE.md, testably (`nc.Opts.Name` non-empty).
  - **`frontend/admin/nginx.conf` needs a `proxy_pass` location.** Since
    `TradingPartner` carries a `context` field like pricing/refdata's
    resources (not a flat, context-free identifier like an account name),
    its REST routes follow **pricing/refdata's `/api/{service}/{context}/...`
    shape**, not accounts-service's flat `/api/accounts/{name}` shape — route
    `/api/trading-partners/` → `trading-partner-service:8080/api/trading-partners/`.
    Confirmed by inspecting the actual file: today it only proxies
    `/api/auth/` and `/api/platform/accounts` to accounts-service plus a
    shipping-service catch-all on `/api/` — there is no existing
    refdata/pricing route to copy verbatim (those UIs live in different
    frontends), so this is a new location block, not a copy-paste.
  - **Swagger** — every other backend in this demo publishes one; add it
    (`/swagger/` on 7204) for consistency.
  - **`README.md` port tables** — add the new Postgres and backend rows, and
    fix the existing drift while in there: the tables currently stop at
    `accounts-service` (7202/5434) and never gained `pricing-service`
    (7203/5435).
- **26e — Admin UI "Trading partners" section.** New nav category (per
  `linebooker_registration_ui_placement.md` and the Admin-vs-`seafreight-app`
  rationale above), `stores/tradingPartners.js` Pinia store, register/list/
  detail panels covering **document registration and status management (not
  file upload — see the storage decision above)**, suspend/reactivate with the
  required reason, an audit-trail view, and (Transporter) fleet-asset
  management with a refdata-backed vehicle-type picker.

#### Checklist

- [x] Business rules confirmed with user before planning (2026-08-13 —
      fields, documents, status model, scope, build order; see
      `linebooker_trading_partner_phase_v1_scope.md`)
- [x] Design review of this plan section (2026-08-13) — 10 findings applied:
      transition legality matrix, `SUSPENDED` in the field list, "no
      enforcement consumer" stated, context→tenant rationale recorded, audit
      trail added (26a1), document storage decided (metadata-only),
      `expiresAt`/`coverageCents` restored + document-driven status promoted
      to a deferred item, `ComplianceDocument` re-classified "CRUD now,
      temporal later", 26d transport/nginx/Swagger/README gaps closed,
      `vehicleTypeCode` + Admin-vs-`seafreight-app` rationale added
- [x] Independent verification pass (2026-08-13) of the above against actual
      repo code confirmed all 10 findings, but found the 26d transport
      conclusion self-contradicted finding #9: `vehicleTypeCode`-vs-refdata
      validation requires `rpc.*` (BR-D28, no REST fallback for
      backend-to-backend calls) over a tenant-scoped NATS connection
      (`refdataconsumer.New(nc *nats.Conn)`'s tenant-local import), not the
      "REST-only, no NATS" transport the same pass had just settled on.
      Corrected: this service gets `pricing-service`'s `internal/tenants`
      wiring for that one outbound `rpc.*` call; only the Admin UI-facing
      surface stays REST-only. Also corrected: `/api/trading-partners/`
      routes are context-scoped (pricing/refdata-shaped), not
      accounts-shaped, since `TradingPartner` carries `context`.
- [x] Plan phase reviewed and signed off by user before implementation
      (2026-08-13)
- [x] 26a: BR-TP01–BR-TP06 numbered and confirmed in
      `BUSINESS_RULES-TRADING-PARTNER.md`; Ginkgo specs written from rules
      first (`trading-partner-service/tradingpartner/trading_partner_test.go`),
      confirmed red (package `internal/domain` didn't exist yet — compile
      failure, not just failing assertions)
- [x] 26a: `domain.TradingPartner`/`PartnerType`/`PartnerStatus` implemented
      (`internal/domain/trading_partner.go`), 15/15 specs green
      (`ginkgo ./...`), `go build`/`go vet`/`gofmt -l` clean
- [x] 26a: every cell of the 3×3 transition legality matrix has its own spec
      — the 3 legal edges succeed, the 6 illegal ones return the matching
      sentinel error (`ErrNotRegistered`/`ErrNotActive`/`ErrNotSuspended`,
      mapped to `409 Conflict` at the REST layer in 26d — the domain layer
      itself returns Go errors, not HTTP status codes) (plus `Register`,
      which always lands in `Registered`)
- [x] 26a: `Suspend` without a `reason` is rejected at the domain boundary,
      checked before the status guard so it rejects the same way regardless
      of current status
- [ ] 26a1: `trading_partner.audit_events` append-only table + Postgres
      adapter (26d). **Scoping note (2026-08-13):** BR-TP06's append-only/
      best-effort/writes-nothing-on-pre-mutation-failure guarantees are
      Postgres/handler-level, not pure domain logic — mirrors
      accounts-service's own `AuditLog` (BR-AC11), which has no dedicated
      Ginkgo/unit test either; verify live via `docker compose up` in 26d
      (register→suspend→reactivate cycle writes three rows carrying
      actor/outcome/reason; a request failing validation before any
      mutation writes none), not as a 26a domain-layer spec.
- [x] 26b: Compliance-document rules confirmed 2026-08-13 (BR-TP07–BR-TP11,
      including two design calls made during confirmation: `Reject` ->
      `Pending` resubmission is in scope, and `CoverageCents` carries no
      domain-level type restriction); Ginkgo specs written from rules first
      (`compliance_document_test.go`), confirmed red (`domain.DocumentType`
      undefined), then `internal/domain/compliance_document.go` implemented,
      31/31 specs green (`ginkgo ./...`), `go build`/`go vet`/`gofmt -l`
      clean. Every cell of the document-status 3×3 matrix has its own spec.
      Still pending (26d/26e, not 26b): `expiresAt`/`coverageCents` actually
      persisted in Postgres, and an expired document surfaced as expired in
      the UI (displayed, not enforced) — 26b covers the pure domain rules
      only.
- [x] 26c: Fleet-asset rules confirmed 2026-08-13 (BR-TP12: Transporter-only
      ownership; BR-TP13: registrationNo/vehicleTypeCode required,
      vin/make/model optional free text; registrationNo global uniqueness
      deferred to 26d as a repository invariant, same treatment as
      BR-TP08). Ginkgo specs written from rules first (`fleet_asset_test.go`),
      confirmed red (`domain.AddFleetAsset` undefined), then
      `internal/domain/fleet_asset.go` implemented, 37/37 specs green
      (`ginkgo ./...`), `go build`/`go vet`/`gofmt -l` clean.
- [ ] 26c/BR-TP14: `vehicleTypeCode` validated against refdata via a
      tenant-scoped `rpc.*` adapter (not a domain-layer spec — see
      BUSINESS_RULES-TRADING-PARTNER.md's BR-TP14 scoping note) — unknown
      code rejected; requires the `vehicle-type` corpus (run
      `refdata-service/cmd/seed-vehicle-types` against the composed stack, or
      seed equivalently); lands with 26d
- [ ] `BUSINESS_RULES-TRADING-PARTNER.md` written (new domain file) and
      indexed from `BUSINESS_RULES.md` (BR-TP prefix — confirmed no collision
      with BR-0xx / BR-D / BR-AC / BR-P)
- [x] 26d: Postgres schema (`internal/postgres/migrate.go`) + repository
      adapters (`trading_partner_repository.go`, `compliance_document_repository.go`,
      `fleet_asset_repository.go`, `audit_repository.go`), REST API on
      `/api/trading-partners/{context}/...` (pricing/refdata-shaped, not
      accounts-shaped — `internal/rest/handlers.go`), docker-compose entry
      on 5436/7204. `go build`/`go vet`/`gofmt -l` clean.
- [x] 26d: `internal/tenants` lifecycle manager (per-tenant NATS connections,
      `notify.accounts.account.*` reactive provisioning/teardown, mirroring
      `pricing/internal/tenants` minus the `browserrpc.Adapter` — no `api.*`
      surface here) + `internal/refdataclient`, a trimmed
      `refdataconsumer`-style `rpc.*` client for BR-TP14's `vehicleTypeCode`
      lookup. `tenants.Manager` implements `domain.VehicleTypeValidator`
      directly, resolving the caller-supplied `tenant` (see
      `fleetAssetRequest.Tenant` in `internal/rest/handlers.go` — the Admin
      UI's existing `tenant.js` selection is the source; no prior REST route
      in this repo needed tenant identity as a request field, since Postgres
      data isn't NATS-account-partitioned the way this one `rpc.*` call is).
- [x] 26d: `NATS_CREDS_DIR` + creds volume mount wired in docker-compose;
      `nats.Connect` sets `nats.Name("trading-partner-service")` (CLAUDE.md
      rule) in `internal/tenants/tenants.go`'s `ensure`.
- [x] 26d: `frontend/admin/nginx.conf` `proxy_pass` location added for
      `/api/trading-partners/` (new location block, context-scoped passthrough
      — no prefix rewrite needed, unlike `/api/platform/accounts`), with the
      service's own BasicAuth secret injected the same way accounts-service's is.
- [x] 26d: Swagger — **skipped, correcting the design review's claim.**
      Checked directly: only shipping-service and refdata-service actually
      have Swagger; accounts-service and pricing-service don't. This service
      follows its closer precedent (accounts-service: REST-only, Admin UI,
      BasicAuth-gated, no Swagger), not the inaccurate "every backend has
      one" claim.
- [x] 26d: `README.md` port tables updated — new trading-partner-service rows
      *and* the previously-missing `pricing-service` rows (7203/5435).
- [x] 26d: Live-verified via `docker compose up` — full
      register→document-register→fleet-asset-add→activate→suspend→reactivate
      cycle against the real composed stack (nats, refdata-service +
      refdata-postgres, accounts-service + accounts-postgres,
      trading-partner-service + trading-partner-postgres). Seeded the
      `vehicle-type` corpus into context `acme` via
      `refdata-service/cmd/seed-vehicle-types`; confirmed BR-TP14 rejects a
      bogus `vehicleTypeCode` and accepts a real one (`TAUTLINER`) via a live
      `rpc.*` round trip to refdata-service over the `acme` tenant's own NATS
      connection; confirmed BR-TP13's `registrationNo` uniqueness rejects a
      duplicate; confirmed the full 409 transition-matrix guard
      (re-`Activate` on an already-`Active` partner); confirmed all 4 audit
      rows (`registered`/`activated`/`suspended`/`reactivated`, the last
      carrying `reason` in `metadata`) directly in Postgres. Also confirmed
      `frontend/admin/nginx.conf`'s new `/api/trading-partners/` route works
      end-to-end through the Admin UI's own port (7100) before any frontend
      code was written.
- [x] 26e: Admin UI "Trading partners" nav category + "Registration" screen
      built (`TradingPartnersPanel.vue`, `IconTradingPartners.vue` — both
      since restructured, see 26f below), wired
      into `App.vue`'s `sections`/`SUBTITLES`/section-render per
      `shared/unifi-theme/LAYOUT.md` — no `AppShell.vue` changes needed
      (existing slot API sufficed). API client functions added to `api.js`;
      dev-mode Vite proxy entry added alongside the existing
      `/api/platform/accounts` one. `npm run build` clean; `npx vitest run`
      shows the same single pre-existing, unrelated failure
      (`ConnectionsPanel.spec.js`'s BR-028 test) confirmed via `git stash` to
      reproduce identically without this phase's changes.
- [x] 26e: Live-verified in-browser (Browser pane) against the real
      `docker compose` stack — registered "Globex Logistics" (Transporter)
      through the actual UI, expanded the row (Compliance
      Documents/Fleet Assets/Audit Trail all render), added a fleet asset
      with a bogus `vehicleTypeCode` (clean "not recognized by refdata"
      error, live `rpc.*` round trip confirmed via network log), seeded the
      `vehicle-type` corpus into `acme-atlantic-fleet` and retried
      successfully, then Activated and Suspended (with reason) through the
      row menu and confirmed all 3 audit rows — including the reason —
      rendered in the Audit Trail sub-table. Every `/api/trading-partners/*`
      network call succeeded exactly as expected; the only console errors
      were the pre-existing, unrelated `dict-b` KV bucket 400s.
- [x] `BUSINESS_RULES-TRADING-PARTNER.md`/`BUSINESS_RULES.md`/plan updated
- [ ] Deferred items carried forward as named open questions (not silently
      dropped): lifecycle-as-CQRS/temporal exploration,
      `ComplianceDocument` temporal classification, document-expiry-driven
      status, real file storage, terminal/offboarding state,
      platform-identity vs tenant-membership split, `notify.*` publication
      once a marketplace consumer exists

#### 26f — Admin sidebar PLATFORM/SYSTEM grouping (IMPLEMENTED, 2026-08-13)

A follow-on to 26e rather than a phase of its own: adding Trading partners
made the admin sidebar's flat eyebrow list read as a pile of unrelated
categories, mixing business-layer screens with NATS/Postgres diagnostics. Not
a backend or business-rule change — no BR-TP rule moved.

- [x] `shared/ui-shell/NavList.vue` grew a third *banding* level (not a third
      nav level): a `sections` entry may now be `{ group, sections }`, a
      collapsible accent-tinted banner over one or more ordinary
      `{ eyebrow?, items }` sections. Both entry forms mix in one ordered
      array, so `seafreight-app`'s flat single section and admin's ungrouped
      Overview are unaffected. Collapse state is the component's own, like
      `AppShell.vue`'s sidebar collapse.
- [x] `shared/ui-shell/app-shell.css`: `.nav-group-toggle` reuses `.eyebrow`
      for its micro-type and finally activates the `.eyebrow.is-open svg`
      chevron-rotate rule that had been sitting there unused; adds
      `.nav-group-body` with a `visibility`-based collapse (so collapsed
      items leave the tab order) plus a `.sidebar.collapsed` override forcing
      groups open on the icon rail — the banners hide there, so a collapsed
      group would otherwise be unreachable.
- [x] Group contents indent 18px (`.nav-group-body.is-grouped`) so they read
      as nested under the banner. 18px = the banner's chevron (12px) + gap
      (6px), landing contents on the group *label's* text column rather than
      its chevron's — the tree-view convention. Ungrouped sections don't
      indent; the icon rail zeroes it so icons stay centred (verified: all 13
      rail icons share one centre, 109.5 against a sidebar centre of 110).
- [x] `admin/src/App.vue`: PLATFORM (Accounts, Trading partners →
      Shippers/Transporters, Settings) before SYSTEM (NATS, Postgres), with
      Overview ungrouped above both. Accounts moved out of the NATS eyebrow —
      it's a platform-membership roster, NATS accounts are just its mechanism.
- [x] Trading partners split per role: `TradingPartnersPanel.vue` takes a
      `partnerType` prop and is mounted twice (`:key` per role so switching
      remounts). Register dialog lost its Type field, list lost its Type
      column, list filters client-side (`GET /api/trading-partners/{context}`
      has no `type` param — noted as revisit-if-paginated).
      `IconTradingPartners.vue` → `IconTransporters.vue`, new
      `IconShippers.vue`.
- [x] `admin/src/components/NavList.spec.js` — 9 specs covering both entry
      forms, per-group independent collapse, and that an ungrouped section is
      never collapsible. `shared/ui-shell/` has no runner of its own, so the
      spec lives under admin's Vitest.
- [x] `shared/unifi-theme/LAYOUT.md` updated with the new `sections` shape,
      the "no third nav level" rule, and admin's per-app note.
- [x] Live-verified in-browser: alignment measured (group banners, eyebrows
      and items all share one left edge; the Trading partners eyebrow's text
      lines up with the Accounts icon, as mocked), independent collapse,
      icon-rail override (13/13 items reachable with a group left collapsed,
      0 banners shown), collapsed items out of the tab order, light + dark
      mode, and the role filter (a Shipper registered through the UI appears
      under Shippers and not under Transporters).

#### 26g — trading-partner-service micro registration (IMPLEMENTED, 2026-08-13)

Prompted by a real observation: the service was absent from the Admin UI's
Services panel despite running with live NATS connections. Root cause — an
outbound-only `rpc.*` requester has nothing for `$SRV` discovery to find, and
`micro.AddService` was never called. `micro.AddService` wires the
`$SRV.PING/INFO/STATS` responders independently of `AddEndpoint`, so discovery
is fixable without any transport migration. Split from 26h deliberately so the
panel fix ships now and the transport change gets its own review.

- [x] New `tradingpartner/internal/browserrpc` package: `micro.AddService`
      with `Name: "trading-partner-service"` (matching the connection's own
      `nats.Name`, the Phase 18 responder/requestor identity invariant),
      `Version: "1.0.0"`, and `Metadata{"tenant": …}` so per-tenant
      registrations are distinguishable in the panel. **Zero endpoints** — the
      `Description` says so out loud rather than leaving an operator to guess
      whether an empty endpoint list is a bug.
- [x] `internal/tenants` now holds a `browserrpc.Adapter` per tenant
      connection alongside the existing `refdataclient.Client`, mirroring
      pricing-service's `resources` struct. Registered in `ensure` (including
      a `Stop()` on the lost-race path), stopped in `TeardownByName` before
      the connection closes (so a suspended tenant stops answering discovery
      rather than lingering as a phantom row, BR-031) and in `Close`.
- [x] 6 new Ginkgo specs (`tradingpartner/browserrpc_test.go`, embedded
      in-process NATS server per refdata-service's `natsrpc_test.go`
      convention) — 43 total green. They assert discoverability *over the
      wire* via a real `$SRV.PING` broadcast, not just that `New()` returns
      nil: a constructor-only test would not have caught the original bug.
      Includes a spec pinning the deliberate zero-endpoint scope, to be
      replaced (not deleted) when 26h lands.
- [x] Stale "there is no api.* adapter / REST only" comments corrected in
      `composition.go`, `cmd/main.go`, `internal/tenants`, and
      `docker-compose.yml`.
- [x] Live-verified: `GET /api/nats/services` and the rendered panel both go
      3 → 4 services, `trading-partner-service v1.0.0`, 1 instance,
      `tenant: acme`, 0 endpoints. The `tenant=test` "Authorization Violation"
      in its startup log is pre-existing and unrelated — pricing-service and
      shipping-service log the identical error from a stale `test.creds`,
      timestamped before this change.

#### 26h — trading-partner-service api.* transport (IMPLEMENTED, 2026-08-13)

Decisions confirmed 2026-08-13 before any code: **`api.*`, not `rpc.*`**;
**REST kept** as a dual transport; **micro registration shipped first** (26g).

The `api.*` choice is the repo's own rule, not a preference: CLAUDE.md's
subject taxonomy and `ARCHITECTURE-COMMUNICATIONS.md` § 2 make `api.*`
frontend-to-service and `rpc.*` service-to-service, and state "a browser
credential is never granted `rpc.>`". The Admin UI is the only caller today,
so its endpoints are
`api.{context}.trading-partner.{entity}.{action}.v1`. `rpc.*` endpoints get
added when a *backend* caller exists — the marketplace/tender phase that
finally gives BR-TP04's `Suspend` an enforcement consumer — not speculatively.

**Correction (2026-08-13): there is no credential blocker — the earlier claim
in this section was wrong.** It conflated the Admin UI's *two* NATS
connections. `usePlatformConnection.js` (PLATFORM account, from
`GET /api/auth/adminConnectInfo`) is genuinely publish-denied —
`auth/token.go`'s `MintAdminToken` sets `Pub.Deny.Add(">")`. But the *tenant*
connection in `useNatsConnection.js` (from
`GET /api/auth/connectInfo?tenant=…`, the same `MintBrowserToken` seafreight
uses) already carries `Pub.Allow = ["api.>", "_INBOX.>"]` and
`Sub.Allow = ["api.>", "notify.>", "obs.api.>", "_INBOX.>"]`
(`auth/token.go:110-111`), and that file's own comment already said so:
"subscribe-only *use* here even though that JWT also carries
`api.>`/`_INBOX.>` publish permission: this app has no command surface of its
own to exercise it with."

trading-partner-service is per-tenant and its `api.*` subjects resolve inside
tenant accounts, so the tenant connection is the correct one and **needs no
change**. No `auth-service`/`accounts-service` edit, no new BR-AC rule, no
token-minting test churn. Also verified: scoped signing keys are *not* in play
(`provisioner.go` uses the unscoped `SigningKeys.Add`; the resolver JWTs carry
flat key arrays), so nothing silently overrides a user JWT's own permissions —
`.claude/memory/nats_scoped_signing_keys.md` describes proposed future work,
not current state.

The remaining gap is purely frontend: `connectionFactory.js` has no
`request()` surface, by an explicit design note ("adding one before anything
needs it would be exactly the speculative feature CLAUDE.md warns against").
This phase is that need arriving.
- [x] `connectionFactory.js` grew a `request()` surface; its doc comment now
      records that Phase 26h is the need arriving, rather than deleting the
      original non-speculative rationale. Also documents that `request()` is
      tenant-connection-only, since the PLATFORM credential is publish-denied.
- [x] `internal/browserrpc` endpoint constants + handlers for all 14
      operations, replacing 26g's zero-endpoint registration. Mirrors
      pricing-service's plumbing (`contextFromSubject`,
      `reply`/`respond`/`respondError`, `Nats-Responder`, `obs.api.*`
      side-channel). 26g's zero-endpoint spec was *replaced*, as it said it
      should be, not deleted.
- [x] **Two properties the api.* path enforces that REST cannot**, both now
      spec'd over the wire: `{context}` comes from the subject (a body
      `context` field cannot redirect a write), and BR-TP14's tenant comes from
      the adapter's own connection (a body `tenant` field is ignored). The
      second is why REST's `fleetAssetRequest.tenant` has no api.* equivalent —
      HTTP had no tenant identity, NATS does.
- [x] Dependency-cycle resolution: `Startup` needs the tenant Manager for
      BR-TP14's validator, while the adapter needs the handlers `Startup`
      builds. Split into a third pass — `MountTenants` → `Startup` →
      `MountAPI` — with `tenants.Manager.apiDeps` nil until `MountAPI` runs and
      every `adapter` teardown path nil-guarded. pricing-service doesn't hit
      this because it has no refdata dependency.
- [x] `admin/src/api.js`'s trading-partner functions moved from `fetch` to NATS
      request; `TradingPartnersPanel.vue` unchanged (signatures held).
      `tpSubject` throws rather than emit a malformed subject if a context
      contains a dot/space/wildcard — a dot would shift every later token and
      silently resolve the wrong context.
- [x] REST retained and still wired in `cmd/main.go`. **Decision: the
      nginx/vite proxy entries and BasicAuth secret stay.** This diverges from
      pricing-service (which has REST but no browser proxy route) on purpose —
      keeping port 7100's `/api/trading-partners/` path preserves the exact
      curl surface used to verify 26d, at the cost of some now-unused browser
      reachability. Revisit if that path ever drifts from the api.* behaviour.
- [x] `ARCHITECTURE-COMMUNICATIONS.md` (new § 3.1 adapter matrix + the
      registration-vs-endpoints and no-rpc-without-a-backend-caller notes),
      `BUSINESS_RULES-TRADING-PARTNER.md`, and this plan updated together.
- [x] **Bug found by live verification, not by the specs:** the first migration
      of `addFleetAsset` dropped the partner `id`, which REST had carried as a
      URL path segment — Postgres rejected it with `invalid input syntax for
      type uuid: ""`. The frontend spec had asserted what was *removed*
      (`tenant`) but never what had to be *kept* (`id`). Fixed, and the spec now
      blanket-asserts every per-partner operation sends its id; confirmed
      red-on-bug then green-after-fix rather than assumed.
- [x] Tests: backend 52 Ginkgo specs (was 43) — registration, subject shape,
      and wire round-trips including context-spoofing, tenant resolution,
      404 mapping, `Nats-Responder`, and the `obs.api.*` mirror, against an
      embedded NATS server with in-memory fakes. Frontend 13 new Vitest specs
      (46 total passing).
- [x] Live-verified in-browser against the real stack: Services panel 0 → 14
      endpoints; Transporters list, all three expansion sub-tables, fleet-asset
      add (with BR-TP14 refdata validation passing), and a Reactivate all over
      NATS with **zero** REST calls to `/api/trading-partners/*` recorded. Data
      confirmed in Postgres, including the audit row's
      `nats:admin-tenant/<id>` provenance. REST and api.* cross-checked
      agreeing on the same records.

### Phase 27 (IMPLEMENTED, 2026-08-14) — Admin UI: Account Activity Panel (/accstatz)

#### Goal

Surface the NATS server's per-account traffic (`/accstatz`) in the Admin UI —
nothing showed it before: Connections lists raw sockets one at a time,
Accounts shows JetStream storage/consumer *limits*, not wire activity.
Follows a design-recommendation request (frontend-design skill) that
compared three placements — a new panel (chosen), columns on the existing
Accounts table, and an alert-only banner — and picked the new panel because
`/accstatz` comes off the same `:8222` monitor port as `/connz`/`/varz`
(Connections' Phase 17c proxy pattern), not from accounts-service.

- [x] Backend: `GET /api/nats/account-activity` in
      `dictionary/internal/rest/nats_ops.go` — `listNatsAccountActivity`
      proxies `/accstatz` (primary read, 502 on failure) and resolves
      `tenantLabel` via a secondary, best-effort `/connz` probe reusing
      `tenantLabelsByAccount` (BR-028's exact mechanism). Swagger docs
      (`docs/docs.go`, `swagger.json`, `swagger.yaml`) hand-patched per the
      established convention (`swag_regen_diff_noise.md`).
- [x] Frontend: `AccountActivityPanel.vue`, modeled on `ServicesPanel.vue`'s
      `.svc-card` pattern (dot · name · inline stat pairs · chevron) rather
      than inventing a new row shape. New nav entry under SYSTEM → NATS
      (`IconActivity.vue`), between Services and Log.
- [x] **The one deliberate design move:** `slow_consumers` gets no routine
      tile. At zero it's silent — same `.dot.ok` convention `ServicesPanel`
      already uses — matching the "facts that only matter in an exceptional
      state get rendered only in that state" rule established for
      Connections' paged-note (`admin_stat_card_one_ratio_rule.md`). Nonzero
      turns the dot red, tints the card border, swaps the "subs" stat for a
      red "slow" stat, and opens the expansion on a named alarm line; a
      summary-row banner appears under the same condition. See BR-034
      (BUSINESS_RULES-SHIPPING.md).
- [x] Tests: 5 new Go tests (`nats_ops_test.go`) covering reshaping/sorting,
      tenant-label resolution off the secondary `/connz` probe, and 502s;
      10 new Vitest specs (`AccountActivityPanel.spec.js`) covering the
      silent-at-zero / alarm-at-nonzero contrast specifically. Full backend
      (`ginkgo ./...`) and frontend (`vitest run`) suites green.

### Phase 28 (business rules confirmed 2026-08-15; implementation in progress) — Distributed Tracing for Inter-Service Comms

#### Goal

The Request/Reply panel renders a *message log*, not a trace. Its correlation
key is `req.Reply()` — an `_INBOX` subject generated fresh by each requestor
and never propagated — so a `browser → shipping-service → refdata-service`
call shows as two unrelated rows, and `evt.*`/`notify.*`/KV writes show as
none at all (no reply inbox means no correlation id even in principle, so the
whole async CQRS tail is unjoinable to the command that caused it). A
`refdataconsumer` call that times out or finds no responder currently emits
**zero** records at either end.

Introduce `obs.trace.*`: one W3C `traceId` minted in the browser and carried
in a `traceparent` header through every hop, service, NATS account, and async
boundary; spans assembled server-side into a KV bucket; and a waterfall in
the Admin UI that shows what a request *caused*, including the account
boundaries it crossed and the read-model work that continued after the client
was already unblocked. Approved UI mockup and ingest-topology design visual
were reviewed before this phase was written.

#### Decisions locked (with user, 2026-08-14)

- **Evolve §4.5 in place**, don't add a ninth panel. Nav key `rpc` and the
  8-entry nav are unchanged; the panel becomes *Request/Reply & Traces* with a
  `[traces] [messages]` view toggle. The flat view survives because it answers
  "is anything arriving on this subject" better than a trace list does.
- **Payloads stay on spans**, but published to the **PLATFORM account only**,
  with a redaction denylist and a 4 KiB cap (truncation flagged, never
  silent). No browser credential is granted `obs.trace.>`.
- **Full rollout in one phase** — all four RPC adapters plus accounts-service
  and the auth surface. Wider than was recommended; mitigated by the
  independently landable sub-phases below.
- **`TRACES` stream + KV bucket keyed by trace id**, assembled by a single
  durable consumer so there is no read-modify-write race. That store split is
  Shape A applied to the lab's own telemetry — replayable log in JetStream,
  keyed current state in KV, Postgres deliberately absent.

#### Design

- **Wire format** — `obs.trace.{context}.{service}.{entity}.{action}`, fixed
  6-token arity so `SubjectPath.vue`'s positional faceting keeps working.
  `traceSpan` is a **strict superset** of `obsEnvelope`: no field renamed or
  retyped, every addition `omitempty`. `RpcPanel.vue` keys rows on
  `correlationId` and `browserrpc_test.go` pins backward-compatible decoding
  of the pre-BR-D36 shape, so a rename breaks the panel. Field names mirror the
  OTLP `Span` message 1:1. `correlationId` is retained as a per-hop field — it
  is **not** the span id.
- **One decorator, not 58 edits.** All four adapters register endpoints
  through a table loop with a single `svc.AddEndpoint` call, and both
  `micro.Handler` and `micro.Request` are interfaces, so one wrap at that call
  site replaces every hand-pasted request-side `publishObs` line (8 shipping,
  5 refdata, 33 pricing, 12 trading-partner). Safe against micro's own stats:
  `service.reqHandler` holds the original `*request` and the wrapper delegates
  `Respond` to it, so `$SRV.STATS` keeps counting errors and the Services
  panel is unaffected. Outcome comes from the existing
  `respond`/`respondError` tails via `natstrace.SpanFrom(req)` (nil-safe for
  unwrapped requests in tests) because those already hold the typed error and
  its 404/500 classification.
- **Tracer is per-adapter, never a package singleton** — shipping, pricing,
  and trading-partner construct one adapter *per tenant NATS account*
  (`rest/tenant.go`, `tenants.go` ×2); only refdata is per-process. `obs.*`
  must not cross an account boundary except via the sanctioned export.
- **`natstrace` is duplicated per service, not shared.** Five independent
  `go.mod` files, no `go.work`, and each Dockerfile builds with its own
  directory as build context, so a `replace`-based local module is simply
  absent from the image; widening the context to `./backend` would invalidate
  all five services' Docker layer caches on any single edit, regressing the
  documented `docker compose up --build` workflow. This matches how
  `obsEnvelope`, `errorResponse`, `obsSubjectFor`, and `contextFromSubject` are
  already duplicated on purpose. Drift is pinned by the §6 wire spec plus a
  per-service contract test.
- **No `go.opentelemetry.io/*` dependency, and the OTLP exporter is a
  container, not a library.** The OTel Go API is `context.Context`-based, but
  every adapter hands the application layer a fresh `context.Background()` at
  ~60 call sites, so real span nesting would mean changing every command and
  query signature it reaches — through the domain layer Quality Rule 3 keeps
  framework-free. Adopting OTel only at the adapter boundary would pay the
  dependency cost for a *flat* trace. `natstrace` is therefore hand-rolled,
  W3C-compatible on the wire and OTLP-shaped in its fields, and the exporter
  is a ~150-line service consuming `obs.trace.>`. That buys retroactive
  export (start the collector late, replay the retained hour — impossible for
  an in-process exporter), no-code toggling, one copy of the OTLP mapping, and
  makes the `obs.*` isolation invariant structural rather than maintained.
  Revisiting the real SDK for `otelpgx`/`otelhttp` auto-instrumentation is
  recorded as an explicit deferred decision.

#### Sub-phases (each independently landable)

- [x] **28a — `natstrace` + decorator, prototyped in trading-partner.** It
      already has `observe`/`reply`/`actor` helpers and no JetStream, so the
      diff is 12 lines deleted, 1 wrap added. Validates the decorator and the
      `SpanFrom(req)` upcast end to end against the live panel before the
      pattern is copied anywhere.
- [x] **28b — Copy to pricing, shipping, refdata.** Reply-side `publishObs` in
      `respond`/`respondOK`/`respondError` becomes `sp.End()` / `sp.Fail()`.
      All three done, `natstrace` verified byte-identical in logic across all
      four services (trading-partner/pricing/shipping/refdata — doc comments
      only differ), `ginkgo ./...` green in every service:
      - **shipping:** `dictionary/internal/natstrace` wired into
        `dictionary/internal/browserrpc/adapter.go`; 8 request-side
        `publishObs` lines and `obsEnvelope`/`obsSubjectFor`/`versionSuffix`
        gone. Retires shipping's `obs.api.*` publish outright (BR-026/BR-027
        amended in `BUSINESS_RULES-SHIPPING.md`) rather than running it
        alongside `obs.trace.*` — `[messages]` view goes dark for shipping
        traffic until 28g, an accepted gap per "full rollout in one phase."
        114/114 `dictionary` suite + 5/5 `natstrace` specs.
      - **pricing:** same wiring, 33 request-side `publishObs` lines removed
        (matching the plan's count exactly); no pre-existing round-trip test
        existed for this adapter, so none needed updating. 43/43 `Pricing`
        suite + 5/5 `natstrace` specs.
      - **refdata:** wired into `refdata/internal/natsrpc/adapter.go` (this
        service's adapter package, `natsrpc` not `browserrpc`); 5 request-side
        `publishObs` lines removed; `Deps.JS`/`RPCTRACE` JetStream-replay
        branch (BR-D29) removed from `publishObs`'s replacement since
        natstrace only does plain `nc.Publish` — BR-D29's catch-up replay is
        deferred to 28f's TRACES stream. Found and fixed a **pre-existing,
        unrelated test bug** while getting this suite green: `ItemGetRequest`/
        `TypeListRequest`/`ItemGetVersionedRequest`/`LocalesListRequest` all
        carry `Context` in the body (Phase 21 design — subject position 2 is
        the caller's NATS account public key here, not the context name), but
        9 existing tests never set it, so every rpc.* success-path assertion
        silently resolved against context `""` instead of the seeded
        `"acme-test"` and got back zero-value replies — confirmed pre-existing
        via `git stash` against this exact commit (same failures with
        natstrace entirely absent). Fixed by adding `Context: itemCtx` (test
        file only, zero production-code risk). All 141 refdata specs +
        5/5 `natstrace` specs now green, comprehensively fixing what was
        previously an 11-failure suite.
      `gofmt` clean across all four `natstrace` copies (fixed one map-literal
      alignment diff in shipping's/refdata's copies).
- [x] **28c — Outbound inject.** `refdataconsumer.requestRPC` and
      `refdataclient.Client` — the `nats.Header` already exists for
      `Nats-Requestor`, so it is one line each. **One span per logical call
      with a `rpc.retry_count` attribute, not one per retry attempt**:
      generate the child span id before the retry loop, or a 3-attempt failure
      yields three parentless siblings. First hop crossing both a service and
      an account boundary.
      Added `natstrace.ContextWithSpan`/`SpanFromContext` (the only ctx.Value
      use in this codebase — deliberately narrow, costs the domain layer
      nothing since ctx is already threaded everywhere) so a browserrpc
      handler's inbound span rides down through the application/command layer
      to the outbound rpc.* call unchanged. Added `Tracer.StartOutbound` for
      client-side spans. **Design correction made mid-implementation:**
      `StartOutbound` cannot parse `{context}/{entity}/{action}` positionally
      off the subject the way `Start` does for inbound requests — an outbound
      caller's subject is a tenant-account **local alias**
      (`refdata.item.get.v1`) that accounts-service's `provisioner.go` remaps
      server-side to the real `rpc.{tenant}.refdata.item.get.v1`
      (`jwt.RenamingSubject` — the account's own identity lands at that
      token, deliberately never a caller-supplied value). An earlier draft
      assumed the bare local alias was itself a bug, rewrote it to construct
      the full subject directly, and only caught the mistake before landing
      it by checking accounts-service's actual import/export declarations —
      that would have both broken routing and put a caller-controlled value
      where the import exists specifically to prevent one. `StartOutbound`
      now takes the label fields as explicit parameters instead. Also moved
      shipping-service's `natstrace` package from `dictionary/internal/` to
      top-level `internal/` — `internal/refdataconsumer` sits outside
      `dictionary/`'s subtree, so Go's internal-visibility rule blocked the
      import from its original location. `ginkgo ./...` green in all four
      services (trading-partner/pricing/shipping/refdata), including a new
      `refdataclient` suite (this package had no tests at all before Phase
      28c) and three new `refdataconsumer` BR-037 specs.
- [x] **28d — Async tail.** `jstream.Publisher` (both services — the sole
      `evt.*` funnels, needing `PublishMsg` plus a `PublishWithTrace` method
      since the ctx is empty today), the three `Consume` callbacks
      (per-message spans only — `handler.go` uses `context.WithoutCancel`, so
      the message ctx is process-lifetime), and `kvstore.publishNotify`. Note
      `jetstream.KeyValue.Put` takes no headers, so a KV entry cannot carry
      trace context — the derived notify does, and trace data must not go in
      the body because the KV inspector distinguishes PUT from DEL by empty
      payload. This sub-phase is what makes the reply-ack line real.
      **shipping-service done** (`jstream.Publisher`/`kvstore.publishNotify`
      were already landed as foundational pieces before this pass): wired the
      remaining 9 `dictionary/internal/browserrpc/adapter.go` handlers'
      `context.Background()` call sites to
      `natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req))`
      so an inbound api.* request's span rides down through
      `commands.ShipHandler.publish`/`commands.ContainerHandler.publish`
      (both now call the widened `Publisher.PublishWithTrace`, `Publisher`
      itself widened to require it) onto the resulting evt.* JetStream
      publish. `dictionary/internal/eventhandler/handler.go`'s shared
      `register()` (used by both `RegisterShapeA`/`RegisterShapeB`),
      `container_handler.go`'s `RegisterContainers`, and `meta_handler.go`'s
      `RegisterMeta` each now mint one span per message via
      `Tracer.StartFromHeaders` (labeled from the subject fields each
      callback already parses — never re-derived positionally, same
      six-token-vs-five-token reasoning as 28c's `StartOutbound` amendment),
      record `entity_id`, thread it via `natstrace.ContextWithSpan` into the
      KV write and `publishNotify`/`publishRawNotify`, and close it with
      `sp.End`/`sp.Fail` on Ack/Nak. `RegisterShapeB` gained an `nc *nats.Conn`
      parameter purely so `register()`'s shared Consume callback can build a
      `Tracer` for it too (Shape B has no notify.* publish of its own).
      `ginkgo ./...` green (129 dictionary-adjacent specs: 120 in
      `dictionary`, plus `natstrace`/`kvstore`/`jstream`/`eventhandler`
      suites) — new specs added in `dictionary/trace_async_test.go` (evt.*
      publish shares its traceId with the reply-side span, including the
      no-inbound-traceparent root-span case; each of the four
      `RegisterShapeA`/`RegisterShapeB`/`RegisterContainers`/`RegisterMeta`
      Consume callbacks publishes exactly one span per message with the
      correct `entity`/`action`/`entity_id`), `dictionary/internal/eventhandler/publish_notify_test.go`
      (white-box `publishNotify`/`publishRawNotify` traceparent
      attach/omit/nil-`nc` cases), and two new specs in `internal/kvstore/kv_test.go`
      (`Store.Put`'s derived notify attaches/omits the `Traceparent` header
      per whether `ctx` carries a span).
- [x] **28e — accounts-service + auth.** `auth/token.go` has no NATS publish
      at all (it is pure JWT minting), so instrument one `http.Handler`
      decorator at `Mount` — symmetric to the micro decorator, covering every
      accounts REST endpoint — plus `publishAccountEvent`, which all four
      notify publishes already funnel through. Also the 9 `notify.accounts.*`
      subscriber call sites across 3 files.
      accounts-service had no `natstrace` at all (a new service for Phase 28)
      — new `internal/natstrace` package, same core span/redact/truncate
      mechanism as the other four services (byte-identical logic), but
      trading the micro.Request-shaped `Start`/`Middleware`/`SpanFrom` for an
      `HTTPMiddleware(next http.Handler) http.Handler`, since this service's
      transport is REST, not NATS micro. Wired once around the whole
      `*http.ServeMux` in `cmd/main.go` (`server.Handler =
      tracer.HTTPMiddleware(mux)`), covering both `accounts.Handlers.Mount`
      and `auth.Handlers.Mount`'s routes from one point — even more literally
      "one decorator" than the per-`AddEndpoint` wrap the other four services
      use. Span labels: `context="_platform"` (this service administers the
      tenant axis itself, no `{context}` of its own — CLAUDE.md's "context-free
      services" note), `service="accounts"`, `entity` from the first `/api/`
      path segment, `action` the lowercased HTTP method; exact path/method/
      status land as span attributes instead. A `>=400` response finishes the
      span as `Fail`, matching how a NATS reply's error path already does.
      `publishAccountEvent` (all four lifecycle publishers funnel through it)
      now mints its own outbound span via `Tracer.StartOutbound`, continuing
      whatever span the HTTP request carried, and attaches its `Traceparent`
      to the `notify.accounts.*` publish — required all four publishers
      (`publishAccountCreated`/`Suspended`/`Reactivated`/`JSLimitsUpdated`)
      to gain a `ctx` parameter (mechanical, `r.Context()` was already in
      scope at each of their 4 call sites). The 9 subscriber call sites
      (`shipping-service/dictionary/internal/rest/tenant.go`,
      `trading-partner-service/tradingpartner/internal/tenants/tenants.go`,
      `pricing-service/pricing/internal/tenants/tenants.go` — 3 subscriptions
      each) now start a per-message span via `StartFromHeaders`, continuing
      the inbound `Traceparent`, before calling `EnsureByName`/`TeardownByName`
      — the "created" closure is reused verbatim for "reactivated" in all
      three files, so the span's action label reads the actual subject's
      trailing token rather than a hardcoded string. **Design correction
      caught mid-implementation:** `publishAccountEvent`'s first draft simply
      forwarded the HTTP request's own span's `Traceparent()` unchanged onto
      the notify publish — a test written to assert "the notify's span id
      differs from the inbound request's span id" caught that this collapsed
      two hops into one span, inconsistent with how every other outbound
      publish in this phase (rpc.* clients, JetStream publishes) mints its
      own child span via `StartOutbound` first; fixed before landing.
      `ginkgo ./...`/`go test ./...` green in all five services (new
      `natstrace` suite in accounts-service, new BR-037 test in
      `accounts/handler_test.go`, new subscriber-side test in shipping-service's
      `dictionary/internal/rest/tenant_lifecycle_trace_test.go`).
- [x] **28f — Trace store + cross-account plumbing.** `TRACES` stream
      (LimitsPolicy, MaxAge 1h, MaxBytes capped), single-writer projector into
      `traces-_platform` keyed `trace.{traceId}`, per-tenant `obs.trace.>`
      stream export imported into PLATFORM, and `allow_trace: true` on service
      exports / stream imports in minted account JWTs. This closes the
      cross-account gap documented at `browserrpc/adapter.go`'s `Deps` comment.
      **Cross-account plumbing (accounts-service):** the design gap found
      mid-implementation — PLATFORM's own account JWT has to explicitly
      import each tenant's `obs.trace.>` export (no wildcard cross-account
      import exists in NATS's decentralized JWT model) — is solved with a
      fully dynamic re-signing mechanism, not a bootstrap-only one.
      `accounts/provisioner.go` gained: `tenantExports()` (a Stream export of
      `obs.trace.>` on the tenant's own claims — no `allow_trace` here,
      `jwt.Export.Validate` rejects that flag on anything but a Service
      export), wired into `newAccountClaims`'s cross-account branch via
      `claims.Exports.Add(tenantExports()...)`; and `addPlatformTraceImport`,
      which looks up PLATFORM's current claims via the already-generic
      `LookupAccountClaims`, idempotently appends a Stream import (`{Account:
      <tenant pubkey>, Subject: "obs.trace.>", Type: Stream, AllowTrace:
      true}` — legal here since `AllowTrace` requires a **Stream** import,
      the mirror image of the export-side restriction), and re-signs/re-pushes
      via the existing `pushClaimsUpdate` ($SYS.REQ.CLAIMS.UPDATE). Wired into
      `CreateAccount` right after the new tenant's own claims push succeeds,
      guarded by `platformPublicKey != ""` so the existing low-level
      provisioner tests (which pass an empty platform key and exercise no
      cross-account wiring at all) are unaffected. `nats/bootstrap-operator.sh`
      gained the day-0 nsc equivalent for the two pre-seeded tenants: each of
      ACME/GLOBEX now runs `nsc add export --account $account --subject
      "obs.trace.>"`, and PLATFORM imports both via `nsc add import
      --account PLATFORM --src-account $account_pub --remote-subject
      "obs.trace.>" --local-subject "obs.trace.>" --allow-trace` — verified by
      actually running the script against real `nsc` in a scratch copy (not
      the committed `nats/` artifacts) and decoding the resulting JWTs: ACME's
      export has `type: stream` with no `allow_trace`; PLATFORM's imports for
      both ACME and GLOBEX have `type: stream, allow_trace: true`, scoped to
      the correct account each. Tested against an embedded operator-mode NATS
      server (`accounts/provisioner_test.go`'s new spec): `CreateAccount` for
      two tenants leaves PLATFORM importing both without one overwriting the
      other, and a repeated `addPlatformTraceImport` call for an
      already-imported tenant (exposed to the black-box Ginkgo spec via a
      standard `export_test.go` bridge, `provisioner_export_test.go`) is a
      no-op, not a duplicate import — plus a white-box unit test
      (`provisioner_claims_test.go`) asserting the tenant-side export survives
      a plain re-sign. BR-AC30 amended with the concrete file/function names
      (the original text referenced a speculative `accounts/jwt.go`'s
      `MintTenantAccount`, which was never how this landed — the real grant
      is account-level, in `provisioner.go`, not a user-JWT `Sub.Allow` entry).
      **Trace store (shipping-service):** new
      `dictionary/internal/eventhandler/trace_store.go`'s `RegisterTraceStore`
      provisions the `TRACES` stream (`obs.trace.>`, `LimitsPolicy`, 1h
      `MaxAge`, 64 MiB `MaxBytes`) and a `traces` KV bucket (wrapped in
      `internal/kvstore.Store`, not raw `jetstream.KeyValue` — bare bucket
      name matching `kvstore`'s "named by the prefix alone" convention, with
      the platform scope folded into the key instead, `_platform.trace.
      {traceId}`; this was a design correction made while starting 28g's
      panel work, before any panel code was written — the original 28f
      landing used a raw-KV `traces-_platform` bucket, replaced here because
      wrapping it in `kvstore.Store` and calling `EnableNotify` gets the
      trace waterfall panel's entire bootstrap+live feed for free via
      mechanisms every other KV panel already uses, eliminating what would
      otherwise have been a bespoke KV-watch bridge goroutine and a new REST
      endpoint), then runs a durable consumer (`trace-store-projector`)
      whose `appendSpan` mirrors `container_handler.go`'s read-then-write
      pattern: read the existing KV record, skip the write if the incoming
      `spanId` is already present (redelivery-safe), else append the raw
      span JSON to the record's `spans` array and write back — merge, never
      overwrite-with-latest. Must run on `mono.PlatformFullJS()` (creating a
      stream/KV bucket is a `$JS.API.>` write, which shipping-admin's
      restricted `mono.JS()` is deliberately locked out of), so
      `monolith.Monolith.PlatformFullJS`'s doc comment was widened from
      "read-only cross-account introspection, nothing else should use it" to
      note this second, write-capable use; `platformNC` (`mono.NC()`, the
      restricted shipping-admin connection) is passed too, for
      `EnableNotify`'s publish — its existing `notify._platform.>`
      `--allow-pub` grant (`nats/bootstrap-operator.sh`) already covers the
      new `notify._platform.kv.traces.>` subject, no bootstrap script change
      needed. `accounts-service/auth/token.go`'s `MintAdminToken` gained that
      subject in its `Sub.Allow` list (the browser's admin connection
      previously had no KV-notify grant at all), with `token_test.go`'s
      pinned assertion updated to match. Nil-safe exactly like
      `RegisterRefdataNotify`/`RegisterRPCTraceNotify` when `platform.creds`
      isn't configured. Wired into `dictionary/composition.go` right after
      those two bridges. Tested against an embedded JetStream-enabled NATS
      server: `trace_store_test.go` (black-box) proves a published span lands
      in the KV bucket and that two spans sharing a `traceId` merge into one
      entry instead of one overwriting the other, plus the nil-safe no-op;
      `trace_store_appendspan_test.go` (white-box) proves same-`spanId`
      redelivery dedup and that two different traces never cross-contaminate
      each other's KV record. BR-036 amended (Phase 28f amendment) naming the
      concrete stream/bucket/functions behind "PLATFORM's cross-account trace
      store." `ginkgo ./...`/`go test ./...` green in both accounts-service and
      shipping-service.
- [x] **28g — Panel + OTLP bridge + retirement.** `[traces] [messages]` toggle
      in `RpcPanel.vue`, new `TraceWaterfall.vue`, the `otlp-bridge` container
      plus env-flagged `jaeger:all-in-one`, then retire `obs.rpc.*`/`obs.api.*`
      once the messages view derives from trace spans.
      **Panel piece landed (checked in with the user before continuing to the
      OTLP bridge/Jaeger/retirement piece):** `stores/ui.js` gained `rpcTab`
      (defaults `'traces'`, same App.vue-remount-survival pattern as
      `accountsTab`). `RpcPanel.vue` wraps its existing messages-view markup
      in `v-else` behind a small `.view-toggle` chip pair
      (`[traces]`/`[messages]`, matching ARCHITECTURE-ADMIN.md §4.5's
      design-history call for "a toggle inside this panel," not a page-level
      `Tabs` bar or a ninth nav item) and renders the new
      `TraceWaterfall.vue` for the traces tab. New `jsonHighlight.js`
      extracts `escapeHtml`/`highlightJson` out of `RpcPanel.vue` so both
      panels share one implementation. `TraceWaterfall.vue` bootstraps via
      the *existing* generic `GET /api/kv/buckets/platform/traces/entries`
      (no new REST endpoint — see BR-036's Phase 28g amendment for why) and
      subscribes live to `notify._platform.kv.traces.>` on the PLATFORM
      connection; per-trace summaries (root span, reply/consistent
      durations, ok/error, account count) and per-span waterfall rows
      (depth, offset, duration, account, crossing, sync/evtl/bad kind, the
      ack-line insertion point) are computed client-side from the KV
      record's raw `spans` array. Two known simplifications versus the
      approved mockup (`diagrams/admin-traces-panel.html`), both because the
      wire span has no field for them: the account gutter is a coarse
      PLATFORM/TENANT split, not a real per-tenant label (see BR-035's Phase
      28g amendment); the mockup's OTel `spanKind` tag is omitted rather
      than faked. `TraceWaterfall.spec.js` covers BR-035's four required
      assertions (5 specs, all green). Building this required a **prerequisite
      backend change, confirmed with the user first**: `natstrace.Span` in
      all five services gained `startedAt`/`traceSpan` gained `durationMs`
      (BR-036 Phase 28g amendment) — without it there was no wire signal for
      a span's own duration, only two different spans' finish timestamps,
      which the waterfall's proportional bars need to not be meaningless.
      Landed via 4 parallel agents (one per service, after doing
      shipping-service directly myself as the reference diff) plus one
      direct edit each to `BUSINESS_RULES-SHIPPING.md`. Verified live against
      the running docker-compose stack (all containers rebuilt), which
      surfaced and required fixing a real, unrelated capacity issue:
      PLATFORM's account was already at its 20-stream JetStream ceiling from
      11h of accumulated demo usage, so `shipping-service` crash-looped
      creating `TRACES`/`traces`. Fixed live and non-destructively — **the
      user ran a small one-off Go script** (using `nats/keys/operator-
      signing-key.nk`, the same `$SYS.REQ.CLAIMS.UPDATE` re-sign mechanism
      `accounts-service`'s own `Provisioner.UpdateAccountLimits` uses for
      tenants, just applied directly to PLATFORM since it isn't a tenant in
      the Store) to bump the limit 20→100 — the auto-mode classifier
      correctly declined to let the agent run this itself, since it directly
      wields the operator signing key outside the normal application code
      path. After that, real `obs.trace.*` traffic (refdata-service,
      accounts-service, the Admin UI's own `api.*`/auth calls) was observed
      flowing end-to-end: bootstrap fetch, live KV-notify updates (trace
      count grew live with the panel open, no reload), waterfall bar
      rendering, and the span detail pane (headers + syntax-highlighted
      body) all confirmed working in the browser. Real cross-account
      (ACME→PLATFORM) trace assembly was **not** verified on this live
      stack — the running `nats.conf`'s resolver still has the pre-28f
      tenant JWTs (no `obs.trace.>` export/import), and regenerating it
      would need a full `docker compose down -v`, which was deliberately
      not done to avoid destroying 11h of accumulated demo data; that gap is
      covered structurally by 28f's own Go integration tests instead. The
      `[messages]` view and its `obs.rpc.*`/`obs.api.*` traffic were
      confirmed unaffected (not yet retired — that's still pending below).
      **OTLP bridge + Jaeger landed:** new standalone module
      `backend/otlp-bridge/` (own `go.mod`, own `Dockerfile`, ~230 lines
      across `cmd/main.go` and `internal/otlpmap/map.go` — not hexagonal
      like the five instrumented services, since it's a translation utility
      with no domain layer or business rules of its own). Per
      ARCHITECTURE-COMMUNICATIONS.md § 6's already-approved design: a
      `TRACES` JetStream consumer, never an in-process SDK — `openConsumer`
      switches between a durable, `DeliverNewPolicy` live-tailing consumer
      (default, resumes across restarts) and an ephemeral `DeliverAllPolicy`
      consumer (`OTLP_BRIDGE_REPLAY=true`, re-exports the whole retained
      window on demand); a `batcher` accumulates spans until `batchSize`
      (100) or `batchInterval` (2s), POSTs one OTLP/HTTP export request, and
      only Acks the batch on a 2xx response — a failed POST Naks every
      message in it, so nothing is lost while Jaeger is unreachable (the
      span stays on `TRACES`, redelivered next attempt). `internal/otlpmap`
      is the pure field-for-field mapping (7 unit tests, no NATS
      dependency): `WireSpan` mirrors only the subset of `natstrace.go`'s
      `traceSpan` the bridge needs (mirroring `trace_store.go`'s own
      `traceSpanKey` precedent for "just enough, not the full shape");
      `spanKind` is never set (OTLP's `SPAN_KIND_UNSPECIFIED` zero value) —
      inventing one would be interpretation, not mapping, matching BR-035's
      documented spanKind omission. **Corrected against a live Jaeger
      rejection, not assumed:** trace/span ids are passed through as the
      same hex `natstrace` already emits, *not* re-encoded to base64 —
      generic protobuf JSON mapping would base64-encode a `bytes` field,
      but Jaeger's OTLP/HTTP receiver decodes ids through OTel collector's
      `pdata` codec, which expects hex specifically (confirmed live: a
      base64-encoded id was rejected with `"invalid length for ID"`); fixed
      and pinned with a test (`TestMarshalExportRequestPassesIdsThroughAsHex`)
      before moving on. `docker-compose.yml` adds `jaeger` (image
      `jaegertracing/all-in-one:1.68.0`, conventional ports 16686/4318 — same
      "keep the widely-recognized port" precedent as NATS/Postgres) and
      `otlp-bridge` (reuses `platform.creds` — read-only JetStream consume
      needs no dedicated credential), both behind Compose's `otlp` profile
      so a bare `docker compose up` is unaffected: two services added or
      removed, no env flag threaded through five service binaries, per the
      design's own "no-code toggling" rationale. Verified live against the
      running stack: real `refdata`/`accounts` spans (correct parent/child
      references, `subject`/`entity`/`action`/`direction`/`correlationId`
      tags) confirmed via both Jaeger's `/api/traces` query API and its UI.
      `go vet`/`gofmt`/`go test ./...` all clean; `ginkgo ./...` re-run in
      shipping-service afterward to confirm zero effect on the five
      instrumented services (9/9 suites still green, as expected — nothing
      in any of them changed). README.md's port table and Docker instructions
      updated with the `--profile otlp` command.
      **Retirement landed.** Investigated before touching anything (a
      dedicated Explore pass, not assumption): the publish side of
      `obs.rpc.*`/`obs.api.*` was already fully dead everywhere — Phase
      28a-28e had already replaced every adapter's `publishObs` call with a
      natstrace span, so what remained was dead/no-op plumbing plus stale
      docs, not live traffic to migrate. What actually changed:
      - **Frontend:** `RpcPanel.vue`'s `[messages]` tab no longer subscribes
        `obs.rpc.>`/`obs.api.>` (both already carrying nothing for any
        service) — it now derives from the same `obs.trace.*`/`traces` KV
        bucket feed `TraceWaterfall.vue` reads, flattened to one row per
        *span* instead of one row per *trace*. Real, unavoidable UX
        difference: a span carries only the reply side (BR-037), so the old
        two-pane Request | Reply detail split is gone in favor of one
        Body/Headers section, and the three-state status model
        (pending/ok/error) becomes two (ok/error) — a span is only ever seen
        already-finished. `getRpcTraceReplay` removed from `api.js`. New
        `RpcPanel.spec.js` (5 specs) covers the flattening, family/status
        filtering, and the single-pane detail view — this component had no
        prior test coverage at all.
      - **shipping-service:** `eventhandler.RegisterRPCTraceNotify` and its
        call site removed; `rpcTraceReplayOnce`/`GET /api/rpctrace/replay`
        removed; the synthetic-RPCTRACE-traffic tests that existed only to
        exercise this now-removed bridge/endpoint deleted with a removal
        note (`platform_notify_test.go`, `replay_test.go`); Swagger
        regenerated (`swag init`), confirmed zero `rpctrace` references
        remain.
      - **refdata-service:** `RPCTraceStreamName`/`RPCTraceMaxAge` consts and
        the `RPCTRACE` stream provisioning removed from `composition.go`;
        `natsrpc.ObsSubjectWildcard` (kept post-28b only to back that
        provisioning) removed now that nothing needs it.
      - **accounts-service:** `MintBrowserToken`'s `obs.api.>` subscribe
        grant dropped. Also caught and fixed a bug the investigation
        surfaced but I hadn't planned to touch yet: `MintAdminToken` still
        granted `notify._platform.rpctrace.>`, a subject nothing publishes
        to anymore post-retirement — dropped that too, with
        `token_test.go`'s assertions updated for both functions (ginkgo
        re-run confirmed both catches: 84/84 + 19/19 + 6/6 specs green).
      - **`nats/bootstrap-operator.sh`:** `shipping-admin`'s RPCTRACE
        `$JS.API.CONSUMER.*` grants dropped, REFDATA's kept. Verified live
        against real `nsc` in a scratch environment (same rigor as 28f's
        script verification) — decoded the resulting JWT and confirmed the
        exact expected grant set, no RPCTRACE remnants.
      - **Docs:** `BUSINESS_RULES-SHIPPING.md` (BR-026 retired, BR-027
        tightened), `BUSINESS_RULES-REFDATA.md` (BR-D29/BR-D36 retired,
        BR-D37 tightened — BR-D29/BR-D36 had never recorded their Phase 28b
        supersession in the markdown at all, a pre-existing doc gap fixed
        retroactively alongside the Phase 28g retirement), `BUSINESS_RULES-
        ACCOUNTS.md` (BR-AC18 amended for both token functions), and
        `ARCHITECTURE-ADMIN.md`/`ARCHITECTURE-ACCOUNTS.md`/`ARCHITECTURE.md`
        (panel index, archetype table, credential mermaid diagram, and two
        historical connection-purpose tables corrected — several referenced
        the stale `traces-_platform` bucket name from before 28g's own
        bucket rename, caught and fixed in passing).
      Verified live against the running stack (all four affected containers
      rebuilt): `[messages]` renders real flattened spans with working
      status/family filters and the single-pane detail view; `[traces]`
      confirmed unaffected (159 live traces, unchanged); all API calls
      200 OK, no new console errors. Full backend re-sweep after every
      change: shipping-service 9/9, refdata-service 3/3, accounts-service
      3/3 suites green; frontend `vitest run` 11/11 files, 81/81 specs
      green (76 → 81: `RpcPanel.spec.js`'s 5 new specs, this component's
      first test coverage); `go vet`/`gofmt` clean on all three touched Go
      services.
      **Final verification pass — complete.** Surveyed existing coverage
      first (Explore agent) rather than assuming what was missing; found two
      real gaps and closed both:
      - **Cross-hop single-traceId assertion** (the checklist's "one
        assertion that matters most") was genuinely missing — no test drove
        shipping's `api.*` through to refdata's `rpc.*` and checked one
        shared `traceId`. Added
        `TestLookupSharesTraceIdAcrossShippingAndRefdataHops` in
        `shipping-service/internal/refdataconsumer/consumer_test.go`: a
        simulated refdata inbound responder (`natstrace.StartFromHeaders`,
        the same stand-in-for-the-other-service's-copy pattern the existing
        `TestLookupContinuesParentSpanAttachedViaContext` already uses)
        proves shipping's outbound span and refdata's inbound span agree on
        one `traceId` with `refdataSpan.ParentSpanID == shippingSpan.SpanID`.
      - **Redaction/truncation contract** was cloned into 4/5 services but
        silently absent from accounts-service's HTTP-middleware copy —
        traced to a real (if narrow) gap: `HTTPMiddleware` always calls
        `sp.End(nil, nil)`/`sp.Fail(err, nil, nil)`, never threading a
        request/response body through, so BR-036's redaction/truncation path
        was reachable only via `publishAccountEvent`'s outbound notify span,
        never the inbound HTTP span, and untested either way. Added an
        `It` in `accounts-service/internal/natstrace/natstrace_test.go`
        exercising `StartFromHeaders`+`End` directly (mirroring the other
        four services' `It`) to pin the shared `finish()` mechanics; did
        **not** change `HTTPMiddleware` to capture HTTP bodies — that's a
        separate buffering-cost/risk tradeoff, not this checklist's scope.
      - Invisible-timeout regression, redaction/truncation (4/5 services),
        and account-isolation (no `Sub.Allow` for `obs.trace.>` in a browser
        JWT) were already covered — confirmed by inspection, not re-written.
      - **No-regression check**, live: Services panel (`$SRV.STATS`) — 4
        services/64 endpoints, request/error counters intact; `[traces]` —
        196 live traces, waterfall detail renders; `[messages]` — flattened
        span rows render per the retirement design; zero console errors.
      - **Live cross-account (ACME→PLATFORM) e2e** — deferred earlier in
        this session because it needs `docker compose down -v` (destroys
        accumulated demo data) so `bootstrap-operator.sh`'s edited grants
        take effect from a truly fresh boot, not just the long-running dev
        stack. Confirmed with the user before running it. Ran
        `down -v` + `up --build`, drove a real ACME-tenant page load
        (shipping-frontend → its own `api.*` → refdata's `rpc.*`), and
        confirmed in the Admin UI (viewed as tenant `acme`) that the
        resulting trace correctly shows a `PLATFORM`-account span
        (`rpc.acme.refdata.type.list.v1`, spans:1, accounts:1).
        **Correction, found later the same session:** this check was
        insufficient — refdata-service always connects natively *as*
        PLATFORM (BR-D08), so its spans never actually cross an account
        boundary regardless of whether ACME's export/import is wired at
        all; the `PLATFORM` label proved nothing about the cross-account
        hop. The real test is a service that publishes `obs.trace.*` from
        a genuinely per-tenant connection — shipping/pricing's own
        `browserrpc`-instrumented `api.*` spans. Driving a real
        `api.acme-atlantic-fleet.shipping.ship.*` call and raw-subscribing
        `obs.trace.>` as PLATFORM (`nats sub`, bypassing the Admin UI/KV
        layer entirely to isolate the account boundary) showed **zero**
        shipping spans arriving, while the same raw subscribe as ACME
        showed them publishing correctly on ACME's own account. Root
        cause: decoding the live `nats/resolver/{ACME,PLATFORM}.jwt`
        showed ACME's account JWT had **no `obs.trace.>` export at all**,
        and PLATFORM had no matching import — `bootstrap-operator.sh`'s
        Phase 28f export/import lines exist in the script but were never
        applied, because the script is idempotent from a scratch `nsc`
        store ("exits early if `operator.jwt` exists, unless `--force`")
        and nobody had re-run it with `--force` since those lines were
        added; the checked-in JWTs simply predated them. Fixed by running
        `bootstrap-operator.sh --force` (confirmed via the same JWT-decode
        that `ACME.exports`/`PLATFORM.imports` now include `obs.trace.>`)
        followed by another `down -v` + `up --build` + `otlp-bridge`
        restart, then re-ran the identical raw-subscribe-as-PLATFORM test
        live: a real ship command's full span chain
        (`ship.register`/`ship.arrive`/`ship.registered`/`ship.arrived`,
        5 spans) now arrives and renders correctly in the Admin UI's
        `[traces]` panel from the ACME tenant view. `ginkgo ./...` in
        shipping-service re-run green after the fix (9/9 — this was a
        NATS-config-only change, no Go code touched).
      - **OTLP-bridge-oracle-vs-Jaeger** — re-confirmed post-rebuild: the
        bridge reconnected through the NATS restart with no re-deploy, and a
        live Jaeger UI search (service `refdata`) shows real spans.
      All test suites re-run green after the rebuild: shipping-service 9/9
      (10 specs added net across the two new tests), accounts-service
      84/84 + 19/19 + 7/7 (natstrace 6→7).
- [x] **Tests:** per sub-phase, one Ginkgo `Context` per business rule
      (Quality Rule 1) and `ginkgo ./...` green in every touched service
      (Quality Rule 2); `vitest run` for 28g. The contract test asserting the
      `traceSpan` JSON shape *and* that an old-shape `obsEnvelope` still
      decodes is cloned into all five services — that test is what makes the
      duplication safe. **The one assertion that matters most:** drive
      shipping's `api.*` → refdata's `rpc.*` and assert a single `traceId`
      across both `obs.*` families with a correct parent/child `spanId` chain.
      Plus the invisible-timeout regression (no responder → a span with
      `statusCode: error`, which today produces no record at all), truncation
      and redaction behaviour, and that a tenant browser JWT has **no**
      `Sub.Allow` for `obs.trace.>`.

#### Business rules (confirmed 2026-08-15, written into domain files)

No new `BR-` prefix: cross-cutting observability rules are paired across
existing domain files (precedent BR-D36/BR-026, BR-D37/BR-027), and Admin-UI
presentation rules go to `BUSINESS_RULES-SHIPPING.md` as bare `BR-0NN` even
when not about ships.

- **BR-036** (SHIPPING) — the `obs.trace.*` envelope is a strict superset of
  the `obs.rpc.*`/`obs.api.*` envelope, carrying W3C trace identity and
  OTLP-shaped span fields; it publishes to the PLATFORM account only, applies
  a redaction denylist before a 4 KiB payload cap, and flags truncation rather
  than cutting silently. Inherits BR-D26: never blocks or fails a business path.
- **BR-D39** (REFDATA) — the same wire contract on refdata-service's `natsrpc`
  publisher side, mirroring BR-036 as BR-D36 mirrors BR-026.
- **BR-037** (SHIPPING) — trace context propagates on every outbound NATS
  message (`rpc.*` request, `evt.*` publish, `notify.*` publish). One span per
  *logical* RPC call with a retry-count attribute, never one per attempt. A KV
  entry cannot carry trace context; the derived notify does.
- **BR-035** (SHIPPING) — presentation rule for the Request/Reply & Traces
  panel: one row per trace; the reply-ack boundary separates synchronous from
  eventual work; the account gutter marks boundary crossings; the header states
  both *reply* and *read-model-consistent* durations. Obeys §2.2's
  exceptional-state rule — a trace with no async tail renders no ack line, and
  a rejected command legitimately has no tail.
- **BR-AC30** (ACCOUNTS) — minted account JWTs carry `allow_trace: true` on
  service exports and stream imports, plus a per-tenant `obs.trace.>` stream
  export imported into PLATFORM. Without this, traces stop dead at the account
  boundary.

*To verify before writing these:* whether `BUSINESS_RULES-PRICING.md` and
`-TRADING-PARTNER.md` already carry obs-envelope rules; if so they need
parallel `BR-P25`/`BR-TP15` entries for symmetry. Also note
`BUSINESS_RULES.md`'s index ranges are already stale (says REFDATA
`BR-D01–D28`, actually D38; ACCOUNTS `AC01–AC13`, actually AC29) — worth
correcting in the same pass.

#### Docs (done ahead of implementation, 2026-08-14)

- [x] `ARCHITECTURE-ADMIN.md` — §1 panel row retitled, §2.1 layout 2 credited,
      §3.1 gains the KV-watch *variant* of snapshot+notify (explicitly **not** a
      fourth archetype — a KV watch replays then goes live on one subscription,
      so it structurally cannot have BR-D29's bootstrap duplicate/gap window),
      §4.5 rewritten with the UI design and a `Design history` table covering
      the viewer and placement arguments.
- [x] `ARCHITECTURE-COMMUNICATIONS.md` — §2.1 gains the `obs.trace.*`
      Supportive row (and the "Five families" count corrected to six), §2.2
      gains the grammar line, §6 gains a Phase 28 blockquote amendment
      embedding `images/otlp-bridge-ingest.png` (ingest topology, per-message
      handling inside the bridge, and the in-process-vs-consumer contrast), and
      the closing payload-sensitivity bullet is amended from advisory to
      enforced.
- [x] Three PNGs exported and embedded: `otlp-bridge-ingest.png` (§6 — ingest
      topology, per-message handling, in-process-vs-consumer contrast),
      `traces-span-envelope.png` (§6 — the HAVE/NEW envelope table and the
      instrumented-surfaces coverage table), and `admin-traces-panel.png`
      (ARCHITECTURE-ADMIN.md §4.5 — the reviewed panel mockup).
- [x] New diagram tooling: `diagrams/export-html-png.mjs` renders a
      hand-authored inline-SVG or mockup HTML page to PNG, alongside the
      existing `export-png.sh` for Draw.io workbook pages. Supports
      `--clip="<selectors>"` to capture one component out of a page that also
      carries prose — used to lift the panel chrome and the capture tables out
      of the mockup separately. These are the first PNGs in `images/`
      **without** a Draw.io source, so `export-png.sh` does not regenerate
      them; editable sources are `diagrams/otlp-bridge-ingest.html` and
      `diagrams/admin-traces-panel.html`, with re-export commands recorded
      beside each embed. Worth folding into the workbook if that divergence
      becomes annoying.
- [ ] `obsidian/POC-Dictionaries/` — findings note on correlation-id vs trace-id
      and why the trace store is Shape A.

### Phase 29 (PROPOSED) — NATS 2.11 Server-Hop Tracing ("Trace this subject")

#### Goal

Phase 28 answers "shipping called refdata and it took 40ms." It cannot answer
"the message was dropped at the account import boundary" — which, in an
operator-mode deployment where every cross-service call goes through a JWT
export/import, is the failure mode that is hardest to diagnose and produces
the least evidence.

NATS 2.11's distributed message tracing reports, per server hop: ingress
(`in`), egress (`eg`), subject mapping (`sm`), stream export (`se`), service
import (`si`), and JetStream store (`js`) — each with the server's own error
string. Add a "Trace this subject" control that publishes with
`Nats-Trace-Dest` and renders the returned hop tree, interleaved into the same
waterfall as Phase 28's application spans.

- [ ] Backend: publish with `Nats-Trace-Dest` (+ `Nats-Trace-Only` for the
      dry-run default, as `nats trace` itself defaults to) and collect trace
      events off the destination subject.
- [ ] Frontend: render hop events as grey hairline ticks rather than duration
      bars — they have no meaningful duration (already specified in
      ARCHITECTURE-ADMIN.md §4.5's UI design).
- [ ] **Why this is worth its own phase:** zero code in any service and no
      per-message cost, so it shares nothing with Phase 28's implementation.
      Requires server 2.11+ and `allow_trace: true` (landed in 28f/BR-AC30).
- [ ] **The payoff for having chosen `traceparent` in Phase 28:** in
      trace-context mode the NATS server stamps *our* trace id onto its own hop
      events, so application spans and infrastructure hops land on one
      waterfall keyed identically. No off-the-shelf tool does this.

### Phase 30 (PROPOSED) — observability-service: Extract Cross-Account Diagnostics from shipping-service

#### Goal

The NATS/SYSTEM diagnostic endpoints (`/api/nats/connections`,
`/api/nats/account-activity`, `/api/nats/log`, `/api/jetstream/streams`,
`/api/jetstream/replay`, `/api/kv/buckets*`) and the `TRACES` stream
consumer (`RegisterTraceStore`, projecting into the `trace-request-reply` KV
bucket) all live in `shipping-service` today for one reason: it is
currently the only service holding live NATS connections into PLATFORM
*and* every tenant account (Main-POC-Plan.md:308's Phase 21 note), so it
was the only place that could answer them. None of this is shipping domain
logic. Extract it into a new PLATFORM-account service,
**`observability-service`**, matching the `obs.*` subject family CLAUDE.md
already reserves for this purpose, and the natural landing spot for the
system/performance telemetry (memory, CPU, JetStream/consumer lag) this
phase's discussion flagged as the next thing wanted here.

#### Decisions locked (with user, 2026-08-16)

- New service on the PLATFORM account, port **7205** (next free in the
  7200–7299 backend range per CLAUDE.md's port allocation table).
- Absorbs, lifted verbatim except where noted: `dictionary/internal/rest/
  {nats_ops,nats_log,streams,kv,replay}.go`'s six handlers, and
  `dictionary/internal/eventhandler/trace_store.go`'s `RegisterTraceStore` +
  `trace-request-reply` KV projection.
- `otlp-bridge` stays a separate deployable — already correctly scoped as a
  translator with no REST API of its own; not folded in.
- **Chose the export/import extension over duplicating shipping-service's
  raw per-tenant connection fan-out** (`TenantResources`/
  `ensureTenantResources`): a second service holding a live credential into
  every tenant account would be a second instance of exactly the pattern
  Phase 21's "target partitioning" note already flagged for retirement, with
  a strictly broader permission surface than an explicit, enumerable
  account-JWT export. `observability-service` ends up holding exactly one
  NATS connection — PLATFORM-scoped, restricted like `shipping-admin` — no
  `.creds` file of its own into any tenant account.

#### Design

- **Two new per-tenant export/import pairs**, wired the identical way
  BR-AC30/`addPlatformTraceImport` already wires `obs.trace.>` — this is
  generalizing a proven mechanism, not inventing one:
  1. **`$SRV` discovery** — each tenant account exports `$SRV.PING`/
     `$SRV.INFO`/`$SRV.STATS` as a service export; PLATFORM imports it per
     tenant with a local remap (e.g. local `monitor.{tenant}.srv.>`),
     letting `observability-service` broadcast `$SRV.STATS` and receive
     replies across the account boundary the same way the `rpc.*` service
     imports already do (ARCHITECTURE-ACCOUNTS.md:83's "`$SRV.>` is not
     exported to tenants" is the opposite direction — PLATFORM's own $SRV
     kept private from tenants — and does not block this reverse export).
  2. **`$JS.API` subset for JetStream/KV introspection** — each tenant
     exports a narrow, explicit subject list, deliberately **not**
     `$JS.API.>` (ARCHITECTURE-ACCOUNTS.md:87–101's caveat: the full
     namespace grants stream *management* — create, delete, purge — not
     just visibility). **This is not a pure read-only grant** — traced
     against the exact call chain in `dictionary/internal/rest/{kv,replay}.go`
     and the `$JS.API` subject constants in the pinned `nats.go@v1.52.0`
     (`go.mod`), `/api/jetstream/replay` and the KV-entries watch both
     create (and in replay's case, explicitly delete) real ephemeral
     JetStream consumers — there is no way to serve them from list/info
     subjects alone:
     - `STREAM.LIST` — bucket + stream listing (`listKVBuckets`,
       `listStreams`; stream consumer *count* rides inside this response's
       `State.Consumers`, so `CONSUMER.LIST`/`NAMES` are never called and
       are excluded)
     - `STREAM.INFO.*` — resolves a named bucket's backing stream
       (`kvBucketEntriesOnce`'s `js.KeyValue()` call)
     - `CONSUMER.CREATE.*` — the legacy nameless-ephemeral form the KV-watch
       path uses (`kv.go`'s `WatchAll` → core-nats `nats.OrderedConsumer()`
       push subscribe); server assigns the name, so this 5-token subject
       can never collide with a caller-chosen durable name
     - `CONSUMER.CREATE.*.*` — the named-ephemeral form
       `jetstreamReplayOnce`'s `js.OrderedConsumer()` uses; the name is
       always a nuid `generateConsName()` value (confirmed: `replay.go`
       sets `FilterSubjects` plural, which `consumer.go:319`'s branch
       condition routes away from the 7-token filter-in-subject form, so
       that variant is excluded as unused)
     - `CONSUMER.MSG.NEXT.*.*` — pulling messages off the replay consumer
     - `CONSUMER.DELETE.*.*` — cleanup for both paths (`replay.go:85`'s
       explicit delete; the KV-watch path's `Stop()`/`Unsubscribe()` — not
       yet empirically confirmed to round-trip a delete rather than rely on
       `InactiveThreshold` reaping, worth pinning with a live trace when 30b
       lands)

     `STREAM.MSG.GET.*` is excluded — no current handler calls it (can be
     added later if a "jump to sequence" feature needs a direct single-
     message fetch).

     **`CONSUMER.DELETE.*.*` is the one export that isn't fully closed by
     subject scoping.** NATS wildcards match any single token regardless of
     content, so this subject permits deleting *any* consumer on *any*
     stream in the tenant account — including a business-critical durable
     one — not just an ephemeral one this connection created. That has to be
     an application-layer invariant, not a JWT-scope one:
     `observability-service` must only ever call `DeleteConsumer` with a
     name it just received from its own preceding `CREATE` response in the
     same request (exactly what `replay.go:85` already does today via
     `consumer.CachedInfo().Name`, never a caller-supplied value) — 30b adds
     a test enforcing that discipline, not just a happy-path spec.
- Both declared in the same two places `obs.trace.>` already is: the day-0
  `nsc` equivalent in `nats/bootstrap-operator.sh`, and
  `accounts/provisioner.go`'s `CreateAccount` — tenant-side via
  `tenantExports()`, PLATFORM-side via a new `addPlatformMonitorImport`-
  style helper mirroring `addPlatformTraceImport` (re-signs PLATFORM's
  claims through `pushClaimsUpdate`/`$SYS.REQ.CLAIMS.UPDATE`, idempotent per
  `(account, subject)` pair).
- Admin UI: `admin/src/api.js` client functions + dev-mode Vite proxy
  entries for the six diagnostic endpoints repoint from shipping-service's
  port to `observability-service`'s (7205). The NATS/SYSTEM nav panels
  themselves (components, props) are unchanged — only where they fetch from
  moves.
- Migration is additive-then-subtractive within the phase: stand up
  `observability-service` answering the same endpoints, flip the Admin UI's
  proxy, verify parity live, then delete the moved code from
  `shipping-service` — not left dual-registered past this phase.

#### Sub-phases (each independently landable)

- [x] **30a — accounts-service: `$SRV` discovery export/import.** (DONE
      2026-08-16) Not quite BR-AC30's mechanism exactly — discovered
      mid-implementation that `$SRV` needs a **Service** export/import
      (`ResponseType: jwt.ResponseTypeStream`, not the library default
      `Singleton` — `$SRV.STATS` is answered by every registered instance,
      not one), unlike `obs.trace.>`'s **Stream** export, and no `$SRV`
      subject had ever crossed an account boundary in this codebase before
      this rule (confirmed via `go doc`/source on the pinned
      `nats.go@v1.52.0`/`jwt/v2@v2.8.2`, and via `nsc add export --help` for
      the `--response-type` flag). Live cross-account reply-fanout routing
      is accordingly *not* proven by this sub-phase's tests — deferred to
      Phase 30i's live verification, per the user's explicit choice not to
      spike-test it first. Landed: BR-AC31 (`BUSINESS_RULES-ACCOUNTS.md`,
      `BUSINESS_RULES.md` index bump); `tenantExports()` +
      `addPlatformMonitorImport` + `CreateAccount` wiring
      (`accounts/provisioner.go`); day-0 `$SRV.>` export/import block
      (`nats/bootstrap-operator.sh`); `TestNewAccountClaimsAddsTenantMonitorExport`
      + the PLATFORM-side idempotency spec (`provisioner_claims_test.go`/
      `provisioner_test.go`) — the latter also fixed to filter by subject
      instead of asserting PLATFORM's raw `Imports` length, which this
      phase's own second import broke; browser-exclusion covered by
      pre-existing `auth/token_test.go` `ConsistOf` assertions, no new test
      needed since `auth/token.go` is untouched. `ginkgo -r ./...` green,
      85/85 specs (accounts-service).
- [x] **30b — accounts-service: `$JS.API` introspection export/import.**
      (DONE 2026-08-16) The six-subject list from Design, wired via the same
      mechanism as 30a, with per-subject `ResponseType` (five `Singleton`,
      `CONSUMER.MSG.NEXT.*.*` alone `Stream` — a batch pull yields multiple
      replies per request, the same not-yet-proven-live mechanism as
      BR-AC31's `$SRV.>`). Landed: BR-AC32 (`BUSINESS_RULES-ACCOUNTS.md`,
      `BUSINESS_RULES.md` index bump); `jsAPIExportSubjects` +
      `addPlatformJSAPIImport` + `CreateAccount` wiring
      (`accounts/provisioner.go`); day-0 six-subject export/import block
      (`nats/bootstrap-operator.sh`); `TestNewAccountClaimsAddsTenantJSAPIExports`
      (positive six-subject + `ResponseType` check + negative "no other
      `$JS.API` subject leaked in" check) and the PLATFORM-side idempotency
      spec, both in `provisioner_claims_test.go`/`provisioner_test.go`. The
      `DeleteConsumer`-name-provenance code-level test/lint is **not** part
      of this sub-phase — `DeleteConsumer` doesn't exist yet in
      `observability-service` until 30e lifts the JetStream/KV handlers;
      carried forward as a 30e checklist item instead. `ginkgo -r ./...`
      green, 86/86 specs (accounts-service).
- [x] **30c — scaffold `observability-service`.** (DONE 2026-08-16) Deviated
      from the original `refdata-service`-mirroring description in one
      respect: `observability/internal/{natsops,tracestore}` were **not**
      pre-created as empty packages — CLAUDE.md's own guidance against
      premature scaffolding — and will be created by 30f/30g when they have
      real content. What landed: `go.mod`/`cmd/main.go` (NATS connect only,
      no Postgres — mirrors `refdata-service`'s `cmd/main.go` minus its DB
      wiring), `observability/composition.go` +
      `observability/internal/rest` with a `GET /healthz` only (every real
      endpoint is 30d–30g), `Dockerfile` (mirrors `otlp-bridge`'s), Docker
      Compose entry on port 7205. Also minted a dedicated restricted
      PLATFORM credential (`nsc add user --account PLATFORM observability`,
      `nats/bootstrap-operator.sh`) rather than reusing the unrestricted
      `platform.creds` `refdata-service`/`otlp-bridge` use — narrow
      `allow-pub`/`allow-sub` of `monitor.>,$SRV.>` (+ `_INBOX.>` sub) is
      everything BR-AC31/BR-AC32's imports resolve to; PLATFORM-native
      `$JS.API` access is deliberately not yet granted, added when 30e/30f
      need it. `go build ./...`/`go vet ./...` clean; `docker compose build
      observability-service` and `docker compose config -q` both clean.
- [x] **30d — lift the pure HTTP-proxy panels.** (DONE 2026-08-16)
      `/api/nats/connections`, `/api/nats/account-activity`,
      `/api/nats/log` — no dependency on 30a/30b confirmed true, but a real
      dependency surfaced mid-implementation that the original sub-phase
      description didn't anticipate: both panels' `tenantLabel` resolution
      (`tenantLabelsByAccount` in the original) worked by matching `/connz`
      rows against the `LocalAddr` of connections **shipping-service itself
      held** — one per tenant, via `TenantResources`. `observability-service`
      holds exactly one connection (PLATFORM), so there is nothing to
      match against; labeling would have silently gone dark. Fixed by
      adding a new `AccountsClient` (`observability/internal/rest/
      accounts_client.go`) that calls accounts-service's existing
      `GET /api/accounts` (already serving the Admin UI's Accounts panel,
      already carrying `{name, publicKey}`) for a direct pubkey→name
      lookup — mirrors accounts-service's own `RefdataClient` pattern
      (`accounts/refdata.go`), needs no new NATS grant, and is simpler than
      the two-stage trick it replaces (no more "which connections are ours"
      matching). New env vars `ACCOUNTS_URL`/`ACCOUNTS_AUTH_SECRET`
      (`docker-compose.yml`, `cmd/main.go`, `observability/composition.go`'s
      new `Config`); `NATS_MONITOR_URL`/`NATS_LOG_PATH` also wired
      (`nats-logs` volume mounted read-only, mirroring shipping-service's
      own mount). Handlers lifted into
      `observability/internal/rest/{nats_connections,nats_log}.go`,
      `Services` (the third original panel in `nats_ops.go`) deliberately
      left for 30f. Tests ported/adapted from `nats_ops_test.go`/
      `nats_log_test.go`, the label specs rewritten against a mocked
      accounts-service instead of real embedded-NATS `LocalAddr`s; `go
      test ./...` 20/20 green, `go vet ./...` clean, `docker compose build
      observability-service` and `docker compose config -q` both clean.
- [x] **30e — lift JetStream/KV introspection.** (DONE 2026-08-16)
      `/api/jetstream/streams`, `/api/jetstream/replay`, `/api/kv/buckets*`
      — the "code-level `DeleteConsumer` lint" carried forward from 30b is
      satisfied by construction, not a separate lint pass: `replay.go`'s
      only `DeleteConsumer` call site uses `consumer.CachedInfo().Name` (the
      server-assigned name from this handler's own preceding `CREATE`),
      never a request-derived value, and a new test
      (`TestJetstreamReplayOnceReturnsAllRetainedMessages`) asserts the
      consumer is actually gone afterward via `ConsumerNames`.

      A mechanism discovery changed the implementation shape from what the
      sub-phase description assumed: rather than hand-rolling `$JS.API`
      requests against `monitor.{tenant}.js.*`, the pinned `nats.go`'s
      `jetstream.NewWithAPIPrefix` transparently honors a custom API prefix
      for every operation these handlers need — confirmed by reading
      `apiSubject()`'s prefix-concatenation and `legacyJetStream()`'s
      prefix propagation to the KV-watch path in the actual `nats.go`
      source, not assumed. So `introspectableAccounts`/`jsForAccount`
      (`kv.go`) construct one `jetstream.JetStream` per account —
      `jetstream.New(nc)` for `"platform"`, `jetstream.NewWithAPIPrefix(nc,
      "monitor."+name+".js")` per tenant, tenant names from
      `AccountsClient.TenantNames` (new method, same accounts-service
      `GET /api/accounts` call 30d already added) — replacing
      shipping-service's `TenantResources` map iteration entirely, and the
      REST handlers themselves (`streams.go`/`kv.go`/`replay.go`) are
      otherwise a close port.

      Two more deliberate deviations, both documented in `replay.go`'s
      package doc comment: (1) no ship/container-specific subject filter —
      domain knowledge that doesn't belong in a domain-agnostic tool, and
      `domain.StreamSubjects()` isn't reachable from a separate `go.mod`
      anyway; every stream now replays unfiltered (cosmetic effect only).
      (2) `account`/`stream` are both required query params for replay, not
      defaulted — the original defaulted to shipping-service's own
      "currently active tenant" session concept, which doesn't exist here.

      `nats/bootstrap-operator.sh`'s `observability` user gained the same
      six-subject `$JS.API` grant BR-AC32 already defines, applied directly
      to its own PLATFORM account (not through any `monitor.*` remap) —
      the deferred item from 30c's scaffold, closed here rather than
      speculatively ahead of this content. Deliberately not
      shipping-service's `PlatformFullJS` pattern (a second, broader-access
      connection) — one connection, narrowly scoped, per this phase's
      design throughout.

      Tests use a real embedded single-account JetStream-enabled NATS
      server (`testutil_test.go`), which fully exercises the `"platform"`
      path (identical mechanism in test and production) and the
      client-side half of the tenant path (`jsForAccount` correctly
      resolves known vs. unknown tenant names), but not the server-side
      half of BR-AC32's actual cross-account import resolution — that,
      like `CONSUMER.MSG.NEXT`'s multi-reply mechanism risk (BR-AC32's own
      design note), is exercised at Phase 30i's live `docker compose`
      verification, not by this sub-phase's own coverage; documented
      explicitly in `testutil_test.go` rather than left implicit.
      `go test ./...` 33/33 green, `go vet ./...` clean, `docker compose
      build observability-service` and `docker compose config -q` both
      clean.
- [x] **30f — lift service discovery.** (DONE 2026-08-16) `/api/nats/services`
      — the shape of this one changed more than 30d/30e's proxies did. The
      original queried two live *connections* it happened to hold
      (`deps.NC` for PLATFORM, `deps.TenantNC` for whichever tenant session
      was active), explicitly documented as blind to every other tenant.
      This service holds one connection but gets a distinct discovery
      *subject* per account instead — PLATFORM's bare `$SRV.STATS` (via
      `micro.ControlSubject`, confirmed to literally equal `"$SRV.STATS"`
      by reading `micro.APIPrefix`'s value) plus, for every tenant
      `AccountsClient.TenantNames` reports, BR-AC31's remapped
      `monitor.{tenant}.srv.STATS` — so the fan-out moved from "N
      connections, one subject" to "one connection, N+1 subjects," same
      concurrency rationale (`collectStats` always blocks for the full
      `srvDiscoveryWindow`, so N+1 subjects queried sequentially would cost
      N+1 windows). **Net capability gain, not just a port**: this panel
      now sees every known tenant's services at once, where the original
      only ever saw one active tenant. Landed in
      `observability/internal/rest/nats_services.go`; `discoverySubjects`
      and `collectStats` (now subject-parameterized rather than deriving
      the subject internally) are the two functions that actually changed,
      everything downstream of the reply collection (dedup, endpoint
      reshaping, sorting) is an unchanged port.

      Tests: the real-registration specs (stats/endpoint counters, metadata
      pass-through, empty-when-nothing) are lifted verbatim against the
      platform path. The dedup/concurrency spec is rewritten to prove the
      new subject-fan-out mechanism specifically — a hand-rolled responder
      on `monitor.faux.srv.STATS` simulates what a real tenant reply would
      look like, so the test proves this file's own fan-out/dedup logic
      without claiming to prove BR-AC31's actual cross-account remap
      resolves on the wire (that's still Phase 30i, per BR-AC31's design
      note — documented again in this file's own package doc comment
      rather than left implicit). `go test ./...` 40/40 green, `go vet
      ./...` clean, `docker compose build observability-service` and
      `docker compose config -q` both clean.
- [x] **30g — lift the trace store.** (DONE 2026-08-16) `RegisterTraceStore`
      + `trace-request-reply` KV projection — the projection logic itself
      really is unchanged, but two things around it weren't as simple as
      "moved":

      **No `internal/kvstore.Store` port.** Shipping-service's version
      wrapped its own general-purpose KV package (Keys/Watch/Delete/multi-
      context, plus a `natstrace`-header-on-notify path). This projector
      only ever calls two of that package's methods (`Get`/`Put`), and the
      natstrace branch was always a no-op here (the consume callback never
      attaches a span to its context) — so
      `observability/internal/tracestore/tracestore.go` inlines just the
      Get/Put-with-notify slice actually used, rather than porting an
      abstraction most of which would sit unused. New package (30c's
      original scaffold deliberately left `tracestore` uncreated until it
      had real content — this is that content landing).

      **A real permissions tension, resolved without breaking the
      one-connection design.** Provisioning is a `$JS.API` *write*
      (`STREAM.CREATE`/`UPDATE` for `TRACES` and `KV_trace-request-reply`)
      — shipping-service's original solved this by using a second,
      unrestricted connection (`PlatformFullJS`) specifically because its
      narrow `shipping-admin` connection was denied it. Kept this service's
      one-connection design instead: granted `$JS.API.STREAM.CREATE`/
      `UPDATE` scoped to exactly those two resource names (never a
      wildcard), plus publish on `$KV.trace-request-reply.>` (what
      `kv.Put` actually publishes to — confirmed via `nats.go`'s
      `kvSubjectsTmpl` source, not assumed) and
      `notify._platform.kv.trace-request-reply.>`. This is a different risk
      shape than BR-AC31/BR-AC32's grants (which are read-only access into
      *other* accounts) — it's create/update access to two specifically-
      named resources in this account, the same resource-scoped-by-name
      pattern `shipping-admin`'s own REFDATA grants already establish.
      Consumer create/pull for the durable `trace-store-projector` needed
      no new grant — the existing `CONSUMER.CREATE.*.*`/
      `CONSUMER.MSG.NEXT.*.*` wildcards already cover any consumer name,
      durable or ephemeral (NATS wildcards don't distinguish the two).
      `nats/bootstrap-operator.sh`'s `observability` user updated
      accordingly.

      Wired into `observability/composition.go`'s `Startup` (now
      `ctx`-aware, builds its own `jetstream.JetStream`, holds the returned
      `jetstream.ConsumeContext` for a new `Handlers.Stop()` `cmd/main.go`
      defers). Tests are new (no direct prior-art file — shipping-service
      covered this indirectly through its `eventhandler` suite): multi-span
      merge, redelivery dedup, the notify publish, malformed-span drop, and
      `Register` idempotency across two calls, all against a real embedded
      JetStream-enabled NATS server. `go test ./...` green across both
      packages, `go vet ./...` clean, `docker compose build
      observability-service` and `docker compose config -q` both clean.

      Left as an accepted interim state until 30h: shipping-service's own
      `RegisterTraceStore` is still active too (uses `platform.creds`,
      unaffected by this phase's grants), so both services independently
      ensure the same `TRACES`/`KV_trace-request-reply` resources exist at
      startup — harmless since `CreateOrUpdate*` is idempotent, but not a
      final state; 30h removes shipping-service's copy.
- [x] **30h — Admin UI cutover + cleanup.** (DONE 2026-08-16) `api.js` needed
      no change — it already used relative `fetch()` paths, routed entirely
      by proxy config. Repointed the proxy layer instead: `admin/vite.config.js`
      gained three more-specific rules (`/api/nats`, `/api/kv`,
      `/api/jetstream` → `localhost:7205`) ahead of the general `/api` rule
      (unchanged, still shipping-service); `admin/nginx.conf` mirrors this
      with `location /api/nats/` / `/api/kv/` / `/api/jetstream/` blocks
      pointed at `observability-service:8080`, the NATS one carrying the
      same SSE buffering/timeout settings as the general block since
      `/api/nats/log` is still a long-lived stream.

      Deleted from `shipping-service`: the four lifted REST files
      (`nats_ops.go`, `nats_log.go`, `kv.go`, `replay.go`) and their six
      test files, plus `dictionary/internal/eventhandler/trace_store.go`
      and its two test files (13 files total). `listStreams` (never its own
      file — it lived in `handlers.go`) removed alongside its
      `introspectableAccounts` dependency (which was `kv.go`'s, not
      separately named in the original plan text).

      **Deviation — this also removed the entire `PlatformFullJS` second
      connection from shipping-service, not just its callers.** Once the
      diagnostic/trace-store code was gone, nothing else in the process
      used `mono.PlatformFullJS()`/`NatsMonitorURL()`/`NatsLogPath()`
      (confirmed via `grep` before touching anything) — so rather than
      leave a dead, unused connection and three dead `Monolith` interface
      methods, removed all of it: the `platform.creds`-backed second
      connection and its fallback/error-handling in `cmd/main.go`, the
      three methods from `internal/monolith/monolith.go`'s interface (and
      their long doc comments — `PlatformFullJS`'s explained a boundary
      that no longer exists in this service), and the corresponding `Deps`
      fields/wiring in `handlers.go`/`composition.go`. `docker-compose.yml`'s
      `shipping-service` block lost `NATS_MONITOR_URL`/`NATS_LOG_PATH` (now
      dead) and the `nats-logs` volume mount (still used by
      `observability-service`'s own block, untouched). `shipping-admin`'s
      restricted connection (`NC()`/`JS()`) is unaffected — still used for
      `$SRV` discovery *of shipping-service's own instances* via
      `browserrpc.Adapter`'s `micro.AddService`, tenant switching, and
      admin ordered-consumer replay of `SHIPPING`.

      One test-helper casualty: `discardLogger`/`discardWriter` had lived
      in the now-deleted `nats_ops_test.go` but were also used by the
      unrelated `trace_middleware_test.go` (BR-037's HTTP tracing specs,
      Phase 28m) — moved into the package's existing `testutil_test.go`
      rather than left orphaned or duplicated.

      Regenerated `docs/docs.go`/`swagger.json`/`swagger.yaml` via
      `swag init -g cmd/main.go -o docs` (swag was already installed) —
      1769 lines net deleted across the three generated files, confirmed
      none of the removed routes remain. `go build ./...`, `go vet ./...`,
      and `ginkgo ./...` (9 suites) all green in `shipping-service`;
      `ginkgo ./...` (3 suites, 117 specs) green in `accounts-service`;
      `go build`/`go vet`/`go test ./...` (2 packages) green in
      `observability-service` — the full three-service combined pass the
      plan's closing checklist calls for. `docker compose config -q` and
      `docker compose build shipping-service` both clean.

      Updated `BUSINESS_RULES-SHIPPING.md`: BR-028's "Enforced in"/"Test"
      lines repointed to `observability-service`, plus a Phase 30h
      amendment paragraph — the label-resolution *mechanism* changed, not
      just its file (`tenantLabelsByAccount`'s `LocalAddr`-matching, which
      only worked because shipping-service held one connection per tenant,
      replaced by `AccountsClient.Labels`' direct `GET /api/accounts` call,
      Phase 30d, since `observability-service` holds only the one PLATFORM
      connection). BR-034 (Account Activity) got the same file/mechanism
      update. The trace-store rule (Phase 28f/28g/28l's amendments) got its
      own Phase 30h amendment paragraph plus new "Enforced in"/"Test"
      lines, covering the `PlatformFullJS` → narrowly-scoped-grant
      permissions change and the deliberate non-port of `internal/kvstore.Store`.
      `BUSINESS_RULES.md`'s index line for BR-034 repointed off the deleted
      `nats_ops.go` path. No new BR numbers were needed — every 30h doc
      change is an amendment to an existing rule, not a new one.

      Deliberately deferred to a spawned follow-up task, not done as part
      of 30h: `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-ADMIN.md`
      (630 lines) still describes the pre-30h shipping-service-only backend
      for this exact Admin UI navbar group in detail (§ 3.1–3.4's shared
      backend patterns, § 4.1–4.7's per-panel sections, § 4.5's trace
      example) — a genuine content correction this phase's scope didn't
      originally call out, and large enough to warrant its own pass rather
      than folding into an already-large 30h diff.
- [x] **30i — Live verification.** (DONE 2026-08-16) `docker compose down -v
      && up --build`, repeated several times across the fix cycle below.
      Every panel in the NATS/SYSTEM nav group (Connections, Services,
      Account Activity, Streams, KV Buckets, Log, Request/Reply Traces)
      confirmed rendering correctly against `observability-service`
      (screenshots taken of each); a tenant (`initech`) created through the
      Admin UI got working Connections/Services visibility immediately, with
      zero restart of any service — BR-030's reactive-provisioning bar,
      confirmed live for the first time against the full Phase 30 split.
      This step existed specifically to prove or disprove the mechanism
      risks BR-AC31/BR-AC32 had flagged as unproven-by-unit-tests since
      30a/30b — it found six real, previously-undetected bugs, none of them
      the flagged multi-reply mechanism itself (which turned out to work
      correctly once everything else was fixed):

      1. **`observability.creds` was never actually generated.** The day-0
         `bootstrap-operator.sh` gained its `observability` user step in
         30c, but this machine's `nats/operator.jwt` already existed from
         before that, and the script exits early ("already exists — skip")
         unless `--force` is passed — so the step had literally never run.
         Docker's bind mount then silently created an empty *directory* at
         the missing host path on first `docker compose up`, and
         `observability-service` looped forever inside `waitForNATS` with
         zero log output (that inner loop logs nothing on failure) trying
         to open it as a file. Fixed by running `--force` — which, being a
         full operator/account/JWT regen, was also the only way for the
         day-0 ACME/GLOBEX tenants to actually pick up every export/import
         this phase had added to `bootstrap-operator.sh` since (BR-AC31,
         BR-AC32, the trace-store grants) — those were equally unproven
         until this same regen.
      2. **`$JS.API.INFO` missing from the `observability` user's grant.**
         `jetstream.CreateOrUpdateKeyValue` (trace-store bucket
         provisioning) calls `AccountInfo()` first; without this the very
         first startup call failed closed.
      3. **Filtered `CONSUMER.CREATE` needs a different, wider subject than
         the unfiltered form.** nats.go embeds a set `FilterSubject`
         directly into the *published* `$JS.API` subject
         (`apiConsumerCreateWithFilterSubjectT`) rather than the request
         body — both the trace-store's durable consumer (filtered to
         `obs.trace.>`) and, more consequentially, the KV Buckets panel's
         `WatchAll` (filtered to `$KV.<bucket>.>`, for *any* bucket in *any*
         account) hit this. Fixed with a new `$JS.API.CONSUMER.CREATE.*.*.>`
         subject — added as BR-AC32's seventh subject (not
         PLATFORM-native-only, since the KV panel needs it for tenant
         buckets too), superseding what was initially a narrower
         TRACES-only fix.
      4. **`$JS.API.DIRECT.GET` missing for the trace store's own KV
         reads.** `appendSpan`'s `kv.Get` uses nats.go's direct-get
         optimization (`apiDirectMsgGetLastBySubjectT`), a different
         subject family than `DIRECT.GET`'s no-filter form, same
         literal-subject-folding shape as #3. PLATFORM-native, scoped to
         `KV_trace-request-reply` only (this service's own resource, not a
         BR-AC32 cross-account concern).
      5. **`$JS.ACK` missing entirely.** Every delivered JetStream message's
         Ack publishes to a *server-generated* reply subject
         (`$JS.ACK.TRACES.trace-store-projector.<numDelivered>...`), not
         one the client constructs — without this grant, Acks silently
         failed and JetStream redelivered the same messages forever.
         PLATFORM-native, scoped to `TRACES`/`trace-store-projector`.
      6. **`AccountsClient.TenantNames` excluded `"PLATFORM"` and assumed
         `"SYS"` needed no exclusion — both wrong against the real,
         running `accounts-service`.** It stores and returns these with
         lowercase names (`"platform"`, `"sys"` —
         `accounts/handler.go`'s `h.Store.Get(ctx, "platform")`), and SYS
         *does* get a Postgres row despite the original comment assuming
         otherwise. The case-sensitive `==` check let both slip through
         into `introspectableAccounts`, which then built a bogus
         `monitor.platform.js`/`monitor.sys.js`-prefixed JetStream context
         for each — no matching cross-account import exists for either, so
         the very next `$JS.API` call through it failed closed with
         "no responders," aborting `listKVBuckets`/`listStreams`'s entire
         response (discarding every real tenant's already-successful
         results) the moment either bogus entry was reached. This is the
         bug a long, initially-wrong investigation chased — a first false
         lead blamed bug #5's redelivery storm for client-side
         slow-consumer pressure on unrelated `$JS.API` calls; isolating the
         actual trigger required a from-scratch Go repro that couldn't
         reproduce it at all until it exactly mirrored `introspectableAccounts`'
         real call order, at which point per-account debug logging pinned
         the two bogus "platform"/"sys" entries directly. Fixed by matching
         `accounts-service`'s own `reservedAccountNames`
         `strings.ToUpper()` comparison exactly, with a new
         `TestTenantNamesExcludesPlatformAndSysCaseInsensitively` pinning
         the real (lowercase) shape.
      7. **`shipping-service`'s tenant-discovery treated
         `observability.creds` as a switchable tenant.** `nonTenantCredsFiles`
         (`dictionary/internal/rest/tenant.go`) was never updated when
         `observability.creds` first landed in the same shared `nats/creds/`
         directory back in 30c — every `shipping-service` startup from that
         point tried (and failed) to provision `SHIPPING`-stream resources
         for a nonexistent "observability" tenant. Fixed by adding it to
         the exclusion set; `TestDiscoverTenantsExcludesReservedNamesCaseInsensitively`
         extended to cover `shipping-admin`/`observability` alongside the
         existing platform/sys cases.

      All fixes verified together in one final clean
      `docker compose down -v && up --build`: zero permission-violation log
      lines anywhere in the stack, all five `observability-service` REST
      endpoints 200, every Admin UI NATS/SYSTEM panel screenshotted
      working (Connections showing resolved account labels including a
      freshly-created tenant; Services showing live `$SRV.>` fan-out counts
      increase for it; KV Buckets' live-entries view rendering real
      contents; Streams showing cross-account `SHIPPING`/`REFDATA`/`TRACES`;
      Request/Reply showing 25 live traces). `go build`/`go vet`/`ginkgo`
      (or `go test`) green across all three touched services (shipping-service
      9 suites, accounts-service 3 suites/86+19+12 specs,
      observability-service 2 packages) as a final combined pass, not just
      per-phase. `docker compose config -q` clean.

      `BUSINESS_RULES-ACCOUNTS.md`'s BR-AC32 and `BUSINESS_RULES-SHIPPING.md`'s
      trace-store rule (Phase 30h's own amendment) both updated with Phase
      30i amendments documenting the new seventh subject and its rationale;
      `nats/bootstrap-operator.sh` carries matching inline comments at each
      fix point, including one explicit correction of its own earlier
      (wrong) diagnosis for bug #6.
- [x] `BUSINESS_RULES-ACCOUNTS.md` gets the new BR-AC rule(s) for the two
      export/import grants (BR-AC31/BR-AC32, done incrementally in 30a/30b);
      `BUSINESS_RULES.md` index bump (done same phases).
- [x] `ginkgo ./...` green in `accounts-service` (3 suites) and the new
      `observability-service` (`go test ./...`, 2 packages — no Ginkgo suite
      in this service, matching how shipping-service's own `rest` package
      tests); `ginkgo ./...` green in `shipping-service` (9 suites) after the
      30h deletions — all three re-verified together as part of 30h's
      closing pass (2026-08-16).

### Phase 100 (PROPOSED — awaiting approval) — Ship Container Capacity Limit

#### Goal

Ships currently have no maximum container capacity — a ship can be loaded with an unbounded number of containers. Add a fixed `Capacity` to the Ship aggregate and enforce it as a load-time domain rule (BR-019), plus surface a load-capacity indicator column in `frontend-port` ("SeaFreight Flow") so the constraint is visible, not just enforced.

#### Design

- **`Ship` domain model** (`dictionary/internal/domain/ship.go`): add `Capacity int` to `ShipState` (ship.go:46-53) and `ShipAggregate` (ship.go:65-70), threaded through `Apply()`/`State()`/`FromState()`.
- **Setting capacity**: no "register ship" command exists — a ship's first `Arrive` is its registration (`ShipAggregate.Arrive()`, ship.go:124-144), which already set-once's `ShipName` when empty. `Capacity` follows the same set-once-at-first-arrival pattern: `ArrivePort` request gains an optional `capacity` field; if omitted on first arrival, a documented default is used (exact default — e.g. 20 — confirmed at implementation time, not fixed by this plan entry). There is still no update-ship command, so capacity is immutable after first arrival unless a follow-up phase adds one.
- **Enforcing BR-019 on `Load`**: `ContainerAggregate.Load()` (container.go:196-219) gains a capacity check alongside its existing BR-012/BR-010/BR-014/BR-008 checks. This needs the ship's *current* on-ship container count at command time — `ContainerHandler.LoadContainer()` (application/commands/container.go:87-106) resolves this before calling `cont.Load(...)`. Two candidate mechanisms, to be decided during implementation:
  1. Event-replay count (consistent with "JetStream is the source of truth" — Working Assumptions): count `.loaded`-without-subsequent-`.unloaded` container events for the ship's `shipID` at hydrate time.
  2. Read-model query against the existing manifest join (Shape A/B projection) — faster, but reads an eventually-consistent projection to guard a write (same class of trade-off Phase 103 documents for BR-008/BR-012 read-model guards).
- **Read model / API surface**: `ShipState`'s KV (Shape A/B) and Postgres projections need the new `Capacity` field so `GET` endpoints (fleet, shape-b ship, shape-c fleet) return it to the frontend.
- **Frontend (`frontend-port`)**: `FleetPanel.vue` (columns at lines 112-131) and `ShipsAtPortPanel.vue` (columns at lines 150-163) each gain a load-capacity indicator column pairing the new `capacity` field with the container count already computed via `store.manifestFor(shipID).length` (e.g. `12 / 50`, colored by fullness). Route any new column label through `l10n` (BR-D16), not a hardcoded literal.

#### Checklist

- [ ] Confirm default capacity value and whether `capacity` is required or optional on `ArrivePort`
- [ ] Decide event-replay vs read-model-guard mechanism for the current-count check (document the trade-off, mirroring Phase 103's treatment of BR-008/BR-012)
- [ ] `ShipState`/`ShipAggregate`: add `Capacity`, thread through `Apply()`/`State()`/`FromState()`
- [ ] `ArrivePort` command + REST handler: accept optional `capacity`, set-once on first arrival
- [ ] `ContainerAggregate.Load()`: new `ErrCapacityExceeded` check (BR-019)
- [ ] `ContainerHandler.LoadContainer()`: resolve current on-ship count before calling `Load()`
- [ ] KV (Shape A/B) + Postgres ship projections: persist and return `Capacity`
- [ ] Ginkgo specs written **before** implementation (red → green): `Container Domain Rules / BR-019` — load rejected at capacity, allowed under capacity, allowed exactly at capacity-minus-one
- [ ] `frontend-port`: load-capacity column in `FleetPanel.vue` and `ShipsAtPortPanel.vue`, via `l10n`
- [ ] `BUSINESS_RULES.md`: BR-019 updated from PROPOSED to enforced, with final error/enforcement/test references
- [ ] `go build ./...` + `ginkgo ./...` green; frontend build green


### Phase 101 — Write-Side Safety (Optimistic Concurrency + Publish Dedup)

#### Goal

Close the two producer-side correctness gaps that stand between "JetStream as event log" and "JetStream as trustworthy event store":

1. **Blind publish → lost invariants under concurrency.** Command handlers hydrate-validate-publish with no guard between read and write. Two concurrent commands on the same aggregate both hydrate the same pre-state, both pass validation, both publish — producing events that are individually valid but jointly violate a business rule (e.g. the same container loaded onto two ships).
2. **No publish dedup → client retries double-write the source of truth.** An HTTP client retrying a command after a timed-out response durably appends the business event twice. In transport-mode this would be caught downstream by Postgres constraints; in event-store mode the duplicate *is* the record.

#### Design

- **Optimistic concurrency**: `hydrate()` already walks the aggregate's events — it additionally returns the last stream sequence seen. Publish carries `Nats-Expected-Last-Subject-Sequence`; if another event landed in between, the server rejects the append (err 10071), and the handler re-hydrates, re-validates, and retries (bounded).
  - ⚠️ **Verify against current NATS docs before implementing**: an aggregate's events span multiple subjects (`…{id}.arrived` vs `…{id}.departed`), and the plain header checks the last sequence *of the published subject only*. Newer servers support `Nats-Expected-Last-Subject-Sequence-Subject` to guard against a wildcard filter (`…{id}.>`). Confirm server + nats.go client support; if unavailable, fall back to a single per-aggregate subject with the event type in the payload/headers, and document the trade-off.
- **Publish dedup**: every publish sets `Nats-Msg-Id` derived from a command idempotency key (client-supplied header, generated by the frontend per user action). Configure the stream's `Duplicates` window **explicitly** (don't rely on the 2-minute default silently).
- The `Publisher` port grows an options parameter (expected sequence, message ID) — kept transport-agnostic in signature so the interface doesn't leak `jetstream` types into `application/`.

#### Checklist

- [ ] Verify `Nats-Expected-Last-Subject-Sequence[-Subject]` semantics and `Duplicates` window behavior against current NATS server / nats.go docs (features move between releases)
- [ ] `hydrate()` / `hydratePair()` return the last relevant stream sequence
- [ ] `Publisher` port + `jstream` adapter: publish options (expected last sequence, msg ID)
- [ ] Command handlers: guard publishes, bounded retry-on-conflict (re-hydrate → re-validate → re-publish)
- [ ] `Nats-Msg-Id` on every publish; explicit stream `Duplicates` window in `CreateStream`
- [ ] REST: accept/generate a command idempotency key per request
- [ ] Ginkgo specs: concurrent conflicting commands — exactly one wins, loser re-validates (double-load race rejected); duplicate publish with same msg ID appends once
- [ ] `BUSINESS_RULES.md`: document the concurrency guarantee the event store now provides
- [ ] `go build ./...` + `ginkgo ./...` green


### Phase 102 — Projection Hardening (Consumer-Side Idempotency + Explicit Limits)

#### Goal

Make projections safe under redelivery and reordering **by engineering, not by accident**. Today's safety rests on "redelivering the same event re-applies the same upsert" — true only if delivery order is preserved, which depends on unexamined consumer defaults. Also make the stream's "never discard" property an explicit decision rather than an implicit absence of limits.

#### Design

- **KV writes**: replace naive `Put` with a guarded write — the stored value carries the source event's stream sequence; the projector skips any event older than what's stored, using `Update` with expected revision (CAS loop) so a stale redelivery can never clobber newer state.
- **Postgres projection**: same guard — persist the last-applied stream sequence per row and skip older events in the upsert (`WHERE excluded.seq > current.seq` style).
- **Consumer ordering**: verify `Consume()` callback concurrency and `MaxAckPending` defaults against current nats.go docs (do not assume); set `MaxAckPending` explicitly per projector and document the ordering guarantee relied upon.
- **Explicit retention decision**: `CreateStream` currently sets no `MaxAge`/`MaxMsgs`/`MaxBytes` — "never discard" is true only implicitly. Make it explicit: document unbounded-is-deliberate in the config (or set `DiscardPolicy` intentionally), so the config can't be copied forward with the decision invisible.
- **Poison messages**: current behavior (ack-on-unmarshal-failure to avoid redelivery loops) is documented; consider a dead-letter subject instead of silently acking — shaped per the § Phase 16 taxonomy (a fixed literal family token first, `{context}` for company/business unit; **not** `{region}`/`{tenant}`, neither of which belongs in a subject).

#### Checklist

- [ ] Verify `Consume()` ordering / `MaxAckPending` semantics against current nats.go docs
- [ ] `kvstore.Store`: guarded write API (sequence-aware CAS); all projector call sites migrated off naive `Put`
- [ ] Postgres projectors: last-applied-sequence guard in upserts
- [ ] Explicit `MaxAckPending` on all projector consumers
- [ ] `CreateStream`: retention/discard decision made explicit in code comment + config
- [ ] Poison-message policy: dead-letter subject or documented ack-and-log, decided and implemented
- [ ] Ginkgo specs: out-of-order redelivery does not clobber newer KV/Postgres state; duplicate redelivery is a no-op
- [ ] `go build ./...` + `ginkgo ./...` green

### Phase 103 — Stream Split + Cross-Aggregate Consistency

#### Goal

Extract container events from the shared `SHIPPING` stream into a dedicated `TERMINAL` stream, turning the two aggregates into two independent bounded contexts. This is a **single-variable change** on top of Phases 8–14: the aggregates, rules, and frontends are unchanged — only the stream topology moves. Post-Phase 9 this is even cleaner than originally planned: **the subjects themselves do not change** — a subject can belong to only one stream, so the split is purely moving the `…container.>` binding from `SHIPPING` to `TERMINAL`. The purpose is to make the **invariant-spanning-two-aggregates problem** concrete and demonstrate the solution options.

#### The problem this phase exposes

After the split, BR-008 (container destPort vs ship's current port) and BR-012 (ship must be docked) still need **both** aggregates' state — but the container command handler can no longer get the ship's state from the same replay. `ContainerAggregate` hydrates from `TERMINAL`; the ship's docked state lives in `SHIPPING`. There is no atomic cross-stream replay.

| Stream | Subject binding | Bounded context |
|---|---|---|
| `SHIPPING` | `evt.{context}.shipping.ship.>` | Ship movements |
| `TERMINAL` | `evt.{context}.shipping.container.>` | Container lifecycle |

#### Solution options to implement and document

The demo implements **option 1** as the default and documents the trade-offs of all three:

1. **Read-model guard (default)** — the container handler reads the ship's KV projection (Shape A/B) to check docked state / current port. Fast and keeps the streams independent, but validates a write against an eventually-consistent read (stale-read window — which Phase 103 measures under load).
2. **Hydrate both streams** — the container handler additionally replays `SHIPPING` for the ship. Strongly consistent, but the container context is no longer independent and every load/unload replays two streams.
3. **Saga / compensating event** — accept the write optimistically and emit a compensating `container.load-rejected` event if the ship turns out not to be docked. The "correct" DDD answer for separate contexts; heaviest to implement.

#### Checklist

- [ ] `internal/jstream/stream.go` — add the `TERMINAL` stream binding `evt.{context}.shipping.container.>`; `SHIPPING` keeps only `…ship.>` (subjects themselves unchanged post-Phase 12.8)
- [ ] `domain/events.go` — route container subject builders / stream-name references to `TERMINAL`
- [ ] `application/commands/container.go` — hydrate containers from `TERMINAL`; replace the in-replay ship check with the **read-model guard** (option 1) for BR-008 / BR-012
- [ ] `eventhandler/` — container projector consumes from `TERMINAL`; ship projector unchanged on `SHIPPING`
- [ ] Ginkgo specs — BR-008 / BR-012 still green via the read-model guard; add a spec documenting the stale-read window (guard sees pre-departure state)
- [ ] Frontend (`frontend/`): JetStream panel stream selector — add `TERMINAL` entry (`streamOptions`); backend `streamJetStream` switch — add `TERMINAL` case
- [ ] Frontend (`frontend/seafreight-app/`): extend the existing `notify.*` NATS WebSocket subscriptions to cover the new `TERMINAL` stream's container events (this app no longer uses SSE — Phase 15d; the directory was also renamed from `frontend-port/`)
- [ ] `ARCHITECTURE.md` — document the two-stream topology, the cross-aggregate invariant problem, and the three solution options with the chosen default
- [ ] `go build ./...` + `ginkgo ./...` green


### Phase 104 — Performance & Load Testing (full suite)

#### Goal

Validate that the *final* architecture holds under realistic throughput and identify the bottlenecks before any production consideration, building on the baseline established in **Phase 10**. Runs after the write path (Phase 101) and stream split (Phase 103) are in place, so the scenarios those phases gate can finally be measured. The POC has two known scalability gaps — first characterised in Phase 10, re-measured here against the final architecture:

1. **Shape C — full replay on every call.** `ReconstructFleet` replays from `seq=1` every time. Latency grows linearly with stream depth.
2. **Write-side hydration — full replay per command.** `hydrate()` in `commands.go` replays all events for a ship on every command. A busy ship accumulates history and slows its own writes.

Both are correct implementations of event sourcing fundamentals — the point is to *measure* the degradation curve and document where snapshots or other mitigations become necessary.

> The baseline harness and the Shape C / single-ship / throughput scenarios are delivered in **Phase 10** (pull-forward baseline). This phase reuses that harness, adds the scenarios gated by Phases 14 and 16, and re-measures the Phase 10 baselines against the final architecture.

#### Tool

**k6** (`k6.io`) — scripted load testing in JavaScript, runs outside the Go stack, produces latency percentiles and throughput metrics. Alternatively `vegeta` for simpler HTTP load.

#### Test scenarios

| Scenario | What it measures | Status |
|---|---|---|
| High-frequency arrivals/departures — single ship | Write-side hydration degradation as event count grows | baseline in Phase 10; re-measure |
| High-frequency arrivals/departures — many ships concurrently | Throughput ceiling of the command pipeline | baseline in Phase 10; re-measure |
| Shape C fleet reconstruction under load | Replay latency vs stream depth; degradation curve | baseline in Phase 10; re-measure |
| KV watch fan-out — many SSE clients | How many concurrent SSE connections the backend sustains before lag | this phase |
| Container load/unload burst — terminal throughput | Cross-stream (`SHIPPING` + `TERMINAL`) consumer lag under write pressure | needs Phase 103 |
| Projection lag — event published → KV updated | End-to-end latency of the Shape A/B projectors under load | this phase |
| Optimistic-concurrency contention — concurrent commands, same aggregate | Retry rate and latency cost of the Phase 101 sequence guard under contention | needs Phase 101 |

#### Baseline metrics to capture

- p50 / p95 / p99 command latency (arrive, depart, load container, unload container)
- Shape C reconstruction time at 100 / 1k / 10k events in stream
- KV watch SSE lag (time from KV write to browser event) at 1 / 10 / 100 concurrent clients
- Max sustained commands/sec before errors or queue buildup

#### Expected findings to investigate

- Shape C becomes unusable beyond a few thousand events without snapshotting
- `hydrate()` degrades for ships with long histories — snapshot checkpoint needed
- SSE fan-out has a practical client ceiling determined by goroutine count and NATS consumer throughput

#### Checklist

The baseline harness, seed script, and the Shape C / single-ship / throughput scenarios are delivered in **Phase 10**. This phase completes the remaining (gated) scenarios and finalises the report:

- [ ] Scenario: optimistic-concurrency contention — retry rate and latency cost of the Phase 101 sequence guard *(needs Phase 101)*
- [ ] Scenario: cross-stream burst — fire `SHIPPING` and `TERMINAL` events concurrently, measure projection consumer lag *(needs Phase 103)*
- [ ] Scenario: SSE fan-out — open 1 / 10 / 50 / 100 concurrent SSE clients, measure KV watch lag
- [ ] Scenario: projection lag — event published → KV updated, measured under load
- [ ] Re-measure the Phase 10 baseline scenarios against the final architecture (with guard + split) and record the before/after delta
- [ ] Finalise `demos/01-dictionary/PERFORMANCE.md` — full baseline numbers, degradation curves, identified thresholds
- [ ] Document architectural mitigations for each bottleneck (snapshot strategy, consumer parallelism, SSE load balancing)


### Phase 105 (optional, PLACEHOLDER — not yet a formal requirement) — Per-Tenant Runtime Theme Spike

#### Goal

Explore whether UI theme/branding (colors, tokens, light/dark presets) can be externalized per tenant and swapped **at runtime**, without a separate build/deploy per tenant. Raised as a "does it make sense to put theme data in the dictionary service" question (2026-07-17) — not a formal requirement yet, so this is scoped as a spike to prove the mechanism out, not a commitment to build it.

#### Why this isn't just another `l10n`-style refdata type

Theme data is fetch-then-apply's worst case: `l10n`/label fallback (BR-D11) and cold-paint caching (BR-D19) tolerate a brief English-text mismatch on first paint, but a full-page flash of the *wrong tenant's brand colors* before a client-side fetch resolves is far more visible and jarring — the same class of problem, magnified. Client-side fetch-and-apply (the pattern used everywhere else in this repo) is therefore the wrong default here.

#### Scope (spike, not production-ready)

- Dictionary service remains the source of truth for each tenant's theme tokens (a new `theme` dictionary type, context-scoped like everything else), but resolution is **not** a browser-side fetch-after-mount.
- Prove out server-side/edge injection instead: a lightweight step (nginx, a tiny Go handler, or an SSR shell) resolves the tenant (subdomain/host header/path) and injects that tenant's CSS custom properties into `index.html` **before** it reaches the browser, so first paint is already correct — no flash, no fallback banner needed.
- Note but don't implement: full SSR, a CDN/edge-cache layer for resolved theme HTML, and live theme-change propagation to already-open tabs (out of scope for a spike).

#### Checklist

- [ ] Confirm this is still wanted as a real requirement before scoping further (currently a placeholder)
- [ ] `theme` dictionary type: define token schema (a small fixed set of CSS custom properties, not an open-ended style system)
- [ ] Spike: a request-time injection step (nginx `sub_filter`, or a minimal Go handler in front of the static build) that resolves tenant → theme tokens → injects into the served `index.html`
- [ ] Verify no flash-of-wrong-theme on first load for a tenant the browser has never seen (the actual test this spike exists to pass)
- [ ] Document the trade-off vs. compiled-in-at-build-time in `ARCHITECTURE.md`: when per-tenant runtime branding is worth the added deploy-topology complexity vs. just rebuilding per tenant

### Phase 106 (DEFERRED from Phase 22b, 2026-08-13) — Context Inheritance on the Live Read Path

#### Goal

Make live reference-data reads honour the context `parent` chain, closing the gap between what the context hierarchy *implies* and what it actually does.

#### The gap

refdata-service has two parallel read paths, and only one inherits:

- **Corpus / versioned path — inherits.** `CorpusRepository.CreateDraft` walks the ancestor chain with a recursive CTE and flattens each ancestor's locally-authored rows via `domain.FlattenCorpus` / `FlattenLocalizations`, writing resolved rows with `source_context` + `is_override`. `inheritance.go`'s header states the intent plainly: the flattened form exists so reads never traverse a chain.
- **Live path — does not.** Every query in `item_repository.go`, `localization_repository.go`, `locale_repository.go` and `reference_repository.go` is a flat `WHERE context = $1`. No CTE, no UNION, no IN-list. `kvcache.Projector.rebuildEntry` builds the `refdata-{context}` bucket from those same exact-match queries, so the live KV cache doesn't inherit either. `Ancestors()` exists but is consumed only by the admin detail endpoint and by `Register`'s cycle check.

The consequence is that a context registered with a parent looks correct in the admin UI's context tree and returns nothing through `rpc.{context}.refdata.item.get.v1`, `type.list.v1`, the REST list/get routes, or the `refdata-{context}` bucket — while `item.get-versioned.v1` returns the fully inherited set. Phase 22b makes this more visible by giving every tenant a parented default context, but does not introduce it.

#### Scope

- [ ] Decide the mechanism: recursive-CTE resolution in the repositories (correct, touches every read query) vs. materialising inherited rows into the child context on write (simpler, duplicates data, drifts when an ancestor changes)
- [ ] `dictionary_locales` is on the flat path and is **not** covered by corpus flattening — whichever mechanism is chosen must cover locales, or `EffectiveDefaultLocale` still resolves against an empty set
- [ ] `kvcache.Projector.rebuildEntry` must project inherited entries into `refdata-{context}`, or readers must fall back to an ancestor's bucket — pick one; a KV cache that disagrees with Postgres is worse than no inheritance
- [ ] Override semantics on the live path must match the corpus path's `is_override` precedence (child wins, nearest ancestor next) so the two paths cannot disagree about the same item
- [ ] Decide whether `Ancestors()` and `ancestorChainTx` (currently duplicated between `context_repository.go` and `corpus_repository.go`) collapse into one implementation as part of this
- [ ] Business rules for live-path inheritance and override precedence; specs covering a child with no local rows, a child overriding one ancestor row, and a three-level chain

---

### Verification status (2026-07-09)

The full compose stack now runs end to end (Docker installed 2026-07-09):
all five services build and start (`nats`, `postgres`, `backend`, `frontend`,
`frontend-port`), Swagger UI serves at `:18080/swagger/`, both frontends serve
with working nginx `/api` proxies, and a live smoke test exercised the full
container lifecycle against the real stack — register → load → BR-012
rejection at sea → unload at destination — with the `meta.known-ports`
projection, terminal yard query, and Shape C fleet+container reconstruction
all returning correct results. `go build` / `go vet` / `ginkgo ./...`
(22/22 specs) and both frontend builds remain green.

---

## Renumbering (done at proposal, updated for Phase 12 insertion)

| Was | Now |
|---|---|
| *(new)* | **Phase 12 — Refdata Versioning, Tenancy & Template Inheritance** |
| Phase 12 (PROPOSED) — Ship Container Capacity Limit | Phase 13 |
| Phase 13 — Write-Side Safety | Phase 14 |
| Phase 14 — Projection Hardening | Phase 15 |
| Phase 15 — Stream Split | Phase 16 |
| Phase 16 — Performance & Load Testing | Phase 17 |
| Phase 17 (optional) — NATS Accounts Spike | Phase 18 (optional) |
| Phase 18 (optional) — Theme Spike | Phase 19 (optional) |

Cross-reference sweep (same commit):

- [x] Main plan internal references (Phase 9 "why this precedes Phase 13"→14, Phase 10's
      Phase 11/14/15 mentions→11/15/16, Phase 13–17 mutual references→14–18)
- [x] `demos/01-dictionary/PERFORMANCE.md` (and the `obsidian/POC-Dictionaries/` copy) — deferred-scenario phase labels
- [x] `demos/01-dictionary/perf/README.md` — deferred-scenario phase labels
- [x] `ARCHITECTURE.md`, `BUSINESS_RULES.md` that cite Phases 13–17
- [x] Go source comments (`events.go`, `container.go`, `commands/container.go`) — Phase 14→16
- [x] `.claude/memory/` notes citing phase numbers — `tenant_service_separation_decision.md` cites Phase 18, which kept its number through this renumbering; no correction needed

---

## Renumbering (2026-07-28 — Phase 18/20 swap)

**Why:** Phase 18 (NATS Accounts Tenancy Spike) completed and Phase 20 (Accounts Service &
Decentralized JWT Tenancy — previously tracked only in a separate approved plan file, not yet
in this document) both needed to sit immediately after Phase 12, ahead of the not-yet-built
Phases 13–19. Renumbering makes room: 15–19 are now free for phases to be inserted between the
new Phase 14 (accounts service) and the resumed sequence at Phase 20, without another cascade.

| Was | Now |
|---|---|
| Phase 18 (18a/18b) — NATS Accounts Tenancy Spike | **Phase 13** (13a/13b) |
| Phase 20 — Accounts Service & Decentralized JWT Tenancy (was only in `rippling-jumping-peacock.md`) | **Phase 14** (14a/14b/14c) — added to this document for the first time |
| Phase 13 — Ship Container Capacity Limit | Phase 20 |
| Phase 14 — Write-Side Safety | Phase 21 |
| Phase 15 — Projection Hardening | Phase 22 |
| Phase 16 — Stream Split | Phase 23 |
| Phase 17 — Performance & Load Testing | Phase 24 |
| Phase 19 — Theme Spike | Phase 25 |

Cross-reference sweep (same commit):

- [x] Main plan internal references (Phase 12's "scope (Phase 18)"→13, Phase 20's
      "Phase 16"→23, Phase 21's "Phase 14/16"→21/23, Phase 24's "Phase 14/16"→21/23)
- [x] `demos/01-dictionary/BUSINESS_RULES-SHIPPING.md` — Phase 16→23, Phase 13→20
- [x] `demos/01-dictionary/PERFORMANCE.md` (and the `obsidian/POC-Dictionaries/` copy) and
      `demos/01-dictionary/perf/README.md` — Phase 14→21, Phase 16→23, Phase 17→24
- [x] `ARCHITECTURE.md` — Phase 15→22, Phase 16→23, Phase 17→24, Phase 18/18b→13/13b
- [x] `System Design - V3 Logistics Platform.md` — Phase 18→13
- [x] `nats/nats.conf`, all Go/Vue/JS source comments citing Phase 18a/18b/16 — 13a/13b/23
- [x] `.claude/memory/` notes — `tenant_service_separation_decision.md`, `accounts_service_plan.md`,
      `MEMORY.md` updated to Phase 13/14
- [x] `AppShell-Extraction-Plan.md`'s "main plan's Phase 19 placeholder" → Phase 25
- [x] Historical/archived docs left untouched on purpose: `Dictionary-POC-Plan-ARCHIVE.md`
      (renamed `Main-POC-Plan-ARCHIVE.md` 2026-08-03 when it absorbed Phases 15–19),
      `Dictionary-Service-Plan.md`, `.ai-archive/*` document *past* renumbering events and are
      frozen snapshots, not live cross-references

---

## Renumbering (2026-08-03 — Phase 26/27 → 20/21, candidate Phases 20–25 → 100–105)

**Why:** Phase 26 (JetStream Account Limits, 20a done/20b in progress) and Phase 27 (Account
Exports/Imports, plan approved) are the two phases actually being worked now, but sat after five
not-yet-started "candidate" phases (old 20–25: Ship Container Capacity Limit, Write-Side Safety,
Projection Hardening, Stream Split, Performance & Load Testing, Theme Spike) purely because those
were proposed earlier and never renumbered down as the active accounts-architecture work grew its
own phases (16–19, then 26–27) ahead of them. Renumbering makes the live-work phases read as 20/21
again, and reserves 22–99 as open space to insert new or promoted-candidate phases without another
cascade — the old 20–25 candidates move to 100–105, out of the way but still each individually
addressable and inspectable, exactly as they were before.

| Was | Now |
|---|---|
| Phase 26 (26a/26b) — JetStream Account Limits | **Phase 20** (20a/20b) |
| Phase 27 — Account Exports/Imports | **Phase 21** |
| Phase 20 (PROPOSED) — Ship Container Capacity Limit | Phase 100 |
| Phase 21 — Write-Side Safety | Phase 101 |
| Phase 22 — Projection Hardening | Phase 102 |
| Phase 23 — Stream Split + Cross-Aggregate Consistency | Phase 103 |
| Phase 24 — Performance & Load Testing | Phase 104 |
| Phase 25 (optional PLACEHOLDER) — Theme Spike | Phase 105 |

Cross-reference sweep (same commit):

- [x] Main plan internal references — old Phase 20's "Phase 23"→103; old Phase 24's "Phase 21"→101,
      "Phase 23"→103; all `needs Phase N` / `(Phase N)` mentions inside the moved blocks updated in
      the same pass as their host block
- [x] Sections physically reordered in this document (not just renumbered in place) so phase numbers
      still read ascending top-to-bottom: 20, 21, then 100–105
- [x] `demos/01-dictionary/BUSINESS_RULES-SHIPPING.md` — "Phase 20" (capacity mechanism note) → 100,
      "Phase 23" (stream-split note) → 103
- [x] `demos/01-dictionary/PERFORMANCE.md` (and the `obsidian/POC-Dictionaries/` copy) and
      `demos/01-dictionary/perf/README.md` — Phase 21→101, Phase 23→103, Phase 24→104
- [x] `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md` — Phase 22→102, Phase 23→103,
      Phase 24→104
- [x] Go source comments (`dictionary/internal/domain/{events,container}.go`,
      `dictionary/internal/application/commands/container.go`) — Phase 23→103
- [x] `.claude/plans/AppShell-Extraction-Plan.md`'s "main plan's Phase 25 placeholder" → Phase 105
- [x] `.claude/memory/` — `phase27_account_exports_imports.md` renamed
      `phase21_account_exports_imports.md` (frontmatter `name` updated too); `MEMORY.md` and
      `accounts_service_plan.md`'s cross-links updated to match
- [x] Historical/archived docs left untouched on purpose: `Main-POC-Plan-ARCHIVE.md`,
      `Dictionary-Service-Plan.md`, `.ai-archive/*`, and the two renumbering tables above this one —
      all document *past* renumbering events and are frozen snapshots, not live cross-references.
      Same reasoning applies to the two memory-file passages recording *earlier* uses of the "Phase
      20" slot (`tenant_service_separation_decision.md`, `accounts_service_plan.md`,
      `Refdata-Versioning-Tenancy-Design.md` § — each describes a 2026-07-28 event, not this one, and
      would be actively misleading if rewritten to say "Phase 100"

---

## Working Assumptions

- JetStream is the source of truth: commands hydrate aggregates by replaying the stream, and Postgres (Shape B) and KV (Shapes A/B) are downstream projections populated only by event consumers — never written directly by the command path. (Superseded earlier assumption that Postgres was the source of truth for Shape B.)
- NATS KV is appropriate for low-latency lookup and watch-based invalidation
- A context key is always present in the KV key — no global/unscoped lookups. **Amended Phase 16a:**
  `{context}` is the **company / business-unit** scope only. Tenant is the **NATS account** (never in
  the key or subject) and region is a **separate regional deployment** (also never in the key or
  subject); the earlier "tenant/region/locale" phrasing conflated three separate axes. Locale
  remains a distinct dimension resolved inside refdata-service. See § Phase 16 and
  `ARCHITECTURE-COMMUNICATIONS.md` § 2.3.
- Eventual consistency is acceptable for dictionary reads
- No approval workflow, audit trail, or versioning needed for this POC
- Demo data is seeded via the command API (no seed scripts needed)
