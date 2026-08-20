# Modeling & Identity

<EyebrowLabel text="NATS" />

## Event-source it, or plain CRUD?

Not everything in a domain earns an event stream. Forcing one onto data
that doesn't need it adds subjects, consumers, and projections for no
benefit.

::: decision Modeling
Does anything need to **replay this to reconstruct state** — not "does it
change over time?"
:::

![Event-source or plain CRUD decision flow](/event-source-decision-flow.png)

The deciding flow: does anything replay this to reconstruct state → is
there a real state machine with transitions → does anything run a
temporal query ("as of date X")? A "yes" to any of those puts the entity
on an event stream; three "no"s puts it in plain Postgres CRUD.

| | Ship / Container | Ports (reference data) |
| --- | --- | --- |
| Write | command → rule → event | direct row write |
| State machine | yes | none |
| Replayed? | every command hydrates from the log | never |
| Audit | transitions *are* the fact | `created_at` only |

**Trap:** "is it reference data" isn't the whole test — a rate table
("what was in effect on date X") secretly needs history even though it
looks like static reference data.

<VerdictBadge status="completed" /> Ship and Container are event-sourced;
the Ports registry is plain Postgres CRUD — no lifecycle worth replaying
(Phase 9.5 / 9.6).

## Aggregate identity — surrogate or natural key?

When the human-facing natural key can be wrong or must change, don't make
it the aggregate's identity.

::: decision Identity
Fold the aggregate by an immutable **surrogate UUID**, or by the
**natural key** (needing an alias-aware corrective event when it
changes)?
:::

| | Surrogate key (UUID) | Natural key + corrective event |
| --- | --- | --- |
| Alias layer | needed on every natural-key read/write, permanently | only when a correction actually happens — rare |
| Retrofit cost | high — routes, keys, and folding all move | low — one new event type |
| Use when | the natural key is an external interchange standard you don't control | already keyed by natural ID everywhere; corrections are one-off |

**Original POC split (Phase 8.3):** Container got a surrogate UUID
because its natural key (the ISO 6346 code, e.g. `TCKU0001234`) is an
external standard the POC doesn't control and might need to correct. Ship
kept its natural key (`shipID`, an internal slug) because nothing forced a
correction.

::: decision Since revised — Phase 12.9
The same pressure that justified Container's surrogate key turned out to
apply to Ship too, once `shipID` was recognized as a mutable
name/call-sign rather than a stable identifier.
:::

**Current state (verified against `domain/ship.go`, `domain/container.go`):**
Both aggregates are now keyed by a surrogate UUID (`ContainerAggregate.ID`,
`ShipAggregate.ID`) — but the *correction* mechanism only exists on Ship
today. `ShipIDCorrectedEvent` and `ShipAggregate.CorrectShipID()` let a
mutable `shipID` be renamed via a compensating event (BR-021, BR-022),
with the KV read model explicitly rekeyed by the event handler. No
equivalent "container ID corrected" command exists — Container's natural
key (the ISO 6346 code) has no correction path in code today, the reverse
of what the original Phase 8.3 split anticipated. Because `shipID` is now
mutable, natural-key lookups (`hydrateByNaturalKey`) can no longer target
one ship via a subject filter the way Container's dedup check does — they
fold every ship's history in a context and match by *current* name.

<VerdictBadge status="completed" /> Both aggregates use a surrogate UUID
as their write-side identity; Ship — not Container — carries the
corrective-event mechanism (Phase 8.3, revised Phase 12.9).
