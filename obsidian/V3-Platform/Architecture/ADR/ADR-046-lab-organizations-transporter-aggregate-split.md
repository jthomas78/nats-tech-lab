---
adr: 46
title: Transporter Aggregate Boundary: Shared Identity, Separate Vetting
status: Accepted
date: 2026-08-20
scope: lab
context: organizations
decision: Organization stays the single identity aggregate for Shipper and Transporter. Vetting state lives in a new event-sourced TransporterProfile aggregate.
why: Identity is current-state CRUD. Vetting has a lifecycle that must replay and audit. One aggregate would force both onto one persistence model.
related: [47, 48, 49, 50, 51]
---

# ADR-046: Transporter Aggregate Boundary — Shared Identity, Separate Vetting

**Status:** Accepted — **amended 2026-08-22 for GIT document placement, see "Amendment"**
**Date:** 2026-08-20 (revised same day — see "Revision History")
**Deciders:** Jeremy (repo owner) — part of Phase 38 design review
**Related:** [ARCHITECTURE-ORGANIZATIONS.md](../Dictionary-POC/ARCHITECTURE-ORGANIZATIONS.md) § "Decision," [BUSINESS_RULES-ORGANIZATIONS.md](../../../../demos/01-dictionary/BUSINESS_RULES-ORGANIZATIONS.md) (BR-TP01–BR-TP17, Phase 26)

## Revision History

This ADR's first version recommended **Option A** (a fully separate
`Transporter` aggregate, duplicating ~4 identity fields from
`Organization`) and only named **Option C** (shared identity, separate
vetting aggregate) as a considered-and-rejected alternative. After
reviewing Option C's write-up, the decision was revised same-day to adopt
**Option C instead**. Kept transparent here rather than silently rewritten,
per this repo's own convention for recording decision reversals (see
`Main-POC-Plan.md`'s renumbering logs). Option A's original scoring is kept
below, unedited, for anyone checking why it was on the table at all.

## Correction (2026-08-20, after ADR-048 and ADR-049)

**The "zero changes to `organizations`" claim below is overstated.** Two
later reviews each found a required change to that package, by completely
independent routes:

- [ADR-049](ADR-049-lab-organizations-cross-aggregate-concurrency.md) finding 5b: Company
  Information is not editable today *at all* — no `partner-update` command,
  no repository update method, and `registerRequest` accepts only
  `{Name, Type}`, so `company_name`/`registration_no`/`vat_registration_no`
  are columns nothing ever writes. Making that data section work needs a new
  domain method, repository method, command, and `api.*` handler, plus a
  `version` column.
- [ADR-048](ADR-048-lab-organizations-document-storage-nats-object-store.md) finding 2c:
  `compliance_documents`' primary key is `(organization_id, type)` — one
  document per type — but GIT status is specified as the worst across
  `GOODS_IN_TRANSIT` documents, plural. Multi-document derivation needs a
  schema change.

**The decision still holds, and is still better than Option A on this axis** —
these are *additive* changes to a tested aggregate, whereas Option A required
a *subtractive* one (retiring `PartnerTypeTransporter`), and Shipper's
behaviour is untouched either way. But the guarantee as originally written
("nothing to retire," implying nothing to touch) is not accurate, and the
regression-risk row in Option C's table below should be read as *"low, and
additive"* rather than *"zero."* Kept as a correction note rather than a
silent edit, consistent with this ADR's own Revision History convention.

## Amendment (2026-08-22, Phase 39 design gate)

This ADR placed compliance documents on the **CRUD side** of the split:
plain Postgres rows hanging off `Organization`, with `TransporterProfile`
holding only the review *status* of each
(`DocumentReviews map[reference]status`). [ADR-050](ADR-050-lab-organizations-git-certificate-change-log-provenance.md)
reverses that **for the `GOODS_IN_TRANSIT` type only**.

