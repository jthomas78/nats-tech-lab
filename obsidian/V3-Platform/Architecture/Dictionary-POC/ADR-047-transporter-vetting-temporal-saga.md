# ADR-047: Temporal Saga Design for `TransporterProfile` Vetting

**Status:** Accepted, with required amendments (see Punch List) — **further amended 2026-08-22, see "Amendment"**
**Date:** 2026-08-20
**Deciders:** Jeremy (repo owner) — part of Phase 38 design review
**Related:** [ARCHITECTURE-ORGANIZATIONS.md](ARCHITECTURE-ORGANIZATIONS.md) §§ "Temporal — role and workflow design," "Lifecycle," "Cross-aggregate invariant / saga," "CRUD vs. event sourcing"; [ADR-046](ADR-046-transporter-aggregate-split.md) (the aggregate-boundary decision this sits on top of)

## Amendment (2026-08-22, Phase 39 design gate)

[ADR-050](ADR-050-git-certificate-change-log-provenance.md) inverts who
produces document events, for the `GOODS_IN_TRANSIT` type.

**As built.** The workflow is the producer: a review command writes its
Postgres row, then signals the workflow (`Adapter.reviewSignal`), and the
workflow appends `document-approved` / `document-rejected`. The signal is
best-effort and its own comment names the failure mode — "a review that
writes its row and then fails to signal reads as approved while the workflow
still waits on it. Nothing reconciles the two automatically." A review
performed with no workflow running, or when the signal fails, produces a row
and **no event at all**.

**After 39a.** The **command** appends `document-approved`, and the
workflow's own emit for that event is **deleted**. The workflow derives its
view by **reading document state** rather than trusting a signal to carry it.

**Why.** Under ADR-050's Option A the event *is* the record of the approval,
so it cannot be allowed to depend on a best-effort delivery — and if both the
command and the workflow appended, every approval would double-emit. Reading
state also closes a second hole this change would otherwise have made worse:
Phase 39's decision 5 locks superseded certificates and cancels open reviews
on them, so a failed cancel signal would leave the workflow waiting on a
review the UI could no longer re-drive. (A superseded certificate
additionally keeps accepting review-resolution, so the outcome is recorded as
*cancelled* rather than abandoned.)

**Scope.** `GOODS_IN_TRANSIT` only. The other four document types keep the
signal path until they follow GIT onto the stream. Points 1 and 2 of the
Punch List below are unaffected — publishes still happen in Activities where
the workflow does publish, and compensation is still a new event.

## Context

Phase 38's design puts a Temporal workflow (`TransporterVettingWorkflow`) in
front of `TransporterProfile`, an event-sourced aggregate (JetStream stream
`TRANSPORTER`, Shape B Postgres+KV read side). The workflow runs two
parallel, independently-compensable branches (document review, GIT
verification) and, once both succeed, a separate cross-aggregate guard
(`Organization.Activate()` requiring `TransporterProfile.Status ==
Vetted`) connects it back to the unchanged, plain-CRUD `Organization`
aggregate from ADR-046.

This repo has **no existing Temporal usage anywhere** (verified: zero
`go.temporal.io/sdk` references, zero `workflow.Context`/`activity.Context`
usage in any `.go` file) — this design introduces the pattern fresh, with
no internal precedent to lean on. It also assumes more infrastructure than
currently exists: `demos/01-dictionary/backend/shipping-service`'s real
event-sourcing code (verified: `internal/application/commands/commands.go`,
`internal/jstream/stream.go`, `internal/eventhandler/handler.go`) has a
working hydrate→validate→publish pattern and a working Shape B dual-write
projector, but has **zero implementation** of the publish-dedup /
optimistic-concurrency guard (`Nats-Msg-Id`, `Nats-Expected-Last-Subject-
Sequence`) the design doc says `TransporterProfile` "reuses" from Phase
101 — Phase 101 itself is 100% unimplemented (`.claude/plans/Main-POC-Plan.md`,
every checklist line unchecked). This distinction matters: the design
doesn't get to *reuse tested code*, it has to *implement Phase 101's design
for the first time*, and get it right on the first attempt with no working
example to copy.

## Decision

**Affirm Temporal as the right tool for this workflow** — a multi-day,
human-in-the-loop, multi-branch approval process with real compensation
requirements is squarely Temporal's best-fit use case (durable timers,
signals, saga compensation), and this was an explicit input to the design
brief, not something this review re-litigates. **The two-branch
parallel-saga structure is sound.** Six specific points needed scrutiny;
findings and required amendments below.

