# Business Rules — Shipping Domain

Domain rules enforced in `dictionary/internal/domain/ship.go` before any event is published to JetStream. A rule violation returns an error to the caller; no event is written.

All rules must have a corresponding test in `dictionary/integration_test.go`.

---

## Ship Rules

### BR-001 — Cannot arrive at a port already docked at
A ship that is currently docked at port X cannot arrive at port X again.

- **Error:** `ErrAlreadyDocked` — "ship is already docked at this port"
- **Enforced in:** `ShipAggregate.Arrive()`
- **Test:** `TestDomainRules/BR-001`

---

### BR-002 — Must depart before arriving at a new port
A ship that is currently docked at port X cannot arrive at port Y without first departing port X.

- **Error:** `ErrMustDepart` — "ship must depart current port first (X)"
- **Enforced in:** `ShipAggregate.Arrive()`
- **Test:** `TestDomainRules/BR-002`

---

### BR-003 — Cannot depart a port the ship is not at
A ship can only depart the port it is currently docked at. Attempting to depart a different port, or departing while already at sea, is rejected.

- **Error:** `ErrNotDocked` — "ship is not docked at this port (currently: X)"
- **Enforced in:** `ShipAggregate.Depart()`
- **Test:** `TestDomainRules/BR-003`

---

### BR-004 — Cannot load cargo unless docked
A ship must be docked in port before cargo can be loaded.

- **Error:** `ErrNotInPort` — "ship must be docked to load or unload cargo"
- **Enforced in:** `ShipAggregate.LoadCargo()`
- **Test:** `TestDomainRules/BR-004`

---

### BR-005 — Cannot unload cargo unless docked
A ship must be docked in port before cargo can be unloaded.

- **Error:** `ErrNotInPort` — "ship must be docked to load or unload cargo"
- **Enforced in:** `ShipAggregate.UnloadCargo()`
- **Test:** `TestDomainRules/BR-005`

---

### BR-006 — Cannot unload cargo not in the manifest
Attempting to unload a cargo item whose description does not exist in the ship's current manifest is rejected. Matching is by description string.

- **Error:** `ErrCargoNotFound` — "cargo item not found in manifest: "X""
- **Enforced in:** `ShipAggregate.UnloadCargo()`
- **Test:** `TestDomainRules/BR-006`

---

### BR-007 — Cargo payload is required for load and unload commands
The `cargo` field must be present (non-nil) in the command input for both load and unload operations. This guard sits in the application layer (`commands.go`) and fires before domain rules are evaluated.

- **Error:** `"cargo is required"` (plain error, not a typed domain error)
- **Enforced in:** `ShipHandler.LoadCargo()`, `ShipHandler.UnloadCargo()`
- **Test:** `TestDomainRules/BR-007`

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
