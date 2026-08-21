# ADR-049: Cross-Aggregate Concurrency — Two Mechanisms, One Guard

**Status:** Accepted, with required amendments (see Punch List)
**Date:** 2026-08-20
**Deciders:** Jeremy (repo owner) — part of Phase 38 design review
**Related:** [ARCHITECTURE-ORGANIZATIONS.md](ARCHITECTURE-ORGANIZATIONS.md) §§ "Concurrency — two operators editing the same Transporter," "Cross-aggregate invariant / saga — two layers, not one," "Frontend," "Open questions" (3); [ADR-046](ADR-046-transporter-aggregate-split.md) (whose "zero changes to `organizations`" claim this ADR corrects); [ADR-047](ADR-047-transporter-vetting-temporal-saga.md) (whose Activity-publish requirement interacts with this one); Phase 101 in [Main-POC-Plan.md](../../../../.claude/plans/Main-POC-Plan.md); [ARCHITECTURE-COMMUNICATIONS.md](ARCHITECTURE-COMMUNICATIONS.md) § 2 (subject taxonomy)

## Context

ADR-046's shared-identity split leaves Phase 38 with two aggregates under
one operator-facing record, with two different consistency models, and
therefore — as the design doc correctly notices — **two different
concurrency mechanisms**:

- `TransporterProfile` (event-sourced): Phase 101's JetStream
  `Nats-Expected-Last-Subject-Sequence` guard.
- `Organization` (plain CRUD): a new `version`-column optimistic lock.

They are joined by one cross-aggregate guard: `Organization.Activate()`
must refuse a `TRANSPORTER`-typed partner whose `TransporterProfile` has not
reached `Vetted`.

Verified starting position, which differs from the design doc in two
material ways:

- **Phase 101 is 100% unimplemented** (nine unchecked items,
  `Main-POC-Plan.md:1196-1204`; independently confirmed — zero hits for
  `Nats-Expected`/`ExpectLastSubjectSequence`/`Nats-Msg-Id`/`MsgId(` in any
  non-test Go). The repo's own docs already say so:
  `demos/01-dictionary/docs/nats/write-side-safety.md:29-42`.
- **`Organization`'s existing concurrency is pessimistic, not the
  compare-and-set the design doc describes.** See finding 5.

## Decision

**Affirm the two-mechanism split** — it is the correct consequence of
ADR-046, and the design doc deserves credit for spotting it rather than
assuming one mechanism would cover both. **But three of the six points
below are corrections, not refinements:** the event-sourced guard provides
close to no protection for this particular aggregate as specified, the
"cross-aggregate invariant" is actually a one-time gate that a clock can
silently break, and `organizations` cannot be left unmodified.

### 1. The per-subject guard barely protects `TransporterProfile` — must resolve in design, not at implementation time

`Nats-Expected-Last-Subject-Sequence` guards **the published subject only**.
Verified subject construction (`shipping-service/dictionary/internal/domain/events.go:60-70`)
puts the event type **last**, as a fixed 6-token join, and hydration reads a
wildcard across the aggregate's leaves
(`ShipInstanceSubject` → `evt.{context}.shipping.ship.{id}.>`, events.go:74-76):

```go
func ShipSubject(context, shipID, event string) string {
	return strings.Join([]string{"evt", context, Domain, "ship", shipID, event}, ".")
}
```

So the guard checks a subject the aggregate only partially occupies. Phase
101's plan already flags exactly this, with a ⚠️ and a named fallback
(`Main-POC-Plan.md:1189-1193`):

> ⚠️ **Verify against current NATS docs before implementing**: an
> aggregate's events span multiple subjects (`…{id}.arrived` vs
> `…{id}.departed`), and the plain header checks the last sequence *of the
> published subject only*. Newer servers support
> `Nats-Expected-Last-Subject-Sequence-Subject` to guard against a wildcard
> filter (`…{id}.>`). Confirm server + nats.go client support; if
> unavailable, fall back to a single per-aggregate subject with the event
> type in the payload/headers, and document the trade-off.

