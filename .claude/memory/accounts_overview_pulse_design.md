---
name: accounts-overview-pulse-design
description: Accounts nav/tabs redesign — Sharing rename, Accounts moved to SYSTEM, Account Activity merged into a new Overview tab with trend charts. IMPLEMENTED Phase 45 (2026-08-18) — see BR-043/BR-044.
metadata:
  type: project
---

**IMPLEMENTED as Main-POC-Plan.md's Phase 45 (2026-08-18).** Everything
below this line is the design-review history that led to it (four mockup
rounds, all decisions the phase's implementation followed). The shipped
version:

- Backend: `observability-service/observability/internal/rest/account_activity_history.go`
  — `AccstatzHistory` (60min ring buffer, 10s poll) + `GET
  /api/nats/account-activity/history?duration=5m|30m|1h` (BR-043).
- Frontend: `AccountsOverviewPanel.vue` (supersedes the deleted
  `AccountActivityPanel.vue`), `SharingPanel.vue` (renamed from
  `TopologyPanel.vue`), `AccountsView.vue`'s three tabs, `App.vue`'s nav
  move. Search gating is BR-044.
- Both rules documented in `BUSINESS_RULES-SHIPPING.md`; `ARCHITECTURE-ADMIN.md`
  updated (§4.3 retitled "relocated", following its own §4.8 precedent for
  a panel that left the SYSTEM · NATS group).
- Verified live against the docker compose stack — real ring-buffer data
  rendering in sparklines/trend charts, duration selector, and search
  gating all confirmed working end-to-end, not just unit-tested.

Design-reviewed (mockups, not yet implemented) restructuring of the Admin UI's
Accounts area:

1. `Topology` tab renamed to `Sharing` — terminology from NATS's cross-account
   security model (declared export/import edges), content unchanged.
2. `Accounts` nav item moves PLATFORM → SYSTEM, first entry (above the NATS
   eyebrow group).
3. SYSTEM · NATS · `Account Activity` retires as a standalone nav item; its
   content becomes a new `Overview` tab — first tab under Accounts, before
   Provisioning.

**Overview tab content — two rounds compared, round 2 chosen:**
- Round 1 (straight port): reused AccountActivityPanel.vue's card list as-is.
  Rejected on review — the per-account expand restated numbers already
  visible in the collapsed row (e.g. "92 conns" in the header, "Connections:
  92" again on expand).
- Round 2 (chosen): collapsed row unchanged (quick-scan snapshot); expand
  replaces the flat number grid with two small trend charts — connections/
  subscriptions line chart, in/out throughput bar chart — plus a plain-
  language read tying the trend to any active alarm (e.g. "inbound has
  outpaced outbound for ~20 min" explains a slow-consumer warning). Fleet
  summary cards each grow a small trend sparkline for the same reason.
- Round 2b (fleet pulse, rejected): no expand/collapse — every account's
  charts always visible, fleet hero cards shrink to a plain-text strip,
  accounts with an active issue sort first. **Rejected 2026-08-18**: doesn't
  scale — occupies too much vertical real estate as more accounts are added.
  Also breaks the collapsed-by-default convention Services/Connections/
  round-1 Account Activity all use elsewhere in this app.

**History source — settled 2026-08-18: a ring buffer.** The backend keeps a
rolling buffer of `/accstatz` samples (raw ~10s-resolution samples covering
the last hour) instead of exposing only the live snapshot — real history,
survives a page reload. Client-side accumulation (starts empty each session)
was the alternative, not chosen.

**Round 3 — duration selector added on top of round 2's design.** A 5m /
30m / 1h toggle (segmented pill control) controls every trend chart on the
tab — fleet summary sparklines and whichever account card is expanded.
Defaults to 30m. Bucket/sample resolution scales with the window (~30s
buckets at 5m, ~2min at 30m, ~5min at 1h) rather than a fixed bucket size,
so shorter windows stay legible instead of collapsing to 1-2 fat bars.
Key property demonstrated in the mockup: the underlying fact ("globex's
inbound has outpaced outbound for ~20 min") reads identically at 30m and 1h
since both windows draw on the same stored samples — only the 5m view reads
differently ("the entire window shown"), because 20 minutes of lag doesn't
fit inside a 5-minute view. That's a real property of short windows to keep
in the copy when implemented, not an artifact to fix.

**Round 4 — search/filter box for the account list, conditionally shown.**
A "Search accounts" text input filters the card list by name (substring,
case-insensitive). Only rendered when there are **more than 3 accounts** —
below that the list is short enough to just read, and a search box above
2-3 rows is a control with nothing to do. Filtering hides non-matching
`.acct-card` rows and shows an in-voice empty state ("No accounts match
"xyz".") rather than a blank list. Not yet built: the same threshold-gated
search on the Provisioning tab's account table, which has the same
few-vs-many shape — flagged as a follow-up, not decided.

**How to apply:** when implementing, build round 4's design (round 3's
duration-windowed trend charts, plus the >3-accounts-gated search box), not
round 2b. Mockups:
`demos/01-dictionary/diagrams/accounts-sharing-overview-mockup.html`
(nav/tab structure), `accounts-overview-pulse-mockup.html` (round 2),
`accounts-overview-fleet-pulse-mockup.html` (round 2b, rejected — kept for
reference on why), `accounts-overview-duration-mockup.html` (round 3),
`accounts-overview-search-mockup.html` (round 4, current direction —
interactive: a "simulate fleet size" demo-only toggle adds 3 more accounts
so the search box's appearance right at the >3 boundary can actually be
watched, then typing filters for real). Round 4's mockup only wires the
duration selector + search to the fleet cards / expanded (globex) card /
account list; per-account charts for platform/acme still aren't hand-built
for all 3 windows. See [[phase21_account_exports_imports]] for the
PLATFORM/tenant account model these panels report on.
