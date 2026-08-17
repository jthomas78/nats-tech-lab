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

### Phase 20 (20a/20b DONE 2026-08-03) — JetStream Account Limits: Update, Visibility, and Stream-Count Redesign

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

### Phases 23, 25–28, 30 — Completed (archived 2026-08-17)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale or checklist detail for a specific completed phase). Each of
these was at 93%+ checklist completion; their few genuinely-open items were
not left stranded in an archived file — see Phase 42 below, which
consolidates them.

- [x] Phase 23 (IMPLEMENTED 2026-08-04) — Admin UI: SSE → NATS WebSocket
      Migration (Dual-Connection Model): all four `frontend/admin` SSE
      streams replaced with direct browser NATS WebSocket pub/sub via a
      dedicated Admin/Platform connection (`MintAdminToken`, BR-AC18) plus
      the existing per-tenant connection; `sse.go`'s watch handlers deleted.
      One item carried forward to Phase 42 (a specific multi-tab
      live-verification pass never explicitly run, though since covered in
      substance by later full-stack rebuilds).
- [x] Phase 25 (25a–25h IMPLEMENTED, 25e RESOLVED 2026-08-06) — Pricing
      Service: Port Linebooker's Rate/Fee Domain: new `pricing-service`
      (FeeScale/RateSheet/FixedRate domain, draft/publish/rollback,
      tenant-aware NATS connections), Admin UI panel, live-verified end to
      end.
- [x] Phase 25i (DONE) — Effective-Dated Diesel Overlay: BR-P17–P24, live
      overlay lookups against a versioned diesel-price corpus.
- [x] Phase 26 (IMPLEMENTED 2026-08-13) — Trading Partner Service:
      Shipper/Transporter Registration: new `trading-partner-service`
      (registration lifecycle, compliance documents, fleet assets with
      refdata-validated `vehicleTypeCode`, append-only audit log), Admin UI
      panel, live-verified end to end. Deferred design questions (temporal
      modeling, marketplace `notify.*`, etc.) carried forward to Phase 42
      rather than dropped.
- [x] Phase 27 (IMPLEMENTED 2026-08-14) — Admin UI: Account Activity Panel
      (`/accstatz`): per-account connection/subscription/message-rate
      activity view reusing BR-028's labeling mechanism.
- [x] Phase 28 (IMPLEMENTED 2026-08-16) — Distributed Tracing for
      Inter-Service Comms: `obs.trace.*` W3C-style trace propagation across
      every hop/service/account boundary, replacing the old
      correlation-id-only `obs.rpc.*`/`obs.api.*` side channel; `TRACES`
      stream + KV trace store; Admin UI waterfall panel; `otlp-bridge`.
      Findings note: `obsidian/POC-Dictionaries/4. Findings - Distributed
      Tracing (Phase 28).md`.
- [x] Phase 30 (IMPLEMENTED 2026-08-16) — `observability-service`: Extract
      Cross-Account Diagnostics from shipping-service: Connections,
      Services, Account Activity, Streams, KV Buckets, Log, and the trace
      store all moved to a new dedicated PLATFORM-account service;
      live-verified via `docker compose down -v && up --build`, found and
      fixed 6 real cross-account permission bugs in the process. Several
      follow-on fixes landed after this phase closed (account-status dots,
      `nonTenantCredsFiles` gaps, `$JS.FC` grant, trace payload echo) — see
      the phase's own addendum in the archive.

---

### Phase 40 (following on from Phase 24; 24a DONE, 24b/24c not started) — Credential Lifecycle Hardening: Hermetic Tests, Volume-Backed Creds, Runtime Tenant Provisioning

> **Renumbered 2026-08-17** from Phase 24 to Phase 40, alongside Phase
> 29 → Phase 41, when Phases 23/25/25i/26/27/28/30 were archived (see the
> "Renumbering (2026-08-17)" log near the end of this document). Sub-phase
> labels below (`24a`/`24b`/`24c`) are kept as-is rather than renumbered to
> `40a`/`40b`/`40c` — they're already referenced under the `24a`/`24b`/`24c`
> spelling in code/test comments (e.g. `isolation_test.go`,
> `tenant_switch_test.go`) and in this phase's own design section below;
> renaming them would be a much larger, purely-cosmetic sweep for no
> functional benefit. Only the containing phase number changed.

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

