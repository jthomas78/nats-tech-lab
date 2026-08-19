# EventSourcing and CQRS

Dictionary/reference data (dropdown options, enums, locale config, tenant
config, CQRS read-model lookups) needs to be derived from an event source,
scoped to an application context (the company / business-unit scope — the
tenant is the NATS account and the region a separate regional deployment;
neither appears in a context value, see `ARCHITECTURE-COMMUNICATIONS.md`
§ 2.3), with locale resolved at read time, and served with low latency. This
demo originally compared three shapes for doing that with NATS side by side;
Phase 31 retired two of them once the comparison was decided (see
"Why this shape won" below) — the demo now runs on the winner.

## NATS KV as a cache in front of Postgres

The canonical CQRS projection lives in **Postgres** (source of truth for
governed data). The same events update the Postgres row first, then refresh
a KV cache bucket (`ships`, keyed `{context}.ship.{shipID}`). Reads check KV
first; a **cache miss falls through to Postgres and backfills KV**. The demo
UI has an "evict" action so you can watch the miss → Postgres → backfill
path happen.

### Why this shape won

Two other shapes were built side by side and retired once the comparison
was decided: **KV as the read model** (events projected directly into KV,
no Postgres table at all — fast, but a cache miss meant a permanently
missing row, not a fallback) and **event-sourced reconstruction** (current
state rebuilt by replaying the full JetStream log on every read — correct
with no persistent read model, but latency grew with stream depth). The
KV-cache-in-front-of-Postgres shape above won because it gets the KV shapes'
read latency on a hit without their availability gap, and never pays the
full-replay cost the event-sourced shape did. See
`obsidian/POC-Dictionaries/` for the full findings write-up and
`Main-POC-Plan-ARCHIVE.md` for the retired shapes' original design detail.

## Two aggregates, one stream (Phase 8)

The domain has two aggregates — **Ship** (arrive/depart) and **Container**
(register/load/unload, ISO 6346 IDs) — co-located on the single `SHIPPING`
stream, partitioned by subject. Cross-aggregate rules (a ship must be docked
at the container's terminal to load it, a container can only be unloaded at
its destination, …) are enforced from **one atomic replay** that hydrates both
aggregates. See [BUSINESS_RULES.md](BUSINESS_RULES.md) for BR-001 … BR-015.

## Three frontends

| App | URL | Role |
|---|---|---|
| Admin / NATS debug | http://localhost:7100 | Raw stream feed, KV buckets, CQRS shape panel |
| Port Management | http://localhost:7101 | One port at a time: terminal yard, docked ships + manifests, container operations |
| Tech Lab Operator | http://localhost:7102 | Reference-data admin: type navigator, item grid, localization/reference editor, locales panel, cache status widget (Phase 11) |

## Docs site (Phase 37)

A browsable VitePress docs site lives at `docs/`, covering architecture
content (CQRS shapes, dictionary, communications, accounts, admin,
platform) as a real site rather than raw markdown. Two ways to run it:

- **Local dev** — `npm install && npm run dev` from `docs/` for
  http://localhost:7106 with hot reload.
- **`docker compose up`** — the `docs-frontend` service builds the static
  site and serves it via nginx, same as the other three frontends, also
  on http://localhost:7106.

## Dictionary as a Service (Phase 11)

A **separate service** (`backend/refdata-service/`, its own Postgres schema and container) providing
shared reference/master data — currencies, countries, Incoterms, units of measure, hazard
classes — with localization (BR-D03 fallback chain), typed cross-references (BR-D05), and a
versioned NATS-KV cache protocol (BR-D04, Q5). Nothing here is event-sourced: it's plain Postgres
CRUD, since no consumer ever needs to replay a lookup value's history. See
[Dictionary-Service-Plan.md](../../.claude/plans/Dictionary-Service-Plan.md) and
[ARCHITECTURE.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md)'s
"Reference Data Service" section for the full design.

The shipping backend demonstrates consuming it: `GET /api/refdata-demo/{context}/{type}/{code}`
reads the `refdata-{context}` KV cache directly, falling through to refdata-service's REST API on
a miss or a stale (version-mismatched) entry — the concrete example is the **hazard-class** type.

