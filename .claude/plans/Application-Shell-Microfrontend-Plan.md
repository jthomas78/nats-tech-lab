# Extensible Application Shell + Micro-Frontend Plugins — Plan

> **Status: Phases 1–5, 7, 8, 13, 14 COMPLETE and archived. Phase 15's design gate PASSED
> (2026-09-02); its task checklist is derived and specs are next. Phase 16
> (`plugin-source: build`) is APPROVED and CLOSED 2026-09-24 — 16a to 16l done, no open item.
> Phase 17 (one navigation tree) is APPROVED 2026-09-24 and OPEN; 17a to 17f are done — both
> contracts carry the new forms, the grouped tree exists as data, its clashes are reported, the
> shared rail renders links and markers, and the shell now draws ONE rail from that tree — and 17g
> (the generic default-route redirect) is next.**
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
re-opened. Done: 16a to 16i — the phase was CLOSED 2026-09-24, then re-opened the same day for
16j, which closes a gap 16e named and deliberately left. Re-opened again the same day for 16k,
an independent review of 16a-16i. Re-opened a third time the same day for 16l, which closes the
one finding 16k carried out as a named open item. Done: 16a to 16l — the phase is CLOSED
2026-09-24 with NO open item.**

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

- **BR-AS82 — A demo's own backend calls are served from the shell's origin, and only the routes
  the demo named.** Where an embedded plugin calls its demo's backend, the shell **must** serve
  those calls from its own origin under a route prefix distinct from the plugin asset prefix of
  BR-AS77 and from the readiness route of BR-AS79. No demo backend **may** be given a CORS grant,
  widened or otherwise, for this purpose. The forwarded surface **must** be enumerated by the demo
  itself, route by route, in the same demo-owned sibling file BR-AS78 requires and derived by the
  same scan BR-AS81 requires; a declaration naming an upstream and no routes **must** read as no
  declaration at all, so that no arrangement here can become a general tunnel to a demo's backend.
  A path under the prefix that no demo named **must** be refused, and **must not** fall through to
  the shell's own application. The readiness declaration of BR-AS79 and the API declaration of this
  rule **must** stay separate: neither extends the other, and a readiness route **must not** be
  reachable through this prefix. A plugin **must** be byte-identical in both catalogue sources
  under BR-AS78, so the choice of base **must** be made when the shell activates the plugin, never
  compiled into it.

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

**16f — Health presentation without a health plane. DONE 2026-09-24.** *Decision 8. Rule BR-AS80. Resolves F-2.*
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

**Verified 2026-09-24.** The substitution is one flag and one mapping, and nothing else moved.

`main.js` resolves `monitored = source === PLUGIN_SOURCE_REGISTRY` once, puts it on the injected
health object beside its empty `signals`, and gates FOUR things on it: `createHealthPlane` itself,
the connection-epoch watch, the five-second ageing timer and the slow reconcile timer. In `build`
mode none of them exist — so "`not configured` must not age" is true because there is no clock to
age against, not merely because a label says so. `PluginsView.vue` reads the flag once for the page
and maps it to the existing `HEALTH_STATE.NOT_CONFIGURED`, with the existing `healthTone`; the cell
title says `no monitoring is configured for this deployment` rather than a time, because nothing was
checked. No `HealthSignal` is constructed anywhere.

Regression proven, not asserted: `git diff` is empty for `healthPlane.js` and `healthText.js`.
`healthSourceIndependence.spec.js` holds that line from now on — neither measurement file may name
`pluginSource`, `PLUGIN_SOURCE`, `VITE_PLUGIN_SOURCE` or `monitored`, and neither composition nor
render site may write a `state:` or a `lastCheckAt`. `PluginsView.spec.js` is new and carries the
acceptance words: in `build` mode both cells read `not configured` for every plugin, the word
`unknown` is absent from the screen, `healthy`/`unavailable`/`stale` are absent, the tone is the
shared quiet `off`, and `healthAttention(undefined)`/`healthAttention({})` both return null — which
is why the nav needs no new code. Four more specs hold `registry` mode still: measured states are
still said, a reading still names when it was taken, an unmeasured plugin still rests at `unknown`
and not at `not configured`, and a health object with no flag at all is treated as monitored.

Live in both modes on 7110, 1920x1080. `build`: 1 entry, `demo-04`, frontend `not configured`,
backend `not configured`, and a DOM query found zero attention marks in the sidebar. `registry`
(with `poc-postgres-1`, `poc-nats-1`, `poc-accounts-service-1`, `poc-mfe-registry-service-1` up):
rev `2`, 2 entries, `demo-04` frontend `unknown` / backend `not applicable`, `demo-catalog`
frontend `unknown` / backend `unavailable (no-responders)` — every one of them a measurement, and
the words `not configured` nowhere on the page. Registry-mode health is visibly what it was.

774 specs pass (66 files, up from 758). Lint 0 errors; `shell frame clean`. The host bundle moved
because `main.js` did, and was re-recorded in the default (registry) mode: 17 assets, digest
`aa366e8cfd9724359404c4ee05120102a1f7b8a75907641a739545b0ce881cf3`; `--verify` is stable after it.

**16g — Legibility. DONE 2026-09-24.** *Decision 11, closing questions. Rule BR-AS81.*
New: a generated `demos/README.md` table written by the same scan as the catalogue; a page-level
"Catalogue source: Build" / "Catalogue source: Registry" statement on the Plugins screen; demos 02
and 03 listed on the menu as shell-owned intro pages with run instructions and links to findings.
Acceptance: the source statement appears once, not per row, and not on any demo card; it is worded
as a property of this shell; demos 02 and 03 carry **no health indicator and no readiness check**;
the `announced` / `preload` labels are untouched and not displayed alongside `plugin-source`. The
demo menu and the Plugins screen are allowed to differ.

**Verified 2026-09-24.** Three pieces, each answering one half of BR-AS81.

The **page-level statement** is on the Plugins screen and nowhere else:
`Catalogue source: Build` under `build`, `Catalogue source: Registry` under
`registry`, one node per page (`.source`, inside `.page-head`), absent from
`tbody`, absent from the demo cards, and worded as a property of this shell —
its title reads `this shell is configured to read its catalogue from …`. The
registry's `announced` / `preload` labels are untouched and appear nowhere
beside it. Five specs in `PluginsView.spec.js` drive both values through
`resetPluginSourceForTests()` and read the rendered page rather than the
module.

The **generated `demos/README.md`** is written by a scan of the demo folders,
not by hand: a `frontend/public/manifest.json` makes a frontend build-sourced,
and a `dockerfile: demos/<demo>/frontend/<app>/Dockerfile` line in a demo's
compose band makes it registry-sourced, with its host port read from the same
service block. Six rows today. It is a standalone command
(`npm --prefix lab-shell run demos:readme`, `--check` to test), deliberately
not a Vite hook — both catalogue plugins already guard `closeBundle` against
repo writes, and a file committed to the repository must be written by a
command somebody ran on purpose. `demosReadme.spec.js` regenerates from the
working tree and fails when the committed page is stale.

**Demos 02 and 03 are on the menu** as shell-owned intro pages at
`/lab-demos/:demo` — not `/demos`, which the demo-catalog plugin already
contributes. Each page carries the demo's question, its exact run commands and
the paths to its findings; every one of those nine paths is asserted to exist
on disk. They carry no health indicator, no readiness check and no
`plugin-source`, and they are drawn as a separate group so a reader can see
which half of the menu a status applies to. Verified live at 1920x1080 in both
modes: the probed demo keeps its status dot, the two lab demos have none.

809 specs pass, lint reports 0 errors, `shell frame clean`. Host bundle
re-recorded in the default mode after the `main.js` and `PluginsView.vue`
changes — 17 assets, digest `57ce00bdf7d8c28805d1ad398bcb521f05410b5877eb08bcbd560986fbbe5fea`
— and `--verify` is stable against it.

**16i — Publish the rules and their coverage rows. DONE 2026-09-24.** *All eleven decisions. Rules BR-AS75 to BR-AS81.*
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

**Verified 2026-09-24.** All seven rules are in
`demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md` under a new Phase 16 section,
copied **verbatim** from the wording above — asserted by extracting the block
from this file and finding it as an exact substring of the published one, not
by reading them side by side. The diff is 151 insertions and 0 deletions, so
no existing BR-AS rule was amended and decision 9's split held.

Each rule carries a coverage row naming checks that run today: BR-AS75 →
`pluginSource.spec.js`; BR-AS76 → `buildCatalogueScan.spec.js`,
`buildCatalogueClient.spec.js` and the `hostBundleFingerprint.mjs`
record/verify gate; BR-AS77 → `pluginAssets.spec.js`; BR-AS78 →
`buildCatalogueScan.spec.js`, `demoReadinessGeneration.spec.js` and demo 04's
own `plugin.spec.js`; BR-AS79 → `demoGate.spec.js`, `demoReadiness.spec.js`,
`demoReadinessGeneration.spec.js`, `frameOwnership.spec.js` and Go's
`ready_api_test.go`; BR-AS80 → `PluginsView.spec.js` and
`healthSourceIndependence.spec.js`; BR-AS81 → `PluginsView.spec.js`,
`labDemos.spec.js` and `demosReadme.spec.js`.

**One gap is recorded rather than claimed.** No spec diffs demo 04's
`manifest.json` bytes across a `build` run and a `registry` run. BR-AS78's
guarantee is asserted structurally — the scan re-serialises to identical bytes,
the dev port never leaks in, and `registry` mode's existing drift check hashes
the same file — but the cross-source file comparison itself does not exist, and
the coverage row says so.

