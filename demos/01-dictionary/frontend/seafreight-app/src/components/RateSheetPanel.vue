<script setup>
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import DatePicker from 'primevue/datepicker'
import Dialog from 'primevue/dialog'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import {
  activeRateSheetVersion,
  addRateSheetEntry,
  applyDieselOverlay,
  createRateSheetDraft,
  publishRateSheet,
  rateSheetVersions,
  rollbackRateSheet,
  setRateSheetFeeScaleOverride,
} from '../api'
import { usePricingStore } from '../stores/pricing'

// Manual-entry UX for RateSheet (Phase 25h) — same shape as FeeScalePanel:
// register, build a draft's lane entries one at a time, optionally set a
// fee-scale override on the draft, publish, roll back. See FeeScalePanel's
// doc comment for why entries are tracked locally rather than re-fetched —
// the same "no get-one-version-by-number endpoint" gap applies here.
const store = usePricingStore()
const { t } = useI18n()

const expandedRows = ref({})
const detailByName = reactive({})

function detail(name) {
  if (!detailByName[name]) {
    detailByName[name] = {
      loading: false,
      error: '',
      active: null,
      versions: [],
      draftVersion: null,
      draftEntries: [],
      newEntry: { routeKey: '', vehicleType: '', baseRate: null, dropPointCount: null, additionalDropRate: null, error: '', busy: false },
      overrideName: '',
      settingOverride: false,
      creatingDraft: false,
      publishing: false,
      rollingBack: false,
      overlayDate: null,
      overlayError: '',
      applyingOverlay: false,
    }
  }
  return detailByName[name]
}

function toCents(dollars) {
  return Math.round((dollars ?? 0) * 100)
}
function fromCents(cents) {
  return (cents ?? 0) / 100
}
// See DieselPricePanel.vue's dateOnlyISOString for why plain .toISOString()
// on a DatePicker value is wrong in positive-UTC-offset timezones.
function dateOnlyISOString(date) {
  return new Date(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate())).toISOString()
}

async function loadDetail(name) {
  const d = detail(name)
  d.loading = true
  d.error = ''
  try {
    d.versions = (await rateSheetVersions(store.context, name)) ?? []
    const draft = d.versions.find((v) => v.status === 'draft')
    d.draftVersion = draft ? draft.version : null
    if (!draft) d.draftEntries = []
    try {
      d.active = await activeRateSheetVersion(store.context, name)
    } catch {
      d.active = null
    }
  } catch (err) {
    d.error = err.message
  } finally {
    d.loading = false
  }
}

function onRowExpand(event) {
  loadDetail(event.data.name)
}

// ── Register ───────────────────────────────────────────────────────────────

const registerOpen = ref(false)
const registerForm = reactive({ name: '', customerKey: '', type: 'normal', active: true })
const registerError = ref('')
const registering = ref(false)

function openRegister() {
  registerForm.name = ''
  registerForm.customerKey = ''
  registerForm.type = 'normal'
  registerForm.active = true
  registerError.value = ''
  registerOpen.value = true
}

async function submitRegister() {
  if (!registerForm.name.trim()) return
  registering.value = true
  registerError.value = ''
  try {
    await store.registerRateSheet({ ...registerForm, name: registerForm.name.trim() })
    registerOpen.value = false
  } catch (err) {
    registerError.value = err.message
  } finally {
    registering.value = false
  }
}

async function toggleActive(rateSheet) {
  try {
    await store.toggleRateSheetActive(rateSheet)
  } catch (err) {
    detail(rateSheet.name).error = err.message
  }
}

// ── Draft / add entry / override / publish ────────────────────────────────

async function createDraft(name) {
  const d = detail(name)
  d.creatingDraft = true
  d.error = ''
  try {
    const version = await createRateSheetDraft(store.context, name)
    d.draftVersion = version.version
    d.draftEntries = []
    d.versions = [version, ...d.versions]
  } catch (err) {
    d.error = err.message
  } finally {
    d.creatingDraft = false
  }
}

async function submitAddEntry(name) {
  const d = detail(name)
  const ne = d.newEntry
  if (!ne.routeKey.trim() || !ne.vehicleType.trim()) {
    ne.error = t('pricing.routeAndVehicleRequired')
    return
  }
  const entry = {
    routeKey: ne.routeKey.trim(),
    vehicleType: ne.vehicleType.trim(),
    centBaseRate: toCents(ne.baseRate),
    dropPointCount: ne.dropPointCount ?? 0,
    centAdditionalDropRate: toCents(ne.additionalDropRate),
  }
  ne.busy = true
  ne.error = ''
  try {
    await addRateSheetEntry(store.context, name, d.draftVersion, entry)
    d.draftEntries.push(entry)
    ne.routeKey = ''
    ne.vehicleType = ''
    ne.baseRate = null
    ne.dropPointCount = null
    ne.additionalDropRate = null
  } catch (err) {
    ne.error = err.message
  } finally {
    ne.busy = false
  }
}