## Admin UI layout — data flow top to bottom

The demo screen maps vertically to the pipeline:

1. **Shipping Operations** — dispatch a command (Arrive / Depart / Register / Load / Unload container); the backend validates domain rules, publishes to JetStream, and returns immediately.
2. **JetStream panel** — live feed of raw `evt.*.shipping.>` messages as they arrive on `SHIPPING`: subject, sequence number, timestamp, payload. Click a row to expand the full payload.
3. **KV Watch Stream** — every KV change event from the `ships` bucket. Filter by operation (PUT / DEL / PURGE) or key text to isolate the event you're interested in.

## What to watch

- The panel updates reactively: KV watch → notify.* → Pinia store. The Pinia stores in the browser are the same idea as server-side projections — read models derived from an event stream, one layer further out.
- The stream uses **LimitsPolicy** retention, so events are kept after acknowledgement: wipe the KV bucket and the projector can rebuild it from replay.
- Every key is context-scoped. Switch context in the topbar to see the isolated bucket contents.

## Run it

```bash
cd demos/01-dictionary
docker compose up --build    # builds the Go backend + both Vue frontends, then starts all services
```

Then open **http://localhost:7100** for the Admin / NATS debug UI,
**http://localhost:7101** for Port Management, or **http://localhost:7102** for Tech Lab Operator.

```bash
docker compose down          # stop and remove containers
docker compose down -v       # also drop NATS and Postgres data volumes
```

**Optional — Jaeger (Phase 28g):** a `docker compose up` above never starts
Jaeger or `otlp-bridge`; both sit behind the `otlp` profile so toggling them
costs no code (see `ARCHITECTURE-COMMUNICATIONS.md` § 6). Bring them up
alongside the rest of the stack with:

```bash
docker compose --profile otlp up -d jaeger otlp-bridge
```

Then open **http://localhost:16686** and search by service (`shipping`,
`refdata`, `accounts`, `pricing`, `trading-partner`) to see the same spans
the Admin UI's `[traces]` panel shows, re-exported to Jaeger. `otlp-bridge`
live-tails `obs.trace.>` by default (durable consumer, resumes across
restarts); set `OTLP_BRIDGE_REPLAY=true` on the `otlp-bridge` service to
re-export the whole retained hour on the next start instead.

| Service              | Host address                                                 |
| -------------------- | ------------------------------------------------------------ |
| Lab shell            | http://localhost:5170                                        |
| Admin UI              | http://localhost:7100                                        |
| Port Management       | http://localhost:7101                                        |
| Tech Lab Operator     | http://localhost:7102                                        |
| NATS UI (under review)| http://localhost:7103                                        |
| NUI (under review)    | http://localhost:7104                                        |
| NATS Tower (under review) | http://localhost:7105                                    |
| Docs (VitePress)      | http://localhost:7106                                        |
| Swagger UI (backend)  | http://localhost:7200/swagger/                              |
| Backend API           | http://localhost:7200                                       |
| Swagger UI (refdata)  | http://localhost:7201/swagger/                              |
| refdata-service API   | http://localhost:7201                                       |
| accounts-service API  | http://localhost:7202                                       |
| pricing-service API   | http://localhost:7203                                       |
| trading-partner-service API | http://localhost:7204                                |
| NATS client           | nats://localhost:4222                                       |
| NATS monitor          | http://localhost:8222                                       |
| NATS WebSocket        | ws://localhost:9222                                          |
| Postgres (shipping-service) | localhost:5432                                         |
| Postgres (refdata-service)  | localhost:5433                                         |
| Postgres (accounts-service) | localhost:5434                                         |
| Postgres (pricing-service)  | localhost:5435                                         |
| Postgres (trading-partner-service) | localhost:5436                                  |
| Jaeger UI (opt-in, `--profile otlp`) | http://localhost:16686                           |
| Jaeger OTLP/HTTP receiver (opt-in)   | http://localhost:4318                            |

**Postgres credentials (shipping-service):** host `localhost`, port `5432`, user `dict`, password `dict`, database `dictionary`

