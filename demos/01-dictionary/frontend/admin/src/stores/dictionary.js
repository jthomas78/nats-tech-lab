// Pinia store = the browser-side projected read model. It is deliberately
// the same idea as the server-side projections: state derived from an event
// stream (here: KV watch → SSE), one layer further out. See CLAUDE.md.
import { defineStore } from 'pinia'

import { getPorts, getRefdataContexts, watchUrl } from '../api'

// Contexts scope the KV keys: each is stored as {context}.ship.{id} in the
// tenant-scoped dict-a and dict-b buckets. A context is the company /
// business-unit scope — NOT the tenant (that's the NATS account) and NOT the
// region (a separate regional deployment); see ARCHITECTURE-COMMUNICATIONS.md
// § 2.3. The values below are
// now only the offline/error fallback (Phase 16f) — the real, tenant-scoped
// list is fetched via loadContexts()/GET /api/refdata/contexts, kept as a
// literal (not deleted) so the dropdown still shows something sensible if
// that fetch never succeeds.
export const CONTEXTS = ['acme-atlantic-fleet', 'acme-pacific-fleet']

export const useDictionaryStore = defineStore('dictionary', {
  state: () => ({
    context: CONTEXTS[0],
    availableContexts: [...CONTEXTS], // Phase 16f: replaced by loadContexts() once the tenant is known
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

    // Fetches this tenant's real context list (Phase 16f), replacing the
    // static CONTEXTS fallback. Called from stores/tenant.js on tenant
    // switch — not from connect() below, so a plain fleet-context change
    // doesn't needlessly refetch the very list it's picking from. Falls
    // back to the existing (initially CONTEXTS) list on error — this is a
    // convenience list for a dropdown, not a required resource.
    //
    // Filters out "_"-reserved contexts (e.g. "_platform"): the fetched list
    // is refdata-service's context tree, which includes the shared platform
    // root every tenant inherits standards from — meaningful for reference-
    // data reads, but no ship or container ever belongs to it. Offering it
    // as a fleet-context choice here would let a click create real (if empty)
    // projection keys for a context with no shipping domain meaning.
    async loadContexts() {
      try {
        const contexts = (await getRefdataContexts()).filter((c) => !c.startsWith('_'))
        if (contexts.length > 0) {
          this.availableContexts = contexts
          if (!this.availableContexts.includes(this.context)) {
            this.context = this.availableContexts[0]
          }
        }
      } catch {
        // keep whatever availableContexts already held (CONTEXTS on first load)
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
