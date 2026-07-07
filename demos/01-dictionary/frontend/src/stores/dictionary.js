// Pinia store = the browser-side projected read model. It is deliberately
// the same idea as the server-side projections: state derived from an event
// stream (here: KV watch → SSE), one layer further out. See CLAUDE.md.
import { defineStore } from 'pinia'

import { watchUrl } from '../api'

export const CONTEXTS = ['en-GB', 'en-US', 'de-DE']

export const useDictionaryStore = defineStore('dictionary', {
  state: () => ({
    context: CONTEXTS[0],
    // key ({entityType}.{id}) → { entry, revision } per shape
    shapeA: {},
    shapeB: {},
    // rolling log of raw watch events, newest first
    events: [],
    connected: false,
    _source: null,
  }),

  getters: {
    shapeARows: (state) =>
      Object.entries(state.shapeA).map(([key, v]) => ({ key, revision: v.revision, ...v.entry })),
    shapeBRows: (state) =>
      Object.entries(state.shapeB).map(([key, v]) => ({ key, revision: v.revision, ...v.entry })),
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

      const source = new EventSource(watchUrl(this.context))
      this._source = source
      source.onopen = () => {
        this.connected = true
      }
      source.onerror = () => {
        this.connected = false
      }
      source.onmessage = (msg) => {
        this.applyWatchEvent(JSON.parse(msg.data))
      }
    },

    disconnect() {
      if (this._source) {
        this._source.close()
        this._source = null
      }
      this.connected = false
    },

    applyWatchEvent(event) {
      const target = event.shape === 'A' ? this.shapeA : this.shapeB
      if (event.op === 'PUT') {
        target[event.key] = { entry: event.value, revision: event.revision }
      } else {
        // Delete/purge: Shape A entries disappear; Shape B keys reappear on
        // the next read via the Postgres fallthrough + backfill.
        delete target[event.key]
      }
      this.events.unshift({ ...event, at: new Date().toLocaleTimeString() })
      if (this.events.length > 50) this.events.pop()
    },
  },
})
