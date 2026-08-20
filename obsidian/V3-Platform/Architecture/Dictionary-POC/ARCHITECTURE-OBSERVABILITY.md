# ARCHITECTURE-OBSERVABILITY.md

Architecture reference for `observability-service`'s cross-account
diagnostics surface. Today that surface is documented panel-by-panel in
[ARCHITECTURE-ADMIN.md](ARCHITECTURE-ADMIN.md) §4 (Connections, Services,
Log, Request/Reply & Traces, Streams, KV Buckets); this file holds decision
records for observability-service capabilities that don't yet have a home
there because they aren't implemented. The first is below. Once a decision
here ships, its panel/data-flow detail moves into
[ARCHITECTURE-ADMIN.md](ARCHITECTURE-ADMIN.md) §4 alongside its siblings,
the same way every other panel in that document is described, and this file
keeps only the ADR as a historical record.

---

## ADR-047: Cross-Tenant Pub/Sub Observability ("Wire Tap") in the Admin UI

**Status:** Accepted (2026-08-20) — implementation explicitly on hold at the
repo owner's request; no business rules, tests, or code exist yet.
**Date:** 2026-08-20
**Deciders:** repo owner — this ADR gated
[Phase 47](../../../../.claude/plans/Main-POC-Plan.md) per the repo's design
gate, now approved.

### Context

NATS documents a "wire tap" monitoring pattern
(`docs.nats.io/concepts/subjects#wire-taps-and-monitoring`): subscribe to a
broad wildcard (`>`) and observe whatever crosses it. The ask was to add
this to `observability-service` so the Admin UI can show live pub/sub
traffic across **every tenant account**, not just the current one.

That pattern doesn't transfer directly to this lab. NATS accounts are this
lab's hard, server-enforced tenant boundary — a subscription to `>` only
ever sees the subject space of the account it's connected as (see
[ARCHITECTURE-ACCOUNTS.md](ARCHITECTURE-ACCOUNTS.md) trust-chain section).
There is no cross-account wildcard subscribe; cross-account visibility only
ever exists where a tenant's account JWT explicitly exports specific
subjects and PLATFORM explicitly imports them — the mechanism BR-AC30/31/32
already use for trace spans, service discovery, and JetStream/KV
introspection respectively. This lab has also already moved *away* from
broad cross-account grants once: Phase 30h retired an earlier
`PlatformFullJS`-style unrestricted second PLATFORM connection in favor of
`observability-service`'s single, narrowly-scoped one.

Separately, `SYS` (the operator-mode system account, `nats/nats.conf`)
already carries the standard, currently-unused
`account-monitoring-streams`/`account-monitoring-services` exports
(`$SYS.ACCOUNT.*.>` / `$SYS.REQ.ACCOUNT.*.*`) — NATS's own built-in
system-account monitoring. Nobody imports them today. They would give
connection/subscription/rate **metadata** per account, never message
payloads — a different, narrower capability than what "wire tap" implies.

Request/reply traffic (`rpc.*`/`api.*`) already has a cross-account
observability answer: `natstrace`'s `obs.trace.*` envelope (BR-036/037),
publish-side-instrumented, narrowly exported per tenant into PLATFORM
(BR-AC30), rendered live in the Admin UI's Request/Reply & Traces panel
([ARCHITECTURE-ADMIN.md §4.5](ARCHITECTURE-ADMIN.md)). What's genuinely
missing is the same visibility for **fire-and-forget** traffic —
`evt.*` (event-sourcing) and `notify.*` (change notification) — which
neither `obs.trace.*` nor a `$SYS` import would answer.

### Decision

Add a second, sibling envelope — **`obs.pubsub.*`** — instrumented only at
`evt.*`/`notify.*` publish call sites, reusing BR-036/037's
redact-before-truncate discipline and BR-AC30's narrow per-tenant
export/import pattern (a second narrow export alongside the existing one,
not a new grant shape). It gets its own dedicated **Messages** panel in the
Admin UI rather than a tab inside the existing Request/Reply & Traces
panel, since "what was published" is a different question from "what was
called/replied to."

