# Extensible Application Shell + Micro-Frontend Plugins — Plan

> **Status: DESIGN GATE PASSED (2026-08-28) — Phase 1 APPROVED, split 1a/1b.**
>
> This file follows `CLAUDE.md`'s required sequence: proposed business rules first, then an explicit
> design gate. The gate was passed on 2026-08-28 — see
> "Design-gate decisions — resolved" at the foot of this file for what was approved and amended.
> Approved rules now live in
> [`BUSINESS_RULES-APP-SHELL.md`](../../demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md)
> (BR-AS01–BR-AS15); this file keeps the phasing.
>
> Target host: [`lab-shell/`](../../lab-shell/)
>
> Source discussion:
> [`lab-shell/application-shell-microfrontend-chat.md`](../../lab-shell/application-shell-microfrontend-chat.md)
>
> Architecture reference:
> [`ARCHITECTURE-APP-SHELL.md`](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-APP-SHELL.md)
> — the shell's own architecture doc (kernel, plugin registry, contribution kinds, extension points,
> loader adapter, migration map). This plan is its phasing; that doc is the design of record.
>
> Phase 1 shell mockups (Design decision 18):
> [`lab-shell/diagrams/phase1-shell-mockups/`](../../lab-shell/diagrams/phase1-shell-mockups/),
> published as a canvas at
> <https://claude.ai/code/artifact/2bd8787c-79a0-4e40-ac39-41429a405da3>
>
> Phase 2 registry mockups:
> [`lab-shell/diagrams/phase2-registry-mockups/`](../../lab-shell/diagrams/phase2-registry-mockups/),
> published as a canvas at
> <https://claude.ai/code/artifact/c7d139c4-1e7a-4ac2-9d41-cb0611409118>,
> all six artboards on one sheet in
> [`phase2-registry-mockups.png`](../../lab-shell/diagrams/phase2-registry-mockups/phase2-registry-mockups.png)
>
> Phase structure reference: [`Main-POC-Plan.md`](Main-POC-Plan.md)

## Purpose

Design an extensible frontend application shell, using this repository's existing Vue/Vite stack,
that starts with no service-specific feature knowledge and composes trusted, independently built UI
plugins at runtime.

The shell must support service-owned contributions such as navigation entries, routes, full-page
features, route-scoped topbar controls, footer content, and information panels placed into named
extension points. A plugin contributes through a versioned contract; it does not mount a second
application shell or manipulate shell DOM directly.

Once the shell architecture and an independently built proof plugin are complete, the three current
applications under [`demos/01-dictionary/frontend/`](../../demos/01-dictionary/frontend/) will be
migrated into it in later, separately approved phases:

- Admin;
- Tech Lab Operator (`refdata`);
- SeaFreight Flow (`seafreight-app`).

The migration is an arrangement and deployment change, not permission to remove existing behavior,
editing capability, trust boundaries, localization guarantees, or the UniFi look and feel.

## Existing repository baseline

### Host

`lab-shell/` is already a Vue 3.5 + Vite 7 application using Vue Router, Pinia, PrimeVue 4, the
shared UniFi theme, and `shared/ui-shell/AppShell.vue`. It currently knows about one Dictionary POC
through a static `demos.js` registry and opens that demo's applications in separate browser tabs.

The shell is therefore evolved in place; it is not replaced with another framework or a second
top-level application.

### Applications to migrate later

| Application | Current view model | Runtime identity | Natural first plugin contributions |
|---|---|---|---|
| Admin | `activeView` ref, no router | restricted PLATFORM NATS connection plus REST diagnostics | Overview, Accounts, Users, Services, Connections, Pub/Sub, Request/Reply, Streams, KV, Logs, Tables, Settings, telemetry footer |
| Tech Lab Operator | `topNav` ref, no router | PLATFORM refdata-admin connection plus a separate tenant connection for Organizations | Reference Data, Shippers, Transporters, route-specific context controls |
| SeaFreight Flow | `activeView` ref, no router | tenant-account connection that reconnects when tenant changes | Fleet, Port Management, Pricing, tenant/business-unit/locale controls |

Their `App.vue` files are mostly orchestration: shared shell slots, navigation metadata, lifecycle
startup, and mutually exclusive feature panels. The underlying feature panels are already separate
Vue components and are suitable route contribution boundaries.

### Existing design system

The following are constraints, not inspiration to reinterpret:

- [`shared/unifi-theme/unifi.css`](../../shared/unifi-theme/unifi.css) owns palette, Inter
  13px/20px typography, panel treatment, tables, tabs, dialogs, and disabled-state styling.
- [`shared/unifi-theme/preset.js`](../../shared/unifi-theme/preset.js) owns the PrimeVue preset and
  dark-mode state.
- [`shared/ui-shell/AppShell.vue`](../../shared/ui-shell/AppShell.vue) owns the single topbar,
  optional sidebar, main content area, footer outlet, theme toggle, and sidebar-collapse state.
- [`shared/ui-shell/NavList.vue`](../../shared/ui-shell/NavList.vue) defines the current navigation
  hierarchy: optional group → optional eyebrow → item; there is no third level.
- [`shared/unifi-theme/LAYOUT.md`](../../shared/unifi-theme/LAYOUT.md) is the shell contract.
- The one bottom-right sidebar toggle, inline SVG, and ARIA behavior are enforced by
  [`AppShell.spec.js`](../../demos/01-dictionary/frontend/admin/src/components/AppShell.spec.js).
- Navigation group behavior is enforced by
  [`NavList.spec.js`](../../demos/01-dictionary/frontend/admin/src/components/NavList.spec.js).
- Layout is designed and judged at **1920×1080** first.

### UI references and mockups

Use the current applications and these artifacts together:

- [`app-shell-reference.html`](../../shared/unifi-theme/app-shell-reference.html) — canonical shell
  composition.
- [`Matching-Unifi-Flat-Theme.png`](../../shared/unifi-theme/Matching-Unifi-Flat-Theme.png) — current,
  implemented UniFi surface direction.
- Admin mockups under [`demos/01-dictionary/diagrams/`](../../demos/01-dictionary/diagrams/) —
  especially Request/Reply, Traces, Connections, and Users references.
- Tech Lab Operator's as-built summary:
  [`phase36-tech-lab-operator.png`](../../obsidian/V3-Platform/Architecture/Dictionary-POC/images/phase36-tech-lab-operator.png).
- Transporter/GIT Certificate references under
  [`images/phase39/`](../../obsidian/V3-Platform/Architecture/Dictionary-POC/images/phase39/).
- SeaFreight Flow has no dedicated current screen mockup; its running application and component tests
  are the fidelity source.

Historical proposals are not automatically targets. In particular,
[`app-shell-collapse-btn-mockup.html`](../../demos/01-dictionary/diagrams/app-shell-collapse-btn-mockup.html)
shows the rejected top-of-sidebar collapse placement; the current bottom placement is canonical.

For every migration mockup, inventory and preserve the running app's create/edit buttons, row menus,
nested tabs, secondary views, status behavior, and navigation. If code, mockup, and documentation
disagree, the running app is the behavioral source of truth and the discrepancy is raised explicitly.

## External implementation research

The design draws patterns from current project code and documentation, not from generic
micro-frontend tutorials.

### OpenMRS O3

