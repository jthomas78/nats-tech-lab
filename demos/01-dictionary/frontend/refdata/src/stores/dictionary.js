// Pinia store for the Dictionary admin UI. Types/items are read fresh over
// api.* on selection (this is plain Postgres CRUD, not an event-sourced read
// model — see obsidian/V3-Platform/Architecture/Dictionary-POC/ARCHITECTURE.md's "Event Sourcing vs Plain
// CRUD" heuristic), while the notify._platform.refdata.> subscription
// (Phase 32 — replaces the old SSE watch) only drives the cache-status
// widget's "something changed, refetch" signal.
import { defineStore } from 'pinia'
import { watch } from 'vue'

import { addLocale, getCacheStatus, getItem, listContexts, listItems, listLocales, listTypes } from '../api'
import { useRefdataAdminConnection } from '../nats/useRefdataAdminConnection.js'

// notify._platform.refdata.> is the same PLATFORM-wide bridge the Admin UI
// has subscribed to since Phase 23 (shipping-service's RegisterRefdataNotify,
// bridging refdata-service's evt.{context}.refdata.{typeKey}.changed feed) —
// not the tenant-scoped notify.{context}.refdata.{typeKey}.changed bridge
// BR-D42 added for Sea Freight Flow-style tenant apps (internal/notifybridge),
// which publishes only inside each tenant's own account and is therefore
// unreachable from this app's PLATFORM connection. "_platform" here is a
// fixed namespace marker, not the mutated context — the real context/typeKey
// tokens follow it (notify._platform.refdata.{context}.{typeKey}.changed),
// exactly mirroring internal/natsrpc's own ContextListSubject convention.
const NOTIFY_SUBJECT = 'notify._platform.refdata.>'

function parseNotifySubject(subject) {
  const parts = subject.split('.')
  if (parts.length !== 6 || parts[0] !== 'notify' || parts[1] !== '_platform' || parts[2] !== 'refdata' || parts[5] !== 'changed') {
    return null
  }
  return { context: parts[3], typeKey: parts[4] }
}

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
    lastCacheEvent: null, // { context, typeKey, at } — bumped on every notify.* message
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
    _unsubscribe: null,
    _unwatchConnected: null,
  }),

  actions: {
    // Phase 22b: availableContexts keeps both fields — {context, name} — so
    // the Select in App.vue can show the human-readable name (e.g. "Pacific
    // Fleet") while still submitting the immutable {context} slug everywhere
    // a context travels as a subject/KV-key token.
    async loadContexts() {
      try {
        const { contexts } = await listContexts()
        const options = (contexts ?? []).map((c) => ({ context: c.context, name: c.name || c.context }))
        this.availableContexts = options
        if (!options.some((c) => c.context === this.context) && options.length > 0) {
          this.context = options[0].context
        }
      } catch {
        this.availableContexts = []
      }
    },

    // connect() is the data-refresh cycle — called once at App.vue mount and
    // again on every context switch. It assumes the NATS transport itself is
    // already up (App.vue's onMounted owns that connect/disconnect, mirroring
    // frontend/admin's App.vue — never reconnecting the WebSocket just
    // because the context Select changed).
    async connect() {
      this.disconnect()

      const nats = useRefdataAdminConnection()
      // Mirror connected onto this store's own state so App.vue's existing
      // store.connected binding keeps working unchanged.
      this._unwatchConnected = watch(nats.connected, (value) => { this.connected = value }, { immediate: true })

      await this.loadContexts()
      await Promise.all([this.refreshTypes(), this.refreshLocales()])
      const target = this.selectedType || (this.types.length > 0 ? this.types[0].typeKey : null)
      if (target) {
        await this.selectType(target)
      }

      // Phase 32: replaces the SSE watch — see NOTIFY_SUBJECT's doc comment.
      this._unsubscribe = nats.subscribe(NOTIFY_SUBJECT, (_payload, subject) => {
        const parsed = parseNotifySubject(subject)
        if (!parsed) return
        this.lastCacheEvent = { context: parsed.context, typeKey: parsed.typeKey, at: Date.now() }
        if (parsed.context === this.context && parsed.typeKey === this.selectedType) {
          this.refreshCacheStatus()
        }
      })
    },

    disconnect() {
      this._unsubscribe?.()
      this._unsubscribe = null
      this._unwatchConnected?.()
      this._unwatchConnected = null
      this.connected = false
    },

    async refreshTypes() {
      const res = await listTypes()
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
