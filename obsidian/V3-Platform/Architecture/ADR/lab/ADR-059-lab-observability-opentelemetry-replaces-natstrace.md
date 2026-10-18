---
adr: 59
title: OpenTelemetry Replaces natstrace; Spans Go to a Collector and Tempo, Not Over NATS
status: Accepted
date: 2026-10-16
scope: lab
context: observability
decision: The lab deletes natstrace and the NATS trace path; six services trace through shared/otelnats and export OTLP/HTTP to an OTel Collector with a disk queue, which writes to Tempo. Operators read traces through observability-service over NATS api.* or in Grafana; tenants read nothing.
why: The Phase 28 bridge had two defects (tenant spans never exported; outbound spans named the callee), and fixing them meant keeping a private tracer. The SDK made the lab's own code small enough to fit one package.
related: [58, 55]
applies: [58]
---

# ADR-059: OpenTelemetry Replaces natstrace; Spans Go to a Collector and Tempo, Not Over NATS

**Status:** **Accepted 2026-10-16** — built as Phase 54 (54a–54d, `04d73e27`); switch made and checked on the au-1 cell 2026-10-16. za-1 not checked.
**Date:** 2026-10-16
**Deciders:** Jeremy (repo owner)
**Governed by:** [ADR-058](../v3/ADR-058-v3-observability-opentelemetry-tracing.md) — the Proposed Linebooker V3 rule. This ADR is that rule applied in the lab, with the evidence for each part.
**Related:** [ADR-055](ADR-055-lab-platform-compose-split-cell-and-global.md) (its Phase 54 note: the `cell/compose.telemetry.yaml` tier); [ARCHITECTURE-COMMUNICATIONS.md](../../Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md) § 6 (the Phase 28 deferral this reverses); [ARCHITECTURE-OBSERVABILITY.md](../../Dictionary-POC/ARCHITECTURE-OBSERVABILITY.md); `BUSINESS_RULES-SHIPPING.md` BR-064–BR-077; `.claude/plans/Main-POC-Plan.md` Phase 54 "Decision ledger"; as-built figure `images/otel-migration-before-after.png`

## Context

Phase 28 built the lab's own tracer: `shared/natstrace` wrote spans to `obs.trace.*`; tenant accounts exported them to PLATFORM (`monitor.{tenant}.trace.>`); a `TRACES` stream fed a KV projection (`trace-request-reply`) for the Admin UI and `otlp-bridge` for Jaeger. Phase 28 recorded a deferred decision: adopt the real SDK "if this POC graduates".

Two defects were verified on 2026-10-14:

- **Tenant spans never reached Jaeger.** `otlp-bridge` read only `obs.trace.>`; tenant spans land on `monitor.*.trace.>`.
- **Outbound spans carried the callee's service name**, so a caller's client span showed under the callee.

## Decision

Phase 54 takes up the Phase 28 deferral and reverses it. Seventeen design decisions are in the plan; each has a ledger row with its status. In short:

- **SDK in every traced service** through one thin package, `shared/otelnats`: shipping, refdata, mfe-registry, pricing, organizations and accounts. observability-service does not export.
- **Pipeline:** SDK → OTLP/HTTP → OTel Collector (disk queue, `file_storage`) → Tempo (monolithic, local disk). Only Grafana is published on the host, with login on.
- **No span crosses an account on NATS.** The tenant `obs.trace.>` export and PLATFORM import are deleted.
- **Edge rule:** every `api.*` call starts a new root; a browser `traceparent` becomes a link (BR-071). Sampler `ParentBased(AlwaysSample())`, 100%.
- **Context through the adapters.** Adapters extract inbound context and inject outbound context into NATS headers. The domain layer may take a `context.Context` but never imports `otel` (decision 6).
- **Two tenant attributes, from trusted context only** (BR-069). `lab.tenant.receiving_account` comes from the connection. `lab.tenant.originating` comes only from server-written routing data, else it is `unattributed`. Both are metadata and a query filter, never authorization. A trusted PLATFORM service can still publish a tenant's routing subject; that is accepted in the lab. Operators read all tenants, tenants read nothing (BR-067).
- **Bodies as span events**, redact then truncate, opt-in by `OTEL_CAPTURE_BODIES` (off by default).
- **Queries:** `api._platform.observability.trace.{search|get|services}.v1` on observability-service, in the lab's own versioned schema (BR-076). Search results are capped, and a capped result is a sample, never a total (BR-074). The Admin UI polls every 5 s while the panel is shown.
- **Service discovery is independent of tracing** (decision 17, BR-072). The Services panel uses `service.list.v1` (NATS micro PING, INFO, STATS). A reply means responding, not ready. A missing reply means not seen in the window, not dead. Partial failures are shown, never hidden as an empty list.
- **One cutover, no history migrated.** Then 54d deleted `natstrace`, `obs.trace.*`, `TRACES`, `trace-request-reply`, `tracestore`, `otlp-bridge`, Jaeger, `useTraceFeed.js`, `cmd/seed-traces` and `GET /api/nats/services`.