The `BUSINESS_RULES.md` index was corrected in the same commit: it still read
`BR-AS01–BR-AS73`, which was already stale by one rule before this phase.

**16k — Independent review of 16a-16i, and its repairs. DONE 2026-09-24.** *Rules BR-AS75,
BR-AS77, BR-AS79. No new rule.*

Codex reviewed 16a to 16i against the approved decisions and raised five findings. Each was checked
against the source before anything was edited. Four are confirmed and fixed; one is confirmed and
NOT fixed, and is carried out of the phase deliberately.

**Finding 1 — build mode did not enforce same-origin plugin URLs. CONFIRMED, FIXED.** P1. Decision
2's trust story is "the shell's own build produced this document". That is an argument about how
the file is usually MADE, not a property of the file the browser fetched. `scanDemos.js` says so
outright in its own header — it copies a demo's `manifest.json` through untouched and stamps no
origin — so a manifest naming `https://outside.example/remoteEntry.js` travelled into the
catalogue and was registered by the federation adapter. `RemoteAllowlist` cannot help: it is built
FROM the admitted manifests, so it allows whatever it was given.

The contract is now a question, asked once. `pluginAssetPath.js` grows `isPluginAssetUrl(id, url)`
beside the three builders that already write the path; `buildCatalogueClient.js` asks it before
admission. Four shapes are refused — a scheme, a protocol-relative authority, anything not rooted
at `/`, and a path that climbs out of the plugin's own directory. The climb is answered by
resolving against a base that cannot be the real origin and reading the result back, twice: once
as written and once percent-decoded, because a server decodes a path before it routes it and
`..%2f..%2fevil.js` is the same climb in another spelling. That second pass was added after the
first version admitted it.

Three properties hold the fix in place. It is **per entry, never per document** — BR-AS13's
tolerance table says an invalid required field costs that plugin and nothing else, and a whole
catalogue emptied by one bad manifest would take the lab down for a typo. It leaves a **malformed**
remote alone, because `validateManifest` already refuses that at admission with its own cause code
and a row in the inventory; a gate that swallowed it first would hide the reason. And it lives in
the **build-mode client**, not in the shared validator, so registry-mode semantics are preserved by
construction: a curated registry's remotes may legitimately name another origin and are gated by
`REGISTRY_ALLOWED_ORIGINS` instead. Refusals are reported through an injected `onRefusal` —
`console.error` by default — so an entry never vanishes from the menu in silence.

**Finding 2 — the packaged registry shell cannot serve demo 04. CONFIRMED, NOT FIXED.** P2. Carried
out of the phase as an open item. The evidence is complete and is recorded here so the next person
does not have to find it again:

- `demos/01-dictionary/registry.json` enables demo 04 with `"url": "/plugins/demo-04/remoteEntry.js"`
  — a path on the shell's own origin.
- `pluginAssets.js` gates its build-time copy on `plugin-source: build`
  (`building = config.command === 'build' && readPluginSource(...) === PLUGIN_SOURCE_BUILD`), so a
  registry-source build copies nothing. Two specs in `pluginAssets.spec.js` already assert exactly
  that, deliberately.
- `lab-shell/Dockerfile` copies only `demos/*/frontend/public/{manifest,demo}.json` and says why:
  "This image is a `plugin-source: registry` shell … and serves no plugin assets of its own."
- `lab-shell/nginx.conf`'s `location /plugins/` is `try_files $uri =404` against the image's own
  disk. Nothing populates it.
- There is no generated asset proxy. `demo-readiness.conf` and `demo-api.conf` are the only two
  generated confs, and both are ungated on plugin source — which is the precedent a fix would
  follow.

So the packaged shell returns 404 for a plugin its own registry advertises, while development works
through the dev proxy. The reason this was not repaired inside 16k is that both available repairs
cross a boundary this phase documented, and neither is a small edit:

- **Option A — ungate the copy.** Drop the `plugin-source` half of `building` and teach
  `lab-shell/Dockerfile` to copy `demos/*/frontend/dist`. This preserves "plugin code is not
  compiled into the shell bundle" exactly, because a `dist/` is copied as static files and never
  enters the bundle — build mode already does this. The cost is the image's build contract:
  `demos/*/frontend/dist` is gitignored, so the demo must be built BEFORE `docker compose build`,
  and a `COPY` that matches nothing fails the image build outright.
- **Option B — generate an asset proxy.** A `demo-assets.conf` produced from the same scan, exactly
  mirroring `demo-api.conf`, driven by a new `assets.hostedUpstream` in `demo.json`. This is what
  `registry.json`'s own comment already claims happens ("in a hosted deployment the reverse proxy
  does") and preserves every rule. The cost is that demo 04 has no frontend container to point at —
  `deploy/compose.yaml` holds only `nats`, and there is no `demos/04-jetstream-cqrs/frontend/Dockerfile`
  — so Option B is new deployment work inside a sealed demo folder.

Recommendation on the record: **Option B**, because it is the arrangement the registry comment
already describes, it keeps the shell's image free of demo output, and it puts demo 04's deployment
where demo 04's own `CLAUDE.md` says it belongs.

**Option B was chosen by the user on 2026-09-24 and built as task 16l. This finding is CLOSED.**

**Finding 3 — the readiness timeout ended before the body was read. CONFIRMED, FIXED.** P2.
`fetch` resolves on the response HEADERS. `probeDemo` cleared its timer there, so a demo that sent
`200` with a JSON content type and then stalled mid-body left `response.json()` awaiting forever
with no timer left to abort it. The panel stayed blank past `timeoutMs`, and because the panel
waits on the promise, every later check queued behind the stuck one. A half-sent body is what a
container killed mid-answer produces, so it is not a theoretical shape. `clearTimeout` moved into a
`finally` that spans both the fetch and the body read; the timeout is reported as
`unknown` / `timeout`, and because the timer is always cleared, the very next check gets a clock of
its own and recovers.

**Finding 4 — the operator recovery command did not start the demo. CONFIRMED, FIXED, with one
correction to the finding.** P2. The old `runCommand` was
`docker compose -f demos/04-jetstream-cqrs/deploy/compose.yaml up -d`, which starts the NATS
container and nothing else. A reader who followed it was left with the same `unknown` the command
was printed to fix: `cqrs serve` was not listening on `20402`, and the snapshotter and projector
were not filling the two KV buckets.

The finding's second claim — that the command "does not initialize the required stream and
buckets" — is **not correct**, and the fix does not act on it. `cqrs/main.go` runs `ensureStream`
and both `ensureKV` calls before it dispatches any subcommand, so any `cqrs` process creates them.
The missing pieces were processes, not initialisation.

New `demos/04-jetstream-cqrs/deploy/start.sh` brings up the container, waits for its health check,
builds the CLI, starts `serve`, `snapshotter` and `projector` under PID files, then waits on
`/readyz` and prints both URLs. It converges — running it on a demo that is already up reports each
piece as already running and exits 0. `deploy/stop.sh` reverses it by PID file, so a `cqrs` the
reader started by hand is left alone. The operator/visitor split is untouched: the command is still
declared in `demo.json`, still shown only to an operator, and the shell still never runs it.

**Finding 5 — manifest edits did not refresh the development catalogue. CONFIRMED, FIXED.** P2.
`server.watcher.add(file)` tells chokidar to REPORT a file. It does not reload the page: Vite turns
a change into an HMR message by looking the file up in its module graph, and both discovery inputs
are read with `fs.readFileSync` from outside the Vite root — they are in no module's import chain,
so the lookup finds nothing and Vite correctly does nothing. The catalogue was re-scanned on every
REQUEST, so the new document was always one manual refresh away; the menu simply never asked.

The reload is now sent explicitly, on `add`, `change` and `unlink`, for any file that was read by
the last scan OR whose tail is one of the two discovery inputs — the second half so that a demo
ADDED while the server runs reloads too, since its manifest was never read and cannot be in the
watched set. A full reload rather than an HMR update, because catalogue membership decides the nav
tree and the route table and both are built once at boot. `server.hot ?? server.ws` keeps the
plugin off a pinned Vite minor.

*Coverage added.* 24 new specs, and each was proved to FAIL against the unfixed code before being
kept — the fix was temporarily reverted, the spec run, and the revert undone:

- `buildCatalogueClient.spec.js` — `the same-origin gate on a build catalogue`: the reported case,
  eleven escape spellings, per-entry refusal, the malformed-remote hand-off, and the refusal
  report. 4 of 19 fail without the gate. The file's own `PLUGIN` fixture moved from `/remoteEntry.js`
  to `/plugins/jetstream-cqrs/remoteEntry.js`, because the old one was itself off the BR-AS77
  contract.
- `demoReadiness.spec.js` — `a body that never finishes arriving`: the stall times out and calls it
  a timeout, and the very next check recovers. Both hang without the fix.
- `buildCatalogueReload.spec.js`, new — the reload wiring, including the never-read manifest, the
  unlink, the quiet case for an unrelated file, and the `server.ws` fallback. 6 of 7 fail without
  the fix.
- `demoRunCommand.spec.js`, new — a `runCommand` that names a path in this repo must exist and be
  executable, plus demo 04's four pieces by name. This is the narrow rule: the shell never runs the
  command, so there is nothing to execute here, but a command pointing at nothing is worse than one
  that does too little.

*Checks run.* `npm --prefix lab-shell test` — 71 files, 866 specs, green (was 842). `npm --prefix
lab-shell run lint` — 0 errors, 30 warnings, the existing baseline, and the repo's own "shell frame
clean" guard passed. `npx vitest run` from `demos/04-jetstream-cqrs/frontend` — 35 files, 513
specs, green. `go test ./...` from `demos/04-jetstream-cqrs/cqrs` — ok. `bash -n` on both new
scripts. `deploy/start.sh` run for real against a live Docker: `/readyz` answered
`{"ready":true}` with all three checks ok, and a second run converged and exited 0.

