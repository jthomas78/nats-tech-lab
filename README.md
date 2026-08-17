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
| Demo UI     | http://localhost:7100  |
| Backend API | http://localhost:7200 |
| NATS        | nats://localhost:4222 (monitor: http://localhost:8222) |
| Postgres    | localhost:5432 (`dict`/`dict`, db `dictionary`)          |

NATS and Postgres both use their standard host ports (4222/8222 and 5432) —
if you already have a Postgres or NATS server running locally on those
ports, stop it first or expect a port conflict when bringing the stack up.
Inside the compose network the services use the standard ports too.

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
npm run dev          # → http://localhost:7100
```

## Demo 01 — Dictionary POC

Serves context-scoped reference data with NATS KV as a cache in front of a
canonical Postgres projection: a cache miss falls through to Postgres and
backfills the `ships` KV bucket. (Two other shapes — KV as the read model,
and event-sourced reconstruction — were built side by side and retired in
Phase 31 once the comparison was decided; see
`obsidian/POC-Dictionaries/` for the findings.)

See `demos/01-dictionary/README.md` for the full intro (also rendered inside
the lab shell).