### 1. Dual event-log correctness — real risk, must fix

Temporal workflow history and the JetStream event log both durably record
overlapping facts about the same vetting process. The failure mode: an
Activity publishes a JetStream event, the publish itself succeeds
server-side, but the Activity's success result never reaches the Temporal
worker (crash, network partition — **exactly the scenario the design's own
durability test deliberately induces**, "kill the Temporal worker
process"). Temporal will retry the Activity on worker restart. Without a
dedup guard, this produces a **second** JetStream publish for the same
logical transition.

The design doc's Shape B eventhandler convention (verified:
`internal/eventhandler/handler.go`, `container_handler.go` — one durable
consumer, Postgres-then-KV in one callback, Ack only after both succeed)
protects the *read side* from partial writes, but does nothing about a
genuinely duplicated *event* arriving as two distinct, validly-ack'd
messages — the projector would happily apply both. The design's phrase
"reuses Phase 101's design" undersells what's needed here: since Phase 101
has no code, `TransporterProfile`'s publish Activity has to be the first
place in this repo this pattern is actually built, and the workflow's own
retry behavior makes it load-bearing from day one, not an optional
hardening pass.

**Required amendment:** every JetStream publish this workflow triggers
must happen inside a Temporal Activity (never inline in workflow code —
also a hard Temporal determinism requirement, independent of this
concern), with `Nats-Msg-Id` derived from a stable, replay-safe key (the
`organizationID` + event type + a workflow-local step counter — **not**
Temporal's own `RunID`, which deliberately changes across a `Resubmit`).
The stream's `Duplicates` window must be configured explicitly, not left
at an implicit default.

### 2. Compensation semantics under event sourcing — ambiguous wording, must fix

The design doc's own words: `CompensateRevertDocumentApprovals` makes
"documents go back to `PendingReview`." In an event-sourced aggregate this
can only be correct as a **new** event appended to the log
(`DocumentApprovalReverted`, projected as "current status reads
`PendingReview` again") — never a rewrite of the original `Approved` event.
The doc doesn't currently say this explicitly, and the phrasing ("go back
to") reads like an in-place mutation to anyone implementing it without
already knowing the constraint.

This isn't just a wording nitpick: the design's own stated reason for
choosing event sourcing here is "an operator or auditor needs to answer
what did we check, in what order, and who approved what" — a literal
revert-in-place would silently destroy exactly the audit trail that
motivated the choice. A named, forward-only compensating event preserves
it (the log shows: approved, then reverted, then why).

**Required amendment:** name the compensating events explicitly
(`DocumentApprovalReverted`, `FleetAvailabilityRevoked`) as distinct event
types from their forward counterparts, and state plainly in the design doc
that compensation means "append a new event," never "undo/delete a prior
one."

### 3. Workflow ID reuse on `Resubmit` — needs explicit verification

The design reuses one workflow ID
(`{context}-transporter-vetting-{organizationID}`) across a
`Rejected → Resubmit → fresh workflow` cycle. Temporal's workflow ID reuse
behavior depends on an explicit `WorkflowIDReusePolicy` at start time, and
this repo's own Phase 101 precedent already models the right posture for
this kind of not-yet-verified SDK behavior claim: "⚠️ Verify against
current NATS docs before implementing." The same discipline applies here
for the current Temporal Go SDK.

**Required amendment:** explicitly set a `WorkflowIDReusePolicy` (a
deliberate new attempt after a definite terminal close is the textbook case
for `AllowDuplicate`) and write a test that starts a workflow, drives it to
`Rejected`, and confirms `Resubmit` actually starts a fresh run rather than
erroring on an ID collision — verified against current SDK docs at
implementation time, not assumed from this ADR.

### 4. Long-running-workflow versioning risk — acceptable POC gap

Real-world vetting could span days, and this workflow's code will likely
change across sub-phases 38b's own iteration. Temporal's determinism
requirements mean changing a workflow's code while instances are in flight
needs `GetVersion`-style patching, which is disproportionate engineering
for what this phase is actually testing (saga/compensation/durability
mechanics, not long-term production operability). **Not a blocker** —
record it as a deliberate, named gap in the design doc (not a silent
omission), with one lightweight process mitigation: don't modify
`TransporterVettingWorkflow`'s code while a test instance is genuinely
mid-flight during the durability test itself.

### 5. Source of truth for `TransporterProfile.Status` — confirmed sound

Checked every place the design doc references status: the cross-aggregate
`Activate` guard reads `TransporterProfile`'s Postgres/KV read model, and
the frontend's stepper is explicitly "driven by the same `evt.*`/KV read
model... no direct Temporal dependency in the browser." Consistent
throughout — no place treats Temporal's own workflow Query as a
pseudo-database. This avoids a hard runtime dependency on Temporal being up
just to read status, which would have been a real architectural mistake.
**No amendment needed.** The unavoidable eventual-consistency lag between
"workflow step completed" and "read model reflects it" is the same
accepted trade-off Ship/Container's Shape B already carries — not a new
risk this design introduces.

### 6. Timeout scale mismatch — must fix, and it's a hard SDK requirement, not a tuning nicety

`RequestGitVerification`'s "hang-past-timeout" test path implies a timeout
value, but none is specified. This isn't optional: the Temporal Go SDK
requires an Activity to declare `StartToCloseTimeout` or
`ScheduleToCloseTimeout` — omitting both is a startup-time configuration
error, not a soft default. Given a real GIT check could plausibly take
much longer than a test run should wait, and the workflow's own overall
execution needs to tolerate human-timescale document review (V2's
`DOCUMENTS_IN_REVIEW` can last days) without being killed by an unrelated
execution timeout, this needs an explicit environment-configurable value
(short for POC test runs, a separately-considered default for anything
resembling production).

**Required amendment:** state explicit `StartToCloseTimeout` (and, if
retries are configured, `ScheduleToCloseTimeout`) for `RequestGitVerification`,
sourced from config, with a documented test-profile value distinct from a
production-scale placeholder.

## Consequences

- Building points 1, 3, and 6 correctly means sub-phase 38b is doing real,
  first-of-its-kind implementation work in this repo (publish dedup,
  workflow ID reuse policy, Activity timeout configuration) — not
  "wiring together two already-proven patterns," despite how the original
  design doc's "reuses Phase 101" phrasing reads. Budget 38b accordingly.
- Naming compensating events explicitly (point 2) is cheap now and expensive
  to retrofit once the stream has real events on it — worth getting right
  in 38a/38b's first pass, not deferring.
- Point 4's accepted gap means this design should not be presented as
  "production-grade Temporal usage" in the eventual pattern-cards doc —
  it's a genuine test of the saga/compensation/durability mechanics
  Temporal offers, with versioning discipline explicitly out of scope.
- Point 5 being sound is itself a finding worth keeping in the pattern-cards
  doc: it validates that Shape B's CQRS separation generalizes cleanly even
  when the write side gains a workflow engine underneath it.

## Punch List

**Must fix in the design doc / must implement correctly before sub-phase 38b starts:**
1. [ ] All JetStream publishes from the workflow happen in Activities, with
       `Nats-Msg-Id` keyed on `organizationID` + event type + step
       counter (not Temporal `RunID`), and the stream's `Duplicates` window
       set explicitly (point 1).
2. [ ] Compensating events get their own explicit names
       (`DocumentApprovalReverted`, `FleetAvailabilityRevoked`); design doc
       states plainly that compensation = new event, never a rewrite
       (point 2).
3. [ ] `WorkflowIDReusePolicy` chosen and verified against current Temporal
       Go SDK docs; a `Resubmit`-after-`Rejected` test confirms a fresh run
       actually starts (point 3).
4. [ ] `StartToCloseTimeout`/`ScheduleToCloseTimeout` for
       `RequestGitVerification` (and any other Activity) explicitly set,
       with a documented test-profile vs. production-placeholder value
       (point 6).

**Added 2026-08-22 (Phase 39 / ADR-050) — must land in sub-phase 39a:**
6. [ ] The workflow stops emitting `document-approved` /
       `document-rejected` for `GOODS_IN_TRANSIT`; the command becomes the
       sole producer (Amendment above).
7. [ ] The workflow reads document state instead of relying on
       `Adapter.reviewSignal` to carry the fact, and the comment justifying
       the best-effort signal is removed rather than left to confuse.

**Acceptable POC-scope gaps — record as deliberate, don't silently omit:**
5. [ ] Workflow versioning/`GetVersion` discipline for in-flight code
       changes — explicitly out of scope; mitigate only by not editing the
       workflow's code mid-durability-test (point 4).

**Confirmed sound, no action needed:**
- Cross-aggregate status reads go through the Postgres/KV read model
  everywhere, never Temporal's workflow Query API (point 5).
