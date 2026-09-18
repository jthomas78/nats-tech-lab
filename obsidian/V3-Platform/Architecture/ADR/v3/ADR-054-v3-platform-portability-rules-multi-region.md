---
adr: 54
title: Portability Rules for Multi-Region Deployment and Integration
status: Accepted
date: 2026-09-07
scope: v3
context: platform
decision: Every managed or vendor-specific dependency sits behind a port with at least two working adapters, a region runs on plain compute, disk and network, and no global component holds a copy of regional data. Ten numbered rules govern multi-region deployment, clustering and integration, including which of three cross-border shapes a residency or sovereignty requirement selects.
why: Linebooker V3 must be liftable between clouds and into a customer's own datacentre. A lock-in decision is cheap to take and expensive to reverse, so the constraint has to be written down before the choices are made, not after.
related: [48, 53]
applied_by: []
---

# ADR-054: Portability Rules for Multi-Region Deployment and Integration

**Status:** **Accepted 2026-09-07** — ten rules, effective for all Proposed
Linebooker V3 architecture documents from L2 down.
**Date:** 2026-09-07
**Deciders:** Jeremy Thomas
**Related:** [ADR-053](ADR-053-v3-data-shared-postgres-by-default.md) (rule 4
narrows it: shared Postgres by default, and standard Postgres always);
[ADR-048](../lab/ADR-048-lab-organizations-document-storage-nats-object-store.md)
(NATS Object Store as today's blob store — rule 1 makes it the first of two
adapters, not the only one);
[Proposed-Linebooker-V3-Architecture-Authority.md](../../../../../proposed-v3-arch/Proposed-Linebooker-V3-Architecture-Authority.md)
(the L0-L4 hierarchy these rules govern);
`demos/02-multi-region/diagrams/multi-cluster-and-region/` (the drawings that
apply them)

## Context

Linebooker V3 is a multi-region, multi-tenant platform with a global control
plane. Three forces make portability a first-class constraint rather than a
preference:

1. **Regulated markets demand in-country deployment.** Botswana is the current
   example: regulated data and its transit must remain in Botswana. A design
   that assumes a hyperscaler region cannot serve a market where that
   hyperscaler has no region, or where the customer requires their own
   datacentre.
2. **A cell is meant to be repeatable.** The architecture already treats a
   region as a deployment stamp: 1..N cells, each identical, provisioned the
   same way. A stamp that only stamps onto one vendor is not a stamp.
3. **Lock-in is asymmetric.** Choosing a managed service takes one sprint;
   leaving it takes a quarter and a migration risk. The decision therefore
   has to be constrained at the point of choosing, which means the constraint
   must already exist in writing.

The consolidated companion view
(`linebooker-v3-high-level-architecture-landscape.html`) already
shows the shape this implies in two places — an `Evidence Store Adapter`
tile marked "S3-compatible option", and a Botswana cell with its own
isolated NATS and local resolver. Both were drawn as observations. Neither
was governed by a rule. This ADR supplies the rule.

It also exposes two tiles that name a vendor directly: `WorkOS / IdPs` on
the identity path and `Temporal` on the workflow path. Under the rules below
those are no longer defaults; they are decisions, and each needs its own
record.

## Decision

Nine rules. They apply to every V3 architecture document from L2 downward,
and to every deployment, clustering and integration choice made under them.

### Rule 1 — Every managed or vendor-specific dependency sits behind a port, with at least two proven adapters

One adapter is a claim of portability. Two adapters that both pass the same
test suite are a fact. The second adapter must be **runnable in CI**, not
merely written.

Applies to: object storage, secrets, mail and notification delivery, maps
and geocoding, identity, workflow orchestration, telemetry sinks.

### Rule 2 — Adapter selection is configuration, not code

The adapter is chosen per deployment by environment, resolved at startup.
This is what lets Australia run on S3 while Botswana runs on-premises MinIO
with a single build and no branch. A per-region code fork is a rule-2
violation even when it works.

### Rule 3 — A region must run on plain compute, disk and network

If a cell cannot be stood up on virtual machines with block storage and a
private network, it is not a region — it is a vendor deployment wearing a
region's name. Managed services may be *used* inside a region; a region may
not *require* one to exist.

### Rule 4 — Postgres stays standard Postgres

Managed hosting is fine and expected. A vendor-only dialect, extension or
feature on a business path is not. The test: `pg_dump` from the managed
instance must restore into a plain PostgreSQL container and the service
suite must pass against it unchanged. This narrows
[ADR-053](ADR-053-v3-data-shared-postgres-by-default.md), which settled
*how many* instances; this settles *which* Postgres.

### Rule 5 — NATS is the portability layer, so it is always self-hostable and the operator NKey is always ours

This is the strongest single reason to build on NATS rather than a cloud
message bus, and it is worth stating rather than assuming. A managed NATS
offering may be used as an operational convenience. The operator NKey — the
root of the trust chain for every account in every region — is generated and
held by Linebooker, never by a provider. A provider-held root is not a
portability problem; it is a sovereignty problem, and it is irreversible.

