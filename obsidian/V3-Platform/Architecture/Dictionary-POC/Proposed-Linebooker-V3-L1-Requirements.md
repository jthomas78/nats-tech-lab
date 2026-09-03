# Proposed Linebooker V3 Architecture - L1 Requirements

This register is the maintained source of truth for the content of
`LB-V3-L1-01 - System and Platform Overview`. Update it before changing the L1
HTML or PDF so the diagram remains traceable to an explicit requirement.

## Status legend

- **Included** - visible in the current L1 artefact.
- **Derived** - acknowledged at L1 and expanded in L2/L3.
- **Open** - requires business, legal or architecture confirmation.

## Requirements

| ID | Status | Requirement | L1 representation |
|---|---|---|---|
| L1-001 | Included | Explain what Linebooker is and who or what participates. | One A3 system and platform landscape. |
| L1-002 | Included | Separate human actors, business participant organisations and external systems. | Three labelled ecosystem boundaries. |
| L1-003 | Included | Distinguish Linebooker from Linebooker Tech. | Linebooker is the product/marketplace-operating business and tenant authority; Linebooker Tech is the technology arm providing and operating the SaaS platform. |
| L1-004 | Included | Show Linebooker Finance as people in a department. | Separate Creditors and Debtors actor tiles. |
| L1-005 | Included | Show Linebooker Controllers as people in a department. | Logistics control and follow-up actor tile. |
| L1-006 | Included | Show Linebooker Tech operational people. | Platform administration and SRE actor tile. |
| L1-007 | Included | Show participant users outside Linebooker. | Shipper users and transporter/driver teams. |
| L1-008 | Included | Show participating organisations. | Shipper and transporter organisations participate inside the marketplace tenant. |
| L1-009 | Included | Keep tenant, organisation and geography separate. | Tenant is marketplace authority; organisation is participation; country/region is placement. |
| L1-010 | Included | Show external platform, operational and enterprise dependencies. | WorkOS/IdP, fleet/GPS/telematics/maps, ERP/finance/banks/payments/notifications/regulatory systems. |
| L1-011 | Included | Provide APIs for authorised external systems to extract data for their own reporting. | Governed external data API for scoped reporting extracts; no direct database access. |
| L1-012 | Included | Show role-specific applications and channels. | Shipper, transporter, driver, tenant/admin, operator and external API experiences. |
| L1-013 | Included | Show the principal trucking logistics lifecycle. | Tendering, loads, trips, routes, fleet, tracking, documents, claims and settlement. |
| L1-014 | Included | Treat financial operations as a first-class domain. | Pricing, tax, fees, invoices, payments and transporter settlement. |
| L1-015 | Included | Show platform foundations without L2 implementation detail. | Identity/access, NATS, Temporal, service data, evidence storage and observability/audit. |
| L1-016 | Included | Show the initial country and regional footprint. | Shared Cape Town, sovereign Botswana and shared Sydney placement cells. |
| L1-017 | Included | Show country-specific regulatory storage and transit requirements. | Botswana example keeps regulated compute, data, transit, logs, keys, backups and recovery in-country. |
| L1-018 | Included | Show data localisation and controlled cross-border movement. | Placement is policy controlled; detailed classifications and exceptions are derived views. |
| L1-019 | Included | Show currency context without deriving it from user location. | ZAR, BWP and AUD are policy defaults; marketplace, contracting legal entity and contract select transaction currency. |
| L1-020 | Included | Make L1 a navigation map into deeper architecture. | L2/L3 concerns are named but their mechanics are deliberately omitted. |
| L1-021 | Included | Deliver the architecture document in editable and distributable formats. | Matching HTML and A3 PDF outputs. |
| L1-022 | Included | Make security, regulatory controls and auditable history visible at L1 without implying universal event sourcing. | Dedicated `Security, residency + immutable audit` platform block covering tenant separation, RBAC/ABAC, encryption, masked logs and country policy. |

## Derived requirements

| ID | Target view | Detail deferred from L1 |
|---|---|---|
| L1-D01 | L2 Logical and Technical Architecture | Application shell/MFE composition, integration boundary, control plane, services, NATS, Temporal, data and deployment structure. |
| L1-D02 | L3 External Integration | API authentication, tenant/organisation scopes, data products, schemas, filtering, pagination, rate limits, audit, masking and export formats. |
| L1-D03 | L3 Participant and Tenancy | Linebooker/Linebooker Tech responsibilities, organisation memberships, department roles and delegated authority. |
| L1-D04 | L3 Financial Architecture | Currency selection, tax, rating, invoicing, creditors, debtors, payments, reconciliation and settlement. |
| L1-D05 | L3 Security and Residency | Data classification, country rules, encryption, masking, cross-border exceptions, retention, backup and recovery. |

## Open confirmations

- Confirm whether Linebooker and Linebooker Tech are separate legal entities,
  brands, divisions or operating organisations; L1 currently models them as two
  organisational arms with different responsibilities.
- Confirm whether brokers/agents are active L1 participants or a future derived
  participant type.
- Confirm which reporting data products external consumers may extract and
  whether bulk/asynchronous exports are required in addition to request APIs.

## Change log

- 2026-09-03 - Added the explicit L1 security, residency and immutable audit
  platform block. The wording promises an auditable business history without
  claiming that every domain reconstructs state through event sourcing.
- 2026-09-03 - Initial register created from the architecture discussion and L1
  generation round. Added Linebooker Finance (Creditors and Debtors), Controllers,
  Linebooker Tech, and the governed external reporting/data API requirement.
