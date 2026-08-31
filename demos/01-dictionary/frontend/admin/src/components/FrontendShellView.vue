<script setup>
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'

import { useUiStore } from '../stores/ui'
import FrontendPluginsPanel from './FrontendPluginsPanel.vue'
import RegistryAuditPanel from './RegistryAuditPanel.vue'
import RegistryPublishersPanel from './RegistryPublishersPanel.vue'

const ui = useUiStore()
</script>

<template>
  <!-- The curated registry and its write history are one subject in two
       readings — what is served right now, and how it came to be that way —
       so they are tabs of one nav item rather than two siblings in the rail
       (see AccountsView.vue for the same pattern). -->
  <Tabs
    v-model:value="ui.frontendShellTab"
    class="panel-tabs"
    data-testid="frontend-shell-tabs"
  >
    <TabList>
      <Tab value="plugins">
        Plugins
      </Tab>
      <Tab value="publishers">
        Publishers
      </Tab>
      <Tab value="audit">
        Registry Audit
      </Tab>
    </TabList>
    <TabPanels>
      <!-- v-if, not just a hidden TabPanel — each panel polls the registry on
           its own onMounted; only mounting the active tab keeps the other
           poll from running while the user sits elsewhere. -->
      <TabPanel value="plugins">
        <FrontendPluginsPanel v-if="ui.frontendShellTab === 'plugins'" />
      </TabPanel>
      <TabPanel value="publishers">
        <RegistryPublishersPanel v-if="ui.frontendShellTab === 'publishers'" />
      </TabPanel>
      <TabPanel value="audit">
        <RegistryAuditPanel v-if="ui.frontendShellTab === 'audit'" />
      </TabPanel>
    </TabPanels>
  </Tabs>
</template>
