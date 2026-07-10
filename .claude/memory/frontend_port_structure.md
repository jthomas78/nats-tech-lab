---
name: frontend-port-structure
description: frontend-port/ layout, component responsibilities, and store getter conventions after the Ship Management split
metadata:
  type: project
---

`demos/01-dictionary/frontend-port/` (dev port 5174) is titled **"Ship Management"** in `App.vue` (renamed from "Port Management" — the title now spans both fleet-wide and port-scoped views).

**Layout — two stacked `.group` sections in `App.vue`:**
1. `FleetPanel.vue` — fleet-wide, read-only, NOT gated on `store.port`. Status filter `Select` (All/Docked/In transit) over `store.allShips`. Shows every ship regardless of which port is selected, including ones docked elsewhere — this replaced an earlier "ships in transit only" design that had a blind spot (docked-elsewhere ships were invisible).
2. "Port Management" group — wraps `TerminalPanel.vue` + `ShipsAtPortPanel.vue`, both still gated on `v-if="store.port"`. The port `<Select>` + "Add port" `+` button live in this group's header now, not the topbar (moved out because the Fleet group above doesn't need it). Topbar keeps only the Fleet/context `<Select>`, connection status `Tag`, and theme toggle — those are fleet-scoped, not port-scoped.

**`stores/port.js` getter conventions:** `dockedShips` and `allShips` both sort by `shipID`; filtering (docked vs in-transit vs by-port) is done in the getter or the component, never mutating state. `manifestFor(shipID)` is a join on `onShipID`, valid for ships at sea too (a container stays on a ship's manifest after departure).

**Operations are localized on the panel whose data they act on** (not a standalone Operations panel — that was removed in an earlier UX pass): Register/Load on `TerminalPanel.vue`, Arrive/Depart/Unload on `ShipsAtPortPanel.vue`. Unload is inline per manifest row (enabled only when `container.destPort === store.port`), not a separate ship/container picker — see [[stale-select-value-on-filter-change]] for why the picker version was buggy.

**How to apply:** When adding a new panel or operation to this frontend, follow the existing pattern — gate port-scoped panels on `store.port`, keep fleet-wide views ungated, and put the operation's controls on the panel showing the data it acts on.
