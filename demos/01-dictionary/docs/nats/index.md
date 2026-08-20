# NATS Design Patterns

<EyebrowLabel text="NATS" />

This section is the design-pattern reference behind the POC's use of NATS —
companion to the Obsidian "Event Sourcing" deep-dive and the
`Event Sourcing + CQRS + NATS — Pattern Cards` deck. Where the
[Architecture](/architecture/) section documents *what each service does*,
this section documents *why the underlying JetStream/KV/Postgres split
looks the way it does* — the decisions, the forks considered, and the
verdict each one landed on.

::: decision The question every page in this section serves
What is the correct responsibility split between **JetStream** (event
backbone), **NATS KV** (fast lookup / watch / cache), **Postgres**
(transactional store), and **CQRS projections**?
:::

![Event Sourcing + CQRS](/event-sourcing-cqrs.png)

Every command flows down the left (blue) path — API → command processor →
write model → an immutable JetStream event log. Every query flows down the
right (green) path — API → query processor → a read model materialized
from that same log. The two paths never share a store; they only share the
log.

## Pattern families

- **Modeling** — is it even event-sourced?
- **Consistency** — where truth and invariants live.
- **Projection** — shaping the read side.
- **Identity** — what an aggregate is keyed by.
- **Safety** — making the log trustworthy, on both the producer and
  consumer side.
- **Performance** *(cross-cutting)* — taming replay cost as streams grow.

## Sections

- **[JetStream + KV Mechanics](/nats/jetstream-kv-design)** — the three
  NATS components this design combines (streams, consumers, KV), the
  rehydration workflow, and event design principles.
- **[Modeling & Identity](/nats/modeling-and-identity)** — event-source it
  or plain CRUD, and what an aggregate is keyed by.
- **[Source of Truth](/nats/source-of-truth)** — which system owns the
  truth, and how cross-aggregate invariants are enforced without one.
- **[Projection Shapes](/nats/projection-shapes)** — the three read-model
  shapes the POC built side-by-side, and which one won.
- **[Write-Side Safety](/nats/write-side-safety)** — what stops concurrent
  commands and redelivery from corrupting the log or a projection.
- **[Performance & Pitfalls](/nats/performance-and-pitfalls)** — snapshot
  strategy for replay cost, plus the standing footguns of a JetStream
  system of record, with a POC map of where each decision is exercised.

<VerdictBadge status="completed" /> 7 decisions, 1 cross-cutting concern,
documented against the code as it stands today — not just the original
pattern deck.
