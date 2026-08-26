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

## The message path, end to end

![Two parallel ingest paths: a tenant service publishes obs.trace.> and obs.pubsub.> inside its own NATS account; JWT stream exports carry both into the PLATFORM account, each with a per-tenant LocalSubject remap onto monitor.{tenant}.trace.> and monitor.{tenant}.pubsub.>; each lands on its own LimitsPolicy stream (TRACES 1h/64 MiB, PUBSUB 1h/32 MiB) that captures both the remapped tenant form and the bare obs.* form PLATFORM publishes itself, is read by one durable AckExplicit consumer, and is projected into a KV bucket — trace-request-reply merged by spanId with the tenant stored per span, pubsub-messages plain-Put, both bounded at 15 min / 8 MiB. Both buckets emit a best-effort notify subject that observability-service bridges to the Admin UI over WebSocket, alongside the REST snapshot each feed bootstraps from.](images/observability-message-path.png)

Editable source: [observability-message-path.html](../../../../demos/01-dictionary/diagrams/observability-message-path.html)
— hand-authored inline SVG rather than a Draw.io workbook page, so
`./diagrams/export-png.sh` does **not** regenerate it. Re-export with

```
node diagrams/export-html-png.mjs diagrams/observability-message-path.html \
  ../../obsidian/V3-Platform/Architecture/Dictionary-POC/images/observability-message-path.png 1024 --clip=".wrap"
```

from `demos/01-dictionary/`. The 1024px width is the geometry the page was
reviewed at; changing it changes the layout. The `--clip=".wrap"` is
load-bearing, not optional.

---

## ADR-047: Cross-Tenant Pub/Sub Observability ("Wire Tap") in the Admin UI

**Status:** Accepted (2026-08-20), **amended 2026-08-25** after a
pre-implementation design review — see "Amendment (2026-08-25)" at the end of
this ADR, which changes the export/import shape, the placement of the `evt.*`
hook, and the scope of instrumented call sites. **Cleared for implementation
2026-08-25**: the hold placed at approval is lifted, and the business rules
(BR-045–049, BR-D45, BR-AC34, BR-TP75) are confirmed. No code exists yet;
Phase 43 is now in the live plan. One gate remains inside 43a — A8's
redaction review runs before the hook is wired.
**Date:** 2026-08-20
**Deciders:** repo owner — this ADR gated
[Phase 43](../../../../.claude/plans/Main-POC-Plan.md) per the repo's design
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

---

## Amendment (2026-08-25) — pre-implementation design review

The decision above (Option C over Options A/B) stands unchanged and was
re-confirmed. This amendment records what a stress-test of the design against
the code it would attach to found, and the resolutions taken. Where a
resolution contradicts the body of the ADR above, **the resolution wins** —
the body is left intact as the record of what was decided on 2026-08-20.

### A1 — Tenant provenance is not recoverable, and the goal depends on it

The goal is a view of traffic across *every tenant account*, but nothing in
the design as approved recovers **which** account an entry came from:

- PLATFORM imports every tenant's `obs.trace.>` onto the **identical local
  subject**, with no `LocalSubject` remap (`addPlatformTraceImport`,
  `accounts/provisioner.go`). `$SRV.>` and `$JS.API.*` *do* remap per tenant
  (`monitorLocalSubjectTmpl`, `jsAPILocalSubjectTmpl`) for exactly this
  reason — the comment there states that PLATFORM's import of a second tenant
  would otherwise collide with the first.
- `natstrace`'s wire envelope carries no account field.
- `{context}` is the company / business-unit scope, explicitly **not** the
  tenant (CLAUDE.md, Phase 16a), so the subject does not carry it either.
- `TraceWaterfall.vue` already documents the consequence: its account gutter
  "can only show a coarse PLATFORM/TENANT split … not which specific"
  account.

The account boundary disambiguates *delivery*, not *provenance*: once N
tenants' streams are imported onto one local subject, a PLATFORM subscriber
sees no indication of origin.

**Resolved:** PLATFORM's import of `obs.pubsub.>` carries a per-tenant
`LocalSubject` remap — `monitor.{tenant}.pubsub.>` — mirroring the pattern
already proven twice in this file for `$SRV.>` and `$JS.API.*`. The tenant
token is inserted **by the NATS server**, so it is unspoofable, unlike an
envelope field a tenant fills in itself. The Messages panel reads the tenant
from that token. BR-AC34 and BR-048 carry this.

**Closed 2026-08-26 (Phase 48).** The deferred half — applying the same
remap to `obs.trace.>` — is done, and it is the whole point of that phase:
`addPlatformTraceImport` now carries `monitor.{tenant}.trace.>` (BR-AC36),
`TRACES` captures both that and the bare `obs.trace.>` PLATFORM publishes
itself, and `TraceWaterfall.vue`'s gutter names the real account instead of
the coarse PLATFORM/TENANT split this item predicted it would be stuck with
(BR-051, BR-054). Three things the implementation settled that this ADR did
not anticipate:

