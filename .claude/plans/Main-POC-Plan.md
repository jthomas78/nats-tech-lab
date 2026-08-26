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

- [x] Phase 23 (IMPLEMENTED 2026-08-04) — Admin UI: SSE → NATS WebSocket
      Migration (Dual-Connection Model): all four `frontend/admin` SSE
      streams replaced with direct browser NATS WebSocket pub/sub via a
      dedicated Admin/Platform connection (`MintAdminToken`, BR-AC18) plus
      the existing per-tenant connection; `sse.go`'s watch handlers deleted.
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
      Provenance per [ADR-050](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ADR-050-git-certificate-change-log-provenance.md)
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
      [ADR-051](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ADR-051-ulid-entity-identity.md);
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

### Phase 43 — APPROVED 2026-08-25 (design approved 2026-08-20, reviewed and amended 2026-08-25, cleared for implementation) — Cross-Tenant Pub/Sub Observability ("Wire Tap") in the Admin UI

> **Renumbered 2026-08-25** from Phase 67 to Phase 43, ahead of a design
> review, to sit in the 40s block with the other in-flight phases. Lineage:
> Phase 47 → 67 (2026-08-20b, when the whole 40–49 block was shifted to
> 60–69) → 43. See the "Renumbering (2026-08-25)" and
> "Renumbering (2026-08-20b)" logs in the archive's "Renumbering history"
> section. Sub-phase labels **were** relettered on both occasions
> (`47a`/`47b`/`47c` → `67a`/`67b`/`67c` → `43a`/`43b`/`43c`), unlike Phase
> 60’s `24a`–`24c`: nothing is implemented yet, so the only references are
> the PROPOSED BR-045–048/BR-D45/BR-AC34 entries and their pending/skipped
> test stubs, all swept in the same pass.

#### Goal

Give the Admin UI a live view of pub/sub traffic across every tenant
account — not just the RPC calls `natstrace`/`obs.trace.*` already covers
(BR-036/BR-037) — triggered by evaluating NATS's own wire-tap/monitoring
pattern (`docs.nats.io/concepts/subjects#wire-taps-and-monitoring`, a plain
`sub >`) against this lab's hard NATS-account tenant boundary.

#### Design decisions

Full ADR lives in
[ARCHITECTURE-OBSERVABILITY.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-OBSERVABILITY.md)
(ADR-047) — not duplicated here. Summary of the decision it records:

- A plain wildcard subscription (`>`), or the dormant `$SYS` account's
  `account-monitoring-streams`/`account-monitoring-services` exports,
  cannot give message-payload visibility across NATS accounts — account
  boundaries are server-enforced subject-space isolation in this lab by
  design, and `$SYS`'s monitoring exports only surface
  connection/subscription metadata, never payloads.
- **Rejected: (A) blanket per-tenant export of `>`** into the observability
  account — the only design with zero instrumentation gaps, but a
  first-time breach of the narrow-grant pattern BR-AC30/31/32 established
  and Phase 30h reinforced (Phase 30h specifically retired an earlier
  *unrestricted* second PLATFORM connection). Revisit only if "see every
  byte, even uninstrumented paths" becomes a hard requirement, with its own
  new safety design.
- **Deferred, not rejected: (B) import the dormant `$SYS` account-monitoring
  exports** — cheap, safe, gives connection/rate metadata with no payload
  visibility, so it doesn't answer the actual ask on its own. Candidate
  follow-on phase for a complementary "account activity" panel.
- **Selected: (C) a new, sibling `obs.pubsub.*` envelope**, instrumented
  only at `evt.*`/`notify.*` publish call sites (never a generic
  `Publish()` wrap — that risks self-observation or picking up JetStream
  control traffic), reusing BR-036/037's redact-before-truncate discipline
  and BR-AC30's narrow per-tenant export/import. `rpc.*`/`api.*`
  (request/reply) keep using the existing `obs.trace.*` channel — the split
  is clean by construction, so no request/reply or JetStream-internals
  filter is needed on this new channel. Gets its own dedicated "Messages"
  panel in the Admin UI (with an `evt`/`notify` family filter), not a tab
  inside `RpcPanel.vue`. Accepted trade-off: only instrumented publish call
  sites are visible.
- **Resolved (2026-08-20):** (B) does become a follow-on — tracked below as
  candidate **Phase 108**, requiring its own UI account filter
  (`$SYS.ACCOUNT.*.>` spans every account at once). Consumer-side behavior
  (redelivery counts, ack latency) is **out of scope** for this phase —
  publish-only for now, may evolve later once this ships and proves useful.

#### Amendment (2026-08-25) — pre-implementation design review

Reviewed with `/engineering:system-design` before any code. The Option C
decision stands; ten findings were folded into ADR-047 as
"Amendment (2026-08-25)" and into the rules. The four that change the design
rather than sharpen it:

- **A1 — the panel could not name the tenant.** PLATFORM imports every
  tenant's `obs.trace.>` onto one identical local subject with no remap, the
  envelope has no account field, and `{context}` is the business unit, not the
  tenant — so "across every tenant account" was unanswerable as designed.
  `obs.pubsub.>` now imports under a per-tenant `monitor.{tenant}.pubsub.>`
  remap, mirroring `$SRV.>`/`$JS.API.*`. Changes BR-AC34's shape.
- **A2 — the call-site list was incomplete**, and organizations-service had
  no rule at all. Five more publishers are in scope; BR-TP75 is new.
- **A3 — the "never wrap a primitive" ban was over-broad.** `PublishWithTrace`
  has three call sites, all `evt.*` — it is the `evt.*` seam, not a generic
  primitive. `evt.*` is now instrumented **in** the seam (structural
  coverage); `notify.*` stays per-call-site. The ban stands for
  `Publish`/`PublishMsg`.
- **A4 — coverage enforcement is now designed**, not aspirational: new
  BR-049 makes the `notify.*` list a CI-checked convention, discharging the
  ADR's own headline "Harder" consequence.

Sharpened, not changed: separate bounded stream with measured caps (A5),
`Nats-Msg-Id` + explicit `Duplicates` window so dedup is enforceable (A6),
best-effort ingestion stated honestly rather than implied complete (A7),
redaction review scoped and scheduled before 43a with transporter-profile
payloads first (A8), panel row cap / pause / `evt`-only default (A9), and a
thin vertical slice first so A1 lands on screen early (A10).

Rules: BR-045–049 (SHIPPING), BR-D45 (REFDATA), BR-AC34 (ACCOUNTS), BR-TP75
(ORGANIZATIONS) — all still PROPOSED, all amended or added in this pass.

#### Sub-phases

Implementation takes a **thin vertical slice first** (A10): shipping's `evt.*`
seam → minimal stream → minimal panel, which puts the tenant-provenance
decision on screen before the export shape is committed. The sub-phases below
then widen it.

