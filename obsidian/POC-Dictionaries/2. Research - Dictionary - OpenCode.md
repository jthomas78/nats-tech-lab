
**Context:** This section was produced by an OpenCode research agent (Jul 2026) against the original brief and the existing analysis above. It reads three new primary sources and three GitHub example repos, then cross-references findings against the existing claims. Each finding tags whether it is **[New]** (no prior mention on this page), **[Extends]** (builds on a claim already present), or **[Resolves]** (answers one of the open questions above).

---

## 1. Subject-token order: Synadia's definitive guide (Jun 2026) — [Resolves Q1]

**[Resolves ✓]** The tension noted in §1 (identifier-first vs domain-first) is resolved by reading the source directly. Synadia's guide defines three patterns:

| Pattern               | Structure                              | When to use                                          |
| --------------------- | -------------------------------------- | ---------------------------------------------------- |
| **Namespace-first**   | `orders.customer.created`              | Service-oriented: "all orders" queries               |
| **Identifier-first**  | `tenant-acme.orders.created`           | Per-entity / multi-tenant: "everything for tenant X" |
| **Multi-dimensional** | `prod.us-east.orders.customer.created` | Cross-environment/region routing                     |

The identifier-first pattern (tenant-first) is explicitly recommended for multi-tenant systems. The first token *is* the isolation key. This **validates** the `{tenant}.{region}.{domain}.{event}` decision from [[Claude Discusion Transfer from Private]].

The domain-first extraction that caused the tension was a misinterpretation of the namespace-first pattern — that pattern is for service-oriented (single-tenant) systems.

**Additional findings from the same source:**
- **[New]** Version tokens belong in the subject *only* when version changes affect routing/subscription behavior; header-only versions are fine for additive payload changes. The guide recommends including a version token (`orders.v1.customer.created`) from day one.
- **[New]** Per-message correlation/request IDs must never be in subjects — they explode cardinality and pollute the server's subscription cache. Use message headers instead.
- **[Extends §1]** For hard isolation: "combine identifier-first subjects with separate NATS Accounts." Subject-prefix alone is insufficient.

