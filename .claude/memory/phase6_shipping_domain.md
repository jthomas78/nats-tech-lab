---
name: phase6-shipping-domain
description: Phase 6 complete — dictionary domain replaced with shipping; three shapes now live
metadata:
  type: project
---

Phase 6 implemented and merged into `poc/dictionary1.6` (2026-07-07). The generic `DictionaryEntry` domain has been fully replaced with a shipping domain (Fowler Ship/Port/Cargo + Petrosyan Go structural pattern).

**What changed:**
- `domain/events.go` — four new subjects: `DICTIONARY.ship.arrived`, `DICTIONARY.ship.departed`, `DICTIONARY.cargo.loaded`, `DICTIONARY.cargo.unloaded`. Stream name stays `DICTIONARY`.
- `domain/ship.go` — `ShipState` (KV/Postgres projection), `ShipAggregate` with four rule-enforcing command methods; pure domain, no JetStream imports.
- `domain/entry.go` — emptied (DictionaryEntry is gone).
- `application/commands/commands.go` — `ShipHandler`; `hydrate()` replays JetStream up to `lastSeq` before each command (Petrosyan pattern).
- `application/queries/shape_c.go` — `ShapeC.ReconstructFleet`: replays all stream messages, builds fleet state purely from events (no KV/Postgres).
- `eventhandler/handler.go` — read-modify-write from KV (`FromState` + `Apply`) instead of full replay per event.
- `postgres/` — new `ships` table replacing `dictionary_entries`.
- Frontend: `ShippingForm.vue` (replaces `EntryForm.vue`), `ShapeCPanel.vue` (fleet table), `ShapePanel.vue` updated for ship columns, `App.vue` updated.

**How to apply:** All three shapes (A, B, C) are wired and tested. `go test ./...` green with 3 new integration tests: ShapeA projection, ShapeB cache hit/miss, ShapeC fleet reconstruction.

**Why:** Demonstrates pure event sourcing (Shape C) with real domain rules, which the generic dictionary domain couldn't show.
