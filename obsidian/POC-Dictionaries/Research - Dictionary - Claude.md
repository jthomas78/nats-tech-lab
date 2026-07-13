# NATS.io Research — Industry Patterns for Multi-Tenancy, Multi-Region KV, and CQRS

Builds on [[Claude Discusion Transfer from Private]]. Compiled from a fan-out research run: 6 search angles → 23 sources fetched → 95 claims extracted → top 25 claims put through 3-vote adversarial verification (need 2/3 votes to refute). 22 survived, 3 were refuted.

> **Note on method:** the automated report-writing step of this run malfunctioned (returned placeholder text instead of a synthesis), so this page was compiled directly from the raw verified claim data rather than an auto-generated summary. Findings below are tagged **[Verified]** (survived the 3-vote adversarial panel) or **[Reported]** (extracted from a source but not run through adversarial verification — still worth weighing, just hasn't been independently stress-tested).

---

## 1. Multi-tenancy: Accounts vs. subject-prefix vs. per-tenant buckets

**[Verified]** Synadia's own guidance is that **subject-prefix isolation alone is not sufficient for hard multi-tenant isolation** — for tenants that must never observe each other's traffic, combine identifier-first subjects *with* separate NATS Accounts. ([synadia.com/blog/designing-nats-subject-hierarchies](https://www.synadia.com/blog/designing-nats-subject-hierarchies))

**[Verified]** Synadia recommends **one NATS Account per tenant** as the strongest isolation boundary; relying solely on subject-based permissions within a single shared account is called "more error-prone and generally not recommended for hard multi-tenant isolation." (same source)

**[Verified]** In NATS, **subject hierarchy design *is* the authorization model** — there's no separate ACL layer, so permission management at scale is tightly coupled to how subjects are structured. (same source)

**[Verified]** **Stream-per-tenant is a reasonable JetStream design** for per-tenant FIFO ordering with parallelism across tenants — but Synadia explicitly scopes this to "hundreds to low thousands of tenants" and warns it may not extend to tens of thousands. Concrete resource math given: **at 1,000 tenants × 5 services, expect ~1,000 streams and ~5,000 durable consumers.** ([synadia.com/blog/nats-jetstream-per-tenant-fifo-processing](https://www.synadia.com/blog/nats-jetstream-per-tenant-fifo-processing))

**[Verified]** The core tradeoff for cloud-side layout: a **single shared multi-tenant stream** keeps stream count constant but depends entirely on subject filtering + permissions for isolation; **one stream per tenant** enables per-tenant retention/limits/placement but stream count grows linearly with tenant count — "be prepared to shard tenants across multiple clusters or accounts if one cluster cannot hold them all." ([synadia.com/blog/nats-jetstream-per-tenant-fifo-processing](https://www.synadia.com/blog/nats-jetstream-per-tenant-fifo-processing))

**[Verified]** Per-tenant stream isolation gives a real **noisy-neighbor mitigation**: a slow/blocked consumer for one tenant doesn't stall processing for other tenants, unlike a single shared stream relying on many filtered consumers. ([synadia.com/blog/nats-jetstream-per-tenant-fifo-processing](https://www.synadia.com/blog/nats-jetstream-per-tenant-fifo-processing))

**[Verified]** **JetStream "Domains"** (used to address distinct JetStream deployments across leaf-node connections) are explicitly **not a tenant-isolation mechanism** — they're only an addressing/routing tool for the JetStream API, and a leaf-node connection is not itself a data-loss-prevention boundary. ([synadia.com/blog/tenant-isolated-edge-cold-starts-jetstream](https://www.synadia.com/blog/tenant-isolated-edge-cold-starts-jetstream))

**[Reported, refuted on adversarial review — treat as anecdotal, not confirmed]** A GitHub issue from a team running per-patient KV/Object buckets (healthcare) describes NATS bucket names as **not tokenizable/wildcardable** — permission subjects must enumerate exact bucket names (e.g. `PATIENTRECORDS_JOESMITH` can't be wildcarded), which they say made per-entity bucket permissioning impractical at scale and forced them to build a gateway service with "god-like" JetStream access wrapping their own ACL layer. The quote is genuine, but the verifier panel rejected the claim as stated (2/3 votes) — likely pushback on the "forced them to" framing rather than the underlying naming-scheme fact. Worth reading the source directly before relying on it: [github.com/nats-io/nats-server/issues/5204](https://github.com/nats-io/nats-server/issues/5204).

**[Reported]** Separately, **per-key/per-bucket permission configuration scales poorly** — roughly 1 line of config per bucket, 2 lines per key — becoming unwieldy with many keys/users. Derek Collison (Synadia CEO) acknowledged this gap (Mar 2024) and said a fix would come via a higher-level product ("Synadia Control Plane"), not native server/JWT primitives. Same issue thread as above.

**[Reported]** A real operational pain point independent of the above: **cross-account administration has no single-connection mechanism** — to manage streams/KVs across multiple NATS Accounts you must authenticate separately as each account's user. ([github.com/nats-io/nats-server/discussions/5606](https://github.com/nats-io/nats-server/discussions/5606))

✅ **[Resolved via [[Research - Dictionary - OpenCode]]]** The subject-order tension is resolved in favor of **identifier-first / tenant-first** subjects for multi-tenant systems. Synadia's subject guide distinguishes three patterns:

| Pattern               | Structure                              | When to use                                            |
| --------------------- | -------------------------------------- | ------------------------------------------------------ |
| **Namespace-first**   | `orders.customer.created`              | Service-oriented / "all orders" queries                |
| **Identifier-first**  | `tenant-acme.orders.created`           | Per-entity or multi-tenant / "everything for tenant X" |
| **Multi-dimensional** | `prod.us-east.orders.customer.created` | Cross-environment or cross-region routing              |

The domain-first interpretation was a misread of the namespace-first pattern, which applies better to service-oriented or single-tenant systems. For Linebooker's multi-tenant logistics context, the subject-pattern decision is now **resolved**: use the **Identifier-first** pattern with this format:

```txt
{tenant}.{region}.{bounded_context}.{event}
```

Here, `bounded_context` means the business domain / service-owned bounded context, not the JetStream Domain infrastructure setting. Version tokens should only be placed in the subject when version changes affect routing/subscription behavior; per-message correlation/request IDs belong in headers, not subjects.

#### **NATS Account vs. application tenant mapping:** 
there are two different cases to distinguish:

**1. Multiple app tenants inside one NATS Account**

You can map many application tenants into one NATS Account and separate them by subject prefixes:

```txt
tenant-a.za.shipments.created
tenant-b.za.shipments.created
tenant-c.za.shipments.created
```

Then permissions can restrict users/services to subjects like:

```txt
tenant-a.>
tenant-b.>
```

This is simpler operationally, but it is **not hard isolation**. A permission mistake can expose another tenant's subjects. That is what the warning about subject-prefix isolation is highlighting.

**2. One service accessing multiple tenant NATS Accounts**

This is also possible, but usually through one of these patterns:

| Pattern | Meaning |
|---|---|
| Multiple NATS connections | Service connects separately to each tenant account using different credentials |
| Account imports/exports | Tenant accounts export selected subjects/services; shared service account imports them |
| Shared/internal service account | Platform services run in their own account and are granted controlled cross-account access |
| Operator/admin tooling | Control-plane manages accounts, users, streams, and credentials |

A single NATS user/credential normally belongs to one NATS Account. Cross-tenant account access is usually handled by **imports/exports** or **multiple credentials/connections**, not by making one normal user belong to every account.

**Decision note:** multiple application tenants can share one NATS Account with subject-prefix permissions, but this is soft isolation. For hard tenant isolation, model each tenant as a separate NATS Account and give shared platform services cross-tenant access through explicit imports/exports or separate service credentials. Application tenant identity remains separate from NATS account identity.

---

## 2. Multi-region / geo-distributed KV

**[Verified]** **Mirrors are strictly read-only** and can source from only a single origin stream — clients cannot publish directly to a mirror. This constrains multi-region write patterns: writes must go to the origin/source region, never the mirror. ([docs.nats.io/nats-concepts/jetstream/source_and_mirror](https://docs.nats.io/nats-concepts/jetstream/source_and_mirror))

**[Verified]** **Mirroring and sourcing are asynchronous, one-way replication** — changes on a mirror (deletes, local publishes) never reflect back on the origin. (same source)

**[Verified]** **A given stream (including all its replicas) is bound to a single cluster** — you cannot spread one stream's replicas across multiple clusters, only across servers within one cluster. Cross-cluster distribution requires mirrors/sources, not native stream replicas. (same source)

**[Verified]** NATS mirror/source replication is **explicitly designed to tolerate high-latency, unreliable WAN links** and to work across leaf nodes/leaf-node domains — this is the intended mechanism for geographic distribution. ([synadia.com/blog/mirror-streams-jetstream](https://www.synadia.com/blog/mirror-streams-jetstream))

**[Verified]** **Placement via server tags**: arbitrary metadata tags (geography, hosting provider, sizing tier) combined with `jetstream.unique_tag` force stream replicas onto servers with *distinct* tag values (e.g. distinct AZs/regions) — this is the mechanism for enforcing geographic replica distribution / data-residency rules (e.g. EU PII pinning). ([docs.nats.io/nats-concepts/jetstream/streams](https://docs.nats.io/nats-concepts/jetstream/streams))

**[Verified]** Stream placement can be constrained via CLI/config to a **specific named cluster** (`nats stream add --cluster aws-us-east1-c1`) for regional pinning. (same source)

**[Verified]** **NATS "supercluster" pattern**: multiple regional clusters plus a dedicated cross-region cluster, connected via gateways — used specifically to make streams resilient to *entire regional failures* by spreading replicas across 3 geographic regions. ([synadia.com/blog/multi-cluster-consistency-models](https://www.synadia.com/blog/multi-cluster-consistency-models), [natsbyexample.com](https://natsbyexample.com/examples/use-cases/cross-region-streams-supercluster/cli))

**[Verified]** **The cross-region latency tradeoff is explicit and documented**: spreading replicas across regions for availability costs latency — appropriate only for streams where availability matters more than latency. (same sources)

**[Verified]** **Stretch clusters** achieve immediate/strong multi-region consistency via Raft across ≥3 regions, at the cost of much higher latency for synchronous writes. An R5 stretch cluster survives failure of two entire regions. ([synadia.com/blog/multi-cluster-consistency-models](https://www.synadia.com/blog/multi-cluster-consistency-models))

**[Verified]** **"Virtual streams"** (NATS 2.10+) give multi-region *eventual* consistency: paired per-region write streams + a read stream that sources from all regional write streams, using subject transformation to strip region tokens — transparent low-latency local publish/read, async cross-region replication. (same source)

**[Reported]** Under the virtual-streams (eventual-consistency) model: **no global named durable consumers** — consumers exist per-region only — and message deletions apply *locally only*, not propagated across regions. Read-model/projection consumers built this way need per-region deployment and can't assume globally consistent deletion/tombstone semantics.

**[Reported]** Virtual streams are **explicitly unsupported for KV buckets with simultaneous multi-region key modifications** — compare-and-set is not possible in this mode. This is a direct constraint on using NATS KV as a geo-distributed, multi-write read/dictionary store — important given the project is evaluating KV for exactly this.

**[Reported]** NATS KV **does not provide read-after-write consistency** by design — direct `get()` can be served by any replica, including stale ones. This matches the tradeoff already noted in [[Claude Discusion Transfer from Private]] but is worth flagging as an explicit, named architectural decision on NATS's side, not an incidental limitation.

---

## 3. Dictionary / reference data patterns

**[Reported]** Derek Collison (NATS founder, Synadia CEO), answering directly: **NATS KV can match Redis performance "when there is a real network"** and recommends using **KV watchers to hold a subset of the KV space locally in-app** for even lower latency than remote `get()` calls — directly applicable to caching dictionary/locale data at the edge. ([github.com/nats-io/nats.go/discussions/1507](https://github.com/nats-io/nats.go/discussions/1507))

**[Reported]** A informal benchmark from a NATS collaborator: KV `get()` ~41.7µs average vs Redis ~18µs on loopback — but the gap "becomes relatively insignificant" once real network round-trip latency (hundreds of µs on LAN) is added. Same source also separately reports a production user seeing only ~15,600 puts/sec and ~16,700 gets/sec on **file-storage** KV against a large dataset (far below their >100k/sec requirement) — but throughput scaled to ~150,000 ops/sec with 10 parallel client threads. **Storage backend (memory vs. file) and client parallelism materially affect KV throughput** — worth benchmarking your actual access pattern rather than trusting single-client synchronous numbers.

**[Reported]** A practitioner blog demonstrates the concrete **cache-invalidation-via-watch** pattern for dictionary-style data: watch a KV bucket with wildcard subjects (`services.>`), dispatch to typed per-key handler callbacks (`func(key string, val []byte) error`) so config changes propagate without a service restart. The implementation explicitly **ignores delete events by default** (`IgnoreDeletes`) — deliberately treating key deletion as a non-event rather than a trigger, which the author flags as a choice that may not generalize (e.g. some services may need to react to a config key disappearing). ([badlyenginee.red/posts/2025-11-05-realtime-configs](https://badlyenginee.red/posts/2025-11-05-realtime-configs/))

**[Reported]** Community guidance (NATS-affiliated responder, accepted answer) frames JetStream KV/Object Store as suited only to "NoSQL"-style needs (KV, object store, subject addressing, compare-and-set) — **explicitly not a substitute** for full SQL with schemas, joins, transactions, and isolation. That remains Postgres's job. Directly validates the "KV for point lookups, Postgres for anything needing joins/complex queries" split already noted in [[Claude Discusion Transfer from Private]].

**[Reported]** The same discussion raises an **open, unanswered question in the community**: whether JetStream/KV has WAL-equivalent recovery guarantees comparable to a traditional database — i.e. durability/recovery gaps relative to an RDBMS are not well documented even among practitioners considering KV as a persistence layer. One practitioner explicitly recommends **not** using JetStream as the sole store when live-updating, queryable semantics are needed — instead use a CDC pipeline (e.g. Debezium/Conduit) streaming Postgres changes into NATS, keeping Postgres as the system of record and NATS as a downstream notification/sync layer. This is the inverse direction from "NATS KV as reference-data source of truth" and worth weighing as an alternative pattern for dictionary data specifically.

---

## 4. CQRS projection patterns

**[Reported]** A NATS maintainer's direct guidance on the CQRS pattern: **use multiple independent consumers, each with optional subject-based filtering, to derive separate read models from the same event stream.** This is the core mechanism for per-domain or per-tenant projections off a shared event-sourced stream. ([github.com/nats-io/nats-server/discussions/3772](https://github.com/nats-io/nats-server/discussions/3772))

**[Reported]** Same thread: as of ~2023, a NATS/Synadia-affiliated engineer said there was **no known compiled list of production users running JetStream as a full event store** for ES/CQRS, despite growing DDD/ES/CQRS community interest — i.e. limited public production case-study evidence for this exact pattern at the time. Worth treating "JetStream as CQRS backbone" as a less battle-tested pattern than the KV/config-cache use cases above.

**[Reported]** JetStream supports **optimistic concurrency control (OCC)** at stream or per-subject level via `Nats-Expected-Last-Sequence` / `Nats-Expected-Last-Subject-Sequence` headers — a publish is rejected if the sequence doesn't match. This gives per-entity/aggregate linearizability without cross-entity contention, and subjects are indexed within a stream so replaying a single aggregate's history (subject-filtered consumer) is a bounded scan, not a full-stream scan. Directly applicable to a tenant-scoped aggregate/entity design.

**[Reported]** As of a Jan 2023 discussion (still referenced through 2025), **JetStream has no built-in tiered/cold storage** (no S3-offload equivalent to Pulsar's Tiered Storage). Teams needing long-lived archival/audit retention for event history must build a separate archiving consumer. Relevant to retention planning for a rebuildable CQRS read model.

**[Reported]** Backup/restore mechanics worth knowing for replay/rebuild strategy: restoring a stream requires the **original stream name** (no rename-on-restore), and restoring **without consumer definitions** (`--no-consumers`) can fail to preserve messages in streams using interest-based retention, since retention there depends on active consumer interest that no longer exists post-restore.

---

## 5. Logistics / supply-chain case studies (beyond Sote)

No other public shipping/fleet/delivery/supply-chain-specific NATS case study surfaced. Synadia's own customer-stories page was checked directly and **does not feature any company in this sector** beyond what's already known. The closest production analogs found:

**[Verified]** **Form3** (multi-cloud, low-latency **payments** platform — regulated, not logistics, but structurally similar: multi-region/multi-cloud, mission-critical, strict SLA) migrated from NATS Streaming to JetStream and measured **top message latency drop from ~300ms to under 50ms** (>6× improvement), which they cite as key to meeting a sub-500ms payment SLA. Form3 uses **NATS Leaf Nodes to bridge AWS EKS and a dedicated physical data center** in an active-active multi-cloud setup, explicitly to avoid dependence on any single cloud provider's SLA ("customers were asking what would happen if AWS goes down"). ([synadia.com/blog/how-form3-built-...](https://www.synadia.com/blog/how-form3-built-a-multi-cloud-low-latency-payments-service-with-nats-io-jetstream))

**[Reported]** **PowerFlex** (EV charging infrastructure) runs **hundreds of leaf nodes** at the edge, relying on JetStream's delivery guarantees to tolerate unreliable/intermittent site internet connectivity — a directly relevant scale/topology precedent for regional leaf-node designs. ([synadia.com/customer-stories/powerflex](https://www.synadia.com/customer-stories/powerflex), [synadia.com/customer-stories](https://www.synadia.com/customer-stories))

**[Reported]** **Schaeffler** (industrial manufacturing) runs **multiple interconnected NATS clusters globally** for high availability, low latency, and regulatory/data-residency compliance across regions. ([synadia.com/customer-stories](https://www.synadia.com/customer-stories))

**[Reported]** **Shopmonkey** (SaaS) uses NATS multi-tenancy specifically to separate customer workflows within a shared platform — a real production example of the account/subject-based multi-tenancy pattern discussed in §1, though the source gives no architectural depth beyond the one-line claim. ([synadia.com/customer-stories](https://www.synadia.com/customer-stories))

### GitHub example repositories

These are **example repositories, not production case studies**. They are still useful for seeing how NATS, JetStream, CQRS-style flows, and Postgres can be wired together in code.

- **[Reported]** **Fizmath-Plaza** — Go application using **NATS JetStream**, PostgreSQL, CQRS language, gRPC-Gateway, and a modular / microservice-ready structure. Useful as a concrete example of NATS + Postgres + event-driven application code, though it is an e-commerce demo rather than logistics. ([github.com/Fizmath/Fizmath-Plaza](https://github.com/Fizmath/Fizmath-Plaza))
- **[Reported]** **nats.go examples** — official Go client examples, including a JetStream examples directory. Useful for low-level API patterns around publishing, consuming, JetStream, and client behavior. ([github.com/nats-io/nats.go/tree/main/examples](https://github.com/nats-io/nats.go/tree/main/examples))
- **[Reported]** **natscli** — official NATS CLI codebase. Not an architecture example, but relevant because the CLI covers JetStream management, KV management, Object Store management, stream/consumer monitoring, backup, pub/sub, request/reply, and service API operations. ([github.com/nats-io/natscli](https://github.com/nats-io/natscli))
- **[Reported, legacy caveat]** **Go-NATS-Streaming-gRPC-PostgreSQL** — Go microservice example combining NATS Streaming, gRPC, PostgreSQL, tracing, Prometheus, Grafana, and clean-architecture patterns. Useful for general NATS + Postgres service structure, but **NATS Streaming is legacy/deprecated and should not be treated as a JetStream design template**. ([github.com/AleksK1NG/Go-NATS-Streaming-gRPC-PostgreSQL](https://github.com/AleksK1NG/Go-NATS-Streaming-gRPC-PostgreSQL))

### Booking-oriented GitHub examples

No complete booking/reservation application surfaced that clearly uses **NATS KV** for dictionary/reference data. The closest matches are ticket-booking examples that use NATS as a message broker for event-driven microservices. These are useful for booking workflow patterns, concurrency, reservations, payments, and service boundaries, but they should **not** be treated as evidence for NATS KV dictionary storage.

- **[Reported]** **microservice_k8s_nats** — ticket-booking e-commerce microservice example using Node.js/TypeScript, MongoDB, NATS, Kubernetes, Docker, Next.js, and React. The repository topics indicate `nats-streaming`, so treat it as a legacy NATS Streaming-style example rather than a JetStream/KV template. ([github.com/osmangoninahid/microservice_k8s_nats](https://github.com/osmangoninahid/microservice_k8s_nats))
- **[Reported]** **ticket-booking-microservices** — microservices-based ticket booking system using Node.js and NATS. Useful as a smaller booking-domain example of event-driven service communication, but it does not appear to demonstrate JetStream KV. ([github.com/talhariaz324/ticket-booking-microservices](https://github.com/talhariaz324/ticket-booking-microservices))
- **[Reported]** **ticketing-app** — ticketing backend for booking tickets and payments, with multiple microservices communicating through NATS; the repository description explicitly calls out concurrency/race-condition handling while booking tickets. Useful for reservation/concurrency thinking, but not a KV/dictionary example. ([github.com/msa-ali/ticketing-app](https://github.com/msa-ali/ticketing-app))

---

## 6. Failure modes and lessons learned

**This section didn't make the adversarially-verified top-25 (crowded out by other central+primary claims), but comes from a rigorous, well-known primary source (Kyle Kingsbury / Jepsen, Dec 2025) and Synadia's own published response — treat as high-credibility despite not going through the panel.**

- **[Reported, primary source]** Jepsen's analysis of NATS 2.12.1 found **JetStream's documentation contradicts itself**: it claims to be "Linearizable" while also claiming the system will "self-heal and always be available" — mathematically impossible per CAP theorem. In practice JetStream behaves as a standard Raft system: available only with a reachable majority.
- **[Reported, primary source]** By default, **JetStream acknowledges writes before fsync**, flushing to disk only every 2 minutes, relying on cluster replication (not fsync) to survive an OS crash. Under coordinated power failures, Jepsen measured **~14% of acknowledged writes lost** (131,418 of 930,005).
- **[Reported, primary source]** More severe: **file corruption (single-bit errors/truncation)** in JetStream's `.blk`/snapshot files on even a *minority* of nodes caused **49.6% of acknowledged writes lost** (679,153 of 1,367,069) and **persistent split-brain that survives cluster recovery** — nodes diverging by up to 78% of acknowledged messages. JetStream has limited ability to auto-recover corruption at the beginning/middle of a stream (vs. the end).
- **[Reported, primary source]** Jepsen explicitly did **not** observe data loss under simple network partitions, process pauses, or crashes in 2.12.1 — the failures above required file corruption or coordinated power loss, not routine node failure.
- **[Reported, primary source]** Synadia's own recommended mitigations: use **replication factor 5 across geographically isolated zones**, set `sync_interval=always` (trading performance for durability), and maintain **independent backups** — this is exactly the case for regional/multi-AZ placement in a multi-region logistics platform, and directly informs how conservative the durability config needs to be if JetStream is the event-sourcing source of truth.
- **[Reported, GitHub issue #4710 + independent reproduction in discussion #5468]** Separately from Jepsen: multiple independent production teams (2.10.3, then again on 2.10.11) hit **KV replica drift** — a 3-replica (R3) KV bucket returning different revisions/stale data for the same key from the same server across successive calls, sometimes stale for **hours**, breaking compare-and-swap semantics. Root cause was Raft replica desync that did **not self-heal**; the only workaround (used independently by both teams) was manually scaling replicas 1→3 to force a resync. Synadia could not guarantee it wouldn't recur at the time; it was later patched via a separate fix in 2.10.19.

These durability/consistency findings matter directly for the "is JetStream the event-sourcing source of truth" question raised in [[Claude Discusion Transfer from Private]] — the failure modes are edge-case (corruption, coordinated power loss) rather than routine, but the mitigations (R5, multi-AZ, `sync_interval=always`, independent backups) aren't defaults and should be an explicit config decision, not an assumption.

---

## 7. Where Postgres fits architecturally

The current findings point to a conservative split: **Postgres remains the transactional system of record for relational business state and governed reference data; NATS/JetStream distributes events and rebuild signals; NATS KV serves fast point-lookups, local caching, and watch-based invalidation.** This answers the original "does dictionaries/KV/Locale make sense?" prompt more directly: yes, but only if "dictionary" means simple current-state lookup data with tolerant consistency requirements. If dictionaries need relational validation, audit history, editorial workflow, strong reads, or multi-region write conflict handling, Postgres should own them.

**Recommended architectural roles:**

- **Postgres as write-side database:** command handlers persist aggregates, transactional state, constraints, and relational reference data here. This is where joins, uniqueness rules, foreign keys, SQL queries, transactional updates, audit tables, and strongly-consistent validation belong.
- **JetStream as event backbone:** services publish domain events after successful state changes, either directly from command handling or via an outbox/CDC pattern. Consumers use these events to build projections, trigger workflows, and synchronize downstream stores.
- **NATS KV as read-side/reference cache:** services keep selected dictionaries or locale lookups in KV for low-latency reads, then optionally keep an in-process cache warm using KV watchers. This is best for values like country codes, supported currencies, carrier-zone labels, feature/config flags, or resolved locale strings where stale reads are acceptable for a short period.
- **Projection databases as read models:** CQRS read models do not have to live in KV. Use Postgres, OpenSearch, Redis, KV, or another store based on query shape. If the UI needs filtering, sorting, joins, reporting, or operational dashboards, a Postgres read model is usually a better fit than KV.

**Two viable dictionary patterns:**

1. **Postgres-owned dictionaries, NATS-distributed cache:** Postgres stores canonical dictionary rows. Changes are emitted through an outbox, Debezium/Conduit, or application-published events into NATS. A consumer updates NATS KV, and services watch KV for local cache invalidation. This is the safer default for locale/reference data that needs governance, auditability, or admin UI editing.
2. **NATS KV-owned dictionaries:** KV is the source of truth for small, operational, point-lookup data. This is simpler and fast, but only fits when there are no complex queries, no strong read-after-write requirement, no rich audit/versioning requirement beyond KV revision history, and no simultaneous multi-region writes to the same key.

**CQRS implication:** in a greenfields logistics system, Postgres and NATS are complementary rather than competing. A service can use Postgres for its command model, publish events to JetStream, and build one or more query models from those events. KV is one possible query model for narrow lookups; it is not a general replacement for a relational read database.

**Concrete logistics examples:**

- Shipment, booking, invoice, carrier contract, and tenant configuration entities: **Postgres**
- Domain events such as `shipment.created`, `booking.accepted`, `invoice.issued`, `carrier.rate.updated`: **JetStream**
- Country/currency code lookups, carrier-zone labels, UI locale bundles, feature flags, service config: **NATS KV or in-process caches fed by KV**
- Search screens, operational dashboards, exception queues, finance reports, customer-visible shipment lists: **Postgres read model or purpose-built query store**

**Key design question to add:** is the team evaluating **NATS KV instead of Postgres**, or **NATS KV as a distribution/cache layer in front of Postgres**? The current evidence strongly favors the second option for dictionary/locale data unless the data is small, simple, centrally written, and tolerant of eventual consistency.

**External support for this recommendation:** no single external source was found that prescribes this exact Linebooker-specific architecture. The recommendation is a synthesis from several narrower sources:

- **NATS KV as a cache/watch layer:** Derek Collison recommends KV watchers to keep a subset of KV data locally in-app and up to date, which supports the "KV as distribution/cache/watch layer" part of the recommendation. ([github.com/nats-io/nats.go/discussions/1507](https://github.com/nats-io/nats.go/discussions/1507))
- **NATS KV watch semantics:** the NATS docs describe watching individual keys or all keys in a bucket, and also state that direct KV reads do not guarantee read-your-writes because reads may be served by followers or mirrors. That supports treating KV carefully for governed data that needs strong consistency. ([docs.nats.io/nats-concepts/jetstream/key-value-store](https://docs.nats.io/nats-concepts/jetstream/key-value-store))
- **Concrete watch-based config pattern:** a practitioner example shows services watching KV keys such as `services.>` and applying runtime configuration changes without service restarts. This supports dictionary/config distribution, but not KV as the canonical relational store. ([badlyenginee.red/posts/2025-11-05-realtime-configs](https://badlyenginee.red/posts/2025-11-05-realtime-configs/))
- **Outbox/CDC pattern for database-owned changes:** Debezium documents the outbox event-router pattern for capturing outbox-table changes and emitting integration events. This supports a Postgres-owned source-of-truth model where changes are propagated downstream through an event pipeline. ([debezium.io/documentation/reference/stable/transformations/outbox-event-router.html](https://debezium.io/documentation/reference/stable/transformations/outbox-event-router.html))

---

## Sources fetched

| URL | Quality | Angle |
|---|---|---|
| [synadia.com/blog/nats-jetstream-per-tenant-fifo-processing](https://www.synadia.com/blog/nats-jetstream-per-tenant-fifo-processing) | primary | multi-tenancy |
| [github.com/nats-io/nats-server/issues/5204](https://github.com/nats-io/nats-server/issues/5204) | primary | multi-tenancy |
| [github.com/nats-io/nats-server/discussions/5606](https://github.com/nats-io/nats-server/discussions/5606) | forum | multi-tenancy |
| [synadia.com/blog/designing-nats-subject-hierarchies](https://www.synadia.com/blog/designing-nats-subject-hierarchies) | primary | multi-tenancy |
| [synadia.com/blog/tenant-isolated-edge-cold-starts-jetstream](https://www.synadia.com/blog/tenant-isolated-edge-cold-starts-jetstream) | primary | multi-tenancy |
| [synadia.com/blog/multi-cluster-consistency-models](https://www.synadia.com/blog/multi-cluster-consistency-models) | primary | multi-region |
| [docs.nats.io/nats-concepts/jetstream/source_and_mirror](https://docs.nats.io/nats-concepts/jetstream/source_and_mirror) | primary | multi-region |
| [synadia.com/blog/mirror-streams-jetstream](https://www.synadia.com/blog/mirror-streams-jetstream) | primary | multi-region |
| [docs.nats.io/nats-concepts/jetstream/streams](https://docs.nats.io/nats-concepts/jetstream/streams) | primary | multi-region |
| [natsbyexample.com — cross-region supercluster](https://natsbyexample.com/examples/use-cases/cross-region-streams-supercluster/cli) | primary | multi-region |
| [nats-architecture-and-design ADR-8](https://github.com/nats-io/nats-architecture-and-design/blob/main/adr/ADR-8.md) | primary | multi-region |
| [github.com/nats-io/nats.go/discussions/1507](https://github.com/nats-io/nats.go/discussions/1507) | forum | dictionary/reference data |
| [badlyenginee.red — realtime configs](https://badlyenginee.red/posts/2025-11-05-realtime-configs/) | blog | dictionary/reference data |
| [github.com/nats-io/nats-server/discussions/3772](https://github.com/nats-io/nats-server/discussions/3772) | primary | CQRS projection |
| [synadia.com/blog/copy-jetstream-stream-messages-backup-mirror](https://www.synadia.com/blog/copy-jetstream-stream-messages-backup-mirror) | primary | CQRS projection |
| [synadia.com/blog/how-form3-built-...](https://www.synadia.com/blog/how-form3-built-a-multi-cloud-low-latency-payments-service-with-nats-io-jetstream) | primary | industry case study |
| [synadia.com/customer-stories](https://www.synadia.com/customer-stories) | secondary | industry case study |
| [jepsen.io/analyses/nats-2.12.1](https://jepsen.io/analyses/nats-2.12.1) | primary | failure modes |
| [synadia.com/blog/jepsen-nats-2-12-1](https://www.synadia.com/blog/jepsen-nats-2-12-1) | primary | failure modes |
| [github.com/nats-io/nats-server/issues/4710](https://github.com/nats-io/nats-server/issues/4710) | primary | failure modes |
| [github.com/nats-io/nats-server/discussions/5468](https://github.com/nats-io/nats-server/discussions/5468) | forum | failure modes |
| [github.com/Fizmath/Fizmath-Plaza](https://github.com/Fizmath/Fizmath-Plaza) | secondary | GitHub example architecture |
| [github.com/nats-io/nats.go/tree/main/examples](https://github.com/nats-io/nats.go/tree/main/examples) | primary | official client examples |
| [github.com/nats-io/natscli](https://github.com/nats-io/natscli) | primary | official CLI / operations examples |
| [github.com/AleksK1NG/Go-NATS-Streaming-gRPC-PostgreSQL](https://github.com/AleksK1NG/Go-NATS-Streaming-gRPC-PostgreSQL) | secondary | GitHub example architecture, legacy NATS Streaming |
| [github.com/osmangoninahid/microservice_k8s_nats](https://github.com/osmangoninahid/microservice_k8s_nats) | secondary | booking-oriented GitHub example, legacy NATS Streaming |
| [github.com/talhariaz324/ticket-booking-microservices](https://github.com/talhariaz324/ticket-booking-microservices) | secondary | booking-oriented GitHub example |
| [github.com/msa-ali/ticketing-app](https://github.com/msa-ali/ticketing-app) | secondary | booking-oriented GitHub example |

---

## Open questions this raises

1. **Isolation boundary**: does the project need Accounts-per-tenant (Synadia's stated strongest boundary), or is subject-prefix + permissions sufficient given the tenant-count ceiling ("hundreds to low thousands") that stream-per-tenant is scoped to?
2. **Durability posture**: if JetStream is the event-sourcing source of truth, does the project adopt Synadia's post-Jepsen recommendations (R5, multi-AZ, `sync_interval=always`, independent backups) as a baseline, or accept default durability?
3. **KV as dictionary store vs. cache-in-front-of-Postgres**: given KV's explicit lack of read-after-write consistency and unsupported multi-region CAS, does dictionary/reference data live in KV as source of truth, or does Postgres remain source of truth with KV as a watch-invalidated cache (the CDC-into-NATS pattern one practitioner recommended)?
4. **CQRS-on-JetStream production maturity**: given the NATS team itself couldn't point to a compiled list of production ES/CQRS users as of 2023, is there more recent evidence of this pattern hardening, or is Linebooker likely to be an early adopter of this specific combination?
5. **Database ownership boundaries**: which service owns each Postgres schema/database, and are integration events emitted from an application outbox/CDC pipeline or published directly by command handlers?

---
