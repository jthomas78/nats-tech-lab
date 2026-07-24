# V3 Logistics Platform — System & Software Design Document

> ⚠ DRAFT — NOT final or authoritative. A top-level system overview assembled from round-1 Slack architecture notes and the nats-tech-lab NATS evaluation. This is a discussion draft for the round-2 architecture review — nothing here should be read as decided until the team confirms it. Treat every "PROPOSED" and "OPEN QUESTION" tag literally.

| Field | Value |
| --- | --- |
| Document Status | Draft |
| Version | 0.1 |
| Date | 2026-07-21 |
| Author(s) | Jeremy, nats-tech-lab |
| Approved By | (not yet reviewed) |

Template: Logistics_System_Design_Template (Computers/Architecture/templates).

## Table of Contents

1.  Introduction
2.  System Architecture Overview
3.  Component Design
4.  Data Flow
5.  Data Design
6.  API Design Overview
7.  Technology Stack
8.  Security Design
9.  Scalability & Performance Considerations
10. Deployment Architecture
11. Design Decisions & Trade-offs
12. Appendix

### Revision History

| Version | Date | Author | Description of Changes |
| --- | --- | --- | --- |
| 0.1 | 2026-07-21 | Jeremy | Initial draft, assembled from V3 building-blocks diagram + nats-tech-lab findings |

## 1. Introduction

### 1.1 Purpose

This document describes the initial, top-level architecture for the V3 Logistics Platform — a greenfield replacement for the existing V2 system. It is a synthesis of two inputs:

- The round-1 team Slack discussion, captured in the V3 Logistics Platform — Initial Building Blocks diagram.
- Working findings from nats-tech-lab, a lab evaluating NATS.io patterns (JetStream, NATS KV, Postgres, CQRS) against a concrete demo.

It is a system overview, not a component-level design. Section 1.3 captures the high-level requirements confirmed so far; a full SRS still does not exist for V3. It is not an approved design — it exists to seed the round-2 architecture meeting with a single written artifact instead of scattered Slack threads.

### 1.2 Scope

Covers the platform-level building blocks only: client layer, edge/routing, auth, the NATS comms backbone, the candidate services list, persistence, and named cross-cutting concerns. Component-internal design lives elsewhere and is out of scope here — where a building block has been prototyped (Dictionary), this document points to its own design notes rather than reproducing them.

Explicitly excluded: individual component/service internal design, individual carrier integrations (none identified yet), UI/UX design, and any deployment/infra decisions beyond what is stated in Section 10 (none are settled).

### 1.3 High-Level Requirements

These are the platform-level requirements the round-2 review is being asked to confirm; none are final. Component-level detail, where it exists, lives in the referenced POC notes rather than here.

- Multi-region: the platform must run across multiple geographic regions from day one — data residency, latency, and regional failover are first-class concerns, not later retrofits. See Section 8 (data residency/compliance routing) and Section 9 (regions assumed multi-region, contingent on Path A).
- Multi-tenant with isolated environments: V3 is multi-tenant SaaS; each tenant's data and environment must be isolated. Today only soft isolation (subject-prefix pattern, DD-04) is evaluated — hard isolation (one NATS Account per tenant) remains an open item (Section 12.B).
- Modularized / isolated services: source may live in one monorepo, but each candidate service (Section 3) deploys and scales independently, with its own Postgres schema. How service deployments and cross-service dependencies are versioned and sequenced in production is not yet decided — open item, Section 12.B. A service's frontend may be packaged and deployed as part of that service (vertical slice) rather than a single platform-wide frontend — open question, unresolved against the lab's current lab-shell-plus-per-demo-frontend pattern.
- Authentication and Fine-Grained Authorization: AuthN via a managed identity provider (authentik candidate, not confirmed) plus AuthZ via ReBAC/FGA (OpenFGA/Zanzibar-style relation tuples), proposed to answer "who can see what" at tenant/org/user/role/document granularity. NATS NSC scoped signing keys were evaluated and rejected as too coarse for this alone (DD-06). See Section 8.
- Globalization: localization (i18n/translation strings with locale fallback, prototyped in the Dictionary POC per BR-D03), tenant/region-specific configuration, and reference data (dictionaries, enums, lookup values) distributed via the Postgres-source-of-truth-plus-NATS-KV-cache pattern validated by that POC (DD-01).

### 1.4 Design Goals

