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
      subject, own hint and own reply shape — no revision, no entries, no signed bytes. Frontend and
      backend stay two signals. 5s interval, 2s timeout, 2 failures, 15s freshness, with ageing at
      READ time on both sides so there is no timer to leak. `shared/natsready` added, because
      presence is not readiness.
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

### Phase 13 — APPROVED 2026-09-01 — Announce the `example-plugin*` Fixtures; Leave Only `demo-catalog` Preloaded

> **Status: APPROVED 2026-09-01** — design gate passed at revision 3, after a
> failed first review and four business rules confirmed by the user.
> Implementation may start. Requested 2026-09-01.
> Follows Phase 7 (publisher signing and the trust table) and Phase 8
> (announcement, preload seeding and the pending tier), both archived
> 2026-09-01 — this phase builds nothing new in the registry's rules, it makes
> the ones already built runnable.
>
> **Revision 2 (2026-09-01)** after a design review found four P1 and four P2
> defects in revision 1. All eight were verified against the code and all eight
> were accepted. What changed: the release counter is now an owned, persisted
> concern (D3); trust seeding is convergent and boot-ordered (D6); signing
> keypairs and NATS credentials are separated (D8); the shared package is split
> so `mferegistry` keeps its dependency-free promise (D1); the drift question
> was withdrawn on a false premise and replaced with manifest provenance (D11);
> the goal's claims were narrowed and an acceptance sub-phase added (13f).
>
> **Revision 3 (2026-09-01)** — the one open fork is resolved: the user chose
> the **full lifecycle**, so Phase 13 carries signed unregister and a live
> withdrawal-then-return, and the announcer becomes a resident process rather
> than a one-shot (D5). See "Scope: full publisher lifecycle".

#### Goal

Move the five `example-plugin*` fixtures off the preload tier and onto the
announcement tier, leaving `demo-catalog` as the only preloaded plugin.

The point is not the fixtures. Phase 8 built the announcement path and Phase 7
built the trust table, but **nothing in the running stack has ever announced,
and no credential can**: `bootstrap-operator.sh:502` grants
`mfe-registry-service` *subscribe* on the announce subject and grants nobody
*publish*. So the whole tier exists only inside Ginkgo specs.

**What first boot alone proves:** `AnnounceInserted` and `AnnouncePending`, and
that an operator's enable puts an announced plugin on screen. **What it does
not prove, and what 13f must drive deliberately:** `AnnounceUpdated`,
`AnnounceRequeued`, key rotation, key revocation and recovery, publisher
withdrawal, and unchanged return afterwards.

#### What this is not

**Not a conversion.** `SOURCE` is derived from the first accepted write's actor
and is deliberately not stored or editable (Phase 8 decision 80, BR-AS43). A
preloaded id cannot become an announced one; the ids are re-created from an
empty store.

#### Scope: full publisher lifecycle (resolved 2026-09-01)

**Phase 13 demonstrates publisher withdrawal, not just operator disable.**
Revision 1 was incoherent here — it moved the unregister DTOs and granted the
unregister subject, but planned only `Sign`, `Announce` and a one-shot
`cmd/announce-plugin`, which exits immediately and can never unregister
anything. Resolved in favour of the full lifecycle, because the stated payoff
of moving these fixtures to `dynamic` (D10) is a live unload, and narrowing to
announce-only would prove little beyond what Phase 8's specs already prove.

This adds to the phase:

- a signed unregister command builder and an `Unregister` call in
  `shared/mferegistry/client` (13a);
- `.unregister.v1` in each announcer's NATS grant (D8);
- a resident announcer rather than a one-shot (D5);
- a live withdrawal-then-return scenario in 13f, which is the release
  sequencing in D3 actually being exercised: `N` announce, `N+1` unregister,
  `N+2` re-announce.

**One rule is preserved and must be stated in the specs:** a crash,
a failed health check, or a container disappearing **does not** authorize
withdrawal. Unregister is an explicit signed action, associated with controlled
shutdown, never with failure detection.

#### Design decisions

