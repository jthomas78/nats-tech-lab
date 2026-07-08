---
name: ship-aggregate-pattern
description: Key architecture decisions in the ShipAggregate / Hydrate pattern for Phase 6
metadata:
  type: project
---

**Hydrate lives in the commands package, not domain.** CLAUDE.md requires "domain has no framework deps." JetStream imports are only in the application and adapter layers. `ShipAggregate` in `domain/ship.go` is pure Go (Apply, command methods, State, FromState). The `hydrate()` helper in `commands/commands.go` does the JetStream replay and feeds events into `agg.Apply()`.

**Projector uses read-modify-write, not full replay.** The Shape A/B event handlers in `eventhandler/handler.go` call `currentAgg()` which reads the current `ShipState` from KV, loads it into a `ShipAggregate` via `FromState()`, then applies the single incoming event via `Apply()`. This is O(1) per event. Full JetStream replay on every projection event would be O(n) and wrong for a production-style projector.

**Shape C uses full replay.** `ShapeC.ReconstructFleet()` in `queries/shape_c.go` creates an ordered consumer with `DeliverAllPolicy`, reads to the stream's `lastSeq`, routes each event to a per-ship aggregate map, and returns fleet snapshots. This is the pure event sourcing property Fowler describes.

**Stopping condition for replay.** Both `hydrate()` and `ReconstructFleet()` read `js.Stream(ctx, StreamName).CachedInfo().State.LastSeq` upfront. They stop consuming when `meta.Sequence.Stream >= lastSeq`. This avoids blocking on `msgs.Next()` waiting for future messages.

**Domain errors map to HTTP 422.** `writeCommandError` in `rest/handlers.go` maps the four domain errors (ErrAlreadyDocked, ErrMustDepart, ErrNotDocked, ErrNotInPort) to 422 Unprocessable Entity so the frontend can show them as inline validation errors rather than generic 400s.
