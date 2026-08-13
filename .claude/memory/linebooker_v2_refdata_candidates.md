---
name: linebooker-v2-refdata-candidates
description: Which Linebooker V2 data should move to refdata-service — the enum+table duplicates, VehicleTypeEntity's per-customer FK validating the context tree, and what V2 lacks (versioning, i18n)
metadata:
  type: project
---

Inventory taken 2026-08-11 over the V2 repo (`/Users/jeremy/dev/github/linebooker/linebooker`, entities under `backend/src/main/java/com/linebooker/console/domain/`).

**Tier 1 — four vocabularies that exist *twice*, as both an enum and a table.** That duplication is V2 telling you it wanted a reference service: `VehicleTypeEntity`+`VehicleType` (~120 constants, **84 seed inserts** across `AddNewTruckTypes.sql`, `AddCarCarrierTruckType.sql`, `AddNewMoffettVehicleType.sql`, `AddRackedVolumax.sql`, `SetVehicleTypeImages.sql`), `DocumentTypeEntity`+`DocumentTypes` (22), `CommodityCategoryEntity`+`CommodityCategory` (13), `LoadUnitTypeEntity`+`LoadUnitType` (7, table has no FK — pure picklist over REST). Plus `JobTitleEntity` (42 seed inserts). The real tell is not the duplication but that adding a truck type needs a migrate script and a deploy — that has happened 7+ times.

**The strongest validation of the POC's context tree:** `VehicleTypeEntity` carries a **`customer_profile_entity_id` FK** — a global truck-type catalogue that is simultaneously per-customer, in the same table, unseparated. That is exactly refdata-service's global-root → tenant → BU inheritance model, solving a real V2 problem rather than an invented one.

**Tier 2 — picklists still trapped as enums:** `DeclineReasonType`, `PodRejectionReason`, `FailType`, `AdditionalCostType` (16, with display strings), `AdditionalCostTransactionType`, `SpecialInstructionType`, `TrackingProviderNames` (36 telematics providers), `BusinessEntityType`, `ReferralSource`, `TemperatureType`, `VatRate`, `EstimationPeriodType`, `LoadAlertType`, `MatchCriterion`. `PolicyChangeEnum` is one constant with an `effective_date` table behind it — pure data in an enum costume, and the only non-pricing V2 lookup with temporal semantics.

**Geography — split the hierarchy from the geometry.** V2 has two never-linked hierarchies: legacy `CountryEntity → RegionEntity → TownEntity` (`CountryEntity` has **no ISO code column**, just a name) and modern `GeoAreaEntity` (`geo_areas`; `level`, `countryCode`, `parentId`, PostGIS `center`+`geom`). Names/ISO codes/parent/level are refdata; `MultiPolygon` boundaries are megabytes per row and belong in a spatial service — pushing them through refdata's KV cache would be the first thing to blow up.

**Config is a candidate but needs a line drawn.** `ConfigEntity` is a flat global key/value store with **no business dimension**, so per-tenant settings leaked into bespoke tables (`PodNotificationConfigEntity`, seeded per-customer by `MigrateSFTPConfigForPepsi.sql`). Context-scoped config fixes that — but that table also holds SFTP passwords, and **secrets must not go in refdata** (shared PLATFORM account; `refdata.contexts.tenant` is documented as not a security boundary, BR-D34).

**Two refdata features are net-new, not ports:** (a) **versioning** — no pure lookup in V2 is versioned or effective-dated, corrections are destructive migrate SQL, so draft/publish/rollback has no legacy semantics to preserve; (b) **localization** — V2 has zero locale bundles or translation tables, but *does* have `messages.properties` vs `messages-thornlands.properties`, i.e. **white-label brand wording, not language**. Expect l10n's first real customer to be per-tenant label overrides for white-labelling.

**Explicit non-candidates:** the ~90 state-machine enums the code switches on (`LoadStatusType`, `LoadProgressStatus`, `LoadHistoryType`, `LoadEventSnapshotActionType`, `BidStatusType`, `PricingType`, `AllocationType`, `TenderStatus`, `PodStatusType`, all `*SortByType`). Making these data lets a business user add a status no handler knows about — same test as [[br_classification_heuristic]] and CLAUDE.md's replay rule.

**Unresolved: is refdata global or regional?** If PLATFORM is per-region, each region holds its own copy of the genuinely global corpus and needs a replication path that does not exist. Refdata is probably **three tiers** — global corpus / regional data / tenant-BU overlay — and the context tree already expresses the bottom two. Settle before `Multi-Region-Plan.md` leaves DRAFT. See [[v3_tenancy_axes_decision]].
