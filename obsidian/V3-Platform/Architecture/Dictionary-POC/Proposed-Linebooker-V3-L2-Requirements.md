# Proposed Linebooker V3 Architecture - L2 Requirements

This register maintains the requirements represented by
`LB-V3-L2-01 - Logical and Technical Architecture`. L2 explains how the
stakeholder landscape in L1 is logically constructed without expanding into
message subjects, payload schemas, workflow steps or detailed permission claims.

## Status vocabulary

- **Included** - visibly represented in the current L2 document.
- **Derived** - intentionally delegated to an L3/L4 concern view.
- **Open** - requires business, legal, regulatory or technical confirmation.

## L2 requirements

| ID | Status | Requirement | L2 representation |
|---|---|---|---|
| L2-001 | Included | Preserve the L1 distinction between people, participant organisations, tenants, geography and external systems. | Client/channel, tenancy, integration and placement boundaries remain separate. |
| L2-002 | Included | Show how browser, mobile and external consumers enter the platform. | App Shell/MFE, driver mobile and governed partner/reporting API channels. |
| L2-003 | Included | Show an explicit integration boundary. | Edge/API gateway, external data API, adapters and webhook/event ingestion. |
| L2-004 | Included | Separate authentication from application authorization. | WorkOS/enterprise IdP authenticates; tenant/org membership, RBAC and ABAC authorize. |
| L2-005 | Included | Show the platform control plane. | Tenant/org management, identity/membership and Linebooker authorization, NATS provisioning, refdata/config, entitlements/billing/admin, observability/audit. |
| L2-006 | Included | Show the NATS trust hierarchy. | NATS operator, one account per marketplace tenant, organisation membership/roles and short-lived user/workload credentials. |
| L2-007 | Included | Organise the logical implementation around business capabilities. | Marketplace/tendering, loads/trips/routes, fleet/tracking, documents/compliance, disputes/acceptance and billing/settlement services. |
| L2-008 | Included | State the service ownership model. | Independently deployable service replicas own their transactional data and publish contracts through NATS. |
| L2-009 | Included | Distinguish synchronous and asynchronous communication. | Governed API/request paths versus NATS command/event paths. |
| L2-010 | Included | Show Temporal's responsibility. | Cell-resident coordination and persistence for long-running tender-to-settlement workflows; domain services retain business ownership. |
| L2-011 | Included | Distinguish transactional state, event history, workflow state and read models. | Service-owned PostgreSQL, JetStream, Temporal persistence and projections/KV are separate stores. |
| L2-012 | Included | Support high-volume evidence document storage without treating NATS Object Store as the default document repository. | Vendor-neutral document storage adapter with an S3-compatible implementation option. |
| L2-013 | Included | Provide governed external data extraction. | Scoped reporting/data APIs and asynchronous exports; no direct database access. |
| L2-014 | Included | Make immutable audit visible without requiring universal event sourcing. | Durable domain facts plus security/administrative audit; PostgreSQL remains the default transactional source of truth. |
| L2-015 | Included | Show the initial deployment footprint side by side. | South Africa shared cell, Botswana sovereign in-country cell and Australia shared cell. |
| L2-016 | Included | Define a repeatable regional-cell model. | Regional ingress, web/MFE delivery, service replicas, three-node logical NATS HA minimum, region-local JetStream, cell-resident Temporal runtime, service databases and document storage. |
| L2-017 | Included | Select an initial NATS multi-region topology. | Independent regional clusters; policy-approved gateway connectivity between shared cells; no cross-region JetStream replication by default. |
| L2-018 | Included | Keep tenant isolation separate from regional placement. | Tenant NATS accounts are messaging security boundaries; placement policy assigns an account and its data to permitted cells. |
| L2-019 | Included | Enforce Botswana country-local storage and transit. | Dedicated Botswana ingress and cell; regulated compute, messages, databases, documents, logs, keys, backups and recovery remain in-country. |
| L2-020 | Included | Prevent Botswana regulated data from traversing cross-border NATS links. | Botswana has no gateway path for regulated tenant accounts; only approved signed policy/configuration enters the local control-plane slice. |
| L2-021 | Included | Show platform security controls. | Tenant separation, RBAC/ABAC, encryption in transit and at rest, log masking, credential rotation and policy enforcement. |
| L2-022 | Included | Show observability and audit as regionalized operational capabilities. | Metrics, traces, masked logs and immutable audit remain subject to the cell's residency policy. |
| L2-023 | Included | Show availability without implying a finalized infrastructure implementation. | Service replicas and three NATS nodes per cell are logical HA minimums; capacity, sizing and orchestration are deferred. |
| L2-024 | Included | Preserve currency and billing policy context. | Entitlements/billing control-plane capability and settlement services consume marketplace/legal-entity/country policy. |
| L2-025 | Included | Deliver the L2 document as matching editable and distributable editions. | One HTML document and a two-sheet A3 landscape PDF. |
| L2-026 | Included | Place the NATS operator signing authority and global trust catalogue. | Primary authority and private signing hierarchy run in the South Africa control plane; cells use local claim resolvers. |
| L2-027 | Included | Show where App Shell bundles and the MFE registry are served. | Every regional/country cell has local web delivery for the shell and MFE registry. |
| L2-028 | Included | State the Temporal hosting and residency model. | Temporal runtime, workers and persistence are cell-resident; Botswana is self-hosted in-country. |

