# Proposed Linebooker V3 Architecture - L4-02 Requirements

This register maintains the requirements represented by
`LB-V3-L4-02 - Distributed Tracing with OpenTelemetry`.

`LB-V3-L4-02` answers one question: **how does one outside call become one
trace across services, NATS accounts, clusters and regions, what does every
span carry, and how do spans reach Grafana without travelling on NATS, in a
local mode and in a live mode?**

Its parent is `LB-V3-L3-10 Observability`. `LB-V3-L3-10` is still `PLANNED`.
`L2-D12` delegates the telemetry architecture to `LB-V3-L3-10`, and
`L3-08-035` delegates the telemetry pipeline to it. This detailed design is
registered under that parent now, on the user's instruction, because the
tracing decision of record already exists.

The decision of record is **ADR-058, Trace Every Service with the OpenTelemetry
SDK, Not Over NATS** (status Proposed). This document is the implementable
design of that ADR. It does not restate the ADR's reasons. Where this document
and ADR-058 disagree, ADR-058 wins and this document is corrected.

`LB-V3-L4-02` is published as a **directional document**. It records a
first-pass position as at 2026-10-19. It is not a settled decision. Nothing in
it is built in V3.

The document is drawn as five A3 landscape sheets:

| Sheet | Question |
|---|---|
| 1 - Proposed Solution | Which parts make up the tracing pipeline, and which path do spans take? |
| 2 - Tracing in the NATS Topology | Where does each part sit among cells, clusters and accounts, and what does a span record at each kind of hop? |
| 3 - Shared Module and Span Contract | What does the shared module own, and which attributes does every span carry? |
| 4 - One Call, End to End | In what order do the client, services, NATS, the agent and Grafana act for one outside call? |
| 5 - Deployment Modes | How does the same code run in local mode and in live mode, and what changes between them? |

## Status vocabulary

- **Included** - visibly represented in the current L4-02 document.
- **Derived** - intentionally delegated to another document or decision.
- **Open** - requires business, legal, regulatory or technical confirmation.

## Maturity vocabulary

Used by the drawing's tile treatment. This describes the element, not the
document.

- **Proposed** - a directional V3 position set by ADR-058 or by this document,
  with no V3 implementation. Dashed grey tile.
- **Open** - not yet decided. Dashed amber tile with a named owner.
- **Refused** - a path the design forbids. Red square or red-outlined tile.

No element on any sheet is drawn as built.

## Terms

The terms are ADR-058's. In short:

- **Trace** - the whole of one outside call, from the moment it reaches the
  platform, through every service, message and reply it causes, including later
  work by consumers of the events it published.
- **Span** - one timed step of work inside a trace.
- **Agent** - a program near the services that receives telemetry over OTLP,
  queues it on disk, batches it, labels it and forwards it to the stores. This
  document also calls it the collector.
- **OTLP** - the OpenTelemetry wire protocol.
- **Body** - the payload of a message, request or reply.

## The control account

`CONTROL` is the standing NATS account for control-plane services, beside
`PLATFORM` (`L3-01-049`, `L3-05-035`). Observability is a control-plane
service. The collector and, when self-hosted, Grafana belong to the control
plane and are drawn in it. Belonging to `CONTROL` is a statement of ownership
and placement. It does not mean spans travel through the `CONTROL` account:
spans never travel on NATS (ADR-058 rule 3).

## L4-02 requirements

