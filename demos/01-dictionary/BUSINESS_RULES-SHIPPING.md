# Business Rules — Shipping Domain

> Split out of `BUSINESS_RULES.md` to keep per-domain reads small. See that
> file's index for the Reference Data Service rules (BR-D*).
>
> Phase 12.10: shipping-service's NATS RPC client wrapper
> (`internal/refdataconsumer`) is a *consumer* of the `rpc.*` transport rules
> governed by [BUSINESS_RULES-REFDATA.md](BUSINESS_RULES-REFDATA.md)'s
> BR-D25/BR-D26 — no separate shipping-side rule is needed since the
> constraints (RPC mirrors an existing REST-backed method; `obs.rpc.*` never
> blocks/fails a reply) are enforced entirely on the `refdata-service` adapter
> side.
>
> Phase 12.11 (IMPLEMENTED, 2026-07-24): BR-D28 adds a shipping-side
> behavioral requirement — `refdataconsumer` calls `rpc.*` exclusively (no
> REST fallback) on every cache miss/refetch across all four of its methods,
> with a bounded number of retries before returning `ErrRPCUnavailable` — but
> the rule itself is still recorded in `BUSINESS_RULES-REFDATA.md` (BR-D28)
> since it governs the transport contract between the two services, not a
> shipping-domain rule of its own. The one shipping-side consequence —
> mapping `ErrRPCUnavailable` to HTTP 503 in the Phase 11.3/11.6 demo
> endpoints — is REST-layer error handling, not a Ship/Container domain
> invariant, so it's documented in `ARCHITECTURE-COMMUNICATIONS.md` § 7 and
> BR-D28 rather than as a BR-0xx entry here.
>
> Phase 15 (browser NATS WebSocket transport, Main-POC-Plan.md Phase 15)
> reverses this: shipping-service now also *serves* `rpc.*` itself
> (`dictionary/internal/natsrpc/`), the same role refdata-service's own
> adapter plays for its domain, plus a new `notify.*` publish family with no
> refdata-service equivalent. BR-023/BR-024 below are shipping-service's own
> rules for these, following the same "one rule for the transport contract,
> not one per subject" convention BR-D25/BR-D26 established.

Domain rules enforced before any event is published to JetStream. A rule
violation returns an error to the caller; no event is written.

Two aggregates share the single `SHIPPING` stream (Phase 8):

- **Ship** rules live in `dictionary/internal/domain/ship.go`
- **Container** rules live in `dictionary/internal/domain/container.go`

Cross-aggregate rules (BR-008, BR-012, BR-014) need both aggregates' state.
Both hydrate from **one atomic replay** of the `SHIPPING` stream
(`commands.hydratePair`), so these checks are strongly consistent. Phase 23
splits the stream and turns exactly these rules into the
invariant-spanning-two-aggregates problem.

All rules must have a corresponding test: ship rules in
`dictionary/integration_test.go`, container rules in
`dictionary/container_test.go`.

---

## Ship Rules

### BR-001 — Cannot arrive at a port already docked at
A ship that is currently docked at port X cannot arrive at port X again.

- **Error:** `ErrAlreadyDocked` — "ship is already docked at this port"
- **Enforced in:** `ShipAggregate.Arrive()`
- **Test:** `Domain Rules / BR-001`

---

### BR-002 — Must depart before arriving at a new port
A ship that is currently docked at port X cannot arrive at port Y without first departing port X.

- **Error:** `ErrMustDepart` — "ship must depart current port first (X)"
- **Enforced in:** `ShipAggregate.Arrive()`
- **Test:** `Domain Rules / BR-002`

---

### BR-003 — Cannot depart a port the ship is not at
A ship can only depart the port it is currently docked at. Attempting to depart a different port, or departing while already at sea, is rejected.

- **Error:** `ErrNotDocked` — "ship is not docked at this port (currently: X)"
- **Enforced in:** `ShipAggregate.Depart()`
- **Test:** `Domain Rules / BR-003`

---

### BR-017 — A ship can only arrive at a registered port
Ports are reference data (a Postgres `ports` table, not an event-sourced aggregate — registered via `POST /api/ports`). Arriving at a port that isn't registered is rejected.

