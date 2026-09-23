# Extensible Application Shell + Micro-Frontend Plugins — Plan

> **Status: Phases 1–5, 7, 8, 13, 14 COMPLETE and archived. Phase 15's design gate PASSED
> (2026-09-02); its task checklist is derived and specs are next. Phase 16 (
> `plugin-source: build`) is PROPOSED 2026-09-23; its gate is in progress — nothing is built.**
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
> Deepening review (2026-09-02):
> [`reviews/architecture-review-20260902.html`](reviews/architecture-review-20260902.html)
> — five shallow-module candidates found by a `/improve-codebase-architecture` pass over the shell,
> with before/after diagrams. All five were implemented; the report is kept as the record of why.
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

### Phase 2 — Completed (archived 2026-09-01) — Dynamic Platform Registry (registry as service state)

Full detail is in `Application-Shell-Microfrontend-Plan-ARCHIVE.md` and is **not read into context
by default**.

- [x] **2a — the module and its store.** `registry/` as a hexagonal module with its own Postgres
      tables, a monotonic revision and an append-only audit; the KV write-through cache; the
      `REGISTRY_ALLOWED_ORIGINS` allowlist enforced on write *and* read; the endpoint moved to
      `/api/platform/registry/frontend-plugins` as a clean break; the boot-time registry file and
      its env var deleted. No `DELETE` route anywhere in the module (BR-AS24).
- [x] **2b — the admin surface.** `FrontendShellView` with Plugins and Registry Audit tabs, the
      entry drawer with both refusal panels (stale revision, disallowed origin), and disable/enable
      as the only lifecycle control — no delete affordance.
- [x] **2c — the shell notices a change.** Conditional reads, a re-read on tab focus plus a
      ~10-minute floor, `degraded: true` handling, incremental re-indexing, and the reload banner
      that is offered and never applied.
- [x] **Design decisions 22–36 and BR-AS16 to BR-AS24** confirmed at the gate 2026-08-28.

---

### Phase 3 — Completed (archived 2026-09-01) — Live-change correctness and the write boundary

Full detail is in `Application-Shell-Microfrontend-Plan-ARCHIVE.md` and is **not read into context
by default**.

- [x] **3a — the shell's live-change model.** `registryDiff.js` deep equality over validated
      manifests; reactive contribution arrays so a live addition reaches the *screen* and not only
      the shell's collections; a mounted-shell spec, because the collections hand back copies and a
      spec reading one twice can pass while the browser never updates.
- [x] **3b — the store's write path.** The installed document is returned from inside the
      transaction, so a post-commit failure is reported as accepted, audited as accepted, and
      leaves no refused row.
- [x] **3c — the write boundary.** The shell's proxy split from the admin's; the shell's origin
      reaches no write route, asserted against the `Mount` return list.
- [x] **3d — rules and docs.** BR-AS19 and BR-AS02 amended, BR-AS25 added.
- [x] **Design decisions 46–52**, gate passed 2026-08-30.

---

### Phase 4 — Completed (archived 2026-09-01) — The shell's NATS transport and push change propagation

Full detail is in `Application-Shell-Microfrontend-Plan-ARCHIVE.md` and is **not read into context
by default**. This phase moved the transport and nothing else — no new registration path, no
lifecycle change, no signing.

- [x] **4a — the connection.** The shell's NATS WebSocket connection and credential mint, with a
      shell-platform transport profile kept separate from every operator profile. A shell that
      cannot connect still renders its built-ins and says why.
- [x] **4b — the read.** `registryTransport` replaced the HTTP read; four operator subjects granted
      to Admin; both panels and the seed CLI moved to NATS; the HTTP registry routes retired, with
      the REST route list left exhaustively empty so a reintroduced route is a test failure.
- [x] **4c — push.** Subscription to `notify._platform.registry.frontend-plugins.changed`, a
      revision-only hint triggering a conditional read, an unconditional read after reconnect, and
      conditional catch-up closing the initial snapshot/subscription gap. `registryWatcher.js`
      deleted and what it proved moved onto the subscription.
- [x] **4d — rules and docs.** BR-AS27 to BR-AS31; BR-AS19 restated as push.
- [x] **Design decisions 53–58**, gate passed 2026-08-30.

---

### Phase 5 — Completed (archived 2026-09-01) — Lifecycle, withdrawal, and health

Full detail is in `Application-Shell-Microfrontend-Plan-ARCHIVE.md` and is **not read into context
by default**. Designed 2026-08-31 across 14 approved decisions; built and verified live 2026-09-01.

- [x] **5a — lifecycle through the stack (BR-AS52–53).** An explicit `static` / `dynamic` class the
      registry owns; legacy unclassified rows backfilled as static without touching enablement,
      signed bytes or the revision; class edits taking effect on reload.
- [x] **5b — unregister, withdrawal and return (BR-AS54–56, BR-AS59).** A service-only signed
      unregister binding the action, the plugin, the publisher and the key, reusing Phase 7's gate
      so there is one gate and not two. `withdrawn` and `release` are real columns: publisher
      availability is separate from operator approval, and both survive a restart. The shell takes
      away contributions and keeps the module; an unchanged return restores without activating
      twice.
- [x] **5c — occupant and dependent placements (BR-AS57–59).** The occupant keeps their URL and
      gets a shell-owned tombstone view while a `beforeEach` guard refuses newcomers — the route
      record stays registered. Withdrawing a slot's owner *suspends* placements aimed at it rather
      than refusing them: the contributor is not at fault.
- [x] **5d — health worker, transport and UI (BR-AS60–65).** Health is decoration with its own
      request/push subjects and own snapshot shape — no revision, no entries, no signed bytes.
      Frontend and backend stay two signals. 5s probes, 2s timeout, 2 failures, 15s freshness.
      Revised 2026-09-02 so the central checker broadcasts each completed observation; shells use
      startup/reconnect reads, local ageing and a 45–75s jittered reconciliation instead of polling
      every five seconds. `shared/natsready` added, because presence is not readiness.
- [x] **5e — evidence, rules and docs.** Registry 358/358 with 0 skipped, shell 481/481, Admin
      335/335, `shared/natsready` 6/6.
- [x] **Live 1920×1080 verification, 2026-09-01.** It found three deployment defects every suite was
      blind to: a Dockerfile missing `COPY` lines for two new shared modules, credentials that
      predated the grant change (`bootstrap-operator.sh --force` is required), and a shell timer
      that repainted without re-reading. All fixed and re-verified. See
      `BUSINESS_RULES-APP-SHELL.md`.

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

**The process split was taken early, 2026-08-31, on its own — `mfe-registry-service`.** Not Phase 6
opening: the publishing lifecycle above is untouched and every trigger listed still fails to hold.
What moved is the module and nothing else — `mfe-registry-service/registry/` (same hexagonal
layout, same schema), its own Postgres on 5437, its own PLATFORM credential, 7206 for `/healthz`,
and the KV bucket renamed `registry` → `mfe-registry`.

Decisions 32–34 priced it correctly, and one of them turned out to be worth more than expected:
because Phase 4 had already moved the read to an `api.*` subject, **neither frontend changed at
all** — not a proxy rule, not a constant. NATS resolves a subject to whichever process is
listening. The costs decision 32 named were the real ones and were all paid: a credential and nsc
user, a port, a database and compose service, a Dockerfile, docs and a suite. The one thing that
deliberately did *not* move is credential minting — `accounts-service` owns the trust chain, so it
still names the registry's subjects when it mints the shell's and the operator's browser grants.
That is now a cross-service contract, held in `shared/mferegistry` and enforced by
`TestShellReadIsUngatedAndEverythingElseIsNot` reading the same list both sides use.

The split needs `docker compose down -v` and a bootstrap reseed (new nsc user, new database, new
bucket). Curated entries do not survive it; there is no seed for them beyond
`cmd/seed-registry`.

---

### Phase 7 — Completed (archived 2026-09-01) — Publisher signing and the trust table

Full detail is in `Application-Shell-Microfrontend-Plan-ARCHIVE.md` and is **not read into context
by default**.

- [x] **7a — verbatim storage.** The signed bytes are stored exactly as received, because a
      re-serialized manifest is a different document and will not verify.
- [x] **7b — publishers, keys and ownership.** The operator's trusted-publisher table, one read and
      one write rather than a subject per operation.
- [x] **7c — verification.** `domain.NKeyVerifier{}` in `composition.go`. The trust anchor is the
      publisher table, so an empty table refuses everything — the same fail-closed behaviour the
      earlier `NoVerifier` placeholder gave, reached by policy instead of by placeholder.
- [x] **7d — revocation**, and **7e — revocation reaching a running browser**, plus the degraded
      read's behaviour under it.
- [x] **7f — rules and docs.** BR-AS35 to BR-AS38 and BR-AS46 to BR-AS51, with Phase 7's trust
      rules and their limits written down.
- [x] **Design decisions 66–71 and 97–105**, gate passed 2026-08-31.

---

### Phase 8 — Completed (archived 2026-09-01) — Announcement, preload seeding, and the pending tier

Full detail is in `Application-Shell-Microfrontend-Plan-ARCHIVE.md` and is **not read into context
by default**. This phase added the two remaining registration paths and the admin surface that makes
the pending one reviewable.

- [x] **8a — preload.** Operator-authored seed entries, with a whole-file parse failure failing boot
      while a single entry refusal logs `withheld` and lets the others through.
- [x] **8b — announcement.** A service saying "I exist, here is my manifest, here is my signature"
      over a service-only subject, which is never permission to run in an operator's browser.
- [x] **8c — the pending tier.** A screen for pending entries, an operator able to add one, a
      derived `source: curated | preload | announced` badge with no stored copy to disagree with the
      audit, and manifest drift reported without changing curation.
- [x] **8d — the demo catalog as a federated plugin**, and **8f — one service per plugin.**
- [x] **8e — rules and docs.** BR-AS39 to BR-AS45, confirmed at the gate.
- [x] **Design decisions 72–86** (84 retired at the architecture review), plus 87–89 for 8f.

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

### Phase 13 — Completed (archived 2026-09-01) — Announce the `example-plugin*` fixtures; leave only `demo-catalog` preloaded

Full detail is in `Application-Shell-Microfrontend-Plan-ARCHIVE.md` and is **not read into context
by default**. This phase built nothing new in the registry's rules — it made the ones Phases 7 and 8
already built runnable. Before it, nothing in the lab ever published on
`rpc._platform.registry.entries.announce.v1`; after it, five sidecars do, and `demo-catalog` is the
only preloaded plugin (BR-AS66).

- [x] **13a/13c — the trust chain.** A NATS account, an `-announcer` credential and a signing keypair
      per plugin, minted by `bootstrap-operator.sh`. Signing keys are deliberately not the NATS trust
      chain, and seeds are mounted read-only and never enter an image layer. Closed BR-D40's
      documented failure mode (`natstenants` reading five new `.creds` stems as five bogus tenants)
      with `NonTenantCredsSuffixes`, not five map entries.
- [x] **13b — the announcer.** `cmd/announce-plugin`: reads the build-owned manifest, injects the
      release it owns immediately before signing, announces, then holds the connection and publishes
      a signed unregister on SIGTERM only. The release counter (BR-AS67) is atomically persisted, so
      `N` / `N+1` / `N+2` survives a restart.
- [x] **13d — convergent, boot-ordered trust seeding.** `cmd/seed-publishers` applies only the
      missing operations and never reverses an operator decision (BR-AS68); a converged registry
      costs zero writes. Compose orders it strictly before anything that announces.
- [x] **13e — five sidecars.** One shared binary from the registry image, only the three read-only
      mounts differing. `example-plugin-unreachable` is announcer-only — no web server, so its
      fixture stays a genuine 404. `registry.json` is down to `demo-catalog`.
- [x] **13f — the lifecycle driven against the running lab.** `cmd/registry-acceptance`, a command
      rather than an env-gated spec, because a spec that skips still prints `ok`. Nine steps on
      `example-plugin` with the other four as a control group. Corrected three assumptions, all of
      them the code being stricter than expected: a revocation also clears approval; a signed
      announcement cannot lift a withholding; `pending` and `requeued` both preserve `withdrawn`.
- [x] **13g — the copy and the docs.** BR-AS66's intro copy is the rule's surface, so it became a
      component with a spec (`shell/ui/FirstBootNote.vue`) rather than a README paragraph. Compose's
      `mfe.source` label corrected to `announced` on the four example frontends and pinned by a spec.
      `ARCHITECTURE-APP-SHELL.md` gained a Phase 13 as-built section and lost four stale claims.
- [x] **Business rules BR-AS66–68**, plus two BR-AS38 clarifications that add no new ID.
- [x] **Design decisions 1–11** for this phase, across three revisions (revision 1 failed review with
      four P1 and four P2 defects; the drift question was withdrawn on a false premise).

---

### Phase 14 — Completed (archived 2026-09-02) — One container per plugin: the announcer moves into the plugin's own process

Full detail is in `Application-Shell-Microfrontend-Plan-ARCHIVE.md` and is **not read into context
by default**. Phase 13e gave every announced plugin its own announcer sidecar, so five plugins cost
ten containers and a plugin developer shipped two runtime units. Phase 14 merged them without
changing what the sidecar existed to protect: the plugin still owns its signing key and still decides
when it is announced. Per-plugin totals are unchanged — one NATS connection, one HTTP listener.