- [x] **43a** — `obs.pubsub.*` envelope; hook **in** the `evt.*` seam
      (`PublishWithTrace`, `JetStreamEventStore.append`) and at each
      `notify.*` call site; `Nats-Msg-Id` = `spanId`; per-tenant
      export + `monitor.{tenant}.pubsub.>` import remap; BR-049's
      coverage test. Redaction review (A8) runs **before** the hook is wired
  - [x] **vertical slice** (A10) — `natstrace.ObservePublish`/
        `ObservePublishAs` (envelope, subject derivation with action as the
        *last* token, trace continuation, `Nats-Msg-Id` = `spanId`,
        redact-then-truncate, self-observation guard); the `evt.*` seam in
        shipping only (`Publisher.EnableObservation` +
        `PublishWithTrace`, opted in per tenant in `rest/tenant.go`);
        `obs.pubsub.>` export + `addPlatformPubsubImport` with the
        `monitor.{tenant}.pubsub.>` remap. All green 2026-08-25
  - [x] **widening** (2026-08-25) — all five of shipping's `notify.*` call
        sites (`publishNotify`, `publishRawNotify`, `publishPortsChanged`,
        `kvstore.publishNotify`, the refdata bridge), plus
        `natstrace.Observe`/`ObserveAs` for bare call sites; the accounts
        channel move off `obs.trace.*` (which also retired the synthetic
        outbound span that channel was the only reason for); the `evt.*`
        seams in refdata (`jstream.Publisher`, opted in via
        `EnableEventObservation`) and organizations
        (`JetStreamEventStore.append`, opted in per tenant runtime); the
        `obs.pubsub.>` allow-pub grant the restricted `shipping-admin` user
        needed; and BR-049's `go/ast` coverage scan
        (`internal/notifycoverage/`). Two findings recorded in the rules:
        refdata's `notify.*` observation belongs in `composition.go`'s
        per-tenant fan-out, not in `notifybridge` which holds no connection
        to attribute it to; and `pubsubstore.publishNotify` is a **second**
        exclusion, because observing it would feed obs.pubsub back into its
        own bucket in an unbounded loop
  - [x] **skip cleanup** (2026-08-25) — the last stubs that still read
        "pending Phase 43a implementation" replaced by real specs:
        accounts' four lifecycle notifies, organizations' event-store seam
        (including the actor-PII redaction list), and refdata's two, which
        moved to `refdata/notify_observability_test.go` — a live per-tenant
        fan-out spec plus a checked convention that `internal/kvcache` never
        calls `natstrace.Observe*`, keeping the `evt.*` hook in the seam.
        `tenantPublisher.publishTo` was split out of the `Range` callback so
        the fan-out's one leg is testable without a tenant `Manager`
- [x] **43b** — `observability-service`: `pubsubstore`, sibling to
      `tracestore` — the `PUBSUB` stream capturing **both**
      `obs.pubsub.>` and `monitor.*.pubsub.>` (tenant exports arrive
      remapped, so one wildcard would have missed them), bounded 1 h /
      32 MiB with an explicit 2 min `Duplicates` window, projecting into
      the `pubsub-messages` bucket (15 min / 8 MiB — a visible window,
      deliberately tighter than the stream) with the tenant derived from
      the arrival subject. Caps measured, not inherited: a real envelope
      is 454–592 B, not the ~2 KiB the ADR assumed. Grants wired in
      `bootstrap-operator.sh` and `MintAdminToken`. All green 2026-08-25.
      **Re-measured after 43a's widening (2026-08-25):** 317 B – 2 019 B
      across the full publisher set (flat ~303 B overhead + payload; the
      biggest is the KV-change notify carrying a whole bucket value), and
      ~4.4 KiB worst case since the 4 KiB truncation cap is a hard ceiling.
      Caps unchanged and comfortable — full table in BR-047. One open
      sizing item, left deliberately unmade: the Messages panel fetches
      every bucket entry on load but renders 500, so a full 8 MiB bucket is
      a multi-megabyte page load; bounding it touches both BR-047's caps
      and 43c's feed
