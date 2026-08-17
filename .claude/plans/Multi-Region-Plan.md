# NATS Multi-Region — Requirements & Plan

**Status:** DRAFT — partially specified (see § 0, added Phase 16a 2026-07-31)
**Date:** 2026-07-23
**Depends on:** Demo 01 — Dictionary POC (Shapes A/B/C, refdata-service)

---

## 0. Decisions carried in from Phase 16a (2026-07-31)

The Phase 16 tenancy/subject-taxonomy formalization settled several of the open
questions in § 1 below. Authoritative source:
`obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-COMMUNICATIONS.md`
§ 2.3 and `.claude/plans/Main-POC-Plan.md` § Phase 16.

**Region is a deployment axis, not a data or subject axis.** The agreed model
is a strict three-level hierarchy:

```
region                        — separate stack deployment, its own NATS instance
  └── tenant                  — NATS account (the only tenancy boundary)
        └── company / group    — {context} subject token
              └── business unit — hyphenated into the same {context} token
```

Consequences for this plan:

1. **Region never appears in a subject token or a context value.** A regional
   deployment implies its own region, so there is nothing to encode. This
   *reverses* the assumption in § 1's open question 3 below, which treated
   `emea-acme` / `apac-orient` as "naturally regional" context values — those
   were pre-Phase-16 names and are being migrated (Phase 16d) to region-free
   company contexts (`acme`, `acme-northdiv`).
2. **Cross-region propagation is a JetStream concern, not a subject-naming
   one.** Because the same tenant is the *same account* in both regions and
   context values are region-free, a subject in region A is byte-identical to
   its counterpart in region B. Moving data between them is therefore stream
   replication, not translation.
3. **Both regional and global platform deployments are expected.**
   `refdata-service` in particular needs a platform-wide `_platform` corpus
   (standards-based reference data, shared templates) alongside per-company
   contexts, so "one deployment per region" is not universal — the split
   between region-local and globally-shared data is a first-class question for
   this plan, not an afterthought.

**Recommended mechanism (for evaluation, not yet decided):** JetStream
**Mirror** (or **Source**, where multiple origins must be merged or
transformed) rather than gateway-propagated core pub/sub. Core pub/sub across a
supercluster is best-effort — if the inter-region link is down when a message
is published, a subscriber on the far side never sees it and there is nothing
to replay. A mirror tracks its own sequence and resumes after a partition:

```
region A:  stream SHIPPING          (subjects evt.*.shipping.>)
region B:  stream SHIPPING_MIRROR   mirror: { name: "SHIPPING",
                                              filter_subject: "evt.acme-northdiv.shipping.>" }
```

Note what the filter is and isn't: `acme-northdiv` is a **`{context}` value —
a company/business unit**, not a tenant. The mirror is configured *inside* a
given tenant's account on both sides, so the tenant is already implied and
appears nowhere in the filter; a subject filter can only ever narrow within one
account. Filtering is therefore how you mirror *part of* a tenant's data (one
company or business unit) rather than the whole stream — it is not, and cannot
be, the mechanism that separates tenants.

Both clusters must share the same operator/account resolver so a tenant is the
*same* account on both sides. Gateways and mirrors do not merge accounts — the
account isolation guarantee holds across regions exactly as it does within one
cluster.

**Verifying propagation for one tenant/context** — four techniques, roughly
increasing in strength:

1. `nats stream info <mirror> --json` → `mirror.lag` / `mirror.active`. First
   alarm, but a whole-stream figure unless the mirror is subject-filtered.
2. Compare filtered message counts / last sequence between origin and mirror
   for the specific `evt.{context}.>` slice. Trivial when the mirror is
   filtered to exactly that slice.
3. `GetLastMsgBySubject` on a known subject on both sides; compare sequence and
   timestamp. Per-message proof.
4. **Canary/heartbeat** (recommended for an SLA rather than an ad-hoc check):
   publish a synthetic message on a dedicated heartbeat subject in region A,
   poll for it on region B's mirror with a timeout. Measures true end-to-end
   propagation latency for that specific slice, independent of other traffic.

**Not built:** nothing in this repo configures gateways, leaf nodes, or any
multi-cluster topology today — `nats/nats.conf` is a single server, single
region. Everything in this section is forward design.

Still open from § 1: the *why* (latency vs. residency vs. DR), which topology
to actually evaluate, new-demo vs. extend-01, per-data-class consistency
requirements, and which failure modes to demonstrate.

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
   - Reference/dictionary data (KV read models) — read-mostly, good replication candidate.
   - Ship/Container event streams (JetStream-backed) — write locality and ordering matter
     more; replication semantics are harder. (Phase 31 retired the demo's KV-as-read-model and
     event-sourced-reconstruction shapes in favor of a single Postgres-plus-KV-cache shape — see
     `obsidian/POC-Dictionaries/` — but the underlying question here, read-mostly vs.
     write-locality-sensitive data, is unaffected by which shape backs the reads.)
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
- How context-scoped KV keys (`{context}.{entityType}.{id}` under the `ships`/`container`/`meta` buckets, etc.) map onto
  region boundaries — is `context` already the right partition key, or does region need to be
  a separate dimension?

---

## 4. Plan / Phases

*(To be broken into phases once scope is confirmed — following the existing plan format in
`Main-POC-Plan.md`: phase → business rules confirmed → Ginkgo specs written → implementation
→ docs updated → tests green.)*

- [ ] Phase 0 — Confirm requirements (topology driver, scope, consistency needs) with user
- [ ] Phase 1 — TBD
- [ ] Phase 2 — TBD

---

## 5. Open Questions

- See numbered questions in §1. Awaiting user follow-up before proceeding to design.