*The host bundle baseline moved, and legitimately.* This task edits host SOURCE —
`buildCatalogueClient.js`, `pluginAssetPath.js`, `readinessProbe.js` — which is exactly what
BR-AS03's digest is supposed to notice. Re-recorded in `build` mode: 17 host assets, digest
`7e6a007a2e92e58d321a9f6b59b4ba8190dc937083e5d424e33e6777c13c3bf0`. The clause the tool also checks
— that the host bundle names no plugin container, remote URL or module path — passed before and
after.

*Limitations, stated plainly.* Finding 2 is not fixed, so the packaged registry shell still 404s on
`/plugins/demo-04/…`; that was verified against source and configuration, not against a running
container, and no registry-acceptance run was made for this task. The same-origin gate is a
build-mode gate only, by design — a curated registry is still trusted to name its own origins.
`start.sh` was verified on macOS with Docker Desktop and has not been run on Linux.

**16j — The demo API proxy. DONE 2026-09-24.** *Decision 8 (same-origin, no CORS). Rule BR-AS82.*
New `build`-source behaviour. 16e proxied **one** readiness call and said so in its own notes: "it
proxies one readiness call, not a general tunnel to this service." That left demo 04's write side
dead inside the shell — the page is on the shell's origin, the command API on `20402` grants only
`http://localhost:20401`, and every button reported `Failed to fetch`. The forbidden repair was the
obvious one: widening `demos/04-jetstream-cqrs/cqrs/names.go`. F-3 and the demo's own `CLAUDE.md`
both rule it out, and the demo's `ready_api_test.go` holds the line from the other side.

The repair is the readiness route's **sibling**, not its extension. `demo.json` grows an `api`
block naming the upstream and every route, one by one; `scanDemos.js` normalises it in the same
pass that already reads `readiness`; `tools/buildCatalogue/demoApi.js` turns that into a Vite dev
proxy and a generated `demo-api.conf` for the container, exactly as `demoReadiness.js` does.
Acceptance: one proxy entry per **declared route**, never one per demo; a declaration with no
routes reads as no declaration; `/readyz` — declared under `readiness`, served by the same backend
— is **not** reachable through this prefix; a suffix does not widen an exact route; an unnamed path
404s rather than reaching the SPA; no CORS grant anywhere moved.

Demo-side: `config.js` exports `COMMAND_API` as a live binding plus `EMBEDDED_COMMAND_API`, and
`plugin.js`'s `activate()` moves the base. The standalone app is untouched, and the built bytes are
identical in both catalogue sources, which is BR-AS78.

**The host bundle baseline moved in this commit, and not because of this task.** The previous
commit (`21361e9`) added `@primeuix/styled` to demo 04's federation `shared` block, which changes
the shared-module set the host's federation runtime is built with; the baseline was not re-recorded
then, so `--verify` was already failing on a clean tree before 16j started. Re-recorded here in
`build` mode: 17 host assets, digest
`57ce00bdf7d8c28805d1ad398bcb521f05410b5877eb08bcbd560986fbbe5fea`. The BR-AS03 clause the tool
also checks — that the host bundle names no plugin container, remote URL or module path — passed
before and after.

**The second door, decided separately and opened in a second commit.** The proxy above did nothing
for the live view, because a WebSocket is not subject to CORS at all — it is gated by
`allowed_origins` in `demos/04-jetstream-cqrs/deploy/nats.conf`, which named only `20401`, so the
embedded page sat at **Not connected** with its controls hidden behind that message. On the user's
decision the list now names `http://localhost:7110` and `http://127.0.0.1:7110` as well, loopback
only, with the file recording why each entry is there.

This is deliberately **not** the same mechanism as BR-AS82. That rule exists so that an HTTP
surface can be reached without a CORS grant widening anywhere; here there is no proxy to build and
no CORS to widen, only a list that either names an origin or refuses it. Verified end to end at
that point: `Record trip` on `truck-7` returned `accepted — travel truck-7 appended to ODOMETER at
seq 84142`, and the KV panels moved with it.

**16h — Registry regression gate. DONE 2026-09-24.** *Decision 9.*
No new behaviour. The Phase 15 acceptance gate runs unchanged. Add focused coverage for the display
distinctions only, and update wording assertions **without weakening any existing behavioural
check**. Exit condition: no BR-AS rule owned by `registry` mode is amended, and no existing spec
changes meaning. If either moves, the split was cut in the wrong place.

**Verified 2026-09-24.**

*The gate, unmodified.* `go run ./backend/mfe-registry-service/cmd/registry-acceptance` from
`demos/01-dictionary/`, against the lab brought up from `deploy/cell/` with the dedicated band
included. All eleven steps green: `PASSED — the publisher lifecycle behaved as the rules say it
should.` No file under `cmd/registry-acceptance/` was touched in this phase, or in this task. Step
10 ("nothing else in the registry moved") now lists demo-04 among the entries it finds still
registered and untouched, which is the phase's own addition passing the phase's own regression
check.

*One thing the gate could not do, found here and repaired after.* `--reset` failed: it ran
`compose exec -T mfe-registry-postgres …`, and no such service exists any more — ADR-055
(2026-09-08) folded the registry database into the cell's shared `postgres` service, and the
harness's reset path was never followed across. It had been dead for sixteen days and nothing said
so, because the gate passes without the flag.

It was not repaired *inside* 16h: this task's first sentence is "the Phase 15 acceptance gate runs
unchanged", so 16h ran it without `--reset` and touched nothing. The repair is its own commit,
made straight afterwards on the user's instruction. It points the reset at `postgres` and lifts the
service, role and database into named constants beside the compose helper, so the next reader is
told where the real source is (`deploy/cell/compose.runtime.yaml`'s `DATABASE_URL`) instead of
finding three bare strings. No assertion the gate makes was touched. Proved by running
`--reset` twice back to back against one live lab: `PASSED` both times, which is the property the
flag exists to provide and the property that had been missing.

*Exit condition 1 — no `registry`-mode rule amended.*
`git diff --numstat f9db851..HEAD -- demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md` gives
`151  0`. One hundred and fifty-one lines added, none removed. Every pre-existing BR-AS rule is
byte-identical to where the phase started.

*Exit condition 2 — no existing spec changed meaning.*
`git diff --numstat f9db851..HEAD -- '*.spec.js' '*_test.go'`, filtered to files with a non-zero
deletion count, returns exactly one file: `lab-shell/src/shell/registry/preloadFixture.spec.js`
(+25 / -6). Every other spec file in the repo is either new or purely additive. That one file was
read line by line. Its change is an *exemption*: `originOf` now answers `null` for a path-form
`remote.url`, and the stray-origin filter skips nulls, because such a URL has no origin of its own
to compare against the allowlist. The service agrees — `Allowlist.Permits`
(`registry/internal/domain/registry.go:434`) grants `sameOriginPath` before it consults the
allowlist at all — so BR-AS20 and BR-AS72 were not weakened. The BR-AS66 assertion widened from
`['demo-catalog']` to `['demo-catalog', 'demo-04']`, which records the new curated row rather than
relaxing the check.

*The one place the split was thin, now closed.* The JS exemption was looser than the Go rule it
mirrors: it tested only `startsWith('/') && !startsWith('//')`, so `/../../x` would have passed
unexamined. 16h adds `16h — a path-form remote url is still checked, on its own terms` to that same
spec file — three specs, touching no existing assertion — asserting that such a URL matches
`^/plugins/<id>/…`, carries no traversal, and resolves onto whatever origin it is joined to. The
first of the three asserts the case is real (`['demo-04']`), so the other two cannot pass
vacuously.

*Focused display coverage, and nothing else.* Decision 9's carve-out is the `build`-mode frontend
mapping. Its `build` half was already asserted in `PluginsView.spec.js` at 16d. 16h adds the half
that could rot quietly — `16h — not configured from the plane, not from the mapping`: `registry`
mode can itself report `not configured` for a plugin with no backend mapped, and the substitution
must neither swallow it into `unknown` nor say it in different words from `build` mode. Three
specs, same file, additive. No health calculation, timer, expiry or poll was added; the carve-out
was not left.

*The rest, green.* 815 specs pass (809 before this task, +6 — the six added here). `npm --prefix
lab-shell run lint` gives 0 errors and `shell frame clean`. `hostBundleFingerprint.mjs --verify`
matches 16g's baseline byte for byte
(`57ce00bdf7d8c28805d1ad398bcb521f05410b5877eb08bcbd560986fbbe5fea`), because this task added spec
files only.

*Phase exit condition — `manifest.json` byte-unchanged.*
`git log --oneline f9db851..HEAD -- demos/04-jetstream-cqrs/frontend/public/manifest.json` returns
one commit, `f224965` (16d), which created it. It has not been amended since, and both sources read
those same bytes.

**16l — The hosted asset proxy, and demo 04's own image. DONE 2026-09-24.** *Closes 16k finding 2.
Rule BR-AS77. Option B, chosen by the user.*

*The gap.* A packaged `registry` shell advertised `/plugins/demo-04/remoteEntry.js` in its own
registry and answered `404`. BR-AS03 is why: the shell's image compiles no plugin and copies no
plugin, so `nginx.conf`'s `location /plugins/` had an empty directory behind it. Development hid
this, because the dev proxy forwards the prefix to demo 04's dev server on `20401`.

*The repair, in three parts.*

1. **Demo-owned metadata.** `demo.json` grows an `assets` block with one field,
   `hostedUpstream`. `scanDemos.js` normalises it in the same pass that already reads `readiness`
   and `api` (`assetsOf`), and refuses anything that is not a bare `scheme://host[:port]` origin —
   no path, no query, no trailing slash. An origin is all that is needed, because the demo's image
   is built with the same `base: '/plugins/demo-04/'` the shell serves it at, so nothing is
   rewritten on the way through and a compiler-written chunk URL cannot drift from the proxy.
2. **A generated conf, gated on plugin source.** `tools/buildCatalogue/demoAssets.js` writes
   `dist/deploy/demo-assets.conf`, one `location` per declaring demo, which the Dockerfile copies
   to `/etc/nginx/demo-assets.conf` and `nginx.conf` includes. It is the exact sibling of
   `demoReadiness.js` and `demoApi.js`, with one difference that matters: it emits **no** location
   in `build` mode. nginx matches the longest prefix, so a `/plugins/demo-04/` proxy would shadow
   the packaged files under `/plugins/` regardless of include order — build mode's static packaging
   is preserved by emitting nothing rather than by ordering. The gate is `readPluginSource(env)`
   against `config.env ?? process.env`, character for character the expression `pluginAssets.js`
   uses to decide whether to package, because the two answers must agree.
3. **Demo 04's own deployment.** `frontend/Dockerfile` (repo-root context, for `@unifi-theme` and
   `@ui-shell`) and `cqrs/Dockerfile`, plus `deploy/compose.shell.yaml`, an overlay that adds the
   frontend, the command API and the two projectors as containers. The frontend's own
   `nginx.conf` ends `try_files $uri $uri/index.html =404` — **no** SPA fallback, so a missing
   asset is a `404` and never HTML that a module loader would try to parse.

