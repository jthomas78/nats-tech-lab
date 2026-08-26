// Pinia store = the browser-side projected read model derived from a
// one-shot bootstrap fetch of the tenant's KV bucket at connect time.
// Live NATS subscriptions were removed in Phase 36.2 (tenant selector
// dropped; see CLAUDE.md). See CLAUDE.md for the server-side projection analogy.
import { defineStore } from 'pinia'

import { getKvBucketEntries, getRefdataContexts } from '../api'
import { useTenantStore } from './tenant.js'

export const useDictionaryStore = defineStore('dictionary', {
  state: () => ({
    context: '',
    availableContexts: [], // Phase 22: populated by loadContexts(); empty until fetched
    // key (ship.{shipID}) → { state: ShipState, revision }
    ships: {},
    // rolling log of raw watch events, newest first
    events: [],
  }),

  getters: {
    // Each row spreads all ShipState fields so consumers (OverviewPanel's KV
    // rev card, TelemetryStrip) can reference data.shipName, data.currentPort,
    // data.cargo etc. directly.
    shipRows: (state) =>
      Object.entries(state.ships).map(([key, v]) => ({ key, revision: v.revision, ...v.state })),
  },

  actions: {
    setContext(context) {
      if (context === this.context) return
      this.context = context
      this.connect()
    },

    // Fetches the backend active account's context list (Phase 16f/22).
    // Filters "_"-reserved contexts (e.g.
    // "_platform", "_default_bu") — those are platform roots, not fleet scopes.
    // A failed fetch leaves availableContexts empty (no stale fallback — Phase 22).
    async loadContexts() {
      try {
        const contexts = (await getRefdataContexts(useTenantStore().tenant)).filter((c) => !c.startsWith('_'))
        this.availableContexts = contexts
        if (contexts.length > 0 && !contexts.includes(this.context)) {
          this.setContext(contexts[0])
        }
      } catch {
        this.availableContexts = []
      }
    },

    // One-shot bootstrap fetch of the tenant's ships KV bucket. No live NATS
    // subscription — the tenant selector was removed in Phase 36.2.
    async connect() {
      if (!this.context) return
      this.ships = {}
      this.events = []
      const account = useTenantStore().tenant
      if (!account) return

      try {
        const rows = await getKvBucketEntries(account, 'ships')
        const prefix = this.context + '.'
        for (const row of rows ?? []) {
          if (!row.key.startsWith(prefix)) continue
          this.applyWatchEvent({
            key: row.key.slice(prefix.length), op: 'PUT', revision: row.revision, value: row.value,
          })
        }
      } catch {
        // best-effort snapshot
      }
    },

    disconnect() {},

    applyWatchEvent(event) {
      if (event.op === 'PUT') {
        this.ships[event.key] = { state: event.value, revision: event.revision }
      } else {
        // Delete/purge: an evicted cache key reappears on the next read via
        // the Postgres fallthrough + backfill.
        delete this.ships[event.key]
      }
      this.events.unshift({ ...event, at: new Date().toLocaleTimeString() })
      if (this.events.length > 50) this.events.pop()
    },
  },
})
