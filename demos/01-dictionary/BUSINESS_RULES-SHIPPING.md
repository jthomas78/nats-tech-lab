# Business Rules — Shipping Domain

> Split out of `BUSINESS_RULES.md` to keep per-domain reads small. See that
> file's index for the Reference Data Service rules (BR-D*).
>
> Phase 12.10: shipping-service's NATS RPC client wrapper
> (`internal/refdataconsumer`) is a *consumer* of the `rpc.*` transport rules
> governed by [BUSINESS_RULES-REFDATA.md](BUSINESS_RULES-REFDATA.md)'s
> BR-D25/BR-D26 — no separate shipping-side rule is needed since the
> constraints (RPC mirrors an existing REST-backed method; `obs.rpc.*` never
> blocks/fails a reply) are enforced entirely on the `refdata-service` adapter
> side.
>
> Phase 12.11 (IMPLEMENTED, 2026-07-24): BR-D28 adds a shipping-side
> behavioral requirement — `refdataconsumer` calls `rpc.*` exclusively (no
> REST fallback) on every cache miss/refetch across all four of its methods,
> with a bounded number of retries before returning `ErrRPCUnavailable` — but
> the rule itself is still recorded in `BUSINESS_RULES-REFDATA.md` (BR-D28)
> since it governs the transport contract between the two services, not a
> shipping-domain rule of its own. The one shipping-side consequence —
> mapping `ErrRPCUnavailable` to HTTP 503 in the Phase 11.3/11.6 demo
> endpoints — is REST-layer error handling, not a Ship/Container domain
> invariant, so it's documented in `ARCHITECTURE-COMMUNICATIONS.md` § 7 and
> BR-D28 rather than as a BR-0xx entry here.
>
> Phase 15 (browser NATS WebSocket transport, Main-POC-Plan.md Phase 15)
> reverses this: shipping-service now also *serves* `rpc.*` itself
> (`dictionary/internal/natsrpc/`), the same role refdata-service's own
> adapter plays for its domain, plus a new `notify.*` publish family with no
> refdata-service equivalent. BR-023/BR-024 below are shipping-service's own
> rules for these, following the same "one rule for the transport contract,
> not one per subject" convention BR-D25/BR-D26 established.

Domain rules enforced before any event is published to JetStream. A rule
violation returns an error to the caller; no event is written.

Two aggregates share the single `SHIPPING` stream (Phase 8):

- **Ship** rules live in `dictionary/internal/domain/ship.go`
- **Container** rules live in `dictionary/internal/domain/container.go`

Cross-aggregate rules (BR-008, BR-012, BR-014) need both aggregates' state.
Both hydrate from **one atomic replay** of the `SHIPPING` stream
(`commands.hydratePair`), so these checks are strongly consistent. Phase 103
splits the stream and turns exactly these rules into the
invariant-spanning-two-aggregates problem.

All rules must have a corresponding test: ship rules in
`dictionary/integration_test.go`, container rules in
`dictionary/container_test.go`.

---

## Ship Rules

### BR-001 — Cannot arrive at a port already docked at
A ship that is currently docked at port X cannot arrive at port X again.

- **Error:** `ErrAlreadyDocked` — "ship is already docked at this port"
- **Enforced in:** `ShipAggregate.Arrive()`
- **Test:** `Domain Rules / BR-001`

---

### BR-002 — Must depart before arriving at a new port
A ship that is currently docked at port X cannot arrive at port Y without first departing port X.

- **Error:** `ErrMustDepart` — "ship must depart current port first (X)"
- **Enforced in:** `ShipAggregate.Arrive()`
- **Test:** `Domain Rules / BR-002`

---

### BR-003 — Cannot depart a port the ship is not at
A ship can only depart the port it is currently docked at. Attempting to depart a different port, or departing while already at sea, is rejected.

- **Error:** `ErrNotDocked` — "ship is not docked at this port (currently: X)"
- **Enforced in:** `ShipAggregate.Depart()`
- **Test:** `Domain Rules / BR-003`

---

### BR-017 — A ship can only arrive at a registered port
Ports are reference data (a Postgres `ports` table, not an event-sourced aggregate — registered via `POST /api/ports`). Arriving at a port that isn't registered is rejected.

- **Error:** `ErrUnknownPort` — "port is not registered"
- **Enforced in:** `ShipAggregate.Arrive()` — the application layer (`ShipHandler.ArrivePort`) resolves `portKnown` via `domain.PortRepository.Exists()` and passes it in as a parameter, the same pattern used for the cross-aggregate checks in `container.go`.
- **Test:** `Domain Rules / BR-017`

---

## Retired Rules (Phase 8)

Cargo moved off the ship aggregate: a ship's manifest is now the container
join (`onShipID == shipID`). The ship-cargo rules were retired and replaced:

| Retired | Was | Replaced by |
|---|---|---|
| BR-004 | Cannot load cargo unless docked | BR-012 |
| BR-005 | Cannot unload cargo unless docked | BR-012 |
| BR-006 | Cannot unload cargo not in the manifest | BR-011 + BR-013 |
| BR-007 | Cargo payload required (input validation) | container input validation in `commands/container.go` |

---

## Container Rules

### BR-008 — Cannot load a container already at its destination
A container whose destination port matches the ship's current port cannot be loaded — it has already been delivered.

