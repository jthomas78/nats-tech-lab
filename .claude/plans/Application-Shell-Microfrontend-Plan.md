# Extensible Application Shell + Micro-Frontend Plugins — Plan

> **Status: Phases 1–5, 7, 8, 13, 14 COMPLETE and archived. Phase 15's design gate PASSED
> (2026-09-02); its task checklist is derived and specs are next.**
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
- [ ] **15e — convergence.** A content-equality no-op that writes no revision and no audit row but
      advances `Accepted` (decision 10, amended). Kept distinct from today's `Admission.NoOp`, which
      means a literal replay at an equal release; a spec should pin that the two are different.
      **From the review:** this is a write to the entry row that does not bump the revision, and it
      has not been checked against that row's concurrency control. Read the update path here and pin
      with a spec that a watermark-only write cannot lose a concurrent real announce.
- [ ] **15f — silence is inert.** Specs proving a plugin that never answers a health ask is unhealthy
      but still registered, and a plugin that ignores a reset notice is simply not re-announced
      (decision 9). Neither path may reach unregister. BR-AS54 unchanged.
- [ ] **15g — docs.** `ARCHITECTURE-APP-SHELL.md` gains the as-built section and loses the claims
      this phase invalidates; `ARCHITECTURE-COMMUNICATIONS.md` gains the two new subjects. The Phase
      14 topology drawing's "after" panel now shows a plugin on two Docker networks and must be
      re-drawn to one, or explicitly dated as Phase 14's state.
      **Drawn ahead of the code (2026-09-02):** `diagrams/mfe-health-over-nats.html` is a separate
      before/after drawing for this phase, embedded in `ARCHITECTURE-APP-SHELL.md` and stamped
      "proposed". 15g brings it to as-built the way Phase 14's was — re-stamp the eyebrow, and fix
      any label the implementation moved. The Phase 14 drawing is left alone; it is dated as Phase
      14's state and the new one carries the change.
- [ ] **15h — the gate.** `cmd/registry-acceptance` green against the running lab. It asserts health
      today, so unlike Phase 14 this phase should expect to touch it — and any edit is recorded in
      this entry the way Phase 14's three were.
      **From the review:** step 9's control group changes — `example-plugin-unreachable` now answers
      and honestly reports `unhealthy` (it self-GETs against a listener that does not exist), where
      today it reports "not configured". That leaves the new `absent` cause with no fixture, so
      add a step that stops a running plugin's process and asserts the registry reports
      `absent` — which exercises BR-AS54 (silence never withdraws) on the same step.

---

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
