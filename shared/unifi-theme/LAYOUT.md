# App shell layout

The page shell (top bar + collapsible sidebar + main content) that every
top-level UI in this repo shares. Colors, type, and PrimeVue tokens are
centralized in `unifi.css` / `preset.js` (imported via the `@unifi-theme`
alias); the shell itself is now real shared code too — see "Shared
component" below.

## Shared component

`shared/ui-shell/AppShell.vue` (+ `app-shell.css`) is the shell, consumed
by all four apps (`lab-shell`, `admin`, `refdata`, `seafreight-app`) via a
`@ui-shell` vite alias (mirroring `@unifi-theme`). It exposes:

- `#brand` — brandmark content (dot/icon + app name); required, no default.
- `#breadcrumb` — optional; plain content or a fleet-style dropdown, per
  the consuming app (see "Per-app notes" below).
- `#topbar-right` — optional; app-specific controls (locale select,
  fleet-context select, connection tag). The theme toggle is baked into
  `AppShell.vue` itself and always renders last — don't add another one.
- `#sidebar` — optional; omit entirely for an app with no nav (the
  `.sidebar` element doesn't render at all if nothing is passed). Sidebar
  content should be styled against the shared `.nav-group` / `.eyebrow` /
  `.nav-item` / `.nav-badge` / `.label-fade` classes from `app-shell.css`.
  `shared/ui-shell/NavList.vue` is a ready-made data-driven renderer for
  the common case — a `sections` prop shaped
  `[{ eyebrow?: string, items: [{ key, label, icon, badge? }] }]` — used
  by `admin` and `seafreight-app`; use it directly rather than
  hand-rolling another nav component unless your nav is bespoke enough
  that it doesn't fit (see `refdata`'s `TypeNavigator.vue`, which renders
  its own markup against the same CSS classes instead).
- Default slot — main content, rendered inside `.main` / `.main-inner`
  (scrollable, padded).
- `#footer` — optional; a status/telemetry bar pinned below `.main-inner`,
  full-bleed to the main area's edges (no padding, not part of the
  scrollable region) — for a bar that should look like a fixed strip, not a
  card floating inside the content's padding. Used by `admin`'s
  `TelemetryStrip.vue`.

Collapse state and the theme toggle are internal to `AppShell.vue` — no
app needs to read or drive either.

`AppShell.vue` must stay dependency-free of `vue-router` and `vue-i18n`
(not every app has both) and must not import Vue component libraries
(e.g. `primevue/button`) — `shared/ui-shell/` has no `node_modules` of
its own, so Rollup can't resolve npm packages imported from there at
build time (Vite's dev server is more lenient and will mask this until a
production build is attempted). Icon-only chrome uses plain `<i
class="pi pi-*">` markup instead — every app already loads
`primeicons.css` globally, so the class works with no import at all.

## Reference

`app-shell-reference.html` is a static, dependency-free HTML/CSS mockup of
the shell — open it directly in a browser. Treat it as the canonical
visual reference for any new top-level screen; `AppShell.vue` is the
buildable version of the same structure.

## Contract

- **Top bar** — brandmark + breadcrumb-style context on the left
  (`Fleet ▾ / Section ▾`); status/context controls (locale, theme toggle,
  environment select) on the right, pinned there with `margin-left: auto`.
  No global search bar unless a screen actually needs one.
- **Sidebar** — collapsible nav rail, ~220px expanded / ~52px collapsed
  (icon-only), toggled via a `.collapsed` class on the sidebar root.
  - Nav items are grouped under muted uppercase "eyebrow" labels once
    there's more than one logical group (e.g. "JetStream" →
    Streams / KV Buckets / CQRS Shapes); eyebrows hide in the collapsed
    state.
  - The collapse control is a plain, borderless `«` / `»` glyph — not a
    boxed icon+label button — at the bottom of the sidebar, mirrored with
    `transform: scaleX(-1)` when collapsed (`rotate()` would flip it
    upside down instead of mirroring it).
  - No "Quick Links" / Settings / Help footer section in the nav —
    deferred; don't reintroduce without an explicit request.
- **Main content** — page head (eyebrow + title + one primary action),
  then panels using the shared `.lab-panel` treatment (the reference
  file's `.panel` is the same shape, spelled out in full for copy-paste).

## Per-app notes

- **lab-shell** — topbar only (brand + tagline-as-breadcrumb), no
  sidebar; `vue-router`'s `<router-view/>` is ordinary main content, not
  shell-owned.
- **refdata** — sidebar is `TypeNavigator.vue` grouping dictionary types
  by `categories.js`'s `CATEGORY_ORDER`/`DOMAIN_CATEGORIES`, plus a
  "Tools" eyebrow group for Localization/Versioning. Breadcrumb is plain
  text (no fleet concept). Previously had its own 2-column grid shell —
  now just ordinary content inside `.main-inner`.
- **admin** — sidebar via `NavList.vue` fed its existing eyebrow-grouped
  `sections` data. Breadcrumb is plain text; the fleet-context select
  stayed in `#topbar-right` rather than moving into the breadcrumb
  dropdown shown in the reference mockup — a deferred polish item, not a
  functional gap.
- **seafreight-app** — sidebar via the same `NavList.vue`, fed a single
  ungrouped section (its `views` were already flat). Same breadcrumb
  treatment as admin.
