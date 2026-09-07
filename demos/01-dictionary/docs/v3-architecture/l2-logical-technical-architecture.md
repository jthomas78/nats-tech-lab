---
title: L2 Logical and Technical Architecture
aside: false
---

# Logical and Technical Architecture

<div class="v3-meta">
<strong>LB-V3-L2-01</strong> &middot; Level L2 &middot; Parent: <a href="/v3-architecture/l1-system-platform-overview">LB-V3-L1-01 System and Platform Overview</a> &middot;
Requirements register: <code>Proposed-Linebooker-V3-L2-Requirements.md</code> &middot;
Catalogue status: AVAILABLE
</div>

::: warning PROPOSED, NOT BUILT
This page describes a **proposed** architecture for a future Linebooker V3
platform. Nothing here is built. The rest of this site documents the Dictionary
proof of concept, which is real code you can run. Do not read the two as one
system.
:::

## The question this view answers

Document `LB-V3-L2-01` answers one question:

**How would the platform be constructed?**

Its sibling `LB-V3-L2-02` answers a different one — which technologies were
selected, and why. Both sit at L2 because they ask different questions, not
because they cover different areas. Splitting L2 up by concern is not allowed;
that is what L3 is for.

The parent view, [LB-V3-L1-01](/v3-architecture/l1-system-platform-overview),
established *who* participates and *where* the platform runs. This view keeps
those boundaries and adds the construction behind them: which elements exist,
which one owns what, and what runs in which place.

## The figures

This document is two sheets. The first is the logical model. The second applies
that model to real geography.

<figure class="v3-figure">
<a class="v3-figure-zoom" href="/v3-architecture/lb-v3-l2-01-sheet1.png" target="_blank" rel="noopener">
<img src="/v3-architecture/lb-v3-l2-01-sheet1.png" alt="LB-V3-L2-01 sheet 1, Logical Architecture and Service Model">
</a>
<figcaption><strong>LB-V3-L2-01</strong> sheet 1 of 2 — Logical Architecture and Service Model.
<a href="/v3-architecture/lb-v3-l2-01-sheet1.png" target="_blank" rel="noopener">Open full size</a> &middot;
<a href="/v3-architecture/lb-v3-l2-01.pdf" target="_blank" rel="noopener">Print edition (PDF)</a>
</figcaption>
</figure>

<figure class="v3-figure">
<a class="v3-figure-zoom" href="/v3-architecture/lb-v3-l2-01-sheet2.png" target="_blank" rel="noopener">
<img src="/v3-architecture/lb-v3-l2-01-sheet2.png" alt="LB-V3-L2-01 sheet 2, Regional Deployment Model">
</a>
<figcaption><strong>LB-V3-L2-01</strong> sheet 2 of 2 — Regional Deployment Model.
<a href="/v3-architecture/lb-v3-l2-01-sheet2.png" target="_blank" rel="noopener">Open full size</a> &middot;
<a href="/v3-architecture/lb-v3-l2-01.pdf" target="_blank" rel="noopener">Print edition (PDF)</a>
</figcaption>
</figure>

## How to read this view

Both sheets carry their own key. In words:

**The coloured boundary tells you what kind of area you are looking at**, not
which product is inside it.

- **Blue boundary** — edge and entry. Where traffic arrives.
- **Green boundary** — authority. The trust chain and the control plane.
- **Purple boundary** — the domain service plane, where business behaviour lives.
- **Cyan boundary** — platform foundations on sheet 1, and a shared deployment
  cell on sheet 2.
- **Amber boundary** — something external, or something governed separately. On
  sheet 2 it marks the sovereign cell, which is restricted by law.

**A tile is one logical element Linebooker owns.** An amber-filled tile is a
policy or trust artefact rather than a running component.

**Every line is one-way and its arrowhead points at the receiver.**

- **Solid blue** — a synchronous request and reply.
- **Dashed amber** — an asynchronous event, or a contracted feed.
- **Solid green** — authority: policy, claims or credentials being issued.
- **Dotted cyan** (sheet 2) — a NATS gateway, for approved accounts and subjects
  only.
- **Dashed red** (sheet 2) — a **prohibited** path. It is drawn precisely to show
  that it is barred.

On sheet 2, **nesting carries meaning**: an element drawn inside a cell runs in
that cell.

