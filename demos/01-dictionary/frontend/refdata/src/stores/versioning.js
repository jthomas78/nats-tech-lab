// Pinia store for the Phase 12 versioning admin view: context hierarchy,
// corpus version lifecycle (draft/publish/rollback), and diff. Kept separate
// from the main dictionary store — this is a distinct admin surface with its
// own selection state, not another type/item view.
import { defineStore } from 'pinia'

import {
  createDraft,
  diffCorpusVersions,
  getContext,
  getDraft,
  listContexts,
  listCorpusVersions,
  publishCorpus,
  registerContext,
  rollbackCorpus,
} from '../api'

export const useVersioningStore = defineStore('versioning', {
  state: () => ({
    contexts: [], // flat list from listContexts(); tree is derived in the component
    selectedContext: '',
    contextDetail: null, // { context, ancestors, descendants } for selectedContext
    versions: [], // corpus_versions rows for selectedContext, newest first
    draft: null, // { version, items, localizations } or null if none
    diffFrom: null,
    diffTo: null,
    diffEntries: [],
    loading: false,
    error: '',
  }),

  getters: {
    // A context tree rooted at every context with no parent, for the
    // hierarchy viewer — built client-side from the flat list so the
    // backend doesn't need a dedicated "full tree" endpoint.
    contextTree(state) {
      const byParent = new Map()
      for (const c of state.contexts) {
        const parent = c.parent || ''
        if (!byParent.has(parent)) byParent.set(parent, [])
        byParent.get(parent).push(c)
      }
      const build = (parent) =>
        (byParent.get(parent) || [])
          .sort((a, b) => a.context.localeCompare(b.context))
          .map((c) => ({ ...c, children: build(c.context) }))
      return build('')
    },
    latestPublishedVersion(state) {
      const published = state.versions.filter((v) => v.status === 'published')
      if (published.length === 0) return null
      return published.reduce((max, v) => (v.version > max.version ? v : max), published[0])
    },
    hasDraft(state) {
      return state.versions.some((v) => v.status === 'draft')
    },
  },

  actions: {
    async refreshContexts() {
      this.loading = true
      this.error = ''
      try {
        const res = await listContexts()
        this.contexts = res?.contexts ?? []
      } catch (err) {
        this.error = err.message
      } finally {
        this.loading = false
      }
    },

    async registerNewContext(input) {
      await registerContext(input)
      await this.refreshContexts()
    },

    async selectContext(context) {
      this.selectedContext = context
      this.diffFrom = null
      this.diffTo = null
      this.diffEntries = []
      await Promise.all([this.refreshContextDetail(), this.refreshVersions()])
    },

    async refreshContextDetail() {
      if (!this.selectedContext) {
        this.contextDetail = null
        return
      }
      try {
        this.contextDetail = await getContext(this.selectedContext)
      } catch (err) {
        this.error = err.message
        this.contextDetail = null
      }
    },

    async refreshVersions() {
      if (!this.selectedContext) {
        this.versions = []
        this.draft = null
        return
      }
      this.loading = true
      this.error = ''
      try {
        const res = await listCorpusVersions(this.selectedContext)
        this.versions = res?.versions ?? []
        await this.refreshDraft()
      } catch (err) {
        this.error = err.message
      } finally {
        this.loading = false
      }
    },

    async refreshDraft() {
      if (!this.hasDraft) {
        this.draft = null
        return
      }
      try {
        this.draft = await getDraft(this.selectedContext)
      } catch {
        // A draft row can exist in Versions() while getDraft 404s under a
        // benign race (e.g. rollback just replaced it) — leave draft null
        // rather than surfacing a transient error banner for this.
        this.draft = null
      }
    },

    async createNewDraft(notes = '') {
      this.error = ''
      try {
        await createDraft(this.selectedContext, notes)
        await this.refreshVersions()
      } catch (err) {
        this.error = err.message
        throw err
      }
    },

    async publish() {
      this.error = ''
      try {
        await publishCorpus(this.selectedContext)
        await this.refreshVersions()
      } catch (err) {
        this.error = err.message
        throw err
      }
    },

    async rollbackTo(version, notes = '') {
      this.error = ''
      try {
        await rollbackCorpus(this.selectedContext, version, notes)
        await this.refreshVersions()
      } catch (err) {
        this.error = err.message
        throw err
      }
    },

    setDiffRange(from, to) {
      this.diffFrom = from
      this.diffTo = to
    },

    async runDiff() {
      if (this.diffFrom == null || this.diffTo == null) return
      this.error = ''
      try {
        const res = await diffCorpusVersions(this.selectedContext, this.diffFrom, this.diffTo)
        this.diffEntries = res?.entries ?? []
      } catch (err) {
        this.error = err.message
        this.diffEntries = []
      }
    },
  },
})
