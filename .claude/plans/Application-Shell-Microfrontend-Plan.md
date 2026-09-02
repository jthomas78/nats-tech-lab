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

### Phase 14 — COMPLETE (2026-09-02) — One container per plugin: the announcer moves into the plugin's own process

**Status: APPROVED 2026-09-02.** Option A and design decisions 1–10 approved. Scope widened the
same day by decisions 12 and 13, which add **BR-AS71 and BR-AS72** — the phase is no longer
rule-neutral, and the earlier "no new business rule IDs" line is superseded. Decisions 11 and 11b (credential naming, and the BR-D40 mechanism
it moves) were raised after approval by the connection-topology count below, and both are now
settled: the credentials are renamed to their plugin ids and excluded from tenant discovery by
directory. Tests are derived; code follows them.

Options review of record:
[`reviews/adr-announcer-topology-20260902.md`](reviews/adr-announcer-topology-20260902.md) — four
topologies compared against BR-AS54, BR-AS67, decision 8 and the credential-naming rule. **Option A
selected**; B (one announce-manager for all plugins), C (Compose anchors only) and D (one-shot
announce/unregister jobs) were rejected there, with reasons.

#### The problem this phase closes

Phase 13e is correct and stays correct — it is the *deployment shape* that is expensive. Today a
plugin developer ships **two** runtime units: the frontend image and an `-announcer` sidecar whose
Compose stanza is ~45 lines. The lab runs 5 plugin containers and 5 announcer containers for 5
plugins. Nothing about the trust model requires two containers; Compose simply has no pod, so
"a sidecar in the same pod" has no expression other than "a second container".

The measured DX cost is **not** the container count on its own. Minting a signing keypair, minting an
`-announcer` credential, adding a `publishers.json` row, and adding `REGISTRY_HEALTH_ORIGINS` /
`REGISTRY_ALLOWED_ORIGINS` entries are per-plugin chores that **no** topology removes. Any phase that
only deletes containers leaves most of the burden in place. This one addresses both halves.

#### Design decisions — approved 2026-09-02 (11 and 11b raised after approval, both settled 2026-09-02)

1. **The announcer becomes a library; the CLI becomes a thin wrapper over it.** Extract
   `cmd/announce-plugin`'s internals into `shared/mferegistry/announcer` behind a single
   `Start(ctx, Config)` that owns connect, announce, the release counter, and the SIGTERM unregister.
   `cmd/announce-plugin` keeps its existing `main_test.go` and stays shipped — it is still the right
   form for an announcer-only fixture and for unusual deployments. This is the same reusable-client
   shape a *backend* plugin will import in Phase 11/12, so the package is built once here rather than
   twice later.

2. **A frontend-only plugin's container gets a small Go static host as PID 1, not a second process.**
   A shared `mfe-plugin-host` image serves the plugin's built assets and calls
   `announcer.Start` in-process. Running nginx behind a Go supervisor was considered and rejected:
   two processes in one container is the thing this phase exists to remove, and it reintroduces the
   signal-forwarding problem that `stop_grace_period` currently works around.

3. **`nginx.conf` is the specification for the Go host, not a starting point.** Its four behaviours
   are load-bearing and each gets its own test: `/healthz` returns `no-store` JSON with **no** CORS
   header (BR-AS61 is server-to-server); `try_files $uri =404` with **no** SPA fallback, because
   `example-plugin-unreachable`'s fixture is a genuine 404 and an `index.html` fallback would turn it
   into a module parse error in the wrong state; a **named** `Access-Control-Allow-Origin` with
   `Vary: Origin`, never `*`, because `REGISTRY_ALLOWED_ORIGINS` is only half a statement if this
   side hands its code to anyone; and **no** `proxy_pass` of any kind.

4. **`example-plugin-unreachable` keeps the CLI form.** It has no web server by design, so it stays
   an announcer-only container. Five plugins therefore become four host containers plus one CLI
   container, and the "one publisher per plugin" property is unchanged.

