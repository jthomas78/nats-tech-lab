// Phase 13b's tenant selector. Deliberately its own store, not folded into
// useDictionaryStore's fleet `context` (CONTEXTS =
// acme/acme-atlantic-fleet/acme-pacific-fleet, Phase 16e — see
// stores/dictionary.js) — a tenant switch reconnects
// shipping-service's NATS connection under a different account entirely, so
// every ship/container endpoint's data changes, not just what one query
// filters by fleet. Backend: rest/tenant.go (Main-POC-Plan.md Phase 13b).
import { defineStore } from 'pinia'

import { getTenant, switchTenant } from '../api'
import { useDictionaryStore } from './dictionary'
import { useNatsConnection } from '../nats/useNatsConnection.js'

export const useTenantStore = defineStore('tenant', {
  state: () => ({
    tenant: null,
    available: [],
    switching: false,
  }),

  actions: {
    // Phase 23: also (re)establishes the browser's own tenant NATS
    // WebSocket connection (useNatsConnection.js) alongside the REST-level
    // tenant read — this is what makes the initial page load's dictionary
    // watch/KV inspector/JetStream raw watch subscriptions possible at all,
    // not just the topbar's Select options. Awaited (not fire-and-forget) so
    // a caller that awaits refresh() — App.vue's onMounted, setTenant below —
    // is guaranteed the NATS connect attempt has settled (success or
    // failure) before it goes on to call loadContexts()/dictionary connect(),
    // which need a live tenant connection to actually subscribe to anything.
    async refresh() {
      const res = await getTenant()
      this.tenant = res?.tenant ?? null
      this.available = res?.available ?? []
      if (this.tenant) {
        try {
          await useNatsConnection().switchTenant(this.tenant)
        } catch {
          // useNatsConnection's own lastError surfaces this; the REST-level
          // tenant read above still succeeded, so refresh() itself doesn't fail.
        }
      }
    },

    async setTenant(tenant) {
      if (tenant === this.tenant || this.switching) return
      this.switching = true
      try {
        await switchTenant(tenant)
        await this.refresh()
        // Phase 16f: the fleet-context dropdown is tenant-scoped, so refresh
        // it here rather than in useDictionaryStore().connect() (which also
        // runs on a plain fleet-context change, where refetching the same
        // list would be pure waste).
        await useDictionaryStore().loadContexts()
        // The active tenant is a different NATS account with its own
        // SHIPPING stream and KV buckets — every ship/container watch
        // reconnects against it, exactly as a fleet-context switch does.
        // refresh() above already re-pointed useNatsConnection() at the new
        // tenant before this runs, so the dictionary store's subscribe calls
        // attach to the fresh connection, not the old tenant's.
        useDictionaryStore().connect()
      } finally {
        this.switching = false
      }
    },
  },
})
