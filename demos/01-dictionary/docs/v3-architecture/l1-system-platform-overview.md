---
title: L1 System and Platform Overview
aside: false
---

# System and Platform Overview

<div class="v3-meta">
<strong>LB-V3-L1-01</strong> &middot; Level L1 &middot; Parent: Architecture Operational Authority &middot;
Requirements register: <code>Proposed-Linebooker-V3-L1-Requirements.md</code> &middot;
Catalogue status: AVAILABLE
</div>

::: warning PROPOSED, NOT BUILT
This page describes a **proposed** architecture for a future Linebooker V3
platform. Nothing here is built. The rest of this site documents the Dictionary
proof of concept, which is real code you can run. Do not read the two as one
system.
:::

## The question this view answers

Document `LB-V3-L1-01` answers one question:

**What is Linebooker, who and what participates in it, and where does it run?**

That is the whole job of this level. Everything else is deliberately pushed
down. If a question needs a protocol, a message subject, a database schema or a
deployment mechanic to answer it, this is the wrong page.

## The figure

<figure class="v3-figure">
<a class="v3-figure-zoom" href="/v3-architecture/lb-v3-l1-01.png" target="_blank" rel="noopener">
<img src="/v3-architecture/lb-v3-l1-01.png" alt="LB-V3-L1-01 System and Platform Overview">
</a>
<figcaption><strong>LB-V3-L1-01</strong> — System and Platform Overview.
<a href="/v3-architecture/lb-v3-l1-01.png" target="_blank" rel="noopener">Open full size</a> &middot;
<a href="/v3-architecture/lb-v3-l1-01.pdf" target="_blank" rel="noopener">Print edition (PDF)</a>
</figcaption>
</figure>

## How to read this view

The drawing carries its own reading rule. In words:

- A **solid blue** line means a role uses a Linebooker experience.
- An **amber dashed** line means an external contract or an event source outside
  Linebooker.
- A **green** line means conceptual implementation, or placement — where
  something runs. Green is never a live data flow.
- **Group colour identifies the viewpoint, not the technology.** A blue box and
  a purple box are not different products; they are the same landscape looked at
  from a different angle.

Two phrases in the drawing mean something narrower than they sound:

- **Immutable audit** means a durable, tamper-evident record of what the business
  did. It does **not** mean every part of the platform rebuilds its state by
  replaying events.
- **Policy-selected placement** means a rule decides which country a thing runs
  in. The green lines at the bottom of the sheet are that rule, not traffic.

Acronyms used on the sheet: **MFE** (micro-frontend, a user interface assembled
from separately built parts); **RBAC** (role-based access control, permission by
job role); **ABAC** (attribute-based access control, permission by properties of
the request); **SRE** (site reliability engineering, the people who keep the
platform running); **ERP** (enterprise resource planning, a customer's own
back-office system); **POD** (proof of delivery); **ETA** (estimated time of
arrival); **GPS** (global positioning system); **IdP** (identity provider, the
service that proves who a user is).

## The architecture

The sheet is read top to bottom in five bands. Each band is a different kind of
thing, and the bands connect downward.

### 1. Human actors — people who use Linebooker

Six groups of people, and the split between them matters more than the count.

Inside Linebooker: the **Finance creditors team** and the **Finance debtors
team** are shown separately, because paying transporters and collecting from
shippers are different jobs. **Controllers** do logistics control and follow-up
for the marketplace operation. **Linebooker Tech** platform administrators and
SRE keep the platform itself running.

Outside Linebooker: **Shipper users** procure and approve transport.
**Transporter teams** dispatch and drive.

### 2. Business participants — organisations in the market

This band answers "who are the companies", and it makes one distinction the rest
of the document depends on:

- **Linebooker** is the product business. It operates the marketplace and is the
  **tenant authority**.
- **Linebooker Tech** is the technology arm. It provides and operates the
  software as a service.

Alongside them, **Shipper organisations** publish demand and own their own
commercial data. **Transporter organisations** offer capacity and run the fleet.

Three ideas are kept apart on purpose, and this is the single most important rule
on the page:

- A **tenant** is the marketplace-operating business. It is the authority
  boundary, and it may span more than one region.
- An **organisation** is participation inside a tenant.
- A **country or region** is placement — where things run.

