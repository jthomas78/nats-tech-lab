---
adr: 58
title: Trace Every Service with the OpenTelemetry SDK, Not Over NATS
status: Proposed
date: 2026-10-18
scope: v3
context: observability
decision: Every V3 service traces with the standard OpenTelemetry SDK through one shared module per programming language, and spans leave over OTLP to a disk-buffered agent, never over NATS accounts. An outside call always starts a new trace, and every span carries its tenant from trusted context and its location (region, cluster, server).
why: A private tracer is code the platform must own, and that code can introduce defects. The standard SDK already does creation, propagation, sampling and export, so only the NATS, tenant and location rules need platform code.
related: []
---

# ADR-058: Trace Every Service with the OpenTelemetry SDK, Not Over NATS

**Status:** **Proposed 2026-10-18**
**Decision Type:** Platform / Observability
**Scope:** Proposed Linebooker V3 Architecture (platform principle)
**Date:** 2026-10-18
**Deciders:** Jeremy (repo owner)
**Related:** none yet. LB-V3-L3-10 Observability will cite this ADR.

## Context

V3 services talk over NATS (`api.*`, `rpc.*`, `evt.*`, `notify.*`) and HTTP. A trace must follow one request across all of them.

### Terms used in this ADR

- **Span** — one timed step of work, such as "handle this request" or "call that service". A **trace** is the tree of spans for one request.
- **Agent** — a separate program that runs near the services. Each service sends its telemetry to the agent. The agent queues it, batches it, adds labels, and forwards it to the stores. Grafana Alloy and the OpenTelemetry Collector are two agent products. The services never talk to a store directly.
- **Trace store** — the database that keeps traces and answers searches, for example Tempo or Jaeger. People read it through a UI such as Grafana.
- **Body** — the payload of a message or an HTTP request or reply: the data a caller sends and the data it gets back, for example the JSON of a load booking. A body can hold personal and commercial data.
- **OTLP** — the OpenTelemetry wire protocol that services, agents and stores use to send telemetry to each other.

### Two ways to trace

1. **A private tracer over NATS.** The platform writes its own span envelope, publishes spans on NATS subjects, carries them across NATS accounts to a JetStream stream, and bridges them to a trace store. This design has known failure modes. Spans from tenant accounts can arrive on a different subject after an account import, and a bridge that reads only the platform subject loses them. A hand-written outbound span can carry the callee's service name instead of the caller's. Both faults sit in code the platform would own.
2. **The standard OpenTelemetry SDK.** One thin shared module adds the NATS glue. Spans go over OTLP to an agent with a disk queue, and a trace store sits behind it. The SDK owns span creation, propagation, sampling and export.

### Chronicle today

Chronicle, the V3 codebase, already uses the OpenTelemetry SDK. One `Setup()` in `services/lib/logger` builds the trace, log and metric providers. Services send OTLP to Grafana Alloy (two nodes behind a private load balancer). Alloy also scrapes NATS server metrics, and forwards traces, logs and metrics to Grafana Cloud (Tempo, Loki, Mimir). A code review on 2026-10-16 found that Chronicle's traces do not join across services, because it never injects trace context into outbound messages.

## Decision

1. **Standard SDK, standard context.** Every V3 service traces with the OpenTelemetry SDK for its language and W3C Trace Context. No in-house tracer. No span envelope on NATS. Adapters extract and inject trace context. Domain code may receive the language's own context type, but never depends on a tracing library.
2. **One shared module per programming language holds the platform glue.** "Per language" means one module for each programming language V3 services are written in. Today V3 services are Go, so this is one Go module. A service in another language gets its own module with the same rules. The module owns: the NATS header carrier (inject and extract on every NATS message), the edge rule (rule 5), the tenant attributes (rule 6), the location attributes (rule 7) and body capture (rule 8). Services call it. They do not copy it.
3. **Telemetry does not travel on the NATS account model.** Spans leave each service over OTLP to an agent, on their own network path. Tracing never depends on tenant exports, imports or provisioning. The benefits:
   - **A new tenant is traced from its first call.** No per-tenant export, import or stream has to exist first, and a missing one cannot silently lose spans.
   - **Telemetry stays up when NATS has a problem.** The spans that show a NATS fault do not have to travel over the faulty NATS.
   - **Telemetry does not compete with business traffic** for NATS connections, subjects, stream storage or account limits.
   - **Standard tools work unchanged.** Any OTLP agent or store can receive the spans, with no bridge to write or maintain.