- Scalability to handle multi-tenant SaaS load across regions.
- High availability for whichever services end up latency-sensitive (Tracking, GEO are the obvious candidates once designed).
- Extensibility to onboard new tenants/regions without re-architecting the backbone.
- Maintainability via clear service boundaries — one service per candidate box, own VM/container, own Postgres schema.
- Auditability — but only where something actually needs to replay history (see the event-sourcing-vs-CRUD heuristic in Section 11, DD-03). Not every service needs an event log by default.
- A settled responsibility split between JetStream, NATS KV, and Postgres — this is the platform's open architectural question and the reason the lab exists.

### 1.5 References

- V3 Logistics Platform — Initial Building Blocks (Architecture/v3-platform-building-blocks.png / .svg).
- Dictionary building-block POC — POC-Dictionaries/Summary/0. Overview (component-level detail lives here, deliberately out of scope for this overview).
- Event Sourcing + CQRS design — Event sourcing/4. Design - Event Sourcing + CQRS.
- Source: v3-stuffies Slack channel, round 1 — not yet a formal SRS.

## 2. System Architecture Overview

### 2.1 Architecture Style

Event-driven services connected through a single shared NATS backbone (JetStream for event streaming/replay, NATS KV for fast lookup/cache/watch), with Postgres as the transactional source of truth per service. This much has real conviction behind it (marked PROPOSED throughout).

What is not settled is the edge topology in front of that backbone — two alternatives are on the table and unresolved:

- Path A (proposed baseline): GCP Global HTTPS Load Balancer → Gateway API (AuthN/AuthZ enforcement, payload validation, HTTP→NATS translation) + a dedicated WebSocket MIG for client subscriptions.
- Path B (floated, unresolved): clients connect to NATS directly, bypassing the LB/Gateway entirely. Raised as "what if we didn't need a load balancer, or even HTTP support" — genuinely unresolved, in particular how per-client subject authorization would be enforced at connect time without the Gateway in the loop.

Rationale for why this remains open rather than resolved here: the team has not converged, and it gates several downstream decisions (WS handling, rate-limit/compliance enforcement point). See Section 12.B.

### 2.2 High-Level Architecture Diagram

V3 Logistics Platform — Initial Building Blocks (round-1 discussion draft)

The generic event-sourcing/CQRS pattern the platform is expected to apply selectively (not uniformly — see DD-03):

Event Sourcing + CQRS — pattern reference, applied only where replay is needed

### 2.3 Core Components

| Component | Status | Responsibility |
| --- | --- | --- |
| Web / App Clients | Proposed | Browser + mobile frontends |
| GCP Global HTTPS LB (Path A) | Proposed | Global anycast IP, per-region backend MIGs, native WS proxying |
| Gateway API (Path A) | Proposed | AuthN/AuthZ enforcement, payload validation, HTTP→NATS translation, compliance-routing enforcement point |
| WebSocket MIG (Path A) | Proposed | Dedicated MIG for WS connection-state affinity; authenticates then subscribes to NATS subjects |
| NATS-native clients (Path B) | Open question | Alternative that bypasses LB/Gateway — unresolved |
| AuthN — authentik (candidate) | Not confirmed | Managed authentication; surfaced once, no follow-up |
| AuthZ / FGA (ReBAC) | Proposed | Zanzibar/OpenFGA-style tenant/org/user/role/document relation tuples; NATS NSC scoped keys evaluated and rejected as too coarse alone |
| NATS (JetStream + KV) | Proposed | The comms backbone every service depends on |
| Dictionary | Prototyped (POC) | Reference/dictionary data building block — the one service prototyped in the lab; component detail in its own POC notes |
| Comms, Tracking, GEO, Docs, Fleet, Routes, POI, Commodities | Not discussed | Candidate services, named only, none designed |
| Postgres | Proposed | Transactional source of truth, one schema per service |
| Postgres / KV / JetStream split | Open question | The question the lab exists to answer — the Dictionary POC is the first concrete data point, not a platform-wide ruling |

## 3. Component Design

Per the template convention, each core component gets a design section. At this stage none of the candidate services have a component-level design — they are named boxes only. Component internals are deliberately out of scope for this system overview.

One building block, Dictionary, has been prototyped end-to-end in nats-tech-lab (own service, own Postgres schema, NATS KV distribution cache, working and tested in Docker). Its component-level design — schema, cache-coherence protocol, API surface, sequence flows — lives in its own POC notes (POC-Dictionaries/Summary) and is not reproduced here. What matters at the platform level is the pattern it validated and the input it gives the open persistence question:

- Postgres as source of truth with NATS KV as a distribution/cache layer in front of it is a workable, tested shape for a reference-data building block.
- That result is one data point for the platform-wide Postgres/KV/JetStream split (Section 11, DD-01) — not a decision that every service must follow.

