# NATS Multi-Region — Requirements & Plan

**Status:** DRAFT — awaiting requirements input
**Date:** 2026-07-23
**Depends on:** Demo 01 — Dictionary POC (Shapes A/B/C, refdata-service)

---

## 1. Problem Statement

*(To be filled in with the user's follow-up instructions.)*

The lab currently evaluates NATS.io patterns (JetStream, KV, CQRS projections) within a single
region/single NATS cluster. The V3 greenfield logistics platform will need to operate across
multiple regions — for latency, data residency, and availability reasons. This plan will define
a new demo (or an extension of an existing one) that evaluates NATS's multi-region capabilities
and the correct responsibility split for cross-region data flow.

Open questions to resolve before design work starts:

1. **Why multi-region?** Latency (serve reads near the user), data residency/regulatory
   (e.g. EU data must stay in EU), disaster recovery/availability, or some combination?
2. **Topology candidates** — which NATS multi-region mechanism(s) are in scope for evaluation?
   - **Superclusters / gateways** — full mesh of independent clusters, cross-cluster subject
     interest propagation (no data replication by default).
   - **JetStream leaf nodes** — regional leaf nodes hanging off a hub cluster.
   - **Stream/KV replication (mirrors and sources)** — async replication of JetStream streams
     and KV buckets across clusters/regions.
   - **Multi-region clustering** (single cluster stretched across regions with RAFT) — usually
     ruled out for latency reasons but worth naming as a rejected option.
3. **Scope: new demo vs. extend existing?** Does this build on `01-dictionary` (context-scoped
   refdata is naturally regional — `emea-acme`, `apac-orient`, etc. from
   `Refdata-Versioning-Tenancy-Design.md`) or is this `02-multi-region`, a standalone demo?
4. **Which data needs to cross regions vs. stay local?**
   - Reference/dictionary data (Shape A/B read models) — read-mostly, good replication candidate.
   - Ship/Container event streams (Shape C, event-sourced) — write locality and ordering matter
     more; replication semantics are harder.
5. **Consistency requirements** — for each data class in scope, is eventual consistency
   acceptable, or does anything require a single source of truth with synchronous cross-region
   confirmation?
6. **Failure modes to demonstrate** — region isolation/partition, failover/failback, split-brain
   avoidance, and what "degraded but available" looks like per region.

---

## 2. Requirements

*(Pending — to be captured from user follow-up. Structure reserved below; do not invent
business rules — see CLAUDE.md's "ask for business rules first" workflow.)*

### 2.1 Functional Requirements

- TBD

### 2.2 Non-Functional Requirements

- TBD (latency targets per region, RPO/RTO for failover, data residency constraints)

### 2.3 Out of Scope

- TBD

---

## 3. Candidate Architecture Sketch

*(Placeholder — to be replaced once requirements are confirmed. Kept here only to frame the
kind of decision this plan needs to make, mirroring the responsibility-split question in
`CLAUDE.md`: what belongs in JetStream vs. KV vs. Postgres vs. CQRS projections, now with a
region dimension added.)*

```
Region: EU                          Region: APAC
┌─────────────────────────┐         ┌─────────────────────────┐
│ NATS cluster (leaf/hub?) │◄───?───►│ NATS cluster (leaf/hub?) │
│  JetStream: local streams │         │  JetStream: local streams │
│  KV: local buckets        │         │  KV: local buckets        │
│  Postgres: local           │         │  Postgres: local           │
└─────────────────────────┘         └─────────────────────────┘
```

Decisions to make once requirements land:

- Gateway mesh vs. leaf nodes vs. stream/KV mirrors (or a combination — e.g. leaf nodes for
  regional autonomy, KV mirrors for cross-region reference-data replication).
- Which streams/buckets are mirrored (source of truth stays regional) vs. sourced (aggregated
  centrally) vs. purely local (never leaves the region).
- How context-scoped KV keys (`{entityType}.{id}` under `dict-a-{context}`, etc.) map onto
  region boundaries — is `context` already the right partition key, or does region need to be
  a separate dimension?

---

## 4. Plan / Phases

*(To be broken into phases once scope is confirmed — following the existing plan format in
`Dictionary-POC-Plan.md`: phase → business rules confirmed → Ginkgo specs written → implementation
→ docs updated → tests green.)*

- [ ] Phase 0 — Confirm requirements (topology driver, scope, consistency needs) with user
- [ ] Phase 1 — TBD
- [ ] Phase 2 — TBD

---

## 5. Open Questions

- See numbered questions in §1. Awaiting user follow-up before proceeding to design.
