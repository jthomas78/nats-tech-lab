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
> <https://claude.ai/code/artifact/c7d139c4-1e7a-4ac2-9d41-cb0611409118>
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

### Phase 2 — PROPOSED (2026-08-28), awaiting design gate — Dynamic Platform Registry (registry as service state)

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

#### Design decisions — proposed, for approval

> Revised 2026-08-28 after a `codebase-design` review of this phase against the shipped Phase 1 code
> and `accounts-service`'s current registry implementation. Decisions 23, 25, 26, 27, 31 and 32 were
> amended (23's stated rationale did not hold; 26 and 32 were under-priced); 35 and 36 are new and
> name two interfaces the phase had left implicit. BR-AS19 and BR-AS20 were amended in the same pass.
> Nothing was implemented — the gate below still stands.


| # | Decision | Rationale |
| --- | --- | --- |
| 22 | **Curation is platform-wide, not per-tenant** (user's call, 2026-08-28) | One curated set for every shell. Per-`{context}` curation is a schema question, so the table keeps room for it (see 24), but no read path or UI takes a scope argument in this phase. |
| 23 | **Postgres source of truth, KV as write-through read cache** | The registry is small, read on every shell boot and rarely written. Applied honestly, the deletion test does not carry the cache on its own: six rows read at boot is not a cost, and a second store adds a coherence path plus a warm/cold state an operator has to learn. It is here because *this shape is the POC's subject* — the registry is a good small instance of the pattern the lab exists to evaluate — not because the read is slow. **The earlier rationale (that the KV entry supplies the watch decision 25 needs) is withdrawn: nothing in this phase consumes a KV watch.** If the cache is dropped later, decision 35's interface is what makes that a one-adapter change. |
| 24 | **Entries are rows, seeded from the existing JSON** | `registry.dev.json` stays in the repo as the seed input, so local review keeps working unchanged and the file stops being production configuration. Seeding follows the repo's existing `cmd/seed-*` idiom. |
| 25 | **Propagation is a signal, never a hot-swap** | A revision change is published on `notify.*` for service-side consumers, and reaches the *shell* over its existing read path (decision 27's conditional GET) — see decision 36 for why the browser is not on NATS in this phase. Either way the shell surfaces "the plugin catalog changed — reload to apply". This is not timidity: the status machine has no transition out of `active`, so a plugin whose entry disappears while its components are mounted has no legal state to move to. Reload is the only sound way to apply a removal, and the contract should say so rather than imply otherwise. |
| 26 | **Additions may be indexed live; removals and URL changes may not** | Indexing reads metadata and fetches no remote code (BR-AS08), so a new entry is safe to place without a reload. This gets the valuable half of live update with none of the risk. **It is not free on the shell side, though, and the phase prices it:** `contributionRegistry.index()` appends to its arrays (re-indexing the same set duplicates) and `createShellRoutes` maps `contributions.routes` into router config once at boot, so live indexing needs an incremental `index()` and a runtime `router.addRoute`. "Built once at boot" is an invariant callers rely on today; this decision retires it deliberately rather than by accident. |
| 27 | **`revision` becomes the concurrency token** | Monotonic, server-assigned. ETag / `If-None-Match` on the read; optimistic concurrency on the write, keyed on the revision the writer read. This is the single-spa race, answered by a transaction instead of a service — but only if writes are actually keyed on it. **The read contract is confirmed unchanged, not assumed:** `manifestSchema.js` already validates `revision` as `string \| number` and stringifies it, so moving from `"dev-1b"` to a monotonic integer needs no shell change. The conditional GET also carries the change signal decision 25 needs. |
| 28 | **Remote origins are allowlisted in service configuration, not in the mutable document** | A dynamic write path widens the blast radius of a compromised registry from "filesystem access on the host" to "one API call". Config-level origin allowlisting means a rogue write still cannot point the shell at an arbitrary host. Per-entry SRI (`remote.integrity`) is the second layer, and is proposed here rather than deferred to Phase 6 because it is cheap to add while the schema is already changing. |
| 29 | **Self-registration stays prohibited** | A plugin may not announce itself, by any transport. This is BR-AS01's guarantee restated for a write path that did not previously exist; without it the registry stops being an operator decision and becomes an ambient one. |
| 30 | **The degraded path is preserved and tested** | An unavailable registry (Postgres down, KV cold, malformed row) logs and serves the built-in set, so the shell renders. This behaviour exists today and a move to a database is exactly the kind of change that quietly loses it. |
| 31 | **Registry writes are audited** | "Who enabled this plugin, when" is asked after an incident, not before one. **Plain append-only rows, not an event-sourced log.** CLAUDE.md's deciding question is whether anything replays the history: the audit panel reads writes in order and nothing reconstructs registry state from them, so this is CRUD. It reuses the *shape* of `accounts/audit.go` (action, actor, outcome, JSONB metadata, no UPDATE, no DELETE) in the registry's own schema — the shape, never the table, since decision 33 forbids the join. It flips to event-sourced only if "what was curated at revision N" becomes a real requirement; that is the named trigger, and it is not in this phase. A refused write consumes no revision and is recorded as refused. |
| 32 | **Own bounded-context module, same process** | The registry becomes its own module with its own domain package and `composition.go`, reaching `accounts` only through a port for the BR-AS05 claims — never into its internals. **Note the real cost: `accounts-service` has no `composition.go` and no hexagonal split today** — it is a flat ~40-file `accounts` package, the one backend module that never adopted the layout the other five use. This decision introduces that pattern to the service, which is more than "move some files"; the gate should price it as such. A separate *process* is still deferred to Phase 6, because it buys none of what makes Phase 6 expensive (the publishing lifecycle, which exists in neither option today) while paying now: its own NATS credential and account user, a 72xx port, its own database and migrations, compose service, health/observability wiring, docs and suite — and it turns the shell's boot-path read into a cross-service call for a document that changes a few times a month. Phase 6 then moves a module into its own `main.go`. |
| 33 | **The registry owns its tables outright** | No join from an accounts table into a registry table, in either direction. This, not the code's location, is what decides whether the Phase 6 split is small or structural. |
| 34 | **The endpoint path names the capability, not today's host** | Move the shell to `/api/platform/registry/frontend-plugins`, still served by `accounts-service`. `/api/platform/accounts/...` bakes the current owner into the shell's constant and every frontend's Vite proxy; renaming it now is one line, renaming it at Phase 6 is a client change across apps. Phase 6 then becomes a routing change. |
| 35 | **The module's interface is named before its store changes** | The phase's own claim — reversible if the store choice turns out wrong — is a property of the interface, not of the store, and today there is no interface to be reversible behind: the registry is two package-level mutable globals, four exported loaders/setters and a handler, which is as much surface as implementation. Phase 2 replaces the implementation and therefore must name the seam it is replacing it behind. Two methods carry the phase — `Current(ctx) (Document, error)` and `Apply(ctx, Write) (Document, error)` — with revision assignment (27), the origin check (28), the KV write-through (23), the audit append (31) and the notification (25) all *behind* them. The handler and the seeder then each learn two methods, and the store swap stays in one place. This also retires a live defect in the current shape: `SetCuratedFrontendPlugins` and `SetCuratedFrontendRevision` are two setters for one fact, so a caller can install a set under the previous revision — exactly BR-AS17's failure mode. `Apply` installs both or neither. |
| 36 | **The change notification is a five-token subject, and the browser is not on NATS in this phase** | Two separate points the phase kept implicit. (a) The subject is `notify._platform.registry.frontend-plugins.changed` — five tokens, matching the `notify.{context}.{service}.{entity}.{action}` family that `shipping-service`'s `internal/notify` already builds and that CLAUDE.md's fixed-arity positional parsers require. `_platform` is the reserved platform context; this is the one place an `accounts-service`-hosted subject carries a context token, and it does so because the registry is its own bounded context (32), not the tenant axis `accounts-service` otherwise administers. (b) **`lab-shell` has no NATS client** — all three migrating apps depend on `@nats-io/nats-core`; the shell does not, and `connectionRegistry.js` is profile bookkeeping rather than a connection. So `notify.*` reaches service-side consumers, and the shell learns of a change through decision 27's conditional GET. Without this, BR-AS19 would have silently depended on Phases 10–12 while the phase header claims independence from them. |

#### Proposed business rules — BR-AS16 to BR-AS22 (for confirmation before any test or code)

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
  removed keeps rendering it and offers a reload. *Failure:* a running plugin is torn down under the
  user, or the change is silent until the next boot.
- **BR-AS20 — Origin allowlist, enforced on write and on read.** A registry entry whose remote URL
  is not on the service's configured origin allowlist is refused at write time **and** withheld at
  read time. The read-side check is not redundant: narrowing the allowlist in configuration leaves
  already-stored rows non-conforming, and that is the case the write-time check cannot cover.
  *Failure:* the shell is offered a remote on an unconfigured host — including after an allowlist
  was narrowed.
- **BR-AS21 — No self-registration.** No transport permits a plugin to add, modify or enable its own
  registry entry. *Failure:* an entry appears that no operator wrote.
- **BR-AS22 — The registry degrades, it does not fail.** With the store unavailable, the endpoint
  serves the built-in set and the shell renders. *Failure:* a registry outage produces a blank shell.

**Gate.** This phase stays PROPOSED — no tasks, tests or code — until design decisions 22–36 above and
BR-AS16–BR-AS22 are confirmed.

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
