---
name: linebooker-transport-execution-phase-naming
description: naming suggestions for the post-marketplace phase (dispatch/collection/in-transit/delivery+POD) — recommended "transport execution" (TMS-industry term, pairs with Marketplace as sourcing) over "fulfillment" (e-commerce-borrowed, still fine) or "operations/dispatch" (too narrow); zero V2 precedent for any of these as a module name
metadata:
  type: project
---

Design discussion 2026-08-13, following the [[linebooker_shipper_vs_customer_naming]] conversation. User asked what the phase after marketplace allocation is generally called in logistics, given it groups loads/trucks/POD/collection.

**Confirmed via grep, no existing precedent:** `grep -ril "fulfillment|execution|dispatch|operations" backend/src/main/java` over V2 returns only incidental unrelated hits (e.g. `DateUtils.java`, `SecurityUtils.java`) — no real module/package groups these concerns. Package layout is flat (`domain/`, `service/`, `repository/`, `web/rest/` at top level). `POD` itself is flat too: `PodDocumentEntity`, `PodNotificationConfigEntity`, `PodDocumentEventListener` — no phase-level grouping name exists in the code to defer to, same situation as [[linebooker_shipper_vs_customer_naming]]'s "Shipper" finding.

**Recommended term: "transport execution"** (or "freight execution") — the standard TMS-industry term, and it pairs cleanly opposite "Marketplace": Marketplace = sourcing/procurement (who moves it, at what price), Execution = physically moving it. **"Fulfillment"** (the user's own instinct) is a legitimate alternative, just borrowed from e-commerce/warehousing rather than freight-specific — still widely understood. **"Operations"/"Dispatch"** are narrower — usually just the truck/driver-assignment action, not the whole collection-to-POD lifecycle; fine as a stage label, too narrow as the phase name.

**Four-stage breakdown proposed** (maps the user's named subgroups — loads, trucks, POD, collection — onto a sequence rather than treating them as an unordered bag):
1. **Dispatch** — assign truck & driver to the won load.
2. **Collection** — pickup at origin.
3. **In-transit** — track the load (this stage wasn't explicitly named by the user but is the natural gap between collection and delivery).
4. **Delivery** — capture POD (proof of delivery).

**How to apply:** per [[design_discussion_vs_implementation_signal]], discussion-stage only, nothing built. If "transport execution" is adopted, it becomes the natural third phase alongside Marketplace (sourcing) in the platform-level diagram from [[linebooker_platform_marketplace_tenant_diagram]] — worth updating that diagram's Trips/per-tenant boxes to reflect where dispatch/collection/in-transit/delivery actually live once that's decided.
