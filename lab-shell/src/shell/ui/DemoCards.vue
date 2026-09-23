<script setup>
/*
  The demo cards on the shell's home screen (BR-AS79, task 16e).

  Shell-owned, and that is the point. The only demo cards in this repository
  before now lived inside the demo-catalog PLUGIN, with its list written by
  hand — a list that is reachable in one plugin source and not the other, and
  that says nothing about whether a demo is actually running. R-1 requires the
  menu-card status to be identical under `build` and under `registry`, so the
  cards have to belong to the shell, and their data has to come from the
  shell-owned demo catalogue.

  They populate WITHOUT anybody opening a demo: `demoStore.startMenuRefresh()`
  probes every catalogued demo at boot and then slowly on a timer. That is a
  different policy from the pre-mount check on purpose — a card is a glance,
  and a glance may be a little old, while a check made because somebody just
  clicked must be fresh.

  A demo with no route contribution is still drawn, with its status and no
  link. That is honest: the demo exists in the lab, and its plugin has not
  been admitted, which is exactly the case where a reader wants to be told
  something rather than shown nothing.
*/
import { computed, inject } from 'vue'

import { demoStatusLabel, demoTone } from '../demos/demoReadinessText.js'
import { SHELL } from '../shellKey.js'

const shell = inject(SHELL)

const cards = computed(() => {
  const demos = shell.demos ?? { byPlugin: {}, results: {} }
  return Object.values(demos.byPlugin).map((entry) => {
    const result = demos.results[entry.pluginId] ?? null
    const route = shell.contributions.routes.find((r) => r.pluginId === entry.pluginId) ?? null
    return {
      key: entry.pluginId,
      name: entry.name,
      /* A route name, never a hand-built path — the same rule the nav follows. */
      to: route ? { name: route.qualifiedId } : null,
      /* No result yet is `unknown`, which is true: nobody has asked. */
      label: demoStatusLabel(result?.state),
      tone: demoTone(result?.state),
    }
  })
})
</script>

<template>
  <div
    v-if="cards.length"
    class="demo-cards"
  >
    <h2>Demos</h2>
    <ul>
      <li
        v-for="card in cards"
        :key="card.key"
        class="demo-card"
      >
        <component
          :is="card.to ? 'router-link' : 'span'"
          v-bind="card.to ? { to: card.to } : {}"
          class="demo-card-name"
        >
          {{ card.name }}
        </component>
        <span
          class="demo-card-status"
          :class="`tone-${card.tone}`"
        >
          <span
            class="dot"
            aria-hidden="true"
          />
          {{ card.label }}
        </span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.demo-cards h2 {
  font-size: 10px; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase;
  color: var(--p-text-disabled-color);
  margin: 0 0 12px;
}
.demo-cards ul { display: grid; gap: 10px; margin: 0; padding: 0; list-style: none; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); }
.demo-card {
  display: flex; align-items: center; justify-content: space-between; gap: 10px;
  padding: 12px 14px; border: 1px solid var(--lab-panel-border); border-radius: 6px;
}
.demo-card-name { font-size: 13px; font-weight: 600; color: var(--p-text-color); text-decoration: none; }
a.demo-card-name:hover { color: var(--lab-accent); }
.demo-card-status { display: inline-flex; align-items: center; gap: 6px; font-size: 11px; color: var(--p-text-muted-color); }
.dot { width: 7px; height: 7px; border-radius: 50%; background: var(--p-text-disabled-color); }
.tone-ok .dot { background: #27c07f; }
.tone-warn .dot { background: #9a7b1e; }
</style>