- [x] **43c** — Admin UI: `MessagesPanel.vue` on its own SYSTEM → NATS nav
      entry (`pubsub`), fed by `usePubsubFeed.js` over the
      `pubsub-messages` bucket. Tenant named per row from the import remap
      (not `TraceWaterfall`'s coarse gutter) and click-to-filter,
      `evt`/`notify` family filter defaulting to `evt`, 500-row cap with an
      eviction note, pause/resume freezing only the visible ordering, and
      `SubjectPath.vue` for every subject. Best-effort disclaimer on the
      panel rather than implied completeness. 9 specs + the feed's 7, all
      green 2026-08-25.

**Cleared for implementation 2026-08-25**, after the design review above.
The hold placed on 2026-08-20 is lifted; the business-rules-first pass is
**done** — BR-045–049 (`BUSINESS_RULES-SHIPPING.md`), BR-D45 (REFDATA),
BR-AC34 (ACCOUNTS), BR-TP75 (ORGANIZATIONS), all confirmed 2026-08-25 and
carrying the amendment. Ginkgo specs still come **before** implementation
per the standing AI Agent Workflow — the pending stubs already in the tree
(`pubsub_observability_test.go` ×4, `pubsub_export_test.go`,
`MessagesPanel.spec.js`) are the red half, derived from the rules, and get
real bodies as each sub-phase lands.

**43a's gate is cleared.** BR-046's redaction review of real
`evt.*`/`notify.*` payloads completed 2026-08-25, before any hook was wired.
Outcome, recorded in full in BR-046:

- **Two fields added to the shared denylist** — `actorName` and
  `actorSourceIP`, from organizations-service's transporter-profile events;
  the only action items in the whole review. Shared list extended, not forked,
  so both are redacted from `obs.trace.*` too. Green in
  `shared/natstrace/natstrace_test.go`.
- **A dependency, now written down:** `Event.Changes`'s `from`/`to` values sit
  under a field name, not a denylisted key, so the denylist cannot reach them.
  They are safe only because BR-TP72 withholds such values structurally —
  weakening BR-TP72 now leaks cross-tenant through this channel.
- **A caveat, now written down:** `publishChange` publishes whatever value was
  `Put`, so any new bucket wired through `kvstore.New` is automatically
  observed and needs its own review. Three benign buckets today.
- **A scope change the review turned up:** accounts-service's four
  `notify.accounts.account.*` publishes already emit `obs.trace.*` spans.
  Decided 2026-08-25 — they **move** to `obs.pubsub.*` rather than appearing on
  both. The Traces panel loses four entry types; this is the one place Phase 43
  edits a shipped pipeline rather than adding beside it. See BR-AC34.

#### 43d — IMPLEMENTED 2026-08-25 — `shared/natsnotify`: give `notify.*` the seam `evt.*` already has

Follows 43a/43b, which are landed (`df96bec`). Arrived at through an
architecture review and a full grilling pass on 2026-08-25; every decision
below was put to the user and confirmed before any code was written.

##### The problem 43a exposed

43a instrumented `notify.*` **at each call site**, because there was no seam
to instrument. Nine publishers across four services each concatenate a
subject, call `nc.PublishMsg`, log-and-swallow, then separately ask
`natstrace` to parse the subject back into observability tokens. That second
step is the defect: the tokens were known when the subject was built, thrown
away by concatenation, and guessed at afterwards by reading positions out of
a string.

Two of the nine subjects prove the guess is wrong rather than merely fragile:

- `notify._platform.refdata.{ctx}.{typeKey}.changed` must observe with
  `kvContext`, **not** the `_platform` sitting in token 1.
- `notify.accounts.account.{action}` is four tokens, below
  `ObservePublish`'s `< 5` floor, so the deriver skips it entirely.

Both already compensate by calling `ObserveAs` with explicit tokens. That
workaround is the design; this sub-phase makes it the only path.

`evt.*` has had the seam since Phase 43a:
`jstream.Publisher.PublishWithTrace`. `natsnotify` is its sibling — a
different transport (core NATS, fire-and-forget, no PubAck) under the same
contract.

##### Design decisions

- **Module.** `shared/natsnotify`, a new module at repo root beside
  `natstrace`, `browserrpc` and `natstenants`. The shared→shared dependency
  on `natstrace` is precedented: `browserrpc` and `natstenants` both already
  `replace … => ../natstrace`.
- **Two layers, deliberately split.** `natsnotify` owns publish, the
  observation gate and the observation emit, and takes the four
  observability tokens — context, service, entity, action — **explicitly,
  always. It never derives them from the subject.** Each service owns its
  own subject construction as named constructors. There is no shared subject
  grammar because there isn't one: arities run 4 to 7, and
  `notify.{ctx}.kv.{bucket}.{key}.changed` is structurally unbounded, since
  KV keys contain dots.
- **Constructor option, not two-phase.** `New(nc, log, WithObservation(obsNC))`
  rather than `evt.*`'s `EnableObservation(nc)`. Same opt-in semantics, no
  window in which a `Notifier` exists misconfigured. A knowing divergence
  from the `evt.*` seam; `shared/jstream` converges on this shape when the
  jstream-deduplication candidate lands.
- **No error return.** Injected `*slog.Logger`, fire-and-forget, exactly as
  today. The span is carried on `ctx` and pulled with
  `natstrace.SpanFromContext`.
- **The `Notifier` holds its connection.** BR-D45 requires the observation
  envelope to be emitted on the tenant's *own* conn, or BR-AC34's import
  remap attributes it to the wrong tenant. Holding the conn makes that
  structural — a `Notifier` is a conn plus a gate, so publishing on the
  wrong one is not expressible. Six of the seven observed sites already
  capture a conn at construction; refdata's `tenantPublisher.PublishToAll`
  is the exception and constructs one `Notifier` per tenant inside its
  `natstenants.Manager`, which is already generic over a per-tenant value.
  The rejected alternative — `Publish` taking a conn per call — hands the
  conn back to every caller, which is precisely what BR-D45 says gets
  misplaced.

##### What this deletes

- Package-level **`Observe` / `ObserveAs`** in `natstrace`. Their only
  callers are the seven `notify.*` sites.
- `Tracer.ObservePublish` (the positional deriver) and `ObservePublishAs`
  **stay**: the three `evt.*` seams call the deriver directly, and
  `ObservePublishAs` is the primitive `natsnotify` calls. Moving `evt.*` to
  explicit tokens is the jstream candidate's business, not this one's.
- The 206-line `notifycoverage` AST lint is **replaced, not deleted**. Its
  premise — "every enumerated site is instrumented" — dies with the
  refactor. Its guard — "nobody bypasses the seam" — is what keeps a deep
  module deep, and survives as a few lines asserting no `"notify."` string
  literal outside `natsnotify` and the per-service subject builders.

##### Adopters

| Service | Shapes | Subject builders | Observation |
|---|---|---|---|
| shipping | 5, across `eventhandler` ×2, `kvstore`, `browserrpc`, `platform_notify` | new `internal/notify/` package | on |
| refdata | 1 | in place | on, per-tenant `Notifier` |
| accounts | 1 | in place | on, explicit `_platform` context |
| observability | 2 | in place | **never enabled** |

`observability-service` adopts **without** `WithObservation`. Its
`pubsubstore` publisher announces writes to the very bucket observation
envelopes land in; observing it is an unbounded feedback loop. Under the
opt-in gate that exclusion stops being a string in a hand-maintained table
and becomes a fact about how the `Notifier` was constructed. The cost is
honest and recorded here: this is the first shared-module dependency for a
service that has deliberately had none.

`notify.accounts.account.{action}` **keeps** its four-token, context-free
shape — the deliberate consequence of accounts administering the tenant axis
itself. Regularising it to `notify._platform.accounts.…` would cost a JWT
grant change at `auth/token.go:180`, a shipping subscriber change, and a
credential regeneration that `Provisioner.CreateUser` would silently strip
of its scoped grants (see Phase 60). The irregularity is also load-bearing
documentation: it is the subject family that has no context because it
predates one.

##### Business rules

BR-046 and BR-047 are message properties and are untouched. The other three
are reworded so the *rule* is a property of the message and the *seam* is
named as the mechanism:

- **BR-049** — from "every `notify.*` publish site is instrumented, enforced
  by AST scan" to "every `notify.*` message is observed, because every
  publisher goes through `natsnotify`."
- **BR-045** — loses its enumerated `file:line` list; keeps the message
  properties (emitted once per publish, redacted then truncated, only after
  the publish succeeds).
- **BR-D45** — keeps its tenant-attribution constraint verbatim,
  reattributed from the call site to the `Notifier`'s connection.

A rule phrased as "these seven places do X" must be re-verified against
every new publisher. Phrased as a property of the message it is true by
construction, and the seam is what makes it so — the same move the refactor
makes in code, applied to the rules.

##### Tasks

- [x] `shared/natsnotify`: module, `Notifier`, `New`/`WithObservation`,
      `Publish`; `go.work` entry and per-consumer `require`/`replace`.
- [x] A small embedded-NATS test helper beside it. Phase 43 left four
      near-identical bootstraps (`newTestNATS`, `newObservabilityTestNATS`
      + `subscribeObservations`, `runEmbeddedNATS`); they migrate
      opportunistically, **not** in this diff.
- [x] `natsnotify`'s own specs, including the `evt.*` seam's
      `TestPublisherWithoutObservationStaysSilent` counterpart.
- [x] shipping: new `internal/notify/` subject-builder package; adopt at
      all five sites (`eventhandler` ×2, `kvstore`, `browserrpc`,
      `platform_notify`'s refdata bridge).

      **Corrected 2026-08-25 during implementation.** The design summary said
      four shapes under `dictionary/internal/notify/`; both were wrong.
      `platform_notify.go`'s `notify._platform.refdata.{ctx}.{typeKey}.changed`
      bridge is a fifth shipping shape (the dossier filed it under refdata,
      whose own shape is `notifybridge.go`'s). And the package cannot live
      under `dictionary/internal/`: Go's internal visibility rule would put it
      out of reach of `internal/kvstore`, which sits outside `dictionary/` and
      is one of the five callers.
- [x] refdata: adopt, one `Notifier` per tenant inside `natstenants.Manager`.
- [x] accounts: adopt, explicit `_platform` context token.
- [x] observability: adopt both sites without `WithObservation`.
- [x] Delete `natstrace.Observe` / `ObserveAs`.
- [x] Replace `notifycoverage/coverage_test.go` with the literal-guard lint.
- [x] Rewrite `pubsub_observability_test.go`'s five stale `PIt` placeholders
      as subject-construction tests against shipping's builder package — no
      NATS required.
- [x] Reword BR-045 / BR-049 (`BUSINESS_RULES-SHIPPING.md`) and BR-D45
      (`BUSINESS_RULES-REFDATA.md`) in the same commit.

Subscriber blast radius is **zero**: every binding is by subject literal or
builder, none by publisher identity — `shared/natstenants/tenants.go:167`,
`seafreight-app/src/api.js:77`, `admin/src/nats/useTraceFeed.js:55`,
`admin/src/nats/usePubsubFeed.js:68`, `refdata/src/stores/dictionary.js:23`,
plus the subject-pinning grants in `accounts-service/auth/token.go:113,180`.

---

#### 43e — IMPLEMENTED 2026-08-25 — `shared/jstream`: one `evt.*` seam instead of two copies

The `evt.*` sibling of 43d, and the second candidate from the same
2026-08-25 architecture review. Grilled to an empty frontier (13 questions
over three rounds) and confirmed before any code was written.

##### The problem

`jstream` existed twice — `shipping-service/internal/jstream/stream.go` (102
lines) and `refdata-service/refdata/internal/jstream/stream.go` (99 lines).
`Publisher`, `Publish`, `PublishMsg`, `PublishWithTrace` and
`EnableObservation` were byte-identical apart from comment wording and
declaration order; four of the thirteen tests across the two suites were the
same test written twice. The copies diverged in exactly one place:
`CreateStream(ctx, js, name, subjects)` vs
`CreateChangeStream(ctx, js, name, subjects, maxAge)`.

##### Design decisions (confirmed 2026-08-25)

| # | Decision |
|---|---|
| Q1 | Scope is the two `jstream` copies only. Organizations' `JetStreamEventStore` keeps its own `append` — the `ExpectedLastSubjSeq` headers, `ErrSequenceConflict` and the returned `ack.Sequence` *are* the reason it exists, and merging would make a shallow generic |
| Q2 | `shared/jstream` owns `Publisher` plus one `CreateStream` taking options, **and** narrows the exported surface to `PublishWithTrace` — `Publish`/`PublishMsg` become unexported. Neither had a caller outside its own package, so the duplicated public surface was larger than the used one |
| Q3 | `NewPublisher(js, WithObservation(nc))`; `EnableObservation` gone — the convergence 43d promised |
| Q4 | `evt.*` keeps positional token derivation. Every `evt.*` subject in the tree is read correctly by it, unlike the two `notify.*` shapes that forced 43d to explicit tokens |
| Q5 | The duplicated specs move to `shared/jstream`; each service keeps only the specs that assert something about **its** events |
| Q6 | Lands as 43e, under the already-approved Phase 43 |
| Q7 | Fix organizations' missing `Traceparent` in this sub-phase |
| Q8 | Converge organizations' gate too: `NewJetStreamEventStore(js, WithObservation(nc))` |
| Q9 | Promote `natstest` to `shared/natstest` and give it a JetStream option |
| Q10 | Keep the local one-method port interfaces in `commands` and `kvcache` — hexagonal rule |
| Q11 | Migrate only the two `jstream` suites onto `shared/natstest`; organizations' and accounts' bootstraps stay opportunistic |
| Q12 | Record organizations' traceparent compliance by extending BR-TP75 and updating BR-037's *Enforced in* — no new BR number |
| Q13 | Delete both `internal/jstream` package directories outright; no re-exporting shim |

##### The BR-037 finding

Organizations' `evt.*` appends carried no `traceparent`. **BR-037 has required
one on every `evt.*` publish since Phase 28** — so this was not a design gap
to fill but a documented rule with one service silently out of compliance,
and nothing failing. Nothing on the consume side read the header, which is
why it stayed invisible. The fix is four lines in `append` plus two specs;
the specs were confirmed red against the unfixed code before the fix landed.

##### Business rules

No new rules. BR-037's *Enforced in* now names the third seam; BR-TP75 gained
the traceparent requirement and the reason this store keeps its own `append`;
BR-045 and BR-D45 were reworded for the merged seam and the construction-time
gate.

##### Tasks

- [x] `shared/natstest`: promote out of `shared/natsnotify/natstest`, add
      `WithJetStream()` / `StartJetStream`; `go.work` entry and
      `require`/`replace` in every consumer.
- [x] `shared/jstream`: module, `Publisher`, `NewPublisher`/`WithObservation`,
      exported `PublishWithTrace` over unexported `publish`/`publishMsg`,
      `CreateStream` with `WithMaxAge`.
- [x] Merge the duplicated specs into `shared/jstream/jstream_test.go`, plus
      two new ones: `TestFailedPublishIsNotObserved` and the pair pinning
      `CreateStream`'s bounded/unbounded retention.
- [x] Delete both `internal/jstream` directories; repoint every importer.
- [x] Narrow `commands.Publisher` and `kvcache.Publisher` to one method.
- [x] Gates: `rest/tenant.go` and `refdata/composition.go` build their
      Publisher with `WithObservation`. `Startup` widened to take the
      `*nats.Conn`; `Handlers.EnableEventObservation` deleted.
- [x] Organizations: `WithObservation` option, `Traceparent` header in
      `append`, and the two BR-037 specs.
- [x] Each service keeps its own subject-shape spec —
      `shipping-service/dictionary/evt_observability_test.go`,
      `refdata-service/refdata/evt_observability_test.go`.
- [x] BR-037 / BR-045 / BR-D45 / BR-TP75 updated in the same commit.

**Deviation from the design summary, recorded during implementation.**
`TestPublishMsgCarriesHeaders` was one of the four duplicated tests slated to
move, but Q2 unexported `PublishMsg`, so there is nothing left for it to call
from outside the package. It is not lost: the header path it covered is the
one `PublishWithTrace` takes whenever a span is present, which
`TestPublishWithTraceAttachesTraceparentWhenSpanPresent` asserts end-to-end
off the wire.

---

### Phase 46 — PROPOSED (follows 39a; design inherited from Phase 39) — GIT Certificate Change Log + `Awaiting` Presentation Fix

> Split out of Phase 39 at the 2026-08-22 design gate. The design decisions
> are Phase 39's 10–13 and 16, 18, 19, 20 — archived with that phase in
> [Main-POC-Plan-ARCHIVE.md](Main-POC-Plan-ARCHIVE.md), no longer above —
> plus
> [ADR-050](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ADR-050-git-certificate-change-log-provenance.md);
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

### Phase 47 — IMPLEMENTED 2026-08-25 — A rejected command must publish no events (BR-050)

Found by inspecting a trace produced during Phase 43e's live verification, but
not a Phase 43 concern: a correctness bug in the write side, filed on its own
number rather than as 43f.

**The bug.** `ShipHandler.ArrivePort` published BR-021's implicit
`evt.…ship.{id}.registered` *before* resolving BR-017's port lookup. An arrival
at an unregistered port therefore returned `ErrUnknownPort` and still committed
a registration to the `LimitsPolicy` SHIPPING stream. The write side rehydrates
by replay, so the ship existed to it; the Postgres projection and KV cache,
having seen only a registration, showed a ship no command had created; and
because an aggregate's surrogate id is immutable once it is in the log, the
natural key was consumed — a later `RegisterShip` failed with `ErrShipExists`.

**Scope.** `ArrivePort` is the only site. It is the only command that emits two
events; every other ship and container command hydrates, validates, and
publishes exactly once (`RegisterContainer` resolves both of its
`ports.Exists` calls before its single publish).

**Design decisions** (approved 2026-08-25):

| # | Decision | Chosen |
|---|---|---|
| 1 | Rule shape | New general rule **BR-050** ("a command that returns an error publishes no events") rather than an ordering clause bolted onto BR-017 — the defect is the class, and it gives a future two-event command something to be checked against. |
| 2 | Fix shape | Structural: compute the arrive event in full (`ports.Exists` → `agg.Arrive()`) before registering, so *every* rejection path precedes any publish. Hoisting only the BR-017 lookup would be correct today but only via an argument about `Arrive()`'s other two rejections implying an already-registered ship — one that must be re-derived whenever a rule is added there. |
| 3 | Test | Strengthen the BR-017 spec to assert the stream is untouched, plus a BR-050 `Context` asserting both an empty stream and that the shipID stays free to register afterwards. Confirmed red first. |
| 4 | Where it lands | Its own numbered phase. Found-by is provenance, not theme. |

- [x] **47a** — three specs added, confirmed red against the unfixed handler
      (`Domain Rules / BR-017 / does not register the ship on the way to
      rejecting it`; `Domain Rules / BR-050 / leaves the stream empty when the
      arrival is rejected`; `… / leaves the shipID free to register
      afterwards`), plus a `streamMsgs` helper.
- [x] **47b** — `ArrivePort` reordered; BR-050 written up and BR-017
      cross-referenced in `BUSINESS_RULES-SHIPPING.md`.
- [x] **47c** — fixed the false-green spec the rule exposed.
      `tenant_switch_test.go`'s reactivation spec claimed to prove the rebuilt
      tenant's projectors were running by arriving a ship and finding it in the
      read model. Its `api.acme.shipping.*` subject resolves the context to
      `acme` (`browserrpc/adapter.go:295` takes the context from the subject
      token, overwriting the body's), which the fake port repo never seeded —
      so the arrival had *always* failed and the ship arrived in the read model
      via the half-committed registration. The spec now seeds `Hamburg` for
      `acme` on a repo shared by both `Deps` fields. This is the second time
      the bug surfaced as a phantom ship, after the `SMOKE-43E` cleanup.
- [x] **47d** — `ginkgo ./...` green across all 9 suites.
- [x] **47e** — *unrelated to BR-050; found while verifying 47 against a
      reseeded stack.* `organizations-service` died on every cold
      `docker compose up -d` after a `down -v`, taking `refdata-frontend`
      down with it (nginx resolves upstreams at startup, so a dead
      `organizations-service` crash-loops it). Temporal has two distinct
      not-ready stages and the service tolerated neither: the frontend not
      listening yet (`client.Dial` → "connection refused"), and the frontend
      listening before `temporal-auto-setup` has created the default
      namespace (`worker.Start` → "Namespace default is not found" — observed
      appearing *one second* after the service gave up). `MountVetting` now
      takes a ctx and retries its **whole construction** — dial, wire, start —
      through one `retryUntilReady` helper, same bounded shape as
      `cmd/main.go`'s `waitForPostgres`, under its own 2-minute budget rather
      than `startupCtx`'s already-running 60s. The first cut probed for those
      two failure modes specifically and was replaced: a point-probe only ever
      covers what someone happened to observe, so anything else arriving late
      (search attributes, say) would still have killed the service. Each
      attempt builds a fresh client and worker via `buildVettingWorker` and
      closes the client on failure; the SDK's `AggregatedWorker` is not
      re-`Start()`ed, since it merges capabilities into state commented
      "should be the only time it is written to" and stops its workflow worker
      if the activity worker fails.
      A `preflightTemporalAddr` fast path sits in front of the loop so a
      typo'd `TEMPORAL_ADDRESS` fails in ~1s instead of burning the full
      budget. It classifies only what can never work — a malformed `host:port`
      or a host that does not resolve — and passes connection-refused,
      timeouts and everything else straight through, because misjudging one of
      those reintroduces the crash this whole change removes. Sound only
      because compose already gives the service
      `depends_on: temporal (service_started)`, so the DNS name exists before
      the process does; the race is *inside* Temporal's startup, not container
      ordering.
      Still fatal if the dependency never arrives — a service answering
      submit-for-vetting with no worker polling strands the workflow.
      Verified by a cold start that hit **both** failure modes through the one
      loop and came up in 2s, with the `organizations-vetting` task queue
      showing live workflow and activity pollers.
      No business rule: this is startup resilience, not domain behaviour.

---

### Phase 48 — APPROVED (design gate closed 2026-08-26; business rules outstanding) — Tenant provenance for `obs.trace.*`

> Closes the one item ADR-047 amendment A1 explicitly deferred: *"Deliberately
> not in scope: applying the same remap to `obs.trace.>`, which would fix the
> existing Traces panel's coarse gutter too. That is a change to a shipped
> pipeline and belongs in its own phase."* This is that phase. It does not
> re-decide A1 — it applies A1's already-proven mechanism to the pipeline A1
> chose not to touch.
>
> **APPROVED 2026-08-26.** Design decisions 1–21 are settled — including
> decision 13's per-span key shape over the revision-CAS alternative offered
> beside it, and decision 18's exclusion of user-level JWT attribution.
> Reopen one only with a stated reason, not by re-deriving it.
>
> **Still gated on business rules.** The five entries under "Business rules
> to confirm" have not been confirmed or supplied, so per CLAUDE.md's
> workflow no tests or code start yet: rules first, then specs derived from
> the rules, then implementation. Sub-phase **48h** is the one exception
> worth noting — it is a test harness rather than a rule-bearing change, so
> it can begin as soon as you want it to, and the sequencing note already
> puts it first.

#### Goal

Give `obs.trace.*` the same server-inserted, unspoofable tenant token that
Phase 43 gave `obs.pubsub.*` (BR-AC34), so a span can be attributed to a
*specific* tenant account rather than a coarse PLATFORM/TENANT split.

Today PLATFORM imports every tenant's `obs.trace.>` onto the identical local
subject with no `LocalSubject` remap. Once N tenants are imported onto one
subject, a PLATFORM subscriber has no indication of origin, and
`natstrace`'s envelope carries no account field to fall back on.
`TraceWaterfall.vue` already documents the consequence in a comment.

**Scope widened 2026-08-26** at your request to carry two defects found in
the same pipeline while tracing it — the unbounded `trace-request-reply`
bucket and `appendSpan`'s write amplification. Both touch the same
projector and the same record, so they are cheaper here than as separate
phases. See decisions 10–15 and sub-phases 48f/48g.

**Scope widened again 2026-08-26** to answer Q1 (there is no multi-span,
cross-account, OK/ERROR trace harness anywhere in the repo) and Q2
(the Admin UI shows no NATS account for either path). Q2 turned out to be
this phase's decision 1 rather than new scope; Q1 is the instrument that
verifies it. See decisions 16–21 and sub-phases 48h/48i.

#### Design decisions — tenant provenance

1. **Mirror `pubsubLocalSubjectTmpl`, don't invent a family.** PLATFORM's
   import of each tenant's `obs.trace.>` gains
   `LocalSubject: monitor.{tenant}.trace.>`, exactly parallel to
   `monitor.{tenant}.pubsub.>`, `monitor.{tenant}.srv.>` and
   `monitor.{tenant}.js.*`. `monitor.*` stays what it already is — a
   PLATFORM-local family that nothing publishes on and no tenant can reach.

2. **The token is inserted by the NATS server, never asserted by the
   publisher.** `jwt.RenamingSubject` on the import is the whole mechanism.
   `natstrace`'s wire envelope gains **no** account/tenant field — an
   envelope field a tenant fills in itself is spoofable, which is precisely
   why A1 rejected that route for pubsub.

3. **Tenant-side publishing does not change.** A service still publishes
   `obs.trace.{context}.{service}.{entity}.{action}`
   (`natstrace.go:521`). `shared/natstrace` needs no change at all — the
   rename happens at the account boundary.

4. **`TRACES` gains a second subject filter rather than replacing the
   first.** The stream captures both `obs.trace.>` (PLATFORM's own services,
   which are already in PLATFORM and cross no import) and
   `monitor.*.trace.>` (every tenant). This is the identical two-filter
   shape `PUBSUB` already runs — `PlatformSubjectWildcard` +
   `TenantSubjectWildcard` — so it is a stream *update*, not a recreate.

5. **Tenant extraction is by subject position at the projector.** Add
   `tenantFromSubject` to `tracestore` mirroring `pubsubstore.go:179`
   verbatim in shape: `parts[0] == "monitor" && parts[2] == "trace"` →
   `parts[1]`, else the `_platform` context constant. Trace subjects are
   fixed-arity, so positional parsing is safe on both the 6-token
   unremapped and 7-token remapped forms.

6. **The KV key shape does not change; the tenant goes in the value.**
   Keep `_platform.trace.{traceId}`. `pubsubstore` already set this
   precedent — `pubsubRecord{Tenant, Span}` carries the tenant inside the
   stored value while the key stays `_platform.msg.{spanId}`. Putting the
   tenant in the key instead would change every Admin UI read path and the
   REST entries surface for no gain, since a trace belongs to exactly one
   tenant and is fetched by `traceId`, never scanned by tenant prefix.
   **Consequence to accept:** the tenant is stored per-span inside the
   merged record, not once per trace — `appendSpan` must not let a later
   span silently rewrite an earlier span's tenant.

7. **No data migration.** `TRACES` is `LimitsPolicy` capped at 1h, so any
   backlog on the old subject ages out within an hour of the change, and
   decision 4 keeps `obs.trace.>` a live filter regardless. Spans already
   merged into KV under the old shape keep their `_platform` tenant until
   they are superseded — acceptable for a diagnostic surface, and visible
   as such rather than silently wrong.

8. **Existing tenants need a re-provision pass.** The remap lives in the
   tenant account's JWT import claims, so already-minted accounts do not
   get it for free. Same rollout shape BR-AC34 needed in Phase 43 — this
   is the real blast radius of the phase, not the code change.

9. **The Admin UI gutter becomes real.** `TraceWaterfall.vue`'s account
   gutter shows the actual tenant name, and the comment documenting the
   coarse split is removed rather than left stale.

#### Business rules — written 2026-08-26

All five are drafted and in place. The design gate is closed; specs derive
from these rules, not from the implementation.

| Rule | File | Covers | Sub-phase |
|---|---|---|---|
| **BR-AC36** | `BUSINESS_RULES-ACCOUNTS.md` | the per-tenant `LocalSubject` remap on the `obs.trace.>` import — the item BR-AC34 explicitly deferred | 48a |
| **BR-051** | `BUSINESS_RULES-SHIPPING.md` | tenant attributed from the arrival subject, never the envelope; `TRACES`'s two subject sets; stored **per span** | 48b, amended 48c |
| **BR-052** | `BUSINESS_RULES-SHIPPING.md` | ~~first-writer-wins on a `traceId` whose spans disagree on tenant~~ — **retired in 48c**, it fires on ordinary cross-account traffic | 48b, retired 48c |
| **BR-053** | `BUSINESS_RULES-SHIPPING.md` | `trace-request-reply` bounded from measurement; one span, one idempotent write | 48f / 48g |
| **BR-054** | `BUSINESS_RULES-SHIPPING.md` | both panels name the originating account; the multi-span cross-account harness that proves it | 48h / 48i |

Two things settled in the writing that the drafts above had left open:

- **The conflict rule took the proposed shape** — first writer wins, mismatch
  logged at warn level, the disagreeing span still stored so the evidence is
  not hidden. Last-write-wins is what a plain struct assignment does by
  default, which is why BR-052 existed as a rule rather than a comment.
  *(Retired in 48c: both options were wrong, because the premise — one
  tenant per trace — was. See 48c's note below.)*
- **BR-054 absorbed the user-JWT exclusion** (decision 18) as part of the
  rule rather than leaving it as a design note, so the reason is recorded
  where someone adding a "user" column would read it.

#### Sub-phases — tenant provenance (draft — not started)

- [x] **48a** — `accounts-service`: add the `LocalSubject` remap to the
      trace import (`addPlatformTraceImport`), with a
      `traceLocalSubjectTmpl` constant beside `pubsubLocalSubjectTmpl`.
      Provisioner tests assert the minted claim, mirroring BR-AC34's.
      **DONE 2026-08-26**, plus the day-0 half in `bootstrap-operator.sh`
      that BR-AC36 names. Two things came out of the implementation:
      **(a)** re-provisioning had to be made to *converge*. The dedupe
      scan every other import uses matches on `(Account, Subject)` alone,
      so a pre-BR-AC36 account's un-remapped import would be found,
      reported as success, and left unchanged — making decision 8's
      re-provision pass a no-op and a wipe the only fix. It now corrects a
      wrong `LocalSubject` in place; there is a spec for it, verified red.
      **(b) An ordering constraint 48d must respect:** with the remap live
      and `TRACES` still filtering only `obs.trace.>`, tenant spans are
      remapped away from the only subject the stream captures and the
      Traces panel goes empty **for tenants only** — PLATFORM's own spans
      keep arriving, which is what makes it read as "no traffic" rather
      than as breakage. Nothing is disturbed until someone reseeds
      (`--force` + `down -v`), so **do not reseed until 48b lands.**
- [x] **48b** — `tracestore`: second subject filter on `TRACES`,
      `tenantFromSubject`, tenant threaded into the stored record, and the
      decision-6 conflict rule. **DONE 2026-08-26** — BR-051 + BR-052.
      Five specs, all verified red against a deliberately broken
      implementation first. Two things worth carrying forward:
      **(a)** the projector's consumer now names **no** `FilterSubject`
      rather than being taught the second one. It carried
      `FilterSubject: obs.trace.>`, which was harmless while the stream
      had one subject and would have silently swallowed every tenant span
      the moment the second was added — the exact failure 48a warned
      about, one layer further in. An unfiltered consumer is what this
      projector actually means, and `pubsubstore`'s already omits it.
      **(b)** No migration: a record stored before this has no `tenant`
      field, so it unmarshals to the arriving span's own attribution and
      corrects itself on the next span. **48a's reseed hazard is now
      cleared** — the stream captures both subject shapes, so 48d can
      re-provision and reseed. **Superseded in part by 48c:** BR-052 was
      retired and the record-level tenant moved onto each span — see below.
- [x] **48c** — Admin UI: real tenant in `TraceWaterfall.vue`'s gutter;
      remove the stale comment; specs for the new attribution.
      **DONE 2026-08-26** — BR-051 (amended) + BR-054's traces-panel half;
      BR-052 retired. Building the gutter exposed a defect in what 48b had
      just landed, so this sub-phase reworked part of it (user decision:
      "per-span tenant, retire BR-052"):
      **(a)** the trace this stack actually produces is *mixed*, not
      single-tenant — `organizations-service` runs tenant-scoped
      connections and `refdata-service` runs `platform.creds`, so one
      `api.*` call yields two `acme` spans and one `_platform` span. A
      per-trace tenant would label refdata's PLATFORM span `acme`: a wrong
      attribution on the panel whose whole point is trustworthy
      provenance, and worse than the coarse split it replaced.
      **(b)** BR-052 fired on that same legitimate traffic — it called two
      tenants under one `traceId` "never normal traffic" and would have
      warned on every cross-account request. Retired, with the reasoning
      kept in `BUSINESS_RULES-SHIPPING.md` rather than deleted.
      **(c)** a stored element is now `{tenant, span}`, mirroring
      `pubsubRecord`. This one *does* need a decoder: the default struct
      decoding reads a pre-48c bare span into an empty wrapper and
      silently drops it, so `storedSpan.UnmarshalJSON` probes for a `span`
      key. An unattributed span renders as `unattributed`, never as a
      guess.
      **(d)** `cmd/seed-traces` was updated in the same pass — it decodes
      the record, so the wrapper would have broken it — and its
      `-expect-tenant` now asserts the *correct* property (the tenant
      appears, `_platform` is the only other value, nothing unattributed)
      rather than uniformity, which this chain rightly violates.
      Every spec verified red first, in both languages. 48g's per-span
      keys were going to touch this code anyway.
- [x] **48d** — re-provision pass for existing tenant accounts (decision 8),
      and live verification across two tenants that spans land under
      distinct tenant names.

      **DONE 2026-08-26.** (1) Decision 8's manual pass was replaced by
      boot-time convergence (BR-AC37): the four `addPlatform*Import` calls
      are reached through one exported `EnsurePlatformImports`, which
      `seedPreexistingAccounts` now runs per tenant on every start. Each
      call is a no-op when the claim is already correct, so a healthy
      stack signs and pushes nothing — that property is what makes it safe
      unconditionally, and it is the spec that guards it. (2) The
      idempotency spec had to observe `$SYS.REQ.CLAIMS.UPDATE` directly: a
      jti is a content hash of the claims, so the obvious
      compare-the-JWT-before-and-after check passes against a provisioner
      that re-signs unchanged claims every time — it was written that way
      first and caught by red-checking it. (3) Live: PLATFORM's runtime
      claims in `/data/jwt` gained `obs.trace.> -> monitor.acme.trace.>`
      and `-> monitor.globex.trace.>` on the first restart after the
      change, without a reseed. `cmd/seed-traces` then passed OK and ERROR
      for both `-expect-tenant acme` and `-expect-tenant globex`. (4) The
      harness's `-settle` default went 10s → 30s: the projector was
      measured taking 10–13s to store a three-span chain, so the old
      default intermittently reported a late trace as a missing one.
- [x] **48e** — docs: `ARCHITECTURE-OBSERVABILITY.md` (A1's deferred item
      marked closed), `ARCHITECTURE-COMMUNICATIONS.md` §6 notes and the
      `observability-message-path` diagram — the trace row's `no remap`
      annotation becomes the remap, which is the whole point of the phase.
      **DONE 2026-08-26.** A1 now carries a resolution note naming the three
      things the implementation settled that the ADR did not anticipate
      (per-span attribution and BR-052's retirement; convergence rather than
      one-off re-provisioning, BR-AC37; no migration needed because `TRACES`
      is `LimitsPolicy`), plus a new action item 5. §6 gained notes on
      subject-derived per-span attribution, on each stream carrying two
      subject sets, and on both buckets being bounded at 15 min / 8 MiB.
      The diagram lost two of its three divergences: both rows now draw the
      same `LocalSubject remap` and the same bucket bound, so the only
      difference left is merge-vs-overwrite, and the retired pair is
      recorded in a "what used to diverge" bullet rather than deleted.
      Re-exported at 1024px with `--clip=".wrap"`; `audit-svg-layout.mjs`
      clean. Two stale sentences in `BUSINESS_RULES-ACCOUNTS.md` and
      `BUSINESS_RULES-SHIPPING.md` that asserted the trace import "has no
      remap" in the present tense were put into the past tense — outside the
      sub-phase's literal scope, but they were wrong as written.

#### Design decisions — bucket bounding (added 2026-08-26 at your request)

10. **`trace-request-reply` gets a `TTL` and a `MaxBytes`, for the reason
    `pubsub-messages` already has them.** It is created today as a bare
    `jetstream.KeyValueConfig{Bucket: bucketName}` (`tracestore.go:87`) —
    no bound of any kind — while the `TRACES` stream feeding it is capped at
    1h/64 MiB. The stream forgets and the bucket does not, so the bucket is
    the only unbounded thing in the whole path. `pubsubstore.go:82` states
    the governing reason: the panel **bootstrap-fetches every entry in the
    bucket on load**, so bucket size is a page-load cost, not just disk. The
    Traces panel does the same fetch, so it inherits the same argument.

11. **The bound is tighter than the stream's, and it is measured, not
    guessed.** `pubsub-messages` landed on 15 min / 8 MiB from a seed run
    over representative payloads (BR-047's reasoning). A trace record is a
    *merged multi-span* document, not a single envelope, so pubsub's numbers
    do not transfer. **Proposed starting point: 30 min / 16 MiB**, to be
    replaced by whatever a seed run over representative `rpc.*` traffic
    actually measures — the sub-phase produces the number, this plan does
    not assert it.

12. **Applied in place via `CreateOrUpdateKeyValue`, no recreate.** A KV
    bucket's TTL and `MaxBytes` are its backing stream's `MaxAge` and
    `MaxBytes`, both updatable on a live stream, so the existing bucket
    keeps its name and contents. Renaming is explicitly *not* on the table
    (CLAUDE.md: "a bucket name is a stream name" — renaming orphans the old
    stream and creates an empty one).

#### Design decisions — `appendSpan` write amplification (added 2026-08-26 at your request)

13. **Replace the read-modify-write merge with one key per span.** Today
    `appendSpan` reads the whole trace record, appends one span, dedupes by
    `spanId`, and writes the whole thing back — so a trace of *n* spans
    writes O(n²) bytes, and every append is a lost-update race that survives
    only because a single durable consumer happens to be the only writer.
    That is a property of the current deployment, not a guarantee of the
    design: it breaks the moment the projector is scaled or a redelivery
    overlaps. Proposed shape: key `_platform.trace.{traceId}.{spanId}`,
    a plain idempotent `Put`, with the trace assembled at read time from a
    key-prefix scan — which is what makes dedup-by-`spanId` free rather than
    a merge step, since the same span overwrites its own key.

14. **This changes the read path and the `notify.*` key, so it moves with
    the frontend.** `useTraceFeed.js` currently keys entries by `traceId`
    and the projector notifies on
    `notify._platform.kv.trace-request-reply.trace.{traceId}.changed`.
    Per-span keys mean a prefix-grouped read and a per-span notify subject.
    **This is the one genuinely risky part of the phase** — it touches the
    surface Phase 43e's reconnect/resync work just stabilised, and the
    monotonic-merge guard in the feed has to keep holding across the new key
    shape.
    **Alternative if you'd rather not move the read path:** keep the single
    merged key and make the write safe with a revision-CAS `Update` plus
    bounded retry. That fixes the *race* but not the O(n²) *write
    amplification* — worth choosing deliberately rather than defaulting.

15. **`TRACES` should also set `Duplicates` explicitly**, the way ADR-047 A6
    made `PUBSUB` do (`pubsubstore.go:66`) rather than inheriting the
    server's 2-minute default. Noticed while reading the two stream configs
    side by side; small, and it belongs with 48g's dedup work rather than in
    its own phase.

#### Additional sub-phases

- [x] **48f** — bound `trace-request-reply`: seed run to measure a real
      trace-record size, `TTL` + `MaxBytes` on the bucket config
      (decisions 10–12), and a spec asserting the bucket is created bounded
      — the kind of regression that is invisible until disk fills.
      **DONE 2026-08-26.** Measured with a new `seed-traces -measure
      -runs N` mode (the sizing input BR-053 names): 1,638 records, mean
      997 B, 1-span p90 1.5 KiB, 3-span p90 3.2 KiB — about 1.1 KiB per
      span. Landed on **15 min / 8 MiB**, tighter than decision 11's
      proposed 30 min / 16 MiB, which the measurement did not support;
      15 min matches `pubsub-messages` so a message and its trace stay
      cross-referenceable in the two panels. `TestRegisterCreatesBoundedBucket`
      verified red against the bare `KeyValueConfig{Bucket}` first. Decision
      12 held — `CreateOrUpdateKeyValue` applied it in place, no recreate —
      with one fact worth knowing: a `MaxAge` is retroactive, so the live
      bucket went 1,638 → 133 records on the first restart. Decision 15's
      `Duplicates` on `TRACES` stays with 48g as sequenced.
- [ ] **48g** — `appendSpan` rewrite per decisions 13–15, including the
      `useTraceFeed.js` read-path change, and a concurrency spec that fails
      against today's read-modify-write (two overlapping appends to one
      `traceId` must not lose a span).

> **Sequencing note:** 48f is small, self-contained, and independent of the
> tenant-provenance work — it could ship first or even separately. 48g is
> the opposite: it should land *after* 48b, because both touch the same
> record shape and doing them in one pass avoids rewriting `appendSpan`
> twice.

#### Design decisions — account provenance in the Admin UI (Q2)

16. **The account is read from the subject, never from the envelope.**
    `natstrace`'s `traceSpan` carries `Requester`
    (`shared/natstrace/natstrace.go:161`) and its own comment says why it
    cannot answer this: it "lifts BR-041's self-declared `Nats-Requestor`
    header onto its own field so the Admin UI can read it **without treating
    it as authorization**". There is no account or JWT field on the envelope
    at all. Adding one would be publisher-asserted and therefore spoofable —
    exactly the objection ADR-047 A1 raised for pubsub, which is why pubsub
    got a server-inserted subject token instead. So **Q2 is not separate
    scope: it is decision 1**. `monitor.{tenant}.trace.>` gives request/reply
    the same unspoofable provenance pubsub already has, decision 5's
    `tenantFromSubject` lifts it, decision 9 renders it.

17. **Pubsub needs a display change only.** The tenant is already stored on
    the record (`pubsubRecord{Tenant, Span}`, `pubsubstore.go:192`) and has
    been since ADR-047 — the Messages panel simply doesn't surface it. No
    backend work on that path.

18. **The *user* JWT is deliberately out of scope.** NATS does not put the
    signing user's subject on a published message, so identifying *which
    user within the account* would mean either correlating against `$SYS`
    connz or having the publisher assert it — the same spoofable-assertion
    problem one level down. Account-level provenance is what makes the panel
    trustworthy; user-level attribution is a separate design, and folding it
    in here would quietly reintroduce the assertion model this phase exists
    to avoid.

#### Design decisions — cross-account multi-span trace harness (Q1)

19. **Nothing exercises a multi-hop trace today, and that is the gap.** The
    only `cmd/` CLIs are the four data seeders. Trace coverage is per-service
    and single-hop — `trace_async_test.go`, `trace_middleware_test.go`,
    `browserrpc_test.go`, `natsrpc_test.go`,
    `browserrpc_roundtrip_test.go` — each asserting that *its own* span is
    emitted correctly. None builds a `traceId` with a real parent/child chain
    across two services, and none crosses an account boundary. The Admin UI's
    waterfall (parent/child nesting, span ordering, ERROR row styling) has
    therefore never been checked against a trace it did not hand-construct in
    a fixture.

20. **The harness drives the real stack, not fixtures.** A `cmd/seed-traces`
    (or a Ginkgo integration spec against the running compose stack) issuing
    a genuine `api.*` call with browser credentials from a tenant account →
    `shipping-service` → `rpc.*` into `refdata-service`, run twice: once with
    a valid payload (every span `StatusCode` OK) and once with a payload the
    callee rejects, so the error span carries `StatusCode`/`StatusMessage`
    **and the parent still closes**. Hand-built fixtures would re-assert what
    the existing unit specs already cover.

21. **This is the phase's verification instrument, not an extra.** Decisions
    5, 6 and 13–14 cannot honestly be verified without it: tenant attribution
    needs a trace that actually crosses an account boundary, and the
    `appendSpan` rewrite needs a trace with several spans arriving
    concurrently under one `traceId`.

#### Additional sub-phases — Q1/Q2

- [x] **48h** — cross-account multi-span trace harness per decisions 19–21:
      OK and ERROR runs, driven against the compose stack, plus assertions on
      the resulting stored trace (span count, parent/child linkage, the error
      span's status fields). **DONE 2026-08-26** —
      `observability-service/cmd/seed-traces`. Both runs green against the
      live stack: 3 spans each, root parented to the harness's own injected
      `traceparent`, spans from both `organizations` and `refdata`. Two
      corrections to BR-054 came out of building it, and are now written into
      the rule: the chain is `organizations-service` → `refdata-service`
      (Phase 32 left shipping-service's `refdataconsumer` with no live
      callers, so the chain the rule originally named is dead), and it drives
      the hop with the tenant's own credential because `connectInfo` 409s
      with *"tenant has no signing key on record"* for the seeded accounts.
      The error run's real span shape — root ERROR, caller-side hop OK,
      refdata's handler span ERROR — is recorded in BR-054 rather than
      asserted around. Tenant attribution is behind an opt-in
      `-expect-tenant` flag and currently reads `(none)`, the expected red
      state until 48b lands.
- [ ] **48i** — surface the account in the Admin UI for both paths
      (decisions 16–17): real tenant in `TraceWaterfall.vue`'s gutter — the
      same work as 48c, listed here because it is what actually answers Q2 —
      and a tenant column/badge in the Messages panel from the record field
      that already exists.

> **Sequencing note (Q1/Q2):** 48h should land early — before 48b — because
> it is the only thing that can demonstrate the tenant token actually arrives
> end to end, and it is equally useful as a regression net for 48g. 48i is
> the tail of 48c and 48b; it has no independent backend work.

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
- [ ] 24b: `docker-compose.yml` — named volume replacing the `./nats/creds` bind mount across every consumer, **including `observability-service`'s `observability.creds` mount** (added in Phase 30c, after this sub-phase's original scope was written)
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
- [ ] **From Phase 26 (Organizations Service) — deferred items, carried
      forward as named open questions, not silently dropped:**
      lifecycle-as-CQRS/temporal exploration, `ComplianceDocument` temporal
      classification, document-expiry-driven status, real file storage,
      terminal/offboarding state, platform-identity vs tenant-membership
      split, `notify.*` publication once a marketplace consumer exists.
      Intentionally open-ended — a list to revisit if/when any of these
      becomes a real requirement, not a task with a completion criterion.

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
