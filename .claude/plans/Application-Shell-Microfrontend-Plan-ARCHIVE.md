# Extensible Application Shell + Micro-Frontend Plugins — Plan ARCHIVE

Completed phases of `Application-Shell-Microfrontend-Plan.md`, in full and verbatim, plus the
plan's renumbering history. **Append-only** — existing sections are frozen snapshots and are never
edited, including their phase numbers during a later renumbering.

This file is **not read into context by default**. The live plan carries a self-describing stub for
every phase archived here.

---

## Renumbering history

**2026-08-28.** Phases 2, 3 and 4 (the three app migrations) became **10, 11 and 12**; the dynamic
platform registry phase became **2**. Registry work therefore precedes the migrations, and the
migration chain keeps its own order and its own gates unchanged. Phase 6 (registry service and
publishing lifecycle) keeps its number, so the plan now reads 1, 2, 6, 10, 11, 12 with deliberate
gaps rather than a dense sequence. No phase's content, status or gate changed in this move.

---

## Phase 1 — Completed (archived 2026-08-28)

### Phase 1 — COMPLETE (2026-08-28), split into 1a and 1b — Application Shell Contract, Runtime Discovery, and Independent Vue Remote Proof

#### Goal

Turn `lab-shell/` into a neutral, UniFi-styled application host and prove the architecture with one
small, independently built Vue remote that contributes navigation, a route, route-scoped shell
controls, and information panels without the host importing service feature code or being rebuilt.

Phase 1 ends with an architecture suitable for the three real migrations. It does **not** migrate
those applications yet.

#### Business rules — APPROVED 2026-08-28 (moved to `BUSINESS_RULES-APP-SHELL.md`)

These were approved at the design gate on 2026-08-28 and are now maintained in
[`demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md`](../../demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md),
**restated there with an observable failure per rule** (CLAUDE.md Quality Rule 1) and with three
amendments plus one addition made at approval. The wording below is the original proposal, kept for
provenance — **the rules file is authoritative**, not this list.

Approval-time changes: BR-AS05 gains a permission source (auth-service JWT claims); BR-AS01 gains a
registry transport (operator-curated backend endpoint, not a file in the shell bundle); BR-AS14's
migration gate becomes **delta** mockups for Phases 10–12 while Phase 1 stays capability-complete; and
**BR-AS15 is new** — a reviewable example plugin must exist and be signed off before any real
application is migrated.

1. **BR-AS01 — Curated discovery.** Only a platform-controlled plugin registry may nominate
   executable frontend modules. The browser must not discover arbitrary NATS services or accept a
   remote URL advertised directly by a domain service.
2. **BR-AS02 — Contribution-only integration.** A plugin extends the application only through the
   published contribution contract. It must not mount another top-level shell, replace global
   providers, or manipulate shell DOM.
3. **BR-AS03 — Independent deployment.** Adding, removing, enabling, disabling, or upgrading a
   compatible remote plugin must not require a shell source change or shell rebuild.
4. **BR-AS04 — Failure isolation.** A failed plugin or contribution must not prevent the shell,
   built-in capabilities, or other plugins from loading and operating.
5. **BR-AS05 — Shell-owned authorization.** The shell evaluates contribution permissions for
   navigation visibility, direct route access, and extension rendering. Hiding UI never replaces
   server-side authorization.
6. **BR-AS06 — Stable identity and order.** Plugin and contribution IDs are globally unique and
   namespaced. Contributions render in deterministic configured order.
7. **BR-AS07 — Host-owned extension points.** The owner of a screen or shell region declares and
   versions its extension points, controls layout and capacity, and passes contributors readonly
   contextual data.
8. **BR-AS08 — Lazy feature loading.** Contribution metadata is available before its implementation
   code. Remote feature code loads only when a route, panel, or explicit lifecycle need requires it.
9. **BR-AS09 — One global UI frame.** The shell is the only owner of AppShell, router, UniFi theme,
   PrimeVue configuration, Toast outlet, global locale selection, sidebar collapse, and global error
   boundaries.
10. **BR-AS10 — Preserved trust boundaries.** Plugin migration must not combine the existing Admin,
    Tech Lab Operator, and SeaFreight NATS credentials into a broader shared credential. Connection
    state remains keyed by its permission/account profile.
