# Performance & Pitfalls

<EyebrowLabel text="NATS" />

## Snapshots — taming replay cost

Write-side hydration replays the log from `seq=1` on every call. Latency
grows linearly with stream depth, so for any long-lived aggregate,
snapshots are a *when*, not an *if*. See
[JetStream + KV Mechanics](/nats/jetstream-kv-design) for the full
rehydration workflow as it runs today — no snapshot shortcut yet.

**The rehydration flow, once snapshots exist:** load the snapshot at
`state @ lastStreamSequence` → replay only the events past that sequence
→ rebuild + validate → append. A snapshot must carry its own position —
without `lastStreamSequence` there's no way to know which events still
need replaying after it:

![Rehydration with snapshot, planned](/rehydration-with-snapshot.png)

```json
{
  "aggregateId": "ship.SH-001",
  "state": { "...": "..." },
  "lastStreamSequence": 9281
}
```

| | |
| --- | --- |
| Where it lives | a write-side snapshot store (KV or a Postgres table) — **not** a read-model projection |
| How far to trust it | authoritative for hard rules, *because* it's replayed to the latest sequence before use |
| Trigger | every N events, or scheduled — always rebuildable from the log if lost |

<VerdictBadge status="proposed" /> Not built yet. Scoped as Phase 104
(Performance & Load Testing), gated on Phases 101 and 103 landing first
and reusing an existing k6 baseline harness. The original "Shape C fleet
reconstruction" load scenario is moot now that Shape C is deleted (see
[Projection Shapes](/nats/projection-shapes)); the live scope today is
write-side hydration degradation as `hydrate()` replays grow, KV-watch/SSE
fan-out limits, cross-stream consumer lag once the `TERMINAL` stream
split lands, projection lag, and optimistic-concurrency contention once
Phase 101 lands.

## Things to be aware of

**Source-of-truth caveats, given Pattern B (JetStream = truth):**

- **Retention must never discard.** If messages age out, truth silently
  erodes — this is the single most dangerous default to get wrong on a
  stream that's a system of record. `LimitsPolicy` is used deliberately;
  the actual limits still need to be a documented decision, not a
  default left unexamined.
- **The Postgres copy lags.** Never read it on the write path or treat it
  as authoritative — it's a derived projection, always slightly behind.
- **You're debugging a lagging copy.** The SQL a developer inspects while
  troubleshooting may trail the stream by a few events; don't mistake
  projection lag for a bug in the stream itself.

**General pitfalls of this pattern:**

- **Exactly-once doesn't exist** — at-least-once delivery plus dedupe on
  id/seq is the real guarantee; every projection must be idempotent.
- **Per-subject ordering only** — there's no global timeline across
  subjects; keep projections aggregate-scoped.
- **No cross-aggregate atomic write under Pattern B** — invariants
  spanning aggregates need a saga or reservation store, not one
  transaction.
- **Never dual-write** — outbox or CDC, never "write the DB then
  publish" as two separate steps.
- **Correcting bad history is a corrective event** — never
  delete-and-republish; that destroys the audit trail and doesn't scale.

## POC map — where each decision is exercised

| Decision | Where it's covered | Status |
| --- | --- | --- |
| Event-source vs. CRUD — Ship/Container vs. Ports | [Modeling & Identity](/nats/modeling-and-identity) (Phase 9.5/9.6) | <VerdictBadge status="completed" /> |
| Source of truth = JetStream (Pattern B) | [Source of Truth](/nats/source-of-truth) | <VerdictBadge status="completed" /> locked working assumption |
| Read-model shapes A / B / C | [Projection Shapes](/nats/projection-shapes) (Phases 1/2/6, retired A/C in Phase 31) | <VerdictBadge status="completed" /> |
| Cross-aggregate — one shared stream | [Source of Truth](/nats/source-of-truth) (Phase 8) | <VerdictBadge status="completed" /> strong from a single replay |
| Aggregate identity — surrogate key | [Modeling & Identity](/nats/modeling-and-identity) (Phase 8.3, revised Phase 12.9) | <VerdictBadge status="completed" /> |
| Subject taxonomy — `evt.{context}.{service}.{entity}.{id}.{event}` | [Architecture: Communications](/architecture/communications) (Phase 12.8/16) | <VerdictBadge status="completed" /> |
| Write-side safety — optimistic concurrency + publish dedup | [Write-Side Safety](/nats/write-side-safety) (Phase 101) | <VerdictBadge status="proposed" /> |
| Projection hardening — sequence-guarded CAS writes | [Write-Side Safety](/nats/write-side-safety) (Phase 102) | <VerdictBadge status="proposed" /> |
| Stream split + cross-aggregate guard — `TERMINAL` stream | [Source of Truth](/nats/source-of-truth) (Phase 103) | <VerdictBadge status="proposed" /> |
| Load / degradation curves — snapshotting | [Performance & Pitfalls](/nats/performance-and-pitfalls) (Phase 104) | <VerdictBadge status="proposed" /> gated on 101 + 103 |

Phase numbers above are the current live-plan numbers
(`.claude/plans/Main-POC-Plan.md`/`-ARCHIVE.md`), not the phase numbers
in the original pattern-card deck — several phases were renumbered as the
plan grew past its first hundred entries.
