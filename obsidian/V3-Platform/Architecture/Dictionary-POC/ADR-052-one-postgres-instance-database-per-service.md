# ADR-052: One Postgres Instance, One Database and One Role per Service

**Status:** **Accepted 2026-09-03** — implemented and live-verified as Phase 53 (`Main-POC-Plan.md`).
**Date:** 2026-09-03
**Deciders:** Jeremy (repo owner)
**Related:** [ARCHITECTURE-ACCOUNTS.md](ARCHITECTURE-ACCOUNTS.md) (tenancy is the NATS account boundary); [ARCHITECTURE-DICTIONARY.md](ARCHITECTURE-DICTIONARY.md) § "Database Schema"; [ARCHITECTURE.md](ARCHITECTURE.md) § "Reference Data Service"; `.claude/plans/Dictionary-Service-Plan.md` Q1 (the 2026-07-27 database-per-service decision this ADR revises); `.claude/memory/refdata_database_per_service.md`, `.claude/memory/tenant_service_separation_decision.md`; `CLAUDE.md` § "Docker Host Port Allocation"

## Context

`demos/01-dictionary/docker-compose.yml` runs **seven** `postgres:16-alpine`
containers: one each for `shipping-service`, `refdata-service`,
`accounts-service`, `mfe-registry-service`, `pricing-service`,
`organizations-service`, and one for Temporal. Six of them exist because of a
2026-07-27 decision to move from schema-per-service to
**database-per-service**, and every later service copied that shape.

The reason recorded for that decision was isolation-as-demonstration: prove
that two services share no datastore. The concrete gap it closed was narrower —
`refdata-service` and `shipping-service` were using the **same Postgres
credentials**, so a private schema was not enforced by access control. A
separate server was the strongest available fix at the time.

What the code actually does today (surveyed 2026-09-03):

| Fact | Detail |
|---|---|
| Postgres containers | 7 (6 owned services + Temporal) |
| Services already in a named schema | 5 — `refdata`, `accounts`, `registry`, `pricing`, `organizations` |
| Services in `public` | 1 — `shipping-service` (`ships`, `containers`, `ports`) |
| Tenant column in any Postgres table | none. Rows carry `context` (business unit). `accounts-service` rows carry `account`, because the account **is** its subject matter. |
| Per-tenant Postgres connections | none. Each service holds one `*sql.DB`; per-tenant fan-out is NATS only (`shared/natstenants`). |
| Migration mechanism | embedded idempotent `CREATE ... IF NOT EXISTS` Go string slices, run at startup, no tool |
| `search_path` reliance | none — every query is schema-qualified |
| Superuser needs | none. `gen_random_uuid()` is core since PG13. One `CREATE EXTENSION IF NOT EXISTS pgcrypto` in organizations is vestigial after ADR-051 (`pgcrypto` is a trusted extension anyway, so a database owner may install it). |
| Integration tests | own throwaway containers (`docker run postgres:16-alpine`) or `*_TEST_DATABASE_URL`; none use compose |

**Tenancy is not a Postgres concern in this repo.** Phase 16's decision record
is explicit: tenancy = NATS account, hard and server-enforced; `{context}` is
a soft app-layer partition; region is a deployment axis. No tenant ever gets
its own database, schema, or role. Consolidating Postgres *servers* therefore
moves nothing on the tenant-isolation axis. The axis it moves is
**service-to-service** isolation.

The forces:

- Seven Postgres processes on one laptop, each with its own memory floor,
  health check, host port (5432–5437), and volume, for a lab whose point is
  NATS patterns, not database operations.
- The docs promise "no shared datastore" between services. That promise is
  worth keeping in a form that access control enforces.
- The repo's shared-instance vs dedicated-server trade-off matrix (cost, ops
  overhead, migrations favour shared; noisy-neighbour and breach risk favour
  dedicated) is written for *tenants* on a server. Between six lab services
  owned by one team, the dedicated-server advantages are small.

## Decision

Run **one** Postgres instance for the six owned services. Keep Temporal on
its own instance. Seven containers become two.

Inside the shared instance:

1. **One database per service**, named for the service's data:
   `dictionary`, `refdata`, `accounts`, `mfe_registry`, `pricing`,
   `organizations`.
2. **One role per service**, owner of exactly its own database, with
   `CONNECT` on every other database **revoked** (`REVOKE CONNECT ON DATABASE
   x FROM PUBLIC`, then `GRANT CONNECT` to the owner only). A role cannot
   reach another service's tables even by accident.
3. **Roles and databases are created once by an init script** mounted at
   `/docker-entrypoint-initdb.d/`. Services keep running their own
   migrations at startup, unchanged; each service's schema (`refdata`,
   `accounts`, …) still lives inside its own database.
4. **Each service keeps its own `DATABASE_URL`.** Only the host and, for
   five of them, the port change: `postgres:5432/<db>`.