All other candidate services (Comms, Tracking, GEO, Docs, Fleet, Routes, POI, Commodities) require their own component-design pass before implementation. Language choice (Java vs. Go, per-service or platform-wide) is also unconfirmed. This is the largest gap in this document — see Section 12.B.

## 4. Data Flow

### 4.1 Generic Event Sourcing + CQRS (pattern reference, applied selectively)

This is the pattern the platform reaches for only when something needs to replay an entity's history (per DD-03). Where a building block instead only needs its current state distributed cheaply (e.g. reference data), it uses the simpler Postgres-plus-KV-cache shape validated by the Dictionary POC rather than an event log. See the CQRS pattern diagram in Section 2.2.

### 4.2 Platform Flows — Not Yet Defined

Order-to-delivery, carrier tracking updates, and any flow touching the candidate services in Section 3 have no sequence diagram yet — they depend on those services being designed first.

## 5. Data Design

At the platform level, two things are settled in principle:

- One Postgres instance topology per environment, one schema per service — service data boundaries are real (separate schemas), not just naming conventions.
- NATS KV is a derived/cache layer, never the sole system of record for governed data; JetStream is the durable event log only for services that actually need replay.

Per-service data domains (Orders, inventory, shipments/tracking, routes/vehicles/drivers, carriers/rate contracts, customers/accounts) are not defined yet — they depend on the candidate services being designed. Detailed entity/schema design for any building block belongs in that component's own design doc, not in this overview (the Dictionary POC's schema, for example, lives in its POC notes).

## 6. API Design Overview

The platform's external API surface depends on the unresolved edge decision (Section 2.1):

- Path A — clients speak HTTP/WS to a Gateway API that translates to NATS subjects.
- Path B — clients speak NATS directly, and "API" becomes a subject/schema contract rather than a REST surface.

No platform-wide API contract exists yet. Where a building block has been prototyped (Dictionary), it exposes REST + Swagger + SSE — but that is a component-level detail, not a platform standard, and should not be generalized until the edge path is chosen.

API contract / schema versioning is explicitly flagged as not-yet-discussed at the platform level, and it is high-stakes: for any event-sourced service, schema evolution can break replay (see Section 12.B).

## 7. Technology Stack

| Layer | Technology | Status |
| --- | --- | --- |
| Frontend (Web) | Vue 3 + PrimeVue + Pinia | In use in the lab |
| Backend Services | Go (hexagonal) in the lab; Java vs. Go platform-wide not confirmed | Partly in use / open |
| Edge / Routing | GCP Global HTTPS LB + Gateway API (Path A) — or NATS-native clients (Path B) | Open question |
| API Gateway | Candidate: Gateway API (Path A only) | Proposed (Path A) |
| Messaging / Events | NATS — JetStream (event backbone) + NATS KV (cache/lookup/watch) | Proposed |
| AuthN | authentik (candidate) | Not confirmed |
| AuthZ | OpenFGA/Zanzibar-style ReBAC | Proposed |
| Database(s) | PostgreSQL, one schema per service | Proposed |
| Caching | NATS KV (kept in the NATS ecosystem, not Redis) | Proposed |
| Infrastructure | Docker Compose per demo in the lab; production platform (Kubernetes/GKE) not decided | Not decided |
| CI/CD | — | Not decided |
| Monitoring/Observability | — | Not decided |

## 8. Security Design

- Authentication: authentik proposed as the candidate AuthN platform — not confirmed, surfaced once in Slack with no follow-up.
- Authorization: OpenFGA/Zanzibar-style ReBAC (tenant/organization/user/role/document relation tuples) — proposed, directly answers the "who can see what" question. NATS NSC scoped signing keys were evaluated and explicitly rejected as too coarse for this on their own.
- Multi-tenancy: subject-prefix soft isolation ({tenant}.{region}.{bounded_context}.{event}, e.g. emea.acme.*) is the pattern evaluated in the lab today. Hard isolation (one NATS Account per tenant) has not been evaluated — named as its own future spike.
- Data protection: TLS in transit / encryption at rest — assumed, not yet confirmed for V3 infra.
- Secrets management: not yet decided.
- Audit logging: event-sourced services get an audit trail for free from the event log; services that use plain CRUD do not, and will need an explicit decision if audit is required. Not to be assumed platform-wide.
- Regional data residency / compliance routing: the team agreed this is a Gateway-tier job, not the LB's — but that only holds if Path A is chosen, and which regions/rules apply is unclear either way. Open question.

## 9. Scalability & Performance Considerations