- **Error:** `ErrContainerAtDestination` — "container destination matches the ship's current port"
- **Enforced in:** `ContainerAggregate.Load()` *(cross-aggregate: needs ship's current port)*
- **Test:** `Container Domain Rules / BR-008`

---

### BR-009 — A container can only be unloaded at its destination port
Unloading anywhere other than the container's registered destination is rejected.

- **Error:** `ErrWrongDestination` — "container can only be unloaded at its destination port"
- **Enforced in:** `ContainerAggregate.Unload()` *(cross-aggregate: needs ship's current port)*
- **Test:** `Container Domain Rules / BR-009`

---

### BR-010 — A container must be in-terminal to be loaded
Only a container sitting in a terminal yard can be crane-loaded. A container already on a ship cannot be loaded again.

- **Error:** `ErrContainerNotInTerminal` — "container must be in a terminal to be loaded"
- **Enforced in:** `ContainerAggregate.Load()`
- **Test:** `Container Domain Rules / BR-010`

---

### BR-011 — A container must be on-ship to be unloaded
Only a container currently on a ship can be unloaded. A container in a yard cannot be unloaded.

- **Error:** `ErrContainerNotOnShip` — "container must be on a ship to be unloaded"
- **Enforced in:** `ContainerAggregate.Unload()`
- **Test:** `Container Domain Rules / BR-011`

---

### BR-012 — A ship must be docked to load or unload containers
Container operations require the ship to be in port; a ship at sea can do neither.

- **Error:** `ErrNotInPort` (defined in ship.go, reused) — "ship must be docked to load or unload containers"
- **Enforced in:** `ContainerAggregate.Load()` / `Unload()` *(cross-aggregate: needs ship's current port)*
- **Test:** `Container Domain Rules / BR-012` (load + unload variants)

---

### BR-013 — A container can only be unloaded from the ship it is actually on
Unloading a container from a ship that is not carrying it (`onShipID != shipID`) is rejected.

- **Error:** `ErrWrongShip` — "container is not on this ship"
- **Enforced in:** `ContainerAggregate.Unload()`
- **Test:** `Container Domain Rules / BR-013`

---

### BR-014 — A container can only be loaded when the ship is docked at the container's terminal port
Loading pulls a container from a specific yard: the ship must be docked at that yard's port (`terminalPort == ship.currentPort`). Without this rule a ship docked in Singapore could load a container sitting in Rotterdam.

- **Error:** `ErrContainerNotAtPort` — "container is not in a terminal at the ship's current port"
- **Enforced in:** `ContainerAggregate.Load()` *(cross-aggregate: needs ship's current port)*
- **Test:** `Container Domain Rules / BR-014`

---

### BR-015 — A container ID can only be registered once
Re-registering an existing container ID would silently reset its state (e.g. teleporting an on-ship container back into a yard).

- **Error:** `ErrContainerExists` — "container is already registered"
- **Enforced in:** `ContainerAggregate.Register()` — the rule decision stays in the domain (`c.registered`), but since Phase 8.3 the container's identity is a surrogate key (UUID), so uniqueness is a **natural-key** constraint. The application (`RegisterContainer`) resolves the natural key against the event stream (`hydrateByNaturalKey`) and folds any existing `.registered` event in, so the domain still sees `c.registered == true` and rejects the duplicate. Resolution is against the authoritative event log, not an eventually-consistent read projection.
- **Test:** `Container Domain Rules / BR-015` and `Container Domain Rules / surrogate key …`

---

### BR-016 — A container ID must be in ISO 6346 format (TCKU + 7 digits)
Every container ID must start with the fixed owner prefix `TCKU` (case-sensitive) followed by exactly 7 digits, e.g. `TCKU1234567`. This lab fixes the owner code rather than validating the full ISO 6346 owner-code space.

- **Error:** `ErrInvalidContainerID` — "container ID must be in ISO 6346 format: TCKU followed by 7 digits"
- **Enforced in:** `ContainerAggregate.Register()`
- **Test:** `Container Domain Rules / BR-016`

---

### BR-018 — A container's origin and destination ports must both be registered
Registering a container with an origin or destination port that isn't in the ports registry is rejected. Reuses `ErrUnknownPort` (BR-017's error), since it's the same underlying rule applied to the container's two port fields instead of a ship's arrival port.

- **Error:** `ErrUnknownPort` — "port is not registered"
- **Enforced in:** `ContainerAggregate.Register()` — checked after BR-016 (format) and before BR-015 (duplicate registration). The application layer (`ContainerHandler.RegisterContainer`) resolves `originKnown`/`destKnown` via `domain.PortRepository.Exists()`.
- **Test:** `Container Domain Rules / BR-018`

---

### BR-019 — A ship's on-board container count must not exceed its capacity (PROPOSED — not yet implemented)
Every ship has a maximum container capacity, a fixed number set at registration (a ship's first `Arrive`, mirroring how `ShipName` is set-once at first arrival). Loading a container onto a ship whose current on-ship container count already equals its capacity is rejected — the ship must be under capacity, not merely docked (BR-012) and at the container's terminal port (BR-014).

- **Error:** `ErrCapacityExceeded` — "ship is at container capacity"
- **Enforced in:** `ContainerAggregate.Load()` — checked alongside the existing BR-012/BR-010/BR-014/BR-008 checks; requires the ship's current on-ship container count, resolved by the application layer before the domain check (mechanism — event-replay count vs. a read-model query — to be decided during implementation; see Phase 100 of `Main-POC-Plan.md`). *Amended Phase 31: previously offered "Shape A/B read-model query" as the alternative; with Shape A retired there is one read model, so the choice is now replay-vs-read-model rather than a three-way.*
- **Test:** `Container Domain Rules / BR-019` (not yet written — pending implementation)

**Frontend:** `frontend/seafreight-app` ("SeaFreight Flow") gains a load-capacity indicator column (e.g. `12 / 50`, colored by how full the ship is) in both `FleetPanel.vue` and `ShipsAtPortPanel.vue`, pairing the new `capacity` field with the container count already computed via `store.manifestFor(shipID).length`.

---

### BR-020 — A shipID and context must be a valid subject/KV-key token
`context` is threaded directly into NATS subjects (`evt.{context}.shipping.ship.{id}.{event}`) and into KV keys as a prefix (`{context}.{entityType}.{id}`). KV buckets are now tenant-scoped (one per role per NATS account: `ships`, `container`, `meta` — `dict-a`/`dict-b` until Phase 31 retired Shape A and renamed Shape B's bucket), so the {context} suffix that was formerly part of the bucket name (`dict-b-{context}`, …) has moved into the key — the charset constraint remains the same because a dot in `{context}` would silently split it across key segments. `shipID` (BR-021/BR-022: the mutable natural key, not the aggregate's surrogate `id`) is a KV-key component (`{context}.ship.{shipID}`) rather than a subject token, but is held to the same charset for consistency and because it's also carried in the event payload. Both must be non-empty and match `^[A-Za-z0-9_-]+$` — NATS's recommended safe subject-token charset. A dot would silently corrupt a KV key or split a subject across tokens; `*`/`>`/whitespace are NATS wildcard/subject metacharacters.

- **Error:** `ErrInvalidToken` — "value must be a non-empty token of letters, digits, '-' or '_'"
- **Enforced in:** `ShipHandler.hydrateByNaturalKey()` (ShipID, on every Arrive/Depart/Register/CorrectShipID) and `ArrivePort`/`DepartPort`/`RegisterContainer`/`LoadContainer`/`UnloadContainer`/`RegisterShip`/`CorrectShipID` (Context)
- **Test:** `Domain Rules / BR-020`

---

### BR-021 — A shipID can only be registered once
Re-registering an existing shipID would silently reset its state. Mirrors BR-015's container dedup, but on the natural key (a ship's surrogate `id` isn't known to the caller yet at registration time).

- **Error:** `ErrShipExists` — "ship is already registered"
- **Enforced in:** `ShipAggregate.Register()` — uniqueness is resolved against the authoritative event log (folding every ship's current state in the context, then matching by current `shipID`), not an eventually-consistent read projection, same convention as BR-015. `ArrivePort` calls this implicitly on a ship's first arrival; `RegisterShip` exposes it explicitly (optional pre-registration).
- **Test:** `Domain Rules / BR-021`

---

### BR-022 — A shipID can be corrected to any other valid, currently-unused shipID within the same context
`CorrectShipID` renames a registered ship's natural key (call-sign / internal fleet code) — e.g. after a re-flagging or a data-entry fix — without affecting its surrogate identity. `newShipID` must satisfy BR-020 and must not currently be in use by another registered ship in the same context. The ship being corrected must already be registered.

- **Error:** `ErrShipIDInUse` — "shipID is already in use by another ship"; `ErrNotFound` if the source shipID isn't registered; `ErrInvalidToken` (BR-020) for a malformed `newShipID`.
- **Enforced in:** `ShipHandler.CorrectShipID()` — resolves both the source and any target-name collision via the same authoritative-replay resolution as BR-021, then calls `ShipAggregate.CorrectShipID()`.
- **Test:** `Domain Rules / BR-022`
- **Known limitation (verified live):** a container's `onShipID` snapshots the ship's natural key at load time and is not updated by a later correction. Renaming a ship while it carries a container leaves that container stuck — unload fails with **both** the new name (BR-013, `onShipID` still holds the old name) **and** the old name (BR-012, `hydrateByNaturalKey`/`hydratePair` resolve a ship by its *current* name, so a stale name no longer matches any ship and looks unregistered/undocked). The only way to unblock it is to `CorrectShipID` back to the exact pre-correction name, unload, then correct forward again if still wanted. Documented, not fixed, in this pass.

---

### BR-023 — Every `api.*` endpoint is a second transport onto an existing REST-backed method, scoped by subject, never by request body

Shipped as `rpc.*`/`internal/natsrpc/` in Phase 15a; renamed to `api.*`/`internal/browserrpc/` in Phase 16b because every caller of these subjects is the browser, never another backend service, and `rpc.*` is reserved for service-to-service traffic (`ARCHITECTURE-COMMUNICATIONS.md` § 2.4). The rename was mechanical — subject constants and the `obs.rpc.*`→`obs.api.*` observability channel — with no change to the behaviour described below.

Mirrors BR-D25 (`BUSINESS_RULES-REFDATA.md`): `dictionary/internal/browserrpc/adapter.go`'s handlers call the exact same `commands.*Handler`/`queries.*` methods `dictionary/internal/rest/handlers.go` calls, so an operation behaves identically regardless of which transport reaches it. The `{context}` token in the subject (`api.{context}.shipping.{entity}.{action}.v1`) — the company / business-unit scope, the same axis as `evt.{context}...` and the context-prefixed KV keys, and **not** the NATS tenant/account nor the region — is the *only* source of truth for which context a request is scoped to. Every handler overwrites the decoded request body's own `context` field (if any) with the subject-derived value before calling into the application layer, so a client cannot spoof or bypass context scoping via the body. Tenant isolation itself is enforced entirely by the NATS account boundary a connection authenticates into (Phase 13a/13b), not by anything in this subject pattern — see `accounts-service/auth/token.go`'s `MintBrowserToken` doc comment (Phase 19 folded auth-service into accounts-service, see `BUSINESS_RULES-ACCOUNTS.md`) and `ARCHITECTURE-COMMUNICATIONS.md` § 2.3 for the full reasoning.

This is the **frontend-to-service** family. `rpc.*` is reserved for service-to-service calls and is a separate family with its own adapter and its own permission grant: a browser credential is never granted `rpc.>`, and backend code never calls `api.>` (`ARCHITECTURE-COMMUNICATIONS.md` § 2.4). An operation may be registered on both when both caller types genuinely need it, but each registration is independent.

- **Enforced in:** `dictionary/internal/browserrpc/adapter.go` — `contextFromSubject()` plus every `handle*` method
- **Test:** `Browser RPC Adapter (Phase 15a/16b) / BR parity` (`dictionary/browserrpc_test.go`)

### BR-024 — Ship, container, and meta projections fire a best-effort `notify.*` event after every KV write; ports fire one from the api.* adapter itself
After the ship projector, the container projector, or the meta projector successfully writes its KV bucket, it fire-and-forget publishes `notify.{context}.shipping.{entity}.changed` (entity: `ship`/`container`/`meta`) carrying the full updated entity (or, for meta, the full known-containers array — a bare JSON array, not a `{"values": [...]}` envelope) as JSON payload — letting a browser connected directly to NATS (Phase 15d) react without KV watch or SSE. **Exactly one publisher per event** is the invariant: a browser must never receive two `notify.*` messages for a single domain event, since the second carries no new information and would re-render for nothing. Ports have no event-sourced projector to hang this off (`commands.PortHandler` writes straight to Postgres and is a single instance shared by every tenant), so `browserrpc.Adapter.handlePortRegister` publishes `notify.{context}.shipping.port.changed` itself, on its own tenant connection, after a successful registration — also a bare array, matching meta's convention rather than the `{"values": [...]}` envelope `api.*.shipping.port.list.v1`'s request/reply uses, so a subscriber never needs to know which entity's REPLY shape to unwrap.

> **Amended Phase 31 (Shape B consolidation).** This rule previously read "After
> the **Shape A** ship projector …" and stated that "**Shape B** does not also
> publish: … a second notify per event would be a duplicate." When Shapes A and
> C were retired the ship projector that publishes became the *only* ship
> projector, so the Shape A/Shape B asymmetry disappeared. What that clause was
> actually protecting — one publisher per event, never two — is now stated
> directly as the invariant, because it stops being self-evident the moment a
> second ship projector is ever added back. Publishing was **moved into** the
> surviving projector rather than deleted with Shape A: it was the sole source
> of Sea Freight Flow's live fleet updates, and deleting it would have
> degraded silently (stale rows, no error).

This is plain core NATS pub/sub — deliberately **no** JetStream retention (unlike `obs.rpc.*`/RPCTRACE, BR-D29, which is refdata-service's own retained observability channel): a notification missed during a brief browser disconnect is covered by a bootstrap `api.*.shipping.{entity}.list.v1` call on reconnect, so no replay mechanism is needed. A publish failure is logged, never returned — `notify.*` is a best-effort reactive-UI convenience, not a correctness requirement the projector's own success depends on.

- **Enforced in:** `eventhandler.publishNotify()` (called from `RegisterShips`/`RegisterContainers`/`RegisterMeta`), `browserrpc.Adapter.publishPortsChanged()`
- **Test:** `notify.* publishes (Phase 15b)` (`dictionary/notify_test.go`)

### BR-025 (Phase 16f) — Reference-data reads resolve against the active tenant's refdata company context, not a hardcoded literal

`GET /api/refdata/types/{type}`, `GET /api/refdata/locales`, and the new `GET /api/refdata/contexts` all resolve the refdata-service company context to read from as `refdataCompanyContext(tenant)`, which today is simply the tenant name itself — per `Main-POC-Plan.md` § Phase 16 decision 11 ("in the common no-company-group case a tenant's own name doubles as its company `{context}` value," the same mapping `BUSINESS_RULES-ACCOUNTS.md`'s BR-AC07 relies on). This replaced the `refdataContext = "acme"` constant hardcoded through Phase 16d.

`GET /api/refdata/contexts` is new in this phase: it calls `refdataconsumer.ListContexts(ctx, tenant)` → `rpc._platform.refdata.context.list.v1` (refdata-service's `BUSINESS_RULES-REFDATA.md` BR-D35) and returns the tenant's real context list, replacing the frontend's previously hardcoded `CONTEXTS` array (`stores/port.js` in Sea Freight Flow, `stores/dictionary.js` in the Admin/Dictionary UI) with a live fetch on tenant init/switch.

**Known gap, not fixed by this rule:** "the active tenant" here is `Deps.Tenant` — REST/SSE's Phase 13b `SwitchTenant` selection, which the Admin/Dictionary frontend drives directly. Sea Freight Flow (Phase 15d) no longer calls `SwitchTenant` at all — it authenticates straight into its own NATS account. Both happen to default to the same tenant (`acme`) today, so these three endpoints read correctly for Sea Freight Flow in the common case, but if the Admin UI's tenant selection and Sea Freight Flow's own NATS tenant were ever switched to different tenants concurrently, these specific reads would silently reflect the Admin UI's selection instead of the browser's actual tenant. A real fix would thread an explicit tenant through the *shared* `useRefdataLabels`/`useL10nCopy` composables (used by both frontends) rather than relying on shipping-service's REST-side state — out of scope for Phase 16f, which only replaced the hardcoded literal, not this pre-existing Phase 15 scope-boundary seam (see that phase's Context section: refdata-service's cross-tenant PLATFORM-account model was already flagged out of scope there).

- **Enforced in:** `dictionary/internal/rest/handlers.go`'s `refdataCompanyContext`, `listRefdataType`, `listRefdataLocales`, `listRefdataContexts`; `internal/refdataconsumer/consumer.go`'s `ListContexts`
- **Test:** `TestListRefdataContextsForwardsActiveTenant`, `TestListRefdataContextsReturns503WhenRPCUnavailable` (`dictionary/internal/rest/refdata_demo_error_test.go`); `TestListContextsUsesRPCAndForwardsTenant`, `TestListContextsReturnsErrRPCUnavailableWhenNoResponder` (`internal/refdataconsumer/consumer_test.go`)

### BR-026 (Phase 17a; superseded for `browserrpc.Adapter` by BR-036, Phase 28b; retired Phase 28g) — Every `obs.api.*` event carries its headers, a publisher-side timestamp, and its payload size

Mirrors refdata-service's `BUSINESS_RULES-REFDATA.md` BR-D36 for this service's own `obs.api.*` observability channel (BR-023's sibling side-channel, same BR-D26/BR-D29 mechanism refdata-service's `obs.rpc.*` uses). `browserrpc.Adapter`'s `obsEnvelope` gained `headers`, a publisher-side `timestamp`, and `payloadBytes` on both the request-side and reply-side event — additive/optional fields, so events published before this rule shipped still decode.

> **Phase 28b amendment:** `browserrpc.Adapter` no longer publishes to `obs.api.*` at all — `internal/natstrace`'s `Tracer` replaces the old two-event (request + reply) `publishObs`/`obsEnvelope` mechanism with one reply-side `obs.trace.*` span per call (BR-036), which is a strict superset of this shape and still decodes under it. The **request-side event is not replaced by an equivalent** — `natstrace.Span` publishes only once, at `End`/`Fail`, so a client that relied on a request-direction `obs.api.*` event (as this rule originally described) no longer sees one for shipping-service. The real wire headers this rule also covers (`Nats-Service-Error`/`Nats-Service-Error-Code` on `respondError`'s actual reply) are unchanged — only the observability-copy half of this rule is superseded. refdata-service's own `obs.rpc.*` channel (BR-D36) is untouched by this amendment.

> **Phase 28g amendment — retired.** The "accepted, phase-scoped gap" the Phase 28b amendment above described (the `[messages]` view showing no new shipping-service traffic until this phase) turned out to already be universal by the time this phase started, not shipping-scoped: Phase 28a-28e had already replaced `publishObs` in every one of the five services' adapters, so `obs.api.*`/`obs.rpc.*` had been dead everywhere, and the `[messages]` view had been showing nothing for any service — not a phase-scoped gap so much as a live pipe carrying nothing. `obs.api.*`/BR-026 is now fully retired: `RpcPanel.vue`'s `[messages]` tab derives from `obs.trace.*`/the `traces` KV bucket instead (one row per span, flattened out of each trace — see `TraceWaterfall.vue`'s bucket-reading pattern, which `RpcPanel.vue` now duplicates for its own flat view), the tenant browser JWT's `obs.api.>` subscribe grant is dropped (`auth/token.go`'s `MintBrowserToken`, accounts-service), and `GET /api/rpctrace/replay` plus its RPCTRACE-stream/notify-bridge backing (`eventhandler.RegisterRPCTraceNotify`, refdata-service's `RPCTraceStreamName` provisioning) are removed outright rather than left as dead code. One real, unavoidable UI difference from the old paired view: a span carries only the *reply* side (BR-037), so the `[messages]` detail pane lost its two-pane Request | Reply split in favor of one Body/Headers section, and the row model dropped its "pending" status (a span is only ever seen already-finished).

- **Enforced in (historical, pre-28b):** `dictionary/internal/browserrpc/adapter.go`'s `publishObs()` and `respondError()` (both removed in Phase 28b)
- **Enforced now:** N/A — retired Phase 28g. The wire mechanism it once described lives on as BR-036 (`obs.trace.*`); the presentation surface it fed (`[messages]`) is governed by BR-035 instead.
- **Test:** `Browser API Adapter (Phase 15a/16b) / obs.trace.* side-channel (BR-036/BR-037)` (`dictionary/browserrpc_test.go`) — an old-shape envelope with none of BR-026's three fields still decodes without error under the new `traceSpan` shape; a failed reply (`container.load.v1` on an unregistered container) still carries real `Nats-Service-Error`/`-Code` headers on both the span and the actual wire reply. `RpcPanel.spec.js` (Phase 28g retirement, `frontend/admin/src/components/`) covers the `[messages]` tab's post-retirement behavior directly: one row per span, family/status filtering, and the single-pane detail view.

---

### BR-027 (Phase 18; `obs.*`-forwarding half superseded for `browserrpc.Adapter` by BR-036/BR-037, Phase 28b) — Every `rpc.*`/`api.*` request carries a `Nats-Requestor` header identifying its caller; every reply carries a `Nats-Responder` header identifying the answering service instance

Mirrors refdata-service's `BUSINESS_RULES-REFDATA.md` BR-D37. NATS doesn't propagate caller or responder identity onto a message itself — auth identity lives at the connection level and never reaches a handler's `Msg`, and a reply's subject alone doesn't distinguish which replica of a horizontally-scaled service actually answered. Both headers share one **instance-qualified format**, `"<name>/<instance ID>"` — the same `service.name`/`service.instance.id` split OpenTelemetry's resource semantic conventions use — so replicas (or browser tabs) stay distinguishable. `Nats-Requestor` is set by the caller: the browser's `useNatsConnection.js` `request()` (this service's `api.*` caller) combines `"seafreight-app"` with a random ID generated once per module load — i.e. per tab; `internal/refdataconsumer`'s `Consumer` (this service's own `rpc.*` caller, calling refdata-service) combines this connection's `nats.Name("shipping-service")` with a NUID generated once at `New()`. `Nats-Responder` is set by `browserrpc.Adapter` on every reply (success and error alike) as `"shipping-service/<micro.Service instance ID>"` — the instance ID is generated fresh per process by `micro.AddService`, with no config of its own. `micro.Config.Name` is set to `"shipping-service"`, matching the connection's own `nats.Name` exactly (not a family-derived name like `shipping-api`), so both headers agree on one name for this service — a mismatch there would make the Admin UI's Request/Reply panel's request and reply sides look like they belong to different entities. Instance IDs are random per process/tab today; a stable infra identity (e.g. a Kubernetes pod name) can seed the instance half later without changing the header format.

> **Phase 28b amendment:** the real wire headers themselves (`Nats-Requestor` on the caller's request, `Nats-Responder` on `browserrpc.Adapter`'s actual reply) are **unaffected** — this rule's core mechanism still holds exactly as written above. What changed is the observability-copy half: this rule used to also say both headers were "attached to the real wire message, not fabricated for the observability channel alone" as a parity claim with the `obs.api.*` copy BR-026 described; since Phase 28b retired that copy for `browserrpc.Adapter` (see BR-026's amendment), `Nats-Requestor` is no longer forwarded into any observability event at all (`natstrace.Span` never captures inbound request headers — only whatever headers `respond`/`respondError` pass to `End`/`Fail`, which are the *reply*'s own headers), and `Nats-Responder` continues to appear only in `traceSpan.headers`, not a separate `obs.api.*` copy.
>
> **Phase 28g amendment:** BR-026's `obs.api.*` channel this rule used to parity-check against is now retired outright, not just superseded for `browserrpc.Adapter` — see BR-026's Phase 28g amendment. This rule's own subject (the real wire headers on the actual `rpc.*`/`api.*` request/reply) is unaffected either way; it was never about `obs.*` traffic itself, only about what an `obs.*` copy could or couldn't parity-check against.

- **Enforced in:** `dictionary/internal/browserrpc/adapter.go`'s `respond()`/`respondError()` (set `Nats-Responder`) and `micro.AddService`'s `Config.Name`; `internal/refdataconsumer/consumer.go`'s `New()`/`requestRPC` (sets `Nats-Requestor`); `frontend/seafreight-app/src/nats/useNatsConnection.js`'s `request()` (sets `Nats-Requestor`)
- **Test:** `Browser API Adapter (Phase 15a/16b) / BR-027` (`dictionary/browserrpc_test.go`) — a successful reply carries `Nats-Responder` (prefixed `shipping-service/`) on the real wire reply; a failed reply carries it too. `TestLookupCarriesInstanceQualifiedRequestorHeader` (`internal/refdataconsumer/consumer_test.go`) asserts the requestor format directly: `"<nats.Name>/<instance>"`, stable across calls from one `Consumer`

---

### BR-028 (Phase 17c) — In the Admin UI, a NATS connection's or service instance's account resolves to a friendly name wherever possible, instead of showing the raw account NKey

**This is a presentation rule, scoped to the Admin UI only** — it governs what the Connections and Services panels *display*, not any wire protocol. Nothing about `Nats-Requestor`/`Nats-Responder` (BR-027) or the actual bytes shipping-service or refdata-service put on the wire changes; `/connz`'s raw account identifier is still the NKey it always was. The rule exists because the Connections/Services panels (Phase 17c) are read-only operator-facing surfaces, and a raw NKey (`AAFBCA52VV7P...`) means nothing to someone scanning the panel, while "acme" does. "Wherever possible" is deliberate, not a hedge: an account this process has no way to identify (accounts-service's SYS account, which shipping-service holds no connection on) correctly stays unresolved rather than guessed at.

Two independent mechanisms enforce this, one per panel — both avoid decoding account NKeys out of credential JWTs entirely, resolving everything from information the process already has or the server already reports:

- **Connections** (`GET /api/nats/connections`) — `tenantLabelsByAccount` resolves every `/connz` row's account NKey to a friendly label in two stages. First, it identifies which rows are shipping-service's *own* connections by matching local socket address (`nc.LocalAddr()` is exactly what the server reports back as that connection's `ip:port` — same TCP socket, both ends), establishing "this account NKey means PLATFORM" / "this account NKey means acme." Then it applies that mapping **by account**, not by address, to every row in the full list — so refdata-service, the `nats` CLI, and any browser tab authenticated on a known tenant account resolve too, not just shipping-service's own three rows.
- **Services** (`GET /api/nats/services`) — `browserrpc.Adapter` tags its `micro.AddService` registration with `Metadata: {"tenant": <name>}` (deliberately metadata, never `Config.Name`/`Config.Version` — those must stay identical across every tenant connection, per BR-027's `Nats-Responder` invariant). `micro.Stats.ServiceIdentity` already carries `Metadata` on every `$SRV.STATS` reply, so `listNatsServices` only needs to pass it through.

Both frontend panels prefer the resolved label over the raw NKey when present (rendered as a colored tag), falling back to a truncated raw NKey otherwise (rendered as monospace code — a deliberately different visual treatment, not just different text, so there's no single shared "accountLabel" string helper on the frontend).

> **Phase 30h amendment — Connections/Services moved to `observability-service`, and the Connections label mechanism changed with them, not just its file.** `dictionary/internal/rest/nats_ops.go`'s `tenantLabelsByAccount` matched `/connz` rows against the `LocalAddr` of connections shipping-service itself held — one per tenant, via `TenantResources` — which only worked because shipping-service was the one service with that per-tenant connection fan-out. `observability-service` holds exactly one connection (PLATFORM), so there is nothing to match against; `AccountsClient.Labels` (Phase 30d) asks accounts-service directly instead, via `GET /api/accounts` (the same endpoint the Admin UI's own Accounts panel already uses) — the authoritative name↔publicKey mapping, not an inference from socket addresses. Services' mechanism (the `micro.Config.Metadata` tag, passed through unchanged by `listNatsServices`) is unaffected — that part ported verbatim.
- **Enforced in:** `observability-service/observability/internal/rest/nats_connections.go`'s `listNatsConnections`/`listNatsAccountActivity` and `accounts_client.go`'s `AccountsClient.Labels`; `observability-service/observability/internal/rest/nats_services.go`'s `listNatsServices`' `Metadata` pass-through; `dictionary/internal/browserrpc/adapter.go`'s `micro.Config.Metadata` (shipping-service, unchanged); `dictionary/internal/rest/tenant.go`'s `ensureTenantResources` (threads the tenant name into `browserrpc.Deps.Tenant`, unchanged); `frontend/admin/src/components/ConnectionsPanel.vue`'s Account column; `frontend/admin/src/components/ServicesPanel.vue`'s tenant tag
- **Test:** `TestListNatsConnectionsResolvesTenantLabelFromAccountsService`, `TestAccountsClientLabelsIsNilSafeAndDegradesOnFailure` (`observability-service/observability/internal/rest/nats_connections_test.go`) — the new HTTP-based label resolution, and its degrade-to-unlabeled behavior when accounts-service is unreachable. `TestListNatsServicesPassesThroughInstanceMetadata` (`nats_services_test.go`, same package) — the REST handler passes `$SRV.STATS`'s metadata through untouched. `Browser API Adapter (Phase 15a/16b) / BR-028` (`dictionary/browserrpc_test.go`, shipping-service) — the production wiring that tags the metadata in the first place (`tenant.go` → `browserrpc.Deps.Tenant` → `micro.Config.Metadata`) actually produces the tag, verified over a real `$SRV.PING`, not just the REST handler's pass-through logic in isolation. `ConnectionsPanel.spec.js` and `ServicesPanel.spec.js` (`frontend/admin/src/components/`) — the resolved label actually renders as a tag and the raw-NKey fallback actually renders too, not just that the API response carries the field

---

### BR-029 (Phase 16g) — Sea Freight Flow's Fleet Management, Ships at Port, and Terminal Yard panels show a loading indicator, never a bare empty state, while a tenant or fleet-context switch is repopulating them

**This is a presentation rule, scoped to Sea Freight Flow's browser UI only** — it governs what these panels *display* mid-switch, not the underlying data or transport. `usePortStore().connect()` (run on every tenant switch via `stores/tenant.js` and every fleet-context switch via `setContext()`) resets `ships`/`containers` to `{}` synchronously, before its `listShips`/`listContainers`/`getPorts`/`knownContainers` bootstrap reads resolve — a NATS WebSocket reconnect (tenant switch) plus a request/reply round trip, both non-zero latency. Without a loading signal, a `DataTable`'s own empty state ("No ships match this filter.", "No ships docked here — send an arrival above.", "No outbound containers in this yard — register one above.") flashes in that gap, misreading as "this tenant/context genuinely has none" rather than "still loading" — most visible on a slower network or during the tenant switch's WS re-authentication, less so on localhost where the gap can be under 100ms. All three panels read off the same `ships`/`containers` state, so the same gap affects all three identically; initially only the Fleet panel was fixed (in direct response to the reported flicker), then extended to the other two once confirmed to be the same root cause.

- **Enforced in:** `stores/port.js`'s `loading` state — set `true` at the start of `connect()` (same point `ships`/`containers` are cleared), cleared once the bootstrap `Promise.allSettled` of `getPorts`/`listShips`/`listContainers`/`knownContainers` lands (success or failure — a failed read shouldn't leave a panel stuck loading). `components/FleetPanel.vue`, `components/ShipsAtPortPanel.vue`, and `components/TerminalPanel.vue` each render a spinner + panel-specific loading copy (`fleet.loading`/`shipsAtPort.loading`/`terminal.loading`) while `store.loading` is true, their `DataTable`(s) otherwise — mutually exclusive via `v-if`/`v-else`(`-if`), so a table's own empty-state message can only ever reflect a genuinely empty result, never a mid-switch one. `ShipsAtPortPanel.vue` layers this under its pre-existing `!store.port` ("select a port") branch; `TerminalPanel.vue` covers both its Outbound and Arrived tables with one shared spinner (they're both driven by the same `store.containers` reset, so there's no reason to show two).
- **Test:** `'shows a loading indicator instead of the empty state while the fleet is (re)loading'`, `'...on Ships at Port while (re)loading'`, `'...on the Terminal yard while (re)loading'` (`App.spec.js`) — each panel renders its spinner and hides its `DataTable`(s) while `store.loading` is true, and never shows its own empty-state text during that window. `'sets loading true and clears the previous context's ships/containers synchronously, then clears loading once the bootstrap reads land'` and `'clears loading even when a bootstrap read fails'` (`stores/port.spec.js`) — `connect()`'s `loading` lifecycle itself, independent of any component.

---

### BR-030 (Phase 16h) — A tenant minted by accounts-service is immediately usable by Sea Freight Flow, without an operator needing to switch the Admin UI to it first

**Found while investigating a related report** ("the port dropdown is empty for a brand-new tenant"): a genuinely new tenant's `api.*` adapter didn't exist on this process at all yet, so *every* `api.*` request against it — ships, containers, ports alike — timed out silently (5s, then swallowed by the browser's own `.catch(() => {})`), reading as "this tenant has nothing" rather than "not provisioned yet." `EnsureAllTenants` (composition.go, Startup) only covers tenants that already existed when this process last started; before this rule, a tenant minted afterward stayed unprovisioned until either a restart or an operator happened to call `POST /api/tenant/switch` for it (the Admin/Dictionary frontend's own tenant selector) — a call Sea Freight Flow never makes, since it authenticates straight into NATS (Phase 15d).

`accounts-service` now publishes `notify.accounts.account.created` the instant a create fully succeeds (BR-AC08, `BUSINESS_RULES-ACCOUNTS.md`) — a context-free subject, since accounts-service has no `{context}` of its own. `shipping-service` subscribes to it on its permanent PLATFORM-account connection and reactively provisions that tenant's resources (JetStream stream, KV buckets, projectors, `browserrpc.Adapter`), the exact same idempotent path `EnsureAllTenants` already used for tenants present at startup — this rule just adds a second trigger for tenants minted afterward, closing the gap `EnsureAllTenants`'s own doc comment already flagged.

- **Enforced in:** `dictionary/internal/rest/tenant.go`'s `EnsureTenantByName` (wraps `discoverTenants` + the existing `ensureTenantResources`, a no-op if the tenant isn't yet visible in `CredsDir` rather than an error — defensive against a stray/duplicate delivery, not expected to actually race the creds-file write in practice); `dictionary/composition.go`'s `mono.NC().Subscribe("notify.accounts.account.created", ...)`, wired right after `EnsureAllTenants` in `Module.Startup`.
- **Test:** `'EnsureTenantByName provisions globex's api.* adapter without ever calling SwitchTenant for it'` (`dictionary/tenant_switch_test.go`) — proves the actual observable behavior end to end against the real shipped `nats.conf`/creds: a request on `globex`'s account gets no reply before `EnsureTenantByName`, then a working reply after, with `SwitchTenant("globex")` never called. `'EnsureTenantByName is a no-op, not an error, for a name with no creds file'` (same file) — the defensive race guard. BR-AC08's own tests (`handler_test.go`, accounts-service) cover the publish side. Live-verified on the running stack: a tenant created via `POST /api/accounts` answered an `api.*` request over its own NATS creds (`nats req`) within ~20ms, and its Register Ship dialog's port dropdown populated immediately in the browser — neither `/api/tenant/switch` nor a restart involved.

---

### BR-031 (Phase 16i) — A tenant suspended by accounts-service stops holding shipping-service resources open, instead of reconnect-looping forever against deleted credentials

**The mirror of BR-030, found while investigating a related question** ("what happens when an RPC is sent from a suspended account?"). NATS force-evicts every connection on an account the instant it's revoked at the resolver (`$SYS.REQ.CLAIMS.DELETE`) — verified against the running stack (2026-08-03; see `ARCHITECTURE-ACCOUNTS.md` § 2t-a), correcting an earlier doc comment on `accounts-service`'s `Provisioner.DeleteAccount` that claimed the opposite. Before this rule, that eviction left `shipping-service`'s per-tenant connection (`nats.go`'s default reconnect logic) retrying forever against a `.creds` file `suspendAccount` had already deleted (`accounts/handler.go`) — one permanent, log-spamming loop per suspension, cleared only by a process restart. The browser side of this was already correct by accident (`connectInfo`'s existing 403 check refuses re-authentication), but its refusal was set on `useNatsConnection.js`'s `lastError` and never rendered anywhere, so a suspended session's panels just went quiet with no explanation.

`accounts-service` now publishes `notify.accounts.account.suspended` the instant a suspend fully succeeds (BR-AC09, `BUSINESS_RULES-ACCOUNTS.md`) — same context-free subject family and same PLATFORM-account connection as BR-AC08's created event. `shipping-service` subscribes to it and tears that tenant's resources down: stops its projectors and `browserrpc.Adapter`, then explicitly closes shipping-service's own connection to that account — the explicit `Close()` is what actually disables `nats.go`'s automatic reconnect; the account being unresolvable does not, by itself, stop the client from retrying. Sea Freight Flow's `App.vue` now also renders `lastError` as a danger `Tag` in the topbar whenever it's non-empty, clearing automatically once a connection succeeds again — no separate acknowledgment step, since `useNatsConnection.js`'s `connect()` already resets it to `''` on success.

Deliberately out of scope for this rule: a terminal-vs-transient classification of connection/JetStream errors as a backstop for a missed or out-of-band suspension (an operator revoking an account directly via `nsc`, bypassing `accounts-service` entirely). The event above covers the normal path; the backstop is a separate, larger design decision (see `ARCHITECTURE-ACCOUNTS.md` § 2t-a's "Proposed" section) not yet implemented.

- **Enforced in:** `dictionary/internal/rest/tenant.go`'s `TeardownTenantByName` (mirrors `EnsureTenantByName`'s shape: `deps.TenantResources` lookup, no-op — not an error — if the tenant was never provisioned or already torn down) and its per-tenant imported `notify.accounts.account.*` subscriptions. `accounts-service`'s `Handlers.publishAccountSuspended` (BR-AC09) is the producer. `frontend/seafreight-app/src/App.vue`'s `lastError` `Tag` in the topbar (`data-testid="connection-error"`).
- **Test:** `'TeardownTenantByName stops globex's api.* adapter by closing shipping-service's own connection to it'` (`dictionary/tenant_switch_test.go`) — proves the observable behavior end to end: `globex`'s adapter answers after `EnsureTenantByName`, goes silent after `TeardownTenantByName`, by way of shipping-service's own connection actually closing, not just local bookkeeping. `'TeardownTenantByName is a no-op, not an error, for a tenant that was never provisioned'` (same file) — the defensive idempotency guard, mirroring BR-030's. BR-AC09's own tests (`handler_test.go`, accounts-service) cover the publish side. `'surfaces a NATS connection error, and clears it once the connection recovers'` (`App.spec.js`) — the `lastError` `Tag` appears and disappears correctly, distinguished from the pre-existing (and, while mocked-disconnected, also-danger-severity) connection-status `Tag`.

---

### BR-032 (Phase 16j) — A tenant reactivated by accounts-service becomes usable again immediately, closing the suspend/reactivate round trip

**A regression introduced by BR-031, caught in the architecture review that followed it.** BR-031 tears a suspended tenant's resources down — correctly — but on its own that is a **one-way door**: `EnsureAllTenants` only runs at process startup, and Sea Freight Flow never calls `SwitchTenant` (Phase 15d), so nothing rebuilt a tenant that came back. A suspend→reactivate cycle left the tenant dark to Sea Freight Flow until shipping-service restarted or an operator happened to switch the Admin UI to it. Before BR-031 the same cycle was ugly but self-healing (the reconnect loop would have recovered once creds returned); making teardown clean is what made the missing counterpart load-bearing. This is the BR-030 gap in a third position, and the three rules now form a closed lifecycle: created → provision (BR-030), suspended → tear down (BR-031), reactivated → provision again (BR-032).

`accounts-service` publishes `notify.accounts.account.reactivated` once a reactivation fully commits (BR-AC10, `BUSINESS_RULES-ACCOUNTS.md`) — crucially *after* the fresh `.creds` file is written, since this consumer resolves a tenant by scanning that directory. `shipping-service` subscribes to it and calls the existing `EnsureTenantByName` **unchanged**: BR-031's teardown removed the tenant from `TenantResources`, so the ordinary first-sight path rebuilds it from scratch against the new credentials (the old ones are deleted on suspend and never reused). No new provisioning code — only a third trigger for the same idempotent path.

- **Enforced in:** `dictionary/composition.go`'s `mono.NC().Subscribe("notify.accounts.account.reactivated", ...)`, wired right after the BR-031 subscription; `dictionary/internal/rest/tenant.go`'s existing `EnsureTenantByName` (unchanged). `accounts-service`'s `Handlers.publishAccountReactivated` (BR-AC10) is the producer.
- **Test:** `'a torn-down tenant is fully restored by EnsureTenantByName, closing the suspend/reactivate round trip'` (`dictionary/tenant_switch_test.go`) — drives the whole round trip against the real shipped `nats.conf`/creds: answering → torn down and genuinely dark → rebuilt and answering again, with `SwitchTenant` never called. It then asserts the rebuilt tenant is *functional*, not merely responding, by arriving a ship through `api.*` and waiting for it to land in the read model — proving the projectors came back too, not just the `browserrpc.Adapter`. BR-AC10's own specs (`handler_test.go`, accounts-service) cover the publish side and its creds-file ordering.

> **Test-race note (2026-08-03).** The BR-030/BR-031/BR-032 specs poll with
> `Eventually` rather than issuing a single request after `EnsureTenantByName`.
> `micro.AddService` does not flush its subscriptions before returning, so a
> request issued in the same instant can still get `no responders` — which made
> BR-030's spec fail roughly one run in four. Production never races this (the
> rebuild is driven by an async `notify.*` delivery, not by a caller waiting on
> it), so polling tests the real invariant without encoding a startup race as a
> hard timing requirement.

---

### BR-033 (Phase 16k) — Sea Freight Flow tells the operator the truth about its connection: an accurate status badge, and failures that name the cause rather than the transport symptom

**A presentation rule, scoped to Sea Freight Flow's browser UI** — a follow-on from BR-031, prompted by a `Depart failed / not connected` toast observed on a suspended account. The behaviour was correct (fail-closed: a suspended tenant must not be able to depart a ship) but the app communicated it badly, in two separate ways:

1. **The status badge lied.** There are two independent `connected` flags — `useNatsConnection`'s, which correctly clears when NATS evicts the connection, and `usePortStore().connected`, which is cleared *only* by the store's own `disconnect()`, something nothing calls on eviction. The topbar read the latter alone, so after a suspension it showed a green **watching** badge directly beside the red **connection error** badge BR-031 had just added — the app claiming to be live and broken at the same time.
2. **Command failures named the symptom, not the cause.** `request()` threw a bare `not connected` whenever `nc` was null, so every panel's catch block toasted that. The real reason — auth-service's `tenant is not active` — was already known and sitting in `lastError`, but only reachable via the status badge's tooltip.

Both are fixed at the source rather than per-panel: `watching` is now `natsConnected && store.connected`, and `request()`/`subscribe()` throw `lastError` in preference to the bare fallback, so every command (arrive, depart, register, load, unload) inherits the better message at once. `lastError` itself now stores `err.message` rather than `String(err)`, dropping the `Error: ` prefix that would otherwise leak into operator-facing text.

- **Enforced in:** `frontend/seafreight-app/src/App.vue`'s `watching` computed (`data-testid="connection-status"`); `frontend/seafreight-app/src/nats/useNatsConnection.js`'s `notConnectedError()` (used by both `request()` and `subscribe()`) and `errorMessage()`.
- **Test:** `'shows disconnected when the NATS connection drops, even while the port store still believes it is connected'` (`App.spec.js`) — drives the exact contradictory state the bug produced and asserts the badge now reads `disconnected`. `nats/useNatsConnection.spec.js` (new file, three specs) — the bare fallback when nothing is known, auth-service's refusal surfacing in its place once `lastError` is set (asserting the toast text no longer mentions the transport symptom at all), and the same for `subscribe()`. These never open a connection, so `nc` stays null and the guard under test is the only path exercised — no NATS server or client mocking needed.

**Not covered by this rule:** disabling the action controls while disconnected, so an operator can't click *Depart* into a guaranteed-failing toast in the first place. That was considered alongside these two and deliberately deferred — it's a broader interaction change across every panel, where these two were the actual correctness bugs.

---

### BR-034 (Phase 27) — The Admin UI's Account Activity view shows per-account NATS traffic, and renders slow_consumers as a silent-until-nonzero alarm rather than a routine stat

**A presentation rule, scoped to the Admin UI only** — it governs what the Account Activity view *displays*, proxying the NATS server's own `/accstatz` monitoring endpoint (`GET /api/nats/account-activity`), same family as Connections' `/connz`/`/varz` and Services' `$SRV.STATS` (BR-028). `/accstatz` reports, per account: connection/subscription counts, sent/received message and byte volume, and `slow_consumers` — a count of subscriptions the server is currently dropping messages to because the client isn't draining fast enough. Every field except the last is routine traffic; `slow_consumers` is the one number an operator has to act on.

The rule: at `slow_consumers: 0` (every account, all day, on a healthy stack) the row says nothing about it at all — no permanent "0 slow" tile competing with real numbers, matching the "facts that only matter in an exceptional state get rendered only in that state" precedent `ConnectionsPanel`'s paged-note already established (`admin_stat_card_one_ratio_rule.md`). The moment an account's `slow_consumers` is nonzero: its status dot turns from green to red, its card border tints red, its "subs" stat is replaced by a red "slow" stat, and expanding the card opens on a named alarm line ("N slow consumers on this account right now…") instead of a bare column. A summary-row banner above the card list appears under the same condition, naming the total slow-consumer count and how many accounts are affected — also absent while every account is healthy.

Account labeling reuses BR-028's mechanism (as amended by Phase 30h): `/accstatz`'s `acc` identifier is resolved to a friendly tenant label via `AccountsClient.Labels` — a secondary, best-effort `GET /api/accounts` read against accounts-service, not the original `/connz` probe — falling back to the raw identifier when this process can't identify the account. A failed probe still costs the caller only the label, never the activity rollup itself, the same secondary-read pattern `/varz` uses for Connections' capacity ceiling.

> **Phase 30h amendment — moved to `observability-service` alongside Connections/Services, same mechanism change as BR-028.**

> **Phase 45 amendment — panel retired as a standalone SYSTEM · NATS nav item; its content is now Accounts' `Overview` tab** (`AccountsOverviewPanel.vue`, superseding the deleted `AccountActivityPanel.vue`). The slow-consumers-as-alarm rule above is unchanged; only its location moved, alongside `Accounts` itself moving PLATFORM → SYSTEM (see `ARCHITECTURE-ADMIN.md`'s Accounts section). The expand-to-detail interaction changed too — see BR-043, which replaces the flat number grid this rule originally described with trend charts.

- **Enforced in:** `observability-service/observability/internal/rest/nats_connections.go`'s `listNatsAccountActivity`; `frontend/admin/src/components/AccountsOverviewPanel.vue`
- **Test:** `TestListNatsAccountActivityReshapesAndSortsAccstatz`, `TestListNatsAccountActivityResolvesTenantLabel`, `TestListNatsAccountActivitySurvivesAccountsServiceFailure`, `TestListNatsAccountActivityReturns502WhenMonitoringEndpointUnreachable`, `TestListNatsAccountActivityReturns502OnMalformedBody` (`observability-service/observability/internal/rest/nats_connections_test.go`) — reshaping/sorting, tenant-label resolution off the accounts-service read, and that a failed label probe doesn't cost the activity rollup. `AccountsOverviewPanel.spec.js` (`frontend/admin/src/components/`) — the silent-at-zero / alarm-at-nonzero contrast: no banner/crit-styling/slow-stat while healthy, and all four (banner, red dot, tinted card, slow stat, alarm line) present the moment `slowConsumers > 0`.

### BR-035 (Phase 28) — The Request/Reply & Traces panel renders one row per trace, separates the synchronous reply from the eventual read-model tail, and marks account-boundary crossings

**A presentation rule**, scoped to the `[traces]` view of the evolved Request/Reply & Traces panel (`RpcPanel.vue` + new `TraceWaterfall.vue`) — the `[messages]` view (the flat per-subject log BR-D36/BR-026 already govern) is unchanged and still answers "is anything arriving on this subject." The traces view groups every `obs.trace.*` span by `traceId` into one waterfall row, not one row per span and not one row per message: a `browser → shipping-service → refdata-service` call that today shows as two unrelated Request/Reply rows becomes one trace with a parent/child span chain.

The **reply-ack boundary** is the moment the synchronous caller's span ends (`sp.End()`/`sp.Fail()` on the reply-side span) — everything drawn after that line is async work the request triggered but the caller was already unblocked for: `evt.*` projections, `notify.*`-derived reads, KV writes. The **account gutter** marks every point a trace's spans cross a NATS account boundary (e.g. a tenant account calling into PLATFORM's `refdata-service`), the same boundary BR-AC30 has to keep open for `obs.trace.>` itself. The header states two durations, not one: *reply latency* (client-observed, ends at the ack boundary) and *read-model-consistent duration* (ends when every projection the trace's async tail touched has caught up) — the second is always ≥ the first, and rendering only one would either hide the async cost entirely or misrepresent it as blocking the caller.

This obeys the exceptional-state rendering rule BR-034 established (ARCHITECTURE-COMMUNICATIONS.md §2.2: facts that only matter in an exceptional state render only in that state): **a trace with no async tail renders no ack line at all** — not a zero-width one — because a rejected command or a pure query legitimately has no tail, and a permanent "no tail" marker on every such trace would be noise competing with the traces that do have one.

- **Enforced in:** `frontend/admin/src/components/RpcPanel.vue` (`[traces]`/`[messages]` toggle); `frontend/admin/src/components/TraceWaterfall.vue` (new, Phase 28g)
- **Test:** `TraceWaterfall.spec.js` (`frontend/admin/src/components/`) — one row per `traceId` from a set of spans with a shared trace but different span ids; the ack-line/no-ack-line contrast (present when an async tail exists, absent for a tail-less trace); the account gutter renders a crossing marker only where consecutive spans in the chain carry different account labels; the header renders both durations and read-model-consistent ≥ reply latency.

> **Phase 28g amendment — the account gutter is a coarse PLATFORM/TENANT split (`accountOf` in `TraceWaterfall.vue`), not the per-tenant label ("ACME", "ACME → PLATFORM") this rule's prose implies.** `traceSpan` (BR-036) never serializes a per-span context/tenant field — only `StartOutbound`'s caller-supplied `contextValue` is used to build the `obs.trace.{context}...` *publish subject*, never stored in the payload itself — so there is no wire signal for which specific tenant a span ran under, only which `service` published it. `accountOf` therefore classifies by a small fixed set (`refdata`, `accounts` → `PLATFORM`; everything else → `TENANT`), which still surfaces this rule's crux scenario (a tenant service crossing into a PLATFORM service) but cannot distinguish ACME from GLOBEX. Upgrading this to a real per-tenant label would require adding a context field to the wire span — not done in this phase. Separately, the detail pane's OTel `spanKind` (client/server/producer/consumer/internal) from the design mockup is omitted entirely rather than shown with a fabricated value: `direction` is always `"reply"` (BR-037's one-span-per-call design), so there is no wire signal for it either. Verified against the live docker-compose stack (not just the unit test above): real `obs.trace.*` traffic from refdata-service/accounts-service/the Admin UI's own `api.*`/auth calls populated the `traces` KV bucket end to end, and both the bootstrap fetch and the live `notify._platform.kv.traces.>` feed were observed working (the visible trace count grew live while the panel was open, with no page reload).

### BR-036 (Phase 28) — The `obs.trace.*` envelope is a strict superset of `obs.rpc.*`/`obs.api.*`, publishes to the PLATFORM account only, and redacts before it truncates

`traceSpan` extends `obsEnvelope` (BR-D26, BR-D36) — every existing field (`direction`, `correlationId`, `subject`, `payload`, `error`, `headers`, `timestamp`, `payloadBytes`) keeps its name and type unchanged, and `correlationId` is retained as-is: it is a per-hop field, **not** the span id. New fields, all `omitempty`, mirror the OTLP `Span` message 1:1: `traceId` and `spanId` (both mandatory in practice, `omitempty` only so a pre-Phase-28 envelope still decodes), `parentSpanId` (absent on a root span), `service`/`entity`/`action` (the taxonomy positions the wire subject already encodes), `statusCode`/`statusMessage` (OTLP's own naming, not a bespoke ok/error enum), `attributes` (a flat key/value map — this is where `rpc.retry_count`, BR-037, lives), `redacted` (the list of field names a denylist stripped from `payload` before truncation — never silently dropped with no trace they existed), and `truncated` (`true` the moment the 4 KiB cap trims `payload`; `payloadBytes` always holds the **pre-truncation** length, so a truncated span's true size is still knowable even though its body isn't).

> **Phase 28g amendment — `durationMs` (not `omitempty`) added to `traceSpan` so the Admin UI's trace waterfall panel can draw proportional bars.** `Timestamp` was always only the span's *finish* moment (when `End`/`Fail` calls `finish()`) — there was never a wire "start" timestamp, so no consumer could compute a span's own elapsed time, only the gap between two DIFFERENT spans' finish times (which conflates queueing/scheduling gaps with actual work). `Span` in all five services' `natstrace.go` copies gained an unexported `startedAt time.Time`, stamped at construction (`Start`/`StartFromHeaders`/`StartOutbound` — whichever subset a given service's copy has), and `finish()` computes `DurationMs: time.Since(sp.startedAt).Milliseconds()`. Deliberately not `omitempty`: `0` is a legitimate fast span, not an absent field, and a pre-28g consumer decoding this envelope still ignores an unrecognized field harmlessly either way. A span's own start time remains recoverable if ever needed (`Timestamp` minus `DurationMs`), so no second wire timestamp field was added.

- **Enforced in (Phase 28g):** every service's `internal/natstrace/natstrace.go` (or `refdata/internal/natstrace/natstrace.go` etc.) — `Span.startedAt`, and `finish()`'s `DurationMs` computation.
- **Test (Phase 28g):** each service's `natstrace_test.go` gained a "records the span's own measured duration" spec — a handler sleeps a fixed duration before calling `End`, and the decoded span's `DurationMs` is asserted `>=` that duration (proving it reflects real elapsed construction-to-finish time, not a hardcoded/zero value).

> **Phase 28h amendment — `requestPayload`/`requestPayloadBytes`/`requestRedacted`/`requestTruncated` added to `traceSpan`, closing the "reply-only" gap this rule's field list originally described.** Every constructor now captures the inbound request body at span-start time — `Span.reqPayload []byte`, set by `Start(req)` (`req.Data()`, the four `micro.Request`-based copies), `StartFromHeaders(..., payload []byte, ...)` (a JetStream `Consume` callback's `msg.Data()`, or a `*nats.Msg`'s `.Data` field), `StartOutbound(..., payload []byte, ...)` (the caller-supplied outbound body, e.g. `refdataconsumer`/`refdataclient`'s `requestRPC`'s own `body`), and accounts-service's HTTP-only copy's `HTTPMiddleware`, which buffers `r.Body` via `io.ReadAll` before `next.ServeHTTP` and restores an equivalent `io.NopCloser` reader so the wrapped handler still sees the real bytes. `finish()` runs the exact same redact-then-truncate pipeline (BR-036's ordering) independently on both payloads via a shared `preparePayload` helper — a large request can truncate while a small reply doesn't, and vice versa — which is why the request side gets its own `requestRedacted`/`requestTruncated` fields rather than reusing `redacted`/`truncated`. For accounts-service's `publishAccountEvent` (a fire-and-forget `notify.accounts.*` publish, not a real request/reply), the "request" and "reply" payloads are simply the same published bytes — there's no second value to capture, so both fields end up identical for that span, which is correct rather than a special case. All four `requestX` fields are `omitempty`, so a pre-28h consumer decoding this envelope is unaffected. Closes the gap `TraceWaterfall.vue`'s "Request body — not captured yet" note used to flag; that note and its now-obsolete `.tw-note-strip` styling are removed, replaced with a real "Request body" section next to "Response body," both reading directly off the span's own fields.

- **Enforced in (Phase 28h):** every service's `internal/natstrace/natstrace.go` (or `refdata/internal/natstrace/natstrace.go`, `accounts-service/internal/natstrace/natstrace.go`) — `Span.reqPayload`, the `payload []byte` parameter added to `StartFromHeaders`/`StartOutbound`, `preparePayload`, and `finish()`'s request-side fields; every external call site of `StartFromHeaders`/`StartOutbound` across all five services (`dictionary/internal/eventhandler/*.go`, `dictionary/internal/rest/tenant.go`, `internal/refdataconsumer/consumer.go`, `tradingpartner/internal/tenants/tenants.go`, `tradingpartner/internal/refdataclient/client.go`, `pricing/internal/tenants/tenants.go`, `accounts/handler.go`'s `publishAccountEvent`); `frontend/admin/src/components/TraceWaterfall.vue`'s two `.tw-body-full` sections (`selectedSpan.requestPayload` / `selectedSpan.payload`).
- **Test (Phase 28h):** each service's `natstrace_test.go` asserts a captured request payload round-trips through `requestPayload` (and is independently redacted/truncated from the reply payload); `TraceWaterfall.spec.js`'s "renders both a Request body and a Response body section from the span's own fields" spec pins the two-section UI directly.

> **Phase 30j amendment — an `evt.*` projector span's reply-side `payload` is now `nil`, not a copy of the event bytes it consumed.** The three shipping-service `Consume` callbacks (`dictionary/internal/eventhandler/handler.go`, `container_handler.go`, `meta_handler.go`) called `sp.End(msg.Data(), nil)`/`sp.Fail(err, msg.Data(), nil)`, passing the same raw event bytes already captured as `requestPayload` (Phase 28h) back in as the reply `payload` too. Unlike the Phase 28h amendment's `publishAccountEvent` case — a genuine fire-and-forget publish, where request and reply really are "the same published bytes" because that's the only value that ever existed — a JetStream projector consuming an `evt.*` message never produces a reply at all: `msg.Ack()`/`msg.Nak()` is flow control, not a response payload. Echoing the event back made `TraceWaterfall.vue`'s Response body section show byte-identical Request/Response panes for every event-consumer span, implying a reply that was never sent. `preparePayload(nil)` already returns a nil `Payload` (dropped by its `omitempty`) and `payloadBytes: 0`, and `highlightJson`/`TraceWaterfall.vue` already fall back to `—` for a nil payload (added for the still-unfinished-span case these three spans can never actually be), so no encoding or UI change was needed — only the three call sites.

- **Enforced in (Phase 30j):** `dictionary/internal/eventhandler/handler.go`, `container_handler.go`, `meta_handler.go` — `sp.End(nil, nil)`/`sp.Fail(err, nil, nil)` in place of echoing `msg.Data()`.
- **Test:** existing `dictionary/trace_async_test.go` (BR-037, Phase 28d) specs already assert each projector callback publishes exactly one span per message with the correct `service`/`entity`/`action`/`entity_id` labeling and continue to pass unchanged; verified live against a real docker-compose trace (`evt....registered`/`evt....arrived` spans) that the Response body section now renders `—` instead of a copy of the request body.

> **Phase 28i amendment — a span records who requested it, not only who answered; and header names are now redacted the same way payload keys are.** Two independent gaps left every non-`Start()` span reporting "no Nats-Requestor" even when the identity was known: (1) `StartFromHeaders` was handed the inbound `nats.Header` but read only `traceparent` out of it and discarded the rest, so every `evt.*` projector span and `notify.accounts.*` lifecycle span dropped its caller identity — fixed by retaining `reqHeaders` from the headers it already receives; (2) a `StartOutbound` span cannot capture its own headers at construction, because the caller can't build them until it has the span (`Traceparent()` is one of the values that goes in), so the client-side span published `Nats-Requestor` on the wire — the callee's span proves it arrived — while showing nothing for itself, the one hop that definitively knows the requestor. Fixed with `Span.SetRequestHeaders(map[string][]string)`, called at each of the three outbound sites right after the header map is built. Retaining full inbound header sets then made header redaction load-bearing rather than theoretical: accounts-service's `HTTPMiddleware` hands over whatever the browser sent, which is exactly where an `Authorization`/`Cookie` value would ride into a trace — so `finish()` now runs `redactHeaders` over the merged map, stripping any header whose name is in the same case-insensitive `redactDenylist` that already guards payload keys and recording each as a dotted `headers.<Name>` entry in the existing `redacted` list (no new wire field; `headers.Authorization` reads as the same kind of path as a nested payload key). The remaining legitimately-empty requestor is an HTTP entry point, which has no `Nats-Requestor` by construction — a UI wording matter, not a missing value.

- **Enforced in (Phase 28i):** every service's `natstrace.go` — `StartFromHeaders`'s `reqHeaders` retention, `Span.SetRequestHeaders`, and `redactHeaders` in `finish()`; the three outbound call sites (`internal/refdataconsumer/consumer.go`, `tradingpartner/internal/refdataclient/client.go`, `accounts/handler.go`'s `publishAccountEvent`).
- **Test (Phase 28i):** each service's `natstrace_test.go` gained a "every span records who requested it" context — a `StartFromHeaders` span retains an inbound `Nats-Requestor`; a `StartOutbound` span carries both identities after `SetRequestHeaders`; and a denylisted header is stripped from the published span (asserting the secret value appears nowhere in the raw wire bytes) while a non-denylisted one survives. accounts-service additionally pins the HTTP path end to end: an inbound `X-Actor` is retained, an inbound `Authorization` never reaches the wire. Verified live against the docker stack — both the parent (`refdata.type.list.v1`) and child (`rpc.acme.refdata.type.list.v1`) spans of a real shipping→refdata call now carry `Nats-Requestor: shipping-service/…`, where the parent previously carried none.

> **Phase 28i amendment (UI) — the detail pane's request/response split now runs the full height of the pane as one continuous grid, not a horizontal identity strip followed by a bordered two-column block followed by two stacked full-width bodies.** The old layout's `→`/`←` markers kept asserting a horizontal axis that the body sections had already abandoned partway down, so the eye had to switch direction mid-pane. `TraceWaterfall.vue`'s new `.tw-rr-grid` carries request always-left/response always-right through identity, headers, attributes, and body alike, joined by one `.tw-rr-seam` divider spanning every row — auto-placed so a tall response body can never drag its request counterpart out of row alignment, each body scrolling inside its own cell instead. Because position is now the reliable signal, the axis is named once in a `→ REQUEST`/`← RESPONSE` caption pair rather than on every sub-label — `Request headers`/`Response headers`/`Request body`/`Response body` all drop their prefix down to `headers`/`body`. Two mockups (`demos/01-dictionary/diagrams/trace-detail-axis-options.html`) were reviewed before implementation; the alternative (full-width stacked bands per direction) was rejected for losing side-by-side header/body comparison, and is effectively what this layout already collapses to below 860px. Separately, the empty-identity copy now distinguishes a real gap from an unavoidable one: an HTTP-transport span (accounts-service, subject always a `/`-prefixed URL path) reads "HTTP entry point — no NATS requestor" rather than the same wording a genuine `rpc.*`/`api.*`/`evt.*` span with a missing requestor would show — the latter case no longer exists after the fix above, but the wording no longer conflates "expected" with "bug" going forward.

> **Phase 28j amendment (UI) — the waterfall column now reads as two labeled, independently-scrollable cards, "Span list" and "Span details," instead of one undifferentiated stack the whole pane scrolled together.** `TraceWaterfall.vue`'s `.tw-wf-body` splits into `.tw-panel-list` (the time axis + waterfall rows, unchanged content) and `.tw-panel-details` (the span detail pane, unchanged content) via a CSS grid whose row heights are `${spanListHeight}px 6px minmax(0, 1fr)` — a draggable `.tw-vresize-handle` between them (mouse-drag + arrow-key resize, mirroring the existing trace-rail handle) lets the height split move, persisted in `ui.spanListHeight` (Pinia, same tear-down-survival reasoning as `traceRailWidth`) rather than a local ref. No data or field changed — this is presentation grouping only, informed by a reviewed concept mockup (a span-list-over-span-details Jaeger/Tempo-style layout) but adapted to this panel's existing dark card language rather than copied verbatim. Separately, **any top-position tab strip on a right-side detail panel must now be a real PrimeVue `Tabs` with `class="panel-tabs"`**, not a custom chip/pill toggle — `RpcPanel.vue`'s `[traces]`/`[messages]` toggle (previously a `.chip` pair, see the Phase 28g note in BR-035's main body above) is migrated to `Tab`/`TabList`/`TabPanels`/`TabPanel`, matching `AccountsView.vue`'s Provisioning/Topology tabs exactly since both now share the one `.panel-tabs` rule in `shared/unifi-theme/unifi.css` (see `shared/unifi-theme/LAYOUT.md`'s "Panel top tabs" section for the full rule).

- **Enforced in (Phase 28j):** `frontend/admin/src/components/TraceWaterfall.vue` (`.tw-wf-body`/`.tw-panel-list`/`.tw-panel-details`/`.tw-vresize-handle`, `spanListHeight` resize handlers); `frontend/admin/src/stores/ui.js` (`spanListHeight`); `frontend/admin/src/components/RpcPanel.vue` (Tabs migration); `frontend/admin/src/components/AccountsView.vue` (`panel-tabs` class, local override removed in favor of the shared one); `shared/unifi-theme/unifi.css` (`.panel-tabs` rule); `shared/unifi-theme/LAYOUT.md` ("Panel top tabs" convention).
- **Test (Phase 28j):** `TraceWaterfall.spec.js`'s existing specs (trace/row/detail selectors) pass unchanged against the new nested structure, confirming the regrouping is presentation-only; `RpcPanel.spec.js`'s existing specs pass unchanged against the Tabs migration (they drive `ui.rpcTab` directly rather than clicking the old toggle, so the markup swap is transparent to them). Verified live against the docker stack: the Traces/Messages tab strip now renders identically to Accounts' Provisioning/Topology (same underline weight/color/spacing), and the Span list/Span details divider drags and resizes correctly.

> **Phase 28k amendment — a span's waterfall-row position (offset, and therefore sort order) now preserves sub-millisecond precision, fixing a real case where a child span rendered ABOVE its own parent.** `span.timestamp` carries the backend's full precision (typically nanoseconds, e.g. `...265438763Z`), but `TraceWaterfall.vue`'s `ownStart`/`ownFinish` computed it via `new Date(span.timestamp).getTime()`, which truncates to whole milliseconds. For a fast parent/child pair (routine at 1-2ms per hop) whose finish times are a fraction of a millisecond apart, that truncation can collapse both spans' *computed start* to the identical millisecond — ties then fall through to `Array.prototype.sort`'s stability guarantee, which preserves `t.spans`' original array order (publish/insertion order) instead of the causal parent-before-child order the waterfall is supposed to show. A real captured trace reproduced this exactly: parent finish `.266084722Z` (duration 2ms) and child finish `.265438763Z` (duration 1ms) both truncated to a start of `264`ms, so the child (which happened to be array index 0) rendered above its parent, indented one level as if it were the parent's own child sitting confusingly above it. Fixed by parsing the fractional-second digits directly from the ISO string (`preciseFinishMs`) instead of routing through `Date.getTime()` — the true, unrounded delta (parent starts before child by however many microseconds the network hop actually took) now sorts correctly every time, not just when spans happen to be far enough apart to survive millisecond rounding. `depthOf`'s tree-depth computation (and therefore each row's indent-rail count) was never wrong — only the row *order* was — so a merely-reordered waterfall could still show a correctly-indented child sitting confusingly above an unindented parent, which is what made the bug read as "indentation is reversed" at a glance.

- **Enforced in (Phase 28k):** `frontend/admin/src/components/TraceWaterfall.vue` — `preciseFinishMs`, `ownStart`/`ownFinish`.
- **Test (Phase 28k):** `TraceWaterfall.spec.js`'s "orders a parent above its child even when their finish timestamps round to the same millisecond" spec, built from the exact real timestamps that reproduced the bug — asserts row order (`SubjectPath` subjects) and indent-rail count (0 for the parent, 1 for the child). Verified live: the real trace that originally surfaced this (via a live screenshot) now renders parent-first after the fix, unchanged after rebuild.

> **Phase 28n amendment — Phase 28k fixed `ownFinish`'s precision but left a second, independent truncation source: `durationMs` itself is whole-millisecond-truncated *server-side* (Go's `time.Duration.Milliseconds()` in every `natstrace.go` copy's `finish()`), and `ownStart = ownFinish - durationMs` inherits that error no matter how precise `ownFinish` is.** Once the Phase 28m HTTP-tracing gap closed above, a real 3-span trace surfaced this: an HTTP root span (`/api/refdata/types/string`, true duration ~66.6ms) wraps an outbound `rpc.*` client span (`refdata.type.list.v1`, true duration ~66.3ms) which wraps the server-side reply (`rpc.acme.refdata.type.list.v1`, 29ms) — both truncate to the *same* `durationMs: 66`. Subtracting an identical truncated duration from each span's own (precise) finish time preserves their finish-time ordering (root finishes after its child — correct) but not their *start*-time ordering, since the root's estimated start is `rootFinish - 66` and the child's is `childFinish - 66`, and `rootFinish > childFinish` makes the root's estimate the *later* one — inverting a relationship `parentSpanId` already proves true, no clock precision involved at all. The previous fix (a flat `.sort((a,b) => a.offset - b.offset)` over every span in the trace) trusted `offset` completely regardless of the known parent/child DAG; `waterfallRows` now instead walks the `parentSpanId` tree in pre-order (a span always immediately precedes its own subtree) and uses `offset` only to order *siblings* relative to each other, never to reorder a span relative to its own ancestor or descendant. This makes parent-before-descendant structural rather than incidental, closing this entire class of bug (both this truncation source and any future one) rather than chasing individual precision leaks.

- **Enforced in (Phase 28n):** `frontend/admin/src/components/TraceWaterfall.vue`'s `waterfallRows` — replaced the flat span sort with a `childrenByParent`-grouped pre-order tree walk (siblings still sorted by `offset`).
- **Test (Phase 28n):** `TraceWaterfall.spec.js`'s "walks the parentSpanId tree instead of flat-sorting by offset" spec, built from the exact real timestamps/durations of the trace that surfaced this (an HTTP root and its direct `rpc.*` child both truncating to `durationMs: 66`) — spans are fed in deliberately scrambled array order to prove the fix reads the tree, not insertion order, and asserts both row order and each row's indent-rail count (0/1/2). Verified live against the exact reproducing trace.

- **Enforced in (Phase 28i UI):** `frontend/admin/src/components/TraceWaterfall.vue` — the `.tw-rr-grid`/`.tw-rr-seam`/`.tw-rr-cap`/`.tw-rr-cell`/`.tw-rr-sep` layout (replacing `.tw-who`/`.tw-det-cols`/`.tw-det-col`/`.tw-body-full`) and the `requestedByEmptyLabel` computed.
- **Test (Phase 28i UI):** `TraceWaterfall.spec.js`'s "renders a request body and a response body in the same request|response split as identity/headers" spec pins the unified grid directly. Verified live against the docker stack (rebuilt `admin-frontend`) — a real `rpc.acme.refdata.type.list.v1` span's identity/headers/body all render aligned under one continuous seam, and an `/api/accounts` span's request-side identity reads "HTTP entry point — no NATS requestor".

Every `obs.trace.{context}.{service}.{entity}.{action}` publish happens on the **PLATFORM account only** — a tenant account never sees its own trace payloads, only PLATFORM's cross-account trace store does, and no browser credential is ever granted `Sub.Allow` for `obs.trace.>` (BR-AC30 enforces the JWT side of that). Redaction runs **before** the 4 KiB cap, not after: truncating first and redacting second could leave a partially-redacted field's tail bytes sitting in the truncated payload. Like `obs.rpc.*`/`obs.api.*` before it (BR-D26), a failure to publish a trace span must never block or fail the business path it describes — tracing is diagnostic, not transactional.

- **Enforced in:** `dictionary/internal/natstrace` (new package, Phase 28a-28b) — `Tracer.publish()`'s redaction-then-truncate ordering and the `traceSpan` struct; `micro.Service` registration only ever running on a tenant or PLATFORM connection matching the intended account.
- **Test:** `natstrace` package specs (`dictionary/internal/natstrace/natstrace_test.go`) — a `traceSpan` round-trips through the OTLP-shaped fields; an old-shape `obsEnvelope` with none of the new fields still decodes; a payload over 4 KiB is truncated with `truncated: true` and `payloadBytes` equal to the original (pre-truncation) length; a denylisted field lands in `redacted` and not in `payload`. Cross-service contract test (BR-D39/BR-P25/BR-TP15's shared clone) asserts the same shape decodes identically in all five services.

> **Phase 28f amendment — "PLATFORM's cross-account trace store" named above is the `TRACES` JetStream stream + `traces` KV bucket, provisioned and projected by shipping-service.** `dictionary/internal/eventhandler/trace_store.go`'s `RegisterTraceStore` creates the `TRACES` stream (`obs.trace.>`, `LimitsPolicy`, 1-hour `MaxAge`, 64 MiB `MaxBytes` — short-lived like the `RPCTRACE` side channel it's replacing, not the unbounded `SHIPPING` stream) and a durable consumer that merges every span into one KV entry per `traceId`: `appendSpan` reads the existing `_platform.trace.{traceId}` record (if any), skips the write entirely if the incoming span's `spanId` is already present (an at-least-once redelivery, not a new span), and otherwise appends the raw span JSON to the record's `spans` array — merge, never overwrite-with-latest, since a trace typically has more than one span (an inbound call plus at least one outbound hop it causes, BR-037). This must run on the **unrestricted** PLATFORM connection (`monolith.Monolith.PlatformFullJS()`, wired from `dictionary/composition.go`), never the restricted shipping-admin `mono.JS()` — creating a stream and a KV bucket is a write to `$JS.API.>`, which shipping-admin's `nats/bootstrap-operator.sh` grant deliberately excludes (`TestShippingAdminCanOnlyUseNarrowOrderedConsumerAccess`). Nil-safe exactly like `RegisterRefdataNotify`/`RegisterRPCTraceNotify`: if `platform.creds` isn't configured, `RegisterTraceStore` is a no-op rather than failing `Startup`. The cross-account leg that gets a tenant's spans onto PLATFORM's `TRACES` stream in the first place is BR-AC30's account-level export/import wiring (`accounts/provisioner.go`'s `tenantExports`/`addPlatformTraceImport`, or `bootstrap-operator.sh`'s day-0 equivalent for the pre-seeded ACME/GLOBEX tenants) — this rule only covers what happens to a span once it's already landed on PLATFORM.
>
> **Phase 28g amendment — the KV bucket is wrapped in `internal/kvstore.Store`, not raw `jetstream.KeyValue`, specifically to get its Admin-UI plumbing for free.** The bucket is named bare (`traces`, not `traces-_platform`) matching `kvstore`'s own "one bucket per role, named by the prefix alone" convention, with the platform scope folded into the KEY instead — every `appendSpan` call goes through `store.Get`/`store.Put(ctx, "_platform", "trace."+traceID, ...)`, so the real on-wire key is `_platform.trace.{traceId}`. This means `RegisterTraceStore` also calls `store.EnableNotify(platformNC, log)`, so every trace write already publishes `notify._platform.kv.traces.trace.{traceId}.changed` — the exact same mechanism (and the same generic `GET /api/kv/buckets`/`GET /api/kv/buckets/{account}/{bucket}/entries` REST endpoints, `rest/kv.go`) every other KV panel in the Admin UI already uses. No new REST endpoint and no new KV-watch bridge goroutine were needed for the trace waterfall panel's bootstrap-then-live feed — a custom bridge was the first design considered and was replaced by this reuse before any panel code was written. This does require widening `auth/token.go`'s `MintAdminToken` to grant `Sub.Allow` for the new `notify._platform.kv.traces.>` subject (the browser's admin connection previously had no KV-notify grant of any kind) — no `bootstrap-operator.sh` change was needed on the publish side, since shipping-admin's existing `notify._platform.>` publish grant already covers it.

- **Enforced in (Phase 28f/28g):** `dictionary/internal/eventhandler/trace_store.go`'s `RegisterTraceStore`/`appendSpan` (built on `internal/kvstore.Store`); `dictionary/composition.go`'s wiring via `mono.PlatformFullJS()`/`mono.NC()`; `accounts-service/auth/token.go`'s `MintAdminToken` (widened `Sub.Allow`).
- **Test (Phase 28f/28g):** `dictionary/internal/eventhandler/trace_store_test.go` (embedded JetStream server, black-box) — a published `obs.trace.*` span lands in the `traces` KV bucket under `_platform.trace.{traceId}`; two spans sharing a `traceId` merge into one KV entry with both spans present, not the latter overwriting the former; a write publishes `notify._platform.kv.traces.>`; `RegisterTraceStore(ctx, nil, nc, log)`/`RegisterTraceStore(ctx, js, nil, log)` are both no-ops, no panic. `trace_store_appendspan_test.go` (white-box, direct `appendSpan` calls against a real `kvstore.Store`) — a redelivered span (same `traceId`+`spanId`) is deduplicated to one entry; two different traces never share spans in each other's KV record. `accounts-service/auth/token_test.go`'s pinned `MintAdminToken` spec asserts the new `notify._platform.kv.traces.>` grant alongside the existing three, and still asserts no `$JS.API`/`$KV`/`rpc.`/`api.` grant leaks in.

> **Phase 28l amendment — the KV bucket (only the KV bucket — the `TRACES` JetStream stream keeps its name) is renamed `traces` → `trace-request-reply`, at the user's explicit request to disambiguate this bucket from other "traces" concepts in the Admin UI.** `traceStoreBucket` in `trace_store.go` is the sole source of truth (`internal/kvstore.New(platformFullJS, traceStoreBucket)`), so the physical bucket, its `notify._platform.kv.trace-request-reply.>` change subject (`internal/kvstore.Store.EnableNotify`'s `"notify." + context + ".kv." + bucket + "." + key + ".changed"` derivation), and the REST path segment (`GET /api/kv/buckets/platform/trace-request-reply/entries`) all follow from the one constant. The one non-obvious consequence: `auth/token.go`'s `MintAdminToken` hardcodes the notify subject it grants the Admin UI's browser JWT (`Sub.Allow.Add(..., "notify._platform.kv.trace-request-reply.>")`) rather than deriving it from the bucket constant, so it had to be updated by hand alongside — missing it would have left live trace updates silently unreceived (the publish still succeeds; only the browser's NATS permission match fails, with no error surfaced anywhere) despite the bootstrap REST fetch working fine. The old `traces` bucket is orphaned (not migrated) — a fresh `trace-request-reply` bucket is created empty on first write, per `internal/kvstore`'s lazy `CreateOrUpdateKeyValue`.

- **Enforced in (Phase 28l):** `dictionary/internal/eventhandler/trace_store.go`'s `traceStoreBucket` constant; `accounts-service/auth/token.go`'s `MintAdminToken` grant; `TraceWaterfall.vue`/`RpcPanel.vue`'s `getKvBucketEntries`/`subscribePlatform` calls.
- **Test (Phase 28l):** `trace_store_test.go` and `accounts-service/auth/token_test.go` updated to assert against `trace-request-reply` throughout (bucket lookups, notify-subject strings, the pinned `MintAdminToken` `Sub.Allow` list).

> **Phase 30h amendment — the trace store moved to `observability-service`, and with it the mechanism for the write access Phase 28f flagged as the reason it needed the unrestricted PLATFORM connection.** `dictionary/internal/eventhandler/trace_store.go`'s `RegisterTraceStore` (using `mono.PlatformFullJS()`, the second, broader PLATFORM connection shipping-service held only for this purpose) is gone; `observability-service/observability/internal/tracestore/tracestore.go`'s `Register` does the same provisioning/projection on that service's one PLATFORM connection instead. Since that connection is otherwise the same narrowly-scoped shape as shipping-admin's (BR-AC31/BR-AC32, `BUSINESS_RULES-ACCOUNTS.md`), provisioning the `TRACES` stream and `KV_trace-request-reply` bucket needed a new, narrower grant — `$JS.API.STREAM.CREATE`/`UPDATE` scoped to exactly those two resource names, never a wildcard — rather than reaching for a second unrestricted connection the way shipping-service's original did; `nats/bootstrap-operator.sh`'s `observability` user carries this grant. `internal/kvstore.Store` was not ported — `tracestore.Register`/`appendSpan` inline just the `Get`/`Put`-with-notify slice actually used, since the `natstrace`-header-on-notify branch of `kvstore.Store.Put` was always a no-op at this call site (the consume callback never attaches a span to its context). `dictionary/composition.go`'s call to `RegisterTraceStore` is removed; shipping-service's `Startup` no longer touches `TRACES`/`trace-request-reply` at all. `accounts-service/auth/token.go`'s `MintAdminToken` grant is unaffected — the browser's admin JWT reads the KV bucket via `GET /api/kv/buckets/...` regardless of which backend process wrote it, and that route itself moved to `observability-service` alongside this Phase 30e (see `observability-service/observability/internal/rest/kv.go`'s own route table).

- **Enforced in (Phase 30h, amended 2026-08-17):** `observability-service/observability/internal/tracestore/tracestore.go`'s `Register`/`appendSpan`; `observability-service/observability/composition.go`'s wiring (`Startup` builds its own `jetstream.JetStream`, calls `tracestore.Register`, holds the returned `jetstream.ConsumeContext` for `Handlers.Stop`); `nats/bootstrap-operator.sh`'s `observability` user (`$JS.API.STREAM.CREATE.TRACES`/`UPDATE.TRACES`/`CREATE.KV_trace-request-reply`/`UPDATE.KV_trace-request-reply`, `$KV.trace-request-reply.>`, `notify._platform.kv.trace-request-reply.>`, and — found live after Phase 30i, since `KeyValue.WatchAll`'s push consumer periodically flow-control-acks on a server-generated `$JS.FC.<stream>.<inbox>` subject that isn't the same grant as a message Ack — `$JS.FC.KV_trace-request-reply.>`; without it the watch doesn't error, it just silently stalls once the consumer's flow-control window fills).
- **Test (Phase 30h):** `observability-service/observability/internal/tracestore/tracestore_test.go` (embedded JetStream server, new package — no direct prior-art file, since shipping-service covered this indirectly through its `eventhandler` suite) — multi-span merge into one record, redelivered-span dedup, the notify publish, a malformed span dropped without blocking later ones, and `Register`'s idempotency across two calls (simulating a restart).

> **Phase 28m amendment — shipping-service's REST layer (`dictionary/internal/rest`) had no HTTP-level tracing at all, so an outbound `rpc.*` call a handler triggers (`internal/refdataconsumer`'s `StartOutbound`) always found no span on `r.Context()` and minted a fresh, untraceable root span for itself — a trace like `GET /api/refdata/types/{type}` showed shipping-service as the apparent originator, with the real browser-originated HTTP request invisible.** `accounts-service` already closed this same gap for its own REST surface (`natstrace.HTTPMiddleware`, wrapped once at startup since its NC never changes); shipping-service's copy (`dictionary/internal/rest/trace_middleware.go`'s `httpTraceMiddleware`, a `*Handlers` method rather than a `*natstrace.Tracer` one) differs in one deliberate way — it reads `h.deps()` fresh on **every request** rather than closing over a single `Deps` snapshot, because shipping-service's NC changes on `SwitchTenant` (Phase 13b) where accounts-service's never does; this keeps every published span on the currently active tenant's own connection, tracking tenant switches like every other per-request field on `Deps`. Wired into every route in `Mount()` **except** `/api/refdata-watch` and `/api/nats/log` — both long-lived SSE streams that block for the connection's lifetime rather than one request/reply, so wrapping them would hold a span open indefinitely instead of the one-span-per-call shape BR-037 assumes — and except `/healthz`/`/swagger/` (infra, not business paths). A nil `deps.TenantNC` (no tenant resources yet) skips tracing entirely rather than risk a nil-pointer publish, matching every other natstrace entry point's best-effort/never-block-the-business-path convention (BR-036).

- **Enforced in (Phase 28m):** `dictionary/internal/rest/trace_middleware.go`'s `httpTraceMiddleware`/`statusRecorder`/`httpEntity`; `handlers.go`'s `Mount()`, routed through a `handle()` closure over every business route.
- **Test (Phase 28m):** `trace_middleware_test.go` — a wrapped handler publishes a span carrying the request's `service`/`entity` and `statusCode: OK` for a 2xx response, `statusCode: ERROR` for a 4xx/5xx one; a handler-level `StartOutbound` call made against `natstrace.SpanFromContext(r.Context())` publishes a child span whose `parentSpanId` equals the HTTP span's own `spanId` (the specific gap this closes — asserted directly, not just implied); a nil `TenantNC` still runs the wrapped handler with no panic and no span published. Verified live against the docker stack: `GET /api/refdata/types/ship-status` now produces a 3-span trace rooted at the real HTTP path, crossing TENANT → PLATFORM, with the HTTP span's request-side identity reading "HTTP entry point — no NATS requestor" (the existing convention Phase 28i's UI amendment established for accounts-service's own HTTP spans).

> **Phase 28o amendment (UI) — each trace is now marked REST or NATS, and the toolbar gained a third filter axis to match.** `traceKind(rootSubject)` classifies by the trace's ROOT span alone (`rootSubject?.startsWith('/')` — an HTTP entry point's subject is always the URL path `httpTraceMiddleware`/`httpEntity` publish under, Phase 28m; everything else, including a NATS trace that fans out into further NATS hops underneath, is `nats`), the same "classify by root, not by counting hop shapes" convention the trace list already used for search and the `n spans` summary. `summarize()` carries the result as `kind` on every trace summary; a `.kind-tag` renders it inline next to the subject in each row (`rest`/`nats`, violet/teal — Phase 28q briefly retuned REST to a yellow, then reverted it back to this violet at the user's request, see below — the same left-border-plus-tint vocabulary `.tw-acct`'s PLATFORM/TENANT tags already use — deliberately not `--lab-accent` or the TENANT tag's amber, so a kind tag is never mistaken for an account tag); and a three-way segmented `kind-group` control (`all`/`rest`/`nats`) sits in the toolbar alongside the existing `errors`/`slow` chips — segmented rather than a second independent boolean chip, since rest/nats is one axis with three states and two AND-combinable booleans could both read "on" with no way to tell that means "all" again. `displayedSummaries` gained one more filter clause (`kindFilter.value !== 'all' && t.kind !== kindFilter.value`), composing with the existing errors/slow/search clauses rather than replacing any of them. Separately, this exposed a latent bug in `SubjectPath.vue`'s NATS-only verb/id heuristic (last dot-token = accent-blue "verb", second-to-last = tinted "id"): a REST path has no dots at all, so index 0 is simultaneously "the last segment," and the WHOLE path rendered in the accent blue meant for a NATS action token. Fixed by branching on the same `/`-prefix check before the dot-split: a REST path now renders as one plain segment (`{ text, path: true }`, styled `var(--p-text-color)`, no verb/id highlighting), which is correct once the kind tag already carries the transport distinction — the color difference was redundant, not merely wrong.
>
> **Phase 28p amendment (UI) — a three-card "pulse strip" (request count, error count, avg latency) now sits directly under the toolbar, above the trace rail/detail split, summarizing the SAME `displayedSummaries` window the trace list below it already renders.** This panel has no historical metrics backend — only the live-buffered trace set — so each card buckets the window into a fixed 20 columns spanning however far back the buffer reaches (`(t.at - minAt) / span`), not a fixed calendar interval; a degenerate single-instant window (one trace, or several sharing one timestamp) is widened to a synthetic 30s span so the bucket math never divides by zero. Requests and Errors render as bar histograms (count per bucket, tallest bucket normalized to full height); Avg latency renders as a filled line (mean `replyMs` per non-empty bucket only — an empty bucket is skipped from the line rather than interpolated to zero, which would misrepresent "no data" as "latency dropped to nothing"). Because the strip reads `displayedSummaries` — the same post-filter array the trace list iterates — toggling `errors`/`slow >100ms`/the Phase 28o `rest`/`nats` control up in the toolbar reshapes the strip and the list together, one dataset with two simultaneous views, not a separate always-global metric that could silently disagree with what's on screen. The whole strip is `v-if`-gated on `displayedSummaries.length > 0`: a filter combination matching zero traces hides it outright rather than rendering a zero-height, zero-everything strip that would just be noise. All colors reuse existing tokens (`--lab-accent` for the two neutral volume/latency cards, the same `#e5484d` `.tw-dot.err`/`.chip.err` already use for the Errors card) — no new palette was introduced for this addition. A mockup (`traces-pulse-strip-mockup.html`, published as an Artifact) was reviewed with the user before implementation.
>
> - **Enforced in (Phase 28o):** `frontend/admin/src/components/TraceWaterfall.vue` — `traceKind`, `summarize()`'s `kind` field, `kindFilter`/`setKindFilter`, `displayedSummaries`' kind clause, the `.kind-group` toolbar control, the `.kind-tag` row marker; `frontend/admin/src/components/SubjectPath.vue` — the `/`-prefix branch in `segments` and its `.seg.path` styling.
> - **Test (Phase 28o):** `TraceWaterfall.spec.js`'s "tags each trace REST or NATS by its root span, and filters the list by transport" spec (trace `t5`, an HTTP-rooted fixture) — asserts each row's `.kind-tag` text and that clicking `[data-k="rest"]`/`[data-k="nats"]`/`[data-k="all"]` narrows/restores the visible row count.
> - **Enforced in (Phase 28p):** `frontend/admin/src/components/TraceWaterfall.vue` — the `pulse` computed (bucketing, histogram/line-point derivation) and the `.pulse-strip`/`.pulse-card` template block and CSS.
> - **Test (Phase 28p):** `TraceWaterfall.spec.js`'s "summarizes the currently displayed trace window into request/error/latency histograms, and reshapes with the toolbar filters" spec — pins exact request/error/avg-latency values against three fixture traces (2 ok, 1 error), asserts the "current" latency reads the newest trace by `at` rather than array order, asserts the errors chip narrows the strip to match the trace list, and asserts the strip disappears entirely under a filter combination with zero matching traces. Verified live against the docker stack (rebuilt `admin-frontend`): the strip renders real bucketed histograms from the live trace buffer and reshapes correctly when the `errors` toolbar filter is toggled.
>
> **Phase 28q amendment (UI) — the `.kind-tag.rest` color was briefly retuned then reverted, and switching between the [traces]/[messages] tabs no longer re-fetches or re-subscribes.** REST's color was first retuned from violet to a muted yellow (`#d1c85a`, picked as a true yellow — R and G close together, B low — versus the TENANT tag's `#e2b86b` amber, which skews orange); the user then asked for it back, so it's reverted to the original `#a78bfa` violet — no net color change from before Phase 28o, just a documented round trip. Separately, `RpcPanel.vue`'s `<TabPanel value="traces">` gated `<TraceWaterfall>` with a plain `v-if="ui.rpcTab === 'traces'"`, so every switch back to [traces] fully destroyed and recreated the component: a fresh `getKvBucketEntries` HTTP round-trip against the (unbounded, session-growing) `trace-request-reply` KV bucket, a full NATS re-subscribe, and every derived computed (`traceSummaries`, `displayedSummaries`, `pulse`, `waterfallRows`) recomputed from scratch — the reported toggling sluggishness. Fixed by wrapping the same `v-if` in a `<KeepAlive>`: the first switch to [traces] still mounts lazily (so a test suite that only ever sets `rpcTab = 'messages'` — see `RpcPanel.spec.js`'s `mountMessagesTab` — still never mounts `TraceWaterfall` and never races its `getKvBucketEntries` mock against `TraceWaterfall`'s own bootstrap call), but once mounted, `KeepAlive` caches the instance instead of unmounting it on the next switch away, so switching back reuses it with no repeat fetch/subscribe/recompute. The `[messages]` tab's own content div keeps its plain `v-if` — its data already lives in `RpcPanel`'s own reactive state (bootstrapped once, unconditionally, in the parent), so remounting its DOM on each switch was always cheap and needed no caching. The `<KeepAlive>` fix stands unchanged by the color revert.
>
> - **Enforced in (Phase 28q):** `frontend/admin/src/components/TraceWaterfall.vue` — `.kind-tag.rest`'s color/background/border-left; `frontend/admin/src/components/RpcPanel.vue` — the `<KeepAlive>` wrapping `<TraceWaterfall v-if="ui.rpcTab === 'traces'">`.
> - **Test (Phase 28q):** `RpcPanel.spec.js`'s "keeps TraceWaterfall mounted across a Messages round-trip instead of re-fetching" spec — mounts with the default `traces` tab active, records the `getKvBucketEntries` call count after initial mount, toggles `rpcTab` to `messages` and back to `traces`, and asserts the call count is unchanged. Verified live against the docker stack (rebuilt `admin-frontend`): `read_network_requests` showed no additional `trace-request-reply/entries` fetch across four Traces↔Messages toggles, and the live trace count/pulse strip kept updating uninterrupted throughout.
>
> **Phase 44 amendment (UI) — the pulse strip moved off this panel entirely,
> onto its own `Pulse` tab in front of `[Traces] [Messages]`, and stopped
> reading `displayedSummaries`.** Design review found the panel explained
> neither the `_INBOX.<nuid>` reply-routing mechanism nor `parentSpanId`/
> `spanId` (the mechanism that actually chains a multi-hop call into the
> tree `Traces`' waterfall reconstructs), so the new tab pairs that
> explanation and an animated Client → NATS Server → Service diagram with
> the pulse cards, enlarged. The cards' own bucketing logic (Phase 28p) is
> unchanged; what changed is the input: `PulsePanel.vue` keeps its own
> bootstrap/subscribe/trace-grouping (duplicated from `TraceWaterfall.vue`
> rather than shared, matching this panel's existing precedent — see the
> component's own doc comment) and buckets the *full* unfiltered
> `traceSummaries`, not `TraceWaterfall`'s toolbar-filtered
> `displayedSummaries` — once `Pulse` is a separate tab it is no longer
> co-rendered with that toolbar, and sharing its filter state would mean
> either duplicating the toolbar on `Pulse` too or having it silently
> reflect filters set on a tab you're not looking at. `ui.rpcTab`'s default
> also changed from `'traces'` to `'pulse'`, since `Pulse` is now first in
> the tab bar and carries the panel's explanatory content. A mockup
> (`diagrams/admin-rpc-overview-mockup.html`, published as an Artifact) was
> reviewed with the user before implementation (Main-POC-Plan.md Phase 44).
>
> - **Enforced in (Phase 44):** `frontend/admin/src/components/PulsePanel.vue`
>   (new) — bootstrap/`connectLive`/`disconnectLive`, `summarize`/
>   `traceSummaries`, the `pulse` computed, the "what request/reply covers"
>   card, the animated flow diagram, and the `.pulse-row`/`.pulse-card`
>   template block and CSS (all removed from `TraceWaterfall.vue`);
>   `frontend/admin/src/components/RpcPanel.vue` — the new `pulse` `Tab`/
>   `TabPanel`, `<KeepAlive>`-wrapped like `traces`; `frontend/admin/src/
>   stores/ui.js` — `rpcTab`'s default.
> - **Test (Phase 44):** `PulsePanel.spec.js`'s "summarizes the full
>   unfiltered trace set into request/error/latency histograms" spec — the
>   same three-fixture-trace assertions the retired `TraceWaterfall.spec.js`
>   Phase 28p spec pinned (values unchanged, since the bucketing math itself
>   didn't change), minus the toolbar-narrowing assertions `Pulse` has no
>   toolbar to exercise; a second spec asserts the explanatory card and flow
>   diagram render even with zero traces bootstrapped, while the stat row
>   hides entirely (same zero-state rule the strip it replaced followed).
>   `RpcPanel.spec.js`'s Phase 28q spec extended to cover both `Pulse` and
>   `Traces` being kept alive across tab round-trips. Verified live against
>   the docker stack (rebuilt `admin-frontend`): `Pulse` renders as the
>   default landing tab with real bucketed histograms from the live trace
>   buffer, and `Traces`/`Messages` are unaffected.

### BR-037 (Phase 28) — Trace context propagates on every outbound NATS message, one span per logical RPC call, never one per retry attempt

Every outbound `rpc.*` request, `evt.*` publish, and `notify.*` publish carries a W3C `traceparent` header derived from the span that caused it — the same way `Nats-Requestor` (BR-027) already rides outbound requests today. A retried `rpc.*` call (e.g. `refdataconsumer.requestRPC`'s retry loop) mints its child span id **once, before the retry loop starts** — a 3-attempt failure produces one span with a `rpc.retry_count` attribute of 2, not three parentless sibling spans with no relationship to each other; the latter would make a single logical call look like three unrelated ones in the waterfall (BR-035).

A NATS KV entry **cannot** carry trace context — `jetstream.KeyValue.Put` takes no headers, and trace data must never go in the KV body, because the KV inspector panel already distinguishes a PUT from a DEL by whether the payload is empty (ARCHITECTURE-ADMIN.md's KV-watch note). The derived `notify.*` publish that follows a KV write carries the trace context instead, which is what lets a trace's waterfall show the async tail continuing past the KV write itself rather than stopping dead at it.

> **Phase 28c amendment — the outbound span's `{context}`/`{entity}`/`{action}` fields cannot be parsed from the subject the caller actually publishes to, and must be supplied explicitly.** `natstrace.Tracer.Start` (inbound) safely reads these positionally off `req.Subject()` because that subject is always the real wire subject a server matched. An *outbound* caller's subject is not reliably that shape: `refdataconsumer.requestRPC` and trading-partner-service's `refdataclient.Client.requestRPC` publish to a tenant account's **local alias** (e.g. `refdata.item.get.v1`), which accounts-service's `provisioner.go` (`tenantImports`) remaps entirely inside the NATS server to the real `rpc.{tenant}.refdata.item.get.v1` — the account's own identity lands at that token via `jwt.RenamingSubject`, deliberately never a value the client supplies (the same reason refdata-service's own `ItemGetRequest` carries `Context` in the body rather than trusting its inbound subject, Phase 21). An early draft of this rule assumed the bare local alias was itself a bug and rewrote it to construct `rpc.{context}.refdata.item.get.v1` directly — this doesn't route (it doesn't match the account's `LocalSubject`) and, worse, would have put a caller-controlled business-unit context at the exact token the import exists to keep server-controlled. `natstrace.Tracer.StartOutbound(parent, subject, contextValue, service, entity, action)` therefore takes the label fields as explicit parameters rather than parsing them from `subject` — the caller already knows its own context/entity/action and must supply them directly.

> **Phase 28d amendment — the async tail (evt.\* publish → JetStream projector → notify.\*) rides the same span via `context.Context`, never by re-parsing the evt.\* subject.** `dictionary/internal/browserrpc/adapter.go`'s 9 handlers seed `natstrace.ContextWithSpan(context.Background(), natstrace.SpanFrom(req))` instead of a bare `context.Background()`, so `commands.ShipHandler.publish`/`commands.ContainerHandler.publish` can recover it via `natstrace.SpanFromContext(ctx)` and call the widened `Publisher.PublishWithTrace(ctx, sp, subject, data)` instead of the old `Publish`. On the consumer side, `eventhandler`'s three JetStream `Consume` callbacks (`handler.go`'s shared `register()`, used by both `RegisterShapeA`/`RegisterShapeB`; `container_handler.go`'s `RegisterContainers`; `meta_handler.go`'s `RegisterMeta`) each mint one span per message with `Tracer.StartFromHeaders(msg.Headers(), msg.Subject(), event.Context, "shipping", entity, action)` — `entity`/`action` come from the subject fields the callback already parses via `domain.SubjectDetails` (never re-derived positionally by `StartFromHeaders` itself, for the same six-token-vs-five-token reason as the Phase 28c amendment above), with the entity's surrogate id recorded as the `entity_id` attribute. That per-message span then rides `context.WithoutCancel(ctx)` via `natstrace.ContextWithSpan` into the KV write and the `publishNotify`/`publishRawNotify` calls that follow it, and is closed with `sp.End`/`sp.Fail` on the Ack/Nak path — mirroring `respond`/`respondError`'s tail in the browserrpc adapters exactly.
>
> A NATS KV entry still cannot carry trace context (`jetstream.KeyValue.Put` takes no headers), so `internal/kvstore/kv.go`'s `Store.publishNotify` was widened to accept `ctx` (threaded from `Put`/`Delete`, which already had it) and derive the span via `natstrace.SpanFromContext(ctx)`, attaching a `Traceparent` header via `nats.Msg`/`PublishMsg` instead of the old headerless `notifyNC.Publish` — nil-safe: no span in `ctx` means no header, identical to pre-28d behavior.

- **Enforced in:** `internal/refdataconsumer/consumer.go`'s `requestRPC` (mints the child span before its retry loop, sets `traceparent` alongside `Nats-Requestor`, publishes to the tenant-local alias unchanged); trading-partner-service's `internal/refdataclient/client.go`'s `requestRPC` (same pattern, second of the two Phase 28c call sites); `internal/jstream/stream.go`'s `Publisher.PublishWithTrace` (Phase 28d); `dictionary/internal/browserrpc/adapter.go`'s 9 handlers seeding `natstrace.ContextWithSpan` (Phase 28d, Piece 1); `dictionary/internal/application/commands/commands.go`'s `Publisher` interface (widened with `PublishWithTrace`) and `ShipHandler.publish`/`dictionary/internal/application/commands/container.go`'s `ContainerHandler.publish` (Phase 28d, Piece 2); `dictionary/internal/eventhandler/handler.go`'s shared `register()`, `container_handler.go`'s `RegisterContainers`, and `meta_handler.go`'s `RegisterMeta` (per-message spans, Phase 28d Piece 3), plus their `publishNotify`/`publishRawNotify`; `internal/kvstore/kv.go`'s `Store.publishNotify` (carries trace context on the derived notify, never in the KV `Put` body).
- **Test:** a 3-attempt-then-fail `requestRPC` call asserts exactly one span published with `attributes["rpc.retry_count"] == 2`, not three; `internal/refdataconsumer/consumer_test.go`'s `TestLookupPublishesOneSpanCarryingATraceparentHeader`/`TestLookupContinuesParentSpanAttachedViaContext`/`TestLookupPublishesOneErrorSpanWithRetryCountWhenNoResponder`, and trading-partner-service's `internal/refdataclient/client_test.go`, pin both the local-alias subject (unchanged) and the span's explicit labeling. Phase 28d: `dictionary/trace_async_test.go`'s "BR-037 (Phase 28d)" specs assert an api.\* command's resulting evt.\* JetStream publish carries the same `traceId` as its reply-side `obs.trace.*` span (including with no inbound `traceparent` at all — a root span), and that each of the three projector `Consume` callbacks (`RegisterShapeA`, `RegisterShapeB`, `RegisterContainers`, `RegisterMeta`) publishes exactly one span per message labeled with the correct `service`/`entity`/`action`/`entity_id`; `dictionary/internal/eventhandler/publish_notify_test.go`'s `TestPublishNotifyAttachesTraceparentWhenSpanPresent`/`TestPublishNotifyOmitsTraceparentWhenSpanNil` (and their `publishRawNotify`/nil-`nc` counterparts) pin `publishNotify`/`publishRawNotify`'s nil-safe header attachment directly; `internal/kvstore/kv_test.go`'s two new `EnableNotify` specs assert `Store.Put`'s derived notify attaches the `Traceparent` header when `ctx` carries a span and omits it cleanly when it doesn't.

---

### BR-038 (Phase 31) — The ship list is served from the Postgres projection; KV is a per-entity cache and never a list source

`api.*.shipping.ship.list.v1` — Sea Freight Flow's bootstrap and reconnect query — resolves against the canonical Postgres ship projection. The KV bucket is a **per-entity** write-through cache, keyed `{context}.ship.{shipID}`, and is never enumerated to build a list: a `WatchAll`/key-scan list read would return whatever subset of ships happens to be cached, which is a correctness problem disguised as a performance choice, since a cache miss is legal at any time and a partial fleet is indistinguishable from a small fleet.

This is the rule that survives Shape A's retirement rather than a new design. Before Phase 31 this query read the `dict-a` bucket directly, which was legitimate *because Shape A's whole premise was that KV is the read model* — every ship was guaranteed present. With Shape B as the only shape that guarantee is gone, so the list must come from the projection that does have it. The cost is explicit and accepted: the fleet bootstrap is now a Postgres round-trip rather than a KV read, in exchange for a list that is always complete. Single-entity reads (`queries.Ships.GetShip`) keep the KV-cache-then-Postgres-fallthrough path unchanged — that is Shape B's actual pattern, and it is unaffected by this rule. (The admin REST route that once exposed this single-entity path directly, `/api/admin/read-path/ships/{context}/{shipID}`, was retired along with the CQRS Shapes admin panel it existed for — the fallthrough behavior itself is still covered directly at the query layer by `integration_test.go`.)

- **Enforced in:** `queries.Ships.ListShips` (Postgres-backed), reached from `dictionary/internal/browserrpc/adapter.go`'s `handleShipList`
- **Test:** `dictionary/browserrpc_test.go` — asserts `api.*.shipping.ship.list.v1` returns ships that exist in the Postgres projection but are absent from KV (an evicted or never-cached entry must still appear in the list)

---

### BR-039 (Phase 33) — Business operations for this service are reachable only over `api.*`/`rpc.*`, never REST; REST is limited to infra health and admin diagnostics

REST's business surface (`/api/ships/*`, `/api/containers/*`, `/api/terminal/*`, `/api/manifest/*`, `/api/ports/*`, `/api/meta/*`) has been deleted outright, not deprecated. Every one of those routes already had (or, for the ship manifest, was given) an `api.*.shipping.*` equivalent — Sea Freight Flow (`frontend/seafreight-app`) already ran entirely over `api.*` before this phase, so nothing lost transport reach; REST was simply a second, now-redundant business surface. `GET /api/manifest/{context}/{shipID}` was the one gap: `api.*.shipping.container.manifest.v1` closes it, following the existing `{context}`-from-subject convention (`contextFromSubject`) and carrying `shipID` in the request body since the subject scheme has no second wildcard segment.

What remains under `/api/*` is infra (`/healthz`) and admin/operator concerns: the raw Postgres ports table (`/api/admin/ports/{context}`) and tenant discovery/switch (`/api/tenant`, `/api/tenant/switch`). This is a transport-contract rule, not a behavior change to any domain rule above: no application-layer method was touched, only which transport(s) can reach it. (Phase 33 also renamed the admin read-path diagnostics route from `/api/shape-b/*` to `/api/admin/read-path/*` on this same reclassification-not-business-route reasoning; that route was later retired outright along with the CQRS Shapes admin panel it existed for, so it no longer appears here.)

- **Enforced in:** `dictionary/internal/rest/handlers.go`'s `Mount` (business routes removed entirely); `dictionary/internal/browserrpc/adapter.go`'s `handleContainerManifest`/`ContainerManifestSubject` (the new `api.*.shipping.container.manifest.v1` endpoint)
- **Test:** `dictionary/browserrpc_test.go` — "Phase 33.2: container.manifest.v1 exposes the ship manifest join over api.* (BR-039)" asserts the new endpoint matches `Terminal.ListByShip` called directly; `dictionary/api_test.go` — covers the surviving infra/admin routes (health, admin ports table), with setup going through the application layer directly since REST no longer exposes any way to create a ship

---

### BR-040 (Phase 34, canonical — mirrored per service below) — Each service's registered REST route set is asserted to exactly match a hardcoded admin/infra/bootstrap allowlist; a business route added later fails the test, not just the code review

BR-039 (and its per-service siblings, BR-P26/BR-TP16/BR-D43) retired today's
business REST. Nothing in the code itself stops a *future* business route
being registered on a `rest/handlers.go` `Mount` — the boundary was achieved,
not enforced. This rule closes that gap: every service's `Mount` function is
changed to return the exact list of `"METHOD /pattern"` strings it registers
(plus any bare-prefix `Handle` mount like `/swagger/`), and a test asserts
that returned list is `ConsistOf` a hardcoded allowlist — exact match, not a
subset check, so the test catches both an unexpectedly *added* route (extra
entry not in the allowlist) and an unexpectedly *removed* one (missing entry,
signalling the allowlist itself is now stale and needs a deliberate edit).
This is the same "prove the boundary is exactly where it should be" intent as
`TestShippingAdminCanOnlyUseNarrowOrderedConsumerAccess`
(`internal/natsaccounts/isolation_test.go`), adapted from a permission-grant
pair (positive narrow access + negative blanket-access rejection) to a route
set, where the bidirectional `ConsistOf` equality plays both roles at once: no
extra route can sneak in, and no allowlist entry can silently stop being
served.

The allowlist is data, not prose, checked into each test file. Current
allowlists (post–Phase 33, one per service):

- **shipping-service** (`dictionary/internal/rest/handlers.go`): `GET
  /api/admin/ports/{context}`, `GET /api/tenant`, `POST /api/tenant/switch`,
  `GET /healthz`, `/swagger/`.
- **refdata-service** (`refdata/internal/rest/handlers.go`): the 23
  `/api/refdata/admin/*` routes (BR-D43's permanent exemption) plus
  `/swagger/`. No `GET /healthz` is registered on this service today — a
  pre-existing gap this phase surfaces but does not fix (out of scope: BR-040
  enforces the boundary as it exists, it doesn't add routes).
- **pricing-service** (`pricing/internal/rest/handlers.go`): `GET /healthz`
  only (BR-P26).
- **trading-partner-service** (`tradingpartner/internal/rest/handlers.go`):
  `GET /healthz` only (BR-TP16).
- **observability-service** (`observability/internal/rest/handlers.go`): `GET
  /healthz`, `GET /api/nats/connections`, `GET /api/nats/account-activity`,
  `GET /api/nats/log`, `GET /api/kv/buckets`, `GET
  /api/kv/buckets/{account}/{bucket}/entries`, `GET /api/jetstream/streams`,
  `GET /api/jetstream/replay`, `GET /api/nats/services` — never touched by
  Phase 33 since these were always admin/infra diagnostics (moved from
  shipping-service in Phase 30h), not business REST.
- **accounts-service** — two independent `Mount` calls onto one mux, each
  gets its own test against its own sub-allowlist: `accounts/handler.go`'s 13
  `BasicAuth`-gated `/api/accounts*` routes (account/business-unit lifecycle),
  and `auth/handler.go`'s 5 deliberately ungated `/api/auth/*` routes (this
  service *is* the tenant axis; there is no business domain to separate REST
  from here — every route is inherently admin/bootstrap).

- **Enforced in:** `internal/rest` (or `accounts/`, `auth/`) package's
  `Mount` function per service, each changed to return `[]string`.
- **Test:** one allowlist test per service/sub-mux — see BR-040's mirror
  entry in each service's own `BUSINESS_RULES-*.md` file for the exact test
  name.

### BR-041 (Phase 34, canonical — mirrored per service below) — A client-supplied requester identity is carried for observability only; it is never read for authorization by any handler

BR-027 already has shipping-service's `refdataconsumer` set a
`Nats-Requestor: <name>/<instance-id>` header on outbound `rpc.*` requests,
and the browser sets the same header on `api.*` calls
(`useNatsConnection.js`). Both predate this rule; BR-041 makes the
constraint explicit and platform-wide rather than leaving it implicit in one
service's transport doc: **this header (and any header like it) is
self-declared by the caller and must never gate anything.** Core NATS
request/reply carries no server-attested caller identity — unlike a mux
route, which the server itself enforces by refusing to serve an unregistered
pattern, nothing stops a caller from putting any string it likes in
`Nats-Requestor`. The only legitimate use is observability: the Admin UI's
Request/Reply & Traces panel may filter or display it, clearly labeled as
self-declared, but no handler in any service may branch on its value, and no
future rule may propose using it as an authorization signal. The trustworthy
axis for filtering "is this an admin request" is the **subject prefix**
(`api.*.refdata.admin.*` vs `api.*.refdata.item.*`, BR-D41) — the server
enforces that split by permission grant; a header filter merely reflects
what the caller claimed.

Phase 34.3 carries this header's value as a first-class `Requester` field on
the `obs.trace.*` envelope (BR-036's `traceSpan` struct) — populated from the
already-merged `Headers["Nats-Requestor"]` at span-`finish()` time, across
all 5 `natstrace` copies — purely so the Admin UI can read it from existing
trace data instead of a new channel. This is additive to BR-036, not a
change to its wire contract's redaction/truncation ordering.

- **Enforced in:** nowhere (deliberately) — grep audit confirms zero
  `Header.Get("Nats-Requestor")` / `Header["Nats-Requestor"]` reads outside
  the setting/forwarding code itself, across every service's Go source.
  `internal/natstrace/natstrace.go`'s `finish()` (all 5 copies) now also
  populates `traceSpan.Requester`.
- **Test:** `internal/natstrace/natstrace_test.go` (each of the 5 copies) —
  asserts a span whose captured request headers include `Nats-Requestor`
  produces a `traceSpan.Requester` equal to that header's value, and a span
  with no such header produces an empty `Requester` (never a placeholder
  that could be mistaken for a real identity).

### BR-042 (Phase 43, revised post-spike 2026-08-18) — "Trace this subject" targets only the existing `tenantImports()` cross-account contract, fired by the service that owns the connection, defaults to dry-run, and merges as a distinct `kind: "hop"` span alongside BR-036's application spans

> **Numbering note (2026-08-19):** this rule was drafted while the phase was
> numbered 36 (see `Main-POC-Plan.md`'s renumbering logs: 29→41→36→43). Its
> heading now cites the phase's current live number, 43, since 36 has since
> been freed and reused for an unrelated phase (Tech Lab Operator rebrand) —
> keeping this heading on the stale number would make it read as a citation
> of that new phase instead.

A live spike against the compose stack (see Phase 43's "Spike findings" in
`Main-POC-Plan.md`) found the original design unbuildable as first proposed:
`observability-service` cannot publish to any business subject (its NATS
user has no such grant, confirmed by a live permissions-violation test), and
most business subjects never cross an account boundary at all — each
tenant-aware service holds one direct connection per tenant
(`natstenants.Manager`), so there is nothing for an account-crossing probe
to observe. The one real, currently-provisioned crossing is
`accounts-service`'s `tenantImports()`/`tenantExports()` contract
(`provisioner.go:207-246`): every tenant imports 4 `refdata-service` RPCs
(`refdata.item.get.v1`, `refdata.type.list.v1`,
`refdata.item.get-versioned.v1`, `refdata.locales.list.v1`, forwarding to
`rpc.{tenant}.refdata.*` in PLATFORM) plus 2 stream imports
(`evt.*.refdata.*.changed`, `notify.accounts.account.*`). This rule targets
*that* contract specifically, not an arbitrary typed subject.

- **The probe target is an enumerated list — the `tenantImports()` contract
  entries — never a free-typed subject.** This is the only crossing this
  system has; a free-text field would only ever mislead an operator into
  thinking any subject was probeable this way.
- **The service that owns the real connection fires its own probe.**
  `shipping-service` and `trading-partner-service` already hold the only
  connections with legitimate publish rights on this crossing (their own
  per-tenant connections, used for their own real `refdataconsumer`/
  `refdataclient` calls). Each exposes a small internal diagnostic hook
  that fires one of its own already-defined outbound calls with
  `Nats-Trace-Dest: obs.trace.hop.{traceId}` and `Nats-Trace-Only: true` by
  default, using the connection it already holds — **no new NATS
  permission grant is added anywhere, for any account.** `MintAdminToken`
  denies all publish (`Pub.Deny.Add(">")`) and a PLATFORM-only connection
  cannot resolve a tenant-local import alias by name at any permission
  level, which is why this can't be `observability-service`'s own publish
  as originally designed.
- **`observability-service` keeps the REST entry point and the
  storage/rendering role.** The browser calls `POST /api/nats/trace`
  (extends BR-040's `observability-service` allowlist, same
  system-topology-diagnostics carve-out as `/api/jetstream/replay`, `POST`
  not `GET` since firing a probe has a real wire effect).
  `observability-service` forwards to the owning service's diagnostic hook
  over an internal PLATFORM→PLATFORM call (no crossing, no new grant on
  this leg either), then normalizes the reply into `kind: "hop"` spans.
- **`Nats-Trace-Only: true` is the default and cannot be overridden to
  `false` by the browser.** Matches `nats trace`'s own default — the
  message is routed (so the real `si`/`se` hop is reported) but never
  delivered to a live subscriber, so firing a probe never produces a real
  side effect. Turning this off is out of scope — see Phase 107.
- **A hop event is a `kind: "hop"` span, additive to BR-036's `traceSpan`
  shape, not a new envelope.** Merges into the same spans array
  `tracestore.appendSpan` already builds, using only the fields that make
  sense for a hop (`Subject`, `Timestamp`, a new `HopType` value of
  `in`/`eg`/`sm`/`se`/`si`/`js`, `Error`) — `Duration`, `Requester`, and
  payload fields stay empty. Every probe mints its own synthetic `traceId`.
- **Confirmed live: destination-interest reporting is unreliable past a
  Service Import and must never be rendered as a failure signal.** Isolated
  by varying subscriber shape one variable at a time — a plain literal
  (non-wildcard, non-queue) subscriber still shows `No active interest`
  through the crossing, while the identical literal subject traced
  same-account (no crossing) correctly reports the real client and an
  egress count. This is a systematic NATS 2.14.3 tracing gap, not a
  configuration mistake and not fixable in this repo's code. Any hop past
  an account boundary renders with a hedged, non-failure treatment and a
  tooltip explaining interest isn't reliably reported there — the UI must
  never assert "dropped" based on this signal.
- **The route joins BR-040's allowlist mechanism, not a special case.**
  `POST /api/nats/trace` is added to `observability-service`'s hardcoded
  allowlist and its `ConsistOf` test, same as every other route on that
  service.

- **Enforced in:** `shipping-service`'s admin surface (new diagnostic hook
  reusing `refdataconsumer`'s existing connection),
  `observability-service/observability/internal/rest/` (new `nats_trace.go`
  handler + `Mount` allowlist entry, forwarding logic),
  `observability-service/observability/internal/tracestore/tracestore.go`
  (`appendSpan` accepts the new `kind: "hop"` shape).
- **Test:** an allowlist test asserting `POST /api/nats/trace` is present
  (BR-040's mirror for this service); a `tracestore_test.go` case that a
  `kind: "hop"` span merges into an existing/new `traceRecord` without
  requiring the application-span fields; an integration test firing a
  probe through `shipping-service`'s diagnostic hook and asserting the
  returned hop tree includes the `si` (service import) hop; a frontend spec
  asserting a hop past an account boundary never renders with failure
  styling regardless of the interest signal it carries.

### BR-043 (Phase 45) — `/accstatz` history is retained in a 60-minute ring buffer sampled every 10s, and a duration query param selects a correctly delta'd, bucketed trend series

**`/accstatz` (BR-034's data source) is a stateless snapshot — it has no memory of its own.** The Overview tab's trend charts and duration selector need real history, so `observability-service` polls `/accstatz` every 10s (the same interval the Admin UI's own poll already used) and appends each sample into an in-memory ring buffer, trimming anything older than 60 minutes on every append. Not persisted to Postgres or NATS KV — deliberately transient telemetry, not source-of-truth data, the same distinction this repo already draws for what does vs. doesn't get event-sourced (`ARCHITECTURE.md` § "Event Sourcing vs Plain CRUD"). The buffer starts empty at process boot and fills in real time; a freshly-restarted service legitimately has less than 60 minutes of history until it's been up that long — there is no synthetic backfill.

`GET /api/nats/account-activity/history?duration=5m|30m|1h` queries the buffer. Any other value (missing, malformed, out of range) is a 400, not a silently-empty 200. Bucket size scales with the window so a short window doesn't collapse to one or two fat bars: 30s buckets at 5m, 2min at 30m, 5min at 1h. Each bucket reports `connections`/`subscriptions` as point samples (the value as of that bucket's end) and `inBytesDelta`/`outBytesDelta`/`inMsgsDelta`/`outMsgsDelta` as **deltas against the previous bucket** — `/accstatz`'s own sent/received counters are cumulative since server start, not per-interval, so charting the raw values would draw an ever-climbing line instead of a throughput bar. The response carries one series per account seen anywhere in the buffer (not just within the requested window), so an account doesn't disappear from the response the moment it's briefly quiet — it just reports zeroed buckets for that stretch.

- **Enforced in:** `observability-service/observability/internal/rest/account_activity_history.go` (`accountHistoryBuffer`, `AccstatzHistory`, `bucketSeries`); wired into `composition.go`'s `Startup` (background poller tied to the process's own shutdown context) and `nats_connections.go`'s `accountActivityHistory` handler.
- **Test:** `TestBucketSeriesProducesCorrectBucketCountAndSizePerDuration`, `TestBucketSeriesComputesDeltasNotRawCumulativeCounters`, `TestAccountHistoryBufferEvictsSamplesOlderThanRetention`, `TestAccountHistoryBufferRetainsSamplesWithinWindow`, `TestAccstatzHistoryQueryRejectsInvalidDuration`, `TestAccstatzHistoryQueryIsNilSafe` (`account_activity_history_test.go`) — bucket count/size per duration, delta-not-raw byte counts, eviction past 60 minutes, and the nil-safe degrade. `TestAccountActivityHistoryReturns400ForInvalidDuration`, `TestAccountActivityHistoryReturnsBucketedSeries` (`nats_connections_test.go`) — the handler's 400 and its wiring to a real (empty, freshly-booted) buffer.

### BR-044 (Phase 45) — The Overview tab's account list gets a name filter, shown only once there are more than 3 accounts

**A pure presentation rule, frontend-only — no backend involvement.** Below 4 accounts the list is short enough to just read, and a search box above 2-3 rows is a control with nothing to do; the box is omitted entirely rather than shown-but-useless. Once there are more than 3 accounts, a text input filters the card list by name (resolved `tenantLabel`, falling back to the raw account identifier per BR-028's convention) — case-insensitive substring match, applied client-side against the already-fetched account list. A count line reads "N accounts" with no query, "M of N accounts" while filtered. A query that matches nothing shows a named empty state ("No accounts match "…".") instead of a blank list, matching this repo's "errors/emptiness are moments for direction, not silence" interface-voice convention. The query clears itself if the account count ever drops back to 3 or fewer (the box disappearing with a stale query still applied would leave the newly-visible short list looking emptier than it is).

- **Enforced in:** `frontend/admin/src/components/AccountsOverviewPanel.vue` (`showSearch`, `filteredAccounts`, the `watch(showSearch, ...)` clear-on-hide guard).
- **Test:** `AccountsOverviewPanel.spec.js` — search box hidden at ≤3 accounts, shown and filtering at 4, the named empty state on a non-matching query, and the "N of M" vs "N" count text.

---

## Guards (not numbered rules)

- **Unregistered container** — load/unload of a container with no `.registered`
  event returns `ErrContainerNotFound` (entity existence, mapped to HTTP 404).
- **Input validation** — required-field checks (`containerID is required`,
  `shipID is required`, …) live in the application layer
  (`commands/container.go`), fire before the aggregate is consulted, and are
  deliberately **not** domain rules — same classification as the retired BR-007.

---

## AIS Navigational Status

Ships carry an AIS-aligned status derived from their current state. This is a read-model concern (set in `ShipAggregate.State()`) and not a domain rule, but it is documented here for reference.

| Status constant | JSON value | Condition | UI colour |
|---|---|---|---|
| `StatusDocked` | `"docked"` | `CurrentPort != ""` | Green |
| `StatusInTransit` | `"in-transit"` | `CurrentPort == ""` | Blue |
| `StatusAtAnchor` | `"at-anchor"` | _(future domain event)_ | Amber |
| `StatusNotUnderCommand` | `"not-under-command"` | _(future domain event)_ | Red |
| `StatusRestrictedManoeuvrability` | `"restricted-manoeuvrability"` | _(future domain event)_ | Orange |

## Container Status

| Status constant | JSON value | Condition |
|---|---|---|
| `ContainerInTerminal` | `"in-terminal"` | `terminalPort` set, `onShipID` nil |
| `ContainerOnShip` | `"on-ship"` | `onShipID` set, `terminalPort` nil |
