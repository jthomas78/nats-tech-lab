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

**Testable (runtime), narrowed to what the code actually enforces:** the
shell offers a plugin no channel other than its contributions — it is placed
into a container the host chose, and nothing in the contract hands it the
router, the theme or the shell's own DOM. A contribution the registry refuses
is recorded in `refusals` and never placed, and a contribution that throws on
activation transitions its plugin to `failed` with the rest of the shell
still rendering.

What is **not** claimed: containment. A plugin's code runs in the shell's JS
realm with full access to `document`, so a plugin determined to mount a second
`AppShell` or write outside its container can, and no assertion in this repo
prevents it. The rule is a contract plugins are expected to honour, enforced
by review and by the registry being curated (BR-AS01) — not a sandbox. Stating
it as though the shell contained a hostile plugin would be a false claim, and
it is the same realm-sharing fact that makes BR-AS25 a security rule rather
than a hygiene one.

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

**Gate — PASSED 2026-08-28.** Phase 10 (SeaFreight Flow migration, renumbered
from 2 on 2026-08-28) did not open until the user had reviewed the running
example plugin. This is a human gate on capability and integration, recorded
in the plan — the automated assertions above are necessary for it but do not
substitute for it. **The user reviewed the running plugin on 2026-08-28,
after the mockup-fidelity pass, and signed off**; the rule stands for any
future shell contract change that would need re-proving.

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
- **`enabled` is always present in the served document**, because the flag
  decides whether code is fetched. It was a *pointer* in the file-backed
  struct so a hand-edited file could distinguish "absent" from "false";
  Phase 2a made it a plain `bool`, since a stored row always states it.
- **~~Curation may come from a file.~~** Superseded by Phase 2a. Curation is
  rows in `registry.entries`, written through the module's endpoints;
  `FRONTEND_PLUGIN_REGISTRY_FILE` and the boot-time file read are gone
  (decision 24 — a boot-time file read alongside a database would let a
  restart silently revert curation). `registry.dev.json` survives as the
  *seed input* for `cmd/seed-registry`, which writes over REST against a
  running service. What made BR-AS03 true of the service is now the database,
  not the file.

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

## Phase 2 — the registry as service state (BR-AS16 to BR-AS26)

Confirmed at the design gate on 2026-08-28, alongside decisions 22–46 in
`.claude/plans/Application-Shell-Microfrontend-Plan.md`. Implemented in Phases
2–4; the current transport is NATS request/reply. They live here rather than in `BUSINESS_RULES-ACCOUNTS.md` because
they describe the shell's registry contract whichever service hosts it, and so
survive the Phase 6 move (decision 40).

- **BR-AS16 — The registry is service state.** The shell's registry response is
  served from the hosting service's own store. A curated entry added or removed
  through the admin surface is visible to a newly booting shell **without
  restarting any service**.
- **BR-AS17 — Revision is server-assigned and monotonic.** Every response
  carries a `revision` the server assigned; it increases on every accepted write
  and never repeats. The entries and the revision are installed together or not
  at all — a set may never be served under the previous revision.
- **BR-AS18 — Writes are revision-checked.** A write carrying a stale revision
  is refused, not merged. Two curation decisions are never combined by the
  server.
- **BR-AS19 — A registry change notifies, and never unloads.** A revision change
  is published on `notify._platform.registry.frontend-plugins.changed`, and
  becomes visible to a running shell through a conditional NATS read triggered
  by that hint. Every reconnect reads unconditionally; there is no focus or
  interval watcher and no HTTP fallback (Phase 4, decisions 55–58). A shell with an
  active plugin whose entry was removed keeps rendering it and offers a reload:
  the status machine has no transition out of `active`, so a reload is the only
  sound way to apply a removal.

  **A new entry id is the only difference a running shell may apply to itself
  (decision 46).** Every other difference between the entry it holds and the
  entry that arrives — a changed label, order, route prefix, permission,
  version, remote or contribution list — is a reload offer. The rule is stated
  as a whitelist because the write path is `ON CONFLICT DO UPDATE`, which
  replaces the whole entry: a single transaction that edits plugin A and adds
  plugin B arrives as one document, and a shell that applies only the addition
  is left holding a catalog that existed at no revision. The comparison is a
  deep equality over the *validated* manifest, so the two sides are normalised
  the same way before they are compared.

  A live addition must reach the **screen**, not only the shell's collections:
  the contribution state is reactive at its source, because a reader's
  `computed()` over a getter that returns a copy registers no dependency and is
  never invalidated.
