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

**Phase 5 status (2026-09-01): COMPLETE and verified live.** BR-AS52–65 and the amendments below
describe implemented behavior; their coverage table names specs that exist and pass. The live
1920×1080 walkthrough ran against the full Docker stack — and found three deployment defects the
suites could not see, recorded with the matrix.

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

**Which profiles exist is the host's to declare, since 2026-09-02.** The rule
is the keying, not the list: `createConnectionRegistry` is told the profiles
that exist and which are tenant-scoped, and refuses any other. The list used to
be frozen in the module while exactly one member of it could be dialled, so the
host had to reject the other four a second time at connect, and every spec
above proved the rule against profiles nothing could reach. A profile arrives
when the credential behind it does. The migration map itself lives in
`ARCHITECTURE-APP-SHELL.md`.

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
**federated feature, not the shell**, which is the cross-owner case the proof
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

A failed plugin or contribution must not prevent the native shell frame or other plugins from loading and operating.

Isolation is at **contribution granularity**, not plugin granularity: a
plugin whose route fails still renders its panel contribution elsewhere if
that panel is sound.

**Testable:** four distinct failure modes each leave the shell and every
other plugin operating — (a) the remote 404s, (b) the remote loads but
`activate()` throws, (c) a rendered contribution throws during render,
(d) the registry endpoint itself is unreachable. In (d) the shell renders
its native Home and Plugins frame, with zero plugins. Error summaries surfaced to the user carry stage and
cause and **never** credentials, tokens, or registry URLs — assert the
rendered error text against a denylist.

### BR-AS03 — Independent deployment

Adding, removing, enabling, disabling, or upgrading a compatible remote
plugin must not require a shell source change or shell rebuild.

**Testable:** two successive registry responses differing by one added entry
produce a shell with one more plugin, with no change to any file under
`lab-shell/src/`. Proven end to end in Phase 1b by deploying the example
remote twice without rebuilding the host.

Containerising both sides did not weaken this. The shell's image is built from
`lab-shell/` with `plugins/` deliberately excluded from its build context, so
the plugin's source cannot reach the host bundle even by accident; the plugin
has its own Dockerfile, lockfile and image. A second plugin costs a compose
service and a registry row — never a shell rebuild. `hostBundleFingerprint.mjs`
still asserts the host bundle names no plugin, container or remote origin (its
denylist covers all five plugin origins, both catalog identity spellings and demo README text).

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
  **and** one into a target owned by another federated feature
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
| Curated registry endpoint (BR-AS01) | `mfe-registry-service/registry/` — `api._platform.mfe-registry.*` subjects, served by its own process since 2026-08-31. The subject list is `shared/mferegistry`, read by this service and by `accounts-service`, which grants the subjects when it mints a browser credential (BR-AS25/AS27). |
| Demo catalog, as a plugin like any other (BR-AS15) | `lab-shell/plugins/demo-catalog/` — independent federated package on **7112**, routes under `/demos` |
| App shell deployment | `lab-shell/Dockerfile` + `lab-shell/nginx.conf` — containerised on **7110** as `app-shell-frontend`; the dev server publishes the same port. Its nginx proxies exactly two paths: the single exact `/api/auth/shellConnectInfo` mint route and the `/nats` WebSocket. Every other `/api/` path returns 404 rather than the SPA page, so a misroute is loud. |
| Example proof plugin (BR-AS15) | `lab-shell/plugins/example-plugin/` — its own build and image, with build output served and announced by the shared Go `mfe-plugin-host` on **7111** as `example-plugin-frontend` (dev server on the same port). Its path-only manifest is signed and announced at runtime; it is not a preload row. |
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
  question asked of any failure report. Absent renders as `—`.
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
- **BR-AS19 — A registry change notifies; ordinary changes offer reload.** A revision change
  is published on `notify._platform.mfe-registry.frontend-plugins.changed`, and
  becomes visible to a running shell through a conditional NATS read triggered
  by that hint. Every reconnect reads unconditionally; there is no focus or
  interval watcher and no HTTP fallback (Phase 4, decisions 55–58). A shell with an
  active plugin whose entry was removed keeps rendering it and offers a reload:
  the status machine has no transition out of `active`, so a reload is the only
  sound way to apply a removal.

  **An offer describes the document the shell last read, so a read that no
  longer supports it takes it back.** The pending offers are synced with each
  successful read rather than accumulated across reads: a withdrawn entry that
  comes back unchanged retracts its offer, one that comes back edited replaces
  it, and every other offer the read still produces stands. A read carrying no
  document — `unchanged`, or degraded — retracts nothing. Retraction withdraws
  the offer and never the plugin: nothing is applied or unloaded either way.
  (Found live: an operator disabling an entry and re-enabling it seconds later
  left every running shell claiming a plugin it was still rendering had been
  withdrawn.)

  **Decided in one place, done in another, since 2026-09-02.** What a read
  means — failed, degraded, `unchanged`, or a document the service vouched for,
  and what each one licenses — is a pure function, `shell/registry/readPolicy.js`.
  `bootShell` installs what it returns and does nothing else with it. The rules
  above are the shell's hardest reasoning and they were previously assertable
  only by booting a shell, admitting manifests and reading back reactive state,
  so a spec about "what a degraded read may retract" had to go through three
  collaborators that have nothing to do with the question. No rule changed in
  the move; `readPolicy.spec.js` states them directly.

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

  **Phase 5 amendment, built 2026-09-01:** the paragraphs above describe the
  ordinary-change behavior. BR-AS53–59 add explicit dynamic withdrawal and unchanged return;
  static removal/disable and changed runtime definitions still offer reload. A held lifecycle
  class changes only on reload. BR-AS49's forced security reload already overrides this rule
  for either class; a degraded/missing document never authorizes ordinary withdrawal.

  A live addition must reach the **screen**, not only the shell's collections:
  the contribution state is reactive at its source, because a reader's
  `computed()` over a getter that returns a copy registers no dependency and is
  never invalidated.
- **BR-AS20 — Origin allowlist, enforced on write and on read.** An entry whose
  remote URL is not on the service's configured origin allowlist is refused at
  write time **and** withheld at read time. The read-side check is not
  redundant: narrowing the allowlist leaves already-stored rows non-conforming,
  which is the case the write-time check cannot cover.
  **Filtered once, clarified 2026-09-02.** The shell-facing document is produced
  by `Service.Read` and by nothing else; the browser adapter serves what it is
  handed and adds no second pass. The removed second pass was defence against
  nothing — `Readable`'s inputs are identical on both calls — and it was not
  free: a withdrawal marker had to carry `Enabled: true` purely to survive being
  filtered again, which put a lie in the domain to satisfy an adapter.
  `domain.Withdrawal` now carries the mark and the id only.
- **BR-AS21 — No self-activation.** A service may announce its own entry over
  `rpc._platform.mfe-registry.entries.announce.v1` when it presents a verified
  publisher key. It may never enable an entry, nor alter `enabled`, `lifecycle`
  or `revision` on any entry, including its own. An announced entry that is not
  already enabled lands `announced` and is inert until an operator enables it.
  No browser transport permits announcement at all.
  **Phase 5 extension, built 2026-09-01:** an owned, verified publisher may unregister
  its dynamic plugin using a new service-only signed command. This changes publisher availability,
  not operator enablement. Valid return may reuse existing approval; it cannot override operator
  disable or revoked trust (BR-AS54–55).
- **BR-AS22 — The registry degrades, it does not fail.** With Postgres
  unavailable the read falls back to the KV cache; with both unavailable the
  read answers successfully with an empty plugin list, `revision: 0` and
  `degraded: true`, and the shell renders its native frame. It never returns a transport error for this state,
  and a degraded response is distinguishable from a genuinely empty registry.
  There is no substitute plugin set: the catalog is also federated. Previously
  discovered plugins remain in a running shell; an initial degraded read loads none.

  **Degraded is a state the shell leaves (decision 48).** It is cleared on any
  successful read, an `unchanged` answer included, and a read the shell could not complete —
  failed or degraded — discards the conditional token so the next read is
  unconditional. Both halves are needed for the same case: recovery at an
  unchanged revision is exactly what answers `unchanged`, so keeping a pre-outage
  token and clearing the flag only on a document made degraded a one-way door.

  **The conditional token IS the revision (decision 58, spelling settled
  2026-09-02).** It travelled as a quoted ETag string while the catalogue came
  over HTTP. Over a subject there is no header to shape, so the shell holds
  `registry.heldRevision` — a number — and the read carries it as
  `heldRevision`. One vocabulary end to end: no client reconstructs a quoted
  token from a revision and no reader parses one back.
- **BR-AS23 — The audit records the surface, not an identity.** Every accepted
  and every refused curation write appends an audit row whose actor is the shared
  administrative identity. This does not identify an individual operator, and
  neither the stored row nor the audit panel may imply it does. Phase 8's
  BR-AS42 extends the actor vocabulary to `preload` and a verified publisher
  key; neither path is misattributed to the shared admin identity.
- **BR-AS24 — An entry is disabled, never deleted.** No transport removes a
  registry row. A disabled entry is withheld from the read and its history is
  retained. **Phase 5 extension, built 2026-09-01:** publisher unregister also retains
  the row/history and records withdrawal separately from operator disable (BR-AS55).
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
  reacts to `notify._platform.mfe-registry.frontend-plugins.changed` by performing a
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

  **One machine decides when to read, since 2026-09-02.** Ordering and
  coalescing — at most one read at a time, and what a hint or a reconnect that
  lands during one is worth — belong to `changeSubscription.js`, including the
  boot read and the read that closes the snapshot/subscription gap.
  `registrySession.js` starts and stops it and says what a read *is* (which
  token to send, where the result goes) and nothing more. The two used to hold
  half a machine each, and each half was blind to the other's: the session
  queued reads the subscription did not know were outstanding, and kept its own
  reconnect counter because `onReconnect` would not hold one before `start`.
  Behaviour is unchanged; `changeSubscription.spec.js` now also pins a
  reconnect during boot and a hint landing behind a queue of reads.
- **BR-AS30 — First paint precedes the connection.** The shell renders its
  native Home and Plugins frame before it connects, mints a credential or reads. A shell
  that cannot connect, cannot mint, or is answered with a degraded document still
  renders (BR-AS22 unchanged in effect, restated for the new transport). A
  connection state that is down is surfaced beside the revision in the footer,
  debounced so a routine reconnect does not flicker — visible because a silently
  stale shell is the exact failure BR-AS22 exists to prevent, and never a reason
  to unload a contribution (BR-AS19). Paint is the gate, but not an unbounded
  one: a tab that is never displayed never composites a frame, so the wait falls
  back to a 1 s timeout — a shell deep-linked into a background tab connects and
  reads on its own rather than waiting for the first look at it.
