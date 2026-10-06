<script setup>
// The rig's lifecycle, its owner, and the service's own reachability. Two
// different things: a service that does not answer says nothing about the
// rig, so the rig line then shows the last state it reported, with its age.
import { computed } from 'vue'

import { hms, pendingOf, secsSince } from '../usePlayground.js'

const props = defineProps({
  state: { type: Object, required: true },
  actions: { type: Object, required: true },
})

const snap = computed(() => props.state.snap)
const rig = computed(() => snap.value?.rig ?? { status: 'unknown', servers: [] })
const procs = computed(() => snap.value?.summary?.processes ?? { running: 0, stopped: 0, gone: 0, unknown: 0 })
const reachable = computed(() => props.state.service.reachable === true)

const lifecycle = computed(() =>
  ['start', 'attach', 'stop'].map((k) => pendingOf(snap.value, k)).find(Boolean),
)

const STATUS = {
  absent: ['Rig not running', '', 'Start the managed T4 rig, or attach to one that is running.'],
  starting: ['Starting', 'acc', 'lab/rig-t4.sh up, then a check of all nine processes and the meta group.'],
  ready: ['Ready', 'ok', ''],
  stopping: ['Stopping', 'warn', 'SIGCONT, then SIGTERM, to the nine verified processes.'],
  partial: ['Partly stopped', 'err', ''],
  refused: ['Control refused', 'err', ''],
  unknown: ['Rig state unknown', '', 'No answer from the control service yet.'],
}

const head = computed(() => {
  const [title, dot, text] = STATUS[rig.value.status] ?? STATUS.unknown
  let small = text
  if (rig.value.status === 'ready') {
    small = rig.value.owner === 'owned'
      ? 'Owned: this service started the rig, and stops it when it shuts down.'
      : 'Attached: this service did not start the rig, and never stops it.'
  }
  if (rig.value.error) small = rig.value.error
  if (lifecycle.value) {
    const s = secsSince(props.state, lifecycle.value.at)
    small = `${lifecycle.value.kind}: ${lifecycle.value.progress || 'running'} (${s?.toFixed(1)} s of ${(lifecycle.value.limitNs / 1e9).toFixed(0)} s)`
  }
  return { title, dot, small }
})

const verified = computed(() => (rig.value.servers ?? []).filter((s) => s.verified).length)

const canStartOrAttach = computed(() => reachable.value && rig.value.status !== 'ready' && !lifecycle.value)
const canStop = computed(() => reachable.value && rig.value.status === 'ready' && rig.value.owner === 'owned' && !lifecycle.value)

// Enabled whenever a cluster is dark or going dark, or any process is
// stopped — also while other commands are pending. Resume always wins.
const canRestore = computed(() => {
  if (!reachable.value) return false
  const cl = snap.value?.clusters ?? []
  return cl.some((c) => c.wanted || c.stopped > 0) || procs.value.stopped > 0
})

const service = computed(() => {
  const s = props.state.service
  if (s.reachable === null) return { cls: '', dot: '', text: 'Asking the control service…' }
  if (s.reachable) return { cls: '', dot: 'ok', text: 'Control service answering' }
  const since = s.lastOkAt ? `; last answer at ${hms(s.lastOkAt)}` : '; never answered'
  return { cls: 'down', dot: 'err', text: `Control service unreachable${since}` }
})
</script>

<template>
  <div
    class="rigbar"
    data-testid="rigbar"
  >
    <div class="rig-state">
      <div class="big">
        <span
          class="dot"
          :class="head.dot"
        />
        <span data-testid="rig-status">{{ head.title }}</span>
        <span
          v-if="rig.owner"
          class="pill"
        >{{ rig.owner }}</span>
        <span
          v-if="lifecycle"
          class="spin"
        />
      </div>
      <div class="small">
        {{ head.small }}
      </div>
    </div>
    <div class="rig-procs">
      Processes, {{ verified }} of 9 verified<br>
      <b>{{ procs.running }} running, {{ procs.stopped }} stopped</b>
    </div>
    <button
      class="btn primary"
      data-testid="start"
      :disabled="!canStartOrAttach"
      @click="actions.startRig()"
    >
      Start rig
    </button>
    <button
      class="btn"
      data-testid="attach"
      :disabled="!canStartOrAttach"
      @click="actions.attach()"
    >
      Attach
    </button>
    <button
      class="btn"
      data-testid="stop"
      :disabled="!canStop"
      :title="rig.owner === 'attached' ? 'This service did not start the rig, so it does not stop it' : ''"
      @click="actions.stopRig()"
    >
      Stop rig
    </button>
    <span class="vsep" />
    <button
      class="btn warnb"
      data-testid="restore"
      :disabled="!canRestore"
      title="Sends SIGCONT to every stopped rig process. Works while other commands are pending."
      @click="actions.restore()"
    >
      Restore all frozen clusters
    </button>
    <span class="vsep" />
    <span
      class="svc"
      :class="service.cls"
      data-testid="service"
    >
      <span
        class="dot"
        :class="service.dot"
      />{{ service.text }}
    </span>
  </div>
</template>