*Network isolation, kept and explained.* Neither side joins the other's private network. A
**third** network, `lab-shell-plugins`, is the edge. It is `external: true` in both compose files,
so Compose will not conjure it and a plain `docker compose up` of either stack is unchanged and
cannot drift onto it. Only `demo04-frontend` and `demo04-cqrs` put a foot on it; demo 04's `nats`
stays on its own default network and is unreachable from the shell. The shell joins it through
`deploy/cell/compose.plugins.yaml`, an overlay named on the command line — opt-in on both sides,
never silent. The upstream goes through a `set` variable with `resolver 127.0.0.11`, not a literal,
so nginx defers the lookup to the request: a stopped demo is a `502` on its own prefix, not a shell
that refuses to start.

*Coverage added.* 24 new specs across two files, both proved to FAIL against the unfixed state
before being kept:

- `demoAssetsGeneration.spec.js` — fixture-driven. The declaration is optional; it normalises to
  one upstream; eight malformed shapes are refused; the snippet serves the plugin prefix as a
  prefix and rewrites nothing; it defers the lookup with a resolver; it never intercepts an
  upstream status; it emits nothing in `build` mode; unset source means `registry` and an unknown
  source throws; one location per declaring demo.
- `hostedPluginAssets.spec.js` — read against the **real repo**, so it rots when the arrangement
  does. Each declaring demo ships a frontend image and an nginx conf that ends `=404` with no SPA
  fallback; its files sit under `/usr/share/nginx/html/plugins/<id>` and its Vite `base` matches;
  the shell's `nginx.conf` includes the generated rule and names no demo; the shell's `COPY` lines
  reference no `frontend/dist`; the edge network is external on both sides, absent from both
  non-overlay composes, and carries only the two containers named above.

Removing the `assets` block fails the first spec of the repo-wide file; removing the `include` line
fails another. Both were reverted straight after.

*Checks actually run — the packaged image, not the dev proxy.*

- `docker build -f lab-shell/Dockerfile` — green. `docker run --rm lab-shell:16l cat
  /etc/nginx/demo-assets.conf` shows the one generated location, and the image's own
  `/usr/share/nginx/html` holds no `plugins/` directory, which is BR-AS03 still true.
- Demo 04 brought up from `deploy/` with `compose.yaml -f compose.shell.yaml`, and the **real**
  cell brought up from `deploy/cell/` with `compose.plugins.yaml` added. Through the cell's shell
  on `7110`: `remoteEntry.js` `200 application/javascript` (63 648 bytes), a CSS file `200`, the
  1.2 MB lazy chunk `200`, a `woff2` `200`, the prefix's own index `200`, and a missing asset
  `404` with nginx's error page, not the SPA.
- **Upstream unavailable, both ways.** `docker stop lab4-frontend` — the shell keeps serving `/`
  and its catalogue at `200`, the prefix answers `502`. Then the shell was **cold-started with no
  demo container running at all**: it came up, served `/` at `200`, and answered `502` on both the
  asset prefix and the readiness route. That is the property the resolver variable exists for.
- **Backend readiness — verified, separately.** `GET /demo-readiness/04-jetstream-cqrs` through the
  packaged shell: `{"ready":true}` with all three checks ok (stream `ODOMETER`, both KV buckets).
- **Interactive operation — verified, separately.** Through the packaged shell's own origin:
  `POST /demo-api/.../commands/register` → `{"seq":84143}`, `POST .../commands/travel` →
  `{"seq":84144}`, then `GET .../rehydrate?id=…` → the folded state, `usedSnapshot:true`. In a
  browser on the cell shell at `/demo-04` the remote **mounted** — nav entry, breadcrumb, all three
  tabs — the Showcase tab drew live KV and stream data over the demo's WebSocket, and the
  Performance tab's `GET /demo-api/.../bench` and three `POST .../bench/seed` calls all returned
  `200`. Loading assets alone would not have shown any of this.
- `npx vitest run` in `lab-shell` — 73 files, 890 specs, green (was 866). `npm run lint` — 0
  errors, 30 warnings, the existing baseline, `shell frame clean`. `npx vitest run` in
  `demos/04-jetstream-cqrs/frontend` — 35 files, 513 specs, green. `go test -count=1 ./...` in
  `demos/04-jetstream-cqrs/cqrs` — ok.
- `hostBundleFingerprint.mjs --verify` — unchanged at
  `7e6a007a2e92e58d321a9f6b59b4ba8190dc937083e5d424e33e6777c13c3bf0`. No host runtime source was
  touched; the generator is build tooling.

*Registry protocol and lifecycle — untouched.* No file under `backend/mfe-registry-service/` or
`cmd/registry-acceptance/` was changed, and no BR-AS rule owned by `registry` mode was amended.
The shell still fetches its catalogue the same way; only what answers a `/plugins/<id>/…` request
changed.

*Limitations, stated plainly.* The frontend image had to copy demo 04's `README.md`, `docs/` and
`diagrams/` into the build, because the About page imports them with `?raw` — they are build
inputs, not runtime assets, and the image build fails without them. Verified on macOS with Docker
Desktop only. The registry-acceptance gate was not re-run for this task, on the grounds that
nothing it asserts was touched. `demo04-cqrs` passes only `-url` and `-addr`, never `-origin`:
F-3's "no CORS added to any demo backend" still holds, and the browser reaches the command API
through the shell's origin, which is 16j's arrangement working as designed.

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

### Phase 17 — APPROVED (design gate passed 2026-09-24) — One navigation tree: plugin nav entries merge into shell-owned groups

**Status: OPEN 2026-09-24. The design gate passed the same day it was proposed. D17-1, D17-6 and
D17-7 were decided by the user; D17-2 to D17-5 were delegated and stand as this entry recommends
them. Settled decisions are not re-opened. The task checklist is derived below. No task is done.**

Raised by the user 2026-09-24, in their own words: they do not prefer the current arrangement and
want "a single navbar where we merge the nav bar elements from the mfe plugins". The worked example
they gave:

```
MFE plugin 1 contributes      MFE plugin 2 contributes       The shell renders
- JETSTREAM                   - JETSTREAM                    - JETSTREAM
  - Lesson 1                    - Lesson 3                     - Lesson 1
  - Lesson 2                  - TOPOLOGY                       - Lesson 2
                                - 3 NATS Cluster               - Lesson 3
                                                             - TOPOLOGY
                                                               - 3 NATS Cluster
```

Two plugins name the same group; the shell shows one band with both plugins' items in it. A
collision or clash is HIGHLIGHTED rather than hidden.

#### The problem this phase would close

Demo 04, embedded in the shell, draws **two** navigation rails: the shell's own sidebar, and a
second lesson rail inside its route's left column (`demos/04-jetstream-cqrs/frontend/src/plugin/OdometerRoute.vue`).
That is not a defect — the shell declares no sidebar extension point, so the demo had nowhere else
to put its lesson index, and demo 04's own `CLAUDE.md` records the choice. It is a preference the
user has now stated against.

#### What already exists, and what does not

The groundwork is further along than it looks, and the gap is narrower than the feature sounds.

- **`group` is already in the contract.** `lab-shell/src/shell/registry/manifestSchema.js:324`
  accepts and normalises a `group` string on a `navigation` contribution.
- **Nothing reads it.** That schema line is the ONLY occurrence of `.group` in `lab-shell/src`.
  Neither `contributionRegistry.js` nor `placementPolicy.js` nor `App.vue` looks at it. It is dead
  data, carried since the schema was written.
- **The architecture doc already promises it.** `ARCHITECTURE-APP-SHELL.md:128` documents
  `contributions.navigation` as carrying "groups". The intent is written down; the render is not.
