# Source of Truth

<EyebrowLabel text="NATS" />

## Which system owns the truth?

Two coherent patterns exist here, and they're mirror images — each bolts
on exactly the component the other gets for free.

::: decision Consistency
Is the durable source of truth the **Postgres event store** (Pattern A)
or the **JetStream stream** (Pattern B)?
:::

**Pattern A — Postgres = truth.** An `events` table with a
`UNIQUE(id, version)` constraint, an outbox written in the same
transaction, a relay that publishes to JetStream, and projections
downstream. NATS is transport only.

**Pattern B — JetStream = truth.** Events append with
`expected-last-seq`; durable consumers project raw events into Postgres
for query/audit, which lags by construction. Postgres is a derived view.

![Source of truth — Pattern A vs Pattern B](/source-of-truth-patterns.png)

| | Pattern A (Postgres) | Pattern B (JetStream) |
| --- | --- | --- |
| Free | SQL-queryable truth | native distribution |
| Bolt-on | outbox | raw-event projection |
| Cross-aggregate txn | yes | saga |
| Global order | natural | per-subject |

The real difference is where transactionally-consistent, queryable
history lives — the source itself (A) or a lagging derivative (B). Only B
actually tests JetStream as an event store; A would relegate NATS to
transport, dodging the question this lab exists to answer.

<VerdictBadge status="completed" /> Pattern B — JetStream is the source
of truth; Postgres and KV are downstream projections, never written by
the command path. This is a locked working assumption; its footguns are
deliberately load-bearing — see [Write-Side Safety](/nats/write-side-safety).

## Cross-aggregate invariants

A rule needing two aggregates' state (e.g. don't load a container whose
destination equals the ship's current port). The difficulty is the
aggregate boundary, not the stream topology.

::: decision Consistency
Are both aggregates on **one stream** (one atomic replay yields both) or
**two** (no atomic cross-stream read)?
:::

![Cross-aggregate invariant fork](/cross-aggregate-fork.png)

| Option | Consistency | Notes |
| --- | --- | --- |
| ① Read-model guard | eventual | cheap; stale-read window |
| ② Hydrate both streams | strong | contexts coupled |
| ③ Reservation store | strong | extra write-side store |
| ④ Saga / compensate | eventual, self-correcting | most moving parts; "textbook" DDD |

**Current code:** one shared stream, `SHIPPING` — `StreamSubjects()`
(`domain/events.go`) binds both the ship and container subject wildcards
onto it, and `container.go`'s own doc comment says it plainly: *"until
Phase 103 both aggregates hydrate from one atomic replay of the SHIPPING
stream."* This is deliberate — one stream keeps invariants strong from a
single replay (Phase 8), which is why the POC started there instead of
with a distributed split.

<VerdictBadge status="proposed" /> Phase 103 (not yet implemented)
proposes splitting `SHIPPING` into a ship-only stream plus a new
container-only `TERMINAL` stream, specifically to surface the
cross-aggregate consistency problem this table describes — then close it
with option ① (the read-model guard) as the default.