- **BR-AS31 — Curation writes move to `api.*` and stay operator-scoped.** The
  admin write routes leave REST for request/reply on their own subjects, granted
  to the Admin UI's credential and to no other. The revision vocabulary the HTTP
  surface carried is re-implemented in the payload rather than dropped: a read
  states the revision it holds and may be answered `unchanged`; a write states
  the revision it saw and may be refused as stale or as missing. BR-AS21 is
  preserved as no self-activation — no browser-reachable subject permits announcement.

### How Phase 4's rules are checked

| Rule | Checked by |
| --- | --- |
| BR-AS27 — read capability only | `auth/token_test.go` § `MintShellToken` — an exact `ConsistOf` over `Pub.Allow` and `Sub.Allow`, the same idiom that already forces `MintAdminToken`'s set to be a deliberate list. An exact match rather than a "does not contain a write subject" assertion, because the failure to catch is a subject added later that nobody thought about |
| BR-AS28 — hint, never payload | `changeSubscription.spec.js` and `changeSubscription.concurrent.spec.js` — bodies are never installed, matching/older revisions read nothing, bursts coalesce without losing a later revision; `registry/transport_integration_test.go` proves the committed `{revision}` hint over real NATS and Postgres |
| BR-AS29 — reconnect re-reads | `changeSubscription.spec.js` — a reconnect performs an **unconditional** read (no held revision in the payload) and converges on a revision published while the shell was disconnected; also pins one read at a time, a reconnect held from before `start`, and a hint held behind a queue |
| BR-AS30 — first paint precedes connect | `afterPaint.spec.js` pins the two-frame gate, the 1 s no-frame fallback and the single settle; `registrySession.spec.js` mounts native navigation before paint permits connect, covers refused connect and reactive late read errors; `shellConnection.spec.js` and `shellConnection.lifecycle.spec.js` cover socket lifecycle; `ShellFooter.spec.js` pins the 5000 ms notice and immediate recovery |
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
| BR-AS19 — notify, never unload | `registryTransport.spec.js` pins conditional read/unchanged; `changeSubscription.spec.js` pins push/reconnect instead of the deleted watcher. `registrySession.spec.js` covers recovery and the boot-read/subscription gap. `phase2RegistryContract.spec.js`, `registryDiff.spec.js`, `RegistrySignalBanner.spec.js`, and `FrontendPluginsPanel.spec.js` preserve reload offers and leave existing contributions running; `liveChange.spec.js` pins retraction on a re-added entry, replacement on an edited one, and no retraction from an unchanged or degraded read; `readPolicy.spec.js` states each outcome's licence — failed, degraded, 304, document — without booting a shell |
| BR-AS20 — origin allowlist | *2a, done.* Refused on write (`store_integration_test.go`: nothing stored, revision unmoved) and withheld on read (`rules_test.go`: `Document.Readable` against a narrowed allowlist and an already-stored row, leaving the stored document unmutated). `NewAllowlist(nil)` permits nothing — empty is not allow-all. *2b, done.* `FrontendPluginsPanel.spec.js` — a non-conforming entry is listed as `withheld` rather than dropped, and the 422 is surfaced by stage and cause with neither the URL nor the configured origins echoed back (BR-AS04) . The allowlist itself is shown on the panel as service configuration with no control to widen it |
| BR-AS21 — no self-activation | `browserrpc/adapter_test.go` pins the exact five registered subjects, `auth/token_test.go` pins their grants, and `rest_test.go` pins an empty route list and 404s. `set-enabled` cannot insert (`adapter_test.go`, `store_integration_test.go`). Phase 8 adds `announce_transport_test.go` (fail-closed verification and pending entries) and `auth/announce_permissions_test.go` (the broker refuses shell announcement) |
| BR-AS22 — degrades, does not fail | *2a* for the response shape; *2c* for the shell's handling. `manifestSchema.js` passes `degraded` through, `bootShell.applyRegistry` records it and skips the diff (a document the service could not vouch for is no basis for offering a reload), and `ShellFooter.spec.js` pins the three-way distinction: normal, degraded, and unreachable. `phase2RegistryContract.spec.js` still pins `revision: 0` validating as `"0"`, which is what lets 0 carry the degraded meaning |
| BR-AS23 — audit records the surface | *2a, done.* `store_integration_test.go`: an accepted write appends a row against the revision it installed, a refused one appends a row with no revision. The actor is `domain.SharedAdminActor` (the literal `admin`), and `rules_test.go` refuses an authorless write outright. *2b, done.* `RegistryAuditPanel.spec.js` — accepted and refused writes are listed together, a refused row shows no revision, and the actor column shows the shared `admin` identity with a note saying so |
| BR-AS24 — disable, never delete | `domain.WriteOps()` is exhaustively pinned by `registry/rules_test.go`; `browserrpc/adapter_test.go` allows no delete/remove subject and `rest_test.go` proves retired paths unreachable. `store_integration_test.go` retains disabled rows/history; `FrontendPluginsPanel.spec.js` offers enable/disable, never delete |
| BR-AS25 — the shell's origin reads only | `TestShellReadIsUngatedAndEverythingElseIsNot` is preserved as a subject-permission assertion in `auth/shell_permissions_test.go`, comparing actual registered subjects with shell/admin JWTs. `MintShellToken`'s exact grants and the shell's exact mint proxy preserve the boundary; the old HTTP registry surface and proxy rules are removed |
| BR-AS26 — a committed write is reported as committed | *3b, done.* `postgres.Store.apply` reads the installed document through `currentDoc(ctx, tx)` **inside** the transaction and commits last, so every error path it returns is one that rolled back — which is what makes `Apply` auditing them all as refusals true. `auditRefusal` and the post-commit cache refresh and notify run on `context.WithoutCancel(ctx)`. `store_integration_test.go` § decision 49 pins all three: a cancelled caller leaves an *accepted* audit row, an already-dead context still records a refusal, and a refused write moves no revision |
| decision 27 — the read contract is unchanged | *Now, and this is the load-bearing one.* `phase2RegistryContract.spec.js` characterizes `validateRegistryDocument`: `revision` is accepted as a string *or* a number and stringified, `0` survives, absent is `null` not an error, and a schema-version move rejects the whole document. Phase 2 replaces `"dev-1b"` with a monotonic integer on the strength of these |
| decisions 34/58 — one read subject | `registryTransport.spec.js` pins `SHELL_READ_SUBJECT` and the held-revision payload. Historical HTTP characterization remains in `phase2RegistryContract.spec.js`/`registryClient.spec.js`, but the host uses no HTTP client or fallback |


## Phase 5 — approved requirements, as built

Approved 2026-08-31 after 14 user decisions and explicit permission to update planning documents,
and built out across 5a–5e. **BR-AS52–65 are unique new IDs.** The old plan-only draft BR-AS30–34
is superseded; canonical Phase 4 BR-AS30/31 are unchanged. Every rule below carries executable
coverage — Go domain rules in Ginkgo `Context`s, real shell/Admin behavior in Vitest mounted specs
— named row by row in the matrix that follows, which is also where the few remaining gaps are
stated rather than implied.

- **BR-AS52 — Lifecycle is explicit and preserved.** Each full registry entry is served with
  `static` or `dynamic`; BR-AS49 security tombstones retain their minimal shape. Source never
  determines shell semantics. Backfill unclassified legacy entries as
  static without disabling them, changing ownership or rewriting signed manifest bytes. The operator
  can inspect/edit lifecycle; publishers cannot supply it in manifests. A running shell holds the
  class it admitted until reload, offering reload for a class edit.
- **BR-AS53 — Static changes require reload.** Removing/disabling an admitted static plugin leaves
  its contributions running and offers reload. Changed runtime definitions require reload for both
  classes. This rule never weakens BR-AS49: revoked trust forces reload for either class. Degraded or
  incomplete reads cannot be interpreted as ordinary withdrawal.
- **BR-AS54 — Dynamic withdrawal requires an authoritative explicit action.** Operator disable or
  signed publisher unregister withdraws a dynamic plugin live. A crashed service, timeout, disconnect,
  health result, drift result or filtered/missing entry alone never does. Unregister must verify
  action-bound signature, current publisher/key trust and ownership, replay order and commit-time
  authorization. Duplicate/stale/replayed actions cannot reverse newer accepted state. Accepted and
  refused actions identify the true actor in audit, with no mutation/revision change on refusal.
- **BR-AS55 — Publisher availability never overrides operator approval.** Unregister retains the
  row, approval and history; publisher withdrawal is stored separately from operator enablement.
  A valid owned reannouncement can restore availability within the approved origin only while trust
  remains valid and the operator has not disabled the entry. Cross-origin return needs approval.
  Restart, replay and concurrent operator writes must preserve these boundaries; unknown IDs cannot
  obtain approval through unregister/return.
- **BR-AS56 — Withdrawal removes owned contributions, not modules or siblings.** Remove the
  plugin's routes for new navigation, navigation entries, controls, footer items and extensions.
  Mark it withdrawn; retain its loaded module and activation results. Repeated withdrawal is safe,
  sibling contributions remain, and import/activation finishing late cannot resurrect withdrawn UI.
  No promise is made to cancel plugin callbacks or fully dispose its resources.
- **BR-AS57 — The occupant stays at the withdrawn route.** Replace the occupied view in place with
  a shell-owned withdrawal explanation and a link back; do not redirect. New navigation/deep links
  cannot enter that withdrawn route. An unchanged authorized return can restore the route in place;
  a changed definition offers reload. This does not promise recovery of unsaved component state.
- **BR-AS58 — Slot withdrawal suspends placements, not their contributors.** When a withdrawn
  plugin owns a slot, suspend only placements targeting that slot. Their contributing plugins remain
  active in other locations. Restore those placements exactly once when the unchanged slot returns,
  provided their contributors are still eligible; host-owned slots and unrelated placements survive.
- **BR-AS59 — Unchanged return reuses activation.** A withdrawn plugin that returns unchanged and
  authorized restores its cached contributions without another `activate()` call. A never-loaded
  plugin remains lazy. Runtime definition equality includes version, remote, contracts, contributions,
  routes, permissions, labels and ordering; changes require reload. Platform control, health and
  signature metadata do not alone mean new code, but never bypass independent trust checks or the
  held-class rule. Return cannot duplicate registrations or restore an ineligible placement.
- **BR-AS60 — Frontend and backend health are separate decorations.** Both static and dynamic
  plugins expose independent frontend/backend results in navigation and the Plugins inventory.
  Health never removes, disables, reorders or automatically reloads content. Background failure
  stays inline with safe stage/cause, no URL/host/port/credential, and no unsolicited modal. Existing
  loading/render errors stay visible independently of probe results.
