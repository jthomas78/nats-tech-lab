# Architecture — EventSourcing CQRS POC

Deep reference for how this demo is implemented. For the overview and run instructions see [README.md](README.md).

---

## Shape Classification — Variant Identifiers

The POC compares several CQRS/event-sourcing **variants** and measures how they
perform as it evolves. Each variant gets a **stable identifier** so the same
operation can be compared across implementations and phases — the k6 harness
tags every metric with its `shape` id (see [PERFORMANCE.md](PERFORMANCE.md)),
and these ids are frozen once assigned.

**Grammar:** `Shape<Surface>.<Mechanism>[.<Scope>]`

- **Surface** — the CQRS role: `Write` (command side — validates, produces
  events), `Proj` (consumer side — materialises read models from events),
  `Read` (query side — serves reads).
- **Mechanism** — a short code naming the *distinguishing technique* of the
  variant (grouped by facet below). An id names the code(s) that distinguish
  the variant and holds the rest as context; compose facets with `.` when more
  than one varies (e.g. `Write.FR.OB`).
- **Scope** — suffix: *(default)* single aggregate; **`.AGG`** = aggregation
  (fold/join/group across many aggregates, or a derived cross-cutting set). Sub-
  types may be suffixed later (`.AGG/join`, `.AGG/group`, `.AGG/set`).

### Code registry (grouped by facet)

**State access** — how current state is obtained/materialised:

| Code | Meaning | Status |
|---|---|---|
| `FR` | full replay (seq `1…end` of the relevant scope) | implemented |
| `S` | snapshot + tail replay | reserved (Phase 17) |
| `KV` | KV projection (materialised read model in NATS KV) | implemented |
| `PG` | Postgres canonical + KV write-through cache | implemented |
| `CRUD` | plain Postgres, non-event-sourced | implemented (ports) |

**Publish / commit reliability** — Write surface, how the event is durably produced:

| Code | Meaning | Status |
|---|---|---|
| `DP` | direct publish (`js.Publish`; the publish *is* the commit) | implemented (all commands) |
| `OB` | transactional outbox (atomic state-write + relay) | reserved |

**Consumer reliability** — Proj surface, how events are safely applied:

| Code | Meaning | Status |
|---|---|---|
| `AL1` | at-least-once, non-idempotent (redelivery on failure) | implemented |
| `IDEM` | idempotent consumer / inbox dedup | reserved (Phase 15) |

`FR` scope note: it means **whole-stream** replay on `Read.FR.AGG` (fleet) but
**per-aggregate** (filtered) replay on `Write.FR` (hydration) — scope is a
property of the surface, so it isn't repeated in the code.

### Phase-9 inventory

**Write (command) surface** — publish reliability is `DP` throughout.

| Id | Path / fn | Distinguishing mechanism | Scope | Legacy | File |
|---|---|---|---|---|---|
| `Write.FR` | ship arrive/depart (`hydrate`) | full replay, filtered per-ship | single | — | commands.go:84 |
| `Write.FR` | container register (`hydrateByNaturalKey`) | full replay, per-container | single | — | container.go:185 |
| `Write.FR.AGG` | container load/unload (`hydratePair`) | one replay folds **ship + container** | multi | — | container.go:148 |
| `Write.CRUD` | port register | Postgres INSERT (idempotent) | ref data | — | port.go:23 |

**Proj (projection/consumer) surface** — consumer reliability is `AL1` throughout.

| Id | Consumer (durable) | Writes to | Scope | Legacy | File |
|---|---|---|---|---|---|
| `Proj.KV` | `ship-shape-a` | `dict-a` KV (IS the read model) | per-ship | **Shape A** | handler.go:21 |
| `Proj.PG` | `ship-shape-b` | Postgres `ships` → `dict-b` KV cache | per-ship | **Shape B** | handler.go:37 |
| `Proj.PG` | `container-projector` | Postgres `containers` → `container` KV | per-container | — | container_handler.go:19 |
| `Proj.KV.AGG` | `meta-projector` | `meta` KV `known-containers` (merge-set) | derived set | — | meta_handler.go:32 |

**Read (query) surface**

