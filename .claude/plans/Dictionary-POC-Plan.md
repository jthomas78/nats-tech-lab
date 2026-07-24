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
- Returned based on application context (tenant, region, locale)
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
- Context-aware key design (tenant/region/locale in key prefix)
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
- [x] Phase 9 — Subject Taxonomy + Doc Realignment (`{region}.events.{tenant}.{aggregate}.{id}.{event}`)
- [x] Phase 9.5 — Ports Reference Table (BR-017, BR-018)
- [x] Phase 9.6 — Postgres Tables Admin Panel (Reference Data → Ports)
- [x] Phase 10 — Performance Baseline (pull-forward, pre-Phase 11/15) — k6 harness, Shape C/hydration/throughput baselines
- [x] Phase 11 — Dictionary as a Service (APPROVED 2026-07-13) — see [Dictionary-Service-Plan.md](Dictionary-Service-Plan.md); sub-phases 11.1–11.11 all delivered

**Verification status (2026-07-09):** full compose stack runs end to end (5 services), Swagger UI live, both frontend `/api` proxies working, live smoke test of full container lifecycle passing, `go build`/`go vet`/`ginkgo ./...` (22/22 at that point) and both frontend builds green. Full detail in the archive.

### Phase 12 (DONE, with known gaps noted below) — Refdata Versioning, Tenancy & Template Inheritance

> **Full detail in separate design document: [Refdata-Versioning-Tenancy-Design.md](Refdata-Versioning-Tenancy-Design.md)** — sub-phases 12.1–12.7.

#### Goal

Evolve the refdata service from a single-context, unversioned CRUD store into a
multi-tenant reference data platform with corpus-level versioning, a draft/publish
lifecycle, first-class rollback with audit trail, multi-level template inheritance
(with overrides and additions, no deletion of inherited entries), version pinning by
consumers, and hybrid KV materialization (eager on publish + TTL-governed lazy
re-materialization on demand via rewrite-on-read).

#### Key design decisions

- **Context hierarchy** — contexts form a tree (`global → emea → emea-acme`); inheritance
  resolution walks child → root, first match wins; overrides break propagation per-item.
- **Corpus-level versioning** — an immutable snapshot of the entire flattened refdata set,
  replacing the current per-type `dictionary_set_versions` as the consumer-facing version.
- **Materialize on publish** — inheritance is resolved and flattened at publish time, not
  read time. Reads never walk the chain.
- **Rollback = new forward version** — version numbers only increase; rollback copies the
  target version's data into a new version and publishes it.
- **Hybrid KV** — versioned buckets (`refdata-{context}-v{N}`); active version has no TTL;
  superseded versions get bucket-level TTL; rewrite-on-read resets TTL for pinned old versions.
- **Backward compatible** — existing unversioned `refdata-{context}` bucket and API continue
  to serve "latest published."

#### Sub-phases

- [x] **12.1 — Context Hierarchy**: `contexts` table, REST endpoints (register/list/get with
      ancestors+descendants), recursive-CTE hierarchy traversal — now integration-tested
      against real Postgres (3+ level chain)
- [x] **12.2 — Corpus Versioning & Draft/Publish Lifecycle**: corpus snapshot tables, draft
      create/edit/publish, version listing, per-version contents (`GET .../draft`,
      `GET .../versions/{v}`), and a diff endpoint (`GET .../diff/{v1}/{v2}`, plain
      added/removed/changed key list per the resolved audit-scope decision)
- [x] **12.3 — Rollback with Audit**: first-class rollback creating forward versions,
      audit fields (`rolled_back_at`, `rolled_back_by`) — integration-tested including
      rollback to a non-immediately-preceding version
- [x] **12.4 — Template Inheritance Resolution**: `CreateDraft` now actually resolves
      inheritance from each ancestor context's latest *published* corpus (not just the
      same context's own prior version, which is what the first implementation pass did) —
      `domain.FlattenCorpus` is wired into the repository, not just unit-tested in isolation.
      Localization inheritance (resolved Q3) is implemented too: `corpus_localizations` flows
      with the item, and a new `PutDraftLocalization` (+ `PUT .../draft/localizations`) lets a
      child override one locale of an inherited item without overriding the item — the
      working-table `SetLocalization` path structurally can't do this (its FK requires the
      item to live in the same context's own `dictionary_items`). Integration-tested for a
      2-level chain including override survival across a later re-draft.
- [x] **12.5 — Hybrid KV Materialization & Version Pinning**: versioned KV buckets
      (`refdata-{context}-v{N}`), eager materialization on publish/rollback
      (`kvcache.VersionNotifier`), bucket-level TTL for superseded versions
      (`kvcache.SupersededVersionTTL` = 30d), rewrite-on-read on every versioned GET
      (`kvcache.VersionReader`), and the versioned read REST surface
      (`GET /api/refdata/v/{version}/{context}/{type}[/{code}]`,
      `GET /api/refdata/v/latest/...`). Integration-tested against a real embedded
      NATS/JetStream server (TTL and rewrite-on-read are genuine server behavior, not
      something a fake can stand in for) in `kvcache_versioned_integration_test.go`.
