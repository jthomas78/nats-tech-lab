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

## UI layout — data flow top to bottom

The demo screen maps vertically to the pipeline:

1. **Create / Update Entry** — dispatch a command; the backend publishes to
   JetStream and returns 202 immediately.
2. **JetStream panel** — live feed of raw `DICTIONARY.*` messages as they
   arrive on the stream: subject, sequence number, timestamp, payload.
   Click a row to expand the full payload.
3. **Shape A | Shape B** — KV projections side by side. Shape B also shows
   the canonical **Postgres projection** in a sub-table below the KV cache
   rows — you can see a Postgres row persist after its KV cache entry is
   evicted.
4. **KV Watch Stream** — every KV change event from both buckets. Filter by
   shape (A / B), operation (PUT / DEL / PURGE), or key text to isolate the
   event you're interested in.

## What to watch

- Both Shape panels update reactively: KV watch → SSE → Pinia store. The
  Pinia stores in the browser are the same idea as server-side projections —
  read models derived from an event stream, one layer further out.
- The stream uses **LimitsPolicy** retention, so events are kept after
  acknowledgement: wipe a KV bucket and the projector can rebuild it from
  replay.
- Evict a Shape B cache key, then hit Read — watch the **miss → Postgres →
  backfill** path: the JetStream panel stays quiet (no new event), but the KV
  Watch stream shows a new PUT as the cache backfills.
- Every key is context-scoped (`dict-a-en-GB`, `dict-b-en-US`, …). Switch
  context in the topbar to see each shape's isolated bucket.
- KV key format is `{entityType}.{id}` (e.g. `currency.GBP`) — NATS KV keys
  only allow `[-/_=.a-zA-Z0-9]`, so `.` is the hierarchy separator.

## Run it

```bash
cd demos/01-dictionary
docker compose up --build    # builds Go backend + Vue frontend, then starts all services
```

Then open **http://localhost:5173**.

```bash
docker compose down          # stop and remove containers
docker compose down -v       # also drop NATS and Postgres data volumes
```

| Service     | Host address                                            |
| ----------- | ------------------------------------------------------- |
| Demo UI     | http://localhost:5173                                   |
| Backend API | http://localhost:18080                                  |
| NATS client | nats://localhost:14222                                  |
| NATS monitor| http://localhost:18222                                  |
| Postgres    | localhost:15432 — user `dict`, password `dict`, db `dictionary` |

All host ports are non-default to avoid clashing with services already
running on your machine. Inside the compose network the services use the
standard ports (4222, 8222, 5432, 8080).