Concretely:

- **Publish-side hook**, wired explicitly at each `evt.*`/`notify.*` call
  site — never by wrapping a shared low-level `Publish()`/`js.Publish()`
  generically. This is a deliberate guard: a generic wrap risks the hook
  observing its own `obs.pubsub.*` publish (a self-observation loop) or
  picking up unrelated JetStream control traffic that happens to share the
  same underlying client call.
- **Subject shape** `obs.pubsub.{context}.{service}.{entity}.{action}`,
  mirroring `obs.trace.*`'s taxonomy.
- **`observability-service`** gains a sibling consumer to `tracestore` for
  `obs.pubsub.>`. No request/reply pair to assemble (unlike RPC spans), so
  each publish becomes one standalone entry. Retention follows the same
  bounded-stream convention `tracestore`'s `TRACES` stream already uses
  (`LimitsPolicy` + `MaxAge` + `MaxBytes`) — a service-owned implementation
  detail, not a new design decision; exact caps tuned at implementation
  time given `evt.*` volume is expected to exceed RPC trace volume.
- **Admin UI**: new "Messages" panel, with an `evt` / `notify` family
  filter — the same family-toggle-chip pattern `RpcPanel.vue` already uses
  for `rpc`/`api` — needed because both families share `obs.pubsub.*` but
  mean different things (`evt.*` is durable/JetStream-backed; `notify.*` is
  ephemeral, non-JetStream).

**Why this doesn't need a request/reply or JetStream-internals filter.**
The split from `obs.trace.*` is clean by construction: `rpc.*`/`api.*`
(request/reply) already flow through `obs.trace.*`; `obs.pubsub.*` never
touches them. Nor does it touch JetStream's own protocol traffic — a
synchronous `js.Publish`'s PubAck reply, `$JS.API.*` consumer/ack control
subjects — because the hook fires only where domain code explicitly calls
publish for an event or notification, not via a passive subscription
against wire traffic. There's nothing else on the wire for it to pick up.

### Options Considered

#### Option A: Blanket per-tenant export of `>` into the observability account

| Dimension | Assessment |
|---|---|
| Complexity | Low (mechanically identical to existing narrow exports, just wider) |
| Coverage | Complete — every byte, including uninstrumented/future publishers |
| Security posture | **Regression** — every tenant's business payloads (rates, manifests, etc.) flow through the observability plane |
| Consistency with existing rules | Breaks BR-AC32's explicit "never a blanket wildcard" principle; reverses the direction Phase 30h deliberately took (retiring an unrestricted PLATFORM connection) |
| Team/POC fit | Would need its own new safety design (subscribe-only permissions, a redaction gate, sampling, platform-admin-only Admin UI role) before it's defensible at all |

**Pros:** the only design with zero instrumentation gaps; simplest to reason about as "does this see everything."
**Cons:** a first-time breach of this lab's hard-won tenant-isolation discipline; unbounded exposure of tenant business data to the observability plane; explicitly contradicts BR-AC32 and the Phase 30h precedent.

#### Option B: Import the dormant `$SYS` account-monitoring exports

| Dimension | Assessment |
|---|---|
| Complexity | Low — the exports already exist in the `SYS` account JWT, unused; just needs an import + a consumer |
| Coverage | Connection/subscription/rate metadata only — no payloads, doesn't answer the actual ask |
| Security posture | No new exposure — this is NATS's own built-in operator-level monitoring, not tenant data |
| Consistency with existing rules | Fully additive, no precedent conflict |
| Team/POC fit | Good complementary panel, poor standalone answer to "wire tap" |

**Pros:** cheap, safe, reuses infrastructure that's already provisioned and sitting idle.
**Cons:** doesn't show message content, so on its own it doesn't satisfy the original ask.

#### Option C: New sibling `obs.pubsub.*` envelope, instrumented at `evt.*`/`notify.*` publish call sites (chosen)

