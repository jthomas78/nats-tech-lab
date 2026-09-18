---
title: L2 Technology Selection and Rationale
aside: false
---

# Technology Selection and Rationale

<div class="v3-meta">
<strong>LB-V3-L2-02</strong> &middot; Level L2 &middot; Parent: <a href="/v3-architecture/l1-01-system-platform-overview">LB-V3-L1-01 System and Platform Overview</a> &middot;
Requirements register: <code>Proposed-Linebooker-V3-L2-02-Requirements.md</code> &middot;
Catalogue status: DRAFT
</div>

::: warning PROPOSED, NOT BUILT
This page describes a **proposed** architecture for a future Linebooker V3
platform. Nothing here is built. The rest of this site documents the Dictionary
proof of concept, which is real code you can run. Do not read the two as one
system.
:::

::: danger DIRECTIONAL, NOT A SETTLED DECISION — as at 3 September 2026
This document is a first-pass technology position, not a record of decisions
made. The decision of record for any single selection is its architecture
decision record (ADR); where none exists yet, the card says so in amber and
that gap is a to-do, not a defect. Re-read the date before relying on this page
— a directional position with no date cannot be trusted a year from now.
:::

## The question this view answers

Document `LB-V3-L2-02` answers one question:

**Which technologies were selected for the proposed platform, and why?**

Its sibling [LB-V3-L2-01](/v3-architecture/l2-01-logical-technical-architecture)
answers a different one — how the platform would be constructed. Both sit at
L2 because they ask different questions about the same system, not because
they cover different areas. This document deliberately draws no relationships
between technologies; `LB-V3-L2-01` owns how the parts fit together. Every card
here instead answers: what problem does this solve, what features does the
platform actually rely on, what was rejected instead, and which `LB-V3-L2-01`
elements depend on it.

## The figures

This document is two sheets. The first is technology the platform is built on.
The second is services the platform buys or integrates with.

<figure class="v3-figure">
<a class="v3-figure-zoom" href="/v3-architecture/lb-v3-l2-02-sheet1.png" target="_blank" rel="noopener">
<img src="/v3-architecture/lb-v3-l2-02-sheet1.png" alt="LB-V3-L2-02 sheet 1, Technology Selection and Rationale">
</a>
<figcaption><strong>LB-V3-L2-02</strong> sheet 1 of 2 — Technology Selection and Rationale.
<a href="/v3-architecture/lb-v3-l2-02-sheet1.png" target="_blank" rel="noopener">Open full size</a> &middot;
<a href="/v3-architecture/lb-v3-l2-02.pdf" target="_blank" rel="noopener">Print edition (PDF)</a>
</figcaption>
</figure>

<figure class="v3-figure">
<a class="v3-figure-zoom" href="/v3-architecture/lb-v3-l2-02-sheet2.png" target="_blank" rel="noopener">
<img src="/v3-architecture/lb-v3-l2-02-sheet2.png" alt="LB-V3-L2-02 sheet 2, External Services and Integrations">
</a>
<figcaption><strong>LB-V3-L2-02</strong> sheet 2 of 2 — External Services and Integrations.
<a href="/v3-architecture/lb-v3-l2-02-sheet2.png" target="_blank" rel="noopener">Open full size</a> &middot;
<a href="/v3-architecture/lb-v3-l2-02.pdf" target="_blank" rel="noopener">Print edition (PDF)</a>
</figcaption>
</figure>

## How to read this view

Both sheets carry their own key. In words:

- **This is an inventory, not a topology.** No line is drawn between cards.
  `LB-V3-L2-01` draws the relationships; this document only lists what and why.
- **A card's left edge is coloured by the plane it serves, using the same
  colour meanings as `LB-V3-L2-01`.** Blue is the experience and entry plane,
  purple is the domain service plane, cyan is platform foundations, green is a
  cross-cutting platform concern, and amber (sheet 2 only) marks an external or
  separately governed service.
- **The status chip describes the selection, not the document.** `Chosen` is
  accepted for V3 with an ADR of record. `Proposed` is a directional
  position with no V3 ADR yet. `Proven in lab` was exercised in the Dictionary
  POC but is not yet accepted for V3. `Open` marks a genuinely undecided
  question, drawn on purpose rather than left off the sheet.
