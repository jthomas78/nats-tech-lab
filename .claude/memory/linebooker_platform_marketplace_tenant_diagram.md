---
name: linebooker-platform-marketplace-tenant-diagram
description: 2026-08-13 hand-drawn diagram of PLATFORM (Refdata/Accounts/Operator Admin UI/Marketplace→Tenders,Bids) vs per-tenant (Tenant Admin UI/Trips/LineBooker UI) for linebooker.com — draft, not yet a decision
metadata:
  type: project
---

**Diagram drafted 2026-08-13** (shared as an image, not yet in any repo file) sketching the top-level relation between PLATFORM, MARKETPLACE, and TENANT for the real Linebooker trucking-logistics app (linebooker.com) — a step beyond [[v3_tenancy_axes_decision]]'s axes discussion, now shaping actual service/UI groupings.

**Structure shown:**
- **PLATFORM** box contains: `REFDATA`, `ACCOUNTS` (both maroon = backend/domain services), and `OPERATOR ADMIN UI` (navy = UI app) — plus a separate `MARKETPLACE` service with `TENDERS` and `BIDS` drawn underneath it, joined by a hollow-triangle UML generalization arrow (i.e. Tenders and Bids are drawn as *subtypes/is-a* of Marketplace, not siblings calling it).
- **TENANT 1** and **TENANT 2** are drawn as separate boxes, each containing the same three elements: `TENANT ADMIN UI` (navy), `TRIPS` (maroon), and `LINEBOOKER UI` (navy) — i.e. two distinct UI apps per tenant (an admin-facing one and an end-user-facing one) plus one per-tenant domain service.
- Color coding is consistent with this repo's own diagram convention: maroon/red = backend domain service, navy/blue = frontend UI application.

**How this extends prior decisions:** confirms the [[v3_tenancy_axes_decision]] premise that tenant = business, not region (two tenant boxes side by side, structurally identical). New information not previously recorded: (1) `Trips` is drawn as a **per-tenant** service, not a platform/marketplace-level one — contrast with `Tenders`/`Bids` which are platform-level; (2) `Marketplace` is modeled as a supertype with `Tenders` and `Bids` as subtypes, which is a specific claim about shared behavior/schema between tendering and bidding that hasn't been discussed before; (3) each tenant has two separate UIs (`Tenant Admin UI` + `LineBooker UI`) rather than one.

**Open questions the diagram raises (not yet asked/answered):** whether the Tenders/Bids generalization is meant literally (shared base class/table) or just visual grouping; whether `Trips` being per-tenant means it's Postgres/NATS-account-scoped per tenant (consistent with tenant = NATS account) while `Marketplace`/`Tenders`/`Bids` cross tenant boundaries by nature (a marketplace inherently spans multiple tenants bidding/tendering against each other) — which would be an interesting exception to "tenant = hard NATS account wall" worth reconciling with [[nats_scoped_signing_keys]] cross-tenant patterns; what `Accounts` vs `Operator Admin UI` vs each tenant's `Tenant Admin UI` map to in terms of who authenticates as what.

**How to apply:** per [[design_discussion_vs_implementation_signal]], this is a draft diagram under discussion — do not start building services/UIs from it. User said questions will follow; wait for those rather than pre-empting them.
