---
name: linebooker-registration-ui-placement
description: Trading-partner Registration (Shipper/Transporter business records) belongs in Admin UI under a new "Trading partners" section, not in RefData UI — it's organisation-owned master data that consumes refdata lookups, not a vocabulary itself
metadata:
  type: project
---

Design discussion 2026-08-13, continuing from [[linebooker_platform_vs_tenant_service_split]] and [[linebooker_shipper_vs_customer_naming]]. User asked whether Shipper/Transporter registration (the `Registration` service) should live in the Admin UI or the RefData UI.

**Decision direction: Admin UI**, under a new **"Trading partners"** nav category (the collective term recommended for Shipper+Transporter — see [[linebooker_trading_partners_term_and_fleet_cardinality]]), not inside RefData UI.

**Reasoning:** applies the existing [[linebooker_refdata_layering_model]] test directly — RefData UI manages stable shared vocabularies (`VehicleType`, `DocumentType`, `CommodityCategory`) with no PII and no identity of a specific real-world business. A trading-partner registration (KYC, compliance docs, VAT number, fleet) is a **named business record** — organisation-owned master data, explicitly one of the "looks like refdata but isn't" examples already on record (`linebooker_refdata_layering_model`'s "what should NOT be treated as refdata" list names "user accounts/permissions" and "actual vehicles/fleet availability" directly). It *consumes* refdata heavily (VehicleType/DocumentType/Country pickers in the registration form) but consuming a lookup doesn't make the consumer part of the lookup service.

**Structural precedent:** mirrors [[tenant_service_separation_decision]] exactly — that decision kept `accounts-service` out of `refdata-service` for the same reasons (PII/compliance sensitivity vs. no-PII lookups; rare onboarding writes vs. high-frequency reads; different blast radius), merging only at the Admin UI layer. Registration's identity/KYC half sits on the Accounts side of that line, not the Refdata side. Admin UI already merges Accounts + Refdata screens into one frontend without merging backends — adding a "Trading partners" section alongside Accounts is the same pattern, not a new one.

**How to apply:** per [[design_discussion_vs_implementation_signal]], discussion-stage only — nothing built. When ready to implement, this reinforces [[linebooker_platform_vs_tenant_service_split]]'s call that Registration's identity/KYC half is platform-level (lives with Accounts), while any tenant-membership half stays wherever tenant-scoped admin screens live.
