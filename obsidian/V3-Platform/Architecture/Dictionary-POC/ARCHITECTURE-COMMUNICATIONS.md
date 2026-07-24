# Architecture — Inter-Service Communications

Deep reference for how services in this demo talk to each other and to
clients: REST/Swagger, NATS core request/reply (`rpc.*`), and how they relate
to the existing JetStream event backbone. For the CQRS shape taxonomy see
[ARCHITECTURE.md](ARCHITECTURE.md); for refdata-service's own seeding/schema
design see
[ARCHITECTURE-DICTIONARY.md](ARCHITECTURE-DICTIONARY.md).

**Status: draft / proposed.** The `rpc.*` adapter described here does not
exist yet — this document captures the design under discussion before an
implementation plan is written. See the diagram below (page "PROPOSED —
Dual-transport RPC (draft)" in
[architecture-dictionary.drawio](architecture-dictionary.drawio)).

![Proposed dual-transport RPC architecture](images/rpc-proposed-dual-transport.png)

---

## 1. Overview

Two transports serve different purposes and are not interchangeable:

| Transport | Use for | Feature |
|---|---|---|
| REST + Swagger | External/edge clients (UIs, third parties), full CRUD surface | Existing `rest/` adapter per service |
| NATS core request/reply (`rpc.*`) | Internal, synchronous, service-to-service calls that need an immediate result | Proposed `natsrpc/` adapter, subset of operations only |
| JetStream (`cmd.*` / `evt.*`) | Durable async commands and immutable domain facts | Existing — see [ARCHITECTURE.md](ARCHITECTURE.md) and CLAUDE.md's stream/subject rules |

`rpc.*` is scoped deliberately narrow: only the specific cross-service lookups
a caller needs synchronously (e.g. shipping-service calling refdata-service
for a reference-data lookup), not a 1:1 mirror of the REST surface.

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
  HTTP ↔ application calls.
- `natsrpc/` (proposed) — NATS request/reply adapter, built on the NATS
  Micro/Services framework, calling the **same** `commands`/`queries`
  methods as `rest/`. No changes needed to `commands`/`domain` to add it —
  both services already isolate transport from application logic behind
  `rest.Deps`, which is exactly the seam a second transport plugs into.

```
HTTP adapter                         Application                    NATS adapter

GET /api/refdata/.../item  ───►  Items.Get(id)  ◄───  rpc.acme.refdata.item.get.v1
```

Only wire `rpc.*` for the specific operations another service needs to call
synchronously — see the diagram in § 1 above for the concrete
shipping-service → refdata-service example.

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

**Dual transport as a built-in test harness (Swagger + REST).** Because
`rest/` and `natsrpc/` call the identical `commands`/`queries` methods, the
existing Swagger-documented REST surface effectively doubles as an isolation
tool for the new RPC layer:

- Manual/exploratory testing via Swagger UI (`/swagger/`) already served by
  both services — no NATS client needed to exercise a service's behavior.
- Contract/integration tests can hit REST directly, decoupled from whether
  the NATS RPC layer works — REST and RPC can be verified independently.
- Debugging: if a NATS `rpc.*` call misbehaves, hit the equivalent REST
  endpoint for the same `commands` method to isolate whether the bug is in
  the adapter (`natsrpc/` vs `rest/`) or in the domain logic — since both
  adapters call the identical function, a REST failure with the same input
  rules out the transport and points at the application/domain layer instead.

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

## 7. Relationship to the existing event backbone

JetStream `evt.*` streams (`SHIPPING`, `REFDATA`) are unaffected by this
addition — a separate concern from the RPC layer described here. See
[ARCHITECTURE.md](ARCHITECTURE.md) and CLAUDE.md's "Stream / subject design"
section for the event-stream rules (`LimitsPolicy`, the fixed `evt` leading
token, etc.).