| ID | Status | Requirement | L4-02 representation |
|---|---|---|---|
| L4-02-001 | Included | Every V3 service traces with the OpenTelemetry SDK for its language and W3C Trace Context (`traceparent`, `tracestate`). No in-house tracer and no span envelope on NATS (ADR-058 rule 1). | Sheet 1, service tiles; sheet 3 |
| L4-02-002 | Included | One shared module per programming language holds the platform glue. Today that is one Go module. Services call it and do not copy it (ADR-058 rule 2). | Sheet 1 module strip; sheet 3 class view |
| L4-02-003 | Included | The shared module owns six parts: the NATS header carrier, the edge rule, the tenant attributes, the location attributes, body capture, and exporter setup. Each part is a named type in the module. | Sheet 3 class view |
| L4-02-004 | Included | The NATS header carrier injects `traceparent` and `tracestate` into the headers of every outbound NATS message, request and reply, and extracts them from every inbound one. It wraps publish, request, subscribe, JetStream publish and JetStream consume. | Sheet 3; sheet 4 steps 3, 5, 9 |
| L4-02-005 | Included | Domain code may receive the language's own context type. It never imports a tracing library. Only adapters and the shared module do. | Sheet 3 dependency note |
| L4-02-006 | Included | Edge rule: on an inbound browser, public HTTP or `api.*` call, the server span is a new root, sampled by the platform. An incoming `traceparent` is kept only as a span link. A client cannot join a platform trace or turn sampling off (ADR-058 rule 5). | Sheet 2 hop H1; sheet 4 steps 1-2 |
| L4-02-007 | Included | Between platform services, normal parent and child propagation applies on `rpc.*`, `evt.*` and `notify.*`, and on JetStream messages. | Sheet 2 hops H2-H6; sheet 4 |
| L4-02-008 | Included | A JetStream message stores its headers with it. A consumer that reads the message later extracts the stored context and starts a child span in the same trace, so later work by an event consumer stays in the trace. | Sheet 2 hop H5; sheet 4 steps 9-10 |
| L4-02-009 | Included | Resource attributes, set once per process and so present on every span: `service.name`, `service.version`, `service.instance.id`, `deployment.environment.name`, `cloud.region` (ADR-058 rule 7). | Sheet 3 attribute table |
| L4-02-010 | Included | Messaging span attributes, read from the live NATS connection when the span starts, not at startup: `nats.server.name`, `nats.cluster.name`, `server.address`. A reconnect to another cluster changes `nats.cluster.name` and leaves `cloud.region` unchanged (ADR-058 rule 7). | Sheet 2; sheet 3 attribute table |
| L4-02-011 | Included | JetStream span attribute `nats.jetstream.domain`: the domain the call is addressed to, taken from the JetStream options of that call. It is left off when the call names no domain. It is not the domain of the connected server (ADR-058 rule 7). Domain names depend on the cluster shape, which is open (`L4-01-O01`): one domain across a supercluster, or one per cluster (`L4-01-024`). This document fixes no domain names. | Sheet 2 hop H4; sheet 3 |
| L4-02-012 | Included | Standard messaging attributes on every NATS span: `messaging.system` = `nats`, `messaging.destination.name` (the subject), `messaging.operation.type` (`send`, `receive` or `process`). Span kind is producer, consumer, client or server. | Sheet 3 attribute table |
| L4-02-013 | Included | Tenant attributes on the receiving service's span, as two separate attributes. `linebooker.account.receiving` comes from the account of the connection the message arrived on. `linebooker.tenant.origin` comes only from server-written routing data, else it is empty and the span shows as unattributed (ADR-058 rule 6). The attribute names are proposed by this document. | Sheet 2; sheet 3 attribute table |
| L4-02-014 | Included | Neither tenant attribute is ever read from a client header, the body or a requester field. A shared connection in `PLATFORM` or `CONTROL` serves many tenants and never names the originating tenant by itself. Both attributes are metadata and a query filter, not a security boundary. | Sheet 3 trust note |
| L4-02-015 | Included | Across an account boundary, the originating tenant comes from the request-info header (`Nats-Request-Info`) that the NATS server writes on a service import. The importing account is always in it; the import's `share` setting adds more requester detail. The exporting account receives it; the client cannot set it. The module maps the importing account to a tenant only through the platform's own account-to-tenant record, and only on an import path the platform provisioned. | Sheet 2 hop H2 |
| L4-02-016 | Included | Bodies are off by default. Body capture is turned on per service. When on, the module redacts personal and secret fields, then truncates to a size limit, in the service, before export (ADR-058 rule 8). | Sheet 3 body capture type |
| L4-02-017 | Included | Export never blocks a business call: a finished span goes into a fixed-size in-memory queue; a background worker sends batches; a full queue drops and counts; every send has a time limit; shutdown waits a fixed short time (ADR-058 rule 9). | Sheet 3 exporter type; sheet 4 step 11 |
| L4-02-018 | Included | Spans leave each service over OTLP to the agent in the same cell, on their own network path. OTLP gRPC on port `4317` is the default; OTLP HTTP on port `4318` is allowed. | Sheet 1 OTLP path; sheet 5 |
| L4-02-019 | Included | Refused path: spans never travel on NATS subjects, never cross an account by export or import, and never sit in a JetStream stream (ADR-058 rule 3). This document proposes the same for logs and metrics; that part stays provisional until the logs and metrics ADR decides it. | Sheet 1 refused path; sheet 2 |
| L4-02-020 | Included | One agent deployment per cell. A service sends only to its own cell's agent, so telemetry stays subject to the cell's residency policy (`L2-022`). | Sheet 2 per-cell agent |
| L4-02-021 | Included | The agent queues on disk. The queue survives an agent restart and a hard kill, is sized for a stated store outage, counts loss when full, and drains into the store with every span stored. Tests on the chosen deployment prove each of these (ADR-058 rule 4). | Sheet 1 agent tile; sheet 5 |
| L4-02-022 | Included | Traces go to Grafana Tempo, logs to Grafana Loki and metrics to Grafana Mimir or Prometheus. Grafana is the one UI (ADR-058 rule 11). The agent forwards traces. Whether the same agent also carries logs and metrics is for the logs and metrics ADR; this document draws one agent as a proposal. | Sheet 1 Grafana group |
| L4-02-023 | Included | The agent and, when self-hosted, the Grafana stack are control-plane services and sit in the control plane beside the `CONTROL` account. They need no NATS credential to carry telemetry. | Sheet 2 control plane; sheet 5 |
| L4-02-024 | Included | The agent may read NATS server health from each server's HTTP monitoring port. It does not use a NATS account for this. | Sheet 2 monitoring line |
| L4-02-025 | Included | Tenants read no traces by default. Grafana is an operator tool (ADR-058 rule 10). | Sheet 1 operators group |
| L4-02-026 | Included | Hop H1, outside call: browser or public client to an edge service over HTTP or `api.*`. New root, client context kept as a link. | Sheet 2 hop table |
| L4-02-027 | Included | Hop H2, across accounts: a tenant service calls a shared service through a service import. Context is carried in the header. The callee span records the receiving account and the originating tenant. | Sheet 2 hop table |
| L4-02-028 | Included | Hop H3, inside one cluster and one account: context carried in the header; caller and callee spans may show different `nats.server.name`. | Sheet 2 hop table |
| L4-02-029 | Included | Hop H4, across a leaf link: a site service on a leaf node calls into the cell. Context carried; `nats.server.name` is the leaf server on the site span; `nats.jetstream.domain` is the addressed domain, not the leaf's own. | Sheet 2 hop table |
| L4-02-030 | Included | Hop H5, through a stream: publish into JetStream, consume later. Context stored with the message; the consumer span is a child in the same trace. | Sheet 2 hop table |
| L4-02-031 | Included | Hop H6, across clusters and regions: a call crosses a gateway from the `za` cell to the `au` cell. Context carried; caller and callee spans differ in `cloud.region` and `nats.cluster.name`. Each side's spans go to its own cell's agent. | Sheet 2 hop table |
| L4-02-032 | Included | Hop H7, sovereign cell: the Botswana cell has no NATS gateway, so no trace crosses a cell boundary on a NATS gateway path. Its spans go to its own agent. Whether any telemetry may leave the jurisdiction by another path is open (`L4-02-O02`). | Sheet 2 hop table |
| L4-02-033 | Included | Sequence of one outside call, drawn with numbered interactions: client, edge service, shared module, NATS, platform service, JetStream, consumer, agent, Tempo, Grafana, operator. | Sheet 4 |
| L4-02-034 | Included | Two modes, one code path. Local mode runs services, agent and the Grafana stack in containers on one machine, with no cloud account, no internet service and no external credentials, telemetry on. Live mode runs the production agent with its disk queue and sender authentication (ADR-058 rule 12). | Sheet 5 |
| L4-02-035 | Included | Only configuration changes between modes. The module reads the standard variables `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL`, `OTEL_SERVICE_NAME` and `OTEL_RESOURCE_ATTRIBUTES`, plus a platform mode setting. | Sheet 5 configuration table |
| L4-02-036 | Included | In live mode a missing agent endpoint is a startup error. There is never a silent fallback to a local address. | Sheet 5 |
| L4-02-037 | Included | No credential, key, endpoint address or account identifier appears on any sheet. | All sheets |
| L4-02-038 | Derived | Rules for logs and metrics, including whether resource attributes go on logs and metrics, and keeping unbounded values out of metric labels. | Logs and metrics ADR (ADR-058, not decided) |
| L4-02-039 | Derived | Sizing of agent queues, store ingest limits and retention per cell. | `LB-V3-L3-12` and a later L4 |
| L4-02-040 | Derived | The broader observability concern: health, alerting, dashboards and service-level objectives. | `LB-V3-L3-10` |
| L4-02-041 | Open | The telemetry-over-NATS clash with `LB-V3-L3-05`. | See `L4-02-O01` |
| L4-02-042 | Open | Whether masked telemetry may leave a jurisdiction. | See `L4-02-O02` |
| L4-02-043 | Open | How live mode hosts Grafana, which agent product is used, the sampling rate and sender authentication. | See `L4-02-O03` to `L4-02-O06` |
| L4-02-044 | Included | A site service on a depot leaf node sends spans over OTLP, never over its leaf link. Which agent it sends to is open (`L4-02-O10`). | Sheet 2 depot tile |

