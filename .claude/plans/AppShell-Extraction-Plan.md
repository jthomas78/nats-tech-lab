# Shared AppShell.vue Extraction — Plan

> **Status: DONE (2026-07-23).** All 4 apps migrated; see "Implementation notes" at the bottom for
> what shipped, deviations from this plan, and known follow-ups.
>
> Cross-refs: [`shared/unifi-theme/LAYOUT.md`](../../shared/unifi-theme/LAYOUT.md) (shell contract +
> `app-shell-reference.html`), `CLAUDE.md` § "Frontend Design System".
>
> **Decision locked at scoping (2026-07-23):** this extraction **migrates all 4 apps to the new
> Scaleway-style shell** (breadcrumb topbar, collapsible icon sidebar) from
> `app-shell-reference.html` — not a like-for-like refactor of each app's current look. Reasoning:
> `LAYOUT.md`/CLAUDE.md already tell future UI work to follow that shell; extracting a shared
> component around the *old* per-app looks would produce shared code that immediately contradicts
> the doc just written.

## Goal

Close the gap `LAYOUT.md` flags under "Closing the gap between documented and shared": theme
tokens are real shared code (`unifi.css`/`preset.js`, imported by all 4 apps); the page shell
(topbar + sidebar) is currently a documented contract only, hand-rolled per app. This plan
extracts a shared `AppShell.vue` so `lab-shell`, `admin`, `refdata`, and `seafreight-app` consume
one real component instead of four independently-maintained (and, for admin/seafreight,
near-duplicated) copies — and, per the decision above, land all 4 on the new shell in the process.

## Current state (code survey, 2026-07-23)

| App | App.vue LOC | Router? | i18n? | Sidebar today |
|---|---|---|---|---|
| `lab-shell` | 71 | vue-router (named routes `menu`, `demo-intro`) | none | none — topbar + `<router-view/>` only |
| `admin` | 273 | none — `activeView` ref | vue-i18n (partial) | `NavSidebar.vue`, data-driven `sections` (eyebrow groups) |
| `seafreight-app` | 267 | none — `activeView` ref | vue-i18n (full) | `NavSidebar.vue` (separate copy, `views` prop, no eyebrows) |
| `refdata` | 142 | none — `store.activeView` (Pinia) | none | `TypeNavigator.vue` — bespoke, category-driven, tightly coupled to `dictionary` store + `categories.js` |

All 4 apps align on `vue ^3.5`, `primevue ^4`/`@primevue/themes ^4`, `pinia ^3` — no version-mismatch
risk. `vue-router` (lab-shell only) and `vue-i18n` (admin/seafreight only) are unevenly present, so
`AppShell.vue` must not hard-depend on either.

Theme toggle (`isDark()`/`toggleTheme()` from `@unifi-theme/preset.js`, mirrored into a local `dark`
ref) is wired identically in all 4 — trivially hoistable into `AppShell.vue` itself. Locale
switching (admin/seafreight only, via `useRefdataLabels()`) and any app-specific top-bar-right
controls (fleet-context select, connection tag, fallback-warning tag) are not uniform and must stay
each app's own responsibility.

None of the 4 apps currently tie nav items to actual routes except lab-shell (which has no
sidebar) — admin/seafreight/refdata all switch views via in-memory ref/Pinia state, not
`vue-router`. `AppShell.vue`'s sidebar cannot assume `<router-link>`-based nav.

## Design decisions

**Q1 — Where does the sidebar's content come from?**
Recommendation: **a `#sidebar` slot**, not a data-driven prop. `admin`/`seafreight` already have a
data-driven "sections/items" style (two near-duplicate `NavSidebar.vue` components) that maps
cleanly onto the new eyebrow-grouped nav-item pattern; `refdata`'s `TypeNavigator` is
category-driven and store-coupled and does not fit a generic items list without a much larger
rewrite of `refdata`'s own domain model. A slot lets `AppShell.vue` own the topbar, collapse
state, and shell CSS while each app supplies its own nav markup (styled against the shell's shared
`.nav-item`/`.eyebrow`/`.nav-group` classes). This also does not block a *second*, later, optional
cleanup where `admin` and `seafreight` dedupe their two `NavSidebar.vue` copies into one shared
list-rendering component that fills that slot — see "In scope" below, since this migration is the
natural moment to do that rather than leaving two copies mid-redesign.