**What this review adds: the severity is not transferable from Ship to
`TransporterProfile`, and the design doc's "unchanged from the prior draft"
treats it as though it were.** Ship has four event types driven by one
naturally-serialising state machine (`arrived`/`departed` alternate; two
operators racing to mark the *same* ship arrived collide on the *same*
subject, and the guard works). `TransporterProfile` is the opposite: its
event types are concurrent **by design** — an operator adds a fleet asset
while a second approves a document while the Temporal workflow records GIT
verification. Three different final tokens, three different subjects, three
per-subject guards that each pass, and **no conflict detected between any of
them**. The mechanism is close to a no-op for precisely the scenario this
section of the design doc exists to address.

Both remedies have real costs, and the choice is architectural:

- `Nats-Expected-Last-Subject-Sequence-Subject` (guard a wildcard filter) —
  keeps the taxonomy intact, but is a newer server feature and must be
  verified against the pinned `nats-server v2.14.5` and
  `nats.go v1.52.0` rather than assumed.
- One subject per aggregate, event type moved into headers/payload — works
  everywhere, but **breaks the repo-wide subject taxonomy**
  (ARCHITECTURE-COMMUNICATIONS.md § 2: every family has fixed arity and
  parsers read tokens by position; `events.go:90-96` asserts `len(parts) == 6`).
  Adopting it for `TransporterProfile` alone means one aggregate's subjects
  no longer match the platform convention, which is a documented divergence,
  not a free fallback.

**Required amendment:** pick one in the design doc, with the cost stated. Do
not carry "reuses Phase 101" forward as though the question were settled —
for this aggregate it is the load-bearing question.

### 2. The "cross-aggregate invariant" is a one-time gate, and a clock can break it — must fix

The section is titled *"Cross-aggregate **invariant**."* As designed it is a
**precondition check**: `Activate()` reads `Vetted` once, at activation, and
nothing ever re-checks. That gap has unusually sharp teeth here because of a
decision made elsewhere in the same design:

**GIT status is derived, not stored, and one of its inputs is time.**
Per the Data-sections table (faithfully following V2's real
`TransporterProfileEntity.gitStatus` getter), status is the worst across the
transporter's `GOODS_IN_TRANSIT` documents, over the 5-value
`PENDING|ACTIVE|EXPIRED|REJECTED|NONE` enum. `EXPIRED` arrives **by the
passage of time** — no command, no actor, and **no event** to hang a guard
on. So an `Organization` can sit at `ACTIVE`, indefinitely, with an expired
GIT certificate, and nothing in the design notices. That is the invariant
being violated in the ordinary course of business, not under a race.

Three honest options, and the design must pick:

- **(a) Rename it.** Call it an *activation gate*, scope it explicitly as a
  precondition, and state that post-activation drift is out of scope. Cheap,
  honest, and probably right for a POC — but then the pattern-cards doc must
  not claim this design demonstrates a maintained cross-aggregate invariant.
- **(b) React to revocation events.** A durable consumer suspends the
  partner when the profile leaves `Vetted`. Two cautions, both verified:
  this repo has **no precedent** for a service consuming another's business
  event to mutate its own aggregate (the only cross-service reaction is
  `shared/natstenants/tenants.go:167-173` subscribing `notify.accounts.*`
  to provision *infrastructure*, not domain state); and it must **not** use
  `notify.*`, which is explicitly best-effort — *"notify.\* is deliberately
  lossy: DeliverNewPolicy, no replay, failures logged and dropped"*
  (`refdata-service/.../notifybridge`), *"best-effort convenience for
  reactive UIs, not a correctness mechanism"*
  (`shipping-service/dictionary/internal/eventhandler/handler.go:179`). A
  correctness-bearing de-activation needs a durable `evt.*` consumer.
- **(c) Sweep for time-derived expiry.** No event-driven approach can catch
  `EXPIRED`, because nothing publishes when a date passes. This needs a
  scheduled re-evaluation — and the phase is *already* introducing the ideal
  tool for it: a Temporal durable timer. That is an elegant, on-theme
  answer, and arguably the most interesting thing this phase could
  demonstrate about Temporal beyond the saga itself.