- **BR-AS20 — Origin allowlist, enforced on write and on read.** An entry whose
  remote URL is not on the service's configured origin allowlist is refused at
  write time **and** withheld at read time. The read-side check is not
  redundant: narrowing the allowlist leaves already-stored rows non-conforming,
  which is the case the write-time check cannot cover.
- **BR-AS21 — No self-registration.** No transport permits a plugin to add,
  modify or enable its own registry entry. This is BR-AS01 restated for a write
  path that did not previously exist.
- **BR-AS22 — The registry degrades, it does not fail.** With Postgres
  unavailable the read falls back to the KV cache; with both unavailable the
  read answers successfully with an empty plugin list, `revision: 0` and
  `degraded: true`, and the shell renders its built-ins. It never returns a transport error for this state,
  and a degraded response is distinguishable from a genuinely empty registry.
  There is no server-side "built-in set" to serve: built-ins ship inside the
  shell's own bundle and are deliberately never curated.

  **Degraded is a state the shell leaves (decision 48).** It is cleared on any
  successful read, an `unchanged` answer included, and a read the shell could not complete —
  failed or degraded — discards the conditional token so the next read is
  unconditional. Both halves are needed for the same case: recovery at an
  unchanged revision is exactly what answers `unchanged`, so keeping a pre-outage
  ETag and clearing the flag only on a document made degraded a one-way door.
- **BR-AS23 — The audit records the surface, not an identity.** Every accepted
  and every refused write appends an audit row whose actor is the shared
  administrative identity the request authenticated as. The rule is deliberately
  weaker than "who did it": while the hosting service authenticates every
  request as one shared secret, no stronger claim is true, and neither the
  stored row nor the audit panel may imply one.
- **BR-AS24 — An entry is disabled, never deleted.** No transport removes a
  registry row. A disabled entry is withheld from the read and its history is
  retained.
- **BR-AS25 — The shell's origin holds read capability only.** The subject a
  shell reads uses its dedicated read-only credential; every subject that curates
  the registry — both writes and the two admin reads that disclose withheld
  entries — is behind the operator credential, and no credential that opens them is ever
  presented from the shell's origin. This is not a preference for least
  privilege. Federated plugin code executes in the shell's own JS realm, so
  **any credential the shell's origin can use is a credential every loaded
  plugin holds**; a shell that authenticated its read with the operator
  credential would hand each plugin the ability to curate the registry that
  curates it (BR-AS21 defeated from inside the browser). The boundary is enforced
  by NATS subject permissions. The shell proxies only its exact credential-mint
  route, never the whole `/api/auth` prefix. The read is filtered for a reader,
  so disabled and non-conforming entries are withheld from it. The former HTTP
  read/write routes are retired; no HTTP boot fallback remains.
- **BR-AS26 — A committed write is reported as committed.** Once the write
  transaction commits, no later failure may be reported to the caller as a
  refusal, audited as one, or allowed to skip the cache refresh and the change
  notification. The document a write answers with is read back inside its own
  transaction, and the post-commit steps run on a context detached from the
  request — a caller that hangs up after the commit must not leave the audit
  disagreeing with the database or every watching shell unaware of a revision
  that already happened.

## Phase 4 — the shell's NATS transport (BR-AS27 to BR-AS31)

Phase 4 moves the transport and only the transport. Nothing here changes what the
shell does with a registry document — Phase 3's diff, its reload offers and its
degraded lifecycle all hold unchanged. Design decisions 53–58 and the gate
answers are in `.claude/plans/Application-Shell-Microfrontend-Plan.md`.

- **BR-AS27 — The shell's registry read is a NATS request/reply on an `api.*`
  subject, and its credential carries read capability only.** The shell holds its
  own credential profile, distinct from the three operator profiles the migrating
  apps use. Its permission set names the registry read subject and `_INBOX.>` and
  nothing else — no registry write subject, no `api.>` prefix grant, and never
  `rpc.>` (a browser credential is never granted `rpc.>`, CLAUDE.md).

  This is BR-AS25 restated from proxy shape to transport shape, and it is a
  security rule for the same reason: federated plugin code executes in the
  shell's JS realm, so **the shell's credential is the credential every loaded
  plugin holds**. Reusing an operator profile would hand each plugin that
  operator's write subjects, and the rule would stop being assertable because
  the profile is shared with a writer.
- **BR-AS28 — A change notification is a hint, never a payload.** The shell
  reacts to `notify._platform.registry.frontend-plugins.changed` by performing a
  revision-conditional read. It never installs a catalog from the message body,
  and a message whose revision matches what it already holds changes nothing.
  Two reasons, both load-bearing: the message arrives on a subject with no
  delivery guarantee, and a second path that can install a catalog would be a
  path Phase 3's diff was not written against.
