# Architecture — EventSourcing CQRS POC

Deep reference for how this demo is implemented. For the overview and run instructions see [README.md](README.md).

---

## CQRS Pattern — Code Mapping

### Write Model

**`dictionary/internal/application/commands/commands.go`**

- `ShipHandler` — command handler with `ArrivePort()`, `DepartPort()`, `LoadCargo()`, `UnloadCargo()`
- `hydrate()` — replays full JetStream history to rebuild the aggregate before each write (no snapshot shortcut)
- `Publisher` interface — outbound port to JetStream

**`dictionary/internal/domain/ship.go`**

- `ShipAggregate` — pure domain aggregate; command methods enforce invariants and return domain events
- `Apply()` — applies one event to aggregate state (used by both write side and Shape C)
- `FromState()` — restores aggregate from a `ShipState` projection (used by projectors)

**Event store:** JetStream stream `DICTIONARY` (`internal/jstream/stream.go`), `LimitsPolicy` so replay is always possible.

---

### Projections

**`dictionary/internal/eventhandler/handler.go`**

- `RegisterShapeA()` — durable consumer `ship-shape-a`; projects each event delta directly into KV (no Postgres)
- `RegisterShapeB()` — durable consumer `ship-shape-b`; upserts Postgres first, then writes through to KV cache
- `currentAgg()` — reads current KV state into a `ShipAggregate` via `FromState()` before applying one delta, so the projector never replays the full stream

Each consumer is independently position-tracked and can lag, replay, or rebuild on its own.

---

### Read Models

| Shape | Read model | Query type | Key file |
|---|---|---|---|
| A | KV bucket `dict-a-{context}` (authoritative) | `ShapeA.ListShips()` | `queries/get_entry.go` |
| B | Postgres (canonical) + KV `dict-b-{context}` (write-through cache) | `ShapeB.GetShip()` / `ShapeB.ListShips()` | `queries/get_entry.go` |
| C | None — full JetStream replay on every call | `ShapeC.ReconstructFleet()` | `queries/shape_c.go` |

Shape B also exposes `EvictCacheShip()` to force the KV miss → Postgres → backfill path.

---

### Materialized Views

- **KV buckets** (`internal/kvstore/kv.go`) — `dict-a-{context}` (Shape A read model) and `dict-b-{context}` (Shape B cache); values are JSON-encoded `ShipState`
- **Postgres `ships` table** (`postgres/migrate.go`, `postgres/repository.go`) — Shape B canonical projection; upserted via `INSERT … ON CONFLICT DO UPDATE`
- **`ShipState` struct** (`domain/ship.go`) — shared projected value type stored in both KV and Postgres
- **Pinia stores** (frontend) — client-side materialized views fed by `kvstore.Watch()` → SSE (`rest/sse.go`); the same projection-from-event-stream pattern one layer further out

---

### Metadata Projections (`meta.*`)

Beyond per-ship state (`ship.*`), the KV store holds a second namespace for cross-cutting derived lookup sets that any part of the UI may need. The working superset of KV namespaces is:

| Namespace | Purpose | Status |
|---|---|---|
| `ship.*` | Per-ship current state (Shape A/B projections) | implemented |
| `meta.*` | Cross-cutting derived lookup sets (ports seen, cargo types, etc.) | in progress |
| `locale.*` | Localisation config per context | future |
| `tenant.*` | Tenant-specific configuration | future |

#### `meta.known-ports` — port dropdown data

The **Shipping Operations** form port dropdown needs to show all ports ever seen in the event stream — not just ports ships are currently docked at. This is maintained as a `meta.known-ports` entry in a `dict-meta-{context}` KV bucket, projected by the backend event handler.

**Current implementation (frontend-only, pre-Phase 7):**

- `stores/dictionary.js` maintains a `seenPorts` array in Pinia state, accumulated from live SSE watch events.
- `ShippingForm.vue` computes `portOptions` as the sorted union of a static `BASE_PORTS` list and `store.seenPorts` using a `Set`.
- The `<Select>` component has the `editable` prop — a typed port appears in the dropdown once its event flows back through the SSE stream.
- **Limitation:** `seenPorts` is in-memory only. Ports that ships have departed are not in current KV state, so they are lost on reload unless a ship happens to be currently docked there.

**Planned implementation (Phase 7 — backend KV projection):**

- The `eventhandler` projector will maintain `meta.known-ports` in `dict-meta-{context}` — updated incrementally on each `ShipArrived` / `ShipDeparted` event.
- `GET /api/meta/{context}/known-ports` will expose the KV value.
- On `connect()`, the frontend will seed `seenPorts` from this endpoint before the SSE stream opens, then continue merging live events as before.
- Result: the full port history survives app reload without event replay, and without the frontend needing to reconstruct it from scratch.

---

### Frontend Data Store (Pinia) Bindings to Backend

The Pinia store (`stores/dictionary.js`) is the browser-side equivalent of a server-side projection — a materialized view that stays current by receiving pushed events rather than polling.

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
