<script setup>
import Button from 'primevue/button'
import Checkbox from 'primevue/checkbox'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import Dialog from 'primevue/dialog'
import InputNumber from 'primevue/inputnumber'
import InputText from 'primevue/inputtext'
import Tag from 'primevue/tag'
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { activeFixedRateVersion, createFixedRateDraft, fixedRateVersions, publishFixedRate, rollbackFixedRate } from '../api'
import { usePricingStore } from '../stores/pricing'

// Manual-entry UX for FixedRate (Phase 25h). Unlike FeeScale/RateSheet,
// domain.FixedRateRepository.CreateDraft takes centRate/pointCount/
// centAdditionalDropRate directly — a FixedRate version has no separate
// per-row entries to add incrementally, so "create draft" and "fill in the
// rate" are the same single dialog here, not a two-step create-then-add
// flow.
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
      draft: null,
      publishing: false,
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
    d.versions = (await fixedRateVersions(store.context, name)) ?? []
    d.draft = d.versions.find((v) => v.status === 'draft') ?? null
    try {
      d.active = await activeFixedRateVersion(store.context, name)
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
const registerForm = reactive({ name: '', customerKey: '', routeKey: '', active: true })
const registerError = ref('')
const registering = ref(false)

function openRegister() {
  registerForm.name = ''
  registerForm.customerKey = ''
  registerForm.routeKey = ''
  registerForm.active = true
  registerError.value = ''
  registerOpen.value = true
}

async function submitRegister() {
  if (!registerForm.name.trim()) return
  registering.value = true
  registerError.value = ''
  try {
    await store.registerFixedRate({ ...registerForm, name: registerForm.name.trim() })
    registerOpen.value = false
  } catch (err) {
    registerError.value = err.message
  } finally {
    registering.value = false
  }
}

async function toggleActive(fixedRate) {
  try {
    await store.toggleFixedRateActive(fixedRate)
  } catch (err) {
    detail(fixedRate.name).error = err.message
  }
}

// ── Draft (create = fill in the rate directly) / publish ─────────────────

const draftDialogName = ref('')
const draftForm = reactive({ centRate: null, pointCount: null, centAdditionalDropRate: null })
const draftError = ref('')
const creatingDraft = ref(false)

function openDraftDialog(name) {
  draftDialogName.value = name
  draftForm.centRate = null
  draftForm.pointCount = null
  draftForm.centAdditionalDropRate = null
  draftError.value = ''
}

async function submitCreateDraft() {
  const name = draftDialogName.value
  creatingDraft.value = true
  draftError.value = ''
  try {
    const version = await createFixedRateDraft(
      store.context,
      name,
      toCents(draftForm.centRate),
      draftForm.pointCount ?? 0,
      toCents(draftForm.centAdditionalDropRate),
    )
    const d = detail(name)
    d.draft = { ...version, centRate: toCents(draftForm.centRate), pointCount: draftForm.pointCount ?? 0, centAdditionalDropRate: toCents(draftForm.centAdditionalDropRate) }
    d.versions = [d.draft, ...d.versions]
    draftDialogName.value = ''
  } catch (err) {
    draftError.value = err.message
  } finally {
    creatingDraft.value = false
  }
}

async function submitPublish(name) {
  const d = detail(name)
  d.publishing = true
  d.error = ''
  try {
    await publishFixedRate(store.context, name)
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
    await rollbackFixedRate(store.context, name, targetVersion)
    await loadDetail(name)
  } catch (err) {
    d.error = err.message
  } finally {
    d.rollingBack = false
  }
}
</script>

<template>
  <div class="pricing-group">
    <div class="group-head">
      <h4>{{ t('pricing.fixedRates') }}</h4>
      <Button icon="pi pi-plus" :aria-label="t('pricing.registerFixedRate')" text rounded size="small" @click="openRegister" />
    </div>
    <DataTable
      v-model:expandedRows="expandedRows"
      :value="store.fixedRates"
      size="small"
      data-key="name"
      resizableColumns
      columnResizeMode="expand"
      @row-expand="onRowExpand"
    >
      <template #empty>
        <span class="lab-muted">{{ t('pricing.fixedRatesEmpty') }}</span>
      </template>
      <Column expander style="width:2.5rem" />
      <Column field="name" :header="t('table.name')" style="font-family:monospace;font-size:12px" />
      <Column field="customerKey" :header="t('table.customerKey')" />
      <Column field="routeKey" :header="t('table.routeKey')" />
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
                v{{ detail(data.name).active.version }} — ${{ fromCents(detail(data.name).active.centRate).toFixed(2) }},
                {{ t('pricing.pointCountValue', detail(data.name).active.pointCount ?? 0) }},
                +${{ fromCents(detail(data.name).active.centAdditionalDropRate).toFixed(2) }}/{{ t('pricing.drop') }}
              </span>
              <span v-else class="lab-muted">{{ t('pricing.noActiveVersion') }}</span>
            </div>

            <div class="detail-row">
              <strong>{{ t('pricing.draft') }}:</strong>
              <span v-if="detail(data.name).draft">
                v{{ detail(data.name).draft.version }} — ${{ fromCents(detail(data.name).draft.centRate).toFixed(2) }},
                {{ t('pricing.pointCountValue', detail(data.name).draft.pointCount ?? 0) }},
                +${{ fromCents(detail(data.name).draft.centAdditionalDropRate).toFixed(2) }}/{{ t('pricing.drop') }}
              </span>
              <span v-else class="lab-muted">{{ t('pricing.noDraft') }}</span>
              <Button v-if="!detail(data.name).draft" :label="t('pricing.createDraft')" size="small" @click="openDraftDialog(data.name)" />
              <Button
                v-else
                :label="t('pricing.publishDraft')"
                size="small"
                :loading="detail(data.name).publishing"
                @click="submitPublish(data.name)"
              />
            </div>

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

  <Dialog v-model:visible="registerOpen" :header="t('pricing.registerFixedRate')" modal style="width:24rem">
    <div class="form-field">
      <InputText v-model="registerForm.name" :placeholder="t('pricing.namePlaceholder')" size="small" style="width:100%" />
    </div>
    <div class="form-field">
      <InputText v-model="registerForm.customerKey" :placeholder="t('pricing.customerKeyPlaceholder')" size="small" style="width:100%" />
    </div>
    <div class="form-field">
      <InputText v-model="registerForm.routeKey" :placeholder="t('pricing.routeKeyPlaceholder')" size="small" style="width:100%" />
    </div>
    <div class="form-field checkbox-field">
      <Checkbox v-model="registerForm.active" :binary="true" input-id="fixedRateActive" />
      <label for="fixedRateActive">{{ t('status.active') }}</label>
    </div>
    <div v-if="registerError" class="domain-error dialog-note">{{ registerError }}</div>
    <template #footer>
      <Button :label="t('action.cancel')" text size="small" @click="registerOpen = false" />
      <Button :label="t('action.register')" size="small" :disabled="!registerForm.name.trim()" :loading="registering" @click="submitRegister" />
    </template>
  </Dialog>

  <Dialog :visible="!!draftDialogName" :header="t('pricing.createDraft')" modal style="width:24rem" @update:visible="(v) => { if (!v) draftDialogName = '' }">
    <div class="form-field">
      <InputNumber v-model="draftForm.centRate" :placeholder="t('pricing.rate')" mode="decimal" :min-fraction-digits="2" :max-fraction-digits="2" size="small" style="width:100%" />
    </div>
    <div class="form-field">
      <InputNumber v-model="draftForm.pointCount" :placeholder="t('pricing.pointCount')" size="small" style="width:100%" />
    </div>
    <div class="form-field">
      <InputNumber v-model="draftForm.centAdditionalDropRate" :placeholder="t('pricing.additionalDropRate')" mode="decimal" :min-fraction-digits="2" :max-fraction-digits="2" size="small" style="width:100%" />
    </div>
    <div v-if="draftError" class="domain-error dialog-note">{{ draftError }}</div>
    <template #footer>
      <Button :label="t('action.cancel')" text size="small" @click="draftDialogName = ''" />
      <Button :label="t('pricing.createDraft')" size="small" :loading="creatingDraft" @click="submitCreateDraft" />
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
.versions-table {
  --p-datatable-header-cell-background: color-mix(in srgb, var(--lab-panel-bg) 90%, var(--lab-accent) 10%);
  --p-datatable-row-background: color-mix(in srgb, var(--lab-panel-bg) 96%, var(--lab-accent) 4%);
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
