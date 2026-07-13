# Architecture — EventSourcing CQRS POC

Deep reference for how this demo is implemented. For the overview and run instructions see [README.md](README.md).

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
- `hydratePair()` — rebuilds **both** aggregates from **one atomic replay** of `SHIPPING`. Identity is parsed from each subject. This keeps cross-aggregate rules strongly consistent until Phase 12 splits the stream.
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
- **Postgres `ports` table** (`postgres/port_repository.go`) — plain reference data, not a projection: no JetStream event ever writes it. Written directly by `POST /api/ports`; read by `ShipHandler`/`ContainerHandler` (BR-017/BR-018) and `GET /api/ports/{context}`. See "Event Sourcing vs Plain CRUD" below.
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

### Frontend Data Store (Pinia) Bindings to Backend

There are two frontends, each with its own Pinia store — both are browser-side equivalents of server-side projections: materialized views that stay current by receiving pushed events rather than polling.

| Frontend | Store | SSE channels |
|---|---|---|
| `frontend/` (admin, :5173) | `stores/dictionary.js` | `/api/watch/{context}` (Shape A + B ship buckets) |
| `frontend-port/` (Port Management, :5174) | `stores/port.js` | `/api/watch/{context}` (ships) + `/api/watch-terminal/{context}` (containers + `meta.*`) |

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
