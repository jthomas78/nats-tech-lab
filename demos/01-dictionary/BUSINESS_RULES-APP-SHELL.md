# Business Rules — Application Shell + Micro-Frontend Plugins (`lab-shell/`)

> Split out of `BUSINESS_RULES.md` to keep per-domain reads small. See that
> file's index for the other domains.
>
> **This is the only rules file whose subject is a frontend, not a Go
> service.** The code it governs lives in `lab-shell/` (repo root), not under
> `demos/01-dictionary/backend/`; the file sits here so the one
> `BUSINESS_RULES.md` index keeps covering every domain in the repo.
>
> Design of record:
> [`ARCHITECTURE-APP-SHELL.md`](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-APP-SHELL.md).
> Phasing: [`Application-Shell-Microfrontend-Plan.md`](../../.claude/plans/Application-Shell-Microfrontend-Plan.md).
> Shell/layout contract the rules build on: [`LAYOUT.md`](../../shared/unifi-theme/LAYOUT.md).

**Status: APPROVED at the design gate (2026-08-28).** BR-AS01–BR-AS14 were
confirmed with three amendments made at approval time, recorded inline below:
BR-AS05's permission source (auth-service JWT claims), BR-AS01's registry
transport (operator-curated backend endpoint), and BR-AS14's migration gate
(delta mockups, not capability-complete, for Phases 10–12). **BR-AS15 was added
at approval** — a reviewable example plugin must exist and be signed off
before any real application is migrated.

**Every rule below states an observable failure**, per CLAUDE.md Quality
Rule 1. Rules originally phrased as properties of the architecture
("the shell starts with no service-specific knowledge") were restated as
assertions a test can fail — the property is the thesis of the whole design,
so it is worth a failing build rather than a paragraph. Where a rule's test
is a build-time or lint check rather than a runtime spec, that is said
explicitly; it still counts.

**Test runner.** `lab-shell/` had no test runner before this phase — only
`dev`, `build`, `preview`. Phase 1a adds Vitest, matching the setup already
proven in `demos/01-dictionary/frontend/admin/` (Vitest 4 + happy-dom +
`@vue/test-utils`, config inside `vite.config.js`, `test` / `test:watch`
scripts). This is a prerequisite of the phase, not an optional extra: none
of the rules below are enforceable without it.

---

## Discovery and trust

### BR-AS01 — Curated discovery

Only a platform-controlled plugin registry may nominate executable frontend
modules. The browser must not discover arbitrary NATS services, and must not
accept a remote URL advertised directly by a domain service.

**Amended at approval:** the registry is served by an **operator-curated
backend endpoint**, not a file baked into the shell's bundle. This is what
makes BR-AS03 true rather than nearly true — adding a plugin is a
configuration change, with no redeploy of the shell's own deployment unit.

**Testable:** given a registry response, the loader resolves remotes *only*
from entries in that response; a remote URL supplied by any other channel
(a message payload, a query parameter, a `postMessage`) is refused and
never fetched. Assert with a spec that hands the shell a forged
`?remote=` / event payload and asserts no network call and a rejection
recorded against no plugin.

### BR-AS13 — Contract compatibility

An unsupported registry `schemaVersion`, `shellApiVersion`, contribution
type, or extension-point version is rejected **before any remote code
executes**.

**Testable:** a registry entry declaring an out-of-range `shellApiVersion`
resolves to status `incompatible` with zero fetches issued for its remote
entry point. Rejection is **per entry, never per registry**: a registry
containing one incompatible entry and one valid entry yields one
`incompatible` and one `available`. Likewise a plugin declaring two
contributions, one of them invalid, loses only the invalid contribution —
its sibling still renders.

### BR-AS10 — Preserved trust boundaries

Plugin migration must not combine the existing Admin, Tech Lab Operator, and
SeaFreight NATS credentials into a broader shared credential. Connection
state remains keyed by its permission/account profile.