async function submitOverride(name) {
  const d = detail(name)
  if (!d.overrideName.trim()) return
  d.settingOverride = true
  d.error = ''
  try {
    await setRateSheetFeeScaleOverride(store.context, name, d.draftVersion, d.overrideName.trim())
  } catch (err) {
    d.error = err.message
  } finally {
    d.settingOverride = false
  }
}

// Applies a diesel overlay to the ACTIVE published version, independent of
// the draft/publish flow above (BR-P17/BR-P20) — resolves the diesel price
// in effect on the chosen date from the DieselPricePanel index and appends
// a minor-version overlay; no draft or publish step involved.
async function submitApplyOverlay(name) {
  const d = detail(name)
  if (!d.overlayDate) return
  d.applyingOverlay = true
  d.overlayError = ''
  try {
    await applyDieselOverlay(store.context, name, dateOnlyISOString(d.overlayDate))
    d.overlayDate = null
    await loadDetail(name)
  } catch (err) {
    d.overlayError = err.message
  } finally {
    d.applyingOverlay = false
  }
}

async function submitPublish(name) {
  const d = detail(name)
  d.publishing = true
  d.error = ''
  try {
    await publishRateSheet(store.context, name)
    await loadDetail(name)
  } catch (err) {
    d.error = err.message
  } finally {
    d.publishing = false
  }
}

async function submitRollback(name, targetVersion) {
  const d = detail(name)
  d.rollingBack = true
  d.error = ''
  try {
    await rollbackRateSheet(store.context, name, targetVersion)
    await loadDetail(name)
  } catch (err) {
    d.error = err.message
  } finally {
    d.rollingBack = false
  }
}

const typeOptions = [
  { label: 'Normal', value: 'normal' },
  { label: 'Fixed Rate', value: 'fixed-rate' },
]
</script>