- **A two-level nav component already exists, proven, and the shell alone does not use it.**
  `shared/ui-shell/NavList.vue:5-41` supports an `{ eyebrow, items }` model with collapsible
  banding, and is covered by `demos/01-dictionary/frontend/admin/src/components/NavList.spec.js`.
  Demo 01's `admin` (`src/App.vue:223`) and `seafreight-app` (`src/App.vue:223`) both render it.
  `lab-shell/` is the one frontend that hand-rolls its rail instead.
  `ARCHITECTURE-APP-SHELL.md:1250-1258` names NavList as the definition of the supported navigation
  hierarchy.
- **The shell's sidebar is hardcoded.** `lab-shell/src/App.vue:104-166` is two literal
  `nav.nav-group` blocks: eyebrow `Shell` with two fixed links, and eyebrow `Features` with a single
  FLAT `v-for` over `shell.contributions.navigation`, sorted by `order`, `pluginId`,
  `declarationIndex` (`contributionRegistry.js:334-340`).
- **The sidebar is not an extension point.** There are three, all in
  `lab-shell/src/shell/extensions/extensionPoints.js:137-155`: `shell/topbar-controls/v1`,
  `shell/footer/v1`, `shell/home-main/v1`. No `shell/sidebar/*` exists, and this phase does NOT
  propose adding one — a plugin contributes a nav ENTRY, it does not fill a nav REGION. BR-AS07's
  "plugins fill targets; they never choose where a target lives" is unchanged by this phase.
- **No nav collision detection exists.** The shell refuses duplicate plugin ids
  (`bootShell.js:72-82`), duplicate contribution ids inside one manifest
  (`manifestSchema.js:151-158`), route-prefix conflicts (`placementPolicy.js:139-156`) and
  duplicate extension-point declarations (`bootShell.js:94-104`). It compares NO nav labels and NO
  group values, anywhere, in `lab-shell/src` or `lab-shell/tools`.

#### The one real obstacle — a nav entry must name a route

`manifestSchema.js:316-318` requires a navigation contribution's `route` to be a kebab-case LOCAL
CONTRIBUTION ID, and rejects a path outright. BR-AS12 is the rule behind it. Demo 04 declares
exactly ONE route (`/demo-04`) and holds its two lessons as internal view state, so today the shell
has nothing to point two nav items at. Merging groups changes nothing until that is resolved.

This is the decision the gate turns on, and it is not a small one: demo 04's own `CLAUDE.md`
records "One route contribution" as a considered and REJECTED alternative — splitting the lessons
into two routes "would change how the demo is navigated". This phase re-opens that decision
deliberately, and the demo's file must be amended in the same commit if it is reversed.

#### Decisions the gate must settle

Revised 2026-09-24 after an independent review by Codex. The review's findings are folded in
below rather than kept in a separate list; where it disagreed with the first draft, the review
wins and the first draft's wording is gone.

- **D17-1 — How a nav item addresses a lesson.** Option A: demo 04 contributes two routes
  (`/demo-04/lesson-01`, `/demo-04/lesson-02`) and two nav entries, and the embedded rail is
  deleted. Option B: a nav entry may carry a path suffix under its own plugin's prefix, leaving one
  route. **A is recommended** — the shell already owns deep links, and B invents a second way to
  address a page while weakening BR-AS12. A gives each lesson its own address most directly, within
  the contract that already exists.

  A is NOT a two-line change, and the gate must settle its consequences with it:
  - **Deep link and refresh.** `/demo-04/lesson-02` must load lesson 02 directly, from a cold
    browser, in both catalogue sources.
  - **What `/demo-04` becomes.** Redirect to a default lesson, or an index page? A bare prefix that
    renders nothing is the worst of the three.
  - **Active state.** Which nav entry is marked active on each path, and what is marked on the
    bare prefix before the redirect resolves.
  - **Readiness, per lesson or per plugin.** BR-AS79's three-state gate is asked once per demo
    today. Two routes do not obviously mean two probes, and probing twice for one backend would be
    a regression.
  - **Route rules.** Whether one plugin may claim several routes under its own prefix without
    tripping `route-prefix-conflict` (`placementPolicy.js:139-156`). It should not, but the rule
    was written for cross-plugin conflict and must be read again before it is relied on.
  - **Standalone is unchanged.** `20401` keeps its own rail and its own view state. Only the
    embedded entry changes. The one-build-two-entries rule holds.

- **D17-2 — What a group IS. A string is not enough.** The first draft proposed a bare string
  matched exactly. That is rejected. A group is an object with a stable identity separate from its
  display text:

  ```js
  group: { id: 'jetstream', label: 'JETSTREAM' }
  ```

  **Merging is by `group.id` and never by `group.label`.** A bare string collapses two failures
  into one field: `JETSTREAM` and `JetStream` would become two accidental bands, while two
  genuinely unrelated groups that happen to share a visible word would become indistinguishable.

  This is a **schema change with a migration**, not a read of existing data.
  `manifestSchema.js:324` accepts a string today and coerces anything else to `null`. The gate must
  say whether a string is still accepted as shorthand (`id` = the string, `label` = the string) or
  is rejected outright. Shorthand is recommended: no plugin ships a `group` today, so nothing
  breaks either way, and shorthand keeps a one-lesson plugin's manifest short.

  **Who owns `order` is a real tension, not an oversight.** The review proposed `order` inside the
  group object. BR-AS07 says "plugins fill targets; they never choose where a target lives", and
  two plugins naming the same `group.id` with different `order` values is itself a clash. The
  recommendation is therefore: a plugin declares `{ id, label }` only, and the SHELL owns group
  placement (D17-3). If the gate admits a plugin-supplied `order`, it must at the same time say
  which plugin wins when two disagree, and that answer must be deterministic, not first-seen.

- **D17-3 — Deterministic ordering, stated as a cascade.** "The shell owns ordering" is not an
  answer; catalogue arrival order differs between `build` and `registry`, so an unstated rule
  produces two different menus from one set of manifests. The rule must be written out and must
  terminate in something stable:

  1. a shell-owned group order table, for the groups the shell knows;
  2. then any group not in the table, ordered by `group.id` — NOT by first-seen;
  3. then, within a group: `order`, then `pluginId`, then `declarationIndex` (the cascade
     `contributionRegistry.js:334-340` already uses);
  4. an entry with no `group` needs a defined home. The existing `Features` band is the obvious
     one, which means that band keeps its name and is not quietly renamed by this phase.

- **D17-4 — What counts as a CLASH.** The first draft named one case. That is too narrow. The gate
  must rule on at least these, and say for each whether it is a clash (rendered and marked) or a
  refusal (omitted):
  - same `group.id`, conflicting `group.label` — two plugins disagree about a band's name;
  - same `group.id`, conflicting group order, if D17-2 admits a plugin-supplied order;
  - two groups with DIFFERENT `group.id` and the SAME visible label — the reader sees one name
    twice;
  - the same child label inside one group, pointing at different routes;
  - duplicate child identity inside one group;
  - a nav entry whose route is missing, refused, withdrawn or not permitted.

  The same `group.id` is the FEATURE, not a clash — it merges. That is the whole point of the
  phase.

- **D17-5 — Clashes need their own diagnostic channel, not `refusals`.** The first draft said the
  shell would reuse `refusals`. That is wrong and is withdrawn. `refusals` explains a contribution
  that was NOT placed; the user's requirement is that both clashing entries stay VISIBLE and both
  are marked. Overloading one collection with "dropped" and "kept but marked" makes the Plugins
  screen lie.

  The proposal is a separate `navigationClashes` collection on the contribution registry, each
  entry naming the clash kind and BOTH owning plugins, surfaced on the Plugins screen beside
  `refusals`. The mark drawn in the rail must be fitted into `navMark.js`'s existing precedence
  (BR-AS60, "one nav item, one mark") rather than drawn beside it — the gate must say where a
  clash mark sits against a load-status dot and a health dot.

- **D17-6 — How the shell renders the tree. `NavList.vue` cannot be dropped in unchanged.** The
  first draft implied it could. It cannot: `NavList.vue` is selection-based — it requires a
  `modelValue` string (`:26`), emits `update:modelValue` (`:29`) and renders each item as a
  `<button>` with `:aria-pressed` (`:98-105`). The shell's navigation is ROUTER-backed
  (`App.vue:139-158` renders `router-link`), and it draws marks `NavList` knows nothing about.
  Three ways out, and the gate must pick one:
  - **A** — extend `NavList.vue` to render a router link when an item supplies one, and to take a
    per-item mark slot. One component for the whole repo; demo 01's `admin` and `seafreight-app`
    keep working unchanged.
  - **B** — a shell-specific tree adapter, leaving `NavList` alone.
  - **C** — extract a shared presentational primitive underneath both, keeping routing in the
    shell.

  **A is recommended.** The repo's standing rule is to add the missing thing to the shared
  component rather than fork it, and B duplicates two-level banding in a second place. C is the
  cleanest long-term shape and the most work; it is worth choosing only if a third consumer is
  already in view.

#### Proposed business rules (not yet approved, numbers not yet allocated)

- Plugin navigation entries are merged into shell-owned groups. A group is IDENTIFIED by the
  plugin (`group.id`) and PLACED by the shell. Merging is by id, never by display label.
- A navigation clash is reported and marked, never silently resolved. Both clashing entries remain
  visible and attributable to their plugin, and the report is a diagnostic distinct from a refusal
  — a refusal means a contribution was not placed, a clash means it was.
- The merged navigation tree is a pure function of the manifests, not of the order in which
  plugins arrived. The same manifests produce the same tree in both catalogue sources.
- The shell's sidebar remains host-owned and is not an extension point. This phase adds no way for
  a plugin to render arbitrary content into the rail.

#### Acceptance checks the phase must satisfy

Listed at gate time so they cannot be negotiated down later. Every one of them must hold in BOTH
catalogue sources.

