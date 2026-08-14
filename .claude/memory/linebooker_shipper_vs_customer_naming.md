---
name: linebooker-shipper-vs-customer-naming
description: "Customer" in V2 is a vendor-relationship word (implies buying) but the role actually submits tenders/loads (a procurement action) — "Shipper" is the industry-standard, function-based alternative, paired with "Transporter"; ADOPTED 2026-08-13 as the V3 term; confirmed zero precedent for Shipper/Carrier/Consignor anywhere in V2
metadata:
  type: project
---

Design discussion 2026-08-13, prompted by the user noticing "Customer" feels wrong because "usually a customer buys something" whereas in this model the role "submits tenders."

**The mismatch:** `BusinessType.CUSTOMER` (see [[linebooker_business_type_vs_entity_type]]) names the demand-side role by its *commercial relationship to a seller* (whoever pays/buys), while `TRANSPORTER` names the supply-side role by its *market function* (moves freight). That's an inconsistent naming axis across the two sides of one marketplace. What the role actually *does* on the platform — submit a tender or post a load — is a procurement/solicitation action (soliciting competing bids), not a retail purchase action, so "Customer" imports the wrong mental model (catalog + checkout) for what's actually a reverse-auction/RFQ mechanic (see [[linebooker_bid_tender_allocation_rules]]).

**Confirmed via grep, no existing precedent either way:** `grep -ril "shipper|carrier|consign" backend/src/main/java` over the full V2 repo returns nothing for shipper/consignor, and only two incidental, unrelated hits for "carrier" (`MatchScoringService.java`, `VehicleType.java` — not a role name). So "Customer" is the only term V2 ever used; there's no hidden "correct" legacy term being overridden by adopting something else for V3.

**Recommended alternative: Shipper**, paired with the existing "Transporter" (classic freight pairing is Shipper/Carrier; since Linebooker already committed to "Transporter" over "Carrier", "Shipper/Transporter" keeps both sides role-based and parallel in register).

**Caveat flagged, still not resolved:** "who submits the tender" and "who pays for the load" could diverge in some freight setups (a broker submitting on a shipper's behalf, freight-collect terms). If V2's `CustomerProfileEntity` always collapses those into one `BusinessEntity`, "Shipper" is a clean rename. If V3 ever needs to separate "who requests" from "who pays," that's a second axis to name separately, not fold into one term. **Phase 26 collapses them deliberately** — one `TradingPartner` record per business, no payer axis — so this caveat survives the adoption below and would surface as a new axis, not a rename.

**ADOPTED 2026-08-13 — no longer a recommendation.** [[v3_tenancy_axes_decision]]'s Organisation axis now reads `Shipper | Transporter | Operator-self | Integrator`, and the Phase 26 plan section in `.claude/plans/Main-POC-Plan.md` builds `PartnerType` = `SHIPPER` | `TRANSPORTER` on the `TradingPartner` aggregate ([[linebooker_trading_partner_phase_v1_scope]]).

**How to apply:** use "Shipper" in all V3 naming — domain types, subjects, UI copy, docs. V2 source still says `CUSTOMER` (`BusinessType`, `CustomerProfileEntity`, `CustomerVehicleEntity`); read those as this role and rename at the port boundary rather than propagating the V2 term. [[linebooker_business_type_vs_entity_type]]'s summary of V2's enum is describing V2 and is correctly left unrenamed.
