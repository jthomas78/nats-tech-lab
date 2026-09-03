---
name: refdata-database-per-service
description: All six owned services share ONE Postgres instance (lb-postgres, port 5432) since Phase 53 / ADR-052, each with its own database + role; the 2026-07-27 one-instance-per-service layout is retired
metadata:
  type: project
---

**Decision (2026-09-03, Phase 53, ADR-052):** one `postgres` container for the six owned
services (`shipping`, `refdata`, `accounts`, `mfe-registry`, `pricing`, `organizations`).
Isolation is **database-per-service + role-per-service**: `demos/01-dictionary/postgres/init.sql`
creates one role and one database per service and revokes `CONNECT` from everyone but the owner.
Temporal keeps its own `temporal-postgres`. Host ports 5433–5437 are gone; everything is on
`localhost:5432`, selected by database + role (password = role name). Organizations' legacy
`trading_partner` database/role became `organizations` in the same change.

**History:** 2026-07-27 moved refdata to its own Postgres *instance* (then accounts, pricing,
organizations, mfe-registry copied that) because refdata and shipping had shared one credential.
The Phase 53 survey found the docs' "own instance" wording was isolation-as-demonstration —
tenancy was never in Postgres (tenant = NATS account, [[phase16_tenancy_taxonomy]]), so a
per-service role gives the same guarantee. See [[tenant_service_separation_decision]].

**How to apply:** new service → add a role + database + REVOKE/GRANT block to `init.sql`, point
`DATABASE_URL` at `postgres:5432/<db>`, `depends_on: postgres`; never a new Postgres container.
Adding to `init.sql` needs `docker compose down -v` (it runs only on an empty volume). Integration
tests are unaffected (own containers or `*_TEST_DATABASE_URL`). ADR:
`obsidian/V3-Platform/Architecture/Dictionary-POC/ADR-052-one-postgres-instance-database-per-service.md`.
