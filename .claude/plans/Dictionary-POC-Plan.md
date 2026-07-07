# nats-tech-lab — Implementation Plan

## Purpose

A lab application for evaluating NATS.io patterns in the context of a V3 greenfield logistics platform. Each demo is self-contained: the user picks a pattern from the lab shell, reads an intro, launches the demo (Docker), and shuts it down when done.

The core architectural question being investigated: **what is the correct responsibility split between JetStream (event backbone), NATS KV (fast lookup/watch/cache), Postgres (transactional source of truth), and CQRS projections?**

---

## Project Structure

```
nats-tech-lab/
  lab-shell/              # Vue 3 + PrimeVue + Pinia frontend (demo menu + intro pages)
  demos/
    01-dictionary/        # First demo: Dictionary POC
      backend/            # Go service (hexagonal layout, borrowed from Fizmath Plaza)
      frontend/           # Vue 3 demo UI (isolated, own docker-compose)
      docker-compose.yml  # Spins up: Postgres + NATS + backend + frontend
      README.md           # Intro text shown in lab shell
```

---

## Lab Shell (Phase 1)

**Stack:** Vue 3 + PrimeVue v4 + Pinia

**Responsibility:** A simple menu listing available demos. Each entry shows:

- Demo title and one-line description
- A "Launch" button that opens the demo UI in a new tab (or iframe — decided: new tab for Phase 1)
- A brief intro page explaining the pattern being demonstrated

**Key design note:** Pinia stores are intentionally used as a frontend analogue to server-side materialized views. Both are projected read models derived from an event source — just at different layers (KV/Postgres on server, Pinia in browser). This parallel should be explicit in the UI and docs.

**Phase 1 scope:** Static menu + intro pages only. No live status. Microfrontend integration is out of scope.

---

## UI Styling — UniFi Aesthetic

**Library:** PrimeVue v4 (Vue 3-only). Start from the **Aura preset** (darkest built-in) and override `--p-*` CSS tokens.

**Design target:** UniFi Network Application — dark, data-dense, angular. Not a pixel-perfect clone; enough fidelity to evoke the aesthetic.

**Verified starting tokens** (community reverse-engineered from proxmorph; text colors survived adversarial verification, background/accent did not — extract those from a live UniFi instance via devtools):

```css
/* Text — medium confidence (proxmorph, 2-1 vote) */
--p-text-color:          #DEE0E3;   /* primary text */
--p-text-muted-color:    #B7BCC2;   /* secondary / label */
--p-text-disabled-color: #737C87;   /* disabled / hint */

/* Background + accent — extract from live UniFi instance */
/* Open UniFi Network App → devtools → inspect :root for --ubnt-* or --unifi-* */
```

**Dark mode:** PrimeVue v4 activates dark mode via `document.documentElement.classList.toggle('p-dark')` — the same class-toggle pattern UniFi uses (`.ubnt-mod-dark` on `body`). Default to dark.

**Data tables:** Use `<DataTable size="small">` — supports frozen columns, row grouping, multi-level headers, and lazy loading, matching the density of UniFi's grid views.

**Shared theme file:** Both `lab-shell/` and `demos/01-dictionary/frontend/` import the same custom Aura-based preset so styling stays in sync across frontends.

---

## Demo 01 — Dictionary POC

### Problem

Dictionary/reference data (UI dropdowns, enums, locale config, tenant config, CQRS read-model lookup data) needs to be:

- Derived from an event source
- Returned based on application context (tenant, region, locale)
- Available with low latency

### Two Shapes to Compare Side-by-Side

#### Shape A — NATS KV as the Read Model

- Event handlers project directly from JetStream into KV
- Dictionary reads go straight to KV
- No Postgres-backed read table involved
- KV key format: `{context}:{entityType}:{id}` (e.g. `en-GB:currency:GBP`)

#### Shape B — NATS KV as Cache in Front of Postgres

- Canonical CQRS projection lives in Postgres (the write-side event sourcing table)
- KV is a derived, low-latency cache/distribution layer
- Watch-based invalidation: when Postgres projection updates, handler writes to KV
- Cache miss falls through to Postgres

### Backend (Go)

Borrow from Fizmath Plaza: jstream wrapper, waiter, monolith composition, hexagonal layout, Docker Compose setup. **Do not retrofit Fizmath — start fresh.**

**Key differences from Fizmath Plaza:**

- Stream retention: `LimitsPolicy` (not `InterestPolicy`) — required for event replay
- Add NATS KV store usage (Fizmath has none)
- Context-aware key design (tenant/region/locale in key prefix)
- No gRPC-Gateway needed for this demo — plain HTTP REST is fine

**Domain structure:**

