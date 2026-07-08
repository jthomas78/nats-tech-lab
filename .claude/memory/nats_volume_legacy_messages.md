---
name: nats-volume-legacy-messages
description: Old NATS volumes carry DICTIONARY.entry.* messages that loop-crash the new ship projectors
metadata:
  type: project
---

When running Docker after a domain change (e.g. Phase 5 → Phase 6), the `nats-data` volume still contains messages from the previous domain with subjects like `DICTIONARY.entry.created`. The new durable consumers use `FilterSubject: "DICTIONARY.>"` which delivers them. Go's JSON unmarshal succeeds (lenient — unmatched fields become zero values), producing a `ShipEvent` with empty `ShipID`. The projector then tries `kv.Put(..., "ship.", ...)` → `"nats: invalid key"` → Nak → infinite redeliver loop, flooding the log.

**Code fix (applied):** In `eventhandler/handler.go`, after unmarshal, check `event.ShipID == ""` and Ack+skip with a Warn log. This stops the loop on hot-restart without clearing volumes.

**Clean fix for users:** `docker compose down -v` from `demos/01-dictionary/` removes `nats-data` and `pg-data` volumes and starts fresh. Required when upgrading between incompatible domain versions.

**How to apply:** When seeing `"projection failed, will redeliver"` with `"nats: invalid key"` in Docker logs, the NATS volume has stale data from a previous domain. Suggest `docker compose down -v`.
