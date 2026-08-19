// Establishes the browser's tenant NATS connection on startup so components
// that depend on it (refdata labels/UI copy via setRefdataTransport) are
// ready before any data loads. No longer drives a topbar tenant selector —
// tenant switching was removed in Phase 36.2 (the selector gave a false
// impression of data filtering when all panels already show every account).
import { defineStore } from 'pinia'

import { getTenant } from '../api'
import { useNatsConnection } from '../nats/useNatsConnection.js'

export const useTenantStore = defineStore('tenant', {
  state: () => ({
    tenant: null,
  }),

  actions: {
    async refresh() {
      const res = await getTenant()
      this.tenant = res?.tenant ?? null
      if (this.tenant) {
        try {
          await useNatsConnection().switchTenant(this.tenant)
        } catch {
          // useNatsConnection's own lastError surfaces this
        }
      }
    },
  },
})