- **BR-AS61 — A plugin reports its own frontend health, and each report costs a real local request.**
  Rewritten 2026-09-02 (Phase 15, decision 14); the responsibility is unchanged, the transport and
  the direction are not. Nobody outside the plugin probes it. The plugin's own publisher checks
  itself with a genuine bounded `GET` against its loopback `/healthz`, expecting a small validated
  response, and publishes the result on a subject derived from the plugin id in the signed catalogue
  entry — never from a deployment-supplied map of origins and never from anything a manifest says. A
  report that did not cost a real request would attest that the publisher is running, which is not
  what is being reported. The self-check deadline must expire strictly before the heartbeat interval,
  so two checks are never in flight for one plugin. Timeout, invalid response or non-success status
  cannot claim health. Redirects, oversized bodies, arbitrary egress and browser-origin fallback are
  refused. The plugin publishes on every change of state and on a heartbeat regardless of change, so
  a silent plugin is a fact and not an inference. **Absent** (no report inside the freshness window)
  and **unhealthy** (a plugin said so about itself) are separate causes and are never merged. Every
  plugin reports — curated entries included — so there is no unmapped state and no plugin that is
  structurally unhealth-checkable. A publisher publishes its first health state **before** it
  announces: when an announcement reaches the registry, that plugin's health is already known. A
  healthy report does not attest that browser networking, `remoteEntry.js` or lazy assets work.
- **BR-AS62 — Backend targets are deployment-controlled readiness probes.** Deployment configuration
  maps plugin IDs to backend service IDs resolved to narrowly granted NATS request/reply subjects.
  Manifests cannot choose probe targets. Missing mapping is not configured; explicit empty list is
  frontend-only/not applicable. Keep per-service readiness results: any unavailable dependency makes
  the backend summary unavailable; otherwise any unknown/stale dependency makes it unknown/stale;
  all healthy makes it healthy. Presence alone is insufficient; malformed responses, timeouts and
  no responders cannot be reported healthy. No browser `rpc.>` or new broad registry grants.
- **BR-AS63 — Health transitions use deterministic thresholds.** Check each target every 5 seconds
  with a 2-second timeout; two consecutive failed checks make it unavailable, one success makes it
  healthy again. Frontend/backend/dependency counters are independent. Initial state is unknown until
  evidence exists; a first failure from unknown never claims healthy. After a previous success, one
  failure may retain healthy while within the freshness window. Checks do not overlap for a target
  and are cancelled/joined at shutdown.

  **Amended 2026-09-02 (decision 14): for frontend health, the plugin owns this, about itself.** The
  numbers are unchanged and the thresholds mean exactly what they meant — but the counter now lives
  in the plugin, which self-checks on its own clock and reports the resulting state. Backend
  readiness (BR-AS62) is unaffected and the registry still owns that loop. **The cost is stated
  rather than discovered:** one threshold that lived in one service now lives in every plugin image,
  so changing it is a fleet redeploy, not a config change. The self-check timeout must stay strictly
  below the heartbeat interval, which is what keeps "checks do not overlap" true once the clock is
  per-plugin.
- **BR-AS64 — Missing freshness becomes unknown/stale.** After 15 seconds without a fresh health
  observation, show unknown/stale with last-check time; before the first check, show unknown with
  no invented timestamp. Assess freshness per signal. Duplicate or old snapshots never refresh
  their check times. The central checker pushes a full timestamped snapshot after every completed
  probe pass, including stable states whose last-check time advanced. The shell reads once on start
  and each connection epoch, reconciles on a slow jittered interval, and ages/repaints locally
  without a five-second network poll. Loss of transport or reconnect cannot leave old health looking
  current and cannot remove content. Explicit not-configured/not-applicable states are not fabricated health.

  **Amended 2026-09-02 (decision 14): for frontend health this window stops being a backstop and
  becomes the detection mechanism.** With nobody probing, a plugin that has died is detected only by
  the absence of its heartbeat, so 15 seconds is now the answer to "how long until a dead plugin
  shows as dead", not merely a guard against a stale snapshot. Two consequences follow and are rules,
  not tuning: **the heartbeat interval must stay well below the freshness window** — 5s against 15s
  is three missed beats, and a heartbeat at or above the window makes every healthy plugin flicker —
  and **the two move together or not at all**. A registry that has never heard from a plugin shows
  unknown with no invented timestamp, which includes the window after a registry restart, because
  frontend health is held in memory and is repopulated by heartbeats rather than by asking.
- **BR-AS65 — Health observations are separate from catalogue state.** Share read-only health
  snapshots through dedicated request and push NATS subjects, with initial/catch-up/reconnect reads.
  The push and read use the same closed `{ok, asOf, plugins}` shape; Core NATS loss is repaired by a
  reconnect read and a 45–75 second jittered reconciliation. Health never changes catalogue revision, signed bytes, approval,
  curation audit, drift results or reload offers. Responses contain safe result codes/times, not
  configured addresses or credentials. Probe work cannot block catalogue reads/writes.

### Phase 5 rule-to-test matrix — implemented

Every row below names specs that exist and pass. Totals as built (2026-09-01): registry Ginkgo
**358/358, 0 Skipped**; lab-shell Vitest **480/480**; Admin Vitest **335/335**; `shared/natsready`
6/6. Phase 4, 7 and 8 coverage is unchanged and still green.
Use domain fake clocks and real adapter/broker integration where boundaries matter; do not mark a
rule covered by tests of a look-alike mechanism such as shell connection debounce or security reload.