- [x] **14a — the package.** `shared/mferegistry/announcer` became its own module;
      `cmd/announce-plugin` is a wrapper over it. Carried the credential rename (decision 11) —
      the five moved to `nats/creds/plugins/` under their plugin ids, excluded from tenant discovery
      by *directory* rather than name suffix because `natstenants.Discover` already skips `IsDir()`
      (BR-D40), retiring `NonTenantCredsSuffixes`. Also carried decision 13: subject token
      `registry` → `mfe-registry`, the service name minus `-service`, matching `refdata` and
      `shipping`; `frontend-plugins` and `rpc._platform.health.{service}.ready.v1` unchanged on
      purpose. One `down -v` and reseed covered all three breaking changes.
- [x] **14a2 — the origin left the image (BR-AS71, BR-AS72).** `PLUGIN_PUBLIC_ORIGIN` is required
      with no manifest fallback and stamped in immediately before signing, so the origin sits inside
      the signed bytes (BR-AS47) and moving a plugin is a deployment change, not a rebuild. All five
      manifests now carry a path-only URL. A protocol-relative `//host/path` is refused — it reads as
      a path and resolves to a foreign host.
- [x] **14b — the host.** `shared/mfe-plugin-host`: one Go binary serving the asset root plus a
      bounded `/healthz` that deliberately carries no CORS header (BR-AS61, server-to-server), a
      *named* `Access-Control-Allow-Origin` on assets and never `*`, a genuine 404 with no SPA
      fallback, and a route set asserted as a set so no proxy-shaped route can be added later without
      a spec turning red. Announcer runs in-process; SIGTERM remains the only thing that withdraws
      (BR-AS54).
- [x] **14c — four fixtures migrated.** Ten containers became five.
      `example-plugin-unreachable` keeps the CLI announcer form so both forms stay covered, and
      `demo-catalog` was left alone — it is curated (`mfe.source: preload`), not announced.
- [x] **14d — the scaffolder.** `scripts/new-plugin.sh` plus its template, pinned by a golden
      fixture in `shared/mfe-plugin-host/deployment_test.go`.
- [x] **14e — rules and docs.** BR-AS67 amended (the counter volume attaches to the plugin
      container), a credential-naming row added, `ARCHITECTURE-COMMUNICATIONS.md` gained the
      `{service}` = service-name-minus-`-service` rule, `ARCHITECTURE-APP-SHELL.md` gained the
      BR-AS71/72 origin section and an as-built subsection, and
      `diagrams/mfe-announcer-topology.html` is the before/after — brought to as-built after the
      gate passed, since the merge falsified its "same credential, same 2 subjects" label.
- [x] **14f — the gate.** `cmd/registry-acceptance` green against the running lab with `-reset`:
      nine steps, four-plugin control group intact. **It needed three edits, against a criterion
      that said "unchanged".** 14c deletes the Compose service the harness named, so the criterion
      and the task list could not both be met literally; decision 10 states the test as *shape*, and
      the shape held. Step 6 came out stronger — moving a plugin is now a deployment override, so
      the harness no longer touches the manifest at all, and "the requeue turns on the origin and
      nothing else" is true by construction rather than by assertion. The full argument is in the
      archived phase under "Outcome 2026-09-02".
- [x] **Business rules BR-AS71–BR-AS72**, plus the BR-AS67 amendment and BR-D40's directory rule.
- [x] **Design decisions 1–13**, including the ADR at `.claude/plans/reviews/`.
- [x] **Implemented largely by Codex** from a handoff prompt kept at `.claude/plans/handoffs/`.
      It ran out of credit before the exit gate, so 14f and the plan bookkeeping were finished in
      Claude Code.

---

### Phase 15 — APPROVED (design gate passed 2026-09-02) — Frontend health over NATS, and a catalogue-reset notice

**Status: APPROVED. Decisions 1-13 are settled; task checklist, specs and code are derived next.**
Direction agreed 2026-09-02 ("let's move health to NATS"). Decisions 11-13 closed the three open
questions, then decisions 1-10 were walked one at a time and approved on 2026-09-02. Nine were
approved as proposed. **Decision 10 was amended at approval** — see its entry below. Both halves land in one phase by the user's call on 2026-09-02: they make a plugin
*subscribe* to something for the first time, and that widening is one decision, not two.

#### The two problems this phase closes

**One.** The registry probes a plugin's frontend with `GET /healthz` over the `frontend` Docker
network. That is the only reason a Phase 14 plugin container joins `frontend` at all — the browser
arrives on a published host port, not over that network. It is also a per-plugin deployment chore:
`REGISTRY_HEALTH_ORIGINS` is a hand-maintained map, exactly the kind of thing the 14d scaffolder
exists to stop generating. *(Both sentences describe the pre-15 state. As built after 15b/15c:
`REGISTRY_HEALTH_ORIGINS` is deleted, `registry/internal/healthhttp/` is deleted, and no plugin
container joins `frontend`.)*

**Two.** A plugin announces once, at start-up. If the registry loses its catalogue while the plugins
keep running, nothing re-announces. Restarting the containers heals it; nothing else does. That hole
is real and small, and it has no rule today.

**Correcting the record.** An earlier note in this conversation said moving health to NATS deletes
decision 6's named cost. It does not. Decision 6's cost is joining `backend`, and the plugin still
needs NATS to announce. What this phase removes is the *second* network, `frontend`. The larger wins
are the deleted chore and the point below about same-origin.

#### Design decisions — APPROVED 2026-09-02 (1-9 as proposed, 10 amended)

1. **The transport moves; the probe does not become an opinion.** The registry asks over NATS. The
   publisher answers only after a real local `GET http://127.0.0.1:<port>/healthz` against its own
   listener. `/healthz` stays, still implemented by the Go host to `nginx.conf`'s spec, and its
   tests survive intact — it simply stops being reached from outside. A reply assembled from memory
   would be the process's opinion of itself; a self-GET exercises the real listener over a real
   socket, on the same code path a browser would take.

   What this gives up, stated plainly: the network path. But the registry's Docker-DNS probe was
   never the browser's path either — the browser arrives via the published port or the ingress. The
   probe already tested a door the browser does not use.

2. **This is a prerequisite for the production shape, not a preference.** Under BR-AS72 a plugin may
   have no origin at all — one hostname, one path prefix per plugin. There is then nothing for the
   registry to dial except a loop back out through the ingress and in again, which inside a cluster
   is often not even reachable. Phase 14 makes the path-prefixed deployment possible; this phase is
   what keeps health working in it.

3. **One subject per plugin, one token wide.** Mirror BR-AS62 exactly, including its reasoning. The
   proposed shape is `rpc._platform.health.frontend.{pluginID}.ready.v1`, and the `frontend`
   discriminator is load-bearing: without it a plugin id could collide with a service id and a
   plugin could answer for a *service*'s readiness. Each plugin's credential subscribes to its own
   token only, never `>`. A plugin must not be able to answer for another plugin.

4. **"Not configured" survives, in a new form.** BR-AS61's safety property is that the registry
   cannot probe arbitrary targets. Before 15b that was `REGISTRY_HEALTH_ORIGINS`. After this the subject
   is derived from the plugin id in the *signed* entry, and the grant is one token wide — so the
   registry can still only reach plugins it holds entries for. The property is preserved; the
   hand-maintained map is not. BR-AS45's allowlist stays untouched for manifest *fetch* origins.

5. **The plugin container drops `frontend`.** `backend` only, plus its published host port.

