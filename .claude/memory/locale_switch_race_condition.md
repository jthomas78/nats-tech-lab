---
name: locale-switch-race-condition
description: useUiCopy.js/useRefdataLabels.js fetch-on-locale-switch race — a slow, stale request can silently revert a newer locale switch; fixed with a request-generation token
metadata:
  type: project
---

**Bug pattern found in `shared/refdata/useUiCopy.js` and `useRefdataLabels.js`** (both consumed by `frontend-port` and `frontend`): switching locale while a previous switch's fetch is still in flight (most visible right after mount, before the initial `connect()` fetch settles) starts a second, overlapping fetch. Network responses aren't guaranteed to resolve in request order — if the *older* request happens to resolve *after* the newer one, its callback still unconditionally applies its (now-stale) locale, silently reverting the user's more recent switch. It "self-heals" moments later when the next SSE-triggered refresh reapplies the correct locale — symptom: switching languages is "momentarily ineffectual" shortly after load.

**Fix:** a request-generation token (`let requestToken = 0`, bumped at the start of each fetch, captured locally, checked before applying results) discards any response that's no longer the most recently *started* one. Applied to both composables — same bug, same fix, in `refreshCatalog()` (ui-copy + i18n locale) and `refreshLabels()` (ship-status labels).

**UX complement:** exposed a `switching` ref from `useUiCopy()`, wired to PrimeVue `Select`'s native `:loading` prop (not `:disabled` — `loading` already blocks interaction *and* shows a spinner in one prop, per `node_modules/primevue/select`'s `onContainerClick` guard). This narrows the window where a user could re-trigger overlapping requests, but the token guard is the actual correctness fix — the loading indicator alone doesn't prevent the race (e.g. the initial mount fetch racing an SSE-triggered refresh).

**Not a BR-D business rule** — deliberately not added to `BUSINESS_RULES.md`. Every existing BR-D rule governs domain data behavior enforced in `refdata-service`'s Go domain layer with a Ginkgo spec; this is a frontend async-ordering bug with no domain-layer counterpart, same category as [primevue-radiobutton-group-for-shared-state](primevue_radiobutton_group_for_shared_state.md) and [stale-select-value-on-filter-change](stale_select_value_bug_pattern.md) — both recorded as memory + regression test, not BRs.

**Tests:** `frontend-port/src/useUiCopy.spec.js` (reproduces the exact race — a stale 'en' fetch resolving after a newer 'es' one must not clobber it) and a spinner-visibility spec in `App.spec.js` (asserts `#locale [data-pc-section="loadingicon"]` appears/disappears with `switching`). Both confirmed to fail without their respective fixes before being accepted.

**How to apply:** any composable here that fetches on a reactive "selector" value (locale, context, filter) and has no guard against overlapping in-flight requests has this same latent bug — check for a request-token (or equivalent) guard before trusting first-fetch-after-mount + rapid-switch behavior.