Four distinct profiles exist today and must remain four: Admin (PLATFORM),
Tech Lab Operator refdata-admin (PLATFORM), Tech Lab Operator tenant
(Organizations), and SeaFreight tenant. A single shell-wide connection would
require the *union* of those grants — strictly more browser authority than
any app holds today — so the shell never opens a business connection on a
plugin's behalf.

**Testable:** the connection registry is keyed by permission profile;
requesting a connection for two different profiles yields two distinct
connection objects, and requesting the same profile twice yields one. Assert
also that deactivating a plugin closes the connections it opened — a
teardown contract, not a leak.

---

## Integration surface

### BR-AS02 — Contribution-only integration

A plugin extends the application only through the published contribution
contract. It must not mount another top-level shell, replace global
providers, or manipulate shell DOM.

**Testable (runtime):** a plugin that attempts to mount a second `AppShell`,
or to write outside its assigned container, is contained — the shell's own
chrome is unchanged after the attempt, and the offending contribution is
recorded `failed`. Assert the shell's topbar/sidebar DOM is identical before
and after.

### BR-AS09 — One global UI frame

The shell is the sole owner of `AppShell`, the router, the UniFi theme, the
PrimeVue configuration, the Toast outlet, global locale selection, sidebar
collapse, and global error boundaries.

**Testable (build-time):** `lab-shell/`'s dependency graph contains no import
from any plugin package, and no plugin package imports `shared/ui-shell/
AppShell.vue` to instantiate a second frame. Enforced as a lint/graph check
that fails the build, not as a runtime spec — the failure a runtime spec
would catch happens too late to be useful.

**Testable (runtime):** exactly one `AppShell` instance and one Toast outlet
exist in the mounted tree with any number of plugins active.

### BR-AS07 — Host-owned extension points

The owner of a screen or shell region declares and versions its extension
points, controls layout and capacity, and passes contributors readonly
contextual data. Plugins fill targets; they never choose where a target
lives.

Extension points are named `{owner}/{region}/v{major}` — e.g.
`shell/topbar-controls/v1`, `shell/footer/v1`, `shell/home-main/v1`,
`demo-catalog/details-sidebar/v1`. The last of those is owned by a
**built-in feature, not the shell**, which is the cross-owner case the proof
remote must exercise.

**Testable:** a contribution targeting an undeclared or wrong-version
extension point is rejected (BR-AS13). Context passed into an extension is
frozen — a contributor mutating it does not affect the host or any sibling
contribution. A target declaring a capacity of N renders at most N
contributions and reports the overflow rather than silently dropping it.

### BR-AS06 — Stable identity and order

Plugin and contribution IDs are globally unique and namespaced.
Contributions render in deterministic configured order.

This rule also settles the concrete collision found in the current
codebase: three Pinia stores registered as `tenant` and two as `dictionary`
across the three apps. Once those apps coexist as plugins in one frame, the
duplicates silently overwrite one another. Store IDs are therefore
namespaced `{plugin-id}/{store-id}`.

**Testable:** registering two plugins that both declare store ID
`dictionary` yields two independent stores whose state does not alias;
a duplicate *plugin* ID is rejected at registry validation. Given a fixed
registry, the rendered order of contributions into one extension point is
identical across runs and independent of remote load-completion order —
assert by resolving remotes in reversed order and comparing rendered
sequence.

---

## Loading, failure, and addressing

### BR-AS08 — Lazy feature loading

Contribution metadata is available before its implementation code. Remote
feature code loads only when a route, panel, or explicit lifecycle need
requires it.

This is the two-stage lifecycle: registry discovered → metadata validated →
contributions indexed → *(first use)* → remote loaded → `activate()` once →
render.