| Rule | Required cases and intended test location | Phase |
| --- | --- | --- |
| BR-AS52 | **Done 2026-09-01.** `registry/lifecycle_test.go`: the two classes are the only ones a write may state, an unclassified row is read as static, and a migration backfills a legacy row without touching enablement, withholding, the signed bytes or the revision; `phase8_integration_test.go` amended so the unclassified case asserts the backfill. `manifestSchema.spec.js`: the admitted class is the registry's, an unstated or unrecognised one is admitted as static, and the source never implies it. `FrontendPluginsPanel.spec.js`: the class is shown per row, edited from the drawer, sent with the write, and the drawer says the change waits for a reload. `registryDiff.spec.js`: a class edit is a reload offer, and a backfilled `static` against an unstated held class is no change at all. | 5a |
| BR-AS53 | **Done 2026-09-01** in `registry/operator_withdrawal_test.go` (registry) and `registry/registryDiff.classSequences.spec.js` (shell): the class is a behaviour the REGISTRY implements, and the shell sees only the two documents it produces — a disabled static entry leaves the document, and an absence is a reload OFFER that takes nothing away, while a disabled dynamic entry is served as a withdrawal marker and applied. `Document.Readable` checks `Withdrawn` before `Enabled`, because an operator disable leaves the row `enabled = false` and `withdrawn = true` together and an Enabled check first would turn the marker back into plain absence. An entry no operator ever approved is never marked, so nothing is said about a plugin that was never running. Re-enabling clears the withdrawal and serves the whole entry, and the withdrawal survives a restart. Class-change sequences: a class edit under a running plugin is an ordinary reload offer; a class edit and a disable arriving in the same read are ONE piece of news, not two; BR-AS49 revocation still outranks both. | 5a/5b |
| BR-AS54 | **Backend done 2026-09-01** in `registry/unregister_test.go` (domain) and `registry/unregister_transport_test.go` (broker): the action, the plugin, the publisher and the signing key are all inside the signed bytes, so an announcement replayed here is refused as "not an unregister" and a signature cannot be lifted onto a request naming another key; ownership, key state and signature reuse Phase 7's gate unchanged, and that gate runs before existence is revealed, so a refusal cannot be used to enumerate registered ids; an older release is refused, the release the running announcement spent is refused as reused, and a duplicate delivery is a no-op at the same revision. The subject `rpc._platform.mfe-registry.entries.unregister.v1` is on neither browser list. A withdrawn entry is served to the shell as a marker, not by vanishing, because absence is not authoritative. Trust revoked between announce and unregister is refused at commit time and writes nothing; an acceptance audits under the publisher key and a refusal moves no revision. **15f, done 2026-09-02:** `registry/health_hint_test.go` distinguishes never heard (`unknown`, no timestamp) from a previously heard plugin that falls silent (`stale`/`absent`) across repeated passes, while the catalogue remains byte-for-byte unchanged; `registry/reset_service_test.go` proves a reset notice with no re-announce leaves the real Postgres entry, revision and audit history untouched; and `announcer` specs prove neither a dropped reset notice nor a lost health connection spends a release or self-withdraws. **Still to do in 5b:** the shell half — a service stop or missing data must never withdraw. Also open: the denial is argued from the subject lists, not yet proven against a real browser credential. | 5b |
| BR-AS55 | **Backend done 2026-09-01** in `registry/unregister_test.go` and `registry/unregister_store_test.go`: an accepted unregister sets a store-owned `withdrawn` beside `enabled` and leaves approval, row, contributions, signed manifest and history alone; it advances the release so a stale return cannot undo it; it is refused outright for an unknown id; a static or unclassified entry is ignored, because curation outranks a publisher; repeat withdrawal is safe. A same-origin return clears the withdrawal, an operator-disabled entry stays disabled and still withdrawn, and a cross-origin return goes back for approval without restoring availability. `withdrawn` and `release` are real Postgres columns: both survive a restart, and a SIGNED entry withdraws without its signed bytes being rewritten — reading either from the manifest would forget the withdrawal and let the stale announcement back in. An operator enable clears the withdrawal, disable does not set one, and a withdrawal on a stale revision is refused. | 5b |
| BR-AS56 | **Done 2026-09-01** in `contributions/contributionWithdrawal.spec.js`, `registry/pluginStatus.withdrawal.spec.js`, `loader/pluginLoader.withdrawal.spec.js` and the mounted `liveWithdrawal.spec.js`: a withdrawal takes away the plugin's routes, navigation and footer items and nothing else — siblings stay, including at a shared extension point; repeating it is safe, a marker for a plugin the shell never held is ignored, and a re-index of the same document cannot put it back. The plugin reads `withdrawn`, not `disabled`. An import or activation finishing late keeps its module and records `active` as the status it is owed, but never reaches the screen; starting a load while withdrawn is refused. **No claim is made about disposing plugin resources or cancelling its callbacks.** **The router half is 5c:** the route records are still registered, so a deep link into a withdrawn route still resolves. | 5b |
| BR-AS57 | **Done 2026-09-01** in `routing/withdrawnRoutes.spec.js` (a real `createMemoryHistory` router) and `views/PluginWithdrawnView.spec.js`: the occupant keeps their URL and is shown a shell-owned view in place, while a `beforeEach` guard returns `false` for anyone navigating in — the route RECORD stays registered, because removing it would turn the path into a not-found and an unchanged return would have to re-register a route with a navigation already in flight. Navigating away clears the exception; a return makes the guard pass again with no re-registration. The view shows curated metadata only, never the remote URL, and offers no retry. | 5c |
| BR-AS58 | **Done 2026-09-01** in `contributions/slotWithdrawal.spec.js`: withdrawing the owner of an extension point suspends the placements aimed at that point and leaves the contributors running everywhere else — a suspension is NOT a refusal, because the contributor is not at fault and the Plugins screen must not report a rejection against it. The placement is owed back exactly once: restoration re-places only the slots the returning plugin owns, and skips a contributor that is itself withdrawn. Host-owned slots are untouched. | 5c |
| BR-AS59 | **Shell half done 2026-09-01** in `registry/registryDiff.withdrawal.spec.js`, `contributions/contributionWithdrawal.spec.js`, `loader/pluginLoader.withdrawal.spec.js` and `liveWithdrawal.spec.js`: a return counts only when the validated definition is deep-equal to the one the shell is running — a moved remote or any edit is a reload offer instead, and equality is what licenses reusing the cached module, so `activate()` runs once in total across withdrawal and return. A never-loaded plugin returns to `available` and stays lazy. Restoration re-runs placement rather than replaying the old decision, so a contribution the session may no longer see stays absent and the plugin stays withdrawn; repeated events place nothing twice. A degraded document may withdraw but never restore. **Slot-owner restoration is 5c.** | 5b/5c |
| BR-AS60 | **Done 2026-09-01** in `registry/health_transport_test.go` (registry) and `healthText.spec.js`, `healthPlane.spec.js`, `PluginsView` / `App.vue` rendering (shell): the two signals are separate fields in the reply and two separate columns on screen, so a plugin whose UI is served while its API is down reads that way instead of as one merged verdict. A cause is one short lowercase word from a closed vocabulary — a structural spec asserts the reply's field set, and the browser refuses any cause outside `^[a-z][a-z0-9-]{0,31}$`, so a host, a port or a message cannot leave the process. The status is inline: no modal, contribution order and availability are untouched, and a healthy endpoint with a broken asset still shows the existing loading error, because health says the origin is serving and never that the code works. `unavailable` renders as a warning, not a failure — the plugin has not failed, something it depends on is not answering. In the navigation the load-status dot wins, and that precedence is now one function — `registry/navMark.js`, specced in `navMark.spec.js` — rather than the order of two template conditionals, because two marks in one corner compete rather than inform. | 5d |
| BR-AS61 | **Superseded 2026-09-02 by the Phase 15 rewrite above, re-specced in task 15b (see the Phase 15 matrix); this row is kept as the record of the HTTP-transport contract it replaces. `registry/internal/healthhttp/` and `REGISTRY_HEALTH_ORIGINS` no longer exist, and `not configured` is no longer reachable on the frontend plane — it survives only on the backend plane of BR-AS62.** Done 2026-09-01; host contract migrated 2026-09-02 in `registry/health_frontend_test.go`, `registry/internal/healthhttp/client.go`, and `shared/mfe-plugin-host/server_test.go`: the probe is a bounded `GET /healthz` against a DEPLOYMENT-mapped target (`REGISTRY_HEALTH_ORIGINS`), read separately from `REGISTRY_FETCH_ORIGINS` so an operator may watch availability without granting a manifest fetch. An unmapped or denied origin is `not configured`, never healthy. Non-success, malformed, oversized, redirected and timed-out answers all end in the closed cause vocabulary rather than an error. The Go plugin host preserves the nginx contract: no-store JSON, no CORS on `/healthz`, and health remains available with an empty asset root. | 5d/14b |
| BR-AS62 | **Done 2026-09-01** in `registry/health_backend_test.go`, `shared/natsready/natsready_test.go` (6 tests) and the bootstrap grant: a backend target is a deployment-configured service id (`REGISTRY_HEALTH_TARGETS`) and never anything from a manifest — a publisher naming its own probe target could point the registry at a service it does not own and read the answer back through the decoration. An absent plugin is `not configured`; a plugin mapped to an empty list is `not applicable`; both are configuration answers and never age. Mixed dependencies aggregate to the worst signal. Presence is not readiness: `natsready` runs the real check (`db.PingContext`) on every ask with a 2-second deadline and caches nothing, so a service holding its NATS connection open while its database is gone answers not-ready. The subject `rpc._platform.health.<service>.ready.v1` is in neither browser profile, and the registry's grant is one token wide, not `>`. | 5d |
| BR-AS63 | **Done 2026-09-01** in `registry/health_worker_test.go` (domain, fake clock) and `registry/health_hint_test.go`, which drives the application layer through the exported `HealthChecker.Step(ctx, now)`: every decision — due, timeout, first versus second failure, an intervening success resetting the count, one success recovering, initial `unknown`, independent counters per target — takes a `now` a spec supplies. `Run` owns the ticker and nothing else. Probes in a pass run concurrently and are joined before the pass ends, so a cancelled shutdown cannot leave one writing into a stopped worker. Numbers as built: interval 5s, timeout 2s, threshold 2 failures. | 5d |
| BR-AS64 | **Done 2026-09-01; push transport revised 2026-09-02** in `registry/health_hint_test.go`, `healthPlane.spec.js` and `healthText.spec.js`: ageing is evaluated at read time by `worker.Snapshot(now)` in Go and `signalsFor(id)` in the browser. A local five-second repaint exposes staleness without network traffic. `unknown` (never looked) and `stale` (true once) stay different words; configuration states never age. The checker broadcasts the full served snapshot after every completed probe pass, including stable observations whose `lastCheckAt` advanced. The browser installs only a strictly newer millisecond `asOf`, so duplicate/out-of-order pushes cannot renew freshness. A failed reconciliation keeps the last reading and lets it age; a throwing screen subscriber cannot fail the health plane. | 5d |
| BR-AS65 | **Done 2026-09-01; push transport revised 2026-09-02** in `registry/health_transport_test.go`, `registry/health_hint_test.go`, `healthPlane.spec.js` and `browserrpc/adapter_test.go`: health uses its own request (`api._platform.mfe-registry.frontend-plugins.health.v1`) and push (`notify._platform.mfe-registry.frontend-plugins.health`) subjects with one `{ok, asOf, plugins}` snapshot shape. The shape carries no catalogue revision, entries or signed bytes, and the checker holds only a read-only `Curated` interface. An unwired checker answers with an empty snapshot and `ok`; reads touch memory and never wait on probes. The shell reads on startup and every connection epoch, accepts central pushes during the session, and makes only a 45–75 second jittered reconciliation request. Payload-free legacy notifications still trigger a coalesced read during rolling restarts. | 5d |

**Completion evidence (2026-09-01):** registry Ginkgo 358/358 with 0 Skipped against real Postgres
and an in-process broker; `shared/natsready` 6/6; lab-shell Vitest 480/480 plus a clean `npm run
build`; Admin Vitest 335/335; `docker compose config -q` clean. Phase 4, 7 and 8 coverage is
unchanged.

**Live verification (2026-09-01, 1920×1080, full Docker stack).** All five plugin origins served
`/healthz`; the readiness responder answered `{"ready":true}`, then `{"ready":false,"cause":
"not-ready"}` with refdata Postgres stopped, then true again — exposing no host, port, DSN or query
text. On screen: separate Frontend and Backend columns, `demo-catalog` moving healthy →
`unavailable (not-ready)` → healthy, `not applicable` for frontend-only plugins, `not configured`
for the unmapped one, and a warning dot on the Demos nav item that cleared on recovery. The
`/demos` route kept loading throughout — health decorates, it never removes content. NATS logged
zero permissions violations.

**Three real defects the live run found, all fixed the same day.** They are recorded because each
is a class of thing no unit spec could have caught:

1. `refdata-service/Dockerfile` copied neither `shared/mferegistry` nor `shared/natsready`, so the
   official `docker compose up --build` failed outright. A new shared module is a new COPY line.
2. `bootstrap-operator.sh` declared the health grants but the checked-in `.creds`/JWT artifacts
   predated them, and the script short-circuits on an existing `operator.jwt` — so the running
   stack logged `Publish Violation` on every probe. Exactly the failure BR-AC34 already warned
   about: **a grant change is only live after `./bootstrap-operator.sh --force`.** The same
   regeneration exposed a second gap — `rpc._platform.mfe-registry.entries.unregister.v1` was never in
   the registry's `--allow-sub` at all, so Phase 5b's withdrawal transport could not have worked in
   Docker. Named in full rather than swept up by a prefix.
3. The shell's five-second health timer only copied memory into the reactive object; nothing
   re-read the network. The plane reads on start, on a hint and after a reconnect, so a first read
   that lost the race with the connection coming up left every signal `unknown` forever with
   nothing left to wake it. The timer now refreshes and then copies — the read is what makes it a
   floor cadence, the copy is what lets a kept reading age into `stale` on schedule.
   `healthPlane.spec.js` gained a spec for the recovery.


## Phase 7 — publisher signing and the trust table (BR-AS35 to BR-AS38, BR-AS46 to BR-AS51)

Phase 7 answers one question: **on what authority does a plugin the operator never
typed in get into the catalogue?** The answer is a publisher — a named holder of
keys that owns a list of plugin ids. Everything below follows from that, including
the parts that say what signing does *not* buy.

- **BR-AS35 — An announced entry must carry a valid publisher signature.** *Rewritten
  2026-08-31 (decision 102): the rule used to key off `lifecycle`, which an operator may
  edit.* An announcement is refused, and never stored as pending, when it carries no
  signature, an invalid one, one from a key that is not trusted and enabled, or one over
  a plugin id the signer does not own. An entry whose `source` is `curated` or `preload`
  may be unsigned. Changing an entry's `lifecycle` never changes what this rule requires
  of it.
- **BR-AS36 — Signature verification happens in the service, never in the browser.** The
  document the shell reads carries the verification outcome. The shell does not re-verify
  and is never asked to hold a trust anchor.
- **BR-AS37 — The signed manifest is stored as signed.** The bytes that were verified are
  the bytes that are stored and re-served. Any representation the service derives for
  query or display is a projection, and is never the artifact a signature is checked
  against.
- **BR-AS38 — Publisher trust is curated, audited state, and a publisher outlives its
  keys.** *Rewritten 2026-08-31 (decisions 103, 104).* A publisher has a stable id and
  holds a list of keys and a list of owned plugin ids. Adding, retiring or revoking a key,
  and every transfer of a plugin id between publishers, is a revision-bearing, audited
  write with an actor. **Retiring a key is not revoking it**: entries signed by a retired
  key remain valid; entries signed by a revoked key are re-evaluated and withheld.
  Re-enabling a revoked key restores nothing on its own — each withheld entry is restored
  individually by an operator, as its own audited write. Revocation is bulk and automatic;
  restoration is one entry at a time and manual.
