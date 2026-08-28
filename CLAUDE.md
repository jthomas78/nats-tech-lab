# CLAUDE.md

Canonical guidance for all AI coding agents in this repo. Agent-specific entry
files should reference this file, not duplicate it.

## Session Memory

At session start, read `.claude/memory/MEMORY.md` (an index of one-line hooks) and
keep it as context. **Do not read every file under `.claude/memory/`** — open an
individual memory file only when its `MEMORY.md` hook looks relevant to the task.
Save new memories to `.claude/memory/` (not `~/.claude/projects/`) so they sync via
git; keep the `MEMORY.md` hook line short enough to judge relevance without opening
the file.

## General preferences

- If asked to do too much at once, stop and say so.
- If computer use helps complete or verify work, shell out to Codex (`codex:rescue`
  skill / `codex:codex-rescue` agent).
- **Don't read large docs whole by default.** Before `Read` with no `offset`/`limit`
  on a file over ~150 lines, prefer `grep`/`Grep` or a targeted `Read`. Full reads
  are fine for a first pass on a new doc or a short file. Applies especially to
  `BUSINESS_RULES-*.md`, `PERFORMANCE.md`, `.claude/plans/*`, and the
  `ARCHITECTURE*.md` docs (see "Architecture Docs").
- Any exploration touching more than 3 files → delegate to an Explore subagent.

## Purpose

A lab for evaluating NATS.io patterns for a V3 greenfield logistics platform. Each
demo is self-contained: pick a demo from the lab shell, read an intro, launch via
Docker, tear down when done.

Core question: **the correct responsibility split between JetStream (event
backbone), NATS KV (fast lookup/watch/cache), Postgres (transactional source of
truth), and CQRS projections.**

## Repository Layout

Read the tree with `ls` — it changes faster than this file. Not visible from the
tree:

- Each demo has its own `docker-compose.yml` and does **not** share a network with
  the lab shell or other demos.
- A demo's top-level `README.md` is the **intro text rendered in the lab shell** —
  edit it with that audience in mind.
- `lab-shell/` is the demo menu / intro pages; per-demo UIs live under
  `demos/<demo>/frontend/`.
- Two obsidian vaults, different jobs — see "Obsidian Vault" and "Architecture
  Docs".

## Docker Host Port Allocation

Two fixed 4-digit ranges:

- **7100–7199** — frontend dev servers
- **7200–7299** — backend/API services

Datastores (Postgres, etc.) and NATS keep their conventional ports. Assign the next
free port in sequence within a demo and record it in that demo's `README.md` port
table.

## Frontend Design System

Every UI in this repo (`lab-shell/` and each app under
`demos/01-dictionary/frontend/`) shares one visual identity and one page shell in
`shared/unifi-theme/`. **This overrides design skills' instinct (`frontend-design`,
`artifact-design`) to invent a new palette/type system/shell per task.** Reuse the
frame; spend creative effort on the content.

