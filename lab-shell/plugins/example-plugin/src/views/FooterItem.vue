<script setup>
/* A `shell-footer` contribution: one status item in the shell's footer bar.
   Capacity there is 6; the seventh is refused at index time with a reason
   rather than being dropped at paint (BR-AS07). */
</script>

<template>
  <span class="ex-foot"><span class="k">example-plugin</span> 0.1.1 · :7111</span>
</template>

<style scoped>
.ex-foot {
  font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace;
  font-size: 11px;
  color: var(--p-text-muted-color);
}
.k { color: var(--p-text-disabled-color); }
</style>