A pending region must **animate** while it waits — the route panel's skeleton
rows sweep, and a reserved extension point renders a soft breathing
placeholder rather than a flat block. Motion is the signal that the shell is
working rather than stalled: a static gray bar is indistinguishable from a
broken render, and an extension point is exactly where a stall is most likely
(it is waiting on a third party's chunk). Motion is suppressed under
`prefers-reduced-motion: reduce`, which leaves the static block — acceptable
there because the user has asked for it.

**Testable:** after registry load and before any navigation, the nav tree
contains every permitted plugin's entries and **zero** remote entry points
have been fetched. Navigating to one plugin's route fetches exactly that
plugin's remote. `activate()` is invoked at most once per plugin across
repeated navigations to its contributions.

### BR-AS04 — Failure isolation

A failed plugin or contribution must not prevent the shell, built-in
capabilities, or other plugins from loading and operating.

Isolation is at **contribution granularity**, not plugin granularity: a
plugin whose route fails still renders its panel contribution elsewhere if
that panel is sound.

**Testable:** four distinct failure modes each leave the shell and every
other plugin operating — (a) the remote 404s, (b) the remote loads but
`activate()` throws, (c) a rendered contribution throws during render,
(d) the registry endpoint itself is unreachable. In (d) the shell renders
with built-ins only. Error summaries surfaced to the user carry stage and
cause and **never** credentials, tokens, or registry URLs — assert the
rendered error text against a denylist.

### BR-AS03 — Independent deployment

Adding, removing, enabling, disabling, or upgrading a compatible remote
plugin must not require a shell source change or shell rebuild.

**Testable:** two successive registry responses differing by one added entry
produce a shell with one more plugin, with no change to any file under
`lab-shell/src/`. Proven end to end in Phase 1b by deploying the example
remote twice without rebuilding the host.

### BR-AS12 — Addressable feature routes

Every primary navigation destination is a namespaced shell route with
browser history, refresh/deep-link behavior, and direct-access permission
checks.

**Testable:** a cold load directly at a plugin's deep route resolves the
registry, loads only that plugin, and renders its view — the deep link does
not require having first visited the shell root. Back/forward traverse
plugin routes correctly. A route whose permission check fails is refused on
direct access, not merely hidden from the nav (see BR-AS05).

---

## Authorization

### BR-AS05 — Shell-owned authorization

The shell evaluates contribution permissions for navigation visibility,
direct route access, and extension rendering. **Hiding UI never replaces
server-side authorization.**

**Amended at approval:** the permission source is the **auth-service JWT
claims** held by the shell. One source for every plugin, independent of
which NATS credential a plugin later opens — which is what keeps this rule
compatible with BR-AS08's metadata-before-code ordering (a credential-derived
source would require a connection before nav could render) and with
BR-AS10's four separate profiles.

**Testable:** given a token whose claims lack a contribution's required
permission, that contribution is absent from the nav *and* refused on direct
route access — assert both, since the second is the one that matters.
Given claims that grant it, both succeed. The shell makes no authorization
decision the server does not also make: assert that a refused route is
refused by the shell without the shell having fabricated a server response.

---

## Preservation and fidelity

### BR-AS11 — Preserved behavior

Migrating an existing app must preserve its current functional capabilities,
business rules, editing affordances, localization behavior, and cleanup
semantics unless a separate approved rule changes them.

Migration is an arrangement and deployment change. It is not permission to
drop behavior.

**Testable:** each migrated app's existing spec suite passes unchanged when
the app runs as a plugin. Where an app has no suite for a behavior being
moved, a characterization spec is added *before* the move, not after.

**Known hazard this rule must resolve before a second plugin exists:**
`useRefdataLabels.js` holds `transport` as a module-global mutable. Two
plugins on different credentials would silently share one transport, last
writer wins. That is a cross-tenant data-leak shape, not a migration
nicety. Refdata client instances are therefore credential-scoped while
locale stays shell-global (locale belongs to the person; refdata content
belongs to the credential).

**Testable:** two plugins holding different credential profiles resolve
refdata labels through two distinct transports; writing the transport in one
does not change the other.

