
<script setup>
/* The embedded entry — one of demo 04's two `route` contributions (task 17h).

   It renders the SAME body as the standalone app, from the same sources, in
   the same build. What it must not render is `@ui-shell/AppShell`: when the
   demo is embedded, `lab-shell` supplies the topbar, the rail and the theme
   toggle, and a second frame inside the first would nest two chromes
   (BR-AS09).

   Task 17h took the lesson rail OUT of this file. The shell now draws one
   rail for the whole page, with this demo's two lessons in it as two nav
   entries, so a second rail in the route's own left column would be the
   double navbar phase 17 exists to remove. Standalone is untouched: `App.vue`
   on 20401 still owns its rail, because nothing else draws one there.

   Which lesson this is comes from the ROUTE, never from a default of its own
   (amendment A4). The shell hands every plugin route its own local
   contribution id as `routeId` (BR-AS91), so a cold link to
   `/demo-04/lesson-02` opens lesson 02, a refresh keeps it, and Back and
   Forward move between the two. This component may NOT ask a router instead:
   `vue-router` is the shell's own dependency and is not shared across the
   federation boundary.

   One component, two routes. There is no second lesson component and no
   duplicated panel — the route decides which lesson `LessonPanels` shows,
   and `LessonPanels` never learns which entry is rendering it. */
import Tag from 'primevue/tag'

import LessonPanels from '../components/LessonPanels.vue'
import { useDemoState } from '../view/useDemoState.js'

const props = defineProps({
  /* The plugin's own id for the route being shown — `lesson-01` or
     `lesson-02`. Absent only in a test that mounts this component bare; an
     unknown value falls back to the first lesson rather than rendering
     nothing. */
  routeId: { type: String, default: null },
})

const state = useDemoState({ lesson: () => props.routeId })
</script>

<template>
  <section class="lesson-body">
    <div class="embedded-head">
      <span class="crumb">
        <span>{{ state.crumb.lesson }}</span>
        <span class="sep">/</span>
        <b>{{ state.crumb.title }}</b>
      </span>
      <Tag
        data-testid="connection-status"
        :severity="state.connection.severity"
        :value="state.connection.label"
      />
    </div>

    <LessonPanels :state="state" />
  </section>
</template>

<style scoped>
.lesson-body {
  min-width: 0;
}

.embedded-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

.crumb {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  color: var(--p-text-muted-color);
  font-size: 12px;
}

.crumb .sep {
  color: var(--p-text-disabled-color);
}
</style>