- **BR-AS29 — A reconnected shell re-reads unconditionally.** On every
  re-establishment of the connection the shell performs an unconditional read and
  reconciles by revision. Core NATS is fire-and-forget: a shell that was
  disconnected, backgrounded or reconnecting missed messages with no gap
  detection and no redelivery, so without this rule a shell offline for one
  minute is stale forever — strictly worse than the poll it replaces, which at
  least self-healed.
- **BR-AS30 — First paint precedes the connection.** The shell indexes its
  built-ins and renders before it connects, mints a credential or reads. A shell
  that cannot connect, cannot mint, or is answered with a degraded document still
  renders (BR-AS22 unchanged in effect, restated for the new transport). A
  connection state that is down is surfaced beside the revision in the footer,
  debounced so a routine reconnect does not flicker — visible because a silently
  stale shell is the exact failure BR-AS22 exists to prevent, and never a reason
  to unload a contribution (BR-AS19).
- **BR-AS31 — Curation writes move to `api.*` and stay operator-scoped.** The
  admin write routes leave REST for request/reply on their own subjects, granted
  to the Admin UI's credential and to no other. The revision vocabulary the HTTP
  surface carried is re-implemented in the payload rather than dropped: a read
  states the revision it holds and may be answered `unchanged`; a write states
  the revision it saw and may be refused as stale or as missing. BR-AS21 is
  unaffected — no plugin-reachable subject writes to the registry.

### How Phase 4's rules are checked

| Rule | Checked by |
| --- | --- |
| BR-AS27 — read capability only | `auth/token_test.go` § `MintShellToken` — an exact `ConsistOf` over `Pub.Allow` and `Sub.Allow`, the same idiom that already forces `MintAdminToken`'s set to be a deliberate list. An exact match rather than a "does not contain a write subject" assertion, because the failure to catch is a subject added later that nobody thought about |
| BR-AS28 — hint, never payload | `changeSubscription.spec.js` and `changeSubscription.concurrent.spec.js` — bodies are never installed, matching/older revisions read nothing, bursts coalesce without losing a later revision; `registry/transport_integration_test.go` proves the committed `{revision}` hint over real NATS and Postgres |
| BR-AS29 — reconnect re-reads | `changeSubscription.spec.js` — a reconnect performs an **unconditional** read (no held revision in the payload) and converges on a revision published while the shell was disconnected |
| BR-AS30 — first paint precedes connect | `registrySession.spec.js` mounts built-in nav before paint permits connect, covers refused connect and reactive late read errors; `shellConnection.spec.js` and `shellConnection.lifecycle.spec.js` cover socket lifecycle; `ShellFooter.spec.js` pins the 5000 ms notice and immediate recovery |
| BR-AS31 — writes are operator-scoped | `auth/token_test.go` exact grants; `shell_permissions_test.go` compares the registered subject surface with both profiles; `registry/internal/browserrpc/{adapter,wire}_test.go` pins accepted/stale/missing/origin-refused payloads, including explicit zero; Admin `registryApi.spec.js`, `RegistryNatsPanels.spec.js`, and `connectionFactory.spec.js` cover callers and structured refusals |

### How Phase 2's rules are checked

Phase 2 is split 2a (the module and its store) / 2b (the admin surface) /
2c (the shell notices a change) — see
`.claude/plans/Application-Shell-Microfrontend-Plan.md`. The table therefore
records what a rule is checked *by* today as well as what it will be checked by,
because several of these rules hold right now for the accidental reason that no
write path exists at all.

The admin surface is one nav item, **Frontend Shell**, under the admin UI's
PLATFORM group, with two tabs — `Plugins` (`FrontendPluginsPanel.vue`) and
`Registry Audit` (`RegistryAuditPanel.vue`), wrapped by `FrontendShellView.vue`.
The catalog and its write history are one subject read two ways, so the rules
below that name either panel are reached through that one item.
`FrontendShellView.spec.js` holds the tab order and the only-the-active-tab-
mounts rule.

