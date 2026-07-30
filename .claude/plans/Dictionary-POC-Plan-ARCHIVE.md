# nats-tech-lab — Dictionary POC Plan Archive (Phases 0–14)

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

