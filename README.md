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
    deploy/
      cell/           One region: NATS, Postgres, the services, the frontends
      global/         The control plane: accounts-service, one per trust domain
      environments/   One env file per cell (local-za-1.env, local-au-1.env)
  02-multi-region/      Cluster mechanics: gateways, account walls, Replicas 1 vs 3
    CLAUDE.md           This folder's own agent rules -- it is a sealed unit
    deploy/             compose.za.yaml (lb-za-1) + compose.au.yaml (lb-au-1),
                        up.sh and contexts.sh
    diagrams/           Topology drawings and the one-account/one-stream options
    docs/               Multi-Region-Plan.md, the record of how it was worked out
    lab/                One script per question
    nats/               Server config and the minted trust chain
    odometer/           The one JetStream + CQRS example (Go)
```

## Prerequisites

- **Docker** (with the compose plugin) — runs the demos
- **Node.js 20+** — runs the lab shell
- **Go 1.27+** — needed to develop/test the backend outside Docker, and to run
  demo 02's odometer (`demos/02-multi-region/lab/03-odometer.sh`)
- **`nats`, `nsc` and `jq`** — demo 02 only, and on the host by choice (there is
  no tool container): `brew install nats-io/nats-tools/nats
  nats-io/nats-tools/nsc jq`

## Launching

### 1. Start the lab shell (demo menu)

```bash
cd lab-shell
npm install
npm run dev          # → http://localhost:5170
```

Browse the demo list, read the intro for a demo, then launch it.

### 2. Start a demo (example: 01-dictionary)

Each demo runs its own isolated Docker stack. Demo 01 is split into a cell
band (one region) and a global band (the control plane) — see ADR-055. One
command starts both:

```bash
cd demos/01-dictionary/deploy/cell
docker compose -p poc --env-file ../environments/local-za-1.env -f compose.yaml -f ../global/compose.control.yaml up -d --build
```

`za-1` holds every base port, so the addresses below are unchanged. A second
cell runs the same files with `-p lb-au-1` and `local-au-1.env`, which shifts
every host port by +50.

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

From the same `demos/01-dictionary/deploy/cell` directory:

```bash
docker compose -p poc --env-file ../environments/local-za-1.env -f compose.yaml -f ../global/compose.control.yaml down
```

Add `-v` to also drop the NATS and Postgres data volumes.

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

Demo 01 is **one region, one NATS server**. It has no cluster, no gateway and
no JetStream domain — all of that lives in demo 02.

## Demo 02 — Multi-Region Cluster Mechanics

Six NATS servers and nothing else. Two clusters of three (`za`, `au`) joined by
a gateway, in two Compose projects (`lb-za-1`, `lb-au-1`) on three networks.
It answers three questions: does a tenant account wall hold across a gateway,
what does `Replicas: 1` cost when a server dies, and — the one that decides the
design — where does a region's data actually live? The third is measured by the
**odometer**: drive 12.5 km once, and with one shared account *both* regions
read 12.5 km out of the **same** bucket, held in cluster `za`. Australia owns
nothing, reads it over the WAN, and loses it if South Africa goes down. One
account per region gives each side a real local stream.

> **Correction, 2026-09-11.** This paragraph used to ask "is the cross-region
> **double capture** real?" and answer "one shared account makes the fleet
> total 50 km". Both were wrong — a projector replay bug, plus adding two
> readings of one bucket together. Behind a gateway one account holds ONE
> stream, so nothing can be stored twice. Details:
> `demos/02-multi-region/diagrams/gateway-double-capture-options-2.html`.

```bash
cd demos/02-multi-region/deploy
./up.sh
```

See `demos/02-multi-region/README.md`.
