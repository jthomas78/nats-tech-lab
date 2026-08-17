# Business Rules — Pricing Service (`backend/pricing-service/`)

> Split out of `BUSINESS_RULES.md` to keep per-domain reads small. See that
> file's index for the Shipping (BR-001–BR-033) and Reference Data
> (BR-D01–BR-D37/BR-V01–BR-V08) domain rules.

Ported from the pricing/rate domain of a real production freight
marketplace (Linebooker) — `RateSheetEntity`/`FeeScaleEntity`/`FixedRateEntity`
and their versioning. Plain Postgres CRUD, not event-sourced: nothing here
is ever replayed from a log — "what fee schedule was in effect" is answered
by querying the latest **published** version, not by reconstructing history
(see "Event Sourcing vs Plain CRUD" in
`obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md`). Unlike
refdata-service's read-only boundary (BR-D28), this domain is
**write-adjacent** — a fee calculation sits on the load-accept path in the
source system — so it is its own service with its own Postgres, not a merge
into refdata-service.

**Scope so far:** all three source aggregates now have a domain model —
`FeeScale` (BR-P01–BR-P06), `RateSheet` (BR-P07–BR-P12), `FixedRate`
(BR-P13–BR-P15), plus a listing rule (BR-P16) shared informally across all
three. Each aggregate's draft/published/rolled-back lifecycle
(BR-P02/BR-P09/BR-P14) is a near-identical set of rules duplicated per
aggregate — with distinctly-named errors per aggregate — rather than
unified behind a shared generic type, since `FeeScaleVersion`'s shape
shipped first and a shared type would have meant rewriting its
already-passing specs for a cosmetic win. Deliberately deferred, not yet
decided against:

- **Context-tree inheritance** (a business-unit context falling back to
  `_platform` defaults) — every aggregate carries a flat `context` field but
  none walks an ancestry chain the way refdata's
  `AncestorChain`/`FlattenCorpus` do.
- **The customer/route dimension's default-fee-scale fallback** (BR-P10) —
  `RateSheetVersion.FeeScaleOverride` resolves when set, but "no override →
  fall through to the customer's default fee scale" has no customer
  aggregate to hang a default off of yet.
- **The source system's `PricingType`/`RateSheetType.FIXED_RATE` flag
  conflation** — the source keeps two independently-set fields that both
  signal "this needs admin-gated acceptance," a real hazard the agent
  research flagged explicitly. `RateSheet.Type` (BR-P07) is this port's
  single source of truth for that signal; there is no `FixedRate`-side flag
  to keep in sync with it. This is a **design constraint for later
  integration work, not a rule with testable behavior in pricing-service
  today** — there is no Load aggregate yet for anything to gate acceptance
  on, so it is recorded here rather than as a numbered BR-P with no test.
- **Cross-service wiring resolved (Phase 25e/25f)** — the consultation
  question closed in favor of the *browser* talking to pricing-service
  directly over `api.*`, not a `shipping-service`-mediated `rpc.*` hop:
  `shipping-service` never consults pricing-service at all. The Sea Freight
  Flow "Pricing" tab (frontend work, later phase) will be the first real
  caller. `RateSheet.Type` checked against a load's acceptance path (per the
  constraint above) is still unimplemented — nothing gates on it yet, since
  there is no Load aggregate to gate.
- **Service wiring (Phase 25d, IMPLEMENTED)** — Postgres schema (own
  `pricing` schema, own `pricing-postgres` container, port 5435), REST API
  (`GET/POST/PUT` under `/api/pricing/{context}/...`), `cmd/main.go`, and a
  `pricing-service` docker-compose entry (port 7203) now exist and were
  verified end to end against a live Postgres — register → draft → add
  range/entry → publish → active-version resolution → fee/drop-charge
  calculation → rollback, for all three aggregates.