- **Build/registry parity.** The same manifests produce the same grouped tree, in the same order,
  with the same clash diagnostics, whether the plugins arrived from `build` or from the registry.
  This is the check that catches an arrival-order dependency (D17-3).
- The exact two-plugin example at the head of this phase renders exactly as drawn.
- Two plugins declaring the same `group.id` with different `group.label` are both rendered and
  both marked.
- Two groups with different `group.id` and the same visible label are both rendered and both
  marked.
- The same child label in one group, pointing at different routes, is rendered twice and marked.
- Withdrawing a plugin updates the merged group live and leaves no empty band and no dead link
  (BR-AS56).
- A nav entry whose route was refused leaves a visible diagnostic and NO clickable dead link.
- A clash mark is reachable by a screen reader, not colour alone, on BOTH affected entries.
- Standalone demo 04 on `20401` keeps its own rail; the embedded plugin has none.
- The host bundle fingerprint is unchanged by adding a plugin (BR-AS03), which
  `lab-shell/tools/hostBundleFingerprint.mjs` already proves.

#### Out of scope

A sidebar extension point. Nested groups deeper than two levels. Any change to demos 02 and 03,
which are shell-owned intro pages with no plugin. Any change to how `route-prefix-conflict` is
decided between DIFFERENT plugins.


#### Design gate — passed 2026-09-24

Three decisions were put to the user; the other three were delegated and stand as recommended
above. The constraints the user attached are part of the decision, not commentary, and a task
that ignores one has not met the gate.

**D17-1 — APPROVED: two routes.** Demo 04 declares `/demo-04/lesson-01` and `/demo-04/lesson-02`
and contributes one nav entry each. With the user's constraints:

- **Both routes render the SAME lesson component.** This is a routing change, not a UI fork. No
  panel is duplicated, no second component appears, and the plugin is not split.
- **The embedded rail is REMOVED; standalone navigation is PRESERVED.** `plugin/OdometerRoute.vue`
  loses its left column; `App.vue` on `20401` keeps its own rail unchanged.
- Recorded for the record, in the user's words: two routes are **not the only possible design**,
  but they fit the existing route-contribution contract most directly. That is why this option
  won, and the note guards against the plan later claiming the alternatives were unworkable.

**D17-6 — APPROVED: extend `NavList.vue`.** The shared renderer is reused rather than forked. Five
boundaries, all explicit, all testable:

- **Existing button-based navigation keeps working.** Demo 01's `admin` and `seafreight-app` are
  unchanged consumers and must stay that way.
- **A router-backed entry renders a REAL link** — open-in-new-tab works, and the browser's own
  active-route behaviour applies. A button that calls `router.push` does not satisfy this.
- **An optional per-item slot** carries a health or clash marker.
- **NavList only RENDERS.** Group merging, ordering and collision detection stay in the shell.
  Nothing in `shared/ui-shell/` learns what a plugin is.
- **Group identity and any collapse state key off stable IDs, never display labels.** This extends
  D17-2's rule past merging and into the component's own persisted state. Collapse is a LEVEL-1
  feature — the outer `{ group, sections }` banner — and the shell's rail does not use it; see A5
  as corrected.
- **Regression coverage for the existing consumers ships beside the new shell tests.** Demo 01's
  `NavList.spec.js` is the existing guard and must grow, not merely keep passing.

**D17-7 — APPROVED (new at the gate): the bare prefix redirects, and the PLUGIN declares it.**
`/demo-04` resolves to `/demo-04/lesson-01`. Old links keep working and lesson 01 is marked active.
The user's constraints make this a contract addition rather than a one-line router rule:

- **No demo-specific redirect may be hardcoded in `lab-shell`.** The plugin declares its own
  default destination; the shell handles the declaration generically, exactly as it handles every
  other contribution.
- **The destination still passes the normal checks** — permission, withdrawal and readiness. A
  redirect must not become a side door around BR-AS79's readiness gate or BR-AS56's withdrawal.
  A default that points at a withdrawn or refused route must not leave a dead prefix.

**D17-2 to D17-5 — delegated, and stand as written above.** Group is `{ id, label }` with string
shorthand accepted; ordering is the four-step cascade ending in `group.id`; the clash list is the
six cases named; clashes report through a `navigationClashes` collection and never through
`refusals`.

#### Task checklist — derived 2026-09-24 — SUPERSEDED

**Superseded the same day by "Task checklist — REVISED" at the foot of this phase. Kept for
the record only: it omitted the Go contract entirely, and it split demo 04's manifest from its
curated registry copy across two tasks. Do not work from the list below.**

Ordered so that each task is shippable and green on its own. The shell's side comes first, because
demo 04 cannot contribute a group until the shell can read one.

- **17a — The contract.** `group` becomes `{ id, label }` in `manifestSchema.js`, with a plain
  string accepted as shorthand (`id` = `label` = the string). Add the plugin's default-route
  declaration for D17-7. Specs first. No renderer changes.
- **17b — The merge.** `contributionRegistry` groups navigation by `group.id` and applies D17-3's
  four-step cascade. Pure data; still rendered flat. This is where build/registry parity is proven.
- **17c — The clash channel.** `navigationClashes` on the registry, the six cases of D17-4,
  surfaced on the Plugins screen beside `refusals`. Still no renderer change.
- **17d — `NavList.vue` grows a link mode and a marker slot**, under D17-6's five boundaries.
  Demo 01's `NavList.spec.js` grows its regression coverage in the same commit.
- **17e — The shell's rail switches to `NavList`.** `App.vue:104-166`'s two hand-rolled blocks are
  replaced. `navMark.js` gains the clash mark's place in its precedence (BR-AS60).
- **17f — The generic default-route redirect**, with the permission, withdrawal and readiness
  checks D17-7 requires.
- **17g — Demo 04 splits.** Two routes, two nav entries, one shared lesson component, the embedded
  rail deleted, the standalone rail untouched. `demos/04-jetstream-cqrs/CLAUDE.md` is amended in
  the same commit, because it records the one-route choice as rejected.
- **17h — The rename that was deferred.** `OdometerRoute.vue` and the `odometer` / `odometer-nav`
  manifest ids move with the route split, once, here. `registry.json` changes with the same bytes.
- **17i — The acceptance checks above, as specs.** Anything not already covered by 17a-17h.

Business rules are written and approved before the task that implements them, per the repo's
required sequence. Numbers are allocated when 17a opens.


#### Gate amendments — 2026-09-24, after a second Codex review

The gate above stands; the chosen design is not re-opened. Five gaps made the checklist
not-yet-implementable, and all five were verified against the code before being accepted. Where an
amendment contradicts the entry above, the amendment wins.

**A1 — The contract is TWO contracts, and the second one is Go.** The checklist named only the
frontend schema. `demos/01-dictionary/backend/mfe-registry-service/registry/internal/domain/registry.go:128`
declares `Group string`, so a `{ id, label }` object fails registry decoding outright: build mode
would go green while registry mode broke, which is precisely the split task 16l was opened to
close. The default-route declaration has the same problem — an unknown field is dropped by ordinary
decoding, or refused by manifest-drift checking, unless the backend is taught it.

The backend's own comment on that struct is the reason this is delicate:
`Contribution` is deliberately ONE flat struct over all five kinds, so that the two sides do not
grow two definitions of one contract. Changing `Group`'s type touches every kind's decoding path.
Backend work therefore gets its own task, before the merge, covering: the types, acceptance of the
legacy string, the persistence and read round-trip, signed registration, and drift coverage.

**A2 — The six clash cases were a checklist, not a policy. Settled here.** The entry asked which
cases are clashes and which are refusals, and the gate summary then implied all six were clashes.
That was contradictory. The settled policy is **three clashes, two inapplicable, one unchanged**:

| Case | Ruling |
|---|---|
| Same `group.id`, conflicting `group.label` | **CLASH.** Both rendered, both marked. |
| Different `group.id`, same visible label | **CLASH.** Both bands rendered, both marked. |
| Same child label in one group, different routes | **CLASH.** Both rendered, both marked. |
| Same `group.id`, conflicting group ORDER | **Inapplicable.** D17-2 gave the shell group placement; a plugin declares no order for a group, so the case cannot arise. |
| Duplicate child identity across plugins | **Inapplicable.** Identity is qualified `${pluginId}/${id}` (`manifestSchema.js:256`), so the same local id in two plugins is VALID and not a clash. Inside one manifest it is already the `duplicate-id` refusal (`manifestSchema.js:151-158`). |
| Route unavailable — missing, refused, withdrawn or not permitted | **Not a clash. Existing route admission and withdrawal behaviour remains UNCHANGED.** These are three different lifecycles and this phase merges none of them: `unresolved-route` (`placementPolicy.js:229-243`) and `permission-denied` (`placementPolicy.js:168-176`) are refusals; **withdrawal is NOT a refusal** — BR-AS56 takes away the plugin's routes and navigation, keeps the current-page behaviour, and is restorable. A dead nav link is still never rendered. |

Two things that policy forces, and which the phase must state rather than discover:

- **A conflicting label still has to DISPLAY something.** Marking both contributors does not choose
  the band's name. The displayed label is taken by the same cascade that orders the band —
  `pluginId`, then `declarationIndex` — so it is deterministic and identical in both catalogue
  sources. The losing label is not lost: it is named in the clash diagnostic.
- **Marker precedence, and what survives losing the dot.** BR-AS60 allows one nav item one mark,
  and load status already outranks health (`navMark.js`). A clash is a configuration fault, visible
  at index time and fixable by an operator; load and health are runtime faults the reader is
  looking at now. **A clash therefore ranks LAST of the three.** Because it can lose the dot, the
  clash must remain reachable without it: it stays in the item's accessible description and on the
  Plugins screen in every case. "Highlighted" is satisfied by the diagnostic plus the description,
  not by the dot alone.