### BR-AS14 — Design fidelity gate

Shell and migration designs use the shared UniFi system and are visually
reviewed at **1920×1080**.

**Amended at approval:** Phase 1 requires a **capability-complete** mockup
set — delivered and approved 2026-08-28, seven artboards covering active,
empty, loading, failed, the extension-point contract, cross-owner
composition, and the plugin status surface. Each real-app migration
(Phases 10–12) requires an approved **delta** mockup instead: only screens
where shell composition changes what the user sees — nav merge across
plugins, route-scoped topbar controls, footer contributions, failure states.
Screens that are pixel-identical before and after do not need an artboard to
prove they are identical.

**Testable:** the shell renders no palette, type ramp, or PrimeVue preset of
its own — it consumes `@unifi-theme` and `@ui-shell` the way the existing
four apps do. Assert as a build-time check that `lab-shell/` defines no
competing theme tokens, plus the existing `AppShell.spec.js` contract
(collapse control placement, glyph, ARIA) continuing to pass with plugins
mounted. The visual review itself is a human gate, not an automated one, and
is recorded in the plan rather than asserted in code.

### BR-AS15 — Proof plugin before migration

**Added at approval (2026-08-28), on the user's instruction.** No existing
application may be migrated into the shell until a purpose-built example
plugin has been built, deployed independently, and **reviewed by the user**
against the full contribution contract.

The example plugin is not a smoke test and not a placeholder — it is the
capability review. It must therefore exercise **every contribution kind at
least once**:

- `route` — a deep-linkable, permission-checked feature route;
- `navigation` — a nav group and entries merged into the shell's rail;
- `extension` — a panel into a shell-owned target (`shell/home-main/v1`)
  **and** one into a target owned by a built-in feature
  (`demo-catalog/details-sidebar/v1`), since cross-owner composition is the
  case a shell-only proof would miss;
- `shell-control` — a topbar control scoped to the active route, appearing
  on entry and removed on exit;
- `shell-footer` — a footer contribution.

It must additionally demonstrate, deliberately and on demand, the states the
mockups specify: `loading`, `failed`, `incompatible`, and a contribution that
throws — so failure isolation (BR-AS04) and per-entry rejection (BR-AS13) can
be *seen*, not just asserted.

**Testable:** the example plugin's own spec suite covers each contribution
kind listed above; the shell's suite asserts that with the example plugin
registered, one entry of each kind is indexed and rendered. The host is
rebuilt zero times between the example plugin's first and second deployment
(BR-AS03).

**Gate:** Phase 10 (SeaFreight Flow migration, renumbered from 2 on 2026-08-28) does not open until the user
has reviewed the running example plugin. This is a human gate on capability
and integration, recorded in the plan — the automated assertions above are
necessary for it but do not substitute for it.

**Port:** 7103. 7100/7101/7102 are taken by the existing frontends, and
CLAUDE.md's allocation rule assigns frontend dev servers from 7100–7199;
the entry goes in the demo's README port table in the same change. Note
`lab-shell`'s own dev port (5170) sits outside that band and is either
aligned or explicitly exempted as part of Phase 1a.

---

## Where these are enforced

