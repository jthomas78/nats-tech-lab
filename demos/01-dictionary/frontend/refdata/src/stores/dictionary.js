// Pinia store for the Dictionary admin UI. Types/items are read fresh from
// the REST API on selection (this is plain Postgres CRUD, not an
// event-sourced read model — see obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md's "Event Sourcing vs Plain
// CRUD" heuristic), while the SSE watch on refdata-{context} only drives the
// cache-status widget's "something changed, refetch" signal.
import { defineStore } from 'pinia'

import { addLocale, getCacheStatus, getItem, listContexts, listItems, listLocales, listTypes, watchUrl } from '../api'

export const useDictionaryStore = defineStore('dictionary', {
  state: () => ({
    context: '_platform', // stays at _platform until loadContexts() resolves
    availableContexts: [], // Phase 22: populated by loadContexts() at connect()

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
    // Single source of truth for the selected type's Postgres/KV sync state —
    // read by both the compact header chip and the full Cache Status panel,
    // so there's exactly one getCacheStatus call per type change, not two.
    cacheStatus: null,
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

  getters: {
    // BR-D31: a domain-enum type's KV keys — items and _meta alike — live
    // under the enum. namespace, so the SSE watch has to match the namespaced
    // key for those types. Derived from the type's category rather than
    // hardcoded per type, mirroring the backend's domain.KeyNamespace().
    selectedTypeMetaKey(state) {
      if (!state.selectedType) return ''
      const type = state.types.find((t) => t.typeKey === state.selectedType)
      const namespace = type?.category === 'domain-enum' ? 'enum.' : ''
      return `${namespace}${state.selectedType}._meta`
    },
  },

  actions: {
    async loadContexts() {
      try {
        const ctxs = await listContexts()
        this.availableContexts = ctxs
        if (!this.availableContexts.includes(this.context) && ctxs.length > 0) {
          this.context = ctxs[0]
        }
      } catch {
        this.availableContexts = []
      }
    },

    async connect() {
      this.disconnect()
      await this.loadContexts()
      await Promise.all([this.refreshTypes(), this.refreshLocales()])
      const target = this.selectedType || (this.types.length > 0 ? this.types[0].typeKey : null)
      if (target) {
        await this.selectType(target)
      }

      const source = new EventSource(watchUrl(this.context))
      this._source = source
      source.onopen = () => { this.connected = true }
      source.onerror = () => { this.connected = false }
      source.onmessage = (msg) => {
        try {
          const event = JSON.parse(msg.data)
          this.lastCacheEvent = { key: event.key, revision: event.revision, at: Date.now() }
          if (event.key === this.selectedTypeMetaKey) this.refreshCacheStatus()
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

    // Fetches the new type's items BEFORE touching selectedType/selectedCode,
    // then commits them together in one synchronous batch. ItemDetailPanel
    // watches [selectedCode, selectedType, selectedLocale]; clearing
    // selectedCode first (the old behavior) made that watcher fire on a
    // momentary selectedType/selectedCode='' mismatch mid-fetch, blanking the
    // detail panel to "Select an item…" and back — a visible flicker on
    // every type switch.
    async selectType(typeKey) {
      const items = await this._fetchItemsForType(typeKey)
      this.activeView = 'items'
      this.selectedType = typeKey
      this.items = items
      this.selectedCode = this._firstCode(items)
      await this.refreshCacheStatus()
    },

    selectItem(code) {
      this.selectedCode = code
    },

    showLocalizationView() {
      this.activeView = 'localization'
    },

    showVersioningView() {
      this.activeView = 'versioning'
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
    // Same fetch-then-batch-commit ordering as selectType, for the same
    // anti-flicker reason.
    async selectCategoryType(typeKey) {
      const items = await this._fetchItemsForType(typeKey)
      this.selectedType = typeKey
      this.items = items
      this.selectedCode = this._firstCode(items)
      await this.refreshCacheStatus()
    },

    async refreshCacheStatus() {
      if (!this.selectedType) {
        this.cacheStatus = null
        return
      }
      this.cacheStatus = await getCacheStatus(this.context, this.selectedType)
    },

    async _fetchItemsForType(typeKey) {
      const res = await listItems(this.context, typeKey, {
        all: this.showDeprecated,
        locale: this.selectedLocale,
      })
      return res?.items ?? []
    },

    _firstCode(items) {
      return items[0]?.code || items[0]?.item?.code || ''
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