**A3 — 17g and 17h must be ONE task.** The checklist changed demo 04's manifest in one task and its
curated `registry.json` copy in the next. BR-AS78 requires those two to be byte-identical, so the
intermediate state is a broken registry — which contradicts "each task is shippable and green on
its own". The route ids, the plugin's exported components, the curated registry entry, the file
rename and the demo's own scoped docs move together, in one commit.

**A4 — D17-7 needs concrete behaviour, not just a principle.** Settled:

- **Declaration.** A `route` contribution may carry `default: true`. At most one per plugin; a
  second is a refusal. This form was chosen over a plugin-level field NAMING a route id because it
  makes the three things the review asked for true by construction: the target exists, the plugin
  owns it, and no loop is expressible.
- **Failure.** If the default route is refused, withdrawn or not permitted, the prefix does NOT
  redirect. The shell renders its normal unavailable state for that plugin, and no nav entry links
  to the dead route. A plugin declaring no default leaves the bare prefix behaving exactly as it
  does today.
- **How the shared component learns which lesson.** From the ROUTE, not from its own default.
  `useDemoState`'s view state currently defaults to lesson 01, and under two routes that default
  becomes a bug: a cold `/demo-04/lesson-02` link would render lesson 01. The phase must cover a
  cold link to lesson 02, a refresh on it, browser back and forward between the two, and what
  happens to readiness when the reader switches lesson — **one probe per plugin, not one per
  lesson.** Probing twice for a single backend would be a regression against BR-AS79.

**A5 — `NavList.vue` needs a legacy adapter, not a consumer migration.** The tension is real but
narrower than the review assumed, and the file says so. NavList's LEVEL-1 band is already
`{ id, label }` and already keys collapse off `group.id` (`NavList.vue:56`, `:63`, `:84`). It is
the LEVEL-2 band that is a bare string: a section is `{ eyebrow?, items }` with no identity
(`NavList.vue:12`, `:93-96`), and the shell's groups map onto that level.

The resolution is an **optional `section.id`, falling back to the eyebrow string when absent**.
Demo 01's `admin` and `seafreight-app` pass no id and are not touched; the shell passes `group.id`
and gets stable identity for the band. This is recorded as the chosen approach: a legacy-input
adapter, NOT a coordinated consumer migration. The fallback is itself a test, not an
implementation detail.

**Corrected 2026-09-24 (review, option A).** As first written this paragraph said the shell "gets
stable collapse", which the code does not do and was never going to. `NavList.vue` collapses the
LEVEL-1 `{ group, sections }` banner only; a level-2 section's `id` is its stable identity in the
list and nothing more. The shell's rail passes flat sections, so its plugin bands are not
collapsible — as the hand-rolled rail they replaced never was. Making them collapsible was weighed
and rejected here: it would add an affordance to a shared component and a design decision to the
rail that nobody has asked for. `section.id` earns its keep on identity alone — a band renamed by
its owner must not read as a different band.

**A6 — Withdrawal is not a refusal (user, 2026-09-24).** Stated because the amendment table above
grouped four unavailable-route situations into one row and read as if all four were refusals. They
are not. Existing route admission and withdrawal behaviour remains unchanged by this phase: a
withdrawal removes navigation as it does today, keeps the reader's current page behaving as it
does today, and remains restorable (BR-AS56, BR-AS58). No task in this phase may re-classify a
withdrawal as a refusal, and no task may route a withdrawal through `navigationClashes`.

**A7 — The frontend contract must NOT trigger an early manifest migration (user, 2026-09-24).**
This is an ordering constraint on the tasks, and it is the practical half of A1. 17a teaches the
frontend schema to ACCEPT the new shapes; it does not make anything emit them. **No production
manifest emits a `group` object or a `default: true` route until 17b is done and the full registry
round-trip — decode, persist, read back, signed registration, drift — is tested.** Demo 04 is the
first production manifest to change, and it changes in 17h, after both contracts hold. A plugin
that emitted the object form between 17a and 17b would be a plugin the registry could not carry.

#### Task checklist — REVISED 2026-09-24 by the amendments above; none done

- **17a — DONE 2026-09-24. The frontend contract, ACCEPTANCE ONLY (A7).** `group` becomes `{ id, label }` in
  `manifestSchema.js`, with a plain string accepted as shorthand. `default: true` on a route
  contribution, at most one per plugin. Specs first. No renderer changes, and — A7 — **no shipped
  manifest changed**: the schema learns to read the new shapes while every manifest in the repo
  still emits the old ones.
  Landed as BR-AS83 and BR-AS84 in `demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md`, specced in
  `lab-shell/src/shell/registry/manifestSchema.spec.js` (12 new specs) and implemented in
  `manifestSchema.js`: `validateNavigationGroup` normalises `group` to a frozen `{ id, label }`,
  accepts the plain string verbatim as shorthand, refuses a non-kebab-case explicit id, a missing
  or blank label and a non-object, and drops a placement key rather than refusing it; `default`
  normalises to a boolean on route contributions, and a second one refuses the whole plugin with
  the new code `duplicate-default-route`. Nothing consumed `group` before this change and no
  manifest declared one, so no renderer moved. Suite 902 green, lint clean.
- **17b — DONE 2026-09-24. The Go contract (A1).** `registry.go`'s `Contribution` learns the object
  form and keeps accepting the legacy string; persistence and read round-trip, signed registration,
  and manifest drift all covered. Registry mode must be provably level with build mode before the
  merge lands.
  Landed as BR-AS85. `internal/domain/navgroup.go` is a new `NavGroup` type whose `UnmarshalJSON`
  takes either form and whose `MarshalJSON` re-emits **the form that was written** — required, not
  cosmetic, because `application/drift.go` re-marshals both sides and compares the text, so a
  one-sided promotion would read as permanent drift on `contributions`. `Contribution.Group` became
  `*NavGroup` (a pointer, so `omitempty` still fires for an absent group) and gained
  `Default bool`; without the latter, `CompareManifest`'s `DisallowUnknownFields` would have read
  any manifest carrying `default` as `invalid-manifest`. `admissible.go` now holds the same door as
  the shell: a non-kebab-case explicit group id, a group with no label, a blank shorthand and a
  second default route are all refused, while the shorthand is exempt from the id pattern and an
  unknown key inside the group object is dropped rather than refused — a registry stricter than the
  shell would reintroduce the split A1 exists to close.
  **The A7 gate is now satisfied.** The full round-trip is proven, not assumed: `navgroup_test.go`
  (decode/encode in both forms, idempotent twice-round entry marshal), `admissible_test.go` (the
  write door, refusals and admissions), `store_integration_test.go` (through Postgres and back via
  a second `Store`, shorthand not promoted), `manifest_test.go` (a **signed** manifest carrying both
  new forms keeps its bytes verbatim and still reports `Attested()`, with a guard spec proving
  attestation can still fail), and `drift_test.go` (both forms and `default` compare `checked`; a
  real group-identity change still reports `drift`). Ginkgo 477 of 477, **0 skipped**, so the
  Postgres specs genuinely ran. `go vet`, `go build` and `gofmt` clean; both `shared/mferegistry`
  submodules green.
- **17c — The merge. DONE.** `contributionRegistry` groups by `group.id` and applies D17-3's
  four-step cascade, including the deterministic displayed label of A2. Pure data; still rendered
  flat. **BR-AS86** is the rule. `lab-shell/src/shell/contributions/navigationTree.js` is the
  projection and `contributionOrder.js` is the cascade, lifted out of `contributionRegistry.js`
  into a module of its own so the tree can import it without a cycle. The registry gained one
  getter, `navigationTree`, rebuilt on every read off the same reactive array the flat list is
  read from, so the two cannot disagree and a plugin placed into a running shell reaches both.
  Nothing renders it — `App.vue` is untouched, as the task says. The shell-owned table is
  `SHELL_GROUP_ORDER`, one band, `features` / `Features`, which is also the home of every
  ungrouped entry (step 4) and cannot be renamed by a plugin that claims the same id. A clash is
  not a refusal: both entries place, the shell's own label wins for a band it owns and otherwise
  the first claimant by `pluginId` then `declarationIndex` names it, and every distinct losing
  label is kept on the node as `labelClaims` for 17d to read out. Purity is specced three ways —
  a reversed index, three separate indexing passes, and a withdrawal followed by a restore all
  produce the identical tree. 21 new specs; lab-shell Vitest **923 green**, lint 0 errors.
- **17d — The clash channel. DONE.** `navigationClashes` on the registry, the THREE clash cases of
  A2, surfaced on the Plugins screen beside `refusals`. The other three cases get specs proving
  they are NOT clashes. **BR-AS87** is the rule.
  `lab-shell/src/shell/contributions/navigationClashes.js` derives the list from the tree on read,
  so it inherits BR-AS86's purity: a withdrawal takes a clash away and a restore brings it back,
  with nothing to clear. D17-5 is honoured literally — `refusals` is untouched, the new collection
  sits beside it on the registry, in `bootShell`'s inventory row and in its own `ul.clashes` on the
  Plugins screen, and a spec asserts the two lists never mix. A clash is a fault of a PAIR, so each
  record carries `pluginIds` and shows on the row of every plugin it names, which is the one shape
  difference from a refusal's single owner. The three silences are specced as hard as the three
  clashes: an agreed label, a band the shell owns, the same name at the same route, and the same
  name in two different bands all report nothing. 27 new specs (21 clash, 3 boot, 3 view);
  lab-shell Vitest **950 green**, lint 0 errors.
