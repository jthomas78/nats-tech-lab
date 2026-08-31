<script setup>
/*
  The one place a registry change reaches the user (BR-AS19, decision 25).

  For an ordinary catalogue change it OFFERS a reload; it never performs one,
  and it never unmounts anything. A plugin whose entry was withdrawn is still
  running behind this bar, which is why the copy says "is still running"
  rather than apologising for it — `active` has no exit transition, so the
  change genuinely does wait.

  A REVOCATION is the one exception (decision 100, BR-AS49). The publisher key
  that signed the plugin has been revoked, so the code on screen is code the
  platform has withdrawn trust from, and waiting for a click is the worse
  option. This bar reloads on its own and offers nothing to dismiss.

  It is still only a reload, and the promise is only what decision 100
  promises: the plugin stops at the next paint. An in-flight callback is not
  interrupted, and nothing here isolates a plugin's runtime.
*/
import { computed, inject, ref, watch } from 'vue'

import { RELOAD_REASON } from '../registry/registryDiff.js'
import { SHELL } from '../shellKey.js'

const shell = inject(SHELL)
const dismissed = ref(false)

const pending = computed(() => shell.pendingReload ?? [])
const forced = computed(() => pending.value.some((p) => p.forced === true))
const visible = computed(() => pending.value.length > 0 && (forced.value || !dismissed.value))

/* The revision the served document was taken at, shown only when the service
   said the read was degraded (BR-AS51). Stale trust presented as current is
   the thing decision 105 refused to ship. */
const degraded = computed(() => shell.registry?.degraded === true)

const summary = computed(() => {
  const of = (reason) => pending.value.filter((p) => p.reason === reason)
  const removed = of(RELOAD_REASON.REMOVED)
  const moved = of(RELOAD_REASON.REMOTE_CHANGED)
  /* Decision 46 made this the common case rather than the exotic one: any
     curated edit that is not a new id lands here, so a missing clause would
     leave the banner announcing a change and then naming nothing. */
  const edited = of(RELOAD_REASON.CHANGED)
  const revoked = of(RELOAD_REASON.REVOKED)
  const parts = []
  /* First, and in its own words. "Withdrawn from the catalog" is what an
     operator did; this is what the platform did, and the difference is the
     whole reason the bar is not asking. */
  if (revoked.length) parts.push(`${label(revoked)} withdrawn by the platform`)
  /* Named, not counted, while the list is short: "Fleet Ops" tells the user
     whether the thing they are standing in is the thing that changed. */
  if (removed.length) parts.push(`${label(removed)} withdrawn from the catalog`)
  if (moved.length) parts.push(`${label(moved)} now served from a different build`)
  if (edited.length) parts.push(`${label(edited)} edited in the catalog`)
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

/* Immediate and not deferred to a click. `immediate` matters: the banner is
   usually mounted before the revocation arrives, but a shell that boots into
   an already-revoked document must reload too. */
watch(forced, (isForced) => {
  if (isForced) reload()
}, { immediate: true })
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
      <!-- One line on purpose: Vue's whitespace condensing drops the gap
           between two elements separated by a newline, which ran the bold
           sentence into the summary. -->
      <b v-if="forced">The platform withdrew a plugin.</b>
      <b v-else>The plugin catalog changed.</b>
      <span
        v-if="forced"
        data-testid="registry-signal-summary"
      >{{ summary }}. Reloading now.</span>
      <span
        v-else
        data-testid="registry-signal-summary"
      >{{ summary }}. Still running until you reload.</span>
    </span>
    <span
      v-if="degraded"
      class="rev stale"
      data-testid="registry-signal-degraded"
    >degraded, as of revision {{ shell.registry?.revision ?? 'n/a' }}</span>
    <span
      v-else
      class="rev"
      data-testid="registry-signal-revision"
    >rev {{ shell.registry?.revision ?? 'n/a' }}</span>
    <button
      v-if="!forced"
      type="button"
      class="ghost"
      data-testid="registry-signal-dismiss"
      @click="dismissed = true"
    >
      Not now
    </button>
    <button
      v-if="!forced"
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
.stale {
  color: var(--lab-warning, #9a7b1e);
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