**Postgres credentials (refdata-service):** host `localhost`, port `5433`, user `refdata`, password `refdata`, database `refdata` — its own instance, not a schema on the one above (see `backend/refdata-service/README.md`).

**Postgres credentials (accounts-service):** host `localhost`, port `5434`, user `accounts`, password `accounts`, database `accounts` — its own instance. Browser NATS credential minting (Phase 15c, folded into this service as its `auth` package in Phase 19 — see `backend/accounts-service/auth/`) reads the same instance in-process, no longer a separate service.

**Postgres credentials (pricing-service):** host `localhost`, port `5435`, user `pricing`, password `pricing`, database `pricing` — its own instance (Phase 25).

**Postgres credentials (trading-partner-service):** host `localhost`, port `5436`, user `trading_partner`, password `trading_partner`, database `trading_partner` — its own instance (Phase 26).

## Dev mode (outside Docker)

Useful for backend hot-reload, or for Vue DevTools (the Docker build serves a
production bundle, which DevTools can't inspect). Requires four terminals.

**1. NATS + Postgres only** (still via Docker — no need to run these natively). `postgres` backs
`shipping-service`; `refdata-postgres` is refdata-service's own separate instance (add it too if
you're also running refdata-service in step 5):

```bash
cd demos/01-dictionary
docker compose up nats postgres refdata-postgres
```

**2. Backend** — the code defaults to the *standard* ports (`localhost:4222`,
`localhost:5432`), which now match what Docker publishes, so these env vars
are just shown explicitly for clarity — omitting them works too:

```bash
cd demos/01-dictionary/backend/shipping-service
NATS_URL=nats://localhost:4222 \
DATABASE_URL="postgres://dict:dict@localhost:5432/dictionary?sslmode=disable" \
go run ./cmd/main.go
```

The backend now listens on `:8080` (not `7200` — that remap only applies to
the Dockerized backend service).

**3. Admin frontend:**

```bash
cd demos/01-dictionary/frontend/admin
npm install   # first time only
npm run dev   # http://localhost:7100, proxies /api to localhost:8080 (see vite.config.js)
```

**4. Port Management frontend** (optional, separate terminal):

```bash
cd demos/01-dictionary/frontend/seafreight-app
npm install   # first time only
npm run dev   # http://localhost:7101, proxies /api to localhost:8080
```

Both `vite.config.js` files proxy `/api` to `http://localhost:8080` — the
plain backend port, since nothing is remapping it outside Docker.

**5. refdata-service** (separate service, separate terminal). Its code also defaults to `:8080`,
which collides if the backend from step 2 is already running locally — so run it on `:8081`
instead when both are up at once:

```bash
cd demos/01-dictionary/backend/refdata-service
NATS_URL=nats://localhost:4222 \
DATABASE_URL="postgres://refdata:refdata@localhost:5433/refdata?sslmode=disable" \
HTTP_ADDR=:8081 \
go run ./cmd/main.go
```

**6. Tech Lab Operator frontend** (optional, separate terminal):

```bash
cd demos/01-dictionary/frontend/refdata
npm install   # first time only
npm run dev   # http://localhost:7102, proxies /api to localhost:8081 (refdata-service, see vite.config.js)
```

## Run the tests

From `demos/01-dictionary/backend/shipping-service/`:

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

From `demos/01-dictionary/frontend/seafreight-app/`:

```bash
# Run the Port Management frontend test suite once
npm run test

# Watch mode — re-runs affected tests on every file save
npm run test:watch
```

All business rules must have a passing test. See [BUSINESS_RULES.md](BUSINESS_RULES.md) for the full rule inventory.

NATS uses its standard host ports (4222, 8222). Other host ports (Postgres,
backend APIs) are non-default to avoid clashing with services already
running on your machine. Inside the compose network the services use the
standard ports.

---

For a deep dive into how each shape is implemented, see
[ARCHITECTURE.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md)
(moved to the obsidian vault; see CLAUDE.md's Obsidian Vault section).

For how services talk to each other (REST/Swagger, NATS `rpc.*` request/reply,
subject taxonomy), see
[ARCHITECTURE-COMMUNICATIONS.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md).
