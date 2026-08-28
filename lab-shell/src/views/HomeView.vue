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
import { SHELL_API_VERSION } from '../shell/versions.js'

const shell = inject(SHELL)
const point = 'shell/home-main/v1'
const placed = computed(() => shell.contributions.extensionsFor(point).length)
const capacity = computed(() => shell.extensionPoints.get(point)?.capacity ?? 0)
const enabled = computed(() => shell.inventory.filter((row) => row.status !== 'disabled').length)
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
      <!-- An empty region is a legitimate state, not a broken one, so it says
           what happened and offers the two places worth going next. -->
      <div
        v-if="placed === 0"
        class="empty"
      >
        <h3>No plugins have contributed to this view</h3>
        <p>
          The registry returned no enabled plugin with a contribution for
          <span class="mono">{{ point }}</span>. The shell, its router and the
          built-in catalog are unaffected.
        </p>
        <div class="empty-actions">
          <router-link
            class="btn"
            to="/demos"
          >
            Open the demo catalog
          </router-link>
          <router-link
            class="btn ghost"
            to="/plugins"
          >
            Plugin status
          </router-link>
        </div>
        <p class="mono foot">
          registry rev {{ shell.registry?.revision ?? 'n/a' }} · {{ enabled }} enabled ·
          shell api {{ SHELL_API_VERSION }}
        </p>
      </div>
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
.empty {
  display: flex; flex-direction: column; align-items: center; gap: 14px;
  text-align: center; max-width: 520px; margin: 24px auto;
}
.empty h3 { margin: 0; font-size: 18px; line-height: 24px; font-weight: 600; }
.empty p { margin: 0; font-size: 13px; line-height: 20px; color: var(--p-text-muted-color); }
.empty-actions { display: flex; gap: 10px; margin-top: 4px; }
.btn {
  display: inline-flex; align-items: center; height: 28px; padding: 0 12px;
  border-radius: 5px; font-size: 12px; font-weight: 600; text-decoration: none;
  background: var(--lab-accent); color: var(--lab-accent-ink); border: 1px solid transparent;
}
.btn.ghost { background: none; border-color: var(--lab-panel-border); color: var(--p-text-color); }
.mono { font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace; color: var(--p-text-color); }
.foot { margin-top: 10px; font-size: 11px; color: var(--p-text-disabled-color); }
</style>
