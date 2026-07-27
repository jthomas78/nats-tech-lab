---
name: frontend-port-structure
description: frontend/seafreight-app/ layout (activity-bar Fleet/Port view split), l10n architecture, component responsibilities, and store getter conventions
metadata:
  type: project
---

**Path note (2026-07-22):** this app was moved and renamed from `demos/01-dictionary/frontend-port/`
to `demos/01-dictionary/frontend/seafreight-app/`, alongside a repo-wide restructuring of
`demos/01-dictionary/` into `backend/{shipping-service,refdata-service}` and
`frontend/{admin,refdata,seafreight-app}` parent folders. The memory filename
(`frontend_port_structure.md`) is unchanged — memory filenames aren't referenced by path from
elsewhere in the repo — but everywhere below now uses the new path/name.

`demos/01-dictionary/frontend/seafreight-app/` (dev port 5174) is titled **"SeaFreight Flow"** in `App.vue` (renamed from "Ship Management" 2026-07-16 — a brand name, so `app.title`'s `en`/`es` seed values are intentionally identical, unlike every other l10n string), spanning both fleet-wide and port-scoped views.

**Layout — activity-bar view switch (superseded the old stacked-sections layout as of Phase 11.10, 2026-07-16):** `App.vue` holds a single `activeView` ref (`'fleet'` | `'port'`); `NavSidebar.vue` (with `IconFleet.vue`/`IconPort.vue`) renders the nav items and drives `v-model="activeView"`. Exactly one of the two `<section>`s renders at a time (`v-if`/`v-else`, `data-testid="fleet-view"` / `"port-view"`) — never both simultaneously. Add more views by pushing onto the `views` computed array, not by introducing a router.
1. Fleet view (`data-testid="fleet-view"`) — `FleetPanel.vue`, fleet-wide, read-only, NOT gated on `store.port`. Status filter `Select` (All/Docked/In transit) over `store.allShips`. Shows every ship regardless of which port is selected, including ones docked elsewhere.
2. Port view (`data-testid="port-view"`) — wraps `TerminalPanel.vue` + `ShipsAtPortPanel.vue`, gated on `v-if="store.port"`. The port `<Select>` + "Add port" `+` button live in this view's header, not the topbar. Topbar keeps only the Fleet/context `<Select>`, connection status `Tag`, and theme toggle — fleet-scoped, not port-scoped.

The topbar subtitle (`.topbar .lab-muted`) switches per view too: `app.subtitleFleet` vs `app.subtitlePort` (two separate l10n keys, split from one shared `app.subtitle` during Phase 11.10).

**Localization (Phase 11.10, implemented 2026-07-16) — Option D: all UI copy lives in refdata, no bundled hand-maintained fallback.** Every user-facing string in `seafreight-app` (~90 literals across `App.vue`/`FleetPanel.vue`/`ShipsAtPortPanel.vue`/`TerminalPanel.vue`) is an `l10n` refdata item (en+es rows in `backend/refdata-service/refdata/seed.go`'s `l10nSeed`) — the sole authored source, routed through `t()`. The bundled cold-paint fallback (`shared/refdata/l10nFallback.en.js`, required by BR-D11) is *generated* from that seed by `scripts/gen-i18n.mjs` (shared parsing logic lives in `scripts/parseL10nSeed.mjs`), wired as a `prebuild` hook, with a CI drift-check (`npm run check:i18n`) failing the build if regenerating produces a diff — this is what keeps the seed authoritative. BR-D16 (`BUSINESS_RULES.md`) is the rule of record; full decision rationale (why D over the other three options considered) is in `.claude/plans/Dictionary-Service-Plan.md`'s Phase 11.10 section — don't re-litigate it if asked to continue this work.

**Locale selection persists** (2026-07-16) — `selectedLocale` in `shared/refdata/useRefdataLabels.js` is initialized from and written back to `localStorage` (key `refdata.locale`, per-origin so `seafreight-app`/`admin` on their different dev ports don't share a choice). Chosen over a URL query param (neither app has a router) or a backend-stored preference (no auth/user concept exists). No validation of the persisted value against the fetched `locales` list — BR-D03's fallback chain already degrades a stale/invalid locale gracefully.

**BR-D19 (2026-07-16) — the translated catalog itself is cached too, not just the locale choice.** Persisting *only* `selectedLocale` surfaced a gap: on reload into a persisted non-`en` locale, the app cold-painted in the bundled English catalog (BR-D11 only ever bundles `en`) for the length of the live refetch, visibly mismatching the locale shown as selected. Fixed by caching the last-successfully-fetched l10n catalog (`useL10nCopy.js`, key `refdata.l10nCache`) and ship-status label map (`useRefdataLabels.js`, key `refdata.shipStatusLabelsCache`) per locale, applied synchronously ahead of the live fetch — in `connect()` for l10n, at module load for ship-status labels (that state doesn't wait for a component to call `connect()`). Unlike the race-condition fix, **this one *is* a BR** (BR-D19 in `BUSINESS_RULES.md`) — BR-D11 is precedent for a frontend-behavior rule living outside the Go domain layer, and this is close kin to it (extends what "cold paint" must render), so it earns the same treatment rather than being memory-only like [[locale-switch-race-condition]].

**Test harness:** `seafreight-app` now has Vitest + `@vue/test-utils` + `happy-dom` (`npm test`, wired into CI in `.github/workflows/seafreight-app.yml`). `src/App.spec.js` mounts `App.vue` with a real i18n instance built from the seed (via `parseL10nSeed.mjs`) and asserts locale-switch, interpolation, pluralization, and the fleet/port no-overlap invariant. Gotcha: resolve `seed.go`'s path via `import.meta.url`/`fileURLToPath`, not `process.cwd()` (breaks under different invocation directories) — and avoid the literal `new URL('...', import.meta.url)` pattern in test files, since Vite's import analysis special-cases that exact syntax for asset resolution and rewrites it to a broken `/@fs/...` dev-server URL; use `dirname(fileURLToPath(import.meta.url))` + `path.resolve` instead.

**`stores/port.js` getter conventions:** `dockedShips` and `allShips` both sort by `shipID`; filtering (docked vs in-transit vs by-port) is done in the getter or the component, never mutating state. `manifestFor(shipID)` is a join on `onShipID`, valid for ships at sea too (a container stays on a ship's manifest after departure).

**Operations are localized on the panel whose data they act on** (not a standalone Operations panel — that was removed in an earlier UX pass): Register/Load on `TerminalPanel.vue`, Arrive/Depart/Unload on `ShipsAtPortPanel.vue`. Unload is inline per manifest row (enabled only when `container.destPort === store.port`), not a separate ship/container picker — see [[stale-select-value-on-filter-change]] for why the picker version was buggy.

**How to apply:** When adding a new panel or operation to this frontend, follow the existing pattern — gate port-scoped panels on `store.port`, keep fleet-wide views ungated, put the operation's controls on the panel showing the data it acts on, and route any new user-facing string through refdata `l10n` (not a hardcoded literal or a hand-written fallback entry).
