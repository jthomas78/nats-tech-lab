---
adr: 50
title: GIT Certificate Change Log Needs a Provenance Source
status: Accepted
date: 2026-08-22
scope: lab
context: organizations
decision: The GOODS_IN_TRANSIT document is event-sourced onto the TRANSPORTER stream. Its Postgres row becomes a projection. The other four document types stay CRUD.
why: A change log needs an event history that did not exist. Only the GIT certificate has a business need for one, so only it pays the event-sourcing cost.
related: [46, 47, 48]
---

# ADR-050: The GIT Certificate Change Log Needs a Provenance Source That Does Not Yet Exist

**Status:** **Accepted 2026-08-22** — Option A, scoped to `GOODS_IN_TRANSIT`. Decided at the Phase 39 design gate; all action items below are closed. Amends [ADR-046](ADR-046-lab-organizations-transporter-aggregate-split.md) and [ADR-047](ADR-047-lab-organizations-transporter-vetting-temporal-saga.md).
**Date:** 2026-08-21
**Deciders:** Jeremy (repo owner) — Phase 39 design review
**Related:** [Main-POC-Plan.md](../../../../.claude/plans/Main-POC-Plan.md) Phase 39 (decisions 11, 12, 13); [ARCHITECTURE-ORGANIZATIONS-TRANSPORTERS.md](../Dictionary-POC/ARCHITECTURE-ORGANIZATIONS-TRANSPORTERS.md) §§ 3, 5, 9.5; [ADR-047](ADR-047-lab-organizations-transporter-vetting-temporal-saga.md) (the saga whose signal is the only current producer of document events); [ADR-046](ADR-046-lab-organizations-transporter-aggregate-split.md) (the aggregate split that put documents on the CRUD side); `CLAUDE.md` § "Event sourcing vs plain CRUD"

## Context

Phase 39 makes three commitments that, taken together, are not satisfiable
against the code as built:

- **Decision 11** — "The change log is two framings of one projection …
  both projections of the `TRANSPORTER` stream, **not** a separate audit
  table (which could disagree with the events). Field-level 'from → to'
  comes from replaying the aggregate to the previous event and diffing."
- **Decision 12** — the log is exportable for a compliance audit, each CSV
  row carrying `event_id · seq` "so a row can be pinned back to the event
  that produced it."
- **Decision 13** — "Every event needs an actor. Nothing on the wire
  currently records who did anything."

Decision 13 identifies a real gap but understates it. The problem is not
that the events lack an actor. **The events do not exist.**

### Verified starting position

Compliance documents are **plain Postgres CRUD and emit nothing to
JetStream**. `ComplianceDocumentHandler` describes itself as "a thin
pass-through onto `domain.ComplianceDocumentRepository` — no audit trail
(BR-TP06 covers only the Organization lifecycle, not document review, which
has no enforcement consequence in v1 either)"
(`internal/application/commands/compliance_document.go:11-13`). Every one of
`AddDocument`, `SetDocumentExpiry`, `ApproveDocument`, `RejectDocument`,
`ResubmitDocument` calls straight through to the repository. No publish, no
append, no actor parameter.

The `document-approved` / `document-rejected` events on the `TRANSPORTER`
stream are **not** produced by those commands. They are produced by the
Temporal workflow when it receives a `DocumentReview` signal
(`transporterprofile/workflow/workflow.go:215-216`), and they carry a
document *reference* only — no fields, no before, no after.

That signal is explicitly **best-effort**. `Adapter.reviewSignal` runs after
the row write and never in front of it, and its own comment names the failure
mode: "a review that writes its row and then fails to signal reads as approved
while the workflow still waits on it. Nothing reconciles the two
automatically" (`internal/browserrpc/adapter.go:715-739`). A review performed
when no workflow is running — or when the signal fails — produces a Postgres
row and **no event at all**.

Finally, the aggregate has nothing to diff. `State` holds `Context`, `ID`,
`Status`, `AttemptNumber`, `FleetAvailabilityGate`, `GitVerified`,
`DocumentReviews map[reference]status`, `TrackingCredentials`, `UpdatedAt`
(`transporterprofile/domain/profile.go`). Insurer, contact details, cover
amount, expiry date, goods types, file name — none of them are on the
aggregate, so "replay to the previous event and diff" yields review-status
transitions and nothing else.

### What that means for the three decisions

A change log projected from the `TRANSPORTER` stream would:

- **omit** registration, expiry edits, file attachment, insurer and contact
  edits, cover amounts and goods types entirely — every field the drill-down
  edit view can change;