## Open confirmations

| ID | Open item | Owner |
|---|---|---|
| L4-02-O01 | `LB-V3-L3-05` still draws telemetry on NATS: the tenant export up (`L3-05-012`), observability state (`L3-05-021`) and the `obs.` subjects (`L3-05-023`). ADR-058 rule 3 forbids telemetry on the NATS account model. This document follows ADR-058. `L3-05-O07` records the same clash from the other side. Settle it by revising `LB-V3-L3-05`. | Architecture |
| L4-02-O02 | Whether masked telemetry may leave a jurisdiction (`L3-08-O08`), and so whether one central Grafana may serve every cell or each cell needs its own stores. | Legal and architecture |
| L4-02-O03 | How live mode hosts Grafana: a managed Grafana service or self-hosted in the control plane. Self-hosted Tempo is AGPLv3. Tempo 3.x in microservices mode needs object storage and a queue; this is not yet checked against the chosen version. | Architecture |
| L4-02-O04 | The agent product: Grafana Alloy or the OpenTelemetry Collector. Either must meet `L4-02-021`. | Architecture |
| L4-02-O05 | The production sampling rate and where it is applied. | Architecture and operations |
| L4-02-O06 | Sender authentication at the agent. | Security |
| L4-02-O07 | Confirm the request-info header name and the import setting that makes the server write it, against the chosen NATS server version, before `L4-02-015` is built. | Architecture |
| L4-02-O08 | Confirm the proposed tenant attribute names `linebooker.account.receiving` and `linebooker.tenant.origin`. | Architecture |
| L4-02-O09 | Which existing code becomes the shared Go module. | Architecture |
| L4-02-O10 | Where a depot site service sends its spans: the agent of the cell its leaf node joins, or an agent at the depot. Depends on the depot network and on `L4-02-O02`. | Architecture |