5. **The release counter moves with the announcer, and stays a named volume.** BR-AS67 is unchanged
   in substance — the counter is still the publisher's state, still persisted outside the image,
   still injected immediately before signing. It is now attached to the plugin container instead of
   a sidecar. This needs a one-line amendment under BR-AS67, not a new rule.

6. **The plugin container joins the `backend` network — named as a trade-off, not hidden.** This is
   the one real cost of Option A: a publicly-served static origin and a NATS credential now share a
   network namespace. Mitigations are structural: the host serves only its asset root, the signing
   seed and creds are mounted outside that root, and decision 3 forbids any proxy route. Recorded in
   `ARCHITECTURE-APP-SHELL.md` as a lab trade-off **and** as a production review item — under
   Kubernetes this becomes a real second container in the same pod, running the same package, and the
   Go host goes back to being optional.

7. **Withdrawal stays keyed to SIGTERM of the plugin's own container.** BR-AS54 is untouched: a crash
   or a failed health probe still withdraws nothing. `stop_grace_period: 30s` moves to the plugin
   stanza for the same reason it exists today — the unregister is a request/reply round trip and
   Compose's default 10s turns a slow bus into a kill. This property is precisely what Option B would
   have cost, and is why B was rejected.

8. **One credential and one signing key per plugin, unchanged.** The bootstrap's existing reasoning
   holds: a single shared announcer credential destroys connection attribution in the Admin
   Connections panel and concentrates every signing key in one blast radius. The `nats.Name()` of the
   plugin host equals its credential name, per the repo's credential-naming rule.

9. **A scaffolder ships in this phase, not "later".** `scripts/new-plugin.sh` generates the plugin
   directory, the single Compose stanza, the `bootstrap-operator.sh` loop entry, the health/allowed
   origin mappings and the README port-table row. Per the problem statement above, this is the larger
   half of the DX win; shipping the container change without it would leave the burden mostly intact.

10. **`cmd/registry-acceptance` must pass unchanged, and is the gate for "done".** It drives
    `compose stop` / `start` on `example-plugin` and asserts withdrawn → returned at releases
    `N` / `N+1` / `N+2` with the other four as a control group. If the shape of that command has to
    change to accommodate this phase, the phase is wrong — that was the tell that disqualified
    Options B and D.

#### Business rules — confirmed 2026-09-02: no new BR IDs

The user confirmed this is a deployment-shape change only. Each rule below is unchanged in substance;
the single amendment is one line under BR-AS67, landing with the code in 14e:

- **BR-AS54** — unchanged. Withdrawal on explicit SIGTERM only; never on crash or health failure.
- **BR-AS67** — unchanged in substance; needs a one-line amendment noting the counter volume now
  attaches to the plugin container.
- **BR-AS61** — unchanged. The frontend probe target and its no-CORS `/healthz` contract survive the
  nginx → Go host swap; decision 3 makes that testable.
- **BR-AS15** — re-read before implementing. A plugin is built by its own toolchain into its own
  image; a **shared base image** is new for this repo and the claim should be confirmed as still true
  (the base contributes a server and a client library, not a build step or a shell import).

#### Connection topology — the fact that shapes decision 6 (added 2026-09-02)

Counted from `docker-compose.yml` and `bootstrap-operator.sh`, **per announced plugin, today**:

| Container | Network | NATS connections | HTTP |
| --- | --- | --- | --- |
| `lb-example-plugin` (nginx) | `frontend` only | **zero** | serves 2 callers: the browser on the published port `7111`, and the registry's `/healthz` probe on `example-plugin-frontend:80` |
| `lb-example-plugin-announcer` | `backend` only | **one** — `example-plugin-announcer.creds`, pub on exactly the announce and unregister subjects, sub on `_INBOX.>` only | **none**, in or out |