- **BR-AS46 — Ownership authorises; a signature alone does not.** A publisher may announce
  only the plugin ids its row owns. An announcement for any other id is refused with its
  own cause, whatever the signature proves and whatever origin the remote names.
- **BR-AS47 — An announcement carries a release number and never goes backwards.** The
  signed bytes include the plugin id and a release counter. The registry refuses a release
  lower than the highest it has accepted for that id, and treats an equal one as a no-op so
  a retry is safe. Returning to an earlier release is an operator act, never an effect of a
  received message.
- **BR-AS48 — Trust is re-checked where the write commits.** The publisher's key state is
  read again inside the transaction that checks the revision. A key revoked after
  verification and before commit causes the write to fail.
- **BR-AS49 — Revocation reaches the running browser.** A revoked entry is withheld from
  the readable document *and* the shell is told to reload, overriding the rule that a
  `static` plugin is not unloaded under the user. **What this rule promises is that the
  plugin stops at the next paint — and nothing more.** It does not promise that an
  in-flight callback is interrupted, that a timer already scheduled will not fire, or that
  anything the plugin wrote to shared state is undone. It is not runtime isolation and must
  never be described as such: a plugin's code runs in the shell's own page, and the whole
  of the containment is that the next page load will not include it.
- **BR-AS50 — The signed bytes survive every hop unchanged.** Storage, the KV cache and the
  wire all carry the verified bytes opaquely. Nothing re-serialises them. Any edit to signed
  content invalidates its attestation; the entry is re-signed, or it falls back to operator
  curation.
- **BR-AS51 — A degraded read is stale, never regressive, and always says so.** A cached
  document may be served while Postgres is unavailable — refusing would turn a rare security
  event into a routine availability failure. A lower revision never overwrites a higher one
  in the cache, **and** the served document carries its revision and age, so the reader shows
  `degraded, as of revision N` rather than presenting stale trust as current. Monotonic
  writes alone were judged insufficient: the staleness has to be visible, not merely bounded.

### What signing does not cover (decision 66)

Stated plainly so nobody reads a signature for more than it is:

- **A signature proves who published a manifest. It says nothing about what the code does.**
  A trusted publisher shipping hostile code is trusted hostile code.
- **The signature covers the manifest, not the remote it points at.** The bytes served from
  the remote URL are not signed and are not checked; a compromised CDN serves whatever it
  likes under a valid attestation. The BR-AS20 origin allowlist, not the signature, is what
  bounds where code may come from.
- **What is signed is the manifest, its plugin id, and its release number — not the
  announcing service's identity** (gate question 1). Ownership (BR-AS46) already stops a key
  speaking for another plugin and the release number (BR-AS47) already stops replay, so
  binding the announcer would add nothing, while ruling out ever handing a signed manifest to
  an operator to place by hand.
- **The publisher keypair is not the NATS trust chain** (gate question 2). It is separate on
  purpose: plugin publishing does not widen the account chain, and a leaked publisher key
  cannot connect to NATS as anything.
- **Withheld is not disabled** (gate question 3). `disabled` means "not reviewed yet";
  `withheld` means "we withdrew this". They are separate state, shown under separate words —
  the Admin says `revoked`, and reserves `withheld` for a non-conforming origin — because an
  operator must never have to guess which of the two they are looking at.

### How Phase 7's rules are checked

| Rule | Where |
|---|---|
| BR-AS35 — a valid publisher signature | `verify_test.go` and `announce_transport_test.go`: no signature, a bad signature, a key that is not enabled, and a foreign id are each refused with their own cause and store no pending row |
| BR-AS36 — verification is server-side | `browserrpc/endpoints.go` serves the outcome only; the shell holds no key material — `manifestSchema.spec.js` has no signature field to validate |
| BR-AS37 / BR-AS50 — the bytes survive | `manifest_test.go`: the verified bytes round-trip through Postgres, the KV cache and the wire without re-serialisation, and an edited manifest fails verification |
| BR-AS38 — trust is curated and audited | `publishers_test.go` (key add/retire/revoke, id transfer, actor and revision on every write) and `revocation_test.go` (retire leaves entries valid; revoke withholds in bulk; re-enabling a key restores nothing until each entry is enabled on its own) |
| BR-AS46 — ownership authorises | `verify_test.go` and `announce_transport_test.go`: a valid signature over an unowned id is refused, with a cause distinct from a signature failure |
| BR-AS47 — releases never go backwards | `verify_test.go` and `announce_transport_test.go`: a lower release is refused, an equal one is a no-op, and a retry is safe |
| BR-AS48 — re-checked at commit | `announce_transport_test.go`: a key revoked between verification and commit fails the write, and the revision does not move |
| BR-AS49 — revocation reaches the browser | Go: `revocation_test.go` and `degraded_test.go` pin the tombstone (`{id, withheld: true}`, no remote, no manifest) and its ordering ahead of the `enabled` and allowlist filters. Browser: `registryDiff.spec.js` (a tombstone for a running plugin is a `forced` reload; for one never held, nothing), `bootShell.spec.js` (tombstones are taken even from a degraded read, and never retract a standing offer), `RegistrySignalBanner.spec.js` (a forced banner reloads on its own and offers nothing to dismiss) |
| BR-AS51 — degraded, never regressive, labelled | `degraded_test.go`: `domain.SupersedesCached` directly, plus a real `kvcache` on an embedded NATS server refusing a lower revision; the served document carries `AsOf`. `RegistrySignalBanner.spec.js` pins the `degraded, as of revision N` label. The Admin has no such label by design — its curated read goes straight to Postgres with no cache, so it cannot serve a stale copy |
| decision 100 — what revocation does not promise | Nothing asserts an in-flight callback is stopped, because nothing does. The specs are worded as "reloaded away", never as "isolated" |


## Phase 8 — preload and announcement

- **BR-AS39 — An announcement never activates.** A verified announcement for an unknown id is stored
  as `announced` and served to no shell until an operator enables it. No transport, payload or
  signature makes an announcement self-activating.
- **BR-AS40 — An enabled dynamic id re-announcing is followed within its origin.** For a
  `lifecycle: dynamic` entry only, a remote change that stays
  within the entry's allowlisted origin is applied without review; one that changes origin returns the
  entry to `announced` and withholds it until an operator re-enables it. **Read with BR-AS46:**
  "origin unchanged" is not authority. An update whose origin never moved is still refused when the
  signer does not own the id — ownership is answered before the origin question is asked, so this
  rule only ever relaxes review for a publisher already entitled to speak for that plugin.
- **BR-AS41 — Preload never reverts curation.** A preloaded entry is written only for an id with no
  existing row. An id the operator has edited, disabled or removed is never re-created or overwritten
  by a service restart.
- **BR-AS42 — Every write names its true actor.** `admin` for a curation, `preload` for a seeded
  insert, the publisher key for an announcement. The audit trail answers "who put this here" without
  reference to any other source.
- **BR-AS43 — A manifest never carries its own trust tier.** A plugin's manifest states what the
  plugin is, never how far it is trusted or by which path it arrived. A `source`, `lifecycle`,
  `enabled` or `revision` field in an announced or preloaded manifest is refused, not ignored: a
  silently dropped claim is one a publisher believes was honoured.
- **BR-AS44 — The shell renders without a registry.** An unreachable, malformed or degraded registry
  read leaves the shell running its **native frame** — `HomeView` and `PluginsView`, which are shell
  views rather than plugins — with the reason visible on the Plugins screen. Reworded 2026-08-31: the
  original said "its built-ins", which decision 84's retirement leaves empty. No registration tier
  may reduce the native frame or prevent the Plugins screen stating why the plugin list is empty.
- **BR-AS45 — Registry outbound HTTP is explicitly bounded.** The registry
  service may fetch a served `manifest.json` only for an entry whose remote origin is already on the
  BR-AS20 allowlist. The fetch is read-only, time-bounded, and never on a request path the shell or an
  operator waits on. Drift is displayed only: the curated copy still wins (decision 77) and a drifting
  entry is never withheld. This is the one outbound HTTP capability currently implemented; the
  approved Phase 5 extension below is the only additional permission, and anything wider needs a
  new gate. (It already egresses to Postgres and NATS — the claim is about HTTP, corrected 2026-08-31.)
  **The allowlist is not the fetch address.** `REGISTRY_ALLOWED_ORIGINS` holds *browser* origins
  (`http://localhost:7111`); `mfe-registry-service` is a container, for which `localhost:7111` is
  itself. A second start-time config maps each allowlisted origin to the URL the *service* reaches it
  on (`http://example-plugin-frontend:8080` — the service is already on the `frontend` network). An
  origin with no mapping is simply not checked. The map may never introduce an origin the allowlist
  does not already carry: it translates addresses, it never widens the envelope.
  **The check has three outcomes, not two:** `checked` (agrees), `drift` (differs, fields named), and
  `not checked` (unmapped, timed out, non-200, or unparsable). A failed fetch must never render as
  "no drift".

  **Phase 5 extension, built 2026-09-01 — withdrawn 2026-09-02 by Phase 15.** The gate had permitted
  one additional HTTP operation: background frontend `GET /healthz` probes reusing the allowlist and
  an explicit service-origin mapping. BR-AS61's rewrite moves that ask onto NATS and the `GET` into
  the plugin's own process, so the registry makes no outbound health request at all and the mapping
  it needed is deleted. **Manifest drift is once again the registry's only outbound HTTP
  capability**, and this rule's envelope narrows accordingly rather than merely going unused. Health
  still observes both lifecycle classes regardless of source, and still never curates an entry.


The operator's preload wrapper may carry `enabled` (decision 79); a plugin's
manifest may not. Preload writes `static`; announcements write `dynamic` by
default. Legacy empty lifecycle values remain unclassified today; approved Phase 5 BR-AS52
backfills them as static. Neither manifest
may choose lifecycle or provenance.

### Phase 8d/8f runtime contract (decisions 87–95)

`builtin` is retired. Every plugin has its own package, lockfile, Dockerfile,
origin and served `manifest.json`; each exposes one `plugin` module. The healthy
example retains six views, while each failure variant has one Overview view.
The unreachable fixture has no service: its allowlisted origin returns a real
404 at `http://localhost:7111/no-such-remoteEntry.js`.

BR-AS08's activation receives exactly one shared frozen object:
`{ version: 1, ui: { ExtensionRegion } }`. The `ui` container is frozen too,
but the Vue component definition is not. The API contains neither credentials
nor a NATS connection nor host registries. Ignoring the argument remains valid
v1 behavior. This is a public API boundary, not a security sandbox: federated
code still executes in the browser's realm and requires server-enforced grants.