**Q2 — Where does the component live?**
Recommendation: a new `shared/ui-shell/` package (`AppShell.vue` + `app-shell.css`), not inside
`shared/unifi-theme/`. `preset.js`'s own header comment states unifi-theme "must stay
dependency-free" (it's imported by both the theme setup and, separately, diagram tooling) —
folding a Vue SFC in there breaks that. Add a `@ui-shell` alias (mirroring the existing
`@unifi-theme`/`@refdata` aliases) to each app's `vite.config.js`, including `server.fs.allow`.

**Q3 — Optional integrations (i18n, router).**
`AppShell.vue` bakes in the theme toggle (identical everywhere) but must not call `useI18n()` or
`useRouter()` internally — that would break in the 2 apps missing each. Locale switcher and any
other top-bar-right controls go through a `#topbar-right` slot; each app keeps exactly the controls
it has today (fleet-context select, locale select, fallback tag, etc.), just re-homed into the
slot instead of hand-rolled topbar markup.

**Q4 — Collapse state.**
Internal to `AppShell.vue` (a local ref + class binding — the properly-Vue version of the
reference HTML's inline `onclick`). No app needs to read or drive it externally today.

**Q5 — Breadcrumb content.**
The new topbar's `Fleet ▾ / Section ▾` breadcrumb has a natural fit in `admin`/`seafreight`
(their existing fleet-context `Select` becomes the first breadcrumb segment). `refdata` and
`lab-shell` have no "fleet" concept — for those, the breadcrumb likely collapses to a single
segment (app/section name, no second dropdown) or is omitted via a `#breadcrumb` slot default.
Confirm the exact per-app breadcrumb content when each app's migration starts, not up front.

**Q6 — Mapping each app's current nav onto the new eyebrow-grouped pattern.**
- `admin`: closest fit already — its `sections` array (JetStream/Postgres-style eyebrow groups)
  maps almost directly onto the reference's nav-group/eyebrow structure.
- `seafreight-app`: currently a flat `views` list (fleet/port) with no grouping — becomes a single
  ungrouped nav-group (matching the reference's ungrouped "Overview" item), or introduces its own
  eyebrow if a natural grouping exists. Decide at migration time.
- `refdata`: `categories.js` (`CATEGORY_ORDER`, `DOMAIN_CATEGORIES`) already expresses
  eyebrow-like grouping for dictionary types — map those onto `eyebrow`/`nav-item` markup, and
  keep the two special entries (Localization, Versioning) as their own ungrouped items or a
  small "Tools" eyebrow group.
- `lab-shell`: no sidebar today, and none is obviously needed — it's a menu of demos, not a
  workspace with sub-sections. Recommendation: adopt the new topbar (brand + theme toggle; no
  fleet-style breadcrumb) but keep the sidebar slot empty/omitted, same as
  `app-shell-reference.html` would render with nothing passed to `#sidebar`.

## In scope

- New `shared/ui-shell/AppShell.vue` + `app-shell.css`, extracted from the shell-specific portion
  of `app-shell-reference.html`'s CSS (topbar, sidebar, nav-item/eyebrow, collapse-button rules) —
  the page-content styles (`.panel`, `.welcome-*`, `.stat-row`, etc.) stay out; those are
  per-app/page concerns, not shell concerns.
- `@ui-shell` vite alias in all 4 apps.
- Migrating all 4 apps' `App.vue` to consume `AppShell.vue`, each supplying its own
  `#sidebar`/`#topbar-right`/default-slot (main content) content per Q5/Q6 above.
- Deduping `admin`'s and `seafreight-app`'s `NavSidebar.vue` into one shared nav-list component
  (living in `shared/ui-shell/` alongside `AppShell.vue`) *if* their post-migration nav shapes end
  up close enough to share — confirm during that phase, don't force it if they diverge.
- Updating `seafreight-app`'s `App.spec.js` (and any admin/refdata component tests touching
  current shell markup) so the suite stays green against the new structure.

## Out of scope

- Any router or i18n unification across apps — each app keeps its own (or lack of one).
- Backend changes of any kind.
- New features beyond the shell itself (no new nav items, no new topbar controls).
- Runtime/per-tenant theming (see main plan's Phase 19 placeholder) — unrelated axis.

## Recommended migration order

1. **`lab-shell`** — simplest case (no sidebar, no i18n in the shell, router only owns
   `<router-view/>` inside the main slot, not shell chrome). Cheapest proof that the abstraction
   holds; lowest blast radius if something about the extraction needs to change.
2. **`refdata`** — forces the `#sidebar` slot design to prove itself against a genuinely bespoke
   nav (`TypeNavigator`) and against a 2-column CSS grid main-content layout (not the other apps'
   flex `.shell`) — resolves whether `AppShell.vue` needs a layout-mode option or whether refdata's
   grid is just slotted main-content styling (leaning toward the latter — try it before adding a
   prop for it).
3. **`admin` + `seafreight-app` together** — last, since they're near-duplicates of each other and
   this is the natural point to also dedupe their two `NavSidebar.vue` copies (see "In scope").

## Checklist

- [x] Confirm this plan (design decisions Q1–Q6, migration order) before implementation starts
- [x] Extract `shared/ui-shell/AppShell.vue` + `app-shell.css` from `app-shell-reference.html`'s
      shell-only CSS; expose `#sidebar`, `#topbar-right`, `#breadcrumb`, and default (main content)
      slots; bake in theme toggle + collapse state internally
- [x] Add `@ui-shell` vite alias (+ `server.fs.allow`) to all 4 apps
- [x] Migrate `lab-shell` onto `AppShell.vue`; verify in Browser pane against
      `app-shell-reference.html`
- [x] Migrate `refdata` onto `AppShell.vue`, mapping `categories.js` groupings to eyebrows;
      resolve the 2-col grid question; verify in browser
- [x] Migrate `admin` and `seafreight-app` onto `AppShell.vue`; dedupe `NavSidebar.vue` (both
      converged on the same `sections` shape — see `shared/ui-shell/NavList.vue`); update
      `App.spec.js` and any other affected tests
- [x] `npm run build` green for all 4 apps; existing frontend test suites green
- [x] Update `CLAUDE.md` § "Frontend Design System" and `LAYOUT.md` to reflect the shell as shared
      code, not just a documented contract

## Implementation notes (2026-07-23)

- **`primevue/button` had to come out of `AppShell.vue`.** `shared/ui-shell/` has no `node_modules`
  of its own, so Rollup couldn't resolve the npm import at production-build time (Vite's dev
  server resolved it fine, which is why this wasn't caught until the first real `npm run build`).
  Fixed by rendering the built-in theme toggle as a plain `<button class="icon-btn"><i
  class="pi pi-sun/-moon"/></button>` — pure CSS-class icon, no JS import — since every app already
  loads `primeicons.css` globally. This is now a hard rule for anything under `shared/`, documented
  in `LAYOUT.md`.
- **`NavList.vue` dedup happened, as hoped but not guaranteed by Q1.** After migration, `admin`'s
  eyebrow-grouped `sections` and `seafreight-app`'s flat `views` (recast as one ungrouped section)
  turned out to be the same shape, so both `NavSidebar.vue` copies were deleted in favor of one
  `shared/ui-shell/NavList.vue`. `refdata`'s `TypeNavigator.vue` stayed bespoke, as anticipated.
- **Breadcrumb (Q5) landed simpler than the reference mockup for `admin`/`seafreight-app`.** Both
  kept their fleet-context `Select` in `#topbar-right` and used `#breadcrumb` for plain text,
  rather than moving the select into a `Fleet ▾ / Section ▾` dropdown inside the breadcrumb. Purely
  a polish gap, not a functional one — worth revisiting if the breadcrumb's dropdown affordance
  becomes something users actually reach for.
- **Known caveat, not fixed:** `admin`'s full-viewport-height scroll sections (e.g. Streams/KV
  table) previously relied on a `height: 100vh` flex chain down through `App.vue`'s own wrapper
  elements. Inside `AppShell`'s `.main`/`.main-inner` (which grow with content instead of being
  height-constrained), that fill-viewport behavior may differ slightly. Still renders and scrolls
  correctly; no test covers the exact viewport-fill behavior. Flagged as a possible follow-up, not
  addressed here since fixing it would mean adding a layout-mode option to `AppShell.vue` that
  Q1–Q6 explicitly deferred.
- Added a `lab-shell` entry to `.claude/launch.json` (previously missing) so it can be previewed
  the same way as the other 3 apps.