> **Risk flagged 2026-08-17 (post-Phase-30 audit) — 24b's scope was written before `observability-service`/BR-AC31/BR-AC32 existed and does not account for them.** `observability`'s PLATFORM user is now the most heavily-permissioned one in `bootstrap-operator.sh`: BR-AC31's `$SRV` export/import, BR-AC32's `$JS.API` subset, plus five grants Phase 30i's live-verification pass found the hard way and are easy to forget porting individually — `$JS.API.INFO`, the filtered-`CONSUMER.CREATE` wildcard form (`$JS.API.CONSUMER.CREATE.*.*.>`), `$JS.API.DIRECT.GET`, `$JS.ACK`, and `$JS.FC.KV_trace-request-reply.>`. If 24b's `Provisioner` restricted-permission-minting path is built by porting only `shipping-admin`'s grants (as originally scoped) and `observability`'s are missed, the next creds regeneration silently drops them — reintroducing the exact bugs Phase 30i's fix cycle closed, just via a different mechanism. 24b's named-volume swap must also mount `observability.creds`, which the original scope (written pre-Phase-30) has no way to have anticipated. Any future 24b work should re-derive the full `observability` permission list directly from the current `bootstrap-operator.sh`, not from this phase's own design section above.

- [ ] 24b: `bootstrap-operator.sh` exports `sys`'s signing key seed (`nats/keys/sys-signing-key.nk`), matching BR-AC19's pattern for the other three accounts
- [ ] 24b: pure-crypto `sys.creds` bootstrap step ahead of `accounts-service`'s first NATS dial
- [ ] 24b: `Provisioner` gains a restricted-permission user-minting path; `shipping-admin`'s **and `observability`'s** exact permission sets ported from `bootstrap-operator.sh`'s `nsc edit user` calls (see risk note above — `observability`'s list has grown well past `shipping-admin`'s since this phase was scoped)
- [ ] 24b: `ensureSigningKey`/`seedPreexistingAccounts` mint-and-write `platform`/`acme`/`globex` creds on adoption/establishment
- [ ] 24b: `docker-compose.yml` — named volume replacing the `./nats/creds` bind mount across every consumer, **including `observability-service`'s `observability.creds` mount** (added in Phase 30c, after this sub-phase's original scope was written)
- [ ] 24b: `BUSINESS_RULES-ACCOUNTS.md` — new rule documenting the populate-on-boot mechanism and the `sys.creds` special case
- [ ] 24b: Live verification — `docker compose down -v && up --build` with `./nats/creds` no longer existing as a bind mount at all; confirm every service connects, **including `observability-service` and every NATS/SYSTEM Admin UI panel it backs (Phase 30's own live-verification checklist)**

> **Risk flagged 2026-08-17 (post-Phase-30 audit) — 24c's already-documented boot-ordering trap is now a multi-service problem, not the single-service one it was scoped against.** The design note above only warns about `shipping-service`'s own creds-directory tenant scan. Since this phase was written, `trading-partner-service` and `pricing-service` each grew their *own* independent creds-directory scanner and their own `nonTenantCredsFiles` exclusion list (`tradingpartner/internal/tenants/tenants.go`, `pricing/internal/tenants/tenants.go`) — and this session found and fixed the identical latent bug (a missing `observability` exclusion entry) in both, independently, confirming the pattern really does drift out of sync across services. Moving `acme`/`globex` to async runtime creation reopens the exact race this phase already flags, but across three independent scanners instead of one. Any future 24c implementation must re-audit every service's tenant-discovery scanner (currently: shipping-service, pricing-service, trading-partner-service — check for new ones before starting), not just shipping-service's.

- [ ] 24c: `Provisioner`/seed step creates `acme`/`globex` via `POST /api/accounts` (or an equivalent internal call) instead of `bootstrap-operator.sh`
- [ ] 24c: confirm `EnsureTenantByName`/`notify.accounts.account.created` actually covers the boot-ordering gap this introduces — trace current callers, don't assume
- [ ] 24c: **re-audit every service's creds-directory tenant-discovery scanner** (shipping-service, pricing-service, trading-partner-service as of 2026-08-17 — confirm the current list before starting), not only shipping-service's, per the risk note above
- [ ] 24c: `bootstrap-operator.sh` scope reduced to `operator` + `SYS` + `PLATFORM` only
- [ ] 24c: `BUSINESS_RULES-ACCOUNTS.md` — rule change documenting PLATFORM-bootstrapped / tenants-runtime as the enforced split
- [ ] 24c: Live verification — fresh `down -v && up --build` produces working `acme`/`globex` tenants with no bootstrap involvement beyond `PLATFORM`

### Phase 41 (following on from Phase 29) (PROPOSED — not started) — NATS 2.11 Server-Hop Tracing ("Trace this subject")

> **Renumbered 2026-08-17** from Phase 29 to Phase 41, alongside Phase
> 24 → Phase 40, when Phases 23/25/25i/26/27/28/30 were archived (see the
> "Renumbering (2026-08-17)" log near the end of this document). No
> internal references needed updating — this phase has none.

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

### Phase 42 — Close-Out Review: Outstanding Items Carried Forward from Archived Phases

#### Goal

Phases 20, 21, 22, 22b, 23, 25, 25i, 26, 27, 28, and 30 were archived to
[Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md) (2026-08-17) once each
reached 93%+ checklist completion. Three of them had small, genuinely-open
items that shouldn't just disappear into a file that isn't read into
context by default. This phase exists purely to hold those items until
they're either done or explicitly re-deferred — it has no design section
of its own because it invents nothing new, only carries forward what was
already scoped.

#### Checklist

- [ ] **From Phase 21 (Account Exports/Imports: Two-Account Partitioning).**
      Live verification: `bootstrap-operator.sh --force` +
      `docker compose down -v && up --build`; refdata labels still resolve
      for both tenants; Connections/Services panels still show
      PLATFORM-labeled rows; a tenant-created event still reactively
      provisions shipping-service resources; crafting the old-style
      cross-context subject directly now fails/times out. **Note
      (2026-08-17):** the first three sub-checks have been incidentally
      re-proven by the many full-stack rebuilds since (Phase 30i and this
      session's own fixes) — labels resolve, panels work, reactive
      provisioning works, all observed directly in this session's browser
      checks. The specific adversarial check (the old-style cross-context
      subject failing/timing out) has not been explicitly re-tested.
- [ ] **From Phase 23 (SSE → NATS WebSocket Migration).** Live
      verification: `nats/bootstrap-operator.sh --force` +
      `docker compose down -v && up --build`, then confirm multi-tab open
      no longer exhausts connections, the connection indicator reflects
      PLATFORM connectivity independent of BU/tenant selection, and all
      four panels function with SSE fully removed. **Note (2026-08-17):**
      SSE is confirmed fully gone from the code — every remaining
      `EventSource`/`watchKVBucket`/`watchJetStream`/`watchRPCObs` hit in
      the tree is a historical comment, not live code — and the panels
      have since been exercised live across several full
      `down -v && up --build` cycles (Phase 30i, and the `$JS.FC`/trace-
      payload fixes) with the connection indicator working correctly every
      time. The one specific claim not yet re-verified is the multi-tab
      connection-exhaustion scenario itself.
- [ ] **From Phase 26 (Trading Partner Service) — deferred items, carried
      forward as named open questions, not silently dropped:**
      lifecycle-as-CQRS/temporal exploration, `ComplianceDocument` temporal
      classification, document-expiry-driven status, real file storage,
      terminal/offboarding state, platform-identity vs tenant-membership
      split, `notify.*` publication once a marketplace consumer exists.
      Intentionally open-ended — a list to revisit if/when any of these
      becomes a real requirement, not a task with a completion criterion.

---

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
## Renumbering (2026-08-17 — Archive close-to-completion phases, free the 23–30 block)

**Why:** Phases 23, 25, 25i, 26, 27, 28, and 30 had each reached 93–100%
checklist completion, and their full design/checklist detail (over 2,400
lines combined) was making the live plan harder to scan for what's actually
still open. Following the same "archive completed work, keep a one-line
summary + link" pattern already used for Phases 0–19, their full detail
moved to `Main-POC-Plan-ARCHIVE.md`, condensed to a one-bullet-per-phase
summary in this document (see "Phases 23, 25–28, 30 — Completed" above).
Their small number of genuinely-open items (Phase 23's multi-tab
live-verification pass, Phase 26's deferred-questions list) were not
archived along with them — an archived file isn't read into context by
default, and a checklist item nobody sees again isn't tracked, it's lost.
They were folded into a new **Phase 42 — Close-Out Review**, a standing
phase with no design of its own, that exists purely to hold carried-forward
loose ends until they're resolved or explicitly re-deferred.

Phases 24 and 29 were **not** close to completion (24: only 24a of
24a/24b/24c done; 29: 0%) and stayed active, but sat orphaned in the middle
of a now-archived block — renumbered up to the 40s to sit together as their
own forward-looking group, following the precedent of the two renumbering
tables above this one (old candidate phases moved to a fresh open range
rather than staying wedged between completed work).

| Was | Now |
|---|---|
| Phase 24 (24a DONE; 24b/24c not started) — Credential Lifecycle Hardening | **Phase 40** |
| Phase 29 (PROPOSED, not started) — NATS 2.11 Server-Hop Tracing | **Phase 41** |
| *(new)* — Close-Out Review, outstanding items from archived Phases 23/26 | **Phase 42** |

Archived in full (no longer numbered as active phases in this document,
only as one-line summaries — see above):

| Archived | Where |
|---|---|
| Phase 23 — Admin UI: SSE → NATS WebSocket Migration | `Main-POC-Plan-ARCHIVE.md` |
| Phase 25 (25a–25h/25i) — Pricing Service | `Main-POC-Plan-ARCHIVE.md` |
| Phase 26 — Trading Partner Service | `Main-POC-Plan-ARCHIVE.md` |
| Phase 27 — Admin UI: Account Activity Panel | `Main-POC-Plan-ARCHIVE.md` |
| Phase 28 — Distributed Tracing for Inter-Service Comms | `Main-POC-Plan-ARCHIVE.md` |
| Phase 30 — `observability-service` Extraction | `Main-POC-Plan-ARCHIVE.md` |

Cross-reference sweep (same commit):

- [x] Main plan internal references — the two prior renumbering tables above
      this one already document *earlier, unrelated* uses of the numbers
      24/29 in an older scheme (a former "Phase 24 — Performance & Load
      Testing" → Phase 104, from the 2026-08-03 renumbering); left
      untouched on purpose, same reasoning as every prior entry in this log
      — they're frozen snapshots of a past event, not live references to
      the phases renumbered here.
- [x] `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-ACCOUNTS.md`,
      `ARCHITECTURE-DICTIONARY.md`, `ARCHITECTURE-COMMUNICATIONS.md`,
      `ARCHITECTURE-ADMIN.md` cite "(Phase 24)"/"(Phase 29)" — **resolved
      2026-08-17.** Two citations (`ARCHITECTURE-COMMUNICATIONS.md`'s
      "SERVER rows need no service code at all", `ARCHITECTURE-ADMIN.md`'s
      "Server-hop spans") were genuinely about the real Phase 29 content and
      just needed the number updated to Phase 41. One
      (`ARCHITECTURE-ACCOUNTS.md`'s "What the tenant connection can and
      cannot reach") was genuinely about Phase 23 and got that number
      instead. The remaining four (`ARCHITECTURE-ACCOUNTS.md`'s "Two
      PLATFORM connections, not one"; `ARCHITECTURE-DICTIONARY.md`'s "Not
      contradicted by the Admin UI's cross-account panels"; and
      `ARCHITECTURE-COMMUNICATIONS.md` §§ 11–12) turned out to be genuinely
      **stale content**, not just mislabeled — all four described
      `shipping-service` hosting cross-account diagnostics
      (`PlatformFullJS`, `tenantLabelsByAccount()`, the six lifted REST
      endpoints) that Phase 30 actually moved to `observability-service`.
      Fixed with Phase 30(d/h) amendment blockquotes in place — original
      text kept as historical record, per this repo's own established
      documentation convention — rather than rewritten in place. Also found
      and fixed the same staleness in a source comment
      (`frontend/admin/src/components/ConnectionsPanel.vue`'s
      `resolveLabel()` doc comment, which still named the retired
      `nats_ops.go`/`tenantLabelsByAccount()` and claimed a resilience
      property — independent resolution if accounts-service is down — that
      no longer holds now that both label tiers depend on accounts-service).
- [x] `demos/01-dictionary/BUSINESS_RULES-*.md` — no "Phase 24"/"Phase 29"
      references found
- [x] Go/Vue source comments — no "Phase 24"/"Phase 29" references found
      (existing `24a`/`24b`/`24c` comments in `isolation_test.go`/
      `tenant_switch_test.go` deliberately left as-is; see Phase 40's own
      renumbering note)
- [x] `.claude/memory/` — no "Phase 24"/"Phase 29" references found
- [x] Historical/archived docs left untouched on purpose: the two prior
      renumbering tables above, `Main-POC-Plan-ARCHIVE.md`,
      `Dictionary-Service-Plan.md`, `.ai-archive/*` — frozen snapshots of
      past events, not live cross-references

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