| Rule | Check |
| --- | --- |
| BR-AS16 — service state | *2a, done.* `registry/store_integration_test.go`, against real Postgres: an entry applied through `Apply` is present in the next `Current`, and reads back through a *second* `Store` so nothing is held in process memory |
| BR-AS17 — monotonic revision | *2a, done.* `domain.NextRevision` in `registry/rules_test.go`; end to end in `store_integration_test.go` — first write is revision 1, three accepted writes are 1/2/3, and `Apply` returns the entries and the revision together. The two setters (`SetCuratedFrontendPlugins`/`SetCuratedFrontendRevision`) are gone with the endpoint |
| BR-AS18 — revision-checked writes | `domain.CheckRevision` and `store_integration_test.go` pin both directions and no revision consumed on refusal; `browserrpc/adapter_test.go` and `wire_test.go` pin missing/stale payload refusals. Explicit JSON `ifRevision: 0` is valid for the first curation; omission/null never reaches the store. `FrontendPluginsPanel.spec.js` and `RegistryNatsPanels.spec.js` show both revisions and offer reload, never merge/force |
| BR-AS19 — notify, never unload | `registryTransport.spec.js` pins conditional read/unchanged; `changeSubscription.spec.js` pins push/reconnect instead of the deleted watcher. `registrySession.spec.js` covers recovery and the boot-read/subscription gap. `phase2RegistryContract.spec.js`, `registryDiff.spec.js`, `RegistrySignalBanner.spec.js`, and `FrontendPluginsPanel.spec.js` preserve reload offers and leave existing contributions running |
| BR-AS20 — origin allowlist | *2a, done.* Refused on write (`store_integration_test.go`: nothing stored, revision unmoved) and withheld on read (`rules_test.go`: `Document.Readable` against a narrowed allowlist and an already-stored row, leaving the stored document unmutated). `NewAllowlist(nil)` permits nothing — empty is not allow-all. *2b, done.* `FrontendPluginsPanel.spec.js` — a non-conforming entry is listed as `withheld` rather than dropped, and the 422 is surfaced by stage and cause with neither the URL nor the configured origins echoed back (BR-AS04) . The allowlist itself is shown on the panel as service configuration with no control to widen it |
| BR-AS21 — no self-registration | `browserrpc/adapter_test.go` pins the exact five registered subjects, `auth/token_test.go` pins their grants, and `rest_test.go` pins an empty route list and 404s. `set-enabled` cannot insert (`adapter_test.go`, `store_integration_test.go`) |
| BR-AS22 — degrades, does not fail | *2a* for the response shape; *2c* for the shell's handling. `manifestSchema.js` passes `degraded` through, `bootShell.applyRegistry` records it and skips the diff (a document the service could not vouch for is no basis for offering a reload), and `ShellFooter.spec.js` pins the three-way distinction: normal, degraded, and unreachable. `phase2RegistryContract.spec.js` still pins `revision: 0` validating as `"0"`, which is what lets 0 carry the degraded meaning |
| BR-AS23 — audit records the surface | *2a, done.* `store_integration_test.go`: an accepted write appends a row against the revision it installed, a refused one appends a row with no revision. The actor is `domain.SharedAdminActor` (the literal `admin`), and `rules_test.go` refuses an authorless write outright. *2b, done.* `RegistryAuditPanel.spec.js` — accepted and refused writes are listed together, a refused row shows no revision, and the actor column shows the shared `admin` identity with a note saying so |
| BR-AS24 — disable, never delete | `domain.WriteOps()` is exhaustively pinned by `registry/rules_test.go`; `browserrpc/adapter_test.go` allows no delete/remove subject and `rest_test.go` proves retired paths unreachable. `store_integration_test.go` retains disabled rows/history; `FrontendPluginsPanel.spec.js` offers enable/disable, never delete |
| BR-AS25 — the shell's origin reads only | `TestShellReadIsUngatedAndEverythingElseIsNot` is preserved as a subject-permission assertion in `auth/shell_permissions_test.go`, comparing actual registered subjects with shell/admin JWTs. `MintShellToken`'s exact grants and the shell's exact mint proxy preserve the boundary; the old HTTP registry surface and proxy rules are removed |
| BR-AS26 — a committed write is reported as committed | *3b, done.* `postgres.Store.apply` reads the installed document through `currentDoc(ctx, tx)` **inside** the transaction and commits last, so every error path it returns is one that rolled back — which is what makes `Apply` auditing them all as refusals true. `auditRefusal` and the post-commit cache refresh and notify run on `context.WithoutCancel(ctx)`. `store_integration_test.go` § decision 49 pins all three: a cancelled caller leaves an *accepted* audit row, an already-dead context still records a refusal, and a refused write moves no revision |
| decision 27 — the read contract is unchanged | *Now, and this is the load-bearing one.* `phase2RegistryContract.spec.js` characterizes `validateRegistryDocument`: `revision` is accepted as a string *or* a number and stringified, `0` survives, absent is `null` not an error, and a schema-version move rejects the whole document. Phase 2 replaces `"dev-1b"` with a monotonic integer on the strength of these |
| decisions 34/58 — one read subject | `registryTransport.spec.js` pins `SHELL_READ_SUBJECT` and the held-revision payload. Historical HTTP characterization remains in `phase2RegistryContract.spec.js`/`registryClient.spec.js`, but the host uses no HTTP client or fallback |