11. **BR-AS11 — Preserved behavior.** Migrating an existing app must preserve its current functional
    capabilities, business rules, editing affordances, localization behavior, and cleanup semantics
    unless a separate approved rule changes them.
12. **BR-AS12 — Addressable feature routes.** Every primary navigation destination is a namespaced
    shell route with browser history, refresh/deep-link behavior, and direct-access permission checks.
13. **BR-AS13 — Contract compatibility.** An unsupported registry schema, shell API version,
    contribution type, or extension-point version is rejected before remote code executes.
14. **BR-AS14 — Design fidelity gate.** Shell and migration designs use the shared UniFi system and
    are visually reviewed at 1920×1080. Each real-app migration requires an approved capability-
    complete mockup before implementation.

If approved, these belong in a new application-shell business-rules document indexed by
`demos/01-dictionary/BUSINESS_RULES.md`; they should not be forced into a backend domain file.

#### Design decisions — approval required

**1. `lab-shell/` is the permanent host.**

Retain its Vue 3/Vite stack and shared aliases. It becomes neutral infrastructure plus optional
built-in plugins. Do not create a second shell beside it and do not introduce `single-spa` while all
known applications use Vue 3.

**2. Separate contribution metadata from executable code.**

The first draft proposed loading every plugin and calling `activate()` at startup to discover what it
contributes. Reject that design. It makes startup cost proportional to installed plugins and requires
executing code before routes or permissions can be evaluated.

Instead, the platform registry supplies declarative contributions and remote locations together. The
shell validates and indexes metadata first, then lazily loads the named export only when needed.

```text
frontend-plugin-registry.json
        │
        ├── module/version/API compatibility
        ├── navigation/route/extension metadata
        └── Module Federation entry + exposed module/export
                         │
                         ▼ only on first use
                 remote implementation code
```

**3. Use one aggregated, versioned registry contract.**

Development may serve static JSON; production may replace that URL with a platform registry API
without changing the browser contract.

Recommended shape:

```json
{
  "schemaVersion": 1,
  "plugins": [
    {
      "id": "fleet",
      "version": "2.4.1",
      "shellApiVersion": 1,
      "enabled": true,
      "remote": {
        "name": "fleet_ui",
        "entry": "https://fleet.example/ui/mf-manifest.json",
        "module": "plugin"
      },
      "contributions": {
        "routes": [
          {
            "id": "fleet.page",
            "path": "/fleet",
            "export": "FleetPage",
            "permissions": ["fleet.read"]
          }
        ],
        "navigation": [
          {
            "id": "fleet.nav",
            "route": "fleet.page",
            "label": "Fleet",
            "order": 40,
            "permissions": ["fleet.read"]
          }
        ],
        "extensions": [
          {
            "id": "fleet.summary",
            "target": "organizations/details-sidebar/v1",
            "export": "FleetSummary",
            "order": 40,
            "permissions": ["fleet.read"]
          }
        ]
      }
    }
  ]
}
```

Unknown optional fields may be tolerated for forward compatibility; invalid required fields reject
that plugin only. Plugin IDs are lowercase kebab-case. Contribution IDs begin with the plugin ID.

**4. Keep Module Federation behind a loader interface.**

The host uses the pure Module Federation runtime to register dynamically discovered remotes. A
service-owned Vite remote uses `module-federation/vite` and exposes one `./plugin` module containing
named lazy component/lifecycle exports.

The host loader understands `entry`, `remote.name`, `module`, and `export`; registries and UI
components do not. A later loader change must not alter the contribution manifest.

The proof must validate `type: "module"`, remote CSS/chunk URLs, development HMR expectations,
production builds, and singleton dependency behavior rather than assuming the example configuration
transfers perfectly to this repo.

**5. Start with focused, typed contribution kinds.**

Phase 1 supports:

- `route` — namespaced path and lazy Vue component;
- `navigation` — label/icon/group/order pointing to a route;
- `extension` — component attached to a versioned named target;
- `shell-control` — a restricted extension kind for active-route topbar content;
- `shell-footer` — a restricted extension kind for active-route footer content.

