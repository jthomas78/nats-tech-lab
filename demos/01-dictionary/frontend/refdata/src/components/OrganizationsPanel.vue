<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputText from 'primevue/inputtext'
import Menu from 'primevue/menu'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import Textarea from 'primevue/textarea'
import { useToast } from 'primevue/usetoast'
import { computed, reactive, ref, watch } from 'vue'

import {
  activateOrganization,
  addComplianceDocument,
  addFleetAsset,
  approveComplianceDocument,
  getOrganizationAudit,
  listComplianceDocuments,
  listFleetAssets,
  listOrganizations,
  reactivateOrganization,
  registerOrganization,
  rejectComplianceDocument,
  resubmitComplianceDocument,
  suspendOrganization,
} from '../api'
import { useTenantStore } from '../stores/tenant'

// Phase 26 — Shipper/Transporter registration, ported from Linebooker
// (BusinessEntity/TransporterProfileEntity/TransporterDocumentEntity/
// FleetAssetEntity). Migrated from frontend/admin in Phase 36.2 — see that
// phase's design decisions for why this reads tenantStore.context (this
// app's own tenant-scoped selector) rather than admin's dictionaryStore.context.
//
// Originally mounted twice — once per role — since both roles are one
// aggregate with a type discriminator server-side (BR-TP01), which made the
// split look purely presentational.
//
// **Phase 38d-i mounts this for Shippers only**; Transporters moved to
// TransporterPanel.vue. What changed is not the aggregate but the surface
// around it: a Transporter now also has an event-sourced TransporterProfile
// (ADR-046) with vetting state, a Temporal saga behind it, and a derived
// goods-in-transit badge, none of which a Shipper has any equivalent of.
// Keeping one component would have meant most of it was reachable for only
// one of its two roles.
//
// The `partnerType` prop and the type-conditional branches below are kept
// rather than hard-coded to SHIPPER — they are correct as written, and
// collapsing them would be an unrelated edit to a file this phase only needed
// to stop sharing.

const props = defineProps({
  // 'SHIPPER' | 'TRANSPORTER' — the single role this instance manages.
  partnerType: { type: String, required: true },
})

const tenantStore = useTenantStore()
const toast = useToast()

const partners = ref([])
const loading = ref(false)
const error = ref('')

const isTransporter = computed(() => props.partnerType === 'TRANSPORTER')
const roleLabel = computed(() => (isTransporter.value ? 'Transporter' : 'Shipper'))

// Confirmed 2026-08-13 (BUSINESS_RULES-ORGANIZATIONS.md BR-TP07): shared
// document types apply to either role; GOODS_IN_TRANSIT is Transporter-only.
const SHARED_DOCUMENT_TYPES = ['CIPC', 'DIRECTOR_ID', 'BANK_CONFIRMATION_LETTER', 'TERMS_AND_CONDITIONS']
const TRANSPORTER_ONLY_DOCUMENT_TYPES = ['GOODS_IN_TRANSIT']

function documentTypesFor(partnerType) {
  return partnerType === 'TRANSPORTER' ? [...SHARED_DOCUMENT_TYPES, ...TRANSPORTER_ONLY_DOCUMENT_TYPES] : SHARED_DOCUMENT_TYPES
}

async function load() {
  if (!tenantStore.context) return
  loading.value = true
  error.value = ''
  try {
    const res = await listOrganizations(tenantStore.context)
    // Filtered here rather than server-side: GET /api/organizations/
    // {context} takes no type filter, and a context's partner list is small
    // enough in this POC that one fetch per role is cheaper than adding a
    // query param and its handler test. Revisit if the list ever paginates
    // server-side.
    partners.value = (res.organizations ?? []).filter((tp) => tp.type === props.partnerType)
  } catch (e) {
    error.value = e.message
  } finally {
    loading.value = false
  }
}

watch(() => tenantStore.context, load, { immediate: true })

// ── Register ──────────────────────────────────────────────────────────────

const registerOpen = ref(false)
const registering = ref(false)
const registerError = ref('')
// No `type` field — the panel's own role supplies it, so registering from the
// Shippers view can't produce a Transporter that then vanishes from the list.
const registerForm = reactive({ name: '' })