## Derived requirements

| ID | Target view | Detail deferred from L2 |
|---|---|---|
| L2-D01 | LB-V3-L3-01 Participant + Tenancy | Authority hierarchy, organisation membership, delegated administration and tenant lifecycle. |
| L2-D02 | LB-V3-L3-03 Application / MFE | Shell composition, plugin discovery, routes, contributions and frontend deployment. |
| L2-D03 | LB-V3-L3-04 External Integration | Authentication, contracts, schemas, rate limits, webhooks, bulk extracts and adapter behavior. |
| L2-D04 | LB-V3-L3-05 Messaging / NATS | Operator/accounts, signing keys, imports/exports, subjects, streams, gateways and failure behavior. |
| L2-D05 | LB-V3-L3-06 Workflow / Temporal | Workflow boundaries, signals, activities, compensation, retries and human tasks. |
| L2-D06 | LB-V3-L3-07 Data Architecture | Per-domain data ownership, outbox, projections, retention, lineage and document lifecycle. |
| L2-D07 | LB-V3-L3-08 Multi-Region | Routing, gateway allowlists, data classifications, HA/DR, backup and recovery per jurisdiction. |
| L2-D08 | LB-V3-L3-09 Security + Identity | Identity federation, RBAC/ABAC policy model, cryptography, secrets, masking and security audit. |
| L2-D09 | LB-V3-L3-11 Financial Architecture | Currency, tax, rating, invoicing, creditors, debtors, payments, reconciliation and settlement. |
| L2-D10 | LB-V3-L3-12 Deployment + Services | Runtime platform, service catalogue, scaling, probes, rollout, topology and capacity. |
| L2-D11 | LB-V3-L3-02 Functional Domains | Capability boundaries, service ownership, domain dependencies and tender-to-settlement responsibility map. |
| L2-D12 | LB-V3-L3-10 Observability | Regional telemetry architecture, masking, health, logs, metrics, traces, audit correlation and operational ownership. |

## Open confirmations

- Confirm the authoritative legal and regulatory controls for Botswana before
  treating the proposed sovereign-cell boundary as compliance evidence.
- Confirm whether South Africa and Australia require NATS gateway connectivity
  at launch or whether both cells should begin fully independent.
- Confirm which tenant accounts may span shared regions and which subject classes
  may traverse a gateway.
- Confirm the chosen cloud/runtime and the S3-compatible document-store provider
  for each country; the architecture intentionally specifies an adapter boundary.
- Confirm whether South Africa and Australia use self-managed or regionally
  managed Temporal services. Botswana requires a self-hosted in-country Temporal
  runtime and persistence layer unless a legally validated country-local service
  becomes available.
- Confirm whether external reporting needs synchronous APIs only or also scheduled
  bulk exports and customer-managed destinations.

## Change log

- 2026-09-03 - Initial L2 register created for `LB-V3-L2-01`, including the
  logical/service model, initial NATS topology and the Botswana sovereign-cell
  example.
- 2026-09-03 - Review corrections added title parity, cloud-neutral Sydney
  placement, explicit trust-authority and frontend-serving locations, cell-local
  Temporal hosting, NATS logical-minimum wording, and complete L3 derivations.
