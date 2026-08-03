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
(`commands.hydratePair`), so these checks are strongly consistent. Phase 23
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
- **Enforced in:** `ContainerAggregate.Load()` — checked alongside the existing BR-012/BR-010/BR-014/BR-008 checks; requires the ship's current on-ship container count, resolved by the application layer before the domain check (mechanism — event-replay count vs. Shape A/B read-model query — to be decided during implementation; see Phase 20 of `Main-POC-Plan.md`)
- **Test:** `Container Domain Rules / BR-019` (not yet written — pending implementation)

**Frontend:** `frontend/seafreight-app` ("SeaFreight Flow") gains a load-capacity indicator column (e.g. `12 / 50`, colored by how full the ship is) in both `FleetPanel.vue` and `ShipsAtPortPanel.vue`, pairing the new `capacity` field with the container count already computed via `store.manifestFor(shipID).length`.

---

### BR-020 — A shipID and context must be a valid subject/KV-key token
`context` is threaded directly into NATS subjects (`evt.{context}.shipping.ship.{id}.{event}`) and into KV keys as a prefix (`{context}.{entityType}.{id}`). KV buckets are now tenant-scoped (one per role per NATS account: `dict-a`, `dict-b`, `container`, `meta`), so the {context} suffix that was formerly part of the bucket name (`dict-a-{context}`, …) has moved into the key — the charset constraint remains the same because a dot in `{context}` would silently split it across key segments. `shipID` (BR-021/BR-022: the mutable natural key, not the aggregate's surrogate `id`) is a KV-key component (`{context}.ship.{shipID}`) rather than a subject token, but is held to the same charset for consistency and because it's also carried in the event payload. Both must be non-empty and match `^[A-Za-z0-9_-]+$` — NATS's recommended safe subject-token charset. A dot would silently corrupt a KV key or split a subject across tokens; `*`/`>`/whitespace are NATS wildcard/subject metacharacters.

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

Mirrors BR-D25 (`BUSINESS_RULES-REFDATA.md`): `dictionary/internal/browserrpc/adapter.go`'s handlers call the exact same `commands.*Handler`/`queries.*` methods `dictionary/internal/rest/handlers.go` calls, so an operation behaves identically regardless of which transport reaches it. The `{context}` token in the subject (`api.{context}.shipping.{entity}.{action}.v1`) — the company / business-unit scope, the same axis as `evt.{context}...` and KV bucket names, and **not** the NATS tenant/account nor the region — is the *only* source of truth for which context a request is scoped to. Every handler overwrites the decoded request body's own `context` field (if any) with the subject-derived value before calling into the application layer, so a client cannot spoof or bypass context scoping via the body. Tenant isolation itself is enforced entirely by the NATS account boundary a connection authenticates into (Phase 13a/13b), not by anything in this subject pattern — see `accounts-service/auth/token.go`'s `MintBrowserToken` doc comment (Phase 19 folded auth-service into accounts-service, see `BUSINESS_RULES-ACCOUNTS.md`) and `ARCHITECTURE-COMMUNICATIONS.md` § 2.3 for the full reasoning.

This is the **frontend-to-service** family. `rpc.*` is reserved for service-to-service calls and is a separate family with its own adapter and its own permission grant: a browser credential is never granted `rpc.>`, and backend code never calls `api.>` (`ARCHITECTURE-COMMUNICATIONS.md` § 2.4). An operation may be registered on both when both caller types genuinely need it, but each registration is independent.

- **Enforced in:** `dictionary/internal/browserrpc/adapter.go` — `contextFromSubject()` plus every `handle*` method
- **Test:** `Browser RPC Adapter (Phase 15a/16b) / BR parity` (`dictionary/browserrpc_test.go`)

### BR-024 — Ship, container, and meta projections fire a best-effort `notify.*` event after every KV write; ports fire one from the api.* adapter itself
After the Shape A ship projector, the container projector, or the meta projector successfully writes its KV bucket, it fire-and-forget publishes `notify.{context}.shipping.{entity}.changed` (entity: `ship`/`container`/`meta`) carrying the full updated entity (or, for meta, the full known-containers array — a bare JSON array, not a `{"values": [...]}` envelope) as JSON payload — letting a browser connected directly to NATS (Phase 15d) react without KV watch or SSE. Shape B does **not** also publish: the browser doesn't distinguish shapes, so a second notify per event would be a duplicate, not new information. Ports have no event-sourced projector to hang this off (`commands.PortHandler` writes straight to Postgres and is a single instance shared by every tenant), so `browserrpc.Adapter.handlePortRegister` publishes `notify.{context}.shipping.port.changed` itself, on its own tenant connection, after a successful registration — also a bare array, matching meta's convention rather than the `{"values": [...]}` envelope `api.*.shipping.port.list.v1`'s request/reply uses, so a subscriber never needs to know which entity's REPLY shape to unwrap.

This is plain core NATS pub/sub — deliberately **no** JetStream retention (unlike `obs.rpc.*`/RPCTRACE, BR-D29, which is refdata-service's own retained observability channel): a notification missed during a brief browser disconnect is covered by a bootstrap `api.*.shipping.{entity}.list.v1` call on reconnect, so no replay mechanism is needed. A publish failure is logged, never returned — `notify.*` is a best-effort reactive-UI convenience, not a correctness requirement the projector's own success depends on.

- **Enforced in:** `eventhandler.publishNotify()` (called from `RegisterShapeA`/`RegisterContainers`/`RegisterMeta`), `browserrpc.Adapter.publishPortsChanged()`
- **Test:** `notify.* publishes (Phase 15b)` (`dictionary/notify_test.go`)

### BR-025 (Phase 16f) — Reference-data reads resolve against the active tenant's refdata company context, not a hardcoded literal

`GET /api/refdata/types/{type}`, `GET /api/refdata/locales`, and the new `GET /api/refdata/contexts` all resolve the refdata-service company context to read from as `refdataCompanyContext(tenant)`, which today is simply the tenant name itself — per `Main-POC-Plan.md` § Phase 16 decision 11 ("in the common no-company-group case a tenant's own name doubles as its company `{context}` value," the same mapping `BUSINESS_RULES-ACCOUNTS.md`'s BR-AC07 relies on). This replaced the `refdataContext = "acme"` constant hardcoded through Phase 16d.

`GET /api/refdata/contexts` is new in this phase: it calls `refdataconsumer.ListContexts(ctx, tenant)` → `rpc._platform.refdata.context.list.v1` (refdata-service's `BUSINESS_RULES-REFDATA.md` BR-D35) and returns the tenant's real context list, replacing the frontend's previously hardcoded `CONTEXTS` array (`stores/port.js` in Sea Freight Flow, `stores/dictionary.js` in the Admin/Dictionary UI) with a live fetch on tenant init/switch.

**Known gap, not fixed by this rule:** "the active tenant" here is `Deps.Tenant` — REST/SSE's Phase 13b `SwitchTenant` selection, which the Admin/Dictionary frontend drives directly. Sea Freight Flow (Phase 15d) no longer calls `SwitchTenant` at all — it authenticates straight into its own NATS account. Both happen to default to the same tenant (`acme`) today, so these three endpoints read correctly for Sea Freight Flow in the common case, but if the Admin UI's tenant selection and Sea Freight Flow's own NATS tenant were ever switched to different tenants concurrently, these specific reads would silently reflect the Admin UI's selection instead of the browser's actual tenant. A real fix would thread an explicit tenant through the *shared* `useRefdataLabels`/`useL10nCopy` composables (used by both frontends) rather than relying on shipping-service's REST-side state — out of scope for Phase 16f, which only replaced the hardcoded literal, not this pre-existing Phase 15 scope-boundary seam (see that phase's Context section: refdata-service's cross-tenant DEFAULT-account model was already flagged out of scope there).

- **Enforced in:** `dictionary/internal/rest/handlers.go`'s `refdataCompanyContext`, `listRefdataType`, `listRefdataLocales`, `listRefdataContexts`; `internal/refdataconsumer/consumer.go`'s `ListContexts`
- **Test:** `TestListRefdataContextsForwardsActiveTenant`, `TestListRefdataContextsReturns503WhenRPCUnavailable` (`dictionary/internal/rest/refdata_demo_error_test.go`); `TestListContextsUsesRPCAndForwardsTenant`, `TestListContextsReturnsErrRPCUnavailableWhenNoResponder` (`internal/refdataconsumer/consumer_test.go`)

### BR-026 (Phase 17a) — Every `obs.api.*` event carries its headers, a publisher-side timestamp, and its payload size

Mirrors refdata-service's `BUSINESS_RULES-REFDATA.md` BR-D36 for this service's own `obs.api.*` observability channel (BR-023's sibling side-channel, same BR-D26/BR-D29 mechanism refdata-service's `obs.rpc.*` uses). `browserrpc.Adapter`'s `obsEnvelope` gains `headers`, a publisher-side `timestamp`, and `payloadBytes` on both the request-side and reply-side event — additive/optional fields, so events published before this rule shipped still decode. `respondError` attaches the real `Nats-Service-Error`/`Nats-Service-Error-Code` headers to both the observability event and the actual wire reply (via `micro.WithHeaders`), additive to the existing JSON error body — no existing client needs to change.

- **Enforced in:** `dictionary/internal/browserrpc/adapter.go`'s `publishObs()` and `respondError()`
- **Test:** `Browser API Adapter (Phase 15a/16b) / BR-026` (`dictionary/browserrpc_test.go`) — a request-side event carries the caller's real headers, a non-zero timestamp, and the true payload size; a failed reply (`container.load.v1` on an unregistered container) carries real `Nats-Service-Error`/`-Code` headers on both the obs event and the actual wire reply; an old-shape envelope with none of the three fields still decodes without error

---

### BR-027 (Phase 18) — Every `rpc.*`/`api.*` request carries a `Nats-Requestor` header identifying its caller; every reply carries a `Nats-Responder` header identifying the answering service instance

Mirrors refdata-service's `BUSINESS_RULES-REFDATA.md` BR-D37. NATS doesn't propagate caller or responder identity onto a message itself — auth identity lives at the connection level and never reaches a handler's `Msg`, and a reply's subject alone doesn't distinguish which replica of a horizontally-scaled service actually answered. Both headers share one **instance-qualified format**, `"<name>/<instance ID>"` — the same `service.name`/`service.instance.id` split OpenTelemetry's resource semantic conventions use — so replicas (or browser tabs) stay distinguishable. `Nats-Requestor` is set by the caller: the browser's `useNatsConnection.js` `request()` (this service's `api.*` caller) combines `"seafreight-app"` with a random ID generated once per module load — i.e. per tab; `internal/refdataconsumer`'s `Consumer` (this service's own `rpc.*` caller, calling refdata-service) combines this connection's `nats.Name("shipping-service")` with a NUID generated once at `New()`. `Nats-Responder` is set by `browserrpc.Adapter` on every reply (success and error alike) as `"shipping-service/<micro.Service instance ID>"` — the instance ID is generated fresh per process by `micro.AddService`, with no config of its own. `micro.Config.Name` is set to `"shipping-service"`, matching the connection's own `nats.Name` exactly (not a family-derived name like `shipping-api`), so both headers agree on one name for this service — a mismatch there would make the Admin UI's Request/Reply panel's request and reply sides look like they belong to different entities. Both headers are attached to the real wire message, not fabricated for the observability channel alone — same convention BR-026 established for the error headers. Instance IDs are random per process/tab today; a stable infra identity (e.g. a Kubernetes pod name) can seed the instance half later without changing the header format.

- **Enforced in:** `dictionary/internal/browserrpc/adapter.go`'s `respond()`/`respondError()` (set `Nats-Responder`) and `micro.AddService`'s `Config.Name`; `internal/refdataconsumer/consumer.go`'s `New()`/`requestRPC` (sets `Nats-Requestor`); `frontend/seafreight-app/src/nats/useNatsConnection.js`'s `request()` (sets `Nats-Requestor`)
- **Test:** `Browser API Adapter (Phase 15a/16b) / BR-027` (`dictionary/browserrpc_test.go`) — a caller's instance-qualified `Nats-Requestor` header is forwarded into the obs event; a successful reply carries `Nats-Responder` (prefixed `shipping-service/`) on both the real wire reply and the obs event; a failed reply carries it too. `TestLookupCarriesInstanceQualifiedRequestorHeader` (`internal/refdataconsumer/consumer_test.go`) asserts the requestor format directly: `"<nats.Name>/<instance>"`, stable across calls from one `Consumer`

---

### BR-028 (Phase 17c) — In the Admin UI, a NATS connection's or service instance's account resolves to a friendly name wherever possible, instead of showing the raw account NKey

**This is a presentation rule, scoped to the Admin UI only** — it governs what the Connections and Services panels *display*, not any wire protocol. Nothing about `Nats-Requestor`/`Nats-Responder` (BR-027) or the actual bytes shipping-service or refdata-service put on the wire changes; `/connz`'s raw account identifier is still the NKey it always was. The rule exists because the Connections/Services panels (Phase 17c) are read-only operator-facing surfaces, and a raw NKey (`AAFBCA52VV7P...`) means nothing to someone scanning the panel, while "acme" does. "Wherever possible" is deliberate, not a hedge: an account this process has no way to identify (accounts-service's SYS account, which shipping-service holds no connection on) correctly stays unresolved rather than guessed at.

Two independent mechanisms enforce this, one per panel — both avoid decoding account NKeys out of credential JWTs entirely, resolving everything from information the process already has or the server already reports:

- **Connections** (`GET /api/nats/connections`) — `tenantLabelsByAccount` resolves every `/connz` row's account NKey to a friendly label in two stages. First, it identifies which rows are shipping-service's *own* connections by matching local socket address (`nc.LocalAddr()` is exactly what the server reports back as that connection's `ip:port` — same TCP socket, both ends), establishing "this account NKey means DEFAULT" / "this account NKey means acme." Then it applies that mapping **by account**, not by address, to every row in the full list — so refdata-service, the `nats` CLI, and any browser tab authenticated on a known tenant account resolve too, not just shipping-service's own three rows.
- **Services** (`GET /api/nats/services`) — `browserrpc.Adapter` tags its `micro.AddService` registration with `Metadata: {"tenant": <name>}` (deliberately metadata, never `Config.Name`/`Config.Version` — those must stay identical across every tenant connection, per BR-027's `Nats-Responder` invariant). `micro.Stats.ServiceIdentity` already carries `Metadata` on every `$SRV.STATS` reply, so `listNatsServices` only needs to pass it through.

Both frontend panels prefer the resolved label over the raw NKey when present (rendered as a colored tag), falling back to a truncated raw NKey otherwise (rendered as monospace code — a deliberately different visual treatment, not just different text, so there's no single shared "accountLabel" string helper on the frontend).

- **Enforced in:** `dictionary/internal/rest/nats_ops.go`'s `tenantLabelsByAccount` and `listNatsServices`'s `Metadata` pass-through; `dictionary/internal/browserrpc/adapter.go`'s `micro.Config.Metadata`; `dictionary/internal/rest/tenant.go`'s `ensureTenantResources` (threads the tenant name into `browserrpc.Deps.Tenant`); `frontend/admin/src/components/ConnectionsPanel.vue`'s Account column; `frontend/admin/src/components/ServicesPanel.vue`'s tenant tag
- **Test:** `TestListNatsConnectionsLabelsAnyConnectionSharingAKnownAccount`, `TestTenantLabelsByAccountSkipsNilTenantEntriesAndUnownedAccounts` (`dictionary/internal/rest/nats_ops_test.go`) — the account fan-out resolves a connection this process doesn't own but shares a known account with, and leaves a genuinely unrelated account (accounts-service's SYS account) unresolved rather than mismatched. `TestListNatsServicesPassesThroughInstanceMetadata` (same file) — the REST handler passes `$SRV.STATS`'s metadata through untouched. `Browser API Adapter (Phase 15a/16b) / BR-028` (`dictionary/browserrpc_test.go`) — the production wiring itself (`tenant.go` → `browserrpc.Deps.Tenant` → `micro.Config.Metadata`) actually produces the tag, verified over a real `$SRV.PING`, not just the REST handler's pass-through logic in isolation. `ConnectionsPanel.spec.js` and `ServicesPanel.spec.js` (`frontend/admin/src/components/`) — the resolved label actually renders as a tag and the raw-NKey fallback actually renders too, not just that the API response carries the field

---

### BR-029 (Phase 16g) — Sea Freight Flow's Fleet Management, Ships at Port, and Terminal Yard panels show a loading indicator, never a bare empty state, while a tenant or fleet-context switch is repopulating them

**This is a presentation rule, scoped to Sea Freight Flow's browser UI only** — it governs what these panels *display* mid-switch, not the underlying data or transport. `usePortStore().connect()` (run on every tenant switch via `stores/tenant.js` and every fleet-context switch via `setContext()`) resets `ships`/`containers` to `{}` synchronously, before its `listShips`/`listContainers`/`getPorts`/`knownContainers` bootstrap reads resolve — a NATS WebSocket reconnect (tenant switch) plus a request/reply round trip, both non-zero latency. Without a loading signal, a `DataTable`'s own empty state ("No ships match this filter.", "No ships docked here — send an arrival above.", "No outbound containers in this yard — register one above.") flashes in that gap, misreading as "this tenant/context genuinely has none" rather than "still loading" — most visible on a slower network or during the tenant switch's WS re-authentication, less so on localhost where the gap can be under 100ms. All three panels read off the same `ships`/`containers` state, so the same gap affects all three identically; initially only the Fleet panel was fixed (in direct response to the reported flicker), then extended to the other two once confirmed to be the same root cause.

- **Enforced in:** `stores/port.js`'s `loading` state — set `true` at the start of `connect()` (same point `ships`/`containers` are cleared), cleared once the bootstrap `Promise.allSettled` of `getPorts`/`listShips`/`listContainers`/`knownContainers` lands (success or failure — a failed read shouldn't leave a panel stuck loading). `components/FleetPanel.vue`, `components/ShipsAtPortPanel.vue`, and `components/TerminalPanel.vue` each render a spinner + panel-specific loading copy (`fleet.loading`/`shipsAtPort.loading`/`terminal.loading`) while `store.loading` is true, their `DataTable`(s) otherwise — mutually exclusive via `v-if`/`v-else`(`-if`), so a table's own empty-state message can only ever reflect a genuinely empty result, never a mid-switch one. `ShipsAtPortPanel.vue` layers this under its pre-existing `!store.port` ("select a port") branch; `TerminalPanel.vue` covers both its Outbound and Arrived tables with one shared spinner (they're both driven by the same `store.containers` reset, so there's no reason to show two).
- **Test:** `'shows a loading indicator instead of the empty state while the fleet is (re)loading'`, `'...on Ships at Port while (re)loading'`, `'...on the Terminal yard while (re)loading'` (`App.spec.js`) — each panel renders its spinner and hides its `DataTable`(s) while `store.loading` is true, and never shows its own empty-state text during that window. `'sets loading true and clears the previous context's ships/containers synchronously, then clears loading once the bootstrap reads land'` and `'clears loading even when a bootstrap read fails'` (`stores/port.spec.js`) — `connect()`'s `loading` lifecycle itself, independent of any component.

---

### BR-030 (Phase 16h) — A tenant minted by accounts-service is immediately usable by Sea Freight Flow, without an operator needing to switch the Admin UI to it first

**Found while investigating a related report** ("the port dropdown is empty for a brand-new tenant"): a genuinely new tenant's `api.*` adapter didn't exist on this process at all yet, so *every* `api.*` request against it — ships, containers, ports alike — timed out silently (5s, then swallowed by the browser's own `.catch(() => {})`), reading as "this tenant has nothing" rather than "not provisioned yet." `EnsureAllTenants` (composition.go, Startup) only covers tenants that already existed when this process last started; before this rule, a tenant minted afterward stayed unprovisioned until either a restart or an operator happened to call `POST /api/tenant/switch` for it (the Admin/Dictionary frontend's own tenant selector) — a call Sea Freight Flow never makes, since it authenticates straight into NATS (Phase 15d).

`accounts-service` now publishes `notify.accounts.account.created` the instant a create fully succeeds (BR-AC08, `BUSINESS_RULES-ACCOUNTS.md`) — a context-free subject, since accounts-service has no `{context}` of its own. `shipping-service` subscribes to it on its permanent DEFAULT-account connection and reactively provisions that tenant's resources (JetStream stream, KV buckets, projectors, `browserrpc.Adapter`), the exact same idempotent path `EnsureAllTenants` already used for tenants present at startup — this rule just adds a second trigger for tenants minted afterward, closing the gap `EnsureAllTenants`'s own doc comment already flagged.

- **Enforced in:** `dictionary/internal/rest/tenant.go`'s `EnsureTenantByName` (wraps `discoverTenants` + the existing `ensureTenantResources`, a no-op if the tenant isn't yet visible in `CredsDir` rather than an error — defensive against a stray/duplicate delivery, not expected to actually race the creds-file write in practice); `dictionary/composition.go`'s `mono.NC().Subscribe("notify.accounts.account.created", ...)`, wired right after `EnsureAllTenants` in `Module.Startup`.
- **Test:** `'EnsureTenantByName provisions globex's api.* adapter without ever calling SwitchTenant for it'` (`dictionary/tenant_switch_test.go`) — proves the actual observable behavior end to end against the real shipped `nats.conf`/creds: a request on `globex`'s account gets no reply before `EnsureTenantByName`, then a working reply after, with `SwitchTenant("globex")` never called. `'EnsureTenantByName is a no-op, not an error, for a name with no creds file'` (same file) — the defensive race guard. BR-AC08's own tests (`handler_test.go`, accounts-service) cover the publish side. Live-verified on the running stack: a tenant created via `POST /api/accounts` answered an `api.*` request over its own NATS creds (`nats req`) within ~20ms, and its Register Ship dialog's port dropdown populated immediately in the browser — neither `/api/tenant/switch` nor a restart involved.

---

### BR-031 (Phase 16i) — A tenant suspended by accounts-service stops holding shipping-service resources open, instead of reconnect-looping forever against deleted credentials

**The mirror of BR-030, found while investigating a related question** ("what happens when an RPC is sent from a suspended account?"). NATS force-evicts every connection on an account the instant it's revoked at the resolver (`$SYS.REQ.CLAIMS.DELETE`) — verified against the running stack (2026-08-03; see `ARCHITECTURE-ACCOUNTS.md` § 2t-a), correcting an earlier doc comment on `accounts-service`'s `Provisioner.DeleteAccount` that claimed the opposite. Before this rule, that eviction left `shipping-service`'s per-tenant connection (`nats.go`'s default reconnect logic) retrying forever against a `.creds` file `suspendAccount` had already deleted (`accounts/handler.go`) — one permanent, log-spamming loop per suspension, cleared only by a process restart. The browser side of this was already correct by accident (`connectInfo`'s existing 403 check refuses re-authentication), but its refusal was set on `useNatsConnection.js`'s `lastError` and never rendered anywhere, so a suspended session's panels just went quiet with no explanation.

`accounts-service` now publishes `notify.accounts.account.suspended` the instant a suspend fully succeeds (BR-AC09, `BUSINESS_RULES-ACCOUNTS.md`) — same context-free subject family and same DEFAULT-account connection as BR-AC08's created event. `shipping-service` subscribes to it and tears that tenant's resources down: stops its projectors and `browserrpc.Adapter`, then explicitly closes shipping-service's own connection to that account — the explicit `Close()` is what actually disables `nats.go`'s automatic reconnect; the account being unresolvable does not, by itself, stop the client from retrying. Sea Freight Flow's `App.vue` now also renders `lastError` as a danger `Tag` in the topbar whenever it's non-empty, clearing automatically once a connection succeeds again — no separate acknowledgment step, since `useNatsConnection.js`'s `connect()` already resets it to `''` on success.

Deliberately out of scope for this rule: a terminal-vs-transient classification of connection/JetStream errors as a backstop for a missed or out-of-band suspension (an operator revoking an account directly via `nsc`, bypassing `accounts-service` entirely). The event above covers the normal path; the backstop is a separate, larger design decision (see `ARCHITECTURE-ACCOUNTS.md` § 2t-a's "Proposed" section) not yet implemented.

- **Enforced in:** `dictionary/internal/rest/tenant.go`'s `TeardownTenantByName` (mirrors `EnsureTenantByName`'s shape: `deps.TenantResources` lookup, no-op — not an error — if the tenant was never provisioned or already torn down); `dictionary/composition.go`'s `mono.NC().Subscribe("notify.accounts.account.suspended", ...)`, wired right after the BR-030 subscription. `accounts-service`'s `Handlers.publishAccountSuspended` (BR-AC09) is the producer. `frontend/seafreight-app/src/App.vue`'s `lastError` `Tag` in the topbar (`data-testid="connection-error"`).
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
