# NATS.io Evaluation for Linebooker V3 — Discussion Summary

  

Context: Evaluating NATS.io (JetStream + KV) as the event backbone for a greenfields V3 logistics platform, under a CQRS architecture, with a specific question about where "dictionaries"/locale reference data fit.

  

---

  

## 1. Clarifying the original task

  

The brief mentioned three things that sound unrelated but aren't: **event source streaming**, **dictionaries/KV**, and **locale**. These map onto two distinct NATS subsystems:

  

- **JetStream** — durable, replayable event streams. This is the event-sourcing backbone (order placed, shipment dispatched, delivery confirmed).

- **NATS KV** — built *on top of* JetStream (a KV bucket is materialized as a stream named `KV_<bucket>`). Not an event log — a materialized, mutable, current-state store (immediately consistent for monotonic writes/reads, but not read-your-writes, since direct gets may hit followers/mirrors).

  

"Dictionaries" = reference/lookup data (country codes, currency codes, carrier zones, i18n strings) — a good fit for KV, but a separate concern from event sourcing.

  

Open questions worth getting answers to before scoping further:

1. Is dictionary data meant to be event-sourced/versioned, or just current-state KV?

2. Does "Locale" mean i18n (UI strings) or regional/geographic (tax jurisdiction, currency, carrier zone)?

3. Consistency requirements for lookups (strong vs. eventually-consistent)?

4. Centrally-owned dictionaries or per-tenant/per-region?

5. Replicated across regional Leaf Nodes, or single-region for V3 initially?

6. Is the deliverable "KV replaces Postgres for reference data" or "KV as a cache in front of Postgres"?

7. Does dictionary data need audit/versioning? (KV keeps revision history per key, capped at 64 versions.)

  

## 2. How CQRS reconciles event streaming + KV

  

- **Write side (Command):** JetStream captures domain events as source of truth — append-only, replayable.

- **Read side (Query):** A consumer subscribes to JetStream events and projects them into a materialized read model — this is where KV fits, as a fast point-lookup store for "current state" views.

- **Dictionaries** sit alongside the CQRS read side but aren't projections of domain events — they're independently-managed reference data that query handlers use to enrich projected state.

  

Additional questions raised by the CQRS framing:

- Who builds/owns the read-model projection consumers?

- Is KV the only read-side store, or KV for point lookups + Postgres for anything needing joins/complex queries (KV has no query language, no secondary indexes)?

- Should read models be rebuildable from JetStream history on demand (implies retention planning)?

- Do command handlers need strongly-consistent reads for validation (may require going to Postgres or the stream leader rather than a KV mirror)?

- Denormalize dictionary values into projections at write time, or resolve them live at read time?

  

## 3. NATS KV — research findings

  

**What it is:** An abstraction over a JetStream stream. Buckets are streams (`KV_<name>`); everything doable on a bucket is doable on the underlying stream directly, with more control.

  

**Best use cases (from docs, ADRs, and production blogs):**

1. Distributed configuration / feature flags (watch-based live reload)

2. Service discovery (watch a wildcard key pattern for instance registration)

3. Distributed locks / leader election / semaphores (atomic `Create`/`Update` with revision-based CAS) — usable but not a drop-in Redlock replacement; at least one team tried a similar token-queue lock pattern and abandoned it due to issues when processing was slower than incoming tokens

4. Redis-replacement for hot key-value + atomic counters (KV get latency ~40µs + network round trip on low-end hardware, in the same neighborhood as Redis)

5. CQRS read-model/projection store (the main relevant pattern here)

6. Metadata/index layer paired with Object Store (small metadata in KV, large blobs in Object Store)

  

**Limitations:**

- No secondary indexes or query language — pure key→value; anything needing joins/range queries needs Postgres or your own indexing

- History depth capped at 64 versions per key

- No cap on total key count (only on total bucket byte size via `max_bytes`)

- Redis still wins for sub-millisecond counters/rate limiters and rich data structures (sorted sets, Lua scripting)

  

**Multi-region behavior (relevant to the tenant/region Leaf Node design):**

- KV `get()` operations automatically route to the local cluster's mirror — fast local reads, with unavoidable "incoherence" tradeoff any cache introduces

- Placement tags can pin a stream to a specific regional cluster (e.g., EU PII data pinned to an EU cluster), with mirrors elsewhere for local read access

  

**Real-world examples:**

- **Sote** — Africa-focused logistics/shipments platform, chose NATS/JetStream for a service-first, domain-segmented architecture (Shipments, Transportation, Jobs) — architecturally close to Linebooker's setup

- **i-flow / manufacturing case study** — real-time analytics across multiple factory locations using NATS Leaf Nodes to bridge edge-level and enterprise-level processing

  

## 4. Why NATS Leaf Nodes (from the i-flow diagram)

  

Leaf nodes let each site (factory, or in Linebooker's case, region) run a locally resilient NATS presence that's still part of one logical enterprise-wide system, without flattening the network:

  

1. **Firewall respect** — leaf node makes an outbound connection to the hub; no inbound ports need opening, no direct enterprise visibility into local edge zones

2. **Local resilience** — local pub/sub keeps working even if the WAN link to the hub drops

3. **Traffic shaping** — raw/high-volume local data gets integrated and harmonized locally before only the useful subset crosses the WAN

4. **Multi-site aggregation** — every site looks identical from the enterprise side; central services don't need to know which physical site data originated from

5. **Security domain isolation** — leaf nodes can run under distinct credentials/accounts from the hub, containing blast radius

  

Directly maps onto Linebooker's regional Leaf Node design with per-tenant Accounts.

  

## 5. Subject convention: `{tenant}.{region}.{domain}.{event}` vs `{region}.{tenant}.{domain}.{event}`

  

Both are valid in NATS — wildcards work at any token position. Tenant-first was reasoned as the better default:

  

1. **Tenant is the stable partition; region is an infra concern** that changes more often (new regions, rebalancing, DR) — tenant-first keeps the tenant's namespace stable regardless of infra reshuffling

2. **A tenant's fleet spans multiple regions** — with tenant-first, "everything for tenant A" is one subscription (`tenantA.>`); with region-first, you'd need to enumerate every region a tenant has touched and subscribe to each separately, which breaks silently the first time a tenant enters a new region

3. **Permissions/account exports map onto the leading token** — per-tenant access grants are a single clean prefix with tenant-first

4. **Regional filtering still works fine either way** — a regional leaf node can filter with `*.us-east.>` just as validly as `us-east.*.>`, so tenant-first doesn't cost you regional data-residency or WAN traffic shaping

  

**When region-first would actually win:** if tenant data can *never* cross a region boundary (hard regulatory partition) and every tenant is guaranteed single-region for life — not the case for cross-border trucking/logistics.

  

---

  

*Generated from a Claude.ai conversation for transfer to another Claude Team account. Original conversation can also be shared via the Share button for the full message-by-message transcript.*