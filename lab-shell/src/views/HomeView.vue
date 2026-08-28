<script setup>
/*
  The shell's home screen — the host of `shell/home-main/v1`.

  It is shell-owned rather than a plugin, and that is consistent with BR-AS09
  rather than an exception to it: the shell owns the frame and the regions it
  offers, and this screen is one of those regions plus the sentence explaining
  what fills it. It has no feature content of its own and imports no plugin.
*/
import { computed, inject } from 'vue'

import ExtensionRegion from '../shell/ui/ExtensionRegion.vue'
import { SHELL } from '../shell/shellKey.js'

const shell = inject(SHELL)
const point = 'shell/home-main/v1'
const placed = computed(() => shell.contributions.extensionsFor(point).length)
const capacity = computed(() => shell.extensionPoints.get(point)?.capacity ?? 0)
</script>

<template>
  <section class="home">
    <header class="home-head">
      <h1>NATS Tech Lab</h1>
      <p>
        An application shell that knows nothing about any feature. Everything
        below arrived from a plugin.
      </p>
    </header>

    <div class="home-region">
      <h2>
        {{ point }}
        <span class="cap">{{ placed }} of {{ capacity }} placed</span>
      </h2>
      <ExtensionRegion
        :point="point"
        :context="{ region: point }"
      />
      <p
        v-if="placed === 0"
        class="empty"
      >
        No plugin contributes a panel here yet.
      </p>
    </div>
  </section>
</template>

<style scoped>
.home { display: flex; flex-direction: column; gap: 22px; }
.home-head h1 { font-size: 20px; margin: 0 0 6px; }
.home-head p { margin: 0; color: var(--p-text-muted-color); max-width: 720px; }
.home-region h2 {
  font-size: 10px; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase;
  color: var(--p-text-disabled-color);
  display: flex; align-items: baseline; gap: 10px;
  margin: 0 0 12px;
}
.cap { font-weight: 500; letter-spacing: 0; text-transform: none; font-size: 11px; }
.empty { color: var(--p-text-disabled-color); margin: 0; }
</style>
