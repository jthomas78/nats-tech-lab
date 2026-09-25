<script setup>
/* The cross-owner case (BR-AS07): this fills `demo-catalog/details-sidebar/v1`,
   a region owned by a federated feature rather than by the shell. The demo
   catalog need not be loaded for this contribution to be placed — placement is
   metadata-only — and the catalog knows nothing about this plugin. */
defineProps({
  context: { type: Object, default: () => ({}) },
})
</script>

<template>
  <article class="ex-card">
    <span class="ex-eyebrow">example-plugin → demo-catalog</span>
    <h2>Contributed sidebar panel</h2>
    <p>
      Rendered beside a demo intro the catalog owns, with the catalog's frozen
      context.
    </p>
    <p class="ex-ctx">
      context.demoId = <code>{{ context.demoId ?? '—' }}</code>
    </p>
  </article>
</template>

<style scoped>
.ex-card {
  border: 1px solid var(--ok);
  border-radius: 6px;
  padding: 14px 16px;
  background: var(--lab-panel-bg);
}
.ex-eyebrow {
  font-size: 10px; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase;
  color: var(--ok);
}
h2 { font-size: 14px; margin: 6px 0 8px; }
p { margin: 0 0 6px; color: var(--p-text-muted-color); }
.ex-ctx { font-size: 11px; color: var(--p-text-disabled-color); }
</style>