- **Error:** `ErrUnknownPort` — "port is not registered"
- **Enforced in:** `ShipAggregate.Arrive()` — the application layer (`ShipHandler.ArrivePort`) resolves `portKnown` via `domain.PortRepository.Exists()` and passes it in as a parameter, the same pattern used for the cross-aggregate checks in `container.go`.
- **Test:** `Domain Rules / BR-017`

---

## Retired Rules (Phase 8)

Cargo moved off the ship aggregate: a ship's manifest is now the container
join (`onShipID == shipID`). The ship-cargo rules were retired and replaced:

| Retired | Was | Replaced by |
|---|---|---|
| BR-004 | Cannot load cargo unless docked | BR-012 |
| BR-005 | Cannot unload cargo unless docked | BR-012 |
| BR-006 | Cannot unload cargo not in the manifest | BR-011 + BR-013 |
| BR-007 | Cargo payload required (input validation) | container input validation in `commands/container.go` |

---

## Container Rules

### BR-008 — Cannot load a container already at its destination
A container whose destination port matches the ship's current port cannot be loaded — it has already been delivered.

- **Error:** `ErrContainerAtDestination` — "container destination matches the ship's current port"
- **Enforced in:** `ContainerAggregate.Load()` *(cross-aggregate: needs ship's current port)*
- **Test:** `Container Domain Rules / BR-008`

---

### BR-009 — A container can only be unloaded at its destination port
Unloading anywhere other than the container's registered destination is rejected.

- **Error:** `ErrWrongDestination` — "container can only be unloaded at its destination port"
- **Enforced in:** `ContainerAggregate.Unload()` *(cross-aggregate: needs ship's current port)*
- **Test:** `Container Domain Rules / BR-009`

---

### BR-010 — A container must be in-terminal to be loaded
Only a container sitting in a terminal yard can be crane-loaded. A container already on a ship cannot be loaded again.

- **Error:** `ErrContainerNotInTerminal` — "container must be in a terminal to be loaded"
- **Enforced in:** `ContainerAggregate.Load()`
- **Test:** `Container Domain Rules / BR-010`

---

### BR-011 — A container must be on-ship to be unloaded
Only a container currently on a ship can be unloaded. A container in a yard cannot be unloaded.

- **Error:** `ErrContainerNotOnShip` — "container must be on a ship to be unloaded"
- **Enforced in:** `ContainerAggregate.Unload()`
- **Test:** `Container Domain Rules / BR-011`

---

### BR-012 — A ship must be docked to load or unload containers
Container operations require the ship to be in port; a ship at sea can do neither.

- **Error:** `ErrNotInPort` (defined in ship.go, reused) — "ship must be docked to load or unload containers"
- **Enforced in:** `ContainerAggregate.Load()` / `Unload()` *(cross-aggregate: needs ship's current port)*
- **Test:** `Container Domain Rules / BR-012` (load + unload variants)

---

### BR-013 — A container can only be unloaded from the ship it is actually on
Unloading a container from a ship that is not carrying it (`onShipID != shipID`) is rejected.

- **Error:** `ErrWrongShip` — "container is not on this ship"
- **Enforced in:** `ContainerAggregate.Unload()`
- **Test:** `Container Domain Rules / BR-013`

---

### BR-014 — A container can only be loaded when the ship is docked at the container's terminal port
Loading pulls a container from a specific yard: the ship must be docked at that yard's port (`terminalPort == ship.currentPort`). Without this rule a ship docked in Singapore could load a container sitting in Rotterdam.

- **Error:** `ErrContainerNotAtPort` — "container is not in a terminal at the ship's current port"
- **Enforced in:** `ContainerAggregate.Load()` *(cross-aggregate: needs ship's current port)*
- **Test:** `Container Domain Rules / BR-014`

---

### BR-015 — A container ID can only be registered once
Re-registering an existing container ID would silently reset its state (e.g. teleporting an on-ship container back into a yard).

- **Error:** `ErrContainerExists` — "container is already registered"
- **Enforced in:** `ContainerAggregate.Register()` — the rule decision stays in the domain (`c.registered`), but since Phase 8.3 the container's identity is a surrogate key (UUID), so uniqueness is a **natural-key** constraint. The application (`RegisterContainer`) resolves the natural key against the event stream (`hydrateByNaturalKey`) and folds any existing `.registered` event in, so the domain still sees `c.registered == true` and rejects the duplicate. Resolution is against the authoritative event log, not an eventually-consistent read projection.
- **Test:** `Container Domain Rules / BR-015` and `Container Domain Rules / surrogate key …`

---

### BR-016 — A container ID must be in ISO 6346 format (TCKU + 7 digits)
Every container ID must start with the fixed owner prefix `TCKU` (case-sensitive) followed by exactly 7 digits, e.g. `TCKU1234567`. This lab fixes the owner code rather than validating the full ISO 6346 owner-code space.

- **Error:** `ErrInvalidContainerID` — "container ID must be in ISO 6346 format: TCKU followed by 7 digits"
- **Enforced in:** `ContainerAggregate.Register()`
- **Test:** `Container Domain Rules / BR-016`

---

### BR-018 — A container's origin and destination ports must both be registered
Registering a container with an origin or destination port that isn't in the ports registry is rejected. Reuses `ErrUnknownPort` (BR-017's error), since it's the same underlying rule applied to the container's two port fields instead of a ship's arrival port.

- **Error:** `ErrUnknownPort` — "port is not registered"
- **Enforced in:** `ContainerAggregate.Register()` — checked after BR-016 (format) and before BR-015 (duplicate registration). The application layer (`ContainerHandler.RegisterContainer`) resolves `originKnown`/`destKnown` via `domain.PortRepository.Exists()`.
- **Test:** `Container Domain Rules / BR-018`

---

### BR-019 — A ship's on-board container count must not exceed its capacity (PROPOSED — not yet implemented)
Every ship has a maximum container capacity, a fixed number set at registration (a ship's first `Arrive`, mirroring how `ShipName` is set-once at first arrival). Loading a container onto a ship whose current on-ship container count already equals its capacity is rejected — the ship must be under capacity, not merely docked (BR-012) and at the container's terminal port (BR-014).

- **Error:** `ErrCapacityExceeded` — "ship is at container capacity"
- **Enforced in:** `ContainerAggregate.Load()` — checked alongside the existing BR-012/BR-010/BR-014/BR-008 checks; requires the ship's current on-ship container count, resolved by the application layer before the domain check (mechanism — event-replay count vs. Shape A/B read-model query — to be decided during implementation; see Phase 20 of `Main-POC-Plan.md`)
- **Test:** `Container Domain Rules / BR-019` (not yet written — pending implementation)

**Frontend:** `frontend/seafreight-app` ("SeaFreight Flow") gains a load-capacity indicator column (e.g. `12 / 50`, colored by how full the ship is) in both `FleetPanel.vue` and `ShipsAtPortPanel.vue`, pairing the new `capacity` field with the container count already computed via `store.manifestFor(shipID).length`.

---

### BR-020 — A shipID and context must be a valid subject/KV-bucket token
`context` is threaded directly into a NATS subject (`evt.{context}.shipping.ship.{id}.{event}`) and a KV bucket name (`dict-a-{context}`, …). `shipID` (BR-021/BR-022: the mutable natural key, not the aggregate's surrogate `id`) is a KV-key component (`ship.{shipID}`) rather than a subject token, but is held to the same charset for consistency and because it's also carried in the event payload. Both must be non-empty and match `^[A-Za-z0-9_-]+$` — NATS's recommended safe subject-token charset. A dot would silently corrupt a KV key or split a subject across tokens; `*`/`>`/whitespace are NATS wildcard/subject metacharacters.

- **Error:** `ErrInvalidToken` — "value must be a non-empty token of letters, digits, '-' or '_'"
- **Enforced in:** `ShipHandler.hydrateByNaturalKey()` (ShipID, on every Arrive/Depart/Register/CorrectShipID) and `ArrivePort`/`DepartPort`/`RegisterContainer`/`LoadContainer`/`UnloadContainer`/`RegisterShip`/`CorrectShipID` (Context)
- **Test:** `Domain Rules / BR-020`

---

### BR-021 — A shipID can only be registered once
Re-registering an existing shipID would silently reset its state. Mirrors BR-015's container dedup, but on the natural key (a ship's surrogate `id` isn't known to the caller yet at registration time).

- **Error:** `ErrShipExists` — "ship is already registered"
- **Enforced in:** `ShipAggregate.Register()` — uniqueness is resolved against the authoritative event log (folding every ship's current state in the context, then matching by current `shipID`), not an eventually-consistent read projection, same convention as BR-015. `ArrivePort` calls this implicitly on a ship's first arrival; `RegisterShip` exposes it explicitly (optional pre-registration).
- **Test:** `Domain Rules / BR-021`

---

### BR-022 — A shipID can be corrected to any other valid, currently-unused shipID within the same context
`CorrectShipID` renames a registered ship's natural key (call-sign / internal fleet code) — e.g. after a re-flagging or a data-entry fix — without affecting its surrogate identity. `newShipID` must satisfy BR-020 and must not currently be in use by another registered ship in the same context. The ship being corrected must already be registered.

- **Error:** `ErrShipIDInUse` — "shipID is already in use by another ship"; `ErrNotFound` if the source shipID isn't registered; `ErrInvalidToken` (BR-020) for a malformed `newShipID`.
- **Enforced in:** `ShipHandler.CorrectShipID()` — resolves both the source and any target-name collision via the same authoritative-replay resolution as BR-021, then calls `ShipAggregate.CorrectShipID()`.
- **Test:** `Domain Rules / BR-022`
- **Known limitation (verified live):** a container's `onShipID` snapshots the ship's natural key at load time and is not updated by a later correction. Renaming a ship while it carries a container leaves that container stuck — unload fails with **both** the new name (BR-013, `onShipID` still holds the old name) **and** the old name (BR-012, `hydrateByNaturalKey`/`hydratePair` resolve a ship by its *current* name, so a stale name no longer matches any ship and looks unregistered/undocked). The only way to unblock it is to `CorrectShipID` back to the exact pre-correction name, unload, then correct forward again if still wanted. Documented, not fixed, in this pass.

---

### BR-023 — Every `api.*` endpoint is a second transport onto an existing REST-backed method, scoped by subject, never by request body

> ⚠️ **Rule text is the Phase 16b target state; the code still uses the
> pre-16b names.** As shipped in Phase 15a the adapter is
> `dictionary/internal/natsrpc/` serving `rpc.*` subjects with
> `obs.rpc.*` observability. Phase 16b renames it to
> `dictionary/internal/browserrpc/` serving `api.*` / `obs.api.*`, because
> every caller of these subjects is the browser, and `rpc.*` is reserved for
> service-to-service traffic (`ARCHITECTURE-COMMUNICATIONS.md` § 2.4). The
> *behaviour* described below is unchanged by that rename and is already
> enforced and tested. Remove this note when 16b lands.

Mirrors BR-D25 (`BUSINESS_RULES-REFDATA.md`): `dictionary/internal/browserrpc/adapter.go`'s handlers call the exact same `commands.*Handler`/`queries.*` methods `dictionary/internal/rest/handlers.go` calls, so an operation behaves identically regardless of which transport reaches it. The `{context}` token in the subject (`api.{context}.shipping.{entity}.{action}.v1`) — the company / business-unit scope, the same axis as `evt.{context}...` and KV bucket names, and **not** the NATS tenant/account nor the region — is the *only* source of truth for which context a request is scoped to. Every handler overwrites the decoded request body's own `context` field (if any) with the subject-derived value before calling into the application layer, so a client cannot spoof or bypass context scoping via the body. Tenant isolation itself is enforced entirely by the NATS account boundary a connection authenticates into (Phase 13a/13b), not by anything in this subject pattern — see `auth-service/auth/token.go`'s `MintBrowserToken` doc comment and `ARCHITECTURE-COMMUNICATIONS.md` § 2.3 for the full reasoning.

This is the **frontend-to-service** family. `rpc.*` is reserved for service-to-service calls and is a separate family with its own adapter and its own permission grant: a browser credential is never granted `rpc.>`, and backend code never calls `api.>` (`ARCHITECTURE-COMMUNICATIONS.md` § 2.4). An operation may be registered on both when both caller types genuinely need it, but each registration is independent.

- **Enforced in:** `dictionary/internal/browserrpc/adapter.go` — `contextFromSubject()` plus every `handle*` method
- **Test:** `Browser RPC Adapter (Phase 15a/16b) / BR parity` (`dictionary/browserrpc_test.go`)

### BR-024 — Ship, container, and meta projections fire a best-effort `notify.*` event after every KV write; ports fire one from the rpc.* adapter itself
After the Shape A ship projector, the container projector, or the meta projector successfully writes its KV bucket, it fire-and-forget publishes `notify.{context}.shipping.{entity}.changed` (entity: `ship`/`container`/`meta`) carrying the full updated entity (or, for meta, the full known-containers array — a bare JSON array, not a `{"values": [...]}` envelope) as JSON payload — letting a browser connected directly to NATS (Phase 15d) react without KV watch or SSE. Shape B does **not** also publish: the browser doesn't distinguish shapes, so a second notify per event would be a duplicate, not new information. Ports have no event-sourced projector to hang this off (`commands.PortHandler` writes straight to Postgres and is a single instance shared by every tenant), so `natsrpc.Adapter.handlePortRegister` publishes `notify.{context}.shipping.port.changed` itself, on its own tenant connection, after a successful registration — also a bare array, matching meta's convention rather than the `{"values": [...]}` envelope `rpc.*.shipping.port.list.v1`'s request/reply uses, so a subscriber never needs to know which entity's REPLY shape to unwrap.

This is plain core NATS pub/sub — deliberately **no** JetStream retention (unlike `obs.rpc.*`/RPCTRACE, BR-D29): a notification missed during a brief browser disconnect is covered by a bootstrap `rpc.*.shipping.{entity}.list.v1` call on reconnect, so no replay mechanism is needed. A publish failure is logged, never returned — `notify.*` is a best-effort reactive-UI convenience, not a correctness requirement the projector's own success depends on.

- **Enforced in:** `eventhandler.publishNotify()` (called from `RegisterShapeA`/`RegisterContainers`/`RegisterMeta`), `natsrpc.Adapter.publishPortsChanged()`
- **Test:** `notify.* publishes (Phase 15b)` (`dictionary/notify_test.go`)

---

## Guards (not numbered rules)

- **Unregistered container** — load/unload of a container with no `.registered`
  event returns `ErrContainerNotFound` (entity existence, mapped to HTTP 404).
- **Input validation** — required-field checks (`containerID is required`,
  `shipID is required`, …) live in the application layer
  (`commands/container.go`), fire before the aggregate is consulted, and are
  deliberately **not** domain rules — same classification as the retired BR-007.

---

## AIS Navigational Status

Ships carry an AIS-aligned status derived from their current state. This is a read-model concern (set in `ShipAggregate.State()`) and not a domain rule, but it is documented here for reference.

| Status constant | JSON value | Condition | UI colour |
|---|---|---|---|
| `StatusDocked` | `"docked"` | `CurrentPort != ""` | Green |
| `StatusInTransit` | `"in-transit"` | `CurrentPort == ""` | Blue |
| `StatusAtAnchor` | `"at-anchor"` | _(future domain event)_ | Amber |
| `StatusNotUnderCommand` | `"not-under-command"` | _(future domain event)_ | Red |
| `StatusRestrictedManoeuvrability` | `"restricted-manoeuvrability"` | _(future domain event)_ | Orange |

## Container Status

| Status constant | JSON value | Condition |
|---|---|---|
| `ContainerInTerminal` | `"in-terminal"` | `terminalPort` set, `onShipID` nil |
| `ContainerOnShip` | `"on-ship"` | `onShipID` set, `terminalPort` nil |
