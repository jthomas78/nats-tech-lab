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
SSE for live views) — nothing here changes their transport. It also doesn't
change the KV-first cache-read pattern (BR-D08): a consumer that gets a cache
hit never calls either transport. This requirement governs what happens next
— the cache-miss/refetch call to the owning service — not the cache layer
itself.

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

**No dedicated stream.** A `RPCTRACE`-style JetStream stream (`LimitsPolicy`,
short `MaxAge`) was considered as a way to give a reconnecting tab a few
seconds of catch-up buffer, but was rejected as unnecessary: the stated
requirement is "only show when the app is visible," which plain Core NATS
pub/sub already satisfies for free — no stream to provision, retain, or
monitor. Reach for a `RPCTRACE` stream later only if a genuine "catch up on
reconnect" requirement emerges; don't build it speculatively.

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
  the old `WithNATS` option.
- REST/Swagger continues to exist as `refdata-service`'s inbound surface for
  frontend/edge clients and for humans/test suites debugging directly (§5)
  — that is unaffected. What's removed is `shipping-service` (or any other
  backend service) acting as an HTTP *client* of it.
- Frontend/edge REST traffic and the KV-first cache-read pattern (BR-D08)
  are unaffected — see § 1's "Backend-to-backend vs. frontend-to-backend"
  note.

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
bundled UI-copy catalog when refdata is unreachable) remains the precedent
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
