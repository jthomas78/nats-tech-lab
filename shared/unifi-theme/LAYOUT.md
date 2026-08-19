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
  `.nav-group-toggle` / `.nav-group-body` / `.nav-item` / `.nav-badge` /
  `.label-fade` classes from `app-shell.css`.
  `shared/ui-shell/NavList.vue` is a ready-made data-driven renderer for
  the common case, used by `admin`, `seafreight-app`, and (since Phase
  36.1) `refdata`; use it directly rather than hand-rolling another nav
  component unless your nav is bespoke enough that it doesn't fit — in
  which case render your own markup against the same shared CSS classes,
  the way `refdata`'s retired `TypeNavigator.vue` used to. Its `sections`
  prop takes an ordered array whose entries are either of:

  ```
  { eyebrow?: string, items: [{ key, label, icon?, badge? }] }
  { group: string, sections: [ <the entry above> ] }
  ```

  giving **two** nav levels — an optional `eyebrow` over a run of items —
  plus optional outer banding via the `group` form: a hairline-divided,
  clickable banner (`admin`'s PLATFORM / SYSTEM) wrapping one or more
  ordinary sections. There is no third level; a would-be third tier is
  expressed as another `eyebrow` section inside the same group. Both
  entry forms mix freely and render in array order, so a flat ungrouped
  section can sit above the grouped ones (`admin`'s Overview does). A
  group with no `eyebrow` on its inner section puts items directly under
  the group banner, which is how a single-screen area reads as one entry
  rather than a nested tier (`admin`'s Accounts and Settings).

  A group's contents are indented 18px so they read as nested beneath the
  banner. That figure is the banner's chevron (12px) plus its gap (6px),
  which lands the contents on the group *label's* text column rather than
  its chevron's — the tree-view convention, where the disclosure control
  hangs to the left of the column its children align to. Ungrouped
  sections don't indent (no parent to sit under), and the collapsed icon
  rail zeroes the indent so icons stay centred.

  Which groups are expanded is `NavList`'s own state — like the sidebar
  collapse below, no app reads or drives it. All groups start expanded,
  and on the collapsed icon rail the banners hide and every group renders
  expanded regardless (a collapsed group would otherwise be unreachable,
  since its banner is the only way to reopen it).
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

## Panel top tabs

Any tab strip positioned at the top of a right-side detail panel (the panels
next to the left navbar — `AccountsView.vue`'s Provisioning/Topology,
`RpcPanel.vue`'s Traces/Messages) must be a real PrimeVue `Tabs`
(`Tab`/`TabList`/`TabPanels`/`TabPanel`) with `class="panel-tabs"` on the
`<Tabs>` root — never a custom chip/pill toggle for this role. `.panel-tabs`
in `unifi.css` is the one place that styles it, so every panel's top tabs
stay identical without each component repeating the override. Ordinary
filter/facet toggles inside a panel body (errors-only, slow-only, family
chips) keep using the plain `.chip` treatment — that's a different UI role
(a filter, not a view switch) and isn't affected by this rule.

**Placement — the `<Tabs>` sits flush on the page, never inside a `.lab-panel`
card.** `AccountsView.vue` is the reference: `App.vue` renders it directly
inside the section's `.group`, with no wrapping card, so the tab strip and
its hairline sit right on the page background. The card treatment (border,
background, padding — `.lab-panel`) belongs on each `TabPanel`'s *content*,
not around the `<Tabs>` itself — `AccountsPanel.vue` wraps its own root in
`<div class="lab-panel accounts-panel">`; `RpcPanel.vue` wraps each
`TabPanel`'s content in `<div class="lab-panel rpc-card">`. Getting this
backwards (wrapping the whole `<Tabs>` in a card, as `RpcPanel.vue` first
did) nests the tablist inside a different background than it expects and
forces a pile of compensating overrides — don't reach for those; move the
card down a level instead.

**Don't strip `.p-tabpanels`' default padding.** Aura's own `tabpanels`
padding (not something this repo sets) is exactly what creates the gap
between the tab hairline and the panel/card below on `AccountsView.vue` —
overriding it to `0` (as `RpcPanel.vue` first did, chasing a "make full
height" goal that had nothing to do with padding) pulls the content flush
against the tab strip, so the tablist's hairline visually merges with the
card's own top border into what reads as one line instead of two. Leave
`.p-tabpanels` padding alone; only touch `flex`/`min-height`/`display` on it
if the tab's content needs to fill the panel's remaining height.

If the tabbed content needs the full panel height (a `DataTable` with
`scroll-height="flex"`, an internally-split view like `TraceWaterfall`), the
consuming component's `<style scoped>` must flex the PrimeVue-rendered
`.p-tabs`/`.p-tabpanels`/`.p-tabpanel` down to the active panel via `:deep()`
— see `RpcPanel.vue` for the pattern. `AccountsView.vue`'s tab content
scrolls with the page instead, so it needs no such override. **Watch for the
same-element `:deep()` gotcha**: `<Tabs class="panel-tabs rpc-tabs">` puts
`rpc-tabs` on the *same* root element PrimeVue renders as `.p-tabs` — so
`.rpc-tabs :deep(.p-tabs) {...}` (a descendant combinator) silently matches
nothing, since there's no ancestor/descendant relationship between two
classes on one node. Use a plain compound selector for that one level
(`.rpc-tabs.p-tabs {...}`, no `:deep()`) — it still gets the component's
scope attribute automatically since the class is applied from that
component's own template. `:deep()` is correctly needed for true descendants
rendered by PrimeVue itself (`.p-tablist`, `.p-tabpanels`, `.p-tabpanel`).

## Per-app notes

- **lab-shell** — topbar only (brand + tagline-as-breadcrumb), no
  sidebar; `vue-router`'s `<router-view/>` is ordinary main content, not
  shell-owned.
- **refdata** (branded "Tech Lab Operator", Phase 36.1) — sidebar is
  `NavList.vue` fed one `Operations` group with two sections: a single
  `Reference Data` entry, and (Phase 36.2) a `Trading Partners` eyebrow over
  `Shippers`/`Transporters` — migrated from `admin`'s own `Trading partners`
  eyebrow, same nesting shape, just relocated. What used to be
  `TypeNavigator.vue`'s three sidebar groups (the flat reference-data type
  list, the Domain category list, and a "Tools" group for Localization/
  Versioning) all moved into `ReferenceDataPanel.vue`, a `panel-tabs` strip
  (`Reference Data` / `Domain` / `Localization` / `Versioning`) rendered as
  the `Reference Data` nav entry's content — same "Panel top tabs" contract
  as `RpcPanel.vue`, above. The `Reference Data` tab keeps a
  `categories.js`-driven type switcher as its own left-hand nav-item list
  (reusing the shared `.nav-group`/`.nav-item`/`.nav-badge` classes, same as
  `TypeNavigator.vue` did) beside `ItemGrid`; the `Domain` tab reuses
  `CategoryTypeList.vue` unchanged behind a small Enums/Strings sub-tab
  strip (a bare `Tabs`/`TabList`, no `TabPanels` — it only drives which
  category `CategoryTypeList` shows, it doesn't own separate panel content,
  unlike `VersioningPanel.vue`'s own nested tabs). Breadcrumb is a literal
  `Operations / Reference Data` (or `/ Trading Partners`) nav path rather
  than descriptive text — this app has no fleet concept of its own for
  Reference Data, but Trading Partners' `TradingPartnersPanel.vue` does need
  one (see below), scoped separately from Reference Data's platform-wide
  `Context` select in `#topbar-right`: a `Tenant` + `Fleet` select pair,
  shown only while a Trading Partners view is active, backed by a second,
  tenant-scoped NATS connection (`useTenantConnection.js`) alongside this
  app's original PLATFORM one — see that module's doc comment for why a
  second connection was necessary rather than reusing the first.
- **admin** — sidebar via `NavList.vue`, and the only app so far using the
  `group` form: an ungrouped Overview, then PLATFORM (Accounts, Settings —
  the former Trading partners eyebrow over Shippers/Transporters moved to
  `refdata` in Phase 36.2), then SYSTEM (NATS, Postgres). The split is by
  what a view is *of*, not which backend serves it — business layer vs.
  infrastructure diagnostics — which is why Accounts sits under PLATFORM
  despite NATS accounts being its mechanism. Breadcrumb is plain text; the
  fleet-context select stayed in `#topbar-right` rather than moving into the
  breadcrumb dropdown shown in the reference mockup — a deferred polish
  item, not a functional gap.
- **seafreight-app** — sidebar via the same `NavList.vue`, fed a single
  ungrouped section (its `views` were already flat). Same breadcrumb
  treatment as admin.