| Area | Location (Phase 1a unless noted) |
| --- | --- |
| Registry fetch, schema + version validation | `lab-shell/src/shell/registry/` |
| Contribution indexing, ordering, identity | `lab-shell/src/shell/contributions/` |
| Extension-point declaration and capacity | `lab-shell/src/shell/extensions/` |
| Permission evaluation from JWT claims | `lab-shell/src/shell/auth/` |
| Loader adapter interface | `lab-shell/src/shell/loader/` (Module Federation implementation: Phase 1b) |
| Connection registry keyed by profile | `lab-shell/src/shell/connections/` |
| Plugin status machine and its transition table | `lab-shell/src/shell/registry/pluginStatus.js` |
| Boot order: discovery → validation → indexing | `lab-shell/src/shell/bootShell.js` |
| Route contributions → vue-router records | `lab-shell/src/shell/routing/` |
| Pinia store-id namespacing (`{plugin-id}/{store-id}`) | `lab-shell/src/shell/state/` |
| Build-time graph/lint checks (BR-AS09, BR-AS14) | `lab-shell/eslint.config.js` (editor-time) + `lab-shell/tools/frameOwnership.js` (whole-graph; run by its spec in `npm test` and standalone in `npm run lint`) |
| Curated registry endpoint (BR-AS01) | `accounts-service/accounts/frontendplugins.go`, `GET /api/accounts/frontend-plugins` — see `BUSINESS_RULES-ACCOUNTS.md` |
| Demo catalog, as a plugin like any other (BR-AS15) | `lab-shell/src/plugins/demo-catalog/` — built-in remote, routes under `/demos` |
| Example proof plugin (BR-AS15) | `lab-shell/plugins/example-plugin/` — its own build, own dev server on 7110, curated by `registry.dev.json` (Phase 1b) |
| Module Federation loader adapter (BR-AS03) | `lab-shell/src/shell/loader/federatedAdapter.js` (Phase 1b) |
| Contribution rendering, failure isolation, placeholders (BR-AS04, AS07) | `lab-shell/src/shell/ui/` (Phase 1b) |
| No-host-rebuild proof (BR-AS03) | `lab-shell/tools/hostBundleFingerprint.mjs` (Phase 1b) |

Paths are the intended layout at approval time; the authority once code
exists is the tree itself.

## As-built contract deltas (Phase 1a)

Three additions the rules above imply but did not spell out, recorded here
because a plugin author reads this file as the contract:

- **`routePrefix` is an optional manifest field, defaulting to the plugin
  id.** BR-AS12 requires a plugin's routes to be namespaced, unique and
  knowable from the URL alone — not that the segment repeat the id. The demo
  catalog is `demo-catalog` serving `/demos`, and a migrated SeaFreight plugin
  will want `/fleet`, not `/seafreight-flow`. Every route a plugin declares
  must still sit at `/{routePrefix}` or below (`route-not-namespaced`), and
  two plugins claiming one prefix is caught at index time
  (`route-prefix-conflict`), which costs only the losing plugin's route
  contributions — its nav and extension contributions stand.
- **A plugin may declare `extensionPoints` of its own**, each
  `{owner}/{region}/v{major}` with the owner segment equal to the declaring
  plugin's id (`extension-point-not-owned` otherwise, so a plugin cannot open
  a region in the host's namespace and capture contributions meant for the
  shell). Points are declared from the manifest rather than from `activate()`,
  which is what lets a contribution be placed into
  `demo-catalog/details-sidebar/v1` before the catalog's own code has ever
  been fetched. A second plugin claiming a declared point is `incompatible`
  (`duplicate-extension-point`); the first declarer keeps it.
- **`available → failed` is a legal transition.** The loader's two
  pre-adapter guards — an uncurated federated URL (`remote-not-curated`) and a
  missing adapter (`no-loader-adapter`) — refuse before any fetch is started.
  Routing those through `loading` first would claim a fetch that never
  happened, and the Loading artboard would show a spinner for work nobody
  started.

## As-built contract deltas (Phase 1b)

The Module Federation loader landed under the interface Phase 1a defined; the
one place it did not fit is recorded here rather than edited in silently.

- **`remote.name` — the federated container name — is a new optional manifest
  field.** Federation addresses a container by a name that must be a legal JS
  identifier (and, in some output formats, a global), while a plugin id is
  kebab-case because it lands in URLs, route prefixes and store keys. Rather
  than mangle one into the other by convention at the call site, a federated
  remote may state both. Omitted, it defaults to the plugin id with hyphens
  turned into underscores (`example-plugin` → `example_plugin`), so no Phase 1a
  manifest changes meaning. A name that is not a legal identifier is
  `malformed` — a registry entry is operator-supplied data, and this value is
  interpolated into a module specifier. Enforced in
  `lab-shell/src/shell/registry/manifestSchema.js`; the id remains the identity
  everywhere above the loader.
