---
name: linebooker-refdata-layering-model
description: Codex's conceptual three-layer refdata model for Linebooker/trucking (platform canonical / tenant overlay / organisation master data), candidate dataset table, and publishing rules — complements the code-level inventory in linebooker_v2_refdata_candidates
metadata:
  type: project
---

Codex response (2026-08-11) to a conceptual framing question — "given the snapshot showing refdata for tenants, what are data candidates in a logistics/trucking app, focused on but not exclusive to Linebooker." This is a top-down framework, distinct from [[linebooker_v2_refdata_candidates]]'s bottom-up inventory of the actual V2 codebase — the two should be read together.

**Three-layer model:** (1) platform canonical reference data — stable shared vocabularies, meaning + stable identifier only; (2) tenant-specific enablement/overlay — enabled flag, display name/order/icon, default selection, required/optional, aliases, legacy-code mapping, regional applicability; (3) organisation-owned master data — actual operational records (fleets, drivers, SKUs, sites) that may *map to* canonical codes but aren't refdata themselves. Central principle: **platform defines meaning, tenant defines availability/presentation, organisation defines operational usage.**

**Highest-priority Linebooker candidates** (first extraction tranche, in order): vehicle taxonomy + attributes, commodity taxonomy, load/measurement units, geography, document/compliance types, additional-charge types, special-instruction/equipment types, tracking-provider registry. Matches [[linebooker_v2_refdata_candidates]]'s Tier 1 finding independently — corroborating signal, not just Codex's opinion.

**What should NOT be treated as refdata**, even though it looks similar: loads/addresses, bids/allocations/tenders, rate sheets and diesel adjustments, actual vehicles/fleet availability, tracking positions, PODs, payments/invoices, user accounts/permissions, contracts themselves, saved UI filters. Nuance worth keeping: a *general geographic corridor classification* can be refdata, but a specific named lane ("Woolworths Montague Gardens → Centurion DC") is organisation master data, and the *contracted rate* for that lane is transactional — three different scopes stacked on what looks like one concept.

**Publishing/versioning rules** (design constraints for refdata-service given hard NATS/DB tenant isolation, no live cross-account queries): use globally stable codes/UUIDs, never tenant-local numeric IDs; never reuse a retired code; deprecate, don't delete; export immutable checksummed dataset versions; namespace tenant extensions (e.g. `tenant.linebooker-za.vehicle.*`); separate canonical fields from overlay fields explicitly; **snapshot the selected code + display name onto historical transactions** so old records stay intelligible after catalog changes later. This last point is a gap neither this repo's BUSINESS_RULES nor the V2 inventory currently addresses — worth checking against refdata-service's event/projection design.

Suggested example code shape from the response: `vehicle.tautliner.superlink.standard` (dot-hierarchical, human-legible, parent-child derivable from the string) — contrast with this repo's own `{entityType}.{id}` KV key convention (CLAUDE.md) before adopting wholesale.