| Id | Route | Mechanism | Scope | Legacy | File |
|---|---|---|---|---|---|
| `Read.PG` | `GET /api/shape-b/ships/{ctx}/{id}` | KV cache → Postgres fallback | single | Shape B (read) | get_entry.go:67 |
| `Read.FR.AGG` | `GET /api/shape-c/fleet` | full replay: fold all + join manifest | multi | **Shape C** | shape_c.go:42 |
| `Read.KV.AGG` | `GET /api/manifest/{ctx}/{ship}` | filter `container` KV (join) | multi/join | — | terminal.go:65 |
| `Read.KV.AGG` | `GET /api/terminal/{ctx}/{port}` | filter `container` KV (group) | multi/group | — | terminal.go:49 |
| `Read.KV.AGG` | `GET /api/containers/{ctx}` | scan `container` KV | multi/set | — | terminal.go:25 |
| `Read.KV.AGG` | `GET /api/meta/{ctx}/known-containers` | get `meta` KV set | derived set | — | meta.go:34 |
| `Read.CRUD` | `GET /api/ports/{ctx}`, `GET /api/admin/ports/{ctx}` | Postgres SELECT | ref data | — | port_repository.go |
| `Read.KV` | *(single-ship KV lookup)* | KV | single | Shape A | get_entry.go:22 — defined, **unwired** |

*(Out of scheme — observability, no read model: the SSE `/api/watch*` streams and raw `/api/jetstream/*`.)*

### Legacy `A`/`B`/`C` alias map

The old letters straddled **surfaces**, which is why they read as overlapping —
`A`/`B` are mostly *projection* strategies, `C` a *read* strategy:

| Legacy | Maps to | Note |
|---|---|---|
| Shape A | `Proj.KV` (+ unwired `Read.KV`) | "KV as read model" is a projection choice |
| Shape B | `Proj.PG` + `Read.PG` | the projection **and** its cached read (two surfaces) |
| Shape C | `Read.FR.AGG` | read-time reconstruction across all aggregates |

The ids are a **documentation / measurement layer**: HTTP route slugs
(`/api/shape-b`, `/api/shape-c`) and existing code are unchanged. Aggregation
(`.AGG`) now appears explicitly on all three surfaces — `Write.FR.AGG`
(`hydratePair`), `Proj.KV.AGG` (`meta-projector`), and the `Read.*.AGG` queries.

---

## CQRS Pattern — Code Mapping

### Write Model — two aggregates, one stream

Phase 8 introduced a second aggregate. Both are co-located on the single
`SHIPPING` stream, partitioned by subject:

| Aggregate | Subjects | Rules |
|---|---|---|
| `ShipAggregate` (`domain/ship.go`) | `emea.events.acme.ship.{shipID}.{arrived\|departed}` | BR-001 … BR-003, BR-017 |
| `ContainerAggregate` (`domain/container.go`) | `emea.events.acme.container.{uuid}.{registered\|loaded\|unloaded}` | BR-008 … BR-016, BR-018 |

Ports (the `originPort`/`destPort`/arrival-port values referenced above) are
**not** a third aggregate — they're plain Postgres reference data (BR-017,
BR-018 check them, but don't event-source them). See "Event Sourcing vs Plain
CRUD" below.

**`dictionary/internal/application/commands/commands.go`**

- `ShipHandler` — `ArrivePort()`, `DepartPort()`
- `hydrate()` — replays only `emea.events.acme.ship.{shipID}.>` to rebuild one ship before each write
- `replayStream()` — shared full-stream fold retained for cross-aggregate container commands
- `Publisher` interface — outbound port to JetStream

**`dictionary/internal/application/commands/container.go`**

- `ContainerHandler` — `RegisterContainer()`, `LoadContainer()`, `UnloadContainer()`
- `hydratePair()` — rebuilds **both** aggregates from **one atomic replay** of `SHIPPING`. Identity is parsed from each subject. This keeps cross-aggregate rules strongly consistent until Phase 16 splits the stream.
- `RegisterContainer()` — mints a fresh surrogate id (`newSurrogateID()`, a dependency-free UUID v4) after `hydrateByNaturalKey()` confirms the natural key is free (BR-015, resolved against the event stream — authoritative, not from a read projection)

#### Container identity — surrogate key (Phase 8.3)

`Container`'s aggregate identity is an immutable **surrogate key** (`id`, a UUID) minted at registration — *not* the ISO 6346 `containerID`. The UUID is carried in the subject; `containerID` remains in the payload as the mutable natural key (BR-015).

