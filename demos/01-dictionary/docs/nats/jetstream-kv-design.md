# JetStream + KV Mechanics

<EyebrowLabel text="NATS" />

The design combines three NATS components into one durable, replayable
system. This page covers the mechanics; see
[Source of Truth](/nats/source-of-truth) and
[Projection Shapes](/nats/projection-shapes) for the decisions built on
top of them.

## The three components

**JetStream streams — the event store.** One shared stream, `SHIPPING`,
carries both Ship and Container events (`domain/events.go`); `refdata-service`
publishes to its own `REFDATA` stream. Retention is `LimitsPolicy`, not
`InterestPolicy` — required so replay stays possible; an `InterestPolicy`
stream discards messages once every consumer has acked them, which would
silently break rehydration. Subjects carry the aggregate ID as their own
token (`evt.{context}.shipping.ship.{shipID}.{event}`), which is what
makes per-aggregate replay and, later, per-aggregate optimistic concurrency
possible — see [Write-Side Safety](/nats/write-side-safety).

**JetStream consumers — replay, not steady-state subscription.** The
write side doesn't keep a long-lived durable consumer per aggregate.
`hydrate()`/`hydratePair()`/`hydrateByNaturalKey()` (`application/commands/`)
create a fresh `OrderedConsumer` with `DeliverAllPolicy` on every single
command, replay it to the end, and delete it (`dropConsumer`). This is
correct today because there's no snapshot to resume from — see
[Performance & Pitfalls](/nats/performance-and-pitfalls) for why that's a
*when*, not an *if*. The read side runs durable consumers instead:
`eventhandler/` projects every event into Postgres and NATS KV as it
arrives.

**NATS KV — two roles, not one.** It's easy to conflate these:

- **Write-side snapshot** *(not yet built — see
  [Performance & Pitfalls](/nats/performance-and-pitfalls))* — safe to
  trust for hard business-rule checks, because a snapshot must always be
  replayed forward to the latest stream sequence before being trusted.
- **Read-model projection** *(what's running today)* — the `ships`,
  `container`, and `meta` buckets are eventually-consistent write-through
  caches in front of Postgres (`queries.Ships`, doc comment: "treats KV as
  a cache in front of the canonical Postgres projection"). Fine for
  display and soft checks; never validate a hard rule against it.

## Rehydration workflow (as it runs today)

![Rehydration workflow](/rehydration-workflow.png)

```
Command arrives for aggregate {id}
         │
         ▼
Create an OrderedConsumer, DeliverAllPolicy,
FilterSubject scoped to {id}
         │
         ▼
Replay every event for {id} from sequence 1
         │
         ▼
Fold events into aggregate state (Apply/FromState)
         │
         ▼
Validate + apply the new command → append event
         │
         ▼
Delete the consumer (dropConsumer)
```

There is no snapshot store and no `seq > N` shortcut anywhere in the
write path — every command pays the full replay cost of its aggregate's
history. This matches the pattern deck's warning almost exactly: *"Both
write-side hydration and Shape C replay the log from seq=1 on every call.
Latency grows linearly with stream depth."* Shape C itself is gone
(retired in Phase 31 — see [Projection Shapes](/nats/projection-shapes)),
but the same growth curve now applies to ordinary command hydration.

## Event design principles

1. **Capture business intent, not a generic diff.** Events describe *what
   happened and why*: `ArrivedPort`, `DepartedPort`, `Registered`,
   `Loaded`, `Unloaded` — never a generic `Updated` with a changed-field
   bag.
2. **Immutable events; corrections are compensating events, never edits.**
   The repo has a real example of this: `ShipIDCorrectedEvent`
   (`domain/events.go`) doesn't rewrite history — it's a new event that
   carries `PreviousShipID`, and the event handler rekeys the KV entry
   from the old key to the new one in response. See
   [Modeling & Identity](/nats/modeling-and-identity) for why only Ship
   has this today, not Container.
3. **Version events as they evolve.** Not yet exercised in this POC —
   there's a single schema generation per event type so far — but the
   principle (tolerant deserialization, upcasters on replay) applies the
   moment a field needs to change shape under existing history.

## Testing strategy

Every business rule maps to one Ginkgo `Context` block with one or more
`It` assertions, written from the rule before the implementation — the
same given/when/then shape the pattern deck recommends: *given* prior
events, *when* a command is applied, *then* assert the emitted event or
rejection. See the Quality Rules in the repo's `CLAUDE.md` for the exact
workflow (rules first, design gate, tests from rules, docs updated in the
same task).

## Common pitfalls (from the design deep-dive)

- **Replaying a growing stream without snapshots** slows every write, not
  just reads — see [Performance & Pitfalls](/nats/performance-and-pitfalls).
- **Treating a KV projection as authoritative** for a hard rule — it's
  eventually consistent by construction; only a write-side snapshot
  replayed to the latest sequence earns that trust.
- **Deleting and republishing to "fix" history** — always a corrective
  event instead; deletion destroys the audit trail and doesn't scale past
  one entity anyway.
- **Unidempotent projectors** — a redelivered or reordered event must
  never corrupt the read model. Today's `kv.Put`/`ON CONFLICT` projectors
  don't yet guard against this — see
  [Write-Side Safety](/nats/write-side-safety).
