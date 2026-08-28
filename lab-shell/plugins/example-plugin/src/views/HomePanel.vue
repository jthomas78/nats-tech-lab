<script setup>
/* An extension into `shell/home-main/v1` — a region the SHELL owns. The host
   decides where this renders and how many siblings fit; the plugin only fills
   the target (BR-AS07). */
defineProps({
  /* Frozen by the host on the way in. Mutating it affects neither the host nor
     any sibling contribution — asserted in the shell's specs. */
  context: { type: Object, default: () => ({}) },
})
</script>

<template>
  <article class="ex-card">
    <span class="ex-eyebrow">example-plugin</span>
    <h2>Remote panel on the shell's home</h2>
    <p>
      This panel's code arrived from port 7110 after the home screen had already
      painted. The shell reserved the space from metadata alone.
    </p>
    <p
      v-if="context.region"
      class="ex-ctx"
    >
      context.region = <code>{{ context.region }}</code>
    </p>
  </article>
</template>

<style scoped>
.ex-card {
  border: 1px solid var(--lab-accent);
  border-radius: 6px;
  padding: 14px 16px;
  background: var(--lab-panel-bg);
}
.ex-eyebrow {
  font-size: 10px; font-weight: 700; letter-spacing: 0.08em; text-transform: uppercase;
  color: var(--lab-accent);
}
h2 { font-size: 14px; margin: 6px 0 8px; }
p { margin: 0 0 6px; color: var(--p-text-muted-color); }
.ex-ctx { font-size: 11px; color: var(--p-text-disabled-color); }
</style>