- **Write side:** `hydratePair()` resolves `containerID → id` from the `.registered` event, then folds strictly by `id`.
- **Postgres:** `containers` primary key is `(context, id)`; `container_id` carries a `UNIQUE (context, container_id)` constraint.
- **KV read model:** the `container-{context}` bucket stays keyed by the human-facing `container.{containerID}` (query convenience) and carries `id` as a field — so it doubles as the natural-key → id lookup.

**Why `Container` and not `Ship`.** The scope is deliberately container-only. `Container`'s natural key is ISO 6346 — an **external interchange standard** other systems reference and that can need correcting (BR-016 exists precisely because a bad ID slipped in). `Ship`'s id is an internal slug with no external-format rule, no correction pressure, so a surrogate key there would be indirection without benefit. This is the recognised industry default: use a surrogate key where the natural key is an externally-governed standard you don't fully control.

**`dictionary/internal/domain/ship.go` / `container.go`**

- Command methods enforce invariants and return domain events; `Apply()` folds one event into state (used by write side, projectors via `FromState()`, and Shape C)
- Cargo is no longer part of the ship aggregate — a ship's manifest is the container join (`onShipID == shipID`)
- `ContainerState` models location as two explicit nullable fields (`terminalPort` / `onShipID`, exactly one non-nil) so queries never branch on status

**Event store:** JetStream stream `SHIPPING` (`internal/jstream/stream.go`), `LimitsPolicy` so replay is always possible.

---

### Projections

**`dictionary/internal/eventhandler/`**

- `RegisterShapeA()` / `RegisterShapeB()` consume `emea.events.acme.ship.>`
- `RegisterContainers()` consumes `emea.events.acme.container.>`
- `RegisterMeta()` consumes `emea.events.acme.>` and maintains the `meta.*` lookup sets
- `currentAgg()` / `currentContainerAgg()` — read current KV state into an aggregate via `FromState()` before applying one delta, so projectors never replay the full stream

Each consumer is independently position-tracked and can lag, replay, or rebuild on its own.

---

### Read Models

| Shape | Read model | Query type | Key file |
|---|---|---|---|
| A | KV bucket `dict-a-{context}` (authoritative) | `ShapeA.ListShips()` | `queries/get_entry.go` |
| B | Postgres (canonical) + KV `dict-b-{context}` (write-through cache) | `ShapeB.GetShip()` / `ShapeB.ListShips()` | `queries/get_entry.go` |
| C | None — full JetStream replay on every call | `ShapeC.ReconstructFleet()` | `queries/shape_c.go` |
| Terminal | KV bucket `container-{context}` | `Terminal.ListByPort()` (yard) / `Terminal.ListByShip()` (manifest) | `queries/terminal.go` |
| Meta | KV bucket `meta-{context}` | `Meta.KnownContainers()` | `queries/meta.go` |
| Ports | Postgres `ports` table (not KV, not event-sourced) | `PortHandler.List()` | `postgres/port_repository.go` |

Shape B also exposes `EvictCacheShip()` to force the KV miss → Postgres → backfill path.
Shape C now folds **both** aggregate types from the same replay and returns each
ship with its manifest (`ShipWithManifest`) plus every reconstructed container.

---

### Materialized Views

- **KV buckets** (`internal/kvstore/kv.go`) — all context-scoped: `dict-a-{context}` (Shape A ships), `dict-b-{context}` (Shape B cache), `container-{context}` (container projection), `meta-{context}` (lookup sets)
- **Postgres `ships` + `containers` tables** (`postgres/`) — canonical projections; upserted via `INSERT … ON CONFLICT DO UPDATE` (containers conflict on the surrogate key `(context, id)`; `container_id` is `UNIQUE`)
- **Postgres `ports` table** (`postgres/port_repository.go`) — plain reference data, not a projection: no JetStream event ever writes it. Written directly by `POST /api/ports`; read by `ShipHandler`/`ContainerHandler` (BR-017/BR-018), `GET /api/ports/{context}` (names, for dropdowns), and `GET /api/admin/ports/{context}` (raw rows — name + `createdAt` — for the admin Postgres Tables panel, below). See "Event Sourcing vs Plain CRUD" below.
- **`ShipState` / `ContainerState` structs** (`domain/`) — shared projected value types stored in both KV and Postgres
- **Pinia stores** (frontends) — client-side materialized views fed by `kvstore.Watch()` → SSE (`rest/sse.go`); the same projection-from-event-stream pattern one layer further out. The Port Management frontend even performs the manifest join (`onShipID == shipID`) client-side over its projected containers.

