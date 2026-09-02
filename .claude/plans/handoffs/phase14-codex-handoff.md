# Codex handoff — implement Phase 14 of the app-shell plan

Repo: `/Users/jeremy/dev/github/jthomas78/nats-tech-lab`
Branch: `poc/dictionary2.2`

You are implementing **Phase 14** end to end. The design gate is already passed —
do not re-open it, do not propose alternatives, do not redesign. Options B, C and
D were compared and rejected in a recorded review. Your job is to build what is
already decided.

---

## 1. Read these first, in this order

1. `CLAUDE.md` — repo-wide rules. The **Quality Rules** and **AI Agent Workflow**
   sections are binding on you.
2. `.claude/plans/Application-Shell-Microfrontend-Plan.md`, the section
   `### Phase 14 — APPROVED (design gate passed 2026-09-02)`. This is your
   specification: 13 design decisions, a derived-test list, and a 6-task
   checklist (14a, 14a2, 14b, 14c, 14d, 14e, 14f). **Follow the checklist in
   order.**
3. `.claude/plans/reviews/adr-announcer-topology-20260902.md` — why Option A won.
   Read it so you do not re-litigate it.
4. `demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md` — search for BR-AS15,
   BR-AS45, BR-AS47, BR-AS54, BR-AS61, BR-AS67, BR-AS71, BR-AS72. Do not read
   the whole 1484-line file; grep for the rule ids.
5. `.claude/memory/MEMORY.md` — an index of one-line hooks. Open an individual
   memory file only when its hook looks relevant. `app-shell-deployment-gaps` is
   relevant to task 14c.
6. `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-APP-SHELL.md`,
   the section `## Phase 14 — one container per plugin, and the two links it
   holds`. It already contains the approved narrative and the BR-AS71/BR-AS72
   subsection. Task 14e extends it; it does not rewrite it.

## 2. What Phase 14 is, in one paragraph

Today every announced MFE plugin ships **two** containers: an nginx frontend on
the `frontend` Docker network holding zero NATS connections, and an `-announcer`
sidecar on the `backend` network holding exactly one NATS connection and no
listener. They never talk to each other. Phase 14 merges them into one container
running a small Go static host as PID 1, which serves the assets **and** calls
the announce client in-process. The per-plugin totals do not change — still one
HTTP listener, still one NATS connection, still one credential, still one signing
key, still one release counter. Only the container count and the network
membership change.

Three breaking changes ride along, because all three need the same
`docker compose down -v` + bootstrap reseed and doing them separately costs three
reseeds:

- **Credential rename** (decision 11): `example-plugin-announcer` →
  `example-plugin`, for all five.
- **Tenant-discovery fix** (decision 11b): plugin creds move to
  `nats/creds/plugins/` and are excluded from tenant discovery by directory
  rather than by name suffix.
- **Subject rename** (decision 13): `_platform.registry.*` →
  `_platform.mfe-registry.*`.

Plus one non-breaking rule addition:

- **Origin stamping** (decision 12, BR-AS71 / BR-AS72): the plugin's public
  origin leaves the image and is stamped into the manifest at announce time,
  immediately before signing.

## 3. Method — this repo's workflow is not optional

- **Specs before code, always.** Each business rule gets a Ginkgo `Context` with
  one or more `It`s. The plan's "Derived tests" section already enumerates them —
  write those specs first, watch them fail, then implement. Do not write the
  implementation first and back-fill tests.
- **Business rules live in the domain layer**, never in handlers or application
  services. Read the module you are in; the path differs per service.
- **Ginkgo is the runner**: `ginkgo ./...` from the service you changed.
  `go test ./...` is the fallback. **Beware:** Postgres-backed specs SKIP silently
  without their `*_TEST_DATABASE_URL` env var and `go test` still prints `ok` —
  green is not proof they ran. Say so in your report if you could not run them.
- **Rules and code land in the same task.** When you add or change a rule, update
  `demos/01-dictionary/BUSINESS_RULES-APP-SHELL.md` in the same change.
- **Tick the checklist.** Mark each `- [ ]` in the plan's Phase 14 task checklist
  as `- [x]` as you complete it, with a one-line note of what actually shipped —
  match the style of the completed Phase 13 entries directly above it.

## 4. Hard constraints — violating any of these fails the phase

**Do not touch:**
- `Application-Shell-Microfrontend-Plan-ARCHIVE.md` — append-only, never edit
  existing content.
- `lab-shell/diagrams/phase2-*` and `phase3-*` — historical design mockups. They
  record what was true then. A repo-wide `sed` for the subject rename would
  corrupt the record.
- `demo-catalog`. **This is the trap.** `lab-shell/plugins/demo-catalog/` and the
  `demo-catalog-frontend` compose service exist and look exactly like the other
  five, but it has **no announcer and no credential** — it is a *curated* plugin,
  not an announced one. It is not migrated, not scaffolded, not given a
  credential, and not given the `backend` network. Exactly five plugins are
  announced: `example-plugin`, `example-plugin-slow`,
  `example-plugin-activate-throws`, `example-plugin-incompatible`,
  `example-plugin-unreachable`.
