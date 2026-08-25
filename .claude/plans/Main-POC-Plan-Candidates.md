# nats-tech-lab — Dictionary POC Plan Candidates

Phases that are **not in flight**: candidate, proposed, deferred,
placeholder, or approved-but-implementation-on-hold. Split out of the live
plan (`Main-POC-Plan.md`) on 2026-08-21 so that file holds only what is
actively being worked or is next up.

This file is a reference — it is not meant to be read into context by
default; open it when picking up, re-scoping, or reviewing one of these
phases. The live plan keeps a one-line entry per phase below under
"Candidate, deferred, and on-hold phases", linking here.

**Moving a phase between files:**

- **Here → live plan**, in full, when work actually starts on it (or its
  design gate is approved *and* it is next up).
- **Live plan → here**, in full, when a phase is deferred or put on hold
  before being implemented.
- **Live plan → `Main-POC-Plan-ARCHIVE.md`** when it is *completed* — a
  phase never reaches the archive from this file without passing through
  the live plan first. Unlike the archive, this file is **not**
  append-only: entries here are expected to be edited, re-scoped, and
  removed.

Numbering: candidates occupy the 100+ block by convention, but a phase
deferred out of the live plan keeps its original number (Phases 63 and 67
below) rather than being renumbered into the 100s — renumbering costs
cross-reference trails in docs, commits, and business rules for no gain.

---

## Deferred / on-hold (original numbering retained)

### Phase 63 (following on from Phase 29, then Phase 41, then Phase 36, then Phase 43; DEFERRED 2026-08-18 — design approved, implementation on hold) — NATS 2.11 Server-Hop Tracing ("Trace this subject")

