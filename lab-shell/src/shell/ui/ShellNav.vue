<script setup>
/*
  The shell's rail (BR-AS89).

  It was two hand-rolled `nav.nav-group` blocks in `App.vue`: one with the
  shell's two fixed links, one with a FLAT `v-for` over every plugin's
  navigation. Phase 17 replaced that with one merged tree, and this renders it
  through `shared/ui-shell/NavList.vue` — the same component demo 01's two
  frontends already use, so the rail the shell draws and the rail a demo draws
  are one definition and cannot drift (D17-6).

  It builds nothing itself. `shellNavSections` turns the tree into sections,
  `navigationTree` merged them, `navigationClashes` found the clashes — this
  file supplies the mark slot and nothing else.

  The rail stays host-owned (BR-AS07). A plugin contributes a nav ENTRY; it
  cannot render into this region, and this phase added no way for it to.
*/
import NavList from '@ui-shell/NavList.vue'
import { computed, inject } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { summarizeAttention } from '../registry/statusRollup.js'
import { SHELL } from '../shellKey.js'
import { shellNavSections } from './shellNavSections.js'

const shell = inject(SHELL)
const route = useRoute()
const router = useRouter()

/* The chrome's one aggregate signal, on the Plugins link. A plugin that
   failed is visible from every screen without opening the inventory — status
   only, never a cause string, because a cause can quote a remote URL
   (BR-AS04). */
const attention = computed(() => summarizeAttention(shell.inventory))

const sections = computed(() => shellNavSections(shell.contributions.navigationTree, {
  clashes: shell.contributions.navigationClashes,
  statusOf: (pluginId) => shell.statuses.get(pluginId)?.status ?? null,
  healthOf: (pluginId) => shell.health?.signals?.[pluginId] ?? null,
  inventoryCount: shell.inventory.length,
  attention: attention.value,
  /* The rail is a pure function and `NavList` may not import a router, so
     the one place that HAS the router resolves for both. */
  currentPath: route.path,
  pathOf: (to) => router.resolve(to).path,
}))
</script>

<template>
  <NavList
    class="shell-nav"
    :sections="sections"
    aria-label="Shell navigation"
  >
    <template #marker="{ item }">
      <template v-if="item.mark">
        <span
          class="nav-dot"
          :class="item.mark.tone"
          :title="item.mark.title"
        />
        <!-- The same thing the dot says, in words. A clash can lose the dot to
             a louder signal (amendment A2), and colour is not a channel a
             screen reader has, so the description is the one that must always
             be here. -->
        <span class="sr-only">{{ item.mark.description }}</span>
      </template>
    </template>
  </NavList>
</template>

<style scoped>
/* `NavList`'s root is one <nav> holding every band, where the rail used to
   hold one <nav> per band and got its spacing from `.sidebar`'s own gap. */
.shell-nav {
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.nav-dot {
  margin-left: auto;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}
.nav-dot.err { background: var(--err); }
.nav-dot.warn { background: var(--warn); }
/* Read out, never drawn. Not `display: none`, which takes it off the
   accessibility tree as well. */
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
  border: 0;
}
</style>
