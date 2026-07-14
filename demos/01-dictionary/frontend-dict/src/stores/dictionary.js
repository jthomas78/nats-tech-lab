// Pinia store for the Dictionary admin UI. Types/items are read fresh from
// the REST API on selection (this is plain Postgres CRUD, not an
// event-sourced read model — see ARCHITECTURE.md's "Event Sourcing vs Plain
// CRUD" heuristic), while the SSE watch on refdata-{context} only drives the
// cache-status widget's "something changed, refetch" signal.
import { defineStore } from 'pinia'

import { addLocale, getItem, listItems, listLocales, listTypes, watchUrl } from '../api'

export const CONTEXTS = ['emea-acme']

export const useDictionaryStore = defineStore('dictionary', {
  state: () => ({
    context: CONTEXTS[0],
    types: [],
    typeCounts: {},
    selectedType: '',
    items: [],
    showDeprecated: false,
    locales: [],
    selectedLocale: '',
    connected: false,
    lastCacheEvent: null, // { key, revision, at } — bumped on every SSE message
    _source: null,
  }),

  actions: {
    async connect() {
      this.disconnect()
      await Promise.all([this.refreshTypes(), this.refreshLocales()])
      if (!this.selectedType && this.types.length > 0) {
        await this.selectType(this.types[0].typeKey)
      }

      const source = new EventSource(watchUrl(this.context))
      this._source = source
      source.onopen = () => { this.connected = true }
      source.onerror = () => { this.connected = false }
      source.onmessage = (msg) => {
        try {
          const event = JSON.parse(msg.data)
          this.lastCacheEvent = { key: event.key, revision: event.revision, at: Date.now() }
        } catch {
          // ignore malformed/heartbeat frames
        }
      }
    },

    disconnect() {
      this._source?.close()
      this._source = null
      this.connected = false
    },

    async refreshTypes() {
      const res = await listTypes(this.context)
      this.types = res?.types ?? []
      await this.refreshTypeCounts()
    },

    async refreshTypeCounts() {
      const counts = {}
      await Promise.all(
        this.types.map(async (t) => {
          const res = await listItems(this.context, t.typeKey, { all: true })
          counts[t.typeKey] = res?.items?.length ?? 0
        }),
      )
      this.typeCounts = counts
    },

    async refreshLocales() {
      const res = await listLocales(this.context)
      this.locales = res?.locales ?? []
    },

    async selectType(typeKey) {
      this.selectedType = typeKey
      await this.refreshItems()
    },

    async refreshItems() {
      if (!this.selectedType) {
        this.items = []
        return
      }
      const res = await listItems(this.context, this.selectedType, {
        all: this.showDeprecated,
        locale: this.selectedLocale,
      })
      this.items = res?.items ?? []
    },

    async fetchItemDetail(code) {
      return getItem(this.context, this.selectedType, code, { locale: this.selectedLocale })
    },

    async addLocaleToContext(locale, isDefault) {
      await addLocale(this.context, locale, isDefault)
      await this.refreshLocales()
    },
  },
})
