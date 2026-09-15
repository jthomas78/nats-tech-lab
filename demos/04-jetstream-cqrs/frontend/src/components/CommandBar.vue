<script setup>
// Task 04.6.6 — the only part of this screen that writes.
//
// Three commands, all visible at once, and NONE of them is ever disabled by a
// business rule. That is the design, not an oversight: pressing "Record trip"
// on a retired vehicle is how the demo shows BR-OD04 refusing, and a greyed
// out button would hide the rule behind JavaScript that claims to know it.
// The only thing that disables a button is a command already in flight.
//
// What comes back is rendered as one of three kinds:
//
//   accepted  the domain said yes, and gave the sequence it was appended at
//   refused   the domain said no, and named the rule
//   broken    the plumbing failed — no rule code, ever
//
// The panel shows no vehicle state of its own. After an accepted command the
// proof is on the read path below: a new row in the log, then the write marker
// moving, then the read marker following it.
import { computed, ref, watch } from 'vue'

import Button from 'primevue/button'

import { COMMAND_API } from '../config.js'
import { COMMANDS } from '../commands/api.js'
import { formatClock } from '../view/format.js'

const props = defineProps({
  // The vehicle selected in the rail, or null. It only prefills the box — the
  // box stays editable, because registering a NEW vehicle means typing an id
  // that is not in the rail yet.
  vehicle: { type: String, default: null },
  pending: { type: String, default: '' },
  outcomes: { type: Array, default: () => [] },
})

const emit = defineEmits(['run'])

const id = ref(props.vehicle ?? '')
const plate = ref('')
const km = ref('')
const reason = ref('')

watch(
  () => props.vehicle,
  (next) => {
    if (next) id.value = next
  },
)

const values = { plate, km, reason }

const PLACEHOLDERS = {
  plate: 'CA 41-208',
  km: '42.0',
  reason: 'scrapped',
}

// The id is the one field the shim itself requires — serve.go answers 400
// "id is required" without ever reaching the domain. Blocking on it locally is
// not a business rule, it is not sending an empty form.
const ready = computed(() => id.value.trim() !== '')

function run(name) {
  emit('run', name, {
    id: id.value,
    plate: plate.value,
    km: km.value,
    reason: reason.value,
  })
}

const KINDS = {
  accepted: { label: 'accepted', hint: 'the domain said yes' },
  refused: { label: 'refused', hint: 'a rule said no — this is the demo working' },
  broken: { label: 'broken', hint: 'the plumbing failed, not a rule' },
}
</script>

<template>
  <section
    class="commands"
    data-testid="command-bar"
  >
    <header>
      <p class="eyebrow">
        Write side · POST {{ COMMAND_API }}/commands/…
      </p>
      <span class="tag">commands only — nothing is read back through here</span>
    </header>

    <label class="idbox">
      <span>vehicle id</span>
      <input
        v-model="id"
        data-testid="command-id"
        type="text"
        spellcheck="false"
        placeholder="truck-7"
        autocomplete="off"
      >
    </label>

    <ul class="rows">
      <li
        v-for="c in COMMANDS"
        :key="c.name"
      >
        <Button
          :label="c.label"
          size="small"
          severity="secondary"
          :loading="pending === c.name"
          :disabled="!ready || (pending !== '' && pending !== c.name)"
          :data-testid="`command-${c.name}`"
          @click="run(c.name)"
        />
        <label
          v-for="f in c.fields"
          :key="f"
          class="field"
        >
          <span>{{ f }}</span>
          <input
            v-model="values[f].value"
            :data-testid="`field-${f}`"
            type="text"
            spellcheck="false"
            autocomplete="off"
            :placeholder="PLACEHOLDERS[f]"
          >
        </label>
        <span class="note">{{ c.note }}</span>
      </li>
    </ul>

    <p class="rule">
      No button here is ever greyed out by a rule. A trip of 0&nbsp;km, a second
      register, a trip on a retired vehicle — all of them are sent, and all of
      them are refused by <code>domain.go</code>, not by this page.
    </p>

    <ol
      v-if="outcomes.length"
      class="outcomes"
      data-testid="outcomes"
    >
      <li
        v-for="(o, i) in outcomes"
        :key="`${o.at?.getTime?.() ?? i}-${i}`"
        :class="o.kind"
      >
        <span
          class="kind"
          :title="KINDS[o.kind].hint"
        >{{ KINDS[o.kind].label }}</span>
        <code class="what">{{ o.command }} {{ o.id }}</code>
        <code
          v-if="o.rule"
          class="rulecode"
          data-testid="outcome-rule"
        >{{ o.rule }}</code>
        <code
          v-if="o.error"
          class="errname"
        >{{ o.error }}</code>
        <span class="msg">{{ o.message }}</span>
        <span class="at">{{ formatClock(o.at) }}</span>
      </li>
    </ol>
    <p
      v-else
      class="empty"
    >
      Nothing sent yet. A command typed in the terminal lands in the same
      stream — the log below does not care which one sent it.
    </p>
  </section>