- **silently omit approvals that actually happened**, whenever the
  best-effort signal did not land;
- produce no field-level from → to, because the fields are not on the
  aggregate.

For a compliance audit export — the stated purpose, confirmed by the user —
silently omitting real approvals is disqualifying. This is not a gap that
adding an actor field closes.

## Decision

**Option A, scoped to `GOODS_IN_TRANSIT`.** The GIT compliance document is
event-sourced onto the `TRANSPORTER` stream; `compliance_documents` becomes a
projection *for that type* while remaining the system of record for the other
four, which keep the CRUD path and get no change log.

The deciding input was not the trade-off table below. It was the user's
decision to **keep decision 12's `event_id · seq` export contract as
written**. Stream coordinates presuppose a stream, so that single answer
eliminated Options B and C outright — B cannot produce them at all, and C can
only produce them for the subset of fields it puts on the stream.

Two things about the change log were re-decided in the same session and are
recorded here because they change what this ADR's chosen option is *for*:

- **The log is GIT-scoped; decision 11's per-organization framing is
  deferred.** Fleet assets and tracking credentials have no provenance record
  of any kind — no events, no audit rows. A cross-area log built today would
  silently omit three of five areas, which is the same flaw this ADR used to
  disqualify a stream-projected log, arrived at from the other direction.
- **Decision 11's "replay the aggregate to the previous event and diff"
  mechanism is replaced by events carrying explicit `from`/`to` per changed
  field.** Diffing cannot represent "this field changed, its values are
  withheld", which the insurance-contact fields require (see Consequences).

Both are recorded in `Main-POC-Plan.md` Phase 39 decisions 11, 16 and 20.

## Options Considered

### Option A: Event-source the compliance document

Document commands become commands on `TransporterProfile`, appending
`document-registered`, `document-details-updated`, `document-file-attached`,
`document-approved`, `document-rejected`, `document-superseded` to
`TRANSPORTER`. `compliance_documents` becomes a projection of the stream
rather than the system of record.

| Dimension | Assessment |
|-----------|------------|
| Complexity | **High** — rewrites the whole document write path; an existing table changes role; existing rows need backfilling with synthetic events |
| Cost | Largest of the three; touches 39a, 39c and 39e |
| Scalability | Fine — same stream, same `LimitsPolicy`, no new subject family beyond the event token |
| Team familiarity | High — this is exactly the Ship/Container pattern the POC already runs |

**Pros:** Delivers decisions 11, 12 and 13 as literally written, `event_id ·
seq` included. Closes the best-effort-signal hole by construction: approval
*is* the event, so it cannot happen without one. Satisfies `CLAUDE.md`'s own
deciding question — "does anything need to replay this?" — which a compliance
audit answers yes to. Cover history (§ 9.8, deferred) becomes free later, as
the plan already assumes.

**Cons:** Much the largest change, and it lands in the phase's first
sub-phase. Reopens ADR-046's placement of documents on the CRUD side of the
aggregate split. Backfill has no real actor to record for historical rows.

### Option B: Extend `organizations.audit_events` to cover documents

The table, its repository and an `Actor` type already exist
(`internal/postgres/audit_repository.go:36`,
`internal/domain/audit.go:36`), and BR-TP06 already writes actor-bearing rows
for the Organization lifecycle. Document handlers gain an actor parameter and
write audit rows.

| Dimension | Assessment |
|-----------|------------|
| Complexity | **Low** — the mechanism exists and is proven |
| Cost | Smallest; mostly threading an actor through five handlers |
| Scalability | Fine; append-only table, already indexed by `(organization_id, created_at DESC)` |
| Team familiarity | High — BR-TP06 is the pattern |

**Pros:** Cheapest path to an exportable log. No change to the aggregate
split.

> **Correction (2026-08-22, verified against the code).** This section
> originally claimed Option B "records the act at the point the act happens,
> so no signal can be lost." **That is false as built.** `AuditRecorder` is
> best-effort *by design* — its own port documentation requires that "a
> failed Record must never block or roll back the lifecycle operation it
> describes" (`internal/domain/audit.go`) — and every caller discards the
> error (`_ = h.audit.Record(...)`). Audit rows can go missing exactly as
> silently as the Temporal signal can. Option B's advantage over the status
> quo was therefore never durability; it was only cost. The trade-off
> analysis below stands on its other reasoning, but this pro should not be
> cited.