Remote builds include their stylesheet assets in the federation expose loader
(`bundleAllCSS`). The host and catalog share `@primeuix/styled` as a singleton,
so PrimeVue widgets use the host-configured UniFi theme rather than an empty
remote theme store. Browser verification checks the existing 320px sidebar and
styled controls at 1920×1080.

The catalog retains the API in plugin module scope. Its local wrapper forwards
attributes and slots to `ui.ExtensionRegion`, including from nested views.
It imports no host runtime module or host version constants. The raw demo README
is compiled into the catalog only; native Home has no hardcoded catalog link.

`pluginLoader.spec.js` checks API shape, identity, freezing and compatibility;
`catalogRuntime.spec.js` checks real routes and nested contributions;
`pluginServices.spec.js` checks independent packages, fixture statuses and
single-service failure isolation. The bundle fingerprint rejects plugin
identities, origins and README content from host assets.

### How Phase 8's rules are checked

| Rule | Checked by |
| --- | --- |
| BR-AS39 — announcement never activates | `registry/announce_test.go` and `registry/announce_transport_test.go`: pending rows persist, and the browser read withholds them until operator enablement |
| BR-AS40 — dynamic updates remain in-origin | `registry/announce_test.go` and `registry/announce_transport_test.go`: in-origin updates and cross-origin requeue; static always wins |
| BR-AS41 — preload never reverts curation | `registry/preload_test.go` and `registry/phase8_integration_test.go`: real Postgres restarts preserve edits, disabled/removed rows, and revision; duplicate ids seed once |
| BR-AS42 — true actor | Domain write-builder specs plus `phase8_integration_test.go` and `announce_transport_test.go`: `preload`/publisher actors, and ignored observations append an audit row without a registry write or notify |
| BR-AS43 — no self-asserted trust tier | `preload_test.go` refuses manifest fields and file revisions; `announce_transport_test.go` exercises each forbidden field through NATS |
| BR-AS44 — native frame survives registry failure | `catalogRuntime.spec.js` mounts the actual App/Home/Plugins for unreachable, malformed and degraded reads, asserts the reason, no catalog link and zero plugin loads |
| BR-AS45 — bounded drift HTTP | `registry/drift_test.go`: failed/invalid responses never agree, one retry, timeout, poll recovery, nonblocking snapshots, redirects refused, body capped; `registry/drift_integration_test.go`: composed NATS reads return during a hanging fetch |
| BR-AS20 / BR-AS45 — mapping translates existing grants only | `registry/drift_test.go`: unmapped origins are not checked, unallowlisted mappings are ignored with a warning, invalid config fails closed, only the mapped `/manifest.json` is fetched |
| Decisions 77/85 — drift is display only | `registry/drift_test.go`: named content fields, platform state excluded, stale observations invalidated; `registry/drift_integration_test.go`: curated entry, revision, audit and notifications unchanged; `FrontendPluginsPanel.spec.js`: separate Manifest column, no automatic write, refresh and edit exclude diagnostics |
| BR-AS04 — drift refusals name stage and cause only | `registry/drift_test.go`: transport/parser errors become fixed codes, no remote values in differences; `FrontendPluginsPanel.spec.js`: `manifest-drift` stage and failure cause remain distinct from serving state |

Phase 8c checks entries whose creating audit actor identifies **preload**; a later
operator edit does not change that source. `REGISTRY_FETCH_ORIGINS` is an optional
JSON object of browser origin to service-reachable HTTP(S) origin, for example
`{"http://localhost:7111":"http://example-plugin-frontend:8080"}`. Addresses are
origins only (no credentials, non-root paths, query or fragment); the checker
appends `/manifest.json`. Compose maps the five existing browser origins without
changing `REGISTRY_ALLOWED_ORIGINS`. Missing mappings remain `not checked`.

The checker starts after composition, makes one serial pass, then waits one minute
before the next. Each GET has a two-second deadline and a 1 MiB response limit;
failure gets one retry after 200 ms. Redirects are not followed, and no ambient
HTTP proxy is used. Failure immediately replaces any earlier agreement with
`not checked`, including during retry backoff. A valid response yields `checked`
or `drift` with differing top-level manifest fields; platform-owned state is
excluded. Unknown fields, including nested ones, render as `not checked` rather
than being silently dropped into agreement. The service never applies the fetched content.

Observations live in memory and carry the last attempt time. They start afresh on
restart, and a changed curated entry cannot reuse an observation of its old copy.
Admin reads the last result through `EntryView`; **Refresh observations** only
repeats that registry read, never starts HTTP. There is no drift field on the
stored entry, shell document, revision, audit trail, KV cache or notification.

`REGISTRY_PRELOAD_FILE` is optional; Compose mounts `demos/01-dictionary/registry.json`.
Whole-file parse/read failures fail boot. Entry refusals log `withheld`, id and
cause, while other entries continue. The result is retained on the module for
the 8c pending/withheld UI (built) reads it from there, not from this module.

The dedicated announcement request is `{ "payload": <manifest object>,
"signature": "<opaque signature>" }`. Verification receives the exact payload
bytes before parsing. Production uses `domain.NKeyVerifier{}` (Phase 7): the trust anchor is the
operator's publisher table, so an empty table still refuses every announcement — the same
fail-closed behaviour the earlier `NoVerifier` placeholder gave, now reached by policy. No bypass
setting exists. Successful
observations retain `announcedAt`/`lastAnnouncedAt`; no TTL is installed. Ignored
static announcements record their time and cause in the audit only.

## Phase 13 — announced plugins, publisher trust and release counters

Confirmed by the user 2026-09-01 at the Phase 13 design gate
(`.claude/plans/Application-Shell-Microfrontend-Plan.md`). **BR-AS66, BR-AS67
and BR-AS68 are new unique IDs.** Each requires executable coverage during
Phase 13 implementation; none is complete until it has one. All three now have
theirs (13b, 13d and 13e). The two clarifications
below add no new ID — they record a decision *not* to grow an existing rule.

