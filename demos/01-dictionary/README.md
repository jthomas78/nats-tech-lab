# EventSourcing and CQRS

Dictionary/reference data (dropdown options, enums, locale config, tenant
config, CQRS read-model lookups) needs to be derived from an event source,
scoped to an application context (tenant / region / locale), and served with
low latency. This demo compares three shapes for doing that with NATS,
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

## Shape C — Event Sourcing Reconstruction

No KV bucket, no Postgres table. Current fleet state is derived entirely from
replaying the JetStream event log from `seq=1`. Demonstrates the defining
property of pure event sourcing: correct state with no persistent read model.

## UI layout — data flow top to bottom

The demo screen maps vertically to the pipeline:

1. **Shipping Operations** — dispatch a command (Arrive / Depart / Load / Unload); the backend validates domain rules, publishes to JetStream, and returns immediately.
2. **JetStream panel** — live feed of raw `DICTIONARY.*` messages as they arrive on the stream: subject, sequence number, timestamp, payload. Click a row to expand the full payload.
3. **Shape A | Shape B | Shape C** — projections side by side. Shape B also shows the canonical **Postgres projection** below the KV cache rows.
4. **KV Watch Stream** — every KV change event from both buckets. Filter by shape (A / B), operation (PUT / DEL / PURGE), or key text to isolate the event you're interested in.

## What to watch

- Both Shape A and B panels update reactively: KV watch → SSE → Pinia store. The Pinia stores in the browser are the same idea as server-side projections — read models derived from an event stream, one layer further out.
- The stream uses **LimitsPolicy** retention, so events are kept after acknowledgement: wipe a KV bucket and the projector can rebuild it from replay.
- Evict a Shape B cache key, then hit Read — watch the **miss → Postgres → backfill** path: the JetStream panel stays quiet (no new event), but the KV Watch stream shows a new PUT as the cache backfills.
- Hit **Reconstruct** on Shape C — it replays from `seq=1` every time; clear the KV and Postgres data and it still returns correct state from the event log alone.
- Every key is context-scoped. Switch context in the topbar to see each shape's isolated bucket.

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

| Service      | Host address                                                    |
| ------------ | --------------------------------------------------------------- |
| Lab shell    | http://localhost:5170                                           |
| Demo UI      | http://localhost:5173                                           |
| Swagger UI   | http://localhost:18080/swagger/                                 |
| Backend API  | http://localhost:18080                                          |
| NATS client  | nats://localhost:14222                                          |
| NATS monitor | http://localhost:18222                                          |
| Postgres     | localhost:15432                                                 |

**Postgres credentials:** host `localhost`, port `15432`, user `dict`, password `dict`, database `dictionary`

## Run the tests

From `demos/01-dictionary/backend/`:

```bash
# Preferred — runs the suite and prints the spec tree at the end
ginkgo ./...

# Watch mode — re-runs on every file save (useful during development)
ginkgo watch ./...

# No install required fallback
go test ./...
```

Install the `ginkgo` CLI once with:

```bash
go install github.com/onsi/ginkgo/v2/ginkgo@latest
```

All business rules must have a passing test. See [BUSINESS_RULES.md](BUSINESS_RULES.md) for the full rule inventory.

All host ports are non-default to avoid clashing with services already
running on your machine. Inside the compose network the services use the
standard ports (4222, 8222, 5432, 8080).

---

For a deep dive into how each shape is implemented, see [ARCHITECTURE.md](ARCHITECTURE.md).
