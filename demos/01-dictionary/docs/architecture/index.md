# Architecture Overview

<EyebrowLabel text="Demo 01 — Dictionary POC" />

The POC evaluates the responsibility split between JetStream (event
backbone), NATS KV (fast lookup/watch/cache), Postgres (transactional
source of truth), and CQRS projections, over a shipping domain with Ship
and Container aggregates.

![System architecture swimlane](/system-architecture-swimlane.png)

::: decision Landed shape
Canonical CQRS projection in Postgres; NATS KV is an eager write-through
cache populated by the same JetStream event handler that upserts
Postgres. A cache miss falls through to Postgres. Two earlier shapes —
KV-as-read-model and full event-sourced reconstruction from JetStream
replay — were built, compared, and retired once this comparison was
decided.
:::

<VerdictBadge status="completed" /> Decided and running in code today.

## Sections

- **CQRS Shapes** — the shape taxonomy this POC compared, and why the
  landed shape won.
- **Dictionary (Reference Data)** — refdata-service: seeding, schema, and
  cross-service consumption.
- **Communications** — the REST/Swagger + NATS `rpc.*` dual-transport
  design and subject taxonomy.
- **Accounts** — the NATS operator-mode trust chain and tenant lifecycle.
- **Admin** — the Admin UI's NATS debugging/observability panels.
- **Platform** — the Tech Lab Operator frontend's nav and feature surface.

Each section below is a landing page for now; deeper content is authored
incrementally as a follow-up to this phase.
