# Demo 01 — Dictionary POC

Dictionary/reference data (dropdown options, enums, locale config, tenant
config, CQRS read-model lookups) needs to be derived from an event source,
scoped to an application context (tenant / region / locale), and served with
low latency. This demo compares two shapes for doing that with NATS,
side by side.

## Shape A — NATS KV as the read model

Events on the `DICTIONARY` JetStream stream are projected **directly into a
context-scoped KV bucket** (`dict-a-{context}`). Reads go straight to KV.
There is no Postgres read table at all — the KV bucket *is* the read model,
and the KV revision number is the entry's version.

## Shape B — NATS KV as a cache in front of Postgres

The canonical CQRS projection lives in **Postgres** (source of truth for
governed data). The same events update the Postgres row first, then refresh
the KV cache bucket (`dict-b-{context}`). Reads check KV first; a **cache
miss falls through to Postgres and backfills KV**. The demo UI has an
"evict" action so you can watch the miss → Postgres → backfill path happen.

## What to watch

- Both panels update reactively: KV watch → SSE → Pinia store. The Pinia
  stores in the browser are the same idea as the server-side projections —
  read models derived from an event stream, one layer further out.
- The stream uses **LimitsPolicy** retention, so events are kept after
  acknowledgement: wipe a KV bucket and the projector can rebuild it by
  replay.
- Every key is context-scoped (`dict-a-en-GB`, `dict-b-en-US`, …). There are
  no global lookups.
- KV key format is `{entityType}.{id}` (e.g. `currency.GBP`) — NATS KV keys
  only allow `[-/_=.a-zA-Z0-9]`, so `.` is the hierarchy separator.

## Run it

```bash
docker compose up --build    # then open http://localhost:5173
docker compose down          # tear down (add -v to drop data)
```

Services: NATS (JetStream, monitor on :8222), Postgres 16, Go backend
(:8080), Vue 3 demo UI (:5173).
