# nats-tech-lab — Dictionary POC Plan Archive (Phases 0–11)

Full verbatim detail for **completed** phases, moved out of the live plan
(`Dictionary-POC-Plan.md`) to keep that file lean. This file is a reference —
it is not meant to be read into context by default; open it only when you
need the original rationale, checklist detail, or design notes for a
specific completed phase.

The live plan (`Dictionary-POC-Plan.md`) keeps a one-line status entry per
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
