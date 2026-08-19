<script setup>
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'

import { useUiStore } from '../stores/ui'
import AccountsOverviewPanel from './AccountsOverviewPanel.vue'
import AccountsPanel from './AccountsPanel.vue'
import SharingPanel from './SharingPanel.vue'

const ui = useUiStore()
</script>

<template>
  <Tabs
    v-model:value="ui.accountsTab"
    class="panel-tabs"
  >
    <TabList>
      <Tab value="overview">Overview</Tab>
      <Tab value="provisioning">Provisioning</Tab>
      <Tab value="sharing">Sharing</Tab>
    </TabList>
    <TabPanels>
      <!-- v-if, not just a hidden TabPanel — each of these polls on its own
           onMounted; only mounting the active tab's panel keeps the other
           two polls from running while the user sits elsewhere. -->
      <TabPanel value="overview">
        <AccountsOverviewPanel v-if="ui.accountsTab === 'overview'" />
      </TabPanel>
      <TabPanel value="provisioning">
        <AccountsPanel />
      </TabPanel>
      <TabPanel value="sharing">
        <SharingPanel v-if="ui.accountsTab === 'sharing'" />
      </TabPanel>
    </TabPanels>
  </Tabs>
</template>