**Cons:** This is precisely the "separate audit table" decision 11 rejected —
though note the stated reason ("could disagree with the events") is currently
vacuous, since there are no events to disagree with. **Cannot produce
`event_id · seq`**; those are stream coordinates. Decision 12's export
contract would have to change to a row id. Contributes nothing to the
deferred cover history.

### Option C: Hybrid — events for the compliance-relevant facts, audit rows for the rest

Registration, approval, rejection, supersede and cover/expiry changes become
events (they are the facts an auditor asks about); cosmetic edits stay CRUD
with audit rows.

| Dimension | Assessment |
|-----------|------------|
| Complexity | **Medium**, but the boundary is a judgement call re-litigated per field |
| Cost | Between A and B |
| Scalability | Fine |
| Team familiarity | Medium — no existing precedent in this repo |

**Pros:** Keeps the event log to facts with a genuine replay need. Cheaper
than A.

**Cons:** Two provenance sources means the change log must merge and order
across a stream and a table, and "which fields are compliance-relevant" will
drift. The unified log decision 11 wanted becomes the hardest of the three to
build.

## Trade-off Analysis

The decisive point is that **decisions 11–12 and the current CRUD document
model are incompatible, and one of them has to move.** `event_id · seq` in an
export contract is a statement that the log's source of truth is a stream.
Either the documents go on the stream (A), or the export contract drops to
row identifiers (B).

Option C's appeal is cost, but it buys that by splitting the log's source in
two — which is the very failure decision 11 was written to avoid, arrived at
from the other direction.

Between A and B, the question is whether GIT certificate history is a domain
concern or a record-keeping one. `CLAUDE.md` frames it as: event-source when
something must *reconstruct state from the log*, enforce rules against a
point-in-time replay, or audit a sequence of transitions. GIT certificates hit
the third condition squarely, and the deferred cover-history timeline (§ 9.8)
is the first — it is a reconstruction of cover state over time, and the plan
already banks on it needing "no migration to add later," which is true under A
and false under B.

**Decided: Option A, scoped to `GOODS_IN_TRANSIT` first.** The other
four document types keep the CRUD path in Phase 39 and follow later. This
keeps 39a's blast radius to the certificate the phase is actually about,
proves the pattern against a real screen, and leaves the export contract in
decision 12 intact. The alternative escape hatch — Option B as a
defensible v1 with decision 12 amended — was offered and **declined**: the
user kept the export contract as written, which is what settles this.

## Consequences

**Easier:** the change log and its export become truthful by construction;
cover history stops being a future migration; the best-effort-signal
reconciliation hole closes for documents.

**Harder:**

- **39a grows considerably**, and is deliberately not split further — it is
  one coherent write-path change and 39c is unbuildable without all of it.
  39d and 39e moved out to a new Phase 46 instead.
- **`TransporterProfile.State` stops being thin.** Certificate detail joins an
  aggregate that today holds only status, gates and two maps, and the saga
  replays it. Chosen over a separate `GitCertificate` aggregate, which would
  have needed either two events per approval or new machinery to bridge the
  certificate's events to the saga. **Amends ADR-046.**
- **The command becomes the sole producer of `document-approved`, and the
  workflow's emit is deleted.** The workflow derives its view by reading
  document state rather than trusting a signal to carry it. This is what
  actually closes the best-effort hole, and it also closes decision 5's lock
  dead-end (secondary finding 2) in the same change. **Amends ADR-047.**
- **The insurance contact's name and number cannot go on the stream.**
  `LimitsPolicy` plus replay means neither could ever be corrected or erased
  — the reasoning `TrackingCredentialConfiguredEvent` already accepted for
  secrets. Events record *that* those fields changed; values live in the
  projection. The export's `from → to` is therefore empty for them.
- **The actor recorded is not trustworthy, and the export must say so.** It
  is an unauthenticated client-supplied header defaulting to the literal
  `"admin"` (`internal/browserrpc/adapter.go`). Every export row carries
  `actor_verified`, always `false` until authenticated identity lands — a
  per-row column, not a banner, because a banner does not survive the file
  being opened in a spreadsheet and re-saved.
- **No backfill.** Existing rows are dev data regenerated by
  `cmd/seed-transporters`; fabricating actors would pollute the one log whose
  value is that it contains nothing nobody can vouch for.

**To revisit:** whether the remaining four document types follow GIT onto the
stream, and when; and the per-organization change-log framing, once fleet
assets and tracking credentials have provenance at all.

## Secondary findings (not blocking, but Phase 39 must answer them)

