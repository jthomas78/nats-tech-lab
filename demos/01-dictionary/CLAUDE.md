# CLAUDE.md — demos/01-dictionary

**This folder is a sealed unit. Read this file instead of the root `CLAUDE.md`
for anything inside it.**

The root file covers the whole lab shell. Demo 01 is the Postgres-backed,
multi-service, multi-frontend POC — the biggest demo here, with its own
plan file, business rules, and architecture docs. Most of the root file
(demo 02, demo 04, the pattern-cards workflow) does not apply here, but the
**general preferences**, **session memory rules**, **Docker host port
allocation**, and **Frontend Design System** sections in root DO apply —
this demo is one of the apps that shares `shared/unifi-theme` /
`shared/ui-shell`.

## Running demo 01

The stack is three bands under `demos/01-dictionary/deploy/` (ADR-055):

- `cell/` — one region. `compose.yaml` includes `compose.infra.yaml` (NATS,
  Postgres, Temporal) and `compose.runtime.yaml` (the services and frontends).
- `cell/compose.dedicated.yaml` — the micro-frontend plugin fixtures. Not part of
  a cell; add it with an extra `-f` when you want them.
- `cell/compose.plugins.yaml` — an overlay that puts `app-shell-frontend` on the
  external `lab-shell-plugins` network, so the packaged shell can reach another
  demo's frontend container and serve it at `/plugins/<id>/…` (app-shell
  BR-AS77, task 16l). Not part of a cell either; add it with an extra `-f`. It
  attaches nothing else, and the cell's own networks are unchanged — the shell
  does **not** join the other demo's network and the other demo does **not**
  join this one's. Create the network once with
  `docker network create lab-shell-plugins`; it is `external: true` on both
  sides, so a cell brought up without this file behaves exactly as before.
- `global/compose.control.yaml` — the control plane (`accounts-service`). One per
  trust domain, not one per region.

A cell's NATS is **one server**, service name `nats`. There is no cluster, no
gateway and no JetStream `domain` here. **All multi-region work — clustering,
gateways, superclusters and JetStream domains — lives in `demos/02-multi-region/`
and must not be added back to demo 01** (reverted 2026-09-09). Demo 01 is one
region, one cell, and stays that way.

Every host port is a variable, supplied by `deploy/environments/local-<cell>.env`.
`za-1` holds the base ports; `au-1` shifts them by +50, so both cells can run at
once. Run from `demos/01-dictionary/deploy/cell/`:

```bash
docker compose -p poc --env-file ../environments/local-za-1.env -f compose.yaml -f ../global/compose.control.yaml up -d --build
```

`refdata-service` and `shipping-service` share a default port outside Docker — see
`demos/01-dictionary/backend/refdata-service/README.md` for standalone run
instructions, and `ARCHITECTURE-DICTIONARY.md` for architecture.

## What it demonstrates

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

## Stream / KV design

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

## Storage naming (streams, KV buckets, Object Stores)

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

## Credential naming (NATS user JWT `name` claim)

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

## Entity identity — ULID in `organizations-service`, UUID elsewhere

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

## Subject families and `{context}` (Phase 16a)

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
