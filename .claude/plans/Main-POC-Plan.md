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

### Phase 51 — APPROVED / IN PROGRESS (follows Phase 50) — Credential Revocation and Connection Outcome

> **Status: APPROVED 2026-08-27 — design gate passed.** The three open
> design questions were resolved the same day and folded in below, and
> the design as it now stands was approved for implementation of **both**
> 51a and 51b. The proposed business rules (BR-062, BR-AC42, BR-AC43)
> are approved to be written into their domain files.
> Prompted by a 2026-08-27 comparison against Synadia's `OPT_IDLE_006`
> ("NATS Disconnected Users") check, which reaches the same
> inventory-x-`/connz` join Phase 50 built, but treats a disconnected
> credential as an *actionable finding* with a *revocation* remedy rather
> than as a display state.

#### Goal

Phase 50 made NATS users enumerable. It did not make them *actionable*. Two
gaps fall out of that, and they are independent — either can ship alone:

- **A `0/0` row is ambiguous.** The panel cannot distinguish "this
  credential has never been used" from "this credential disconnected ninety
  seconds ago". Both render as zeros.
- **There is no revocation path anywhere in the stack.** `Store.DeleteUser`
  exists, has **no caller outside tests**, and its own header comment says it
  is *"Not a revocation"* — deleting a registry row leaves the credential
  working. So the honest answer to "this credential should stop working" is
  currently *edit the account by hand with `nsc`*.

The second is the one that matters. Roster noise is cosmetic; the absence of
revocation is a missing capability.

#### Verified 2026-08-27 against the running stack and `jwt` v2.8.2 — read this before revising the design

Four facts established empirically rather than assumed. The first one
partly undercuts 51a as originally drafted.

1. **`/connz?state=closed` is feasible and carries everything the join
   needs** — a closed entry has `jwt`, `authorized_user`, `stop`,
   `reason`, `last_activity`, so BR-058's decode path and BR-060's NKey
   join both work unchanged.
2. **The closed ring is ~100% session churn.** Observed: 59 closed
   connections, every one a short-TTL session JWT, 31 distinct NKeys,
   **zero** long-lived service credentials — the service credentials
   never disconnect. Reasons: 35 `Authentication Expired`, 19 `Client
   Closed`, 5 `Authentication Failure`. **Consequence: a last-seen
   *timestamp* would be blank on exactly the credential rows it was
   meant to explain.** `reason` is the field with value — a refused
   connection (`Authentication Failure`) is invisible in the Admin UI
   today, and is also precisely what a revoked credential produces,
   which makes 51a the verification surface for 51b.
3. **A revocation entry for one of our credentials is permanent.**
   `RevocationList.MaybeCompact` prunes *only* entries superseded by a
   `*` (`jwt.All`) revocation — there is no expiry-based pruning. Our
   credentials are `expires_at IS NULL`, so every per-credential
   revocation is a permanent addition to the account JWT pushed to the
   resolver. Also confirmed: `IsRevoked` compares `ts >= iat`, so
   revoking at `time.Now()` has **no same-second escape gap**.
4. **`*` (revoke-every-user-in-an-account) is redundant here.**
   `suspendAccount` already issues `$SYS.REQ.CLAIMS.DELETE` against the
   account JWT, taking the whole tenant offline, and is reversible via
   `reactivate`. Per-user is the only new grain 51b needs.

#### Design decisions

**51a — connection outcome (`/connz?state=closed`)**

- **Source stays in observability-service.** It already owns the NATS-monitor
  HTTP dependency; Phase 50 deliberately kept accounts-service free of one
  and that decision stands. This extends `rest/nats_connections.go`, not the
  `api._platform.accounts.user.*` subjects.
- **Join key is unchanged** — the user public NKey, per BR-058.
- **`reason` is the primary signal, `stop` is supporting detail** — see
  finding 2. Drafted originally as a last-seen timestamp; that framing
  did not survive contact with the data. *(Confirmed 2026-08-27.)*
- **`reason` is shown for every row kind; `stop` only for
  `kind=credential`.** *(Decided 2026-08-27.)* The credential-only
  scoping inherited from Phase 50 was an argument about *timestamps*
  being meaningless for rotating session NKeys; it does not transfer to
  a refusal reason. Sessions are where essentially the whole closed ring
  lives (finding 2) — including all five `Authentication Failure`
  entries — so scoping reason to credentials would discard the rows that
  generate nearly all of it.
- **Best-effort, and must present as such.** The closed-connection ring
  is bounded and finding 2 shows session churn is what fills it, so an
  absent entry means "outside the retained window", **not** "never
  connected" — the same honesty constraint as the existing
  `partial · /connz paged` note, surfaced the same way.