- **Theme** — `shared/unifi-theme/unifi.css` + `preset.js`: colors, typography
  (Inter, 13px/20px body), PrimeVue v4 preset, dark mode (`.p-dark` on `<html>`).
  Imported via the `@unifi-theme` alias (see each app's `vite.config.js`). A new
  frontend wires it the same way the existing four do. Add a genuinely missing
  token there rather than forking locally.
- **Layout** — `shared/ui-shell/AppShell.vue` (+ `app-shell.css`): topbar /
  collapsible sidebar / main content shell, imported via `@ui-shell`. Read
  `shared/unifi-theme/LAYOUT.md` before building or redesigning any top-level
  screen — it documents the slot API (`#brand`, `#breadcrumb`, `#topbar-right`,
  `#sidebar`, default) and per-app notes. Consume `AppShell.vue`, don't hand-roll
  topbar/sidebar markup. `shared/unifi-theme/app-shell-reference.html` is the
  static visual reference.
- **Sidebar collapse control** — exactly **one** rail toggle in the repo, in
  `AppShell.vue`, identical in all four apps: a borderless 26px icon button in a
  `.sidebar-foot` row at the **bottom-right** of the rail (centred when collapsed),
  drawing an inline panel-toggle SVG (not a PrimeIcon, not a `«`/`»` glyph), with
  `aria-label` (`Collapse sidebar`/`Expand sidebar`) and `aria-expanded`. Don't add
  a per-app collapse affordance; don't override `.sidebar-foot` /
  `.sidebar-collapse-btn` in an app's CSS — a needed change is a change to
  `AppShell.vue` for everyone. Bottom placement is deliberate (top leaves dead
  space below the topbar). `AppShell.spec.js` (in `admin/src/components/`) enforces
  placement, glyph, and ARIA.
- **Exception — `demos/01-dictionary/docs/` (VitePress, Phase 37).** Has its own
  theming layer, so it does not import `@unifi-theme`/`@ui-shell`. Still must not
  invent a palette: reuse the same colors by overriding VitePress's `--vp-c-*`
  properties in `.vitepress/theme/custom.css` (dark `#131416`/`#006fff`, light
  `#f4f5f7`/`#005fdb`). Presentational idioms (eyebrow labels, "DECISION" callout,
  verdict badges) live as local theme components in `.vitepress/theme/`, not forked
  into `shared/unifi-theme/` unless a second app needs them.
- **Design viewport — 1920x1080. Always judge layout there.** The Browser pane
  opens at ~800px and `resize_window`'s `desktop` preset returns to the *pane's*
  size, not a design width. **Before assessing any layout, call `resize_window`
  with `{width: 1920, height: 1080}`**; reset with `desktop` when done. Narrower
  widths are worth a graceful-degradation look but aren't the target — don't spend
  column budget or add horizontal scroll for them.

### Generated reports, sketches, and diagrams

Same override for one-off generated artifacts (architecture review reports, ad hoc
HTML sketches, drawio diagrams). **Ignore a skill's default styling (light
backgrounds, stone/slate, emerald/indigo) in favor of the dark UniFi palette:**

