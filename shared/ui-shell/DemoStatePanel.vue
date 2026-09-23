<script setup>
// One presentation of "this demo is not usable right now" (BR-AS79, task 16e).
//
// It lives in `shared/ui-shell/` because BOTH sides draw it and they must not
// diverge:
//
//   - the SHELL draws it BEFORE a plugin mounts, from a readiness check;
//   - a PLUGIN draws it AFTER it has mounted, when its own connection drops.
//
// Those are different owners at different moments, and before this component
// they would have been two panels that slowly grew apart — the same outage
// looking like two different products depending on when the reader arrived.
//
// It renders words, never decides them. Every string arrives as a prop:
// the shell writes its own in `demoReadinessText.js`, a plugin writes its own
// for its own failure, and nothing a demo's BACKEND sends is ever shown here.
// `items` is the one exception, and it is rendered as a list of labels, never
// spliced into a sentence, for exactly that reason.
//
// It has no dark-mode block of its own: it draws on the shared UniFi tokens,
// so it follows `.p-dark` with the rest of the shell.
const props = defineProps({
  // 'ok' | 'warn' | 'off' — the same three tones the health dots use.
  tone: { type: String, default: 'off' },
  headline: { type: String, required: true },
  detail: { type: String, default: '' },
  // Identifiers, not prose: the failing check names, shown as chips.
  items: { type: Array, default: () => [] },
  // A command the reader may run. Whether there IS one is the caller's
  // decision (audience, F-5); this component only draws what it is given.
  command: { type: String, default: null },
  // When the reading was taken. Empty means nothing has been observed, and
  // nothing is drawn — "never" would read as a fact about the demo.
  checkedAt: { type: String, default: '' },
  busy: { type: Boolean, default: false },
  retryLabel: { type: String, default: 'Check again' },
})
defineEmits(['retry'])

const copied = defineModel('copied', { default: false })

async function copyCommand() {
  try {
    await navigator.clipboard.writeText(props.command)
    copied.value = true
    setTimeout(() => { copied.value = false }, 1500)
  } catch {
    // A refused clipboard is not worth a message: the command is on screen
    // and can be selected by hand.
  }
}
</script>

<template>
  <section class="demo-state-panel" :class="`tone-${tone}`" role="status" aria-live="polite">
    <p class="demo-state-headline">
      <span class="demo-state-dot" aria-hidden="true" />
      {{ headline }}
    </p>

    <p v-if="detail" class="demo-state-detail">{{ detail }}</p>

    <ul v-if="items.length" class="demo-state-items">
      <li v-for="item in items" :key="item">{{ item }}</li>
    </ul>

    <div v-if="command" class="demo-state-command">
      <code>{{ command }}</code>
      <button type="button" class="demo-state-copy" @click="copyCommand">
        {{ copied ? 'Copied' : 'Copy' }}
      </button>
    </div>

    <div class="demo-state-foot">
      <button type="button" class="demo-state-retry" :disabled="busy" @click="$emit('retry')">
        {{ busy ? 'Checking…' : retryLabel }}
      </button>
      <span v-if="checkedAt" class="demo-state-checked">Checked {{ checkedAt }}</span>
    </div>
  </section>
</template>

<style scoped>
.demo-state-panel {
  max-width: 44rem;
  margin: 2rem auto;
  padding: 1.25rem 1.5rem;
  border: 1px solid var(--p-content-border-color);
  border-radius: 8px;
  background: var(--p-content-background);
  color: var(--p-text-color);
}

.demo-state-headline {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin: 0;
  font-size: 1rem;
  font-weight: 600;
}

.demo-state-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--p-text-muted-color);
  flex: 0 0 auto;
}
.tone-ok .demo-state-dot { background: #27c07f; }
.tone-warn .demo-state-dot { background: #9a7b1e; }

.demo-state-detail {
  margin: 0.5rem 0 0;
  color: var(--p-text-muted-color);
}

.demo-state-items {
  display: flex;
  flex-wrap: wrap;
  gap: 0.375rem;
  margin: 0.75rem 0 0;
  padding: 0;
  list-style: none;
}
.demo-state-items li {
  padding: 0.125rem 0.5rem;
  border: 1px solid var(--p-content-border-color);
  border-radius: 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.8125rem;
}

.demo-state-command {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-top: 0.875rem;
  padding: 0.5rem 0.625rem;
  border-radius: 6px;
  background: var(--p-surface-900, #131416);
  overflow-x: auto;
}
.demo-state-command code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 0.8125rem;
  white-space: nowrap;
}
.demo-state-copy {
  margin-left: auto;
  flex: 0 0 auto;
}

.demo-state-foot {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin-top: 1rem;
}
.demo-state-checked {
  color: var(--p-text-muted-color);
  font-size: 0.8125rem;
}

.demo-state-retry,
.demo-state-copy {
  padding: 0.25rem 0.75rem;
  border: 1px solid var(--p-content-border-color);
  border-radius: 4px;
  background: transparent;
  color: inherit;
  font: inherit;
  font-size: 0.8125rem;
  cursor: pointer;
}
.demo-state-retry:disabled { cursor: default; opacity: 0.6; }
.demo-state-retry:not(:disabled):hover,
.demo-state-copy:hover { border-color: var(--p-primary-color); }
</style>
