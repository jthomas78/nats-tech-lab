---
name: stale-select-value-on-filter-change
description: PrimeVue Select v-model doesn't auto-clear when the chosen value drops out of a reactive option list — watch and reset explicitly
metadata:
  type: project
---

**Bug pattern found in `frontend-port/src/components/ShipsAtPortPanel.vue`:** a PrimeVue `<Select>` bound to a plain-string `v-model` (e.g. `unloadForm.shipID`) keeps its old value when the option list changes and no longer contains it (e.g. switching the selected port changes `dockedShipOptions`). The UI shows the placeholder text (looks unselected), but the underlying reactive value is still truthy — so any `:disabled="!form.shipID"` check still evaluates to enabled, and any submit still sends the stale value.

**Fix applied:** an explicit `watch(() => store.port, () => { /* reset every dependent form field + error state */ })`. There is no built-in PrimeVue behavior that clears a `Select`'s `v-model` when its `:options` array changes — this must always be done manually with a watcher on whatever drives the option list.

**How to apply:** Any time a `<Select>`'s `:options` is a computed derived from some other reactive value (port, ship, filter), and the component also has a submit/action gated on that select being non-empty, check whether a watcher resets the bound value when the upstream driver changes. If not, it's a latent bug — the enablement check will pass with stale data even though the UI looks reset. This bit twice in the same component (unload ship/container form) before being fixed with a `watch`.