**What changes.** GIT document mutations become commands on
`TransporterProfile`, appending to the `TRANSPORTER` stream, and
`compliance_documents` becomes a projection for that type. Certificate
detail — insurer, cover amount, expiry, goods types, file reference — joins
`TransporterProfile.State`, which until now deliberately held only status,
gates and two maps.

**What does not change.** The aggregate boundary itself: shared identity,
separate vetting, `TransporterProfile` keyed by `Organization`'s ID. The
other four document types keep the CRUD path. No new aggregate is introduced
— a separate `GitCertificate` aggregate was considered and rejected, because
approval would then need either two events (one for the log, one for the
saga) or new machinery to bridge the certificate's events into the vetting
saga.

**Why the reversal is narrow rather than a re-opening of the split.** This
ADR's reasoning was about *identity* duplication, not about which side
document fields live on; and per-document state was already partly on the
aggregate via `DocumentReviews`, so this extends an existing precedent rather
than contradicting one. The trigger is a compliance-audit requirement —
`CLAUDE.md`'s own deciding question, "does anything need to replay this," is
answered yes by an audit of a sequence of transitions.

**Cost, named rather than glossed:** the aggregate the Temporal saga replays
gets materially bigger, and `State` stops being thin.

## Context

`organizations-service` today has one `Organization` aggregate
(`internal/domain/organizations.go`) with a `Type` discriminator
(`SHIPPER`|`TRANSPORTER`, BR-TP01), a 3-state lifecycle
(`Registered → Active ⇄ Suspended`), and two child concepts —
`ComplianceDocument` and `FleetAsset` — that are already **partly gated by
that discriminator**:

- `AddFleetAsset(partnerType PartnerType, ...)`
  (`internal/domain/fleet_asset.go:42`) rejects any partner whose type isn't
  `TRANSPORTER` (BR-TP12).
- `ValidateDocumentType(partnerType PartnerType, docType DocumentType)`
  (`internal/domain/compliance_document.go:72`) rejects
  `GOODS_IN_TRANSIT` for anything but `TRANSPORTER` (BR-TP07).

Phase 38 needs Transporter to grow substantially beyond this: a
Temporal-orchestrated, event-sourced vetting workflow with a genuine saga
and compensating transactions, real document blob storage, and a materially
richer lifecycle than `Registered/Active/Suspended`. Shipper has no such
requirement and stays exactly as shipped.

## Decision

**`Organization` (Phase 26, unchanged) is the single identity aggregate
for both Shipper and Transporter.** `Register`, its `Type` discriminator,
and its `Registered → Active ⇄ Suspended` lifecycle are untouched;
`PartnerTypeTransporter` **stays a fully legal, actively-used value** — it
is literally how a Transporter's identity record comes into existence. A
new **`TransporterProfile`** aggregate — event-sourced,
Temporal-orchestrated — holds everything actually Transporter-specific:
fleet, documents, GIT state, tracking credentials, operating areas, and the
vetting workflow's own state. `TransporterProfile` is keyed by the **same
ID** as its `Organization` record — a 1:1 relationship by shared
identity, no separate surrogate ID, no join table.

One new coupling connects them: `Organization.Activate()`, for a
`TRANSPORTER`-typed partner, must not succeed until `TransporterProfile`
reaches `Vetted`. This lives at the command-handling boundary (the
`browserrpc`/`api.*` layer that already routes `activate`), not inside
either aggregate's own domain code, and only applies to `TRANSPORTER` — a
`SHIPPER`'s `Activate()` behavior is byte-for-byte identical to today.

## Options Considered

### Option A: Fully separate aggregate, duplicated identity fields (originally recommended, now superseded)

Transporter gets its own domain package, own Postgres tables, own
JetStream stream — including its own copy of name/registrationNo/VAT no.

