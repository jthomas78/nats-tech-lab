// Pinia store = the browser-side projected read model. It is deliberately
// the same idea as the server-side projections: state derived from an event
// stream (here: KV watch → SSE), one layer further out. See CLAUDE.md.
import { defineStore } from 'pinia'

import { getKnownPorts, watchUrl } from '../api'

// Fleet contexts scope the KV buckets, same as tenant/region in the dictionary
// domain. Each context maps to dict-a-{context} and dict-b-{context} buckets.
export const CONTEXTS = ['global', 'atlantic-fleet', 'pacific-fleet']

export const useDictionaryStore = defineStore('dictionary', {
  state: () => ({
    context: CONTEXTS[0],
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

    connect() {
      this.disconnect()
      this.shapeA = {}
      this.shapeB = {}
      this.events = []
      this.seenPorts = []

      // Seed the port list from the meta.known-ports KV projection so the
      // full port history survives reload; live events keep merging below.
      getKnownPorts(this.context)
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
