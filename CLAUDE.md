# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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

```
nats-tech-lab/
  lab-shell/              # Vue 3 + PrimeVue + Pinia — demo menu + intro pages
  demos/
    01-dictionary/        # First demo: Dictionary POC
      backend/
        shipping-service/ # Go service (hexagonal layout) — Ship/Container CQRS shapes A/B/C
        refdata-service/  # Go service (Phase 11) — dictionary-as-a-service, own Postgres schema + container
          README.md         # refdata-service-specific: what it is, how to run/query it standalone
      frontend/
        admin/           # Vue 3 architecture/demo UI
        seafreight-app/  # Vue 3 ship/terminal operations UI
        refdata/         # Vue 3 dictionary/reference-data admin UI (Phase 11)
	  docker-compose.yml  # Postgres + NATS + shipping-service + refdata-service + all three frontends
      README.md           # Intro text shown in lab shell
  obsidian/
    POC-Dictionaries/     # Obsidian vault for demo 01 (research, findings, stakeholder docs)
    V3-Platform/
      Architecture/
        Dictionary-POC/   # Architecture reference docs for demo 01 (see "Architecture Docs" below)
```

Each demo has its own `docker-compose.yml` and does **not** share a network with the lab shell or other demos.

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
create/suspend/reactivate lifecycle, user auth and token lifecycle), and
`ARCHITECTURE-ADMIN.md` (the Admin UI's SYSTEM → NATS navbar group —
per-panel architecture and data-flow patterns, plus the shared UI design
system these panels draw from) — live in the obsidian vault
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

### Backend (Go — `demos/01-dictionary/backend/shipping-service/`)

```bash
go build ./...

# Tests — preferred runner is Ginkgo (install once: go install github.com/onsi/ginkgo/v2/ginkgo@latest)
ginkgo ./...                    # runs suite and prints spec tree at the end
ginkgo watch ./...              # re-run on file changes

# Fallback (no install required)
go test ./...
go test ./path/to/package/...   # run a single package

docker compose up --build       # from demos/01-dictionary/
docker compose down             # tear down
```

### Refdata service (Go — `demos/01-dictionary/backend/refdata-service/`)

```bash
go build ./...
go test ./...

docker compose up --build       # from demos/01-dictionary/ — starts shipping-service + refdata-service together
```

See `demos/01-dictionary/backend/refdata-service/README.md` for standalone run instructions (including the
default-port collision with `shipping-service` when both run outside Docker) and
`obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-DICTIONARY.md`
for its overall architecture — seeding, Postgres schema/ER diagram, data access paths (Postgres/REST/KV),
and cross-service consumption from the shipping backend.

### Frontend (Vue 3 — either demo frontend or `lab-shell/`)

```bash
npm install
npm run dev
npm run build
```

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

```
cmd/main.go                       # bootstraps monolith, calls Startup on each module
internal/monolith/                # Monolith + Module interfaces
internal/jstream/stream.go        # JetStream wrapper (LimitsPolicy)
internal/kvstore/kv.go            # NATS KV wrapper
dictionary/
  composition.go
  internal/
    domain/                       # Ship + Container aggregates, events, repository ports
    application/commands/         # ship/container commands + JetStream hydration
    application/queries/          # Shapes A/B/C, terminal and metadata queries
    postgres/                     # ship/container projection repositories
    eventhandler/                 # JetStream consumers → Postgres/KV projections
    rest/                         # HTTP handlers
```

## Architectural Notes

- **Hexagonal layout** throughout the Go backend: domain has no framework deps; adapters (postgres, rest, eventhandler) live in their own packages and wire in via `composition.go`.
- **Pinia stores** in the frontend are an intentional analogue to server-side materialized views — both are projected read models derived from an event source. This parallel should be preserved in UI and docs.
- **LimitsPolicy** (not InterestPolicy) on JetStream streams — required to support event replay.
- **Context-scoped KV keys**: every lookup includes a context prefix — no global unscoped lookups. `{context}` is the **company / business-unit** scope; the tenant is the **NATS account** and the region is a **separate regional deployment**, and neither ever appears in a key or subject (Phase 16a — see "Subject families and `{context}`" above).
- The demo frontend updates reactively via KV watch → SSE (or WebSocket) → frontend panels.
- **Every `nats.Connect` call must set `nats.Name(...)`** with the service name (e.g. `"shipping-service"`, `"refdata-service"`) — anonymous connections are indistinguishable in `nats server list connections` / `/connz` when debugging a running stack. This is testable: assert `nc.Opts.Name != ""` (or equals the expected name) on the returned `*nats.Conn` in any test that calls `nats.Connect` directly.
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

## AI Skill Roles (Future, not yet implemented)

Intent, for later: introduce `.claude/skills/` personas (`product-owner`, `technical-analyst`, `software-developer`, `tester`) so plan phases get walked through each role in sequence instead of going straight from requirement to code.

## Implementation Status

See `.claude/plans/Main-POC-Plan.md` for the full phased plan and checkbox tracking.

### Archiving completed phases

`Main-POC-Plan.md` is read every session, so completed phases must not be left
to accumulate in it. **When a phase is complete, move its full detail to
`.claude/plans/Main-POC-Plan-ARCHIVE.md` and leave a one-line `- [x]` stub
behind in the live plan.** Follow the shape the existing "Phases 0–11",
"Phases 12–14", and "Phases 15–19" sections already use: a short
`### Phases N–M — Completed` heading, the standing note that full detail is
archived and *not read into context by default*, then one checked bullet per
phase naming what it delivered.

Rules that matter when doing this:

- **Archive by completion, not by number.** Completed phases are rarely a
  contiguous block — a later phase is often finished while an earlier one is
  still `PROPOSED`. Never archive an unfinished phase just because it sits
  between two finished ones.
- **Never edit the archive's existing content.** It is a set of frozen
  snapshots. Append new sections; don't rewrite old ones, and don't update
  their phase numbers during a renumbering (the renumbering tables at the
  bottom of the live plan record why).
- **Keep the stub bullet self-describing.** Someone should be able to tell what
  a phase did from the live plan alone, and only need the archive for original
  rationale or checklist detail.
- **Candidate/deferred phases move to the 100+ block**, at the end of the live
  plan — they are not archived, since they were never implemented.