Actions, commands, settings providers, global search, notification renderers, and arbitrary event-bus
messages remain outside Phase 1. Add a kind only when a real consumer defines its input and layout.

**6. Version extension-point IDs and define their context.**

Use `{owner}/{region}/v{major}`, for example:

- `shell/home-main/v1`;
- `shell/topbar-controls/v1`;
- `shell/footer/v1`;
- `demo-catalog/details-sidebar/v1`;
- later, `operator/organizations/details-sidebar/v1`.

Each target documents accepted contribution type, context schema, order semantics, per-plugin or total
capacity, loading treatment, and empty behavior. Context objects are frozen before delivery. A plugin
may declare a target that is not currently mounted, but it cannot invent the host region itself.

**7. The shell owns route and navigation semantics.**

Registry routes are installed through a shell adapter over `router.addRoute()`. Route names are the
contribution IDs. Reserved shell paths cannot be claimed. A global guard checks permissions and
renders shell-owned unauthorized/not-found states.

Navigation metadata is rendered by the host using the shared nav classes. The current `NavList.vue`
is button/model-value based; either adapt it compatibly or introduce a shell-local router-aware
renderer without weakening the shared hierarchy and collapse rules. Plugins never render their own
sidebar or sidebar toggle.

**8. The shell owns one set of Vue-wide providers.**

Install once in `lab-shell`:

- Vue and Vue Router;
- one Pinia root;
- PrimeVue with the UniFi preset;
- ToastService and one `<Toast>` outlet;
- the tooltip directive required by Admin;
- one vue-i18n instance and global locale selection;
- global UniFi and PrimeIcons CSS.

Remote entries must not call `createApp()`, install another provider, import the global theme, render
`AppShell`, or render another `<Toast>`. Their existing `main.js` can remain as a standalone
development harness that installs the same providers locally.

Share Vue, Pinia, Vue Router, and vue-i18n as compatible singletons. PrimeVue subpath sharing and
injection behavior must be proven in Phase 1; do not lock an untested federation setting merely to
reduce bundle duplication.

**9. Plugin state shares one Pinia only after IDs are namespaced.**

Current collisions make simultaneous loading unsafe:

- all three apps define `tenant`;
- Admin and Tech Lab Operator both define `dictionary`.

Real migrations rename store IDs to `{plugin-id}/{store-id}` before the plugins can coexist. The
shell does not read or mutate plugin stores. A plugin-scoped Pinia per remote is rejected initially
because it complicates Vue injection, devtools, and shared service integration without providing a
security boundary.

**10. Separate global locale choice from credential-scoped refdata transport.**

Admin and SeaFreight currently install separate i18n instances. Their shared
`useRefdataLabels.js` also contains module-global state and one mutable `transport`; both apps call
`setRefdataTransport()` with different NATS connections. Sharing it unchanged would make the last
loaded plugin silently replace the other's transport.

Recommended boundary:

- the shell owns selected locale and the global vue-i18n composer;
- plugins register namespaced messages/label resolvers;
- refdata label access becomes an instance factory keyed by plugin and credential profile, consuming
  the shell locale but not sharing mutable NATS transport;
- old per-port locale preferences need an explicit one-time migration/fallback when apps move to the
  shell's single origin.

This preserves SeaFreight's cold-paint/cache rules and prevents transport cross-wiring.

**11. Plugins own domain connections; the shell owns endpoint configuration.**

Do not replace the four current permission profiles with one shell credential:

- Admin's restricted PLATFORM connection;
- Tech Lab Operator's refdata-admin PLATFORM connection;
- Tech Lab Operator's tenant Organizations connection;
- SeaFreight's tenant connection.

A plugin lifecycle service creates and closes its own credential-scoped connections. The shell may
later offer a connection factory, but any reuse key must include permission/account identity and must
not broaden grants.

The shell provides immutable environment endpoints/base URLs so plugins stop assuming they own the
origin. This is necessary because today's Vite/nginx configurations disagree about which backend
`/api` means. `/nats`, `/api/auth`, `/files`, `/geo`, and service-specific REST paths must resolve
unambiguously in one host.

Remote-owned static assets resolve from the remote base, not `/assets` on the shell. Host-owned
platform assets may use stable shell URLs. Tech Lab Operator's `/geo/operating-areas.geojson`,
Leaflet CSS, and OpenStreetMap CSP requirements are explicit migration cases.