Source: [synadia.com/blog/designing-nats-subject-hierarchies](https://www.synadia.com/blog/designing-nats-subject-hierarchies) (Jun 2026)

---

## 2. Multi-region consistency spectrum — [Extends §2, New for Dictionary context]

**[New]** NATS JetStream offers three distinct multi-region consistency models, each with different tradeoffs relevant to the dictionary use-case:

| Model | Read Latency | Write Latency | CAS Support | Dictionary suitability |
|---|---|---|---|---|
| **Mirrors** (Super-Cluster) | Low (local) | Low to origin | Yes (on origin) | Origin writes fail if region lost |
| **Stretch Cluster** (Raft ≥3) | Higher | High (cross-region) | Yes | Full CAS, higher write latency |
| **Virtual Streams** (2.10+) | Low (local) | Low (local) | **No** | **Not suitable for multi-write KV** |

Key constraints for the dictionary research context:

- **[Extends §2]** Virtual streams are **explicitly unsupported for KV buckets with simultaneous multi-region key modifications** — CAS is impossible. This is a direct constraint on using NATS KV as a geo-distributed dictionary store where multiple regions might modify the same locale entry.
- **[New]** Under virtual streams, deletes apply locally only — not propagated. No global named durable consumers; consumers are per-region.
- **[New]** Stretch clusters provide immediate consistency across ≥3 regions. At R5 across 5 regions, survives 2 region failures. P99 write latency can be <20ms with proper network provisioning (Derek Collison, P99 conf).

Source: [synadia.com/blog/multi-cluster-consistency-models](https://www.synadia.com/blog/multi-cluster-consistency-models) (Apr 2024)

---

## 3. CQRS/ES on JetStream — updated production maturity — [Extends §4, Addresses Q5]

**[Extends §4]** A NATS maintainer (bruth, Jan 2023) confirms JetStream is architecturally well-suited for event sourcing:
- Per-subject OCC via `Nats-Expected-Last-Subject-Sequence` header — per-entity linearizability without cross-entity contention
- Subjects are indexed within streams, so replaying a single aggregate's history is a bounded scan, not full-stream
- Multiple independent consumers with subject filtering can derive separate read models from same stream
- Single-stream write/read performance beats a single Kafka partition in both throughput and latency

**[Addresses Q5]** The maintainer could not point to a compiled list of production ES/CQRS users in 2023. **No compiled list has surfaced since.** Individual examples exist (see §5 below), and the features are all present, but the pattern is less battle-tested than KV/config-cache. Linebooker would likely be an early adopter for the full ES/CQRS combination. The KV/config-cache use-cases noted in [[Claude Discusion Transfer from Private]] §3 (distributed config, service discovery, locks, counters, CQRS projections) remain far more widely deployed.

**[Extends §4]** No built-in tiered/cold storage as of 2025. Discussion #6478 (Mar 2025) proposes S3-offload as a future feature — still aspirational. Teams needing archival/audit retention must build a separate archiving consumer.

Source: [github.com/nats-io/nats-server/discussions/3772](https://github.com/nats-io/nats-server/discussions/3772)

---

## 4. Dictionary/reference data patterns — [Extends §3, Addresses Q4]

**[Extends §3]** Synadia's subject guide confirms dictionaries/locale data is an identifier-first use case — each tenant's reference data lives under `tenant-acme.refdata.locale.>` or similar, mapping onto KV bucket-per-tenant or key-prefix-per-tenant.

**[Addresses Q4]** The CDC-into-NATS pattern (Postgres as source of truth → Debezium/Conduit → NATS → downstream consumers) was explicitly recommended by a NATS practitioner as an alternative to KV-as-source-of-truth for dictionary data. This aligns with the [[Claude Discusion Transfer from Private]] §3 split: "KV for point lookups, Postgres for anything needing joins/complex queries."

**[Extends §3]** The KV-watch-invalidation pattern is well-documented: services watch a KV bucket with wildcard subjects, maintain an in-memory cache of dictionary items, and react to PUT/DELETE events. Derek Collison explicitly recommends this approach for dictionary/locale data.

Source: [github.com/nats-io/nats.go/discussions/1507](https://github.com/nats-io/nats.go/discussions/1507)

---

## 5. GitHub example architectures — [New]

Three real-world codebases demonstrating NATS in patterns relevant to this research:

- **[New]** **Fizmath-Plaza** ([github.com/Fizmath/Fizmath-Plaza](https://github.com/Fizmath/Fizmath-Plaza)): Go microservice architecture using NATS JetStream as the event backbone. Each service (Payments, Customers, Orders) has its own PostgreSQL, communicates via NATS. Demonstrates service-per-database + NATS as integration fabric — directly applicable to the CQRS pattern described in [[Claude Discusion Transfer from Private]] §2.

- **[New]** **ThreeDotsLabs NATS Example** ([github.com/ThreeDotsLabs/nats-example](https://github.com/ThreeDotsLabs/nats-example)): From the "Event-Driven Architecture in Go" book. Demonstrates CQRS with JetStream as event store: events → JetStream → separate consumer projects into a read-model database. Example code for the projection pattern from §4.

- **[New]** **AleksK1NG Go-NATS-gRPC-PostgreSQL** ([github.com/AleksK1NG/Go-NATS-Streaming-gRPC-PostgreSQL](https://github.com/AleksK1NG/Go-NATS-Streaming-gRPC-PostgreSQL)): Clean-architecture monolith with NATS, gRPC, PostgreSQL. NATS for event communication between domain modules. Listed in [[NATS]] notes.

---

## 6. Additional industry case studies — [Extends §5]

- **[Extends §5]** **PowerFlex** (EV charging): Runs **hundreds of leaf nodes** at the edge, relying on JetStream delivery guarantees to tolerate unreliable/intermittent site internet connectivity. Directly relevant scale/topology precedent for the regional leaf-node design in [[Claude Discusion Transfer from Private]] §4 — logistics depots/warehouses share the same intermittent-connectivity profile.

- **[Extends §5]** **Schaeffler** (industrial manufacturing): Multiple interconnected NATS clusters globally for HA, low latency, and regulatory/data-residency compliance. Demonstrates the super-cluster pattern at industrial scale.

- **[New]** **NATS + Benthos patterns** (from [[NATS]] notes): Four documented integration patterns — Bridging, Enrichment, Analytics Pipeline, and Backup/Tiered Storage. Benthos (Redpanda Connect) provides a declarative stream-processing bridge between NATS and databases/object stores, relevant to the CDC/dictionary-sync pattern from §4 above.

Sources: Synadia customer stories; [synadia.com/screencasts/rethinking-stream-processing](https://www.synadia.com/screencasts/rethinking-stream-processing)

---

## Sources added in this review

| URL | Quality | Tag in findings |
|---|---|---|
| [synadia.com/blog/designing-nats-subject-hierarchies](https://www.synadia.com/blog/designing-nats-subject-hierarchies) | primary (Jun 2026) | Subject-token order resolved |
| [github.com/nats-io/nats-server/discussions/3772](https://github.com/nats-io/nats-server/discussions/3772) | primary (maintainer) | CQRS/ES on JetStream |
| [synadia.com/blog/multi-cluster-consistency-models](https://www.synadia.com/blog/multi-cluster-consistency-models) | primary | Multi-region consistency spectrum |
| [github.com/nats-io/nats.go/discussions/1507](https://github.com/nats-io/nats.go/discussions/1507) | forum | Dictionary KV patterns |
| [github.com/Fizmath/Fizmath-Plaza](https://github.com/Fizmath/Fizmath-Plaza) | secondary | Real-world CQRS example |
| [github.com/ThreeDotsLabs/nats-example](https://github.com/ThreeDotsLabs/nats-example) | secondary | CQRS projection example |
| [github.com/AleksK1NG/Go-NATS-Streaming-gRPC-PostgreSQL](https://github.com/AleksK1NG/Go-NATS-Streaming-gRPC-PostgreSQL) | secondary | Clean architecture + NATS |
| [github.com/nats-io/nats-server/discussions/6478](https://github.com/nats-io/nats-server/discussions/6478) | forum | S3 cold storage proposal |
