# Write-Side Safety

<EyebrowLabel text="NATS" />

Making JetStream trustworthy *as a store*, not merely as a log. This is
the part of [Source of Truth](/nats/source-of-truth)'s Pattern B verdict
that has to be earned, not assumed.

## Producer side

::: decision Safety
How do you stop concurrent commands and client retries from corrupting
the source of truth?
:::

**Gap — lost invariants.** Two concurrent commands hydrate the same
pre-state, both validate, both publish — jointly violating a rule (e.g.
the same container ending up on two ships). **Fix — optimistic
concurrency:** publish with `Nats-Expected-Last-Subject-Sequence`; on
reject, re-hydrate, re-validate, and retry (bounded).

**Gap — retry double-write.** An HTTP client retrying after a timeout
durably appends the business event twice — and in event-sourced mode,
the duplicate *is* the record, not just a duplicate row. **Fix — publish
dedup:** `Nats-Msg-Id` from a command idempotency key, with the stream's
`Duplicates` window configured explicitly rather than left at its
default.

**Why the subject taxonomy is load-bearing here.** Per-aggregate
concurrency only works because every event subject carries the `{id}`
token (`evt.{context}.shipping.ship.{shipID}.{event}`) —
expected-last-*subject*-sequence is scoped per subject. If events for one
aggregate were ever split across multiple leaf subjects, this would need
the `-Subject` variant explicitly, or one subject per aggregate as a
fallback.

<VerdictBadge status="proposed" /> **Not implemented today.** A grep of
the shipping-service Go code finds no `Nats-Expected-Last-Subject-Sequence`,
`ExpectLastSubjectSequence`, `Nats-Msg-Id`, or `MsgId(` anywhere —
`Publisher.Publish`/`PublishMsg` (`internal/jstream/stream.go`) take no
sequence or dedup options today. This is scoped as Phase 101
(Write-Side Safety), planned to keep `jetstream` types out of the
application layer via the existing `Publisher` port.

## Consumer side

::: decision Safety
How does a projector guarantee a stale or duplicated redelivery can't
clobber newer state?
:::

**Guarded write (CAS loop).** The stored value carries the source
event's sequence; on redelivery, skip if `event.seq <= stored.seq`,
otherwise write with a compare-and-swap (KV `Update` with an expected
revision; Postgres `ON CONFLICT ... WHERE excluded.seq > current.seq`).

**Every projector, by construction, should be:**

- **Idempotent** — re-applying an event never corrupts the model.
- **Ordered where required** — same-entity events applied in sequence.
- **Checkpointed** — tracks its last processed stream position.
- **Version-aware** — upcasters handle old event schemas on replay.

<VerdictBadge status="proposed" /> **Not implemented today.**
`eventhandler/handler.go` writes via a plain `kv.Put(...)` with no
revision guard; the Postgres upserts in `postgres/repository.go` and
`postgres/container_repository.go` are plain `ON CONFLICT (context, id)
DO UPDATE` with no sequence comparison — last-write-wins, with no defense
against stale redelivery. This is scoped as Phase 102 (Projection
Hardening): CAS-guarded KV writes, a persisted last-applied-sequence
column on the Postgres upserts, explicit `MaxAckPending` per projector
consumer, an explicit retention/discard decision on `CreateStream`, and a
documented poison-message policy.

## Standing footguns of Pattern B

Concurrency control is what actually separates a *store* from a *log* —
Postgres gets it from `UNIQUE`, JetStream gets it from
`expected-seq`/dedup, and hand-rolling it (e.g. via Redis) is explicitly
the thing to avoid on the write path.

- **Exactly-once doesn't exist.** At-least-once delivery plus dedupe on
  id/seq is the real guarantee — every projector must be idempotent.
- **Per-subject ordering only.** There's no global timeline across
  subjects; projections must stay aggregate-scoped.
- **No cross-aggregate atomic write under Pattern B.** Invariants that
  span aggregates need a saga or reservation store, not one transaction —
  see [Source of Truth](/nats/source-of-truth)'s cross-aggregate section.
- **Never dual-write.** Outbox or CDC only — never "write the DB, then
  publish" as two separate steps.
- **Correcting bad history is a corrective event, never a
  delete-and-republish** — deletion destroys the audit trail and doesn't
  scale past a single entity. See
  [Modeling & Identity](/nats/modeling-and-identity)'s `ShipIDCorrectedEvent`
  example.
