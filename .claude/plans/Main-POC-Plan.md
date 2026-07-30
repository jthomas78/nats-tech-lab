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

### Phase 23 — Stream Split + Cross-Aggregate Consistency

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

1. **Read-model guard (default)** — the container handler reads the ship's KV projection (Shape A/B) to check docked state / current port. Fast and keeps the streams independent, but validates a write against an eventually-consistent read (stale-read window — which Phase 23 measures under load).
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
- Context key (tenant/region/locale) is always present in the KV key — no global/unscoped lookups
- Eventual consistency is acceptable for dictionary reads
- No approval workflow, audit trail, or versioning needed for this POC
- Demo data is seeded via the command API (no seed scripts needed)
