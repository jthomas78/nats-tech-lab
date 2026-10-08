<script setup>
// The Meta-Leader page: two tabs, Overview and Live, under ONE controller.
//
// The controller sits here, above the tabs, so a tab switch keeps every
// observation, history line and pending command, and never reconnects. A tab
// switch changes only the route param, so the shell keeps this instance; it
// starts polling when the page mounts and stops when the reader leaves.
// Stopping only clears the poll timer: navigation never starts, stops,
// freezes or resumes the rig. No AppShell: the lab shell owns the frame.
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'

import LivePanel from '../components/LivePanel.vue'
import OverviewPanel from '../components/OverviewPanel.vue'
import { usePlayground } from '../usePlayground.js'

import { goToTab, isTab, rememberTab, sessionTabOf, TABS, useShellRouter } from './tabs.js'

const props = defineProps({
  routeId: { type: String, default: 'meta-leader' },
  // The route param. Absent on a bare /demo-03/meta-leader.
  tab: { type: String, default: '' },
})

const router = useShellRouter()
const local = ref(isTab(props.tab) ? props.tab : sessionTabOf())
const tab = computed(() => (isTab(props.tab) ? props.tab : local.value))

// The URL is the record: back/forward and direct links arrive here.
watch(
  () => props.tab,
  (t) => {
    if (!isTab(t)) return
    local.value = t
    rememberTab(t)
  },
  { immediate: true },
)

function select(t) {
  if (!isTab(t) || t === tab.value) return
  rememberTab(t)
  local.value = t
  if (router) goToTab(router, t)
}

const { state, actions, start, stop } = usePlayground()

onMounted(() => {
  // A bare /demo-03/meta-leader names no tab: write the session's into the
  // URL, in place, so a reload keeps it.
  if (router && !isTab(props.tab)) goToTab(router, local.value, { replace: true })
  start()
})
onUnmounted(stop)
</script>

<template>
  <section
    class="ml"
    data-testid="meta-leader"
  >
    <Tabs
      :value="tab"
      class="panel-tabs"
      @update:value="select"
    >
      <TabList>
        <Tab
          v-for="t in TABS"
          :key="t.key"
          :value="t.key"
          :data-testid="`meta-leader-tab-${t.key}`"
        >
          {{ t.label }}
        </Tab>
      </TabList>
      <TabPanels>
        <TabPanel value="overview">
          <OverviewPanel v-if="tab === 'overview'" />
        </TabPanel>
        <TabPanel value="live">
          <LivePanel
            v-if="tab === 'live'"
            :state="state"
            :actions="actions"
          />
        </TabPanel>
      </TabPanels>
    </Tabs>
  </section>
</template>
