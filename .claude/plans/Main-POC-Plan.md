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
Bucket names:   dict-a-{context}  (Shape A read model)
                dict-b-{context}  (Shape B cache)
Key format:     {entityType}.{id}
Value:          JSON-encoded DictionaryEntry
```

> **Implementation finding:** the original `{entityType}:{id}` key format is
> invalid — NATS KV keys only allow `[-/_=.a-zA-Z0-9]`, so `.` is used as the
> separator. Buckets were also split per shape (`dict-a-*` / `dict-b-*`) so
> the two projections stay independent for the side-by-side comparison.

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

Full detail archived in [Dictionary-POC-Plan-ARCHIVE.md](Dictionary-POC-Plan-ARCHIVE.md)
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

Full detail archived in [Dictionary-POC-Plan-ARCHIVE.md](Dictionary-POC-Plan-ARCHIVE.md)
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

### Phase 15 (15a/15b/15c/15d) — Browser NATS WebSocket Transport

#### Goal

The Admin UI's ~5 long-lived SSE connections per tab exhaust Chrome's 6-connection-per-origin
HTTP/1.1 limit — a second tab to the same origin hangs indefinitely. Rather than work around the
limit (HTTP/2, `visibilitychange`, `BroadcastChannel`), make the browser a first-class NATS
client: one WebSocket connection (`ws://localhost:9222`, already exposed for `nats-ui`) replaces
all SSE streams and REST calls for the **Sea Freight Flow** ("public") frontend. Admin/Dictionary
("internal") frontends and cross-account imports (DEFAULT-account streams like RPCTRACE/REFDATA)
are explicitly out of scope for this phase.

Browser interaction model: `rpc.{ctx}.shipping.{entity}.{action}.v1` (request/reply, replaces
REST) and `notify.{ctx}.shipping.{entity}.changed` (pub/sub with the full projected entity as
payload, replaces SSE/KV-watch). See
[admin_ui_realtime_transport_options.md](../memory/admin_ui_realtime_transport_options.md) for the
design discussion that led here.

#### Sub-phases