**12. Use a two-stage plugin lifecycle.**

```text
registry discovered → metadata validated → contributions indexed
                                              │
                                              ▼ first route/extension use
                                  remote loaded → activate once → render
```

`activate()` is optional and reserved for plugin-wide services such as NATS subscriptions. It runs
once on first use, not during registry discovery. Activation receives a frozen, capability-scoped
context: plugin identity, endpoint configuration, locale service, navigation API, and lifecycle
disposer registration. It does not receive the raw Vue app, DOM, router, Pinia, or registry objects.

Every activation registration returns/records a disposer. Failed activation rolls back partial
registrations. `deactivate()` is required for development unload/retry and eventual plugin disable;
normal production version replacement may require a page reload in v1.

**13. Isolate loading and rendering failures at contribution granularity.**

Lifecycle status is observable per plugin: discovered, incompatible, available, loading, active,
failed, or disabled. A route failure gets a shell-owned route error; a panel failure gets a local
error boundary and leaves sibling panels working. Diagnostics reveal plugin ID/version/status and a
safe error summary without exposing credentials or tokens.

The shell becomes usable before remote code finishes loading. An unavailable remote cannot block the
built-in demo catalog or other remotes.

**14. Treat remote code as trusted, not sandboxed.**

Runtime validation governs compatibility and placement, but a loaded plugin executes with browser
privileges. Production discovery must use a platform-controlled registry, approved HTTPS origins,
CSP, immutable/versioned asset URLs or equivalent cache policy, and server-side authorization.

Subresource integrity for federated chunks remains a proof question; do not promise it until the
selected Vite/Module Federation output demonstrates a workable integrity chain.

**15. Keep the current demo launcher as a built-in plugin.**

Recommended: move the current demo registry/menu/intro behind the same declarative contract as a
built-in `demo-catalog` plugin. It remains bundled with the host but exercises no privileged registry
path. The shell root becomes a neutral extension-driven home; the catalog moves to `/demos`, while
`/demos/:id` remains compatible.

This recommendation still requires user confirmation because the earlier discussion did not decide
whether the current demo-launcher role survives the platform-shell transition.

**16. Prove the contract with a deliberately small independent Vue remote.**

Before touching the three real apps, an example remote must be separately built and served. It must
demonstrate:

- registry-only installation with no host source edit;
- navigation and deep-linked route contribution;
- a route-scoped topbar control;
- panels in at least two extension targets, including one owned by another built-in feature;
- plugin activation/disposal;
- lazy code/CSS loading;
- online, unavailable, incompatible, and throwing states;
- remote rebuild/redeploy visible after shell reload without a host rebuild.

The fixture can live temporarily under `lab-shell/example-remote/`; it is not a precedent for keeping
real service plugins under the host folder.

**17. Preserve independent plugin development.**

Each real remote keeps a standalone harness for focused development and component tests. The harness
uses the shared theme and mounts the same feature roots, but production exposes an unmounted plugin
module. A minimal shell test harness should also be available so a plugin team can test navigation,
extension targets, permissions, and lifecycle without running every backend UI.

**18. Make mockup approval a hard gate for each real migration.**

Phase 1 shell mockups must show empty, loading, active, and failed-plugin states plus the contributed
route/panel composition. Later app mockups must begin from the running app and carry forward every
functional affordance. Approved/current references are used; rejected/deferred mockups are labelled
as such and do not silently become scope.

**19. Migrate one existing app per later phase, initially as one remote each.**

Do not immediately split the applications by backend microservice. Their present screens aggregate
cross-service state and have tested orchestration. First migration boundary:

```text
lab-shell host
├── admin remote
├── seafreight remote
└── tech-lab-operator remote
```

Each remote may contribute several routes and shell regions. Finer vertical-slice decomposition is a
future design decision after coexistence is proven.

Recommended order is SeaFreight Flow → Admin → Tech Lab Operator:

1. SeaFreight is the smallest navigation surface while proving tenant switching, full localization,
   NATS lifecycle, and navigation between Fleet/Port/Pricing.
