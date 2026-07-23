<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tab from 'primevue/tab'
import TabList from 'primevue/tablist'
import TabPanel from 'primevue/tabpanel'
import TabPanels from 'primevue/tabpanels'
import Tabs from 'primevue/tabs'
import Tag from 'primevue/tag'
import Textarea from 'primevue/textarea'
import { useToast } from 'primevue/usetoast'
import { computed, onMounted, ref } from 'vue'

import { useVersioningStore } from '../stores/versioning'

// Phase 12.6 — the versioning admin UI: context hierarchy viewer, corpus
// version lifecycle (draft/publish/rollback), and a diff viewer. A
// standalone tabbed panel, not woven into the type/item master-detail —
// this operates one level up, on whole corpus versions rather than
// individual dictionary items.
const store = useVersioningStore()
const toast = useToast()

const activeTab = ref('contexts')

const newContextDialog = ref(false)
const newContext = ref({ context: '', parent: '', name: '', description: '' })

const draftNotesDialog = ref(false)
const draftNotes = ref('')

const rollbackDialog = ref(false)
const rollbackTarget = ref(null)
const rollbackNotes = ref('')

onMounted(async () => {
  await store.refreshContexts()
  if (!store.selectedContext && store.contexts.length > 0) {
    await store.selectContext(store.contexts[0].context)
  }
})

function severityFor(status) {
  if (status === 'published') return 'success'
  if (status === 'draft') return 'info'
  return 'secondary' // rolled-back
}

async function selectContext(context) {
  await store.selectContext(context)
}

async function submitNewContext() {
  try {
    await store.registerNewContext({ ...newContext.value })
    toast.add({ severity: 'success', summary: 'Context registered', life: 3000 })
    newContextDialog.value = false
    newContext.value = { context: '', parent: '', name: '', description: '' }
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not register context', detail: err.message, life: 4000 })
  }
}

async function submitNewDraft() {
  try {
    await store.createNewDraft(draftNotes.value)
    toast.add({ severity: 'success', summary: 'Draft created', life: 3000 })
    draftNotesDialog.value = false
    draftNotes.value = ''
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not create draft', detail: err.message, life: 4000 })
  }
}

async function publish() {
  try {
    await store.publish()
    toast.add({ severity: 'success', summary: 'Published', life: 3000 })
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not publish', detail: err.message, life: 4000 })
  }
}

function openRollback(version) {
  rollbackTarget.value = version
  rollbackNotes.value = ''
  rollbackDialog.value = true
}

async function submitRollback() {
  try {
    await store.rollbackTo(rollbackTarget.value.version, rollbackNotes.value)
    toast.add({ severity: 'success', summary: `Rolled back to v${rollbackTarget.value.version}`, life: 3000 })
    rollbackDialog.value = false
  } catch (err) {
    toast.add({ severity: 'error', summary: 'Could not roll back', detail: err.message, life: 4000 })
  }
}

const versionOptions = computed(() =>
  store.versions
    .filter((v) => v.status !== 'draft')
    .map((v) => ({ label: `v${v.version} (${v.status})`, value: v.version }))
    .sort((a, b) => b.value - a.value),
)

async function runDiff() {
  await store.runDiff()
}

function diffSeverity(change) {
  if (change === 'added') return 'success'
  if (change === 'removed') return 'danger'
  return 'warn' // changed
}

// Flattens the context tree into an indented option list for the parent
// selector — simplest widget that still conveys hierarchy depth.
function flattenTree(nodes, depth = 0, out = []) {
  for (const node of nodes) {
    out.push({ context: node.context, label: `${'—'.repeat(depth)} ${node.name || node.context}`.trim() })
    flattenTree(node.children, depth + 1, out)
  }
  return out
}
const parentOptions = computed(() => flattenTree(store.contextTree))
</script>

