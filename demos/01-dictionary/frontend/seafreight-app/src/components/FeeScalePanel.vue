<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Select from 'primevue/select'
import Tag from 'primevue/tag'
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { activeFeeScaleVersion, addFeeScaleRange, createFeeScaleDraft, feeScaleVersions, publishFeeScale, rollbackFeeScale } from '../api'
import { usePricingStore } from '../stores/pricing'

// Manual-entry UX for FeeScale (Phase 25h): register, build a draft's
// ranges one at a time (lower limit auto-chained from the previous range's
// upper limit — Linebooker's own ergonomic, kept per Main-POC-Plan.md's
// 25h note), publish, and roll back to any prior published version.
// Deliberately NOT carried over from Linebooker: forcing the last range's
// upper limit to infinity (BR-P05 exists specifically to reject a bid above
// every configured range instead of silently charging zero — a forced-
// infinite top range would recreate that exact bug) and the date-driven
// "no publish step" versioning (this port's corpus draft/publish/rollback
// lifecycle, BR-P02, was already chosen over that model in Phase 25a).
//
// There is no "get one version's ranges" endpoint for an arbitrary version
// (Versions() returns metadata only — status/parent/rolledBackBy, no
// ranges; only ActiveVersion() resolves ranges, and only for a published
// version). So a draft's ranges are tracked locally as they're added in
// this session (each AddRange call's own input is the only record the
// browser needs) rather than re-fetched from the server — reloading the
// page mid-draft loses the in-progress range list from view, though the
// ranges themselves remain persisted and are still there once published.
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
      draftRanges: [],
      newRange: { upperLimit: null, rateType: 'flat', fee: null, percentage: null, error: '', busy: false },
      creatingDraft: false,
      publishing: false,
      rollbackTarget: null,
      rollingBack: false,
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

