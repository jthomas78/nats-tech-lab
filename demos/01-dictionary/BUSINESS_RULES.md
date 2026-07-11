# Business Rules — Shipping Domain

Domain rules enforced before any event is published to JetStream. A rule
violation returns an error to the caller; no event is written.

Two aggregates share the single `SHIPPING` stream (Phase 8):

- **Ship** rules live in `dictionary/internal/domain/ship.go`
- **Container** rules live in `dictionary/internal/domain/container.go`

Cross-aggregate rules (BR-008, BR-012, BR-014) need both aggregates' state.
Both hydrate from **one atomic replay** of the `SHIPPING` stream
(`commands.hydratePair`), so these checks are strongly consistent. Phase 12
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
