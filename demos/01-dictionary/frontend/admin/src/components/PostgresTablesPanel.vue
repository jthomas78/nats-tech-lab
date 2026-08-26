<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import { onMounted, ref, watch } from 'vue'

import { getPortsTable } from '../api'
import { useDictionaryStore } from '../stores/dictionary'

const store = useDictionaryStore()

const collapsed = ref(false)
const loading = ref(false)
const portRows = ref([])

async function loadPorts() {
  // The store's context is '' until loadContexts() resolves, and this panel
  // mounts before that. `/api/admin/ports/` with an empty {context} segment
  // matches no route on shipping-service, so firing anyway only logged a 404
  // in the browser console on every page load. The watch below re-runs this
  // the moment a real context arrives.
  if (!store.context) {
    portRows.value = []
    return
  }
  loading.value = true
  try {
    const res = await getPortsTable(store.context)
    portRows.value = res?.rows ?? []
  } catch {
    portRows.value = []
  } finally {
    loading.value = false
  }
}

function formatDate(ts) {
  if (!ts) return ''
  return new Date(ts).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })
}

watch(() => store.context, loadPorts)
onMounted(loadPorts)
</script>

<template>
  <div class="lab-panel pg-panel">
    <div class="panel-header" @click="collapsed = !collapsed">
      <div class="panel-header-left">
        <span class="collapse-icon">{{ collapsed ? '▶' : '▼' }}</span>
        <span class="panel-title">Postgres Tables</span>
      </div>
      <Button
        icon="pi pi-refresh"
        text
        rounded
        size="small"
        :loading="loading"
        aria-label="Refresh"
        @click.stop="loadPorts"
      />
    </div>

    <template v-if="!collapsed">
      <div class="table-group">
        <h4 class="group-title">Reference Data</h4>
        <Tabs value="ports">
          <TabList>
            <Tab value="ports">Ports</Tab>
          </TabList>
          <TabPanels>
            <TabPanel value="ports">
              <p class="lab-muted description">
                Plain Postgres reference data, not event-sourced — see "Event Sourcing vs Plain CRUD"
                in obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md. BR-017/BR-018 enforce that ships can only arrive at, and containers
                can only route through, a port registered here.
              </p>
              <DataTable :value="portRows" size="small" paginator :rows="5" class="pg-table">
                <template #empty>
                  <span class="lab-muted">No ports registered for this fleet context yet.</span>
                </template>
                <Column field="name" header="Name" />
                <Column header="Created At">
                  <template #body="{ data }">{{ formatDate(data.createdAt) }}</template>
                </Column>
              </DataTable>
            </TabPanel>
          </TabPanels>
        </Tabs>
      </div>
    </template>
  </div>
</template>

<style scoped>
.pg-panel {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  cursor: pointer;
  user-select: none;
}
.panel-header-left {
  display: flex;
  align-items: center;
  gap: 0.4rem;
}
.collapse-icon {
  font-size: 9px;
  color: var(--p-text-muted-color);
  width: 10px;
}
.panel-title {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--lab-accent);
}
.table-group {
  display: flex;
  flex-direction: column;
  gap: 0.35rem;
}
.group-title {
  margin: 0;
  font-size: 11px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--p-text-muted-color);
}
.description {
  margin: 0;
  font-size: 0.85rem;
}
.pg-panel :deep(.p-datatable-tbody > tr > td) {
  padding-top: 3px;
  padding-bottom: 3px;
}
.pg-panel :deep(.p-tabs) {
  --p-tabs-tablist-border-width: 0 0 1px 0;
}
</style>
