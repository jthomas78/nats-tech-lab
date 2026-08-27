<script setup>
// BR-061 — the one way an NKey is drawn in the Admin UI.
//
// Renders `elideNKey()`'s `[FIRST5...LAST5]`, and nothing else: there is no
// prop that expands it, and no `title` carrying the full value. That absence is
// the rule. It replaced three tooltips (ConnectionsPanel's Account and
// Credential cells, UsersPanel's name cell) that each hung 56 characters off a
// hover, which put the key on screen while claiming it was hidden.
//
// `copyable` is for DETAIL PANES ONLY. The rule is that a key is never *shown*
// in full, not that it can never be *obtained* — a pane is where an operator
// goes to fetch a key for `nsc`, and removing the full render without giving
// them any way to get it would make the pane worse at its actual job. The
// clipboard gets all 56 characters; the screen never does. Table cells pass no
// `copyable`: a column exists to let you recognise a row, not to extract a key
// from one, and a copy control on every row would be noise in a dense table.
import { ref } from 'vue'

import { elideNKey } from '../format'

const props = defineProps({
  value: { type: String, default: '' },
  // Show the click-to-copy control. Detail panes only — see above.
  copyable: { type: Boolean, default: false },
  // What to render when there is no key at all. An em-dash reads as "this row
  // genuinely has none"; empty brackets would read as a rendering fault.
  empty: { type: String, default: '—' },
})

const copied = ref(false)
let timer = null

async function copy() {
  try {
    await navigator.clipboard.writeText(props.value)
    copied.value = true
    clearTimeout(timer)
    // Long enough to read, short enough that the pane doesn't keep claiming a
    // copy that happened a minute ago.
    timer = setTimeout(() => (copied.value = false), 1500)
  } catch {
    // A denied or unavailable clipboard is not worth a toast: the operator can
    // see nothing happened, and there is no fallback that doesn't put the full
    // key back on screen — which is the one thing this rule forbids.
  }
}
</script>

<template>
  <span v-if="!value" class="lab-muted">{{ empty }}</span>
  <span v-else class="nkey">
    <span class="nk" data-testid="nkey">{{ elideNKey(value) }}</span>
    <button
      v-if="copyable"
      type="button"
      class="nk-copy"
      :aria-label="copied ? 'Copied' : 'Copy full NKey'"
      @click.stop="copy"
    >{{ copied ? 'copied' : 'copy' }}</button>
  </span>
</template>

<style scoped>
.nkey {
  display: inline-flex;
  align-items: baseline;
  gap: 6px;
}

/* Quieter than the value beside it. The key is the *secondary* half of the
   pair — the name labels, the key only identifies — and it is scanned for a
   match rather than read, so it recedes until looked for.

   Size and tone were settled separately. 9px is a second step down from the
   10px this started at, because at 10px the token still competed with the name
   beside it. The colour is NOT walked down to match: a run at #6b7079 put it
   near 3.2:1 on the panel background and was reverted the same day, so
   #8a9099 stands as the floor — one step below `--p-text-muted-color`, still
   legible character-against-character. Dimming is by colour rather than an
   opacity on the token, which would take the ten significant characters down
   along with the brackets. */
.nk {
  font-family: var(--lab-mono, ui-monospace, SFMono-Regular, Menlo, monospace);
  font-size: 9px;
  letter-spacing: 0.02em;
  color: #8a9099;
  white-space: nowrap;
}

.nk-copy {
  border: 0;
  background: none;
  padding: 0;
  font: inherit;
  font-size: 10px;
  color: var(--p-text-muted-color);
  cursor: pointer;
  opacity: 0.7;
}

.nk-copy:hover,
.nk-copy:focus-visible {
  opacity: 1;
  color: var(--lab-accent, #006fff);
}
</style>