- **`remote.module` may be spelled with or without federation's `./`
  prefix.** The adapter strips a leading `./`, because the plugin's own
  `vite.config.js` writes `exposes: { './plugin': ... }` and an operator
  copying that spelling into a registry entry should not get a 404 for the
  punctuation.
- **A contribution's `routes` replaced the never-read `routeMatch`** on the
  service-side registry struct (`accounts-service/accounts/frontendplugins.go`).
  Phase 1a shipped the wrong field name; the shell has always read `routes`.
  Corrected before any curated entry used it.
- **`enabled` is a pointer in the service's struct and always present in the
  served document.** A curated *file* must be able to distinguish "absent" from
  "false" — a plain `bool` would turn every entry that omits the flag into a
  disabled one — while the document the shell reads states it explicitly,
  because the flag decides whether code is fetched.
- **Curation may come from a file.** `accounts-service` reads
  `FRONTEND_PLUGIN_REGISTRY_FILE` at startup and falls back to the compiled-in
  set when the file is missing or malformed, logging rather than failing to
  start — the same "degraded, not broken" posture BR-AS04 requires of the
  shell. This is what keeps a `localhost:7110` development remote out of the
  platform's own source, and what makes BR-AS03 true of the *service* as well
  as the shell.

Three defects the live BR-AS15 review turned up, all in Phase 1a code that
only a running remote could exercise:

- **The route-level failure panel showed the underlying error text.** A
  federation failure quotes the remote's URL in its message, so
  `PluginErrorView` was printing `http://localhost:7110/...` on screen — a
  BR-AS04 leak that no built-in plugin could have produced, since a built-in
  never fails with a URL in hand. The panel now renders stage and cause code
  only, exactly as `PluginSlot` already did, and points at the console.
- **Status records were not reactive**, so the Plugins inventory kept
  reporting `available` beside a feature the user had just watched fail. The
  record stays a plain state machine; `bootShell` wraps each one in
  `reactive()` as it is created.
- **The provided shell object was built with a spread**, which evaluated the
  `inventory` getter once and froze its result at boot. Composition now goes
  through `withRuntime()` (prototype delegation), and a spec asserts a
  post-boot transition is visible through the composed object.

## As-built contract deltas (Phase 1b, mockup-fidelity pass)

The BR-AS14 artboards were approved as a design, and approval is not
construction: a comparison of the running shell against
`lab-shell/diagrams/phase1-shell-mockups/` found the chrome carrying less of
the design's information than the artboards do. Closing that gap added two
optional fields and one shared token block, all recorded here.

- **`version` is a new optional manifest field.** Free-form, never
  interpreted: compatibility stays decided by `schemaVersion` and
  `shellApiVersion` alone (BR-AS13). It exists so the inventory and the failure
  panel can name *which build* of a plugin is on screen, which is the first
  question asked of any failure report. Absent renders as `built-in`.
- **`revision` is a new optional registry-document field.** Opaque to the
  shell — displayed in the Plugins header and the footer, and nothing else —
  so an accounts-service that omits it still serves. It names *which* registry
  the shell read; the endpoint itself is never displayed (BR-AS04).
- **`--ok` / `--warn` / `--err` moved into `shared/unifi-theme/unifi.css`.**
  They existed only in the static composition reference, so every `var(--ok)`
  in a real app resolved to nothing and fell back to `currentColor` — which is
  how the shell shipped with `available`, `failed` and `incompatible` all
  rendering in body text colour. This is a shared-theme fix, not a lab-shell
  one: every app in the repo drawing a status colour was affected.

