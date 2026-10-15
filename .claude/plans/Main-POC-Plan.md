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
      deploy/             # Compose, split into bands (ADR-055): cell/, global/, environments/
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
Microfrontend integration is planned separately in
[Application-Shell-Microfrontend-Plan.md](Application-Shell-Microfrontend-Plan.md)
(design gate, 2026-08-27), whose design of record is
[`ARCHITECTURE-APP-SHELL.md`](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-APP-SHELL.md).

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

Each demo owns its own Compose files. Lab shell has its own. They do not share networks.

Demo 01's were split into bands on 2026-09-08 (ADR-055) and the one flat
`docker-compose.yml` was deleted. Launch it from `demos/01-dictionary/deploy/cell`:

```bash
docker compose -p lb-za-1 --env-file ../environments/local-za-1.env \
  -f compose.yaml -f ../global/compose.control.yaml up -d --build
```

```yaml
# demos/01-dictionary/deploy/cell/ services:
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

### Phases 20–22b — Completed (archived 2026-08-17)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale or checklist detail for a specific completed phase). Phase 21's
one open item was folded into Phase 62 below rather than left stranded in
an archived file.

- [x] Phase 20 (20a/20b DONE 2026-08-03) — JetStream Account Limits: Update,
      Visibility, and Stream-Count Redesign: `Provisioner.UpdateAccountLimits`
      + usage endpoint (BR-AC12); collapsed shipping-service's per-context KV
      buckets to one bucket per tenant with context-prefixed keys, closing
      the `js_max_streams` headroom problem structurally rather than just
      raising the ceiling.
- [x] Phase 21 (IMPLEMENTED 2026-08-03) — Account Exports/Imports:
      Two-Account Partitioning (PLATFORM Cross-Cutting, Tenant Data-Plane):
      `bootstrap-operator.sh` PLATFORM exports/imports + restricted
      `shipping-admin` user; `accounts/provisioner.go` claim-preservation and
      tenant import minting. One item carried forward to Phase 62 (an
      adversarial live-verification check — the old-style cross-context
      subject failing/timing out — never explicitly re-run, though the
      surrounding behavior has been exercised live many times since).
- [x] Phase 22 — Business Units Owned by accounts-service: `business_units`
      table + reserved-name validation + `_default_bu` auto-creation;
      `refdata.contexts` gains a `visible` column; three hardcoded frontend
      `CONTEXTS` fallback arrays deleted in favor of the live list.
- [x] Phase 22b (IMPLEMENTED 2026-08-13) — Business Unit Name/Context Split;
      Per-Tenant Default BU: `business_units` gains a globally-unique,
      immutable `context` slug distinct from its display `name`;
      `{tenant}-default` replaces the shared `_default_bu` literal as each
      tenant's actual default, parented under the now platform-owned
      `_default_bu` template.

### Phases 23, 25–28, 30 — Completed (archived 2026-08-17)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale or checklist detail for a specific completed phase). Each of
these was at 93%+ checklist completion; their few genuinely-open items were
not left stranded in an archived file — see Phase 62 below, which
consolidates them.

- [x] Phase 23 (IMPLEMENTED 2026-08-04; topology simplified 2026-08-26) —
      Admin UI: SSE → NATS WebSocket Migration. The original dual-connection
      model was reduced to one Admin/Platform connection after centralized
      observability and Phase 36 removed every tenant-live consumer;
      `admin-tenant` no longer exists. `sse.go`'s watch handlers remain
      deleted.
      One item carried forward to Phase 62 (a specific multi-tab
      live-verification pass never explicitly run, though since covered in
      substance by later full-stack rebuilds).
- [x] Phase 25 (25a–25h IMPLEMENTED, 25e RESOLVED 2026-08-06) — Pricing
      Service: Port Linebooker's Rate/Fee Domain: new `pricing-service`
      (FeeScale/RateSheet/FixedRate domain, draft/publish/rollback,
      tenant-aware NATS connections), Admin UI panel, live-verified end to
      end.
- [x] Phase 25i (DONE) — Effective-Dated Diesel Overlay: BR-P17–P24, live
      overlay lookups against a versioned diesel-price corpus.
- [x] Phase 26 (IMPLEMENTED 2026-08-13) — Organizations Service:
      Shipper/Transporter Registration: new `organizations-service`
      (registration lifecycle, compliance documents, fleet assets with
      refdata-validated `vehicleTypeCode`, append-only audit log), Admin UI
      panel, live-verified end to end. Deferred design questions (temporal
      modeling, marketplace `notify.*`, etc.) carried forward to Phase 62
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

### Phase 31 — Completed (archived 2026-08-19)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale or checklist detail).

- [x] Phase 31 (IMPLEMENTED 2026-08-17) — Consolidate to Shape B: retired
      Shapes A and C. Shape A's two production paths (live ship notify,
      `ListShips` bootstrap) migrated to Shape B first; Shape A/C code,
      queries, projectors, durables, and Admin UI comparison panels then
      deleted; `RegisterShapeB`/`queries.ShapeB`/`dict-b` renamed to neutral
      `RegisterShips`/`queries.Ships`/`ships` (KV bucket rebuilt via `down -v`
      reset); frontend panels/store/specs repointed; BR-024 rewritten,
      BR-020/BR-019 amended; docs/diagrams/swagger regenerated; findings
      write-up added to `obsidian/POC-Dictionaries/`. Live-verified: Sea
      Freight Flow fleet panel populates on connect and updates live on
      arrive/depart; Admin UI KV Buckets panel confirms only
      `container`/`meta`/`ships` remain; CQRS Shapes nav badge reads `1`.

---

### Phase 32 — Completed (archived 2026-08-17)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale or checklist detail).

- [x] Phase 32 (IMPLEMENTED 2026-08-17) — refdata-service Serves Browsers
      Directly: per-tenant `api.*` connections (`refdata/internal/tenants`),
      admin/business subject split enforced by `MintBrowserToken`'s
      subject-prefix Deny (BR-D41), `notify.*` replacing `/api/refdata-watch`
      SSE (`internal/notifybridge`, BR-D42), and shipping-service's five
      refdata relay routes retired. `frontend/refdata` migrated onto
      `nats.ws` end to end (discovered mid-phase to be a cross-tenant
      platform-operator tool, not a tenant app — needed its own PLATFORM
      credential, `MintRefdataAdminToken`; see
      [phase32_refdata_platform_credential](../memory/phase32_refdata_platform_credential.md)).
      Prerequisite for Phase 33 (retiring business REST) satisfied.

---

### Phase 33 — Completed (archived 2026-08-17)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale or checklist detail).

- [x] Phase 33 (IMPLEMENTED 2026-08-17) — Retire Business REST: deleted business
      REST across shipping-service (`/api/ships,containers,terminal,manifest,ports,meta`),
      pricing-service (`/api/pricing/*`, 34 routes), organizations-service
      (`/api/organizations/*`, 14 routes), and refdata-service's business
      reads (`/api/refdata/*`); renamed `/api/shape-b/*` → `/api/admin/read-path/*`;
      added `api.*.shipping.container.manifest.v1`. Deviation: refdata-service's
      `/api/refdata/admin/*` could not be deleted — `accounts-service` calls it
      server-to-server with no NATS equivalent — so it became a permanent
      documented exemption (BR-D43) instead; see
      [phase33_refdata_admin_rest_exemption](../memory/phase33_refdata_admin_rest_exemption.md).
      All backend suites + frontend suites/builds green; live `docker compose`
      smoke test confirmed deleted routes 404 and surviving admin/exemption
      routes work. Prerequisite for Phase 34 (mux allowlist enforcement)
      satisfied for the routes that actually got deleted.

### Phase 34 — Completed (archived 2026-08-17)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale or checklist detail).

- [x] Phase 34 (IMPLEMENTED 2026-08-17) — Enforce the Boundary: every
      service's `Mount` now returns the exact route list it registers,
      asserted `ConsistOf` a hardcoded admin/infra allowlist per service
      (BR-040, mirrored as BR-D44/BR-P27/BR-TP17/BR-AC33) so a future
      business route fails a test instead of quietly shipping; `traceSpan`
      (all 5 `natstrace` copies) gained a `Requester` field lifting the
      self-declared, never-authoritative `Nats-Requestor` header onto the
      wire envelope (BR-041); the Admin UI's Request/Reply & Traces panel
      gained two visibly-distinguished toolbar filters (subject-prefix,
      server-enforced; requester, self-declared); a full test-suite audit
      confirmed no business integration test exercises REST anywhere.
      Live-verified: a throwaway business route added to pricing-service's
      `Mount` failed its allowlist test with a clear diff; all 6 services'
      full suites green post-merge.

---

### Phase 35 — Completed (archived 2026-08-18)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale or checklist detail).

- [x] Phase 35 (IMPLEMENTED 2026-08-18) — Shared Go Package Extraction:
      `natstenants`, `natstrace`, `browserrpc` Infra Tail. Repo-root `go.work`
      + per-service `replace` directives (belt-and-suspenders) established as
      the module strategy, proven against both `go build ./...` and every
      Dockerfile's now-repo-root build context. `shared/natstenants.Manager[R
      any]` extracted and consumed by `pricing-service`,
      `organizations-service`, and `refdata-service` directly, and by
      `shipping-service` for connection lifecycle only; `shared/browserrpc`'s
      reply-tail helpers consumed by all four `adapter.go` files (call-site
      signatures kept per-service rather than force-unified); `shared/
      natstrace` consumed by all five services incl. `accounts-service`. Each
      service's own duplicate package/functions deleted outright — no
      compatibility shims. `ARCHITECTURE-ACCOUNTS.md`, `ARCHITECTURE-
      COMMUNICATIONS.md` § 6, and BR-D40 updated from recommendation/
      duplication language to implemented-shared-package language. All 10
      workspace modules build/vet clean; every service's full `ginkgo ./...`
      suite green; live `docker compose down -v && up --build` verified zero
      panics/fatals/auth violations and the Admin UI's Request/Reply trace
      panel showing live, error-free `api.*` traffic through the refactored
      adapters.

---

### Phase 36 — Completed (archived 2026-08-19)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale or checklist detail).

- [x] Phase 36.1/36.2 (IMPLEMENTED 2026-08-19) — Tech Lab Operator Rebrand &
      Organizations Migration: renamed `refdata` to "Tech Lab Operator"
      with an `Operations` nav (`Reference Data` tabbed panel); migrated
      `admin`'s Organizations section (Shippers/Transporters) into it via
      a new tenant-scoped browser NATS connection (`useTenantConnection.js`)
      fed by accounts-service's `GET /api/auth/tenants`, avoiding a
      cross-app coupling to shipping-service's shared tenant connection.

---

### Phase 37 — Completed (archived 2026-08-19)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale or checklist detail).

- [x] Phase 37 (IMPLEMENTED 2026-08-19) — VitePress Documentation Site:
      first proposed as Phase 46, renamed to 37 and approved in the same
      request; scaffolded a standalone VitePress project at
      `demos/01-dictionary/docs/` (port 7106) with a custom theme
      overriding VitePress's `--vp-c-*` tokens to the UniFi palette, a
      Pattern-Cards-inspired "DECISION" custom container plus
      `EyebrowLabel`/`VerdictBadge` components, nav/sidebar with landing
      pages for the Architecture section (CQRS Shapes, Dictionary,
      Communications, Accounts, Admin, Platform), and the
      `system-architecture-swimlane.png` diagram copied into
      `docs/public/`. Live-verified in-browser: `npm run build` clean,
      `npm run dev` no console errors, both light and dark palettes render
      correctly.
- [x] Phase 37 follow-up, same day — dockerized the docs site: reverses
      this phase's own original "no `docker-compose.yml` change, hosting
      out of scope" design decision, on direct user request. Added
      `demos/01-dictionary/docs/Dockerfile` (multi-stage `node:24-alpine`
      build → `nginx:1.27-alpine` static serve, same pattern as the three
      existing frontend Dockerfiles but with a self-contained build
      context — this site imports nothing from `shared/unifi-theme`, so
      unlike the other three the build context is `docs/` itself, not the
      repo root) and `nginx.conf` (clean-URL `try_files` fallback plus a
      proper `error_page 404` so direct hits on VitePress's per-page
      `.html` output resolve, and 404s return a real 404 status instead of
      200). Added a `docs-frontend` service to `docker-compose.yml`
      (port `7106:80`, no `depends_on` — the site makes no backend calls).
      Only covers building the docs site into the local demo stack;
      publishing it online (GitHub Pages etc.) remains a separate,
      still-unaddressed decision. Live-verified: `docker compose build
      docs-frontend` clean, container serves `/` (200), a clean URL
      `/architecture/accounts` (200, correct page title), a real 404 route
      (404 status, VitePress's own 404 page content), and the copied
      diagram PNG (200); container stopped/removed after verification.

---

### Phase 38 — Completed (archived 2026-08-21)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale, design decisions, or checklist detail).

- [x] Phase 38 (DONE 2026-08-21; first proposed as Phase 46, renumbered to
      38 and design-approved 2026-08-20) — **Transporter Registration &
      Vetting (Organizations)**: an event-sourced `TransporterProfile`
      aggregate keyed by the shared `Organization` ID, a Temporal saga
      driving the vetting lifecycle, compliance documents in a NATS Object
      Store, and a dedicated Transporter UI. Covers BR-TP18–BR-TP63 and
      BR-D46–BR-D48. Delivered in eleven sub-phases:
- [x] **38a** — `TransporterProfile` domain package and event-sourcing
      skeleton: aggregate, commands, JetStream `TRANSPORTER` stream,
      Postgres projection (BR-TP18–BR-TP20).
- [x] **38b** — Temporal vetting saga: workflow, activities, and worker
      packages (BR-TP21–BR-TP28). Reopened 2026-08-21 because the saga was
      built and tested but never composed into the running service; closed
      by "38b (completion)" below.
- [x] **38c-i** — `organizations` schema pass + editable Company
      Information, including the PK widening and a new HTTP ingress
      (BR-TP29–BR-TP36).
- [x] **38d-i** — Transporter UI: dedicated component, registration wizard,
      tabbed detail view, state-transition stepper, Company Information
      editing (BR-TP37–BR-TP39). Not purely frontend.
- [x] **38c-ii** — NATS Object Store compliance-document upload/download,
      keyed off 38c-i's document ID (BR-TP40–BR-TP45).
- [x] **38d-ii** — Operating Areas + Tracking Credentials, backend and
      frontend — neither had any backend before this (BR-D46–BR-D48,
      BR-TP46–BR-TP55).
- [x] **38e** — `organizations` rename across service, packages, subjects,
      and UI labels; amended 2026-08-21 to rename the `api.*` entity token
      `partner` → `organization` (9 subjects).
- [x] **38b (completion)** — composed the Temporal worker into the running
      service and verified it live; added the vetting-decision subject
      (BR-TP56–BR-TP58).
- [x] **38g** — `cmd/seed-transporters`: a ten-rung seeding ladder driven
      over the public API against the real Temporal saga, from bare
      registration through vetted and rejected states.
- [x] **38h-i** — made a compliance document's `expiresAt` settable through
      the `api.*` surface (BR-TP59).
- [x] **38h-ii** — replaced BR-TP28's polling Temporal Schedule with a
      durable expiry timer and the `CoverLapsed` transition, plus the
      Postgres CHECK-constraint migration (BR-TP60–BR-TP63). Verified
      against a freshly reseeded stack.

---

### Phase 39 — Completed (archived 2026-08-23)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale, the 29 design decisions, or checklist detail).

- [x] Phase 39 (DONE 2026-08-22; design gate closed 2026-08-22) — **GIT
      Certificates: status view, drill-down edit**. Gave goods-in-transit
      cover its own tab on the Transporter detail view — a flat certificate
      table, a drill-down edit view, and an always-open registration drop
      zone — closing the gap between our single `CoverageCents` and V2's
      per-goods-type cover map, and introducing the `FOR_REVIEW` document
      state. Covers BR-TP64–BR-TP72 and amends BR-TP11 and BR-TP38.
      Provenance per [ADR-050](../../obsidian/V3-Platform/Architecture/ADR/lab/ADR-050-lab-organizations-git-certificate-change-log-provenance.md)
      (Option A, scoped to `GOODS_IN_TRANSIT`). Delivered in three
      sub-phases:
- [x] **39a** — domain: goods types on `ComplianceDocument`, per-goods-type
      cover, the `FOR_REVIEW` state, locking on approval, actor on every
      command, and the Option A write path — GIT document commands moved onto
      the `TransporterProfile` aggregate, which became the sole producer of
      `document-approved` — plus register-plus-upload so a drop creates row
      and file together (BR-TP64–BR-TP72).
- [x] **39b** — `goods-type` refdata vocabulary and seeded corpus, without
      which 39c could not be exercised.
- [x] **39c** — the GIT Certificates tab itself: flat table, always-open drop
      zone, drill-down edit view, and a new read query (`ListGitCertificates`)
      that keeps superseded rows and orders newest registration first.

**Two rule changes came out of 39c's live verification (2026-08-22), not
from the original design:**

- **BR-TP38 amended.** `DeriveGitStatus` took the worst status across all
      current GIT documents — correct while BR-TP30 superseded on *upload*,
      wrong once decision 5 moved supersession to *approval* and renewals
      began coexisting with live cover. A rejected renewal was reading as the
      transporter's cover status, and since `IsGitActive` feeds BR-TP28's
      suspension, it would have revoked fleet availability from a covered
      transporter. The approved certificate now answers for the transporter;
      worst-of-the-rest applies only when there is none.
- **BR-TP11 scoped to the four CRUD document types.** GIT resubmission was
      removed entirely: a rejection is final for that certificate and the
      operator registers a new one. This resolved a genuine conflict — 39c
      had shipped a Resubmit button while the intended workflow was
      replacement.

*(Follow-on work — the GIT certificate change log and the
`Awaiting` presentation fix — was split out at the design gate
as Phase 46, which remains PROPOSED.)*

---

### Phase 40 — Completed (archived 2026-08-24)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need the original
rationale, the ten design decisions, or checklist detail).

- [x] Phase 40 (IMPLEMENTED 2026-08-24; design gate closed 2026-08-24) —
      **Every Compliance Document Is a File**. Collapsed the two shapes a
      compliance document could take — registered by reference with no bytes
      (`PENDING`), or registered with bytes — into one, for all five document
      types. A document cannot be registered without a file, and its human
      handle is its document name.
- [x] `reference` left the model; `document_name` replaced it, supplied to the
      **register** command rather than the upload, because
      `DocumentRegistered` fires before the bytes land on a permanent
      `LimitsPolicy` stream. Labelled **Document Name** in the UI (matching
      Linebooker V2), `documentName` on the wire, `X-Document-Name` on the
      upload. Read-only once registered.
- [x] **New rule BR-TP74** — a document name is unique per organization across
      *every* status. Enforced twice on purpose: a `DocumentNameExists`
      pre-check before the append, plus a non-partial unique index
      (`compliance_documents_document_name_idx`) as the race-proof authority.
      The index alone was not enough — GIT registration appends to the stream
      before its projection row exists, so a duplicate caught only in Postgres
      would already be on the log permanently.
- [x] `PENDING` deleted from `DocumentStatus`, and **document resubmission
      retired** with it (BR-TP11), generalizing Phase 39c's GIT decision to
      every type. BR-TP26's *vetting* resubmission is untouched, but now
      refuses a re-vet until a replacement document exists.
- [x] `document.add` mints the upload ticket in its reply, which gave the four
      shared types GIT's one-gesture drop flow and let the standalone
      `upload-ticket` verb retire alongside `resubmit` — `api.*` endpoints
      28 → 26. Upload refuses a name that does not match the registered one,
      checked before `store.Put` so a mismatched drop leaves no orphan object.
- [x] Documents tab rebuilt as a drop zone for all five types; seeder uploads
      a stub PDF on every document, every rung. Reseeded rather than migrated
      (`down -v`), verified against the live database rather than a skipped
      suite.

**Two capability losses were accepted, not overlooked:** there is no longer
any way to record that a document exists without holding a copy of it, and
rejection is a dead end for that row — since names are read-only and unique
across rejected rows too, replacing a rejected `scan0001.pdf` means renaming
the file before re-dropping it.

*(Shipped in commit `46fe7c6` together with Phase 41 — the two edit the same
repositories and migration and neither half builds alone.)*

---

### Phase 41 — Completed (archived 2026-08-24)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need the original
rationale, the six design decisions, or checklist detail).

- [x] Phase 41 (IMPLEMENTED 2026-08-24; renumbered from Phase 48 on
      2026-08-24) — **ULID Entity Identity (`organizations-service`)**.
      Replaced UUID entity identity with ULID: 26 Crockford-base32
      characters, minted by the service in `organizations/internal/identity`,
      time-sortable. Full argument in
      [ADR-051](../../obsidian/V3-Platform/Architecture/ADR/lab/ADR-051-lab-organizations-ulid-entity-identity.md);
      rule text in `BUSINESS_RULES-ORGANIZATIONS.md` BR-TP73.
- [x] ULID chosen over UUIDv7 (36 chars for the same 128 bits) and NUID
      (sorts by insertion order within one process lifetime only, so a restart
      forfeits the B-tree locality that is half the point).
- [x] `<Country-Code>-<Registration-Number>` **rejected as identity** — two
      blockers in our own model: `registrationNo` is optional at `Register`
      (BR-TP35) so it does not exist when an ID is needed, and it is editable
      (BR-TP32) so it would be a mutable identity on an event-sourced
      aggregate. Plus registry-vs-country granularity, subject-unsafe
      character sets, no check digit, and PII in an immutable log.
- [x] Minting moved from Postgres to the service — `gen_random_uuid()`
      defaults removed, columns became `TEXT` with no default. The `{id}`
      subject token stays; only its length changes, 36 → 26.
- [x] **Scoped to `organizations-service`** — `shipping-service` and
      `accounts-service` consciously excluded, so two ID formats now coexist
      in this repo by decision. Recorded in `CLAUDE.md` and ADR-051 so it does
      not read later as an oversight.
- [x] Wipe-and-reseed rather than an in-place renumber: an aggregate's id is
      in every subject it has published on the `LimitsPolicy` stream, so
      renumbering orphans the history and the aggregate rehydrates **empty
      with no error**.

*(Kept as its own phase rather than folded into Phase 40 — it is a repo-wide
identity decision with its own ADR, rule and scope call, and a shared commit
is an artifact of entangled files, not a shared subject.)*

---

### Phase 42 — Completed (archived 2026-08-24)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need the original
rationale, the four design decisions, or the verification log).

- [x] Phase 42 (IMPLEMENTED 2026-08-23; renumbered from Phase 47 on
      2026-08-24) — **State Vocabulary Rename (wire-level)**. Shortened two
      `organizations-service` status enums so the list-view badges read
      cleanly: `AwaitingDocumentation` → `Awaiting` and `DocumentsInReview` →
      `InReview` on `transporterprofile/domain.Status`, and
      `REGISTERED`/`ACTIVE`/`SUSPENDED` → `registered`/`active`/`suspended` on
      `internal/domain.PartnerStatus`. `Vetted`, `Rejected` and `CoverLapsed`
      unchanged.
- [x] **Wire values, not just Go identifiers.** The vetting status is a field
      on the `TransporterProfile` event envelope, so one move reached
      JetStream, both Postgres projections, the KV cache and the badge text
      together.
- [x] **No read-side alias — a clean cut.** Pre-rename events do not hydrate,
      so the migration was a wipe-and-reseed of the `TRANSPORTER` stream, the
      transporter KV buckets and both projections. Chosen over a permanent
      compatibility shim because the POC's transporter history is disposable.
- [x] **Both `CHECK` constraints now dropped and recreated on boot** rather
      than living only in `CREATE TABLE`, which never runs against an existing
      database. That was a latent bug on `organizations.organizations` with a
      silent failure mode: unmigrated DB rejects the write, projector Naks,
      JetStream redelivers forever, nothing logs.
- [x] `PartnerStatus` is now **the only lower-cased status enum in the repo** —
      `accounts-service`, `refdata-service` and this service's own
      `DocumentStatus` stay SCREAMING. Recorded in
      `BUSINESS_RULES-ORGANIZATIONS.md` as a deliberate inconsistency so it is
      not "corrected" back later.
- [x] Verified beyond the suite: 25/25 Postgres specs run for real (they skip
      silently without `ORGANIZATIONS_TEST_DATABASE_URL` while `go test` still
      prints `ok`), both constraints read back live, `"status":"Awaiting"`
      confirmed on the stream itself with a later event hydrating to `Vetted`
      from replay, and the UI checked at `localhost:7102` after a frontend
      rebuild — the first check had read a cached bundle and looked like a
      regression.

**Accepted consequence:** the Status column renders lower-case beside
title-case Vetting and GIT badges, because `<Tag :value="data.status">` shows
the raw value and no read-side alias was taken. A display-only title-case map
would re-hide exactly what the rename made visible.

*(Its 2026-08-23 reseed has since been superseded by Phase 40/41's on
2026-08-24 — the live stack reflects the later one.)*

---

### Phase 43 — Completed (archived 2026-08-26)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need the original
rationale, the design decisions, or the verification log).

- [x] Phase 43 (IMPLEMENTED 2026-08-25; renumbered from Phase 67 on
      2026-08-25) — **Cross-Tenant Pub/Sub Observability ("Wire Tap") in the
      Admin UI**. Gave `evt.*`/`notify.*` the same cross-account visibility
      `obs.trace.*` already had, so the Admin UI can show one tenant's
      published messages from PLATFORM without either side asserting who it
      is.
- [x] **43a** — the `obs.pubsub.*` envelope, hooked **in** the `evt.*` seam
      and at every `notify.*` call site rather than at each publisher;
      `Nats-Msg-Id` = `spanId`; per-tenant export plus the
      `monitor.{tenant}.pubsub.>` import remap; BR-049's `go/ast` coverage
      scan. BR-046's redaction review ran *before* any hook was wired and
      added `actorName`/`actorSourceIP` to the shared denylist.
- [x] **43b** — `observability-service`'s `pubsubstore`, sibling to
      `tracestore`: the `PUBSUB` stream capturing **both** `obs.pubsub.>` and
      `monitor.*.pubsub.>` (remapped tenant exports would have missed one
      wildcard), bounded 1 h / 32 MiB with an explicit `Duplicates` window,
      projecting into `pubsub-messages` at 15 min / 8 MiB. Caps measured
      rather than inherited — a real envelope is 317 B – 2 KiB, not the
      ~2 KiB flat the ADR assumed.
- [x] **43c** — `MessagesPanel.vue` on its own SYSTEM → NATS nav entry, fed
      by `usePubsubFeed.js`, with the tenant named per row from the import
      remap, click-to-filter, family filters, a 500-row cap and a
      best-effort disclaimer instead of implied completeness.
- [x] **43d** — `shared/natsnotify`: one `notify.*` seam replacing the
      per-service copies.
- [x] **43e** — `shared/jstream`: the same convergence for `evt.*`, one seam
      instead of two copies, plus the reconnect/resync hardening the trace
      and message feeds both now rest on.

**Carried forward, deliberately unmade:** the Messages panel fetches every
bucket entry on load but renders 500, so a full 8 MiB bucket is a
multi-megabyte page load. Bounding it touches BR-047's caps and 43c's feed
together.

---

### Phase 44 — Completed (archived 2026-08-18)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale or checklist detail).

- [x] Phase 44 (IMPLEMENTED 2026-08-18) — Request/Reply gets a `Pulse` Tab.
      Split the requests/errors/avg-latency pulse strip off `TraceWaterfall.vue`'s
      *Traces* view onto its own tab in front of it — `[Pulse] [Traces]
      [Messages]` — pairing the enlarged pulse cards with a "what
      request/reply covers" card (including the `parentSpanId`/`spanId`
      chaining mechanism, previously undocumented anywhere in the panel) and
      an animated Client → NATS Server → Service flow diagram, per the
      approved mockup (`diagrams/admin-rpc-overview-mockup.html`). New
      `PulsePanel.vue` duplicates `TraceWaterfall.vue`'s bootstrap/subscribe/
      trace-grouping rather than sharing it (matching `RpcPanel.vue`'s
      existing Messages-tab precedent) and aggregates the *full* unfiltered
      trace set rather than `Traces`' toolbar-filtered view, since the two
      are no longer co-rendered once `Pulse` is a separate tab. `ui.rpcTab`'s
      default changed to `'pulse'`. `BUSINESS_RULES-SHIPPING.md`'s Phase 28p
      entry amended; `ARCHITECTURE-ADMIN.md` §4.5 updated from proposed to
      shipped, including resolving its "one deliberate omission" tension
      explicitly rather than leaving it unaddressed. Frontend Vitest suite
      green (3 pre-existing, unrelated `TraceWaterfall.spec.js` failures
      confirmed present before this phase's changes too, left as found);
      verified live against the docker stack.

---

### Phase 45 — Completed (archived 2026-08-19)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need original
rationale or checklist detail).

- [x] Phase 45 (IMPLEMENTED 2026-08-18) — Accounts Overview: nav restructure
      (Accounts moved PLATFORM → SYSTEM as its first entry, absorbing the
      retired standalone Account Activity panel into a new `Overview` tab;
      `TOPOLOGY` renamed `Sharing`), `observability-service` ring-buffer
      trend history (60min @ 10s, delta'd throughput, `5m/30m/1h` duration
      selector via new `GET /api/nats/account-activity/history`), and a
      gated name-filter search shown only past 3 accounts (BR-034 amended,
      BR-043/BR-044 added). Live-verified against the real ring buffer;
      `go build`/tests and frontend build/Vitest green.

---

### Phase 46 — PROPOSED (follows 39a; design inherited from Phase 39) — GIT Certificate Change Log + `Awaiting` Presentation Fix

> Split out of Phase 39 at the 2026-08-22 design gate. The design decisions
> are Phase 39's 10–13 and 16, 18, 19, 20 — archived with that phase in
> [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md), no longer above —
> plus
> [ADR-050](../../obsidian/V3-Platform/Architecture/ADR/lab/ADR-050-lab-organizations-git-certificate-change-log-provenance.md);
> this phase builds them, it does not re-decide them.

#### Goal

Two things that were 39d and 39e, neither on the critical path to the GIT
Certificates screen and neither buildable before 39a's events exist:

- the `Awaiting` presentation fix and outstanding-documents
  checklist;
- the GIT certificate change log and its CSV export.

#### Sub-phases

- [ ] **46a** — `Awaiting` presentation fix. **Partly overtaken by Phase 42
      (2026-08-23, renumbered from 47), which did change the wire value** — this sub-phase's
      original "sentence-cased label from a single label table (wire value
      unchanged)" no longer describes the state of the code: the state is now
      `Awaiting` on the wire and the stepper label is "Awaiting". What remains
      outstanding here is the rest of it — amber "your move" severity instead
      of grey, outstanding-documents checklist with progress, full-region
      dashed drop target — plus a decision on whether a label table is still
      worth introducing now that the raw value is already presentable.
- [ ] **46b** — change log, per-certificate framing only (Phase 39
      decision 20 defers the per-organization framing until fleet assets and
      tracking credentials have provenance), filters, and CSV export with
      `actor_verified` on every row.

---

### Phase 47 — Completed (archived 2026-08-26)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need the original
rationale, the design decisions, or the verification log).

- [x] Phase 47 (IMPLEMENTED 2026-08-25) — **A rejected command must publish
      no events (BR-050)**. Found by inspecting a trace produced during
      43e's live verification: `ArrivePort` published its event before the
      guard that could reject the command ran, so a refused command still
      left an event on the stream.
- [x] **47a/47b/47c** — three specs written red against the unfixed handler,
      `ArrivePort` reordered so validation precedes publication, BR-050
      written up and BR-017 amended, and the false-green spec the new rule
      exposed fixed rather than left passing for the wrong reason.
- [x] **47d** — `ginkgo ./...` green across all 9 suites.
- [x] **47e** — *unrelated to BR-050, found while verifying against a
      reseeded stack.* `organizations-service` died on every cold
      `docker compose up -d` after a `down -v`, taking `refdata-frontend`
      with it (nginx resolves upstreams at startup). Temporal has two
      distinct not-ready stages — frontend not listening, and listening
      before `temporal-auto-setup` has created the default namespace — and
      the service tolerated neither. Both now retried through one loop.

---

### Phase 48 — Completed (archived 2026-08-26)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need the original
rationale, the design decisions, or the verification log).

- [x] Phase 48 (IMPLEMENTED 2026-08-26) — **Tenant provenance for
      `obs.trace.*`**. Closed the one item ADR-047 amendment A1 explicitly
      deferred, so a span's originating account is established by the NATS
      server rather than asserted by its publisher.
- [x] **48a/48d** — the per-tenant `LocalSubject` remap on the `obs.trace.>`
      import (`monitor.{tenant}.trace.>`), plus the re-provision pass that
      converges every existing tenant account's imports on each boot rather
      than only newly created ones.
- [x] **48b/48c** — `TRACES` gained a second subject filter, the tenant is
      derived positionally from the **arrival subject** and stored per span
      (BR-051), and `TraceWaterfall.vue`'s gutter names the real tenant. The
      first-writer-wins rule (BR-052) was **retired** during 48c: it fires
      on ordinary cross-account traffic, where a trace legitimately holds
      spans from two accounts.
- [x] **48e** — `ARCHITECTURE-OBSERVABILITY.md`, A1's other deferred item.
- [x] **48f** — `trace-request-reply` bounded at 15 min / 8 MiB, sized from
      a measured seed run (BR-053) rather than from the ADR's estimate,
      matching `pubsub-messages` exactly so the two panels cross-reference.
- [x] **48g** — one span per KV key (`trace.{traceId}.{spanId}`) with an
      idempotent `Put`, the trace assembled at read time in
      `useTraceFeed.js`. Replaces a read-modify-write that was O(n²) and
      lost spans under concurrent writes. Three findings worth keeping: the
      concurrency spec is only red when it bypasses `Register`'s serialised
      consume callback (16 writers stored 1 span of 16); the `Duplicates`
      window was inert until `natstrace` set `Nats-Msg-Id`; and the feed's
      monotonic-merge guard was removed rather than ported, because per-span
      merging makes a stale snapshot a no-op instead of a rollback.
- [x] **48h** — the cross-account multi-span trace harness (`seed-traces`),
      driving `organizations-service` → `refdata-service` in both outcomes.
      Two corrections against the running stack: the chain is
      `organizations-service`, not `shipping-service` (Phase 32 removed the
      latter's refdata relay routes), and it drives the hop with the
      tenant's own credential, since the seeded accounts have no signing key
      for a browser token.
- [x] **48i** — closed by live verification rather than by code: the
      Messages panel's tenant column landed in Phase 43c. What it shows is
      the *publisher's* account, so an `evt.acme.…` row reads `_platform` —
      the `acme` in the subject is `{context}`, not the tenant.
- [x] **48j** — `natstrace.End` honours micro's `Nats-Service-Error`
      (BR-055), so a delivered refusal closes its span ERROR and the chain
      reads red end to end. The harness's transport-failure discriminator
      moved to BR-037's `rpc.retry_count` with it.

**Not carried forward as a task, but recorded:** user-level attribution is
out of scope by design. NATS does not put the signing user on a published
message, so it would mean either `$SYS` connz correlation or a publisher
assertion — the spoofable model BR-051 exists to avoid.

---

### Phase 49 — Completed (archived 2026-08-26)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need the original
rationale, the design decisions, or the verification log).

- [x] Phase 49 (IMPLEMENTED 2026-08-26) — **Span timing precision, and a
      waterfall that cannot draw an impossible trace.** Found by inspection
      of a live 3-span trace whose root bar started after its own
      grandchild's.
- [x] **49a** — the wire duration is `durationUs`, microseconds, with no
      millisecond field beside it (BR-056). A span carries no start time, so
      a start is derived as finish minus duration; `Milliseconds()` truncates
      while the finish timestamp is nanosecond-precise, which biased every
      derived start LATE by up to 0.999ms — worst for the longest span, the
      root. Three consumers derived a start independently and were wrong
      together: `TraceWaterfall.vue`, `PulsePanel.vue`, and `otlp-bridge`,
      which exported the same inversion into OTLP. They now share one seam,
      `nats/spanTiming.js`.
- [x] **49b** — the waterfall clamps a child's bar never to start left of
      its parent's, top-down through the tree, with the axis scaled from the
      same clamped geometry (BR-057). 49a removed the *cause* of the
      observed inversion; it cannot make inversion unrepresentable, because
      a trace's spans are stamped by several hosts and a few ms of clock
      skew is ordinary. The clamp moves only children — the parent is the
      causal ancestor, so letting a skewed child pull it left would be the
      worse error — and adjusts geometry only, never the duration a row
      prints.

**Worth keeping:** Phase 28m had already met this defect and fixed only half
of it. It made row ORDER structural (walk the `parentSpanId` tree instead of
sorting by derived start) and left the bar geometry trusting the same
arithmetic it had just declared untrustworthy. The lesson is the shape of
49b: where a derived value is known to be unreliable, every consumer of it
needs the structural defence, not just the one where the symptom was noticed.

---

### Phase 50 — Completed (archived 2026-08-27)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need the original
rationale, the design decisions, or the verification log).

- [x] Phase 50 (IMPLEMENTED 2026-08-27) — **the Admin UI can list NATS users,
      which nothing in the stack could do before.** An operator-mode stack
      stores no users anywhere: minting keeps nothing, the resolver holds
      account JWTs only, and `/connz` knows only who is connected right now.
      So "list users" meant *build a registry*, not *read one*.
- [x] **50a** — the registry (BR-AC38, BR-AC39). `accounts.users` in
      accounts-service's schema, written at mint **before** the credential is
      returned, so a mint path with no registry fails at construction rather
      than issuing a credential nothing can account for. Users minted outside
      the service by `nsc` (`platform`, `sys`, `acme`, `globex`,
      `shipping-admin`, `observability`) converge via a creds-dir backfill on
      start — idempotent, no re-sign, on BR-AC37's model.
- [x] **50b** — the read path (BR-AC40, BR-AC41).
      `api._platform.accounts.user.{list,get}.v1` — **`api.*`, not the
      `rpc.*` the design gate proposed**: the consumer is a browser, and a
      browser credential is never granted `rpc.>`. Mounted on the PLATFORM
      connection only, so "PLATFORM-only" is a server-enforced account
      boundary rather than a handler check. `.get.v1` resolves **effective**
      permissions: under a scoped signing key the key's template is what the
      server enforces and the JWT's own grants are returned separately, to be
      shown struck through.
- [x] **50c** — the panel (BR-060). `UsersPanel.vue`, the `Identity` eyebrow
      group (Accounts + Users, in containment order), live counts joined from
      `/connz` in the browser. Two design-gate proposals were reconciled
      during the build and the reasons recorded in BR-060: Health's `unused`
      state was dropped as connection-derived (the rule's own clause forbids
      Health reading `/connz`) with BR-AC38's `pending` in its place, and the
      "roster never complete" caveat narrowed to the paged `/connz` join,
      since 50a's registry made sessions enumerable.
- [x] **50e** — detail-pane and presentation pass (BR-061, BR-058 amendment).
      **An NKey is never rendered in full anywhere in the Admin UI** —
      `[FIRST5...LAST5]`, one helper and one component, enforced as an
      *absence* (no full key in the render, no `title` carrying one), which is
      what surfaced a fourth hover tooltip nobody had catalogued. Detail panes
      get click-to-copy; table cells don't. A live review then corrected the
      first cut: the key sits *beside* its value rather than stacked, the
      glyph is unspaced, and every column moved to a fixed width — the Name
      column's lone `min-width` had been absorbing all the table's slack,
      which was the real cause of the dead space beside it.
- [x] **50d** — docs. `ARCHITECTURE-ACCOUNTS.md` § "NATS user registry"
      (including the `### Scoped signing keys and effective permissions`
      subsection `ARCHITECTURE-ADMIN.md` had been citing by name since the
      design gate without it existing), and §4.10 + §1's `Identity` group in
      `ARCHITECTURE-ADMIN.md`.

**Worth keeping:** an empty permission set on a NATS user means
*unrestricted*, not *locked out* — a bare `nsc add user` grants everything
within the account, which is why `platform`/`sys`/`acme`/`globex` show no
claims in the drill-in. The pane cannot distinguish that from "nothing
granted", because for a user JWT the two genuinely are the same state.

---

### Phase 51 — Completed (archived 2026-08-27)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need the original
rationale, the design decisions, or the verification log).

- [x] Phase 51 (IMPLEMENTED 2026-08-27) — **Phase 50 made NATS users
      enumerable; this made them actionable.** Prompted by a comparison
      against Synadia's `OPT_IDLE_006` check, which reaches the same
      registry-x-`/connz` join Phase 50 built but treats a disconnected
      credential as an *actionable finding with a revocation remedy* rather
      than as a display state.
- [x] **51a** — connection outcome (BR-062). `/connz?state=closed` added to
      observability-service's monitor read, on its own route rather than as a
      field on the live list: the closed ring is far larger and only this
      panel reads it. A **Last outcome** column carries the server's own
      `ClosedState` vocabulary, so an ordinary TTL expiry is distinguishable
      from a refusal; an absent value reads "outside retained window" rather
      than being silently blank. The join is on the public NKey, never on
      `name` (BR-058).
- [x] **51b** — revocation (BR-AC42, BR-AC43). `Provisioner.RevokeUser`
      writes into the account JWT's revocation list, re-signs, and pushes via
      `$SYS.REQ.CLAIMS.UPDATE`; `revoked_at` added to `accounts.users`;
      `api._platform.accounts.user.revoke.v1` on the PLATFORM connection only.
      **Terminal by design** — there is no un-revoke call to pair with it, and
      recovery from a mis-revocation is minting a replacement, so the
      confirmation dialog names the credential and its live connection count.
- [x] Committed as `ff97953`, together with Phase 52 (see that phase's stub
      for why the two could not be split cleanly).

**Worth keeping:** `jwt.RevocationList.MaybeCompact` prunes only by
**wildcard, never by expiry** — a revocation entry keeps its NKey forever.
That is why Phase 52's reaper exempts a revoked registry row at any age: the
row is the only human-readable mirror of a list that never forgets.

> **One open item**, carried into Phase 62's checklist rather than left in an
> archived file: the live revocation verification. Every credential in the
> running stack is load-bearing and revocation is terminal, so proving it
> end-to-end needs a throwaway credential and a `down -v` reseed to recover.

---

### Phase 52 — Completed (archived 2026-08-27)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need the original
rationale, the design decisions, or the verification log).

- [x] Phase 52 (IMPLEMENTED 2026-08-27) — **the user registry got a ceiling.**
      Every browser connect mints a session row with a 15–30m TTL (BR-AC20),
      and nothing deleted it: **56 rows, 44 of them expired**, after a day of
      uptime. The only sweep that existed ran once at start-up and covered
      `pending` rows only — the mint-crash case, not the ordinary one.
- [x] **BR-AC44** — `Store.ReapExpiredSessions`, a **whitelist** DELETE
      (`kind='session'` AND expired AND `revoked_at IS NULL`), so the failure
      mode of any future edit is a row that outstays its welcome rather than a
      credential that disappears. Credentials are never candidates; a revoked
      row is kept at any age; a `pending` row goes with no grace, subsuming
      the old start-up sweep.
- [x] **BR-AC45** — `SessionReaper` runs once at boot, then on a ticker. A
      failed sweep warns and waits for the next tick (a reaper that dies on
      the first Postgres blip has silently stopped reaping); a **bad config is
      fatal at boot**. Silent when it removes nothing.
- [x] **Retention and cadence are two knobs, not one.**
      `ACCOUNTS_SESSION_RETENTION=24h` is the operator knob — roughly how long
      a row stays *explainable*, since past 24h the bounded `/connz` closed
      ring has rolled over and the row has no outcome left to pair with.
      `ACCOUNTS_REAPER_INTERVAL=15m` is a load knob only. Retention `0`
      **disables** the reaper rather than meaning reap-on-expiry.
- [x] **`hide expired` reversed to default OFF** (BR-060 amended), the table
      now being bounded — which exposed two bugs the old default had masked
      (BR-062 amended): `Authentication Expired` was classed as a *refusal*,
      and since a refusal is deliberately never-idle, `hide idle` was
      structurally unable to hide the largest class of dead rows; and a chip
      reported a held-back count while holding nothing back.
- [x] Docs: the `admin-users-data-flow` diagram gained the reap path as its
      only deleting edge plus a new **Figure D** (the life of one session
      row), and `ARCHITECTURE-ACCOUNTS.md` a `### Session reaping` section.
- [x] Committed as `ff97953`, together with Phase 51 — 52's frontend work is
      edits to 51's *uncommitted* code (the `hideExpired` default, the
      `isRefusal` test), so splitting them would have shipped an intermediate
      commit carrying the two bugs 52 fixes.

**Worth keeping:** the predicate was dry-run as a `SELECT` at five retentions
before anything was deleted — `24h→0, 8h→3, 4h→19, 1h→36, 0→51` — monotonic
and never exceeding the 51 expired sessions, so the credentials and live
sessions are provably untouched at every setting, **including `0`**. The
predicate, not the window, is the safety argument.

**Also worth keeping:** two filters that overlap can hide each other's bugs,
and changing which one runs first is a real test of both. Neither defect above
was visible to a test written against the old default.

> **One deferred item**, carried into Phase 62's checklist: pagination on
> `.list.v1`. It would cost the panel its honest held-back counts, and a
> bounded table no longer needs it — buy it with a re-measurement, not a
> hypothesis.

---


### Phase 53 — Completed (archived 2026-09-03)

Full detail archived in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md)
(not read into context by default — open only when you need the original
rationale, the design decisions, or the verification log).

- [x] Phase 53 (IMPLEMENTED 2026-09-03) — **seven Postgres containers became
      two.** One `postgres` instance for the six owned services, one for
      Temporal. Isolation kept as database-per-service + role-per-service:
      `demos/01-dictionary/postgres/init.sql` creates a role and a database
      per service and revokes `CONNECT` from every other role — verified live
      (six cross-database connects refused, each role sees only its own
      schema). Tenancy untouched (tenant = NATS account, never a Postgres
      object). Host ports 5433–5437 released; organizations' legacy
      `trading_partner` database/role renamed `organizations`; vestigial
      `pgcrypto` extension dropped. Design record:
      [ADR-052](../../obsidian/V3-Platform/Architecture/ADR/lab/ADR-052-lab-data-one-postgres-instance-database-per-service.md).

---


### Phase 54 — DESIGN APPROVED 2026-10-15, REVISED same day after a Claude/Codex review; decisions 15 (c) and 16 (a) chosen by the user 2026-10-15 (business rules BR-064–BR-071 written into BUSINESS_RULES-SHIPPING.md 2026-10-15, specs red; 54a IN PROGRESS) — Replace `natstrace` with OpenTelemetry: SDK → Collector → Tempo, Clients Query Through `observability-service`

> Reverses the deferred decision recorded in ARCHITECTURE-COMMUNICATIONS.md
> § 6 ("Revisit the real SDK if this POC graduates"). Agreed direction
> 2026-10-14, after a Claude/Codex review: the user wants the custom tracing
> pipeline removed, not extended. Design approved 2026-10-15 after a second
> Claude/Codex review of buffering, the client interface and access.
> Revised 2026-10-15 after a third Claude/Codex review (static, not run
> against the stack): decisions 3, 4, 8, 10, 11, 12 and 13 amended;
> decisions 15 (Pulse meaning) and 16 (cutover style) added; the user
> chose **15 (c)** — Pulse waits for Phase 55's metrics — and **16 (a)** —
> one planned cutover, gated on 54c — the same day; BR-071 added. One review finding
> was withdrawn: seeded tenants *can* get a browser token, because startup
> runs `ensureSigningKey` for them (`accounts-service/cmd/main.go`), so the
> 54a path stays browser-driven. Not yet verified on the running stack.

#### Goal

OpenTelemetry owns span creation, context propagation, sampling and export.
The OTel Collector owns ingestion; Grafana Tempo owns storage and query.
`observability-service` is the **only client-facing trace interface**: any
authorized NATS client (the Admin UI first) queries traces over `api.*`
request/reply and never learns Tempo exists. Grafana gives operators deeper
investigation on the same trace ID. The custom envelope, its stream, its KV
projection and the OTLP bridge are retired.

#### Why now — two defects in the current bridge path (verified 2026-10-14)

- **Tenant spans never reach Jaeger.** `otlp-bridge` filters `TRACES` on
  `obs.trace.>` only (`otlp-bridge/cmd/main.go` `tracesSubjectWildcard`),
  but tenant spans land on `monitor.*.trace.>` (BR-AC36, `tracestore.go`
  `TenantSubjectWildcard`). Only PLATFORM spans are exported.
- **Outbound spans carry the callee's service name.** `StartOutbound`
  callers pass the destination (`"refdata"`) as `service`
  (`refdataconsumer/consumer.go`, `refdataclient/client.go`), and
  `otlpmap` uses that as `service.name`. The caller's client span shows
  under the callee.

Neither is fixed in place: this phase removes the code that has them.

#### Design decisions

1. **OTel Go SDK in every service.** `go.opentelemetry.io/otel` API + SDK,
   W3C `traceparent` via the standard propagator. The lab's own glue —
   a `TextMapCarrier` over `nats.Header`, so inject/extract works on NATS
   messages the same way it does on HTTP, plus the policy in decisions 4
   and 7 — lives in one shared package (decision 14), never per service.
2. **Pipeline: SDK → OTLP → Collector → Tempo; Grafana reads Tempo.**
   Jaeger is removed. Collector, Tempo and Grafana join the cell's base
   compose, not an opt-in profile, because the Admin UI now depends on them.
   **Only Grafana is published on the host** (3000, +50 in `au-1`, recorded
   in both `local-*.env` files), with its login on and anonymous access off.
   The Collector (4318) and Tempo (3200) stay inside the Docker network:
   a published Collector would accept forged spans with any tenant
   (decision 4), and a published Tempo would let a client bypass
   `observability-service` (decision 9).
   **Tempo runs in monolithic mode on local disk in the lab** (chosen
   2026-10-14). That is a development setup, not a production one:
   production Tempo 3.x (microservices mode) needs a Kafka-compatible queue
   and object storage, and Tempo is AGPLv3. **The V3 trace store is a
   separate V3 decision**; the alternative already weighed is Jaeger v2 +
   OpenSearch (no Kafka, Apache 2.0, an OpenSearch cluster to run instead).
   Decision 9's gateway keeps that swap to the Collector config and one
   query adapter — no service code changes.
3. **Telemetry leaves the NATS account model.** Services export OTLP over
   HTTP straight to the Collector. Tenant accounts stop exporting
   `obs.trace.>` and PLATFORM stops importing it (BR-AC36's trace half
   retired; BR-AC34's `obs.pubsub.>` half is untouched — ADR-047's wire tap
   is out of scope). **Out of scope means its transport and behaviour, not
   its code:** the `obs.pubsub.*` envelope is published by `natstrace`
   itself (`Tracer.ObservePublish`/`ObservePublishAs`,
   `shared/natstrace/natstrace.go`), so decision 13 must move that
   publisher before deleting the package.
4. **Access: operators read all tenants; tenants read nothing.** (Agreed
   2026-10-15.) The lab has no per-operator tenant scope, so none is
   invented here.
   - *Who may call* is a NATS subject permission: the trace subjects are
     granted only in `MintAdminToken`'s PLATFORM profile and are never
     exported to a tenant account. A tenant credential **cannot reach the
     trace service or read trace data**. The error it sees is not the
     rule: `MintBrowserToken` grants `Pub api.>`, so a tenant publish is
     allowed and returns *no responders*, because nothing in the tenant
     account answers. That passes. An explicit `Pub.Deny api._platform.>`
     is an optional extra policy, not needed to meet this rule.
   - *Which tenant a span belongs to* replaces BR-051, which derived it from
     the server-written arrival subject. With no arrival subject, the
     **receiving service** sets two attributes, kept apart (Codex review
     2026-10-15):
     - the **receiving account** — from the connection the message arrived
       on. Always known. For `refdata-service` this is PLATFORM, because it
       runs on `platform.creds`; that matches BR-051's per-span `_platform`
       attribution today.
     - the **originating tenant** — set only from trusted routing context
       the NATS server writes (54a picks the source and proves **a tenant
       client** cannot forge it), else left empty and shown as
       `unattributed` (BR-054). A PLATFORM connection serves many tenants,
       so the connection alone cannot name one.

     Neither comes from a client-written header, the body or
     `Nats-Requestor`. A header the **server** writes — for example
     `Nats-Request-Info`, which the server adds when the import sets
     `share: true` — **qualifies only after 54a verifies that a client
     cannot spoof or override the value the receiving service reads**: a
     client-sent copy is replaced, not kept beside it, and the service
     reads the server's copy. Naming such a header in BR-069 does not by
     itself give that guarantee; the 54a check does. Until it passes, the
     header is not a source.
   - **The trust model names its attacker: a tenant client.** Platform
     services are trusted, not checked. The import-renamed subject token
     (`rpc.{tenant}.refdata.…`, `provisioner.go` `tenantImports`) is
     server-written only when the message comes through the tenant's
     import. `platform.creds` has no publish limits
     (`nats/bootstrap-operator.sh`), so any PLATFORM service can publish
     `rpc.acme.refdata.…` directly and pick any tenant token. That is
     accepted in the lab because tenants run no code and only lab services
     can reach the Collector (decision 2). In V3 the Collector must
     authenticate senders and platform credentials need limits; both are V3
     decisions.
   - The tenant attribute is **metadata and a query filter, never a
     security boundary**, and the docs must not describe it as one.
   - **Known lab gap, not fixed here:** `GET /api/auth/adminConnectInfo` is
     deliberately ungated (`accounts-service/auth/handler.go`), so anyone who
     reaches accounts-service can obtain an operator credential. This is
     today's exposure too (the trace KV is readable through
     `observability-service`'s ungated `/api/kv/…`). Operator login is
     candidate Phase 109. The 404-vs-403 choice (agreed: 404) applies when
     per-tenant access exists, not in this phase.
5. **Span semantics.** Each process sets its own `service.name`. Spans carry
   a kind: `SERVER` for inbound `rpc.*`/`api.*`, `CLIENT` for outbound
   requests, `PRODUCER`/`CONSUMER` for `evt.*`/`notify.*`, `INTERNAL`
   otherwise. An outbound span belongs to the caller; the destination is an
   attribute. BR-037's "one span per logical call, never per retry" and
   `rpc.retry_count` carry over unchanged.
   **Untrusted trace context at the browser edge (BR-071).** A browser can
   send any `traceparent` — any trace ID, and `sampled = 0`. `natstrace`
   continues it today (`parseTraceparent` in `Start`). So on an inbound
   `api.*` call the SERVER span **always starts a new trace**; an incoming
   `traceparent` is kept only as a **span link** (a reference, not a
   parent). That edge is sampled always-on, not parent-based, so a browser
   cannot turn tracing off for its own calls. Between trusted services
   (`rpc.*`, `evt.*`, `notify.*`) normal parent/child propagation applies.
   Cost, accepted: browser and server trace ancestry are separate traces.
6. **Context propagation through the adapters.** Inbound context is
   extracted at the adapter and passed down; outbound context is injected
   into NATS headers. The domain layer stays framework-free: it may receive
   a `context.Context` (stdlib) but never imports `otel`. Codex's audit
   counted 85 `context.Background()` expressions in adapters, 18 already
   carrying a span — 54a re-verifies that count before relying on it. Not
   every one is wrong; each is examined, not blanket-replaced.
7. **Request and reply bodies are kept, as named span events.** Events
   `rpc.request.body` and `rpc.reply.body` (headers alongside). Capture is
   opt-in per service. Redaction, then truncation, happen in-process
   **before export** — BR-036's denylist and 4 KiB cap carry over, and
   `redacted`/`truncated` become event attributes. This is application code
   and is accepted as such; standard OTel instrumentation does not provide it.
8. **The Traces panel polls instead of being pushed.** It polls the
   decision 9 `api.*` subjects. A push hint is Phase 56, not this phase.
   Poll every few
   seconds while the panel is visible; pause when the page is hidden; show
   the last refresh time; offer a manual refresh. An open trace may fill in
   as late spans arrive. Preserving body inspection outranks preserving push.
   **Visibility delay is measured, not assumed.** The SDK batch delay, the
   Collector and Tempo's ingest each add time before a new trace appears.
   54a records the time from "call made" to "trace returned by
   `trace.search.v1`" and to "returned by `trace.get.v1`"; 54c sets the
   poll interval and the panel's "may be late" wording from that number.
9. **Trace queries are NATS `api.*` request/reply only** (agreed
   2026-10-15; no REST trace routes, so BR-040's allowlist is unchanged):
   - `api._platform.observability.trace.search.v1`
   - `api._platform.observability.trace.get.v1` (resources, attributes,
     events and links preserved)
   - `api._platform.observability.trace.services.v1` (filter values)

   `api.*` per BR-023 (browser-facing; no backend reads traces, so no
   `rpc.*` adapter). One query implementation behind them, with query
   limits and timeouts. `observability-service` calls Tempo over HTTP
   internally; clients never do. **The reply is the lab's own versioned
   schema, never Tempo's passed through** — that contract, not hiding a URL,
   is what makes the Tempo → Jaeger v2 swap a change to one adapter. The
   Admin UI also offers "Open in Grafana" for the same trace ID.
10. **Export never blocks a business call.** SDK batch span processor,
    asynchronous, drop on a full queue (its default). Collector outage
    behaviour is measured in 54a, not assumed, in **two** failure modes:
    *stopped* (connection refused at once) and *hung* (`docker pause` — it
    accepts nothing and answers nothing, the same reason demo 03 cuts with
    `kill -STOP`). A hung Collector holds each export until it times out,
    so `shared/otelnats` sets an explicit export timeout and an explicit
    SDK shutdown timeout, so a service never waits on a dead Collector to
    stop.
11. **Durable buffering moves from JetStream to the Collector; replay is
    given up.** Which component is down decides what protects a span:
    - *Tempo down* — the Collector's sending queue, backed by the
      `file_storage` extension on a named volume (an in-memory queue does
      not survive a Collector restart), with `retry_on_failure` set
      deliberately (the default gives up after 5 min; `max_elapsed_time: 0`
      retries until the queue or disk is full). Sized to hold a **2-hour**
      Tempo outage at measured peak span volume, with headroom (agreed
      2026-10-15). Also agreed: with the Collector down, p95 business
      latency rises **< 5 ms**.
    - *Collector restarting* — the persisted queue survives; spans in
      flight in memory or sent during the restart can be lost.
    - *Collector unreachable from a service* — only the SDK's bounded
      in-memory queue. This is the remaining loss window, measured, not
      closed. A local Collector agent per host would shrink it; not in the
      lab (one cell, one Collector).
    Re-exporting past spans to a new backend is no longer possible;
    accepted, since no requirement asks for it. `TRACES` is not retired
    (54d) until 54a has proved the recovery below.
    **Configuration is not evidence; three separate tests are.**
    - *Durability* — with Tempo down, queued spans survive a graceful
      Collector restart **and** a hard kill (`docker kill`). Proved in 54a.
    - *Capacity* — the queue holds 2 hours of representative peak traffic,
      including body-capture sizes, with headroom. Peak volume needs every
      service instrumented, so this is proved **after 54b**, not in 54a.
    - *Overflow* — fill the queue to full: the loss is visible (a counted,
      logged number, not silent) and business calls are unaffected.
      Proved after 54b, with the capacity test.
    **No in-memory `batch` processor in front of the persistent queue** —
    it is a second loss window on restart. Batch in the exporter instead,
    or, if a `batch` processor is kept, write that window down. **Pin one
    exact Collector version** and run every test against it: an upstream
    issue reports persistent-queue loss on shutdown in v0.157.x
    (open-telemetry/opentelemetry-collector#15677). That does not prove
    this setup is hit; it is why the tests, not the config, are the
    evidence.
12. **Sampling stays 100% in the lab.** Between trusted services it is
    parent-based always-on; at the browser edge it is always-on with a new
    root (decision 5, BR-071). Parent-based alone is not 100%: the Go SDK
    honours an unsampled remote parent, so a browser could opt out. The
    Pulse tab's trace-derived counts are only correct while nothing is
    sampled out — and only as complete as the spans that reached Tempo
    (decision 11). After the KV feed goes, Pulse waits for Phase 55
    (decision 15 (c)).
13. **Retired at the end:** `shared/natstrace`, the `obs.trace.*` envelope,
    the `TRACES` stream, the `trace-request-reply` KV bucket and its
    `tracestore` projector, `otlp-bridge`, and the Admin UI's KV-watch
    trace feed (`useTraceFeed.js`). **Two things depend on these and must
    move first:**
    - *The Messages wire tap's publisher.* `ObservePublish`/
      `ObservePublishAs` move to a surviving shared package
      (`shared/otelnats` is the default home, not a requirement) and take
      the trace and span IDs from the OTel span context, so a Messages row
      still links to its trace. BR-045–BR-047 keep their behaviour; only
      their "Enforced in" paths change. Callers today: `shared/jstream`,
      `shared/natsnotify`.
    - *Every reader of `useTraceFeed.js`*, not only the Traces panel:
      `TraceWaterfall.vue`, `PulsePanel.vue` and `RpcPanel.vue`'s
      `[messages]` tab. All three move in 54c.
14. **One thin shared package, `shared/otelnats`** (agreed 2026-10-15
    after a Codex review). `natstrace` was five per-service copies until
    Phase 34 merged them; OTel must not repeat that. The split:

    | Responsibility | `shared/otelnats` owns | The service supplies |
    |---|---|---|
    | SDK lifecycle | one init helper (exporter config, resource) and one shutdown helper | its `service.name`, its config, and the init/shutdown calls in its own `main.go` |
    | NATS propagation | standard OTel inject/extract through `nats.Header` | the request `context.Context` |
    | Body capture (decision 7) | opt-in capture, redact-then-truncate, the span-event format | whether capture is on |
    | Tenant attribution (decision 4) | the attribute names and the rules for setting them | the trusted account and routing context from its receiving adapter |

    - **Thin.** Standard OTel providers, tracers, propagators and span
      kinds — not a new in-house tracing framework. Domain code still gets
      a plain `context.Context` and never imports the package.
    - **Wired into the existing shared seams** (`shared/browserrpc`,
      `shared/jstream`, `shared/natsnotify`, `shared/natstenants`) where
      they already call `natstrace`, to remove repetition.
    - **No promise that service code stays unchanged.** A helper cannot
      repair a dropped context or choose a span's boundaries. Decision 6's
      adapter context audit still runs, service by service.
15. **Pulse waits for Phase 55's metrics** (chosen by the user
    2026-10-15, option (c)). Today `PulsePanel.vue` counts the whole live
    trace set from `useTraceFeed.js`. Decision 9's search has a result
    limit, so a capped search is **never** shown as a total count.
    - From the cutover (decision 16) until Phase 55 ships, the Pulse tab is
      **marked unavailable** — it says why and points to Phase 55. It never
      shows numbers from a capped search. The tab stays, so the
      explanation card and flow diagram (Phase 44) are kept.
    - `ui.rpcTab`'s default moves off `'pulse'` for that period, so the
      panel does not open on an empty tab.
    - Phase 55 decision 10 rebuilds Pulse on Prometheus. 54's finish
      therefore leaves a known gap that 55 closes.
    - Not chosen: (a) keep the time window with a new count/aggregate query
      in the gateway — more 54c work for a panel 55 replaces anyway; (b)
      "the last N traces" from one capped search — changes the feature's
      meaning.
16. **One planned cutover, gated on 54c** (chosen by the user 2026-10-15,
    option (a)). 54a moves `refdata-service`, which every caller uses.
    Propagation already interoperates (`natstrace` speaks W3C
    `traceparent`), but storage does not: run mixed, the Admin UI (KV feed)
    misses OTel spans and Tempo misses `natstrace` spans, so a trace
    through `refdata-service` is half in each store. So it never runs
    mixed on the default stack:
    - 54a, 54b and 54c are built and tested off the default stack (a
      branch, or one compose switch). The default stack keeps `natstrace`,
      `TRACES`, the KV feed and Jaeger throughout.
    - **The gate is 54c complete and validated** — not 54c merged. That
      means every 54a/54b test green on the OTel stack, plus the Admin UI
      checks in 54c (Traces, `[messages]`, Messages-to-trace link, Pulse
      marked unavailable) passed against it.
    - Then one switch moves every service and the UI together.
    - **Nothing old is deleted before the gate.** No code, stream, bucket,
      export/import, `otlp-bridge` or Jaeger is removed until the switch
      has been made and checked; that removal is 54d.
    - **History is not migrated.** Tempo starts empty at the switch. Spans
      from before it stay only in the old stores — `trace-request-reply`
      (15 min) and `TRACES` (1 h), PLATFORM spans in Jaeger — which the new
      UI does not read, and which go in 54d. Accepted: this is a diagnostic
      window, and decision 11 already gave up replay.
    - Not chosen: (b) move services one at a time and accept split traces
      for 54a → 54c; dual-writing both stores from `otelnats` — more code to
      delete later, in a lab.

17. **NATS micro discovery and live statistics remain part of Phase 54**
    (user requested 2026-10-15; BR-072). Build on the existing
    `observability-service` Services endpoint and Admin Services panel:
    `$SRV.STATS` already discovers PLATFORM and tenant instances through
    BR-AC31's `monitor.{tenant}.srv.>` imports. Preserve these imports when
    retiring the trace imports; preserve BR-027 instance identity and
    BR-028 account labels.
    - Use micro PING for responsiveness, INFO for identity/version and
      endpoint descriptions, and STATS for live request/error counts,
      last errors and processing times. Collect multiple replies in a
      bounded discovery window; retain distinct instances and account
      provenance. Do not infer service availability from trace arrival.
    - A response means **responding**, not dependency-ready. A missing
      response means **not observed within the discovery window**, not
      proof the process is dead. Failed/partial discovery must be visible
      rather than presented as a successful empty inventory. Where a
      readiness check exists, reuse `shared/natsready` (BR-AS62) and show
      its result separately; otherwise readiness is unknown. Do not add
      a second dependency-readiness framework in this phase.
    - **Client transport is NATS request/reply only** (user approved
      2026-10-15): `api._platform.observability.service.list.v1` returns
      a versioned snapshot of instances, identity, endpoint statistics,
      observation time, discovery errors/partial results and readiness
      where supported. One transport-independent discovery implementation
      behind the adapter uses PING/INFO/STATS internally; clients do not
      call raw `$SRV.*` subjects or choose arbitrary discovery subjects.
    - Grant the endpoint explicitly to operator credentials through
      `MintAdminToken` and the observability responder credentials. Do not
      export it to tenants or grant browsers `$SRV.>`/`monitor.*.srv.>`.
      Apply decision 4's operator access model and known login gap.
      Bound discovery time and response size; expose truncation explicitly.
    - Switch the Services panel to this endpoint in 54c. Keep
      `GET /api/nats/services` on the old stack until the coordinated
      cutover; remove that route in 54d, including its docs and BR-040
      allowlist entry. Other existing diagnostics routes are unchanged.
      Live micro statistics do not replace Phase 55's historical metrics
      or restore Pulse early.
    - Complete and validate Phase 54 locally before merging/transferring
      selected work to Chronicle, the intended Linebooker V3 codebase.
      The eventual target supports both local Docker and AWS deployments;
      service registration and discovery semantics must be portable, with
      topology, credentials and discovery timing supplied by deployment
      configuration. AWS deployment and the Chronicle merge are later
      work, not Phase 54 completion claims.

#### Slicing

- **54a — one cross-account request, end to end.** Path: browser `api.*`
  → `organizations-service` (tenant account) → `rpc.*` → `refdata-service`
  (PLATFORM), via `organizations/internal/refdataclient`. Builds
  `shared/otelnats` (decision 14) **alongside this one path**, so the
  package is proved by real use before 54b spreads it. Instrumented with
  OTel only: correct parent/child links and kinds, the caller's
  `service.name` on the client span, both tenant attributes per decision 4
  (receiving account; originating tenant from a trusted source, or empty),
  and retrieval through `api._platform.observability.trace.get.v1`.
  Driven by a real browser token for a seeded tenant (startup gives seeded
  tenants a signing key; confirm on the running stack first). Built and
  tested off the default stack (decision 16). Proves:
  - outage tests (decision 11 — durability only; capacity and overflow
    come after 54b). (1) *Durability:* stop Tempo, send known traces,
    restart the Collector gracefully while Tempo is still down, start
    Tempo: every known span arrives. Repeat with a hard kill
    (`docker kill`) instead of a restart. (2) *Collector stopped:* the
    business call still succeeds, p95 rises < 5 ms, and the SDK's loss
    window is measured and recorded. (3) *Collector hung* (`docker pause`):
    the same three things, plus a service stops within its shutdown
    timeout. All on the one pinned Collector version.
  - four access checks. (1) An operator credential reads traces from every
    tenant. (2) A tenant credential cannot reach the trace service or read
    trace data: *no responders* or a permission error passes, a reply with
    trace data fails. Run with both a tenant browser token and the tenant's
    `.creds`. (3) Tempo and the Collector are not reachable from the host;
    Grafana requires a login. (4) No doc calls the tenant attribute a
    security boundary.
  - the trust checks. (1) A tenant client cannot set the originating
    tenant (decision 4); the doc names "tenant client" as the attacker. If
    a server-written header is the source, a client-sent copy of it is
    replaced and cannot change the attribute. (2)
    A browser-sent `traceparent`, sampled or not, does not become a parent:
    the `api.*` SERVER span starts a new trace with a link (BR-071).
  - the visibility delay (decision 8), recorded as a number.

  Sets the pattern. Build order:
  - [x] **54a-1** — BR-064–BR-071 written; failing specs in
        `shared/otelnats` (BR-064/065/066/069/071),
        `observability/internal/tracequery` (BR-067/070) and
        `accounts-service/auth/token_test.go` (BR-067). Skeletons only.
  - [x] **54a-2** — `deploy/cell/compose.otel.yaml` overlay: pinned
        Collector (`file_storage` queue, no `batch` processor), Tempo
        (monolithic), Grafana with login. Only Grafana on the host.
        Pins: otelcol-contrib 0.162.0, tempo 3.1.0, grafana 13.2.4.
        Smoke-tested on `au-1` 2026-10-15: a span sent to the Collector
        read back from Tempo; Grafana 401 without login; Tempo not
        resolvable from `backend`. Found: Tempo's v2 reply carries IDs as
        base64, not hex — the BR-070 fixture is now that real reply.
  - [x] **54a-3** — implement `shared/otelnats`; its specs green. Done
    2026-10-15: 26 of 26 specs green, also with `-race` over five runs.
    Choices: the provider never sets the OTel globals; the exporter has
    no retry (durability is the Collector's queue, BR-068); `Endpoint` is
    the Collector base URL and `/v1/traces` is added; `HeaderCarrier.Get`
    matches a key in any case, because `natstrace` writes `Traceparent`.
  - [x] **54a-4** — the fleet-asset path (`organizations-service`) and
        refdata `item.get` use `otelnats` when the overlay is on. Done
    2026-10-15. The switch is `otelnats.InitFromEnv`: only
    `compose.otel.yaml` sets `OTEL_EXPORTER_OTLP_ENDPOINT`; unset gives a
    nil Provider and every path stays on `natstrace`. New
    `otelnats.Middleware` wraps a micro handler; the span ends just before
    the reply goes out, and a `Nats-Service-Error` reply sets ERROR.
    `refdataclient` makes one CLIENT span per logical call, with
    `rpc.retry_count`. Trust: `acme` (the adapter's tenant) on api.*,
    `PLATFORM` on refdata; the originating tenant stays empty until 54a-6.
    Specs: otelnats 36, organizations and refdata suites green; both
    modules vet clean with `GOWORK=off`. Smoke on `au-1`: one fleet-asset
    add gave one trace — api SERVER → CLIENT → refdata SERVER — seen in
    Tempo about 18 s later. Known gap until 54b: other callers of
    `item.get` (shipping, pricing) send `natstrace`'s `Traceparent`, which
    refdata now continues on OTel, so their traces are split.
  - [x] **54a-5** — `trace.get` gateway in `observability-service`, its
        NATS user permissions, the `MintAdminToken` grant; specs green.
    Done 2026-10-15. `tracequery.Mount` adds a micro service on the
    PLATFORM connection, only when `TEMPO_URL` is set (only
    `compose.otel.yaml` sets it). `trace.get` maps Tempo's v2 reply to
    `lab.trace.v1`: base64 IDs become hex, typed attribute values are
    unwrapped. Errors: 400 bad body or ID, 404 unknown trace, 503 Tempo
    failure without Tempo's text. `trace.search` and `trace.services` are
    served but reply 501 until 54c specifies their shape. `MintAdminToken`
    names the three subjects (BR-AC18 amended); the observability user
    subscribes on them and publishes on `_INBOX.>` for replies
    (`bootstrap-operator.sh`). Specs: tracequery 13, accounts and auth
    suites green; observability-service vets clean with `GOWORK=off`.
    Process slip: the 404/400/501 specs were written with the code, not
    run red first.
    `nats/creds/observability.creds` was re-signed, not regenerated: the
    nsc store is not on disk, so a one-off tool kept the same user NKey and
    name, set the grants from `bootstrap-operator.sh`, and signed with the
    PLATFORM signing key (`nats/keys/platform-signing-key.nk`). No
    `down -v`; `za-1` is untouched. A later `--force` run gives the same
    grants, signed by the account identity key instead.
    Smoke on `au-1`, PLATFORM creds: `trace.get` for the 54a-4 trace
    returned `lab.trace.v1` with all three spans (hex IDs, typed attributes
    unwrapped); unknown ID 404; `../../api/search` 400; `trace.search` 501.
    `acme.creds`: no responders. No permission errors in the service log.
    Not yet run with a minted admin browser token; that is 54a-6's access
    check.
  - [x] **54a-6** — stack checks: access, trust, outage/durability,
        visibility delay.
    Done 2026-10-15 on `au-1`, Collector 0.162.0, Tempo 3.1.0. Driver:
    `observability-service/cmd/otel-stackcheck` (see "How to rerun"
    below), which fetches real browser tokens from accounts-service, holds them in memory only, and
    finds each trace in Tempo by the link BR-071 makes from a sent
    `traceparent`. Tempo search ran through Grafana's proxy, because
    `trace.search` replies 501 until 54c. All fleet-asset calls used a
    vehicle type refdata does not know, so every trace is the 3-span
    ERROR shape.
    - **Access.** (1) PASS: an admin browser token read an acme and a
      globex trace through `trace.get` (3 spans each, the right
      `receiving_account`). First run failed: the running accounts-service
      was the pre-54a-5 build; a rebuild fixed it. (2) PASS: acme and
      globex browser tokens and both `.creds`: all three subjects give
      *no responders*. (3) PASS: Tempo and the Collector publish no host
      port; Grafana answers 401 without login. (4) PASS: the only docs
      that mention a security boundary say the tenant attribute is not one.
    - **Trust.** (1) PASS: an acme call with spoofed `Nats-Requestor`,
      `Nats-Request-Info`, a `tenant` header and `tenant` body fields
      naming globex: `receiving_account` = acme, `originating` absent.
      (2) PASS: a sent `traceparent` with flags 00 and with 01 both gave
      a new root (no parent, new trace ID), one link to the sent ID, and
      the 00 trace was still recorded.
    - **Outage (BR-066).** Baseline 300 calls p95 2.66 ms. Collector
      stopped: 300 calls, 0 errors, p95 1.23 ms; 0 of 300 traces arrived.
      Collector hung (`docker pause`): 300 calls, 0 errors, p95 1.45 ms;
      94 of 300 arrived after unpause. With the Collector hung,
      organizations-service stopped in 2.5 s and refdata-service in
      0.26 s, both exit 0 (limit: 5 s shutdown timeout). The SDK drops a
      batch whose export fails (no retry, by design), so the loss window
      is the Collector's down time plus up to one 5 s batch interval: 50
      calls sent in the second of a Collector restart were all lost; 50
      sent 15 s after it all arrived.
    - **Durability (BR-068).** Tempo stopped, 100 known traces sent and
      handed to the Collector, then (a) `docker restart` of the
      Collector, (b) in a second run `docker kill`; Tempo started: 100 of
      100 traces, each with all 3 spans, in both runs. On the graceful
      restart the Collector logs "Exporting failed. Dropping data …
      interrupted due to shutdown" for every in-flight item, yet nothing
      was lost: the persistent queue still held them. Do not read that log
      line as loss on this version.
    - **Visibility delay (decision 8).** 8 runs, call to "found by Tempo
      search": min 30.4 s, p50 31.0 s, max 31.1 s. `trace.get` returned all
      3 spans within 0.1 s of the search finding the ID, so get delay ≤
      search delay. The 54a-4 smoke saw about 18 s; that was a single,
      unpolled reading. Tempo and the Collector run their default timings;
      what sets the 31 s is not yet proved. `trace.search` itself is
      unmeasured until 54c builds it.
    - **For 54c.** Tempo's TraceQL matches `link:traceID` only without
      leading zeros (`00ea51…` misses, `ea51…` hits); `trace.search` must
      strip them. The poll interval and "may be late" wording start from
      the 31 s above.
    - **How to rerun.** Bring a cell up with `-f compose.otel.yaml`, then
      run from the repo root. Defaults are za-1's ports; for au-1 add
      `-accounts http://127.0.0.1:7252 -nats-ws ws://127.0.0.1:9322
      -nats nats://127.0.0.1:4322 -grafana http://127.0.0.1:3050`. Set
      `GRAFANA_ADMIN_PASSWORD` if it is not `admin`. `access` and `trust`
      exit 1 on any FAIL; rerun 2026-10-15 on au-1: `access` PASS.

      ```bash
      go run ./demos/01-dictionary/backend/observability-service/cmd/otel-stackcheck access
      go run ./demos/01-dictionary/backend/observability-service/cmd/otel-stackcheck trust
      go run ./demos/01-dictionary/backend/observability-service/cmd/otel-stackcheck delay 8
      ```

      Outage: `send 300 <batch>` with the Collector stopped or paused,
      then `waitcount <batch> 300 120`. Durability: stop Tempo,
      `send 100 <batch>`, restart or kill the Collector, start Tempo,
      then `waitcount <batch> 100 180` and `verify <batch>`. `<batch>` is
      any 32-hex trace ID; it becomes every call's span link.
- **54b — remaining services**, one at a time, following 54a's pattern and
  using `shared/otelnats` — no service-local copy of its four jobs. Each
  service gets decision 6's context audit. Moves the `obs.pubsub.*`
  publisher off `natstrace` (decision 13). Ends with the capacity and
  overflow tests (decision 11), now that peak volume can be measured.
  Includes a micro registration inventory and BR-072 integration checks:
  PLATFORM plus tenant PING/INFO/STATS, two instances of one service,
  successful and failed endpoint calls reflected in STATS, stop/restart,
  dependency failure while NATS still responds where readiness is supported,
  and discovery failure/partial-result reporting. Exercise the versioned
  `api._platform.observability.service.list.v1` endpoint with operator and
  tenant credentials: operators receive the snapshot; tenants cannot reach
  the responder/read the data; browser credentials cannot query raw micro
  controls. Run again with Collector
  and Tempo unavailable to prove discovery is independent of tracing.

  Build order (approved 2026-10-15). Each step ends with its specs green
  and its own commit. Off the default stack throughout (decision 16).
  - [x] **54b-1** — failing specs first: the shared seams
        (`shared/browserrpc`, `shared/jstream`, `shared/natsnotify`,
        `shared/natstenants`) on `otelnats`; the `obs.pubsub.*` publisher
        in its new home, taking IDs from the OTel span context
        (BR-045–BR-047); `service.list` (BR-072). Run red.
        Done 2026-10-15. Red run, every failure on an assertion:
        - `shared/otelnats`: 74 specs, 40 pass, 34 fail — `StartProducer`
          / `StartConsumer` (BR-037), body events (BR-036, decision 7,
          added here because it shares redaction with the publisher) and
          `Observer` (BR-045–BR-047). The publisher's new home is
          `otelnats.Observer` (decision 13).
        - `shared/jstream` 4 red (`Publish(ctx, …)` and `WithOTel`; the
          natstrace fallback too), `shared/natsnotify` 3 red,
          `shared/natstenants` 3 red (`SubscribeLifecycle(…, WithOTel)`,
          one CONSUMER span per delivery), `shared/browserrpc` 1 red
          (`SpanContext` on a wrapped request). browserrpc's two reply
          status specs pass already — `otelnats.Middleware` reads the
          reply headers — and stay as guards.
        - `observability/internal/servicelist` (new): 16 specs, 15 red.
          The bounded-window spec passes on the skeleton and stays as a
          guard. Real two-account server, ACME's `$SRV.>` imported as
          `monitor.acme.srv.>`. Readiness reuses `shared/natsready`;
          tenant names come through a `TenantSource` that returns an
          error, because `AccountsClient.TenantNames` hides failure.
        - `accounts-service/auth`: 2 red — `MintAdminToken` gains
          `service.list.v1`, and no `$SRV.*` / `monitor.*` subject.
        - All other existing specs in the touched modules stay green.
        Wiring: the four seams and the five services that lacked it gain
        the `otelnats` replace; Dockerfiles copy `shared/otelnats`.
        `accounts-service` and `mfe-registry-service` also gain the
        missing `shared/natstest` replace (an old gap: `go mod tidy`
        failed there before this step). All seven services build.
  - [x] **54b-2** — the shared seams and the `obs.pubsub.*` publisher move
        to `otelnats`; their specs green.
        Done 2026-10-15. All green:
        - `shared/otelnats`: 74/74. New `redact.go` (natstrace's denylist,
          redact and truncate, ported unchanged). `StartProducer` /
          `StartConsumer`, body events with `Middleware` capture, and
          `Observer` implemented.
        - `shared/jstream` (`Publish(ctx, …)`: OTel with `WithOTel`, else
          the natstrace span on ctx), `shared/natsnotify`,
          `shared/natstenants` (one CONSUMER span per lifecycle delivery)
          and `shared/browserrpc` (`SpanContext` returns the SERVER span's
          ctx when wrapped) all green.
        - `shipping-service` and `refdata-service` suites green; all seven
          services build.
        Two 54b-1 spec mistakes fixed, not the code: `body.bytes` for the
        5000-byte pad is 5010, not 5011; and BR-046's `payloadBytes` is the
        size after redaction (4080), as natstrace sends it, not before
        (decision 13: behaviour unchanged). That spec now also checks the
        payload is not truncated and does not hold `actorName`, which is
        what proves redaction ran first.
  - [x] **54b-3** — the rest of `refdata-service` and
        `organizations-service`, with decision 6's context audit. Split in
        two on 2026-10-15 (user's choice), so each service has its own
        test and commit boundary. Each substep ends with its specs and its
        service suite green.
    - [x] **54b-3a** — the shared `natstenants.NewManager` fix (it calls
          `SubscribeLifecycle` with no options, so it cannot pass
          `WithOTel` today), then the rest of `refdata-service`: the 32
          api.* endpoints (tenant and PLATFORM trust), the 4 remaining
          rpc.* endpoints, the `kvcache` evt.* publisher, the notify.*
          fan-out, and a CONSUMER span in `notifybridge`. Context audit:
          the 6 `context.Background()` handler contexts in
          `natsrpc/adapter.go`.
          Done 2026-10-15. Specs first: 13 new specs ran red on their
          assertions (natstenants 1, refdata Ginkgo 10, refdata `testing` 2),
          all 166 old refdata specs green. Then all green:
          - `shared/natstenants`: `NewManager(…, opts ...ManagerOption)`
            and `WithTracing(p)`; each tenant's lifecycle subscription gets
            `WithOTel(p, tenant)`.
          - `refdata-service`: `refdata.Startup(…, WithTracing(p))` (so
            `cmd/main.go` runs `otelnats.InitFromEnv` before `Startup`);
            `MountRPC(nc, js, log)` drops its tracing param. Every rpc.*
            endpoint on `EdgeRPC` (PLATFORM); every api.* endpoint on
            `EdgeAPI`, receiving account = the tenant, or PLATFORM on the
            `_platform` mount. `kvcache.Publisher` is now
            `Publish(ctx, subject, data)` on `jstream.WithOTel`.
            `notifybridge.WithOTel` adds the CONSUMER span; the notify.*
            fan-out is on `natsnotify.WithOTel`.
          - Context audit: the 6 `context.Background()` sites in
            `natsrpc/adapter.go` now start from `otelnats.ContextFrom(req)`.
            Each is a request-scoped handler ctx.
          - `refdata-service`, `shipping-service`, `pricing-service`,
            `organizations-service` suites green; all seven services build.
          Not tested directly: that the rpc.* handler ctx is the SERVER
          span (covered by the code change only). The Temporal gap below
          is still open.
    - [x] **54b-3b** — the rest of `organizations-service`: the 26 api.*
          endpoints, `JetStreamEventStore` on `otelnats.Observer`, a
          CONSUMER span in the `TRANSPORTER` projector. Context audit: the
          35 `context.Background()` handler contexts in
          `browserrpc/adapter.go` that drop the span, plus the projector
          and `composition.go` sites.
          Done 2026-10-15. Specs first: 10 new specs ran red on their
          assertions (orchestration `testing` 4, organizations Ginkgo 6).
          Two 54a-4 specs assumed only fleet-asset add was on OTel; they now
          pick its SERVER span by name. Then all green:
          - api.*: every endpoint on `EdgeAPI` when `Deps.Tracing` is set;
            `otelSubjects` deleted. Receiving account = the adapter's tenant.
          - Context audit: all 35 handler sites now use
            `sharedbrowserrpc.SpanContext(req)` (the SERVER span with OTel,
            the natstrace span without). Each is a request-scoped handler
            ctx. `isGitDocument` takes a ctx. On the natstrace path this
            also means the span now reaches the command layer, where it did
            not before.
          - `JetStreamEventStore`: `WithOTel(p)` makes `append` a PRODUCER
            span with its traceparent on the message, Error status on a
            failed or rejected append, and the observation from
            `otelnats.Observer` (set by `WithObservation`) after the PubAck.
          - Projector: `WithOTel(p, tenant)` consumes under a CONSUMER span
            (receiving account = tenant). Its 4 `context.Background()` sites
            (hydrate, projection, cache, certificates) now use that delivery
            ctx.
          - Wiring: `transporterprofile.Start(…, WithTracing(p, tenant))`;
            `tenants.NewManager(…, tracing)` replaces the `WithTracing`
            builder, because the shared manager is built inside
            `NewManager` and now gets `natstenants.WithTracing(tracing)`.
          - Kept as is: `composition.go`'s one `context.Background()`
            (`DeleteGitMonitorSchedules`, a startup cleanup) and
            `dropHydrationConsumer`'s timeout ctx. Neither is
            request-scoped.
          - `organizations-service` suite green (202/202 Ginkgo specs in
            `organizations`); `shipping-service` suite green; all seven
            services build.
          Not tested directly: the wiring in `transporterprofile.Start` and
          `tenants.NewManager` (both need Postgres to start a tenant; the
          options they pass are tested where they are defined). The 30
          `internal/postgres` specs skip without
          `ORGANIZATIONS_TEST_DATABASE_URL`; this step did not touch that
          package. The Temporal gap below is unchanged.
    - **Known gap — the Temporal hop is not traced.** Temporal has no OTel
      interceptor here (`client.Dial` in `organizations/composition.go`,
      `temporalworker.Options{}` in `worker/worker.go`). Affected path: an
      api.* handler starts or signals a workflow (`ExecuteWorkflow`,
      `SignalWorkflow` in `worker/worker.go`) → an activity
      (`AppendWorkflowEvent` in `activities.go`) → `JetStreamEventStore.append`.
      The activity's ctx holds no span, so that evt.* publish carries no
      `traceparent`, and its `obs.pubsub.*` envelope starts a new root
      trace. The trace of the api.* call ends at the Temporal call. 54b
      does not describe this path as traced.
      **Decided 2026-10-15: an accepted limitation.** It is written into
      BR-064's scope note (`BUSINESS_RULES-SHIPPING.md`). Phase 54 adds no
      Temporal OTel interceptor. A later phase that needs continuity
      across Temporal adds the interceptor with its own rule and tests.
  - [x] **54b-4** — `shipping-service`. Closes 54a-4's split-trace gap
        for shipping's `item.get` calls.
        Done 2026-10-15. Specs first: 16 new specs ran red on their
        assertions (`testing` 3: refdataconsumer 2, rest wiring 1;
        dictionary Ginkgo 13). One guard spec (no tracing → natstrace)
        passed from the start. Then all green:
        - api.*: every endpoint on `EdgeAPI` when `Deps.Tracing` is set.
          Receiving account = the adapter's tenant. All 9 handler ctx
          sites use `sharedbrowserrpc.SpanContext(req)`.
        - evt.*: `commands.Publisher` is now ctx-only
          (`Publish(ctx, subject, data)`); the tenant publisher gets
          `jstream.WithOTel(p)`, so the evt.* PRODUCER span is a child of
          the SERVER span.
        - Projectors: `eventhandler.WithTracing(p, tenant)` on all three
          (ships, container, meta). Each message is a CONSUMER span
          (receiving account = tenant, `consumer.durable` set); its
          `notify.*` publishes are PRODUCER spans under it. New file
          `eventhandler/tracing.go`.
        - notify.*: `kvstore.EnableNotify` and the adapter's notifier take
          `natsnotify.WithOTel(p)`.
        - `refdataconsumer`: `WithTracing(p)` makes each `item.get` one
          CLIENT span (`peer.service` refdata, `rpc.retry_count`, Error
          status on no answer or a service-error header) with its
          traceparent on the request.
        - Wiring: `natstenants.WithTracing` in `rest.NewHandlers`;
          `dictionary.Module{Tracing}`; `cmd/main.go` calls
          `otelnats.InitFromEnv(ctx, "shipping-service")`;
          `deploy/cell/compose.otel.yaml` sets shipping-service's
          `OTEL_EXPORTER_OTLP_ENDPOINT`.
        - **Finding:** `internal/refdataconsumer` (the only `item.get`
          caller) has no production caller. Only its tests use it. So no
          live split trace exists today. It gets the CLIENT span anyway,
          so a future caller is traced.
        - Kept as is: REST `trace_middleware.go` stays on natstrace (it
          covers only the infra routes `/api/admin/ports`, `/api/tenant`,
          `/api/tenant/switch`; it retires in 54d).
          `RegisterRefdataNotify` (the PLATFORM bridge) has no tracing.
          `commands.dropConsumer`'s detached 5 s ctx is not request-scoped.
        - `shipping-service` `go test ./...` green; dictionary Ginkgo 105
          passed, 0 failed, 13 pending (the old Phase 43a `PDescribe`, not
          this step). All seven services build.
        Not tested directly: natstenants lifecycle tracing through
        `NewHandlers` (tested where the option is defined) and the
        `cmd/main.go` `InitFromEnv` call. The Temporal gap above is
        unchanged; it is now an accepted limitation (BR-064).
  - [x] **54b-5** — `pricing-service`, `mfe-registry-service` and
        `accounts-service`.
        Done 2026-10-15. Specs first, red on the missing API (`Deps.Tracing`,
        `registry.Option`, `Handlers.Tracing`, `Provider.HTTPMiddleware`).
        Then all green:
        - New shared piece: `otelnats.Provider.HTTPMiddleware` (BR-073). It
          is the standard `otelhttp` handler on our Provider with a thin
          policy on top: every request is a public endpoint (new sampled
          root, a valid sent `traceparent` as one link), receiving account
          from the caller of the function, body capture redacted (BR-036).
          `otelhttp` v0.72.0 has no `WithPublicEndpoint`;
          `WithPublicEndpointFn(func(*http.Request) bool { return true })`
          is the same. Error status is 5xx only (OTel convention; natstrace
          used 400 and up). The `shared/telemetry` extraction stays
          deferred.
        - pricing: all 33 api.* endpoints on `EdgeAPI` when `Deps.Tracing`
          is set; receiving account = the adapter's tenant; all 33 handler
          ctx sites now use `sharedbrowserrpc.SpanContext(req)` (were
          `context.Background()`). `MountAPI(..., tracing)` also passes
          `natstenants.WithTracing`. `cmd/main.go` calls `InitFromEnv`.
        - mfe-registry: `registry.Startup(..., opts ...Option)` with
          `WithFetchOrigins` and `WithTracing` (the old variadic
          `...domain.FetchOrigins` is gone; two callers updated). api.* on
          `EdgeAPI`, the two rpc.* endpoints on `EdgeRPC`, both on
          `PLATFORM`; the notifier gets `natsnotify.WithOTel`.
        - accounts: `WithUsersTracing(p)` on the Users adapter (`EdgeAPI`,
          `PLATFORM`); `Handlers.Tracing` puts the `notify.accounts.*`
          publishes on OTel; `TraceHTTP(mux, tracing, legacy)` picks the
          REST tracing — OTel with a Provider, natstrace without
          (decision 16: the legacy middleware stays on the default stack
          until the coordinated cutover).
        - `deploy/cell/compose.otel.yaml` sets the endpoint for all three.
          `docker compose ... config` with the overlay and
          `global/compose.control.yaml` merges cleanly.
        Results: otelnats 83/83; pricing 79/79; mfe-registry 492/492, 0
        skipped (Postgres fixture ran); accounts suite 165/165; shipping
        dictionary 105 passed, 13 pending (the old Phase 43a `PDescribe`);
        natsnotify and natstenants green. All 8 backend modules build.
        `auth` suite: 41 passed, 2 failed — the two `MintAdminToken` specs
        for `observability.service.list.v1`, committed red in 54b-1 for
        54b-6. Not this step.
        One spec fix: the accounts REST spec read `Traceparent` from the
        notify message. OTel writes the W3C key in lowercase and
        `nats.Header` is case-sensitive, so the spec now reads it through
        `otelnats.HeaderCarrier`.
        BR-AC35 on the OTel path: `service.name` is the responder; the
        requester's `Nats-Requestor` is only in the redacted headers of the
        body event, so only with capture on.
        Not tested directly: the three `cmd/main.go` `InitFromEnv` calls;
        no stack run on `au-1` yet (54b-7).
  - [x] **54b-6** — `api._platform.observability.service.list.v1` in
        `observability-service`, its responder grants and the
        `MintAdminToken` grant (BR-AC18).
        Done 2026-10-15. All green:
        - `observability/internal/servicelist`: 16/16 (green 5 runs in a
          row). INFO and STATS in one window across PLATFORM and every
          tenant import, joined by instance ID; readiness from
          `shared/natsready` in a second window, PLATFORM only; fixed error
          text; defaults 500 ms and 200 instances. Mounted in
          `composition.go` on every stack, Tempo or not.
        - `rest.AccountsClient.Tenants` keeps the failure `TenantNames`
          hides; `TestTenantsKeepsTheFailure`. observability-service: all
          6 suites green.
        - `accounts-service/auth`: 43/43 — the 2 specs left red since 54b-1
          now pass. Accounts suite 165/165 (3 runs; one earlier run had a
          setup timeout in `provisioner_test.go`, unchanged code, and did
          not repeat).
        - `nats/bootstrap-operator.sh`: the `observability` user subscribes
          on the subject and may publish on
          `rpc._platform.health.*.ready.v1`. Live only after
          `./bootstrap-operator.sh --force` and a reseed — 54b-7.
  - [ ] **54b-7** — stack checks on `au-1`: BR-072's integration checks
        (above), then BR-068 capacity and overflow at measured peak.
- **54c — Admin UI** switches to the gateway: polling, body events,
  "Open in Grafana". Covers every `useTraceFeed.js` reader:
  `TraceWaterfall.vue`, `RpcPanel.vue`'s `[messages]` tab, and
  `PulsePanel.vue`, which is marked unavailable and loses the default tab
  (decision 15). Poll interval set from 54a's measured visibility delay.
  Also validates the Services panel (decision 17): instance identity,
  account labels, live statistics, bounded observation semantics and
  visible discovery failures through the NATS service-list endpoint, with
  no Services REST fallback. Include these checks in the cutover gate.
  **Ends at decision 16's gate:** 54c complete and validated, then the one
  switch on the default stack.
- **54d — retire** everything in decision 13, Jaeger, and the
  accounts-service trace export/import. Only after the switch has been
  made and checked, and the Messages panel is proved to still work and to
  link to its trace. Old trace history goes with it (decision 16).
  Retire `GET /api/nats/services` after its NATS replacement is validated
  (decision 17); update route docs and BR-040 together.

#### Business rules this phase adds or changes

New (drafted 2026-10-15, written into BUSINESS_RULES-SHIPPING.md with 54a):

- **BR-064** — one request across services is one trace: SERVER → CLIENT →
  SERVER parent/child, linked by `traceparent` in NATS headers.
- **BR-065** — an outbound CLIENT span carries the caller's `service.name`;
  the destination is an attribute.
- **BR-066** — with the Collector down — stopped or hung — the business
  call succeeds, spans drop within a bounded queue, p95 rises < 5 ms, and a
  service still stops within its shutdown timeout.
- **BR-067** — only an operator credential may call
  `api._platform.observability.trace.{search|get|services}.v1`; a tenant
  credential cannot reach the trace service or read trace data (*no
  responders* or a permission error both satisfy this) (decision 4).
- **BR-068** — with Tempo down, the Collector keeps spans on disk across a
  graceful restart and a hard kill; the queue holds a 2-hour outage at
  measured peak traffic; all queued spans arrive when Tempo returns; a full
  queue's loss is counted and visible, and business calls are unaffected.
  Durability proved in 54a, capacity and overflow after 54b.
- **BR-069** — replaces BR-051: the receiving service sets two span
  attributes — the receiving account, from the connection the message
  arrived on, and the originating tenant, only from server-written routing
  context (else empty, shown `unattributed`). Never from a client-supplied
  header, the body or `Nats-Requestor`. A server-written header is a source
  only after a test proves a client cannot spoof or override the value the
  service reads. Unforgeable by a tenant client; platform services are
  trusted, not checked.
- **BR-070** — every trace reply is the lab's own versioned schema, never
  Tempo's passed through.
- **BR-071** — an inbound `api.*` call always starts a new trace, sampled
  always-on; a browser-sent `traceparent` becomes a span link, never a
  parent, and cannot turn sampling off.

- **BR-072** — micro supplies live service responsiveness, identity and
  endpoint statistics across the permitted accounts, independently of
  telemetry export. PING/INFO/STATS do not assert dependency readiness;
  incomplete or failed discovery is explicit. Drafted with decision 17;
  Clients use the operator-only, versioned NATS service-list endpoint;
  implementation and tests pending in 54b/54c.

- **BR-073** (added 54b-5) — an inbound REST request is one SERVER span
  under the `EdgeAPI` policy (new root, sent context as a link, receiving
  account from the mounting code); the publishes its handler makes are
  PRODUCER children in the same trace. Error status is 5xx only.

Changed, in the same commits as the code (Quality Rule 4): BR-035 (push →
poll), BR-036 (envelope → span events; redact-before-truncate kept), BR-037
(propagation via OTel; one span per logical call kept), BR-051 (superseded
by BR-069), BR-053 (retired with the bucket), BR-056 (retired — OTel
carries real start and end times), BR-AC36 (trace import retired), BR-AC18
(`MintAdminToken` gains the three trace subjects), BR-045–BR-047
("Enforced in" paths move off `natstrace`; behaviour unchanged), and
BR-035's Pulse amendment (Phase 44: Pulse marked unavailable until Phase
55, decision 15).
BR-040 loses only `GET /api/nats/services` at retirement (decision 17);
no REST trace routes are added. BR-AC18 gains the service-list subject as
well as the three trace subjects. BR-041's requester identity
stays observability-only, recorded as a span attribute. ARCHITECTURE-COMMUNICATIONS.md § 6 and
ARCHITECTURE-OBSERVABILITY.md are revised with it (including an "after"
edition of `observability-message-path`), and an ADR records the reversal.

---

### Phase 55 — PROPOSED (awaiting approval; follows Phase 54) — Logs in Loki, Metrics in Prometheus, One Grafana View With Traces

> Proposed 2026-10-14. Phase 54 puts traces in Tempo and Grafana; this phase
> adds the other two signals so one Grafana view links a trace, its log
> lines and the metrics around it. Design gate only — no tasks, tests or
> code until approved.

#### Goal

Grafana shows traces (Tempo, Phase 54), logs (Loki) and metrics
(Prometheus) and links between them: span → its log lines, log line → its
trace, slow point on a latency chart → an example trace. NATS server
metrics appear beside the services' own.

#### Starting point (verified 2026-10-14)

- Services log with `slog.NewTextHandler(os.Stdout, …)` — plain text, no
  trace ID, collected by nothing but `docker compose logs`.
- No Prometheus, no metrics endpoint in any service. NATS exposes its
  monitoring port (`8222`, `NATS_MONITOR_PORT`) and nothing scrapes it.

#### Design decisions

1. **Depends on Phase 54.** It reuses 54's Collector, Grafana and context
   propagation: a log line can only carry a trace ID when `ctx` reaches the
   logging call. 55a alone (NATS server metrics) has no such dependency.
2. **Logs go out over OTLP.** Each service's `slog` handler is wrapped with
   the OTel `otelslog` bridge: records go to the Collector → Loki (Loki 3
   accepts OTLP natively), and still to stdout so `docker compose logs`
   keeps working. Logging calls on a request path use the `…Context(ctx, …)`
   forms so `trace_id`/`span_id` attach automatically.
3. **Loki labels stay low-cardinality.** Labels: `service.name`, `level`.
   `trace_id`, `span_id`, tenant and `{context}` are structured metadata,
   never labels. Entity IDs never become labels.
4. **Log lines carry no request or reply bodies.** Bodies live only in
   Phase 54's span events, behind its redaction and size limit. A log line
   that needs a payload detail names a field, not the payload.
5. **Service metrics are OTel metrics, pushed.** Each service records
   request count, error count and a duration histogram per operation
   (RED), plus JetStream consumer lag where it consumes. The SDK pushes
   OTLP to the Collector → Prometheus (OTLP receiver). No `/metrics`
   endpoint per service.
6. **Exemplars link metrics to traces.** The duration histogram carries the
   trace ID of sampled requests as an exemplar, so a slow point on a chart
   opens its trace in Tempo.
7. **Metric labels stay low-cardinality.** `service.name`, operation
   (subject with IDs stripped to the taxonomy positions), status. Tenant
   and `{context}` are allowed as labels only if the count stays bounded in
   the lab; never entity IDs.
8. **NATS server metrics come from `prometheus-nats-exporter`** (the
   NATS project's own exporter), scraping `8222` (`varz`, `connz`, `jsz`,
   `accstatz`). Prometheus scrapes the exporter. A NATS dashboard is
   provisioned in Grafana.
9. **Grafana is configured from files in the repo.** Data sources and the
   links between them are provisioned, not clicked: Tempo → Loki (trace to
   logs by `trace_id`), Tempo → Prometheus, Loki derived field → Tempo,
   Prometheus exemplars → Tempo. Dashboards are JSON files in the repo.
10. **The Pulse tab moves to metrics.** Phase 54 decision 12 keeps Pulse
    trace-derived only while sampling is 100%. Here Pulse reads request
    rate, error rate and latency from Prometheus through
    `observability-service`, over NATS `api._platform.observability.*.v1`
    request/reply like Phase 54 decision 9, which frees sampling to change
    later. **This is now the only path back for Pulse:** Phase 54 decision
    15 (c) marks the tab unavailable from 54's cutover until this ships,
    and restoring it (and `ui.rpcTab`'s `'pulse'` default) is part of this
    phase.
11. **Grafana is an operator tool, not a tenant one.** It sits on the
    platform side, with its login on and anonymous access off (Phase 54
    decision 2). Access follows Phase 54 decision 4: operators read all
    tenants, tenants read nothing; Grafana has no tenant login in the lab.
12. **Lab storage is local disk, short retention.** Loki and Prometheus run
    as single processes with local storage, **inside the Docker network
    only** (Loki 3100, Prometheus 9090, exporter 7777 — not published on the
    host, for the same reason as Tempo in Phase 54 decision 2).
13. **Production stores are a V3 decision.** As with Tempo (Phase 54
    decision 2): Loki scales on object storage and is AGPLv3; Prometheus is
    Apache 2.0 but single-node, so V3 needs Mimir (AGPLv3) or another
    long-term store. Not decided here.
14. **Out of scope:** Postgres metrics, host/container metrics, alerting.
    Each is a candidate if wanted.

#### Proposed slicing (for approval — becomes sub-phases only after the gate)

- **55a — NATS server metrics.** Prometheus, `prometheus-nats-exporter`,
  the NATS dashboard. Needs nothing from Phase 54.
- **55b — service metrics.** RED metrics and exemplars in one service
  first, then the rest; exemplar → Tempo link working.
- **55c — logs.** `otelslog` bridge, Loki, trace ID on request-path logs;
  span → logs and log → trace links working.
- **55d — Pulse tab on metrics** through `observability-service`.

#### Business rules this phase adds or changes

New rules (IDs assigned at the gate): log labels and metric labels stay
low-cardinality (decisions 3, 7); log lines never carry bodies
(decision 4); every request-path log line carries `trace_id`
(decision 2); Grafana links are provisioned from the repo (decision 9).
Changed: the Pulse rule in BUSINESS_RULES-SHIPPING.md (Phase 28p entry,
amended by Phase 44) moves Pulse to metrics (decision 10). ARCHITECTURE-OBSERVABILITY.md gains a logs-and-metrics
section.

---

### Phase 56 — PROPOSED (awaiting approval; follows Phase 54) — Trace Activity Hints: a Push Event So Clients Refresh Without Polling Blind

> Proposed 2026-10-15, split out of Phase 54 after a Claude/Codex review.
> Phase 54 gives every authorized client trace queries over NATS `api.*`;
> this phase adds an optional push, shaped like the NATS server's own
> advisories (`$SYS.ACCOUNT.<id>.CONNECT`): a small typed event that says
> something happened, with the detail fetched by request/reply. Design gate
> only — no tasks, tests or code until approved.

#### Goal

A client subscribed to one subject learns that new trace activity was
observed, then fetches the trace through Phase 54's
`api._platform.observability.trace.get.v1`. Polling stays the fallback and
the reconciliation path.

#### Design decisions (draft)

1. **The event means "root-span end observed" — a refresh hint, not "trace
   complete".** A root span may end before async `evt.*` spans arrive, the
   root span may be missing, and export retries can deliver it twice. The
   event never claims completeness.
2. **Subject and shape.** One subject on PLATFORM, e.g.
   `notify._platform.observability.trace.root-ended` (name fixed at the
   gate). Payload: `type`
   (`io.linebooker.observability.v1.trace_root_ended`), `traceId`, service,
   operation, status, duration, tenant. No attributes beyond these, no
   bodies.
3. **Best effort, core NATS.** No stream, no replay. A client that missed
   events reconciles with `trace.search.v1` after reconnect; that is
   reconciliation, not guaranteed replay. Clients de-duplicate by `traceId`
   and retry a fetch a bounded number of times.
4. **No ordering with Tempo.** The event can arrive before the trace is
   retrievable, or while Tempo is down. A failed fetch after the retries is
   an expected outcome, shown as such, not an error.
5. **Source: a second Collector export to `observability-service`.** Only
   this phase needs it; Phase 54's queries do not. It is a separate exporter
   with its own queue, and a failing notification receiver must never stop
   or slow the durable Tempo export (Phase 54 decision 11) — proved by a
   test, not assumed.
6. **Access as Phase 54 decision 4.** The subject is granted only in the
   operator PLATFORM profile and never exported to a tenant account. One
   shared subject cannot filter by the JSON `tenant` field, so per-tenant
   delivery would need its own design (subject per tenant via a
   provisioned export, or service-managed private inboxes) — out of scope
   until per-tenant access exists.

#### Open at the gate

- Whether the Admin UI uses the hint at all, or keeps polling only.
- Event rate limits under load (one event per root span may be noisy).

---

### Phase 60 (following on from Phase 24; 24a DONE, 24b/24c not started) — Credential Lifecycle Hardening: Hermetic Tests, Volume-Backed Creds, Runtime Tenant Provisioning

> **Renumbered 2026-08-17** from Phase 24 to Phase 40, alongside Phase
> 29 → Phase 41, when Phases 23/25/25i/26/27/28/30 were archived (see the
> "Renumbering (2026-08-17)" log in the archive's "Renumbering
> history" section). Sub-phase
> labels below (`24a`/`24b`/`24c`) are kept as-is rather than renumbered to
> `40a`/`40b`/`40c` — they're already referenced under the `24a`/`24b`/`24c`
> spelling in code/test comments (e.g. `isolation_test.go`,
> `tenant_switch_test.go`) and in this phase's own design section below;
> renaming them would be a much larger, purely-cosmetic sweep for no
> functional benefit. Only the containing phase number changed.

> **Renumbered again 2026-08-20b** from Phase 40 to Phase 60, when the
> whole 40–49 block was shifted to 60–69 (see the "Renumbering
> (2026-08-20b)" log in the archive's "Renumbering
> history" section). The `24a`/`24b`/`24c`
> sub-phase labels stay as-is for the same reason given above.

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
- [ ] 24b: `deploy/cell/compose.runtime.yaml` — named volume replacing the `./nats/creds` bind mount across every consumer, **including `observability-service`'s `observability.creds` mount** (added in Phase 30c, after this sub-phase's original scope was written)
- [ ] 24b: `BUSINESS_RULES-ACCOUNTS.md` — new rule documenting the populate-on-boot mechanism and the `sys.creds` special case
- [ ] 24b: Live verification — `docker compose down -v && up --build` with `./nats/creds` no longer existing as a bind mount at all; confirm every service connects, **including `observability-service` and every NATS/SYSTEM Admin UI panel it backs (Phase 30's own live-verification checklist)**

> **Risk flagged 2026-08-17 (post-Phase-30 audit) — 24c's already-documented boot-ordering trap is now a multi-service problem, not the single-service one it was scoped against.** The design note above only warns about `shipping-service`'s own creds-directory tenant scan. Since this phase was written, `organizations-service` and `pricing-service` each grew their *own* independent creds-directory scanner and their own `nonTenantCredsFiles` exclusion list (`organizations/internal/tenants/tenants.go`, `pricing/internal/tenants/tenants.go`) — and this session found and fixed the identical latent bug (a missing `observability` exclusion entry) in both, independently, confirming the pattern really does drift out of sync across services. Moving `acme`/`globex` to async runtime creation reopens the exact race this phase already flags, but across three independent scanners instead of one. Any future 24c implementation must re-audit every service's tenant-discovery scanner (currently: shipping-service, pricing-service, organizations-service — check for new ones before starting), not just shipping-service's.

- [ ] 24c: `Provisioner`/seed step creates `acme`/`globex` via `POST /api/accounts` (or an equivalent internal call) instead of `bootstrap-operator.sh`
- [ ] 24c: confirm `EnsureTenantByName`/`notify.accounts.account.created` actually covers the boot-ordering gap this introduces — trace current callers, don't assume
- [ ] 24c: **re-audit every service's creds-directory tenant-discovery scanner** (shipping-service, pricing-service, organizations-service as of 2026-08-17 — confirm the current list before starting), not only shipping-service's, per the risk note above
- [ ] 24c: `bootstrap-operator.sh` scope reduced to `operator` + `SYS` + `PLATFORM` only
- [ ] 24c: `BUSINESS_RULES-ACCOUNTS.md` — rule change documenting PLATFORM-bootstrapped / tenants-runtime as the enforced split
- [ ] 24c: Live verification — fresh `down -v && up --build` produces working `acme`/`globex` tenants with no bootstrap involvement beyond `PLATFORM`

### Phase 62 — Close-Out Review: Outstanding Items Carried Forward from Archived Phases

> **Renumbered 2026-08-20b** from Phase 42 to Phase 62, when the whole
> 40–49 block was shifted to 60–69 (see the "Renumbering (2026-08-20b)" log in the archive's "Renumbering
> history" section).

#### Goal

Phases 20, 21, 22, 22b, 23, 25, 25i, 26, 27, 28, and 30 were archived to
[Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md) (2026-08-17) once each
reached 93%+ checklist completion, and Phases 51 and 52 on 2026-08-27. Some of
them had small, genuinely-open items that shouldn't just disappear into a file that isn't read into
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
- [ ] **From Phase 26 (Organizations Service) — deferred items, carried
      forward as named open questions, not silently dropped:**
      lifecycle-as-CQRS/temporal exploration, `ComplianceDocument` temporal
      classification, document-expiry-driven status, real file storage,
      terminal/offboarding state, platform-identity vs tenant-membership
      split, `notify.*` publication once a marketplace consumer exists.
      Intentionally open-ended — a list to revisit if/when any of these
      becomes a real requirement, not a task with a completion criterion.
- [ ] **From Phase 51 (Credential Revocation and Connection Outcome).** Live
      verification of 51b: revoke a credential, confirm the server drops its
      connection, and confirm the reconnect attempt surfaces in 51a's panel as
      `Authentication Failure` rather than the ordinary `Authentication
      Expired`. Not done in-session for a concrete reason rather than by
      oversight: every credential in the running stack is load-bearing, and
      revocation is **terminal** — there is no un-revoke, so recovering from a
      test revocation means `docker compose down -v` plus a bootstrap reseed.
      Doing it properly means minting a throwaway credential first.
- [ ] **From Phase 52 (Reaping Expired Sessions) — deferred, with the
      condition that would reopen it.** Pagination on
      `api._platform.accounts.user.list.v1`. Deliberately not built: the
      panel's held-back counts are honest only because it holds the whole
      set, and now that the reaper bounds the table the unbounded list is no
      longer the growth risk it was. Revisit if a measurement — not a
      hypothesis — shows the list outgrowing one response.

---

### Phase 64 — PROPOSED (awaiting approval) — Local NATS Supercluster: Three Clusters, Nine Servers, Gateway Mesh

Full requirements, design decisions and checklist live in
[Multi-Region-Plan.md](../../demos/02-multi-region/docs/Multi-Region-Plan.md) § 3-4, where the multi-region
requirements already sat. Summary: turn the topology drawing into a running
local stack — `hub`/`za`/`au` clusters, three servers each, full gateway mesh,
real R3 inside each cluster — on one Docker engine, because the user's stated
goal is to replicate the structure locally rather than deploy to cloud. NATS
servers only; services and frontends unchanged. Withdraws the "AWS solves the
network problem" deferral written into the compose band headers, so ADR-055's
rationale needs revising.

---

### Candidate, deferred, and on-hold phases

Not in flight. Full detail lives in
[Main-POC-Plan-Candidates.md](Main-POC-Plan-Candidates.md) (not read into
context by default — open it when picking up or re-scoping one of these).
A phase moves back into this file, in full, when work on it actually
starts.

- [ ] **Phase 63** (DEFERRED 2026-08-18 — design approved, implementation
      on hold) — NATS 2.11 Server-Hop Tracing ("Trace this subject").
      Lineage: Phase 29 → 41 → 36 → 43 → 63.
- [ ] **Phase 100** (PROPOSED) — Ship Container Capacity Limit (BR-019).
- [ ] **Phase 101** — Write-Side Safety (optimistic concurrency + publish
      dedup).
- [ ] **Phase 102** — Projection Hardening (consumer-side idempotency +
      explicit limits).
- [ ] **Phase 103** — Stream Split + Cross-Aggregate Consistency.
- [ ] **Phase 104** — Performance & Load Testing (full suite).
- [ ] **Phase 105** (optional, PLACEHOLDER — not yet a formal requirement)
      — Per-Tenant Runtime Theme Spike.
- [ ] **Phase 106** (DEFERRED from Phase 22b, 2026-08-13) — Context
      Inheritance on the Live Read Path.
- [ ] **Phase 107** (candidate, deferred from Phase 36's design gate,
      2026-08-18) — Re-fire a Captured Trace with Server-Hop Tracing.
- [ ] **Phase 108** (candidate, deferred from Phase 43's design gate,
      2026-08-20) — Live Account Activity Panel via `$SYS`
      Account-Monitoring Exports.
- [ ] **Phase 109** (candidate, from Phase 54's design gate, 2026-10-15) —
      Operator Login: Gate the PLATFORM Credential Mints.

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

## Renumbering history

Every renumbering log — the ten `Renumbering (...)` sections that used
to sit here, covering the original proposal through the 2026-08-20b
`40–49 → 60–69` shift — is archived verbatim under "Renumbering history"
in [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md). Consult it when a
phase number in an older doc, commit message, or business rule doesn't
match this plan; append new renumbering logs there, not here.

---

## Working Assumptions

- JetStream is the source of truth: commands hydrate aggregates by replaying the stream, and Postgres and KV are downstream projections populated only by event consumers — never written directly by the command path. (Superseded earlier assumption that Postgres was the source of truth. Also superseded: this assumption used to distinguish "Postgres (Shape B) and KV (Shapes A/B)" — Phase 31 retired Shapes A and C, so there is one shape and the parenthetical no longer applies.)
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
