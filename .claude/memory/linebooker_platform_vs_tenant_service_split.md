---
name: linebooker-platform-vs-tenant-service-split
description: which of Refdata/Accounts/Registration/Marketplace/Payments genuinely belong at PLATFORM level vs tenant level, given tenant = the marketplace-operating business — corrects the original hand-drawn diagram's placement of Marketplace under PLATFORM
metadata:
  type: project
---

Design discussion 2026-08-13, continuing from [[linebooker_platform_marketplace_tenant_diagram]] and the end-to-end phase diagrams ([[linebooker_transport_execution_phase_naming]], [[linebooker_payments_settlement_phase]]). User proposed Refdata, Shipper/Transporter registrations, and Account/user registrations as platform/cross-cutting candidates; asked to confirm against the diagram.

**Confirmed platform, backed by this repo's own built architecture (not speculation):**
- **Refdata** — Phase 21's target partitioning ([[phase21_account_exports_imports]]) explicitly names `refdata-service` as a PLATFORM-account service exported to every tenant via `rpc.*.refdata.*` service exports.
- **Accounts & auth** — CLAUDE.md's subject-family rules state `auth-service`/`accounts-service` subjects carry **no** `{context}` because "they administer the tenant axis itself" — the working definition of cross-cutting in this repo's actual architecture.

**Registration needs splitting into two layers, not one service:**
- Platform-level **identity/KYC profile** (who the business is, compliance docs, VAT number) — genuinely cross-tenant if the same real-world Transporter can operate across more than one tenant marketplace.
- Tenant-level **membership** (allowed to participate in *this* tenant's marketplace, credit terms, bidding permissions) — this is the `OrganisationTenantMembership` open decision already sitting unresolved in [[v3_tenancy_axes_decision]]. Don't collapse the two into one service — same reasoning as [[tenant_service_separation_decision]] (different owners, different blast radius, different lifecycles).

**Two corrections to [[linebooker_platform_marketplace_tenant_diagram]]'s original placement, surfaced here for the first time:**
1. **Marketplace likely does NOT belong at platform level.** [[v3_tenancy_axes_decision]]'s core premise is tenant = *the marketplace-operating business itself* — which implies each tenant runs its own marketplace among its own participants, not one shared platform-wide marketplace. Under that reading, `Marketplace`/`Tenders`/`Bids` should be tenant-scoped, not PLATFORM as originally drawn — unless cross-tenant bidding is deliberately wanted, which would require the same NATS export/import mechanism Phase 21 built for refdata, and would be a genuine, deliberate exception to "tenant = hard NATS account wall."
2. **Payments is very likely tenant-level, not platform.** Money movement, invoicing, and VAT registration bind to a specific legal entity — the tenant's business, not Linebooker's platform account. The *reference data* Payments depends on (VAT rates, currency codes) is platform Refdata; the settlement engine/ledger itself is not.

**Net platform layer, narrower than "everything administrative":** Refdata + Accounts/Auth + the identity half of Registration. Marketplace, Payments, and the membership half of Registration are tenant-scoped candidates pending explicit confirmation.

**How to apply:** per [[design_discussion_vs_implementation_signal]], discussion-stage only. When the user is ready to settle this, update [[linebooker_platform_marketplace_tenant_diagram]]'s box placement and record a decision note (mirroring how [[tenant_service_separation_decision]] was eventually written up).