**51b — Revocation**

- **Mechanism is the account JWT's revocation list**, not row deletion:
  `jwt.AccountClaims.Revocations.Revoke(pubKey, time.Now())`, re-sign the
  account JWT with the operator signing key, push via
  `$SYS.REQ.CLAIMS.UPDATE`. `Provisioner` already holds all three
  prerequisites (operator signing key, `sysNC`, `LookupAccountClaims` at
  `provisioner.go:461`). No revocation code exists anywhere in the repo
  today — verified.
- **This is BR-AC38's continuation, not a new claim.** That rule's closing
  paragraph already states this mechanism as the thing deletion *is not*,
  and lists revocation under "Not covered".
- **It disconnects live clients** when the claims update lands — so the UI
  must confirm, naming the credential and its current connection count.
- **The registry gets a `revoked_at TIMESTAMPTZ NULL` column — not a
  `revoked` status.** *(Revised 2026-08-27; the first draft of this phase
  said `status = revoked`.)* `UserStatus` is documented at `users.go:65`
  as "the mint outcome as the issuer knows it", and a revocation is not a
  mint outcome. Overloading it also destroys information: a credential
  revoked while stuck at `pending` is the alarming case, and BR-AC38
  explicitly names revocation as the resolution for a stuck `pending`
  credential, so that combination must stay representable. A timestamp
  column additionally mirrors the JWT list's own (key -> timestamp) shape,
  making drift between registry and account JWT detectable.
- **Scope to `kind=credential`.** Revoking a 15-minute session is pointless.
- **PLATFORM-only**, same server-enforced account boundary as the Phase 50
  read path — a new `api._platform.accounts.user.revoke.v1` mounted on the
  PLATFORM connection, not a handler check.
- **No actor is recorded.** There is no operator identity in this POC — the
  Admin UI connects as the shared `admin-app` credential — so a
  `revoked_by` column would record the same worthless string every time
  and imply an audit trail that does not exist.
- **Revocation is terminal — the panel exposes no un-revoke.** *(Decided
  2026-08-27.)* `jwt.RevocationList.ClearRevocation` exists and would
  reuse the same re-sign-and-push path, but it is deliberately not
  surfaced: recovery from a mis-revocation is minting a replacement
  credential. That keeps the account JWT's revocation list append-only
  in practice as well as in mechanism, and matches the mental model that
  a revoked key is burned. Accept the consequence knowingly — revoking a
  service credential by mistake takes that service down until a new
  credential is minted and mounted, and per finding 3 the entry it
  leaves behind in the account JWT is permanent. Whole-tenant recovery
  remains available (`suspend`/`reactivate`, finding 4); per-credential
  recovery does not.

#### Open questions — resolved 2026-08-27

All three are answered and folded into the design decisions above; kept
here so the reasoning is not lost, and so a later revision does not
reopen them by accident.

1. **Does the BR-062 reframing land?** **Yes** — refusal/disconnect
   *reason* is the primary signal, not a last-seen timestamp, per
   finding 2.
2. **Does `reason` cover sessions too, or credentials only?** **All
   kinds**, with `stop` detail on `kind=credential` only.
3. **Is revocation reversible from the panel?** **No** — revocation is
   terminal; recovery is a replacement credential, not `ClearRevocation`.

#### Business rules to confirm before any code (per CLAUDE.md's rules-first workflow)

These are **proposed, not agreed** — this is the step the workflow asks
for. They are no longer blocked on open questions (all three resolved
above); they are blocked only on approval of this design:

- **BR-062** (`BUSINESS_RULES-SHIPPING.md`, joining BR-060/061) — a row with
  no live connections shows *why* its most recent connection ended or was
  refused, when the server still remembers. The reason is shown for every
  row kind (credentials and sessions alike); the `stop` time is shown only
  for `kind=credential`. An absent entry reads as "outside the retained
  window", never as "never connected".
- **BR-AC42** (`BUSINESS_RULES-ACCOUNTS.md`) — revocation is by public key
  into the issuing account's JWT revocation list and is server-enforced. The
  registry mirrors it via `revoked_at` and the row is **never deleted**;
  deleting a row is not and never has been a revocation. A revocation entry
  for a never-expiring credential is permanent (finding 3); whole-account
  revocation is `suspend`, not this (finding 4).
