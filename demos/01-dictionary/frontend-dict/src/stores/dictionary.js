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
    // Master-detail selection (Phase 11.9): the code whose detail panel is
    // shown next to the item list. Kept valid by refreshItems.
    selectedCode: '',
    showDeprecated: false,
    locales: [],
    defaultLocale: '', // the context's fallback locale (BR-D03); '' if none set
    selectedLocale: 'en', // BR-D13: default to en rather than raw codes ('')
    connected: false,
    lastCacheEvent: null, // { key, revision, at } — bumped on every SSE message
    // 'items' — the type navigator + item grid (default); 'localization' —
    // the promoted locale-admin + types×locales completeness matrix view
    // (Phase 11.7); 'domain-category' — a category-level list of types
    // (e.g. all Enums), so the tree stays flat and a category can hold more
    // than one type without redesigning the nav. Not a route — this app has
    // no router, just a mode flag.
    activeView: 'items',
    selectedCategory: '', // set when activeView === 'domain-category'
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
      this.defaultLocale = res?.defaultLocale ?? ''
    },

    async selectType(typeKey) {
      this.activeView = 'items'
      this.selectedType = typeKey
      this.selectedCode = ''
      await this.refreshItems()
    },

    selectItem(code) {
      this.selectedCode = code
    },

    showLocalizationView() {
      this.activeView = 'localization'
    },

    async showCategoryView(categoryKey) {
      this.activeView = 'domain-category'
      this.selectedCategory = categoryKey
      // Auto-select the first type in the category so the values pane isn't
      // empty on entry. Types are already loaded (refreshTypes on connect).
      const first = this.types
        .filter((t) => (t.category || 'standards') === categoryKey)
        .sort((a, b) => a.typeKey.localeCompare(b.typeKey))[0]
      if (first) {
        await this.selectCategoryType(first.typeKey)
      } else {
        this.selectedType = ''
        this.items = []
      }
    },

    // Select a type from within the category view. Unlike selectType this does
    // NOT flip activeView back to 'items' — the type's entries render in the
    // category view's own values pane, so we stay in the master-detail.
    async selectCategoryType(typeKey) {
      this.selectedType = typeKey
      this.selectedCode = ''
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
      // Keep the detail panel pointing at something real: drop a selection
      // that fell out of the list (deleted / filtered), default to the first
      // item otherwise.
      const codes = this.items.map((i) => i.code || i.item?.code)
      if (!codes.includes(this.selectedCode)) {
        this.selectedCode = codes[0] ?? ''
      }
    },

    async fetchItemDetail(code) {
      return getItem(this.context, this.selectedType, code, { locale: this.selectedLocale })
    },

    async addLocaleToContext(locale, isDefault) {
      await addLocale(this.context, locale, isDefault)
      await this.refreshLocales()
    },

    // Registering an existing locale with isDefault=true is the API's
    // set-default operation — the backend clears the old default atomically.
    async setDefaultLocale(locale) {
      await addLocale(this.context, locale, true)
      await this.refreshLocales()
    },
  },
})