**Required amendment:** choose (a), (b), or (c) explicitly. (a) is a
legitimate POC answer; silently keeping the word "invariant" while
implementing (a) is not.

### 3. The guard's stale read fails in the permissive direction — must bound, cheap to fix

The guard reads the Shape B read model, which lags the log. The dangerous
direction is *permissive*: the model still says `Vetted` while a revocation
or compensation event (`FleetAvailabilityRevoked`, per ADR-047) is published
but not yet projected — so activation succeeds on a premise that is already
false. The reverse (model not yet showing `Vetted`, activation refused) is
merely annoying and self-correcting.

**Required amendment:** have the guard read the **Postgres projection, not
the KV cache** (one fewer hop of lag, and the cache is explicitly a cache),
and state the accepted window explicitly. A strictly-correct version would
hydrate the profile's event stream at guard time; that is disproportionate
here, but the choice should be recorded rather than defaulted into. Note
this is a *different* claim from ADR-047 finding 5 — that finding confirmed
the guard reads the read model *rather than Temporal*, which is right; this
one is about which layer of the read model, and in which direction the lag
hurts.

### 4. Temporal Activity publishes and operator edits collide — an interaction neither ADR covers. Must fix

ADR-047 requires every workflow publish to happen inside an Activity with a
stable `Nats-Msg-Id`. This ADR requires every publish to carry an expected
sequence. Compose them:

1. The Activity hydrates, computes the expected sequence, and publishes.
2. An operator edit lands in between → the server rejects the append
   (err 10071).
3. Temporal sees a failed Activity and **retries** it.

The good news, verified against ADR-047's own choice: the `Nats-Msg-Id` key
is `organizationID` + event type + step counter, deliberately *not* the
Temporal `RunID`, so it stays stable across retries even though the expected
sequence changes on each attempt. The two designs are compatible — but only
by luck of that earlier decision, and only if the retry re-hydrates.

The bad news: a sequence conflict is **not a business failure**, yet it
arrives at the workflow as a generic Activity failure. Left alone, a
persistent editor could exhaust the retry policy and fail the workflow —
firing `CompensateRevertDocumentApprovals` and
`CompensateDeactivateFleetAssets` because two humans were typing at the same
time. Compensation for a concurrency conflict is a wrong and very
confusing outcome.

**Required amendment:** classify sequence-conflict as its own retryable
error type, with a retry policy sized for human edit cadence (not the
default), and ensure it can never reach the compensation path — a conflict
that exhausts retries should surface as "try again," not as a failed
vetting.

### 5. `Organization` cannot be left unmodified — ADR-046's headline claim needs correcting

Two verified facts change the picture:

**(a) The existing mechanism is pessimistic, and the design doc describes it
wrongly.** The doc says status transitions "already get a natural check via
`WHERE status = ?`". There is no such predicate. The only UPDATE on the
table is (`organizations-service/organizations/internal/postgres/organization_repository.go:112-113`):

```go
UPDATE organizations.organizations SET status = $2 WHERE id = $1
```

Safety comes instead from a row lock taken earlier in the same transaction
(:96-99, `SELECT … WHERE id = $1 FOR UPDATE`), with the repository's own
comment explaining why (:84-87): the domain guard runs against the locked
row *"so BR-TP03/04/05's legality checks always run against the actually-
persisted status, never a stale in-memory copy."* The conclusion (transitions
are safe) is right; the stated reason is wrong, and the wrong reason matters
because it makes the new mechanism look like an extension of an existing
compare-and-set when it is actually a *second, different* mechanism
alongside a pessimistic lock.

It also clarifies *why* the `version` column is genuinely needed rather than
redundant: `FOR UPDATE` locks at save time, so operator A opening an edit
form, operator B saving, then A saving produces a silent lost update — A's
transaction sees B's value and overwrites it, no conflict raised. Detecting
that requires a version compared against what A *read*, which is exactly
what `FOR UPDATE` cannot do. Worth stating, since "we already lock the row"
is an easy and wrong objection to the version column.

