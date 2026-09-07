---
title: L0 Architecture Atlas
aside: false
---

# Architecture Atlas

<div class="v3-meta">
<strong>LB-V3-L0-01</strong> &middot; Level L0 &middot; Parent: Architecture Operational Authority &middot;
Requirements register: this authority document &middot;
Catalogue status: AVAILABLE
</div>

::: warning PROPOSED, NOT BUILT
This page describes a **proposed** architecture for a future Linebooker V3
platform. Nothing here is built. The rest of this site documents the Dictionary
proof of concept, which is real code you can run. Do not read the two as one
system.
:::

## The question this view answers

Document `LB-V3-L0-01` answers one question:

**Where do I find the smallest diagram that answers my question?**

This page is not a technical architecture view. It is a graphical table of
contents and a document-status map for the whole Proposed Linebooker V3
series. Read it to choose a branch, then leave it — the substance lives on the
page you branch to.

## The figure

<figure class="v3-figure">
<a class="v3-figure-zoom" href="/v3-architecture/lb-v3-l0-01.png" target="_blank" rel="noopener">
<img src="/v3-architecture/lb-v3-l0-01.png" alt="LB-V3-L0-01 Architecture Atlas">
</a>
<figcaption><strong>LB-V3-L0-01</strong> — Architecture Atlas.
<a href="/v3-architecture/lb-v3-l0-01.png" target="_blank" rel="noopener">Open full size</a> &middot;
<a href="/v3-architecture/lb-v3-l0-01.pdf" target="_blank" rel="noopener">Print edition (PDF)</a>
</figcaption>
</figure>

## How to read this view

- **Levels increase detail going down the page.** L1 establishes the
  stakeholder landscape, L2 explains the logical construction, L3 isolates one
  architectural concern at a time, and L4 records implementable designs.
  Follow the tree only as far as the question at hand needs — stop before
  detail the question does not require.
- **Card colour and border are the status, not the subject.** A solid blue
  card is `AVAILABLE`: its drawn and print editions exist and passed
  validation. A solid amber card is `DRAFT`: reviewable, not yet accepted. A
  dashed grey card is `PLANNED`: catalogued as a future document, with nothing
  drawn yet. The atlas itself is drawn as a green card, marking it as the
  navigation root rather than a concern view.
- **A solid blue or amber card is a link.** In the HTML edition, selecting one
  opens that document directly. A dashed grey card has nothing to open — it
  names a reserved place in the catalogue.
- **The stable ID is the only safe way to refer to a document** —
  `LB-V3-L<level>-<nn>`, for example `LB-V3-L3-05`. IDs are never renumbered
  or reused, even if a document is later superseded, so an ID always points to
  the same architectural question.
- **A coloured band groups one level.** The blue band is L1, the green band is
  L2, the purple band is L3, the cyan band is L4. Banding exists so a reader
  can see at a glance how much of each level is drawn versus still planned.
- **The arrows describe two different relationships.** The solid blue arrow
  leaving the atlas is the recommended entry route into the set — start with
  L1. The dashed grey arrows below it mean "zoom": a child adds detail to its
  parent's question, it does not repeat it.
- **No superseded state is shown yet**, because no document in the series has
  been replaced. The legend reserves a treatment for that state so the atlas
  will not need to change shape the first time it happens.

## The architecture

The atlas root, `LB-V3-L0-01` itself, sits at the top and points to exactly
one L1 document.

**L1 — stakeholder landscape and architecture entry point.** One document,
`LB-V3-L1-01 System and Platform Overview`, `AVAILABLE`. This is the
recommended starting point for any reader new to the series: participants,
experiences, capabilities, platform and regions, in plain terms, before any
technology is named.

**L2 — logical and technical architecture.** Two documents branch from L1,
distinguished by question rather than by concern:

- `LB-V3-L2-01 Logical and Technical Architecture`, `AVAILABLE`, two sheets —
  how the platform is constructed: services, messaging, data, deployment.
- `LB-V3-L2-02 Technology Selection and Rationale`, `DRAFT`, two sheets —
  which technologies are selected, and why.

**L3 — focused architecture views.** Twelve concern views branch from
`LB-V3-L2-01`, each isolating one architectural concern so it can be read
without the others. Two are drawn today:

- `LB-V3-L3-05 Messaging / NATS`, `AVAILABLE` — trust, accounts and topology.
- `LB-V3-L3-08 Multi-Region`, `AVAILABLE` — cells, residency and recovery.

The remaining ten are catalogued and `PLANNED`, reserved places for concern
views not yet drawn: Participant + Tenancy, Functional Domains, Application /
MFE (micro-frontend), External Integration, Workflow / Temporal, Data
Architecture, Security + Identity, Observability, Financial Architecture, and
Deployment + Services.

**L4 — detailed designs.** Six implementable-design groups are catalogued
under L3, all `PLANNED`: NATS contracts, Workflows, Data schemas, APIs +
adapters, Deployment, and Permissions. None is drawn yet.

## Why it is shaped this way

- **One document, one question, at every level.** The atlas exists so a new
  diagram can be registered without turning L0 into a mega-diagram — a reader
  should never need to read the whole tree to answer one question.
- **L2 branches by question, not by concern.** `LB-V3-L2-01` and
  `LB-V3-L2-02` sit side by side because "how is it constructed" and "which
  technologies and why" are different questions about the same system.
  Splitting by concern is deliberately left to L3.
- **Status is visual, not just tabular.** The catalogue table in the authority
  document is the source of truth, but the atlas gives every status a shape
  and colour so drift between "catalogued" and "actually drawn" is visible on
  sight, not only on a careful table read.
- **Stable IDs never renumber**, so a document can be cited, linked or
  superseded without breaking every reference that came before it.

## What this view deliberately excludes

The atlas shows no system design of its own: no service, no data store, no
message subject, no deployment mechanic. Anything at that depth belongs on the
document a card links to, not on this page. Specifically:

- Participants, capabilities and platform shape → `LB-V3-L1-01`.
- Service, messaging, data and deployment construction → `LB-V3-L2-01`.
- Technology selection and rationale → `LB-V3-L2-02`.
- Any single concern in depth (messaging, regions, security, and so on) → the
  matching `LB-V3-L3-*` document.
- Contracts, subjects, schemas and permissions → the matching `LB-V3-L4-*`
  document, once drawn.

## Open confirmations

None. The atlas is a navigation index over the catalogue; it carries no
architectural claim of its own to confirm. Open confirmations belong to the
documents the atlas links to — see each document's own requirements register.

## Related documents

- Child: [L1-01 · System and Platform Overview](/v3-architecture/l1-system-platform-overview)
- Also reachable from L1: [L2-01 · Logical and Technical Architecture](/v3-architecture/l2-logical-technical-architecture)
