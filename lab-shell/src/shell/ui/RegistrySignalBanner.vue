<script setup>
/*
  The one place a registry change reaches the user (BR-AS19, decision 25).

  It OFFERS a reload; it never performs one, and it never unmounts anything.
  A plugin whose entry was withdrawn is still running behind this bar, which
  is why the copy says "is still running" rather than apologising for it —
  `active` has no exit transition, so the change genuinely does wait.
*/
import { computed, inject, ref } from 'vue'

import { RELOAD_REASON } from '../registry/registryDiff.js'
import { SHELL } from '../shellKey.js'

const shell = inject(SHELL)
const dismissed = ref(false)

const pending = computed(() => shell.pendingReload ?? [])
const visible = computed(() => pending.value.length > 0 && !dismissed.value)

const summary = computed(() => {
  const removed = pending.value.filter((p) => p.reason === RELOAD_REASON.REMOVED)
  const moved = pending.value.filter((p) => p.reason === RELOAD_REASON.REMOTE_CHANGED)
  const parts = []
  /* Named, not counted, while the list is short: "Fleet Ops" tells the user
     whether the thing they are standing in is the thing that changed. */
  if (removed.length) parts.push(`${label(removed)} withdrawn from the catalog`)
  if (moved.length) parts.push(`${label(moved)} now served from a different build`)
  return parts.join('; ')
})

function label(entries) {
  if (entries.length === 1) return entries[0].name || entries[0].id
  return `${entries.length} plugins`
}

/* Deliberately the browser's own reload: the shell has no teardown path that
   could reproduce a clean boot, and pretending otherwise is how a half-applied
   registry change would get shipped. */
const reload = () => globalThis.location?.reload?.()
</script>

<template>
  <div
    v-if="visible"
    class="signal"
    data-testid="registry-signal"
    role="status"
  >
    <i class="pi pi-sync" />
    <span class="text">
      <b>The plugin catalog changed.</b>
      <span data-testid="registry-signal-summary"> {{ summary }}. Still running until you reload.</span>
    </span>
    <span
      class="rev"
      data-testid="registry-signal-revision"
    >rev {{ shell.registry?.revision ?? 'n/a' }}</span>
    <button
      type="button"
      class="ghost"
      data-testid="registry-signal-dismiss"
      @click="dismissed = true"
    >
      Not now
    </button>
    <button
      type="button"
      class="solid"
      data-testid="registry-signal-reload"
      @click="reload"
    >
      Reload
    </button>
  </div>
</template>

<style scoped>
.signal {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 16px;
  padding: 8px 12px;
  font-size: 12px;
  border: 1px solid var(--lab-panel-border);
  border-left: 2px solid var(--lab-accent);
  border-radius: 4px;
  background: var(--lab-panel-bg, #1a1e23);
}
.signal i {
  color: var(--lab-accent);
}
.text {
  flex: 1;
}
.rev {
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 10px;
  color: var(--p-text-disabled-color);
}
button {
  font: inherit;
  font-weight: 600;
  border-radius: 3px;
  padding: 3px 12px;
  cursor: pointer;
}
.ghost {
  background: transparent;
  border: 1px solid var(--lab-panel-border);
  color: var(--p-text-muted-color);
}
.solid {
  background: var(--lab-accent);
  border: 1px solid var(--lab-accent);
  color: #fff;
}
</style>