---

### Metadata Projections (`meta.*`)

Beyond per-entity state, the KV store holds a namespace for cross-cutting derived lookup sets that any part of the UI may need. The working superset of KV namespaces is:

| Namespace | Bucket family | Purpose | Status |
|---|---|---|---|
| `ship.*` | `dict-a-*` / `dict-b-*` | Per-ship current state (Shape A/B projections) | implemented |
| `container.*` | `container-*` | Per-container current state (terminal projection) | implemented (Phase 8) |
| `meta.*` | `meta-*` | Cross-cutting derived lookup sets | implemented (Phase 8) |
| `locale.*` | — | Localisation config per context | future |
| `tenant.*` | — | Tenant-specific configuration | future |

#### `meta.known-containers`

The container ID picker needs the **full history** of registered container
IDs — not just what current entity state happens to reference. This is
maintained as a sorted JSON array in the `meta-{context}` bucket by the
`meta-projector` durable consumer (`eventhandler/meta_handler.go`):

- `container.registered` → merge the container ID into `known-containers`

Exposed over REST: `GET /api/meta/{context}/known-containers`.

On `connect()` both frontends seed the container picker from this endpoint
before the SSE streams open, then keep merging live `META` watch events on
`/api/watch-terminal/{context}`. Result: the full history survives app reload
without event replay and without client-side reconstruction.

Because a single durable consumer processes events sequentially, the
read-merge-write on the meta key has no concurrent writers.

`meta.known-ports` (the equivalent projection for ports) was **retired** —
see "Event Sourcing vs Plain CRUD" below for why ports moved to a Postgres
reference table instead of a derived KV projection.

---

### Event Sourcing vs Plain CRUD — Design Heuristic

Not every piece of state in this domain is event-sourced, and the ports
registry (`postgres/port_repository.go`, `POST/GET /api/ports`) is the
worked example of where the lab draws that line.

**The deciding question is "does anything need to replay this to reconstruct
state," not "does it change over time."**

