---
name: shipping-domain-overview
description: Current shipping domain state (Phase 8) — Ship + Container aggregates on the SHIPPING stream — how it got here from Phase 6, and the architecture decisions that still govern it
metadata:
  type: project
---

**Phase 6** (merged into `poc/dictionary1.6`, 2026-07-07) replaced the generic `DictionaryEntry` domain with a shipping domain (Fowler Ship/Port/Cargo + Petrosyan Go structural pattern): `ShipAggregate`, `hydrate()` replay-per-command, Shape A/B/C all wired. Stream was named `DICTIONARY`; subjects were `DICTIONARY.ship.*` / `DICTIONARY.cargo.*`.

**Phase 8** (introduced on branch `poc/dictionary1.8.2`; the repo has since moved on to later phases, e.g. Phase 11's dictionary-as-a-service work) was a bigger revision on top of Phase 6, not a separate phase to track independently — this memory describes the Ship/Container domain foundation, which later phases have built on rather than replaced:

- Stream renamed `DICTIONARY` → `SHIPPING` (breaking change to every subject).
- `Container` added as its own aggregate (`ContainerAggregate`, `domain/container.go`), co-located with `ShipAggregate` on the single `SHIPPING` stream. The `Cargo` value object on `ShipAggregate` was retired — a ship's manifest is now a client-side/query-side join on `onShipID == shipID`.
- Container lifecycle: `ContainerStatus` has exactly two values, `in-terminal` / `on-ship` (no richer states like "delivered" — see [[container-status-model]]).
- Business rules BR-008 through BR-016 live in `domain/container.go`; BR-001 through BR-003 (ship rules) in `domain/ship.go`. Full list in `demos/01-dictionary/BUSINESS_RULES.md`.
- Second frontend added: `frontend-port/` (Port Management / "SeaFreight Flow" UI, dev port 5174) alongside the original `frontend/` (admin/raw NATS debug view, port 5173). See [[frontend-port-structure]].
- `meta-{context}` KV bucket added for `known-ports` / `known-containers` lookups feeding UI dropdowns.

**Architecture decisions (set in Phase 6, still governing Phase 8):**

- **Hydrate lives in the commands package, not domain.** CLAUDE.md requires "domain has no framework deps." JetStream imports are only in the application and adapter layers. `ShipAggregate` in `domain/ship.go` is pure Go (Apply, command methods, State, FromState). The `hydrate()` helper in `commands/commands.go` does the JetStream replay and feeds events into `agg.Apply()`.
- **Projectors use read-modify-write, not full replay.** The Shape A/B event handlers in `eventhandler/handler.go` call `currentAgg()` which reads the current state from KV, loads it into an aggregate via `FromState()`, then applies the single incoming event via `Apply()`. O(1) per event — full JetStream replay on every projection event would be O(n) and wrong for a production-style projector.
- **Shape C uses full replay.** `ShapeC.ReconstructFleet()` in `queries/shape_c.go` creates an ordered consumer with `DeliverAllPolicy`, reads to the stream's `lastSeq`, routes each event to a per-ship aggregate map, and returns fleet snapshots — the pure event-sourcing property Fowler describes.
- **Stopping condition for replay.** Both `hydrate()` and `ReconstructFleet()` read `js.Stream(ctx, StreamName).CachedInfo().State.LastSeq` upfront and stop consuming when `meta.Sequence.Stream >= lastSeq`. This avoids blocking on `msgs.Next()` waiting for future messages.
- **Domain errors map to HTTP 422.** `writeCommandError` in `rest/handlers.go` maps domain errors (originally the four ship errors — `ErrAlreadyDocked`, `ErrMustDepart`, `ErrNotDocked`, `ErrNotInPort` — now also the container errors added through BR-016, e.g. `ErrInvalidContainerID`) to 422 Unprocessable Entity, so the frontend can show them as inline validation errors rather than generic 400s. Any new domain rule's error should follow this same mapping.

**How to apply:** When asked about "the shipping domain" or "the stream," assume Phase 8 state (SHIPPING stream, two aggregates) unless the user is explicitly asking about pre-Phase-8 history. When adding a new command or projector, follow the hydrate/read-modify-write/422 conventions above rather than re-deriving them.
