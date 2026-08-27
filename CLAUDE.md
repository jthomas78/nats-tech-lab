# CLAUDE.md

This file is the canonical guidance for all AI coding agents working with code
in this repository. Agent-specific entry files should reference this file rather
than duplicate its contents.

## Session Memory

At the start of every session, read `.claude/memory/MEMORY.md` — the index of one-line hooks — and keep it as persistent context. **Do not read every file under `.claude/memory/` unconditionally**: each is 30–75 lines and reading all of them every session is pure token overhead when a task only ever touches a few. Instead, open an individual memory file only once `MEMORY.md`'s hook line suggests it's relevant to the task at hand (same "read on relevance" policy as the global auto-memory system). When saving new memories during a session, write them to `.claude/memory/` (not `~/.claude/projects/`) so they are shared across devices via git, and keep `MEMORY.md`'s hook line for that file short enough to judge relevance without opening it.

## General preferences

- If asked to do too much work at once, stop and state that clearly.
- If computer use is helpful for completing or verifying work, shell out to GPT-5.5 with Codex for it (the `codex:rescue` skill / `codex:codex-rescue` agent).
- **Don't read large docs whole by default.** Before calling `Read` with no `offset`/`limit` on a file over ~150 lines, prefer `grep`/`Grep` for the section you need, or a targeted `Read` with `offset`/`limit`. Full reads are fine when you genuinely need the whole file (e.g. first pass on a new doc, or it's already short); the point is to default to targeted reads for the large reference docs in this repo (`BUSINESS_RULES-*.md`, `PERFORMANCE.md`, files under `.claude/plans/`, and the `ARCHITECTURE*.md` files under `obsidian/V3-Platform/Architecture/Dictionary-POC/` — see "Architecture Docs" below), not to read them cover-to-cover out of habit.

## Purpose

A lab for evaluating NATS.io patterns relevant to a V3 greenfield logistics platform. Each demo is self-contained: the user picks a demo from the lab shell, reads an intro, launches it via Docker, and tears it down when done.

The core architectural question: **what is the correct responsibility split between JetStream (event backbone), NATS KV (fast lookup/watch/cache), Postgres (transactional source of truth), and CQRS projections?**

## Repository Layout

Read the tree with `ls` rather than from here — it changes faster than this
file does. The parts that aren't visible from the tree:

- Each demo has its own `docker-compose.yml` and does **not** share a network
  with the lab shell or other demos.
- A demo's top-level `README.md` is the **intro text rendered in the lab
  shell**, not just developer docs — edit it with that audience in mind.
- `lab-shell/` is the demo menu and intro pages; the per-demo UIs live under
  `demos/<demo>/frontend/`.
- The two obsidian vaults have different jobs — see "Obsidian Vault" and
  "Architecture Docs" below.

## Docker Host Port Allocation

Host ports for demo `docker-compose.yml` files are assigned from two fixed 4-digit ranges, so ports stay predictable across demos and don't need per-demo negotiation:

- **7100–7199** — frontend dev servers
- **7200–7299** — backend/API services