- **Five fields answer the same five questions on every card**: what it
  *realises* (the role, stated before the product), what problem it *solves*,
  what features it actually *relies on*, what it stands *instead of*, the
  *decision* record if one exists, and which `LB-V3-L2-01` elements are
  *used by* it.
- **Amber inline text is a work list, not a footnote.** `no V3 ADR yet` marks a
  selection the platform already leans on without a written decision behind
  it. On sheet 2, `carried from V2` marks a dependency that is running
  operational reality, not a fresh V3 choice.
- **No cloud vendor appears anywhere on this document.** Object storage is
  named by its API contract (S3-compatible), not a product, and the cloud
  provider itself is drawn as an open card.

## The architecture

### Sheet 1 — platform selections

Sixteen cards, grouped by the same four planes `LB-V3-L2-01` uses.

**Experience and entry (blue).** Vue 3 with PrimeVue and Module Federation are
both `Proven in lab`, carrying the Dictionary POC's micro-frontend shell
forward as a proposal. WorkOS (or an equivalent enterprise identity provider)
and OpenFGA are both `Proposed`: WorkOS for authentication and single sign-on
federation, OpenFGA for relationship-based authorisation — who may act on
which organisation, depot or load, a question V2's role enums could not
express. Neither has a V3 ADR; WorkOS additionally has an outstanding
commercial selection.

**Domain service plane (purple).** Go is `Proven in lab` as the service
implementation language, chosen in the lab for fast-starting, cheap-to-run
service replicas with first-class NATS and Temporal SDKs.

**Platform foundations (cyan).** NATS Core, NATS JetStream, NATS KV and
Temporal are all `Proven in lab` — exercised in the Dictionary POC at demo
scale, under one operator, with no regulated data, but none yet has a V3 ADR.
PostgreSQL is the one `Chosen` card on the sheet, backed by `ADR-052` and the
V3-scoped `ADR-053`. S3-compatible object storage is `Proposed`: the lab chose
NATS Object Store instead (`ADR-048`), so this is a deliberate V3 divergence,
not a continuation, and it has no V3 ADR of its own yet. Container platform
and cloud provider are both drawn `Open` — genuinely undecided, not
overlooked; the lab runs Docker Compose on one host and assumes only the S3
and OpenTelemetry protocol (OTLP) API contracts are portable.

**Cross-cutting platform concerns (green).** OpenTelemetry/OTLP and ULID are
both `Proven in lab`. ULID additionally carries a lab-scoped decision record,
`ADR-051`, that does not by itself accept it for V3. The build and release
pipeline is drawn `Open`; the lab builds directly with Vite and Go toolchains,
where V2 runs fifteen Google Cloud Build pipelines plus GitHub Actions.

Only one selection on this sheet — PostgreSQL — is `Chosen` with a V3-scoped
ADR. Thirteen of the sixteen cards carry no V3 ADR at all.

### Sheet 2 — external services and integrations

Sixteen cards read out of the V2 codebase as an observed inventory, not a
wish list. Every card is `Proposed`, and every card but the last is marked
`carried from V2` — running operational reality that has not been re-tendered
or reassessed for V3. Grouped by role rather than by plane:

- **Finance and commercial** — Acumatica (accounting and finance system of
  record) and monday.com (commercial pipeline and account boards).
- **Communication** — the Meta WhatsApp Cloud API (the load-bearing channel to
  drivers and transporters), SendGrid (transactional email) and Twilio Verify
  (SMS/voice second factor, which overlaps WorkOS multi-factor authentication
  — one of the two should go).
- **Payments** — Peach Payments (card acceptance) and Ozow (instant bank
  transfer), both South-Africa-only and not portable to an Australian cell.
