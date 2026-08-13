<script setup>
import { useI18n } from 'vue-i18n'

import DieselPricePanel from './DieselPricePanel.vue'
import FeeScalePanel from './FeeScalePanel.vue'
import FixedRatePanel from './FixedRatePanel.vue'
import RateSheetPanel from './RateSheetPanel.vue'
import { usePricingStore } from '../stores/pricing'

// Outer shell for the "Pricing" tab — one BR-029-style loading gate over
// the store's bootstrap (Phase 25g), delegating each aggregate's table +
// manual-entry UX (register/draft/add-range-or-entry/publish/rollback,
// Phase 25h) to its own panel, the same split App.vue's Port Management
// view uses for TerminalPanel + ShipsAtPortPanel.
const store = usePricingStore()
const { t } = useI18n()
</script>

<template>
  <section class="lab-panel">
    <h3>{{ t('pricing.title') }}</h3>

    <p v-if="store.loading" class="lab-muted loading-line">
      <span class="spinner" aria-hidden="true" />
      {{ t('pricing.loading') }}
    </p>
    <template v-else>
      <FeeScalePanel />
      <RateSheetPanel />
      <FixedRatePanel />
      <DieselPricePanel />
    </template>
  </section>
</template>

<style scoped>
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