<template>
  <div class="pricing-group">
    <div class="group-head">
      <h4>{{ t('pricing.rateSheets') }}</h4>
      <Button icon="pi pi-plus" :aria-label="t('pricing.registerRateSheet')" text rounded size="small" @click="openRegister" />
    </div>
    <DataTable
      v-model:expandedRows="expandedRows"
      :value="store.rateSheets"
      size="small"
      data-key="name"
      resizableColumns
      columnResizeMode="expand"
      @row-expand="onRowExpand"
    >
      <template #empty>
        <span class="lab-muted">{{ t('pricing.rateSheetsEmpty') }}</span>
      </template>
      <Column expander style="width:2.5rem" />
      <Column field="name" :header="t('table.name')" style="font-family:monospace;font-size:12px" />
      <Column field="customerKey" :header="t('table.customerKey')" />
      <Column :header="t('table.type')" style="width:120px">
        <template #body="{ data }">
          <Tag severity="info" :value="data.type === 'fixed-rate' ? t('pricing.typeFixedRate') : t('pricing.typeNormal')" />
        </template>
      </Column>
      <Column :header="t('status.label')" style="width:160px">
        <template #body="{ data }">
          <div class="status-cell">
            <Tag :severity="data.active ? 'success' : 'danger'" :value="data.active ? t('status.active') : t('status.inactive')" />
            <Button :label="data.active ? t('pricing.deactivate') : t('pricing.activate')" text size="small" @click="toggleActive(data)" />
          </div>
        </template>
      </Column>

      <template #expansion="{ data }">
        <div class="detail">
          <p v-if="detail(data.name).loading" class="lab-muted loading-line">
            <span class="spinner" aria-hidden="true" />
            {{ t('pricing.loadingDetail') }}
          </p>
          <template v-else>
            <div class="detail-row">
              <strong>{{ t('pricing.activeVersion') }}:</strong>
              <span v-if="detail(data.name).active">
                v{{ detail(data.name).active.version }}.{{ detail(data.name).active.minorVersion ?? 0 }} — {{ t('pricing.entryCount', detail(data.name).active.entries?.length ?? 0) }}
                <template v-if="detail(data.name).active.feeScaleOverride"> · {{ t('pricing.feeScaleOverride') }}: {{ detail(data.name).active.feeScaleOverride }}</template>
              </span>
              <span v-else class="lab-muted">{{ t('pricing.noActiveVersion') }}</span>
            </div>
            <DataTable v-if="detail(data.name).active?.entries?.length" :value="detail(data.name).active.entries" size="small" class="ranges-table">
              <Column field="routeKey" :header="t('table.routeKey')" />
              <Column field="vehicleType" :header="t('pricing.vehicleType')" />
              <Column :header="t('pricing.baseRate')"><template #body="{ data: e }">${{ fromCents(e.centBaseRate).toFixed(2) }}</template></Column>
              <Column field="dropPointCount" :header="t('pricing.dropPointCount')" />
              <Column :header="t('pricing.additionalDropRate')"><template #body="{ data: e }">${{ fromCents(e.centAdditionalDropRate).toFixed(2) }}</template></Column>
            </DataTable>

            <template v-if="detail(data.name).active">
              <div class="detail-row">
                <strong>{{ t('pricing.dieselOverlays') }}:</strong>
              </div>
              <DataTable v-if="detail(data.name).active.overlays?.length" :value="detail(data.name).active.overlays" size="small" class="ranges-table">
                <Column field="routeKey" :header="t('table.routeKey')" />
                <Column field="vehicleType" :header="t('pricing.vehicleType')" />
                <Column field="minorVersion" :header="t('pricing.minorVersion')" style="width:70px" />
                <Column :header="t('pricing.overlayStart')"><template #body="{ data: o }">{{ o.startDate?.slice(0, 10) }}</template></Column>
                <Column :header="t('pricing.overlayEnd')"><template #body="{ data: o }">{{ o.endDate ? o.endDate.slice(0, 10) : t('pricing.overlayOpenEnded') }}</template></Column>
                <Column :header="t('pricing.adjustedRate')"><template #body="{ data: o }">${{ fromCents(o.centAdjustedRate).toFixed(2) }}</template></Column>
              </DataTable>
              <span v-else class="lab-muted">{{ t('pricing.noOverlays') }}</span>

              <div class="add-range-row">
                <DatePicker v-model="detail(data.name).overlayDate" :placeholder="t('pricing.overlayDatePlaceholder')" date-format="yy-mm-dd" show-icon size="small" style="width:160px" />
                <Button
                  :label="t('pricing.applyOverlay')"
                  size="small"
                  :disabled="!detail(data.name).overlayDate"
                  :loading="detail(data.name).applyingOverlay"
                  @click="submitApplyOverlay(data.name)"
                />
              </div>
              <div v-if="detail(data.name).overlayError" class="domain-error">{{ detail(data.name).overlayError }}</div>
            </template>

            <div class="detail-row">
              <strong>{{ t('pricing.draft') }}:</strong>
              <span v-if="detail(data.name).draftVersion !== null">v{{ detail(data.name).draftVersion }}</span>
              <span v-else class="lab-muted">{{ t('pricing.noDraft') }}</span>
              <Button
                v-if="detail(data.name).draftVersion === null"
                :label="t('pricing.createDraft')"
                size="small"
                :loading="detail(data.name).creatingDraft"
                @click="createDraft(data.name)"
              />
              <Button
                v-else
                :label="t('pricing.publishDraft')"
                size="small"
                :disabled="detail(data.name).draftEntries.length === 0 || detail(data.name).publishing"
                :loading="detail(data.name).publishing"
                @click="submitPublish(data.name)"
              />
            </div>

            <template v-if="detail(data.name).draftVersion !== null">
              <DataTable v-if="detail(data.name).draftEntries.length" :value="detail(data.name).draftEntries" size="small" class="ranges-table">
                <Column field="routeKey" :header="t('table.routeKey')" />
                <Column field="vehicleType" :header="t('pricing.vehicleType')" />
                <Column :header="t('pricing.baseRate')"><template #body="{ data: e }">${{ fromCents(e.centBaseRate).toFixed(2) }}</template></Column>
                <Column field="dropPointCount" :header="t('pricing.dropPointCount')" />
                <Column :header="t('pricing.additionalDropRate')"><template #body="{ data: e }">${{ fromCents(e.centAdditionalDropRate).toFixed(2) }}</template></Column>
              </DataTable>

              <div class="add-range-row">
                <InputText v-model="detail(data.name).newEntry.routeKey" :placeholder="t('pricing.routeKeyPlaceholder')" size="small" style="width:120px" />
                <InputText v-model="detail(data.name).newEntry.vehicleType" :placeholder="t('pricing.vehicleTypePlaceholder')" size="small" style="width:100px" />
                <InputNumber v-model="detail(data.name).newEntry.baseRate" :placeholder="t('pricing.baseRate')" mode="decimal" :min-fraction-digits="2" :max-fraction-digits="2" size="small" style="width:100px" />
                <InputNumber v-model="detail(data.name).newEntry.dropPointCount" :placeholder="t('pricing.dropPointCount')" size="small" style="width:90px" />
                <InputNumber v-model="detail(data.name).newEntry.additionalDropRate" :placeholder="t('pricing.additionalDropRate')" mode="decimal" :min-fraction-digits="2" :max-fraction-digits="2" size="small" style="width:100px" />
                <Button :label="t('pricing.addEntry')" size="small" :loading="detail(data.name).newEntry.busy" @click="submitAddEntry(data.name)" />
              </div>
              <div v-if="detail(data.name).newEntry.error" class="domain-error">{{ detail(data.name).newEntry.error }}</div>

              <div class="add-range-row">
                <InputText v-model="detail(data.name).overrideName" :placeholder="t('pricing.feeScaleOverridePlaceholder')" size="small" style="width:180px" />
                <Button :label="t('pricing.setOverride')" size="small" text :loading="detail(data.name).settingOverride" @click="submitOverride(data.name)" />
              </div>
            </template>

            <div v-if="detail(data.name).error" class="domain-error">{{ detail(data.name).error }}</div>

            <div class="detail-row">
              <strong>{{ t('pricing.versionHistory') }}:</strong>
            </div>
            <DataTable :value="detail(data.name).versions" size="small" data-key="version" class="versions-table">
              <template #empty><span class="lab-muted">{{ t('pricing.noVersions') }}</span></template>
              <Column field="version" :header="t('pricing.version')" style="width:80px" />
              <Column :header="t('status.label')" style="width:120px">
                <template #body="{ data: v }"><Tag :severity="v.status === 'published' ? 'success' : v.status === 'draft' ? 'warning' : 'secondary'" :value="v.status" /></template>
              </Column>
              <Column header="" style="width:140px">
                <template #body="{ data: v }">
                  <Button
                    v-if="v.status === 'published'"
                    :label="t('pricing.rollbackTo')"
                    size="small"
                    text
                    :disabled="detail(data.name).rollingBack"
                    @click="submitRollback(data.name, v.version)"
                  />
                </template>
              </Column>
            </DataTable>
          </template>
        </div>
      </template>
    </DataTable>
  </div>

  <Dialog v-model:visible="registerOpen" :header="t('pricing.registerRateSheet')" modal style="width:24rem">
    <div class="form-field">
      <InputText v-model="registerForm.name" :placeholder="t('pricing.namePlaceholder')" size="small" style="width:100%" />
    </div>
    <div class="form-field">
      <InputText v-model="registerForm.customerKey" :placeholder="t('pricing.customerKeyPlaceholder')" size="small" style="width:100%" />
    </div>
    <div class="form-field">
      <Select v-model="registerForm.type" :options="typeOptions" option-label="label" option-value="value" size="small" style="width:100%" />
    </div>
    <div class="form-field checkbox-field">
      <Checkbox v-model="registerForm.active" :binary="true" input-id="rateSheetActive" />
      <label for="rateSheetActive">{{ t('status.active') }}</label>
    </div>
    <div v-if="registerError" class="domain-error dialog-note">{{ registerError }}</div>
    <template #footer>
      <Button :label="t('action.cancel')" text size="small" @click="registerOpen = false" />
      <Button :label="t('action.register')" size="small" :disabled="!registerForm.name.trim()" :loading="registering" @click="submitRegister" />
    </template>
  </Dialog>