- **17e — `NavList.vue`. DONE.** A link mode, a per-item marker slot and the optional `section.id`
  of A5, under D17-6's five boundaries. Demo 01's `NavList.spec.js` grew in the same commit,
  including the id-absent fallback. **BR-AS88** is the rule.
  One markup block serves both modes — `<component :is="tagFor(item)">` renders a `<button>` as
  before and a real `<router-link>` when the item carries `to` — so a change to how an item looks
  cannot reach one mode and miss the other, and a button calling `router.push` was never on the
  table. `modelValue` became optional, because a link-mode list holds no selection of its own: in
  that mode nothing is emitted and no `aria-pressed` is written. The marker slot is scoped to the
  item and empty for every consumer that passes no slot. `section.id` falls back to the eyebrow
  string, so `admin` and `seafreight-app` are untouched, and the fallback is itself a spec.
  The coverage is split across two apps on purpose: `admin` drives the grouped shape but has no
  `vue-router` dependency, so its 9 new specs use `RouterLinkStub`, and the half a stub cannot
  prove — a real `href`, the router alone marking what is active, and the mark following a
  `router.push` nobody told the component about — lives in
  `lab-shell/src/shell/ui/navListLinkMode.spec.js` against a real router. 14 new specs
  (9 admin, 5 lab-shell); admin Vitest **351 green**, seafreight-app **36 green**, lab-shell
  **955 green**, lab-shell lint 0 errors. `shared/ui-shell/` sits outside every `lint` script in
  the repo, so the edited component is guarded by its specs and not by ESLint.
- **17f — The shell's rail switches to `NavList`. DONE.** `App.vue`'s two hand-rolled blocks are
  gone; the rail is `navigationTree` rendered through the shared component. `navMark.js` gained the
  clash mark last in its precedence, and the accessible description that survives losing the dot
  (A2). **BR-AS89** is the rule, and **BR-AS88**'s link bullet gained the root-link clause.
  The rail split in two rather than growing an `App.spec.js` there has never been: `shellNavSections.js`
  turns the tree into sections and is a pure function, and `ShellNav.vue` supplies the mark slot and
  nothing else — the same split `ShellFooter.vue` and `RegistrySignalBanner.vue` already use, so the
  rail's ORDER and its MARKS are assertable without a DOM. Two smaller things fell out of the swap.
  `iconClass.js` bridges the two icon forms: `admin` and `seafreight-app` pass imported SVG
  components, while every manifest names an icon by PrimeIcon class string, so the shell wraps the
  string in a functional component cached on the string itself — `shared/ui-shell/` learned nothing
  about either (BR-AS88). And `.nav-item` gained `text-decoration: none` in `app-shell.css`, because
  every prior consumer rendered a `<button>` and no link had ever been drawn onto that class.
  One expected change was tried and then removed: an `exact` flag for the Home link. A spec written
  against a real router proved it dead — vue-router decides active from the matched route RECORD,
  not from the path text, and the shell's routes are flat siblings, so `/` is never active on the
  screens under it. The flag went; the spec that disproved it stayed, because the day that changes
  the rail goes wrong quietly on every screen.
  **Corrected 2026-09-24 (second review).** That reasoning was right about `/` and wrong as a
  general claim, and the entry said it too broadly. Record matching is not descendant matching:
  sibling records never light each other, so dropping the rail's hand-written prefix match also
  darkened an entry on its own detail pages. See the second follow-up below.
  Clash membership is asked of the clash record, not guessed from the plugin: every record carries
  its participants' qualified ids, so a plugin with four entries and one clashing label marks one of
  the four, and a `duplicate-group-label` whose other participant is the shell (`qualifiedId: null`)
  blames nobody. 35 new specs (9 `navMark`, 22 `shellNavSections`, 10 `ShellNav`, less the 2 that
  replaced the withdrawn `exact` pair in `navListLinkMode`); lab-shell Vitest **997 green**, admin
  **351 green**, seafreight-app **36 green**, lab-shell lint 0 errors.
- **17f review follow-up (2026-09-24). DONE.** An external review of `5015952` raised four points.
  Two were based on a misreading — it took the phase to be claiming 17g, which nothing does. Two
  were real and are fixed here, with the wording call taken as **option A**.
  The real defect was a **split door** on `default`, against BR-AS85. `manifestSchema.js` read
  `raw.default === true`, so `default: "yes"` was admitted as no declaration; the registry carries
  the field as a Go `bool` and its decoder refuses the same value. A manifest could therefore work
  in `build` mode and fail in `registry` mode — the one thing BR-AS85 exists to prevent. A
  non-boolean `default` is now refused as malformed, which is the Go behaviour spelled out on the
  shell's side rather than a new rule.
  The boundary case was real and smaller: `SHELL_GROUP_ORDER` holds only `features`, so the rail's
  own `Shell` band is not in the tree and clash detection could not see it — a plugin band labelled
  `Shell` drew the word twice and reported nothing. `RESERVED_RAIL_LABELS` now lives beside the
  order table, seeds the duplicate-name pass, and is where `shellNavSections.js` takes the band's
  label from, so the name the rail draws and the name the check compares against cannot drift. A
  reserved band alone is not a clash; it becomes one only when a manifest spells the same word, and
  the shell is never blamed for it. Being reserved is about the WORD — it opens no door into the
  band, and BR-AS07 is unchanged.
  The collapse point was a **wording** defect, not a code one. A5 said the shell "gets stable
  collapse" from `section.id`; `NavList.vue` collapses the level-1 banner only, and the shell's
  rail passes flat sections. Option A corrects the words: `section.id` is identity and nothing
  more, and the rail's plugin bands are not collapsible — as the hand-rolled rail they replaced
  never was. Building it was weighed and rejected: an affordance in a shared component and a design
  decision in the rail that nobody has asked for.
  5 new specs (2 clash, 3 manifest, less the one that asserted the old `"yes"` reading); lab-shell
  Vitest **1000 green**, lint 0 errors.
- **17f second review follow-up (2026-09-24). DONE.** The same reviewer returned with two findings
  it had left out of the first pass. Both were reproduced and both were real.
  **A clash lost its participants.** `navigationTree.js` folded a band's label claims down to one
  per DISTINCT label, and `navigationClashes.js` built `participants` from that folded list. So
  with A and B both asking for `JETSTREAM` and C asking for `Streams`, B took part in the clash and
  was never named in it — no dot, no words, while its neighbours carried both. The fold is right
  for the MESSAGE, which is a sentence about names; it is wrong for the blame. The tree now carries
  `labelOwners` — every claimant, cascade order, nobody folded away — beside the unchanged
  `labelClaims`, and both clash kinds take their participants from it. **BR-AS87** gained the rule.
  **A regression, and mine.** The 17f entry above claimed the router "already gets this right" and
  needed no matching flag. It does for `/`; it does not for a section and its detail pages, which
  are sibling records. The rail before 17f matched `route.path === target.path ||
  route.path.startsWith(`${target.path}/`)` by hand, and deleting `isActive` with the old markup
  quietly took that away. It is restored where it can be tested: an item may carry `active` as a
  boolean and `NavList.vue` then stands the router's own matching down for it, so the class has one
  author. `shared/ui-shell/` still imports no router — it may not, two of its three consumers have
  none — so `ShellNav.vue` resolves the path and `shellNavSections.js` decides, for plugin entries
  only. The shell's own two links keep exact matching, which is what the hand-rolled rail gave
  them. **BR-AS88**'s link bullet and **BR-AS89** now say so.
  11 new specs (2 tree/clash, 6 `shellNavSections`, 3 `navListLinkMode`); lab-shell Vitest
  **1011 green**, admin **351 green**, seafreight-app **36 green**, lab-shell lint 0 errors.
- **17g — The generic default-route redirect. DONE.** `/<routePrefix>` now redirects to the route
  that declared `default: true`, and `lab-shell` names no demo doing it: both halves of the record
  come from the manifest. **BR-AS90** is the rule.
  The whole change is 19 lines in `shellRoutes.js`, and that is the point — the funnel every plugin
  route already passes through emits one more record. A4's three failure cases needed no code:
  a refused or unpermitted default never reaches the funnel, because the contribution registry
  dropped it at index time, so the bare prefix simply has no record and falls through to not-found;
  a withdrawal leaves the record in place and the existing guard refuses it by `meta.pluginId`,
  which is amendment A6 honoured by not writing anything; and readiness is untouched because the
  redirect lands on the real route record, whose component the demo gate already wraps. Each of the
  three is a spec that asserts the absence rather than a branch that implements it.
  Two edges are handled explicitly: a plugin whose default route's path already IS the bare prefix
  gets no self-redirect, and the record's name is `default-route:<pluginId>` — a colon cannot
  appear in a kebab-case qualified id, so it can never collide with a contribution's own name.
  The Go side needed nothing: `Contribution.Default` and `Entry.RoutePrefix` were carried in 17b,
  which is A7's ordering kept — the contract went first and the frontend followed it.
  13 new specs (10 record shape, 3 against a real router); lab-shell Vitest **1022 green**,
  lint 0 errors.
- **17h — Demo 04 splits, in ONE commit (A3).** Two routes, two nav entries, one shared lesson
  component driven by the route, the embedded rail deleted, the standalone rail untouched, the
  `odometer` / `odometer-nav` ids and `OdometerRoute.vue` renamed, `registry.json` updated with the
  same bytes, and `demos/04-jetstream-cqrs/CLAUDE.md` amended — it records the one-route choice as
  rejected.
- **17i — The acceptance checks as specs.** Anything not already covered by 17a-17h, cold
  lesson-02 links and back/forward included.


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
