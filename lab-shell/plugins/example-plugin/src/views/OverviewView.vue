<script setup>
/* The plugin's route contribution. A plain Vue component: the shell mounted
   it, so it gets the shell's Vue, router and theme without importing any of
   them. */
import { ref } from 'vue'

const clicks = ref(0)
</script>

<template>
  <section class="ex-panel">
    <h1>Example Plugin</h1>
    <p class="ex-muted">
      Served from <code>http://localhost:7110</code> and built by its own Vite
      toolchain. The shell has never imported this file.
    </p>

    <dl class="ex-facts">
      <div><dt>Contribution</dt><dd>route <code>example-plugin/overview</code></dd></div>
      <div><dt>Remote</dt><dd>federated · <code>example_plugin/./plugin</code></dd></div>
      <div><dt>Loaded</dt><dd>on first navigation to this route</dd></div>
    </dl>

    <button
      type="button"
      class="ex-button"
      @click="clicks += 1"
    >
      Local state still works — clicked {{ clicks }}×
    </button>
  </section>
</template>

<style scoped>
.ex-panel {
  border: 1px solid var(--lab-panel-border);
  border-radius: 6px;
  padding: 18px 20px;
  background: var(--lab-panel-bg);
}
h1 { font-size: 18px; margin: 0 0 6px; }
.ex-muted { color: var(--p-text-muted-color); margin: 0 0 16px; }
.ex-facts { display: grid; gap: 6px; margin: 0 0 18px; }
.ex-facts > div { display: flex; gap: 10px; }
.ex-facts dt { width: 120px; color: var(--p-text-disabled-color); }
.ex-facts dd { margin: 0; }
.ex-button {
  font: inherit;
  padding: 6px 12px;
  border-radius: 5px;
  border: 1px solid var(--lab-panel-border);
  background: none;
  color: var(--p-text-color);
  cursor: pointer;
}
.ex-button:hover { border-color: var(--lab-accent); color: var(--lab-accent); }
</style>
