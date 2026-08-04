// Pinia store = the browser-side projected read model. It is deliberately
// the same idea as the server-side projections: state derived from an event
// stream (here: KV watch → SSE), one layer further out. See CLAUDE.md.
import { defineStore } from 'pinia'

import { getPorts, getRefdataContexts, watchUrl } from '../api'

export const useDictionaryStore = defineStore('dictionary', {
  state: () => ({
    context: '',
    availableContexts: [], // Phase 22: populated by loadContexts(); empty until fetched
    // key (ship.{shipID}) → { state: ShipState, revision } per shape
    shapeA: {},
    shapeB: {},
    // rolling log of raw watch events, newest first
    events: [],
    connected: false,
    _source: null,
    // ports seen across all events so the shipping form can auto-populate
    seenPorts: [],
  }),

  getters: {
    // Each row spreads all ShipState fields so ShapePanel columns can reference
    // data.shipName, data.currentPort, data.cargo etc. directly.
    shapeARows: (state) =>
      Object.entries(state.shapeA).map(([key, v]) => ({ key, revision: v.revision, ...v.state })),
    shapeBRows: (state) =>
      Object.entries(state.shapeB).map(([key, v]) => ({ key, revision: v.revision, ...v.state })),
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

    connect() {
      this.disconnect()
      this.shapeA = {}
      this.shapeB = {}
      this.events = []
      this.seenPorts = []

      // Seed the port list from the Postgres-backed ports registry
      // (BR-017/BR-018) so the dropdown reflects real, arrival-eligible
      // ports; live ship-arrival events keep merging below.
      getPorts(this.context)
        .then((res) => this.mergePorts(res?.values ?? []))
        .catch(() => {})

      const source = new EventSource(watchUrl(this.context))
      this._source = source
      source.onopen = () => { this.connected = true }
      source.onerror = () => { this.connected = false }
      source.onmessage = (msg) => { this.applyWatchEvent(JSON.parse(msg.data)) }
    },

    disconnect() {
      if (this._source) {
        this._source.close()
        this._source = null
      }
      this.connected = false
    },

    mergePorts(ports) {
      const merged = new Set([...this.seenPorts, ...ports])
      this.seenPorts = [...merged].sort()
    },

    applyWatchEvent(event) {
      const target = event.shape === 'A' ? this.shapeA : this.shapeB
      if (event.op === 'PUT') {
        target[event.key] = { state: event.value, revision: event.revision }
        const port = event.value?.currentPort
        if (port) this.mergePorts([port])
      } else {
        // Delete/purge: Shape A ships disappear; Shape B keys reappear on
        // the next read via the Postgres fallthrough + backfill.
        delete target[event.key]
      }
      this.events.unshift({ ...event, at: new Date().toLocaleTimeString() })
      if (this.events.length > 50) this.events.pop()
    },
  },
})