</template>

<style scoped>
.commands {
  margin-top: 20px;
  padding: 14px 16px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  background: var(--lab-panel-bg);
  border-top: 2px solid var(--d4-write);
}

header {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.eyebrow {
  margin: 0;
  overflow-wrap: anywhere;
}

.tag {
  margin-left: auto;
  padding: 2px 8px;
  border: 1px solid color-mix(in srgb, var(--d4-write) 40%, var(--lab-panel-border));
  border-radius: 999px;
  color: var(--d4-write);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  white-space: nowrap;
}

.idbox,
.field {
  display: flex;
  align-items: center;
  gap: 8px;
}

.idbox {
  margin-top: 14px;
}

.idbox > span,
.field > span {
  color: var(--p-text-disabled-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
}

input {
  padding: 5px 9px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 4px;
  background: var(--p-content-background);
  color: var(--p-text-color);
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 12px;
  width: 200px;
}

input:focus-visible {
  outline: 2px solid var(--p-primary-color);
  outline-offset: 1px;
}

.rows {
  list-style: none;
  margin: 12px 0 0;
  padding: 0;
  display: grid;
  gap: 8px;
}

.rows li {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

/* The three buttons line up, so the eye reads a column of actions rather than
   three unrelated controls. */
.rows li > :deep(.p-button) {
  min-width: 118px;
  justify-content: center;
}

.note,
.rule,
.empty {
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.rule {
  max-width: 76ch;
  margin: 14px 0 0;
}

.rule code {
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
}

.outcomes {
  list-style: none;
  margin: 12px 0 0;
  padding: 10px 0 0;
  border-top: 1px solid var(--lab-panel-border);
  display: grid;
  gap: 6px;
  font-size: 12px;
}

.outcomes li {
  display: flex;
  align-items: baseline;
  gap: 10px;
  flex-wrap: wrap;
}

.kind {
  min-width: 68px;
  padding: 1px 8px;
  border: 1px solid var(--lab-panel-border);
  border-radius: 999px;
  font-size: 11px;
  text-align: center;
}

.outcomes li.accepted .kind {
  color: var(--d4-read);
  border-color: color-mix(in srgb, var(--d4-read) 45%, var(--lab-panel-border));
}

/* A refusal is not an error. It is warning-coloured because the demo is
   working when it happens. */
.outcomes li.refused .kind {
  color: var(--warn);
  border-color: color-mix(in srgb, var(--warn) 45%, var(--lab-panel-border));
}

.outcomes li.broken .kind {
  color: var(--err);
  border-color: color-mix(in srgb, var(--err) 45%, var(--lab-panel-border));
}

.what,
.rulecode,
.errname,
.at {
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
}

.rulecode {
  padding: 1px 6px;
  border-radius: 3px;
  background: color-mix(in srgb, var(--d4-write) 18%, transparent);
  color: var(--d4-write);
}

.errname {
  color: var(--p-text-muted-color);
}

.msg {
  color: var(--p-text-muted-color);
}

.at {
  margin-left: auto;
  color: var(--p-text-disabled-color);
}

.empty {
  margin: 12px 0 0;
  padding-top: 10px;
  border-top: 1px solid var(--lab-panel-border);
  max-width: 76ch;
}
</style>
