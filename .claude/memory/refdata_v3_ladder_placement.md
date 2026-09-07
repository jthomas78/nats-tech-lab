---
name: refdata_v3_ladder_placement
description: refdata's agreed home in the Proposed V3 ladder — data jobs in L3-07, shell theme consumption in L3-03, whole-service story as an L4
metadata:
  type: project
---

`refdata-service` had no place in the Proposed V3 L0-L4 ladder — only in the
lab's built-system docs (`ARCHITECTURE-DICTIONARY.md`). Agreed placement
(2026-09-04, discussion only — nothing drawn or catalogued yet):

Refdata provides four platform-wide services beyond plain lookup tables:

- Data localization
- Enum and string value translations
- App Shell theming (CSS)
- Centralized configuration store

**Agreed split ("Option A"):**

| Responsibility | Home |
|---|---|
| Data localization | `LB-V3-L3-07 Data Architecture` |
| Enum / string translations | `LB-V3-L3-07 Data Architecture` |
| Centralized config store | `LB-V3-L3-07 Data Architecture` |
| App Shell theme — *storing and serving* the values | `LB-V3-L3-07 Data Architecture` |
| App Shell theme — *consuming and rendering* in the shell | `LB-V3-L3-03 Application / MFE` |
| The whole refdata story in one place | an **L4** under `LB-V3-L3-07` |

**Why:** L3 nodes are *concern* views, not *service* views — none of the twelve
is named after a service, so refdata must not get its own L3. Theming is two
jobs: storage/distribution is a data concern, consumption/rendering is an
application concern. The L4 exists so a reader can still find "what is refdata"
in one document despite the slices being spread across concern views.

**How to apply:** `LB-V3-L3-07` was drawn and became `AVAILABLE` on 2026-09-04 —
its sheet 2 carries the localisation, translation, theme-storage and
configuration-store slices, with a `rendering it is L3-03` pointer on the theme
tile. `LB-V3-L3-03` was drawn and became `AVAILABLE` on 2026-09-04 - its sheet 2
carries the theme-consumption slice as the `Labels and theme` tile, which names
L3-07 as the store, and it also carries the "language belongs to the person,
content belongs to the connection" split. The L4 is
not yet numbered — allocate its ID through the authority, never ad hoc. Follow
`architecture-draughtsman` plus the central authority for the actual work. See
[[proposed_linebooker_v3_architecture_levels]] for the catalogue inventory and
[[linebooker_refdata_layering_model]] for the platform/tenant/org layering
refdata already sits in.
