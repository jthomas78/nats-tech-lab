---
adr: 56
title: Five Independent Axes — Region, Tenant, Account, Cell, JetStream Domain
status: Accepted
date: 2026-09-18
scope: v3
context: platform
decision: Region, tenant, account, cell and JetStream domain are five independent axes, each answering a different question, and no two of them may be defined as permanently equal. The bare word "domain" is reserved for the NATS JetStream sense; the DNS sense is renamed "market host" and the DDD sense is always written "business domain".
why: A team review collapsed placement, ownership, authority, deployment and storage into one nested hierarchy. Collapsing any two of them removes the ability to move one without the other, and the word "domain" already carries three unrelated meanings in the same documents.
related: [54]
applied_by: []
---

# ADR-056: Five Independent Axes — Region, Tenant, Account, Cell, JetStream Domain

**Status:** **Accepted 2026-09-18** — binding vocabulary for all Proposed
Linebooker V3 architecture documents and for all lab work that cites them.
**Date:** 2026-09-18
**Deciders:** Jeremy Thomas
**Related:** [ADR-054](ADR-054-v3-platform-portability-rules-multi-region.md)
(rule set that already treats a cell as a repeatable stamp);
[Proposed-Linebooker-V3-Architecture-Authority.md](../../../../../proposed-v3-arch/Proposed-Linebooker-V3-Architecture-Authority.md)
(the L0-L4 hierarchy this vocabulary governs);
[Proposed-Linebooker-V3-L3-01-Requirements.md](../../../../../proposed-v3-arch/Proposed-Linebooker-V3-L3-01-Requirements.md)
(`LB-V3-L3-01 Participant + Tenancy`, whose open item `L3-01-O06` asked for
this record);
`demos/03-multi-cluster-and-accounts/REPORT.md` (the measured NATS behaviour
behind rule 3)

## Context

A team review of `LB-V3-L3-01` treated tenancy and placement as one nested
hierarchy — an operator containing accounts containing regions containing
cells. That reading is wrong, and the drawing was changed to show the two
axes crossing rather than nesting.

The review exposed a larger problem. The platform uses five words for five
genuinely different questions, and the documents had never said which
question each word answers:

| Word | Question it answers |
|---|---|
| Region | **Where** does it run? |
| Tenant | **Whose** is it? |
| Account | **What** may it touch? |
| Cell | **What ships together?** |
| JetStream domain | **Which storage pool holds the log?** |

Only one of the five is enforced by a machine. NATS itself enforces the
account wall; `demos/03-multi-cluster-and-accounts` measured it across seven
topologies and 73 checks. Region, tenant and cell are platform words made
real by naming, configuration and deployment. Nothing in NATS will stop an
engineer breaking them.

Two failure modes follow from leaving this unwritten.

**The collapse.** "One tenant = one account = one cell" is tidy on day one.
On day two a large tenant needs a dedicated cell and a small tenant should
share one. The rule is then broken in code rather than revised in a document,
and the mapping becomes unrecoverable.

**The word collision.** "Domain" already means three unrelated things in the
same sentence in current material:

1. **Site domain** — a DNS name: `linebooker.co.za`, `linebooker.au`.
2. **JetStream domain** — a named, isolated JetStream storage pool, addressed
   as `$JS.<domain>.API.>`, needed wherever a leaf node runs its own
   JetStream alongside a hub's.
3. **Business domain** — the domain-driven-design sense: booking, invoicing,
   reference data.

`LB-V3-L3-01` counts 26 occurrences of "domain" on one drawing, carrying at
least two of these senses. A reader cannot resolve them from context.

## Decision

Six rules. They bind every V3 architecture document from L0 down, every ADR
citing them, and all lab work that references V3 vocabulary.

### Rule 1 — Each of the five words answers exactly one question

The table above is the definition. A document that uses one of these words to
answer a different question is wrong and must be corrected, not annotated.

- **Region** is *placement*. A geography. It carries no identity and no
  authority.
- **Tenant** is *business ownership*. A customer of the platform. It is a
  commercial fact, not a runtime object.
- **Account** is *authority*. A NATS account: the hard messaging boundary,
  the only one of the five the server enforces.
- **Cell** is *deployment*. One region's complete, repeatable runtime stamp
  (ADR-054 rule 2). It is a place, not a tenant.
- **JetStream domain** is *storage identity*. It names one pool of stream and
  key-value storage so a client can address the right one on purpose.

### Rule 2 — No two axes may be defined as permanently equal

Any mapping between two axes is a **choice recorded per tenant**, never a law
written into the architecture. Specifically:

- A tenant **may** own more than one account (for example production and
  test). One tenant is not defined as one account.