| Dimension | Assessment |
|---|---|
| Complexity | Medium — a second full hexagonal skeleton, but a well-worn shape in this repo. |
| Coupling | Lowest of the three options — zero shared code paths at all. |
| Duplication | ~4 identity fields, no behavior. |
| Regression risk on BR-TP01–17 | Zero, **conditional on retiring `PartnerTypeTransporter` from `organizations`** — this ADR's first version required that as a correction; **no longer applicable under Option C**, since the value stays legal there by design. |
| Consistency with repo conventions | High — matches this repo's hexagonal layout. |

**Superseded, not merely rejected** — approved once, then reconsidered once
Option C's write-up was reviewed. The original case for A over C ("~4
duplicated scalars are cheaper than a two-step creation flow") is a real
trade-off, just judged the wrong way on reflection: DDD aggregate
boundaries drawn by *consistency need* (Option C) are worth a small amount
of added creation-flow complexity, and that complexity turned out to be
boundable (see Decision above and Consequences below) rather than open-ended.

### Option B: Extend `Organization` via the existing `Type` discriminator

Keep one aggregate; add vetting/saga/fleet-activation fields and behavior
conditionally on `Type == TRANSPORTER`.

**Rejected, unchanged from this ADR's first version.** The branch-growth
risk is not hypothetical — it is already present today (`AddFleetAsset`,
`ValidateDocumentType`) — and CLAUDE.md's own event-sourcing test answers
differently for Shipper (no) and Transporter (yes) within what would have
to be one aggregate. Neither Option A nor Option C has this problem.

### Option C: Shared identity aggregate, separate event-sourced vetting aggregate — ACCEPTED

Keep `Organization` exactly as shipped, serving both Shipper and
Transporter identity. Add `TransporterProfile`, event-sourced, referencing
`Organization` by shared ID, holding only what needs replay/saga/
compensation.

| Dimension | Assessment |
|---|---|
| Complexity | Medium — same two-package shape as Option A, but identity lives in exactly one place; the new cost is a two-step creation flow and one cross-aggregate guard, both bounded and concretely designed (see Decision). |
| Coupling | Low-medium — `TransporterProfile`'s existence depends on an `Organization` record existing first, and `Organization.Activate()` depends on `TransporterProfile`'s state for one partner type. Both dependencies are one-directional and narrow (an ID reference and a status read), not shared mutable state. |
| Duplication | None. |
| Regression risk on BR-TP01–17 | Zero — `Organization`'s own code is **entirely untouched** (not even the "retire a value" edit Option A needed); the only new code sits in a new package plus a thin guard at the API boundary. |
| Consistency with repo conventions | High — the more textbook DDD move (boundaries around consistency needs, not around "type of business entity"), and V2's own real entity split (`BusinessEntity` vs. `TransporterProfileEntity`) independently mirrors this shape — see `ARCHITECTURE-ORGANIZATIONS.md` § "Lifecycle." |

**Pros:** zero field duplication; zero changes to shipped, tested
`Organization` code (stronger regression guarantee than Option A, which
needed a `PartnerTypeTransporter` retirement); a genuinely cross-aggregate
invariant (two real aggregates, two consistency models) to test, which is
a more realistic exercise of "saga and compensating functions across
aggregates" than Option A's version (where the saga's two branches lived
inside one aggregate). **Cons:** two-step registration needs explicit
partial-failure handling; one new orchestration-layer dependency to design
carefully (direction matters — see Consequences).

## Trade-off Analysis

The decision reduces to: is a small, well-scoped amount of **workflow**
complexity (two-step creation, one cross-aggregate guard) worse than a
small, well-scoped amount of **data** complexity (duplicated identity
fields, Option A)? Once the workflow complexity was actually designed out
in concrete terms — an idempotent upsert-by-ID for step 2, a guard that
lives at one well-defined layer, not scattered — it stopped being "real
complexity" and became "a documented two-step process," which is a better
trade than permanent field duplication with no such mitigation available
(duplicated fields don't get less duplicated by designing around them).

## Consequences

- **Zero changes to `organizations`.** Stronger than Option A's
  "zero regression risk, conditional on retiring a value" — here there is
  nothing to retire. `PartnerTypeTransporter` remains exactly as useful as
  `PartnerTypeShipper`.
- **Registration is now two steps for a Transporter**, handled explicitly:
  `Organization.Register(...)` then idempotent
  `CreateTransporterProfile(id)` (upsert-by-ID), with a bounded retry in
  the command handler and a standalone `EnsureTransporterProfile(id)` for
  manual recovery. A partial failure leaves a **visible, recoverable**
  "Registered, profile pending" state — not a silent data-integrity gap,
  and not the previous version's dual-entry-point hazard (which could skip
  vetting *entirely* via a legacy path; this can only ever *delay* it).