- `example-plugin-unreachable`'s form. It has no web server *by design* — its
  fixture is a genuine 404. It **keeps the CLI announcer container**. Four
  plugins migrate to the host image, not five.

**Subject rename — scope it precisely:**
- Only position 3 changes, and only for the twelve constants in
  `shared/mferegistry/subjects.go`. `registry` → `mfe-registry`.
- **`rpc._platform.health.{service}.ready.v1` does NOT change.** Its position-3
  token is `health` — a platform-wide readiness family no single module owns —
  and its `{service}` sits in position 4. It is in the same file. Leave it alone.
- **`frontend-plugins` is NOT shortened.** It is position 4, the entity. Position
  3 is the service and position 4 is the entity; `refdata` reads the same way.
- Scope is live code, live grants, live tests and live docs. ~129 occurrences
  across ~35 files.
- **The JS side is the real risk.**
  `demos/01-dictionary/frontend/admin/src/api.js` inlines six raw subject strings
  at their call sites with no constants and no drift test. A missed string there
  does not fail a build — it fails at runtime as a request that times out.
  **Pull those six into a constants module mirroring `subjects.go` BEFORE
  renaming anything**, with a spec asserting the module's values. `lab-shell` is
  already disciplined (three exported constants).
- `shared/mferegistry`'s `TestShellReadIsUngatedAndEverythingElseIsNot` iterates
  `Subjects()`, so a missed grant fails a test. It must pass unchanged after the
  rename.

**Security constraints that survive this phase unchanged:**
- The Ed25519 signing seed is minted **outside** the nsc trust chain, mounted
  read-only at runtime, and **never enters an image layer**. `publishers.json`
  holds public keys only.
- `/healthz` deliberately carries **no** `Access-Control-Allow-Origin` header.
  BR-AS61 is server-to-server; a CORS header there invites the browser to ask a
  question it must read from the registry.
- Asset responses carry a **named** `Access-Control-Allow-Origin` plus
  `Vary: Origin`. **Never `*`.** An unset allowed-origin is a startup error.
- **No `proxy_pass`-shaped route of any kind.** The mux's route set is exactly
  `/healthz` plus the asset root, asserted as a set so a future proxy handler
  fails the suite rather than passing review. This is what makes joining the
  `backend` network acceptable.
- `try_files $uri =404` semantics with **no SPA fallback**. An `index.html`
  fallback turns `example-plugin-unreachable`'s intended fetch 404 into a module
  parse error in the wrong state.
- Path traversal (`../`) cannot leave the asset root. nginx gave this away free;
  Go does not. The signing seed and credential are mounted as siblings of that
  root.
- BR-AS54 is untouched: withdrawal happens on **SIGTERM only** — never on a
  crash, never on a failed health probe, never on silence. Note the *new* risk
  Option A introduces: the host now has a second way to die that a sidecar never
  had. A failure in the **serving** half (unreadable asset root, listener cannot
  bind, panic in a handler) must exit **without** unregistering. Serving is not
  availability.
- `nats.Name()` must equal the credential name, per the repo's credential-naming
  rule. After the rename that is the plugin id.

**Naming conventions:** streams are `SCREAMING_SNAKE`; KV buckets and Object
Stores are `lowercase-kebab`; credentials are `lowercase-kebab` and double as
`.creds` filenames.

## 5. The tasks

Work them in checklist order. Each is defined in full in the plan — this is a
summary so you can see the shape, not a replacement for reading it.

- **14a — the package.** Extract `cmd/announce-plugin`'s internals into
  `shared/mferegistry/announcer` behind a single `Start(ctx, Config)` owning
  connect, announce, the release counter and the SIGTERM unregister. Own
  `go.mod`, added to `go.work` beside `shared/mferegistry/client`. Move the
  BR-AS67 and BR-AS54 specs first, then the code. `cmd/announce-plugin` stays
  shipped as a thin wrapper with a passing `main_test.go`. Also carries decisions
  11, 11b and 13 (see §4).
  - Two BR-AS67 specs are **new**: an unset release-state path is a startup
    error, never a silent write to a layer that vanishes with the container; and
    the CLI and the host share **one** state format, verified in both directions.
    That second one is the real risk of the extraction — a forked format looks
    green in both suites and loses the counter exactly once, in production, at
    the migration.
- **14a2 — the origin.** BR-AS71 / BR-AS72. `PLUGIN_PUBLIC_ORIGIN` read from
  deployment config and stamped into the manifest **immediately before signing**
  — the same point as the release counter. The signature is what forces the
  ordering: BR-AS47 puts the URL inside the signed bytes, so a spec must prove a
  manifest rewritten *after* signing fails attestation. Three URL shapes at the
  registry's admission boundary: path-only admitted with no allowlist entry;
  absolute still checked against BR-AS45; protocol-relative `//host/path`
  **refused** (it reads as a path and resolves to a foreign host). Plus relative
  resolution in `lab-shell/src/shell/loader/federatedAdapter.js`, and strip the
  baked-in `http://localhost:711x` from all five fixture `public/manifest.json`
  files.