Three chrome behaviours the artboards specify and the build did not have:

- **The nav marks the plugin that is in trouble, and only that one.** A `failed`
  entry takes an error dot, an `incompatible` one a warning dot; `disabled` is
  deliberately unmarked, because an operator switching a plugin off is not a
  fault and a signal that fires on intended states gets ignored. A sibling's
  failure never dots a healthy entry — that isolation is the claim BR-AS04
  makes, so the chrome has to show it.
- **The topbar carries one aggregate signal** — `1 plugin failed` /
  `N need attention`, linking to the inventory — so a failure is visible from
  every screen without opening it. Status words only, never a cause string: a
  cause can quote a remote URL (BR-AS04).
- **The breadcrumb attributes the screen to its plugin by display name.** Two
  segments, never more: the shell's route table is one level deep by
  construction, and a trail that grew arms would invent structure the router
  does not have.

The failure panel now matches the Failed artboard: a monospace block naming
plugin, route, stage and cause; Retry, Plugin status and Back to demos; and the
BR-AS04 footnote stating that error summaries never include credentials, tokens
or registry URLs. **Retry is a real second attempt, not a page reload** —
`failed -> loading` is a legal transition and the loader drops its cached
in-flight promise on failure, so re-entering the route runs the whole load
again.

| Rule | Check |
| --- | --- |
| BR-AS04 — which statuses the chrome marks | `statusRollup.spec.js` — `failed` and `incompatible` are marked, `disabled` and every transient status are not; the summary phrase and tone are asserted per set |
| BR-AS04 — the Detail column is shell-authored | `inventoryText.spec.js` — a failed row reports stage and cause code and contains no `http` |
| BR-AS04 — stage never leaks the message | `failureStage.spec.js` — every cause code maps to a shell-owned stage label, and an unmapped code reads `unknown` rather than blank |
| BR-AS04 — retry, not reload | `PluginErrorView.spec.js` — the Retry button calls `loader.load` with the plugin's manifest |
| Breadcrumb attribution | `breadcrumb.spec.js` — a plugin screen is attributed to the plugin's display name, a shell screen to the shell, and a plugin with no record falls back to its curated id |
| Loading affordance names what is arriving | `PluginSlot.vue` labels a reserved panel `{kind} contribution — {qualifiedId}`; `navigationPending` carries the same metadata for a deep link |

### How Phase 1b's claims are checked

| Rule | Check |
| --- | --- |
| BR-AS03 — no host rebuild | `lab-shell/tools/hostBundleFingerprint.mjs --record` / `--verify` around an independent plugin rebuild: the host digest must be identical, and the host bundle must contain no plugin name, container name or remote URL |
| BR-AS08 — lazy loading | `federatedAdapter.spec.js` (nothing is registered or fetched until a load is asked for) and `PluginSlot.spec.js` (a mounted slot loads; an unmounted contribution does not) |
| BR-AS08 — deep link | `navigationPending.spec.js` — a deep link into an unloaded remote shows a pending frame, which clears on settle *and* on error |
| BR-AS04 — isolation | `ExtensionRegion.spec.js` — a failing sibling leaves the healthy contributions in the same region rendered |
| BR-AS04 — error denylist | `PluginSlot.spec.js` asserts the rendered card contains no URL, host, port or credential from the underlying error; the detail goes to the console |
| BR-AS13 — incompatible | a curated entry with `shellApiVersion: 2`, refused on metadata with its remote never fetched |
| BR-AS04 — route-level denylist | `PluginErrorView.spec.js` — the rendered panel names the plugin and the cause code and contains no URL, host or port from the federation error |
| BR-AS04 — a live inventory | `bootShell.spec.js` — a transition after boot is observed by `inventory`, both directly and through the composed object the app provides |
| BR-AS01 — no browser-nominated remotes | the four failure modes are curated registry entries, not query parameters — the shell offers no channel by which a browser could select one |
