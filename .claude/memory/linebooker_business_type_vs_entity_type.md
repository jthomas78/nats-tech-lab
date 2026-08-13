---
name: linebooker-business-type-vs-entity-type
description: V2's BusinessEntity has two orthogonal classification fields — businessType (role: CUSTOMER/TRANSPORTER/OPERATOR/INTEGRATOR) and businessEntityType (legal structure: COMPANY/CLOSED_CORPORATION/TRUST/SOLE_TRADER_INDIVIDUAL/GOVERNMENT/PARTNERSHIP/OTHER) — "Company" is not a third marketplace role
metadata:
  type: project
---

Confirmed 2026-08-13 against `/Users/jeremy/dev/github/linebooker/linebooker/backend/src/main/java/com/linebooker/console/domain/` plus the live public site (linebooker.com homepage, `/for-transporters`), in response to a question about what "customer, transporter, company" mean in Linebooker terminology.

**Two separate, orthogonal fields on every `BusinessEntity`:**
- `businessType` (enum `BusinessType`) — the marketplace **role**: `CUSTOMER | TRANSPORTER | OPERATOR | INTEGRATOR`.
- `businessEntityType` (enum `BusinessEntityType`) — the **legal structure** it's registered as: `COMPANY | CLOSED_CORPORATION | TRUST | SOLE_TRADER_INDIVIDUAL | GOVERNMENT | PARTNERSHIP | OTHER`.

"Company" is a value of the *second* field, not a third role alongside Customer/Transporter — it specifically means a registered corporate entity (e.g. Pty Ltd), as distinct from a Close Corporation, Trust, Sole Trader/Individual, Government department, or Partnership. Any Customer or any Transporter can be any of these legal-entity types; the two fields are independent. This is exactly the "generic Organisation + type discriminator" pattern noted in [[v3_tenancy_axes_decision]], just with the legal-structure dimension not yet named as its own axis there.

**Public-site confirmation of roles (homepage + `/for-transporters`):** only **Customer** (shipper, "Register as a Customer") and **Transporter** (transport company/fleet owner, "Register as a Transporter") are marketed as account types — the two sides of the marketplace. **Operator** on the public site means Linebooker's *own* staff ("professional transport operators... managing daily operations for customers") — confirms [[v3_tenancy_axes_decision]]'s existing note that `BusinessType.OPERATOR` = Linebooker staff, not a customer-facing account. **Broker** appears only as the incumbent middleman the platform disintermediates ("removes the need for a transport broker"), never as a platform role. **Driver / Fleet Owner** appear in marketing copy but aren't modeled as separate account types — they're part of a Transporter's own organization. **Integrator** exists in the code enum but has no public-site marketing language found — likely a technical/API-partner classification rather than a marketed persona.

**How to apply:** when modeling org/business-entity-type as refdata (per [[linebooker_v2_refdata_candidates]]'s Tier-1 list and [[linebooker_refdata_layering_model]]'s three-layer framework), keep `BusinessType` (role) and `BusinessEntityType` (legal structure) as two independent lookup tables/dimensions — don't collapse "Company" into the role enum.
