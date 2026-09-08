---
name: linebooker_truck_types_open_work
description: truck/vehicle types exist only as a one-off preview seeder (114 rows, 4-deep hierarchy) — not wired into refdata Seed(); user flagged 2026-09-08 that this needs proper handling later
metadata:
  type: project
---

Flagged by the user on 2026-09-08 as work to be handled at some point. No
implementation was asked for; this is the record of where truck types stand.

**What exists today**

- `demos/01-dictionary/backend/refdata-service/cmd/seed-vehicle-types/` — a
  one-off, standalone preview seeder. Its own header says it is **NOT wired
  into `refdata/seed.go`'s `Seed()`** and does not run at service startup. It
  answers "what would linebooker's vehicle-type data look like as a
  refdata-service dictionary type?" and nothing more.
- `data.go` holds **114 rows**, generated from V2's `vehicle_type_entity`
  table (`linebooker_v2`, `docker-linebooker-mysql-1`) plus
  `VehicleType.java`'s `getCategory()` switch.
- It seeds over refdata-service's **REST admin API** (never Postgres direct),
  so it walks the same validation path as an admin-UI user. Re-runnable:
  everything upserts except item registration (BR-D01), whose 409 it treats as
  "already there".
- `vehicleTypeCategorySeed` mirrors V2's `VehicleTypeCategory` enum as its own
  small domain-enum type — `DOUBLE_TRAILER`, `SINGLE_TRAILER`, `RIGID`,
  `PARENT_GROUP` — so category is a typed reference (BR-D05) instead of a Java
  switch.
- Consumer side: `organizations-service`'s
  `organizations/internal/domain/fleet_asset.go` — `FleetAsset.VehicleTypeCode`
  is required (BR-TP13) and its existence in refdata's vehicle-type corpus is
  BR-TP14, checked by the Phase 26d adapter, not the domain.

**Shape of the data (why it is not a flat lookup table)**

- **Self-referencing hierarchy up to 4 levels deep** via `ParentCode`, e.g.
  `TAUTLINER` → `TAUTLINER_SUPERLINK` → `..._STANDARD` → `..._STANDARD_6_BY_12`.
  Interior nodes carry category `PARENT_GROUP`; only leaves are selectable
  types.
- `Display` (short, e.g. "6m x 12m") and `FullDisplay` (e.g. "Tautliner
  Superlink Standard 6m x 12m") are both carried — a leaf's short label is
  meaningless without its ancestors.
- `OrderIndex` is a **float** (4.1, 4.2 …) because V2 inserted types between
  existing ones without renumbering.
- `Icon` is set only on top-level parents and points at **external GCS URLs**
  (`https://storage.googleapis.com/linebooker-qa-public/truck-icons/...`).

**Open questions to settle when this is picked up**

- Does vehicle-type become a real seeded dictionary type in `Seed()`, or stay
  admin-managed data? Related: [[linebooker_v2_refdata_candidates]] and
  [[linebooker_refdata_layering_model]] — it looks like platform-layer refdata
  with tenant override potential.
- BR-TP14 fleet-asset validation needs the corpus present **in the tenant's
  own context**, not the `linebooker` preview context — the seeder's
  `-context acme` flag is the current workaround.
- Externally hosted icons need a home (refdata theme storage — see
  [[refdata_v3_ladder_placement]]).
- Cardinality/ownership context: [[linebooker_trading_partners_term_and_fleet_cardinality]]
  (Transporter→truck one-to-many, plus the unresolved
  `FleetAssetEntity.subcontractingOwner` subcontracting nuance).