[OpenMRS O3](https://github.com/openmrs/openmrs-esm-core) separates:

- a routes/contributions registry that declares pages, extensions, privileges, feature flags, and
  runtime conditions;
- an import map that locates module bundles;
- Module Federation that loads component code only when a route or extension needs it.

Its extension slots are host-owned, while declarative configuration can add, remove, configure, and
reorder assigned extensions. This metadata/code split is the strongest match for the desired shell:
[module loading](https://o3-docs.openmrs.org/en-US/docs/frontend-modules/loading-modules/) and
[extension system](https://o3-docs.openmrs.org/en-US/docs/extension-system/).

### Grafana

[Grafana UI extensions](https://grafana.com/developers/plugin-tools/how-to-guides/ui-extensions/ui-extensions-concepts)
separate content providers, content consumers, and extension points. The extension-point owner
decides how contributions render, freezes contextual data passed to them, may limit contributors, and
declares extension points in plugin metadata. Grafana also versions extension-point IDs, e.g.
`plugin-id/toolbar/v1`. Adopt the ownership, context, declaration, and versioning patterns.

### Backstage

[Backstage's frontend system](https://backstage.io/docs/next/frontend-system/architecture/extensions/)
models extensions as typed outputs attached to compatible parent inputs. It validates unique IDs,
supports disabled/configured extensions, and recommends lean factories with lazy work. Adopt typed
contribution kinds and compatible host targets, but do not reproduce Backstage's full extension tree
for the first shell.

### Eclipse Theia

[Theia](https://github.com/eclipse-theia/theia) uses separate contribution interfaces and registries
for application lifecycle, commands, menus, keybindings, and widgets. Adopt the principle that
contribution kinds have focused registries and explicit lifecycle stages; do not introduce its
dependency-injection container into this Vue application.

### Module Federation for Vite

The current [`module-federation/vite`](https://github.com/module-federation/vite) project includes a
Vue 3 host/remote example and shares Vue between builds. The Module Federation runtime supports
registering and loading remotes discovered at runtime. This is the proposed loader, behind a
shell-owned adapter rather than exposed as the application architecture.

## Implementation phases

### Phase 1 — Completed (archived 2026-08-28) — Application Shell Contract, Runtime Discovery, and Independent Vue Remote Proof

Full detail is in `Application-Shell-Microfrontend-Plan-ARCHIVE.md` and is **not read into context
by default**.

- [x] **1a — the contract, with no remote at all.** Vitest in `lab-shell/`; the manifest schema and
      status machine; the contribution registry and its five kinds; shell-owned extension points;
      the permission evaluator; boot as discovery → validation → indexing with zero code loaded; the
      curated registry endpoint on `accounts-service`; `demo-catalog` as the first plugin, using the
      public contribution API and no privileged path.
- [x] **1b — an independently built and deployed Vue remote.** The Module Federation loader behind a
      shell-owned adapter (the host declares no remotes at build time, BR-AS03); the example plugin
      on its own build and dev server, exercising every contribution kind plus the `loading`,
      `failed`, `incompatible` and activate-throws states on demand; failure isolation and per-entry
      rejection made visible; the host-bundle fingerprint proving zero host rebuilds between two
      plugin deployments.
- [x] **1b mockup-fidelity pass (2026-08-28).** Breadcrumb, nav dots, topbar attention aggregate,
      the rebuilt failure panel with a real retry, the shared status tokens, and the Plugins screen's
      version / shell-API / contribution / lifecycle detail — the chrome the artboards drew.
- [x] **BR-AS14 (capability-complete mockups) satisfied 2026-08-28** — seven 1920×1080 artboards.
- [x] **BR-AS15 gate PASSED 2026-08-28** — the user reviewed the running example plugin and signed
      off. Phase 10 no longer waits on this gate, only on its own.

---

### Phase 2 — APPROVED (gate passed 2026-08-28) — Dynamic Platform Registry (registry as service state)

**Not blocked on Phases 10–12.** This phase changes only where the curated registry lives and how it
propagates; the shell's read contract (`GET` a document carrying `schemaVersion`, `revision` and
entries) is unchanged — confirmed against `manifestSchema.js`, not assumed (decision 27) — which is
what makes it independent of the migrations. The independence is real only because the change signal
rides that same read path: routing it over NATS instead would have made this phase wait for the
credential profiles Phases 10–12 bring, since `lab-shell` holds no NATS connection today (decision
36). Reversibility of the store choice is likewise not free — it is bought by decision 35's
interface, and without that it is a claim rather than a property.

**Problem.** The registry is centralized already — one operator-curated document, one endpoint — but
statically sourced: a JSON file bind-mounted into `accounts-service` and read once at process start
(`FRONTEND_PLUGIN_REGISTRY_FILE`). Changing curation therefore means editing a file on the host and
restarting a backend service. There is no audit of who changed what, no concurrency story when two
pipelines write, and `revision` is a hand-typed string (`"dev-1b"`) rather than a value anything can
trust. single-spa built a dedicated *import map deployer* service largely to solve that last problem —
the race when several deployments update one shared document — which is the clearest external signal
that a file does not survive past one team.

**Scope.** Registry entries become service state in `accounts-service`: Postgres as source of truth,
a KV read cache in front of it (the shape the POC settled on), an admin CRUD surface, a real
monotonic `revision`, and change propagation over the existing `notify.*` family. The mounted JSON
file becomes a **seed**, the same relationship the rest of this repo already has between seed data
and Postgres.

**Explicitly out of scope** (this is Phase 6's subject, and the boundary is the point): plugin
publishing — upload, signing, verification, staging, promotion — and any notion of a plugin
announcing itself. Also out of scope: unloading a plugin whose `activate()` has run.

#### Design decisions — 22–36, approved at the gate

> Revised 2026-08-28 after a `codebase-design` review of this phase against the shipped Phase 1 code
> and `accounts-service`'s current registry implementation. Decisions 23, 25, 26, 27, 31 and 32 were
> amended (23's stated rationale did not hold; 26 and 32 were under-priced); 35 and 36 are new and
> name two interfaces the phase had left implicit. BR-AS19 and BR-AS20 were amended in the same pass.
> Nothing was implemented — the gate below still stands.


| # | Decision | Rationale |
| --- | --- | --- |
| 22 | **Curation is platform-wide, not per-tenant** (user's call, 2026-08-28) | One curated set for every shell. Per-`{context}` curation is a schema question, so the table keeps room for it (see 24), but no read path or UI takes a scope argument in this phase. |
| 23 | **Postgres source of truth, KV as write-through read cache** | The registry is small, read on every shell boot and rarely written. Applied honestly, the deletion test does not carry the cache on its own: six rows read at boot is not a cost, and a second store adds a coherence path plus a warm/cold state an operator has to learn. It is here because *this shape is the POC's subject* — the registry is a good small instance of the pattern the lab exists to evaluate — not because the read is slow. **The earlier rationale (that the KV entry supplies the watch decision 25 needs) is withdrawn: nothing in this phase consumes a KV watch.** If the cache is dropped later, decision 35's interface is what makes that a one-adapter change. |
| 24 | **Entries are rows, seeded by a manual CLI only** (amended at the gate, Q6) | `registry.dev.json` stays in the repo as the seed input, so local review keeps working unchanged and the file stops being production configuration. Seeding is `cmd/seed-registry`, run by hand against the running service over REST — never at process start, matching every other `cmd/seed-*` in this repo. **`FRONTEND_PLUGIN_REGISTRY_FILE` is removed from the service** in the same change: leaving a boot-time file read alongside a database would let a restart silently revert curation, and the repo's seeder idiom dissolves that problem by construction rather than guarding against it. |
| 25 | **Propagation is a signal, never a hot-swap** | A revision change is published on `notify.*` for service-side consumers, and reaches the *shell* over its existing read path (decision 27's conditional GET) — see decision 36 for why the browser is not on NATS in this phase. Either way the shell surfaces "the plugin catalog changed — reload to apply". This is not timidity: the status machine has no transition out of `active`, so a plugin whose entry disappears while its components are mounted has no legal state to move to. Reload is the only sound way to apply a removal, and the contract should say so rather than imply otherwise. |
| 26 | **Additions may be indexed live; removals and URL changes may not** | Indexing reads metadata and fetches no remote code (BR-AS08), so a new entry is safe to place without a reload. This gets the valuable half of live update with none of the risk. **It is not free on the shell side, though, and the phase prices it:** `contributionRegistry.index()` appends to its arrays (re-indexing the same set duplicates) and `createShellRoutes` maps `contributions.routes` into router config once at boot, so live indexing needs an incremental `index()` and a runtime `router.addRoute`. "Built once at boot" is an invariant callers rely on today; this decision retires it deliberately rather than by accident. |
| 27 | **`revision` becomes the concurrency token** | Monotonic, server-assigned. ETag / `If-None-Match` on the read; optimistic concurrency on the write, keyed on the revision the writer read. This is the single-spa race, answered by a transaction instead of a service — but only if writes are actually keyed on it. **The read contract is confirmed unchanged, not assumed:** `manifestSchema.js` already validates `revision` as `string \| number` and stringifies it, so moving from `"dev-1b"` to a monotonic integer needs no shell change. The conditional GET also carries the change signal decision 25 needs. |
| 28 | **Remote origins are allowlisted in service configuration, not in the mutable document** | A dynamic write path widens the blast radius of a compromised registry from "filesystem access on the host" to "one API call". Config-level origin allowlisting means a rogue write still cannot point the shell at an arbitrary host. **Per-entry SRI is deferred to Phase 6** (amended at the gate, Q11): `federatedAdapter.js` registers remotes with `type: 'module'` and loads them through `loadRemote`, i.e. a dynamic `import()`, which carries no integrity attribute — an `integrity` field on the entry would be stored and displayed but never enforced, which is worse than absent. It belongs with the signing lifecycle that can actually check it. Phase 2's second layer is therefore the allowlist alone. |
| 29 | **Self-registration stays prohibited** | A plugin may not announce itself, by any transport. This is BR-AS01's guarantee restated for a write path that did not previously exist; without it the registry stops being an operator decision and becomes an ambient one. |
| 30 | **The degraded path is preserved and tested** (amended at the gate, Q7/Q8) | Read order is **Postgres → KV → built-ins**: source of truth first, cache as the fallback, so a stale cache can never mask a live row. With both unavailable the endpoint answers `200 { schemaVersion, revision: 0, degraded: true, plugins: [] }` and the shell renders its built-ins. **There is no "built-in set" for the service to serve** — `curatedFrontendPlugins` is deliberately empty because the shell's built-ins ship inside the shell's own bundle and are never listed (`frontendplugins.go:116`); a degraded response is therefore an empty document that says so, not a substitute catalog. `revision: 0` can never collide with a real revision, which starts at 1. |
| 31 | **Registry writes are audited** | "Who enabled this plugin, when" is asked after an incident, not before one. **Plain append-only rows, not an event-sourced log.** CLAUDE.md's deciding question is whether anything replays the history: the audit panel reads writes in order and nothing reconstructs registry state from them, so this is CRUD. It reuses the *shape* of `accounts/audit.go` (action, actor, outcome, JSONB metadata, no UPDATE, no DELETE) in the registry's own schema — the shape, never the table, since decision 33 forbids the join. It flips to event-sourced only if "what was curated at revision N" becomes a real requirement; that is the named trigger, and it is not in this phase. A refused write consumes no revision and is recorded as refused. **The actor is the literal `admin`** (amended at the gate, Q1) — `accounts-service` authenticates every request as one shared BasicAuth identity (`middleware.go:14`), so no operator identity exists to record; see BR-AS23. |
| 32 | **Own bounded-context module, same process** | The registry becomes its own module with its own domain package and `composition.go`, reaching `accounts` only through a port for the BR-AS05 claims — never into its internals. **Note the real cost: `accounts-service` has no `composition.go` and no hexagonal split today** — it is a flat ~40-file `accounts` package, the one backend module that never adopted the layout the other five use. This decision introduces that pattern to the service, which is more than "move some files"; the gate should price it as such. A separate *process* is still deferred to Phase 6, because it buys none of what makes Phase 6 expensive (the publishing lifecycle, which exists in neither option today) while paying now: its own NATS credential and account user, a 72xx port, its own database and migrations, compose service, health/observability wiring, docs and suite — and it turns the shell's boot-path read into a cross-service call for a document that changes a few times a month. Phase 6 then moves a module into its own `main.go`. |
| 33 | **The registry owns its tables outright** | No join from an accounts table into a registry table, in either direction. This, not the code's location, is what decides whether the Phase 6 split is small or structural. |
| 34 | **The endpoint path names the capability, not today's host** | Move the shell to `/api/platform/registry/frontend-plugins`, still served by `accounts-service`. `/api/platform/accounts/...` bakes the current owner into the shell's constant and every frontend's Vite proxy; renaming it now is one line, renaming it at Phase 6 is a client change across apps. Phase 6 then becomes a routing change. **Clean break** (confirmed at the gate, Q5): the old path 404s rather than aliasing, and the five call sites — the shell's `REGISTRY_ENDPOINT` constant plus the Vite proxies — move in the same change. |
| 35 | **The module's interface is named before its store changes** | The phase's own claim — reversible if the store choice turns out wrong — is a property of the interface, not of the store, and today there is no interface to be reversible behind: the registry is two package-level mutable globals, four exported loaders/setters and a handler, which is as much surface as implementation. Phase 2 replaces the implementation and therefore must name the seam it is replacing it behind. Two methods carry the phase — `Current(ctx) (Document, error)` and `Apply(ctx, Write) (Document, error)` — with revision assignment (27), the origin check (28), the KV write-through (23), the audit append (31) and the notification (25) all *behind* them. The handler and the seeder then each learn two methods, and the store swap stays in one place. This also retires a live defect in the current shape: `SetCuratedFrontendPlugins` and `SetCuratedFrontendRevision` are two setters for one fact, so a caller can install a set under the previous revision — exactly BR-AS17's failure mode. `Apply` installs both or neither. |
| 36 | **The change notification is a five-token subject, and the browser is not on NATS in this phase** | Two separate points the phase kept implicit. (a) The subject is `notify._platform.registry.frontend-plugins.changed` — five tokens, matching the `notify.{context}.{service}.{entity}.{action}` family that `shipping-service`'s `internal/notify` already builds and that CLAUDE.md's fixed-arity positional parsers require. `_platform` is the reserved platform context; this is the one place an `accounts-service`-hosted subject carries a context token, and it does so because the registry is its own bounded context (32), not the tenant axis `accounts-service` otherwise administers. (b) **`lab-shell` has no NATS client** — all three migrating apps depend on `@nats-io/nats-core`; the shell does not, and `connectionRegistry.js` is profile bookkeeping rather than a connection. So `notify.*` reaches service-side consumers, and the shell learns of a change through decision 27's conditional GET. Without this, BR-AS19 would have silently depended on Phases 10–12 while the phase header claims independence from them. |

#### Design decisions — resolved at the gate, 2026-08-28

> Fifteen open questions were put to the user one at a time, each checked against shipped code first
> so the options described what exists rather than what the plan assumed. Three of those checks
> changed the phase: there is no operator identity to audit (37), nothing in the shell ever re-reads
> the registry (44), and the service has no built-in set to serve when degraded (30, amended).
> Decisions 24, 28, 30, 31 and 34 above were amended in the same pass.

| # | Decision | Rationale |
| --- | --- | --- |
| 37 | **Write auth is the existing shared secret; the audit says so** (Q1) | Writes sit behind the same BasicAuth as every other `/api/accounts*` route. `BasicAuthUser` is the fixed literal `admin` and only the password is a credential (`middleware.go:14`), so the audit records *that a curated change was made through the admin surface*, not *who made it*. BR-AS23 states this rather than letting an `actor` column imply an identity the system cannot produce. Real operator identity is WorkOS-backed and already deferred; the audit table's shape does not change when it lands. |
| 38 | **The read stays gated** (Q2) | `GET /frontend-plugins` remains behind BasicAuth, reached in dev through each app's Vite proxy. Unchanged from today; noted because a write path is the moment someone re-opens it. |
| 39 | **A new hexagonal module, leaving `accounts` alone** (Q3) | `accounts-service/registry/` with `composition.go` and `internal/{domain,postgres,kvcache,rest}`, plus `cmd/seed-registry/`, following `pricing-service/pricing/`. The flat `accounts` package is not restructured as part of this phase — decision 32 introduces the pattern *to the service*, it does not retrofit it. |
| 40 | **`BUSINESS_RULES-APP-SHELL.md` owns BR-AS16–AS24** (Q4) | The rules are about the shell's registry contract, whichever service happens to host it; keeping them together survives the Phase 6 move. `BUSINESS_RULES-ACCOUNTS.md` gets a cross-reference stub so someone reading the service's rules finds them. |
| 41 | **KV holds one whole document** (Q9) | Bucket `registry` in the platform account, key `_platform.frontend-plugins.current`, value the entire serialized document. The read is all-or-nothing, so per-entry keys would buy nothing and add a torn-read window between them. Naming follows CLAUDE.md: `lowercase-kebab` bucket, context-prefixed key. |
| 42 | **Disable, never delete** (Q10) | No `DELETE` route. An entry is disabled, which withholds it from the read and leaves the row and its audit trail intact. This also keeps the removal case — decision 25's reload — reachable and testable without destroying history. |
| 43 | **The origin allowlist is `REGISTRY_ALLOWED_ORIGINS`, per deployment** (Q12) | An env var read at start, not an editable row, following the precedent `config.go:10-25` already sets: an envelope that *is* the business rule belongs in configuration or code, never behind a runtime toggle a compromised write path could widen. Production simply omits localhost rather than special-casing it. |
| 44 | **The shell re-reads on focus, plus a slow interval** (Q13) | `visibilitychange` (hidden → visible) and a ~10-minute interval both fire decision 27's conditional GET. **This is new behaviour, not a description of existing code:** `fetchRegistry()` has exactly one caller today (`bootShell.js:104`), with no interval, no visibility handling and no ETag anywhere in `lab-shell/src`. Without it BR-AS19's "becomes visible to a running shell" would be false as written. `If-None-Match` makes the common case a 304 that costs nothing. |
| 45 | **`notify.*` is published now, with no consumer, and never fails the write** (Q14) | Publish is the last step, after the Postgres commit and the KV write. The commit has already happened by then, so a publish failure is logged and the write still succeeds — KV and `notify` are both derived from the committed row, and neither may retroactively refuse it. No subscriber exists in this phase; the subject is visible in the Admin Pub/Sub panel and is the anchor for any later push channel. |
| 46 | **All four screens ship in this phase** (Q15) | Registry list, entry drawer (with the stale-revision and origin-refused refusals), the shell's reload banner, and the audit panel. Every decision from 22–45 then has a surface that demonstrates it, and the registry is usable without a follow-up phase. The mockups for all four already exist. |

#### Business rules — BR-AS16 to BR-AS24 (confirmed at the gate, 2026-08-28)

- **BR-AS16 — The registry is service state.** The shell's registry response is served from
  `accounts-service`'s own store. A curated entry added or removed through the admin surface is
  visible to a newly booting shell **without restarting any service**. *Failure:* an entry changed
  through the admin surface is still absent from a fresh boot's response.
- **BR-AS17 — Revision is server-assigned and monotonic.** Every response carries a `revision` the
  server assigned; it increases on every accepted write and never repeats. *Failure:* two different
  documents are served under one revision.
- **BR-AS18 — Writes are revision-checked.** A write carrying a stale revision is refused, not
  merged. *Failure:* two concurrent writes both succeed and one silently loses.
- **BR-AS19 — A registry change notifies, and never unloads.** A revision change is published on
  `notify._platform.registry.frontend-plugins.changed` for service-side consumers, and becomes
  visible to a running shell through a conditional read of the registry endpoint (decision 36 — the
  shell holds no NATS connection in this phase). A shell with an active plugin whose entry was
  removed keeps rendering it and offers a reload. The read is triggered on window focus and on a slow
  interval (decision 44). *Failure:* a running plugin is torn down under the user, or the change is
  silent until the next boot.
- **BR-AS20 — Origin allowlist, enforced on write and on read.** A registry entry whose remote URL
  is not on the service's configured origin allowlist is refused at write time **and** withheld at
  read time. The read-side check is not redundant: narrowing the allowlist in configuration leaves
  already-stored rows non-conforming, and that is the case the write-time check cannot cover.
  *Failure:* the shell is offered a remote on an unconfigured host — including after an allowlist
  was narrowed.
- **BR-AS21 — No self-registration.** No transport permits a plugin to add, modify or enable its own
  registry entry. *Failure:* an entry appears that no operator wrote.
- **BR-AS22 — The registry degrades, it does not fail.** With Postgres unavailable the endpoint falls
  back to the KV cache; with both unavailable it answers `200` with an empty plugin list, `revision:
  0` and `degraded: true`, and the shell renders its built-ins. It never answers `5xx` and never
  serves a plugin list it cannot attest to. *Failure:* a registry outage produces a blank shell, or a
  degraded response is indistinguishable from a genuinely empty registry.
- **BR-AS23 — The audit records the surface, not an identity.** Every accepted and every refused
  write appends an audit row whose actor is the shared administrative identity the request
  authenticated as. The rule is deliberately weaker than "who did it": while `accounts-service`
  authenticates every request as one shared secret, no stronger claim is true, and the audit must not
  imply one. *Failure:* the audit displays a per-operator identity the authentication cannot
  establish.
- **BR-AS24 — An entry is disabled, never deleted.** No transport removes a registry row. A disabled
  entry is withheld from the read and its history is retained. *Failure:* a curated entry and its
  audit trail can be destroyed through the admin surface.

**Gate — PASSED 2026-08-28.** Design decisions 22–46 and BR-AS16–BR-AS24 confirmed by the user
across fifteen questions. The phase may be broken into tasks; rules go into
`BUSINESS_RULES-APP-SHELL.md` and specs are derived from them before implementation.

#### Sub-phases — 2a, 2b, 2c (split 2026-08-28)

The phase divides on *what kind of code changes*, and the seam between the three is decision 35's
interface. **2a is blocking** — everything else reads its endpoints — and **2c is the risky one**,
because it is the only part that retires an invariant the shipped shell relies on ("the contribution
registry and the router are built once at boot", decision 26). Isolating 2c keeps that retirement
from hiding inside a change that also touches Go.

| | Scope | Touches | Rules provable here |
| --- | --- | --- | --- |
| **2a** | The module and its store — no UI at all | `accounts-service/registry/`, migrations, `cmd/seed-registry`, endpoint move | BR-AS16, AS17, AS18, AS20, AS21, AS22, AS23, AS24 |
| **2b** | The admin surface | `frontend/admin/src` only, against 2a's endpoints | AS18 and AS20's refusals, AS23's audit, AS24's disable — *as surfaces* |
| **2c** | The shell notices a change | `lab-shell/src` only | BR-AS19 |

##### 2a — the module and its store

- [x] `accounts-service/registry/` as a new hexagonal module — `composition.go` plus
      `internal/{domain,postgres,kvcache,rest}`, following `pricing-service/pricing/` (decision 39).
      The flat `accounts` package is not restructured (decision 32's note).
- [x] Decision 35's interface first, before the store changes: `Current(ctx) (Document, error)` and
      `Apply(ctx, Write) (Document, error)`, with revision assignment, the origin check, the KV
      write-through, the audit append and the `notify` publish all behind them. `Apply` installs the
      entry set and the revision together — the defect the two setters
      (`SetCuratedFrontendPlugins` / `SetCuratedFrontendRevision`) allow today.
- [x] Postgres migrations: the entry table, the monotonic revision, and the append-only audit table
      in the registry's own schema (decision 33 — no join to an accounts table, either direction).
- [x] KV write-through cache: bucket `registry`, key `_platform.frontend-plugins.current`, one whole
      serialized document (decision 41). Read order Postgres → KV → degraded (decision 30).
- [x] `REGISTRY_ALLOWED_ORIGINS` read at start (decision 43), enforced on write **and** on read.
- [x] Endpoint move to `/api/platform/registry/frontend-plugins`, clean break (decision 34). The
      proxy prefix is shared, so this is a **new proxy rule** in `lab-shell/vite.config.js`,
      `frontend/admin/vite.config.js` and `frontend/admin/nginx.conf` (each rewriting
      `/api/platform` → `/api`), plus the shell's `REGISTRY_ENDPOINT` constant — not an edit to the
      existing `/api/platform/accounts` rules, which other routes still need.
- [x] Remove `GET /api/accounts/frontend-plugins` from `accounts/handler.go:316` and from
      `handler_allowlist_test.go:41`; the new module gets its own route-allowlist test.
- [x] Remove `FRONTEND_PLUGIN_REGISTRY_FILE` and the boot-time file read (decision 24), and delete
      `frontendplugins_file_test.go` with it.
- [x] `cmd/seed-registry` — reads `registry.dev.json`, writes over REST against a running service,
      never at process start.
- [x] `notify._platform.registry.frontend-plugins.changed` published last, after the commit and the
      KV write, and never failing the write (decisions 36, 45).
- [x] No `DELETE` route anywhere in the module (decision 42 / BR-AS24).

##### 2b — the admin surface

- [x] One nav key under the existing **Platform** group in `frontend/admin/src/App.vue` beside
      `settings` — `frontend-shell`. The admin app has no router; it is a grouped activity bar with
      `activeView` and `v-else-if` sections, so this follows that pattern rather than introducing
      one. It shipped as two keys (`frontend-plugins`, `registry-audit`) and was collapsed to one
      afterwards: the catalog and its write history are one subject read two ways, and two rail
      entries for it crowded the group.
- [x] `FrontendShellView.vue` — the two readings as tabs, `Plugins` then `Registry Audit`, in the
      `AccountsView.vue` idiom (PrimeVue `Tabs`, the active tab held in the `ui` store as
      `frontendShellTab` so it survives navigating away, and `v-if` on each panel so the idle tab
      does not poll). The page subtitle is per-tab, like `ACCOUNTS_SUBTITLES`.
- [x] `FrontendPluginsPanel.vue` — the registry list (mockup `body-Main.html`).
- [x] The entry drawer (mockup `body-EntryEditor.html`), with both refusal panels: stale revision
      (`body-StaleRevision.html`, BR-AS18) and origin-not-allowlisted (BR-AS20).
- [x] `RegistryAuditPanel.vue` — the audit trail (mockup `body-AuditTrail.html`), actor column
      showing the shared `admin` identity and nothing stronger (BR-AS23).
- [x] Disable/enable as the only lifecycle control; no delete affordance (BR-AS24).
- [x] `api.js` calls against `/api/platform/registry/...`, and specs in the `*Panel.spec.js` idiom
      the admin app already uses.

##### 2c — the shell notices a change

- [x] `If-None-Match` on `fetchRegistry()`, and a `304` result the caller can distinguish from a
      fresh document. Nothing in `lab-shell/src` sends or stores an ETag today.
- [x] A re-read trigger: `visibilitychange` (hidden → visible) plus a ~10-minute interval
      (decision 44). `fetchRegistry()` has exactly one caller today (`bootShell.js:104`); this is
      new behaviour, not a rewiring of existing behaviour.
- [x] Handle `degraded: true` — the shell renders its built-ins and says the registry is degraded.
      No `degraded` handling exists anywhere in `lab-shell/src` today (BR-AS22).
- [x] Incremental `contributionRegistry.index()` (it appends today, so re-indexing the same set
      duplicates) and runtime `router.addRoute`, so an *addition* can be placed live (decision 26).
- [x] The reload banner for removals and URL changes — offered, never applied (decision 25 /
      BR-AS19). A plugin already `active` keeps rendering.

---

### Phase 3 — APPROVED (gate passed 2026-08-30) — Live-change correctness and the write boundary (Phase 2c hardening)

**Why this is a phase and not a bug list.** Five defects found by an external review pass (two
independent reviewers, 2026-08-29), each verified against shipped code rather than accepted as
reported. Three of them share one root — 2c retired "the shell is built once at boot" (decision 26)
without giving the shell either a reactive substrate or lifecycle rules for its non-200 reads — and
fixing them separately would mean touching `bootShell`/`contributionRegistry` three times with three
partial models of what a re-read means. The other two are independent: one in 2a's store, one in the
shell's transport. **Phase 2 is not complete while 2c's headline capability does not reach the
screen**, so this closes 2c rather than opening new ground; it adds no feature and no new surface.

**Problem.** As shipped, 2c can place an addition into the registry object but not into the rendered
shell; it applies a change model coarser than the document is mutable; and it cannot leave the
degraded state it enters. Separately, 2a's store can report a committed write as refused — writing a
false row into the one artifact whose honesty decision 31 sells as the feature — and the shell's dev
proxy hands every federated plugin the credential that authorises registry writes, which is the exact
thing decision 29 forbids.

**Scope.** Five fixes, the business-rule amendments they imply, and the specs the current suites lack
(all five are green today precisely because nothing mounts a shell, interleaves a write, or asserts
on the transport surface).

**Explicitly out of scope.** The sixteen further findings from the same review pass, recorded below
under "Deferred findings" for a follow-up pass once this phase lands. Nothing here opens Phase 6's
publishing lifecycle, Phase 10's permission claims, or any plugin unloading.

#### Design decisions — 46–52, PROPOSED

| # | Decision | Rationale |
| --- | --- | --- |
| 46 | **A new id is the only change that may be applied live. Every other difference is a reload offer.** | Amends decision 26, which named a three-way taxonomy — addition / removal / changed remote — that the write path does not respect: `ON CONFLICT DO UPDATE SET enabled, entry` replaces the *whole* entry, so label, order, `routePrefix`, `permission`, `version`, `enabled` and `remote.name` are all mutable and all invisible to `remoteOf()`. The failure is worse than staleness: a transaction that edits A and adds B applies only B, leaving the shell holding a catalog that existed at **no revision** — which contradicts the one-snapshot-per-session guarantee in `ARCHITECTURE-APP-SHELL.md`. The diff becomes deep equality over the validated manifest. The alternative — declaring a fixed immutable subset of entry fields and enforcing it server-side — is rejected: it constrains curation forever to buy a diff optimisation on a document read a few times a day. |
| 47 | **Contribution state is reactive at its source, not at its readers.** | `contributionRegistry` holds `routes`/`navigation`/`extensions`/`shellControls`/`footerItems` as plain closure arrays and `bootShell` holds `statuses` as a plain `Map`; only the individual `PluginStatusRecord`s are reactive. So `App.vue`'s `computed(() => shell.contributions.navigation)` evaluates once with **zero reactive dependencies** and is never invalidated — a live-added plugin gets its router record and nothing else. The fix belongs at the source (`reactive([])`, `reactive(new Map())`) rather than at each reader: the `[...array]` getters then track correctly by construction, and no future reader has to know the rule. This is the decision that makes decision 26 — and therefore BR-AS19's "becomes visible to a running shell" — true rather than asserted. |
| 48 | **A read the service will not vouch for invalidates the shell's conditional token.** | Degraded is currently a one-way door, and the trace crosses four files: no ETag on a degraded response (`rest.go:79`), so the client returns `etag: null`, so `registry.etag = discovery.etag ?? registry.etag` keeps the pre-outage token (`bootShell.js:137`), and the watcher only advances `lastEtag` on `ok && etag`. Recovery at the same revision therefore answers `304`, and `applyRegistry` returns at the `unchanged` guard — one line *above* where `registry.degraded` is assigned. Two rules: observing `degraded` (or a failed read) clears the token so the next read is unconditional, and `registry.degraded` is cleared by any successful read including a `304`. The second half matters on its own: a `304` is positive evidence the service is answering. |
| 49 | **A committed write is never reported as refused.** | `apply()` commits and then returns `s.Current(ctx)` on the *request* context (`store.go:148`); a cancellation or read failure in that window makes `Apply` return an error, whereupon the wrapper audits a **refusal for a write that is durably committed**, answers 500, and skips both the KV refresh and the notify. The document a write installed is knowable inside the transaction, so the post-commit read goes away. Whatever post-commit work remains (cache, notify) runs on a context detached from the request and can log but never become the caller's error — and `auditRefusal` fires only on paths that provably did not commit. |
| 50 | **The shell's origin carries the read capability only.** | Federated plugin code executes in the shell's JavaScript realm, so any credential the shell's origin can use is a credential every loaded plugin holds. Today `lab-shell`'s Vite proxy injects the shared admin BasicAuth for the whole `/api/platform/registry` prefix — which contains `POST /entries` and `POST /entries/{id}/enabled` — so a plugin can self-register or self-enable with one `fetch()`. Decision 29 ("self-registration stays prohibited, by any transport") has to be a property of the transport, not of plugin good behaviour. **Recommended shape:** mount `GET /api/registry/frontend-plugins` ungated and drop the credential injection from `lab-shell`'s proxy entirely, keeping the four admin routes behind BasicAuth in the admin app's proxy. The document is already allowlist-filtered for exactly this reader (BR-AS20), it is the boot document every browser must fetch, and `auth-service`'s handlers set the ungated-route precedent in this same binary. The alternative — a second read-only credential — is available if the gate wants the endpoint to stay authenticated. |
| 51 | **Each fix ships with the spec class that could have caught it.** | All five defects live in the gaps between the existing suites, not inside them: nothing mounts the shell and asserts on rendered nav after a live addition, nothing drives degraded → recover-at-the-same-revision, nothing interleaves a failure between commit and read, and nothing asserts what the shell's origin can reach. Four new spec classes, not four new assertions in existing files. |
| 52 | **Two business rules are amended and one is added.** | BR-AS19 restated to decision 46's rule (only a new id is live-appliable; the degraded/ETag lifecycle is part of the rule, not an implementation detail). BR-AS02's "contribution failure is contained" note narrowed to what the code enforces. New **BR-AS25 — the shell's origin holds no registry write authority**, testable by asserting the surface the shell can reach carries no write route. |

#### Gate answers — 2026-08-30

1. **Decision 50's shape — ungate the read route.** `GET /api/registry/frontend-plugins` is mounted unauthenticated and `lab-shell`'s proxy drops its credential injection entirely; the four admin routes stay behind BasicAuth in the admin app's proxy. The document is already allowlist-filtered for exactly this reader (BR-AS20), it is the boot document every browser must fetch, and `auth-service` sets the ungated-route precedent in the same binary. Phase 4 later replaces the transport, not the boundary, so nothing here is thrown away.
2. **Decision 46's blast radius — accepted as scoped.** Any difference that is not a new id is a reload offer, including a corrected nav label. Correct by construction, and honest about what the shell can apply to itself; curation is a rare operator act on a document read a few times a day, so the cost is bounded and visible rather than subtle. Revisited if Phase 5's dynamic class makes an edit absorbable as withdraw-then-add.
3. **Decision 47's boundary — reactive at the source.** `contributionRegistry` holds `reactive([])` and `bootShell` a `reactive(new Map())`. The `[...array]` getters then track by construction and no future reader has to know the rule. `bootShell` already wraps `PluginStatusRecord` in `reactive()`, so the precedent exists; the module header is amended to claim framework independence for the **state machine**, not for the container that holds it.
4. **Sequencing — one phase.** Splitting the backend fixes from the shell fixes would only help if something were waiting on half of it, and nothing is.

#### Tasks — gate passed; ready to start

##### 3a — the shell's live-change model (decisions 46, 47, 48)
- [x] `registryDiff.js` — deep equality over validated manifests; `reloadRequired` gains the reason for an edited entry.
- [x] `contributionRegistry.js` / `bootShell.js` — reactive contribution arrays and status map per decision 47.
- [x] `bootShell.applyRegistry` — clear `degraded` on any successful read including `304`; clear the ETag on degraded or failed reads. `registryWatcher` learns the same rule for `lastEtag`.
- [x] Specs: a **mounted** shell that renders a live-added plugin's nav entry, inventory row and extension placement; an edited-entry reload offer; degraded → recover-at-same-revision.

##### 3b — the store's write path (decision 49)
- [x] `postgres.Store.apply` returns the installed document from inside the transaction; the post-commit `Current` is removed.
- [x] `auditRefusal` reachable only from paths that did not commit; post-commit work detached from the request context.
- [x] Specs: a write whose post-commit step fails is reported as accepted, audited as accepted, and leaves no refused row.

##### 3c — the write boundary (decision 50)
- [x] Split the shell's proxy from the admin's; the shell's origin reaches no write route.
- [x] Spec: assert the route surface the shell can reach (the `Mount` return list already exists to make this assertable).

##### 3d — rules and docs (decision 52)
- [x] `BUSINESS_RULES-APP-SHELL.md` — amend BR-AS19 and BR-AS02, add BR-AS25.
- [x] `ARCHITECTURE-APP-SHELL.md` — the one-snapshot-per-session guarantee restated to match decision 46.

#### Deferred findings — reviewed 2026-08-29, not in this phase

Sixteen further items from the same review pass, set aside deliberately and worth a second look once
this phase lands. **Ten are real but below the line:** the KV revision regression (an unconditional
`Put` on every read lets a delayed read of N overwrite a cached N+1; wants revision-aware CAS plus a
monotonic guard shell-side); the ETag covering only the revision and not the allowlist-filtered
representation; a refused status record blocking later re-admission of the same plugin id; manifest
validation rejecting a whole plugin on its first bad contribution (contradicting the within-plugin
isolation the module's own header claims); shallow-frozen extension context; extension-point capacity
winners chosen before configured ordering; malformed bodies and bad `If-Match` refused before the
audit seam; route-load retry resolving the router-cached error component; ETag matching being
exact-match only (no `W/`, no `*`); and `Store.Current`'s two-statement read (**downgraded** — the
two reviewers disagreed and the code supports the milder reading: the revision is read before the
entries, so the only reachable skew is revision N with entries ≥ N, and the write path re-reads the
revision under `FOR UPDATE`, so concurrency control never depends on `Current`'s pairing; cost is at
most a one-poll delivery delay, and the asymmetry deserves a doc line, not a fix). **Six are owned by
a later phase or are not defects:** BR-AS05's grant-all permission evaluator (Phase 10); the plugin
lifecycle contract's missing `activate()` argument, disposers, deactivation and connection
refcounting (Phase 3+ of the original plan, i.e. reviewed against a contract that has not shipped
yet); the federation proof sharing only Vue rather than the full singleton set (proof breadth, load-
bearing only at first migration); BR-AS02 overstating runtime containment (a wording change, folded
into decision 52 rather than deferred); SRI/CSP/immutable assets (Phase 6, already stated as
production requirements); and the registry-in-`accounts-service` coupling (priced, with Phase 6 as
the escape hatch).

---

### Phase 4 — PROPOSED (design gate not passed) — The shell's NATS transport and push change propagation

**Why this is a phase.** Phase 2c gave the shell a change *model* and Phase 3 makes it reach the
screen, but the shell still learns about change by polling every ten minutes over HTTP. The
requirements pass of 2026-08-30 settled that the shell should hold a NATS connection like every
other app in the repo, and that registration changes and connection changes should both arrive as
push. This phase moves the transport and nothing else: no new registration path, no lifecycle
change, no signing.

**Problem.** Three things, all transport. The shell has no NATS connection at all —
`connections/connectionRegistry.js` is credential-profile state with nothing behind it, built for a
migration that has not happened. `notify._platform.registry.frontend-plugins.changed` is published
on every accepted write and **subscribed by nobody**, so a curation decision takes up to ten minutes
to reach an open shell. And the read is an HTTP GET through a dev proxy, which is why decision 50
existed in the first place.

**Scope.** The shell's NATS connection and credential mint; the registry read as an `api.*`
request/reply; a subscription to the existing notify subject; the reconnect resync rule; deletion of
`registryWatcher.js`; the request/reply equivalents of the conditional-read and optimistic-concurrency
vocabulary the HTTP surface carries today.

**Explicitly out of scope.** Connection *presence* as a health signal (Phase 5 — health has no meaning
until entries carry a lifecycle class). Moving the admin write routes off REST. Anything about
plugins registering themselves.

#### Design decisions — 53–58, PROPOSED

| # | Decision | Rationale |
| --- | --- | --- |
| 53 | **The shell holds a long-lived NATS connection, minted the way the other three apps already mint theirs.** | The Admin, Refdata and Seafreight apps have a working credential path; a fourth invented for the shell would be a second trust story for the same browser. `shared/natsconn`'s `MaxReconnects(-1)` rule applies: nats.go's default of 60 attempts then *closes* the connection permanently, which for a shell means a session that silently stops hearing about anything. |
| 54 | **The boot read stays off the critical path: built-ins are indexed, the shell paints, and only then does it connect and read.** | First paint must stay bounded by the shell's own bundle (BR-AS08, BR-AS04). Connecting first would put credential minting — a network round trip that can fail — in front of every pixel, and BR-AS22's degraded story would have to be rewritten to cover "no shell at all". The curated document arrives one step later than it does today and nothing about the rendered result differs. |
| 55 | **The notification is a hint; the read is authoritative.** | The `notify.*` message carries the new revision and nothing else the shell trusts. The shell answers with a conditional read and no-ops when the revision already matches what it holds. A shell that acted on the message body would be applying a document it never read, from a subject with no delivery guarantee — and would have two code paths that can install a catalog, only one of which is the one Phase 3's diff was written against. |
| 56 | **On every reconnect the shell re-reads unconditionally.** | Core NATS is fire-and-forget: a shell that is disconnected, backgrounded or reconnecting misses messages with no gap detection and no redelivery. Without this rule a shell offline for one minute is stale forever, which is strictly worse than the poll it replaces — the poll at least self-heals. This is the single rule most likely to be forgotten and the hardest to notice in testing, so it is a business rule rather than an implementation note. |
| 57 | **`registryWatcher.js` is deleted, not retained as a fallback.** | A poll kept "just in case" beside a push channel means two paths that can install a document, divergent conditional-read state (`lastEtag` vs the subscription's revision), and a failure mode nobody exercises because the poll quietly covers it. Decision 56 is what makes the fallback unnecessary; keeping both would make decision 56 untestable. |
| 58 | **The conditional-read and concurrency vocabulary is re-implemented in the payload, deliberately.** | ETag/`If-None-Match`/304 and `If-Match`/409/428 are doing real work today and none of it is free over request/reply. Revision is already a domain concept, so the payload carries it explicitly: a read states the revision it holds and may be answered "unchanged"; a write states the revision it saw and may be refused as stale or as missing. This is re-implementation, not deletion, and it is the part most likely to acquire subtle bugs — so it ships with its own spec class. |

#### Business rules — BR-AS26 to BR-AS29 (draft, to be confirmed at the gate)

- **BR-AS26 — The shell's registry read is a NATS request/reply on an `api.*` subject.** The browser
  never calls `rpc.>`. The subject the shell can reach carries read capability only; no write
  subject is in its permission set (extends BR-AS25 from proxy shape to transport shape).
- **BR-AS27 — A change notification is a hint, never a payload.** The shell reacts to
  `notify._platform.registry.frontend-plugins.changed` by performing a conditional read. It never
  installs a catalog from the message body, and a message whose revision matches what the shell
  holds changes nothing.
- **BR-AS28 — A reconnected shell re-reads unconditionally.** On every reconnect the shell performs
  an unconditional read and reconciles by revision, because messages published while it was
  disconnected are not redelivered.
- **BR-AS29 — First paint precedes the connection.** The shell renders its built-ins before it
  connects or reads. A shell that cannot connect, cannot mint a credential, or is answered with a
  degraded document still renders (BR-AS22 unchanged in effect, restated for the new transport).

#### Gate questions

1. **Credential scope** — does the shell get its own credential profile, or reuse
   `operator-refdata-platform`? A dedicated profile is one more nsc user and one more `.creds`
   filename; reuse means the shell's subject permissions widen whatever that profile already holds.
2. **Do the admin write routes move in this phase or stay REST?** Moving them is where subject
   permissions genuinely earn their keep; leaving them keeps the phase to one transport change.
3. **What does the shell do while unconnected but painted** — retry silently, or surface a
   connection state in the footer beside the revision?

#### Tasks — not started; this phase is PROPOSED

> No code, specs or business-rule edits until the gate passes (AI Agent Workflow step 2).

##### 4a — the connection
- [ ] The shell's NATS WebSocket connection and credential mint, following the existing three apps.
- [ ] `connectionRegistry.js` gains the transport it was designed as a placeholder for.
- [ ] Specs: a shell that cannot connect still renders its built-ins and reports why.

##### 4b — the read
- [ ] `registryClient` gains an `api.*` request/reply path; the revision-conditional read moves into the payload.
- [ ] Service side: the read subject and its handler, answering unchanged / document / degraded.
- [ ] Specs: unchanged, changed, and degraded answers over the new transport; the 428/409/422 equivalents.

##### 4c — push
- [ ] Subscribe to `notify._platform.registry.frontend-plugins.changed`; a message triggers a conditional read.
- [ ] Reconnect handler performs an unconditional read (decision 56).
- [ ] Delete `registryWatcher.js` and its specs; move what they proved onto the subscription.
- [ ] Specs: a message whose revision matches is a no-op; a shell that missed a message while disconnected converges on reconnect.

##### 4d — rules and docs
- [ ] `BUSINESS_RULES-APP-SHELL.md` — BR-AS26 to BR-AS29; BR-AS19's "triggered on window focus and on a slow interval" restated as push.
- [ ] `ARCHITECTURE-APP-SHELL.md` — the read contract restated for request/reply; `ARCHITECTURE-COMMUNICATIONS.md` gains the new `api.*` subject.

---

### Phase 5 — PROPOSED (design gate not passed) — Two lifecycle classes, withdrawal, and health

**Why this is a phase.** BR-AS19's "notify, never unload" was written when every plugin was
permanent, and the code is built on that assumption: `contributionRegistry`'s arrays are append-only
behind an `indexedPluginIds` set, and the status machine has no transition out of `active`. The
requirements pass of 2026-08-30 settled that this stays true for plugins an operator placed, and
stops being true for plugins a service announced. Splitting the classes is what makes withdrawal
affordable — the expensive semantics are paid for only where they are needed.

**Problem.** Three behaviours have nowhere to live today. An operator can disable a plugin and the
shell keeps rendering it until reload, with no way to express anything else. A plugin whose backing
service is down looks identical to one that is healthy. And there is no concept of a plugin that may
legitimately leave.

**Scope.** A stored lifecycle class on each entry; a plugin scope that tracks what a plugin
registered so it can be withdrawn; the tombstone view; the health overlay and its debounce; live
enable/disable for the dynamic class.

**Explicitly out of scope.** Full `scope.dispose()` — this phase removes contributions and leaves the
federated module resident (see decision 62). Any way for a plugin to *become* dynamic; every dynamic
entry in this phase is one an operator marked by hand, which is the seam Phase 8 later fills.

#### Design decisions — 59–65, PROPOSED

| # | Decision | Rationale |
| --- | --- | --- |
| 59 | **The lifecycle class is stored on the entry, not inferred.** | `diffRegistry` branches on `remote.kind === 'builtin'` today; the new branch is "may this removal be applied, or must it be offered". Deriving that from provenance ("did this come from preload") leaves the shell guessing at the one moment it must not, and makes the rule invisible in the document an operator reads. An explicit `lifecycle: 'static' | 'dynamic'` is also the thing a spec can assert on. |
| 60 | **Static keeps today's semantics exactly.** | No transition out of `active`; a removal or an edit is a `pendingReload` offer; disable takes effect on the next reload and the Admin panel says so. This is not a compromise pending better machinery — it is the correct behaviour for a plugin an operator placed deliberately, and it keeps the shell's degraded floor (BR-AS22) intact, because the static set is what renders when the registry cannot be read. |
| 61 | **Only an explicit unregister or an operator disable withdraws a plugin. Disconnection never does.** | Presence tells you a *backend service* is reachable. Treating that as the plugin's existence means a `docker compose restart` yanks a feature out from under whoever is using it, and a slow deploy silently removes it and puts it back — nav items appearing and vanishing on their own. Health is an overlay on `active`, not a state beside it. |
| 62 | **Withdrawal removes contributions; the loaded module stays resident.** | A federated container cannot be un-evaluated from the JavaScript realm — dropping references is all any implementation can do — so full disposal buys memory hygiene in a long session, not a cleaner UI. Contribution removal is roughly a fifth of the work, has no leak surface to get wrong, and delivers every visible behaviour: the nav item goes, the route stops resolving, the occupant is told. Full `scope.dispose()` stays a later question, not a promise made in a shipped contract. |
| 63 | **A withdrawn route stays resolvable for exactly one occupant, and renders a shell-owned tombstone.** | Redirecting a user who is reading a page moves them without warning and loses anything unsaved with no explanation. The tombstone replaces the view in place — this feature was withdrawn, with a link back — while the nav item goes immediately and the route stops resolving for new navigations. It is more work than a redirect and it is the only option where disposal is deterministic *and* the user stays in control. |
| 64 | **The health signal is debounced, and is decoration only.** | A dot that flickers on every transient failure trains people to ignore it, which is worse than no dot. The nav item is never removed, reordered or greyed to the point of looking disabled — deployment availability controls presence, operational health controls appearance, and that line is the one the AWS Console prior art is most insistent about. |
| 65 | **Failure surfaces stay inline; a dialog is reserved for a failure the user's own action caused.** | The shell already has the better pattern: `PluginSlot` and `ExtensionRegion` contain a failing contribution as a card beside healthy siblings, and `PluginErrorView` names the plugin and cause at route level. A modal for an unsolicited background failure interrupts work the user was doing for something they cannot act on. A user who clicked into a plugin that will not load is a different case and deserves an answer at the point of the click. |

#### Business rules — BR-AS30 to BR-AS34 (draft, to be confirmed at the gate)

- **BR-AS30 — Every entry carries a lifecycle class.** `static` or `dynamic`, stored on the entry and
  served in the document. The shell applies removal semantics by class and never by inference.
- **BR-AS31 — A static plugin is never withdrawn from a running shell.** Its removal, edit or
  disable is offered as a reload and never applied (BR-AS19, unchanged, now scoped to the class).
- **BR-AS32 — A dynamic plugin's withdrawal removes its contributions.** Routes, navigation,
  extensions, shell controls and footer items registered by that plugin are removed from the running
  shell. The plugin's loaded module is not required to be unloaded.
- **BR-AS33 — Withdrawal never relocates the user.** A user occupying a withdrawn plugin's route is
  shown a shell-owned tombstone in place. The route resolves for that occupant and for no new
  navigation.
- **BR-AS34 — Backing-service health decorates, it never removes.** An unreachable backing service is
  reported beside the plugin's nav entry and in the Plugins inventory. It does not remove, reorder or
  disable any contribution, and the report contains no URL, host, port or credential (BR-AS04).

#### Gate questions

1. **What actually reports health?** NATS presence of the backing service, or a liveness check the
   shell performs against the remote? These answer different questions — a plugin whose service is up
   but whose `remoteEntry.js` 404s is healthy by the first measure and broken by the second.
2. **Debounce window** — a fixed interval, or "unavailable only after a failed retry"?
3. **Does a static plugin ever show the dot?** It has a backing service too, and the argument for
   showing it is the same; the argument against is that the operator cannot act on it without a
   deploy.

#### Tasks — not started; this phase is PROPOSED

> No code, specs or business-rule edits until the gate passes (AI Agent Workflow step 2).

##### 5a — the class
- [ ] `lifecycle` on the entry: domain, store, document, admin surface, manifest schema.
- [ ] `registryDiff.js` branches on class: static removal offers, dynamic removal withdraws.
- [ ] Specs: the same removal produces a reload offer for one class and a withdrawal for the other.

##### 5b — the scope
- [ ] `contributionRegistry` learns to remove a plugin's contributions; the append-only invariant becomes append-and-withdraw, guarded by the same id set.
- [ ] `pluginStatus` gains `withdrawn`; the state machine's transitions are stated for both classes.
- [ ] Specs: a mounted shell loses the nav entry, the route and the extension placement of a withdrawn plugin, and keeps every sibling.

##### 5c — the occupant
- [ ] The tombstone view and the router rule that keeps a withdrawn route resolvable for its occupant.
- [ ] Specs: a user on the withdrawn route sees the tombstone; a fresh navigation to it does not resolve.

##### 5d — health
- [ ] The health overlay, its debounce, and its rendering beside the nav entry and in the inventory.
- [ ] Specs: a flapping service produces one state change, not many; an unavailable plugin keeps its contributions.

##### 5e — rules and docs
- [ ] `BUSINESS_RULES-APP-SHELL.md` — BR-AS30 to BR-AS34; BR-AS19 scoped to the static class.
- [ ] `ARCHITECTURE-APP-SHELL.md` — the two state machines and the health overlay.

---

### Phase 6 — CANDIDATE (not opened) — Plugin Registry Service and publishing lifecycle (the Grafana shape)

Stub, recorded so the idea is not lost. A dedicated platform registry service, separate from
`accounts-service`, owning the plugin **publishing lifecycle**: upload → sign → verify → stage →
promote → deprecate/delist. This is Grafana's model, where every plugin is cryptographically signed
to be loadable at all and the catalog owner may delist for security, quality or compatibility —
the centralized-governance end of the spectrum, as against Backstage's decentralized one, and the
end this platform's tenants want.

**Triggers that would justify opening it**, none of which hold today: frontends outnumbering the
three or four in this repo; plugin publishing acquiring a lifecycle of its own (a build produced by
a team that does not operate the platform); several products sharing one catalog; or an external or
semi-trusted plugin author appearing, at which point signing stops being optional.

Deliberately a destination rather than a starting point. The shell's read contract does not change
between Phase 2 and Phase 6 — the server behind the endpoint does — so everything worth learning
about curation, revisioning, propagation and integrity can be learned inside Phase 2 first, and
Phase 6 is then about the lifecycle, which is the part that actually needs the separate service.
Per-tenant curation (deferred by Design decision 22) is the other candidate for this phase.

Phase 2's decisions 32–34 exist to make this phase small when it opens: the registry is already its
own bounded-context module owning its own tables, and the shell already reads a capability-named
path (`/api/platform/registry/frontend-plugins`), so opening Phase 6 is a `main.go` plus routing
change rather than a client change across every frontend.

---

### Phase 7 — PROPOSED (design gate not passed) — Publisher signing and the trust table

**Why this is a phase, and why it comes before Phase 8.** The requirements pass of 2026-08-30 asked
for plugins to be digitally signed so no unauthorised MFE can be injected, and settled that a service
may announce itself only if it can prove who it is. Unsigned announcements are refused by rule, so
the trust machinery has to exist before the announce path does. It is also the phase with the one
schema change that is cheap now and expensive later.

**Problem.** Trust today rests entirely on the origin allowlist and on the fact that only an operator
can write to the registry. That is sufficient while every entry is placed by hand and insufficient
the moment anything else can write. There is no notion of *who authored a manifest*, and the store
cannot represent one: the service holds columns and reassembles the document on read, and any
reassembly invalidates a signature.

**Scope.** Signed manifest bytes stored verbatim with the columns as a derived projection; NKey
signature verification in the domain layer; a curated trusted-publishers table with the registry's own
revision, audit and disable-never-delete semantics; the revocation fan-out.

**Explicitly out of scope.** Signing the *code* — that is Phase 6's immutable signed bundles, and this
phase must not be described as if it delivered it. Browser-side verification. Any producer of signed
manifests other than a test fixture; Phase 8 brings the first real one.

#### Design decisions — 66–71, PROPOSED

| # | Decision | Rationale |
| --- | --- | --- |
| 66 | **Signing the manifest is not signing the code, and the docs say so plainly.** | A signature over the manifest proves the *pointer* is authentic; it says nothing about what `remoteEntry.js` serves tomorrow. Module Federation makes this sharper than usual — hashing the entry chunk SRI-style still leaves every lazily-fetched chunk unhashed. What this phase buys is authenticated provenance of the pointer, with origin-allowlist trust for the code. Overstating it would be the worst possible outcome, because it would retire the allowlist in people's minds while leaving it load-bearing. |
| 67 | **Verification is server-side, at curation time.** | Browser-side verification is mostly theatre: the verifier arrived from the same origin as the code it would be checking, so anyone who can tamper with the load can tamper with the verifier. Verifying in the domain layer where the entry is accepted, and storing the outcome, means the shell trusts the service it already trusts for the whole document. Client-side checking earns its keep only in Phase 6's bundle model, where digest and signature are fetched independently. |
| 68 | **The signed manifest bytes are the entry's source of truth; the columns are a derived projection.** | The store reassembles the document from columns today, and reassembly — key ordering, whitespace, a field defaulted on read — invalidates a signature. The alternative is canonical serialisation (JCS/RFC 8785) applied identically before every sign and every verify, which is one more thing that must never drift. Storing bytes verbatim and projecting for query and display removes the failure mode instead of managing it. |
| 69 | **Publisher keys are NKeys, curated as service state.** | The repo is already in operator mode with `nsc`; NKeys are Ed25519 with the encoding and tooling used everywhere else, and `nkeys.FromPublicKey(...).Verify(...)` is the whole verification. The trusted-publishers table carries the registry's own semantics — server-assigned revision, audited writes, disable rather than delete — because a key list that can be silently edited is not a trust anchor. |
| 70 | **Revoking a key re-evaluates every entry that key signed.** | Disabling a key must do more than stop accepting new manifests; entries already admitted on that signature are exactly the ones at risk. This is the only operation in the trust model that touches an unbounded set of entries in a single revision, and the current `Apply` path is one entry per write. Designing it now is cheaper than discovering it during an incident, which is the only other time it runs. |
| 71 | **Signature is required on dynamic entries and optional on operator-placed ones.** | The threat is an untrusted announcer, and an operator-placed entry has a human who already vouched, with an audit row naming them. Requiring signatures everywhere would mean an `nsc` key before a hello-world plugin and a signed dev seed file, which buys nothing against the actual threat. Unsigned *dynamic* entries are refused outright rather than queued — there is nothing for an operator to review, because the announcement proves nothing. |

#### Business rules — BR-AS35 to BR-AS38 (draft, to be confirmed at the gate)

- **BR-AS35 — A dynamic entry must carry a valid publisher signature.** An announcement with no
  signature, an invalid signature, or a signature from a key that is not trusted and enabled is
  refused and never stored as pending. Operator-placed entries may be unsigned.
- **BR-AS36 — Signature verification happens in the service, never in the browser.** The document the
  shell reads carries the verification outcome; the shell does not re-verify and is not asked to hold
  a trust anchor.
- **BR-AS37 — The signed manifest is stored as signed.** The bytes that were verified are the bytes
  that are stored and re-served. Any representation the service derives for query or display is a
  projection and is never the artifact a signature is checked against.
- **BR-AS38 — Publisher trust is curated, audited state.** Adding or disabling a publisher key is a
  revision-bearing, audited write with an actor. Disabling a key re-evaluates every entry signed by
  it; entries that no longer verify are withheld from the readable document.

#### Gate questions

1. **What exactly is signed** — the manifest object alone, or the manifest plus the announcing
   service's identity? The second binds "who published" to "who announced" and closes replay of a
   valid manifest by a different service; it also means a manifest cannot be handed to an operator to
   curate by hand.
2. **Key provisioning** — does a publisher key come from `nsc` alongside the account trust chain, or
   is it an independent keypair the registry knows nothing else about? The first reuses the existing
   ceremony; the second keeps plugin publishing from widening the NATS trust chain.
3. **Does a revoked key's entry get disabled or deleted-by-withholding?** Disable-never-delete says
   the row stays; the question is whether the operator sees it as disabled-by-revocation, which is a
   state the admin surface does not have today.

#### Tasks — not started; this phase is PROPOSED

> No code, specs or business-rule edits until the gate passes (AI Agent Workflow step 2).

##### 7a — verbatim storage
- [ ] Entry storage holds the manifest bytes; columns become a projection; `Document` assembles from bytes.
- [ ] Specs: a stored entry round-trips byte-identically; a projection change does not alter what is served for verification.

##### 7b — verification
- [ ] NKey verification in `registry/internal/domain`; the outcome stored with the entry.
- [ ] Specs: valid, invalid, unknown-key and disabled-key signatures, each with its own refusal cause.

##### 7c — the trust table
- [ ] Trusted-publishers table with revision, audit and disable-never-delete; admin surface for it.
- [ ] Specs: a publisher write is revision-checked and audited exactly as a registry write is.

##### 7d — revocation
- [ ] Disabling a key re-evaluates every entry it signed, in one revision.
- [ ] Specs: entries signed by a revoked key are withheld; entries signed by other keys are untouched.

##### 7e — rules and docs
- [ ] `BUSINESS_RULES-APP-SHELL.md` — BR-AS35 to BR-AS38.
- [ ] `ARCHITECTURE-APP-SHELL.md` — the three trust gates, and an explicit statement of what signing does not cover.

---

### Phase 8 — PROPOSED (design gate not passed) — Announcement, preload seeding, and the pending tier

**Why this is a phase.** With a lifecycle class (Phase 5) and a trust anchor (Phase 7) in place, a
service can be allowed to say "I exist, here is my manifest, here is my signature" without that
becoming permission to run in an operator's browser. This phase adds the two remaining registration
paths and the admin surface that makes the pending one reviewable.

**Problem.** Registration today is one path: an operator editing an existing row. The Admin panel has
**no Add affordance at all** — the drawer only opens from a row — so a new manifest can arrive only
by `curl` or by the seeder, and the seeder is a manual `go run` that is not revision-idempotent.

**Scope.** The announce subject and its handler; the `announced` pending state and the Admin tier that
shows it; operator enablement of an announced entry; server-side preload seeding; the changed-remote
rule.

**Explicitly out of scope.** Anything that lets an announcement activate itself. A client-side static
plugin set — settled against on 2026-08-30; preload is a server-side tier only.

#### Design decisions — 72–77, PROPOSED

| # | Decision | Rationale |
| --- | --- | --- |
| 72 | **Announce is not activate.** | A service may announce; only an operator may enable. If any service could insert a remote URL, compromising *any* backend service would mean injecting JavaScript into every operator's browser, in the shell's own realm, alongside their NATS credentials. The origin allowlist blunts that and does not close it. An announced entry is stored, visible in Admin, and served to no shell. |
| 73 | **BR-AS21 is relaxed, not deleted.** | "No self-registration by any transport" becomes "self-registration permitted with a verified publisher key, and never self-activation." The rule that mattered was never "services may not speak" — it was "a service may not decide what runs in a browser", and that half is preserved exactly. |
| 74 | **An already-enabled id re-announcing is applied without review, within the same origin.** | A restart or redeploy of an enabled plugin must not need a human, or operators will click approve without reading — which is worse than not asking. A remote that moves to a *different* origin re-queues as announced, because a substitution attack has to cross an origin boundary to matter and that is the boundary the allowlist already defends. |
| 75 | **A preloaded entry is inserted only for an id the store has never seen.** | This is NATS's own `resolver_preload` shape: a fallback tier, not a competing source of truth. It answers decision 24's objection — "a boot-time file read alongside a database would let a restart silently revert curation" — because an entry the operator has since edited or disabled is never touched. The write goes through the normal `Apply` path so it carries a revision and an audit row like any other. |
| 76 | **Preload's audit actor is `preload`, and an announcement's is the publisher key.** | "Where did this entry come from" must stay answerable from the audit trail alone. Attributing either to the shared admin identity (BR-AS23) would make the one artifact whose honesty decision 31 sells as the feature quietly untrue. |
| 77 | **A static entry outranks an announcement for the same id.** | The operator's act outranks the service's. The announcement is recorded and shown in Admin as ignored rather than dropped, so a publisher who believes they are registering can see why nothing happened. |

#### Business rules — BR-AS39 to BR-AS42 (draft, to be confirmed at the gate)

- **BR-AS39 — An announcement never activates.** A verified announcement for an unknown id is stored
  as `announced` and served to no shell until an operator enables it. No transport, payload or
  signature makes an announcement self-activating.
- **BR-AS40 — An enabled id re-announcing is followed within its origin.** A remote change that stays
  within the entry's allowlisted origin is applied without review; one that changes origin returns the
  entry to `announced` and withholds it until an operator re-enables it.
- **BR-AS41 — Preload never reverts curation.** A preloaded entry is written only for an id with no
  existing row. An id the operator has edited, disabled or removed is never re-created or overwritten
  by a service restart.
- **BR-AS42 — Every write names its true actor.** `admin` for a curation, `preload` for a seeded
  insert, the publisher key for an announcement. The audit trail answers "who put this here" without
  reference to any other source.

#### Gate questions

1. **Announce subject family** — `notify.*` (a service reporting its own state, fire-and-forget) or
   `rpc.*` (a request the registry answers, so the service learns it was refused)? The second gives a
   publisher feedback on an invalid signature, which is the difference between a debuggable
   integration and a silent one.
2. **Does an announcement expire?** A service that announces and never returns leaves a pending row
   forever. A TTL keeps the review queue honest; no TTL keeps the record.
3. **Does preload seed announced entries too**, or only enabled ones? Seeding straight to enabled is
   what makes a fresh dev environment usable; seeding to announced is what makes preload safe to point
   at an unfamiliar file.

#### Tasks — not started; this phase is PROPOSED

> No code, specs or business-rule edits until the gate passes (AI Agent Workflow step 2).

##### 8a — preload
- [ ] Seed-on-start in the service, inserting only unseen ids through `Apply`, actor `preload`.
- [ ] `registry.dev.json` becomes compose configuration rather than a manual `go run`.
- [ ] Specs: a restart does not revert an edited, disabled or removed entry; a fresh store is seeded once.

##### 8b — announcement
- [ ] The announce subject and handler: verify signature, check origin, record as `announced`.
- [ ] The changed-remote rule (decision 74) and the static-wins collision rule (decision 77).
- [ ] Specs: unsigned refused; unknown id pending; enabled id re-announcing applied; cross-origin move re-queued.

##### 8c — the pending tier
- [ ] Admin surface for announced entries: what announced, its remote, its contributions, its publisher.
- [ ] Enable/disable from that surface; the missing Add affordance for a manual entry.
- [ ] Specs: an announced entry is absent from the shell's document until enabled.

##### 8d — rules and docs
- [ ] `BUSINESS_RULES-APP-SHELL.md` — BR-AS39 to BR-AS42; BR-AS21 relaxed per decision 73.
- [ ] `ARCHITECTURE-APP-SHELL.md` — the three registration paths and the gate between announce and activate.

---

### Phase 10 — NOT OPENED (BR-AS15 gate passed 2026-08-28; awaiting its own design gate) — SeaFreight Flow Plugin Migration

Goal stub only: migrate Fleet, Port Management, and Pricing into the approved shell contract while
preserving tenant/business-unit switching, localization and cold-paint rules, NATS lifecycle,
operations, and current UI capability. This phase requires its own current-state audit, business-rule
confirmation, **delta** mockup (BR-AS14 as amended), design decisions, and approval before tasks are written.
~~It does not open until the user has reviewed the running Phase 1b example plugin (BR-AS15).~~
**BR-AS15 satisfied 2026-08-28** — the remaining gate is this phase's own.

---

### Phase 11 — NOT OPENED (after Phase 10) — Admin Plugin Migration

Goal stub only: migrate Admin's full navigation and operational panels, including route-scoped
topbar controls and telemetry footer, without weakening PLATFORM permissions or dropping current
diagnostics. This phase requires its own business-rule confirmation, **delta** mockup, design gate,
and derived tests.

---

### Phase 12 — NOT OPENED (after Phase 11) — Tech Lab Operator Plugin Migration

Goal stub only: migrate Reference Data, Shippers, and Transporters while preserving the separate
PLATFORM refdata and tenant Organizations connections, all editing/document/fleet/certificate
capabilities, and map/file assets. Last in the order because it is the only app holding **two**
credential profiles at once. This phase requires its own business-rule confirmation, **delta**
mockup, design gate, and derived tests.

## Working assumptions

- All known production plugins use Vue 3; framework heterogeneity is not a current requirement.
- The shell and remotes are trusted first-party platform artifacts, not untrusted third-party code.
- ~~The platform can eventually serve one curated registry response over HTTP; Phase 1 may use static
  JSON with the identical schema.~~ **Superseded at approval (2026-08-28):** the registry is served
  by an operator-curated endpoint on `accounts-service` from Phase 1a onward (Design decision 21).
  Static JSON inside the shell's bundle was rejected because it makes BR-AS03 only nearly true —
  adding a plugin would still mean redeploying the shell's deployment unit.
- Existing backend APIs and NATS permission models remain unchanged during Phase 1.
- Existing app behavior is authoritative over stale architecture prose or historical mockups.
- ~~User approval of Phase 1's proposed rules and design is required before this file gains an
  implementation/test checklist or any application source is changed.~~ **Satisfied 2026-08-28.**
  Task checklists for 1a and 1b are derived next, from the approved rules.

## Design-gate decisions — resolved (2026-08-28)

| # | Decision | Outcome |
| --- | --- | --- |
| 1 | BR-AS01–BR-AS14 as the initial rules | **Approved with amendments.** Restated testably and moved to `BUSINESS_RULES-APP-SHELL.md`; BR-AS15 added (see below). |
| 2 | Metadata-first discovery over eager `activate()` | **Approved.** Eager discovery would collapse lazy loading, failure isolation and version rejection at once — a plugin throwing on activate would take the nav with it. |
| 3 | Vue-only Module Federation for Phase 1 | **Approved.** Every existing frontend is Vue 3 + Vite + PrimeVue. Reversibility is preserved by Design decision 12's loader adapter: no plugin imports a federation type directly. |
| 4 | Demo catalog stays as a built-in plugin at `/demos` | **Approved**, and promoted to Phase 1a's primary test fixture — it proves the contract before any remote exists. No privileged path: it uses the public contribution API. |
| 5 | One remote per current app for first migration | **Approved.** Per-service decomposition during migration would make every failure ambiguous between "the contract is wrong" and "we split this app badly". |
| 6 | Migration order SeaFreight → Admin → Tech Lab Operator | **Approved.** Rationale recorded as *credential-profile complexity ascending*, not app size: SeaFreight is single-tenant-scoped, Admin holds PLATFORM, Tech Lab Operator is last because it is the only app holding **two** profiles at once (refdata-admin PLATFORM + tenant Organizations). |
| 7 | Shell-global locale, credential-scoped refdata clients | **Approved.** Locale belongs to the person, refdata content to the credential. Prerequisite recorded in BR-AS11: `useRefdataLabels.js`'s module-global `transport` must be fixed before a second plugin exists — it is a cross-tenant leak shape, not a migration nicety. |
| 8 | Plugin-owned credential-scoped NATS lifecycles | **Approved.** One shell connection would need the union of four permission profiles — strictly more browser authority than any app holds today. Cost accepted: N reconnect state machines and a teardown contract (BR-AS10). |
| 9 | Mockup gate for Phase 1 and every migration | **Approved for Phase 1** (capability-complete; satisfied 2026-08-28, seven artboards). **Qualified for migrations:** delta mockups only — screens where shell composition changes what the user sees. Pixel-identical screens need no artboard. |

### Resolved at approval, not in the original nine

| Question | Outcome |
| --- | --- |
| BR-AS05's permission source | **auth-service JWT claims** held by the shell. One source for every plugin, independent of which NATS credential a plugin opens — which is what keeps BR-AS05 compatible with BR-AS08's metadata-before-code ordering and BR-AS10's four profiles. |
| Registry transport and owner | **Operator-curated endpoint on `accounts-service`** (Design decision 21). Not a new service, and not a file in the shell's bundle. |
| Test runner | **Mandatory** — Vitest in `lab-shell/`, Phase 1a's first task, matching admin's Vitest 4 + happy-dom + `vue/test-utils` setup. Nothing is enforceable without it. |
| Example plugin before migration | **BR-AS15, new.** No real app is migrated until a purpose-built example plugin exercising every contribution kind has been deployed and reviewed by the user. |

### Still open

- ~~**Registry endpoint placement** is the one decision made without strong precedent~~ —
  **carried forward, 2026-08-28.** `accounts-service` keeps the endpoint through Phase 2 (registry as
  service state); a dedicated platform service is now recorded as Phase 6, opened only by the triggers
  listed there. The shell's read contract is identical either way, which is what keeps the decision
  reversible.
- **Task checklists for 1a and 1b** are derived next from the approved rules.
