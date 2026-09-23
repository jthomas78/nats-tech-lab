<script setup>
/*
  The intro page for a demo that has no frontend (task 16g).

  Shell-owned, because there is no plugin to own it. It answers the two things
  a reader of the menu actually wants — what is this demo asking, and how do I
  run it — and then hands over to the demo's own written findings rather than
  paraphrasing them here.

  It carries no status of any kind. Nothing probes these demos, so there is
  nothing to show; a dot would be a mark nobody took (decision 8), and a
  readiness state would be a check nobody made (BR-AS79).

  The paths below are repository paths, printed as text. They are deliberately
  not links: nothing serves this repository over HTTP, and a link that cannot
  open is worse than a path that can be copied.
*/
import { computed } from 'vue'
import { useRoute } from 'vue-router'

import { labDemo } from '../shell/demos/labDemos.js'

const route = useRoute()
const demo = computed(() => labDemo(route.params.demo))
</script>

<template>
  <section
    v-if="demo"
    class="lab-demo"
  >
    <header class="page-head">
      <h1>{{ demo.name }}</h1>
      <p class="id">
        {{ demo.id }} · no frontend, so no plugin and no catalogue entry
      </p>
    </header>

    <div class="lab-panel">
      <h3>The question</h3>
      <p class="question">
        {{ demo.question }}
      </p>
      <p class="summary">
        {{ demo.summary }}
      </p>
    </div>

    <div class="lab-panel">
      <h3>Run it</h3>
      <ol class="run">
        <li
          v-for="step in demo.run"
          :key="step.command"
        >
          <span class="step-label">{{ step.label }}</span>
          <code>{{ step.command }}</code>
        </li>
      </ol>
    </div>

    <div class="lab-panel">
      <h3>Findings</h3>
      <ul class="findings">
        <li
          v-for="item in demo.findings"
          :key="item.path"
        >
          <span class="step-label">{{ item.label }}</span>
          <code>{{ item.path }}</code>
        </li>
      </ul>
    </div>
  </section>
  <section
    v-else
    class="lab-demo"
  >
    <h1>Unknown demo</h1>
    <p class="id">
      This shell lists no demo called {{ route.params.demo }}.
    </p>
  </section>
</template>

<style scoped>
.lab-demo { display: flex; flex-direction: column; gap: 18px; }
h1 { margin: 0; font-size: 20px; line-height: 26px; font-weight: 600; }
.id { margin: 2px 0 0; font-size: 12px; color: var(--p-text-muted-color); }
h3 { margin: 0 0 8px; font-size: 13px; font-weight: 600; }
.question { margin: 0; font-size: 14px; color: var(--p-text-color); }
.summary { margin: 8px 0 0; font-size: 13px; color: var(--p-text-muted-color); }
.run, .findings { margin: 0; padding-left: 18px; display: grid; gap: 10px; }
.findings { list-style: none; padding-left: 0; }
.step-label { display: block; font-size: 12px; color: var(--p-text-muted-color); margin-bottom: 3px; }
code {
  display: inline-block; font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px; color: var(--p-text-color);
  background: color-mix(in srgb, var(--lab-panel-border) 35%, transparent);
  border-radius: 4px; padding: 2px 6px;
}
</style>