- **Attribution is per span, not per trace.** An ordinary cross-account
  chain has spans on both sides of the boundary, so a single tenant on the
  record would have to label one of its own spans falsely — on the one panel
  whose entire value is trustworthy provenance. BR-052, drafted as a
  first-writer-wins tie-break, was retired for the same reason: the
  disagreement it arbitrated is the normal case, not a conflict.
- **The remap must be converged, not just minted.** An account provisioned
  before the change keeps the old import until something rewrites it, so
  `accounts-service` now re-converges PLATFORM's imports for every known
  tenant on every start (BR-AC37) rather than relying on a one-off
  re-provision pass someone has to remember.
- **A shipped pipeline needed no migration.** `TRACES` is `LimitsPolicy`
  capped at 1 h, so the old subject shape ages out on its own; records
  written before the change decode through an explicit legacy path and
  render as `unattributed` rather than as a guessed tenant.

### A2 — The instrumented-call-site list was incomplete, and one service had no rule

BR-045 named five choke points. A sweep of the tree found these additional
live `evt.*`/`notify.*` publishers, none of them covered:

| Publisher | Disposition |
|---|---|
| `shipping-service/internal/kvstore/kv.go` — `notify.{ctx}.kv.*` | **In** — BR-045 |
| `dictionary/internal/eventhandler/platform_notify.go` — `notify._platform.refdata.*` | **In** — BR-045 |
| `refdata` `notifybridge.go` — `notify.{ctx}.refdata.*` | **In** — BR-D45 |
| `accounts` `handler.go` ×4 — `notify.accounts.account.*` | **In** — BR-AC34 |
| `organizations-service` transporter-profile `evt.*` | **In** — new BR-TP75 |