- **`api.*` frontend adapter (Phase 25f, IMPLEMENTED)** — a Sea Freight
  Flow browser reaches pricing-service the same way it reaches
  shipping-service: `api.{context}.pricing.{entity}.{action}.v1` over NATS
  WebSocket, one `browserrpc.Adapter` (package name matches
  shipping-service's own, though the wire family is `api.*` not `rpc.*` —
  see that package's doc comment) per tenant NATS connection. Unlike
  shipping-service's per-tenant bundle, there is no JetStream/KV/projector
  behind it — every tenant's adapter shares the exact same
  FeeScale/RateSheet/FixedRate command handlers, since pricing data is
  scoped by `context` (business unit) in one shared Postgres, not by NATS
  account. `internal/tenants.Manager` discovers known tenants from the
  shared creds directory at boot (`EnsureAll`) and reacts to
  `notify.accounts.account.{created,suspended,reactivated}` the same way
  shipping-service's `EnsureTenantByName`/`TeardownTenantByName` do —
  verified live for the boot-time discovery path (both `acme` and `globex`
  got independent, correctly-isolated adapters over the real NATS CLI); the
  reactive provisioning path mirrors shipping-service's already-proven
  mechanism exactly but was not independently exercised end-to-end (no
  fresh tenant was minted via accounts-service in this pass) — a known gap
  for a later live-verification pass, not a design uncertainty.
- **Sea Freight Flow "Pricing" tab (Phase 25g, IMPLEMENTED)** — a landing
  panel listing every FeeScale/RateSheet/FixedRate registered in the current
  context, built while implementing this phase's List endpoints (BR-P16).
  Follows BR-029's loading-state convention (`stores/pricing.js`). Unlike
  `stores/port.js`, this store has no `notify.*` subscriptions —
  pricing-service publishes no change-notification stream yet (see the
  service-wiring bullet below), so the panel is a one-shot bootstrap fetch
  with no live updates; a manual refresh (re-selecting the tab, or a
  reconnect) is the only way to see another browser tab's edit.
- **Manual-entry UX (Phase 25h, IMPLEMENTED)** — register, build a draft,
  publish, and roll back, for all three aggregates, from the Pricing tab
  itself (`FeeScalePanel.vue`/`RateSheetPanel.vue`/`FixedRatePanel.vue`,
  composed by `PricingPanel.vue`). `stores/pricing.js` only owns the three
  list arrays; `register*`/`toggle*Active` are store actions (upserting into
  those lists, mirroring `stores/port.js`'s `addShippingPort`), while
  create-draft/add-range/add-entry/set-fee-scale-override/publish/rollback
  are per-row detail operations the panels call directly against `api.js`
  and track in their own local component state — the same split
  `ShipsAtPortPanel.vue`/`TerminalPanel.vue` already use for their own
  arrive/depart/load/unload actions. Two UX choices ported deliberately
  *against* the Linebooker source, both already decided in the 25e/25f
  design pass: FeeScale ranges auto-chain each new range's lower limit from
  the previous range's upper limit (an ergonomic kept from Linebooker), but
  there is **no forced-infinite top range** (BR-P05 exists specifically to
  reject a bid above every configured range rather than silently charging
  zero — a forced-infinite top range would recreate that exact bug), and
  there is **no date-driven "no publish step" versioning** — every draft is
  built up, then explicitly published, per this port's corpus lifecycle
  (BR-P02/BR-P09/BR-P14). A structural gap shapes how drafts are edited: no
  endpoint resolves an arbitrary version's ranges/entries by number (only
  `Versions()`, metadata-only, and `ActiveVersion()`, ranges/entries
  included but published-only) — so a draft's ranges/entries are tracked
  purely client-side as they're added in the current browser session, not
  re-fetched from the server. Reloading the page mid-draft loses that
  in-progress list from view (though the rows themselves stay persisted and
  reappear once published) — an accepted, disclosed limitation rather than
  a bug, consistent with a from-scratch manual-entry session being built in
  one sitting. Live-verified end to end in-browser against the real
  `docker compose` stack for all three aggregates: register → create draft
  → add range/entry (or, for FixedRate, the single scalar-field draft
  dialog, since `CreateDraft` takes `centRate`/`pointCount`/
  `centAdditionalDropRate` directly rather than incrementally) → publish →
  roll back to a prior published version, plus RateSheet/FixedRate's
  active/inactive toggle (re-`Register` with the flag flipped — there is no
  separate update endpoint).
- **Diesel-driven mid-version repricing** — the source system mutates a rate
  between publishes via a separate sub-versioning mechanism
  (`RateSheetDieselAdjustmentEntity`): effective-dated overlays keyed on
  `minor_version` / `start_date` / `cent_adjusted_rate`, looked up by the
  load's own `executionDate`. This is **not** equivalent to a new publish —
  folding diesel into "just another publish" loses backdated-load pricing.
  `RateSheet` diesel overlay is implemented in Phase 25i (BR-P17–P23);
  `FixedRate` overlay (`FixedRateSubVersionEntity`) is deferred to Phase 25j.

**Phase 25i (IMPLEMENTED) — Effective-Dated Diesel Overlay** adds BR-P17–P23:
the `major.minor` two-axis version identity and the diesel price index +
overlay mechanism for `RateSheet`. The prior claim that "a diesel-triggered
repricing is just another publish" is **superseded** — see the preamble
bullet and the corrected comments in `rate_sheet.go`/`fixed_rate.go`.
`FixedRate` overlay is deferred to Phase 25j (BR-P14 body updated to note this).

Rules live in `backend/pricing-service/pricing/internal/domain/fee_scale.go`,
`rate_sheet.go`, and `fixed_rate.go`.

---

### BR-P16 — Listing a FeeScale/RateSheet/FixedRate excludes soft-deleted fee scales
Every aggregate can be listed by context (`List`, added to support the Sea
Freight Flow "Pricing" tab, Phase 25g — no endpoint previously existed to
discover what's registered without already knowing an exact name). Only
`FeeScale` carries a soft-delete flag (BR-P01); a listing filters those out
so a deleted-but-not-removed fee scale doesn't resurface in a browse view,
even though it remains reachable by name via `Get`/`Versions`/etc.
`RateSheet`/`FixedRate` have no soft-delete concept, so their listings return
every registered entry for the context unfiltered (`Active`/`Inactive` is
informational, not a delete flag — see BR-P08/BR-P13).

- **Enforced in:** `domain.VisibleFeeScales()` (applied by
  `commands.FeeScaleHandler.List`); `commands.RateSheetHandler.List` and
  `commands.FixedRateHandler.List` are plain pass-throughs with no filtering
  rule to enforce.
- **Test:** `FeeScale Rules / BR-P16`

---

### BR-P01 — A fee scale is a named, context-scoped schedule
A `FeeScale` is identified by a `name` within a `context` (the same
company/business-unit scope used elsewhere in this POC) and can be
soft-deleted without being hard-removed.

- **Enforced in:** `domain.FeeScale`
- **Test:** `FeeScale Rules / BR-P01`

---

### BR-P02 — Draft lifecycle: at most one draft, publish only from draft, rollback only onto a published version
A fee scale's version history follows refdata-service's corpus lifecycle:
`draft → published`, immutable once published. At most one draft may exist
at a time. Rollback may only target an already-published version; the
caller creates a **new**, forward-numbered published version from it —
rollback never rewrites or mutates the target.

- **Errors:** `ErrDraftAlreadyExists`, `ErrOnlyDraftCanPublish`, `ErrRollbackTargetNotPublished`
- **Enforced in:** `domain.CanCreateDraft()`, `domain.FeeScaleVersion.CanPublish()`, `domain.CanRollbackTo()`
- **Test:** `FeeScale Rules / BR-P02`

---

### BR-P03 — Range boundary matching: the zero-lower-bound range is inclusive-inclusive, every other range is exclusive-inclusive
A published version's ranges are matched against a bid amount by cents. The
range whose lower limit is `0` matches `[lower, upper]` inclusively at both
ends — so a zero-value bid still resolves a fee. Every other range matches
`(lower, upper]`. This preserves the source system's deliberate special case
("otherwise empty bids would not get a fee") without depending on range
order.

- **Enforced in:** `domain.FeeScaleVersion.CalculateFee()`
- **Test:** `FeeScale Rules / BR-P03`

---

### BR-P04 — A matched range charges exactly one of a flat cent fee or a percentage fee, never both
Each `FeeScaleRange` declares a `RateType` of `flat` or `percentage`; a flat
range charges its fixed `CentFee` and ignores `PercentageFee`, a percentage
range charges `PercentageFee * bid` (rounded half-up) and ignores `CentFee`.
A range whose rate type is neither is rejected outright.

- **Error:** `ErrInvalidRateType`
- **Enforced in:** `domain.ValidateRange()`, `domain.FeeScaleVersion.CalculateFee()`
- **Test:** `FeeScale Rules / BR-P04`

---

### BR-P05 — A bid above every configured range is rejected, not silently charged zero fee
Fixes a fail-open bug found in the source system: a bid priced above the
top range's upper limit returned a `null` range there, which the calling
code treated as a zero fee. This port raises `ErrBidAboveHighestRange`
instead, since the gap should be visible to a caller rather than absorbed
as free.

- **Error:** `ErrBidAboveHighestRange`
- **Enforced in:** `domain.FeeScaleVersion.CalculateFee()`
- **Test:** `FeeScale Rules / BR-P05`

---

### BR-P06 — The active version is the highest-numbered published version; drafts are never eligible, and there is no date-based fallback
Fixes two further source-system inconsistencies in one rule: FeeScale
version selection there sorted by `id desc` (insertion order) rather than
its own `activationDate`, and RateSheet version selection fell back to the
*earliest* version when none had started yet rather than reporting "no
version." Under the draft/publish lifecycle, "active" is simply the
highest-`Version` entry whose status is `published` — no draft is ever
eligible, and if none is published yet, resolution reports that plainly
instead of guessing.

- **Enforced in:** `domain.ActiveVersion()`
- **Test:** `FeeScale Rules / BR-P06`

---

### BR-P07 — A rate sheet is a named, context-scoped, customer-scoped sheet with a type
A `RateSheet` is identified by a `name` within a `context`, belongs to one
`CustomerKey` (an opaque identifier pricing-service owns itself — this POC
has no customer aggregate), and has a `Type` of `normal` or `fixed-rate`.
`Type` is this port's single source of truth for "does accepting a lane on
this sheet need admin gating" — see the flag-conflation note above.

- **Enforced in:** `domain.RateSheet`, `domain.RateSheetType`
- **Test:** `RateSheet Rules / BR-P07`

---

### BR-P08 — Only an active rate sheet is eligible for version resolution
An inactive rate sheet never resolves an active version, even if one of its
versions is published — version resolution is gated on the sheet's own
active/inactive status first.

- **Enforced in:** `domain.ActiveRateSheetVersion()`
- **Test:** `RateSheet Rules / BR-P08 and BR-P09`

---

### BR-P09 — Draft lifecycle: at most one draft, publish only from draft, rollback only onto a published version
Same shape as BR-P02, applied to `RateSheetVersion`: `draft → published`,
immutable once published, at most one draft at a time, rollback only onto
an already-published version (the caller creates a new forward-numbered
version from it). Combined with BR-P08, the active version is the
highest-numbered published version of an active sheet.

- **Errors:** `ErrRateSheetDraftAlreadyExists`, `ErrRateSheetOnlyDraftCanPublish`, `ErrRateSheetRollbackTargetNotPublished`
- **Enforced in:** `domain.CanCreateRateSheetDraft()`, `domain.RateSheetVersion.CanPublish()`, `domain.CanRollbackRateSheetTo()`, `domain.ActiveRateSheetVersion()`
- **Test:** `RateSheet Rules / BR-P08 and BR-P09`

---

### BR-P10 — A version may override its fee scale by name
A `RateSheetVersion` may carry a `FeeScaleOverride`; when set, it names the
fee scale to use instead of a customer's default. The default-fallback half
of this rule (no override → the customer's default fee scale) is deferred —
see the scope note above — since there is no customer aggregate yet to hang
a default off of.

- **Enforced in:** `domain.RateSheetVersion.ResolvedFeeScaleName()`
- **Test:** `RateSheet Rules / BR-P10`

---

### BR-P11 — A version's lane entries carry a base rate and drop-point terms
A `RateSheetEntry` is keyed by an opaque `RouteKey` and `VehicleType`, and
carries `CentBaseRate`, `DropPointCount`, and `CentAdditionalDropRate`.

- **Enforced in:** `domain.RateSheetEntry`
- **Test:** `RateSheet Rules / BR-P11 and BR-P12`

---

### BR-P12 — The additional-drops charge only counts drops beyond the entry's included point count
Ported directly from the source system's `getRateForLoad`:
`charge = max(0, addressCount - entry.DropPointCount) * entry.CentAdditionalDropRate`.
Fewer addresses than the included point count charges nothing — it never
goes negative.

- **Enforced in:** `domain.RateSheetEntry.AdditionalDropsCharge()`
- **Test:** `RateSheet Rules / BR-P11 and BR-P12`

---

### BR-P13 — A fixed rate is scoped to one customer and one route
A `FixedRate` belongs to one `CustomerKey` + `RouteKey` pair (both opaque
identifiers) and has its own active/inactive status.

- **Enforced in:** `domain.FixedRate`
- **Test:** `FixedRate Rules / BR-P13`

---

### BR-P14 — Draft lifecycle and active-fixed-rate gating
Same shape as BR-P08/BR-P09, applied to `FixedRate`/`FixedRateVersion`: an
inactive fixed rate never resolves a version; among an active fixed rate's
versions, `draft → published` with at most one draft at a time, publish
only from draft, rollback only onto a published version, and the active
version is the highest-numbered published one. Diesel-driven
sub-versioning is a separate axis: for `RateSheet` it is an effective-dated
overlay (Phase 25i, BR-P17–P23); for `FixedRate` it is deferred to Phase 25j.

- **Errors:** `ErrFixedRateDraftAlreadyExists`, `ErrFixedRateOnlyDraftCanPublish`, `ErrFixedRateRollbackTargetNotPublished`
- **Enforced in:** `domain.CanCreateFixedRateDraft()`, `domain.FixedRateVersion.CanPublish()`, `domain.CanRollbackFixedRateTo()`, `domain.ActiveFixedRateVersion()`
- **Test:** `FixedRate Rules / BR-P14`

---

### BR-P15 — The additional-drops charge mirrors RateSheetEntry's formula
Same formula as BR-P12, applied to a `FixedRateVersion`'s own `PointCount`/
`CentAdditionalDropRate` rather than a `RateSheetEntry`'s.

- **Enforced in:** `domain.FixedRateVersion.AdditionalDropsCharge()`
- **Test:** `FixedRate Rules / BR-P15`

---

## Phase 25i — Effective-Dated Diesel Overlay (IMPLEMENTED)

These rules add the `major.minor` version axis and the diesel price overlay
mechanism for `RateSheet`. The existing major-version lifecycle (BR-P09) is
**unchanged**. `FixedRate` overlay is Phase 25j (deferred).

---

### BR-P17 — A rate-sheet version carries a `major.minor` identity; diesel price changes bump minor only
A `RateSheetVersion` has two version axes: `Version` (major, bumped by the
existing draft→publish lifecycle, BR-P09 **unchanged**) and `MinorVersion`
(minor, starts at 0 on every new publish, bumped each time a diesel price
overlay is appended). A diesel-price change never triggers a new major publish.

- **Enforced in:** `domain.RateSheetVersion.AppendDieselOverlay()` (increments `MinorVersion`, never `Version`)
- **Test:** `RateSheet Diesel Overlay Rules / BR-P17`

---

### BR-P18 — The diesel price index maps an active date to a coastal/inland price; lookup returns the greatest `active_date ≤` query date
The diesel price index (`pricing.diesel_prices`, context-scoped) is a time
series where each row is `(active_date, coastal_cents, inland_cents)`. "Price
in effect on date X" is the row with the greatest `active_date` that does not
exceed X. If no such row exists, the index has no coverage for X (see BR-P21).

- **Enforced in:** `domain.DieselPriceOn()`
- **Test:** `RateSheet Diesel Overlay Rules / BR-P18`

---

### BR-P19 — Each rate-sheet entry carries diesel baseline fields as part of its major version
A `RateSheetEntry` carries two diesel baseline fields alongside its authored
base rate: `DieselPct float64` (percentage of the base rate exposed to diesel
cost) and `InitialDieselCents int64` (the diesel price at authoring time, used
as the formula denominator). These are immutable once the version is published.

- **Enforced in:** `domain.RateSheetEntry`
- **Stored in:** `pricing.rate_sheet_entries.diesel_pct`, `initial_diesel_cents`
- **Test:** `RateSheet Diesel Overlay Rules / BR-P19`

---

### BR-P20 — A diesel price change auto-appends an effective-dated overlay per entry; adjusted rate pre-computed at creation
When a diesel price becomes effective on date D, one `DieselOverlay` is
appended per `RateSheetEntry` in the currently published major version:
`StartDate = D`; the previously-open overlay's `EndDate` closes to D
(contiguous, non-overlapping windows). The adjusted rate is pre-computed
once at append time:

```
adjusted = base + base·(DieselPct/100)·((currentDiesel − InitialDieselCents)/InitialDieselCents)
```

where `currentDiesel = CoastalCents` of the diesel price in effect on D.
Overlays are stored in `pricing.rate_sheet_overlays`.

- **Enforced in:** `domain.AdjustedRate()`, `domain.RateSheetVersion.AppendDieselOverlay()`
- **Persisted by:** `postgres.RateSheetRepository.PersistDieselOverlay()`
- **Test:** `RateSheet Diesel Overlay Rules / BR-P20`

---

### BR-P21 — No diesel price indexed on or before the effective date → reject (fail-closed)
If `DieselPriceOn` returns no entry for the requested date, overlay creation is
rejected with `ErrNoDieselPrice`. Fail-closed: no silent fallback to zero or
"current." Same spirit as BR-P05 (bid above every range is rejected, not
silently zero).

- **Error:** `ErrNoDieselPrice`
- **Enforced in:** `domain.AppendDieselOverlayFromIndex()`
- **Test:** `RateSheet Diesel Overlay Rules / BR-P21`

---

### BR-P22 — Load pricing: active major → entry → overlay window containing effectiveDate → adjusted rate + drop surcharge
Pricing a load resolves `(route_key, vehicle_type, effectiveDate, addressCount)`
against the active published major version (BR-P08): find the matching entry
(`ErrEntryNotFound` if absent), find the `DieselOverlay` window containing
`effectiveDate` (`start ≤ date < end`, last window open-ended), return that
adjusted rate plus the additional-drops surcharge (BR-P12, unchanged).
`effectiveDate` is the load's own pickup date, **not** "now."

- **Error:** `ErrEntryNotFound` (no matching entry)
- **Enforced in:** `domain.RateSheetVersion.RateForLoad()`
- **Test:** `RateSheet Diesel Overlay Rules / BR-P22`

---

### BR-P23 — `effectiveDate` before first overlay window falls back to authored base rate; new major starts with no overlays
When `effectiveDate` precedes the `StartDate` of every overlay for the matched
entry, `RateForLoad` falls back to the entry's authored `CentBaseRate` (plus
the drop surcharge). A newly published major version starts at `MinorVersion =
0` with no overlays and accrues its own overlays going forward — it does not
inherit its predecessor's diesel windows.

- **Enforced in:** `domain.RateSheetVersion.RateForLoad()` (fallback path)
- **Test:** `RateSheet Diesel Overlay Rules / BR-P23`

---

### BR-P24 — An entry with no authored diesel baseline is skipped when an overlay is appended, not corrupted to zero
`AdjustedRate`'s formula divides by `InitialDieselCents`; an entry that was
never given a diesel baseline (`InitialDieselCents == 0`, the zero-value
default — true of every rate sheet entry created before Phase 25i, and of
any entry added afterward without one) would otherwise divide by zero,
producing `NaN` that silently converts to a `0` adjusted rate — corrupting
the price to $0 rather than erroring or falling back sensibly. Found live
during 25i-c's docker-compose smoke test against a pre-25i seeded rate
sheet. `AppendDieselOverlay` now skips appending an overlay for any entry
with `InitialDieselCents <= 0`; that entry is left un-overlaid and keeps
resolving to its authored `CentBaseRate` via `RateForLoad`'s BR-P23
fallback. Entries that do carry a baseline are unaffected and still get
adjusted normally.

- **Enforced in:** `domain.RateSheetVersion.AppendDieselOverlay()`
- **Test:** `RateSheet Diesel Overlay Rules / BR-P24`

### BR-P25 (Phase 28) — The same `obs.trace.*` wire contract as `BUSINESS_RULES-SHIPPING.md`'s BR-036, on pricing-service's publisher side

Mirrors `BUSINESS_RULES-SHIPPING.md`'s BR-036 for this service's own tracing publisher. `browserrpc.Adapter`'s `traceSpan` is a strict superset of its existing `obsEnvelope` — no field renamed or retyped, every addition (`traceId`, `spanId`, `parentSpanId`, `service`/`entity`/`action`, `statusCode`/`statusMessage`, `attributes`, `redacted`, `truncated`) `omitempty` — and every `obs.trace.{context}.pricing.{entity}.{action}` publish goes to the PLATFORM account only, with the same redact-before-truncate ordering and 4 KiB cap BR-036 establishes. Never blocks or fails a business path.

- **Enforced in:** `pricing/internal/natstrace` (new package, Phase 28b) — mirrors `dictionary/internal/natstrace`'s `Tracer.publish()` redaction-then-truncate ordering and `traceSpan` struct field-for-field.
- **Test:** `pricing/internal/natstrace/natstrace_test.go` — the shared cross-service contract test (BR-036's clone) asserting the `traceSpan` JSON shape decodes identically to shipping-service's, and that an old-shape `obsEnvelope` with none of the Phase 28 fields still decodes.

### BR-P26 (Phase 33.4) — Business operations are reachable only over `api.*`/`rpc.*`; REST reduces to infra health

All 34 `/api/pricing/{context}/...` REST routes (FeeScale, RateSheet, FixedRate, and the diesel price index/overlay endpoints) are deleted now that `internal/browserrpc`'s `api.*` adapter (Phase 25f) has full 1:1 parity with them. Nothing outside pricing-service ever called them: `frontend/seafreight-app`, `frontend/admin`, and every other frontend in the repo already talk to pricing-service exclusively over `api.*`. REST's only remaining surface is `GET /healthz`, mirroring the convention `dictionary/internal/rest` already established. pricing-service has no admin-only or BasicAuth-gated REST routes to carve out an exception for — unlike `refdata-service`'s `/api/refdata/admin/*`, every pricing operation is tenant-facing business data, so there is no third "admin REST" category here, only the two: infra (`/healthz`) and business (`api.*`/`rpc.*`).

- **Enforced in:** `pricing/internal/rest/handlers.go` (now just `Mount()` registering `/healthz`); `pricing/composition.go`'s `Handlers.Mount(mux)` no longer takes command-handler dependencies since the REST layer has none left to wire.
- **Test:** N/A — this is a route-deletion/transport-contract rule, not a domain rule; correctness is covered by `go build ./...` compiling cleanly with `internal/rest` down to zero business handlers, and the full `ginkgo ./...` suite staying green since `api.*`/`rpc.*` and the domain layer are untouched.
