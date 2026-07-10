---
name: nats-volume-legacy-messages
description: Old NATS volumes carry stale-domain messages (e.g. legacy DICTIONARY.entry.* under the old stream name) that loop-crash newer projectors on the current SHIPPING stream
metadata:
  type: project
---

When running Docker after a domain change (originally seen Phase 5 → Phase 6; the same class of bug applies to any subject/stream rename, including the Phase 8 `DICTIONARY` → `SHIPPING` stream rename), the `nats-data` volume still contains messages from the previous domain with subjects like `DICTIONARY.entry.created`. A durable consumer with a broad `FilterSubject` (e.g. `SHIPPING.>` today, was `DICTIONARY.>`) delivers them. Go's JSON unmarshal succeeds (lenient — unmatched fields become zero values), producing e.g. a `ShipEvent` with empty `ShipID`. The projector then tries `kv.Put(..., "ship.", ...)` → `"nats: invalid key"` → Nak → infinite redeliver loop, flooding the log.

**Code fix (applied):** In `eventhandler/handler.go`, after unmarshal, check `event.ShipID == ""` and Ack+skip with a Warn log. This stops the loop on hot-restart without clearing volumes.

**Clean fix for users:** `docker compose down -v` from `demos/01-dictionary/` removes `nats-data` and `pg-data` volumes and starts fresh. Required when upgrading between incompatible domain versions.

**How to apply:** When seeing `"projection failed, will redeliver"` with `"nats: invalid key"` in Docker logs, the NATS volume has stale data from a previous domain. Suggest `docker compose down -v`.