Note the pre-existing asymmetry this exposed: BR-045 explicitly excluded
`observability-service`'s copy of the KV-notify helper while saying nothing
about the shipping-service original the copy was made from. Both are now
addressed — the original is in, the observability copy stays out (it is that
service's own internal plumbing, not a domain event).

**Resolved:** all five are instrumented, and organizations-service gains a
sibling rule (BR-TP75) per the BR-036/BR-D39/BR-P25/BR-TP15 convention.

### A3 — The "never wrap a primitive" ban was over-broad

The ADR bans the hook from `Publish` / `PublishMsg` / `PublishWithTrace`
alike, on a self-observation-loop argument. That argument holds for the first
two and does not hold for the third:

- `PublishWithTrace` has exactly **three call sites in the entire backend**,
  and all three are `evt.*` publishes — one per service. It is not a generic
  primitive; it is already the `evt.*` seam.
- `obs.pubsub.*` is emitted over core `nc.Publish`, not through
  `jstream.Publisher`, so instrumenting the seam cannot observe its own
  emission.

Banning that seam is also what made this ADR's own headline "Harder"
consequence — that every future call site must remember to wire the hook, and
that this "needs to be a checked convention, not tribal knowledge" —
impossible to discharge structurally.

**Resolved:** the rule splits by family.

- **`evt.*` is instrumented *in* the seam**: `PublishWithTrace`
  (shipping, refdata) and `JetStreamEventStore.append`
  (organizations-service). Coverage becomes structural — a new `evt.*`
  publisher is observed by construction, and one test asserts the invariant.
- **`notify.*` keeps per-call-site wiring**, because it genuinely has no
  seam — its publishes are scattered bare `nc.Publish` calls across five
  files.
- The ban stands, unchanged, for `Publish` / `PublishMsg` and for any future
  generic primitive.

### A4 — Coverage enforcement is now a designed mechanism, not an aspiration

A3 discharges the `evt.*` half by construction. For `notify.*`, a
source-scanning test asserts that every `notify.` publish literal in the tree
is either on the instrumented list or on a documented exclusion list, so a new
uninstrumented publisher fails CI rather than going silently invisible.
Carried by new **BR-049**.

### A5 — Retention and volume

- The `obs.pubsub.>` stream is **its own stream**, not a second subject set on
  `TRACES`: sharing would let an event burst evict RPC traces, which are the
  more expensive signal to lose. Named per the repo's storage-naming rule
  (`SCREAMING_SNAKE`).
- Inheriting `TRACES`'s 1 hour / 64 MiB unexamined is not safe. At roughly
  2 KiB an envelope, 64 MiB is on the order of 32k messages — under load that
  is minutes of history, not the hour the `MaxAge` advertises. Caps are sized
  from a measured seed run at implementation time, and the measurement is part
  of 43b rather than a follow-up.

### A6 — Dedup needs a mechanism, not only a rule

BR-047 required dedup "by span/message id," but JetStream dedup requires a
`Nats-Msg-Id` header plus an explicit `Duplicates` window on the stream, and
`natstrace`'s emit today is a bare `nc.Publish` with no headers. As written
the rule was unenforceable.

**Resolved:** 43a sets `Nats-Msg-Id` to the envelope's `spanId`; 43b sets the
stream's `Duplicates` window explicitly rather than relying on the 2-minute
default. This is the same ground Phase 101 covers for domain publishes — the
two should land on the same convention.

### A7 — Failure semantics on a command path

The `evt.*` seam sits inside a command that already awaits a synchronous
`PublishMsg` PubAck. Observation must never fail or measurably delay the
domain publish: the emit is fire-and-forget, its error dropped, matching
`natstrace`'s existing behavior.

The honest consequence is that core-NATS publish is lossy under a slow
consumer or a reconnect, so BR-047's "every published envelope becomes
visible" is **best-effort, not guaranteed** — and is now worded that way. A
Messages panel that silently drops entries under load while implying
completeness would be worse than one that admits the bound.

### A8 — Redaction review, scoped

BR-046 deferred a payload review to implementation without naming the
payloads. The one that matters is **organizations-service's
transporter-profile events**: after Phases 40/41 those carry compliance
documents and sit beside an `organizations-secrets` bucket. That is where an
RPC-shaped denylist is most likely to be wrong — not in ship/container events.
The review runs **before** 43a's hook is wired, not during.

### A9 — Panel behavior under volume

`evt.*`/`notify.*` volume exceeds RPC volume, and `notify.*` is largely a
fan-out of events already visible on the `evt` side — so an undifferentiated
feed would be dominated by low-information rows. The panel therefore needs a
client-side row cap and a pause control (reusing `RpcPanel`'s), and the family
filter defaults to `evt` only. Carried by BR-048.

### A10 — Sequencing

43a alone produces nothing visible and 43b without 43c is invisible, so the
first real feedback would otherwise arrive very late — after the export shape
is already committed. Implementation takes a **thin vertical slice first**:
`evt.*` in shipping only, minimal stream, minimal panel, which puts A1's
tenant-provenance decision on screen on day one where it is cheapest to
correct. The remaining call sites and the `notify.*` family follow once the
slice holds.

### Revised action items

1. [x] Business-rules-first pass — BR-045–049 (shipping), BR-D45 (refdata),
       BR-AC34 (accounts), BR-TP75 (organizations) drafted and amended per
       this review. Still PROPOSED, awaiting confirmation.
2. [x] Redaction review of real `evt.*`/`notify.*` payloads (A8) —
       completed 2026-08-25, before 43a's hook was wired. Two fields added
       to the shared denylist (`actorName`, `actorSourceIP`); recorded in
       full in BR-046.
3. [x] Derive specs from those rules (red → green → refactor) — done for
       the slice below; the widening step's specs are still the pending
       stubs.
4. [x] Implement as a thin vertical slice first (A10), then widen. Both
       halves landed 2026-08-25. **Slice:** `natstrace.ObservePublish`/`ObservePublishAs`
       (the envelope, its subject derivation, trace continuation,
       `Nats-Msg-Id` = `spanId`, redact-then-truncate, the
       self-observation guard), the `evt.*` seam in shipping only
       (`Publisher.EnableObservation` + `PublishWithTrace`), and A1's
       `obs.pubsub.>` export + `monitor.{tenant}.pubsub.>` import remap.
       **Widening (same day):** all five of shipping's `notify.*` call
       sites, the accounts channel move off `obs.trace.*`, refdata's and
       organizations' `evt.*` seams, and BR-049's `go/ast` coverage scan.
       Three things the widening settled that the design had not: the
       accounts channel move made the outbound span it used to publish
       redundant (it now continues the causing request's span instead of
       minting a hop nothing receives); refdata's `notify.*` observation
       belongs in the per-tenant fan-out rather than the bridge, since only
       the fan-out holds a tenant connection to make the envelope
       attributable — A1's provenance argument again, on the emit side; and
       `pubsubstore.publishNotify` had to join the exclusion list, because
       observing a notify about the obs.pubsub bucket feeds that bucket
       from itself without bound. **43b landed 2026-08-25** — `pubsubstore`, its own `PUBSUB`
       stream over both subject sets, the `pubsub-messages` bucket, and
       measured caps (A5/A6/A7). Two corrections it produced: a real
       envelope is 454–592 B, not the ~2 KiB A5 assumed, and the stream
       must capture `monitor.*.pubsub.>` as well as `obs.pubsub.>` or it
       sees no tenant traffic at all. 43c is what puts any of it on screen.
5. [x] A1's deferred `obs.trace.>` remap — **closed 2026-08-26 in Phase
       48**; see the resolution note under A1 above for what it changed and
       what it did not anticipate. `trace-request-reply`, unbounded at the
       time this ADR was written, was bounded in the same phase on measured
       numbers (BR-053).
6. [x] Panel description moved into
       [ARCHITECTURE-ADMIN.md](ARCHITECTURE-ADMIN.md) §4.9 alongside its
       siblings (2026-08-25, with 43c); this ADR remains behind as the
       historical decision record. **43c landed 2026-08-25** —
       `MessagesPanel.vue` on its own `pubsub` nav entry, fed by
       `usePubsubFeed.js`, with the tenant named per row from the import
       remap, an `evt`-default family filter, a 500-row cap, pause/resume,
       and an explicit best-effort disclaimer (A1/A9 discharged on screen).
