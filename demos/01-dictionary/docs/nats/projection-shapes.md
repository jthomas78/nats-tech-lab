# Projection Shapes

<EyebrowLabel text="NATS" />

The POC built all three read-model shapes side-by-side over the same
Ship/Container domain so the trade-off would be felt, not argued.

::: decision Projection
Read from a **KV projection**, a **cache in front of Postgres**, or
**reconstruct from the log** with no stored model at all?
:::

**Shape A — KV only.** `event → projector → KV`; a read is a KV lookup.
No Postgres. Cheapest reads; KV *is* the read model.

**Shape B — KV + Postgres.** `event → Postgres (canonical)`, then a
write-through KV update; a read hits KV first, falls through to Postgres
on a miss, and warms KV on the way back.

**Shape C — replay.** A read replays the aggregate's stream from
sequence 1 and folds it. No stored model at all — the defining
event-sourcing property: state from history alone.

![Projection shapes A, B, and C](/projection-shapes.png)

| Shape | Store | Cost at scale |
| --- | --- | --- |
| A | KV bucket | cheap reads |
| B | Postgres + KV | cheap + a durable fallback |
| C | none | grows with stream depth — needs snapshots |

**KV has two roles here — don't confuse them.** A write-side snapshot is
safe to trust for hard rules, *if* it's replayed forward to the latest
sequence first. A read-model projection is eventually consistent by
construction — fine for soft checks and display, never a hard-rule
validation source. See
[JetStream + KV Mechanics](/nats/jetstream-kv-design) for more on this
split.

<VerdictBadge status="completed" /> All three were built and compared
(Phases 1 / 2 / 6): A and B showed event-driven CQRS into persistent
projections; C proved reconstruction works — clear every store, hit the
endpoint, and state still comes back from the log alone.

## What's running today

Phase 31 retired Shapes A and C once the comparison was decided — their
query code, REST routes, Swagger docs, and performance scenarios were
deleted outright, not just disabled. `application/queries/` now holds
only Shape B: `get_entry.go` (`queries.Ships` — doc comment: *"treats KV
as a cache in front of the canonical Postgres projection"*), `terminal.go`
(the container KV list), and `meta.go` (the known-containers lookup set).
There is no `ReconstructFleet` or shape-labeled code anywhere in the tree
today; the only surviving references to Shape A/C are historical, in
plan docs and old test names.

This is the shape the code runs in production terms: canonical CQRS
projection in Postgres, NATS KV as an eager write-through cache populated
by the same JetStream event handler that upserts Postgres, cache miss
falls through to Postgres.