Acronyms used on the sheets, expanded: **ABAC** attribute-based access control,
**API** application programming interface, **BFF** backend for frontend, **ERP**
enterprise resource planning, **ETA** estimated time of arrival, **HA** high
availability, **IdP** identity provider, **JS** JetStream (the NATS persistence
layer, not JavaScript), **JWT** JSON Web Token, **KV** key/value store, **MFE**
micro-frontend, **POD** proof of delivery (not a Kubernetes pod), **RBAC**
role-based access control, **RPC** remote procedure call, **RPO** recovery point
objective, **RTO** recovery time objective, **S3** the object-storage API, **TLS**
Transport Layer Security, **WAF** web application firewall. Country and currency
codes: **ZA**/**ZAR** South Africa, **AU**/**AUD** Australia, **BW**/**BWP**
Botswana.

Two names are not abbreviations. **NATS** is a product name. **WorkOS** is a
commercial identity provider, named on the sheet as an example rather than as a
committed selection.

## The architecture — sheet 1, the logical model

Sheet 1 reads top to bottom. Traffic and trust enter at the top, pass a boundary,
meet authority, reach the services that own the business, and rest on shared
foundations.

### 1. Experience channels

Four ways in, each carrying a role and an organisation context:

- **App Shell + MFE** — the shipper and transporter workspaces.
- **Operator + tenant admin** — control tower, finance and policy screens.
- **Driver mobile** — tasks, tracking and evidence capture.
- **Partner API consumers** — integrations and report extraction.

A channel is not a permission. What a channel may do is decided further down.

### 2. External dependencies

Two groups, both under contract rather than under Linebooker's control:

- **Identity federation** — WorkOS and enterprise identity providers.
- **Operational and enterprise systems** — telematics, ERP, maps, payments,
  notifications and regulatory services.

These reach the platform as **contracted APIs and events**, drawn as a dashed
amber line, never as a direct database connection.

### 3. NATS trust

The messaging trust chain, drawn as four steps: **operator** (the trust root) →
**tenant account** (one per market) → **organisation roles** (RBAC and ABAC) →
**short-lived credentials** for users and workloads.

Signed tenant and organisation context flows from here down into the boundary, on
a green authority line.

### 4. Application and integration boundary

Everything entering the platform passes five elements:

- **Regional edge + WAF** — TLS, routing and rate limits, under country endpoint
  policy.
- **Identity + policy broker** — resolves membership, RBAC and ABAC into a tenant
  and organisation context.
- **BFF + API facade** — role-shaped commands and reads. The tile states the rule
  plainly: **no direct database access**.
- **Reporting + data API** — scoped extracts and asynchronous export, with masking
  and audit.
- **Integration adapters** — webhooks, polling and events, acting as an
  anti-corruption boundary so an external system's model does not leak inwards.

### 5. Control plane

Labelled on the sheet as the **Linebooker Tech platform authority** — the arm the
parent view identified as providing and operating the platform. Six capabilities:

- **Tenant + organisation** — lifecycle, membership and authority boundaries.
- **Identity + membership** — roles and policy attributes; Linebooker's own
  authorization, as distinct from proving who someone is.
- **NATS provisioning** — accounts, credentials, claims and placement.
- **Reference + config** — country and market policy, including currency defaults.
- **Entitlements + admin** — plans, billing, support and operator controls.
- **Observe + audit** — health and an immutable record, on masked regional
  telemetry.

The control plane holds **shared authority**. It does not hold a tenant's business
records.

### 6. Domain service plane

Six independently deployable services, each named for the business it owns and
each stating what state it owns:

| Service | Owns |
|---|---|
| Marketplace + tender | Demand, bids, awards — tender state |
| Loads + trips + routes | Allocation and execution — movement state |
| Fleet + tracking | Assets, drivers, ETA — capacity and position |
| Documents | Evidence and compliance — metadata and policy |
| Disputes + acceptance | Proof gates and exceptions — dispute state |
| Billing + settlement | Rating, invoice, payment — commercial state |

Replicas may scale independently, but a service's rules, its transactional state
and its published contracts stay with the service.

### 7. Platform foundations

Five foundations, and the sheet is deliberate that **they are not
interchangeable**:

- **NATS Core + JetStream** — request/reply and commands, plus durable domain
  facts. The tenant account is the boundary.
- **Temporal workflows** — long-running coordination. The tile is explicit that
  services retain the business rules.
- **Service-owned Postgres** — the transactional source of truth, a database per
  service owner.
- **Projections + NATS KV** — read models and fast lookup: derived, rebuildable
  state.
- **Document store adapter** — an S3-compatible evidence store, keeping large
  files outside NATS.

### 8. Security, residency and immutable audit

Drawn as a policy band across the foot of the sheet, because it applies to every
layer above it rather than to one of them: tenant separation, RBAC and ABAC, TLS,
encryption at rest, masked logs and country policy.

Its qualifying line matters, and is easy to skim past: the platform would keep a
**durable business history without requiring universal event-sourced
reconstruction**.

## The architecture — sheet 2, the regional model

Sheet 2 takes the same logical model and places it. It is the same picture three
times, plus the rules about what may pass between the copies.

### 9. Platform trust and policy authority

Four artefacts, hosted in the **primary South Africa control plane**, and the
group title states its own limit: **no regulated payloads**.

- **NATS operator + signing hierarchy** — JWT account claims and scoped keys. The
  signing authority itself is hosted in South Africa.
- **Tenant placement policy** — which cells and subject classes a tenant is
  permitted. The tile carries the rule: **an account is not a region**.
- **Country + commercial policy** — residency, currency, tax and retention, with
  the legal owner and the contract deciding.
- **Service + schema catalogue** — versions, health and deployment metadata, and
  **no tenant business records**.

### 10. Traffic placement

One wide element, and it fixes the order of operations: **resolve tenant and
country policy before selecting ingress**. Botswana traffic terminates
in-country, and a client does not choose its own residency by where it happens to
be.

### 11. The repeatable cell model

Each of the three cells repeats the same set of elements. That repetition is the
point — a cell is a template, not three bespoke builds:

- Regional or country **ingress**, and local **web delivery** of the App Shell and
  the MFE registry.
- **Service replicas** — control, domain, integration and workflow workers, with
  independent scale and rollout.
- **NATS logical HA, three nodes**, with tenant accounts and a local claim
  resolver. The sheet labels this a minimum topology and defers sizing.
- A **region-local JetStream domain**, with no default mirror.
- A **cell-resident Temporal runtime** — workers and persistence.
- **Service data** — Postgres per owner, with local backups.
- An **evidence store** — adapter plus object data.
- **Resident operations** — masked logs, metrics, traces, audit and keys, with
  backup and recovery following placement policy.
- A **policy tile** naming the cell's currency default and which tenant accounts
  it accepts.

The three cells are **South Africa shared (Cape Town)**, **Australia shared
(Sydney)** and **Botswana sovereign (in-country)**. Botswana differs in three
drawn ways: its replicas run a control-plane **slice** with no regulated remote
execution, its NATS cluster is **isolated**, and its Temporal runtime is
**self-hosted in-country**.

### 12. What crosses a border, and what does not

Only two paths cross between cells on the sheet, and one of them is drawn as
forbidden.

- Between the two **shared** cells, a **NATS gateway** — dotted cyan, and captioned
  "approved accounts and subjects only". There is no default JetStream mirror
  across regions.
- Along Botswana's edge, a **dashed red line** labelled **NO CROSS-BORDER
  REGULATED GATEWAY**. It is drawn to be seen. Only approved signed policy,
  claims and non-sensitive deployment metadata may enter the country-local
  control-plane slice.

## Why it is shaped this way

::: decision Proving who someone is, and deciding what they may do, are two jobs
An external identity provider authenticates. Linebooker authorizes, using tenant,
organisation, role and attributes it holds itself. Keeping the two apart means a
change of identity provider does not become a change to the permission model.
Traced to requirement L2-004.
:::

::: decision A tenant is a NATS account; a region is a placement
The sheet keeps these on separate axes on purpose, and states it twice — "an
account is not a region" on sheet 2, and organisation roles do not create new
accounts. The account is a hard messaging security boundary. Placement policy
then decides which cells that account and its data are permitted in. Conflating
the two would make every residency question a messaging change. Traced to L2-006
and L2-018.
:::

::: decision There are four kinds of store, and they are not substitutes
Postgres holds transactional truth. JetStream holds durable fact history. Temporal
holds workflow state. Projections and NATS KV hold derived, rebuildable reads.
The sheet names each by its role rather than by its product, so that "we already
have somewhere to put it" never becomes an architectural answer. Traced to L2-011
and L2-014.

The instance-level default sits in a separate governing decision rather than on
the sheet. **ADR-053** (*Use Shared PostgreSQL Instances by Default*, accepted,
V3 scope) shares Postgres infrastructure within a deployment region while keeping
a logically isolated database, dedicated credentials and independent migrations
per service. So "Postgres per owner" on the sheet is about **database
ownership**, not about one server per service.
:::

::: decision Temporal coordinates the process; the service still owns the rules
Tender expiry, execution, proof, claims and settlement all span services, so
something must coordinate them. The sheet gives that job to Temporal and then
guards against the usual failure: business rules migrating into the workflow. The
tile says it outright — services retain business rules. Traced to L2-010.
:::

::: decision Evidence documents go to an S3-compatible store, not to NATS
Proof of delivery, licences and similar files are high-volume and large. The sheet
puts them behind a vendor-neutral **adapter** with an S3-compatible
implementation, and states the reason on the tile: large files stay outside NATS.
NATS Object Store is not the default document repository. The adapter boundary
also means the provider can differ per country without changing the
architecture. Traced to L2-012.
:::

::: decision Regions start independent; a gateway is an exception, not a mesh
The proposed starting topology is independent regional clusters, with gateway
connectivity permitted only between shared cells and only for approved accounts
and subjects, and with no cross-region JetStream replication by default. A global
mesh is easy to assume and hard to withdraw once assumed. Traced to L2-017.
:::

::: decision A sovereign boundary includes transit, not just storage
Botswana's cell keeps compute, NATS traffic, workflow state, databases, documents,
telemetry, keys, backups and recovery in-country. Stating "the data is stored
locally" while messages route through another country would not satisfy the
requirement, which is why the prohibited gateway path is drawn rather than simply
omitted. Traced to L2-019 and L2-020.
:::

## What this view deliberately excludes

L2 explains construction. It does not expand into message subjects, payload
schemas, workflow steps or detailed permission claims. Each exclusion below has a
named destination in the L3 layer, and every one of those views is currently
`PLANNED` rather than written.

| Deferred | Goes to | Detail held back |
|---|---|---|
| L2-D01 | L3-01 Participant + Tenancy | Authority hierarchy, organisation membership, delegated administration, tenant lifecycle |
| L2-D02 | L3-03 Application / MFE | Shell composition, plugin discovery, routes, contributions, frontend deployment |
| L2-D03 | L3-04 External Integration | Authentication, contracts, schemas, rate limits, webhooks, bulk extracts, adapter behaviour |
| L2-D04 | L3-05 Messaging / NATS | Operator and accounts, signing keys, imports and exports, subjects, streams, gateways, failure behaviour |
| L2-D05 | L3-06 Workflow / Temporal | Workflow boundaries, signals, activities, compensation, retries, human tasks |
| L2-D06 | L3-07 Data Architecture | Per-domain ownership, outbox, projections, retention, lineage, document lifecycle |
| L2-D07 | L3-08 Multi-Region | Routing, gateway allowlists, data classifications, HA and DR, backup and recovery per jurisdiction |
| L2-D08 | L3-09 Security + Identity | Identity federation, the RBAC/ABAC policy model, cryptography, secrets, masking, security audit |
| L2-D09 | L3-11 Financial Architecture | Currency, tax, rating, invoicing, creditors, debtors, payments, reconciliation, settlement |
| L2-D10 | L3-12 Deployment + Services | Runtime platform, service catalogue, scaling, probes, rollout, topology, capacity |
| L2-D11 | L3-02 Functional Domains | Capability boundaries, service ownership, domain dependencies, the tender-to-settlement responsibility map |
| L2-D12 | L3-10 Observability | Regional telemetry architecture, masking, health, logs, metrics, traces, audit correlation, operational ownership |

Two further limits are stated on the sheets themselves. The three-node NATS
cluster and the service replicas are **logical high-availability minimums**, not a
sizing decision — capacity, sizing and orchestration are deferred. And the
logical tiles are an **ownership model**: a tile does not promise a one-to-one
mapping to a deployable or to a database.

## Open confirmations

Six questions remain open on this view. They need legal, regulatory, commercial
or technical sign-off, and they are recorded here rather than settled quietly in
the drawing.

- **Botswana's authoritative legal and regulatory controls.** Until these are
  confirmed, the proposed sovereign-cell boundary is a design intent and **not
  compliance evidence**. The sheet's own note says the same.
- **Whether South Africa and Australia need gateway connectivity at launch**, or
  whether both shared cells should begin fully independent.
- **Which tenant accounts may span shared regions**, and which subject classes may
  traverse a gateway.
- **The cloud or runtime platform, and the S3-compatible document-store provider
  for each country.** The architecture specifies an adapter boundary on purpose,
  so this can be answered per country without redrawing.
- **Whether South Africa and Australia use self-managed or regionally managed
  Temporal.** Botswana requires a self-hosted in-country runtime and persistence
  layer unless a legally validated country-local service becomes available.
- **Whether external reporting needs synchronous APIs only**, or also scheduled
  bulk exports and customer-managed destinations. Note that the parent L1 view
  confirmed request APIs and webhooks only at its own level; this L2 question is
  about the delivery mechanics beneath that answer, and stays open.

## Related documents

- **Parent:** [`LB-V3-L1-01` System and Platform Overview](/v3-architecture/l1-system-platform-overview)
  — who participates, and where the platform runs.
- **Sibling:** `LB-V3-L2-02` Technology Selection and Rationale — which
  technologies were selected and why. Status `DRAFT`; no written edition yet.
- **Children:** twelve L3 concern views, `LB-V3-L3-01` to `LB-V3-L3-12`, listed in
  the exclusions table above. All are `PLANNED`.
- **Navigation root:** `LB-V3-L0-01` Architecture Atlas.