6. **The reset notice is a statement of fact, not a command.** Proposed subject
   `notify._platform.mfe-registry.entries.reset` (position 3 per decision 13; entity `entries`
   because this is publisher-facing, not the browser's `frontend-plugins` view). The registry states
   that its catalogue was reset. Each publisher decides to re-announce. Deliberately **not** `cmd.*`
   — that family is reserved and has zero uses, and opening it here would claim the registry has
   authority over a plugin's process, which BR-AS54 says it does not.

7. **Jitter, with the window carried in the message.** The herd is not the notice — one message
   fanning out is free. The herd is the *replies*: five hundred signed announces, each a signature
   verify plus a Postgres write. A queue group is the wrong tool (it delivers to one subscriber, and
   we want all of them). Precedent already exists in this codebase: BR-AS65's shell does a 45–75
   second jittered reconciliation read. Putting the window in the notice lets the registry widen the
   spread without redeploying a single plugin.

8. **The notice needs no durability.** Core NATS, no JetStream, no retention, per BR-AS64. A
   publisher that is down when it fires misses it and announces on start-up anyway, so the offline
   case is already covered. This is a simplification, not a gap.

9. **Silence never withdraws.** The most important guardrail in the phase. A plugin that does not
   answer a health ask is unhealthy; a plugin that does not answer a reset notice is simply not
   re-announced. Neither is ever unregistered. BR-AS54 is untouched, and any design that reads
   absence as an authoritative action is wrong by construction.

10. **A resync announce spends a release number (BR-AS67); identical content writes no revision and
    no audit row, but the release watermark still advances.** **Amended at approval (2026-09-02),
    from a Codex review of the proposed wording.** The proposal said a converged registry should
    cost *zero* writes. That is very nearly right and is wrong in one way that matters: the release
    counter is not decoration, it is this protocol's stale-announcement protection.
    `domain.Verify` refuses `Release < Accepted` with `ErrReleaseBackwards`, and
    `UnregisterCommand` refuses a release the running announcement already spent
    (`ErrReleaseReused`). `Accepted` is the watermark both of those read.

    So a resync at a higher release with identical content is treated as: **no catalogue revision,
    no audit event, but `Accepted` advances to the new release.** If the watermark did not advance,
    every release number the publisher spent on a resync would stay indefinitely acceptable, which
    widens the replay window by exactly the number of resyncs — the opposite of what the counter is
    for.

    Stated honestly, this means a reset storm is not literally free: it is one small watermark update
    per plugin, not zero. It is still the cheap case, because the expensive parts — the revision bump
    and the audit row — are what convergence skips, and it keeps BR-AS68's principle intact in the
    form that principle was actually about. Note that today's `Admission.NoOp` is a narrower thing
    (`Release == Accepted`, i.e. a literal replay); this decision adds a *content*-equality no-op
    beside it, and the two must not be collapsed into one flag.

#### Rules — one rewrite, one addition (approved 2026-09-02; ids assigned when the specs land)

- **BR-AS61 is rewritten in place**, not superseded. It is the same business rule — frontend
  availability is centrally probed through a bounded endpoint, and a successful probe still does not
  attest that browser networking, `remoteEntry.js` or lazy assets work. Only the transport and the
  clauses that were HTTP-shaped change: redirects, oversized bodies, arbitrary egress and
  browser-origin fallback stop being meaningful, and the mapped-origin clause becomes decision 4's
  derived subject. `registry/health_frontend_test.go` stays its home. **Recommendation, for the
  gate:** rewrite rather than add a BR-AS73 for the same responsibility — two rules claiming central
  frontend health probing would be worse than one rule with a dated amendment.
- **A new rule for the reset notice**, id to be assigned at approval (next free is BR-AS73). It
  states decisions 6, 7, 8 and 9 together: the registry may state that its catalogue was reset;
  publishers re-announce on their own initiative within a carried, jittered window; the notice is
  not durable and needs not to be; and silence is never withdrawal.

#### Resolved before the gate closes (2026-09-02)

11. **Nested deadlines: the inner one always expires first, and the gap is a rule, not a habit.**
    `HealthProbeTimeout` is 2s today and becomes the *outer* NATS request deadline unchanged. The
    inner local `GET http://127.0.0.1:<port>/healthz` gets **1s**, half the outer. The ordering is
    what makes the reply meaningful: if the inner expires first the publisher answers, on time, that
    its own listener is slow — a real observation with a cause. If the outer expired first the
    registry would see a NATS timeout and could not tell a slow listener from a dead process, a
    missing subscription, or a broken bus. So the two constants are not independently tunable, and
    the phase adds a spec asserting `inner < outer` rather than two loose numbers that can drift
    apart in a later edit. `HealthFreshness` (15s) and `HealthFailureThreshold` (2) are untouched —
    they are about interpreting answers, not obtaining them.

12. **The health worker stays.** BR-AS61 runs probes on a separate worker joined per pass, so one
    slow plugin never delays a catalogue read, and that property survives the transport swap
    unchanged. NATS request/reply is asynchronous and carries its own deadline, so the worker looks
    redundant — but "a slow probe must not delay a read" is a property this codebase decided was
    worth owning in a spec, and moving it into the transport's behaviour makes it something no test
    of ours asserts any more. The existing worker specs stay meaningful, and the diff stays a
    transport swap instead of a transport swap plus a concurrency change.

13. **The reset notice fires only on an actual catalogue reset, never on a plain restart.** Firing
    on every restart is simpler and self-healing by construction — the registry never has to know
    *why* it is empty. It is rejected on cost at the scale this is designed for: a rolling restart
    would set off a full re-announce storm, and at 500 plugins that is 500 signature verifies and
    500 Postgres writes that decision 7's jitter window can only spread out, not avoid. The cost
    accepted in exchange, stated plainly: the registry must now tell "I restarted" apart from "I
    lost my catalogue", and **if that check is ever wrong the hole this phase exists to close
    reopens silently.** That makes the reset predicate itself a rule with a spec, not an
    implementation detail — and decision 10's convergence question is the safety net, because a
    re-announce of identical content should cost zero writes whether or not the notice was correct.

#### Decision 14 — the health signal is pushed, not polled (reopened 2026-09-02, after 15a)

Decisions 1-13 changed the health *transport* and left BR-AS63's cadence alone, so a polling loop
written for HTTP landed unexamined on a message bus: the registry asking every plugin, every five
seconds, forever. Nobody chose that for NATS — it was inherited. Raised from the drawing, not from
the prose, which is the second time this phase a picture has caught something a review missed.

**Settled: the plugin pushes; the registry listens.** The plugin runs its own clock, self-`GET`s its
loopback `/healthz`, and publishes its state on change plus a heartbeat. The registry subscribes once
across all plugins and keeps the last message per plugin. Nothing asks.

**Why this and not the hybrid.** A "push, plus ask on start-up / reconnect / reset" census was
considered and is the pattern the catalogue plane already uses (BR-AS28, BR-AS29, BR-AS65). It was
**deferred, not rejected** — because the heartbeat already covers every trigger a census would fire
on. A registry that boots with empty health fills in at the next heartbeat; so does one that missed a
change across a reconnect. The census buys **latency, not correctness**: it turns a ≤15s window of
`unknown` into a sub-second one, and `unknown` is a true statement, not a lie. Decision 13 already
names what a second trigger predicate costs — a rule that reopens a hole silently when it is wrong —
and this would be the second one.

**So it is measured before it is built.** Ship push-and-heartbeat; watch the real blank window once
Phase 15 lands; add the census only if it actually annoys an operator. **One thing is paid for now
anyway:** the plugin's subscribe grant gets the census subject even though nothing publishes on it,
because a grant is the one part that is expensive to add later — it is a `bootstrap-operator.sh` edit
plus a `docker compose down -v` reseed, and doing it now costs one line.

**What this changes from decisions 1-13:**

- **The subject flips family.** `rpc._platform.health.frontend.{pluginID}.ready.v1` (request/reply)
  becomes `notify._platform.health.frontend.{pluginID}.v1` (push), mirroring decision 3's shape so
  the `health` namespace stays one namespace. The registry subscribes on
  `notify._platform.health.frontend.*.v1` — one token wide, in the plugin-id position.
- **Review finding 2 dissolves.** The registry stops publishing frontend health entirely, so there
  is no grant to widen. `rpc._platform.health.*.ready.v1` stays exactly as it is, still serving
  BR-AS62's backend readiness, still one token.
- **Review finding 6 is restated, not dropped.** "Subscribe → confirm → announce" becomes
  **first health push, then announce**, so an entry is never briefly visible with no health. The spec
  still asserts the observable property — when an announcement reaches the registry, that plugin's
  health is already known — not a source ordering.
- **Decision 12 reverses.** The probe worker does not stay. It becomes a subscriber plus an expiry
  sweep, and its existing specs no longer pass unedited — 15b must stop treating that as a red flag.
- **The failure policy moves into the plugin.** BR-AS63's "two consecutive failures" is now decided
  by the plugin about itself. That is the real cost of this decision, and it is named: one number
  that lived in one service now lives in every plugin image, and changing it is a fleet redeploy.
- **BR-AS64's freshness stops being a backstop and becomes the mechanism.** "No heartbeat inside the
  window" is now the only way a dead plugin is detected.

**Cadence: heartbeat stays 5s, freshness stays 15s.** Chosen because it moves no existing number —
15s is exactly three missed beats, the same margin the polling model had — while still halving
traffic, since the reply disappears. A longer heartbeat is where the real saving is (15s heartbeat is
roughly one sixth the messages) but freshness must move with it, to ~45s, and that trades detection
speed for volume the lab has no reason to buy yet. **Raise both together or neither** — a heartbeat
at or above the freshness window makes every healthy plugin flicker stale.

#### Architecture review — 2026-09-02, folded into the checklist below

A review of the approved decisions (`.claude/plans/reviews/adr-phase15-health-over-nats-20260902.md`)
found nine gaps, three of them blocking. All nine were walked one at a time and settled the same day;
two were amended at approval from a Codex reading. The resolutions are recorded in full in that file
and are carried into the tasks below. The three that would have broken something:

- **`demo-catalog` would have lost health entirely** — it is probed today, but it is curated, with no
  announcer process and no NATS credential, so nobody could answer for it. Settled: **every plugin
  has frontend health, curated included.** `demo-catalog` gains a health responder and a credential
  granted only its own health token. The "not configured" state ceases to exist.
- **The registry's existing grant would not have matched the new subject** —
  `rpc._platform.health.*.ready.v1` is a *one*-token wildcard and decision 3's subject has two tokens
  after `health`. The failure would have been a silent permissions denial at runtime.
- **`bootstrap-operator.sh` states "a plugin speaks, it does not listen" as a security property**,
  beside the grant that enforces it. This phase knowingly inverts it, so the comment is rewritten in
  the same change rather than left contradicting the code.

#### Task checklist — derived from the approved rules, not from an implementation

- [x] **15a — the rules first. Done 2026-09-02.** Rewrite BR-AS61 in place (same responsibility, HTTP-shaped clauses
      replaced by decision 4's derived subject) and add the reset-notice rule stating decisions 6,
      7, 8 and 9 together. Assign its id at this point; next free is BR-AS73. Update
      `BUSINESS_RULES-APP-SHELL.md` in the same change, per CLAUDE.md's rule 4.
      **From the review:** BR-AS73 is written as catalogue *recovery*, not as a reset notification —
      "plugins must announce themselves during startup; this is the primary mechanism for populating
      the catalogue. The registry may issue a reset notice when its catalogue must be reconstructed
      while existing plugins remain running. A reset notice is not required for whole-system
      restarts." The sentence to preserve: **start-up announcement is the primary path; reset is the
      backstop for catalogue loss without plugin restart.** BR-AS61's rewrite must also drop the
      "no mapping means not checked" clause — after the review there is no unmapped state.
      **Outcome 2026-09-02:** BR-AS61 rewritten in place; BR-AS73 added as *Catalogue recovery*, in
      the wording approved at the review. One change beyond the stated scope, and it is a narrowing:
      BR-AS45's "Phase 5 extension" permitted the registry one outbound health `GET`, and that
      permission is now withdrawn rather than left standing unused — manifest drift is once again the
      registry's only outbound HTTP capability. `BUSINESS_RULES.md`'s index updated to BR-AS73. The
      BR-AS61 evidence row is marked superseded and kept, since it records the contract 15b replaces.
      **Revised 2026-09-02 by decision 14** (health is pushed, not polled): BR-AS61 rewritten a second
      time onto `notify._platform.health.frontend.{pluginID}.v1`; BR-AS63 and BR-AS64 amended in place,
      because the cadence moved into the plugin and freshness became the detection mechanism rather
      than a backstop. BR-AS73 is unaffected.
- [x] **15b — the health transport.** *(Done 2026-09-02.)* Specs before code. `rpc._platform.health.frontend.{pluginID}.ready.v1`
      (decision 3), subject derived from the signed entry with a one-token grant (decision 4), and
      the publisher answering only after a real local `GET http://127.0.0.1:<port>/healthz`
      (decision 1). One spec asserts `inner < outer` deadline ordering (decision 11) rather than
      pinning 1s and 2s independently. The probe worker stays (decision 12), so its existing specs
      must still pass unedited — if one needs editing, that is a signal the concurrency shape moved
      when it was not supposed to.
      **Superseded in part by decision 14 — health is pushed, not asked.** The subject is
      `notify._platform.health.frontend.{pluginID}.v1`; the plugin publishes on change plus a 5s
      heartbeat; the registry subscribes once on `notify._platform.health.frontend.*.v1` and runs an
      expiry sweep against BR-AS64's 15s window. The `inner < outer` deadline spec becomes
      `self-GET deadline < heartbeat interval` — the plugin must never have two checks in flight — and
      **decision 12 reverses, so the probe-worker specs are expected to change**; 15b must not treat
      that as a signal something moved when it should not have.
      **Still standing from the review:** the registry's `rpc._platform.health.*.ready.v1` grant is
      left exactly as it is, still one token, still serving BR-AS62's backend readiness — finding 2
      dissolves because the registry no longer publishes frontend health at all. The plugin gains one
      **publish** subject (its own health token) and subscribes to exactly two named subjects, the
      reset notice and the reserved census subject, with no wildcard on the plugin side; rewrite
      `bootstrap-operator.sh`'s "a plugin speaks, it does not listen" comment to state that narrower
      rule. `absent` (no report inside the freshness window) and `unhealthy` (a plugin said so
      about itself) are **separate** causes in the closed vocabulary, shown differently. The ordering
      rule becomes **first health push, then announce**, specced as the *observable* invariant — when
      an announcement reaches the registry, that plugin's health is already known — rather than as a
      source-code ordering.
- [x] **15c — delete the chore.** *(Done 2026-09-02.)* `REGISTRY_HEALTH_ORIGINS` and its per-plugin entries go.
      `REGISTRY_FETCH_ORIGINS` (BR-AS45) stays and must be shown to be untouched. The plugin
      container drops the `frontend` network (decision 5), and `scripts/new-plugin.sh` and its golden
      fixture lose the health-origin chore they currently generate.
      **From the review:** `demo-catalog` gains a health responder and a credential granted only its
      own health token — no announce, no unregister. Curation stays a property of how an entry
      reached the catalogue, not of whether it can be asked whether it is alive.
- [x] **15d — the reset notice.** *(Done 2026-09-02.)* `notify._platform.mfe-registry.entries.reset` on core NATS with no
      durability (decisions 6, 8), carrying its own jitter window (decision 7). The reset *predicate*
      — "I lost my catalogue", not "I restarted" — is itself a rule with a spec (decision 13),
      because a wrong predicate reopens the hole this phase closes and does so silently.
      **From the review:** the plugin **clamps** the carried jitter window to a locally-owned floor
      and ceiling. The registry keeps the power to widen the spread without a redeploy; nothing on
      the wire gains the power to narrow it to zero, which would turn the notice into the stampede
      decision 7 exists to prevent. That is a rule about not trusting input, so it gets a spec. The
      predicate's specs follow the review's scenario table: plugin starts → startup announcement;
      everything restarts → startup announcements; registry restarts with catalogue intact → nothing;
      catalogue lost with plugins alive, or restored from a stale backup → reset → jitter →
      re-announce.
- [x] **15e — convergence.** *(Done 2026-09-02.)* A content-equality no-op that writes no revision and no audit row but
      advances `Accepted` (decision 10, amended). Kept distinct from today's `Admission.NoOp`, which
      means a literal replay at an equal release; a spec should pin that the two are different.
      **From the review:** this is a write to the entry row that does not bump the revision, and it
      has not been checked against that row's concurrency control. Read the update path here and pin
      with a spec that a watermark-only write cannot lose a concurrent real announce.
- [x] **15f — silence is inert.** *(Done 2026-09-02.)* Specs proving a plugin that stops publishing
      health becomes stale/absent but stays registered, while a never-heard plugin stays unknown; a plugin
      that ignores a reset notice is simply not re-announced (decision 9). Neither path may reach unregister.
      BR-AS54 unchanged.
- [x] **15g — docs.** *(Done 2026-09-02.)* `ARCHITECTURE-APP-SHELL.md` gains the as-built section and loses the claims
      this phase invalidates; `ARCHITECTURE-COMMUNICATIONS.md` gains the two new subjects. The Phase
      14 topology drawing's "after" panel now shows a plugin on two Docker networks and must be
      re-drawn to one, or explicitly dated as Phase 14's state.
      **Drawn ahead of the code (2026-09-02):** `diagrams/mfe-health-over-nats.html` is a separate
      before/after drawing for this phase, embedded in `ARCHITECTURE-APP-SHELL.md` and stamped
      "proposed". 15g brings it to as-built the way Phase 14's was — re-stamp the eyebrow, and fix
      any label the implementation moved. The Phase 14 drawing is left alone; it is dated as Phase
      14's state and the new one carries the change.
      **Done.** `ARCHITECTURE-APP-SHELL.md` § "Independent health observations": the paragraph
      describing the registry dialling `GET /healthz` through BR-AS45's origin map is withdrawn, the
      cadence paragraph now says the frontend clock is the plugin's, `not configured` is marked
      backend-plane-only, and a new "As built, Phase 15" block covers the subject derived from the
      signed entry, the lost `frontend` network membership, curated publishers reporting, the two
      subjects a plugin now subscribes to, and `unavailable`/`unreachable` vs `stale`/`absent`.
      `ARCHITECTURE-COMMUNICATIONS.md` gains the two Phase 15 subjects *and* the two health subjects
      from Phase 5 that were never listed, with the inverse-grant note and the reserved census
      subject named. `diagrams/mfe-health-over-nats.html` re-stamped "as built", proposal disclaimer
      replaced by the 15h live result; one label the implementation moved is fixed — the drawing
      said `unhealthy` where the shipped vocabulary reports `unavailable` with cause `unreachable`.
      Layout audit clean, PNG re-exported. Phase 14 drawing untouched, as planned. Also picked up:
      the stale "no mapping means not checked" clause in `registry/internal/domain/health.go`,
      which 15a was supposed to drop.
- [x] **15h — the gate.** *(Done 2026-09-02. Green against the running lab, 11 steps.)*
      `cmd/registry-acceptance` green against the running lab. It asserts health
      today, so unlike Phase 14 this phase should expect to touch it — and any edit is recorded in
      this entry the way Phase 14's three were.
      **From the review:** step 9's control group changes — `example-plugin-unreachable` now answers
      and honestly reports `unhealthy` (it self-GETs against a listener that does not exist), where
      today it reports "not configured". That leaves the new `absent` cause with no fixture, so
      add a step that stops a running plugin's process and asserts the registry reports
      `absent` — which exercises BR-AS54 (silence never withdraws) on the same step.
      **Correction to this entry as written:** the binary did *not* assert health today — the claim
      was wrong, and the health assertions below are new rather than migrated.
      **Edits to `cmd/registry-acceptance` (four, recorded the way Phase 14's three were):**
      1. *A second connection.* `harness` gained a `shell *nats.Conn`, and `connect` gained `mint`
         and `name` parameters so `run()` opens both `/api/auth/adminConnectInfo`
         (`registry-acceptance`) and `/api/auth/shellConnectInfo` (`registry-acceptance-shell`).
         `HealthRead` is deliberately the *shell's* subject (BR-AS25/AS27) and the server refuses
         an operator credential there. Minting the right credential was the fix; widening a grant
         was not considered.
      2. *New step 2 — "every plugin reports its own frontend health, curated included."* Placed
         before `otherEntries()` so the control group's baseline already holds the enablement.
         Asserts `demo-catalog` (curated, `HEALTH_ONLY`) reports `healthy` and is not
         `not configured` — that state is gone from the frontend plane — then enables
         `example-plugin-unreachable` and asserts `unavailable` with cause `unreachable`.
      3. *New step 11 — "a plugin that goes silent is reported absent — and stays registered."* The
         `absent` fixture the review said was missing. A new `hardKill` helper (SIGKILL, distinct
         from the existing graceful `kill` precisely because a grace period lets the unregister
         through) drops a live publisher, then the step awaits `stale` with cause `absent` and
         asserts the entry is still registered, spent no release, is not withdrawn, and stays
         enabled — BR-AS54 end to end.
      4. *Renumber.* Two inserted steps, so the banner comments now run 1–11.
      **Two live defects the gate found, both invisible to unit specs:**
      - *The deployed credentials predated the phase.* Every plugin was logging
        `Permissions Violation for Publish to "notify._platform.health.frontend.<id>.v1"` and for
        subscription to `notify._platform.mfe-registry.entries.reset`. The grants were correct in
        `bootstrap-operator.sh`; the minted JWTs were older than they were, so the whole health
        plane was dead in Docker while every spec stayed green. Fixed by
        `./bootstrap-operator.sh --force` + `docker compose down -v && docker compose up --build`.
        Any future credential-affecting phase needs that wipe as part of its own gate.
      - *A curated publisher could not start.* `demo-catalog` crash-looped on
        `PLUGIN_MANIFEST_PATH is required`: `announcer.ConfigFromEnv()` demanded the four
        announce-only variables *before* reading `HEALTH_ONLY`, which made `Validate()`'s
        curated-publisher exemption unreachable from any real deployment. Fixed red-first —
        `HEALTH_ONLY` is now read first and the rest are plain `Getenv`, with `Validate()` left as
        the one place that decides what a given shape of publisher must have. Two specs added in
        `shared/mferegistry/announcer/health_test.go` under BR-AS61.

### Phase 15i — Done 2026-09-02 — The backend health target moves out of the deployment and onto the entry

Raised by the user on 2026-09-02, looking at the plugins table: *"Backend target maps for dynamic
plugins should be localized, i.e. defined in the plugin itself. It should not be baked in. Ideally
this should be the same for static mfe plugins too."* Supersedes **decision 12** of Phase 5d (the
deployment-owned map), which stays in the archive as the record of what was replaced.

#### Design decisions

1. **The plugin declares, the operator approves.** The manifest names the backend service ids the
   plugin depends on; the catalogue entry carries the operator's approval; only the approved list is
   probed. Chosen over "the manifest is authoritative because it is signed" — a signature proves who
   said a thing, not that they may say it. The threat the old map defended against is unchanged: the
   registry's grant is `rpc._platform.health.*.ready.v1`, one token wide across every service, so a
   publisher naming its own target could point the registry at a service it does not own and read
   the answer back through the health decoration. The approval is the gate, and it is the only gate.
2. **Static and dynamic work the same way**, per the user's "ideally the same for static too": both
   are catalogue entries, and the preload file may carry an approval because the preload file *is*
   the operator speaking.
3. **The declaration is publisher-asserted; the approval is platform-owned** (BR-AS70). So
   `backendServices` is inside the signed bytes and `approvedBackendServices` is in
   `CuratedFields()`, cleared by `WithoutCuration()` and refused by `ParseManifest`. Approving a
   plugin cannot un-attest it.
4. **An approval is a subset of what is still declared.** A publisher that drops a service from its
   manifest drops the probe with it, and a stored approval can always be read back as "an operator
   saw this plugin ask for this".
5. **A re-announce carries the approval across**, narrowed by rule 4, applied once before the branch
   cascade in `DecideAnnounce` so the convergence comparison sees the same value on both sides.
   Without it every heartbeat would silently revoke what an operator granted.
6. **The approval is a lifted column** (`approved_backend_services JSONB`, nullable), because a
   signed row is rebuilt from its manifest bytes on every read. NULL means unanswered and `[]` means
   answered-with-nothing; the two are different answers and the nil/empty distinction survives the
   whole round trip.
7. **`not configured` covers "never declared" and "declared, not yet approved".** The shell's health
   vocabulary is closed and adding a seventh state for one screen is not worth it. The Admin UI says
   `awaiting approval` instead, because the actionable wording belongs where the action is.
8. **No new subject and no new grant.** Approving rides the `upsert` the panel already makes.

#### As built

- Domain: `internal/domain/backendservices.go` (new) — `MaxBackendServices`,
  `ValidateBackendServices`, `EffectiveBackendServices`, `carryApproval`; `Entry` gains the two
  fields; `Admissible()` checks the declaration where the rest of the entry's shape is checked.
- Deleted: `domain.HealthTargets` / `NewHealthTargets` / `Dependencies`, `registry.ParseHealthTargets`,
  and the `REGISTRY_HEALTH_TARGETS` env var and its map in `docker-compose.yml`. Tombstone comments
  left at each site.
- Store: the new column in `migrate.go`, read in `currentDoc`, written in `apply`.
- Fixtures: every `lab-shell/plugins/*/public/manifest.json` declares `backendServices` (`[]` for the
  frontend-only ones, `["refdata-service"]` for `demo-catalog`), and `registry.json` carries
  `demo-catalog`'s approval. The old map had silently forgotten `example-plugin-unreachable`, which
  is the failure mode this phase removes.
- Admin UI: a `Backend` column, and one checkbox per declared service in the edit drawer.
- Specs: `registry/health_backend_test.go` (rewritten), `curation_test.go`, `manifest_test.go`,
  `preload_test.go`, `store_integration_test.go`, `lab-shell/.../pluginServices.spec.js`,
  `admin/.../FrontendPluginsPanel.spec.js`. All green.
- Docs: BR-AS62 rewritten in `BUSINESS_RULES-APP-SHELL.md` with its matrix row;
  `ARCHITECTURE-APP-SHELL.md` § health.
- **Not done here:** no acceptance-gate run. `cmd/registry-acceptance` is unaffected by the change
  (it asserts frontend health), but the compose fixtures moved, so the next lab bring-up is the
  first end-to-end proof.

---

---

### Phase 16 — APPROVED (design gate passed 2026-09-23) — `plugin-source: build`: one shell, two catalogue sources, and the demos as plugins

**Status: OPEN 2026-09-23. The design gate passed the same day — the eleven decisions below are
APPROVED, F-1 to F-5 are resolved, and the task checklist is derived. Settled decisions are not
re-opened. Done: 16a, 16b, 16c, 16d, 16e. Not started: 16f to 16i.**

Direction agreed 2026-09-23 after scoping three alternatives. The other two were considered and
rejected — see "Alternatives rejected" at the foot of this phase.

#### The problem this phase closes

`lab-shell/` has two jobs and they have different dependency needs.

1. It is **the lab's demo menu** — a repo-level surface that introduces every demo. Root `CLAUDE.md`
   describes it that way.
2. It is **demo 01's micro-frontend host** — the Phase 1–15 proof of runtime discovery, curation,
   signing, withdrawal and health.

Job 2 has a hard dependency on demo 01, and that dependency is correct for job 2. `main.js` dials
NATS and mints a credential from `accounts-service` *before* it reads the catalogue; the catalogue
comes from `mfe-registry-service`; the trust chain comes from `nats/bootstrap-operator.sh`; every
plugin's container, published port and origin allowlist entry live in demo 01's compose bands
(ADR-055).

Job 1 inherits all of it, and for job 1 the dependency is backwards: the lab's menu cannot show a
demo without one particular demo's Postgres, NATS operator mode and seven services being up. That
also contradicts root `CLAUDE.md` — each demo is self-contained and shares no network with the lab
shell or other demos.

Demo 04 is the case that makes it concrete. It is a sealed unit: one NATS server on 4422, one Go
binary, one Vue app, no Postgres, no operator mode. Its UI is already the right shape for a plugin —
[`App.vue`](../../demos/04-jetstream-cqrs/frontend/src/App.vue) composes `@ui-shell/AppShell.vue`
and swaps separate panel components, exactly like the three apps Phases 10–12 will migrate. But
admitting it through the registry would make demo 04's frontend a demo 01 deployment artifact and
break its seal.

#### The shape: a second catalogue source, not a second shell

One codebase, one menu, two catalogue sources. `plugin-source` is chosen once, at boot; every stage
after the catalogue read is the same code.

| Boot stage | `registry` mode (today) | `build` mode (new) |
|---|---|---|
| Theme, permissions, router skeleton | unchanged | identical |
| `bootShell()` | unchanged | identical |
| Connection | `createShellDialer` mints a credential and dials the WebSocket | **skipped** — a null connection with the same surface |
| Catalogue read | `createRegistryTransport` over `api._platform.mfe-registry.frontend-plugins.read.v1` | `createBuildCatalogueClient`, fetching a generated document over HTTP |
| Trust | signature verified, publisher table, `withheld` / `withdrawn` honoured | none — the generated document is the authority |
| `RemoteAllowlist` | built from validated manifests | **identical, unchanged** |
| `validateRegistryDocument` + per-manifest validation | unchanged | **identical, unchanged** |
| Loading | `createFederatedAdapter` | **identical, unchanged** |
| Live change | push on `notify.*`, then a conditional re-read | none at runtime; the dev server's reload |
| Health plane | probes over NATS (Phase 15) | not started; the Plugins screen reads "not tracked" |
| Revision | monotonic, from Postgres | a hash of the generated document |

The seams already exist and are why this is a small phase rather than a fork. `bootShell()` takes an
**optional** `registryClient`; `createRegistrySession({ connection, client, ... })` takes both
injected. Neither knows it is talking to NATS.

#### What does not change, and why it matters most

**A plugin is byte-identical in both modes.** Same `plugin.js` export shape, same five contribution
kinds, same `public/manifest.json` schema, same federation container naming. A demo plugin built for
`build` mode can later be signed and announced into `registry` mode with **no source change** — only
deployment. That is the property this phase must not trade away, and it is what keeps `build` mode a
convenience rather than a second architecture.

#### BR-AS03 is preserved, not bent

The 2026-08-28 working assumption rejected "static JSON inside the shell's bundle" because it makes
BR-AS03 only nearly true — adding a plugin would still mean redeploying the shell. That objection
does not apply here and the distinction is load-bearing: the build-generated document is **generated and
served**, never bundled. The host declares no remotes at build time, and
`tools/hostBundleFingerprint.mjs` must stay green across adding a build-sourced plugin. If it does not, the
design is wrong.

#### Where demo plugins live

**In the demo, not in `lab-shell/plugins/`.** `demos/04-jetstream-cqrs/frontend/` *becomes* the
plugin. Nothing moves, no seal is broken, and no demo `CLAUDE.md` needs an edit beyond recording
that its frontend now also builds as a remote.

The catalogue generator globs `demos/*/frontend/public/manifest.json`. The manifest file is the
**opt-in**: a frontend without one is not a plugin. Demo 01's three apps therefore do not appear,
which is correct — they arrive through Phases 10–12, into `registry` mode.

#### Naming — settled at the gate 2026-09-23

The setting is **`plugin-source`**, with values **`build`** and **`registry`**. Three earlier names
were tried and dropped, and the reasons are kept because each was dropped for saying something
untrue:

- **`local`** — rejected once it was established that this catalogue will be **hosted publicly**.
  It describes where the catalogue comes from, not where the shell runs.
- **`manifest`** — rejected as not an opposition: `registry` mode serves validated manifests too, so
  the pair invites "but doesn't registry mode use manifests?"
- **`bundled`** — rejected as the opposite of the truth. Plugin code is never in the shell's bundle
  in either mode; each plugin is a separate `remoteEntry.js` loaded at runtime. Naming the mode
  `bundled` would assert exactly the misunderstanding BR-AS03 exists to prevent.

`plugin-source` rather than `plugin-mode` or `mode`, accepting one known cost: the compose label
`com.nats-tech-lab.mfe.source` (`announced` / `preload`) already exists. The two sit at different
levels — `mfe.source` is how one entry got curated *inside* `registry` mode; `plugin-source` is
where the whole catalogue comes from — and the overlap is documented here rather than designed out.

#### Proposed design decisions — UNAPPROVED, for the gate

1. **A second catalogue source, not a second shell.** **APPROVED 2026-09-23 as proposed.** `demo-shell` as a separate application was considered and
   rejected: it duplicates ~1,200 lines of loader, contribution registry, extension points and
   routing, and produces two demo menus that will drift. The registry half is what `build` mode
   skips, and skipping is cheaper than copying.

2. **`plugin-source` is selected in one place, and the build is `build` mode's trust anchor.**
   **APPROVED 2026-09-23, amended at the gate.** The proposal said `build` mode was dev-only and
   gated on `import.meta.env.DEV`. That was rejected on a corrected premise: `build` describes how
   the catalogue is obtained, not where the shell runs, and this menu **may** be hosted — public
   hosting is supported, not committed to.

   So `pluginSource.js` selects the mode from an explicit environment variable, with no `DEV`
   requirement — `build` mode is a legitimate deployment. The trust the mode gives up is replaced,
   not waived. In `build` mode:

   - the catalogue **must** be produced by the same build that produced the shell, from this
     repository. It is never fetched from a service and never editable after deploy, so there is no
     runtime admission path for an entry to arrive through;
   - every `remote.url` **must** be same-origin with the shell. ~~or on a short allowlist fixed at
     build time.~~ **The cross-origin exception was closed by decision 6**, which makes same-origin
     unconditional in `build` mode via a single public path layout. `registry` mode's own origin
     handling — `RemoteAllowlist` and BR-AS45's manifest-fetch allowlist — is untouched.

   Both are specs, not conventions. Together they are arguably a stronger anchor than `registry`
   mode's runtime signature check, because nothing can be admitted at runtime at all — which is why
   dropping the `DEV` gate costs nothing real.

3. **The null connection has the real surface.** **APPROVED 2026-09-23 as proposed.** `state.epoch`, `subscribe`, `request`, `start`,
   `flush`, `close`. `createRegistrySession` then runs unmodified in both modes. The alternative —
   branching inside the session on whether a connection exists — puts mode knowledge into a file
   whose whole job is lifecycle.

4. **The build catalogue client returns the transport's exact result shape, and tells an empty
   catalogue apart from a broken one.** **APPROVED 2026-09-23, extended at the gate.** The shape is
   `{ ok, unchanged, revision, plugins, degraded, heldRevision, fetchedAt }`, so `decideRead` stays
   pure and never learns that sources exist.

   `degraded` is always `false`: there is no service here to be half-available. The extension is the
   part the proposal left implicit, and it matters wherever this menu is served — two very
   different situations both end in an empty screen, and they must not look alike:

   - a catalogue that generated correctly and holds **zero entries**, because no demo carries a
     `public/manifest.json` yet, is a **successful empty read** (`ok: true, plugins: []`);
   - a catalogue that is **missing or will not parse** is a **failed read** (`ok: false`, with a
     code), which `decideRead`'s existing failure branch already renders with a reason.

   The rejected alternative was to treat any absence as emptiness. It is a simpler client and it
   makes a broken build indistinguishable from a lab with no demos — debuggable only by guessing.

5. **The catalogue is generated by the build, emitted as a static asset, and never written to the
   repo — and the URLs it carries stay relative.** **APPROVED 2026-09-23, amended at the gate.**

   A Vite plugin scans the demos. In dev it serves the document from memory and re-scans when a
   manifest changes. In `npm run build` it emits the document into `dist/` as a separate static
   asset, deployed with the shell. Changing catalogue membership therefore requires a rebuild, which
   is decision 2's anchor holding rather than a limitation to work around. A committed
   `plugins.json` is rejected separately: it is a second source of truth that will disagree with the
   folder.

   **Generating the catalogue at container start-up was rejected.** It buys per-deployment
   membership changes and pays for them by breaking decision 2 — a catalogue the shell's build did
   not produce is a runtime admission path, and the trust anchor goes with it.

   **Amended at the gate: where a plugin lives is part of the trust boundary, not a deployment
   detail.** The proposal was to hand "which plugins" (build) and "where they live" (deploy) to
   different owners, mirroring BR-AS71/72. That is wrong here and the reason is exact: a fixed
   plugin list with freely replaceable remote URLs is a fixed list of *names*, and it can still be
   pointed at somebody else's code. The build must fix both.

   So the generator emits **relative** URLs, resolved at load against the shell's own origin. One
   build then runs under any hostname without the catalogue being rewritten, and decision 2's
   same-origin property holds by construction rather than by configuration. Manifests are already
   path-only under BR-AS72, so in `build` mode the generator leaves the URL alone — it stamps no
   origin at all.

   Decision 6 settles path layout and hosting shape only. **An arbitrary deployment-time origin
   override reopens decision 2 and is out of scope for this phase.**

6. **One public path layout, served two ways.** **APPROVED 2026-09-23, replacing the proposal and
   carrying two qualifications.** The proposal had the generator stamp each plugin's dev-server
   origin onto `remote.url`. Decision 5's amendment removes that job entirely — URLs stay relative —
   but it exposes what the old wording hid: in dev every plugin runs on its own Vite server, and a
   different port is a different origin, so a naive same-origin rule would refuse every plugin on a
   developer's own machine.

   The contract, which is the decision:

   > Build-mode plugins load under `/plugins/<id>/…` on the shell's origin. Development proxies and
   > hosted asset placement implement that same public path layout. The catalogue and development
   > proxy mappings derive from the same discovery data.

   **Amended 2026-09-23 (R-2): "proxy mappings" is plural.** There are two — static plugin assets
   under this prefix, and BR-AS79's readiness routes, which are deliberately *not* under it. Both
   still derive from the one repository scan, so the contract holds; the singular reading would
   have forbidden the second.

   Dev and hosted therefore share one URL contract and differ only in serving mechanism. Same-origin
   becomes a property rather than a configured exception.

   **Qualification 1 — the prefix covers the whole plugin.** Entry module, lazy chunks, CSS, fonts
   and images, not just `remoteEntry.js`: an entry proxied alone that then pulls its dependencies
   from another port defeats the rule while appearing to satisfy it. Dev HMR must also work through
   the proxy. **This is verified against demo 04 before the layout is treated as settled** — it is a
   task-list exit condition, not an assumption.

   **Qualification 2 — only the `build` mode cross-origin exception is removed.** Same-origin URL
   validation stays and is what this decision makes unconditional. Nothing `registry` mode relies on
   is touched: `RemoteAllowlist` is unchanged in both modes, and BR-AS45's manifest-fetch origin
   allowlist is unchanged. (An earlier framing at the gate said the allowlist "becomes dead code and
   can be deleted". That was wrong and is corrected here.)

   **Consequence for deployment:** a hosted shell must ship the plugin **assets** at those paths.
   Emitting the catalogue alone is not sufficient, so collecting each plugin's build output into the
   shell's served tree is a deliverable of this phase, not a hosting detail left to whoever deploys
   it.

7. **A demo whose backend is down must say so plainly — and availability is split between the shell
   and the plugin.** **APPROVED 2026-09-23 with six amendments.** Demo 04's UI needs its NATS
   WebSocket and its command API. The plugin loads fine without them and then fails in a way that
   reads as a plugin bug. The shell owns a shell-rendered unavailable state, keyed off a readiness
   probe the plugin declares. This is the one genuinely new piece of design in the phase and it must
   not be deferred.

   The proposal stopped at a pre-mount probe. That is not sufficient, and the amendments say why:

   1. **Probe readiness, not reachability.** An HTTP server answering does not prove demo 04's NATS
      connection or command handling are ready. The probe asserts the thing the UI actually needs.
   2. **Unavailable is not unknown.** A timeout or a blocked request means "cannot reach demo
      services", never a definitive "the demo is stopped". Three states, not two.
   3. **Retry and recovery are part of the design.** Re-check on opening the demo, offer Retry in
      the unavailable panel, and give the **menu card its own refresh policy** — a probe that runs
      only before mounting never populates the card, which is the place people actually look.
   4. **Instructions are audience-appropriate.** A local operator gets a start command. A visitor to
      a hosted shell gets "demo services are unavailable" and no instruction to start a backend on
      their own machine. ~~The shell already knows which it is; note that this uses the dev/hosted
      distinction for **wording only**.~~ **Superseded 2026-09-23 by F-5:** audience is **declared
      by the deployment, defaulting to visitor**, and is never inferred from dev, preview or
      production state. Decision 2 removed that distinction from the trust path, and F-5 removes it
      from the wording path too — it does not come back here in any form.
   5. **A successful probe guarantees nothing after mounting.** So ownership splits: the **shell**
      owns the consistent status and the pre-mount panel; the **plugin** owns connection loss and
      operation errors while it runs. A shared presentation component keeps both sets of messages
      looking like one system.
   6. **Scoped to demo availability.** The readiness declaration is optional and demo-facing;
      `registry` mode neither requires nor reads it in this phase. Extending the manifest contract
      for every registry plugin is a separate decision and is deliberately not taken here.

8. **Health is absent, not fabricated — and the existing vocabulary already says so.**
   **APPROVED 2026-09-23. Implementation checked at the gate; two proposed changes withdrawn.**

   The gate proposed adding an *unknown* / *not tracked* distinction and re-wording "healthy". Both
   were checked against the code and **neither is needed**:

   - `registry` mode already carries six states (`healthPlane.js`, and `domain/health.go`):
     `healthy`, `unavailable`, `stale`, `unknown`, `not configured`, `not applicable` — split
     frontend/backend, so a live UI over a dead API stays visible. `not configured` is already the
     *not tracked* case and already does not age, because a configuration answer is not an
     observation.
   - The wording is already honest by design: the domain states that the last three values exist
     "because 'we did not check' must never be spelled the same way as 'we checked and it is fine'".

   So `build` mode adds **no state, no timer and no freshness rule**. It reuses the existing
   vocabulary and the existing labels. No synthetic "healthy" is ever emitted.

   **One genuine gap was found and must be closed by this phase.** Since Phase 15,
   `HealthNotConfigured` is **backend-only**: on the frontend plane a plugin reports about itself on
   a subject derived from its own id, so an enabled plugin is "either heard from or absent, and
   never 'not configured'". In `build` mode nothing is listening on that plane at all, so every
   plugin's frontend signal would rest at `unknown` — which means "monitoring exists, no reading
   yet" and would be false. `build` mode needs frontend health to read as *not tracked*. Closing
   this is display-and-mapping work in the shell; it must not re-open `not configured` on the
   registry service's frontend plane.

   Decision 7's readiness result and this health signal stay separate. Readiness answers "can this
   demo be mounted"; health answers "is this plugin being watched, and what was last seen".
   **Neither signal establishes the other**, and neither is rendered using the other's control.

9. **`registry` mode's protocol and lifecycle behaviour are preserved.**
   **APPROVED 2026-09-23 with a tighter boundary than proposed.**

   The proposal said "untouched" and meant behaviour. That is too loose: **display states are
   observable behaviour too.** The claim is therefore stated as *protocol and lifecycle behaviour
   are preserved* — no subject changes, no grant changes, no boot-order changes, no plugin rebuilt,
   and no change to how health is calculated.

   **The display carve-out, and its hard edge.** Shared UI may distinguish `not tracked` from
   `unknown` and clarify health labels **using existing health data only**. If the work turns out to
   need a new freshness timer, an expiry rule, polling, or any change to how health is *calculated*,
   it has left the carve-out and needs its own decision. (As decision 8 records, the check at the
   gate found `registry` mode already carries both states and honest labels, so the expected size of
   this carve-out is now near zero — it covers the `build`-mode frontend mapping and nothing else.)

   **Tests.** The Phase 15 acceptance gate is unchanged. Add focused coverage for the display
   distinctions, and update wording assertions where necessary **without weakening any existing
   behavioural check**.

   If a BR-AS rule has to be amended or an existing spec changes meaning, the split was done in the
   wrong place.

10. **Catalogue source and demo role are independent, and this phase must not tie them together.**
    **REWRITTEN AND APPROVED 2026-09-23.** The proposal read "`build` mode is a showcase,
    `registry` mode is the validation". That is a category error and is withdrawn.

    1. **`plugin-source` describes how plugins are discovered. It says nothing about what a demo
       proves.** Both sources must carry a demo whose declared role is showcase, validation, or
       both. A demo keeps the role it declared; being hosted under one source or the other never
       changes it, and no future demo may have its role inferred from its source.
    2. **Demo 04 keeps its own declared role.** This phase uses demo 04 to **demonstrate and
       verify the `build`-source integration**. That is the phase's use of the demo, not a
       redefinition of the demo.
    3. **Phase 15's acceptance gate is regression coverage for `registry` mode.** It is not "the
       validation", and nothing in this phase is excused from evidence by pointing at it.
    4. **Byte-identical plugin code does not make the phase evidence-free.** What is new here is the
       catalogue generation, the asset routing, the lifecycle and the failure handling — none of
       which the plugin's bytes say anything about. Each must be verified on demo 04 before the
       layout is treated as settled (this is the same verification decision 6 already requires).
    5. **No comparative performance study, unless a specific decision needs one.** And the earlier
       claim that fewer services are therefore "faster" is **withdrawn**: fewer moving parts is not
       a measurement, and nothing in this phase may report it as one.

    For the record, against the playbook: validation is defined by **reproducible experiments and
    measured evidence**, not by the absence of a frontend. A script may run the experiment while a
    frontend presents the findings — which is exactly why a demo with a UI can still be a
    validation, and why the withdrawn framing would have been wrong even if the roles had lined up.

11. **A demo's `plugin-source` is derived and displayed; it is never encoded in a path.**
    **APPROVED 2026-09-23.** Direction agreed
    2026-09-23, answering "which demos are `build` and which are `registry`, and why can I not see
    it from the tree?" Both facts already exist in files — a frontend carrying
    `public/manifest.json` under `demos/*/frontend/` is a build-sourced plugin, and a frontend listed in
    demo 01's compose bands is a registry plugin — so the source is read, never restated. It is then
    surfaced in exactly two places:

    - a **generated `demos/README.md` table** (demo, frontend, `plugin-source`, port, one line each), written
      by the same scan the catalogue generator already performs, so it cannot drift from the folder;
    - a **page-level "Catalogue source: Build" / "Catalogue source: Registry" statement on the
      Plugins screen**.

    **Amended at the gate 2026-09-23**, replacing an earlier proposal of a per-demo-card badge and a
    per-row column. Three corrections, all of which change what the label is allowed to claim:

    1. **Source belongs to the shell's current configuration, not permanently to the demo.** The
       same plugin can be discovered through either source. The label must be worded as a statement
       about this shell right now, never as a property of the demo.
    2. **One statement, not one per row.** The shell selects one source for the whole catalogue, so
       repeating the identical value in every row adds no information. A per-plugin column becomes
       correct only if mixed sources are ever supported — and that is a separate decision, not a
       default.
    3. **It stays off the demo cards.** A demo card answers what the demo does and whether it is
       ready (decision 7). Discovery plumbing is an operator question and belongs on the operator's
       screen.

    **Keep registry entry provenance separate.** The existing `announced` / `preload` labels on demo
    01's compose bands answer a different question — how an entry reached the registry — and are not
    merged with, renamed after, or displayed alongside `plugin-source`.

    The path stays silent on purpose. Decision 1's promise is that a plugin is byte-identical in
    both modes, so a demo moving from `build` to `registry` must be a deployment change — and a
    source-named folder would make it a rename across the repository instead. See "Alternatives
    rejected" for the subfolder proposal this replaces.

#### Contradictions found while deriving the checklist — all five resolved 2026-09-23

Derived after the gate passed, then **resolved the same day**. Each was a collision between an
approved decision and code that already exists, or a choice the gate did not take. The finding is
kept with its resolution so the reasoning is not lost.

**F-1 — A new top-level manifest field breaks drift checking in `registry` mode.**
*Was blocking decision 7.* `CompareManifest` calls `decoder.DisallowUnknownFields()`
(`registry/internal/domain/drift.go:70`), deliberately: silently dropping an unfamiliar field would
claim agreement about content that was never compared. `domain.Entry` has no free-form area, so a
manifest carrying a `readiness` key would return `invalid-manifest` on every drift pass — breaking
decision 1 and decision 9 at once. (The shell's own `manifestSchema.js` is not the problem: it
already ignores fields it does not read, which is why `backendServices` appears in manifests and
nowhere in that file.)

**RESOLVED — readiness lives in a sibling, demo-owned metadata file.** `manifest.json` is unchanged
and stays the registry contract. Decision 9 is not re-opened, the Phase 15 gate is not re-argued,
and `Entry` gains no field. Cost accepted: a second declaration file next to the manifest.

**Refined 2026-09-23 by R-1.** The first wording had the *build* catalogue generator read the
sibling file, which made readiness disappear in `registry` mode. It is instead read into a
**shell-owned demo catalogue** consumed in **both** sources, keyed by a stable demo identifier —
still without the registry seeing it and still without the contract being extended. See R-1 for the
authority boundary that keeps this safe.

**F-2 — Where the `build`-mode health mapping lives.** Decision 8 settles *what* is shown
(`not configured` on both planes) and forbids re-opening `not configured` on the registry service's
frontend plane. It did not say where the substitution happens.

**RESOLVED — the smallest presentation-layer mapping, and no observation is fabricated.** In `build`
mode the health plane is **not constructed at all**, and the injected health object carries an
explicit `monitored: false` alongside its empty `signals`. The render site reads that flag and says
`not configured`, reusing the existing `HEALTH_STATE.NOT_CONFIGURED` constant so the wording stays
shared. Rationale, in order of why it was chosen over the alternatives:

- `healthPlane.js` is **not** touched. It owns `registry` mode's semantics — freshness, ageing,
  merge order — and a mode-aware default inside it would put source knowledge into the one file
  whose job is measurement. Its `unknown()` for an absent reading stays correct for `registry` mode.
- `healthText.js` is **not** branched. It answers "how is one signal said"; `monitored: false` is
  not a signal, and giving that file a second question to answer is how the two drift apart.
- No `HealthSignal` object is synthesised. A fabricated `not configured` reading would be an
  observation nobody took, which is exactly what BR-AS80 forbids — the flag says *there is no
  monitoring*, which is a fact about the deployment, not about the plugin.
- Blast radius is composition plus render site only: `main.js` and the Plugins screen.

Note the nav dot needs nothing: `healthAttention` already draws a mark only for `unavailable`, so a
`build`-mode shell is quiet by existing behaviour.

**F-3 — The readiness probe's own origin.** Decision 6 makes plugin **assets** same-origin. A
readiness probe is not an asset: demo 04's command API is on port 20402, a different origin.

**RESOLVED — readiness stays same-origin through an explicit demo-specific proxy route, and no
cross-origin exception is introduced.** Readiness routes are **distinct from static plugin assets**
and **must not** sit under the `/plugins/<id>/…` asset prefix, because one is a dynamic call to a
demo's backend and the other is a file. Both environments implement the same public route:

- **Development** — a Vite dev-server proxy entry, derived from the same discovery data that
  produces the catalogue and the asset proxy, forwarding the readiness route to the demo's own
  backend port.
- **Hosted** — a reverse-proxy rule shipped as part of the deployment, forwarding the same public
  route to wherever the demo's backend runs.

CORS is not added to any demo backend. Decision 2's same-origin property is about `remote.url` and
is unaffected; this extends the same discipline to the one dynamic call the shell makes.

**F-4 — Demo 04's frontend is not plugin-shaped.** It renders `@ui-shell/AppShell.vue` itself
(`App.vue:7,69`) and has no router — it swaps panels.

**RESOLVED — one plugin route, existing panels preserved, and this is not a navigation redesign.**
Demo 04 contributes a **single** `route` contribution whose component is its existing panel
container, with its current panel/tab interaction intact. The outer chrome comes from `lab-shell`
when embedded, satisfying BR-AS09 — so the plugin entry must not render `AppShell` itself. Demo 04's
standalone entry keeps its own `AppShell` and keeps working, both entries built from the same
sources by the same build. Splitting panels into several routes was considered and **rejected**: it
would change how the demo is navigated, which no approved decision asked for. Demo 04's dev port
remains 20401, outside CLAUDE.md's 7100-7199 band; under decision 6 the port is visible only to the
dev proxy, and it is recorded here rather than changed.

**F-5 — Operator versus visitor wording.** Decision 7 said "the shell already knows which it is",
read from the dev/hosted distinction. A production build run locally through `vite preview` is
hosted by that test and would show a visitor's message to an operator.

**RESOLVED — audience is declared, never inferred.** The dev/preview/build distinction is **not**
used for this, which also removes decision 7's last dependency on it. Two small, explicit pieces:

- the demo's sibling metadata file (F-1) may carry an optional local run command — it is
  *information about the demo*, always true, and the demo owns it;
- the **shell deployment** declares its audience explicitly, defaulting to **visitor**. Run
  instructions are shown as recovery steps only when the deployment has declared itself an operator
  shell.

The default is the safe one: a deployment that declares nothing never tells a stranger to start a
backend on their own machine. An operator shell shows the command it was given; a visitor shell
shows the unavailable message alone.

#### Remaining conflicts after the F-1 to F-5 resolutions — recorded 2026-09-23

Two consequences, one cleared contradiction. Neither open item blocks a task; both are stated so
they are not discovered later as surprises.

**R-1 — A demo moving `build` to `registry` would have lost its readiness panel. RESOLVED
2026-09-23: readiness is independent of `plugin-source`.** The first resolution of F-1 tied
readiness to the build catalogue generator, so the registry never read it and the panel vanished on
a mode change. That is now corrected without touching `manifest.json` and without extending the
registry contract:

- Readiness **stays** in the sibling, demo-owned metadata file. Registry manifests are unchanged.
- The shell consumes that metadata through its own **demo catalogue** — shell-owned, generated from
  the same repository scan — and does so in **both** sources.
- A plugin and a demo are associated by a **stable demo identifier**, not by which source discovered
  the plugin.

**The trust separation is the whole point, and it is a hard boundary.** Registry discovery alone
decides which plugins exist and where they load from. Readiness metadata is **decoration on a
demo**: it can never admit a plugin, never enable a disabled one, never contribute a route, and
never supply or override a `remote.url`. A demo catalogue entry with no admitted plugin shows
nothing.

Readiness routes and audience configuration are available in **both** deployments, so an operator
running `registry` mode gets the same panel and the same wording rules. A registry plugin with no
associated lab demo needs no readiness metadata and is unaffected — absence is the normal case, not
a fault.

BR-AS78's "moving between sources is a deployment change only" is therefore now true of the plugin
**and** of its readiness, which is what it always claimed.

**R-2 — There are now two proxy mappings, not one.** Decision 6 says the catalogue and the
development proxy mappings derive from the same discovery data. After F-3 there are two mappings —
static assets under `/plugins/<id>/…` and readiness on its own demo-specific route. Both still
derive from that one scan, so the decision holds as written; the plural is recorded here because the
singular wording would otherwise read as forbidding the second.

**Cleared — decision 7's amendment 4.** It said audience was read from the dev/hosted distinction.
F-5 replaces that with a declared audience, and the amendment is struck through in place rather than
deleted, so the reversal is visible.

#### Business rules — wording derived 2026-09-23, UNAPPROVED

**Next free ID confirmed as BR-AS75**; BR-AS74 is the highest defined
(`demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md:1141`). Seven rules, one more than the gate's
proposed coverage listed: decision 6's path layout earns its own rule rather than being folded into
the trust rule, because it is what makes same-origin true rather than merely required.

- **BR-AS75 — The catalogue has exactly one source, chosen once, and `build` mode replaces runtime
  trust with build-time trust.** The shell **must** resolve `plugin-source` to exactly one of
  `build` or `registry` from an explicit environment variable, once, before any catalogue read, and
  **must not** vary it per plugin. `build` mode **must not** be conditioned on a development build:
  it is a legitimate deployment. In `build` mode the catalogue **must** be produced by the same
  build that produced the shell, from this repository; it **must not** be fetched from any service
  and **must not** be editable after deployment, so no runtime admission path exists. Every
  `remote.url` **must** resolve same-origin with the shell. `registry` mode's own origin handling —
  `RemoteAllowlist` and BR-AS45's manifest-fetch allowlist — **must** be unchanged.

- **BR-AS76 — The `build` catalogue is generated and served, never bundled, and never committed.**
  The catalogue **must** be produced by scanning the repository for demo frontends carrying
  `public/manifest.json`, served from memory in development and emitted into `dist/` as a separate
  static asset by the production build. No plugin's code **may** enter the shell's bundle, so
  BR-AS03 holds unchanged in both sources and remains provable by
  `tools/hostBundleFingerprint.mjs`. A generated catalogue **must not** be written back into the
  repository, because a committed copy is a second source of truth that will disagree with the
  folder. A catalogue that generated correctly and holds zero entries **must** be a successful empty
  read; a catalogue that is missing or will not parse **must** be a failed read carrying a code.
  These two **must not** render alike.

- **BR-AS77 — Build-mode plugins are served under one public path layout, and the prefix covers the
  whole plugin.** Build-mode plugins **must** load under `/plugins/<id>/…` on the shell's origin.
  Development proxies and hosted asset placement **must** implement that same public path layout,
  and the catalogue and the development proxy mappings — plural: static assets and the readiness
  routes of BR-AS79 — **must** all derive from the same discovery data.
  The prefix **must** cover the entry module, lazy chunks, CSS, fonts and images — not the entry
  alone — and development hot module replacement **must** work through it. The generator **must**
  emit relative URLs and **must not** stamp any origin. A hosted deployment **must** ship each
  plugin's built assets at those paths; emitting the catalogue alone is not sufficient. Dynamic
  demo routes — readiness under BR-AS79 — **must** be served from routes distinct from this asset
  prefix and **must not** be placed under `/plugins/<id>/…`, because a file and a call to a demo's
  backend are not the same kind of thing and **must not** share a namespace.

- **BR-AS78 — A plugin is identical across both sources.** A plugin's built output, its
  `manifest.json` and its contributions **must** be byte-identical whether it is discovered by
  `build` or by `registry`. Moving a plugin between sources **must** therefore be a deployment
  change only, and **must not** require a rebuild, a manifest edit, a code change or a move within
  the repository. No path, folder name or identifier **may** encode which source discovered a
  plugin.

- **BR-AS79 — Demo availability is a three-state shell concern before mount, and a plugin concern
  after it.** Where a demo declares a readiness check, the shell **must** run it before mounting
  that demo and **must** render the unavailable state itself rather than letting the plugin mount
  and fail. The check **must** assert that the demo's services are *ready*, not merely that a port
  answers. Three states **must** be distinguished: available; unavailable; and unknown, where a
  timeout or a blocked request **must** read as "cannot reach demo services" and **must not** claim
  the demo is stopped. The shell **must** re-check when the demo is opened, **must** offer a retry
  in the unavailable panel, and **must** give the menu card its own refresh policy, because a check
  that runs only before mount never populates the card. After mount, the **plugin** owns connection loss and operation
  errors; a successful check guarantees nothing about the time after it. Both sides **must** use one
  shared presentation component so the messages read as one system.

  The readiness declaration **must** live in a sibling, demo-owned metadata file beside
  `manifest.json`. `manifest.json` **must** be unchanged by this rule, and the registry contract
  **must not** be extended: a manifest carrying an unfamiliar top-level field fails `registry`
  mode's drift check by design.

  **Readiness is independent of `plugin-source`.** The shell **must** consume that metadata through
  its own shell-owned demo catalogue, generated from the same repository scan, in **both** sources,
  and **must** associate a demo with a plugin by a **stable demo identifier** — never by which
  source discovered the plugin. Readiness routes and audience configuration **must** be available in
  both deployments.

  **Readiness metadata is decoration on a demo and carries no authority.** It **must not** admit a
  plugin, enable a disabled one, contribute a route, or supply or override any `remote.url`.
  Discovery — `registry` or `build` — alone decides which plugins exist and where they load from. A
  registry plugin with no associated lab demo **must** need no readiness metadata, and its absence
  **must not** be reported as a fault.

  The readiness request **must** be same-origin with the shell, served through an explicit
  demo-specific proxy route in both development and hosted deployments. No cross-origin exception
  **may** be introduced and no demo backend **may** be given CORS for this purpose.

  Audience **must** be declared, never inferred: the development, preview or production nature of a
  build **must not** decide it. A deployment **must** default to treating its reader as a visitor,
  and **may** declare itself an operator shell, in which case a local run command carried by the
  demo's metadata **may** be shown as a recovery step. A shell that has declared nothing **must not**
  show run instructions.

  This rule is **scoped to demo availability**: the readiness declaration is optional, and
  `registry` mode neither requires nor reads it.

- **BR-AS80 — Health is reported as measured, or reported as absent; it is never assumed.** The
  shell **must not** display a health state it did not receive a measurement for. `not configured`
  (nothing is set up to watch) and `unknown` (watching exists, no current reading) **must** remain
  distinct, and `not configured` **must not** age, because a configuration answer is not an
  observation. In `build` mode the health plane **must not** be constructed, and every plugin's
  frontend and backend signals **must** read as `not configured`, never as `unknown`, `healthy` or
  `stale`. That mapping **must** be made at the presentation layer, from an explicit
  "monitoring is not configured" flag on the injected health object. No `HealthSignal` **may** be
  synthesised to produce it, `healthPlane.js` **must not** become source-aware, and `registry`
  mode's health semantics **must not** change. A health label **must** describe what was actually measured and **must not** imply the
  plugin's code works. Demo readiness under BR-AS79 and plugin health under this rule **must** stay
  separate: neither establishes the other, and neither **may** be rendered using the other's
  control.

- **BR-AS81 — `plugin-source` is a fact about the running shell, stated once, and derived from
  files.** The active `plugin-source` **must** be stated once at page level on the Plugins screen
  and **must not** be repeated per row while one source serves the whole catalogue. It **must not**
  appear on demo cards, which answer what a demo does and whether it is ready. It **must** be
  worded as a property of the current shell configuration, never as a permanent property of a demo.
  Where a demo's source is listed outside the shell it **must** be derived by the same scan the
  catalogue generator performs, never restated by hand. The registry entry provenance labels
  `announced` and `preload` answer a different question and **must not** be merged with, renamed
  after, or displayed alongside `plugin-source`.

#### Task checklist — derived 2026-09-23, UNAPPROVED

Every task names the decisions it implements and the rules it must satisfy. **New `build`-source
behaviour** is work that did not exist before. **Registry regression** is coverage that proves
`registry` mode did not move; it adds no feature and no rule.

**16a — Source selection and the null connection. DONE 2026-09-23.** *Decisions 2, 3. Rule BR-AS75.*
New: `pluginSource.js` resolving `build` or `registry` from an explicit environment variable, once,
before any catalogue read; a null connection exposing the real surface (`state.epoch`, `subscribe`,
`request`, `start`, `flush`, `close`) so `createRegistrySession` runs unmodified; `main.js` rewired
so the connection is no longer acquired unconditionally ahead of the catalogue read.
Acceptance: the source resolves once and cannot vary per plugin; `build` mode boots with no broker,
no credential mint and no call to `accounts-service`; `registrySession.js` and `readPolicy.js` are
unchanged files. Regression: `registry` mode's boot order, subjects and grants unchanged.

**16b — The catalogue generator and the build client. DONE 2026-09-23.** *Decisions 4, 5. Rules BR-AS76, BR-AS81.*
New: a Vite plugin scanning `demos/*/frontend/public/manifest.json`, serving from memory in dev with
a re-scan when a manifest changes, and emitting a static document into `dist/` on build; a build
catalogue client returning the transport's exact shape
(`{ ok, unchanged, revision, plugins, degraded, heldRevision, fetchedAt }`) with `degraded` always
`false`.
Acceptance: zero entries is `ok: true, plugins: []`; a missing or unparseable catalogue is
`ok: false` with a code, and the two render differently; nothing is written back into the
repository; `tools/hostBundleFingerprint.mjs` still proves no plugin code in the shell bundle.

**16c — One public path layout, and the assets to fill it. DONE 2026-09-23.** *Decision 6. Rule BR-AS77.*
New: `/plugins/<id>/…` as the single public layout for static plugin assets; a dev proxy derived
from the same discovery data as the catalogue; collection of each plugin's build output into the
shell's served tree. **Readiness routes are built in 16e and are deliberately not under this
prefix** (F-3).
Acceptance (exit condition, verified **on demo 04**, not assumed): entry module, lazy chunks, CSS,
fonts and images all load through the prefix; HMR works through the proxy; no request escapes to a
plugin's own port; no readiness route appears under `/plugins/<id>/…`. Regression:
`RemoteAllowlist` unchanged; BR-AS45's manifest-fetch allowlist unchanged; same-origin validation
still enforced, only the `build`-mode cross-origin exception removed.

**Verified 2026-09-23 on demo 04**, under a temporary `public/manifest.json` and a temporary
`base: '/plugins/demo-04/'`, both removed afterwards; 16d makes them permanent. Through the shell's
own origin at `/plugins/demo-04/…`: the entry, source modules, lazy chunks, scoped CSS, a `woff2`
font and a `png` all answered 200, `[vite] connected` confirmed HMR through the prefix, and no
request escaped to port 20401. The build copied the demo's built output into
`dist/plugins/demo-04/`, and an opted-in demo with no built output failed the build. The
`build`-mode cross-origin exception was already struck from decision 2 at the gate and had no code
to remove; `RemoteAllowlist` and BR-AS45 are unchanged files.

**16d — Demo 04 becomes a plugin.** *Decisions 1, 10. Rule BR-AS78. Resolves F-4.* **DONE 2026-09-23.**
New: a **single** `route` contribution whose component is demo 04's existing panel container, with
its current panel/tab interaction unchanged; the plugin entry must not render `@ui-shell/AppShell`,
because `lab-shell` supplies the outer chrome when embedded (BR-AS09); `public/manifest.json` added.
Demo 04's standalone entry keeps its own `AppShell` and keeps working, both entries built from the
same sources by the same build.
Acceptance: demo 04 runs standalone and as a plugin from that one build, byte-identical, with no
manifest edit or code change between the two; its navigation is **unchanged** — no panel becomes a
separate route, and no menu, breadcrumb or tab is redesigned; the phase demonstrates and verifies
build-source catalogue generation, asset routing, lifecycle and failure handling on it. **Demo 04
keeps its own declared role; this task does not redefine it, and no comparative performance claim is
made.** Port 20401 is left as it is and is visible only to the dev proxy.

**Verified 2026-09-23.** Demo 04's frontend now builds `index.html` and `remoteEntry.js` from one
`vite build`, with `base: '/plugins/demo-04/'` and a `demo_04` federation container exposing
`./plugin`. The body both entries render was lifted into `components/LessonPanels.vue` and
`view/useDemoState.js`; `App.vue` kept its own `AppShell` and its slots, and the new
`plugin/OdometerRoute.vue` renders none. No panel became a route, no tab or menu changed, and demo
04's own suite went 491 → 502 specs with the new `src/plugin.spec.js` and no existing spec relaxed.

Through the shell at `http://localhost:7110/demo-04` in `plugin-source: build`: the catalogue
generated one entry from the repository scan, 100 requests answered under `/plugins/demo-04/…`
(entry, source modules, lazy chunks, CSS, a `png`, `.md` and `.vue` files), **none escaped to port
20401**, and both lessons and their tabs behaved as they do standalone. `hostBundleFingerprint.mjs
--verify` still reports `e8bf0d98…` unchanged — the strongest form of BR-AS03's claim, now that a
real plugin exists rather than a placeholder. The production build collected the demo's output into
`dist/plugins/demo-04/`. Standalone still runs: `http://localhost:20401/` redirects to
`/plugins/demo-04/` and renders its own chrome.

One gap, deliberately left to 16e: the command API on `20402` is still an absolute cross-origin URL,
so the write side does not work from inside the shell. Reads do, because a WebSocket is not subject
to CORS. F-3's answer is a same-origin proxy route, not a CORS widening on `cqrs/names.go`.

**16e — Demo readiness, in both sources.** *Decision 7. Rule BR-AS79. Resolves F-1, F-3, F-5, R-1.* **DONE 2026-09-24.**
New: a **sibling, demo-owned metadata file** beside `manifest.json` carrying the optional readiness
declaration and an optional local run command; `manifest.json` untouched. A **shell-owned demo
catalogue**, generated from the same repository scan and consumed in **both** sources, associating a
demo with a plugin by a **stable demo identifier**. An explicit **demo-specific proxy route,
distinct from the asset prefix**, implemented as a Vite dev-server proxy entry in development and a
reverse-proxy rule in hosted deployments, available in **both** deployments. A pre-mount check; a
shell-rendered unavailable panel with retry; a menu-card refresh policy separate from the pre-mount
check; an **explicitly declared** deployment audience defaulting to visitor; one shared presentation
component used by both the shell's pre-mount panel and the plugin's running-state errors.

Acceptance: readiness is asserted, not reachability; a timeout reads as "cannot reach demo services"
and never as "the demo is stopped"; the menu card populates without anyone opening the demo; the
plugin still handles connection loss after a successful check; the readiness request is same-origin
in both environments, with **no CORS added to any demo backend**; a deployment that declares no
audience shows **no** run instructions, and dev/preview/build state changes nothing about the
wording.

**Acceptance — the mode-switch check (R-1), and it is an exit condition.** Run the **same demo**
under `plugin-source: build` and under `plugin-source: registry` and prove:
- the readiness panel, its three states, its retry and its menu-card status are **identical** in
  both, with no change to the demo's files between the two runs;
- `registry` mode's **protocol and lifecycle behaviour are unchanged** — no new subject, no new
  grant, no change to announce, drift, withdrawal, health or revision handling;
- readiness metadata **admits nothing**: a demo whose metadata names a plugin the registry has not
  admitted shows no plugin, no route and no navigation entry, and a readiness file **cannot** supply
  or override a `remote.url`;
- a registry plugin with **no** associated lab demo mounts normally and reports no fault.

Regression: `manifest.json` is byte-unchanged, so `registry` mode's drift check is unaffected —
**prove it, do not assume it** (F-1); the registry contract is not extended.

**Verified 2026-09-24.** The readiness declaration is `demos/04-jetstream-cqrs/frontend/public/demo.json`,
a sibling of `manifest.json` — `manifest.json` itself is **byte-unchanged since 16d** (`git diff f224965`
is empty; `sha256 e2be1e07…`), so `registry` mode's drift check is untouched and the registry contract is
not extended. F-1 is proven, not assumed.

One scan feeds everything (BR-AS81): `tools/buildCatalogue/scanDemos.js` now reads the sibling metadata as
well as the manifest, and three consumers derive from it — the shell-owned demo catalogue
(`/demo-catalogue.json`), the plugin asset prefix `/plugins/<id>/…`, and the readiness route
`/demo-readiness/<demo>`. The last two are R-2's two mappings, deliberately separate: one is files, one is
a call. In development the readiness route is a Vite proxy entry; in a hosted deployment it is a generated
nginx snippet, `dist/deploy/demo-readiness.conf`, which the Dockerfile moves out of the served tree. No
`CORS` was added to any demo backend — `cqrs/names.go` still grants only `http://localhost:20401`, and the
new `/readyz` handler deliberately never calls `setCORS` (F-3).

The shell pieces: `src/shell/demos/` (the store, the probe, the pre-mount gate), `src/shell/ui/DemoCards.vue`
and the one shared presentation component `shared/ui-shell/DemoStatePanel.vue`, used by both the shell's
pre-mount panel and the plugin's running-state errors. The store is created in `main.js` **outside** any
source branch, which is how R-1 is held structurally rather than by matching two code paths.

**The mode-switch check (R-1), run live, same files, same session, nothing edited between the two runs:**

| Check | `plugin-source: build` | `plugin-source: registry` |
|---|---|---|
| plugin mounts at `/demo-04` | yes | yes |
| menu card, with nobody opening the demo | Running | Running |
| NATS down, `/readyz` 503 | "This demo is not ready." + `stream ODOMETER`, `kv odometer-write`, `kv odometer-read` | identical |
| menu card in that state | Not ready | Not ready |
| command API down, proxy 5xx | "Cannot reach demo services. / The check could not be delivered. The demo may be running, or it may be stopped — we could not tell." | identical |
| menu card in that state | Unknown | Unknown |
| "Check again" while down, then services back | mounts in place | mounts in place |
| plugins · revision | 1 · `313c695e88df9f95` | 2 · rev `2` |

A timeout or an unreachable proxy never reads as "the demo is stopped"; the word appears only inside the
one sentence that says we could not tell. The menu card populates from the store's own refresh, separate
from the pre-mount check — no card in the table above was produced by opening a demo.

`registry` mode's protocol and lifecycle are unchanged: no new subject, no new grant, no change to announce,
drift, withdrawal, health or revision handling. Demo 04 was curated into `demos/01-dictionary/registry.json`
by copying its manifest **byte for byte** and adding only `enabled`, which a plugin may never state about
itself (decision 79, BR-AS43). Because `Allowlist.Permits` admits a same-origin path (BR-AS72), the curated
row keeps the manifest's relative `remote.url`, so **no** `REGISTRY_ALLOWED_ORIGINS` entry and **no** second
build were needed — the strongest available form of BR-AS78. The service logged `seeded=2 skipped=0
withheld=0` with its allowlist unchanged.

**Readiness metadata admits nothing.** With `poc-mfe-registry-service-1` stopped, the shell reported
`plugins 0`, an empty FEATURES nav, and `/demo-04` answered "Nothing here — no plugin claims /demo-04". The
demo card was still drawn and still read "Running", but as a `generic` element with **no link**. The served
`/demo-catalogue.json` carries no `remote` and no contributions at all, so a readiness file cannot supply or
override a `remote.url`.

**A registry plugin with no lab demo.** `demo-catalog` on 7112 mounted normally at `/demos` and rendered its
own content. The Plugins screen listed it `available`, `0 rejected`, with no readiness panel and no fault —
the gate is not applied at all when a plugin has no catalogued demo (`demoGate.spec.js`).

**Both deployments, proven on the image rather than argued.** `lab-shell/Dockerfile` could not build at all
since 16b — `vite.config.js` has imported `lab-shell/tools` since then and the Dockerfile never copied it.
Fixed here, together with two consequences that only the image could reveal:

- The build context now also carries the demos' **metadata and nothing else**
  (`COPY --parents demos/*/frontend/public/{manifest,demo}.json`), derived by wildcard rather than a
  hand-written list. No demo source and no demo `dist/` enters the context, so the Dockerfile's own claim —
  the host never compiles a plugin — still holds.
- `pluginAssets`'s "a catalogued demo with no built output fails the build" now applies only to a
  `plugin-source: build` build. A registry-source shell resolves every remote from the registry and serves
  no plugin assets of its own, so demanding a demo's `dist/` failed a build that had no use for it. The
  readiness scan is **not** gated this way — that one is required in both sources.
- The generated nginx snippet passes each upstream through a **variable with a resolver**. nginx resolves a
  literal `proxy_pass` host once, at startup, and refuses to start when it cannot: `nginx -t` failed with
  `host not found in upstream "demo04-cqrs"` simply because the demo was not running, which would have taken
  the whole shell down to report one demo. Deferred to the request, an absent demo is a 502 the shell reads
  as "cannot reach demo services".

Measured on the built image on the lab network with demo 04 absent: `nginx -t` passes,
`/demo-readiness/04-jetstream-cqrs` answers **502**, `/demo-readiness/99-nope` answers **404** (never the
SPA), and `/demo-catalogue.json` is served with revision `ec8d38f85b1a6185` — the same revision the dev
server produced, from the same scan.

Suites: `lab-shell` 754 → 758 specs, demo 04's frontend 502 → 508, demo 04's Go package ok, eslint 0 errors,
`frameOwnership` clean. `hostBundleFingerprint.mjs` was re-recorded — 17 host assets, digest
`806a959514f744fc7b88e0f4a5762c7ffbb4c26edb499fe596f4719cdfa3746a` — because 16e changes the shell itself,
which is the case BR-AS03 does not cover; the asset count is unchanged and `--verify` is stable.

One thing left where it was: `nginx.conf` still names `accounts-service` in a literal `proxy_pass`, so the
shell image still will not start without that service. That is a lab-service dependency, not a demo one, and
it predates this task.

**16f — Health presentation without a health plane.** *Decision 8. Rule BR-AS80. Resolves F-2.*
New: in `build` mode the health plane is not constructed; the injected health object carries an
explicit "monitoring is not configured" flag beside its empty signals; the Plugins screen reads that
flag and renders `not configured`, reusing the existing `HEALTH_STATE.NOT_CONFIGURED` constant.
Touched files are composition and render site only — `main.js` and the Plugins screen.
Acceptance: no synthetic `healthy`; **no `HealthSignal` is synthesised at all**; `not configured`
never ages; no plugin rests at `unknown` in `build` mode; the nav is quiet without new code, because
`healthAttention` already marks only `unavailable`. Regression: `healthPlane.js` and `healthText.js`
are unchanged files; no new state, no new timer, no new freshness rule, and **no change to how
health is calculated** — if any is needed, decision 9's display carve-out has been exceeded and a
new decision is required. `HealthNotConfigured` stays backend-only on the registry service.

**16g — Legibility.** *Decision 11, closing questions. Rule BR-AS81.*
New: a generated `demos/README.md` table written by the same scan as the catalogue; a page-level
"Catalogue source: Build" / "Catalogue source: Registry" statement on the Plugins screen; demos 02
and 03 listed on the menu as shell-owned intro pages with run instructions and links to findings.
Acceptance: the source statement appears once, not per row, and not on any demo card; it is worded
as a property of this shell; demos 02 and 03 carry **no health indicator and no readiness check**;
the `announced` / `preload` labels are untouched and not displayed alongside `plugin-source`. The
demo menu and the Plugins screen are allowed to differ.

**16i — Publish the rules and their coverage rows.** *All eleven decisions. Rules BR-AS75 to BR-AS81.*
No behaviour. The seven rules are **written in full in this phase, above**, and are the wording of
record until they are published. Deliverable: add BR-AS75 to BR-AS81 to
`demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md` under a Phase 16 section, **verbatim** from this
file, each with its coverage row naming the spec or gate that proves it. Deliberately last: a rule
published before its task is built has no coverage row to carry, and a row written against work that
has not landed is a claim rather than a proof.
Acceptance: all seven appear, byte-identical to the wording above; every one carries a coverage row
pointing at a spec or gate that actually runs; no existing BR-AS rule is amended in the process — if
one has to be, the split was cut in the wrong place (decision 9). Until this task is done, the rules
live only here, and that is deliberate, not an omission.

**16h — Registry regression gate.** *Decision 9.*
No new behaviour. The Phase 15 acceptance gate runs unchanged. Add focused coverage for the display
distinctions only, and update wording assertions **without weakening any existing behavioural
check**. Exit condition: no BR-AS rule owned by `registry` mode is amended, and no existing spec
changes meaning. If either moves, the split was cut in the wrong place.

**Phase exit conditions.** 16c verified on demo 04 — the whole-plugin prefix and HMR, not the entry
alone. F-1 to F-5 are resolved (2026-09-23) and no task is blocked. BR-AS75 to BR-AS81 approved and
added to `BUSINESS_RULES-APP-SHELL.md` with their coverage rows — **task 16i**, which is what
tracks it; the rules stay in this file, in full, until 16i runs. Phase 15's gate green and
unmodified. `manifest.json` byte-unchanged across the phase, proven against `registry` mode's drift
check. 16e's mode-switch check green: the same demo keeps its readiness panel under both sources
with no file change and no registry protocol or lifecycle change.

#### Not in scope

- No change to `registry` mode's **protocol or lifecycle**, to `mfe-registry-service`, to the
  trust chain, or to any BR-AS rule it owns. Amended 2026-09-23 (R-1): `registry` mode does gain
  the **readiness panel**, because readiness is independent of `plugin-source`. That addition is
  additive and authority-free — it admits no plugin and overrides no remote URL — so the
  decision 9 claim is unaffected; the earlier blanket "no change" wording would have
  contradicted it.
- No migration of demo 01's three apps — that is Phases 10, 11 and 12, and they stay ahead of this
  phase in the queue for `registry` mode.
- No signing, no health **plane**, no probing and no live change in `build` mode. Amended
  2026-09-23: health *presentation* is in scope — decision 8 and BR-AS80 require `build` mode
  to report `not configured` rather than nothing, and the earlier blanket "no health" wording
  contradicted that.
- No move of any demo folder.

#### Alternatives rejected 2026-09-23

- **A separate `demo-shell` application under `demos/`, reading the plugins folder directly.** The
  idea that produced this phase, and the source of decisions 1, 5 and 6. Rejected as a *second
  application* because it forks the shell kernel and gives the lab two menus; adopted entirely as a
  *mode*. Also noted at the time: a browser cannot read a folder, so discovery needs a generator
  either way.

- **Mode as a subfolder — `demos/<source>/…`.** Raised 2026-09-23 for a real
  need: the tree gives no way to tell which demos the shell loads in which mode. Rejected on three
  counts. Mode belongs to the shell's boot, not to a demo, so encoding it in a path contradicts
  decision 1 and turns a future deployment change into a repository-wide rename. The split is also
  one against three — only demo 01 is `registry` — and two of the remaining three
  (`02-multi-region`, `03-multi-cluster-and-accounts`) have no frontend at all, so a mode label
  would be wrong for them. The underlying need is legibility, and decision 11 answers it directly.

- **Moving `demos/01-dictionary/` out of `demos/`.** Raised because demo 01 no longer feels like a
  demo — it is the platform, and `lab-shell` depends on it. Rejected: the dependency is on demo 01's
  *services*, not on its path, so the move changes the feeling and not the coupling, at a cost of
  390 files including generated PDFs, obsidian notes and ADR front matter. `build` mode removes the
  dependency instead of relocating it. Recorded here so it is not re-asked; if it is ever revisited
  it is an ADR and a phase of its own, not a step inside this one.

#### Closing questions — both settled 2026-09-23

- **`build` mode keeps the Plugins screen at `/plugins`.** It carries the page-level
  "Catalogue source: Build" statement (decision 11) and the unmonitored health presentation agreed
  in decision 8. **Use decision 8's final state name, not the gate's shorthand:** the existing
  vocabulary's word is `not configured`; "not tracked" was discussion wording and must not be
  reintroduced as a label.
- **Demos 02 and 03 stay on the menu as shell-owned intro pages**, carrying run instructions and
  links to their findings. They need no frontend plugin to be listed, and they receive **no plugin
  health indicator and no mount-readiness check** — neither signal has anything to measure on them,
  and decision 8 forbids drawing a mark nobody took.

**The closing rule of the phase:** the **demo menu** lists the lab's demos; the **Plugins screen**
lists the active catalogue's plugins. Those two inventories need not be identical, and nothing in
this phase may quietly make one derive from the other.

---

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
| 4 | Demo catalog stays as a built-in plugin at `/demos` | **Approved**, and promoted to Phase 1a's primary test fixture — it proves the contract before any remote exists. No privileged path: it uses the public contribution API. **Revisited by Phase 8 decision 83:** having served as the fixture, it becomes a federated plugin preloaded like any other, and the `builtin` kind is retired with it (decision 84, RETIRED 2026-08-31) — the shell's fallback becomes its native frame, not a bundled plugin. |
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