- **15a — shipping-service natsrpc server**: new `dictionary/internal/natsrpc/` adapter (mirrors
  refdata-service's `natsrpc/adapter.go` — Micro/Services framework, obs.rpc.* observability)
  exposing ship/container/port commands plus new list queries (`ship.list.v1`,
  `container.list.v1`, `meta.known-containers.v1`) as `rpc.*` subjects. Registered per-tenant
  connection, kept alive independently of the single-active-tenant REST `SwitchTenant` pattern.
  **Known gap (accepted):** unlike refdata-service's adapter (always on the DEFAULT account), this
  adapter's obs.rpc.* events publish onto whichever TENANT account is active — isolated from the
  DEFAULT-only Admin UI RPC panel. Publishing anyway so a future cross-account-imports phase makes
  it visible with no code change; see the adapter's "KNOWN GAP" doc comment.
- **15b — notify.\* publishes**: the four event-handler projectors
  (`dictionary/internal/eventhandler/`) fire-and-forget publish the full projected entity to
  `notify.{context}.shipping.{entity}.changed` after each KV write. Plain core NATS pub/sub, no
  JetStream retention — a missed notification is covered by the bootstrap RPC call on reconnect.
- **15c — auth-service**: new standalone Go service (modeled on accounts-service) that mints
  short-lived (5 min TTL), permission-restricted browser user JWTs (`GET
  /api/auth/connectInfo?tenant=X`) plus a tenant list for the browser (`GET /api/auth/tenants`).
  Reads account signing keys from accounts-postgres (read-only). `POST /api/auth/login` is a 501
  placeholder for the future WorkOS flow (BR-UA01).
- **15d — Sea Freight Flow migration**: `nats.ws` client, new `shared/nats/useNatsConnection.js`
  singleton composable, `api.js`/`stores/port.js`/`stores/tenant.js` rewritten to use
  `rpc.*`/`notify.*` instead of REST/SSE. Refdata's shared composables
  (`useRefdataLabels`/`useL10nCopy`) are unchanged — they stay on REST + SSE.

Full implementation plan: see `.claude/plans/ancient-sauteeing-hartmanis.md` (this session's plan
file) for file-level detail, or its contents once folded into this document on completion.

---

### Phase 16 (16a–16k) — Subject Taxonomy & Tenancy Formalization

#### Goal

Phase 15 introduced `notify.*` and put the browser on `rpc.*` without updating the taxonomy
docs, which exposed a deeper inconsistency: `{context}` was documented as "tenant/region scope"
(refdata-service, `ARCHITECTURE-COMMUNICATIONS.md` § 2, `emea-acme`) but implemented as a
tenant-agnostic fleet qualifier in shipping-service, where Phase 13 had made the **NATS account**
the real tenant boundary. Two services had quietly diverged on what the same token means, and no
commit reconciled them. Phase 16 settles the model, documents it once, and migrates the code to
match.

#### Decision record (2026-07-31)

1. **Tenancy is enforced strictly and only by NATS accounts.** Follows NATS's own guidance:
   an account is an isolated tenant with its own subject space and an absolute boundary; accounts
   additionally carry independent resource limits (connections, JetStream storage, streams,
   consumers) that a naming convention cannot express. Subject-prefix/permission separation inside
   one shared account is the pattern NATS documents as *legacy* and weaker, and is not used for
   tenancy here. Already proven by `natsaccounts/isolation_test.go` and
   `accounts/provisioner_test.go`.
2. **`{context}` has no relation to tenant.** It is the **company / business-unit** scope — a soft
   partition inside one tenant's own subject space, for addressing and routing, never a security
   boundary. Context vocabulary is per-tenant, not shared.
3. **Region is a deployment axis**, handled by separate regional stacks each with their own NATS
   instance. It never appears in a context value or subject token. Full hierarchy:
   `region → tenant (NATS account) → company/group → business unit ({context})`.
4. **Business units are hyphenated into the single `{context}` token** (`acme`, `acme-northdiv`),
   never dot-separated: every subject family has **fixed arity** and parsers read `{context}` by
   position (`domain.SubjectDetails` rejects anything but exactly 6 tokens), so a variable-arity
   context would make token 3 ambiguous between "business unit" and a shifted `{service}`.
   Accepted trade-off: NATS wildcards match whole tokens, so `evt.acme-*.>` is invalid — there is
   no subject-level "all business units of company X"; that grouping is answered by the KV/Postgres
   lookup instead. The value is **opaque** — never split on `-` to recover the parts.
5. **`_`-prefixed context values are reserved for platform use** (`_platform`), enforced by
   `accounts-service` rejecting `_`-prefixed company names. Sigils like `$` are unusable: a context
   is also a KV bucket-name component and must match `^[A-Za-z0-9_-]+$`.
6. **No hard isolation between business units** in one tenant account — the separation is
   organizational, enforced in the application layer. Two business units that must be mutually
   opaque have to be modelled as **separate tenants** (separate accounts); `{context}` cannot
   provide it.
7. **Five subject families, two groups.** *Core:* `evt.*` (event sourcing), `rpc.*`
   (service-to-service), `api.*` (frontend-to-service — **new**), `notify.*` (service-side change
   notification, replaces SSE). *Supportive:* `obs.rpc.*` / `obs.api.*` (debugging side-channel,
   deliberately off the business subjects so a slow/absent watcher can never add latency or
   backpressure to a real call). `cmd.*` stays reserved and unused.
8. **`rpc.*` / `api.*` separation rule.** An operation may be registered on both when a backend
   *and* a browser genuinely need it, but each registration is independent (own adapter, own
   permission grant). **A browser credential is never granted `rpc.>`; backend code never calls
   `api.>`.** This closes a latent gap: browser JWTs currently grant `rpc.>`, so any future
   backend-only `rpc.*` endpoint added inside a tenant account would have been browser-reachable
   by accident.
9. **`auth-service` and `accounts-service` carry no `{context}`** — they administer the tenant
   axis itself, so scoping them by company is incoherent (creating the account is what brings the
   company into existence). Not a blanket platform-service exemption: `refdata-service` is equally
   a platform service but its *data* is company-scoped, so it keeps `{context}`.
10. **refdata inheritance keeps arbitrary depth** as already implemented
    (`context_repository.go`'s recursive ancestor CTE) — not restricted to a fixed number of hops.
    Resolution stays **server-side**: a caller asks for one context and receives a resolved answer,
    mirroring `domain.ResolveLabel`'s existing locale-fallback chain. Clients never walk the chain
    or issue N calls. `rpc._platform.refdata.*` exists for stewards/tooling to read or administer the
    global corpus directly, **not** as a fallback path ordinary callers invoke.
11. **A "company group" may sit between tenant and company.** One tenant account can host more
    than one company (the `company or company group` level of the hierarchy). This is what makes a
    company qualifier inside `{context}` meaningful rather than a redundant echo of the account
    name — for a single-company tenant it is a harmless degenerate case.
12. **Both services use the fully-qualified context form** (`acme-atlantic-fleet`), not each
    service's locally-minimal form. Rationale for the choice: `refdata-service` runs on a single
    shared account and so has **no account boundary** to tell whose corpus a request concerns — the
    company qualifier must be in the value. `shipping-service` does have that boundary and could
    use the bare `atlantic-fleet`, but then the same logical scope would carry two different
    canonical names depending on which service you asked, re-creating the divergence this phase
    exists to eliminate (and forcing a composition rule at every crossing point). One vocabulary
    everywhere was chosen over locally-minimal names: the cost is a redundant prefix inside
    shipping's own account and a value migration (16e); the benefit is that a context value means
    exactly one thing everywhere and crosses service boundaries unchanged.
    - Consequence: shipping's company-wide context (formerly `global`) becomes just `acme`, so its
      tree mirrors refdata's exactly (`_platform → acme → acme-atlantic-fleet`). Deliberately *not*
      `acme-global`, which reads oddly for "all of Acme" — and which would also sit confusingly
      close to the reserved root.
    - The reserved root is `_platform`, **not** `_global`: shipping already has a user-space context
      historically called `global`, and while the `_` prefix makes the two formally unambiguous, the
      visual proximity in logs and subjects was judged not worth it. `_platform` also matches the
      platform-services tier this corpus belongs to.
13. **A context may be linked to a tenant** (new optional `tenant` column on `refdata.contexts`).
    **Governance/ownership metadata and query scoping only — explicitly not a security boundary**:
    refdata-service runs on a single shared NATS account and so has no server-supplied caller
    identity to enforce it against. Making it enforceable is an open item (see
    `Refdata-Versioning-Tenancy-Design.md` § 2.1).

#### Sub-phases

- **16a — documentation formalization (DONE 2026-07-31, docs only, no code).** `ARCHITECTURE-COMMUNICATIONS.md`
  § 1–2 rewritten (five families, Core/Supportive, full `{context}` rules, `rpc.*`/`api.*`
  separation rule); `ARCHITECTURE-DICTIONARY.md` context definition corrected;
  `CLAUDE.md`'s taxonomy block corrected (it described the token as `<tenant>`);
  `BUSINESS_RULES-SHIPPING.md` BR-023 and `BUSINESS_RULES-REFDATA.md` amended;
  `Refdata-Versioning-Tenancy-Design.md` § 2.1 reconciled (root `global` → `_platform`, region
  removed as a node, `tenant` column added); `Multi-Region-Plan.md` § 0 added with the
  region/tenancy answers and the Mirror-vs-gateway recommendation.
- **16b — `api.*` migration (DONE 2026-07-31).** `shipping-service`'s `dictionary/internal/natsrpc/` →
  `dictionary/internal/browserrpc/`; `rpc.*` → `api.*`, `obs.rpc.*` → `obs.api.*`;
  `auth-service`'s `MintBrowserToken` grants `api.>`/`notify.>` and **drops `rpc.>`**;
  frontend subject builders updated; tests renamed/retargeted. Mechanical — no behaviour change.
  A fresh `internal/natsrpc/` gets created only if/when shipping-service genuinely needs a
  backend-to-backend endpoint (none exists today); no empty placeholder package is left behind.
- **16c — reserved-name enforcement (DONE 2026-07-31).** Enforced in **both** services, not just
  accounts-service as first scoped — discovered while implementing that `{context}` is
  refdata-service's own resource (`refdata.contexts`), registrable independently of any NATS
  account via `POST /api/refdata/admin/contexts`, so accounts-service alone couldn't guarantee the
  invariant. `accounts-service` (BR-AC07) rejects `_`-prefixed account names at `POST /api/accounts`
  (`400`, distinct from BR-AC06's `409` for an exact reserved-name match) — needed because in the
  common no-company-group case (decision 11) a tenant's own name doubles as its company context, so
  an unguarded account name could smuggle a `_`-prefixed value into the context namespace.
  `refdata-service` (BR-D33) rejects `_`-prefixed context names in `ValidateContextName` — the
  primary enforcement point, since it's the one that can't be bypassed by skipping account creation
  entirely. Both raise a typed error (`ErrReservedContextPrefix`/inline), both tested, both allow a
  mid-string underscore (`acme_northdiv`). Known pending item folded into 16d: `ValidateContextName`
  as written also rejects the platform root's own future registration — 16d's seeding must
  special-case it.
- **16d — refdata context tree (DONE 2026-07-31).** `_platform` (reserved root) → `acme`
  (company, `tenant: "acme"`) → `acme-atlantic-fleet` (business unit); `emea-acme` retired.
  `refdata.contexts.tenant` added (`ALTER TABLE`, nullable — BR-D34, governance metadata only,
  not enforced, per decision 13). `_platform` is seeded via a new `ContextHandler.RegisterPlatformRoot`,
  the one sanctioned exception to BR-D33's blanket `_`-prefix rejection — the public REST endpoint
  still rejects `_platform` unconditionally; only `seed.go` calls the bypass.
  **Seed data now actually demonstrates inheritance**, not just registers a tree: standards types
  (currency/country/incoterm/uom/hazard-class) moved to `_platform`; domain types (ship-status,
  UI-copy strings) moved to `acme`. `hazard-class` alone carries all three inheritance states —
  codes 1/2/4-9 inherited, code 3 overridden at `acme` (BR-V07), code `X1` an addition only at
  `acme-atlantic-fleet` (BR-V06). Critically, `Seed` also idempotently drafts+publishes an initial
  corpus version per context, **parent-first** — required because `CreateDraft` silently skips
  (not errors) an ancestor that has never published, so without this the tree would exist but
  inherit nothing, exactly as invisible as the one-context state it replaces.
  Two live consumers hardcoded `"emea-acme"` and would have broken on retirement, found and fixed
  in the same pass: `shipping-service`'s `refdataContext` const (→ `"acme"`) and
  `frontend/refdata`'s `CONTEXTS` array (→ all three contexts, so the admin UI can browse the
  whole chain). `internal/domain/validation.go` also gained a shared `ValidateSubjectToken`
  (charset-only) that both `ValidateContextName` and `RegisterPlatformRoot` build on.
- **16e — shipping context value migration (DONE 2026-07-31).** Adopted the fully-qualified form
  (decision 12) throughout shipping-service: `global` → `acme`, `atlantic-fleet` →
  `acme-atlantic-fleet`, `pacific-fleet` → `acme-pacific-fleet` (the third literal wasn't named in
  this entry's original text but was migrated identically for consistency). Renamed every literal
  occurrence — `postgres/migrate.go`'s `seedDefaultPorts`, 8 Go test files' fixture consts,
  Swagger/doc-comment examples in `rest/handlers.go`/`sse.go`/`kv.go`/`browserrpc/adapter.go`,
  `auth-service/auth/token.go`'s doc comment (now `accounts-service/auth/token.go` post-Phase-19), and both frontends' `CONTEXTS` arrays
  (`seafreight-app/stores/port.js`, `admin/stores/dictionary.js`) — then regenerated Swagger docs.
  **Investigated but deliberately NOT built**, because it turned out to have no structural basis:
  the plan's "context seeding becomes tenant-aware" goal. `seedDefaultPorts` runs once at startup
  before any tenant connection exists, and shipping-service's Postgres schema (`ports`/`ships`/
  `containers`) has **no tenant column at all** — every tenant sharing this Postgres instance reads
  the same rows, scoped only by `context`. Tenant isolation for this data lives entirely in which
  NATS account a request authenticates into, not in a Postgres row; making per-tenant context
  seeding real would mean adding a tenant dimension to this schema, which is a materially bigger
  change than a value rename and was out of this phase's scope. Documented in `migrate.go`'s
  `seedDefaultPorts` doc comment so this isn't mistaken for an oversight later.
  **KV bucket rename**: confirmed no rename/migration mechanism exists anywhere in
  `internal/kvstore` — `Bucket()`'s `{prefix}-{context}` naming (`kv.go`) only creates-or-updates
  the new name; a `docker compose down -v` was required (and run) to get a clean environment
  rather than leaving stale old-named buckets silently reading empty.
- **16f — dynamic context list (DONE 2026-07-31).** Backend: `refdata-service` gained
  `ContextRepository.ListByTenant`/`ContextHandler.ListByTenant` (BR-D35, `BUSINESS_RULES-REFDATA.md`)
  — returns a tenant's own contexts plus the shared `_`-reserved platform roots — exposed on both
  transports (`GET /api/refdata/admin/contexts?tenant=`, `rpc._platform.refdata.context.list.v1`).
  `shipping-service` added `refdataconsumer.ListContexts` (calling that new rpc.* endpoint, BR-D28
  NATS-only) and a new `GET /api/refdata/contexts` REST endpoint (`listRefdataContexts`, BR-025)
  that resolves the caller's tenant via `refdataCompanyContext(deps.Tenant)`.
  **Extended scope beyond the plan's original wording**: the plan also flagged
  `refdataconsumer` passing a hardcoded `"acme"` company-context constant as itself "pending Phase
  16f" — fixed by deriving it from the active tenant (decision 11's "tenant name doubles as company
  context in the no-company-group case," the same mapping BR-AC07 relies on) instead of leaving it
  hardcoded, applied consistently across `listRefdataType`/`listRefdataLocales`/`listRefdataContexts`
  and the `watchRefdata` SSE subject filter.
  Frontend: `CONTEXTS` (previously hardcoded, just renamed in 16e) is now the offline/error
  fallback only — both `seafreight-app/stores/port.js` and `admin/stores/dictionary.js` gained an
  `availableContexts` reactive field and a `loadContexts()` action, fetched on tenant init/switch
  (not on a plain fleet-context change, to avoid refetching the same list a context switch is
  picking from) — both apps' `<Select>` dropdowns bind to the live list.
  **Known gap, explicitly documented rather than silently left implicit** (BR-025's "Known gap"
  paragraph): Sea Freight Flow (Phase 15d) no longer drives shipping-service's REST-side
  `Deps.Tenant`/`SwitchTenant` at all — it authenticates directly into its own NATS account. These
  refdata REST reads resolve tenant from `Deps.Tenant`, which the Admin/Dictionary frontend does
  control. Both happen to default to the same tenant (`acme`) today, so this reads correctly in the
  common case, but the two are not actually the same signal — if the Admin UI's tenant selection and
  Sea Freight Flow's own NATS tenant ever diverged, these three endpoints would reflect the Admin
  UI's selection. A real fix needs an explicit tenant threaded through the *shared*
  `useRefdataLabels`/`useL10nCopy` composables rather than server-side state; left as a documented
  seam, not fixed, since it's a pre-existing Phase 15 scope boundary (that phase already flagged
  refdata-service's cross-tenant DEFAULT-account model as out of scope), not something 16f
  introduced or was asked to resolve.
  `refdataconsumer` passes the real context instead of the hardcoded `emea-acme` it uses today
  (`consumer.go:19-20` — "demo-scoped to that context"). First time the frontend does not know its
  contexts synchronously, so it needs a genuine loading/error path.
- **16g — Sea Freight Flow Fleet panel tenant/context-switch flicker fix (DONE 2026-08-02, BR-029).**
  User report: "not sure if there's a momentary flicker on the fleet management panel" when switching
  tenants. Root-caused via a live DOM-mutation-observer capture on the running stack (not
  speculation): `usePortStore().connect()` (called by both `stores/tenant.js`'s `setTenant()` and
  `setContext()`) resets `ships`/`containers` to `{}` synchronously, before its `listShips`/
  `listContainers`/`getPorts`/`knownContainers` bootstrap reads land — so the `FleetPanel.vue`
  `DataTable`, which renders straight off `store.allShips`, briefly rendered its own empty state
  ("No ships match this filter.") mid-switch, misreading as "this tenant has none" rather than
  "still loading." Measured on localhost: ~80ms of stale old-tenant data during the NATS WebSocket
  re-authentication, then a ~10ms empty flash, then the new tenant's data — small locally, but a
  real, reproducible gap that widens under real network latency.
  Fix: `stores/port.js` gained a `loading` state — set `true` at the top of `connect()` (same point
  the reset happens), cleared via `Promise.allSettled(...).finally()` once all four bootstrap reads
  settle (success or failure). `FleetPanel.vue` renders a spinner + `fleet.loading` copy while
  `store.loading` is true, the `DataTable` otherwise (`v-if`/`v-else`, mutually exclusive) — reusing
  the exact spinner CSS convention from the Admin UI's `ServicesPanel.vue` (17c follow-up, same
  session) for visual consistency across both frontends. New `fleet.loading` l10n key added to
  `backend/refdata-service/refdata/seed.go` (en/es/af) and regenerated into
  `shared/refdata/l10nFallback.en.js` via `npm run gen:i18n` — resolves from the bundled fallback
  immediately; no live refdata-service reseed was needed since `useL10nCopy.js`'s catalog always
  starts from the bundled fallback and only overlays live-fetched keys on top.
  Verified live: rebuilt `shipping-frontend` (`docker compose build --no-cache shipping-frontend`,
  needed because the first `--build` reused a stale cached layer), confirmed via a DOM-mutation
  observer that the empty-state flash is gone and replaced by the spinner across a real acme→globex
  switch. Initially scoped to the Fleet Management panel only, per the user's report; the user then
  asked for the same fix on `ShipsAtPortPanel.vue`/`TerminalPanel.vue` — confirmed they read from the
  same `store.ships`/`store.containers` reset and have the identical gap, so the same `store.loading`
  flag was reused (no new store-level work needed) and both panels got the same spinner/DataTable
  `v-if`/`v-else` treatment. `ShipsAtPortPanel.vue` layers the spinner as a third branch alongside its
  pre-existing `!store.port` ("select a port") message; `TerminalPanel.vue` covers both its Outbound
  and Arrived tables with one shared spinner rather than two, since both come from the same reset.
  Re-verified live the same way (DOM-mutation observer, real acme→globex switch, `spinnerCount: 2`
  during the loading window, no "No ships docked here"/"No outbound containers" flash).
  - [x] `stores/port.js` — `loading` state + `Promise.allSettled` around the bootstrap reads
  - [x] `components/FleetPanel.vue` — spinner/DataTable `v-if`/`v-else`, spinner CSS
  - [x] `components/ShipsAtPortPanel.vue` — spinner as a third `v-if`/`v-else-if`/`v-else` branch, spinner CSS
  - [x] `components/TerminalPanel.vue` — one shared spinner covering both Outbound/Arrived tables, spinner CSS
  - [x] `backend/refdata-service/refdata/seed.go` + regenerated `l10nFallback.en.js` — `fleet.loading`,
        `shipsAtPort.loading`, `terminal.loading` keys
  - [x] `BUSINESS_RULES-SHIPPING.md` — BR-029 (updated to cover all three panels)
  - [x] Tests: `App.spec.js` (render-level, one test per panel: spinner shown/hidden, empty message
        never shown mid-switch), `stores/port.spec.js` (`connect()`'s `loading` lifecycle, success and
        failure paths) — 20/20 passing
  - [x] Live-verified via DOM-mutation-observer capture on the rebuilt `shipping-frontend` container,
        both for the initial Fleet-only fix and the Ships at Port/Terminal Yard extension
- **16h — reactive tenant provisioning (DONE 2026-08-02, BR-030/BR-AC08).**
  User report: "when I create a new tenant, the port selection dropdown is empty in the register ship
  dialog." Investigating live turned up something much bigger than an empty dropdown: shipping-service
  logs showed zero mentions of the new tenant at all, and `EnsureAllTenants`'s own doc comment already
  said why — "a tenant minted later by accounts-service is instead picked up the first time any
  SwitchTenant call names it... there is no background poll for newly minted tenants nobody has
  referenced yet." Sea Freight Flow never calls `SwitchTenant` (Phase 15d — it authenticates straight
  into NATS), so a brand-new tenant's `browserrpc.Adapter` genuinely did not exist on this process:
  every `api.*` request against it — ships, containers, ports alike — timed out after 5s and was
  swallowed silently by the browser's `.catch(() => {})`, reading as "this tenant has nothing" rather
  than "not provisioned yet." Confirmed live: `Initech.creds` was written well after shipping-service's
  last restart, and manually POSTing `/api/tenant/switch {"tenant":"Initech"}` (the Admin UI's own
  lazy-provisioning path) immediately fixed it — proving the mechanism worked, it just had no trigger
  for a tenant nobody had switched to yet.
  Presented two fix options (reactive event vs. a lazy browser-side "ensure" call before connecting);
  user chose the reactive event.
  Fix: `accounts-service` gained a second, DEFAULT-account NATS connection
  (`NATS_DEFAULT_CREDS_PATH`, optional/nil-safe) used only to publish `notify.accounts.account.created`
  (context-free subject — accounts-service has no `{context}` of its own) after a create fully commits
  (resolver JWT + creds file + Postgres row) — `accounts/handler.go`'s new `publishAccountCreated`,
  called from `createAccount` (BR-AC08). `shipping-service` subscribes to it on `mono.NC()` (its own
  DEFAULT-account connection) in `composition.go`'s `Module.Startup`, right after `EnsureAllTenants`,
  and calls a new `Handlers.EnsureTenantByName` (BR-030) — the same idempotent `ensureTenantResources`
  path `EnsureAllTenants` already uses, just triggered per-tenant instead of only at startup. No new
  docker-compose volume needed — accounts-service already bind-mounts the whole shared `./nats/creds`
  directory read-write, `default.creds` included; just a new env var.
  Verified live end to end on the running stack (not just via tests): created a fresh tenant via
  `POST /api/accounts`, confirmed both accounts-service's `accounts-service-default` connection and
  shipping-service's reaction in `/connz`, then used the `nats` CLI with that tenant's own freshly-minted
  creds to `nats req api.acme.shipping.ship.list.v1 '{}'` — got a reply in ~20ms, `SwitchTenant` never
  called for it — and confirmed the Register Ship dialog's port dropdown populated immediately in the
  browser.
  - [x] `backend/accounts-service/accounts/handler.go` — `Handlers.NotifyNC` field, `publishAccountCreated`,
        called from `createAccount` after `Store.Insert` succeeds
  - [x] `backend/accounts-service/cmd/main.go` — second DEFAULT-account connection, `NATS_DEFAULT_CREDS_PATH`
  - [x] `backend/shipping-service/dictionary/internal/rest/tenant.go` — `Handlers.EnsureTenantByName`
  - [x] `backend/shipping-service/dictionary/composition.go` — `notify.accounts.account.created` subscription
  - [x] `docker-compose.yml` — `NATS_DEFAULT_CREDS_PATH` env var for accounts-service
  - [x] `BUSINESS_RULES-ACCOUNTS.md` — BR-AC08; `BUSINESS_RULES-SHIPPING.md` — BR-030
  - [x] Tests: accounts-service `handler_test.go` (event published only after a successful create, with
        the right payload; create still succeeds with `NotifyNC` nil) — 29/29 passing. shipping-service
        `dictionary/tenant_switch_test.go` (`EnsureTenantByName` makes globex's adapter answer without
        `SwitchTenant` ever being called; no-op for an unknown tenant name) — full suite (5 packages)
        green.
  - [x] Live-verified end to end: `/connz`, `nats req` with a freshly-minted tenant's own creds, and the
        Register Ship dialog in the browser — no restart, no `/api/tenant/switch` call involved.
