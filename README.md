# nats-tech-lab

A lab for evaluating NATS.io patterns for a V3 greenfield logistics platform.
Each demo is self-contained: pick it from the lab shell, read the intro,
launch it with Docker, tear it down when done.

The core question under investigation: **what is the correct responsibility
split between JetStream (event backbone), NATS KV (fast lookup/watch/cache),
Postgres (transactional source of truth), and CQRS projections?**

## Layout

```
lab-shell/              Vue 3 + PrimeVue — demo menu + intro pages
shared/unifi-theme/     Shared UniFi-style theme preset (used by all frontends)
demos/
  01-dictionary/        Dictionary POC: KV-as-read-model vs KV-as-cache-over-Postgres
    backend/
      shipping-service/ Go service (hexagonal layout)
    frontend/
      admin/            Vue 3 demo UI
    docker-compose.yml  NATS + Postgres + shipping-service + admin
```

## Prerequisites

- **Docker** (with the compose plugin) — runs the demos
- **Node.js 20+** — runs the lab shell
- **Go 1.26+** — only needed to develop/test the backend outside Docker

## Launching

### 1. Start the lab shell (demo menu)

```bash
cd lab-shell
npm install
npm run dev          # → http://localhost:5170
```

Browse the demo list, read the intro for a demo, then launch it.

### 2. Start a demo (example: 01-dictionary)

Each demo runs its own isolated Docker stack:

```bash
cd demos/01-dictionary
docker compose up --build
```

| Service     | URL                    |
| ----------- | ---------------------- |
| Demo UI     | http://localhost:5173  |
| Backend API | http://localhost:18080 |
| NATS        | nats://localhost:14222 (monitor: http://localhost:18222) |
| Postgres    | localhost:15432 (`dict`/`dict`, db `dictionary`)         |

NATS and Postgres are published on non-default host ports (14222/18222/15432)
so the stack doesn't clash with a NATS or Postgres already running on your
machine. Inside the compose network the services use the standard ports.

The "Launch" button in the lab shell opens the demo UI — the Docker stack
must already be running.

### 3. Tear down

```bash
docker compose down      # add -v to also drop NATS/Postgres data volumes
```

## Development without Docker

Backend (needs a local NATS with JetStream and a Postgres, or just run the
tests — they use an embedded in-process NATS server):

```bash
cd demos/01-dictionary/backend/shipping-service
go test ./...        # integration smoke tests, no external services needed
go build ./...
```

Demo frontend in dev mode (proxies `/api` to `localhost:8080`):

```bash
cd demos/01-dictionary/frontend/admin
npm install
npm run dev          # → http://localhost:5173
```

## Demo 01 — Dictionary POC

Two shapes for serving context-scoped reference data, side by side:

- **Shape A** — NATS KV *is* the read model: events project straight into
  `dict-a-{context}`; reads never touch Postgres.
- **Shape B** — KV as cache in front of a canonical Postgres projection:
  cache miss falls through to Postgres and backfills `dict-b-{context}`.

See `demos/01-dictionary/README.md` for the full intro (also rendered inside
the lab shell).
