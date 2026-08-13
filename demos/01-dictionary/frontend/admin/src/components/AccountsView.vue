<script setup>
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'

import { useUiStore } from '../stores/ui'
import AccountsPanel from './AccountsPanel.vue'
import TopologyPanel from './TopologyPanel.vue'

const ui = useUiStore()
</script>

<template>
  <Tabs v-model:value="ui.accountsTab">
    <TabList>
      <Tab value="provisioning">Provisioning</Tab>
      <Tab value="topology">Topology</Tab>
    </TabList>
    <TabPanels>
      <TabPanel value="provisioning">
        <AccountsPanel />
      </TabPanel>
      <!-- v-if, not just a hidden TabPanel — TopologyPanel starts a 15s
           refresh() poll onMounted; only mounting it while its tab is
           actually active keeps that poll from running while the user sits
           on Provisioning. -->
      <TabPanel value="topology">
        <TopologyPanel v-if="ui.accountsTab === 'topology'" />
      </TabPanel>
    </TabPanels>
  </Tabs>
</template>

<style scoped>
:deep(.p-tabs) {
  --p-tabs-tablist-border-width: 0 0 1px 0;
}
</style>
