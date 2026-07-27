# Architecture — Inter-Service Communications

Deep reference for how services in this demo talk to each other and to
clients: REST/Swagger, NATS core request/reply (`rpc.*`), and how they relate
to the existing JetStream event backbone. For the CQRS shape taxonomy see
[ARCHITECTURE.md](ARCHITECTURE.md); for refdata-service's own seeding/schema
design see
[ARCHITECTURE-DICTIONARY.md](ARCHITECTURE-DICTIONARY.md).

**Status: IMPLEMENTED (Phase 12.10, 2026-07-24).** The design below is built
as described: `refdata-service`'s `internal/natsrpc/adapter.go` (built on
`github.com/nats-io/nats.go/micro`) serves `rpc.{context}.refdata.item.get.v1`
off the same `commands.LocalizationHandler.ResolveItem()` method
`GET /api/refdata/{context}/{type}/{code}` calls (BR-D25); `shipping-service`'s
`internal/refdataconsumer` consumes it as an optional fallback ahead of REST
(`refdataconsumer.WithNATS(nc)`); the `obs.rpc.*` best-effort observability
side-channel (BR-D26) feeds a live "RPC Traffic" panel in the Admin UI
(`frontend/admin`) via a new SSE bridge (`GET /api/rpc-watch`). See
`BUSINESS_RULES-REFDATA.md`'s BR-D25/BR-D26 for the enforced rules and their
tests (`refdata/natsrpc_test.go`). The diagram below remains the accurate
reference for the shape actually built (page "PROPOSED — Dual-transport RPC
(draft)" in [architecture-dictionary.drawio](architecture-dictionary.drawio)
— title kept as-is; only this status line changed).

**Amendment status: IMPLEMENTED (Phase 12.11, 2026-07-24).** An audit of
actual inter-service traffic found `rpc.*` was a minority transport under
the design as first shipped — only `Lookup`/`item.get` had an RPC path at
all, gated behind a KV cache miss, with any RPC error (or the absence of
`WithNATS`) falling straight through to REST; `ResolveType`,
`LookupAtVersion`, and `Locales` had no RPC path whatsoever and always used
REST. The requirement changed twice in discussion (RPC-primary-with-REST-
fallback, then RPC-primary-with-circuit-breaker) before landing on what's
now built: **NATS-only for backend-to-backend calls, full stop.** No REST
fallback, no circuit breaker, no host:port coupling between backend
services at all. `refdata-service`'s `internal/natsrpc/adapter.go` now
serves all four operations (`item.get`, `type.list`, `item.get-versioned`,
`locales.list`, all via a `natsrpc.Deps` struct — see § 3);
`shipping-service`'s `internal/refdataconsumer` calls `rpc.*` exclusively
(`New(kv, nc, ...)` — `nc` is a required constructor argument, not an
option), with a bounded number of retries and backoff
(`WithRPCRetries`/`WithRPCBackoff`/`WithRPCTimeout`, defaulting to 2 retries,
150ms linear backoff, 3s per-attempt timeout) before returning
`ErrRPCUnavailable`. All REST-client coupling
(`REFDATA_SERVICE_URL`/`refdataServiceURL()`/`http://localhost:7201`,
`baseURL`/`httpc`, `fetchViaAPI`/`fetchTypeViaAPI`/`fetchVersionedViaAPI`) was
deleted from `refdataconsumer` and from `docker-compose.yml`. Frontend/edge
REST traffic is unaffected. See § 7 for the full design record and
`BUSINESS_RULES-REFDATA.md`'s BR-D28 for the enforced rule and its tests.

