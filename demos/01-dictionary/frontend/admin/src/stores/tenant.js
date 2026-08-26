// Reads the backend's active account label for the legacy Overview snapshot.
// This store does not open a browser NATS connection. Tenant switching was
// removed in Phase 36.2 because the selector implied that all Admin panels
// were filtered when the diagnostics panels already showed every account.
import { defineStore } from 'pinia'

import { getTenant } from '../api'

export const useTenantStore = defineStore('tenant', {
  state: () => ({
    tenant: null,
  }),

  actions: {
    async refresh() {
      const res = await getTenant()
      this.tenant = res?.tenant ?? null
    },
  },
})