- **BR-AC43** — revoking disconnects live connections using that credential.
  It requires explicit confirmation naming the credential and its current
  connection count, is restricted to `kind=credential`, is served
  PLATFORM-only, and records no actor. It is **terminal**: there is no
  un-revoke, and recovery from a mistaken revocation is minting a
  replacement credential.

#### Checklist (not started — gated on approval above)

- [x] 51a: `/connz?state=closed` added to observability-service's monitor read
- [x] 51a: disconnect/refusal `reason` surfaced per row (all kinds), `stop` detail on `kind=credential` only; absent value presented as "outside retained window"
- [x] 51a: `BUSINESS_RULES-SHIPPING.md` — BR-062
- [x] 51b: `Provisioner.RevokeUser` — revoke, re-sign, `$SYS.REQ.CLAIMS.UPDATE` push
- [x] 51b: `revoked_at` column added to `accounts.users`; `MarkUserRevoked` store method
- [x] 51b: `api._platform.accounts.user.revoke.v1`, PLATFORM connection only
- [x] 51b: confirmation dialog naming credential + live connection count
- [x] 51b: `BUSINESS_RULES-ACCOUNTS.md` — BR-AC42, BR-AC43, each with a Ginkgo `Context`
- [x] 51a: live verification — `GET :7205/api/nats/connections/closed` returns 75 closed
  connections with `reason`/`stop`/`userKey` and no `jwt`; the Users panel renders the
  **Last outcome** column (`Authentication Expired`, `Client Closed`, "outside the
  retained window", suppressed while a row has live connections)
- [ ] 51b: live verification — revoke a credential, confirm the server drops its connection and that the reconnect attempt surfaces in 51a's panel as `Authentication Failure`
  (2026-08-27: everything up to the press is verified against the running stack — the
  `revoked_at` migration applied, the adapter logs all three subjects including
  `.revoke.v1`, and the confirmation dialog renders with the credential name, account,
  elided NKeys, live connection count and the "cannot be undone" warning. The press
  itself is left for the operator: all six credentials in the registry are load-bearing
  in the running stack and revocation is terminal, so confirming one costs a
  `docker compose down -v` + reseed to recover.)
- [x] `ginkgo ./...` green in `accounts-service` and `shipping-service` (2026-08-27: accounts 143+31 specs, shipping all suites, `Test Suite Passed`; admin `vitest run` 222 tests)

---

### Phase 52 — COMPLETE (follows Phase 51) — Reaping Expired Sessions from the User Registry

> **Status: APPROVED and IMPLEMENTED 2026-08-27 — design gate passed the
> same day it opened.** The three open questions were answered: retention
> **24h**, `hide expired` default **OFF**, pagination **deferred**.
> Prompted by the live reading taken at the end of Phase 51: **56 registry
> rows, 44 of them expired sessions**, after roughly one day of stack
> uptime, with nothing in the stack deleting them.

#### Goal

`accounts.users` grew without bound. A browser session mints a row on every
connect (BR-AC38) with a 15–30 minute TTL (BR-AC20), and the row outlived the
credential by forever. The only sweep that existed —
`Store.SweepExpiredPendingUsers`, called once at start-up — deleted
`status = 'pending'` rows only, the mint-crash case rather than the ordinary
one. Phase 51's `hide expired` checkbox made this survivable in the panel but
not in the table.

#### Design decisions (as approved)

**D1 — Two knobs, not one. Cadence is not retention.**

- **Retention** (`ACCOUNTS_SESSION_RETENTION`, **24h**) — how long a row
  survives past its own `expires_at`. The operator knob. A session that
  expired four minutes ago is exactly the row someone is reading when they
  ask "why did my tab drop"; reaping on expiry destroys the answer at the
  moment it is wanted. 24h is roughly how long the row stays *explainable* —
  past it, the `/connz` closed ring feeding the **Last outcome** column
  (bounded, measured at ~59 entries) has rolled over.
- **Cadence** (`ACCOUNTS_REAPER_INTERVAL`, **15m**) — a load knob only. One
  minimum-TTL period; ~1% overshoot against a 24h window.
- Retention `0` **disables** the reaper rather than meaning reap-on-expiry;
  a negative or unparseable value is fatal at boot.

**D2 — What may be deleted is a whitelist:** `kind = 'session'` AND
`expires_at` already past AND `revoked_at IS NULL`. A revoked row is never
reaped at any age — it is the only human-readable mirror of an entry in the
account JWT's revocation list, which keeps the NKey forever. A pending row
keeps its existing no-grace semantics. The old start-up-only sweep folded in.

**D3 — Start-up run first, then the ticker.** A stack down for a week must
not wait out an interval, and the start-up run surfaces a misconfiguration
now rather than fifteen minutes from now.

**D4 — A failing tick logs and waits for the next one.** A reaper that dies
on the first Postgres blip has silently stopped reaping. Silent when it
removes nothing.

**D5 — Pagination deferred.** With D1 in force the table is bounded at
`mint-rate x retention`. Pagination costs the panel's honest counting: the
`hide expired` / `hide idle` chips can report a held-back count only because
the client holds the whole population, so a paged `.list.v1` would need
server-computed totals shipped alongside every page or the counts start
lying. Buy it with a measurement, not a hypothesis.

#### Two defects the default flip exposed — worth recording

Turning `hide expired` off did not merely change a default; it uncovered two
bugs that the ON default had been masking.

1. **`Authentication Expired` was being grouped as a refusal.** It matches
   BR-062's `/Authentication|Authorization|Revoked/i` colour test textually,
   but it is the server's word for a session whose TTL ran out — the ordinary
   end of every browser session here, and 35 of the 59 reasons in the Phase 51
   measurement. Because `isQuietlyIdle` deliberately treats a refusal as
   never-idle (so BR-062's rows survive `hide idle`), this made `hide idle`
   structurally unable to hide the largest class of dead rows. Invisible while
   `hide expired` claimed those rows first; the flip exposed it as **41 rows
   visible on a roster with 8 live connections**. Fixed with an `Expired`
   exclusion that runs before the refusal test.
2. **A chip reported a held-back count while holding nothing back** — "51"
   beside an unchecked box, which reads as "51 hidden" when nothing is. Each
   chip now renders its count only while it is actually filtering.

The lesson worth keeping: **two filters that overlap can hide each other's
bugs, and changing which one runs first is a real test of both.**

#### Business rules

- **BR-AC44** — an expired session row is reaped once older than the
  retention window past its `expires_at`; credentials and revoked rows never
  are. Written into `BUSINESS_RULES-ACCOUNTS.md`.
- **BR-AC45** — the reaper runs at start-up then on a fixed interval; a
  failed run never stops it, but a bad config is fatal at boot. Written into
  `BUSINESS_RULES-ACCOUNTS.md`.
- **BR-060 amended** — `hide expired` now defaults OFF, and the
  non-double-counting rule restated for the case where it is off.
- **BR-062 amended** — the `Expired` exclusion, and why it has to come first.

#### Tasks

- [x] `Store.ReapExpiredSessions(ctx, retention)` — the whitelist DELETE,
  subsuming `SweepExpiredPendingUsers` (`accounts/users.go`)
- [x] `ReaperConfig` / `ReaperConfigFromEnv` / `SessionReaper` — defaults,
  explicit-zero-is-off, reject-rather-than-fall-back, start-up run, ticker,
  retry-on-failure (`accounts/reaper.go`)
- [x] Wired in `cmd/main.go` as a goroutine on the process context, fatal on
  a bad config; both variables added to `docker-compose.yml` at their
  defaults so they are discoverable there
- [x] `hideExpired` default flipped to `false` (`UsersPanel.vue`)
- [x] `isRefusal` excludes `Expired`; chip counts gated on the filter
  actually filtering (`UsersPanel.vue`)
- [x] BR-AC44 / BR-AC45 written into `BUSINESS_RULES-ACCOUNTS.md`; BR-060 and
  BR-062 amended in `BUSINESS_RULES-SHIPPING.md`
- [x] Specs: `accounts/reaper_test.go` (11), `accounts/users_store_test.go`
  BR-AC44 (6), `UsersPanel.spec.js` (54 total)
- [x] `ginkgo -r` green — accounts **160 + 31**, shipping **9 suites**;
  admin `vitest run` **247 tests / 25 files**
- [x] Live verification (2026-08-27): reaper logged
  `session reaper started retention=24h0m0s interval=15m0s` and reaped
  nothing, correctly — the predicate dry-run against the live registry
  returned `24h → 0, 8h → 3, 4h → 19, 1h → 36, 0 → 51`, monotonic and never
  exceeding the 51 expired sessions, with the 6 credentials and 2 live
  sessions untouched at every retention. Panel at 1920x1080: 62 users,
  `hide expired` unchecked with no count shown, `hide idle` holding back 46,
  **16 rows visible = 6 credentials + 2 live sessions + 8 genuine
  `Authentication Failure` rows** kept by the refusal exemption.
- [ ] **Deferred to a later phase (D5):** pagination on `.list.v1`, with
  server-computed population totals so the filter counts stay honest. Open
  it only if a re-measurement shows the bounded table is still too big to
  ship whole.

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
