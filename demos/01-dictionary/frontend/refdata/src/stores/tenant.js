// Organizations' tenant + context selector (Phase 36.2). Deliberately its
// own store, separate from useDictionaryStore's `context` (that one is
// platform-wide — every tenant's contexts at once, via the PLATFORM
// connection, for browsing dictionary standards) — this store scopes to one
// tenant's own contexts at a time, because organizations-service derives
// tenant identity from *which NATS account the connection authenticated as*,
// not from a request parameter. Mirrors frontend/admin's stores/tenant.js in
// spirit, not in mechanism: no POST /api/tenant/switch call here — see
// nats/useTenantConnection.js's doc comment for why.
import { defineStore } from 'pinia'

import { listAvailableTenants, listContextsForTenant } from '../api'
import { useTenantConnection } from '../nats/useTenantConnection.js'

export const useTenantStore = defineStore('tenant', {
  state: () => ({
    tenant: '',
    available: [], // tenant names, active accounts only
    context: '',
    availableContexts: [], // {context, name} — this tenant's own, once selected
    switching: false,
  }),

  actions: {
    // Populates the tenant Select. Called lazily, the first time Trading
    // Partners is opened — not at app boot, since nothing else in Tech Lab
    // Operator needs a tenant identity.
    async refresh() {
      try {
        this.available = await listAvailableTenants()
      } catch {
        this.available = []
      }
      if (this.available.length > 0 && !this.available.includes(this.tenant)) {
        await this.setTenant(this.available[0])
      }
    },

    async setTenant(tenant) {
      if (tenant === this.tenant || this.switching) return
      this.switching = true
      try {
        await useTenantConnection().switchTenant(tenant)
        this.tenant = tenant
        await this.loadContexts()
      } finally {
        this.switching = false
      }
    },

    async loadContexts() {
      try {
        const contexts = await listContextsForTenant(this.tenant)
        const options = contexts.map((c) => ({ context: c.context, name: c.name || c.context }))
        this.availableContexts = options
        this.context = options.length > 0 ? options[0].context : ''
      } catch {
        this.availableContexts = []
        this.context = ''
      }
    },

    setContext(context) {
      this.context = context
    },
  },
})