<template>
  <div class="lab-panel versioning-panel">
    <div class="panel-header">
      <h3>Refdata Versioning</h3>
      <span class="lab-muted subtitle">context hierarchy · corpus draft/publish/rollback · diff</span>
    </div>

    <p
      v-if="store.error"
      class="error-banner"
    >
      {{ store.error }}
    </p>

    <Tabs v-model:value="activeTab">
      <TabList>
        <Tab value="contexts">
          Contexts
        </Tab>
        <Tab value="versions">
          Corpus Versions
        </Tab>
        <Tab value="diff">
          Diff
        </Tab>
      </TabList>
      <TabPanels>
        <!-- ── Contexts ─────────────────────────────────────────────────── -->
        <TabPanel value="contexts">
          <div class="tab-toolbar">
            <Button
              label="New context"
              icon="pi pi-plus"
              size="small"
              @click="newContextDialog = true"
            />
          </div>
          <ul class="context-tree">
            <template
              v-for="root in store.contextTree"
              :key="root.context"
            >
              <li
                :class="{ active: root.context === store.selectedContext }"
                @click="selectContext(root.context)"
              >
                <i class="pi pi-sitemap" /> {{ root.name || root.context }}
                <span class="lab-muted context-key">{{ root.context }}</span>
              </li>
              <ul
                v-if="root.children.length"
                class="context-tree nested"
              >
                <li
                  v-for="child in root.children"
                  :key="child.context"
                  :class="{ active: child.context === store.selectedContext }"
                  @click="selectContext(child.context)"
                >
                  <i class="pi pi-angle-right" /> {{ child.name || child.context }}
                  <span class="lab-muted context-key">{{ child.context }}</span>
                </li>
              </ul>
            </template>
            <p
              v-if="store.contexts.length === 0"
              class="lab-muted"
            >
              No contexts registered yet.
            </p>
          </ul>

          <div
            v-if="store.contextDetail"
            class="context-detail"
          >
            <h4>{{ store.contextDetail.context.name || store.contextDetail.context.context }}</h4>
            <p class="lab-muted">
              Ancestors: {{ (store.contextDetail.ancestors || []).map((c) => c.context).join(' → ') || '(root)' }}
            </p>
            <p class="lab-muted">
              Descendants: {{ (store.contextDetail.descendants || []).slice(1).map((c) => c.context).join(', ') || '(none)' }}
            </p>
          </div>
        </TabPanel>

        <!-- ── Corpus Versions ──────────────────────────────────────────── -->
        <TabPanel value="versions">
          <div
            v-if="!store.selectedContext"
            class="lab-muted"
          >
            Select a context to manage its corpus versions.
          </div>
          <template v-else>
            <div class="tab-toolbar">
              <Button
                label="Create draft"
                icon="pi pi-plus"
                size="small"
                :disabled="store.hasDraft"
                @click="draftNotesDialog = true"
              />
              <Button
                label="Publish draft"
                icon="pi pi-cloud-upload"
                size="small"
                severity="success"
                :disabled="!store.hasDraft"
                @click="publish"
              />
            </div>

            <div
              v-if="store.draft"
              class="draft-summary"
            >
              <Tag
                severity="info"
                :value="`Draft v${store.draft.version.version}`"
              />
              <span class="lab-muted">{{ store.draft.items?.length ?? 0 }} items · {{ store.draft.localizations?.length ?? 0 }} localizations</span>
            </div>

            <DataTable
              :value="store.versions"
              size="small"
              data-key="version"
            >
              <Column
                field="version"
                header="Version"
              >
                <template #body="{ data }">
                  v{{ data.version }}
                </template>
              </Column>
              <Column
                field="status"
                header="Status"
              >
                <template #body="{ data }">
                  <Tag
                    :severity="severityFor(data.status)"
                    :value="data.status"
                  />
                </template>
              </Column>
              <Column
                field="parentVersion"
                header="Parent"
              >
                <template #body="{ data }">
                  {{ data.parentVersion != null ? `v${data.parentVersion}` : '—' }}
                </template>
              </Column>
              <Column
                field="baseContextVersion"
                header="Base (parent context)"
              >
                <template #body="{ data }">
                  {{ data.baseContextVersion != null ? `v${data.baseContextVersion}` : '—' }}
                </template>
              </Column>
              <Column
                field="notes"
                header="Notes"
              />
              <Column header="Actions">
                <template #body="{ data }">
                  <Button
                    v-if="data.status === 'published'"
                    label="Rollback to this"
                    size="small"
                    text
                    @click="openRollback(data)"
                  />
                </template>
              </Column>
            </DataTable>
            <p
              v-if="store.versions.length === 0"
              class="lab-muted"
            >
              No corpus versions yet — create a draft to get started.
            </p>
          </template>
        </TabPanel>

        <!-- ── Diff ─────────────────────────────────────────────────────── -->
        <TabPanel value="diff">
          <div
            v-if="!store.selectedContext"
            class="lab-muted"
          >
            Select a context to diff its versions.
          </div>
          <template v-else>
            <div class="diff-picker">
              <label class="lab-muted">From</label>
              <Select
                :model-value="store.diffFrom"
                :options="versionOptions"
                option-label="label"
                option-value="value"
                size="small"
                placeholder="version"
                @update:model-value="(v) => store.setDiffRange(v, store.diffTo)"
              />
              <label class="lab-muted">To</label>
              <Select
                :model-value="store.diffTo"
                :options="versionOptions"
                option-label="label"
                option-value="value"
                size="small"
                placeholder="version"
                @update:model-value="(v) => store.setDiffRange(store.diffFrom, v)"
              />
              <Button
                label="Diff"
                size="small"
                :disabled="store.diffFrom == null || store.diffTo == null"
                @click="runDiff"
              />
            </div>

            <DataTable
              :value="store.diffEntries"
              size="small"
            >
              <Column
                field="typeKey"
                header="Type"
              />
              <Column
                field="code"
                header="Code"
              />
              <Column
                field="change"
                header="Change"
              >
                <template #body="{ data }">
                  <Tag
                    :severity="diffSeverity(data.change)"
                    :value="data.change"
                  />
                </template>
              </Column>
            </DataTable>
            <p
              v-if="store.diffFrom != null && store.diffTo != null && store.diffEntries.length === 0"
              class="lab-muted"
            >
              No differences between v{{ store.diffFrom }} and v{{ store.diffTo }}.
            </p>
          </template>
        </TabPanel>
      </TabPanels>
    </Tabs>

    <!-- ── Dialogs ────────────────────────────────────────────────────────── -->
    <Dialog
      v-model:visible="newContextDialog"
      header="Register context"
      modal
      style="width: 28rem"
    >
      <div class="dialog-form">
        <label class="lab-muted">Context key</label>
        <InputText
          v-model="newContext.context"
          size="small"
          placeholder="e.g. emea-globex"
        />
        <label class="lab-muted">Parent (optional)</label>
        <Select
          v-model="newContext.parent"
          :options="parentOptions"
          option-label="label"
          option-value="context"
          size="small"
          show-clear
          placeholder="(root — no parent)"
        />
        <label class="lab-muted">Name</label>
        <InputText
          v-model="newContext.name"
          size="small"
        />
        <label class="lab-muted">Description</label>
        <Textarea
          v-model="newContext.description"
          size="small"
          rows="2"
          auto-resize
        />
      </div>
      <template #footer>
        <Button
          label="Cancel"
          text
          @click="newContextDialog = false"
        />
        <Button
          label="Register"
          :disabled="!newContext.context || !newContext.name"
          @click="submitNewContext"
        />
      </template>
    </Dialog>

    <Dialog
      v-model:visible="draftNotesDialog"
      header="Create draft"
      modal
      style="width: 24rem"
    >
      <div class="dialog-form">
        <label class="lab-muted">Notes (optional)</label>
        <Textarea
          v-model="draftNotes"
          size="small"
          rows="2"
          auto-resize
        />
      </div>
      <template #footer>
        <Button
          label="Cancel"
          text
          @click="draftNotesDialog = false"
        />
        <Button
          label="Create draft"
          @click="submitNewDraft"
        />
      </template>
    </Dialog>

    <Dialog
      v-model:visible="rollbackDialog"
      header="Rollback"
      modal
      style="width: 24rem"
    >
      <div
        v-if="rollbackTarget"
        class="dialog-form"
      >
        <p>
          Roll back to <strong>v{{ rollbackTarget.version }}</strong>? This publishes a new,
          forward-numbered version with v{{ rollbackTarget.version }}'s content — it does not
          rewrite history.
        </p>
        <label class="lab-muted">Notes (optional)</label>
        <Textarea
          v-model="rollbackNotes"
          size="small"
          rows="2"
          auto-resize
        />
      </div>
      <template #footer>
        <Button
          label="Cancel"
          text
          @click="rollbackDialog = false"
        />
        <Button
          label="Roll back"
          severity="warn"
          @click="submitRollback"
        />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.versioning-panel {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.panel-header h3 {
  margin: 0 0 2px;
}
.subtitle {
  font-size: 11px;
}
.error-banner {
  color: var(--p-red-500, #e57373);
  font-size: 12px;
  margin: 0;
}
.tab-toolbar {
  display: flex;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
}
.context-tree {
  list-style: none;
  margin: 0 0 0.75rem;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.context-tree.nested {
  margin-left: 1.25rem;
}
.context-tree li {
  display: flex;
  align-items: center;
  gap: 0.4rem;
  padding: 0.35rem 0.5rem;
  border-radius: 3px;
  cursor: pointer;
  font-size: 12px;
}
.context-tree li:hover {
  background: var(--lab-disabled-bg);
}
.context-tree li.active {
  background: var(--p-highlight-background);
  color: var(--p-highlight-color);
}
.context-key {
  margin-left: auto;
  font-family: monospace;
  font-size: 10px;
}
.context-detail {
  border-top: 1px solid var(--lab-disabled-bg);
  padding-top: 0.5rem;
}
.context-detail h4 {
  margin: 0 0 0.25rem;
}
.context-detail p {
  margin: 0.15rem 0;
  font-size: 12px;
}
.draft-summary {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.5rem;
}
.diff-picker {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  margin-bottom: 0.75rem;
}
.dialog-form {
  display: flex;
  flex-direction: column;
  gap: 0.4rem;
}
</style>
