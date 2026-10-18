---
adr: 58
title: Trace Every Service with the OpenTelemetry SDK, Not Over NATS
status: Proposed
date: 2026-10-18
scope: v3
context: observability
decision: Every V3 service traces with the standard OpenTelemetry SDK through one shared module per language, and spans leave over OTLP to a disk-buffered agent, never over NATS accounts. An outside call always starts a new trace, and a span's tenant comes only from trusted context.
why: The lab's private tracer had two defects in code the platform would have to own. The standard SDK already does creation, propagation, sampling and export, so only the NATS and tenant rules need platform code.
related: [59, 55]
applied_by: [59]
---

# ADR-058: Trace Every Service with the OpenTelemetry SDK, Not Over NATS

**Status:** **Proposed 2026-10-18**
**Decision Type:** Platform / Observability
**Scope:** Proposed Linebooker V3 Architecture (platform principle, not a lab implementation)
**Date:** 2026-10-18
**Deciders:** Jeremy (repo owner)
**Related:** [ADR-059](../lab/ADR-059-lab-observability-opentelemetry-replaces-natstrace.md) applies this principle in the tech lab (Phase 54) and records what was measured there; [ARCHITECTURE-COMMUNICATIONS.md](../../Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md) § 6; [ARCHITECTURE-OBSERVABILITY.md](../../Dictionary-POC/ARCHITECTURE-OBSERVABILITY.md); `.claude/plans/Main-POC-Plan.md` Phase 54 "Decision ledger"

## Context

V3 services talk over NATS (`api.*`, `rpc.*`, `evt.*`, `notify.*`) and HTTP. A trace must follow one request across all of them.

The tech lab tried two designs:

1. **A private tracer over NATS** (lab Phase 28). A hand-written span envelope on `obs.trace.*`, carried across NATS accounts to a JetStream stream, then bridged to a trace store. It had two defects (verified 2026-10-14): tenant spans never reached the trace store, and an outbound span carried the callee's service name.
2. **The standard OpenTelemetry SDK** (lab Phase 54). One thin shared package for the NATS glue, OTLP to a Collector with a disk queue, and a trace store behind it. Built and checked on one lab cell. See ADR-059.

Chronicle, the V3 codebase, already uses the OpenTelemetry SDK through `services/lib/logger` and Grafana Alloy. A code comparison on 2026-10-16 found that its traces do not join across services, because it never injects trace context into outbound messages.

## Decision

1. **Standard SDK, standard context.** Every V3 service traces with the OpenTelemetry SDK for its language and W3C Trace Context. No in-house tracer. No span envelope on NATS.
2. **One shared module per language holds the platform glue.** It owns: the NATS header carrier (inject and extract on every NATS message), the edge rule (rule 5), the tenant attribute names and rules (rule 6), and body capture (rule 7). Services call it. They do not copy it.
3. **Telemetry does not travel on the NATS account model.** Spans leave each service over OTLP to a collector agent. Tracing never depends on tenant exports, imports or provisioning.
4. **The agent buffers on disk.** Its queue survives an agent restart and is sized for a stated outage of the trace store. An in-memory queue alone is not enough.
5. **A call from outside the platform starts a new trace.** On an inbound browser, public HTTP or `api.*` call, the server span is a new root and is sampled by the platform. An incoming `traceparent` is kept only as a span link. A client cannot join a platform trace or switch sampling off. Between platform services, normal parent/child propagation applies.
6. **The tenant on a span comes from trusted context only.** The receiving service sets it from the authenticated connection or server-written routing data. Never from a client header, the body or a requester field. The attribute is metadata and a query filter. It is never a security boundary.
7. **Bodies are off by default.** Where a service turns capture on, it redacts, then truncates, in-process, before export.
8. **Export never blocks a business call.** Asynchronous batch export, a bounded in-process queue, and explicit export and shutdown timeouts.
9. **Tenants read no traces by default.** Trace data is an operator tool. Any tenant-facing trace view is a separate decision.

## Not decided here

Each of these is a separate V3 decision:

- **The trace store** (Tempo or Jaeger v2 + OpenSearch). Production Tempo needs object storage and a Kafka-compatible queue, and is AGPLv3.
- **The agent product** (Grafana Alloy or the OpenTelemetry Collector). Rule 4 applies to either.
- **A trace query API for applications.** The lab built one; V3 needs one only when an application needs it.
- **Production sampling rate.** The lab samples 100%.
- **Sender authentication at the agent,** and publish limits on platform credentials.
- **Tracing through Temporal.**

## Options Considered

### A private tracer over NATS — rejected

Keeps telemetry inside the NATS security model and JetStream gives replay. But the platform owns a tracer, its export bridge and its mapping to OTLP. The lab found two defects in that code. It also ties tracing to tenant provisioning.

### Standard SDK, each service wires its own — rejected

No shared code to maintain. But the NATS carrier, the edge rule and the tenant rule are exactly what each service would get differently. The lab's tracer was five copies before it was merged into one package.

### Standard SDK through one shared module — chosen

The platform owns only what is specific to NATS and tenants.

## Trade-off Analysis

The choice gives up NATS-native telemetry for standard tooling. A tracer over NATS keeps spans inside the account model and lets JetStream replay them. The SDK path gives up both. In return the platform stops owning span creation, export and the OTLP mapping, which is where the lab's defects were. The shared module keeps the part the SDK cannot know (NATS headers, the edge rule, the tenant rule) in one place, at the cost of one module per language to maintain.

The disk queue at the agent replaces JetStream as the durability point. It protects spans while the trace store is down. It does not protect spans before they reach the agent: that window stays the SDK's in-memory queue.

## Consequences

### Positive

- Traces join across NATS and HTTP with standard tooling.
- The trace store and agent can change without service code changes.
- A client cannot hide its calls from tracing or forge a tenant on a span.

### Negative

- Replay of past spans to a new backend is given up.
- Telemetry needs its own network path and its own agent to run and secure.
- Browser and server ancestry are separate traces, joined only by a link.

## Chronicle

This ADR is the rule Chronicle is measured against. Adopting it in Chronicle is separate work, and nothing here claims Chronicle meets it. The lab comparison (2026-10-16) found these gaps, recorded as observations, not results:

- Rule 2: Chronicle has no NATS header carrier in use, so its traces do not join.
- Rule 4: Chronicle's Alloy queue is in memory only.
- Rule 5: Chronicle has no edge rule.
- Its tenant is set per deployment, which is trusted context under rule 6.

Which code becomes the shared module (rule 2) is chosen by demo 03's three-way comparison of `shared/otelnats`, demo 03's own module and Chronicle's `services/lib/logger`.

## Action Items

- [ ] Chronicle: adopt rules 1–8, starting with the NATS header carrier.
- [ ] Decide the V3 trace store.
- [ ] Decide the V3 agent (Alloy or Collector), including its disk queue.
- [ ] Write LB-V3-L3-10 Observability, citing this ADR.