5. **One host port**, `5432`. Ports 5433–5437 are released. Operators reach a
   service's database by database name and role, not by port.
6. **Temporal stays separate.** Its `auto-setup` image wants
   `CREATE DATABASE` at boot and is third-party infrastructure, not one of
   our bounded contexts. Folding it in is a possible later step, not this one.

## Options Considered

### Option 1 — One instance, database-per-service, role-per-service (chosen)

| Dimension | Assessment |
|-----------|------------|
| Complexity | Low — compose + init SQL + six URL strings; no Go migration changes |
| Cost | 7 → 2 containers, 6 → 1 host ports, 6 → 1 data volumes |
| Isolation | Credential-enforced: a role can only `CONNECT` to its own database |
| Team familiarity | High — standard Postgres pattern |

**Pros:** keeps the "own credential, own data" story the docs already tell;
shipping stays in `public` of its own database, so no service code changes
beyond default URL strings; one place to back up, patch, inspect.
**Cons:** one process failure takes six services down (acceptable in a lab
where `docker compose down` already does that); shared CPU/IO between
services (none of them is load-tested here, see Phase 104).

### Option 2 — One instance, one database, schema-per-service with role-per-schema

| Dimension | Assessment |
|-----------|------------|
| Complexity | Medium — shipping must move out of `public`; per-schema `GRANT`/`REVOKE` and default privileges per role |
| Cost | Same container/port saving as Option 1 |
| Isolation | Weaker — shared catalog, shared `public`, one wrong `GRANT` leaks |
| Team familiarity | Medium |

**Pros:** cross-service joins become *possible* (which is exactly the thing
the architecture forbids — this is a con dressed as a pro).
**Cons:** more moving parts for no extra saving over Option 1; breaks the
"own database" wording in three architecture docs and one memory.

### Option 3 — One instance for everything, including Temporal

Same as Option 1 plus Temporal. Needs a `temporal` role with `CREATEDB`,
`DBNAME`/`VISIBILITY_DBNAME` pinned, and a third-party boot script sharing
our server. 7 → 1. Deferred: small extra saving, larger debugging surface.

### Option 0 — Keep seven

Status quo. Rejected: the isolation it buys over Option 1 is process-level,
which nothing in this repo needs, and it costs a container per new service
forever.

## Trade-off Analysis

The only real thing lost is **process-level fault and resource isolation
between services**. Nothing in the lab depends on it: no service is
load-tested, no chaos scenario targets Postgres, and the tenant axis was
never on Postgres. The thing the original decision actually wanted —
**a service cannot read another service's tables** — is preserved, and made
explicit for the first time, by per-role `CONNECT` grants. Before this ADR
the guarantee was "different server"; after it, it is "different role,
enforced by `pg_hba` and `CONNECT` privilege", which is the guarantee a
production database-per-service deployment on a managed instance would use
anyway.

## Consequences

- **Easier:** startup, memory use, one `psql` endpoint, one volume to reset,
  one port row in the README, one health check for all six services to wait
  on.
- **Harder:** nothing in day-to-day use. Anyone reaching a database by its
  old host port must switch to database name + role on `5432`.
- **Data:** this is a `docker compose down -v` + reseed change. Six named
  volumes (`pg-data`, `pg-refdata-data`, `pg-accounts-data`,
  `pg-mfe-registry-data`, `pg-pricing-data`, `pg-trading-partner-data`)
  become one. Existing dev data is not migrated — the repo already treats
  reseed as the standard reset path.
- **Revisit when:** a service needs Postgres tuning the others must not see
  (separate instance again), or the lab adds a load-testing phase (Phase 104)
  that wants per-service IO isolation, or Temporal is folded in (Option 3).
- **Docs to change:** `ARCHITECTURE.md` § refdata/accounts "own Postgres
  instance" wording, `ARCHITECTURE-DICTIONARY.md` § "Database Schema",
  `demos/01-dictionary/README.md` port table and credential prose,
  compose comments, `organizations/internal/postgres/migrate.go` header
  comment, memory `refdata_database_per_service.md`,
  `Dictionary-Service-Plan.md` Q1.

## Action Items

Tracked as **Phase 53** in `.claude/plans/Main-POC-Plan.md`.

1. [x] Init SQL creating six roles and six databases with `CONNECT` revoked from `PUBLIC`.
2. [x] Compose: one `postgres` service, one volume, six `DATABASE_URL` rewrites, six `depends_on` rewrites, drop five services and five volumes.
3. [x] Six `cmd/main.go` fallback defaults → `localhost:5432/<db>`; fix the organizations default (`organization` vs compose's `trading_partner`).
4. [x] Verification: from each service's role, `\c` into another service's database must fail; all six services healthy after `down -v && up --build`.
5. [x] Docs and memory listed under Consequences.