Kept: the `obs.pubsub.*` wire tap (BR-045–BR-048), now published by `otelnats.Observer`.

## Options Considered

- **Fix the bridge in place — rejected.** Keeps a private tracer, its envelope and its OTLP mapping.
- **Move services one at a time — rejected** (decision 16 (b)). Traces through refdata would be split across two stores for weeks.
- **Dual-write both stores — rejected.** More code to delete later, in a lab.
- **SDK through one package, one cutover — chosen.**

## Trade-off Analysis

The lab traded replay and live push for a smaller, standard tracer. JetStream gave an hour of replay and the KV watch gave the Admin UI live push. The SDK path gives up both, and Pulse with them until Phase 55. In return the lab no longer owns a span envelope, a bridge or an OTLP mapping, and tracing no longer depends on tenant exports.

Durability moved from JetStream to the Collector's disk queue. That protects spans while Tempo is down. It does not close the window before the Collector: with the Collector unreachable, only the SDK's bounded in-memory queue holds spans, and export retries are off in the SDK.

One cutover was chosen over a gradual move. The cost is that trace history before the switch is gone. The gain is that no trace is ever split across two stores.

## Evidence

| Claim | Status | Where |
|---|---|---|
| Traces join across services (SERVER → CLIENT → SERVER) | Proved | BR-064 specs |
| Export never blocks a call (Collector stopped or hung) | Proved | 54a |
| Disk queue survives restart and hard kill | Proved | 54a, BR-068 |
| Queue holds peak volume; loss is counted when full | Proved by volume, not by a 2-hour elapsed outage | 54b-7b, BR-068 |
| A full queue reaches Tempo intact | **Not proved** — Tempo's default limits dropped most spans on drain | 54b-7b |
| Tenant cannot reach the trace gateway | Proved | BR-067 |
| za-1 cell | **Not checked** | Port 3000 taken by the Chronicle `lgtm` container |

## Consequences

### Positive

- Tracing no longer depends on tenant provisioning.
- Swapping Tempo is one query adapter plus Collector config (lab schema, decision 9).

### Negative

- Replay to a new backend is gone. Trace history before the switch is gone.
- The Traces panel polls instead of being pushed (push is proposed Phase 56).
- Pulse is unavailable until Phase 55 (BR-077).
- The Temporal hop is not traced (accepted, BR-064).
- `adminConnectInfo` stays ungated; operator login is candidate Phase 109.
- Spans can be lost before the Collector: in flight during a Collector restart, or past the SDK's bounded queue while the Collector is unreachable. Measured, not closed.
- A new trace is searchable about 10–15 s after the call on a local cell (Tempo `query_end_cutoff` set to 5 s). Capped search cannot replace Pulse counts.

## Not a Chronicle claim

This ADR records what the lab built and measured. It does not claim anything about Chronicle. Demo 03 builds its own OTel module for now; the target is one shared module for Chronicle, picked by demo 03's three-way comparison (ADR-058, rule 2).

## Action Items

- [ ] Check za-1.
- [ ] Proposed, not decided: raise Tempo's ingest limits and re-run the full-queue drain. The lab keeps Tempo's defaults by decision (2026-10-16).
- [ ] Open from 54b: one process with two same-name micro services lists as two instances.
- [ ] Open from 54b: `type.list` throughput looks serial. The cause is not proved.
- [ ] LB-V3-L3-10 Observability.