- An account **may** exist in many cells at once, keeping one identity in
  each (`L3-01-014`).
- A cell **may** hold many accounts (`L3-01-011`).
- A cell **may** be given its own JetStream domain, but a cell is not defined
  as a JetStream domain.

A design that needs two axes to move together states that coupling as an
explicit, dated constraint with an owner. It does not achieve it by
redefining a word.

### Rule 3 — The account is the only enforced boundary; the other four are conventions

Region, tenant, cell and site placement are made real by naming, configuration
and deployment discipline. When a document claims isolation, it must say
which mechanism provides it. If the answer is not "a NATS account", the
isolation is a convention and must be labelled as one.

This rule is evidence-backed: demo 03 measured that an account is a wall for
data, that an export-and-import pair is the only path between two accounts,
that the path is one-directional and read-only, and that an account is not a
wall for availability.

### Rule 4 — The bare word "domain" means JetStream domain, and nothing else

Anywhere in V3 material — prose, drawings, code comments, subject names,
configuration keys — an unqualified "domain" is the NATS JetStream sense.

### Rule 5 — The DNS sense is renamed "market host"

`linebooker.co.za` and `linebooker.au` are **market hosts**. "Site domain" is
retired as a term. The rename is preferred over a qualifier because it removes
the collision at its source rather than policing it in every review.

Where a document needs the market itself rather than its hostname, the word is
**market**. A market is not a region: two markets may be served from one
region, and one market may be served from two.

### Rule 6 — The DDD sense is always written "business domain"

Never shortened. `LB-V3-L3-02 Functional Domains` keeps its catalogued title,
but its body writes "business domain" in full.

## Options Considered

**A. Leave the vocabulary implicit.** Rejected. The review that prompted
`LB-V3-L3-01` proved the words are read as a hierarchy when nothing says they
are not. The cost of the misreading is a drawing that has to be rebuilt.

**B. Define the five axes, but qualify "domain" in place** — always write
"site domain" and "JetStream domain", never rename. Rejected. It requires
every author and every reviewer to catch a missing qualifier forever. Two of
the three senses are frequent enough that the qualifier will be dropped.

**C. Define the five axes and rename the DNS sense to "market host".**
Chosen. One rename, done once, leaves the bare word unambiguous. The cost is
a sweep of existing material; the term is young enough that the sweep is
small.

**D. Collapse the axes into a smaller model** — for example define a cell as
a region and drop one word. Rejected. Demo 03's measurements depend on the
distinction: a stream lands where the client is unless a cluster is named
(`L3-01-030`), which is a cell-level statement, not a region-level one.

## Trade-off Analysis

| Force | Effect of this decision |
|---|---|
| Review speed | Improves. A reviewer checks one table instead of inferring intent. |
| Document churn | One-off cost: existing "site domain" usages need renaming. |
| Flexibility | Preserved. Rule 2 keeps every axis independently movable. |
| Risk of over-specification | Low. The rules define words, not designs. No topology, account scheme or placement policy is decided here. |
| What it does not settle | The actual tenant-to-account and tenant-to-cell mappings. Those stay open (`L3-01-O01` to `L3-01-O05`, `L3-01-O07`, `L3-01-O08`). |

## Consequences

**Positive.** `LB-V3-L3-01` open item `L3-01-O06` is partly closed: a
V3-scoped decision of record now exists for participant and tenancy
vocabulary. Later L3 documents — `L3-02 Functional Domains`, `L3-08
Multi-Region`, `L3-09 Security + Identity`, `L3-12 Deployment + Services` —
inherit the five axes instead of re-deriving them.

**Negative.** Existing material that says "site domain" is now wrong and must
be swept. Any drawing using a bare "domain" for the DNS sense must be
re-exported.

**Neutral.** This ADR governs words. It takes no position on how many cells a
region holds, how many accounts a tenant owns, or whether a service replica
is shared across accounts. Those remain open.

## Action Items

| # | Action | Owner |
|---|---|---|
| 1 | **Done 2026-09-18.** Companion figure drawn: `proposed-v3-arch/drawings/adr-056-five-axes-vocabulary.html`, exported to `output/pdf/ADR-056 - Five Independent Axes.pdf`. | Architecture |
| 2 | Sweep V3 material for "site domain" and for a bare "domain" meaning DNS; rename to "market host". | Architecture |
| 3 | Narrow `L3-01-O06` to the mappings that remain open, now that vocabulary has a record. | Architecture |
| 4 | Cite this ADR from `LB-V3-L3-01`, `LB-V3-L3-05` and `LB-V3-L3-08` requirement registers. | Architecture |