</template>

<style scoped>
.pricing-group {
  margin-bottom: 1rem;
}
.group-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 0.4rem;
}
.group-head h4 {
  margin: 0;
  font-size: 12px;
  line-height: 18px;
  letter-spacing: 0.02em;
}
.status-cell {
  display: flex;
  align-items: center;
  gap: 0.4rem;
}
.detail {
  padding: 0.5rem 0.5rem 0.75rem 2.5rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.detail-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  font-size: 12px;
}
.ranges-table,
.versions-table {
  --p-datatable-header-cell-background: color-mix(in srgb, var(--lab-panel-bg) 90%, var(--lab-accent) 10%);
  --p-datatable-row-background: color-mix(in srgb, var(--lab-panel-bg) 96%, var(--lab-accent) 4%);
}
.add-range-row {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.domain-error {
  font-size: 0.85rem;
  color: var(--p-red-400, #f87171);
  background: rgba(248, 113, 113, 0.08);
  border: 1px solid rgba(248, 113, 113, 0.25);
  border-radius: 4px;
  padding: 0.35rem 0.6rem;
}
.dialog-note {
  margin-top: 0.5rem;
}
.form-field {
  margin-bottom: 0.6rem;
}
.checkbox-field {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}
.loading-line {
  margin: 0;
  font-size: 12px;
  display: flex;
  align-items: center;
  gap: 8px;
}
.spinner {
  flex-shrink: 0;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  border: 2px solid var(--lab-panel-border);
  border-top-color: var(--lab-accent);
  animation: spin 0.7s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
@media (prefers-reduced-motion: reduce) {
  .spinner {
    animation: none;
  }
}
</style>