1. **The client half moves to a new `shared/mferegistry/client` subpackage —
   NOT into `shared/mferegistry` itself.** `subjects.go:17` states the base
   package's promise in as many words: *"Deliberately dependency-free: a
   contract, not a client. Nothing here dials, encodes or knows what a registry
   entry looks like."* Revision 1 would have broken that for every existing
   importer by adding `nkeys` and `nats.go` to it. Split instead:

   - `shared/mferegistry` — subjects, plus the dependency-free wire DTOs and
     the five outcome constants. Still dials nothing.
   - `shared/mferegistry/client` — NKey signing and NATS request/reply.

   `internal/servicerpc` type-aliases the shared DTOs rather than declaring its
   own, so server and client cannot drift.

   There is no duplication to unwind today — there is no client at all — but
   the wire types sit behind `internal/`, so the first publisher outside this
   module would hand-redeclare the request struct, the response struct and the
   outcome strings with no compiler link to the server.
2. **The admission decisions do not move.** The eight gates, `DecideAnnounce`,
   `DecideUnregister` and `NKeyVerifier` stay in the registry's domain. A
   verifier in a shared package invites a caller to "pre-check" client-side and
   then trust the answer, which is the precise failure the gates exist to
   prevent.
3. **The release counter is owned, persisted, and never derived from a
   clock.** Revision 1 assumed a restart could safely re-announce the same
   release as a harmless `NoOp`. That is true **only** for retrying the
   currently accepted announcement. Announce and unregister share one
   monotonic sequence per plugin: `AdmitUnregister` (`unregister.go:154`)
   refuses an equal release unless the entry is already withdrawn, so a
   withdrawal must spend `N+1`; re-announcing `N+1` afterwards then hits
   `NoOp` at `verify.go:177` and **leaves the plugin withdrawn**. Phase 13
   must decide and write down:

   - who owns each plugin's counter and where it persists;
   - how announce → unregister → unchanged re-announce obtains `N`, `N+1`,
     `N+2`;
   - whether an ordinary container restart is a retry of the current release
     or a new availability action;
   - how a publisher recovers after losing its local release state.
4. **Per-plugin announcer sidecar, not one shared announcer.** Each fixture
   gets its own announcer, publisher id and signing key. A single announcer
   speaking for five plugins would be one publisher wearing five names, and the
   two behaviours worth testing — rotation and revocation — are per-publisher:
   revoking one key must withhold exactly one plugin (BR-AS38). A shared key
   cannot show that.

   **Note a fixture asymmetry to resolve in 13e:** `example-plugin-unreachable`
   has **no plugin package and no container** — it exists only as a
   `registry.json` row pointing at a dead path on port 7111. It has nothing to
   sidecar. Either it gets an announcer-only container, or it is announced by
   the `example-plugin` sidecar, which contradicts decision 4's one-publisher-
   per-plugin premise.
5. **The announcer is a resident process, not a one-shot.** It announces at
   start, stays connected for the container's life, and on `SIGTERM` publishes
   a signed unregister before exiting — the controlled-shutdown case BR-AS54
   describes. It must:

   - hold its release counter in a mounted writable volume, so `N` survives a
     restart and `N+1`/`N+2` are reachable (D3);
   - treat a failed unregister on shutdown as a logged warning, never a hang —
     the container still exits, and the entry is then reconciled on next start;
   - never unregister on a health-check failure or a crash. Only `SIGTERM`.

   Compose must give it a real `stop_grace_period` so the unregister round trip
   completes before the kill.
6. **Trust seeding is convergent and boot-ordered, not a blind write.** "Write
   each publisher with one enabled key" is not one operation. The API has four
   separate revision-checked ops (`publisher.go:45–48`):
   `publisher-upsert`, `publisher-add-key`, `publisher-set-key-state`,
   `publisher-transfer` for plugin ownership. Each needs the latest trust-table
   revision, and even a no-op write consumes a revision and an audit row. So
   the seeder must **read, compare, and apply only what is missing**, and must
   preserve operator decisions already made — retired and revoked keys,
   and ownership transfers. Boot order is explicit and enforced:

   ```
   NATS + accounts + registry ready
       -> trust seed completes successfully
           -> announcer jobs start
   ```

   Without that ordering the one-shot announcers race the seed, get
   `not-owned` or `key-not-trusted`, and exit.