They are not the same axis. Collapsing any two of them together is a design
error, not a simplification.

### 3. External systems — contracted dependencies

Things Linebooker depends on but does not own, grouped in three:

- **Identity dependency** — WorkOS and enterprise identity providers.
- **Operational integrations** — fleet, GPS, telematics, maps and routing.
- **Enterprise and utility systems** — ERP, finance, banks, payments,
  notifications and regulatory systems.

These are ecosystem actors, not business participants. They connect through
contracts, and they are drawn with the amber dashed line for that reason.

### 4. Application experiences — the channels

One product, six governed channels, each shaped for a role:

| Experience | For | Carries |
|---|---|---|
| Shipper workspace | Shipper users | Tenders, tracking, cost, approvals |
| Transport workspace | Transporter orgs | Offers, fleet, jobs, proof of delivery, payment |
| Driver mobile | Drivers | Tasks, evidence, status, alerts |
| Tenant / org admin | Administrators | Members, policies, roles, configuration |
| Operator control tower | Linebooker Tech | Exceptions, service, tenants, audit, health |
| External data APIs | Authorised systems | Ingest, scoped report extracts, webhooks |

The last row carries a governance rule, not just a feature: an authorised outside
system extracts data for its own reporting **through a governed API, never by
reaching into a database directly**.

Everything in this band feeds one line downward, labelled *role-filtered business
actions*. A person's role decides what they may do, at the channel.

### 5. Logistics and commercial capabilities — one governed lifecycle

The business itself, drawn as one tender-to-settlement lifecycle rather than as
separate products:

- **Marketplace and tendering** — demand, rates, bids, awards
- **Loads, trips and routes** — allocation, stops, execution
- **Fleet and capacity** — trucks, drivers, eligibility
- **Tracking and exceptions** — position, ETA, alerts, control tower
- **Documents and compliance** — proof of delivery, licences, insurance, evidence
- **Claims and acceptance** — proof gates, disputes, audit trail
- **Billing and settlement** — pricing, tax, invoice, fees, payment

Two things to notice. First, the platform **mediates** this lifecycle. Shippers
and transporters do not message each other directly; the platform sits in the
middle and governs the exchange. Second, **money is a first-class domain, not a
report at the end.** Pricing, tax, fees, invoices, payments and transporter
settlement follow the marketplace legal entity and the contract.

### 6. Application platform — the shared foundations

What every capability above is built on, named but not explained at this level:

- **Identity and access** — membership, RBAC and ABAC
- **NATS messaging** — live and durable facts
- **Temporal** — long-running workflows
- **Service data** — Postgres and projections
- **Evidence storage** — an adapter over S3-compatible storage
- **Observe and audit** — metrics, logs, traces

Under those sits a dedicated block: **Security, residency and immutable audit** —
tenant separation, RBAC/ABAC, encryption, masked logs and country policy. It is
drawn as its own block rather than as a footnote because it is a promise that
applies across the whole platform.

Each store in this band is named by the **role** it plays, not by the product
alone. Service data is transactional truth and derived read models; NATS carries
durable facts; evidence storage is the document repository.

### 7. Country and regional placement

The bottom band is the only band that answers "where":

| Cell | Placement | Currency default |
|---|---|---|
| South Africa — shared Cape Town cell | `af-south-1`, regional data | ZAR |
| Botswana — sovereign in-country cell | Compute, data, transit and recovery stay local | BWP |
| Australia — shared Sydney cell | `ap-southeast-2`, regional data | AUD |

Botswana is drawn differently on purpose. It is the worked example of a country
whose regulation requires the compute, the data, the transit, the logs, the keys,
the backups and the recovery path to stay inside the country.

**Currency is not guessed from where a user is sitting.** The country codes shown
are policy defaults. The actual transaction currency is selected by the
marketplace, the contracting legal entity and the contract.

## Why it is shaped this way

::: decision Tenant, organisation and country are three separate axes
Requirement L1-009. A tenant is marketplace authority, an organisation is
participation, and a country is placement. A tenant may span regions, so tenancy
cannot be a location. This is why the sheet has both a participants band and a
separate placement band instead of one combined map.
:::

