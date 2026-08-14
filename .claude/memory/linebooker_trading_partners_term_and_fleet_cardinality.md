---
name: linebooker-trading-partners-term-and-fleet-cardinality
description: collective term for Shipper+Transporter is "Trading partners" (industry-standard, freight/EDI); Transporter-to-truck relationship confirmed one-to-many via V2's FleetAssetEntity FK, with a subcontractingOwner nuance layered on top
metadata:
  type: project
---

Design discussion 2026-08-13, continuing from [[linebooker_shipper_vs_customer_naming]]. User asked (a) what Shippers and Transporters are collectively called, wanting an Admin UI nav category name for a new `Registration` service, and (b) confirmed their presumption that Transporter-to-truck is one-to-many.

**Collective term: "Trading partners"** — the standard freight/EDI/TMS industry term for both sides of a shipper-carrier relationship; stays role-neutral the same way Shipper/Transporter does. **"Participants"** is a solid simpler alternative if less freight-jargon is wanted. Avoid "Users"/"Accounts" — those already mean something specific in the platform layer (login identity, NATS account) and would collide. No existing precedent either way: `grep -ril "trading.partner|counterpart" backend/src/main/java` over V2 returns nothing, same "genuinely open, not a hidden legacy term" situation as [[linebooker_shipper_vs_customer_naming]] and [[linebooker_transport_execution_phase_naming]].

**Admin UI category recommendation:** name the nav category after the collective term — "Trading partners" (or "Participants") — with `Registration` as the first screen inside it, giving a home for what naturally follows later (compliance docs, KYC status, fleet screens) without renaming again. This is the decision [[linebooker_registration_ui_placement]] built on; that file's own cross-reference for "Trading partners" incorrectly pointed at `linebooker_shipper_vs_customer_naming` — corrected to point here instead. **As built (2026-08-13), the single `Registration` screen became two — `Shippers` and `Transporters`** — under the "Trading partners" eyebrow, on the user's call that role-specific fields will keep diverging (fleet assets and `GOODS_IN_TRANSIT` already are transporter-only), making the split cheaper now than later. The category name itself held up exactly as intended. See [[linebooker_trading_partner_phase_v1_scope]].

**Transporter-to-truck cardinality confirmed one-to-many**, checked against V2's actual FK rather than assumed: `FleetAssetEntity` (the real truck/trailer record — registration number, VIN, make/model, tracking credentials) carries `@ManyToOne transporterProfileEntity`, i.e. many trucks point to one Transporter. Note `CustomerVehicleEntity` is a red herring for this question — it links `CustomerProfileEntity` to `VehicleTypeEntity` (a Shipper's *preferred vehicle types* for their loads), not a Transporter's fleet.

**Nuance layered on top, not yet resolved:** `FleetAssetEntity.subcontractingOwner` is a self-referencing FK to another `FleetAssetEntity`, so a truck's *legal owner* and the *Transporter operating it on a given job* can diverge via subcontracting. That's a secondary relationship on top of the simple one-to-many, not a replacement for it — fine to defer for the POC's first pass, just flagging it exists so it isn't rediscovered as a surprise later.

**How to apply:** per [[design_discussion_vs_implementation_signal]], discussion-stage only. Feeds directly into the "Trading Partner" implementation phase — see the business-rules-first prompt drafted for that phase, which should confirm whether subcontracting is in scope for v1.