7. **The seeded trust rows are curated writes, not a new tier.** The seeder
   uses the existing `api._platform.registry.publishers.write.v1` under the
   shared operator identity — the same path the Registry Publishers panel uses.
   No new write path, no boot-time bypass of the revision check or audit trail
   (Phase 2 decision 75).
8. **Signing keypairs and NATS credentials are different things, and revision
   1 conflated them.** Separate the two axes:

   - **Five publisher signing keypairs**, minted outside the nsc trust chain
     (Phase 7 gate answer 2), so a leaked signing key cannot connect to NATS
     as anything.
   - **Five NATS transport credentials**, one per announcer. If per-publisher
     isolation and connection attribution are the reason for five sidecars
     (decision 4), one shared transport credential contradicts it — five
     holder-named credentials are the coherent choice, and CLAUDE.md's
     credential-naming rule already says a dedicated credential is named for
     its holder.

   Each grant covers exactly
   `rpc._platform.registry.entries.announce.v1` (plus `.unregister.v1` if the
   open fork resolves to full lifecycle), named in full, never as
   `rpc._platform.registry.entries.>` — the same reasoning the registry's own
   grant records at `bootstrap-operator.sh:491`.

   Each container mounts only its own signing seed and its own creds,
   read-only, at runtime. **Signing seeds must not enter image layers.**

   **Adding NATS users means `down -v` is not enough.** The sequence is:

   ```
   ./nats/bootstrap-operator.sh --force
   docker compose down -v
   docker compose up --build
   ```

   `down -v` alone does not regenerate the PLATFORM JWT or the creds files.
9. **Announced entries stay disabled on first boot.** BR-AS39 is not relaxed
   and nothing auto-enables. **Scoped correctly this time:** this is a property
   of a *fresh registry database*, not of every boot. Once an operator enables
   the fixtures, Postgres keeps that decision across ordinary Compose restarts,
   and equal-release re-announcements are no-ops that do not disable them
   again. The acceptance rule is therefore:

   > On first boot against an empty registry database, only `demo-catalog` is
   > served, until an operator enables the five announced candidates.

   The lab-shell intro copy must say so, or a first run reads as broken.
10. **The five fixtures become `dynamic`, and that is the payoff.** Announce
   forces `LifecycleDynamic` (`announce.go:70`). Withdrawal for those five goes
   from a reload offer to a live unload — the sequence BR-AS52/AS54 describe,
   which Phase 5 could only test against two constructed documents.
   `demo-catalog` staying `static` keeps both classes represented live.
11. **The announced bytes come from a build-owned artifact.** Revision 1 asked
    whether drift would still pass. **That question was withdrawn: it had a
    false premise.** Drift checking is preload-only by design —
    `FetchOrigins.Target` returns `not-preloaded` for any other source
    (`drift.go:156`), and the Admin UI shows drift only for preload rows. So
    announced fixtures will neither stay `checked` nor go orange; the MANIFEST
    column simply stops applying to them, and `demo-catalog` becomes the only
    row it covers.

    A manifest provenance decision is still needed, for a different reason: the
    plugin's *content* must come from a build-owned artifact, not a
    hand-maintained copy. Four fixtures already have
    `lab-shell/plugins/<id>/public/manifest.json`; `example-plugin-unreachable`
    has none (see decision 4).

    **Amended 2026-09-01 after 13b was built.** Revision 2 said every manifest
    needed an explicit positive `release` baked in at build time, since gate 5
    refuses `release <= 0`. That is wrong, and 13b's implementation is right:
    **the announcer injects the release into the manifest at runtime, just
    before signing** (`cmd/announce-plugin/main.go`, `manifestAtRelease`). The
    release is the *publisher's* state, not the build's (BR-AS67) — baking it
    into an image would mean rebuilding a container every time a plugin
    withdraws and returns, and would put one process's counter in an artifact
    several things read. So:

    - the build-owned `manifest.json` carries the plugin's content and **no**
      `release` field;
    - the announcer re-encodes it with the release it owns, and signs that;
    - BR-AS37 is preserved because it signs and sends the same bytes — the
      re-encode happens before signing, never between signing and publishing.

    13e therefore does **not** add `release` fields to the fixture manifests.
