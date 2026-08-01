// Tenant selector (Phase 13b, rebuilt on NATS WebSocket transport in Phase
// 15d). Deliberately its own store, not folded into usePortStore's fleet
// `context` (CONTEXTS = acme/acme-atlantic-fleet/acme-pacific-fleet, Phase
// 16e — see stores/port.js) — a tenant switch re-authenticates the browser's NATS
// WebSocket connection into a different account entirely (auth-service's
// GET /api/auth/connectInfo, Phase 15c), so every ship/container endpoint's
// data changes, not just what one query filters by fleet.
//
// Unlike the pre-Phase-15 version, there is no server-side "active tenant"
// for the browser to read (that concept — rest/tenant.go's SwitchTenant —
// still exists, but only governs the Admin/Dictionary REST+SSE frontends,
// which stay out of scope for this phase). The browser picks its own
// tenant and authenticates directly into that NATS account.
import { defineStore } from 'pinia'

import { useNatsConnection } from '../nats/useNatsConnection'
import { usePortStore } from './port'

// Mirrors shipping-service composition.go's initialTenant — the tenant a
// fresh browser session connects to before anyone has picked one
// explicitly, if it's available.
const DEFAULT_TENANT = 'acme'

async function fetchAvailableTenants() {
  const res = await fetch('/api/auth/tenants')
  const body = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`)
  return body.tenants ?? []
}

export const useTenantStore = defineStore('tenant', {
  state: () => ({
    tenant: null,
    available: [],
    switching: false,
  }),

  actions: {
    async loadAvailable() {
      this.available = await fetchAvailableTenants()
    },

    // init() authenticates the browser's NATS connection for the first
    // time this session — call once, before usePortStore().connect().
    async init() {
      await this.loadAvailable()
      const initial = this.available.includes(DEFAULT_TENANT) ? DEFAULT_TENANT : this.available[0]
      if (!initial) throw new Error('no tenants available')
      await useNatsConnection().connect(initial)
      this.tenant = initial
      // Phase 16f: the fleet-context dropdown is tenant-scoped, so refresh
      // it here rather than in usePortStore().connect() (which also runs on
      // a plain fleet-context change, where refetching the same list would
      // be pure waste).
      await usePortStore().loadContexts()
    },

    async setTenant(tenant) {
      if (tenant === this.tenant || this.switching) return
      this.switching = true
      try {
        await useNatsConnection().switchTenant(tenant)
        this.tenant = tenant
        await usePortStore().loadContexts()
        // The active tenant is a different NATS account with its own
        // SHIPPING stream and KV buckets — the port store's api.*/notify.*
        // subscriptions reconnect against it, exactly as a fleet-context
        // switch does.
        await usePortStore().connect()
      } finally {
        this.switching = false
      }
    },
  },
})