> **Renumbered 2026-08-17** from Phase 29 to Phase 41, alongside Phase
> 24 → Phase 40, when Phases 23/25/25i/26/27/28/30 were archived (see the
> "Renumbering (2026-08-17)" log in the archive's "Renumbering
> history" section). No
> internal references needed updating — this phase has none.

> **Renumbered again 2026-08-18** from Phase 41 to Phase 36 — the next
> available number after completed Phase 35, rather than sitting orphaned
> in the 40s block reserved for Phase 40/42 (see the "Renumbering
> (2026-08-18)" log in the archive's "Renumbering
> history" section). Cross-references in
> `ARCHITECTURE-COMMUNICATIONS.md` and `ARCHITECTURE-ADMIN.md` updated to
> match.

> **Renumbered a third time, 2026-08-18** from Phase 36 to Phase 43, and
> moved down here past Phase 100+ into deferred status, since the phase was
> deferred for further research rather than moved into implementation (see
> the "Renumbering (2026-08-18b)" log in the archive's "Renumbering
> history" section). The
> design stays **approved as-is**; only the number and status changed.
> Phase 107 (candidate, "Re-fire a Captured Trace") still names this phase
> by its old number in its own heading — see that phase's entry for the
> cross-reference note.

> **Renumbered a fourth time, 2026-08-20b** from Phase 43 to **Phase 63**,
> when the whole 40–49 block was shifted to 60–69 (see the "Renumbering
> (2026-08-20b)" log in the archive's "Renumbering
> history" section). Status is unchanged
> (DEFERRED, design approved). The `phase43-*.png` diagram filenames under
> `images/` keep their old number — renaming assets and their references
> would be purely cosmetic.

> **Status: DEFERRED, design approved.** The spike below fully validated a
> design (see "Spike findings" and "Design decisions"), and BR-042 in
> `BUSINESS_RULES-SHIPPING.md` is drafted to match it. Implementation is on
> hold pending further research — no code has been written yet. A
> before/after diagram summarizing what the spike changed from the original
> proposal is saved at
> `obsidian/V3-Platform/Architecture/Dictionary-POC/images/phase43-trace-this-subject-before-after.png`
> ("Trace this subject" — before and after the spike) — read that first when
> this phase is picked back up, before re-deriving the design from the prose
> below.

#### Goal

Phase 28 answers "shipping called refdata and it took 40ms." It cannot answer
"the message was dropped at the account import boundary" — which, in an
operator-mode deployment where every cross-service call goes through a JWT
export/import, is the failure mode that is hardest to diagnose and produces
the least evidence.

NATS 2.11's distributed message tracing reports, per server hop: ingress
(`in`), egress (`eg`), subject mapping (`sm`), stream export (`se`), service
import (`si`), and JetStream store (`js`) — each with the server's own error
string. Add a "Trace this subject" control that publishes with
`Nats-Trace-Dest` and renders the returned hop tree, interleaved into the same
waterfall as Phase 28's application spans.

**Scoped to the ad-hoc probe shape only.** Fire a probe with no prior request
required, and get back a standalone trace row containing only hop ticks.
Re-firing an *already captured* trace in place (merging hop ticks into that
trace's existing waterfall row instead of creating a new one) is deliberately
deferred to Phase 107, since it needs a stored-payload-replay path this phase
doesn't otherwise require.

**Probe target is enumerated, not free-typed** (revised after the spike
below) — see Design decisions.

#### Spike findings (2026-08-18, against the live compose stack)

The original design assumed `observability-service` could itself publish an
arbitrary business subject with `Nats-Trace-Dest` set, using the
`obs.trace.>` `AllowTrace` grant BR-AC30 already wires. Four things, checked
live against the running stack (`nats trace`/`nats pub`/`nats request`
directly against `lb-nats`), each corrected the previous assumption:

1. **`observability-service` cannot publish to any business subject at
   all.** Its NATS user (`bootstrap-operator.sh:389`) is narrowly scoped to
   `monitor.>`/`$SRV.>`/specific `$JS.API.*` — confirmed live: `nats pub
   --creds observability.creds rpc.acme.refdata.item.get.v1` returns
   `Permissions Violation for Publish`, while the same publish from
   `acme.creds` succeeds immediately.
2. **Most business subjects never cross an account boundary at all.**
   `natstenants.Manager` (`shared/natstenants/tenants.go:292`) gives
   `refdata-service`, `pricing-service`, and `organizations-service` one
   direct connection *per tenant*, authenticated straight into that
   tenant's own account — so an ordinary intra-tenant call has no
   export/import in its path, and no permission grant would ever produce a
   real `si`/`se` hop for it.
3. **One real crossing already exists, independent of BR-AC30, and works
   today:** `accounts-service`'s `tenantImports()`/`tenantExports()`
   (`provisioner.go:207-246`) wires each tenant to import 4 refdata RPCs
   (aliased locally as `refdata.item.get.v1`, `refdata.type.list.v1`,
   `refdata.item.get-versioned.v1`, `refdata.locales.list.v1`, forwarding to
   `rpc.{tenant}.refdata.*` in PLATFORM, where `refdata-service`'s real
   `micro`-registered responder actually lives) plus 2 stream imports
   (`evt.*.refdata.*.changed`, `notify.accounts.account.*`). Confirmed live:
   `nats request --creds acme.creds refdata.item.get.v1 '{}'` gets a real
   reply from `refdata-service`; the literal subject
   `rpc.acme.refdata.item.get.v1` gets "No responders" when tried directly
   from `acme.creds` (it only resolves via the import's remap). **This is
   the only real cross-account crossing in the whole system for business
   traffic** — so it's what the probe has to target, not an arbitrary
   subject.
4. **Nobody a browser action can reach can fire a probe on that crossing.**
   `MintAdminToken` (`auth/token.go:178`) denies all publish
   (`Pub.Deny.Add(">")`) — the Admin UI's own NATS connection is
   subscribe-only. And structurally, the tenant-local alias
   (`refdata.item.get.v1`) only resolves *inside* the importing tenant
   account — a PLATFORM-only connection like `observability-service`'s
   cannot address it by name at any permission level. The only connections
   that legitimately hold publish rights on it today are `shipping-service`
   and `organizations-service`'s own per-tenant connections (the real
   callers of refdata).
5. **The crossing itself traces correctly; final-delivery interest does
   not, and this is a NATS-server limitation, not ours.**
   `nats trace --creds acme.creds refdata.item.get.v1` reports the hop
   cleanly: `Service Import from:"refdata.item.get.v1"
   to:"rpc.acme.refdata.item.get.v1" account:"ADGEUWC..."` — satisfying the
   "report cross-account hops" requirement. But it then reports `No active
   interest`, even though `refdata-service` demonstably answers this exact
   call (`nats request` above got a real reply). Isolated the cause by
   varying one thing at a time: a plain literal (non-wildcard, non-queue)
   test subscriber placed on the far side still shows `No active interest`
   through the crossing, while tracing the *same* literal subject
   *same-account* (no crossing) correctly reports
   `--C Client "refdata-service" ... subject:"rpc.*.refdata.item.get.v1"
   queue:"q"` with an egress count. So neither the wildcard subscription
   nor the queue group is the cause — NATS 2.14.3's tracing interest-check
   simply never re-evaluates interest on the far side of a Service Import.
   This is systematic (100% reproducible), not probabilistic, and not
   fixable in this repo's code.

#### Design decisions (revised 2026-08-18, post-spike)

- **Probe target is the existing `tenantImports()`/`tenantExports()`
  contract, not an arbitrary typed subject.** It's the only place a real
  cross-account crossing exists in this system today (finding 2/3 above).
  "Trace this subject" becomes "trace one of these known cross-account
  operations" — a short enumerated list (the 4 refdata RPC aliases + 2
  stream imports), not a free-text subject field.
- **The probe is fired by the service that owns the real connection, not
  by `observability-service`.** `shipping-service` and
  `organizations-service` already hold the only connections with
  legitimate publish rights on this crossing (finding 4). Each gets a
  small internal diagnostic hook that fires *one of its own already-defined
  outbound calls* with `Nats-Trace-Dest`/`Nats-Trace-Only` set, reusing the
  exact connection it holds for real business reasons. No new NATS
  permission grant anywhere, on any account.
- **`observability-service` keeps the REST entry point and the
  storage/rendering role, but not the publish.** The browser still calls
  `POST /api/nats/trace` on `observability-service` (same reasoning as
  before: extends its existing system-topology-diagnostics REST carve-out,
  same category as `/api/jetstream/replay`, `POST` not `GET` since it has a
  real wire effect). `observability-service` forwards the request over an
  internal, same-account (PLATFORM→PLATFORM) call to whichever service owns
  the target operation — e.g. `shipping-service`'s own admin surface — asks
  it to fire the traced probe on its existing tenant-scoped connection, and
  gets the hop tree back to normalize/store/serve exactly as before
  (`kind: "hop"` spans merged into `tracestore`'s `traceRecord`, a fresh
  synthetic `traceId` per probe, destination subject inside the existing
  `obs.trace.>` family). This leg needs no new grant either — both
  `observability-service` and `shipping-service`'s admin connection already
  live in PLATFORM.
- **Final-delivery interest cannot be shown as fact, ever, for a
  cross-account hop — labeled, not fixed.** Confirmed as a systematic NATS
  2.14.3 tracing limitation (finding 5), not something this repo's code can
  correct. The waterfall renders the confirmed `si`/`se` hop normally, but
  any signal past that hop gets a distinct, hedged treatment (not
  red/failure) with a tooltip explaining that destination interest isn't
  reliably reported across a Service Import — never asserted as
  "dropped."
- **The new route joins BR-040/041's existing mux-allowlist enforcement,
  not a special case** — `POST /api/nats/trace` gets added to
  `observability-service`'s allowlisted route set and its
  `TestMountRoutesMatchAdminAllowlist`-equivalent test, same mechanism as
  every other diagnostics route.

- [ ] Backend (`shipping-service`): a small internal diagnostic hook (its
      own admin RPC/REST, PLATFORM-scoped) that fires one of
      `refdataconsumer`'s existing outbound calls with
      `Nats-Trace-Dest: obs.trace.hop.{traceId}` and `Nats-Trace-Only: true`
      by default, using its own already-live tenant-scoped connection, and
      returns the collected hop events.
- [ ] Backend (`observability-service`): `POST /api/nats/trace` — takes an
      enumerated target (one of the 4 refdata RPC aliases / 2 stream
      imports), forwards to the owning service's diagnostic hook, then
      normalizes the reply into `kind: "hop"` spans and appends to a new
      `traceRecord` via the existing `tracestore.appendSpan` path.
- [ ] Add the route to `observability-service`'s mux allowlist + allowlist
      test (BR-040/041 pattern).
- [ ] Frontend: a "Trace this subject" control offering the enumerated
      target list (not free text) calling the new REST route; render
      `kind: "hop"` spans as grey hairline ticks rather than duration bars
      (ARCHITECTURE-ADMIN.md §4.5's UI design); any signal past a
      cross-account hop renders hedged/unconfirmed, never as a failure.
- [x] Business rules: BR-042 revised in `BUSINESS_RULES-SHIPPING.md` for
      the corrected design — enumerated target set, `shipping-service`
      firing its own probe, and the documented interest-signal limitation.
- [ ] **Why this is worth its own phase:** zero code in `refdata-service`
      itself and no per-message cost; requires server 2.11+ (already
      running: `nats:2.14.3`). No longer needs `allow_trace`/BR-AC30 at all
      — that assumption didn't survive the spike (finding 3); the crossing
      this phase actually uses is `tenantImports()`'s existing contract.
- [ ] **The payoff for having chosen `traceparent` in Phase 28:** in
      trace-context mode the NATS server stamps *our* trace id onto its own hop
      events, so application spans and infrastructure hops land on one
      waterfall keyed identically. No off-the-shelf tool does this.

---


## Candidates (100+ block)

### Phase 100 (PROPOSED — awaiting approval) — Ship Container Capacity Limit

#### Goal

Ships currently have no maximum container capacity — a ship can be loaded with an unbounded number of containers. Add a fixed `Capacity` to the Ship aggregate and enforce it as a load-time domain rule (BR-019), plus surface a load-capacity indicator column in `frontend-port` ("SeaFreight Flow") so the constraint is visible, not just enforced.

> **Flagged 2026-08-17 (Phase 31).** This phase's design below still reasons
> about "Shape A/B" as two read models to keep in sync. Phase 31 retired
> Shape A (and Shape C) — there is now one shape (`queries.Ships`, the `ships`
> KV bucket). The trade-off in point 2 below ("event-replay count vs.
> read-model query") still applies, just against one read model instead of a
> choice between two; re-scoping this phase's design to the post-31
> vocabulary is deferred to implementation time, not done here.

#### Design

- **`Ship` domain model** (`dictionary/internal/domain/ship.go`): add `Capacity int` to `ShipState` (ship.go:46-53) and `ShipAggregate` (ship.go:65-70), threaded through `Apply()`/`State()`/`FromState()`.
- **Setting capacity**: no "register ship" command exists — a ship's first `Arrive` is its registration (`ShipAggregate.Arrive()`, ship.go:124-144), which already set-once's `ShipName` when empty. `Capacity` follows the same set-once-at-first-arrival pattern: `ArrivePort` request gains an optional `capacity` field; if omitted on first arrival, a documented default is used (exact default — e.g. 20 — confirmed at implementation time, not fixed by this plan entry). There is still no update-ship command, so capacity is immutable after first arrival unless a follow-up phase adds one.
- **Enforcing BR-019 on `Load`**: `ContainerAggregate.Load()` (container.go:196-219) gains a capacity check alongside its existing BR-012/BR-010/BR-014/BR-008 checks. This needs the ship's *current* on-ship container count at command time — `ContainerHandler.LoadContainer()` (application/commands/container.go:87-106) resolves this before calling `cont.Load(...)`. Two candidate mechanisms, to be decided during implementation:
  1. Event-replay count (consistent with "JetStream is the source of truth" — Working Assumptions): count `.loaded`-without-subsequent-`.unloaded` container events for the ship's `shipID` at hydrate time.
  2. Read-model query against the existing manifest join (Shape A/B projection) — faster, but reads an eventually-consistent projection to guard a write (same class of trade-off Phase 103 documents for BR-008/BR-012 read-model guards).
- **Read model / API surface**: `ShipState`'s KV (Shape A/B) and Postgres projections need the new `Capacity` field so `GET` endpoints (fleet, shape-b ship, shape-c fleet) return it to the frontend.
- **Frontend (`frontend-port`)**: `FleetPanel.vue` (columns at lines 112-131) and `ShipsAtPortPanel.vue` (columns at lines 150-163) each gain a load-capacity indicator column pairing the new `capacity` field with the container count already computed via `store.manifestFor(shipID).length` (e.g. `12 / 50`, colored by fullness). Route any new column label through `l10n` (BR-D16), not a hardcoded literal.

#### Checklist

- [ ] Confirm default capacity value and whether `capacity` is required or optional on `ArrivePort`
- [ ] Decide event-replay vs read-model-guard mechanism for the current-count check (document the trade-off, mirroring Phase 103's treatment of BR-008/BR-012)
- [ ] `ShipState`/`ShipAggregate`: add `Capacity`, thread through `Apply()`/`State()`/`FromState()`
- [ ] `ArrivePort` command + REST handler: accept optional `capacity`, set-once on first arrival
- [ ] `ContainerAggregate.Load()`: new `ErrCapacityExceeded` check (BR-019)
- [ ] `ContainerHandler.LoadContainer()`: resolve current on-ship count before calling `Load()`
- [ ] KV (Shape A/B) + Postgres ship projections: persist and return `Capacity`
- [ ] Ginkgo specs written **before** implementation (red → green): `Container Domain Rules / BR-019` — load rejected at capacity, allowed under capacity, allowed exactly at capacity-minus-one
- [ ] `frontend-port`: load-capacity column in `FleetPanel.vue` and `ShipsAtPortPanel.vue`, via `l10n`
- [ ] `BUSINESS_RULES.md`: BR-019 updated from PROPOSED to enforced, with final error/enforcement/test references
- [ ] `go build ./...` + `ginkgo ./...` green; frontend build green


### Phase 101 — Write-Side Safety (Optimistic Concurrency + Publish Dedup)

#### Goal

Close the two producer-side correctness gaps that stand between "JetStream as event log" and "JetStream as trustworthy event store":

1. **Blind publish → lost invariants under concurrency.** Command handlers hydrate-validate-publish with no guard between read and write. Two concurrent commands on the same aggregate both hydrate the same pre-state, both pass validation, both publish — producing events that are individually valid but jointly violate a business rule (e.g. the same container loaded onto two ships).
2. **No publish dedup → client retries double-write the source of truth.** An HTTP client retrying a command after a timed-out response durably appends the business event twice. In transport-mode this would be caught downstream by Postgres constraints; in event-store mode the duplicate *is* the record.

#### Design

- **Optimistic concurrency**: `hydrate()` already walks the aggregate's events — it additionally returns the last stream sequence seen. Publish carries `Nats-Expected-Last-Subject-Sequence`; if another event landed in between, the server rejects the append (err 10071), and the handler re-hydrates, re-validates, and retries (bounded).
  - ⚠️ **Verify against current NATS docs before implementing**: an aggregate's events span multiple subjects (`…{id}.arrived` vs `…{id}.departed`), and the plain header checks the last sequence *of the published subject only*. Newer servers support `Nats-Expected-Last-Subject-Sequence-Subject` to guard against a wildcard filter (`…{id}.>`). Confirm server + nats.go client support; if unavailable, fall back to a single per-aggregate subject with the event type in the payload/headers, and document the trade-off.
- **Publish dedup**: every publish sets `Nats-Msg-Id` derived from a command idempotency key (client-supplied header, generated by the frontend per user action). Configure the stream's `Duplicates` window **explicitly** (don't rely on the 2-minute default silently).
- The `Publisher` port grows an options parameter (expected sequence, message ID) — kept transport-agnostic in signature so the interface doesn't leak `jetstream` types into `application/`.

#### Checklist

- [ ] Verify `Nats-Expected-Last-Subject-Sequence[-Subject]` semantics and `Duplicates` window behavior against current NATS server / nats.go docs (features move between releases)
- [ ] `hydrate()` / `hydratePair()` return the last relevant stream sequence
- [ ] `Publisher` port + `jstream` adapter: publish options (expected last sequence, msg ID)
- [ ] Command handlers: guard publishes, bounded retry-on-conflict (re-hydrate → re-validate → re-publish)
- [ ] `Nats-Msg-Id` on every publish; explicit stream `Duplicates` window in `CreateStream`
- [ ] REST: accept/generate a command idempotency key per request
- [ ] Ginkgo specs: concurrent conflicting commands — exactly one wins, loser re-validates (double-load race rejected); duplicate publish with same msg ID appends once
- [ ] `BUSINESS_RULES.md`: document the concurrency guarantee the event store now provides
- [ ] `go build ./...` + `ginkgo ./...` green


### Phase 102 — Projection Hardening (Consumer-Side Idempotency + Explicit Limits)

#### Goal

Make projections safe under redelivery and reordering **by engineering, not by accident**. Today's safety rests on "redelivering the same event re-applies the same upsert" — true only if delivery order is preserved, which depends on unexamined consumer defaults. Also make the stream's "never discard" property an explicit decision rather than an implicit absence of limits.

#### Design

- **KV writes**: replace naive `Put` with a guarded write — the stored value carries the source event's stream sequence; the projector skips any event older than what's stored, using `Update` with expected revision (CAS loop) so a stale redelivery can never clobber newer state.
- **Postgres projection**: same guard — persist the last-applied stream sequence per row and skip older events in the upsert (`WHERE excluded.seq > current.seq` style).
- **Consumer ordering**: verify `Consume()` callback concurrency and `MaxAckPending` defaults against current nats.go docs (do not assume); set `MaxAckPending` explicitly per projector and document the ordering guarantee relied upon.
- **Explicit retention decision**: `CreateStream` currently sets no `MaxAge`/`MaxMsgs`/`MaxBytes` — "never discard" is true only implicitly. Make it explicit: document unbounded-is-deliberate in the config (or set `DiscardPolicy` intentionally), so the config can't be copied forward with the decision invisible.
- **Poison messages**: current behavior (ack-on-unmarshal-failure to avoid redelivery loops) is documented; consider a dead-letter subject instead of silently acking — shaped per the § Phase 16 taxonomy (a fixed literal family token first, `{context}` for company/business unit; **not** `{region}`/`{tenant}`, neither of which belongs in a subject).

#### Checklist

- [ ] Verify `Consume()` ordering / `MaxAckPending` semantics against current nats.go docs
- [ ] `kvstore.Store`: guarded write API (sequence-aware CAS); all projector call sites migrated off naive `Put`
- [ ] Postgres projectors: last-applied-sequence guard in upserts
- [ ] Explicit `MaxAckPending` on all projector consumers
- [ ] `CreateStream`: retention/discard decision made explicit in code comment + config
- [ ] Poison-message policy: dead-letter subject or documented ack-and-log, decided and implemented
- [ ] Ginkgo specs: out-of-order redelivery does not clobber newer KV/Postgres state; duplicate redelivery is a no-op
- [ ] `go build ./...` + `ginkgo ./...` green

### Phase 103 — Stream Split + Cross-Aggregate Consistency

> **Flagged 2026-08-17 (Phase 31).** Option 1 below ("read-model guard")
> reasons about "the ship's KV projection (Shape A/B)" as a choice between
> two read models. Phase 31 retired Shape A (and Shape C) — there is now one
> KV projection (`queries.Ships`, the `ships` bucket) backing that guard.
> The stale-read trade-off this phase measures is unchanged; re-scoping the
> wording to the post-31 vocabulary is deferred to implementation time.

#### Goal

Extract container events from the shared `SHIPPING` stream into a dedicated `TERMINAL` stream, turning the two aggregates into two independent bounded contexts. This is a **single-variable change** on top of Phases 8–14: the aggregates, rules, and frontends are unchanged — only the stream topology moves. Post-Phase 9 this is even cleaner than originally planned: **the subjects themselves do not change** — a subject can belong to only one stream, so the split is purely moving the `…container.>` binding from `SHIPPING` to `TERMINAL`. The purpose is to make the **invariant-spanning-two-aggregates problem** concrete and demonstrate the solution options.

#### The problem this phase exposes

After the split, BR-008 (container destPort vs ship's current port) and BR-012 (ship must be docked) still need **both** aggregates' state — but the container command handler can no longer get the ship's state from the same replay. `ContainerAggregate` hydrates from `TERMINAL`; the ship's docked state lives in `SHIPPING`. There is no atomic cross-stream replay.

| Stream | Subject binding | Bounded context |
|---|---|---|
| `SHIPPING` | `evt.{context}.shipping.ship.>` | Ship movements |
| `TERMINAL` | `evt.{context}.shipping.container.>` | Container lifecycle |

#### Solution options to implement and document

The demo implements **option 1** as the default and documents the trade-offs of all three:

1. **Read-model guard (default)** — the container handler reads the ship's KV projection (Shape A/B) to check docked state / current port. Fast and keeps the streams independent, but validates a write against an eventually-consistent read (stale-read window — which Phase 103 measures under load).
2. **Hydrate both streams** — the container handler additionally replays `SHIPPING` for the ship. Strongly consistent, but the container context is no longer independent and every load/unload replays two streams.
3. **Saga / compensating event** — accept the write optimistically and emit a compensating `container.load-rejected` event if the ship turns out not to be docked. The "correct" DDD answer for separate contexts; heaviest to implement.

#### Checklist

- [ ] `internal/jstream/stream.go` — add the `TERMINAL` stream binding `evt.{context}.shipping.container.>`; `SHIPPING` keeps only `…ship.>` (subjects themselves unchanged post-Phase 12.8)
- [ ] `domain/events.go` — route container subject builders / stream-name references to `TERMINAL`
- [ ] `application/commands/container.go` — hydrate containers from `TERMINAL`; replace the in-replay ship check with the **read-model guard** (option 1) for BR-008 / BR-012
- [ ] `eventhandler/` — container projector consumes from `TERMINAL`; ship projector unchanged on `SHIPPING`
- [ ] Ginkgo specs — BR-008 / BR-012 still green via the read-model guard; add a spec documenting the stale-read window (guard sees pre-departure state)
- [ ] Frontend (`frontend/`): JetStream panel stream selector — add `TERMINAL` entry (`streamOptions`); backend `streamJetStream` switch — add `TERMINAL` case
- [ ] Frontend (`frontend/seafreight-app/`): extend the existing `notify.*` NATS WebSocket subscriptions to cover the new `TERMINAL` stream's container events (this app no longer uses SSE — Phase 15d; the directory was also renamed from `frontend-port/`)
- [ ] `ARCHITECTURE.md` — document the two-stream topology, the cross-aggregate invariant problem, and the three solution options with the chosen default
- [ ] `go build ./...` + `ginkgo ./...` green


### Phase 104 — Performance & Load Testing (full suite)

> **Flagged 2026-08-17 (Phase 31) — this phase's Shape C scope is now moot,
> not just stale wording.** Phase 31 retired Shape C along with its
> `GET /api/shape-c/fleet` endpoint and `perf/scenarios/shape-c-reconstruction.js`
> harness — there is nothing left to re-measure for the "Shape C — full
> replay on every call" gap this phase's Goal names, or for the "Shape C
> fleet reconstruction under load" scenario and "Shape C reconstruction time"
> baseline metric below. Phase 10's baseline #1 numbers remain the
> historical record (see `PERFORMANCE.md`'s Phase 31 note). The write-side
> hydration gap (point 2 below) is unaffected and still needs measuring here.
> Re-scoping this phase to drop the Shape C scenario (or replace it with
> something else worth measuring) is deferred to implementation time, not
> done here — Phase 103's "Shape A/B" wording in the projection-lag row below
> has the same lighter staleness as Phases 100/103.

#### Goal

Validate that the *final* architecture holds under realistic throughput and identify the bottlenecks before any production consideration, building on the baseline established in **Phase 10**. Runs after the write path (Phase 101) and stream split (Phase 103) are in place, so the scenarios those phases gate can finally be measured. The POC has two known scalability gaps — first characterised in Phase 10, re-measured here against the final architecture:

1. **Shape C — full replay on every call.** `ReconstructFleet` replays from `seq=1` every time. Latency grows linearly with stream depth.
2. **Write-side hydration — full replay per command.** `hydrate()` in `commands.go` replays all events for a ship on every command. A busy ship accumulates history and slows its own writes.

Both are correct implementations of event sourcing fundamentals — the point is to *measure* the degradation curve and document where snapshots or other mitigations become necessary.

> The baseline harness and the Shape C / single-ship / throughput scenarios are delivered in **Phase 10** (pull-forward baseline). This phase reuses that harness, adds the scenarios gated by Phases 14 and 16, and re-measures the Phase 10 baselines against the final architecture.

#### Tool

**k6** (`k6.io`) — scripted load testing in JavaScript, runs outside the Go stack, produces latency percentiles and throughput metrics. Alternatively `vegeta` for simpler HTTP load.

#### Test scenarios

| Scenario | What it measures | Status |
|---|---|---|
| High-frequency arrivals/departures — single ship | Write-side hydration degradation as event count grows | baseline in Phase 10; re-measure |
| High-frequency arrivals/departures — many ships concurrently | Throughput ceiling of the command pipeline | baseline in Phase 10; re-measure |
| Shape C fleet reconstruction under load | Replay latency vs stream depth; degradation curve | baseline in Phase 10; re-measure |
| KV watch fan-out — many SSE clients | How many concurrent SSE connections the backend sustains before lag | this phase |
| Container load/unload burst — terminal throughput | Cross-stream (`SHIPPING` + `TERMINAL`) consumer lag under write pressure | needs Phase 103 |
| Projection lag — event published → KV updated | End-to-end latency of the Shape A/B projectors under load | this phase |
| Optimistic-concurrency contention — concurrent commands, same aggregate | Retry rate and latency cost of the Phase 101 sequence guard under contention | needs Phase 101 |

#### Baseline metrics to capture

- p50 / p95 / p99 command latency (arrive, depart, load container, unload container)
- Shape C reconstruction time at 100 / 1k / 10k events in stream
- KV watch SSE lag (time from KV write to browser event) at 1 / 10 / 100 concurrent clients
- Max sustained commands/sec before errors or queue buildup

#### Expected findings to investigate

- Shape C becomes unusable beyond a few thousand events without snapshotting
- `hydrate()` degrades for ships with long histories — snapshot checkpoint needed
- SSE fan-out has a practical client ceiling determined by goroutine count and NATS consumer throughput

#### Checklist

The baseline harness, seed script, and the Shape C / single-ship / throughput scenarios are delivered in **Phase 10**. This phase completes the remaining (gated) scenarios and finalises the report:

- [ ] Scenario: optimistic-concurrency contention — retry rate and latency cost of the Phase 101 sequence guard *(needs Phase 101)*
- [ ] Scenario: cross-stream burst — fire `SHIPPING` and `TERMINAL` events concurrently, measure projection consumer lag *(needs Phase 103)*
- [ ] Scenario: SSE fan-out — open 1 / 10 / 50 / 100 concurrent SSE clients, measure KV watch lag
- [ ] Scenario: projection lag — event published → KV updated, measured under load
- [ ] Re-measure the Phase 10 baseline scenarios against the final architecture (with guard + split) and record the before/after delta
- [ ] Finalise `demos/01-dictionary/PERFORMANCE.md` — full baseline numbers, degradation curves, identified thresholds
- [ ] Document architectural mitigations for each bottleneck (snapshot strategy, consumer parallelism, SSE load balancing)


### Phase 105 (optional, PLACEHOLDER — not yet a formal requirement) — Per-Tenant Runtime Theme Spike

#### Goal

Explore whether UI theme/branding (colors, tokens, light/dark presets) can be externalized per tenant and swapped **at runtime**, without a separate build/deploy per tenant. Raised as a "does it make sense to put theme data in the dictionary service" question (2026-07-17) — not a formal requirement yet, so this is scoped as a spike to prove the mechanism out, not a commitment to build it.

#### Why this isn't just another `l10n`-style refdata type

Theme data is fetch-then-apply's worst case: `l10n`/label fallback (BR-D11) and cold-paint caching (BR-D19) tolerate a brief English-text mismatch on first paint, but a full-page flash of the *wrong tenant's brand colors* before a client-side fetch resolves is far more visible and jarring — the same class of problem, magnified. Client-side fetch-and-apply (the pattern used everywhere else in this repo) is therefore the wrong default here.

#### Scope (spike, not production-ready)

- Dictionary service remains the source of truth for each tenant's theme tokens (a new `theme` dictionary type, context-scoped like everything else), but resolution is **not** a browser-side fetch-after-mount.
- Prove out server-side/edge injection instead: a lightweight step (nginx, a tiny Go handler, or an SSR shell) resolves the tenant (subdomain/host header/path) and injects that tenant's CSS custom properties into `index.html` **before** it reaches the browser, so first paint is already correct — no flash, no fallback banner needed.
- Note but don't implement: full SSR, a CDN/edge-cache layer for resolved theme HTML, and live theme-change propagation to already-open tabs (out of scope for a spike).

#### Checklist

- [ ] Confirm this is still wanted as a real requirement before scoping further (currently a placeholder)
- [ ] `theme` dictionary type: define token schema (a small fixed set of CSS custom properties, not an open-ended style system)
- [ ] Spike: a request-time injection step (nginx `sub_filter`, or a minimal Go handler in front of the static build) that resolves tenant → theme tokens → injects into the served `index.html`
- [ ] Verify no flash-of-wrong-theme on first load for a tenant the browser has never seen (the actual test this spike exists to pass)
- [ ] Document the trade-off vs. compiled-in-at-build-time in `ARCHITECTURE.md`: when per-tenant runtime branding is worth the added deploy-topology complexity vs. just rebuilding per tenant

### Phase 106 (DEFERRED from Phase 22b, 2026-08-13) — Context Inheritance on the Live Read Path

#### Goal

Make live reference-data reads honour the context `parent` chain, closing the gap between what the context hierarchy *implies* and what it actually does.

#### The gap

refdata-service has two parallel read paths, and only one inherits:

- **Corpus / versioned path — inherits.** `CorpusRepository.CreateDraft` walks the ancestor chain with a recursive CTE and flattens each ancestor's locally-authored rows via `domain.FlattenCorpus` / `FlattenLocalizations`, writing resolved rows with `source_context` + `is_override`. `inheritance.go`'s header states the intent plainly: the flattened form exists so reads never traverse a chain.
- **Live path — does not.** Every query in `item_repository.go`, `localization_repository.go`, `locale_repository.go` and `reference_repository.go` is a flat `WHERE context = $1`. No CTE, no UNION, no IN-list. `kvcache.Projector.rebuildEntry` builds the `refdata-{context}` bucket from those same exact-match queries, so the live KV cache doesn't inherit either. `Ancestors()` exists but is consumed only by the admin detail endpoint and by `Register`'s cycle check.

The consequence is that a context registered with a parent looks correct in the admin UI's context tree and returns nothing through `rpc.{context}.refdata.item.get.v1`, `type.list.v1`, the REST list/get routes, or the `refdata-{context}` bucket — while `item.get-versioned.v1` returns the fully inherited set. Phase 22b makes this more visible by giving every tenant a parented default context, but does not introduce it.

#### Scope

- [ ] Decide the mechanism: recursive-CTE resolution in the repositories (correct, touches every read query) vs. materialising inherited rows into the child context on write (simpler, duplicates data, drifts when an ancestor changes)
- [ ] `dictionary_locales` is on the flat path and is **not** covered by corpus flattening — whichever mechanism is chosen must cover locales, or `EffectiveDefaultLocale` still resolves against an empty set
- [ ] `kvcache.Projector.rebuildEntry` must project inherited entries into `refdata-{context}`, or readers must fall back to an ancestor's bucket — pick one; a KV cache that disagrees with Postgres is worse than no inheritance
- [ ] Override semantics on the live path must match the corpus path's `is_override` precedence (child wins, nearest ancestor next) so the two paths cannot disagree about the same item
- [ ] Decide whether `Ancestors()` and `ancestorChainTx` (currently duplicated between `context_repository.go` and `corpus_repository.go`) collapse into one implementation as part of this
- [ ] Business rules for live-path inheritance and override precedence; specs covering a child with no local rows, a child overriding one ancestor row, and a three-level chain

---

### Phase 107 (candidate, deferred from Phase 36's design gate, 2026-08-18) — Re-fire a Captured Trace with Server-Hop Tracing

> **Note (2026-08-18b):** Phase 36 was itself renumbered to **Phase 43** the
> same day, after this phase’s heading was written (see that phase’s entry
> and the "Renumbering (2026-08-18b)" log). References to "Phase 36" below
> mean that same phase.

> **Note (2026-08-20b):** that phase was renumbered again from Phase 43 to
> **Phase 63** when the 40–49 block was shifted to 60–69 (see the
> "Renumbering (2026-08-20b)" log in the archive's "Renumbering
> history" section). It is
> also DEFERRED — this phase remains a candidate either way, since it was
> never scheduled ahead of Phase 63’s own implementation. The heading above
> still names Phase 36 as the design gate this was deferred from; that is
> the historically accurate number at the time and is left as-is.

#### Goal

Phase 63 ships the ad-hoc shape of "Trace this subject": pick any subject
cold and see the physical hop path it would take. This phase adds the
complementary shape — select an *already-captured* trace row in the Phase 28
waterfall and re-fire a copy of its real payload with `Nats-Trace-Dest`/
`Nats-Trace-Only`, merging the resulting hop ticks into that same row instead
of creating a new one. Answers "what path did this specific call already
take?" rather than "what path would a call to this subject take?"

#### Scope

- [ ] Store (or look up from tracestore/KV) the original request payload for
      a captured span, keyed by traceId, so it can be replayed
- [ ] Re-publish that payload tagged with the *original* traceId (not a
      fresh one) so hop events append into the existing `traceRecord`
      instead of starting a new row
- [ ] Decide whether `Nats-Trace-Only` can ever be turned off for a re-fire
      (i.e. an intentional real replay, not just a dry-run) — out of scope
      for Phase 63's ad-hoc probe, which has no captured payload to safely
      replay in the first place
- [ ] Same REST-route/allowlist/business-rule treatment as Phase 63, as an
      addition to the route it introduces rather than a new one

---

### Phase 108 (candidate, deferred from Phase 43's design gate, 2026-08-20) — Live Account Activity Panel via `$SYS` Account-Monitoring Exports

#### Goal

Phase 43 (ADR-047,
[ARCHITECTURE-OBSERVABILITY.md](../../obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE-OBSERVABILITY.md))
deliberately deferred Option B out of its own scope: importing the
`SYS` account's already-present, currently-unused
`account-monitoring-streams`/`account-monitoring-services` exports
(`$SYS.ACCOUNT.*.>` / `$SYS.REQ.ACCOUNT.*.*`) to give the Admin UI a live,
cross-account connection/subscription/rate feed — complementary to Phase
67's payload-level Messages panel, not a replacement for it (this stays
metadata-only, no message content). Confirmed as a real follow-on, not
just a maybe.

#### Design decisions (partial — full design still needed before this leaves PROPOSED)

- Import `SYS`'s existing monitoring exports into `observability-service`'s
  PLATFORM connection — no new export needs minting, they're already in the
  `SYS` account JWT, just unimported.
- **Confirmed requirement: the UI needs an account filter.** `$SYS.ACCOUNT.*.>`
  fires for every account at once, so a live feed with no way to narrow to
  one (or a small set of) accounts would read as an undifferentiated
  cross-tenant firehose — the same reasoning behind Phase 43's `evt`/
  `notify` family filter, applied here to account instead of subject
  family.
- Relationship to the existing (poll-only) Account Activity panel
  (`AccountsOverviewPanel.vue`, Phase 45, `GET /api/nats/account-activity`)
  not yet decided: replace it with a live feed, or add this as a second,
  genuinely-live view alongside the existing poll — needs its own design
  pass.

#### Scope (draft — not yet a committed checklist)

- [ ] Import `account-monitoring-streams`/`-services` into
      `observability-service`'s PLATFORM connection
- [ ] Backend: consume `$SYS.ACCOUNT.*.>`, decide storage shape (KV keyed
      by account? in-memory ring buffer like `AccstatzHistory`?)
- [ ] Admin UI: account filter/facet, live feed rendering
- [ ] Decide relationship to the existing poll-only Account Activity panel

---