Datastores (Postgres, etc.) and NATS keep their own conventional ports (see each demo's `README.md`), since those defaults are widely recognized and worth preserving rather than folding into this range. Assign the next free port in sequence within a demo and record it in that demo's `README.md` port table.

## Frontend Design System

Every UI in this repo — `lab-shell/` and each app under
`demos/01-dictionary/frontend/` — shares one visual identity and one page
shell, defined in `shared/unifi-theme/`. **This overrides the generic
instinct of design-oriented skills (e.g. `frontend-design`,
`artifact-design`) to avoid "genericness" by inventing a new palette, type
system, or page shell per task.** For any UI in this repo, reuse what's
below and spend creative effort on the content within it, not the frame
around it.

- **Theme** — `shared/unifi-theme/unifi.css` + `preset.js`: colors,
  typography (Inter, 13px/20px body), the PrimeVue v4 preset, and dark
  mode (`.p-dark` on `<html>`). Imported by every app via the
  `@unifi-theme` alias (see each app's `vite.config.js`). A new frontend
  wires these the same way the existing four already do — it does not
  define its own tokens or PrimeVue preset. Add a genuinely missing token
  there rather than forking it locally in one app.
- **Layout** — `shared/ui-shell/AppShell.vue` (+ `app-shell.css`): the top
  bar / collapsible sidebar / main content shell, imported by every app
  via the `@ui-shell` alias. Read `shared/unifi-theme/LAYOUT.md` before
  building or redesigning any top-level screen — it documents the slot
  API (`#brand`, `#breadcrumb`, `#topbar-right`, `#sidebar`, default) and
  the per-app usage notes. A new frontend consumes `AppShell.vue` the
  same way the existing four already do — it does not hand-roll its own
  topbar/sidebar markup. `shared/unifi-theme/app-shell-reference.html`
  remains the static visual reference the component is built from.
- **Sidebar collapse control** — there is exactly **one** rail toggle in
  this repo, defined in `AppShell.vue`, and it looks and behaves
  identically in all four apps. It is a borderless 26px icon button in a
  `.sidebar-foot` row at the **bottom-right** of the rail (centred once
  collapsed), drawing an inline panel-toggle SVG — not a PrimeIcon (there
  is no `panel-left`) and not a `«` / `»` text glyph. It carries
  `aria-label` (`Collapse sidebar` / `Expand sidebar`) and
  `aria-expanded`. **Don't add a per-app collapse/expand affordance, and
  don't override `.sidebar-foot` / `.sidebar-collapse-btn` in an app's own
  CSS** — an app that needs the control to differ is a change to
  `AppShell.vue` for everyone, or it isn't a change worth making. Bottom
  placement is deliberate and was arrived at the hard way: putting it at
  the top of the rail leaves a band of dead space between the topbar and
  the first nav group. `AppShell.spec.js` (in `admin/src/components/`,
  alongside `NavList.spec.js`, since `shared/ui-shell/` has no runner of
  its own) enforces the placement, the glyph, and the ARIA contract.
- **Exception — `demos/01-dictionary/docs/` (VitePress, Phase 37).**
  VitePress has its own theming layer, not a PrimeVue app, so this site
  does not import `@unifi-theme`/`@ui-shell` the way the four apps above
  do. It still must not invent an unrelated palette: reuse the same
  colors by overriding VitePress's own `--vp-c-*` custom properties in
  `.vitepress/theme/custom.css` (see that file for the mapping — dark
  `#131416`/`#006fff`, light `#f4f5f7`/`#005fdb`). Presentational idioms
  from `obsidian/Event sourcing/Event Sourcing + CQRS + NATS — Pattern
  Cards.pdf` (eyebrow labels, a "DECISION" callout container, verdict
  badges) live as local theme components/containers in
  `.vitepress/theme/`, not forked into `shared/unifi-theme/` unless a
  second app needs them.
- **Design viewport — 1920x1080. Check layout there, always.** Every UI
  in this repo is designed for that size, so it is the width any layout
  judgement has to be made at: column widths, table density, wrapping,
  dead space, whether something needs to scroll. This matters because the
  Browser pane opens at roughly 800px and its `resize_window` `desktop`
  preset returns the tab to the *pane's* size, not to a design width — so
  the default view is never the target, and sizing decisions taken there
  come out cramped on the real thing. **Before assessing or reporting on
  any layout, call `resize_window` with `{width: 1920, height: 1080}`**,
  and reset with the `desktop` preset when finished. Narrower widths are
  worth a look for graceful degradation, but they are not what the design
  is *for*: don't spend column budget or introduce horizontal scroll to
  satisfy them, and don't report a constraint measured at a narrower
  width as if it were a real limit.

### Generated reports, sketches, and diagrams

The same override applies to one-off generated artifacts that aren't live
app UI — architecture review reports (e.g. the
`mattpocock-skills:improve-codebase-architecture` command's HTML output),
ad hoc HTML sketches, and drawio diagrams (see the
`drawio-architecture-drawer` skill, which already applies this palette to
`.drawio` exports). **Ignore a skill's own default styling guidance (light
backgrounds, stone/slate neutrals, emerald/indigo accents, etc.) in favor of
the dark UniFi palette below** — do not default to that skill's built-in
report/sketch theme.

- Canvas / page background: `#131416` (matches `shared/unifi-theme/unifi.css`'s
  `--lab-bg`; `drawio-architecture-drawer` uses `#14171B` for diagram canvases
  specifically — either is acceptable, don't mix both in one document)
- Panels / cards: `#1A1E23`
- Primary accent (UI and service nodes, links, primary badges): `#006FFF`
- Authoritative-data accent (e.g. Postgres/source-of-truth nodes): `#27C07F`
- Lane, lifeline, and border strokes: `#4A515B`
- Primary text: `#DEE0E3`
- Secondary/muted text: `#B7BCC2`
- Warning / fallback accent: `#9A7B1E`
- Typography: Inter (fall back to `-apple-system, 'Segoe UI', Roboto,
  'Helvetica Neue', Arial, sans-serif`), 13px body / 20px line-height,
  matching `shared/unifi-theme/unifi.css`.

## Obsidian Vault (`obsidian/POC-Dictionaries/`)

An Obsidian vault accompanies demo 01, used to:

- **Capture research notes** — investigation and background on the NATS/CQRS patterns being evaluated.
- **Document the POC and findings** — problem statement, design write-ups, and results as they emerge.
- **Communicate the POC with stakeholders** — the vault is the shareable narrative layer (including exported PDFs like the pattern cards and poster).

Treat these as living documents: when a phase produces a notable finding or decision, add or update the relevant note here as well as the code-side docs (`BUSINESS_RULES.md`, and the `ARCHITECTURE*.md` docs — see "Architecture Docs" below). `BUSINESS_RULES.md` remains the source-of-truth for *how the code works*; `POC-Dictionaries/` is the source-of-truth for *why* and *what we learned*.

## Architecture Docs (`obsidian/V3-Platform/Architecture/Dictionary-POC/`)

The `ARCHITECTURE*.md` reference docs — `ARCHITECTURE.md` (CQRS shape taxonomy,
event sourcing vs. plain CRUD), `ARCHITECTURE-DICTIONARY.md` (refdata-service's
seeding, Postgres schema/ER diagram, data access paths, cross-service
consumption), `ARCHITECTURE-COMMUNICATIONS.md` (REST/Swagger + NATS
`rpc.*` dual-transport design, subject taxonomy),
`ARCHITECTURE-ACCOUNTS.md` (NATS operator-mode trust chain, tenant account
create/suspend/reactivate lifecycle, user auth and token lifecycle),
`ARCHITECTURE-ADMIN.md` (the Admin UI's SYSTEM → NATS navbar group —
per-panel architecture and data-flow patterns, plus the shared UI design
system these panels draw from), and `ARCHITECTURE-PLATFORM.md` (entry point
for the "Tech Lab Operator" frontend — the `refdata` app's operator/tenant-
facing nav and feature surface; owns the nav taxonomy and cross-feature
design, while `ARCHITECTURE-DICTIONARY.md` continues to own the Reference
Data feature's own backend/schema detail as one subset of it) — live in the obsidian vault
under `obsidian/V3-Platform/Architecture/Dictionary-POC/`, not in the repo
tree, alongside the editable `architecture-dictionary.drawio` workbook and its
exported PNGs (`images/`). This is a different vault location from
`obsidian/POC-Dictionaries/` above: `POC-Dictionaries/` is the narrative/
research vault (why and what was learned), while `Dictionary-POC/` under
`V3-Platform/Architecture/` holds the code-facing architecture reference layer
that used to live in the repo — still describing *how the code works*, just
relocated. The diagram generation scripts
(`demos/01-dictionary/diagrams/sync-unifi-assets.mjs`,
`demos/01-dictionary/diagrams/export-png.sh`) remain in the repo and resolve
paths into that vault directory; see the `drawio-architecture-drawer` skill.

## Commands

Standard `go build ./...` / `go test ./...` / `npm run dev` / `docker compose
up --build` work as you'd expect. What isn't standard:

### Tests — Ginkgo is the preferred runner

```bash
# install once
go install github.com/onsi/ginkgo/v2/ginkgo@latest

ginkgo ./...                    # runs suite and prints spec tree at the end
ginkgo watch ./...              # re-run on file changes
```

`go test ./...` is the no-install fallback. **Beware:** Postgres-backed specs
SKIP silently without their `*_TEST_DATABASE_URL` env var set, and `go test`
still prints `ok` — a green run is not proof those specs executed.

### Refdata service (Go — `demos/01-dictionary/backend/refdata-service/`)

`docker compose up --build` from `demos/01-dictionary/` starts every backend
service together. See `demos/01-dictionary/backend/refdata-service/README.md` for standalone run instructions (including the
default-port collision with `shipping-service` when both run outside Docker) and
`obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-DICTIONARY.md`
for its overall architecture — seeding, Postgres schema/ER diagram, data access paths (Postgres/REST/KV),
and cross-service consumption from the shipping backend.

## Demo 01 — Dictionary POC

### What it demonstrates

The POC evaluated three side-by-side CQRS/event-sourcing shapes over a shipping domain with Ship and Container aggregates — **Shape A** (KV as the read model), **Shape B** (Postgres projection + KV write-through cache), and **Shape C** (event-sourced reconstruction from JetStream replay) — before settling on one:

- **KV as a cache in front of Postgres** (the former "Shape B"): canonical CQRS projection in Postgres; KV is an eager write-through cache — the same JetStream event handler that upserts Postgres also overwrites the KV entry; cache miss falls through to Postgres. This is the shape the code runs today.

Phase 31 retired Shape A (KV-as-read-model) and Shape C (event-sourced reconstruction) once the comparison was decided — see `obsidian/POC-Dictionaries/` for the findings write-up on why Shape B won and `Main-POC-Plan-ARCHIVE.md` for the retired shapes' original design detail.

### Stream / KV design

```
Stream:   SHIPPING
Subjects: evt.{context}.shipping.ship.{shipID}.{arrived|departed}
          evt.{context}.shipping.container.{surrogateUUID}.{registered|loaded|unloaded}
Retention: LimitsPolicy (enables replay — NOT InterestPolicy)
Note: the leading token is the fixed literal "evt", not a wildcard — a
stream subject filter with an unbounded wildcard in the first position can
textually overlap "$SYS.>"/"$JS.API.>", and JetStream refuses to create such
a stream without NoAck (which would break synchronous Publish/PubAck). The
second token ("shipping") identifies the service in a shared
evt.{context}.{service}.{entity}.{entity-id}.{event} taxonomy — refdata-service
publishes under evt.{context}.refdata.{typeKey}.changed on its own REFDATA
stream.

KV buckets: ships, container, meta — one bucket per role per NATS account
(tenant-scoped by the account boundary itself, not by a per-context bucket
suffix); {context} lives in the key instead (see below), not the bucket name.
Key format: {context}.{entityType}.{id}   — NATS KV keys only allow [-/_=.a-zA-Z0-9]; ':' is illegal
Value: JSON-encoded ShipState / ContainerState / metadata
```

### Storage naming (streams, KV buckets, Object Stores)

**Streams are `SCREAMING_SNAKE`; KV buckets and Object Stores are
`lowercase-kebab`.** Examples as built: streams `SHIPPING`, `REFDATA`,
`TRANSPORTER`; KV `ships`, `container`, `meta`, `refdata`,
`organizations`, `organizations-secrets`; Object Store
`organizations-docs`.

- **KV and Object Stores share one casing on purpose.** NATS already
  distinguishes them where it matters — a KV bucket surfaces as the stream
  `KV_<name>` and an Object Store as `OBJ_<name>` — so casing them
  differently would re-encode, less reliably, a distinction the prefix
  already makes. It would also split them on two axes rather than one,
  since `SCREAMING_SNAKE` forces `_` where the rest use `-`, leaving
  sibling stores in one service looking unrelated
  (`ORGANISATION_DOCS` beside `organizations-secrets` — the concrete case
  this rule was written to settle, 2026-08-21).
- **The stream/bucket case split does earn its keep**, which is why it
  stays: `SHIPPING` and `ships` are the same domain in two roles, and
  nothing in NATS encodes which is which — the case does.
- **American spelling, plural** for the entity part, matching the
  entity-naming convention elsewhere (`organizations`, not
  `organisation`/`organization`).
- **A bucket name is a stream name.** Renaming a KV or Object Store bucket
  does not migrate its contents — it orphans the old stream and creates an
  empty new one. Check for existing data before renaming.

### Credential naming (NATS user JWT `name` claim)

**Same `lowercase-kebab` form as KV buckets above — these names double as
`.creds` filenames.** The `name` claim is the credential's only human label
and is what the Admin UI's Connections panel shows in its **Credential**
column. Full rules, the applied rename table, and the migration costs:
`obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-ACCOUNTS.md`
§ "Credential naming". The short form:

- **The name identifies the credential, not the connection** — several
  connections sharing one JWT are one credential.
- **A dedicated credential is named for its holder, spelled exactly as that
  process's `nats.Name()`** (`observability-service`, not `observability`).
  This is what makes a Name/Credential mismatch in the panel a *signal* —
  shared credential, or wrong `.creds` file mounted.
- **One holder needing several credentials suffixes the account**
  (`accounts-service-sys`, `accounts-service-platform`). That is the only
  place an account name belongs in a credential name.
- **A shared credential is named for the grant, not a holder** (`acme`).
- **Don't encode the account (except above), the tenant, ephemerality, or a
  `_token`/`_user` suffix** — the Connections panel's other columns already
  carry all of it, and a suffix on 100% of values distinguishes nothing.
- **Renaming an nsc user is delete-and-re-add**, so it mints a new user
  NKey and needs `docker compose down -v` + a bootstrap reseed, with
  compose mounts/env moving to the new filenames in the same change. A
  *tenant* creds filename is additionally load-bearing (`SwitchTenant`
  scans `<tenant>.creds`).

### Entity identity — ULID in `organizations-service`, UUID elsewhere

**Two ID formats coexist in this repo by decision, not by accident.**

- **`organizations-service` mints ULIDs** (ADR-051, BR-TP73): 26
  Crockford-base32 chars, minted in Go by `organizations/internal/identity`,
  never by Postgres. Its `id` / `organization_id` columns are therefore
  `TEXT` with no default — **don't "fix" them back to `uuid`, and don't add
  a `gen_random_uuid()` default**; Postgres cannot produce a ULID, so a new
  table in this service supplies its ID from `identity.New()` before the
  INSERT.
- **`shipping-service` and `accounts-service` stay on UUID.** Consciously
  excluded from ADR-051's scope, not overlooked.

Two rules that apply to any new ID anywhere in this repo:

- **An ID that appears in a subject token or KV key must be
  subject-safe.** No `.` (it would split the token and break the fixed-arity
  positional parsing below), no `*` or `>`, and nothing outside NATS KV's
  `[-/_=.a-zA-Z0-9]` set. This is why a country-prefixed company
  registration number was rejected as an identifier — see ADR-051 for the
  full case, and treat that ADR as settling the question rather than
  reopening it.
- **An aggregate's ID is immutable, because it is in the log.** An
  event-sourced aggregate's id is embedded in every subject it has ever
  published on a `LimitsPolicy` stream. Renumbering it orphans the whole
  history and the aggregate then **rehydrates as empty with no error**. So
  never migrate IDs in place: the supported path is `docker compose down -v`
  plus a reseed, which clears streams, KV buckets and Postgres together.

### Subject families and `{context}` (Phase 16a)

Full rules:
`obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md`
§ 2. Summary:

- **Core** — `evt.*` (event sourcing), `rpc.*` (service-to-service),
  `api.*` (frontend-to-service), `notify.*` (service-side change
  notification, replaces SSE). **Supportive** — `obs.rpc.*`/`obs.api.*`
  (debugging side-channel; never on a business path). `cmd.*` is reserved
  and unused.
- **`{context}` is the company / business-unit scope. It is NOT the tenant
  and NOT the region.** Tenancy is enforced strictly by the **NATS account**
  boundary (hard, server-enforced, resource-limited); region is a separate
  regional stack/NATS deployment. Neither ever appears in a subject token.
  Never reintroduce a tenant name into `{context}` — that is the
  pre-Phase-13 model Phase 13 deliberately replaced.
- A business unit is **hyphenated into the one token** (`acme`,
  `acme-northdiv`), never dot-separated — every subject family has fixed
  arity and parsers read `{context}` by position. Treat the value as opaque;
  don't split on `-`.
- Context values starting with `_` are **reserved for platform use**
  (`_platform`); enforced in both `refdata-service` (`ValidateContextName`,
  BR-D33 — the primary point, since context is its own resource) and
  `accounts-service` (BR-AC07 — a tenant name can double as its context in
  the common no-company-group case).
- `auth-service` and `accounts-service` subjects carry **no `{context}`**
  (they administer the tenant axis itself). `refdata-service` does carry it.
- A browser credential is never granted `rpc.>`; backend code never calls
  `api.>`.

### Backend package layout

Hexagonal, one module per bounded context: `cmd/main.go` bootstraps a monolith
and calls `Startup` on each module, and each module's `composition.go` is the
single wiring point where adapters bind to domain ports. Read the module you're
working in; the layout rule that isn't visible from the tree is in
"Architectural Notes" below.

## Architectural Notes

- **Hexagonal layout** throughout the Go backend: domain has no framework deps; adapters (postgres, rest, eventhandler) live in their own packages and wire in via `composition.go`.
- **Pinia stores** in the frontend are an intentional analogue to server-side materialized views — both are projected read models derived from an event source. This parallel should be preserved in UI and docs.
- **LimitsPolicy** (not InterestPolicy) on JetStream streams — required to support event replay.
- **Context-scoped KV keys**: every lookup includes a context prefix — no global unscoped lookups. `{context}` is the **company / business-unit** scope; the tenant is the **NATS account** and the region is a **separate regional deployment**, and neither ever appears in a key or subject (Phase 16a — see "Subject families and `{context}`" above).
- The demo frontend updates reactively via KV watch → SSE (or WebSocket) → frontend panels.
- **A long-lived service connection is built from `shared/natsconn`.** `natsconn.Options(name, credsPath, log)` supplies the name, credentials, and — the part that actually bit — `MaxReconnects(-1)`. nats.go's default is 60 attempts, so ~2 minutes after NATS goes away the client stops retrying and *closes* the connection permanently; every later JetStream/KV call then fails with `nats: connection closed` until the process is restarted. That is not theoretical: a `docker compose restart nats` used to leave `observability-service` dead until restarted by hand. Every long-lived connection in the repo now goes through this, including the per-tenant ones in `shared/natstenants`. A short-lived CLI (`cmd/seed-*`) is the exception — it should fail fast, not wait.
- **Every `nats.Connect` call must set `nats.Name(...)`** with the service name (e.g. `"shipping-service"`, `"refdata-service"`) — anonymous connections are indistinguishable in `nats server list connections` / `/connz` when debugging a running stack. `natsconn.Options` does this for you; a direct `nats.Connect` must do it itself. This is testable: assert `nc.Opts.Name != ""` (or equals the expected name) on the returned `*nats.Conn` in any test that calls `nats.Connect` directly.

- **Event sourcing vs plain CRUD — the deciding question is "does anything need to replay this," not "does it change."** Event-source an entity when its *history* is itself a domain concern: something needs to reconstruct state from the log (the write-side `hydrate()` path replays an aggregate's own events before applying a new command — see `ship.go`/`container.go`'s `Apply()`/`FromState()`), enforce rules against a point-in-time replay (Ship/Container cross-aggregate checks), or audit a sequence of transitions. Use plain Postgres CRUD when only *current state* matters and nothing ever reconstructs it from history — typically reference/master data with no state machine (lookup tables, config, enums). Don't let "is it reference data" be the whole test, though: some reference-looking data secretly needs history (a rate table where "what was in effect on date X" matters), and some lifecycle-looking entities are simple enough for plain CRUD if nothing ever replays them. See `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md` § "Event Sourcing vs Plain CRUD" for the worked example (Ship/Container vs the ports registry).

## Quality Rules

These apply to every task — new features, changes, and bug fixes alike:

1. **Every business rule must have a test.** If a domain rule is added or changed, a corresponding integration test must be added or updated in the same task before marking it complete.
2. **All tests must be green before a task is complete.** Run `ginkgo ./...` from `demos/01-dictionary/backend/shipping-service/` as the final step of any backend change. A task is not done until this passes clean.
3. **Business rules live in the domain layer** (`dictionary/internal/domain/`). Rule enforcement must not leak into handlers or application services.
4. **The business rules summary must be kept in sync.** When a rule is added or removed, update the relevant domain file — `demos/01-dictionary/BUSINESS_RULES-SHIPPING.md` (Ship/Container) or `demos/01-dictionary/BUSINESS_RULES-REFDATA.md` (refdata-service) — as part of the same task. `BUSINESS_RULES.md` is just an index; don't add rule detail there.

## AI Agent Workflow

Any exploration touching more than 3 files should be delegated to an Explore subagent rather than done with inline `Read`/`grep` calls — keeps the main conversation's context window free for the actual task.

When updating or implementing a plan phase, the agent should follow this sequence:

1. **Ask for business rules first.** Before writing any code or updating a plan, ask the user to confirm or supply the applicable business rules for the feature. If rules are already documented (see `BUSINESS_RULES.md`'s index for which domain file), confirm they are complete and up to date.
2. **Design gate.** Phase entries in `Main-POC-Plan.md` must include a "Design decisions" section, and the phase stays PROPOSED — no tasks, tests, or code written — until the user approves it.
3. **Derive tests from rules, not from implementation.** Each business rule maps to one `Context` block in Ginkgo with one or more `It` assertions. Write the specs before writing the implementation (red → green → refactor).
4. **Update the relevant `BUSINESS_RULES-*.md` and the plan together.** New rules go into the matching domain file and the plan checklist in the same commit.

## Implementation Status

See `.claude/plans/Main-POC-Plan.md` for the phased plan and checkbox tracking.

### The plan is three files — keep it that way

`Main-POC-Plan.md` holds **only phases that are actively being worked or are
next up**. Everything else lives beside it under `.claude/plans/`, and neither
sibling is read into context by default:

- **Completed** phases → `Main-POC-Plan-ARCHIVE.md` (append-only; never edit
  its existing content), along with all renumbering logs.
- **Never-implemented** phases — candidate, proposed, deferred, placeholder,
  approved-but-on-hold → `Main-POC-Plan-Candidates.md`.

Each keeps a one-line self-describing stub in the live plan. This is a standing
rule, not a one-off cleanup: move a phase out **as soon as** it completes or is
deferred, rather than letting the live plan accumulate. Invoke the
`archive-plan-phase` skill for the full procedure and its rules — don't
improvise it.
