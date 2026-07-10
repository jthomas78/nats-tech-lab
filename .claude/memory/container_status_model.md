---
name: container-status-model
description: Container has only two statuses (in-terminal / on-ship) — no "delivered" or "for-shipment" state exists
metadata:
  type: project
---

`ContainerStatus` (`demos/01-dictionary/backend/dictionary/internal/domain/container.go`) has exactly two values:

```go
const (
	ContainerInTerminal ContainerStatus = "in-terminal"
	ContainerOnShip     ContainerStatus = "on-ship"
)
```

Location is modelled via two nullable pointer fields (`TerminalPort`, `OnShipID`) — exactly one non-nil at a time — never a third status. There is no "delivered", "for-shipment", "collected", "outbound", or "inbound" status anywhere in the backend.

**"Arrived at destination" is not a separate state.** A container that reached its destination is still just `in-terminal`; the only signal is `TerminalPort == DestPort`. BR-008/BR-009 (`ContainerAggregate.Load`/`Unload`) check this equality directly rather than reading a status field.

**How to apply:** When asked to add UI groupings like "outbound vs inbound" or "delivered," don't look for or add a new domain status — derive the split client-side from `destPort` vs the current/selected port (as done in `frontend-port/src/components/TerminalPanel.vue`'s Outbound/Arrived tables and `FleetPanel.vue`'s status filter). This has been done twice now (TerminalPanel split, Fleet panel filter) and the pattern held both times without any backend change.