::: decision Linebooker and Linebooker Tech are drawn as two arms
Requirements L1-003 and L1-006. The product business operates the marketplace and
holds tenant authority; the technology arm provides and operates the platform.
Keeping them apart is what makes the Operator control tower a legitimate,
separate channel rather than a back door into a customer's data.
:::

::: decision Outside systems read through a governed API, never the database
Requirement L1-011. Authorised external systems need Linebooker data for their
own reporting. Granting database access would make every internal schema a public
contract and would defeat tenant separation. A scoped API is the only channel.
:::

::: decision Finance is a domain, not a report
Requirement L1-014. Pricing, tax, fees, invoices, payments and transporter
settlement are drawn inside the lifecycle band. Money moves as part of the
governed lifecycle, so it cannot be a downstream summary of it.
:::

::: decision "Immutable audit" promises history, not universal event sourcing
Requirement L1-022, added on 2026-09-03. The platform promises a durable,
tamper-evident business history. It does not claim every domain rebuilds state by
replaying events. The lab's own finding is that the deciding question is whether
anything needs to replay a thing, not whether it changes — so a blanket promise
of event sourcing at L1 would be wrong.
:::

::: decision Currency follows the contract, not the user's location
Requirement L1-019. Inferring currency from where someone is sitting breaks as
soon as a South African user books freight under an Australian contract. The
country code on each placement cell is a policy default only.
:::

## What this view deliberately excludes

L1's exclusions are service internals, protocols, schemas and deployment
mechanics. Each is registered as a derived requirement with the view that owns it:

| Deferred | Goes to |
|---|---|
| Application shell and micro-frontend composition, integration boundary, control plane, services, NATS, Temporal, data and deployment structure (L1-D01) | `LB-V3-L2-01` Logical and Technical Architecture |
| External API authentication, tenant and organisation scopes, data products, schemas, filtering, pagination, rate limits, audit, masking, export formats (L1-D02) | `LB-V3-L3-04` External Integration |
| Linebooker and Linebooker Tech responsibilities, organisation memberships, department roles, delegated authority (L1-D03) | `LB-V3-L3-01` Participant and Tenancy |
| Currency selection, tax, rating, invoicing, creditors, debtors, payments, reconciliation, settlement (L1-D04) | `LB-V3-L3-11` Financial Architecture |
| Data classification, country rules, encryption, masking, cross-border exceptions, retention, backup, recovery (L1-D05) | `LB-V3-L3-09` Security and Identity |

L1 also names which technologies appear (NATS, Temporal, Postgres, WorkOS,
S3-compatible storage) without saying why they were chosen. That reasoning is
`LB-V3-L2-02` Technology Selection and Rationale.

## Confirmations

This view opened with three questions the drawing could not answer on its own.
All three were confirmed by the business owner on 2026-09-03, and each answer
matches what the sheet already draws, so the drawing did not change.

- **Linebooker and Linebooker Tech are two organisational arms of one business**,
  not separate legal entities. Linebooker operates the marketplace; Linebooker
  Tech provides and operates the platform. That is why the sheet draws them as
  two participants without a legal boundary between them. Which arm contracts,
  invoices and owns data for residency purposes is a responsibility split, and it
  belongs to the L3 Participant and Tenancy view.
- **Brokers and agents are not participants at this level.** They remain a future
  participant type, which is why the sheet shows shipper and transporter
  organisations only. Introducing one later would also introduce a visibility
  question — whether a broker may see a transporter's price — and that question
  is not an L1 question.
- **External consumers would extract data through request APIs and webhooks
  only.** Bulk and asynchronous export is not proposed at this level, so the
  single governed `External data APIs` channel on the sheet is complete as drawn.
  Which data products may leave through it is deferred to the L3 External
  Integration view.

Nothing further is outstanding on this view. The drawing's own footer still
carries the standing caution, which these confirmations do not lift: this is a
proposed target architecture, and its regulatory and financial rules require
jurisdiction and legal-owner validation before anything is built.

## Related documents

- **Parent:** the Architecture Operational Authority — scope, levels, catalogue
  and identity rules for this whole series.
- **Children:** `LB-V3-L2-01` Logical and Technical Architecture (how the
  platform is constructed) and `LB-V3-L2-02` Technology Selection and Rationale
  (which technologies were selected and why).
- **Navigation root:** `LB-V3-L0-01` Architecture Atlas.