function openRegister() {
  registerForm.name = ''
  registerError.value = ''
  registerOpen.value = true
}

async function submitRegister() {
  if (!registerForm.name) return
  registering.value = true
  registerError.value = ''
  try {
    const tp = await registerOrganization(tenantStore.context, { name: registerForm.name, type: props.partnerType })
    registerOpen.value = false
    await load()
    toast.add({ severity: 'success', summary: 'Organization registered', detail: tp.name, life: 3000 })
  } catch (e) {
    registerError.value = e.message
  } finally {
    registering.value = false
  }
}

// ── Lifecycle: Activate / Suspend / Reactivate (BR-TP03-BR-TP05) ──────────

async function activate(tp) {
  try {
    await activateOrganization(tenantStore.context, tp.id)
    await load()
    toast.add({ severity: 'success', summary: 'Activated', detail: tp.name, life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to activate', detail: e.message, life: 5000 })
  }
}

const suspendOpen = ref(false)
const suspendPartner = ref(null)
const suspendReason = ref('')
const suspendSaving = ref(false)
const suspendError = ref('')

function openSuspend(tp) {
  suspendPartner.value = tp
  suspendReason.value = ''
  suspendError.value = ''
  suspendOpen.value = true
}

async function submitSuspend() {
  if (!suspendReason.value) return
  suspendSaving.value = true
  suspendError.value = ''
  try {
    await suspendOrganization(tenantStore.context, suspendPartner.value.id, suspendReason.value)
    suspendOpen.value = false
    await load()
    toast.add({ severity: 'success', summary: 'Suspended', detail: suspendPartner.value.name, life: 3000 })
  } catch (e) {
    suspendError.value = e.message
  } finally {
    suspendSaving.value = false
  }
}

async function reactivate(tp) {
  try {
    await reactivateOrganization(tenantStore.context, tp.id)
    await load()
    toast.add({ severity: 'success', summary: 'Reactivated', detail: tp.name, life: 3000 })
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to reactivate', detail: e.message, life: 5000 })
  }
}

// ── Row expansion: documents + fleet assets + audit trail ─────────────────

const expandedRows = ref([])
const documentsByPartner = ref({})
const fleetAssetsByPartner = ref({})
const auditByPartner = ref({})
const expansionLoading = ref({})

async function onRowExpand(event) {
  const tp = event.data
  expansionLoading.value = { ...expansionLoading.value, [tp.id]: true }
  try {
    const [docs, audit] = await Promise.all([
      listComplianceDocuments(tenantStore.context, tp.id),
      getOrganizationAudit(tenantStore.context, tp.id),
    ])
    documentsByPartner.value = { ...documentsByPartner.value, [tp.id]: docs.documents ?? [] }
    auditByPartner.value = { ...auditByPartner.value, [tp.id]: audit.events ?? [] }
    if (tp.type === 'TRANSPORTER') {
      const fleet = await listFleetAssets(tenantStore.context, tp.id)
      fleetAssetsByPartner.value = { ...fleetAssetsByPartner.value, [tp.id]: fleet.fleetAssets ?? [] }
    }
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to load details', detail: e.message, life: 5000 })
  } finally {
    expansionLoading.value = { ...expansionLoading.value, [tp.id]: false }
  }
}

async function refreshDocuments(tp) {
  const docs = await listComplianceDocuments(tenantStore.context, tp.id)
  documentsByPartner.value = { ...documentsByPartner.value, [tp.id]: docs.documents ?? [] }
}

async function refreshFleetAssets(tp) {
  const fleet = await listFleetAssets(tenantStore.context, tp.id)
  fleetAssetsByPartner.value = { ...fleetAssetsByPartner.value, [tp.id]: fleet.fleetAssets ?? [] }
}

// ── Compliance documents (BR-TP07-BR-TP11) ────────────────────────────────

const addDocOpen = ref(false)
const addDocPartner = ref(null)
const addDocSaving = ref(false)
const addDocError = ref('')
const addDocForm = reactive({ type: '', reference: '' })

function openAddDocument(tp) {
  addDocPartner.value = tp
  addDocForm.type = documentTypesFor(tp.type)[0]
  addDocForm.reference = ''
  addDocError.value = ''
  addDocOpen.value = true
}

async function submitAddDocument() {
  if (!addDocForm.type || !addDocForm.reference) return
  addDocSaving.value = true
  addDocError.value = ''
  try {
    await addComplianceDocument(tenantStore.context, addDocPartner.value.id, { type: addDocForm.type, reference: addDocForm.reference })
    addDocOpen.value = false
    await refreshDocuments(addDocPartner.value)
    toast.add({ severity: 'success', summary: 'Document registered', detail: addDocForm.type, life: 3000 })
  } catch (e) {
    addDocError.value = e.message
  } finally {
    addDocSaving.value = false
  }
}

async function approveDoc(tp, doc) {
  try {
    await approveComplianceDocument(tenantStore.context, tp.id, doc.id)
    await refreshDocuments(tp)
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to approve', detail: e.message, life: 5000 })
  }
}

async function rejectDoc(tp, doc) {
  try {
    await rejectComplianceDocument(tenantStore.context, tp.id, doc.id)
    await refreshDocuments(tp)
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to reject', detail: e.message, life: 5000 })
  }
}

async function resubmitDoc(tp, doc) {
  try {
    await resubmitComplianceDocument(tenantStore.context, tp.id, doc.id)
    await refreshDocuments(tp)
  } catch (e) {
    toast.add({ severity: 'error', summary: 'Failed to resubmit', detail: e.message, life: 5000 })
  }
}

// ── Fleet assets (BR-TP12-BR-TP14, Transporter only) ──────────────────────

const addFleetOpen = ref(false)
const addFleetPartner = ref(null)
const addFleetSaving = ref(false)
const addFleetError = ref('')
const addFleetForm = reactive({ registrationNo: '', vin: '', make: '', model: '', vehicleTypeCode: '' })

function openAddFleetAsset(tp) {
  addFleetPartner.value = tp
  addFleetForm.registrationNo = ''
  addFleetForm.vin = ''
  addFleetForm.make = ''
  addFleetForm.model = ''
  addFleetForm.vehicleTypeCode = ''
  addFleetError.value = ''
  addFleetOpen.value = true
}

async function submitAddFleetAsset() {
  if (!addFleetForm.registrationNo || !addFleetForm.vehicleTypeCode) return
  addFleetSaving.value = true
  addFleetError.value = ''
  try {
    await addFleetAsset(tenantStore.context, addFleetPartner.value.id, tenantStore.tenant, { ...addFleetForm })
    addFleetOpen.value = false
    await refreshFleetAssets(addFleetPartner.value)
    toast.add({ severity: 'success', summary: 'Fleet asset added', detail: addFleetForm.registrationNo, life: 3000 })
  } catch (e) {
    addFleetError.value = e.message
  } finally {
    addFleetSaving.value = false
  }
}

// ── Formatting + row menu ──────────────────────────────────────────────────

function statusSeverity(status) {
  if (status === 'ACTIVE') return 'success'
  if (status === 'SUSPENDED') return 'danger'
  return 'secondary' // REGISTERED
}

function docStatusSeverity(status) {
  if (status === 'APPROVED') return 'success'
  if (status === 'REJECTED') return 'danger'
  return 'secondary' // PENDING
}

function formatDate(ts) {
  if (!ts) return ''
  return new Date(ts).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' })
}

const rowMenu = ref()
const menuPartner = ref(null)

function openRowMenu(event, tp) {
  menuPartner.value = tp
  rowMenu.value.toggle(event)
}

const rowMenuItems = computed(() => {
  const tp = menuPartner.value
  if (!tp) return []
  const items = []
  if (tp.status === 'REGISTERED') items.push({ label: 'Activate', icon: 'pi pi-play', command: () => activate(tp) })
  if (tp.status === 'ACTIVE') items.push({ label: 'Suspend', icon: 'pi pi-ban', command: () => openSuspend(tp) })
  if (tp.status === 'SUSPENDED') items.push({ label: 'Reactivate', icon: 'pi pi-play', command: () => reactivate(tp) })
  items.push({ label: 'Add Document', icon: 'pi pi-file', command: () => openAddDocument(tp) })
  if (tp.type === 'TRANSPORTER') items.push({ label: 'Add Fleet Asset', icon: 'pi pi-truck', command: () => openAddFleetAsset(tp) })
  return items
})
</script>

<template>
  <div class="lab-panel organizations-panel">
    <div class="panel-header">
      <span class="panel-title">{{ roleLabel }}s</span>
      <div class="header-actions">
        <Button
          icon="pi pi-refresh"
          text
          rounded
          size="small"
          :loading="loading"
          aria-label="Refresh"
          @click="load"
        />
        <Button
          :label="`Register ${roleLabel}`"
          icon="pi pi-plus"
          size="small"
          :disabled="!tenantStore.context"
          @click="openRegister"
        />
      </div>
    </div>

    <p class="lab-muted description">
      {{ roleLabel }} registration (Phase 26), ported from Linebooker's <code>BusinessEntity</code>/
      <code>TransporterProfileEntity</code>. Context-scoped to the current fleet context, like Ports.
    </p>

    <p
      v-if="!tenantStore.context"
      class="lab-muted"
    >
      Select a tenant and fleet context above to manage {{ roleLabel.toLowerCase() }}s.
    </p>
    <p
      v-if="error"
      class="error-text"
    >
      {{ error }}
    </p>

    <DataTable
      v-model:expanded-rows="expandedRows"
      :value="partners"
      size="small"
      paginator
      :rows="10"
      class="partners-table"
      @row-expand="onRowExpand"
    >
      <template #empty>
        <span class="lab-muted">No {{ roleLabel.toLowerCase() }}s registered in this context yet.</span>
      </template>
      <Column
        expander
        style="width: 2.5rem"
      />
      <template #expansion="{ data: tp }">
        <div class="expansion">
          <div
            v-if="expansionLoading[tp.id]"
            class="lab-muted"
          >
            Loading…
          </div>
          <template v-else>
            <!-- Compliance documents (BR-TP07-BR-TP11) -->
            <div class="expansion-section">
              <div class="expansion-header">
                <span class="expansion-title">Compliance Documents</span>
              </div>
              <DataTable
                :value="documentsByPartner[tp.id] ?? []"
                size="small"
                class="sub-table"
              >
                <template #empty>
                  <span class="lab-muted">No documents registered yet.</span>
                </template>
                <Column
                  header="Type"
                  field="type"
                />
                <Column header="Status">
                  <template #body="{ data: doc }">
                    <Tag
                      :severity="docStatusSeverity(doc.status)"
                      :value="doc.status"
                    />
                  </template>
                </Column>
                <Column
                  header="Reference"
                  field="reference"
                  class="ref-col"
                />
                <Column
                  header=""
                  style="width: 14rem"
                >
                  <template #body="{ data: doc }">
                    <div class="doc-actions">
                      <Button
                        v-if="doc.status === 'PENDING'"
                        label="Approve"
                        size="small"
                        text
                        @click="approveDoc(tp, doc)"
                      />
                      <Button
                        v-if="doc.status === 'PENDING'"
                        label="Reject"
                        size="small"
                        text
                        severity="danger"
                        @click="rejectDoc(tp, doc)"
                      />
                      <Button
                        v-if="doc.status === 'REJECTED'"
                        label="Resubmit"
                        size="small"
                        text
                        @click="resubmitDoc(tp, doc)"
                      />
                    </div>
                  </template>
                </Column>
              </DataTable>
            </div>

            <!-- Fleet assets (BR-TP12-BR-TP14, Transporter only) -->
            <div
              v-if="tp.type === 'TRANSPORTER'"
              class="expansion-section"
            >
              <div class="expansion-header">
                <span class="expansion-title">Fleet Assets</span>
              </div>
              <DataTable
                :value="fleetAssetsByPartner[tp.id] ?? []"
                size="small"
                class="sub-table"
              >
                <template #empty>
                  <span class="lab-muted">No fleet assets registered yet.</span>
                </template>
                <Column
                  header="Registration No"
                  field="registrationNo"
                />
                <Column
                  header="Make"
                  field="make"
                />
                <Column
                  header="Model"
                  field="model"
                />
                <Column
                  header="Vehicle Type"
                  field="vehicleTypeCode"
                />
              </DataTable>
            </div>

            <!-- Audit trail (BR-TP06) -->
            <div class="expansion-section">
              <div class="expansion-header">
                <span class="expansion-title">Audit Trail</span>
              </div>
              <DataTable
                :value="auditByPartner[tp.id] ?? []"
                size="small"
                class="sub-table"
              >
                <template #empty>
                  <span class="lab-muted">No audit events yet.</span>
                </template>
                <Column
                  header="Action"
                  field="action"
                />
                <Column
                  header="Actor"
                  field="actor"
                />
                <Column header="Reason">
                  <template #body="{ data: e }">
                    {{ e.metadata?.reason ?? '' }}
                  </template>
                </Column>
                <Column header="When">
                  <template #body="{ data: e }">
                    {{ formatDate(e.createdAt) }}
                  </template>
                </Column>
              </DataTable>
            </div>
          </template>
        </div>
      </template>
      <!-- No Type column — every row in this table is the panel's own role. -->
      <Column
        header="Name"
        field="name"
      />
      <Column header="Status">
        <template #body="{ data }">
          <Tag
            :severity="statusSeverity(data.status)"
            :value="data.status"
          />
        </template>
      </Column>
      <Column
        header=""
        style="width: 2.5rem"
      >
        <template #body="{ data }">
          <Button
            icon="pi pi-ellipsis-v"
            text
            size="small"
            aria-label="Organization actions"
            @click.stop="openRowMenu($event, data)"
          />
        </template>
      </Column>
    </DataTable>

    <Menu
      ref="rowMenu"
      :model="rowMenuItems"
      popup
    />

    <Dialog
      v-model:visible="registerOpen"
      :header="`Register ${roleLabel}`"
      modal
      :style="{ width: '26rem' }"
    >
      <div class="form-field">
        <label for="tp-name">Name</label>
        <InputText
          id="tp-name"
          v-model="registerForm.name"
          :placeholder="isTransporter ? 'e.g. Acme Trucking' : 'e.g. Globex Manufacturing'"
          autofocus
        />
      </div>
      <p
        v-if="registerError"
        class="error-text"
      >
        {{ registerError }}
      </p>
      <template #footer>
        <Button
          label="Cancel"
          text
          @click="registerOpen = false"
        />
        <Button
          label="Register"
          :loading="registering"
          :disabled="!registerForm.name"
          @click="submitRegister"
        />
      </template>
    </Dialog>

    <Dialog
      v-model:visible="suspendOpen"
      :header="suspendPartner ? `Suspend — ${suspendPartner.name}` : 'Suspend'"
      modal
      :style="{ width: '26rem' }"
    >
      <div class="form-field">
        <label for="suspend-reason">Reason</label>
        <Textarea
          id="suspend-reason"
          v-model="suspendReason"
          rows="3"
          placeholder="Required — recorded in the audit trail"
          autofocus
        />
      </div>
      <p
        v-if="suspendError"
        class="error-text"
      >
        {{ suspendError }}
      </p>
      <template #footer>
        <Button
          label="Cancel"
          text
          @click="suspendOpen = false"
        />
        <Button
          label="Suspend"
          severity="danger"
          :loading="suspendSaving"
          :disabled="!suspendReason"
          @click="submitSuspend"
        />
      </template>
    </Dialog>

    <Dialog
      v-model:visible="addDocOpen"
      :header="addDocPartner ? `Add Document — ${addDocPartner.name}` : 'Add Document'"
      modal
      :style="{ width: '26rem' }"
    >
      <div class="form-field">
        <label for="doc-type">Type</label>
        <Select
          id="doc-type"
          v-model="addDocForm.type"
          :options="addDocPartner ? documentTypesFor(addDocPartner.type) : []"
        />
      </div>
      <div class="form-field">
        <label for="doc-reference">Reference</label>
        <InputText
          id="doc-reference"
          v-model="addDocForm.reference"
          placeholder="Opaque external document locator"
        />
      </div>
      <p
        class="lab-muted"
        style="font-size: 0.8rem; margin: 0"
      >
        Metadata-only in v1 — this is a reference to the document, not a file upload.
      </p>
      <p
        v-if="addDocError"
        class="error-text"
      >
        {{ addDocError }}
      </p>
      <template #footer>
        <Button
          label="Cancel"
          text
          @click="addDocOpen = false"
        />
        <Button
          label="Add"
          :loading="addDocSaving"
          :disabled="!addDocForm.type || !addDocForm.reference"
          @click="submitAddDocument"
        />
      </template>
    </Dialog>

    <Dialog
      v-model:visible="addFleetOpen"
      :header="addFleetPartner ? `Add Fleet Asset — ${addFleetPartner.name}` : 'Add Fleet Asset'"
      modal
      :style="{ width: '28rem' }"
    >
      <div class="form-field">
        <label for="fleet-reg">Registration No</label>
        <InputText
          id="fleet-reg"
          v-model="addFleetForm.registrationNo"
          placeholder="e.g. CA123456"
          autofocus
        />
      </div>
      <div class="form-grid">
        <div class="form-field">
          <label for="fleet-make">Make</label>
          <InputText
            id="fleet-make"
            v-model="addFleetForm.make"
            placeholder="e.g. Volvo"
          />
        </div>
        <div class="form-field">
          <label for="fleet-model">Model</label>
          <InputText
            id="fleet-model"
            v-model="addFleetForm.model"
            placeholder="e.g. FH16"
          />
        </div>
      </div>
      <div class="form-field">
        <label for="fleet-vin">VIN</label>
        <InputText
          id="fleet-vin"
          v-model="addFleetForm.vin"
        />
      </div>
      <div class="form-field">
        <label for="fleet-vtc">Vehicle Type Code</label>
        <InputText
          id="fleet-vtc"
          v-model="addFleetForm.vehicleTypeCode"
          placeholder="e.g. TAUTLINER"
        />
      </div>
      <p
        class="lab-muted"
        style="font-size: 0.8rem; margin: 0"
      >
        Validated against refdata-service's <code>vehicle-type</code> corpus (BR-TP14) via the
        <strong>{{ tenantStore.tenant }}</strong> tenant's NATS connection.
      </p>
      <p
        v-if="addFleetError"
        class="error-text"
      >
        {{ addFleetError }}
      </p>
      <template #footer>
        <Button
          label="Cancel"
          text
          @click="addFleetOpen = false"
        />
        <Button
          label="Add"
          :loading="addFleetSaving"
          :disabled="!addFleetForm.registrationNo || !addFleetForm.vehicleTypeCode"
          @click="submitAddFleetAsset"
        />
      </template>
    </Dialog>
  </div>
</template>

<style scoped>
.organizations-panel {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 0.25rem;
}
.panel-title {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.04em;
  text-transform: uppercase;
  color: var(--lab-accent);
}
.description {
  margin: 0;
  font-size: 0.85rem;
}
.error-text {
  margin: 0;
  color: var(--p-red-400, #f87171);
  font-size: 0.85rem;
}
.form-field {
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  margin-bottom: 0.75rem;
}
.form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 0.75rem;
}
.expansion {
  padding: 0.5rem 0.5rem 0.75rem 2.75rem;
  position: relative;
  background: linear-gradient(to right, var(--lab-bg) 1.1rem, var(--lab-nested-bg) 1.1rem);
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
}
.expansion::before {
  content: '';
  position: absolute;
  left: 1.1rem;
  top: 0;
  bottom: 0.25rem;
  width: 2px;
  background: rgba(0, 111, 255, 0.35);
  border-radius: 1px;
}
.expansion-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 0.3rem;
}
.expansion-title {
  font-size: 0.8rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--lab-accent);
}
.sub-table {
  width: 100%;
  --p-datatable-header-cell-background: color-mix(in srgb, var(--lab-nested-bg) 95%, var(--lab-accent) 5%);
}
.sub-table :deep(.p-datatable-tbody > tr) {
  background-color: var(--lab-nested-bg);
}
.doc-actions {
  display: flex;
  gap: 0.25rem;
}
.ref-col {
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 0.8rem;
}
</style>