- **BR-AS66 — A fresh lab serves only its preloaded plugin.** On first boot
  against an empty registry database, `demo-catalog` is the only plugin served
  to the shell. Every announced candidate is stored disabled and stays disabled
  until an operator enables it. This is BR-AS39 seen from the deployment's side
  and it is a first-boot property only: once an operator has enabled an entry,
  Postgres keeps that decision across restarts, and an equal-release
  re-announcement is a no-op that must not disable it again. **The lab-shell
  intro copy must state this**, or a correct first run reads as a broken one.
  - **Test:** `lab-shell/src/shell/registry/preloadFixture.spec.js` — the
    preload document curates `demo-catalog` and nothing else, and no announced
    id leaks back into it. The check is on the fixture rather than on a booted
    stack because the failure it guards is an editing failure: BR-AS39 already
    has the runtime half (an unknown id lands pending, never enabled), and
    nothing else notices when a plugin is quietly re-added to the file that
    bypasses it. Same file also holds 13e's wiring checks — every announced
    plugin has exactly one publisher process and a release volume outside the
    container. Four publishers run in their frontend's Go host and only the
    unreachable fixture keeps the CLI announcer; mounting another plugin's seed
    or manifest is silent until a signature fails. Same file also pins Compose's
    `com.nats-tech-lab.mfe.source` label per frontend — documentation read by
    no code (decision 80), which is why it drifted to `preload` on all five
    until 13g.
  - **Test (the intro copy half, 13g):** `lab-shell/src/shell/ui/
    FirstBootNote.spec.js` — the shell-owned note says the rule, names the
    preloaded plugin and the announced fixtures separately, says `disabled`
    means awaiting an operator rather than failed, and says the enable survives
    restarts. It also asserts the note is rendered by both screens a first run
    lands on (`HomeView.vue`'s empty region and `PluginsView.vue`), because a
    component nothing renders is copy that does not exist.
- **BR-AS67 — A publisher owns its release counter, and it only goes up.** Each
  plugin has one release sequence, shared by announce and unregister, held
  inside the signed bytes (BR-AS47) and persisted by the *publisher*, never
  minted by the registry and never derived from a clock. An announcement at the
  accepted release is a no-op; an unregister at the accepted release is refused
  as reused unless the entry is already withdrawn. So a withdraw-then-return
  spends three numbers, not two: `N` announce, `N+1` unregister, `N+2`
  re-announce. A publisher that loses its local counter must recover it before
  announcing again, because re-announcing a spent release leaves the plugin
  withdrawn with no error. The registry side is already covered by
  `registry/announce_test.go` and `registry/unregister_test.go`. **Publisher
  side built 2026-09-01 (13b), extracted 2026-09-02 (14a)** in
  `shared/mferegistry/announcer/` — `release.go` holds the counter in an
  atomically written state file, and `announcer_test.go` pins
  `N`/`N+1`/`N+2`, a crash restart retrying `N`, and explicit recovery after
  state loss. **The release is injected into the manifest at runtime, just
  before signing**, not baked into the build artifact: the counter is the
  publisher's state, so an image rebuild must not be needed to withdraw and
  return. BR-AS37 still holds — the re-encode happens before signing, never
  between signing and publishing.
  The named counter volume now attaches to the plugin container that owns the
  publisher lifecycle; the CLI-only unreachable fixture attaches the same
  volume to its announcer container. The state format is identical because
  both process entry points call `announcer.Start`.
  **Amended 2026-09-02 (15e) — a resync spends a release number even when it
  changes nothing.** An announcement whose content equals the stored entry is
  *converged*: no catalogue revision, no audit row, no change notification and
  no cache refresh, but the accepted release still advances to the number the
  announcement spent. The advance is not bookkeeping — `Accepted` is the
  watermark `ErrReleaseBackwards` and `ErrReleaseReused` both read, so leaving
  it behind would widen the replay window by one on every resync. This is
  deliberately **not** the same fact as the existing no-op (`Release ==
  Accepted`, a literal replay that spends no number); the wire carries them as
  `converged` and `NoOp`, and they must not be collapsed into one flag.

- **BR-AS68 — Trust seeding converges, and never reverses an operator
  decision.** The boot-time seeder (`cmd/seed-publishers`, 13d) reads the
  publishers document first and applies only the operations actually missing
  from it: it creates a publisher row only when absent, attaches a signing key
  only when no publisher holds it, and claims a plugin id only when nobody owns
  it. Three differences are deliberately left alone and reported rather than
  written: a key present in **any** state keeps that state, so a `revoked` key
  is never handed its trust back and a `retired` one is never un-retired; a
  plugin owned by a different publisher stays owned by them, because that is an
  operator transfer; a key held by a different publisher is named, not
  re-claimed. Re-running the seeder against a converged registry performs
  **zero** writes — not a cosmetic property, since every publisher op is
  revision-checked and spends a revision plus an audit row even when it changes
  nothing (decision 6). The seeder writes through
  `api._platform.mfe-registry.publishers.write.v1` under the shared operator
  identity, the same endpoint the Registry Publishers panel uses: seeded rows
  are curated writes, not a new tier and not a boot-time bypass of the revision
  check or the audit trail (decision 7, decision 75). A refused write is a
  decision, never a timing accident, so it is surfaced and the one-shot exits
  non-zero; only the *read* is retried, on a bounded and logged schedule, so a
  registry still running its migrations does not become a failed seed.
  Compose orders the seed strictly before anything that announces
  (`registry-publisher-seed`, `service_completed_successfully`) — without that
  the announcers race it and exit on `not-owned`.
- **BR-AS69 — The registry refuses a structurally unusable entry.** Added
  2026-09-02. An upsert — operator curation or a verified publisher's
  announcement — is refused when the entry could not be placed by any shell:
  an id or route prefix that is not kebab-case, a contribution of a kind
  outside the closed five (BR-AS02), a contribution id that is not kebab-case
  or is declared twice, a route outside the plugin's own prefix segment
  (BR-AS12), a route with no title, a navigation entry that names a path
  instead of a local route id, an extension or shell-control whose target or
  region is not `{owner}/{region}/v{major}`, an extension point owned by
  another plugin, or an entry that contributes nothing at all. A refusal is an
  ordinary refused write: nothing is stored, the revision does not move, and
  the audit row is written (BR-AS23).

  **The split with the shell's own validator is the rule, not an
  accident.** The registry owns *structure*: naming and shape, which never
  vary by reader, and which only the registry sees across the whole curated
  set. The shell owns *compatibility*: `schemaVersion` and `shellApiVersion`
  are checked in the browser and nowhere else, because one registry serves
  shells of several vintages across an upgrade and refusing on a version the
  current shell does not know would refuse an entry that is good for the shell
  it was published for. So the shell may always reject more than the registry
  refuses, never less. A rule tightening on the shell side needs no mirror
  here, which is what stops the two halves becoming two definitions of one
  contract. Before this rule the registry stored entries every shell rejected
  as `incompatible`, and the operator who curated one learned nothing until
  someone opened the Plugins screen.
- **BR-AS70 — Publisher-asserted and platform-owned are one named split.**
  Added 2026-09-02. `enabled`, `lifecycle`, `withheld`, `withdrawn`,
  `announcedAt` and `lastAnnouncedAt` are the platform's; everything else in an
  entry is the publisher's. `release` is deliberately the publisher's — it is
  inside the signed bytes, so it cannot be moved without the key (BR-AS47). The
  set is named once, in `domain.CuratedFields()`, and the three paths that
  cross the split ask it: a payload asserting one of these is **refused by
  name** rather than ignored (a publisher who is ignored believes they were
  heard); an announcement clears the whole set whatever the caller assembled;
  and attestation compares the publisher's half only, so a platform decision
  never invalidates a signature.

  **Why one definition.** Each path used to carry its own list, and they had
  drifted: the parser refused four names, the announcement zeroed three fields,
  the attestation comparison zeroed four, and the struct's comments named two
  more. Two live consequences followed. A signed payload could assert
  `"withheld": true`, which reaches every shell running that plugin as a
  tombstone and forces a reload — a publisher choosing a platform action. And a
  withdrawn signed entry reported itself un-attested, because the comparison
  still counted a field the platform had set. A spec pins every field of
  `Entry` to one side or the other, so a field added without a decision fails a
  test rather than defaulting to publisher-asserted by silence.
  - **Test:** `cmd/seed-publishers/main_test.go` — pins the exact op sequence
    for an empty registry and its ordering independence from the file's own
    order; a second run planning zero writes; a `revoked` key left revoked and
    a `retired` key left retired; an ownership transfer not reversed; a key
    held by another publisher not stolen; a partially seeded registry filling
    only its gaps; the revision threaded through each write with no
    client-supplied actor on the wire; a stale-revision refusal surfacing
    instead of being retried; and the bounded read retry giving up rather than
    waiting forever.

### Evidence — the publisher lifecycle end to end (13f)

The rules above are each covered by their own specs. What no spec could show is
that the deployed pieces meet: a bootstrap-minted credential, a seeded trust
row, a publisher process holding its seed on a read-only mount, and a Compose
`SIGTERM`.
`backend/mfe-registry-service/cmd/registry-acceptance` drives the running lab
through the whole lifecycle and asserts at each step. A command, not a spec: an
env-gated spec that skips still prints `ok`, and this one either walks the
sequence or exits non-zero. Run it from `demos/01-dictionary`:

```
go run ./backend/mfe-registry-service/cmd/registry-acceptance
```

It exercises BR-AS38, BR-AS42, BR-AS47, BR-AS54, BR-AS55 and BR-AS67 together
on one plugin, with the other four announced plugins as a control group it
proves never moved. Three statements it pins that are easy to read the wrong
way round:

- **A withdrawal and a revocation are not the same shape of event.** An
  accepted unregister leaves the operator's approval alone (BR-AS55). A key
  revocation clears it, in the same transaction that withholds the entry
  (`enabled = false, withheld = true`) — because a withdrawal is the publisher
  saying "not available" and a revocation is the platform saying "not this
  code".
- **A publisher cannot lift a withholding, however good its signature.**
  Re-enabling the revoked key restores nothing, and neither does a fresh, valid
  announcement: the entry comes back for review, still withheld. Only an
  operator enabling *that entry* clears it, because only that is somebody
  looking at the entry rather than at the key (BR-AS38).
- **Neither `pending` nor `requeued` clears `withdrawn`.** A cross-origin move
  does not put a withdrawn plugin back on offer, and a return is not an
  approval. One operator decision answers approval and availability together
  (BR-AS55).

And the one rule the sequence exists to keep honest: the withdrawal it drives
is a `docker compose stop` — a `SIGTERM`, so the plugin host (or the
unreachable fixture's CLI announcer) sends an explicit
signed unregister before exiting. **A crash, a failed health check, or a
container disappearing withdraws nothing** (BR-AS54). Withdrawal is an action a
publisher takes; it is never inferred from silence.

### Clarification — a seeded publisher key has no state of its own (BR-AS38)

`KeyStates()` stays three: `enabled`, `retired`, `revoked`
(`registry/internal/domain/publisher.go:27`). A key written by the Phase 13
bootstrap seeder is an ordinary `enabled` key. **No fourth "seeded" state.**
Who created a key is read out of the audit trail, the same way an entry's
`source` is (BR-AS42, decision 80) — provenance is derived, never stored, and a
fourth state would add a case to every gate, badge and spec to record something
already recorded.

### Clarification — key revocation is a documented demo step (BR-AS38)

Revoking one fixture's signing key is a first-class thing to demonstrate, not
an edge case: it withholds exactly the plugins that key signed and leaves every
other publisher untouched. The recovery path is **not** `docker compose down
-v`. As `registry/revocation_test.go` already proves, re-enabling the key
restores nothing on its own — each withheld entry must be enabled again
individually. That asymmetry is the point of the demo and the intro copy must
not hide it.

## Deepening refactor — 2026-09-02

An `/improve-codebase-architecture` pass over the shell found five shallow modules; all five were
deepened, with tests, in one task. The report — problems, before/after diagrams and the reasoning —
is kept at
[`.claude/plans/reviews/architecture-review-20260902.html`](../../.claude/plans/reviews/architecture-review-20260902.html).
No rule changed. What changed is where each rule is enforced, and therefore where it is tested:

| Rules | Moved to | Specced in |
| --- | --- | --- |
| BR-AS28, BR-AS29 and the recovery-read half of BR-AS64/BR-AS65 — one read at a time, a catalogue/legacy hint is a reason to read, a reconnect is a gap | `shell/registry/hintedReader.js` — the coalescing machine the catalogue plane uses for normal notifications and the health plane retains for startup/reconnect/reconciliation/legacy reads; current health pushes install directly after `asOf` validation | `hintedReader.spec.js` (the machine on its own), with `changeSubscription.spec.js` and `healthPlane.spec.js` covering their respective policies |
| BR-AS02, BR-AS04, BR-AS06, BR-AS07, BR-AS58 — placement, refusal, capacity, suspension | `shell/contributions/placementPolicy.js` — `decidePlacements` returns a plan; `contributionRegistry.index()` only applies it. A return now re-decides through an explicit `reindex` rather than deleting its own bookkeeping to get past its guard | `placementPolicy.spec.js` — plain values, no reactivity, no registry |
| BR-AS56, BR-AS59 — work that settles after a withdrawal | `PluginStatusRecord.settleWhileWithdrawn()`; the loader no longer writes `restoreTo` directly, and only `active` or `failed` is accepted | `pluginStatus.spec.js` |
| BR-AS60 — one nav item, one mark | `shell/registry/navMark.js`; the template renders one conditional dot | `navMark.spec.js` |
| BR-AS43, decision 80 — source badge and drift on the operator view | `registry/internal/application/curated.go` — the join left the NATS adapter, which now holds JSON tags only | `registry/internal/application/curated_test.go` |

---

## Phase 14 — one container per plugin, and where a plugin's origin comes from

- **BR-AS71 — A plugin's public origin is stamped at announce time, never built
  into its image.** Added 2026-09-02. A plugin image carries its code and its
  manifest, and never the origin it will be served from. The publisher reads
  its own public origin from deployment configuration and writes it into the
  manifest immediately before signing — the same point, and for the same
  reason, as the release counter (BR-AS67). A manifest in a plugin's source
  tree therefore carries no origin in `remote.url`, and one image is servable
  from any deployment.

  **Why the ordering is forced.** BR-AS15 says a plugin is built by its own
  toolchain, from its own lockfile, into its own image. An origin frozen at
  build time contradicts that: it ties one image to one deployment, and a
  plugin built on a laptop announces `http://localhost:7111` to production —
  which is what the five fixtures do today. The signature settles where the
  stamp goes. The origin is inside the signed bytes (BR-AS47), so it must be
  written before signing, and no proxy, ingress or registry can rewrite it
  afterwards without invalidating the signature. That is the property, not a
  side effect: the origin a shell loads from is the origin the publisher
  attested to.

- **BR-AS72 — A remote URL is either same-origin or an allowlisted absolute
  origin.** Added 2026-09-02. `remote.url` may be a path with no scheme and no
  authority (`/plugins/example-plugin/remoteEntry.js`). Such a URL is
  same-origin with the shell by construction, resolves against the shell's own
  document, and carries no allowlist entry because there is no other origin to
  allow. Any URL carrying a scheme or an authority is an absolute origin and
  goes through BR-AS45's allowlist unchanged. Anything in between — a
  protocol-relative `//host/path`, or a string that resolves to an authority it
  did not name — is refused rather than guessed at.

  **Why the third case is a rule and not an implementation detail.** A
  same-origin deployment is the likely production shape: one hostname, one path
  prefix per plugin, no cross-origin fetch and therefore no CORS header at all.
  That makes "no origin" a normal answer rather than a missing value. But "no
  origin" must mean *literally the shell's origin*, and a protocol-relative URL
  is the trap — it reads as a path, resolves to a foreign host, and would carry
  a plugin past the allowlist while looking like the safe case. The refusal is
  what keeps the relative form from becoming a bypass.

  **Federation needs no change for this.** `remoteEntry.js` already loads its
  chunks with a relative `import('./assets/…')`, resolved against its own URL,
  so a path-prefixed deployment addresses every chunk correctly with nothing
  configured. Only the entry URL is at issue.

### How Phase 14's rules are checked

**Built 2026-09-02 in Phase 14 task 14a2.** The matrix names the executable
contracts that shipped with the rules.

| Rule | Specced by |
| --- | --- |
| BR-AS71 | `shared/mferegistry/announcer/announcer_test.go` proves deployment origin + release are stamped before publish and a post-sign rewrite fails verification; `shared/mfe-plugin-host/deployment_test.go` asserts all five checked-in fixture manifests are path-only. |
| BR-AS72 | `registry/rules_test.go` covers path-only admission, unchanged BR-AS45 checking for absolute URLs, and protocol-relative refusal; `lab-shell/src/shell/loader/federatedAdapter.spec.js` proves resolution against the shell document. |

---

## Phase 15 — health over NATS, and how a lost catalogue is recovered

BR-AS61 above was rewritten in place rather than retired: the responsibility it
carries is the same one, and a second rule saying "frontend availability" would
leave two answers to one question. The rule added here is the new one — what
happens when the registry loses the catalogue itself.

- **BR-AS73 — Catalogue recovery.** Added 2026-09-02. Plugins **must** announce
  themselves during startup. That is the primary mechanism for populating the
  registry catalogue, and it is the mechanism every ordinary case uses.

  The registry **may** issue a reset notice when its catalogue must be
  reconstructed while existing plugins remain running. On receipt, a plugin
  re-announces after a jitter interval. A reset notice is **not** required for
  whole-system restarts, where plugins restart and perform their normal startup
  announcement.

  The notice is a statement of fact on a dedicated subject over core NATS with no
  durability — it is worth nothing to a plugin that was not running when the
  catalogue was lost, because such a plugin announces at startup anyway. It
  carries the jitter window as a field, so the registry can widen the spread
  across a fleet without redeploying a single plugin. **A plugin clamps that
  window to a locally-owned floor and ceiling before using it**: the registry
  keeps the power to widen, and nothing on the wire gains the power to narrow it
  to zero, which would turn the notice into the simultaneous re-announce it
  exists to prevent.

  A reset fires only on an actual loss of catalogue, never on a plain restart of
  the registry with its catalogue intact. Ignoring a notice is inert — a plugin
  that never re-announces is simply not re-announced, and no path from a notice,
  or from silence in response to one, may reach unregister (BR-AS54).

  A re-announce of content the catalogue already holds writes no revision and no
  audit row, but still advances the plugin's accepted release watermark, so a
  correct-but-unnecessary notice costs the fleet nothing but the messages.

  **Why the scope is stated in the rule.** Start-up announcement is the primary
  path; the reset notice is the backstop for catalogue loss *without* plugin
  restart. The common way a catalogue is lost in this lab — `docker compose down
  -v` — restarts the plugins too, so the notice earns nothing there. It earns its
  keep on a truncated table, a restored backup, a recreated volume. Written
  without that sentence, a later reader mistakes the backstop for the recovery
  path and builds accordingly.

### How Phase 15's rules are checked

Rules first, specs next (task 15b onward). This matrix is filled in as each lands.
15b, 15c, 15d, 15e and 15f landed 2026-09-02.

| Rule | Specced by |
| --- | --- |
| BR-AS61 (rewritten) | **Done 2026-09-02.** Plugin side in `shared/mferegistry/announcer/health_test.go`: publishes on the subject derived from its own plugin id and no other, carries that id in the body as well, reports healthy only when the loopback request actually succeeded, never claims a state or cause outside the closed vocabulary, never claims the receiver-only `absent`, keeps publishing on the heartbeat when nothing changed, keeps reporting after a publish fails, and refuses to start at all when the self-check target is not a bounded loopback `GET` (no redirect, no credentials/query/fragment, capped body, deadline its own rather than the caller's). The self-check deadline is asserted **strictly less than** the heartbeat rather than both pinned. `first health push, then announce` is asserted as the observable property. A curated plugin reports without announcing, and needs no signing seed. Registry side in `registry/health_frontend_test.go`, on `domain.HealthInbox`: the id on the subject and the id in the body must agree, and everything outside the two claimable states and the closed cause set is refused. Decision 14 reverses decision 12, so `registry/internal/healthhttp/` and `domain.HealthOrigins` were **deleted**, and the probe worker now schedules BR-AS62 backend readiness only. Checklist it was written against: subject; the self-check derived from the signed entry; the self-check deadline asserted strictly less than the heartbeat interval rather than both pinned independently; a report proved to cost a real loopback `GET`; publish-on-change **and** on heartbeat both covered; `absent` and `unhealthy` proved distinct; first-health-push-then-announce asserted as the observable property, not as a source ordering. Decision 14 reverses decision 12, so the probe-worker specs are **expected** to change here. |
| BR-AS63, BR-AS64 (amended) | **Done 2026-09-02.** Threshold in `announcer/health_test.go` — one failed check is not unhealthy, the second consecutive one is (with that check's cause), any success resets the run so one failure per heartbeat forever never reaches the threshold, and a plugin whose very first checks fail starts unhealthy because nothing ever proved it good. It is the plugin's own count, reported and not inferred. Freshness in `registry/health_frontend_test.go` and `registry/health_hint_test.go` — healthy right up to the window, `stale` with cause `absent` past it, `absent` kept distinct from a plugin's own `unhealthy`, a future timestamp clamped to the receiver's clock so no plugin can buy permanent freshness, a redelivered or out-of-order report refused so nothing refreshes a dead plugin's lease, and a plugin that stops reporting aged to stale **with no probe issued anywhere**. The ratio is specced as `HealthHeartbeat < HealthFrontendFreshness`, not as two pinned numbers. Checklist it was written against: the threshold counter proved to live in the plugin and to be reported, not inferred; the registry's expiry sweep proved to mark a plugin absent one freshness window after its last heartbeat; and a spec asserting `heartbeat < freshness` rather than pinning 5s and 15s independently, since that ratio is what stops a healthy plugin flickering. |
| BR-AS73 | `registry/reset_test.go` — the predicate on its own (a plain restart is silent; a first boot is silent; a catalogue that moved *forward* is silent; a truncated catalogue and a catalogue restored from a **stale backup** both state a reset) and the carried jitter window clamped (an in-range window honoured; `0` and a negative refused up to the floor; absent takes the default; past the ceiling clamped down; the subject proved to be `notify.`, never `cmd.`). `registry/reset_service_test.go` — the same five scenarios end to end over an embedded NATS server, plus the three things an *outage* must not be mistaken for (unreadable store, unreadable witness, no cache at all), that the notice carries nothing installable, and that repairing the witness leaves the second startup silent. **15f, done 2026-09-02:** the reset-without-reannounce path leaves the real catalogue entry, revision and audit history exactly as they were; a publisher that drops the notice neither re-announces nor spends its release, and never unregisters. `shared/mferegistry/announcer/reset_test.go` — a burst of notices costs one re-announce, a notice never withdraws, an undecodable body is dropped, a shutdown mid-wait is not a withdrawal, and the subscription is one exact subject. `shared/mfe-plugin-host/deployment_test.go` — the grants: the registry may publish and never subscribe; an announcing plugin may subscribe and never publish; `demo-catalog`, which cannot announce, is not granted it at all. Checklist it was written against: 15d — decisions 6, 7, 8, 9, 13, and review item P2. **15e, convergence (decision 10):** `registry/converge_test.go` — what converges (an identical re-announce at a higher release, differing only in the signed bytes) and what does not (a table of seven content fields, a withdrawn entry, a withheld one, and every branch outside enabled/same-origin/dynamic), plus that the wire value is distinct from the other five and that a literal replay is still `NoOp` while a resync at a higher release is not. `registry/announce_transport_test.go` — the same end to end over Postgres and NATS: a converged announce consumes no revision, writes no audit row, emits no `Changed` hint and still moves the release; the release it spent is no longer acceptable below the watermark; and the watermark-only write is checked against the entry row's concurrency control — it refuses to move a row backwards, leaves a concurrent real announce's content and higher release untouched, moves only the release column when it does apply, and is still refused under a signing key that is no longer enabled (BR-AS48). |

**Live verification, 15h (2026-09-02).** `cmd/registry-acceptance` was extended and run green
against the running Docker lab, 11 steps. It now reads health on the *shell's* subject with a
second, shell-scoped credential (`HealthRead` is BR-AS25/AS27's subject and refuses an operator
credential), asserts that `demo-catalog` — curated, `HEALTH_ONLY`, announcing nothing — reports
`healthy` about itself and that `not configured` is no longer reachable on the frontend plane, and
asserts `example-plugin-unreachable` reports `unavailable` with cause `unreachable` from its own
failed loopback `GET`. A closing step SIGKILLs a live publisher so no unregister is sent, then
proves the registry's `absent` cause end to end and, on the same step, BR-AS54: the entry is still
registered, spent no release, is not withdrawn, and stays enabled.

The gate found two defects no unit spec could reach. The **deployed credentials predated the
phase** — the grants were right in `bootstrap-operator.sh`, but the minted JWTs were older, so every
plugin was refused publish on `notify._platform.health.frontend.{pluginID}.v1` and subscribe on
`notify._platform.mfe-registry.entries.reset`, and the entire health plane was dead in Docker with
every spec green (fixed by `bootstrap-operator.sh --force` + `docker compose down -v`). And a
**curated publisher could not start at all**: `announcer.ConfigFromEnv()` demanded the four
announce-only variables before reading `HEALTH_ONLY`, making `Validate()`'s curated exemption
unreachable from a deployment. `HEALTH_ONLY` is now read first; two specs under BR-AS61 in
`shared/mferegistry/announcer/health_test.go` hold it there.
