---
name: primevue-radiobutton-group-for-shared-state
description: Standalone PrimeVue v4 RadioButtons in a v-for each keep independent local checked state — wrap in RadioButtonGroup to share one source of truth
metadata:
  type: project
---

**Bug pattern found in `frontend-dict/src/components/LocalizationView.vue`** (Phase 11.9's locale-default radio column): a PrimeVue v4 `<RadioButton>` rendered once per row (one per locale), each bound independently via `:model-value="store.defaultLocale"` / `@update:model-value="makeDefault(data.locale)"`, produced a state where **both** the old and newly-clicked locale showed as checked simultaneously.

**Root cause:** PrimeVue's `BaseEditableHolder` mixin gives every component instance its own local `d_value`, initialized from (and watched against) its own `modelValue` prop. Clicking a radio calls `writeValue()` on *that instance only*, which optimistically sets its own `d_value` immediately — before the async round-trip (`addLocale` POST → `refreshLocales` GET) resolves and the shared `store.defaultLocale` prop change propagates back down to every row's watcher. During that window (and it can appear "stuck" if the fetch is slow), the previously-checked row hasn't been told to uncheck yet, so two rows read as checked at once. This is not a backend issue — the Postgres set-default transaction was already atomic (see [ui-bug-triage-trust-framing](ui_bug_triage_trust_framing.md)).

**Fix:** wrap the whole radio column (or the whole surrounding table) in PrimeVue's `<RadioButtonGroup :model-value="..." @update:model-value="...">`. It centralizes `d_value` in one shared instance via `provide`/`inject` (`$pcRadioButtonGroup`), so every child `<RadioButton>` reads/writes the same value — no more per-row local state to go stale. `RadioButtonGroup`'s root div defaults to `display: inline-flex`, which can disturb surrounding layout (e.g. wrapping a `<DataTable>`); neutralize it with `:deep(.p-radiobutton-group) { display: contents; }` in scoped CSS.

**How to apply:** Any time a PrimeVue `RadioButton` (or likely `Checkbox`/similar controlled input) is rendered in a `v-for` with each instance independently bound to the *same* shared reactive value (rather than each instance owning its own distinct value), check whether they're wrapped in the corresponding `*Group` component. If not, the optimistic-local-state race described above is latent even if it hasn't been noticed yet — it depends on network timing to surface.