- **A new cross-aggregate dependency, direction matters.** The `Activate`
  guard must query `TransporterProfile`'s read model from the
  `organizations`-side command path (or from a thin orchestration layer
  above both) — never the reverse. If implementation finds this awkward
  (e.g. `organizations`'s existing `browserrpc` handler isn't a natural
  place for it), a new orchestration package is the right fix, not letting
  `organizations` import `transporterprofile`.
- **Concurrency now needs two separate mechanisms**, not one — a
  consequence Option A didn't have to face, since everything Transporter-
  specific was event-sourced there. `TransporterProfile` reuses Phase 101's
  JetStream-sequence design; `Organization`'s own identity-field edits
  (Company Information) need a plain optimistic-lock (`version` column),
  which is new scope Phase 26 never needed. See `ARCHITECTURE-ORGANIZATIONS.md`
  § "Concurrency" and § "Open questions" — whether this lands in 38a or a
  follow-up phase is still open.
- Produces a cleaner CRUD-vs-event-sourced comparison for the eventual
  pattern-cards doc than Option A did: here the CRUD side is genuinely
  shared by both party types, so the comparison isn't confounded by "is
  this different because of party type or because of consistency model."

## What Would Change This Decision

- If the two-step registration's partial-failure handling
  (`CreateTransporterProfile`/`EnsureTransporterProfile`) proves unreliable
  or confusing in practice once built — that's the signal Option A's
  simplicity (one atomic creation, accept the duplication) was actually the
  better trade after all.
- If the 1:1 shared-ID relationship ever needs to become 1:many (e.g. one
  `Organization` legitimately needing multiple `TransporterProfile`-like
  records) — the shared-ID design doesn't extend cleanly to that; a
  surrogate ID + explicit FK would be needed, which is most of the way back
  toward re-evaluating this boundary entirely.
- If a future Customer aggregate needs the same identity fields as both
  Shipper and Transporter — that's actually **support** for this decision,
  not a reason to revisit it (a third consumer of shared identity is a
  stronger case for Option C, not weaker).

## Action Items

1. [ ] Implement `TransporterProfile` keyed by `Organization`'s ID (no
       separate surrogate) — sub-phase 38a.
2. [ ] Implement `CreateTransporterProfile`/`EnsureTransporterProfile` as
       idempotent upserts, and the bounded-retry registration command
       handler — sub-phase 38a.
3. [ ] Implement the cross-aggregate `Activate` guard at the
       command-handling boundary, confirm dependency direction
       (`transporterprofile`/orchestration → `organizations`, never
       reversed) — sub-phase 38a/38b boundary (needs `TransporterProfile`'s
       read model to exist first).
4. [ ] Decide and record whether `Organization`'s new optimistic-lock
       need (Company Information concurrent edits) is in 38a's scope or a
       separate follow-up phase — currently open in
       `ARCHITECTURE-ORGANIZATIONS.md` § "Open questions."
5. [ ] Confirm BR-TP01/BR-TP07/BR-TP12 need **no wording changes** —
       unlike Option A, `TRANSPORTER` remains valid everywhere it is today;
       this is a check, not an edit.
6. [ ] Move GIT document commands onto `TransporterProfile` and grow its
       `State` with certificate detail, per the Amendment above — sub-phase
       39a.