1. **Actor is half-built, which is good news.** `organizationcommands.Actor`
   exists and BR-TP06 already records one. Decision 13 is an *extension* of an
   existing concept, not a new one — and the system-actor convention already
   exists in the wild as `Actor{Name: "temporal-git-monitor"}`
   (`transporterprofile/orchestration/git_status_drop.go:55`). Decision 13's
   `cover timer` spelling should align with that rather than start a second
   vocabulary. The comment justifying the omission for documents — "no
   enforcement consequence in v1" — is falsified by the audit-export
   requirement and should be removed, not left to confuse.

2. **Locking on approval makes the best-effort signal worse.** Decision 5
   locks earlier certificates and cancels open reviews on them. If the
   cancelling signal fails, the workflow still waits on a review that the UI
   can no longer re-drive, because locked certificates reject mutating
   commands. Today that state is recoverable by re-reviewing; after decision 5
   it is not. Needs a reconciliation answer, or the lock needs an escape hatch.

3. **Per-goods-type cover changes `DeriveGitStatus` and the cover timer's
   meaning, even though no workflow is added.** `DeriveGitStatus` returns one
   worst-across-all status (`internal/domain/git_status.go:50-61`); with cover
   per goods type, "Active" stops being a single fact. And BR-TP60's timer
   sleeps until the earliest expiry across approved GIT documents — under the
   new model that first expiry may drop cover for one goods type only, so
   `CoverLapsed` plus full fleet-gate revocation is likely too blunt. Decision
   9 is right that no new workflow is needed; the *semantics* still change.

4. **`FOR_REVIEW` needs the CHECK constraint widened, and `gitStatusOf`'s
   `default:` branch made explicit.** Today anything not `APPROVED`/`REJECTED`
   falls to `Pending` (`git_status.go:66-78`), so `FOR_REVIEW` would map there
   silently. That is probably correct — but it should be a spec, not a
   fall-through.

5. **The flat table cannot be served by the current read path.**
   `ListDocuments` excludes superseded rows by design ("current documents
   only … never returned",
   `internal/postgres/compliance_document_repository.go:122-131`) and orders
   `BY type`. Decision 1's table shows every certificate — the mockup's
   Expired and Rejected locked rows are exactly what today's query hides —
   newest registration first. A new query is needed.

6. **There is no column to sort "newest registration first" by.**
   `compliance_documents` has `updated_at` and no `created_at`
   (`internal/postgres/migrate.go:37-46`). Registration order and the change
   log's `occurred_at` both need one added.

7. **Early renewal makes "the incumbent" plural.** `AddDocument` supersedes
   with `WHERE organization_id = $1 AND type = $2 AND status <> 'SUPERSEDED'`
   — an unbounded update written when one current document per type was the
   invariant (`compliance_document_repository.go:96-102`). Under decision 4
   two live GIT certificates coexist, and under decision 5 this statement must
   move to the approval path and target *earlier* certificates specifically.
   The partial index is non-unique, so nothing breaks at the schema level —
   the bug would be silent.

## Action Items

All closed at the 2026-08-22 design gate.

1. [x] **Option A**, scoped to `GOODS_IN_TRANSIT`. Decision 12's export
      contract kept as written, which is what forced it.
2. [x] ADR-046 amended for the document placement change (see Consequences).
      **No backfill** — existing rows are regenerated by
      `cmd/seed-transporters`, so no backfill actor is needed.
3. [x] Secondary finding 2 answered: the workflow stops depending on a signal
      for its facts and reads document state instead (also action item 1's
      producer inversion), and a superseded certificate keeps accepting
      review-resolution so a stranded review can be driven to rest and
      recorded as *cancelled*.
4. [x] Findings 3–7 folded into Phase 39's decisions 15, 21 and sub-phases
      39a/39c. Finding 3: `DeriveGitStatus` keeps one status and the timer's
      semantics are unchanged, because per-goods-type cover is capture-only —
      there is no load allocation in this codebase and today's
      `CoverageCents` is already written and never read. Finding 4:
      `FOR_REVIEW` derives to `Pending` as an explicit branch with a spec.
      Findings 5–7: new read query, `created_at` column, and the supersede
      statement moved to the approval path as a write-side invariant backed
      by a **unique** partial index on `(organization_id, type) WHERE status =
      'APPROVED'`.
5. [x] BR-TP64+ rules confirmed; the design gate is closed. Two of them
      changed on confirmation: expiry is guarded at registration *and* at
      approval, and a superseded certificate accepts `SetExpiry` as well as
      review-resolution (which deletes `SetExpiry`'s own
      `ErrDocumentSuperseded` guard and contradicts its comment — both to be
      rewritten in 39a).