```
demos/01-dictionary/backend/
  cmd/main.go               # bootstraps monolith, calls Startup on each module
  internal/monolith/        # Monolith + Module interfaces (ported from Fizmath)
  internal/jstream/         # JetStream wrapper with LimitsPolicy
  internal/kvstore/         # NATS KV wrapper
  dictionary/
    composition.go
    internal/
      domain/               # DictionaryEntry entity, events, repo interface
      application/
        commands/           # CreateEntry, UpdateEntry
        queries/            # GetEntry (Shape A: from KV, Shape B: from KV→Postgres)
      postgres/             # repo implementation (Shape B only)
      eventhandler/         # JetStream consumer → projects into KV (both shapes)
      rest/                 # HTTP handlers
```

### Stream Design

```
Stream name:    DICTIONARY
Subjects:       DICTIONARY.entry.created
                DICTIONARY.entry.updated
Retention:      LimitsPolicy (enables replay)
Storage:        File
```

### KV Bucket Design

```
Bucket name:    dict-{context}   (e.g. dict-en-GB, dict-us-west)
Key format:     {entityType}:{id}
Value:          JSON-encoded DictionaryEntry
```

### Frontend (Demo UI)

Isolated Vue 3 app inside `demos/01-dictionary/frontend/`. Own docker-compose service.

Two panels side by side:

- **Shape A panel** — reads from KV directly; shows key, value, KV sequence
- **Shape B panel** — reads from KV cache with Postgres fallback; shows cache hit/miss

A form to create/update a dictionary entry fires a command to the backend, which publishes an event. Both panels update reactively (KV watch → SSE or WebSocket → frontend).

---

## Docker Compose Strategy

Each demo has its own `docker-compose.yml`. Lab shell has its own. They do not share networks.

```yaml
# demos/01-dictionary/docker-compose.yml services:
  nats:       nats:latest with JetStream enabled
  postgres:   postgres:16
  backend:    built from ./backend
  frontend:   built from ./frontend
```

Tear-down is `docker compose down` inside the demo directory.

---

## Implementation Phases

### Phase 0 — Scaffolding

- [ ] Initialise Go module in `demos/01-dictionary/backend/`
- [ ] Port `internal/monolith` interfaces from Fizmath Plaza
- [ ] Write `internal/jstream/stream.go` with `LimitsPolicy`
- [ ] Write `internal/kvstore/kv.go` wrapper
- [ ] `docker-compose.yml` for demo 01 (NATS + Postgres only first)

### Phase 1 — Shape A (KV-only read model)

- [ ] `dictionary/internal/domain/` — DictionaryEntry, events
- [ ] `dictionary/internal/application/commands/` — CreateEntry, UpdateEntry
- [ ] `dictionary/internal/eventhandler/` — consumes JetStream, writes to KV
- [ ] `dictionary/internal/application/queries/` — GetEntry reads from KV
- [ ] `dictionary/internal/rest/` — HTTP handlers
- [ ] Backend wired in `composition.go` + `cmd/main.go`
- [ ] Smoke test: create entry → event → KV → GET returns value

### Phase 2 — Shape B (KV cache + Postgres projection)

- [ ] `dictionary/internal/postgres/` — repo implementation, migration
- [ ] Event handler variant: projects to Postgres AND writes KV
- [ ] Query variant: KV hit → return; KV miss → Postgres → write KV → return
- [ ] Demonstrate cache miss path explicitly in demo UI

### Phase 3 — Demo Frontend

- [ ] Scaffold Vue 3 app in `demos/01-dictionary/frontend/`
- [ ] Install PrimeVue v4, create shared UniFi theme preset (Aura base + `--p-*` token overrides)
- [ ] Side-by-side Shape A / Shape B panels (use `<DataTable size="small">`)
- [ ] Create/Update entry form
- [ ] KV watch → SSE → reactive panel updates
- [ ] Default to dark mode (`p-dark` class on `documentElement`)
- [ ] Add frontend container to docker-compose.yml

### Phase 4 — Lab Shell

- [ ] Scaffold Vue 3 + PrimeVue v4 in `lab-shell/`
- [ ] Import shared UniFi theme preset (same file as demo frontend)
- [ ] Demo menu page
- [ ] Demo 01 intro page (content from README.md)
- [ ] "Launch demo" → new tab

---

## Working Assumptions

- Postgres remains the source of truth for governed dictionary data (Shape B assumption)
- NATS KV is appropriate for low-latency lookup and watch-based invalidation
- Context key (tenant/region/locale) is always present in the KV key — no global/unscoped lookups
- Eventual consistency is acceptable for dictionary reads
- No approval workflow, audit trail, or versioning needed for this POC
- Demo data is seeded via the command API (no seed scripts needed)
