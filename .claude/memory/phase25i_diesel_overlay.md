---
name: phase25i_diesel_overlay
description: Phase 25i diesel overlay DONE 2026-08-07; two live bugs found and fixed during smoke test — BR-P24 zero-baseline corruption, DatePicker UTC-offset shift
metadata:
  type: project
---

Phase 25i (effective-dated diesel overlay for `RateSheet`, BR-P17–BR-P24) is
complete as of 2026-08-07: domain, Postgres, REST/browserrpc, and the
Pricing-tab frontend (`DieselPricePanel.vue` + `RateSheetPanel.vue`
overlay UI) are all live-verified via `docker compose`.

Two real bugs were found and fixed during the live smoke test, not caught
by the 40 pre-existing Ginkgo specs:

- **BR-P24 — zero-baseline corruption.** `domain.AdjustedRate()` divided by
  `RateSheetEntry.InitialDieselCents` with no guard. Every pre-25i seeded
  rate sheet entry has `InitialDieselCents == 0` (the zero-value default),
  so applying a diesel overlay to them produced `NaN` that Go silently
  converted to `centAdjustedRate: 0` — corrupting the price to $0 instead
  of erroring. Confirmed live via a direct REST call on `acme-standard`.
  **Why:** the specs only ever construct entries with an authored baseline,
  so the zero-baseline path was never exercised until real (seeded) data
  hit it. **Fix (user-decided):** `AppendDieselOverlay` now skips
  appending an overlay for any entry with `InitialDieselCents <= 0`; that
  entry keeps resolving to its base rate via the existing BR-P23 fallback.
  3 new specs in `rate_sheet_diesel_test.go`.
- **DatePicker UTC-offset shift.** `registerForm.activeDate.toISOString()`
  on a PrimeVue `DatePicker`'s local-midnight `Date` shifts the calendar
  day backward in any positive-UTC-offset timezone — reproduced live in
  this sandbox (`Africa/Johannesburg`, UTC+2: picking Aug 15 sent Aug 14).
  **Fix:** a `dateOnlyISOString(date)` helper re-anchors at
  `Date.UTC(y, m, d)` before calling `.toISOString()`, applied in both
  `DieselPricePanel.vue` and `RateSheetPanel.vue`. **How to apply:** any
  future PrimeVue `DatePicker` → server-date payload in this codebase
  needs the same re-anchor, not a plain `.toISOString()`.

**How to apply generally:** live docker-compose smoke tests against
already-seeded (pre-feature) data are worth doing even when the Ginkgo
suite is 100% green — both bugs here were edge cases the seed data
happened to trigger that synthetic test fixtures didn't.

25j (FixedRate overlay, same pattern) is still not started.