**Amendment status: IMPLEMENTED (Phase 12.12, 2026-07-27).** Phase 12.11 made
`rpc.*` the sole *transport*, but `refdataconsumer` still read the
`refdata-{context}` KV bucket directly as its hot-path *cache* — a bounded-
context violation: that bucket is refdata-service's own internal projection,
and a consumer coupled to its shape has to change in lockstep whenever the
producer's cache shape changes (as happened when `kvcache.Entry`'s `Item`
field was slimmed from `domain.DictionaryItem`/`attrs` to a leaner
`CacheItem` with resolved `label`/`description` — a refdata-service-internal
change that nonetheless forced a `refdataconsumer` mirror-struct update).
**The KV-first cache-read tier now lives entirely inside refdata-service's
own `natsrpc` handler** (`resolveItemKVFirst`/`resolveTypeKVFirst` call
`kvcache.Projector.ReadEntry`/`ReadType` before falling through to Postgres),
not in the consumer. `refdataconsumer.Consumer` holds only a `*nats.Conn` —
no `*kvstore.Store`, no KV bucket naming knowledge at all — and every
`Lookup`/`ResolveType`/`LookupAtVersion`/`Locales` call goes straight to
`rpc.*`; the shipping backend's `/api/refdata-watch` SSE endpoint likewise
now subscribes to refdata-service's published `evt.{context}.refdata.*.changed`
change-event stream (the `REFDATA` JetStream stream, § 2's subject taxonomy
and § 8's event-backbone note) instead of watching the KV bucket directly.
See § 9 for the full design record and `BUSINESS_RULES-REFDATA.md`'s BR-D08
for the enforced rule and its tests.

![Dual-transport RPC architecture](images/rpc-proposed-dual-transport.png)

---

## 1. Overview

Two transports serve different purposes and are not interchangeable:

| Transport | Use for | Feature |
|---|---|---|
| REST + Swagger | Frontend/edge clients (UIs, third parties), full CRUD surface. **Inbound only** — a service exposes REST for callers outside the backend, but never acts as an HTTP *client* to another backend service (see amendment below) | Existing `rest/` adapter per service |
| NATS core request/reply (`rpc.*`) | **Sole transport for backend-to-backend synchronous calls** — no REST fallback (Phase 12.11, proposed) | `natsrpc/` adapter (Phase 12.10, extending to full coverage in 12.11) |
| JetStream (`cmd.*` / `evt.*`) | Durable async commands and immutable domain facts | Existing — see [ARCHITECTURE.md](ARCHITECTURE.md) and CLAUDE.md's stream/subject rules |

**Backend-to-backend vs. frontend-to-backend.** This transport split governs
service-to-service calls only. Frontend clients (`frontend/admin`,
`frontend/refdata`, `frontend/seafreight-app`) keep talking REST/Swagger (and
SSE for live views) — nothing here changes their transport. Since Phase 12.12
(BR-D08, § 9), the KV-first cache-read pattern lives entirely inside
refdata-service's own `rpc.*` handler — a consumer's `rpc.*` call may still
be served from a warm cache without a Postgres round-trip, but that cache
tier is internal to refdata-service and invisible to the caller; no other
service ever reads refdata-service's KV bucket directly.

**Backend services should only be aware of NATS.** For inter-service calls, a
backend service holds a NATS connection and nothing else — no HTTP client, no
base URL, no hostname/port config pointing at a peer backend service. REST
stays purely as the surface a service exposes *inbound* to frontend/edge
callers; no backend service is itself a REST *client* of another backend
service. This is a hard architectural invariant (location transparency), not
a preference — see § 7.

`rpc.*` was originally scoped deliberately narrow — only the specific
cross-service lookups a caller needed synchronously, not a 1:1 mirror of the
REST surface. As of the Phase 12.11 amendment, "narrow" no longer means
"optional/fallback-only": every backend-to-backend synchronous operation
must have an `rpc.*` counterpart and use it as its only path. It's still not
a 1:1 mirror of the *entire* REST surface — operations no other backend
service calls synchronously (e.g. admin-only writes) don't need one.

## 2. Subject taxonomy

Parallel to the existing event subject grammar
(`evt.{context}.{service}.{entity}.{id}.{event}`):

```
rpc.{context}.{service}.{entity}.{action}.v{n}
```

- `rpc` — fixed literal, like `evt`; avoids the same wildcard-overlap issue
  with `$SYS.>` / `$JS.API.>` / `$SRV.>`.
- `{context}` — tenant/region scope, only included at the subject level if a
  caller genuinely needs subject-level routing/authz by context; otherwise
  carry it in the payload and drop it from the subject.
- `{service}` — the service that owns/answers the call.
- `{entity}.{action}` — mirrors the `commands.XHandler` / `queries.X` method
  name 1:1, so Swagger `operationId`, subject, and Go method stay aligned.
- `v{n}` — same versioning discipline as event subjects.

Example: `rpc.acme.refdata.item.get.v1`.

Three subject families, not interchangeable:

- `rpc.*` — query-like or synchronous operations that return a result now.
- `cmd.*` — durable asynchronous intent; publish, then observe the result via
  events or a status/read model. (Not yet used in this repo; noted for
  completeness.)
- `evt.*` — immutable facts, not an alternate API method. Never map `evt.*`
  to a Swagger-shaped contract.

## 3. Dual-adapter pattern

Each service keeps one transport-neutral application layer
(`commands.*Handler` / `queries.*`) behind two adapters:

- `rest/` — existing HTTP + Swagger adapter. No business logic; translates
  HTTP ↔ application calls. **Inbound-only**: a service exposes this for
  frontend/edge callers; no other backend service in this repo is an HTTP
  client of it.
- `natsrpc/` (proposed) — NATS request/reply adapter, built on the NATS
  Micro/Services framework, calling the **same** `commands`/`queries`
  methods as `rest/`. No changes needed to `commands`/`domain` to add it —
  both services already isolate transport from application logic behind
  `rest.Deps`, which is exactly the seam a second transport plugs into. This
  is the **only** adapter a backend service uses to call another backend
  service.

```
HTTP adapter                         Application                    NATS adapter

GET /api/refdata/.../item  ───►  Items.Get(id)  ◄───  rpc.acme.refdata.item.get.v1
```

Wire `rpc.*` for every operation another service calls synchronously — see
§ 7 for why this is now "every operation," not an opportunistic subset — and
see the diagram in § 1 above for the concrete shipping-service →
refdata-service example.

## 4. Discovery & documentation

- **Runtime discovery** — building `natsrpc/` on the NATS Micro/Services
  framework (`github.com/nats-io/nats.go/micro`) gives free discovery via
  `$SRV.PING` / `$SRV.PING.<name>` (who's alive), `$SRV.INFO.<name>`
  (declared endpoints/subjects), and `$SRV.STATS.<name>` (call counts,
  errors, latency) — queryable via `nats micro list` / `nats micro info
  <name>`. Each service self-declares its endpoints at startup; no separate
  registry to keep in sync.
- **Static documentation** — Swagger/OpenAPI (already generated from
  `@Summary`/`@Param`/`@Router` annotations, served at `/swagger/` in both
  services) documents REST. `rpc.*` and `evt.*` should be documented in
  AsyncAPI (or a shared operation/schema definition both docs generate
  from), keeping `operationId` (Swagger), subject (NATS), and application
  method name aligned three ways.

## 5. Benefits

**Location independence and transparency (NATS).** A `natsrpc/` caller
addresses a subject, not a host:port — the client doesn't know or care which
instance of refdata-service answers, whether it's been rescheduled, scaled
out, or moved between nodes. NATS's subject-based addressing plus
Micro/Services discovery means service-to-service calls stay correct through
deploys and topology changes without service-discovery infrastructure,
client-side load balancer config, or DNS/registry updates — the caller's code
never changes when the callee's location does.

**Dual transport as a built-in test/debugging harness (Swagger + REST) — for
humans and test suites, not for any backend service's own runtime code.**
Because `rest/` and `natsrpc/` call the identical `commands`/`queries`
methods, the existing Swagger-documented REST surface effectively doubles as
an isolation tool for the RPC layer — but only ever exercised by a person or
a test process, never by another backend service acting as an HTTP client
(that coupling is exactly what § 1's "backend services should only be aware
of NATS" invariant rules out):

- Manual/exploratory testing via Swagger UI (`/swagger/`) already served by
  both services — no NATS client needed to exercise a service's behavior.
- Contract/integration tests (run by CI or a developer, not by the other
  backend service) can hit REST directly, decoupled from whether the NATS
  RPC layer works — REST and RPC can be verified independently.
- Debugging: a person can hit the equivalent REST endpoint for the same
  `commands` method to isolate whether a misbehaving `rpc.*` call is a bug
  in the adapter (`natsrpc/` vs `rest/`) or in the domain logic — since both
  adapters call the identical function, a REST failure with the same input
  rules out the transport and points at the application/domain layer
  instead. This is a manual diagnostic step, not an automatic fallback the
  system takes on its own.

## 6. RPC observability (Admin UI live view)

The Admin UI wants to show `rpc.*` calls and replies as they happen, with no
requirement to see history from before the panel was opened. This is **not**
a JetStream concern — it's a separate, best-effort side-channel off the
`natsrpc/` adapter.

**No dedicated stream (original decision, since revised — see below).** A
`RPCTRACE`-style JetStream stream (`LimitsPolicy`, short `MaxAge`) was
considered as a way to give a reconnecting tab a few seconds of catch-up
buffer, but was rejected as unnecessary: the stated requirement at the time
was "only show when the app is visible," which plain Core NATS pub/sub
already satisfies for free — no stream to provision, retain, or monitor. The
explicit condition for revisiting this was: reach for a `RPCTRACE` stream
only if a genuine "catch up on reconnect" requirement emerges; don't build it
speculatively.

**Revised (BR-D29): `RPCTRACE` added once the requirement became concrete.**
The requirement narrowed from "live while visible" to "show whatever
happened in the last 10 minutes, even if the tab wasn't open for it" — a
genuine catch-up-on-reconnect need, exactly the condition above. `RPCTRACE`
is a `LimitsPolicy` stream with subject filter `obs.rpc.>` and `MaxAge: 10m`,
provisioned by `refdata-service`'s `composition.go` alongside the `REFDATA`
change stream. `publishObs()` now publishes via `PublishAsync` when
JetStream is configured (falling back to the original `nc.Publish` otherwise)
— `PublishAsync` only blocks for the send, never the server's ack, so the
"must never block the real RPC reply" invariant this section's design
already establishes is unchanged. Because a JetStream publish is still an
ordinary NATS message on the wire, the live-tail design below (`obs.rpc.*`
fire-and-forget publish) is otherwise untouched: `shipping-service`'s
`/api/rpc-watch` SSE handler now opens an ephemeral ordered consumer on
`RPCTRACE` (`DeliverAllPolicy`) to drain the retained backlog *before*
falling through to the same live core subscribe it always used, established
first so nothing published during the drain is missed. See `BR-D29` in
`BUSINESS_RULES-REFDATA.md` for the full rule.

**Design: `obs.rpc.*` fire-and-forget publish, both directions.** Each
`natsrpc/` Micro endpoint handler publishes two observability messages
around the real call — one for the request, one for the reply — on a subject
distinct from `rpc.*` itself (e.g. `obs.rpc.{context}.{service}.{entity}.{action}`),
so the Admin UI subscribes to `obs.rpc.>` independently of the actual RPC
traffic:

```go
func (a *Adapter) getItemHandler(req micro.Request) {
    a.publishObs("request", subject, correlationID, req.Data())

    result, err := a.queries.GetItem(...)
    reply := ...

    a.publishObs("reply", subject, correlationID, reply)   // include on error too
    req.Respond(reply)
}
```

- **Reply payload is captured, but only because it's explicitly published a
  second time** — Core NATS request/reply is peer-to-peer, so nothing about
  the response is visible to a third-party observer unless the adapter emits
  it itself. There is no way to pair this "for free."
- **Correlation** — reuse the request's reply-to inbox (or a generated
  `correlationID`) in both the request and reply obs publishes, so the Admin
  UI can join them into one row instead of two unrelated list entries.
- **Failure case** — publish the reply-side obs event even when the handler
  errors, so a failed call is visible in the UI, not just successes.
- **Decoupled from the real call path** — the obs publish is best-effort and
  async; it must never add latency to, or be able to fail, the actual RPC
  reply to the caller. A slow or disconnected Admin UI subscriber is a
  no-op for the real cross-service call (core NATS drops published messages
  with no subscriber).
- **Payload sensitivity** — the obs publish is a second copy of the same
  payload on a subject anyone subscribed to `obs.rpc.>` can see. Apply the
  same redaction rules to it as would apply to logging that payload
  elsewhere; not expected to be an issue for the reference-data lookups this
  repo currently scopes `rpc.*` to, but worth checking per-operation as new
  `rpc.*` endpoints are added.

## 7. `rpc.*` as the sole backend-to-backend transport (Phase 12.11, IMPLEMENTED 2026-07-24)

**Requirement — this supersedes two earlier drafts of this section
(RPC-primary-with-REST-fallback, then RPC-primary-with-circuit-breaker):**
for backend-to-backend synchronous calls (frontend-to-backend is
unaffected), `rpc.*` is the **only** transport. No REST fallback in any
form — not a per-call fallback, not a circuit breaker that degrades to REST.
A bounded number of retries against `rpc.*` (with backoff), then an error
returned to the caller. **Backend services should only be aware of NATS**
for inter-service calls — no HTTP client, base URL, or hostname/port config
pointing at a peer backend service. This is a hard invariant (location
transparency), not a resilience trade-off to weigh case by case.

**What the Phase 12.10 audit found (2026-07-24):** actual consumer traffic
from `shipping-service` → `refdata-service` breaks down as:

| Consumer method | KV direct | NATS RPC | REST |
|---|---|---|---|
| `Lookup` (single item) | yes (`refdata-{context}`) | yes — but only on a KV miss, and only because `WithNATS` happens to be wired | yes — final fallback, always |
| `ResolveType` (list) | yes (enumerates bucket) | only indirectly, once per item, via `Lookup` | yes, if the bucket's empty/absent |
| `LookupAtVersion` (pinned corpus version) | yes (`refdata-{context}-v{N}`) | no RPC path exists at all | yes, final fallback |
| `Locales` | no | no | yes, sole transport |

Only `Lookup` had any RPC path, and even there it was the third tier: a KV
cache hit short-circuited everything; only on a miss did it try
`rpc.{context}.refdata.item.get.v1`, and any RPC error fell straight
through to REST. `Locales` in particular had **no RPC path at all** — so
12.11 landed full `rpc.*` coverage for all four operations *first*, then cut
REST out *after*, rather than in the same commit as a half-covered
transport (per this section's own sequencing note when it was still
PROPOSED).

**What was built:**

- All four `refdataconsumer` operations have an `rpc.*` counterpart, served
  by `refdata-service`'s `internal/natsrpc/adapter.go` via a `natsrpc.Deps`
  struct (`Localizations`, `Items`, `VersionReader`, `Projector`, `Log` —
  each nil-safe, mirroring the REST layer's own nil checks): `item.get`
  (Phase 12.10, unchanged), new `type.list` (`ResolveType` — reuses
  `ItemGetResponse` per item), `item.get-versioned` (`LookupAtVersion` — the
  corpus version travels in the request body, not the subject; response is
  `kvcache.VersionedEntry` directly, no separate wire shape), and
  `locales.list` (`Locales`).
- On a KV cache miss/stale entry, the consumer calls `rpc.*` via
  `requestRPC()`, which makes a bounded number of attempts (default: 1
  initial + 2 retries = 3 total) with linear backoff (default 150ms ×
  attempt number) and a 3s per-attempt timeout, all overridable via
  `WithRPCRetries`/`WithRPCBackoff`/`WithRPCTimeout` (tests use these to stay
  fast). If every attempt fails, the call returns `ErrRPCUnavailable`
  (wrapping the last underlying error) — there is no other transport to fall
  through to.
- **Not-found vs. other errors, without HTTP status codes to lean on:**
  every `natsrpc` endpoint's error response now carries a `notFound bool`
  alongside `error string` (`isNotFoundErr()` in `adapter.go`, mirroring the
  same domain-sentinel set REST's `writeError` status-code switch checks —
  `ErrItemNotFound`, `ErrTypeNotFound`, `ErrReferenceNotFound`,
  `ErrContextNotFound`, `ErrDraftNotFound`, `kvcache.ErrVersionedKeyNotFound`).
  The consumer's `checkRPCError()` maps `notFound: true` to this package's
  `ErrNotFound`, anything else to a generic wrapped error — the one
  categorization the old REST-fallback path used to get "for free" from
  REST's own not-found handling, now made explicit at the wire level instead.
- **All REST-client coupling is removed from `internal/refdataconsumer`**,
  not just deprioritized: the `REFDATA_SERVICE_URL` env var
  (`docker-compose.yml`), the `refdataServiceURL()` function and its
  hardcoded `http://localhost:7201` default (`dictionary/composition.go`),
  the `baseURL`/`httpc` fields, and the `fetchViaAPI` / `fetchTypeViaAPI` /
  `fetchVersionedViaAPI` / REST-based `Locales` methods on `Consumer` are all
  deleted. `Consumer` holds a `*kvstore.Store` and a `*nats.Conn` and nothing
  else; `New(kv, nc, ...)` takes `nc` as a required constructor argument, not
  the old `WithNATS` option. *(Superseded by Phase 12.12, § 9: `Consumer` no
  longer holds a `*kvstore.Store` either — `New(nc, ...)` takes just the NATS
  connection.)*
- REST/Swagger continues to exist as `refdata-service`'s inbound surface for
  frontend/edge clients and for humans/test suites debugging directly (§5)
  — that is unaffected. What's removed is `shipping-service` (or any other
  backend service) acting as an HTTP *client* of it.
- Frontend/edge REST traffic is unaffected — see § 1's "Backend-to-backend
  vs. frontend-to-backend" note. The KV-first cache-read pattern (BR-D08) was
  still consumer-side direct-KV-read at this phase; § 9 moves it inside
  refdata-service.

**Product-behavior consequence, resolved:** before this change, a KV miss
always eventually succeeded via REST — the consumer's only failure mode was
a wrong/missing item. After this change, a sustained NATS outage on a KV
miss produces `ErrRPCUnavailable` with no fallback, where none existed
before. This is handled at the REST layer: `dictionary/internal/rest`'s
`getRefdataDemo`, `listRefdataType`, and `listRefdataLocales` handlers now
share a `writeRefdataError()` helper that maps `refdataconsumer.ErrNotFound`
→ 404 (unchanged) and `refdataconsumer.ErrRPCUnavailable` → 503 ("reference
data temporarily unavailable, try again"), distinct from the generic 500 for
a genuine internal fault — a "the dependency is down" signal rather than
"something is broken." BR-D11's existing pattern (frontend falls back to a
bundled string catalog when refdata is unreachable) remains the precedent
for the frontend-to-backend layer, unaffected by and unrelated to this
backend-to-backend change. This 503 mapping is REST-layer error handling for
a Phase 11.3/11.6 demo endpoint, not a Ship/Container domain invariant, so it
is not tracked as a `BUSINESS_RULES-SHIPPING.md` BR entry — its tests are
`dictionary/internal/rest/refdata_demo_error_test.go`.

See `BUSINESS_RULES-REFDATA.md`'s BR-D28 for the corresponding business rule
and its tests, and `.claude/plans/Dictionary-POC-Plan.md`'s Phase 12.11 for
the task checklist (all tasks complete).

## 8. Relationship to the existing event backbone

JetStream `evt.*` streams (`SHIPPING`, `REFDATA`) are unaffected by this
addition — a separate concern from the RPC layer described here. See
[ARCHITECTURE.md](ARCHITECTURE.md) and CLAUDE.md's "Stream / subject design"
section for the event-stream rules (`LimitsPolicy`, the fixed `evt` leading
token, etc.).

---

## 9. refdata-service's KV cache becomes internal-only (Phase 12.12, IMPLEMENTED 2026-07-27)

**Problem:** Phase 12.11 made `rpc.*` the sole *transport* for backend-to-
backend calls, but `refdataconsumer` still read the `refdata-{context}` KV
bucket *directly* as its hot-path cache, ahead of any `rpc.*` call. That
bucket is refdata-service's own internal projection — Postgres is the source
of truth, KV is a derived read cache rebuilt from Postgres on every mutation
(see `ARCHITECTURE-DICTIONARY.md`'s Q5 versioned-read protocol). A consumer
reading another service's internal cache bucket directly is the NATS-KV
equivalent of one microservice querying another's database tables: it
couples the two services to a storage shape neither transport (REST or
`rpc.*`) was meant to expose. This was concretely demonstrated the same week:
slimming `kvcache.Entry`'s `Item` field from `domain.DictionaryItem`
(carrying an `attrs map[string]any`) to a leaner `CacheItem` with the
default-locale `label`/`description` resolved inline — a refdata-service-
internal storage optimization — required a coordinated mirror-struct update
in `shipping-service`'s `refdataconsumer`, purely because that consumer had
its own copy of the KV entry's JSON shape.

**What changed:** the KV-first cache-read tier moved from the consumer into
refdata-service's own `rpc.*` handler.

- **`kvcache.Projector` gained two read-side methods**, the counterparts of
  its existing write-side `rebuildEntry`/`rebuildMeta`: `ReadEntry(ctx,
  itemContext, typeKey, code)` returns a type's cached `Entry` only if it
  exists and its stamped version matches the type's current `_meta` version
  (nil on a miss or stale entry, mirroring `rebuildEntry`'s own freshness
  check); `ReadType(ctx, itemContext, typeKey)` returns every entry for a
  type only if the cache is complete and every entry is fresh (a partial or
  stale cache is treated as a miss for the whole type, not patched
  entry-by-entry). `kvstore.Store` (refdata-service's copy) gained a
  `Keys()` method — refdata-service's own equivalent of the enumeration
  `refdataconsumer` used to do against the bucket it no longer touches.
- **`natsrpc.Adapter`'s `handleItemGet`/`handleTypeList`** now call
  `resolveItemKVFirst`/`resolveTypeKVFirst`, which try `ReadEntry`/`ReadType`
  first and only fall through to Postgres (`ResolveItem`/`ListAssignable`)
  on a miss — backfilling the cache afterward exactly as before (BR-D27).
  Label resolution against a cache hit reconstructs a `[]domain.Localization`
  from the entry's `map[string]domain.LocalizationValue` and calls the same
  `domain.ResolveLabel` (BR-D03) the Postgres path uses, so a cache hit and a
  cache miss resolve locale fallback identically. One known asymmetry: a
  cache-hit response's embedded `Item.Attrs` is always empty (the KV cache
  intentionally omits `attrs`, a Postgres/REST-only concern per the
  `CacheItem` refactor above), while a Postgres-served response carries real
  attrs — harmless today since no `rpc.*` consumer reads `Item.Attrs`, but
  worth knowing if that ever changes.
- **`refdataconsumer.Consumer` lost its `*kvstore.Store` field entirely.**
  `New(nc, ...)` takes only the NATS connection.
  `Lookup`/`ResolveType`/`LookupAtVersion` all call their `rpc.*` counterpart
  unconditionally — there is no KV read, no `_meta` version check, and no
  bucket-key enumeration left in this package. The one exception:
  `LookupAtVersion`'s wire protocol (`item.get-versioned`) still returns
  every locale rather than a pre-resolved label, so the consumer still
  applies the BR-D03 fallback chain locally via `resolveLocalization()`
  (renamed from `resolveLabel()` in Phase 12.13, § 10, once it also had to
  resolve `Description`) against that RPC response — this was already true
  before Phase 12.12 and is unchanged; what's gone is only the *KV-hit* path
  that used to short-circuit this call entirely.
- **The shipping backend's `/api/refdata-watch` SSE endpoint** no longer
  watches the `refdata-{emea-acme}` KV bucket (`kvstore.Store.Watch`).
  Instead it subscribes to refdata-service's `REFDATA` JetStream stream,
  filtered to `evt.emea-acme.refdata.>` (the same change-event pointers
  `kvcache.Projector.NotifyItemChanged` already publishes — § 2's subject
  taxonomy, § 8's event-backbone note) via an ordered consumer with
  `DeliverNewPolicy`. No historical replay: a client already does its own
  initial REST fetch on connect (`useRefdataLabels.js`'s `connect()`), so
  the stream only needs to signal "something changed" going forward, not
  reconstruct state. This removes the last direct KV read from
  `shipping-service` for the non-versioned refdata path; `KVRefdata` is
  gone from `dictionary/composition.go` and `rest.Deps` entirely.
- **`shipping-service`'s `internal/kvstore.Store` lost `GetVersioned`/
  `PutVersioned`** (and the `versionedBucketName` helper) — these existed
  solely to seed/read the `refdata-{context}-v{N}` bucket directly, which
  nothing does anymore now that `LookupAtVersion` is `rpc.*`-only.

**What did not change:** REST/Swagger's role for frontend/edge clients (§5);
the `rpc.*` subject taxonomy and wire shapes (§2, §3); the bounded-retry/
`ErrRPCUnavailable` behavior for a sustained `rpc.*` outage (BR-D28, § 7);
Postgres as the single source of truth (`ARCHITECTURE-DICTIONARY.md`'s Q5
protocol is unchanged — only *which side of the RPC boundary* reads the
cache moved).

See `BUSINESS_RULES-REFDATA.md`'s BR-D08 for the corresponding business rule
and its tests.

---

## 10. `CacheItem` drops `Label`/`Description` — default locale becomes mandatory-first (Phase 12.13, IMPLEMENTED 2026-07-27)

§9's `CacheItem` (and `VersionedEntry.Item`, which is the same type) still
carried `Label`/`Description` fields — a "resolved fallback label" computed
at write time by picking the default locale's localization, or, if that was
absent, **whichever locale happened to be first** in the order the
repository returned (`kvcache.go`'s `fallbackLoc()`, and an independent
inlined duplicate of the same logic in `versioned.go`'s `Materialize`). That
"first available" branch was an implicit, unenforced assumption: an item
could end up with only a `fr` localization and have it silently become the
item-level fallback, with nobody having decided `fr` should be the default.

**BR-D30** closes this by making the assumption an explicit, enforced
invariant instead: `LocalizationHandler.SetLocalization` now rejects setting
any locale other than the context's effective default (BR-D15) until that
default locale's localization already exists for the same item. Once that's
true, the default locale's entry in `Entry.Localizations` /
`VersionedEntry.Localizations` is a **guarantee**, not a hope, whenever an
item has any localizations at all — so `CacheItem.Label`/`Description`
become pure duplication and are removed entirely. A reader resolves the
default-locale label straight from `Localizations[defaultLocale]`.

This has a small ripple into `refdataconsumer`: `fetchVersionedViaRPC` used
to read `Description` straight off `entry.Item.Description` (a shortcut that
only worked because that field happened to hold the write-time-resolved
fallback) while resolving `Label` per-locale via the BR-D03 chain. With
`Item.Description` gone, `Description` is now resolved the same way `Label`
is — the function doing so was renamed `resolveLabel` →
`resolveLocalization` and returns both fields together.

This is also a REST wire-format change:
`GET /api/refdata/{context}/{type}/versions/{version}/items/{code}` (and its
list variant) marshal `kvcache.VersionedEntry` straight to the response
body, so `item.label`/`item.description` no longer appear there. No
frontend reads those sub-fields today (confirmed, same search as § 9).

See `BUSINESS_RULES-REFDATA.md`'s BR-D30 for the corresponding business rule
and its tests.
