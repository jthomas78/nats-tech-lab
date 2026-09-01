<script setup>
/*
  BR-AS66 — a fresh lab serves only its preloaded plugin.

  On first boot against an empty registry database the shell shows one plugin
  and five disabled rows. That is the rule working, not a broken lab, and
  there is nothing on the screen that says so: `disabled` reads as a fault to
  anyone who has not read the phase notes. This note is that sentence, written
  once and placed wherever the empty state is visible — the home region that
  no plugin has filled, and the inventory that explains why.

  Shell-authored text (BR-AS04): no plugin contributes a word of it.
*/
</script>

<template>
  <p class="first-boot">
    <b>A fresh lab serves only its preloaded plugin.</b>
    <span class="mono">demo-catalog</span> is curated in the lab's preload file, so it
    starts enabled. The <span class="mono">example-plugin*</span> fixtures announce
    themselves from their own publisher sidecars and arrive <b>disabled</b>, waiting for
    an operator to enable them in the Admin UI's Frontend Plugins panel. Disabled here
    means awaiting review, not failed — and an enable, once granted, survives restarts.
  </p>
</template>

<style scoped>
.first-boot {
  margin: 0;
  padding: 10px 12px;
  border: 1px solid var(--lab-panel-border);
  border-left: 2px solid var(--lab-accent);
  border-radius: 4px;
  font-size: 12px;
  line-height: 19px;
  color: var(--p-text-muted-color);
  text-align: left;
  max-width: 720px;
}
.first-boot b { color: var(--p-text-color); font-weight: 600; }
.mono { font-family: ui-monospace, 'SF Mono', Menlo, Consolas, monospace; color: var(--p-text-color); }
</style>