| | Ship / Container | Ports |
|---|---|---|
| Write path | `ArrivePort`/`Register` etc. publish a JetStream event | `POST /api/ports` writes straight to Postgres, no event |
| State machine | Yes — `arrived → docked → departed`, `registered → loaded → unloaded`, with cross-aggregate rules that need a point-in-time replay of both aggregates together (BR-008, BR-012, BR-014) | No — a port is either registered or not; there is no transition to get wrong |
| Reconstructable from history? | Yes, and exercised directly — Shape C (`ReconstructFleet`) rebuilds ships **and** containers from `seq=1` with no KV/Postgres involved | No consumer ever asks "what ports existed as of sequence N" — Shape C's reconstructed fleet doesn't need a reconstructed ports list; the registry is looked up live at command time |
| Audit need | The sequence of transitions *is* the domain fact (BR-015's duplicate check is resolved against the authoritative log, not a projection) | Satisfied by a plain `created_at` column — no one needs to replay to answer "when was this port added" |

Before event-sourcing a new entity in this lab, check whether it actually
clears that bar. It's an "it depends" call the moment either of these becomes
true — and neither is true for ports today:

- The entity gains a **real state machine** (e.g. ports could be
  deactivated or renamed, and a stale in-flight command needs to know which
  state was in effect).
- Someone needs a **temporal query** (e.g. "which ports were valid when this
  historical container was registered"), not just current state.

Forcing event sourcing onto something that never clears this bar just adds
ceremony — subjects, consumers, projections — with no corresponding benefit.
Reference/master data (lookup tables, config, enums) is the common case that
fails the bar; but don't treat "is it reference data" as the test on its own,
since some reference-looking tables secretly need history (a rate table
where "what was in effect on date X" matters) and some lifecycle-looking
entities are simple enough for plain CRUD if nothing ever replays them.

---

### Postgres Tables Panel (Admin UI)

`frontend/admin/src/components/PostgresTablesPanel.vue`, mounted in `App.vue` right
after `JetStreamPanel` — the two panels pair the "raw source" views together:
JetStream shows the raw event log, this panel shows a raw Postgres table that
has **no** event log at all. It's the concrete UI counterpart to "Event
Sourcing vs Plain CRUD" above.

- Collapsible, same hand-rolled header/collapse pattern as `JetStreamPanel`/`ShapeCPanel` (no shared composable — copy-pasted per existing convention).
- Contents are grouped under a heading (currently one: **Reference Data**), each group a `Tabs` block with one tab per table. Today that's just **Ports**. Adding another Postgres table later (e.g. a "Projections" group with the `ships`/`containers` tables) means adding another heading + `Tabs` block, not a redesign.
- Data source: `GET /api/admin/ports/{context}` (`rest/handlers.go` → `commands.PortHandler.ListRecords` → `domain.PortRepository.ListRecords` → `postgres/port_repository.go`), returning raw rows (`name`, `createdAt`) — distinct from `GET /api/ports/{context}`, which returns names only and backs the dropdowns.
- No live push channel (unlike KV, Postgres writes here aren't watched) — a manual refresh button re-fetches, and the table also refetches when the Fleet context changes.

---

### Frontend Data Store (Pinia) Bindings to Backend

There are two frontends, each with its own Pinia store — both are browser-side equivalents of server-side projections: materialized views that stay current by receiving pushed events rather than polling.

| Frontend | Store | SSE channels |
|---|---|---|
| `frontend/admin/` (admin, :5173) | `stores/dictionary.js` | `/api/watch/{context}` (Shape A + B ship buckets) |
| `frontend/seafreight-app/` (Port Management, :5174) | `stores/port.js` | `/api/watch/{context}` (ships) + `/api/watch-terminal/{context}` (containers + `meta.*`) |

The sections below describe the admin store; the port store follows the same pattern with two `EventSource` connections and client-side joins (`dockedShips`, `yardContainers`, `manifestFor`).

#### Connection lifecycle

```
connect()
  └─ new EventSource(GET /api/watch/{context})   ← long-lived HTTP connection
       └─ source.onmessage = (msg) => applyWatchEvent(JSON.parse(msg.data))
```

`connect()` is called on app mount and whenever the Fleet context dropdown changes. The previous `EventSource` is closed first (`disconnect()`), then a new one is opened scoped to the selected context (e.g. `global`, `atlantic-fleet`).

#### Server push path

```
NATS KV write (Shape A or B projector)
  └─ kvstore.Watch() in rest/sse.go                ← backend watches the KV bucket
       └─ writes JSON event to HTTP response stream  ← SSE frame: "data: {...}\n\n"
            └─ browser EventSource fires onmessage   ← intercept point in the store
                 └─ applyWatchEvent(event)            ← mutates Pinia state
                      └─ Vue components re-render     ← reactive binding
```

No polling. No manual refresh. The component re-renders because it reads from reactive Pinia state, and `applyWatchEvent` mutates that state directly.

#### `applyWatchEvent` — what it does

Each SSE event carries: `shape` (A or B), `op` (PUT / DEL / PURGE), `key`, `value` (the `ShipState` JSON), and `revision` (NATS KV sequence number).

```js
applyWatchEvent(event) {
  const target = event.shape === 'A' ? this.shapeA : this.shapeB

  if (event.op === 'PUT') {
    target[event.key] = { state: event.value, revision: event.revision }
    // also merges any new port into seenPorts for the dropdown
  } else {
    delete target[event.key]   // DEL or PURGE — ship removed from view
  }

  this.events.unshift({ ...event, at: new Date().toLocaleTimeString() })
  if (this.events.length > 50) this.events.pop()   // KV watch log, capped
}
```

#### EventSource vs WebSocket

`EventSource` (SSE) is used here rather than WebSocket because the data flow is one-directional: the server pushes, the browser only reads. Commands (Arrive, Depart, Load, Unload) travel back to the server as separate `fetch` POST requests — they do not need the watch channel.

| | EventSource (SSE) | WebSocket |
|---|---|---|
| Direction | Server → browser only | Full duplex |
| Protocol | Plain HTTP | `ws://` upgrade |
| Proxy/firewall support | Works without config | Requires explicit support |
| Auto-reconnect | Built in | Must implement manually |
| Used for | KV watch stream | Not used in this demo |

#### Context scoping

The Fleet dropdown sets `store.context`. Changing it calls `connect()`, which reconnects the `EventSource` to `/api/watch/{newContext}`. The backend watch endpoint (`rest/sse.go`) opens a `kvstore.Watch()` on `dict-a-{context}` and `dict-b-{context}` — so the frontend view is always isolated to the selected context bucket. `shapeA` and `shapeB` in the store are cleared on reconnect so stale data from the previous context does not bleed through.

---

### Snapshots

**Not formally implemented.** Two implicit approximations exist:

1. **Projector-side implicit snapshot** — `currentAgg()` in `eventhandler/handler.go` reads current KV state into a `ShipAggregate` via `FromState()`. KV acts as a rolling snapshot so projectors apply one delta per event rather than replaying from `seq=1`.

2. **Write-side has no snapshot** — `hydrate()` in `commands.go` replays all events from `seq=1` on every command. This is the main scalability gap: as the stream grows, every write gets slower. A proper snapshot would checkpoint aggregate state at a known sequence number and replay only the tail.

---

## Reference Data Service (`backend/refdata-service/`)

Phase 11, [Dictionary-Service-Plan.md](../../.claude/plans/Dictionary-Service-Plan.md) — a
**separate Go service and container**, not a module in the shipping backend's monolith. It shares
the same Postgres instance as `shipping-service` but owns its own schema (`refdata`) and tables; it does
not touch the `SHIPPING` stream, KV buckets, or Postgres tables the shipping backend uses.

**Why plain CRUD, not event-sourced.** Per the "Event Sourcing vs Plain CRUD" heuristic above:
nothing in the platform ever needs to replay a dictionary item's history to reconstruct it — only
its *current* value is ever consulted. So there is no aggregate and no event log. NATS JetStream
*is* used (Phase 11.3), but strictly for cache distribution and a bounded change-event feed —
never as this service's source of truth (Dictionary-Service-Plan.md's Q6). Postgres remains
authoritative throughout.

**Layout** mirrors the shipping backend's hexagonal style, scoped to its own module:

```
refdata-service/
  cmd/main.go                    — connects Postgres + NATS, runs migration + seed, starts HTTP
  refdata/
    composition.go               — wires Postgres repos + KV cache + REFDATA stream into handlers
    seed.go                      — idempotent ISO/UNECE/UN seed data (currencies, countries, …)
    internal/
      domain/                    — DictionaryType/Item/Reference/Localization, BR-D01–D06 sentinel errors
      application/commands/      — ItemHandler, ReferenceHandler, TypeHandler, LocalizationHandler
      postgres/                  — migrate.go (schema refdata) + repo implementations + VersionRepository
      kvcache/                   — Q5 versioned-read protocol: Projector (bump+rebuild+publish), Entry/MetaEntry
      jstream/, kvstore/         — same wrapper shape as the shipping backend's (separate module, so duplicated)
      rest/                      — REST + Swagger + SSE watch endpoint
```

**Identity.** A `DictionaryItem`'s identity is the natural composite key `(context, type_key,
code)` — no surrogate key. Unlike `Container` (Phase 8.3), a dictionary code is never an external
interchange standard a *different* system might correct out from under this service; the type
registry itself defines the code space, so there's no natural-key-correction pressure to design
around. There is deliberately no per-item version column either — versioning is a property of the
type's whole set (BR-D04), not of one row.

### Q5 versioned-read protocol (Phase 11.3)

Every mutation (`ItemHandler`/`ReferenceHandler`/`LocalizationHandler`, via the shared
`domain.ChangeNotifier` port implemented by `kvcache.Projector`) does three things, in order:

1. **Bump** `{context, type}`'s set version — a single atomic Postgres `UPSERT ... RETURNING`
   (`postgres.VersionRepository.Bump`), so a concurrent bump is never lost or torn.
2. **Rebuild** the affected item's KV cache entry (`refdata-{context}` bucket, key `{type}.{code}`)
   — the item plus its localizations and outbound references, stamped with the new version — and
   the type's `{type}._meta` entry (version + item count). A deleted item's key is removed instead.
3. **Publish** a bounded change-event *pointer* (`{region}.refdata.{tenant}.{type}.changed` on the
   `REFDATA` stream, `LimitsPolicy` + explicit `MaxAge` — 48h in this lab) carrying only
   `{typeKey, context, version}`, never item state. `REFDATA` is a notification channel, not an
   event store; a consumer that missed a push can always re-derive truth from KV or the REST API.

**Cache miss / cold start.** `Projector.Backfill` performs the same rebuild at the type's *current*
version, without bumping it or publishing an event — used by the REST `GET` item handler as a
best-effort side effect after every successful read, so a consumer that hit a miss and fell
through to the API leaves the cache warm for the next reader (identical in spirit to Shape B's
miss path).

**Consumer demo (shipping backend).** `backend/shipping-service/internal/refdataconsumer` reads the
`refdata-{context}` KV bucket directly — the shipping backend has no dependency on
refdata-service's Go code, only on the bucket-naming and JSON-shape convention, exactly as two
independent platform services would agree in the real system. `Consumer.Lookup` reads the item's
cache entry and the type's `_meta` in the same call: if the entry's stamped version doesn't match
`_meta`'s current version (or either key is missing), it falls through to
`GET /api/refdata/{context}/{type}/{code}` on refdata-service's REST API — the "updatable read on
version mismatch" this design targets. Exposed for the demo at
`GET /api/refdata-demo/{context}/{type}/{code}` on the shipping backend (`{"source": "kv-cache" |
"api-refetch"}`), the hazard-class type being the concrete example. `REFDATA_SERVICE_URL` (compose:
`http://refdata-service:8080`) points at the REST fallback.

**Testing.** Domain-rule Ginkgo specs (`refdata/item_test.go`, `refdata/reference_test.go`,
`refdata/localization_test.go`) run against in-memory fake repos (`refdata/fakes_test.go`) — no
real Postgres/NATS. `refdata/kvcache_test.go` runs against a real embedded in-process NATS server
(JetStream + KV), same convention as the shipping backend's `integration_test.go`, covering version
bump atomicity under concurrency, cache/`_meta` rebuild, change-event publication, cold start, and
miss backfill. The consumer demo's version-mismatch behavior is covered by
`backend/shipping-service/internal/refdataconsumer/consumer_test.go` (embedded NATS KV + `httptest` fake REST API).

### Dictionary frontend (`frontend/refdata/`, Phase 11.4)

A fourth Vue 3 + PrimeVue v4 app, same UniFi theme preset (`@unifi-theme`) and structural
conventions as `frontend/admin/` and `frontend/seafreight-app/` (dev port `5175`; nginx proxies `/api/` straight to
`refdata-service:8080` in the Docker build, not to `shipping-service`).

```
frontend/refdata/src/
  App.vue                       — topbar + two-column layout (type navigator, content)
  api.js                        — thin REST client, same request()/error convention as the other frontends
  stores/dictionary.js          — Pinia store: types+items fetched fresh from REST (plain CRUD, not
                                   an event-sourced read model); SSE watch on refdata-{context} only
                                   drives the cache-status widget's "something changed" signal
  components/
    TypeNavigator.vue           — left rail, types with item counts
    ItemGrid.vue                — main panel: locale selector, show-deprecated toggle, add/edit/
                                   deprecate/delete (BR-D02: delete offered but a 409 on a referenced
                                   item is caught and the toast points at Deprecate instead)
    ItemEditorDialog.vue        — Localizations tab + References tab, each list-then-add
    LocalesPanel.vue            — registered locales, add-locale form (with default flag),
                                   per-type localization completeness for a chosen locale
    CacheStatusWidget.vue       — Postgres set version vs KV _meta version (Q5, made visible);
                                   re-fetches on type change and on a matching SSE watch event
```

**Two REST additions this frontend needed** that weren't required by any earlier sub-phase:
`GET /api/refdata/{context}/{type}/{code}/localizations` and `.../references` (list, not just
single-relation `Get`/`Expand`) so the editor can show what's already there, and
`GET /api/refdata/{context}/{type}/cache-status` (Postgres version + KV `_meta` version + item
count + `inSync` bool in one response) so the widget doesn't have to parse KV JSON client-side.

AI-assisted translation is **out of scope** (parked per the Phase 11 approval decision) — the
Localizations tab only supports manual entry.

**Verified live** (2026-07-14): full stack via `docker compose up --build`, exercised in-browser —
type navigator counts, item grid CRUD, localization add + fallback-chain resolution, reference
creation (`country.AE --defaultCurrency--> currency.AED`), locales panel, and the cache status
widget correctly reporting `Postgres version == KV version, in sync` after a live mutation.