async function loadDetail(name) {
  const d = detail(name)
  d.loading = true
  d.error = ''
  try {
    d.versions = (await feeScaleVersions(store.context, name)) ?? []
    const draft = d.versions.find((v) => v.status === 'draft')
    d.draftVersion = draft ? draft.version : null
    if (!draft) d.draftRanges = []
    try {
      d.active = await activeFeeScaleVersion(store.context, name)
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
const registerName = ref('')
const registerError = ref('')
const registering = ref(false)

function openRegister() {
  registerName.value = ''
  registerError.value = ''
  registerOpen.value = true
}

async function submitRegister() {
  const name = registerName.value.trim()
  if (!name) return
  registering.value = true
  registerError.value = ''
  try {
    await store.registerFeeScale(name)
    registerOpen.value = false
  } catch (err) {
    registerError.value = err.message
  } finally {
    registering.value = false
  }
}

// ── Draft / add range / publish ───────────────────────────────────────────

async function createDraft(name) {
  const d = detail(name)
  d.creatingDraft = true
  d.error = ''
  try {
    const version = await createFeeScaleDraft(store.context, name)
    d.draftVersion = version.version
    d.draftRanges = []
    d.versions = [version, ...d.versions]
  } catch (err) {
    d.error = err.message
  } finally {
    d.creatingDraft = false
  }
}

function nextLowerLimitCents(d) {
  if (d.draftRanges.length === 0) return 0
  return d.draftRanges[d.draftRanges.length - 1].centUpperLimit
}

async function submitAddRange(name) {
  const d = detail(name)
  const nr = d.newRange
  const lowerCents = nextLowerLimitCents(d)
  const upperCents = toCents(nr.upperLimit)
  if (!nr.upperLimit || upperCents <= lowerCents) {
    nr.error = t('pricing.rangeUpperMustExceedLower')
    return
  }
  const range = {
    centLowerLimit: lowerCents,
    centUpperLimit: upperCents,
    rateType: nr.rateType,
    centFee: nr.rateType === 'flat' ? toCents(nr.fee) : 0,
    percentageFee: nr.rateType === 'percentage' ? (nr.percentage ?? 0) / 100 : 0,
  }
  nr.busy = true
  nr.error = ''
  try {
    await addFeeScaleRange(store.context, name, d.draftVersion, range)
    d.draftRanges.push(range)
    nr.upperLimit = null
    nr.fee = null
    nr.percentage = null
  } catch (err) {
    nr.error = err.message
  } finally {
    nr.busy = false
  }
}

async function submitPublish(name) {
  const d = detail(name)
  d.publishing = true
  d.error = ''
  try {
    await publishFeeScale(store.context, name)
    await loadDetail(name)
  } catch (err) {
    d.error = err.message
  } finally {
    d.publishing = false
  }
}

async function submitRollback(name) {
  const d = detail(name)
  if (!d.rollbackTarget) return
  d.rollingBack = true
  d.error = ''
  try {
    await rollbackFeeScale(store.context, name, d.rollbackTarget)
    d.rollbackTarget = null
    await loadDetail(name)
  } catch (err) {
    d.error = err.message
  } finally {
    d.rollingBack = false
  }
}

const rateTypeOptions = [
  { label: 'Flat', value: 'flat' },
  { label: 'Percentage', value: 'percentage' },
]
</script>

<template>
  <div class="pricing-group">
    <div class="group-head">
      <h4>{{ t('pricing.feeScales') }}</h4>
      <Button icon="pi pi-plus" :aria-label="t('pricing.registerFeeScale')" text rounded size="small" @click="openRegister" />
    </div>
    <DataTable
      v-model:expandedRows="expandedRows"
      :value="store.feeScales"
      size="small"
      data-key="name"
      resizableColumns
      columnResizeMode="expand"
      @row-expand="onRowExpand"
    >
      <template #empty>
        <span class="lab-muted">{{ t('pricing.feeScalesEmpty') }}</span>
      </template>
      <Column expander style="width:2.5rem" />
      <Column field="name" :header="t('table.name')" style="font-family:monospace;font-size:12px" />

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
                v{{ detail(data.name).active.version }} — {{ t('pricing.rangeCount', detail(data.name).active.ranges?.length ?? 0) }}
              </span>
              <span v-else class="lab-muted">{{ t('pricing.noActiveVersion') }}</span>
            </div>
            <DataTable v-if="detail(data.name).active?.ranges?.length" :value="detail(data.name).active.ranges" size="small" class="ranges-table">
              <Column :header="t('pricing.rangeLower')"><template #body="{ data: r }">{{ fromCents(r.centLowerLimit).toFixed(2) }}</template></Column>
              <Column :header="t('pricing.rangeUpper')"><template #body="{ data: r }">{{ fromCents(r.centUpperLimit).toFixed(2) }}</template></Column>
              <Column :header="t('table.type')" field="rateType" />
              <Column :header="t('pricing.rangeCharge')">
                <template #body="{ data: r }">{{ r.rateType === 'flat' ? `$${fromCents(r.centFee).toFixed(2)}` : `${(r.percentageFee * 100).toFixed(2)}%` }}</template>
              </Column>
            </DataTable>

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
                :disabled="detail(data.name).draftRanges.length === 0 || detail(data.name).publishing"
                :loading="detail(data.name).publishing"
                @click="submitPublish(data.name)"
              />
            </div>

            <template v-if="detail(data.name).draftVersion !== null">
              <DataTable v-if="detail(data.name).draftRanges.length" :value="detail(data.name).draftRanges" size="small" class="ranges-table">
                <Column :header="t('pricing.rangeLower')"><template #body="{ data: r }">{{ fromCents(r.centLowerLimit).toFixed(2) }}</template></Column>
                <Column :header="t('pricing.rangeUpper')"><template #body="{ data: r }">{{ fromCents(r.centUpperLimit).toFixed(2) }}</template></Column>
                <Column :header="t('table.type')" field="rateType" />
                <Column :header="t('pricing.rangeCharge')">
                  <template #body="{ data: r }">{{ r.rateType === 'flat' ? `$${fromCents(r.centFee).toFixed(2)}` : `${(r.percentageFee * 100).toFixed(2)}%` }}</template>
                </Column>
              </DataTable>

              <div class="add-range-row">
                <span class="lab-muted">{{ t('pricing.rangeStartsAt', { value: fromCents(nextLowerLimitCents(detail(data.name))).toFixed(2) }) }}</span>
                <InputNumber v-model="detail(data.name).newRange.upperLimit" :placeholder="t('pricing.rangeUpper')" mode="decimal" :min-fraction-digits="2" :max-fraction-digits="2" size="small" style="width:110px" />
                <Select v-model="detail(data.name).newRange.rateType" :options="rateTypeOptions" option-label="label" option-value="value" size="small" style="width:120px" />
                <InputNumber
                  v-if="detail(data.name).newRange.rateType === 'flat'"
                  v-model="detail(data.name).newRange.fee"
                  :placeholder="t('pricing.rangeCharge')"
                  mode="decimal"
                  :min-fraction-digits="2"
                  :max-fraction-digits="2"
                  size="small"
                  style="width:100px"
                />
                <InputNumber
                  v-else
                  v-model="detail(data.name).newRange.percentage"
                  :placeholder="t('pricing.percentPlaceholder')"
                  suffix="%"
                  size="small"
                  style="width:100px"
                />
                <Button :label="t('pricing.addRange')" size="small" :loading="detail(data.name).newRange.busy" @click="submitAddRange(data.name)" />
              </div>
              <div v-if="detail(data.name).newRange.error" class="domain-error">{{ detail(data.name).newRange.error }}</div>
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
                    @click="detail(data.name).rollbackTarget = v.version; submitRollback(data.name)"
                  />
                </template>
              </Column>
            </DataTable>
          </template>
        </div>
      </template>
    </DataTable>
  </div>

  <Dialog v-model:visible="registerOpen" :header="t('pricing.registerFeeScale')" modal style="width:22rem">
    <InputText v-model="registerName" :placeholder="t('pricing.namePlaceholder')" size="small" style="width:100%" @keyup.enter="submitRegister" />
    <div v-if="registerError" class="domain-error dialog-note">{{ registerError }}</div>
    <template #footer>
      <Button :label="t('action.cancel')" text size="small" @click="registerOpen = false" />
      <Button :label="t('action.register')" size="small" :disabled="!registerName.trim()" :loading="registering" @click="submitRegister" />
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