4. **The agent buffers on disk, and tests prove it.** Its queue survives an agent restart and is sized for a stated outage of the trace store. An in-memory queue alone is not enough. Configuration is not evidence. On the chosen deployment, tests must show that the queue survives a restart and a hard kill, holds the stated volume, and counts its loss when full, and that a full queue drains into the trace store with every span stored.
5. **A call from outside the platform starts a new trace.** On an inbound browser, public HTTP or `api.*` call, the server span is a new root and is sampled by the platform. An incoming `traceparent` is kept only as a span link. A client cannot join a platform trace or switch sampling off. Between platform services, normal parent/child propagation applies.
6. **The tenant on a span comes from trusted context only.** The receiving service sets two separate attributes. The receiving account comes from the connection the message arrived on. The originating tenant comes only from server-written routing data, else it stays empty and shows as unattributed. A shared platform connection serves many tenants, so it never names the originating tenant. Neither attribute comes from a client header, the body or a requester field. Both are metadata and a query filter. Neither is a security boundary.
7. **Every span says where it ran.** Two kinds of location, set by the shared module:
   - **Where the service runs** — on the resource, so on every span, log and metric: `service.name`, `service.instance.id`, `deployment.environment.name`, `cloud.region`.
   - **Which NATS server the call used** — on every messaging span: `nats.server.name`, `nats.cluster.name` and `server.address`. The module reads these from the live connection when the span starts, not once at startup, because a client can reconnect to another cluster. After such a reconnect, `cloud.region` stays the same and `nats.cluster.name` changes.
   - **Which JetStream domain the call targets** — on JetStream spans only: `nats.jetstream.domain`. This is the domain the call is addressed to, taken from the JetStream options the service set for that call. It is not the domain of the server the client is connected to, because over a leaf link the two can differ. When the call names no domain, the attribute is left off.

   Location values come from the deployment and the live connection, never from a client. A value with no bound, such as a request ID, is never a metric label.
8. **Bodies are off by default.** A service records no message or request bodies on its spans unless it is turned on for that service. When it is on, the module redacts personal and secret fields, then truncates to a size limit, in the service, before export.
9. **Export never blocks a business call.** Sending telemetry must never slow down or stop the request it describes. In practice:
   - A finished span goes into a small, fixed-size queue in the service's memory. The request continues at once.
   - A background worker sends the queued spans to the agent in batches.
   - When the queue is full, new spans are dropped and counted. The service never waits for space.
   - Each send to the agent has a time limit. At shutdown the service waits a fixed, short time to send what is left, then exits.
10. **Tenants read no traces by default.** Trace data is an operator tool. Any tenant-facing trace view is a separate decision.

### Not decided here

Each of these is a separate V3 decision:

- **The trace store.** Chronicle uses Grafana Cloud (hosted Tempo) today. Other options are self-hosted Tempo or Jaeger v2 + OpenSearch. Self-hosted Tempo 3.x in microservices mode, the production layout, is documented to need object storage and a Kafka-compatible queue; check this against the chosen version and mode. Tempo is AGPLv3. Every store has ingest limits, and a full agent queue drains faster than normal traffic, so the limits must be sized with the queue (rule 4).
- **The agent product.** Grafana Alloy is already in Chronicle and already carries logs and metrics; the OpenTelemetry Collector is the other option. Rule 4 applies to either. Before Alloy is confirmed, check that the pinned Alloy version supports a disk-backed queue.
- **Logs and metrics.** This ADR decides traces only. Chronicle's `Setup()` already builds the log and metric providers too. Open question: should one shared module per language (rule 2) own setup for all three signals, and should one agent carry all three? Alignment looks likely, but it needs its own decision with its own rules for logs and metrics.
- **A trace query API for applications.** V3 needs one only when an application needs it. If one is built, its contract is a platform schema, so the trace store can change behind it.
- **Production sampling rate.**
- **Sender authentication at the agent,** and publish limits on platform credentials.

#### The agent and the trace store are independent

Services send OTLP to the agent only. The agent sends OTLP to the store. So:

- Changing the store is a change to the agent's configuration. No service changes.
- Changing the agent is a change to the endpoint the services send to. No store changes.
- Both choices can be made separately, with no change to service instrumentation.

Some couplings stay. The agent and the store must agree on transport, authentication and exporter settings. Replacing the agent means rebuilding its processing and its disk queue (rule 4). The store's ingest limits must be sized with the agent's queue.

## Options Considered

### A private tracer over NATS — rejected

Keeps telemetry inside the NATS security model, and JetStream gives replay. But the platform owns a tracer, its export bridge and its mapping to OTLP, and the failure modes in Context sit in that code. It also ties tracing to tenant provisioning.

### Standard SDK, each service wires its own — rejected

No shared code to maintain. But the NATS carrier, the edge rule, the tenant rule and the location rule are exactly what each service would get differently. Per-service tracing code risks drifting apart.

### Standard SDK through one shared module — chosen

The platform owns only what is specific to NATS, tenants and location.

## Trade-off Analysis

The choice gives up NATS-native telemetry for standard tooling. A tracer over NATS keeps spans inside the account model and lets JetStream replay them. The SDK path gives up both. In return the platform stops owning span creation, export and the OTLP mapping, which a private tracer adds as platform-owned failure points. The shared module keeps the part the SDK cannot know (NATS headers, the edge rule, the tenant and location rules) in one place, at the cost of one module per language to maintain.

The disk queue at the agent replaces JetStream as the durability point. It protects spans while the trace store is down. It does not protect spans before they reach the agent: that window stays the service's in-memory queue (rule 9).

## Consequences

### Positive

- Traces join across NATS and HTTP with standard tooling.
- The trace store and agent can change without service code changes.
- A trace shows the region, cluster and server of every hop, including after a failover.
- Client-supplied trace context cannot override the platform's sampling policy.
- Tenant attribution ignores untrusted client input.

### Negative

- Replay of past spans to a new backend is given up.
- Telemetry needs its own network path and its own agent to run and secure.
- Browser and server ancestry are separate traces, joined only by a link.

### Chronicle

This ADR is the rule Chronicle is measured against. Adopting it in Chronicle is separate work, and nothing here claims Chronicle meets it. The code review (2026-10-16) found these gaps, recorded as observations, not results:

- Rule 2: Chronicle has no NATS header carrier in use, so its traces do not join.
- Rule 4: Chronicle's Alloy queue is in memory only.
- Rule 5: Chronicle has no edge rule.
- Rule 7: Chronicle's spans carry no broker location attributes.
- Its tenant is set per deployment, which is trusted context under rule 6.

Which code becomes the shared module (rule 2) is a separate choice, made by comparing candidate modules, Chronicle's `services/lib/logger` among them.

## Action Items

- [ ] Chronicle: adopt rules 1–10, starting with the NATS header carrier.
- [ ] Choose the shared module (rule 2).
- [ ] Decide whether the shared module and the agent also cover logs and metrics.
- [ ] Decide the V3 trace store.
- [ ] Decide the V3 agent (Alloy or Collector), including its disk queue.
- [ ] Write LB-V3-L3-10 Observability, citing this ADR.
