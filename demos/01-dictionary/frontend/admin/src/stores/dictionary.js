// Pinia store = the browser-side projected read model. It is deliberately
// the same idea as the server-side projections: state derived from an event
// stream (here, Phase 23: KV bootstrap fetch + notify.* subscribe on the
// tenant NATS connection — previously KV watch → SSE), one layer further
// out. See CLAUDE.md.
import { defineStore } from 'pinia'

import { getKvBucketEntries, getPorts, getRefdataContexts } from '../api'
import { useNatsConnection } from '../nats/useNatsConnection.js'
import { parseKvNotifySubject } from '../nats/kvNotifySubject.js'

export const useDictionaryStore = defineStore('dictionary', {
  state: () => ({
    context: '',
    availableContexts: [], // Phase 22: populated by loadContexts(); empty until fetched
    // key (ship.{shipID}) → { state: ShipState, revision }
    ships: {},
    // rolling log of raw watch events, newest first
    events: [],
    connected: false,
    _unsubscribe: null,
    // ports seen across all events so the shipping form can auto-populate
    seenPorts: [],
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

    // Fetches this tenant's real context list (Phase 16f/22). Called from
    // stores/tenant.js on tenant switch. Filters "_"-reserved contexts (e.g.
    // "_platform", "_default_bu") — those are platform roots, not fleet scopes.
    // A failed fetch leaves availableContexts empty (no stale fallback — Phase 22).
    async loadContexts() {
      try {
        const contexts = (await getRefdataContexts()).filter((c) => !c.startsWith('_'))
        this.availableContexts = contexts
        if (contexts.length > 0 && !contexts.includes(this.context)) {
          this.setContext(contexts[0])
        }
      } catch {
        this.availableContexts = []
      }
    },

    // Phase 23: replaces the /api/watch/{context} EventSource with a
    // one-shot bootstrap fetch (getKvBucketEntries against the ships bucket)
    // plus a notify.{context}.kv.ships.> subscribe on the tenant NATS
    // connection. Bootstrap entries come back with the {context}. key prefix
    // still attached (kv.go's kvBucketEntriesOnce reads the raw bucket,
    // unlike the old SSE handler's kvstore.Store.Watch, which stripped it) —
    // filtered and stripped here so ships keeps the same bare-key shape
    // (e.g. "ship.SHIP1") shipRows' consumers already expect.
    async connect() {
      this.disconnect()
      // No context means no valid subject to subscribe on — App.vue calls
      // this unconditionally after loadContexts(), which leaves this.context
      // at its initial '' when getRefdataContexts() fails (e.g. refdata-service
      // not yet ready). Subscribing anyway produced a malformed
      // notify..kv.{bucket}.> subject (empty {context} token) that the NATS
      // server correctly rejected as a Subscription Violation — a real
      // failure mode this Log panel first surfaced, not just log noise.
      if (!this.context) return
      this.ships = {}
      this.events = []
      this.seenPorts = []

      const { connected: tenantConnected, subscribe, tenant } = useNatsConnection()

      // Seed the port list from the Postgres-backed ports registry
      // (BR-017/BR-018) so the dropdown reflects real, arrival-eligible
      // ports; live ship-arrival events keep merging below.
      getPorts(this.context)
        .then((res) => this.mergePorts(res?.values ?? []))
        .catch(() => {})

      // Subscribe before the bootstrap fetch, same ordering sse.go's own
      // watchRefdata/watchRPCObs use: a message published in the narrow gap
      // before the subscribe took effect could in principle be re-applied by
      // the bootstrap fetch below, which is harmless (applyWatchEvent is
      // idempotent per key) — the alternative order risks losing it entirely.
      const bucket = 'ships'
      const unsub = tenantConnected.value
        ? subscribe(`notify.${this.context}.kv.${bucket}.>`, (value, subject) => {
            const parsed = parseKvNotifySubject(subject)
            if (!parsed) return
            this.applyWatchEvent(
              value === null
                ? { key: parsed.key, op: 'DEL' }
                : { key: parsed.key, op: 'PUT', revision: undefined, value },
            )
          })
        : null
      this._unsubscribe = unsub
      this.connected = tenantConnected.value

      try {
        const rows = await getKvBucketEntries(tenant.value, bucket)
        const prefix = this.context + '.'
        for (const row of rows ?? []) {
          if (!row.key.startsWith(prefix)) continue
          this.applyWatchEvent({
            key: row.key.slice(prefix.length), op: 'PUT', revision: row.revision, value: row.value,
          })
        }
      } catch {
        // best-effort snapshot — live subscribe above still works even if this fails
      }
    },

    disconnect() {
      this._unsubscribe?.()
      this._unsubscribe = null
      this.connected = false
    },

    mergePorts(ports) {
      const merged = new Set([...this.seenPorts, ...ports])
      this.seenPorts = [...merged].sort()
    },

    applyWatchEvent(event) {
      if (event.op === 'PUT') {
        this.ships[event.key] = { state: event.value, revision: event.revision }
        const port = event.value?.currentPort
        if (port) this.mergePorts([port])
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