- Canvas / page background: `#131416` (`drawio-architecture-drawer` uses `#14171B`
  for diagram canvases — either is fine, don't mix both in one doc)
- Panels / cards: `#1A1E23`
- Primary accent (UI/service nodes, links, primary badges): `#006FFF`
- Authoritative-data accent (Postgres/source-of-truth): `#27C07F`
- Lane / lifeline / border strokes: `#4A515B`
- Primary text: `#DEE0E3`
- Secondary/muted text: `#B7BCC2`
- Warning / fallback accent: `#9A7B1E`
- Typography: Inter (fall back to `-apple-system, 'Segoe UI', Roboto,
  'Helvetica Neue', Arial, sans-serif`), 13px / 20px line-height.

## Obsidian Vault (`obsidian/POC-Dictionaries/`)

Narrative/research vault for demo 01 — research notes, POC problem statement /
design write-ups / findings, and the shareable stakeholder narrative (including
exported PDFs). Living documents: when a phase produces a finding or decision,
update the relevant note here as well as the code-side docs. Split of
responsibility: `BUSINESS_RULES.md` + `ARCHITECTURE*.md` = *how the code works*;
`POC-Dictionaries/` = *why* and *what we learned*.

## Architecture Docs (`obsidian/V3-Platform/Architecture/Dictionary-POC/`)

Code-facing architecture reference (relocated out of the repo tree). Read targeted,
not whole. The `ARCHITECTURE*.md` set:

- `ARCHITECTURE.md` — CQRS shape taxonomy; event sourcing vs plain CRUD
- `ARCHITECTURE-DICTIONARY.md` — refdata-service seeding, Postgres schema/ER,
  data access paths (Postgres/REST/KV), cross-service consumption
- `ARCHITECTURE-COMMUNICATIONS.md` — REST/Swagger + NATS `rpc.*` dual transport,
  subject taxonomy
- `ARCHITECTURE-ACCOUNTS.md` — NATS operator-mode trust chain, tenant account
  lifecycle, user auth / token lifecycle
- `ARCHITECTURE-ADMIN.md` — Admin UI's SYSTEM → NATS navbar group; per-panel
  architecture and data-flow, plus its shared UI design system
- `ARCHITECTURE-PLATFORM.md` — entry point for the "Tech Lab Operator" frontend
  (`refdata` app's nav + feature surface); owns nav taxonomy / cross-feature design
- `ARCHITECTURE-APP-SHELL.md` — extensible app shell: frontend plugin registry,
  contribution kinds, host-owned extension points, Module Federation loader,
  migration map. Phase plan:
  `.claude/plans/Application-Shell-Microfrontend-Plan.md`

Same directory holds the editable `architecture-dictionary.drawio` and exported
PNGs (`images/`). Diagram scripts stay in the repo
(`demos/01-dictionary/diagrams/{sync-unifi-assets.mjs,export-png.sh}`) and resolve
into that vault dir; see the `drawio-architecture-drawer` skill.

## Commands

Standard `go build ./...` / `go test ./...` / `npm run dev` / `docker compose up
--build` work as expected. Non-standard:

### Tests — Ginkgo is the preferred runner

```bash
go install github.com/onsi/ginkgo/v2/ginkgo@latest   # once
ginkgo ./...          # runs suite, prints spec tree
ginkgo watch ./...    # re-run on change
```

`go test ./...` is the no-install fallback. **Beware:** Postgres-backed specs SKIP
silently without their `*_TEST_DATABASE_URL` env var, and `go test` still prints
`ok` — green is not proof they ran.

### Backend services

`docker compose up --build` from `demos/01-dictionary/` starts every backend
service together. `refdata-service` and `shipping-service` share a default port
outside Docker — see `demos/01-dictionary/backend/refdata-service/README.md` for
standalone run instructions, and `ARCHITECTURE-DICTIONARY.md` for architecture.

## Demo 01 — Dictionary POC

### What it demonstrates

The POC compared three CQRS/event-sourcing shapes over a shipping domain (Ship +
Container aggregates) — Shape A (KV as read model), Shape B (Postgres projection +
KV write-through cache), Shape C (event-sourced reconstruction from replay) — and
settled on:

- **KV as a cache in front of Postgres** (former "Shape B"): canonical CQRS
  projection in Postgres; KV is an eager write-through cache (the same JetStream
  handler that upserts Postgres overwrites the KV entry); cache miss falls through
  to Postgres. This is what the code runs today.

Phase 31 retired Shapes A and C. Findings write-up: `obsidian/POC-Dictionaries/`.
Retired-shape design detail: `Main-POC-Plan-ARCHIVE.md`.

### Stream / KV design

```
Stream:   SHIPPING
Subjects: evt.{context}.shipping.ship.{shipID}.{arrived|departed}
          evt.{context}.shipping.container.{surrogateUUID}.{registered|loaded|unloaded}
Retention: LimitsPolicy (enables replay — NOT InterestPolicy)
```

The leading token is the fixed literal `evt`, not a wildcard — an unbounded
wildcard in position 1 textually overlaps `$SYS.>`/`$JS.API.>`, and JetStream
refuses such a stream without NoAck (which breaks synchronous Publish/PubAck). The
2nd token identifies the service in a shared
`evt.{context}.{service}.{entity}.{entity-id}.{event}` taxonomy — refdata-service
publishes `evt.{context}.refdata.{typeKey}.changed` on its own REFDATA stream.

```
KV buckets: ships, container, meta — one bucket per role per NATS account
            (tenant-scoped by the account boundary; {context} lives in the key)
Key format: {context}.{entityType}.{id}   — KV keys allow only [-/_=.a-zA-Z0-9]; ':' is illegal
Value:      JSON-encoded ShipState / ContainerState / metadata
```

### Storage naming (streams, KV buckets, Object Stores)

**Streams are `SCREAMING_SNAKE`; KV buckets and Object Stores are
`lowercase-kebab`.** As built: streams `SHIPPING`, `REFDATA`, `TRANSPORTER`; KV
`ships`, `container`, `meta`, `refdata`, `organizations`, `organizations-secrets`;
Object Store `organizations-docs`.

- **KV and Object Stores share one casing on purpose** — NATS already distinguishes
  them via the `KV_<name>` / `OBJ_<name>` stream prefix, and `SCREAMING_SNAKE`
  would force `_` where the rest use `-` (settled 2026-08-21).
- **The stream/bucket case split earns its keep**: `SHIPPING` and `ships` are one
  domain in two roles and only the case encodes which.
- **American spelling, plural** for the entity part (`organizations`).
- **A bucket name is a stream name.** Renaming a KV/Object Store bucket does not
  migrate contents — it orphans the old stream and creates an empty new one. Check
  for data before renaming.

### Credential naming (NATS user JWT `name` claim)

Same `lowercase-kebab` form; these names double as `.creds` filenames and show in
the Admin UI Connections panel's **Credential** column. Full rules + rename table:
`ARCHITECTURE-ACCOUNTS.md` § "Credential naming". Short form:

- **The name identifies the credential, not the connection** — several connections
  on one JWT are one credential.
- **A dedicated credential is named for its holder, spelled exactly as that
  process's `nats.Name()`** (`observability-service`, not `observability`) — so a
  Name/Credential mismatch in the panel is a signal.
- **One holder needing several credentials suffixes the account**
  (`accounts-service-sys`, `accounts-service-platform`) — the only place an account
  name belongs in a credential name.
- **A shared credential is named for the grant** (`acme`), not a holder.
- **Don't encode** the account (except above), tenant, ephemerality, or a
  `_token`/`_user` suffix — other panel columns carry that.
- **Renaming an nsc user is delete-and-re-add** — mints a new NKey, needs
  `docker compose down -v` + bootstrap reseed, with compose mounts/env moving to
  the new filenames in the same change. A *tenant* creds filename is additionally
  load-bearing (`SwitchTenant` scans `<tenant>.creds`).

### Entity identity — ULID in `organizations-service`, UUID elsewhere

Two ID formats coexist by decision:

- **`organizations-service` mints ULIDs** (ADR-051, BR-TP73): 26 Crockford-base32
  chars, minted in Go by `organizations/internal/identity`, never by Postgres. Its
  `id` / `organization_id` columns are `TEXT` with no default — **don't "fix" them
  to `uuid` or add a `gen_random_uuid()` default**; a new table here supplies its
  ID from `identity.New()` before the INSERT.
- **`shipping-service` and `accounts-service` stay on UUID** — consciously outside
  ADR-051's scope.

Two rules for any new ID anywhere:

- **An ID in a subject token or KV key must be subject-safe** — no `.` (splits the
  token, breaks fixed-arity positional parsing), no `*`/`>`, nothing outside
  `[-/_=.a-zA-Z0-9]`. (Why a country-prefixed registration number was rejected —
  see ADR-051, treat it as settled.)
- **An aggregate's ID is immutable, because it is in the log.** It's embedded in
  every subject the aggregate ever published on a `LimitsPolicy` stream;
  renumbering orphans the history and it **rehydrates as empty with no error**.
  Never migrate IDs in place — the path is `docker compose down -v` + reseed
  (clears streams, KV, Postgres together).

### Subject families and `{context}` (Phase 16a)

Full rules: `ARCHITECTURE-COMMUNICATIONS.md` § 2. Summary:

- **Core** — `evt.*` (event sourcing), `rpc.*` (service-to-service), `api.*`
  (frontend-to-service), `notify.*` (service-side change notification, replaces
  SSE). **Supportive** — `obs.rpc.*`/`obs.api.*` (debug side-channel, never on a
  business path). `cmd.*` is reserved and unused.
- **`{context}` is the company / business-unit scope. NOT the tenant, NOT the
  region.** Tenancy is enforced by the **NATS account** boundary (hard,
  server-enforced); region is a separate regional deployment. Neither ever appears
  in a subject token. Never put a tenant name back into `{context}` (the
  pre-Phase-13 model).
- A business unit is **hyphenated into one token** (`acme`, `acme-northdiv`), never
  dot-separated — fixed arity, parsers read `{context}` by position. Treat it as
  opaque; don't split on `-`.
- Context values starting with `_` are **reserved for platform use** (`_platform`)
  — enforced in `refdata-service` (`ValidateContextName`, BR-D33) and
  `accounts-service` (BR-AC07).
- `auth-service` and `accounts-service` subjects carry **no `{context}`** (they
  administer the tenant axis); `refdata-service` does carry it.
- A browser credential is never granted `rpc.>`; backend code never calls `api.>`.

## Architectural Notes

- **Hexagonal layout** throughout the Go backend, one module per bounded context:
  domain has no framework deps; adapters (postgres, rest, eventhandler) live in
  their own packages and wire in via `composition.go`. `cmd/main.go` bootstraps a
  monolith and calls `Startup` on each module. Read the module you're in.
- **Pinia stores** in the frontend are an intentional analogue to server-side
  materialized views — both are projected read models from an event source.
  Preserve the parallel in UI and docs.
- **LimitsPolicy** (not InterestPolicy) on JetStream streams — required for replay.
- **Context-scoped KV keys** — every lookup includes a context prefix; no global
  unscoped lookups.
- The demo frontend updates reactively via KV watch → SSE (or WebSocket) → panels.
- **A long-lived service connection is built from `shared/natsconn`.**
  `natsconn.Options(name, credsPath, log)` supplies the name, creds, and
  `MaxReconnects(-1)`. nats.go defaults to 60 attempts, then *closes* the
  connection permanently — every later JetStream/KV call fails with `nats:
  connection closed` until restart (this really bit `observability-service` after
  `docker compose restart nats`). Every long-lived connection goes through this,
  including per-tenant ones in `shared/natstenants`. Short-lived CLIs (`cmd/seed-*`)
  are the exception — fail fast.
- **Every `nats.Connect` must set `nats.Name(...)`** with the service name —
  anonymous connections are indistinguishable in `nats server list connections` /
  `/connz`. `natsconn.Options` does this; a direct `nats.Connect` must too.
  Testable: assert `nc.Opts.Name != ""` on the returned `*nats.Conn`.
- **Event sourcing vs plain CRUD — the deciding question is "does anything need to
  replay this," not "does it change."** Event-source when history is itself a
  domain concern: something reconstructs state from the log (write-side `hydrate()`
  replays an aggregate's events — see `ship.go`/`container.go` `Apply()`/
  `FromState()`), enforces rules against a point-in-time replay, or audits a
  sequence of transitions. Plain Postgres CRUD when only current state matters
  (lookup tables, config, enums). Not a pure "is it reference data" test — a rate
  table where "what was in effect on date X" matters needs history. Worked example:
  `ARCHITECTURE.md` § "Event Sourcing vs Plain CRUD".

## Quality Rules

Apply to every task — features, changes, bug fixes:

1. **Every business rule must have a test** — added/changed in the same task before
   it's complete.
2. **All tests green before a task is done.** Run `ginkgo ./...` from the service
   you changed. The backend is seven modules (`accounts-service`,
   `observability-service`, `organizations-service`, `otlp-bridge`,
   `pricing-service`, `refdata-service`, `shipping-service`). Also run
   `shipping-service`'s suite whenever a change reaches into it.
3. **Business rules live in the domain layer.** Path differs per service
   (`dictionary/internal/domain/` in `shipping-service`, `refdata/internal/domain/`
   in `refdata-service`, …) — read the module you're in. No rule enforcement in
   handlers or application services.
4. **Keep the business rules summary in sync.** When a rule is added/removed, update
   the matching file under `demos/01-dictionary/` — `BUSINESS_RULES-SHIPPING.md`
   (Ship/Container), `-REFDATA.md`, `-ACCOUNTS.md`, `-ORGANIZATIONS.md`,
   `-PRICING.md`, or `-APP-SHELL.md` (the `lab-shell/` app shell + micro-frontend
   plugins) — in the same task. `BUSINESS_RULES.md` is just an index.

## AI Agent Workflow — plan phases

1. **Ask for business rules first.** Before code or a plan update, ask the user to
   confirm or supply the applicable rules. If already documented (see
   `BUSINESS_RULES.md`'s index), confirm they're complete.
2. **Design gate.** A phase entry in `Main-POC-Plan.md` must include a "Design
   decisions" section and stays PROPOSED — no tasks/tests/code — until the user
   approves.
3. **Derive tests from rules, not implementation.** Each rule → one Ginkgo
   `Context` with one or more `It`s. Specs before implementation (red → green →
   refactor).
4. **Update the `BUSINESS_RULES-*.md` and the plan together**, same commit.

## Implementation Status

`.claude/plans/Main-POC-Plan.md` holds the phased plan and checkbox tracking.

### The plan is three files — keep it that way

`Main-POC-Plan.md` holds **only phases actively being worked or next up**. The
siblings aren't read into context by default:

- **Completed** → `Main-POC-Plan-ARCHIVE.md` (append-only; never edit existing
  content), with all renumbering logs.
- **Never-implemented** (candidate, proposed, deferred, on-hold) →
  `Main-POC-Plan-Candidates.md`.

Each leaves a one-line stub in the live plan. Move a phase out **as soon as** it
completes or is deferred. Use the `archive-plan-phase` skill for the procedure —
don't improvise it.
