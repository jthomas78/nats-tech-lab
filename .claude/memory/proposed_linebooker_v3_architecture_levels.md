# Proposed Linebooker V3 architecture levels

Source: `proposed-linebooker-v3-architecture-discussion.md` (noted 2026-09-03).

This is reference context from an architecture discussion, not authorization to edit, rename, or regenerate artefacts unless the user requests that work separately.

Use `.claude/skills/linebooker-architecture-documenter/SKILL.md` for future work
on this document set. It applies the level, ID, output and L0 synchronization
workflow, then uses `html-diagram-drawer` for rendering mechanics.

## Proposed naming and hierarchy

- `Proposed Linebooker V3 Architecture` is the architecture baseline/version.
- `L0 — Architecture Atlas and Diagram Sitemap` is the graphical table of
  contents, document-status map and navigation root. It links to available
  diagrams and may show planned views, but contains no implementation design.
- Deliver every document in this architecture series in both editable HTML and PDF formats.
- Maintain L1 requirements in `obsidian/V3-Platform/Architecture/Dictionary-POC/Proposed-Linebooker-V3-L1-Requirements.md` and update that register before changing the L1 artefact.
- Retire the old use of V2/V3 as diagram-depth labels.
- `L1 — System & Platform Overview`: system context for business stakeholders—human actors, business participants, external systems, applications, major logistics capabilities, platform and regions.
- `L2 — Logical & Technical Architecture`: conceptual construction—application shell/MFE, integration boundary, domain services, NATS, Temporal, data, platform services and regional boundaries.
- Multiple L3 concern views: participant/tenancy, functional domains, application/MFE, integrations, NATS messaging, Temporal workflows, data, multi-region, security and observability.
- Optional L4 detailed designs: subjects/streams, workflows, schemas, deployment, APIs, adapters, permissions and other implementation detail.
- Use stable diagram IDs such as `LB-V3-L1-01`, `LB-V3-L2-01`, `LB-V3-L3-04`, with each level visibly zooming into a concept from the level above.

## Viewpoint and modelling rules

- Each diagram should answer one primary architectural question; do not make L1/L2 mega-diagrams.
- Separate business tenancy from geography: region is workload/data placement; tenant is business authority/security isolation; organisation is participation; user membership and context further scope access.
- The participant hierarchy is Platform → Operator/Market → Organisation → User. Customer/shipper, transporter and possibly broker/agent are organisation types; each organisation contains admins, operations, finance and other roles.
- Linebooker is the product/marketplace-operating business and tenant authority. Linebooker Tech is the technology arm that provides and operates the logistics SaaS platform. Within Linebooker, Finance includes Creditors and Debtors teams, while Controllers perform logistics control and follow-up.
- Distinguish human actors and business participants from external systems:
  - Human actors: platform admins, operator users, customer/shipper users, transporter users and drivers.
  - Business participants: marketplace operators, customer/shipper organisations, transporter organisations, brokers/agents.
  - Operational integrations: telematics, fleet tracking and GPS.
  - Enterprise integrations: ERP, finance and accounting systems.
  - Platform dependencies: WorkOS/identity.
  - Utilities: maps, geocoding, payments and notifications.
- Show external systems at L1, an explicit integration boundary at L2, and separate integration concern/design views at L3/L4.
- L1 must show a governed external data API for authorised systems to extract scoped reporting data; it is not direct database access.
- Organise functional architecture by business capabilities first (marketplace, orders/loads/trips, fleet, tracking, documents/compliance, billing/settlement), not deployable/service names.
- Use dedicated non-functional views for security, data, deployment, multi-region, HA/DR, residency and observability.
- Long-lived logistics processes should receive workflow/state/sequence views, with Temporal shown as workflow coordination and NATS as commands/events rather than interchangeable storage technologies.
- Data views must distinguish service-owned PostgreSQL transactional truth, JetStream event history, Temporal workflow state, projections/KV read models, and object storage for POD and other documents.

## Proposed catalogue baseline

- L0 Architecture Atlas and Diagram Sitemap (`LB-V3-L0-01`)
- L1 System & Platform Overview
- L2 Logical & Technical Architecture
- L3 Participant & Tenancy
- L3 Functional Domains
- L3 Application / MFE
- L3 External Integration
- L3 Messaging / NATS
- L3 Workflow / Temporal
- L3 Data
- L3 Multi-Region
- L3 Security & Identity
- L3 Observability
- L3 Financial Architecture
- L3 Deployment & Services
- L4 NATS contracts, workflows, data schemas, APIs/adapters, deployment specifications and permissions

The discussion recommends defining an Architecture Structure / Diagram Catalogue before the next generation round, then treating L1 and L2 as navigational views into the derived diagrams.