### Rule 6 — Kubernetes and Helm are the deployment contract

Deployment definitions are Helm charts; infrastructure is OpenTofu or
Terraform; delivery is Argo CD, one `Application` per cell namespace. Not a
vendor's own application platform. Docker Compose remains the local
development shape and is explicitly *not* the contract — it has no replica
concept, so it cannot express `R3` and will not catch a replica mistake.

### Rule 7 — Data residency beats convenience: a global component holds a pointer, never a copy

The global control plane may hold the identity, the placement and the
entitlement of a tenant. It may not hold that tenant's business data,
documents or audit trail. Where a global view is genuinely needed, it is an
aggregate computed in-region and pushed as a summary, never a replica of the
rows.

This rule decides questions that would otherwise be argued one at a time.
Two worked examples:

- **Billing splits in two.** Entitlements, subscription and metering rollup
  are per-tenant platform records and belong to the global control plane.
  Invoice and settlement between a shipper and a transporter are tenant
  money in a regulated market and stay in-region. The global side holds a
  pointer to the regional billing record.
- **Audit stays home.** An audit trail is regional and may be legally barred
  from leaving. The global plane may know that an audit record exists; it may
  not store it.

### Rule 8 — Every region keeps serving when the global layer is unreachable

Including login and token renewal. This is already achieved by
`resolver: full`: every NATS server persists its own copy of the account
JWTs and claims updates are pushed over the system account, so no region
consults the control plane to authenticate anyone. A global-layer outage must
degrade to "no new tenants can be created", never to "a region stops
working". Any new global dependency has to be checked against this rule
before it is added.

### Rule 9 — Three cross-border shapes, and the requirement picks one

There are three shapes, not two, and the deciding question is what the
requirement actually covers: **storage, or storage plus access and control.**
Confusing them is a compliance failure in one direction and pointless
operational cost in the other.

- A **leaf node** dials out to a parent cluster and, by default, shares the
  parent's operator and trust chain. Its purpose is edge survivability — an
  on-premises depot, yard or partner hub inside a region already served, that
  must keep working when the WAN drops. It is one site, not one country.
  Optional, added only on a demonstrated on-premises requirement. It is never
  the answer to a cross-border data requirement of any kind.
- A **regional cell** — the same shape as any other region — is the answer to
  a **data-residency** requirement. Own NATS cluster, own Postgres, own object
  store, joined to the platform by gateway, under the **one global operator**.
  Residency is enforced by placement: the tenant is pinned to the cell, no
  stream mirrors or sources leave it, and rule 10 holds its backups and
  telemetry in. The operator NKey and the account JWT are platform metadata —
  an account name, its limits, its permissions — not the tenant's data, so a
  storage rule does not reach them.
- A **sovereign cell** has its own operator, its own resolver, its own stores
  and no gateway. Nothing crosses the border implicitly and any integration is
  a deliberate, reviewed export. This is the answer when the requirement
  reaches **access and control**, not only storage: when no foreign party may
  be *able* to grant access to the data. A Linebooker-held operator can mint a
  credential that reads the cell, so under that reading the ability is itself
  the transfer.

Two failure modes to name, because both look correct on a drawing:

- **Attaching a regulated country as a leaf** into a foreign cluster places
  its trust root outside its borders while appearing to satisfy the
  requirement.
- **Making a residency-only country a sovereign cell** is not a compliance
  failure but it is a real and recurring cost: a second provisioning act
  instead of the single global one, refdata by export instead of push, a
  second key chain to rotate and a second resolver to run. Do not pay it
  without a control or access requirement in writing.

Botswana is residency-only on the current reading of the requirement, which
makes it a **regional cell**. This contradicts three recorded L1/L2
requirements and they have to be amended before the shape changes:

- `L1-017` and `L2-019` list **keys** among the things that stay in-country.
  Keys in-country is the sovereign reading, not the residency reading.
- `L2-020` states Botswana has **no gateway path** for regulated tenant
  accounts. A regional cell joins by gateway, so the two cannot both stand.
- `L1-016` and `L2-015` name the placement as a **sovereign** cell.

Until those are amended, the governed L1, L2 and L3-08 documents remain the
authority and continue to show the sovereign shape. `L3-08-O01` already flags
that the authoritative Botswana legal controls are unconfirmed, which is the
open item this turns on.

### Rule 10 — Backup, disaster recovery and raw telemetry inherit residency

A residency boundary that only covers the live database is not a residency
boundary. Everything derived from the data carries the same constraint:

- **Backups and dumps stay in-region.** A nightly dump to a global bucket is
  the classic way a correct deployment fails an audit.
- **Disaster recovery is in-region.** A second site inside the border, or
  none. A DR replica across the border is a copy, and rule 7 already forbids
  the global plane holding one.
- **The object-store adapter must point at an in-region endpoint.** Rule 1
  makes the store a port; this rule constrains where the chosen adapter is
  allowed to resolve to.
- **Raw telemetry and the audit trail stay in-region.** An aggregate roll-up
  may leave, per rule 7. Raw spans and audit records may not.

