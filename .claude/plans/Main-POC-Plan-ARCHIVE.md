# nats-tech-lab — Dictionary POC Plan Archive (Phases 0–19)

Full verbatim detail for **completed** phases, moved out of the live plan
(`Main-POC-Plan.md`) to keep that file lean. This file is a reference —
it is not meant to be read into context by default; open it only when you
need the original rationale, checklist detail, or design notes for a
specific completed phase.

The live plan (`Main-POC-Plan.md`) keeps a one-line status entry per
phase below, linking back here.

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