So there are **not** two connections in one place today. There are two containers, each holding
exactly one kind of link, and **they never talk to each other** — they share only the plugin id and
the manifest file. The frontend is HTTP-only with no bus access at all; the announcer is bus-only
with no listener at all.

**After this phase the totals are unchanged: one NATS connection and one HTTP listener per plugin.**
Nothing is added or removed; the two are merely held by the same process. The lab still opens five
announcer connections, still on five distinct credentials, still visible as five rows in the Admin
Connections panel.

What *does* change is network membership: the plugin container must now join **both** networks —
`frontend` because the registry's BR-AS61 probe dials it there (the registry straddles both) and the
browser reaches the published port, and `backend` because that is where `nats` is. That is decision
6 stated precisely. The existing `frontend` comment ("for uniformity only. This container has no
`proxy_pass` rule of any kind, so joining the network gains it nothing it can use") stops being true
of `backend`, and decision 3's no-proxy rule is what keeps it from mattering.

#### Decision 11 — the credential's name follows its holder — SETTLED 2026-09-02: option (a), rename

The repo's credential rule is that a dedicated credential is *named for its holder, spelled exactly
as that process's `nats.Name()`*, so that a Name/Credential mismatch in the Connections panel is a
signal rather than noise. Today the holder is a process called the announcer, and
`example-plugin-announcer` is right. After this phase the holder is the plugin host, and the name
would be describing a job the process does rather than the process itself.

**The call: rename.** Each plugin's credential becomes its plugin id — `example-plugin`,
`example-plugin-slow`, `example-plugin-activate-throws`, `example-plugin-incompatible`,
`example-plugin-unreachable` — and the host sets `nats.Name(pluginID)` to match. The rejected
alternative was keeping `-announcer` and pointing `nats.Name()` at it: no reseed, but the panel
would show a connection named for a container that no longer exists, which is the quiet drift the
naming rule exists to prevent.

Renaming an `nsc` user is delete-and-re-add, so this mints a new NKey for each of the five and needs
`docker compose down -v` plus a bootstrap reseed, with the compose mounts moving to the new
filenames in the same change. That is the lab's normal path and the release volumes are re-created
on a fresh boot anyway (BR-AS66), so the cost lands entirely inside 14a.

#### Decision 11b — BR-D40 stops keying on the name and starts keying on the directory

Discovered while pricing decision 11, and it is the reason the rename is not a pure find-and-replace.
`natstenants.NonTenantCredsSuffixes` is `[]string{"-announcer"}` — the suffix *is* how BR-D40 keeps
five plugin credentials from being read as five bogus tenants. Rename the stems to plugin ids and
that match evaporates, and the failure BR-D40 describes arrives five at a time. The plugin ids share
no suffix to match on, and matching the `example-plugin` prefix would only work because these five
are fixtures — a real `acme-widgets` plugin would not carry it.

**Move the family marker from the spelling to the location.** `natstenants.Discover` scans one
directory with `os.ReadDir` and already skips entries where `e.IsDir()`, so plugin credentials
minted into `nats/creds/plugins/` are invisible to tenant discovery without any name matching at
all. The bootstrap writes them there, the compose mounts read them from there, and
`NonTenantCredsSuffixes` loses its only entry.

Two consequences worth stating, because both are improvements the rename paid for rather than costs:

- **A plugin credential can now be named anything a plugin id can be**, including a name that would
  otherwise collide with a tenant. The directory boundary is stronger than a suffix convention, and
  it cannot be defeated by a plugin author picking an unlucky id.
- **`NonTenantCredsSuffixes` becomes an empty exported slice, not a deleted one.** The mechanism was
  right and is worth keeping for the next family that genuinely has a shared suffix; its doc comment
  changes to record why the announcer family stopped needing it.

Its spec follows the same move: the existing BR-D40 case asserting that `-announcer` stems are
skipped is replaced by one asserting that a `plugins/` subdirectory is not descended into, and that
a `.creds` file whose stem equals a plugin id in the *top-level* directory is still read as a
tenant — the rule has to keep working in the direction that would be a security problem if it did
#### Decision 12 — the origin is stamped at announce time (BR-AS71, BR-AS72) — added 2026-09-02

Found by reading how Module Federation actually resolves a remote. `public/manifest.json` ships
`"url": "http://localhost:7111/remoteEntry.js"` **inside the image**, which is a live BR-AS15
violation: the image is tied to one deployment, and a plugin built on a laptop would announce a
laptop address to production.

This lands in Phase 14 rather than a phase of its own because 14a already opens the exact code path.
The announcer is moving into the plugin's own process, where it can read `PLUGIN_PUBLIC_ORIGIN` from
deployment configuration and stamp it into the manifest immediately before signing — the same place,
and the same one-line move, as the release counter already makes (BR-AS67). Doing it later means
opening the announce path twice.

Two shapes of URL are admissible after this, and the second is the one that matters for production:

- **An absolute origin**, stamped from configuration, checked against BR-AS45's allowlist. This is
  the lab, where five plugins sit on five ports.
- **A path with no origin at all** (`/plugins/example-plugin/remoteEntry.js`), which is same-origin
  with the shell and needs no allowlist entry. This is the likely production shape — one hostname,
  one path prefix per plugin, no cross-origin fetch and therefore no CORS header anywhere.

The refusal case is the reason BR-AS72 is a rule rather than a parser detail: a protocol-relative
`//host/path` reads as a path and resolves to a foreign host, so it would walk a plugin past the
allowlist while looking like the safe form.

Federation needs nothing for the path-prefixed case. `remoteEntry.js` already loads its chunks with
a relative `import('./assets/…')` resolved against its own URL, so every chunk follows the entry
wherever it is mounted.

#### Decision 13 — `registry` becomes `mfe-registry` in subject position 3 — added 2026-09-02

Position 3 is the service token, and the convention is the service name minus `-service`:
`refdata-service` → `refdata`, `shipping-service` → `shipping`. `mfe-registry-service` → `registry`
is the one deviation in the taxonomy, and it leaves three spellings of one thing (service
`mfe-registry-service`, package `mferegistry`, subject `registry`). After this there are two, and
they differ only because Go forbids a hyphen.

Measured before deciding: **129 occurrences across 35 files.** It rides Phase 14 because a subject
rename is a breaking wire change needing `docker compose down -v` plus a bootstrap reseed, which
14a already requires for the credential rename. Held back, it costs a second reseed.

The Go side is mechanically safe — twelve constants in `shared/mferegistry/subjects.go`, with
`TestShellReadIsUngatedAndEverythingElseIsNot` iterating `Subjects()` so a missed grant fails a test.
**The JS side is not, and that is the real work.** `lab-shell` is disciplined (three exported
constants), but `demos/01-dictionary/frontend/admin/src/api.js` inlines six raw subject strings at
their call sites with no constants and no drift test. A missed string there does not fail a build;
it fails at runtime as a request that times out. So 14a pulls those six into a constants module
mirroring `subjects.go` **before** renaming anything — a module worth having whether or not the
rename happens.

Two things deliberately not done:

- **`frontend-plugins` is not shortened.** With `mfe-registry` in position 3 the word "plugins"
  appears twice, which is normal — position 3 is the service and position 4 is the entity, and
  `refdata` reads the same way. The `frontend-plugins` / `entries` split is meaningful (the browser
  view versus the publisher records), and bundling a second rename doubles the risk for nothing.
- **The archives are not rewritten.** `Application-Shell-Microfrontend-Plan-ARCHIVE.md` is
  append-only, and `lab-shell/diagrams/phase2-*` / `phase3-*` are historical design mockups. They
  record what was true then. A blanket `sed` across the repo would corrupt the record, so the rename
  is scoped to live code, live grants, live tests and live docs.

not.
#### Derived tests — from the rules, before the code

Per `CLAUDE.md`: each rule gets a `Context` with one or more `It`s, specs land before the
implementation, and the `BUSINESS_RULES-APP-SHELL.md` edit lands in the same task as the code.

**BR-AS67 — the release sequence survives the move.** The four existing `It`s in
`cmd/announce-plugin/main_test.go` move into the new package suite with their meaning unchanged:
`N`/`N+1`/`N+2` across announce → unregister → re-announce; a crash retries the *current* announce
release rather than claiming a new availability action; lost state that reuses a spent release and
draws a `NoOp` demands explicit recovery; an exhausted or plugin-mismatched state file is refused.
Two are **new to this phase**:

- [ ] The state path is honoured as given, and there is **no** fallback to a path inside the image —
      a config with an unset release path is a startup error, not a silent write to a layer that
      vanishes with the container.
- [ ] **The CLI and the host share one state format.** A state file written by `announce-plugin`
      is read by the plugin host and continues the same sequence, and the reverse. This is the real
      risk of the extraction — a forked format would look green in both suites and lose the counter
      exactly once, in production, at the migration.

**BR-AS54 — only SIGTERM withdraws.** The three existing `It`s move unchanged: a crash or a failed
health check neither unregisters nor spends a release; SIGTERM publishes the unregister and persists
the spent release; a failed SIGTERM unregister warns and still exits. **New surface to cover**, and
the one genuinely new risk Option A introduces — the host now has a second way to die that a sidecar
never had:

- [ ] A failure in the *serving* half (an unreadable asset root, a listener that cannot bind, a
      panic in a handler) exits **without** unregistering. Serving is not availability, and a plugin
      whose disk went read-only has not been withdrawn by anybody.
- [ ] `Start` returns without unregistering when its context is cancelled for any reason other than
      SIGTERM.

**BR-AS61 — the probe contract survives nginx → Go.** New specs against the host's handler:

- [ ] `GET /healthz` answers 200 `application/json` with `Cache-Control: no-store`.
- [ ] `/healthz` carries **no** `Access-Control-Allow-Origin` header. It is server-to-server, and a
      CORS header here would invite the browser to ask a question it must read from the registry.
- [ ] `/healthz` answers even when the asset root is empty — it says this origin is still serving,
      never that the code works.

**Decision 3 — the static contract is the specification, so each clause is a spec.**

- [ ] A missing path is 404, never `index.html`. `example-plugin-unreachable`'s fixture depends on
      this; an SPA fallback turns a fetch 404 into a module parse error in the wrong state.
- [ ] An existing asset is 200 with a **named** `Access-Control-Allow-Origin` and `Vary: Origin`.
- [ ] The allowed origin is required configuration. Unset is a startup error; `*` is never produced.
- [ ] A traversal (`../`) cannot leave the asset root. nginx gave this away free and Go does not, and
      the signing seed and credential are mounted as siblings of that root.
- [ ] The mux's route set is exactly `/healthz` and the asset root — asserted as a set, so a future
      `proxy`-shaped handler fails the suite rather than passing review.

**BR-AS15 — own toolchain, own image, now over a shared base.** Per the
`app-shell-deployment-gaps` memory, a green unit suite proves nothing about a Dockerfile, so these
read the real files:

- [ ] Each migrated plugin's Dockerfile keeps its own `package.json`, its own lockfile and its own
      `npm run build`, and its final stage copies **only** `dist` into the shared base. The base
      contributes a server and a client library, never a build step and never a shell import.
- [ ] Compose shape: every announced plugin is exactly one service, and the only `-announcer`
      service remaining is `example-plugin-unreachable`'s.
- [ ] Every migrated plugin service joins both networks, and none of them gains a `proxy_pass`,
      an `extra_hosts` or a port beyond its own.

**Decision 9 — the scaffolder cannot drift from the real stanzas.**

- [ ] `scripts/new-plugin.sh` run for a fixed id is compared against a golden fixture, and the
      fixture is generated from a real migrated plugin — so a hand-edit to one of the five that the
      generator does not know about fails the suite.


**BR-D40 — the plugin credential family is excluded by location, not by spelling (decision 11b).**
Two `It`s in `shared/natstenants`, replacing the one that asserts a `-announcer` stem is skipped:

- [ ] `Discover` on a directory holding `acme.creds` and a `plugins/` subdirectory containing
      `example-plugin.creds` returns exactly one tenant, `acme` — the subdirectory is not descended
      into and its contents are not tenants.
- [ ] `Discover` still returns a tenant for a top-level `example-plugin.creds`. The exclusion is the
      directory and nothing else, so a future change that starts matching plugin ids by name is a
      failing spec rather than a silent widening of what counts as not-a-tenant.


**BR-AS71 — the origin is stamped, and it is stamped before the signature (decision 12).** Three
`It`s in the announcer package, plus one fixture assertion:

- [ ] Every fixture's checked-in `public/manifest.json` has a `remote.url` with no scheme and no
      authority. Asserted over the set, so a sixth plugin added with a baked-in origin fails here
      rather than in a deployment.
- [ ] `announcer.Start` with `PLUGIN_PUBLIC_ORIGIN` configured publishes a manifest whose
      `remote.url` carries that origin, and the plugin's own build never supplied it.
- [ ] A manifest whose origin is rewritten *after* signing fails attestation. This is what pins the
      stamp ahead of the signature rather than merely near it — without it, a passing suite would
      still allow an ingress to rewrite the origin a shell loads from.

**BR-AS72 — the three URL shapes (decision 12).** Four `It`s at the registry's admission boundary,
one in the shell:

- [ ] A path-only `remote.url` is admitted with no allowlist entry present.
- [ ] An absolute `remote.url` is still checked against BR-AS45's allowlist and refused when unlisted
      — the relative form must not have widened the absolute one.
- [ ] A protocol-relative `//host/remoteEntry.js` is refused. The one that looks safe and is not.
- [ ] A path-only entry resolves against the shell's own document origin in `federatedAdapter`, so
      the same manifest loads correctly from any hostname.

**Decision 13 — the subject rename has one list per language.** Not a business rule, so it earns
structural specs rather than a `Context`:

- [ ] The admin app's six inlined subject strings become one constants module, and a spec asserts the
      module's values against the subject list — so the next rename is one edit per language, not a
      repo-wide find-and-replace with no safety net.
- [ ] `TestShellReadIsUngatedAndEverythingElseIsNot` passes unchanged after the rename, which is the
      existing gate proving no subject shipped without a grant decision.

**The exit criterion is not a unit test.** `go run ./backend/mfe-registry-service/cmd/registry-acceptance`
must pass **unchanged**, against the running lab, with its nine steps and its four-plugin control
group. If that command has to be edited to accommodate this phase, the phase is wrong — needing to
edit it is precisely what disqualified Options B and D.

**Outcome 2026-09-02: PASSED, and the harness did need three edits.** They are recorded here rather
than waved through, because "unchanged" was the written criterion and this is a deviation from it.
Decision 10 states the test as *shape*, and none of the three touches the shape: all nine steps
survive, every assertion survives, and the four-plugin control group survives. What changed is the
deployment vocabulary the harness speaks, which is exactly what this phase set out to change.

- `subjectService` moves from `example-plugin-announcer` to `example-plugin-frontend`. 14c *requires*
  deleting that Compose service, so no version of this phase could have left the constant standing —
  the criterion and the task list cannot both be satisfied literally.
- `pathOf` and `reOrigin` are deleted, with their three unit tests. Step 6 relocated the plugin by
  rewriting the manifest's origin. Since BR-AS71 there is no origin in a manifest to rewrite, and
  `PLUGIN_PUBLIC_ORIGIN` is required with no fallback, so the rewrite was not merely redundant — the
  stamped value silently won and the step asserted against a value the publisher never sent.
- `spawn` gains an env argument, and step 6 moves the plugin by overriding `PLUGIN_PUBLIC_ORIGIN` for
  that one container. This is a **stronger** test than the one it replaces: "the requeue turns on the
  origin and nothing else" used to be three assertions about a rewrite, and is now true by
  construction because the harness no longer touches the manifest at all.

The run that passed used `-reset` (registry schema drop and re-seed). A run against a registry left
mid-sequence by an earlier failure will fail at step 1 on a stale approval — that is the flag doing
its job, not a regression.

#### Task checklist

- [x] **14a — the package.** Extract `shared/mferegistry/announcer` (own `go.mod`, added to
      `go.work` beside `shared/mferegistry/client`). Move the BR-AS67 and BR-AS54 specs first, then
      the code; `cmd/announce-plugin` becomes a wrapper and keeps a passing suite. Carries decisions
      11 and 11b: the five credentials are re-minted under their plugin ids into `nats/creds/plugins/`,
      `NonTenantCredsSuffixes` empties, `natstenants` gains its subdirectory spec, and the compose
      mounts move in the same change. Also carries decision 13: the admin constants module lands
      FIRST, then `registry` becomes `mfe-registry` across live code, grants, tests and docs — never
      the archives. Ends with `docker compose down -v` and a bootstrap reseed, one for all three
      breaking changes.
      *Shipped: `shared/mferegistry/announcer` (announcer.go, release.go, own module in `go.work`);
      `cmd/announce-plugin` is now a wrapper. Creds live in `nats/creds/plugins/` under plugin ids,
      excluded by directory (BR-D40) since `natstenants.Discover` skips `IsDir()`. Subject token
      `registry` → `mfe-registry` across live code, grants, tests and docs; admin reads it from the
      new `registrySubjects.js`. `frontend-plugins` and `rpc._platform.health.{service}.ready.v1`
      unchanged by design. Reseeded with `down -v`.*
- [x] **14a2 — the origin.** Decision 12: `PLUGIN_PUBLIC_ORIGIN` stamped before signing, the three
      URL shapes at the registry boundary, relative resolution in `federatedAdapter`, and the five
      fixture manifests stripped of their baked-in `http://localhost:711x`. Specs before code.
      *Shipped: `PLUGIN_PUBLIC_ORIGIN` is required with no manifest fallback, stamped in immediately
      before signing so the signed bytes carry it (BR-AS47). All five manifests now hold a path-only
      URL. Protocol-relative `//host/path` is refused (BR-AS72).*
- [x] **14b — the host.** `shared/mfe-plugin-host`: the decision-3 and BR-AS61 specs first, then the
      server, then `announcer.Start` alongside it. No second process.
      *Shipped: `shared/mfe-plugin-host` — one Go binary serving the asset root plus a bounded
      `/healthz` with no CORS header (BR-AS61), a named `Access-Control-Allow-Origin` (never `*`) on
      assets, `try_files`-equivalent 404 with no SPA fallback, and a route set asserted as a set so
      no proxy-shaped route can be added. Announcer runs in-process; SIGTERM is still the only thing
      that withdraws (BR-AS54).*
- [x] **14c — migrate four fixtures.** `example-plugin`, `-slow`, `-activate-throws`,
      `-incompatible` onto the base image; delete their four announcer stanzas; move
      `stop_grace_period` and the release volume onto the plugin service; add `backend`.
      `example-plugin-unreachable` keeps the CLI form and its own stanza.
      *Shipped: ten containers became five. `example-plugin-unreachable` keeps its CLI announcer, and
      `demo-catalog` was left alone — it is curated (`mfe.source: preload`), not announced.*
- [x] **14d — the scaffolder.** `scripts/new-plugin.sh` plus its golden-fixture spec, and the
      bootstrap/origins/README chores it generates.
      *Shipped: `scripts/new-plugin.sh` + `scripts/templates/plugin-compose.yml.tpl`, pinned by a
      golden fixture in `shared/mfe-plugin-host/deployment_test.go`. `lab-shell/plugins/README.md`
      documents the flow.*
- [x] **14e — rules and docs.** The one-line BR-AS67 amendment (the counter volume now attaches to
      the plugin container), the credential-naming table row (decision 11), and the subject-token row
      for `mfe-registry` (decision 13). BR-AS71 and BR-AS72 are already written; 14e checks their
      test matrix against what shipped.
      `ARCHITECTURE-APP-SHELL.md` gains the Phase 14 as-built section, the before/after diagram, and
      loses the Phase 13 claims the migration invalidates.
      *Shipped: BR-AS67 amended, credential-naming row added, `ARCHITECTURE-COMMUNICATIONS.md` gained
      the `{service}` = service-name-minus-`-service` rule (decision 13), `ARCHITECTURE-APP-SHELL.md`
      gained the BR-AS71/72 origin section plus an as-built subsection, and
      `diagrams/mfe-announcer-topology.html` → `images/mfe-announcer-topology.png` is the before/after.
      Phase 2 and Phase 3 diagrams left untouched as historical record.*
- [x] **14f — the gate.** `cmd/registry-acceptance` unchanged and green against the running lab.
      *PASSED 2026-09-02 with `-reset`: all nine steps, four-plugin control group intact. The harness
      needed three edits; they are recorded and justified in "Outcome 2026-09-02" above.*

---

### Phase 15 — PROPOSED (design gate OPEN, opened 2026-09-02) — Frontend health over NATS, and a catalogue-reset notice

**Status: PROPOSED. No tasks, no tests, no code until the design decisions below are approved.**
Direction agreed 2026-09-02 ("let's move health to NATS"); the decisions themselves are not yet
approved. Both halves land in one phase by the user's call on 2026-09-02: they make a plugin
*subscribe* to something for the first time, and that widening is one decision, not two.

#### The two problems this phase closes

**One.** The registry probes a plugin's frontend with `GET /healthz` over the `frontend` Docker
network. That is the only reason a Phase 14 plugin container joins `frontend` at all — the browser
arrives on a published host port, not over that network. It is also a per-plugin deployment chore:
`REGISTRY_HEALTH_ORIGINS` is a hand-maintained map, exactly the kind of thing the 14d scaffolder
exists to stop generating.

**Two.** A plugin announces once, at start-up. If the registry loses its catalogue while the plugins
keep running, nothing re-announces. Restarting the containers heals it; nothing else does. That hole
is real and small, and it has no rule today.

**Correcting the record.** An earlier note in this conversation said moving health to NATS deletes
decision 6's named cost. It does not. Decision 6's cost is joining `backend`, and the plugin still
needs NATS to announce. What this phase removes is the *second* network, `frontend`. The larger wins
are the deleted chore and the point below about same-origin.

#### Proposed design decisions — NOT YET APPROVED

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
   cannot probe arbitrary targets. Today that is `REGISTRY_HEALTH_ORIGINS`. After this the subject
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

10. **A resync announce spends a release number (BR-AS67), and a converged registry should still
    cost zero writes.** Needs checking against BR-AS68's convergence principle: a re-announce with
    identical content and a higher release ought to be a no-op write, not a revision bump and an
    audit row per plugin per reset.

#### Proposed rules — one rewrite, one addition

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

#### Open before the gate closes

- **Nested deadlines.** The outer NATS request deadline and the inner local-GET deadline. The inner
  must expire first, or a slow listener returns a NATS timeout that looks like a dead process.
- **Does the health worker still need its own worker?** BR-AS61's current shape runs probes on a
  separate worker joined per pass, so a slow probe never delays a catalogue read. Request/reply over
  NATS may or may not keep that shape.
- **Whether the reset notice should also fire on a registry *restart***, or only on an actual
  catalogue reset. Firing on every restart is simpler and self-healing; it also means a rolling
  restart triggers a full re-announce storm, which decision 7's jitter would have to absorb.

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
