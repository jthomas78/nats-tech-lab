// Phase 18b's tenant selector. Deliberately its own store, not folded into
// useDictionaryStore's fleet `context` (CONTEXTS = global/atlantic-fleet/
// pacific-fleet, see stores/dictionary.js) — a tenant switch reconnects
// shipping-service's NATS connection under a different account entirely, so
// every ship/container endpoint's data changes, not just what one query
// filters by fleet. Backend: rest/tenant.go (Main-POC-Plan.md Phase 18b).
import { defineStore } from 'pinia'

import { getTenant, switchTenant } from '../api'
import { useDictionaryStore } from './dictionary'

export const useTenantStore = defineStore('tenant', {
  state: () => ({
    tenant: null,
    available: [],
    switching: false,
  }),

  actions: {
    async refresh() {
      const res = await getTenant()
      this.tenant = res?.tenant ?? null
      this.available = res?.available ?? []
    },

    async setTenant(tenant) {
      if (tenant === this.tenant || this.switching) return
      this.switching = true
      try {
        await switchTenant(tenant)
        await this.refresh()
        // The active tenant is a different NATS account with its own
        // SHIPPING stream and KV buckets — every ship/container watch
        // reconnects against it, exactly as a fleet-context switch does.
        useDictionaryStore().connect()
      } finally {
        this.switching = false
      }
    },
  },
})
