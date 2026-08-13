<script setup>
import Button from 'primevue/button'
import Column from 'primevue/column'
import DataTable from 'primevue/datatable'
import DatePicker from 'primevue/datepicker'
import Dialog from 'primevue/dialog'
import InputNumber from 'primevue/inputnumber'
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { usePricingStore } from '../stores/pricing'

// Diesel price index surface (Phase 25i, BR-P18) — a flat index/list, not a
// draft/publish lifecycle like FeeScale/RateSheet/FixedRate: IndexDieselPrice
// upserts a dated row directly (no versioning), so this panel is a plain
// register form + table, sibling to RateSheetPanel's "Apply Diesel Overlay"
// control which consumes this index to append an overlay on a named sheet.
const store = usePricingStore()
const { t } = useI18n()

function toCents(dollars) {
  return Math.round((dollars ?? 0) * 100)
}
function fromCents(cents) {
  return (cents ?? 0) / 100
}
function formatDate(isoString) {
  return isoString ? isoString.slice(0, 10) : ''
}
// DatePicker gives back a Date at LOCAL midnight for the picked calendar
// day — plain .toISOString() converts through UTC and shifts that day
// backward in any positive-UTC-offset timezone (e.g. Africa/Johannesburg,
// UTC+2: local midnight Aug 15 becomes Aug 14T22:00Z). Re-anchor at UTC
// midnight for the same Y/M/D instead, so the calendar day the user
// clicked is what activeDate carries.
function dateOnlyISOString(date) {
  return new Date(Date.UTC(date.getFullYear(), date.getMonth(), date.getDate())).toISOString()
}

const registerOpen = ref(false)
const registerForm = reactive({ activeDate: null, coastalDollars: null, inlandDollars: null })
const registerError = ref('')
const registering = ref(false)

function openRegister() {
  registerForm.activeDate = null
  registerForm.coastalDollars = null
  registerForm.inlandDollars = null
  registerError.value = ''
  registerOpen.value = true
}

async function submitRegister() {
  if (!registerForm.activeDate || registerForm.coastalDollars === null) return
  registering.value = true
  registerError.value = ''
  try {
    await store.indexDieselPrice({
      activeDate: dateOnlyISOString(registerForm.activeDate),
      coastalCents: toCents(registerForm.coastalDollars),
      inlandCents: toCents(registerForm.inlandDollars),
    })
    registerOpen.value = false
  } catch (err) {
    registerError.value = err.message
  } finally {
    registering.value = false
  }
}
</script>

<template>
  <div class="pricing-group">
    <div class="group-head">
      <h4>{{ t('pricing.dieselPrices') }}</h4>
      <Button icon="pi pi-plus" :aria-label="t('pricing.indexDieselPrice')" text rounded size="small" @click="openRegister" />
    </div>
    <DataTable :value="store.dieselPrices" size="small" data-key="activeDate" resizableColumns columnResizeMode="expand">
      <template #empty>
        <span class="lab-muted">{{ t('pricing.dieselPricesEmpty') }}</span>
      </template>
      <Column :header="t('pricing.activeDate')" style="width:140px">
        <template #body="{ data }">{{ formatDate(data.activeDate) }}</template>
      </Column>
      <Column :header="t('pricing.coastalPrice')"><template #body="{ data }">${{ fromCents(data.coastalCents).toFixed(2) }}</template></Column>
      <Column :header="t('pricing.inlandPrice')"><template #body="{ data }">${{ fromCents(data.inlandCents).toFixed(2) }}</template></Column>
    </DataTable>
  </div>

  <Dialog v-model:visible="registerOpen" :header="t('pricing.indexDieselPrice')" modal style="width:24rem">
    <div class="form-field">
      <DatePicker v-model="registerForm.activeDate" :placeholder="t('pricing.activeDate')" date-format="yy-mm-dd" show-icon size="small" style="width:100%" />
    </div>
    <div class="form-field">
      <InputNumber v-model="registerForm.coastalDollars" :placeholder="t('pricing.coastalPrice')" mode="decimal" :min-fraction-digits="2" :max-fraction-digits="2" size="small" style="width:100%" />
    </div>
    <div class="form-field">
      <InputNumber v-model="registerForm.inlandDollars" :placeholder="t('pricing.inlandPrice')" mode="decimal" :min-fraction-digits="2" :max-fraction-digits="2" size="small" style="width:100%" />
    </div>
    <div v-if="registerError" class="domain-error dialog-note">{{ registerError }}</div>
    <template #footer>
      <Button :label="t('action.cancel')" text size="small" @click="registerOpen = false" />
      <Button :label="t('action.register')" size="small" :disabled="!registerForm.activeDate || registerForm.coastalDollars === null" :loading="registering" @click="submitRegister" />
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
</style>