- [x] **12.6 — Frontend (Versioning Admin UI)**: new "Versioning" nav entry in `frontend/refdata`
      alongside the existing Localization view, `VersioningPanel.vue` with three tabs — Contexts
      (tree viewer + register-new-context dialog), Corpus Versions (create draft/publish/rollback,
      version table with parent/base-context-version columns), Diff (pick two versions, see
      added/removed/changed keys). New `stores/versioning.js` Pinia store. Verified end-to-end in
      the browser against the live docker stack: created a draft (200 items/600 localizations from
      the seed data), published it, made a working-table edit, drafted+published v2/v3, ran a real
      diff (correctly showed `currency/USD: changed`), and rolled back to v1 (v4 published with
      v1's content, v3 flipped to `rolled-back`, v1/v2 stayed `published` — versions coexisting
      indefinitely, confirmed via a direct `GET /api/refdata/v/4/...` hit showing v1's data).
- [x] **12.7 — Consumer Integration & Documentation**: `refdataconsumer.LookupAtVersion` reads
      `refdata-{context}-v{N}` directly (KV-first, versioned-REST fallback), independent of the
      existing unversioned `Lookup`; `ARCHITECTURE-DICTIONARY.md` documents the versioning model
      end to end. Unit-tested (embedded NATS) in `consumer_test.go`.

> **Correctness note (2026-07-22):** the first implementation pass (by Codex) had
> `domain.FlattenCorpus` written and unit-tested but never actually called from
> `CreateDraft` — a new draft only ever copied the *same context's* prior version, so no
> context ever really inherited from a different parent context. This has been fixed and is
> now covered by Postgres-backed integration tests in `corpus_repository_integration_test.go`
> (context_repository.go and corpus_repository.go had zero test coverage beyond pure
> in-memory domain checks before this pass).

> **Known gaps (2026-07-22), left for a later phase:** KV bucket cleanup once a version has
> no pinned consumers is deferred to a future pin registry (resolved open question 4);
> `corpus_references` exists in the schema but nothing populates or flattens typed
> references the way items and localizations are; and two Go `net/http` ServeMux route
> conflicts were found and fixed only once these routes were exercised against a real
> server rather than just `go build` — see `ARCHITECTURE-DICTIONARY.md`'s "Corpus
> Versioning, Tenancy & Template Inheritance" section and the design doc's "Versioned Read"
> note for detail; worth remembering that `go build`/`go vet` cannot catch this class of bug.

- [x] **12.8 — Subject Taxonomy: Region/Tenant → Context (both services)**: an audit of NATS
      subject naming against docs.nats.io/Synadia guidance found that both services built every
      subject from hardcoded `Region = "emea"` / `Tenant = "acme"` Go constants, while the real
      per-request tenant identity (`Context`) was carried only in the event payload
      (`event.Context`), never the subject — so `ShipHandler.hydrate()`'s replay filter and
      `ContainerHandler.hydratePair`/`hydrateByNaturalKey` never scoped by context at all: two
      tenants sharing a `shipID` or container natural key would silently merge event histories.
      Fixed by threading `Context`/`itemContext` into every subject and replay filter. Also added
      a subject/KV-key token format validator (BR-020, BR-D22), since `ShipID`, `TypeKey`, `Code`,
      and `Context` all flowed from REST input into a subject or KV key with no validation.
      Final subject shapes (after a same-day follow-up revision, see below): shipping
      `evt.{context}.shipping.{ship|container}.{id}.{event}`; refdata
      `evt.{context}.refdata.{typeKey}.changed`. **Note:** the literal category token (`evt`) is
      the *first* token, context second — not context-first as originally scoped — because
      JetStream refuses to create a stream whose subject filter has an unbounded wildcard in the
      leading position (`*.events.>` can textually overlap `$SYS.>`/`$JS.API.>`, and the server
      requires `NoAck: true` to allow it, which would break the synchronous `Publish`/PubAck flow
      every command handler relies on) — discovered via a real stream-creation failure during
      implementation, not anticipated in the original design pass. Region is dropped entirely (a
      future deployment-instance concern, not part of this lab's subject taxonomy); no
      ancestor-wildcard subject design — the leaf `Context` value is used verbatim, same string as
      the KV bucket-name convention. Requires a local dev data reset
      (`docker compose down -v && up --build` — old-shape stream data is disposable lab data, no
      migration/dual-read compatibility). NATS Accounts-based hard tenant isolation remains out of
      scope (Phase 18).

      **Same-day follow-up revision:** the shape above (`events.{context}.../refdata.{context}...`)
      shipped and was live-verified, then revised once more per a direct user request to unify both
      services under one shared prefix and add an explicit per-service partition: `Domain` constants
      (`"shipping"`, `"refdata"`) were added to each service's domain package, and every subject
      literal/wildcard/parser was updated so the category marker is `evt` (not `events`/`refdata`)
      with the service name as its own token — e.g. `evt.{context}.shipping.ship.{shipID}.{event}`,
      `evt.{context}.refdata.{typeKey}.changed`. A "reduce subject cardinality by moving entity id to
      a header" variant was discussed and explicitly reverted — entity id stays a subject token in
      every case, so `ShipHandler.hydrate()` keeps its targeted `FilterSubject`-based single-ship
      replay (no regression to a full-stream-replay-and-filter-by-header pattern).

#### Business Rules (New)

| Rule | Description |
|---|---|
| BR-V01 | At most one draft per context at a time |
| BR-V02 | Only a draft can be published |
| BR-V03 | Publish is atomic — entire corpus snapshot or nothing |
| BR-V04 | Rollback target must be a previously-published version |
| BR-V05 | Rollback creates a new forward version (numbers never go backward) |
| BR-V06 | A child context cannot delete an inherited item |
| BR-V07 | An override breaks propagation for that item to all descendants |
| BR-V08 | Publishing a parent does not automatically publish descendants |
| BR-020 (shipping) | shipID/context must be a valid subject/KV-bucket token |
| BR-D22 (refdata) | typeKey/code/context must be a valid subject/KV-key token |

See [Refdata-Versioning-Tenancy-Design.md](Refdata-Versioning-Tenancy-Design.md) for the
full data model, API surface, migration strategy, and open questions.

- [x] **12.9 — Ship Surrogate UUID Identity (mirrors Container's pattern)**: reverses an
      earlier decision (documented in `ARCHITECTURE.md`'s former "Why `Container` and not
      `Ship`" note) once it was clarified that `shipID` behaves like a name/call-sign/
      internal-fleet-code — mutable, reassignable — the same pressure that already justified
      `Container`'s surrogate key. `Ship`'s aggregate identity is now an immutable UUID
      (`ID`), minted by `RegisterShip()` (new, explicit) or implicitly by `ArrivePort()` on
      first arrival (optional pre-registration); `shipID` becomes a mutable natural key,
      renameable via the new `CorrectShipID()` command. Because `shipID` is mutable, natural-
      key resolution (`hydrateByNaturalKey()`) can no longer target one ship via `FilterSubject`
      the way Container's dedup check does — it folds every ship's history in a context and
      matches by *current* name (shared via `foldAllShips()`/`resolveShipByNaturalKey()` with
      `ContainerHandler.hydratePair`, which had the identical ship-resolution dependency on the
      old shipID-carrying subject). Postgres's `ships` table moves its conflict target to
      `(context, id)` (mirrors `containers`); the KV read model stays keyed by the natural
      `ship.{shipID}` for query convenience, so a correction is the one case requiring an
      explicit KV rekey (delete old key, put new). Also fixed, found via test failure during
      this work: Shape C's fleet-manifest join compared a container's `OnShipID` (always a
      natural key) against the ship's surrogate map key — silently broken by this change until
      corrected to compare against the ship's current `shipID`. Known limitation, verified live
      and documented not fixed: a container's `OnShipID` snapshots the ship's name at load time
      and doesn't track a later correction — renaming a ship mid-carriage leaves the container
      stuck (unload fails with both the new name, BR-013, and the stale old name, BR-012, since
      resolution is by current name); only unblocked by correcting back to the exact
      pre-correction name first. New rules: BR-021 (a shipID can only be
      registered once), BR-022 (a shipID can be corrected to another valid, unused shipID).
      Requires the same local dev-data reset as 12.8 (old ship events/rows use the pre-surrogate
      identity scheme).

| BR-021 (shipping) | A shipID can only be registered once |
| BR-022 (shipping) | A shipID can be corrected to another valid, unused shipID |

- [x] **12.10 (APPROVED 2026-07-24 — IMPLEMENTED 2026-07-24) — Dual-Transport RPC (`rpc.*`) + Admin UI Observability**

  > **Full detail in
  > [ARCHITECTURE-COMMUNICATIONS.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md)**
  > (status now IMPLEMENTED; see its embedded diagram, page "PROPOSED — Dual-transport
  > RPC (draft)" in
  > [architecture-dictionary.drawio](../../obsidian/V3-Platform/Architecture/Dictionary-POC/architecture-dictionary.drawio)
  > — diagram title kept as-is, only the doc's status line changed).

  #### Goal

  Add a narrow, internal-only NATS core request/reply transport (`rpc.*`) alongside
  each service's existing REST/Swagger surface, for the specific synchronous
  cross-service calls one service needs to make on another (e.g. shipping-service
  looking up refdata-service) — not a full mirror of REST. Give the Admin UI a live,
  non-persisted view of `rpc.*` traffic while it's open.

  #### Key design decisions (carried over from the design doc, see §§1–6 there)

  - `rpc.*` is Core NATS request/reply — no JetStream stream, no persistence, no
    replay. Distinct from `evt.*` (JetStream facts) and `cmd.*` (not used in this
    repo).
  - Subject shape: `rpc.{context}.{service}.{entity}.{action}.v{n}` (parallel to the
    `evt.*` grammar from 12.8), fixed `rpc` leading literal.
  - Dual-adapter pattern: new `internal/natsrpc/` adapter per service, built on
    `github.com/nats-io/nats.go/micro`, calling the **same**
    `commands.*Handler`/`queries.*` methods as the existing `rest/` adapter — no
    domain/application-layer changes required to add it.
  - Only wire `rpc.*` for operations another service actually needs synchronously
    (first concrete case: shipping-service → refdata-service item lookup), not every
    REST endpoint.
  - Runtime discovery via NATS Micro/Services (`$SRV.PING`/`INFO`/`STATS`); static
    docs via AsyncAPI, keeping `operationId` (Swagger) / subject / Go method name
    aligned.
  - Admin UI observability is a **separate, best-effort side-channel**, not a
    stream: each `natsrpc/` handler fire-and-forget publishes request and reply
    (with a shared correlation ID, including on error) to `obs.rpc.*`; Admin UI
    subscribes to `obs.rpc.>` only while the panel is open — no `RPCTRACE` stream,
    no TTL/backlog (rejected as unneeded per the doc's §6 rationale).

  #### Tasks

  - [x] Confirm/finalize business rules for this sub-phase — confirmed as
        transport/infrastructure rules, adopted as drafted: BR-D25 ("an `rpc.*`
        operation must exist as a `commands`/`queries` method already exposed via
        REST") and BR-D26 ("an `obs.rpc.*` publish must never block or fail the
        real RPC reply"), both added to `BUSINESS_RULES-REFDATA.md` (with a
        cross-reference note in `BUSINESS_RULES-SHIPPING.md` since
        shipping-service only *consumes* the rpc.* transport, it doesn't define
        new rules of its own).
  - [x] `refdata-service`: `internal/natsrpc/adapter.go` — `micro.AddService` +
        one `micro.AddEndpoint` (`item-get`) for `rpc.*.refdata.item.get.v1`,
        wired to the existing `commands.LocalizationHandler.ResolveItem()`
        method (the same one `GET /api/refdata/{context}/{type}/{code}` calls).
        Wired into `cmd/main.go` via `Handlers.MountRPC(nc, log)` (a new method
        on `composition.go`, parallel to `Mount`) — `natsrpc` is `internal/` so
        `cmd/main.go` can't import it directly.
  - [x] `shipping-service`: `internal/refdataconsumer`'s `fetchViaAPI` now tries
        `rpc.{context}.refdata.item.get.v1` first when `WithNATS(nc)` is
        configured (any error — no responder, timeout, malformed reply — falls
        through to the existing, well-tested REST path unchanged); wired in
        production via `dictionary/composition.go`'s
        `refdataconsumer.New(kvRefdata, refdataServiceURL(), refdataconsumer.WithNATS(mono.NC()))`.
        `monolith.Monolith` gained an `NC() *nats.Conn` accessor (previously only
        `jetstream.JetStream` was threaded through). Existing REST-fallback tests
        are unaffected (`WithNATS` is opt-in via a functional option).
  - [x] `obs.rpc.*` publish helper — `natsrpc.Adapter.publishObs()`: fire-and-forget
        `nc.Publish` (never `nc.Request`), correlation ID = the request's reply-to
        inbox, fires on both request and reply (including error replies), panic-
        recovered so a marshal/publish failure can never propagate to the caller.
  - [x] Admin UI: new `RpcPanel.vue` (`frontend/admin`) — `EventSource` against a
        new `GET /api/rpc-watch` SSE bridge (`dictionary/internal/rest/sse.go`'s
        `watchRPCObs`, subscribing `obs.rpc.>` via `nc.ChanSubscribe`); pairs
        request/reply rows by correlation ID in a reactive map (not an
        index-based array, since prepending new rows would shift indices).
        Verified live: a direct `nats request` to `rpc.emea-acme.refdata.item.get.v1`
        appears in the panel with matched request/reply payloads, and a
        not-found lookup renders as a red "error" row with the failure message.
  - [x] Integration tests: `refdata/natsrpc_test.go` (embedded core-NATS server,
        no JetStream needed) — BR-D25 (rpc.* and direct `ResolveItem()` return
        identical results, including the not-found error case) and BR-D26 (the
        real reply returns in <500ms with no `obs.rpc.>` subscriber, with a
        deliberately slow one, and the reply-side obs event still carries the
        error on failure). `internal/refdataconsumer/consumer_test.go` gained
        `TestLookupUsesRPCWhenConfigured` / `TestLookupFallsBackToRESTWhenRPCHasNoResponder`.
        97/97 refdata-service specs green; shipping-service `ginkgo ./...` green.
  - [x] Updated `ARCHITECTURE-COMMUNICATIONS.md` status from "draft/proposed" to
        "IMPLEMENTED (Phase 12.10, 2026-07-24)", naming the actual files/methods
        built.

- [x] **12.11 (APPROVED 2026-07-24 — IMPLEMENTED 2026-07-24) — `rpc.*` as the Sole Backend-to-Backend Transport (no REST fallback)**

  > **Full detail in
  > [ARCHITECTURE-COMMUNICATIONS.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md) § 7**
  > and `BUSINESS_RULES-REFDATA.md`'s BR-D28 (both IMPLEMENTED). This design
  > went through two earlier drafts (RPC-primary-with-REST-fallback, then
  > RPC-primary-with-circuit-breaker) before landing here — see § 7's
  > "superseded decisions" note.

  #### Goal

  Make `rpc.*` the **only** transport for backend-to-backend synchronous
  calls between `shipping-service` and `refdata-service` — no REST fallback
  in any form. Backend services should only be aware of NATS for
  inter-service calls: no HTTP client, base URL, or hostname/port config
  pointing at a peer backend service. On repeated `rpc.*` failure, return an
  error to the caller; do not degrade to REST. Frontend-to-backend traffic
  (REST/Swagger for `frontend/admin`, `frontend/refdata`,
  `frontend/seafreight-app`) is explicitly out of scope and unaffected — REST
  stays as each service's inbound surface for those callers.

  #### Key design decisions (carried over from the design doc, see § 7 there)

  - Audit finding (2026-07-24) that motivated this: only `Lookup`/`item.get`
    has any `rpc.*` path, and even that is third-tier (KV hit, then RPC only
    on a miss, then REST on any RPC error) — `ResolveType`, `LookupAtVersion`,
    and `Locales` have no RPC path at all and always call REST.
  - Extend `rpc.*` coverage to every `refdataconsumer` operation: `item.get`
    (exists), new `type.list` (`ResolveType`), a versioned `item.get.v{n}`
    (`LookupAtVersion`), and `locales.list` (`Locales`) — land all four
    **before** removing REST, so no operation is ever left with zero working
    transport mid-rollout.
  - On a KV cache miss/stale entry, the consumer retries `rpc.*` a bounded
    number of times (with backoff); on exhaustion, return an error — no
    REST fallback, no circuit breaker that degrades to REST.
  - **Location transparency is a hard invariant.** Delete
    `REFDATA_SERVICE_URL`, `refdataServiceURL()` (and its hardcoded
    `http://localhost:7201` default), `baseURL`/`httpc`, and every
    REST-calling method (`fetchViaAPI`, `fetchTypeViaAPI`,
    `fetchVersionedViaAPI`, REST-based `Locales`) from
    `internal/refdataconsumer`. `Consumer` holds a `*nats.Conn` and nothing
    else — wire it unconditionally in `dictionary/composition.go` (no
    `WithNATS` opt-in).
  - KV-first caching (BR-D08) and frontend/edge REST traffic are unaffected —
    this only changes what happens on a cache miss/refetch.
  - Resolved: `shipping-service`'s own callers (the Phase 11.3/11.6 demo REST
    handlers) map a retry-exhausted error to HTTP 503 via a shared
    `writeRefdataError()` helper — see the corresponding task below.

  #### Tasks

  - [x] Confirm/finalize business rules for this sub-phase — BR-D28 drafted
        in `BUSINESS_RULES-REFDATA.md` (PROPOSED). Confirmed 2026-07-24
        (final, after two reversed drafts): `rpc.*` is the sole
        backend-to-backend transport — no REST fallback, no circuit
        breaker. A bounded number of retries against `rpc.*`, then an error
        to the caller. All REST-client coupling (`REFDATA_SERVICE_URL`,
        `refdataServiceURL()`/`http://localhost:7201`, `baseURL`/`httpc`) is
        removed from `internal/refdataconsumer`, not merely deprioritized.
        REST/Swagger is unaffected as each service's inbound surface for
        frontend/edge clients and human/test-suite debugging.
  - [x] `refdata-service`: `internal/natsrpc/adapter.go` gained a `Deps`
        struct (`Localizations`, `Items`, `VersionReader`, `Projector`,
        `Log` — all nil-safe) replacing `New`'s old positional args, plus
        three new endpoints wired to the same `commands`/`queries` methods
        their REST counterparts already call (BR-D25 parity): `type-list`
        (`rpc.*.refdata.type.list.v1`, reuses `ItemGetResponse` per item),
        `item-get-versioned` (`rpc.*.refdata.item.get-versioned.v1`, corpus
        version in the request body; response is `kvcache.VersionedEntry`
        directly), `locales-list` (`rpc.*.refdata.locales.list.v1`). Every
        error reply now also carries `notFound bool` (`isNotFoundErr()`,
        mirroring REST's own not-found status-code switch), replacing the
        old bare `error string` shape. `composition.go`'s `MountRPC` updated
        to the `Deps` struct.
  - [x] `shipping-service`: `internal/refdataconsumer/consumer.go` fully
        rewritten — `fetchViaRPC` (existing), `fetchTypeViaRPC`,
        `fetchVersionedViaRPC`, and `Locales` (all new) cover all four
        operations. Deleted: `fetchViaAPI`, `fetchTypeViaAPI`,
        `fetchVersionedViaAPI`, REST-based `Locales`, `baseURL`/`httpc` on
        `Consumer`, `WithNATS` (NATS is now a required `New(kv, nc, ...)`
        constructor argument), and `refdataServiceURL()` /
        `REFDATA_SERVICE_URL` from `dictionary/composition.go` and
        `docker-compose.yml`. `checkRPCError()` maps the new `notFound`
        field to this package's `ErrNotFound`.
  - [x] Implemented bounded retry with backoff in `requestRPC()`: default 1
        initial attempt + 2 retries (3 total), linear backoff
        (150ms × attempt), 3s per-attempt timeout — all overridable via
        `WithRPCRetries`/`WithRPCBackoff`/`WithRPCTimeout`. Exhaustion
        returns `ErrRPCUnavailable`, wrapping the last underlying NATS
        error. Values recorded in `ARCHITECTURE-COMMUNICATIONS.md` § 7.
  - [x] Decided and implemented: `dictionary/internal/rest/handlers.go`
        gained a shared `writeRefdataError()` used by `getRefdataDemo`,
        `listRefdataType`, and `listRefdataLocales` — maps
        `refdataconsumer.ErrNotFound` → 404 (unchanged) and
        `refdataconsumer.ErrRPCUnavailable` → 503 (new), distinct from the
        generic 500. Judged to be REST-layer error handling for a Phase
        11.3/11.6 demo endpoint, not a Ship/Container domain rule, so it's
        documented in `ARCHITECTURE-COMMUNICATIONS.md` § 7 and BR-D28 rather
        than as a new `BUSINESS_RULES-SHIPPING.md` entry.
  - [x] Integration tests: `refdata/natsrpc_test.go` gained BR-D25 parity
        Context blocks for `type.list` and `locales.list` (same
        `NATS RPC Adapter` Describe, reusing its embedded core-NATS server)
        plus a separate `item.get-versioned` Describe (needs its own
        embedded JetStream server, seeded directly via
        `kvcache.NewVersionMaterializer` — no Postgres needed, same
        no-Postgres convention as the rest of the file), covering both the
        success and not-found cases. `internal/refdataconsumer/consumer_test.go`
        replaced `TestLookupFallsBackToRESTWhenRPCHasNoResponder` and
        `TestLookupMissForwardsLocaleToAPI` with
        `TestLookupReturnsErrRPCUnavailableWhenNoResponder`,
        `TestLookupRetriesBeforeSucceeding` (proves retries actually loop,
        not just fail once), `TestLookupMissForwardsLocaleToRPC`, and added
        RPC-path coverage for `ResolveType`/`LookupAtVersion`/`Locales`
        (`TestResolveTypeUsesRPCWhenBucketEmpty`,
        `TestLookupAtVersionMissUsesRPC`, `TestLocalesUsesRPC`,
        `TestLocalesReturnsErrRPCUnavailableWhenNoResponder`). New
        `dictionary/internal/rest/refdata_demo_error_test.go` covers the
        503 mapping for all three demo handlers. 106/106 refdata-service
        specs green; shipping-service `ginkgo ./...` green (82 specs across
        4 suites, plus all `go test` packages).
  - [x] Updated `ARCHITECTURE-COMMUNICATIONS.md` § 7 and BR-D28 from
        PROPOSED to IMPLEMENTED, recording the retry/backoff values and
        endpoints actually built.

---

### Phase 13 (PROPOSED — awaiting approval) — Ship Container Capacity Limit

#### Goal

Ships currently have no maximum container capacity — a ship can be loaded with an unbounded number of containers. Add a fixed `Capacity` to the Ship aggregate and enforce it as a load-time domain rule (BR-019), plus surface a load-capacity indicator column in `frontend-port` ("SeaFreight Flow") so the constraint is visible, not just enforced.

#### Design

- **`Ship` domain model** (`dictionary/internal/domain/ship.go`): add `Capacity int` to `ShipState` (ship.go:46-53) and `ShipAggregate` (ship.go:65-70), threaded through `Apply()`/`State()`/`FromState()`.
- **Setting capacity**: no "register ship" command exists — a ship's first `Arrive` is its registration (`ShipAggregate.Arrive()`, ship.go:124-144), which already set-once's `ShipName` when empty. `Capacity` follows the same set-once-at-first-arrival pattern: `ArrivePort` request gains an optional `capacity` field; if omitted on first arrival, a documented default is used (exact default — e.g. 20 — confirmed at implementation time, not fixed by this plan entry). There is still no update-ship command, so capacity is immutable after first arrival unless a follow-up phase adds one.
- **Enforcing BR-019 on `Load`**: `ContainerAggregate.Load()` (container.go:196-219) gains a capacity check alongside its existing BR-012/BR-010/BR-014/BR-008 checks. This needs the ship's *current* on-ship container count at command time — `ContainerHandler.LoadContainer()` (application/commands/container.go:87-106) resolves this before calling `cont.Load(...)`. Two candidate mechanisms, to be decided during implementation:
  1. Event-replay count (consistent with "JetStream is the source of truth" — Working Assumptions): count `.loaded`-without-subsequent-`.unloaded` container events for the ship's `shipID` at hydrate time.
  2. Read-model query against the existing manifest join (Shape A/B projection) — faster, but reads an eventually-consistent projection to guard a write (same class of trade-off Phase 16 documents for BR-008/BR-012 read-model guards).
- **Read model / API surface**: `ShipState`'s KV (Shape A/B) and Postgres projections need the new `Capacity` field so `GET` endpoints (fleet, shape-b ship, shape-c fleet) return it to the frontend.
- **Frontend (`frontend-port`)**: `FleetPanel.vue` (columns at lines 112-131) and `ShipsAtPortPanel.vue` (columns at lines 150-163) each gain a load-capacity indicator column pairing the new `capacity` field with the container count already computed via `store.manifestFor(shipID).length` (e.g. `12 / 50`, colored by fullness). Route any new column label through `ui-copy` (BR-D16), not a hardcoded literal.

#### Checklist

- [ ] Confirm default capacity value and whether `capacity` is required or optional on `ArrivePort`
- [ ] Decide event-replay vs read-model-guard mechanism for the current-count check (document the trade-off, mirroring Phase 16's treatment of BR-008/BR-012)
- [ ] `ShipState`/`ShipAggregate`: add `Capacity`, thread through `Apply()`/`State()`/`FromState()`
- [ ] `ArrivePort` command + REST handler: accept optional `capacity`, set-once on first arrival
- [ ] `ContainerAggregate.Load()`: new `ErrCapacityExceeded` check (BR-019)
- [ ] `ContainerHandler.LoadContainer()`: resolve current on-ship count before calling `Load()`
- [ ] KV (Shape A/B) + Postgres ship projections: persist and return `Capacity`
- [ ] Ginkgo specs written **before** implementation (red → green): `Container Domain Rules / BR-019` — load rejected at capacity, allowed under capacity, allowed exactly at capacity-minus-one
- [ ] `frontend-port`: load-capacity column in `FleetPanel.vue` and `ShipsAtPortPanel.vue`, via `ui-copy`
- [ ] `BUSINESS_RULES.md`: BR-019 updated from PROPOSED to enforced, with final error/enforcement/test references
- [ ] `go build ./...` + `ginkgo ./...` green; frontend build green

---

### Phase 14 — Write-Side Safety (Optimistic Concurrency + Publish Dedup)

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

---

### Phase 15 — Projection Hardening (Consumer-Side Idempotency + Explicit Limits)

#### Goal

Make projections safe under redelivery and reordering **by engineering, not by accident**. Today's safety rests on "redelivering the same event re-applies the same upsert" — true only if delivery order is preserved, which depends on unexamined consumer defaults. Also make the stream's "never discard" property an explicit decision rather than an implicit absence of limits.

#### Design

- **KV writes**: replace naive `Put` with a guarded write — the stored value carries the source event's stream sequence; the projector skips any event older than what's stored, using `Update` with expected revision (CAS loop) so a stale redelivery can never clobber newer state.
- **Postgres projection**: same guard — persist the last-applied stream sequence per row and skip older events in the upsert (`WHERE excluded.seq > current.seq` style).
- **Consumer ordering**: verify `Consume()` callback concurrency and `MaxAckPending` defaults against current nats.go docs (do not assume); set `MaxAckPending` explicitly per projector and document the ordering guarantee relied upon.
- **Explicit retention decision**: `CreateStream` currently sets no `MaxAge`/`MaxMsgs`/`MaxBytes` — "never discard" is true only implicitly. Make it explicit: document unbounded-is-deliberate in the config (or set `DiscardPolicy` intentionally), so the config can't be copied forward with the decision invisible.
- **Poison messages**: current behavior (ack-on-unmarshal-failure to avoid redelivery loops) is documented; consider a dead-letter subject (`{region}.dlq.{tenant}.…`) instead of silently acking.

#### Checklist

- [ ] Verify `Consume()` ordering / `MaxAckPending` semantics against current nats.go docs
- [ ] `kvstore.Store`: guarded write API (sequence-aware CAS); all projector call sites migrated off naive `Put`
- [ ] Postgres projectors: last-applied-sequence guard in upserts
- [ ] Explicit `MaxAckPending` on all projector consumers
- [ ] `CreateStream`: retention/discard decision made explicit in code comment + config
- [ ] Poison-message policy: dead-letter subject or documented ack-and-log, decided and implemented
- [ ] Ginkgo specs: out-of-order redelivery does not clobber newer KV/Postgres state; duplicate redelivery is a no-op
- [ ] `go build ./...` + `ginkgo ./...` green

---

### Phase 16 — Stream Split + Cross-Aggregate Consistency

#### Goal

Extract container events from the shared `SHIPPING` stream into a dedicated `TERMINAL` stream, turning the two aggregates into two independent bounded contexts. This is a **single-variable change** on top of Phases 8–14: the aggregates, rules, and frontends are unchanged — only the stream topology moves. Post-Phase 9 this is even cleaner than originally planned: **the subjects themselves do not change** — a subject can belong to only one stream, so the split is purely moving the `…container.>` binding from `SHIPPING` to `TERMINAL`. The purpose is to make the **invariant-spanning-two-aggregates problem** concrete and demonstrate the solution options.

#### The problem this phase exposes

After the split, BR-008 (container destPort vs ship's current port) and BR-012 (ship must be docked) still need **both** aggregates' state — but the container command handler can no longer get the ship's state from the same replay. `ContainerAggregate` hydrates from `TERMINAL`; the ship's docked state lives in `SHIPPING`. There is no atomic cross-stream replay.

| Stream | Subject binding | Bounded context |
|---|---|---|
| `SHIPPING` | `evt.{tenant}.shipping.ship.>` | Ship movements |
| `TERMINAL` | `evt.{tenant}.shipping.container.>` | Container lifecycle |

#### Solution options to implement and document

The demo implements **option 1** as the default and documents the trade-offs of all three:

1. **Read-model guard (default)** — the container handler reads the ship's KV projection (Shape A/B) to check docked state / current port. Fast and keeps the streams independent, but validates a write against an eventually-consistent read (stale-read window — which Phase 16 measures under load).
2. **Hydrate both streams** — the container handler additionally replays `SHIPPING` for the ship. Strongly consistent, but the container context is no longer independent and every load/unload replays two streams.
3. **Saga / compensating event** — accept the write optimistically and emit a compensating `container.load-rejected` event if the ship turns out not to be docked. The "correct" DDD answer for separate contexts; heaviest to implement.

#### Checklist

- [ ] `internal/jstream/stream.go` — add the `TERMINAL` stream binding `evt.{tenant}.shipping.container.>`; `SHIPPING` keeps only `…ship.>` (subjects themselves unchanged post-Phase 12.8)
- [ ] `domain/events.go` — route container subject builders / stream-name references to `TERMINAL`
- [ ] `application/commands/container.go` — hydrate containers from `TERMINAL`; replace the in-replay ship check with the **read-model guard** (option 1) for BR-008 / BR-012
- [ ] `eventhandler/` — container projector consumes from `TERMINAL`; ship projector unchanged on `SHIPPING`
- [ ] Ginkgo specs — BR-008 / BR-012 still green via the read-model guard; add a spec documenting the stale-read window (guard sees pre-departure state)
- [ ] Frontend (`frontend/`): JetStream panel stream selector — add `TERMINAL` entry (`streamOptions`); backend `streamJetStream` switch — add `TERMINAL` case
- [ ] Frontend (`frontend-port/`): add SSE watch on `TERMINAL.*`
- [ ] `ARCHITECTURE.md` — document the two-stream topology, the cross-aggregate invariant problem, and the three solution options with the chosen default
- [ ] `go build ./...` + `ginkgo ./...` green

---

### Phase 17 — Performance & Load Testing (full suite)

#### Goal

Validate that the *final* architecture holds under realistic throughput and identify the bottlenecks before any production consideration, building on the baseline established in **Phase 10**. Runs after the write path (Phase 14) and stream split (Phase 16) are in place, so the scenarios those phases gate can finally be measured. The POC has two known scalability gaps — first characterised in Phase 10, re-measured here against the final architecture:

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
| Container load/unload burst — terminal throughput | Cross-stream (`SHIPPING` + `TERMINAL`) consumer lag under write pressure | needs Phase 16 |
| Projection lag — event published → KV updated | End-to-end latency of the Shape A/B projectors under load | this phase |
| Optimistic-concurrency contention — concurrent commands, same aggregate | Retry rate and latency cost of the Phase 14 sequence guard under contention | needs Phase 14 |

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

- [ ] Scenario: optimistic-concurrency contention — retry rate and latency cost of the Phase 14 sequence guard *(needs Phase 14)*
- [ ] Scenario: cross-stream burst — fire `SHIPPING` and `TERMINAL` events concurrently, measure projection consumer lag *(needs Phase 16)*
- [ ] Scenario: SSE fan-out — open 1 / 10 / 50 / 100 concurrent SSE clients, measure KV watch lag
- [ ] Scenario: projection lag — event published → KV updated, measured under load
- [ ] Re-measure the Phase 10 baseline scenarios against the final architecture (with guard + split) and record the before/after delta
- [ ] Finalise `demos/01-dictionary/PERFORMANCE.md` — full baseline numbers, degradation curves, identified thresholds
- [ ] Document architectural mitigations for each bottleneck (snapshot strategy, consumer parallelism, SSE load balancing)

---

### Phase 18 (optional) — NATS Accounts Tenancy Spike

#### Goal

Today tenancy is a string convention: one unauthenticated `nats.Connect`, tenant scoping enforced only by the subject/bucket names the application happens to use. NATS **accounts** are the server-enforced isolation mechanism — this spike exercises them so "subject prefixes are enough" vs "accounts are required" is a measured decision, not an assumption, before the real platform commits.

#### Scope (spike, not production auth)

- Two accounts in server config (e.g. `acme`, `globex`) with per-tenant credentials; backend connects per tenant.
- Verify the server actually enforces isolation: tenant A's credentials cannot publish/subscribe/replay tenant B's subjects or KV buckets — including JetStream API access (streams/consumers are per-account resources).
- Resolve the taxonomy interaction: inside an account, the `{tenant}` subject token is redundant — decide whether the token stays (portability across account-per-tenant vs shared-account deployments) or the account *is* the tenant boundary, and document the trade-off.
- Note but don't implement: operator/JWT mode vs static server-config accounts; exports/imports for any cross-tenant sharing.

#### Checklist

- [ ] Server config with two accounts + creds; docker-compose wiring
- [ ] Isolation verified by test: cross-tenant publish/subscribe/JetStream access rejected by the server
- [ ] Decision documented in `ARCHITECTURE.md`: account-per-tenant vs shared-account+prefixes, and what the `{tenant}` subject token means under each

---

### Phase 19 (optional, PLACEHOLDER — not yet a formal requirement) — Per-Tenant Runtime Theme Spike

#### Goal

Explore whether UI theme/branding (colors, tokens, light/dark presets) can be externalized per tenant and swapped **at runtime**, without a separate build/deploy per tenant. Raised as a "does it make sense to put theme data in the dictionary service" question (2026-07-17) — not a formal requirement yet, so this is scoped as a spike to prove the mechanism out, not a commitment to build it.

#### Why this isn't just another `ui-copy`-style refdata type

Theme data is fetch-then-apply's worst case: `ui-copy`/label fallback (BR-D11) and cold-paint caching (BR-D19) tolerate a brief English-text mismatch on first paint, but a full-page flash of the *wrong tenant's brand colors* before a client-side fetch resolves is far more visible and jarring — the same class of problem, magnified. Client-side fetch-and-apply (the pattern used everywhere else in this repo) is therefore the wrong default here.

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
- [ ] `.claude/memory/` notes citing phase numbers (none currently do)

---

## Working Assumptions

- JetStream is the source of truth: commands hydrate aggregates by replaying the stream, and Postgres (Shape B) and KV (Shapes A/B) are downstream projections populated only by event consumers — never written directly by the command path. (Superseded earlier assumption that Postgres was the source of truth for Shape B.)
- NATS KV is appropriate for low-latency lookup and watch-based invalidation
- Context key (tenant/region/locale) is always present in the KV key — no global/unscoped lookups
- Eventual consistency is acceptable for dictionary reads
- No approval workflow, audit trail, or versioning needed for this POC
- Demo data is seeded via the command API (no seed scripts needed)