- Putting NATS KV in front of Postgres removes the database from the hot read path for lookup-heavy building blocks — the shape validated by the Dictionary POC and a candidate pattern for other read-heavy services.
- Snapshotting for event-sourced services is not yet solved — as an event stream grows, every write gets slower. Relevant to any future service that adopts event sourcing.
- Regions are assumed multi-region (global GCP LB, per-region backend MIGs) contingent on Path A — not yet confirmed which regions or under what rules.
- No load-testing targets exist yet for any component.

## 10. Deployment Architecture

Nothing is settled here for production. Current state, for context: each nats-tech-lab demo runs its own docker-compose.yml and does not share a network with the lab shell or other demos — a lab-isolation choice, not a statement about how V3 will be deployed. No environments (dev/staging/prod), orchestration platform, autoscaling groups, CDN, database topology, or disaster-recovery region have been decided. Section 2.1's Path A assumes GCP with per-region backend MIGs behind a global LB, contingent on Path A being chosen at all.

## 11. Design Decisions & Trade-offs

| ID | Decision | Alternatives Considered | Rationale |
| --- | --- | --- | --- |
| DD-01 | For governed data, keep Postgres as the source of truth with NATS KV as a distribution/cache layer in front of it — validated for Dictionary, offered as a candidate platform pattern | NATS KV as the read model directly; event-sourcing reference data via JetStream | Data with no replay need gains durability/governance from Postgres without event-log machinery; KV supplies low-latency distribution. First proven data point for the platform-wide split |
| DD-02 | Build reference-data capability in-house rather than adopt a commercial RDM/MDM product | Off-the-shelf RDM/MDM | Commercial products solve a governance problem the platform doesn't have yet, and none provide the NATS-KV distribution/cache-coherence layer under evaluation |
| DD-03 | Event-source an entity only when something needs to replay its history — not merely because it changes over time | Event-source everything uniformly; plain CRUD everywhere | Two triggers for revisiting later: the entity gains a real state machine, or someone needs a temporal query. Keeps the event-log cost where it earns its keep |
| DD-04 | Identifier-first NATS subject pattern: {tenant}.{region}.{bounded_context}.{event} | Namespace-first (e.g. orders.customer.created) | Namespace-first suits single-tenant systems; identifier-first supports soft multi-tenant isolation now, with a path to hard isolation (separate NATS Accounts) later |
| DD-05 | Edge/routing topology — unresolved | Path A: GCP LB + Gateway API + dedicated WS MIG · Path B: clients connect to NATS directly | Not yet decided — gates WS handling, compliance-routing enforcement point, and rate-limit enforcement. See Section 12.B |
| DD-06 | NATS NSC scoped signing keys alone are insufficient for platform AuthZ | Rely on NSC scopes only | Too coarse for tenant/org/user/role/document-level relation checks — ReBAC proposed to cover the gap |

## 12. Appendix

### A. Glossary

- JetStream — NATS's persistent streaming layer; durable event logs and, where useful, bounded change-notification feeds.
- NATS KV — key-value store backed by JetStream; fast lookup, caching, and watch-based live updates.
- CQRS — Command-Query Responsibility Segregation; separates the write model from read-optimized projections.
- ReBAC / FGA — Relationship-Based Access Control / Fine-Grained Authorization, Zanzibar/OpenFGA-style relation tuples.
- Soft vs. hard tenant isolation — soft: subject-prefix convention within one shared NATS Account; hard: one NATS Account per tenant with explicit cross-tenant imports/exports.
- LimitsPolicy — JetStream retention policy that enables replay (vs. InterestPolicy, which doesn't).
- MIG — GCP Managed Instance Group.

### B. Open Issues

Each is a real, currently-unresolved item, not a placeholder:

- Edge/routing path (Path A vs. Path B) — unresolved; blocks WS handling and rate-limit/compliance enforcement decisions.
- AuthN candidate (authentik) — surfaced once, never confirmed.
- Multi-tenancy hard isolation (NATS Account per tenant) — not yet evaluated; only soft isolation exists today.
- Regional data residency / compliance routing — agreed to be a Gateway-tier job in principle, but which regions and rules is undefined, and only holds if Path A wins.
- API contract / schema versioning — on the original tech list, never revisited; high-stakes for any event-sourced service.
- Component-level design for every candidate service (Comms, Tracking, GEO, Docs, Fleet, Routes, POI, Commodities) — not started.
- Platform-wide Postgres/KV/JetStream responsibility split — the Dictionary POC is one input, not a platform-wide ruling; other services may land differently depending on whether they need replay.
- Language choice per service (Java vs. Go) — not confirmed.
- Snapshotting — unsolved for any future event-sourced service; the main scalability gap on that side.

### C. Sign-off / Approval Log

(empty — draft, not yet reviewed)