This rule applies to every region, not only to a regulated one. It is written
separately from rule 7 because rule 7 governs what the *global plane* holds,
and this one governs what the *region* is allowed to ship out of itself.

## Options Considered

**Option A — nine written rules, enforced at document review (chosen).** The
constraint exists before the choices, and each architecture document can be
checked against it. Cost: the rules have to be read and applied by hand.

**Option B — a portability principle in the authority document, no ADR.**
Cheaper to write. Rejected: the authority document governs *document
structure*, not technology choices, and a principle with no decision record
cannot be cited by an ADR that later departs from it.

**Option C — decide portability per component, at the point of choosing.**
This is the status quo and it is what produced two vendor-named tiles with no
recorded rationale. Rejected: it optimises each choice locally and the
platform globally loses.

**Option D — forbid managed services outright.** Simple to enforce and
genuinely portable. Rejected as too expensive: it would mean self-hosting
identity, mail and telemetry from day one for a portability benefit that
rule 1's two-adapter test already delivers at a fraction of the cost.

## Trade-off Analysis

| | Chosen (A) | Cost |
|---|---|---|
| **Speed** | Slower per component: a second adapter is real work | Accepted. The second adapter is also the local-development and on-premises adapter, so it is rarely wasted. |
| **Cost** | Higher CI cost — two adapter paths per port | Accepted, and bounded: the ports are few and the suites are shared. |
| **Depth of lock-in** | Bounded at the adapter seam | The seam itself is a design cost: a port wide enough to cover two providers is sometimes less expressive than one provider's SDK. |
| **Residency markets** | Reachable at region cost | Rule 9 answers a storage-only requirement with an ordinary regional cell, so the single global provisioning act survives. The cost is a cell per residency market. |
| **Sovereign markets** | Reachable at trust-domain cost | Where the requirement reaches access and control, rule 9 requires a separate operator and resolver — overhead that scales with countries, not tenants. Rule 9 exists partly to stop that cost being paid where only residency was asked for. |
| **Vendor features** | Deliberately unused where they are vendor-only | The known casualties are managed-Postgres extensions and provider-specific workflow features. |

## Consequences

- **Two existing choices become recorded decisions, not defaults.** `WorkOS`
  on the identity path and `Temporal` on the workflow path are vendor names
  in the architecture. Identity is the worst place to accept lock-in because
  it sits on every request. Neither is wrong; both now need an ADR under
  rule 1 naming the port and the second adapter.
- **The blob store gains a port.** The lab stores documents in NATS Object
  Store per
  [ADR-048](../lab/ADR-048-lab-organizations-document-storage-nats-object-store.md).
  Under rule 1 that becomes adapter one of two, with an S3-compatible
  adapter alongside it, and the region diagrams gain the tile the drawings
  were missing.
- **Billing is now two components, not one**, per rule 7, and they sit in
  different planes.
- **Botswana is drawn as an ordinary regional cell, pinned**, per rule 9 — on
  the residency-only reading it keeps the one global operator and the single
  provisioning act. **This is not yet consistent with the governed
  documents**: `L1-016`, `L1-017`, `L2-015`, `L2-019` and `L2-020` record the
  sovereign shape, and `L2-020` specifically forbids a gateway path. Those
  requirements are amended first, or this stays a discussion sketch. The optional edge node
  becomes a distinct, dashed, on-premises-only tile for depots and partner
  hubs, and the sovereign shape stays documented for the market that asks for
  it.
- **Residency has a checklist, not a diagram.** Rule 10 puts backups, DR, the
  object-store endpoint and raw telemetry inside the same border as the
  database. These are the parts a drawing does not show and an audit does.
- **`R3` is a rule-6 casualty of local development.** Compose cannot express
  replicas, so a local stack cannot prove the Helm charts are right. The
  charts have to be validated against a real Kubernetes API — `kind` is
  enough — or the replica gap stays invisible until production.
- **These rules govern; they do not implement.** No lab code changes because
  of this ADR. A lab ADR that applies one of these rules records it with a
  `**Governed by:**` line and adds its number to this ADR's `applied_by`.

## Action Items

- [ ] Record an ADR for the identity provider choice, naming the port and the
      second adapter (rule 1). Highest priority of the three — identity is on
      every request path.
- [ ] Record an ADR for the workflow engine choice on the same terms
      (rule 1).
- [ ] Define the object-store port and add the S3-compatible adapter
      alongside the NATS Object Store one (rule 1), amending ADR-048.
- [ ] Split billing into an entitlement component (global) and an
      invoice/settlement component (regional) in the L3 documents (rule 7).
- [ ] Add the `pg_dump` restore-into-plain-Postgres check to CI (rule 4).
- [ ] Validate the Helm charts against `kind` so the `R3` gap is caught
      outside production (rule 6).
- [ ] Confirm in writing, per residency market, whether the requirement covers
      storage only or also access and control — the rule-9 shape follows from
      that answer and from nothing else.
- [ ] Add a residency check to the backup, DR and telemetry paths so a
      cross-border destination fails rather than succeeds quietly (rule 10).
