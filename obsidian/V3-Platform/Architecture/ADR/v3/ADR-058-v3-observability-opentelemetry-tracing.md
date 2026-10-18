---
adr: 58
title: Trace Every Service with the OpenTelemetry SDK, Not Over NATS
status: Proposed
date: 2026-10-18
scope: v3
context: observability
decision: Every V3 service traces with the standard OpenTelemetry SDK through one shared module per language, and spans leave over OTLP to a disk-buffered agent, never over NATS accounts. An outside call always starts a new trace, and a span's tenant comes only from trusted context.
why: A private tracer is code the platform must own, and that code can introduce defects. The standard SDK already does creation, propagation, sampling and export, so only the NATS and tenant rules need platform code.
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

There are two ways to build this:

1. **A private tracer over NATS.** The platform writes its own span envelope, publishes spans on NATS subjects, carries them across NATS accounts to a JetStream stream, and bridges them to a trace store. This design has known failure modes. Spans from tenant accounts can arrive on a different subject after an account import, and a bridge that reads only the platform subject loses them. A hand-written outbound span can carry the callee's service name instead of the caller's. Both faults sit in code the platform would own.
2. **The standard OpenTelemetry SDK.** One thin shared module adds the NATS glue. Spans go over OTLP to a collector agent with a disk queue, and a trace store sits behind it. The SDK owns span creation, propagation, sampling and export.

Chronicle, the V3 codebase, already uses the OpenTelemetry SDK through `services/lib/logger` and Grafana Alloy. A code review on 2026-10-16 found that its traces do not join across services, because it never injects trace context into outbound messages.

## Decision

1. **Standard SDK, standard context.** Every V3 service traces with the OpenTelemetry SDK for its language and W3C Trace Context. No in-house tracer. No span envelope on NATS. Adapters extract and inject trace context. Domain code may receive the language's own context type, but never depends on a tracing library.
2. **One shared module per language holds the platform glue.** It owns: the NATS header carrier (inject and extract on every NATS message), the edge rule (rule 5), the tenant attribute names and rules (rule 6), and body capture (rule 7). Services call it. They do not copy it.
3. **Telemetry does not travel on the NATS account model.** Spans leave each service over OTLP to a collector agent. Tracing never depends on tenant exports, imports or provisioning.
4. **The agent buffers on disk, and tests prove it.** Its queue survives an agent restart and is sized for a stated outage of the trace store. An in-memory queue alone is not enough. Configuration is not evidence. On the chosen deployment, tests must show that the queue survives a restart and a hard kill, holds the stated volume, and counts its loss when full, and that a full queue drains into the trace store with every span stored.
5. **A call from outside the platform starts a new trace.** On an inbound browser, public HTTP or `api.*` call, the server span is a new root and is sampled by the platform. An incoming `traceparent` is kept only as a span link. A client cannot join a platform trace or switch sampling off. Between platform services, normal parent/child propagation applies.
6. **The tenant on a span comes from trusted context only.** The receiving service sets two separate attributes. The receiving account comes from the connection the message arrived on. The originating tenant comes only from server-written routing data, else it stays empty and shows as unattributed. A shared platform connection serves many tenants, so it never names the originating tenant. Neither attribute comes from a client header, the body or a requester field. Both are metadata and a query filter. Neither is a security boundary.
7. **Bodies are off by default.** Where a service turns capture on, it redacts, then truncates, in-process, before export.
8. **Export never blocks a business call.** Asynchronous batch export, a bounded in-process queue, and explicit export and shutdown timeouts.
9. **Tenants read no traces by default.** Trace data is an operator tool. Any tenant-facing trace view is a separate decision.

## Not decided here

Each of these is a separate V3 decision:

- **The trace store** (Tempo or Jaeger v2 + OpenSearch). Tempo 3.x in microservices mode, the production layout, needs object storage and a Kafka-compatible queue. Tempo is AGPLv3. Tempo's default ingest limits can drop spans when a full agent queue drains, so limits must be sized with the queue.
- **The agent product** (Grafana Alloy or the OpenTelemetry Collector). Rule 4 applies to either.
- **A trace query API for applications.** V3 needs one only when an application needs it. If one is built, its contract is a platform schema, so the trace store can change behind it.
- **Production sampling rate.**
- **Sender authentication at the agent,** and publish limits on platform credentials.
- **Tracing through Temporal.**

## Options Considered

### A private tracer over NATS — rejected

Keeps telemetry inside the NATS security model, and JetStream gives replay. But the platform owns a tracer, its export bridge and its mapping to OTLP, and the failure modes in Context sit in that code. It also ties tracing to tenant provisioning.

### Standard SDK, each service wires its own — rejected

No shared code to maintain. But the NATS carrier, the edge rule and the tenant rule are exactly what each service would get differently. Per-service tracing code risks drifting apart.

### Standard SDK through one shared module — chosen

The platform owns only what is specific to NATS and tenants.

## Trade-off Analysis

The choice gives up NATS-native telemetry for standard tooling. A tracer over NATS keeps spans inside the account model and lets JetStream replay them. The SDK path gives up both. In return the platform stops owning span creation, export and the OTLP mapping, which a private tracer adds as platform-owned failure points. The shared module keeps the part the SDK cannot know (NATS headers, the edge rule, the tenant rule) in one place, at the cost of one module per language to maintain.

The disk queue at the agent replaces JetStream as the durability point. It protects spans while the trace store is down. It does not protect spans before they reach the agent: that window stays the SDK's in-memory queue.

## Consequences

### Positive

- Traces join across NATS and HTTP with standard tooling.
- The trace store and agent can change without service code changes.
- Client-supplied trace context cannot override the platform's sampling policy.
- Tenant attribution ignores untrusted client input.

### Negative

- Replay of past spans to a new backend is given up.
- Telemetry needs its own network path and its own agent to run and secure.
- Browser and server ancestry are separate traces, joined only by a link.

## Chronicle

This ADR is the rule Chronicle is measured against. Adopting it in Chronicle is separate work, and nothing here claims Chronicle meets it. The code review (2026-10-16) found these gaps, recorded as observations, not results:

- Rule 2: Chronicle has no NATS header carrier in use, so its traces do not join.
- Rule 4: Chronicle's Alloy queue is in memory only.
- Rule 5: Chronicle has no edge rule.
- Its tenant is set per deployment, which is trusted context under rule 6.

Which code becomes the shared module (rule 2) is a separate choice, made by comparing candidate modules, Chronicle's `services/lib/logger` among them.

## Action Items

- [ ] Chronicle: adopt rules 1–8, starting with the NATS header carrier.
- [ ] Choose the shared module (rule 2).
- [ ] Decide the V3 trace store.
- [ ] Decide the V3 agent (Alloy or Collector), including its disk queue.
- [ ] Write LB-V3-L3-10 Observability, citing this ADR.