- **16i — reactive tenant teardown (DONE 2026-08-03, BR-031/BR-AC09).** The mirror of 16h, found while
  investigating a follow-up question: "what's supposed to happen when an RPC is sent from a suspended
  account?" Answering it live turned up a second architectural gap symmetric to 16h's. NATS force-evicts
  every connection on an account the instant `$SYS.REQ.CLAIMS.DELETE` revokes it (confirmed on the running
  stack — a live `nats sub` dropped within ~3s of a suspend call) — this directly contradicted
  `Provisioner.DeleteAccount`'s own doc comment and `BUSINESS_RULES-ACCOUNTS.md`'s BR-AC03, both of which
  claimed existing connections continue; both were corrected as part of this phase. The browser side was
  already correct by accident (`connectInfo`'s 403 refuses re-authentication), but its refusal only set
  `useNatsConnection.js`'s `lastError`, which nothing rendered — a suspended session's panels just went
  quiet. The real bug was on shipping-service's side: its per-tenant connection, evicted like any other,
  had no equivalent gate, so `nats.go`'s default reconnect logic retried forever against a `.creds` file
  `suspendAccount` had already deleted — one permanent, log-spamming loop per suspension, previously
  cleared only by a restart (`UmbrellaTest2`/`suspendtest` had been looping for an entire prior session
  before this phase's `docker compose restart shipping-service`).
  Sequence diagrams (current + proposed) were sketched first and added to `ARCHITECTURE-ACCOUNTS.md` § 2t-a
  before any code changed, per this repo's usual "diagram/rules first, implementation second" flow; user
  confirmed the reactive-event approach (same shape as 16h) before implementation began.
  Fix: `accounts-service` publishes `notify.accounts.account.suspended` (BR-AC09) — same subject family,
  same `Handlers.NotifyNC` connection as 16h's created event — right after `Store.SetStatus` marks the
  account suspended. `shipping-service` subscribes to it in `composition.go` (right after the BR-030
  subscription) and calls a new `Handlers.TeardownTenantByName` (BR-031): stops that tenant's projectors
  and `browserrpc.Adapter`, then explicitly closes shipping-service's own connection to that account —
  the explicit `Close()` is what actually disables `nats.go`'s auto-reconnect; eviction alone doesn't.
  `App.vue` also now renders `useNatsConnection.js`'s `lastError` as a danger `Tag` in the topbar whenever
  non-empty, clearing itself once a connection succeeds again (no new state needed — `connect()` already
  resets `lastError` to `''` on success).
  Deliberately left out of scope: a terminal-vs-transient error classification as a backstop for a missed
  or out-of-band suspension (e.g. an operator revoking an account directly via `nsc`). The event covers the
  normal path; the backstop is a separate, larger design decision, sketched in `ARCHITECTURE-ACCOUNTS.md`
  § 2t-a's "Proposed" section but not implemented.
  - [x] `backend/accounts-service/accounts/handler.go` — `publishAccountSuspended`, called from
        `suspendAccount` after `Store.SetStatus` succeeds
  - [x] `backend/accounts-service/accounts/provisioner.go` — corrected `DeleteAccount`'s doc comment
  - [x] `backend/shipping-service/dictionary/internal/rest/tenant.go` — `Handlers.TeardownTenantByName`
  - [x] `backend/shipping-service/dictionary/composition.go` — `notify.accounts.account.suspended` subscription
  - [x] `frontend/seafreight-app/src/App.vue` — `lastError` danger `Tag`, `data-testid="connection-error"`
  - [x] `backend/refdata-service/refdata/seed.go` + regenerated `l10nFallback.en.js` — `connection.error` key
  - [x] `BUSINESS_RULES-ACCOUNTS.md` — BR-AC03 corrected, BR-AC09 added; `BUSINESS_RULES-SHIPPING.md` — BR-031
  - [x] `ARCHITECTURE-ACCOUNTS.md` — § 2t corrected, § 2t-a added (current + proposed sequence diagrams,
        rendered and verified with `mmdc`)
  - [x] Tests: accounts-service `handler_test.go` (BR-AC09's mirrored pair — suspend of an unknown account
        publishes nothing; a successful suspend publishes the tenant's name; suspend with a nil `NotifyNC`
        still succeeds) — 31/31 passing. shipping-service `dictionary/tenant_switch_test.go`
        (`TeardownTenantByName` makes globex's adapter go silent by actually closing shipping-service's own
        connection, not just local bookkeeping; no-op for a never-provisioned tenant) — full suite (5
        packages, 107 specs) green. `frontend/seafreight-app` `App.spec.js` (`lastError` Tag appears/clears,
        distinguished from the pre-existing connection-status Tag which is also danger-severity while
        disconnected) — 11/11 passing in that file.
  - [x] Live-verified the underlying eviction/loop behavior on the running stack before implementing
        anything: a throwaway tenant's live `nats sub` connection dropped within ~3s of suspension, and
        shipping-service logged the ENOENT reconnect loop against its deleted `.creds` file.
- **16j — reactive tenant restore (DONE 2026-08-03, BR-032/BR-AC10).** Closes the lifecycle triple, and
  fixes a regression 16i itself introduced — found immediately afterward during a requested architecture
  review of the accounts area rather than by a user report. 16i's teardown is correct but was a **one-way
  door**: `EnsureAllTenants` runs only at startup and Sea Freight Flow never calls `SwitchTenant`
  (Phase 15d), so nothing rebuilt a tenant that came back, leaving a suspend→reactivate cycle dark until
  a restart. Ironically 16i made this worse than before — the pre-16i reconnect loop was ugly but would
  have self-healed once creds returned; a clean teardown made the missing counterpart load-bearing.
  Fix: `accounts-service`'s `reactivateAccount` publishes `notify.accounts.account.reactivated` after the
  whole reactivation commits — deliberately *after* the fresh `.creds` write, since the consumer resolves
  tenants by scanning that directory (asserted in the spec, not just commented). `shipping-service`
  subscribes and calls the existing `EnsureTenantByName` **unchanged** — 16i's teardown removed the tenant
  from `TenantResources`, so the ordinary first-sight path rebuilds it against the new credentials. No new
  provisioning code, just a third trigger for the same idempotent path. The three publishers were also
  de-triplicated behind one nil-safe `publishAccountEvent` helper, keeping each event's own doc comment.
  - [x] `backend/accounts-service/accounts/handler.go` — `publishAccountReactivated` + shared
        `publishAccountEvent` helper; called from `reactivateAccount` after `SetStatus(active)`
  - [x] `backend/shipping-service/dictionary/composition.go` — `notify.accounts.account.reactivated`
        subscription
  - [x] `BUSINESS_RULES-ACCOUNTS.md` — BR-AC10; `BUSINESS_RULES-SHIPPING.md` — BR-032;
        `ARCHITECTURE-ACCOUNTS.md` § 2t-a — reactivation asymmetry marked resolved, lifecycle table added
  - [x] Tests: accounts-service `handler_test.go` (a failed reactivation publishes nothing; a successful
        one publishes the name **and** has already written the creds file by the time the event is
        observable; nil `NotifyNC` still succeeds) — 33/33. shipping-service `tenant_switch_test.go` —
        full round trip (answering → dark → answering again, `SwitchTenant` never called), then a ship
        arrival driven through `api.*` and awaited in the read model to prove the *projectors* came back
        too, not just the adapter — 108 specs green.
  - [x] Fixed a pre-existing ~1-in-4 flake in BR-030's spec while here: `micro.AddService` doesn't flush
        its subscriptions before returning, so a request issued in the same instant can get
        `no responders`. BR-030/031/032 specs now poll with `Eventually`; verified 10/10 clean full-suite
        runs (previously reproducible within 4). Documented as a note under BR-032 so it isn't
        re-introduced.
- **16k — connection honesty in Sea Freight Flow (DONE 2026-08-03, BR-033).** Follow-on from 16i, prompted
  by a user question rather than a report: is `Depart failed / not connected` expected on a suspended
  account? Functionally yes (fail-closed is correct), but tracing it found the app communicating badly in
  two ways, one of which was a genuine bug.
  (a) **The status badge lied.** Two independent `connected` flags exist — `useNatsConnection`'s (clears
  correctly on eviction) and `usePortStore().connected` (cleared only by the store's own `disconnect()`,
  which nothing calls on eviction). The topbar read the latter alone, so after a suspension it showed a
  green "watching" badge directly beside 16i's red "connection error" badge — visible in this session's own
  verification screenshot and initially missed. Fixed with a `watching = natsConnected && store.connected`
  computed.
  (b) **Command failures named the symptom, not the cause.** `request()` threw a bare `not connected`
  whenever `nc` was null; the real reason (auth-service's `tenant is not active`) was already in
  `lastError` but only reachable via a tooltip. Fixed centrally in `notConnectedError()` so every command
  — arrive, depart, register, load, unload — inherits it, rather than patching each panel's catch block.
  `lastError` now stores `err.message`, not `String(err)`, dropping the `Error: ` prefix from
  operator-facing text.
  Deliberately deferred: disabling action controls while disconnected (a broader interaction change across
  every panel; these two were the correctness bugs).
  - [x] `frontend/seafreight-app/src/App.vue` — `watching` computed, `data-testid="connection-status"`
  - [x] `frontend/seafreight-app/src/nats/useNatsConnection.js` — `notConnectedError()`, `errorMessage()`
  - [x] `BUSINESS_RULES-SHIPPING.md` — BR-033
  - [x] Tests: `App.spec.js` (drives the exact contradictory state — NATS down, port store still
        "connected" — and asserts the badge reads `disconnected`); new `src/nats/useNatsConnection.spec.js`
        (3 specs: bare fallback, auth-service's refusal surfacing in its place with the transport symptom
        gone from the message, and the same for `subscribe()`) — 18 passing across the three touched files,
        up from 14

---

### Phase 17 (17a/17b/17c DONE 2026-08-01) — Request/Reply Panel v2, Connections + Services Panels

#### Goal

The Admin UI's Request/Reply panel (renamed from "RPC Traffic" 2026-08-01, when it also
gained the live `obs.api.>` half) shows one row per correlated call but stops there: no
headers, no filtering, and payload inspection is a cramped in-cell expand. Rebuild it as a
proper traffic inspector — DevTools/Fiddler/Datadog-class — with a detail view whose
layout mirrors what the obs channel structurally *is*: two correlated messages.

#### Design decisions (2026-08-01)

Three layouts were evaluated (right drawer / bottom paired panes / facet-rail + flyout).
**Chosen: bottom detail split with side-by-side Request | Reply panes, plus
token-facet filtering** — the paired panes are the one layout that reflects the
two-message structure of the channel, the full-width table handles NATS-length subjects,
and click-a-token filtering exploits the fixed-arity subject taxonomy (Phase 16a) instead
of bolting on a Datadog-style facet rail that would fight the AppShell sidebar.
**Approved static reference: `demos/01-dictionary/frontend/admin/request-reply-reference.html`**
(same convention as `shared/unifi-theme/app-shell-reference.html`) — built on the UniFi
theme tokens; the implementation should match it, not re-derive it.

Key elements, per the reference:

- **Filter bar**: free-text subject match; toggle chips for family (`rpc`/`api`) and
  status (`ok`/`error`/`pending`); **clicking any subject token** (in a row or the detail
  head) adds a positional facet chip (`family:`/`context:`/`service:`/`entity:`/`action:`)
  — the subject *is* the filter, per the fixed-arity taxonomy; a **pause** control that
  freezes the visible list without dropping the SSE (filtering a stream that's still
  prepending rows is otherwise unusable).
- **Table**: adds family badge, ms-precision time, **latency**, and request⁄reply
  **sizes** to today's status/subject/time columns; selected row carries the accent
  inset bar; in-cell payload expand is removed (payloads move to the detail).
- **Detail split (bottom, ~46%)**: header strip (subject, status, latency, correlationId,
  tenant, live/replay source) over two panes — **→ Request** and **← Reply** — each with
  a Headers key/value table and a syntax-tinted JSON body, copy affordances, and an
  error banner on the reply pane for failed calls (surfacing NATS micro's real
  `Nats-Service-Error`/`Nats-Service-Error-Code` headers).

**Data prerequisite (the real work): the obs envelope is too thin.** Today's
`obsEnvelope` (both `browserrpc/adapter.go` and refdata's `natsrpc/adapter.go`) carries
only `direction`/`correlationId`/`subject`/`payload`/`error` — no headers, no
server-side timestamp (the UI clocks arrival time, which lies for replayed events), no
sizes. Latency and both Headers sections have nothing to render until the envelope
gains `headers map[string][]string`, `timestamp` (publisher-side), and `payloadBytes`;
sizes and latency then derive client-side. Envelope changes are additive (new optional
fields), so old retained RPCTRACE events still parse — no migration.

#### Sub-phases

- **17a — obs envelope extension (backend, DONE 2026-08-01).** `obsEnvelope` in both
  adapters gained `headers` (request: `req.Headers()`; reply: `nil` on success, the real
  outgoing error headers on failure — see below), publisher-side `timestamp`
  (`time.Now().UTC()` at publish), and `payloadBytes` (`len(payload)`). New rule
  **BR-D36** (`BUSINESS_RULES-REFDATA.md`) + mirror **BR-026**
  (`BUSINESS_RULES-SHIPPING.md`) — confirmed with the user before implementation per the
  workflow gate (BR-D31 was already taken by the enum-namespace rule from Phase 12.14;
  landed as BR-D36/BR-026 instead). **Extended beyond the original wording**:
  `respondError` in both adapters now also attaches the real
  `Nats-Service-Error`/`Nats-Service-Error-Code` headers to the actual wire reply (via
  `micro.WithHeaders`, additive to the existing JSON error body) — not just the
  observability copy, so the panel shows headers that genuinely traveled on the wire.
  Tests: `natsrpc_test.go`'s new `BR-D36` context and `browserrpc_test.go`'s new `BR-026`
  context (request headers/timestamp/size; reply error-headers on both the obs event and
  the real wire reply; old-shape envelope with none of the three fields still decodes).
  Verified live end-to-end against the running stack via the `nats` CLI.
- **17b — panel rebuild (frontend, DONE 2026-08-01).** `RpcPanel.vue` rebuilt to match
  the reference: filter bar (text + family/status chips + token-facet chips + pause),
  upgraded table (family badge, ms-precision time, latency, sizes), bottom detail split
  with paired Request/Reply panes (headers table, syntax-tinted JSON body, copy, error
  banner). `SubjectPath.vue` gained an opt-in `clickable` prop + `token-click` emit
  (`.stop`-guarded so a token click doesn't also select the row), additive so
  `StreamView.vue`'s existing non-clickable usage is unaffected. Facets are positional
  per the fixed 6-token arity (index 0 = family toggles the existing rpc/api chip instead
  of a redundant facet; indices 1–5 = context/service/entity/action/version). Pause
  freezes the *visible* list only — `rowsById`/`order` keep updating live underneath, so
  an already-open detail pane still resolves. **Known gap, not fixed in this phase: no
  Vitest specs** — the admin app has no test runner configured at all (`package.json` has
  no `test` script, no vitest devDependency), unlike `seafreight-app`/`refdata` which do;
  none of admin's other 10+ panels have component tests either, so this follows existing
  (imperfect) project convention rather than introducing new test infrastructure
  unrequested. Verified instead via live browser testing against the real running stack:
  filtering (text, family/status toggles, token-click facets with toggle-off), pause/
  resume, row selection, paired detail panes, real error headers, and — unprompted but
  informative — the old-envelope backlog rows from before the 17a redeploy rendering
  correctly with "—" placeholders, proving the additive-migration requirement live rather
  than just in a test.

#### Checklist

- [x] Confirm BR wording for the envelope extension (landed as BR-D36 + BR-026)
- [x] 17a: `obsEnvelope` + both adapters emit headers/timestamp/payloadBytes; adapter tests
- [x] 17a: `rpc_watch_test.go` still green; old envelopes still parse end-to-end
- [x] 17b: `RpcPanel.vue` rebuilt to match `request-reply-reference.html`
- [x] 17b: `SubjectPath.vue` token-click filtering; facet chips; pause control
- [ ] 17b: Vitest specs — **not done**, admin app has no test infra at all (see 17b note above)
- [x] `BUSINESS_RULES-REFDATA.md` (BR-D36) + `BUSINESS_RULES-SHIPPING.md` (BR-026) updated
- [x] `ARCHITECTURE-COMMUNICATIONS.md` §6 updated (envelope fields, panel capabilities)
- [x] `go build`/`ginkgo` (shipping), `go test` (refdata), admin `npm run build` all green
- [x] Live browser verification: rpc.* + api.* rows, headers visible, token-filter, pause

- **17c — Connections + Services panels (DONE 2026-08-01).** Two new sidebar
  entries under NATS, after Request/Reply: **Connections** ("what's attached
  to the server right now" — every raw connection) and **Services**
  ("what does each service deliberately offer" — micro-registered endpoints
  + live counters). Researched Synadia Insights first: it groups
  Connections and Services as sibling nav entries under a CLIENTS heading
  (confirmed via a screenshot the user shared), which validated putting
  both under this app's NATS eyebrow — but Insights' own Services view
  isn't publicly documented and, per a second research pass, may not
  actually expose `$SRV`-level endpoint stats the way this panel does, so
  the internals here aren't copied from anywhere, just the *grouping*
  decision.

  **Connections** (`GET /api/nats/connections`) proxies the NATS server's
  own HTTP monitoring endpoint (`/connz?subs=true&auth=true`) — a new
  `NatsMonitorURL` plumbed through `monolith.Monolith` → `cmd/main.go`
  (env `NATS_MONITOR_URL`, defaults to `http://localhost:8222`) →
  `rest.Deps` → `docker-compose.yml` (`http://nats:8222`, the container-network
  hostname). Server-wide, not tenant-scoped — a `/connz` snapshot spans
  every account, unlike every other panel in this app. A plain REST poll
  (10s), not SSE: `/connz` is a single request/reply snapshot with no
  native push model, unlike the KV/JetStream watches.

  **Services** (`GET /api/nats/services`) broadcasts a `$SRV.STATS`
  discovery request — the bare, name-less control subject
  (`micro.ControlSubject(micro.StatsVerb, "", "")`), which every
  registered instance replies to because `nats.go/micro`'s discovery
  subscriptions are deliberately unqueued (unlike its queued business
  endpoints) — and collects replies for a 500ms window, the same protocol
  the `nats micro stats` CLI uses. Queried on both `deps.NC` (DEFAULT —
  where refdata-service's `natsrpc` adapter registers) and
  `deps.TenantNC` (the active tenant — where shipping-service's
  `browserrpc` adapter registers); results are deduped by
  `(service name, instance ID)` since both connections can observe the
  same instance depending on account topology.

  **accounts-service now registers too** (`micro.AddService`, registration
  only — no endpoints, since its provisioning API is REST-only, not
  `rpc.*`/`api.*`) — but on the **SYS account** it already holds for
  `$SYS.REQ.CLAIMS.*` operations. `$SRV` subjects don't cross NATS account
  boundaries, so this admin backend's NC/TenantNC query connections can't
  see it: a **known, accepted gap**, not a bug — confirmed via
  `AskUserQuestion` rather than adding a third, SYS-scoped query connection
  to the admin REST layer (a real privilege-scope tradeoff, not just more
  code). **auth-service was deferred entirely**: unlike accounts-service it
  has no NATS connection at all today (pure JWT/NKey signing via
  `nats-io/jwt` + `nats-io/nkeys`, no `nats.go` client), so registering it
  would mean adding a live connection first, not just one `micro.AddService`
  call — confirmed via the same `AskUserQuestion` as a follow-up, not
  in-scope here.

  No new `BUSINESS_RULES-*.md` entry at the time, consistent with 17b's
  precedent: an envelope/wire-protocol change gets a BR (17a's
  BR-D36/BR-026); admin/observability panel UI does not, since it encodes
  no new domain constraint. **Revisited below** — once the tenant-labeling
  follow-up added real, non-trivial behavior (account resolution, a
  fallback rule, cross-panel consistency), the user asked for it to be
  formalized after all, landing as BR-028 — explicitly scoped to *Admin UI
  presentation*, not a wire-protocol rule, so this paragraph's reasoning
  wasn't wrong, just superseded once the feature grew past "pure plumbing."

#### 17c Checklist

- [x] `NatsMonitorURL` plumbed: `monolith.Monolith` interface, `cmd/main.go`
      (env `NATS_MONITOR_URL`), `composition.go` → `rest.Deps`,
      `docker-compose.yml` (`http://nats:8222`)
- [x] `GET /api/nats/connections` — connz proxy, reshaped to camelCase,
      sorted by `cid`; 502 on unreachable/malformed monitoring endpoint
- [x] `GET /api/nats/services` — `$SRV.STATS` fan-in over NC + TenantNC,
      deduped by `(name, instance ID)`
- [x] accounts-service: `micro.AddService` registration (no endpoints) on
      its existing SYS-account connection
- [x] auth-service: `micro.AddService` — **superseded by Phase 19**, not
      built as originally scoped here. auth-service's routes now run inside
      accounts-service's own process (which already registers on its
      SYS-account connection), so the "needs a NATS connection added first"
      blocker no longer applies — there's no separate binary left to
      register.
- [x] `nats_ops_test.go`: connz reshape/sort + 502 error paths (mocked);
      `$SRV.STATS` fan-in against a real embedded NATS server + real
      `micro.AddService` instance (request/error counters, cross-connection
      dedup, nil-connection safety)
- [x] `ConnectionsPanel.vue` + `ServicesPanel.vue` +
      `IconConnections.vue`/`IconServices.vue`
- [x] Wired into `App.vue`'s NATS sidebar group, after Request/Reply
- [x] `go build`/`ginkgo` (shipping-service), `go build`/`go test`
      (accounts-service), admin `npm run build` all green
- [x] Live browser verification: both panels rendering real data against
      the running stack (containers rebuilt with `NATS_MONITOR_URL` +
      accounts-service registration) — Connections shows all 7 live
      connections incl. accounts-service (visible in `/connz` even though
      its micro registration isn't $SRV-discoverable, confirming the two
      data sources are genuinely independent); Services shows
      refdata-service (5 endpoints, real request/latency counts) and
      shipping-service (12 endpoints) with expand/collapse working

#### 17c follow-up — tenant labeling (DONE 2026-08-02)

The user noticed the Connections panel showing 3 bare `shipping-service`
rows with no way to tell which is the DEFAULT-only connection
(`refdataconsumer`'s `rpc.*` client) versus which tenant's `browserrpc`
adapter each of the other two is — every `nats.Connect` in this codebase
correctly sets `nats.Name("shipping-service")` per the CLAUDE.md rule, but
that rule only guarantees a connection is attributable to a *service*, not
to a *tenant* within it. Three options were weighed:

- **(A) suffix `nats.Name()` per tenant** (e.g.
  `"shipping-service/tenant:acme"`) — rejected: `browserrpc/adapter.go`
  deliberately pins `micro.Config.Name` to the literal `"shipping-service"`
  specifically to match `nats.Name("shipping-service")` (Phase 18's
  `Nats-Responder` invariant — the two identities must never diverge, or
  the Request/Reply panel reads as if one service is two). Suffixing the
  connection name alone would silently break that invariant again.
- **(B) resolve a friendly label server-side, without touching
  `nats.Name()`** — **done**, then generalized past shipping-service's own
  rows on a follow-up prompt ("what about the rest of the services"). First
  pass matched by local socket address alone, so only shipping-service's own
  3 connections got labeled — refdata-service, the `nats` CLI, and any
  browser tab (all sharing DEFAULT or a tenant account with a connection
  shipping-service itself holds) still showed a raw NKey. `nats_ops.go`'s
  `tenantLabelsByAccount` is now two stages: (1) match `/connz` rows to
  shipping-service's own connections (`Deps.NC` → `"DEFAULT"`, each
  `TenantResources[name].nc` → that tenant's name) by **local socket
  address** (`nc.LocalAddr()` is exactly what the server reports back as
  that connection's `ip:port` — same TCP socket, both ends), establishing
  "this account NKey means DEFAULT / means acme"; (2) apply that mapping by
  **account**, not address, to every row in the full `/connz` list — so
  refdata-service and the CLI (DEFAULT) and any tenant-authenticated browser
  tab resolve too, not just shipping-service's own rows. Sidesteps JWT/NKey
  decoding entirely either way. Surfaced as `tenantLabel` on
  `GET /api/nats/connections`; the frontend's Account column prefers it over
  the raw account NKey. accounts-service is the one row that stays
  unresolved — it authenticates on the SYS account,
  which shipping-service holds no connection on, so there's no known
  mapping to apply (same account-boundary shape as the Services panel's
  `$SRV` gap, just showing up as "stays raw" instead of "doesn't appear").
- **(C) `micro.Config.Metadata` tenant tag, Services panel** — **done**.
  `browserrpc.Deps` gained a `Tenant string` field (threaded from
  `tenant.go`'s `ensureTenantResources`, which already has the tenant name
  in scope), attached as `Metadata: {"tenant": <name>}` on the
  `micro.AddService` call — deliberately metadata, not `Config.Name`, to
  leave the Phase 18 invariant alone. `micro.Stats.ServiceIdentity` already
  carries `Metadata`, so `listNatsServices` needed only to pass it through
  (`natsServiceInstance.Metadata`); `ServicesPanel.vue` shows it as a small
  tag next to the instance ID.

Tests: `TestListNatsConnectionsLabelsAnyConnectionSharingAKnownAccount`
(opens two *real* connections against the embedded test server so
`LocalAddr()` is a real ephemeral port, not a fabricated value; the mocked
`/connz` reports those addresses back to seed the account map, then a
fourth row simulating refdata-service — DEFAULT account, but an address
this process never held — proves the account fan-out labels it too; a fifth
row on a genuinely unrelated account proves accounts-service-shaped
connections stay unlabeled rather than mismatched),
`TestTenantLabelsByAccountSkipsNilTenantEntriesAndUnownedAccounts`,
`TestListNatsServicesPassesThroughInstanceMetadata`. `go build`/`ginkgo`
all green (one pre-existing parallel-run flake unrelated to this change,
per its earlier note in Phase 17b — reran green); admin `npm run build`
green. Verified live: refdata-service, the `nats` CLI, and the browser's
websocket connection all now show `DEFAULT`/`acme` instead of a raw NKey;
accounts-service correctly still shows raw (SYS account, out of reach).

#### 17c coverage audit (DONE 2026-08-02) — formalized as BR-028, closed three real gaps

The user asked for a deliberate coverage audit against this phase's
functional/business requirements — not a general "add more tests" request,
but specifically: is the account→friendly-name behavior actually a tested
requirement, or just incidentally covered? The audit surfaced three real
gaps, all closed:

1. **The rule itself had no formal BR**, so there was no single place
   asserting "this must hold" independent of any one test file. **Landed as
   BR-028** in `BUSINESS_RULES-SHIPPING.md`, explicitly scoped to *Admin UI
   presentation* — the user confirmed this scope directly ("only when
   presented in the UI"), so it does not claim anything about the wire
   protocol BR-027 already governs.
2. **The production wiring seam was untested.** `nats_ops_test.go`'s
   existing tests proved the REST handler's reshaping/pass-through logic
   was correct given *any* input, but nothing proved `tenant.go`'s
   `ensureTenantResources` actually threads the real tenant name into
   `browserrpc.Deps.Tenant` in production — a dropped field there would
   have silently broken the Services panel's tenant tag with no test
   catching it. Closed by a new `BR-028` Ginkgo context in
   `dictionary/browserrpc_test.go` that sends a real `$SRV.PING` over the
   wire and asserts `Metadata["tenant"]` equals the fleet context —
   verifying the actual wiring, not a synthetic `micro.AddService` call.
3. **The admin app had zero frontend test infrastructure**, so neither
   panel's rendering logic (prefer the resolved label, fall back to the raw
   NKey, render each with different markup) had any coverage at all —
   pure backend-API coverage doesn't prove the UI actually uses what the
   API returns. Vitest + `@vue/test-utils` + `happy-dom` added to
   `frontend/admin/`, mirroring `seafreight-app`'s/`refdata`'s existing
   config exactly (same versions, same `test`/`test:watch` scripts, same
   `environment: 'happy-dom'`) rather than inventing a new convention —
   this was previously an explicit known gap (17b's checklist note: "admin
   app has no test infra at all"), now closed. New
   `ConnectionsPanel.spec.js` (8 tests) and `ServicesPanel.spec.js` (6
   tests) cover BR-028's rendering plus filtering, row selection/detail
   pane, expand/collapse, and error states — all passed on the first real
   run against real PrimeVue `DataTable` in `happy-dom`, no mocking of the
   components under test themselves.

A fourth item was investigated and intentionally left as-is:
**accounts-service's `micro.AddService` call had no regression test**,
consistent with this repo's existing convention that `cmd/main.go`
bootstrap wiring isn't unit-tested anywhere (shipping-service's own
`main.go` has the same gap) — but "consistent with convention" isn't the
same as "actually verified," so this was checked live first
(`nats micro info accounts-service --creds sys.creds`, confirming the
registration genuinely responds on the SYS account, not just that
`main()` didn't error at startup) before deciding what to do. Since the
user asked for a test here too, the registration was **extracted** out of
`main.go` into a new, directly testable `accounts.RegisterMicroService(nc
*nats.Conn) (micro.Service, error)` (`accounts-service/accounts/service.go`)
— mirroring this repo's own architecture convention (bootstrap wiring in
`main.go` stays thin; testable logic lives in a package a test can import).
New `accounts/service_test.go` reuses the existing operator-mode embedded
server helper (`newOperatorTestServer`/`.ConnectSys`, already established
by `provisioner_test.go`) so the test authenticates as a real SYS account,
not a plain unauthenticated one — a permissions regression on that account
would be caught here, which the previous "no error at startup" check could
not have caught. Two specs: responds to `$SRV.PING` with the right
name/version; registers zero endpoints (registration-only, by design).

- [x] BR-028 written up in `BUSINESS_RULES-SHIPPING.md`, index updated in
      `BUSINESS_RULES.md`
- [x] `dictionary/browserrpc_test.go`'s Context renamed to `BR-028: ...`
      (matching the BR-025/026/027 naming convention — Ginkgo Context
      strings are how this codebase makes a rule searchable)
- [x] Vitest infra added to `frontend/admin/` (`package.json`,
      `vite.config.js`) — mirrors `seafreight-app`/`refdata` exactly
- [x] `ConnectionsPanel.spec.js` (8 tests), `ServicesPanel.spec.js`
      (6 tests) — all green; `npm run test` and `npm run build` both green
- [x] Found and fixed a real bug while writing the frontend tests:
      `ConnectionsPanel.vue` defined an `accountLabel()` helper that was
      never actually called — the template duplicated its logic inline
      instead. Removed the dead function; fixed the stale comment above it
      that still described the pre-generalization `tenantLabelsByLocalAddr`
      by name
- [x] `accounts-service/accounts/service.go` — `RegisterMicroService`
      extracted out of `cmd/main.go`
- [x] `accounts/service_test.go` — 2 new specs against a real SYS-account
      connection; full `accounts-service` suite green (27/27)
- [x] `go build`/`ginkgo` (shipping-service), `go build`/`ginkgo`
      (accounts-service), admin `npm run test` + `npm run build` all green
- [x] Live-reverified after the `accounts-service` refactor: rebuilt the
      container, confirmed `nats micro info accounts-service` still
      responds correctly post-extraction

---

### Phase 18 (DONE 2026-08-01) — Requestor/Responder Identity Headers

#### Goal

BR-D36/BR-026 (Phase 17a) made a message's real headers visible in the Request/Reply
panel, but no header actually identified *who* sent or answered a call — NATS doesn't
attach caller/responder identity to a message on its own; that's connection-level auth
state a handler's `Msg` never sees. Add explicit `Nats-Requestor` (on every request) and
`Nats-Responder` (on every reply) header, matching the convention BR-D36 already
established for real, wire-carried headers rather than observability-only ones.

#### Design decisions (2026-08-01)

- **`Nats-Requestor`** is set by the caller, instance-qualified (`"<name>/<instance ID>"`,
  matching `Nats-Responder`'s format and OpenTelemetry's `service.name`/`service.instance.id`
  split — added 2026-08-01 after review flagged that a bare name couldn't distinguish
  replicas): `refdataconsumer` (shipping-service's `rpc.*` caller) combines the connection's
  own `nats.Name(...)` with a NUID generated once per `Consumer`; `useNatsConnection.js`'s
  `request()` (the browser's `api.*` caller) combines `"seafreight-app"` with a random ID
  generated once per tab — so concurrent tabs are tellable apart. Tenant isn't included,
  since that's already the NATS account boundary itself and would just repeat what the
  account already encodes.
- **`Nats-Responder`** is set by the answering adapter on every reply, success or error alike,
  as `"<service's own nats.Name>/<micro.Service instance ID>"`. The subject alone already
  identifies which *service* answers a given `rpc.*`/`api.*` family in this repo (there's no
  fan-out), so the new information is the *instance* — `micro.AddService` generates a fresh
  unique ID per running process, letting the panel distinguish replicas if this ever scales
  horizontally.
- **Naming-inconsistency fix, found while implementing:** each adapter's `micro.Config.Name`
  (`refdata-rpc`, `shipping-api` — family-derived) didn't match its own connection's
  `nats.Name` (`refdata-service`, `shipping-service`). Left as-is, `Nats-Requestor` and
  `Nats-Responder` would show two different names for the same physical service — the panel
  would read as if the requestor and responder were different entities. Both `Config.Name`
  values were renamed to match their connection's `nats.Name` exactly as part of this phase.

#### Checklist

- [x] Confirm design (Nats-Requestor on request, Nats-Responder on reply) with user
- [x] `refdataconsumer.requestRPC` sets `Nats-Requestor` (shipping-service's `rpc.*` caller)
- [x] `useNatsConnection.js`'s `request()` sets `Nats-Requestor` (browser's `api.*` caller)
- [x] Requestor made instance-qualified too (`<name>/<NUID|per-tab ID>`) — symmetric with
      responder; `TestLookupCarriesInstanceQualifiedRequestorHeader` asserts format + stability
- [x] `natsrpc`/`browserrpc` `respondOK`/`respond`/`respondError` set `Nats-Responder`
- [x] Found and fixed `micro.Config.Name` mismatch (`refdata-rpc`→`refdata-service`,
      `shipping-api`→`shipping-service`)
- [x] Confirm BR wording (landed as BR-D37 + BR-027)
- [x] `BUSINESS_RULES-REFDATA.md` (BR-D37) + `BUSINESS_RULES-SHIPPING.md` (BR-027) updated
- [x] `ARCHITECTURE-COMMUNICATIONS.md` §6 updated
- [x] `go build`/`ginkgo` (both services), admin/seafreight-app `npm run build` all green
- [x] Live verification: both headers observed on the real wire (`nats` CLI + Admin panel)
      for `rpc.*` and `api.*`, both success and error replies

---

### Phase 19 (DONE 2026-08-03) — Merge auth-service into accounts-service

#### Goal

`auth-service` (Phase 15c) and `accounts-service` (Phase 14b) were reviewed
for whether the split still earned its keep. auth-service's only real job —
minting browser NATS credentials — read `accounts-service`'s own
`accounts.accounts` Postgres table over a second connection to the same
instance; it had no NATS connection, no independent lifecycle, and no state
of its own beyond what `accounts.Store` already held. That's a fake service
boundary, not a real one — merge them.

#### What changed

- `auth-service/auth/{handler.go,token.go}` moved to
  `accounts-service/auth/`, now importing `accounts.Store` directly instead
  of a duplicate read-only `AccountReader` over a second Postgres
  connection.
- `accounts-service/accounts/store.go` gained `ListActiveTenantNames`
  (active accounts minus `default`/`sys`, reusing `handler.go`'s
  `reservedAccountNames` map) — replaces auth-service's own `ListTenants`
  query.
- `accounts-service/cmd/main.go` wires one `*accounts.Store` into both
  `accounts.Handlers` (BasicAuth-gated `/api/accounts/*`) and
  `auth.Handlers` (ungated `/api/auth/*`), mounted on the same
  `http.ServeMux`. New `NATS_WS_URL` env var (default
  `ws://localhost:9222`) replaces auth-service's own copy of the same
  setting.
- `auth-service`'s Go module, Dockerfile, and `docker-compose.yml` service
  entry are gone. `accounts-service` now serves both route families on port
  7202; `vite.config.js`'s `/api/auth` proxy target moved from 7203 to
  7202.
- Test infrastructure for the `auth` package now calls `accounts.Migrate`
  for schema setup instead of hand-duplicating its `CREATE TABLE`
  statements — only possible now that both packages share one Go module.
- See `BUSINESS_RULES-ACCOUNTS.md`'s "Phase 19 — auth-service merged in"
  note for the full before/after.

#### Checklist

- [x] `accounts-service/auth/` package created (`handler.go`, `token.go`,
      tests), reading `accounts.Store` directly
- [x] `accounts.Store.ListActiveTenantNames` added + unit test
      (`accounts/store_test.go`)
- [x] `cmd/main.go` mounts both handler sets on one mux; `NATS_WS_URL` env
      var added
- [x] `auth-service/` directory deleted (module, Dockerfile, `cmd/`)
- [x] `docker-compose.yml`: `auth-service` entry removed, `accounts-service`
      gains `NATS_WS_URL`, `shipping-frontend`'s `depends_on` updated
- [x] `vite.config.js`'s `/api/auth` proxy retargeted to port 7202
- [x] `README.md` service table + Postgres credentials note updated
- [x] Stray `auth-service` path references fixed
      (`browserrpc/adapter.go`, `BUSINESS_RULES-SHIPPING.md`)
- [x] `go build`/`ginkgo ./...` green in `accounts-service` (48 specs: 36
      accounts + 12 auth)

---

### Phase 20 (PROPOSED — awaiting approval) — Ship Container Capacity Limit

#### Goal

Ships currently have no maximum container capacity — a ship can be loaded with an unbounded number of containers. Add a fixed `Capacity` to the Ship aggregate and enforce it as a load-time domain rule (BR-019), plus surface a load-capacity indicator column in `frontend-port` ("SeaFreight Flow") so the constraint is visible, not just enforced.

#### Design

- **`Ship` domain model** (`dictionary/internal/domain/ship.go`): add `Capacity int` to `ShipState` (ship.go:46-53) and `ShipAggregate` (ship.go:65-70), threaded through `Apply()`/`State()`/`FromState()`.
- **Setting capacity**: no "register ship" command exists — a ship's first `Arrive` is its registration (`ShipAggregate.Arrive()`, ship.go:124-144), which already set-once's `ShipName` when empty. `Capacity` follows the same set-once-at-first-arrival pattern: `ArrivePort` request gains an optional `capacity` field; if omitted on first arrival, a documented default is used (exact default — e.g. 20 — confirmed at implementation time, not fixed by this plan entry). There is still no update-ship command, so capacity is immutable after first arrival unless a follow-up phase adds one.
- **Enforcing BR-019 on `Load`**: `ContainerAggregate.Load()` (container.go:196-219) gains a capacity check alongside its existing BR-012/BR-010/BR-014/BR-008 checks. This needs the ship's *current* on-ship container count at command time — `ContainerHandler.LoadContainer()` (application/commands/container.go:87-106) resolves this before calling `cont.Load(...)`. Two candidate mechanisms, to be decided during implementation:
  1. Event-replay count (consistent with "JetStream is the source of truth" — Working Assumptions): count `.loaded`-without-subsequent-`.unloaded` container events for the ship's `shipID` at hydrate time.
  2. Read-model query against the existing manifest join (Shape A/B projection) — faster, but reads an eventually-consistent projection to guard a write (same class of trade-off Phase 23 documents for BR-008/BR-012 read-model guards).
- **Read model / API surface**: `ShipState`'s KV (Shape A/B) and Postgres projections need the new `Capacity` field so `GET` endpoints (fleet, shape-b ship, shape-c fleet) return it to the frontend.
- **Frontend (`frontend-port`)**: `FleetPanel.vue` (columns at lines 112-131) and `ShipsAtPortPanel.vue` (columns at lines 150-163) each gain a load-capacity indicator column pairing the new `capacity` field with the container count already computed via `store.manifestFor(shipID).length` (e.g. `12 / 50`, colored by fullness). Route any new column label through `l10n` (BR-D16), not a hardcoded literal.

#### Checklist

- [ ] Confirm default capacity value and whether `capacity` is required or optional on `ArrivePort`
- [ ] Decide event-replay vs read-model-guard mechanism for the current-count check (document the trade-off, mirroring Phase 23's treatment of BR-008/BR-012)
- [ ] `ShipState`/`ShipAggregate`: add `Capacity`, thread through `Apply()`/`State()`/`FromState()`
- [ ] `ArrivePort` command + REST handler: accept optional `capacity`, set-once on first arrival
- [ ] `ContainerAggregate.Load()`: new `ErrCapacityExceeded` check (BR-019)
- [ ] `ContainerHandler.LoadContainer()`: resolve current on-ship count before calling `Load()`
- [ ] KV (Shape A/B) + Postgres ship projections: persist and return `Capacity`
- [ ] Ginkgo specs written **before** implementation (red → green): `Container Domain Rules / BR-019` — load rejected at capacity, allowed under capacity, allowed exactly at capacity-minus-one
- [ ] `frontend-port`: load-capacity column in `FleetPanel.vue` and `ShipsAtPortPanel.vue`, via `l10n`
- [ ] `BUSINESS_RULES.md`: BR-019 updated from PROPOSED to enforced, with final error/enforcement/test references
- [ ] `go build ./...` + `ginkgo ./...` green; frontend build green


### Phase 21 — Write-Side Safety (Optimistic Concurrency + Publish Dedup)

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


### Phase 22 — Projection Hardening (Consumer-Side Idempotency + Explicit Limits)

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

### Phase 23 — Stream Split + Cross-Aggregate Consistency

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

1. **Read-model guard (default)** — the container handler reads the ship's KV projection (Shape A/B) to check docked state / current port. Fast and keeps the streams independent, but validates a write against an eventually-consistent read (stale-read window — which Phase 23 measures under load).
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


### Phase 24 — Performance & Load Testing (full suite)

#### Goal

Validate that the *final* architecture holds under realistic throughput and identify the bottlenecks before any production consideration, building on the baseline established in **Phase 10**. Runs after the write path (Phase 21) and stream split (Phase 23) are in place, so the scenarios those phases gate can finally be measured. The POC has two known scalability gaps — first characterised in Phase 10, re-measured here against the final architecture:

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
| Container load/unload burst — terminal throughput | Cross-stream (`SHIPPING` + `TERMINAL`) consumer lag under write pressure | needs Phase 23 |
| Projection lag — event published → KV updated | End-to-end latency of the Shape A/B projectors under load | this phase |
| Optimistic-concurrency contention — concurrent commands, same aggregate | Retry rate and latency cost of the Phase 21 sequence guard under contention | needs Phase 21 |

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

- [ ] Scenario: optimistic-concurrency contention — retry rate and latency cost of the Phase 21 sequence guard *(needs Phase 21)*
- [ ] Scenario: cross-stream burst — fire `SHIPPING` and `TERMINAL` events concurrently, measure projection consumer lag *(needs Phase 23)*
- [ ] Scenario: SSE fan-out — open 1 / 10 / 50 / 100 concurrent SSE clients, measure KV watch lag
- [ ] Scenario: projection lag — event published → KV updated, measured under load
- [ ] Re-measure the Phase 10 baseline scenarios against the final architecture (with guard + split) and record the before/after delta
- [ ] Finalise `demos/01-dictionary/PERFORMANCE.md` — full baseline numbers, degradation curves, identified thresholds
- [ ] Document architectural mitigations for each bottleneck (snapshot strategy, consumer parallelism, SSE load balancing)


### Phase 25 (optional, PLACEHOLDER — not yet a formal requirement) — Per-Tenant Runtime Theme Spike

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
- [x] Historical/archived docs left untouched on purpose: `Dictionary-POC-Plan-ARCHIVE.md`,
      `Dictionary-Service-Plan.md`, `.ai-archive/*` document *past* renumbering events and are
      frozen snapshots, not live cross-references

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