- **14b — the host.** `shared/mfe-plugin-host`. Decision-3 and BR-AS61 specs
  first, then the server, then `announcer.Start` alongside it. One process. Treat
  `lab-shell/plugins/example-plugin/nginx.conf` as the **specification** to port,
  not a starting point — each of its four behaviours gets its own test.
- **14c — migrate four fixtures.** `example-plugin`, `-slow`,
  `-activate-throws`, `-incompatible` onto the base image. Delete their four
  announcer stanzas. Move `stop_grace_period: 30s` and the release volume onto
  the plugin service. Add the `backend` network. Per the
  `app-shell-deployment-gaps` memory, a green unit suite proves nothing about a
  Dockerfile — the specs here must read the real files and assert each plugin
  keeps its own `package.json`, its own lockfile and its own `npm run build`,
  with the final stage copying **only** `dist` into the shared base.
- **14d — the scaffolder.** `scripts/new-plugin.sh` (the `scripts/` directory
  does not exist yet — create it). It generates the plugin directory, the single
  Compose stanza, the `demos/01-dictionary/nats/bootstrap-operator.sh` loop
  entry, the `REGISTRY_HEALTH_ORIGINS` / `REGISTRY_ALLOWED_ORIGINS` mappings and
  the README port-table row. Its spec compares a run for a fixed id against a
  golden fixture **generated from a real migrated plugin**, so a hand-edit the
  generator does not know about fails the suite. This is the larger half of the
  DX win — shipping the container change without it leaves the burden mostly
  intact.
- **14e — rules and docs.** The one-line BR-AS67 amendment (the counter volume
  now attaches to the plugin container); the credential-naming table row in
  `ARCHITECTURE-ACCOUNTS.md` § "Credential naming"; the subject-token row for
  `mfe-registry`. BR-AS71 and BR-AS72 are **already written** — 14e only checks
  their test matrix against what shipped and flips the "Not yet built" note in
  `BUSINESS_RULES-APP-SHELL.md` § "How Phase 14's rules are checked" to name the
  real spec files. `ARCHITECTURE-APP-SHELL.md` gains the Phase 14 as-built
  section and loses the Phase 13 claims the migration invalidates.
- **14f — the gate.** See §6.

## 6. The exit criterion

`go run ./backend/mfe-registry-service/cmd/registry-acceptance` must pass
**unchanged** against the running lab — its nine steps, its `compose stop`/`start`
of `example-plugin`, its withdrawn → returned at releases `N`/`N+1`/`N+2`, and its
four-plugin control group.

**If that command has to be edited to accommodate this phase, the phase is
wrong.** Needing to edit it is precisely what disqualified Options B and D. Do
not edit it to make it pass. If it fails, the implementation is wrong — fix the
implementation, or stop and report.

Getting there requires, from `demos/01-dictionary/`:

```
docker compose down -v
./nats/bootstrap-operator.sh      # re-mints creds under nats/creds/plugins/
docker compose up --build
go run ./backend/mfe-registry-service/cmd/registry-acceptance
```

`down -v` is authorized for this task and is the lab's normal path for a breaking
wire change — the volumes hold seed data that the bootstrap recreates, and the
release counters are re-created on a fresh boot by BR-AS66. Run it once, at the
end of 14a's breaking changes, not repeatedly.

**If your sandbox cannot run Docker**, do not fake it and do not claim the gate
passed. Complete 14a through 14e, run every unit and spec suite you can, and
report clearly which of the Docker-dependent steps you could not execute, with
the exact commands the user must run.

## 7. Test commands

```
ginkgo ./...                                                    # from the changed Go module
go test ./...                                                   # no-install fallback
cd lab-shell && npm test                                        # vitest
cd demos/01-dictionary/frontend/admin && npm test               # vitest
```

The backend is seven Go modules. Run the suites for everything you touched —
at minimum `mfe-registry-service`, `shared/mferegistry`, `shared/mferegistry/announcer`,
`shared/mfe-plugin-host` and `shared/natstenants`.

## 8. Report back with

1. Which checklist items are done, and which are not, honestly. A partial phase
   reported accurately is worth more than a complete one reported optimistically.
2. Every test suite you ran and its real result. Name any suite that skipped
   silently for a missing env var — do not count a skip as a pass.
3. Whether `registry-acceptance` passed unchanged, or the exact reason you could
   not run it.
4. Anything in the plan you found to be wrong, impossible, or ambiguous. Do not
   silently work around a defect in the specification — name it. If a design
   decision turns out to be unimplementable as written, **stop and say so**
   rather than substituting your own design.
5. The full list of files you changed.