- **Location and tracking** — a telematics broker normalising around
  thirty-eight vendor feeds (carried forward as Linetracker, V3 shape not yet
  designed), Google Maps Platform (geocoding, distance, routing) and
  MapLibre GL with MapTiler (map rendering, split from Google's per-view cost).
- **Documents and analytics** — Google Document AI (extraction from scanned
  proof of delivery — the strongest single cloud lock-in on the sheet),
  BigQuery with Looker Studio (analytics warehouse, never a source of truth),
  customer file drops (contracted secure file transfer for enterprise
  shippers) and voucher rewards (a single, region-specific retailer).
- **Trust** — reCAPTCHA, a candidate to fold into the network edge.
- **Not carried into V3** — a closing card naming what is deliberately left
  behind: the JHipster scaffolding, Spring Boot 2.1, MySQL, RabbitMQ, a Redis
  cache, Elasticsearch, Logstash, Dropwizard metrics, React 17 with Redux and
  Material UI, CASL, jOOQ and Liquibase. Each has a V3 replacement on sheet 1,
  or is simply dropped.

Four services are South-Africa-only and would need a regional equivalent, or a
deliberate gap, for an Australian cell: card payments, bank transfer, the
voucher retailer and most telematics vendors.

## Why it is shaped this way

- **The document is directional by requirement** (`L2-02-001`): the header
  carries a date stamp, and every reader must treat the position as
  provisional until an ADR replaces it.
- **Every card names what was rejected** (`L2-02-006`), so a reader can tell a
  choice was actually made rather than defaulted into.
- **The decision field never hides a missing ADR** (`L2-02-008`) — a card with
  no V3 architecture decision record says so in amber rather than looking
  settled.
- **A lab result is not a V3 acceptance** (`L2-02-011`). `Proven in lab` exists
  as its own status precisely so exercising something in the Dictionary POC is
  never mistaken for choosing it for V3.
- **No cloud vendor is branded or selected** (`L2-02-012`), consistent with the
  authority's modelling invariant that the architecture may be AWS-shaped
  without naming a provider until that decision is confirmed.
- **Sheet 2 exists because bought and built are different risk profiles**
  (`L2-02-019`): a vendor owns the roadmap and the outage for anything on that
  sheet, and every card there was read directly out of the running V2
  codebase (`L2-02-020`), not proposed fresh.
- **The final sheet-2 card records the negative space** (`L2-02-022`) — an
  inventory that only lists what is kept hides the size of the change; what is
  dropped needs a record too.

## What this view deliberately excludes

- Version pinning, sizing, high-availability topology and operational runbooks
  → an L3/L4 concern view (`L2-02-016`).
- Per-technology configuration, message subjects, schemas and deployment
  specifications → L4 detailed designs (`L2-02-017`).
- The full argument for any single selection → the ADR named on that card
  (`L2-02-018`).
- Endpoints, credentials, keys and account identifiers for any listed service
  → deliberately absent from both sheets (`L2-02-025`).
- Adapter shape, retry and idempotency rules, and per-vendor integration
  contracts → `LB-V3-L3-04 External Integration`, once drawn.

## Open confirmations

Carried from the requirements register, unresolved:

- **`L2-02-014`** — Confirm the V3 messaging, workflow and data selections
  through V3-scoped ADRs. Thirteen of the sixteen sheet 1 cards currently show
  `no V3 ADR yet`.
- **`L2-02-015`** — Confirm the identity provider commercially before treating
  WorkOS as chosen.
- **`L2-02-026`** — Re-tender or confirm each inherited external service for
  V3, and resolve the two duplicate second-factor services (WorkOS
  multi-factor and Twilio Verify).
- Only `ADR-053` is V3-scoped today. `ADR-046` through `ADR-052` are lab-scoped
  and do not by themselves accept a technology for V3.
- The container platform, cloud provider and build/release pipeline are
  genuinely undecided, not oversights.
- No external service on sheet 2 has been re-tendered for V3. Card payments,
  bank transfer, the voucher retailer and most telematics vendors need a
  regional equivalent for an Australian cell.

## Related documents

- Parent: [LB-V3-L1-01 · System and Platform Overview](/v3-architecture/l1-01-system-platform-overview)
- Sibling: [LB-V3-L2-01 · Logical and Technical Architecture](/v3-architecture/l2-01-logical-technical-architecture)
- Atlas: [LB-V3-L0-01 · Architecture Atlas](/v3-architecture/l0-01-architecture-atlas)
