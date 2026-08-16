<script setup>
import { computed } from 'vue'

// Renders a NATS subject as segmented chips so the dot-hierarchy reads at a
// glance:  emea.events.acme.ship.«MV-AURELIA».arrived
// Heuristic for the shipping domain: the last segment is the event verb, the
// segment before it is the entity id. Everything else is routing context.
const props = defineProps({
  subject: { type: String, default: '' },
  // When true, each segment is clickable and emits 'token-click' with its
  // position (Phase 17b) — used by RpcPanel to build positional facet
  // filters from api.*/rpc.* subjects (fixed 6-token arity: family, context,
  // service, entity, action, version; see ARCHITECTURE-COMMUNICATIONS.md §2
  // decision 4). Off by default so StreamView's plain evt.* display, which
  // doesn't share that fixed arity, is unaffected.
  clickable: { type: Boolean, default: false },
})

const emit = defineEmits(['token-click'])

const segments = computed(() => {
  if (!props.subject) return []
  // A REST path (httpTraceMiddleware's span subject, always "/"-prefixed —
  // Phase 28o) has no NATS dot-hierarchy at all, so the verb/id heuristic
  // below misreads it: with no dots, index 0 is simultaneously "the last
  // segment," so the WHOLE path got marked verb and rendered in the accent
  // blue reserved for a NATS subject's action token. Now that the kind tag
  // (rest/nats) already carries that distinction, a REST path renders as
  // one plain segment instead — no verb/id highlighting, which only ever
  // made sense for the dot-hierarchy a NATS subject actually has.
  if (props.subject.startsWith('/')) {
    return [{ text: props.subject, path: true }]
  }
  const parts = props.subject.split('.')
  return parts.map((text, i) => ({
    text,
    verb: i === parts.length - 1,
    id: i === parts.length - 2 && parts.length >= 2,
  }))
})

function onTokenClick(seg, i, evt) {
  if (!props.clickable) return
  evt.stopPropagation() // don't also trigger a parent row's own click (e.g. row selection)
  emit('token-click', { index: i, text: seg.text })
}
</script>

<template>
  <span class="subject" :title="subject">
    <template v-for="(seg, i) in segments" :key="i">
      <span class="sep" v-if="i > 0">.</span>
      <span
        class="seg"
        :class="{ verb: seg.verb, id: seg.id, path: seg.path, clickable: props.clickable }"
        @click="onTokenClick(seg, i, $event)"
      >{{ seg.text }}</span>
    </template>
  </span>
</template>

<style scoped>
.subject {
  display: inline-flex;
  align-items: center;
  font-family: ui-monospace, 'SF Mono', 'JetBrains Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  flex-wrap: wrap;
}
.seg {
  color: var(--p-text-muted-color);
}
.seg.id {
  color: var(--p-text-color);
  background: rgba(255, 255, 255, 0.04);
  border-radius: 3px;
  padding: 0 4px;
}
:root:not(.p-dark) .seg.id {
  background: rgba(0, 0, 0, 0.05);
}
.seg.verb {
  color: var(--lab-accent);
  font-weight: 600;
}
/* A REST path carries no NATS verb — the kind tag already says "rest", so
   this renders as plain text (the same weight/color a NATS subject's own
   non-verb segments already use), not the accent blue reserved for a NATS
   action token. */
.seg.path {
  color: var(--p-text-color);
}
.sep {
  color: var(--p-text-disabled-color);
  padding: 0 1px;
}
.seg.clickable {
  border-radius: 3px;
  padding: 0 2px;
  cursor: pointer;
}
.seg.clickable:hover {
  background: rgba(0, 111, 255, 0.18);
  color: var(--p-text-color);
}
</style>