2. Admin then proves the broadest navigation tree, extensive component suite, tooltip/toast use,
   diagnostics REST paths, and telemetry footer.
3. Tech Lab Operator goes last because it combines two NATS identities, Reference Data and
   Organizations, the largest feature component, file/geo assets, and route-dependent context bars.

This order is recommended, not yet approved.

**20. Move shared shell contract tests to the host when implementation begins.**

`AppShell.spec.js` and `NavList.spec.js` currently run under Admin only because `shared/ui-shell` has
no test runner. Once `lab-shell` is the permanent host, it becomes the canonical runner for these
tests.

**Confirmed at approval (2026-08-28): a test runner is mandatory, because everything must be
testable.** `lab-shell/package.json` today has only `dev`, `build` and `preview` — no runner at all,
so not one of BR-AS01–BR-AS15 is currently enforceable there. **Phase 1a's first task** is adding
Vitest to `lab-shell/`, matching the setup already proven in
`demos/01-dictionary/frontend/admin/`: Vitest 4 + happy-dom + `vue/test-utils`, `test` config inside
`vite.config.js`, `test` / `test:watch` scripts. Two rules (BR-AS09's no-plugin-imports graph check
and BR-AS14's no-competing-theme-tokens check) are enforced as build-time lint/graph checks run from
`npm test` rather than as runtime specs — a runtime spec would catch those too late to be useful. Existing app suites remain green throughout migration, and every plugin must pass a production
build because Vite development has previously hidden shared-module resolution failures.

**21. Registry ownership — `accounts-service` serves the curated registry (decided at approval).**

BR-AS01's amendment requires an operator-curated backend endpoint rather than a file in the shell's
bundle. `accounts-service` is the placement: it is already the context-free service administering the
platform/tenant axis (its subjects carry no `{context}`, per CLAUDE.md), and it already owns the auth
and token lifecycle that BR-AS05's permission claims come from — so the registry and the claims that
gate it share one owner. This is the one placement decision made without a strong precedent in the
repo; a dedicated platform service instead would be a small, contained change to Phase 1a.

---

#### Phase 1a — the contract, provable with no remote

Split out at approval so the contract can land and be reviewed before Module Federation is
introduced. If federation misbehaves, 1a has still shipped.

Scope: Vitest in `lab-shell/` (first task — see Design decision 20); plugin metadata schema
(`schemaVersion`, `shellApiVersion`); the registry client against the `accounts-service` endpoint;
the contribution registry and its five kinds; host-owned extension points at
`{owner}/{region}/v{major}`; permission evaluation from auth-service JWT claims; the loader adapter
**interface** with a local synchronous implementation only; `{plugin-id}/{store-id}` Pinia
namespacing; and the demo catalog migrated to a built-in plugin at `/demos`.

Exit criteria: `/demos` renders entirely through the public contribution API with no privileged
path; `lab-shell/` imports nothing app-specific (build-time graph check, BR-AS09); the registry
reports `discovered → available → active`; `lab-shell`'s dev port is aligned into 7100–7199 or
explicitly exempted.

#### Phase 1a tasks

Derived from the approved rules; each task names the rule whose observable failure its specs must
reproduce. `1a-1` is first by Design decision 20 — no other 1a task lands without specs.

- [x] **1a-1 — Test runner.** Vitest 4 + happy-dom + `vue/test-utils` in `lab-shell/`, config in the
      existing `vite.config.js` (`test` block, matching the other three frontends), `npm test` script.
      Precondition for every rule below, not a rule of its own.
- [x] **1a-2 — Port alignment.** Move the dev server off `5170` to **7109**, and add a
      `.claude/launch.json` entry. Not 7103–7108: `docker-compose` publishes every port 7100–7106 and
      holds them whenever the stack is up, and 7107/7108 are already claimed in `launch.json`. Record
      it in `lab-shell/`'s port table.
- [x] **1a-3 — Shell source layout.** `lab-shell/src/shell/{registry,contributions,extensions,auth,loader,connections}/`,
      matching the "Where these are enforced" table in `BUSINESS_RULES-APP-SHELL.md`.
- [x] **1a-4 — Plugin metadata schema and validator.** `schemaVersion` + `shellApiVersion`.
      *(BR-AS13 — an unsupported version resolves `incompatible` and its code never executes.)*
- [x] **1a-5 — Registry client** against the `accounts-service` endpoint, curated-only.
      *(BR-AS01 — a remote URL absent from the registry response is never fetched, however it reaches
      the browser. BR-AS04 — registry unreachable still boots the shell with its built-ins.)*
- [x] **1a-6 — `accounts-service` registry endpoint.** Read-only, no `{context}` token, Ginkgo spec.
      Its rule stays BR-AS01 in the app-shell rules file; cross-reference it from
      `BUSINESS_RULES-ACCOUNTS.md` rather than restating it there.
- [x] **1a-7 — Contribution registry and the five kinds** (`route`, `navigation`, `extension`,
      `shell-control`, `shell-footer`). *(BR-AS06 — duplicate or un-namespaced IDs are rejected and
      render order is deterministic. BR-AS13 — an unknown kind is rejected before load.)*
- [x] **1a-8 — Host-owned extension points** at `{owner}/{region}/v{major}`, with owner-declared
      capacity and readonly context. Ships `shell/topbar-controls/v1`, `shell/footer/v1`,
      `shell/home-main/v1`. *(BR-AS07 — a contributor cannot exceed capacity or mutate the context it
      is passed. BR-AS13 — a wrong-major target is rejected before load.)*
- [x] **1a-9 — Permission evaluation** from auth-service JWT claims, for nav visibility, direct route
      access, and extension rendering. *(BR-AS05 — the route guard must be shown to run independently
      of nav visibility, so hiding is never the only check.)*
- [x] **1a-10 — Plugin status model** — the seven observable statuses as one machine
      (`discovered`, `incompatible`, `disabled`, `available`, `loading`, `active`, `failed`),
      surfaced to the Plugins screen. *(BR-AS04, BR-AS08.)*
- [x] **1a-11 — Loader adapter interface** plus a local synchronous implementation only; no Module
      Federation in 1a. *(BR-AS08 — `activate()` runs exactly once per plugin, after first use.)*
- [x] **1a-12 — Pinia namespacing** `{plugin-id}/{store-id}`. *(BR-AS02/BR-AS06 — the audit found
      three plugins that would each register `tenant` and two that would register `dictionary`; two
      plugins using the same store id must not observe each other's state.)*
- [x] **1a-13 — Connection registry keyed by credential profile.** *(BR-AS10 — the four profiles
      resolve to four distinct connections and never merge, and no module-global mutable per-tenant
      value — the `useRefdataLabels.js` shape — is reachable from shell code.)*
- [x] **1a-14 — Global frame ownership.** AppShell, router, UniFi theme, PrimeVue config, Toast
      outlet, locale, sidebar collapse, global error boundary. Enforced by an ESLint rule plus a
      build-time import-graph check. *(BR-AS09 — `lab-shell/src/shell/**` importing anything
      app-specific fails the build.)*
- [x] **1a-15 — Demo catalog as a built-in plugin** at `/demos`, through the public contribution API.
      *(BR-AS02 — the dogfood proof: it registers through the same entry shape as a remote and has no
      privileged path the contract does not expose.)*
- [x] **1a-16 — Addressable routes.** Namespaced shell routes with history, refresh, deep-link and
      direct-access permission checks. *(BR-AS12.)*
- [x] **1a-17 — Docs sync.** As-built deltas into `ARCHITECTURE-APP-SHELL.md` and
      `BUSINESS_RULES-APP-SHELL.md`, including the loading-animation decision under BR-AS08 (it
      currently exists only in the mockups and the canvas annotation).

#### Phase 1a — COMPLETE 2026-08-28

All 17 tasks landed. 167 specs across 13 files green in `lab-shell` (`npm test`), plus 8 DB-free
Ginkgo specs on the `accounts-service` endpoint. Verified in the browser at 1920×1080: `/` redirects
into `/demos`, the nav entry is rendered from the catalog's navigation contribution, a deep link to
`/demos/01-dictionary` resolves on direct address and refresh, an unclaimed path falls through to
not-found, and the shell boots and renders with the registry endpoint returning 404 (BR-AS04).

Four as-built deltas to the approved contract — `routePrefix`, manifest-declared plugin-owned
extension points, `available → failed` as a legal transition, and the endpoint path
`/api/accounts/frontend-plugins` — are recorded in `BUSINESS_RULES-APP-SHELL.md` § "As-built
contract deltas" and `ARCHITECTURE-APP-SHELL.md` § "As built — Phase 1a".

#### Phase 1b — the example plugin, and everything that can only fail across a network

Scope: the `module-federation/vite` implementation of the loader interface; the **example plugin**
required by BR-AS15, built and served independently on port 7110; no-host-rebuild deployment; lazy
loading on first use; deep links into a not-yet-loaded remote; and the four states the mockups
specify — `loading`, `failed`, `incompatible`, and a contribution throwing inside `activate()`.

Per BR-AS15 the example plugin is the capability review, not a smoke test: it must contribute one of
every kind (route, navigation, extension into both a shell-owned and a built-in-owned target,
route-scoped shell control, footer) and must be able to demonstrate each failure state on demand.

#### Phase 1b tasks

- [x] **1b-1 — Module Federation loader.** `module-federation/vite` implementing the 1a loader
      interface. If the interface has to change to accommodate it, that is a recorded revision of the
      1a contract, not a silent edit. *(BR-AS03.)*
- [x] **1b-2 — Example plugin package** at `lab-shell/plugins/example-plugin/` — its own
      `package.json`, Vite config and build, dev port **7110**, built and served independently of the
      host. *(BR-AS03, BR-AS15.)*
- [x] **1b-3 — One contribution of every kind.** Route; navigation; an extension into
      `shell/home-main/v1` (shell-owned) **and** into `demo-catalog/details-sidebar/v1` (owned by a
      built-in feature — the cross-owner case); a route-scoped `shell-control`; a `shell-footer`.
      *(BR-AS07, BR-AS15 — this is the capability review, not a smoke test.)*
- [x] **1b-4 — On-demand failure switches.** `loading` (injected delay), `failed` (unreachable
      chunk), `incompatible` (bumped `shellApiVersion`), and a contribution throwing inside
      `activate()`. Each is both a spec and something the user can trigger live during the review.
      *(BR-AS04, BR-AS13.)*
- [x] **1b-5 — Lazy loading on first use.** *(BR-AS08 — no remote chunk is requested while only
      metadata is needed; the nav entry exists before its code does.)*
- [x] **1b-6 — Deep link into a not-yet-loaded remote.** Cold start resolves the route, loads the
      remote, and renders, with the loading state visible in between. *(BR-AS08, BR-AS12.)*
- [x] **1b-7 — Failure isolation proof.** With the example plugin failing, the shell, the demo
      catalog, and any second plugin stay loaded and operable. *(BR-AS04.)*
- [x] **1b-8 — No-host-rebuild proof.** Deploy the example plugin twice with a visible change and
      show the host bundle hash unchanged. A scripted check, not an assertion in prose. *(BR-AS03.)*
- [x] **1b-9 — Loading affordance as designed.** Skeleton sweep plus the fuzzy extension-point
      placeholder, `prefers-reduced-motion` honoured, reviewed at 1920×1080. *(BR-AS08, BR-AS14 —
      motion is the signal that the shell is working rather than stalled.)*
- [x] **1b-10 — User review of the running example plugin.** The gate itself; nothing below it
      proceeds without sign-off. *(BR-AS15.)* **SATISFIED 2026-08-28** — reviewed and signed off by
      the user after the mockup-fidelity pass. Phase 10 is now unblocked.

Exit criteria: the Phase 1 mockup gate (BR-AS14, satisfied 2026-08-28); the host demonstrably not
rebuilt between two deployments of the example plugin; and — the gate that actually opens Phase 10 —
**the user has reviewed the running example plugin and signed off on capability and integration**.

#### Gate before Phase 10 — PASSED (2026-08-28)

**No existing application is migrated until the Phase 1b example plugin has been reviewed by the
user** (BR-AS15). The automated assertions are necessary for that review but do not substitute for
it. **The user reviewed the running plugin on 2026-08-28 and signed off**, so Phase 1 is complete
and Phase 10 no longer waits on this gate — it waits only on its own current-state audit,
business-rule confirmation, delta mockup and design approval.