## Notes on evidence

- Nothing on any sheet is built in V3.
- The rules come from ADR-058. Chronicle, the V3 codebase, already sends OTLP
  to a Grafana agent and Grafana stores. A review on 2026-10-16 found it does
  not yet meet rules 2, 4, 5, 7 and 12. Those are observations, not results.
- Chronicle's control-plane NATS account is named `LB`. In V3 that account is
  `CONTROL`.
- Standard attribute names follow the OpenTelemetry semantic conventions.
  `nats.*` and `linebooker.*` names are platform names.

## Change log

- 2026-10-19 - Review pass. JetStream domain names now follow the open cluster
  shape (`L4-01-O01`) instead of fixed per-cell names (`L4-02-011`, sheet 2).
  Rule 3 binds spans only; logs and metrics on the same path, and one agent for
  all three signals, stay proposals until the logs and metrics ADR (`L4-02-019`,
  `L4-02-022`). `L4-02-015` names `Nats-Request-Info` and the account-to-tenant
  mapping. H7 says "no NATS gateway path" (`L4-02-032`). Added `L4-02-044` and
  `L4-02-O10` for the depot span route. `L4-02-O03` restores the Tempo 3.x
  qualifier. Sheet 4 step 9 now points JetStream to consumer. Sheet 3 uses
  blue for the module, so purple keeps one meaning: control plane.
- 2026-10-19 - Register created as the second L4 document, parented under
  `LB-V3-L3-10 Observability` (still `PLANNED`) on the user's instruction. It is
  the detailed design of ADR-058. Records the shared module and its six parts,
  the NATS header carrier, the edge rule, the span attribute contract, seven
  hop scenarios across accounts, clusters, leaf links, streams and regions, the
  agent and Grafana in the control plane beside the `CONTROL` account, and two
  deployment modes. Records the clash with `LB-V3-L3-05` as `L4-02-O01`.
  Published as a directional document as at 2026-10-19.