12. **`REGISTRY_HEALTH_TARGETS` and the origin maps stay configuration.**
    Keyed by plugin id, deployment-owned regardless of tier (BR-AS61/AS62).
    Nothing here moves them into the manifest.

#### Business rules — CONFIRMED 2026-09-01

All four candidates were put to the user and confirmed as recommended. Written
into `demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md` § "Phase 13 — confirmed
requirements, not yet implemented". Both new rules need executable coverage
during implementation; neither is complete without it.

- **BR-AS66 — A fresh lab serves only its preloaded plugin.** First boot against
  an empty registry database serves `demo-catalog` alone; announced candidates
  wait for an operator. A first-boot property only — an enable survives
  restarts, and an equal-release re-announcement must not undo it. The intro
  copy must say so.
- **BR-AS67 — A publisher owns its release counter, and it only goes up.** One
  sequence per plugin, shared by announce and unregister, persisted by the
  publisher. Withdraw-then-return spends `N` / `N+1` / `N+2`. This is D3 and D5
  stated as a rule.
- **Clarification (BR-AS38) — a seeded key has no state of its own.**
  `KeyStates()` stays `enabled` / `retired` / `revoked`. A bootstrap-seeded key
  is an ordinary `enabled` key; its provenance is read from the audit trail,
  the way an entry's `source` is. No fourth state.
- **Clarification (BR-AS38) — key revocation is a documented demo step.**
  Revoking one fixture's key withholds exactly that publisher's plugins.
  Recovery is **not** `down -v`: re-enabling the key restores nothing until
  each withheld entry is enabled individually
  (`registry/revocation_test.go`). 13f drives this and 13g documents it.

#### Sub-phases (shape only — not tasks until approved)

- [x] **13a** (built 2026-09-01) — `shared/mferegistry` wire DTOs and outcome constants
      (dependency-free), plus `shared/mferegistry/client` for signing and
      request/reply — `Announce` and `Unregister`, each with its own signed
      command builder. `internal/servicerpc` aliases them. Specs assert the
      bytes signed are the bytes sent (BR-AS37).
- [x] **13b** (built 2026-09-01) — a resident `cmd/announce-plugin` on 13a: announce at start,
      signed unregister on `SIGTERM`, release-counter persistence and recovery
      per decisions 3 and 5.
- [x] **13c** (built 2026-09-01) — bootstrap: five publisher signing keypairs and five holder-named
      NATS credentials granting `announce.v1` and `unregister.v1` by full
      subject; `--force` reseed path documented. The reseed exposed a latent
      gap rather than causing one: `shared/natstenants.Discover` treats every
      unlisted `.creds` stem in the shared directory as a tenant, so the five
      new `*-announcer.creds` files became five bogus tenant connections in
      every `natstenants`-based service (BR-D40's documented failure mode,
      arriving five at a time). Closed by `NonTenantCredsSuffixes`
      (`-announcer`), with a spec, rather than by five more map entries.
- [ ] **13d** — convergent, boot-ordered trust seeding through the existing
      curated write path.
- [ ] **13e** — five announcer sidecars, the `example-plugin-unreachable`
      asymmetry resolved, a writable release-state volume and a real
      `stop_grace_period` per sidecar; `registry.json` reduced to
      `demo-catalog`. No `release` field is added to any manifest (D11).
- [ ] **13f** — a Compose-level acceptance sequence driving `AnnounceUpdated`,
      `AnnounceRequeued`, rotation, revocation and recovery, plus a live
      withdrawal (`docker compose stop`) and unchanged return (`start`) showing
      releases `N` / `N+1` / `N+2`. Without this the goal's claims are not
      evidenced.
- [ ] **13g** — intro copy, `BUSINESS_RULES-APP-SHELL.md` and
      `ARCHITECTURE-APP-SHELL.md`, in the same commits as the code.

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
