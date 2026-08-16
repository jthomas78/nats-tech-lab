---
name: phase28-trace-detail-request-response-split
description: Phase 28g follow-up (2026-08-15) — natstrace dropped Nats-Requestor on the wire; fixed + TraceWaterfall.vue detail pane redesigned into Request/Response columns; Phase 28q (2026-08-16) — REST kind-tag recolored, KeepAlive fixes tab-toggle remount perf bug
metadata:
  type: project
---

Follow-up to [[phase18_requestor_responder_headers]] and Phase 28 (distributed
tracing, `natstrace` package) — found via user feedback on the Admin UI's
traces panel (`TraceWaterfall.vue`), fixed 2026-08-15.

**Bug found:** `natstrace.go`'s `Span.Start()` captures the inbound request's
headers into `sp.reqHeaders` (including `Nats-Requestor`), but `finish()`
never read that field — it only serialized whatever `headers` map the caller
explicitly passed to `End`/`Fail` (always just `Nats-Responder`). So every
published `obs.trace.*` span showed only the responder's identity; the
requester was silently dropped before it ever reached the wire.

**Fix:** added a `mergeHeaders(reqHeaders, headers)` helper to all 5
`natstrace.go` copies (shipping, pricing, refdata, trading-partner,
accounts — this package is deliberately duplicated per service, not shared)
and wired it into `finish()`. Outgoing headers win on key collision.
Live-verified after the fix: a real `rpc.acme.refdata.type.list.v1` span now
carries both `Nats-Requestor: shipping-service/...` and
`Nats-Responder: refdata-service/...`.

**Two more UI bugs fixed same session, unrelated to headers:**
- **Double-encoded Body**: a truncated payload (>4KiB cap) is re-encoded
  server-side as a JSON *string* of the cut bytes (not left as broken inline
  JSON) — `jsonHighlight.js`'s `highlightJson()` was unconditionally calling
  `JSON.stringify` on it again, double-escaping every embedded `"`. Fixed:
  only stringify non-string values; a string is shown as-is.
- **Resize handle invisible at rest**: `TraceWaterfall.vue`'s trace-list/
  waterfall divider was fully functional (drag + keyboard resize) but only
  showed a 1px hairline until hover. Added a 3-dot grip affordance visible at
  rest, matching the divider's existing drag/keyboard behavior.

**Detail pane redesign (frontend-only, no further backend change):** the
pane now shows a "requested by / responded by" identity strip, then splits
the SAME merged `headers`/`attributes` maps into Request and Response
columns by key-name classification (`Nats-Requestor`, `traceparent`,
`http.method`/`http.path` → request; `Nats-Responder`,
`Nats-Service-Error*`, `http.status_code` → response) — this works entirely
client-side since one span already carries both identities after the merge
fix above. Response body stays a single section (natstrace is one-span-
per-call, BR-037 — there is only ever one payload). A dashed "Request body —
not captured yet" note flags the one genuine remaining gap: showing an
actual request payload needs a new `traceSpan.RequestPayload` field captured
at `Start()`/`StartFromHeaders()` time, not yet implemented. A static mockup
(via `frontend-design` skill) preceded the real implementation — see
`demos/01-dictionary/diagrams/trace-detail-request-response.html`.

**Gotcha caught during implementation:** see
[[vue_todisplaystring_array_gotcha]] — the identity strip initially rendered
`[ "shipping-service/..." ]` instead of a plain string.

