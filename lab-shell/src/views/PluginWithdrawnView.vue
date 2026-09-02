<script setup>
/* Rendered in place of a route whose plugin the publisher withdrew
   (BR-AS54, BR-AS57).

   In place, and only in place: the occupant keeps their URL. A redirect would
   throw away where they were, and this panel is a better answer than a home
   page they did not ask for. Nobody NEW reaches it — the router guard refuses
   entry — so this is only ever shown to someone who was already standing here.

   No retry. A load failure is worth another attempt; a withdrawal is the
   publisher saying the feature is gone, and a button that re-asked would be
   offering something the shell cannot deliver. Curated metadata only, no
   remote URL (BR-AS04). */
import { computed, inject } from 'vue'
import { useRoute } from 'vue-router'

import { SHELL } from '../shell/shellKey.js'

const shell = inject(SHELL, null)
const route = useRoute()

const pluginId = computed(() => route.meta?.pluginId ?? '')
const manifest = computed(() =>
  shell?.manifestFor?.(pluginId.value) ?? null,
)
</script>

<template>
  <div class="withdrawn-wrap">
    <div class="lab-panel withdrawn">
      <h2>{{ route.meta.title }} is no longer available</h2>
      <p class="lab-muted lead">
        The publisher of this plugin withdrew it. The page you are on is
        unchanged, and nothing else in the shell is affected.
      </p>

      <div class="detail">
        <div>
          <span class="lab-dim">plugin</span> {{ pluginId }}
          <template v-if="manifest?.version">
            {{ manifest.version }}
          </template>
        </div>
        <div>
          <span class="lab-dim">route</span>
          {{ route.meta.contributionId ?? route.name }} &rarr; {{ route.path }}
        </div>
        <div><span class="lab-dim">state</span> withdrawn by publisher</div>
      </div>

      <div class="actions">
        <router-link
          class="btn"
          to="/"
        >
          Back to Home
        </router-link>
        <router-link
          class="btn ghost"
          to="/plugins"
        >
          Plugin status
        </router-link>
      </div>

      <p class="lab-dim note">
        If the publisher restores it unchanged, it comes back on its own — no
        reload needed.
      </p>
    </div>
  </div>
</template>

<style scoped>
.withdrawn-wrap { display: flex; justify-content: center; }
.withdrawn {
  max-width: 640px;
  padding: 22px 24px;
}
h2 { margin: 0; font-size: 16px; line-height: 22px; font-weight: 600; }
.lead { margin: 6px 0 0; font-size: 13px; line-height: 20px; }
.detail {
  margin-top: 16px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  background: var(--lab-nested-bg);
  padding: 10px 12px;
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  line-height: 18px;
  color: var(--p-text-muted-color);
}
.actions { display: flex; gap: 10px; margin-top: 16px; }
.btn {
  display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 12px;
  border: 1px solid transparent; border-radius: 5px; font-size: 12px; font-weight: 600;
  background: var(--lab-accent); color: var(--lab-accent-ink);
  text-decoration: none; cursor: pointer;
}
.btn.ghost {
  background: none; border-color: var(--lab-panel-border); color: var(--p-text-color);
}
.note { margin: 12px 0 0; font-size: 11px; }
</style>
