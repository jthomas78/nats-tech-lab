---
name: refdata-database-per-service
description: refdata-service now runs on its own Postgres instance (refdata-postgres, port 5433), not a schema on shipping-service's shared postgres — agreed decision and how it was implemented
metadata:
  type: project
---

**Decision (agreed and implemented 2026-07-27):** `refdata-service` moved from schema-per-service
(private `refdata` schema on the same Postgres instance/database — `dictionary` — as
`shipping-service`) to full **database-per-service**: a separate `refdata-postgres` container in
`docker-compose.yml` (port `5433`, role/database `refdata`/`refdata`). NATS is now the only
infrastructure shared between `shipping-service` and `refdata-service`.

**Why:** followed a design discussion (see [[tenant_service_separation_decision]]) about what
"isolated service" means for refdata-service, since it's a data-plane concern with no direct
service-to-service coupling already (backend-to-backend calls are NATS-only per BR-D28). Researched
the standard microservices database-isolation spectrum (private-tables / schema-per-service /
database-per-service — microservices.io) and found the remaining gap was that both services used
the *same Postgres credentials*, so schema separation wasn't actually enforced by access control.
User chose the strongest tier (option 2: separate DB server) specifically to make refdata-service
provably independent, since demonstrating the service boundary is the point of this being a
separate service at all (`Dictionary-Service-Plan.md` Q1).

**How to apply:** if extending refdata-service's infra or writing new run instructions, use
`refdata-postgres` / port `5433` / user+db `refdata`, not `postgres` / `5432` / `dict`. Files
touched by this change (check these stay in sync on any related edit): `docker-compose.yml`,
`backend/refdata-service/cmd/main.go` (DATABASE_URL fallback default),
`backend/refdata-service/README.md`, `demos/01-dictionary/README.md`,
`.claude/plans/Dictionary-Service-Plan.md` (Q1), and the two obsidian architecture docs
(`ARCHITECTURE.md` § "Reference Data Service", `ARCHITECTURE-DICTIONARY.md` § "Database Schema").
The `corpus_repository_integration_test.go` integration test was unaffected — it already spins up
its own throwaway Postgres container via `docker run`, independent of compose.
