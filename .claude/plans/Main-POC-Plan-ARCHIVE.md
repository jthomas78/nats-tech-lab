# nats-tech-lab — Dictionary POC Plan Archive

Full verbatim detail for **completed** phases, moved out of the live plan
(`Main-POC-Plan.md`) to keep that file lean, plus the plan's whole
**renumbering history** (see the "Renumbering history" section at the end).
This file is a reference — it is not meant to be read into context by
default; open it only when you need the original rationale, checklist
detail, or design notes for a specific completed phase.

The live plan (`Main-POC-Plan.md`) keeps a one-line status entry per
phase below, linking back here. Phases that were never implemented —
candidate, deferred, placeholder, or approved-but-on-hold — are **not**
archived here; they live in `Main-POC-Plan-Candidates.md`.

This file is append-only: never edit or reflow content already in it.

---

## Implementation Phases

### Phase 0 — Scaffolding

- [x] Initialise Go module in `demos/01-dictionary/backend/`
- [x] Port `internal/monolith` interfaces from Fizmath Plaza (written fresh: `Monolith` + `Module`)
- [x] Write `internal/jstream/stream.go` with `LimitsPolicy`
- [x] Write `internal/kvstore/kv.go` wrapper
- [x] `docker-compose.yml` for demo 01 (NATS + Postgres only first)

### Phase 1 — Shape A (KV-only read model)

- [x] `dictionary/internal/domain/` — DictionaryEntry, events
- [x] `dictionary/internal/application/commands/` — CreateEntry, UpdateEntry
- [x] `dictionary/internal/eventhandler/` — consumes JetStream, writes to KV
- [x] `dictionary/internal/application/queries/` — GetEntry reads from KV
- [x] `dictionary/internal/rest/` — HTTP handlers
- [x] Backend wired in `composition.go` + `cmd/main.go`
- [x] Smoke test: create entry → event → KV → GET returns value
      (`dictionary/integration_test.go` against an embedded in-process NATS server)

### Phase 2 — Shape B (KV cache + Postgres projection)

- [x] `dictionary/internal/postgres/` — repo implementation, migration
- [x] Event handler variant: projects to Postgres AND writes KV
- [x] Query variant: KV hit → return; KV miss → Postgres → write KV → return
- [x] Demonstrate cache miss path explicitly in demo UI
      (`DELETE /api/shape-b/cache/...` + per-row Evict button, hit/miss badge)

### Phase 3 — Demo Frontend

- [x] Scaffold Vue 3 app in `demos/01-dictionary/frontend/`
- [x] Install PrimeVue v4, create shared UniFi theme preset (Aura base + `--p-*` token overrides)
      (`shared/unifi-theme/` at repo root; dependency-free factory so both apps import the same file)
- [x] Side-by-side Shape A / Shape B panels (use `<DataTable size="small">`)
- [x] Create/Update entry form
- [x] KV watch → SSE → reactive panel updates (`GET /api/watch/{context}` → EventSource → Pinia)
- [x] Default to dark mode (`p-dark` class on `documentElement`)
- [x] Add frontend container to docker-compose.yml

### Phase 4 — Lab Shell

- [x] Scaffold Vue 3 + PrimeVue v4 in `lab-shell/`
- [x] Import shared UniFi theme preset (same file as demo frontend)
- [x] Demo menu page
- [x] Demo 01 intro page (content from README.md, imported `?raw` + rendered with marked)
- [x] "Launch demo" → new tab

### Phase 5 — Data-Flow Vertical Layout Redesign

Restructure the demo frontend so the screen layout maps top-to-bottom to the data pipeline:
Command → JetStream → KV projections → KV watch stream.

- [x] Backend: add `GET /api/jetstream/watch` SSE endpoint (ephemeral ordered consumer on `DICTIONARY.*`, `DeliverNew` policy)
  - `rest/handlers.go` — add `js jetstream.JetStream` field + param + new route
  - `rest/sse.go` — add `jsEvent` struct + `watchJetStream` handler
  - `composition.go` — pass `mono.JS()` to `rest.NewHandlers`
- [x] Frontend: reorder `App.vue` sections (EntryForm → JetStreamPanel → panels → EventLog)
- [x] New `components/JetStreamPanel.vue` — live NATS subject/seq/payload feed via `/api/jetstream/watch`
- [x] `components/ShapePanel.vue` — Shape B: add Postgres projection sub-table below KV cache rows
- [x] `src/api.js` — add `listShapeB(context)` → `GET /api/shape-b/entries/{context}`
- [x] `components/EventLog.vue` — add filter bar: Shape (All/A/B), Op (All/PUT/DEL/PURGE), Key text search

> Full implementation detail in `.claude/plans/shiny-skipping-flask.md`

### Phase 6 — Shipping Domain + Shape C (Event Sourcing Reconstruction)

**Domain reference:** Martin Fowler — Event Sourcing: https://martinfowler.com/eaaDev/EventSourcing.html
(Ship → Port → Cargo as the domain subject)

**Structural reference:** Petrosyan — CQRS and Event Sourcing in Go: https://medium.com/@stani.petrosyan/how-to-implement-cqrs-and-event-sourcing-pattern-in-go-fd47dc0afd80
(CommandBus → Handler → hydrate aggregate → validate → publish as the Go implementation pattern)

#### Motivation

Shape A and Shape B demonstrate event-driven CQRS: events flow into persistent projections (KV, Postgres) and reads go to those projections. What they don't show is the defining property of pure event sourcing: **the ability to reconstruct current state by replaying the event log alone, with no persistent read model**.

Shape C closes that gap. It also introduces a domain with real business rules, where the command handler must know current aggregate state before accepting a command — a pattern not present in the generic dictionary domain.

#### Domain change — replace dictionary with shipping

The generic create/update entry form is replaced with a shipping operations panel. The infrastructure (JetStream, KV, Postgres, SSE) is unchanged; only the domain and UI fields change.

| Concept | Example values |
|---|---|
| Ship | "Orient Express", "Pacific Star" |
| Port | "Hamburg", "Rotterdam", "Singapore" |
| Cargo | "Electronics — 42 units", "Textiles — 180 units" |
| Events | `ship.arrived`, `ship.departed`, `cargo.loaded`, `cargo.unloaded` |

#### Domain rules (enforced before publishing to JetStream)

- A ship **cannot depart** a port it has not arrived at
- A ship **cannot arrive** at a port it is already at
- A ship **cannot load or unload cargo** unless it is currently docked in port

#### Architecture — ShipAggregate (Petrosyan's CommandBus pattern adapted for NATS JetStream)

The key structural difference from the Medium article: the event store is **NATS JetStream**, not an in-memory store. Aggregate hydration replays events from JetStream filtered by ship ID. Otherwise the pattern is identical.

The flow for each command:

1. Command arrives via REST (e.g. `DepartPort{shipID: "orient-express", port: "Hamburg"}`)
2. Command handler creates a blank `ShipAggregate` for that ship ID
3. Handler calls `aggregate.Hydrate(ctx, js, shipID)` — replays all events for that ship from JetStream to rebuild current state (current port, cargo manifest)
4. Handler calls `aggregate.Depart(port)` — validates the rule, returns the new event or an error
5. On success, handler publishes the event to JetStream

The same `ShipAggregate.Hydrate` + `Apply` logic is used by both the command handlers (write side) and Shape C's fleet query (read side). This is the key insight: **one aggregate, two uses**.

#### Shape C — pure event sourcing read model

- No KV bucket, no Postgres table
- `GET /api/shape-c/fleet` replays the full stream from `seq=1`, builds a `ShipAggregate` per ship by calling `Apply` for each event, returns the current fleet state
- Sits alongside Shape A and Shape B in the panels grid
- Makes the reconstruction property visible: clear the KV / Postgres data, hit the endpoint — Shape C still returns correct state from the event log alone

#### Frontend changes

- **Shipping Operations panel** replaces `EntryForm.vue` — operation selector (Arrive / Depart / Load / Unload) with contextual fields per operation; domain rule violations shown as inline error messages (not just toasts)
- **Shape C panel** added to the panels grid alongside A and B — fleet table: ship, current port, cargo manifest
- JetStream panel Stream tab now shows the full shipping event history

#### Files to add / modify

| File | Change |
|---|---|
| `backend/dictionary/internal/domain/ship.go` | `ShipAggregate`: state (shipID, currentPort, cargo list), `Hydrate(ctx, js, shipID)`, `Apply(event)`, command methods `Arrive / Depart / LoadCargo / UnloadCargo` each returning a domain event or error |
| `backend/dictionary/internal/domain/events.go` | Replace entry events with: `ShipArrived`, `ShipDeparted`, `CargoLoaded`, `CargoUnloaded`; update `StreamSubjects()` |
| `backend/dictionary/internal/application/commands/ship.go` | Four command handlers (one per operation); each hydrates the aggregate, calls the domain method, publishes on success |
| `backend/dictionary/internal/application/queries/shape_c.go` | `ReconstructFleet(ctx, js)`: iterates all stream messages, routes each to the correct `ShipAggregate` via `Apply`, returns `[]ShipState` |
| `backend/dictionary/internal/rest/handlers.go` | Replace entry routes with ship command routes; add `GET /api/shape-c/fleet`; remove Shape A/B list routes that depended on entry domain (KV watch still works — shape A/B projectors just project ship events now) |
| `backend/dictionary/internal/eventhandler/handler.go` | Update to consume new event subjects |
| `backend/dictionary/composition.go` | Wire new command handlers and Shape C query |
| `frontend/src/components/ShippingForm.vue` | New — replaces `EntryForm.vue`; operation selector + contextual fields + inline validation errors |
| `frontend/src/components/ShapeCPanel.vue` | New — fleet state table (ship, current port, cargo) polled from `GET /api/shape-c/fleet` |
| `frontend/src/App.vue` | Replace `<EntryForm />` with `<ShippingForm />`; add `<ShapeCPanel />` to panels grid |
| `frontend/src/api.js` | Replace entry functions with ship command functions; add `getFleet()` |
| `frontend/src/stores/dictionary.js` | Update KV watch event shape to match ship projection keys |

#### Checklist

- [x] Backend: `domain/ship.go` — `ShipAggregate` with `Hydrate`, `Apply`, and four rule-enforcing command methods
- [x] Backend: `domain/events.go` — replace entry event types with four shipping events; update `StreamSubjects()`
- [x] Backend: `application/commands/commands.go` — four handlers, each hydrates aggregate before publishing
- [x] Backend: `application/queries/shape_c.go` — `ReconstructFleet` via full stream replay
- [x] Backend: `rest/handlers.go` — new ship command routes + `GET /api/shape-c/fleet`
- [x] Backend: `eventhandler/handler.go` — consume updated subjects
- [x] Backend: `composition.go` — wire new handlers and Shape C query
- [x] Backend: `go build ./...` + `go test ./...` green
- [x] Frontend: `ShippingForm.vue` — operation selector, contextual fields, inline error display
- [x] Frontend: `ShapeCPanel.vue` — fleet table, reconstructed on demand from `/api/shape-c/fleet`
- [x] Frontend: `App.vue` — swap form, add Shape C panel
- [x] Frontend: `api.js` — ship commands + `getFleet`
- [x] Frontend: `npm run build` green
- [x] Frontend: `stores/dictionary.js` — track `seenPorts` (unique, sorted); updated in `applyWatchEvent` from `event.value.currentPort` on every PUT event
- [x] Frontend: `ShippingForm.vue` — `portOptions` computed merges static `BASE_PORTS` with `store.seenPorts`; port dropdown auto-populates with any port seen in the event source that is not already in the static list

### Phase 7 — Swagger / OpenAPI + Ginkgo Test Runner

Add self-documenting API support using `swaggo/swag` so the backend routes are explorable without reading source code. Also establishes Ginkgo as the canonical test runner to support the red→green→refactor cycle for all subsequent phases.

**Swagger approach:** annotate existing handlers with `swaggo` doc comments; `swag init` generates an OpenAPI 2.0 spec; serve Swagger UI at `/swagger/` via `swaggo/http-swagger`.

**Ginkgo approach:** `github.com/onsi/ginkgo/v2` with Gomega matchers. A custom `ReportAfterSuite` in `dictionary/suite_test.go` prints a spec tree grouping results under their `Describe` / `Context` nodes, mirroring the `BUSINESS_RULES.md` structure. For each subsequent phase: write specs first (one `Context` per rule, one `It` per assertion), run `ginkgo ./...` to confirm red, implement, confirm green.

#### Checklist

- [x] Backend: `go get github.com/swaggo/swag` + `go get github.com/swaggo/http-swagger`
- [x] Backend: annotate all existing handlers in `rest/handlers.go` and `rest/sse.go` with `swaggo` comments (summary, params, responses)
- [x] Backend: run `swag init` from `backend/` to generate `docs/` package
- [x] Backend: mount Swagger UI at `/swagger/` in `handlers.go` via `httpSwagger.Handler`
- [x] Backend: add `swag init` step to Dockerfile so the spec stays in sync with handlers
- [x] Backend: `go get github.com/onsi/ginkgo/v2` + `go get github.com/onsi/gomega`; install `ginkgo` CLI
- [x] Backend: rewrite `dictionary/integration_test.go` in Ginkgo DSL (`Describe` / `Context` / `It` / `By` / `BeforeEach`)
- [x] Backend: `dictionary/suite_test.go` — bootstrap + `ReportAfterSuite` tree reporter
- [x] Verify Swagger UI accessible at `http://localhost:18080/swagger/` (verified live 2026-07-09 once Docker was installed)

---

### Phase 8 — Two-Aggregate Domain + Terminal + Port Frontend (single stream)

#### Overview

Introduces the `Container` domain entity (a second aggregate alongside `Ship`), the terminal/port model, and a purpose-built Port Management frontend — all on a **single JetStream stream**. This is the baseline: two aggregates sharing one consistency boundary, so every cross-aggregate rule (BR-008…BR-012) is enforced with **strong consistency from a single atomic replay**. Phase 16 then splits the stream to expose the distributed-consistency problem.

> **Why single-stream first.** The invariant-spanning-aggregates problem comes from `Ship` and `Container` being *separate aggregates* — not from stream topology. Keeping both aggregates on one stream in Phase 8 means a command handler hydrates **both** from one replay of `SHIPPING` (folding `ship.*` into `ShipAggregate`, `container.*` into `ContainerAggregate`), so cross-aggregate rules stay locally consistent. Phase 16 changes exactly one variable — the stream split — turning the same invariant into a distributed problem. This isolation is the teaching point.

#### Terminology

- **Terminal** (not warehouse) — the facility at a port where containers are stored in the yard and crane-loaded onto ships. Every port has a terminal.
- **Container** — ISO 6346 shipping container (e.g. `TCKU1234567`), the unit of cargo transport.

#### Aggregate design (the decision that makes Phase 16 a clean delta)

- `Container` is its **own aggregate** (`ContainerAggregate`), **not** folded into `ShipAggregate`. A container's lifecycle (`registered in terminal → loaded → unloaded at destination`) means it belongs to no ship while it sits in the yard, so it cannot be a field on the ship aggregate the way `Cargo` is today.
- Both aggregate types are **co-located on the single `SHIPPING` stream**, partitioned by subject:

| Stream | Subjects | Aggregate |
|---|---|---|
| `SHIPPING` | `SHIPPING.ship.arrived`, `SHIPPING.ship.departed` | `ShipAggregate` |
| `SHIPPING` | `SHIPPING.container.registered`, `SHIPPING.container.loaded`, `SHIPPING.container.unloaded` | `ContainerAggregate` |

The rename `DICTIONARY` → `SHIPPING` happens here (the old name is a legacy misnomer — the events have always been shipping events). It is a breaking change to all subject names and stream references across backend and frontend.

The legacy `Cargo` value object on `ShipAggregate` is retired; a ship's manifest is now derived by joining containers whose `onShipID == shipID`.

#### Container model

```
Container
  id            string   — ISO 6346 format (e.g. TCKU1234567)
  cargo         string   — description of contents
  originPort    string
  destPort      string   — cannot be loaded at this port; can only be unloaded here
  status        enum     — in-terminal | on-ship
  terminalPort  *string  — set when status == in-terminal (the yard the container is in); nil otherwise
  onShipID      *string  — set when status == on-ship (the ship carrying it); nil otherwise
```

> Location is modelled as two explicit nullable fields rather than one overloaded string, so queries never have to branch on `status` to interpret it: `ListByPort` filters `terminalPort`, `ListByShip` (and the ship manifest join) filters `onShipID`. Exactly one is non-nil at any time.

#### Business rules

BR-004 and BR-005 (cargo load/unload while at sea) are retired and replaced by BR-012. BR-006 and BR-007 (ship-cargo manifest rules) are retired and replaced by container-specific rules.

All of these are enforced **from a single replay of the `SHIPPING` stream** — the command handler hydrates the relevant `ShipAggregate` and `ContainerAggregate` together, so cross-aggregate checks (BR-008, BR-012) are strongly consistent in this phase.

| Rule | Description | Error | Needs |
|---|---|---|---|
| BR-008 | A container cannot be loaded if its destination port matches the ship's current port | `ErrContainerAtDestination` | ship + container |
| BR-009 | A container can only be unloaded at its destination port | `ErrWrongDestination` | container |
| BR-010 | A container must be `in-terminal` to be loaded | `ErrContainerNotInTerminal` | container |
| BR-011 | A container must be `on-ship` to be unloaded | `ErrContainerNotOnShip` | container |
| BR-012 | A ship must be docked to load or unload containers | `ErrNotInPort` (reused) | ship |
| BR-013 | A container can only be unloaded from the ship it is actually on (`onShipID == shipID`) | `ErrWrongShip` | container |
| BR-014 | A container can only be loaded when the ship is docked at the container's terminal port (`terminalPort == ship.currentPort`) | `ErrContainerNotAtPort` | ship + container |
| BR-015 | A container ID can only be registered once | `ErrContainerExists` | container |
| BR-016 | A container ID must be in ISO 6346 format: `TCKU` + 7 digits | `ErrInvalidContainerID` | container |

#### Frontend split

| Frontend | Role | Dev port |
|---|---|---|
| `frontend/` (existing) | Admin / NATS debug view — raw streams, KV buckets, Shape A/B/C projections | 5173 |
| `frontend-port/` (new) | Port Management — terminal view, ship manifest, arrivals/departures, container operations | 5174 |

The Port Management frontend is scoped to one port at a time (port selector in topbar).

#### Checklist

**Backend — stream rename (`DICTIONARY` → `SHIPPING`)**
- [x] `internal/jstream/stream.go` — create the `SHIPPING` stream on startup (single stream, subjects `SHIPPING.>`); remove `DICTIONARY`
- [x] `domain/events.go` — rename all subject constants from `DICTIONARY.*` to `SHIPPING.*`
- [x] `eventhandler/handler.go` — update consumer subject filters to `SHIPPING.*`
- [x] `rest/handlers.go` / `rest/sse.go` — update subject and stream-name references (swagger regenerated)
- [x] Frontend (`frontend/`): update SSE watch + JetStream panel references from `DICTIONARY.*` to `SHIPPING.*`; stream selector option `DICTIONARY` → `SHIPPING`; removed dead `EntryForm.vue`
- [x] `go build ./...` + `ginkgo ./...` green after rename (11/11 specs — green checkpoint before any container work)

**Backend — container aggregate (same `SHIPPING` stream)**
- [x] `domain/container.go` — `Container` entity, `ContainerAggregate` with `Apply` / `Register` / `Load` / `Unload`; domain errors BR-008 to BR-015
- [x] `domain/ship.go` — retire the `Cargo` value object and the cargo command methods (`LoadCargo`/`UnloadCargo`) and their events
- [x] `domain/events.go` — add `SHIPPING.container.registered/loaded/unloaded` subjects; remove `cargo.*` subjects
- [x] `application/commands/container.go` — `RegisterContainer`, `LoadContainer`, `UnloadContainer`; `Load`/`Unload` hydrate **both** `ShipAggregate` and `ContainerAggregate` from one `SHIPPING` replay (`hydratePair`), validate, then publish
- [x] `application/queries/terminal.go` — `ListByPort(port)` (filters `terminalPort`), `ListByShip(shipID)` (filters `onShipID`; this is also the ship manifest)
- [x] `application/queries/shape_c.go` — extend `ReconstructFleet` to fold `container.*` events too, so reconstructed ships carry their container manifests
- [x] `postgres/` — `containers` table migration + `ContainerRepository` (upsert on each event); `ships.cargo` column dropped
- [x] `eventhandler/` — container projector (`container-projector`) for `container.*` events into the `container-{context}` KV bucket + Postgres
- [x] `rest/handlers.go` — container command routes (`POST /api/containers/register`, `/load`, `/unload`) + terminal query routes (`GET /api/terminal/{context}/{port}`, `GET /api/manifest/{context}/{shipID}`, `GET /api/containers/{context}`)
- [x] `composition.go` — wire container handlers, projector, new KV buckets
- [x] Ginkgo specs — one `Context` per BR-008 to BR-015, written before implementation (red confirmed); `ginkgo ./...` green (22/22)

**Backend — metadata projections (`meta.*`)**

The port selector in the Port Management frontend needs to show all ports ever seen — not just ports with ships currently docked. Rather than accumulating this client-side (the current `seenPorts` approach), it is projected into a `meta.known-ports` KV key and seeded from a REST endpoint on connect.

The working superset of KV namespaces:

| Namespace | Purpose | Status |
|---|---|---|
| `ship.*` | Per-ship current state (Shape A/B projections) | carried forward |
| `container.*` | Per-container current state (Shape A/B projection) | this phase |
| `meta.*` | Cross-cutting derived lookup sets (known ports, known container IDs) | this phase |
| `locale.*` | Localisation config per context | future |
| `tenant.*` | Tenant-specific configuration | future |

- [x] Backend: create `meta-{context}` KV bucket in `composition.go`
- [x] Backend: `eventhandler/meta_handler.go` — on each `ship.arrived` / `ship.departed`, maintain `meta.known-ports` (JSON array, sorted); on each `container.registered`, merge origin + destination ports into `known-ports` and the ID into `meta.known-containers`
- [x] Backend: `rest/handlers.go` — `GET /api/meta/{context}/known-ports`, `GET /api/meta/{context}/known-containers`; plus `GET /api/watch-terminal/{context}` SSE (container + meta buckets)

**Frontend — Port Management (new `frontend-port/`)**
- [x] Scaffold Vue 3 + PrimeVue v4, import shared UniFi theme (dev port 5174)
- [x] Port selector (topbar) — seeded from `known-ports` on `connect()` before SSE opens, then merges live `META` events; drives all panels
- [x] Terminal panel — containers in yard: ID, cargo, origin, destination
- [x] Ships at port panel — docked ships with container manifests (client-side join by `onShipID == shipID`)
- [x] Operations panel — Arrive / Depart / Register Container / Load Container / Unload Container, all scoped to the selected port _(superseded by the UX-refinement block below: this standalone panel was removed and its operations localized onto the Terminal Yard / Ships at Port panels)_
- [x] Inline rule violation feedback (BR-008 to BR-015 error messages)
- [x] SSE watch — `/api/watch/{context}` (ships) + `/api/watch-terminal/{context}` (containers + meta)

**Frontend — Port Management UX refinement (panel-localized operations)**

Follow-up to the initial Port Management scaffold: operations move off a
standalone panel and onto the panel whose data they act on, and a new port
can be added directly from the topbar instead of only appearing after an
event references it.

- [x] Topbar — `+` icon button next to the Port `<Select>`; opens a popup (`Dialog`) capturing a new port name (labelled "Add", with a "staged in this session only" note)
- [x] `port.js` store — `addShippingPort(port)` adds the name to `knownPorts` and makes it active; named `add*` (not `register*`) to avoid colliding with the event-publishing command verb, and documented as client-side only (no backend Port aggregate — the port becomes durable in `meta.known-ports` once a real ship arrival or container registration references it)
- [x] `TerminalPanel.vue` — "Register container" (popup) + inline "Load container" row, above the yard `DataTable`
- [x] `ShipsAtPortPanel.vue` — inline "Ship arrives" / "Ship departs" / "Unload container" rows, above the ships `DataTable`
- [x] Both panels gate their operations on a selected port (`v-if="store.port"` with a fallback prompt) — restores the guard the removed `OperationsPanel` had; without it Arrive sent `port:""` and returned a nonsensical `ErrAlreadyDocked`
- [x] Removed `OperationsPanel.vue` and its use in `App.vue` — no more standalone "Operations — select a port" panel
- [x] `npm run build` + lint green (0 errors)

**Frontend — Admin (existing `frontend/`)**
- [x] ShippingForm — cargo ops replaced by Register / Load / Unload container
- [x] ShapePanel — cargo column removed; ShapeCPanel — manifest column + reconstructed containers table
- [x] Pinia store — `seenPorts` now seeded from `known-ports` (in-memory-only limitation fixed)

**Docker**
- [x] `docker-compose.yml` — add `frontend-port` service on host port 5174
- [x] `frontend-port/Dockerfile` — nginx serving production build (code-reviewed; Docker not installed on dev machine)

**Documentation**
- [x] `ARCHITECTURE.md` — update stream design (single `SHIPPING` stream, two aggregates); add container domain and `meta.*` KV namespace
- [x] `BUSINESS_RULES.md` — retire BR-004 to BR-007; add BR-008 to BR-015
- [x] `README.md` — two-aggregate overview, two-frontend table, service table with `frontend-port` entry

---

### Phase 8.2 — Ship Management Split View, Fleet Panel, Yard Split, BR-016

**Branch:** `poc/dictionary1.8.2`

#### Overview

Follow-up work on top of the Phase 8 baseline, kicked off by this request:

> I'd like to update the plan - phase 8 with the following:
> On Port Management UI:
> - Split the UI into 2 vertical group panels:
>    - Ships at sea or in-transit (you can choose best header title)
>       - A table showing all ship in transit
>    - Port Management
>       - Existing functionality with only change is to move the port selection dropdown into this panel
>
> The main title for the UI should be Ship Management

All of it is scoped to `frontend-port/` plus one small backend rule (BR-016);
no other backend, domain, or business-rule change beyond what's called out
below.

A first draft of the split defined "in transit" as `store.ships` minus
`store.dockedShips` — caught in review as wrong, since it would silently
exclude ships docked at a port other than the one currently selected. That
gap led directly to the fleet-wide redesign below, so the two-group layout
described in the request above was never shipped as originally worded — it
was superseded by a single fleet-wide panel before implementation.

Also confirmed (no code change, informational only): none of this UI work
needed backend changes for rule enforcement — the domain aggregates
re-derive state from the event stream on every command regardless of which
frontend (or a raw API call) issued it, so there is no way for a second UI
to bypass BR-008 etc. by supplying different client-side state.

#### Ship Management split view + Fleet panel

Restructure `frontend-port/` from a single row of two port-scoped panels
into two stacked **group panels** — a port-independent, fleet-wide "Fleet"
group above the existing port-scoped "Port Management" group — and rename
the page to reflect that it now spans both.

Docked-vs-in-transit is derived from the ship model (`domain/ship.go`): a
ship is at sea when `currentPort === ''` (projected `status === 'in-transit'`);
otherwise it is `docked` at `currentPort`. The Fleet panel lists **every** ship
in the context and filters on this client-side, so a ship docked at a
non-selected port is still visible there (no blind spot) — the original
"in-transit-only" design from the request above would have left such ships
invisible, which is why it was replaced with a fleet-wide filterable panel
(`all` / `docked` / `in-transit`) instead of the originally-requested
two-group split.

- [x] `App.vue` — title "Port Management" → "Ship Management"; subtitle now "fleet overview · terminal yard · docked ships · container operations"; single `.panels` grid replaced with two stacked `.group` sections
- [x] `port.js` store — added an `allShips` getter (`Object.values(state.ships)` sorted by `shipID`); fleet-scoped implicitly (the store already only holds the current context's ships). Docked/in-transit is filtered in the panel, not the store
- [x] New `FleetPanel.vue` — a `.lab-panel` with a `Select` status filter (`All` / `Docked` / `In transit`, default `All`) over `store.allShips`; columns: Ship ID (monospace), Name, Status (`Tag`: Docked=success / In transit=info), Port (`currentPort` or "at sea"), and manifest count (`store.manifestFor(shipID).length` — a ship at sea still carries its loaded containers via `onShipID`). Not gated on `store.port`. Empty state: "No ships match this filter." Read-only — no operations
- [x] "Port Management" group — a `.group` wrapper (`<h2>` heading "Port Management" + the port `<Select>`, `editable`, and the `+` "Add port" button/`Dialog`, all moved out of the topbar) enclosing the existing `TerminalPanel.vue` and `ShipsAtPortPanel.vue` in their current 2-column grid, unchanged
- [x] Topbar — keeps the Fleet (context) `<Select>`, connection `Tag`, and theme toggle (both groups are fleet-scoped); the Port `<Select>` + `+` button moved into the Port Management group
- [x] Port-gating still holds: the two port-scoped panels keep their `v-if="store.port"` fallback; the Fleet panel renders regardless of selection
- [x] `npm run build` + lint green (0 errors)

#### Terminal Yard split (Outbound / Arrived)

Splits the single yard `DataTable` in `TerminalPanel.vue` into two: Outbound
(`destPort !== store.port`) and Arrived (`destPort === store.port`, i.e. the
container has reached its destination terminal — the domain has no separate
"delivered" status, this is a client-side view of the same `in-terminal`
containers). Client-side filter over the existing `store.yardContainers`
getter — no store, query, or backend change.

- [x] `TerminalPanel.vue` — `outboundContainers` / `arrivedContainers` computed filters; two `DataTable`s with their own empty states, replacing the single yard table
- [x] Load dropdown narrowed to `outboundContainers` only — loading an arrived container is always rejected by BR-008, so offering it was a guaranteed-fail UX trap
- [x] `npm run build` + lint green (0 errors)

#### BR-016: container ID format (TCKU + 7 digits)

Formal domain rule: a container ID must match ISO 6346 shape with a fixed
`TCKU` owner prefix (case-sensitive) + exactly 7 digits, e.g. `TCKU1234567`.
Enforced server-side (source of truth) with client-side validation in
`frontend-port/` for fast feedback; the admin debug frontend (`frontend/`)
is intentionally left as free-form raw input since it exists to exercise
the raw API.

- [x] `domain/container.go` — `ErrInvalidContainerID` + `containerIDPattern` (`^TCKU[0-9]{7}$`); checked first in `ContainerAggregate.Register()`, before BR-015's already-registered check
- [x] `rest/handlers.go` — `ErrInvalidContainerID` mapped to 422 alongside the other container domain errors; swagger doc comment on `registerContainer` updated to mention BR-016 (`docs/docs.go` / `swagger.json` / `swagger.yaml` hand-patched — `swag init` regeneration produced a large unrelated `$ref`-naming diff from a swag version/config mismatch, so it was reverted in favor of a targeted string edit)
- [x] Ginkgo spec — `Container Domain Rules / BR-016`, two cases (wrong prefix, wrong digit count); confirmed all existing test container IDs already matched `TCKU` + 7 digits before adding the rule, so no other spec needed updating
- [x] `frontend-port/TerminalPanel.vue` — client-side `CONTAINER_ID_PATTERN` mirrors the domain regex; Register button disabled until valid; inline hint shown once the field is non-empty and invalid
- [x] `BUSINESS_RULES.md` — BR-016 entry added
- [x] Phase 8 container-rules table — BR-016 row added
- [x] `go build ./...` + `ginkgo ./...` green (24/24) + `npm run build` + lint green (0 errors)

---

### Phase 8.3 — Surrogate Key (UUID) for Container

#### Overview

Follow-up to fixing `TCKU0001` by hand in Phase 8.2: rather than building a
`container.id-corrected` corrective-event pattern to handle natural-key
mistakes after the fact, adopt an immutable **surrogate key (UUID)** as
`Container`'s true aggregate identity now, while the POC is still small and
before the design becomes load-bearing across the backend and both
frontends. This is the "design it in from day one" side of the
surrogate-vs-corrective-event tradeoff — cheap here, expensive as a later
retrofit on a real (V3) system.

**Scope: `Container` only, not `Ship`.** See design rationale below.

Because this changes what `ContainerAggregate` folds by (and the `containers`
table's primary key), existing event history and the old Postgres schema are not
compatible without a backfill migration. For a POC the reset is cheaper than the
migration — a `docker compose down -v` clears `nats-data` and `pg-data` so
Container aggregates adopt UUID identity from the next registration, no backfill
needed. (Ship data is lost too since it shares the stream/volumes, but `Ship`
itself is unchanged — only test data is lost, not the model.) This is tracked as
the single operational step in the checklist below.

#### Design rationale — Container gets a surrogate key, Ship does not

| | `Container` | `Ship` |
|---|---|---|
| Natural key | Container ID, ISO 6346 format (BR-016: `TCKU` + 7 digits) | Ship ID — an internal slug/handle (e.g. `my-ship`) |
| External interchange standard? | Yes — ISO 6346 exists specifically so *other systems* recognize the same container by this ID | No — no equivalent business rule constrains or standardizes it for external use |
| Correction/rename risk | Real — this is exactly what happened with `TCKU0001` | Low — nothing today forces or expects a ship ID to be corrected |
| Identity model | Surrogate key (UUID) = identity; container ID = mutable natural-key attribute | Natural key stays the identity — no surrogate needed |

The general lesson (captured in the personal Event Sourcing notes this
session produced) is: adopt a surrogate key where the natural key is an
external interchange standard that a system doesn't fully control and may
need to correct. `Ship` doesn't have that pressure, so adding a surrogate
key there would be indirection without a matching benefit — this phase
does **not** touch `Ship`.

#### Design

```
Container
  id            string   — surrogate key (UUID), assigned at Register(); this is what
                            Hydrate/Apply fold by — never changes after creation
  containerID   string   — natural key, ISO 6346 format (BR-016); mutable attribute,
                            enforced unique via the new natural-key index (not by folding)
  cargo         string
  originPort    string
  destPort      string
  status        enum     — in-terminal | on-ship
  terminalPort  *string
  onShipID      *string
```

A new `container-index-{context}` KV bucket (or a unique Postgres column,
depending on final implementation choice) maps `containerID -> id (UUID)`,
maintained by the container-projector on every `container.registered`
event. This index is **load-bearing**, not optional — every natural-key
route or command has to resolve through it before it can hydrate or query
by the surrogate key (see the "indexing cost" discussion below).

Public API surface stays **natural-key addressed**
(`/api/containers/{containerID}`, `LoadContainer(containerID, ...)`) — per
the earlier decision that ISO 6346 is meant to be the external interchange
key. Response bodies gain an `id` (UUID) field alongside `containerID` for
callers that want a stable reference across a future correction.

> **Two implementation refinements vs. the design sketch above**, both to keep
> BR-015 strongly consistent:
> 1. **No separate index bucket.** The command side resolves `containerID -> id`
>    from the **event stream replay** (authoritative), not from an
>    eventually-consistent read projection. So there is no `container-index-*`
>    KV bucket; the natural-key "index" is (a) the stream itself on the write
>    side and (b) the `UNIQUE (context, container_id)` Postgres column on the
>    read side. This sidesteps the stale-read hazard the notes flagged for
>    validating a write against a read model.
> 2. **BR-015 stays a domain rule, unchanged in spirit.** `RegisterContainer`
>    hydrates the container by natural key (`hydrateByNaturalKey`) so the domain
>    still sees `c.registered == true` and rejects the duplicate in
>    `ContainerAggregate.Register()` — the rule did *not* move into the handler.

#### Checklist

**Backend**
- [x] `domain/container.go` — `ID` (UUID) added to `ContainerAggregate` + `ContainerState`; `Apply`/`State`/`FromState` carry it; `Register`/`Load`/`Unload` emit it; `IsRegistered()` exposed for the mint-decision
- [x] `domain/events.go` — `ContainerEvent` gains an `id` field alongside `containerID`, carried on every container event
- [x] `application/commands/container.go` — `RegisterContainer` mints a dependency-free UUID v4 (`newSurrogateID`) after `hydrateByNaturalKey` confirms the natural key is free; `hydratePair` (Load/Unload) resolves `containerID -> id` from the `.registered` event, then folds strictly by `id`
- [x] Natural-key resolution: chosen mechanism is the **event-stream replay** on the write side + the `UNIQUE (context, container_id)` Postgres column on the read side (no separate `container-index-*` KV bucket — see refinement note above)
- [x] `postgres/migrate.go` — `containers` PK is now `(context, id)`; `container_id` carries `UNIQUE (context, container_id)`; `container_repository.go` upserts on the surrogate key
- [x] `eventhandler/container_handler.go` — projector carries `id` end to end (`currentContainerAgg` seeds it); KV read model stays keyed by `container.{containerID}` and carries `id` as a field (doubles as the natural-key lookup)
- [x] `rest/handlers.go` — routes stay natural-key addressed; `ContainerState` responses include `id` automatically; swagger `ContainerState` definition hand-patched (docs.go / swagger.json / swagger.yaml) to add the field without a `swag init` regen; BR-015 confirmed still rejecting duplicates via a Ginkgo spec
- [x] `go build ./...` + `go vet` + `gofmt` + `ginkgo ./...` green (50/50; 3 new surrogate-key specs)

**Frontend**
- [x] `frontend-port/` — confirmed no change required (panels key/display by `containerID`; the new `id` field is additive and ignored)
- [ ] `frontend/` (admin debug UI) — optional, skipped: could show `id` alongside `containerID` in Shape A/B/C panels; deferred as non-essential
- [x] Neither frontend touched, so no rebuild needed (backward-compatible JSON addition)

**Documentation**
- [x] `ARCHITECTURE.md` — new "Container identity — surrogate key (Phase 8.3)" subsection: fold-by-id, Postgres PK/UNIQUE, KV read model, and why `Ship` is out of scope; aggregate-rules range bumped to BR-016
- [x] `BUSINESS_RULES.md` — BR-015 enforcement note updated (natural-key resolution against the stream; rule stays in the domain)

**Operational (run when bringing the live stack onto the new schema)**
- [ ] `docker compose down -v` — the running Postgres still has the old `containers` schema (PK `(context, container_id)`, no `id` column); `CREATE TABLE IF NOT EXISTS` won't alter it, so a volume reset is required before the backend can upsert on `(context, id)`. Left for the user to run (destructive — wipes the current dev data).

---

### Phase 9 — Subject Taxonomy + Doc Realignment

#### Goal

Move tenant, region, and aggregate identity out of the JSON payload and into subject tokens, adopting the target production scheme:

```
{region}.events.{tenant}.{aggregate}.{id}.{event}
e.g.  emea.events.acme.ship.SH-001.arrived
      emea.events.acme.container.9f3c…uuid….loaded
```

Region and tenant are **hardcoded constants for the POC** (`emea`, `acme`) — the point is that the subject *shape* is right from here on, because subject taxonomy is the highest-cost-to-change axis in the whole system and every later phase multiplies the number of subjects and consumers built on it.

#### Why this phase must precede Phase 14

`Nats-Expected-Last-Subject-Sequence` (Phase 14's optimistic-concurrency guard) is scoped **per subject**. With today's `SHIPPING.ship.arrived` shape, every ship shares one subject — the guard would serialize the entire fleet. The aggregate-instance `{id}` token is what makes per-aggregate concurrency control possible at all.

#### Design notes

- Stream name stays `SHIPPING`; it now binds `{region}.events.{tenant}.ship.>` and `{region}.events.{tenant}.container.>` (until Phase 16 moves the container binding to `TERMINAL`).
- The `{id}` token is the **aggregate identity**: `shipID` for ships, the **surrogate UUID** (Phase 8.3) for containers — not the ISO 6346 natural key.
- Subject constants in `domain/events.go` become **builder functions** (`ShipSubject(region, tenant, shipID, event)`), since subjects are now parameterized.
- Consumers/queries that switch on the full subject string must instead **parse tokens** (aggregate + event type by position).
- Hydrating a single ship can now use a **filtered consumer** on `{region}.events.{tenant}.ship.{id}.>` instead of folding the whole stream — a replay-cost win that Phase 16 can measure. Natural-key container lookup still scans `…container.>`.
- The `Context` payload field stays for now (KV bucket naming still uses it); subjects become the authoritative scoping mechanism. Whether `context` collapses into `{tenant}`/`{region}` is decided in Phase 17.

#### Checklist

- [x] `domain/events.go` — replace subject constants with builder functions; add hardcoded `Region = "emea"`, `Tenant = "acme"` constants; update wildcards
- [x] `internal/jstream/stream.go` — stream binds the new subject filters
- [x] `application/commands/` — publish to per-instance subjects; hydrate via filtered subject where the aggregate ID is known
- [x] `eventhandler/`, `queries/`, `rest/sse.go` — update filter subjects; parse event type from subject tokens
- [x] Frontend JetStream panel — subject display/filtering updated for the new shape
- [x] Docs realignment (same commit): fix `CLAUDE.md` dictionary-domain drift (package layout, entities); update stale "Phase 9 = stream split" references in `ARCHITECTURE.md`, `BUSINESS_RULES.md`, and code comments (`events.go`, `container.go`) to Phase 14
- [x] `go build ./...` + `ginkgo ./...` green (52/52 tests, including subject-taxonomy tests)

---

### Phase 9.5 — Ports Reference Table (BR-017, BR-018)

#### Goal

Replace the frontend-only port list (hardcoded `BASE_PORTS` in `ShippingForm.vue`; client-side-only `addShippingPort` in the `frontend-port` port store) with a real Postgres-backed reference table, registered via a REST API. Ports are plain master data — a direct Postgres write, not an event-sourced aggregate — since a port has no lifecycle worth replaying; it exists only to be looked up when enforcing BR-017/BR-018. This also retires the derived `meta.known-ports` KV projection, which is now redundant with the registry.

#### Checklist

- [x] `domain/repository.go` — `PortRepository` interface (`Exists`, `Register`, `List`)
- [x] `domain/ship.go` / `container.go` — `ErrUnknownPort`; `ShipAggregate.Arrive()` takes `portKnown bool`; `ContainerAggregate.Register()` takes `originKnown, destKnown bool`
- [x] `postgres/migrate.go` — `ports` table (context-scoped); seeds the original 6 default ports for every fleet context so a fresh install still works out of the box
- [x] `postgres/port_repository.go` — `PortRepository` implementation
- [x] `application/commands/port.go` — `PortHandler` (direct repo write/read, no JetStream)
- [x] `ShipHandler` / `ContainerHandler` — resolve port existence via `PortRepository.Exists()` before calling `Arrive`/`Register`
- [x] `rest/handlers.go` — `GET /api/ports/{context}`, `POST /api/ports`; removed `GET /api/meta/{context}/known-ports`
- [x] `eventhandler/meta_handler.go` + `queries/meta.go` — retired `known-ports` (kept `known-containers`)
- [x] Frontend — both `api.js` clients: `getPorts`/`registerPort` replace `getKnownPorts`; `dictionary.js` and `port.js` stores seed from the new endpoint; `frontend-port`'s existing "add a shipping port" dialog now calls `POST /api/ports` instead of staging client-side only; `ShippingForm.vue`'s `BASE_PORTS` removed
- [x] `BUSINESS_RULES.md` — BR-017, BR-018
- [x] `go build ./...` + `ginkgo ./...` green (56/56 tests)

---

### Phase 9.6 — Postgres Tables Admin Panel (Reference Data → Ports)

#### Goal

Give the admin UI (`frontend/`, "EventSourcing CQRS POC") a tabbed panel for browsing raw Postgres table contents, grouped under headings — starting with a "Reference Data" group containing one table, Ports. Pairs with `JetStreamPanel` (raw event log) as the other "raw source" view, and is the concrete UI counterpart to the "Event Sourcing vs Plain CRUD" heuristic added to `ARCHITECTURE.md` in Phase 9.5: this panel shows a table with no event log at all.

#### Checklist

- [x] `domain/repository.go` — `PortRecord{Name, CreatedAt}`; `ListRecords` added to `PortRepository` interface
- [x] `postgres/port_repository.go` — `ListRecords` implementation (`SELECT name, created_at`)
- [x] `application/commands/port.go` — `PortHandler.ListRecords` forwards to the repo
- [x] `rest/handlers.go` — `GET /api/admin/ports/{context}` returns `{"rows": [{name, createdAt}]}`; distinct namespace (`/api/admin/...`) from the domain-facing `/api/ports/*`, reserved for future per-table admin views (e.g. ships/containers)
- [x] Tests — `fakePortRepo.ListRecords` (`integration_test.go`); new `admin — postgres tables` spec in `api_test.go`
- [x] Frontend — `api.js`: `getPortsTable(context)`; new `PostgresTablesPanel.vue` (collapsible, same header pattern as `JetStreamPanel`/`ShapeCPanel`; "Reference Data" group wrapping a `Tabs` with a Ports tab; manual refresh button, no live push channel since Postgres writes here aren't KV-watched); mounted in `App.vue` right after `JetStreamPanel`
- [x] `ARCHITECTURE.md` — new "Postgres Tables Panel (Admin UI)" subsection
- [x] `go build ./...` + `ginkgo ./...` green (57/57 tests); `npm run build` clean in `frontend/`

---

### Phase 10 — Performance Baseline (pull-forward, pre-Phase 11/15)

#### Goal

Establish a load-test **baseline on the current implementation**, before the write path and stream topology change in later phases. This is a scoped pull-forward of the full performance work (Phase 17) — **measurement only, no mitigations**.

Two known scalability gaps already exist and are this baseline's primary targets:

1. **Shape C — full replay on every call.** `ReconstructFleet` replays from `seq=1` every time. Latency grows linearly with stream depth.
2. **Write-side hydration — full replay per command.** `hydrate()` in `commands.go` replays all events for a ship on every command. A busy ship accumulates history and slows its own writes.

Both are correct implementations of event-sourcing fundamentals — the point is to *measure* the degradation curve, not fix it here.

#### Why pull this forward

1. **The harness is phase-independent.** k6 install, the seed script, the docker load environment, and the metrics-capture format are reused by every subsequent phase — building them now is pure upside.
2. **A clean pre-guard baseline is only obtainable now.** Phases 14–16 don't change the two gaps' fundamentals. Capturing command latency **before** Phase 14's optimistic-concurrency guard lands gives a before/after delta that answers Phase 17's question — "what does the sequence guard cost?" — and cannot be reconstructed later.

#### In scope (stable against Phases 13–15)

- k6 harness + seed script (reusable infrastructure)
- Shape C reconstruction latency vs stream depth
- Single-ship write-side hydration degradation
- Raw command-throughput ceiling (concurrent ships)

#### Explicitly deferred to Phase 17 (would be thrown away if measured now)

- Optimistic-concurrency contention → needs **Phase 14** (guard doesn't exist yet)
- Cross-stream burst / consumer lag → needs **Phase 16** (no `TERMINAL` stream yet)
- Cross-aggregate stale-read window → needs **Phase 16**

> **Measurement only.** This phase characterises the degradation curves; it does **not** implement mitigations (snapshotting, etc.), because those interact with Phases 14–16. Record results as a **partial pass** in `PERFORMANCE.md`, clearly separating captured baselines from pending (deferred) scenarios owned by Phase 17.

#### Tool

**k6** (`k6.io`) — scripted load testing in JavaScript, runs outside the Go stack, produces latency percentiles and throughput metrics. Alternatively `vegeta` for simpler HTTP load. The same harness is carried into Phase 17.

#### Checklist

Harness lives in [`demos/01-dictionary/perf/`](../../demos/01-dictionary/perf/README.md) (k6, env-overridable, targets the dockerized backend on `:18080`).

- [x] Choose load testing tool (k6) — install (`brew install k6`) still required on the run machine
- [x] Write k6 script: seed data (`perf/seed.js` — register containers, arrive ships) + shared lib (`perf/lib/`)
- [x] Scenario written: single-ship hydration degradation (`perf/scenarios/hydration-single-ship.js`) — latency bucketed by prior-event band
- [x] Scenario written: concurrent ships (`perf/scenarios/throughput-concurrent-ships.js`) — ramp to 500 VUs, p95 + error rate
- [x] Scenario written: Shape C reconstruction (`perf/scenarios/shape-c-reconstruction.js`) — latency sampled per stream depth
- [x] Create `demos/01-dictionary/PERFORMANCE.md` as a **partial pass**: baseline tables filled, deferred scenarios (Phases 11/15) separated
- [x] **Capture baselines** (2026-07-13, M3 Pro / dockerized): Shape C ~linear replay curve (0.9→45ms @100→10k); hydration per-command climbs 0.65→18ms @0→10k events; throughput ceiling ≈3,800 cmd/s — key finding: Postgres `max_connections=100` + an uncapped `*sql.DB` pool (no `SetMaxOpenConns`) are the concurrency bottleneck (flagged for Phase 16, not fixed)

---

### Phase 11 — Dictionary as a Service (APPROVED 2026-07-13)

> **Full detail in separate plan document: [Dictionary-Service-Plan.md](Dictionary-Service-Plan.md)** — sub-phases 11.1–11.5.

New project goal, alongside the shipping event-sourcing shapes: the Dictionary as **shared
reference/master data** — a central repository for lookup values used throughout the platform
(vehicle types, order statuses, currencies, units of measure, trailer types, Incoterms, hazard
classes, countries, …), with localization, typed cross-references, and a versioned NATS-KV cache
protocol. Delivered as a **separate service** (`demos/01-dictionary/refdata-service/` — own Go
service + container, own Postgres schema, own `refdata-{context}` KV bucket) — additive to the
compose stack, no changes to the existing shipping implementation.

**Decisions made at approval** (see the sub-plan for full rationale):

- Separate service, not a module in the existing monolith (Q1, Option B).
- The shipping backend's Phase 11.3 demo consumer is the **hazard-class** dictionary type.
- Approved scope now: **11.1–11.4**. AI-assisted translation (inside 11.4) and the NATS `micro`
  request-reply spike (inside 11.3) are parked, not in this pass. 11.5 stays optional.
- BR-D01–D07 confirmed as drafted.

#### Sub-phases

- [x] **11.1 — Core service** (Postgres CRUD + read API): scaffold, schema, domain rules
      BR-D01/02/05/06, REST + Swagger + seed data, Ginkgo specs (2026-07-14)
- [x] **11.2 — Localization + reference resolution**: fallback chain (BR-D03), locale
      management, reference expansion, bulk localized export (2026-07-14)
- [x] **11.3 — KV cache + versioned-read protocol + NATS comms**: set-version bump (BR-D04),
      `refdata-{context}` write-through, `REFDATA` change-event stream, hazard-class consumer
      demo in the shipping backend, KV watch → SSE (2026-07-14)
- [x] **11.4 — UniFi-style frontend** (`frontend-dict/`): view/add/delete/deprecate (BR-D02),
      item editor, locales panel, cache status widget (2026-07-14)
- [x] **11.5 (optional)** — ports-registry migration evaluation (decision: leave as-is) +
      Obsidian findings write-up (2026-07-14)
- [x] **11.11 — Enum value localization UX** (`frontend-dict` three-panel redesign, frontend-only):
      make enum values first-class, localizable data — compact values table, a
      `General | Translations | Usage` detail panel, and a bulk translation matrix. Reuses the
      existing BR-D18 attrs-update PATCH; no new backend. Full detail below.

> Note: the inline list above is abbreviated (11.1–11.5); sub-phases 11.6–11.10 are already
> delivered and tracked in [Dictionary-Service-Plan.md](Dictionary-Service-Plan.md), which is the
> per-sub-phase source of truth.

See [Dictionary-Service-Plan.md](Dictionary-Service-Plan.md) for the full checklist per sub-phase.

#### Phase 11.11 — Enum value localization UX (added 2026-07-17)

##### Goal

Enum values (e.g. `Ship Status` → `at-anchor` = "At Anchor") can be viewed but not localized in the
admin UI, and reference-data items only get a one-locale-at-a-time editor. Give both a proper
localization workflow: treat an enum value as first-class data (stable key, default label,
translations, description, status) rather than a bag of generic attributes, and make bulk
translation across locales fast. This is primarily a `frontend-dict` change; the one backend
addition is the service's first **item-update** endpoint (there is currently create / deprecate /
reactivate / delete but no way to edit an existing item's default label or description).

##### Design — three-level layout

Keep the master-detail spatial model already used across the dictionary UI, now three panels:

```
┌─────────────────┬──────────────────────────────────┬────────────────────────────┐
│ Enum types      │ Ship Status              5 values │ Selected value             │
│                 │                                  │                             │
│ + New enum      │ + Add value    Search…           │ General | Translations |    │
│                 │                                  │           Usage             │
│ Ship Status  5  │ Key         Default label  Status│                             │
│ Cargo Type  12  │ at-anchor   At Anchor      ●     │ (tab content)               │
│                 │ docked      Docked         ●     │                             │
│                 │ in-transit  In Transit     ●     │                             │
└─────────────────┴──────────────────────────────────┴────────────────────────────┘
```

- **Left — enum types:** existing type list (register type, per-type value count).
- **Middle — enum values as a compact table** (replaces the current text list):
  - `Key` — monospace, fixed width, truncates with the full key in a tooltip; not inline-editable.
  - `Default label` — flexible width.
  - `Status` — compact badge/icon (active / deprecated).
  - Sortable and searchable; a per-row overflow menu offers **Edit · Deactivate/Reactivate ·
    Duplicate · Delete**. Fixes today's problems: keys no longer wrap unpredictably, label no longer
    reads as glued to the key, long values truncate cleanly, sort/search are trivial, and status is
    visible in the list rather than only in the detail header.
- **Right — selected value detail**, retabbed from the generic `Attrs | Localizations | References`
  to first-class **`General | Translations | Usage`** ("Attributes" is too generic for what is
  structured data — any genuinely arbitrary attrs still surface under General):
  - **General** — Key (read-only), Default label (editable), Status, Description (editable).
  - **Translations** — a table, not one locale at a time:

    ```
    Locale   Display name              Translation    Status
    en-za    English — South Africa    At Anchor      Default
    af-za    Afrikaans — South Africa  Voor Anker     Complete
    fr-fr    French                    Au mouillage   Complete
    de-de    German                    —              Missing
    ```

    (Locale codes are lower case throughout — BR-D20, added 2026-07-17 — not the
    BCP-47-conventional upper-cased region subtag.)

    Controls: search locales · **Missing only** toggle · **+ Add locale**. Inline editing is
    appropriate here (low-risk, repetitive edits). Locale display names come from the browser's
    `Intl.DisplayNames` — no backend change. "Missing" rows are a client-side join of the registered
    locale set (`store.locales`) against the value's existing localizations.
  - **Usage** — where the value is referenced (existing `listItemReferences`), reframed from the old
    References tab.
- **Bulk translation matrix** — a per-type **`Values | Translation Matrix`** toggle. The matrix lays
  enum values (rows) against locales (columns) with editable cells, so a translator fills a whole
  language column without opening each value:

    ```
    Enum value    English      Afrikaans    French
    at-anchor     At Anchor    Voor Anker   Au mouillage
    docked        Docked       Vasgemeer    À quai
    in-transit    In Transit   In Transito  En transit
    ```

  Every cell is just an existing `setLocalization` upsert; no new API beyond the item-update
  endpoint below. Distinct from Phase 11.7's types×locales completeness matrix (that shows ratios;
  this edits individual values within one type).

##### Design — backend (already delivered; frontend-only wiring left)

The item-update capability this UX needs **already exists** — no new endpoint or business rule:

- **BR-D18** — an item's `attrs` map can be replaced after creation, exposed as
  `PATCH /api/refdata/admin/items/{type}/{context}/{code}/attrs` (`handlers.go`,
  `commands.ItemHandler.UpdateItemAttrs()`). It's a **full-map replace**, so editing the default
  label = read current attrs, set `attrs.name` (and `attrs.description`), PATCH the whole map back.
  The stable key is not part of attrs and is already immutable.
- **Duplicate** clones a value into a new key via the existing create path (`registerItem`, copying
  attrs; translations start empty) — no backend change.

The only missing piece is a `frontend-dict` `api.js`/store `updateItem` method that calls the
existing PATCH; everything else in 11.11 is UI.

##### Checklist

- [x] `frontend-dict` `api.js` + store: `updateItem` method wrapping the existing
      `PATCH …/items/{type}/{context}/{code}/attrs` (BR-D18); duplicate flow via `registerItem`
      (implemented 2026-07-17)
- [x] Middle panel → sortable/searchable `DataTable` (Key / Default label / Status), key truncates
      with full-key tooltip, per-row overflow menu (Edit · Deactivate/Reactivate · Duplicate · Delete)
      (implemented 2026-07-17)
- [x] Detail panel retabbed to `General | Translations | Usage` (restructure `ItemDetailPanel.vue`;
      shared with the Reference Data screen, so both benefit) (implemented 2026-07-17)
- [x] Translations tab: locale table with Default/Complete/Missing status, `Intl.DisplayNames`
      display names, **Missing only** filter, **+ Add locale**, inline edit (implemented 2026-07-17)
- [x] Bulk **Translation Matrix** view + `Values | Translation Matrix` toggle (implemented 2026-07-17)
- [x] ~~All UI strings routed through `ui-copy` (BR-D16)~~ — **N/A, corrected at implementation.**
      `frontend-dict` has no `vue-i18n`/`ui-copy` wiring at all (unlike `frontend-port` —
      Phase 11.10 explicitly scoped that work to `frontend-port` only); every existing
      `frontend-dict` component (`categories.js`, `TypeNavigator.vue`, etc.) uses plain hardcoded
      English strings. New strings in this phase follow that same existing convention rather than
      introducing i18n wiring out of scope for this task.
- [x] No new business rule needed — reuses BR-D18 as designed; no domain behaviour changed
- [x] `frontend-dict` build green (`vite build`, `eslint`) + new Vitest suite (24 tests) for the
      pure logic in `itemFields.js`/`localization.js`. **Browser click-through verification could
      not be completed** — this session runs as a background job with no display/browser-extension
      access, and the Codex rescue agent's Playwright fallback was blocked by sandbox filesystem
      permissions. Substituted: every new mutating code path (`updateItem` PATCH, `setLocalization`
      upsert create+edit, `registerItem` for Add value/Duplicate, deprecate/reactivate/delete) was
      exercised directly against the live `refdata-service` container with disposable, cleaned-up
      scratch data, confirming request/response shapes match the frontend's assumptions exactly.
      **Recommend a manual click-through next time the app is opened in a real browser**,
      particularly for layout/visual concerns (three-panel responsiveness, `SelectButton` toggle
      styling, DataTable row-click highlight) that only a rendered check can catch.

---

### Verification status (2026-07-09)

The full compose stack now runs end to end (Docker installed 2026-07-09):
all five services build and start (`nats`, `postgres`, `backend`, `frontend`,
`frontend-port`), Swagger UI serves at `:18080/swagger/`, both frontends serve
with working nginx `/api` proxies, and a live smoke test exercised the full
container lifecycle against the real stack — register → load → BR-012
rejection at sea → unload at destination — with the `meta.known-ports`
projection, terminal yard query, and Shape C fleet+container reconstruction
all returning correct results. `go build` / `go vet` / `ginkgo ./...`
(22/22 specs) and both frontend builds remain green.

---

### Phase 12 (DONE, with known gaps noted below) — Refdata Versioning, Tenancy & Template Inheritance

> **Full detail in separate design document: [Refdata-Versioning-Tenancy-Design.md](Refdata-Versioning-Tenancy-Design.md)** — sub-phases 12.1–12.7.

#### Goal

Evolve the refdata service from a single-context, unversioned CRUD store into a
multi-tenant reference data platform with corpus-level versioning, a draft/publish
lifecycle, first-class rollback with audit trail, multi-level template inheritance
(with overrides and additions, no deletion of inherited entries), version pinning by
consumers, and hybrid KV materialization (eager on publish + TTL-governed lazy
re-materialization on demand via rewrite-on-read).

#### Key design decisions

- **Context hierarchy** — contexts form a tree (`global → emea → emea-acme`); inheritance
  resolution walks child → root, first match wins; overrides break propagation per-item.
- **Corpus-level versioning** — an immutable snapshot of the entire flattened refdata set,
  replacing the current per-type `dictionary_set_versions` as the consumer-facing version.
- **Materialize on publish** — inheritance is resolved and flattened at publish time, not
  read time. Reads never walk the chain.
- **Rollback = new forward version** — version numbers only increase; rollback copies the
  target version's data into a new version and publishes it.
- **Hybrid KV** — versioned buckets (`refdata-{context}-v{N}`); active version has no TTL;
  superseded versions get bucket-level TTL; rewrite-on-read resets TTL for pinned old versions.
- **Backward compatible** — existing unversioned `refdata-{context}` bucket and API continue
  to serve "latest published."

#### Sub-phases

- [x] **12.1 — Context Hierarchy**: `contexts` table, REST endpoints (register/list/get with
      ancestors+descendants), recursive-CTE hierarchy traversal — now integration-tested
      against real Postgres (3+ level chain)
- [x] **12.2 — Corpus Versioning & Draft/Publish Lifecycle**: corpus snapshot tables, draft
      create/edit/publish, version listing, per-version contents (`GET .../draft`,
      `GET .../versions/{v}`), and a diff endpoint (`GET .../diff/{v1}/{v2}`, plain
      added/removed/changed key list per the resolved audit-scope decision)
- [x] **12.3 — Rollback with Audit**: first-class rollback creating forward versions,
      audit fields (`rolled_back_at`, `rolled_back_by`) — integration-tested including
      rollback to a non-immediately-preceding version
- [x] **12.4 — Template Inheritance Resolution**: `CreateDraft` now actually resolves
      inheritance from each ancestor context's latest *published* corpus (not just the
      same context's own prior version, which is what the first implementation pass did) —
      `domain.FlattenCorpus` is wired into the repository, not just unit-tested in isolation.
      Localization inheritance (resolved Q3) is implemented too: `corpus_localizations` flows
      with the item, and a new `PutDraftLocalization` (+ `PUT .../draft/localizations`) lets a
      child override one locale of an inherited item without overriding the item — the
      working-table `SetLocalization` path structurally can't do this (its FK requires the
      item to live in the same context's own `dictionary_items`). Integration-tested for a
      2-level chain including override survival across a later re-draft.
- [x] **12.5 — Hybrid KV Materialization & Version Pinning**: versioned KV buckets
      (`refdata-{context}-v{N}`), eager materialization on publish/rollback
      (`kvcache.VersionNotifier`), bucket-level TTL for superseded versions
      (`kvcache.SupersededVersionTTL` = 30d), rewrite-on-read on every versioned GET
      (`kvcache.VersionReader`), and the versioned read REST surface
      (`GET /api/refdata/v/{version}/{context}/{type}[/{code}]`,
      `GET /api/refdata/v/latest/...`). Integration-tested against a real embedded
      NATS/JetStream server (TTL and rewrite-on-read are genuine server behavior, not
      something a fake can stand in for) in `kvcache_versioned_integration_test.go`.
- [x] **12.6 — Frontend (Versioning Admin UI)**: new "Versioning" nav entry in `frontend/refdata`
      alongside the existing Localization view, `VersioningPanel.vue` with three tabs — Contexts
      (tree viewer + register-new-context dialog), Corpus Versions (create draft/publish/rollback,
      version table with parent/base-context-version columns), Diff (pick two versions, see
      added/removed/changed keys). New `stores/versioning.js` Pinia store. Verified end-to-end in
      the browser against the live docker stack: created a draft (200 items/600 localizations from
      the seed data), published it, made a working-table edit, drafted+published v2/v3, ran a real
      diff (correctly showed `currency/USD: changed`), and rolled back to v1 (v4 published with
      v1's content, v3 flipped to `rolled-back`, v1/v2 stayed `published` — versions coexisting
      indefinitely, confirmed via a direct `GET /api/refdata/v/4/...` hit showing v1's data).
- [x] **12.7 — Consumer Integration & Documentation**: `refdataconsumer.LookupAtVersion` reads
      `refdata-{context}-v{N}` directly (KV-first, versioned-REST fallback), independent of the
      existing unversioned `Lookup`; `ARCHITECTURE-DICTIONARY.md` documents the versioning model
      end to end. Unit-tested (embedded NATS) in `consumer_test.go`.

> **Correctness note (2026-07-22):** the first implementation pass (by Codex) had
> `domain.FlattenCorpus` written and unit-tested but never actually called from
> `CreateDraft` — a new draft only ever copied the *same context's* prior version, so no
> context ever really inherited from a different parent context. This has been fixed and is
> now covered by Postgres-backed integration tests in `corpus_repository_integration_test.go`
> (context_repository.go and corpus_repository.go had zero test coverage beyond pure
> in-memory domain checks before this pass).

> **Known gaps (2026-07-22), left for a later phase:** KV bucket cleanup once a version has
> no pinned consumers is deferred to a future pin registry (resolved open question 4);
> `corpus_references` exists in the schema but nothing populates or flattens typed
> references the way items and localizations are; and two Go `net/http` ServeMux route
> conflicts were found and fixed only once these routes were exercised against a real
> server rather than just `go build` — see `ARCHITECTURE-DICTIONARY.md`'s "Corpus
> Versioning, Tenancy & Template Inheritance" section and the design doc's "Versioned Read"
> note for detail; worth remembering that `go build`/`go vet` cannot catch this class of bug.

- [x] **12.8 — Subject Taxonomy: Region/Tenant → Context (both services)**: an audit of NATS
      subject naming against docs.nats.io/Synadia guidance found that both services built every
      subject from hardcoded `Region = "emea"` / `Tenant = "acme"` Go constants, while the real
      per-request tenant identity (`Context`) was carried only in the event payload
      (`event.Context`), never the subject — so `ShipHandler.hydrate()`'s replay filter and
      `ContainerHandler.hydratePair`/`hydrateByNaturalKey` never scoped by context at all: two
      tenants sharing a `shipID` or container natural key would silently merge event histories.
      Fixed by threading `Context`/`itemContext` into every subject and replay filter. Also added
      a subject/KV-key token format validator (BR-020, BR-D22), since `ShipID`, `TypeKey`, `Code`,
      and `Context` all flowed from REST input into a subject or KV key with no validation.
      Final subject shapes (after a same-day follow-up revision, see below): shipping
      `evt.{context}.shipping.{ship|container}.{id}.{event}`; refdata
      `evt.{context}.refdata.{typeKey}.changed`. **Note:** the literal category token (`evt`) is
      the *first* token, context second — not context-first as originally scoped — because
      JetStream refuses to create a stream whose subject filter has an unbounded wildcard in the
      leading position (`*.events.>` can textually overlap `$SYS.>`/`$JS.API.>`, and the server
      requires `NoAck: true` to allow it, which would break the synchronous `Publish`/PubAck flow
      every command handler relies on) — discovered via a real stream-creation failure during
      implementation, not anticipated in the original design pass. Region is dropped entirely (a
      future deployment-instance concern, not part of this lab's subject taxonomy); no
      ancestor-wildcard subject design — the leaf `Context` value is used verbatim, same string as
      the KV bucket-name convention. Requires a local dev data reset
      (`docker compose down -v && up --build` — old-shape stream data is disposable lab data, no
      migration/dual-read compatibility). NATS Accounts-based hard tenant isolation remains out of
      scope (Phase 13).

      **Same-day follow-up revision:** the shape above (`events.{context}.../refdata.{context}...`)
      shipped and was live-verified, then revised once more per a direct user request to unify both
      services under one shared prefix and add an explicit per-service partition: `Domain` constants
      (`"shipping"`, `"refdata"`) were added to each service's domain package, and every subject
      literal/wildcard/parser was updated so the category marker is `evt` (not `events`/`refdata`)
      with the service name as its own token — e.g. `evt.{context}.shipping.ship.{shipID}.{event}`,
      `evt.{context}.refdata.{typeKey}.changed`. A "reduce subject cardinality by moving entity id to
      a header" variant was discussed and explicitly reverted — entity id stays a subject token in
      every case, so `ShipHandler.hydrate()` keeps its targeted `FilterSubject`-based single-ship
      replay (no regression to a full-stream-replay-and-filter-by-header pattern).

#### Business Rules (New)

| Rule | Description |
|---|---|
| BR-V01 | At most one draft per context at a time |
| BR-V02 | Only a draft can be published |
| BR-V03 | Publish is atomic — entire corpus snapshot or nothing |
| BR-V04 | Rollback target must be a previously-published version |
| BR-V05 | Rollback creates a new forward version (numbers never go backward) |
| BR-V06 | A child context cannot delete an inherited item |
| BR-V07 | An override breaks propagation for that item to all descendants |
| BR-V08 | Publishing a parent does not automatically publish descendants |
| BR-020 (shipping) | shipID/context must be a valid subject/KV-bucket token |
| BR-D22 (refdata) | typeKey/code/context must be a valid subject/KV-key token |

See [Refdata-Versioning-Tenancy-Design.md](Refdata-Versioning-Tenancy-Design.md) for the
full data model, API surface, migration strategy, and open questions.

- [x] **12.9 — Ship Surrogate UUID Identity (mirrors Container's pattern)**: reverses an
      earlier decision (documented in `ARCHITECTURE.md`'s former "Why `Container` and not
      `Ship`" note) once it was clarified that `shipID` behaves like a name/call-sign/
      internal-fleet-code — mutable, reassignable — the same pressure that already justified
      `Container`'s surrogate key. `Ship`'s aggregate identity is now an immutable UUID
      (`ID`), minted by `RegisterShip()` (new, explicit) or implicitly by `ArrivePort()` on
      first arrival (optional pre-registration); `shipID` becomes a mutable natural key,
      renameable via the new `CorrectShipID()` command. Because `shipID` is mutable, natural-
      key resolution (`hydrateByNaturalKey()`) can no longer target one ship via `FilterSubject`
      the way Container's dedup check does — it folds every ship's history in a context and
      matches by *current* name (shared via `foldAllShips()`/`resolveShipByNaturalKey()` with
      `ContainerHandler.hydratePair`, which had the identical ship-resolution dependency on the
      old shipID-carrying subject). Postgres's `ships` table moves its conflict target to
      `(context, id)` (mirrors `containers`); the KV read model stays keyed by the natural
      `ship.{shipID}` for query convenience, so a correction is the one case requiring an
      explicit KV rekey (delete old key, put new). Also fixed, found via test failure during
      this work: Shape C's fleet-manifest join compared a container's `OnShipID` (always a
      natural key) against the ship's surrogate map key — silently broken by this change until
      corrected to compare against the ship's current `shipID`. Known limitation, verified live
      and documented not fixed: a container's `OnShipID` snapshots the ship's name at load time
      and doesn't track a later correction — renaming a ship mid-carriage leaves the container
      stuck (unload fails with both the new name, BR-013, and the stale old name, BR-012, since
      resolution is by current name); only unblocked by correcting back to the exact
      pre-correction name first. New rules: BR-021 (a shipID can only be
      registered once), BR-022 (a shipID can be corrected to another valid, unused shipID).
      Requires the same local dev-data reset as 12.8 (old ship events/rows use the pre-surrogate
      identity scheme).

| BR-021 (shipping) | A shipID can only be registered once |
| BR-022 (shipping) | A shipID can be corrected to another valid, unused shipID |

- [x] **12.10 (APPROVED 2026-07-24 — IMPLEMENTED 2026-07-24) — Dual-Transport RPC (`rpc.*`) + Admin UI Observability**

  > **Full detail in
  > [ARCHITECTURE-COMMUNICATIONS.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md)**
  > (status now IMPLEMENTED; see its embedded diagram, page "PROPOSED — Dual-transport
  > RPC (draft)" in
  > [architecture-dictionary.drawio](../../obsidian/V3-Platform/Architecture/Dictionary-POC/architecture-dictionary.drawio)
  > — diagram title kept as-is, only the doc's status line changed).

  #### Goal

  Add a narrow, internal-only NATS core request/reply transport (`rpc.*`) alongside
  each service's existing REST/Swagger surface, for the specific synchronous
  cross-service calls one service needs to make on another (e.g. shipping-service
  looking up refdata-service) — not a full mirror of REST. Give the Admin UI a live,
  non-persisted view of `rpc.*` traffic while it's open.

  #### Key design decisions (carried over from the design doc, see §§1–6 there)

  - `rpc.*` is Core NATS request/reply — no JetStream stream, no persistence, no
    replay. Distinct from `evt.*` (JetStream facts) and `cmd.*` (not used in this
    repo).
  - Subject shape: `rpc.{context}.{service}.{entity}.{action}.v{n}` (parallel to the
    `evt.*` grammar from 12.8), fixed `rpc` leading literal.
  - Dual-adapter pattern: new `internal/natsrpc/` adapter per service, built on
    `github.com/nats-io/nats.go/micro`, calling the **same**
    `commands.*Handler`/`queries.*` methods as the existing `rest/` adapter — no
    domain/application-layer changes required to add it.
  - Only wire `rpc.*` for operations another service actually needs synchronously
    (first concrete case: shipping-service → refdata-service item lookup), not every
    REST endpoint.
  - Runtime discovery via NATS Micro/Services (`$SRV.PING`/`INFO`/`STATS`); static
    docs via AsyncAPI, keeping `operationId` (Swagger) / subject / Go method name
    aligned.
  - Admin UI observability is a **separate, best-effort side-channel**, not a
    stream: each `natsrpc/` handler fire-and-forget publishes request and reply
    (with a shared correlation ID, including on error) to `obs.rpc.*`; Admin UI
    subscribes to `obs.rpc.>` only while the panel is open — no `RPCTRACE` stream,
    no TTL/backlog (rejected as unneeded per the doc's §6 rationale).

  #### Tasks

  - [x] Confirm/finalize business rules for this sub-phase — confirmed as
        transport/infrastructure rules, adopted as drafted: BR-D25 ("an `rpc.*`
        operation must exist as a `commands`/`queries` method already exposed via
        REST") and BR-D26 ("an `obs.rpc.*` publish must never block or fail the
        real RPC reply"), both added to `BUSINESS_RULES-REFDATA.md` (with a
        cross-reference note in `BUSINESS_RULES-SHIPPING.md` since
        shipping-service only *consumes* the rpc.* transport, it doesn't define
        new rules of its own).
  - [x] `refdata-service`: `internal/natsrpc/adapter.go` — `micro.AddService` +
        one `micro.AddEndpoint` (`item-get`) for `rpc.*.refdata.item.get.v1`,
        wired to the existing `commands.LocalizationHandler.ResolveItem()`
        method (the same one `GET /api/refdata/{context}/{type}/{code}` calls).
        Wired into `cmd/main.go` via `Handlers.MountRPC(nc, log)` (a new method
        on `composition.go`, parallel to `Mount`) — `natsrpc` is `internal/` so
        `cmd/main.go` can't import it directly.
  - [x] `shipping-service`: `internal/refdataconsumer`'s `fetchViaAPI` now tries
        `rpc.{context}.refdata.item.get.v1` first when `WithNATS(nc)` is
        configured (any error — no responder, timeout, malformed reply — falls
        through to the existing, well-tested REST path unchanged); wired in
        production via `dictionary/composition.go`'s
        `refdataconsumer.New(kvRefdata, refdataServiceURL(), refdataconsumer.WithNATS(mono.NC()))`.
        `monolith.Monolith` gained an `NC() *nats.Conn` accessor (previously only
        `jetstream.JetStream` was threaded through). Existing REST-fallback tests
        are unaffected (`WithNATS` is opt-in via a functional option).
  - [x] `obs.rpc.*` publish helper — `natsrpc.Adapter.publishObs()`: fire-and-forget
        `nc.Publish` (never `nc.Request`), correlation ID = the request's reply-to
        inbox, fires on both request and reply (including error replies), panic-
        recovered so a marshal/publish failure can never propagate to the caller.
  - [x] Admin UI: new `RpcPanel.vue` (`frontend/admin`) — `EventSource` against a
        new `GET /api/rpc-watch` SSE bridge (`dictionary/internal/rest/sse.go`'s
        `watchRPCObs`, subscribing `obs.rpc.>` via `nc.ChanSubscribe`); pairs
        request/reply rows by correlation ID in a reactive map (not an
        index-based array, since prepending new rows would shift indices).
        Verified live: a direct `nats request` to `rpc.emea-acme.refdata.item.get.v1`
        appears in the panel with matched request/reply payloads, and a
        not-found lookup renders as a red "error" row with the failure message.
  - [x] Integration tests: `refdata/natsrpc_test.go` (embedded core-NATS server,
        no JetStream needed) — BR-D25 (rpc.* and direct `ResolveItem()` return
        identical results, including the not-found error case) and BR-D26 (the
        real reply returns in <500ms with no `obs.rpc.>` subscriber, with a
        deliberately slow one, and the reply-side obs event still carries the
        error on failure). `internal/refdataconsumer/consumer_test.go` gained
        `TestLookupUsesRPCWhenConfigured` / `TestLookupFallsBackToRESTWhenRPCHasNoResponder`.
        97/97 refdata-service specs green; shipping-service `ginkgo ./...` green.
  - [x] Updated `ARCHITECTURE-COMMUNICATIONS.md` status from "draft/proposed" to
        "IMPLEMENTED (Phase 12.10, 2026-07-24)", naming the actual files/methods
        built.

- [x] **12.11 (APPROVED 2026-07-24 — IMPLEMENTED 2026-07-24) — `rpc.*` as the Sole Backend-to-Backend Transport (no REST fallback)**

  > **Full detail in
  > [ARCHITECTURE-COMMUNICATIONS.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md) § 7**
  > and `BUSINESS_RULES-REFDATA.md`'s BR-D28 (both IMPLEMENTED). This design
  > went through two earlier drafts (RPC-primary-with-REST-fallback, then
  > RPC-primary-with-circuit-breaker) before landing here — see § 7's
  > "superseded decisions" note.

  #### Goal

  Make `rpc.*` the **only** transport for backend-to-backend synchronous
  calls between `shipping-service` and `refdata-service` — no REST fallback
  in any form. Backend services should only be aware of NATS for
  inter-service calls: no HTTP client, base URL, or hostname/port config
  pointing at a peer backend service. On repeated `rpc.*` failure, return an
  error to the caller; do not degrade to REST. Frontend-to-backend traffic
  (REST/Swagger for `frontend/admin`, `frontend/refdata`,
  `frontend/seafreight-app`) is explicitly out of scope and unaffected — REST
  stays as each service's inbound surface for those callers.

  #### Key design decisions (carried over from the design doc, see § 7 there)

  - Audit finding (2026-07-24) that motivated this: only `Lookup`/`item.get`
    has any `rpc.*` path, and even that is third-tier (KV hit, then RPC only
    on a miss, then REST on any RPC error) — `ResolveType`, `LookupAtVersion`,
    and `Locales` have no RPC path at all and always call REST.
  - Extend `rpc.*` coverage to every `refdataconsumer` operation: `item.get`
    (exists), new `type.list` (`ResolveType`), a versioned `item.get.v{n}`
    (`LookupAtVersion`), and `locales.list` (`Locales`) — land all four
    **before** removing REST, so no operation is ever left with zero working
    transport mid-rollout.
  - On a KV cache miss/stale entry, the consumer retries `rpc.*` a bounded
    number of times (with backoff); on exhaustion, return an error — no
    REST fallback, no circuit breaker that degrades to REST.
  - **Location transparency is a hard invariant.** Delete
    `REFDATA_SERVICE_URL`, `refdataServiceURL()` (and its hardcoded
    `http://localhost:7201` default), `baseURL`/`httpc`, and every
    REST-calling method (`fetchViaAPI`, `fetchTypeViaAPI`,
    `fetchVersionedViaAPI`, REST-based `Locales`) from
    `internal/refdataconsumer`. `Consumer` holds a `*nats.Conn` and nothing
    else — wire it unconditionally in `dictionary/composition.go` (no
    `WithNATS` opt-in).
  - KV-first caching (BR-D08) and frontend/edge REST traffic are unaffected —
    this only changes what happens on a cache miss/refetch.
  - Resolved: `shipping-service`'s own callers (the Phase 11.3/11.6 demo REST
    handlers) map a retry-exhausted error to HTTP 503 via a shared
    `writeRefdataError()` helper — see the corresponding task below.

  #### Tasks

  - [x] Confirm/finalize business rules for this sub-phase — BR-D28 drafted
        in `BUSINESS_RULES-REFDATA.md` (PROPOSED). Confirmed 2026-07-24
        (final, after two reversed drafts): `rpc.*` is the sole
        backend-to-backend transport — no REST fallback, no circuit
        breaker. A bounded number of retries against `rpc.*`, then an error
        to the caller. All REST-client coupling (`REFDATA_SERVICE_URL`,
        `refdataServiceURL()`/`http://localhost:7201`, `baseURL`/`httpc`) is
        removed from `internal/refdataconsumer`, not merely deprioritized.
        REST/Swagger is unaffected as each service's inbound surface for
        frontend/edge clients and human/test-suite debugging.
  - [x] `refdata-service`: `internal/natsrpc/adapter.go` gained a `Deps`
        struct (`Localizations`, `Items`, `VersionReader`, `Projector`,
        `Log` — all nil-safe) replacing `New`'s old positional args, plus
        three new endpoints wired to the same `commands`/`queries` methods
        their REST counterparts already call (BR-D25 parity): `type-list`
        (`rpc.*.refdata.type.list.v1`, reuses `ItemGetResponse` per item),
        `item-get-versioned` (`rpc.*.refdata.item.get-versioned.v1`, corpus
        version in the request body; response is `kvcache.VersionedEntry`
        directly), `locales-list` (`rpc.*.refdata.locales.list.v1`). Every
        error reply now also carries `notFound bool` (`isNotFoundErr()`,
        mirroring REST's own not-found status-code switch), replacing the
        old bare `error string` shape. `composition.go`'s `MountRPC` updated
        to the `Deps` struct.
  - [x] `shipping-service`: `internal/refdataconsumer/consumer.go` fully
        rewritten — `fetchViaRPC` (existing), `fetchTypeViaRPC`,
        `fetchVersionedViaRPC`, and `Locales` (all new) cover all four
        operations. Deleted: `fetchViaAPI`, `fetchTypeViaAPI`,
        `fetchVersionedViaAPI`, REST-based `Locales`, `baseURL`/`httpc` on
        `Consumer`, `WithNATS` (NATS is now a required `New(kv, nc, ...)`
        constructor argument), and `refdataServiceURL()` /
        `REFDATA_SERVICE_URL` from `dictionary/composition.go` and
        `docker-compose.yml`. `checkRPCError()` maps the new `notFound`
        field to this package's `ErrNotFound`.
  - [x] Implemented bounded retry with backoff in `requestRPC()`: default 1
        initial attempt + 2 retries (3 total), linear backoff
        (150ms × attempt), 3s per-attempt timeout — all overridable via
        `WithRPCRetries`/`WithRPCBackoff`/`WithRPCTimeout`. Exhaustion
        returns `ErrRPCUnavailable`, wrapping the last underlying NATS
        error. Values recorded in `ARCHITECTURE-COMMUNICATIONS.md` § 7.
  - [x] Decided and implemented: `dictionary/internal/rest/handlers.go`
        gained a shared `writeRefdataError()` used by `getRefdataDemo`,
        `listRefdataType`, and `listRefdataLocales` — maps
        `refdataconsumer.ErrNotFound` → 404 (unchanged) and
        `refdataconsumer.ErrRPCUnavailable` → 503 (new), distinct from the
        generic 500. Judged to be REST-layer error handling for a Phase
        11.3/11.6 demo endpoint, not a Ship/Container domain rule, so it's
        documented in `ARCHITECTURE-COMMUNICATIONS.md` § 7 and BR-D28 rather
        than as a new `BUSINESS_RULES-SHIPPING.md` entry.
  - [x] Integration tests: `refdata/natsrpc_test.go` gained BR-D25 parity
        Context blocks for `type.list` and `locales.list` (same
        `NATS RPC Adapter` Describe, reusing its embedded core-NATS server)
        plus a separate `item.get-versioned` Describe (needs its own
        embedded JetStream server, seeded directly via
        `kvcache.NewVersionMaterializer` — no Postgres needed, same
        no-Postgres convention as the rest of the file), covering both the
        success and not-found cases. `internal/refdataconsumer/consumer_test.go`
        replaced `TestLookupFallsBackToRESTWhenRPCHasNoResponder` and
        `TestLookupMissForwardsLocaleToAPI` with
        `TestLookupReturnsErrRPCUnavailableWhenNoResponder`,
        `TestLookupRetriesBeforeSucceeding` (proves retries actually loop,
        not just fail once), `TestLookupMissForwardsLocaleToRPC`, and added
        RPC-path coverage for `ResolveType`/`LookupAtVersion`/`Locales`
        (`TestResolveTypeUsesRPCWhenBucketEmpty`,
        `TestLookupAtVersionMissUsesRPC`, `TestLocalesUsesRPC`,
        `TestLocalesReturnsErrRPCUnavailableWhenNoResponder`). New
        `dictionary/internal/rest/refdata_demo_error_test.go` covers the
        503 mapping for all three demo handlers. 106/106 refdata-service
        specs green; shipping-service `ginkgo ./...` green (82 specs across
        4 suites, plus all `go test` packages).
  - [x] Updated `ARCHITECTURE-COMMUNICATIONS.md` § 7 and BR-D28 from
        PROPOSED to IMPLEMENTED, recording the retry/backoff values and
        endpoints actually built.

---

### Phase 13 (APPROACH AGREED 2026-07-27 — IMPLEMENTED) — NATS Accounts Tenancy Spike

#### Goal

Today tenancy is a string convention: one unauthenticated `nats.Connect`, tenant scoping enforced only by the subject/bucket names the application happens to use. NATS **accounts** are the server-enforced isolation mechanism — this spike exercises them so "subject prefixes are enough" vs "accounts are required" is a measured decision, not an assumption, before the real platform commits.

#### Scope (spike, not production auth)

- Two accounts in server config (e.g. `acme`, `globex`) with per-tenant credentials.
- Verify the server actually enforces isolation: tenant A's credentials cannot publish/subscribe/replay tenant B's subjects or KV buckets — including JetStream API access (streams/consumers are per-account resources).
- Resolve the taxonomy interaction: inside an account, the `{tenant}` subject token is redundant — decide whether the token stays (portability across account-per-tenant vs shared-account deployments) or the account *is* the tenant boundary, and document the trade-off.
- Note but don't implement: operator/JWT mode vs static server-config accounts; exports/imports for any cross-tenant sharing.

#### Delivery: two steps, narrow then broad (agreed 2026-07-27)

Split so the isolation question is answered by a cheap, additive change before any
service's connection code is touched. **13a proves the server enforces isolation;
13b proves the application can live inside it.** 13b is gated on 13a's result — if
13a shows accounts don't buy anything the subject prefixes don't already give us,
13b is cancelled rather than executed.

Static server-config accounts only (an `accounts {}` block with `users`, per
[the NATS accounts docs](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/accounts)) —
no operator/JWT mode. That keeps NATS Tower's operator-mode requirement (see
`.claude/memory/nats_tower_operator_mode_tradeoff.md`) a separate, still-undecided
question rather than dragging it into this spike.

**The structural fact that shapes both steps:** JetStream assets are *per-account*.
Two accounts therefore means **two separate `SHIPPING` streams** and two separate
sets of KV buckets, mutually invisible — not one stream with tenant-wildcarded
subjects. So the cross-context wildcards (`evt.*.shipping.ship.>`,
`domain.StreamSubjects()`, events.go:43-45) and the `{context}` bucket suffix
(`{prefix}-{context}`, kvstore/kv.go:41) both become redundant *inside* an account.
That is the taxonomy trade-off this phase exists to document, and it is why 13b
uses one tenant-scoped connection rather than a per-tenant connection map.

#### What the NATS docs recommend for consumers + tenancy (researched 2026-07-27)

Sources: [JetStream consumers](https://docs.nats.io/nats-concepts/jetstream/consumers),
[JetStream resource management](https://docs.nats.io/running-a-nats-service/configuration/resource_management),
[accounts](https://docs.nats.io/running-a-nats-service/configuration/securing_nats/accounts).

- **Per-account JetStream isolation is explicit, not inferred**: *"A JetStream enabled
  server supports creating fully isolated JetStream environments for different
  accounts."* Streams, consumers and KV buckets are account-scoped resources.
- **The documented way to partition one stream by tenant is one durable per tenant,
  filtered on the tenant token.** The consumers page's own worked example is a stream
  on `factory-events.*.*` with a consumer filtered to `factory-events.A.*`, plus
  multi-filter consumers (`[factory-events.A.*, factory-events.B.*]`). Mapped onto our
  taxonomy that is `FilterSubject: evt.acme.shipping.ship.>` — **not** today's
  `evt.*.shipping.ship.>`.
- Which leaves three distinct shapes, only two of which are documented patterns:
  1. **Today** — one durable per projector on a tenant-agnostic wildcard, tenant
     resolved from the *payload* (`event.Context`) at write time. Convenient and
     deliberate (events.go:40-42), but it is not an isolation pattern: nothing stops a
     projector seeing every tenant, because nothing is meant to.
  2. **Shared account, per-tenant durables** — the docs' factory-events pattern; the
     natural end state if 13a concludes "subject prefixes are enough". Costs
     N tenants × 4 projector durables.
  3. **Account per tenant** — durables are per-account by construction, so the tenant
     token in the filter is redundant; always 4 durables per account.
- **Durables are designed to outlive their client.** Durables *"remain even when there
  are periods of inactivity"* and clients "resume" by rebinding; only ephemerals are
  *"automatically cleaned up (deleted) after a period of inactivity, when no
  subscriptions are bound."* Hence 13b's swap is stop-and-rebind, not delete-and-recreate.
  Corollary: do **not** set `InactiveThreshold` on projector consumers.
- **`max_consumers` is a per-account limit** (docs example: 100), alongside
  `max_streams`, `max_mem`, `max_file`. This is a concrete scaling argument for shape 3
  over shape 2: shape 2 accumulates every tenant's durables against a single account
  ceiling, shape 3 does not. Record it as spike evidence.
- **Pull consumers are the recommendation for new projects** — already satisfied: the
  `jetstream` package's `Consume()` (eventhandler/handler.go:104-137) is pull-based with
  automatic flow control. No change needed.

##### 13a — Narrow: additive config + isolated proof

Additive only. No existing service changes behaviour; nothing in `backend/` is edited.

- `nats/nats.conf` (currently 18 lines, zero auth) gains an `accounts {}` block with
  `acme` and `globex`, one user each, **no exports/imports between them**.
- A default/`$G` no-auth account is preserved so `shipping-service` (cmd/main.go:74),
  `refdata-service` (cmd/main.go:125), and all three frontends keep connecting
  exactly as they do today — zero code changes, zero compose changes to those services.
- Isolation is proven by a **standalone Go test** that dials the tenant creds
  directly (its own package; not wired into either service) and asserts the *server*
  rejects cross-tenant core pub/sub, KV access, and JetStream API calls
  (stream/consumer create + read). Per CLAUDE.md this test also asserts
  `nc.Opts.Name != ""` on every connection it opens.
- Blast radius: one config file + one new test package.

Checklist:

- [x] **Business-rule question resolved (2026-07-27): this phase adds no numbered
      business rule.** The invariant it enforces —
      *a tenant's credentials must not read or write another tenant's events, streams,
      or KV buckets, and the server rather than the application enforces this* —
      is a **deployment/infrastructure invariant, not a domain rule**, and is recorded
      in `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md` alongside
      the account-vs-prefix decision (see "Findings to document" below). No `BR-` number
      is assigned; **BR-023 remains unallocated.**

      Why it fails the `BUSINESS_RULES-SHIPPING.md` format: every rule there
      (BR-001…BR-022) names a domain **Error**, an **Enforced in** aggregate method or
      handler that calls one, and a `Domain Rules / BR-0xx` **Test**. This invariant has
      no domain error (the failure is a NATS permissions violation surfaced by the client
      library), no aggregate method to enforce it in (the whole point is that the
      application *cannot*), and could not live in `dictionary/internal/domain/` as
      CLAUDE.md Quality Rule 3 requires, since that layer has no framework dependencies.
      It also spans both services, so it is not a shipping-domain concern — refdata keeps
      its own `BR-D*` series for the same reason. Closest analogue, BR-020 (a `context`
      must be a valid subject/KV-bucket token), is still domain-enforced via
      `ErrInvalidToken` and is about the *shape* of a tenant identifier, not isolation
      between tenants.

      The Ginkgo spec is still written (next checklist item) — it is an **infrastructure
      spec asserting the server rejects cross-account access**, not a
      `Domain Rules / BR-023` spec.
- [x] `nats/nats.conf`: `accounts {}` with `acme` + `globex` + preserved `DEFAULT`
      account via `no_auth_user: default`; creds/passwords are plaintext spike
      fixtures, clearly marked non-production, not reused anywhere else
- [x] Standalone isolation test —
      `shipping-service/internal/natsaccounts/isolation_test.go`, loads the real
      shipped `nats.conf` via `server.ProcessConfigFile` into an embedded server
      (not a reimplementation): cross-tenant core pub/sub isolation (invisibility,
      not a rejected call — confirmed against the NATS docs' actual model),
      wrong-credentials rejection, and `nats.Name(...)` asserted on every connection
- [x] Confirmed the two accounts get independent JetStream: identical stream name
      (`SPIKE_ISO`) created in both `acme` and `globex` accounts without collision,
      each seeing only its own messages; identical KV bucket name (`dict-a`) same
      result — `globex` gets `ErrBucketNotFound` looking up `acme`'s bucket, then
      creates its own independently. All 5 tests pass (`go test ./internal/natsaccounts/... -v`)
- [x] `go build ./...` + `ginkgo ./...` green with the new config in place — 82
      existing specs plus all other packages unaffected
- [x] Verified against the real docker-compose stack, not just the embedded-server
      test: `docker compose down -v && docker compose up -d --build` (down -v
      required per the operational note below — **this destroyed all local
      Postgres/NATS/nats-tower/nui demo data**, expected and low-cost since it's
      lab data, flagging since it's the kind of action worth knowing about even when
      expected), then confirmed `lb-shipping-service` and `lb-refdata-service` connect
      with **zero credentials, zero code changes**, no auth errors in the NATS log,
      and `GET /api/ports/global` returns real seeded data end-to-end
- [x] 13a result: **isolation holds, cleanly, with no changes to any existing
      service.** Go/no-go for 13b: **go** — proceed per the design already agreed

##### 13b — Broad: one tenant-scoped connection, swapped from a tenant selector

Gated on 13a. **`shipping-service` only** — see the refdata exclusion below.

> **Deferred to after 13b (agreed 2026-07-27):** how the tenant dropdown's option
> list is *populated* — e.g. the server's HTTP monitoring endpoint
> `GET :8222/accountz` (confirmed working against this config: returns
> `{"accounts": ["$G","GLOBEX","$SYS","DEFAULT","ACME"]}`, no NATS-level auth
> required since HTTP monitoring isn't gated by client creds) vs. a static list in
> app config vs. something else. For 13b itself, hardcode the two tenant options
> (`acme`, `globex`) — don't build discovery yet. Revisit once 13b is implemented
> and reviewed. Note for later: `/accountz` reflects the static config file at that
> moment, not a live/dynamic registry — true runtime tenant onboarding is really an
> operator/JWT-resolver question (see
> `.claude/memory/nats_tower_operator_mode_tradeoff.md`), not something static
> accounts give you for free.

Rather than a `map[tenant]*nats.Conn` routed per request, the process holds **one**
connection whose credentials are swapped when the operator changes a tenant
selector. Under per-account JetStream assets this is the natural shape: the process
simply *is* acme's process for that window, and `event.Context` / the `{context}`
subject token stop carrying isolation weight. It also makes the isolation visible in
the demo — switch tenant and the other tenant's ships vanish because the server
refused, not because a query filtered.

Accepted spike limitations, to be stated in the write-up rather than solved:

- The active tenant becomes **process-global mutable state driven by one browser**.
  Fine for a single-operator lab demo; a non-starter for production. This is a
  deliberate spike simplification.
- While switched to one tenant, the other tenant's projectors are not running. Its
  durable consumers are per-account server-side state and survive, so backlog is
  consumed on switch-back — worth demonstrating, not fixing.

Three known costs, in descending order of work:

1. **Projector *subscription* lifecycle does not exist today.** The four durables
   (`ship-shape-a`, `ship-shape-b`, `container-projector`, `meta-projector`) are
   started once in `Module.Startup` and never stopped — composition.go:59-70
   discards each returned `jetstream.ConsumeContext` with `_`. They are bound to
   cross-tenant wildcards *on purpose* (events.go:40-42: projectors are
   "intentionally tenant-agnostic"). A tenant swap needs each `ConsumeContext`
   captured and stopped, then re-established against the new account.
   **This is client-side work only** — a durable is server-side state that the NATS
   docs explicitly design to outlive its client (durables "remain even when there
   are periods of inactivity"; clients "resume" by rebinding), so each account
   retains its own four durable definitions *and their stream positions*
   permanently. Nothing is deleted or recreated server-side, and no position is
   lost across swaps. (An earlier draft of this plan called this "tearing down and
   rebuilding the durables… the bulk of 13b" — that overstated it.)
2. **The existing dropdown is the wrong dropdown.** It selects *fleets* —
   `['global', 'atlantic-fleet', 'pacific-fleet']` (admin `stores/dictionary.js:10`,
   seafreight-app `stores/port.js:10`) — and rest/handlers.go:557-560 deliberately
   decouples fleet context from refdata context (`const refdataContext = "emea-acme"`).
   13b adds a **separate tenant selector**; it must not overload the fleet selector,
   or the distinction the code currently makes on purpose is lost.
3. **`refdata-service` is excluded, and this is a finding, not a workaround.** It is
   inherently cross-tenant: it serves reference data to every tenant via
   `rpc.*.refdata.*.v1` wildcards, deriving the tenant from the subject token per
   request (`natsrpc/adapter.go:529`, `contextFromSubject`). Placing it in one
   tenant's account makes it unreachable from the other. Doing it properly needs a
   service **export** from a refdata account **imported** by each tenant account —
   exactly what this phase's scope defers. So refdata-service stays on the default
   account for 13b, and "cross-tenant shared services require exports/imports" is
   recorded as a Phase 13 conclusion feeding any future hard-isolation work (see
   `.claude/memory/tenant_service_separation_decision.md`).

Already in our favour (little or no work needed):

- Every SSE endpoint creates its KV watcher / ordered consumer / core subscription
  **per request**, bound to `r.Context()` (rest/sse.go:191-276) — nothing pooled
  across a connection swap.
- The frontends already reset all client state and reopen their EventSources on
  context change (`stores/dictionary.js:42-61`, `stores/port.js:78-108`), which is
  the exact reflex a tenant switch needs.
- KV buckets are created lazily on first touch (`kvstore/kv.go:41-42`), so each
  account's buckets materialise on their own.

Checklist:

- [x] Go/no-go confirmed from 13a — go
- [x] Per-tenant creds available to `shipping-service` — hardcoded spike fixtures in
      `composition.go`'s `tenantCredentials` map (matching nats.conf, same spike-only
      framing as 13a); `nats.Name("shipping-service")` retained on every connection
      (`rest/tenant.go`'s `SwitchTenant`)
- [x] Projector subscription lifecycle — turned out to need **no `eventhandler` package
      changes**: `RegisterShapeA/B/Containers/Meta` already returned
      `jetstream.ConsumeContext` (composition.go previously discarded it with `_`).
      `rest/tenant.go`'s `SwitchTenant` now captures all four, stops them on the next
      switch, and rebuilds against the new account — server-side durables are untouched
- [x] Confirmed no `InactiveThreshold` is set on any projector consumer — asserted in the
      Ginkgo spec (`tenant_switch_test.go`) via `consumer.Info().Config.InactiveThreshold`,
      not just a source read
- [x] Connection swap path implemented in `rest/tenant.go`'s `SwitchTenant`: drains the
      old `*nats.Conn`, connects with the new tenant's creds, rebuilds the JetStream
      context, re-runs `CreateStream`, rebuilds the 4 projectors and KV stores — `kvstore.New`
      always returns a fresh `Store` with an empty handle map, so no stale bucket handle
      can survive a switch

      **Bug found and fixed during frontend verification, not by the Ginkgo spec:**
      `eventhandler.register()` (and `RegisterContainers`/`RegisterMeta`) close over their
      *setup-time* `context.Context` for the **entire lifetime** of the projector — every
      event it ever processes calls `project(ctx, ...)`. `SwitchTenant` was passing
      `POST /api/tenant/switch`'s `r.Context()` straight through, which Go cancels the
      instant that HTTP response is sent — so every event published *after* a
      REST-triggered switch failed its projector with `"context canceled"` and
      redelivered forever (confirmed directly in `docker logs`, an infinite NAK loop, not
      a one-off). Fixed at the root in all three `Register*` call sites
      (`context.WithoutCancel(ctx)`) rather than only at the `SwitchTenant` call site, so
      any future caller with a short-lived context is protected too. The Ginkgo spec
      hadn't caught this because it only read state after a switch, never published a
      *new* event afterward — added that as an explicit regression assertion
      (`tenant_switch_test.go`: arrive a ship post-switch, confirm it lands). Verified live
      against the real stack afterward: arrive → switch (HTTP) → switch back (HTTP) →
      arrive again → both ships present, zero errors in `docker logs`.
- [x] `GET /api/tenant` + `POST /api/tenant/switch` added; admin frontend gained a
      **separate** `stores/tenant.js` Pinia store + topbar `Select`, visually and
      functionally distinct from the Fleet selector; label added via l10n (BR-D16) —
      `nav.tenant` seeded in refdata-service's `seed.go` and regenerated into
      `l10nFallbackEn` via `npm run gen:i18n`, not hardcoded in the component
- [x] In-flight SSE reconnection — satisfied by the **client** closing and reopening its
      own `EventSource` on tenant switch (`stores/tenant.js`'s `setTenant` calls
      `useDictionaryStore().connect()`), which cancels the old request's `r.Context()`
      and lets the existing SSE handler's cleanup path run — no server-side forced
      termination was needed given there's one browser client
- [x] Ginkgo spec `dictionary/tenant_switch_test.go`: registers a ship as acme, confirms
      it's reachable; switches to globex, confirms unreachable via the same API AND
      independently confirms (via a raw connection) globex's own SHIPPING stream has zero
      messages — the server-side fact, not an application filter; switches back to acme,
      confirms the ship reappears (durable position never lost)
- [x] `go build ./...`, `ginkgo ./...` (83 specs), and `npm run build` (admin) all green.
      Verified live against the real docker-compose stack too, both via `curl` and by
      driving the actual admin UI in a browser: registered a ship under acme, switched to
      globex (ship count → 0), switched back (ship reappeared with its original data)

#### Findings to document (both steps)

- [x] **The tenant-isolation invariant itself**, stated in
      `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md` as a
      deployment/infrastructure invariant (*not* a numbered business rule — see 13a's
      first checklist item for why): a tenant's credentials must not read or write
      another tenant's events, streams, or KV buckets, and the server rather than the
      application enforces it. Written now that both 13a and 13b have actually
      demonstrated it, not before
- [x] Decision recorded in the same doc: account-per-tenant vs shared-account+prefixes,
      and what the `{context}` subject token and `{prefix}-{context}` bucket suffix mean
      under each (inside an account both are redundant — say so explicitly)
- [x] Cross-tenant shared services (refdata) need exports/imports — captured as the
      concrete blocker for hard isolation
- [x] Which of the three consumer/tenancy shapes above the POC recommends, with the
      `max_consumers`-per-account scaling evidence — and, if shape 2 (shared account +
      per-tenant durables) wins, note that today's tenant-agnostic wildcard projectors
      would need per-tenant `FilterSubject`s even *without* accounts
- [ ] If the outcome changes the platform-level position, update **DD-04** and the
      Section 12.B open issue "Multi-tenancy hard isolation (NATS Account per tenant)
      — not yet evaluated" in `obsidian/V3-Platform/System Design - V3 Logistics Platform.md`

#### Operational note

Any `nats.conf` change needs `docker compose down -v`, not just `down` — a config
change against a retained volume reproduces the stale-subject Nak loop already
recorded in `.claude/memory/nats_volume_legacy_messages.md`.

---

### Phase 14 (PLAN APPROVED 2026-07-28 — awaiting go-ahead to implement) — Accounts Service & Decentralized JWT Tenancy

#### Context

Phase 13 proved NATS account isolation with a static `accounts{}` block in `nats.conf` and hardcoded user/password pairs. That mechanism was identified as too static and restrictive for N-tenant scaling. This phase replaces it entirely with NATS operator mode (decentralized JWTs, `resolver: full`), adds a new `accounts-service` for dynamic tenant provisioning, and surfaces it in the admin UI's sidebar.

**Key decisions (confirmed 2026-07-28):**
- All services convert to `.creds` files (including refdata-service); `no_auth_user` removed
- Use `github.com/nats-io/jwt/v2` + `github.com/nats-io/nkeys` Go libraries for programmatic JWT minting (these are what `nsc` uses internally — importing `nsc` itself drags in CLI framework code for no benefit)
- Basic auth middleware (shared secret) on accounts-service; WorkOS deferred to a later phase
- Accounts-service is a separate backend from refdata-service (per `.claude/memory/tenant_service_separation_decision.md`); only the admin UI merges both

**Overlaps checked:** Phase 25 (theme spike) is unrelated. No other phase touches auth/accounts/operator mode. `.claude/memory/nats_tower_operator_mode_tradeoff.md`'s previously undecided question is resolved by this phase committing to operator mode.

---

#### 14a — Convert to Operator Mode

**Goal:** Replace `nats.conf`'s static `accounts{}` block with operator-mode JWTs and `.creds` files. Zero new features — all existing Ginkgo specs and the full docker-compose stack must work identically afterward.

##### Bootstrap script: `nats/bootstrap-operator.sh`

A one-shot idempotent shell script (run on the host, not in Docker) using the `nsc` CLI to produce seed artifacts checked into the repo:

- **Operator** `lab-operator` with a signing key
- **System account** `SYS` (required for `$SYS.REQ.CLAIMS.UPDATE` — how 14b pushes JWTs at runtime)
- **Accounts** `DEFAULT` (1G/5G/20/100 — matching current config), `ACME` (256M/1G/10/20), `GLOBEX` (same)
- One service user per account, each with a `.creds` file

Outputs checked into the repo:
```
nats/
  operator.jwt
  resolver/           ← account JWTs for resolver: full
  creds/              ← .creds files (default.creds, acme.creds, globex.creds, sys.creds)
  keys/               ← operator signing key seed (most sensitive artifact; secrets manager in prod)
```

Idempotent: skips if outputs exist. `--force` flag regenerates everything.

##### Rewrite `nats/nats.conf`

Remove entire `accounts: { ... }` block + `no_auth_user: default`. Replace with:
```
operator: /etc/nats/operator.jwt
system_account: <SYS public key>
resolver: { type: full, dir: /data/jwt, allow_delete: true, interval: "2m" }
resolver_preload: { <DEFAULT pubkey>: <jwt>, <ACME pubkey>: <jwt>, <GLOBEX pubkey>: <jwt> }
```

##### Docker Compose — NATS service

Add volume mounts:
- `./nats/operator.jwt:/etc/nats/operator.jwt:ro`
- `./nats/resolver:/data/jwt` (read-write — the server accepts pushed JWTs at runtime, enabling 14b)

##### Docker Compose — service creds

**shipping-service:** mount `./nats/creds/` → `/etc/nats/creds/:ro`, add env `NATS_CREDS_DIR=/etc/nats/creds`
**refdata-service:** mount `./nats/creds/default.creds` → `/etc/nats/creds/default.creds:ro`, add env `NATS_CREDS_PATH=/etc/nats/creds/default.creds`

##### Service code changes

**refdata-service `cmd/main.go`** (~2 lines): add `nats.UserCredentials(credsPath)` option when `NATS_CREDS_PATH` env var is set. Falls back to bare connect when empty (local dev without Docker).

**shipping-service `cmd/main.go`**: same pattern for the DEFAULT connection; pass `credsDir` into the `app` struct so it reaches `composition.go`.

**`rest/handlers.go`**: `TenantCredentials` changes from `{User, Password string}` to `{CredsPath string}`.

**`rest/tenant.go`**: `nats.UserInfo(creds.User, creds.Password)` → `nats.UserCredentials(creds.CredsPath)`.

**`composition.go`**: hardcoded `tenantCredentials` map built from `credsDir` instead of inline user/password pairs:
```go
tenantCredentials = map[string]rest.TenantCredentials{
    "acme":   {CredsPath: filepath.Join(credsDir, "acme.creds")},
    "globex": {CredsPath: filepath.Join(credsDir, "globex.creds")},
}
```

##### Test infrastructure

**`natsaccounts/isolation_test.go`**: `connectAs(user, password)` → `connectAs(name)` using `nats.UserCredentials` against `nats/creds/<name>.creds`. `TestNoAuthUserPreservesTodaysBehavior` — rewritten (not deleted) into two tests: `TestDefaultCredsGetFullJetStreamAccess` (DEFAULT's `.creds` still gets full JetStream + pub/sub access) and `TestUnauthenticatedConnectionRejected` (a bare connection with zero credentials is now rejected — `no_auth_user` no longer exists). `TestWrongCredentialsRejected` now tampers one byte of a real `.creds` file's JWT signature rather than supplying a wrong password (there is no password anymore).

**`tenant_switch_test.go`**: same — `TenantCredentials` values change from `{User, Password}` to `{CredsPath}`. Relative paths to `../../../nats/creds/*.creds` follow the existing depth convention.

##### NATS Tower

Should now work (it requires operator mode + SYS account). May need `nats/creds/sys.creds` mounted into the `nats-tower` compose service. Verify "Error loading data" messages are gone.

##### 14a verification

- [x] All Ginkgo specs pass (`ginkgo ./...` in shipping-service — 83/83; `go test ./...` in refdata-service)
- [x] `docker compose up` starts cleanly; all three frontends load; tenant selector works — verified live: arrived a ship as acme, switched to globex (fleet emptied), switched back (ship reappeared), confirmed visually in the admin UI browser (topbar tenant selector, live SSE panel)
- [x] `curl localhost:8222/varz` reports operator mode — `trusted_operators_count: 1`, `system_account` set, NATS log line `Managing all jwt in exclusive directory /data/jwt`
- [x] `nats/creds/` and `nats/resolver/` directories contain expected files — `default/acme/globex/sys.creds`, `DEFAULT/ACME/GLOBEX/SYS.jwt`
- [ ] NATS Tower shows real account data — not wired up: unlike shipping/refdata-service, Tower registers its NATS connection through its own UI/API (like `nui`/`nats-ui`), not env vars, so there's nothing for docker-compose to configure. A user who wants to try it now adds `nats/creds/sys.creds` through Tower's own "add installation" flow. Left as a manual follow-up, not a blocker.

Additional test-infrastructure note beyond the plan's original scope: both `isolation_test.go` and `tenant_switch_test.go` load the *actual shipped* `nats/nats.conf` via `server.ProcessConfigFile` (not a reimplementation) — operator mode adds two docker-only absolute paths to that file (`operator: /etc/nats/operator.jwt`, the resolver's `dir: "/data/jwt"`), so both test files now rewrite just those two paths to test-local temp paths before parsing, keeping every account/JetStream-limit/resolver_preload JWT byte-for-byte as shipped.

---

#### 14b — Accounts Service

**Goal:** New Go service that creates NATS accounts dynamically (mint JWTs, push to resolver, generate `.creds`) and persists account metadata in its own Postgres.

##### Directory structure

```
backend/accounts-service/
  cmd/main.go           ← refdata-service bootstrap pattern (no monolith abstraction)
  go.mod / go.sum
  Dockerfile            ← identical 2-stage pattern as refdata-service
  accounts/
    handler.go          ← REST handlers
    store.go            ← Postgres repository + auto-migration
    provisioner.go      ← jwt/v2 + nkeys wrapper (JWT minting + resolver push)
    middleware.go       ← basic auth (shared secret from env var)
    suite_test.go       ← Ginkgo suite
    provisioner_test.go ← embedded nats-server in operator mode
    handler_test.go     ← httptest end-to-end
```

##### Postgres schema (port 5434, `accounts-postgres`)

```sql
CREATE TABLE IF NOT EXISTS accounts (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL UNIQUE,
    public_key       TEXT NOT NULL UNIQUE,
    signing_key_seed TEXT NOT NULL,  -- encrypted in prod; plaintext for spike
    status           TEXT NOT NULL DEFAULT 'active',
    js_max_mem       BIGINT NOT NULL DEFAULT 268435456,
    js_max_file      BIGINT NOT NULL DEFAULT 1073741824,
    js_max_streams   INT NOT NULL DEFAULT 10,
    js_max_consumers INT NOT NULL DEFAULT 20,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

##### `provisioner.go` — core operations

Holds the operator signing key (loaded from `OPERATOR_SIGNING_KEY_FILE`) and a SYS-account NATS connection.

**`CreateAccount(name, limits)`**: `nkeys.CreateAccount()` → build `jwt.AccountClaims` → sign with operator key → push via `nc.Request("$SYS.REQ.CLAIMS.UPDATE", jwt, timeout)` → return public key + signing key seed.

**`CreateUser(accountPubKey, accountSigningKeySeed)`**: `nkeys.CreateUser()` → build `jwt.UserClaims` → sign with account signing key → combine JWT + seed into `.creds` format → return creds bytes.

**`DeleteAccount(accountPubKey)`**: push revocation via `$SYS.REQ.CLAIMS.DELETE`.

##### REST API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/accounts` | Create account — returns account + one-time `.creds` content |
| `GET` | `/api/accounts` | List all accounts (no creds) |
| `GET` | `/api/accounts/{name}` | Get account details |
| `DELETE` | `/api/accounts/{name}` | Suspend (soft delete + resolver revocation) |

All routes gated by `BasicAuth(ACCOUNTS_AUTH_SECRET)` middleware.

##### Seeding

On startup, check for `DEFAULT`/`ACME`/`GLOBEX` in Postgres — if missing, insert them as pre-seeded records (with public keys from bootstrap artifacts) so the list is complete. Does NOT re-mint their JWTs.

##### Shared creds volume

New Docker named volume `nats-creds` mounted by both `accounts-service` (writes new `.creds` files on account creation) and `shipping-service` (reads them on tenant switch). Convention: `<account-name>.creds`. Bootstrap script seeds initial files.

Shipping-service's `composition.go` changes from a static map to a directory-scanning function (`os.ReadDir` + filter `.creds` files). `getTenant` handler rescans on each call (directory is small).

##### Docker Compose

```yaml
accounts-postgres:   # port 5434, postgres:16-alpine, pg-accounts-data volume
accounts-service:    # port 7202:8080, mounts sys.creds + operator signing key
```

##### 14b verification

- [x] `go build ./...` + Ginkgo specs pass in accounts-service — 13/13 (3 provisioner specs against a live embedded operator-mode server, 6 Postgres-backed store specs, 4 httptest end-to-end handler specs, all green)
- [x] `POST /api/accounts` with new tenant name → JWT pushed to resolver → `.creds` written → shipping-service can switch to new tenant — verified live against the real docker-compose stack: minted `initech`, it appeared in `GET /api/tenant` immediately (no restart), switched shipping-service to it, arrived a ship, switched back to acme and confirmed acme's own ship (not initech's) was the one visible
- [x] `GET /api/accounts` returns DEFAULT, ACME, GLOBEX + any newly created accounts — verified live: seeded three accounts appear from first boot (decoded straight from `nats/resolver/*.jwt`), `initech` appeared after minting
- [x] `POST /api/accounts/{name}/suspend` suspends and revokes → connection rejected — verified live: suspended `initech`, it disappeared from `GET /api/tenant`'s available list, `POST /api/tenant/switch` to it now 400s ("unknown tenant"), and (in `provisioner_test.go`/`handler_test.go`) a direct NATS connection attempt with its old `.creds` is rejected outright, not just hidden from the dropdown (originally `DELETE /api/accounts/{name}`, changed to explicit action verb to avoid implying data destruction)

Implementation deviated from the plan text in two ways, both noted for the record:
- The "shared nats-creds volume" is the existing `./nats/creds` host bind mount (already used by shipping-service since 14a), not a new named Docker volume — simpler for a lab, and avoids needing a seed step to copy bootstrap-generated files into a separate volume.
- `composition.go`'s directory-scanning change (`tenantCredentials` map → `discoverTenants(credsDir)`) landed as part of 14b as planned, but lives in `rest/tenant.go` (next to `SwitchTenant`/`getTenant`, its only two callers) rather than `composition.go` itself.

---

#### 14c — Accounts Page in Admin UI

**Goal:** "Platform" sidebar section with an Accounts page for tenant CRUD, plus dynamic tenant selector population.

##### New files

- `frontend/admin/src/components/icons/IconAccounts.vue` — SVG icon (existing pattern)
- `frontend/admin/src/components/AccountsPanel.vue` — page component

##### `AccountsPanel.vue`

- Header row: title + "Create Account" button
- DataTable: Name, Status, Public Key (truncated), JetStream limits summary, Created At
- Create dialog: form for name + JS limits → `POST /api/platform/accounts` → one-time `.creds` display

##### API routing

Admin frontend's nginx currently routes all `/api/` to shipping-service. Add a second location block:
```nginx
location /api/platform/ {
    proxy_pass http://accounts-service:8080/api/accounts/;
    proxy_set_header Authorization "Basic <base64 of admin:admin-spike-pass>";
}
```

Vite dev proxy: `/api/platform` → `http://localhost:7202` with same rewrite.

API client (`api.js`): `listAccounts()`, `createAccount(input)`, `getAccount(name)`, `deleteAccount(name)` — thin wrappers hitting `/api/platform/accounts`.

##### `App.vue` changes

- Import `AccountsPanel` + `IconAccounts`
- Add `{ eyebrow: 'Platform', items: [{ key: 'accounts', label: 'Accounts', icon: IconAccounts }] }` to `sections`
- Add `accounts:` entry to `SUBTITLES`
- Add `v-else-if="activeView === 'accounts'"` rendering `<AccountsPanel />`

##### Dynamic tenant selector

The tenant store's `refresh()` currently hits shipping-service's `GET /api/tenant`. After 14c, the "available" list can be populated from `listAccounts()` (accounts-service) instead — or shipping-service's `getTenant` can rescan the shared creds directory. Either way, newly-created accounts appear in the dropdown without a restart.

##### 14c verification

- [x] "Platform > Accounts" appears in sidebar, renders the accounts table — verified live in a browser against the real docker-compose stack: sidebar entry, DataTable listing DEFAULT/ACME/GLOBEX plus previously-minted accounts
- [x] Create a new account → creds displayed → new tenant appears in tenant dropdown — verified live: created `browser-test-tenant` through the Create Account dialog, one-time `.creds` dialog appeared, toast confirmed, and it showed up in the topbar tenant `Select` immediately with no page reload
- [x] Switch to the new tenant → shipping-service connects to its isolated NATS account — verified in the prior 14b live check (a different minted tenant, `initech`, switched to and from via the topbar selector; the same code path this page's dropdown drives)
- [x] Suspend an account → it disappears from available tenants — verified live: clicked Suspend on `browser-test-tenant`, toast confirmed, row flipped to "suspended", and the tenant dropdown immediately excluded it alongside the already-suspended `initech`

No new `BUSINESS_RULES-SHIPPING.md`/`BUSINESS_RULES-REFDATA.md` entry for any of Phase 14 (14a/14b/14c): same reasoning as Phase 13a's first checklist item — this is a deployment/infrastructure capability (account provisioning mechanics), not a Ship/Container/refdata domain rule. No new domain error, no aggregate method, and it spans backend services plus a new one, so it doesn't fit either `BUSINESS_RULES-*.md`'s format. Recorded here and in the phase's design notes instead of as a numbered BR.

---

#### Sequencing

```
14a (operator mode)  →  14b (accounts-service)  →  14c (admin UI)
```

Each sub-phase is independently mergeable and verifiable. 14b depends on 14a's resolver + operator signing key. 14c depends on 14b's REST API.

All three sub-phases implemented and verified 2026-07-28 (same day, on explicit go-ahead).


---

### Phase 15 (15a/15b/15c/15d) — Browser NATS WebSocket Transport

#### Goal

The Admin UI's ~5 long-lived SSE connections per tab exhaust Chrome's 6-connection-per-origin
HTTP/1.1 limit — a second tab to the same origin hangs indefinitely. Rather than work around the
limit (HTTP/2, `visibilitychange`, `BroadcastChannel`), make the browser a first-class NATS
client: one WebSocket connection (`ws://localhost:9222`, already exposed for `nats-ui`) replaces
all SSE streams and REST calls for the **Sea Freight Flow** ("public") frontend. Admin/Dictionary
("internal") frontends and cross-account imports (DEFAULT-account streams like RPCTRACE/REFDATA)
are explicitly out of scope for this phase.

Browser interaction model: `rpc.{ctx}.shipping.{entity}.{action}.v1` (request/reply, replaces
REST) and `notify.{ctx}.shipping.{entity}.changed` (pub/sub with the full projected entity as
payload, replaces SSE/KV-watch). See
[admin_ui_realtime_transport_options.md](../memory/admin_ui_realtime_transport_options.md) for the
design discussion that led here.

#### Sub-phases

- **15a — shipping-service natsrpc server**: new `dictionary/internal/natsrpc/` adapter (mirrors
  refdata-service's `natsrpc/adapter.go` — Micro/Services framework, obs.rpc.* observability)
  exposing ship/container/port commands plus new list queries (`ship.list.v1`,
  `container.list.v1`, `meta.known-containers.v1`) as `rpc.*` subjects. Registered per-tenant
  connection, kept alive independently of the single-active-tenant REST `SwitchTenant` pattern.
  **Known gap (accepted):** unlike refdata-service's adapter (always on the DEFAULT account), this
  adapter's obs.rpc.* events publish onto whichever TENANT account is active — isolated from the
  DEFAULT-only Admin UI RPC panel. Publishing anyway so a future cross-account-imports phase makes
  it visible with no code change; see the adapter's "KNOWN GAP" doc comment.
- **15b — notify.\* publishes**: the four event-handler projectors
  (`dictionary/internal/eventhandler/`) fire-and-forget publish the full projected entity to
  `notify.{context}.shipping.{entity}.changed` after each KV write. Plain core NATS pub/sub, no
  JetStream retention — a missed notification is covered by the bootstrap RPC call on reconnect.
- **15c — auth-service**: new standalone Go service (modeled on accounts-service) that mints
  short-lived (5 min TTL), permission-restricted browser user JWTs (`GET
  /api/auth/connectInfo?tenant=X`) plus a tenant list for the browser (`GET /api/auth/tenants`).
  Reads account signing keys from accounts-postgres (read-only). `POST /api/auth/login` is a 501
  placeholder for the future WorkOS flow (BR-UA01).
- **15d — Sea Freight Flow migration**: `nats.ws` client, new `shared/nats/useNatsConnection.js`
  singleton composable, `api.js`/`stores/port.js`/`stores/tenant.js` rewritten to use
  `rpc.*`/`notify.*` instead of REST/SSE. Refdata's shared composables
  (`useRefdataLabels`/`useL10nCopy`) are unchanged — they stay on REST + SSE.

Full implementation plan: see `.claude/plans/ancient-sauteeing-hartmanis.md` (this session's plan
file) for file-level detail, or its contents once folded into this document on completion.

---

### Phase 16 (16a–16k) — Subject Taxonomy & Tenancy Formalization

#### Goal

Phase 15 introduced `notify.*` and put the browser on `rpc.*` without updating the taxonomy
docs, which exposed a deeper inconsistency: `{context}` was documented as "tenant/region scope"
(refdata-service, `ARCHITECTURE-COMMUNICATIONS.md` § 2, `emea-acme`) but implemented as a
tenant-agnostic fleet qualifier in shipping-service, where Phase 13 had made the **NATS account**
the real tenant boundary. Two services had quietly diverged on what the same token means, and no
commit reconciled them. Phase 16 settles the model, documents it once, and migrates the code to
match.

#### Decision record (2026-07-31)

1. **Tenancy is enforced strictly and only by NATS accounts.** Follows NATS's own guidance:
   an account is an isolated tenant with its own subject space and an absolute boundary; accounts
   additionally carry independent resource limits (connections, JetStream storage, streams,
   consumers) that a naming convention cannot express. Subject-prefix/permission separation inside
   one shared account is the pattern NATS documents as *legacy* and weaker, and is not used for
   tenancy here. Already proven by `natsaccounts/isolation_test.go` and
   `accounts/provisioner_test.go`.
2. **`{context}` has no relation to tenant.** It is the **company / business-unit** scope — a soft
   partition inside one tenant's own subject space, for addressing and routing, never a security
   boundary. Context vocabulary is per-tenant, not shared.
3. **Region is a deployment axis**, handled by separate regional stacks each with their own NATS
   instance. It never appears in a context value or subject token. Full hierarchy:
   `region → tenant (NATS account) → company/group → business unit ({context})`.
4. **Business units are hyphenated into the single `{context}` token** (`acme`, `acme-northdiv`),
   never dot-separated: every subject family has **fixed arity** and parsers read `{context}` by
   position (`domain.SubjectDetails` rejects anything but exactly 6 tokens), so a variable-arity
   context would make token 3 ambiguous between "business unit" and a shifted `{service}`.
   Accepted trade-off: NATS wildcards match whole tokens, so `evt.acme-*.>` is invalid — there is
   no subject-level "all business units of company X"; that grouping is answered by the KV/Postgres
   lookup instead. The value is **opaque** — never split on `-` to recover the parts.
5. **`_`-prefixed context values are reserved for platform use** (`_platform`), enforced by
   `accounts-service` rejecting `_`-prefixed company names. Sigils like `$` are unusable: a context
   is also a KV bucket-name component and must match `^[A-Za-z0-9_-]+$`.
6. **No hard isolation between business units** in one tenant account — the separation is
   organizational, enforced in the application layer. Two business units that must be mutually
   opaque have to be modelled as **separate tenants** (separate accounts); `{context}` cannot
   provide it.
7. **Five subject families, two groups.** *Core:* `evt.*` (event sourcing), `rpc.*`
   (service-to-service), `api.*` (frontend-to-service — **new**), `notify.*` (service-side change
   notification, replaces SSE). *Supportive:* `obs.rpc.*` / `obs.api.*` (debugging side-channel,
   deliberately off the business subjects so a slow/absent watcher can never add latency or
   backpressure to a real call). `cmd.*` stays reserved and unused.
8. **`rpc.*` / `api.*` separation rule.** An operation may be registered on both when a backend
   *and* a browser genuinely need it, but each registration is independent (own adapter, own
   permission grant). **A browser credential is never granted `rpc.>`; backend code never calls
   `api.>`.** This closes a latent gap: browser JWTs currently grant `rpc.>`, so any future
   backend-only `rpc.*` endpoint added inside a tenant account would have been browser-reachable
   by accident.
9. **`auth-service` and `accounts-service` carry no `{context}`** — they administer the tenant
   axis itself, so scoping them by company is incoherent (creating the account is what brings the
   company into existence). Not a blanket platform-service exemption: `refdata-service` is equally
   a platform service but its *data* is company-scoped, so it keeps `{context}`.
10. **refdata inheritance keeps arbitrary depth** as already implemented
    (`context_repository.go`'s recursive ancestor CTE) — not restricted to a fixed number of hops.
    Resolution stays **server-side**: a caller asks for one context and receives a resolved answer,
    mirroring `domain.ResolveLabel`'s existing locale-fallback chain. Clients never walk the chain
    or issue N calls. `rpc._platform.refdata.*` exists for stewards/tooling to read or administer the
    global corpus directly, **not** as a fallback path ordinary callers invoke.
11. **A "company group" may sit between tenant and company.** One tenant account can host more
    than one company (the `company or company group` level of the hierarchy). This is what makes a
    company qualifier inside `{context}` meaningful rather than a redundant echo of the account
    name — for a single-company tenant it is a harmless degenerate case.
12. **Both services use the fully-qualified context form** (`acme-atlantic-fleet`), not each
    service's locally-minimal form. Rationale for the choice: `refdata-service` runs on a single
    shared account and so has **no account boundary** to tell whose corpus a request concerns — the
    company qualifier must be in the value. `shipping-service` does have that boundary and could
    use the bare `atlantic-fleet`, but then the same logical scope would carry two different
    canonical names depending on which service you asked, re-creating the divergence this phase
    exists to eliminate (and forcing a composition rule at every crossing point). One vocabulary
    everywhere was chosen over locally-minimal names: the cost is a redundant prefix inside
    shipping's own account and a value migration (16e); the benefit is that a context value means
    exactly one thing everywhere and crosses service boundaries unchanged.
    - Consequence: shipping's company-wide context (formerly `global`) becomes just `acme`, so its
      tree mirrors refdata's exactly (`_platform → acme → acme-atlantic-fleet`). Deliberately *not*
      `acme-global`, which reads oddly for "all of Acme" — and which would also sit confusingly
      close to the reserved root.
    - The reserved root is `_platform`, **not** `_global`: shipping already has a user-space context
      historically called `global`, and while the `_` prefix makes the two formally unambiguous, the
      visual proximity in logs and subjects was judged not worth it. `_platform` also matches the
      platform-services tier this corpus belongs to.
13. **A context may be linked to a tenant** (new optional `tenant` column on `refdata.contexts`).
    **Governance/ownership metadata and query scoping only — explicitly not a security boundary**:
    refdata-service runs on a single shared NATS account and so has no server-supplied caller
    identity to enforce it against. Making it enforceable is an open item (see
    `Refdata-Versioning-Tenancy-Design.md` § 2.1).

#### Sub-phases

- **16a — documentation formalization (DONE 2026-07-31, docs only, no code).** `ARCHITECTURE-COMMUNICATIONS.md`
  § 1–2 rewritten (five families, Core/Supportive, full `{context}` rules, `rpc.*`/`api.*`
  separation rule); `ARCHITECTURE-DICTIONARY.md` context definition corrected;
  `CLAUDE.md`'s taxonomy block corrected (it described the token as `<tenant>`);
  `BUSINESS_RULES-SHIPPING.md` BR-023 and `BUSINESS_RULES-REFDATA.md` amended;
  `Refdata-Versioning-Tenancy-Design.md` § 2.1 reconciled (root `global` → `_platform`, region
  removed as a node, `tenant` column added); `Multi-Region-Plan.md` § 0 added with the
  region/tenancy answers and the Mirror-vs-gateway recommendation.
- **16b — `api.*` migration (DONE 2026-07-31).** `shipping-service`'s `dictionary/internal/natsrpc/` →
  `dictionary/internal/browserrpc/`; `rpc.*` → `api.*`, `obs.rpc.*` → `obs.api.*`;
  `auth-service`'s `MintBrowserToken` grants `api.>`/`notify.>` and **drops `rpc.>`**;
  frontend subject builders updated; tests renamed/retargeted. Mechanical — no behaviour change.
  A fresh `internal/natsrpc/` gets created only if/when shipping-service genuinely needs a
  backend-to-backend endpoint (none exists today); no empty placeholder package is left behind.
- **16c — reserved-name enforcement (DONE 2026-07-31).** Enforced in **both** services, not just
  accounts-service as first scoped — discovered while implementing that `{context}` is
  refdata-service's own resource (`refdata.contexts`), registrable independently of any NATS
  account via `POST /api/refdata/admin/contexts`, so accounts-service alone couldn't guarantee the
  invariant. `accounts-service` (BR-AC07) rejects `_`-prefixed account names at `POST /api/accounts`
  (`400`, distinct from BR-AC06's `409` for an exact reserved-name match) — needed because in the
  common no-company-group case (decision 11) a tenant's own name doubles as its company context, so
  an unguarded account name could smuggle a `_`-prefixed value into the context namespace.
  `refdata-service` (BR-D33) rejects `_`-prefixed context names in `ValidateContextName` — the
  primary enforcement point, since it's the one that can't be bypassed by skipping account creation
  entirely. Both raise a typed error (`ErrReservedContextPrefix`/inline), both tested, both allow a
  mid-string underscore (`acme_northdiv`). Known pending item folded into 16d: `ValidateContextName`
  as written also rejects the platform root's own future registration — 16d's seeding must
  special-case it.
- **16d — refdata context tree (DONE 2026-07-31).** `_platform` (reserved root) → `acme`
  (company, `tenant: "acme"`) → `acme-atlantic-fleet` (business unit); `emea-acme` retired.
  `refdata.contexts.tenant` added (`ALTER TABLE`, nullable — BR-D34, governance metadata only,
  not enforced, per decision 13). `_platform` is seeded via a new `ContextHandler.RegisterPlatformRoot`,
  the one sanctioned exception to BR-D33's blanket `_`-prefix rejection — the public REST endpoint
  still rejects `_platform` unconditionally; only `seed.go` calls the bypass.
  **Seed data now actually demonstrates inheritance**, not just registers a tree: standards types
  (currency/country/incoterm/uom/hazard-class) moved to `_platform`; domain types (ship-status,
  UI-copy strings) moved to `acme`. `hazard-class` alone carries all three inheritance states —
  codes 1/2/4-9 inherited, code 3 overridden at `acme` (BR-V07), code `X1` an addition only at
  `acme-atlantic-fleet` (BR-V06). Critically, `Seed` also idempotently drafts+publishes an initial
  corpus version per context, **parent-first** — required because `CreateDraft` silently skips
  (not errors) an ancestor that has never published, so without this the tree would exist but
  inherit nothing, exactly as invisible as the one-context state it replaces.
  Two live consumers hardcoded `"emea-acme"` and would have broken on retirement, found and fixed
  in the same pass: `shipping-service`'s `refdataContext` const (→ `"acme"`) and
  `frontend/refdata`'s `CONTEXTS` array (→ all three contexts, so the admin UI can browse the
  whole chain). `internal/domain/validation.go` also gained a shared `ValidateSubjectToken`
  (charset-only) that both `ValidateContextName` and `RegisterPlatformRoot` build on.
- **16e — shipping context value migration (DONE 2026-07-31).** Adopted the fully-qualified form
  (decision 12) throughout shipping-service: `global` → `acme`, `atlantic-fleet` →
  `acme-atlantic-fleet`, `pacific-fleet` → `acme-pacific-fleet` (the third literal wasn't named in
  this entry's original text but was migrated identically for consistency). Renamed every literal
  occurrence — `postgres/migrate.go`'s `seedDefaultPorts`, 8 Go test files' fixture consts,
  Swagger/doc-comment examples in `rest/handlers.go`/`sse.go`/`kv.go`/`browserrpc/adapter.go`,
  `auth-service/auth/token.go`'s doc comment (now `accounts-service/auth/token.go` post-Phase-19), and both frontends' `CONTEXTS` arrays
  (`seafreight-app/stores/port.js`, `admin/stores/dictionary.js`) — then regenerated Swagger docs.
  **Investigated but deliberately NOT built**, because it turned out to have no structural basis:
  the plan's "context seeding becomes tenant-aware" goal. `seedDefaultPorts` runs once at startup
  before any tenant connection exists, and shipping-service's Postgres schema (`ports`/`ships`/
  `containers`) has **no tenant column at all** — every tenant sharing this Postgres instance reads
  the same rows, scoped only by `context`. Tenant isolation for this data lives entirely in which
  NATS account a request authenticates into, not in a Postgres row; making per-tenant context
  seeding real would mean adding a tenant dimension to this schema, which is a materially bigger
  change than a value rename and was out of this phase's scope. Documented in `migrate.go`'s
  `seedDefaultPorts` doc comment so this isn't mistaken for an oversight later.
  **KV bucket rename**: confirmed no rename/migration mechanism exists anywhere in
  `internal/kvstore` — `Bucket()`'s `{prefix}-{context}` naming (`kv.go`) only creates-or-updates
  the new name; a `docker compose down -v` was required (and run) to get a clean environment
  rather than leaving stale old-named buckets silently reading empty.
- **16f — dynamic context list (DONE 2026-07-31).** Backend: `refdata-service` gained
  `ContextRepository.ListByTenant`/`ContextHandler.ListByTenant` (BR-D35, `BUSINESS_RULES-REFDATA.md`)
  — returns a tenant's own contexts plus the shared `_`-reserved platform roots — exposed on both
  transports (`GET /api/refdata/admin/contexts?tenant=`, `rpc._platform.refdata.context.list.v1`).
  `shipping-service` added `refdataconsumer.ListContexts` (calling that new rpc.* endpoint, BR-D28
  NATS-only) and a new `GET /api/refdata/contexts` REST endpoint (`listRefdataContexts`, BR-025)
  that resolves the caller's tenant via `refdataCompanyContext(deps.Tenant)`.
  **Extended scope beyond the plan's original wording**: the plan also flagged
  `refdataconsumer` passing a hardcoded `"acme"` company-context constant as itself "pending Phase
  16f" — fixed by deriving it from the active tenant (decision 11's "tenant name doubles as company
  context in the no-company-group case," the same mapping BR-AC07 relies on) instead of leaving it
  hardcoded, applied consistently across `listRefdataType`/`listRefdataLocales`/`listRefdataContexts`
  and the `watchRefdata` SSE subject filter.
  Frontend: `CONTEXTS` (previously hardcoded, just renamed in 16e) is now the offline/error
  fallback only — both `seafreight-app/stores/port.js` and `admin/stores/dictionary.js` gained an
  `availableContexts` reactive field and a `loadContexts()` action, fetched on tenant init/switch
  (not on a plain fleet-context change, to avoid refetching the same list a context switch is
  picking from) — both apps' `<Select>` dropdowns bind to the live list.
  **Known gap, explicitly documented rather than silently left implicit** (BR-025's "Known gap"
  paragraph): Sea Freight Flow (Phase 15d) no longer drives shipping-service's REST-side
  `Deps.Tenant`/`SwitchTenant` at all — it authenticates directly into its own NATS account. These
  refdata REST reads resolve tenant from `Deps.Tenant`, which the Admin/Dictionary frontend does
  control. Both happen to default to the same tenant (`acme`) today, so this reads correctly in the
  common case, but the two are not actually the same signal — if the Admin UI's tenant selection and
  Sea Freight Flow's own NATS tenant ever diverged, these three endpoints would reflect the Admin
  UI's selection. A real fix needs an explicit tenant threaded through the *shared*
  `useRefdataLabels`/`useL10nCopy` composables rather than server-side state; left as a documented
  seam, not fixed, since it's a pre-existing Phase 15 scope boundary (that phase already flagged
  refdata-service's cross-tenant DEFAULT-account model as out of scope), not something 16f
  introduced or was asked to resolve.
  `refdataconsumer` passes the real context instead of the hardcoded `emea-acme` it uses today
  (`consumer.go:19-20` — "demo-scoped to that context"). First time the frontend does not know its
  contexts synchronously, so it needs a genuine loading/error path.
- **16g — Sea Freight Flow Fleet panel tenant/context-switch flicker fix (DONE 2026-08-02, BR-029).**
  User report: "not sure if there's a momentary flicker on the fleet management panel" when switching
  tenants. Root-caused via a live DOM-mutation-observer capture on the running stack (not
  speculation): `usePortStore().connect()` (called by both `stores/tenant.js`'s `setTenant()` and
  `setContext()`) resets `ships`/`containers` to `{}` synchronously, before its `listShips`/
  `listContainers`/`getPorts`/`knownContainers` bootstrap reads land — so the `FleetPanel.vue`
  `DataTable`, which renders straight off `store.allShips`, briefly rendered its own empty state
  ("No ships match this filter.") mid-switch, misreading as "this tenant has none" rather than
  "still loading." Measured on localhost: ~80ms of stale old-tenant data during the NATS WebSocket
  re-authentication, then a ~10ms empty flash, then the new tenant's data — small locally, but a
  real, reproducible gap that widens under real network latency.
  Fix: `stores/port.js` gained a `loading` state — set `true` at the top of `connect()` (same point
  the reset happens), cleared via `Promise.allSettled(...).finally()` once all four bootstrap reads
  settle (success or failure). `FleetPanel.vue` renders a spinner + `fleet.loading` copy while
  `store.loading` is true, the `DataTable` otherwise (`v-if`/`v-else`, mutually exclusive) — reusing
  the exact spinner CSS convention from the Admin UI's `ServicesPanel.vue` (17c follow-up, same
  session) for visual consistency across both frontends. New `fleet.loading` l10n key added to
  `backend/refdata-service/refdata/seed.go` (en/es/af) and regenerated into
  `shared/refdata/l10nFallback.en.js` via `npm run gen:i18n` — resolves from the bundled fallback
  immediately; no live refdata-service reseed was needed since `useL10nCopy.js`'s catalog always
  starts from the bundled fallback and only overlays live-fetched keys on top.
  Verified live: rebuilt `shipping-frontend` (`docker compose build --no-cache shipping-frontend`,
  needed because the first `--build` reused a stale cached layer), confirmed via a DOM-mutation
  observer that the empty-state flash is gone and replaced by the spinner across a real acme→globex
  switch. Initially scoped to the Fleet Management panel only, per the user's report; the user then
  asked for the same fix on `ShipsAtPortPanel.vue`/`TerminalPanel.vue` — confirmed they read from the
  same `store.ships`/`store.containers` reset and have the identical gap, so the same `store.loading`
  flag was reused (no new store-level work needed) and both panels got the same spinner/DataTable
  `v-if`/`v-else` treatment. `ShipsAtPortPanel.vue` layers the spinner as a third branch alongside its
  pre-existing `!store.port` ("select a port") message; `TerminalPanel.vue` covers both its Outbound
  and Arrived tables with one shared spinner rather than two, since both come from the same reset.
  Re-verified live the same way (DOM-mutation observer, real acme→globex switch, `spinnerCount: 2`
  during the loading window, no "No ships docked here"/"No outbound containers" flash).
  - [x] `stores/port.js` — `loading` state + `Promise.allSettled` around the bootstrap reads
  - [x] `components/FleetPanel.vue` — spinner/DataTable `v-if`/`v-else`, spinner CSS
  - [x] `components/ShipsAtPortPanel.vue` — spinner as a third `v-if`/`v-else-if`/`v-else` branch, spinner CSS
  - [x] `components/TerminalPanel.vue` — one shared spinner covering both Outbound/Arrived tables, spinner CSS
  - [x] `backend/refdata-service/refdata/seed.go` + regenerated `l10nFallback.en.js` — `fleet.loading`,
        `shipsAtPort.loading`, `terminal.loading` keys
  - [x] `BUSINESS_RULES-SHIPPING.md` — BR-029 (updated to cover all three panels)
  - [x] Tests: `App.spec.js` (render-level, one test per panel: spinner shown/hidden, empty message
        never shown mid-switch), `stores/port.spec.js` (`connect()`'s `loading` lifecycle, success and
        failure paths) — 20/20 passing
  - [x] Live-verified via DOM-mutation-observer capture on the rebuilt `shipping-frontend` container,
        both for the initial Fleet-only fix and the Ships at Port/Terminal Yard extension
- **16h — reactive tenant provisioning (DONE 2026-08-02, BR-030/BR-AC08).**
  User report: "when I create a new tenant, the port selection dropdown is empty in the register ship
  dialog." Investigating live turned up something much bigger than an empty dropdown: shipping-service
  logs showed zero mentions of the new tenant at all, and `EnsureAllTenants`'s own doc comment already
  said why — "a tenant minted later by accounts-service is instead picked up the first time any
  SwitchTenant call names it... there is no background poll for newly minted tenants nobody has
  referenced yet." Sea Freight Flow never calls `SwitchTenant` (Phase 15d — it authenticates straight
  into NATS), so a brand-new tenant's `browserrpc.Adapter` genuinely did not exist on this process:
  every `api.*` request against it — ships, containers, ports alike — timed out after 5s and was
  swallowed silently by the browser's `.catch(() => {})`, reading as "this tenant has nothing" rather
  than "not provisioned yet." Confirmed live: `Initech.creds` was written well after shipping-service's
  last restart, and manually POSTing `/api/tenant/switch {"tenant":"Initech"}` (the Admin UI's own
  lazy-provisioning path) immediately fixed it — proving the mechanism worked, it just had no trigger
  for a tenant nobody had switched to yet.
  Presented two fix options (reactive event vs. a lazy browser-side "ensure" call before connecting);
  user chose the reactive event.
  Fix: `accounts-service` gained a second, DEFAULT-account NATS connection
  (`NATS_DEFAULT_CREDS_PATH`, optional/nil-safe) used only to publish `notify.accounts.account.created`
  (context-free subject — accounts-service has no `{context}` of its own) after a create fully commits
  (resolver JWT + creds file + Postgres row) — `accounts/handler.go`'s new `publishAccountCreated`,
  called from `createAccount` (BR-AC08). `shipping-service` subscribes to it on `mono.NC()` (its own
  DEFAULT-account connection) in `composition.go`'s `Module.Startup`, right after `EnsureAllTenants`,
  and calls a new `Handlers.EnsureTenantByName` (BR-030) — the same idempotent `ensureTenantResources`
  path `EnsureAllTenants` already uses, just triggered per-tenant instead of only at startup. No new
  docker-compose volume needed — accounts-service already bind-mounts the whole shared `./nats/creds`
  directory read-write, `default.creds` included; just a new env var.
  Verified live end to end on the running stack (not just via tests): created a fresh tenant via
  `POST /api/accounts`, confirmed both accounts-service's `accounts-service-default` connection and
  shipping-service's reaction in `/connz`, then used the `nats` CLI with that tenant's own freshly-minted
  creds to `nats req api.acme.shipping.ship.list.v1 '{}'` — got a reply in ~20ms, `SwitchTenant` never
  called for it — and confirmed the Register Ship dialog's port dropdown populated immediately in the
  browser.
  - [x] `backend/accounts-service/accounts/handler.go` — `Handlers.NotifyNC` field, `publishAccountCreated`,
        called from `createAccount` after `Store.Insert` succeeds
  - [x] `backend/accounts-service/cmd/main.go` — second DEFAULT-account connection, `NATS_DEFAULT_CREDS_PATH`
  - [x] `backend/shipping-service/dictionary/internal/rest/tenant.go` — `Handlers.EnsureTenantByName`
  - [x] `backend/shipping-service/dictionary/composition.go` — `notify.accounts.account.created` subscription
  - [x] `docker-compose.yml` — `NATS_DEFAULT_CREDS_PATH` env var for accounts-service
  - [x] `BUSINESS_RULES-ACCOUNTS.md` — BR-AC08; `BUSINESS_RULES-SHIPPING.md` — BR-030
  - [x] Tests: accounts-service `handler_test.go` (event published only after a successful create, with
        the right payload; create still succeeds with `NotifyNC` nil) — 29/29 passing. shipping-service
        `dictionary/tenant_switch_test.go` (`EnsureTenantByName` makes globex's adapter answer without
        `SwitchTenant` ever being called; no-op for an unknown tenant name) — full suite (5 packages)
        green.
  - [x] Live-verified end to end: `/connz`, `nats req` with a freshly-minted tenant's own creds, and the
        Register Ship dialog in the browser — no restart, no `/api/tenant/switch` call involved.
- **16i — reactive tenant teardown (DONE 2026-08-03, BR-031/BR-AC09).** The mirror of 16h, found while
  investigating a follow-up question: "what's supposed to happen when an RPC is sent from a suspended
  account?" Answering it live turned up a second architectural gap symmetric to 16h's. NATS force-evicts
  every connection on an account the instant `$SYS.REQ.CLAIMS.DELETE` revokes it (confirmed on the running
  stack — a live `nats sub` dropped within ~3s of a suspend call) — this directly contradicted
  `Provisioner.DeleteAccount`'s own doc comment and `BUSINESS_RULES-ACCOUNTS.md`'s BR-AC03, both of which
  claimed existing connections continue; both were corrected as part of this phase. The browser side was
  already correct by accident (`connectInfo`'s 403 refuses re-authentication), but its refusal only set
  `useNatsConnection.js`'s `lastError`, which nothing rendered — a suspended session's panels just went
  quiet. The real bug was on shipping-service's side: its per-tenant connection, evicted like any other,
  had no equivalent gate, so `nats.go`'s default reconnect logic retried forever against a `.creds` file
  `suspendAccount` had already deleted — one permanent, log-spamming loop per suspension, previously
  cleared only by a restart (`UmbrellaTest2`/`suspendtest` had been looping for an entire prior session
  before this phase's `docker compose restart shipping-service`).
  Sequence diagrams (current + proposed) were sketched first and added to `ARCHITECTURE-ACCOUNTS.md` § 2t-a
  before any code changed, per this repo's usual "diagram/rules first, implementation second" flow; user
  confirmed the reactive-event approach (same shape as 16h) before implementation began.
  Fix: `accounts-service` publishes `notify.accounts.account.suspended` (BR-AC09) — same subject family,
  same `Handlers.NotifyNC` connection as 16h's created event — right after `Store.SetStatus` marks the
  account suspended. `shipping-service` subscribes to it in `composition.go` (right after the BR-030
  subscription) and calls a new `Handlers.TeardownTenantByName` (BR-031): stops that tenant's projectors
  and `browserrpc.Adapter`, then explicitly closes shipping-service's own connection to that account —
  the explicit `Close()` is what actually disables `nats.go`'s auto-reconnect; eviction alone doesn't.
  `App.vue` also now renders `useNatsConnection.js`'s `lastError` as a danger `Tag` in the topbar whenever
  non-empty, clearing itself once a connection succeeds again (no new state needed — `connect()` already
  resets `lastError` to `''` on success).
  Deliberately left out of scope: a terminal-vs-transient error classification as a backstop for a missed
  or out-of-band suspension (e.g. an operator revoking an account directly via `nsc`). The event covers the
  normal path; the backstop is a separate, larger design decision, sketched in `ARCHITECTURE-ACCOUNTS.md`
  § 2t-a's "Proposed" section but not implemented.
  - [x] `backend/accounts-service/accounts/handler.go` — `publishAccountSuspended`, called from
        `suspendAccount` after `Store.SetStatus` succeeds
  - [x] `backend/accounts-service/accounts/provisioner.go` — corrected `DeleteAccount`'s doc comment
  - [x] `backend/shipping-service/dictionary/internal/rest/tenant.go` — `Handlers.TeardownTenantByName`
  - [x] `backend/shipping-service/dictionary/composition.go` — `notify.accounts.account.suspended` subscription
  - [x] `frontend/seafreight-app/src/App.vue` — `lastError` danger `Tag`, `data-testid="connection-error"`
  - [x] `backend/refdata-service/refdata/seed.go` + regenerated `l10nFallback.en.js` — `connection.error` key
  - [x] `BUSINESS_RULES-ACCOUNTS.md` — BR-AC03 corrected, BR-AC09 added; `BUSINESS_RULES-SHIPPING.md` — BR-031
  - [x] `ARCHITECTURE-ACCOUNTS.md` — § 2t corrected, § 2t-a added (current + proposed sequence diagrams,
        rendered and verified with `mmdc`)
  - [x] Tests: accounts-service `handler_test.go` (BR-AC09's mirrored pair — suspend of an unknown account
        publishes nothing; a successful suspend publishes the tenant's name; suspend with a nil `NotifyNC`
        still succeeds) — 31/31 passing. shipping-service `dictionary/tenant_switch_test.go`
        (`TeardownTenantByName` makes globex's adapter go silent by actually closing shipping-service's own
        connection, not just local bookkeeping; no-op for a never-provisioned tenant) — full suite (5
        packages, 107 specs) green. `frontend/seafreight-app` `App.spec.js` (`lastError` Tag appears/clears,
        distinguished from the pre-existing connection-status Tag which is also danger-severity while
        disconnected) — 11/11 passing in that file.
  - [x] Live-verified the underlying eviction/loop behavior on the running stack before implementing
        anything: a throwaway tenant's live `nats sub` connection dropped within ~3s of suspension, and
        shipping-service logged the ENOENT reconnect loop against its deleted `.creds` file.
- **16j — reactive tenant restore (DONE 2026-08-03, BR-032/BR-AC10).** Closes the lifecycle triple, and
  fixes a regression 16i itself introduced — found immediately afterward during a requested architecture
  review of the accounts area rather than by a user report. 16i's teardown is correct but was a **one-way
  door**: `EnsureAllTenants` runs only at startup and Sea Freight Flow never calls `SwitchTenant`
  (Phase 15d), so nothing rebuilt a tenant that came back, leaving a suspend→reactivate cycle dark until
  a restart. Ironically 16i made this worse than before — the pre-16i reconnect loop was ugly but would
  have self-healed once creds returned; a clean teardown made the missing counterpart load-bearing.
  Fix: `accounts-service`'s `reactivateAccount` publishes `notify.accounts.account.reactivated` after the
  whole reactivation commits — deliberately *after* the fresh `.creds` write, since the consumer resolves
  tenants by scanning that directory (asserted in the spec, not just commented). `shipping-service`
  subscribes and calls the existing `EnsureTenantByName` **unchanged** — 16i's teardown removed the tenant
  from `TenantResources`, so the ordinary first-sight path rebuilds it against the new credentials. No new
  provisioning code, just a third trigger for the same idempotent path. The three publishers were also
  de-triplicated behind one nil-safe `publishAccountEvent` helper, keeping each event's own doc comment.
  - [x] `backend/accounts-service/accounts/handler.go` — `publishAccountReactivated` + shared
        `publishAccountEvent` helper; called from `reactivateAccount` after `SetStatus(active)`
  - [x] `backend/shipping-service/dictionary/composition.go` — `notify.accounts.account.reactivated`
        subscription
  - [x] `BUSINESS_RULES-ACCOUNTS.md` — BR-AC10; `BUSINESS_RULES-SHIPPING.md` — BR-032;
        `ARCHITECTURE-ACCOUNTS.md` § 2t-a — reactivation asymmetry marked resolved, lifecycle table added
  - [x] Tests: accounts-service `handler_test.go` (a failed reactivation publishes nothing; a successful
        one publishes the name **and** has already written the creds file by the time the event is
        observable; nil `NotifyNC` still succeeds) — 33/33. shipping-service `tenant_switch_test.go` —
        full round trip (answering → dark → answering again, `SwitchTenant` never called), then a ship
        arrival driven through `api.*` and awaited in the read model to prove the *projectors* came back
        too, not just the adapter — 108 specs green.
  - [x] Fixed a pre-existing ~1-in-4 flake in BR-030's spec while here: `micro.AddService` doesn't flush
        its subscriptions before returning, so a request issued in the same instant can get
        `no responders`. BR-030/031/032 specs now poll with `Eventually`; verified 10/10 clean full-suite
        runs (previously reproducible within 4). Documented as a note under BR-032 so it isn't
        re-introduced.
- **16k — connection honesty in Sea Freight Flow (DONE 2026-08-03, BR-033).** Follow-on from 16i, prompted
  by a user question rather than a report: is `Depart failed / not connected` expected on a suspended
  account? Functionally yes (fail-closed is correct), but tracing it found the app communicating badly in
  two ways, one of which was a genuine bug.
  (a) **The status badge lied.** Two independent `connected` flags exist — `useNatsConnection`'s (clears
  correctly on eviction) and `usePortStore().connected` (cleared only by the store's own `disconnect()`,
  which nothing calls on eviction). The topbar read the latter alone, so after a suspension it showed a
  green "watching" badge directly beside 16i's red "connection error" badge — visible in this session's own
  verification screenshot and initially missed. Fixed with a `watching = natsConnected && store.connected`
  computed.
  (b) **Command failures named the symptom, not the cause.** `request()` threw a bare `not connected`
  whenever `nc` was null; the real reason (auth-service's `tenant is not active`) was already in
  `lastError` but only reachable via a tooltip. Fixed centrally in `notConnectedError()` so every command
  — arrive, depart, register, load, unload — inherits it, rather than patching each panel's catch block.
  `lastError` now stores `err.message`, not `String(err)`, dropping the `Error: ` prefix from
  operator-facing text.
  Deliberately deferred: disabling action controls while disconnected (a broader interaction change across
  every panel; these two were the correctness bugs).
  - [x] `frontend/seafreight-app/src/App.vue` — `watching` computed, `data-testid="connection-status"`
  - [x] `frontend/seafreight-app/src/nats/useNatsConnection.js` — `notConnectedError()`, `errorMessage()`
  - [x] `BUSINESS_RULES-SHIPPING.md` — BR-033
  - [x] Tests: `App.spec.js` (drives the exact contradictory state — NATS down, port store still
        "connected" — and asserts the badge reads `disconnected`); new `src/nats/useNatsConnection.spec.js`
        (3 specs: bare fallback, auth-service's refusal surfacing in its place with the transport symptom
        gone from the message, and the same for `subscribe()`) — 18 passing across the three touched files,
        up from 14

---

### Phase 17 (17a/17b/17c DONE 2026-08-01) — Request/Reply Panel v2, Connections + Services Panels

#### Goal

The Admin UI's Request/Reply panel (renamed from "RPC Traffic" 2026-08-01, when it also
gained the live `obs.api.>` half) shows one row per correlated call but stops there: no
headers, no filtering, and payload inspection is a cramped in-cell expand. Rebuild it as a
proper traffic inspector — DevTools/Fiddler/Datadog-class — with a detail view whose
layout mirrors what the obs channel structurally *is*: two correlated messages.

#### Design decisions (2026-08-01)

Three layouts were evaluated (right drawer / bottom paired panes / facet-rail + flyout).
**Chosen: bottom detail split with side-by-side Request | Reply panes, plus
token-facet filtering** — the paired panes are the one layout that reflects the
two-message structure of the channel, the full-width table handles NATS-length subjects,
and click-a-token filtering exploits the fixed-arity subject taxonomy (Phase 16a) instead
of bolting on a Datadog-style facet rail that would fight the AppShell sidebar.
**Approved static reference: `demos/01-dictionary/frontend/admin/request-reply-reference.html`**
(same convention as `shared/unifi-theme/app-shell-reference.html`) — built on the UniFi
theme tokens; the implementation should match it, not re-derive it.

Key elements, per the reference:

- **Filter bar**: free-text subject match; toggle chips for family (`rpc`/`api`) and
  status (`ok`/`error`/`pending`); **clicking any subject token** (in a row or the detail
  head) adds a positional facet chip (`family:`/`context:`/`service:`/`entity:`/`action:`)
  — the subject *is* the filter, per the fixed-arity taxonomy; a **pause** control that
  freezes the visible list without dropping the SSE (filtering a stream that's still
  prepending rows is otherwise unusable).
- **Table**: adds family badge, ms-precision time, **latency**, and request⁄reply
  **sizes** to today's status/subject/time columns; selected row carries the accent
  inset bar; in-cell payload expand is removed (payloads move to the detail).
- **Detail split (bottom, ~46%)**: header strip (subject, status, latency, correlationId,
  tenant, live/replay source) over two panes — **→ Request** and **← Reply** — each with
  a Headers key/value table and a syntax-tinted JSON body, copy affordances, and an
  error banner on the reply pane for failed calls (surfacing NATS micro's real
  `Nats-Service-Error`/`Nats-Service-Error-Code` headers).

**Data prerequisite (the real work): the obs envelope is too thin.** Today's
`obsEnvelope` (both `browserrpc/adapter.go` and refdata's `natsrpc/adapter.go`) carries
only `direction`/`correlationId`/`subject`/`payload`/`error` — no headers, no
server-side timestamp (the UI clocks arrival time, which lies for replayed events), no
sizes. Latency and both Headers sections have nothing to render until the envelope
gains `headers map[string][]string`, `timestamp` (publisher-side), and `payloadBytes`;
sizes and latency then derive client-side. Envelope changes are additive (new optional
fields), so old retained RPCTRACE events still parse — no migration.

#### Sub-phases

- **17a — obs envelope extension (backend, DONE 2026-08-01).** `obsEnvelope` in both
  adapters gained `headers` (request: `req.Headers()`; reply: `nil` on success, the real
  outgoing error headers on failure — see below), publisher-side `timestamp`
  (`time.Now().UTC()` at publish), and `payloadBytes` (`len(payload)`). New rule
  **BR-D36** (`BUSINESS_RULES-REFDATA.md`) + mirror **BR-026**
  (`BUSINESS_RULES-SHIPPING.md`) — confirmed with the user before implementation per the
  workflow gate (BR-D31 was already taken by the enum-namespace rule from Phase 12.14;
  landed as BR-D36/BR-026 instead). **Extended beyond the original wording**:
  `respondError` in both adapters now also attaches the real
  `Nats-Service-Error`/`Nats-Service-Error-Code` headers to the actual wire reply (via
  `micro.WithHeaders`, additive to the existing JSON error body) — not just the
  observability copy, so the panel shows headers that genuinely traveled on the wire.
  Tests: `natsrpc_test.go`'s new `BR-D36` context and `browserrpc_test.go`'s new `BR-026`
  context (request headers/timestamp/size; reply error-headers on both the obs event and
  the real wire reply; old-shape envelope with none of the three fields still decodes).
  Verified live end-to-end against the running stack via the `nats` CLI.
- **17b — panel rebuild (frontend, DONE 2026-08-01).** `RpcPanel.vue` rebuilt to match
  the reference: filter bar (text + family/status chips + token-facet chips + pause),
  upgraded table (family badge, ms-precision time, latency, sizes), bottom detail split
  with paired Request/Reply panes (headers table, syntax-tinted JSON body, copy, error
  banner). `SubjectPath.vue` gained an opt-in `clickable` prop + `token-click` emit
  (`.stop`-guarded so a token click doesn't also select the row), additive so
  `StreamView.vue`'s existing non-clickable usage is unaffected. Facets are positional
  per the fixed 6-token arity (index 0 = family toggles the existing rpc/api chip instead
  of a redundant facet; indices 1–5 = context/service/entity/action/version). Pause
  freezes the *visible* list only — `rowsById`/`order` keep updating live underneath, so
  an already-open detail pane still resolves. **Known gap, not fixed in this phase: no
  Vitest specs** — the admin app has no test runner configured at all (`package.json` has
  no `test` script, no vitest devDependency), unlike `seafreight-app`/`refdata` which do;
  none of admin's other 10+ panels have component tests either, so this follows existing
  (imperfect) project convention rather than introducing new test infrastructure
  unrequested. Verified instead via live browser testing against the real running stack:
  filtering (text, family/status toggles, token-click facets with toggle-off), pause/
  resume, row selection, paired detail panes, real error headers, and — unprompted but
  informative — the old-envelope backlog rows from before the 17a redeploy rendering
  correctly with "—" placeholders, proving the additive-migration requirement live rather
  than just in a test.

#### Checklist

- [x] Confirm BR wording for the envelope extension (landed as BR-D36 + BR-026)
- [x] 17a: `obsEnvelope` + both adapters emit headers/timestamp/payloadBytes; adapter tests
- [x] 17a: `rpc_watch_test.go` still green; old envelopes still parse end-to-end
- [x] 17b: `RpcPanel.vue` rebuilt to match `request-reply-reference.html`
- [x] 17b: `SubjectPath.vue` token-click filtering; facet chips; pause control
- [ ] 17b: Vitest specs — **not done**, admin app has no test infra at all (see 17b note above)
- [x] `BUSINESS_RULES-REFDATA.md` (BR-D36) + `BUSINESS_RULES-SHIPPING.md` (BR-026) updated
- [x] `ARCHITECTURE-COMMUNICATIONS.md` §6 updated (envelope fields, panel capabilities)
- [x] `go build`/`ginkgo` (shipping), `go test` (refdata), admin `npm run build` all green
- [x] Live browser verification: rpc.* + api.* rows, headers visible, token-filter, pause

- **17c — Connections + Services panels (DONE 2026-08-01).** Two new sidebar
  entries under NATS, after Request/Reply: **Connections** ("what's attached
  to the server right now" — every raw connection) and **Services**
  ("what does each service deliberately offer" — micro-registered endpoints
  + live counters). Researched Synadia Insights first: it groups
  Connections and Services as sibling nav entries under a CLIENTS heading
  (confirmed via a screenshot the user shared), which validated putting
  both under this app's NATS eyebrow — but Insights' own Services view
  isn't publicly documented and, per a second research pass, may not
  actually expose `$SRV`-level endpoint stats the way this panel does, so
  the internals here aren't copied from anywhere, just the *grouping*
  decision.

  **Connections** (`GET /api/nats/connections`) proxies the NATS server's
  own HTTP monitoring endpoint (`/connz?subs=true&auth=true`) — a new
  `NatsMonitorURL` plumbed through `monolith.Monolith` → `cmd/main.go`
  (env `NATS_MONITOR_URL`, defaults to `http://localhost:8222`) →
  `rest.Deps` → `docker-compose.yml` (`http://nats:8222`, the container-network
  hostname). Server-wide, not tenant-scoped — a `/connz` snapshot spans
  every account, unlike every other panel in this app. A plain REST poll
  (10s), not SSE: `/connz` is a single request/reply snapshot with no
  native push model, unlike the KV/JetStream watches.

  **Services** (`GET /api/nats/services`) broadcasts a `$SRV.STATS`
  discovery request — the bare, name-less control subject
  (`micro.ControlSubject(micro.StatsVerb, "", "")`), which every
  registered instance replies to because `nats.go/micro`'s discovery
  subscriptions are deliberately unqueued (unlike its queued business
  endpoints) — and collects replies for a 500ms window, the same protocol
  the `nats micro stats` CLI uses. Queried on both `deps.NC` (DEFAULT —
  where refdata-service's `natsrpc` adapter registers) and
  `deps.TenantNC` (the active tenant — where shipping-service's
  `browserrpc` adapter registers); results are deduped by
  `(service name, instance ID)` since both connections can observe the
  same instance depending on account topology.

  **accounts-service now registers too** (`micro.AddService`, registration
  only — no endpoints, since its provisioning API is REST-only, not
  `rpc.*`/`api.*`) — but on the **SYS account** it already holds for
  `$SYS.REQ.CLAIMS.*` operations. `$SRV` subjects don't cross NATS account
  boundaries, so this admin backend's NC/TenantNC query connections can't
  see it: a **known, accepted gap**, not a bug — confirmed via
  `AskUserQuestion` rather than adding a third, SYS-scoped query connection
  to the admin REST layer (a real privilege-scope tradeoff, not just more
  code). **auth-service was deferred entirely**: unlike accounts-service it
  has no NATS connection at all today (pure JWT/NKey signing via
  `nats-io/jwt` + `nats-io/nkeys`, no `nats.go` client), so registering it
  would mean adding a live connection first, not just one `micro.AddService`
  call — confirmed via the same `AskUserQuestion` as a follow-up, not
  in-scope here.

  No new `BUSINESS_RULES-*.md` entry at the time, consistent with 17b's
  precedent: an envelope/wire-protocol change gets a BR (17a's
  BR-D36/BR-026); admin/observability panel UI does not, since it encodes
  no new domain constraint. **Revisited below** — once the tenant-labeling
  follow-up added real, non-trivial behavior (account resolution, a
  fallback rule, cross-panel consistency), the user asked for it to be
  formalized after all, landing as BR-028 — explicitly scoped to *Admin UI
  presentation*, not a wire-protocol rule, so this paragraph's reasoning
  wasn't wrong, just superseded once the feature grew past "pure plumbing."

#### 17c Checklist

- [x] `NatsMonitorURL` plumbed: `monolith.Monolith` interface, `cmd/main.go`
      (env `NATS_MONITOR_URL`), `composition.go` → `rest.Deps`,
      `docker-compose.yml` (`http://nats:8222`)
- [x] `GET /api/nats/connections` — connz proxy, reshaped to camelCase,
      sorted by `cid`; 502 on unreachable/malformed monitoring endpoint
- [x] `GET /api/nats/services` — `$SRV.STATS` fan-in over NC + TenantNC,
      deduped by `(name, instance ID)`
- [x] accounts-service: `micro.AddService` registration (no endpoints) on
      its existing SYS-account connection
- [x] auth-service: `micro.AddService` — **superseded by Phase 19**, not
      built as originally scoped here. auth-service's routes now run inside
      accounts-service's own process (which already registers on its
      SYS-account connection), so the "needs a NATS connection added first"
      blocker no longer applies — there's no separate binary left to
      register.
- [x] `nats_ops_test.go`: connz reshape/sort + 502 error paths (mocked);
      `$SRV.STATS` fan-in against a real embedded NATS server + real
      `micro.AddService` instance (request/error counters, cross-connection
      dedup, nil-connection safety)
- [x] `ConnectionsPanel.vue` + `ServicesPanel.vue` +
      `IconConnections.vue`/`IconServices.vue`
- [x] Wired into `App.vue`'s NATS sidebar group, after Request/Reply
- [x] `go build`/`ginkgo` (shipping-service), `go build`/`go test`
      (accounts-service), admin `npm run build` all green
- [x] Live browser verification: both panels rendering real data against
      the running stack (containers rebuilt with `NATS_MONITOR_URL` +
      accounts-service registration) — Connections shows all 7 live
      connections incl. accounts-service (visible in `/connz` even though
      its micro registration isn't $SRV-discoverable, confirming the two
      data sources are genuinely independent); Services shows
      refdata-service (5 endpoints, real request/latency counts) and
      shipping-service (12 endpoints) with expand/collapse working

#### 17c follow-up — tenant labeling (DONE 2026-08-02)

The user noticed the Connections panel showing 3 bare `shipping-service`
rows with no way to tell which is the DEFAULT-only connection
(`refdataconsumer`'s `rpc.*` client) versus which tenant's `browserrpc`
adapter each of the other two is — every `nats.Connect` in this codebase
correctly sets `nats.Name("shipping-service")` per the CLAUDE.md rule, but
that rule only guarantees a connection is attributable to a *service*, not
to a *tenant* within it. Three options were weighed:

- **(A) suffix `nats.Name()` per tenant** (e.g.
  `"shipping-service/tenant:acme"`) — rejected: `browserrpc/adapter.go`
  deliberately pins `micro.Config.Name` to the literal `"shipping-service"`
  specifically to match `nats.Name("shipping-service")` (Phase 18's
  `Nats-Responder` invariant — the two identities must never diverge, or
  the Request/Reply panel reads as if one service is two). Suffixing the
  connection name alone would silently break that invariant again.
- **(B) resolve a friendly label server-side, without touching
  `nats.Name()`** — **done**, then generalized past shipping-service's own
  rows on a follow-up prompt ("what about the rest of the services"). First
  pass matched by local socket address alone, so only shipping-service's own
  3 connections got labeled — refdata-service, the `nats` CLI, and any
  browser tab (all sharing DEFAULT or a tenant account with a connection
  shipping-service itself holds) still showed a raw NKey. `nats_ops.go`'s
  `tenantLabelsByAccount` is now two stages: (1) match `/connz` rows to
  shipping-service's own connections (`Deps.NC` → `"DEFAULT"`, each
  `TenantResources[name].nc` → that tenant's name) by **local socket
  address** (`nc.LocalAddr()` is exactly what the server reports back as
  that connection's `ip:port` — same TCP socket, both ends), establishing
  "this account NKey means DEFAULT / means acme"; (2) apply that mapping by
  **account**, not address, to every row in the full `/connz` list — so
  refdata-service and the CLI (DEFAULT) and any tenant-authenticated browser
  tab resolve too, not just shipping-service's own rows. Sidesteps JWT/NKey
  decoding entirely either way. Surfaced as `tenantLabel` on
  `GET /api/nats/connections`; the frontend's Account column prefers it over
  the raw account NKey. accounts-service is the one row that stays
  unresolved — it authenticates on the SYS account,
  which shipping-service holds no connection on, so there's no known
  mapping to apply (same account-boundary shape as the Services panel's
  `$SRV` gap, just showing up as "stays raw" instead of "doesn't appear").
- **(C) `micro.Config.Metadata` tenant tag, Services panel** — **done**.
  `browserrpc.Deps` gained a `Tenant string` field (threaded from
  `tenant.go`'s `ensureTenantResources`, which already has the tenant name
  in scope), attached as `Metadata: {"tenant": <name>}` on the
  `micro.AddService` call — deliberately metadata, not `Config.Name`, to
  leave the Phase 18 invariant alone. `micro.Stats.ServiceIdentity` already
  carries `Metadata`, so `listNatsServices` needed only to pass it through
  (`natsServiceInstance.Metadata`); `ServicesPanel.vue` shows it as a small
  tag next to the instance ID.

Tests: `TestListNatsConnectionsLabelsAnyConnectionSharingAKnownAccount`
(opens two *real* connections against the embedded test server so
`LocalAddr()` is a real ephemeral port, not a fabricated value; the mocked
`/connz` reports those addresses back to seed the account map, then a
fourth row simulating refdata-service — DEFAULT account, but an address
this process never held — proves the account fan-out labels it too; a fifth
row on a genuinely unrelated account proves accounts-service-shaped
connections stay unlabeled rather than mismatched),
`TestTenantLabelsByAccountSkipsNilTenantEntriesAndUnownedAccounts`,
`TestListNatsServicesPassesThroughInstanceMetadata`. `go build`/`ginkgo`
all green (one pre-existing parallel-run flake unrelated to this change,
per its earlier note in Phase 17b — reran green); admin `npm run build`
green. Verified live: refdata-service, the `nats` CLI, and the browser's
websocket connection all now show `DEFAULT`/`acme` instead of a raw NKey;
accounts-service correctly still shows raw (SYS account, out of reach).

#### 17c coverage audit (DONE 2026-08-02) — formalized as BR-028, closed three real gaps

The user asked for a deliberate coverage audit against this phase's
functional/business requirements — not a general "add more tests" request,
but specifically: is the account→friendly-name behavior actually a tested
requirement, or just incidentally covered? The audit surfaced three real
gaps, all closed:

1. **The rule itself had no formal BR**, so there was no single place
   asserting "this must hold" independent of any one test file. **Landed as
   BR-028** in `BUSINESS_RULES-SHIPPING.md`, explicitly scoped to *Admin UI
   presentation* — the user confirmed this scope directly ("only when
   presented in the UI"), so it does not claim anything about the wire
   protocol BR-027 already governs.
2. **The production wiring seam was untested.** `nats_ops_test.go`'s
   existing tests proved the REST handler's reshaping/pass-through logic
   was correct given *any* input, but nothing proved `tenant.go`'s
   `ensureTenantResources` actually threads the real tenant name into
   `browserrpc.Deps.Tenant` in production — a dropped field there would
   have silently broken the Services panel's tenant tag with no test
   catching it. Closed by a new `BR-028` Ginkgo context in
   `dictionary/browserrpc_test.go` that sends a real `$SRV.PING` over the
   wire and asserts `Metadata["tenant"]` equals the fleet context —
   verifying the actual wiring, not a synthetic `micro.AddService` call.
3. **The admin app had zero frontend test infrastructure**, so neither
   panel's rendering logic (prefer the resolved label, fall back to the raw
   NKey, render each with different markup) had any coverage at all —
   pure backend-API coverage doesn't prove the UI actually uses what the
   API returns. Vitest + `@vue/test-utils` + `happy-dom` added to
   `frontend/admin/`, mirroring `seafreight-app`'s/`refdata`'s existing
   config exactly (same versions, same `test`/`test:watch` scripts, same
   `environment: 'happy-dom'`) rather than inventing a new convention —
   this was previously an explicit known gap (17b's checklist note: "admin
   app has no test infra at all"), now closed. New
   `ConnectionsPanel.spec.js` (8 tests) and `ServicesPanel.spec.js` (6
   tests) cover BR-028's rendering plus filtering, row selection/detail
   pane, expand/collapse, and error states — all passed on the first real
   run against real PrimeVue `DataTable` in `happy-dom`, no mocking of the
   components under test themselves.

A fourth item was investigated and intentionally left as-is:
**accounts-service's `micro.AddService` call had no regression test**,
consistent with this repo's existing convention that `cmd/main.go`
bootstrap wiring isn't unit-tested anywhere (shipping-service's own
`main.go` has the same gap) — but "consistent with convention" isn't the
same as "actually verified," so this was checked live first
(`nats micro info accounts-service --creds sys.creds`, confirming the
registration genuinely responds on the SYS account, not just that
`main()` didn't error at startup) before deciding what to do. Since the
user asked for a test here too, the registration was **extracted** out of
`main.go` into a new, directly testable `accounts.RegisterMicroService(nc
*nats.Conn) (micro.Service, error)` (`accounts-service/accounts/service.go`)
— mirroring this repo's own architecture convention (bootstrap wiring in
`main.go` stays thin; testable logic lives in a package a test can import).
New `accounts/service_test.go` reuses the existing operator-mode embedded
server helper (`newOperatorTestServer`/`.ConnectSys`, already established
by `provisioner_test.go`) so the test authenticates as a real SYS account,
not a plain unauthenticated one — a permissions regression on that account
would be caught here, which the previous "no error at startup" check could
not have caught. Two specs: responds to `$SRV.PING` with the right
name/version; registers zero endpoints (registration-only, by design).

- [x] BR-028 written up in `BUSINESS_RULES-SHIPPING.md`, index updated in
      `BUSINESS_RULES.md`
- [x] `dictionary/browserrpc_test.go`'s Context renamed to `BR-028: ...`
      (matching the BR-025/026/027 naming convention — Ginkgo Context
      strings are how this codebase makes a rule searchable)
- [x] Vitest infra added to `frontend/admin/` (`package.json`,
      `vite.config.js`) — mirrors `seafreight-app`/`refdata` exactly
- [x] `ConnectionsPanel.spec.js` (8 tests), `ServicesPanel.spec.js`
      (6 tests) — all green; `npm run test` and `npm run build` both green
- [x] Found and fixed a real bug while writing the frontend tests:
      `ConnectionsPanel.vue` defined an `accountLabel()` helper that was
      never actually called — the template duplicated its logic inline
      instead. Removed the dead function; fixed the stale comment above it
      that still described the pre-generalization `tenantLabelsByLocalAddr`
      by name
- [x] `accounts-service/accounts/service.go` — `RegisterMicroService`
      extracted out of `cmd/main.go`
- [x] `accounts/service_test.go` — 2 new specs against a real SYS-account
      connection; full `accounts-service` suite green (27/27)
- [x] `go build`/`ginkgo` (shipping-service), `go build`/`ginkgo`
      (accounts-service), admin `npm run test` + `npm run build` all green
- [x] Live-reverified after the `accounts-service` refactor: rebuilt the
      container, confirmed `nats micro info accounts-service` still
      responds correctly post-extraction

---

### Phase 18 (DONE 2026-08-01) — Requestor/Responder Identity Headers

#### Goal

BR-D36/BR-026 (Phase 17a) made a message's real headers visible in the Request/Reply
panel, but no header actually identified *who* sent or answered a call — NATS doesn't
attach caller/responder identity to a message on its own; that's connection-level auth
state a handler's `Msg` never sees. Add explicit `Nats-Requestor` (on every request) and
`Nats-Responder` (on every reply) header, matching the convention BR-D36 already
established for real, wire-carried headers rather than observability-only ones.

#### Design decisions (2026-08-01)

- **`Nats-Requestor`** is set by the caller, instance-qualified (`"<name>/<instance ID>"`,
  matching `Nats-Responder`'s format and OpenTelemetry's `service.name`/`service.instance.id`
  split — added 2026-08-01 after review flagged that a bare name couldn't distinguish
  replicas): `refdataconsumer` (shipping-service's `rpc.*` caller) combines the connection's
  own `nats.Name(...)` with a NUID generated once per `Consumer`; `useNatsConnection.js`'s
  `request()` (the browser's `api.*` caller) combines `"seafreight-app"` with a random ID
  generated once per tab — so concurrent tabs are tellable apart. Tenant isn't included,
  since that's already the NATS account boundary itself and would just repeat what the
  account already encodes.
- **`Nats-Responder`** is set by the answering adapter on every reply, success or error alike,
  as `"<service's own nats.Name>/<micro.Service instance ID>"`. The subject alone already
  identifies which *service* answers a given `rpc.*`/`api.*` family in this repo (there's no
  fan-out), so the new information is the *instance* — `micro.AddService` generates a fresh
  unique ID per running process, letting the panel distinguish replicas if this ever scales
  horizontally.
- **Naming-inconsistency fix, found while implementing:** each adapter's `micro.Config.Name`
  (`refdata-rpc`, `shipping-api` — family-derived) didn't match its own connection's
  `nats.Name` (`refdata-service`, `shipping-service`). Left as-is, `Nats-Requestor` and
  `Nats-Responder` would show two different names for the same physical service — the panel
  would read as if the requestor and responder were different entities. Both `Config.Name`
  values were renamed to match their connection's `nats.Name` exactly as part of this phase.

#### Checklist

- [x] Confirm design (Nats-Requestor on request, Nats-Responder on reply) with user
- [x] `refdataconsumer.requestRPC` sets `Nats-Requestor` (shipping-service's `rpc.*` caller)
- [x] `useNatsConnection.js`'s `request()` sets `Nats-Requestor` (browser's `api.*` caller)
- [x] Requestor made instance-qualified too (`<name>/<NUID|per-tab ID>`) — symmetric with
      responder; `TestLookupCarriesInstanceQualifiedRequestorHeader` asserts format + stability
- [x] `natsrpc`/`browserrpc` `respondOK`/`respond`/`respondError` set `Nats-Responder`
- [x] Found and fixed `micro.Config.Name` mismatch (`refdata-rpc`→`refdata-service`,
      `shipping-api`→`shipping-service`)
- [x] Confirm BR wording (landed as BR-D37 + BR-027)
- [x] `BUSINESS_RULES-REFDATA.md` (BR-D37) + `BUSINESS_RULES-SHIPPING.md` (BR-027) updated
- [x] `ARCHITECTURE-COMMUNICATIONS.md` §6 updated
- [x] `go build`/`ginkgo` (both services), admin/seafreight-app `npm run build` all green
- [x] Live verification: both headers observed on the real wire (`nats` CLI + Admin panel)
      for `rpc.*` and `api.*`, both success and error replies

---

### Phase 19 (DONE 2026-08-03) — Merge auth-service into accounts-service

#### Goal

`auth-service` (Phase 15c) and `accounts-service` (Phase 14b) were reviewed
for whether the split still earned its keep. auth-service's only real job —
minting browser NATS credentials — read `accounts-service`'s own
`accounts.accounts` Postgres table over a second connection to the same
instance; it had no NATS connection, no independent lifecycle, and no state
of its own beyond what `accounts.Store` already held. That's a fake service
boundary, not a real one — merge them.

#### What changed

- `auth-service/auth/{handler.go,token.go}` moved to
  `accounts-service/auth/`, now importing `accounts.Store` directly instead
  of a duplicate read-only `AccountReader` over a second Postgres
  connection.
- `accounts-service/accounts/store.go` gained `ListActiveTenantNames`
  (active accounts minus `default`/`sys`, reusing `handler.go`'s
  `reservedAccountNames` map) — replaces auth-service's own `ListTenants`
  query.
- `accounts-service/cmd/main.go` wires one `*accounts.Store` into both
  `accounts.Handlers` (BasicAuth-gated `/api/accounts/*`) and
  `auth.Handlers` (ungated `/api/auth/*`), mounted on the same
  `http.ServeMux`. New `NATS_WS_URL` env var (default
  `ws://localhost:9222`) replaces auth-service's own copy of the same
  setting.
- `auth-service`'s Go module, Dockerfile, and `docker-compose.yml` service
  entry are gone. `accounts-service` now serves both route families on port
  7202; `vite.config.js`'s `/api/auth` proxy target moved from 7203 to
  7202.
- Test infrastructure for the `auth` package now calls `accounts.Migrate`
  for schema setup instead of hand-duplicating its `CREATE TABLE`
  statements — only possible now that both packages share one Go module.
- See `BUSINESS_RULES-ACCOUNTS.md`'s "Phase 19 — auth-service merged in"
  note for the full before/after.

#### Checklist

- [x] `accounts-service/auth/` package created (`handler.go`, `token.go`,
      tests), reading `accounts.Store` directly
- [x] `accounts.Store.ListActiveTenantNames` added + unit test
      (`accounts/store_test.go`)
- [x] `cmd/main.go` mounts both handler sets on one mux; `NATS_WS_URL` env
      var added
- [x] `auth-service/` directory deleted (module, Dockerfile, `cmd/`)
- [x] `docker-compose.yml`: `auth-service` entry removed, `accounts-service`
      gains `NATS_WS_URL`, `shipping-frontend`'s `depends_on` updated
- [x] `vite.config.js`'s `/api/auth` proxy retargeted to port 7202
- [x] `README.md` service table + Postgres credentials note updated
- [x] Stray `auth-service` path references fixed
      (`browserrpc/adapter.go`, `BUSINESS_RULES-SHIPPING.md`)
- [x] `go build`/`ginkgo ./...` green in `accounts-service` (48 specs: 36
      accounts + 12 auth)

---

### Phase 23 (IMPLEMENTED 2026-08-04, pending live verification) — Admin UI: SSE → NATS WebSocket Migration (Dual-Connection Model)

#### Goal

Replace all four of `frontend/admin`'s `EventSource`/SSE streams (dictionary watch, KV inspector, JetStream watch, RPC watch) with direct browser-side NATS WebSocket pub/sub, closing the multi-tab connection-exhaustion gap Phase 15 already fixed for `seafreight-app` (`admin_ui_realtime_transport_options` memory) and decoupling the topbar connection indicator from BU/tenant selection (today it's a side effect of `/api/watch/{context}` failing on an empty context). Scope: `frontend/admin` only — `frontend/refdata` is a separate, later decision.

#### Design

**Two browser connections**, mirroring the split shipping-service's backend already makes internally (`Deps.TenantNC`/`Deps.JS` vs `Deps.PlatformJS`):
- **Admin/Platform** — new dedicated NATS user under `PLATFORM`, minted via a new `MintAdminToken`/`GET /api/auth/adminConnectInfo` path in `accounts-service/auth/`, deliberately *not* routed through `Store.Get(tenant)`/`SigningKeySeed`/`Status` (PLATFORM has no tenant lifecycle). Opened once at boot, never reconnects. Sub-only permissions, no `$JS.API.>`/`$KV.>`: `notify.accounts.account.>` plus new REFDATA/RPCTRACE `notify.*` subjects (below). Drives the topbar connection indicator.
- **Tenant** — existing `MintBrowserToken`/`connectInfo?tenant=` flow, unchanged, plus one added sub permission: `obs.api.>` (RPC panel live tail). Reopens on tenant switch, same as `seafreight-app` today.

**New `notify.*` publish points** (backend adds these; browser gets no `$JS.API.>`/`$KV.>` permissions anywhere — decided over the alternative of granting direct JetStream/KV API access):
- KV bucket puts (today's `watchKVBucket`/`kv.WatchAll`) → `notify.{tenant}.kv.{bucket}.{key}.changed`
- Raw `SHIPPING` stream events (today's `watchJetStream`) → `notify.{context}.shipping.{entity}.{event}` (may already be redundant with the existing Shape A/B `notify.*` publish in `eventhandler/handler.go:181` — confirm at implementation time whether JetStream watch needs its own subject or can reuse it)
- REFDATA changes (today's `watchRefdata`) → `notify._platform.refdata.{typeKey}.changed`
- RPCTRACE (today's `watchRPCObs`'s replay half) → `notify._platform.rpctrace.entry` for live tail

**Replay/bootstrap** for the two streams that need history (JetStream raw watch, RPCTRACE last-10-min) moves to a one-shot REST GET — a stripped-down version of today's existing replay logic minus the long-lived SSE wrapper — followed by a `notify.*` subscribe for the live continuation, the same bootstrap-then-subscribe shape `seafreight-app` already uses for `api.*.list.v1` + `notify.*`.

**Frontend**: new `usePlatformConnection.js` (Admin/Platform singleton, modeled on `seafreight-app/src/nats/useNatsConnection.js`) plus a tenant-connection composable reused/adapted from the same file. `stores/dictionary.js`'s `connect()`/`disconnect()`, `KvInspector.vue`, `StreamView.vue`, and `RpcPanel.vue` all move off `EventSource` onto these. Topbar `connected` tag (`App.vue:110`) switches from `store.connected` to the Admin/Platform connection's own state.

**Backend removal**: all of `sse.go`'s `watch`/`watchTerminal`/`watchKVBucket`/`watchJetStream`/`watchRPCObs`/`watchRefdata` handlers and their routes, plus the corresponding URL builders in `api.js` (`watchUrl`, `kvBucketWatchUrl`, `jetstreamWatchUrl`, `rpcWatchUrl`).

**Open implementation fact to confirm before coding**: where PLATFORM's signing key material actually lives today (however `shipping-admin.creds` itself gets signed in `bootstrap-operator.sh`) — `MintAdminToken` needs the same access.

**Approved BR-AC18** (to land in `BUSINESS_RULES-ACCOUNTS.md` alongside BR-AC15-17 as part of this phase's checklist):
> **BR-AC18 — Admin token minting is isolated from the tenant lifecycle.** `MintAdminToken` mints a NATS user JWT under `PLATFORM` directly from its own signing key material, independent of `accounts.Store`'s `Status`/`SigningKeySeed`/reactivation state machine (which governs tenant accounts only). The minted JWT carries subscribe-only permissions (no `$JS.API.>`, `$KV.>`, or publish grants) scoped to `notify.accounts.account.>` and the REFDATA/RPCTRACE `notify.*` subjects this phase adds. Enforced by an isolation test asserting the admin JWT cannot publish to any subject and cannot subscribe to any tenant-scoped (`api.*`/`notify.{tenant}.*`) subject.

#### Checklist

- [x] Confirm PLATFORM signing-key access path for accounts-service — turned out already solved: `cmd/main.go`'s `ensureSigningKey` establishes a signing key for the seeded `platform` account at every boot (same as tenants), so `MintAdminToken` just reads `Store.Get(ctx, "platform")` directly, no new Provisioner plumbing needed
- [x] `accounts-service/auth`: `MintAdminToken` + `GET /api/auth/adminConnectInfo`; Ginkgo isolation spec per BR-AC18 (`auth/token_test.go`, `auth/handler_test.go`)
- [x] `accounts-service/auth/token.go`: added `obs.api.>` to tenant `MintBrowserToken` sub permissions
- [x] Backend: new `notify.*` publish points + specs — KV puts/deletes (`internal/kvstore.Store.EnableNotify`), SHIPPING raw ship/container events (`eventhandler.publishRawNotify`, piggybacked on the existing Shape A/container projectors rather than a new consumer), REFDATA + RPCTRACE (two new permanent background bridges, `eventhandler.RegisterRefdataNotify`/`RegisterRPCTraceNotify` — registered once at `composition.go` startup, not per-tenant, since both read PLATFORM-account streams)
- [x] Backend: one-shot REST bootstrap endpoints — `GET /api/jetstream/replay`, `GET /api/rpctrace/replay` (`rest/replay.go`), plus one not originally scoped: `GET /api/kv/buckets/{bucket}/entries` (`rest/kv.go`) — needed once it became clear `watchKVBucket`'s snapshot half had no one-shot equivalent yet
- [x] Backend: deleted `sse.go`'s `watch`/`watchTerminal`/`watchKVBucket`/`watchJetStream`/`replayJetStream`/`watchRPCObs` + routes. **Scope correction found during implementation: `watchRefdata`/`/api/refdata-watch` was NOT deleted** — it backs `shared/refdata/useRefdataLabels.js`'s UI-text/label refresh, used by every frontend in this repo (admin, seafreight-app, refdata), not just the four admin-specific panels this phase targeted; conflating it with the other five was a scope-drift mistake in this checklist's original `notify.*` bullet, caught before landing. It now lives in its own `rest/refdata_watch.go`, unchanged.
- [x] Frontend: `usePlatformConnection.js` + `useNatsConnection.js` (tenant) for `frontend/admin`, sharing a `connectionFactory.js` — both subscribe-only (this app issues no `api.*` commands, so no `request()`/publish surface was built)
- [x] Frontend: migrated `dictionary.js`, `KvInspector.vue`, `StreamView.vue`, `RpcPanel.vue` off `EventSource`
- [x] Frontend: topbar connection indicator wired to Admin/Platform connection state
- [x] Frontend: deleted `watchUrl`/`kvBucketWatchUrl`/`jetstreamWatchUrl`/`jetstreamStreamUrl`/`rpcWatchUrl`/`watchTerminalUrl` from `api.js` (the last was already-dead code, removed in the same pass)
- [x] `BUSINESS_RULES-ACCOUNTS.md`: BR-AC18 entry
- [x] `ARCHITECTURE-COMMUNICATIONS.md` § 6 / `ARCHITECTURE-ACCOUNTS.md` "Admin UI browser connections": dual-connection browser model + new `notify.*` subjects, with mermaid diagrams
- [x] `go build`/`go vet`/`go test ./...` green in `accounts-service` and `shipping-service`; `vite build` + `vitest run` + `eslint` (0 errors) green in `frontend/admin`
- [ ] Live verification: `nats/bootstrap-operator.sh --force` (needed — `shipping-admin`'s permissions gained `notify._platform.>` publish) + `docker compose down -v && up --build`, then confirm multi-tab open no longer exhausts connections, the connection indicator reflects PLATFORM connectivity independent of BU/tenant selection, and all four panels function with SSE fully removed — not yet run in this session (destructive/regenerates all creds, needs an explicit go-ahead)

---

### Phase 25 (25a–25d, 25f–25h IMPLEMENTED; 25e RESOLVED, 2026-08-06) — Pricing Service: Port Linebooker's Rate/Fee Domain

#### Goal

Explore whether refdata-service's corpus-versioning *pattern*
(draft → published → rollback, immutable snapshots) holds up for a
**write-adjacent** domain, by porting the pricing/rate domain of a real
production freight marketplace (Linebooker) —
`RateSheetEntity`/`FeeScaleEntity`/`FixedRateEntity` and their versioning —
into a new standalone `pricing-service`, alongside `shipping-service` and
`refdata-service`. Unlike refdata-service, this domain sits on a write path
in the source system (fee calculation on load-accept, bid validation
against book-now price), so it gets its own service and own Postgres rather
than a merge into refdata-service — see
`obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-DICTIONARY.md`
BR-D28 for the read-only boundary this deliberately does *not* inherit.

A source-code read of the Linebooker domain surfaced several real bugs
worth fixing rather than porting faithfully: FeeScale range matching is
fail-open (a bid above the top range silently charges zero fee), FeeScale
active-version selection sorts by insertion order (`id desc`) instead of
its own `activationDate`, and RateSheet "current version" falls back to
the *earliest* version rather than reporting "none yet." All three are
fixed in the port (BR-P05/BR-P06 in `BUSINESS_RULES-PRICING.md`) rather
than reproduced.

**Confirmed CQRS classification:** plain Postgres CRUD, not event-sourced —
applying the heuristic in `ARCHITECTURE.md` § "Event Sourcing vs Plain
CRUD," nothing here ever needs to replay a log to reconstruct state; "what
rate was in effect on date X" is answered by querying stored, effective-
dated/versioned rows, the same shape as refdata's corpus snapshots.

**Sub-phases**, each independently landable:

- **25a — FeeScale domain (IMPLEMENTED).** `FeeScale`/`FeeScaleVersion`/
  `FeeScaleRange` in `pricing-service/pricing/internal/domain/fee_scale.go`,
  with the corpus draft/published/rolled-back lifecycle reused at
  per-aggregate granularity (each FeeScale versions independently, not one
  context-wide bundle covering RateSheet/FixedRate too). Domain layer only —
  no Postgres/REST/NATS adapters, no `cmd/` yet.
- **25b — RateSheet domain (IMPLEMENTED).** `RateSheet`/`RateSheetVersion`/
  `RateSheetEntry` in `rate_sheet.go` — self-contained opaque
  `CustomerKey`/`RouteKey`/`VehicleType` identifiers owned by
  pricing-service, no dependency on shipping-service's Ship/Container. The
  same draft/published/rolled-back lifecycle as 25a, duplicated with
  distinct error names rather than unified behind a shared type (would have
  meant rewriting 25a's already-passing specs). `FeeScaleOverride`
  resolves when set; the default-fallback half of that rule stays deferred
  (no customer aggregate to hang a default off of).
- **25c — FixedRate domain (IMPLEMENTED).** `FixedRate`/`FixedRateVersion`
  in `fixed_rate.go`, same lifecycle shape again. The source's two
  independently-set "fixed rate" flags (`PricingType.FIXED_RATE` on the
  load, `RateSheetType.FIXED_RATE` on the rate sheet) don't collapse into a
  new field on `FixedRate` as originally planned — on closer read there is
  no `FixedRate`-side flag to collapse; `RateSheet.Type` (BR-P07) is simply
  designated the single source of truth for that "needs admin gating"
  signal, recorded as a design constraint for whenever 25e adds a load to
  gate (see `BUSINESS_RULES-PRICING.md`'s scope note — deliberately not a
  numbered BR-P, since there's nothing testable yet).
- **25d — Service wiring (IMPLEMENTED).** Own `pricing` Postgres schema/
  container (`pricing-postgres`, port 5435 — next free after
  `accounts-postgres`'s 5434), a `postgres/*_repository.go` adapter per
  aggregate mirroring refdata's corpus-repository transaction/rollback
  pattern (Publish/Rollback are transaction-bound; Rollback marks the
  previously-active version `rolled-back` with `rolled_back_by` pointing at
  the new one, exactly like `CorpusRepository.Rollback`), thin
  `application/commands` pass-through handlers (no notifier — no NATS this
  phase), a REST API under `/api/pricing/{context}/...` (port 7203, next
  free in the backend range), and a `pricing-service` docker-compose entry.
  Verified end to end against a live Postgres via `docker compose up`: full
  register → draft → add range/entry → publish → active-version resolution
  → fee/drop-charge calculation → rollback cycle for all three aggregates.
- **25e (RESOLVED) — Cross-service consultation.** Not `shipping-service`
  consulting pricing-service on a write path (the original framing) — the
  real first consumer is the Sea Freight Flow **browser**, directly, over
  `api.*`, mimicking a "Pricing" tab's manual rate-entry UX loosely modeled
  on Linebooker's own admin screens (auto-chained fee-scale range rows kept;
  Linebooker's forced-infinite top range and date-driven versioning
  deliberately not carried over — the former would recreate the exact
  fail-open bug BR-P05 closed, the latter contradicts the corpus
  draft/publish/rollback model already chosen for this port).
- **25f (IMPLEMENTED) — `api.*` adapter + tenant NATS connections.** See
  `BUSINESS_RULES-PRICING.md`'s "`api.*` frontend adapter" entry for the
  full design (package `internal/browserrpc`, lifecycle manager
  `internal/tenants`) — live-verified via the `nats` CLI against real
  `acme`/`globex` tenant creds; the reactive
  `notify.accounts.account.created`-triggered provisioning path mirrors
  shipping-service's proven mechanism but wasn't independently exercised
  with a freshly-minted tenant in this pass.
- **25g (IMPLEMENTED) — Sea Freight Flow "Pricing" tab.** A new sidebar
  entry in `frontend/seafreight-app` (pushed into `App.vue`'s `sections`
  array, no router changes needed — per `shared/unifi-theme/LAYOUT.md`), a
  new Pinia store (`stores/pricing.js`) modeled on `stores/port.js`'s
  bootstrap-then-loading-clears shape, and a read-only `PricingPanel.vue`
  listing every FeeScale/RateSheet/FixedRate in the current context.
  Discovered mid-phase that no endpoint existed to list what's registered
  without already knowing an exact name — added `List` per aggregate
  (BR-P16 in `BUSINESS_RULES-PRICING.md`, excludes soft-deleted FeeScales)
  across the domain/Postgres/REST/`api.*` layers first. Unlike
  `stores/port.js`, there is **no `notify.*` subscription** — pricing-service
  publishes no change-notification stream yet, so this store is a one-shot
  bootstrap read per connect/context-switch, not a live-updating view (BR-029's
  loading-state convention still applies, since the same reset-then-fetch
  gap exists). Live-verified in-browser against the real `docker compose`
  stack: seeded FeeScale/RateSheet/FixedRate rows via REST, confirmed they
  render over the real `api.*` NATS WebSocket path, confirmed a BU-context
  switch reconnects and shows that context's own distinct rows, and
  confirmed the new l10n keys (added to `refdata-service/refdata/seed.go`'s
  `l10nSeed` and regenerated via `npm run gen:i18n`) render correctly in
  both English and Spanish. Drilling into a row's version detail and the
  add/edit/publish/rollback UI are Phase 25h, deliberately out of scope here.
- **25h (IMPLEMENTED) — Manual-entry UX.** `FeeScalePanel.vue`/
  `RateSheetPanel.vue`/`FixedRatePanel.vue` (composed by `PricingPanel.vue`,
  replacing its 25g read-only tables) — register, create-draft, add-
  range-or-entry (auto-chained lower limits for FeeScale, no forced-infinite
  top row — that would recreate BR-P05's fail-open bug), publish, roll back,
  for all three aggregates; RateSheet/FixedRate also get an active/inactive
  toggle (re-`Register` with the flag flipped). FixedRate's "add entry" step
  collapses into "create draft" itself, since `CreateDraft` takes its rate
  fields directly rather than incrementally. No endpoint resolves an
  arbitrary version's ranges/entries by number, so a draft's rows are
  tracked client-side as added in the current session rather than
  re-fetched — a disclosed limitation, not a bug. `stores/pricing.js` only
  grew list-upserting actions (`register*`/`toggle*Active`); every other
  action is a direct `api.js` call from the owning panel, mirroring
  `ShipsAtPortPanel.vue`/`TerminalPanel.vue`'s existing split. Live-verified
  end to end in-browser for all three aggregates' full lifecycle.
- **Deferred, not rejected:** context-tree inheritance (a business-unit
  context falling back to `_platform` defaults) for pricing entities — every
  aggregate carries a flat `context` field but does not walk an ancestry
  chain.

#### Checklist

- [x] 25a: Business rules confirmed with user before implementation (BR-P01–BR-P06)
- [x] 25a: Ginkgo specs written from rules before implementation (`pricing/fee_scale_test.go`), confirmed red
- [x] 25a: `domain.FeeScale`/`FeeScaleVersion`/`FeeScaleRange` implemented, specs green (`ginkgo ./...` in `pricing-service`)
- [x] 25a: `BUSINESS_RULES-PRICING.md` written and indexed from `BUSINESS_RULES.md`
- [x] 25b: RateSheet domain rules confirmed with user (BR-P07–BR-P12), specs written (confirmed red), implemented (green)
- [x] 25c: FixedRate domain rules confirmed with user (BR-P13–BR-P15), specs written (confirmed red), implemented (green); flag-conflation resolved as a design constraint on `RateSheet.Type`, not a new field
- [x] 25b/25c: `BUSINESS_RULES-PRICING.md` updated with BR-P07–BR-P15 and the deferred/design-constraint notes; index in `BUSINESS_RULES.md` updated
- [x] 25d: Postgres schema + adapters (own `pricing-postgres`, port 5435) for all three aggregates
- [x] 25d: REST API (`cmd/main.go`, port 7203), docker-compose entries (`pricing-postgres` + `pricing-service`)
- [x] 25d: Live-verified via `docker compose up` — full lifecycle for all three aggregates, including rollback semantics
- [x] 25d: `BUSINESS_RULES-PRICING.md`/plan updated to reflect service wiring is live
- [x] 25e: Resolved with user — browser talks to pricing-service directly via `api.*`; `shipping-service` never consults it
- [x] 25f: `internal/browserrpc` adapter (27 endpoints across FeeScale/RateSheet/FixedRate) implemented, mirroring shipping-service's browserrpc pattern
- [x] 25f: `internal/tenants` lifecycle manager (`EnsureAll`/`EnsureByName`/`TeardownByName`, notify.accounts.account.* subscription) implemented
- [x] 25f: `cmd/main.go`/docker-compose wired (`NATS_URL`, `NATS_CREDS_DIR`, creds volume, depends_on `nats`)
- [x] 25f: Live-verified via `nats` CLI against real `acme`/`globex` creds — full FeeScale lifecycle + confirmed per-tenant adapter isolation
- [x] 25f: `BUSINESS_RULES-PRICING.md`/plan updated
- [x] 25g: `List` endpoint added per aggregate (BR-P16) across domain/Postgres/REST/`api.*` layers, Ginkgo spec green
- [x] 25g: `stores/pricing.js` + `PricingPanel.vue` + `IconPricing.vue` built; wired into `App.vue`'s sections/onMounted/onUnmounted and `stores/tenant.js`'s tenant-switch reconnect
- [x] 25g: l10n keys added to `refdata-service/refdata/seed.go`, `npm run gen:i18n` regenerated `l10nFallback.en.js`
- [x] 25g: `App.spec.js`'s hardcoded nav-item list updated for the new tab; full `vitest run` back to the same pre-existing failure count (7, unrelated to this phase)
- [x] 25g: Live-verified in-browser against `docker compose` — seeded data renders over real `api.*` NATS, BU-context switch reconnects to a distinct context's rows, English/Spanish both render correctly
- [x] 25g: `BUSINESS_RULES-PRICING.md`/`BUSINESS_RULES.md`/plan updated
- [x] 25h: `api.js` extended with the full register/get/create-draft/add-range-or-entry/set-fee-scale-override/publish/rollback/versions/active endpoint set for all three aggregates
- [x] 25h: `stores/pricing.js` gained `register*`/`toggle*Active` list-upserting actions; `stores/pricing.spec.js` added (6 specs, mirrors `port.spec.js`'s conventions)
- [x] 25h: `FeeScalePanel.vue`/`RateSheetPanel.vue`/`FixedRatePanel.vue` built (register dialog, row-expansion detail, create-draft, add-range-or-entry, publish, rollback); `PricingPanel.vue` simplified to compose them
- [x] 25h: l10n keys (42 new) added to `seed.go`, `npm run gen:i18n` regenerated `l10nFallback.en.js`; cross-checked every `t(...)` call used in the new panels resolves to a real key
- [x] 25h: `vite build` and full `vitest run` clean (31 tests: 24 passing + the pre-existing 7 unrelated failures, same as before this phase)
- [x] 25h: Live-verified in-browser against `docker compose` — full register→draft→add-range/entry→publish→rollback cycle for FeeScale, RateSheet (incl. fee-scale override), and FixedRate (incl. active/inactive toggle)
- [x] 25h: `BUSINESS_RULES-PRICING.md`/`BUSINESS_RULES.md`/plan updated

---

### Phase 25i (DONE) — Effective-Dated Diesel Overlay

#### Goal

Add the one dimension the Phase 25 port deliberately left out: diesel
adjustments as a **date-effective overlay** on a stable, published rate
sheet — not a re-publish. A source-code read of Linebooker (delegated
investigation, 2026-08-07, citations in the 25i design sketch) established
that `RateSheetDieselAdjustmentEntity` is an independently effective-dated
overlay (`start_date`/`end_date`/`minor_version`) on a stable
`RateSheetVersionEntryEntity`, selected **by the load's own execution
date** (`RateSheetVersionEntryEntityRepository.java:106-121`;
`RateSheetEntityServiceImpl.java:372-377`) — not "the latest." This
**supersedes and corrects** the claim currently written into
`pricing/internal/domain/rate_sheet.go:75-77` and `fixed_rate.go:36-39`
that "a diesel-triggered repricing is just another publish": that is
factually wrong about the source and loses real behaviour (a backdated
load must reprice against the diesel window in effect *then*).

**Confirmed CQRS classification (unchanged):** still plain Postgres CRUD,
not event-sourced. "What diesel-adjusted rate was in effect on date X" is
answered by an interval query over dated rows, never by replaying a log —
the exact worked example of "reference-looking data that needs
date-effective history but still isn't event-sourced" that
`ARCHITECTURE.md` § "Event Sourcing vs Plain CRUD" describes.

#### Versioning model — two axes, `major.minor`

The correction is not that "publish" goes away; it is that **two kinds of
change were collapsed onto one axis**. This phase splits them:

- **Major version — content changes → publish.** Editing lanes, base
  rates, or drop rules stays exactly the existing `draft → published →
  rolled-back` lifecycle (BR-P09, **unchanged**).
- **Minor version — a diesel-price change → append an overlay.** A new
  diesel price appends a dated adjustment on top of the *currently
  published major version*, with **no new major publish**.

Resolved identity becomes `major.minor` (e.g. `v3.0` published content,
`v3.1`/`v3.2` diesel re-prices on it) — matching Linebooker's
`major_version` + `minor_version` two-column model.

#### Decisions locked (with user, 2026-08-07)

- **Diesel price index lives in `pricing-service`** (one more table in
  schema `pricing`), not `refdata-service` — keeps the overlay
  self-contained, zero new cross-service `rpc.*` hops, does not re-open the
  BR-D28 read-only-boundary question. (The refdata option is recorded as a
  rejected-for-now alternative, not deleted.)
- **Diesel adjustments are auto-appended as immediately-effective minor
  versions** — no draft/publish gate. They are system-generated from an
  authoritative price change; the human draft/publish ceremony stays for
  content (major) edits only. Faithful to the source.
- **FixedRate overlay deferred to Phase 25j** — 25i covers RateSheet (the
  richer case); FixedRate reuses the identical pattern as a mechanical
  follow-up (`FixedRateSubVersionEntity` in the source).

#### Proposed business rules (final wording confirmed at 25i-a, before specs)

- **BR-P17** — A resolved rate carries a `major.minor` version. Major = the
  existing published content version (BR-P09, unchanged); minor = a diesel
  overlay on a published major. A diesel-price change bumps minor only,
  never a new major publish.
- **BR-P18** — A diesel price index is a time series `active_date →
  (cent_coastal_price, cent_inland_price)`. "Price in effect on date X" =
  the entry with the greatest `active_date ≤ X`.
- **BR-P19** — Each rate-sheet entry carries its diesel baseline
  (`cent_initial_diesel_price`, `diesel_percentage`, `diesel_type` ∈
  {coastal, inland}), authored as part of the major version; these are the
  formula inputs.
- **BR-P20** — When a diesel price becomes effective on date D, a new
  minor-version adjustment is appended per affected entry (`start_date =
  D`); the previously-open window's `end_date` closes to D (contiguous,
  non-overlapping). Adjusted rate is computed **once, at creation**:
  `adjusted = base + base·(dieselPct/100)·((currentDiesel − initialDiesel)/initialDiesel)`,
  with `currentDiesel` the indexed price (coastal|inland per the entry's
  `diesel_type`) in effect on D.
- **BR-P21** — If no diesel price is indexed on/before D, adjustment
  creation is **rejected** (same fail-closed spirit as BR-P05), never
  silently zero or "current."
- **BR-P22** — Pricing a load resolves `(sheet, route, vehicle,
  effectiveDate)`: active published major (BR-P08) → entry for
  route×vehicle → the adjustment window containing `effectiveDate`
  (`start ≤ date < next start`; the last window is open-ended) → that
  adjusted rate → then the drop surcharge (BR-P12, unchanged).
  `effectiveDate` = the load's pickup date, not "now."
- **BR-P23** — A date preceding the first adjustment window falls back to
  the entry's authored base rate. A newly published major version starts
  with no overlays and accrues its own going forward.

#### Design

- **Domain** (`pricing/internal/domain/`): `rate_sheet.go`'s
  `RateSheetEntry` gains `CentInitialDieselPrice`, `DieselPercentage`,
  `DieselType`; new `DieselAdjustment` (minor version, start/end,
  precomputed `CentAdjustedRate`); a `DieselPriceIndex` lookup
  (`greatest active_date ≤ X`); a `major.minor` resolver
  (`ResolveRate(sheet, versions, adjustments, route, vehicle,
  effectiveDate)`); the BR-P20 formula. Major-version lifecycle code
  untouched.
- **Postgres** (`pricing/internal/postgres/migrate.go` + repos): 3 new
  columns on `pricing.rate_sheet_entries`; new `pricing.diesel_prices`
  (`context, active_date, cent_coastal_price, cent_inland_price`); new
  `pricing.rate_sheet_diesel_adjustments` (`context, rate_sheet_name,
  major_version, route_key, vehicle_type, minor_version, start_date,
  end_date, cent_adjusted_rate`, FK to the entry) with window-maintenance
  on insert.
- **`api.*`/browserrpc** (`internal/browserrpc`): publish-a-diesel-price
  (which auto-generates the overlay across affected entries),
  list-diesel-prices, and resolve-rate-at-date. Overlay generation is a
  server-side effect of the price publish, not a separate authoring step
  (locked decision).
- **Frontend** (`frontend/seafreight-app`, Pricing tab): a diesel-price
  entry/list surface and a resolved `major.minor` + effective-date
  display on the rate-sheet panel. l10n keys via `seed.go` +
  `gen:i18n` (BR-D16), same as 25g/25h.

#### Sub-phases (each independently landable)

- **25i-a — Domain + rules.** Confirm BR-P17–P23 wording, write Ginkgo
  specs (red), implement (green). Domain layer only.
- **25i-b — Postgres.** Schema + repositories + migration + window
  maintenance; integration-verified.
- **25i-c — `api.*` + UI.** browserrpc endpoints + Pricing-tab surfaces;
  live-verified via `docker compose`.
- **25j (separate) — FixedRate overlay.** Same pattern applied to
  FixedRate sub-versions.

#### Checklist

- [x] 25i-a: BR-P17–P23 final wording confirmed with user before specs (2026-08-07)
- [x] 25i-a: Ginkgo specs written from rules (`pricing/rate_sheet_diesel_test.go`), confirmed red
- [x] 25i-a: Domain types `DieselPrice`/`DieselOverlay`; `MinorVersion`+`Overlays` on `RateSheetVersion`; `DieselPct`/`InitialDieselCents` on `RateSheetEntry`; `AdjustedRate`, `DieselPriceOn`, `AppendDieselOverlay`, `AppendDieselOverlayFromIndex`, `RateForLoad` implemented; 40/40 specs green
- [x] 25i-a: Correction landed — "just another publish" replaced in `rate_sheet.go`/`fixed_rate.go`; superseded-claim note + BR-P14 body updated in `BUSINESS_RULES-PRICING.md`
- [x] 25i-b: `migrate.go` — `minor_version` on `rate_sheet_versions`; `diesel_pct`/`initial_diesel_cents` on `rate_sheet_entries`; new `pricing.diesel_prices` + `pricing.rate_sheet_overlays` tables (note: plan used `rate_sheet_diesel_adjustments` — shipped as `rate_sheet_overlays`); `AddEntry`/`ActiveVersion` updated; `IndexDieselPrice`/`ListDieselPrices`/`PersistDieselOverlay` implemented
- [x] 25i-b: Live-verified via `docker compose` — indexed diesel prices, confirmed overlays appended (contiguous window-closing, minor-version bump), confirmed BR-P21 fail-closed (404 on an unindexed date) and BR-P22/BR-P23 resolution behavior
- [x] 25i-c: REST endpoints (`POST/GET /api/pricing/{context}/diesel-prices`, `POST .../diesel-overlay`); browserrpc subjects (`DieselPriceIndexSubject`, `DieselPriceListSubject`, `RateSheetApplyOverlaySubject`); `ApplyDieselOverlay` command; error mapping for `ErrNoDieselPrice`/`ErrEntryNotFound`
- [x] 25i-c: Pricing-tab frontend — `DieselPricePanel.vue` (index/list surface); `RateSheetPanel.vue` gained `major.minor` display, an overlays table, and an "Apply Diesel Overlay" control; l10n keys added via `seed.go` + `gen:i18n`
- [x] 25i-c: `vite build` + `vitest run` clean (pre-existing unrelated `useL10nCopy`/`useRefdataLabels` localStorage failures excluded); live-verified in-browser against real stack, including a DatePicker-timezone bug fix (`.toISOString()` on a local-midnight `Date` shifted the calendar day back a day under UTC+2 — fixed with a UTC-midnight re-anchor in both `DieselPricePanel.vue` and `RateSheetPanel.vue`)
- [x] BR-P24 (found + fixed during 25i-c's live smoke test): `AppendDieselOverlay` divided by `InitialDieselCents` with no zero guard — any entry without an authored diesel baseline (true of every pre-25i seeded rate sheet) was silently corrupted to a $0 adjusted rate via a NaN→int64 conversion. Fixed to skip overlaying baseline-less entries (they keep resolving to their base rate via BR-P23); 3 new Ginkgo specs added, 43/43 green.
- [x] `BUSINESS_RULES-PRICING.md` updated with BR-P17–P24 (IMPLEMENTED, Phase 25i); index in `BUSINESS_RULES.md` range updated
- [x] Plan checklist updated to reflect landed sub-phases

---

### Phase 26 (IMPLEMENTED, 2026-08-13) — Trading Partner Service: Shipper/Transporter Registration

#### Goal

Port Linebooker's Shipper/Transporter onboarding model (V2's `BusinessEntity`
+ `CustomerProfileEntity`/`TransporterProfileEntity`/`TransporterDocumentEntity`/
`FleetAssetEntity`) into a new standalone `trading-partner-service`, alongside
`shipping-service`/`refdata-service`/`accounts-service`/`pricing-service`.
Collective role term is **Trading partners** (Shipper + Transporter — see
`linebooker_shipper_vs_customer_naming.md`,
`linebooker_trading_partners_term_and_fleet_cardinality.md` in
`.claude/memory/`), surfaced in Admin UI under a new "Trading partners" nav
section rather than RefData UI — this is organisation-owned master data that
*consumes* refdata lookups, not a vocabulary itself
(`linebooker_registration_ui_placement.md`).

**Why Admin UI and not `seafreight-app`, given Phase 25's counter-precedent.**
The immediately preceding "port a real V2 domain" phase put its UI in
`frontend/seafreight-app/` (`PricingPanel.vue`, nav key `pricing`), not Admin
— so the precedent for "ported business domain → which frontend" currently
points at the ops app, and this phase deliberately diverges from it. The
distinction is *who does the task, and how often*: pricing is day-to-day
operational work performed by the same people running ships and terminals,
whereas partner onboarding is operator-administration — rare, privileged,
PII/KYC-bearing, and adjacent to the tenant lifecycle already administered on
the Accounts screen. That places it with Accounts in the Admin UI, matching
`linebooker_registration_ui_placement.md`'s reasoning. Recorded explicitly so
a later reader doesn't find two ported domains in two different frontends
with no stated reason.

**Confirmed CQRS classification.** CLAUDE.md's test ("does anything need to
replay this?") applies per entity, not per service, so it is applied per
entity here rather than asserted once for the phase:

- **`TradingPartner` identity** (`name`, `registrationNo`, `vatRegistrationNo`,
  …) — only current state is ever queried; nothing reconstructs a partner
  from a log. **Plain Postgres CRUD.**
- **`FleetAsset`** — same; a truck's current registration/VIN/make/model is
  all anything asks for. **Plain Postgres CRUD.**
- **`ComplianceDocument` — CRUD now, temporal later (not a clean CRUD case).**
  "Was this partner's GIT cover valid on the date of the load that was
  damaged?" is a point-in-time question, which is precisely CLAUDE.md's own
  *"a rate table where 'what was in effect on date X' matters"* counter-example
  to the reference-data-looking-therefore-CRUD instinct. v1 still ships plain
  CRUD — nothing in the POC asks that question yet — but the classification is
  provisional and is filed under the deferred item below, not settled
  alongside the two above.
- **Status transitions** — the deferred item itself; see below.

Note on the Phase 25 precedent: pricing-service avoided event sourcing but did
**not** avoid history — it built an explicit draft/published/rolled-back
version lifecycle in Postgres (BR-P07–P12). In this repo, "ported domain,
plain CRUD" has in practice meant "CRUD *plus* explicit versioning wherever
history matters." Phase 25 is not a licence for pure CRUD everywhere, and
`ComplianceDocument` above is where that distinction bites.

**Deferred, not rejected (named open item, not designed or scheduled):** the
user flagged wanting a follow-up exploration of whether the Registered→Active
transition should eventually become its own CQRS/event-sourced shape or use a
temporal/effective-dated model (e.g. for re-vetting after suspension)
(`linebooker_trading_partner_phase_v1_scope.md`). Two further questions belong
in that *same* exploration rather than being decided piecemeal: (a)
`ComplianceDocument`'s temporal classification above, and (b)
document-expiry-driven suspension (see "Deferred: document-driven status"
below).

**Scope decisions confirmed 2026-08-13** (full detail:
`linebooker_trading_partner_phase_v1_scope.md`):

- **No platform-identity/tenant-membership split for v1.** One
  `TradingPartner` record (identity + status), no separate platform-account
  vs. tenant-membership tables.
  **Why this does not make "suspended" ambiguous:** `TradingPartner.context`
  is not a free-floating label. `accounts.business_units` carries a
  **globally unique** `context` column with an `account_id` FK
  (`accounts/store.go`'s `business_units_context_key`), and refdata's
  `domain.Context` carries a `Tenant` field with `ListByTenant`. So
  context → tenant is 1:1 and resolvable from data that already exists;
  a partner row is therefore **single-tenant by construction**, and
  `Suspended` means "suspended within this context, which belongs to exactly
  one tenant." The absence of a membership table removes a degree of freedom
  rather than creating an ambiguity.
  **Trigger that would force the split:** the first real-world partner
  needing rows in two contexts owned by *different* tenants. At that point
  `context` alone stops identifying the partner and the platform-identity /
  tenant-membership layering of
  `linebooker_platform_vs_tenant_service_split.md` becomes required. Until
  then, don't build it.
- **Status lifecycle: `Registered` → `Active` → `Suspended` → `Reactivate`
  (back to `Active`)**, confirmed 2026-08-13 to mirror accounts-service's
  create/suspend/reactivate triple — all transitions set **manually**,
  independent of compliance-document approval state (not derived/gated by
  documents in v1).
- **Transition legality matrix** (needed before any BR-TP spec can be
  written; mirrors accounts-service's explicit guards, e.g.
  `reactivateAccount`'s `409 Conflict` when status is not `Suspended` —
  `accounts/handler.go`):

  | From \ command | `Activate` | `Suspend` | `Reactivate` |
  |---|---|---|---|
  | `Registered` | → `Active` | **409** (nothing to suspend from; a partner that should never go live is simply never activated) | **409** |
  | `Active`     | **409** (already active) | → `Suspended` | **409** (not suspended) |
  | `Suspended`  | **409** (`Reactivate` is the only way back — one named command per edge, as in accounts-service) | **409** (already suspended) | → `Active` |

  `Register` is creation, not a transition; it always lands in `Registered`.
- **v1 `Suspend` has no enforcement consumer — this is a status flag, not a
  gate.** accounts-service's suspend has teeth: it revokes the account JWT at
  the resolver, force-evicts live connections, deletes the `.creds` file, and
  publishes `notify.accounts.account.suspended`, which two downstream services
  act on (BR-AC09 → BR-031/BR-032). `TradingPartner.Suspended` has **no
  equivalent enforcement point anywhere in this POC** — there is no
  tender/bid/marketplace service yet to refuse a suspended partner. Stated
  explicitly so the phase isn't mistaken for shipping an enforced boundary:
  what 26a actually delivers is a guarded state machine plus an audit trail.
  **The eventual consumer is the marketplace/tender phase** (bid submission
  and allocation must reject a non-`Active` partner — see
  `linebooker_bid_tender_allocation_rules.md`); a `notify.*` publication
  mirroring BR-AC08–AC10 is deliberately *not* built now, because there is
  nothing to subscribe to it.
- **No offboarding / terminal state in v1 — explicit non-goal, not an
  oversight.** accounts-service deliberately allows no hard delete, on
  regulatory-retention grounds (BR-AC03); a KYC/compliance record with
  identity documents attached has at least as strong a retention argument, so
  "delete a trading partner" is not implemented and no `Deregistered`/`Closed`
  terminal state is defined. A partner that stops trading is left
  `Suspended`. Adding a distinct terminal state is a follow-on decision, and
  belongs with the deferred lifecycle exploration above.
- **v1 includes both compliance documents and Transporter fleet assets** —
  not deferred to a follow-on phase. Subcontracting
  (`FleetAssetEntity.subcontractingOwner`) stays out of scope regardless.
- **Shipper and Transporter built together**, one generic `TradingPartner`
  aggregate with a `PartnerType` discriminator (`SHIPPER` | `TRANSPORTER`),
  mirroring V2's `BusinessEntity` + `BusinessType` pattern
  (`v3_tenancy_axes_decision.md`).

**Confirmed fields** (trimmed from V2's full `BusinessEntity`/
`TransporterProfileEntity`, per 2026-08-13 sign-off):

- Shared: `name`, `tradingAs`, `companyName`, `registrationNo`,
  `vatRegistrationNo`, `type` (`SHIPPER`|`TRANSPORTER`), `status`
  (`REGISTERED`|`ACTIVE`|`SUSPENDED` — all three states of the lifecycle
  above), `context` (business-unit scope, per this repo's `{context}`
  convention). Dropped from V2: `contactPerson`/`contactNo` (redundant with a
  future Users/contacts model).
- Transporter-only: fleet assets, one-to-many (`registrationNo`, `vin`,
  `make`, `model`, **`vehicleTypeCode`** — a trimmed `FleetAssetEntity`, no
  `subcontractingOwner`).
  **`vehicleTypeCode` is validated against refdata**, not stored as free
  text. Without it this phase consumes *zero* reference data, which both
  wastes the demonstration (a repo whose central question is
  refdata-as-a-service) and hollows out the stated reason for the Admin-UI
  placement — `linebooker_registration_ui_placement.md` justifies it partly on
  "consumes refdata lookups (VehicleType/DocumentType/Country pickers)". The
  corpus already exists: `refdata-service/cmd/seed-vehicle-types` builds a
  `vehicle-type` dictionary type (plus a `vehicle-type-category` domain enum)
  directly from Linebooker V2's own `VehicleType.java`, via refdata's REST
  admin API. `make`/`model` stay free text — they are the specific truck, not
  a vocabulary.
- **`gitCoverage`: the earlier "insurance is a document, not a number"
  rationale was wrong on the V2 evidence and is corrected here.** In V2 the
  coverage amount does not live on the profile at all — it lives *on the
  document*, as `TransporterDocumentEntity.coverage`/`centCoverage`. So
  "tracked as a document" does not justify dropping the number; the number is
  part of the document record. It is therefore **restored as
  `coverageCents` on `ComplianceDocument`** (nullable, meaningful only for
  `GOODS_IN_TRANSIT`), not on `TradingPartner`. Any "is this transporter
  adequately insured?" view needs it, and it is close to free once the
  document entity exists. Consciously dropped from the same V2 entity, for the
  record rather than by omission: `insuranceCompanyName`,
  `insuranceContactPersonName`, `insuranceContactNumber`,
  `thirdPartyContingencyCover` (the last pairs with the dropped
  `GIT_CONTINGENCY_POLICY` document type, so both go together).

**Confirmed compliance documents** (subset of V2's `DocumentTypes`,
per-document `status`: `PENDING`|`APPROVED`|`REJECTED`, independent of the
parent `TradingPartner.status`):

- Both roles: `CIPC` (company registration cert), `DIRECTOR_ID`,
  `BANK_CONFIRMATION_LETTER`, `TERMS_AND_CONDITIONS`.
- Transporter-only addition: `GOODS_IN_TRANSIT` (insurance cert). Dropped
  from V2 candidates: `GIT_CONTINGENCY_POLICY`, `BEE_COMPLIANCE_CERTIFICATE`
  — note the latter is a ZA-regulatory artefact and a business call rather
  than a technical one; expect it back.
- **Document type stays a Go enum in v1, not a refdata `document-type`
  vocabulary — deliberate, and worth the sentence** because
  `linebooker_registration_ui_placement.md` names `DocumentType` as an example
  of exactly the sort of stable shared vocabulary refdata *should* own. The
  reason it stays in the domain layer for now: the per-type rules are domain
  logic, not lookup data (which types are Transporter-only, which carry
  `coverageCents`, which are required before a partner can sensibly be
  activated later), and refdata has no `document-type` corpus seeded today.
  Migrating it to refdata later is additive. `vehicleTypeCode` above is the
  opposite case and *does* go to refdata, because its corpus already exists
  and carries no per-code behaviour.
- **Per-document fields:** `type`, `status`
  (`PENDING`|`APPROVED`|`REJECTED`), **`expiresAt` (nullable)**,
  **`coverageCents` (nullable)**, `reference` (see storage decision below),
  `updatedAt`.
  **`expiresAt` is added in v1 even though nothing reads it.** V2's
  `TransporterDocumentEntity` has an `expiry_date` column, and GIT insurance
  certificates genuinely expire — expiry, not rejection, is the realistic
  trigger for the deferred document-driven-status rule below. Adding the
  column now costs nothing; adding it later costs a migration, and without it
  the UI cannot even *display* an expired certificate as expired.
- **Document storage: metadata-only in v1 — no file bytes are stored, and
  this is a decision, not an omission.** V2 splits document metadata
  (`TransporterDocumentEntity`) from the file itself (a separate
  `DocumentEntity`). This stack has no blob store and adding one (S3/MinIO
  container, upload endpoints, content-type and size validation, signed URLs)
  is a materially larger piece of work than the domain model this phase is
  actually about. `ComplianceDocument` therefore carries a `reference` string
  (an opaque external document locator, unvalidated in v1) alongside status
  and expiry. Consequence to keep straight in 26e: the Admin UI surfaces
  **document registration and status management, not file upload** — the
  earlier "document upload" wording was wrong and is retracted. Real file
  storage is a follow-on phase.

**Deferred: document-driven status (named open item, mirroring the
CQRS/temporal one — not a settled non-feature).** v1 keeps every status
transition manual and fully independent of document state, as confirmed
2026-08-13. That decision stands for this phase, since gating would couple two
state machines before either has been exercised. But it is a *deferral with a
known trigger*, not a permanent design property, and was previously written up
as though settled — corrected here so it is tracked symmetrically with the
CQRS/temporal exploration. The trigger is `expiresAt` passing: a Transporter
whose `GOODS_IN_TRANSIT` cover has lapsed is the concrete case where a
document should influence status. **Intended shape when it lands:** an expiry
raises a flag/notification for an operator, and does **not** auto-flip
`status` — so the "all transitions are manual and audited" invariant (and its
BR-TP specs) survives the change rather than being contradicted by it.

**Sub-phases**, each independently landable (mirroring Phase 25's
domain-first decomposition):

- **26a — TradingPartner domain model.** `TradingPartner` aggregate +
  `PartnerType`/`PartnerStatus` in
  `trading-partner-service/tradingpartner/internal/domain/`. Manual
  `Register` → `Activate` → `Suspend` → `Reactivate` lifecycle (confirmed
  2026-08-13 — mirrors accounts-service's create/suspend/reactivate triple,
  BR-AC08–AC10 in `BUSINESS_RULES-ACCOUNTS.md`), all transitions manual (no
  document-approval gating in v1, per the Active-gate decision above) and
  guarded by the legality matrix above (every illegal edge is a
  `409 Conflict`, as in `reactivateAccount`). `PartnerStatus`: `Registered` |
  `Active` | `Suspended`. `Suspend` takes a **required `reason`** (see 26a1).
  Domain layer only.
- **26a1 — Lifecycle audit trail.** An append-only
  `trading_partner.audit_events` table, deliberately the same shape as
  `accounts.audit_events` (BR-AC11): action, partner id, actor, source IP,
  outcome `success`/`failed`, JSONB metadata; no `UPDATE`, no `DELETE`.
  **This is the substantive counterweight to `Suspend` having no enforcement
  consumer in v1** — the durable record of *who* suspended *whom*, *when*, and
  *why* is the actual deliverable of the lifecycle, and a KYC/compliance
  domain carries a stronger audit obligation than the NATS-account minting
  BR-AC11 already covers. Reuse BR-AC11's conventions verbatim rather than
  inventing new ones: `X-Actor` header over the basic-auth username as the
  pre-WorkOS actor placeholder; best-effort writes that log but never block or
  roll back the operation they describe; nothing written for a request that
  fails validation before any state was mutated. `Suspend`'s `reason` is
  required at the domain boundary and lands in `metadata`.
- **26b — Compliance documents.** `ComplianceDocument` child entity
  (per-role subset above, with `expiresAt`/`coverageCents`/`reference`),
  independent status field, no link enforced to parent
  `TradingPartner.status` in v1 — a deferral with a named trigger, per
  "Deferred: document-driven status" above, not a settled non-feature.
  Metadata-only: no file bytes, no upload path.
- **26c — Transporter fleet assets.** `FleetAsset` child entity,
  one-to-many off `TradingPartner` (Transporter only), including
  `vehicleTypeCode` validated against refdata's `vehicle-type` corpus.
- **26d — Service wiring.** Own Postgres (`trading-partner-postgres`, host
  port 5436) and REST API (`trading-partner-service`, host port 7204), plus a
  docker-compose entry. **Ports verified free directly against
  `demos/01-dictionary/docker-compose.yml`** (not assumed): Postgres hosts in
  use are 5432 shipping / 5433 refdata / 5434 accounts / 5435 pricing;
  backend hosts in use are 7200 shipping / 7201 refdata / 7202 accounts /
  7203 pricing. 5436 and 7204 are the next free in each range.
  - **Transport: REST (frontend) + a tenant-scoped `rpc.*` client
    (backend-to-backend, for 26c's `vehicleTypeCode` validation only) —
    corrected 2026-08-13 after a design-review contradiction.** The first
    pass of this plan called the transport "REST-only, no NATS, no tenants
    package," reasoning by analogy to accounts-service. That's wrong: BR-D28
    in `BUSINESS_RULES-REFDATA.md` is absolute — *"`rpc.*` is the only
    transport for backend-to-backend synchronous calls, full stop... no HTTP
    client, base URL, or hostname/port config pointing at a peer backend
    service"* — there is no REST fallback for one backend calling another,
    and that was tried and deliberately closed. Since 26c validates
    `vehicleTypeCode` against refdata-service, and
    `refdataconsumer.New(nc *nats.Conn)` shows that call rides the caller's
    **own tenant-local NATS import** (Phase 21's account-export/import model
    — even reading platform-root corpus data requires being connected as
    that tenant's account, not a shared/platform connection), this service
    needs the **same tenant-scoped NATS wiring `pricing-service` built**:
    `internal/tenants` (per-tenant connection lifecycle,
    `notify.accounts.account.*` reactive provisioning/teardown, mirroring
    `pricing/internal/tenants`), `NATS_CREDS_DIR` + creds volume mount in
    docker-compose, and a small `refdataconsumer`-style client for the one
    `rpc.{context}.refdata.type.list.v1` (or `item.get.v1`) call 26c needs.
    The browser-facing surface stays REST-only (no `api.*` browserrpc
    adapter for the Admin UI — that part of the original reasoning holds);
    only the backend's *outbound* call to refdata-service moves onto
    `rpc.*`. Every `nats.Connect` call must set `nats.Name("trading-partner-service")`
    per CLAUDE.md, testably (`nc.Opts.Name` non-empty).
  - **`frontend/admin/nginx.conf` needs a `proxy_pass` location.** Since
    `TradingPartner` carries a `context` field like pricing/refdata's
    resources (not a flat, context-free identifier like an account name),
    its REST routes follow **pricing/refdata's `/api/{service}/{context}/...`
    shape**, not accounts-service's flat `/api/accounts/{name}` shape — route
    `/api/trading-partners/` → `trading-partner-service:8080/api/trading-partners/`.
    Confirmed by inspecting the actual file: today it only proxies
    `/api/auth/` and `/api/platform/accounts` to accounts-service plus a
    shipping-service catch-all on `/api/` — there is no existing
    refdata/pricing route to copy verbatim (those UIs live in different
    frontends), so this is a new location block, not a copy-paste.
  - **Swagger** — every other backend in this demo publishes one; add it
    (`/swagger/` on 7204) for consistency.
  - **`README.md` port tables** — add the new Postgres and backend rows, and
    fix the existing drift while in there: the tables currently stop at
    `accounts-service` (7202/5434) and never gained `pricing-service`
    (7203/5435).
- **26e — Admin UI "Trading partners" section.** New nav category (per
  `linebooker_registration_ui_placement.md` and the Admin-vs-`seafreight-app`
  rationale above), `stores/tradingPartners.js` Pinia store, register/list/
  detail panels covering **document registration and status management (not
  file upload — see the storage decision above)**, suspend/reactivate with the
  required reason, an audit-trail view, and (Transporter) fleet-asset
  management with a refdata-backed vehicle-type picker.

#### Checklist

- [x] Business rules confirmed with user before planning (2026-08-13 —
      fields, documents, status model, scope, build order; see
      `linebooker_trading_partner_phase_v1_scope.md`)
- [x] Design review of this plan section (2026-08-13) — 10 findings applied:
      transition legality matrix, `SUSPENDED` in the field list, "no
      enforcement consumer" stated, context→tenant rationale recorded, audit
      trail added (26a1), document storage decided (metadata-only),
      `expiresAt`/`coverageCents` restored + document-driven status promoted
      to a deferred item, `ComplianceDocument` re-classified "CRUD now,
      temporal later", 26d transport/nginx/Swagger/README gaps closed,
      `vehicleTypeCode` + Admin-vs-`seafreight-app` rationale added
- [x] Independent verification pass (2026-08-13) of the above against actual
      repo code confirmed all 10 findings, but found the 26d transport
      conclusion self-contradicted finding #9: `vehicleTypeCode`-vs-refdata
      validation requires `rpc.*` (BR-D28, no REST fallback for
      backend-to-backend calls) over a tenant-scoped NATS connection
      (`refdataconsumer.New(nc *nats.Conn)`'s tenant-local import), not the
      "REST-only, no NATS" transport the same pass had just settled on.
      Corrected: this service gets `pricing-service`'s `internal/tenants`
      wiring for that one outbound `rpc.*` call; only the Admin UI-facing
      surface stays REST-only. Also corrected: `/api/trading-partners/`
      routes are context-scoped (pricing/refdata-shaped), not
      accounts-shaped, since `TradingPartner` carries `context`.
- [x] Plan phase reviewed and signed off by user before implementation
      (2026-08-13)
- [x] 26a: BR-TP01–BR-TP06 numbered and confirmed in
      `BUSINESS_RULES-TRADING-PARTNER.md`; Ginkgo specs written from rules
      first (`trading-partner-service/tradingpartner/trading_partner_test.go`),
      confirmed red (package `internal/domain` didn't exist yet — compile
      failure, not just failing assertions)
- [x] 26a: `domain.TradingPartner`/`PartnerType`/`PartnerStatus` implemented
      (`internal/domain/trading_partner.go`), 15/15 specs green
      (`ginkgo ./...`), `go build`/`go vet`/`gofmt -l` clean
- [x] 26a: every cell of the 3×3 transition legality matrix has its own spec
      — the 3 legal edges succeed, the 6 illegal ones return the matching
      sentinel error (`ErrNotRegistered`/`ErrNotActive`/`ErrNotSuspended`,
      mapped to `409 Conflict` at the REST layer in 26d — the domain layer
      itself returns Go errors, not HTTP status codes) (plus `Register`,
      which always lands in `Registered`)
- [x] 26a: `Suspend` without a `reason` is rejected at the domain boundary,
      checked before the status guard so it rejects the same way regardless
      of current status
- [x] 26a1: `trading_partner.audit_events` append-only table + Postgres
      adapter (26d). **Scoping note (2026-08-13):** BR-TP06's append-only/
      best-effort/writes-nothing-on-pre-mutation-failure guarantees are
      Postgres/handler-level, not pure domain logic — mirrors
      accounts-service's own `AuditLog` (BR-AC11), which has no dedicated
      Ginkgo/unit test either; verify live via `docker compose up` in 26d
      (register→suspend→reactivate cycle writes three rows carrying
      actor/outcome/reason; a request failing validation before any
      mutation writes none), not as a 26a domain-layer spec. **Confirmed
      done (2026-08-17 audit):** `internal/postgres/audit_repository.go` +
      `migrate.go` implement the table/adapter; 26d's own live-verification
      bullet below already confirms all 4 audit rows written correctly —
      this checkbox was just never flipped.
- [x] 26b: Compliance-document rules confirmed 2026-08-13 (BR-TP07–BR-TP11,
      including two design calls made during confirmation: `Reject` ->
      `Pending` resubmission is in scope, and `CoverageCents` carries no
      domain-level type restriction); Ginkgo specs written from rules first
      (`compliance_document_test.go`), confirmed red (`domain.DocumentType`
      undefined), then `internal/domain/compliance_document.go` implemented,
      31/31 specs green (`ginkgo ./...`), `go build`/`go vet`/`gofmt -l`
      clean. Every cell of the document-status 3×3 matrix has its own spec.
      Still pending (26d/26e, not 26b): `expiresAt`/`coverageCents` actually
      persisted in Postgres, and an expired document surfaced as expired in
      the UI (displayed, not enforced) — 26b covers the pure domain rules
      only.
- [x] 26c: Fleet-asset rules confirmed 2026-08-13 (BR-TP12: Transporter-only
      ownership; BR-TP13: registrationNo/vehicleTypeCode required,
      vin/make/model optional free text; registrationNo global uniqueness
      deferred to 26d as a repository invariant, same treatment as
      BR-TP08). Ginkgo specs written from rules first (`fleet_asset_test.go`),
      confirmed red (`domain.AddFleetAsset` undefined), then
      `internal/domain/fleet_asset.go` implemented, 37/37 specs green
      (`ginkgo ./...`), `go build`/`go vet`/`gofmt -l` clean.
- [x] 26c/BR-TP14: `vehicleTypeCode` validated against refdata via a
      tenant-scoped `rpc.*` adapter (not a domain-layer spec — see
      BUSINESS_RULES-TRADING-PARTNER.md's BR-TP14 scoping note) — unknown
      code rejected; requires the `vehicle-type` corpus (run
      `refdata-service/cmd/seed-vehicle-types` against the composed stack, or
      seed equivalently); lands with 26d. **Confirmed done (2026-08-17
      audit):** `internal/refdataclient/client.go`,
      `internal/domain/vehicle_type_validator.go`, and `internal/tenants/
      tenants.go` implement it; both 26d's and 26e's live-verification
      bullets below already confirm a bogus code rejected and a real one
      (`TAUTLINER`) accepted via a live `rpc.*` round trip — this checkbox
      was just never flipped.
- [x] `BUSINESS_RULES-TRADING-PARTNER.md` written (new domain file) and
      indexed from `BUSINESS_RULES.md` (BR-TP prefix — confirmed no collision
      with BR-0xx / BR-D / BR-AC / BR-P)
- [x] 26d: Postgres schema (`internal/postgres/migrate.go`) + repository
      adapters (`trading_partner_repository.go`, `compliance_document_repository.go`,
      `fleet_asset_repository.go`, `audit_repository.go`), REST API on
      `/api/trading-partners/{context}/...` (pricing/refdata-shaped, not
      accounts-shaped — `internal/rest/handlers.go`), docker-compose entry
      on 5436/7204. `go build`/`go vet`/`gofmt -l` clean.
- [x] 26d: `internal/tenants` lifecycle manager (per-tenant NATS connections,
      `notify.accounts.account.*` reactive provisioning/teardown, mirroring
      `pricing/internal/tenants` minus the `browserrpc.Adapter` — no `api.*`
      surface here) + `internal/refdataclient`, a trimmed
      `refdataconsumer`-style `rpc.*` client for BR-TP14's `vehicleTypeCode`
      lookup. `tenants.Manager` implements `domain.VehicleTypeValidator`
      directly, resolving the caller-supplied `tenant` (see
      `fleetAssetRequest.Tenant` in `internal/rest/handlers.go` — the Admin
      UI's existing `tenant.js` selection is the source; no prior REST route
      in this repo needed tenant identity as a request field, since Postgres
      data isn't NATS-account-partitioned the way this one `rpc.*` call is).
- [x] 26d: `NATS_CREDS_DIR` + creds volume mount wired in docker-compose;
      `nats.Connect` sets `nats.Name("trading-partner-service")` (CLAUDE.md
      rule) in `internal/tenants/tenants.go`'s `ensure`.
- [x] 26d: `frontend/admin/nginx.conf` `proxy_pass` location added for
      `/api/trading-partners/` (new location block, context-scoped passthrough
      — no prefix rewrite needed, unlike `/api/platform/accounts`), with the
      service's own BasicAuth secret injected the same way accounts-service's is.
- [x] 26d: Swagger — **skipped, correcting the design review's claim.**
      Checked directly: only shipping-service and refdata-service actually
      have Swagger; accounts-service and pricing-service don't. This service
      follows its closer precedent (accounts-service: REST-only, Admin UI,
      BasicAuth-gated, no Swagger), not the inaccurate "every backend has
      one" claim.
- [x] 26d: `README.md` port tables updated — new trading-partner-service rows
      *and* the previously-missing `pricing-service` rows (7203/5435).
- [x] 26d: Live-verified via `docker compose up` — full
      register→document-register→fleet-asset-add→activate→suspend→reactivate
      cycle against the real composed stack (nats, refdata-service +
      refdata-postgres, accounts-service + accounts-postgres,
      trading-partner-service + trading-partner-postgres). Seeded the
      `vehicle-type` corpus into context `acme` via
      `refdata-service/cmd/seed-vehicle-types`; confirmed BR-TP14 rejects a
      bogus `vehicleTypeCode` and accepts a real one (`TAUTLINER`) via a live
      `rpc.*` round trip to refdata-service over the `acme` tenant's own NATS
      connection; confirmed BR-TP13's `registrationNo` uniqueness rejects a
      duplicate; confirmed the full 409 transition-matrix guard
      (re-`Activate` on an already-`Active` partner); confirmed all 4 audit
      rows (`registered`/`activated`/`suspended`/`reactivated`, the last
      carrying `reason` in `metadata`) directly in Postgres. Also confirmed
      `frontend/admin/nginx.conf`'s new `/api/trading-partners/` route works
      end-to-end through the Admin UI's own port (7100) before any frontend
      code was written.
- [x] 26e: Admin UI "Trading partners" nav category + "Registration" screen
      built (`TradingPartnersPanel.vue`, `IconTradingPartners.vue` — both
      since restructured, see 26f below), wired
      into `App.vue`'s `sections`/`SUBTITLES`/section-render per
      `shared/unifi-theme/LAYOUT.md` — no `AppShell.vue` changes needed
      (existing slot API sufficed). API client functions added to `api.js`;
      dev-mode Vite proxy entry added alongside the existing
      `/api/platform/accounts` one. `npm run build` clean; `npx vitest run`
      shows the same single pre-existing, unrelated failure
      (`ConnectionsPanel.spec.js`'s BR-028 test) confirmed via `git stash` to
      reproduce identically without this phase's changes.
- [x] 26e: Live-verified in-browser (Browser pane) against the real
      `docker compose` stack — registered "Globex Logistics" (Transporter)
      through the actual UI, expanded the row (Compliance
      Documents/Fleet Assets/Audit Trail all render), added a fleet asset
      with a bogus `vehicleTypeCode` (clean "not recognized by refdata"
      error, live `rpc.*` round trip confirmed via network log), seeded the
      `vehicle-type` corpus into `acme-atlantic-fleet` and retried
      successfully, then Activated and Suspended (with reason) through the
      row menu and confirmed all 3 audit rows — including the reason —
      rendered in the Audit Trail sub-table. Every `/api/trading-partners/*`
      network call succeeded exactly as expected; the only console errors
      were the pre-existing, unrelated `dict-b` KV bucket 400s.
- [x] `BUSINESS_RULES-TRADING-PARTNER.md`/`BUSINESS_RULES.md`/plan updated
- [ ] Deferred items carried forward as named open questions (not silently
      dropped): lifecycle-as-CQRS/temporal exploration,
      `ComplianceDocument` temporal classification, document-expiry-driven
      status, real file storage, terminal/offboarding state,
      platform-identity vs tenant-membership split, `notify.*` publication
      once a marketplace consumer exists

#### 26f — Admin sidebar PLATFORM/SYSTEM grouping (IMPLEMENTED, 2026-08-13)

A follow-on to 26e rather than a phase of its own: adding Trading partners
made the admin sidebar's flat eyebrow list read as a pile of unrelated
categories, mixing business-layer screens with NATS/Postgres diagnostics. Not
a backend or business-rule change — no BR-TP rule moved.

- [x] `shared/ui-shell/NavList.vue` grew a third *banding* level (not a third
      nav level): a `sections` entry may now be `{ group, sections }`, a
      collapsible accent-tinted banner over one or more ordinary
      `{ eyebrow?, items }` sections. Both entry forms mix in one ordered
      array, so `seafreight-app`'s flat single section and admin's ungrouped
      Overview are unaffected. Collapse state is the component's own, like
      `AppShell.vue`'s sidebar collapse.
- [x] `shared/ui-shell/app-shell.css`: `.nav-group-toggle` reuses `.eyebrow`
      for its micro-type and finally activates the `.eyebrow.is-open svg`
      chevron-rotate rule that had been sitting there unused; adds
      `.nav-group-body` with a `visibility`-based collapse (so collapsed
      items leave the tab order) plus a `.sidebar.collapsed` override forcing
      groups open on the icon rail — the banners hide there, so a collapsed
      group would otherwise be unreachable.
- [x] Group contents indent 18px (`.nav-group-body.is-grouped`) so they read
      as nested under the banner. 18px = the banner's chevron (12px) + gap
      (6px), landing contents on the group *label's* text column rather than
      its chevron's — the tree-view convention. Ungrouped sections don't
      indent; the icon rail zeroes it so icons stay centred (verified: all 13
      rail icons share one centre, 109.5 against a sidebar centre of 110).
- [x] `admin/src/App.vue`: PLATFORM (Accounts, Trading partners →
      Shippers/Transporters, Settings) before SYSTEM (NATS, Postgres), with
      Overview ungrouped above both. Accounts moved out of the NATS eyebrow —
      it's a platform-membership roster, NATS accounts are just its mechanism.
- [x] Trading partners split per role: `TradingPartnersPanel.vue` takes a
      `partnerType` prop and is mounted twice (`:key` per role so switching
      remounts). Register dialog lost its Type field, list lost its Type
      column, list filters client-side (`GET /api/trading-partners/{context}`
      has no `type` param — noted as revisit-if-paginated).
      `IconTradingPartners.vue` → `IconTransporters.vue`, new
      `IconShippers.vue`.
- [x] `admin/src/components/NavList.spec.js` — 9 specs covering both entry
      forms, per-group independent collapse, and that an ungrouped section is
      never collapsible. `shared/ui-shell/` has no runner of its own, so the
      spec lives under admin's Vitest.
- [x] `shared/unifi-theme/LAYOUT.md` updated with the new `sections` shape,
      the "no third nav level" rule, and admin's per-app note.
- [x] Live-verified in-browser: alignment measured (group banners, eyebrows
      and items all share one left edge; the Trading partners eyebrow's text
      lines up with the Accounts icon, as mocked), independent collapse,
      icon-rail override (13/13 items reachable with a group left collapsed,
      0 banners shown), collapsed items out of the tab order, light + dark
      mode, and the role filter (a Shipper registered through the UI appears
      under Shippers and not under Transporters).

#### 26g — trading-partner-service micro registration (IMPLEMENTED, 2026-08-13)

Prompted by a real observation: the service was absent from the Admin UI's
Services panel despite running with live NATS connections. Root cause — an
outbound-only `rpc.*` requester has nothing for `$SRV` discovery to find, and
`micro.AddService` was never called. `micro.AddService` wires the
`$SRV.PING/INFO/STATS` responders independently of `AddEndpoint`, so discovery
is fixable without any transport migration. Split from 26h deliberately so the
panel fix ships now and the transport change gets its own review.

- [x] New `tradingpartner/internal/browserrpc` package: `micro.AddService`
      with `Name: "trading-partner-service"` (matching the connection's own
      `nats.Name`, the Phase 18 responder/requestor identity invariant),
      `Version: "1.0.0"`, and `Metadata{"tenant": …}` so per-tenant
      registrations are distinguishable in the panel. **Zero endpoints** — the
      `Description` says so out loud rather than leaving an operator to guess
      whether an empty endpoint list is a bug.
- [x] `internal/tenants` now holds a `browserrpc.Adapter` per tenant
      connection alongside the existing `refdataclient.Client`, mirroring
      pricing-service's `resources` struct. Registered in `ensure` (including
      a `Stop()` on the lost-race path), stopped in `TeardownByName` before
      the connection closes (so a suspended tenant stops answering discovery
      rather than lingering as a phantom row, BR-031) and in `Close`.
- [x] 6 new Ginkgo specs (`tradingpartner/browserrpc_test.go`, embedded
      in-process NATS server per refdata-service's `natsrpc_test.go`
      convention) — 43 total green. They assert discoverability *over the
      wire* via a real `$SRV.PING` broadcast, not just that `New()` returns
      nil: a constructor-only test would not have caught the original bug.
      Includes a spec pinning the deliberate zero-endpoint scope, to be
      replaced (not deleted) when 26h lands.
- [x] Stale "there is no api.* adapter / REST only" comments corrected in
      `composition.go`, `cmd/main.go`, `internal/tenants`, and
      `docker-compose.yml`.
- [x] Live-verified: `GET /api/nats/services` and the rendered panel both go
      3 → 4 services, `trading-partner-service v1.0.0`, 1 instance,
      `tenant: acme`, 0 endpoints. The `tenant=test` "Authorization Violation"
      in its startup log is pre-existing and unrelated — pricing-service and
      shipping-service log the identical error from a stale `test.creds`,
      timestamped before this change.

#### 26h — trading-partner-service api.* transport (IMPLEMENTED, 2026-08-13)

Decisions confirmed 2026-08-13 before any code: **`api.*`, not `rpc.*`**;
**REST kept** as a dual transport; **micro registration shipped first** (26g).

The `api.*` choice is the repo's own rule, not a preference: CLAUDE.md's
subject taxonomy and `ARCHITECTURE-COMMUNICATIONS.md` § 2 make `api.*`
frontend-to-service and `rpc.*` service-to-service, and state "a browser
credential is never granted `rpc.>`". The Admin UI is the only caller today,
so its endpoints are
`api.{context}.trading-partner.{entity}.{action}.v1`. `rpc.*` endpoints get
added when a *backend* caller exists — the marketplace/tender phase that
finally gives BR-TP04's `Suspend` an enforcement consumer — not speculatively.

**Correction (2026-08-13): there is no credential blocker — the earlier claim
in this section was wrong.** It conflated the Admin UI's *two* NATS
connections. `usePlatformConnection.js` (PLATFORM account, from
`GET /api/auth/adminConnectInfo`) is genuinely publish-denied —
`auth/token.go`'s `MintAdminToken` sets `Pub.Deny.Add(">")`. But the *tenant*
connection in `useNatsConnection.js` (from
`GET /api/auth/connectInfo?tenant=…`, the same `MintBrowserToken` seafreight
uses) already carries `Pub.Allow = ["api.>", "_INBOX.>"]` and
`Sub.Allow = ["api.>", "notify.>", "obs.api.>", "_INBOX.>"]`
(`auth/token.go:110-111`), and that file's own comment already said so:
"subscribe-only *use* here even though that JWT also carries
`api.>`/`_INBOX.>` publish permission: this app has no command surface of its
own to exercise it with."

trading-partner-service is per-tenant and its `api.*` subjects resolve inside
tenant accounts, so the tenant connection is the correct one and **needs no
change**. No `auth-service`/`accounts-service` edit, no new BR-AC rule, no
token-minting test churn. Also verified: scoped signing keys are *not* in play
(`provisioner.go` uses the unscoped `SigningKeys.Add`; the resolver JWTs carry
flat key arrays), so nothing silently overrides a user JWT's own permissions —
`.claude/memory/nats_scoped_signing_keys.md` describes proposed future work,
not current state.

The remaining gap is purely frontend: `connectionFactory.js` has no
`request()` surface, by an explicit design note ("adding one before anything
needs it would be exactly the speculative feature CLAUDE.md warns against").
This phase is that need arriving.
- [x] `connectionFactory.js` grew a `request()` surface; its doc comment now
      records that Phase 26h is the need arriving, rather than deleting the
      original non-speculative rationale. Also documents that `request()` is
      tenant-connection-only, since the PLATFORM credential is publish-denied.
- [x] `internal/browserrpc` endpoint constants + handlers for all 14
      operations, replacing 26g's zero-endpoint registration. Mirrors
      pricing-service's plumbing (`contextFromSubject`,
      `reply`/`respond`/`respondError`, `Nats-Responder`, `obs.api.*`
      side-channel). 26g's zero-endpoint spec was *replaced*, as it said it
      should be, not deleted.
- [x] **Two properties the api.* path enforces that REST cannot**, both now
      spec'd over the wire: `{context}` comes from the subject (a body
      `context` field cannot redirect a write), and BR-TP14's tenant comes from
      the adapter's own connection (a body `tenant` field is ignored). The
      second is why REST's `fleetAssetRequest.tenant` has no api.* equivalent —
      HTTP had no tenant identity, NATS does.
- [x] Dependency-cycle resolution: `Startup` needs the tenant Manager for
      BR-TP14's validator, while the adapter needs the handlers `Startup`
      builds. Split into a third pass — `MountTenants` → `Startup` →
      `MountAPI` — with `tenants.Manager.apiDeps` nil until `MountAPI` runs and
      every `adapter` teardown path nil-guarded. pricing-service doesn't hit
      this because it has no refdata dependency.
- [x] `admin/src/api.js`'s trading-partner functions moved from `fetch` to NATS
      request; `TradingPartnersPanel.vue` unchanged (signatures held).
      `tpSubject` throws rather than emit a malformed subject if a context
      contains a dot/space/wildcard — a dot would shift every later token and
      silently resolve the wrong context.
- [x] REST retained and still wired in `cmd/main.go`. **Decision: the
      nginx/vite proxy entries and BasicAuth secret stay.** This diverges from
      pricing-service (which has REST but no browser proxy route) on purpose —
      keeping port 7100's `/api/trading-partners/` path preserves the exact
      curl surface used to verify 26d, at the cost of some now-unused browser
      reachability. Revisit if that path ever drifts from the api.* behaviour.
- [x] `ARCHITECTURE-COMMUNICATIONS.md` (new § 3.1 adapter matrix + the
      registration-vs-endpoints and no-rpc-without-a-backend-caller notes),
      `BUSINESS_RULES-TRADING-PARTNER.md`, and this plan updated together.
- [x] **Bug found by live verification, not by the specs:** the first migration
      of `addFleetAsset` dropped the partner `id`, which REST had carried as a
      URL path segment — Postgres rejected it with `invalid input syntax for
      type uuid: ""`. The frontend spec had asserted what was *removed*
      (`tenant`) but never what had to be *kept* (`id`). Fixed, and the spec now
      blanket-asserts every per-partner operation sends its id; confirmed
      red-on-bug then green-after-fix rather than assumed.
- [x] Tests: backend 52 Ginkgo specs (was 43) — registration, subject shape,
      and wire round-trips including context-spoofing, tenant resolution,
      404 mapping, `Nats-Responder`, and the `obs.api.*` mirror, against an
      embedded NATS server with in-memory fakes. Frontend 13 new Vitest specs
      (46 total passing).
- [x] Live-verified in-browser against the real stack: Services panel 0 → 14
      endpoints; Transporters list, all three expansion sub-tables, fleet-asset
      add (with BR-TP14 refdata validation passing), and a Reactivate all over
      NATS with **zero** REST calls to `/api/trading-partners/*` recorded. Data
      confirmed in Postgres, including the audit row's
      `nats:admin-tenant/<id>` provenance. REST and api.* cross-checked
      agreeing on the same records.

---

### Phase 27 (IMPLEMENTED, 2026-08-14) — Admin UI: Account Activity Panel (/accstatz)

#### Goal

Surface the NATS server's per-account traffic (`/accstatz`) in the Admin UI —
nothing showed it before: Connections lists raw sockets one at a time,
Accounts shows JetStream storage/consumer *limits*, not wire activity.
Follows a design-recommendation request (frontend-design skill) that
compared three placements — a new panel (chosen), columns on the existing
Accounts table, and an alert-only banner — and picked the new panel because
`/accstatz` comes off the same `:8222` monitor port as `/connz`/`/varz`
(Connections' Phase 17c proxy pattern), not from accounts-service.

- [x] Backend: `GET /api/nats/account-activity` in
      `dictionary/internal/rest/nats_ops.go` — `listNatsAccountActivity`
      proxies `/accstatz` (primary read, 502 on failure) and resolves
      `tenantLabel` via a secondary, best-effort `/connz` probe reusing
      `tenantLabelsByAccount` (BR-028's exact mechanism). Swagger docs
      (`docs/docs.go`, `swagger.json`, `swagger.yaml`) hand-patched per the
      established convention (`swag_regen_diff_noise.md`).
- [x] Frontend: `AccountActivityPanel.vue`, modeled on `ServicesPanel.vue`'s
      `.svc-card` pattern (dot · name · inline stat pairs · chevron) rather
      than inventing a new row shape. New nav entry under SYSTEM → NATS
      (`IconActivity.vue`), between Services and Log.
- [x] **The one deliberate design move:** `slow_consumers` gets no routine
      tile. At zero it's silent — same `.dot.ok` convention `ServicesPanel`
      already uses — matching the "facts that only matter in an exceptional
      state get rendered only in that state" rule established for
      Connections' paged-note (`admin_stat_card_one_ratio_rule.md`). Nonzero
      turns the dot red, tints the card border, swaps the "subs" stat for a
      red "slow" stat, and opens the expansion on a named alarm line; a
      summary-row banner appears under the same condition. See BR-034
      (BUSINESS_RULES-SHIPPING.md).
- [x] Tests: 5 new Go tests (`nats_ops_test.go`) covering reshaping/sorting,
      tenant-label resolution off the secondary `/connz` probe, and 502s;
      10 new Vitest specs (`AccountActivityPanel.spec.js`) covering the
      silent-at-zero / alarm-at-nonzero contrast specifically. Full backend
      (`ginkgo ./...`) and frontend (`vitest run`) suites green.

---

### Phase 28 (IMPLEMENTED, 2026-08-16) — Distributed Tracing for Inter-Service Comms

#### Goal

The Request/Reply panel renders a *message log*, not a trace. Its correlation
key is `req.Reply()` — an `_INBOX` subject generated fresh by each requestor
and never propagated — so a `browser → shipping-service → refdata-service`
call shows as two unrelated rows, and `evt.*`/`notify.*`/KV writes show as
none at all (no reply inbox means no correlation id even in principle, so the
whole async CQRS tail is unjoinable to the command that caused it). A
`refdataconsumer` call that times out or finds no responder currently emits
**zero** records at either end.

Introduce `obs.trace.*`: one W3C `traceId` minted in the browser and carried
in a `traceparent` header through every hop, service, NATS account, and async
boundary; spans assembled server-side into a KV bucket; and a waterfall in
the Admin UI that shows what a request *caused*, including the account
boundaries it crossed and the read-model work that continued after the client
was already unblocked. Approved UI mockup and ingest-topology design visual
were reviewed before this phase was written.

#### Decisions locked (with user, 2026-08-14)

- **Evolve §4.5 in place**, don't add a ninth panel. Nav key `rpc` and the
  8-entry nav are unchanged; the panel becomes *Request/Reply & Traces* with a
  `[traces] [messages]` view toggle. The flat view survives because it answers
  "is anything arriving on this subject" better than a trace list does.
- **Payloads stay on spans**, but published to the **PLATFORM account only**,
  with a redaction denylist and a 4 KiB cap (truncation flagged, never
  silent). No browser credential is granted `obs.trace.>`.
- **Full rollout in one phase** — all four RPC adapters plus accounts-service
  and the auth surface. Wider than was recommended; mitigated by the
  independently landable sub-phases below.
- **`TRACES` stream + KV bucket keyed by trace id**, assembled by a single
  durable consumer so there is no read-modify-write race. That store split is
  Shape A applied to the lab's own telemetry — replayable log in JetStream,
  keyed current state in KV, Postgres deliberately absent.

#### Design

- **Wire format** — `obs.trace.{context}.{service}.{entity}.{action}`, fixed
  6-token arity so `SubjectPath.vue`'s positional faceting keeps working.
  `traceSpan` is a **strict superset** of `obsEnvelope`: no field renamed or
  retyped, every addition `omitempty`. `RpcPanel.vue` keys rows on
  `correlationId` and `browserrpc_test.go` pins backward-compatible decoding
  of the pre-BR-D36 shape, so a rename breaks the panel. Field names mirror the
  OTLP `Span` message 1:1. `correlationId` is retained as a per-hop field — it
  is **not** the span id.
- **One decorator, not 58 edits.** All four adapters register endpoints
  through a table loop with a single `svc.AddEndpoint` call, and both
  `micro.Handler` and `micro.Request` are interfaces, so one wrap at that call
  site replaces every hand-pasted request-side `publishObs` line (8 shipping,
  5 refdata, 33 pricing, 12 trading-partner). Safe against micro's own stats:
  `service.reqHandler` holds the original `*request` and the wrapper delegates
  `Respond` to it, so `$SRV.STATS` keeps counting errors and the Services
  panel is unaffected. Outcome comes from the existing
  `respond`/`respondError` tails via `natstrace.SpanFrom(req)` (nil-safe for
  unwrapped requests in tests) because those already hold the typed error and
  its 404/500 classification.
- **Tracer is per-adapter, never a package singleton** — shipping, pricing,
  and trading-partner construct one adapter *per tenant NATS account*
  (`rest/tenant.go`, `tenants.go` ×2); only refdata is per-process. `obs.*`
  must not cross an account boundary except via the sanctioned export.
- **`natstrace` is duplicated per service, not shared.** Five independent
  `go.mod` files, no `go.work`, and each Dockerfile builds with its own
  directory as build context, so a `replace`-based local module is simply
  absent from the image; widening the context to `./backend` would invalidate
  all five services' Docker layer caches on any single edit, regressing the
  documented `docker compose up --build` workflow. This matches how
  `obsEnvelope`, `errorResponse`, `obsSubjectFor`, and `contextFromSubject` are
  already duplicated on purpose. Drift is pinned by the §6 wire spec plus a
  per-service contract test.
- **No `go.opentelemetry.io/*` dependency, and the OTLP exporter is a
  container, not a library.** The OTel Go API is `context.Context`-based, but
  every adapter hands the application layer a fresh `context.Background()` at
  ~60 call sites, so real span nesting would mean changing every command and
  query signature it reaches — through the domain layer Quality Rule 3 keeps
  framework-free. Adopting OTel only at the adapter boundary would pay the
  dependency cost for a *flat* trace. `natstrace` is therefore hand-rolled,
  W3C-compatible on the wire and OTLP-shaped in its fields, and the exporter
  is a ~150-line service consuming `obs.trace.>`. That buys retroactive
  export (start the collector late, replay the retained hour — impossible for
  an in-process exporter), no-code toggling, one copy of the OTLP mapping, and
  makes the `obs.*` isolation invariant structural rather than maintained.
  Revisiting the real SDK for `otelpgx`/`otelhttp` auto-instrumentation is
  recorded as an explicit deferred decision.

#### Sub-phases (each independently landable)

- [x] **28a — `natstrace` + decorator, prototyped in trading-partner.** It
      already has `observe`/`reply`/`actor` helpers and no JetStream, so the
      diff is 12 lines deleted, 1 wrap added. Validates the decorator and the
      `SpanFrom(req)` upcast end to end against the live panel before the
      pattern is copied anywhere.
- [x] **28b — Copy to pricing, shipping, refdata.** Reply-side `publishObs` in
      `respond`/`respondOK`/`respondError` becomes `sp.End()` / `sp.Fail()`.
      All three done, `natstrace` verified byte-identical in logic across all
      four services (trading-partner/pricing/shipping/refdata — doc comments
      only differ), `ginkgo ./...` green in every service:
      - **shipping:** `dictionary/internal/natstrace` wired into
        `dictionary/internal/browserrpc/adapter.go`; 8 request-side
        `publishObs` lines and `obsEnvelope`/`obsSubjectFor`/`versionSuffix`
        gone. Retires shipping's `obs.api.*` publish outright (BR-026/BR-027
        amended in `BUSINESS_RULES-SHIPPING.md`) rather than running it
        alongside `obs.trace.*` — `[messages]` view goes dark for shipping
        traffic until 28g, an accepted gap per "full rollout in one phase."
        114/114 `dictionary` suite + 5/5 `natstrace` specs.
      - **pricing:** same wiring, 33 request-side `publishObs` lines removed
        (matching the plan's count exactly); no pre-existing round-trip test
        existed for this adapter, so none needed updating. 43/43 `Pricing`
        suite + 5/5 `natstrace` specs.
      - **refdata:** wired into `refdata/internal/natsrpc/adapter.go` (this
        service's adapter package, `natsrpc` not `browserrpc`); 5 request-side
        `publishObs` lines removed; `Deps.JS`/`RPCTRACE` JetStream-replay
        branch (BR-D29) removed from `publishObs`'s replacement since
        natstrace only does plain `nc.Publish` — BR-D29's catch-up replay is
        deferred to 28f's TRACES stream. Found and fixed a **pre-existing,
        unrelated test bug** while getting this suite green: `ItemGetRequest`/
        `TypeListRequest`/`ItemGetVersionedRequest`/`LocalesListRequest` all
        carry `Context` in the body (Phase 21 design — subject position 2 is
        the caller's NATS account public key here, not the context name), but
        9 existing tests never set it, so every rpc.* success-path assertion
        silently resolved against context `""` instead of the seeded
        `"acme-test"` and got back zero-value replies — confirmed pre-existing
        via `git stash` against this exact commit (same failures with
        natstrace entirely absent). Fixed by adding `Context: itemCtx` (test
        file only, zero production-code risk). All 141 refdata specs +
        5/5 `natstrace` specs now green, comprehensively fixing what was
        previously an 11-failure suite.
      `gofmt` clean across all four `natstrace` copies (fixed one map-literal
      alignment diff in shipping's/refdata's copies).
- [x] **28c — Outbound inject.** `refdataconsumer.requestRPC` and
      `refdataclient.Client` — the `nats.Header` already exists for
      `Nats-Requestor`, so it is one line each. **One span per logical call
      with a `rpc.retry_count` attribute, not one per retry attempt**:
      generate the child span id before the retry loop, or a 3-attempt failure
      yields three parentless siblings. First hop crossing both a service and
      an account boundary.
      Added `natstrace.ContextWithSpan`/`SpanFromContext` (the only ctx.Value
      use in this codebase — deliberately narrow, costs the domain layer
      nothing since ctx is already threaded everywhere) so a browserrpc
      handler's inbound span rides down through the application/command layer
      to the outbound rpc.* call unchanged. Added `Tracer.StartOutbound` for
      client-side spans. **Design correction made mid-implementation:**
      `StartOutbound` cannot parse `{context}/{entity}/{action}` positionally
      off the subject the way `Start` does for inbound requests — an outbound
      caller's subject is a tenant-account **local alias**
      (`refdata.item.get.v1`) that accounts-service's `provisioner.go` remaps
      server-side to the real `rpc.{tenant}.refdata.item.get.v1`
      (`jwt.RenamingSubject` — the account's own identity lands at that
      token, deliberately never a caller-supplied value). An earlier draft
      assumed the bare local alias was itself a bug, rewrote it to construct
      the full subject directly, and only caught the mistake before landing
      it by checking accounts-service's actual import/export declarations —
      that would have both broken routing and put a caller-controlled value
      where the import exists specifically to prevent one. `StartOutbound`
      now takes the label fields as explicit parameters instead. Also moved
      shipping-service's `natstrace` package from `dictionary/internal/` to
      top-level `internal/` — `internal/refdataconsumer` sits outside
      `dictionary/`'s subtree, so Go's internal-visibility rule blocked the
      import from its original location. `ginkgo ./...` green in all four
      services (trading-partner/pricing/shipping/refdata), including a new
      `refdataclient` suite (this package had no tests at all before Phase
      28c) and three new `refdataconsumer` BR-037 specs.
- [x] **28d — Async tail.** `jstream.Publisher` (both services — the sole
      `evt.*` funnels, needing `PublishMsg` plus a `PublishWithTrace` method
      since the ctx is empty today), the three `Consume` callbacks
      (per-message spans only — `handler.go` uses `context.WithoutCancel`, so
      the message ctx is process-lifetime), and `kvstore.publishNotify`. Note
      `jetstream.KeyValue.Put` takes no headers, so a KV entry cannot carry
      trace context — the derived notify does, and trace data must not go in
      the body because the KV inspector distinguishes PUT from DEL by empty
      payload. This sub-phase is what makes the reply-ack line real.
      **shipping-service done** (`jstream.Publisher`/`kvstore.publishNotify`
      were already landed as foundational pieces before this pass): wired the
      remaining 9 `dictionary/internal/browserrpc/adapter.go` handlers'
      `context.Background()` call sites to
      `natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req))`
      so an inbound api.* request's span rides down through
      `commands.ShipHandler.publish`/`commands.ContainerHandler.publish`
      (both now call the widened `Publisher.PublishWithTrace`, `Publisher`
      itself widened to require it) onto the resulting evt.* JetStream
      publish. `dictionary/internal/eventhandler/handler.go`'s shared
      `register()` (used by both `RegisterShapeA`/`RegisterShapeB`),
      `container_handler.go`'s `RegisterContainers`, and `meta_handler.go`'s
      `RegisterMeta` each now mint one span per message via
      `Tracer.StartFromHeaders` (labeled from the subject fields each
      callback already parses — never re-derived positionally, same
      six-token-vs-five-token reasoning as 28c's `StartOutbound` amendment),
      record `entity_id`, thread it via `natstrace.ContextWithSpan` into the
      KV write and `publishNotify`/`publishRawNotify`, and close it with
      `sp.End`/`sp.Fail` on Ack/Nak. `RegisterShapeB` gained an `nc *nats.Conn`
      parameter purely so `register()`'s shared Consume callback can build a
      `Tracer` for it too (Shape B has no notify.* publish of its own).
      `ginkgo ./...` green (129 dictionary-adjacent specs: 120 in
      `dictionary`, plus `natstrace`/`kvstore`/`jstream`/`eventhandler`
      suites) — new specs added in `dictionary/trace_async_test.go` (evt.*
      publish shares its traceId with the reply-side span, including the
      no-inbound-traceparent root-span case; each of the four
      `RegisterShapeA`/`RegisterShapeB`/`RegisterContainers`/`RegisterMeta`
      Consume callbacks publishes exactly one span per message with the
      correct `entity`/`action`/`entity_id`), `dictionary/internal/eventhandler/publish_notify_test.go`
      (white-box `publishNotify`/`publishRawNotify` traceparent
      attach/omit/nil-`nc` cases), and two new specs in `internal/kvstore/kv_test.go`
      (`Store.Put`'s derived notify attaches/omits the `Traceparent` header
      per whether `ctx` carries a span).
- [x] **28e — accounts-service + auth.** `auth/token.go` has no NATS publish
      at all (it is pure JWT minting), so instrument one `http.Handler`
      decorator at `Mount` — symmetric to the micro decorator, covering every
      accounts REST endpoint — plus `publishAccountEvent`, which all four
      notify publishes already funnel through. Also the 9 `notify.accounts.*`
      subscriber call sites across 3 files.
      accounts-service had no `natstrace` at all (a new service for Phase 28)
      — new `internal/natstrace` package, same core span/redact/truncate
      mechanism as the other four services (byte-identical logic), but
      trading the micro.Request-shaped `Start`/`Middleware`/`SpanFrom` for an
      `HTTPMiddleware(next http.Handler) http.Handler`, since this service's
      transport is REST, not NATS micro. Wired once around the whole
      `*http.ServeMux` in `cmd/main.go` (`server.Handler =
      tracer.HTTPMiddleware(mux)`), covering both `accounts.Handlers.Mount`
      and `auth.Handlers.Mount`'s routes from one point — even more literally
      "one decorator" than the per-`AddEndpoint` wrap the other four services
      use. Span labels: `context="_platform"` (this service administers the
      tenant axis itself, no `{context}` of its own — CLAUDE.md's "context-free
      services" note), `service="accounts"`, `entity` from the first `/api/`
      path segment, `action` the lowercased HTTP method; exact path/method/
      status land as span attributes instead. A `>=400` response finishes the
      span as `Fail`, matching how a NATS reply's error path already does.
      `publishAccountEvent` (all four lifecycle publishers funnel through it)
      now mints its own outbound span via `Tracer.StartOutbound`, continuing
      whatever span the HTTP request carried, and attaches its `Traceparent`
      to the `notify.accounts.*` publish — required all four publishers
      (`publishAccountCreated`/`Suspended`/`Reactivated`/`JSLimitsUpdated`)
      to gain a `ctx` parameter (mechanical, `r.Context()` was already in
      scope at each of their 4 call sites). The 9 subscriber call sites
      (`shipping-service/dictionary/internal/rest/tenant.go`,
      `trading-partner-service/tradingpartner/internal/tenants/tenants.go`,
      `pricing-service/pricing/internal/tenants/tenants.go` — 3 subscriptions
      each) now start a per-message span via `StartFromHeaders`, continuing
      the inbound `Traceparent`, before calling `EnsureByName`/`TeardownByName`
      — the "created" closure is reused verbatim for "reactivated" in all
      three files, so the span's action label reads the actual subject's
      trailing token rather than a hardcoded string. **Design correction
      caught mid-implementation:** `publishAccountEvent`'s first draft simply
      forwarded the HTTP request's own span's `Traceparent()` unchanged onto
      the notify publish — a test written to assert "the notify's span id
      differs from the inbound request's span id" caught that this collapsed
      two hops into one span, inconsistent with how every other outbound
      publish in this phase (rpc.* clients, JetStream publishes) mints its
      own child span via `StartOutbound` first; fixed before landing.
      `ginkgo ./...`/`go test ./...` green in all five services (new
      `natstrace` suite in accounts-service, new BR-037 test in
      `accounts/handler_test.go`, new subscriber-side test in shipping-service's
      `dictionary/internal/rest/tenant_lifecycle_trace_test.go`).
- [x] **28f — Trace store + cross-account plumbing.** `TRACES` stream
      (LimitsPolicy, MaxAge 1h, MaxBytes capped), single-writer projector into
      `traces-_platform` keyed `trace.{traceId}`, per-tenant `obs.trace.>`
      stream export imported into PLATFORM, and `allow_trace: true` on service
      exports / stream imports in minted account JWTs. This closes the
      cross-account gap documented at `browserrpc/adapter.go`'s `Deps` comment.
      **Cross-account plumbing (accounts-service):** the design gap found
      mid-implementation — PLATFORM's own account JWT has to explicitly
      import each tenant's `obs.trace.>` export (no wildcard cross-account
      import exists in NATS's decentralized JWT model) — is solved with a
      fully dynamic re-signing mechanism, not a bootstrap-only one.
      `accounts/provisioner.go` gained: `tenantExports()` (a Stream export of
      `obs.trace.>` on the tenant's own claims — no `allow_trace` here,
      `jwt.Export.Validate` rejects that flag on anything but a Service
      export), wired into `newAccountClaims`'s cross-account branch via
      `claims.Exports.Add(tenantExports()...)`; and `addPlatformTraceImport`,
      which looks up PLATFORM's current claims via the already-generic
      `LookupAccountClaims`, idempotently appends a Stream import (`{Account:
      <tenant pubkey>, Subject: "obs.trace.>", Type: Stream, AllowTrace:
      true}` — legal here since `AllowTrace` requires a **Stream** import,
      the mirror image of the export-side restriction), and re-signs/re-pushes
      via the existing `pushClaimsUpdate` ($SYS.REQ.CLAIMS.UPDATE). Wired into
      `CreateAccount` right after the new tenant's own claims push succeeds,
      guarded by `platformPublicKey != ""` so the existing low-level
      provisioner tests (which pass an empty platform key and exercise no
      cross-account wiring at all) are unaffected. `nats/bootstrap-operator.sh`
      gained the day-0 nsc equivalent for the two pre-seeded tenants: each of
      ACME/GLOBEX now runs `nsc add export --account $account --subject
      "obs.trace.>"`, and PLATFORM imports both via `nsc add import
      --account PLATFORM --src-account $account_pub --remote-subject
      "obs.trace.>" --local-subject "obs.trace.>" --allow-trace` — verified by
      actually running the script against real `nsc` in a scratch copy (not
      the committed `nats/` artifacts) and decoding the resulting JWTs: ACME's
      export has `type: stream` with no `allow_trace`; PLATFORM's imports for
      both ACME and GLOBEX have `type: stream, allow_trace: true`, scoped to
      the correct account each. Tested against an embedded operator-mode NATS
      server (`accounts/provisioner_test.go`'s new spec): `CreateAccount` for
      two tenants leaves PLATFORM importing both without one overwriting the
      other, and a repeated `addPlatformTraceImport` call for an
      already-imported tenant (exposed to the black-box Ginkgo spec via a
      standard `export_test.go` bridge, `provisioner_export_test.go`) is a
      no-op, not a duplicate import — plus a white-box unit test
      (`provisioner_claims_test.go`) asserting the tenant-side export survives
      a plain re-sign. BR-AC30 amended with the concrete file/function names
      (the original text referenced a speculative `accounts/jwt.go`'s
      `MintTenantAccount`, which was never how this landed — the real grant
      is account-level, in `provisioner.go`, not a user-JWT `Sub.Allow` entry).
      **Trace store (shipping-service):** new
      `dictionary/internal/eventhandler/trace_store.go`'s `RegisterTraceStore`
      provisions the `TRACES` stream (`obs.trace.>`, `LimitsPolicy`, 1h
      `MaxAge`, 64 MiB `MaxBytes`) and a `traces` KV bucket (wrapped in
      `internal/kvstore.Store`, not raw `jetstream.KeyValue` — bare bucket
      name matching `kvstore`'s "named by the prefix alone" convention, with
      the platform scope folded into the key instead, `_platform.trace.
      {traceId}`; this was a design correction made while starting 28g's
      panel work, before any panel code was written — the original 28f
      landing used a raw-KV `traces-_platform` bucket, replaced here because
      wrapping it in `kvstore.Store` and calling `EnableNotify` gets the
      trace waterfall panel's entire bootstrap+live feed for free via
      mechanisms every other KV panel already uses, eliminating what would
      otherwise have been a bespoke KV-watch bridge goroutine and a new REST
      endpoint), then runs a durable consumer (`trace-store-projector`)
      whose `appendSpan` mirrors `container_handler.go`'s read-then-write
      pattern: read the existing KV record, skip the write if the incoming
      `spanId` is already present (redelivery-safe), else append the raw
      span JSON to the record's `spans` array and write back — merge, never
      overwrite-with-latest. Must run on `mono.PlatformFullJS()` (creating a
      stream/KV bucket is a `$JS.API.>` write, which shipping-admin's
      restricted `mono.JS()` is deliberately locked out of), so
      `monolith.Monolith.PlatformFullJS`'s doc comment was widened from
      "read-only cross-account introspection, nothing else should use it" to
      note this second, write-capable use; `platformNC` (`mono.NC()`, the
      restricted shipping-admin connection) is passed too, for
      `EnableNotify`'s publish — its existing `notify._platform.>`
      `--allow-pub` grant (`nats/bootstrap-operator.sh`) already covers the
      new `notify._platform.kv.traces.>` subject, no bootstrap script change
      needed. `accounts-service/auth/token.go`'s `MintAdminToken` gained that
      subject in its `Sub.Allow` list (the browser's admin connection
      previously had no KV-notify grant at all), with `token_test.go`'s
      pinned assertion updated to match. Nil-safe exactly like
      `RegisterRefdataNotify`/`RegisterRPCTraceNotify` when `platform.creds`
      isn't configured. Wired into `dictionary/composition.go` right after
      those two bridges. Tested against an embedded JetStream-enabled NATS
      server: `trace_store_test.go` (black-box) proves a published span lands
      in the KV bucket and that two spans sharing a `traceId` merge into one
      entry instead of one overwriting the other, plus the nil-safe no-op;
      `trace_store_appendspan_test.go` (white-box) proves same-`spanId`
      redelivery dedup and that two different traces never cross-contaminate
      each other's KV record. BR-036 amended (Phase 28f amendment) naming the
      concrete stream/bucket/functions behind "PLATFORM's cross-account trace
      store." `ginkgo ./...`/`go test ./...` green in both accounts-service and
      shipping-service.
- [x] **28g — Panel + OTLP bridge + retirement.** `[traces] [messages]` toggle
      in `RpcPanel.vue`, new `TraceWaterfall.vue`, the `otlp-bridge` container
      plus env-flagged `jaeger:all-in-one`, then retire `obs.rpc.*`/`obs.api.*`
      once the messages view derives from trace spans.
      **Panel piece landed (checked in with the user before continuing to the
      OTLP bridge/Jaeger/retirement piece):** `stores/ui.js` gained `rpcTab`
      (defaults `'traces'`, same App.vue-remount-survival pattern as
      `accountsTab`). `RpcPanel.vue` wraps its existing messages-view markup
      in `v-else` behind a small `.view-toggle` chip pair
      (`[traces]`/`[messages]`, matching ARCHITECTURE-ADMIN.md §4.5's
      design-history call for "a toggle inside this panel," not a page-level
      `Tabs` bar or a ninth nav item) and renders the new
      `TraceWaterfall.vue` for the traces tab. New `jsonHighlight.js`
      extracts `escapeHtml`/`highlightJson` out of `RpcPanel.vue` so both
      panels share one implementation. `TraceWaterfall.vue` bootstraps via
      the *existing* generic `GET /api/kv/buckets/platform/traces/entries`
      (no new REST endpoint — see BR-036's Phase 28g amendment for why) and
      subscribes live to `notify._platform.kv.traces.>` on the PLATFORM
      connection; per-trace summaries (root span, reply/consistent
      durations, ok/error, account count) and per-span waterfall rows
      (depth, offset, duration, account, crossing, sync/evtl/bad kind, the
      ack-line insertion point) are computed client-side from the KV
      record's raw `spans` array. Two known simplifications versus the
      approved mockup (`diagrams/admin-traces-panel.html`), both because the
      wire span has no field for them: the account gutter is a coarse
      PLATFORM/TENANT split, not a real per-tenant label (see BR-035's Phase
      28g amendment); the mockup's OTel `spanKind` tag is omitted rather
      than faked. `TraceWaterfall.spec.js` covers BR-035's four required
      assertions (5 specs, all green). Building this required a **prerequisite
      backend change, confirmed with the user first**: `natstrace.Span` in
      all five services gained `startedAt`/`traceSpan` gained `durationMs`
      (BR-036 Phase 28g amendment) — without it there was no wire signal for
      a span's own duration, only two different spans' finish timestamps,
      which the waterfall's proportional bars need to not be meaningless.
      Landed via 4 parallel agents (one per service, after doing
      shipping-service directly myself as the reference diff) plus one
      direct edit each to `BUSINESS_RULES-SHIPPING.md`. Verified live against
      the running docker-compose stack (all containers rebuilt), which
      surfaced and required fixing a real, unrelated capacity issue:
      PLATFORM's account was already at its 20-stream JetStream ceiling from
      11h of accumulated demo usage, so `shipping-service` crash-looped
      creating `TRACES`/`traces`. Fixed live and non-destructively — **the
      user ran a small one-off Go script** (using `nats/keys/operator-
      signing-key.nk`, the same `$SYS.REQ.CLAIMS.UPDATE` re-sign mechanism
      `accounts-service`'s own `Provisioner.UpdateAccountLimits` uses for
      tenants, just applied directly to PLATFORM since it isn't a tenant in
      the Store) to bump the limit 20→100 — the auto-mode classifier
      correctly declined to let the agent run this itself, since it directly
      wields the operator signing key outside the normal application code
      path. After that, real `obs.trace.*` traffic (refdata-service,
      accounts-service, the Admin UI's own `api.*`/auth calls) was observed
      flowing end-to-end: bootstrap fetch, live KV-notify updates (trace
      count grew live with the panel open, no reload), waterfall bar
      rendering, and the span detail pane (headers + syntax-highlighted
      body) all confirmed working in the browser. Real cross-account
      (ACME→PLATFORM) trace assembly was **not** verified on this live
      stack — the running `nats.conf`'s resolver still has the pre-28f
      tenant JWTs (no `obs.trace.>` export/import), and regenerating it
      would need a full `docker compose down -v`, which was deliberately
      not done to avoid destroying 11h of accumulated demo data; that gap is
      covered structurally by 28f's own Go integration tests instead. The
      `[messages]` view and its `obs.rpc.*`/`obs.api.*` traffic were
      confirmed unaffected (not yet retired — that's still pending below).
      **OTLP bridge + Jaeger landed:** new standalone module
      `backend/otlp-bridge/` (own `go.mod`, own `Dockerfile`, ~230 lines
      across `cmd/main.go` and `internal/otlpmap/map.go` — not hexagonal
      like the five instrumented services, since it's a translation utility
      with no domain layer or business rules of its own). Per
      ARCHITECTURE-COMMUNICATIONS.md § 6's already-approved design: a
      `TRACES` JetStream consumer, never an in-process SDK — `openConsumer`
      switches between a durable, `DeliverNewPolicy` live-tailing consumer
      (default, resumes across restarts) and an ephemeral `DeliverAllPolicy`
      consumer (`OTLP_BRIDGE_REPLAY=true`, re-exports the whole retained
      window on demand); a `batcher` accumulates spans until `batchSize`
      (100) or `batchInterval` (2s), POSTs one OTLP/HTTP export request, and
      only Acks the batch on a 2xx response — a failed POST Naks every
      message in it, so nothing is lost while Jaeger is unreachable (the
      span stays on `TRACES`, redelivered next attempt). `internal/otlpmap`
      is the pure field-for-field mapping (7 unit tests, no NATS
      dependency): `WireSpan` mirrors only the subset of `natstrace.go`'s
      `traceSpan` the bridge needs (mirroring `trace_store.go`'s own
      `traceSpanKey` precedent for "just enough, not the full shape");
      `spanKind` is never set (OTLP's `SPAN_KIND_UNSPECIFIED` zero value) —
      inventing one would be interpretation, not mapping, matching BR-035's
      documented spanKind omission. **Corrected against a live Jaeger
      rejection, not assumed:** trace/span ids are passed through as the
      same hex `natstrace` already emits, *not* re-encoded to base64 —
      generic protobuf JSON mapping would base64-encode a `bytes` field,
      but Jaeger's OTLP/HTTP receiver decodes ids through OTel collector's
      `pdata` codec, which expects hex specifically (confirmed live: a
      base64-encoded id was rejected with `"invalid length for ID"`); fixed
      and pinned with a test (`TestMarshalExportRequestPassesIdsThroughAsHex`)
      before moving on. `docker-compose.yml` adds `jaeger` (image
      `jaegertracing/all-in-one:1.68.0`, conventional ports 16686/4318 — same
      "keep the widely-recognized port" precedent as NATS/Postgres) and
      `otlp-bridge` (reuses `platform.creds` — read-only JetStream consume
      needs no dedicated credential), both behind Compose's `otlp` profile
      so a bare `docker compose up` is unaffected: two services added or
      removed, no env flag threaded through five service binaries, per the
      design's own "no-code toggling" rationale. Verified live against the
      running stack: real `refdata`/`accounts` spans (correct parent/child
      references, `subject`/`entity`/`action`/`direction`/`correlationId`
      tags) confirmed via both Jaeger's `/api/traces` query API and its UI.
      `go vet`/`gofmt`/`go test ./...` all clean; `ginkgo ./...` re-run in
      shipping-service afterward to confirm zero effect on the five
      instrumented services (9/9 suites still green, as expected — nothing
      in any of them changed). README.md's port table and Docker instructions
      updated with the `--profile otlp` command.
      **Retirement landed.** Investigated before touching anything (a
      dedicated Explore pass, not assumption): the publish side of
      `obs.rpc.*`/`obs.api.*` was already fully dead everywhere — Phase
      28a-28e had already replaced every adapter's `publishObs` call with a
      natstrace span, so what remained was dead/no-op plumbing plus stale
      docs, not live traffic to migrate. What actually changed:
      - **Frontend:** `RpcPanel.vue`'s `[messages]` tab no longer subscribes
        `obs.rpc.>`/`obs.api.>` (both already carrying nothing for any
        service) — it now derives from the same `obs.trace.*`/`traces` KV
        bucket feed `TraceWaterfall.vue` reads, flattened to one row per
        *span* instead of one row per *trace*. Real, unavoidable UX
        difference: a span carries only the reply side (BR-037), so the old
        two-pane Request | Reply detail split is gone in favor of one
        Body/Headers section, and the three-state status model
        (pending/ok/error) becomes two (ok/error) — a span is only ever seen
        already-finished. `getRpcTraceReplay` removed from `api.js`. New
        `RpcPanel.spec.js` (5 specs) covers the flattening, family/status
        filtering, and the single-pane detail view — this component had no
        prior test coverage at all.
      - **shipping-service:** `eventhandler.RegisterRPCTraceNotify` and its
        call site removed; `rpcTraceReplayOnce`/`GET /api/rpctrace/replay`
        removed; the synthetic-RPCTRACE-traffic tests that existed only to
        exercise this now-removed bridge/endpoint deleted with a removal
        note (`platform_notify_test.go`, `replay_test.go`); Swagger
        regenerated (`swag init`), confirmed zero `rpctrace` references
        remain.
      - **refdata-service:** `RPCTraceStreamName`/`RPCTraceMaxAge` consts and
        the `RPCTRACE` stream provisioning removed from `composition.go`;
        `natsrpc.ObsSubjectWildcard` (kept post-28b only to back that
        provisioning) removed now that nothing needs it.
      - **accounts-service:** `MintBrowserToken`'s `obs.api.>` subscribe
        grant dropped. Also caught and fixed a bug the investigation
        surfaced but I hadn't planned to touch yet: `MintAdminToken` still
        granted `notify._platform.rpctrace.>`, a subject nothing publishes
        to anymore post-retirement — dropped that too, with
        `token_test.go`'s assertions updated for both functions (ginkgo
        re-run confirmed both catches: 84/84 + 19/19 + 6/6 specs green).
      - **`nats/bootstrap-operator.sh`:** `shipping-admin`'s RPCTRACE
        `$JS.API.CONSUMER.*` grants dropped, REFDATA's kept. Verified live
        against real `nsc` in a scratch environment (same rigor as 28f's
        script verification) — decoded the resulting JWT and confirmed the
        exact expected grant set, no RPCTRACE remnants.
      - **Docs:** `BUSINESS_RULES-SHIPPING.md` (BR-026 retired, BR-027
        tightened), `BUSINESS_RULES-REFDATA.md` (BR-D29/BR-D36 retired,
        BR-D37 tightened — BR-D29/BR-D36 had never recorded their Phase 28b
        supersession in the markdown at all, a pre-existing doc gap fixed
        retroactively alongside the Phase 28g retirement), `BUSINESS_RULES-
        ACCOUNTS.md` (BR-AC18 amended for both token functions), and
        `ARCHITECTURE-ADMIN.md`/`ARCHITECTURE-ACCOUNTS.md`/`ARCHITECTURE.md`
        (panel index, archetype table, credential mermaid diagram, and two
        historical connection-purpose tables corrected — several referenced
        the stale `traces-_platform` bucket name from before 28g's own
        bucket rename, caught and fixed in passing).
      Verified live against the running stack (all four affected containers
      rebuilt): `[messages]` renders real flattened spans with working
      status/family filters and the single-pane detail view; `[traces]`
      confirmed unaffected (159 live traces, unchanged); all API calls
      200 OK, no new console errors. Full backend re-sweep after every
      change: shipping-service 9/9, refdata-service 3/3, accounts-service
      3/3 suites green; frontend `vitest run` 11/11 files, 81/81 specs
      green (76 → 81: `RpcPanel.spec.js`'s 5 new specs, this component's
      first test coverage); `go vet`/`gofmt` clean on all three touched Go
      services.
      **Final verification pass — complete.** Surveyed existing coverage
      first (Explore agent) rather than assuming what was missing; found two
      real gaps and closed both:
      - **Cross-hop single-traceId assertion** (the checklist's "one
        assertion that matters most") was genuinely missing — no test drove
        shipping's `api.*` through to refdata's `rpc.*` and checked one
        shared `traceId`. Added
        `TestLookupSharesTraceIdAcrossShippingAndRefdataHops` in
        `shipping-service/internal/refdataconsumer/consumer_test.go`: a
        simulated refdata inbound responder (`natstrace.StartFromHeaders`,
        the same stand-in-for-the-other-service's-copy pattern the existing
        `TestLookupContinuesParentSpanAttachedViaContext` already uses)
        proves shipping's outbound span and refdata's inbound span agree on
        one `traceId` with `refdataSpan.ParentSpanID == shippingSpan.SpanID`.
      - **Redaction/truncation contract** was cloned into 4/5 services but
        silently absent from accounts-service's HTTP-middleware copy —
        traced to a real (if narrow) gap: `HTTPMiddleware` always calls
        `sp.End(nil, nil)`/`sp.Fail(err, nil, nil)`, never threading a
        request/response body through, so BR-036's redaction/truncation path
        was reachable only via `publishAccountEvent`'s outbound notify span,
        never the inbound HTTP span, and untested either way. Added an
        `It` in `accounts-service/internal/natstrace/natstrace_test.go`
        exercising `StartFromHeaders`+`End` directly (mirroring the other
        four services' `It`) to pin the shared `finish()` mechanics; did
        **not** change `HTTPMiddleware` to capture HTTP bodies — that's a
        separate buffering-cost/risk tradeoff, not this checklist's scope.
      - Invisible-timeout regression, redaction/truncation (4/5 services),
        and account-isolation (no `Sub.Allow` for `obs.trace.>` in a browser
        JWT) were already covered — confirmed by inspection, not re-written.
      - **No-regression check**, live: Services panel (`$SRV.STATS`) — 4
        services/64 endpoints, request/error counters intact; `[traces]` —
        196 live traces, waterfall detail renders; `[messages]` — flattened
        span rows render per the retirement design; zero console errors.
      - **Live cross-account (ACME→PLATFORM) e2e** — deferred earlier in
        this session because it needs `docker compose down -v` (destroys
        accumulated demo data) so `bootstrap-operator.sh`'s edited grants
        take effect from a truly fresh boot, not just the long-running dev
        stack. Confirmed with the user before running it. Ran
        `down -v` + `up --build`, drove a real ACME-tenant page load
        (shipping-frontend → its own `api.*` → refdata's `rpc.*`), and
        confirmed in the Admin UI (viewed as tenant `acme`) that the
        resulting trace correctly shows a `PLATFORM`-account span
        (`rpc.acme.refdata.type.list.v1`, spans:1, accounts:1).
        **Correction, found later the same session:** this check was
        insufficient — refdata-service always connects natively *as*
        PLATFORM (BR-D08), so its spans never actually cross an account
        boundary regardless of whether ACME's export/import is wired at
        all; the `PLATFORM` label proved nothing about the cross-account
        hop. The real test is a service that publishes `obs.trace.*` from
        a genuinely per-tenant connection — shipping/pricing's own
        `browserrpc`-instrumented `api.*` spans. Driving a real
        `api.acme-atlantic-fleet.shipping.ship.*` call and raw-subscribing
        `obs.trace.>` as PLATFORM (`nats sub`, bypassing the Admin UI/KV
        layer entirely to isolate the account boundary) showed **zero**
        shipping spans arriving, while the same raw subscribe as ACME
        showed them publishing correctly on ACME's own account. Root
        cause: decoding the live `nats/resolver/{ACME,PLATFORM}.jwt`
        showed ACME's account JWT had **no `obs.trace.>` export at all**,
        and PLATFORM had no matching import — `bootstrap-operator.sh`'s
        Phase 28f export/import lines exist in the script but were never
        applied, because the script is idempotent from a scratch `nsc`
        store ("exits early if `operator.jwt` exists, unless `--force`")
        and nobody had re-run it with `--force` since those lines were
        added; the checked-in JWTs simply predated them. Fixed by running
        `bootstrap-operator.sh --force` (confirmed via the same JWT-decode
        that `ACME.exports`/`PLATFORM.imports` now include `obs.trace.>`)
        followed by another `down -v` + `up --build` + `otlp-bridge`
        restart, then re-ran the identical raw-subscribe-as-PLATFORM test
        live: a real ship command's full span chain
        (`ship.register`/`ship.arrive`/`ship.registered`/`ship.arrived`,
        5 spans) now arrives and renders correctly in the Admin UI's
        `[traces]` panel from the ACME tenant view. `ginkgo ./...` in
        shipping-service re-run green after the fix (9/9 — this was a
        NATS-config-only change, no Go code touched).
      - **OTLP-bridge-oracle-vs-Jaeger** — re-confirmed post-rebuild: the
        bridge reconnected through the NATS restart with no re-deploy, and a
        live Jaeger UI search (service `refdata`) shows real spans.
      All test suites re-run green after the rebuild: shipping-service 9/9
      (10 specs added net across the two new tests), accounts-service
      84/84 + 19/19 + 7/7 (natstrace 6→7).
- [x] **Tests:** per sub-phase, one Ginkgo `Context` per business rule
      (Quality Rule 1) and `ginkgo ./...` green in every touched service
      (Quality Rule 2); `vitest run` for 28g. The contract test asserting the
      `traceSpan` JSON shape *and* that an old-shape `obsEnvelope` still
      decodes is cloned into all five services — that test is what makes the
      duplication safe. **The one assertion that matters most:** drive
      shipping's `api.*` → refdata's `rpc.*` and assert a single `traceId`
      across both `obs.*` families with a correct parent/child `spanId` chain.
      Plus the invisible-timeout regression (no responder → a span with
      `statusCode: error`, which today produces no record at all), truncation
      and redaction behaviour, and that a tenant browser JWT has **no**
      `Sub.Allow` for `obs.trace.>`.

#### Business rules (confirmed 2026-08-15, written into domain files)

No new `BR-` prefix: cross-cutting observability rules are paired across
existing domain files (precedent BR-D36/BR-026, BR-D37/BR-027), and Admin-UI
presentation rules go to `BUSINESS_RULES-SHIPPING.md` as bare `BR-0NN` even
when not about ships.

- **BR-036** (SHIPPING) — the `obs.trace.*` envelope is a strict superset of
  the `obs.rpc.*`/`obs.api.*` envelope, carrying W3C trace identity and
  OTLP-shaped span fields; it publishes to the PLATFORM account only, applies
  a redaction denylist before a 4 KiB payload cap, and flags truncation rather
  than cutting silently. Inherits BR-D26: never blocks or fails a business path.
- **BR-D39** (REFDATA) — the same wire contract on refdata-service's `natsrpc`
  publisher side, mirroring BR-036 as BR-D36 mirrors BR-026.
- **BR-037** (SHIPPING) — trace context propagates on every outbound NATS
  message (`rpc.*` request, `evt.*` publish, `notify.*` publish). One span per
  *logical* RPC call with a retry-count attribute, never one per attempt. A KV
  entry cannot carry trace context; the derived notify does.
- **BR-035** (SHIPPING) — presentation rule for the Request/Reply & Traces
  panel: one row per trace; the reply-ack boundary separates synchronous from
  eventual work; the account gutter marks boundary crossings; the header states
  both *reply* and *read-model-consistent* durations. Obeys §2.2's
  exceptional-state rule — a trace with no async tail renders no ack line, and
  a rejected command legitimately has no tail.
- **BR-AC30** (ACCOUNTS) — minted account JWTs carry `allow_trace: true` on
  service exports and stream imports, plus a per-tenant `obs.trace.>` stream
  export imported into PLATFORM. Without this, traces stop dead at the account
  boundary.

*To verify before writing these:* whether `BUSINESS_RULES-PRICING.md` and
`-TRADING-PARTNER.md` already carry obs-envelope rules; if so they need
parallel `BR-P25`/`BR-TP15` entries for symmetry. Also note
`BUSINESS_RULES.md`'s index ranges are already stale (says REFDATA
`BR-D01–D28`, actually D38; ACCOUNTS `AC01–AC13`, actually AC29) — worth
correcting in the same pass.

#### Docs (done ahead of implementation, 2026-08-14)

- [x] `ARCHITECTURE-ADMIN.md` — §1 panel row retitled, §2.1 layout 2 credited,
      §3.1 gains the KV-watch *variant* of snapshot+notify (explicitly **not** a
      fourth archetype — a KV watch replays then goes live on one subscription,
      so it structurally cannot have BR-D29's bootstrap duplicate/gap window),
      §4.5 rewritten with the UI design and a `Design history` table covering
      the viewer and placement arguments.
- [x] `ARCHITECTURE-COMMUNICATIONS.md` — §2.1 gains the `obs.trace.*`
      Supportive row (and the "Five families" count corrected to six), §2.2
      gains the grammar line, §6 gains a Phase 28 blockquote amendment
      embedding `images/otlp-bridge-ingest.png` (ingest topology, per-message
      handling inside the bridge, and the in-process-vs-consumer contrast), and
      the closing payload-sensitivity bullet is amended from advisory to
      enforced.
- [x] Three PNGs exported and embedded: `otlp-bridge-ingest.png` (§6 — ingest
      topology, per-message handling, in-process-vs-consumer contrast),
      `traces-span-envelope.png` (§6 — the HAVE/NEW envelope table and the
      instrumented-surfaces coverage table), and `admin-traces-panel.png`
      (ARCHITECTURE-ADMIN.md §4.5 — the reviewed panel mockup).
- [x] New diagram tooling: `diagrams/export-html-png.mjs` renders a
      hand-authored inline-SVG or mockup HTML page to PNG, alongside the
      existing `export-png.sh` for Draw.io workbook pages. Supports
      `--clip="<selectors>"` to capture one component out of a page that also
      carries prose — used to lift the panel chrome and the capture tables out
      of the mockup separately. These are the first PNGs in `images/`
      **without** a Draw.io source, so `export-png.sh` does not regenerate
      them; editable sources are `diagrams/otlp-bridge-ingest.html` and
      `diagrams/admin-traces-panel.html`, with re-export commands recorded
      beside each embed. Worth folding into the workbook if that divergence
      becomes annoying.
- [x] `obsidian/POC-Dictionaries/` — findings note on correlation-id vs trace-id
      and why the trace store is Shape A. [[4. Findings - Distributed Tracing (Phase 28)]]
      (2026-08-17).

---

### Phase 30 (IMPLEMENTED, 2026-08-16) — observability-service: Extract Cross-Account Diagnostics from shipping-service

#### Goal

The NATS/SYSTEM diagnostic endpoints (`/api/nats/connections`,
`/api/nats/account-activity`, `/api/nats/log`, `/api/jetstream/streams`,
`/api/jetstream/replay`, `/api/kv/buckets*`) and the `TRACES` stream
consumer (`RegisterTraceStore`, projecting into the `trace-request-reply` KV
bucket) all live in `shipping-service` today for one reason: it is
currently the only service holding live NATS connections into PLATFORM
*and* every tenant account (Main-POC-Plan.md:308's Phase 21 note), so it
was the only place that could answer them. None of this is shipping domain
logic. Extract it into a new PLATFORM-account service,
**`observability-service`**, matching the `obs.*` subject family CLAUDE.md
already reserves for this purpose, and the natural landing spot for the
system/performance telemetry (memory, CPU, JetStream/consumer lag) this
phase's discussion flagged as the next thing wanted here.

#### Decisions locked (with user, 2026-08-16)

- New service on the PLATFORM account, port **7205** (next free in the
  7200–7299 backend range per CLAUDE.md's port allocation table).
- Absorbs, lifted verbatim except where noted: `dictionary/internal/rest/
  {nats_ops,nats_log,streams,kv,replay}.go`'s six handlers, and
  `dictionary/internal/eventhandler/trace_store.go`'s `RegisterTraceStore` +
  `trace-request-reply` KV projection.
- `otlp-bridge` stays a separate deployable — already correctly scoped as a
  translator with no REST API of its own; not folded in.
- **Chose the export/import extension over duplicating shipping-service's
  raw per-tenant connection fan-out** (`TenantResources`/
  `ensureTenantResources`): a second service holding a live credential into
  every tenant account would be a second instance of exactly the pattern
  Phase 21's "target partitioning" note already flagged for retirement, with
  a strictly broader permission surface than an explicit, enumerable
  account-JWT export. `observability-service` ends up holding exactly one
  NATS connection — PLATFORM-scoped, restricted like `shipping-admin` — no
  `.creds` file of its own into any tenant account.

#### Design

- **Two new per-tenant export/import pairs**, wired the identical way
  BR-AC30/`addPlatformTraceImport` already wires `obs.trace.>` — this is
  generalizing a proven mechanism, not inventing one:
  1. **`$SRV` discovery** — each tenant account exports `$SRV.PING`/
     `$SRV.INFO`/`$SRV.STATS` as a service export; PLATFORM imports it per
     tenant with a local remap (e.g. local `monitor.{tenant}.srv.>`),
     letting `observability-service` broadcast `$SRV.STATS` and receive
     replies across the account boundary the same way the `rpc.*` service
     imports already do (ARCHITECTURE-ACCOUNTS.md:83's "`$SRV.>` is not
     exported to tenants" is the opposite direction — PLATFORM's own $SRV
     kept private from tenants — and does not block this reverse export).
  2. **`$JS.API` subset for JetStream/KV introspection** — each tenant
     exports a narrow, explicit subject list, deliberately **not**
     `$JS.API.>` (ARCHITECTURE-ACCOUNTS.md:87–101's caveat: the full
     namespace grants stream *management* — create, delete, purge — not
     just visibility). **This is not a pure read-only grant** — traced
     against the exact call chain in `dictionary/internal/rest/{kv,replay}.go`
     and the `$JS.API` subject constants in the pinned `nats.go@v1.52.0`
     (`go.mod`), `/api/jetstream/replay` and the KV-entries watch both
     create (and in replay's case, explicitly delete) real ephemeral
     JetStream consumers — there is no way to serve them from list/info
     subjects alone:
     - `STREAM.LIST` — bucket + stream listing (`listKVBuckets`,
       `listStreams`; stream consumer *count* rides inside this response's
       `State.Consumers`, so `CONSUMER.LIST`/`NAMES` are never called and
       are excluded)
     - `STREAM.INFO.*` — resolves a named bucket's backing stream
       (`kvBucketEntriesOnce`'s `js.KeyValue()` call)
     - `CONSUMER.CREATE.*` — the legacy nameless-ephemeral form the KV-watch
       path uses (`kv.go`'s `WatchAll` → core-nats `nats.OrderedConsumer()`
       push subscribe); server assigns the name, so this 5-token subject
       can never collide with a caller-chosen durable name
     - `CONSUMER.CREATE.*.*` — the named-ephemeral form
       `jetstreamReplayOnce`'s `js.OrderedConsumer()` uses; the name is
       always a nuid `generateConsName()` value (confirmed: `replay.go`
       sets `FilterSubjects` plural, which `consumer.go:319`'s branch
       condition routes away from the 7-token filter-in-subject form, so
       that variant is excluded as unused)
     - `CONSUMER.MSG.NEXT.*.*` — pulling messages off the replay consumer
     - `CONSUMER.DELETE.*.*` — cleanup for both paths (`replay.go:85`'s
       explicit delete; the KV-watch path's `Stop()`/`Unsubscribe()` — not
       yet empirically confirmed to round-trip a delete rather than rely on
       `InactiveThreshold` reaping, worth pinning with a live trace when 30b
       lands)

     `STREAM.MSG.GET.*` is excluded — no current handler calls it (can be
     added later if a "jump to sequence" feature needs a direct single-
     message fetch).

     **`CONSUMER.DELETE.*.*` is the one export that isn't fully closed by
     subject scoping.** NATS wildcards match any single token regardless of
     content, so this subject permits deleting *any* consumer on *any*
     stream in the tenant account — including a business-critical durable
     one — not just an ephemeral one this connection created. That has to be
     an application-layer invariant, not a JWT-scope one:
     `observability-service` must only ever call `DeleteConsumer` with a
     name it just received from its own preceding `CREATE` response in the
     same request (exactly what `replay.go:85` already does today via
     `consumer.CachedInfo().Name`, never a caller-supplied value) — 30b adds
     a test enforcing that discipline, not just a happy-path spec.
- Both declared in the same two places `obs.trace.>` already is: the day-0
  `nsc` equivalent in `nats/bootstrap-operator.sh`, and
  `accounts/provisioner.go`'s `CreateAccount` — tenant-side via
  `tenantExports()`, PLATFORM-side via a new `addPlatformMonitorImport`-
  style helper mirroring `addPlatformTraceImport` (re-signs PLATFORM's
  claims through `pushClaimsUpdate`/`$SYS.REQ.CLAIMS.UPDATE`, idempotent per
  `(account, subject)` pair).
- Admin UI: `admin/src/api.js` client functions + dev-mode Vite proxy
  entries for the six diagnostic endpoints repoint from shipping-service's
  port to `observability-service`'s (7205). The NATS/SYSTEM nav panels
  themselves (components, props) are unchanged — only where they fetch from
  moves.
- Migration is additive-then-subtractive within the phase: stand up
  `observability-service` answering the same endpoints, flip the Admin UI's
  proxy, verify parity live, then delete the moved code from
  `shipping-service` — not left dual-registered past this phase.

#### Sub-phases (each independently landable)

- [x] **30a — accounts-service: `$SRV` discovery export/import.** (DONE
      2026-08-16) Not quite BR-AC30's mechanism exactly — discovered
      mid-implementation that `$SRV` needs a **Service** export/import
      (`ResponseType: jwt.ResponseTypeStream`, not the library default
      `Singleton` — `$SRV.STATS` is answered by every registered instance,
      not one), unlike `obs.trace.>`'s **Stream** export, and no `$SRV`
      subject had ever crossed an account boundary in this codebase before
      this rule (confirmed via `go doc`/source on the pinned
      `nats.go@v1.52.0`/`jwt/v2@v2.8.2`, and via `nsc add export --help` for
      the `--response-type` flag). Live cross-account reply-fanout routing
      is accordingly *not* proven by this sub-phase's tests — deferred to
      Phase 30i's live verification, per the user's explicit choice not to
      spike-test it first. Landed: BR-AC31 (`BUSINESS_RULES-ACCOUNTS.md`,
      `BUSINESS_RULES.md` index bump); `tenantExports()` +
      `addPlatformMonitorImport` + `CreateAccount` wiring
      (`accounts/provisioner.go`); day-0 `$SRV.>` export/import block
      (`nats/bootstrap-operator.sh`); `TestNewAccountClaimsAddsTenantMonitorExport`
      + the PLATFORM-side idempotency spec (`provisioner_claims_test.go`/
      `provisioner_test.go`) — the latter also fixed to filter by subject
      instead of asserting PLATFORM's raw `Imports` length, which this
      phase's own second import broke; browser-exclusion covered by
      pre-existing `auth/token_test.go` `ConsistOf` assertions, no new test
      needed since `auth/token.go` is untouched. `ginkgo -r ./...` green,
      85/85 specs (accounts-service).
- [x] **30b — accounts-service: `$JS.API` introspection export/import.**
      (DONE 2026-08-16) The six-subject list from Design, wired via the same
      mechanism as 30a, with per-subject `ResponseType` (five `Singleton`,
      `CONSUMER.MSG.NEXT.*.*` alone `Stream` — a batch pull yields multiple
      replies per request, the same not-yet-proven-live mechanism as
      BR-AC31's `$SRV.>`). Landed: BR-AC32 (`BUSINESS_RULES-ACCOUNTS.md`,
      `BUSINESS_RULES.md` index bump); `jsAPIExportSubjects` +
      `addPlatformJSAPIImport` + `CreateAccount` wiring
      (`accounts/provisioner.go`); day-0 six-subject export/import block
      (`nats/bootstrap-operator.sh`); `TestNewAccountClaimsAddsTenantJSAPIExports`
      (positive six-subject + `ResponseType` check + negative "no other
      `$JS.API` subject leaked in" check) and the PLATFORM-side idempotency
      spec, both in `provisioner_claims_test.go`/`provisioner_test.go`. The
      `DeleteConsumer`-name-provenance code-level test/lint is **not** part
      of this sub-phase — `DeleteConsumer` doesn't exist yet in
      `observability-service` until 30e lifts the JetStream/KV handlers;
      carried forward as a 30e checklist item instead. `ginkgo -r ./...`
      green, 86/86 specs (accounts-service).
- [x] **30c — scaffold `observability-service`.** (DONE 2026-08-16) Deviated
      from the original `refdata-service`-mirroring description in one
      respect: `observability/internal/{natsops,tracestore}` were **not**
      pre-created as empty packages — CLAUDE.md's own guidance against
      premature scaffolding — and will be created by 30f/30g when they have
      real content. What landed: `go.mod`/`cmd/main.go` (NATS connect only,
      no Postgres — mirrors `refdata-service`'s `cmd/main.go` minus its DB
      wiring), `observability/composition.go` +
      `observability/internal/rest` with a `GET /healthz` only (every real
      endpoint is 30d–30g), `Dockerfile` (mirrors `otlp-bridge`'s), Docker
      Compose entry on port 7205. Also minted a dedicated restricted
      PLATFORM credential (`nsc add user --account PLATFORM observability`,
      `nats/bootstrap-operator.sh`) rather than reusing the unrestricted
      `platform.creds` `refdata-service`/`otlp-bridge` use — narrow
      `allow-pub`/`allow-sub` of `monitor.>,$SRV.>` (+ `_INBOX.>` sub) is
      everything BR-AC31/BR-AC32's imports resolve to; PLATFORM-native
      `$JS.API` access is deliberately not yet granted, added when 30e/30f
      need it. `go build ./...`/`go vet ./...` clean; `docker compose build
      observability-service` and `docker compose config -q` both clean.
- [x] **30d — lift the pure HTTP-proxy panels.** (DONE 2026-08-16)
      `/api/nats/connections`, `/api/nats/account-activity`,
      `/api/nats/log` — no dependency on 30a/30b confirmed true, but a real
      dependency surfaced mid-implementation that the original sub-phase
      description didn't anticipate: both panels' `tenantLabel` resolution
      (`tenantLabelsByAccount` in the original) worked by matching `/connz`
      rows against the `LocalAddr` of connections **shipping-service itself
      held** — one per tenant, via `TenantResources`. `observability-service`
      holds exactly one connection (PLATFORM), so there is nothing to
      match against; labeling would have silently gone dark. Fixed by
      adding a new `AccountsClient` (`observability/internal/rest/
      accounts_client.go`) that calls accounts-service's existing
      `GET /api/accounts` (already serving the Admin UI's Accounts panel,
      already carrying `{name, publicKey}`) for a direct pubkey→name
      lookup — mirrors accounts-service's own `RefdataClient` pattern
      (`accounts/refdata.go`), needs no new NATS grant, and is simpler than
      the two-stage trick it replaces (no more "which connections are ours"
      matching). New env vars `ACCOUNTS_URL`/`ACCOUNTS_AUTH_SECRET`
      (`docker-compose.yml`, `cmd/main.go`, `observability/composition.go`'s
      new `Config`); `NATS_MONITOR_URL`/`NATS_LOG_PATH` also wired
      (`nats-logs` volume mounted read-only, mirroring shipping-service's
      own mount). Handlers lifted into
      `observability/internal/rest/{nats_connections,nats_log}.go`,
      `Services` (the third original panel in `nats_ops.go`) deliberately
      left for 30f. Tests ported/adapted from `nats_ops_test.go`/
      `nats_log_test.go`, the label specs rewritten against a mocked
      accounts-service instead of real embedded-NATS `LocalAddr`s; `go
      test ./...` 20/20 green, `go vet ./...` clean, `docker compose build
      observability-service` and `docker compose config -q` both clean.
- [x] **30e — lift JetStream/KV introspection.** (DONE 2026-08-16)
      `/api/jetstream/streams`, `/api/jetstream/replay`, `/api/kv/buckets*`
      — the "code-level `DeleteConsumer` lint" carried forward from 30b is
      satisfied by construction, not a separate lint pass: `replay.go`'s
      only `DeleteConsumer` call site uses `consumer.CachedInfo().Name` (the
      server-assigned name from this handler's own preceding `CREATE`),
      never a request-derived value, and a new test
      (`TestJetstreamReplayOnceReturnsAllRetainedMessages`) asserts the
      consumer is actually gone afterward via `ConsumerNames`.

      A mechanism discovery changed the implementation shape from what the
      sub-phase description assumed: rather than hand-rolling `$JS.API`
      requests against `monitor.{tenant}.js.*`, the pinned `nats.go`'s
      `jetstream.NewWithAPIPrefix` transparently honors a custom API prefix
      for every operation these handlers need — confirmed by reading
      `apiSubject()`'s prefix-concatenation and `legacyJetStream()`'s
      prefix propagation to the KV-watch path in the actual `nats.go`
      source, not assumed. So `introspectableAccounts`/`jsForAccount`
      (`kv.go`) construct one `jetstream.JetStream` per account —
      `jetstream.New(nc)` for `"platform"`, `jetstream.NewWithAPIPrefix(nc,
      "monitor."+name+".js")` per tenant, tenant names from
      `AccountsClient.TenantNames` (new method, same accounts-service
      `GET /api/accounts` call 30d already added) — replacing
      shipping-service's `TenantResources` map iteration entirely, and the
      REST handlers themselves (`streams.go`/`kv.go`/`replay.go`) are
      otherwise a close port.

      Two more deliberate deviations, both documented in `replay.go`'s
      package doc comment: (1) no ship/container-specific subject filter —
      domain knowledge that doesn't belong in a domain-agnostic tool, and
      `domain.StreamSubjects()` isn't reachable from a separate `go.mod`
      anyway; every stream now replays unfiltered (cosmetic effect only).
      (2) `account`/`stream` are both required query params for replay, not
      defaulted — the original defaulted to shipping-service's own
      "currently active tenant" session concept, which doesn't exist here.

      `nats/bootstrap-operator.sh`'s `observability` user gained the same
      six-subject `$JS.API` grant BR-AC32 already defines, applied directly
      to its own PLATFORM account (not through any `monitor.*` remap) —
      the deferred item from 30c's scaffold, closed here rather than
      speculatively ahead of this content. Deliberately not
      shipping-service's `PlatformFullJS` pattern (a second, broader-access
      connection) — one connection, narrowly scoped, per this phase's
      design throughout.

      Tests use a real embedded single-account JetStream-enabled NATS
      server (`testutil_test.go`), which fully exercises the `"platform"`
      path (identical mechanism in test and production) and the
      client-side half of the tenant path (`jsForAccount` correctly
      resolves known vs. unknown tenant names), but not the server-side
      half of BR-AC32's actual cross-account import resolution — that,
      like `CONSUMER.MSG.NEXT`'s multi-reply mechanism risk (BR-AC32's own
      design note), is exercised at Phase 30i's live `docker compose`
      verification, not by this sub-phase's own coverage; documented
      explicitly in `testutil_test.go` rather than left implicit.
      `go test ./...` 33/33 green, `go vet ./...` clean, `docker compose
      build observability-service` and `docker compose config -q` both
      clean.
- [x] **30f — lift service discovery.** (DONE 2026-08-16) `/api/nats/services`
      — the shape of this one changed more than 30d/30e's proxies did. The
      original queried two live *connections* it happened to hold
      (`deps.NC` for PLATFORM, `deps.TenantNC` for whichever tenant session
      was active), explicitly documented as blind to every other tenant.
      This service holds one connection but gets a distinct discovery
      *subject* per account instead — PLATFORM's bare `$SRV.STATS` (via
      `micro.ControlSubject`, confirmed to literally equal `"$SRV.STATS"`
      by reading `micro.APIPrefix`'s value) plus, for every tenant
      `AccountsClient.TenantNames` reports, BR-AC31's remapped
      `monitor.{tenant}.srv.STATS` — so the fan-out moved from "N
      connections, one subject" to "one connection, N+1 subjects," same
      concurrency rationale (`collectStats` always blocks for the full
      `srvDiscoveryWindow`, so N+1 subjects queried sequentially would cost
      N+1 windows). **Net capability gain, not just a port**: this panel
      now sees every known tenant's services at once, where the original
      only ever saw one active tenant. Landed in
      `observability/internal/rest/nats_services.go`; `discoverySubjects`
      and `collectStats` (now subject-parameterized rather than deriving
      the subject internally) are the two functions that actually changed,
      everything downstream of the reply collection (dedup, endpoint
      reshaping, sorting) is an unchanged port.

      Tests: the real-registration specs (stats/endpoint counters, metadata
      pass-through, empty-when-nothing) are lifted verbatim against the
      platform path. The dedup/concurrency spec is rewritten to prove the
      new subject-fan-out mechanism specifically — a hand-rolled responder
      on `monitor.faux.srv.STATS` simulates what a real tenant reply would
      look like, so the test proves this file's own fan-out/dedup logic
      without claiming to prove BR-AC31's actual cross-account remap
      resolves on the wire (that's still Phase 30i, per BR-AC31's design
      note — documented again in this file's own package doc comment
      rather than left implicit). `go test ./...` 40/40 green, `go vet
      ./...` clean, `docker compose build observability-service` and
      `docker compose config -q` both clean.
- [x] **30g — lift the trace store.** (DONE 2026-08-16) `RegisterTraceStore`
      + `trace-request-reply` KV projection — the projection logic itself
      really is unchanged, but two things around it weren't as simple as
      "moved":

      **No `internal/kvstore.Store` port.** Shipping-service's version
      wrapped its own general-purpose KV package (Keys/Watch/Delete/multi-
      context, plus a `natstrace`-header-on-notify path). This projector
      only ever calls two of that package's methods (`Get`/`Put`), and the
      natstrace branch was always a no-op here (the consume callback never
      attaches a span to its context) — so
      `observability/internal/tracestore/tracestore.go` inlines just the
      Get/Put-with-notify slice actually used, rather than porting an
      abstraction most of which would sit unused. New package (30c's
      original scaffold deliberately left `tracestore` uncreated until it
      had real content — this is that content landing).

      **A real permissions tension, resolved without breaking the
      one-connection design.** Provisioning is a `$JS.API` *write*
      (`STREAM.CREATE`/`UPDATE` for `TRACES` and `KV_trace-request-reply`)
      — shipping-service's original solved this by using a second,
      unrestricted connection (`PlatformFullJS`) specifically because its
      narrow `shipping-admin` connection was denied it. Kept this service's
      one-connection design instead: granted `$JS.API.STREAM.CREATE`/
      `UPDATE` scoped to exactly those two resource names (never a
      wildcard), plus publish on `$KV.trace-request-reply.>` (what
      `kv.Put` actually publishes to — confirmed via `nats.go`'s
      `kvSubjectsTmpl` source, not assumed) and
      `notify._platform.kv.trace-request-reply.>`. This is a different risk
      shape than BR-AC31/BR-AC32's grants (which are read-only access into
      *other* accounts) — it's create/update access to two specifically-
      named resources in this account, the same resource-scoped-by-name
      pattern `shipping-admin`'s own REFDATA grants already establish.
      Consumer create/pull for the durable `trace-store-projector` needed
      no new grant — the existing `CONSUMER.CREATE.*.*`/
      `CONSUMER.MSG.NEXT.*.*` wildcards already cover any consumer name,
      durable or ephemeral (NATS wildcards don't distinguish the two).
      `nats/bootstrap-operator.sh`'s `observability` user updated
      accordingly.

      Wired into `observability/composition.go`'s `Startup` (now
      `ctx`-aware, builds its own `jetstream.JetStream`, holds the returned
      `jetstream.ConsumeContext` for a new `Handlers.Stop()` `cmd/main.go`
      defers). Tests are new (no direct prior-art file — shipping-service
      covered this indirectly through its `eventhandler` suite): multi-span
      merge, redelivery dedup, the notify publish, malformed-span drop, and
      `Register` idempotency across two calls, all against a real embedded
      JetStream-enabled NATS server. `go test ./...` green across both
      packages, `go vet ./...` clean, `docker compose build
      observability-service` and `docker compose config -q` both clean.

      Left as an accepted interim state until 30h: shipping-service's own
      `RegisterTraceStore` is still active too (uses `platform.creds`,
      unaffected by this phase's grants), so both services independently
      ensure the same `TRACES`/`KV_trace-request-reply` resources exist at
      startup — harmless since `CreateOrUpdate*` is idempotent, but not a
      final state; 30h removes shipping-service's copy.
- [x] **30h — Admin UI cutover + cleanup.** (DONE 2026-08-16) `api.js` needed
      no change — it already used relative `fetch()` paths, routed entirely
      by proxy config. Repointed the proxy layer instead: `admin/vite.config.js`
      gained three more-specific rules (`/api/nats`, `/api/kv`,
      `/api/jetstream` → `localhost:7205`) ahead of the general `/api` rule
      (unchanged, still shipping-service); `admin/nginx.conf` mirrors this
      with `location /api/nats/` / `/api/kv/` / `/api/jetstream/` blocks
      pointed at `observability-service:8080`, the NATS one carrying the
      same SSE buffering/timeout settings as the general block since
      `/api/nats/log` is still a long-lived stream.

      Deleted from `shipping-service`: the four lifted REST files
      (`nats_ops.go`, `nats_log.go`, `kv.go`, `replay.go`) and their six
      test files, plus `dictionary/internal/eventhandler/trace_store.go`
      and its two test files (13 files total). `listStreams` (never its own
      file — it lived in `handlers.go`) removed alongside its
      `introspectableAccounts` dependency (which was `kv.go`'s, not
      separately named in the original plan text).

      **Deviation — this also removed the entire `PlatformFullJS` second
      connection from shipping-service, not just its callers.** Once the
      diagnostic/trace-store code was gone, nothing else in the process
      used `mono.PlatformFullJS()`/`NatsMonitorURL()`/`NatsLogPath()`
      (confirmed via `grep` before touching anything) — so rather than
      leave a dead, unused connection and three dead `Monolith` interface
      methods, removed all of it: the `platform.creds`-backed second
      connection and its fallback/error-handling in `cmd/main.go`, the
      three methods from `internal/monolith/monolith.go`'s interface (and
      their long doc comments — `PlatformFullJS`'s explained a boundary
      that no longer exists in this service), and the corresponding `Deps`
      fields/wiring in `handlers.go`/`composition.go`. `docker-compose.yml`'s
      `shipping-service` block lost `NATS_MONITOR_URL`/`NATS_LOG_PATH` (now
      dead) and the `nats-logs` volume mount (still used by
      `observability-service`'s own block, untouched). `shipping-admin`'s
      restricted connection (`NC()`/`JS()`) is unaffected — still used for
      `$SRV` discovery *of shipping-service's own instances* via
      `browserrpc.Adapter`'s `micro.AddService`, tenant switching, and
      admin ordered-consumer replay of `SHIPPING`.

      One test-helper casualty: `discardLogger`/`discardWriter` had lived
      in the now-deleted `nats_ops_test.go` but were also used by the
      unrelated `trace_middleware_test.go` (BR-037's HTTP tracing specs,
      Phase 28m) — moved into the package's existing `testutil_test.go`
      rather than left orphaned or duplicated.

      Regenerated `docs/docs.go`/`swagger.json`/`swagger.yaml` via
      `swag init -g cmd/main.go -o docs` (swag was already installed) —
      1769 lines net deleted across the three generated files, confirmed
      none of the removed routes remain. `go build ./...`, `go vet ./...`,
      and `ginkgo ./...` (9 suites) all green in `shipping-service`;
      `ginkgo ./...` (3 suites, 117 specs) green in `accounts-service`;
      `go build`/`go vet`/`go test ./...` (2 packages) green in
      `observability-service` — the full three-service combined pass the
      plan's closing checklist calls for. `docker compose config -q` and
      `docker compose build shipping-service` both clean.

      Updated `BUSINESS_RULES-SHIPPING.md`: BR-028's "Enforced in"/"Test"
      lines repointed to `observability-service`, plus a Phase 30h
      amendment paragraph — the label-resolution *mechanism* changed, not
      just its file (`tenantLabelsByAccount`'s `LocalAddr`-matching, which
      only worked because shipping-service held one connection per tenant,
      replaced by `AccountsClient.Labels`' direct `GET /api/accounts` call,
      Phase 30d, since `observability-service` holds only the one PLATFORM
      connection). BR-034 (Account Activity) got the same file/mechanism
      update. The trace-store rule (Phase 28f/28g/28l's amendments) got its
      own Phase 30h amendment paragraph plus new "Enforced in"/"Test"
      lines, covering the `PlatformFullJS` → narrowly-scoped-grant
      permissions change and the deliberate non-port of `internal/kvstore.Store`.
      `BUSINESS_RULES.md`'s index line for BR-034 repointed off the deleted
      `nats_ops.go` path. No new BR numbers were needed — every 30h doc
      change is an amendment to an existing rule, not a new one.

      Deliberately deferred to a spawned follow-up task, not done as part
      of 30h: `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-ADMIN.md`
      (630 lines) still describes the pre-30h shipping-service-only backend
      for this exact Admin UI navbar group in detail (§ 3.1–3.4's shared
      backend patterns, § 4.1–4.7's per-panel sections, § 4.5's trace
      example) — a genuine content correction this phase's scope didn't
      originally call out, and large enough to warrant its own pass rather
      than folding into an already-large 30h diff.
- [x] **30i — Live verification.** (DONE 2026-08-16) `docker compose down -v
      && up --build`, repeated several times across the fix cycle below.
      Every panel in the NATS/SYSTEM nav group (Connections, Services,
      Account Activity, Streams, KV Buckets, Log, Request/Reply Traces)
      confirmed rendering correctly against `observability-service`
      (screenshots taken of each); a tenant (`initech`) created through the
      Admin UI got working Connections/Services visibility immediately, with
      zero restart of any service — BR-030's reactive-provisioning bar,
      confirmed live for the first time against the full Phase 30 split.
      This step existed specifically to prove or disprove the mechanism
      risks BR-AC31/BR-AC32 had flagged as unproven-by-unit-tests since
      30a/30b — it found six real, previously-undetected bugs, none of them
      the flagged multi-reply mechanism itself (which turned out to work
      correctly once everything else was fixed):

      1. **`observability.creds` was never actually generated.** The day-0
         `bootstrap-operator.sh` gained its `observability` user step in
         30c, but this machine's `nats/operator.jwt` already existed from
         before that, and the script exits early ("already exists — skip")
         unless `--force` is passed — so the step had literally never run.
         Docker's bind mount then silently created an empty *directory* at
         the missing host path on first `docker compose up`, and
         `observability-service` looped forever inside `waitForNATS` with
         zero log output (that inner loop logs nothing on failure) trying
         to open it as a file. Fixed by running `--force` — which, being a
         full operator/account/JWT regen, was also the only way for the
         day-0 ACME/GLOBEX tenants to actually pick up every export/import
         this phase had added to `bootstrap-operator.sh` since (BR-AC31,
         BR-AC32, the trace-store grants) — those were equally unproven
         until this same regen.
      2. **`$JS.API.INFO` missing from the `observability` user's grant.**
         `jetstream.CreateOrUpdateKeyValue` (trace-store bucket
         provisioning) calls `AccountInfo()` first; without this the very
         first startup call failed closed.
      3. **Filtered `CONSUMER.CREATE` needs a different, wider subject than
         the unfiltered form.** nats.go embeds a set `FilterSubject`
         directly into the *published* `$JS.API` subject
         (`apiConsumerCreateWithFilterSubjectT`) rather than the request
         body — both the trace-store's durable consumer (filtered to
         `obs.trace.>`) and, more consequentially, the KV Buckets panel's
         `WatchAll` (filtered to `$KV.<bucket>.>`, for *any* bucket in *any*
         account) hit this. Fixed with a new `$JS.API.CONSUMER.CREATE.*.*.>`
         subject — added as BR-AC32's seventh subject (not
         PLATFORM-native-only, since the KV panel needs it for tenant
         buckets too), superseding what was initially a narrower
         TRACES-only fix.
      4. **`$JS.API.DIRECT.GET` missing for the trace store's own KV
         reads.** `appendSpan`'s `kv.Get` uses nats.go's direct-get
         optimization (`apiDirectMsgGetLastBySubjectT`), a different
         subject family than `DIRECT.GET`'s no-filter form, same
         literal-subject-folding shape as #3. PLATFORM-native, scoped to
         `KV_trace-request-reply` only (this service's own resource, not a
         BR-AC32 cross-account concern).
      5. **`$JS.ACK` missing entirely.** Every delivered JetStream message's
         Ack publishes to a *server-generated* reply subject
         (`$JS.ACK.TRACES.trace-store-projector.<numDelivered>...`), not
         one the client constructs — without this grant, Acks silently
         failed and JetStream redelivered the same messages forever.
         PLATFORM-native, scoped to `TRACES`/`trace-store-projector`.
      6. **`AccountsClient.TenantNames` excluded `"PLATFORM"` and assumed
         `"SYS"` needed no exclusion — both wrong against the real,
         running `accounts-service`.** It stores and returns these with
         lowercase names (`"platform"`, `"sys"` —
         `accounts/handler.go`'s `h.Store.Get(ctx, "platform")`), and SYS
         *does* get a Postgres row despite the original comment assuming
         otherwise. The case-sensitive `==` check let both slip through
         into `introspectableAccounts`, which then built a bogus
         `monitor.platform.js`/`monitor.sys.js`-prefixed JetStream context
         for each — no matching cross-account import exists for either, so
         the very next `$JS.API` call through it failed closed with
         "no responders," aborting `listKVBuckets`/`listStreams`'s entire
         response (discarding every real tenant's already-successful
         results) the moment either bogus entry was reached. This is the
         bug a long, initially-wrong investigation chased — a first false
         lead blamed bug #5's redelivery storm for client-side
         slow-consumer pressure on unrelated `$JS.API` calls; isolating the
         actual trigger required a from-scratch Go repro that couldn't
         reproduce it at all until it exactly mirrored `introspectableAccounts`'
         real call order, at which point per-account debug logging pinned
         the two bogus "platform"/"sys" entries directly. Fixed by matching
         `accounts-service`'s own `reservedAccountNames`
         `strings.ToUpper()` comparison exactly, with a new
         `TestTenantNamesExcludesPlatformAndSysCaseInsensitively` pinning
         the real (lowercase) shape.
      7. **`shipping-service`'s tenant-discovery treated
         `observability.creds` as a switchable tenant.** `nonTenantCredsFiles`
         (`dictionary/internal/rest/tenant.go`) was never updated when
         `observability.creds` first landed in the same shared `nats/creds/`
         directory back in 30c — every `shipping-service` startup from that
         point tried (and failed) to provision `SHIPPING`-stream resources
         for a nonexistent "observability" tenant. Fixed by adding it to
         the exclusion set; `TestDiscoverTenantsExcludesReservedNamesCaseInsensitively`
         extended to cover `shipping-admin`/`observability` alongside the
         existing platform/sys cases.

      All fixes verified together in one final clean
      `docker compose down -v && up --build`: zero permission-violation log
      lines anywhere in the stack, all five `observability-service` REST
      endpoints 200, every Admin UI NATS/SYSTEM panel screenshotted
      working (Connections showing resolved account labels including a
      freshly-created tenant; Services showing live `$SRV.>` fan-out counts
      increase for it; KV Buckets' live-entries view rendering real
      contents; Streams showing cross-account `SHIPPING`/`REFDATA`/`TRACES`;
      Request/Reply showing 25 live traces). `go build`/`go vet`/`ginkgo`
      (or `go test`) green across all three touched services (shipping-service
      9 suites, accounts-service 3 suites/86+19+12 specs,
      observability-service 2 packages) as a final combined pass, not just
      per-phase. `docker compose config -q` clean.

      `BUSINESS_RULES-ACCOUNTS.md`'s BR-AC32 and `BUSINESS_RULES-SHIPPING.md`'s
      trace-store rule (Phase 30h's own amendment) both updated with Phase
      30i amendments documenting the new seventh subject and its rationale;
      `nats/bootstrap-operator.sh` carries matching inline comments at each
      fix point, including one explicit correction of its own earlier
      (wrong) diagnosis for bug #6.
- [x] `BUSINESS_RULES-ACCOUNTS.md` gets the new BR-AC rule(s) for the two
      export/import grants (BR-AC31/BR-AC32, done incrementally in 30a/30b);
      `BUSINESS_RULES.md` index bump (done same phases).
- [x] `ginkgo ./...` green in `accounts-service` (3 suites) and the new
      `observability-service` (`go test ./...`, 2 packages — no Ginkgo suite
      in this service, matching how shipping-service's own `rest` package
      tests); `ginkgo ./...` green in `shipping-service` (9 suites) after the
      30h deletions — all three re-verified together as part of 30h's
      closing pass (2026-08-16).

#### Addendum — follow-on fixes found after 30i closed (2026-08-16/17)

None of these change Phase 30's scope; each is a real bug surfaced by using
the split stack live after 30i's own live-verification pass, fixed in the
same session rather than deferred.

- [x] **Streams panel account-status dot.** `JetStreamPanel.vue`'s
      account-group dot reflected "is this the browser's currently connected
      NATS tenant," not the account's actual `active`/`suspended` lifecycle
      status — irrelevant on a panel showing every account's streams at
      once. Reworked to read `accounts-service`'s real status
      (`AccountsClient.TenantStatuses`, `observability-service`); surfaced
      two real preconditions along the way: `introspectableAccounts` aborted
      the *entire* response on one unreachable/suspended account (now logs
      and skips), and a suspended account then vanished from the rail
      entirely rather than showing red (fixed by adding an authoritative
      `accounts` field to the API response, independent of per-account
      introspection success). `BUSINESS_RULES-ACCOUNTS.md`'s BR-AC03
      amendment.
- [x] **Same account-status-dot fix applied to `KvInspector.vue`** (KV
      Buckets panel), which had an even less accurate hardcoded
      `account === 'platform'` dot. Same `accounts` field / rail-rebuild
      pattern reused.
- [x] **`trading-partner-service` `Subscription Violation`s on
      `notify.accounts.account.*`/`api.*.trading-partner.*`.** Root-caused
      (by matching account/user pubkeys between `observability-service`'s
      own log lines and `trading-partner-service`'s) to a phantom tenant
      connection, not a missing grant: `nonTenantCredsFiles`
      (`tradingpartner/internal/tenants/tenants.go`) never had
      `observability.creds` added when that file first landed in the shared
      creds directory back in 30c, so `Discover` treated it as a switchable
      tenant and opened a connection with mismatched permissions. Fixed;
      `TestDiscoverExcludesReservedNamesCaseInsensitively` added.
- [x] **Identical latent bug fixed proactively in `pricing-service`**
      (`pricing/internal/tenants/tenants.go`) — same missing
      `nonTenantCredsFiles` entry, never yet triggered live but certain to
      hit the same failure once exercised. Same test mirrored.
- [x] **`$JS.FC.KV_trace-request-reply.>` `Publish Violation` from
      `observability-service`.** `KeyValue.WatchAll`'s push consumer
      periodically flow-control-acks on a server-generated
      `$JS.FC.<stream>.<inbox>` subject — distinct from a message Ack
      (`$JS.ACK`) and not covered by any existing grant; without it the
      watch doesn't error, it silently stalls once the flow-control window
      fills. Added to the `observability` PLATFORM user's `--allow-pub` list
      in `nats/bootstrap-operator.sh`; required a full `--force`
      operator/account regen (all prior `.creds` invalidated) to take
      effect, confirmed with the user before running given the blast
      radius, then verified live. `BUSINESS_RULES-SHIPPING.md`'s trace-store
      rule amendment (Phase 30h, amended 2026-08-17).
- [x] **`TraceWaterfall.vue`'s "(span not yet finished)" label was always
      wrong.** No span in the trace store can ever be unfinished —
      `natstrace`'s `finish()` is the sole `obs.trace.*` publish point, so a
      span is only ever seen already-finished. Replaced with a real
      classification: `respondedByEmptyLabel` computed reads "async event —
      no NATS responder" for an `evt.*`-subject span (no synchronous caller
      by design) and "call failed — no reply received" for a
      `statusCode === 'ERROR'` span, mirroring the existing
      `requestedByEmptyLabel` pattern. Two new `TraceWaterfall.spec.js`
      specs; verified live against fresh `arrive` traces (both a normal
      event-consumer span and a real 422 failure).
- [x] **`evt.*` projector spans echoed their request payload back as the
      response payload.** The three shipping-service `Consume` callbacks
      (`dictionary/internal/eventhandler/{handler,container_handler,
      meta_handler}.go`) called `sp.End(msg.Data(), nil)`/
      `sp.Fail(err, msg.Data(), nil)` — the same raw event bytes already
      captured as `requestPayload` — making every event-consumer span's
      Response body section in the Admin UI show a byte-identical copy of
      its Request body, implying a reply that was never sent (a JetStream
      consumer's `Ack`/`Nak` is flow control, not a response payload).
      Changed to `sp.End(nil, nil)`/`sp.Fail(err, nil, nil)`; `preparePayload`
      and the frontend's existing nil-payload fallback (`—`) needed no
      change. `BUSINESS_RULES-SHIPPING.md`'s BR-036 Phase 30j amendment;
      verified live.

---

### Phase 20 (20a/20b DONE 2026-08-03) — JetStream Account Limits: Update, Visibility, and Stream-Count Redesign

#### Goal

Raised as "Gap #5" from the 2026-08-03 accounts architecture review: the `acme` account hit `js_max_streams=10` with zero headroom (`SHIPPING` + 4 KV buckets × 2 contexts already provisioned), silently wedging the Shape A/B projectors into an infinite Nak/redeliver loop for a new context — a tenant minted at runtime had no way to raise its own limits, and no way to see usage against them beforehand. Two parts, per Synadia's own operational guidance for this failure mode: (20a) make limit changes a routine, supported, monitored operation; (20b) stop the *design* from needing ever-larger limits by collapsing the per-`{context}` stream fan-out so a tenant's stream cost is fixed regardless of how many business-unit contexts it adds.

#### Phase 20a — accounts-service: update + visibility (DONE 2026-08-03)

- [x] `Provisioner.UpdateAccountLimits` + `Store.SetJSLimits` (mirrors the existing `ReactivateAccount` pattern)
- [x] `POST /api/accounts/{name}/jslimits` handler + route + `AuditActionJSLimitsUpdated` — see **BR-AC12**, `BUSINESS_RULES-ACCOUNTS.md`
- [x] `GET /api/accounts/usage` — bulk JetStream usage endpoint (`accounts/jsusage.go`, `GET {NATS_MONITOR_URL}/jsz?accounts=true` joined against `Store.List()`'s limits)
- [x] Ginkgo specs: successful update reflected via `GET`/live `AccountInfo()`, 404 unknown account, negative-value rejection, audit row, notify event
- [x] `BUSINESS_RULES-ACCOUNTS.md` BR-AC12 entry; `BUSINESS_RULES.md` index bump
- [x] `ginkgo ./...` from `backend/accounts-service/` green

#### Phase 20b — Admin UI + shipping-service kvstore redesign (DONE 2026-08-03)

- [x] Admin UI: usage column (colored above 80%/100%) + Edit Limits dialog reusing the Create Account dialog's four `InputNumber` fields, calling the Phase 20a endpoint
- [x] `internal/kvstore/kv.go`: collapse per-`(role, context)` bucket to one bucket per role per **tenant** (bucket name = `s.prefix`, already tenant-scoped since `kvstore.New(js, prefix)` is called per-tenant); `Put`/`Get`/`Delete` build the real key as `kvContext + "." + key`; `Keys`/`Watch` switch to a context-filtered `ListKeysFiltered(ctx, kvContext+".>")` and equivalent filtered `Watch` (NATS KV keys with `.` become multi-token subjects internally)
- [x] `queries/terminal.go`'s `Terminal.List`/`ListByPort`/`ListByShip` and Shape A's `ListShips`: no code change needed — isolation now comes from the filtered `Keys()` call above, not bucket separation
- [x] Migration: since `SHIPPING` retention is `LimitsPolicy` (full replay), delete the old per-context KV buckets and let the Shape A/B projector consumers replay `SHIPPING` from the start to repopulate under the new key scheme — no custom migration script
- [x] Tests: `internal/natsaccounts/isolation_test.go`'s `TestKVBucketIsolation` doc comment already describes the target shape (one bucket, no context suffix) — confirm it now matches shipped code; add specs asserting two contexts under the same tenant get correctly-scoped, non-overlapping `Keys()`/`Watch()` results from a shared bucket
- [x] `BUSINESS_RULES-SHIPPING.md`: rewrite BR-020's bucket-naming clause (bucket names are tenant-scoped, `{context}` is now a KV-key prefix component) and light footnote pass on BR-021/024/030-032
- [x] Swagger doc comments (`dictionary/internal/rest/kv.go`): update `@Description` strings that reference per-context bucket names
- [x] `ginkgo ./...` from `backend/shipping-service/` green
- [x] Live smoke test: reapply acme's existing limits through `POST /api/accounts/acme/jslimits`; `GET /api/accounts/usage` reports `5 / 10` streams (`SHIPPING` + four shared KV buckets); confirm `PHASE20B-ATLANTIC` in `acme-atlantic-fleet` appears in Sea Freight Flow


### Phase 21 (IMPLEMENTED 2026-08-03) — Account Exports/Imports: Two-Account Partitioning (PLATFORM Cross-Cutting, Tenant Data-Plane)

#### Goal

Move cross-account communication from "open a second connection with a second `.creds` file" (today: `accounts-service` holds SYS+PLATFORM, `shipping-service` holds PLATFORM + every tenant account) to NATS's own account-JWT-declared exports/imports — the fix Phase 13's completion note and `ARCHITECTURE-ACCOUNTS.md`'s "Production-scale fix" sketch both flagged and deferred. Target partitioning: **PLATFORM** holds cross-cutting services (`accounts-service`, `refdata-service`) and declares exports; **tenant accounts** (`acme`, `globex`, runtime-minted) hold the data plane (`shipping-service`'s per-tenant `SHIPPING` stream + KV, the browser) and declare matching imports.

Bonus: a service import with a subject remap (tenant publishes a bare local subject; the server stamps the tenant's own account identity onto it) closes a real gap — today `refdataconsumer`'s `{context}` is caller-supplied, so nothing stops a client connected as `acme` from asking for `globex`'s data.

Full design (four export/import declarations, `refdataconsumer`/`provisioner.go`/`bootstrap-operator.sh`/shipping-service changes, test plan, doc updates) recorded in the plan-mode session that produced this phase — see git history / session log for 2026-08-03, or regenerate from this checklist:

#### Checklist

- [x] `bootstrap-operator.sh`: PLATFORM exports/imports and restricted `shipping-admin` user
- [x] `accounts/provisioner.go`: preserve claim wiring and mint tenant imports
- [x] `accounts/handler.go`: fetches PLATFORM public key before minting
- [x] `refdataconsumer/consumer.go`: four fixed local subjects; context-list unchanged
- [x] tenant-scoped consumer and lifecycle subscriptions wired in `tenant.go`
- [x] narrowed admin connection/docs and `NATS_ADMIN_CREDS_PATH`
- [x] Docker admin credential path and tenant-creds exclusion
- [x] import/remap/isolation lifecycle specs
- [x] account-claim wiring/preservation specs
- [x] refdata consumer test harness asserts local-import transport
- [x] account/import preservation business rule
- [x] refdata/shipping rule notes updated
- [x] accounts architecture updated
- [x] `ginkgo ./...` green in both `accounts-service` and `shipping-service`
- [ ] Live verification: `bootstrap-operator.sh --force` + `docker compose down -v && up --build`; refdata labels still resolve for both tenants; Connections/Services panels still show PLATFORM-labeled rows; a tenant-created event still reactively provisions shipping-service resources; crafting the old-style cross-context subject directly now fails/times out

### Phase 22 — Business Units Owned by accounts-service

#### Goal

Business units (the `{context}` scope — `acme-pacific-fleet`, `acme-atlantic-fleet`, …) are currently independent of the tenant/account concept: refdata-service seeds them via its own `seed.go`, with only an unenforced `tenant` metadata column (BR-D34) loosely linking a context back to an account name. There is no registration flow — the two demo business units are a fixed, hardcoded pair, and three frontend stores each carry their own hardcoded `CONTEXTS` fallback array as a result.

This phase makes accounts-service the sole authority for which business units exist per account: registered through the Admin UI's Accounts panel, with a reserved `_default_bu` context that silently covers any account with zero registered business units, and no hardcoded context lists anywhere in the frontend.

#### Design

**Ownership.** accounts-service gains its own `business_units` table (one row per account per business unit: `account_id`, `name`, `visible`, `created_at`) — the authoritative registry a human manages via the Admin UI. refdata-service's existing `contexts` table remains the store every context-consuming read (corpus inheritance, KV/Postgres scoping) already goes through unchanged; accounts-service becomes its *writer* — calling refdata-service's existing `POST /api/refdata/admin/contexts` at BU-creation time — instead of refdata-service seeding a fixed list at its own startup. This keeps the shipping-service/refdata-service read path (`rpc.*.refdata.*` and BR-D35's `ListByTenant`) exactly as it is today; only who writes the row changes.

**Reserved `_default_bu`.** A single shared literal context value (not per-account), seeded once by refdata-service's own `seed.go` alongside `_platform` — the same sharing model `_platform` already uses, safe because tenant isolation is the NATS account boundary, not the context string (Phase 20b). `Parent: PlatformContext`, `Tenant: ""` (untenanted, so BR-D34/BR-D35's `ListByTenant` returns it for every tenant automatically, the same mechanism that already surfaces `_platform`). Requires a second named, sanctioned exception to BR-D33 (the first being `_platform`).

Every account implicitly resolves to `_default_bu` when it has zero registered real business units — accounts-service does not need to create a per-account row for it in refdata-service; it only needs its own per-account `business_units` row (name `_default_bu`, `visible` defaulting to `true`) purely for the Admin UI's own visibility bookkeeping.

**Mutual exclusivity — relaxed, not strict.** `_default_bu` and real business units are not hard-exclusive. Registering an account's first real business unit always surfaces a confirmation prompt in the Admin UI asking whether to hide `_default_bu` (no attempt to detect whether it actually holds data — that would require accounts-service reading into shipping-service/refdata-service's stores, a new cross-service read dependency this phase deliberately avoids). Confirming sets `visible = false` on the account's `_default_bu` row (`PATCH /api/accounts/{name}/business-units/_default_bu`); declining leaves it visible and selectable permanently alongside real business units. `visible` is a toggle, not a delete, and is also directly editable per-row in the Admin UI's business-unit table.

**Migration of `_default_bu`'s underlying data — explicitly deferred.** No migration path ships in this phase. Flagging as a known gap: `_default_bu`'s context id may already be referenced inside published NATS events (JetStream history, KV entries) that a later migration to a named business unit would need to handle without silently orphaning or duplicating data.

**Demo seed data.** refdata-service's `seed.go` drops its own creation of the two demo business units (`PacificFleetContext`/`BusinessUnitContext` constants and the `Register()` calls that create them) — those become the responsibility of accounts-service's own seed step, calling the new BU-creation endpoint for the `acme` account exactly like it already seeds the `acme`/`globex` accounts themselves. The BR-V06/V07 hazard-class override demo data (`3` override, `X1` addition — currently seeded onto the two demo business units) moves onto `_default_bu` instead, since that context is guaranteed to exist at refdata-service's own startup with no ordering dependency on accounts-service's dynamic creation.

**shipping-service.** `dictionary/internal/postgres/migrate.go`'s `seedDefaultPorts` currently hardcodes `contexts := []string{"acme-pacific-fleet", "acme-atlantic-fleet"}` — replaced with seeding default ports only for `_default_bu` (the one context guaranteed to exist for every tenant at every startup). A dynamically-registered real business unit starts with zero ports; the operator adds them via the existing "Add a shipping port" UI action, consistent with "no hardcoded/fallback lists" for business units generally. `listRefdataContexts`'s existing `rpc.*` call is unchanged in transport; refdata-service's `ListByTenant` now filters to `visible = true` server-side (no separate "include hidden" mode needed — the Admin UI's BU management table reads accounts-service's own registry directly, which already carries `visible` per row, not refdata-service's copy).

**Frontend.** The three hardcoded `CONTEXTS` fallback arrays (`frontend/seafreight-app/src/stores/port.js`, `frontend/admin/src/stores/dictionary.js`, `frontend/refdata/src/stores/dictionary.js`) are deleted outright, per this phase's "no fallbacks" requirement — a failed business-unit fetch shows an empty dropdown, not a guessed one. `frontend/admin/src/components/AccountsPanel.vue` gains a business-unit sub-table per account (name, visible checkbox, "Add business unit" action) and the hide-confirmation dialog described above.

#### Checklist

- [x] `accounts-service`: `business_units` table + migration (`account_id`, `name`, `visible` default `true`, `created_at`; unique on `(account_id, name)`)
- [x] `accounts-service`: reserved-name validation mirroring BR-D33 (leading `_` rejected except the literal `_default_bu`); PLATFORM/SYS accounts excluded from BU registration entirely
- [x] `accounts-service`: `_default_bu` row auto-created for every account at account-creation time (`visible: true`)
- [x] `accounts-service`: `GET/POST /api/accounts/{name}/business-units`, `PATCH /api/accounts/{name}/business-units/{buName}` (`{visible: bool}`); `POST` also calls refdata-service's `POST /api/refdata/admin/contexts` to create the matching context row (`tenant` set to the account name, per BR-D34)
- [x] `accounts-service`: demo seed step registers `acme-pacific-fleet`/`acme-atlantic-fleet` for the `acme` account via the new endpoint (replacing refdata-service's retired hardcoded seeding of the same two contexts)
- [x] `refdata-service`: `visible` boolean column on `refdata.contexts` (default `true`); `ListByTenant` filters to `visible = true`
- [x] `refdata-service`: `_default_bu` reserved context — second sanctioned BR-D33 exception, seeded once in `seed.go` (`Parent: PlatformContext`, untenanted)
- [x] `refdata-service`: `seed.go` drops `PacificFleetContext`/`BusinessUnitContext` creation; BR-V06/V07 hazard-class demo override (`3`, `V1`) moves onto `_default_bu`
- [x] `shipping-service`: `migrate.go`'s `seedDefaultPorts` seeds default ports for `_default_bu` only, not a hardcoded business-unit list
- [x] `frontend/admin`: `AccountsPanel.vue` business-unit sub-table (name, visible checkbox, add action) + hide-`_default_bu` confirmation dialog on an account's first real-BU registration
- [x] `frontend`: delete the three hardcoded `CONTEXTS` fallback arrays (`seafreight-app/stores/port.js`, `admin/stores/dictionary.js`, `refdata/stores/dictionary.js`) and any spec asserting their contents
- [x] `BUSINESS_RULES-ACCOUNTS.md`: new BR-AC15/16/17 entries (BU registration, `_default_bu` reservation, visibility toggle semantics)
- [x] `BUSINESS_RULES-REFDATA.md`: BR-D38 (`_default_bu` sanctioned exception) alongside BR-D33; note on BR-D34/BR-D35 that per-tenant BU rows are now accounts-service-authored, not refdata-service-seeded
- [x] `ARCHITECTURE-ACCOUNTS.md` / `ARCHITECTURE-DICTIONARY.md` updated (this phase's design section)
- [x] `ginkgo ./...` green in `accounts-service` and `refdata-service`; shipping-service tests updated for the dropped static port-context seeding
- [x] Live verification: `docker compose down -v && up --build`; fleet dropdowns populate from the live business-unit list with no hardcoded fallback; adding an account's first real BU surfaces the hide-`_default_bu` prompt; declining leaves `_default_bu` selectable alongside the new BU; toggling `visible` in the Admin UI table is reflected in the fleet dropdown

### Phase 22b (IMPLEMENTED 2026-08-13) — Business Unit Name/Context Split; Per-Tenant Default BU

#### Goal

Phase 22 gave a business unit a single `name` field that doubles as both the human label and the `{context}` subject token, so an operator registering "Pacific Fleet" has to type `acme-pacific-fleet` and live with that string as the UI label forever. This phase splits the two: a free-text English **name** (`Pacific Fleet`) and an immutable, subject-safe **context** slug (`acme-pacific-fleet`), auto-derived from the name at registration and editable before it is committed.

It also retires the shared `_default_bu` *as a tenant's context*. Today every account with no real business units resolves to one globally shared context value, and because `refdata.dictionary_items` is keyed `(context, type_key, code)` with no tenant column, two tenants writing the same code under `_default_bu` collide on the same Postgres rows. Each account gets its own `{tenant}-default` instead.

#### Design

**refdata-service already models the split.** `refdata.contexts` has carried both a `context` column (PK, subject-safe, validated by `ValidateContextName`) and a free-text `name` column since Phase 16 — accounts-service has simply been collapsing them, sending `{"context": buName, "name": buName}` from both `cmd/main.go`'s `refdataRegisterContext` and `accounts/handler.go`'s `callRefdataRegisterContext`. **No refdata-service schema change is required by this phase**; accounts-service stops throwing the display name away.

**Slug derivation and immutability.** The slug is auto-derived in the Add-BU dialog as `{tenant}-{slugify(name)}` and stays editable until submit. After that it is immutable — not a preference but a hard constraint: none of refdata's data tables (`dictionary_items`, `dictionary_localizations`, `dictionary_references`, `dictionary_locales`) carry a foreign key back to `refdata.contexts`, so renaming a slug silently orphans every row, plus the `refdata-{context}` KV bucket, the versioned corpus buckets, and the already-immutable `evt.{context}.…` JetStream history. `name` stays freely editable and gains the `PATCH` field it has never had.

**Global slug uniqueness.** accounts-service's `UNIQUE (account_id, name)` is per-account, but refdata's `context` is a **primary key — globally unique** — and `ContextRepository.Register` upserts on conflict while accounts-service's call to it is best-effort/log-only. A slug clash therefore lets one tenant silently overwrite another tenant's context row (name and `tenant` ownership metadata). `UNIQUE (context)` across all accounts, rejected with a 409, closes that; tenant-prefixing makes it collide-by-accident-proof in practice. The prefix is a naming convention for uniqueness and readability only — per `ARCHITECTURE-COMMUNICATIONS.md` § 2.3 the value stays **opaque** and is never split on `-` to recover the tenant.

**Validation moves upstream.** accounts-service currently validates only non-empty and a leading `_`; there is no charset check at all, so a BU named `west coast` persists locally and fails *silently* downstream because the refdata call is best-effort. A `ValidateSubjectToken`-equivalent moves onto the slug at accounts-service, stricter than refdata's `^[A-Za-z0-9_-]+$`: **lowercase-only**, since NATS subjects are case-sensitive and `Acme` ≠ `acme` is a live footgun, plus a length cap (the slug ends up inside `refdata-{context}-v{N}` bucket names).

**Default BU: tenant-owned, no `_` prefix, readonly.** The default becomes `{tenant}-default` (name `Default`, `[reserved]` tag retained in the UI). Dropping the `_` prefix keeps BR-D33 a hard two-exception rule instead of growing an exception per tenant, and removes the need for a `RegisterDefaultBu`-style validation bypass — a tenant default is just an ordinary BU that happens to be auto-created. It is identified by an explicit `is_default BOOLEAN` column, **not** by string-matching the slug (Phase 22 hardcodes the literal `_default_bu` in ~3 backend and ~4 frontend spots; per-tenant slugs break all of them). Readonly covers identity only: not renamable, not deletable, not creatable through `POST /business-units`. `visible` remains toggleable — BR-AC17's hide-once-a-real-BU-exists flow depends on it.

**`_default_bu` survives as a platform-owned template.** It stops being any tenant's context and becomes the parent every tenant default inherits from: `_platform` → `_default_bu` → `{tenant}-default`. Its `_` prefix becomes *correct* rather than an exception-by-fiat, BR-D38 keeps its sanctioned-exception status with a clarified meaning, and the BR-V06/V07 hazard-class override demo data stays in one place while every tenant default still inherits it through the ancestor walk.

**Inheritance works, but only through the corpus path.** `CorpusRepository.CreateDraft` walks the ancestor chain and flattens each ancestor's locally-authored rows (`domain.FlattenCorpus`); the live path (`item_repository.go` et al.) is a flat `WHERE context = $1` with no chain traversal anywhere. This is **already true of `_default_bu` today** — it works because it is directly seeded and its corpus is published at seed time, not because live reads inherit. Per-tenant defaults parented into the same chain therefore reproduce current behavior exactly, with two ordering requirements: (1) locales are **not** covered by corpus flattening (`dictionary_locales` is on the flat path), so each tenant default must have its locales registered explicitly the way `seed.go` already does for `_default_bu`; (2) `CreateDraft` *silently* skips an ancestor with no published corpus, so tenant-default creation must be ordered after refdata-service's seed completes, with a retry rather than best-effort. Closing the live-path gap is deferred to Phase 106.

#### Checklist

- [x] `accounts-service`: `business_units` gains `context TEXT NOT NULL` + `is_default BOOLEAN NOT NULL DEFAULT false`; `UNIQUE (context)` global; keep `UNIQUE (account_id, name)` for display names. Migration backfills `context = name` (every existing value is already a valid slug — `acme-pacific-fleet`, `_default_bu`), rewrites any legacy `_default_bu` row to `{tenant}-default` before the unique index is built (two accounts both carrying the shared literal would otherwise collide on it), then `SET NOT NULL`
- [x] `accounts-service`: slug validation — `^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`, 48-char cap, leading `_` impossible by construction (the charset has no way to produce one); applied on write in `createBusinessUnit`, not deferred to the best-effort refdata call (`accounts/slug.go`)
- [x] `accounts-service`: `businessUnitResponse` gains `context` + `isDefault`; `createBURequest` gains optional `context` (server derives `{tenant}-{slugify(name)}` when omitted); `updateBURequest` gains `name` (as a pointer, alongside `visible`, so a rename-only request can't be misread as "hide")
- [x] `accounts-service`: `PATCH /api/accounts/{name}/business-units/{buContext}` path param is now the slug; rejects rename of an `is_default` row (409); no delete endpoint exists for a BU at all (Phase 22 never added one), so "not deletable" holds trivially; `visible` toggle unchanged
- [x] `accounts-service`: both refdata calls send `context` and `name` as distinct values, via a new shared `RefdataClient` (`accounts/refdata.go`) — replaces the two hand-rolled, already-drifting HTTP helpers in `handler.go` and `cmd/main.go`
- [x] `accounts-service`: default BU becomes `{tenant}-default` (name `Default`, `is_default: true`), auto-created in `createAccount` (BR-AC16) **and** in `cmd/main.go`'s `seedPreexistingAccounts`; registers the context with `Parent: "_default_bu"`, registers its locales explicitly, then `CreateDraft` + `Publish` — gated on `RefdataClient.WaitForPublishedAncestor`, which doubles as a "refdata-service isn't listening yet" retry, not just a "corpus not published yet" check (a cold `docker compose up` hits connection-refused, not merely an empty result, on the very first call)
- [x] `refdata-service`: no schema change; `_default_bu` stops being assigned to tenants and is documented as the platform-owned template parent
- [x] `shipping-service`: investigated and found to be a **non-issue**, not a fix — `port_repository.go`'s `_default_bu` OR-fallback and `migrate.go`'s seeded base-port list target the *platform template* context, which still exists unchanged (it never stopped being a real, valid context; it only stopped being assigned as anyone's tenant default). Left untouched; the frontend's separate `getPorts('_default_bu')` TODO(tenant-scoping) hack in `seafreight-app/stores/port.js` is likewise unaffected and remains its own pre-existing, unrelated gap
- [x] `frontend/admin`: Add-BU dialog gains two fields (Name free-text; Context auto-derived via a client-side `deriveContext`/`slugify` mirroring the backend, editable, regex-validated inline, with an immutable-after-creation warning); BU table gains a Context column (mono); `is_default` replaces every `bu.name === '_default_bu'` string-match; hide-default-placeholder dialog now names the real per-tenant slug instead of a hardcoded literal
- [x] `frontend`: `availableContexts` becomes `{context, name}[]` in `seafreight-app` (sourced from accounts-service's now-unfiltered BU list, selecting on `isDefault` rather than a `_`-prefix string check) and `frontend/refdata` (sourced from refdata-service's context list, which already had both fields — the frontend was just discarding `name`); the two `<Select>`s gain `option-label="name" option-value="context"` (precedent: `refdata/components/VersioningPanel.vue`). `frontend/admin`'s `dictionary.js` has no context `<Select>` at all (context is auto-picked, never operator-chosen) and was left as-is — no label/value split to make. `store.context` keeps holding the slug everywhere, so the ~80 downstream API call sites are unaffected
- [x] `BUSINESS_RULES-ACCOUNTS.md`: BR-AC26 (name/context split + slug immutability), BR-AC27 (slug charset + global uniqueness), BR-AC28 (default BU is tenant-owned, auto-created, readonly), BR-AC29 (tenant default parenting, locale registration, corpus publish ordering); amend BR-AC15/16/17
- [x] `BUSINESS_RULES-REFDATA.md`: amend BR-D38 — `_default_bu` is the platform-owned template parent for tenant defaults, never a tenant's own context
- [x] `ARCHITECTURE-ACCOUNTS.md` / `ARCHITECTURE-DICTIONARY.md` updated — rewrote ARCHITECTURE-ACCOUNTS.md's "Business unit registration" section for the name/context split and per-tenant default; rewrote ARCHITECTURE-DICTIONARY.md's Seeding section and "Contexts form a tree" (now three levels: `_platform` → `{_default_bu, real BUs}` → `{tenant}-default`), fixing a pre-existing stale claim along the way (the per-context locale-registration loop never actually ran over `acme-pacific-fleet`/`acme-atlantic-fleet` — only over the two reserved roots)
- [x] **Tests**: Phase 22 shipped with *no* Go coverage of BU behavior at all — added `accounts/slug_test.go` (`ValidateContext`/`DeriveContext`/`DefaultContext`, BR-AC26–28) and a new `Describe("Business units …")` block in `accounts/handler_test.go` covering BR-AC16/26/27/28 end to end over real HTTP + Postgres (default auto-create, derived vs. explicit slug, invalid-slug rejection, cross-account global-uniqueness conflict, rename-preserves-slug, default-rename-rejected-but-still-toggleable, default-sorts-first)
- [x] `ginkgo ./...` green in `accounts-service` (82/82 specs, including the new BR-AC26–29 coverage)
- [x] Live verification: `docker compose down -v && up --build`; confirmed in Postgres that `acme`/`globex` each carry their own `{tenant}-default` row (`acme-default`, `globex-default`) with no `_default_bu` collision, and that `refdata.contexts` shows the full `_platform → _default_bu → {tenant}-default` chain with `name` populated distinctly from `context`; confirmed via the versioned corpus API that `acme-default` inherits `_platform`'s full item set (`sourceContext: "_platform"` on every row) while the live (non-versioned) read path correctly returns empty, matching the documented Phase 106 gap; exercised the Admin UI end to end — Add-BU dialog auto-derives `acme-west-coast-fleet` from "West Coast Fleet" and rejects an edited-to-invalid slug inline; registering globex's first real BU correctly named its real slug (`globex-default`) in the hide-placeholder dialog rather than a hardcoded literal, and hiding it round-tripped to `visible: false` in Postgres

Phase 22b is now fully complete: code, tests, both `BUSINESS_RULES-*.md` files, both architecture docs, and the Admin UI's BU table width follow-up (widened from a 36rem cap to a proportioned, full-width layout once the Context column made it cramped) are all done and verified live.

---

## Phase 32 — Completed (archived 2026-08-17)

### Phase 32 (PROPOSED 2026-08-17) — refdata-service Serves Browsers Directly: per-tenant `api.*`, admin/business subject split, `nats.ws` in the refdata UI

#### Goal

`refdata-service` is the last service a browser cannot reach directly. Today the
path is `browser → REST → shipping-service → rpc.* → refdata-service`, using
five relay routes on shipping-service's mux (`/api/refdata-demo/{context}/{type}/{code}`,
`/api/refdata/types/{type}`, `/api/refdata/locales`, `/api/refdata/contexts`,
`/api/refdata-watch`) — shipping-service acting as an API conduit for another
service's data, which is exactly the coupling Phase 33 exists to remove. The
`frontend/refdata` admin UI meanwhile still talks plain REST + SSE end to end
(`frontend/refdata/src/api.js`), the only frontend that never migrated.

This phase gives `refdata-service` its own per-tenant NATS connections and
`api.*` adapter, so both the refdata UI and any business app reach it directly
inside the account they authenticated into.

**Prerequisite for Phase 33.** refdata's REST surface cannot be retired before
its `api.*` replacement exists.

#### Design decisions

- **Copy the `pricing-service`/`trading-partner-service` per-tenant connection
  pattern**, not a new design: one `nats.Connect` per provisioned tenant
  discovered from `NATS_CREDS_DIR`, kept in sync via
  `notify.accounts.account.{created,suspended,reactivated}`, with the `api.*`
  adapter mounted per connection. refdata data is scoped by `{context}`, not by
  account, so every tenant's adapter shares one set of handlers — the same
  property `pricing-service`'s `internal/tenants` already relies on.
  **Include the `nonTenantCredsFiles` exclusion** (`observability.creds`,
  `sys.creds`, `shipping-admin.creds`, `platform.creds`) from the outset — its
  omission is the bug that has already cost three separate fixes; see
  `ARCHITECTURE-ACCOUNTS.md` § "Three services now open per-tenant connections".
- **This is knowingly the fourth copy of that manager.** Extraction into a
  shared `natstenants` package is the documented recommendation, blocked on a
  Go-module strategy (7 modules, no `go.work`). Either extract first and have
  this phase consume it, or paste the fourth copy and delete it later — but
  record which was chosen, and do not treat the duplication as resolved.
- **Admin and business subjects are split by namespace, not just by convention:**
  `api.{context}.refdata.admin.*` for corpus drafting/publish/rollback/diff and
  context/type/locale/item registration; `api.{context}.refdata.{item,type,locales}.*`
  for the plain reads a business app needs. The split exists so **permission
  grants can scope by subject prefix** — `MintBrowserToken` must never carry
  `api.*.refdata.admin.>`. This is the mechanism that replaces the old
  "admin lives on a different transport" separation, and it is what makes
  Phase 34's requester filter meaningful ("omit all admin requests" becomes a
  subject-prefix filter, which is trustworthy, rather than a header filter,
  which is self-declared).
- **`rpc.*` is untouched.** `refdata-service`'s existing `internal/natsrpc`
  backend-to-backend surface (consumed by `shipping-service`'s
  `refdataconsumer` and `trading-partner-service`'s `refdataclient`) stays
  exactly as-is. Adding `api.*` is additive; per
  `ARCHITECTURE-COMMUNICATIONS.md` § 2.4 the two registrations are independent
  adapters over the same handlers.
- **Subjects keep the `.v1` suffix**, consistent with every existing subject.
- **`/api/refdata-watch` SSE is replaced by `notify.*`**, following Phase 23's
  precedent for the Admin UI — `refdata-service` already publishes
  `evt.{context}.refdata.{typeKey}.changed`, so the notify bridge has a source.

#### Sub-phases

- **32.1 — Business rules.** New BRs in `BUSINESS_RULES-REFDATA.md`: the
  per-tenant connection contract, the admin/business subject split and its
  permission consequence, and the notify-replaces-SSE rule. Rules before code.
- **32.2 — `internal/tenants`.** Per-tenant connection manager (or consumption
  of an extracted shared package), incl. the creds-exclusion list and
  lifecycle subscription. `nats.Name("refdata-service")` on every connection
  (CLAUDE.md's rule; assert it in a test).
- **32.3 — `internal/browserrpc` business subjects.** `item.get`, `item.get-versioned`,
  `type.list`, `locales.list`, `completeness`, `cache-status` — the `api.*`
  counterparts of the non-admin REST reads, calling the same query methods.
- **32.4 — `internal/browserrpc` admin subjects.** `api.{context}.refdata.admin.*`
  covering corpus draft/publish/rollback/versions/diff, and context/type/locale/
  item/reference/localization registration.
- **32.5 — Account exports/imports: confirmed no change needed.** Grepped
  `bootstrap-operator.sh` and `provisioner.go` for `pricing`/`trading-partner`/
  `api.*` — zero matches. `api.*` business subjects need no NATS account
  export/import declaration at all: a tenant's own `.creds` file already
  carries full pub/sub permissions within its own account (that's what lets
  `pricing-service`'s and `trading-partner-service`'s per-tenant connections
  reach `api.*` with no operator-level plumbing), and the admin/business split
  is enforced entirely by `MintBrowserToken`'s subject-prefix Deny (BR-D41),
  not by account-level imports. No trust-chain regeneration or `down -v` reset
  is required for this sub-phase — superseding this bullet's original text.
- **32.6 — `frontend/refdata` gains `nats.ws`.** Mirror `frontend/admin`'s
  `useNatsConnection.js` + `connectionFactory.js`; swap `src/api.js` from
  `fetch()` to NATS request/reply; replace the `/api/refdata-watch` EventSource
  with a `notify.*` subscription. Vitest specs updated.
- **32.7 — Retire shipping-service's five refdata relay routes** and the
  `refdataconsumer`-backed handlers behind them, plus `frontend/admin`'s
  `/api/refdata/contexts` call (`admin/src/api.js`) — repointed at refdata's
  own `api.*`. This is the coupling the phase exists to remove, so it lands
  here rather than being deferred to Phase 33.
- **32.8 — Tests.** One `Context` block per new BR. Include a **permission
  test** asserting a browser token cannot reach `api.*.refdata.admin.>` — the
  rule is only real if a denied grant proves it.

#### Checklist

- [x] 32.1 BR-D40/41/42 written in `BUSINESS_RULES-REFDATA.md` and confirmed; BR-D34 amended (its "per-tenant accounts" candidate is now taken, for the `api.*` surface only)
- [x] 32.2 per-tenant manager (`refdata/internal/tenants`) + creds exclusion + `nats.Name("refdata-service")`. **Decision recorded: fourth copy pasted, not extracted** — a shared `natstenants` package stays blocked on the missing `go.work` across 7 modules, and `trading-partner-service`'s copy had already diverged (two-pass `MountAPI`), so there was no clean template to extract. Recorded in BR-D40.
- [x] 32.3 business `api.*` subjects registered, reusing the same query methods `natsrpc`/`rest` call (incl. BR-D08's KV-first item/type resolution). `context.list`/`context.get` classified as business, not admin — REST only nests them under `/admin/` to dodge a Go `ServeMux` ambiguity.
- [x] 32.4 admin `api.*` subjects registered (21 subjects: corpus draft/publish/rollback/versions/diff + context/type/locale/item/reference/localization/translation)
- [x] 32.5 **no exports/imports change needed** — verified by grep that `api.*` needs no operator-level plumbing (same as pricing/trading-partner); the split is enforced by `MintBrowserToken`'s subject-prefix Deny. No trust-chain regen or `down -v` required. See the sub-phase bullet above.
- [x] 32.6 **scope corrected and widened.** `frontend/refdata` turned out not to use the refdata composables at all; the live consumers of the retired routes were `shared/refdata/useRefdataLabels.js` + `useL10nCopy.js`, shared by **admin and seafreight-app**. Both migrated to `api.*` + `notify.*` via an injected transport (`setRefdataTransport`) — `shared/` can't import `@nats-io/nats-core` itself, the same constraint that already forced `connectionFactory.js` to be duplicated per app. Added `refdata/internal/notifybridge` (BR-D42) since no `notify.*` source existed yet.
- [x] 32.7 shipping-service's 5 refdata relay routes deleted, plus `refdata_watch.go`, the dead response types, and the now-unused `Deps.Refdata`/`Deps.PlatformJS` wiring
- [x] 32.8 BR-D41 permission tests green (JWT deny-grant assertion in accounts-service; subject-pattern coverage in refdata-service, verified to fail when an admin subject escapes the prefix). `ginkgo ./...` green (9 suites, 115 specs) in shipping-service; `go test ./...` green in refdata-service (incl. new `internal/tenants/tenants_test.go` — `nats.Name` assertion, discover exclusion, idempotent ensure/teardown/close, all against a real embedded NATS server) and accounts-service; admin 89/89, seafreight-app 26/33 (7 pre-existing `localStorage`-undefined failures in `useL10nCopy.spec.js`/`useRefdataLabels.spec.js`, confirmed present on `HEAD` before any Phase 32 change — a vitest/happy-dom environment issue, not a regression), and refdata 33/33 Vitest green; all frontends build clean.
- [x] Live verification: **`frontend/refdata` itself was still on REST** when this checkbox was first reached — 32.6's original scope (migrate the shared label composables) never actually touched the refdata admin UI's own `src/api.js`, so Phase 33.6's premise ("both business reads and `/api/refdata/admin/*` moved to `api.*` in Phase 32") was false until this pass. Closed out:
  - **Bug fix — context-from-subject convention.** `internal/browserrpc/adapter.go` originally carried `{context}` as a request-body field on every endpoint, copying `internal/natsrpc`'s (rpc.*-specific) convention instead of `pricing-service`'s/`trading-partner-service`'s established `api.*` convention (`{context}` read off the subject via `contextFromSubject`). Fixed across all ~29 endpoints; `TypesListSubject`/`TypeRegisterSubject`/`ContextListSubject`/`ContextRegisterSubject` stay fixed-literal `_platform` (types and the context registry are genuinely global, not context-scoped).
  - **New subjects discovered missing while building the frontend client:** `types.list` (REST's `listTypes` — distinct from the existing, confusingly-named `type.list` which lists a type's *items*), `item.localizations-list`, `item.references-list` (both actively used by `ItemDetailPanel.vue`/`TranslationMatrix.vue`), and `type.list`'s `all` flag (BR-D06 deprecated-inclusive listing — bypasses the KV cache, mirroring REST). All added with adapter tests updated.
  - **PLATFORM refdata-admin credential.** `frontend/refdata` turned out to be a cross-tenant, platform-operator tool (no tenant/account concept — edits `_platform` standards and every tenant's contexts alike), so it cannot use a `MintBrowserToken`-shaped tenant credential (denied from `admin.>`) or a business tenant token (wrong trust model — any tenant could then edit shared standards). Added `accounts-service`'s `MintRefdataAdminToken` (PLATFORM account, `Pub`+`Sub` scoped to exactly `api.*.refdata.>`, plus `Sub` on `notify._platform.refdata.>`) and `GET /api/auth/refdataAdminConnectInfo`; refdata-service's `composition.go` gained `MountPlatformAPI` mounting `browserrpc.Adapter` on the same PLATFORM connection `MountRPC`'s `rpc.*` adapter already runs on (confirmed live: refdata-service shows 4 `$SRV` instances — 2 tenant + `rpc.*` PLATFORM + this new `api.*` PLATFORM one).
  - **`frontend/refdata` migrated**: `src/nats/{connectionFactory.js,useRefdataAdminConnection.js}` added (single connection, never reconnects on context switch — owned by `App.vue`'s mount lifecycle, mirroring `admin`'s `usePlatformConnection.js`); `src/api.js` fully rewritten onto `api.*`; the SSE watch replaced by a subscription to `notify._platform.refdata.>` (the **existing** Phase-23 `shipping-service` bridge, not the new tenant-fanout `internal/notifybridge` — the two are different, incompatible subject shapes for different account scopes, see `stores/dictionary.js`'s doc comment).
  - **Live-verified against the running docker stack**: create-draft → publish → rollback all succeeded end-to-end through the UI (v1 published → v2 drafted/published → v3 rollback, all real toasts, zero console errors); `admin`'s Services panel confirmed refdata-service's 4 live `$SRV` instances; `admin`'s own dashboard (shared composables) still renders clean.
  - `refdata-frontend`'s docker-compose entry needed `restart: on-failure` (nginx resolves the new `/api/auth` upstream at startup — same DNS-ordering race `admin-frontend` already guards against) and a `depends_on: accounts-service`.

#### Follow-ups left open by this phase

- **`internal/refdataconsumer` is now unused by shipping-service production code.** Deleting the five relay routes removed its only callers, so shipping-service no longer consumes refdata at all. The package (and its BR-D08/BR-D28/BR-D37 tests) was deliberately left in place rather than deleted in this phase — whether shipping-service should keep an `rpc.*` path to refdata is a Phase 33 transport-contract decision, not a side effect of deleting REST routes.
- **BR-D42 has no automated test yet** — the notify bridge is covered by live verification only. Needs an integration test asserting a synthetic `evt.*` event reaches a tenant-connection subscriber on the matching `notify.*` subject.
- **The pre-existing seafreight-app `localStorage`-undefined Vitest failures** (7 specs, `useL10nCopy.spec.js`/`useRefdataLabels.spec.js`) predate Phase 32 (confirmed reproducible on `HEAD`) and were not introduced or fixed here — a vitest/happy-dom environment gap, not a Phase 32 regression. Worth a dedicated fix, out of scope for this phase.
- **`references.list`/`localizations.list`-style reads on the admin subjects for corpus versions** (e.g. inspecting a rolled-back version's references) were not added — only the plain business-item equivalents exist. Not currently blocking any UI flow.
- **BR-D41 has no live-server test** — `accounts-service` has no embedded operator-mode NATS harness (only `shipping-service`'s `internal/natsaccounts` does, scoped to that module). Both halves of the contract are asserted statically; the server actually rejecting the publish is live-verification only.
- **Pre-existing, unrelated:** `seafreight-app`'s Vitest run has 7 failures from `happy-dom@20.10.6` + `vitest@4.1.10` leaving `localStorage` undefined. Confirmed present before this phase (baseline 7 failures, unchanged after). Not touched here — the fix is a dependency change that should be decided on its own.


## Phase 33 — Completed (archived 2026-08-17)

### Phase 33 (IMPLEMENTED 2026-08-17) — Retire Business REST: NATS-Only Business Transport, REST Reserved for Admin and Bootstrap

#### Goal

Delete the REST surface for business operations across all four domain services,
so `api.*`/`rpc.*` is the only way business data moves. REST remains for admin
operations, infra health, and the auth bootstrap that cannot itself run over
NATS. The point is not merely fewer routes — it is that **admin calls and
business calls stop overlapping**, so the Admin UI's live views can distinguish
them structurally rather than by guesswork.

Most of this is deletion, not migration: `seafreight-app` already runs ships,
containers and ports entirely over `api.*` (Phase 15d), and `pricing-service`
and `trading-partner-service` already have full `api.*` parity with their REST
routes. The REST handlers are a parallel exposure of the same handlers
(`ARCHITECTURE-COMMUNICATIONS.md` § 2.4) that no live frontend depends on.

#### Design decisions

- **One genuine gap to fill first:** `GET /api/manifest/{context}/{shipID}`
  (containers-on-a-ship) has no `api.*` counterpart. Add
  `api.*.shipping.manifest.get.v1` before deleting the route.
- **Three categories of REST survive, and the distinction is architectural, not
  taste:**
  1. **Infra** — `/healthz`.
  2. **Auth bootstrap — structurally exempt.** `accounts-service`'s
     `GET /api/auth/connectInfo`, `/api/auth/adminConnectInfo`,
     `/api/auth/tenants` mint the browser's NATS credential. A browser cannot
     fetch its NATS credential over NATS, so these can never move. Same for
     `seafreight-app`'s `/api/platform/accounts/{tenant}/business-units`
     bootstrap read. Document this as a named exemption class so it is not
     mistaken for un-migrated business REST later.
  3. **Admin/operator** — `/api/tenant` + `/api/tenant/switch` (which NATS
     account to connect under: an operator action, and the Admin UI is its only
     caller), `/api/admin/ports/{context}` (raw table rows for the Postgres
     Tables panel), `accounts-service`'s and `trading-partner-service`'s
     BasicAuth-gated operator endpoints, and `observability-service`'s
     diagnostic surface.
- **`/api/shape-b/*` decision, deferred here from Phase 31.**
  `GET /api/shape-b/ships/{context}/{shipID}` and
  `DELETE /api/shape-b/cache/{context}/{shipID}` back the Admin UI's read-path
  panel and its cache-evict button — diagnostics, not business CRUD. They are
  **kept and reclassified as admin**, renamed to `/api/admin/read-path/...` so
  the surviving name no longer references a shape taxonomy Phase 31 deleted.
  (Phase 31 deliberately left the rename until now to avoid renaming twice.)
- **Swagger becomes the admin API's documentation.** After this phase Swagger UI
  should show only admin + bootstrap routes. That is the visible acceptance
  test: if a business operation is still browsable in Swagger, the phase is not
  done.

#### Deviation discovered mid-phase (33.6)

The design above assumed Phase 32 had already moved *both* halves of
refdata-service's REST surface (business reads and `/api/refdata/admin/*`)
onto `api.*`, so 33.6 could delete `/api/refdata/*` outright. That assumption
was wrong: Phase 32's `browserrpc` adapter only covers *browser-facing*
traffic. `accounts-service` has a separate, live server-to-server HTTP client
(`accounts/refdata.go`'s `RefdataClient`) that calls `/api/refdata/admin/*`
directly for account/BU provisioning (context register/visibility, locale add,
corpus draft/publish) — and refdata-service's NATS surface has no admin-write
equivalent for that caller. Deleting the route would have broken live tenant
provisioning.

**Resolution (owner decision):** `/api/refdata/admin/*` becomes a permanent,
named REST exemption — same category as the auth-bootstrap class above —
rather than building a new `rpc.*` admin-write path in this phase. Only the
business (browser-facing) reads under `/api/refdata/*` were deleted; those
already had full `api.*` parity. Documented as BR-D43 in
`BUSINESS_RULES-REFDATA.md`. See
[[phase33_refdata_admin_rest_exemption]] for the full writeup. A future phase
would need to give `accounts-service` an `rpc.*` path for these admin writes
before this exemption could be retired.

#### Sub-phases

- **33.1 — Business rules.** The transport-contract rule: business operations
  are reachable only over `api.*`/`rpc.*`; REST is admin, infra, or bootstrap.
  Name the three surviving categories explicitly, since a rule that says only
  "no business REST" gives no guidance on the next new route.
- **33.2 — Add `api.*.shipping.manifest.get.v1`** (reuses `Terminal.ListByShip`).
- **33.3 — shipping-service:** delete `/api/ships/*`, `/api/containers/*`,
  `/api/terminal/*`, `/api/manifest/*`, `/api/ports/*` (GET + POST),
  `/api/meta/*` and their swagger annotations; rename `/api/shape-b/*` to
  `/api/admin/read-path/*`; repoint `frontend/admin`'s `api.js` accordingly.
- **33.4 — pricing-service:** delete `/api/pricing/*` (34 routes, already 1:1
  with `api.*`).
- **33.5 — trading-partner-service:** delete `/api/trading-partners/*` (14
  routes); confirm `frontend/admin`'s `TradingPartnersPanel.vue` is fully on
  `api.*` first.
- **33.6 — refdata-service:** delete the business half of `/api/refdata/*`.
  `/api/refdata/admin/*` kept permanently — see Deviation above.
- **33.7 — Swagger regeneration** across all services (`swag init`; note
  `swag_regen_diff_noise` — it rewrites `$ref` names repo-wide, so review the
  diff rather than trusting it). Verify Swagger UI lists only admin/bootstrap
  (refdata-service: admin + the retained `/api/refdata/admin/*` exemption).
- **33.8 — Frontend sweep.** Any remaining business `fetch()` in
  `frontend/admin` repointed at `api.*`; confirm `seafreight-app` and
  `frontend/refdata` retain only the exempt bootstrap calls.

#### Checklist

- [x] 33.1 transport-contract BR written, naming the three surviving REST categories — BR-039 (shipping), BR-P26 (pricing), BR-TP16 (trading-partner), BR-D43 (refdata)
- [x] 33.2 `api.*.shipping.container.manifest.v1` registered and tested (final subject name; reuses `Terminal.ListByShip`)
- [x] 33.3 shipping-service business routes deleted; `/api/shape-b/*` → `/api/admin/read-path/*`; admin UI repointed
- [x] 33.4 pricing-service `/api/pricing/*` deleted (34 routes); `/healthz` added (service had none before)
- [x] 33.5 trading-partner-service `/api/trading-partners/*` deleted (14 routes); frontend already fully on `api.*`
- [x] 33.6 refdata-service business `/api/refdata/*` reads deleted; `/api/refdata/admin/*` kept as a permanent documented exemption (see Deviation above); REST is `/healthz` + that exemption
- [x] 33.7 swagger regenerated everywhere (pricing/trading-partner-service had no swagger to regenerate); Swagger UI shows no business operation
- [x] 33.8 no business `fetch()` left in any frontend; `frontend/admin`'s `api.js`/`ShippingForm.vue` migrated onto `api.*`; `seafreight-app`/`frontend/refdata` were already clean
- [x] `ginkgo ./...` + `go test ./...` + all frontend suites green — shipping (9 suites), refdata (5 suites), accounts-service, pricing (3 suites), trading-partner (4 suites) all pass; `frontend/admin` 91/91 Vitest + build; `frontend/refdata` and `frontend/seafreight-app` builds clean (untouched by this phase, baseline unaffected)
- [x] Live verification: `docker compose up --build` — all services and frontends started clean, no restart loops; deleted routes confirmed 404 (`/api/ships/*`, `/api/pricing/*`, `/api/trading-partners/*`); surviving admin routes confirmed reachable (`/api/admin/read-path/*`, `/api/refdata/admin/*`); all three frontends serve 200. (Full manual UI happy-path click-through not performed — route-level smoke test only.)

---

### Phase 34 (IMPLEMENTED 2026-08-17) — Enforce the Boundary: Admin-Allowlist Mux Tests, Requester Attribution, Admin-Traffic Filter

#### Goal

Phase 33 removes today's business REST. Nothing stops a future business route
being added back to a `rest/handlers.go`, and nothing yet lets the Admin UI
answer "show me only business-app traffic". This phase makes the boundary
enforced rather than merely achieved, and makes it observable.

#### Design decisions

- **The mux allowlist test is the enforcement mechanism.** Per service, walk the
  registered route set and assert every route matches the admin/infra/bootstrap
  allowlist from Phase 33's rule. A new business route then fails a test rather
  than quietly shipping. This is the pattern
  `TestShippingAdminCanOnlyUseNarrowOrderedConsumerAccess` already establishes
  for permission grants, applied to HTTP routes.
- **Requester attribution is observability, never authorization.** A
  client-supplied `Nats-Requestor`-style header (BR-027 already carries one) is
  **self-declared and must never gate anything** — core NATS request/reply
  carries no server-attested caller identity. The Admin UI may filter on it;
  no handler may branch on it. State this in the rule, because a header that
  looks like identity invites exactly that mistake.
- **The trustworthy filter axis is the subject prefix, not the header.** This is
  why Phase 32 split `api.*.refdata.admin.*` from the business subjects: "omit
  all admin requests" is a subject-prefix filter, which the server itself
  enforced by permission grant, whereas a header filter merely reflects what the
  caller claimed. Offer both in the UI, and label which is which.
- **Business tests use `api.*`/`rpc.*` only.** Confirmed requirement: no
  integration test may exercise a business operation over REST, since a test
  doing so would keep a retired path alive.

#### Sub-phases

- **34.1 — Business rules.** The route-allowlist rule (with its per-service
  allowlist as data, not prose) and the requester-attribution rule including its
  explicit non-authorization clause.
- **34.2 — Per-service allowlist tests:** shipping-service, refdata-service,
  pricing-service, trading-partner-service, accounts-service,
  observability-service.
- **34.3 — Requester attribution on `api.*`/`rpc.*`,** surfaced as a span field
  in the `obs.trace.*` envelope (BR-036's shape) so the Admin UI can read it
  from existing trace data rather than a new channel.
- **34.4 — Admin UI filter** in the Request/Reply & Traces panel: by subject
  prefix (trustworthy) and by requester (self-declared), visibly distinguished.
- **34.5 — Test-suite audit:** assert no business integration test drives a REST
  route.
- **34.6 — Docs:** `ARCHITECTURE-COMMUNICATIONS.md` § 1's transport table
  rewritten — REST is no longer "frontend/edge clients, full CRUD surface",
  which is the framing this whole 31–34 group replaced.

#### Implementation notes

`Mount` in every service's `internal/rest/handlers.go` (and, for
accounts-service, both `accounts/handler.go` and `auth/handler.go`) now
returns `[]string` — the exact "METHOD /pattern" strings it registers,
including bare-prefix mounts like `/swagger/`. Each service gained a
`TestMountRoutesMatchAdminAllowlist`-named test (accounts-service:
`TestAccountsMountRoutesMatchAdminAllowlist` and
`TestAuthMountRoutesMatchAdminAllowlist`, one per its two independent
`Mount` calls) asserting that returned list `ConsistOf` a hardcoded
allowlist — exact match, not a subset check, so both an added and a removed
route fail the test. `traceSpan` (BR-036's envelope, `internal/natstrace`,
all 5 copies: shipping/refdata/pricing/trading-partner/accounts-service)
gained a `Requester string` field populated in `finish()` from the
already-merged `Nats-Requestor` header via a small `firstHeaderValue`
helper — additive, `omitempty`, no wire-contract break. The work was
parallelized across 6 worktree-isolated agents (one per service) plus a
research/audit pass, then merged back file-by-file since each agent's
worktree started from an unrelated stub commit rather than the branch tip
(a worktree-setup quirk, not a code issue) and had to `git reset --hard`/
`checkout` the real branch content before editing — worth remembering if
worktree isolation is used again for a similar fan-out.

#### Checklist

- [x] 34.1 allowlist + requester-attribution BRs written — BR-040/BR-041 canonical in `BUSINESS_RULES-SHIPPING.md`; mirrors BR-D44 (refdata), BR-P27 (pricing), BR-TP17 (trading-partner), BR-AC33 (accounts); observability-service's allowlist documented directly inside BR-040 (its BRs already live in the SHIPPING file per the BR-034 precedent, no separate BUSINESS_RULES-OBSERVABILITY.md)
- [x] 34.2 allowlist test per service; each fails when a business route is added — all 6 services + accounts-service's 2 sub-mounts, 7 tests total
- [x] 34.3 requester attribution carried on `api.*`/`rpc.*` and visible in trace spans — `traceSpan.Requester`, all 5 `natstrace` copies
- [x] 34.4 Admin UI filter shipped, subject-prefix and requester axes labeled distinctly — `TraceWaterfall.vue` toolbar, shield icon "server-enforced" vs person icon "self-declared" with explanatory `title` tooltips, live-verified in browser (subject-prefix filter narrowed 186→29 traces, requester filter narrowed 186→34 traces on real traffic)
- [x] 34.5 no business integration test exercises REST — audited across all 6 services, zero violations; two harmless mentions found (a stale-rename comment, a doc-comment path example) and confirmed non-live
- [x] 34.6 `ARCHITECTURE-COMMUNICATIONS.md` § 1 transport table rewritten + § 6 amendment added; `ARCHITECTURE-ADMIN.md` § 4.5 documents the new filter
- [x] Live verification: added a throwaway `POST /api/pricing/{context}/fee-scales` route to pricing-service's `Mount` — `TestMountRoutesMatchAdminAllowlist` failed with a clear `ConsistOf` diff naming the extra route; reverted, test green again. All 6 services' full suites green post-merge (`ginkgo ./...` for shipping-service: 9 suites passed; `go test ./...` for refdata/pricing/trading-partner/accounts-service/observability-service: all packages ok).

---

### Phase 35 (IMPLEMENTED 2026-08-18) — Shared Go Package Extraction: `natstenants`, `natstrace`, `browserrpc` Infra Tail

#### Goal

Three infrastructure pieces were duplicated verbatim across services because
nothing in this repo shared Go code — 7 independent `go.mod` files, no
`go.work`, no `replace` directives, and every Dockerfile's build context was
a single service directory:

| Package | Copies | Size each | Cause found by |
|---|---|---|---|
| per-tenant connection manager (`internal/tenants`) | 4 — `pricing-service`, `trading-partner-service`, `refdata-service` (Phase 32), `shipping-service` (embedded in `rest/tenant.go`) | 288–577 lines | A real bug: the `observability.creds` exclusion was missing from two of the first three copies, opening phantom PLATFORM connections that failed with subscription violations — found live via NATS server logs, fixed separately in each file. See BR-D40, [tenants_manager_triplication](../memory/tenants_manager_triplication.md). |
| `natstrace` (BR-036/BR-037 hand-rolled tracing) | 5 — every service including `accounts-service` | ~250 lines | Documented, not accidental — the package doc and `ARCHITECTURE-COMMUNICATIONS.md` § 6 both explained the duplication, citing the identical build-context blocker this phase addressed. No live bug; lower urgency than the other two, but the same fix applied. |
| `browserrpc` infra tail — `contextFromSubject`, `respond`/`respondError`/`reply`, `responderIdentity`, `responderHeader` | 4 — every `browserrpc/adapter.go` | ~60–90 lines | Byte-identical apart from `refdata-service`'s `respondOK` vs `respond` naming. No documented rationale anywhere — silent duplication like `tenants.Manager`, just smaller. The service-specific handlers in each `adapter.go` (543–1301 lines) were **not** duplication and stayed where they were; only this shared tail moved. |

Bundled all three into one phase rather than paying the module-strategy
decision three separate times.

#### Design decisions

- **The module strategy was the actual deliverable of this phase**, not a
  prerequisite to work around. Chose `go.work` (workspace mode — each
  service keeps its own `go.mod`, a top-level `go.work` lists them all, `go
  build`/`go test` resolve locally without a registry) **plus** an explicit
  `replace` directive in each consuming service's `go.mod` as a
  belt-and-suspenders pin, since plain `go mod` subcommands (`tidy` in
  particular) and Docker builds don't reliably honor `go.work`'s implicit
  local override once the replaced module has its own external
  dependencies.
- **New package location:** `shared/natstenants`, `shared/natstrace`,
  `shared/browserrpc` — alongside the existing `shared/unifi-theme` and
  `shared/ui-shell` frontend packages, making "shared Go code lives in
  `shared/`" a repo-wide convention rather than a frontend-only one.
- **`shared/browserrpc` is infra only.** Exports the reply-tail helpers and
  the `Adapter` plumbing they hang off; imports no service's domain types —
  direction of dependency is service → `shared/browserrpc`, never the
  reverse. Each service's existing `adapter.go` kept its own handlers and
  just calls the shared reply tail.
- **`shared/natstenants` takes the adapter constructor as a callback**
  (`Manager[R any]`, generic over each service's own per-tenant resource
  shape — a bare `*browserrpc.Adapter` for pricing/refdata, an
  `Adapter`+`refdataclient.Client` pair for trading-partner-service, or a
  full JetStream/KV/projector bundle for shipping-service's local
  `tenantResources`), so the package never imports any service's
  `browserrpc` — same directional rule as above.
- **`shipping-service` adopted `natstenants` for connection lifecycle only**
  (`Discover`/`SubscribeLifecycle`) — its per-tenant JetStream/KV
  provisioning and its REST `SwitchTenant`/`getTenant` "active tenant"
  concept stayed local, since it already owned an equivalent per-tenant map
  (`Deps.TenantResources`) that the full `Manager` would only have
  duplicated.
- **Reply-plumbing call-site signatures were kept per-service, not
  force-unified.** `pricing-service`/`trading-partner-service` (whose
  existing `reply(req, result, err)` already matched the shared
  convention) got a one-line delegating `reply`; `refdata-service` and
  `shipping-service` (neither has a single `reply()` funnel — handlers call
  `respond`/`respondError` directly, often several times per handler at
  early-exit validation points) kept their existing `(req, subject,
  correlationID, ...)`-shaped wrapper methods with delegated bodies rather
  than forcing a purely cosmetic ~90-call-site diff in refdata-service.
- **No compatibility shims.** Every old per-service package/type replaced
  by shared code was deleted outright (`pricing/internal/tenants`,
  `tradingpartner/internal/tenants`'s old content, `refdata/internal/tenants`,
  `dictionary/internal/rest/tenant.go`'s local `discoverTenants` body,
  each service's local `errorResponse`/`responderHeader`/`responderIdentity`),
  never kept behind aliases.
- **Docs asserting "duplicated per service" fixed as code moved:** each
  package's own doc comment, `ARCHITECTURE-ACCOUNTS.md` § "per-tenant
  connections", `ARCHITECTURE-COMMUNICATIONS.md` § 6's `natstrace` passage,
  and BR-D40.

#### Sub-phases (all completed)

- **35.1 — Module strategy spike.** Proved `go.work` + per-service `replace`
  directives against both `go build ./...` from the repo root and every
  service's Docker build (repo-root build context, `COPY shared/<pkg>`
  ahead of the service's own source — the same pattern the frontend
  Dockerfiles already used for `shared/unifi-theme`).
- **35.2 — Extracted `shared/natstenants`.** Consumed by `pricing-service`
  and `trading-partner-service` first, then `refdata-service` and
  `shipping-service` (lifecycle-only). Each service's own `tenants.go`
  deleted as it was replaced.
- **35.3 — Extracted `shared/browserrpc` infra tail.** Consumed by all four
  `adapter.go` files; the duplicated functions deleted from each.
  `shared/browserrpc.Reply` (the pricing/trading-partner convention) and
  `Respond`/`RespondError` (the refdata/shipping convention) both ship from
  the same package.
- **35.4 — Extracted `shared/natstrace`.** Consumed by all five services
  incl. `accounts-service`; each service's own `internal/natstrace` package
  deleted.
- **35.5 — Tests moved with the code they cover.** `natstenants_test.go`,
  `natstrace_test.go`, and the new `browserrpc_test.go` (embedded NATS
  server, `github.com/nats-io/nats-server/v2/server`) became each shared
  package's own suite — every consuming service benefits from the same
  coverage instead of only the service that happened to have written it
  first.
- **35.6 — Documentation.** `ARCHITECTURE-ACCOUNTS.md`'s per-tenant-
  connections section rewritten from RECOMMENDATION to IMPLEMENTED voice
  (extraction diagram PNG kept as a record of what happened);
  `ARCHITECTURE-COMMUNICATIONS.md` § 6's `natstrace`-duplication passages
  amended in place; `BUSINESS_RULES-REFDATA.md`'s BR-D40 gained a "Phase 35
  amendment" paragraph recording the extraction landing and pointing at
  `shared/natstenants` instead of the deleted `refdata/internal/tenants`.

#### What shipped, one detail worth keeping that isn't obvious from the code

Fixing the mechanical `sed`-based rename of `contextFromSubject(` (and, in
refdata-service, `decode[`/`spanCtx(`) across each `adapter.go` reliably
also mangled the identifier's own local `func` definition line into invalid
Go syntax (e.g. `func sharedbrowserrpc.ContextFromSubject(...)`) — happened
identically in all four services. Not a design decision, just a repeatable
gotcha worth remembering if this shared-extraction pattern is used again:
after a blind rename-by-sed across a file, always check whether the
renamed identifier's own definition line was in the sed's blast radius.

#### Checklist

- [x] 35.1 module strategy chosen and proven against both `go build ./...` and every service's Docker build
- [x] 35.2 `shared/natstenants` extracted; all four services (pricing, trading-partner, refdata, shipping) consume it; no service has its own copy left
- [x] 35.3 `shared/browserrpc` infra tail extracted; all four `adapter.go` files consume it
- [x] 35.4 `shared/natstrace` extracted; all five services (incl. `accounts-service`) consume it
- [x] 35.5 shared test coverage (embedded NATS server) benefits every consumer, not just refdata-service
- [x] 35.6 `ARCHITECTURE-ACCOUNTS.md`, `ARCHITECTURE-COMMUNICATIONS.md` § 6, BR-D40, and each package's own doc comment updated from recommendation/duplication language to implemented-shared-package language
- [x] `ginkgo ./...` + `go test ./...` green in every service; `go build ./...`/`go vet ./...` green for all 10 workspace modules from the repo root
- [x] Live verification: full `docker compose down -v && up --build`, all services connected with zero panics/fatals/auth violations across every container's logs, Admin UI's Request/Reply trace panel showed 45 live `api.*`/`rpc.*` traces at 0 errors incl. `api.acme-atlantic-fleet.shipping.port.list.v1` (shipping-service's refactored adapter) and `api._platform.refdata.type.list.v1` (refdata-service's)

---

### Phase 44 (APPROVED and IMPLEMENTED 2026-08-18) — Request/Reply gets a `Pulse` Tab

#### Goal

Split `TraceWaterfall.vue`'s requests/errors/avg-latency pulse strip off the
*Traces* view entirely, onto its own tab in front of it — the Request/Reply
panel's tab bar becomes `[Pulse] [Traces] [Messages]`. The new tab adds a
plain-language summary of what request/reply covers and an animated
Client → NATS Server → Service flow diagram alongside the enlarged pulse
cards, addressing two gaps found during design review: the panel had no
explanation of the `_INBOX.<nuid>` reply-routing mechanism anywhere, and no
mention of `parentSpanId`/`spanId` — the mechanism that actually chains a
multi-hop call into the tree *Traces*' waterfall reconstructs (`traceId`
alone only says the hops belong to the same call, not how they nest).

#### Design decisions

- **Mockup:** `demos/01-dictionary/diagrams/admin-rpc-overview-mockup.html` — approved as-is (see ARCHITECTURE-ADMIN.md §4.5).
- **Tab name:** `Pulse`, not `Overview` — matches the strip's own existing
  internal name in the code (`pulse` computed, `.pulse-card`), over a more
  generic label.
- **Data scope (open question at mockup approval, resolved here):** `Pulse`
  aggregates the *full* unfiltered live-buffered trace set, independent of
  whatever `errors`/`slow`/`rest`-`nats` filters are active on the *Traces*
  tab. Today's pulse strip reads `displayedSummaries` (post-filter) only
  because it happens to render directly above the *Traces* toolbar; once it
  is a separate tab, sharing that filter state would mean either duplicating
  the filter UI on `Pulse` too or having it silently reflect filters set on a
  tab you're not looking at — the simpler, more honest behavior is a tab that
  always shows the whole buffer. This is the same "one deliberate omission"
  the panel already lives by (ARCHITECTURE-ADMIN.md §4.5): an aggregate must
  not claim more authority than what it actually counted.
- **Following from that:** `PulsePanel.vue` gets its own bootstrap
  (`GET /api/kv/buckets/platform/trace-request-reply/entries`) + live
  subscribe (`notify._platform.kv.trace-request-reply.>`) + trace-grouping,
  duplicated from `TraceWaterfall.vue` rather than extracted into a shared
  store — matching this exact panel's own established precedent
  (`RpcPanel.vue`'s *Messages* tab already duplicates the same bootstrap/
  subscribe pair for a flat-by-span aggregation; `Pulse` is a third
  aggregation shape over the same feed, grouped-by-trace like *Traces* but
  unfiltered).
- **Default tab:** `ui.rpcTab` default changes from `'traces'` to `'pulse'`
  — `Pulse` is first in the tab bar and is where the explanatory content
  lives, so it's the more useful landing view.
- **Flow diagram:** static/illustrative (fixed example subject and span-id
  values), not wired to live trace data — it demonstrates the mechanism, not
  a specific trace.

#### Tasks

- [x] `PulsePanel.vue`: bootstrap + live-subscribe + trace-grouping (own copy,
      trimmed to `ok`/`replyMs`/`at`), pulse bucket computation (moved from
      `TraceWaterfall.vue`, reading the local unfiltered `traceSummaries`
      instead of `displayedSummaries`), "what request/reply covers" card,
      animated flow diagram, enlarged pulse cards.
- [x] `RpcPanel.vue`: add `Pulse` tab first in the tab bar, `<KeepAlive>`-
      wrapped like `Traces`.
- [x] `stores/ui.js`: `rpcTab` default `'traces'` → `'pulse'`.
- [x] `TraceWaterfall.vue`: remove the pulse strip (markup, `pulse` computed,
      `PULSE_BUCKETS`, `.pulse-*` CSS) — rail + waterfall unchanged otherwise.
- [x] Vitest specs: a `PulsePanel.spec.js` covering the bucket computation
      (ported/adapted from `TraceWaterfall.spec.js`'s Phase 28p spec) against
      the *unfiltered* feed; update/remove the Phase 28p spec in
      `TraceWaterfall.spec.js` to match the strip's removal; `RpcPanel.spec.js`
      updated for the new default tab.
- [x] `BUSINESS_RULES-SHIPPING.md`: Phase 28p entry amended for the new tab
      and unfiltered-scope behavior.
- [x] `ginkgo`/frontend test suites green; verified live against the docker
      stack.

**Pre-existing, unrelated failures found while running the frontend suite:**
three `TraceWaterfall.spec.js` specs ("marks a crossing...", the Phase 28k
and Phase 28m ordering specs) fail on `main` before this phase's changes too
(confirmed by stashing this phase's `TraceWaterfall.vue` edit and
re-running) — not fixed here, out of scope for this phase.

---

### Phase 31 (IMPLEMENTED 2026-08-17) — Consolidate to Shape B: Retire Shapes A and C

#### Goal

The POC's founding question was which CQRS read-model shape to build V3 on, so
three were built side by side: **Shape A** (KV as the read model), **Shape B**
(Postgres projection + KV write-through cache), **Shape C** (event-sourced
reconstruction from JetStream replay). That question is now answered —
**Shape B is the chosen shape** — and the other two have stopped being evidence
and started being maintenance: three projector durables where one is needed,
three query types over the same aggregates, and a comparison UI whose only
purpose was the comparison.

This phase retires Shapes A and C. It is deliberately sequenced **first** in the
REST→NATS group (Phases 31–34) because everything downstream inherits less
surface: one shape's read paths to migrate off REST instead of three.

**This is not a pure deletion — that is the trap.** Shape A owns two
*production* browser paths, both of which must move to Shape B before anything
is deleted:

| Shape-A-owned | Consumed by | Symptom if deleted naively |
|---|---|---|
| `publishNotify`/`publishRawNotify` for `entity="ship"` — fired **only** from `RegisterShapeA` (`eventhandler/handler.go:53,56`); `RegisterShapeB` explicitly does not publish | Sea Freight Flow live fleet updates (`seafreight-app/src/stores/port.js`), Admin raw-watch panel | Ship rows silently go stale until page reload |
| `queries.ShapeA.ListShips` (reads the `dict-a` bucket) — backs `api.*.shipping.ship.list.v1` (`browserrpc/adapter.go:318-335`) | Sea Freight Flow bootstrap and reconnect (`seafreight-app/src/api.js`) | Fleet panel empty on connect |

The second carries a **real behaviour change**: `ShapeB.ListShips` reads
Postgres, not KV, so the fleet bootstrap leaves the KV read path entirely. That
is correct per Shape B's own definition (KV is a per-entity cache, never a list
index) but it is a latency-characteristic change, which is why it gets its own
business rule rather than being left as an implementation detail.

#### Design decisions (confirmed 2026-08-17)

- **Identifiers are renamed to neutral terms.** "Shape B" only ever meant
  anything in contrast to A and C. `RegisterShapeB` → `RegisterShips`,
  `queries.ShapeB` → `queries.Ships`, and the KV bucket `dict-b` → `ships`.
- **The KV bucket rename is a data migration.** KV bucket names are immutable,
  so `dict-b` → `ships` means creating the new bucket and abandoning the old
  one. For this POC the accepted approach is a `docker compose down -v` reset
  rather than a dual-read migration path — the buckets are projections,
  rebuildable from JetStream by definition. Anything that hardcodes `dict-a`
  as a *test fixture* bucket name (`internal/kvstore/kv_test.go`,
  `internal/natsaccounts/isolation_test.go`,
  `observability-service/.../kv_test.go`) is infrastructure, not shape logic —
  rename to a neutral name, don't delete the test.
- **The REST route `/api/shape-b/ships/...` is NOT renamed in this phase.**
  Renaming it here and then reclassifying it in Phase 33 (business REST
  retirement) is churn for a route that changes twice. Phase 31 leaves it
  alone; Phase 33 decides whether it becomes an admin-allowlisted diagnostic
  route or is deleted. Same for `/api/shape-c/fleet` — except that one *is*
  deleted here, since its query type ceases to exist.
- **The Admin UI keeps a single-shape panel.** `ShapeCPanel.vue` is deleted;
  `ShapePanel.vue` loses its `shape: 'A' | 'B'` prop and becomes the Shape B
  read-path panel (nav badge `3` → `1`). The KV-cache-vs-Postgres read path is
  still the interesting mechanism to show, even with nothing to compare it to.
- **`ARCHITECTURE.md`'s retired variant ids are deleted, not tombstoned.**
  `Proj.KV`, `Read.FR.AGG`, `Read.KV` and the legacy `A`/`B`/`C` alias map come
  out, notwithstanding the doc's "frozen once assigned" note — the narrative
  vault (`obsidian/POC-Dictionaries/`) is where the "we evaluated three and
  chose one" record lives, so the taxonomy doc doesn't also need to carry it.
- **Phases 100/103/104 are flagged, not re-scoped.** Each reasons about A/B/C
  trade-offs to justify itself, and Phase 104 (snapshotting) exists
  specifically because Shape C degrades with stream depth. This phase adds a
  note to each that its rationale changed; re-scoping is a follow-up, so that
  three speculative phases aren't rewritten mid-deletion.

**Confirmed out of scope — `container` and `meta` survive untouched.** They look
Shape-A-adjacent but are independent: `RegisterContainers` upserts Postgres
*first*, then writes KV (structurally the Shape B pattern), and `ARCHITECTURE.md`
classifies both with no shape letter. Their doc comments cross-reference
`RegisterShapeA` for the nil-safe-`nc` convention only, and those references
need rehoming rather than the handlers changing.

#### Sub-phases

- **31.1 — Migrate Shape A's two production responsibilities to Shape B.** Move
  the `publishNotify`/`publishRawNotify` ship block into the surviving ship
  projector; repoint `browserrpc.handleShipList` at the Postgres-backed query.
  Nothing is deleted yet. Rewrite `notify_test.go`'s three Shape-A-driven specs
  and `trace_async_test.go`'s Shape-A span fixtures against Shape B. This
  sub-phase must land green on its own — after it, Shape A is genuinely unused.
- **31.2 — Delete Shape A.** `queries.ShapeA`, `RegisterShapeA`, the
  `ship-shape-a` durable, `ShapeABucketPrefix`, `kvA` wiring through
  `rest/tenant.go`, the `KVA` Deps field (and dead `KVB` alongside it), and the
  Shape A blocks in `integration_test.go`/`api_test.go`/`browserrpc_test.go`.
- **31.3 — Delete Shape C.** `queries/shape_c.go` whole file, the `getFleet`
  REST handler and its `GET /api/shape-c/fleet` route, `ShapeC` Deps field and
  `tenant.go` wiring, the Shape C spec blocks in
  `integration_test.go`/`api_test.go`/`hydration_consumer_test.go`, and
  `perf/scenarios/shape-c-reconstruction.js` + its `shapeCFleet()` helper.
  **Do not touch the aggregate replay machinery** (`ship.go`/`container.go`'s
  `Apply()`/`FromState()`) — the write-side `hydrate()` path still needs it;
  only the "Shape C reconstruction" section banners are stale.
- **31.4 — Rename to neutral identifiers.** `RegisterShapeB` → `RegisterShips`,
  `queries.ShapeB` → `queries.Ships`, KV bucket `dict-b` → `ships`, and the
  neutral-naming pass over fixture bucket names. Excludes the REST route (see
  Design decisions).
- **31.5 — Frontend.** Delete `ShapeCPanel.vue` and `api.js`'s `getFleet()`;
  collapse `ShapePanel.vue` to single-shape; `stores/dictionary.js` drops
  `shapeA` state, `shapeARows`, and the two-bucket subscribe loop, and the
  `{shape, key, op, value, revision}` event envelope becomes single-valued.
  **Repoint `TelemetryStrip.vue` and `OverviewPanel.vue`** — these are shared
  dashboard panels, not part of the shapes view, and `TelemetryStrip` computes
  its headline ship count from `shapeARows.length`. Fix
  `stores/dictionary.spec.js`'s dual-bucket assertions and `KvInspector.vue`'s
  `dict-a` default selection.
- **31.6 — Business rules.** BR-024 rewrite, BR-020 and BR-019 amendments, and
  the new ship-list read-path rule. See `BUSINESS_RULES-SHIPPING.md`.
- **31.7 — Documentation and diagrams.** `CLAUDE.md` (incl. its already-stale
  `dict-a-{context}` bucket list — Phase 20b moved `{context}` into the key),
  `AGENTS.md`, root + demo `README.md`, `PERFORMANCE.md` (§1 "Shape C vs
  stream depth" is a whole measured section), `ARCHITECTURE.md` taxonomy,
  `ARCHITECTURE-ADMIN.md`'s CQRS Shapes view write-up,
  `ARCHITECTURE-DICTIONARY.md`/`-COMMUNICATIONS.md` bucket references, this
  plan's own `## Working Assumptions`, `.claude/memory/shipping_domain_overview.md`,
  and the narrative vault. Regenerate `system-architecture.png` (**source
  generator not found in-repo — locate it first**) and the two
  `POC-Dictionaries/Summary/Diagrams/*.svg` that name `Shape A`/`dict-a/b`.
  Regenerate `docs/` swagger rather than hand-editing.
- **31.8 — Write the finding up.** "Why Shape B won" belongs in
  `obsidian/POC-Dictionaries/` as a findings note — the POC's actual
  deliverable, and the reason the taxonomy doc can afford to drop the alias
  map. Note that `4. Findings - Distributed Tracing (Phase 28).md` argues *from*
  Shape A/C's existence ("Why the trace store is Shape A"); its vocabulary
  needs an authorial pass, not a find-and-replace.

#### Checklist

- [x] 31.1 ship `notify.*` block moved into the Shape B projector; `handleShipList` repointed
- [x] 31.1 `notify_test.go` + `trace_async_test.go` rewritten against Shape B; `ginkgo ./...` green
- [x] 31.2 Shape A deleted (queries, projector, durable, bucket prefix, `kvA`/`KVA`/`KVB` wiring, specs)
- [x] 31.3 Shape C deleted (query file, REST handler + route, Deps/wiring, specs, perf scenario)
- [x] 31.3 verified: aggregate `Apply()`/`FromState()` replay machinery untouched and write-side hydrate still green
- [x] 31.4 neutral rename landed
- [x] 31.4 `docker compose down -v && up --build` confirms the `ships` bucket rebuilds from JetStream
- [x] 31.5 frontend: panels, store, spec, `TelemetryStrip`/`OverviewPanel` repointed; both frontend builds green
- [x] 31.6 BR-024 rewritten; BR-020/BR-019 amended; new ship-list rule added with its test (confirmed already pre-written to target state)
- [x] 31.7 docs, diagrams, swagger regenerated; no `dict-a`/`Shape A`/`Shape C` references left outside the archive and narrative vault
- [x] 31.7 Phases 100/103/104 each carry a note that their A/B/C rationale changed
- [x] 31.8 findings note written in `obsidian/POC-Dictionaries/`
- [x] Live verification: full `down -v && up --build`, Sea Freight Flow fleet panel populates on connect **and** updates live on arrive/depart (the two Shape-A-owned paths) — verified 2026-08-17: registered `phase31-verify-ship` at Hamburg, appeared live immediately and survived a page reload (bootstrap); Admin UI's KV Buckets panel confirms ACME's tenant buckets are exactly `container`/`meta`/`ships`, no `dict-a`/`dict-b`; CQRS Shapes nav badge reads `1`, single consolidated panel renders correctly

---

### Phase 45 (IMPLEMENTED 2026-08-18) — Accounts Overview: Nav Restructure, Ring-Buffer Trend History, Gated Search

#### Goal

Implement the Admin UI redesign design-reviewed across four mockup rounds
(`accounts_overview_pulse_design.md`): `Accounts` moves from PLATFORM to
SYSTEM as its first entry; `TOPOLOGY` becomes `Sharing`; SYSTEM · NATS ·
`Account Activity` retires as a standalone nav item and its content becomes
Accounts' new first tab, `Overview` — so Accounts' tab bar reads `Overview |
Provisioning | Sharing`. `Overview` replaces `AccountActivityPanel`'s flat
number-restating expand with two small trend charts per account (connections/
subscriptions, in/out throughput), a 5m/30m/1h duration selector, and a
name-filter search box shown only once there are more than 3 accounts. This
requires real history — `/accstatz` is a stateless snapshot today — so
`observability-service` gains a ring buffer.

#### Design decisions

**Nav/tab restructure (no backend changes):**
- `App.vue`'s `sections`: move the `accounts` item out of the `Platform`
  group into `System`, as its own `{ items: [...] }` entry before the `NATS`
  eyebrow section (so it renders above Connections/Services/etc, not inside
  that eyebrow group). Remove `account-activity` from the `NATS` eyebrow's
  `items` and delete its `SUBTITLES` entry.
- **This reverses an explicit prior placement rationale** (`App.vue:94-97`:
  "Accounts is a tenant roster — a platform-membership question, so it sits
  here rather than under SYSTEM's NATS group"). That comment gets rewritten,
  not silently left stale: Accounts moves to SYSTEM specifically because its
  new Overview tab absorbs a SYSTEM/NATS panel, making Accounts the one home
  for both the business roster and the NATS-account health view, rather than
  a PLATFORM item that displays SYSTEM content.
- `AccountsView.vue`'s tab values: add `overview` (new, first `Tab`/
  `TabPanel`); rename `topology` → `sharing` (value, label, and
  `ui.accountsTab` default target for the third tab) for consistency between
  the visible label and the internal value — `ui.js`'s `accountsTab` default
  changes from `'provisioning'` to `'overview'` to match every other nav
  section's Overview-first convention (`App.vue`'s own top-level
  `activeView` already defaults to `'overview'`).
- `TopologyPanel.vue` → renamed `SharingPanel.vue` (plus its spec file) —
  content and behavior unchanged, renamed so the component name doesn't
  contradict the tab label it backs.
- `AccountActivityPanel.vue` is retired (deleted, alongside its spec) — its
  content is superseded by the new Overview tab component below, not kept
  as a second copy.
- New `ACCOUNTS_SUBTITLES.overview` entry; `topology` key renamed to
  `sharing`.

**Ring-buffer history (`observability-service`) — new, not covered by the
mockups, needs confirmation before implementation:**
- A background goroutine polls the server's own `/accstatz` every 10s (same
  interval `AccountActivityPanel.vue`'s frontend poll already uses today —
  no change in cadence, just a second consumer of the same data) and appends
  one sample per account into an in-memory buffer, trimming samples older
  than 60 minutes on each tick. Not persisted to Postgres or NATS KV —
  explicitly transient telemetry, not source-of-truth data, same reasoning
  this repo already applies to what does vs. doesn't get event-sourced.
  Buffer starts empty at process boot and fills in real time; a
  freshly-restarted service legitimately has less than 60 minutes of history
  until it's been up that long — no synthetic backfill.
- **Correctness point:** `/accstatz`'s `sent`/`received` byte and message
  counts are cumulative totals since server start, not per-interval deltas.
  The throughput chart needs bucket-over-bucket *deltas* of those counters,
  not the raw values — the ring buffer stores raw cumulative samples; the
  history endpoint computes deltas when bucketing.
- New route `GET /api/nats/account-activity/history?duration=5m|30m|1h`
  (any other value → 400). Bucket size scales with the window per the round-3
  mockup: 30s buckets at 5m, 2min at 30m, 5min at 1h. Response carries one
  bucketed series per account (connections/subscriptions as point samples,
  in/out bytes and msgs as per-bucket deltas) plus each account's
  `tenantLabel`; the fleet summary sparklines are summed client-side from the
  per-account series rather than duplicating a fleet-aggregate endpoint.
- Existing `GET /api/nats/account-activity` (the live snapshot) is unchanged
  — the new route is additive, not a replacement, since the collapsed-row
  snapshot still needs to read from a moment-in-time state.

**Frontend Overview tab component:**
- New `AccountsOverviewPanel.vue` (superseding `AccountActivityPanel.vue`):
  fleet summary cards (unchanged 4 stats) each grow a sparkline; the
  `.acct-card` list stays collapsed-by-default, expand swaps the flat number
  grid for the two trend charts, fetched from the new history route only for
  whichever card is expanded (not all accounts at once).
  - Slow-consumer explanatory copy ties directly to `slowConsumers > 0` for
    that account within the window, not a scripted "~20 min" estimate — the
    mockup's precise wording was illustrative, not a number to reproduce from
    real data.
- Duration selector (5m/30m/1h pill, defaults 30m) re-fetches history for the
  fleet sparklines and the currently-expanded card on change.
- Search box: rendered only when `accounts.length > 3`; case-insensitive
  substring match against each account's resolved label (falling back to the
  raw account identifier, matching BR-028's existing fallback); empty state
  "No accounts match "…"." Client-side only, no backend involvement.

**Explicitly out of scope for this phase** (flagged, not decided as "never"):
applying the same gated search to the Provisioning tab's account table;
persisting ring-buffer history across restarts; per-account charts for every
account rendered simultaneously rather than only the expanded one.

**Candidate business rules** (`BUSINESS_RULES-SHIPPING.md`, same file as
BR-028/BR-034, which this amends — despite living in `observability-service`,
matching that file's existing convention for Admin-UI-facing NATS monitoring
panels):
- **BR-034 amended**: retitled/rescoped — the panel it describes moves from a
  standalone SYSTEM · NATS item to Accounts' Overview tab; the slow-consumers-
  as-alarm rule itself is unchanged.
- **BR-043 (new)**: `/accstatz` history is retained in a 60-minute ring
  buffer sampled every 10s; a duration query param (`5m`/`30m`/`1h`, rejecting
  any other value) selects a bucketed, correctly-delta'd trend series.
- **BR-044 (new)**: the Overview tab's account search box is shown only when
  there are more than 3 accounts, filters by name (case-insensitive
  substring) client-side, and shows a named empty state rather than a blank
  list.

#### Checklist

- [x] Confirm the design decisions above (ring buffer shape/retention,
      history endpoint shape, BR-034 amendment + BR-043/BR-044 additions) —
      confirmed 2026-08-18
- [x] `observability-service`: background `/accstatz` poller + in-memory ring
      buffer (per-account, 60min @ 10s, trimmed each tick)
- [x] `observability-service`: `GET /api/nats/account-activity/history`
      handler — duration validation, bucketing, cumulative-counter deltas
- [x] Go tests (plain `testing`, this service's existing convention, not
      Ginkgo) for BR-043: bucket count/size per duration, delta-not-raw byte
      counts, 400 on an invalid duration, buffer eviction past 60 minutes
- [x] `App.vue`: move `accounts` nav item PLATFORM → SYSTEM (first entry);
      remove `account-activity`; rewrite the stale placement-rationale
      comment; update `SUBTITLES`/`ACCOUNTS_SUBTITLES`
- [x] `AccountsView.vue` + `ui.js`: add `overview` tab (default), rename
      `topology` → `sharing`
- [x] Rename `TopologyPanel.vue`/spec → `SharingPanel.vue`/spec (no spec
      file existed to rename)
- [x] New `AccountsOverviewPanel.vue` (+ Vitest spec) superseding
      `AccountActivityPanel.vue`: sparklines, expand-to-trend-charts,
      duration selector, gated search + empty state; delete
      `AccountActivityPanel.vue`/spec
- [x] ~~`docs/docs.go`/`swagger.json`/`swagger.yaml`: new history route
      documented~~ — N/A: `observability-service` has no generated swagger
      docs at all (unlike `shipping-service`); its `@Summary`/`@Router`
      comments are documentation-style only, confirmed no `swaggo` import
      or `docs/` folder exists for this service
- [x] `BUSINESS_RULES-SHIPPING.md`: BR-034 amended, BR-043/BR-044 added
- [x] `ARCHITECTURE-ADMIN.md`: Accounts/Account-Activity section updated to
      match the new nav/tab structure — scope statement, panel index, §2/§3
      inventories all updated to six panels; §4.3 retitled "relocated" with
      Phase 45's history/search additions documented, following §4.8's
      retired-panel precedent
- [x] `go build ./...` (observability-service) + its test suite green;
      frontend build + Vitest green (3 pre-existing, unrelated
      `TraceWaterfall.spec.js` failures confirmed present before this
      phase's changes too, left as found); verified live against the
      docker stack — nav restructure, Overview tab (real sparklines/charts
      from the live ring buffer), duration selector, and gated search all
      confirmed working against real `/accstatz` data

### Phase 36 — Completed (archived 2026-08-19)

### Phase 36 (APPROVED 2026-08-19 — design gate passed; mockups required before implementation) — Tech Lab Operator Rebrand & Trading Partners Migration

> **Numbering note:** the user explicitly requested "Phase 36." That number
> was historically tied to the NATS server-hop tracing phase (29 → 41 → 36 →
> 43), which is now live at **Phase 43**, DEFERRED. Per explicit instruction,
> every remaining stale "Phase 36" reference to that phase (BR-042's
> heading, `ARCHITECTURE-COMMUNICATIONS.md` §6, `ARCHITECTURE-ADMIN.md`
> §4.5, and the `phase36-trace-the-subject-options.png` image) was swept to
> cite 43 first — see "Renumbering (2026-08-19 — collision cleanup, Phase 36
> freed for reuse)" near the end of this document — before 36 was
> reassigned here. This phase has two sub-phases, 36.1 and 36.2, sequenced
> as separate design-gate approvals rather than one combined approval,
> since 36.2 carries a real coupling risk (below) that 36.1 does not.
>
> **Approval (2026-08-19):** the user approved both sub-phases' design
> decisions as drafted, with two additions folded in below: (1) mockups of
> the final frontend outcome are a required deliverable, reviewed and
> approved by the user, before any implementation code is written for
> either sub-phase; (2) a new architecture doc,
> [ARCHITECTURE-PLATFORM.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-PLATFORM.md),
> was created as the entry point for Tech Lab Operator's design, and
> cross-referenced with `ARCHITECTURE-DICTIONARY.md` (Reference Data is a
> subset of Platform's broader operator-facing surface). CLAUDE.md's
> "Architecture Docs" section index was updated to list it alongside the
> other `ARCHITECTURE*.md` docs.

#### Goal

Rename the `refdata` frontend app (currently branded "Dictionary") to
**"Tech Lab Operator"** and restructure it around operator/tenant-facing
tasks — refdata setup, configuration, registration of users, companies,
transporters, etc. — starting with a visual-only nav restructure (36.1),
then migrating the `admin` app's Trading Partners section into it (36.2).

#### Design decisions — 36.1 (visual rebrand + nav restructure)

- **Mockups required before implementation.** A mockup of the final
  Tech Lab Operator UI — the `Operations`/`Reference Data` nav and the new
  tabbed info panel — is produced and reviewed/approved by the user before
  any implementation code is written for this sub-phase. This is a
  deliverable gate on top of the design-decisions approval above, not a
  substitute for it.
- **Scope: frontend-visual-only.** No backend/business-rule change. Per the
  AI Agent Workflow's "ask for business rules first" step — this sub-phase
  adds/changes no domain rule, so that step doesn't apply; no
  `BUSINESS_RULES-*.md` update is needed for 36.1 itself.
- **Nav shape.** Top-level `Operations` group containing `Reference Data`,
  built with `@ui-shell/NavList.vue` — the same component `admin` already
  uses for its `Platform`/other groups. This is new for `refdata`: its
  current sidebar (`TypeNavigator.vue`) is hand-rolled and dynamically
  generated from `store.types`/`categories.js`, not `NavList.vue`-driven.
  Adopting `NavList.vue` here is a first for this app, not a refactor of an
  existing usage.
- **Existing nav content → tabbed info panel.** `TypeNavigator.vue`'s
  current content (per-category/per-type items, the "Domain" group, and the
  "Tools" group's Localization/Versioning) moves into a new tabbed panel in
  the main content area, reusing the already-documented "Panel top tabs"
  contract in `shared/unifi-theme/LAYOUT.md` (§ lines ~112–162): real
  PrimeVue `Tabs`/`TabList`/`Tab`/`TabPanels`/`TabPanel` with
  `class="panel-tabs"` on the `<Tabs>` root, `<Tabs>` flush on the page
  (never wrapped in `.lab-panel`), the card living on each `TabPanel`'s
  content — copying `admin`'s `RpcPanel.vue` (Pulse/Traces/Messages) as the
  concrete reference, per LAYOUT.md's own stated precedent. Exact tab
  breakdown (e.g. Items / Localization / Versioning / Domain) confirmed
  once this sub-phase starts, not fixed here.
- **Branding rename — narrowly scoped.** Only the frontend app's own
  display name changes:
  - `demos/01-dictionary/frontend/refdata/src/App.vue`'s `#brand` slot
    (`"Dictionary"` → `"Tech Lab Operator"`)
  - `demos/01-dictionary/frontend/refdata/index.html`'s `<title>`
  - `demos/01-dictionary/README.md` lines referencing the app by name in
    its host/port tables and the "Dictionary frontend" terminal instruction
  - **Explicitly out of scope:** the Go backend's `dictionary` package/
    domain naming, `POSTGRES_DB: dictionary`, the demo path
    `demos/01-dictionary/`, "Dictionary POC" as the demo's name in
    `lab-shell/src/demos.js`, and the `obsidian/.../Dictionary-POC/`
    architecture vault — none of these are the frontend app's branding and
    must not be touched by this rename. Particular care around
    `demos/01-dictionary/README.md`'s "Dictionary as a Service (Phase 11)"
    heading, which describes the backend `refdata-service` concept and sits
    right next to the table row that does need renaming — a blind find/
    replace on "Dictionary" in that file would over-rename it.
  - No `frontend/refdata/README.md` exists today; this sub-phase does not
    create one unless the user asks for it separately.
- **Design-system compliance.** Reuses `shared/unifi-theme` and
  `shared/ui-shell/AppShell.vue` exactly as `refdata` already does today —
  no new palette, no forked layout. `shared/unifi-theme/LAYOUT.md`'s
  existing "per-app notes" section on `refdata` (which still describes the
  old `TypeNavigator.vue`-only sidebar) gets rewritten as part of this
  sub-phase to describe the new `Operations`/`Reference Data` nav and the
  new tabbed panel, per CLAUDE.md's rule that a changed top-level screen
  must keep LAYOUT.md current.

#### Design decisions — 36.2 (Trading Partners migration)

- **Mockups required before implementation.** Same gate as 36.1: a mockup
  of Trading Partners' final placement inside Tech Lab Operator's nav is
  produced and reviewed/approved by the user before implementation code is
  written for this sub-phase.
- **Source.** `admin`'s `Platform` group's `Trading partners` eyebrow
  (`Shippers`, `Transporters`) — a plain-JS `sections` array in
  `admin/src/App.vue`, rendered by `NavList.vue`, backed by one shared
  `TradingPartnersPanel.vue` parameterized by a `partnerType` prop.
  `IconShippers.vue`/`IconTransporters.vue` are portable as-is; the backend
  `trading-partner-service` is already independent of `admin` — no backend
  coupling blocks this migration.
- **`useTenantStore()` resolution (RESOLVED 2026-08-19).** Investigation
  found the panel's actual dependency on `admin`'s `useTenantStore()` is
  two trivial reads (`tenantStore.tenant` passed as a documented no-op arg
  to `addFleetAsset`, and the same value shown in a dialog's helper text) —
  not the store's tenant-switch/reconnect machinery. The deeper issue is
  one level up: `trading-partner-service` derives tenant identity from
  *which NATS account the calling connection authenticated as*
  (`tenants.Manager` mounts `api.*` per tenant connection, no platform-wide
  mount exists), while Tech Lab Operator's `useRefdataAdminConnection.js`
  is a single cross-tenant **platform** connection with no tenant identity
  or reconnect lifecycle at all — the architectural opposite of `admin`'s
  per-tenant model. Target end-state (per user direction 2026-08-19): both
  `refdata-service` and `trading-partner-service` are PLATFORM-tier
  services, so Tech Lab Operator should keep its single platform
  connection and treat "tenant" as an explicit operator-selected *parameter*
  passed into requests — the same shape `refdata-service` already has via
  its Phase 32 `MountPlatformAPI` credential (see
  [[phase32_refdata_platform_credential]]) — rather than admin's
  per-tenant-reconnect model. `trading-partner-service` doesn't have that
  platform-mounted path yet; giving it one (plus an explicit authorization
  check, since a platform credential acting on a tenant by parameter is a
  wider trust surface than today's connection-scoped-by-account model) is
  real backend work, **explicitly deferred out of 36.2** — the user wants
  to explore the general "operator selects a tenant" UX further after 36.2
  ships. **36.2 stopgap:** migrate `TradingPartnersPanel.vue` using the
  same per-tenant connection pattern `admin` uses today, functionally
  unchanged, just physically relocated into `refdata` — no backend change,
  no new tenant-selection UX, in this sub-phase.
- **Sequencing.** 36.2 does not start until 36.1 is implemented and its own
  design-gate approval is separately confirmed — the two are not one
  combined approval.

#### Checklist — 36.1

- [x] Create `obsidian/V3-Platform/Architecture/Dictionary-POC/
      ARCHITECTURE-PLATFORM.md` as the Tech Lab Operator entry point,
      cross-referenced with `ARCHITECTURE-DICTIONARY.md`; add it to
      CLAUDE.md's "Architecture Docs" index (done 2026-08-19)
- [x] Produce mockups of the final nav + tabbed info panel; get explicit
      user approval on them before starting the items below — APPROVED
      2026-08-19 (see `.claude/memory/mockup_fidelity_functional_capability.md`)
- [x] Add `Operations` → `Reference Data` nav via `NavList.vue` in
      `refdata/src/App.vue`, replacing the current `#sidebar` composition
      (done 2026-08-19)
- [x] Build the new tabbed info panel (PrimeVue `Tabs`/`TabPanel`, following
      `RpcPanel.vue` + LAYOUT.md's "Panel top tabs" contract) housing the
      content currently in `TypeNavigator.vue`/`CategoryTypeList`/
      `LocalizationView`/`VersioningPanel` — done 2026-08-19 as the new
      `ReferenceDataPanel.vue` (`TypeNavigator.vue` retired, its
      reference-data switcher moved into the Reference Data tab, its Domain
      group became the Domain tab's Enums/Strings sub-tabs, its Tools group
      became the Localization/Versioning tabs)
- [x] Rebrand: `App.vue` `#brand`, `index.html` `<title>`,
      `demos/01-dictionary/README.md`'s app-name table rows/heading — leave
      the adjacent "Dictionary as a Service" backend section untouched
      (done 2026-08-19)
- [x] Rewrite `shared/unifi-theme/LAYOUT.md`'s `refdata` per-app note to
      describe the new nav/panel shape (done 2026-08-19)
- [x] Verify in-browser: nav renders, all previously-reachable views are
      still reachable via tabs, no console errors, dark + light mode both
      checked (LAYOUT.md/CLAUDE.md verification convention) — done
      2026-08-19: all four tabs (Reference Data, Domain incl. Enums/Strings,
      Localization, Versioning) verified live against the running stack, no
      console errors, sidebar collapse + light/dark toggle checked

#### Checklist — 36.2

- [x] Produce mockups of Trading Partners' final placement inside Tech Lab
      Operator's nav; get explicit user approval on them before starting
      the items below — APPROVED 2026-08-19
- [x] Confirm the `useTenantStore()` vs. `context` resolution approach with
      the user before writing code (design decision above) — resolved
      2026-08-19: stopgap migration, platform-credential model deferred
- [x] Move `TradingPartnersPanel.vue` + icons into `refdata`, wired to the
      new `Operations` nav (or a new group, TBD at design time) with
      `Shippers`/`Transporters` entries — done 2026-08-19: added as a
      `Trading Partners` eyebrow alongside `Reference Data`, same as
      `admin`'s own eyebrow shape. A follow-up design question surfaced
      during implementation and was resolved with the user before writing
      code: the tenant selector needed for this panel must not reuse
      shipping-service's `GET/POST /api/tenant[/switch]` (that reconnects a
      *shared backend* connection `admin` also depends on — a new cross-app
      coupling neither app has today). Resolved: `refdata` gained its own
      second, tenant-scoped NATS connection
      (`nats/useTenantConnection.js`, alongside the existing PLATFORM one in
      `useRefdataAdminConnection.js`) that only reconnects the *browser's*
      own credential — no backend reconnect at all — fed by a new
      `stores/tenant.js`. Its tenant list comes from accounts-service's
      `GET /api/auth/tenants` (already proxied for the PLATFORM connection's
      own credential fetch; no new proxy rule needed) rather than
      `GET /api/accounts` — the latter includes the reserved
      `platform`/`sys` infrastructure accounts (BR-AC06), which are not
      real tenants and were caught surfacing as bogus "tenants" during
      in-browser verification. Once a tenant is selected, its own context
      list comes from refdata-service's existing `context.list.v1` subject
      with an explicit `{tenant}` filter in the body (BR-D35), which already
      supported exactly this and needed no backend change.
- [x] Remove the migrated section from `admin`'s `Platform` group once
      parity is confirmed in Tech Lab Operator — done 2026-08-19
      (`TradingPartnersPanel.vue`, `IconShippers.vue`, `IconTransporters.vue`,
      `tradingPartnerApi.spec.js`, and `api.js`'s trading-partner client
      deleted from/trimmed out of `admin`)
- [x] Update `shared/unifi-theme/LAYOUT.md`'s `admin` and `refdata` per-app
      notes to reflect the moved section — done 2026-08-19
- [x] Verify in-browser: Shippers/Transporters CRUD flows work identically
      post-migration, no regression in `admin`'s remaining Platform items —
      done 2026-08-19: registered a shipper live against `trading-partner-
      service` under the `globex` tenant, expanded a transporter row
      (Compliance Documents/Fleet Assets/Audit Trail all populated), and
      switched tenants (`acme` ↔ `globex`) confirming each one's own fleet
      context list loads independently with no cross-tenant bleed; light
      and dark mode both checked; no console errors (one transient
      dev-server-only WebSocket warm-up artifact was investigated and ruled
      unrelated — reproduces identically on the unmodified `admin` app's own
      connection code path, not a regression from this change).

---

## Phase 37 — Completed (archived 2026-08-19)

### Phase 37 (IMPLEMENTED 2026-08-19) — VitePress Documentation Site

> **Numbering note:** the user's original request named this "Phase 36."
> **36 was not available at the time** — it was a heavily cross-referenced
> historical number for the NATS server-hop tracing phase (29 → 41 → 36 →
> 43, see the two renumbering logs near the end of the live plan and
> `obsidian/V3-Platform/Architecture/Dictionary-POC/images/
> phase43-trace-the-subject-options.png`, itself renamed off "phase36" in
> the 2026-08-19 cleanup below). Reusing it here would have collided with
> that trail. This phase was first proposed as **Phase 46** (the next open
> number following Phase 45 at the time), per this plan's own established
> "next available number" convention.
>
> **Update 2026-08-19:** every remaining live reference to "Phase 36" for
> the server-hop tracing phase was updated to cite its current number, 43
> (BR-042's heading, `ARCHITECTURE-COMMUNICATIONS.md` §6,
> `ARCHITECTURE-ADMIN.md` §4.5, the image filename above, and the live
> plan's own memory index) — see the "Renumbering (2026-08-19 — collision
> cleanup, Phase 36 freed for reuse)" log in the live plan. With that
> cleanup done, 36 was deliberately reused for a new, unrelated phase —
> see **Phase 36** (Tech Lab Operator rebrand), archived immediately above
> in this document.
>
> **Update 2026-08-19 (second):** the user then asked to rename this phase
> from 46 to **37** — the next available number immediately following
> Phase 36 — and approved it in the same request. See the "Renumbering
> (2026-08-19 — Phase 46 → Phase 37, approval)" log in the live plan. The
> phase was implemented immediately after approval; see the Checklist
> below.

#### Goal

Stand up a VitePress-based documentation site for demo 01, so architecture
and reference content can be browsed as a real docs site — locally for
now, publishable online later — rather than only as raw markdown files
across the repo and the obsidian vault. This phase is tooling/scaffolding;
it is not a business-rule change, so the "ask for business rules first"
step of the AI Agent Workflow does not apply — no domain rule is added,
changed, or enforced by this phase.

#### Design decisions

- **Location & tooling.** New standalone npm project at
  `demos/01-dictionary/docs/` using VitePress (Vue 3 + Vite) — the `docs/`
  folder is both the npm project root and the content root (VitePress's
  own recommended layout: `.vitepress/config.mts` + `package.json` live
  directly inside it). It does not join `go.work` and is not a Docker
  service — a standalone frontend-style project, same pattern as
  `lab-shell/` and the three `frontend/*` apps, each already independent
  npm projects with their own `package.json`.
- **Content ownership — fresh, not synced.** `docs/architecture/` is
  purpose-written content for this site, not a copy, symlink, or
  build-time sync of `obsidian/V3-Platform/Architecture/Dictionary-POC/`'s
  `ARCHITECTURE*.md` files. Those files are unchanged by this phase and
  remain the internal / AI-agent-facing architecture reference per
  CLAUDE.md's existing "Architecture Docs" section — this phase does not
  touch that section's policy. The docs site's `architecture/` pages may
  draw on and summarize that material, but there is no obligation to
  mirror it 1:1 and no sync mechanism to keep in sync.
- **Structure.** This phase scaffolds top-level nav sections and
  landing/index pages; deep content authoring for every page is tracked as
  follow-up (see checklist) rather than required to complete this phase.
  See the separate structure proposal shared alongside this plan update
  for the concrete section breakdown — final nav shape is confirmed once
  VitePress is actually up and content can be viewed, per the original
  request ("we can discuss structure in the generated site once support
  is added").
- **Port.** `7106` — next free port in the frontend range (7100 Admin UI,
  7101 Port Management, 7102 Dictionary, 7103–7105 reserved "under
  review" for NATS UI/NUI/NATS Tower per the existing port table). Add a
  row to `demos/01-dictionary/README.md`'s port table.
- **Theme.** Reuse `shared/unifi-theme`'s CSS variables (dark `#131416`
  background / `#006fff` accent and their light-mode counterparts) by
  overriding VitePress's own `--vp-c-*` custom properties in a small
  `.vitepress/theme/` extension of the default theme — this keeps the
  "one visual identity" rule from CLAUDE.md's "Frontend Design System"
  section true in substance even though VitePress doesn't consume
  PrimeVue/AppShell directly (it has its own theming layer, not a
  PrimeVue app). Layered on top: presentational idioms adapted from
  `obsidian/Event sourcing/Event Sourcing + CQRS + NATS — Pattern
  Cards.pdf` (eyebrow/label-caps section headers, a "DECISION"-style
  callout container, a verdict/summary badge) as custom Markdown
  containers or small Vue components local to the docs theme — not a
  second competing palette, just reference-doc-specific layout patterns
  expressed in UniFi's own colors.
- **Diagrams.** Reference already-exported PNGs (e.g.
  `obsidian/V3-Platform/Architecture/Dictionary-POC/images/`) or copy the
  specific ones needed into `docs/public/`. Raw `.drawio` workbooks are
  not embedded directly, consistent with how the rest of the repo already
  treats these exports (`drawio-architecture-drawer` skill).
- **Hosting/deployment is out of scope for this phase.** No GitHub Pages
  (or other) deploy workflow is proposed yet — the existing
  `.github/workflows/seafreight-app.yml` pattern (build-verify only, no
  deploy step) is the only CI precedent in this repo today. This phase
  covers local `npm run dev` / `npm run build` only; hosting is a
  follow-up decision once content has stabilized.
- **No `docker-compose.yml` change.** This is an authoring/build tool, not
  a demo-running service — it doesn't need a container the way the
  backend/frontend demo apps do.

#### Checklist

- [x] Scaffold `demos/01-dictionary/docs/` as a standalone VitePress
      project (own `package.json`, `.vitepress/config.mts`)
- [x] Wire `dev`/`build`/`preview` npm scripts; dev server on port `7106`
- [x] Add the docs site's port to `demos/01-dictionary/README.md`'s port
      table
- [x] Custom VitePress theme: override `--vp-c-*` tokens from
      `shared/unifi-theme` (dark + light)
- [x] Pattern-Cards-inspired custom containers/components (decision
      callout, verdict badge, eyebrow label) as local theme
      components — do not fork them into `shared/unifi-theme` unless a
      second app needs them
- [x] Scaffold top-level nav/sidebar structure with landing/index pages
      per section (see structure proposal) — deep content authoring
      tracked separately, not required for this phase's completion
- [x] `architecture/` section: author initial overview page(s); full
      page-by-page scope confirmed once nav structure is agreed
- [x] Copy/reference the diagram PNGs the initial content actually needs
      into `docs/public/`
- [x] `npm run build` produces a working static site locally; `npm run
      dev` has no console errors
- [x] Repo-root or demo-level `README.md` gets a short pointer to the new
      docs site once it has real content

Live-verified: `npm install`, `npm run build` (clean, no errors), and
`npm run dev` on port 7106 checked in-browser via the Browser pane — home
page and `/architecture/` overview render with UniFi dark palette
(`#131416`/`#006fff`) by default and the light palette (`#f4f5f7`/`#005fdb`)
on toggle, the "DECISION" custom container and `VerdictBadge` component
render correctly, the copied `system-architecture-swimlane.png` diagram
loads, and no console errors were present in either theme.

#### Docker follow-up (same day) — dockerize the docs site

Originally landed as a separate "Phase 38," folded into this phase's own
record on explicit user request the same day, since it is a direct,
same-day follow-up on this phase's own scope rather than independent
work — see the live plan's Phase 37 entry, which carries this same
folding note.

Bring `demos/01-dictionary/docs/` into `docker-compose.yml` as a
buildable/runnable service, matching the three existing frontend apps, so
`docker compose up` alone can also stand up the docs site — for local
production-style builds and as groundwork for a later real deployment.
This directly reverses this phase's own "No `docker-compose.yml`
change... hosting is a follow-up decision" design decision above, on
explicit user request, after the user asked whether the docs site could
form part of the docker-compose process and was walked through the
tradeoff (build-into-local-stack vs. actually publishing online, which
stays a separate, still-open decision) before approving.

Design decisions:

- **Same containerization pattern as the other three frontends** —
  `demos/01-dictionary/frontend/{admin,seafreight-app,refdata}/Dockerfile`
  — a multi-stage build: `node:24-alpine` runs `npm ci && npm run build`,
  then the static output is copied into an `nginx:1.27-alpine` stage
  serving on port 80, mapped to host port `7106` (already reserved for
  this site above).
- **Self-contained build context — the one deliberate deviation from that
  pattern.** The three existing frontends build with `context: ../..`
  (the repo root) because they import `shared/unifi-theme`/`shared/ui-shell`
  via Vite aliases and need those directories inside the Docker build
  context. The docs site imports neither — its palette is reproduced
  locally in `.vitepress/theme/custom.css` (see the Theme design decision
  above and CLAUDE.md's Frontend Design System "Exception" note) — so
  `docs/Dockerfile`'s context is `demos/01-dictionary/docs/` itself, and
  `docker-compose.yml`'s `docs-frontend` service uses `context: ./docs`
  rather than `context: ../..`.
- **No `/api/` proxy rules in `nginx.conf`.** Unlike `admin`/`refdata`/
  `seafreight-app`'s `nginx.conf` (which proxy to backend services), the
  docs site makes no backend calls at all, so its `nginx.conf` is just a
  static file server. Correspondingly `docker-compose.yml`'s
  `docs-frontend` service has no `depends_on`.
- **Clean-URL fallback, with a real 404 status.** `.vitepress/config.mts`
  sets `cleanUrls: true`, so the build writes per-page output as e.g.
  `architecture/accounts.html` (no directory-index form) — nginx needs
  `try_files $uri $uri.html $uri/index.html` to resolve a direct hit on
  `/architecture/accounts` to that file, matching VitePress's own
  documented nginx config for `cleanUrls`. A first pass used a bare
  `try_files ... /404.html` fallback, which serves the 404 page's content
  but returns HTTP 200 (a common nginx gotcha) — verified wrong in-browser
  via `curl`, then fixed with `try_files ... =404;` plus
  `error_page 404 /404.html; location = /404.html { internal; }`, which
  preserves the real 404 status while still serving VitePress's built
  404 page.
- **Still out of scope: publishing online.** This Docker follow-up only
  covers building the docs site into the local `docker compose up` demo
  stack. Actually hosting it somewhere reachable (GitHub Pages or
  otherwise) remains a separate, unaddressed decision — the "Hosting/
  deployment is out of scope" design decision above still holds for that
  half of the question.

Checklist:

- [x] `demos/01-dictionary/docs/Dockerfile` — multi-stage
      `node:24-alpine` build → `nginx:1.27-alpine` serve, self-contained
      `docs/`-only build context
- [x] `demos/01-dictionary/docs/nginx.conf` — clean-URL `try_files`
      fallback with a proper `error_page 404`
- [x] `demos/01-dictionary/docs/.dockerignore` — excludes host
      `node_modules`/`.vitepress/dist`/`.vitepress/cache` from the build
      context
- [x] `docker-compose.yml` — new `docs-frontend` service, `context: ./docs`,
      port `7106:80`, `frontend` network, no `depends_on`
- [x] `demos/01-dictionary/README.md`'s docs-site section updated to
      document both ways to run it (local `npm run dev` and
      `docker compose up`)

Live-verified: `docker compose build docs-frontend` clean; `docker compose
up -d docs-frontend` then `curl` against the running container — `/` (200),
a clean URL `/architecture/accounts` (200, response body's `<title>`
correctly reads "Accounts | Dictionary POC Docs"), the copied
`system-architecture-swimlane.png` (200), and a nonexistent route `/nope`
(404 status, VitePress's own 404 page content in the body) all checked
before and after the `try_files`/`error_page` fix above. Container stopped
and removed after verification (`docker compose stop docs-frontend &&
docker compose rm -f docs-frontend`) rather than left running.

---

## Phase 38 — Completed (archived 2026-08-21)

### Phase 38 (DONE 2026-08-20 — design approved 2026-08-20) — Transporter Registration & Vetting (Organizations)

> **Renumbered 2026-08-20** from Phase 46 to Phase 38 — see the
> "Renumbering (2026-08-20 — Phase 46 → Phase 38)" log near the end of this
> document.

#### Goal

Replicate Linebooker V2's `Transporters` registration/vetting workflow in
the POC: a new, event-sourced `TransporterProfile` aggregate — keyed by the
existing `Organization`'s ID, not a duplicate of its identity fields —
with a Temporal-orchestrated vetting workflow, a genuine saga with
compensating transactions (GIT insurance verification vs. document
approval), a second, genuinely cross-aggregate invariant connecting it back
to `Organization`, NATS Object Store-backed document upload, and a Tech
Lab Operator UI wizard/detail view with a state-transition visualization.
`organizations-service` is renamed to `organizations-service` as the
**last** sub-phase, not the first. Customers and Members are explicitly out
of scope (see design doc).

#### Design decisions

Full design lives in
[ARCHITECTURE-ORGANIZATIONS.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-ORGANIZATIONS.md)
— not duplicated here. Summary of the decisions it records:

- **Shared identity, separate vetting aggregate** ([ADR-046](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ADR-046-transporter-aggregate-split.md),
  revised same-day from an initial "fully separate aggregate" recommendation):
  `Organization` (Phase 26) stays the **single identity aggregate for
  both Shipper and Transporter**, completely unchanged —
  `PartnerTypeTransporter` remains fully legal, no BR-TP edits needed. A
  new `TransporterProfile` aggregate, keyed by the **same ID**, holds
  everything Transporter-specific (fleet, documents, GIT, tracking
  credentials, operating areas, vetting state). Zero duplicated fields,
  zero changes to shipped code — stronger than the superseded version's
  "zero regression, conditional on retiring a `Type` value" guarantee.
- **Event-sourced**: `TransporterProfile` only (JetStream stream
  `TRANSPORTER`, Shape B Postgres+KV read side, reusing the already-settled
  pattern) — passes the repo's own "does anything need to replay this?"
  test; `Organization` (both Shipper and Transporter identity) stays
  plain CRUD, untouched.
- **Two-step registration, handled explicitly**: `Organization.Register(...)`
  then idempotent `CreateTransporterProfile(id)` (upsert-by-ID, bounded
  retry in the handler, a standalone `EnsureTransporterProfile(id)` for
  manual recovery) — a partial failure leaves a visible, recoverable
  "Registered, profile pending" state, not a silent gap.
- **A genuinely cross-aggregate invariant**: `Organization.Activate()`,
  for a `TRANSPORTER`-typed partner only, is guarded on
  `TransporterProfile.Status == Vetted`. Lives at the command-handling
  boundary, not inside either aggregate's domain code; dependency direction
  is `transporterprofile`/orchestration → `organizations`, never reversed.
  This is a more realistic "cross-aggregate invariant with saga/compensation"
  test than a single-aggregate version would give, since the two sides are
  real, separately-owned aggregates with different consistency models.
- **Temporal drives only the vetting workflow** (not general CRUD ops): two
  parallel saga branches — document review, GIT/insurance verification —
  gate `FleetAsset.AvailableForAssignment` and `TransporterProfile.Status`,
  with defined compensations (`CompensateRevertDocumentApprovals`,
  `CompensateDeactivateFleetAssets`) and a mock insurer activity supporting
  pass/fail/timeout outcomes for testing. This intra-aggregate saga is
  distinct from the cross-aggregate `Activate` guard above — both are
  worth reporting on separately. New infra: a Temporal server joins the
  compose stack.
- **Durability test is explicit**: kill the Temporal worker mid-workflow,
  restart, confirm resumption without re-asking already-satisfied signals.
- **Temporal/saga design reviewed in [ADR-047](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ADR-047-transporter-vetting-temporal-saga.md)
  — four required amendments for 38b, not optional hardening:** (1) every
  JetStream publish the workflow triggers must be an Activity with
  `Nats-Msg-Id` dedup keyed on `organizationID`+event+step-counter (never
  Temporal's `RunID`) and an explicit stream `Duplicates` window — this is
  genuinely new implementation, **not reuse of working code**, since Phase
  101 itself is 100% unimplemented in this repo today; (2) compensations
  are new, explicitly-named events (`DocumentApprovalReverted`,
  `FleetAvailabilityRevoked`), never a rewrite of prior events, or the
  audit trail that motivated event sourcing here is destroyed; (3)
  `WorkflowIDReusePolicy` must be chosen and verified against current
  Temporal SDK docs so `Resubmit` after `Rejected` actually starts a fresh
  run; (4) `StartToCloseTimeout`/`ScheduleToCloseTimeout` on
  `RequestGitVerification` must be explicit and environment-configurable —
  the SDK requires one to be set at all, and test vs. production timescales
  differ hugely. Workflow-versioning discipline for in-flight code changes
  is an accepted, named POC-scope gap, not addressed.
- **Two state machines, one guard**: `Organization`'s
  (`Registered → Active ⇄ Suspended`, BR-TP03–TP05) is untouched;
  `TransporterProfile` gets its own `AwaitingDocumentation →
  DocumentsInReview → {Vetted | Rejected}`, mapped directly onto V2's real
  4-axis status model (source-verified — `TransporterProfileStatus`,
  `accountInactive`, `underProbation`, `TransporterRegistrationStepType`),
  with `Resubmit` from `Rejected` starting a fresh workflow. **Deliberate
  improvement over V2**: V2 has no enforced transition guard between its 4
  vetting states (admin dropdown sets any value directly) — this design's
  Temporal state machine adds one.
- **Documents**: NATS Object Store (tenant-scoped bucket, same
  `{context}`-prefixed naming convention as KV), not S3/MinIO or V2's real
  choice (Google Cloud Storage) — tenant isolation comes free from the NATS
  account boundary, and it adds a second pattern comparison point.
  Reviewed in [ADR-048](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ADR-048-document-storage-nats-object-store.md),
  which affirms the choice but **rejects the "avoids a new infra dependency"
  rationale as incomplete**: Object Store bytes share the tenant's 1 GiB
  JetStream disk quota and 10-stream cap with the event log itself, so
  documents and event publishing now share a failure domain. Five required
  amendments: explicit bucket `MaxBytes` + per-file cap + a stream-budget
  check against `MaxStreams: 10`; object name uses a **service-minted UUID,
  not the user's filename** (which would make re-upload silently purge the
  prior document's bytes while the log still claims both); **blob written
  first, event second, never the reverse**; multi-document GIT status
  resolved against `compliance_documents`' `(organization_id, type)` PK;
  and a **new HTTP ingress**, since the service's only command surface is
  NATS `micro` (JSON, non-streaming, `max_payload`-bounded) after Phase 33.5
  deleted its REST routes — real 38c-ii work the design had assumed away.
- **Concurrency now needs two mechanisms**, a direct consequence of the
  shared-identity split: `TransporterProfile` draws on Phase 101's
  optimistic-concurrency design (`Nats-Expected-Last-Subject-Sequence`);
  `Organization`'s own identity-field edits (Company Information) need a
  plain `version`-column optimistic lock instead, since that aggregate has
  no event stream to guard against. Reviewed in
  [ADR-049](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ADR-049-cross-aggregate-concurrency.md),
  which affirms the split but corrects both halves: (1) the per-subject
  guard is **close to a no-op for `TransporterProfile`** — its event types
  are concurrent by design and land on different subjects, unlike Ship's
  naturally-serialising state machine, so the subject-guard shape is a
  **blocking 38a design decision** (wildcard-filter header vs. one subject
  per aggregate, the latter diverging from the platform subject taxonomy);
  (2) the "cross-aggregate invariant" is really a **one-time activation
  gate**, and since `GitStatus` is time-derived, `EXPIRED` breaks it with no
  command, actor, or event to guard on — needs an explicit choice between
  gate-only, a durable `evt.*` revocation consumer (never `notify.*`, which
  is deliberately lossy), or a Temporal durable timer; (3) sequence
  conflicts must not reach Temporal's compensation path, or two operators
  typing at once could revert document approvals; (4) the doc's claim that
  status transitions get "a natural check via `WHERE status = ?`" is
  **factually wrong** — the real mechanism is `SELECT … FOR UPDATE`, and the
  `version` column is still needed but for a different reason (row locks
  can't detect lost updates across a user's think-time); (5) Company
  Information is **not editable today at all**, so this needs a new
  `partner-update` command/handler/repository/domain method in
  `organizations` — which, with ADR-048's PK finding, means **ADR-046's
  "zero changes to `organizations`" is overstated** (correction note added
  there; the decision still holds, the changes are additive).
- **Save boundaries must align to the aggregate boundary** (ADR-049): the
  two-aggregate split is a backend seam only for *reads*; for *writes*, one
  submit spanning both aggregates can half-commit across two different
  conflict mechanisms, in a UI that has deliberately hidden which is which.
- **Operating Areas**: Leaflet + OpenStreetMap, two-level (Country → Region)
  overlay from a hand-authored GeoJSON; region list owned as a new
  refdata-service reference-data type (not hardcoded), seeded from V2's
  real corpus. **Corrected 2026-08-20** — this previously read "a
  reduced-depth version of V2's real hierarchical/polygon model." Checking
  the running V2 database (not just the Java source) showed `geo_areas` and
  `transporter_operating_areas` both hold **0 rows**: the polygon/GIS model
  was built and never populated. What V2 actually runs is a flat
  `region_entity` → `country_entity` two-level list with **48,041** live
  assignments — so Country → Region **matches production rather than
  reducing it**, and needs no simplification defence.
- **Tracking Credentials — confirmed divergence from V2**: V2 stores raw
  secrets (API keys, passwords) as plaintext Postgres columns with no
  encryption anywhere (source-verified). This design uses a NATS KV bucket
  with at-rest encryption instead, and — importantly — never publishes the
  secret payload onto the JetStream event stream (an event-sourced log
  shouldn't carry permanent secret material).
- **Rate Sheets — confirmed stub/placeholder tab for this phase.** V2's
  real shape is Customer-owned, read-only from the Transporter's side
  (source-verified); this phase excludes Customer. Rather than build a
  Transporter-owned flat table with no V2 analog, this phase adds the UI
  tab with an empty state only — no persistence, no domain model. Revisit
  with real fidelity if/when a Customer phase exists.
- **Rename last**: `organizations` naming used in new subjects from day
  one (cheap now, expensive to migrate later), but the actual
  service/package/UI-label rename is sub-phase 38e, after 38a–38d-ii ship and
  verify under the current name.
- **The four ADR-048/049 open questions are now resolved** (design doc §
  "Open Questions" 4–7): (4) subject-guard shape —
  `Nats-Expected-Last-Subject-Sequence-Subject`, confirmed supported by both
  the pinned `nats.go v1.52.0` client and `nats-server v2.14.5`, no subject
  taxonomy divergence needed; (5) activation gate vs. invariant — a
  `TransporterGitMonitorWorkflow` Temporal cron workflow re-checks
  `GitStatus` post-`Vetted` and calls `Organization.Suspend()` if it drops,
  subsuming the separate revocation-consumer option; (6) multi-document GIT
  status — `compliance_documents`' PK widens to
  `(organization_id, id)` with a service-minted document ID, the same ID
  used in the Object Store object name; (7) ADR-046's "zero changes to
  `organizations`" is corrected in place (both changes are additive, the
  decision itself is unaffected). Open question 1 (Operating Areas region
  list source) is **closed 2026-08-20** — sourced from the live V2 database;
  the seed and the three data-quality calls it forces are in
  `ARCHITECTURE-ORGANIZATIONS.md` § "Operating Areas — region seed".
  Question 2 (sub-phase numbering) is settled in practice —
  letters stay under one phase number, subdividing with roman numerals
  (38c-i/38c-ii) when scope demands. Question 3 (where `partner-update` +
  `version` land) is closed — 38c-i; see the note below the sub-phase list.

#### Sub-phases (design doc § "Naming & Sequencing")

- [x] **38a** — DONE 2026-08-20 (BR-TP18–BR-TP20). `TransporterProfile`
      domain package, event-sourcing
      skeleton (no Temporal yet): aggregate keyed by the shared
      `Organization` ID, commands, JetStream stream, Postgres projection,
      KV cache — every publish carrying
      `Nats-Expected-Last-Subject-Sequence-Subject` against the aggregate's
      wildcard filter (**resolved**, ADR-049 finding 1; confirmed supported
      by both the pinned `nats.go v1.52.0` and `nats-server v2.14.5`, no
      subject-taxonomy divergence needed). Also: idempotent
      `CreateTransporterProfile`/`EnsureTransporterProfile`, and the
      cross-aggregate `Activate` guard at the command-handling boundary,
      reading the Postgres projection (not KV) per ADR-049 finding 3. Per
      [ADR-046](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ADR-046-transporter-aggregate-split.md),
      `PartnerTypeTransporter` needs no change, no retirement — its broader
      "zero changes to `organizations`" claim is corrected (not
      retracted) by ADR-048/049's two additive changes, tracked separately
      under 38c-i (`compliance_documents` PK, `partner-update`), neither
      blocking this sub-phase's start.
- [x] **38b** — DONE 2026-08-21 (BR-TP21–BR-TP28). Was reopened 2026-08-21
      (originally marked DONE 2026-08-20) because **the saga was built and
      tested but never composed into the running service**; closed by the
      "38b (completion)" entry below, which wired it and verified it live. The workflow/activities/worker packages and their
      specs are done and unchanged; what was missing is every seam that
      connects them to the live stack. Temporal vetting
      workflow, GIT saga, compensations,
      durability test, **plus a `TransporterGitMonitorWorkflow` Temporal
      cron workflow** (resolves ADR-049 finding 2 / design doc "Open
      questions" 5): started once vetting reaches `Vetted`, periodically
      re-checks `GitStatus` and calls `Organization.Suspend()` if it drops
      — this is what earns the word "invariant" for the cross-aggregate
      gate, and subsumes the separate revocation-consumer option since one
      check catches both a saga compensation and time-derived `EXPIRED`.
      **Must satisfy [ADR-047](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ADR-047-transporter-vetting-temporal-saga.md)'s
      four required amendments** (publish-dedup Activities, explicitly-named
      compensating events, verified `WorkflowIDReusePolicy`, explicit
      Activity timeouts) before this sub-phase is considered done — these
      are correctness requirements the durability test itself would expose
      if skipped, not optional polish. **Plus ADR-049 finding 4**: a
      sequence conflict must surface as "try again," never reach the
      compensation path.
- [x] **38c-i** — DONE 2026-08-20 (BR-TP29–BR-TP36). `organizations` schema
      pass + editable Company Information. **Split out of a single 38c on 2026-08-20**: once 38c
      absorbed `partner-update` (open question 3) on top of Object Store,
      the PK widening, and a new HTTP ingress, it carried two unrelated
      red→green cycles — a pure-Postgres/RPC one and a binary-transport
      one. Splitting keeps each to one business-rules pass. **Both
      `organizations` schema changes land here, in one migration pass**,
      so 38c-ii never touches the schema:
      - The `compliance_documents` PK widens to
        `(organization_id, id)` with a service-minted document ID
        (resolves ADR-048 finding 2c / design doc "Open questions" 6) —
        `document-add` becomes an insert, superseding a document becomes an
        explicit transition. Landed here rather than with the blob work
        because it is a plain schema/CRUD change, and because the same ID
        becomes the Object Store object name's `{documentID}` token that
        38c-ii depends on.
      - The `version` column plus `partner-update` (decided 2026-08-20,
        resolves design-doc "Open questions" 3). Editing Company
        Information is a confirmed requirement, so the read-only fallback
        is off the table. Scope, all of it absent today (verified): a
        details-mutator domain method on `Organization`; a matching
        repository method (the port exposes only
        `Register`/`Get`/`List`/`Activate`/`Suspend`/`Reactivate`); a
        `partner.update` handler, which makes it the **15th** `api.*`
        endpoint (`browserrpc_test.go`'s "advertises all 14 api.*
        endpoints" spec must be updated in the same task); the `version`
        column with `UPDATE … WHERE id = ? AND version = ?` and a 409
        surfaced through `browserrpc`; and **`Register` widening** to
        accept `companyName`/`registrationNo`/`vatRegistrationNo`/`tradingAs`
        optionally, since `registerRequest` currently accepts only
        `{Name, Type}` and *no code path writes those columns at all*
        today. Widening `Register` rather than having 38d-i's wizard do
        register-then-update deliberately avoids introducing the very
        half-commit shape ADR-049 finding 6 warns about.

      Needs its own business-rules-first pass (BR-TP29+) including a spec
      for the lost-update-across-think-time case a pessimistic row lock
      structurally cannot catch (ADR-049 finding 5a). Conflict
      *presentation* is deliberately NOT decided here — that stays 38d-i's
      call.
- [x] **38d-i** — DONE 2026-08-20 (BR-TP37–BR-TP39). Transporter UI: dedicated
      component, registration wizard, tabbed detail view, state-transition
      stepper, Company Information editing. **Split from a single 38d on
      2026-08-20**, and **not purely frontend** — see the correction note below.
      **As built:** a dedicated `TransporterPanel.vue` (Shippers stay on
      `OrganizationsPanel.vue`) with a **drill-in** detail view rather than a
      table expansion row — five tabs, an editable form and a vetting stepper do
      not fit legibly in a nested row. Registration is a 3-step wizard that
      **commits at step 1** (fleet assets and documents need an existing id),
      which the wizard states rather than hiding.
      Two things found while building, both recorded because neither was
      predicted by the design: (1) the browser's NATS request helper discarded
      the error envelope's `conflict`/`notFound` flags, so **BR-TP39's banner
      had nothing to trigger on** and a 409 was indistinguishable from a 500 in
      every frontend in this repo — fixed in
      `frontend/refdata/src/nats/connectionFactory.js` with specs proven to fail
      without it; (2) **no `vehicle-type` corpus is seeded in this dev stack**,
      so BR-TP14 rejects every fleet-asset add. That is pre-existing (the
      shared panel had it too) and out of this sub-phase's scope — the one-off
      `refdata-service/cmd/seed-vehicle-types` seeder targets context
      `linebooker` over the REST surface Phase 33 retired, so making fleet
      assets work end-to-end is its own small task, not a 38d-i fix.
      Verified live against the composed stack: wizard → drill-in, GIT badge
      re-deriving `None` → `Pending` → `Active` as documents changed, the
      BR-TP39 banner on a real 409 (row bumped out-of-band mid-edit) with typed
      values preserved, and Overwrite winning at v3. Carries one small backend
      task first: a **`partner.profile.get` `api.*` endpoint (the 16th)**
      wrapping the `CanonicalProjectionReader` 38a already built, plus the
      derived **GIT status** (5 values, computed worst-of-documents per the
      design's "Data sections", never hand-set). Without that endpoint the
      browser has no route to vetting state at all and 38b's entire Temporal
      saga is invisible to any UI. Per-section save boundaries aligned to the
      aggregate boundary, conflict UI naming the losing section (ADR-049
      finding 6). **Conflict presentation decided 2026-08-20** (closing the
      last open half of design question 3): an **inline banner on the
      affected section that keeps the operator's typed values** and offers
      Reload (discard mine) or Overwrite (re-read the version, reapply
      mine) — nothing is discarded without an explicit choice, since
      silently reloading over an operator's edits is the very loss BR-TP34
      exists to prevent.
      **Runs before 38c-ii, so the Documents tab has no binary transport
      underneath it yet.** Ship that tab against 38c-i's
      `compliance_documents` metadata only — rows, type, status,
      service-minted document ID, and the review transitions — with
      upload/download explicitly deferred, not faked. Do not stub an upload
      that silently discards the file; a visibly disabled or "coming in
      38c-ii" affordance is honest, a no-op file input is a demo trap.
      Rate Sheets ships as the design's stub/empty-state tab (no backend by
      design).
- [x] **38c-ii** — DONE 2026-08-20 (BR-TP40–BR-TP45). NATS Object Store
      document upload/download. **Ran after
      38d-i** (ordering decided 2026-08-20). Depends on 38c-i's
      document ID (the object name's `{documentID}` token), and completes
      38d-i's Documents tab by wiring real upload/download to the controls
      38d-i deliberately left inert. **Must satisfy
      [ADR-048](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ADR-048-document-storage-nats-object-store.md)'s
      four remaining required amendments** — explicit bucket/file size
      limits + stream budget check, UUID-based object names (no user
      filenames), blob-then-event write order, and a dedicated HTTP
      ingress (a scoped, deliberate partial reversal of Phase 33.5's REST
      retirement — budget it, the service has no binary path today).
      Its own business-rules pass, numbered after whatever 38c-i and 38d-i
      consume. Carries a small frontend tail (the deferred upload/download
      controls), so it is not a backend-only sub-phase despite its name.
      **As built:** all four ADR-048 amendments discharged. The one that was
      *not* a wiring task is the ingress's auth — ADR-048 said "own auth", but
      **no JWT verification exists anywhere in this repo** (accounts-service
      only mints NATS credentials; every other caller authenticates by
      connecting to an account). Rather than build a second authentication
      system for two byte routes, BR-TP41 mints a **single-use, 2-minute
      capability ticket** over the authenticated NATS connection, and the HTTP
      call carries only that — the tenancy decision stays server-side on the
      account boundary. Also as built: bytes are **write-once** (BR-TP43),
      with supersede-and-replace as the correction path, since overwriting an
      object would purge bytes the immutable log still references; the raw
      request body is the transport, not `multipart/form-data`, there being
      exactly one field; and `nginx.conf` needed `client_max_body_size 10m`,
      as nginx's 1 MiB default would have returned a 413 the service never saw.
      **Stream-budget check (ADR-048's precondition) done against the running
      stack:** ACME 5→6 streams of 10, GLOBEX 3→4 — it fits, no `/jslimits`
      raise needed, and the ADR's worry about refdata's versioned KV buckets
      was misplaced (those live in PLATFORM, limit 20). Verified live: real
      upload with a non-ASCII filename, byte-identical download, ticket replay
      → 403, second upload → `conflict: true`, 11 MiB → 413 leaving the
      document file-less and a 10 MiB orphan object in the bucket — BR-TP43's
      deliberate trade, observed rather than asserted.
- [x] **38d-ii** — DONE 2026-08-20 (BR-D46–BR-D48, BR-TP46–BR-TP55).
      Operating Areas + Tracking Credentials, backend and frontend, verified
      in-browser against the composed stack. Split out because
      **neither has any backend at all** (verified 2026-08-20: no
      `OperatingArea` persistence, no region corpus, no `organizations-secrets`
      KV command — these were never built by 38a/38b/38c-i). Each is a new
      data section needing persistence, not a frontend gap: Operating Areas
      needs the refdata-owned Country -> Region corpus (**sourced 2026-08-20
      from the live V2 database — open question 1 is closed**; 33 regions
      across ZA/BW/NA, see `ARCHITECTURE-ORGANIZATIONS.md` § "Operating
      Areas — region seed"), plus the `TransporterOperatingArea(transporterId,
      regionCode, level)` join and the Leaflet/GeoJSON map; Tracking
      Credentials needs the at-rest-encrypted KV bucket and a command that
      never publishes the secret onto the event log. Splitting was also what
      stopped open question 1 from blocking the rest of the Transporter UI;
      with it now closed, **38d-ii is unblocked** and the split stands on
      its own scope grounds.

      **Rules pass done 2026-08-20** (13 rules, two suites). Operating Areas:
      BR-D46–BR-D48 add a refdata `region` corpus and the Country → Region
      hierarchy; BR-TP46–BR-TP50 add assignment, corpus validation, overlap
      rejection, and freely-editable-post-`Vetted` coverage. Tracking
      Credentials: BR-TP51–BR-TP55 keep the secret in an at-rest-encrypted KV
      bucket and off the event log, make credentials overwritable where
      documents are write-once (BR-TP43), and extend
      `AvailableForAssignment` to require configured credentials —
      **BR-TP55 is the only rule that touches working 38b code.**

      Two findings from the rules pass worth carrying into implementation:

      - **No refdata schema change is needed.** The `country` type is already
        seeded (52 items, localized `en`/`af-za`/`es`) and
        `DictionaryReference` already models a typed item-to-item relation
        with BR-D05's integrity guards, so Country → Region needs no new
        column. But `dictionary_references` holds **0 rows** — BR-D47 is its
        first production use, so treat BR-D05 as unproven against real data.
        And only `ZA` of the three seeded countries exists; `BW`/`NA` must be
        added by the same seed.
      - **The map overlay is already built** —
        `frontend/refdata/public/geo/operating-areas.geojson`, 32 features
        (ZA 9 / BW 10 / NA 13) from geoBoundaries gbOpen ADM1, with source
        defects corrected (Angola's `Cunene` removed from the Namibia file,
        SA's `Nothern Cape` typo and non-ISO codes remapped,
        `Caprivi`→`Zambezi`, `Karas`→`ǁKaras`). **Namibia ships 13 regions
        with Kavango undivided under the retired `NA-OK` code** — the
        post-2013 split geometry was not obtainable, V2's own corpus also
        carries a single `Kavango Region`, and fabricating a border is worse
        than an honest retired code; reversible later, since the corpus and
        the GeoJSON are joined only by ISO code.
- [x] **38e** — DONE 2026-08-20. `organizations` rename (service, packages,
      subjects, UI labels). **Amended 2026-08-21:** the `api.*` entity token
      `partner` was renamed to `organization`
      (`api.*.organizations.organization.<verb>.v1`, 9 subjects) and the
      `Partner*Subject` constants to `Organization*Subject`. 38e had renamed
      the service token but left the entity token on the very term the phase
      existed to retire, in the one place that is a public contract. The
      `{service}.{entity}` stutter is accepted deliberately: every other
      layer already reads `organization`, and the taxonomy's rule is that the
      entity token is the singular entity type.
      **Amended 2026-08-21 (storage naming + Streams rail):** the Object
      Store bucket was reverted from `ORGANISATION_DOCS` to
      `organizations-docs` (British spelling, singular and SCREAMING_SNAKE
      all three broke the convention), its two live objects migrated with
      `nats obj get|put` and SHA-256-verified, and the naming rule written
      down for the first time — **streams `SCREAMING_SNAKE`, KV buckets and
      Object Stores `lowercase-kebab`** — in `CLAUDE.md` and
      `ARCHITECTURE-COMMUNICATIONS.md` §2.6, alongside §2.5's
      singular/plural-by-layer table. The Admin UI Streams rail follows:
      `/api/jetstream/streams` stopped dropping `KV_*` and now tags every
      row `kind: stream|kv|objstore`, so the rail can answer ADR-048's
      "how much of `MaxStreams: 10` is spent" — kv/objstore are opt-in
      chips, off by default, with a name filter, prefix stripping, and
      kind tags reusing `TraceWaterfall.vue`'s `.kind-tag` vocabulary.
      Mockup: `diagrams/admin-streams-rail-mockup.html`.
- [x] **38b (completion)** — DONE 2026-08-21. Tracked as
      finishing 38b rather than as new scope: **the decision 2026-08-21 was
      that this is 38b's unclosed remainder, not a new sub-phase.** 38b is
      accordingly reopened from DONE. **Compose the Temporal worker into
      `organizations-service` and give vetting a live entry point.** 38b built `transporterprofile/{workflow,worker,
      activities}` and tested them thoroughly, but **nothing in the running
      service ever composes them** — verified 2026-08-21:
      `transporterprofile.Start()` wires only the event store, projector,
      Postgres projection and KV cache; `VettingService` has no caller
      outside its own file and tests; and `temporal task-queue describe
      --task-queue organizations-vetting` reports **zero pollers** against
      the live stack. The saga is code that has never run outside a test.
      This is an unclosed gap in 38b, not a deferral — 38b's own design
      section calls for a durability test that "kill[s] the Temporal worker
      mid-workflow", which presumes a worker exists to kill (BR-TP27).

      Consequences today, all observed rather than assumed: every
      `TRANSPORTER` profile is created in `AwaitingDocumentation`
      (`adapter.go` `handlePartnerRegister`) and can never leave it; and
      because **BR-TP19**'s activation gate is live on `partner.activate`,
      no transporter can reach `Active` or, therefore, `Suspended` either.
      Three lifecycle states are unreachable in the composed stack.

      #### Design decisions

      - **D1 — One worker per service, not per tenant; activities resolve
        the tenant from `Event.Context`.** `TaskQueue` is the constant
        `organizations-vetting`, so a worker per tenant account would put
        several pollers on one queue and let them steal each other's tasks
        — an activity would then publish one tenant's events over another
        tenant's NATS connection. The fix is to make
        `ProfileActivities`' `WorkflowEventPublisher` tenant-resolving:
        it looks the tenant up through the existing `tenants.Manager`
        using the `Context` already carried on every `VettingInput`,
        `GitVerificationInput` and `ProfileEventInput`. **Alternative
        considered and rejected:** a per-tenant task queue
        (`organizations-vetting-{context}`) keeps the publisher
        tenant-bound at construction, but makes `TaskQueue` dynamic,
        multiplies workers by tenant count, and puts the tenant axis into
        Temporal's routing where the rest of the system keeps it on the
        NATS account boundary.
      - **D1a — the workflow inputs must carry `Tenant`, not just
        `Context`.** Found while wiring 2026-08-21: `tenants.Manager` and
        every existing resolver on it (`DocumentStore`, `SecretStore`,
        `ProfileCommands`) are keyed by **tenant**, while 38b's
        `VettingInput` / `ProfileEventInput` / `GitVerificationInput` carry
        only `Context`. Context to tenant is **many-to-one** — `acme` and
        `acme-northdiv` are two business units inside one NATS account — so
        the tenant cannot be derived from the context an activity is handed.
        Both axes are genuinely needed: the event *subject* is built from
        `{context}`, while the *connection* it publishes over is the
        tenant's. `Tenant` is therefore added to those inputs. Safe to
        change now precisely because no workflow has ever run — there is no
        in-flight history to break determinism for; it would be a migration
        after this lands.
      - **D2 — Vetting is submitted explicitly, by a new command.** New
        subject `api.*.organizations.organization.submit-vetting.v1`,
        which starts `TransporterVettingWorkflow` with
        `RequiredDocumentReferences` set to the transporter's
        currently-pending compliance document IDs (**BR-TP36**: the
        document ID *is* the vetting reference). Explicit submission is
        symmetric with **BR-TP26**'s existing explicit resubmit, and an
        implicit "start once N documents exist" trigger would need a rule
        about what N is that no V2 behaviour supports. Workflow ID reuses
        `worker.workflowID(context, organizationID)` so resubmission keeps
        landing on the same ID.
      - **D3 — `document.approve` / `document.reject` also signal the
        running workflow.** **BR-TP23** makes document review
        signal-driven per reference, but the live approve/reject commands
        only write the `compliance_documents` row — nothing sends
        `DocumentReview`. The command keeps writing the row unchanged and
        then signals; if no workflow is running for that transporter the
        signal is skipped, since reviewing a document outside a vetting
        attempt stays legal. **The tension worth naming:** review state
        then exists in two places — the document row and the workflow's
        `DocumentReviews` map — and they are reconciled only by both
        being driven from the same command. A single source of truth
        would mean the workflow owning document rows outright, which is a
        larger change to 38c-i's schema than this sub-phase should make.
      - **D4 — GIT verification uses the existing `MockGitVerifier`.**
        `activities.MockGitVerifier` already supports `pass`/`fail`/
        `timeout`; there is no real GIT/insurance system in the lab.
        Outcome comes from an env var (default `pass`) so the failure and
        compensation branches (**BR-TP22**) stay reachable by hand.
      - **D5 — No change to BR-TP19.** The activation gate is already
        correct; it only ever looked unreachable because nothing could
        produce a `Vetted` profile.

      #### Proposed business rules (BR-TP56–BR-TP58)

      Numbers follow BR-TP55, the last rule 38d-ii landed. Wording is
      drafted, not settled — these need the standing business-rules-first
      confirmation before any spec is written.

      - **BR-TP56 (submit for vetting):** Submitting is legal only for a
        `TRANSPORTER` whose profile is `AwaitingDocumentation` or
        `Rejected`, and only when it has at least one pending compliance
        document; any other status or an empty document set is rejected
        without starting a workflow. The started workflow's required
        references are exactly the pending document IDs at submit time.
      - **BR-TP57 (review signals reach the workflow):** Approving or
        rejecting a compliance document sends one `DocumentReview` signal
        carrying that document's ID and outcome to the transporter's
        vetting workflow, if one is running. A document reviewed with no
        workflow running updates its own row and nothing else. Signalling
        is best-effort in the same sense as BR-TP06's audit writes: a
        failed signal is logged and never rolls back the review.
      - **BR-TP58 (one worker; activities are tenant-resolved, never
        tenant-bound):** The service starts exactly one Temporal worker
        polling `organizations-vetting`, and every activity resolves the
        NATS connection it publishes over from the `Tenant` on its own
        input, through `tenants.Manager`, rather than from a connection
        bound at construction. An activity naming a tenant this service has
        no credential for fails with `ErrTenantNotConnected` and appends
        nothing — it never falls back to another tenant's connection.
        *Reframed 2026-08-21 from "the worker runs in the composed stack",
        which stated a deployment fact rather than a rule with a failure
        mode worth testing. The isolation guarantee is the part that can
        regress silently: a single constant task queue plus a
        construction-bound publisher is exactly the shape that would let one
        tenant's events be published over another's connection.*

      #### Progress

      **Worker wiring DONE 2026-08-21 (BR-TP58).** `MountVetting` in
      `organizations/composition.go` dials Temporal and starts one worker;
      `cmd/main.go` calls it as a fourth pass after `MountAPI` (it needs the
      connections `MountTenants` opened); `tenants.Manager` gained a
      `ProfileEvents` resolver matching the existing
      `DocumentStore`/`SecretStore`/`ProfileCommands` shape;
      `ProfileActivities` now takes a `PublisherResolver` instead of a bound
      publisher; `Tenant` was added to the four workflow inputs; and
      `transporterprofile.Runtime` retains its event store. Compose gains
      `TEMPORAL_ADDRESS`, `ORGANIZATIONS_GIT_OUTCOME` and a `temporal`
      dependency. 4 new specs (tenant routing, cross-tenant isolation,
      unknown tenant appends nothing, and a structural guard against
      reintroducing a bound publisher); all 10 suites green. Verified live:
      `organizations-vetting` now reports **pollers on both workflow and
      activity types** where it reported none.

      **Two things this uncovered, both worth keeping:**
      1. **The production build was broken and nothing said so.** The first
         container build failed on `missing go.sum entry` for every
         `go.temporal.io/*` package — the SDK had only ever been reachable
         from test files, so the main module's build closure never needed
         those sums. That is independent evidence the saga was never part of
         the shipped binary. Fixing it required an explicit
         `google.golang.org/genproto` require: `go-grpc-middleware` v1.4.0
         pulls the 2020 monolith, which still carries `googleapis/{api,rpc}`
         and makes every import of them ambiguous against the split modules.
         **`go mod tidy` removes that require and reintroduces the failure** —
         the go.mod comment says so.
      2. **`docker compose up -d --build <svc>` exits 0 when the build
         fails**, leaving the previous image running. It cost a full
         debugging cycle here: the service looked deployed, logged normally,
         and silently ran code from 48 minutes earlier. Prefer
         `docker compose build <svc>` as a separate step when verifying that
         a change actually reached the stack.

      **BR-TP56 + BR-TP57 DONE 2026-08-21.** New subject
      `api.*.organizations.organization.submit-vetting.v1` (24th endpoint —
      the `$SRV.INFO` census spec caught the change, as designed);
      `VettingService.Submit` guards on type, profile status and pending
      documents, and **routes a `Rejected` profile into BR-TP26's Resubmit**
      rather than starting fresh, so the two paths can never both start a
      workflow for one transporter. `document.approve`/`reject` now signal
      after the row write, `resubmit` deliberately does not, and a transition
      the domain refused signals nothing — signalling a refused approval
      would advance the attempt on a document that never moved. 16 new specs
      across the worker facade and the api.* boundary; all 10 suites green.

      **A latent panic this work surfaced:** `Adapter.log` is nil whenever
      `Deps.Log` is unset, and BR-TP57's failure path was the first code to
      dereference it directly — every other use goes through
      `ReplyWithConflicts`, which nil-checks internally. Found by the new
      signal-failure spec, not by inspection. Guarded.

      **Verified live, end to end, for the first time in the project:**
      register TRANSPORTER → add GOODS_IN_TRANSIT document → submit → profile
      moves `AwaitingDocumentation` → `DocumentsInReview` with the document
      tracked as `PendingReview` → approve → **`Vetted`, `gitVerified: true`,
      `fleetAvailabilityGate: true`** → `Activate` succeeds through BR-TP19's
      gate → `Suspend` succeeds. Temporal shows the workflow `Completed`. All
      three guards return 409 with their own messages (SHIPPER, no pending
      document, already-Vetted).

      **BR-TP28's GIT monitor is now wired too** (this was the last open
      piece). `HandleGitStatusDrop` no longer binds one (tenant, context)
      pair at construction: `Manager.ProfileStore(tenant)` resolves the
      event store per tenant, and a `gitMonitor` in `composition.go`
      implements `GitStatusDrop`/`IsGitActive`/`ScheduleGitMonitor`
      (`IsGitActive` reuses `domain.DeriveGitStatus`, so there is no second
      definition of "active"). The vetting workflow executes
      `ScheduleGitMonitorActivity` immediately after `VettedEvent`, creating
      a Temporal Schedule `<tenant>-transporter-git-monitor-<id>` at
      `ORGANIZATIONS_GIT_MONITOR_INTERVAL` (5m in compose). **Bug found and
      fixed while verifying:** `GitMonitorScheduleOptions` never set
      `Tenant` in the schedule args, so the live monitor failed with
      "tenant is not connected" — the schedule args are now asserted in the
      spec, not just the workflow input. Verified live: schedule created,
      the drop path flipped `fleetAvailabilityGate` to false and set the
      organization `SUSPENDED`, and three monitor runs all `Completed`
      without double-suspending (idempotent).

      **BR-TP27's durability test passed:** killed
      `lb-organizations-service` with 1 of 2 documents approved, restarted,
      approved only the second → `Vetted`, gate true, both references
      recorded `Approved`. The workflow resumed without re-asking.

      **One pre-existing thing noticed while wiring, not introduced here:**
      `organizations.transporter_profiles` is keyed on `organization_id`
      alone with no tenant column, so one projection reader serves every
      tenant. Profile isolation lives at the NATS/event layer, not in this
      table — 38a behaviour, recorded because the shared reader in
      `Vetting.factory` looks like a wiring shortcut and is not one.

      #### Verification

      BR-TP27's durability test becomes runnable for the first time and is
      part of this sub-phase, not a follow-up: start a vetting attempt,
      approve one of two documents, restart the worker, confirm the
      workflow resumes without re-asking for the already-approved
      reference.

- [x] **38g** — DONE 2026-08-21; depended on 38b's completion above.
      **As built:** `cmd/seed-transporters` (+ `main_test.go`) drives the
      ten-rung ladder over the public API and the real Temporal saga —
      bare → company info → areas → fleet → docs pending → docs rejected →
      vetted → tracked → active → suspended. A full `-n 10` run against
      `acme` completed and was verified in Postgres (area/fleet/document/
      credential counts per rung). Notes worth keeping: `checkPrerequisites`
      probes **every** corpus code it will use, not just the first (an
      earlier draft invented `FLAT_DECK`/`TRUCK`, exactly the failure the
      design warned about) and carries the underlying error through rather
      than guessing a cause; `registrationNo` is globally unique, so each
      run mints a per-run token via `plate()` — **the seeder is re-runnable
      but not idempotent**, `register` creates new organizations each run.
      Four structural tests cover the ladder's cumulative shape, distinct
      labels, and the review/suspend rung preconditions.
      **`seed-transporters` — deterministic test data at varying vetting
      completeness.** A third seeder alongside `refdata-service`'s existing
      `cmd/seed-regions` and `cmd/seed-vehicle-types`, following their
      established shape: `go run ./cmd/seed-transporters -n 10`, talking to
      the running service over its public API rather than Postgres
      directly, so the seed traverses the same validation a human operator
      would. Unlike those two it speaks **NATS `api.*` request/reply with a
      tenant `.creds` file, not REST** — **BR-TP16** reduced this service's
      REST surface to infra health only. Seeds into `acme` (flagged,
      defaulting there).

      #### Design decisions

      - **Completeness is a deterministic ladder, not randomness** — one
        rung per transporter, named for its state, so a re-run reproduces
        identical data and any row's state is readable from its name.
        `-n` above 10 cycles the ladder with a counter suffix; below 10
        truncates it. Every step tolerates its "already there" response,
        so the seeder is re-runnable.

        | # | Rung | Rules exercised |
        | --- | --- | --- |
        | 1 | bare — name/type/context only | BR-TP02 |
        | 2 | + company information | BR-TP32–35 |
        | 3 | + operating areas | BR-TP46–50 |
        | 4 | + fleet assets | BR-TP12–14 |
        | 5 | + documents, pending review | BR-TP29–31 |
        | 6 | one document rejected | BR-TP23, BR-TP57 |
        | 7 | vetted | BR-TP21, BR-TP56 |
        | 8 | + tracking credentials | BR-TP51–55 |
        | 9 | active | BR-TP03, BR-TP19 |
        | 10 | suspended, with reason | BR-TP04 |

      - **Rungs 7, 9 and 10 are why this depends on 38b's completion.** Rung 8 does
        not — **BR-TP51** gates tracking credentials on `TRANSPORTER` type
        alone, not on `Vetted`.
      - **The vetted rungs drive the real saga** (decided 2026-08-21):
        the seeder submits for vetting and waits for the workflow, rather
        than appending profile events directly. Appending directly would
        be faster and need no worker, but the resulting rows would never
        have traversed the path they represent and could diverge from
        what the UI shows for a genuinely vetted transporter.
      - **Compliance documents are seeded as records without file bytes**
        (decided 2026-08-21). Enough for the review, vetting and list
        surfaces; skipping the upload-ticket flow (**BR-TP40–45**) keeps
        the seeder simple and keeps seed runs out of the
        `organizations-docs` Object Store, which counts against the
        tenant's `MaxStreams` budget (ADR-048).
      - **Corpus-drift test**, following `seed-regions`' `main_test.go`
        precedent: the vehicle type codes and region codes the seeder
        uses must exist in refdata's corpus, or the seeded rungs fail
        halfway with nothing to explain why.

      #### Feasibility walk-through, 2026-08-21 (no seeder written)

      Every rung driven by hand over `acme.creds` against the composed
      stack. **All ten are reachable**, and the resubmit path completes to
      `Vetted` on attempt 2. Three prerequisites the seeder must handle, all
      found this way rather than by reading:

      1. **Rungs 3 and 4 need the two refdata seeders to have run first.**
         `region` and `vehicle-type` are not part of refdata's base seed —
         a fresh volume has countries/currencies/incoterms/uom but neither
         of these, so operating-area add fails with "code does not exist in
         the refdata corpus" and fleet-asset add with "vehicle type code is
         not recognized". `go run ./cmd/seed-regions` and
         `./cmd/seed-vehicle-types` fix both. The seeder should either
         depend on them explicitly or check and say so.
      2. **`vehicle-type` is seeded into context `linebooker`, not
         `_platform`** (unlike `region`, which BR-D46 puts in `_platform`).
         BR-TP14's validation resolves in the *organization's own* context,
         so a fleet asset could only be added to an organization registered
         in `linebooker`. **Resolved 2026-08-21: the corpus is now seeded
         into `acme`** (`go run ./cmd/seed-vehicle-types -context acme`), so
         the ladder stays in `acme` as decided. Two fixes were needed to make
         that work: `registerContext` hardcoded `name: "Linebooker"` and
         `tenant: linebooker` regardless of `-context`, which would have
         stamped Linebooker's identity onto any other context — it now
         derives both, with a new `-tenant` flag defaulting to the context
         name (BR-AC07's common case). There was also **no refdata context
         named `acme` at all** before this — only `acme-default`,
         `acme-atlantic-fleet`, `acme-pacific-fleet` — and since Phase 106's
         context inheritance is not on the live read path, the lookup is
         exact-context, so `acme` had to be created rather than inherited
         from. Verified: `TAUTLINER` now resolves for an `acme` organization.
      3. **Codes are the real V2 corpus**, e.g. `TAUTLINER`,
         `TAUTLINER_SUPERLINK_STANDARD_6_BY_12` — 114 of them. The seeder
         must pick from the corpus, not invent plausible ones (`TRUCK` is
         not a code).

      **Latent defect found and FIXED 2026-08-21 — the interim status of a
      resubmitted attempt.** `TransporterProfile.Resubmit()` built its event
      as `p.event(VettingResubmittedEvent, attempt+1, p.state.Status)`,
      carrying the *old* status `Rejected`. The workflow skips its own
      `VettingStarted` append when `Resubmitted` is true, so nothing ever set
      `DocumentsInReview` and a profile with a live attempt running read as
      `Rejected` until it finished. Now carries `StatusDocumentsInReview`;
      see BR-TP26's amendment for the full reasoning.

      **The first spec written for it was vacuous and passed with the bug
      restored** — caught only by deliberately reverting the fix and re-running.
      The cause is a genuine structural split worth remembering: `Apply()`
      already hardcodes `StatusDocumentsInReview` for
      `VettingResubmittedEvent`, so the *aggregate* was never wrong, while the
      projector does not replay `Apply` — it copies `Status` straight off the
      event payload (and already special-cases `CreatedEvent` for the same
      reason). Two implementations of "what does this event mean for state",
      and only the payload one was broken. The spec now asserts the appended
      event, with that reasoning written into it so it does not get
      "simplified" back. Verified live: attempt 2 reads `DocumentsInReview`
      while Temporal shows the run `Running`.
- [x] **38h-i** — DONE 2026-08-21 (BR-TP59). **Make a compliance
      document's expiry settable through the `api.*` surface.** Sequenced
      **before** 38h-ii and split out from it on the user's call
      2026-08-21: `expiresAt` exists on the domain type and the
      `compliance_documents` column, but no API accepts it. The expiry
      driving the 2026-08-21 walkthrough had to be written with SQL, and
      the `expires_at` scan defect fixed that day (TIMESTAMPTZ scanned
      into `*int64`, broken in both directions) had survived precisely
      because nothing in the running system ever wrote the column. 38h-ii's
      timer has nothing to arm against until this lands.

      #### Design decisions

      - **On document add, and on a dedicated update — not on approve.**
        An expiry is a fact about the document, supplied when it is
        registered; folding it into the review transition would conflate
        "this cover runs to date X" with "a human accepted it".
      - **Optional, and validated as future-dated on write.** A GIT
        document with no expiry cannot lapse by time (38h-ii's D5). A
        past-dated expiry on a *new* document is a data-entry error, not a
        lapse, and is rejected rather than silently arming an
        already-expired timer.
      - **Unit stays Unix seconds on the wire**, matching every other
        instant in this domain (`DocumentFile.UploadedAt`), with
        `expiryParam`/`scanDocument` continuing to bridge to TIMESTAMPTZ.

      #### Business rule (approved 2026-08-21)

      - **BR-TP59** — a compliance document may carry an optional expiry,
        set at registration or by a later update; when set it must be in
        the future at the time of writing; changing it is what re-arms
        BR-TP61's timer.

      **As built.** `domain.ComplianceDocument.SetExpiry(expiresAt, now)` is
      the single write point, called by both the add path and
      `postgres.SetDocumentExpiry`, so the future-dating rule cannot hold on
      one route and be forgotten on the other; the repository applies it
      against the row it holds `FOR UPDATE` rather than the caller's copy.
      New subject `api.*.organizations.document.set-expiry.v1`;
      `document.add.v1` gains an optional `expiresAt`, a pointer on the wire
      so "not supplied" stays distinct from "cleared". 10 new specs plus the
      `api.*` round trip; whole suite green. Verified live over `acme.creds`
      — added with an expiry, moved forward, past date refused, cleared to
      NULL in Postgres.

      Two notes worth carrying forward. **`AddDocument`'s domain signature
      was left alone** and the expiry applied through `SetExpiry` after
      construction, rather than threading a fifth parameter through six call
      sites and the seeder — one validation path, less churn. And the past-
      date refusal returns **500 rather than 400**: not new to this rule, but
      newly annoying, since `shared/browserrpc` classifies only 404 and 409
      specially and every input-validation error on this surface has always
      come back as 500. Fixing it properly means adding a 400 class and
      classifying every validation error at once — deliberately out of scope
      here rather than making this one rule inconsistent with its
      neighbours.

- [x] **38h-ii** — DONE 2026-08-21 (BR-TP60–BR-TP63). **Replace
      BR-TP28's polling Schedule with a durable expiry timer.** BR-TP28 as built
      creates a Temporal Schedule per vetted transporter firing every
      `ORGANIZATIONS_GIT_MONITOR_INTERVAL` (5m in compose), each firing a
      separate `TransporterGitMonitorWorkflow` execution whose entire body
      is one `HandleGitStatusDrop` activity. Measured on the live stack
      2026-08-21: **184 monitor executions against 16 vetting workflows**,
      from 8 schedules — roughly 2,300 executions/day at 8 transporters,
      growing linearly with the fleet and unbounded in time.

      Three problems, all observed rather than assumed:

      1. **It polls for an event whose time is already known.**
         `compliance_documents.expires_at` is a stored timestamp. Asking
         288 times a day whether a date has passed is work Temporal's
         durable timers exist to avoid — and a scheduled poll is a poor
         demonstration of Temporal, since cron does it just as well.
      2. **Nothing ever deletes or pauses a schedule.** Verified by grep:
         there is no `Delete`/`Pause` call anywhere in
         `transporterprofile/`. A suspended transporter keeps polling
         forever, and a schedule outlives the tenant assumptions it was
         created under — the live logs still carry
         `HandleGitStatusDrop ... Error="tenant is not connected"` for org
         `7730206f-5095-4f8d-b3de-b0a175080779`, a leftover from before
         D1a's tenant fix.
      3. **The workflow list is unusable at this ratio.** One row per
         transporter per interval, permanently, drowning the vetting
         workflows an operator actually wants to see.

      #### Design decisions

      - **D1 — one long-lived `TransporterGitCoverWorkflow` per
        transporter, replacing the Schedule.** It computes the earliest
        `expires_at` across the organization's current GIT documents,
        `workflow.Sleep`s until that instant, then runs the *existing*
        `HandleGitStatusDrop` activity. `HandleGitStatusDrop`,
        `DeriveGitStatus` and BR-TP28's two effects
        (`FleetAvailabilityRevoked` then `Organization.Suspend()`) are
        unchanged — this sub-phase replaces **when** the check runs, not
        what it does, so BR-TP28's existing specs stay green.
      - **D2 — a `CoverChanged` signal resets the timer.** A document can
        be superseded (BR-TP30) or re-dated after the timer is armed, so
        the workflow blocks on a `workflow.Selector` over the timer and
        the signal channel rather than a bare `Sleep`. The signal is sent
        from the same place that already knows: the document write path in
        `orchestration`, on add/supersede/approve of a
        `DocumentTypeGoodsInTransit`. **Alternative considered and
        rejected:** having the projector watch for the profile event
        instead — it would put a Temporal client in the projector purely
        to re-drive a timer, and the document repository already has the
        expiry in hand.
      - **D3 — `ContinueAsNew` after each armed-and-fired cycle.** A
        workflow that lives for the life of a transporter would otherwise
        accumulate history forever, which is the same unboundedness in a
        different place. Continue-as-new resets history while keeping the
        workflow ID stable, so "this transporter's cover watcher" remains
        one addressable thing in the UI.
      - **D4 — the timer's lifetime is exactly the `Vetted` state**
        (requested 2026-08-21). Armed on entry to `Vetted`, cancelled on
        any exit from it, re-armed on re-entry — the timer exists **if and
        only if** the profile is `Vetted`. This replaces an earlier
        formulation where the watcher was a free-standing workflow that
        merely happened to be started after `VettedEvent` and cleaned up
        afterwards; tying it to the state makes "an armed timer on a
        non-vetted transporter" unrepresentable rather than merely a bug
        to remember to avoid.
      - **D4a — realised as a cancellation scope inside a lifecycle
        workflow, not as cleanup code.** `workflow.WithCancel` wraps the
        timer; entering `Vetted` opens the scope, leaving cancels it.
        Cancellation is structural — nothing has to remember to tear the
        timer down, and there is no window in which the state has moved on
        but the timer has not. This requires a workflow that is still
        running while the profile sits in `Vetted`, which today's does not
        (see D4b).
      - **D4b — `TransporterVettingWorkflow` becomes a lifecycle ("entity")
        workflow. DECIDED 2026-08-21: entity workflow, accepting the costs
        below.** Today it **completes** at `Vetted`, returning
        `VettingResult`, and BR-TP26 restarts it under the same workflow
        ID for a resubmission. To hold a cancellation scope across the
        `Vetted` state it must instead stay running, with `Vetted` as a
        state it occupies rather than a terminal result, and resubmission
        arriving as a signal rather than a restart. Consequences, stated
        plainly because they are not small:
          - The Temporal UI no longer shows a `Completed` run with a
            verdict in `Result` — the verdict is read from the profile
            projection, which is where it is authoritative anyway. This
            weakens the "workflow list as vetting audit" affordance that
            38b's history dump demonstrated.
          - BR-TP26's resubmit path changes shape: `Resubmitted: true` on a
            fresh run becomes a `Resubmit` signal, and the
            `VettingResubmittedEvent`-before-restart ordering that 38b
            settled has to be re-derived.
          - `ContinueAsNew` (D3) becomes load-bearing rather than tidy,
            since the workflow now spans the whole profile lifetime.
        **Alternative considered and rejected 2026-08-21:** keep two
        separate workflows and have the watcher exit on a `StateChanged`
        signal. Cheaper, and it does not disturb BR-TP26 at all — but the
        invariant is then only as good as the signal delivery, and a missed
        or unsent signal leaves an armed timer on a transporter that is no
        longer vetted. That is precisely the class of bug D4 exists to make
        impossible.
      - **D4c — a lapse becomes a real state transition: `Vetted` →
        `CoverLapsed`. DECIDED 2026-08-21.** Found while writing this up:
        `profile.go:257` appends `FleetAvailabilityRevoked` with
        `p.state.Status` unchanged, and `Apply` (`profile.go:179`) flips
        only `FleetAvailabilityGate` and `GitVerified` — so a transporter
        whose cover has lapsed **remains `Vetted` with `gate=false`**,
        as observed live 2026-08-21 on the walkthrough org. D4's
        if-and-only-if would therefore re-arm a timer for a transporter
        that has already lapsed.

        The chosen fix is to make the lapse visible in `Status` rather than
        to compound the guard: a new `StatusCoverLapsed`, entered from
        `Vetted` on `FleetAvailabilityRevoked`. The timer guard then stays
        `Status == Vetted` alone, which is exactly the invariant D4 asked
        for, and "vetted" stops meaning two different things depending on
        which field you read. **Alternative considered and rejected:** a
        compound `Status == Vetted && GitVerified` guard — contained
        entirely within this sub-phase and touching no other code, but it
        preserves the double meaning and leaves the next reader to
        rediscover it. This is the same "two implementations of what does
        this event mean for state" hazard 38b hit once already, and the
        rejected option is the one that leaves it in place.

        This is a change to 38a's aggregate, deliberately accepted, and it
        reaches beyond the workflow:
          - `domain/profile.go` — new `StatusCoverLapsed`, and
            `RevokeFleetAvailability` sets it instead of preserving the
            incumbent status.
          - The Postgres projection and KV cache carry the new value; no
            migration is needed (status is stored as text), but existing
            rows reading `Vetted / gate=false` are **already-lapsed
            transporters mislabelled** and need a one-off correction, not
            just new-code coverage.
          - `38d-ii`'s stepper and any status chip must render the new
            state, or a lapsed transporter falls off the UI's known set.
          - Re-entry to `Vetted` from `CoverLapsed` goes through the normal
            resubmit/re-vet path (BR-TP26) — there is no direct
            un-lapse.
      - **D4d — workflow ID.** Unchanged from BR-TP26's convention:
        `{context}-transporter-vetting-{organizationID}` if D4b is taken
        (one workflow), or
        `{context}-transporter-git-cover-{organizationID}` under the D4b
        fallback (two workflows).
      - **D5 — no expiry means no timer, not a poll.** A GIT document with
        a null `expires_at` cannot lapse by time, so the workflow parks on
        the signal channel alone. This is the common case today, since the
        `api.*` surface still has no `expiresAt` field (see the open
        question below).
      - **D6 — migrate the existing schedules, don't strand them.**
        Startup deletes any `{context}-transporter-git-monitor-*` schedule
        it finds and starts the replacement workflow for that
        organization. Without this the 8 live schedules — including the
        broken `7730206f` one — keep firing against a
        `TransporterGitMonitorWorkflow` that is no longer registered.
        `TransporterGitMonitorWorkflow` and `GitMonitorScheduleOptions`
        are then deleted, along with `ORGANIZATIONS_GIT_MONITOR_INTERVAL`.

      #### Proposed business rules (to confirm before any code)

      - **BR-TP60** — a transporter that reaches `Vetted` gets exactly one
        durable cover watcher, armed to the earliest expiry across its
        current GIT documents; when that instant arrives it performs
        BR-TP28's drop, unchanged.
      - **BR-TP61** — a change to a GIT document's expiry (BR-TP59)
        re-arms the watcher, and the watcher never fires on a stale expiry.
      - **BR-TP62** — an armed cover timer exists **if and only if** the
        profile is `Vetted`. Leaving that state cancels the timer;
        re-entering it re-arms from the then-current expiry, not the one
        the previous arming used. The timer is at-most-once per lapse and
        survives worker restarts (BR-TP27's durability property) without
        duplicating the drop.
      - **BR-TP63** — a lapse transitions the profile `Vetted` →
        `CoverLapsed` (D4c); `FleetAvailabilityGate` and `GitVerified` go
        false as they do today, and the organization is suspended as
        BR-TP28 already specifies. Return to `Vetted` is only via the
        normal re-vetting path.

      **As built.** `watchCover` holds the timer in a `workflow.WithCancel`
      scope inside the vetting workflow; `CoverExpiry` reads the earliest
      expiry across current GIT documents on every arming;
      `SignalCoverChanged` re-arms from the document write path.
      `TransporterGitMonitorWorkflow`, `GitMonitorScheduleOptions` and
      `ORGANIZATIONS_GIT_MONITOR_INTERVAL` are deleted, and
      `DeleteGitMonitorSchedules` clears the retired schedules at startup.
      Verified live: **8 schedules cleared, 0 monitor executions after**, a
      90-second cover armed and fired at its exact expiry, profile ending
      `CoverLapsed` with the organization `SUSPENDED`.

      **D4b cost less than feared — amended 2026-08-21.** The design assumed
      resubmission had to become a signal. It does not: the workflow only
      needs to stay alive *while `Vetted`*, and both exits from that state
      (`Rejected`, `CoverLapsed`) still complete the run — so **BR-TP26's
      restart-under-the-same-ID path is untouched**. The one cost that did
      land is that a vetted run has no Result to read, which is why the
      `vettingState` **query handler** (previously floated as a nice-to-have)
      is now load-bearing: the test harness and the Temporal UI both read it.

      **Two silent defects found on the way, both now covered.** The drop's
      half-completed retry branch keyed off `Vetted` — the state BR-TP63
      moved — so a retry returned success **without suspending**, leaving a
      lapsed transporter ACTIVE and assignable; there had never been a spec
      for that path. And the projection's status CHECK constraint enumerated
      the four old statuses, so the projector Nak'd `CoverLapsed` and
      JetStream redelivered it forever (ack floor frozen, consumer sequence
      past 47,000, nothing in the log) while the organization was already
      SUSPENDED — the two halves of BR-TP28 visibly disagreeing.
      `transporter_projection_test.go` now asserts every domain `Status` is
      writable.

      **Worth knowing: the Postgres-backed specs skip silently** unless
      `ORGANIZATIONS_TEST_DATABASE_URL` is set — 13 of them, reported as
      `ok`. Both 38h-i's and 38h-ii's were run for real against the live
      database (`postgres://trading_partner@localhost:5436`) before either
      was marked done.

      #### Dependency

      **Depends on 38h-i.** Without a settable `expiresAt` there is no
      expiry for BR-TP60's timer to arm against, and D5's signal-only path
      would be the universal case rather than the exception.


**Design approved 2026-08-20**; implementation is under way sub-phase by
sub-phase. Each sub-phase still gets its own business-rules-first pass
(BR-TP-style rule IDs, or a new `BUSINESS_RULES-ORGANIZATIONS.md` once 38e
lands) and Ginkgo specs derived from those rules before implementation, per
the standing AI Agent Workflow — 38a landed BR-TP18–BR-TP20 and 38b landed
BR-TP21–BR-TP28 that way.

Two design amendments were decided during 38b and are **not** reflected in
the design-decisions prose above — see `ARCHITECTURE-ORGANIZATIONS.md`
§ "Implementation notes (38a/38b as built)" for both:

- **`FleetAvailabilityGate`** is a boolean owned by `TransporterProfile`'s
  own aggregate state; `FleetAsset.AvailableForAssignment` is a **computed**
  read-layer join of the legacy per-asset rows with that gate, not a column
  `TransporterProfile` writes. This keeps `FleetAsset` in
  `organizations/internal/domain` untouched, preserving 38a's boundary.
- **`HandleGitStatusDrop`** is one orchestration command doing both halves
  of the late-GIT reaction (append `FleetAvailabilityRevoked`, then
  `Organization.Suspend()`), closing the gap where ADR-047 and ADR-049
  each specified only one of the two.

**Open question 3 is now closed (2026-08-20):** `partner-update` + the
`version` column land in **38c-i**, and `Register` widens to accept Company
Information fields. Editing Company Information was confirmed as a hard
requirement, so the "ship it read-only" fallback the design offered is
withdrawn. Detail in 38c-i's bullet above. What remains open is only the
conflict *presentation* (dialog vs. inline, and what recovery it offers),
which is 38d-i's decision.

**Sub-phase 38c was split into 38c-i and 38c-ii (2026-08-20).** Absorbing
`partner-update` left one sub-phase carrying two unrelated concerns — a
Postgres/RPC schema-and-editing pass and a binary-transport/Object Store
pass — which would have meant two business-rules passes and two red→green
cycles inside a single checklist item. 38c-i takes both `organizations`
schema changes (the `compliance_documents` PK and the `version` column) in
one migration, so 38c-ii is purely the blob path and touches no schema.
This is a partial answer to open question 2 (whether 38a–38e stay lettered
under one phase number): they stay lettered, and a letter subdivides with a
roman numeral when scope demands it rather than being promoted to its own
phase number.

**Execution order is 38a → 38b → 38c-i → 38d-i → 38c-ii → 38d-ii → 38e
(decided 2026-08-20; 38d split into 38d-i/38d-ii the same day).** The sub-phase list above is written in that order, so **the
letters are no longer alphabetical** — `38c-ii` deliberately sits after
`38d-i`. Labels were kept stable rather than reshuffled, because they are
already cited from `ARCHITECTURE-ORGANIZATIONS.md`, ADR-048 and ADR-049,
and renaming them would silently invalidate those references; read the list
order, not the letters, as the schedule.

**Correction (2026-08-20), recorded because the earlier wording here was
wrong:** this section previously called 38d "purely frontend against a
working API". That held for Company Information, Fleet and Documents, but
**not** for the vetting UI. A check before starting 38d-i found that no
`api.*` endpoint exposes `TransporterProfile` at all — 38a wired its
projection reader only into the activation guard — so the state-transition
stepper and the GIT/vetting tab, the features Phase 38 most exists to
demonstrate, had no data source. 38d-i therefore carries a small backend
task (the 16th endpoint) rather than being frontend-only. Operating Areas
and Tracking Credentials turned out to have no backend whatsoever and moved
to 38d-ii. The lesson worth keeping: "frontend-only" was asserted from the
sub-phase's description, not verified against the endpoint list.

Why 38d-i still moves ahead of 38c-ii: after the split, 38d-i depends only
on 38c-i plus its own one endpoint
(`partner-update`, `version`, and the `compliance_documents` metadata
shape), and nothing in 38c-ii's blob path. Running the frontend earlier
means the wizard, the editable Company Information tab, the state-transition
stepper and the whole vetting lifecycle become demonstrable end-to-end
sooner, and — more usefully — the frontend exercises 38a/38b/38c-i's API
surface while that code is still fresh, so anything wrong in it surfaces
before the binary transport is layered on top. The cost is bounded and
known: 38d-i's Documents tab ships without upload/download, and 38c-ii picks
up a small frontend tail to finish it. That is the trade accepted here —
one deliberately incomplete tab, in exchange for validating the API early.
The alternative ordering's only advantage was letting the frontend ship every tab
complete in one pass; nothing else about it was better.

---

## Renumbering history

Every renumbering log, moved out of the live plan on 2026-08-21 (it had
grown to ten sections, ~7.2k est. tokens). Verbatim and in chronological
order; append new logs at the end of this section.

### Renumbering (done at proposal, updated for Phase 12 insertion)

| Was | Now |
|---|---|
| *(new)* | **Phase 12 — Refdata Versioning, Tenancy & Template Inheritance** |
| Phase 12 (PROPOSED) — Ship Container Capacity Limit | Phase 13 |
| Phase 13 — Write-Side Safety | Phase 14 |
| Phase 14 — Projection Hardening | Phase 15 |
| Phase 15 — Stream Split | Phase 16 |
| Phase 16 — Performance & Load Testing | Phase 17 |
| Phase 17 (optional) — NATS Accounts Spike | Phase 18 (optional) |
| Phase 18 (optional) — Theme Spike | Phase 19 (optional) |

Cross-reference sweep (same commit):

- [x] Main plan internal references (Phase 9 "why this precedes Phase 13"→14, Phase 10's
      Phase 11/14/15 mentions→11/15/16, Phase 13–17 mutual references→14–18)
- [x] `demos/01-dictionary/PERFORMANCE.md` (and the `obsidian/POC-Dictionaries/` copy) — deferred-scenario phase labels
- [x] `demos/01-dictionary/perf/README.md` — deferred-scenario phase labels
- [x] `ARCHITECTURE.md`, `BUSINESS_RULES.md` that cite Phases 13–17
- [x] Go source comments (`events.go`, `container.go`, `commands/container.go`) — Phase 14→16
- [x] `.claude/memory/` notes citing phase numbers — `tenant_service_separation_decision.md` cites Phase 18, which kept its number through this renumbering; no correction needed

---

### Renumbering (2026-07-28 — Phase 18/20 swap)

**Why:** Phase 18 (NATS Accounts Tenancy Spike) completed and Phase 20 (Accounts Service &
Decentralized JWT Tenancy — previously tracked only in a separate approved plan file, not yet
in this document) both needed to sit immediately after Phase 12, ahead of the not-yet-built
Phases 13–19. Renumbering makes room: 15–19 are now free for phases to be inserted between the
new Phase 14 (accounts service) and the resumed sequence at Phase 20, without another cascade.

| Was | Now |
|---|---|
| Phase 18 (18a/18b) — NATS Accounts Tenancy Spike | **Phase 13** (13a/13b) |
| Phase 20 — Accounts Service & Decentralized JWT Tenancy (was only in `rippling-jumping-peacock.md`) | **Phase 14** (14a/14b/14c) — added to this document for the first time |
| Phase 13 — Ship Container Capacity Limit | Phase 20 |
| Phase 14 — Write-Side Safety | Phase 21 |
| Phase 15 — Projection Hardening | Phase 22 |
| Phase 16 — Stream Split | Phase 23 |
| Phase 17 — Performance & Load Testing | Phase 24 |
| Phase 19 — Theme Spike | Phase 25 |

Cross-reference sweep (same commit):

- [x] Main plan internal references (Phase 12's "scope (Phase 18)"→13, Phase 20's
      "Phase 16"→23, Phase 21's "Phase 14/16"→21/23, Phase 24's "Phase 14/16"→21/23)
- [x] `demos/01-dictionary/BUSINESS_RULES-SHIPPING.md` — Phase 16→23, Phase 13→20
- [x] `demos/01-dictionary/PERFORMANCE.md` (and the `obsidian/POC-Dictionaries/` copy) and
      `demos/01-dictionary/perf/README.md` — Phase 14→21, Phase 16→23, Phase 17→24
- [x] `ARCHITECTURE.md` — Phase 15→22, Phase 16→23, Phase 17→24, Phase 18/18b→13/13b
- [x] `System Design - V3 Logistics Platform.md` — Phase 18→13
- [x] `nats/nats.conf`, all Go/Vue/JS source comments citing Phase 18a/18b/16 — 13a/13b/23
- [x] `.claude/memory/` notes — `tenant_service_separation_decision.md`, `accounts_service_plan.md`,
      `MEMORY.md` updated to Phase 13/14
- [x] `AppShell-Extraction-Plan.md`'s "main plan's Phase 19 placeholder" → Phase 25
- [x] Historical/archived docs left untouched on purpose: `Dictionary-POC-Plan-ARCHIVE.md`
      (renamed `Main-POC-Plan-ARCHIVE.md` 2026-08-03 when it absorbed Phases 15–19),
      `Dictionary-Service-Plan.md`, `.ai-archive/*` document *past* renumbering events and are
      frozen snapshots, not live cross-references

---

### Renumbering (2026-08-03 — Phase 26/27 → 20/21, candidate Phases 20–25 → 100–105)

**Why:** Phase 26 (JetStream Account Limits, 20a done/20b in progress) and Phase 27 (Account
Exports/Imports, plan approved) are the two phases actually being worked now, but sat after five
not-yet-started "candidate" phases (old 20–25: Ship Container Capacity Limit, Write-Side Safety,
Projection Hardening, Stream Split, Performance & Load Testing, Theme Spike) purely because those
were proposed earlier and never renumbered down as the active accounts-architecture work grew its
own phases (16–19, then 26–27) ahead of them. Renumbering makes the live-work phases read as 20/21
again, and reserves 22–99 as open space to insert new or promoted-candidate phases without another
cascade — the old 20–25 candidates move to 100–105, out of the way but still each individually
addressable and inspectable, exactly as they were before.

| Was | Now |
|---|---|
| Phase 26 (26a/26b) — JetStream Account Limits | **Phase 20** (20a/20b) |
| Phase 27 — Account Exports/Imports | **Phase 21** |
| Phase 20 (PROPOSED) — Ship Container Capacity Limit | Phase 100 |
| Phase 21 — Write-Side Safety | Phase 101 |
| Phase 22 — Projection Hardening | Phase 102 |
| Phase 23 — Stream Split + Cross-Aggregate Consistency | Phase 103 |
| Phase 24 — Performance & Load Testing | Phase 104 |
| Phase 25 (optional PLACEHOLDER) — Theme Spike | Phase 105 |

Cross-reference sweep (same commit):

- [x] Main plan internal references — old Phase 20's "Phase 23"→103; old Phase 24's "Phase 21"→101,
      "Phase 23"→103; all `needs Phase N` / `(Phase N)` mentions inside the moved blocks updated in
      the same pass as their host block
- [x] Sections physically reordered in this document (not just renumbered in place) so phase numbers
      still read ascending top-to-bottom: 20, 21, then 100–105
- [x] `demos/01-dictionary/BUSINESS_RULES-SHIPPING.md` — "Phase 20" (capacity mechanism note) → 100,
      "Phase 23" (stream-split note) → 103
- [x] `demos/01-dictionary/PERFORMANCE.md` (and the `obsidian/POC-Dictionaries/` copy) and
      `demos/01-dictionary/perf/README.md` — Phase 21→101, Phase 23→103, Phase 24→104
- [x] `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md` — Phase 22→102, Phase 23→103,
      Phase 24→104
- [x] Go source comments (`dictionary/internal/domain/{events,container}.go`,
      `dictionary/internal/application/commands/container.go`) — Phase 23→103
- [x] `.claude/plans/AppShell-Extraction-Plan.md`'s "main plan's Phase 25 placeholder" → Phase 105
- [x] `.claude/memory/` — `phase27_account_exports_imports.md` renamed
      `phase21_account_exports_imports.md` (frontmatter `name` updated too); `MEMORY.md` and
      `accounts_service_plan.md`'s cross-links updated to match
- [x] Historical/archived docs left untouched on purpose: `Main-POC-Plan-ARCHIVE.md`,
      `Dictionary-Service-Plan.md`, `.ai-archive/*`, and the two renumbering tables above this one —
      all document *past* renumbering events and are frozen snapshots, not live cross-references.
      Same reasoning applies to the two memory-file passages recording *earlier* uses of the "Phase
      20" slot (`tenant_service_separation_decision.md`, `accounts_service_plan.md`,
      `Refdata-Versioning-Tenancy-Design.md` § — each describes a 2026-07-28 event, not this one, and
      would be actively misleading if rewritten to say "Phase 100"

---
### Renumbering (2026-08-17 — Archive close-to-completion phases, free the 23–30 block)

**Why:** Phases 23, 25, 25i, 26, 27, 28, and 30 had each reached 93–100%
checklist completion, and their full design/checklist detail (over 2,400
lines combined) was making the live plan harder to scan for what's actually
still open. Following the same "archive completed work, keep a one-line
summary + link" pattern already used for Phases 0–19, their full detail
moved to `Main-POC-Plan-ARCHIVE.md`, condensed to a one-bullet-per-phase
summary in this document (see "Phases 23, 25–28, 30 — Completed" above).
Their small number of genuinely-open items (Phase 23's multi-tab
live-verification pass, Phase 26's deferred-questions list) were not
archived along with them — an archived file isn't read into context by
default, and a checklist item nobody sees again isn't tracked, it's lost.
They were folded into a new **Phase 42 — Close-Out Review**, a standing
phase with no design of its own, that exists purely to hold carried-forward
loose ends until they're resolved or explicitly re-deferred.

Phases 24 and 29 were **not** close to completion (24: only 24a of
24a/24b/24c done; 29: 0%) and stayed active, but sat orphaned in the middle
of a now-archived block — renumbered up to the 40s to sit together as their
own forward-looking group, following the precedent of the two renumbering
tables above this one (old candidate phases moved to a fresh open range
rather than staying wedged between completed work).

| Was | Now |
|---|---|
| Phase 24 (24a DONE; 24b/24c not started) — Credential Lifecycle Hardening | **Phase 40** |
| Phase 29 (PROPOSED, not started) — NATS 2.11 Server-Hop Tracing | **Phase 41** |
| *(new)* — Close-Out Review, outstanding items from archived Phases 23/26 | **Phase 42** |

Archived in full (no longer numbered as active phases in this document,
only as one-line summaries — see above):

| Archived | Where |
|---|---|
| Phase 23 — Admin UI: SSE → NATS WebSocket Migration | `Main-POC-Plan-ARCHIVE.md` |
| Phase 25 (25a–25h/25i) — Pricing Service | `Main-POC-Plan-ARCHIVE.md` |
| Phase 26 — Organizations Service | `Main-POC-Plan-ARCHIVE.md` |
| Phase 27 — Admin UI: Account Activity Panel | `Main-POC-Plan-ARCHIVE.md` |
| Phase 28 — Distributed Tracing for Inter-Service Comms | `Main-POC-Plan-ARCHIVE.md` |
| Phase 30 — `observability-service` Extraction | `Main-POC-Plan-ARCHIVE.md` |

Cross-reference sweep (same commit):

- [x] Main plan internal references — the two prior renumbering tables above
      this one already document *earlier, unrelated* uses of the numbers
      24/29 in an older scheme (a former "Phase 24 — Performance & Load
      Testing" → Phase 104, from the 2026-08-03 renumbering); left
      untouched on purpose, same reasoning as every prior entry in this log
      — they're frozen snapshots of a past event, not live references to
      the phases renumbered here.
- [x] `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-ACCOUNTS.md`,
      `ARCHITECTURE-DICTIONARY.md`, `ARCHITECTURE-COMMUNICATIONS.md`,
      `ARCHITECTURE-ADMIN.md` cite "(Phase 24)"/"(Phase 29)" — **resolved
      2026-08-17.** Two citations (`ARCHITECTURE-COMMUNICATIONS.md`'s
      "SERVER rows need no service code at all", `ARCHITECTURE-ADMIN.md`'s
      "Server-hop spans") were genuinely about the real Phase 29 content and
      just needed the number updated to Phase 41. One
      (`ARCHITECTURE-ACCOUNTS.md`'s "What the tenant connection can and
      cannot reach") was genuinely about Phase 23 and got that number
      instead. The remaining four (`ARCHITECTURE-ACCOUNTS.md`'s "Two
      PLATFORM connections, not one"; `ARCHITECTURE-DICTIONARY.md`'s "Not
      contradicted by the Admin UI's cross-account panels"; and
      `ARCHITECTURE-COMMUNICATIONS.md` §§ 11–12) turned out to be genuinely
      **stale content**, not just mislabeled — all four described
      `shipping-service` hosting cross-account diagnostics
      (`PlatformFullJS`, `tenantLabelsByAccount()`, the six lifted REST
      endpoints) that Phase 30 actually moved to `observability-service`.
      Fixed with Phase 30(d/h) amendment blockquotes in place — original
      text kept as historical record, per this repo's own established
      documentation convention — rather than rewritten in place. Also found
      and fixed the same staleness in a source comment
      (`frontend/admin/src/components/ConnectionsPanel.vue`'s
      `resolveLabel()` doc comment, which still named the retired
      `nats_ops.go`/`tenantLabelsByAccount()` and claimed a resilience
      property — independent resolution if accounts-service is down — that
      no longer holds now that both label tiers depend on accounts-service).
- [x] `demos/01-dictionary/BUSINESS_RULES-*.md` — no "Phase 24"/"Phase 29"
      references found
- [x] Go/Vue source comments — no "Phase 24"/"Phase 29" references found
      (existing `24a`/`24b`/`24c` comments in `isolation_test.go`/
      `tenant_switch_test.go` deliberately left as-is; see Phase 40's own
      renumbering note)
- [x] `.claude/memory/` — no "Phase 24"/"Phase 29" references found
- [x] Historical/archived docs left untouched on purpose: the two prior
      renumbering tables above, `Main-POC-Plan-ARCHIVE.md`,
      `Dictionary-Service-Plan.md`, `.ai-archive/*` — frozen snapshots of
      past events, not live cross-references

---

### Renumbering (2026-08-18 — Phase 41 → Phase 36, close the 36–39 gap)

**Why:** The 2026-08-17 renumbering moved Phase 29 (NATS 2.11 Server-Hop
Tracing) up to Phase 41 alongside Phase 24 → Phase 40, grouping them as a
forward-looking pair since both sat orphaned mid-block at the time. That
grouping is no longer the right shape: Phase 40 (Credential Lifecycle
Hardening) and the tracing phase have no dependency on each other, and
Phase 35's completion the next day freed 36–39 as unclaimed space
immediately following the last completed phase. Moved the tracing phase
down to Phase 36 — the next available number — rather than leaving it
sitting in the 40s for no reason once the pairing that justified it no
longer applies. Phase 40 itself is unaffected and keeps its number.

| Was | Now |
|---|---|
| Phase 41 (PROPOSED, not started) — NATS 2.11 Server-Hop Tracing | **Phase 36** |

Cross-reference sweep (same commit):

- [x] Main plan internal references — the 2026-08-17 renumbering table and
      its cross-reference-sweep entries above document *that* event and are
      left untouched on purpose, same reasoning as every prior entry in
      this log — they're a frozen snapshot, not a live reference to this
      further move.
- [x] Section physically moved (not just renumbered in place) to sit
      immediately after Phase 35, ahead of Phase 40, so phase numbers still
      read ascending top-to-bottom.
- [x] `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md`
      §6 ("SERVER rows need no service code at all") — "Phase 41" → "Phase 36"
- [x] `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-ADMIN.md`
      §4.5 ("Server-hop spans") — "Phase 41" → "Phase 36"
- [x] `demos/01-dictionary/BUSINESS_RULES-*.md` — no "Phase 41" references
      found
- [x] Go/Vue source comments — no "Phase 41" references found
- [x] `.claude/memory/` — no "Phase 41" references found (phase is
      PROPOSED/not started, has never landed, so no implementation memory
      exists yet to update)

---

### Renumbering (2026-08-18b — Phase 36 → Phase 43, deferred for further research)

**Why:** The design-gate spike fully validated a corrected design for
"Trace this subject" (see the phase's own "Spike findings"/"Design
decisions" sections and BR-042), but the user decided to defer
implementation pending further research rather than start it immediately.
The phase was never implemented, so per this document's own convention
("candidate/deferred phases move to the 100+ block ... since they were
never implemented") it moves out of the low-numbered active block. Renamed
Phase 36 → Phase 43 (the next available number after Phase 42) and moved
it physically to sit after Phase 42, ahead of the Phase 100+ block, so
phase numbers still read ascending top-to-bottom. Unlike prior renumbers,
this one also carries a status change: the header now reads
**DEFERRED 2026-08-18 — design approved, implementation on hold**, and a
note points at the before/after summary diagram saved for when the phase
is picked back up. No content in "Spike findings"/"Design decisions"/BR-042
changed — only the number, position, and status.

| Was | Now |
|---|---|
| Phase 36 (design approved 2026-08-18) — NATS 2.11 Server-Hop Tracing | **Phase 43** (DEFERRED 2026-08-18) |

Cross-reference sweep (same commit):

- [x] Main plan internal references — the 2026-08-17 and 2026-08-18
      renumbering tables above document those events and are left untouched
      on purpose, same reasoning as every prior entry in this log.
- [x] Phase 107 ("Re-fire a Captured Trace") — added a note pointing "Phase
      36" references at this phase's new number and status, rather than
      rewriting its own heading/body text which was accurate at the time it
      was written.
- [x] Section physically moved (not just renumbered in place) to sit after
      Phase 42 and before Phase 100, so phase numbers still read ascending
      top-to-bottom.
- [x] `demos/01-dictionary/BUSINESS_RULES-SHIPPING.md` — BR-042's own
      heading already says "Phase 36" in its title; left as-is since it
      documents the phase's history at the time BR-042 was drafted, same
      as this log's own entries do — the plan's Phase 43 header is the
      live cross-reference of record for the current number.
- [x] `obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md`
      / `ARCHITECTURE-ADMIN.md` — both already say "Phase 36" from the prior
      renumbering; left as-is for the same reason as BR-042 above (not
      re-swept for a phase that is now deferred rather than active — revisit
      if/when Phase 43 resumes).
- [x] Go/Vue source comments — no "Phase 36" references found (no code has
      been written for this phase yet)
- [x] `.claude/memory/` — no "Phase 36" references found

### Renumbering (2026-08-19 — collision cleanup, Phase 36 freed for reuse)

**Why:** the user asked to use "Phase 36" for a new, unrelated frontend
phase (Tech Lab Operator rebrand + Organizations migration). The number
was deliberately left alone in the 2026-08-18b sweep above because the
server-hop tracing phase — by then already renumbered to 43 — still had
live cross-references reading "Phase 36" in BR-042's heading and both
architecture docs, and those references were accurate *at the time each was
written*. Reusing 36 for a second, unrelated phase without first updating
those references would make "Phase 36" ambiguous going forward — old docs
would keep meaning server-hop tracing while the live plan meant something
else entirely. So, on explicit request, this pass finishes the sweep that
2026-08-18b intentionally deferred, updating every remaining "Phase 36"
citation for server-hop tracing to its current live number, 43, before 36
is reassigned below.

| File | Change |
|---|---|
| `demos/01-dictionary/BUSINESS_RULES-SHIPPING.md` | BR-042 heading "(Phase 36, ...)" → "(Phase 43, ...)"; added a numbering-note callout under the heading; "Phase 36's 'Spike findings'" → "Phase 43's 'Spike findings'" |
| `obsidian/.../ARCHITECTURE-COMMUNICATIONS.md` §6 | "(Phase 36, renumbered ... then 2026-08-18 to Phase 36 — still not started)" → cites Phase 43 and notes 36 was later freed |
| `obsidian/.../ARCHITECTURE-ADMIN.md` §4.5 | same update as above |
| `obsidian/.../images/phase36-trace-the-subject-options.png` | renamed to `phase43-trace-the-subject-options.png` (file had no other referrers besides the Phase 46 numbering note, updated in the same pass) |
| `.claude/memory/phase36_nats_hop_tracing_renumbered.md` | renamed to `phase43_nats_hop_tracing_renumbered.md`, content rewritten to the current 43/DEFERRED state and to note 36's reuse |
| `.claude/memory/MEMORY.md` | index line updated to point at the renamed memory file and cite Phase 43 |
| This plan, Phase 46's numbering note | appended an "Update 2026-08-19" paragraph rather than rewriting the original note, so the historical reasoning stays intact |

Cross-reference sweep (same commit):

- [x] Phase 43's own header and body already say "Phase 43" throughout — no
      change needed there.
- [x] Phase 107 ("Re-fire a Captured Trace") — already points at Phase 43
      per the 2026-08-18b sweep; no "Phase 36" text remained to fix.
- [x] The 2026-08-17, 2026-08-18, and 2026-08-18b renumbering tables above
      are left untouched — they are a historical audit trail of what each
      renumbering event did, not live cross-references, same reasoning as
      every prior entry in this log.
- [x] No other `grep -r "Phase 36\|phase36"` hits remain outside this log's
      own history and the new Phase 36 section below.

### Renumbering (2026-08-19 — Phase 46 → Phase 37, approval)

**Why:** the user asked to rename the VitePress Documentation Site phase
from 46 to 37 and approve it in the same request. 37 was free — a genuine
gap left by the 2026-08-18 renumbering ("close the 36–39 gap") that was
never actually filled, and `grep -rn "Phase 37"` across the repo returned
no hits before this move, so unlike Phase 36 there was no historical
cross-reference trail to collide with. Moved the section to sit
immediately after Phase 36, ahead of Phase 40, so phase numbers still read
ascending top-to-bottom. The status also changed from PROPOSED to
**APPROVED**, and its checklist is now underway.

| Was | Now |
|---|---|
| Phase 46 (PROPOSED — awaiting approval) — VitePress Documentation Site | **Phase 37** (APPROVED) |

Cross-reference sweep (same commit):

- [x] Main plan internal references — the prior renumbering tables above
      document those events and are left untouched on purpose, same
      reasoning as every prior entry in this log.
- [x] Section physically moved (not just renumbered in place) to sit
      immediately after Phase 36, ahead of Phase 40.
- [x] `grep -rn "Phase 46"` outside this document — no hits found; the
      phase was never implemented, so no code, `BUSINESS_RULES-*.md`, or
      architecture doc ever cited it.
- [x] `.claude/memory/` — no "Phase 46" references found.

---

### Renumbering (2026-08-20 — Phase 46 → Phase 38, Transporter Registration & Vetting)

**Why:** the user asked to rename the Transporter Registration & Vetting
(Organizations) phase from 46 to 38. 46 was originally the VitePress
Documentation Site's number before its own 2026-08-19 renumbering above
freed it for reuse; 38 was checked and is genuinely free — it was briefly
used same-day for a docs-site Docker follow-up that was folded into Phase
37's own record before any renumbering ever touched it (see
`Main-POC-Plan-ARCHIVE.md`'s "Docker follow-up" note under Phase 37), so it
carries no separate historical cross-reference trail of its own. Moved the
section to sit immediately after Phase 37, ahead of Phase 40, so phase
numbers still read ascending top-to-bottom. Status is unchanged
(**PROPOSED — awaiting approval**); this is a rename only, not an approval.

| Was | Now |
|---|---|
| Phase 46 (PROPOSED — awaiting approval) — Transporter Registration & Vetting (Organizations) | **Phase 38** (PROPOSED — awaiting approval) |

Cross-reference sweep (same commit):

- [x] Main plan internal references — the prior renumbering tables above
      (including the 2026-08-19 Phase 46 → Phase 37 entry) document those
      events and are left untouched on purpose, same reasoning as every
      prior entry in this log.
- [x] Section physically moved (not just renumbered in place) to sit
      immediately after Phase 37, ahead of Phase 40; sub-phase labels
      46a–46e relettered to 38a–38e throughout the moved section.
- [x] `grep -rln "Phase 46"` outside this document — four hits, all design
      docs for this same phase (`ARCHITECTURE-ORGANIZATIONS.md`,
      `ADR-046-transporter-aggregate-split.md`,
      `ADR-047-transporter-vetting-temporal-saga.md`,
      `ADR-048-document-storage-nats-object-store.md`,
      `ADR-049-cross-aggregate-concurrency.md`) — updated to "Phase 38" in
      the same pass. **ADR file numbers themselves are unchanged** (ADR-046
      through ADR-049 are a separate numbering series from POC plan phases;
      the coincidence that "046" matched the old phase number is exactly
      that, a coincidence — only in-prose "Phase 46" mentions inside those
      files were renamed, never the ADR filenames/numbers/cross-links to
      them).
- [x] `.claude/memory/` — no "Phase 46" or "46a"–"46e" references found.

---

### Renumbering (2026-08-20b — Phases 40–49 → 60–69, free the 40s block)

**Why:** the user asked to move every plan in the 40–49 block up to 60–69
respectively, freeing the 40s for future work. Six sections carried numbers
in that block; only the four still-open ones were moved. **Phases 44 and 45
were deliberately left at their numbers** — both are complete, their full
detail is a frozen snapshot in `Main-POC-Plan-ARCHIVE.md` (which this repo's
conventions say is never edited, phase numbers included), and their numbers
are baked into ~25 shipped-code comments and business rules describing work
that has already landed (`PulsePanel.vue`, `AccountsOverviewPanel.vue`,
BR-042/BR-043/BR-044, `ARCHITECTURE-ADMIN.md` §§ 4.3/4.5). Renumbering them
would have desynced the live stubs from the archive to relabel history.
Phases 41, 46, 48, and 49 were already free — 41 and 46 survive only inside
the frozen renumbering tables above, which record past events and are not
live references.

Sections were renumbered in place; they already sat in ascending order and
no section needed physically moving. Statuses are unchanged — this is a
rename only, not an approval or a deferral.

| Was | Now |
|---|---|
| Phase 40 (24a DONE; 24b/24c not started) — Credential Lifecycle Hardening | **Phase 60** |
| Phase 42 — Close-Out Review: Outstanding Items Carried Forward | **Phase 62** |
| Phase 43 (DEFERRED, design approved) — NATS 2.11 Server-Hop Tracing | **Phase 63** |
| Phase 47 (APPROVED, implementation on hold) — Cross-Tenant Pub/Sub Observability | **Phase 67** |
| *(not moved)* Phase 44 — Request/Reply `Pulse` Tab (COMPLETE, archived) | Phase 44 |
| *(not moved)* Phase 45 — Accounts Overview (COMPLETE, archived) | Phase 45 |

Sub-phase labels: **`47a`/`47b`/`47c` were relettered to `67a`/`67b`/`67c`**,
which is the opposite of the call made for Phase 60's `24a`–`24c`. The
reasoning differs because the facts do: Phase 67 has no implementation, so
its only references were the PROPOSED BR entries and the pending/skipped test
stubs that name them — a small, safe sweep — whereas `24a`–`24c` label
shipped test code.

Cross-reference sweep (same commit):

- [x] Main plan internal references — live cross-references to Phases 42/43/47
      updated (the archived-phase stubs' "carried forward to Phase 42" lines,
      Phase 107's body, Phase 108's heading and body). The renumbering tables
      above, and the historical `> **Renumbered ...**` notes inside the moved
      sections themselves, are left untouched on purpose — frozen snapshots
      of past events, same reasoning as every prior entry in this log. A new
      note was appended to each moved section instead.
- [x] `demos/01-dictionary/BUSINESS_RULES-SHIPPING.md` — BR-042 "(Phase 43…)"
      → Phase 63; BR-045–048 "(Phase 47a/b/c…)" → 67a/67b/67c.
- [x] `demos/01-dictionary/BUSINESS_RULES-REFDATA.md` — BR-D45 → Phase 67a.
- [x] `demos/01-dictionary/BUSINESS_RULES-ACCOUNTS.md` — BR-AC34 → Phase 67a.
- [x] `demos/01-dictionary/BUSINESS_RULES.md` — index lines for BR-045–048,
      BR-D45, BR-AC34 → Phase 67/67a.
- [x] `obsidian/.../ARCHITECTURE-ADMIN.md` § 4.4 and
      `ARCHITECTURE-COMMUNICATIONS.md` § 6 — server-hop-tracing lineage notes
      extended with the 43 → 63 hop; `ARCHITECTURE-COMMUNICATIONS.md`'s
      "Phase 30i/Phase 42 notes" → Phase 62;
      `ARCHITECTURE-ORGANIZATIONS.md`'s "Phase 42 close-out list" → Phase 62;
      `ARCHITECTURE-OBSERVABILITY.md`'s "Phase 47" link → Phase 67.
- [x] Pending/skipped test stubs naming Phase 47a/47b/47c — swept to
      67a/67b/67c (`pubsub_observability_test.go` ×3, `pubsub_export_test.go`,
      `MessagesPanel.spec.js`). No non-pending code was touched.
- [x] `.claude/memory/` — `phase43_nats_hop_tracing_renumbered.md` renamed to
      `phase63_nats_hop_tracing_renumbered.md` (slug and body updated to cite
      Phase 63, prior numbers kept as history), and `MEMORY.md`'s index line
      updated; `phase36_tech_lab_operator_rebrand.md`'s "now live at Phase 43"
      → Phase 63.
- [x] `images/phase43-*.png` diagram filenames left unrenamed — asset renames
      plus their in-doc references would be purely cosmetic. Noted in
      Phase 63's own entry.
- [x] Historical/archived docs left untouched on purpose:
      `Main-POC-Plan-ARCHIVE.md`, the prior renumbering tables above,
      `Dictionary-Service-Plan.md`, `.ai-archive/*`.

---

