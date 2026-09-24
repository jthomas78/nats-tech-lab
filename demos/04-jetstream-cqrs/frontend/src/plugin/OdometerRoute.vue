
<script setup>
/* The embedded entry — demo 04's single `route` contribution (task 16d).

   It renders the SAME body as the standalone app, from the same sources, in
   the same build. What it must not render is `@ui-shell/AppShell`: when the
   demo is embedded, `lab-shell` supplies the topbar, the rail and the theme
   toggle, and a second frame inside the first would nest two chromes
   (BR-AS09).

   The lesson rail and the connection tag still have to go somewhere. The shell
   declares no extension point for its own sidebar, and splitting the two
   lessons into two routes was rejected — it would change how the demo is
   navigated. So the same `NavList`, with the same two entries and the same
   behaviour, sits in this route's own left column. That is a move, not a
   redesign: no panel became a route, and no menu, breadcrumb or tab changed. */
import Tag from 'primevue/tag'

import NavList from '@ui-shell/NavList.vue'

import LessonPanels from '../components/LessonPanels.vue'
import { useDemoState } from '../view/useDemoState.js'

const state = useDemoState()
</script>

<template>
  <div class="odometer-route">
    <aside class="lesson-rail">
      <NavList
        v-model="state.view"
        :sections="state.sections"
        aria-label="JetStream"
      />
    </aside>

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
  </div>
</template>

<style scoped>
.odometer-route {
  display: flex;
  align-items: flex-start;
  gap: 20px;
}

.lesson-rail {
  flex: 0 0 200px;
  padding-right: 16px;
  border-right: 1px solid var(--lab-panel-border);
}

.lesson-body {
  flex: 1 1 auto;
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

/* The rail stacks above the body when the shell is narrow, rather than
   squeezing both columns until neither reads. */
@media (max-width: 900px) {
  .odometer-route {
    display: block;
  }

  .lesson-rail {
    padding: 0 0 12px;
    border-right: none;
    border-bottom: 1px solid var(--lab-panel-border);
    margin-bottom: 16px;
  }
}
</style>