| Dimension | Assessment |
|---|---|
| Complexity | Medium — new envelope + new consumer + new panel, but reuses BR-036/037's shape, BR-AC30's export pattern, and most of the trace-panel plumbing |
| Coverage | Every instrumented publish call site — misses anything nobody wires the hook into |
| Security posture | No new cross-account grant shape; same narrow, explicit, redacted, per-tenant export this lab already trusts |
| Consistency with existing rules | Extends BR-036/037/BR-AC30 rather than contradicting them |
| Team/POC fit | Best fit for a lab whose stated goal is evaluating these patterns deliberately, not maximizing raw capture |

**Pros:** keeps the account boundary intact; reuses proven redaction/export/UI patterns almost unchanged; the coverage gap is a known, bounded, and deliberate cost rather than an open-ended one.
**Cons:** a publisher nobody instruments is invisible — no passive safety net the way a true wire tap would give; needs its own redaction-list review since event/notify payloads may carry different sensitive fields than RPC payloads did.

### Trade-off Analysis

The real trade-off isn't technical, it's how much tenant-isolation risk this
lab is willing to accept in exchange for capture completeness. Option A
buys total, gap-free coverage at the cost of reversing a security principle
this codebase has already enforced and then doubled down on (BR-AC32,
Phase 30h). Option B is risk-free but answers a narrower question
(activity, not content). Option C accepts a bounded, known coverage gap
(uninstrumented call sites) in exchange for keeping every existing
isolation guarantee intact and reusing infrastructure this lab already
trusts — the same trade this lab already made once for RPC traffic
(`obs.trace.*` over a truly generic tap), now made consistently for pub/sub
traffic too.

### Consequences

- **Easier:** operators get a genuine, live, cross-tenant view of
  `evt.*`/`notify.*` traffic without any new cross-account grant shape to
  reason about; the Admin UI gains a natural home (Messages panel) for a
  question the existing panels don't answer.
- **Harder:** every new `evt.*`/`notify.*` publish call site introduced in
  the future must remember to wire the observation hook, or it's silently
  invisible to this feature — this needs to be a checked convention (e.g.
  a lint/test), not tribal knowledge.
- **Resolved (2026-08-20):** Option B (dormant `$SYS` exports) **does**
  become a follow-on — tracked as candidate
  [Phase 108](../../../../.claude/plans/Main-POC-Plan.md), not part of this
  phase. Its own design must include a UI account filter — `$SYS.ACCOUNT.*.>`
  spans every account at once, so the panel needs a way to narrow to one
  (or a small set) rather than rendering an undifferentiated cross-tenant
  firehose, the same way the new Messages panel here needs its `evt`/
  `notify` family filter.
- **Resolved (2026-08-20):** consumer-side behavior (redelivery counts, ack
  latency for `evt.*` projectors) is **out of scope for this phase.** This
  phase shows only that something was published — not what happened to it
  downstream. Explicitly left open to evolve later once publish-side
  visibility is in place and proves useful; not designed here.
- **Still to revisit:** the redaction denylist, which needs its own review
  for event/notify payloads rather than inheriting BR-036's RPC-shaped list
  unreviewed.

### Action Items

1. [ ] Business-rules-first pass: draft candidate rules for the
       `obs.pubsub.*` envelope, its redaction scope, and the
       explicit-hook-only (never-generic-wrap) constraint — continuing the
       `BR-04x` sequence in
       [BUSINESS_RULES-SHIPPING.md](../../../../demos/01-dictionary/BUSINESS_RULES-SHIPPING.md),
       plus sibling-service equivalents per the BR-036/BR-D39/BR-P25/BR-TP15
       convention — for confirmation before any code is written.
2. [ ] Derive Ginkgo specs from those rules (red → green → refactor).
3. [ ] Implement: publish-side hook at `evt.*`/`notify.*` call sites →
       `observability-service`'s `obs.pubsub.>` consumer + bounded retention
       → Admin UI Messages panel with the `evt`/`notify` family filter.
4. [ ] Once implemented, move this panel's description into
       [ARCHITECTURE-ADMIN.md](ARCHITECTURE-ADMIN.md) §4 alongside its
       siblings (panel index table, data-flow archetype, UI design), and
       leave only this ADR behind here as the historical decision record.