**(b) Company Information is not editable today — at all — so this is new
`organizations` code, not just a new column.** Verified: the repository
port has no update method (`internal/domain/repository.go:10-17` — only
`Register`/`Get`/`List`/`Activate`/`Suspend`/`Reactivate`); the fourteen
`api.*` handlers contain no `partner-update`
(`internal/browserrpc/adapter.go:151-165`); and `registerRequest` accepts
only `{Name, Type}` (:194-197). Consequently `trading_as`, `company_name`,
`registration_no`, and `vat_registration_no` exist as columns and are
returned by `Get`/`List`, but **no code path in the service ever writes a
non-empty value into them** — they are always `''`. (The domain comment
claims *"every other field is fillable incrementally as KYC/vetting
proceeds"*; nothing implements it.)

So the Company Information data-section row requires a new domain method, a
new repository method, a new command, and a new `api.*` handler in
`organizations` — plus the `version` column. **ADR-046's strongest selling
point over Option A — "zero changes to `organizations`," a stronger
regression guarantee than Option A's conditional one — is materially weaker
than stated.** It is still the better position (additive changes to a tested
aggregate beat Option A's *subtractive* "retire `PartnerTypeTransporter`",
and Shipper's behaviour is untouched either way), but the claim as written
is not accurate and should be corrected rather than defended. Note ADR-048
finding 2c reaches the same conclusion by a completely independent route
(multi-document GIT status needs a `compliance_documents` PK change) — two
separate reviews, two separate schema changes, one overstated claim.

### 6. One composed UI over two conflict mechanisms — must align save boundaries

The Frontend section says the split is *"a backend seam only"* and the
operator *"never sees two things."* Correct for **reads**. For **writes** it
is a hazard: the two aggregates fail differently (a JetStream 10071
rejection vs. a Postgres version mismatch), and a single save spanning the
Company Information tab and a profile tab can half-commit, leaving the
operator with one aggregate updated, one not, and a UI that has deliberately
hidden which is which.

**Required amendment:** save boundaries must align to the aggregate
boundary — per-section saves, never one submit spanning both — and the
conflict UI must name the section that lost. Hiding the seam for reads is
good product design; hiding it for writes converts a clean architectural
boundary into an unattributable error.

### 7. Open question 3 is answerable now — and the answer isn't "38a or later"

The doc asks whether `Organization`'s optimistic lock lands in 38a or a
follow-up. Finding 5(b) reframes it: the lock is inseparable from a
`partner-update` command **that does not exist yet**, so the real question
is where *that* lands. And it cannot slip past **38d**, which ships the
editable Company Information tab — shipping 38d without it means exactly one
tab in the record has no concurrency protection while every other tab does.

**Resolved recommendation:** deferring past 38a is fine; deferring past 38d
is not. Either land `partner-update` + `version` together in whichever
sub-phase first makes those fields editable, or make Company Information
read-only in 38d and say so.

## Trade-off Analysis

The design's core judgement — accept two mechanisms rather than force one —
is right, and the alternative (make `Organization` event-sourced too, so
one mechanism covers both) would trade a small amount of plumbing for
exactly the confounded CRUD-vs-event-sourced comparison ADR-046 worked to
avoid. What this review changes is the cost estimate: the event-sourced side
needs a real decision it doesn't have yet (finding 1), the CRUD side needs
new code rather than a new column (finding 5), and the guard between them is
a gate rather than an invariant (finding 2). None of these argue for a
different architecture; all three argue against the current sizing.

## Consequences

- Finding 1 makes the subject-shape question a **blocking design decision
  for 38a**, not an implementation detail — it may force a documented
  divergence from the platform subject taxonomy for one aggregate.
- Finding 2 determines whether this phase can honestly claim a maintained
  cross-aggregate invariant in the pattern-cards doc, or only an activation
  gate. Option (c) — a Temporal durable timer re-evaluating time-derived GIT
  expiry — would be the strongest result available, and is nearly free given
  Temporal is already in scope.
- Finding 5 means ADR-046 needs a correction note. Both this ADR and ADR-048
  independently found `organizations` changes that ADR-046 promised away;
  the decision still holds, the guarantee was overstated.
- Findings 4 and 6 are both "two accepted designs, considered separately,
  compose badly" — worth noting as a review-process observation: ADR-047 and
  this ADR were each sound in isolation.
- The `version`-vs-`FOR UPDATE` distinction in finding 5(a) is a genuinely
  useful pattern-card aside: pessimistic locks protect *transactions*,
  optimistic locks protect *user think-time*, and a form-based admin UI
  needs the latter regardless of what the former already does.

## What Would Change This Decision

- If finding 1's remedy forces one subject per aggregate, the platform
  subject taxonomy has met a case it doesn't serve — worth revisiting the
  taxonomy itself (does `{event}`-last belong in the subject at all for
  aggregates with many event types?) rather than treating it as a
  one-aggregate exception forever.
- If finding 2 lands on option (b) or (c) and the reactive/scheduled
  de-activation proves fragile, that is evidence the two aggregates are more
  tightly coupled than ADR-046 assumed, and the boundary deserves a second
  look.
- If `partner-update` (finding 5b) turns out to need most of `Organization`
  rebuilt to be safely editable, the "plain CRUD, unchanged" premise is
  weaker than ADR-046 priced in.

## Punch List

**Must fix in the design doc before 38a:**
1. [ ] Decide the subject-guard shape for `TransporterProfile`:
       `Nats-Expected-Last-Subject-Sequence-Subject` (verify support against
       the pinned `nats-server v2.14.5` / `nats.go v1.52.0`) **or** one
       subject per aggregate with the event type in headers — and state the
       cost, including the documented divergence from
       ARCHITECTURE-COMMUNICATIONS.md § 2 if the latter — finding 1.
2. [ ] Choose (a) rename to "activation gate," (b) durable `evt.*`
       revocation consumer — **never `notify.*`** — or (c) a Temporal
       durable timer re-evaluating time-derived GIT expiry. Stop calling it
       an invariant unless (b) or (c) is implemented — finding 2.
3. [ ] Correct the claim that status transitions get "a natural check via
       `WHERE status = ?`" — the real mechanism is `SELECT … FOR UPDATE`;
       and record why a `version` column is still needed (lost updates
       across user think-time, which row locks cannot detect) — finding 5a.
4. [ ] Record that Company Information requires a **new** `partner-update`
       command/handler/repository/domain method in `organizations`, and add
       a correction note to ADR-046's "zero changes to `organizations`"
       claim — finding 5b.

**Must fix before 38b (Temporal) / 38d (frontend):**
5. [ ] Sequence-conflict is a distinct retryable Activity error with a
       human-cadence retry policy, and can never reach the compensation
       path — finding 4, before 38b.
6. [ ] Save boundaries align to the aggregate boundary (per-section saves,
       never one submit spanning both); conflict UI names the losing
       section — finding 6, before 38d.
7. [ ] `partner-update` + `version` land no later than 38d, or Company
       Information ships read-only in 38d — closes Open question 3 —
       finding 7.

**Cheap fix, bounded risk:**
8. [ ] The `Activate` guard reads the **Postgres projection, not the KV
       cache**, with the accepted staleness window stated — finding 3.

**Confirmed sound, no action needed:**
- Guard dependency direction (`transporterprofile`/orchestration →
  `organizations`, never reversed) — consistent with ADR-046.
- The aggregate-instance `{id}` token in the subject is what makes
  per-aggregate concurrency control possible at all
  (`Main-POC-Plan-ARCHIVE.md:561` records why); `TransporterProfile`
  inherits this correctly.
- Recognising that two aggregates need two mechanisms, rather than assuming
  one covers both — the design doc got this right unprompted.