**Phase 28h follow-up (2026-08-15, same day) — the `RequestPayload` gap above is closed.**
Every `natstrace.go` copy (all 5 services) now captures the inbound request
body at span-construction time (`Span.reqPayload`) — `Start(req)` reads
`req.Data()` automatically; `StartFromHeaders`/`StartOutbound` gained a
`payload []byte` parameter every call site across all 5 services was updated
to pass; accounts-service's HTTP-only copy buffers `r.Body` in
`HTTPMiddleware` via `io.ReadAll` + `io.NopCloser` restore. `finish()` runs
the same redact-then-truncate pipeline independently on both payloads via a
new shared `preparePayload` helper, publishing `requestPayload`/
`requestPayloadBytes`/`requestRedacted`/`requestTruncated` alongside the
existing reply-side fields (all `omitempty`, backward compatible). For
accounts-service's fire-and-forget `publishAccountEvent`, request and reply
payload are the same bytes (there's no real second value) — correct, not a
special case. `TraceWaterfall.vue`'s "Request body — not captured yet" note
and its `.tw-note-strip` CSS are removed, replaced with a real "Request
body" section (`selectedSpan.requestPayload`) next to "Response body" —
verified live against the docker stack (a real
`rpc.acme.refdata.locales.list.v1` span showed `{"context": "_platform"}` in
Request body). Test coverage: each service's `natstrace_test.go` gained a
"captures the request payload independently of the reply payload" spec;
`TraceWaterfall.spec.js` gained a matching two-section render spec.
Documented as BR-036's Phase 28h amendment in `BUSINESS_RULES-SHIPPING.md`.

**Phase 28i (2026-08-15) — "no Nats-Requestor on this span" was a real bug, two
of them.** Found by querying the live `traces` KV bucket and grouping spans by
subject family + hasRequestor, which isolated it precisely: `api.*` roots and
`rpc.*` *children* had a requestor; outbound *roots*
(`refdata.type.list.v1`, `rpc._platform.refdata.context.list.v1`) never did.
(1) `StartFromHeaders` received the inbound headers but read only `traceparent`
and discarded the rest — one-line fix, retain `reqHeaders`. (2) A
`StartOutbound` span *cannot* capture its own headers at construction (the
caller needs `sp.Traceparent()` to build them, so the span exists first) — added
`Span.SetRequestHeaders(...)`, called at the 3 outbound sites after the header
map is built. Retaining full header sets then made header redaction necessary:
accounts-service's `HTTPMiddleware` passes whatever the browser sent, so
`finish()` now runs `redactHeaders` using the same `redactDenylist` as payload
keys, recording strips as `headers.<Name>` in the existing `redacted` list.
Remaining empty requestors are HTTP entry points — genuinely no NATS caller.

**How to apply:** when adding a new span constructor, remember the asymmetry —
inbound spans capture headers, outbound spans must be *told* them. And the
`traces` KV bucket is the fastest way to diagnose span-shape questions:
`curl -s localhost:7200/api/kv/buckets/platform/traces/entries` then group by
subject family; the presence/absence pattern localises the bug far faster than
reading the five duplicated `natstrace.go` copies.

**Layout — implemented, Option A (2026-08-15, same session).** The detail
pane's axis break (2-col identity/headers, then full-width stacked bodies) is
fixed: one continuous `.tw-rr-grid` (identity/headers/attributes/body all in
the same 2-column split, one `.tw-rr-seam` divider spanning every row),
`→ REQUEST`/`← RESPONSE` named once in the caption row instead of on every
sub-label. Mockup that was approved: `demos/01-dictionary/diagrams/
trace-detail-axis-options.html`.

**Gotcha hit while implementing:** picked `.tw-axis` as the new grid's class
name — collided with a PRE-EXISTING `.tw-axis` (the waterfall's time-ruler
ticks bar, `height: 22px`), which silently collapsed the new grid to 22px
tall (cascade: my rule didn't redeclare `height`, so the old rule's height
survived). Then picked `.tw-split` as the fix — ALSO already taken (the
trace-list/waterfall resizable pane container). Landed on `.tw-rr-*`
(request/response). **Before naming a new class in this file, grep the file
for it first** — it's grown enough sections that short/generic names
(`tw-axis`, `tw-split`, `tw-grid`, `tw-list`, `tw-row`) are already spoken
for, and a collision fails silently (no console error, just wrong CSS)
rather than loudly.

Also fixed alongside: the empty-identity copy now reads "HTTP entry point —
no NATS requestor" (computed from `subject.startsWith('/')`) instead of the
generic "no Nats-Requestor on this span" wording, so a legitimately-empty
HTTP-transport identity no longer reads like the same bug that was just
fixed above.

**Phase 28j (2026-08-15, later same session) — two more asks via
`frontend-design`: Span list/Span details grouping, and a shared top-tabs
rule.** (1) `TraceWaterfall.vue`'s waterfall column now splits into two
labeled, independently-scrollable cards — `.tw-panel-list` (axis + rows) and
`.tw-panel-details` (the existing `.tw-rr-grid` detail pane, content
unchanged) — via a CSS grid (`.tw-wf-body`, row heights
`${spanListHeight}px 6px minmax(0,1fr)`) with a draggable
`.tw-vresize-handle` between them, mirroring the existing horizontal
trace-rail handle pattern exactly (mouse-drag + arrow-key resize, height
persisted in the `ui` Pinia store as `spanListHeight` for the same
tear-down-survival reason as `traceRailWidth`). Driven by a reference
screenshot (a Jaeger/Tempo-style span-list-over-span-details concept) but
NOT copied verbatim — adapted to this panel's existing dark-card language.
(2) Formalized a **"panel top tabs" rule**: any top-position tab strip on a
right-side detail panel must be a real PrimeVue `Tabs` with
`class="panel-tabs"`, never a custom chip/pill toggle — added `.panel-tabs`
to `shared/unifi-theme/unifi.css` (previously AccountsView.vue had a local
`:deep(.p-tabs){--p-tabs-tablist-border-width:...}` override that turned out
to be redundant with Aura's own default) and documented it in
`shared/unifi-theme/LAYOUT.md`'s new "Panel top tabs" section. Migrated
`RpcPanel.vue`'s `[traces]`/`[messages]` `.chip` toggle to real
`Tab`/`TabList`/`TabPanels`/`TabPanel` — needed `:deep()` flex-fill CSS on
`.p-tabs`/`.p-tabpanels`/`.p-tabpanel` since (unlike AccountsView) this
panel's content needs full panel height (`DataTable
scroll-height="flex"`, `TraceWaterfall`'s own flex layout). Both existing
spec files (`TraceWaterfall.spec.js`, `RpcPanel.spec.js`) passed unchanged
against both changes — proof the regrouping/tab-migration were pure
presentation, no behavior change. Documented as BR-035's Phase 28j amendment
in `BUSINESS_RULES-SHIPPING.md`.

**How to apply:** before naming any new class in `TraceWaterfall.vue`, grep
first — this file has now hit two prior silent collisions (`.tw-axis`,
`.tw-split`, see above). When a panel needs a "make this look like panel X"
ask, check whether the existing implementation is already just a framework
default (as AccountsView's tabs were) before writing new CSS — sometimes the
fix is centralizing what already exists rather than inventing new rules.

**Phase 28j follow-up (same day) — three bugs the first pass introduced/left
behind, found via user re-review of a live screenshot.** (1) **Tabs still
looked different from Accounts**: `RpcPanel.vue`'s `<Tabs class="panel-tabs
rpc-tabs">` puts both classes on the SAME root element as PrimeVue's own
`.p-tabs` — so a selector like `.rpc-tabs :deep(.p-tabs) {...}` (descendant
combinator) never matches anything, because there's no ancestor/descendant
relationship between two classes on one element. Silent failure: no console
error, `.p-tabs` just kept PrimeVue's own `flex: 0 1 auto` default instead of
the intended `flex: 1`, so the whole Tabs root grew to fit its content
(22000px+) instead of being height-constrained — this is *why* the list/
scroll bugs below happened, not a separate root cause. Fixed with a plain
compound selector, no `:deep()`: `.rpc-tabs.p-tabs { flex: 1; ... }` (works
un-deep because `.rpc-tabs` is applied via this component's own template,
so it already carries the scope attribute). Separately, RpcPanel's Tabs are
nested inside a `.lab-panel` card (App.vue's shared `streams-panel`
wrapper — also used by Streams/KV/Connections, not something to strip
just for this one panel) while AccountsView's Tabs sit flush on the bare
page — so the tablist's own PrimeVue-default background
(`{content.background}`, resolves to `--lab-bg`) visibly mismatched the
surrounding card's `--lab-panel-bg`. Fixed by making `.p-tablist`
background transparent and pulling it edge-to-edge past the card's own
0.75rem padding (`margin: -0.75rem -0.75rem 0; padding: 0 0.75rem;`) so its
hairline border-bottom now spans the full card width, matching how it spans
the full page-content width on Accounts. (2) **Messages table / Traces list
/ Span details not scrollable**: direct consequence of bug (1) — once
`.p-tabs` is actually height-constrained, `.tw-list-body`/`.rpc-table`'s
existing `overflow:auto` rules (unchanged from before Phase 28j) started
working correctly again. No new overflow CSS was needed once the real bug
was found — resist the urge to add scroll-fix CSS in a dozen places when
the actual break is one wrong selector three levels up. (3) **"Span
list"/"Span details" headers needed a subtle highlight**: this app already
has an established card-title convention (`AccountsPanel.vue`'s "ACCOUNTS"
heading, Overview's "PIPELINE HEALTH") — uppercase label in
`var(--lab-accent)`. Reused it exactly rather than inventing a new
treatment: `.tw-panel-title { color: var(--lab-accent); }` plus a faint
`background: rgba(0, 111, 255, 0.06)` on `.tw-panel-head`.

**How to apply (debugging a "my :deep() override isn't applying" case):**
check whether the class you're targeting for `:deep()` is on the SAME
element as the class the parent component put in its own `class="..."`
binding — if so, `:deep()` isn't needed and will silently never match; use
a plain compound selector instead (it still gets the scope attribute
automatically since it's written by the component that renders that
element in its own template).

**Phase 28j second follow-up (same day) — tabs moved fully outside/above the
panel card, matching Accounts structurally, not just visually; plus a
highlight on the "TRACES" list-rail header too.** The edge-to-edge hack
above (negative margin pulling the tablist past the card's padding) was a
patch for the WRONG structure — AccountsView's Tabs were never inside a
`.lab-panel` card to begin with; only its TAB CONTENT (`AccountsPanel.vue`
wraps itself in `<div class="lab-panel accounts-panel">`) is boxed. Fixed
properly: `App.vue`'s `rpc` section no longer wraps `<RpcPanel/>` in
`<div class="lab-panel streams-panel">` — that wrapper is dropped entirely,
`RpcPanel.vue`'s own `.rpc-panel` root already carries the equivalent flex
sizing. Inside `RpcPanel.vue`, each `TabPanel`'s content is now wrapped in
its own `<div class="lab-panel rpc-card">` (traces: wraps `TraceWaterfall`;
messages: wraps the toolbar/table/detail block, guarded by `v-if` on the
div itself since it replaces the old `<template v-if>`). This let the
edge-to-edge `.p-tablist` margin/padding/background hack be deleted outright
— once the Tabs aren't nested in a card, they need no override at all,
exactly like Accounts. Also: `TraceWaterfall.vue`'s "TRACES" list-rail
header (`.tw-list-head`, the "traces / 445" bar above the trace list) got
the same `.tw-panel-title`/`.tw-panel-count` treatment as Span
list/details — reused directly rather than writing new CSS, since it's the
same "small uppercase list-header bar" shape.

**How to apply:** when told "make X look/behave like Y," check Y's actual
DOM nesting, not just its rendered CSS values — a structural difference
(what's inside what) can require a structural fix; patching the visual
symptom (background/margin tricks) works but leaves the underlying mismatch
to resurface on the next ask, as it did here.

**Phase 28k (2026-08-15, later same session) — a real "child rendered above
its parent" bug, found by the user asking why indentation "looked reversed"
in a screenshot.** First response (wrong): explained indentation as
depth-based tree indent (correct in general) and assumed the screenshot just
showed normal parent-then-child ordering. User pushed back with a second
screenshot showing the reverse. Root-caused by fetching the actual trace's
raw spans from `/api/kv/buckets/platform/traces/entries` (same
diagnostic technique as Phase 28i — grep the KV bucket directly rather than
guessing from rendered pixels) and hand-computing `ownStart`/`ownFinish` in
a Node one-liner: `TraceWaterfall.vue`'s `ownStart`/`ownFinish` used
`new Date(span.timestamp).getTime()`, which truncates the backend's
nanosecond-precision timestamp to whole milliseconds. A fast parent/child
pair (1-2ms each) can have finish times a FRACTION of a millisecond apart —
truncated, both computed the same integer millisecond, so `ownStart` tied
for both spans. `waterfallRows`' `.sort((a,b) => a.offset - b.offset)` is
stable on ties (`Array.prototype.sort` since ES2019), so a 0-0 comparison
falls through to `t.spans`' original array order — which is publish order,
not causal order — landing the child above the parent it depends on. Fixed
with `preciseFinishMs`, parsing the ISO string's fractional-second digits
directly instead of routing through `Date`. Verified two ways: (1) a Node
script computing both the buggy (`Date.getTime()`) and fixed
(`preciseFinishMs`) values against the exact real timestamps side by side,
confirming the fix produces `parent.start < child.start` as causality
requires; (2) live in the docker stack, searching for the exact trace ID
from the user's screenshot and confirming it now renders parent-first.
Depth/indentation itself was never wrong — only row ORDER was — which is
why the bug read as "indentation looks reversed" even though each row's own
indent-rail count was correct for its own depth the whole time; a correctly
indented child one row above its unindented parent still looks backwards at
a glance.

**How to apply:** `Date.getTime()`/`new Date(iso).getTime()` truncates to
millisecond precision — any UI computation ordering/diffing FAST events
(sub-few-ms apart) off a backend timestamp with sub-millisecond precision
should parse the fractional-second digits directly rather than routing
through `Date`, or ties will silently fall back to array/insertion order
instead of true chronological order. When a user reports something looking
"reversed" or "wrong" in a specific screenshot, prefer pulling the exact
underlying data for that exact instance (the KV bucket, in this app) over
reasoning from the general code path — the general path can be individually
correct (depth was) while a DIFFERENT nearby computation (offset/sort) is
what's actually broken.

**Phase 28j third follow-up (same day) — the gap/hairline was STILL missing
after the structural fix, because a separate override was fighting it.**
`RpcPanel.vue`'s `.rpc-tabs :deep(.p-tabpanels)` had `padding: 0` (added
while chasing full-height layout, unrelated to spacing) — but Aura's own
default `tabpanels` padding (`11.375px 14.625px 14.625px`, confirmed via
`getComputedStyle` on the live Accounts page) is exactly what creates the
visible gap between the tab hairline and the card below on
`AccountsView.vue`. Zeroing it made `RpcPanel.vue`'s `.rpc-card` sit flush
against the tablist, so its top border visually merged with the tablist's
own hairline into one line. Fix: deleted the `padding: 0` line, keeping only
`flex`/`min-height`/`display`/`flex-direction` on `.p-tabpanels`. Verified
via `getComputedStyle` before/after (`.rpc-card` rect.y moved from directly
adjacent to the tablist to matching Accounts' ~11px offset) and a live
screenshot comparison against Accounts. Documented as a full "Panel top
tabs" rule rewrite in `shared/unifi-theme/LAYOUT.md` (placement — Tabs never
wrapped in a card, the card goes on each TabPanel's content instead;
`.p-tabpanels` padding must be left alone; plus the same-element `:deep()`
gotcha from the previous follow-up) so the next panel that adds tabs doesn't
reintroduce any of these three bugs from scratch.

**Phase 28l — the trace KV bucket was renamed `traces` → `trace-request-reply`
at the user's explicit request (JetStream `TRACES` stream name kept as-is).**
Mechanical rename of `traceStoreBucket` in `trace_store.go` plus every
consumer, but one grant is easy to miss: `accounts-service/auth/token.go`'s
`MintAdminToken` hardcodes the notify subject
(`notify._platform.kv.{bucket}.>`) it grants the Admin UI's browser JWT
rather than deriving it from the bucket constant — missing that update would
have left live trace updates silently unreceived (publish succeeds; only the
browser's NATS permission match fails, no error anywhere) despite bootstrap
REST fetch working fine. Old `traces` KV bucket was deleted outright (not
migrated) after confirming the new bucket was live. Also purged the `TRACES`
stream's old messages (`nats stream purge`) as a separate, explicitly
requested cleanup.

**Phase 28m — shipping-service's REST layer had NO HTTP-level tracing at
all**, unlike accounts-service (which already had `natstrace.HTTPMiddleware`).
This meant every outbound `rpc.*` call a REST handler triggered via
`refdataconsumer.StartOutbound` found no parent span on `r.Context()` and
minted its own untraceable root — a trace like `GET /api/refdata/types/
{type}` showed shipping-service itself as the apparent originator, hiding
the real browser HTTP request entirely. Root-caused across several turns:
first assumed (wrongly) that the three refdata REST routes were dead
demo-only code with no real caller — corrected after grepping frontend
`api.js` files too narrowly and missing that `shared/refdata/
useRefdataLabels.js`/`useL10nCopy.js` (imported via the `@refdata` vite
alias, used by `admin/App.vue`/`i18n.js`/`ShapePanel.vue`) call `fetch()`
directly, bypassing `api.js` entirely — confirmed via live network capture
on page reload. Fixed with `dictionary/internal/rest/trace_middleware.go`'s
`httpTraceMiddleware`, a `*Handlers` method (not a `*natstrace.Tracer` one
like accounts-service's) specifically because it reads `h.deps()` fresh on
**every request** rather than closing over a startup-time snapshot —
shipping-service's NC changes on `SwitchTenant`, accounts-service's never
does. Wired into every REST route except the two long-lived SSE streams
(`/api/refdata-watch`, `/api/nats/log` — wrapping them would hold a span
open for the connection's whole lifetime) and `/healthz`/`/swagger/`.

**Phase 28n — Phase 28k's timestamp-precision fix left a second, INDEPENDENT
truncation source untouched: `durationMs` itself is whole-millisecond-
truncated server-side** (Go's `time.Duration.Milliseconds()` in every
`natstrace.go` copy's `finish()`), and `ownStart = ownFinish - durationMs`
inherits that error regardless of how precise `ownFinish` is. Surfaced
immediately once Phase 28m's HTTP tracing went live: an HTTP root span and
its own direct outbound `rpc.*` child both truncated to the identical
`durationMs: 66`, so subtracting that same truncated number from each span's
own (precise) finish time preserved their finish-time order but inverted
their estimated *start* order — the root's estimate landed later than its
child's, even though `parentSpanId` already proves the root started first.
Root-caused by pulling the real KV trace record and computing `ownStart` by
hand for all 3 spans (same diagnostic technique as Phase 28k). **The fix
this time is structural, not another precision patch**: `waterfallRows` no
longer flat-sorts every span in a trace by `offset`; it walks the known
`parentSpanId` tree in pre-order (a span always renders immediately above
its own subtree), using `offset` only to break ties among *siblings*, never
to reorder a span relative to its own ancestor/descendant. This closes the
whole class of bug rather than the one instance — a future truncation source
(or a NEW one) can't invert parent/child order again, since that's now
structurally guaranteed by construction.

**How to apply:** (1) when a REST/HTTP layer in this codebase is missing
tracing, check whether ANOTHER service already solved it (accounts-service's
`HTTPMiddleware`) before designing from scratch — but check for
service-specific reasons the pattern can't just copy 1:1 (here: per-request
vs. startup-time NC). (2) Before claiming "no frontend calls this route,"
grep is only as complete as the files you grep — a shared composable
directory reached via a vite alias, calling `fetch()` directly instead of
going through the app's own `api.js` wrapper, is exactly the kind of thing a
narrow grep misses; a live network capture on reload is the ground truth.
(3) Any computation that estimates a value by subtracting one imprecise
number from a precise one (`ownStart = preciseFinish - truncatedDuration`)
can still be wrong even after fixing the precise half — check EVERY input to
the formula for truncation, not just the one already fixed. (4) When you
already know a structural invariant (parentSpanId proves causal order), a
sort keyed on estimated/derived values should never be allowed to violate
that invariant — walk the known tree and use the imprecise heuristic only to
break ties among siblings, not to override known structure. This is more
robust than chasing precision bugs one instance at a time.

**Phase 28o — trace kind marker (rest/nats) + toolbar filter, via
`/frontend-design:frontend-design`.** `traceKind(rootSubject)` classifies a
whole trace by its ROOT span alone (`/`-prefixed subject = `rest`, matching
`httpTraceMiddleware`'s published subject shape; everything else = `nats`),
same "classify by root" convention the trace list already used elsewhere.
Rendered as a `.kind-tag` per row plus a three-way segmented `all/rest/nats`
toolbar control (segmented, not a second independent boolean chip, since two
AND-combinable booleans could both read "on" with no way to tell that means
"all" again — same reasoning as the existing errors/slow chips vs. this
control). Surfaced a real latent bug in `SubjectPath.vue`: its NATS-only
verb/id heuristic (last dot-token = accent blue) misfired on a REST path,
since a no-dot string makes index 0 simultaneously "the last segment" —
fixed with a `/`-prefix branch that renders a REST path as one plain
segment, since the kind tag already carries the transport distinction.

**Phase 28p — "pulse strip" (request count / error count / avg latency)
under the toolbar, via `/frontend-design:frontend-design`, mockup-first.**
Three cards bucket `displayedSummaries` — the SAME post-filter array the
trace list already renders — into a fixed 20 columns spanning however far
back the live trace buffer reaches (no historical metrics backend exists
here, only the live buffer, so the window is "as far back as buffered," not
a calendar interval). Requests/Errors are bar histograms; Avg latency is a
filled line built only from non-empty buckets (an empty bucket is skipped,
never interpolated to zero — zero would misrepresent "no data" as "latency
dropped to nothing"). Because it reads the same filtered array the list
uses, toggling errors/slow/rest-nats in the toolbar reshapes the strip and
the list together automatically — no separate global-metric path to keep in
sync. All colors reuse existing tokens (`--lab-accent`, the same `#e5484d`
err red already used elsewhere) — zero new palette. `v-if`-gated on a
non-empty filtered list so a zero-match filter combination hides the strip
outright instead of rendering an empty one.

**How to apply (28o/28p):** (1) `/frontend-design:frontend-design` mockups
for a feature added to an EXISTING app screen (not a new page) should be
built from the real component's own CSS variables/class conventions
one-for-one (verified by reading the target `.vue` file's `<style>` block
first), not a fresh palette — the user's sign-off on the mockup is sign-off
on that exact visual language carrying over unchanged. (2) A derived/
computed UI feature (histograms, filters) still falls under this repo's
"every business rule needs a test" quality gate even though it isn't a
domain rule — write the Vitest spec in the same task, not as a follow-up;
the earlier Phase 28n audit already established that skipping this is a
real, catchable gap, and doing it proactively this time closed it before
the user had to ask. (3) When a UI change lands but its
`BUSINESS_RULES-*.md` amendment doesn't (Phase 28o shipped without one until
this later cleanup pass), check the doc BEFORE claiming a phase is "done" —
grepping for the phase label in the `.md` file is a 5-second check that
would have caught this immediately.

**Phase 28q (2026-08-16) — two small user-reported follow-ups: a color tweak
(later reverted) and a real remount-on-toggle performance bug.** (1)
`.kind-tag.rest`'s color was first moved from violet (`#a78bfa`) to a muted
true yellow (`#d1c85a`) — picked deliberately distinct from the
`tw-acct.tenant` amber (`#e2b86b`, which skews orange: `R-G≈42`) by keeping R
and G close together (`R-G≈9`) so the two tags stay visually distinguishable
at a glance, per the standing "a kind tag must never be mistaken for an
account tag" rule from Phase 28o. The user then asked for it back ("Under the
yellow classification REST color... revert to what it was before") — reverted
to `#a78bfa` violet, same session, no lingering yellow anywhere. Net effect:
REST's color ends this phase exactly where it started; only the KeepAlive fix
below is a lasting change. (2) The real
bug: `RpcPanel.vue`'s `<TabPanel value="traces">` gated `<TraceWaterfall>`
with a bare `v-if="ui.rpcTab === 'traces'"`, so every switch AWAY from and
BACK TO the Traces tab fully destroyed and recreated the component —
re-running its `onMounted` (`bootstrap()`: a fresh `getKvBucketEntries` HTTP
call against the `trace-request-reply` KV bucket, which is unbounded and only
grows over a session; `connectLive()`: a full NATS unsubscribe+resubscribe)
plus recomputing every derived computed (`traceSummaries`,
`displayedSummaries`, `pulse`, `waterfallRows`) from scratch — this was the
user-reported toggling sluggishness. Fix: wrap the same `v-if` in a
`<KeepAlive>`. This preserves the LAZY first mount (so `RpcPanel.spec.js`'s
`mountMessagesTab` helper, which deliberately never sets `rpcTab = 'traces'`
specifically to avoid `TraceWaterfall` racing its own `getKvBucketEntries`
call against that spec file's mock — see the comment at the top of that
file — still holds; a messages-only test run never mounts `TraceWaterfall` at
all), but once mounted, `KeepAlive` caches the instance across a
switch-away instead of unmounting it, so switching back reuses it with zero
repeat fetch/subscribe/recompute. First attempt (reverted) was `v-if` →
`v-show` on both tab panels' wrapper divs — simpler, but broke two
`RpcPanel.spec.js` specs because it unconditionally mounted `TraceWaterfall`
even in `mountMessagesTab`-only tests, racing exactly the mock collision the
original comment warned about. `KeepAlive` was the correct fix because it
gets both properties (lazy first mount + no remount on toggle) that `v-show`
alone only gets one of.

**How to apply:** when a `v-if`-gated child component has its own expensive
`onMounted` (a network fetch, a subscription), and the surrounding UI toggles
that `v-if` repeatedly (a tab, an accordion, a modal reopen), reach for
`<KeepAlive>` around the same `v-if` rather than switching to `v-show` —
`v-show` trades the remount cost for an always-on first mount (which can
break tests/behavior relying on lazy mounting, as it did here) and keeps the
component's subscriptions/timers running even while hidden; `KeepAlive`
keeps the lazy-mount property intact while still avoiding the repeat-mount
cost on every subsequent toggle.
